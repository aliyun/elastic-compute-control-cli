//go:build !windows

package configfile

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadBoundedRegularRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.ReadBoundedRegular(1024); err == nil {
		t.Fatal("FIFO configuration was accepted")
	}
}
