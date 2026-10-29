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
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func quotaCleanupHarness(t *testing.T) (execpkg.Config, *cleanup, *[]*cleanupItem, map[string]any, *scenario.Suite, string) {
	t.Helper()
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalCleanupTestDir(t)
	bin := filepath.Join(dir, "ecctl")
	configureQuotaProbeFixture(t, dir, bin)
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	journal := filepath.Join(dir, "cleanup-journal.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, journal,
		report.CleanupJournal{RunID: "quota-run", ExecutionID: "quota-execution", Surface: "public"}, func(string, ...any) {})
	data := map[string]any{"run_name": "owned-quota-case"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		path, err := commandAction(command)
		if err != nil || len(path) != 3 || path[0] != "sandbox" || path[1] != "team" || path[2] != "create" {
			t.Fatalf("unexpected command before Team ownership: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
	}
	scope := new([]*cleanupItem)
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[0], time.Second); !ok {
		t.Fatalf("protected Team create failed: %+v", step)
	}
	if len(*scope) != 1 || (*scope)[0].ownedTeamID != "team-owned" || !(*scope)[0].teamCreateConfirmed || !(*scope)[0].quotaBound || cl.quotaBinding == nil || data["team_id"] != "team-owned" {
		t.Fatalf("Team ownership was not confirmed: scope=%+v data=%+v", *scope, data)
	}
	return cfg, cl, scope, data, suite, journal
}

func TestQuotaPaginationReservesTwoDistinctOwnedPairs(t *testing.T) {
	cfg, cl, scope, data, original, journalPath := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{
			"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": 32, "memory_gb": 64},
		}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, original.Steps[1], time.Second); !ok {
		t.Fatalf("first Quota update failed: %+v", step)
	}
	data["first_team_id"] = "team-owned"
	firstName := data["team_name"]
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	if data["team_name"] == firstName {
		t.Fatal("second Team reused first ownership nonce")
	}
	pagination, err := scenario.Load("../../cases/sandbox/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{
			"team": map[string]any{"id": "team-second", "status": "active"},
		}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, pagination.Steps[2], time.Second); !ok {
		t.Fatalf("second protected Team create failed: %+v", step)
	}
	(*scope)[1].quotaUpdatePending = true
	secondUpdateCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		secondUpdateCalls++
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{
			"quota": map[string]any{"id": "team-second", "team_id": "team-second", "cpu_cores": 32, "memory_gb": 64},
		}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, pagination.Steps[3], time.Second); ok || secondUpdateCalls != 0 {
		t.Fatalf("unsettled prior Quota reached provider: step=%+v calls=%d", step, secondUpdateCalls)
	}
	(*scope)[1].quotaUpdatePending = false
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, pagination.Steps[3], time.Second); !ok {
		t.Fatalf("second protected Quota update failed: %+v", step)
	}
	journal := quotaJournal(t, journalPath)
	if len(journal.Entries) != 4 || len(*scope) != 4 || journal.Entries[0].Teardown != "ecctl sandbox team delete team-owned --timeout 5m" ||
		journal.Entries[1].Teardown != "ecctl sandbox quota delete team-owned --output=json" ||
		journal.Entries[2].Teardown != "ecctl sandbox team delete team-second --timeout 5m" ||
		journal.Entries[3].Teardown != "ecctl sandbox quota delete team-second --output=json" || journal.Entries[1].QuotaUpdatePending || journal.Entries[3].QuotaUpdatePending {
		t.Fatalf("pagination ownership is not two settled pairs: %+v", journal.Entries)
	}
}

func quotaCloudSSORetryConfig(origin, token, current string) []byte {
	return []byte(fmt.Sprintf(`{"current":%q,"profiles":[{"name":"quota-test","mode":"CloudSSO","cloud_sso_sign_in_url":%q,"cloud_sso_account_id":"account-a","cloud_sso_access_config":"access-a","access_token":%q},{"name":"other","mode":"AK","access_key_id":"public-other","access_key_secret":"dummy-other"}]}`, current, origin, token))
}

func quotaCloudSSORetryHarness(t *testing.T) (execpkg.Config, *cleanup, *[]*cleanupItem, map[string]any, *scenario.Suite, string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalCleanupTestDir(t)
	config := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(config, quotaCloudSSORetryConfig("https://signin-a.example.test/login", "dummy-token-a", "quota-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_PROFILE", "quota-test")
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-native.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", config)
	for _, key := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE", "ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"} {
		t.Setenv(key, "")
	}
	probes := filepath.Join(dir, "probes")
	t.Setenv("FAKE_QUOTA_RETRY_PROBES", probes)
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) printf 'configure\n' >> "$FAKE_QUOTA_RETRY_PROBES"; echo '{"profile":"quota-test","mode":"CloudSSO"}';;
  *"call sts GetCallerIdentity"*) printf 'sts\n' >> "$FAKE_QUOTA_RETRY_PROBES"; echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	journal := filepath.Join(dir, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, journal,
		report.CleanupJournal{RunID: "quota-retry", ExecutionID: "quota-retry-execution", Surface: "public"}, func(string, ...any) {})
	data := map[string]any{"run_name": "owned-cloud-sso-retry"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	scope := new([]*cleanupItem)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[0], time.Second); !ok {
		t.Fatalf("owned Team setup failed: %+v", step)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("Quota update setup failed: %+v", step)
	}
	journalState := quotaJournal(t, journal)
	if len(journalState.Entries) != 2 || journalState.Entries[1].QuotaUpdatePending || journalState.QuotaBinding == nil || journalState.QuotaBinding.Mode != "CloudSSO" || !journalState.QuotaBinding.ResolvedMode {
		t.Fatalf("fixture lacks settled CloudSSO ownership: %+v", journalState)
	}
	return cfg, cl, scope, data, suite, journal, config, probes
}

func TestQuotaAutomaticCleanupRechecksOriginalBindingOnEveryRetry(t *testing.T) {
	previousDelays := cleanupRetryDelays
	cleanupRetryDelays = []time.Duration{0}
	defer func() { cleanupRetryDelays = previousDelays }()
	for _, tc := range []struct{ name, target, change string }{
		{"Quota origin drift", "quota", "origin"},
		{"Team origin drift", "team", "origin"},
		{"Quota account selector drift", "quota", "account"},
		{"Quota stable retry", "quota", "none"},
		{"Team stable retry", "team", "none"},
		{"Quota token renewal", "quota", "token"},
		{"Team token renewal", "team", "token"},
		{"Quota unrelated current switch", "quota", "current"},
		{"Team unrelated current switch", "team", "current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, scope, data, suite, path, config, probes := quotaCloudSSORetryHarness(t)
			if tc.target == "team" {
				cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
					if !strings.Contains(command, "sandbox quota delete team-owned") {
						t.Fatalf("wrong child completion command: %s", command)
					}
					return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
				}
				if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[4], time.Second); !ok {
					t.Fatalf("Quota child was not completed before Team retry: %+v", step)
				}
			}
			before := quotaJournal(t, path)
			probeBefore, err := os.ReadFile(probes)
			if err != nil {
				t.Fatal(err)
			}
			if tc.target == "team" && (len(before.Entries) != 1 || !(*scope)[1].done) {
				t.Fatalf("Team retry fixture retained an unfinished child: %+v", before.Entries)
			}
			mutated := false
			cl.logf = func(format string, _ ...any) {
				if !strings.HasPrefix(format, "cleanup retry ") || mutated {
					return
				}
				mutated = true
				var next []byte
				switch tc.change {
				case "origin":
					next = quotaCloudSSORetryConfig("https://signin-b.example.test/login", "dummy-token-a", "quota-test")
				case "account":
					next = bytes.Replace(quotaCloudSSORetryConfig("https://signin-a.example.test/login", "dummy-token-a", "quota-test"), []byte(`"cloud_sso_account_id":"account-a"`), []byte(`"cloud_sso_account_id":"account-b"`), 1)
				case "token":
					next = quotaCloudSSORetryConfig("https://signin-a.example.test/login", "dummy-token-b", "quota-test")
				case "current":
					next = quotaCloudSSORetryConfig("https://signin-a.example.test/login", "dummy-token-a", "other")
				case "none":
					return
				default:
					t.Fatalf("unknown retry mutation: %s", tc.change)
				}
				if err := os.WriteFile(config, next, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}
			quotaCalls, teamCalls := 0, 0
			cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				if bound.Bin != cfg.Bin || bound.Region != cfg.Region || bound.Profile != "quota-test" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity || journalfile.TemplateEnvironmentValue("ECCTL_ALIYUN_CONFIG_PATH", bound.Env) != config {
					t.Fatalf("retry lost original binary/region/profile/source/principal: %+v", bound)
				}
				switch {
				case strings.Contains(command, "sandbox quota delete team-owned"):
					quotaCalls++
					if tc.target == "quota" && quotaCalls == 1 {
						return execpkg.Result{Command: command, Started: true, Exit: 1, Stdout: `{"error":{"code":"OperationConflict"}}`}
					}
				case strings.Contains(command, "sandbox team delete team-owned"):
					teamCalls++
					if tc.target == "team" && teamCalls == 1 {
						return execpkg.Result{Command: command, Started: true, Exit: 1, Stdout: `{"error":{"code":"OperationConflict"}}`}
					}
				default:
					t.Fatalf("unexpected cleanup command: %s", command)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			cl.keep = false
			failures := cl.runCaseCleanup(*scope)
			after := quotaJournal(t, path)
			probeAfter, err := os.ReadFile(probes)
			if err != nil || !mutated {
				t.Fatalf("retry interval was not reached: failures=%v mutated=%t read=%v", failures, mutated, err)
			}
			newProbes := string(probeAfter[len(probeBefore):])
			wantProbes := 3
			if tc.target == "team" {
				wantProbes = 2
			}
			if tc.change == "origin" || tc.change == "account" {
				wantProbes = 1
				if len(failures) != 1 || !strings.Contains(failures[0], "configuration state changed") || !strings.Contains(failures[0], "prior invocation") || quotaCalls > 1 || teamCalls > 1 || !reflect.DeepEqual(after.QuotaBinding, before.QuotaBinding) {
					t.Fatalf("origin drift launched another cleanup or lost evidence: failures=%v quota=%d team=%d before=%+v after=%+v", failures, quotaCalls, teamCalls, before, after)
				}
				if tc.target == "quota" && (quotaCalls != 1 || teamCalls != 0 || len(after.Entries) != 2 || !reflect.DeepEqual(after.Entries, before.Entries) || (*scope)[0].done || (*scope)[1].done) {
					t.Fatalf("failed Quota child did not retain child-before-parent ownership: %+v", after.Entries)
				}
				if tc.target == "team" && (quotaCalls != 0 || teamCalls != 1 || len(after.Entries) != 1 || after.Entries[0].Teardown != before.Entries[0].Teardown || !after.Entries[0].QuotaBound || !after.Entries[0].TeamReplayAttempted || (*scope)[0].done) {
					t.Fatalf("failed Team parent lost its attempted finalizer: %+v", after.Entries)
				}
			} else if len(failures) != 0 || len(after.Entries) != 0 || !(*scope)[0].done || !(*scope)[1].done || tc.target == "quota" && (quotaCalls != 2 || teamCalls != 1) || tc.target == "team" && (quotaCalls != 0 || teamCalls != 2) {
				t.Fatalf("allowed retry did not finish child and parent: failures=%v quota=%d team=%d entries=%+v", failures, quotaCalls, teamCalls, after.Entries)
			}
			if newProbes != strings.Repeat("configure\nsts\n", wantProbes) {
				t.Fatalf("unexpected configure/STS subprocess count after retry: want=%d calls=%q", wantProbes, newProbes)
			}
		})
	}
}

func configureQuotaProbeFixture(t *testing.T, dir, bin string) {
	t.Helper()
	writeQuotaIdentityProbeFake(t, bin, dir)
	configureQuotaProfileFixture(t, dir)
}

// Configure only the selected dummy credential profile. Tests with their own
// provider script can share the real binding prerequisite without overwriting
// that script's command-specific behavior.
func configureQuotaProfileFixture(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_PROFILE", "quota-test")
	config := filepath.Join(dir, "ecctl.json")
	if err := os.WriteFile(config, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"public-dummy","access_key_secret":"dummy-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", config)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	for _, key := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
		t.Setenv(key, "")
	}
}

func writeQuotaIdentityProbeFake(t *testing.T, bin, home string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("quota identity probe fixture uses a shell script")
	}
	t.Setenv("HOME", home)
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
}

func quotaJournal(t *testing.T, path string) report.CleanupJournal {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestConfirmedQuotaTeamRetainsSessionBeforeFirstChildReservation(t *testing.T) {
	cfg, cl, scope, _, _, path := quotaCleanupHarness(t)
	if len(cl.quotaReservations) != 0 || len(*scope) != 1 || !(*scope)[0].teamCreateConfirmed || !(*scope)[0].quotaBound {
		t.Fatalf("fixture did not establish a pre-child owned Team: scope=%+v", *scope)
	}
	cl.keep = false
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 1, Err: errors.New("cleanup failed")}
	}
	if failures := cl.run(*scope); len(failures) == 0 || (*scope)[0].done || !cl.hasRetainedQuotaOwnership() {
		t.Fatalf("unfinished bound parent lost Session ownership: failures=%v scope=%+v", failures, *scope)
	}
	if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamCreateConfirmed || !entries[0].QuotaBound {
		t.Fatalf("retained parent lost durable evidence: %+v", entries)
	}
	session := NewSession()
	session.retainQuotaOwnership()
	suite := &scenario.Suite{Resource: "sandbox/team", Serial: true, Steps: []scenario.Step{{Name: "next Team", Run: "ecctl sandbox team create --name next"}}}
	run, err := Run(context.Background(), Options{Session: session, Suites: []*scenario.Suite{suite}, Region: cfg.Region, Surface: "public", EcctlBin: filepath.Join(filepath.Dir(cfg.Bin), "must-not-launch")})
	if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(err.Error(), "prior Quota execution retains") {
		t.Fatalf("next execution reached allocation: run=%+v err=%v", run, err)
	}
	for _, tc := range []struct {
		name string
		item cleanupItem
	}{
		{"unconfirmed", cleanupItem{quotaBound: true}},
		{"ordinary", cleanupItem{teamCreateConfirmed: true}},
		{"consumed", cleanupItem{teamCreateConfirmed: true, quotaBound: true, done: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := &cleanup{ownedTeams: []*cleanupItem{&tc.item}}
			if other.hasRetainedQuotaOwnership() {
				t.Fatal("nonretained Team fenced another execution")
			}
		})
	}
}

func TestQuotaTeamParentUsesDurableCredentialBindingAfterChildConsumption(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity probe fixture uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_PROFILE", "quota-test")
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"public-dummy"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", config)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
		t.Setenv(name, "")
	}
	t.Setenv("FAKE_QUOTA_USER", "quota-user")
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "$FAKE_QUOTA_USER";;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}
	binding, err := journalfile.CaptureQuotaBinding("quota-test", "AK", identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cleanup.json")
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.quotaBinding = binding
	team := "ecctl sandbox team delete team-owned --timeout 5m"
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, QuotaBinding: binding, Entries: []report.Resource{
		{Scope: "case", Teardown: team, RegionRole: "primary", Region: cfg.Region, ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true},
		{Scope: "stack", Teardown: "ecctl t thing delete ordinary --profile other", RegionRole: "primary", Region: cfg.Region, ExecutionID: "execution"},
	}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, automatic := range []bool{false, true} {
		called := 0
		run := func(bound execpkg.Config) error {
			called++
			if bound.Profile != "quota-test" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
				t.Errorf("Quota Team lost creation credentials: %+v", bound)
			}
			return nil
		}
		var err error
		if automatic {
			err = cl.withTemplateCleanupGuardBefore(context.Background(), "primary", true, nil, run, team)
		} else {
			err = cl.withTemplateCleanupGuard(context.Background(), "primary", run, team)
		}
		if err != nil || called != 1 {
			t.Fatalf("Quota Team bound cleanup automatic=%v calls=%d err=%v", automatic, called, err)
		}
	}
	ordinaryCalls := 0
	if err := cl.withTemplateCleanupGuard(context.Background(), "primary", func(bound execpkg.Config) error {
		ordinaryCalls++
		if bound.Profile != "" || bound.ExpectedIdentity != nil {
			t.Errorf("ordinary finalizer inherited Quota credentials: %+v", bound)
		}
		return nil
	}, journal.Entries[1].Teardown); err != nil || ordinaryCalls != 1 {
		t.Fatalf("ordinary finalizer rejected: calls=%d err=%v", ordinaryCalls, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*report.CleanupJournal)
	}{
		{"run", func(j *report.CleanupJournal) { j.RunID = "other" }},
		{"execution", func(j *report.CleanupJournal) { j.ExecutionID = "other" }},
		{"role", func(j *report.CleanupJournal) { j.RegionRole = "secondary" }},
		{"region", func(j *report.CleanupJournal) { j.Region = "cn-beijing" }},
		{"surface", func(j *report.CleanupJournal) { j.Surface = "full" }},
		{"binary", func(j *report.CleanupJournal) { j.EcctlBin = "other-bin" }},
		{"path", func(j *report.CleanupJournal) { j.JournalPath = path + "-copied" }},
		{"credential", func(j *report.CleanupJournal) {
			copy := *j.QuotaBinding
			copy.Profile = "other"
			j.QuotaBinding = &copy
		}},
	} {
		t.Run("journal-"+tc.name, func(t *testing.T) {
			changed := journal
			tc.change(&changed)
			changedBody, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, changedBody, 0o600); err != nil {
				t.Fatal(err)
			}
			called := 0
			err = cl.withTemplateCleanupGuard(context.Background(), "primary", func(execpkg.Config) error { called++; return nil }, team)
			if err == nil || called != 0 {
				t.Fatalf("changed %s journal reached Team provider: calls=%d err=%v", tc.name, called, err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, drift := range []string{"config", "principal"} {
		if drift == "config" {
			if err := os.WriteFile(config, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"changed-public-id"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(config, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"public-dummy"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_QUOTA_USER", "different-user")
		}
		called := 0
		err := cl.withTemplateCleanupGuard(context.Background(), "primary", func(execpkg.Config) error { called++; return nil }, team)
		if err == nil || called != 0 {
			t.Fatalf("%s drift reached Team provider: calls=%d err=%v", drift, called, err)
		}
		retained, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(retained, body) {
			t.Fatalf("%s drift changed retained journal: %v", drift, readErr)
		}
	}
}

func TestQuotaRuntimeCleanupKeepsAliyunProfileAfterNativeCurrentCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity probe fixture uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	t.Setenv("HOME", dir)
	native := filepath.Join(dir, "ecctl.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	t.Setenv("ECCTL_CONFIG_PATH", native)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
	for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH", "ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
		t.Setenv(name, "")
	}
	if err := os.WriteFile(aliyun, []byte(`{"current":"profile-a","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"public-a"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := journalfile.CaptureQuotaBinding("profile-a", "AK", identity, nil)
	if err != nil || binding.ConfigState["ecctl"].Exists {
		t.Fatalf("initial native config was not absent: binding=%+v err=%v", binding, err)
	}
	if err := os.WriteFile(native, []byte(`{"current":"profile-b","profiles":[{"name":"default","output_format":"json"},{"name":"profile-b","output_format":"json"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"--profile profile-a"*) ;;
  *) exit 90;;
esac
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	path := filepath.Join(dir, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.quotaBinding = binding
	command := "ecctl sandbox team delete team-owned --timeout 5m"
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: cfg.Region, Surface: "public", EcctlBin: bin, QuotaBinding: binding, Entries: []report.Resource{{Scope: "case", Teardown: command, RegionRole: "primary", Region: cfg.Region, ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true}}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, automatic := range []bool{false, true} {
		called := 0
		use := func(bound execpkg.Config) error {
			called++
			if bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
				t.Errorf("runtime cleanup lost Aliyun A identity: %+v", bound)
			}
			return nil
		}
		var err error
		if automatic {
			err = cl.withTemplateCleanupGuardBefore(context.Background(), "primary", true, nil, use, command)
		} else {
			err = cl.withTemplateCleanupGuard(context.Background(), "primary", use, command)
		}
		if err != nil || called != 1 {
			t.Fatalf("native current switch blocked runtime cleanup automatic=%v calls=%d err=%v", automatic, called, err)
		}
	}
}

func TestQuotaReservationRejectsReplacedRememberedParentBeforeProvider(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	parent := filepath.Dir(path)
	moved := parent + "-original"
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := filepath.Glob(path + ".team-create-*.json")
	if err != nil || len(intents) != 1 {
		t.Fatalf("expected retained Team intent: %v %v", intents, err)
	}
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok || providerCalls != 0 || !strings.Contains(step.Error, "parent") || !bytes.Equal(before, after) || len(*scope) != 1 {
		t.Fatalf("replaced Quota parent crossed provider boundary: step=%+v calls=%d scope=%+v", step, providerCalls, *scope)
	}
	if _, err := os.Stat(filepath.Join(moved, filepath.Base(intents[0]))); err != nil {
		t.Fatalf("original Team intent was lost: %v", err)
	}
}

func TestExplicitProtectedDeleteRejectsLostJournalParentBeforeCredentialProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential probe fixture uses a shell script")
	}
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --timeout 5m",
		"ecctl sandbox quota delete team-owned --output=json",
	} {
		for _, replaced := range []bool{false, true} {
			name := command + "/missing"
			if replaced {
				name = command + "/same-path-copy"
			}
			t.Run(name, func(t *testing.T) {
				for _, tokenName := range []string{"ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN"} {
					t.Setenv(tokenName, "")
				}
				dir := canonicalCleanupTestDir(t)
				configureQuotaProfileFixture(t, dir)
				parent := filepath.Join(dir, "journals")
				journalPath := filepath.Join(parent, "cleanup.json")
				bin, logPath := filepath.Join(dir, "ecctl"), filepath.Join(dir, "probe-calls")
				if err := os.WriteFile(bin, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_QUOTA_GUARD_LOG"
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
					t.Fatal(err)
				}
				env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + filepath.Join(dir, "ecctl.json"), "ECCTL_ALIYUN_CONFIG_PATH=" + filepath.Join(dir, "missing-aliyun.json"), "FAKE_QUOTA_GUARD_LOG=" + logPath}
				identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}
				binding, err := journalfile.CaptureQuotaBinding("quota-test", "AK", identity, env)
				if err != nil {
					t.Fatal(err)
				}
				binding.ResolvedMode = true
				cfg := execpkg.Config{Bin: bin, Env: env, Region: "cn-hangzhou"}
				cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, journalPath,
					report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
				cl.quotaBinding = binding
				var scope []*cleanupItem
				team := "ecctl sandbox team delete team-owned --timeout 5m"
				if err := cl.push(&scope, "sandbox/team", team, "primary", nil); err != nil {
					t.Fatal(err)
				}
				journal, _, err := readRunnerJournal(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				// The real Team reservation anchors the journal before protected ownership is recorded.
				journal.JournalPath = journalPath
				journal.Entries[0].TeamCreateConfirmed = true
				journal.Entries[0].QuotaBound = true
				journal.Entries = append(journal.Entries, report.Resource{
					Scope: "sandbox/quota", Teardown: "ecctl sandbox quota delete team-owned --output=json",
					RegionRole: "primary", Region: cfg.Region, ExecutionID: "execution", QuotaBound: true,
				})
				body, err := json.Marshal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(journalPath, body, 0o600); err != nil {
					t.Fatal(err)
				}
				moved := parent + "-original"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
				if replaced {
					if err := os.Mkdir(parent, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(journalPath, body, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				deleteCalls := 0
				run := func(execpkg.Config) error { deleteCalls++; return nil }
				if err := cl.withTemplateCleanupGuard(context.Background(), "primary", run, command); err == nil || deleteCalls != 0 {
					t.Fatalf("lost parent admitted explicit delete: calls=%d err=%v", deleteCalls, err)
				}
				if calls, err := os.ReadFile(logPath); !os.IsNotExist(err) {
					t.Fatalf("lost parent reached configure/STS: calls=%q err=%v", calls, err)
				}
				if _, err := os.Stat(journalPath + ".replay.lock"); !os.IsNotExist(err) {
					t.Fatalf("lost parent received a new replay lock: %v", err)
				}
				if replaced {
					current, err := os.ReadFile(journalPath)
					if err != nil || !bytes.Equal(current, body) {
						t.Fatalf("replacement journal was modified: %v", err)
					}
					if err := os.RemoveAll(parent); err != nil {
						t.Fatal(err)
					}
				} else if _, err := os.Stat(parent); !os.IsNotExist(err) {
					t.Fatalf("missing parent was recreated: %v", err)
				}
				if err := os.Rename(moved, parent); err != nil {
					t.Fatal(err)
				}
				if err := cl.withTemplateCleanupGuard(context.Background(), "primary", run, command); err != nil || deleteCalls != 1 {
					t.Fatalf("restored original parent rejected normal guarded delete: calls=%d err=%v", deleteCalls, err)
				}
				calls, err := os.ReadFile(logPath)
				if err != nil || strings.Count(string(calls), "configure get") != 1 || strings.Count(string(calls), "GetCallerIdentity") != 1 {
					t.Fatalf("restored parent did not verify credentials before delete: calls=%q err=%v", calls, err)
				}
			})
		}
	}
}

func rawTeamDeleteResult(command, region, code string) execpkg.Result {
	response := map[string]any{"code": code, "requestId": "req-delete"}
	payload := map[string]any{"product": "fcsandbox", "operation": "DeleteTeam", "region": region, "response": response}
	body, _ := json.Marshal(payload)
	return execpkg.Result{Command: command, Started: true, Exit: 0, Stdout: string(body), JSON: payload}
}

func TestConcurrentRunCannotAllocateIntoQuotaJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, stack := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared-stack=%t", stack), func(t *testing.T) {
			dir := t.TempDir()
			configureQuotaProfileFixture(t, dir)
			fake := filepath.Join(dir, "ecctl")
			allocations := filepath.Join(dir, "allocations")
			t.Setenv("FAKE_ALLOCATIONS", allocations)
			script := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
case "$*" in
  *"sandbox team create"*) echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox quota update"*) echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}';;
  *"test resource create"*|*"test stack create"*)
    echo allocation >> "$FAKE_ALLOCATIONS"
    echo '{"resource":{"id":"resource-other"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			loaded, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(dir, "cleanup.json")
			base := Options{RunName: "same-run", RunID: "same-label", ExecutionID: "execution-01", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, Keep: true, StepTimeout: 5 * time.Second}
			quotaReached := make(chan struct{})
			releaseQuota := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseQuota) }) }
			t.Cleanup(release)
			previous := quotaBeforeReplayLock
			quotaBeforeReplayLock = func() { close(quotaReached); <-releaseQuota }
			t.Cleanup(func() { quotaBeforeReplayLock = previous })
			type outcome struct {
				run *report.Run
				err error
			}
			firstDone := make(chan outcome, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			first := base
			first.Suites = []*scenario.Suite{{Resource: "sandbox/quota", Serial: true, Steps: loaded.Steps[:2]}}
			go func() {
				run, err := Run(ctx, first)
				firstDone <- outcome{run: run, err: err}
			}()
			select {
			case <-quotaReached:
			case <-ctx.Done():
				release()
				<-firstDone
				t.Fatal("first Run did not reach quota admission")
			}
			second := base
			if stack {
				fixture := filepath.Join(dir, "stack.yaml")
				body := "provision:\n  - id: other\n    resource: test/resource\n    run: ecctl test stack create\n    at: $.resource\n    capture: {resource_id: id}\n    teardown: ecctl test stack delete {{.resource_id}}\n"
				if err := os.WriteFile(fixture, []byte(body), 0o600); err != nil {
					release()
					<-firstDone
					t.Fatal(err)
				}
				second.StackFile = fixture
				second.Suites = []*scenario.Suite{{Resource: "test/resource", Needs: []string{"other"}, Steps: []scenario.Step{{Name: "read", Run: "ecctl test resource list"}}}}
			} else {
				second.Suites = []*scenario.Suite{{Resource: "test/resource", Steps: []scenario.Step{{Name: "allocate", Run: "ecctl test resource create", At: "$.resource", Capture: map[string]string{"resource_id": "id"}, Teardown: "ecctl test resource delete {{.resource_id}}"}}}}
			}
			secondRun, secondErr := Run(ctx, second)
			release()
			firstResult := <-firstDone
			if secondErr == nil || !strings.Contains(secondErr.Error(), "in use by another run") || secondRun == nil || len(secondRun.Cases) != 1 || secondRun.Cases[0].Resource != "setup" {
				t.Fatalf("concurrent Run reached allocation admission: run=%+v err=%v", secondRun, secondErr)
			}
			if _, err := os.Stat(allocations); !os.IsNotExist(err) {
				t.Fatalf("second Run launched allocation provider: %v", err)
			}
			if firstResult.err != nil || firstResult.run == nil || firstResult.run.Summary.Failed != 0 {
				t.Fatalf("first Run failed after rejecting overlapping allocation: run=%+v err=%v", firstResult.run, firstResult.err)
			}
		})
	}
}

func TestQuotaRunRejectedWhileOtherRunAllocationIsUnregistered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, stack := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared-stack=%t", stack), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			fake := filepath.Join(dir, "ecctl")
			started := filepath.Join(dir, "allocation-started")
			release := filepath.Join(dir, "release-allocation")
			quotaProvider := filepath.Join(dir, "quota-provider")
			t.Setenv("FAKE_STARTED", started)
			t.Setenv("FAKE_RELEASE", release)
			t.Setenv("FAKE_QUOTA_PROVIDER", quotaProvider)
			script := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
case "$*" in
  *"test resource create"*|*"test stack create"*)
    echo started > "$FAKE_STARTED"
    while [ ! -f "$FAKE_RELEASE" ]; do sleep 0.02; done
    echo '{"resource":{"id":"resource-other"}}';;
  *"sandbox team create"*)
    echo team >> "$FAKE_QUOTA_PROVIDER"
    echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox quota update"*)
    echo quota >> "$FAKE_QUOTA_PROVIDER"
    echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}';;
  *"test resource list"*) echo '{"resource":{"id":"resource-other"}}';;
  *) exit 90;;
esac
`
			if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			loaded, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(dir, "cleanup.json")
			base := Options{RunName: "same-run", RunID: "same-label", ExecutionID: "execution-01", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, Keep: true, StepTimeout: 10 * time.Second}
			ordinary := base
			if stack {
				fixture := filepath.Join(dir, "stack.yaml")
				body := "provision:\n  - id: other\n    resource: test/resource\n    run: ecctl test stack create\n    at: $.resource\n    capture: {resource_id: id}\n    teardown: ecctl test stack delete {{.resource_id}}\n"
				if err := os.WriteFile(fixture, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				ordinary.StackFile = fixture
				ordinary.Suites = []*scenario.Suite{{Resource: "test/resource", Needs: []string{"other"}, Steps: []scenario.Step{{Name: "read", Run: "ecctl test resource list"}}}}
			} else {
				ordinary.Suites = []*scenario.Suite{{Resource: "test/resource", Steps: []scenario.Step{{Name: "allocate", Run: "ecctl test resource create", At: "$.resource", Capture: map[string]string{"resource_id": "id"}, Teardown: "ecctl test resource delete {{.resource_id}}"}}}}
			}
			type outcome struct {
				run *report.Run
				err error
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			ordinaryDone := make(chan outcome, 1)
			go func() {
				run, err := Run(ctx, ordinary)
				ordinaryDone <- outcome{run: run, err: err}
			}()
			wait := time.NewTicker(10 * time.Millisecond)
			defer wait.Stop()
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				select {
				case <-wait.C:
				case <-ctx.Done():
					_ = os.WriteFile(release, []byte("go"), 0o600)
					<-ordinaryDone
					t.Fatal("ordinary allocation did not start")
				}
			}
			quota := base
			if stack {
				alias := filepath.Join(dir, "journal-alias")
				if err := os.Symlink(dir, alias); err != nil {
					_ = os.WriteFile(release, []byte("go"), 0o600)
					<-ordinaryDone
					t.Fatal(err)
				}
				quota.CleanupJournal = filepath.Join(alias, "cleanup.json")
			}
			quota.Suites = []*scenario.Suite{{Resource: "sandbox/quota", Serial: true, Steps: loaded.Steps[:2]}}
			quotaRun, quotaErr := Run(ctx, quota)
			if err := os.WriteFile(release, []byte("go"), 0o600); err != nil {
				t.Fatal(err)
			}
			ordinaryResult := <-ordinaryDone
			if quotaErr == nil || !strings.Contains(quotaErr.Error(), "in use by another run") || quotaRun == nil || len(quotaRun.Cases) != 1 || quotaRun.Cases[0].Resource != "setup" {
				t.Fatalf("quota Run reached admission during unregistered allocation: run=%+v err=%v", quotaRun, quotaErr)
			}
			if _, err := os.Stat(quotaProvider); !os.IsNotExist(err) {
				t.Fatalf("quota Run launched provider before admission: %v", err)
			}
			if ordinaryResult.err != nil || ordinaryResult.run == nil || ordinaryResult.run.Summary.Failed != 0 {
				t.Fatalf("ordinary Run failed after lease release: run=%+v err=%v", ordinaryResult.run, ordinaryResult.err)
			}
			entries := quotaJournal(t, journal).Entries
			if len(entries) != 1 || !strings.Contains(entries[0].Teardown, "resource-other") {
				t.Fatalf("ordinary finalizer was not durably registered: %+v", entries)
			}
		})
	}
}

func TestQuotaUpdateRefusesPreexistingUnrelatedCleanupEntry(t *testing.T) {
	for _, tc := range []struct {
		name, command, rejection string
		durableOnly              bool
		entryCount               int
	}{
		{"registered replayable", "ecctl vpc delete vpc-prior", "unrelated active cleanup entry", false, 2},
		{"durable replayable", "ecctl vpc delete vpc-prior", "unexpected active cleanup entries", true, 2},
		{"runtime-only", "ecctl vpc detach vpc-prior", "unrelated active cleanup entry", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, teamScope, data, suite, path := quotaCleanupHarness(t)
			var otherScope []*cleanupItem
			if err := cl.push(&otherScope, "prior VPC", tc.command, "primary", nil); err != nil {
				t.Fatal(err)
			}
			if tc.durableOnly {
				cl.registeredItems = cl.registeredItems[:1] // Exercise the journal fence without the other in-memory registration.
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("Quota update launched with another active cleanup entry: %s", command)
				return execpkg.Result{}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, teamScope, data, suite.Steps[1], time.Second)
			entries := quotaJournal(t, path).Entries
			if ok || !strings.Contains(step.Error, tc.rejection) || len(*teamScope) != 1 || len(otherScope) != 1 || len(entries) != tc.entryCount || entries[0].Teardown != "ecctl sandbox team delete team-owned --timeout 5m" || entries[0].QuotaUpdatePending || cl.hasPendingQuotaUpdate() {
				t.Fatalf("Quota preflight stranded earlier resources: step=%+v team=%+v other=%+v entries=%+v", step, *teamScope, otherScope, entries)
			}
			cl.keep = false
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if command != tc.command && !strings.Contains(command, "sandbox team delete team-owned") {
					t.Fatalf("unexpected cleanup after rejected Quota update: %s", command)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			if failures := cl.run(otherScope); len(failures) != 0 {
				t.Fatalf("earlier VPC cleanup blocked: %v", failures)
			}
			if failures := cl.run(*teamScope); len(failures) != 0 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("Team cleanup blocked after rejected Quota update: %v", failures)
			}
		})
	}
}

func TestConfirmedTeamRegistrationWriteFailureRepairsBeforeCleanup(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprintf("installed=%t", installed), func(t *testing.T) {
			suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			dir := canonicalCleanupTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			data := map[string]any{"run_name": "case"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			previous := writeTeamRegistrationJournal
			t.Cleanup(func() { writeTeamRegistrationJournal = previous })
			writeTeamRegistrationJournal = func(path string, journal report.CleanupJournal) error {
				if installed {
					if err := previous(path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected Team registration failure")
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
			}
			var scope []*cleanupItem
			step, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, suite.Steps[0], time.Second)
			if ok || !strings.Contains(step.Error, "injected Team registration failure") || len(scope) != 1 || !scope[0].teamRegistrationFailed {
				t.Fatalf("confirmed Team was lost: step=%+v scope=%+v", step, scope)
			}
			cl.keep = false
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if !strings.Contains(command, "sandbox team delete team-owned") || len(quotaJournal(t, path).Entries) != 1 {
					t.Fatalf("Team delete before exact repair: %s", command)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			if failures := cl.runCaseCleanup(scope); len(failures) != 0 || calls != 0 || len(cl.deferredScopes) != 1 {
				t.Fatalf("uncertain Team registration did not defer cleanup: failures=%v calls=%d", failures, calls)
			}
			writeTeamRegistrationJournal = previous
			if failures := cl.runDeferredCleanup(); len(failures) != 0 || calls != 1 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("Team cleanup failed after registration repair: failures=%v calls=%d journal=%+v", failures, calls, quotaJournal(t, path))
			}
		})
	}
}

func TestFailedFirstQuotaTeamRegistrationRebindsAfterCurrentSwitch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity probe fixture uses a shell script")
	}
	for _, mode := range []string{"automatic", "explicit", "identity-drift", "auth-drift"} {
		t.Run(mode, func(t *testing.T) {
			suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			dir := canonicalCleanupTestDir(t)
			native := filepath.Join(dir, "ecctl.json")
			aliyun := filepath.Join(dir, "missing-aliyun.json")
			t.Setenv("HOME", dir)
			t.Setenv("ECCTL_CONFIG_PATH", native)
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH", "ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
				t.Setenv(name, "")
			}
			configA := []byte(`{"current":"profile-a","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"public-a"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`)
			if err := os.WriteFile(native, configA, 0o600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "ecctl")
			t.Setenv("FAKE_QUOTA_USER", "user-a")
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "$FAKE_QUOTA_USER";;
  *) exit 91;;
esac
`), 0o755); err != nil {
				t.Fatal(err)
			}
			identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
			path := filepath.Join(dir, "cleanup.json")
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			data := map[string]any{"run_name": "case"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			previous := writeTeamRegistrationJournal
			t.Cleanup(func() { writeTeamRegistrationJournal = previous })
			writeTeamRegistrationJournal = func(string, report.CleanupJournal) error {
				return errors.New("injected first Team registration failure")
			}
			createCalls := 0
			cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				createCalls++
				if bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
					t.Fatalf("Team create lost selected A identity: profile=%q pin=%v", bound.Profile, bound.ExpectedIdentity)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
			}
			var scope []*cleanupItem
			step, ok := runStep(context.Background(), Options{quotaLifecycle: true}, cfg, cl, &scope, data, suite.Steps[0], time.Second)
			if ok || !strings.Contains(step.Error, "injected first Team registration failure") || createCalls != 1 || len(scope) != 1 || !scope[0].teamRegistrationFailed {
				t.Fatalf("first append failure not retained: step=%+v calls=%d scope=%+v", step, createCalls, scope)
			}
			before := quotaJournal(t, path)
			if before.QuotaBinding == nil || len(before.Entries) != 0 {
				t.Fatalf("test did not establish empty pre-create journal: %+v", before)
			}
			writeTeamRegistrationJournal = previous
			configB := []byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"public-a"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`)
			if mode == "auth-drift" {
				configB = []byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"changed"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`)
			}
			if err := os.WriteFile(native, configB, 0o600); err != nil {
				t.Fatal(err)
			}
			if mode == "identity-drift" {
				t.Setenv("FAKE_QUOTA_USER", "user-b")
			}
			cl.keep = false
			deleteCalls := 0
			cl.runCommand = func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				deleteCalls++
				if bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity || !strings.Contains(command, "sandbox team delete team-owned") {
					t.Fatalf("repaired Team delete lost creation binding: command=%s bound=%+v", command, bound)
				}
				if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].QuotaBound || !entries[0].TeamReplayAttempted {
					t.Fatalf("Team delete preceded exact repaired ownership: %+v", entries)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			if mode == "explicit" {
				step, ok = runStep(context.Background(), Options{}, cfg, cl, &scope, data, scenario.Step{Name: "delete", Run: "ecctl sandbox team delete team-owned --timeout 5m"}, time.Second)
				if !ok || deleteCalls != 1 || len(quotaJournal(t, path).Entries) != 0 {
					t.Fatalf("explicit repaired Team delete failed: step=%+v calls=%d", step, deleteCalls)
				}
				return
			}
			failures := cl.run(scope)
			if mode == "automatic" {
				if len(failures) != 0 || deleteCalls != 1 || len(quotaJournal(t, path).Entries) != 0 {
					t.Fatalf("automatic repaired Team delete failed: failures=%v calls=%d", failures, deleteCalls)
				}
				return
			}
			retained := quotaJournal(t, path)
			if len(failures) == 0 || deleteCalls != 0 || scope[0].done || len(retained.Entries) != 1 || !retained.Entries[0].TeamCreateConfirmed || !retained.Entries[0].QuotaBound || retained.Entries[0].Teardown != scope[0].cmd || retained.QuotaBinding == nil || retained.QuotaBinding.Profile != "profile-a" || retained.QuotaBinding.Identity != identity {
				t.Fatalf("credential drift consumed ownership: failures=%v calls=%d scope=%+v journal=%+v", failures, deleteCalls, scope, retained)
			}
		})
	}
}

func TestParallelTeamCreateDefersOtherScopeCleanupUntilSettlement(t *testing.T) {
	_, cl, scope, _, _, path := quotaCleanupHarness(t)
	cl.keep = false
	cl.teamCreatesPending = 1 // a second nonserial case is still creating its Team
	calls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		calls++
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || calls != 0 || !(*scope)[0].deferred || len(cl.deferredScopes) != 1 {
		t.Fatalf("parallel Team create was not deferred: failures=%v calls=%d scope=%+v", failures, calls, *scope)
	}
	cl.teamCreatesPending = 0
	if failures := cl.runDeferredCleanup(); len(failures) != 0 || calls != 1 || len(quotaJournal(t, path).Entries) != 0 {
		t.Fatalf("settled parallel Team cleanup failed: failures=%v calls=%d entries=%+v", failures, calls, quotaJournal(t, path).Entries)
	}
}

func TestSandboxTeamCreateUsesOneFinalizerGrammarBeforeAndAfterLaunch(t *testing.T) {
	for _, tc := range []struct {
		name, teardown string
		accepted       bool
	}{
		{"canonical equivalent timeout", "ecctl sandbox team delete {{.team_id}} --timeout 300s", true},
		{"alias short timeout and JSON", "ecctl sbx team delete {{.team_id}} --output=json --timeout=1m", true},
		{"inline timeout before ID", "ecctl sandbox team delete --timeout=1m {{.team_id}}", true},
		{"split timeout before ID", "ecctl sandbox team delete --timeout 1m {{.team_id}}", true},
		{"unsupported profile", "ecctl sandbox team delete {{.team_id}} --timeout 5m --profile foreign", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			path := filepath.Join(dir, "journal.json")
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			data := map[string]any{"run_name": "owned-quota"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			create := suite.Steps[0]
			create.Teardown = tc.teardown
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
			}
			var scope []*cleanupItem
			step, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, create, time.Second)
			if !tc.accepted {
				if ok || calls != 0 || len(scope) != 0 {
					t.Fatalf("invalid Team teardown reached CreateTeam: step=%+v calls=%d scope=%+v", step, calls, scope)
				}
				return
			}
			entries := quotaJournal(t, path).Entries
			if !ok || calls != 1 || len(scope) != 1 || len(entries) != 1 || !entries[0].TeamCreateConfirmed || entries[0].Teardown != scope[0].cmd {
				t.Fatalf("validated Team finalizer was not retained: step=%+v calls=%d scope=%+v entries=%+v", step, calls, scope, entries)
			}
			if target, err := journalfile.TeamDeleteTarget(entries[0].Teardown); err != nil || target != "team-owned" {
				t.Fatalf("journal lost owned Team target: target=%q err=%v", target, err)
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
			}
			if update, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, suite.Steps[1], time.Second); !ok {
				t.Fatalf("accepted Team spelling cannot reserve Quota: %+v", update)
			}
		})
	}
}

func TestProtectedTeamTimeoutBeforeIDRunsAutomaticCleanup(t *testing.T) {
	for _, teardown := range []string{
		"ecctl sandbox team delete --timeout=1m {{.team_id}}",
		"ecctl sandbox team delete --timeout 1m {{.team_id}}",
	} {
		t.Run(teardown, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			path := filepath.Join(dir, "journal.json")
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			data := map[string]any{"run_name": "owned-team"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			create := suite.Steps[0]
			create.Teardown = teardown
			deletes := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if isTeamCreate(command) {
					return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
				}
				deletes++
				if target, err := journalfile.TeamDeleteTarget(command); err != nil || target != "team-owned" {
					t.Fatalf("automatic cleanup changed the accepted finalizer: %q target=%q err=%v", command, target, err)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			var scope []*cleanupItem
			if step, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, create, time.Second); !ok {
				t.Fatalf("protected Team create failed: %+v", step)
			}
			if failures := cl.runCaseCleanup(scope); len(failures) != 0 || deletes != 1 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("accepted Team finalizer did not clean up: failures=%v deletes=%d entries=%+v", failures, deletes, quotaJournal(t, path).Entries)
			}
		})
	}
}

func TestReaderTeamTeardownChecksCoLocatedRoleOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, secondaryRegion, teardown string
		blocked                         bool
		durableOnly                     bool
	}{
		{"same physical region", "cn-hangzhou", "", true, false},
		{"same region retained journal", "cn-hangzhou", "", true, true},
		{"distinct physical region", "cn-beijing", "", false, false},
		{"distinct matching selector", "cn-beijing", " --region cn-beijing", false, false},
		{"secondary retargets primary", "cn-beijing", " --region cn-hangzhou", true, false},
		{"secondary retargets durable primary", "cn-beijing", " --region cn-hangzhou", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, scope, data, suite, _ := quotaCleanupHarness(t)
			cl.execCfg["secondary"] = execpkg.Config{Bin: cfg.Bin, Region: tc.secondaryRegion, Env: cfg.Env}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
			}
			if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
				t.Fatalf("settled Quota prerequisite failed: %+v", step)
			}
			if tc.durableOnly {
				cl.ownedTeams = nil // Exercise the durable cross-role fence without an in-memory owner.
			}
			reader := scenario.Step{Name: "reader", Run: "ecctl sandbox team get team-owned --output=json", Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m" + tc.teardown, TeardownRegion: "secondary"}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned"}}}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, reader, time.Second)
			if tc.blocked {
				if ok || len(*scope) != 2 || len(cl.manifest) != 2 {
					t.Fatalf("co-located reader registered Team delete: step=%+v scope=%+v", step, *scope)
				}
				return
			}
			if !ok || len(*scope) != 3 {
				t.Fatalf("distinct-region reader was rejected: step=%+v scope=%+v", step, *scope)
			}
			cl.keep = false
			deletes := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				deletes++
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			if failures := cl.runCleanup([]*cleanupItem{(*scope)[2]}, false); len(failures) != 0 || deletes != 1 {
				t.Fatalf("distinct-region Team cleanup failed: failures=%v deletes=%d", failures, deletes)
			}
		})
	}
}

func TestOrdinaryTeamMatchingRegionJournalReplays(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses shell")
	}
	for _, command := range []string{
		"ecctl sandbox team delete independent-team --timeout 5m --region cn-hangzhou",
		"ecctl sandbox team delete independent-team --timeout 5m --lang en --output=json",
		"ecctl --region cn-hangzhou sandbox team delete independent-team --lang en",
		"ecctl sandbox --lang=en team --region=cn-hangzhou delete independent-team",
	} {
		t.Run(command, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			path, bin := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
			logPath := filepath.Join(dir, "calls")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$*\" >> \"$TEAM_CALL_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TEAM_CALL_LOG", logPath)
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			var scope []*cleanupItem
			if err := cl.push(&scope, "case", command, "primary", nil); err != nil {
				t.Fatalf("ordinary Team command was not journaled: %v", err)
			}
			if entries := quotaJournal(t, path).Entries; len(entries) != 1 || entries[0].Teardown != command {
				t.Fatalf("ordinary Team producer changed command: %+v", entries)
			}
			result, err := sweeper.ReplayJournalWithOptions(context.Background(), path, sweeper.ReplayOptions{Config: cfg, RunID: "run", Surface: "public"})
			if err != nil || result.Errors != 0 || result.Deleted != 1 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("ordinary Team replay failed: result=%+v err=%v", result, err)
			}
			if calls, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(calls), "independent-team") || !strings.Contains(string(calls), "cn-hangzhou") {
				t.Fatalf("replay command changed: calls=%q err=%v", calls, err)
			}
		})
	}
}

func TestOrdinaryTeamGlobalFlagsReachExplicitSubprocess(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox team delete independent-team --lang en --output=json",
		"ecctl --region cn-hangzhou sandbox --lang en team delete independent-team",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, actual string) execpkg.Result {
				calls++
				if actual != command {
					t.Fatalf("ordinary Team command was rewritten: %q", actual)
				}
				return execpkg.Result{Command: actual, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "ordinary delete", Run: command}, time.Second)
			if !ok || step.Status != report.StatusPass || calls != 1 || len(quotaJournal(t, path).Entries) != 1 {
				t.Fatalf("ordinary Team delete did not reach provider: step=%+v calls=%d", step, calls)
			}
		})
	}
}

func TestOrdinaryTeamRegionFormsFailBeforeRegistration(t *testing.T) {
	for _, suffix := range []string{"--region cn-beijing", "--region=", "--region cn-hangzhou --region cn-hangzhou", "--profile foreign"} {
		t.Run(suffix, func(t *testing.T) {
			_, cl, scope, _, _, path := quotaCleanupHarness(t)
			before := quotaJournal(t, path)
			if err := cl.push(scope, "ordinary", "ecctl sandbox team delete independent-team --timeout 5m "+suffix, "primary", nil); err == nil {
				t.Fatal("unsupported ordinary Team selector was journaled")
			}
			if after := quotaJournal(t, path); len(after.Entries) != len(before.Entries) || len(*scope) != 1 {
				t.Fatalf("rejected selector changed ownership: %+v", after)
			}
		})
	}
}

func TestTeamDeleteSelectorConflictStopsExplicitSubprocess(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --timeout 5m --region cn-beijing",
		"ecctl --region cn-beijing sandbox team delete team-owned --timeout 5m",
		"ecctl sandbox team --page=2 delete team-owned --timeout 5m",
		"ecctl sandbox team --future-page=2 delete team-owned --timeout 5m",
		`ecctl call fcsandbox ' DeleteTeam' --request '{"teamID":"team-owned"}' --region cn-beijing`,
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, actual string) execpkg.Result {
				t.Fatalf("conflicting selector launched: %s", actual)
				return execpkg.Result{}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "conflict", Run: command}, time.Second)
			if ok || step.Error == "" || len(quotaJournal(t, path).Entries) != 1 || quotaJournal(t, path).Entries[0].TeamReplayAttempted {
				t.Fatalf("conflicting selector changed owned Team: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
			}
		})
	}
}

func TestEarlierSecondaryTeamFinalizerCannotDeleteLaterPrimaryOwner(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	path := filepath.Join(dir, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg, "secondary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var earlier []*cleanupItem
	if err := cl.push(&earlier, "earlier-reader", "ecctl sandbox team delete team-owned --timeout 5m", "secondary", nil); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"run_name": "later-owned-team"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
	}
	var later []*cleanupItem
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, &later, data, suite.Steps[0], time.Second); !ok {
		t.Fatalf("later protected Team create failed: %+v", step)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("earlier secondary finalizer deleted protected primary Team: %s", command)
		return execpkg.Result{}
	}
	if failures := cl.runCaseCleanup(earlier); len(failures) != 1 || !strings.Contains(failures[0], "co-located region role") {
		t.Fatalf("earlier secondary finalizer crossed ownership fence: %v", failures)
	}
	if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamCreateConfirmed || entries[0].TeamReplayAttempted {
		t.Fatalf("earlier secondary cleanup changed primary ownership: %+v", entries)
	}
}

func TestCleanupRetryKeepsProviderResultWhenRequestedJournalRebinds(t *testing.T) {
	_, cl, scope, data, suite, path := quotaCleanupHarness(t)
	alias := filepath.Join(t.TempDir(), "requested")
	if err := os.Symlink(filepath.Dir(path), alias); err != nil {
		t.Fatal(err)
	}
	cl.requestedJournal = filepath.Join(alias, filepath.Base(path))
	cl.journalPaths = map[string]string{"primary": path}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cl.execCfg["primary"], cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("settled Quota prerequisite failed: %+v", step)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	previousDelays := cleanupRetryDelays
	cleanupRetryDelays = []time.Duration{0}
	defer func() { cleanupRetryDelays = previousDelays }()
	cl.keep = false
	var logs []string
	cl.logf = func(format string, values ...any) { logs = append(logs, fmt.Sprintf(format, values...)) }
	calls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		calls++
		if calls != 1 {
			t.Fatalf("retry ran after journal rebind: %s", command)
		}
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, alias); err != nil {
			t.Fatal(err)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 1, Stdout: `{"error":{"code":"OperationConflict","request_id":"req-first"}}`, Stderr: "provider detail"}
	}
	failures := cl.runCaseCleanup(*scope)
	if calls != 1 || len(failures) != 1 || !strings.Contains(failures[0], "target changed") || !strings.Contains(failures[0], "cleanup exit=1") || !strings.Contains(failures[0], "req-first") || !strings.Contains(failures[0], "provider detail") || !strings.Contains(strings.Join(logs, "\n"), "req-first") {
		t.Fatalf("retry lost provider evidence: calls=%d failures=%v logs=%v", calls, failures, logs)
	}
	if len(quotaJournal(t, path).Entries) != 2 {
		t.Fatalf("retry rebind lost durable cleanup entries: %+v", quotaJournal(t, path).Entries)
	}
	preflightOnly := cl.runCaseCleanup(*scope)
	if calls != 1 || len(preflightOnly) != 1 || strings.Contains(preflightOnly[0], "prior invocation") || strings.Contains(preflightOnly[0], "req-first") || strings.Contains(preflightOnly[0], "cleanup exit=") {
		t.Fatalf("preflight-only refusal fabricated provider result: calls=%d failures=%v", calls, preflightOnly)
	}
}

func TestCaseReportKeepsQuotaProviderResultAfterRetryRebind(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	configureQuotaProfileFixture(t, dir)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	writeQuotaIdentityProbeFake(t, cfg.Bin, dir)
	path := filepath.Join(dir, "cleanup.json")
	alias := filepath.Join(dir, "requested")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	previousDelays := cleanupRetryDelays
	cleanupRetryDelays = []time.Duration{0}
	defer func() { cleanupRetryDelays = previousDelays }()
	var logs []string
	logf := func(format string, values ...any) { logs = append(logs, fmt.Sprintf(format, values...)) }
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, logf)
	cl.requestedJournal = filepath.Join(alias, filepath.Base(path))
	cl.journalPaths = map[string]string{"primary": path}
	loaded, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	suite := loaded
	suite.Steps = suite.Steps[:2]
	deletes := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		path, err := commandAction(command)
		if err != nil || len(path) != 3 {
			return execpkg.Result{Command: command, Started: true, Exit: 90, Err: fmt.Errorf("unexpected command: %s", command)}
		}
		switch path[1] + "/" + path[2] {
		case "team/create":
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
		case "quota/update":
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
		case "quota/delete":
			deletes++
			if deletes != 1 {
				t.Fatalf("rebound journal allowed a second provider call: %s", command)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, alias); err != nil {
				t.Fatal(err)
			}
			return execpkg.Result{Command: command, Started: true, Exit: 1, Stdout: `{"error":{"code":"OperationConflict","request_id":"req-case"}}`, Stderr: "provider rejected"}
		}
		return execpkg.Result{Command: command, Started: true, Exit: 90, Err: fmt.Errorf("unexpected command: %s", command)}
	}
	caseResult := runCase(context.Background(), Options{RunName: "quota-rebind", InputsDir: filepath.Join(dir, "inputs"), StepTimeout: time.Second, Logf: logf}, cfg, cl, cl.operations, map[string]any{}, nil, nil, suite)
	if caseResult.Status != report.StatusFail || deletes != 1 || !strings.Contains(caseResult.Error, "target changed") || !strings.Contains(caseResult.Error, "cleanup exit=1") || !strings.Contains(caseResult.Error, "req-case") || !strings.Contains(caseResult.Error, "provider rejected") || !strings.Contains(strings.Join(logs, "\n"), "req-case") {
		t.Fatalf("case report lost cleanup provider evidence: case=%+v deletes=%d logs=%v", caseResult, deletes, logs)
	}
}

func TestSandboxTeamCreateRejectsEarlierOrdinaryTeamFinalizerBeforeLaunch(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, filepath.Join(dir, "journal.json"),
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var scope []*cleanupItem
	if err := cl.push(&scope, "case", "ecctl sandbox team delete team-owned --timeout 1m", "primary", nil); err != nil {
		t.Fatal(err)
	}
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"run_name": "case"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("Team create launched beside ordinary finalizer: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, suite.Steps[0], time.Second)
	if ok || !strings.Contains(step.Error, "ordinary Team finalizer") || len(scope) != 1 || cl.teamCreatesPending != 0 {
		t.Fatalf("earlier ordinary Team finalizer did not stop create: step=%+v scope=%+v pending=%d", step, scope, cl.teamCreatesPending)
	}
}

func TestSandboxQuotaUpdateCleanupReservedBeforeMutationAndConsumed(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	updateCalls, quotaDeletes, teamDeletes := 0, 0, 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		switch {
		case strings.Contains(command, "sandbox quota update "):
			updateCalls++
			journal := quotaJournal(t, journalPath)
			if journal.Version != 2 || journal.RunID != "quota-run" || journal.ExecutionID != "quota-execution" || journal.Region != cfg.Region || journal.Surface != "public" || journal.EcctlBin != cfg.Bin || len(journal.Entries) != 2 || journal.Entries[0].Teardown != "ecctl sandbox team delete team-owned --timeout 5m" || journal.Entries[1].Teardown != "ecctl sandbox quota delete team-owned --output=json" || !journal.Entries[1].QuotaUpdatePending {
				t.Fatalf("UpdateQuota reached cloud before exact durable cleanup: %+v", journal)
			}
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
		case strings.Contains(command, "sandbox quota delete "):
			quotaDeletes++
		case strings.Contains(command, "sandbox team delete "):
			teamDeletes++
		default:
			t.Fatalf("unexpected Quota cleanup command: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	for _, index := range []int{1, 4, 6} {
		if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[index], time.Second); !ok {
			t.Fatalf("step %q failed: %+v", suite.Steps[index].Name, step)
		}
		if index == 1 && quotaJournal(t, journalPath).Entries[1].QuotaUpdatePending {
			t.Fatal("confirmed UpdateQuota retained the pending fence")
		}
	}
	if updateCalls != 1 || quotaDeletes != 1 || teamDeletes != 1 || len(quotaJournal(t, journalPath).Entries) != 0 {
		t.Fatalf("explicit cleanup repeated or retained: updates=%d quota=%d team=%d journal=%+v", updateCalls, quotaDeletes, teamDeletes, quotaJournal(t, journalPath).Entries)
	}
}

func TestSandboxQuotaSecondUpdateRejectsBeforeMutationAndKeepsUniqueChild(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	updates := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "sandbox quota update") {
			updates++
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("first update: %+v", step)
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); ok || !strings.Contains(step.Error, "unfinished in-memory reservation") {
		t.Fatalf("second update was not rejected: %+v", step)
	}
	if updates != 1 || len(*scope) != 2 || len(quotaJournal(t, path).Entries) != 2 || quotaJournal(t, path).Entries[1].QuotaUpdatePending {
		t.Fatalf("second update damaged unique child: updates=%d scope=%+v journal=%+v", updates, *scope, quotaJournal(t, path))
	}
	for _, index := range []int{4, 6} {
		if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[index], time.Second); !ok {
			t.Fatalf("cleanup step %d: %+v", index, step)
		}
	}
	if len(quotaJournal(t, path).Entries) != 0 {
		t.Fatalf("unique child or parent retained: %+v", quotaJournal(t, path).Entries)
	}
}

func TestSandboxQuotaParentDeleteWaitsForChild(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("update: %+v", step)
	}
	calls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		calls++
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[6], time.Second); ok || !strings.Contains(step.Error, "Quota child") {
		t.Fatalf("parent-before-child delete accepted: %+v", step)
	}
	if calls != 0 || quotaJournal(t, path).Entries[0].TeamReplayAttempted {
		t.Fatalf("blocked parent crossed subprocess or attempt marker: calls=%d journal=%+v", calls, quotaJournal(t, path))
	}
	for _, index := range []int{4, 6} {
		if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[index], time.Second); !ok {
			t.Fatalf("ordered cleanup step %d: %+v", index, step)
		}
	}
	if calls != 2 || len(quotaJournal(t, path).Entries) != 0 {
		t.Fatalf("ordered cleanup incomplete: calls=%d journal=%+v", calls, quotaJournal(t, path))
	}
}

func TestSandboxTeamUnknownDeleteMarksAttemptBeforeCallAndBlocksQuotaUpdate(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	teamCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		teamCalls++
		if !quotaJournal(t, path).Entries[0].TeamReplayAttempted {
			t.Fatalf("Team delete started without durable attempt marker: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown Team delete result")}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[6], time.Second); ok || !strings.Contains(step.Error, "unknown Team delete result") {
		t.Fatalf("unknown Team delete result: %+v", step)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("Quota update started after Team delete attempt: %s", command)
		return execpkg.Result{}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); ok || !strings.Contains(step.Error, "no confirmed run-owned Team") {
		t.Fatalf("attempted Team authorized update: %+v", step)
	}
	if teamCalls != 1 || !quotaJournal(t, path).Entries[0].TeamReplayAttempted || len(quotaJournal(t, path).Entries) != 1 {
		t.Fatalf("unknown Team delete lost attempt fence: calls=%d journal=%+v", teamCalls, quotaJournal(t, path))
	}
}

func TestSandboxTeamAliasUnknownDeleteRetainsAttemptFence(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	step := scenario.Step{Name: "delete Team alias", Run: "ecctl sbx team delete {{.team_id}} --timeout=1m --output=json"}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if !quotaJournal(t, path).Entries[0].TeamReplayAttempted {
			t.Error("alias Team delete started before durable attempt")
		}
		return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown Team delete")}
	}
	if result, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, step, time.Second); ok || !strings.Contains(result.Error, "unknown Team delete") {
		t.Fatalf("alias Team unknown result: %+v", result)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("attempted Team authorized Quota update: %s", command)
		return execpkg.Result{}
	}
	if result, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); ok || !strings.Contains(result.Error, "no confirmed run-owned Team") {
		t.Fatalf("attempted alias Team reauthorized Quota: %+v", result)
	}
}

func TestSandboxTeamDeleteVariantsShareOneTargetClaim(t *testing.T) {
	cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
	cl.operations = newOperationRuntime(2)
	started, rejected, release := make(chan struct{}), make(chan struct{}, 1), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	previous := deleteClaimRejected
	deleteClaimRejected = func(command string) {
		if strings.Contains(command, "sandbox team delete team-owned") {
			select {
			case rejected <- struct{}{}:
			default:
			}
		}
	}
	defer func() { deleteClaimRejected = previous }()
	var calls atomic.Int32
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	dag := &scenario.Suite{Resource: "sandbox/team", Steps: []scenario.Step{
		{Name: "canonical", Run: "ecctl sandbox team delete {{.team_id}} --timeout 5m"},
		{Name: "alias", Run: "ecctl sbx team delete {{.team_id}} --timeout=1m --output=json"},
	}}
	caseReport := &report.Case{Status: report.StatusPass}
	done := make(chan struct{})
	go func() {
		runDAGCase(context.Background(), Options{}, cfg, cl, cl.operations, scope, data, nil, nil, dag, time.Second, caseReport)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first Team delete did not start")
	}
	select {
	case <-rejected:
	case <-time.After(2 * time.Second):
		t.Fatal("second Team delete did not reach target claim")
	}
	releaseOnce()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Team delete DAG did not finish")
	}
	if calls.Load() != 1 || !(*scope)[0].done || len(quotaJournal(t, path).Entries) != 0 {
		t.Fatalf("variant Team delete duplicated target: calls=%d scope=%+v journal=%+v", calls.Load(), *scope, quotaJournal(t, path))
	}
}

func TestSandboxTeamAutomaticCleanupMarksAttemptBeforeDelete(t *testing.T) {
	_, cl, scope, _, _, path := quotaCleanupHarness(t)
	cl.keep = false
	calls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		calls++
		journal := quotaJournal(t, path)
		if len(journal.Entries) != 1 || !journal.Entries[0].TeamReplayAttempted {
			t.Errorf("automatic Team delete started before durable attempt: %+v", journal)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
		t.Fatalf("automatic Team cleanup: %v", failures)
	}
	if calls != 1 || !(*scope)[0].done || len(quotaJournal(t, path).Entries) != 0 {
		t.Fatalf("automatic Team cleanup did not consume exact parent: calls=%d scope=%+v journal=%+v", calls, *scope, quotaJournal(t, path))
	}
}

func TestSandboxQuotaDAGDeletesUseOneClaimThroughInvocationAndConsumption(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("update: %+v", step)
	}
	cl.operations = newOperationRuntime(2)
	started, rejected, release := make(chan struct{}), make(chan struct{}, 1), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	previous := deleteClaimRejected
	deleteClaimRejected = func(command string) {
		if strings.Contains(command, "sandbox quota delete") {
			select {
			case rejected <- struct{}{}:
			default:
			}
		}
	}
	defer func() { deleteClaimRejected = previous }()
	var calls atomic.Int32
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	deleteStep := scenario.Step{Run: "ecctl sandbox quota delete {{.team_id}} --output=json"}
	dag := &scenario.Suite{Resource: "sandbox/quota", Steps: []scenario.Step{{Name: "delete one", Run: deleteStep.Run}, {Name: "delete two", Run: deleteStep.Run}}}
	caseReport := &report.Case{Status: report.StatusPass}
	done := make(chan struct{})
	go func() {
		runDAGCase(context.Background(), Options{}, cfg, cl, cl.operations, scope, data, nil, nil, dag, time.Second, caseReport)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first delete did not start")
	}
	select {
	case <-rejected:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent delete did not reach the claim")
	}
	releaseOnce()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DAG delete did not finish")
	}
	if calls.Load() != 1 || len(quotaJournal(t, path).Entries) != 1 || !(*scope)[1].done {
		t.Fatalf("DAG launched duplicate delete or lost exact child: calls=%d scope=%+v journal=%+v", calls.Load(), *scope, quotaJournal(t, path))
	}
}

func TestSandboxQuotaDAGTeamAttemptFencesWaitingUpdate(t *testing.T) {
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := canonicalCleanupTestDir(t)
	bin := filepath.Join(dir, "ecctl")
	configureQuotaProbeFixture(t, dir, bin)
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	path := filepath.Join(dir, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(2), true, path,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	data := map[string]any{"run_name": "dag-quota-case"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	teamStarted, updateReady, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	previous := quotaBeforeReplayLock
	quotaBeforeReplayLock = func() { <-teamStarted; close(updateReady) }
	defer func() { quotaBeforeReplayLock = previous }()
	var updateCalls atomic.Int32
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		actionPath, _ := commandAction(command)
		if len(actionPath) == 3 && actionPath[0] == "sandbox" && actionPath[1] == "team" && actionPath[2] == "create" {
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
		}
		if len(actionPath) == 3 && actionPath[0] == "sandbox" && actionPath[1] == "team" && actionPath[2] == "delete" {
			if !quotaJournal(t, path).Entries[0].TeamReplayAttempted {
				t.Error("Team delete started before its durable attempt marker")
			}
			close(teamStarted)
			<-release
			return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown Team deletion")}
		}
		updateCalls.Add(1)
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	create := suite.Steps[0]
	deleteTeam := suite.Steps[6]
	update := suite.Steps[1]
	deleteTeam.DependsOn = []string{create.Name}
	update.DependsOn = []string{create.Name}
	dag := &scenario.Suite{Resource: "sandbox/quota", Steps: []scenario.Step{create, deleteTeam, update}}
	caseReport := &report.Case{Status: report.StatusPass}
	var scope []*cleanupItem
	done := make(chan struct{})
	go func() {
		runDAGCase(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, cl.operations, &scope, data, nil, nil, dag, time.Second, caseReport)
		close(done)
	}()
	select {
	case <-updateReady:
	case <-time.After(2 * time.Second):
		t.Fatal("DAG update did not reach replay fence")
	}
	releaseOnce()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DAG Team/update interleaving did not finish")
	}
	journal := quotaJournal(t, path)
	if updateCalls.Load() != 0 || len(journal.Entries) != 1 || !journal.Entries[0].TeamReplayAttempted || len(scope) != 1 {
		t.Fatalf("waiting update crossed attempted Team: updates=%d scope=%+v journal=%+v", updateCalls.Load(), scope, journal)
	}
}

func TestSandboxQuotaDeleteRejectsUnownedExecutableCommands(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox quota delete foreign-team --output=json",
		"ecctl sbx quota delete foreign-team --output=json",
		"ecctl --region cn-hangzhou sandbox quota delete foreign-team --output=json",
		"ecctl sandbox --output=json quota delete foreign-team",
		"ecctl sandbox quota --output=json delete foreign-team",
		"ecctl sandbox quota rm foreign-team --output=json",
		"ecctl sandbox quota delete team-owned --output=json",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, actual string) execpkg.Result {
				calls++
				return execpkg.Result{Command: actual, Started: true, Exit: 0}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "unowned delete", Run: command}, time.Second)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if ok || calls != 0 || !bytes.Equal(before, after) || len(*scope) != 1 {
				t.Fatalf("unowned delete crossed the guard: step=%+v calls=%d journal_changed=%t", step, calls, !bytes.Equal(before, after))
			}
		})
	}
}

func TestSandboxQuotaDeleteRequiresExactSettledDurableReservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*report.CleanupJournal)
	}{
		{"missing child", func(j *report.CleanupJournal) { j.Entries = j.Entries[:1] }},
		{"ambiguous child", func(j *report.CleanupJournal) { j.Entries = append(j.Entries, j.Entries[1]) }},
		{"pending child", func(j *report.CleanupJournal) { j.Entries[1].QuotaUpdatePending = true }},
		{"attempted parent", func(j *report.CleanupJournal) { j.Entries[0].TeamReplayAttempted = true }},
		{"wrong scope", func(j *report.CleanupJournal) { j.Entries[1].Scope = "foreign" }},
		{"wrong binary", func(j *report.CleanupJournal) { j.EcctlBin = "foreign-ecctl" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
			}
			if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
				t.Fatalf("update: %+v", step)
			}
			journal := quotaJournal(t, path)
			tc.mutate(&journal)
			if err := writeRunnerJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[4], time.Second)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if ok || calls != 0 || !bytes.Equal(before, after) {
				t.Fatalf("invalid reservation launched delete: step=%+v calls=%d journal_changed=%t", step, calls, !bytes.Equal(before, after))
			}
		})
	}
}

func TestSandboxQuotaFinalizerRefusesStaleChildAndKeepsParent(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("update: %+v", step)
	}
	journal := quotaJournal(t, path)
	journal.Entries = journal.Entries[:1]
	if err := writeRunnerJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	cl.keep = false
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("stale Quota finalizer or its Team parent launched: %s", command)
		return execpkg.Result{}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 1 || (*scope)[0].done || (*scope)[1].done || len(quotaJournal(t, path).Entries) != 1 {
		t.Fatalf("stale Quota finalizer changed parent: failures=%v scope=%+v entries=%+v", failures, *scope, quotaJournal(t, path).Entries)
	}
}

func TestSandboxQuotaDeleteCanceledBeforeSharedFenceKeepsReservation(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("update: %+v", step)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- journalfile.WithReplayLock(context.Background(), path, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("canceled Quota delete launched: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(ctx, Options{}, cfg, cl, scope, data, suite.Steps[4], time.Second)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok || !bytes.Equal(before, after) || (*scope)[1].done {
		t.Fatalf("canceled Quota delete changed reservation: step=%+v journal_changed=%t", step, !bytes.Equal(before, after))
	}
}

func TestQuotaCandidateIgnoresUnrelatedTeardownFlagValue(t *testing.T) {
	step := scenario.Step{Teardown: "ecctl sandbox template delete template-id --profile 'quota delete'"}
	for _, command := range []string{"ecctl sandbox template create --profile 'quota delete' --name template-name", "ecctl sandbox template-cache create --profile 'quota delete' --team-id team-owned --template-id template-id --output=json"} {
		if quotaUpdateCandidate(step, command) || quotaDeleteCandidate(command) {
			t.Fatalf("unrelated command classified as Quota mutation: %s", command)
		}
	}
}

func TestCleanupJournalBindingRejectsChangedSymlinkBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	requested := filepath.Join(alias, "cleanup.json")
	bound, err := journalfile.ResolveJournalPath(requested)
	if err != nil {
		t.Fatal(err)
	}
	cl := newCleanup(map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-hangzhou"}}, nil, true, bound,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.requestedJournal = requested
	cl.journalPaths = map[string]string{"primary": bound}
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	if err := cl.push(&scope, "case", "ecctl sandbox team delete team-owned --timeout 5m", "primary", nil); err == nil || !strings.Contains(err.Error(), "target changed") {
		t.Fatalf("changed target registration error = %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("changed journal target retained cleanup item: %+v", scope)
	}
}

func TestRunRejectsUnresolvableJournalBeforeAnyTeamMutation(t *testing.T) {
	dir := t.TempDir()
	alias := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), alias); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{CleanupJournal: filepath.Join(alias, "cleanup.json"), Region: "cn-hangzhou", Suites: []*scenario.Suite{{Resource: "sandbox/quota"}}})
	if err == nil || !strings.Contains(err.Error(), "bind cleanup journal") {
		t.Fatalf("unresolvable journal setup error = %v", err)
	}
}

func TestSandboxQuotaMutationEntryRejectsAlternateFormsWithoutTeardown(t *testing.T) {
	for _, tc := range []struct {
		name, command string
	}{
		{"inline flags before update", "ecctl sandbox quota --cpu-cores=32 --memory-gb=64 update foreign-team --output=json"},
		{"sbx alias", "ecctl sbx quota update foreign-team --cpu-cores 32 --memory-gb 64 --output=json"},
		{"separate flags before update", "ecctl sandbox quota --cpu-cores 32 --memory-gb 64 update foreign-team --output=json"},
		{"flags before resource", "ecctl sandbox --cpu-cores=32 quota --memory-gb=64 update foreign-team --output=json"},
		{"flags before product", "ecctl --cpu-cores=32 sandbox quota update foreign-team --memory-gb=64 --output=json"},
		{"root flag and alias", "ecctl --region cn-hangzhou sbx quota --cpu-cores=32 update foreign-team --memory-gb=64 --output=json"},
		{"unknown flag within Quota family", "ecctl sandbox quota --unclassified=1 update foreign-team --cpu-cores 32 --memory-gb 64 --output=json"},
		{"unknown flag before family", "ecctl --unclassified=1 sandbox quota update foreign-team --cpu-cores 32 --memory-gb 64 --output=json"},
		{"unknown flag before interspersed family", "ecctl --unclassified=1 sandbox --cpu-cores=32 quota update foreign-team --memory-gb=64 --output=json"},
		{"canonical foreign target", "ecctl sandbox quota update foreign-team --cpu-cores 32 --memory-gb 64 --output=json"},
		{"canonical owned target lacks teardown", "ecctl sandbox quota update team-owned --cpu-cores 32 --memory-gb 64 --output=json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: tc.name, Run: tc.command}, time.Second)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if ok || !strings.Contains(step.Error, "Quota update coordination") || calls != 0 || len(*scope) != 1 || !bytes.Equal(before, after) {
				t.Fatalf("unowned Quota mutation crossed the entry guard: step=%+v calls=%d scope=%+v journal_changed=%t", step, calls, *scope, !bytes.Equal(before, after))
			}
		})
	}
}

func TestSandboxQuotaReadAndUnrelatedCommandsKeepOrdinaryInvocation(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox quota get team-owned --output=json",
		"ecctl sandbox quota --all=true list --output=json",
		"ecctl sbx quota --all=false list --output=json",
		"ecctl sandbox team get team-owned --profile quota --output=json",
		"ecctl sandbox team --all=true list --output=json",
		"ecctl sbx team --all=false list --output=json",
		"ecctl sandbox team --page=2 list --limit=1 --output=json",
		"ecctl sandbox team --page 2 list --limit 1 --output=json",
		"ecctl sbx team --page=2 list --limit=1 --output=json",
	} {
		t.Run(command, func(t *testing.T) {
			if strings.Contains(command, "--page") {
				action, err := commandAction(command)
				if err != nil || len(action) != 3 || (action[0] != "sandbox" && action[0] != "sbx") || action[1] != "team" || action[2] != "list" {
					t.Fatalf("Team page was not classified as a list action: path=%v err=%v", action, err)
				}
			}
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, actual string) execpkg.Result {
				calls++
				return execpkg.Result{Command: actual, Started: true, Exit: 0, JSON: map[string]any{}}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "read", Run: command}, time.Second)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !ok || calls != 1 || len(*scope) != 1 || !bytes.Equal(before, after) {
				t.Fatalf("ordinary command changed Quota entry behavior: step=%+v calls=%d scope=%+v journal_changed=%t", step, calls, *scope, !bytes.Equal(before, after))
			}
		})
	}
}

func TestSandboxCrossScopeExplicitTeamDeleteCannotConsumeRetainedQuotaParent(t *testing.T) {
	cfg, cl, ownerScope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, ownerScope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("settle Quota child: %+v", step)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("later scope launched protected Team delete: %s", command)
		return execpkg.Result{}
	}
	var laterScope []*cleanupItem
	step, ok := runStep(context.Background(), Options{}, cfg, cl, &laterScope, data,
		scenario.Step{Name: "later delete", Run: "ecctl sbx team delete {{.team_id}} --timeout=1m --output=json"}, time.Second)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok || step.Command != "" || !strings.Contains(step.Error, "Quota child") || !bytes.Equal(before, after) {
		t.Fatalf("cross-scope Team delete changed retained ownership: step=%+v journal_changed=%t", step, !bytes.Equal(before, after))
	}
}

func TestSandboxOrdinaryTeamDeleteWithoutTimeoutKeepsTargetIsolation(t *testing.T) {
	cfg, cl, ownerScope, data, suite, _ := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, ownerScope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("settle Quota child: %+v", step)
	}
	calls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		calls++
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	var laterScope []*cleanupItem
	blocked, ok := runStep(context.Background(), Options{}, cfg, cl, &laterScope, data,
		scenario.Step{Name: "protected bare delete", Run: "ecctl sandbox team delete team-owned"}, time.Second)
	if ok || blocked.Command != "" || calls != 0 {
		t.Fatalf("bare Team delete bypassed another scope: %+v calls=%d", blocked, calls)
	}
	other, ok := runStep(context.Background(), Options{}, cfg, cl, &laterScope, data,
		scenario.Step{Name: "unrelated bare delete", Run: "ecctl sandbox team delete team-other"}, time.Second)
	if !ok || calls != 1 || other.Command == "" {
		t.Fatalf("unrelated Team target was blocked: %+v calls=%d", other, calls)
	}
	alias, ok := runStep(context.Background(), Options{}, cfg, cl, &laterScope, data,
		scenario.Step{Name: "unrelated alias delete", Run: "ecctl sbx team rm team-third --timeout=1m"}, time.Second)
	if !ok || calls != 2 || alias.Command == "" {
		t.Fatalf("unrelated Team alias was blocked: %+v calls=%d", alias, calls)
	}
}

func TestSandboxQuotaReplaySnapshotConsumesTeamBeforeReservation(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	replayRead := make(chan struct{})
	releaseReplay := make(chan struct{})
	replayDone := make(chan error, 1)
	go func() {
		replayDone <- journalfile.WithReplayLock(context.Background(), path, func() error {
			if err := journalfile.WithLock(context.Background(), path, func() error {
				journal, _, err := readRunnerJournal(path)
				if err != nil {
					return err
				}
				if len(journal.Entries) != 1 {
					return errors.New("replay did not snapshot Team-only journal")
				}
				return nil
			}); err != nil {
				return err
			}
			close(replayRead)
			<-releaseReplay
			return journalfile.WithLock(context.Background(), path, func() error {
				journal, _, err := readRunnerJournal(path)
				if err != nil {
					return err
				}
				journal.Entries = nil
				return writeRunnerJournal(path, journal)
			})
		})
	}()
	<-replayRead
	previous := quotaBeforeReplayLock
	attempting := make(chan struct{})
	quotaBeforeReplayLock = func() { close(attempting) }
	defer func() { quotaBeforeReplayLock = previous }()
	providerCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		t.Errorf("UpdateQuota launched after Team replay: %s", command)
		return execpkg.Result{}
	}
	type outcome struct {
		step report.Step
		ok   bool
	}
	stepDone := make(chan outcome, 1)
	go func() {
		step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
		stepDone <- outcome{step, ok}
	}()
	<-attempting
	close(releaseReplay)
	if err := <-replayDone; err != nil {
		t.Fatal(err)
	}
	result := <-stepDone
	entries := quotaJournal(t, path).Entries
	if result.ok || !strings.Contains(result.step.Error, "unexpected active cleanup entries") || providerCalls != 0 || len(*scope) != 1 || !(*scope)[0].teamCreateConfirmed || (*scope)[0].teamReplayAttempted || (*scope)[0].done || len(entries) != 0 {
		t.Fatalf("late reservation bypassed replay: result=%+v calls=%d scope=%+v entries=%+v", result, providerCalls, *scope, entries)
	}
}

func TestSandboxQuotaReplayAttemptedTeamCannotAuthorizeUpdate(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	if err := journalfile.WithReplayLock(context.Background(), path, func() error {
		return journalfile.WithLock(context.Background(), path, func() error {
			journal, _, err := readRunnerJournal(path)
			if err != nil {
				return err
			}
			journal.Entries[0].TeamReplayAttempted = true
			return writeRunnerJournal(path, journal)
		})
	}); err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		t.Errorf("UpdateQuota launched after Team replay attempt: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	entries := quotaJournal(t, path).Entries
	if ok || !strings.Contains(step.Error, "unexpected active cleanup entries") || providerCalls != 0 || len(*scope) != 1 || !(*scope)[0].teamCreateConfirmed || (*scope)[0].teamReplayAttempted || (*scope)[0].done || len(entries) != 1 || !entries[0].TeamReplayAttempted {
		t.Fatalf("replay-attempted Team authorized Quota: step=%+v calls=%d scope=%+v entries=%+v", step, providerCalls, *scope, entries)
	}
}

func TestSandboxQuotaUpdatePendingExcludesReplayAndUnknownRemainsFenced(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	updateStarted := make(chan struct{})
	releaseUpdate := make(chan struct{})
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		close(updateStarted)
		<-releaseUpdate
		return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown result")}
	}
	type outcome struct {
		step report.Step
		ok   bool
	}
	stepDone := make(chan outcome, 1)
	go func() {
		step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
		stepDone <- outcome{step, ok}
	}()
	<-updateStarted
	if entries := quotaJournal(t, path).Entries; len(entries) != 2 || !entries[1].QuotaUpdatePending {
		t.Fatalf("active update lacks durable pending fence: %+v", entries)
	}
	replayCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := sweeper.ReplayJournal(replayCtx, path, cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("replay entered active update: %v", err)
	}
	close(releaseUpdate)
	result := <-stepDone
	if result.ok || !strings.Contains(result.step.Error, "remains pending") || !strings.Contains(result.step.Error, "unknown result") {
		t.Fatalf("unknown update result = %+v", result)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), path, cfg); !errors.Is(err, journalfile.ErrQuotaUpdatePending) {
		t.Fatalf("replay did not honor unknown pending update: %v", err)
	}
}

func TestSandboxQuotaSuccessfulUpdateSettlementFailureKeepsPending(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	previous := writeQuotaSettlementJournal
	writeQuotaSettlementJournal = func(string, report.CleanupJournal) error { return errors.New("injected settlement failure") }
	defer func() { writeQuotaSettlementJournal = previous }()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, Duration: 37 * time.Millisecond,
			Stdout: `{"quota":{"id":"team-owned"},"actions":[{"request_id":"req-update"}]}`, Stderr: "provider warning"}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || !strings.Contains(step.Error, "injected settlement failure") || step.Command == "" || step.Exit != 0 || step.DurationMs != 37 || !strings.Contains(step.Stdout, "req-update") || step.Stderr != "provider warning" || len(quotaJournal(t, path).Entries) != 2 || !quotaJournal(t, path).Entries[1].QuotaUpdatePending {
		t.Fatalf("settlement failure lost pending recovery: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
	}
}

func TestSandboxSuccessfulDeleteConsumptionFailureRetainsProcessEvidence(t *testing.T) {
	for _, action := range []string{"Quota", "Team"} {
		t.Run(action, func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			if action == "Quota" {
				cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
					return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
				}
				if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
					t.Fatalf("reserve settled Quota child: %+v", step)
				}
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0, Duration: 41 * time.Millisecond,
					Stdout: `{"deleted":true,"actions":[{"request_id":"req-delete"}]}`, Stderr: "provider warning", JSON: map[string]any{"deleted": true}}
			}
			index := 6
			if action == "Quota" {
				index = 4
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[index], time.Second)
			if ok || step.Status != report.StatusError || step.Error == "" || step.Command == "" || step.Exit != 0 || step.DurationMs != 41 || !strings.Contains(step.Stdout, "req-delete") || step.Stderr != "provider warning" {
				t.Fatalf("post-delete journal error erased invocation evidence: %+v", step)
			}
		})
	}
}

func TestSandboxQuotaSettlementUncertaintyKeepsInMemoryFence(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	previous := writeQuotaSettlementJournal
	writeQuotaSettlementJournal = func(path string, journal report.CleanupJournal) error {
		if err := writeRunnerJournal(path, journal); err != nil {
			return err
		}
		return errors.New("injected post-rename settlement uncertainty")
	}
	defer func() { writeQuotaSettlementJournal = previous }()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || !strings.Contains(step.Error, "post-rename settlement uncertainty") || quotaJournal(t, path).Entries[1].QuotaUpdatePending || !(*scope)[1].quotaUpdatePending {
		t.Fatalf("uncertain settlement lost one recovery fence: step=%+v scope=%+v entries=%+v", step, *scope, quotaJournal(t, path).Entries)
	}
	cl.keep = false
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("in-process cleanup crossed uncertain settlement: %s", command)
		return execpkg.Result{}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
		t.Fatalf("uncertain settlement cleanup was not deferred: %v", failures)
	}
}

func TestSandboxQuotaUpdateReservationWriteFailureBlocksMutation(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	previous := writeQuotaReservationJournal
	writeQuotaReservationJournal = func(string, report.CleanupJournal) error { return errors.New("injected durable write failure") }
	defer func() { writeQuotaReservationJournal = previous }()
	updateCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		updateCalls++
		t.Fatalf("mutation launched after reservation failure: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || !strings.Contains(step.Error, "injected durable write failure") || updateCalls != 0 || len(*scope) != 2 || len(quotaJournal(t, journalPath).Entries) != 1 {
		t.Fatalf("reservation failure did not stop UpdateQuota: step=%+v calls=%d scope=%+v journal=%+v", step, updateCalls, *scope, quotaJournal(t, journalPath).Entries)
	}
	writeQuotaReservationJournal = previous
	cl.keep = false
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if !strings.Contains(command, "sandbox team delete") {
			t.Fatalf("unexpected cleanup: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || len(quotaJournal(t, journalPath).Entries) != 0 || !(*scope)[1].done {
		t.Fatalf("unstarted Quota reservation blocked Team cleanup: failures=%v entries=%+v", failures, quotaJournal(t, journalPath).Entries)
	}
}

func TestSandboxQuotaUpdatePropagatesPostReservationProbeFailure(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	previous := writeQuotaReservationJournal
	writeQuotaReservationJournal = func(path string, journal report.CleanupJournal) error {
		if err := writeRunnerJournal(path, journal); err != nil {
			return err
		}
		config := os.Getenv("ECCTL_CONFIG_PATH")
		return os.WriteFile(config, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"changed-dummy","access_key_secret":"dummy-secret"}]}`), 0o600)
	}
	defer func() { writeQuotaReservationJournal = previous }()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("Quota update reached provider after credential change: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || step.Status != report.StatusError || !strings.Contains(step.Error, "protected Quota configuration state changed") || len(*scope) != 2 || !(*scope)[1].done {
		t.Fatalf("post-reservation probe failure was lost: step=%+v scope=%+v", step, *scope)
	}
	if entries := quotaJournal(t, journalPath).Entries; len(entries) != 1 || !entries[0].TeamCreateConfirmed || !entries[0].QuotaBound {
		t.Fatalf("post-reservation failure lost or retained wrong cleanup: %+v", entries)
	}
}

func TestSandboxQuotaUpdateUncertainReservationWriteKeepsRecovery(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	previous := writeQuotaReservationJournal
	writeQuotaReservationJournal = func(path string, journal report.CleanupJournal) error {
		if err := writeRunnerJournal(path, journal); err != nil {
			return err
		}
		return errors.New("injected post-rename uncertainty")
	}
	defer func() { writeQuotaReservationJournal = previous }()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("mutation launched after uncertain reservation write: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	entries := quotaJournal(t, journalPath).Entries
	if ok || !strings.Contains(step.Error, "injected post-rename uncertainty") || len(*scope) != 2 || len(entries) != 2 || entries[1].Teardown != "ecctl sandbox quota delete team-owned --output=json" {
		t.Fatalf("uncertain reservation did not retain recovery evidence: step=%+v scope=%+v entries=%+v", step, *scope, entries)
	}
	writeQuotaReservationJournal = previous
	cl.keep = false
	previousSettlement := writeQuotaSettlementJournal
	writeQuotaSettlementJournal = func(string, report.CleanupJournal) error { return errors.New("injected retirement failure") }
	defer func() { writeQuotaSettlementJournal = previousSettlement }()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if !strings.Contains(command, "sandbox team delete") {
			t.Fatalf("unexpected cleanup: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || len(cl.deferredScopes) != 1 || len(quotaJournal(t, journalPath).Entries) != 2 {
		t.Fatalf("failed retirement did not retain deferred child: failures=%v entries=%+v", failures, quotaJournal(t, journalPath).Entries)
	}
	writeQuotaSettlementJournal = previousSettlement
	if failures := cl.runDeferredCleanup(); len(failures) != 0 || len(quotaJournal(t, journalPath).Entries) != 0 || !(*scope)[1].done {
		t.Fatalf("installed unstarted Quota child blocked Team cleanup: failures=%v entries=%+v", failures, quotaJournal(t, journalPath).Entries)
	}
}

func TestSandboxQuotaUpdateUnknownResultRetainsRecovery(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("injected unknown result")}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	entries := quotaJournal(t, journalPath).Entries
	if ok || !strings.Contains(step.Error, "remains pending") || !strings.Contains(step.Error, "injected unknown result") || len(*scope) != 2 || len(entries) != 2 || entries[1].Teardown != "ecctl sandbox quota delete team-owned --output=json" || !entries[1].QuotaUpdatePending {
		t.Fatalf("unknown UpdateQuota result lost exact recovery: step=%+v scope=%+v entries=%+v", step, *scope, entries)
	}
	cl.keep = false
	var cleanupCalls []string
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		cleanupCalls = append(cleanupCalls, command)
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || len(cleanupCalls) != 0 || len(quotaJournal(t, journalPath).Entries) != 2 {
		t.Fatalf("unknown UpdateQuota was not fenced: failures=%v calls=%v entries=%+v", failures, cleanupCalls, quotaJournal(t, journalPath).Entries)
	}
}

func TestSandboxQuotaReplayLockWaitCancellationPreservesParent(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- journalfile.WithReplayLock(context.Background(), path, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("canceled Quota update launched: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(ctx, Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if ok || !strings.Contains(step.Error, "canceled") || len(quotaJournal(t, path).Entries) != 1 {
		t.Fatalf("canceled reservation changed parent: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
	}
}

func TestSandboxQuotaCleanupFailureKeepsTeamUntilQuotaClears(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("owned Quota update failed: %+v", step)
	}
	cl.keep = false
	var cleanupCalls []string
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		cleanupCalls = append(cleanupCalls, command)
		if strings.Contains(command, "sandbox team delete ") {
			t.Fatalf("Team deleted before failed Quota cleanup was resolved: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 1, JSON: map[string]any{"error": map[string]any{"code": "QuotaCleanupRejected"}}}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) == 0 {
		t.Fatal("failed Quota cleanup did not fail the case")
	}
	if len(cleanupCalls) != 1 || cleanupCalls[0] != "ecctl sandbox quota delete team-owned --output=json" || len(quotaJournal(t, journalPath).Entries) != 2 || (*scope)[0].done || (*scope)[1].done {
		t.Fatalf("failed Quota cleanup changed parent order: calls=%v scope=%+v entries=%+v", cleanupCalls, *scope, quotaJournal(t, journalPath).Entries)
	}
}

func TestSandboxQuotaUpdateNeverStartedRetiresChildBeforeFinalCleanup(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process", true: "replay"}[replay], func(t *testing.T) {
			if replay && runtime.GOOS == "windows" {
				t.Skip("fake CLI uses a shell script")
			}
			cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("injected spawn rejection")}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
			entries := quotaJournal(t, journalPath).Entries
			if ok || !strings.Contains(step.Error, "injected spawn rejection") || len(*scope) != 2 || !(*scope)[1].done || len(entries) != 1 || !entries[0].TeamCreateConfirmed {
				t.Fatalf("proven non-start left a pending Quota child: step=%+v scope=%+v entries=%+v", step, *scope, entries)
			}
			if replay {
				logPath := filepath.Join(filepath.Dir(journalPath), "recovered-calls")
				t.Setenv("FAKE_RECOVERED_CALLS", logPath)
				if !entries[0].QuotaBound || quotaJournal(t, journalPath).QuotaBinding == nil {
					t.Fatal("replay fixture lost its protected Team credential binding")
				}
				if err := os.WriteFile(cfg.Bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
printf '%s\n' "$*" >> "$FAKE_RECOVERED_CALLS"
case "$*" in
  *"sandbox team delete team-owned"*) echo '{"deleted":true}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
					t.Fatal(err)
				}
				if _, err := sweeper.ReplayJournal(context.Background(), journalPath, cfg); err != nil {
					t.Fatal(err)
				}
				calls, err := os.ReadFile(logPath)
				if err != nil || strings.Count(string(calls), "sandbox team delete team-owned") != 1 || strings.Contains(string(calls), "quota delete") {
					t.Fatalf("replay did not reclaim only the Team: calls=%q err=%v", calls, err)
				}
			} else {
				cl.keep = false
				cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
					if !strings.Contains(command, "team delete") || !quotaJournal(t, journalPath).Entries[0].TeamReplayAttempted {
						t.Errorf("cleanup did not preserve Team attempt fence: %s", command)
					}
					return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
				}
				if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
					t.Fatalf("proven non-start blocked final cleanup: %v", failures)
				}
			}
			if remaining := quotaJournal(t, journalPath).Entries; len(remaining) != 0 {
				t.Fatalf("proven non-start left recovery entries: %+v", remaining)
			}
		})
	}
}

func TestSandboxQuotaUncertainNoStartRetirementRetainsPendingFence(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprintf("removal-installed=%t", installed), func(t *testing.T) {
			cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
			previous := writeQuotaSettlementJournal
			writeQuotaSettlementJournal = func(path string, journal report.CleanupJournal) error {
				if installed {
					if err := previous(path, journal); err != nil {
						return err
					}
				}
				return errors.New("injected retirement write failure")
			}
			defer func() { writeQuotaSettlementJournal = previous }()
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("spawn rejected")}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
			entries := quotaJournal(t, journalPath).Entries
			wantEntries := 2
			if installed {
				wantEntries = 1
			}
			if ok || !strings.Contains(step.Error, "injected retirement write failure") || !strings.Contains(step.Error, "spawn rejected") || len(entries) != wantEntries || !(*scope)[1].quotaUpdatePending || !(*scope)[1].quotaReservationUnstarted || (*scope)[1].done {
				t.Fatalf("uncertain retirement lost proven non-start: step=%+v scope=%+v entries=%+v", step, *scope, entries)
			}
			cl.keep = false
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("cleanup launched before retirement durability recovered: %s", command)
				return execpkg.Result{}
			}
			if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || len(cl.deferredScopes) != 1 || (*scope)[1].done {
				t.Fatalf("transient retirement did not defer exact child: failures=%v scope=%+v", failures, *scope)
			}
			writeQuotaSettlementJournal = previous
			teamCalls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				teamCalls++
				if !strings.Contains(command, "sandbox team delete team-owned") {
					t.Fatalf("unexpected cleanup: %s", command)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			if failures := cl.runDeferredCleanup(); len(failures) != 0 || teamCalls != 1 || !(*scope)[1].done || len(quotaJournal(t, journalPath).Entries) != 0 {
				t.Fatalf("recovered retirement failed: failures=%v calls=%d entries=%+v", failures, teamCalls, quotaJournal(t, journalPath).Entries)
			}
		})
	}
}

func TestSandboxQuotaReadTeardownCannotDeleteOwnedTeamBeforeChild(t *testing.T) {
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	read := suite.Steps[2]
	read.Teardown = "ecctl sandbox team delete {{.team_id}} --timeout 1m"
	suite.Steps = []scenario.Step{suite.Steps[0], suite.Steps[1], read}
	dir := canonicalCleanupTestDir(t)
	configureQuotaProfileFixture(t, dir)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	writeQuotaIdentityProbeFake(t, cfg.Bin, dir)
	journalPath := filepath.Join(dir, "journal.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, journalPath,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var deletes []string
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		path, _ := commandAction(command)
		if len(path) != 3 {
			t.Fatalf("unexpected command: %s", command)
		}
		switch path[1] + "/" + path[2] {
		case "team/create":
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
		case "quota/update", "quota/get":
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
		case "quota/delete", "team/delete":
			deletes = append(deletes, path[1]+"/"+path[2])
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
		default:
			t.Fatalf("unexpected command: %s", command)
			return execpkg.Result{}
		}
	}
	result := runCase(context.Background(), Options{RunName: "read-teardown", InputsDir: filepath.Join(dir, "inputs"), StepTimeout: time.Second, Logf: func(string, ...any) {}}, cfg, cl, cl.operations, map[string]any{}, nil, nil, suite)
	if result.Status != report.StatusFail || len(result.Steps) != 3 || !strings.Contains(result.Steps[2].Error, "ordinary Team finalizer") || strings.Join(deletes, ",") != "quota/delete,team/delete" || len(quotaJournal(t, journalPath).Entries) != 0 {
		t.Fatalf("read teardown bypassed ordered cleanup: case=%+v deletes=%v journal=%+v", result, deletes, quotaJournal(t, journalPath))
	}
}

func TestSandboxQuotaPendingChildBlocksOrdinaryTeamFinalizerAndCleanup(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	var reserved *cleanupItem
	err := journalfile.WithReplayLock(context.Background(), path, func() error {
		var reserveErr error
		reserved, reserveErr = cl.reserveOwnedQuotaUpdate(context.Background(), path, scope, data, suite.Steps[1],
			"ecctl sandbox quota update team-owned --cpu-cores 32 --memory-gb 64 --output=json", cfg, nil)
		return reserveErr
	})
	if err != nil || reserved == nil || !reserved.quotaUpdatePending {
		t.Fatalf("pending child setup: item=%+v err=%v", reserved, err)
	}
	if err := cl.push(scope, caseScope(data), "ecctl sbx team delete team-owned --timeout=1m --output=json", "primary", nil); err == nil {
		t.Fatal("ordinary Team finalizer was registered beside pending child")
	}
	cl.keep = false
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("pending Quota child allowed cleanup subprocess: %s", command)
		return execpkg.Result{}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
		t.Fatalf("pending child cleanup should defer: %v", failures)
	}
	entries := quotaJournal(t, path).Entries
	if len(*scope) != 2 || len(entries) != 2 || !entries[1].QuotaUpdatePending || entries[0].TeamReplayAttempted {
		t.Fatalf("pending child or Team fence changed: scope=%+v entries=%+v", *scope, entries)
	}
}

func TestSandboxQuotaUpdateRejectsUnownedTarget(t *testing.T) {
	cfg, cl, scope, data, suite, journalPath := quotaCleanupHarness(t)
	data["team_id"] = "foreign-team"
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("unowned Quota update launched: %s", command)
		return execpkg.Result{}
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || !strings.Contains(step.Error, "no confirmed run-owned Team") || len(*scope) != 1 || len(quotaJournal(t, journalPath).Entries) != 1 {
		t.Fatalf("unowned target altered journal: step=%+v scope=%+v entries=%+v", step, *scope, quotaJournal(t, journalPath).Entries)
	}
}

func TestRawDeleteTeamKeepsAttemptedParentBeforeQuotaUpdate(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	deleteCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		deleteCalls++
		if !strings.Contains(command, "call fcsandbox DeleteTeam") {
			t.Fatalf("unexpected subprocess: %s", command)
		}
		marked := quotaJournal(t, path).Entries
		if len(marked) != 1 || !marked[0].TeamReplayAttempted {
			t.Fatalf("raw DeleteTeam launched before durable attempt: %+v", marked)
		}
		return rawTeamDeleteResult(command, cfg.Region, "200")
	}
	raw := scenario.Step{Name: "raw delete", Run: `ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --output=json`}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, raw, time.Second)
	entries := quotaJournal(t, path).Entries
	if !ok || deleteCalls != 1 || len(entries) != 1 || !entries[0].TeamReplayAttempted || (*scope)[0].done {
		t.Fatalf("raw DeleteTeam lost attempted ownership: step=%+v calls=%d entries=%+v", step, deleteCalls, entries)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("Quota update launched after Team delete: %s", command)
		return execpkg.Result{}
	}
	update, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second)
	if ok || !strings.Contains(update.Error, "no confirmed run-owned Team") {
		t.Fatalf("Quota update was authorized after raw DeleteTeam: %+v", update)
	}
}

func TestRawDeleteTeamRequiresProviderBusinessSuccessAndRetainsOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		pass   bool
	}{
		{"success code", func(map[string]any) {}, true},
		{"business failure", func(root map[string]any) { root["response"].(map[string]any)["code"] = "409" }, false},
		{"missing code", func(root map[string]any) { delete(root["response"].(map[string]any), "code") }, false},
		{"non-string code", func(root map[string]any) { root["response"].(map[string]any)["code"] = 200 }, false},
		{"missing response", func(root map[string]any) { delete(root, "response") }, false},
		{"invalid response", func(root map[string]any) { root["response"] = "invalid" }, false},
		{"wrong operation", func(root map[string]any) { root["operation"] = "ListTeams" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamReplayAttempted {
					t.Fatalf("raw Team delete started without durable attempt: %+v", entries)
				}
				result := rawTeamDeleteResult(command, cfg.Region, "200")
				root := result.JSON.(map[string]any)
				tc.change(root)
				body, _ := json.Marshal(root)
				result.Stdout = string(body)
				return result
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data,
				scenario.Step{Name: "raw delete", Run: `ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --output=json`}, time.Second)
			entries := quotaJournal(t, path).Entries
			if calls != 1 || ok != tc.pass {
				t.Fatalf("raw Team result misclassified: step=%+v calls=%d entries=%+v", step, calls, entries)
			}
			if len(entries) != 1 || !entries[0].TeamReplayAttempted || (*scope)[0].done {
				t.Fatalf("raw Team result consumed unconfirmed parent: step=%+v entries=%+v", step, entries)
			}
			if tc.pass {
				if step.Status != report.StatusPass || step.Stdout == "" {
					t.Fatalf("accepted raw Team delete lost provider evidence: %+v", step)
				}
			} else if step.Status != report.StatusError || !strings.Contains(step.Error, "raw Team delete") || step.Stdout == "" {
				t.Fatalf("unconfirmed raw Team delete consumed parent: step=%+v entries=%+v", step, entries)
			}
		})
	}
}

func TestRawDeleteTeamAcceptanceKeepsFinalizerThroughWaiterStates(t *testing.T) {
	cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
	raw := scenario.Step{Name: "raw delete", Run: `ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --output=json`}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return rawTeamDeleteResult(command, cfg.Region, "200")
	}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, raw, time.Second)
	if !ok || step.Status != report.StatusPass || step.Stdout == "" {
		t.Fatalf("raw DeleteTeam acceptance lost provider result: %+v", step)
	}
	cl.keep = false
	for _, state := range []struct {
		name   string
		result execpkg.Result
	}{
		{"deleting", execpkg.Result{Started: true, Exit: 1, JSON: map[string]any{"team": map[string]any{"status": "deleting"}}}},
		{"delete_failed", execpkg.Result{Started: true, Exit: 1, JSON: map[string]any{"error": map[string]any{"code": "DeleteFailed"}}}},
		{"waiter timeout", execpkg.Result{Started: true, Exit: 1, Stderr: "waiter timed out"}},
		{"GetTeam read error", execpkg.Result{Started: true, Exit: 1, Err: errors.New("GetTeam read failed")}},
	} {
		t.Run(state.name, func(t *testing.T) {
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if !strings.Contains(command, "sandbox team delete team-owned") {
					t.Fatalf("raw retry bypassed first-class Team delete waiter: %s", command)
				}
				result := state.result
				result.Command = command
				return result
			}
			failures := cl.runCaseCleanup(*scope)
			entries := quotaJournal(t, path).Entries
			if len(failures) == 0 || len(entries) != 1 || !entries[0].TeamReplayAttempted || (*scope)[0].done {
				t.Fatalf("%s consumed Team before absence: failures=%v entries=%+v", state.name, failures, entries)
			}
		})
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if !strings.Contains(command, "sandbox team delete team-owned") {
			t.Fatalf("final Team confirmation bypassed waiter: %s", command)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || len(quotaJournal(t, path).Entries) != 0 || !(*scope)[0].done {
		t.Fatalf("confirmed Team absence did not retire owner: failures=%v entries=%+v", failures, quotaJournal(t, path).Entries)
	}
}

func TestTeamDeleteAttemptPostRenameFailureRequiresBarrierOnRuntimeRetry(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			cl.keep = !automatic
			previous := writeTeamAttemptJournal
			t.Cleanup(func() { writeTeamAttemptJournal = previous })
			writes := 0
			writeTeamAttemptJournal = func(path string, journal report.CleanupJournal) error {
				writes++
				if err := previous(path, journal); err != nil {
					return err
				}
				if writes <= 2 {
					return errors.New("injected post-rename Team attempt sync failure")
				}
				return nil
			}
			providerCalls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				providerCalls++
				if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamReplayAttempted {
					t.Fatalf("Team delete started before durable attempt: %+v", entries)
				}
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			attempt := func() string {
				if automatic {
					return strings.Join(cl.runCaseCleanup(*scope), "; ")
				}
				step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[6], time.Second)
				if ok && step.Status != report.StatusPass {
					t.Fatalf("explicit Team delete returned inconsistent result: %+v", step)
				}
				return step.Error
			}
			for round := 1; round <= 2; round++ {
				failure := attempt()
				entries := quotaJournal(t, path).Entries
				if !strings.Contains(failure, "injected post-rename Team attempt sync failure") || providerCalls != 0 || len(entries) != 1 || !entries[0].TeamCreateConfirmed || !entries[0].TeamReplayAttempted || (*scope)[0].done {
					t.Fatalf("round %d crossed uncertain Team attempt: error=%q calls=%d entries=%+v", round, failure, providerCalls, entries)
				}
			}
			if failure := attempt(); failure != "" || providerCalls != 1 || writes != 3 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("Team attempt barrier did not recover: error=%q calls=%d writes=%d entries=%+v", failure, providerCalls, writes, quotaJournal(t, path).Entries)
			}
		})
	}
}

func TestBlankReaderTeardownKeepsEarlierTeamCleanupRunnable(t *testing.T) {
	for _, teardown := range []string{" ", "{{.blank}}"} {
		t.Run(teardown, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			data["blank"] = ""
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned"}}}
			}
			reader := scenario.Step{Name: "read", Run: "ecctl sandbox team get team-owned --output=json", Teardown: teardown}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, reader, time.Second)
			if ok || !strings.Contains(step.Error, "cleanup finalizer is empty") || len(*scope) != 1 || len(quotaJournal(t, path).Entries) != 1 {
				t.Fatalf("blank teardown changed prior ownership: step=%+v scope=%+v entries=%+v", step, *scope, quotaJournal(t, path).Entries)
			}
			if possibleRawTeamDelete(nil) || possibleRawTeamDelete([]string{}) {
				t.Fatal("empty raw command classified as Team delete")
			}
			cl.keep = false
			cleanupCalls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				cleanupCalls++
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
			}
			if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || cleanupCalls != 1 || len(quotaJournal(t, path).Entries) != 0 {
				t.Fatalf("prior Team cleanup blocked after blank teardown: failures=%v calls=%d entries=%+v", failures, cleanupCalls, quotaJournal(t, path).Entries)
			}
		})
	}
}

func TestRawDeleteTeamRejectsUnclassifiedTargetsBeforeSubprocess(t *testing.T) {
	for _, command := range []string{
		`ecctl call fcsandbox DeleteTeam --request @delete.json`,
		`ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --region cn-beijing`,
		`ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --aliyun-arg teamID=foreign`,
		`ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}' --api-param teamID=team-owned --api-param teamID=foreign`,
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("unclassified DeleteTeam launched: %s", command)
				return execpkg.Result{}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "raw delete", Run: command}, time.Second)
			if ok || len(quotaJournal(t, path).Entries) != 1 {
				t.Fatalf("unclassified raw DeleteTeam changed ownership: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
			}
		})
	}
}

func TestRawDeleteTeamWaitsForOwnedQuotaChild(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
		t.Fatalf("Quota update prerequisite failed: %+v", step)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("raw DeleteTeam launched before Quota child cleanup: %s", command)
		return execpkg.Result{}
	}
	raw := scenario.Step{Name: "raw delete", Run: `ecctl call fcsandbox DeleteTeam --request '{"teamID":"team-owned"}'`}
	step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, raw, time.Second)
	if ok || !strings.Contains(step.Error, "Quota child") || len(quotaJournal(t, path).Entries) != 2 {
		t.Fatalf("raw DeleteTeam crossed Quota child: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
	}
}

func TestRawDeleteTeamInterspersedOutputKeepsQuotaChildAndAttemptFence(t *testing.T) {
	for _, command := range []string{
		`ecctl call -o json fcsandbox DeleteTeam --request '{"teamID":"team-owned"}'`,
		`ecctl call fcsandbox -o=json DeleteTeam --request '{"teamID":"team-owned"}'`,
		`ecctl call --aliyun-arg ignored=1 fcsandbox DeleteTeam --request '{"teamID":"team-owned"}'`,
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
			}
			if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
				t.Fatalf("quota setup: %+v", step)
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("raw Team delete crossed child: %s", command)
				return execpkg.Result{}
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "raw delete", Run: command}, time.Second)
			entries := quotaJournal(t, path).Entries
			if ok || len(entries) != 2 || !entries[0].TeamCreateConfirmed || entries[0].TeamReplayAttempted || !entries[1].QuotaReserved || entries[1].QuotaUpdatePending {
				t.Fatalf("raw DeleteTeam bypassed child: step=%+v entries=%+v", step, entries)
			}
		})
	}
	for _, command := range []string{
		`ecctl call -o json fcsandbox DeleteTeam --request '{"teamID":"team-owned"}'`,
		`ecctl call fcsandbox -o=json DeleteTeam --request '{"teamID":"team-owned"}'`,
	} {
		t.Run(command+"/accepted", func(t *testing.T) {
			cfg, cl, scope, data, _, path := quotaCleanupHarness(t)
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamReplayAttempted {
					t.Fatalf("Team delete started without durable attempt: %+v", entries)
				}
				return rawTeamDeleteResult(command, cfg.Region, "200")
			}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, scenario.Step{Name: "raw delete", Run: command}, time.Second)
			entries := quotaJournal(t, path).Entries
			if !ok || calls != 1 || len(entries) != 1 || !entries[0].TeamReplayAttempted || (*scope)[0].done {
				t.Fatalf("accepted raw delete lost Team owner: step=%+v calls=%d entries=%+v", step, calls, entries)
			}
		})
	}
}

func TestFreshRunRejectsRetainedPendingQuotaBeforeTeamAllocation(t *testing.T) {
	cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown Quota result")}
	}
	if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); ok || !strings.Contains(step.Error, "pending") {
		t.Fatalf("pending prerequisite missing: %+v", step)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	intentsBefore, err := filepath.Glob(path + ".team-create-*.json")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fresh.Steps = fresh.Steps[:2]
	logPath := filepath.Join(filepath.Dir(cfg.Bin), "provider-calls")
	if err := os.WriteFile(cfg.Bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FAKE_LOG\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	run, runErr := Run(context.Background(), Options{RunName: "same", RunID: "quota-run", ExecutionID: "quota-execution", Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, CleanupJournal: path, Keep: true, StepTimeout: time.Second, Suites: []*scenario.Suite{fresh}})
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	intentsAfter, err := filepath.Glob(path + ".team-create-*.json")
	if err != nil {
		t.Fatal(err)
	}
	_, providerErr := os.Stat(logPath)
	if runErr == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "Quota credential binding is not owned by this run") || !bytes.Equal(after, before) || len(intentsAfter) != len(intentsBefore) || !os.IsNotExist(providerErr) {
		t.Fatalf("fresh run crossed durable pending fence: run=%+v err=%v journal=%s intents=%v", run, runErr, after, intentsAfter)
	}
}

func TestTeamCreateAdmissionWaitsForReplayFenceThroughRegistration(t *testing.T) {
	cfg, cl, _, _, suite, path := quotaCleanupHarness(t)
	data := map[string]any{"run_name": "second-team"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		if entries := quotaJournal(t, path).Entries; len(entries) != 1 {
			t.Fatalf("Team create ran after unexpected replay: %+v", entries)
		}
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-second", "status": "active"}}}
	}
	if err := journalfile.WithReplayLock(context.Background(), path, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		step, ok := runStep(ctx, Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, &scope, data, suite.Steps[0], time.Second)
		if ok || !strings.Contains(step.Error, "context deadline exceeded") || providerCalls != 0 || len(scope) != 0 {
			t.Fatalf("Team create did not wait for replay lock: step=%+v calls=%d scope=%+v", step, providerCalls, scope)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("held replay lock changed ownership: %s %v", after, err)
	}
	assertFenceHeld := func(stage string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		err := journalfile.WithReplayLock(ctx, path, func() error { return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Team replay fence released during %s: %v", stage, err)
		}
	}
	previousRegistration := writeTeamRegistrationJournal
	t.Cleanup(func() { writeTeamRegistrationJournal = previousRegistration })
	writeTeamRegistrationJournal = func(path string, journal report.CleanupJournal) error {
		assertFenceHeld("registration")
		return previousRegistration(path, journal)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		providerCalls++
		assertFenceHeld("provider invocation")
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-second", "status": "active"}}}
	}
	if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, &scope, data, suite.Steps[0], time.Second); !ok || providerCalls != 1 || len(scope) != 1 || !scope[0].teamCreateConfirmed || !scope[0].quotaBound || len(quotaJournal(t, path).Entries) != 2 {
		t.Fatalf("clean matching Team create rejected: step=%+v calls=%d scope=%+v entries=%+v", step, providerCalls, scope, quotaJournal(t, path).Entries)
	}
}

func TestRawDeleteTeamTrimmedOperationKeepsOwnedFences(t *testing.T) {
	for _, selector := range []string{"fcsandbox ' DeleteTeam'", "' FCSandbox ' 'deleteteam '"} {
		t.Run(selector+"/child", func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
			}
			if step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); !ok {
				t.Fatalf("quota setup: %+v", step)
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("raw Team delete crossed child: %s", command)
				return execpkg.Result{}
			}
			raw := scenario.Step{Name: "raw delete", Run: `ecctl call ` + selector + ` --request '{"teamID":"team-owned"}' --output=json`}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, raw, time.Second)
			if ok || !strings.Contains(step.Error, "Quota child") || len(quotaJournal(t, path).Entries) != 2 || quotaJournal(t, path).Entries[0].TeamReplayAttempted {
				t.Fatalf("trimmed raw operation bypassed child: step=%+v entries=%+v", step, quotaJournal(t, path).Entries)
			}
		})
		t.Run(selector+"/attempt", func(t *testing.T) {
			cfg, cl, scope, data, suite, path := quotaCleanupHarness(t)
			calls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				calls++
				if entries := quotaJournal(t, path).Entries; len(entries) != 1 || !entries[0].TeamReplayAttempted {
					t.Fatalf("raw call started without durable attempt: %+v", entries)
				}
				return execpkg.Result{Command: command, Started: true, Exit: -1, Err: errors.New("unknown raw result")}
			}
			raw := scenario.Step{Name: "raw delete", Run: `ecctl call ` + selector + ` --request '{"teamID":"team-owned"}' --output=json`}
			step, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, raw, time.Second)
			if ok || calls != 1 || !strings.Contains(step.Error, "unknown raw result") {
				t.Fatalf("raw attempt not fenced: step=%+v calls=%d", step, calls)
			}
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				t.Fatalf("Quota mutation started after raw attempt: %s", command)
				return execpkg.Result{}
			}
			if update, ok := runStep(context.Background(), Options{}, cfg, cl, scope, data, suite.Steps[1], time.Second); ok || !strings.Contains(update.Error, "Team") {
				t.Fatalf("Quota reservation accepted after raw attempt: %+v", update)
			}
		})
	}
}

func TestDAGDeleteCannotOvertakePendingProtectedTeamCreate(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	journalPath := filepath.Join(dir, "cleanup.json")
	operations := newOperationRuntime(4)
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, operations, true, journalPath,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	loaded, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	create := loaded.Steps[0]
	create.Name = "create"
	list := scenario.Step{Name: "list", Run: "ecctl sandbox team list --output=json", At: "$.team", Capture: map[string]string{"team_id": "id"}}
	deleteStep := scenario.Step{Name: "delete", DependsOn: []string{"list"}, Run: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
	update := loaded.Steps[1]
	update.Name = "update"
	update.DependsOn = []string{"delete"}
	suite := &scenario.Suite{Execution: scenario.ExecutionDAG, Steps: []scenario.Step{create, list, deleteStep, update}}
	base := map[string]any{"run_name": "dag-owned-team"}
	if err := teamOwnership(base); err != nil {
		t.Fatal(err)
	}
	createEntered := make(chan struct{})
	releaseCreate := make(chan struct{})
	deleteWaiting := make(chan struct{}, 1)
	var unexpectedCalls atomic.Int32
	var deleteCalls atomic.Int32
	previousHook := teamDeleteBeforeReplayLock
	teamDeleteBeforeReplayLock = func() {
		select {
		case deleteWaiting <- struct{}{}:
		default:
		}
	}
	defer func() { teamDeleteBeforeReplayLock = previousHook }()
	cl.runCommand = func(commandCtx context.Context, _ execpkg.Config, command string) execpkg.Result {
		path, parseErr := commandAction(command)
		if parseErr != nil {
			unexpectedCalls.Add(1)
			return execpkg.Result{Command: command, Started: true, Exit: 90, Err: parseErr}
		}
		switch strings.Join(path, " ") {
		case "sandbox team create":
			close(createEntered)
			select {
			case <-releaseCreate:
			case <-commandCtx.Done():
				return execpkg.Result{Command: command, Started: true, Exit: 1, Err: commandCtx.Err()}
			}
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
		case "sandbox team list":
			select {
			case <-createEntered:
			case <-commandCtx.Done():
				return execpkg.Result{Command: command, Started: true, Exit: 1, Err: commandCtx.Err()}
			}
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned"}}}
		case "sandbox team delete":
			deleteCalls.Add(1)
			if entries := quotaJournal(t, journalPath).Entries; len(entries) != 1 || !entries[0].TeamCreateConfirmed || !entries[0].TeamReplayAttempted {
				return execpkg.Result{Command: command, Started: true, Exit: 90, Err: fmt.Errorf("Team delete ran before durable registration and attempt: %+v", entries)}
			}
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
		default:
			unexpectedCalls.Add(1)
			return execpkg.Result{Command: command, Started: true, Exit: 90, Err: errors.New("unexpected Quota update subprocess")}
		}
	}
	var scope []*cleanupItem
	caseResult := &report.Case{}
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		runDAGCase(ctx, Options{Logf: func(string, ...any) {}}, cfg, cl, operations, &scope, base, nil, nil, suite, time.Second, caseResult)
		close(done)
	}()
	select {
	case <-deleteWaiting:
	case <-ctx.Done():
		close(releaseCreate)
		<-done
		t.Fatal("DAG delete did not reach replay-lock admission")
	}
	if deleteCalls.Load() != 0 {
		close(releaseCreate)
		<-done
		t.Fatal("DAG delete ran before protected Team registration")
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, 30*time.Millisecond)
	probeErr := journalfile.WithReplayLock(probeCtx, journalPath, func() error { return nil })
	cancelProbe()
	if !errors.Is(probeErr, context.DeadlineExceeded) {
		close(releaseCreate)
		<-done
		t.Fatalf("DAG create did not hold the replay fence: %v", probeErr)
	}
	close(releaseCreate)
	<-done
	if unexpectedCalls.Load() != 0 {
		t.Fatalf("DAG launched %d unexpected subprocesses: %+v", unexpectedCalls.Load(), caseResult.Steps)
	}
	if len(caseResult.Steps) != 4 || caseResult.Steps[0].Status != report.StatusPass || caseResult.Steps[1].Status != report.StatusPass || caseResult.Steps[2].Status != report.StatusPass || caseResult.Steps[3].Status != report.StatusError || !strings.Contains(caseResult.Steps[3].Error, "no confirmed run-owned Team") || deleteCalls.Load() != 1 {
		t.Fatalf("DAG did not preserve Team ownership ordering: %+v deletes=%d", caseResult.Steps, deleteCalls.Load())
	}
	entries := quotaJournal(t, journalPath).Entries
	if len(entries) != 0 {
		t.Fatalf("DAG left invalid Team ownership: %+v", entries)
	}
}

func TestDAGUncertainQuotaConsumptionBlocksSecondReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("external replay fake uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "ecctl"), Region: "cn-hangzhou"}
	configureQuotaProbeFixture(t, dir, cfg.Bin)
	journalPath := filepath.Join(dir, "cleanup.json")
	operations := newOperationRuntime(4)
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, operations, true, journalPath,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	loaded, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	create := loaded.Steps[0]
	create.Name = "create"
	initial := loaded.Steps[1]
	initial.Name, initial.DependsOn = "initial update", []string{"create"}
	deleteStep := loaded.Steps[4]
	deleteStep.Name, deleteStep.DependsOn = "delete", []string{"initial update"}
	second := loaded.Steps[1]
	second.Name, second.DependsOn = "second update", []string{"initial update"}
	suite := &scenario.Suite{Execution: scenario.ExecutionDAG, Steps: []scenario.Step{create, initial, deleteStep, second}}
	base := map[string]any{"run_name": "quota-race"}
	if err := teamOwnership(base); err != nil {
		t.Fatal(err)
	}
	secondWaiting := make(chan struct{})
	releaseSecond := make(chan struct{})
	deleteFinished := make(chan struct{})
	previousLockHook := quotaBeforeReplayLock
	var lockCalls atomic.Int32
	quotaBeforeReplayLock = func() {
		if lockCalls.Add(1) == 2 {
			close(secondWaiting)
			<-releaseSecond
		}
	}
	defer func() { quotaBeforeReplayLock = previousLockHook }()
	previousWrite := writeQuotaConsumptionJournal
	var writes atomic.Int32
	writeQuotaConsumptionJournal = func(path string, journal report.CleanupJournal) error {
		if err := writeRunnerJournal(path, journal); err != nil {
			return err
		}
		if writes.Add(1) == 1 {
			return errors.New("injected post-rename directory sync failure")
		}
		return nil
	}
	defer func() { writeQuotaConsumptionJournal = previousWrite }()
	previousCompletion := dagStepAfterCompletion
	dagStepAfterCompletion = func(name string) {
		if name == "delete" {
			close(deleteFinished)
		}
	}
	defer func() { dagStepAfterCompletion = previousCompletion }()
	var updateCalls, deleteCalls atomic.Int32
	cl.runCommand = func(commandCtx context.Context, _ execpkg.Config, command string) execpkg.Result {
		path, err := commandAction(command)
		if err != nil || len(path) != 3 {
			return execpkg.Result{Command: command, Started: true, Exit: 90, Err: fmt.Errorf("unexpected DAG command: %s", command)}
		}
		switch path[1] + "/" + path[2] {
		case "team/create":
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
		case "quota/update":
			updateCalls.Add(1)
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"quota": map[string]any{"id": "team-owned", "team_id": "team-owned", "cpu_cores": float64(32), "memory_gb": float64(64)}}}
		case "quota/delete":
			deleteCalls.Add(1)
			select {
			case <-secondWaiting:
			case <-commandCtx.Done():
				return execpkg.Result{Command: command, Started: true, Exit: 1, Err: commandCtx.Err()}
			}
			return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
		}
		return execpkg.Result{Command: command, Started: true, Exit: 90, Err: fmt.Errorf("unexpected DAG command: %s", command)}
	}
	var scope []*cleanupItem
	caseResult := &report.Case{}
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		runDAGCase(ctx, Options{quotaLifecycle: true, quotaLifecycleKnown: true, Logf: func(string, ...any) {}}, cfg, cl, operations, &scope, base, nil, nil, suite, time.Second, caseResult)
		close(done)
	}()
	select {
	case <-deleteFinished:
	case <-ctx.Done():
		close(releaseSecond)
		<-done
		t.Fatal("Quota delete did not finish before the competing update")
	}
	close(releaseSecond)
	<-done
	if len(caseResult.Steps) != 4 || caseResult.Steps[0].Status != report.StatusPass || caseResult.Steps[1].Status != report.StatusPass || !strings.Contains(caseResult.Steps[2].Error, "injected post-rename directory sync failure") || !strings.Contains(caseResult.Steps[3].Error, "unfinished in-memory reservation") || updateCalls.Load() != 1 || deleteCalls.Load() != 1 || writes.Load() != 1 {
		t.Fatalf("DAG uncertainty admitted duplicate child: steps=%+v updateCalls=%d deleteCalls=%d writes=%d", caseResult.Steps, updateCalls.Load(), deleteCalls.Load(), writes.Load())
	}
	entries := quotaJournal(t, journalPath).Entries
	if len(scope) != 2 || scope[1].done || len(entries) != 1 || !entries[0].TeamCreateConfirmed {
		t.Fatalf("uncertain Quota consumption lost Team recovery: scope=%+v entries=%+v", scope, entries)
	}
	if err := os.WriteFile(cfg.Bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"sandbox team delete"*) echo '{"deleted":true}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	replayed, err := sweeper.ReplayJournalWithOptions(context.Background(), journalPath, sweeper.ReplayOptions{Config: cfg, RunID: "run", Surface: "public"})
	if err != nil || replayed == nil || replayed.Errors != 0 || replayed.Deleted != 1 || len(quotaJournal(t, journalPath).Entries) != 0 {
		t.Fatalf("external replay could not recover Team: result=%+v err=%v journal=%+v", replayed, err, quotaJournal(t, journalPath))
	}
}
