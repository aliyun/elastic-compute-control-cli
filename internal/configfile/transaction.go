package configfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

var ErrTargetReplaced = errors.New("configuration target was replaced")

// Target is the stable filesystem target behind a requested configuration
// path. Resolving symlinks once and verifying them again under the sidecar lock
// prevents a writer from silently switching files during a transaction.
type Target struct {
	requestedPath string
	path          string
}

type PrivateReplace struct {
	target    *Target
	file      *os.File
	path      string
	reserve   int64
	committed bool
}

func Resolve(requestedPath string, createParent bool) (*Target, error) {
	if requestedPath == "" {
		return nil, errors.New("configuration path is required")
	}
	abs, err := filepath.Abs(requestedPath)
	if err != nil {
		return nil, err
	}
	if createParent {
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, err
		}
	}
	resolved, err := resolvePath(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve configuration target: %w", err)
	}
	return &Target{requestedPath: abs, path: resolved}, nil
}

func resolvePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Abs(resolved)
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if _, lstatErr := os.Lstat(path); lstatErr == nil {
		// The requested path exists but its target does not. Never replace a
		// dangling symlink with a regular configuration file.
		return "", err
	} else if !os.IsNotExist(lstatErr) {
		return "", lstatErr
	}
	parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
	if parentErr != nil {
		return "", parentErr
	}
	parent, parentErr = filepath.Abs(parent)
	if parentErr != nil {
		return "", parentErr
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func (t *Target) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

func (t *Target) Verify() error {
	if t == nil || t.requestedPath == "" || t.path == "" {
		return errors.New("configuration target is unavailable")
	}
	resolved, err := resolvePath(t.requestedPath)
	if err != nil {
		return err
	}
	if resolved != t.path {
		return ErrTargetReplaced
	}
	return nil
}

func (t *Target) WithLock(ctx context.Context, timeout, retry time.Duration, fn func() error) error {
	if t == nil || t.path == "" {
		return errors.New("configuration target is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if retry <= 0 {
		retry = 25 * time.Millisecond
	}
	lock := flock.New(t.path + ".lock")
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	locked, err := lock.TryLockContext(lockCtx, retry)
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("timed out acquiring configuration lock")
	}
	defer lock.Unlock()
	if err := t.Verify(); err != nil {
		return err
	}
	if fn == nil {
		return nil
	}
	return fn()
}

func (t *Target) Read() ([]byte, os.FileInfo, error) {
	if t == nil || t.path == "" {
		return nil, nil, errors.New("configuration target is unavailable")
	}
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(t.path)
	if err != nil {
		return nil, nil, err
	}
	return raw, info, nil
}

func (t *Target) AtomicWrite(raw []byte, mode os.FileMode) error {
	if t == nil || t.path == "" {
		return errors.New("configuration target is unavailable")
	}
	if mode == 0 {
		mode = 0o600
	}
	temp, metadata, err := createAtomicTemp(filepath.Dir(t.path), ".ecctl-config-*.tmp", t.path, mode.Perm())
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = CleanupSensitiveTemp(tempPath) }()
	closeWithError := func(current error) error {
		if closeErr := temp.Close(); current == nil {
			return closeErr
		}
		return current
	}
	if err := prepareReplacementBeforeWrite(temp, metadata); err != nil {
		return closeWithError(err)
	}
	if _, err := temp.Write(raw); err != nil {
		return closeWithError(err)
	}
	if err := temp.Sync(); err != nil {
		return closeWithError(err)
	}
	if err := finishReplacementAfterWrite(temp, metadata); err != nil {
		return closeWithError(err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, t.path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(t.path))
}

// AtomicWritePrivate replaces a credential-bearing file with a canonical
// current-user-only file. Unlike AtomicWrite, it never preserves arbitrary
// security metadata from the old target.
func (t *Target) AtomicWritePrivate(raw []byte) error {
	if t == nil || t.path == "" {
		return errors.New("private target is unavailable")
	}
	dir := filepath.Dir(t.path)
	if err := PreparePrivateDirectory(dir); err != nil {
		return err
	}
	if _, err := os.Lstat(t.path); err == nil {
		if err := ValidatePrivateFile(t.path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := CreateSensitiveTemp(dir, ".ecctl-private-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = CleanupSensitiveTemp(tempPath) }()
	closeWithError := func(current error) error {
		if closeErr := temp.Close(); current == nil {
			return closeErr
		}
		return current
	}
	if err := preparePrivateTemp(temp); err != nil {
		return closeWithError(err)
	}
	if _, err := temp.Write(raw); err != nil {
		return closeWithError(err)
	}
	if err := temp.Sync(); err != nil {
		return closeWithError(err)
	}
	if err := validatePrivateOpenFile(temp); err != nil {
		return closeWithError(err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, t.path); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	return ValidatePrivateFile(t.path)
}

// BeginPrivateReplace prepares and reserves space for a private atomic
// replacement before a remote one-time credential rotation is attempted.
func (t *Target) BeginPrivateReplace(reserve int64) (*PrivateReplace, error) {
	if t == nil || t.path == "" {
		return nil, errors.New("private target is unavailable")
	}
	if reserve <= 0 {
		return nil, errors.New("private replacement reserve must be positive")
	}
	dir := filepath.Dir(t.path)
	if err := PreparePrivateDirectory(dir); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(t.path); err == nil {
		if err := ValidatePrivateFile(t.path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := CreateSensitiveTemp(dir, ".ecctl-private-reserved-*.tmp")
	if err != nil {
		return nil, err
	}
	replacement := &PrivateReplace{target: t, file: file, path: file.Name(), reserve: reserve}
	if err := reservePrivateFile(file, reserve); err != nil {
		_ = replacement.Abort()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = replacement.Abort()
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = replacement.Abort()
		return nil, err
	}
	if err := validatePrivateOpenFile(file); err != nil {
		_ = replacement.Abort()
		return nil, err
	}
	return replacement, nil
}

func (p *PrivateReplace) Commit(raw []byte) error {
	if p == nil || p.target == nil || p.file == nil || p.committed {
		return errors.New("private replacement is unavailable")
	}
	if int64(len(raw)) > p.reserve {
		return errors.New("private replacement exceeds reserved size")
	}
	if err := p.target.Verify(); err != nil {
		return err
	}
	if _, err := p.file.WriteAt(raw, 0); err != nil {
		return err
	}
	if err := p.file.Truncate(int64(len(raw))); err != nil {
		return err
	}
	if err := p.file.Sync(); err != nil {
		return err
	}
	if err := validatePrivateOpenFile(p.file); err != nil {
		return err
	}
	closeErr := p.file.Close()
	p.file = nil
	if closeErr != nil {
		return closeErr
	}
	if err := replaceFile(p.path, p.target.path); err != nil {
		return err
	}
	p.committed = true
	cleanupErr := CleanupSensitiveTemp(p.path)
	syncErr := syncDirectory(filepath.Dir(p.target.path))
	validateErr := ValidatePrivateFile(p.target.path)
	return errors.Join(cleanupErr, syncErr, validateErr)
}

func (p *PrivateReplace) Abort() error {
	if p == nil || p.committed {
		return nil
	}
	var closeErr error
	if p.file != nil {
		closeErr = p.file.Close()
		p.file = nil
	}
	cleanupErr := CleanupSensitiveTemp(p.path)
	return errors.Join(closeErr, cleanupErr)
}
