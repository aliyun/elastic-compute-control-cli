package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/releaseartifact"
)

func runMainCommand(t *testing.T, args ...string) string {
	t.Helper()
	oldArgs, oldFlags, oldOut := os.Args, flag.CommandLine, os.Stdout
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	os.Args = append([]string{"releaseprep"}, args...)
	flag.CommandLine = flag.NewFlagSet("releaseprep", flag.ContinueOnError)
	os.Stdout = output
	defer func() { os.Args, flag.CommandLine, os.Stdout = oldArgs, oldFlags, oldOut }()
	main()
	raw, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMainDispatchesReleaseChecks(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, "version.txt", "1.2.3\n")
	if out := runMainCommand(t, "--check-version-file"); out != "1.2.3\n" {
		t.Fatalf("version output = %q", out)
	}
	for _, tt := range []struct{ tag, want string }{{"v0.1.0", "false\n"}, {"v1.2.3", "true\n"}} {
		if out := runMainCommand(t, "--update-v2-required", "--release-tag", tt.tag); out != tt.want {
			t.Fatalf("signed update output = %q", out)
		}
	}
	t.Setenv("ECCTL_TELEMETRY_ENDPOINT_B64", base64.StdEncoding.EncodeToString([]byte("https://tracing-cn-hangzhou.arms.aliyuncs.com/v1/traces")))
	t.Setenv("ECCTL_TELEMETRY_HEADERS_B64", base64.StdEncoding.EncodeToString([]byte(`{}`)))
	runMainCommand(t, "--check-telemetry-config")
	runMainCommand(t, "--check-homebrew-version", "--release-tag", "v1.2.3", "--first-homebrew-release")
	writeFile(t, "go.mod", "module ecctl\n\ngo 1.25.0\n")
	runMainCommand(t, "--write", "--module", "github.com/example/ecctl")
	assertFileContains(t, "go.mod", "module github.com/example/ecctl")
	runMainCommand(t, "--check", "--repository", "example/ecctl")
	intel, arm := strings.Repeat("a", 64), strings.Repeat("b", 64)
	writeFile(t, "cask.rb", validReleaseCask("1.2.3", intel, arm))
	writeFile(t, "checksums.txt", intel+"  ecctl_1.2.3_darwin_amd64.tar.gz\n"+arm+"  ecctl_1.2.3_darwin_arm64.tar.gz\n")
	args := []string{"--cask", "cask.rb", "--checksums-file", "checksums.txt", "--release-tag", "v1.2.3"}
	runMainCommand(t, append([]string{"--verify-homebrew-cask"}, args...)...)
	converted := runMainCommand(t, append([]string{"--convert-homebrew-cask", "--cask-format", "v2"}, args...)...)
	if err := releaseartifact.ValidateCaskFormat([]byte(converted), releaseartifact.CaskExpectation{Version: "1.2.3", IntelSHA256: intel, ArmSHA256: arm}, releaseartifact.CaskFormatV2); err != nil {
		t.Fatalf("converted Cask: %v", err)
	}
}

func TestReadBoundedReleaseMetadata(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "data")
	writeFile(t, file, "abcd")
	if raw, err := readBoundedRegularFile(file, 4); err != nil || string(raw) != "abcd" {
		t.Fatalf("boundary read = %q, %v", raw, err)
	}
	for _, tt := range []struct {
		path  string
		limit int64
	}{{file, 3}, {file, 0}, {dir, 100}, {filepath.Join(dir, "missing"), 100}} {
		if _, err := readBoundedRegularFile(tt.path, tt.limit); err == nil {
			t.Fatalf("invalid metadata accepted: %+v", tt)
		}
	}
	writeFile(t, file, "")
	if _, err := readBoundedRegularFile(file, 100); err == nil {
		t.Fatal("empty metadata accepted")
	}
	writeFile(t, file, "abcd")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err == nil {
		if _, err := readBoundedRegularFile(link, 100); err == nil {
			t.Fatal("symlink metadata accepted")
		}
	}
}

func TestVerifyUpdateManifestFilesRejectsMissingAndInvalidEvidence(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := verifyUpdateManifestFiles(); err == nil || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "read update manifest") {
		t.Fatalf("missing manifest = %v", err)
	}
	writeFile(t, releaseManifestFile, `{}`)
	if err := verifyUpdateManifestFiles(); err == nil || !strings.Contains(err.Error(), "read Sigstore bundle") {
		t.Fatalf("missing bundle = %v", err)
	}
	writeFile(t, releaseBundleFile, `{}`)
	if err := verifyUpdateManifestFiles(); err == nil {
		t.Fatal("unsigned manifest accepted")
	}
}

func TestReleasePrepMainRejectsInvalidRequests(t *testing.T) {
	t.Setenv("ECCTL_TELEMETRY_ENDPOINT_B64", "")
	t.Setenv("ECCTL_TELEMETRY_HEADERS_B64", "")
	for _, args := range [][]string{
		nil, {"--check", "--write"}, {"--write", "--allow-existing-release"},
		{"--check-telemetry-config"}, {"--write", "--module", "invalid"},
		{"--check-homebrew-version", "--release-tag", "invalid"},
		{"--check-version-file"}, {"--verify-homebrew-cask"}, {"--convert-homebrew-cask"},
		{"--generate-update-manifest", "--release-tag", "v1.2.3"},
		{"--verify-update-manifest"}, {"--update-v2-required", "--release-tag", "invalid"}, {"--check"},
	} {
		command := exec.Command(os.Args[0], append([]string{"-test.run=^TestReleasePrepMainHelper$", "--"}, args...)...)
		command.Env = append(os.Environ(), "ECCTL_TEST_RELEASEPREP_MAIN=1")
		command.Dir = t.TempDir()
		output, err := command.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || strings.TrimSpace(string(output)) == "" {
			t.Fatalf("main %v: err=%v output=%s", args, err, output)
		}
	}
}

func TestReleasePrepMainHelper(t *testing.T) {
	if os.Getenv("ECCTL_TEST_RELEASEPREP_MAIN") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"releaseprep"}, os.Args[index+1:]...)
			flag.CommandLine = flag.NewFlagSet("releaseprep", flag.ExitOnError)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing helper argument separator")
}
