package journalfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ExecutableSHA256 hashes one absolute, regular executable without following
// a file symlink. Identity checks around the read reject replacement races.
func ExecutableSHA256(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("ecctl executable path is not absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path && (runtime.GOOS != "windows" || !strings.EqualFold(resolved, path)) {
		return "", fmt.Errorf("ecctl executable path is redirected: %v", err)
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || runtime.GOOS != "windows" && before.Mode()&0o111 == 0 {
		return "", fmt.Errorf("ecctl executable is unavailable or redirected: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", fmt.Errorf("ecctl executable changed before hashing: %v", err)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	after, err := os.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return "", fmt.Errorf("ecctl executable changed during hashing: %v", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
