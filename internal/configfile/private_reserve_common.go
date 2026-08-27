package configfile

import (
	"errors"
	"os"
)

func writeZeroReservation(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return errors.New("private reservation is unavailable")
	}
	zeroes := make([]byte, 64<<10)
	remaining := size
	for remaining > 0 {
		chunk := int64(len(zeroes))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := file.Write(zeroes[:int(chunk)]); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}
