package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSemVerRejectsMalformedIdentifiersAndOrdersLargeNumbers(t *testing.T) {
	for _, raw := range []string{"", "1.2.3+", "1.2.3+bad!", "1.2.3-a!", "1.2.3-01", "1.2.3+one+two"} {
		if _, err := parseSemVersion(raw); err == nil {
			t.Fatalf("invalid release version accepted: %q", raw)
		}
	}
	for _, test := range []struct {
		left, right string
		want        int
	}{{"1.2.3-alpha.1", "1.2.3-alpha", 1}, {"1.2.3-z", "1.2.3-a", 1}, {"1.2.3-alpha.1", "1.2.3-alpha.1", 0}, {"100000000000000000000.0.0", "99999999999999999999.0.0", 1}} {
		left, err := parseSemVersion(test.left)
		if err != nil {
			t.Fatal(err)
		}
		right, err := parseSemVersion(test.right)
		if err != nil {
			t.Fatal(err)
		}
		if got := compareSemVersion(left, right); got != test.want {
			t.Fatalf("compare %s,%s=%d", test.left, test.right, got)
		}
	}
}

func TestReleaseVersionValidationRejectsUnreadableHistoryAndInvalidTags(t *testing.T) {
	root := t.TempDir()
	version := filepath.Join(root, "version.txt")
	writeFile(t, version, "1.2.3\n")
	if _, err := checkReleaseVersion(version, "", "", "", true, false); err == nil {
		t.Fatal("duplicate allow-existing option accepted")
	}
	if _, err := checkReleaseVersion(version, filepath.Join(root, "missing"), "", ""); err == nil {
		t.Fatal("missing previous release accepted")
	}
	if _, err := checkReleaseVersion(version, "", filepath.Join(root, "missing"), ""); err == nil {
		t.Fatal("missing release tags accepted")
	}
	tags := filepath.Join(root, "tags")
	for _, raw := range []string{"1.0.0\n", "vinvalid\n", strings.Repeat("x", 70<<10)} {
		writeFile(t, tags, raw)
		if _, err := checkReleaseVersion(version, "", tags, ""); err == nil {
			t.Fatalf("invalid tag history accepted: %.40s", raw)
		}
	}
	if err := checkTelemetryConfig(nil); err == nil {
		t.Fatal("missing telemetry source accepted")
	}
	if err := validatePublicModule("github.com/owner/repo!"); err == nil {
		t.Fatal("invalid GitHub repository accepted")
	}
}

func TestReleaseChecksumsRejectDuplicatesAndMissingArchitectures(t *testing.T) {
	root := t.TempDir()
	checksums := filepath.Join(root, "checksums")
	checksum := strings.Repeat("a", 64) + "  ecctl_1.2.3_darwin_arm64.tar.gz\n"
	for _, raw := range []string{checksum + checksum, strings.Repeat("x", 70<<10)} {
		writeFile(t, checksums, raw)
		if _, err := readReleaseChecksums(checksums); err == nil {
			t.Fatal("invalid release checksums accepted")
		}
	}
	writeFile(t, checksums, "\n"+checksum)
	if parsed, err := readReleaseChecksums(checksums); err != nil || len(parsed) != 1 {
		t.Fatalf("blank checksum line=%v, %v", parsed, err)
	}
	cask := filepath.Join(root, "cask.rb")
	writeFile(t, cask, "cask")
	if _, _, err := readHomebrewCask(cask, checksums, "1.2.3"); err == nil {
		t.Fatal("missing Intel checksum accepted")
	}
	if _, _, err := readHomebrewCask(cask, checksums, "invalid"); err == nil {
		t.Fatal("invalid Cask version accepted")
	}
	if _, _, err := readHomebrewCask(filepath.Join(root, "missing"), checksums, "1.2.3"); err == nil {
		t.Fatal("missing Cask accepted")
	}
	if _, err := readReleaseChecksums(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing checksums accepted")
	}
}

func TestPublicModuleRewriteRequiresRootModuleAndSkipsDependencies(t *testing.T) {
	root := t.TempDir()
	if err := rewritePublicModule(root, "github.com/example/public"); err == nil {
		t.Fatal("missing root module rewritten")
	}
	writeFile(t, filepath.Join(root, "go.mod"), "go 1.25.0\n")
	if err := rewritePublicModule(root, "github.com/example/public"); err == nil {
		t.Fatal("module declaration invented")
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module internal.example/private\n\ngo 1.25.0\n")
	dependency := filepath.Join(root, "node_modules", "dependency.go")
	if err := os.MkdirAll(filepath.Dir(dependency), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dependency, "package dependency\n// internal.example/private\n")
	if err := rewritePublicModule(root, "github.com/example/public"); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, dependency, "internal.example/private")
	assertFileContains(t, filepath.Join(root, "go.mod"), "github.com/example/public")
}
