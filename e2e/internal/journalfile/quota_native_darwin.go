//go:build darwin

package journalfile

import (
	"os"
	"syscall"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/privatefs"
)

// macOS ACLs can grant access even when POSIX mode is 0600. Match the
// configuration writer's private-file check before accepting a new target.
func quotaNativeNewFilePrivate(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privateRecoveryDescriptor(file) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && os.SameFile(opened, after)
}

func privateRecoveryDescriptor(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && privatefs.NoExtendedACLFile(file)
}
