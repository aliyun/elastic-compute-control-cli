//go:build darwin

package cli

import "testing"

func TestPlatformBrowserCommandUsesSystemOpen(t *testing.T) {
	command, err := platformBrowserCommand("https://signin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if command.Path != "/usr/bin/open" {
		t.Fatalf("browser command path = %q", command.Path)
	}
}
