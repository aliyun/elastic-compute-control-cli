package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	paramspkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/params"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

// TestRunWithFakeEcctl exercises the full runner — provision skip, step exec,
// matchers, capture, and two-level teardown — against a fake ecctl that echoes
// canned JSON, so the orchestration is verified without touching the cloud.
func TestRunWithFakeEcctl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()

	fake := filepath.Join(dir, "ecctl")
	script := `#!/usr/bin/env bash
args="$*"
echo "$args" >> "$FAKE_LOG"
if [[ "$args" == *" delete "* ]]; then echo '{"ok":true}'; exit 0; fi
echo '{"resource":{"id":"res-123","status":"Available","cidr":"10.20.0.0/16"}}'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseYAML := `resource: t/thing
steps:
  - name: create
    run: ecctl t thing create --name {{.run_name}}
    at: $.resource
    expect:
      id: { type: string, prefix: res- }
      status: Available
    capture:
      tid: id
    teardown: ecctl t thing delete {{.tid}}
  - name: get
    run: ecctl t thing get {{.tid}}
    at: $.resource
    expect:
      id: res-123
      cidr: "10.20.0.0/16"
`
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(caseYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir:    filepath.Join(dir, "cases"),
		InputsDir:   filepath.Join(dir, "inputs"),
		RunName:     "ecctl-e2e-test",
		RunID:       "test",
		Parallel:    2,
		EcctlBin:    fake,
		StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Summary.Passed != 1 || run.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v\ncase: %+v", run.Summary, run.Cases)
	}
	logb, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logb), "delete res-123") {
		t.Fatalf("teardown not invoked; calls:\n%s", logb)
	}
}

func TestRunRendersRelativeMonitorWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" > "$FAKE_LOG"
echo '{"monitor_data":[]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ecs")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "monitor.yaml"), []byte(`
resource: ecs/instance
steps:
  - name: monitor
    run: ecctl ecs instance monitor i-test --start-time {{.time.monitor_start}} --end-time {{.time.monitor_end}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, InputsDir: filepath.Join(dir, "inputs"), RunName: "test", RunID: "test",
		EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil || run.Summary.Failed != 0 {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	logb, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(logb))
	if len(fields) != 8 {
		t.Fatalf("monitor command = %q", logb)
	}
	start, err := time.Parse(time.RFC3339, fields[5])
	if err != nil {
		t.Fatalf("start time %q is not RFC3339: %v", fields[5], err)
	}
	end, err := time.Parse(time.RFC3339, fields[7])
	if err != nil {
		t.Fatalf("end time %q is not RFC3339: %v", fields[7], err)
	}
	if end.Sub(start) != time.Hour {
		t.Fatalf("monitor window = %s, want 1h", end.Sub(start))
	}
}

func TestRunFailsAtStepWhenDeclaredCaptureIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{"policies":[]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ack")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "policy.yaml"), []byte(`
resource: ack/region
steps:
  - name: list policies
    run: ecctl ack policy list
    capture:
      policy_name: $.policies[0].name
  - name: get policy
    run: ecctl ack policy get {{.policy_name}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir:    filepath.Join(dir, "cases"),
		InputsDir:   filepath.Join(dir, "inputs"),
		RunName:     "ecctl-e2e-test",
		RunID:       "test",
		EcctlBin:    fake,
		StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 1 || len(run.Cases) != 1 {
		t.Fatalf("run = %+v, want one failed case", run)
	}
	steps := run.Cases[0].Steps
	if len(steps) != 1 {
		t.Fatalf("steps = %+v, want failure at capture-producing step", steps)
	}
	if !strings.Contains(steps[0].Error, `capture "policy_name": path "$.policies[0].name" not found`) {
		t.Fatalf("step error = %q, want missing capture detail", steps[0].Error)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "policy get") {
		t.Fatalf("later step ran after missing capture: %s", log)
	}
}

func TestRunProvisionsOnlyRequestedStackNodes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{"resource":{"id":"res-123"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: vpc
    run: ecctl test stack vpc create
    at: $.resource
    capture: { vpc_id: id }
    teardown: ecctl test stack vpc delete {{.vpc_id}}
  - id: image
    run: ecctl test stack image create
    at: $.resource
    capture: { image_id: id }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		Suites: []*scenario.Suite{{
			Resource: "test/vpc", Needs: []string{"vpc"},
			Steps: []scenario.Step{{Name: "list", Run: "ecctl test case vpc"}},
		}},
		StackFile: stack,
		RunName:   "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "stack image") {
		t.Fatalf("unrequested image node was provisioned: %s", log)
	}
	if !strings.Contains(string(log), "stack vpc create") {
		t.Fatalf("requested vpc node was not provisioned: %s", log)
	}
}

func TestRunIsolatesIndependentStackBranchFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *"stack image create"* ]]; then
  echo '{"error":{"message":"image unavailable"}}'
  exit 42
fi
echo '{"resource":{"id":"res-123"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: vpc
    run: ecctl test stack vpc create
    at: $.resource
    capture: { vpc_id: id }
    teardown: ecctl test stack vpc delete {{.vpc_id}}
  - id: image
    run: ecctl test stack image create
    at: $.resource
    capture: { image_id: id }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		Suites: []*scenario.Suite{
			{Resource: "test/vpc", Needs: []string{"vpc"}, Steps: []scenario.Step{{Name: "list", Run: "ecctl test case vpc"}}},
			{Resource: "test/image", Needs: []string{"image"}, Steps: []scenario.Step{{Name: "list", Run: "ecctl test case image"}}},
		},
		StackFile: stack,
		RunName:   "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	vpcCase := findReportCase(t, run, "test/vpc")
	if vpcCase.Status != report.StatusPass {
		t.Fatalf("independent vpc case = %+v, want pass", vpcCase)
	}
	imageCase := findReportCase(t, run, "test/image")
	if imageCase.Status != report.StatusSkipped {
		t.Fatalf("image-dependent case = %+v, want skipped", imageCase)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "case image") {
		t.Fatalf("case ran despite failed image dependency: %s", log)
	}
	if !strings.Contains(string(log), "case vpc") {
		t.Fatalf("independent vpc case did not run: %s", log)
	}
}

func findReportCase(t *testing.T, run *report.Run, resource string) report.Case {
	t.Helper()
	for _, result := range run.Cases {
		if result.Resource == resource {
			return result
		}
	}
	t.Fatalf("case %q not found in %+v", resource, run.Cases)
	return report.Case{}
}

// TestMatcherVarSubstitution guards the fix where {{.var}} inside matcher
// values (e.g. eq: "{{.tid}}") must be rendered before comparison.
func TestMatcherVarSubstitution(t *testing.T) {
	data := map[string]any{"tid": "res-123"}
	exps := scenario.Expectations{
		{Path: "id", Matcher: scenario.Matcher{HasEq: true, Eq: "{{.tid}}"}},
	}
	rexps, _, err := renderExpectations(exps, nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if got := rexps[0].Matcher.Eq; got != "res-123" {
		t.Fatalf("eq not rendered: got %v", got)
	}
}

func TestRunDryRun(t *testing.T) {
	dir := t.TempDir()
	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseYAML := `resource: t/thing
steps:
  - name: create
    run: ecctl t thing create --name {{.run_name}}-x
    capture:
      tid: id
  - name: get
    run: ecctl t thing get {{.tid}}
`
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(caseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := Run(context.Background(), Options{
		CasesDir: filepath.Join(dir, "cases"),
		RunName:  "ecctl-e2e-test",
		RunID:    "test",
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Skipped != 1 {
		t.Fatalf("expected 1 skipped case in dry-run, got %+v", run.Summary)
	}
	// second step must have rendered the captured placeholder
	got := run.Cases[0].Steps[1].Command
	if !strings.Contains(got, "<tid>") {
		t.Fatalf("dry-run did not render placeholder capture: %q", got)
	}
}

func TestRunInjectsDeclaredGlobalFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(`
resource: t/thing
requires_global: [regions.primary]
steps:
  - name: list
    run: ecctl t thing list --region {{.global.regions.primary}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "global.yaml")
	if err := os.WriteFile(global, []byte(`
values:
  regions.primary:
    value: cn-shanghai
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir:      filepath.Join(dir, "cases"),
		InputsDir:     filepath.Join(dir, "inputs"),
		GlobalFixture: global,
		RunName:       "ecctl-e2e-test",
		RunID:         "test",
		EcctlBin:      fake,
		StepTimeout:   30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "--region cn-shanghai") {
		t.Fatalf("global fixture was not rendered: %s", log)
	}
}

func TestRunResolvesECSParamsBeforeCaseExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	script := `#!/usr/bin/env bash
args="$*"
case "$args" in
  *"DescribeImages"*) echo '{"response":{"Images":{"Image":[{"ImageId":"aliyun-3"}]}}}' ;;
  *"DestinationResource InstanceType"*) echo '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-hangzhou-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"InstanceType","SupportedResources":{"SupportedResource":[{"Value":"ecs.g7.large","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}' ;;
  *"DestinationResource SystemDisk"*) echo '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-hangzhou-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"SystemDisk","SupportedResources":{"SupportedResource":[{"Value":"cloud_efficiency","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}' ;;
  *"DestinationResource DataDisk"*) echo '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-hangzhou-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"DataDisk","SupportedResources":{"SupportedResource":[{"Value":"cloud_efficiency","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}' ;;
  *) echo "$args" >> "$FAKE_LOG"; echo '{}' ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ecs")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "instance.yaml"), []byte(`
resource: ecs/instance
requires_params: [ecs.instance_type]
steps:
  - name: list
    run: ecctl ecs instance list --filter type={{.params.ecs.instance_type}} --zone {{.zone}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte(`
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
  image_owner_alias: system
  io_optimized: optimized
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir:        filepath.Join(dir, "cases"),
		InputsDir:       filepath.Join(dir, "inputs"),
		ParameterMode:   "auto",
		ParameterPolicy: policy,
		Region:          "cn-hangzhou",
		RunName:         "ecctl-e2e-test",
		RunID:           "test",
		EcctlBin:        fake,
		StepTimeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "--filter type=ecs.g7.large") {
		t.Fatalf("resolved parameter was not rendered into case: %s", log)
	}
	if !strings.Contains(string(log), "--zone cn-hangzhou-b") {
		t.Fatalf("resolved zone was not exposed to top-level case data: %s", log)
	}
	if got := run.Parameters["ecs"].(map[string]any)["instance_type"]; got != "ecs.g7.large" {
		t.Fatalf("report parameters instance_type = %#v", got)
	}
}

func TestRunSkipsOnlyConstrainedCasesWhenNoCompatibleECSCombinationExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
case "$*" in
  *"DescribeInstanceTypes"*) echo '{"response":{"InstanceTypes":{"InstanceType":[]}}}' ;;
  *"DestinationResource InstanceType"*) echo '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-hangzhou-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"InstanceType","SupportedResources":{"SupportedResource":[{"Value":"ecs.g7.large","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}' ;;
  *) echo '{}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ecs")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "eni.yaml"), []byte(`
resource: ecs/eni
requires_params: [ecs.instance_type]
parameter_constraints:
  ecs:
    min_eni_quantity: 2
steps:
  - name: list
    run: ecctl ecs eni list
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "instance.yaml"), []byte(`
resource: ecs/instance
requires_params: [ecs.instance_type]
steps:
  - name: list
    run: ecctl ecs instance list --filter type={{.params.ecs.instance_type}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte("ecs:\n  cores: [1]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run, err := Run(context.Background(), Options{
		CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"),
		ParameterMode: "auto", ParameterPolicy: policy, Region: "cn-hangzhou",
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("constrained inventory miss must not abort run: %v", err)
	}
	statuses := map[string]string{}
	for _, result := range run.Cases {
		statuses[result.Resource] = result.Status
	}
	if statuses["ecs/eni"] != report.StatusSkipped {
		t.Fatalf("ENI status = %q, want skipped; cases=%+v", statuses["ecs/eni"], run.Cases)
	}
	if statuses["ecs/instance"] != report.StatusPass {
		t.Fatalf("instance status = %q, want pass; cases=%+v", statuses["ecs/instance"], run.Cases)
	}
}

func TestRunSkipsOnlyACKUpgradeCaseWhenNoUpgradePathExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"ack version list"*) echo '{"versions":[{"version":"1.36.1-aliyun.1","creatable":true}]}' ;;
  *) echo '{}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ack")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "crud.yaml"), []byte(`
resource: ack/ack
requires_params: [ack.cluster_type, ack.version]
steps:
  - name: list
    run: ecctl ack list
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "upgrade.yaml"), []byte(`
resource: ack/ack
requires_params: [ack.cluster_type, ack.version, ack.upgrade_version]
needs: [vpc]
steps:
  - name: upgrade
    run: ecctl ack upgrade c-example --version {{.params.ack.upgrade_version}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte("ecs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: vpc
    run: ecctl test stack create
    at: $.vpc
    capture: { vpc: id }
    teardown: ecctl test stack delete {{.vpc}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"),
		StackFile:     stack,
		ParameterMode: "auto", ParameterPolicy: policy, Region: "cn-hangzhou",
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("missing upgrade path must not abort ACK CRUD: %v", err)
	}
	statuses := map[string]string{}
	for _, result := range run.Cases {
		statuses[result.Name] = result.Status
	}
	if statuses["crud"] != report.StatusPass || statuses["upgrade"] != report.StatusSkipped {
		t.Fatalf("case statuses = %#v; cases=%+v", statuses, run.Cases)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logData), "test stack create") {
		t.Fatalf("stack used only by skipped upgrade case was still provisioned:\n%s", logData)
	}
}

func TestResourcePrefixIsStableAndBounded(t *testing.T) {
	long := "ecctl-e2e-public-live-ack-node-region-20260714-134254"
	first := resourcePrefix(long)
	second := resourcePrefix(long)
	if first != second {
		t.Fatalf("prefix is not stable: %q != %q", first, second)
	}
	if len([]rune(first)) > 40 {
		t.Fatalf("prefix length = %d, want <= 40: %q", len([]rune(first)), first)
	}
	if first == resourcePrefix(long+"-other") {
		t.Fatalf("different run names must retain distinct hash suffixes: %q", first)
	}
}

func TestRunResolvesVSwitchZoneWithoutFullECSDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"ecs zone list --verbose"*) echo '{"zones":[{"id":"cn-hangzhou-b","available_resource_creation":["VSwitch","Instance"]}]}' ;;
  *) echo '{"vpc":{"id":"vpc-1"},"vswitch":{"id":"vsw-1"}}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: vpc
    run: ecctl test stack vpc create
    at: $.vpc
    capture: { vpc: id }
  - id: vswitch
    needs: [vpc]
    requires_params: [ecs.zone]
    run: ecctl test stack vswitch create --zone {{.params.ecs.zone}}
    at: $.vswitch
    capture: { vswitch: id }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte("ecs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		Suites:    []*scenario.Suite{{Resource: "test/vswitch", Needs: []string{"vswitch"}, Steps: []scenario.Step{{Name: "list", Run: "ecctl test case vswitch"}}}},
		StackFile: stack, ParameterPolicy: policy, Region: "cn-hangzhou",
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "ecs zone list --verbose") {
		t.Fatalf("zone inventory was not queried: %s", log)
	}
	if strings.Contains(string(log), "DescribeAvailableResource") || strings.Contains(string(log), "DescribeImages") {
		t.Fatalf("zone-only resolution performed full ECS discovery: %s", log)
	}
	ecs, ok := run.Parameters["ecs"].(map[string]any)
	if !ok || ecs["zone"] != "cn-hangzhou-b" {
		t.Fatalf("ECS parameters = %#v", run.Parameters["ecs"])
	}
	if _, exists := ecs["instance_type"]; exists {
		t.Fatalf("zone-only ECS parameters unexpectedly contain instance fields: %#v", ecs)
	}
}

func TestStaticParametersExposeOnlyRequestedACKFields(t *testing.T) {
	parameters := staticParameters(paramspkg.Policy{}, "cn-hangzhou", "", false, true, false, []string{"ack.cluster_type"}, nil)
	ack, ok := parameters["ack"].(map[string]any)
	if !ok || ack["cluster_type"] != "ManagedKubernetes" {
		t.Fatalf("ACK parameters = %#v", parameters["ack"])
	}
	if _, exists := ack["zone"]; exists {
		t.Fatalf("metadata-only static ACK parameters unexpectedly contain node fields: %#v", ack)
	}
}

func TestOSSExportPrefixIsStableAndObjectSafe(t *testing.T) {
	first := ossExportPrefix("run/with spaces")
	if first != ossExportPrefix("run/with spaces") {
		t.Fatal("OSS export prefix must be deterministic for one run")
	}
	if len(first) != 27 || !strings.HasPrefix(first, "E2E") {
		t.Fatalf("OSS export prefix = %q", first)
	}
	if first == ossExportPrefix("another-run") {
		t.Fatal("different runs must not share an OSS export prefix")
	}
	for _, char := range first {
		if (char < '0' || char > '9') && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			t.Fatalf("OSS export prefix contains unsupported character %q: %q", char, first)
		}
	}
}

func TestRunResolvesACKMetadataWithoutECSDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"ack version list"*) echo '{"versions":[{"version":"1.31.1-aliyun.1","creatable":true,"upgradable_versions":["1.32.0-aliyun.1"],"runtimes":[{"name":"containerd","version":"1.6.28"}]}]}' ;;
  *) echo '{}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte("ecs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: vswitch
    requires_params: [ecs.zone]
    run: ecctl test stack vswitch create
  - id: image
    requires_params: [ecs.image_id]
    run: ecctl test stack image create
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)

	run, err := Run(context.Background(), Options{
		Suites: []*scenario.Suite{{
			Resource: "ack/region", RequiresParams: []string{"ack.cluster_type"},
			Steps: []scenario.Step{{Name: "list", Run: "ecctl ack region list --type {{.params.ack.cluster_type}}"}},
		}},
		StackFile: stack, ParameterPolicy: policy, Region: "cn-hangzhou",
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "DescribeAvailableResource") || strings.Contains(string(log), "DescribeImages") || strings.Contains(string(log), "ecs zone list") {
		t.Fatalf("ACK metadata resolution performed ECS discovery: %s", log)
	}
	if !strings.Contains(string(log), "--type ManagedKubernetes") {
		t.Fatalf("resolved ACK cluster type was not rendered: %s", log)
	}
	ack, ok := run.Parameters["ack"].(map[string]any)
	if !ok || ack["cluster_type"] != "ManagedKubernetes" {
		t.Fatalf("ACK parameters = %#v", run.Parameters["ack"])
	}
	if _, exists := ack["zone"]; exists {
		t.Fatalf("metadata-only ACK parameters unexpectedly contain node fields: %#v", ack)
	}
}

func TestRunResolvesLingjunParamsFromDynamicInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
case "$*" in
  *"lingjun node list"*) echo '{"nodes":[{"node_group":"ng-a","hpn_zone":"hpn-a","zone":"cn-hangzhou-b","machine_type":"lingjun.g1xlarge","image_id":"img-lite-1"},{"node_group":"ng-b","hpn_zone":"hpn-a","zone":"cn-hangzhou-b","machine_type":"lingjun.g1xlarge","image_id":"img-lite-1"}]}' ;;
  *) echo '{}' ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "lingjun")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "node.yaml"), []byte(`
resource: lingjun/node
requires_params: [lingjun.hpn_zone, lingjun.machine_type]
requires_prerequisites: [lingjun.cluster]
steps:
  - name: list
    run: ecctl lingjun node list --free --filter hpn-zone={{.params.lingjun.hpn_zone}} --filter machine-type={{.params.lingjun.machine_type}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte(`
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, InputsDir: filepath.Join(dir, "inputs"),
		ParameterPolicy: policy, ParameterMode: "auto", Region: "cn-hangzhou", RunName: "ecctl-e2e-test",
		RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
		Regions: map[string]Region{"primary": {ID: "cn-hangzhou", Prerequisites: map[string]any{
			"lingjun": map[string]any{"cluster": map[string]any{"node_group_ids": []any{"ng-a", "ng-b"}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	lingjun := run.Parameters["lingjun"].(map[string]any)
	if lingjun["cluster_type"] != "Lite" || lingjun["hpn_zone"] != "hpn-a" || lingjun["machine_type"] != "lingjun.g1xlarge" {
		t.Fatalf("resolved Lingjun parameters = %#v", lingjun)
	}
}

func TestRunReusesPreResolvedLingjunParams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *"--limit 100"* ]]; then
  echo '{"error":{"code":"DuplicateInventoryQuery"}}'
  exit 1
fi
echo '{"nodes":[]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	casesDir := filepath.Join(dir, "cases", "lingjun")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "node.yaml"), []byte(`
resource: lingjun/node
requires_params: [lingjun.hpn_zone, lingjun.machine_type]
requires_prerequisites: [lingjun.cluster]
steps:
  - name: list
    run: ecctl lingjun node list --free --filter hpn-zone={{.params.lingjun.hpn_zone}} --filter machine-type={{.params.lingjun.machine_type}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "parameter-policy.yaml")
	if err := os.WriteFile(policy, []byte("ecs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := paramspkg.LingjunResult{
		Region: "cn-hangzhou", ClusterType: "Lite", HPNZone: "hpn-a", Zone: "cn-hangzhou-b",
		MachineType: "lingjun.g1xlarge", ImageID: "img-lite-1",
	}

	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, ParameterPolicy: policy, ParameterMode: "auto", Region: "cn-hangzhou",
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
		PreResolvedLingjun: &resolved,
		Regions: map[string]Region{"primary": {ID: "cn-hangzhou", Prerequisites: map[string]any{
			"lingjun": map[string]any{"cluster": map[string]any{"node_group_ids": []any{"ng-a", "ng-b"}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "--limit 100") {
		t.Fatalf("Lingjun inventory was queried twice:\n%s", log)
	}
	if !strings.Contains(string(log), "--filter hpn-zone=hpn-a --filter machine-type=lingjun.g1xlarge") {
		t.Fatalf("pre-resolved parameters were not rendered:\n%s", log)
	}
}

func TestFatalParameterTextTreatsQuotaAsFatal(t *testing.T) {
	if !fatalParameterText("QuotaExceeded") {
		t.Fatal("quota errors must be fatal and must not trigger region fallback")
	}
	if !fatalParameterText("InternalError") {
		t.Fatal("unknown parameter query errors must be fatal")
	}
}

func TestRunFailsWhenCaseTeardownFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
if [[ "$*" == *" delete "* ]]; then
  echo 'delete failed' >&2
  exit 1
fi
echo '{"resource":{"id":"res-123"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(`
resource: t/thing
steps:
  - name: create
    run: ecctl t thing create
    at: $.resource
    capture:
      id: id
    teardown: ecctl t thing delete {{.id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run, err := Run(context.Background(), Options{
		CasesDir:    filepath.Join(dir, "cases"),
		InputsDir:   filepath.Join(dir, "inputs"),
		RunName:     "ecctl-e2e-test",
		RunID:       "test",
		EcctlBin:    fake,
		StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 1 || run.Cases[0].Status != "fail" {
		t.Fatalf("teardown failure must fail case, got %+v", run)
	}
}

func TestRunPreservesCommandFailureWhenTeardownCaptureIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo '{"error":{"code":"CreateRejected","message":"cloud rejected create"}}'
exit 2
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(`
resource: t/thing
steps:
  - name: create
    run: ecctl t thing create
    at: $.resource
    capture:
      id: id
    teardown: ecctl t thing delete {{.id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run, err := Run(context.Background(), Options{
		CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"),
		RunName: "ecctl-e2e-test", RunID: "test", EcctlBin: fake, StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	step := run.Cases[0].Steps[0]
	if !strings.Contains(step.Error, "CreateRejected") || strings.Contains(step.Error, "render teardown") {
		t.Fatalf("command failure was obscured by teardown rendering: %+v", step)
	}
}

func TestRunPersistsCleanupJournalWhenCreateSucceeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo '{"resource":{"id":"res-123"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "thing.yaml"), []byte(`
resource: t/thing
steps:
  - name: create
    run: ecctl t thing create
    at: $.resource
    capture:
      id: id
    teardown: ecctl t thing delete {{.id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(dir, "reports", "cleanup-journal.json")
	if _, err := Run(context.Background(), Options{
		CasesDir:       filepath.Join(dir, "cases"),
		InputsDir:      filepath.Join(dir, "inputs"),
		CleanupJournal: journalPath,
		RunName:        "ecctl-e2e-test",
		RunID:          "test",
		EcctlBin:       fake,
		StepTimeout:    30 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	if journal.RunID != "test" || len(journal.Entries) != 1 || journal.Entries[0].Teardown != "ecctl t thing delete res-123" {
		t.Fatalf("journal = %s", data)
	}
}

func TestRunExecutesRuntimeFinalizerButPersistsOnlyDeleteCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{"kubeconfig":{"cluster":"c-test","config":"secret"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ack")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "kubeconfig.yaml"), []byte(`
resource: ack/kubeconfig
steps:
  - name: create
    run: ecctl ack kubeconfig create --cluster c-test
    teardown: ecctl ack kubeconfig revoke --cluster c-test
  - name: revoke
    run: ecctl ack kubeconfig revoke --cluster c-test
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	if _, err := Run(context.Background(), Options{
		CasesDir:       filepath.Join(dir, "cases"),
		InputsDir:      filepath.Join(dir, "inputs"),
		CleanupJournal: journalPath,
		RunName:        "ecctl-e2e-test",
		RunID:          "test",
		EcctlBin:       fake,
		StepTimeout:    30 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(log), "ack kubeconfig revoke --cluster c-test"); count != 1 {
		t.Fatalf("explicit cleanup should satisfy the runtime finalizer, got %d calls: %s", count, log)
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Entries) != 0 {
		t.Fatalf("cleanup journal must contain delete commands only: %s", data)
	}
}

func TestRunInjectsRegionRolesAndUsesTeardownRegion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{"image":{"id":"m-copy"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "ecs")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "image-copy.yaml"), []byte(`
resource: ecs/image
requires_prerequisites: [ecs.image]
region_requirements:
  destination:
    requires_prerequisites: [ecs.image]
    distinct_from: primary
steps:
  - name: copy
    run: ecctl ecs image copy {{.prerequisites.ecs.image.image_id}} --bucket {{.prerequisites.ecs.image.oss_bucket}} --destination-region {{.regions.destination.id}}
    at: $.image
    capture: { image_id: id }
    teardown: ecctl ecs image delete {{.image_id}}
    teardown_region: destination
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	journalPath := filepath.Join(dir, "reports", "cleanup-journal.json")

	run, err := Run(context.Background(), Options{
		CasesDir:       filepath.Join(dir, "cases"),
		InputsDir:      filepath.Join(dir, "inputs"),
		CleanupJournal: journalPath,
		ExecutionID:    "execution-01",
		Region:         "cn-hangzhou",
		Regions: map[string]Region{
			"primary": {
				ID: "cn-hangzhou",
				Prerequisites: map[string]any{"ecs": map[string]any{"image": map[string]any{
					"image_id": "m-source", "oss_bucket": "e2e-images",
				}}},
			},
			"destination": {
				ID:            "cn-zhangjiakou",
				Prerequisites: map[string]any{"ecs": map[string]any{"image": map[string]any{"enabled": true}}},
			},
		},
		RunName:     "ecctl-e2e-test",
		RunID:       "test",
		Surface:     "public",
		EcctlBin:    fake,
		StepTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("run failed: %+v", run.Cases)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "ecs image copy m-source --bucket e2e-images --destination-region cn-zhangjiakou --region cn-hangzhou") {
		t.Fatalf("copy command did not use role values: %s", log)
	}
	if !strings.Contains(string(log), "ecs image delete m-copy --region cn-zhangjiakou") {
		t.Fatalf("teardown did not use destination region: %s", log)
	}
	destinationJournal := filepath.Join(dir, "reports", "cleanup-journal-destination.json")
	data, err := os.ReadFile(destinationJournal)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	if journal.ExecutionID != "execution-01" || journal.RegionRole != "destination" || journal.Region != "cn-zhangjiakou" {
		t.Fatalf("destination journal = %s", data)
	}
}
