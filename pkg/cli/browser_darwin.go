//go:build darwin

package cli

import "os/exec"

func platformBrowserCommand(rawURL string) (*exec.Cmd, error) {
	return exec.Command("/usr/bin/open", rawURL), nil
}
