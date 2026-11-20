package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func TestNoWaitVolumeDeleteRejectsBeforeMutation(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("Volume create failed: %+v", result)
	}
	deletingReads := 0
	cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "volume get") {
			if _, err := os.Stat(filepath.Join(dir, "deleted-vol-owned")); err == nil && deletingReads == 0 {
				deletingReads++
				return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": (*scope)[1].templateBinding.Name, "team_id": "team-1", "status": "DELETING"}}}
			}
		}
		return execpkg.Run(ctx, bound, command)
	}
	step := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --no-wait --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || !strings.Contains(result.Error, "requires the waited absence") {
		t.Fatalf("no-wait deletion was admitted: %+v", result)
	}
	if deletingReads != 0 || (*scope)[1].done || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("no-wait deletion lost its retained finalizer: reads=%d done=%v", deletingReads, (*scope)[1].done)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || strings.Contains(string(calls), "volume-delete:vol-owned") {
		t.Fatalf("no-wait deletion reached provider: %v %s", err, calls)
	}
}

func TestSplitNonserialVolumeCasesRejectedBeforeFixtureSetup(t *testing.T) {
	suites := []*scenario.Suite{
		{Resource: "sandbox/volume", Serial: false, Steps: []scenario.Step{{Name: "parent", Run: "ecctl sandbox team create --name owned --output=json"}}},
		{Resource: "sandbox/volume", Serial: false, Steps: []scenario.Step{{Name: "child", Run: "ecctl sandbox volume create --name owned --team-id team-1 --output=json"}}},
	}
	setupCalled := false
	run, err := Run(context.Background(), Options{Suites: suites, Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(t.TempDir(), "must-not-launch"), testFixtureSetup: func(*cleanup, map[string]any) error { setupCalled = true; return nil }})
	if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "one serial case-owned Team") || setupCalled {
		t.Fatalf("split nonserial Volume lifecycle reached fixture setup: run=%+v err=%v", run, err)
	}
}

func TestVolumeDeleteFinalizerRegistrationRejectsBatch(t *testing.T) {
	_, cl, _, scope := volumeRecoveryFixture(t)
	if err := cl.push(scope, "case", "ecctl sandbox volume delete vol-one vol-two", "primary", nil); err == nil {
		t.Fatal("invalid batch finalizer was silently omitted from the journal")
	}
	if err := cl.push(scope, "case", "ecctl ecs instance detach i-one", "primary", nil); err != nil {
		t.Fatalf("in-process non-delete finalizer was rejected: %v", err)
	}
}

func TestVolumeCaseTeamDeleteRequiresActiveCaseOwner(t *testing.T) {
	_, cl, _, scope := volumeRecoveryFixture(t)
	for _, command := range []string{
		"ecctl sandbox team delete team-other --timeout 5m --output=json",
		"ecctl sandbox team delete team-1 --timeout 5m --output=json",
	} {
		if _, err := cl.prepareOwnedTeamDelete(*scope, command, "primary", nil, "another-case"); err == nil {
			t.Fatalf("Volume case accepted a Team outside its active ownership: %s", command)
		}
	}
}

func volumeRecoveryFixture(t *testing.T, transforms ...func(string) string) (execpkg.Config, *cleanup, string, *[]*cleanupItem) {
	t.Helper()
	cfg, cl, dir := nativeCleanupFixture(t, true)
	cl.testSyntheticVolumeReservation = true
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	volumeScript := `
if [[ "$*" == *"capabilities"* ]]; then
 echo '{"surface":"public","products":[{"product":"sandbox"}]}'
 exit 0
fi
if [[ "$*" == *"volume create"* ]]; then
 name=''; prev=''
 for arg in "$@"; do
  [[ "$prev" == '--name' ]] && name="$arg"
  [[ "$arg" == --name=* ]] && name="${arg#--name=}"
  prev="$arg"
 done
 printf '%s' "$name" > "$FAKE_NATIVE_DIR/volume-name"
 echo volume-create >> "$FAKE_NATIVE_DIR/calls"
	id='vol-owned'
	[[ "$(grep -c '^volume-create$' "$FAKE_NATIVE_DIR/calls")" -gt 1 ]] && id='vol-owned-2'
	printf '%s' "$name" > "$FAKE_NATIVE_DIR/volume-name-$id"
	printf '%s' 'team-1' > "$FAKE_NATIVE_DIR/volume-team-$id"
 case "$FAKE_VOLUME_MODE" in
  wait-fail)
   echo '{"error":{"kind":"service","code":"WaitFailed","current_state":"ERROR","recovery_command":["ecctl","sandbox","volume","delete","vol-owned","--region","cn-hangzhou","--team-id","team-1","--profile","account-a"]},"actions":[{"action_name":"CreateVolume","request_id":"req-create"}]}'; exit 1 ;;
  wait-fail-no-request)
   echo '{"error":{"kind":"service","code":"WaitFailed","current_state":"ERROR","recovery_command":["ecctl","sandbox","volume","delete","vol-owned","--region","cn-hangzhou","--team-id","team-1","--profile","account-a"]},"actions":[{"action_name":"CreateVolume"}]}'; exit 1 ;;
  read-transient)
   echo '{"error":{"kind":"service","code":"WaitTimeout","message":"connection reset by peer","recovery_command":["ecctl","sandbox","volume","delete","vol-owned","--region","cn-hangzhou","--team-id","team-1","--profile","account-a"]},"actions":[{"action_name":"CreateVolume","request_id":"req-create"}]}'; exit 1 ;;
  conflict)
   echo '{"error":{"kind":"service","code":"CloudAPIError","retryable":false},"actions":[{"action_name":"CreateVolume","code":"VolumeConflict","request_id":"req-conflict","provider_verified":true}]}'; exit 2 ;;
  generic-conflict)
   echo '{"error":{"kind":"service","code":"CloudAPIError","retryable":false},"actions":[{"action_name":"CreateVolume","code":"409","request_id":"req-conflict","provider_verified":true}]}'; exit 2 ;;
  unverified-conflict)
   echo '{"error":{"kind":"service","code":"CloudAPIError","retryable":false},"actions":[{"action_name":"CreateVolume","code":"VolumeConflict","request_id":"req-conflict","provider_verified":false}]}'; exit 2 ;;
  contradictory-conflict)
   printf '{"volume":{"id":"vol-owned","name":"%s","team_id":"team-1"},"error":{"kind":"service","code":"CloudAPIError","retryable":false},"actions":[{"action_name":"CreateVolume","code":"VolumeConflict","request_id":"req-conflict","provider_verified":true}]}\n' "$name"; exit 2 ;;
  unknown|pending|foreign-list)
   echo 'connection reset by peer' >&2; exit 1 ;;
 esac
 printf '{"volume":{"id":"%s","name":"%s","team_id":"team-1","status":"AVAILABLE"}}\n' "$id" "$name"; exit 0
fi
if [[ "$*" == *"volume list"* ]]; then
 echo "volume-list profile=$profile flag=$given args=$*" >> "$FAKE_NATIVE_DIR/calls"
 [[ "$FAKE_VOLUME_MODE" == pending ]] && echo 'connection reset by peer' >&2 && exit 1
 name="$(cat "$FAKE_NATIVE_DIR/volume-name")"
 [[ "$FAKE_VOLUME_MODE" == foreign-list ]] && printf '{"volumes":[{"id":"vol-foreign","name":"%s","team_id":"team-other"}],"pagination":{"has_more":false}}\n' "$name" && exit 0
 printf '{"volumes":[{"id":"vol-owned","name":"%s","team_id":"team-1"}],"pagination":{"has_more":false}}\n' "$name"; exit 0
fi
if [[ "$*" == *"volume delete"* ]]; then
 id=''; prev=''
 for arg in "$@"; do
  [[ "$prev" == delete ]] && id="$arg" && break
  prev="$arg"
 done
 echo "volume-delete:$id" >> "$FAKE_NATIVE_DIR/calls"
	[[ "$FAKE_VOLUME_DELETE_NOTFOUND" == 1 ]] && touch "$FAKE_NATIVE_DIR/deleted-$id" && echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"DeleteVolume","code":"VolumeNotFound","request_id":"req-delete-absent","provider_verified":true}]}' && exit 4
	[[ "$FAKE_VOLUME_DELETE_GENERIC404" == 1 ]] && touch "$FAKE_NATIVE_DIR/deleted-$id" && echo '{"error":{"kind":"service","code":"CloudAPIError"},"actions":[{"action_name":"DeleteVolume","code":"404","request_id":"req-generic"}]}' && exit 2
 [[ "$FAKE_VOLUME_DELETE_FAIL" == 1 ]] && echo 'volume delete failed' >&2 && exit 2
 if [[ -e "$FAKE_NATIVE_DIR/deleted-$id" ]]; then
  echo '{"error":{"kind":"service","code":"CloudAPIError"},"actions":[{"action_name":"DeleteVolume","code":"VolumeNotFound"}]}'; exit 2
 fi
 touch "$FAKE_NATIVE_DIR/deleted-$id"
 echo '{"absent_in_scope":true,"actions":[{"action_name":"DeleteVolume","request_id":"req-delete"},{"action_name":"GetVolume","code":"VolumeNotFound","request_id":"req-specific","provider_verified":true}]}'; exit 0
fi
if [[ "$*" == *"volume get"* || "$*" == *"volume show"* || "$*" == *"volume describe"* ]]; then
 echo "volume-get profile=$profile flag=$given args=$*" >> "$FAKE_NATIVE_DIR/calls"
	[[ "$FAKE_VOLUME_PREFLIGHT_GENERIC404" == 1 ]] && echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetVolume","code":"404","request_id":"req-generic"}]}' && exit 4
 [[ "$FAKE_VOLUME_GET_UNKNOWN" == 1 ]] && echo '{"error":{"kind":"service","code":"CloudAPIError"},"actions":[{"action_name":"GetVolume","code":"Timeout"}]}' && exit 2
 id=''; selected_team=''; prev=''
 for arg in "$@"; do
  [[ "$prev" == get || "$prev" == show || "$prev" == describe ]] && id="$arg"
	  [[ "$prev" == --team-id ]] && selected_team="$arg"
	  [[ "$arg" == --team-id=* ]] && selected_team="${arg#--team-id=}"
  prev="$arg"
 done
	team="$(cat "$FAKE_NATIVE_DIR/volume-team-$id" 2>/dev/null)"
	name="$(cat "$FAKE_NATIVE_DIR/volume-name-$id" 2>/dev/null)"
	if [[ -n "$selected_team" && "$selected_team" != "$team" ]]; then
	 echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetVolume","code":"VolumeNotFound","request_id":"req-specific","provider_verified":true}]}'; exit 4
	fi
 if [[ -e "$FAKE_NATIVE_DIR/deleted-$id" ]]; then
	  if [[ "$FAKE_VOLUME_GET_UNKNOWN_AFTER_DELETE" == 1 ]]; then
	   echo '{"error":{"kind":"service","code":"CloudAPIError"},"actions":[{"action_name":"GetVolume","code":"Timeout"}]}' ; exit 2
	  fi
	  if [[ "$FAKE_VOLUME_GET_GENERIC404" == 1 ]]; then
	   echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetVolume","code":"404","request_id":"req-generic"}]}'; exit 4
	  fi
	  if [[ "$FAKE_VOLUME_GET_UNVERIFIED" == 1 ]]; then
	   echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetVolume","code":"VolumeNotFound","request_id":"req-specific"}]}'; exit 4
	  fi
	  echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetVolume","code":"VolumeNotFound","request_id":"req-specific","provider_verified":true}]}'; exit 4
 fi
	if [[ -n "$FAKE_VOLUME_RECONCILE_BUCKET" && "$*" == *"--lang=en"* ]]; then
	 printf '{"volume":{"id":"%s","name":"%s","team_id":"%s","status":"AVAILABLE","oss_volume_config":{"bucketName":"%s","bucketPath":"/","endpoint":"https://oss-cn-hangzhou.aliyuncs.com","readOnly":true},"mount_config":{"role":"acs:ram::1234567890123456:role/%s-role"}}}\n' "$id" "$name" "$team" "$FAKE_VOLUME_RECONCILE_BUCKET" "$FAKE_VOLUME_RECONCILE_BUCKET"; exit 0
	fi
	printf '{"volume":{"id":"%s","name":"%s","team_id":"%s","status":"%s"}}\n' "$id" "$name" "$team" "${FAKE_VOLUME_STATUS:-AVAILABLE}"; exit 0
fi
if [[ "$*" == *"volume update"* ]]; then
 id=''; team='team-1'; prev=''
 for arg in "$@"; do
  [[ "$prev" == update ]] && id="$arg"
  [[ "$prev" == --team-id ]] && team="$arg"
  [[ "$arg" == --team-id=* ]] && team="${arg#--team-id=}"
  prev="$arg"
 done
 echo "volume-update:$id" >> "$FAKE_NATIVE_DIR/calls"
 [[ "$FAKE_VOLUME_UPDATE_FAIL" == 1 ]] && echo 'volume update result unknown' >&2 && exit 2
	printf '%s' "$team" > "$FAKE_NATIVE_DIR/volume-team-$id"
 printf '{"volume":{"id":"%s","team_id":"%s","status":"AVAILABLE"}}\n' "$id" "$team"; exit 0
fi
`
	replaced := strings.Replace(string(body), "echo '{}'\n", volumeScript+"echo '{}'\n", 1)
	if replaced == string(body) {
		t.Fatal("fake CLI insertion point missing")
	}
	for _, transform := range transforms {
		replaced = transform(replaced)
	}
	if err := os.WriteFile(cfg.Bin, []byte(replaced), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := journalfile.ExecutableSHA256(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	cl.journalMeta.EcctlBinSHA256 = digest
	cl.journalMeta.JournalPath = cl.journal
	scope := new([]*cleanupItem)
	if _, err := cl.pushItem(scope, "case", "ecctl sandbox team delete team-1 --timeout 5m --output=json", "primary", nil, false, true, false); err != nil {
		t.Fatal(err)
	}
	return cfg, cl, dir, scope
}

// Seed the same durable dispatch, accepted-ID, and nonce-settlement records
// that a successful Volume Team create leaves before a Volume child starts.
func settleVolumeTeamFixture(t *testing.T, cl *cleanup, journal *report.CleanupJournal, binding *report.QuotaCredentialBinding) {
	t.Helper()
	const name = "ecctl-t-0123456789abcdef01234567"
	const teamID = "team-1"
	path := cl.journal
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	description := "ecctl E2E Team owner=" + name[8:]
	intent := &teamCreateIntent{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin, Name: name, Description: description, Input: []string{"ecctl", "sandbox", "team", "create", "--name", name, "--description", description, "--region", journal.Region, "--output=json"}, CaseScope: journal.Entries[0].Scope, Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", VolumeBinding: binding, DispatchProtocol: 1, CreatedAt: time.Now().UTC(), path: path + ".team-create-" + name[8:] + ".json", journalPath: path, parent: parent}
	journal.VolumeTeamCreateIDs = map[string]string{name: ""}
	if err := writeRunnerJournal(path, *journal); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WriteExclusiveDurable(intent.path, body); err != nil {
		t.Fatal(err)
	}
	if err := markVolumeTeamCreateDispatch(cl, intent); err != nil {
		t.Fatal(err)
	}
	result := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": teamID, "name": name, "description": description}}}
	if err := recordVolumeTeamCreateID(cl, intent, teamID, result); err != nil {
		t.Fatal(err)
	}
	journal.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{teamID: binding}
	journal.Entries[0].VolumeTeamBound = true
	if err := settleVolumeTeamRegistration(journal, intent, teamID, path); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerJournal(path, *journal); err != nil {
		t.Fatal(err)
	}
}

func appendSyntheticVolumeBacking(journal *report.CleanupJournal) (string, string, error) {
	if journal == nil || journal.VolumeFixtureBinding == nil {
		return "", "", fmt.Errorf("synthetic Volume fixture credential is missing")
	}
	bucket := journal.VolumeFixtureBucket
	entry := func(kind, teardown string) report.Resource {
		return report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region,
			VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1, Teardown: teardown}
	}
	bucketEntry := entry("bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	bucketEntry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
	roleEntry := entry("role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}'`)
	roleEntry.VolumeFixtureRoleID = "role-created"
	policyEntry := entry("policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"`+bucket+`-read"}'`)
	policyEntry.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
	attachmentEntry := entry("attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role","PolicyName":"`+bucket+`-read","PolicyType":"Custom"}'`)
	journal.Entries = append(journal.Entries, bucketEntry, roleEntry, policyEntry, attachmentEntry)
	oss := `{"bucketName":"` + bucket + `","bucketPath":"/","endpoint":"https://oss-cn-hangzhou.aliyuncs.com","readOnly":true}`
	mount := `{"role":"acs:ram::` + journal.VolumeFixtureBinding.Identity.AccountID + `:role/` + bucket + `-role"}`
	if err := validateOwnedVolumeBacking(*journal, oss, "", "", mount); err != nil {
		return "", "", fmt.Errorf("synthetic Volume backing: %w", err)
	}
	return oss, mount, nil
}

func TestRenderedVolumeShorthandCannotLaunchUnownedWrites(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	data := map[string]any{"inputs": map[string]any{"extra_flags": "-h=false"}}
	callsPath := filepath.Join(dir, "calls")
	before, _ := os.ReadFile(callsPath)
	for _, step := range []scenario.Step{
		{Name: "unowned update", Run: "ecctl --lang sandbox sandbox volume update vol-unowned --team-id team-2 {{.inputs.extra_flags}}"},
		{Name: "unowned delete", Run: "ecctl --profile sandbox sandbox volume delete vol-unowned {{.inputs.extra_flags}}"},
		{Name: "unreserved create", Run: "ecctl --lang sandbox sandbox volume create --name unreserved --team-id team-1 {{.inputs.extra_flags}}"},
		{Name: "raw create", Run: "ecctl --lang sandbox call fcsandbox CreateVolume --request '{}' {{.inputs.extra_flags}}"},
	} {
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, data, step, time.Second); ok || result.Status != report.StatusError {
			t.Fatalf("%s escaped rendered write guard: %+v", step.Name, result)
		}
		after, _ := os.ReadFile(callsPath)
		if string(after) != string(before) {
			t.Fatalf("%s launched subprocess: before=%q after=%q", step.Name, before, after)
		}
	}
	read := scenario.Step{Name: "read only", Run: "ecctl sandbox volume get vol-owned --output=json {{.inputs.extra_flags}}"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, data, read, time.Second); !ok {
		t.Fatalf("read-only Volume command rejected: %+v", result)
	}
}

func TestVolumeExplicitDeleteRetiresOnlyExactSingleTeamFinalizer(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	for range 2 {
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
			t.Fatalf("create: %+v", result)
		}
	}
	deleteStep := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --timeout 5m --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteStep, time.Second); !ok {
		t.Fatalf("exact single-Team delete was not completed: %+v", result)
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || !(*scope)[1].done || (*scope)[2].done || !strings.Contains(entries[1].Teardown, "vol-owned-2") {
		t.Fatalf("single-Team delete changed the sibling Volume: %+v", entries)
	}
	journal, _, err := readRunnerJournal(cl.journal)
	if err != nil || len(journal.VolumeTerminalReceipts) != 1 {
		t.Fatalf("exact Volume terminal receipt was not retained: %v %+v", err, journal.VolumeTerminalReceipts)
	}
	for _, receipt := range journal.VolumeTerminalReceipts {
		if receipt.VolumeID != "vol-owned" || receipt.TeamID != "team-1" || receipt.Region != "cn-hangzhou" || receipt.Profile != "account-a" {
			t.Fatalf("terminal receipt changed the original Volume binding: %+v", receipt)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || strings.Contains(string(calls), "volume-delete:vol-owned-2") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("explicit delete changed another resource: %s", calls)
	}
}

func TestVolumeAlreadyAbsentRestartReplayConfirmsGetBeforeTeam(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(dir, "deleted-vol-owned"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Errors != 0 || replay.Deleted != 2 {
		t.Fatalf("single-Team absence replay did not retire Volume before Team: result=%+v err=%v", replay, err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	get := "volume-get profile=account-a flag=account-a args=sandbox volume get vol-owned --team-id team-1 --region cn-hangzhou --profile account-a"
	if strings.Contains(string(calls), "volume-delete:vol-owned") || !strings.Contains(string(calls), get) || !strings.Contains(string(calls), "team-delete") ||
		strings.Index(string(calls), get) > strings.Index(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("terminal replay missed exact read or Team cleanup: %s", calls)
	}
}

func TestAcceptedNoWaitVolume404DoesNotRetireChildOrParent(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("accepted no-wait create: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(dir, "deleted-vol-owned"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_VOLUME_PREFLIGHT_GENERIC404", "1")
	replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	if err != nil || replay.Errors == 0 || replay.Deleted != 0 {
		t.Fatalf("provisional 404 retired accepted Volume: %+v %v", replay, err)
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 2 {
		t.Fatalf("provisional 404 lost child or parent: %+v", entries)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("provisional 404 caused mutation: %s", calls)
	}
}

func TestVolumeAlreadyAbsentAuthoredDeleteRetiresAfterExactRead(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(dir, "deleted-vol-owned"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deleteStep := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteStep, time.Second); ok || !strings.Contains(result.Error, "already absent") {
		t.Fatalf("authored delete passed without a DeleteVolume action: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete") || !strings.Contains(string(calls), "volume-get") || len(readJournalEntries(t, cl.journal)) != 1 || !(*scope)[1].done {
		t.Fatalf("already absent Volume sent delete or retained finalizer: calls=%s", calls)
	}
}

func TestVolumeUnstartedDeleteRestoresOnlyFreshMarker(t *testing.T) {
	for _, mode := range []string{"authored", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
				t.Fatalf("create: %+v", result)
			}
			injected := 0
			markedAtInjection := false
			cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
				if request, err := journalfile.ParseVolumeDeleteRequest(command); err == nil && request.ID == "vol-owned" {
					injected++
					journal, legacy, readErr := readRunnerJournal(cl.journal)
					markedAtInjection = readErr == nil && !legacy && len(journal.Entries) == 2 &&
						journal.Entries[1].Teardown == (*scope)[1].cmd && journal.Entries[1].VolumeDeleteAttempted
					return execpkg.Result{Exit: -1, Err: fmt.Errorf("synthetic start failure")}
				}
				return execpkg.Run(ctx, bound, command)
			}
			if mode == "authored" {
				step := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
				if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
					t.Fatalf("unstarted authored delete succeeded: %+v", result)
				}
			} else {
				cl.keep = false
				if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
					t.Fatal("unstarted ordinary cleanup succeeded")
				}
			}
			if injected != 1 || !markedAtInjection {
				t.Fatalf("unstarted fault was not injected after an exact durable Volume marker: injected=%d marked=%t", injected, markedAtInjection)
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 2 || entries[1].Teardown != (*scope)[1].cmd || entries[1].VolumeDeleteAttempted || (*scope)[1].volumeDeleteAttempted || (*scope)[1].done {
				t.Fatalf("unused fresh marker was not restored: %+v", entries)
			}
			if err := cl.markOwnedVolumeDeleteAttempt((*scope)[1]); err != nil {
				t.Fatalf("install historical attempt marker: %v", err)
			}
			t.Setenv("FAKE_VOLUME_STATUS", "ERROR")
			if mode == "authored" {
				step := scenario.Step{Name: "historical delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
				if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || !strings.Contains(result.Error, "already attempted") {
					t.Fatalf("historical marker allowed another authored delete: %+v", result)
				}
			} else if failures := cl.runCleanup(*scope, false); len(failures) == 0 || !strings.Contains(strings.Join(failures, "\n"), "already attempted") {
				t.Fatalf("historical marker allowed another cleanup delete: %v", failures)
			}
			entries = readJournalEntries(t, cl.journal)
			if injected != 1 || len(entries) != 2 || !entries[1].VolumeDeleteAttempted || !(*scope)[1].volumeDeleteAttempted || (*scope)[1].done {
				t.Fatalf("historical Volume marker was reset or another delete was launched: injected=%d entries=%+v", injected, entries)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "volume-delete") || strings.Contains(string(calls), "team-delete") {
				t.Fatalf("unstarted delete reached the provider: %s", calls)
			}
		})
	}
}

func TestVolumeInvalidDeleteTimeoutLeavesFreshMarkerUnused(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	for _, timeout := range []string{"invalid", "0s"} {
		step := scenario.Step{Name: "invalid delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --timeout " + timeout + " --output=json"}
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
			t.Fatalf("invalid delete timeout %q was admitted: %+v", timeout, result)
		}
		entries := readJournalEntries(t, cl.journal)
		if len(entries) != 2 || entries[1].VolumeDeleteAttempted || (*scope)[1].volumeDeleteAttempted {
			t.Fatalf("invalid delete timeout %q spent a marker: %+v", timeout, entries)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete:") {
		t.Fatalf("invalid delete timeout reached provider: %s", calls)
	}
}

func TestVolumeAcceptedErrorReceiptRequiresConfirmedAllocation(t *testing.T) {
	intent := &templateCreateIntent{TemplateCreateIntent: journalfile.TemplateCreateIntent{Name: "workspace", TeamID: "team-1", Region: "cn-hangzhou"}}
	accepted := map[string]any{"id": "vol-1", "name": intent.Name, "team_id": intent.TeamID, "region": intent.Region, "operation": "CreateVolume", "request_id": "req-1"}
	root := map[string]any{
		"error":   map[string]any{"kind": "service", "code": "InvalidVolumeResponse", "retryable": false, "accepted_volume": accepted},
		"actions": []any{map[string]any{"action_name": "CreateVolume", "code": "InvalidVolumeResponse", "request_id": "req-1"}},
	}
	result := execpkg.Result{Started: true, Exit: 2}
	if _, ok := volumeAcceptedCreateErrorReceipt(result, root, intent, "vol-1"); ok {
		t.Fatal("diagnostic tuple without allocation proof entered cleanup")
	}
	accepted["allocation_confirmed"] = true
	if requestID, ok := volumeAcceptedCreateErrorReceipt(result, root, intent, "vol-1"); !ok || requestID != "req-1" {
		t.Fatalf("confirmed allocation was not retained: %q %t", requestID, ok)
	}
}

func TestDefinitivelyRejectedVolumeDeleteCanRetryExactID(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	denials := 0
	cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if request, err := journalfile.ParseVolumeDeleteRequest(command); err == nil && request.ID == "vol-owned" {
			denials++
			return execpkg.Result{Started: true, Exit: 2, JSON: map[string]any{
				"error":   map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false},
				"actions": []any{map[string]any{"action_name": "DeleteVolume", "code": "Forbidden", "request_id": "req-denied", "provider_verified": true}},
			}}
		}
		return execpkg.Run(ctx, bound, command)
	}
	deleteStep := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteStep, time.Second); ok || !strings.Contains(result.Error, "retry is available") {
		t.Fatalf("verified rejection was not settled: %+v", result)
	}
	if entries := readJournalEntries(t, cl.journal); denials != 1 || len(entries) != 2 || entries[1].VolumeDeleteAttempted || (*scope)[1].volumeDeleteAttempted {
		t.Fatalf("verified rejection retained a spent marker: denials=%d entries=%+v", denials, entries)
	}
	cl.runCommand = execpkg.Run
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteStep, time.Second); !ok {
		t.Fatalf("retry did not finish after exact VolumeNotFound: %+v", result)
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 1 || !(*scope)[1].done {
		t.Fatalf("exact VolumeNotFound did not retire only the child: %+v", entries)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("retry changed another resource: %s", calls)
	}
}

func TestParallelTeamCaseCleanupDefersUntilSiblingVolumeAndStackDependencySettle(t *testing.T) {
	cfg, cl, dir, teamScope := volumeRecoveryFixture(t)
	// These two supported primary resources can run as independent cases. The
	// fixture uses the same fake provider and journal that the runner uses.
	teamSuite := &scenario.Suite{Resource: "sandbox/team", Serial: false}
	volumeSuite := &scenario.Suite{Resource: "sandbox/volume", Serial: false,
		Steps: []scenario.Step{{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}}}
	if teamSuite.Resource == volumeSuite.Resource || teamSuite.Serial || volumeSuite.Serial || len(teamSuite.Needs) != 0 || len(volumeSuite.Needs) != 0 || len(teamSuite.RequiresPrerequisites) != 0 || len(volumeSuite.RequiresPrerequisites) != 0 {
		t.Fatal("test no longer models two independent nonserial primary suites")
	}
	var volumeScope []*cleanupItem
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, &volumeScope, map[string]any{}, volumeSuite.Steps[0], time.Second); !ok {
		t.Fatalf("sibling Volume allocation: %+v", result)
	}
	var stackScope []*cleanupItem
	if _, err := cl.pushItem(&stackScope, "stack", "ecctl sandbox team delete team-2 --timeout 5m --output=json", "primary", nil, false, true, false); err != nil {
		t.Fatal(err)
	}
	cl.keep = false
	teamFinished := make(chan []string, 1)
	go func() { teamFinished <- cl.runCaseCleanup(*teamScope) }()
	select {
	case failures := <-teamFinished:
		if len(failures) != 0 || len(cl.deferredScopes) != 1 || !(*teamScope)[0].deferred {
			t.Fatalf("Team scope did not enter deferred cleanup: failures=%v scopes=%d item=%+v", failures, len(cl.deferredScopes), (*teamScope)[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Team case cleanup did not finish its live-child wait")
	}
	if calls, _ := os.ReadFile(filepath.Join(dir, "calls")); strings.Contains(string(calls), "team-delete") || strings.Contains(string(calls), "volume-delete") {
		t.Fatalf("provider cleanup ran before sibling Volume case finished: %s", calls)
	}
	t.Setenv("FAKE_VOLUME_GET_GENERIC404", "1")
	if failures := cl.runCaseCleanup(volumeScope); len(failures) == 0 {
		t.Fatal("scoped absence released sibling Volume cleanup")
	}
	journal, _, err := readRunnerJournal(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	if outstanding, err := journalfile.OutstandingVolumeChild(cl.journal, journal, cfg.Env); err != nil || !outstanding {
		t.Fatalf("retained child did not fence deferred Team: outstanding=%v err=%v", outstanding, err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || strings.Count(string(calls), "volume-delete") != 1 || strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) < 3 || (*teamScope)[0].done {
		t.Fatalf("retained Volume released a parent: %s %v", calls, err)
	}
	_ = stackScope
}

func TestDeferredParentBeforeChildRetriesOnlyAfterChildProgress(t *testing.T) {
	for _, includeChild := range []bool{true, false} {
		t.Run(map[bool]string{true: "child follows parent", false: "no progress"}[includeChild], func(t *testing.T) {
			cfg, cl, dir, teamScope := volumeRecoveryFixture(t)
			create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			var volumeScope []*cleanupItem
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, &volumeScope, map[string]any{}, create, time.Second); !ok {
				t.Fatalf("Volume create: %+v", result)
			}
			cl.mu.Lock()
			(*teamScope)[0].deferred = true
			cl.deferredScopes = append(cl.deferredScopes, *teamScope)
			if includeChild {
				volumeScope[0].deferred = true
				cl.deferredScopes = append(cl.deferredScopes, volumeScope)
			}
			cl.mu.Unlock()
			cl.keep = false
			t.Setenv("FAKE_VOLUME_GET_GENERIC404", "1")
			failures := cl.runDeferredCleanup()
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if includeChild {
				if len(failures) == 0 || (*teamScope)[0].done || volumeScope[0].done || strings.Contains(string(calls), "team-delete") || strings.Count(string(calls), "volume-delete") != 1 {
					t.Fatalf("deferred parent/child order: failures=%v calls=%s", failures, calls)
				}
			} else if len(failures) != 1 || (*teamScope)[0].done || strings.Contains(string(calls), "team-delete") || strings.Contains(string(calls), "volume-delete") {
				t.Fatalf("stalled deferred parent was attempted: failures=%v calls=%s", failures, calls)
			}
		})
	}
}

func TestIndependentVolumeCleanupContinuesAfterOneUncertainDelete(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "success-two")
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	for range 2 {
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
			t.Fatalf("owned Volume create: %+v", result)
		}
	}
	originalRun := cl.runCommand
	failedDelete := 0
	cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "delete") && strings.Contains(command, "vol-owned-2") {
			failedDelete++
			if err := os.WriteFile(filepath.Join(dir, "deleted-vol-owned-2"), nil, 0o600); err != nil {
				return execpkg.Result{Exit: -1, Err: err}
			}
			return execpkg.Result{Started: true, Exit: 2, Err: errors.New("uncertain provider completion")}
		}
		return originalRun(ctx, bound, command)
	}
	cl.keep = false
	t.Setenv("FAKE_VOLUME_GET_GENERIC404", "1")
	failures := cl.runCleanup(*scope, false)
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || failedDelete != 1 || len(failures) != 2 || strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 3 {
		t.Fatalf("independent child did not run after uncertain sibling: failed=%d failures=%v calls=%s err=%v", failedDelete, failures, calls, err)
	}
}

func TestReplayVolumeDeletePersistsTerminalReceiptAcrossRestart(t *testing.T) {
	cfg, cl, _, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "success")
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("owned Volume create: %+v", result)
	}
	first, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	if err != nil || first == nil || first.Deleted < 1 {
		t.Fatalf("Volume replay did not consume its finalizer: result=%+v err=%v", first, err)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy || len(journal.VolumeTerminalReceipts) != 1 {
		t.Fatalf("Volume replay lost its terminal receipt: journal=%+v err=%v", journal, err)
	}
	if outstanding, err := journalfile.OutstandingVolumeChild(cl.journal, journal, cfg.Env); err != nil || outstanding {
		t.Fatalf("terminal Volume still fences its Team: outstanding=%t err=%v", outstanding, err)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil {
		t.Fatalf("restart replay rejected terminal receipt: %v", err)
	}
	installed, _, err := readRunnerJournal(cl.journal)
	if err != nil || !reflect.DeepEqual(installed.VolumeTerminalReceipts, journal.VolumeTerminalReceipts) {
		t.Fatalf("restart changed terminal receipt: journal=%+v err=%v", installed, err)
	}
}

func TestVolumeUnprovenNoWaitAndForeignDeleteKeepOwnedJournal(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	foreign := scenario.Step{Name: "foreign", Run: "ecctl sandbox volume delete vol-other --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, foreign, time.Second); ok || result.Error == "" {
		t.Fatalf("foreign delete reached provider: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete:vol-other") {
		t.Fatalf("foreign Volume delete reached provider: %s", calls)
	}
	if len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
		t.Fatal("foreign Volume deletion consumed the owned finalizer")
	}
	t.Setenv("FAKE_VOLUME_DELETE_FAIL", "1")
	t.Setenv("FAKE_VOLUME_STATUS", "ERROR")
	owned := scenario.Step{Name: "failure", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, owned, time.Second); ok || result.Exit != 2 {
		t.Fatalf("failed delete unexpectedly succeeded: %+v", result)
	}
	if len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
		t.Fatal("unproven Volume failure consumed the finalizer")
	}
	if entries := readJournalEntries(t, cl.journal); !entries[1].VolumeDeleteAttempted {
		t.Fatal("possibly submitted Volume delete lacked a durable attempt marker")
	}
	t.Setenv("FAKE_VOLUME_DELETE_FAIL", "0")
	noWait := scenario.Step{Name: "accepted only", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --no-wait --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, noWait, time.Second); ok || !strings.Contains(result.Error, "requires the waited absence") {
		t.Fatalf("uncertain delete was repeated: %+v", result)
	}
	if len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
		t.Fatal("no-wait request consumed the finalizer")
	}
	if err := cl.withOwnedTeamJournal((*scope)[0], true); err == nil || !strings.Contains(err.Error(), "Volume child") {
		t.Fatalf("Team mark accepted an outstanding Volume child: %v", err)
	}
	if err := cl.withOwnedTeamJournal((*scope)[0], false); err == nil || !strings.Contains(err.Error(), "Volume child") {
		t.Fatalf("Team consume accepted an outstanding Volume child: %v", err)
	}
	cl.keep = false
	if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
		t.Fatal("uncertain Volume delete was repeated during cleanup")
	}
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete:vol-owned") != 1 || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("uncertain Volume delete reached the provider twice or removed its Team: %s", calls)
	}
}

func TestVolumeMutationsRejectForeignTargetsBeforeProvider(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	createOwnedVolumeForMutation(t, cfg, cl, scope)
	before := readJournalEntries(t, cl.journal)
	for _, command := range []string{
		"ecctl sandbox volume delete vol-other --team-id team-1 --output=json",
		"ecctl sbx volume rm vol-other --output=json",
		"ecctl sandbox volume delete vol-owned --team-id team-other --output=json",
		"ecctl sandbox volume update vol-other --status=AVAILABLE --output=json",
		"ecctl --region=cn-hangzhou sbx volume update vol-other --team-id=team-1 --output=json",
		"ecctl sandbox volume update vol-owned --team-id team-other --output=json",
		"ecctl sandbox volume update vol-owned --region other-region --status=AVAILABLE --output=json",
		"ecctl --bogus=value sandbox volume update vol-owned --status=AVAILABLE --output=json",
		"ecctl call fcsandbox UpdateVolume --api-param volumeID=vol-other",
		"ecctl call fcsandbox DeleteVolume --api-param volumeID=vol-other",
	} {
		step := scenario.Step{Name: "reject foreign Volume mutation", Run: command}
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
			t.Errorf("mutation was not rejected before provider: %s: %+v", command, result)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-update:") || strings.Contains(string(calls), "volume-delete:") {
		t.Errorf("foreign Volume mutation reached provider: %s", calls)
	}
	if after := readJournalEntries(t, cl.journal); !reflect.DeepEqual(after, before) || (*scope)[1].done {
		t.Errorf("rejected Volume mutation changed finalizer: before=%+v after=%+v", before, after)
	}
}

func TestVolumeDeleteRejectsChangedExecutionConfig(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	createOwnedVolumeForMutation(t, cfg, cl, scope)
	cfg.Env = append(cfg.Env, "ECCTL_CONFIG_PATH="+filepath.Join(dir, "other-config.json"))
	step := scenario.Step{Name: "changed config delete", Run: "ecctl sandbox volume delete vol-owned --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
		t.Fatalf("changed config was accepted: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete:") || (*scope)[1].done || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("changed config reached Volume delete or consumed finalizer: %s", calls)
	}
}

func createOwnedVolumeForMutation(t *testing.T, cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem) {
	t.Helper()
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
}

func TestVolumeCoLocatedRoleCannotBypassChildFence(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	cl.execCfg["secondary"] = cfg
	primaryJournal := cl.journalMeta.JournalPath
	cl.journalMeta.JournalPath = cleanupJournalPath(cl.journal, "secondary")
	secondary, err := cl.pushItem(scope, "case", "ecctl sandbox team delete team-2 --timeout 5m --output=json", "secondary", nil, false, true, false)
	cl.journalMeta.JournalPath = primaryJournal
	if err != nil {
		t.Fatal(err)
	}
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	if _, err := cl.prepareOwnedTeamDelete(*scope, secondary.cmd, "secondary", secondary); err == nil || !strings.Contains(err.Error(), "Volume child") {
		t.Fatalf("co-located Team delete escaped primary Volume child: %v", err)
	}
	secondaryCreate := scenario.Step{Name: "create under secondary Team", Run: "ecctl sandbox volume create --name authored --team-id team-2 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, secondaryCreate, time.Second); ok || !strings.Contains(result.Error, "primary cleanup role") {
		t.Fatalf("secondary Team allocation escaped role fence: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-create") != 1 || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("co-located role fence allowed a cloud mutation: %s", calls)
	}
}

func TestVolumeJournalTamperingCannotAuthorizeTeamDelete(t *testing.T) {
	for _, kind := range []string{"foreign-volume-id", "foreign-run"} {
		t.Run(kind, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
				t.Fatalf("create: %+v", result)
			}
			if err := journalfile.WithExistingParentLock(context.Background(), cl.journal, func() error {
				journal, _, err := readRunnerJournal(cl.journal)
				if err != nil {
					return err
				}
				if kind == "foreign-run" {
					journal.RunID = "other-run"
				} else {
					journal.Entries[1].Teardown = "ecctl sandbox volume delete vol-foreign --region cn-hangzhou --profile account-a"
				}
				return writeRunnerJournal(cl.journal, journal)
			}); err != nil {
				t.Fatal(err)
			}
			deleteTeam := scenario.Step{Name: "parent delete", Run: "ecctl sandbox team delete team-1 --timeout 5m --output=json"}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteTeam, time.Second); ok || result.Error == "" {
				t.Fatalf("tampered journal allowed Team delete: %+v", result)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "team-delete") {
				t.Fatalf("tampered Team delete reached provider: %s", calls)
			}
		})
	}
}

func TestVolumeDeleteKeepsFinalizerWhenAbsenceProbeIsUnknown(t *testing.T) {
	cfg, cl, _, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	t.Setenv("FAKE_VOLUME_GET_UNKNOWN_AFTER_DELETE", "1")
	deleteVolume := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, deleteVolume, time.Second); ok || !strings.Contains(result.Error, "unproven") {
		t.Fatalf("unknown absence was accepted: %+v", result)
	}
	if len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
		t.Fatal("unknown absence consumed the Volume finalizer")
	}
	cl.keep = false
	if failures := cl.runCleanup(*scope, false); len(failures) == 0 || len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
		t.Fatalf("scoped absence retired the protected finalizer: %v", failures)
	}
}

func TestDeletingVolumeIsObservedWithoutAnotherDelete(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	t.Setenv("FAKE_VOLUME_STATUS", "DELETING")
	step := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || !strings.Contains(result.Error, "already deleting") {
		t.Fatalf("case delete did not retain an in-progress Volume: %+v", result)
	}
	cl.keep = false
	if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
		t.Fatal("case cleanup treated an in-progress Volume as deleted")
	}
	if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || result.Errors == 0 || result.Deleted != 0 {
		t.Fatalf("restart replay did not retain an in-progress Volume: %+v, %v", result, err)
	}
	entries := readJournalEntries(t, cl.journal)
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || len(entries) != 2 || entries[1].VolumeDeleteAttempted || strings.Contains(string(calls), "volume-delete:") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("in-progress Volume lost custody or reached delete: %+v, %s, %v", entries, calls, err)
	}
}

func TestVolumeUnprovenAbsenceKeepsFinalizerAndTeam(t *testing.T) {
	for _, tc := range []struct {
		name, mode    string
		genericDelete bool
		unverifiedGet bool
	}{
		{name: "case delete returns generic", mode: "case", genericDelete: true},
		{name: "case successful delete then generic get", mode: "case"},
		{name: "cleanup delete returns generic", mode: "cleanup", genericDelete: true},
		{name: "restart delete returns generic", mode: "replay", genericDelete: true},
		{name: "cleanup successful delete then generic get", mode: "cleanup"},
		{name: "restart successful delete then generic get", mode: "replay"},
		{name: "cleanup delete then unverified exact absence", mode: "cleanup", unverifiedGet: true},
		{name: "restart delete then unverified exact absence", mode: "replay", unverifiedGet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
				t.Fatalf("create: %+v", result)
			}
			if tc.genericDelete {
				t.Setenv("FAKE_VOLUME_DELETE_GENERIC404", "1")
			}
			if tc.unverifiedGet {
				t.Setenv("FAKE_VOLUME_GET_UNVERIFIED", "1")
			} else {
				t.Setenv("FAKE_VOLUME_GET_GENERIC404", "1")
			}
			switch tc.mode {
			case "case":
				step := scenario.Step{Name: "delete", Run: "ecctl sandbox volume delete vol-owned --team-id team-1 --output=json"}
				if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok {
					t.Fatalf("generic 404 confirmed case deletion: %+v", result)
				}
				if len(readJournalEntries(t, cl.journal)) != 2 || (*scope)[1].done {
					t.Fatal("case delete consumed an unproven Volume finalizer")
				}
				cl.keep = false
				if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
					t.Fatal("case cleanup accepted generic Volume 404")
				}
			case "cleanup":
				cl.keep = false
				if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
					t.Fatal("cleanup accepted generic Volume 404")
				}
			case "replay":
				replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
				if err != nil || replay.Errors != 1 || replay.Deleted != 0 {
					t.Fatalf("restart replay accepted generic Volume 404: result=%+v err=%v", replay, err)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 2 || !entries[1].VolumeDeleteAttempted || strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || strings.Contains(string(calls), "team-delete") {
				t.Fatalf("unproven Volume outcome lost its single attempted delete or Team fence: journal=%+v calls=%s", entries, calls)
			}
			firstRead, deleteCall := strings.Index(string(calls), "volume-get"), strings.Index(string(calls), "volume-delete:vol-owned")
			if firstRead < 0 || deleteCall <= firstRead {
				t.Fatalf("Volume delete had no successful preflight read: %s", calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "deleted-vol-owned")); err != nil || strings.Index(string(calls)[deleteCall:], "volume-get") < 0 {
				t.Fatalf("post-delete absence probe did not follow ambiguous delete: %s %v", calls, err)
			}
		})
	}
}

func TestVolumeUnprovenPreflightNeverSendsDelete(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	t.Setenv("FAKE_VOLUME_PREFLIGHT_GENERIC404", "1")
	cl.keep = false
	if failures := cl.runCleanup(*scope, false); len(failures) == 0 {
		t.Fatal("generic preflight absence was accepted")
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || entries[1].VolumeDeleteAttempted || strings.Contains(string(calls), "volume-delete:") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("unproven preflight sent mutation or changed delete marker: journal=%+v calls=%s", entries, calls)
	}
}

func TestVolumeNoWaitCancellationAfterAcceptedIDKeepsReplayableFinalizer(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readinessStarted := false
	cl.runCommand = func(callCtx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "'volume' 'get'") {
			readinessStarted = true
			cancel()
			return execpkg.Result{Command: command, Started: false, Exit: -1, Err: callCtx.Err()}
		}
		return execpkg.Run(callCtx, bound, command)
	}
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(ctx, Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); ok || !readinessStarted || !strings.Contains(result.Error, "readiness") {
		t.Fatalf("canceled readiness lost the accepted create boundary: %+v started=%v", result, readinessStarted)
	}
	paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("accepted Volume intent is missing: %v %v", paths, err)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.AcceptedVolumeID != "vol-owned" || intent.State != "journaled" {
		t.Fatalf("accepted ID was not retained before readiness: %+v %v", intent, err)
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || !strings.Contains(entries[1].Teardown, "vol-owned") || (*scope)[1].done {
		t.Fatalf("canceled readiness lost Volume cleanup authority: %+v", entries)
	}
	if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Errors != 1 || replay.Deleted != 0 || !strings.Contains(strings.Join(replay.Details, "\n"), "requires durable positive observation") {
		t.Fatalf("restart did not fence an unobserved Volume: %+v %v", replay, err)
	}
	observedBody, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(observedBody, &intent) != nil || intent.VolumeObserved {
		t.Fatalf("replay silently promoted an unobserved Volume: %+v %v", intent, err)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy || len(journal.VolumeTerminalReceipts) != 0 || len(journal.Entries) != 2 || journal.Entries[1].VolumeDeleteAttempted {
		t.Fatalf("unobserved Volume or Team lost its replay fence: %+v %v", journal, err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-create\n") != 1 || !strings.Contains(string(calls), "volume-get") ||
		strings.Contains(string(calls), "volume-delete:") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("restart repeated creation or deleted an unobserved Volume: %s", calls)
	}
}

func TestVolumeNoWaitReadinessPersistsMatchedObservation(t *testing.T) {
	cfg, cl, _, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("matched no-wait readiness failed: %+v", result)
	}
	paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("accepted Volume intent missing: %v %v", paths, err)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.State != "journaled" || intent.AcceptedVolumeID != "vol-owned" || !intent.VolumeObserved {
		t.Fatalf("matched readiness observation was not retained: %+v %v", intent, err)
	}
}

func TestHistoricalVolumeTransferMarkerBlocksNewDelete(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	item := (*scope)[1]
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy {
		t.Fatalf("read owned journal: %v", err)
	}
	journal.VolumeTransferHistory = map[string]map[string]bool{item.templateBinding.Name: {"team-other": true}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cl.journal, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ownedVolumeDeleteScopes(item); err == nil || !strings.Contains(err.Error(), "historical Volume transfer") {
		t.Fatalf("historical transfer was admitted for a new delete: %v", err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete") {
		t.Fatalf("historical transfer sent a delete: %s", calls)
	}
}

func TestObservedVolumeNotFoundDeleteRetiresChild(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	t.Setenv("FAKE_VOLUME_DELETE_NOTFOUND", "1")
	cl.keep = false
	failures := cl.run(*scope)
	if !(*scope)[1].done {
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		t.Fatalf("observed Volume was not retired after exact terminal absence: failures=%v calls=%s", failures, calls)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy || len(journal.VolumeTerminalReceipts) != 1 {
		t.Fatalf("Volume terminal receipt was not retained: %+v %v", journal, err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	firstRead, deleteCall := strings.Index(string(calls), "volume-get"), strings.Index(string(calls), "volume-delete:vol-owned")
	if strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || firstRead < 0 || deleteCall < firstRead {
		t.Fatalf("Volume delete was not preceded by the original-Team read: %s", calls)
	}
}

func TestOwnedVolumeListUserIDReachesBoundSubprocess(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	step := scenario.Step{Name: "list by user", Run: "ecctl sandbox volume list --team-id team-1 --user-id user-a --limit 1 --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("bound list rejected public user-id: %+v", result)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || !strings.Contains(string(calls), "volume-list profile=account-a flag=account-a") || !strings.Contains(string(calls), "--user-id user-a") {
		t.Fatalf("bound user-id list did not reach subprocess: %s %v", calls, err)
	}
}

func TestUnownedVolumeReadsRejectForeignRegionBeforeProvider(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, true)
	providerCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	for _, command := range []string{
		"ecctl sandbox volume get vol-any --region cn-beijing --output=json",
		"ecctl sandbox volume get vol-any --region=cn-beijing --output=json",
		"ecctl sandbox volume list --team-id team-any --region cn-beijing --output=json",
		"ecctl sandbox volume list --team-id team-any --region=cn-beijing --output=json",
		"ecctl sandbox volume get vol-any --region cn-beijing --region cn-hangzhou --output=json",
	} {
		step, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, new([]*cleanupItem), map[string]any{}, scenario.Step{Name: "foreign region read", Run: command}, time.Second)
		if ok || step.Status != report.StatusError || providerCalls != 0 {
			t.Fatalf("unowned Volume read reached provider: command=%q step=%+v calls=%d", command, step, providerCalls)
		}
	}
	for _, command := range []string{
		"ecctl sandbox volume get vol-any --region cn-hangzhou --output=json",
		"ecctl sandbox volume list --team-id team-any --region=cn-hangzhou --output=json",
		"ecctl ecs instance list --region cn-beijing --output=json",
	} {
		bound, _, err := cl.bindOwnedVolumeRead(context.Background(), command, "primary", cfg, time.Second)
		if err != nil || bound != command {
			t.Fatalf("unowned read was changed: command=%q bound=%q err=%v", command, bound, err)
		}
	}
}

func TestVolumeCreateRejectsTeamCreatorOAuthGenerationDrift(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "account-a")
	cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=account-a", "FAKE_NATIVE_DIR=" + dir}
	cl.execCfg["primary"] = cfg
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config := func(generation string) []byte {
		return []byte(`{"profiles":[{"name":"account-a","mode":"OAuth","oauth_site_type":"International","oauth_generation":"` + generation + `","oauth_account_id":"1234567890123456"}]}`)
	}
	if err := os.WriteFile(native, config("generation-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, _, err := captureQuotaCredentialBinding(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cl.volumeFixtureBinding = binding
	cl.volumeFixtureBucket = "ecctl-e2e-20261104t120000-123456abcdef"
	journal, _, err := readRunnerJournal(cl.journal)
	if err != nil || len(journal.Entries) != 1 {
		t.Fatalf("Team fixture journal = %+v, err=%v", journal, err)
	}
	journal.VolumeFixtureBinding = binding
	journal.VolumeFixtureBucket = cl.volumeFixtureBucket
	oss, mount, err := appendSyntheticVolumeBacking(&journal)
	if err != nil {
		t.Fatal(err)
	}
	settleVolumeTeamFixture(t, cl, &journal, binding)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --oss-volume-config '" + oss + "' --mount-config '" + mount + "' --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("unchanged Team creator credential was rejected: %+v", result)
	}
	before, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, config("generation-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second)
	after, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || ok || result.Status != report.StatusError || !strings.Contains(result.Error, "configuration state changed") || string(after) != string(before) {
		t.Fatalf("same-principal OAuth rotation reached Volume create: result=%+v before=%s after=%s err=%v", result, before, after, err)
	}
}

func TestOwnedVolumeReadRejectsSelectorConfigPrincipalAndSourceConflicts(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	for _, command := range []string{
		"ecctl sandbox volume get vol-owned --team-id team-1 --profile account-b --output=json",
		"ecctl sandbox volume show vol-owned --team-id team-1 --region cn-shanghai --output=json",
		"ecctl sandbox volume get vol-owned --team-id team-other --output=json",
		"ecctl sandbox volume list --team-id team-1 --profile account-b --limit 1 --output=json",
		"ecctl sandbox volume list --team-id team-other --team-id team-1 --output=json",
		"ecctl sandbox volume get vol-owned --region cn-shanghai --region cn-hangzhou --output=json",
	} {
		before, _ := os.ReadFile(filepath.Join(dir, "calls"))
		result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, scenario.Step{Name: "conflicting read", Run: command}, time.Second)
		after, _ := os.ReadFile(filepath.Join(dir, "calls"))
		if ok || result.Status != report.StatusError || string(after) != string(before) {
			t.Fatalf("conflicting selector reached provider: command=%s result=%+v calls=%s", command, result, after)
		}
	}
	originalConfigPath := os.Getenv("ECCTL_CONFIG_PATH")
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "other-config.json"))
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, scenario.Step{Name: "target drift", Run: "ecctl sandbox volume get vol-owned --team-id team-1 --output=json"}, time.Second); ok || result.Status != report.StatusError {
		t.Fatalf("changed config target was accepted: %+v", result)
	}
	t.Setenv("ECCTL_CONFIG_PATH", originalConfigPath)
	t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, scenario.Step{Name: "principal drift", Run: "ecctl sandbox volume get vol-owned --team-id team-1 --output=json"}, time.Second); ok || result.Status != report.StatusError {
		t.Fatalf("changed principal was accepted: %+v", result)
	}
	t.Setenv("FAKE_PRINCIPAL_USER", "")
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("second create: %+v", result)
	}
	(*scope)[2].templateBinding.Profile = "account-b"
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, scenario.Step{Name: "ambiguous list", Run: "ecctl sandbox volume list --team-id team-1 --limit 1 --output=json"}, time.Second); ok || result.Status != report.StatusError || !strings.Contains(result.Error, "ambiguous") {
		t.Fatalf("multiple sources were silently selected: %+v", result)
	}
}

func TestVolumeTwoOwnedCreatesKeepDistinctDurableTargets(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "success-two")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	for i := 0; i < 2; i++ {
		data := map[string]any{}
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, data, step, time.Second); !ok {
			t.Fatalf("owned Volume %d: %+v", i+1, result)
		}
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 3 || entries[1].Teardown != "'ecctl' 'sandbox' 'volume' 'delete' 'vol-owned' '--region' 'cn-hangzhou' '--profile' 'account-a'" || entries[2].Teardown != "'ecctl' 'sandbox' 'volume' 'delete' 'vol-owned-2' '--region' 'cn-hangzhou' '--profile' 'account-a'" {
		t.Fatalf("two owned Volumes did not retain distinct delete targets: %+v", entries)
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err != nil {
		t.Fatalf("second reservation broke native ledger: %v", err)
	}
	if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Errors != 0 || replay.Deleted != 3 {
		t.Fatalf("restart did not retire two exact Volumes and their Team: result=%+v err=%v", replay, err)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy || len(journal.Entries) != 0 || len(journal.VolumeTerminalReceipts) != 2 {
		t.Fatalf("distinct Volume terminal receipts were not retained: %+v %v", journal, err)
	}
	seen := map[string]bool{}
	for _, receipt := range journal.VolumeTerminalReceipts {
		if receipt.TeamID != "team-1" || receipt.Region != "cn-hangzhou" || receipt.Profile != "account-a" || seen[receipt.VolumeID] {
			t.Fatalf("Volume receipt changed target or pinned selectors: %+v", receipt)
		}
		seen[receipt.VolumeID] = true
	}
	if !seen["vol-owned"] || !seen["vol-owned-2"] {
		t.Fatalf("two owned IDs did not retain distinct terminal receipts: %+v", seen)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-update:vol-owned-2") || strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 ||
		strings.Count(string(calls), "volume-delete:vol-owned-2\n") != 1 || strings.Count(string(calls), "team-delete\n") != 1 ||
		strings.Index(string(calls), "team-delete") < strings.LastIndex(string(calls), "volume-get") {
		t.Fatalf("restart lost distinct Volume targets or deleted Team before proof: %s", calls)
	}
}

func TestVolumeCreateJournalRecoversWaitFailureAndUnknownResponse(t *testing.T) {
	for _, mode := range []string{"wait-fail", "wait-fail-no-request", "read-transient"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			t.Setenv("FAKE_VOLUME_MODE", mode)
			data := map[string]any{}
			step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, data, step, time.Second)
			if ok || !strings.Contains(result.Error, "journaled one owned cleanup target") {
				t.Fatalf("uncertain create did not preserve cleanup: %+v", result)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "volume-create") != 1 || strings.Contains(string(calls), "volume-list") {
				t.Fatalf("non-idempotent Volume create replayed or failed to reconcile: %s", calls)
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 2 || entries[1].Teardown != "'ecctl' 'sandbox' 'volume' 'delete' 'vol-owned' '--region' 'cn-hangzhou' '--profile' 'account-a'" {
				t.Fatalf("exact Volume cleanup target missing: %+v", entries)
			}
			// A new replay process has no in-memory runner state. The accepted
			// Volume still needs durable positive observation before any delete.
			if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Errors != 1 || replay.Deleted != 0 {
				t.Fatalf("restart replay released Volume: result=%+v err=%v", replay, err)
			}
			calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
			if !strings.Contains(string(calls), "volume-get") || strings.Contains(string(calls), "volume-delete") || strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 2 {
				t.Fatalf("restart mutated an unobserved Volume or its Team: %s", calls)
			}
		})
	}
}

func TestVolumeRecoverySelectorMismatchKeepsPendingFence(t *testing.T) {
	cfg, cl, _, scope := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	intent, err := reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	if err := intent.withVolumeLaunchFence(context.Background(), func() { launches++ }); err != nil || launches != 1 {
		t.Fatalf("Volume test did not install the ordinary launch fence: launches=%d err=%v", launches, err)
	}
	response := execpkg.Result{Started: true, Exit: 1, JSON: map[string]any{
		"error":   map[string]any{"code": "WaitFailed", "current_state": "ERROR", "recovery_command": []any{"ecctl", "sandbox", "volume", "delete", "vol-owned", "--region", "cn-hangzhou", "--team-id", "team-other", "--profile", "account-a"}},
		"actions": []any{map[string]any{"action_name": "CreateVolume", "request_id": "req-create"}},
	}}
	if err := finishVolumeCreate(response, cl, scope, map[string]any{}, nil, time.Second, intent); err == nil || !strings.Contains(err.Error(), "selectors differ") {
		t.Fatalf("mismatched recovery gained cleanup authority: %v", err)
	}
	if len(readJournalEntries(t, cl.journal)) != 1 {
		t.Fatal("mismatched recovery registered a Volume finalizer")
	}
	var retained journalfile.TemplateCreateIntent
	body, err := os.ReadFile(intent.path)
	if err != nil || json.Unmarshal(body, &retained) != nil || retained.State != "pending" || retained.AcceptedVolumeID != "" {
		t.Fatalf("mismatched recovery altered pending ownership: %+v %v", retained, err)
	}
}

func TestVolumeReservationFailureSettlesUnstartedSidecar(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprint("installed=", installed), func(t *testing.T) {
			cfg, cl, dir, _ := volumeRecoveryFixture(t)
			previous := writeVolumeCreateReservationJournal
			t.Cleanup(func() { writeVolumeCreateReservationJournal = previous })
			writeVolumeCreateReservationJournal = func(root *os.Root, path string, journal report.CleanupJournal) error {
				if installed {
					if err := previous(root, path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected reservation durability failure")
			}
			step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if _, err := reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second); err == nil {
				t.Fatal("failed reservation was reported successful")
			}
			if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err != nil {
				t.Fatalf("unused Volume reservation still blocked cleanup: %v", err)
			}
			paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
			if err != nil || len(paths) != 1 {
				t.Fatalf("unstarted sidecar missing: %v %v", paths, err)
			}
			var intent journalfile.TemplateCreateIntent
			body, err := os.ReadFile(paths[0])
			if err != nil || json.Unmarshal(body, &intent) != nil || intent.State != "not_started" || intent.Started == nil || *intent.Started {
				t.Fatalf("unused sidecar was not settled: %+v %v", intent, err)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "volume-create") {
				t.Fatalf("reservation failure launched provider: %s", calls)
			}
		})
	}
}

func TestVolumeReservationInterruptionBeforeJournalLink(t *testing.T) {
	cfg, cl, _, _ := volumeRecoveryFixture(t)
	prior := readJournalEntries(t, cl.journal)
	previous := writeVolumeCreateReservationJournal
	t.Cleanup(func() { writeVolumeCreateReservationJournal = previous })
	writeVolumeCreateReservationJournal = func(*os.Root, string, report.CleanupJournal) error {
		panic("simulated process interruption before journal link")
	}
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("reservation did not reach journal link")
			}
		}()
		_, _ = reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second)
	}()
	writeVolumeCreateReservationJournal = previous
	if got := readJournalEntries(t, cl.journal); !reflect.DeepEqual(got, prior) {
		t.Fatalf("interrupted reservation changed prior cleanup: %+v", got)
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err != nil {
		t.Fatalf("unlinked never-started sidecar blocked cleanup: %v", err)
	}
	paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("interrupted sidecar missing: %v %v", paths, err)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.State != "not_started" || intent.Started == nil || *intent.Started {
		t.Fatalf("interrupted sidecar is blocking: %+v %v", intent, err)
	}
}

func TestVolumeAcceptedIDIsInFirstResultWrite(t *testing.T) {
	cfg, cl, _, scope := volumeRecoveryFixture(t)
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	intent, err := reserveVolumeCreate(context.Background(), step.Run, cfg, cl, step, time.Second, "volume-case")
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	if err := intent.withVolumeLaunchFence(context.Background(), func() { launches++ }); err != nil || launches != 1 {
		t.Fatalf("ordinary Volume launch fence was not installed: launches=%d err=%v", launches, err)
	}
	// Fail finalizer registration after the first result write.
	cl.requestedJournal = filepath.Join(t.TempDir(), "other-journal.json")
	response := execpkg.Result{Started: true, Exit: 0, Stdout: "accepted", JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": intent.Name, "team_id": intent.TeamID, "status": "AVAILABLE"}}}
	if err := finishVolumeCreate(response, cl, scope, map[string]any{}, nil, time.Second, intent); err == nil || !strings.Contains(err.Error(), "cleanup journal target changed") {
		t.Fatalf("injected finalizer failure was not reached: %v", err)
	}
	var retained journalfile.TemplateCreateIntent
	body, err := os.ReadFile(intent.path)
	if err != nil || json.Unmarshal(body, &retained) != nil || retained.AcceptedVolumeID != "vol-owned" || retained.CaseScope != "volume-case" || retained.StdoutSHA256 == "" || retained.State != "pending" {
		t.Fatalf("first durable result lost accepted ID: %+v %v", retained, err)
	}
	if len(readJournalEntries(t, cl.journal)) != 1 {
		t.Fatal("failed finalizer registration released the pending Volume fence")
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err == nil {
		t.Fatal("first accepted receipt made generic replay eligible before its finalizer")
	}
	intent.AcceptedVolumeID = "vol-other"
	if err := intent.save(); err == nil {
		t.Fatal("accepted receipt ID was overwritten before finalizer registration")
	}
	if after, err := os.ReadFile(intent.path); err != nil || !bytes.Equal(after, body) {
		t.Fatalf("rejected receipt rewrite changed durable bytes: %v", err)
	}
	intent.AcceptedVolumeID = retained.AcceptedVolumeID
	intent.State = "not_started"
	if err := intent.save(); err == nil {
		t.Fatal("accepted receipt was retired as an unstarted create")
	}
}

func TestVolumeNameOnlyCreateRecoveryKeepsPendingFence(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "unknown")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || !strings.Contains(result.Error, "without an invocation-linked accepted ID") {
		t.Fatalf("name-only recovery escaped pending fence: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-create") != 1 || strings.Count(string(calls), "volume-list") != 1 || strings.Contains(string(calls), "volume-delete") {
		t.Fatalf("name-only recovery replayed create or adopted same-name Volume: %s", calls)
	}
	if len(readJournalEntries(t, cl.journal)) != 1 || journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env) == nil {
		t.Fatal("name-only lookup released the pending Volume intent")
	}
}

func TestVolumeVerifiedConflictRetiresIntentWithoutAdoptingSameNameVolume(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "conflict")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
		t.Fatalf("provider-rejected create succeeded: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-create") != 1 || strings.Contains(string(calls), "volume-list") || strings.Contains(string(calls), "volume-delete") {
		t.Fatalf("verified conflict adopted or deleted a same-name Volume: %s", calls)
	}
	if len(readJournalEntries(t, cl.journal)) != 1 {
		t.Fatal("verified conflict registered a Volume finalizer")
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err != nil {
		t.Fatalf("durably rejected create remained pending: %v", err)
	}
}

func TestVolumeUnprovenConflictKeepsPendingFence(t *testing.T) {
	for _, mode := range []string{"generic-conflict", "unverified-conflict", "contradictory-conflict"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir, scope := volumeRecoveryFixture(t)
			t.Setenv("FAKE_VOLUME_MODE", mode)
			step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
			if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
				t.Fatalf("unproven conflict escaped pending fence: %+v", result)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "volume-create") != 1 || strings.Contains(string(calls), "volume-delete") || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Fatalf("unproven conflict adopted a Volume: %s", calls)
			}
			if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err == nil {
				t.Fatal("unproven conflict released the pending intent")
			}
		})
	}
}

func TestVolumeUnknownCreateFencesDuplicateAndRestartCleanup(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "pending")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	for attempt := 0; attempt < 2; attempt++ {
		if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || result.Error == "" {
			t.Fatalf("unknown create escaped fence: %+v", result)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-create") != 1 || strings.Contains(string(calls), "volume-delete") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("unknown create retried or deleted unproven resource: %s", calls)
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err == nil {
		t.Fatal("pending Volume ownership did not fence replay")
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err == nil {
		t.Fatal("restart replay ignored uncertain Volume allocation")
	}
}

func TestVolumeReconciliationRejectsForeignTeamResult(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	t.Setenv("FAKE_VOLUME_MODE", "foreign-list")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); ok || !strings.Contains(result.Error, "ownership mismatch") {
		t.Fatalf("foreign ListVolumes item became an owned delete: %+v", result)
	}
	if len(readJournalEntries(t, cl.journal)) != 1 {
		t.Fatal("foreign Volume was registered for cleanup")
	}
	if err := journalfile.CheckTemplateCreateIntents(cl.journal, report.CleanupJournal{}, cfg.Env); err == nil {
		t.Fatal("foreign reconciliation released pending intent")
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("foreign reconciliation triggered a destructive command: %s", calls)
	}
}

func TestVolumeFailedReplayKeepsOwnedTeam(t *testing.T) {
	independentDelete := func(script string) string {
		return strings.Replace(script, "echo '{}'\n", "if [[ \"$*\" == *\"ecs instance delete\"* ]]; then echo ecs-delete >> \"$FAKE_NATIVE_DIR/calls\"; echo '{\"deleted\":true}'; exit 0; fi\necho '{}'\n", 1)
	}
	cfg, cl, dir, scope := volumeRecoveryFixture(t, independentDelete)
	t.Setenv("FAKE_VOLUME_MODE", "success")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	if _, err := cl.pushItem(scope, "independent", "ecctl ecs instance delete i-independent --output=json", "primary", nil, false, false, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_VOLUME_DELETE_FAIL", "1")
	t.Setenv("FAKE_VOLUME_STATUS", "ERROR")
	replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	if err != nil || replay.Errors != 1 || replay.Deleted != 1 {
		t.Fatalf("failed replay result=%+v err=%v", replay, err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete") != 1 || strings.Count(string(calls), "ecs-delete\n") != 1 || strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("failed Volume delete blocked an independent finalizer or removed its Team: %s", calls)
	}
	restarted, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	if err != nil || restarted.Errors != 1 || restarted.Deleted != 0 {
		t.Fatalf("restart replay result=%+v err=%v", restarted, err)
	}
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete") != 1 || strings.Count(string(calls), "ecs-delete\n") != 1 || strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("restart retried an uncertain delete or lost a retained finalizer: %s", calls)
	}
}

func TestVolumeFailedInProcessCleanupRunsIndependentFinalizer(t *testing.T) {
	independentDelete := func(script string) string {
		return strings.Replace(script, "echo '{}'\n", "if [[ \"$*\" == *\"ecs instance delete\"* ]]; then echo '{\"deleted\":true}'; exit 0; fi\necho '{}'\n", 1)
	}
	cfg, cl, dir, scope := volumeRecoveryFixture(t, independentDelete)
	t.Setenv("FAKE_VOLUME_MODE", "success")
	step := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
	independent, err := cl.pushItem(scope, "independent", "ecctl ecs instance delete i-independent --output=json", "primary", nil, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_VOLUME_DELETE_FAIL", "1")
	t.Setenv("FAKE_VOLUME_STATUS", "ERROR")
	cl.keep = false
	failures := cl.runCleanup(*scope, false)
	if len(failures) == 0 || !independent.done || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("independent finalizer did not proceed: failures=%v done=%t", failures, independent.done)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "team-delete") {
		t.Fatalf("unresolved Volume released its Team: %s", calls)
	}
}
