//go:build !windows

package journalfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
