//go:build windows

package journalfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDurableRootedWriteReplacesVolumeReceiptOnWindows(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, body := range []string{"pending\n", "accepted-id\n"} {
		if err := WriteDurableRoot(root, "volume-intent.json", []byte(body)); err != nil {
			t.Fatalf("durable Volume receipt write: %v", err)
		}
		if !PrivateRecoverySidecar(filepath.Join(root.Name(), "volume-intent.json")) {
			t.Fatal("rooted Volume receipt lacks a protected private ACL")
		}
		installed, err := root.ReadFile("volume-intent.json")
		if err != nil || string(installed) != body {
			t.Fatalf("installed receipt=%q, err=%v; want %q", installed, err, body)
		}
	}
}

func TestDurableWindowsJournalReplacementUsesPrivateACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	if err := WriteDurable(path, []byte("owned journal\n")); err != nil {
		t.Fatal(err)
	}
	if !PrivateRecoverySidecar(path) {
		t.Fatal("journal replacement lacks a protected private ACL")
	}
}
