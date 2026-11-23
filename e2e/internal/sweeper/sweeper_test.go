package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func canonicalJournalTestDir(t *testing.T) string {
	t.Helper()
	return canonicalJournalDir(t, t.TempDir())
}

func canonicalJournalDir(t *testing.T, path string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVolumeReplayAttemptChecksCurrentTransferBinding(t *testing.T) {
	const name = "ecctl-p-0123456789abcdef01234567"
	for _, tc := range []struct {
		name        string
		linkedOwner string
		retain      bool
	}{
		{name: "bound Volume", linkedOwner: name, retain: true},
		{name: "independent Volume", linkedOwner: "ecctl-p-89abcdef0123456701234567"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			entry := report.Resource{
				Scope: "case", Teardown: "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner",
				RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution",
			}
			team := report.Resource{
				Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json",
				RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true,
			}
			expected := report.CleanupJournal{
				Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary",
				Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(dir, "ecctl"), Entries: []report.Resource{team, entry},
			}
			binding := journalfile.TemplateCreateIntent{
				Resource: "volume", Name: name, TeamID: "team-owned", RunID: expected.RunID, ExecutionID: expected.ExecutionID,
			}
			latest := expected
			latest.VolumeTeamBindings = map[string]string{"team-destination": tc.linkedOwner}
			if !tc.retain {
				latest.VolumeScopeBindings = map[string][]string{tc.linkedOwner: {"team-destination"}}
				latest.VolumeTransferHistory = map[string]map[string]bool{tc.linkedOwner: {"team-destination": false}}
			}
			body, err := json.Marshal(latest)
			if err != nil {
				t.Fatal(err)
			}
			if err := journalfile.WriteDurable(path, body); err != nil {
				t.Fatal(err)
			}
			parent, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: expected.EcctlBin, Region: expected.Region}, RunID: expected.RunID, Surface: expected.Surface}
			marked, err := markVolumeDeleteReplayAttempt(context.Background(), path, opt, expected, binding, entry, parent)
			if tc.retain {
				if !errors.Is(err, errHistoricalVolumeTransfer) || marked != entry {
					t.Fatalf("new transfer binding did not fence the attempt: entry=%+v err=%v", marked, err)
				}
				if installed, err := os.ReadFile(path); err != nil || string(installed) != string(body) {
					t.Fatalf("retaining the Volume changed the journal: %v", err)
				}
				return
			}
			if err != nil || !marked.VolumeDeleteAttempted {
				t.Fatalf("unrelated transfer blocked the Volume attempt: entry=%+v err=%v", marked, err)
			}
			latest.Entries = []report.Resource{team, marked}
			installed, legacy, err := readReplayJournal(path)
			if err != nil || legacy || !reflect.DeepEqual(installed, latest) {
				t.Fatalf("attempt did not preserve the independently retained history: journal=%+v err=%v", installed, err)
			}
		})
	}
}

func TestReplayBinaryAcceptsMatchingRelativeSelectorForAbsoluteJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relative test binary uses a POSIX shell script")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ecctl-public")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, bin)
	if err != nil {
		t.Fatal(err)
	}
	journal := report.CleanupJournal{EcctlBin: bin}
	opt := ReplayOptions{Config: execpkg.Config{Bin: relative}}
	if err := validateJournalIdentity(journal, &opt); err != nil || opt.Config.Bin != bin {
		t.Fatalf("matching relative replay selector rejected: bin=%q err=%v", opt.Config.Bin, err)
	}
	legacy := report.CleanupJournal{EcctlBin: relative}
	legacyOpt := ReplayOptions{Config: execpkg.Config{Bin: relative}}
	if err := validateJournalIdentity(legacy, &legacyOpt); err != nil || legacyOpt.Config.Bin != relative {
		t.Fatalf("legacy relative journal spelling changed: bin=%q err=%v", legacyOpt.Config.Bin, err)
	}
	other := ReplayOptions{Config: execpkg.Config{Bin: filepath.Join(t.TempDir(), "other")}}
	if err := validateJournalIdentity(journal, &other); err == nil {
		t.Fatal("different replay binary was accepted")
	}
}

func TestVolumeTeamReplayWaitsForPendingNonceRegistration(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", TeamCreateConfirmed: true, VolumeTeamBound: true}
	journal := report.CleanupJournal{
		Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/bin/ecctl",
		VolumeFixtureBinding: &report.QuotaCredentialBinding{}, VolumeTeamCreateIDs: map[string]string{"ecctl-t-0123456789abcdef01234567": ""},
		Entries: []report.Resource{entry},
	}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = markTeamReplayAttempt(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: "/bin/ecctl", Region: "cn-hangzhou"}, RunID: "run", Surface: "public"}, entry, parent)
	if err == nil || !strings.Contains(err.Error(), "pending registration") {
		t.Fatalf("Team replay did not fence incomplete registration: %v", err)
	}
	installed, _, err := readReplayJournal(path)
	if err != nil || installed.Entries[0].TeamReplayAttempted {
		t.Fatalf("Team replay marked deletion despite pending registration: %v %+v", err, installed)
	}
}

func TestReplayJournalFixtureResolvesSymlinkTempAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires POSIX filesystem semantics")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	dir := canonicalJournalDir(t, alias)
	if dir == alias {
		t.Fatal("test fixture retained symlink alias")
	}
	path := filepath.Join(dir, "cleanup.json")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "alias-run", ExecutionID: "alias-execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "must-not-launch"}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region}, RunID: journal.RunID, Surface: journal.Surface})
	if err != nil || result.Deleted != 0 || result.Errors != 0 {
		t.Fatalf("canonical alias fixture was rejected: result=%+v err=%v", result, err)
	}
}

func TestReplayJournalUsesRunLeaseForWholeReplay(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := journalfile.AcquireRunLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ReplayJournal(context.Background(), path, execpkg.Config{Bin: "must-not-launch"}); err == nil {
		t.Fatalf("replay entered an active runner's journal: %+v", result)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	previous := replayJournalAfterRead
	replayJournalAfterRead = func(string) {
		otherRelease, err := journalfile.AcquireRunLease(path)
		if err == nil {
			_ = otherRelease()
			t.Error("replay released the run lease before processing entries")
		}
	}
	defer func() { replayJournalAfterRead = previous }()
	result, err := ReplayJournal(context.Background(), path, execpkg.Config{Bin: "must-not-launch"})
	if err != nil || result.Deleted != 0 || result.Errors != 0 {
		t.Fatalf("idle replay: result=%+v err=%v", result, err)
	}
	missing := filepath.Join(dir, "missing", "cleanup.json")
	if _, err := ReplayJournal(context.Background(), missing, execpkg.Config{}); err == nil {
		t.Fatal("replay created a lost journal parent")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("lost journal parent was recreated: %v", err)
	}
}

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

func TestReplayJournalExcludesConcurrentRunnerAndPreservesLaterRegistration(t *testing.T) {
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
				} else {
					t.Setenv("REPLAY_FAIL", "0")
				}
				dir := canonicalJournalTestDir(t)
				journalPath := filepath.Join(dir, "cleanup-journal.json")
				oldEntry := report.Resource{Scope: "old", Teardown: "ecctl t thing delete old"}
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
				var initial any = report.CleanupJournal{
					Version: 2, JournalPath: journalPath, RunID: "test", ExecutionID: "execution",
					RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
					Entries: []report.Resource{oldEntry},
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
				replayCtx, cancelReplay := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancelReplay()
				go func() {
					_, err := ReplayJournal(replayCtx, journalPath, execpkg.Config{Bin: fake})
					replayDone <- err
				}()
				select {
				case <-replayRead:
				case err := <-replayDone:
					t.Fatalf("replay returned before read hook: %v", err)
				case <-replayCtx.Done():
					t.Fatalf("replay did not reach read hook: %v", replayCtx.Err())
				}

				type writerOutcome struct {
					run *report.Run
					err error
				}
				writerDone := make(chan writerOutcome, 1)
				writerStarted := filepath.Join(dir, "writer-started")
				writerOptions := runnerpkg.Options{
					CleanupJournal: journalPath,
					RunName:        "test",
					RunID:          "test",
					ExecutionID:    "execution",
					Region:         "cn-hangzhou",
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
				}
				go func() {
					run, err := runnerpkg.Run(context.Background(), writerOptions)
					if err == nil && run.Failed() {
						err = fmt.Errorf("runner failed: %+v", run.Cases)
					}
					writerDone <- writerOutcome{run: run, err: err}
				}()
				select {
				case outcome := <-writerDone:
					if outcome.err == nil || !strings.Contains(outcome.err.Error(), "is in use by another run") || outcome.run == nil || len(outcome.run.Cases) != 1 || outcome.run.Cases[0].Resource != "setup" {
						t.Fatalf("concurrent runner was not refused at run lease: run=%+v err=%v", outcome.run, outcome.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("concurrent runner did not promptly reject active replay lease")
				}
				if _, err := os.Stat(writerStarted); !os.IsNotExist(err) {
					t.Fatalf("concurrent runner reached provider before lease refusal: %v", err)
				}
				releaseReplay()
				if err := <-replayDone; err != nil {
					t.Fatal(err)
				}
				if legacy {
					_, err := runnerpkg.Run(context.Background(), writerOptions)
					if err == nil || !strings.Contains(err.Error(), "existing cleanup journal does not match") {
						t.Fatalf("idle runner accepted an unbound legacy journal: %v", err)
					}
					if _, err := os.Stat(writerStarted); !os.IsNotExist(err) {
						t.Fatalf("legacy journal reached provider after replay: %v", err)
					}
				} else {
					run, err := runnerpkg.Run(context.Background(), writerOptions)
					if evidenceErr := concurrentWriterEvidence(run, err, writerStarted); evidenceErr != nil {
						t.Fatal(evidenceErr)
					}
				}

				data, err = os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				var entries []report.Resource
				if legacy {
					if !strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
						t.Fatalf("final legacy journal lost its array format: %s", data)
					}
					if err := json.Unmarshal(data, &entries); err != nil {
						t.Fatalf("final legacy journal is not an array: %v\n%s", err, data)
					}
				} else {
					if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
						t.Fatalf("final versioned journal lost its object format: %s", data)
					}
					var journal report.CleanupJournal
					if err := json.Unmarshal(data, &journal); err != nil {
						t.Fatalf("final journal is not versioned: %v\n%s", err, data)
					}
					entries = journal.Entries
				}
				wantTeardowns := []string{"ecctl t thing delete new"}
				if legacy {
					wantTeardowns = nil
				}
				if replayFails {
					wantTeardowns = []string{"ecctl t thing delete old"}
					if !legacy {
						wantTeardowns = append(wantTeardowns, "ecctl t thing delete new")
					}
				}
				if got := journalTeardowns(entries); !slices.Equal(got, wantTeardowns) {
					t.Fatalf("final journal teardowns = %q, want %q: %s", got, wantTeardowns, data)
				}
			})
		}
	}
}

// A nil runner error is insufficient: skipped cases do not make Run.Failed true.
func concurrentWriterEvidence(run *report.Run, runErr error, marker string) error {
	if runErr != nil {
		return fmt.Errorf("concurrent runner: %w", runErr)
	}
	if run == nil || run.RunID != "test" || run.Region != "cn-hangzhou" || run.Surface != "public" || run.Summary != (report.Summary{Cases: 1, Passed: 1}) || len(run.Cases) != 1 {
		return fmt.Errorf("concurrent runner did not complete one passing case: %+v", run)
	}
	caseResult := run.Cases[0]
	if caseResult.Resource != "t/thing" || caseResult.Status != report.StatusPass || len(caseResult.Steps) != 1 {
		return fmt.Errorf("concurrent writer case did not pass: %+v", caseResult)
	}
	step := caseResult.Steps[0]
	if step.Name != "create" || step.Status != report.StatusPass || step.Exit != 0 || strings.TrimSpace(step.Stdout) != `{"resource":{"id":"new"}}` {
		return fmt.Errorf("concurrent writer provider create did not pass: %+v", step)
	}
	if len(run.Manifest) != 1 || run.Manifest[0].Teardown != "ecctl t thing delete new" || run.Manifest[0].ExecutionID != "execution" || run.Manifest[0].RegionRole != "primary" {
		return fmt.Errorf("concurrent writer did not register one bound teardown: %+v", run.Manifest)
	}
	info, err := os.Stat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("concurrent writer provider marker is missing or invalid: %v", err)
	}
	return nil
}

func TestConcurrentWriterEvidenceRequiresProviderExecution(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "writer-started")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	passing := &report.Run{
		RunID: "test", Region: "cn-hangzhou", Surface: "public", Summary: report.Summary{Cases: 1, Passed: 1},
		Cases:    []report.Case{{Resource: "t/thing", Status: report.StatusPass, Steps: []report.Step{{Name: "create", Status: report.StatusPass, Exit: 0, Stdout: `{"resource":{"id":"new"}}`}}}},
		Manifest: []report.Resource{{Teardown: "ecctl t thing delete new", ExecutionID: "execution", RegionRole: "primary"}},
	}
	if err := concurrentWriterEvidence(passing, nil, marker); err != nil {
		t.Fatalf("fast completed writer was rejected: %v", err)
	}
	skipped := *passing
	skipped.Summary = report.Summary{Cases: 1, Skipped: 1}
	skipped.Cases = []report.Case{{Resource: "t/thing", Status: report.StatusSkipped}}
	failed := *passing
	failed.Summary = report.Summary{Cases: 1, Failed: 1}
	failed.Cases = []report.Case{{Resource: "t/thing", Status: report.StatusFail, Steps: []report.Step{{Name: "create", Status: report.StatusFail, Exit: 1}}}}
	for _, test := range []struct {
		name   string
		run    *report.Run
		err    error
		marker string
	}{
		{name: "no run", marker: marker},
		{name: "skipped", run: &skipped, marker: marker},
		{name: "failed", run: &failed, marker: marker},
		{name: "missing marker", run: passing, marker: marker + "-missing"},
		{name: "runner error", run: passing, err: errors.New("setup failed"), marker: marker},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := concurrentWriterEvidence(test.run, test.err, test.marker); err == nil {
				t.Fatal("missing provider evidence accepted")
			}
		})
	}
}

func journalTeardowns(entries []report.Resource) []string {
	result := make([]string, len(entries))
	for i, entry := range entries {
		result[i] = entry.Teardown
	}
	return result
}

func TestReplayJournalCanceledContextPrecedesBusyRunLease(t *testing.T) {
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
	replayJournalAfterRead = func(string) {
		close(replayRead)
		<-resumeReplay
	}
	defer func() { replayJournalAfterRead = previousHook }()

	firstDone := make(chan error, 1)
	go func() {
		_, err := ReplayJournal(context.Background(), journalPath, execpkg.Config{})
		firstDone <- err
	}()
	select {
	case <-replayRead:
	case err := <-firstDone:
		t.Fatalf("first replay returned before read hook: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first replay did not reach read hook")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReplayJournal(ctx, journalPath, execpkg.Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replay error = %v, want context canceled before lease contention", err)
	}
	busyCtx, cancelBusy := context.WithTimeout(context.Background(), time.Second)
	defer cancelBusy()
	if _, err := ReplayJournal(busyCtx, journalPath, execpkg.Config{}); err == nil || !strings.Contains(err.Error(), "is in use by another run") {
		t.Fatalf("active replay lease did not exclude second replay: %v", err)
	}

	releaseReplay()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestReplayJournalLockWaitHonorsContext(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	if err := os.WriteFile(journalPath, []byte(`{"version":2,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := replayParentIdentity(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseHolder := sync.OnceFunc(func() { close(release) })
	defer releaseHolder()
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- journalfile.WithExistingReplayLock(context.Background(), journalPath, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-holderDone:
		t.Fatalf("replay lock holder returned before admission: %v", err)
	case <-time.After(time.Second):
		t.Fatal("replay lock holder did not enter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	called := false
	err = withAnchoredReplayLock(ctx, journalPath, parent, func() error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("contended replay lock ignored context: err=%v called=%t", err, called)
	}
	releaseHolder()
	if err := <-holderDone; err != nil {
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
				// A short journal transaction may merge a new entry while the
				// replay owns the run lease; it performs no provider allocation.
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var current report.CleanupJournal
				if err := json.Unmarshal(body, &current); err != nil {
					return err
				}
				current.Entries = append(current.Entries, report.Resource{Scope: "late", Teardown: "ecctl t thing delete late"})
				body, err = json.Marshal(current)
				if err != nil {
					return err
				}
				return journalfile.WriteDurable(path, body)
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
	if got := journalTeardowns(journal.Entries); !slices.Equal(got, []string{"ecctl t thing delete late"}) {
		t.Fatalf("replay lost merged entry or retained completed delete after cancellation: %q; journal=%s", got, data)
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
