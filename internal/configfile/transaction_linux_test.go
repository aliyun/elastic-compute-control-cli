//go:build linux

package configfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAtomicWritePreservesLinuxSecurityMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	const attribute = "user.ecctl_metadata_test"
	if err := unix.Setxattr(path, attribute, []byte("preserve"), 0); err != nil {
		t.Skipf("filesystem does not support user xattrs: %v", err)
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

func TestLinuxReplacementStaysPrivateUntilContentIsSynced(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(targetPath, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	temp, metadata, err := createAtomicTemp(dir, ".ecctl-config-*.tmp", targetPath, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()
	if err := prepareReplacementBeforeWrite(temp, metadata); err != nil {
		t.Fatal(err)
	}
	info, err := temp.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pre-write temp mode=%v", info.Mode().Perm())
	}
	if _, err := temp.Write([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := temp.Sync(); err != nil {
		t.Fatal(err)
	}
	info, err = temp.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("post-sync temp mode=%v", info.Mode().Perm())
	}
	if err := finishReplacementAfterWrite(temp, metadata); err != nil {
		t.Fatal(err)
	}
	info, err = temp.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("final temp mode=%v", info.Mode().Perm())
	}
}

func TestLinuxRestrictedSecurityXattrsAreComparisonOnly(t *testing.T) {
	for _, name := range []string{"security.selinux", "security.capability", "trusted.example", "system.example"} {
		if linuxReplayableXattr(name) {
			t.Fatalf("restricted xattr %q would be replayed", name)
		}
	}
	for _, name := range []string{"user.example", "system.posix_acl_access"} {
		if !linuxReplayableXattr(name) {
			t.Fatalf("safe xattr %q would not be replayed", name)
		}
	}
}

func TestEqualLinuxXattrsRejectsDifferentEmptyKeys(t *testing.T) {
	if equalLinuxXattrs(map[string][]byte{"user.one": {}}, map[string][]byte{"user.two": {}}) {
		t.Fatal("different zero-length Linux xattrs compared equal")
	}
}
