package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowUsesInfraGuardWebhookAction(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	workflow := string(raw)
	if !strings.Contains(workflow, `action: "edited"`) {
		t.Fatal(`release webhook action is not "edited" as required by the InfraGuard-compatible FC handler`)
	}
	if strings.Contains(workflow, `action: "published"`) {
		t.Fatal(`release webhook still contains unsupported action "published"`)
	}
	if !strings.Contains(workflow, `delivery_id=$(uuidgen | tr '[:upper:]' '[:lower:]')`) {
		t.Fatal("release webhook does not generate a fresh delivery ID for each retry")
	}
	if strings.Contains(workflow, "uuid.uuid5") {
		t.Fatal("release webhook still reuses a release-derived FC task ID")
	}
	for _, required := range []string{
		`X-Hub-Signature-256: sha256=${signature}`,
		`X-Fc-Invocation-Type: Async`,
		`X-Fc-Async-Task-Id: ${delivery_id}`,
		`case "${http_code}" in`,
		`409)`,
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("release webhook is missing InfraGuard contract fragment %q", required)
		}
	}
}

func TestValidatePublicModule(t *testing.T) {
	for _, module := range []string{
		"github.com/example/ecctl",
		"github.com/aliyun/ecctl",
		"github.com/aliyun/elastic-compute-control-cli",
	} {
		if err := validatePublicModule(module); err != nil {
			t.Fatalf("validatePublicModule(%q) = %v", module, err)
		}
	}

	for _, module := range []string{
		"",
		"ecctl",
		"gitlab.alibaba-inc.com/ai-storm/ecctl",
		"github.com/example/ecctl/v2",
		"github.com/bad_owner/ecctl",
		"github.com/-bad/ecctl",
		"github.com/bad-/ecctl",
		"github.com/example/bad/repo",
		"github.com/example/<repo>",
	} {
		if err := validatePublicModule(module); err == nil {
			t.Fatalf("validatePublicModule(%q) succeeded, want error", module)
		}
	}
}

func TestCheckReleaseReadyRejectsUnfrozenModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module ecctl\n\ngo 1.25.0\n")

	err := checkReleaseReady(root, "example/ecctl")
	if err == nil || !strings.Contains(err.Error(), "module path is not frozen") {
		t.Fatalf("checkReleaseReady error = %v, want module path failure", err)
	}
}

func TestCheckReleaseReadyRejectsReplace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n\nreplace example.com/a => example.com/b v1.0.0\n")

	err := checkReleaseReady(root, "example/ecctl")
	if err == nil || !strings.Contains(err.Error(), "replace directives") {
		t.Fatalf("checkReleaseReady error = %v, want replace failure", err)
	}
}

func TestCheckReleaseReadyRejectsInstallPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/example/ecctl\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")

	err := checkReleaseReady(root, "example/ecctl")
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

	if err := checkReleaseReady(root, "example/ecctl"); err != nil {
		t.Fatalf("checkReleaseReady: %v", err)
	}
}

func TestCheckReleaseReadyRejectsRepositoryMismatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/attacker/ecctl\n\ngo 1.25.0\n")

	err := checkReleaseReady(root, "aliyun/elastic-compute-control-cli")
	if err == nil || !strings.Contains(err.Error(), "must match repository") {
		t.Fatalf("checkReleaseReady error = %v, want repository mismatch", err)
	}
}

func TestCheckReleaseReadyRejectsMismatchedGoInstallModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/aliyun/elastic-compute-control-cli\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/attacker/ecctl/cmd/ecctl@latest\n")

	err := checkReleaseReady(root, "aliyun/elastic-compute-control-cli")
	if err == nil || !strings.Contains(err.Error(), "go install commands must use public module") {
		t.Fatalf("checkReleaseReady error = %v, want go install module mismatch", err)
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

func TestCheckReleaseVersionAllowsCanonicalAdvance(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.txt")
	previous := filepath.Join(root, "previous.txt")
	writeFile(t, current, "1.3.0\n")
	writeFile(t, previous, "1.2.3\n")

	got, err := checkReleaseVersion(current, previous, "", "v1.3.0")
	if err != nil {
		t.Fatalf("checkReleaseVersion: %v", err)
	}
	if got != "1.3.0" {
		t.Fatalf("checkReleaseVersion = %q, want 1.3.0", got)
	}
}

func TestCheckReleaseVersionAllowsPrereleaseAdvance(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.txt")
	previous := filepath.Join(root, "previous.txt")
	writeFile(t, current, "1.3.0-rc.2\n")
	writeFile(t, previous, "1.3.0-rc.1\n")

	if _, err := checkReleaseVersion(current, previous, "", "v1.3.0-rc.2"); err != nil {
		t.Fatalf("checkReleaseVersion: %v", err)
	}
}

func TestCheckReleaseVersionRejectsInvalidFiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty"},
		{name: "missing newline", content: "1.2.3"},
		{name: "multiple lines", content: "1.2.3\n1.2.4\n"},
		{name: "carriage return", content: "1.2.3\r\n"},
		{name: "surrounding whitespace", content: " 1.2.3\n"},
		{name: "tag prefix", content: "v1.2.3\n"},
		{name: "build metadata", content: "1.2.3+build.1\n"},
		{name: "leading zero", content: "1.02.3\n"},
		{name: "byte order mark", content: "\xef\xbb\xbf1.2.3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "version.txt")
			writeFile(t, path, test.content)
			if _, err := checkReleaseVersion(path, "", "", ""); err == nil {
				t.Fatal("checkReleaseVersion succeeded, want error")
			}
		})
	}
}

func TestCheckReleaseVersionRejectsTagMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.txt")
	writeFile(t, path, "1.2.3\n")

	if _, err := checkReleaseVersion(path, "", "", "v1.2.4"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("checkReleaseVersion error = %v, want tag mismatch", err)
	}
}

func TestCheckReleaseVersionRejectsNonAdvance(t *testing.T) {
	for _, test := range []struct {
		name     string
		current  string
		previous string
	}{
		{name: "equal", current: "1.2.3\n", previous: "1.2.3\n"},
		{name: "downgrade", current: "1.2.2\n", previous: "1.2.3\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := filepath.Join(root, "current.txt")
			previous := filepath.Join(root, "previous.txt")
			writeFile(t, current, test.current)
			writeFile(t, previous, test.previous)
			if _, err := checkReleaseVersion(current, previous, "", ""); err == nil || !strings.Contains(err.Error(), "must be greater") {
				t.Fatalf("checkReleaseVersion error = %v, want non-advance", err)
			}
		})
	}
}

func TestCheckReleaseVersionRejectsPublishedTagRegression(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "version.txt")
	tags := filepath.Join(root, "tags.txt")
	writeFile(t, current, "1.3.0-rc.1\n")
	writeFile(t, tags, "v1.2.0\nv1.3.0-rc.2\n")

	if _, err := checkReleaseVersion(current, "", tags, "v1.3.0-rc.1"); err == nil || !strings.Contains(err.Error(), "existing release tag v1.3.0-rc.2") {
		t.Fatalf("checkReleaseVersion error = %v, want published tag regression", err)
	}
}

func TestCheckReleaseVersionAllowsRecoveryOfCurrentTag(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "version.txt")
	tags := filepath.Join(root, "tags.txt")
	writeFile(t, current, "1.3.0\n")
	writeFile(t, tags, "v1.2.0\nv1.3.0\n")

	if _, err := checkReleaseVersion(current, "", tags, "v1.3.0"); err != nil {
		t.Fatalf("checkReleaseVersion recovery: %v", err)
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
	writeFile(t, filepath.Join(root, "e2e", "go.mod"), "module ecctl/e2e\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "package main\n\nimport \"ecctl/pkg/cli\"\n")
	writeFile(t, filepath.Join(root, "README.md"), "go install github.com/<owner>/ecctl/cmd/ecctl@latest\n")
	writeFile(t, filepath.Join(root, ".goreleaser.yaml"), "ldflags:\n  - -X ecctl/pkg/cli.version={{ .Version }}\n")
	writeFile(t, filepath.Join(root, "Makefile"), "PUBLIC_MODULE is required, for example github.com/<owner>/ecctl\n")
	writeFile(t, filepath.Join(root, "cmd", "releaseprep", "main.go"), "package main\n\nconst usage = \"github.com/<owner>/ecctl\"\n")

	if err := rewritePublicModule(root, "github.com/example/elastic-compute-control-cli"); err != nil {
		t.Fatalf("rewritePublicModule: %v", err)
	}
	if err := rewritePublicModule(root, "github.com/example/elastic-compute-control-cli"); err != nil {
		t.Fatalf("rewritePublicModule second run: %v", err)
	}
	if err := rewritePublicModule(root, "github.com/another/ecctl-cli"); err != nil {
		t.Fatalf("rewritePublicModule retarget: %v", err)
	}
	assertFileContains(t, filepath.Join(root, "go.mod"), "module github.com/another/ecctl-cli")
	assertFileContains(t, filepath.Join(root, "e2e", "go.mod"), "module github.com/another/ecctl-cli/e2e")
	assertFileContains(t, filepath.Join(root, "cmd", "ecctl", "main.go"), "\"github.com/another/ecctl-cli/pkg/cli\"")
	assertFileContains(t, filepath.Join(root, "README.md"), "go install github.com/another/ecctl-cli/cmd/ecctl@latest")
	assertFileContains(t, filepath.Join(root, ".goreleaser.yaml"), "-X github.com/another/ecctl-cli/pkg/cli.version={{ .Version }}")
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
