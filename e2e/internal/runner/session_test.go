package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestSessionReusesRunLifetimeFixturesAndCleansOnceInReverseOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *"parent create"* ]]; then echo '{"resource":{"id":"parent-id"}}'; exit 0; fi
if [[ "$*" == *"child create"* ]]; then echo '{"resource":{"id":"child-id"}}'; exit 0; fi
echo '{}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: parent
    resource: test/parent
    lifetime: run
    run: ecctl test parent create --name {{.resource_prefix}}
    at: $.resource
    capture: { parent_id: id }
    teardown: ecctl test parent delete {{.parent_id}}
  - id: child
    resource: test/child
    lifetime: run
    needs: [parent]
    run: ecctl test child create --parent {{.parent_id}}
    at: $.resource
    capture: { child_id: id }
    teardown: ecctl test child delete {{.child_id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "case.yaml"), []byte(`
resource: ecs/region
needs: [child]
steps:
  - name: list
    run: ecctl ecs region list
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	session := NewSession()
	baseOptions := Options{
		CasesDir: casesDir, StackFile: stack, InputsDir: filepath.Join(dir, "inputs"),
		RunName: "same", RunID: "test", Surface: "public", Region: "cn-test",
		EcctlBin: fake, StepTimeout: time.Second, Session: session,
	}
	for i := 0; i < 2; i++ {
		options := baseOptions
		options.ExecutionID = "execution-" + string(rune('1'+i))
		run, err := Run(context.Background(), options)
		if err != nil || run.Summary.Failed != 0 {
			t.Fatalf("run %d = %+v, err = %v", i+1, run, err)
		}
	}
	beforeClose, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(beforeClose), "test parent create"); count != 1 {
		t.Fatalf("parent create count = %d, log:\n%s", count, beforeClose)
	}
	if count := strings.Count(string(beforeClose), "test child create"); count != 1 {
		t.Fatalf("child create count = %d, log:\n%s", count, beforeClose)
	}
	if strings.Contains(string(beforeClose), " delete ") {
		t.Fatalf("run fixtures cleaned before session close:\n%s", beforeClose)
	}
	if failures := session.Close(); len(failures) != 0 {
		t.Fatalf("close failures = %v", failures)
	}
	afterClose, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(afterClose)
	if count := strings.Count(logText, "test parent delete"); count != 1 {
		t.Fatalf("parent delete count = %d, log:\n%s", count, logText)
	}
	if count := strings.Count(logText, "test child delete"); count != 1 {
		t.Fatalf("child delete count = %d, log:\n%s", count, logText)
	}
	if strings.Index(logText, "test child delete") > strings.Index(logText, "test parent delete") {
		t.Fatalf("cleanup was not reverse dependency order:\n%s", logText)
	}
	if failures := session.Close(); len(failures) != 0 {
		t.Fatalf("second close failures = %v", failures)
	}
}

func TestSessionSkipsMissingFixturePrerequisiteBeforeRender(t *testing.T) {
	session := NewSession()
	operations, err := session.operationRuntime(1)
	if err != nil {
		t.Fatal(err)
	}
	execCfg := execpkg.Config{Bin: "/unused/ecctl", Region: "cn-test"}
	cleanupRegistry := newCleanup(
		map[string]execpkg.Config{"primary": execCfg}, operations, false, "",
		report.CleanupJournal{}, func(string, ...any) {},
	)
	stackVars := map[string]any{}
	stackCase := report.Case{Name: "(shared stack)", Resource: "stack", Status: report.StatusPass}
	failures, err := session.acquire(
		context.Background(),
		Options{Surface: "public", EcctlBin: execCfg.Bin, Region: execCfg.Region, Parallel: 1},
		execCfg,
		cleanupRegistry,
		map[string]any{"prerequisites": map[string]any{}},
		stackVars,
		&Fixture{Provision: []ProvisionStep{{
			ID: "optional", Resource: "test/optional", Lifetime: FixtureLifetimeRun,
			RequiresPrerequisites: []string{"test.optional"},
			Run:                   "ecctl test create {{.prerequisites.test.optional.id}}",
		}}},
		&stackCase,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !stackStepSkipped(failures["optional"]) {
		t.Fatalf("failure = %q, want skipped fixture marker", failures["optional"])
	}
	if len(stackCase.Steps) != 1 || stackCase.Steps[0].Status != report.StatusSkipped {
		t.Fatalf("stack steps = %+v", stackCase.Steps)
	}
	if len(session.entries) != 0 {
		t.Fatalf("skipped fixture created session entries: %#v", session.entries)
	}
}

func TestSessionRejectsRenderedCommandMismatchBeforeSecondMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
echo '{"resource":{"id":"resource-id"}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: shared
    resource: test/shared
    lifetime: run
    run: ecctl test shared create --name {{.resource_prefix}}
    at: $.resource
    capture: { shared_id: id }
    teardown: ecctl test shared delete {{.shared_id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "case.yaml"), []byte(`
resource: ecs/region
needs: [shared]
steps:
  - name: list
    run: ecctl ecs region list
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	session := NewSession()
	options := Options{
		CasesDir: casesDir, StackFile: stack, InputsDir: filepath.Join(dir, "inputs"),
		RunName: "first", RunID: "test", Surface: "public", Region: "cn-test",
		EcctlBin: fake, StepTimeout: time.Second, Session: session,
	}
	if _, err := Run(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	options.RunName = "different"
	if _, err := Run(context.Background(), options); err == nil || !strings.Contains(err.Error(), "rendered command changed") {
		t.Fatalf("error = %v, want command mismatch", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(logData), "test shared create"); count != 1 {
		t.Fatalf("shared create count = %d, log:\n%s", count, logData)
	}
	session.Close()
}

func TestSessionDiscardCleansAssignmentBeforeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_LOG\"\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	cleanupRegistry := newCleanup(
		map[string]execpkg.Config{"primary": {Bin: fake, Region: "cn-first"}},
		newOperationRuntime(1), false, "", report.CleanupJournal{}, func(string, ...any) {},
	)
	var scope []*cleanupItem
	if err := cleanupRegistry.push(&scope, "stack", "ecctl test shared delete shared-id", "primary", nil); err != nil {
		t.Fatal(err)
	}
	session := NewSession()
	key := sessionFixtureKey("public", fake, "cn-first", "shared")
	session.entries[key] = &sessionFixture{
		surface: "public", binary: fake, region: "cn-first", id: "shared",
		cleanup: cleanupRegistry, scope: scope,
	}
	session.order = []string{key}
	if failures := session.Discard("public", fake, "cn-first"); len(failures) != 0 {
		t.Fatalf("discard failures = %v", failures)
	}
	if len(session.entries) != 0 || len(session.order) != 0 {
		t.Fatalf("discard retained assignment entries: %#v %#v", session.entries, session.order)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "test shared delete shared-id") {
		t.Fatalf("discard did not clean assignment:\n%s", logData)
	}
}
