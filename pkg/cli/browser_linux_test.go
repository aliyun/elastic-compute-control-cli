//go:build linux

package cli

import (
	"strings"
	"testing"
)

func TestPlatformBrowserCommandUsesTrustedDirectory(t *testing.T) {
	command, err := platformBrowserCommand("https://signin.example.com")
	if err != nil {
		t.Skipf("xdg-open unavailable: %v", err)
	}
	if !strings.HasPrefix(command.Path, "/usr/bin/") && !strings.HasPrefix(command.Path, "/bin/") {
		t.Fatalf("browser command path = %q", command.Path)
	}
}
