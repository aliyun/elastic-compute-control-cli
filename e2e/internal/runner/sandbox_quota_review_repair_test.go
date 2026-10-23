package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestUnresolvedQuotaTeamCreateFencesLaterCasesAndSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, tc := range []struct{ create, reconcile string }{
		{"first", "failed"}, {"first", "empty"},
		{"second", "failed"}, {"second", "empty"},
	} {
		t.Run(tc.create+"/"+tc.reconcile, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			configureQuotaProfileFixture(t, dir)
			bin := filepath.Join(dir, "ecctl")
			calls := filepath.Join(dir, "calls")
			creates := filepath.Join(dir, "creates")
			t.Setenv("FAKE_QUOTA_CALLS", calls)
			t.Setenv("FAKE_QUOTA_CREATES", creates)
			t.Setenv("FAKE_FAIL_CREATE", tc.create)
			t.Setenv("FAKE_RECONCILE", tc.reconcile)
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
printf '%s\n' "$*" >> "$FAKE_QUOTA_CALLS"
case "$*" in
  *"sandbox team create"*)
    echo create >> "$FAKE_QUOTA_CREATES"
    count=$(wc -l < "$FAKE_QUOTA_CREATES")
    if [ "$FAKE_FAIL_CREATE" = first ] || [ "$count" -eq 2 ]; then echo 'unknown Team create result' >&2; exit 1; fi
    echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox team list"*)
    if [ "$FAKE_RECONCILE" = failed ]; then exit 1; fi
    case "$*" in
      *"--filter name="*) echo '{"teams":[],"pagination":{"has_more":false}}';;
      *) echo '{"teams":[{"id":"foreign-team"}],"pagination":{"has_more":false}}';;
    esac;;
  *"sandbox quota update"*) echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}';;
  *"sandbox quota delete"*|*"sandbox team delete"*) echo '{"deleted":true}';;
  *) echo 'unexpected command' >&2; exit 91;;
esac
`), 0o755); err != nil {
				t.Fatal(err)
			}
			quota, err := scenario.Load("../../cases/sandbox/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			steps := 2
			wantIntents := 1
			if tc.create == "second" {
				steps = 3
				wantIntents = 2
			}
			quota.Steps = quota.Steps[:steps]
			foreign := &scenario.Suite{Resource: "sandbox/team", Serial: true, Steps: []scenario.Step{{
				Name: "foreign Team list", Run: "ecctl sandbox team list --all --output=json", At: "$.teams[0]",
				Capture: map[string]string{"team_id": "id"}, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m",
			}}}
			session := NewSession()
			journal := filepath.Join(dir, "cleanup.json")
			opt := Options{Session: session, Suites: []*scenario.Suite{quota, foreign}, RunName: "quota-unknown", RunID: "run", ExecutionID: "execution-1", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, CleanupJournal: journal, StepTimeout: 5 * time.Second}
			run, err := Run(context.Background(), opt)
			if err != nil || len(run.Cases) != 2 || run.Cases[0].Status == report.StatusPass || run.Cases[1].Status != report.StatusSkipped || len(run.Cases[1].Steps) != 0 {
				t.Fatalf("unresolved Team did not skip later case: run=%+v err=%v", run, err)
			}
			if entries := quotaJournal(t, journal).Entries; len(entries) != 0 {
				t.Fatalf("successful first pair was not cleaned before Session test: %+v", entries)
			}
			intents, err := filepath.Glob(journal + ".team-create-*.json")
			if err != nil || len(intents) != wantIntents {
				t.Fatalf("original Team intents were lost: %v %v", intents, err)
			}
			before, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			opt.Suites = []*scenario.Suite{foreign}
			opt.ExecutionID = "execution-2"
			opt.CleanupJournal = filepath.Join(dir, "next-cleanup.json")
			next, err := Run(context.Background(), opt)
			if err == nil || next == nil || len(next.Cases) != 1 || next.Cases[0].Resource != "setup" || !strings.Contains(err.Error(), "prior Quota execution retains") {
				t.Fatalf("next execution admitted a foreign capture: run=%+v err=%v", next, err)
			}
			after, err := os.ReadFile(calls)
			if err != nil || string(after) != string(before) || strings.Contains(string(after), "sandbox team delete foreign-team") {
				t.Fatalf("foreign Team was read or deleted: before=%q after=%q err=%v", before, after, err)
			}
		})
	}
}

func TestExactQuotaTeamReconciliationReleasesUnresolvedCreateFence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	configureQuotaProfileFixture(t, dir)
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"sandbox team list"*) printf '{"teams":[{"id":"team-owned","name":"%s","description":"%s"}],"pagination":{"has_more":false}}\n' "$FAKE_TEAM_NAME" "$FAKE_TEAM_OWNER";;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"run_name": "owned-quota"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TEAM_NAME", data["team_name"].(string))
	t.Setenv("FAKE_TEAM_OWNER", data["team_owner"].(string))
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	journal := filepath.Join(dir, "cleanup.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, journal,
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	suite, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scope := new([]*cleanupItem)
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 1, Err: errors.New("unknown Team create result")}
	}
	step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[0], 5*time.Second)
	if ok || !strings.Contains(step.Error, "journaled owned Team for cleanup") || cl.hasUnresolvedQuotaTeamCreate() || len(*scope) != 1 || !(*scope)[0].teamCreateConfirmed {
		t.Fatalf("exact read-only reconciliation did not register ownership: step=%+v scope=%+v", step, *scope)
	}
	cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
	}
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 || cl.hasRetainedQuotaOwnership() || len(quotaJournal(t, journal).Entries) != 0 {
		t.Fatalf("cleaned recovered Team retained a false Session fence: failures=%+v journal=%+v", failures, quotaJournal(t, journal))
	}
}

func TestQuotaUpdateNeverRecreatesMissingJournalParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, loseAtHook := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-update", true: "at-replay-hook"}[loseAtHook], func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			bin := filepath.Join(dir, "ecctl")
			configureQuotaProbeFixture(t, dir, bin)
			parent := filepath.Join(dir, "journal")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "cleanup.json")
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
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
			scope := new([]*cleanupItem)
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				return execpkg.Result{Command: command, Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "status": "active"}}}
			}
			if step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[0], 5*time.Second); !ok {
				t.Fatalf("owned Team setup failed: %+v", step)
			}
			moved := parent + "-moved"
			move := func() {
				if err := os.Rename(parent, moved); err != nil {
					t.Fatal(err)
				}
			}
			if loseAtHook {
				previous := quotaBeforeReplayLock
				quotaBeforeReplayLock = move
				t.Cleanup(func() { quotaBeforeReplayLock = previous })
			} else {
				move()
			}
			providerCalls := 0
			cl.runCommand = func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				providerCalls++
				return execpkg.Result{Command: command, Started: true, Exit: 0}
			}
			step, ok := runStep(context.Background(), Options{quotaLifecycle: true, quotaLifecycleKnown: true}, cfg, cl, scope, data, suite.Steps[1], 5*time.Second)
			if ok || providerCalls != 0 || !strings.Contains(step.Error, "parent") || len(*scope) != 1 {
				t.Fatalf("Quota update crossed lost parent: step=%+v providerCalls=%d scope=%+v", step, providerCalls, *scope)
			}
			if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Quota update recreated missing parent: %v", err)
			}
			if _, err := os.Stat(path + ".replay.lock"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Quota update created a new replay lock: %v", err)
			}
			if _, err := os.Stat(filepath.Join(moved, "cleanup.json")); err != nil {
				t.Fatalf("moved journal was lost: %v", err)
			}
			intents, err := filepath.Glob(filepath.Join(moved, "cleanup.json.team-create-*.json"))
			if err != nil || len(intents) != 1 {
				t.Fatalf("moved Team recovery intent was lost: %v %v", intents, err)
			}
		})
	}
}
