package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	runnerpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/runner"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
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
	var remaining []report.Resource
	data, err = os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("successful legacy replay retained entries: %s", data)
	}
}

func TestReplayJournalRunsControlledAssociatedTransferRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	journal := filepath.Join(dir, "cleanup-journal.json")
	data, err := json.Marshal(report.CleanupJournal{
		Version: 2,
		Entries: []report.Resource{{
			Scope:    "case",
			Teardown: "ecctl rg associated-transfer update --status Disable --enable-existing-resources-transfer false",
		}},
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
	if res.Deleted != 1 || res.Errors != 0 {
		t.Fatalf("result = %+v", res)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(calls)), "rg associated-transfer update --status Disable --enable-existing-resources-transfer false"; got != want {
		t.Fatalf("call = %q, want %q", got, want)
	}
	data, err = os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	var completed report.CleanupJournal
	if err := json.Unmarshal(data, &completed); err != nil {
		t.Fatal(err)
	}
	if len(completed.Entries) != 0 {
		t.Fatalf("successful restore replay retained entries: %s", data)
	}
	if _, err := ReplayJournal(context.Background(), journal, execpkg.Config{Bin: fake}); err != nil {
		t.Fatal(err)
	}
	calls, err = os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.TrimSpace(string(calls)), "associated-transfer update"); got != 1 {
		t.Fatalf("restore replay count = %d, want one-shot journal; calls=%s", got, calls)
	}
}

func TestReplayJournalDoesNotLoseConcurrentRunnerRegistration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	for _, legacy := range []bool{false, true} {
		for _, replayFails := range []bool{false, true} {
			name := "versioned"
			if legacy {
				name = "legacy"
			}
			if replayFails {
				name += "-failed-old-entry"
			}
			t.Run(name, func(t *testing.T) {
				if replayFails {
					t.Setenv("REPLAY_FAIL", "1")
				}
				dir := t.TempDir()
				journalPath := filepath.Join(dir, "cleanup-journal.json")
				oldEntry := report.Resource{Scope: "old", Teardown: "ecctl t thing delete old"}
				var initial any = report.CleanupJournal{
					Version: 2, RunID: "test", Surface: "public", Entries: []report.Resource{oldEntry},
				}
				if legacy {
					initial = []report.Resource{oldEntry}
				}
				data, err := json.Marshal(initial)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(journalPath, data, 0o600); err != nil {
					t.Fatal(err)
				}

				fake := filepath.Join(dir, "ecctl")
				if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
if [[ "$*" == *"thing create"* ]]; then
  : > "$WRITER_STARTED"
  echo '{"resource":{"id":"new"}}'
else
  echo '{}'
  if [[ "$REPLAY_FAIL" == "1" ]]; then
    exit 1
  fi
fi
`), 0o755); err != nil {
					t.Fatal(err)
				}

				replayRead := make(chan struct{})
				resumeReplay := make(chan struct{})
				releaseReplay := sync.OnceFunc(func() { close(resumeReplay) })
				defer releaseReplay()
				previousHook := replayJournalAfterRead
				replayJournalAfterRead = func(string) {
					close(replayRead)
					<-resumeReplay
				}
				defer func() { replayJournalAfterRead = previousHook }()

				replayDone := make(chan error, 1)
				go func() {
					_, err := ReplayJournal(context.Background(), journalPath, execpkg.Config{Bin: fake})
					replayDone <- err
				}()
				<-replayRead

				writerDone := make(chan error, 1)
				writerStarted := filepath.Join(dir, "writer-started")
				go func() {
					run, err := runnerpkg.Run(context.Background(), runnerpkg.Options{
						CleanupJournal: journalPath,
						RunName:        "test",
						RunID:          "test",
						Surface:        "public",
						EcctlBin:       fake,
						StepTimeout:    30 * time.Second,
						Keep:           true,
						Env:            []string{"WRITER_STARTED=" + writerStarted},
						Suites: []*scenario.Suite{{
							Surface: scenario.SurfacePublic, Resource: "t/thing", Path: "concurrent-writer.yaml",
							Steps: []scenario.Step{{
								Name: "create", Run: "ecctl t thing create", At: "$.resource",
								Capture: map[string]string{"id": "id"}, Teardown: "ecctl t thing delete {{.id}}",
							}},
						}},
					})
					if err == nil && run.Failed() {
						err = fmt.Errorf("runner failed: %+v", run.Cases)
					}
					writerDone <- err
				}()
				waitForFile(t, writerStarted, 10*time.Second)

				select {
				case err := <-writerDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					releaseReplay()
					if err := <-replayDone; err != nil {
						t.Fatal(err)
					}
					if err := <-writerDone; err != nil {
						t.Fatal(err)
					}
					t.Fatal("runner journal registration blocked behind replay work")
				}
				releaseReplay()
				if err := <-replayDone; err != nil {
					t.Fatal(err)
				}

				data, err = os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				var journal report.CleanupJournal
				if err := json.Unmarshal(data, &journal); err != nil {
					t.Fatalf("final journal is not versioned: %v\n%s", err, data)
				}
				wantTeardowns := []string{"ecctl t thing delete new"}
				if replayFails {
					wantTeardowns = []string{"ecctl t thing delete old", "ecctl t thing delete new"}
				}
				if got := journalTeardowns(journal.Entries); !slices.Equal(got, wantTeardowns) {
					t.Fatalf("final journal teardowns = %q, want %q: %s", got, wantTeardowns, data)
				}
			})
		}
	}
}

func journalTeardowns(entries []report.Resource) []string {
	result := make([]string, len(entries))
	for i, entry := range entries {
		result[i] = entry.Teardown
	}
	return result
}

func TestReplayJournalLockWaitHonorsContext(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	data, err := json.Marshal(report.CleanupJournal{Version: 2, Entries: []report.Resource{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	replayRead := make(chan struct{})
	resumeReplay := make(chan struct{})
	releaseReplay := sync.OnceFunc(func() { close(resumeReplay) })
	defer releaseReplay()
	previousHook := replayJournalAfterRead
	var hookCalls atomic.Int32
	replayJournalAfterRead = func(string) {
		if hookCalls.Add(1) == 1 {
			close(replayRead)
			<-resumeReplay
		}
	}
	defer func() { replayJournalAfterRead = previousHook }()

	firstDone := make(chan error, 1)
	go func() {
		_, err := ReplayJournal(context.Background(), journalPath, execpkg.Config{})
		firstDone <- err
	}()
	<-replayRead

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	secondDone := make(chan error, 1)
	go func() {
		_, err := ReplayJournal(ctx, journalPath, execpkg.Config{})
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second replay error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		releaseReplay()
		if err := <-firstDone; err != nil {
			t.Fatal(err)
		}
		if err := <-secondDone; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		t.Fatal("canceled replay remained blocked waiting for the journal lock")
	}

	releaseReplay()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestReplayJournalConsumesSuccessAfterCallerCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	data, err := json.Marshal(report.CleanupJournal{
		Version: 2,
		Entries: []report.Resource{{Scope: "case", Teardown: "ecctl t thing delete old"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	transactionHeld := make(chan struct{})
	releaseTransaction := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseTransaction) })
	defer release()
	holderDone := make(chan error, 1)
	successfulDelete := make(chan struct{})
	resumeReplay := make(chan struct{})
	previousHook := replayJournalAfterSuccess
	replayJournalAfterSuccess = func(path string) {
		go func() {
			holderDone <- journalfile.WithLock(context.Background(), path, func() error {
				close(transactionHeld)
				<-releaseTransaction
				return nil
			})
		}()
		<-transactionHeld
		close(successfulDelete)
		<-resumeReplay
	}
	defer func() { replayJournalAfterSuccess = previousHook }()

	ctx, cancel := context.WithCancel(context.Background())
	type replayOutcome struct {
		result *Result
		err    error
	}
	replayDone := make(chan replayOutcome, 1)
	go func() {
		result, err := ReplayJournal(ctx, journalPath, execpkg.Config{Bin: fake})
		replayDone <- replayOutcome{result: result, err: err}
	}()
	<-successfulDelete
	cancel()
	close(resumeReplay)

	select {
	case outcome := <-replayDone:
		release()
		if holderErr := <-holderDone; holderErr != nil {
			t.Fatal(holderErr)
		}
		t.Fatalf("replay returned before committing successful deletion: result=%+v err=%v", outcome.result, outcome.err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	if holderErr := <-holderDone; holderErr != nil {
		t.Fatal(holderErr)
	}
	outcome := <-replayDone
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if outcome.result.Deleted != 1 || outcome.result.Errors != 0 {
		t.Fatalf("result = %+v, want one successful deletion", outcome.result)
	}

	data, err = os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Entries) != 0 {
		t.Fatalf("successful deletion remained in journal after cancellation: %s", data)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", path)
		case <-ticker.C:
		}
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

func TestRenderDeleteCommandUsesOnlyExplicitItemFields(t *testing.T) {
	kind := Kind{
		Name:         "lingjun-subnet",
		Delete:       "ecctl lingjun subnet delete {{.id}} --vpd {{.vpd}} --zone {{.zone}}",
		DeleteFields: map[string]string{"vpd": "vpd", "zone": "zone"},
	}
	item := map[string]any{
		"id": "subnet-1", "vpd": "vpd-1", "zone": "cn-test-a",
		"secret": "must-not-be-exposed",
	}

	got, err := renderDeleteCommand(kind, item, "subnet-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ecctl lingjun subnet delete subnet-1 --vpd vpd-1 --zone cn-test-a"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestRenderDeleteCommandRejectsMissingMappedItemField(t *testing.T) {
	kind := Kind{
		Name:         "lingjun-subnet",
		Delete:       "ecctl lingjun subnet delete {{.id}} --vpd {{.vpd}} --zone {{.zone}}",
		DeleteFields: map[string]string{"vpd": "vpd", "zone": "zone"},
	}
	item := map[string]any{"id": "subnet-1", "vpd": "vpd-1"}

	if _, err := renderDeleteCommand(kind, item, "subnet-1"); err == nil || !strings.Contains(err.Error(), "zone") {
		t.Fatalf("error = %v, want missing zone before delete execution", err)
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
		{
			name:   "unmapped delete template field",
			caseY:  sweepCheckCase(true),
			config: strings.ReplaceAll(sweepCheckConfig(true), " --force\"", " --zone {{.zone}} --force\""),
			code:   "invalid_delete_template",
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

func TestCheckConfigValidatesConsumerBeforeProviderOrder(t *testing.T) {
	consumer := sweepCheckConfig(true)
	consumer = strings.Replace(consumer, "    delete:", "    depends_on: [ecs-sg]\n    delete:", 1)
	provider := `  - name: ecs-sg
    resource: ecs/sg
    list: ecctl ecs sg list --filter tag.ecctl-e2e=1
    items_path: $.security_groups
    id_field: id
    runid_field: tags.run-id
    created_field: creation_time
    delete: "ecctl ecs sg delete {{.id}}"
`

	t.Run("valid", func(t *testing.T) {
		_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(true), consumer+provider)
		rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
		if err != nil {
			t.Fatal(err)
		}
		if hasSweepCheckCode(rep, "invalid_dependency_order") {
			t.Fatalf("consumer before provider should be valid: %+v", rep.Errors)
		}
	})

	t.Run("drift", func(t *testing.T) {
		configY := "kinds:\n" + provider + strings.TrimPrefix(consumer, "kinds:\n")
		_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(true), configY)
		rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
		if err != nil {
			t.Fatal(err)
		}
		if !hasSweepCheckCode(rep, "invalid_dependency_order") {
			t.Fatalf("provider-before-consumer drift should fail: %+v", rep.Errors)
		}
	})
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

func TestCheckConfigAllowsProviderOwnedCreateWithoutTeardown(t *testing.T) {
	_, cases, config := writeSweepCheckFixture(t, sweepCheckCase(false), sweepCheckNonSweepableConfig("provider-no-delete"))

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("provider-owned create without a delete API should not require teardown: %+v", rep.Errors)
	}
}

func TestCheckConfigUsesCanonicalResourceForNestedACKCommands(t *testing.T) {
	caseY := `
resource: ack/instance
steps:
  - name: create
    run: ecctl ack policy-instance create --cluster c-test --policy-name p
    teardown: ecctl ack policy-instance delete i-test --cluster c-test
`
	configY := sweepCheckConfig(false) + `non_sweepable:
  - resource: ack/instance
    reason: provider-limitation
    review_after: 2026-10-06
`
	_, cases, config := writeSweepCheckFixture(t, caseY, configY)

	rep, err := CheckConfig(CheckOptions{CasesDir: cases, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("nested ACK command should resolve to its canonical resource: %+v", rep.Errors)
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
