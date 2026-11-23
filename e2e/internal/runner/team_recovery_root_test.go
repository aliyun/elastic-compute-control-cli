package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTeamCompletionRootRejectsReplacedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming an open directory is not portable to Windows")
	}
	base := t.TempDir()
	parent := filepath.Join(base, "journal")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := filepath.Join(parent, "cleanup.json")
	if err := teamCompletionRootMatchesPath(root, path); err != nil {
		t.Fatalf("original parent was rejected: %v", err)
	}
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := teamCompletionRootMatchesPath(root, path); err == nil {
		t.Fatal("replacement parent inherited protected Team journal custody")
	}
}
