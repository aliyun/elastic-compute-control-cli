package caselint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsValidCaseSet(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))

	rep, err := Check(Options{
		CasesDir:     cases,
		InputsDir:    inputs,
		CoveragePath: coveragePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected valid lint report, got %+v", rep)
	}
	if rep.Cases != 1 || rep.Steps != 2 {
		t.Fatalf("unexpected counts for %s: %+v", root, rep)
	}
}

func TestCheckAcceptsNestedParentResourceCommand(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	inputs := filepath.Join(root, "fixtures", "inputs")
	coveragePath := filepath.Join(root, "coverage.yaml")
	mustMkdir(t, filepath.Join(cases, "ack"))
	mustMkdir(t, inputs)
	mustWrite(t, filepath.Join(cases, "ack", "check-item.yaml"), `
resource: ack/check-item
steps:
  - name: list check items
    run: ecctl ack diagnosis check-item list --cluster c --type node
`)
	mustWrite(t, coveragePath, `
version: 2
resources:
  ack/check-item:
    operations:
      list:
        status: offline
        case: cases/ack/check-item.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)
	rep, err := Check(Options{
		CasesDir:     cases,
		InputsDir:    inputs,
		CoveragePath: coveragePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected nested parent command to lint, got %+v", rep)
	}
}

func TestCheckAllowsUntaggableResourceCreates(t *testing.T) {
	_, cases, inputs, coveragePath := writeLintFixture(t, `
resource: rg/role
steps:
  - name: create role
    run: ecctl rg role create --name role --assume-role-policy-document '{"Version":"1"}'
    teardown: ecctl rg role delete role
`, `
version: 2
resources:
  rg/role:
    operations:
      create:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)

	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected untaggable role create to lint, got %+v", rep.Errors)
	}
}

func TestCheckAllowsReviewedCreateSafetyAlternatives(t *testing.T) {
	tests := []struct {
		name     string
		caseBody string
		resource string
	}{
		{
			name: "ack kubeconfig has an immediate revoke finalizer",
			caseBody: `
resource: ack/kubeconfig
steps:
  - name: create
    run: ecctl ack kubeconfig create --cluster c
    teardown: ecctl ack kubeconfig revoke --cluster c
  - name: revoke
    run: ecctl ack kubeconfig revoke --cluster c
`,
			resource: "ack/kubeconfig",
		},
		{
			name: "ack nodepool create has no tag input",
			caseBody: `
resource: ack/nodepool
steps:
  - name: create
    run: ecctl ack nodepool create --cluster c --name pool
    capture:
      nodepool_id: id
    teardown: ecctl ack nodepool delete {{.nodepool_id}} --cluster c
`,
			resource: "ack/nodepool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cases, inputs, coveragePath := writeLintFixture(t, tt.caseBody, `
version: 2
resources:
  `+tt.resource+`:
    operations:
      create:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)
			rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Invalid != 0 {
				t.Fatalf("reviewed create safety alternative should lint: %+v", rep.Errors)
			}
		})
	}
}

func TestCheckAcceptsRunnerBaseVariables(t *testing.T) {
	_, cases, inputs, coveragePath := writeLintFixture(t, `
resource: ecs/instance
steps:
  - name: get
    run: ecctl ecs instance get {{.region}}-{{.zone}}
`, `
version: 2
resources:
  ecs/instance:
    operations:
      get:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)

	rep, err := Check(Options{
		CasesDir:     cases,
		InputsDir:    inputs,
		CoveragePath: coveragePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected runner base variables to lint, got %+v", rep)
	}
}

func TestCheckAcceptsDeclaredRegionalPrerequisiteReferences(t *testing.T) {
	_, cases, inputs, coveragePath := writeLintFixture(t, `
resource: ecs/instance
requires_prerequisites: [ecs.instance_renew]
region_requirements:
  destination:
    requires_prerequisites: [ecs.image]
    distinct_from: primary
steps:
  - name: get
    run: ecctl ecs instance get {{.prerequisites.ecs.instance_renew.instance_id}} --destination-region {{.regions.destination.id}} --token {{.regions.destination.prerequisites.ecs.image.token}}
`, `
version: 2
resources:
  ecs/instance:
    operations:
      get:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)

	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 {
		t.Fatalf("declared regional prerequisites should lint: %+v", rep.Errors)
	}
}

func TestCheckRejectsUndeclaredPrerequisiteTemplateReference(t *testing.T) {
	_, cases, inputs, coveragePath := writeLintFixture(t, `
resource: ecs/instance
steps:
  - name: get
    run: ecctl ecs instance get {{.prerequisites.ecs.instance_renew.instance_id}}
`, `
version: 2
resources:
  ecs/instance:
    operations:
      get:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)

	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "missing_prerequisite_requirement") {
		t.Fatalf("expected missing_prerequisite_requirement, got %+v", rep.Errors)
	}
}

func TestCheckRejectsUndeclaredGlobalTemplateReference(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t, `
resource: ecs/instance
steps:
  - name: get
    run: ecctl ecs instance get {{.global.regions.primary}}
`, `
version: 2
resources:
  ecs/instance:
    operations:
      get:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)
	global := filepath.Join(root, "global.yaml")
	mustWrite(t, global, `
values:
  regions.primary:
    value: cn-hangzhou
`)

	rep, err := Check(Options{
		CasesDir:      cases,
		InputsDir:     inputs,
		CoveragePath:  coveragePath,
		GlobalFixture: global,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "missing_global_requirement") {
		t.Fatalf("expected missing_global_requirement, got %+v", rep.Errors)
	}
}

func TestCheckRejectsUndeclaredParameterTemplateReference(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t, `
resource: ecs/instance
steps:
  - name: get
    run: ecctl ecs instance get {{.params.ecs.instance_type}}
`, `
version: 2
resources:
  ecs/instance:
    operations:
      get:
        status: offline
        case: cases/ecs/instance.yaml
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`)
	policy := filepath.Join(root, "parameter-policy.yaml")
	mustWrite(t, policy, `
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
`)

	rep, err := Check(Options{
		CasesDir:        cases,
		InputsDir:       inputs,
		CoveragePath:    coveragePath,
		ParameterPolicy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "missing_parameter_requirement") {
		t.Fatalf("expected missing_parameter_requirement, got %+v", rep.Errors)
	}
}

func TestCheckRejectsUnsupportedStackParameter(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
	stack := filepath.Join(root, "stack.yaml")
	mustWrite(t, stack, `
provision:
  - id: image
    requires_params: [ecs.not_supported]
    run: ecctl call ecs DescribeImages
`)
	policy := filepath.Join(root, "parameter-policy.yaml")
	mustWrite(t, policy, `
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
`)
	rep, err := Check(Options{
		CasesDir:        cases,
		InputsDir:       inputs,
		CoveragePath:    coveragePath,
		StackFile:       stack,
		ParameterPolicy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "unknown_parameter_requirement") {
		t.Fatalf("expected unknown_parameter_requirement, got %+v", rep.Errors)
	}
}

func TestCheckRejectsPolicyMissingSelectedParameterCandidates(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t,
		strings.Replace(validCaseYAML(), "resource: ecs/instance", "resource: ecs/instance\nrequires_params: [ecs.instance_type]", 1),
		validCoverageYAML("cases/ecs/instance.yaml"),
	)
	policy := filepath.Join(root, "parameter-policy.yaml")
	mustWrite(t, policy, "ecs: {}\n")
	_, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath, ParameterPolicy: policy})
	if err == nil || !strings.Contains(err.Error(), "non-empty cores") {
		t.Fatalf("error = %v, want requirement-specific policy failure", err)
	}
}

func TestCheckRejectsStackTemplateWithoutParameterRequirement(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
	stack := filepath.Join(root, "stack.yaml")
	mustWrite(t, stack, `
provision:
  - id: vpc
    run: ecctl vpc create --name {{.params.ecs.zone}}
`)
	policy := filepath.Join(root, "parameter-policy.yaml")
	mustWrite(t, policy, `
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
`)
	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath, StackFile: stack, ParameterPolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "missing_parameter_requirement") {
		t.Fatalf("expected stack missing_parameter_requirement, got %+v", rep.Errors)
	}
}

func TestCheckRejectsUnknownStackNeed(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintFixture(t,
		strings.Replace(validCaseYAML(), "resource: ecs/instance", "resource: ecs/instance\nneeds: [image]", 1),
		validCoverageYAML("cases/ecs/instance.yaml"),
	)
	stack := filepath.Join(root, "stack.yaml")
	mustWrite(t, stack, `
provision:
  - id: vpc
    run: ecctl vpc create
    capture: { vpc: id }
`)

	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath, StackFile: stack})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "unknown_stack_need") {
		t.Fatalf("expected unknown_stack_need, got %+v", rep.Errors)
	}
}

func TestCheckRejectsStackCaptureOutsideDeclaredNeeds(t *testing.T) {
	caseBody := strings.Replace(validCaseYAML(), "resource: ecs/instance", "resource: ecs/instance\nneeds: [vpc]", 1)
	caseBody = strings.Replace(caseBody, "ecctl ecs instance get {{.instance_id}}", "ecctl ecs instance get {{.instance_id}} --sg {{.stack.security_group}}", 1)
	root, cases, inputs, coveragePath := writeLintFixture(t, caseBody, validCoverageYAML("cases/ecs/instance.yaml"))
	stack := filepath.Join(root, "stack.yaml")
	mustWrite(t, stack, `
provision:
  - id: vpc
    run: ecctl vpc create
    capture: { vpc: id }
  - id: security_group
    needs: [vpc]
    run: ecctl ecs sg create
    capture: { security_group: id }
`)

	rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath, StackFile: stack})
	if err != nil {
		t.Fatal(err)
	}
	if !hasLintCode(rep, "missing_stack_need") {
		t.Fatalf("expected missing_stack_need, got %+v", rep.Errors)
	}
}

func TestCheckRejectsAmbiguousStackContract(t *testing.T) {
	tests := []struct {
		name  string
		stack string
		code  string
	}{
		{
			name: "duplicate node id",
			stack: `
provision:
  - { id: vpc, run: ecctl vpc create }
  - { id: vpc, run: ecctl vpc create }
`,
			code: "duplicate_stack_id",
		},
		{
			name: "duplicate capture provider",
			stack: `
provision:
  - id: vpc
    run: ecctl vpc create
    capture: { shared: id }
  - id: image
    run: ecctl call ecs DescribeImages
    capture: { shared: ImageId }
`,
			code: "duplicate_stack_capture",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cases, inputs, coveragePath := writeLintFixture(t, validCaseYAML(), validCoverageYAML("cases/ecs/instance.yaml"))
			stack := filepath.Join(root, "stack.yaml")
			mustWrite(t, stack, tt.stack)
			rep, err := Check(Options{CasesDir: cases, InputsDir: inputs, CoveragePath: coveragePath, StackFile: stack})
			if err != nil {
				t.Fatal(err)
			}
			if !hasLintCode(rep, tt.code) {
				t.Fatalf("expected %s, got %+v", tt.code, rep.Errors)
			}
		})
	}
}

func TestCheckRejectsStaticCaseProblems(t *testing.T) {
	tests := []struct {
		name     string
		caseBody string
		coverage string
		code     string
	}{
		{
			name:     "missing input key",
			caseBody: replace(validCaseYAML(), "{{.inputs.type}}", "{{.inputs.size}}"),
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "missing_input",
		},
		{
			name: "undefined capture",
			caseBody: `
resource: ecs/instance
steps:
  - name: get
    run: ecctl ecs instance get {{.instance_id}}
`,
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "undefined_var",
		},
		{
			name:     "coverage case missing",
			caseBody: validCaseYAML(),
			coverage: validCoverageYAML("cases/ecs/missing.yaml"),
			code:     "coverage_case_missing",
		},
		{
			name: "create missing e2e tags",
			caseBody: `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --type {{.inputs.type}}
    capture:
      instance_id: id
    teardown: ecctl ecs instance delete {{.instance_id}}
`,
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "missing_run_tag",
		},
		{
			name: "duplicate capture",
			caseBody: `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --type {{.inputs.type}} --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id
    teardown: ecctl ecs instance delete {{.instance_id}}
  - name: get
    run: ecctl ecs instance get {{.instance_id}}
    capture:
      instance_id: id
`,
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "duplicate_capture",
		},
		{
			name: "create missing teardown",
			caseBody: `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --type {{.inputs.type}} --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id
`,
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "missing_teardown",
		},
		{
			name:     "coverage operation missing",
			caseBody: validCaseYAML(),
			coverage: validCoverageWithMissingOperation("cases/ecs/instance.yaml"),
			code:     "coverage_operation_missing",
		},
		{
			name: "invalid command shape",
			caseBody: `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs disk create --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      disk_id: id
    teardown: ecctl ecs disk delete {{.disk_id}}
`,
			coverage: validCoverageYAML("cases/ecs/instance.yaml"),
			code:     "invalid_command_shape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cases, inputs, coveragePath := writeLintFixture(t, tt.caseBody, tt.coverage)
			rep, err := Check(Options{
				CasesDir:     cases,
				InputsDir:    inputs,
				CoveragePath: coveragePath,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !hasLintCode(rep, tt.code) {
				t.Fatalf("expected %q, got %+v", tt.code, rep.Errors)
			}
		})
	}
}

func writeLintFixture(t *testing.T, caseBody, coverage string) (root, cases, inputs, coveragePath string) {
	t.Helper()
	root = t.TempDir()
	cases = filepath.Join(root, "cases")
	inputs = filepath.Join(root, "fixtures", "inputs")
	coveragePath = filepath.Join(root, "coverage.yaml")
	mustMkdir(t, filepath.Join(cases, "ecs"))
	mustMkdir(t, inputs)
	mustWrite(t, filepath.Join(cases, "ecs", "instance.yaml"), caseBody)
	mustWrite(t, filepath.Join(inputs, "ecs-instance.yaml"), "type: ecs.g6.large\n")
	mustWrite(t, coveragePath, coverage)
	return root, cases, inputs, coveragePath
}

func validCaseYAML() string {
	return `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --type {{.inputs.type}} --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id
    teardown: ecctl ecs instance delete {{.instance_id}}
  - name: get
    run: ecctl ecs instance get {{.instance_id}}
    expect:
      id: { eq: "{{.instance_id}}" }
`
}

func validCoverageYAML(casePath string) string {
	return `
version: 2
resources:
  ecs/instance:
    operations:
      create:
        status: offline
        case: ` + casePath + `
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
      get:
        status: offline
        case: ` + casePath + `
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`
}

func validCoverageWithMissingOperation(casePath string) string {
	return `
version: 2
resources:
  ecs/instance:
    operations:
      delete:
        status: offline
        case: ` + casePath + `
        fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
        time: "2026-07-15T00:00:00Z"
        reason: not-run
`
}

func replace(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}

func hasLintCode(rep *Report, code string) bool {
	for _, err := range rep.Errors {
		if err.Code == code {
			return true
		}
	}
	return false
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
