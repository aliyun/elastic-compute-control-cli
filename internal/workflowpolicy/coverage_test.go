package workflowpolicy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestCoverageGate(t *testing.T) {
	t.Parallel()
	requireAPISyncTools(t, "awk")
	script := filepath.Join("..", "..", ".github", "scripts", "check-coverage.awk")
	for _, tt := range []struct {
		name, profile string
		pass          bool
	}{
		{"exact threshold", "mode: set\nx.go:1.1,2.1 95 1\nx.go:3.1,4.1 5 0\n", true},
		{"above threshold", "mode: atomic\nx.go:1.1,2.1 96 2\nx.go:3.1,4.1 4 0\n", true},
		{"rounded threshold fails", "mode: count\nx.go:1.1,2.1 94999 10\nx.go:3.1,4.1 5001 0\n", false},
		{"all uncovered", "mode: set\nx.go:1.1,2.1 100 0\n", false},
		{"empty", "", false},
		{"header only", "mode: set\n", false},
		{"zero statements", "mode: set\nx.go:1.1,2.1 0 1\n", false},
		{"missing header", "x.go:1.1,2.1 100 1\n", false},
		{"invalid mode", "mode: invalid\nx.go:1.1,2.1 100 1\n", false},
		{"invalid block", "mode: set\nx.go:1.1,2.1 100 1\ninvalid\n", false},
		{"invalid counts", "mode: set\nx.go:1.1,2.1 -1 1\n", false},
		{"cross package hit", "mode: set\nx.go:1.1,2.1 100 0\nx.go:1.1,2.1 100 1\n", true},
		{"duplicate blocks do not inflate coverage", "mode: set\nx.go:1.1,2.1 90 1\nx.go:1.1,2.1 90 1\nx.go:3.1,4.1 10 0\n", false},
		{"inconsistent block", "mode: set\nx.go:1.1,2.1 100 1\nx.go:1.1,2.1 101 1\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("awk", "-f", script)
			cmd.Stdin = strings.NewReader(tt.profile)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tt.pass {
				t.Fatalf("pass = %v, want %v: %s", err == nil, tt.pass, out)
			}
		})
	}
}

func TestCIEnforcesCoverage(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]apiSyncWorkflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["test"].Steps {
		if step.Run == "make coverage" && step.If == "" {
			return
		}
	}
	t.Fatal("CI must unconditionally run make coverage")
}

func TestCoverageMakeTargetPropagatesFailures(t *testing.T) {
	t.Parallel()
	requireAPISyncTools(t, "make", "awk")
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", "check-coverage.awk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, listExit, testExit, profile string
		pass                              bool
	}{
		{"threshold passes", "0", "0", "mode: set\nx.go:1.1,2.1 95 1\nx.go:3.1,4.1 5 0\n", true},
		{"coverage fails", "0", "0", "mode: set\nx.go:1.1,2.1 94 1\nx.go:3.1,4.1 6 0\n", false},
		{"tests fail despite full coverage", "0", "1", "mode: set\nx.go:1.1,2.1 100 1\n", false},
		{"listing fails despite stale profile", "1", "0", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, path := range []string{"bin", "reports", ".github/scripts"} {
				if err := os.MkdirAll(filepath.Join(dir, path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for path, raw := range map[string][]byte{"Makefile": makefile, ".github/scripts/check-coverage.awk": script, "reports/coverage.out": []byte("mode: set\nx.go:1.1,2.1 100 1\n")} {
				if err := os.WriteFile(filepath.Join(dir, path), raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stub := `#!/bin/sh
case "$1" in
list) printf '%s\n' example/pkg example/cmd/specgen; exit "$LIST_EXIT" ;;
test) printf '%s' "$PROFILE" > reports/coverage.out; printf '%s\n' "$@" > test-args; exit "$TEST_EXIT" ;;
*) exit 99 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "bin", "go"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("make", "--no-print-directory", "coverage")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "MAKEFLAGS=", "PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "LIST_EXIT="+tt.listExit, "TEST_EXIT="+tt.testExit, "PROFILE="+tt.profile)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tt.pass {
				t.Fatalf("pass=%t, want %t: %s", err == nil, tt.pass, out)
			}
			if tt.listExit != "0" {
				if _, err := os.Stat(filepath.Join(dir, "reports", "coverage.out")); !os.IsNotExist(err) {
					t.Fatalf("stale profile survived listing failure: %v", err)
				}
			} else {
				args, err := os.ReadFile(filepath.Join(dir, "test-args"))
				if err != nil || !strings.Contains(string(args), "-coverpkg=example/pkg\n") || strings.Contains(string(args), "specgen") {
					t.Fatalf("wrong test scope: %q, %v", args, err)
				}
			}
		})
	}
}
