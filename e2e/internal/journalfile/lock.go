// Package journalfile serializes cleanup-journal updates across processes.
package journalfile

import (
	"context"
	"errors"
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
	return withLock(waitCtx, path, path+".lock", false, true, update)
}

// WithExistingParentLock checks an existing journal without recreating a lost
// parent directory. The directory must still be the same one after locking.
func WithExistingParentLock(ctx context.Context, path string, update func() error) error {
	waitCtx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	defer cancel()
	return withLock(waitCtx, path, path+".lock", false, false, update)
}

// WithReplayLock prevents two processes from replaying the same journal at the
// same time without blocking short journal transactions by an active replay.
func WithReplayLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", false, true, update)
}

// WithExistingReplayLock reserves a new allocation only in a journal parent
// that the runner has already observed. It cannot recreate a lost directory.
func WithExistingReplayLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", false, false, update)
}

// WithCleanupLock permits independent runtime deletes to overlap, while
// excluding new allocation reservations and unattended replay on the same file.
// These operations may outlast a short journal transaction; callers own their
// cancellation/deadline rather than inheriting the transaction's 30s limit.
func WithCleanupLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", true, true, update)
}

// WithExistingCleanupLock guards a registered finalizer without recreating a
// journal directory that disappeared before cleanup began.
func WithExistingCleanupLock(ctx context.Context, path string, update func() error) error {
	return withLock(ctx, path, path+".replay.lock", true, false, update)
}

// AcquireRunLease rejects another active runner using the same resolved journal.
// It uses a separate file so the runner can still take its replay and journal
// transaction locks while the lease is held.
func AcquireRunLease(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock := flock.New(path + ".run.lock")
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock cleanup journal run %s: %w", path, err)
	}
	if !locked {
		_ = lock.Close()
		return nil, fmt.Errorf("cleanup journal %s is in use by another run", path)
	}
	return func() error {
		unlockErr := lock.Unlock()
		closeErr := lock.Close()
		if unlockErr != nil {
			return fmt.Errorf("unlock cleanup journal run %s: %w", path, unlockErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close cleanup journal run lock %s: %w", path, closeErr)
		}
		return nil
	}, nil
}

func withLock(ctx context.Context, path, lockPath string, shared, createParent bool, update func() error) (err error) {
	parent := filepath.Dir(path)
	var original os.FileInfo
	if createParent {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	} else {
		original, err = os.Stat(parent)
		if err != nil {
			return fmt.Errorf("existing cleanup journal parent %s: %w", parent, err)
		}
		if !original.IsDir() {
			return fmt.Errorf("existing cleanup journal parent %s is not a directory", parent)
		}
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
	checkParent := func() error {
		if createParent {
			return nil
		}
		current, statErr := os.Stat(parent)
		if statErr != nil {
			return fmt.Errorf("existing cleanup journal parent %s changed: %w", parent, statErr)
		}
		if !os.SameFile(original, current) {
			return fmt.Errorf("existing cleanup journal parent %s changed", parent)
		}
		return nil
	}
	if err := checkParent(); err != nil {
		return err
	}
	return errors.Join(update(), checkParent())
}
