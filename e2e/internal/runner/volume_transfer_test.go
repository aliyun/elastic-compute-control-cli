package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestVolumeUpdateCandidate(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"ecctl sandbox volume update vol-1 --team-id team-2", true},
		{"ecctl sbx volume update vol-1 --team-id team-2", true},
		{"ecctl sandbox volume create --name x --team-id team-1", false},
		{"ecctl sandbox volume delete vol-1 --team-id team-1", false},
		{"ecctl sandbox volume get vol-1 --team-id team-1", false},
		{"ecctl call fcsandbox UpdateVolume --request '{}'", false},
	} {
		if got := volumeUpdateCandidate(tc.command); got != tc.want {
			t.Errorf("volumeUpdateCandidate(%q) = %t, want %t", tc.command, got, tc.want)
		}
	}
}

func TestVolumeTransferAccepted(t *testing.T) {
	good := map[string]any{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true, "request_id": "req-update"}}}
	if !volumeTransferAccepted(good) {
		t.Fatal("verified UpdateVolume action was rejected")
	}
	for _, root := range []map[string]any{
		{},
		{"actions": []any{map[string]any{"action_name": "GetVolume", "provider_verified": true, "request_id": "req"}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": false, "request_id": "req"}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true}}},
		{"actions": []any{map[string]any{"action_name": "UpdateVolume", "provider_verified": true, "request_id": "req", "code": "Forbidden"}}},
	} {
		if volumeTransferAccepted(root) {
			t.Errorf("unproven UpdateVolume action was accepted: %+v", root)
		}
	}
}

func TestReserveVolumeTransferRequiresOwnedBinding(t *testing.T) {
	cl := &cleanup{}
	_, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1 --team-id team-2", execpkg.Config{Region: "cn-hangzhou", Bin: "/bin/ecctl"}, cl, scenario.Step{}, time.Second, "case")
	if err == nil {
		t.Fatal("transfer without an owned creation binding was accepted")
	}
	if _, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1", execpkg.Config{}, cl, scenario.Step{}, time.Second, "case"); err == nil {
		t.Fatal("transfer without a destination Team was accepted")
	}
	if _, err := reserveVolumeTransfer(context.Background(), "ecctl sandbox volume update vol-1 --team-id team-2", execpkg.Config{}, cl, scenario.Step{Teardown: "ecctl sandbox volume delete vol-1"}, time.Second, "case"); err == nil {
		t.Fatal("transfer registering a second finalizer was accepted")
	}
}

func TestRewriteVolumeIntentTeamRoot(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "cleanup.json")
	intent := journalfile.TemplateCreateIntent{
		Version: 2, Resource: "volume", State: "journaled", Name: "ecctl-p-0123456789abcdef01234567",
		RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou",
		Surface: "public", EcctlBin: "/bin/ecctl", Profile: "owner", TeamID: "team-source", AcceptedVolumeID: "vol-owned",
	}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(journal) + ".template-create-0123456789abcdef01234567.json"
	if err := os.WriteFile(filepath.Join(dir, name), append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	reanchored := intent
	reanchored.TeamID = "team-destination"
	if err := rewriteVolumeIntentTeamRoot(root, journal, reanchored); err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	installed, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var retained journalfile.TemplateCreateIntent
	if err := json.Unmarshal(installed, &retained); err != nil {
		t.Fatal(err)
	}
	if retained.TeamID != "team-destination" || retained.AcceptedVolumeID != "vol-owned" || retained.Name != intent.Name {
		t.Fatalf("rewrite changed retained fields: %+v", retained)
	}
	mismatch := reanchored
	mismatch.AcceptedVolumeID = "vol-other"
	if err := rewriteVolumeIntentTeamRoot(root, journal, mismatch); err == nil {
		t.Fatal("rewrite accepted a mismatched creation binding")
	}
}

// Team recovery pins the created identity through the team_id capture; every
// owned Team create in the shipped lifecycle must follow that convention.
func TestShippedVolumeTeamCreatesUseRecoveryTeardownConvention(t *testing.T) {
	suite, err := scenario.Load("../../cases/sandbox/volume-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	creates := 0
	for _, step := range suite.Steps {
		if !isTeamCreate(step.Run) {
			continue
		}
		creates++
		if step.Teardown != "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json" {
			t.Fatalf("owned Team create %q breaks the recovery teardown convention: teardown=%q", step.Name, step.Teardown)
		}
	}
	if creates != 2 {
		t.Fatalf("shipped Volume lifecycle owns %d Teams, want the source and destination Team", creates)
	}
}

// The sequential lifecycle path must admit the destination Team of a Volume
// transfer with a rotated ownership nonce, and still fence a third Team.
func TestVolumeSequentialCaseAdmitsDestinationTeam(t *testing.T) {
	for _, teams := range []int{2, 3} {
		t.Run(fmt.Sprintf("%d-team-creates", teams), func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
				t.Setenv(name, "")
			}
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "FAKE_NATIVE_DIR=" + dir}
			body, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			updated := strings.Replace(string(body), "echo '{}'\n", `if [[ "$*" == *"team create"* ]]; then
 name=''; prev=''
 for arg in "$@"; do
  [[ "$prev" == '--name' ]] && name="$arg"
  [[ "$arg" == --name=* ]] && name="${arg#--name=}"
  prev="$arg"
 done
 id="team-${name:8:8}"
 echo "team-create:$name" >> "$FAKE_NATIVE_DIR/calls"
 printf '%s' "$name" > "$FAKE_NATIVE_DIR/team-name-$id"
 date -u +%Y-%m-%dT%H:%M:%SZ > "$FAKE_NATIVE_DIR/team-created-at-$id"
 printf '{"team":{"id":"%s","name":"%s","description":"ecctl E2E Team owner=%s","status":"active"}}\n' "$id" "$name" "${name:8}"; exit 0
fi
if [[ "$*" == *"team get"* && "$*" == *"--agent-envelope=false"* ]]; then
 id=''; prev=''
 for arg in "$@"; do [[ "$prev" == get ]] && id="$arg"; prev="$arg"; done
 name="$(cat "$FAKE_NATIVE_DIR/team-name-$id" 2>/dev/null)"
 printf '{"team":{"id":"%s","name":"%s","description":"ecctl E2E Team owner=%s","created_time":"%s"}}\n' "$id" "$name" "${name:8}" "$(cat "$FAKE_NATIVE_DIR/team-created-at-$id")"; exit 0
fi
if [[ "$*" == *"template list"* ]]; then echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *"team list"* ]]; then
 teams=''; sep=''
 for f in "$FAKE_NATIVE_DIR"/team-name-*; do
  [[ -e "$f" ]] || continue
  id="${f##*team-name-}"; name="$(cat "$f")"
  teams="$teams$sep{\"id\":\"$id\",\"name\":\"$name\",\"description\":\"ecctl E2E Team owner=${name:8}\"}"; sep=','
 done
 printf '{"teams":[%s],"pagination":{"has_more":false}}\n' "$teams"; exit 0
fi
echo '{}'
`, 1)
			if updated == string(body) {
				t.Fatal("fake Team provider insertion point missing")
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
			data := map[string]any{"run_name": "volume-transfer"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			firstTeamName := data["team_name"].(string)
			createRun := `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}" --plan eco --output=json`
			suite := &scenario.Suite{Resource: "sandbox/volume", Serial: true, Steps: []scenario.Step{
				{Name: "create first owned Team", Run: createRun, At: "$.team", Capture: map[string]string{"first_team_id": "id"}, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json"},
				{Name: "create destination owned Team", Run: createRun, At: "$.team", Capture: map[string]string{"destination_team_id": "id"}, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json"},
			}}
			if teams == 3 {
				suite.Steps = append(suite.Steps, scenario.Step{Name: "create third owned Team", Run: createRun, At: "$.team", Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json"})
			}
			cr := report.Case{Name: "volume-lifecycle", Resource: suite.Resource, Status: report.StatusPass}
			opt := Options{RunName: "volume-transfer", StepTimeout: time.Second, Logf: func(string, ...any) {}}
			opt.volumeLifecycle = true
			opt.quotaLifecycleKnown = true
			runSequentialCase(context.Background(), opt, cfg, cl, cl.operations, &scope, data, suite, time.Second, &cr)
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			creates := 0
			for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				if strings.HasPrefix(line, "team-create:") {
					creates++
				}
			}
			if creates != 2 {
				t.Fatalf("sequential path launched %d Team creates, want 2: calls=%s case=%+v", creates, calls, cr)
			}
			if data["team_name"] == firstTeamName {
				t.Fatal("second Team create reused the first ownership nonce")
			}
			firstID, _ := data["first_team_id"].(string)
			destinationID, _ := data["destination_team_id"].(string)
			if firstID == "" || destinationID == "" || firstID == destinationID {
				t.Fatalf("owned Team captures lost distinct identities: first=%q destination=%q steps=%+v", firstID, destinationID, cr.Steps)
			}
			journal, _, err := readRunnerJournal(cl.journal)
			if err != nil || journal.VolumeTeamCreators[firstID] == nil || journal.VolumeTeamCreators[destinationID] == nil {
				t.Fatalf("journal lost an owned Team creator: journal=%+v err=%v", journal, err)
			}
			if teams == 2 {
				if cr.Status != report.StatusPass || len(cr.Steps) != 2 {
					t.Fatalf("destination Team was not admitted: case=%+v", cr)
				}
				return
			}
			if cr.Status != report.StatusFail || len(cr.Steps) != 3 || cr.Steps[2].Status != report.StatusError || !strings.Contains(cr.Steps[2].Error, "at most two owned Teams") {
				t.Fatalf("third Team create was not fenced before execution: case=%+v", cr)
			}
		})
	}
}
