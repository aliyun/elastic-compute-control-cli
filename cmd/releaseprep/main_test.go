package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePublicModule(t *testing.T) {
	for _, module := range []string{"github.com/example/ecctl", "github.com/aliyun/ecctl"} {
		if err := validatePublicModule(module); err != nil {
			t.Fatalf("validatePublicModule(%q) = %v", module, err)
		}
	}

	for _, module := range []string{
		"",
		"ecctl",
		"gitlab.alibaba-inc.com/ai-storm/ecctl",
		"github.com/example/not-ecctl",
		"github.com/example/ecctl/v2",
		"github.com/bad_owner/ecctl",
		"github.com/-bad/ecctl",
		"github.com/bad-/ecctl",
	} {
		if err := validatePublicModule(module); err == nil {
			t.Fatalf("validatePublicModule(%q) succeeded, want error", module)
		}
	}
}

func TestCheckReleaseReadyRejectsUnfrozenModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")

	err := checkReleaseReady(root)
	if err == nil || !strings.Contains(err.Error(), "module path is not frozen") {
		t.Fatalf("checkReleaseReady error = %v, want module path failure", err)
	}
}

func TestCheckReleaseReadyRejectsReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n\nreplace example.com/a => example.com/b v1.0.0\n")

	err := checkReleaseReady(root)
	if err == nil || !strings.Contains(err.Error(), "replace directives") {
		t.Fatalf("checkReleaseReady error = %v, want replace failure", err)
	}
}

func TestCheckReleaseReadyRejectsInstallPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")

	err := checkReleaseReady(root)
	if err == nil || !strings.Contains(err.Error(), "public release placeholders") {
		t.Fatalf("checkReleaseReady error = %v, want placeholder failure", err)
	}
}

func TestCheckReleaseReadyAllowsReleasePrepOnlyPlaceholders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "Makefile"), "prepare-public-release:\n\tgo run ./cmd/releaseprep --write --module \"$(PUBLIC_MODULE)\"\n")
	writeFile(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "package main\n\nconst usage = \"github.com/<owner>/ecctl\"\n")
	writeFile(t, filepath.Join(root, "docs", "superpowers", "plans", "plan.md"), "Before publish, set PUBLIC_MODULE to github.com/<owner>/ecctl.\n")

	if err := checkReleaseReady(root); err != nil {
		t.Fatalf("checkReleaseReady: %v", err)
	}
}

func TestRewritePublicModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "package main\n\nimport \"ecctl/pkg/cli\"\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")
	writeFile(t, filepath.Join(root, ".goreleaser.yaml"), "ldflags:\n  - -X ecctl/pkg/cli.version={{ .Version }}\n")
	writeFile(t, filepath.Join(root, "Makefile"), "PUBLIC_MODULE is required, for example github.com/<owner>/ecctl\n")
	writeFile(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "package main\n\nconst usage = \"github.com/<owner>/ecctl\"\n")

	if err := rewritePublicModule(root, "github.com/example/ecctl"); err != nil {
		t.Fatalf("rewritePublicModule: %v", err)
	}
	assertFileContains(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl")
	assertFileContains(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "\"github.com/example/ecctl/pkg/cli\"")
	assertFileContains(t, filepath.Join(root, "README.md"), "go install github.com/example/ecctl/cmd/ecctl@latest")
	assertFileContains(t, filepath.Join(root, ".goreleaser.yaml"), "-X github.com/example/ecctl/pkg/cli.version={{ .Version }}")
	assertFileContains(t, filepath.Join(root, "Makefile"), "github.com/<owner>/ecctl")
	assertFileContains(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "github.com/<owner>/ecctl")
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(raw), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, raw)
	}
}
