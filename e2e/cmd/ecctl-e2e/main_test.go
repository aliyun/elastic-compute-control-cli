package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/coverage"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/prereqcheck"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestCoverageRegistryCheckAndSummary(t *testing.T) {
	root, specs, cases := writeCLIFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	capabilities := filepath.Join(root, "capabilities.json")
	mustWriteFile(t, capabilities, `{
  "surface": "public",
  "products": [{
    "product": "ecs",
    "resources": [{"name": "instance", "actions": ["create", "delete", "list"]}]
  }]
}`)
	runCLI(t,
		"registry", "init",
		"--specs", specs,
		"--cases", cases,
		"--registry", registry,
		"--capabilities", capabilities,
	)
	reg, err := coverage.LoadRegistryFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	wantPublic := coverage.RegistryPublicSummary{Surface: "public", Resources: 1, Operations: 3, MissingCases: 1, NotPassed: 2}
	if reg.Summary != wantPublic {
		t.Fatalf("public summary = %+v, want %+v", reg.Summary, wantPublic)
	}

	out := runCLI(t,
		"registry", "check",
		"--specs", specs,
		"--cases", cases,
		"--registry", registry,
		"--capabilities", capabilities,
	)
	if !strings.Contains(out, "registry: 2 operations, 0 live-pass, 2 offline, 0 invalid") {
		t.Fatalf("unexpected check output:\n%s", out)
	}

	out = runCLI(t,
		"registry", "summary",
		"--registry", registry,
		"--output", "json",
	)
	var summary struct {
		Entries  int            `json:"entries"`
		ByStatus map[string]int `json:"by_status"`
	}
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("summary is not JSON: %v\n%s", err, out)
	}
	if summary.Entries != 2 || summary.ByStatus["offline"] != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestCoverageRegistryInitRequiresPublicCapabilities(t *testing.T) {
	root, specs, cases := writeCLIFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"registry", "init",
		"--specs", specs,
		"--cases", cases,
		"--registry", registry,
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "requires --ecctl-bin or --capabilities") {
		t.Fatalf("init without public capabilities error = %v, output=%s", err, out.String())
	}

	fullCapabilities := filepath.Join(root, "full-capabilities.json")
	mustWriteFile(t, fullCapabilities, `{"surface":"full","products":[{"product":"ecs","resources":[]}]}`)
	cmd = coverageCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"registry", "init",
		"--specs", specs,
		"--cases", cases,
		"--registry", registry,
		"--capabilities", fullCapabilities,
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), `does not match requested "public"`) {
		t.Fatalf("init with full capabilities error = %v, output=%s", err, out.String())
	}
}

func TestCoverageCommandFailsWhenAResourceHasNoCase(t *testing.T) {
	_, specs, cases := writeCLIFixture(t)
	mustWriteFile(t, filepath.Join(specs, "disk.yaml"), `
product: ecs
resource: disk
operations:
  list: {}
`)

	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--specs", specs,
		"--cases", cases,
		"--fail-on-resource-gap",
	})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitTestsFailed {
		t.Fatalf("expected exitTestsFailed, got %T %[1]v", err)
	}
	if !strings.Contains(out.String(), "RESOURCE GAP  ecs/disk") {
		t.Fatalf("expected resource gap in output, got:\n%s", out.String())
	}
}

func TestCapabilityFilterRequiresSurfaceMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(path, []byte(`{"products":[{"product":"ecs","resources":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCapabilitySelection(path, "", "public"); err == nil || !strings.Contains(err.Error(), "surface") {
		t.Fatalf("err = %v, want missing-surface validation", err)
	}
}

func TestCapabilitySelectionCountsOnlyResourcesWithActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(path, []byte(`{
	  "surface": "public",
	  "products": [{
	    "product": "ecs",
	    "resources": [
	      {"name": "instance", "actions": ["list"]},
	      {"name": "empty", "actions": []}
	    ]
	  }]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	selection, err := loadCapabilitySelection(path, "", "public")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ResourceCount != 1 || len(selection.Filter) != 1 {
		t.Fatalf("selection = %+v, want one actionable resource and operation", selection)
	}
}

func TestRunCollectOnlyFiltersBySurface(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "public.yaml"), `
surface: public
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)
	mustWriteFile(t, filepath.Join(cases, "ecs", "full.yaml"), `
surface: full
resource: ecs/assistant
steps:
  - name: list
    run: ecctl ecs assistant get
`)

	cmd := runCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--cases", cases, "--collect-only", "--quiet", "--surface", "public"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("collect public surface: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "public.yaml") || strings.Contains(out.String(), "full.yaml") {
		t.Fatalf("surface selection output = %q", out.String())
	}
}

func TestRunCollectOnlyJSONIncludesFixtureDependencyClosure(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	stack := filepath.Join(root, "fixtures", "stack.yaml")
	mustMkdirAll(t, filepath.Join(cases, "ack"))
	mustMkdirAll(t, filepath.Dir(stack))
	mustWriteFile(t, filepath.Join(cases, "ack", "diagnosis.yaml"), `
surface: full
resource: ack/diagnosis
execution: dag
covers:
  - ack/check-item
needs:
  - ack_diagnosis_node
steps:
  - name: create
    needs: [ack_diagnosis_node]
    requires_prerequisites: [ack.auto_repair_policy]
    locks: ["ack-cluster:c"]
    run: ecctl ack diagnosis create --cluster c --type node --target n
`)
	mustWriteFile(t, stack, `
provision:
  - id: cluster
    resource: ack/ack
    lifetime: run
    run: ecctl ack create --name c
  - id: ack_diagnosis_node
    resource: ack/node
    lifetime: run
    needs: [cluster]
    run: ecctl ack node add --cluster c --instances i
`)

	cmd := runCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--cases", cases,
		"--stack", stack,
		"--surface", "full",
		"--collect-only",
		"--output", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("collect fixture dependencies: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		`"covers": [`,
		`"ack/check-item"`,
		`"direct_needs": [`,
		`"ack_diagnosis_node"`,
		`"id": "cluster"`,
		`"resource": "ack/ack"`,
		`"id": "ack_diagnosis_node"`,
		`"resource": "ack/node"`,
		`"mode": "create"`,
		`"lifetime": "run"`,
		`"execution": "dag"`,
		`"requires_prerequisites": [`,
		`"ack.auto_repair_policy"`,
		`"locks": [`,
		`"ack-cluster:c"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("collect output missing %q:\n%s", want, got)
		}
	}
}

func TestRunAlwaysUsesFixedResourceNamePrefix(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	config := filepath.Join(root, "e2e.yaml")
	fake := filepath.Join(root, "ecctl")
	logPath := filepath.Join(root, "calls.log")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "list.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list --filter name={{.run_name}}
`)
	mustWriteFile(t, config, `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
values:
  cleanup.name_prefix:
    value: should-not-be-used
paths:
  cases: `+cases+`
`)
	mustWriteFile(t, fake, `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"public","products":[{"product":"ecs","resources":[{"name":"instance","actions":["list"]}]}]}'
else
  printf '%s' '{"instances":[]}'
fi
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)

	cmd := runCmd()
	cmd.SetArgs([]string{
		"--config", config,
		"--ecctl-bin", fake,
		"--report-dir", filepath.Join(root, "reports"),
		"--label", "fixed",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	log := string(mustReadFile(t, logPath))
	if !strings.Contains(log, "--filter name=ecctl-e2e-fixed") {
		t.Fatalf("fixed name prefix was not used:\n%s", log)
	}
	if strings.Contains(log, "should-not-be-used") {
		t.Fatalf("legacy cleanup.name_prefix still affected the run:\n%s", log)
	}
}

func TestRunRejectsBinaryWithWrongSurface(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"full","products":[{"product":"ecs","resources":[{"name":"instance","actions":["list"]}]}]}'
else
  printf '%s' '{}'
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := filepath.Join(t.TempDir(), "cases")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "public.yaml"), `
surface: public
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)
	cmd := runCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--cases", cases, "--ecctl-bin", fake, "--surface", "public"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `reports surface "full", want "public"`) {
		t.Fatalf("err = %v, want binary surface mismatch", err)
	}
}

func TestRunRejectsMissingParameterRequirementBeforeExecution(t *testing.T) {
	cases := filepath.Join(t.TempDir(), "cases")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "broken.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list --filter type={{.params.ecs.typo}}
`)
	cmd := runCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--cases", cases, "--collect-only"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing_parameter_requirement") {
		t.Fatalf("err = %v, want missing parameter requirement", err)
	}
}

func TestRunRejectsExplicitMissingFixturePath(t *testing.T) {
	cases := filepath.Join(t.TempDir(), "cases")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "list.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)
	cmd := runCmd()
	cmd.SetArgs([]string{"--cases", cases, "--collect-only", "--stack", filepath.Join(t.TempDir(), "missing-stack.yaml")})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "stack file") {
		t.Fatalf("err = %v, want explicit missing stack failure", err)
	}
}

func TestRunFallsBackToNextConfiguredRegion(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	inputs := filepath.Join(root, "inputs")
	stack := filepath.Join(root, "stack.yaml")
	config := filepath.Join(root, "e2e.yaml")
	fake := filepath.Join(root, "ecctl")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustMkdirAll(t, inputs)
	mustWriteFile(t, filepath.Join(cases, "ecs", "list.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)
	mustWriteFile(t, stack, "provision: []\n")
	mustWriteFile(t, config, "version: 2\nregions:\n  candidates:\n    - id: cn-first\n    - id: cn-second\npaths:\n  cases: "+cases+"\n  stack: "+stack+"\n  inputs: "+inputs+"\n")
	mustWriteFile(t, fake, `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"public","products":[{"product":"ecs","resources":[{"name":"instance","actions":["list"]}]}]}'
  exit 0
fi
case "$*" in
  *"--region cn-first"*) printf '%s' '{"error":{"code":"InvalidRegionId","message":"unsupported"}}'; exit 1 ;;
  *) printf '%s' '{}' ;;
esac
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(root, "reports")
	cmd := runCmd()
	cmd.SetArgs([]string{
		"--config", config,
		"--ecctl-bin", fake,
		"--surface", "public",
		"--report-dir", reports,
		"--label", "region-fallback",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run with region fallback: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(reports, "e2e-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Region         string           `json:"region"`
		RegionAttempts []map[string]any `json:"region_attempts"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Region != "cn-second" || len(got.RegionAttempts) != 2 {
		t.Fatalf("report region=%q attempts=%v", got.Region, got.RegionAttempts)
	}
}

func TestRunRejectsMissingRegionalPrerequisiteBeforeCaseExecution(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	config := filepath.Join(root, "e2e.yaml")
	fake := filepath.Join(root, "ecctl")
	logPath := filepath.Join(root, "calls.log")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "image.yaml"), `
resource: ecs/image
requires_prerequisites: [future.bundle]
steps:
  - name: list
    run: ecctl ecs image list --filter name={{.prerequisites.future.bundle.name}}
`)
	mustWriteFile(t, config, `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
paths:
  cases: `+cases+`
`)
	mustWriteFile(t, fake, `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"public","products":[{"product":"ecs","resources":[{"name":"image","actions":["list"]}]}]}'
else
  printf '%s' '{}'
fi
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)

	cmd := runCmd()
	cmd.SetArgs([]string{"--config", config, "--ecctl-bin", fake, "--surface", "public"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "future.bundle") {
		t.Fatalf("err = %v, want missing prerequisite planning failure", err)
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if calls := strings.TrimSpace(string(data)); calls != "capabilities --output json" {
		t.Fatalf("unexpected cloud or case calls before planning failure: %q", calls)
	}
}

func TestRunSkipsCasesWithUnavailableProbedPrerequisites(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	config := filepath.Join(root, "e2e.yaml")
	policy := filepath.Join(root, "parameter-policy.yaml")
	fake := filepath.Join(root, "ecctl")
	logPath := filepath.Join(root, "calls.log")
	reports := filepath.Join(root, "reports")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustMkdirAll(t, filepath.Join(cases, "lingjun"))
	mustWriteFile(t, filepath.Join(cases, "lingjun", "cluster.yaml"), `
resource: lingjun/cluster
requires_prerequisites: [lingjun.cluster]
steps:
  - name: list
    run: ecctl lingjun cluster list
`)
	mustWriteFile(t, filepath.Join(cases, "ecs", "instance.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)
	mustWriteFile(t, policy, "ecs: {}\n")
	mustWriteFile(t, config, `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
      prerequisites:
        lingjun.cluster:
          node_group_ids: [ng-a, ng-b]
paths:
  cases: `+cases+`
  parameter_policy: `+policy+`
`)
	mustWriteFile(t, fake, `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
case "$*" in
  "capabilities"*) printf '%s' '{"surface":"public","products":[{"product":"ecs","resources":[{"name":"image","actions":["list"]},{"name":"instance","actions":["list"]}]},{"product":"lingjun","resources":[{"name":"cluster","actions":["list"]}]}]}' ;;
  *"ListFreeNodes"*) printf '%s' '{"response":{"Nodes":[{"NodeGroupId":"ng-a","HpnZone":"hpn-a","ZoneId":"cn-hangzhou-b","MachineType":"lingjun.g1xlarge"}]}}' ;;
  *"ecs instance list"*) printf '%s' '{"instances":[]}' ;;
  *) printf '%s' '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)

	cmd := runCmd()
	cmd.SetArgs([]string{"--config", config, "--ecctl-bin", fake, "--report-dir", reports, "--label", "prerequisite-skip"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run with unavailable optional prerequisites: %v", err)
	}
	log := string(mustReadFile(t, logPath))
	if count := strings.Count(log, "ListFreeNodes"); count != 1 {
		t.Fatalf("Lingjun free-node query count = %d, want only the preflight query:\n%s", count, log)
	}
	if !strings.Contains(log, "ecs instance list") {
		t.Fatalf("unrelated case did not run:\n%s", log)
	}
	reportData := string(mustReadFile(t, filepath.Join(reports, "e2e-report.json")))
	if !strings.Contains(reportData, `"passed": 1`) || !strings.Contains(reportData, `"skipped": 1`) ||
		!strings.Contains(reportData, "lingjun.cluster") {
		t.Fatalf("report does not describe prerequisite skips: %s", reportData)
	}
}

func TestPreflightIncludesDeclarativePrerequisites(t *testing.T) {
	suites := []*scenario.Suite{{
		RequiresPrerequisites: []string{
			prereqcheck.ACKAutoRepairPolicy,
			prereqcheck.RGNotificationDisabled,
			"future.bundle",
		},
	}}
	required := probedPrerequisites(suites)
	if !required[prereqcheck.ACKAutoRepairPolicy] || !required[prereqcheck.RGNotificationDisabled] {
		t.Fatalf("declarative prerequisites missing from preflight: %#v", required)
	}
	if required["future.bundle"] {
		t.Fatalf("unowned prerequisites unexpectedly added to preflight: %#v", required)
	}
}

func TestRunDoesNotExposeDeprecatedBillingOptInFlag(t *testing.T) {
	if flag := runCmd().Flags().Lookup("allow-billing-mutations"); flag != nil {
		t.Fatalf("deprecated billing opt-in flag is still registered: %s", flag.Name)
	}
}

func TestPartitionSkipsDeclarativePrerequisiteRemovedByPreflight(t *testing.T) {
	suite := &scenario.Suite{
		Path:                  "cases/rg/notification-lifecycle.yaml",
		RequiresPrerequisites: []string{prereqcheck.RGNotificationDisabled},
	}
	profiles := []fixtureconfig.RegionProfile{{ID: "cn-hangzhou"}}
	runnable, skipped := partitionUnavailablePrerequisiteSuites(
		[]*scenario.Suite{suite}, profiles, "cn-hangzhou",
	)
	if len(runnable) != 0 || len(skipped) != 1 ||
		!strings.Contains(skipped[0].Reason, prereqcheck.RGNotificationDisabled) {
		t.Fatalf("runnable=%#v skipped=%#v, want declarative case skipped", runnable, skipped)
	}
}

func TestRunTreatsPrerequisiteProbePermissionErrorAsFatal(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	config := filepath.Join(root, "e2e.yaml")
	fake := filepath.Join(root, "ecctl")
	logPath := filepath.Join(root, "calls.log")
	mustMkdirAll(t, filepath.Join(cases, "lingjun"))
	mustWriteFile(t, filepath.Join(cases, "lingjun", "cluster.yaml"), `
resource: lingjun/cluster
requires_prerequisites: [lingjun.cluster]
steps:
  - name: list
    run: ecctl lingjun cluster list
`)
	mustWriteFile(t, config, `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
      prerequisites:
        lingjun.cluster:
          node_group_ids: [ng-a, ng-b]
paths:
  cases: `+cases+`
`)
	mustWriteFile(t, fake, `#!/bin/sh
echo "$*" >> "$FAKE_LOG"
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"public","products":[{"product":"lingjun","resources":[{"name":"cluster","actions":["list"]}]}]}'
  exit 0
fi
printf '%s' '{"error":{"code":"NoPermission","message":"not authorized"}}'
exit 1
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)

	cmd := runCmd()
	cmd.SetArgs([]string{"--config", config, "--ecctl-bin", fake, "--label", "prerequisite-permission"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "NoPermission") {
		t.Fatalf("err = %v, want fatal prerequisite permission error", err)
	}
	if log := string(mustReadFile(t, logPath)); strings.Count(log, "ListFreeNodes") > 1 {
		t.Fatalf("case ran after fatal prerequisite probe error:\n%s", log)
	}
}

func TestRunFallsBackWhenDynamicZoneQueryRejectsRegion(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	inputs := filepath.Join(root, "inputs")
	policy := filepath.Join(root, "parameter-policy.yaml")
	config := filepath.Join(root, "e2e.yaml")
	fake := filepath.Join(root, "ecctl")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustMkdirAll(t, inputs)
	mustWriteFile(t, filepath.Join(cases, "ecs", "list.yaml"), `
resource: ecs/instance
requires_params: [ecs.zone]
steps:
  - name: list
    run: ecctl ecs instance list --zone {{.params.ecs.zone}}
`)
	mustWriteFile(t, policy, `
ecs:
  cores: [1]
  image_family: acs:alibaba_cloud_linux_3_2104_lts_x64
  image_owner_alias: system
`)
	mustWriteFile(t, config, "version: 2\nregions:\n  candidates:\n    - id: cn-first\n    - id: cn-second\npaths:\n  cases: "+cases+"\n  inputs: "+inputs+"\n  parameter_policy: "+policy+"\n")
	mustWriteFile(t, fake, `#!/bin/sh
if [ "$1" = "capabilities" ]; then
  printf '%s' '{"surface":"public","products":[{"product":"ecs","resources":[{"name":"instance","actions":["list"]}]}]}'
  exit 0
fi
case "$*" in
  *"ecs zone list --verbose"*"--region cn-first"*) printf '%s' '{"error":{"code":"InvalidRegionId","message":"unsupported"}}'; exit 1 ;;
  *"ecs zone list --verbose"*) printf '%s' '{"zones":[{"id":"cn-second-b","available_resource_creation":["VSwitch","Instance"]}]}'; exit 0 ;;
  *"DescribeAvailableResource"*"--region cn-first"*) printf '%s' '{"error":{"code":"InvalidRegionId","message":"unsupported"}}'; exit 1 ;;
  *"DestinationResource SystemDisk"*) printf '%s' '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-second-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"SystemDisk","SupportedResources":{"SupportedResource":[{"Value":"cloud_efficiency","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}'; exit 0 ;;
  *"DestinationResource DataDisk"*) printf '%s' '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-second-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"DataDisk","SupportedResources":{"SupportedResource":[{"Value":"cloud_efficiency","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}'; exit 0 ;;
  *"DescribeAvailableResource"*) printf '%s' '{"response":{"AvailableZones":{"AvailableZone":[{"ZoneId":"cn-second-b","Status":"Available","StatusCategory":"WithStock","AvailableResources":{"AvailableResource":[{"Type":"InstanceType","SupportedResources":{"SupportedResource":[{"Value":"ecs.g7.large","Status":"Available","StatusCategory":"WithStock"}]}}]}}]}}}'; exit 0 ;;
  *"DescribeImages"*) printf '%s' '{"response":{"Images":{"Image":[{"ImageId":"img-1"}]}}}'; exit 0 ;;
  *) printf '%s' '{}' ;;
esac
`)
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(root, "reports")
	cmd := runCmd()
	cmd.SetArgs([]string{"--config", config, "--ecctl-bin", fake, "--surface", "public", "--report-dir", reports, "--label", "dynamic-region-fallback"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run with dynamic region fallback: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(reports, "e2e-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Region         string           `json:"region"`
		RegionAttempts []map[string]any `json:"region_attempts"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Region != "cn-second" || len(got.RegionAttempts) != 2 {
		t.Fatalf("report region=%q attempts=%v", got.Region, got.RegionAttempts)
	}
}

func TestCoverageRegistryCheckReturnsExitErrorOnInvalidRegistry(t *testing.T) {
	root, specs, cases := writeCLIFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWriteFile(t, registry, `
version: 3
resources:
  ecs:
    instance:
      operations:
        create:
          status: maybe
        delete:
          status: offline
          case: cases/ecs/instance.yaml
          fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
          time: "2026-07-15T00:00:00Z"
          reason: not-run
        list:
          status: missing
`)

	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"registry", "check",
		"--specs", specs,
		"--cases", cases,
		"--registry", registry,
	})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitTestsFailed {
		t.Fatalf("expected exitTestsFailed, got %T %[1]v", err)
	}
	if !strings.Contains(out.String(), "unknown_status") {
		t.Fatalf("expected machine-readable error code in output, got:\n%s", out.String())
	}
}

func TestCoverageRegistryCheckHelpDoesNotExposeTierFlags(t *testing.T) {
	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"registry", "check",
		"--help",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, removed := range []string{"--tier", "--tiers", "--allow-stale", "--fail-on-missing", "--fail-on-stale"} {
		if strings.Contains(got, removed) {
			t.Fatalf("coverage registry check must not expose %s:\n%s", removed, got)
		}
	}
}

func TestCoverageRegistryCheckHelpDoesNotExposeLiveEvidenceFlag(t *testing.T) {
	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"registry", "check",
		"--help",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "--require-live-evidence") {
		t.Fatalf("coverage registry check must not expose live evidence flag:\n%s", out.String())
	}
}

func TestCaseLintCommand(t *testing.T) {
	root, cases, inputs, coveragePath := writeLintCLIFixture(t, validLintCLICase(), validLintCLICoverage("cases/ecs/instance.yaml"))

	out := runLintCLI(t,
		"cases",
		"--cases", cases,
		"--inputs", inputs,
		"--coverage", coveragePath,
	)
	if !strings.Contains(out, "lint: 1 cases, 2 steps, 0 invalid") {
		t.Fatalf("unexpected lint output for %s:\n%s", root, out)
	}
}

func TestCaseLintCommandReturnsExitErrorOnInvalidCases(t *testing.T) {
	_, cases, inputs, coveragePath := writeLintCLIFixture(t, validLintCLICase(), validLintCLICoverage("cases/ecs/missing.yaml"))
	cmd := lintCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"cases",
		"--cases", cases,
		"--inputs", inputs,
		"--coverage", coveragePath,
	})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitTestsFailed {
		t.Fatalf("expected exitTestsFailed, got %T %[1]v", err)
	}
	if !strings.Contains(out.String(), "coverage_case_missing") {
		t.Fatalf("expected machine-readable error code in output, got:\n%s", out.String())
	}
}

func TestSweepCheckCommand(t *testing.T) {
	_, cases, config := writeSweepCLIFixture(t, true)

	out := runSweepCLI(t,
		"check",
		"--cases", cases,
		"--config", config,
	)
	if !strings.Contains(out, "sweep check: 1 cases, 1 sweep kinds, 1 live creates, 0 invalid") {
		t.Fatalf("unexpected sweep check output:\n%s", out)
	}
}

func TestSweepCheckCommandReturnsExitErrorOnInvalidConfig(t *testing.T) {
	_, cases, config := writeSweepCLIFixture(t, false)
	cmd := sweepCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"check",
		"--cases", cases,
		"--config", config,
	})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitTestsFailed {
		t.Fatalf("expected exitTestsFailed, got %T %[1]v", err)
	}
	if !strings.Contains(out.String(), "missing_sweep_kind") {
		t.Fatalf("expected machine-readable error code in output, got:\n%s", out.String())
	}
}

func TestReportCheckCommand(t *testing.T) {
	path := writeReportCLIFixture(t, `{"summary":{"cases":1,"passed":1,"failed":0},"cases":[{"name":"case","status":"pass"}]}`)
	out := runReportCLI(t, "check", path, "--failed", "0")
	if !strings.Contains(out, "report: 1 cases, 0 failed, 0 invalid") {
		t.Fatalf("unexpected report check output:\n%s", out)
	}
}

func TestReportCheckCommandReturnsExitErrorOnFailedReport(t *testing.T) {
	path := writeReportCLIFixture(t, `{"summary":{"cases":1,"passed":0,"failed":1},"cases":[{"name":"case","status":"fail"}]}`)
	cmd := reportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"check", path, "--failed", "0"})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitTestsFailed {
		t.Fatalf("expected exitTestsFailed, got %T %[1]v", err)
	}
	if !strings.Contains(out.String(), "too_many_failed") {
		t.Fatalf("expected too_many_failed in output, got:\n%s", out.String())
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := coverageCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func runReportCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := reportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func runSweepCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := sweepCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func runLintCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := lintCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func writeCLIFixture(t *testing.T) (root, specs, cases string) {
	t.Helper()
	root = t.TempDir()
	specs = filepath.Join(root, "specs")
	cases = filepath.Join(root, "cases")
	mustMkdirAll(t, filepath.Join(specs, "ecs"))
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(specs, "ecs", "instance.yaml"), `
product: ecs
resource: instance
operations:
  create: {}
  delete: {}
  list: {}
`)
	mustWriteFile(t, filepath.Join(cases, "ecs", "instance.yaml"), `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --name x
  - name: delete
    run: ecctl ecs instance delete i-1
`)
	return root, specs, cases
}

func writeLintCLIFixture(t *testing.T, caseBody, coverage string) (root, cases, inputs, coveragePath string) {
	t.Helper()
	root = t.TempDir()
	cases = filepath.Join(root, "cases")
	inputs = filepath.Join(root, "fixtures", "inputs")
	coveragePath = filepath.Join(root, "coverage.yaml")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustMkdirAll(t, inputs)
	mustWriteFile(t, filepath.Join(cases, "ecs", "instance.yaml"), caseBody)
	mustWriteFile(t, filepath.Join(inputs, "ecs-instance.yaml"), "type: ecs.g6.large\n")
	mustWriteFile(t, coveragePath, coverage)
	return root, cases, inputs, coveragePath
}

func validLintCLICase() string {
	return `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --type {{.inputs.type}} --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id
    teardown: ecctl ecs instance delete {{.instance_id}}
  - name: delete
    run: ecctl ecs instance delete {{.instance_id}}
`
}

func validLintCLICoverage(casePath string) string {
	return `
version: 3
resources:
  ecs:
    instance:
      operations:
        create:
          status: offline
          case: ` + casePath + `
          fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
          time: "2026-07-15T00:00:00Z"
          reason: not-run
        delete:
          status: offline
          case: ` + casePath + `
          fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
          time: "2026-07-15T00:00:00Z"
          reason: not-run
`
}

func writeSweepCLIFixture(t *testing.T, includeKind bool) (root, cases, config string) {
	t.Helper()
	root = t.TempDir()
	cases = filepath.Join(root, "cases")
	config = filepath.Join(root, "sweep.yaml")
	mustMkdirAll(t, filepath.Join(cases, "ecs"))
	mustWriteFile(t, filepath.Join(cases, "ecs", "instance.yaml"), `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --tag ecctl-e2e=1 --tag run-id={{.run_id}}
    capture:
      instance_id: id
    teardown: ecctl ecs instance delete {{.instance_id}} --force
`)
	if includeKind {
		mustWriteFile(t, config, `
kinds:
  - name: ecs-instance
    list: ecctl ecs instance list --filter tag.ecctl-e2e=1
    items_path: $.instances
    id_field: id
    runid_field: tags.run-id
    created_field: creation_time
    delete: "ecctl ecs instance delete {{.id}} --force"
`)
	} else {
		mustWriteFile(t, config, `
kinds:
  - name: ecs-disk
    list: ecctl ecs disk list --filter tag.ecctl-e2e=1
    items_path: $.disks
    id_field: id
    runid_field: tags.run-id
    created_field: creation_time
    delete: "ecctl ecs disk delete {{.id}}"
`)
	}
	return root, cases, config
}

func writeReportCLIFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "e2e-report.json")
	mustWriteFile(t, path, body)
	return path
}

func mustMkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var _ *cobra.Command
