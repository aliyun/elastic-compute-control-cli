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
	waitCtx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	defer cancel()
	return withLock(waitCtx, path, path+".lock", false, update)
}

// WithReplayLock prevents two processes from replaying the same journal at the
// same time without blocking short journal transactions by an active replay.
func WithReplayLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", false, update)
}

// WithCleanupLock permits independent runtime deletes to overlap, while
// excluding new allocation reservations and unattended replay on the same file.
// These operations may outlast a short journal transaction; callers own their
// cancellation/deadline rather than inheriting the transaction's 30s limit.
func WithCleanupLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", true, update)
}

func withLock(ctx context.Context, path, lockPath string, shared bool, update func() error) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock := flock.New(lockPath)
	acquire := lock.TryLockContext
	if shared {
		acquire = lock.TryRLockContext
	}
	locked, lockErr := acquire(ctx, lockRetryDelay)
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
