//go:build !windows

package configfile

import "os"

func createSensitiveFile(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}
