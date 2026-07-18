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

func TestCheckBinaryReleaseReady(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "brew tap aliyun/ecctl https://github.com/aliyun/elastic-compute-control-cli\nbrew install ecctl\n")

	if err := checkBinaryReleaseReady(root, binaryReleaseRepository); err != nil {
		t.Fatalf("checkBinaryReleaseReady: %v", err)
	}
}

func TestCheckBinaryReleaseReadyRejectsWrongRepository(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")

	err := checkBinaryReleaseReady(root, "aliyun/ecctl")
	if err == nil || !strings.Contains(err.Error(), binaryReleaseRepository) {
		t.Fatalf("checkBinaryReleaseReady error = %v, want repository identity failure", err)
	}
}

func TestCheckBinaryReleaseReadyRejectsModuleMigration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/aliyun/ecctl\n\ngo 1.25.0\n")

	err := checkBinaryReleaseReady(root, binaryReleaseRepository)
	if err == nil || !strings.Contains(err.Error(), "expects module") {
		t.Fatalf("checkBinaryReleaseReady error = %v, want module failure", err)
	}
}

func TestCheckBinaryReleaseReadyRejectsReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n\nreplace example.com/a => example.com/b v1.0.0\n")

	err := checkBinaryReleaseReady(root, binaryReleaseRepository)
	if err == nil || !strings.Contains(err.Error(), "replace directives") {
		t.Fatalf("checkBinaryReleaseReady error = %v, want replace failure", err)
	}
}

func TestCheckBinaryReleaseReadyAllowsPinnedMetadataModuleReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n\nreplace "+allowedBinaryReplace+"\n")

	if err := checkBinaryReleaseReady(root, binaryReleaseRepository); err != nil {
		t.Fatalf("checkBinaryReleaseReady: %v", err)
	}
}

func TestCheckBinaryReleaseReadyRejectsGoInstallAndStaleRepository(t *testing.T) {
	for _, content := range []string{
		"go install github.com/aliyun/ecctl/cmd/ecctl@latest\n",
		"Download from https://github.com/aliyun/ecctl/releases.\n",
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")
		writeFile(t, filepath.Join(root, "README.md"), content)

		err := checkBinaryReleaseReady(root, binaryReleaseRepository)
		if err == nil || !strings.Contains(err.Error(), "unfrozen Go module/repository") {
			t.Fatalf("checkBinaryReleaseReady(%q) error = %v, want misrepresentation failure", content, err)
		}
	}
}

func TestCheckBinaryReleaseReadyRejectsStaleRepositoryInJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "website", "i18n", "footer.json"), `{"github":"https://github.com/aliyun/ecctl"}`)

	err := checkBinaryReleaseReady(root, binaryReleaseRepository)
	if err == nil || !strings.Contains(err.Error(), "unfrozen Go module/repository") {
		t.Fatalf("checkBinaryReleaseReady error = %v, want JSON misrepresentation failure", err)
	}
}

func TestCheckHomebrewCaskVersionAllowsAdvance(t *testing.T) {
	cask := filepath.Join(t.TempDir(), "ecctl.rb")
	writeFile(t, cask, "cask \"ecctl\" do\n  version \"1.2.3\"\nend\n")

	if err := checkHomebrewCaskVersion("v1.3.0", cask, false); err != nil {
		t.Fatalf("checkHomebrewCaskVersion: %v", err)
	}
}

func TestCheckHomebrewCaskVersionAllowsFirstRelease(t *testing.T) {
	if err := checkHomebrewCaskVersion("v0.0.0", "", true); err != nil {
		t.Fatalf("checkHomebrewCaskVersion first release: %v", err)
	}
}

func TestCheckHomebrewCaskVersionRequiresExplicitCaskState(t *testing.T) {
	for _, test := range []struct {
		name         string
		cask         string
		firstRelease bool
	}{
		{name: "missing state"},
		{name: "ambiguous state", cask: "ecctl.rb", firstRelease: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := checkHomebrewCaskVersion("v1.0.0", test.cask, test.firstRelease); err == nil {
				t.Fatal("checkHomebrewCaskVersion succeeded, want explicit state error")
			}
		})
	}
}

func TestCheckHomebrewCaskVersionRejectsNonAdvance(t *testing.T) {
	for _, test := range []struct {
		name string
		tag  string
		want string
	}{
		{name: "downgrade", tag: "v1.2.2", want: "refusing to downgrade"},
		{name: "stable build metadata downgrade", tag: "v1.2.2+old-build", want: "refusing to downgrade"},
		{name: "equal", tag: "v1.2.3", want: "equal-precedence"},
		{name: "build metadata is equal precedence", tag: "v1.2.3+build.2", want: "equal-precedence"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cask := filepath.Join(t.TempDir(), "ecctl.rb")
			writeFile(t, cask, "cask \"ecctl\" do\n  version \"1.2.3\"\nend\n")
			err := checkHomebrewCaskVersion(test.tag, cask, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("checkHomebrewCaskVersion(%q) error = %v, want %q", test.tag, err, test.want)
			}
		})
	}
}

func TestCheckHomebrewCaskVersionAllowsPrereleaseWithoutReadingCask(t *testing.T) {
	if err := checkHomebrewCaskVersion("v1.3.0-rc.1", filepath.Join(t.TempDir(), "missing.rb"), false); err != nil {
		t.Fatalf("checkHomebrewCaskVersion prerelease: %v", err)
	}
}

func TestCheckHomebrewCaskVersionRejectsMalformedInput(t *testing.T) {
	for _, test := range []struct {
		name    string
		tag     string
		content string
	}{
		{name: "malformed tag", tag: "v1.02.3", content: "version \"1.2.3\"\n"},
		{name: "missing version", tag: "v1.2.4", content: "cask \"ecctl\" do\nend\n"},
		{name: "multiple versions", tag: "v1.2.4", content: "version \"1.2.2\"\nversion \"1.2.3\"\n"},
		{name: "malformed current version", tag: "v1.2.4", content: "version \"1.02.3\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cask := filepath.Join(t.TempDir(), "ecctl.rb")
			writeFile(t, cask, test.content)
			if err := checkHomebrewCaskVersion(test.tag, cask, false); err == nil {
				t.Fatalf("checkHomebrewCaskVersion(%q) succeeded, want error", test.tag)
			}
		})
	}
}

func TestCompareSemVersionPrereleaseOrdering(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 0; i+1 < len(ordered); i++ {
		left, err := parseSemVersion(ordered[i])
		if err != nil {
			t.Fatalf("parseSemVersion(%q): %v", ordered[i], err)
		}
		right, err := parseSemVersion(ordered[i+1])
		if err != nil {
			t.Fatalf("parseSemVersion(%q): %v", ordered[i+1], err)
		}
		if compareSemVersion(left, right) >= 0 {
			t.Fatalf("compareSemVersion(%q, %q) >= 0", ordered[i], ordered[i+1])
		}
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
