//go:build windows

package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// VerifiedExecutableCopy pins the original Windows file handle without
// sharing write or delete access. The handle remains open through process
// exit, so CreateProcess cannot reopen a replacement at the same pathname.
func VerifiedExecutableCopy(path, expectedSHA256 string) (string, func(), error) {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".exe") || len(expectedSHA256) != 64 || strings.ToLower(expectedSHA256) != expectedSHA256 {
		return "", nil, fmt.Errorf("verified executable identity is invalid")
	}
	launch, err := osexec.LookPath(path)
	if err != nil || !strings.EqualFold(filepath.Clean(launch), filepath.Clean(path)) {
		return "", nil, fmt.Errorf("verified executable would resolve to another launch path: %v", err)
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return "", nil, fmt.Errorf("verified executable digest is invalid: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.EqualFold(resolved, path) {
		return "", nil, fmt.Errorf("verified executable path is redirected: %v", err)
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", nil, fmt.Errorf("verified executable is unavailable: %v", err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	release := func() { _ = file.Close() }
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		release()
		return "", nil, fmt.Errorf("verified executable changed before hashing: %v", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		release()
		return "", nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		release()
		return "", nil, fmt.Errorf("verified executable changed before launch: %v", err)
	}
	return path, release, nil
}
