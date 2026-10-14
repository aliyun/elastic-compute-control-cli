package journalfile

import (
	"golang.org/x/sys/windows"
	"os"
)

func ensureRecoveryDirectory(dir string) error { return os.MkdirAll(dir, 0o755) }

func installDurable(tmp, path string, replace bool) error {
	source, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(source, target, flags)
}
