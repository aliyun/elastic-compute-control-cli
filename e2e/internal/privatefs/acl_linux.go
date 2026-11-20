//go:build linux

package privatefs

import (
	"bytes"
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var linuxPOSIXACLAttrs = [...]string{"system.posix_acl_access", "system.posix_acl_default"}

// ClearInheritedACL removes inherited POSIX ACLs from an opened private inode.
// Unknown ACL formats and xattr inspection errors remain unsafe.
func ClearInheritedACL(file *os.File) error {
	if file == nil {
		return os.ErrInvalid
	}
	fd := int(file.Fd())
	for _, attr := range linuxPOSIXACLAttrs {
		_, err := unix.Fgetxattr(fd, attr, nil)
		if errors.Is(err, unix.ENODATA) {
			continue
		}
		if err != nil {
			return os.NewSyscallError("fgetxattr", err)
		}
		if err := unix.Fremovexattr(fd, attr); err != nil {
			return os.NewSyscallError("fremovexattr", err)
		}
	}
	if !NoExtendedACLFile(file) {
		return os.ErrPermission
	}
	return nil
}

// NoExtendedACLFile checks POSIX ACLs directly and then rejects other listed
// ACL attributes on the same opened inode. Unreadable ACL state fails closed.
func NoExtendedACLFile(file *os.File) bool {
	if file == nil {
		return false
	}
	fd := int(file.Fd())
	for _, attr := range linuxPOSIXACLAttrs {
		_, err := unix.Fgetxattr(fd, attr, nil)
		if !errors.Is(err, unix.ENODATA) {
			return false
		}
	}
	size, err := unix.Flistxattr(fd, nil)
	if err != nil {
		return false
	}
	if size == 0 {
		return true
	}
	attributes := make([]byte, size)
	n, err := unix.Flistxattr(fd, attributes)
	if err != nil || n != size || attributes[n-1] != 0 {
		return false
	}
	for _, attr := range bytes.Split(attributes, []byte{0}) {
		if len(attr) > 0 && strings.Contains(strings.ToLower(string(attr)), "acl") {
			return false
		}
	}
	return true
}

// NoExtendedACL binds a path check to the same opened inode.
func NoExtendedACL(path string) bool {
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !NoExtendedACLFile(file) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && os.SameFile(opened, after)
}
