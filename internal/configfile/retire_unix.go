//go:build !windows

package configfile

import "os"

func retireFile(source, tombstone string) error {
	return os.Rename(source, tombstone)
}
