//go:build !windows

package journalfile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDurableRootedWriteCannotFollowReplacedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "journal-parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := WriteDurableRoot(root, "intent.json", []byte("pending\n")); err != nil {
		t.Fatal(err)
	}
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "intent.json"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, "intent.json", []byte("accepted\n")); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(moved, "intent.json"))
	if err != nil || !bytes.Equal(original, []byte("accepted\n")) {
		t.Fatalf("original parent lost rooted write: %v %q", err, original)
	}
	replacement, err := os.ReadFile(filepath.Join(parent, "intent.json"))
	if err != nil || !bytes.Equal(replacement, []byte("replacement\n")) {
		t.Fatalf("replacement parent received rooted write: %v %q", err, replacement)
	}
}

func TestFailedDeleteMarkerBarrierRestoresUnusedMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	original, marked := []byte("unattempted\n"), []byte("attempted\n")
	if err := WriteDurable(path, original); err != nil {
		t.Fatal(err)
	}
	previous := syncJournalDirectory
	t.Cleanup(func() { syncJournalDirectory = previous })
	failOnce := true
	syncJournalDirectory = func(dir string) error {
		if failOnce && dir == filepath.Dir(path) {
			failOnce = false
			return errors.New("synthetic post-rename directory sync failure")
		}
		return syncDirectory(dir)
	}
	if err := WriteDurable(path, marked); err == nil {
		t.Fatal("marker write unexpectedly passed the injected barrier")
	}
	if err := RestoreUninvokedMarker(path, marked, original); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(installed, original) {
		t.Fatalf("unused marker remained installed: %q %v", installed, err)
	}
}

func TestInstalledRecoveryFileNeedsRetryBarrier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "volume-intent.json")
	if err := WriteDurable(path, []byte("pending\n")); err != nil {
		t.Fatal(err)
	}
	previous := syncJournalDirectory
	t.Cleanup(func() { syncJournalDirectory = previous })
	failOnce := true
	syncJournalDirectory = func(dir string) error {
		if failOnce && dir == filepath.Dir(path) {
			failOnce = false
			return errors.New("synthetic post-rename directory sync failure")
		}
		return syncDirectory(dir)
	}
	accepted := []byte("journaled\n")
	if err := WriteDurable(path, accepted); err == nil {
		t.Fatal("recovery write unexpectedly passed the injected barrier")
	}
	if installed, err := os.ReadFile(path); err != nil || !bytes.Equal(installed, accepted) {
		t.Fatalf("post-rename recovery file was not visible: %q %v", installed, err)
	}
	if err := ReaffirmDurable(path, accepted); err != nil {
		t.Fatalf("visible recovery file did not receive a retry barrier: %v", err)
	}
}

func TestIntentFreshDirectoryAncestorsAreDurableBeforeReturn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new", "nested", "reports", "journal.json.team-create-owner.json")
	var synced []string
	previous := syncJournalDirectory
	syncJournalDirectory = func(path string) error {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		synced = append(synced, real)
		return syncDirectory(path)
	}
	t.Cleanup(func() { syncJournalDirectory = previous })
	if err := WithLock(context.Background(), path, func() error { return WriteExclusiveDurable(path, []byte("owned intent\n")) }); err != nil {
		t.Fatal(err)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for current := realDir; ; current = filepath.Dir(current) {
		found := false
		for _, actual := range synced {
			if actual == current {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unsynced intent directory/ancestor %s; actual barriers: %v", current, synced)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
}

func TestIntentAncestorSyncFailureClosesReservationAndRetry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new", "nested", "reports", "intent.json")
	wantParent := filepath.Join(root, "new")
	barrierFailure := errors.New("ancestor durability barrier failed")
	previous := syncJournalDirectory
	t.Cleanup(func() { syncJournalDirectory = previous })
	for attempt := 0; attempt < 2; attempt++ {
		syncJournalDirectory = func(path string) error {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			parent, err := filepath.EvalSymlinks(wantParent)
			if err != nil {
				return err
			}
			if real == parent {
				return barrierFailure
			}
			return syncDirectory(path)
		}
		err := WithLock(context.Background(), path, func() error { return WriteExclusiveDurable(path, []byte("owned intent\n")) })
		if !errors.Is(err, barrierFailure) {
			t.Fatalf("reservation succeeded without ancestor barrier (attempt %d): %v", attempt, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("intent became usable after failed ancestor barrier: %v", err)
		}
	}
	syncJournalDirectory = previous
	if err := WithLock(context.Background(), path, func() error { return WriteExclusiveDurable(path, []byte("owned intent\n")) }); err != nil {
		t.Fatalf("reservation cannot recover after barrier restored: %v", err)
	}
}
