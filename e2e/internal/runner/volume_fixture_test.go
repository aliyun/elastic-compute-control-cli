package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

// A fake provider in this package owns every same-name create for its temp
// journal. This grant exists only in test setup and never reaches CLI options.
func grantSyntheticVolumeFixtureScope(t *testing.T, cl *cleanup, bucket string, phases ...string) {
	t.Helper()
	if cl.fixtureMutationScope != nil {
		t.Fatal("synthetic Volume fixture scope already installed")
	}
	allowed := make(map[string]bool, len(phases))
	for _, phase := range phases {
		if phase != "create" && phase != "delete" {
			t.Fatalf("invalid synthetic Volume fixture phase %q", phase)
		}
		allowed[phase] = true
	}
	path, runID, executionID := cl.journal, cl.journalMeta.RunID, cl.journalMeta.ExecutionID
	bin := cl.execCfg["primary"].Bin
	cl.fixtureMutationScope = func(phase, kind, name string, cfg execpkg.Config) bool {
		return allowed[phase] && name == bucket && cl.journal == path &&
			cl.journalMeta.RunID == runID && cl.journalMeta.ExecutionID == executionID &&
			cfg.Bin == bin && (kind == "bucket" || kind == "role" || kind == "policy" || kind == "attachment")
	}
}

func TestNonprivateFixtureJournalCannotBecomeRunnerReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits and fake provider use Unix semantics")
	}
	for _, kind := range []string{"fixture", "Volume Team"} {
		t.Run(kind, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			path, bin, calls := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
			native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			t.Setenv("ECCTL_PROFILE", "fixture-profile")
			t.Setenv("FAKE_JOURNAL_CALLS", calls)
			if err := os.WriteFile(bin, []byte("#!/bin/sh\necho called >> \"$FAKE_JOURNAL_CALLS\"\nexit 90\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"fixture-profile","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=fixture-profile", "FAKE_JOURNAL_CALLS=" + calls}
			pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
			binding, err := journalfile.CaptureQuotaBinding("fixture-profile", "AK", pin, env)
			if err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Profile: binding.Profile, ExpectedIdentity: &pin, Env: env}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			cl.volumeFixtureBinding, cl.volumeFixtureBucket = binding, bucket
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: cfg.Region, Surface: "public", EcctlBin: bin,
				VolumeFixtureBinding: binding, VolumeFixtureBucket: bucket,
				Entries: []report.Resource{{Scope: "injected", Teardown: "ecctl ecs instance delete i-unowned --force", RegionRole: "primary", Region: cfg.Region, ExecutionID: "execution"}},
			}
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "fixture":
				var scope []*cleanupItem
				create := `ecctl call oss PutBucket --request '{"Bucket":"` + bucket + `","ACL":"private"}' --region cn-hangzhou --output=json`
				teardown := `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou --output=json`
				if _, err := cl.reserveVolumeFixture(&scope, "bucket", create, teardown); err == nil || !strings.Contains(err.Error(), "protected cleanup journal is not a private regular file") || len(scope) != 0 {
					t.Fatalf("fixture reservation accepted untrusted journal: scope=%d err=%v", len(scope), err)
				}
			case "Volume Team":
				data := map[string]any{}
				if err := teamOwnership(data); err != nil {
					t.Fatal(err)
				}
				command := "ecctl sandbox team create --name " + data["team_name"].(string) + " --description '" + data["team_owner"].(string) + "' --plan eco --output=json"
				step := scenario.Step{Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json"}
				if intent, err := reserveTeamCreate(command, data, cfg, cl, step, env, nil, binding); err == nil || !strings.Contains(err.Error(), "protected cleanup journal is not a private regular file") || intent != nil {
					t.Fatalf("Volume Team reservation accepted untrusted journal: intent=%+v err=%v", intent, err)
				}
				if intents, err := filepath.Glob(path + ".team-create-*.json"); err != nil || len(intents) != 0 {
					t.Fatalf("Volume Team wrote an intent before journal admission: %v %v", intents, err)
				}
			}
			after, err := os.ReadFile(path)
			info, statErr := os.Stat(path)
			if err != nil || statErr != nil || string(after) != string(before) || info.Mode().Perm() != 0o644 {
				t.Fatalf("rejected journal was rewritten: read=%v stat=%v", err, statErr)
			}
			if log, err := os.ReadFile(calls); !os.IsNotExist(err) {
				t.Fatalf("rejected journal reached provider: calls=%s err=%v", log, err)
			}
		})
	}
}

func TestRunnerReadsOrdinaryLegacyJournalArray(t *testing.T) {
	path := filepath.Join(canonicalCleanupTestDir(t), "legacy-cleanup.json")
	if err := os.WriteFile(path, []byte(`[{"scope":"case","teardown":"ecctl ecs instance delete i-old --force"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || !legacy || len(journal.Entries) != 1 || journal.Entries[0].Scope != "case" || journal.Entries[0].Teardown != "ecctl ecs instance delete i-old --force" {
		t.Fatalf("ordinary legacy journal changed: journal=%+v legacy=%t err=%v", journal, legacy, err)
	}
}

func TestFixtureReservationPostInstallFailureRestoresUnstartedJournal(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "fixture-journal.json")
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	cl.volumeFixtureBucket = bucket
	previous := writeVolumeFixtureReservationJournal
	t.Cleanup(func() { writeVolumeFixtureReservationJournal = previous })
	writeVolumeFixtureReservationJournal = func(path string, journal report.CleanupJournal) error {
		if err := previous(path, journal); err != nil {
			return err
		}
		return errors.New("injected post-install fixture reservation failure")
	}
	var scope []*cleanupItem
	create := `ecctl call oss PutBucket --request '{"Bucket":"` + bucket + `","ACL":"private"}' --region cn-hangzhou --output=json`
	deleteCommand := `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou --output=json`
	if _, err := cl.reserveVolumeFixture(&scope, "bucket", create, deleteCommand); err == nil || len(scope) != 0 {
		t.Fatalf("failed reservation was accepted: scope=%d err=%v", len(scope), err)
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy || len(journal.Entries) != 0 || journal.VolumeFixtureBucket != bucket {
		t.Fatalf("unstarted fixture reservation still blocked cleanup: journal=%+v err=%v", journal, err)
	}
}

func TestFixtureDeleteMarkerPostInstallFailureRestoresUninvokedAttempt(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "fixture-journal.json")
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	cl.volumeFixtureBucket = bucket
	var scope []*cleanupItem
	create := `ecctl call oss PutBucket --request '{"Bucket":"` + bucket + `","ACL":"private"}' --region cn-hangzhou --output=json`
	deleteCommand := `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou --output=json`
	item, err := cl.reserveVolumeFixture(&scope, "bucket", create, deleteCommand)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.settleVolumeFixture(item); err != nil {
		t.Fatal(err)
	}
	if err := cl.recordVolumeFixtureBucketEvidence(item, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	previous := writeVolumeFixtureDeleteAttemptJournal
	t.Cleanup(func() { writeVolumeFixtureDeleteAttemptJournal = previous })
	writeVolumeFixtureDeleteAttemptJournal = func(root *os.Root, path string, journal report.CleanupJournal) error {
		if err := previous(root, path, journal); err != nil {
			return err
		}
		return errors.New("injected post-install marker failure")
	}
	if err := cl.markVolumeFixtureDeleteAttempt(item); err == nil {
		t.Fatal("failed marker write was reported successful")
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy || len(journal.Entries) != 1 || journal.Entries[0].VolumeFixtureDeleteAttempted || item.volumeFixtureDeleteAttempted {
		t.Fatalf("uninvoked fixture delete marker remained spent: journal=%+v err=%v", journal, err)
	}
}

func TestOrdinaryVolumeBucketVerifiedAbsentRetiresWithoutMutationScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	aliyun, native, calls := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json"), filepath.Join(dir, "calls")
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "fixture-profile")
	t.Setenv("FAKE_BUCKET_CALLS", calls)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"fixture-profile","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_BUCKET_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"fixture-profile","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss ListObjects"*) echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'; exit 4;;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=fixture-profile"}}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	command := `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`
	item, err := cl.pushItemWithFixture(&scope, "stack", command, "primary", nil, false, false, false, nil, "bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.settleVolumeFixture(item); err != nil {
		t.Fatal(err)
	}
	if err := cl.recordVolumeFixtureBucketEvidence(item, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if failures := cl.runCleanup(scope, false); len(failures) != 0 || !item.done || len(readJournalEntries(t, path)) != 0 {
		t.Fatalf("verified absent bucket was not retired: failures=%v item=%+v", failures, item)
	}
	log, err := os.ReadFile(calls)
	if err != nil || strings.Contains(string(log), "DeleteBucket") || strings.Contains(string(log), "GetBucketInfo") || !strings.Contains(string(log), "ListObjects") {
		t.Fatalf("absent bucket reached a mutation: %s %v", log, err)
	}
}

func TestVolumeFixtureAncestorBarrierFailurePreventsProviderCreate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "new", "nested", "reports", "cleanup.json")
	bin := filepath.Join(dir, "ecctl")
	aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
	calls := filepath.Join(dir, "calls")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "volume-test")
	t.Setenv("VOLUME_FIXTURE_CALLS", calls)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss PutBucket"*) echo provider-create >> "$VOLUME_FIXTURE_CALLS"; echo '{"product":"oss","operation":"PutBucket","response":{}}';;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
	barrierFailure := errors.New("sync recovery intent ancestor failed")
	previous := ensureVolumeFixtureRecoveryDirectory
	t.Cleanup(func() { ensureVolumeFixtureRecoveryDirectory = previous })
	called := false
	ensureVolumeFixtureRecoveryDirectory = func(candidate string) error {
		called = true
		if candidate != path {
			t.Fatalf("barrier targeted %s, want %s", candidate, path)
		}
		if parent, err := os.Stat(filepath.Dir(path)); err != nil || !parent.IsDir() {
			t.Fatalf("fresh nested journal parent unavailable at barrier: %v", err)
		}
		return barrierFailure
	}
	var scope []*cleanupItem
	caseReport := &report.Case{}
	failures := provisionStack(context.Background(), Options{StepTimeout: 2 * time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope,
		map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, caseReport, nil)
	if !called || !strings.Contains(failures["volume_oss_bucket"], barrierFailure.Error()) || len(scope) != 0 || len(caseReport.Steps) != 1 || caseReport.Steps[0].Status != report.StatusError {
		t.Fatalf("failed ancestor barrier accepted fixture: called=%v failures=%v scope=%d steps=%+v", called, failures, len(scope), caseReport.Steps)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("fixture provider ran after failed ancestor barrier: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed ancestor barrier installed a reservation: %v", err)
	}
}

func TestVolumeFixtureRequiresExclusiveTrustedIdentityBeforeReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, tc := range []struct {
		name, identity, failure string
		allowed, scopeGrant     bool
	}{
		{"Account", `{"response":{"AccountId":"1234567890123456","IdentityType":"Account"}}`, "RAMUser or AssumedRoleUser", false, true},
		{"RAMUser without mutation scope", `{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}`, "", true, false},
		{"RAMUser", `{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}`, "", true, true},
		{"AssumedRoleUser", `{"response":{"AccountId":"1234567890123456","IdentityType":"AssumedRoleUser","RoleId":"role-a"}}`, "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
			native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
			calls := filepath.Join(dir, "provider-calls")
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			t.Setenv("ECCTL_PROFILE", "volume-test")
			t.Setenv("FAKE_STS_JSON", tc.identity)
			t.Setenv("VOLUME_FIXTURE_CALLS", calls)
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo "$FAKE_STS_JSON";;
  *"call oss PutBucket"*) printf '%s\n' "$*" >> "$VOLUME_FIXTURE_CALLS"; echo '{"product":"oss","operation":"PutBucket","region":"cn-hangzhou","response":{"RequestId":"req-put-bucket","ServerDate":"Mon, 16 Nov 2026 13:00:00 GMT","Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-16T13:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			if tc.scopeGrant {
				grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
			}
			if tc.name == "RAMUser without mutation scope" {
				if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
					t.Fatal(err)
				}
			}
			fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
			var scope []*cleanupItem
			steps := &report.Case{}
			failures := provisionStack(context.Background(), Options{StepTimeout: 2 * time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope,
				map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, steps, nil)
			if !tc.allowed {
				if !strings.Contains(failures["volume_oss_bucket"], tc.failure) || len(scope) != 0 || len(steps.Steps) != 1 || steps.Steps[0].Status != report.StatusError || cl.volumeFixtureBinding != nil {
					t.Fatalf("rejected fixture published a binding: failures=%v scope=%d steps=%+v binding=%+v", failures, len(scope), steps.Steps, cl.volumeFixtureBinding)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("Account installed a journal: %v", err)
				}
				if _, err := os.Stat(calls); !os.IsNotExist(err) {
					t.Fatalf("Account reached PutBucket: %v", err)
				}
				if tc.name == "Account" {
					cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}}
					cl.volumeFixtureBucket = bucket
					if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err == nil {
						t.Fatal("cached Account binding was accepted")
					}
					if _, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`); err == nil || len(scope) != 0 {
						t.Fatalf("cached Account reserved a bucket: scope=%d err=%v", len(scope), err)
					}
				}
				return
			}
			wantType := tc.name
			if tc.name == "RAMUser without mutation scope" {
				wantType = "RAMUser"
			}
			if len(failures) != 0 || len(scope) != 1 || len(steps.Steps) != 1 || steps.Steps[0].Status != report.StatusPass || cl.volumeFixtureBinding == nil || cl.volumeFixtureBinding.Identity.Type != wantType {
				t.Fatalf("trusted %s fixture failed: failures=%v scope=%d steps=%+v binding=%+v", tc.name, failures, len(scope), steps.Steps, cl.volumeFixtureBinding)
			}
			bound, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second)
			if err != nil || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != cl.volumeFixtureBinding.Identity {
				t.Fatalf("trusted %s cached binding changed: bound=%+v err=%v", tc.name, bound, err)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil || len(journal.Entries) != 1 || journal.Entries[0].VolumeFixturePending {
				t.Fatalf("trusted %s create was not settled: journal=%+v err=%v", tc.name, journal, err)
			}
			callsBody, err := os.ReadFile(calls)
			if err != nil || !strings.Contains(string(callsBody), "PutBucket") {
				t.Fatalf("trusted %s did not reach PutBucket: calls=%q err=%v", tc.name, callsBody, err)
			}
		})
	}
}

func TestLegacyAccountVolumeCaseStepStopsBeforeProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir := t.TempDir()
	bin, calls := filepath.Join(dir, "ecctl"), filepath.Join(dir, "provider-calls")
	t.Setenv("FAKE_VOLUME_CALLS", calls)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho invoked >> \"$FAKE_VOLUME_CALLS\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(dir, "cleanup.json"),
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}}
	cl.volumeFixtureBucket = "ecctl-e2e-20261104t120000-123456abcdef"
	step := scenario.Step{Name: "explicit-fixture-delete", Run: `ecctl call oss DeleteBucket --request '{"Bucket":"ecctl-e2e-20261104t120000-123456abcdef"}' --region cn-hangzhou`}
	var scope []*cleanupItem
	got, ok := runStepUnfenced(context.Background(), Options{volumeLifecycle: true}, cfg, cl, &scope, map[string]any{}, step, time.Second)
	if ok || got.Status != report.StatusError || !strings.Contains(got.Error, "RAMUser or AssumedRoleUser") {
		t.Fatalf("explicit Volume case accepted old Account binding: step=%+v ok=%t", got, ok)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("explicit Volume case invoked provider: %v", err)
	}
}

func TestLegacyAccountVolumeTeamReservationStopsBeforeJournal(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cleanup.json")
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}
	binding := &report.QuotaCredentialBinding{Profile: "fixture-profile", Identity: pin}
	cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou", Profile: binding.Profile, ExpectedIdentity: &pin}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	data := map[string]any{}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	command := "ecctl sandbox team create --name " + data["team_name"].(string) + " --description '" + data["team_owner"].(string) + "' --plan eco --output=json"
	step := scenario.Step{Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json"}
	if _, err := reserveTeamCreate(command, data, cfg, cl, step, nil, nil, binding); err == nil || !strings.Contains(err.Error(), "RAMUser or AssumedRoleUser") {
		t.Fatalf("old Account Volume Team reservation was accepted: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old Account Volume Team installed a journal: %v", err)
	}
}

func TestVolumeFixtureCreateRefusesCopiedJournalAfterParentRotation(t *testing.T) {
	cfg, _, dir := nativeCleanupFixture(t, true)
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "account-a")
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=account-a", "FAKE_NATIVE_DIR=" + dir}
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	fake := strings.Replace(string(body), "echo '{}'\n", `if [[ "$*" == *"call oss PutBucket"* ]]; then echo provider-create >> "$FAKE_NATIVE_DIR/calls"; echo '{"product":"oss","operation":"PutBucket","response":{}}'; exit 0; fi
echo '{}'
`, 1)
	if fake == string(body) {
		t.Fatal("fake provider insertion point missing")
	}
	if err := os.WriteFile(cfg.Bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "owned")
	path := filepath.Join(parent, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
	rotated := false
	logf := func(string, ...any) {
		if rotated {
			return
		}
		journal, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(parent, parent+"-old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, journal, 0o600); err != nil {
			t.Fatal(err)
		}
		rotated = true
	}
	var scope []*cleanupItem
	caseReport := &report.Case{}
	failures := provisionStack(context.Background(), Options{StepTimeout: time.Second, Logf: logf}, cfg, cl, &scope,
		map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, caseReport, nil)
	if !rotated || failures["volume_oss_bucket"] == "" || len(scope) != 1 || len(caseReport.Steps) != 1 || caseReport.Steps[0].Status != report.StatusError {
		t.Fatalf("rotated journal parent reached provider: rotated=%t failures=%v scope=%+v steps=%+v", rotated, failures, scope, caseReport.Steps)
	}
	if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
		t.Fatalf("fixture provider was called after parent rotation: %v", err)
	}
	if err := cl.checkReservedVolumeFixture(scope[0]); err == nil {
		t.Fatal("copied pending journal passed pre-invocation guard")
	}
	if err := cl.settleVolumeFixture(scope[0]); err == nil {
		t.Fatal("copied pending journal was settled")
	}
	if journal, _, err := readRunnerJournal(path); err != nil || len(journal.Entries) != 1 || !journal.Entries[0].VolumeFixturePending {
		t.Fatalf("copied journal lost pending fixture: %+v %v", journal, err)
	}
}

func TestVolumeFixturePreInvocationRequiresExactPendingEntry(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "account-a")
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Env = []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=account-a", "FAKE_NATIVE_DIR=" + dir}
	cl.execCfg["primary"] = cfg
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	item, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.checkReservedVolumeFixture(item); err != nil {
		t.Fatalf("original pending fixture was rejected: %v", err)
	}
	journal, _, err := readRunnerJournal(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	journal.Entries[0].VolumeFixturePending = false
	if err := writeRunnerJournal(cl.journal, journal); err != nil {
		t.Fatal(err)
	}
	if err := cl.checkReservedVolumeFixture(item); err == nil {
		t.Fatal("changed pending fixture passed pre-invocation guard")
	}
	if err := cl.settleVolumeFixture(item); err == nil {
		t.Fatal("changed pending fixture was settled")
	}
}

func TestPinnedVolumeOSSRequestReachesSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
	calls := filepath.Join(dir, "calls")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "volume-test")
	t.Setenv("VOLUME_FIXTURE_CALLS", calls)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss PutBucket"*) printf '%s\n' "$*" > "$VOLUME_FIXTURE_CALLS"; echo '{"product":"oss","operation":"PutBucket","region":"cn-hangzhou","response":{"RequestId":"req-put-bucket","ServerDate":"Mon, 16 Nov 2026 13:00:00 GMT","Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-16T13:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	requested := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: requested}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
	var scope []*cleanupItem
	caseReport := &report.Case{}
	failures := provisionStack(context.Background(), Options{StepTimeout: 2 * time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope,
		map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, caseReport, nil)
	if len(failures) != 0 || len(scope) != 1 || len(caseReport.Steps) != 1 || caseReport.Steps[0].Status != report.StatusPass {
		t.Fatalf("pinned OSS fixture did not complete: failures=%v scope=%d steps=%+v", failures, len(scope), caseReport.Steps)
	}
	journal, _, err := readRunnerJournal(path)
	if err != nil || len(journal.Entries) != 1 || journal.Entries[0].VolumeFixturePending {
		t.Fatalf("successful provider call did not settle ownership: journal=%+v err=%v", journal, err)
	}
	log, err := os.ReadFile(calls)
	if err != nil || !strings.Contains(string(log), "--profile volume-test") || !strings.Contains(string(log), "--request") || !strings.Contains(string(log), bucket) {
		t.Fatalf("pinned fixture argv = %q, err=%v", log, err)
	}
}

func TestVolumeBucketCreateFailureKeepsPreflightReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cleanup.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	native := filepath.Join(dir, "native.json")
	bin := filepath.Join(dir, "ecctl")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "volume-test")
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss PutBucket"*) echo '{"error":{"code":"InternalError"}}'; exit 42;;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	requested := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: requested}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
	var scope []*cleanupItem
	caseReport := &report.Case{}
	failures := provisionStack(context.Background(), Options{StepTimeout: 2 * time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope,
		map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, caseReport, nil)
	if failures["volume_oss_bucket"] == "" || len(scope) != 1 || len(caseReport.Steps) != 1 || caseReport.Steps[0].Status != report.StatusFail {
		t.Fatalf("failed setup lost reservation: failures=%v scope=%d steps=%+v", failures, len(scope), caseReport.Steps)
	}
	journal, _, err := readRunnerJournal(path)
	if err != nil || len(journal.Entries) != 1 || !journal.Entries[0].VolumeFixturePending {
		t.Fatalf("failed provider call lost pending ownership: journal=%+v err=%v", journal, err)
	}
}

func TestVolumeBucketDuplicateCreateCannotAdoptOrDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	calls := filepath.Join(dir, "provider-calls")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "volume-test")
	t.Setenv("FAKE_VOLUME_CREATE_CALLS", calls)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss PutBucket"*) printf '%s\n' "$*" >> "$FAKE_VOLUME_CREATE_CALLS"; echo '{"error":{"kind":"conflict","code":"BucketAlreadyExists"},"actions":[{"action_name":"PutBucket","code":"BucketAlreadyExists"}]}'; exit 4;;
  *"call oss DeleteBucket"*) echo DeleteBucket >> "$FAKE_VOLUME_CREATE_CALLS"; exit 90;;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_oss_bucket", Resource: "oss/bucket", Run: `ecctl call oss PutBucket --request '{"Bucket":"{{.oss_bucket_name}}","ACL":"private"}' --region cn-hangzhou`, Teardown: `ecctl call oss DeleteBucket --request '{"Bucket":"{{.oss_bucket_name}}"}' --region cn-hangzhou`}}}
	var scope []*cleanupItem
	steps := &report.Case{}
	failures := provisionStack(context.Background(), Options{StepTimeout: 2 * time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope,
		map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, steps, nil)
	if failures["volume_oss_bucket"] == "" || len(scope) != 1 || len(steps.Steps) != 1 || steps.Steps[0].Status != report.StatusFail {
		t.Fatalf("duplicate PutBucket adopted fixture: failures=%v scope=%d steps=%+v", failures, len(scope), steps.Steps)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	journal, _, err := readRunnerJournal(path)
	if err != nil || len(journal.Entries) != 1 || !journal.Entries[0].VolumeFixturePending || journal.Entries[0].VolumeFixtureDeleteAttempted {
		t.Fatalf("duplicate PutBucket lost pending fence: journal=%+v err=%v", journal, err)
	}
	if failures := cl.runCleanup(scope, false); len(failures) == 0 {
		t.Fatal("pending duplicate PutBucket was cleaned automatically")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("pending duplicate journal changed: err=%v", err)
	}
	log, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(log), "call oss PutBucket") != 1 || strings.Contains(string(log), "DeleteBucket") {
		t.Fatalf("duplicate PutBucket reached cleanup provider: log=%q err=%v", log, err)
	}
}

func TestLegacyAccountVolumeFixtureOrdinaryCleanupRetainsJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	mode, calls := filepath.Join(dir, "account-mode"), filepath.Join(dir, "provider-calls")
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	t.Setenv("ECCTL_PROFILE", "volume-test")
	t.Setenv("FAKE_VOLUME_ACCOUNT_MODE", mode)
	t.Setenv("FAKE_VOLUME_CLEANUP_CALLS", calls)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*)
    if [ -f "$FAKE_VOLUME_ACCOUNT_MODE" ]; then echo '{"response":{"AccountId":"1234567890123456","IdentityType":"Account"}}'
    else echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}'; fi;;
  *"call oss "*|*"call ram "*) printf '%s\n' "$*" >> "$FAKE_VOLUME_CLEANUP_CALLS"; exit 90;;
  *) exit 90;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	item, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.settleVolumeFixture(item); err != nil {
		t.Fatal(err)
	}
	journal, _, err := readRunnerJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	account := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "Account"}
	journal.VolumeFixtureBinding.Identity = account
	cl.volumeFixtureBinding.Identity = account
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mode, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	failures := cl.runCleanup(scope, false)
	if len(failures) == 0 {
		t.Fatalf("old Account fixture cleanup did not fail closed: %v", failures)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("old Account fixture journal was changed: err=%v", err)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("old Account fixture cleanup invoked a provider mutation: %v", err)
	}
}

func TestVolumeFixtureReservationSurvivesUncertainCreate(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cleanup.json")
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	cl := newCleanup(map[string]execpkg.Config{"primary": {Bin: "/tmp/ecctl", Region: "cn-hangzhou"}}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
	cl.volumeFixtureBucket = bucket
	cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	var scope []*cleanupItem
	item, err := cl.reserveVolumeFixture(&scope, "bucket", `ecctl call oss PutBucket --request '{"Bucket":"`+bucket+`","ACL":"private"}' --region cn-hangzhou`, `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`)
	if err != nil {
		t.Fatal(err)
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy || journal.JournalPath != path || len(journal.Entries) != 1 || !journal.Entries[0].VolumeFixturePending {
		t.Fatalf("create reservation missing before provider call: journal=%+v err=%v", journal, err)
	}
	if err := journalfile.VolumeFixtureCleanupReady(path, journal, journal.Entries[0], nil); err == nil {
		t.Fatal("uncertain create was eligible for cleanup")
	}
	if err := cl.settleVolumeFixture(item); err != nil {
		t.Fatal(err)
	}
	journal, _, err = readRunnerJournal(path)
	if err != nil || journal.Entries[0].VolumeFixturePending {
		t.Fatalf("confirmed reservation stayed pending: journal=%+v err=%v", journal, err)
	}
	if _, err := readGuardJournal(path, journal); err != nil {
		t.Fatalf("fixture journal guard rejected retained ownership: %v", err)
	}
	changed := journal
	changed.VolumeFixtureBucket = "ecctl-e2e-20261104t120000-fedcba654321"
	if _, err := readGuardJournal(path, changed); err == nil {
		t.Fatal("fixture journal guard accepted a changed bucket")
	}
}

func TestUnstartedVolumePolicyReservationAllowsAncestorCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, mode := range []string{"normal", "replay"} {
		t.Run(mode, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bin, path := filepath.Join(dir, "ecctl"), filepath.Join(dir, "cleanup.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			t.Setenv("ECCTL_PROFILE", "volume-test")
			t.Setenv("FAKE_FIXTURE_CALLS", filepath.Join(dir, "calls"))
			t.Setenv("FAKE_FIXTURE_BUCKET_DELETED", filepath.Join(dir, "bucket-deleted"))
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"volume-test","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"volume-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss ListObjects"*)
    if [ -f "$FAKE_FIXTURE_BUCKET_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}';;
  *"call oss GetBucketInfo"*) echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call ram GetRole"*)
    if [ -f "$FAKE_FIXTURE_BUCKET_DELETED.role" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram DeleteRole"*) touch "$FAKE_FIXTURE_BUCKET_DELETED.role"; echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-role"}}';;
  *"call oss DeleteBucket"*) touch "$FAKE_FIXTURE_BUCKET_DELETED"; echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=volume-test"}}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "volume-run", ExecutionID: "volume-execution", Surface: "public"}, func(string, ...any) {})
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "create", "delete")
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			ancestors := []struct{ kind, command string }{
				{"bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`},
				{"role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`},
			}
			for _, ancestor := range ancestors {
				item, err := cl.pushItemWithFixture(&scope, "stack", ancestor.command, "primary", nil, false, false, false, nil, ancestor.kind, nil)
				if err != nil {
					t.Fatal(err)
				}
				if ancestor.kind == "role" {
					item.volumeFixtureRoleID = "role-created"
				}
				if err := cl.settleVolumeFixture(item); err != nil {
					t.Fatal(err)
				}
				if ancestor.kind == "bucket" {
					if err := cl.recordVolumeFixtureBucketEvidence(item, "2026-11-04T12:00:00Z"); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_ram_policy", Resource: "ram/policy", Run: `ecctl call ram CreatePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read","Description":"ecctl E2E Volume ` + bucket + `","PolicyDocument":"{\"Version\":\"1\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"oss:ListObjects\"],\"Resource\":[\"acs:oss:*:*:` + bucket + `\"]},{\"Effect\":\"Allow\",\"Action\":[\"oss:GetObject\"],\"Resource\":[\"acs:oss:*:*:` + bucket + `/*\"]}]}"}'`, Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`}}}
			opt := Options{StepTimeout: time.Second, Logf: func(format string, _ ...any) {
				if strings.Contains(format, "stack %q") {
					cancel()
				}
			}}
			failures := provisionStack(ctx, opt, cfg, cl, &scope, map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, &report.Case{}, nil)
			if failures["volume_ram_policy"] == "" || len(scope) != 3 || !scope[2].done {
				t.Fatalf("unstarted policy was not retired: failures=%v scope=%+v", failures, scope)
			}
			journal, _, err := readRunnerJournal(path)
			if err != nil || len(journal.Entries) != 2 || journal.Entries[0].VolumeFixturePending || journal.Entries[1].VolumeFixturePending || journal.Entries[0].VolumeFixtureDeleteProtocol != 1 || journal.Entries[1].VolumeFixtureDeleteProtocol != 1 {
				t.Fatalf("ancestor journal changed during retirement: journal=%+v err=%v", journal, err)
			}
			if mode == "normal" {
				if failures := cl.runCleanup(scope, false); len(failures) != 0 {
					t.Fatalf("normal ancestor cleanup failed: %v", failures)
				}
			} else {
				result, err := sweeper.ReplayJournal(context.Background(), path, cfg)
				if err != nil || result.Errors != 1 || result.Deleted != 0 || len(result.Details) != 1 || !strings.Contains(result.Details[0], "mutation scope") {
					t.Fatalf("production replay bypassed name mutation fence: result=%+v err=%v", result, err)
				}
				if entries := readJournalEntries(t, path); len(entries) != 2 {
					t.Fatalf("fenced replay lost ancestors: %+v", entries)
				}
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil || strings.Contains(string(calls), "DeleteRole") || strings.Contains(string(calls), "DeleteBucket") {
					t.Fatalf("fenced replay reached provider mutation: %q err=%v", calls, err)
				}
				return
			}
			if entries := readJournalEntries(t, path); len(entries) != 0 {
				t.Fatalf("ancestor cleanup left entries: %+v", entries)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || strings.Contains(string(calls), "CreatePolicy") || !strings.Contains(string(calls), "DeleteRole") || !strings.Contains(string(calls), "DeleteBucket") {
				t.Fatalf("unexpected provider calls after prestart cancellation: %q err=%v", calls, err)
			}
		})
	}
}

func TestVolumeFixtureBucketCaptureMustMatchTrustedIdentity(t *testing.T) {
	cl := &cleanup{volumeFixtureBucket: "ecctl-e2e-20261104t120000-123456abcdef",
		volumeFixtureBinding: &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}}
	values := map[string]any{
		"volume_bucket_name": "ecctl-e2e-20261104t120000-123456abcdef", "volume_bucket_owner_id": "1234567890123456",
		"volume_bucket_location": "oss-cn-hangzhou", "volume_bucket_endpoint": "oss-cn-hangzhou.aliyuncs.com",
		"volume_bucket_creation_date": "2026-11-04T12:00:00Z",
	}
	result := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "oss", "operation": "GetBucketInfo", "region": "cn-hangzhou", "response": map[string]any{"Bucket": map[string]any{"Name": cl.volumeFixtureBucket, "Location": "oss-cn-hangzhou", "ExtranetEndpoint": "oss-cn-hangzhou.aliyuncs.com", "CreationDate": values["volume_bucket_creation_date"], "Owner": map[string]any{"ID": "1234567890123456"}}}}}
	if err := validVolumeFixtureBucketInfo(cl, values, result); err != nil {
		t.Fatal(err)
	}
	values["volume_bucket_owner_id"] = "0000000000000000"
	if err := validVolumeFixtureBucketInfo(cl, values, result); err == nil {
		t.Fatal("foreign bucket owner was accepted")
	}
	values["volume_bucket_owner_id"] = "1234567890123456"
	response := result.JSON.(map[string]any)["response"].(map[string]any)
	bucket := response["Bucket"]
	delete(response, "Bucket")
	response["BucketInfo"] = map[string]any{"Bucket": bucket}
	if err := validVolumeFixtureBucketInfo(cl, values, result); err == nil {
		t.Fatal("obsolete wrapped bucket response was accepted")
	}
	delete(response, "BucketInfo")
	response["Bucket"] = "malformed"
	if err := validVolumeFixtureBucketInfo(cl, values, result); err == nil {
		t.Fatal("malformed bucket response was accepted")
	}
	response["Bucket"] = bucket
	bucket.(map[string]any)["Owner"].(map[string]any)["ID"] = "0000000000000000"
	if err := validVolumeFixtureBucketInfo(cl, values, result); err == nil {
		t.Fatal("provider bucket owner mismatch was accepted")
	}
}

func TestVolumeFixtureRAMCreateRequiresProviderEvidence(t *testing.T) {
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	cl := &cleanup{volumeFixtureBucket: bucket, volumeFixtureBinding: &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}}
	for _, tc := range []struct {
		name, kind string
		change     func(map[string]any, map[string]any)
		wantValid  bool
	}{
		{name: "policy response", kind: "policy", wantValid: true},
		{name: "attachment response", kind: "attachment", wantValid: true},
		{name: "missing request ID", kind: "policy", change: func(_, response map[string]any) { delete(response, "RequestId") }},
		{name: "blank request ID", kind: "attachment", change: func(_, response map[string]any) { response["RequestId"] = " " }},
		{name: "wrong policy name", kind: "policy", change: func(_, response map[string]any) { response["Policy"].(map[string]any)["PolicyName"] = "other" }},
		{name: "wrong policy type", kind: "policy", change: func(_, response map[string]any) { response["Policy"].(map[string]any)["PolicyType"] = "System" }},
		{name: "missing policy", kind: "policy", change: func(_, response map[string]any) { delete(response, "Policy") }},
		{name: "provider error body", kind: "policy", change: func(_, response map[string]any) { response["Code"] = "Failure" }},
		{name: "provider message body", kind: "policy", change: func(_, response map[string]any) { response["Message"] = "failed" }},
		{name: "error envelope", kind: "attachment", change: func(root, _ map[string]any) { root["error"] = map[string]any{"code": "Failure"} }},
		{name: "wrong operation", kind: "attachment", change: func(root, _ map[string]any) { root["operation"] = "CreatePolicy" }},
		{name: "empty response", kind: "attachment", change: func(root, _ map[string]any) { root["response"] = map[string]any{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation := map[string]string{"policy": "CreatePolicy", "attachment": "AttachPolicyToRole"}[tc.kind]
			response := map[string]any{"RequestId": "request-create"}
			if tc.kind == "policy" {
				response["Policy"] = map[string]any{"PolicyName": bucket + "-read", "PolicyType": "Custom", "CreateDate": "2026-11-04T12:00:00Z"}
			}
			root := map[string]any{"product": "ram", "operation": operation, "region": "cn-hangzhou", "response": response}
			if tc.change != nil {
				tc.change(root, response)
			}
			err := validVolumeFixtureRAMCreate(cl, tc.kind, execpkg.Result{Started: true, Exit: 0, JSON: root})
			if (err == nil) != tc.wantValid {
				t.Fatalf("RAM create evidence accepted=%t, want=%t: %v", err == nil, tc.wantValid, err)
			}
		})
	}
}

func TestCreatedVolumeRoleIDIsDurableBeforeSettlement(t *testing.T) {
	for _, mode := range []string{"settled", "identity write before install", "identity write after install"} {
		t.Run(mode, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			cl.volumeFixtureBinding = &report.QuotaCredentialBinding{Profile: "quota-test", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}}
			cl.volumeFixtureBucket = bucket
			parent, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			cl.journalParents = map[string]os.FileInfo{path: parent}
			var scope []*cleanupItem
			bucketItem, err := cl.pushItemWithFixture(&scope, "stack", `ecctl call oss DeleteBucket --request '{"Bucket":"`+bucket+`"}' --region cn-hangzhou`, "primary", nil, false, false, false, nil, "bucket", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := cl.settleVolumeFixture(bucketItem); err != nil {
				t.Fatal(err)
			}
			roleItem, err := cl.pushItemWithFixture(&scope, "stack", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"`+bucket+`-role"}'`, "primary", nil, false, false, false, nil, "role", nil)
			if err != nil {
				t.Fatal(err)
			}
			vars := map[string]any{"volume_role_arn": "acs:ram::1234567890123456:role/" + bucket + "-role", "volume_role_name": bucket + "-role", "volume_role_id": "role-created"}
			created := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "CreateRole", "region": "cn-hangzhou", "response": map[string]any{"RequestId": "request-create-role", "Role": map[string]any{"Arn": vars["volume_role_arn"], "RoleName": vars["volume_role_name"], "RoleId": vars["volume_role_id"]}}}}
			if err := validVolumeFixtureRole(cl, vars, created); err != nil {
				t.Fatal(err)
			}
			roleItem.volumeFixtureRoleID = vars["volume_role_id"].(string)
			previous := writeVolumeFixtureRoleIdentityJournal
			t.Cleanup(func() { writeVolumeFixtureRoleIdentityJournal = previous })
			if mode != "settled" {
				writeVolumeFixtureRoleIdentityJournal = func(root *os.Root, path string, journal report.CleanupJournal) error {
					if mode == "identity write after install" {
						if err := previous(root, path, journal); err != nil {
							return err
						}
					}
					return errors.New("injected role identity persistence uncertainty")
				}
			}
			err = cl.settleVolumeFixture(roleItem)
			journal, _, readErr := readRunnerJournal(path)
			if readErr != nil || len(journal.Entries) != 2 {
				t.Fatalf("created role reservation disappeared: %+v %v", journal, readErr)
			}
			role := journal.Entries[1]
			if mode == "settled" {
				if err != nil || role.VolumeFixturePending || role.VolumeFixtureRoleID != "role-created" || roleItem.volumeFixturePending {
					t.Fatalf("created RoleId was not durably settled: %+v item=%+v err=%v", role, roleItem, err)
				}
			} else if err == nil || role.VolumeFixturePending != (mode == "identity write before install") || !roleItem.volumeFixturePending || role.VolumeFixtureCreateValidated || role.VolumeFixtureRoleID != map[string]string{"identity write before install": "", "identity write after install": "role-created"}[mode] {
				t.Fatalf("uncertain RoleId write cleared the deletion fence: %+v item=%+v err=%v", role, roleItem, err)
			}
		})
	}
}

func TestVolumeRoleCreateUsesAssignedNameAndARN(t *testing.T) {
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	cl := &cleanup{volumeFixtureBucket: bucket, volumeFixtureBinding: &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}}
	for _, tc := range []struct {
		name, capturedID, returnedID, requestID string
		started, accepted                       bool
	}{
		{"missing captured ID", "", "role-created", "request-create", true, true},
		{"missing returned ID", "role-created", "", "request-create", true, true},
		{"different returned ID", "role-created", "role-replacement", "request-create", true, true},
		{"unknown response", "role-created", "role-created", "", true, false},
		{"unstarted response", "role-created", "role-created", "request-create", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := map[string]any{"volume_role_arn": "acs:ram::1234567890123456:role/" + bucket + "-role", "volume_role_name": bucket + "-role", "volume_role_id": tc.capturedID}
			result := execpkg.Result{Started: tc.started, Exit: 0, JSON: map[string]any{"product": "ram", "operation": "CreateRole", "region": "cn-hangzhou", "response": map[string]any{"RequestId": tc.requestID, "Role": map[string]any{"Arn": vars["volume_role_arn"], "RoleName": vars["volume_role_name"], "RoleId": tc.returnedID}}}}
			if err := validVolumeFixtureRole(cl, vars, result); (err == nil) != tc.accepted {
				t.Fatalf("CreateRole name proof accepted=%v, err=%v", tc.accepted, err)
			}
		})
	}
}

func TestVolumePolicyRejectedAndUnknownCreateResults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprint("verified_rejection=", rejected), func(t *testing.T) {
			t.Setenv("FAKE_POLICY_REJECTION", "0")
			if rejected {
				t.Setenv("FAKE_POLICY_REJECTION", "1")
			}
			dir := canonicalCleanupTestDir(t)
			bin, path := filepath.Join(dir, "ecctl"), filepath.Join(dir, "cleanup.json")
			aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			t.Setenv("ECCTL_PROFILE", "fixture-profile")
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"fixture-profile","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"fixture-profile","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call ram CreatePolicy"*)
    if [ "$FAKE_POLICY_REJECTION" = 1 ]; then
      echo '{"error":{"kind":"service","code":"CloudAPIError","retryable":false},"actions":[{"action_name":"CreatePolicy","code":"Forbidden","request_id":"req-policy","provider_verified":true}]}'
      exit 2
    fi
    echo '{"product":"ram","operation":"CreatePolicy","region":"cn-hangzhou","response":{}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=fixture-profile"}}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			for _, ancestor := range []struct{ kind, command string }{
				{"bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`},
				{"role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`},
			} {
				item, err := cl.pushItemWithFixture(&scope, "stack", ancestor.command, "primary", nil, false, false, false, nil, ancestor.kind, nil)
				if err != nil {
					t.Fatal(err)
				}
				if ancestor.kind == "role" {
					item.volumeFixtureRoleID = "role-created"
				}
				if err := cl.settleVolumeFixture(item); err != nil {
					t.Fatal(err)
				}
			}
			fixture := &Fixture{Provision: []ProvisionStep{{ID: "volume_ram_policy", Resource: "ram/policy", Run: `ecctl call ram CreatePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read","Description":"ecctl E2E Volume ` + bucket + `","PolicyDocument":"{\"Version\":\"1\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"oss:ListObjects\"],\"Resource\":[\"acs:oss:*:*:` + bucket + `\"]},{\"Effect\":\"Allow\",\"Action\":[\"oss:GetObject\"],\"Resource\":[\"acs:oss:*:*:` + bucket + `/*\"]}]}"}'`, Teardown: `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`}}}
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "create")
			failures := provisionStack(context.Background(), Options{StepTimeout: time.Second, Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{"oss_bucket_name": bucket}, map[string]any{}, fixture, &report.Case{}, nil)
			journal, _, err := readRunnerJournal(path)
			if rejected {
				if failures["volume_ram_policy"] == "" || err != nil || len(journal.Entries) != 2 || len(scope) != 3 || !scope[2].done || journalfile.VolumeFixtureCleanupReady(path, journal, journal.Entries[0], cfg.Env) != nil {
					t.Fatalf("verified rejection stranded earlier fixtures: failures=%v journal=%+v err=%v", failures, journal, err)
				}
				return
			}
			if failures["volume_ram_policy"] == "" || err != nil || len(journal.Entries) != 3 || !journal.Entries[2].VolumeFixturePending || len(scope) != 3 || !scope[2].volumeFixturePending {
				t.Fatalf("unknown policy create result settled ownership: failures=%v journal=%+v err=%v", failures, journal, err)
			}
			if err := journalfile.CheckVolumeFixtureParents("attachment", journal); err == nil {
				t.Fatal("unknown policy result allowed attachment allocation")
			}
		})
	}
}

func TestOrdinaryVolumePolicyDeleteNeverRepeatsAfterConsumptionUncertainty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, mode := range []string{"normal", "before install", "after install"} {
		t.Run(mode, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			bin, path := filepath.Join(dir, "ecctl"), filepath.Join(dir, "cleanup.json")
			aliyun, native := filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "native.json")
			present, calls := filepath.Join(dir, "policy-present"), filepath.Join(dir, "calls")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			t.Setenv("ECCTL_PROFILE", "fixture-profile")
			t.Setenv("FAKE_POLICY_PRESENT", present)
			t.Setenv("FAKE_POLICY_CALLS", calls)
			t.Setenv("FAKE_POLICY_JOURNAL", path)
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"fixture-profile","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(present, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_POLICY_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"fixture-profile","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss ListObjects"*)
    echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
    exit 4;;
  *"call oss DeleteBucket"*) echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *"call ram GetPolicy"*)
    if [ -f "$FAKE_POLICY_PRESENT" ] && [ "$(cat "$FAKE_POLICY_PRESENT")" = original ]; then
      echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}'
    elif [ -f "$FAKE_POLICY_PRESENT" ]; then
      echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-05T12:00:00Z"}}}'
    else
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi;;
  *"call ram DeletePolicy"*)
    if ! grep -q '"volume_fixture_delete_attempted": true' "$FAKE_POLICY_JOURNAL"; then exit 91; fi
    rm "$FAKE_POLICY_PRESENT"
    echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"first-policy-delete"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=fixture-profile"}}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			ancestors := []struct{ kind, command string }{
				{"bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`},
				{"role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`},
			}
			for _, ancestor := range ancestors {
				prior, err := cl.pushItemWithFixture(&scope, "stack", ancestor.command, "primary", nil, false, false, false, nil, ancestor.kind, nil)
				if err != nil {
					t.Fatal(err)
				}
				if ancestor.kind == "role" {
					prior.volumeFixtureRoleID = "role-created"
				}
				if err := cl.settleVolumeFixture(prior); err != nil {
					t.Fatal(err)
				}
			}
			command := `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`
			item, err := cl.pushItemWithFixture(&scope, "stack", command, "primary", nil, false, false, false, nil, "policy", nil)
			if err != nil {
				t.Fatal(err)
			}
			item.volumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
			if err := cl.settleVolumeFixture(item); err != nil {
				t.Fatal(err)
			}
			if item.volumeFixtureDeleteProtocol != 1 {
				t.Fatal("fresh policy reservation lacks the first-delete protocol")
			}
			if failures := cl.runCleanup(scope[:1], false); len(failures) != 0 {
				t.Fatalf("confirmed bucket ancestor cleanup failed: %v", failures)
			}
			if entries := readJournalEntries(t, path); len(entries) != 2 || entries[0].VolumeFixtureKind != "role" || entries[1].VolumeFixtureKind != "policy" {
				t.Fatalf("policy test did not reach retained role and policy: %+v", entries)
			}
			policyScope := []*cleanupItem{item}
			previous := writeVolumeFixtureConsumptionJournal
			t.Cleanup(func() { writeVolumeFixtureConsumptionJournal = previous })
			if mode != "normal" {
				writeVolumeFixtureConsumptionJournal = func(root *os.Root, path string, journal report.CleanupJournal) error {
					if mode == "after install" {
						if err := previous(root, path, journal); err != nil {
							return err
						}
					}
					return errors.New("injected policy consumption durability failure")
				}
			}
			first := cl.runCleanup(policyScope, false)
			if (len(first) == 0) != (mode == "normal") {
				t.Fatalf("first policy cleanup = %v", first)
			}
			writeVolumeFixtureConsumptionJournal = previous
			stored, _, err := readRunnerJournal(path)
			wantEntries := 2
			if mode != "before install" {
				wantEntries = 1
			}
			if err != nil || len(stored.Entries) != wantEntries || stored.Entries[0].VolumeFixtureKind != "role" || mode == "before install" && !stored.Entries[1].VolumeFixtureDeleteAttempted {
				t.Fatalf("policy consumption state = %+v, err=%v", stored, err)
			}
			if mode == "normal" {
				return
			}
			if err := os.WriteFile(present, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			_ = cl.runCleanup(policyScope, false)
			log, err := os.ReadFile(calls)
			if err != nil || strings.Count(string(log), "call ram DeletePolicy") != 1 {
				t.Fatalf("replacement policy was deleted: %s %v", log, err)
			}
			if _, err := os.Stat(present); err != nil {
				t.Fatalf("replacement policy was removed: %v", err)
			}
			if mode == "before install" {
				if err := os.Remove(present); err != nil {
					t.Fatal(err)
				}
				if failures := cl.runCleanup(policyScope, false); len(failures) != 0 || len(readJournalEntries(t, path)) != 1 {
					t.Fatalf("exact policy absence did not retire finalizer: %v", failures)
				}
			}
		})
	}
}

func TestOrdinaryVolumeFixtureCleanupRetainsRAMUntilBucketConfirmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake provider uses a shell script")
	}
	for _, tc := range []struct {
		name, failed, pendingKind, preflight     string
		pendingTeam, settledTeam, skipAttachment bool
	}{
		{name: "multipart", failed: "Multipart"},
		{name: "generic bucket absence", failed: "DeleteBucket"},
		{name: "unknown bucket result", failed: "UnknownBucket"},
		{name: "empty DeleteBucket response with live bucket", failed: "EmptyBucket"},
		{name: "RAM attachment failure", failed: "DetachPolicyFromRole"},
		{name: "pending role", pendingKind: "role"},
		{name: "pending policy", pendingKind: "policy"},
		{name: "pending attachment", pendingKind: "attachment"},
		{name: "pending Team create", pendingTeam: true},
		{name: "settled Team create history", settledTeam: true},
		{name: "clean bucket"},
		{name: "fresh policy already absent", preflight: "policy_only_absent", skipAttachment: true},
		{name: "fresh attachment role already absent", preflight: "role_absent"},
		{name: "fresh attachment policy already absent", preflight: "policy_absent"},
		{name: "fresh attachment role permission denied", preflight: "role_permission"},
		{name: "fresh attachment policy unknown", preflight: "policy_unknown"},
		{name: "fresh attachment policy malformed absence", preflight: "policy_malformed"},
		{name: "fresh policy permission denied", preflight: "policy_only_permission", skipAttachment: true},
		{name: "fresh policy unknown", preflight: "policy_only_unknown", skipAttachment: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failed := tc.failed
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bin, path := filepath.Join(dir, "ecctl"), filepath.Join(dir, "cleanup.json")
			bucket := "ecctl-e2e-20261104t120000-123456abcdef"
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "native.json"))
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "aliyun.json"))
			t.Setenv("ECCTL_PROFILE", "fixture-profile")
			t.Setenv("FAKE_FIXTURE_FAILED", failed)
			t.Setenv("FAKE_FIXTURE_PREFLIGHT", tc.preflight)
			t.Setenv("FAKE_FIXTURE_CALLS", filepath.Join(dir, "calls"))
			t.Setenv("FAKE_FIXTURE_BUCKET_DELETED", filepath.Join(dir, "bucket-deleted"))
			if err := os.WriteFile(filepath.Join(dir, "aliyun.json"), []byte(`{"profiles":[{"name":"fixture-profile","mode":"AK","access_key_id":"fake-id","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_FIXTURE_CALLS"
case "$*" in
  *"configure get"*) echo '{"profile":"fixture-profile","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *"call oss ListObjects"*)
    if [ -f "$FAKE_FIXTURE_BUCKET_DELETED" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"ListObjects","code":"NoSuchBucket","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"oss","operation":"ListObjects","response":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","IsTruncated":false,"KeyCount":"0"}}';;
  *"call oss GetBucketInfo"*) echo '{"product":"oss","operation":"GetBucketInfo","region":"cn-hangzhou","response":{"Bucket":{"Name":"ecctl-e2e-20261104t120000-123456abcdef","Location":"oss-cn-hangzhou","ExtranetEndpoint":"oss-cn-hangzhou.aliyuncs.com","CreationDate":"2026-11-04T12:00:00Z","Owner":{"ID":"1234567890123456"}}}}';;
  *"call ram GetRole"*)
    if [ "$FAKE_FIXTURE_PREFLIGHT" = role_absent ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_FIXTURE_PREFLIGHT" = role_permission ]; then
      echo '{"error":{"kind":"provider","code":"Forbidden"},"actions":[{"action_name":"GetRole","code":"Forbidden","provider_verified":true}]}'
      exit 4
    fi
    if [ -f "$FAKE_FIXTURE_BUCKET_DELETED.role" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetRole","code":"EntityNotExist.Role","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetRole","region":"cn-hangzhou","response":{"RequestId":"request-get-role","Role":{"RoleId":"role-created","RoleName":"ecctl-e2e-20261104t120000-123456abcdef-role","Arn":"acs:ram::1234567890123456:role/ecctl-e2e-20261104t120000-123456abcdef-role"}}}';;
  *"call ram GetPolicy"*)
    if [ "$FAKE_FIXTURE_PREFLIGHT" = policy_only_permission ]; then
      echo '{"error":{"kind":"provider","code":"Forbidden"},"actions":[{"action_name":"GetPolicy","code":"Forbidden","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_FIXTURE_PREFLIGHT" = policy_absent ] || [ "$FAKE_FIXTURE_PREFLIGHT" = policy_only_absent ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    if [ "$FAKE_FIXTURE_PREFLIGHT" = policy_malformed ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy"}]}'
      exit 4
    fi
    if [ "$FAKE_FIXTURE_PREFLIGHT" = policy_unknown ] || [ "$FAKE_FIXTURE_PREFLIGHT" = policy_only_unknown ]; then echo '{"product":"ram","operation":"GetPolicy"}'; exit 0; fi
    if [ -f "$FAKE_FIXTURE_BUCKET_DELETED.policy" ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetPolicy","code":"EntityNotExist.Policy","provider_verified":true}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"GetPolicy","region":"cn-hangzhou","response":{"RequestId":"request-get-policy","Policy":{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom","CreateDate":"2026-11-04T12:00:00Z"}}}';;
  *"call ram ListPoliciesForRole"*)
    if [ "$FAKE_FIXTURE_FAILED" = DetachPolicyFromRole ]; then
      echo '{"product":"ram","operation":"ListPoliciesForRole","region":"cn-hangzhou","response":{"RequestId":"request-list","Policies":{"Policy":[{"PolicyName":"ecctl-e2e-20261104t120000-123456abcdef-read","PolicyType":"Custom"}]}}}'
    else
      echo '{"product":"ram","operation":"ListPoliciesForRole","region":"cn-hangzhou","response":{"RequestId":"request-list","Policies":{"Policy":[]}}}'
    fi;;
  *"call ram DetachPolicyFromRole"*)
    if [ "$FAKE_FIXTURE_FAILED" = DetachPolicyFromRole ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"DetachPolicyFromRole","code":"InvalidAccessKeyId.NotFound"}]}'
      exit 4
    fi
    echo '{"product":"ram","operation":"DetachPolicyFromRole","region":"cn-hangzhou","response":{"RequestId":"request-detach"}}';;
  *"call ram DeletePolicy"*) touch "$FAKE_FIXTURE_BUCKET_DELETED.policy"; echo '{"product":"ram","operation":"DeletePolicy","region":"cn-hangzhou","response":{"RequestId":"request-policy"}}';;
  *"call ram DeleteRole"*) touch "$FAKE_FIXTURE_BUCKET_DELETED.role"; echo '{"product":"ram","operation":"DeleteRole","region":"cn-hangzhou","response":{"RequestId":"request-role"}}';;
  *"call oss DeleteBucket"*)
    if [ "$FAKE_FIXTURE_FAILED" = Multipart ]; then
      echo '{"error":{"kind":"provider","code":"0015-00000302","message":"unfinished multipart upload"},"actions":[{"action_name":"DeleteBucket","code":"0015-00000302"}]}'
      exit 2
    fi
    if [ "$FAKE_FIXTURE_FAILED" = DeleteBucket ]; then
      echo '{"error":{"kind":"not_found","code":"NotFound","message":"ecctl-e2e-20261104t120000-123456abcdef not found"},"actions":[{"action_name":"DeleteBucket","code":"404"}]}'
      exit 4
    fi
    if [ "$FAKE_FIXTURE_FAILED" = UnknownBucket ]; then
      echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{"Code":"unknown"}}'
      exit 0
    fi
    if [ "$FAKE_FIXTURE_FAILED" = EmptyBucket ]; then
      echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}'
      exit 0
    fi
    touch "$FAKE_FIXTURE_BUCKET_DELETED"
    echo '{"product":"oss","operation":"DeleteBucket","region":"cn-hangzhou","response":{}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + filepath.Join(dir, "native.json"), "ECCTL_ALIYUN_CONFIG_PATH=" + filepath.Join(dir, "aliyun.json"), "ECCTL_PROFILE=fixture-profile"}}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			grantSyntheticVolumeFixtureScope(t, cl, bucket, "delete")
			if _, err := cl.boundVolumeFixtureConfig(context.Background(), cfg, bucket, time.Second); err != nil {
				t.Fatal(err)
			}
			commands := []struct{ kind, command string }{
				{"bucket", `ecctl call oss DeleteBucket --request '{"Bucket":"` + bucket + `"}' --region cn-hangzhou`},
				{"role", `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}'`},
				{"policy", `ecctl call ram DeletePolicy --region cn-hangzhou --request '{"PolicyName":"` + bucket + `-read"}'`},
				{"attachment", `ecctl call ram DetachPolicyFromRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role","PolicyName":"` + bucket + `-read","PolicyType":"Custom"}'`},
			}
			var scope []*cleanupItem
			for _, command := range commands {
				if tc.skipAttachment && command.kind == "attachment" {
					continue
				}
				item, err := cl.pushItemWithFixture(&scope, "stack", command.command, "primary", nil, false, false, false, nil, command.kind, nil)
				if err != nil {
					t.Fatal(err)
				}
				if command.kind == tc.pendingKind {
					break
				}
				if command.kind == "role" {
					item.volumeFixtureRoleID = "role-created"
				} else if command.kind == "policy" {
					item.volumeFixturePolicyCreateDate = "2026-11-04T12:00:00Z"
				}
				if err := cl.settleVolumeFixture(item); err != nil {
					t.Fatal(err)
				}
				if command.kind == "bucket" {
					if err := cl.recordVolumeFixtureBucketEvidence(item, "2026-11-04T12:00:00Z"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.pendingTeam || tc.settledTeam {
				journal, _, err := readRunnerJournal(path)
				if err != nil {
					t.Fatal(err)
				}
				id := ""
				if tc.settledTeam {
					id = "team-deleted"
				}
				journal.VolumeTeamCreateIDs = map[string]string{"ecctl-t-0123456789abcdef01234567": id}
				if err := writeRunnerJournal(path, journal); err != nil {
					t.Fatal(err)
				}
			}
			failures := cl.runCleanup(scope, false)
			preflightError := strings.HasSuffix(tc.preflight, "_permission") || strings.HasSuffix(tc.preflight, "_unknown") || strings.HasSuffix(tc.preflight, "_malformed")
			wantRemaining := len(scope)
			switch failed {
			case "DetachPolicyFromRole":
				wantRemaining = 3
			case "":
				if tc.pendingKind == "" && !tc.pendingTeam && !preflightError {
					wantRemaining = 0
				} else if tc.preflight != "" {
					wantRemaining = len(scope) - 1
				}
			}
			wantFailures := 0
			if failed != "" || tc.pendingKind != "" || tc.pendingTeam || preflightError {
				wantFailures = 1
			}
			if len(failures) != wantFailures {
				t.Fatalf("fixture cleanup failures = %v", failures)
			}
			entries := readJournalEntries(t, path)
			if len(entries) != wantRemaining {
				t.Fatalf("fixture cleanup consumed unconfirmed dependency: %+v", entries)
			}
			if preflightError {
				for _, entry := range entries {
					if entry.VolumeFixtureDeleteAttempted {
						t.Fatalf("unproven RAM absence spent a delete attempt: %+v", entry)
					}
				}
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if (tc.pendingKind != "" || tc.pendingTeam) && (strings.Contains(string(calls), "call oss DeleteBucket") || strings.Contains(string(calls), "call ram ")) {
				t.Fatalf("pending fixture or Team reached cleanup provider: %s", calls)
			}
			if wantRemaining == 4 && strings.Contains(string(calls), "call ram ") {
				t.Fatalf("RAM dependency deleted before bucket confirmation: %s", calls)
			}
			if wantRemaining == 3 && tc.pendingKind == "" && (!strings.Contains(string(calls), "call oss DeleteBucket") || strings.Contains(string(calls), "call ram DeletePolicy") || strings.Contains(string(calls), "call ram DeleteRole")) {
				t.Fatalf("RAM fixture order changed after bucket confirmation: %s", calls)
			}
			bucketCall, attachmentCall := strings.Index(string(calls), "call oss DeleteBucket"), strings.Index(string(calls), "call ram DetachPolicyFromRole")
			if wantRemaining == 0 {
				if bucketCall < 0 {
					t.Fatalf("bucket confirmation did not precede RAM cleanup: %s", calls)
				}
				if tc.preflight == "" && (attachmentCall < bucketCall || !strings.Contains(string(calls), "call ram DeletePolicy") || !strings.Contains(string(calls), "call ram DeleteRole")) {
					t.Fatalf("RAM dependency deleted before bucket confirmation: %s", calls)
				}
				if tc.preflight != "" {
					wantPolicyDelete := tc.preflight == "role_absent"
					wantRoleDelete := tc.preflight == "policy_absent" || tc.preflight == "policy_only_absent"
					if attachmentCall >= 0 || strings.Contains(string(calls), "call ram DeletePolicy") != wantPolicyDelete || strings.Contains(string(calls), "call ram DeleteRole") != wantRoleDelete {
						t.Fatalf("verified absence sent an unsafe RAM mutation: %s", calls)
					}
				}
			}
			if preflightError {
				if attachmentCall >= 0 || strings.Contains(string(calls), "call ram DeletePolicy") || strings.Contains(string(calls), "call ram DeleteRole") {
					t.Fatalf("unproven RAM absence reached mutation: %s", calls)
				}
			}
			if failed == "" {
				return
			}
			t.Setenv("FAKE_FIXTURE_FAILED", "")
			if failed == "Multipart" || failed == "DeleteBucket" || failed == "UnknownBucket" || failed == "EmptyBucket" {
				if failures := cl.runCleanup(scope, false); len(failures) != 1 || len(readJournalEntries(t, path)) != 4 {
					t.Fatalf("possibly started bucket delete was retried or retired: failures=%v entries=%+v", failures, readJournalEntries(t, path))
				}
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil || strings.Count(string(calls), "call oss DeleteBucket") != 1 {
					t.Fatalf("possibly started bucket delete repeated: %s %v", calls, err)
				}
				if err := os.WriteFile(filepath.Join(dir, "bucket-deleted"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if failures := cl.runCleanup(scope, false); len(failures) != 0 || len(readJournalEntries(t, path)) != 0 {
				t.Fatalf("confirmed retry did not consume fixture: failures=%v entries=%+v", failures, readJournalEntries(t, path))
			}
			if failed == "Multipart" || failed == "DeleteBucket" || failed == "UnknownBucket" || failed == "EmptyBucket" {
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil || strings.Count(string(calls), "call oss DeleteBucket") != 1 {
					t.Fatalf("read-only bucket retirement repeated deletion: %s %v", calls, err)
				}
			}
		})
	}
}
