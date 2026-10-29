package journalfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupSharedLockExcludesReservationAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- WithCleanupLock(context.Background(), path, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := WithCleanupLock(ctx, path, func() error { return nil }); err != nil {
		cancel()
		close(release)
		<-done
		t.Fatalf("independent shared cleanup blocked: %v", err)
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
	exclusiveRan := false
	if err := WithReplayLock(ctx, path, func() error { exclusiveRan = true; return nil }); err == nil || exclusiveRan {
		cancel()
		close(release)
		<-done
		t.Fatalf("reservation/replay overlapped active cleanup: %v", err)
	}
	cancel()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := WithReplayLock(context.Background(), path, func() error { return nil }); err != nil {
		t.Fatalf("exclusive coordination could not resume: %v", err)
	}
}

func TestExistingParentLockRejectsLostAndReplacedJournalDirectory(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "journals")
	path := filepath.Join(parent, "journal.json")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := WithExistingParentLock(context.Background(), path, func() error { called = true; return nil }); err == nil || called {
		t.Fatalf("lost journal parent was accepted: err=%v called=%v", err, called)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("read-only lock recreated the lost parent: %v", err)
	}
	if err := os.Rename(moved, parent); err != nil {
		t.Fatal(err)
	}
	if err := WithExistingParentLock(context.Background(), path, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("restored journal parent was rejected: err=%v called=%v", err, called)
	}
	called = false
	if err := WithExistingParentLock(context.Background(), path, func() error {
		called = true
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		return os.Mkdir(parent, 0o700)
	}); err == nil || !called {
		t.Fatalf("replaced journal parent was accepted: err=%v called=%v", err, called)
	}
}

func TestExistingReplayLockNeverRecreatesOrFollowsChangedParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "journals")
	path := filepath.Join(parent, "cleanup.json")
	called := false
	if err := WithExistingReplayLock(context.Background(), path, func() error { called = true; return nil }); err == nil || called {
		t.Fatalf("missing parent entered replay reservation: called=%v err=%v", called, err)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("replay reservation recreated missing parent: %v", err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	moved := parent + "-moved"
	called = false
	if err := WithExistingReplayLock(context.Background(), path, func() error {
		called = true
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		return os.Mkdir(parent, 0o700)
	}); err == nil || !called {
		t.Fatalf("replay reservation accepted parent replacement: called=%v err=%v", called, err)
	}
}
