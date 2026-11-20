package runner

import (
	"context"
	"encoding/json"
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

func TestJournaledNoWaitVolumeReconcilePersistsPositiveObservation(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "account-a")
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, _, err := captureQuotaCredentialBinding(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	t.Setenv("FAKE_VOLUME_RECONCILE_BUCKET", bucket)
	cl.volumeFixtureBinding, cl.volumeFixtureBucket = binding, bucket
	journal, _, err := readRunnerJournal(cl.journal)
	if err != nil || len(journal.Entries) != 1 {
		t.Fatalf("Volume Team fixture is unavailable: %v %+v", err, journal)
	}
	journal.VolumeFixtureBinding, journal.VolumeFixtureBucket = binding, bucket
	settleVolumeTeamFixture(t, cl, &journal, binding)
	oss, mount, err := appendSyntheticVolumeBacking(&journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerJournal(cl.journal, journal); err != nil {
		t.Fatal(err)
	}
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --oss-volume-config '" + oss + "' --mount-config '" + mount + "' --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second); !ok {
		t.Fatalf("accepted no-wait create failed: %+v", result)
	}
	paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("Volume sidecar is unavailable: %v %v", err, paths)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.State != "journaled" || intent.VolumeObserved {
		t.Fatalf("no-wait sidecar did not retain unobserved state: %v %+v", err, intent)
	}
	journalSHA, err := teamReconcileSHA256(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	binSHA, err := teamReconcileSHA256(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReconcilePendingVolume(context.Background(), cl.journal, VolumeReconcileOptions{
		Config: cfg, RunID: journal.RunID, Surface: journal.Surface, Name: intent.Name, Profile: intent.Profile,
		JournalSHA256: journalSHA, JournalBinSHA256: binSHA, ProbeBin: cfg.Bin, ProbeBinSHA256: binSHA,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(body, &intent) != nil || !intent.VolumeObserved {
		t.Fatalf("positive GetVolume was not retained: %v %+v", err, intent)
	}
	installed, _, err := readRunnerJournal(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deleted-vol-owned"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !journalfile.VolumeAbsenceTerminal(installed, intent, installed.Entries[len(installed.Entries)-1]) {
		t.Fatal("positive reconciliation did not establish exact single-Team retirement eligibility")
	}
	after, _, err := readRunnerJournal(cl.journal)
	if err != nil || len(after.Entries) != len(installed.Entries) || len(after.VolumeTerminalReceipts) != 0 {
		t.Fatalf("retirement eligibility alone changed the journal: %+v %v", after, err)
	}
}

func TestVolumeReconcileListRejectsZeroMultipleAndCollidingNonce(t *testing.T) {
	intent := journalfile.TemplateCreateIntent{Name: "ecctl-p-0123456789abcdef01234567", TeamID: "team-owned"}
	item := func(id, team string) any { return map[string]any{"id": id, "name": intent.Name, "team_id": team} }
	for _, tc := range []struct {
		name  string
		items []any
		more  any
		want  string
	}{
		{"one exact", []any{item("vol-owned", "team-owned")}, false, "vol-owned"},
		{"zero", []any{}, false, ""},
		{"multiple", []any{item("vol-one", "team-owned"), item("vol-two", "team-owned")}, false, ""},
		{"duplicate ID", []any{item("vol-one", "team-owned"), item("vol-one", "team-owned")}, false, ""},
		{"foreign Team", []any{item("vol-one", "team-other")}, false, ""},
		{"partial page", []any{item("vol-one", "team-owned")}, true, ""},
		{"missing page proof", []any{item("vol-one", "team-owned")}, nil, ""},
		{"invalid item", []any{"wrong"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := volumeReconcileListID(map[string]any{"volumes": tc.items, "pagination": map[string]any{"has_more": tc.more}}, intent)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("Volume list proof = %q, %v", got, err)
			}
		})
	}
}

func TestVolumeReconcileGetRequiresOriginalBackingAndIdentity(t *testing.T) {
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	binding := &report.QuotaCredentialBinding{Profile: "owner", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", VolumeFixtureBucket: bucket, VolumeFixtureBinding: binding}
	entry := func(kind, command string) report.Resource {
		return report.Resource{Scope: "stack", ExecutionID: journal.ExecutionID, RegionRole: "primary", Region: journal.Region, VolumeFixtureKind: kind, VolumeFixtureDeleteProtocol: 1, Teardown: command}
	}
	bucketEntry := entry("bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	bucketEntry.VolumeFixtureBucketCreationDate = "2026-11-04T12:00:00Z"
	role := entry("role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}'`)
	role.VolumeFixtureRoleID = "role-created"
	policy := entry("policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"`+bucket+`-read"}'`)
	policy.VolumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
	attachment := entry("attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role","PolicyName":"`+bucket+`-read","PolicyType":"Custom"}'`)
	journal.Entries = []report.Resource{bucketEntry, role, policy, attachment}
	intent := journalfile.TemplateCreateIntent{Name: "ecctl-p-0123456789abcdef01234567", TeamID: "team-owned"}
	volume := map[string]any{
		"id": "vol-owned", "name": intent.Name, "team_id": intent.TeamID,
		"oss_volume_config": map[string]any{"bucketName": bucket, "bucketPath": "/", "endpoint": "https://oss-cn-hangzhou.aliyuncs.com", "readOnly": true},
		"mount_config":      map[string]any{"role": "acs:ram::1234567890123456:role/" + bucket + "-role"},
	}
	if err := volumeReconcileGetProof(map[string]any{"volume": volume}, "vol-owned", journal, intent); err != nil {
		t.Fatalf("owned Volume get was rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"different ID", func(v map[string]any) { v["id"] = "vol-foreign" }},
		{"different Team", func(v map[string]any) { v["team_id"] = "team-foreign" }},
		{"different name", func(v map[string]any) { v["name"] = "foreign" }},
		{"other bucket", func(v map[string]any) { v["oss_volume_config"].(map[string]any)["bucketName"] = "business-bucket" }},
		{"writable", func(v map[string]any) { v["oss_volume_config"].(map[string]any)["readOnly"] = false }},
		{"other role", func(v map[string]any) { v["mount_config"].(map[string]any)["role"] = "business-role" }},
		{"unexpected AgenticFS", func(v map[string]any) {
			v["agentic_fs_volume_config"] = map[string]any{"serverAddr": "foreign.example:/"}
		}},
		{"unexpected AgenticBucket", func(v map[string]any) { v["agentic_bucket_volume_config"] = map[string]any{"bucketName": "foreign"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := map[string]any{}
			for key, value := range volume {
				copy[key] = value
			}
			copy["oss_volume_config"] = map[string]any{}
			for key, value := range volume["oss_volume_config"].(map[string]any) {
				copy["oss_volume_config"].(map[string]any)[key] = value
			}
			copy["mount_config"] = map[string]any{}
			for key, value := range volume["mount_config"].(map[string]any) {
				copy["mount_config"].(map[string]any)[key] = value
			}
			tc.change(copy)
			if err := volumeReconcileGetProof(map[string]any{"volume": copy}, "vol-owned", journal, intent); err == nil {
				t.Fatal("foreign Volume get passed recovery")
			}
		})
	}
}

func TestVolumeReconcileRejectsAnotherSidecarClaimingTheID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	owned := path + ".template-create-0123456789abcdef01234567.json"
	other := path + ".template-create-aaaaaaaaaaaaaaaaaaaaaaaa.json"
	journal := report.CleanupJournal{JournalPath: path, NativeIntentPaths: []string{owned, other}}
	command := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	body, err := json.Marshal(journalfile.TemplateCreateIntent{Resource: "volume", Deletes: []string{command}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkVolumeReconcileIDCollision(journal, owned, "vol-owned"); err == nil {
		t.Fatal("another sidecar's Volume ID was adopted")
	}
	if err := checkVolumeReconcileIDCollision(journal, owned, "vol-other"); err != nil {
		t.Fatalf("unrelated Volume ID collided: %v", err)
	}
	journal.NativeIntentPaths[1] = filepath.Join(t.TempDir(), "foreign.json")
	if err := checkVolumeReconcileIDCollision(journal, owned, "vol-other"); err == nil {
		t.Fatal("sidecar outside the journal was read")
	}
}

func TestVolumeReconcileFinalizerSurvivesJournalWriteWindow(t *testing.T) {
	intent := journalfile.TemplateCreateIntent{Resource: "volume", Region: "cn-hangzhou", Profile: "owned", TeamID: "team-owned", Name: "ecctl-p-0123456789abcdef01234567", AcceptedVolumeID: "vol-owned"}
	command := quotedTemplateCommand([]string{"ecctl", "sandbox", "volume", "delete", "vol-owned", "--region", intent.Region, "--profile", intent.Profile})
	journal := report.CleanupJournal{RegionRole: "primary", Region: intent.Region, ExecutionID: "execution"}
	changed, err := volumeReconcileFinalizer(&journal, intent, command, "case")
	if err != nil || !changed || len(journal.Entries) != 1 {
		t.Fatalf("first finalizer = %v %v %+v", changed, err, journal)
	}
	changed, err = volumeReconcileFinalizer(&journal, intent, command, "case")
	if err != nil || changed || len(journal.Entries) != 1 {
		t.Fatalf("restart duplicated finalizer: %v %v %+v", changed, err, journal)
	}
	journal.Entries[0].Scope = "foreign"
	if changed, err := volumeReconcileFinalizer(&journal, intent, command, "case"); err == nil || changed {
		t.Fatalf("scope collision accepted: %v %v", changed, err)
	}
	journal.Entries[0].Scope = "case"
	journal.Entries = append(journal.Entries, journal.Entries[0])
	if changed, err := volumeReconcileFinalizer(&journal, intent, command, "case"); err == nil || changed {
		t.Fatalf("duplicate finalizer accepted: %v %v", changed, err)
	}
}

func TestVolumeReconcileCannotEnterActiveRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	release, err := journalfile.AcquireRunLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	opt := VolumeReconcileOptions{
		Config: execpkg.Config{Bin: "/bin/ecctl", Region: "cn-hangzhou"},
		RunID:  "run-1", Surface: "public", Name: "ecctl-p-0123456789abcdef01234567", Profile: "owner",
		JournalSHA256: strings.Repeat("a", 64), JournalBinSHA256: strings.Repeat("b", 64),
		ProbeBin: "/bin/probe", ProbeBinSHA256: strings.Repeat("c", 64),
	}
	if _, err := ReconcilePendingVolume(context.Background(), path, opt); err == nil || !strings.Contains(err.Error(), "in use by another run") {
		t.Fatalf("active run lease did not fence Volume reconciliation: %v", err)
	}
}
