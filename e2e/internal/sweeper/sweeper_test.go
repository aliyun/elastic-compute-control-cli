package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	execpkg "ecctl/e2e/internal/exec"
	"ecctl/e2e/internal/report"
)

func TestReplayJournalRunsTeardownsInReverseOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	journal := filepath.Join(dir, "cleanup-journal.json")
	data, err := json.Marshal([]report.Resource{
		{Scope: "case", Teardown: "ecctl t thing delete first"},
		{Scope: "case", Teardown: "ecctl t thing delete second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, data, 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", log)
	res, err := ReplayJournal(context.Background(), journal, execpkg.Config{Bin: fake})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 2 || res.Errors != 0 {
		t.Fatalf("result = %+v", res)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(calls)); got != "t thing delete second\nt thing delete first" {
		t.Fatalf("calls = %q", got)
	}
}

func TestReplayJournalRejectsNonEcctlTeardown(t *testing.T) {
	j := filepath.Join(t.TempDir(), "cleanup-journal.json")
	data, err := json.Marshal([]report.Resource{{Scope: "case", Teardown: "/bin/sh -c 'touch /tmp/should-not-run'"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ReplayJournal(context.Background(), j, execpkg.Config{Bin: "ecctl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 || res.Errors != 1 {
		t.Fatalf("result = %+v, want one rejected entry", res)
	}
	if !strings.Contains(res.Details[0], "invalid teardown") {
		t.Fatalf("detail = %q, want invalid teardown", res.Details[0])
	}
}

func TestReplayJournalRejectsEcctlCommandsThatAreNotDeletes(t *testing.T) {
	j := filepath.Join(t.TempDir(), "cleanup-journal.json")
	data, err := json.Marshal([]report.Resource{{Scope: "case", Teardown: "ecctl ecs instance get i-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := ReplayJournal(context.Background(), j, execpkg.Config{Bin: "ecctl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 || res.Errors != 1 || !strings.Contains(res.Details[0], "delete command") {
		t.Fatalf("result = %+v, want non-delete rejection", res)
	}
}

func TestReplayJournalRejectsMismatchedRunIdentity(t *testing.T) {
	j := filepath.Join(t.TempDir(), "cleanup-journal.json")
	data, err := json.Marshal(report.CleanupJournal{
		Version: 1, RunID: "run-a", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/bin/ecctl-public",
		Entries: []report.Resource{{Scope: "case", Teardown: "ecctl ecs instance delete i-1 --force"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, opt := range map[string]ReplayOptions{
		"run":     {RunID: "run-b"},
		"region":  {Config: execpkg.Config{Region: "cn-beijing"}},
		"surface": {Surface: "full"},
		"binary":  {Config: execpkg.Config{Bin: "/bin/ecctl-full"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReplayJournalWithOptions(context.Background(), j, opt); err == nil {
				t.Fatal("expected journal identity mismatch")
			}
		})
	}
}

func deleteTasks(n int) []deleteTask {
	tasks := make([]deleteTask, n)
	for i := range tasks {
		tasks[i] = deleteTask{kind: "ecs-instance", id: fmt.Sprintf("i-%d", i), reason: "test", cmd: "ecctl ecs instance delete x --force"}
	}
	return tasks
}

func TestRunDeletesBoundsConcurrency(t *testing.T) {
	var inFlight, maxSeen int32
	run := func(_ context.Context, _ deleteTask) execpkg.Result {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return execpkg.Result{Exit: 0}
	}

	res := &Result{}
	var mu sync.Mutex
	runDeletes(context.Background(), deleteTasks(10), 4, res, &mu, func(string, ...any) {}, run)

	if res.Deleted != 10 || res.Errors != 0 {
		t.Fatalf("res = %+v, want Deleted=10 Errors=0", res)
	}
	if got := atomic.LoadInt32(&maxSeen); got != 4 {
		t.Fatalf("max in-flight = %d, want exactly the concurrency limit 4", got)
	}
}

func TestRunDeletesAccumulatesFailures(t *testing.T) {
	run := func(_ context.Context, _ deleteTask) execpkg.Result {
		return execpkg.Result{
			Exit: 1,
			JSON: map[string]any{"error": map[string]any{"code": "IncorrectInstanceStatus", "message": "running"}},
		}
	}

	res := &Result{}
	var mu sync.Mutex
	runDeletes(context.Background(), deleteTasks(4), 8, res, &mu, func(string, ...any) {}, run)

	if res.Deleted != 0 || res.Errors != 4 {
		t.Fatalf("res = %+v, want Deleted=0 Errors=4", res)
	}
	if len(res.Details) != 4 {
		t.Fatalf("Details = %d entries, want 4", len(res.Details))
	}
	if !strings.Contains(res.Details[0], "IncorrectInstanceStatus") {
		t.Fatalf("detail = %q, want it to carry the ecctl error", res.Details[0])
	}
}

func TestRunDeletesEmptyIsNoop(t *testing.T) {
	res := &Result{}
	var mu sync.Mutex
	called := false
	runDeletes(context.Background(), nil, 4, res, &mu, func(string, ...any) {}, func(context.Context, deleteTask) execpkg.Result {
		called = true
		return execpkg.Result{}
	})
	if called || res.Deleted != 0 {
		t.Fatalf("expected no work for empty task list (called=%v res=%+v)", called, res)
	}
}

func TestFailureReasonPrefersStdoutErrorJSON(t *testing.T) {
	// ecctl writes its structured error to stdout, not stderr.
	r := execpkg.Result{
		Exit: 1,
		JSON: map[string]any{
			"error": map[string]any{
				"kind":             "client",
				"code":             "InvalidFilter",
				"message":          "unsupported filter tag",
				"suggested_action": "Run `ecctl schema ecs.instance.list` to list supported filters.",
			},
		},
	}
	got := failureReason(r)
	for _, want := range []string{"InvalidFilter", "unsupported filter tag", "supported filters"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reason = %q, want it to contain %q", got, want)
		}
	}
}

func TestFailureReasonFallsBackToStderr(t *testing.T) {
	r := execpkg.Result{
		Exit:   1,
		Stderr: "panic: boom\n",
	}
	if got := failureReason(r); got != "panic: boom" {
		t.Fatalf("reason = %q, want stderr fallback", got)
	}
}

func TestFailureReasonFallsBackToErr(t *testing.T) {
	r := execpkg.Result{Exit: -1, Err: errors.New("context deadline exceeded")}
	if got := failureReason(r); got != "context deadline exceeded" {
		t.Fatalf("reason = %q, want the process error", got)
	}
}

func TestFailureReasonNoSignal(t *testing.T) {
	if got := failureReason(execpkg.Result{Exit: 1}); got != "(no output)" {
		t.Fatalf("reason = %q, want placeholder", got)
	}
}

func TestEcctlErrorMessageIgnoresNonErrorJSON(t *testing.T) {
	if got := ecctlErrorMessage(map[string]any{"instances": []any{}}); got != "" {
		t.Fatalf("ecctlErrorMessage = %q, want empty for a normal payload", got)
	}
	if got := ecctlErrorMessage("not-a-map"); got != "" {
		t.Fatalf("ecctlErrorMessage = %q, want empty for non-object JSON", got)
	}
}

func TestParseTimestampAcceptsAliyunFormats(t *testing.T) {
	for _, in := range []string{
		"2026-06-29T06:04Z",         // Aliyun default (minute precision, no seconds)
		"2026-06-29T06:04:05Z",      // with seconds
		"2026-06-29T06:04:05+08:00", // RFC3339 with offset
		"2026-06-29 06:04:05",       // space-separated
	} {
		if _, err := parseTimestamp(in); err != nil {
			t.Fatalf("parseTimestamp(%q) error: %v", in, err)
		}
	}
}

func TestParseTimestampRejectsGarbage(t *testing.T) {
	if _, err := parseTimestamp("not-a-time"); err == nil {
		t.Fatalf("parseTimestamp(garbage): expected error")
	}
}

func TestFlattenLineCollapsesAndTruncates(t *testing.T) {
	if got := flattenLine("  foo\n\tbar   baz \n"); got != "foo bar baz" {
		t.Fatalf("flattenLine = %q, want collapsed whitespace", got)
	}
	long := strings.Repeat("x", 400)
	got := flattenLine(long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 301 {
		t.Fatalf("flattenLine truncation = %d runes (%q…)", len([]rune(got)), got[:10])
	}
}

func TestRunDeletesRetriesTransientDependencyViolation(t *testing.T) {
	orig := sweepRetrySleep
	sweepRetrySleep = func(time.Duration) {} // no real backoff in tests
	defer func() { sweepRetrySleep = orig }()

	var calls int32
	run := func(_ context.Context, _ deleteTask) execpkg.Result {
		if atomic.AddInt32(&calls, 1) < 3 {
			return execpkg.Result{Exit: 2, JSON: map[string]any{"error": map[string]any{"code": "DependencyViolation", "message": "资源存在依赖"}}}
		}
		return execpkg.Result{Exit: 0}
	}

	res := &Result{}
	var mu sync.Mutex
	runDeletes(context.Background(), deleteTasks(1), 4, res, &mu, func(string, ...any) {}, run)

	if res.Deleted != 1 || res.Errors != 0 {
		t.Fatalf("res = %+v, want Deleted=1 Errors=0 after transient retries", res)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("attempts = %d, want 3 (2 dependency failures + success)", got)
	}
}

func TestRunDeletesDoesNotRetryNonTransient(t *testing.T) {
	orig := sweepRetrySleep
	sweepRetrySleep = func(time.Duration) {}
	defer func() { sweepRetrySleep = orig }()

	var calls int32
	run := func(_ context.Context, _ deleteTask) execpkg.Result {
		atomic.AddInt32(&calls, 1)
		return execpkg.Result{Exit: 1, JSON: map[string]any{"error": map[string]any{"code": "InvalidParameter", "message": "bad"}}}
	}
	res := &Result{}
	var mu sync.Mutex
	runDeletes(context.Background(), deleteTasks(1), 4, res, &mu, func(string, ...any) {}, run)

	if res.Errors != 1 || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("non-transient must not retry: res=%+v calls=%d", res, calls)
	}
}

func TestCheckConfigAcceptsLiveCreateWithTeardownAndSweepKind(t *testing.T) {
	root, cases, config := writeSweepCheckFixture(t, sweepCheckCase(true), sweepCheckConfig(true))

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected valid sweep check for %s, got %+v", root, rep)
	}
	if rep.LiveCreates != 1 || rep.SweepKinds != 1 {
		t.Fatalf("unexpected counts: %+v", rep)
	}
}

func TestCheckConfigRequiresCleanupForEveryCreate(t *testing.T) {
	_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(false), sweepCheckConfig(false))

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSweepCheckCode(rep, "missing_teardown") || !hasSweepCheckCode(rep, "missing_sweep_kind") {
		t.Fatalf("every create should require teardown and sweep coverage, got %+v", rep.Errors)
	}
}

func TestCheckConfigRejectsCleanupProblems(t *testing.T) {
	tests := []struct {
		name   string
		caseY  string
		config string
		code   string
	}{
		{
			name:   "missing teardown",
			caseY:  sweepCheckCase(false),
			config: sweepCheckConfig(true),
			code:   "missing_teardown",
		},
		{
			name:   "missing sweep kind",
			caseY:  sweepCheckCase(true),
			config: sweepCheckConfig(false),
			code:   "missing_sweep_kind",
		},
		{
			name:   "missing run-id selector",
			caseY:  sweepCheckCase(true),
			config: strings.ReplaceAll(sweepCheckConfig(true), "    runid_field: tags.run-id\n", ""),
			code:   "missing_run_id_selector",
		},
		{
			name:   "missing delete command",
			caseY:  sweepCheckCase(true),
			config: strings.ReplaceAll(sweepCheckConfig(true), "    delete: \"ecctl ecs instance delete {{.id}} --force\"\n", ""),
			code:   "missing_delete_command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cases, config := writeSweepCheckFixture(t, tt.caseY, tt.config)
			rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
			if err != nil {
				t.Fatal(err)
			}
			if !hasSweepCheckCode(rep, tt.code) {
				t.Fatalf("expected %q, got %+v", tt.code, rep.Errors)
			}
		})
	}
}

func TestCheckConfigAcceptsAllowedNonSweepableReason(t *testing.T) {
	_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(true), sweepCheckNonSweepableConfig("provider-no-delete"))

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected valid non-sweepable cleanup coverage, got %+v", rep.Errors)
	}
}

func TestCheckConfigAcceptsKubeconfigRuntimeFinalizer(t *testing.T) {
	caseY := `
resource: ack/kubeconfig
steps:
  - name: create
    run: ecctl ack kubeconfig create --cluster c-test
    teardown: ecctl ack kubeconfig revoke --cluster c-test
  - name: revoke
    run: ecctl ack kubeconfig revoke --cluster c-test
`
	configY := sweepCheckConfig(false) + `non_sweepable:
  - resource: ack/kubeconfig
    reason: provider-limitation
    review_after: 2026-10-06
`
	_, cases, config := writeSweepCheckFixture(t, caseY, configY)

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 {
		t.Fatalf("runtime revoke finalizer should satisfy cleanup check: %+v", rep.Errors)
	}
}

func TestCheckConfigRejectsInvalidNonSweepableReason(t *testing.T) {
	_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(true), sweepCheckNonSweepableConfig("because"))

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSweepCheckCode(rep, "invalid_non_sweepable_reason") {
		t.Fatalf("expected invalid_non_sweepable_reason, got %+v", rep.Errors)
	}
}

func writeSweepCheckFixture(t *testing.T, caseY, configY string) (root, cases, config string) {
	t.Helper()
	root = t.TempDir()
	cases = filepath.Join(root, "cases")
	config = filepath.Join(root, "sweep.yaml")
	mustMkdirSweep(t, filepath.Join(cases, "ecs"))
	mustWriteSweep(t, filepath.Join(cases, "ecs", "instance.yaml"), caseY)
	mustWriteSweep(t, config, configY)
	return root, cases, config
}

func sweepCheckCase(withTeardown bool) string {
	teardown := ""
	if withTeardown {
		teardown = "\n    teardown: ecctl ecs instance delete {{.instance_id}} --force"
	}
	return `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id` + teardown + `
`
}

func sweepCheckConfig(includeKind bool) string {
	if !includeKind {
		return "kinds:\n  - name: ecs-disk\n    resource: ecs/disk\n    list: ecctl ecs disk list --filter tag.ecctl-e2e=1\n    items_path: $.disks\n    id_field: id\n    runid_field: tags.run-id\n    created_field: creation_time\n    delete: \"ecctl ecs disk delete {{.id}}\"\n"
	}
	return "kinds:\n  - name: ecs-instance\n    resource: ecs/instance\n    list: ecctl ecs instance list --filter tag.ecctl-e2e=1\n    items_path: $.instances\n    id_field: id\n    runid_field: tags.run-id\n    created_field: creation_time\n    delete: \"ecctl ecs instance delete {{.id}} --force\"\n"
}

func sweepCheckNonSweepableConfig(reason string) string {
	return sweepCheckConfig(false) + "non_sweepable:\n  - resource: ecs/instance\n    reason: " + reason + "\n    review_after: 2026-10-06\n"
}

func hasSweepCheckCode(rep *CheckReport, code string) bool {
	for _, err := range rep.Errors {
		if err.Code == code {
			return true
		}
	}
	return false
}

func mustMkdirSweep(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteSweep(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
