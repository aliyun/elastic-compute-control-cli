//go:build !windows

package journalfile

import (
	"os"
	"path/filepath"
)

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
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
