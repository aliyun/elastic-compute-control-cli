//go:build darwin

package configfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func reservePrivateFile(file *os.File, size int64) error {
	store := &unix.Fstore_t{Flags: unix.F_ALLOCATEALL, Posmode: unix.F_PEOFPOSMODE, Length: size}
	if err := unix.FcntlFstore(file.Fd(), unix.F_PREALLOCATE, store); err != nil {
		if !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOTSUP) {
			return err
		}
		if err := writeZeroReservation(file, size); err != nil {
			return err
		}
	}
	return file.Truncate(size)
}
