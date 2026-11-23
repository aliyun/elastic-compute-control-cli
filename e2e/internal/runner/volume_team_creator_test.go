package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func TestShippedVolumeTeamStepsKeepOriginalSelectors(t *testing.T) {
	suite, err := scenario.Load("../../cases/sandbox/volume-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Steps) < 2 || suite.Steps[0].Name != "create first owned Team" {
		t.Fatal("shipped Volume lifecycle no longer starts with its owned Team")
	}
	if action, err := nativeVolumeMutationAction(suite.Steps[1].Run); err != nil || action != "create" {
		t.Fatalf("shipped step after Team is not a Volume create: action=%q err=%v", action, err)
	}
	for _, selector := range []string{"default", "symlink"} {
		// The first shipped step is the only Team create; the next creates a Volume.
		for stepIndex := range 1 {
			outcomes := []string{"success", "reconcile", "shared Team-only", "unresolved", "never-started", "definitive-rejection", "unproven-rejection", "reservation post-install", "late-cancel", "register-before", "register-after", "selector drift", "profile drift", "principal drift"}
			if selector == "symlink" {
				outcomes = append(outcomes, "target drift")
			}
			for _, outcome := range outcomes {
				t.Run(selector+"/"+suite.Steps[stepIndex].Name+"/"+outcome, func(t *testing.T) {
					cfg, cl, dir := nativeCleanupFixture(t, true)
					t.Setenv("HOME", dir)
					for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
						t.Setenv(name, "")
					}
					var native, aliyun string
					if selector == "default" {
						native = filepath.Join(dir, ".ecctl", "config.json")
						aliyun = filepath.Join(dir, ".aliyun", "config.json")
						for _, path := range []string{native, aliyun} {
							if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
								t.Fatal(err)
							}
						}
						t.Setenv("ECCTL_CONFIG_PATH", "")
						t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", "")
						cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=", "ECCTL_ALIYUN_CONFIG_PATH=", "FAKE_NATIVE_DIR=" + dir}
					} else {
						native = filepath.Join(dir, "native.json")
						aliyun = filepath.Join(dir, "aliyun.json")
						nativeLink := filepath.Join(dir, "native-link.json")
						aliyunLink := filepath.Join(dir, "aliyun-link.json")
						for _, pair := range [][2]string{{native, nativeLink}, {aliyun, aliyunLink}} {
							if err := os.Symlink(pair[0], pair[1]); err != nil {
								t.Fatal(err)
							}
						}
						t.Setenv("ECCTL_CONFIG_PATH", nativeLink)
						t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyunLink)
						cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + nativeLink, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyunLink, "FAKE_NATIVE_DIR=" + dir}
					}
					if selector == "symlink" {
						if err := os.WriteFile(native, []byte(`{}`), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
						t.Fatal(err)
					}
					cl.execCfg["primary"] = cfg
					body, err := os.ReadFile(cfg.Bin)
					if err != nil {
						t.Fatal(err)
					}
					updated := strings.Replace(string(body), "echo '{}'\n", `if [[ "$*" == *"sandbox team get"* && "$*" == *"--agent-envelope=false"* ]]; then
 if [[ ! -e "$FAKE_NATIVE_DIR/team-created-at" ]]; then date -u +%Y-%m-%dT%H:%M:%SZ > "$FAKE_NATIVE_DIR/team-created-at"; fi
 printf '{"team":{"id":"team-owned","name":"%s","description":"%s","created_time":"%s"}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER" "$(cat "$FAKE_NATIVE_DIR/team-created-at")"; exit 0
fi
if [[ "$*" == *"sandbox template list"* ]]; then echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *"sandbox team list"* ]]; then
 if [[ "$FAKE_TEAM_LIST_EMPTY" == 1 ]]; then echo '{"teams":[],"pagination":{"has_more":false}}'; exit 0; fi
 printf '{"teams":[{"id":"team-owned","name":"%s","description":"%s"}],"pagination":{"has_more":false}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER"; exit 0
fi
if [[ "$*" == *"call oss ListObjects"* ]]; then
 if [[ -e "$FAKE_NATIVE_DIR/bucket-deleted" ]]; then echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4; fi
 echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}'; exit 0
fi
if [[ "$*" == *"call oss GetBucketInfo"* ]]; then echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}'; exit 0; fi
if [[ "$*" == *"call oss DeleteBucket"* ]]; then
 echo DeleteBucket >> "$FAKE_NATIVE_DIR/calls"
 if [[ -e "$FAKE_NATIVE_DIR/bucket-deleted" ]]; then exit 2; fi
 touch "$FAKE_NATIVE_DIR/bucket-deleted"
 echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}'; exit 0
fi
echo '{}'
`, 1)
					if updated == string(body) {
						t.Fatal("fake Team list insertion point missing")
					}
					if err := os.WriteFile(cfg.Bin, []byte(updated), 0o755); err != nil {
						t.Fatal(err)
					}
					digest, err := journalfile.ExecutableSHA256(cfg.Bin)
					if err != nil {
						t.Fatal(err)
					}
					cl.journalMeta.EcctlBinSHA256 = digest
					cfg.ExpectedBinSHA256 = digest
					cl.execCfg["primary"] = cfg
					bucket := "ecctl-e2e-20261104t120000-123456abcdef"
					if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
						t.Fatal(err)
					}
					var scope []*cleanupItem
					bucketItem, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
					if err != nil {
						t.Fatal(err)
					}
					if err := cl.settleVolumeFixture(bucketItem); err != nil {
						t.Fatal(err)
					}
					if err := cl.recordVolumeFixtureBucketEvidence(bucketItem, "2026-11-04T12:00:00Z"); err != nil {
						t.Fatal(err)
					}
					data := map[string]any{}
					if err := teamOwnership(data); err != nil {
						t.Fatal(err)
					}
					t.Setenv("FAKE_TEAM_NAME", data["team_name"].(string))
					t.Setenv("FAKE_TEAM_OWNER", data["team_owner"].(string))
					if outcome == "unresolved" || outcome == "unproven-rejection" {
						t.Setenv("FAKE_TEAM_LIST_EMPTY", "1")
					}
					calls := 0
					cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
						calls++
						if outcome != "shared Team-only" {
							body, readErr := os.ReadFile(cl.journal + ".team-create-" + data["team_name"].(string)[8:] + ".json")
							var dispatched teamCreateIntent
							if readErr != nil || json.Unmarshal(body, &dispatched) != nil || !dispatched.Dispatched || dispatched.DispatchProtocol != 1 || dispatched.AcceptedID != "" {
								t.Errorf("CreateTeam reached provider without its durable dispatch marker: intent=%+v err=%v", dispatched, readErr)
							}
							if journal, _, err := readRunnerJournal(cl.journal); err != nil {
								t.Errorf("Team create lost its pending journal: %v", err)
							} else if id, exists := journal.VolumeTeamCreateIDs[data["team_name"].(string)]; !exists || id != "" {
								t.Errorf("Team create reached provider without pending ownership: %+v", journal.VolumeTeamCreateIDs)
							}
							if bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.UserID != "user-a" {
								t.Errorf("Team create lost bound principal: %+v", bound)
							}
						}
						if outcome == "reconcile" || outcome == "unresolved" {
							return execpkg.Result{Command: command, Started: true, Exit: 1, Err: errors.New("unknown Team create result")}
						}
						if outcome == "never-started" || outcome == "late-cancel" {
							failure := errors.New("injected Start failure")
							if outcome == "late-cancel" {
								failure = context.Canceled
							}
							return execpkg.Result{Command: command, Started: false, Exit: -1, Err: failure}
						}
						if outcome == "definitive-rejection" || outcome == "unproven-rejection" {
							body, readErr := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "aliyun", "testdata", "team-create-rejections.json"))
							var fixtures map[string]json.RawMessage
							if readErr != nil || json.Unmarshal(body, &fixtures) != nil {
								t.Fatalf("product CreateTeam rejection contract is unavailable: %v", readErr)
							}
							name := "proven"
							if outcome == "unproven-rejection" {
								name = "unproven"
							}
							var rejection map[string]any
							if err := json.Unmarshal(fixtures[name], &rejection); err != nil {
								t.Fatal(err)
							}
							return execpkg.Result{Command: command, Started: true, Exit: 2, JSON: rejection}
						}
						return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
					}
					switch outcome {
					case "selector drift":
						cfg.Env = append(cfg.Env, "ECCTL_ALIYUN_CONFIG_PATH="+filepath.Join(dir, "changed.json"))
					case "profile drift":
						response := filepath.Join(dir, "other-profile.json")
						if err := os.WriteFile(response, []byte(`{"profile":"account-b","mode":"AK"}`), 0o600); err != nil {
							t.Fatal(err)
						}
						t.Setenv("FAKE_CONFIG_RESPONSE", response)
					case "principal drift":
						t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
					case "target drift":
						other := filepath.Join(dir, "other-target.json")
						if err := os.WriteFile(other, []byte(`{}`), 0o600); err != nil {
							t.Fatal(err)
						}
						link := filepath.Join(dir, "aliyun-link.json")
						if err := os.Remove(link); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(other, link); err != nil {
							t.Fatal(err)
						}
					}
					previousRegistration := writeTeamRegistrationJournal
					if outcome == "reservation post-install" {
						previous := writeTeamCreateJournal
						t.Cleanup(func() { writeTeamCreateJournal = previous })
						writeTeamCreateJournal = func(path string, journal report.CleanupJournal) error {
							if id, exists := journal.VolumeTeamCreateIDs[data["team_name"].(string)]; exists && id == "" {
								if err := previous(path, journal); err != nil {
									return err
								}
								return errors.New("injected post-install Team reservation failure")
							}
							return previous(path, journal)
						}
					}
					if strings.HasPrefix(outcome, "register-") {
						t.Cleanup(func() { writeTeamRegistrationJournal = previousRegistration })
						writeTeamRegistrationJournal = func(path string, journal report.CleanupJournal) error {
							if outcome == "register-after" {
								if err := previousRegistration(path, journal); err != nil {
									return err
								}
							}
							return errors.New("injected reconciliation registration failure")
						}
					}
					step, ok := runStep(context.Background(), Options{volumeLifecycle: outcome != "shared Team-only", quotaLifecycleKnown: true, Logf: func(string, ...any) {}}, cfg, cl, &scope, data, suite.Steps[stepIndex], time.Second)
					if outcome == "shared Team-only" {
						journal, _, err := readRunnerJournal(cl.journal)
						if !ok || step.Status != report.StatusPass || calls != 1 || data["first_team_id"] != "team-owned" || len(scope) != 2 || !scope[1].teamCreateConfirmed || scope[1].volumeTeamBound || err != nil || len(journal.Entries) != 2 || journal.Entries[1].VolumeTeamBound || len(journal.VolumeTeamCreators) != 0 || len(journal.VolumeTeamCreateIDs) != 0 {
							t.Fatalf("shared Team-only step gained Volume creator ownership: step=%+v calls=%d journal=%+v err=%v", step, calls, journal, err)
						}
						return
					}
					if outcome == "reservation post-install" {
						journal, _, err := readRunnerJournal(cl.journal)
						_, exists := journal.VolumeTeamCreateIDs[data["team_name"].(string)]
						if ok || calls != 0 || err != nil || exists || len(scope) != 1 || len(journal.Entries) != 1 || journalfile.VolumeFixtureCleanupReady(cl.journal, journal, journal.Entries[0], cfg.Env) != nil {
							t.Fatalf("unused Team reservation was not retired: step=%+v calls=%d journal=%+v err=%v", step, calls, journal, err)
						}
						return
					}
					if strings.HasSuffix(outcome, "drift") {
						want := map[string]string{"selector drift": "selector changed", "target drift": "target changed", "profile drift": "selected profile", "principal drift": "credential principal"}[outcome]
						if ok || calls != 0 || !strings.Contains(step.Error, want) || len(scope) != 1 {
							t.Fatalf("changed %s reached Team create: step=%+v calls=%d scope=%+v", outcome, step, calls, scope)
						}
						return
					}
					if outcome == "never-started" || outcome == "late-cancel" || outcome == "definitive-rejection" {
						journal, _, err := readRunnerJournal(cl.journal)
						_, exists := journal.VolumeTeamCreateIDs[data["team_name"].(string)]
						wantStatus := report.StatusError
						if outcome == "definitive-rejection" {
							wantStatus = report.StatusFail
						}
						if ok || step.Status != wantStatus || calls != 1 || len(scope) != 1 || err != nil || exists || !strings.Contains(step.Error, map[string]string{"never-started": "injected Start failure", "late-cancel": "context canceled", "definitive-rejection": `"provider_verified": true`}[outcome]) {
							t.Fatalf("non-allocating Team left a pending reservation: step=%+v calls=%d journal=%+v err=%v", step, calls, journal, err)
						}
						if err := journalfile.VolumeFixtureCleanupReady(cl.journal, journal, journal.Entries[0], cfg.Env); err != nil {
							t.Fatalf("retired Team still blocks ordinary fixture cleanup: %v", err)
						}
						bucketCalls := []string{}
						deleted := false
						cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
							if bound.Bin != cfg.Bin || bound.Region != "cn-hangzhou" || bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.AccountID != "1234567890123456" || bound.ExpectedIdentity.UserID != "user-a" {
								t.Errorf("bucket cleanup lost its original principal: %+v", bound)
							}
							switch {
							case strings.Contains(command, "call oss ListObjects"):
								bucketCalls = append(bucketCalls, "ListObjects")
								if deleted {
									return execpkg.Result{Command: command, Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound", "message": bucket + " not found"}, "actions": []any{map[string]any{"action_name": "ListObjects", "code": "NoSuchBucket", "provider_verified": true}}}}
								}
								return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "ListObjects", "response": map[string]any{"Name": bucket, "IsTruncated": false, "KeyCount": "0"}}}
							case strings.Contains(command, "call oss GetBucketInfo"):
								bucketCalls = append(bucketCalls, "GetBucketInfo")
								if deleted {
									return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("bucket was already deleted")}
								}
								return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "GetBucketInfo", "region": "cn-hangzhou", "response": map[string]any{"Bucket": map[string]any{"Name": bucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com", "CreationDate": "2026-11-04T12:00:00Z", "Owner": map[string]any{"ID": "1234567890123456"}}}}}
							case strings.Contains(command, "call oss DeleteBucket"):
								bucketCalls = append(bucketCalls, "DeleteBucket")
								if deleted {
									return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("duplicate bucket delete")}
								}
								if err := os.WriteFile(filepath.Join(dir, "bucket-deleted"), nil, 0o600); err != nil {
									return execpkg.Result{Command: command, Started: false, Exit: -1, Err: err}
								}
								deleted = true
								return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "DeleteBucket", "region": "cn-hangzhou", "response": map[string]any{}}}
							default:
								t.Errorf("unexpected cleanup command %s", command)
								return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("unexpected cleanup command")}
							}
						}
						cl.keep = false
						grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
						if failures := cl.runCleanup(scope, false); len(failures) != 0 {
							t.Fatalf("retired Team blocked ordinary cleanup: %v", failures)
						}
						if _, err := journalfile.VolumeFixtureBucketAbsent(context.Background(), journalfile.BoundQuotaConfig(journal.VolumeFixtureBinding, cfg), journal.Entries[0], journal, cl.runCommand); err != nil {
							t.Fatalf("retired Team bucket absence is unproven: %v", err)
						}
						if !deleted || strings.Join(bucketCalls, "/") != "ListObjects/GetBucketInfo/DeleteBucket/ListObjects/ListObjects" {
							t.Fatalf("retired Team bucket cleanup skipped ownership or repeated delete: %v", bucketCalls)
						}
						replayed, replayErr := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
						if replayErr != nil || replayed.Errors != 0 || replayed.Deleted != 0 {
							t.Fatalf("retired Team blocked empty replay: %+v %v", replayed, replayErr)
						}
						return
					}
					if strings.HasPrefix(outcome, "register-") {
						if ok || len(scope) != 2 || !scope[1].teamRegistrationFailed || !strings.Contains(step.Error, "injected reconciliation registration failure") {
							t.Fatalf("failed reconciliation registration lost Team: step=%+v scope=%+v", step, scope)
						}
						writeTeamRegistrationJournal = previousRegistration
						if err := cl.repairTeamRegistration(scope[1]); err != nil {
							t.Fatal(err)
						}
						journal, _, err := readRunnerJournal(cl.journal)
						if err != nil || len(journal.Entries) != 2 || journal.VolumeTeamCreateIDs[data["team_name"].(string)] != "team-owned" {
							t.Fatalf("reconciled Team name remained pending: %+v %v", journal, err)
						}
						return
					}
					if outcome == "unresolved" || outcome == "reconcile" || outcome == "unproven-rejection" {
						journal, _, err := readRunnerJournal(cl.journal)
						pendingID, exists := journal.VolumeTeamCreateIDs[data["team_name"].(string)]
						want := "absence is unproven"
						if outcome == "reconcile" {
							want = "diagnostic without a durable invocation-linked ID"
						}
						if ok || calls != 1 || len(scope) != 1 || err != nil || !exists || pendingID != "" || !strings.Contains(step.Error, want) {
							t.Fatalf("unresolved Team lost its pending ownership: step=%+v calls=%d scope=%+v journal=%+v err=%v", step, calls, scope, journal, err)
						}
						if outcome == "unproven-rejection" {
							body, readErr := os.ReadFile(cl.journal + ".team-create-" + data["team_name"].(string)[8:] + ".json")
							var pending teamCreateIntent
							if readErr != nil || json.Unmarshal(body, &pending) != nil || !pending.Dispatched || pending.AcceptedID != "" {
								t.Fatalf("unproven product rejection lost its dispatched recovery intent: %+v %v", pending, readErr)
							}
						}
						cl.keep = false
						if failures := cl.runCleanup(scope, false); len(failures) == 0 {
							t.Fatal("ordinary cleanup removed bucket after unresolved Team create")
						}
						if calls != 1 || len(readJournalEntries(t, cl.journal)) != 1 {
							t.Fatalf("ordinary cleanup reached provider or consumed bucket: calls=%d", calls)
						}
						result, replayErr := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
						if result != nil && result.Deleted != 0 || replayErr == nil && (result == nil || result.Errors == 0) || len(readJournalEntries(t, cl.journal)) != 1 {
							t.Fatalf("replay accepted unresolved Team create: %+v", result)
						}
						providerCalls, _ := os.ReadFile(filepath.Join(dir, "calls"))
						if strings.Contains(string(providerCalls), "DeleteBucket") {
							t.Fatalf("bucket delete reached provider after unresolved Team create: %s", providerCalls)
						}
						return
					}
					var target string
					var targetErr error
					if len(scope) == 2 {
						target, targetErr = journalfile.TeamDeleteTarget(scope[1].cmd)
					}
					if calls != 1 || len(scope) != 2 || !scope[1].teamCreateConfirmed || targetErr != nil || target != "team-owned" {
						t.Fatalf("shipped Team step did not retain its exact finalizer: step=%+v calls=%d scope=%+v", step, calls, scope)
					}
					journal, _, err := readRunnerJournal(cl.journal)
					if err != nil || len(journal.Entries) != 2 || journal.Entries[1].Teardown != scope[1].cmd || journal.VolumeTeamCreators["team-owned"] == nil || journal.VolumeTeamCreateIDs[data["team_name"].(string)] != "team-owned" {
						t.Fatalf("shipped Team ownership was not durable: journal=%+v err=%v", journal, err)
					}
					if _, err := os.Stat(cl.journal + ".team-create-" + data["team_name"].(string)[8:] + ".json"); err != nil {
						t.Fatalf("shipped Team step did not reserve recovery intent: %v", err)
					}
					if outcome == "success" {
						if err := journalfile.CheckVolumeTeamCreationLink(journal, "team-owned", &journal.Entries[1]); err != nil {
							t.Fatalf("authored Team did not establish a durable Volume creator: %v", err)
						}
					}
					if outcome == "success" && selector == "default" {
						backing := journal
						backing.Entries = nil
						oss, mount, err := appendSyntheticVolumeBacking(&backing)
						if err != nil {
							t.Fatal(err)
						}
						entries := make([]report.Resource, 0, len(journal.Entries)+3)
						entries = append(entries, journal.Entries[0])
						entries = append(entries, backing.Entries[1:]...)
						journal.Entries = append(entries, journal.Entries[1:]...)
						if err := writeRunnerJournal(cl.journal, journal); err != nil {
							t.Fatal(err)
						}
						cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
							argv, parseErr := shlex.Split(command)
							hasSelector := func(flag, value string) bool {
								for i, arg := range argv {
									if arg == flag+"="+value || arg == flag && i+1 < len(argv) && argv[i+1] == value {
										return true
									}
								}
								return false
							}
							if parseErr != nil || !volumeCreateCandidate(command) || !hasSelector("--team-id", "team-owned") || !hasSelector("--region", "cn-hangzhou") || !hasSelector("--profile", "account-a") ||
								bound.Region != "cn-hangzhou" || bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.UserID != "user-a" ||
								journalfile.TemplateEnvironmentValue("HOME", bound.Env) != dir || journalfile.TemplateEnvironmentValue("ECCTL_CONFIG_PATH", bound.Env) != native || journalfile.TemplateEnvironmentValue("ECCTL_ALIYUN_CONFIG_PATH", bound.Env) != aliyun {
								t.Errorf("first Volume create lost its Team, selectors or creator identity: argv=%q region=%q profile=%q identity=%+v parse=%v", argv, bound.Region, bound.Profile, bound.ExpectedIdentity, parseErr)
								return execpkg.Result{Command: command, Exit: -1, Err: errors.New("unexpected Volume create")}
							}
							name, _ := data["volume_name"].(string)
							return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": name, "team_id": "team-owned", "status": "AVAILABLE"}}}
						}
						volume := scenario.Step{Name: "first owned Volume", Run: "ecctl sandbox volume create --name authored --team-id team-owned --oss-volume-config '" + oss + "' --mount-config '" + mount + "' --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
						created, createdOK := runStep(context.Background(), Options{volumeLifecycle: true, quotaLifecycleKnown: true, Logf: func(string, ...any) {}}, cfg, cl, &scope, data, volume, time.Second)
						if !createdOK || created.Status != report.StatusPass || len(scope) != 3 || scope[2].templateBinding == nil || scope[2].templateBinding.Resource != "volume" {
							t.Fatalf("authored Team could not register its first Volume cleanup: step=%+v scope=%+v", created, scope)
						}
						journal, _, err = readRunnerJournal(cl.journal)
						if err != nil || len(journal.Entries) != 6 || journal.Entries[5].Teardown != scope[2].cmd {
							t.Fatalf("first Volume finalizer was not retained: journal=%+v err=%v", journal, err)
						}
						if err := journalfile.CheckVolumeTeamCreationLink(journal, "team-owned", &journal.Entries[4]); err != nil {
							t.Fatalf("first Volume changed its Team creator link: %v", err)
						}
						deleted := false
						cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
							if bound.Region != "cn-hangzhou" || bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.UserID != "user-a" {
								t.Errorf("Volume delete lost its original creator identity: %s %+v", command, bound)
								return execpkg.Result{Command: command, Exit: -1, Err: errors.New("unbound Volume delete")}
							}
							action, parseErr := commandAction(command)
							if parseErr != nil || len(action) < 3 || action[0] != "sandbox" || action[1] != "volume" {
								t.Errorf("unexpected Volume cleanup command: %s", command)
								return execpkg.Result{Command: command, Exit: -1, Err: errors.New("unexpected Volume cleanup command")}
							}
							switch action[2] {
							case "get":
								if deleted {
									return execpkg.Result{Command: command, Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetVolume", "code": "VolumeNotFound", "request_id": "req-absent", "provider_verified": true}}}}
								}
								return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": data["volume_name"], "team_id": "team-owned", "status": "AVAILABLE"}}}
							case "delete":
								deleted = true
								return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"absent_in_scope": true}}
							default:
								t.Errorf("unexpected Volume cleanup action: %s", command)
								return execpkg.Result{Command: command, Exit: -1, Err: errors.New("unexpected Volume cleanup action")}
							}
						}
						remove := scenario.Step{Name: "delete first owned Volume", Run: "ecctl sandbox volume delete vol-owned --team-id team-owned --timeout 5m --output=json"}
						removed, removedOK := runStep(context.Background(), Options{volumeLifecycle: true, quotaLifecycleKnown: true, Logf: func(string, ...any) {}}, cfg, cl, &scope, data, remove, time.Second)
						if !removedOK || removed.Status != report.StatusPass || !deleted || !scope[2].done {
							t.Fatalf("first Volume cleanup did not reach strict scoped absence: step=%+v scope=%+v", removed, scope)
						}
						journal, _, err = readRunnerJournal(cl.journal)
						if err != nil || len(journal.Entries) != 5 || len(journal.VolumeTerminalReceipts) != 1 {
							t.Fatalf("first Volume cleanup lost its terminal receipt: journal=%+v err=%v", journal, err)
						}
						cl.keep = false
						cl.runCommand = execpkg.Run
						if failures := cl.runCleanup([]*cleanupItem{scope[1]}, false); len(failures) != 0 || !scope[1].done {
							t.Fatalf("owned Team cleanup was blocked after Volume retirement: failures=%v Team=%+v", failures, scope[1])
						}
						journal, _, err = readRunnerJournal(cl.journal)
						if err != nil || len(journal.Entries) != 4 || journalfile.CheckVolumeTeamCreationLink(journal, "team-owned", nil) != nil {
							t.Fatalf("owned Team cleanup lost its accepted creator: journal=%+v err=%v", journal, err)
						}
					}
					if outcome == "success" {
						if !ok || step.Status != report.StatusPass || data["first_team_id"] != "team-owned" {
							t.Fatalf("shipped Team success lost capture: step=%+v data=%+v", step, data)
						}
					} else if ok || !strings.Contains(step.Error, "journaled owned Team for cleanup") {
						t.Fatalf("shipped Team reconciliation did not preserve failed create: %+v", step)
					}
				})
			}
		}
	}
}

func TestVolumeTeamCreatorSurvivesInterruptionBeforeFirstVolume(t *testing.T) {
	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	for _, mode := range []string{"normal", "replay"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, false)
			aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"},{"name":"account-b","mode":"AK","access_key_id":"fake-b","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "FAKE_NATIVE_DIR=" + dir}
			cl.execCfg["primary"] = cfg
			body, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			old := `if [[ "$*" == *"team delete"* ]]; then echo team-delete >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi`
			replacement := `if [[ "$*" == *"team create"* ]]; then
 echo "team-create profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 date -u +%Y-%m-%dT%H:%M:%SZ > "$FAKE_NATIVE_DIR/team-created-at"
 printf '{"team":{"id":"team-1","name":"%s","description":"%s","status":"active"}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER"; exit 0
fi
if [[ "$*" == *"team get"* && "$*" == *"--agent-envelope=false"* ]]; then
 printf '{"team":{"id":"team-1","name":"%s","description":"%s","created_time":"%s"}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER" "$(cat "$FAKE_NATIVE_DIR/team-created-at")"; exit 0
fi
if [[ "$*" == *"template list"* ]]; then echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *"team list"* ]]; then
 printf '{"teams":[{"id":"team-1","name":"%s","description":"%s"}],"pagination":{"has_more":false}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER"; exit 0
fi
if [[ "$*" == *"team delete"* ]]; then echo "team-delete profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi
if [[ "$*" == *"call oss ListObjects"* ]]; then
 if [[ -e "$FAKE_NATIVE_DIR/bucket-deleted" ]]; then echo "bucket-list absent profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"; echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4; fi
 echo "bucket-list present profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}'; exit 0
fi
if [[ "$*" == *"call oss GetBucketInfo"* ]]; then
 echo "bucket-info profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 if [[ -e "$FAKE_NATIVE_DIR/bucket-deleted" ]]; then exit 2; fi
 echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}'; exit 0
fi
if [[ "$*" == *"call oss DeleteBucket"* ]]; then
 echo "bucket-delete profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 if [[ -e "$FAKE_NATIVE_DIR/bucket-deleted" ]]; then exit 2; fi
 touch "$FAKE_NATIVE_DIR/bucket-deleted"
 echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}'; exit 0
fi`
			updated := strings.Replace(string(body), old, replacement, 1)
			if updated == string(body) {
				t.Fatal("fake Team provider insertion point missing")
			}
			if err := os.WriteFile(cfg.Bin, []byte(issue128HumanOutput(updated, "team create", "team list", "template list")), 0o755); err != nil {
				t.Fatal(err)
			}
			digest, err := journalfile.ExecutableSHA256(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			cl.journalMeta.EcctlBinSHA256 = digest
			cfg.ExpectedBinSHA256 = digest
			cl.execCfg["primary"] = cfg
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			bucketItem, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
			if err != nil {
				t.Fatal(err)
			}
			if err := cl.settleVolumeFixture(bucketItem); err != nil {
				t.Fatal(err)
			}
			if err := cl.recordVolumeFixtureBucketEvidence(bucketItem, "2026-11-04T12:00:00Z"); err != nil {
				t.Fatal(err)
			}
			data := map[string]any{}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_TEAM_NAME", data["team_name"].(string))
			t.Setenv("FAKE_TEAM_OWNER", data["team_owner"].(string))
			step := scenario.Step{Name: "create Team before first Volume", Run: `ecctl sandbox team create --name ` + data["team_name"].(string) + ` --description "` + data["team_owner"].(string) + `" --plan eco --output=json`, Teardown: `ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json`}
			result, ok := runStep(context.Background(), Options{volumeLifecycle: true, quotaLifecycleKnown: true, Logf: func(string, ...any) {}}, cfg, cl, &scope, data, step, time.Second)
			if !ok || result.Status != report.StatusPass || len(scope) != 2 || !json.Valid([]byte(result.Stdout)) || !strings.Contains(result.Command, " --no-color=true") {
				t.Fatalf("Team creation was not retained before first Volume: result=%+v scope=%+v", result, scope)
			}
			journal, _, err := readRunnerJournal(cl.journal)
			if err != nil || len(journal.Entries) != 2 || !journal.Entries[1].VolumeTeamBound || journal.VolumeTeamCreators["team-1"] == nil || journal.VolumeTeamCreators["team-1"].Profile != "account-a" {
				t.Fatalf("Team creator was not durable before first Volume: journal=%+v err=%v", journal, err)
			}

			if err := journalfile.CheckVolumeTeamCreationLink(journal, "team-1", &journal.Entries[1]); err != nil {
				t.Fatalf("durable Team finalizer was not linked to the accepted create: %v", err)
			}
			// The process ends here, before it can create or journal a Volume.
			if err := os.WriteFile(filepath.Join(dir, "current"), []byte("account-b"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
			if mode == "normal" {
				if failures := cl.runCleanup([]*cleanupItem{scope[1]}, false); len(failures) == 0 {
					t.Fatal("normal cleanup accepted a different principal")
				}
			} else {
				result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
				if err == nil && result.Errors == 0 {
					t.Fatalf("replay accepted a different principal: result=%+v", result)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "team-delete") || strings.Contains(string(calls), "bucket-delete") {
				t.Fatalf("Team or bucket delete reached a different principal: %s", calls)
			}
			t.Setenv("FAKE_PRINCIPAL_USER", "user-a")
			if err := cl.withTemplateCleanupGuard(context.Background(), "primary", func(bound execpkg.Config) error {
				if bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.UserID != "user-a" {
					t.Errorf("explicit Team cleanup used another credential: %+v", bound)
				}
				return nil
			}, scope[1].cmd); err != nil {
				t.Fatalf("explicit Team cleanup guard rejected original credential: %v", err)
			}
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
			if mode == "normal" {
				if failures := cl.runCleanup(scope, false); len(failures) != 0 {
					t.Fatalf("original principal could not clean Team and bucket: %v", failures)
				}
			} else {
				result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
				if result == nil || result.Deleted != 1 || result.Errors == 0 {
					t.Fatalf("replay did not clean Team and fence ungranted bucket: result=%+v err=%v", result, err)
				}
				remaining := readJournalEntries(t, cl.journal)
				if len(remaining) != 1 || remaining[0].VolumeFixtureKind != "bucket" {
					t.Fatalf("replay consumed an ungranted bucket: %+v", remaining)
				}
				beforeScope, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil || strings.Contains(string(beforeScope), "bucket-delete") {
					t.Fatalf("ungranted replay reached bucket delete: calls=%q err=%v", beforeScope, err)
				}
				if failures := cl.runCleanup([]*cleanupItem{scope[0]}, false); len(failures) != 0 {
					t.Fatalf("original principal could not clean scoped fake bucket: %v", failures)
				}
			}
			if _, err := journalfile.VolumeFixtureBucketAbsent(context.Background(), journalfile.BoundQuotaConfig(journal.VolumeFixtureBinding, cfg), journal.Entries[0], journal, execpkg.Run); err != nil {
				t.Fatalf("original principal could not prove bucket absence: %v", err)
			}
			calls, err = os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || !strings.Contains(string(calls), "team-delete profile=account-a flag=account-a") {
				t.Fatalf("original Team profile was not used: %q err=%v", calls, err)
			}
			wantCalls := []string{
				"team-create profile=account-a flag=account-a",
				"team-delete profile=account-a flag=account-a",
			}
			if mode == "replay" {
				wantCalls = append(wantCalls, "bucket-list present profile=account-a flag=account-a")
			}
			wantCalls = append(wantCalls,
				"bucket-list present profile=account-a flag=account-a",
				"bucket-info profile=account-a flag=account-a",
				"bucket-delete profile=account-a flag=account-a",
				"bucket-list absent profile=account-a flag=account-a",
				"bucket-list absent profile=account-a flag=account-a",
			)
			if got, want := string(calls), strings.Join(wantCalls, "\n")+"\n"; got != want {
				t.Fatalf("bucket cleanup lost its original profile or operation order: calls=%q want=%q", got, want)
			}
		})
	}
}
