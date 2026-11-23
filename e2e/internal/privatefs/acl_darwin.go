//go:build darwin && cgo

package privatefs

/*
#include <errno.h>
#include <sys/acl.h>

static int ecctl_no_extended_acl(int fd) {
	errno = 0;
	acl_t acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
	if (acl != NULL) {
		acl_free(acl);
		return 0;
	}
	return errno == ENOENT ? 1 : -1;
}

static int ecctl_clear_extended_acl(int fd) {
	acl_t empty = acl_init(0);
	if (empty == NULL) return -errno;
	int result = acl_set_fd_np(fd, empty, ACL_TYPE_EXTENDED);
	int saved_errno = errno;
	acl_free(empty);
	return result == 0 ? 0 : -saved_errno;
}
*/
import "C"

import (
	"os"
	"syscall"
)

// ClearInheritedACL removes grants from the inode already opened by the caller.
func ClearInheritedACL(file *os.File) error {
	if file == nil {
		return os.ErrInvalid
	}
	if result := C.ecctl_clear_extended_acl(C.int(file.Fd())); result != 0 {
		return os.NewSyscallError("acl_set_fd_np", syscall.Errno(-result))
	}
	return nil
}

// NoExtendedACLFile treats retrieval failure and any present ACL as unsafe.
func NoExtendedACLFile(file *os.File) bool {
	return file != nil && C.ecctl_no_extended_acl(C.int(file.Fd())) == 1
}

// NoExtendedACL binds the pathname check to the same opened inode.
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
