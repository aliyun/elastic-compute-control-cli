package configfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicWritePreservesSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.json")
	requestedPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(targetPath, []byte("before"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(targetPath), requestedPath); err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(requestedPath, false)
	if err != nil {
		t.Fatal(err)
	}
	err = target.WithLock(context.Background(), time.Second, time.Millisecond, func() error {
		_, info, err := target.Read()
		if err != nil {
			return err
		}
		return target.AtomicWrite([]byte("after"), info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(requestedPath)
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink mode=%v err=%v", linkInfo.Mode(), err)
	}
	info, err := os.Stat(targetPath)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("target mode=%v err=%v", info.Mode().Perm(), err)
	}
	raw, _ := os.ReadFile(targetPath)
	if string(raw) != "after" {
		t.Fatalf("target contents = %q", raw)
	}
}

func TestWithLockRejectsReplacedSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	link := filepath.Join(dir, "config.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Base(first), link); err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(link, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(second), link); err != nil {
		t.Fatal(err)
	}
	called := false
	err = target.WithLock(context.Background(), time.Second, time.Millisecond, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrTargetReplaced) || called {
		t.Fatalf("replaced target error=%v called=%t", err, called)
	}
}

func TestResolveRejectsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink("missing.json", link); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(link, true); err == nil {
		t.Fatal("dangling configuration symlink was accepted")
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling symlink changed: mode=%v err=%v", info.Mode(), err)
	}
}

func TestBeginPrivateReplaceReservesAndCommitsPrivateFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	path := filepath.Join(dir, "entry.json")
	target, err := Resolve(path, true)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := target.BeginPrivateReplace(4096)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := replacement.file.Stat(); err != nil || info.Size() != 4096 || info.Mode().Perm() != 0o600 {
		t.Fatalf("reserved file info=%#v err=%v", info, err)
	}
	if err := replacement.Commit([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateFile(path); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "secret" {
		t.Fatalf("committed contents=%q err=%v", raw, err)
	}
}

func TestBeginPrivateReplaceAbortPreservesExistingTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "entry.json")
	target, err := Resolve(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.AtomicWritePrivate([]byte("before")); err != nil {
		t.Fatal(err)
	}
	replacement, err := target.BeginPrivateReplace(4096)
	if err != nil {
		t.Fatal(err)
	}
	stagingDir := filepath.Dir(replacement.path)
	if err := replacement.Abort(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "before" {
		t.Fatalf("target contents=%q err=%v", raw, err)
	}
	if _, err := os.Stat(stagingDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging directory remained: %v", err)
	}
}
