package cli

import (
	"errors"
	"net/url"
	"os"
	"os/exec"

	"github.com/aliyun/elastic-compute-control-cli/internal/credentialenv"
)

func openBrowserURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("browser URL must be an absolute HTTPS URL without user information")
	}
	command, err := prepareBrowserCommand(rawURL)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func prepareBrowserCommand(rawURL string) (*exec.Cmd, error) {
	command, err := platformBrowserCommand(rawURL)
	if err != nil {
		return nil, err
	}
	command.Env = credentialenv.WithoutSensitive(os.Environ())
	return command, nil
}
