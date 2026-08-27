//go:build !windows

package configfile

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
