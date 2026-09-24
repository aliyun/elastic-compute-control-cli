package workflowpolicy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestAPISyncWorkflowIsReportOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", ".github", "workflows", "api-sync.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range apiSyncWorkflowPolicyViolations(string(raw)) {
		t.Error(violation)
	}
}

func TestAPISyncWorkflowPolicyRejectsMutationAndCloudCredentials(t *testing.T) {
	t.Parallel()
	unsafe := `
concurrency:
  group: api-sync-${{ github.run_id }}
  cancel-in-progress: false
permissions:
  contents: write
jobs:
  detect:
    environment: live-e2e
    permissions:
      contents: write
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - run: git push origin HEAD
      - run: gh pr create --base main
      - run: bin/specdrift baseline -spec-dir specs
      - run: go list -m github.com/aliyun/aliyun-openapi-meta
      - run: go get github.com/aliyun/aliyun-openapi-meta@master
      - run: make metadata-sync
      - run: make drift-baseline
      - run: go run ./cmd/openapimeta-sync
`
	violations := strings.Join(apiSyncWorkflowPolicyViolations(unsafe), "\n")
	for _, want := range []string{
		"stable api-sync-plan concurrency group",
		"must use cancel-in-progress",
		"workflow permissions must be contents: read",
		"must not use an environment",
		"must not grant id-token: write",
		"action must use a full commit SHA",
		`must not contain "git push"`,
		`must not contain "gh pr create"`,
		`must not contain "specdrift baseline"`,
		`must not contain "go list -m"`,
		`must not contain "go get"`,
		`must not contain "make metadata-sync"`,
		`must not contain "make drift-baseline"`,
		`must not contain "go run ./cmd/openapimeta-sync"`,
		`push paths must include "pkg/aliyun/**"`,
		`push paths must include "internal/openapimeta/**"`,
		`push paths must include "cmd/openapimeta-sync/**"`,
		`push paths must include ".github/workflows/api-sync.yml"`,
	} {
		if !strings.Contains(violations, want) {
			t.Errorf("expected violation %q, got:\n%s", want, violations)
		}
	}
}

func TestAPISyncWorkflowReadsManifestRevision(t *testing.T) {
	t.Parallel()
	step := apiSyncStep(t, "metadata")
	sha := strings.Repeat("a", 40)
	for _, tt := range []struct {
		name       string
		repository string
		revision   any
		valid      bool
	}{
		{"full SHA", "aliyun/aliyun-openapi-meta", sha, true},
		{"wrong repository", "other/repository", sha, false},
		{"short SHA", "aliyun/aliyun-openapi-meta", sha[:12], false},
		{"long SHA", "aliyun/aliyun-openapi-meta", sha + "a", false},
		{"non hex", "aliyun/aliyun-openapi-meta", strings.Repeat("g", 40), false},
		{"missing revision", "aliyun/aliyun-openapi-meta", nil, false},
		{"numeric revision", "aliyun/aliyun-openapi-meta", 123, false},
		{"trailing newline", "aliyun/aliyun-openapi-meta", sha + "\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := filepath.Join(dir, "internal", "openapimeta", "manifest.json")
			if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]any{"repository": tt.repository, "revision": tt.revision})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			output, _, log, err := runAPISyncStep(t, dir, step.Run)
			if tt.valid {
				if err != nil || output != "pinned_sha="+sha+"\n" {
					t.Fatalf("manifest output = %q, error = %v, log = %s", output, err, log)
				}
			} else if err == nil || output != "" {
				t.Fatalf("invalid manifest must fail without outputs: output = %q, error = %v", output, err)
			}
		})
	}
}

func TestAPISyncWorkflowStalenessIsNonFatal(t *testing.T) {
	t.Parallel()
	step := apiSyncStep(t, "staleness")
	sha := strings.Repeat("a", 40)
	for _, tt := range []struct {
		name     string
		upstream string
		exitCode string
		state    string
	}{
		{"same SHA", sha, "0", "current"},
		{"same short prefix", sha[:12] + strings.Repeat("b", 28), "0", "stale"},
		{"unreachable", "", "1", "unreachable"},
		{"failed request with SHA", sha, "1", "unreachable"},
		{"empty response", "", "0", "unreachable"},
		{"short SHA", sha[:12], "0", "unreachable"},
		{"non hex", strings.Repeat("g", 40), "0", "unreachable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			// Only the SHA endpoint is allowed; never contact GitHub in this test.
			stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$GH_CALL\"\nprintf '%s\\n' \"$UPSTREAM_SHA\"\nexit \"$GH_EXIT\"\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			callPath := filepath.Join(dir, "gh-call")
			output, summary, log, err := runAPISyncStep(t, dir, step.Run,
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"PINNED_SHA="+sha, "UPSTREAM_SHA="+tt.upstream, "GH_EXIT="+tt.exitCode, "GH_CALL="+callPath,
			)
			if err != nil {
				t.Fatalf("staleness check must not fail: %v\n%s", err, log)
			}
			call, err := os.ReadFile(callPath)
			if err != nil || string(call) != "api repos/aliyun/aliyun-openapi-meta/commits/master --jq .sha\n" {
				t.Fatalf("unexpected gh call %q: %v", call, err)
			}
			wantOutput := "state=" + tt.state + "\n"
			if tt.state != "unreachable" {
				wantOutput = "upstream=" + tt.upstream + "\n" + wantOutput
			}
			if output != wantOutput {
				t.Errorf("outputs = %q, want %q", output, wantOutput)
			}
			if !strings.Contains(summary, sha) || !strings.Contains(summary, "fixed snapshot") {
				t.Errorf("summary must identify the fixed snapshot: %s", summary)
			}
			if strings.Contains(log, "::notice::") != (tt.state == "stale") {
				t.Errorf("only stale snapshots should produce a notice: %s", log)
			}
			if strings.Contains(log, "::warning::") != (tt.state == "unreachable") {
				t.Errorf("only unreachable/invalid upstream should produce a warning: %s", log)
			}
		})
	}
}

func TestMetadataSyncMakeTargetIsExplicit(t *testing.T) {
	t.Parallel()
	requireAPISyncTools(t, "make")
	makefile, err := filepath.Abs(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	for _, tt := range []struct {
		name     string
		archive  string
		revision string
		missing  string
	}{
		{"both missing", "", "", "METADATA_ARCHIVE"},
		{"archive missing", "", sha, "METADATA_ARCHIVE"},
		{"revision missing", "/tmp/metadata archive.tar.gz", "", "METADATA_REVISION"},
		{"explicit inputs", "/tmp/metadata archive.tar.gz", sha, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("make", "--no-print-directory", "-f", makefile, "metadata-sync",
				"METADATA_ARCHIVE="+tt.archive, "METADATA_REVISION="+tt.revision)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "MAKEFLAGS=", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			log, err := cmd.CombinedOutput()
			if tt.missing != "" {
				if err == nil || !strings.Contains(string(log), tt.missing+" is required") || strings.Contains(string(log), "<run>") {
					t.Fatalf("missing input must fail before running the updater: %v\n%s", err, log)
				}
				return
			}
			want := "<run>\n<./cmd/openapimeta-sync>\n<-archive>\n<" + tt.archive + ">\n<-revision>\n<" + sha + ">\n<-out>\n<internal/openapimeta>\n"
			if err != nil || !strings.Contains(string(log), want) {
				t.Fatalf("unexpected updater invocation: %v\n%s", err, log)
			}
		})
	}
	for _, target := range []string{"metadata-sync", "lint"} {
		cmd := exec.Command("make", "--no-print-directory", "-n", "-f", makefile, target,
			"METADATA_ARCHIVE=metadata.tar.gz", "METADATA_REVISION="+sha)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "MAKEFLAGS=")
		log, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("dry-run %s: %v\n%s", target, err, log)
		}
		forbidden := []string{"gh api", "curl ", "wget ", "specdrift baseline", "drift-baseline"}
		if target == "lint" {
			forbidden = append(forbidden, "metadata-sync", "openapimeta-sync")
		}
		for _, text := range forbidden {
			if strings.Contains(string(log), text) {
				t.Errorf("%s must not download metadata, refresh the baseline, or implicitly sync: %s", target, log)
			}
		}
	}
}

func apiSyncStep(t *testing.T, id string) apiSyncWorkflowStep {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "api-sync.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document apiSyncWorkflowDocument
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, step := range document.Jobs["detect"].Steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("missing step %q", id)
	return apiSyncWorkflowStep{}
}

func requireAPISyncTools(t *testing.T, tools ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Ubuntu workflow shell tests require a POSIX host")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for workflow shell tests: %v", tool, err)
		}
	}
}

func runAPISyncStep(t *testing.T, dir, script string, env ...string) (output, summary, log string, runErr error) {
	t.Helper()
	requireAPISyncTools(t, "bash", "jq")
	outputPath := filepath.Join(dir, "github-output")
	summaryPath := filepath.Join(dir, "github-summary")
	for _, path := range []string{outputPath, summaryPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath, "GITHUB_STEP_SUMMARY="+summaryPath)
	cmd.Env = append(cmd.Env, env...)
	rawLog, runErr := cmd.CombinedOutput()
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	return read(outputPath), read(summaryPath), string(rawLog), runErr
}

func apiSyncWorkflowPolicyViolations(workflow string) []string {
	var document apiSyncWorkflowDocument
	if err := yaml.Unmarshal([]byte(workflow), &document); err != nil {
		return []string{fmt.Sprintf("api-sync.yml must parse as YAML: %v", err)}
	}
	var violations []string
	for _, path := range []string{"pkg/aliyun/**", "internal/openapimeta/**", "cmd/openapimeta-sync/**", ".github/workflows/api-sync.yml"} {
		found := false
		for _, candidate := range document.On.Push.Paths {
			if candidate == path {
				found = true
			}
		}
		if !found {
			violations = append(violations, fmt.Sprintf("api-sync.yml push paths must include %q", path))
		}
	}
	if document.Concurrency.Group != "api-sync-plan" {
		violations = append(violations, "api-sync.yml must use the stable api-sync-plan concurrency group")
	}
	if !document.Concurrency.CancelInProgress {
		violations = append(violations, "api-sync.yml must use cancel-in-progress")
	}
	if len(document.Permissions) != 1 || document.Permissions["contents"] != "read" {
		violations = append(violations, "api-sync.yml workflow permissions must be contents: read only")
	}
	target, ok := document.Jobs["detect"]
	if !ok {
		return append(violations, `api-sync.yml must define job "detect"`)
	}
	if target.Environment != nil {
		violations = append(violations, `api-sync.yml job "detect" must not use an environment`)
	}
	if target.Permissions["contents"] != "read" || target.Permissions["issues"] != "write" || len(target.Permissions) != 2 {
		violations = append(violations, `api-sync.yml job "detect" permissions must be contents: read and issues: write`)
	}
	if target.Permissions["id-token"] == "write" || document.Permissions["id-token"] == "write" {
		violations = append(violations, "api-sync.yml must not grant id-token: write")
	}
	if !strings.Contains(target.If, "refs/heads/main") {
		violations = append(violations, "api-sync.yml manual dispatch must be restricted to main")
	}

	allRun := ""
	checkoutFound := false
	uploadAlways := false
	upsertAlways := false
	for index, step := range target.Steps {
		allRun += "\n" + step.Run
		if step.ID == "staleness" {
			if step.Env["PINNED_SHA"] != "${{ steps.metadata.outputs.pinned_sha }}" || step.Env["GH_TOKEN"] != "${{ github.token }}" {
				violations = append(violations, "api-sync.yml staleness must use the manifest SHA and GitHub token")
			}
		}
		if step.Name == "build dry-run sync plan and bounded issue body" || step.Name == "upsert drift monitor issue" {
			if step.Env["METADATA_REVISION"] != "${{ steps.metadata.outputs.pinned_sha }}" || !strings.Contains(step.Run, "embedded metadata snapshot commit:") {
				violations = append(violations, "api-sync.yml reports must identify the embedded snapshot revision")
			}
		}
		if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
			violations = append(violations, fmt.Sprintf(
				"api-sync.yml job detect step %d action must use a full commit SHA, got %q", index+1, step.Uses,
			))
		}
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkoutFound = true
			if workflowString(step.With["ref"]) != "${{ github.event.repository.default_branch }}" {
				violations = append(violations, "api-sync.yml checkout must pin the repository default branch")
			}
			if workflowString(step.With["persist-credentials"]) != "false" {
				violations = append(violations, "api-sync.yml checkout must disable persisted credentials")
			}
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") && strings.Contains(step.If, "always()") {
			uploadAlways = true
		}
		if step.Name == "upsert drift monitor issue" && strings.Contains(step.If, "always()") && strings.Contains(step.If, "detect.outcome") {
			upsertAlways = true
		}
	}
	if !checkoutFound {
		violations = append(violations, "api-sync.yml must check out the repository default branch")
	}
	if !uploadAlways {
		violations = append(violations, "api-sync.yml must upload drift evidence even when planning fails")
	}
	if !upsertAlways {
		violations = append(violations, "api-sync.yml must maintain the drift issue even when planning fails")
	}
	for _, forbidden := range []string{
		"git push", "gh pr create", "specdrift baseline", "ecctl-e2e", "ECCTL_BOT", "configure-aliyun-credentials",
		"go list -m", "git ls-remote", "go get", "make metadata-sync", "go run ./cmd/openapimeta-sync", "make drift-baseline", "tarball/",
	} {
		if strings.Contains(allRun, forbidden) {
			violations = append(violations, fmt.Sprintf("api-sync.yml must not contain %q", forbidden))
		}
	}
	if !strings.Contains(allRun, "specdrift render") || !strings.Contains(allRun, "-limit 50") {
		violations = append(violations, "api-sync.yml must render a bounded 50-row issue body")
	}
	return violations
}

type apiSyncWorkflowDocument struct {
	On struct {
		Push struct {
			Paths []string `yaml:"paths"`
		} `yaml:"push"`
	} `yaml:"on"`
	Concurrency apiSyncConcurrency            `yaml:"concurrency"`
	Permissions map[string]string             `yaml:"permissions"`
	Jobs        map[string]apiSyncWorkflowJob `yaml:"jobs"`
}

type apiSyncConcurrency struct {
	Group            string `yaml:"group"`
	CancelInProgress bool   `yaml:"cancel-in-progress"`
}

type apiSyncWorkflowJob struct {
	If          string                `yaml:"if"`
	Environment any                   `yaml:"environment"`
	Permissions map[string]string     `yaml:"permissions"`
	Steps       []apiSyncWorkflowStep `yaml:"steps"`
}

type apiSyncWorkflowStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}
