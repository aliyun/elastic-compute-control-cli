// Package journalfile serializes cleanup-journal updates across processes.
package journalfile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const (
	lockRetryDelay  = 10 * time.Millisecond
	lockWaitTimeout = 30 * time.Second
)

// WithLock runs update while holding the short-lived advisory transaction lock
// associated with path.
// The lock file is separate so atomic replacement of the journal does not
// change the inode that concurrent processes coordinate on.
func WithLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".lock", update)
}

// WithReplayLock prevents two processes from replaying the same journal at the
// same time without blocking short journal transactions by an active replay.
func WithReplayLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", update)
}

func withLock(ctx context.Context, path, lockPath string, update func() error) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock := flock.New(lockPath)
	waitCtx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	locked, lockErr := lock.TryLockContext(waitCtx, lockRetryDelay)
	cancel()
	if lockErr != nil {
		_ = lock.Close()
		return fmt.Errorf("lock cleanup journal %s: %w", path, lockErr)
	}
	if !locked {
		_ = lock.Close()
		return fmt.Errorf("lock cleanup journal %s: not acquired", path)
	}
	defer func() {
		if unlockErr := lock.Unlock(); err == nil && unlockErr != nil {
			err = fmt.Errorf("unlock cleanup journal %s: %w", path, unlockErr)
		}
		if closeErr := lock.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close cleanup journal lock %s: %w", path, closeErr)
		}
	}()
	return update()
}
