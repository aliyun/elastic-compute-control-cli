//go:build windows

package cli

import (
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func platformBrowserCommand(rawURL string) (*exec.Cmd, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	return exec.Command(filepath.Join(dir, "rundll32.exe"), "url.dll,FileProtocolHandler", rawURL), nil
}
