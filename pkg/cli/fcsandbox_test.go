package cli

import (
	"strings"
	"testing"
)

func TestFCSandboxTeamIsPublic(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"fcsandbox", "team", "--help"}, {"schema", "--list", "fcsandbox"}, {"examples", "fcsandbox.team"}} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code != 0 || !strings.Contains(stdout, "team") {
			t.Fatalf("public command %v: exit %d %s %s", args, code, stdout, stderr)
		}
	}
}
