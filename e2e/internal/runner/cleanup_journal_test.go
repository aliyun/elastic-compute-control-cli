package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestCleanupJournalPreservesDuplicatePendingEntries(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "cleanup-journal.json")
	registry := newCleanup(
		map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-hangzhou"}},
		nil,
		true,
		journalPath,
		report.CleanupJournal{RunID: "run-1", ExecutionID: "execution-1", Surface: "public"},
		func(string, ...any) {},
	)
	var scope []*cleanupItem
	const teardown = "ecctl t thing delete shared"
	if err := registry.push(&scope, "case-a", teardown, "primary", nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.push(&scope, "case-a", teardown, "primary", nil); err != nil {
		t.Fatal(err)
	}
	if got := readJournalEntries(t, journalPath); len(got) != 2 {
		t.Fatalf("pending journal entries = %d, want 2: %+v", len(got), got)
	}

	if err := registry.satisfy(scope, teardown, "primary"); err != nil {
		t.Fatal(err)
	}
	if got := readJournalEntries(t, journalPath); len(got) != 1 {
		t.Fatalf("journal entries after satisfying one duplicate = %d, want 1: %+v", len(got), got)
	}
}

func TestCleanupJournalRejectsMismatchedExistingMetadata(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "cleanup-journal.json")
	initial := report.CleanupJournal{
		Version: 2, RunID: "other-run", ExecutionID: "execution-1",
		Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl",
		Entries: []report.Resource{{Scope: "old", Teardown: "ecctl t thing delete old"}},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	registry := newCleanup(
		map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-hangzhou"}},
		nil,
		true,
		journalPath,
		report.CleanupJournal{RunID: "run-1", ExecutionID: "execution-1", Surface: "public"},
		func(string, ...any) {},
	)
	var scope []*cleanupItem
	err = registry.push(&scope, "new", "ecctl t thing delete new", "primary", nil)
	if err == nil || !strings.Contains(err.Error(), "run id") {
		t.Fatalf("push error = %v, want run id mismatch", err)
	}
	entries := readJournalEntries(t, journalPath)
	if len(entries) != 1 || entries[0].Teardown != "ecctl t thing delete old" {
		t.Fatalf("mismatched writer changed journal: %+v", entries)
	}
}

func readJournalEntries(t *testing.T, path string) []report.Resource {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	return journal.Entries
}
