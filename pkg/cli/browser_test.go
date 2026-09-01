package cli

import (
	"strings"
	"testing"
)

func TestOpenBrowserURLRejectsUntrustedURL(t *testing.T) {
	for _, raw := range []string{"", "http://signin.example.com", "https://user@signin.example.com"} {
		if err := openBrowserURL(raw); err == nil {
			t.Fatalf("browser URL %q was accepted", raw)
		}
	}
}

func TestPrepareBrowserCommandScrubsCredentialEnvironment(t *testing.T) {
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "must-not-leak")
	t.Setenv("OSS_SESSION_TOKEN", "must-not-leak")
	t.Setenv("DISPLAY", ":99")
	command, err := prepareBrowserCommand("https://signin.example.com")
	if err != nil {
		t.Skipf("browser launcher unavailable: %v", err)
	}
	joined := strings.Join(command.Env, "\n")
	if strings.Contains(joined, "must-not-leak") {
		t.Fatalf("browser environment leaked credentials: %s", joined)
	}
	if !strings.Contains(joined, "DISPLAY=:99") {
		t.Fatalf("browser environment removed desktop state: %s", joined)
	}
}
