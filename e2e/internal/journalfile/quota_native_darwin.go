//go:build darwin

package journalfile

import (
	"bytes"
	"os"
	"os/exec"
	"syscall"
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
	command := exec.Command("/bin/ls", "-lde", path)
	command.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	output, err := command.Output()
	trimmed := bytes.TrimSpace(output)
	return err == nil && len(trimmed) > 0 && len(bytes.Split(trimmed, []byte{'\n'})) == 1
}
