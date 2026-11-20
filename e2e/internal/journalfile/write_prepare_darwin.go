//go:build darwin

package journalfile

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/privatefs"
)

func prepareDurableTemp(file *os.File) error {
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	// Remove any inherited ACL through the opened file before recovery data is
	// written. The parent may have been renamed after its root was opened.
	if err := privatefs.ClearInheritedACL(file); err != nil {
		return fmt.Errorf("remove inherited recovery-file ACL: %w", err)
	}
	return verifyPrivateDurableFile(file)
}

func verifyPrivateDurableFile(file *os.File) error {
	path, err := openedDarwinPath(file)
	if err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		named, err := os.Lstat(path)
		if err != nil || !os.SameFile(opened, named) || !privateRecoveryDescriptor(file) {
			return fmt.Errorf("durable recovery file is not private: %v", err)
		}
	}
	return nil
}

func openedDarwinPath(file *os.File) (string, error) {
	var path [4096]byte
	const fGetPath = 0x32
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), fGetPath, uintptr(unsafe.Pointer(&path[0])))
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(path[:], 0)
	if end <= 0 {
		return "", fmt.Errorf("durable recovery file path is unavailable")
	}
	return string(path[:end]), nil
}
