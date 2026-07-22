//go:build windows

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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// These values are a cross-version protocol: an installed older ecctl
	// starts the downloaded newer binary with internalApplyMode. Keep v1
	// decoding available when adding future helper protocols.
	internalApplyMode   = "apply-v1"
	internalCleanupMode = "cleanup-v1"
	internalProbeMode   = "probe-v1"
	updateStatusSuffix  = ".update-status.json"
	updateLockSuffix    = ".update.lock"
	pendingStatusMaxAge = 5 * time.Minute
	pendingTerminalAge  = 30 * time.Minute
	windowsStillActive  = 259
)

type windowsUpdateStatus struct {
	State         string    `json:"state"`
	TargetVersion string    `json:"target_version"`
	Token         string    `json:"token"`
	Error         string    `json:"error,omitempty"`
	HelperPID     int       `json:"helper_pid,omitempty"`
	HelperPath    string    `json:"helper_path,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func installPreparedExecutable(ctx context.Context, options Options, candidatePath, targetVersion string) (bool, bool, error) {
	token, releaseLock, err := acquireWindowsUpdateLock(options.Executable)
	if err != nil {
		return false, false, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			releaseLock()
		}
	}()
	if err := verifyExecutableVersion(ctx, options, options.Executable, options.CurrentVersion); err != nil {
		return false, false, fmt.Errorf("current executable changed before update: %w", err)
	}
	if output, err := options.RunCommand(ctx, nil, candidatePath, "__update", internalProbeMode); err != nil {
		return false, false, WrapError(ErrorInvalidTarget, commandError("validate Windows update protocol", output, err))
	}
	statusPath := options.Executable + updateStatusSuffix
	if err := writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{State: "pending", TargetVersion: targetVersion, Token: token}); err != nil {
		return false, false, fmt.Errorf("record pending Windows update: %w", err)
	}
	command := exec.Command(candidatePath,
		"__update",
		internalApplyMode,
		strconv.Itoa(os.Getpid()),
		options.Executable,
		targetVersion,
		token,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		startErr := fmt.Errorf("start Windows update helper: %w", err)
		_ = writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{State: "failed", TargetVersion: targetVersion, Token: token, Error: startErr.Error()})
		return false, false, startErr
	}
	if err := writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{
		State: "pending", TargetVersion: targetVersion, Token: token,
		HelperPID: command.Process.Pid, HelperPath: candidatePath,
	}); err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		statusErr := fmt.Errorf("record running Windows update helper: %w", err)
		_ = writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{State: "failed", TargetVersion: targetVersion, Token: token, Error: statusErr.Error()})
		return false, false, statusErr
	}
	_ = command.Process.Release()
	keepLock = true
	return true, true, nil
}

func RunInternalUpdate(args []string) error {
	if len(args) == 0 {
		return errors.New("internal update mode is required")
	}
	switch args[0] {
	case internalProbeMode:
		if len(args) != 1 {
			return errors.New("internal update probe does not accept arguments")
		}
		return nil
	case internalApplyMode:
		return applyPendingUpdate(args[1:])
	case internalCleanupMode:
		return cleanupUpdateHelper(args[1:])
	default:
		return fmt.Errorf("unknown internal update mode %q", args[0])
	}
}

func applyPendingUpdate(args []string) (resultErr error) {
	if len(args) != 4 {
		return errors.New("internal apply update requires parent PID, target path, version, and token")
	}
	parentPID, err := parseProcessID(args[0])
	if err != nil {
		return err
	}
	targetVersion, err := NormalizeVersion(args[2])
	if err != nil {
		return err
	}
	token, err := validateWindowsUpdateToken(args[3])
	if err != nil {
		return err
	}
	helperPath, err := os.Executable()
	if err != nil {
		return err
	}
	targetPath, helperPath, err := validateWindowsUpdatePaths(args[1], helperPath)
	if err != nil {
		return err
	}
	statusPath := targetPath + updateStatusSuffix
	lockPath := targetPath + updateLockSuffix
	waitForProcess(parentPID)
	lockGuard, err := openWindowsUpdateLockGuard(lockPath, token)
	if err != nil {
		return fmt.Errorf("claim Windows update lock: %w", err)
	}
	defer func() {
		_ = lockGuard.Close()
	}()
	defer func() {
		if resultErr != nil {
			message := resultErr.Error()
			if len(message) > 500 {
				message = message[:500]
			}
			_ = writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{State: "failed", TargetVersion: targetVersion, Token: token, Error: message})
		}
	}()

	backupPath := fmt.Sprintf("%s.update-backup-%d", targetPath, parentPID)
	installPath := fmt.Sprintf("%s.update-new-%d", targetPath, os.Getpid())
	if err := requireMissingPath(backupPath); err != nil {
		return err
	}
	if err := requireMissingPath(installPath); err != nil {
		return err
	}
	if err := copyExecutable(targetPath, backupPath); err != nil {
		return fmt.Errorf("back up current executable: %w", err)
	}
	defer os.Remove(installPath)
	if err := copyExecutable(helperPath, installPath); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("prepare Windows executable replacement: %w", err)
	}
	if err := os.Rename(installPath, targetPath); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("install updated executable: %w", err)
	}
	options := Options{Executable: targetPath, RunCommand: runCommand}
	validateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := verifyExecutableVersion(validateCtx, options, targetPath, targetVersion); err != nil {
		if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous executable: %w", restoreErr))
		}
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove update backup: %w", err)
	}
	if err := writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{State: "succeeded", TargetVersion: targetVersion, Token: token}); err != nil {
		return fmt.Errorf("record completed Windows update: %w", err)
	}
	_ = startWindowsCleanup(targetPath, helperPath)
	return nil
}

func cleanupUpdateHelper(args []string) error {
	if len(args) != 2 {
		return errors.New("internal cleanup requires parent PID and helper path")
	}
	parentPID, err := parseProcessID(args[0])
	if err != nil {
		return err
	}
	currentPath, err := os.Executable()
	if err != nil {
		return err
	}
	helperPath, err := validateWindowsCleanupPath(args[1], currentPath)
	if err != nil {
		return err
	}
	waitForProcess(parentPID)
	deadline := time.Now().Add(time.Minute)
	for {
		err := os.Remove(helperPath)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remove Windows update helper: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func startWindowsCleanup(targetPath, helperPath string) error {
	command := exec.Command(targetPath,
		"__update",
		internalCleanupMode,
		strconv.Itoa(os.Getpid()),
		helperPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Windows cleanup helper: %w", err)
	}
	return command.Process.Release()
}

func waitForProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_, _ = process.Wait()
}

func parseProcessID(raw string) (int, error) {
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid process ID %q", raw)
	}
	return pid, nil
}

func validateWindowsUpdatePaths(targetPath, helperPath string) (string, string, error) {
	target, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", err
	}
	helper, err := filepath.Abs(helperPath)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(filepath.Dir(target), filepath.Dir(helper)) || !strings.HasPrefix(filepath.Base(helper), ".ecctl-update-") {
		return "", "", errors.New("invalid Windows update helper paths")
	}
	return filepath.Clean(target), filepath.Clean(helper), nil
}

func validateWindowsCleanupPath(helperPath, currentPath string) (string, error) {
	helper, err := filepath.Abs(helperPath)
	if err != nil {
		return "", err
	}
	current, err := filepath.Abs(currentPath)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Dir(helper), filepath.Dir(current)) || !strings.HasPrefix(filepath.Base(helper), ".ecctl-update-") {
		return "", errors.New("invalid Windows cleanup helper path")
	}
	return filepath.Clean(helper), nil
}

func checkPendingInstall(_ context.Context, options Options) error {
	statusPath := options.Executable + updateStatusSuffix
	info, err := os.Stat(statusPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read previous Windows update status: %w", err)
	}
	if info.Size() > 64*1024 {
		_ = os.Remove(statusPath)
		return errors.New("previous Windows update status is invalid")
	}
	raw, err := os.ReadFile(statusPath)
	if err != nil {
		return fmt.Errorf("read previous Windows update status: %w", err)
	}
	var status windowsUpdateStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		_ = os.Remove(statusPath)
		return errors.New("previous Windows update status is invalid")
	}
	if _, err := validateWindowsUpdateToken(status.Token); err != nil {
		_ = os.Remove(statusPath)
		return errors.New("previous Windows update status has an invalid token")
	}
	target, err := NormalizeVersion(status.TargetVersion)
	if err != nil {
		clearWindowsUpdateState(options.Executable, status.Token)
		return errors.New("previous Windows update status has an invalid target version")
	}
	switch status.State {
	case "succeeded":
		if options.CurrentVersion != target {
			clearWindowsUpdateState(options.Executable, status.Token)
			return fmt.Errorf("previous Windows update reported success for %s, but the running version is %s; retry the update", target, options.CurrentVersion)
		}
		clearWindowsUpdateState(options.Executable, status.Token)
		return nil
	case "failed":
		clearWindowsUpdateState(options.Executable, status.Token)
		if status.Error == "" {
			status.Error = "unknown helper failure"
		}
		return fmt.Errorf("previous Windows update to %s failed: %s", target, status.Error)
	case "pending":
		if options.CurrentVersion == target {
			clearWindowsUpdateState(options.Executable, status.Token)
			return nil
		}
		age := time.Since(status.UpdatedAt)
		if !status.UpdatedAt.IsZero() && age >= 0 && age <= pendingStatusMaxAge {
			return WrapError(ErrorBusy, fmt.Errorf("Windows update to %s is still pending; close other ecctl processes and try again", target))
		}
		if age >= 0 && age <= pendingTerminalAge && validWindowsUpdateHelper(options.Executable, status) {
			return WrapError(ErrorBusy, fmt.Errorf("Windows update to %s is still pending; close other ecctl processes and try again", target))
		}
		clearWindowsUpdateState(options.Executable, status.Token)
		return fmt.Errorf("previous Windows update to %s did not complete; retry the update", target)
	default:
		clearWindowsUpdateState(options.Executable, status.Token)
		return errors.New("previous Windows update status has an invalid state")
	}
}

func acquireWindowsUpdateLock(targetPath string) (string, func(), error) {
	lockPath := targetPath + updateLockSuffix
	token, err := newWindowsUpdateToken()
	if err != nil {
		return "", nil, err
	}
	err = os.Mkdir(lockPath, 0o700)
	if err == nil {
		ownerPath := windowsUpdateLockOwner(lockPath)
		file, createErr := os.OpenFile(ownerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			_ = os.Remove(lockPath)
			return "", nil, fmt.Errorf("create Windows update lock owner: %w", createErr)
		}
		if _, writeErr := fmt.Fprintln(file, token); writeErr != nil {
			_ = file.Close()
			_ = os.Remove(ownerPath)
			_ = os.Remove(lockPath)
			return "", nil, writeErr
		}
		if syncErr := file.Sync(); syncErr != nil {
			_ = file.Close()
			_ = os.Remove(ownerPath)
			_ = os.Remove(lockPath)
			return "", nil, syncErr
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(ownerPath)
			_ = os.Remove(lockPath)
			return "", nil, closeErr
		}
		return token, func() { _ = removeWindowsUpdateLock(lockPath, token) }, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", nil, fmt.Errorf("create Windows update lock: %w", err)
	}
	if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > pendingStatusMaxAge {
		staleToken, readErr := readWindowsUpdateLock(lockPath)
		if readErr == nil && removeWindowsUpdateLock(lockPath, staleToken) {
			return acquireWindowsUpdateLock(targetPath)
		}
		if readErr != nil && reclaimMalformedWindowsUpdateLock(lockPath, token) {
			return acquireWindowsUpdateLock(targetPath)
		}
	}
	return "", nil, WrapError(ErrorBusy, errors.New("another Windows update is already in progress"))
}

func reclaimMalformedWindowsUpdateLock(lockPath, token string) bool {
	info, err := os.Lstat(lockPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	entries, err := os.ReadDir(lockPath)
	if err != nil || !safeMalformedLockEntries(entries) {
		return false
	}
	quarantine := lockPath + ".stale-" + token
	if err := requireMissingPath(quarantine); err != nil {
		return false
	}
	if err := os.Rename(lockPath, quarantine); err != nil {
		return false
	}
	entries, err = os.ReadDir(quarantine)
	if err != nil || !safeMalformedLockEntries(entries) {
		// The stale lock is detached, but unexpected race evidence is kept.
		return true
	}
	if len(entries) == 1 {
		_ = os.Remove(filepath.Join(quarantine, "owner"))
	}
	_ = os.Remove(quarantine)
	return true
}

func safeMalformedLockEntries(entries []os.DirEntry) bool {
	if len(entries) == 0 {
		return true
	}
	if len(entries) != 1 || entries[0].Name() != "owner" {
		return false
	}
	info, err := entries[0].Info()
	return err == nil && info.Mode().IsRegular()
}

func clearWindowsUpdateState(targetPath, token string) {
	statusPath := targetPath + updateStatusSuffix
	var current windowsUpdateStatus
	if raw, err := os.ReadFile(statusPath); err == nil && json.Unmarshal(raw, &current) == nil && current.Token == token {
		if os.Remove(statusPath) == nil {
			_ = removeWindowsUpdateLock(targetPath+updateLockSuffix, token)
		}
	}
}

func newWindowsUpdateToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate Windows update token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func validateWindowsUpdateToken(raw string) (string, error) {
	if len(raw) != 32 || raw != strings.ToLower(raw) {
		return "", errors.New("invalid Windows update token")
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", errors.New("invalid Windows update token")
	}
	return raw, nil
}

func windowsUpdateLockOwner(lockPath string) string {
	return filepath.Join(lockPath, "owner")
}

func readWindowsUpdateLock(lockPath string) (string, error) {
	raw, err := os.ReadFile(windowsUpdateLockOwner(lockPath))
	if err != nil {
		return "", err
	}
	if len(raw) != 33 || raw[32] != '\n' {
		return "", errors.New("invalid Windows update lock")
	}
	return validateWindowsUpdateToken(string(raw[:32]))
}

func windowsUpdateLockMatches(lockPath, token string) bool {
	current, err := readWindowsUpdateLock(lockPath)
	return err == nil && current == token
}

func removeWindowsUpdateLock(lockPath, token string) bool {
	current, err := readWindowsUpdateLock(lockPath)
	if err != nil || current != token {
		return false
	}
	if err := os.Remove(windowsUpdateLockOwner(lockPath)); err != nil {
		return false
	}
	return os.Remove(lockPath) == nil
}

func openWindowsUpdateLockGuard(lockPath, token string) (*os.File, error) {
	ownerPath := windowsUpdateLockOwner(lockPath)
	pathPointer, err := syscall.UTF16PtrFromString(ownerPath)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pathPointer,
		syscall.GENERIC_READ,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), ownerPath)
	raw, readErr := io.ReadAll(io.LimitReader(file, 34))
	if readErr != nil || string(raw) != token+"\n" {
		_ = file.Close()
		if readErr != nil {
			return nil, readErr
		}
		return nil, errors.New("Windows update lock token does not match")
	}
	return file, nil
}

func validWindowsUpdateHelper(targetPath string, status windowsUpdateStatus) bool {
	if status.HelperPID <= 0 || status.HelperPath == "" || !windowsProcessRunning(status.HelperPID) ||
		!windowsUpdateLockMatches(targetPath+updateLockSuffix, status.Token) {
		return false
	}
	_, helperPath, err := validateWindowsUpdatePaths(targetPath, status.HelperPath)
	if err != nil {
		return false
	}
	info, err := os.Stat(helperPath)
	return err == nil && !info.IsDir()
}

func windowsProcessRunning(pid int) bool {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	return syscall.GetExitCodeProcess(handle, &exitCode) == nil && exitCode == windowsStillActive
}

func writeWindowsUpdateStatus(path string, status windowsUpdateStatus) error {
	status.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".ecctl-update-status-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
