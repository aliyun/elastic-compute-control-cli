//go:build !windows

package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func loadOrCreateInstallationToken(ctx context.Context, configPath string) ([]byte, error) {
	directory, err := ensureTelemetryDirectory(configPath)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(directory, "installation-v1.lock")
	lockFile, err := openIdentityLock(lockPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
	}()
	lockCtx, cancel := context.WithTimeout(ctx, identityLockTimeout)
	defer cancel()
	if err := lockIdentityFile(lockCtx, lockFile); err != nil {
		return nil, err
	}

	path := filepath.Join(directory, "installation-v1")
	if raw, err := readIdentityCacheFile(path); err == nil {
		return decodeInstallationToken(raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	token, err := newInstallationToken()
	if err != nil {
		return nil, err
	}
	raw, err := encodeInstallationToken(token)
	if err != nil {
		return nil, err
	}
	if err := writeSecureTelemetryFile(directory, path, lockPath, ".installation-v1-*.tmp", raw); err != nil {
		return nil, err
	}
	return token, nil
}
