package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionRejectsUnsafeAndAliasedTargetsBeforeChangingFiles(t *testing.T) {
	for _, fault := range []string{"rc directory", "script directory", "nested markers", "aliased targets"} {
		t.Run(fault, func(t *testing.T) {
			home := completionTestHome(t)
			rc, script := filepath.Join(home, ".zshrc"), filepath.Join(home, ".zfunc", "_ecctl")
			if err := os.Mkdir(filepath.Dir(script), 0o700); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "rc directory":
				if err := os.Mkdir(rc, 0o700); err != nil {
					t.Fatal(err)
				}
			case "script directory":
				if err := os.Mkdir(script, 0o700); err != nil {
					t.Fatal(err)
				}
			case "nested markers":
				if err := os.WriteFile(rc, []byte(zshCompletionStart+"\n"+zshCompletionStart+"\n"+zshCompletionEnd+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "aliased targets":
				if err := os.WriteFile(rc, []byte("original\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(rc, script); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
			}
			if _, err := installZshCompletion(context.Background(), &cobra.Command{Use: "ecctl"}, false); err == nil {
				t.Fatalf("unsafe completion %s accepted", fault)
			}
			if fault == "aliased targets" {
				if raw, err := os.ReadFile(rc); err != nil || string(raw) != "original\n" {
					t.Fatal("aliased completion target changed")
				}
			}
		})
	}
}

func TestCompletionPropagatesMissingHomeAndInvalidResolvedTargets(t *testing.T) {
	completionTestHome(t)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := installZshCompletion(context.Background(), &cobra.Command{Use: "ecctl"}, false); err == nil {
		t.Fatal("missing home accepted")
	}
}
