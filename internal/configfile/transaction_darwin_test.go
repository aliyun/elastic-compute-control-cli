//go:build darwin

package configfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func addDarwinACL(t *testing.T, path, rule string) {
	t.Helper()
	if output, err := exec.Command("/bin/chmod", "+a", rule, path).CombinedOutput(); err != nil {
		t.Skipf("filesystem does not support Darwin ACLs: %v: %s", err, output)
	}
}

func TestCreateSensitiveTempRemovesInheritedDarwinACLBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	addDarwinACL(t, dir, "everyone allow read,write,execute,file_inherit,directory_inherit")
	file, err := CreateSensitiveTemp(dir, "credential-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer CleanupSensitiveTemp(path)
	defer file.Close()
	if err := validatePrivateOpenFile(file); err != nil {
		t.Fatalf("sensitive temp inherited an ACL: %v", err)
	}
	if err := ValidatePrivateFile(path); err != nil {
		t.Fatalf("sensitive temp path is not private: %v", err)
	}
}

func TestAtomicWriteDarwinDoesNotImportParentACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	addDarwinACL(t, dir, "everyone allow read,write,execute,file_inherit,directory_inherit")
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if acl, err := readDarwinACL(path); err != nil || acl != "" {
		t.Fatalf("replacement ACL=%q err=%v", acl, err)
	}
}

func TestAtomicWriteDarwinPreservesSourceACLInsteadOfParentACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	addDarwinACL(t, path, "everyone allow read")
	wantACL, err := readDarwinACL(path)
	if err != nil {
		t.Fatal(err)
	}
	addDarwinACL(t, dir, "everyone allow read,write,execute,file_inherit,directory_inherit")
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readDarwinACL(path); err != nil || got != wantACL {
		t.Fatalf("replacement ACL=%q, want %q, err=%v", got, wantACL, err)
	}
}

func TestAtomicWriteDarwinFallsBackWhenCloneIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	addDarwinACL(t, path, "everyone allow read")
	wantACL, err := readDarwinACL(path)
	if err != nil {
		t.Fatal(err)
	}
	originalClone := cloneDarwinFile
	cloneDarwinFile = func(string, string, int) error { return unix.ENOTSUP }
	defer func() { cloneDarwinFile = originalClone }()
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("after"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, err := readDarwinACL(path); err != nil || got != wantACL {
		t.Fatalf("fallback ACL=%q, want %q, err=%v", got, wantACL, err)
	}
}

func TestAtomicWriteNewDarwinTargetIsPrivateUnderInheritedACL(t *testing.T) {
	dir := t.TempDir()
	addDarwinACL(t, dir, "everyone allow read,write,execute,file_inherit,directory_inherit")
	path := filepath.Join(dir, "new-config.json")
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if acl, err := readDarwinACL(path); err != nil || acl != "" {
		t.Fatalf("new target ACL=%q err=%v", acl, err)
	}
}

func TestEqualDarwinXattrsRejectsDifferentEmptyKeys(t *testing.T) {
	if equalDarwinXattrs(map[string][]byte{"user.one": {}}, map[string][]byte{"user.two": {}}) {
		t.Fatal("different zero-length Darwin xattrs compared equal")
	}
}

func TestAtomicWritePreservesDarwinSecurityMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	const attribute = "com.openai.ecctl.metadata-test"
	if err := unix.Setxattr(path, attribute, []byte("preserve"), 0); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWrite([]byte("after"), before.Mode()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat := before.Sys().(*syscall.Stat_t)
	afterStat := after.Sys().(*syscall.Stat_t)
	if before.Mode() != after.Mode() || beforeStat.Uid != afterStat.Uid || beforeStat.Gid != afterStat.Gid {
		t.Fatalf("metadata changed: before=%v/%d/%d after=%v/%d/%d", before.Mode(), beforeStat.Uid, beforeStat.Gid, after.Mode(), afterStat.Uid, afterStat.Gid)
	}
	value := make([]byte, 64)
	size, err := unix.Getxattr(path, attribute, value)
	if err != nil || string(value[:size]) != "preserve" {
		t.Fatalf("xattr=%q err=%v", value[:size], err)
	}
}

func TestAtomicWritePrivateRejectsExistingDarwinACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "entry.json")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Skipf("filesystem does not support Darwin ACLs: %v: %s", err, output)
	}
	if err := ValidatePrivateFile(path); err == nil {
		t.Fatal("private file with a Darwin ACL was accepted")
	}
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWritePrivate([]byte("secret")); err == nil {
		t.Fatal("private replacement preserved an unsafe Darwin ACL")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "before" {
		t.Fatalf("unsafe target contents changed: %q, %v", raw, err)
	}
}

func TestAtomicWritePrivateCreatesDarwinFileWithoutACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	path := filepath.Join(dir, "entry.json")
	target, err := Resolve(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWritePrivate([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateFile(path); err != nil {
		t.Fatal(err)
	}
}
