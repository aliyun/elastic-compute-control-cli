package workflowpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

var pinnedAction = regexp.MustCompile(`^[^@[:space:]]+@[0-9a-f]{40}$`)

func TestLiveE2EWorkflowsUseBoundedOIDCCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path        string
		job         string
		environment string
		role        string
		expiration  string
		session     string
	}{
		{
			path:        "e2e-nightly.yml",
			job:         "e2e",
			environment: "live-e2e",
			role:        "ecctl-e2e-ci",
			expiration:  "18000",
			session:     "ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}",
		},
		{
			path:        "e2e-sweeper.yml",
			job:         "sweep",
			environment: "live-e2e-sweeper",
			role:        "ecctl-e2e-sweeper-ci",
			expiration:  "7200",
			session:     "ecctl-sweeper-${{ github.run_id }}-${{ github.run_attempt }}",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("..", "..", ".github", "workflows", tt.path)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			for _, violation := range liveE2EWorkflowPolicyViolations(
				tt.path, string(contents), tt.job, tt.environment, tt.role, tt.expiration, tt.session,
			) {
				t.Error(violation)
			}
		})
	}
}

func TestLiveE2EWorkflowPolicyRejectsLegacyConfiguration(t *testing.T) {
	t.Parallel()

	legacy := `
permissions:
  id-token: write
jobs:
  e2e:
    environment: live-e2e
    steps:
      - uses: actions/checkout@v4
      - uses: aliyun/configure-aliyun-credentials-action@v1
        with:
          audience: sts.aliyuncs.com
          role-session-duration: 21600
`
	violations := strings.Join(liveE2EWorkflowPolicyViolations(
		"legacy.yml",
		legacy,
		"e2e",
		"live-e2e",
		"ecctl-e2e-ci",
		"18000",
		"ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}",
	), "\n")

	for _, expected := range []string{
		`must set role-session-expiration to "18000"`,
		`must set role-session-name to "ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}"`,
		"uses unsupported role-session-duration input",
		"action must use a full commit SHA",
	} {
		if !strings.Contains(violations, expected) {
			t.Errorf("expected violation %q, got:\n%s", expected, violations)
		}
	}
}

func TestLiveE2EWorkflowPolicyRejectsRequiredTextInAnotherJob(t *testing.T) {
	t.Parallel()

	workflow := `
permissions:
  contents: read
jobs:
  decoy:
    environment: live-e2e
    permissions:
      id-token: write
    steps:
      - uses: aliyun/configure-aliyun-credentials-action@1e5248c8d5d93a8781ac344a68e19a43341e79e6
        with:
          role-to-assume: acs:ram::123:role/ecctl-e2e-ci
          audience: sts.aliyuncs.com
          role-session-expiration: 18000
          role-session-name: ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}
  e2e:
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262
`
	violations := strings.Join(liveE2EWorkflowPolicyViolations(
		"misplaced.yml",
		workflow,
		"e2e",
		"live-e2e",
		"ecctl-e2e-ci",
		"18000",
		"ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}",
	), "\n")
	if !strings.Contains(violations, `job "e2e"`) {
		t.Fatalf("expected job-scoped policy violation, got:\n%s", violations)
	}
}

func liveE2EWorkflowPolicyViolations(name, workflow, job, environment, role, expiration, session string) []string {
	var violations []string
	var document workflowPolicyDocument
	if err := yaml.Unmarshal([]byte(workflow), &document); err != nil {
		return []string{fmt.Sprintf("%s must parse as YAML: %v", name, err)}
	}
	if document.Permissions["id-token"] == "write" {
		violations = append(violations, name+" must not grant id-token: write at workflow scope")
	}
	for jobName, candidate := range document.Jobs {
		if jobName != job && candidate.Permissions["id-token"] == "write" {
			violations = append(violations, fmt.Sprintf("%s job %q must not grant id-token: write", name, jobName))
		}
	}
	target, ok := document.Jobs[job]
	if !ok {
		return append(violations, fmt.Sprintf("%s must define job %q", name, job))
	}
	if target.EnvironmentName() != environment {
		violations = append(violations, fmt.Sprintf("%s job %q must use environment %q", name, job, environment))
	}
	if target.Permissions["id-token"] != "write" {
		violations = append(violations, fmt.Sprintf("%s job %q must grant id-token: write", name, job))
	}

	var credential *workflowPolicyStep
	for _, candidate := range target.Steps {
		if strings.HasPrefix(candidate.Uses, "aliyun/configure-aliyun-credentials-action@") {
			step := candidate
			credential = &step
			break
		}
	}
	if credential == nil {
		violations = append(violations, fmt.Sprintf("%s job %q must configure Alibaba Cloud credentials", name, job))
	} else {
		roleARN := workflowString(credential.With["role-to-assume"])
		if !strings.HasSuffix(roleARN, ":role/"+role) {
			violations = append(violations, fmt.Sprintf("%s job %q must assume role %q", name, job, role))
		}
		providerARN := workflowString(credential.With["oidc-provider-arn"])
		if !strings.HasSuffix(providerARN, ":oidc-provider/github-actions") {
			violations = append(violations, fmt.Sprintf("%s job %q must use the github-actions OIDC provider", name, job))
		}
		for key, want := range map[string]string{
			"audience":                "sts.aliyuncs.com",
			"role-session-expiration": expiration,
			"role-session-name":       session,
		} {
			if got := workflowString(credential.With[key]); got != want {
				violations = append(violations, fmt.Sprintf("%s job %q must set %s to %q, got %q", name, job, key, want, got))
			}
		}
		if _, exists := credential.With["role-session-duration"]; exists {
			violations = append(violations, name+" uses unsupported role-session-duration input")
		}
	}

	for jobName, candidate := range document.Jobs {
		for stepIndex, step := range candidate.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				violations = append(violations, fmt.Sprintf(
					"%s job %q step %d action must use a full commit SHA, got %q", name, jobName, stepIndex+1, step.Uses,
				))
			}
		}
	}
	return violations
}

type workflowPolicyDocument struct {
	Permissions map[string]string            `yaml:"permissions"`
	Jobs        map[string]workflowPolicyJob `yaml:"jobs"`
}

type workflowPolicyJob struct {
	Environment any                  `yaml:"environment"`
	Permissions map[string]string    `yaml:"permissions"`
	Steps       []workflowPolicyStep `yaml:"steps"`
}

func (j workflowPolicyJob) EnvironmentName() string {
	switch environment := j.Environment.(type) {
	case string:
		return environment
	case map[string]any:
		return workflowString(environment["name"])
	default:
		return ""
	}
}

type workflowPolicyStep struct {
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

func workflowString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
