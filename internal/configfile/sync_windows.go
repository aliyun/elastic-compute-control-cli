//go:build windows

package configfile

import "golang.org/x/sys/windows"

var flushFileBuffers = windows.FlushFileBuffers

// Windows does not provide a portable directory fsync through os.File. The
// write-through tombstone moves used by private-file retirement provide their
// own namespace durability.
func syncDirectory(string) error { return nil }

func syncReplacementPlatform(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_WRITE_THROUGH,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return flushFileBuffers(handle)
}
