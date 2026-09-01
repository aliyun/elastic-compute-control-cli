//go:build !darwin && !linux && !windows

package cli

import (
	"errors"
	"os/exec"
)

func platformBrowserCommand(string) (*exec.Cmd, error) {
	return nil, errors.New("automatic browser launch is not supported on this platform")
}
