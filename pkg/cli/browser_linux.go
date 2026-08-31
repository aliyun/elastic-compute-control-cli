//go:build linux

package cli

import (
	"errors"
	"os"
	"os/exec"
)

func platformBrowserCommand(rawURL string) (*exec.Cmd, error) {
	for _, path := range []string{"/usr/bin/xdg-open", "/bin/xdg-open"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return exec.Command(path, rawURL), nil
		}
	}
	return nil, errors.New("trusted xdg-open executable is unavailable")
}
