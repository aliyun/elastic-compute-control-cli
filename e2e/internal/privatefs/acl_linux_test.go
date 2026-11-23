//go:build linux

package privatefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxACLEvidenceFollowsOpenedInode(t *testing.T) {
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
		t.Fatal("ordinary private file lacks ACL absence evidence")
	}
	if NoExtendedACLFile(nil) || NoExtendedACL(filepath.Join(filepath.Dir(path), "missing")) {
		t.Fatal("unavailable ACL evidence was accepted")
	}
	const attr = "user.fixture_acl"
	if err := unix.Fsetxattr(int(file.Fd()), attr, []byte("inert"), 0); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
			t.Skipf("test filesystem cannot set ACL-shaped xattr: %v", err)
		}
		t.Fatal(err)
	}
	if NoExtendedACLFile(file) || NoExtendedACL(path) {
		t.Fatal("ACL-shaped xattr was accepted")
	}
	if err := ClearInheritedACL(file); err == nil {
		t.Fatal("unknown ACL-shaped xattr was silently cleared")
	}
	if err := unix.Fremovexattr(int(file.Fd()), attr); err != nil {
		t.Fatal(err)
	}
	if !NoExtendedACLFile(file) || !NoExtendedACL(path) {
		t.Fatal("private custody did not recover after xattr removal")
	}
	if err := file.Close(); err != nil || NoExtendedACLFile(file) {
		t.Fatalf("closed descriptor was accepted as ACL evidence: %v", err)
	}
}

func TestLinuxInheritedPOSIXACLIsRemoved(t *testing.T) {
	path := t.TempDir()
	dir, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	// Linux POSIX ACL xattr v2 with owner, group, and other entries.
	acl := []byte{
		2, 0, 0, 0,
		1, 0, 7, 0, 255, 255, 255, 255,
		4, 0, 0, 0, 255, 255, 255, 255,
		32, 0, 0, 0, 255, 255, 255, 255,
	}
	if err := unix.Fsetxattr(int(dir.Fd()), "system.posix_acl_default", acl, 0); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
			t.Skipf("test filesystem cannot set a default POSIX ACL: %v", err)
		}
		t.Fatal(err)
	}
	if NoExtendedACLFile(dir) || NoExtendedACL(path) {
		t.Fatal("default POSIX ACL was accepted")
	}
	if err := ClearInheritedACL(dir); err != nil {
		t.Fatal(err)
	}
	if !NoExtendedACLFile(dir) || !NoExtendedACL(path) {
		t.Fatal("default POSIX ACL remained after clearing")
	}
}
