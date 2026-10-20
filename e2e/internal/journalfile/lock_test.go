package journalfile

import (
	"context"
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
