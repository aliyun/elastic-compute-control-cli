//go:build linux

package configfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func reservePrivateFile(file *os.File, size int64) error {
	if err := unix.Fallocate(int(file.Fd()), 0, 0, size); err != nil {
		if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOTSUP) {
			return err
		}
		if err := writeZeroReservation(file, size); err != nil {
			return err
		}
	}
	return file.Truncate(size)
}
