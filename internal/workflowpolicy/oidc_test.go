package workflowpolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pinnedAction = regexp.MustCompile(`^[^@[:space:]]+@[0-9a-f]{40}$`)

func TestLiveE2EWorkflowsUseBoundedOIDCCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path       string
		expiration string
		session    string
	}{
		{
			path:       "e2e-nightly.yml",
			expiration: "role-session-expiration: 18000",
			session:    "role-session-name: ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}",
		},
		{
			path:       "e2e-sweeper.yml",
			expiration: "role-session-expiration: 7200",
			session:    "role-session-name: ecctl-sweeper-${{ github.run_id }}-${{ github.run_attempt }}",
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
				tt.path, string(contents), tt.expiration, tt.session,
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
		"role-session-expiration: 18000",
		"role-session-name: ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}",
	), "\n")

	for _, expected := range []string{
		`must contain "role-session-expiration: 18000"`,
		`must contain "role-session-name: ecctl-e2e-${{ github.run_id }}-${{ github.run_attempt }}"`,
		"uses unsupported role-session-duration input",
		"action must use a full commit SHA",
	} {
		if !strings.Contains(violations, expected) {
			t.Errorf("expected violation %q, got:\n%s", expected, violations)
		}
	}
}

func liveE2EWorkflowPolicyViolations(name, workflow, expiration, session string) []string {
	var violations []string
	for _, required := range []string{
		"id-token: write",
		"environment: live-e2e",
		"audience: sts.aliyuncs.com",
		expiration,
		session,
	} {
		if !strings.Contains(workflow, required) {
			violations = append(violations, fmt.Sprintf("%s must contain %q", name, required))
		}
	}
	if strings.Contains(workflow, "role-session-duration:") {
		violations = append(violations, name+" uses unsupported role-session-duration input")
	}

	for lineNumber, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- uses: ") {
			continue
		}
		ref := strings.Fields(strings.TrimPrefix(trimmed, "- uses: "))[0]
		if !pinnedAction.MatchString(ref) {
			violations = append(violations, fmt.Sprintf(
				"%s:%d action must use a full commit SHA, got %q", name, lineNumber+1, ref,
			))
		}
	}
	return violations
}
