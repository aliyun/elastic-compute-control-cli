//go:build !windows

package journalfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestRootedTemplateLedgerReadKeepsOriginalParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "journal-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "cleanup.json")
	original := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "original", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl"}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(path, body); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.RunID = "replacement"
	replacementBody, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(path, replacementBody); err != nil {
		t.Fatal(err)
	}
	if err := CheckTemplateCreateIntents(path, original, nil, root); err != nil {
		t.Fatalf("rooted ledger read switched parents: %v", err)
	}
	if err := CheckTemplateCreateIntents(path, original, nil); err == nil {
		t.Fatal("path read unexpectedly used the original parent")
	}
	if installed, err := os.ReadFile(path); err != nil || string(installed) != string(replacementBody) {
		t.Fatalf("rooted read changed replacement parent: %q, %v", installed, err)
	}
}
