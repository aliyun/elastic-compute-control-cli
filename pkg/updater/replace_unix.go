//go:build !windows

package updater

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	unixUpdateSchema    = 1
	unixUpdateLockName  = ".ecctl-update.lock"
	unixUpdateStateName = ".ecctl-update-state.json"
	unixUpdateStateMax  = 64 << 10
	unixUpdateCandidate = ".ecctl-update-"
	unixUpdateBackup    = ".ecctl-update-backup-"
	unixStagePrepared   = "prepared"
	unixStageBackedUp   = "backed_up"
	unixStageInstalled  = "installed"
	unixStageVerified   = "verified"
	unixRecoveryTimeout = 30 * time.Second
)

type unixUpdateState struct {
	Schema         int    `json:"schema"`
	Token          string `json:"token"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Candidate      string `json:"candidate"`
	Backup         string `json:"backup"`
	Stage          string `json:"stage"`
}

var syncUpdateDirectory = syncDirectory

func installPreparedExecutable(ctx context.Context, options Options, candidatePath, targetVersion string) (bool, bool, error) {
	unlock, err := acquireUnixUpdateLock(options.Executable)
	if err != nil {
		return false, false, err
	}
	defer unlock()
	if err := reconcileUnixUpdate(ctx, options); err != nil {
		return false, false, err
	}
	if err := verifyExecutableVersion(ctx, options, options.Executable, options.CurrentVersion); err != nil {
		return false, false, fmt.Errorf("current executable changed before update: %w", err)
	}

	directory := filepath.Dir(options.Executable)
	candidate, err := validatePreparedCandidate(directory, candidatePath)
	if err != nil {
		return false, false, err
	}
	token, err := newUnixUpdateToken()
	if err != nil {
		return false, false, err
	}
	state := unixUpdateState{
		Schema:         unixUpdateSchema,
		Token:          token,
		CurrentVersion: options.CurrentVersion,
		TargetVersion:  targetVersion,
		Candidate:      filepath.Base(candidate),
		Backup:         unixUpdateBackup + token,
		Stage:          unixStagePrepared,
	}
	statePath := filepath.Join(directory, unixUpdateStateName)
	if err := requireMissingPath(statePath); err != nil {
		return false, false, err
	}
	if err := writeUnixUpdateState(statePath, state); err != nil {
		return false, false, fmt.Errorf("record prepared update: %w", err)
	}
	backupPath := filepath.Join(directory, state.Backup)
	installed := false
	fail := func(cause error) (bool, bool, error) {
		var recoveryErr error
		if installed {
			recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unixRecoveryTimeout)
			recoveryErr = restoreUnixBackup(recoveryCtx, options, state)
			cancel()
		} else {
			recoveryErr = cleanupUnixUpdateState(options.Executable, state)
		}
		if recoveryErr != nil {
			return true, false, errors.Join(cause, fmt.Errorf("preserve or recover interrupted update: %w", recoveryErr))
		}
		return false, false, cause
	}

	if err := requireMissingPath(backupPath); err != nil {
		return fail(err)
	}
	if err := copyExecutable(options.Executable, backupPath); err != nil {
		return fail(fmt.Errorf("back up current executable: %w", err))
	}
	if err := syncUpdateDirectory(directory); err != nil {
		return fail(fmt.Errorf("sync update backup directory: %w", err))
	}
	state.Stage = unixStageBackedUp
	if err := writeUnixUpdateState(statePath, state); err != nil {
		return fail(fmt.Errorf("record backed-up update: %w", err))
	}
	if err := os.Rename(candidate, options.Executable); err != nil {
		return fail(fmt.Errorf("install updated executable: %w", err))
	}
	installed = true
	if err := syncUpdateDirectory(directory); err != nil {
		return fail(fmt.Errorf("sync updated executable directory: %w", err))
	}
	state.Stage = unixStageInstalled
	if err := writeUnixUpdateState(statePath, state); err != nil {
		return fail(fmt.Errorf("record installed update: %w", err))
	}
	if err := verifyExecutableVersion(ctx, options, options.Executable, targetVersion); err != nil {
		return fail(err)
	}
	state.Stage = unixStageVerified
	if err := writeUnixUpdateState(statePath, state); err != nil {
		return fail(fmt.Errorf("record verified update: %w", err))
	}
	if err := removeRegularFileIfPresent(backupPath); err != nil {
		return true, false, fmt.Errorf("remove update backup: %w", err)
	}
	if err := syncUpdateDirectory(directory); err != nil {
		return true, false, fmt.Errorf("sync updated executable directory: %w", err)
	}
	if err := removeRegularFileIfPresent(statePath); err != nil {
		return true, false, fmt.Errorf("remove update journal: %w", err)
	}
	if err := syncUpdateDirectory(directory); err != nil {
		return true, false, fmt.Errorf("sync completed update directory: %w", err)
	}
	return false, false, nil
}

func validatePreparedCandidate(directory, candidatePath string) (string, error) {
	absCandidate, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", err
	}
	absDirectory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if filepath.Dir(absCandidate) != absDirectory || !strings.HasPrefix(filepath.Base(absCandidate), unixUpdateCandidate) {
		return "", errors.New("prepared update candidate must be a same-directory .ecctl-update-* file")
	}
	if err := requireRegularFile(absCandidate); err != nil {
		return "", fmt.Errorf("validate prepared update candidate: %w", err)
	}
	return absCandidate, nil
}

func acquireUnixUpdateLock(executable string) (func(), error) {
	lockPath := filepath.Join(filepath.Dir(executable), unixUpdateLockName)
	if info, err := os.Lstat(lockPath); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("update lock %s is not a regular file", lockPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !locked {
		_ = lock.Close()
		return nil, WrapError(ErrorBusy, errors.New("another ecctl update is already in progress"))
	}
	return func() {
		_ = lock.Unlock()
		_ = lock.Close()
	}, nil
}

func checkPendingInstall(ctx context.Context, options Options) error {
	unlock, err := acquireUnixUpdateLock(options.Executable)
	if err != nil {
		return err
	}
	defer unlock()
	return reconcileUnixUpdate(ctx, options)
}

func reconcileUnixUpdate(ctx context.Context, options Options) error {
	directory := filepath.Dir(options.Executable)
	statePath := filepath.Join(directory, unixUpdateStateName)
	state, exists, err := readUnixUpdateState(statePath)
	if err != nil || !exists {
		return err
	}
	if err := validateUnixUpdateState(directory, state); err != nil {
		return fmt.Errorf("previous Unix update journal is invalid: %w", err)
	}
	targetInfo, err := os.Lstat(options.Executable)
	if errors.Is(err, os.ErrNotExist) {
		backupPath := filepath.Join(directory, state.Backup)
		if err := requireRegularFile(backupPath); err != nil {
			return fmt.Errorf("previous Unix update is missing both target and valid backup: %w", err)
		}
		if err := verifyExecutableVersion(ctx, options, backupPath, state.CurrentVersion); err != nil {
			return fmt.Errorf("previous Unix update backup is not the recorded current version: %w", err)
		}
		if err := os.Rename(backupPath, options.Executable); err != nil {
			return fmt.Errorf("restore interrupted Unix update: %w", err)
		}
		if err := syncUpdateDirectory(directory); err != nil {
			return fmt.Errorf("sync restored Unix update: %w", err)
		}
		return cleanupUnixUpdateState(options.Executable, state)
	}
	if err != nil {
		return err
	}
	if !targetInfo.Mode().IsRegular() {
		return errors.New("previous Unix update target is not a regular file")
	}
	if verifyExecutableVersion(ctx, options, options.Executable, state.TargetVersion) == nil {
		return cleanupUnixUpdateState(options.Executable, state)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if verifyExecutableVersion(ctx, options, options.Executable, state.CurrentVersion) == nil {
		return cleanupUnixUpdateState(options.Executable, state)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("previous Unix update target is neither recorded version %s nor %s; evidence was preserved", state.CurrentVersion, state.TargetVersion)
}

func restoreUnixBackup(ctx context.Context, options Options, state unixUpdateState) error {
	directory := filepath.Dir(options.Executable)
	backupPath := filepath.Join(directory, state.Backup)
	if err := requireRegularFile(backupPath); err != nil {
		return err
	}
	if err := verifyExecutableVersion(ctx, options, backupPath, state.CurrentVersion); err != nil {
		return fmt.Errorf("validate update backup before restore: %w", err)
	}
	if err := os.Rename(backupPath, options.Executable); err != nil {
		return fmt.Errorf("restore previous executable: %w", err)
	}
	if err := syncUpdateDirectory(directory); err != nil {
		return fmt.Errorf("sync restored executable directory: %w", err)
	}
	return cleanupUnixUpdateState(options.Executable, state)
}

func cleanupUnixUpdateState(executable string, state unixUpdateState) error {
	directory := filepath.Dir(executable)
	for _, name := range []string{state.Candidate, state.Backup} {
		if err := removeRegularFileIfPresent(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	if err := removeRegularFileIfPresent(filepath.Join(directory, unixUpdateStateName)); err != nil {
		return err
	}
	return syncUpdateDirectory(directory)
}

func readUnixUpdateState(path string) (unixUpdateState, bool, error) {
	var state unixUpdateState
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > unixUpdateStateMax {
		return state, false, errors.New("Unix update journal must be a small regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return state, false, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, unixUpdateStateMax+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return state, false, errors.New("Unix update journal contains trailing data")
	}
	return state, true, nil
}

func validateUnixUpdateState(directory string, state unixUpdateState) error {
	if state.Schema != unixUpdateSchema {
		return fmt.Errorf("unsupported schema %d", state.Schema)
	}
	if err := validateUnixUpdateToken(state.Token); err != nil {
		return err
	}
	for label, version := range map[string]string{"current": state.CurrentVersion, "target": state.TargetVersion} {
		normalized, err := NormalizeVersion(version)
		if err != nil || normalized != version {
			return fmt.Errorf("invalid %s version %q", label, version)
		}
	}
	if !safeJournalBase(state.Candidate) || !strings.HasPrefix(state.Candidate, unixUpdateCandidate) {
		return fmt.Errorf("invalid candidate basename %q", state.Candidate)
	}
	if state.Backup != unixUpdateBackup+state.Token || !safeJournalBase(state.Backup) {
		return fmt.Errorf("invalid backup basename %q", state.Backup)
	}
	switch state.Stage {
	case unixStagePrepared, unixStageBackedUp, unixStageInstalled, unixStageVerified:
	default:
		return fmt.Errorf("invalid update stage %q", state.Stage)
	}
	for _, name := range []string{state.Candidate, state.Backup} {
		path := filepath.Join(directory, name)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("journal artifact %s is not a regular file", name)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func safeJournalBase(name string) bool {
	return name != "" && name != "." && !filepath.IsAbs(name) && filepath.Base(name) == name &&
		!strings.Contains(name, "..") && !strings.ContainsAny(name, `/\\`)
}

func writeUnixUpdateState(path string, state unixUpdateState) error {
	directory := filepath.Dir(path)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(directory, ".ecctl-update-state-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	removeTemp = false
	return syncUpdateDirectory(directory)
}

func newUnixUpdateToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func validateUnixUpdateToken(token string) error {
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 16 || token != strings.ToLower(token) {
		return errors.New("invalid Unix update token")
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func removeRegularFileIfPresent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove non-regular update artifact %s", path)
	}
	return os.Remove(path)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func RunInternalUpdate([]string) error {
	return errors.New("internal update helper is only available on Windows")
}
