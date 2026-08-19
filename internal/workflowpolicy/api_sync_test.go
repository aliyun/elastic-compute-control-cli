package workflowpolicy

import (
	"fmt"
	"os"
	"path/filepath"
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
	} {
		if !strings.Contains(violations, want) {
			t.Errorf("expected violation %q, got:\n%s", want, violations)
		}
	}
}

func apiSyncWorkflowPolicyViolations(workflow string) []string {
	var document apiSyncWorkflowDocument
	if err := yaml.Unmarshal([]byte(workflow), &document); err != nil {
		return []string{fmt.Sprintf("api-sync.yml must parse as YAML: %v", err)}
	}
	var violations []string
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
	Name string         `yaml:"name"`
	If   string         `yaml:"if"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}
