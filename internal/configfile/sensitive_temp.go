package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const sensitiveTempDirectoryPrefix = ".ecctl-sensitive-"

// CreateSensitiveTemp creates a temporary file inside a freshly-created,
// current-user-only staging directory. The directory and file are both
// normalized and verified before the caller can write credential material.
func CreateSensitiveTemp(dir, pattern string) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	stage, err := os.MkdirTemp(dir, sensitiveTempDirectoryPrefix+"*")
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = os.Remove(stage)
	}
	if err := prepareCreatedPrivateDirectory(stage); err != nil {
		cleanup()
		return nil, err
	}
	file, err := createSensitiveFile(stage, pattern)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := preparePrivateTemp(file); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		cleanup()
		return nil, err
	}
	return file, nil
}

// CleanupSensitiveTemp removes a file created by CreateSensitiveTemp and its
// empty private staging directory. It is safe to call after the file has been
// atomically moved to its final destination.
func CleanupSensitiveTemp(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), sensitiveTempDirectoryPrefix) {
		return errors.New("sensitive temporary path is outside a private staging directory")
	}
	if _, err := os.Lstat(dir); err == nil {
		if err := validateSensitiveTempDirectory(dir); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var errs []error
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
