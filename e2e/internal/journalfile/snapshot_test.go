package journalfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivateRecoverySnapshotBindsBytesAndParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "journal")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "cleanup.json")
	if err := WriteDurable(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadPrivateRecoverySnapshot(path)
	if err != nil || string(snapshot.Body) != "first" {
		t.Fatalf("private snapshot = %q, %v", snapshot.Body, err)
	}
	if err := snapshot.VerifyCurrent(path); err != nil {
		t.Fatalf("unchanged snapshot rejected: %v", err)
	}
	if err := WriteDurable(path, []byte("later")); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.VerifyCurrent(path); err == nil {
		t.Fatal("changed bytes kept the original snapshot authority")
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPrivateRecoverySnapshot(path); err == nil {
			t.Fatal("inherited Windows DACL became private recovery evidence")
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPrivateRecoverySnapshot(path); err == nil {
			t.Fatal("nonprivate journal became reconciliation evidence")
		}
		if err := os.Rename(parent, parent+"-old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := snapshot.VerifyCurrent(path); err == nil {
			t.Fatal("replacement parent kept the original snapshot authority")
		}
	}
}

func TestExclusiveRootReceiptStaysInOpenedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows directory replacement is compile-only here")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(base, "journal")
	if err := os.Mkdir(parent, 0o700); err != nil {
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
	if err := WriteExclusiveDurableRoot(root, "receipt.json", []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "receipt.json")); !os.IsNotExist(err) {
		t.Fatalf("receipt escaped into replacement parent: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(parent+"-moved", "receipt.json")); err != nil || string(body) != `{"version":1}` {
		t.Fatalf("opened parent lost receipt: %q %v", body, err)
	}
}
