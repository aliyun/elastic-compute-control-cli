//go:build !windows

package configfile

import (
	"os"
	"path/filepath"
)

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncReplacementPlatform(path string) error {
	return syncDirectory(filepath.Dir(path))
}
