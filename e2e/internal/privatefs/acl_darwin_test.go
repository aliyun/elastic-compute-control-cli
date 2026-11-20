//go:build darwin && cgo

package privatefs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeACLEvidenceFollowsOpenedInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !NoExtendedACLFile(file) || !NoExtendedACL(path) {
		t.Fatal("ordinary private file lacks native absence evidence")
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Fatalf("set ACL fixture: %v: %s", err, output)
	}
	if NoExtendedACLFile(file) || NoExtendedACL(path) {
		t.Fatal("native ACL evidence ignored a grant on the opened inode")
	}
	if err := ClearInheritedACL(file); err != nil {
		t.Fatal(err)
	}
	if !NoExtendedACLFile(file) || !NoExtendedACL(path) {
		t.Fatal("native ACL clear did not restore private custody")
	}
	if NoExtendedACLFile(nil) || NoExtendedACL(filepath.Join(filepath.Dir(path), "missing")) {
		t.Fatal("unavailable ACL evidence was accepted")
	}
	if err := file.Close(); err != nil || NoExtendedACLFile(file) {
		t.Fatalf("closed descriptor was accepted as ACL evidence: %v", err)
	}
}
