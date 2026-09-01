package configfile

import (
	"errors"
	"os"
	"path/filepath"
)

const privateRetiredSuffix = ".ecctl-retired"

var ErrPrivateRetirementAlias = errors.New("private file and retirement tombstone refer to the same file")

// RemovePrivateFile durably retires a validated current-user-only file from its
// canonical path before best-effort tombstone cleanup. Windows uses a
// write-through namespace move; Unix syncs the containing directory.
func RemovePrivateFile(path string) error {
	return removePrivateFileWithSync(path, syncDirectory)
}

func removePrivateFileWithSync(path string, syncFn func(string) error) error {
	if path == "" {
		return errors.New("private file path is unavailable")
	}
	if syncFn == nil {
		syncFn = syncDirectory
	}
	dir := filepath.Dir(path)
	tombstone := retiredPrivatePath(path)
	_, pathErr := os.Lstat(path)
	if errors.Is(pathErr, os.ErrNotExist) {
		tombstoneExists := false
		if _, tombstoneErr := os.Lstat(tombstone); tombstoneErr == nil {
			tombstoneExists = true
			if err := ValidatePrivateFile(tombstone); err != nil {
				return err
			}
		} else if !errors.Is(tombstoneErr, os.ErrNotExist) {
			return tombstoneErr
		}
		// A prior retirement may have become visible before its directory
		// sync failed. Sync before deleting the only durable evidence.
		if err := syncFn(dir); err != nil {
			if tombstoneExists {
				return &PostCommitError{Err: err}
			}
			return err
		}
		cleanupPrivateTombstone(tombstone, syncFn)
		return nil
	}
	if pathErr != nil {
		return pathErr
	}
	if err := ValidatePrivateFile(path); err != nil {
		return err
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(tombstone); err == nil {
		if err := ValidatePrivateFile(tombstone); err != nil {
			return err
		}
		tombstoneInfo, err := os.Stat(tombstone)
		if err != nil {
			return err
		}
		if os.SameFile(pathInfo, tombstoneInfo) {
			return ErrPrivateRetirementAlias
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := retireFile(path, tombstone); err != nil {
		return err
	}
	if err := syncFn(dir); err != nil {
		return &PostCommitError{Err: err}
	}
	cleanupPrivateTombstone(tombstone, syncFn)
	return nil
}

func retiredPrivatePath(path string) string {
	return path + privateRetiredSuffix
}

func cleanupPrivateTombstone(path string, syncFn func(string) error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = syncFn(filepath.Dir(path))
}
