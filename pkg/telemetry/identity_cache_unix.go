//go:build !windows

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const identityCacheMaxBytes = 1 << 20

func openIdentityCache(ctx context.Context, configPath string) (*identityCacheHandle, error) {
	directory, err := ensureTelemetryDirectory(configPath)
	if err != nil {
		return nil, err
	}

	lockPath := filepath.Join(directory, "identity-v1.lock")
	lockFile, err := openIdentityLock(lockPath)
	if err != nil {
		return nil, err
	}
	lockCtx, cancel := context.WithTimeout(ctx, identityLockTimeout)
	defer cancel()
	if err := lockIdentityFile(lockCtx, lockFile); err != nil {
		_ = lockFile.Close()
		return nil, err
	}

	cachePath := filepath.Join(directory, "identity-v1.json")
	cache := identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{}}
	if raw, err := readIdentityCacheFile(cachePath); err == nil {
		cache = decodeIdentityCache(raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return nil, err
	}

	handle := &identityCacheHandle{cache: cache}
	handle.write = func(updated identityCacheFile) error {
		return writeIdentityCacheFile(directory, cachePath, lockPath, updated)
	}
	handle.close = func() {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
	}
	return handle, nil
}

func ensureTelemetryDirectory(configPath string) (string, error) {
	parent, err := ensureIdentityConfigParent(configPath)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(parent, "telemetry")
	if info, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if err := validateIdentityDirectoryInfo(info); err != nil {
		return "", err
	}
	if err := validateIdentityDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func ensureIdentityConfigParent(configPath string) (string, error) {
	parent := filepath.Dir(configPath)
	if info, err := os.Lstat(parent); err == nil {
		return parent, validateIdentityDirectoryInfo(info)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	ancestor := filepath.Dir(parent)
	if err := validateIdentityDirectory(ancestor); err != nil {
		return "", err
	}
	if err := os.Mkdir(parent, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := validateIdentityDirectory(parent); err != nil {
		return "", err
	}
	return parent, nil
}

func openIdentityLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, err
	}
	if errors.Is(err, unix.EEXIST) {
		if err := validateIdentityFilePath(path); err != nil {
			return nil, err
		}
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("identity lock is unavailable")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := validateIdentityFileInfo(info); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func lockIdentityFile(ctx context.Context, file *os.File) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func readIdentityCacheFile(path string) ([]byte, error) {
	if err := validateIdentityFilePath(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("identity cache is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := validateIdentityFileInfo(info); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, identityCacheMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > identityCacheMaxBytes {
		return nil, errors.New("identity cache is too large")
	}
	return raw, nil
}

func writeIdentityCacheFile(directory, path, lockPath string, cache identityCacheFile) error {
	raw, err := encodeIdentityCache(cache)
	if err != nil {
		return err
	}
	return writeSecureTelemetryFile(directory, path, lockPath, ".identity-v1-*.tmp", raw)
}

func writeSecureTelemetryFile(directory, path, lockPath, temporaryPattern string, raw []byte) error {
	if err := validateIdentityDirectory(directory); err != nil {
		return err
	}
	if err := validateIdentityFilePath(lockPath); err != nil {
		return err
	}
	if err := validateIdentityTarget(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, temporaryPattern)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true

	if err := validateIdentityDirectory(directory); err != nil {
		return err
	}
	if err := validateIdentityFilePath(lockPath); err != nil {
		return err
	}
	if err := validateIdentityTarget(path); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return validateIdentityFilePath(path)
}

func validateIdentityTarget(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return validateIdentityFilePath(path)
}

func validateIdentityDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return validateIdentityDirectoryInfo(info)
}

func validateIdentityDirectoryInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("identity cache directory is unsafe")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("identity cache directory permissions are unsafe")
	}
	return validateIdentityOwner(info)
}

func validateIdentityFilePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return validateIdentityFileInfo(info)
}

func validateIdentityFileInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("identity cache file is unsafe")
	}
	if info.Mode().Perm() != 0o600 {
		return errors.New("identity cache file permissions are unsafe")
	}
	return validateIdentityOwner(info)
}

func validateIdentityOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("identity cache owner is unsafe")
	}
	return nil
}
