package updater

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const (
	autoCheckInterval = 24 * time.Hour
	failureBackoff    = time.Hour
	maxCacheBytes     = 64 << 10
)

type AutoCheckOptions struct {
	CurrentVersion   string
	CachePath        string
	Client           *Client
	Now              func() time.Time
	MarkNotification bool
}

type AutoCheckResult struct {
	LatestVersion string
	Available     bool
	Notify        bool
}

type cacheState struct {
	CheckedAt       time.Time `json:"checked_at,omitempty"`
	FailedAt        time.Time `json:"failed_at,omitempty"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	NotifiedVersion string    `json:"notified_version,omitempty"`
	NotifiedAt      time.Time `json:"notified_at,omitempty"`
}

func DefaultCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ecctl", "update-check.json"), nil
}

func AutoCheck(ctx context.Context, options AutoCheckOptions) (AutoCheckResult, error) {
	current, err := NormalizeVersion(options.CurrentVersion)
	if err != nil {
		return AutoCheckResult{}, err
	}
	if options.CachePath == "" {
		options.CachePath, err = DefaultCachePath()
		if err != nil {
			return AutoCheckResult{}, err
		}
	}
	if options.Client == nil {
		options.Client = NewClient(800 * time.Millisecond)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	now := options.Now().UTC()
	unlock, locked, err := acquireCacheLock(options.CachePath)
	if err != nil {
		return AutoCheckResult{}, err
	}
	if !locked {
		return AutoCheckResult{}, nil
	}
	defer unlock()

	// Keep every cache read and write under the same lock. Windows cannot
	// atomically replace the file while another process has it open for reading.
	state := readCache(options.CachePath)
	if freshResult, ok := cachedAutoCheckResult(current, state, now); ok {
		return maybeMarkNotification(options.CachePath, state, freshResult, now, false, options.MarkNotification)
	}
	if !state.FailedAt.IsZero() && now.Sub(state.FailedAt) >= 0 && now.Sub(state.FailedAt) < failureBackoff {
		return AutoCheckResult{}, nil
	}
	latest, err := options.Client.LatestVersion(ctx)
	if err != nil {
		state.FailedAt = now
		if writeErr := writeCache(options.CachePath, state); writeErr != nil {
			return AutoCheckResult{}, errors.Join(err, writeErr)
		}
		return AutoCheckResult{}, err
	}
	state.CheckedAt = now
	state.FailedAt = time.Time{}
	state.LatestVersion = latest
	result := compareAutoCheck(current, latest)
	return maybeMarkNotification(options.CachePath, state, result, now, true, options.MarkNotification)
}

func cachedAutoCheckResult(current string, state cacheState, now time.Time) (AutoCheckResult, bool) {
	if state.CheckedAt.IsZero() || now.Sub(state.CheckedAt) < 0 || now.Sub(state.CheckedAt) >= autoCheckInterval {
		return AutoCheckResult{}, false
	}
	latest, err := NormalizeVersion(state.LatestVersion)
	if err != nil || isPrereleaseVersion(latest) {
		return AutoCheckResult{}, false
	}
	return compareAutoCheck(current, state.LatestVersion), true
}

func compareAutoCheck(current, latest string) AutoCheckResult {
	order, err := CompareVersions(latest, current)
	return AutoCheckResult{LatestVersion: latest, Available: err == nil && order > 0}
}

func maybeMarkNotification(cachePath string, state cacheState, result AutoCheckResult, now time.Time, persist bool, markNotification bool) (AutoCheckResult, error) {
	if !result.Available {
		if persist {
			if err := writeCache(cachePath, state); err != nil {
				return AutoCheckResult{}, err
			}
		}
		return result, nil
	}
	if markNotification && notificationDue(state, result, now) {
		result.Notify = true
		state.NotifiedVersion = result.LatestVersion
		state.NotifiedAt = now
		persist = true
	}
	if persist {
		if err := writeCache(cachePath, state); err != nil {
			return AutoCheckResult{}, err
		}
	}
	return result, nil
}

func notificationDue(state cacheState, result AutoCheckResult, now time.Time) bool {
	return state.NotifiedVersion != result.LatestVersion || state.NotifiedAt.IsZero() ||
		now.Sub(state.NotifiedAt) < 0 || now.Sub(state.NotifiedAt) >= autoCheckInterval
}

func readCache(path string) cacheState {
	var state cacheState
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Size() < 0 || pathInfo.Size() > maxCacheBytes {
		return state
	}
	file, err := os.Open(path)
	if err != nil {
		return state
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxCacheBytes {
		return state
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxCacheBytes+1))
	if err != nil || len(raw) > maxCacheBytes {
		return state
	}
	if json.Unmarshal(raw, &state) != nil {
		return cacheState{}
	}
	return state
}

func writeCache(path string, state cacheState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".update-check-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func acquireCacheLock(cachePath string) (func(), bool, error) {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return nil, false, err
	}
	lockPath := cachePath + ".lock"
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, false, err
	}
	if !locked {
		_ = lock.Close()
		return func() {}, false, nil
	}
	return func() {
		_ = lock.Unlock()
		_ = lock.Close()
	}, true, nil
}
