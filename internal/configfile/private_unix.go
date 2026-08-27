//go:build !windows

package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func ValidatePrivateFile(path string) error {
	if err := validatePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("private file target is unsafe")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("private file permissions are %o, want 600", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("private file owner is unsafe")
	}
	return validatePrivatePathACL(path, false)
}

func PreparePrivateDirectory(path string) error {
	if path == "" {
		return errors.New("private file directory is unavailable")
	}
	_, beforeErr := os.Lstat(path)
	created := errors.Is(beforeErr, os.ErrNotExist)
	if beforeErr != nil && !created {
		return beforeErr
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("private file directory is unsafe")
	}
	if created {
		if err := normalizePrivatePath(path, true); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return validatePrivateDirectory(path)
}

func prepareCreatedPrivateDirectory(path string) error {
	if err := normalizePrivatePath(path, true); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return validatePrivateDirectory(path)
}

func validatePrivateDirectory(path string) error {
	parent, err := os.Lstat(path)
	if err != nil {
		return err
	}
	parentStat, ok := parent.Sys().(*syscall.Stat_t)
	if parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir() || parent.Mode().Perm() != 0o700 || !ok || int(parentStat.Uid) != os.Geteuid() {
		return errors.New("private file directory is unsafe")
	}
	return validatePrivatePathACL(path, true)
}

func validateSensitiveTempDirectory(path string) error {
	return validatePrivateDirectory(path)
}

func preparePrivateTemp(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	if err := normalizePrivateOpenFile(file); err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return validatePrivateOpenFile(file)
}

func validatePrivateOpenFile(file *os.File) error {
	if file == nil {
		return errors.New("private temporary file is unavailable")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("private temporary file metadata is unsafe")
	}
	return validatePrivateOpenFileACL(file)
}
