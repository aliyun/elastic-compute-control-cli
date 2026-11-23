//go:build !windows && !darwin

package journalfile

import (
	"fmt"
	"os"
)

func prepareDurableTemp(file *os.File) error { return verifyPrivateDurableFile(file) }

func verifyPrivateDurableFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("durable recovery file is not private: %v", err)
	}
	return nil
}
