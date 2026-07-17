package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFileAcceptsPassingReport(t *testing.T) {
	path := writeReportFixture(t, Run{
		RunID:  "run-1",
		Region: "cn-test",
		Summary: Summary{
			Cases:  1,
			Passed: 1,
			Failed: 0,
		},
		Cases: []Case{{Name: "case", Resource: "ecs/instance", Path: "cases/ecs/instance.yaml", Status: StatusPass}},
	})

	rep, err := CheckFile(path, CheckOptions{Failed: 0})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected valid report, got %+v", rep)
	}
	if rep.Cases != 1 || rep.Failed != 0 {
		t.Fatalf("unexpected counts: %+v", rep)
	}
}

func TestCheckFileRejectsFailedOrErroredReport(t *testing.T) {
	tests := []struct {
		name string
		run  Run
		code string
	}{
		{
			name: "too many failed",
			run: Run{
				Summary: Summary{Cases: 1, Failed: 1},
				Cases:   []Case{{Name: "case", Status: StatusFail}},
			},
			code: "too_many_failed",
		},
		{
			name: "case error",
			run: Run{
				Summary: Summary{Cases: 1, Failed: 0},
				Cases:   []Case{{Name: "case", Status: StatusError}},
			},
			code: "case_not_passed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeReportFixture(t, tt.run)
			rep, err := CheckFile(path, CheckOptions{Failed: 0})
			if err != nil {
				t.Fatal(err)
			}
			if !hasReportCheckCode(rep, tt.code) {
				t.Fatalf("expected %q, got %+v", tt.code, rep.Errors)
			}
		})
	}
}

func TestRedactScrubsRegionAttemptReasons(t *testing.T) {
	run := Run{
		RegionAttempts: []RegionAttempt{{Reason: "AccessKeyId=ak Secret=sk token=abc"}},
		Executions: []Execution{{
			Attempts: []ExecutionAttempt{{Reason: "token=execution-secret"}},
			Cases:    []Case{{Steps: []Step{{Command: "ecctl ecs list --password nested-secret"}}}},
		}},
	}
	run.Redact()
	if run.RegionAttempts[0].Reason != "AccessKeyId=*** Secret=*** token=***" {
		t.Fatalf("region attempt reason was not redacted: %q", run.RegionAttempts[0].Reason)
	}
	if run.Executions[0].Attempts[0].Reason != "token=***" {
		t.Fatalf("execution attempt reason was not redacted: %q", run.Executions[0].Attempts[0].Reason)
	}
	if run.Executions[0].Cases[0].Steps[0].Command != "ecctl ecs list --password ***" {
		t.Fatalf("nested execution command was not redacted: %q", run.Executions[0].Cases[0].Steps[0].Command)
	}
}

func TestRedactRemovesKubeconfigMaterialFromAllReportFormats(t *testing.T) {
	const secret = "VERY-SECRET-CLIENT-KEY"
	secretOutput := `{"kubeconfig":{"config":"apiVersion: v1\\nusers:\\n- user:\\n    client-key-data: ` + secret + `"}}`
	failedCase := Case{
		Name:     "ack/kubeconfig",
		Resource: "ack/kubeconfig",
		Status:   StatusFail,
		Steps: []Step{{
			Name:   "get kubeconfig",
			Status: StatusFail,
			Stdout: secretOutput,
			Checks: []Check{{Path: "kubeconfig.config", Detail: "client-key-data: " + secret}},
		}},
	}
	run := Run{
		Summary: Summary{Cases: 1, Failed: 1},
		Cases:   []Case{failedCase},
		Executions: []Execution{{
			ID:      "execution-01",
			Cases:   []Case{failedCase},
			Summary: Summary{Cases: 1, Failed: 1},
		}},
	}

	run.Redact()
	if got := run.Cases[0].Steps[0].Stdout; strings.Contains(got, secret) || !strings.Contains(got, `"config":"***"`) {
		t.Fatalf("top-level kubeconfig output was not structurally redacted: %q", got)
	}
	if got := run.Executions[0].Cases[0].Steps[0].Stdout; strings.Contains(got, secret) {
		t.Fatalf("execution kubeconfig output was not redacted: %q", got)
	}

	dir := t.TempDir()
	paths := []struct {
		name  string
		write func(*Run, string) error
	}{
		{name: "report.json", write: WriteJSON},
		{name: "report.html", write: WriteHTML},
		{name: "report.xml", write: WriteJUnit},
	}
	for _, artifact := range paths {
		path := filepath.Join(dir, artifact.name)
		if err := artifact.write(&run, path); err != nil {
			t.Fatalf("write %s: %v", artifact.name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("%s leaked kubeconfig material", artifact.name)
		}
	}
}

func writeReportFixture(t *testing.T, run Run) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "e2e-report.json")
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasReportCheckCode(rep *CheckReport, code string) bool {
	for _, err := range rep.Errors {
		if err.Code == code {
			return true
		}
	}
	return false
}
