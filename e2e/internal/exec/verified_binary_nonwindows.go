//go:build !windows

package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/privatefs"
)

// verifiedTempRoot rejects a TMPDIR whose directory names can be replaced by
// another local principal. A sticky directory is safe only when its owner is
// the current user or root; the private child remains owned by this process.
func verifiedTempRoot() (string, error) {
	root, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil || !filepath.IsAbs(root) {
		return "", fmt.Errorf("verified executable temporary root is unavailable: %v", err)
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("verified executable temporary ancestor is unavailable: %v", err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 && owner.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 || !sameFileAndNoACL(dir, info) {
			return "", fmt.Errorf("verified executable temporary ancestor is not owner controlled")
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return root, nil
}

// An explicit root must already be private and may not redirect through a
// symlink. Invalid configuration never falls back to the default temp root.
func verifiedStagingRoot() (string, error) {
	root, configured := os.LookupEnv("ECCTL_E2E_VERIFIED_EXEC_DIR")
	if !configured {
		return verifiedTempRoot()
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("verified executable staging root must be a canonical absolute directory")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("verified executable staging root is unavailable: %w", err)
	}
	if resolved != root {
		return "", fmt.Errorf("verified executable staging root is redirected")
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("verified executable staging ancestor is unavailable: %v", err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !sameFileAndNoACL(dir, info) {
			return "", fmt.Errorf("verified executable staging ancestor lacks stable custody")
		}
		if dir == root {
			if owner.Uid != uint32(os.Getuid()) || info.Mode().Perm() != 0o700 {
				return "", fmt.Errorf("verified executable staging root is not current-user private")
			}
		} else if owner.Uid != 0 && owner.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return "", fmt.Errorf("verified executable staging ancestor is not owner controlled")
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return root, nil
}

// VerifiedExecutableCopy binds an invocation to bytes read from one regular
// source file. The private copy is launched instead of reopening the original
// pathname after verification. The caller must retain it through process exit.
func VerifiedExecutableCopy(path, expectedSHA256 string) (string, func(), error) {
	if !filepath.IsAbs(path) || len(expectedSHA256) != 64 || strings.ToLower(expectedSHA256) != expectedSHA256 {
		return "", nil, fmt.Errorf("verified executable identity is invalid")
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return "", nil, fmt.Errorf("verified executable digest is invalid: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", nil, fmt.Errorf("verified executable path is redirected: %v", err)
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&0o111 == 0 {
		return "", nil, fmt.Errorf("verified executable is unavailable: %v", err)
	}
	source, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", nil, fmt.Errorf("verified executable changed before copying: %v", err)
	}
	stagingRoot, err := verifiedStagingRoot()
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(stagingRoot, "ecctl-verified-")
	if err != nil {
		return "", nil, err
	}
	release := func() { _ = os.RemoveAll(dir) }
	dirFile, err := os.Open(dir)
	if err != nil {
		release()
		return "", nil, err
	}
	aclErr := privatefs.ClearInheritedACL(dirFile)
	closeErr := dirFile.Close()
	if aclErr != nil || closeErr != nil {
		release()
		return "", nil, fmt.Errorf("verified executable directory ACL is uncertain: %v %v", aclErr, closeErr)
	}
	owned, err := os.Lstat(dir)
	owner, ok := ownedOwner(owned)
	if err != nil || !ok || !owned.IsDir() || owned.Mode().Perm() != 0o700 || owner != uint32(os.Getuid()) || !sameFileAndNoACL(dir, owned) {
		release()
		return "", nil, fmt.Errorf("verified executable private directory lacks current-user custody: %v", err)
	}
	copyPath := filepath.Join(dir, filepath.Base(path))
	copyFile, err := os.OpenFile(copyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		release()
		return "", nil, err
	}
	if err := privatefs.ClearInheritedACL(copyFile); err != nil {
		copyFile.Close()
		release()
		return "", nil, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(copyFile, hash), source)
	syncErr := copyFile.Sync()
	closeErr = copyFile.Close()
	after, statErr := os.Lstat(path)
	if copyErr != nil || syncErr != nil || closeErr != nil || statErr != nil || !os.SameFile(before, after) || hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		release()
		return "", nil, fmt.Errorf("verified executable bytes changed while copying: %v %v %v %v", copyErr, syncErr, closeErr, statErr)
	}
	if err := os.Chmod(copyPath, 0o500); err != nil {
		release()
		return "", nil, err
	}
	copyInfo, err := os.Lstat(copyPath)
	copyOwner, ok := ownedOwner(copyInfo)
	if err != nil || !ok || !copyInfo.Mode().IsRegular() || copyInfo.Mode().Perm() != 0o500 || copyOwner != uint32(os.Getuid()) || !sameFileAndNoACL(copyPath, copyInfo) {
		release()
		return "", nil, fmt.Errorf("verified executable copy lost current-user custody: %v", err)
	}
	installed, err := os.Open(copyPath)
	if err != nil {
		release()
		return "", nil, err
	}
	installedInfo, statErr := installed.Stat()
	installedACL := privatefs.NoExtendedACLFile(installed)
	installedHash := sha256.New()
	_, hashErr := io.Copy(installedHash, installed)
	closeErr = installed.Close()
	lastInfo, lastErr := os.Lstat(copyPath)
	if statErr != nil || hashErr != nil || closeErr != nil || lastErr != nil || !os.SameFile(copyInfo, installedInfo) || !os.SameFile(copyInfo, lastInfo) || hex.EncodeToString(installedHash.Sum(nil)) != expectedSHA256 || !installedACL {
		release()
		return "", nil, fmt.Errorf("verified executable installed copy changed: %v %v %v %v", statErr, hashErr, closeErr, lastErr)
	}
	return copyPath, release, nil
}

func sameFileAndNoACL(path string, info os.FileInfo) bool {
	if info == nil {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privatefs.NoExtendedACLFile(file) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && os.SameFile(opened, after)
}

func ownedOwner(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return owner.Uid, true
}
