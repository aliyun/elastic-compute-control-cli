package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestDAGRunsIndependentStepsConcurrentlyAndHonorsLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	for _, test := range []struct {
		name     string
		parallel int
		locks    string
		overlap  bool
	}{
		{name: "independent", parallel: 2, overlap: true},
		{name: "operation semaphore", parallel: 1},
		{name: "same rendered lock", parallel: 2, locks: "    locks: [shared]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
if mkdir "$FAKE_ACTIVE" 2>/dev/null; then
  sleep 0.25
  rmdir "$FAKE_ACTIVE"
else
  echo overlap >> "$FAKE_OVERLAP"
  sleep 0.25
fi
echo '{}'
`), 0o755); err != nil {
				t.Fatal(err)
			}
			casesDir := filepath.Join(dir, "cases")
			if err := os.MkdirAll(casesDir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := "resource: ecs/region\nexecution: dag\nsteps:\n" +
				"  - name: one\n" + test.locks + "    run: ecctl ecs region list\n" +
				"  - name: two\n" + test.locks + "    run: ecctl ecs region list\n"
			if err := os.WriteFile(filepath.Join(casesDir, "dag.yaml"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			overlapPath := filepath.Join(dir, "overlap.log")
			t.Setenv("FAKE_ACTIVE", filepath.Join(dir, "active"))
			t.Setenv("FAKE_OVERLAP", overlapPath)
			run, err := Run(context.Background(), Options{
				CasesDir: casesDir, InputsDir: filepath.Join(dir, "inputs"),
				RunName: "test", RunID: "test", EcctlBin: fake, Parallel: test.parallel, StepTimeout: 5 * time.Second,
			})
			if err != nil || run.Summary.Failed != 0 {
				t.Fatalf("run = %+v, err = %v", run, err)
			}
			_, overlapErr := os.Stat(overlapPath)
			gotOverlap := overlapErr == nil
			if gotOverlap != test.overlap {
				t.Fatalf("overlap = %t, want %t", gotOverlap, test.overlap)
			}
		})
	}
}

func TestDAGDependentStartsBeforeUnrelatedRootCompletes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
if [[ "$*" == *"--fast-root"* ]]; then
  echo fast-start >> "$FAKE_LOG"
  sleep 0.05
  echo fast-end >> "$FAKE_LOG"
elif [[ "$*" == *"--slow-root"* ]]; then
  echo slow-start >> "$FAKE_LOG"
  sleep 0.5
  echo slow-end >> "$FAKE_LOG"
elif [[ "$*" == *"--dependent"* ]]; then
  echo dependent-start >> "$FAKE_LOG"
fi
echo '{}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "dag.yaml"), []byte(`
resource: ecs/region
execution: dag
steps:
  - name: fast root
    run: ecctl ecs region list --fast-root
  - name: slow root
    run: ecctl ecs region list --slow-root
  - name: dependent
    depends_on: [fast root]
    run: ecctl ecs region list --dependent
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, InputsDir: filepath.Join(dir, "inputs"),
		RunName: "test", RunID: "test", EcctlBin: fake, Parallel: 3, StepTimeout: 2 * time.Second,
	})
	if err != nil || run.Summary.Failed != 0 {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	dependentStart := strings.Index(logText, "dependent-start")
	slowEnd := strings.Index(logText, "slow-end")
	if dependentStart < 0 || slowEnd < 0 || dependentStart > slowEnd {
		t.Fatalf("dependent did not start before unrelated root completed:\n%s", logText)
	}
	wantOrder := []string{"fast root", "slow root", "dependent"}
	for index, want := range wantOrder {
		if got := run.Cases[0].Steps[index].Name; got != want {
			t.Fatalf("reported step %d = %q, want YAML order %q", index, got, want)
		}
	}
}

func TestDAGFailureAndMissingPrerequisiteOnlyBlockDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	for _, test := range []struct {
		name       string
		steps      string
		wantStatus string
		wantLog    []string
		absentLog  []string
	}{
		{
			name: "failed predecessor",
			steps: `
  - name: failing
    run: ecctl ecs region list --fail
  - name: blocked
    depends_on: [failing]
    run: ecctl ecs region list --blocked
  - name: independent
    run: ecctl ecs region list --independent
`,
			wantStatus: "fail",
			wantLog:    []string{"--fail", "--independent"},
			absentLog:  []string{"--blocked"},
		},
		{
			name: "missing optional prerequisite",
			steps: `
  - name: ordinary
    run: ecctl ecs region list --ordinary
  - name: renew
    requires_prerequisites: [test.optional]
    run: ecctl ecs region list --renew
  - name: renew readback
    depends_on: [renew]
    run: ecctl ecs region list --renewed
`,
			wantStatus: "pass",
			wantLog:    []string{"--ordinary"},
			absentLog:  []string{"--renew", "--renewed"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *"--fail"* ]]; then exit 1; fi
echo '{}'
`), 0o755); err != nil {
				t.Fatal(err)
			}
			casesDir := filepath.Join(dir, "cases")
			if err := os.MkdirAll(casesDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(casesDir, "dag.yaml"), []byte("resource: ecs/region\nexecution: dag\nsteps:\n"+test.steps), 0o644); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "calls.log")
			t.Setenv("FAKE_LOG", logPath)
			run, err := Run(context.Background(), Options{
				CasesDir: casesDir, InputsDir: filepath.Join(dir, "inputs"),
				RunName: "test", RunID: "test", EcctlBin: fake, Parallel: 2, StepTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := run.Cases[0].Status; got != test.wantStatus {
				t.Fatalf("case status = %q, want %q: %+v", got, test.wantStatus, run.Cases[0])
			}
			logData, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			logText := string(logData)
			for _, expected := range test.wantLog {
				if !strings.Contains(logText, expected) {
					t.Fatalf("log missing %q: %s", expected, logText)
				}
			}
			for _, absent := range test.absentLog {
				if strings.Contains(logText, absent) {
					t.Fatalf("log unexpectedly contains %q: %s", absent, logText)
				}
			}
		})
	}
}

func TestDAGFixtureFailureSkipsOnlyConsumers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *"stack create"* ]]; then exit 1; fi
echo '{}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: broken
    resource: test/stack
    run: ecctl test stack create
`), 0o644); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "dag.yaml"), []byte(`
resource: ecs/region
execution: dag
steps:
  - name: consumer
    needs: [broken]
    run: ecctl ecs region list --consumer
  - name: independent
    run: ecctl ecs region list --independent
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, StackFile: stack, InputsDir: filepath.Join(dir, "inputs"),
		RunName: "test", RunID: "test", EcctlBin: fake, Parallel: 2, StepTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := run.Cases[len(run.Cases)-1].Status; got != "pass" {
		t.Fatalf("DAG case status = %q, want pass: %+v", got, run.Cases)
	}
	steps := run.Cases[len(run.Cases)-1].Steps
	if steps[0].Status != "skipped" || steps[1].Status != "pass" {
		t.Fatalf("steps = %+v", steps)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logData), "--consumer") || !strings.Contains(string(logData), "--independent") {
		t.Fatalf("unexpected calls:\n%s", logData)
	}
}

func TestDAGMissingFixturePrerequisiteSkipsOnlyConsumersBeforeRender(t *testing.T) {
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
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte(`
provision:
  - id: optional
    resource: test/stack
    requires_prerequisites: [test.optional]
    run: ecctl test stack create --id {{.prerequisites.test.optional.id}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "dag.yaml"), []byte(`
resource: ecs/region
execution: dag
steps:
  - name: consumer
    needs: [optional]
    run: ecctl ecs region list --consumer
  - name: descendant
    depends_on: [consumer]
    run: ecctl ecs region list --descendant
  - name: independent
    run: ecctl ecs region list --independent
`), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	run, err := Run(context.Background(), Options{
		CasesDir: casesDir, StackFile: stack, InputsDir: filepath.Join(dir, "inputs"),
		RunName: "test", RunID: "test", EcctlBin: fake, Parallel: 2, StepTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 0 {
		t.Fatalf("missing optional fixture prerequisite failed run: %+v", run.Cases)
	}
	if len(run.Cases) != 2 || len(run.Cases[0].Steps) != 1 || run.Cases[0].Steps[0].Status != report.StatusSkipped {
		t.Fatalf("shared stack result = %+v", run.Cases)
	}
	caseSteps := run.Cases[1].Steps
	if len(caseSteps) != 3 || caseSteps[0].Status != report.StatusSkipped || caseSteps[1].Status != report.StatusSkipped || caseSteps[2].Status != report.StatusPass {
		t.Fatalf("case steps = %+v", caseSteps)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if strings.Contains(logText, "stack create") || strings.Contains(logText, "--consumer") || strings.Contains(logText, "--descendant") || !strings.Contains(logText, "--independent") {
		t.Fatalf("unexpected calls:\n%s", logText)
	}
}
