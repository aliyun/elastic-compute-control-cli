package journalfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootInventoryStaysWithOpenedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "journal")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "old.edge"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(parent, parent+".moved"); err != nil {
		t.Skipf("parent move is unavailable: %v", err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "new.edge"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := readRootInventory(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "old.edge" {
		t.Fatalf("opened parent inventory = %v, %v", entries, err)
	}
}
