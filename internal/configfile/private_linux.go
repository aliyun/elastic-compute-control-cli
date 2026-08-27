//go:build linux

package configfile

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var linuxPrivateACLNames = []string{"system.posix_acl_access", "system.posix_acl_default"}

func normalizePrivatePath(path string, directory bool) error {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return removeLinuxPrivateACLs(fd, directory)
}

func normalizePrivateOpenFile(file *os.File) error {
	return removeLinuxPrivateACLs(int(file.Fd()), false)
}

func removeLinuxPrivateACLs(fd int, directory bool) error {
	names := linuxPrivateACLNames[:1]
	if directory {
		names = linuxPrivateACLNames
	}
	for _, name := range names {
		if err := unix.Fremovexattr(fd, name); err != nil && !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
			return err
		}
	}
	return nil
}

func validatePrivatePathACL(path string, directory bool) error {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return validateLinuxPrivateACLs(fd)
}

func validatePrivateOpenFileACL(file *os.File) error {
	return validateLinuxPrivateACLs(int(file.Fd()))
}

func validateLinuxPrivateACLs(fd int) error {
	size, err := unix.Flistxattr(fd, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	raw := make([]byte, size)
	read, err := unix.Flistxattr(fd, raw)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(raw[:read]), "\x00") {
		if name == "system.posix_acl_access" || name == "system.posix_acl_default" {
			return errors.New("private file ACL is unsafe")
		}
	}
	return nil
}
