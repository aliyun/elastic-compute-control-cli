//go:build windows

package configfile

import "os"

func reservePrivateFile(file *os.File, size int64) error {
	if err := writeZeroReservation(file, size); err != nil {
		return err
	}
	return file.Truncate(size)
}
