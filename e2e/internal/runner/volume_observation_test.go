package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

// volumeObservationSidecar returns the single retained Volume creation sidecar.
func volumeObservationSidecar(t *testing.T, cl *cleanup) string {
	t.Helper()
	paths, err := filepath.Glob(cl.journal + ".template-create-*.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("expected one retained Volume sidecar, got %v (%v)", paths, err)
	}
	return paths[0]
}

// startUnobservedNoWaitVolume runs an accepted --no-wait create whose first
// readiness read fails, leaving a journaled accepted ID with VolumeObserved=false.
func startUnobservedNoWaitVolume(t *testing.T, cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem) {
	t.Helper()
	readinessFailed := false
	cl.runCommand = func(callCtx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if !readinessFailed && strings.Contains(command, "'volume' 'get'") {
			readinessFailed = true
			return execpkg.Result{Command: command, Started: false, Exit: -1, Err: errors.New("injected first readiness read failure")}
		}
		return execpkg.Run(callCtx, bound, command)
	}
	create := scenario.Step{Name: "create", Run: "ecctl sandbox volume create --name authored --team-id team-1 --no-wait --output=json", Teardown: "ecctl sandbox volume delete {{.volume_id}} --output=json"}
	result, ok := runStep(context.Background(), Options{volumeLifecycle: true, Logf: func(string, ...any) {}}, cfg, cl, scope, map[string]any{}, create, time.Second)
	if ok || !readinessFailed || !strings.Contains(result.Error, "readiness") {
		t.Fatalf("first readiness read did not fail while retaining the accepted create: %+v started=%v", result, readinessFailed)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(volumeObservationSidecar(t, cl))
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.AcceptedVolumeID != "vol-owned" || intent.State != "journaled" || intent.VolumeObserved {
		t.Fatalf("accepted no-wait Volume was not retained unobserved: %+v %v", intent, err)
	}
}

func TestNoWaitVolumeCleanupPersistsObservationBeforeDelete(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	startUnobservedNoWaitVolume(t, cfg, cl, scope)
	intentPath := volumeObservationSidecar(t, cl)
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(intentPath)
	if err != nil || json.Unmarshal(body, &intent) != nil {
		t.Fatalf("read retained sidecar: %v", err)
	}
	observedAtDelete, receiptAtDelete := false, false
	cl.runCommand = func(callCtx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "'volume' 'delete'") {
			var atDelete journalfile.TemplateCreateIntent
			if raw, readErr := os.ReadFile(intentPath); readErr == nil && json.Unmarshal(raw, &atDelete) == nil {
				observedAtDelete = atDelete.VolumeObserved
			}
			if journal, _, readErr := readRunnerJournal(cl.journal); readErr == nil {
				_, receiptAtDelete = journal.VolumeObservationReceipts[intent.Name]
			}
		}
		return execpkg.Run(callCtx, bound, command)
	}
	cl.keep = false
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
		t.Fatalf("automatic cleanup did not complete: %v", failures)
	}
	if !observedAtDelete || !receiptAtDelete {
		t.Fatalf("DeleteVolume was sent before the observation was durable: observed=%v receipt=%v", observedAtDelete, receiptAtDelete)
	}
	observedBody, err := os.ReadFile(intentPath)
	var observed journalfile.TemplateCreateIntent
	if err != nil || json.Unmarshal(observedBody, &observed) != nil || !observed.VolumeObserved {
		t.Fatalf("cleanup did not retain the positive observation: %+v %v", observed, err)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy {
		t.Fatalf("read cleanup journal: %v", err)
	}
	sum := sha256.Sum256(observedBody)
	if receipt := journal.VolumeObservationReceipts[intent.Name]; receipt != hex.EncodeToString(sum[:]) {
		t.Fatalf("observation receipt does not bind the durable sidecar: %q", receipt)
	}
	if _, retired := journal.VolumeTerminalReceipts[intent.Name]; !retired {
		t.Fatalf("observed Volume finalizer was not consumable to terminal absence: %+v", journal.VolumeTerminalReceipts)
	}
	for _, entry := range journal.Entries {
		if strings.Contains(entry.Teardown, "vol-owned") {
			t.Fatalf("consumed Volume finalizer remains journaled: %+v", entry)
		}
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 || !strings.Contains(string(calls), "team-delete") {
		t.Fatalf("cleanup did not perform exactly one observed delete and Team teardown: %s (%v)", calls, err)
	}
	if firstRead, deleteCall := strings.Index(string(calls), "volume-get"), strings.Index(string(calls), "volume-delete:vol-owned"); firstRead < 0 || deleteCall <= firstRead {
		t.Fatalf("observed delete lacked its positive recovery read: %s", calls)
	}
	if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Errors != 0 || replay.Deleted != 0 {
		t.Fatalf("post-cleanup journal failed lineage replay: %+v %v", replay, err)
	}
}

func TestNoWaitVolumeCleanupRefusesDeleteWhenObservationCannotPersist(t *testing.T) {
	cfg, cl, dir, scope := volumeRecoveryFixture(t)
	startUnobservedNoWaitVolume(t, cfg, cl, scope)
	intentPath := volumeObservationSidecar(t, cl)
	originalWrite := writeVolumeReconcileRootBytes
	t.Cleanup(func() { writeVolumeReconcileRootBytes = originalWrite })
	writeVolumeReconcileRootBytes = func(root *os.Root, path string, body []byte) error {
		return errors.New("injected observation journal persistence failure")
	}
	cl.keep = false
	if failures := cl.runCaseCleanup(*scope); len(failures) == 0 {
		t.Fatal("cleanup deleted an unobserved Volume despite observation persistence failure")
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "volume-delete:") || strings.Contains(string(calls), "team-delete") {
		t.Fatalf("refused observation still sent a mutation: %s", calls)
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || entries[1].VolumeDeleteAttempted {
		t.Fatalf("refused observation lost the replayable finalizer: %+v", entries)
	}
	var intent journalfile.TemplateCreateIntent
	body, err := os.ReadFile(intentPath)
	if err != nil || json.Unmarshal(body, &intent) != nil || intent.VolumeObserved {
		t.Fatalf("refused observation silently promoted the Volume: %+v %v", intent, err)
	}
	if replay, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || replay.Deleted != 0 {
		t.Fatalf("replay deleted an unobserved Volume after refused observation: %+v %v", replay, err)
	}
	writeVolumeReconcileRootBytes = originalWrite
	if failures := cl.runCaseCleanup(*scope); len(failures) != 0 {
		t.Fatalf("retry after restored persistence did not complete: %v", failures)
	}
	journal, legacy, err := readRunnerJournal(cl.journal)
	if err != nil || legacy {
		t.Fatalf("read cleanup journal after retry: %v", err)
	}
	if _, retired := journal.VolumeTerminalReceipts[intent.Name]; !retired {
		t.Fatalf("retry did not consume the observed Volume finalizer: %+v", journal.VolumeTerminalReceipts)
	}
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "volume-delete:vol-owned\n") != 1 {
		t.Fatalf("retry did not perform exactly one observed delete: %s", calls)
	}
}
