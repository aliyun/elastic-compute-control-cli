package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackInspectCaller(t *testing.T, fake *fakeSpecCaller, wantResource string) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Parent != "inspect" || resource.Resource != wantResource || resource.APIProduct != "CS" {
			t.Fatalf("resource = product:%s parent:%s resource:%s api:%s, want ack/inspect/%s api=CS", resource.Product, resource.Parent, resource.Resource, resource.APIProduct, wantResource)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})
}

func TestACKInspectHelpShowsConfigAndReportChildren(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "inspect", "--help")
	if code != 0 {
		t.Fatalf("ack inspect --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"config", "report"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("inspect help missing %q:\n%s", want, stdout)
		}
	}

	for _, tt := range []struct {
		args []string
		want []string
	}{
		{args: []string{"ack", "inspect", "config", "update", "--help"}, want: []string{"--cluster string", "--enabled", "--schedule string", "--scope string"}},
		{args: []string{"ack", "inspect", "config", "delete", "--help"}, want: []string{"--cluster string"}},
		{args: []string{"ack", "inspect", "report", "create", "--help"}, want: []string{"--cluster string", "--no-wait", "--timeout duration"}},
		{args: []string{"ack", "inspect", "report", "get", "--help"}, want: []string{"get <report-id>", "--cluster string"}},
		{args: []string{"ack", "inspect", "report", "list", "--help"}, want: []string{"--cluster string", "--limit int", "--page int"}},
	} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, tt.args...)...)
		if code != 0 {
			t.Fatalf("%s exit %d stderr=%s stdout=%s", strings.Join(tt.args, " "), code, stderr, stdout)
		}
		for _, want := range tt.want {
			if !strings.Contains(stdout, want) {
				t.Fatalf("%s help missing %q:\n%s", strings.Join(tt.args, " "), want, stdout)
			}
		}
	}
}

func TestACKInspectConfigUpdateDeleteGetRouteToConfigAPIs(t *testing.T) {
	t.Parallel()

	t.Run("update", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{
			{"cluster_id": "c-123", "enabled": false, "schedule": "weekly"},
			{"requestId": "req-update"},
			{"cluster_id": "c-123", "enabled": true, "schedule": "daily", "scope": "all"},
		}}
		runCLI := ackInspectCaller(t, fake, "config")

		stdout, stderr, code := runCLI("ack", "inspect", "config", "update", "--region", "cn-hangzhou", "--cluster", "c-123", "--enabled", "--schedule", "daily", "--scope", "all")
		if code != 0 {
			t.Fatalf("inspect config update exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 3 ||
			fake.calls[0].operation != "GetClusterInspectConfig" ||
			fake.calls[1].operation != "UpdateClusterInspectConfig" ||
			fake.calls[2].operation != "GetClusterInspectConfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		if fake.calls[1].request["cluster_id"] != "c-123" || fake.calls[1].request["body.enabled"] != true {
			t.Fatalf("update request = %#v", fake.calls[1].request)
		}
		config, _ := decodeObject(t, stdout)["config"].(map[string]any)
		if config == nil || config["cluster_id"] != "c-123" || config["enabled"] != true {
			t.Fatalf("unexpected output: %s", stdout)
		}
	})

	t.Run("create when missing", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{
			{"requestId": "req-empty"},
			{"requestId": "req-create"},
			{"cluster_id": "c-123", "enabled": true, "schedule": "daily", "scope": "all"},
		}}
		runCLI := ackInspectCaller(t, fake, "config")

		stdout, stderr, code := runCLI("ack", "inspect", "config", "update", "--region", "cn-hangzhou", "--cluster", "c-123", "--enabled", "--schedule", "daily", "--scope", "all")
		if code != 0 {
			t.Fatalf("inspect config update create path exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 3 ||
			fake.calls[0].operation != "GetClusterInspectConfig" ||
			fake.calls[1].operation != "CreateClusterInspectConfig" ||
			fake.calls[2].operation != "GetClusterInspectConfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		if fake.calls[1].request["cluster_id"] != "c-123" || fake.calls[1].request["body.enabled"] != true {
			t.Fatalf("create request = %#v", fake.calls[1].request)
		}
		config, _ := decodeObject(t, stdout)["config"].(map[string]any)
		if config == nil || config["cluster_id"] != "c-123" || config["enabled"] != true {
			t.Fatalf("unexpected output: %s", stdout)
		}
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{"requestId": "req-delete"}}}
		runCLI := ackInspectCaller(t, fake, "config")

		stdout, stderr, code := runCLI("ack", "inspect", "config", "delete", "--region", "cn-hangzhou", "--cluster", "c-123")
		if code != 0 {
			t.Fatalf("inspect config delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteClusterInspectConfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{"cluster_id": "c-123", "enabled": true}}}
		runCLI := ackInspectCaller(t, fake, "config")

		stdout, stderr, code := runCLI("ack", "inspect", "config", "get", "--region", "cn-hangzhou", "--cluster", "c-123")
		if code != 0 {
			t.Fatalf("inspect config get exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "GetClusterInspectConfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
	})
}

func TestACKInspectReportCreateGetListRouteToReportAPIs(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{
			{"requestId": "req-run", "task_id": "task-123"},
			{"reports": []any{map[string]any{"report_id": "r-1", "status": "finished"}}},
			{"report_id": "r-1", "status": "finished", "details": []any{map[string]any{"name": "node-check"}}},
		}}
		runCLI := ackInspectCaller(t, fake, "report")

		stdout, stderr, code := runCLI("ack", "inspect", "report", "create", "--region", "cn-hangzhou", "--cluster", "c-123")
		if code != 0 {
			t.Fatalf("inspect report create exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 3 ||
			fake.calls[0].operation != "RunClusterInspect" ||
			fake.calls[1].operation != "ListClusterInspectReports" ||
			fake.calls[2].operation != "GetClusterInspectReportDetail" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		if fake.calls[2].request["report_id"] != "r-1" {
			t.Fatalf("detail request = %#v", fake.calls[2].request)
		}
		report, _ := decodeObject(t, stdout)["report"].(map[string]any)
		if report == nil || report["report_id"] != "r-1" || report["details"] == nil {
			t.Fatalf("unexpected output: %s", stdout)
		}
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{"report_id": "r-1", "status": "finished"}}}
		runCLI := ackInspectCaller(t, fake, "report")

		stdout, stderr, code := runCLI("ack", "inspect", "report", "get", "r-1", "--region", "cn-hangzhou", "--cluster", "c-123")
		if code != 0 {
			t.Fatalf("inspect report get exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "GetClusterInspectReportDetail" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		if fake.calls[0].request["report_id"] != "r-1" {
			t.Fatalf("get request = %#v", fake.calls[0].request)
		}
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"reports":   []any{map[string]any{"report_id": "r-1", "status": "finished"}},
			"page_info": map[string]any{"total_count": 1},
		}}}
		runCLI := ackInspectCaller(t, fake, "report")

		stdout, stderr, code := runCLI("ack", "inspect", "report", "list", "--region", "cn-hangzhou", "--cluster", "c-123")
		if code != 0 {
			t.Fatalf("inspect report list exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "ListClusterInspectReports" {
			t.Fatalf("calls = %#v", fake.calls)
		}
	})
}
