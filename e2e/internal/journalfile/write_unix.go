//go:build !windows

package journalfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Injectable only at the directory barrier so tests can reproduce sync errors.
var syncJournalDirectory = syncDirectory

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ensureRecoveryDirectory(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	// Sync through the real existing ancestors as well as new directories.
	// A prior failed barrier may have left an existing but unsynced chain.
	for current := real; ; current = filepath.Dir(current) {
		if err := syncJournalDirectory(current); err != nil {
			return fmt.Errorf("sync recovery intent directory %s: %w", current, err)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func installDurable(tmp, path string, replace bool) error {
	if replace {
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	} else {
		// Link is exclusive and installs the already-synced inode atomically.
		if err := os.Link(tmp, path); err != nil {
			return err
		}
		if err := os.Remove(tmp); err != nil {
			return err
		}
	}
	return syncJournalDirectory(filepath.Dir(path))
}
