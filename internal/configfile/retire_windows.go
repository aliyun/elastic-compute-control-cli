//go:build windows

package configfile

import "golang.org/x/sys/windows"

func retireFile(source, tombstone string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	tombstonePtr, err := windows.UTF16PtrFromString(tombstone)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePtr,
		tombstonePtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
