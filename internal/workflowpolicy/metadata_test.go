package workflowpolicy

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestMetadataToolsUseHostDuringCrossBuild(t *testing.T) {
	t.Parallel()
	requireAPISyncTools(t, "make")
	makefile, err := filepath.Abs(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Empty overrides still inherit a cross target persisted in GOENV.
	stub := `#!/bin/sh
set -eu
case "$1" in
run)
  test "${GOOS:-windows}" = "$METADATA_TEST_HOST_OS"
  test "${GOARCH:-amd64}" = "$METADATA_TEST_HOST_ARCH"
  ;;
build)
  test "$GOOS" = windows
  test "$GOARCH" = amd64
  printf '#!/bin/sh\necho ecctl-test\n' > bin/ecctl
  chmod 700 bin/ecctl
  ;;
env)
  case "$2" in
  GOHOSTOS) printf '%s\n' "$METADATA_TEST_HOST_OS" ;;
  GOHOSTARCH) printf '%s\n' "$METADATA_TEST_HOST_ARCH" ;;
  *) exit 1 ;;
  esac
  exit 0
  ;;
*) exit 1 ;;
esac
printf '%s\n' "$1" >> "$METADATA_TEST_LOG"
`
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "go.log")
	cmd := exec.Command("make", "--no-print-directory", "-f", makefile,
		"metadata-prepare", "metadata-sync", "build",
		"METADATA_ARCHIVE=source.tar.gz", "METADATA_REVISION="+strings.Repeat("a", 40))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MAKEFLAGS=", "GOOS=windows", "GOARCH=amd64",
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"METADATA_TEST_HOST_OS="+runtime.GOOS, "METADATA_TEST_HOST_ARCH="+runtime.GOARCH,
		"METADATA_TEST_LOG="+logPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("metadata tools must run on the host during a cross build: %v\n%s", err, output)
	}
	log, err := os.ReadFile(logPath)
	if err != nil || string(log) != "run\nrun\nrun\nbuild\n" {
		t.Fatalf("metadata preparation, update, check and target build were not all exercised: %q, %v", log, err)
	}
}

func TestMetadataPreparationCoversBuildWorkflows(t *testing.T) {
	for _, name := range []string{"ci.yml", "release.yml", "api-sync.yml", "e2e-nightly.yml", "e2e-sweeper.yml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						Run              string         `yaml:"run"`
						WorkingDirectory string         `yaml:"working-directory"`
						With             map[string]any `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			builds := 0
			for jobName, job := range workflow.Jobs {
				prepared := map[string]bool{}
				for _, step := range job.Steps {
					dir := step.WorkingDirectory
					if dir == "" {
						dir = "."
					}
					for _, line := range strings.Split(step.Run, "\n") {
						line = strings.TrimSpace(line)
						if line == "make metadata-prepare" || line == "go run ./cmd/openapimeta-sync -prepare" {
							prepared[dir] = true
						}
						if strings.Contains(line, "go build") || strings.HasPrefix(line, "go test ") || line == "make lint" || line == "make test" || strings.HasPrefix(line, "make -C e2e ") {
							builds++
							if !prepared[dir] {
								t.Errorf("%s/%s builds in %s before metadata preparation: %s", name, jobName, dir, line)
							}
						}
					}
					args, _ := step.With["args"].(string)
					if strings.HasPrefix(args, "release ") {
						builds++
						if workdir, ok := step.With["workdir"].(string); ok {
							dir = workdir
						}
						if !prepared[dir] {
							t.Errorf("%s/%s releases in %s before metadata preparation", name, jobName, dir)
						}
					}
				}
			}
			if builds == 0 {
				t.Fatal("workflow build coverage disappeared")
			}
		})
	}
}

func TestMetadataArtifactsStayOutsideGit(t *testing.T) {
	root := filepath.Join("..", "..")
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/internal/openapimeta/.generated/", "/internal/openapimeta/.cache/", "/internal/openapimeta/.prepare.lock", "/internal/openapimeta/.prepare-*"} {
		if !strings.Contains(string(ignore), path+"\n") {
			t.Errorf("build artifact is not ignored: %s", path)
		}
	}
	files, err := os.ReadDir(filepath.Join(root, "internal", "openapimeta"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".zip") || strings.HasSuffix(file.Name(), ".gz") {
			t.Errorf("metadata package retains a compressed snapshot: %s", file.Name())
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Before struct {
			Hooks []string `yaml:"hooks"`
		} `yaml:"before"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(config.Before.Hooks, "\n"), "make metadata-prepare") {
		t.Fatal("standalone GoReleaser builds must prepare metadata")
	}
}
