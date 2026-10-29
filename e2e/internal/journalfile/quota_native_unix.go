//go:build !windows && !darwin

package journalfile

import (
	"os"
	"syscall"
)

// A newly created native config must be owned by this user and private.
// Existing configs retain the captured mode comparison instead.
func quotaNativeNewFilePrivate(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
