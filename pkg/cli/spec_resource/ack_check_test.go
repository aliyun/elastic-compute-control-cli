package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackCheckCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, res spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if res.Product != "ack" || res.Resource != "check" || res.APIProduct != "CS" {
			t.Fatalf("resource = %#v, want ack/check with CS API product", res)
		}
		return fake, nil
	})
}

func fakeACKCheckResponse(id string, status string) map[string]any {
	return map[string]any{
		"request_id":  "req-get",
		"check_id":    id,
		"type":        "ClusterUpgrade",
		"status":      status,
		"message":     "task succeed",
		"created_at":  "2026-06-01T02:56:02Z",
		"finished_at": "2026-06-01T02:56:18Z",
	}
}

func TestACKCheckCreateRunsClusterCheckAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-run", "check_id": "chk-1"},
		fakeACKCheckResponse("chk-1", "Succeeded"),
	}}
	runCLI := ackCheckCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "check", "create", "--region", "cn-hangzhou", "--cluster", "ce-1", "--type", "ClusterUpgrade", "--timeout", "1s")
	if code != 0 {
		t.Fatalf("ack check create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "RunClusterCheck" || fake.calls[1].operation != "GetClusterCheck" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	runRequest := fake.calls[0].request
	if runRequest["cluster_id"] != "ce-1" || runRequest["body.type"] != "ClusterUpgrade" {
		t.Fatalf("RunClusterCheck request = %#v", runRequest)
	}
	getRequest := fake.calls[1].request
	if getRequest["cluster_id"] != "ce-1" || getRequest["check_id"] != "chk-1" {
		t.Fatalf("GetClusterCheck request = %#v", getRequest)
	}
	check, _ := decodeObject(t, stdout)["check"].(map[string]any)
	if check == nil || check["check_id"] != "chk-1" || check["status"] != "Succeeded" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKCheckCreateNoWaitReturnsCheckIDWithoutReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-run", "check_id": "chk-1"},
	}}
	runCLI := ackCheckCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "check", "create", "--region", "cn-hangzhou", "--cluster", "ce-1", "--type", "ClusterUpgrade", "--no-wait")
	if code != 0 {
		t.Fatalf("ack check create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunClusterCheck" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	check, _ := decodeObject(t, stdout)["check"].(map[string]any)
	if check == nil || check["check_id"] != "chk-1" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKCheckGetUsesPositionalCheckID(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKCheckResponse("chk-1", "Succeeded")}}
	runCLI := ackCheckCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "check", "get", "chk-1", "--region", "cn-hangzhou", "--cluster", "ce-1")
	if code != 0 {
		t.Fatalf("ack check get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetClusterCheck" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["cluster_id"] != "ce-1" || request["check_id"] != "chk-1" {
		t.Fatalf("GetClusterCheck request = %#v", request)
	}
	check, _ := decodeObject(t, stdout)["check"].(map[string]any)
	if check == nil || check["check_id"] != "chk-1" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKCheckListUsesPaginationAndFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-list",
		"checks": []any{
			fakeACKCheckResponse("chk-1", "Succeeded"),
		},
	}}}
	runCLI := ackCheckCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "check", "list",
		"--region", "cn-hangzhou",
		"--cluster", "ce-1",
		"--filter", "type=ClusterUpgrade",
		"--filter", "target=np-1",
		"--limit", "20",
		"--page", "2",
	)
	if code != 0 {
		t.Fatalf("ack check list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListClusterChecks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["cluster_id"] != "ce-1" || request["type"] != "ClusterUpgrade" || request["target"] != "np-1" {
		t.Fatalf("ListClusterChecks filters not mapped: %#v", request)
	}
	if request["PageSize"] != 20 || request["PageNumber"] != 2 {
		t.Fatalf("ListClusterChecks pagination not mapped: %#v", request)
	}
	out := decodeObject(t, stdout)
	checks, _ := out["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("checks = %#v; stdout=%s", checks, stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil || pagination["limit"] != float64(20) || pagination["page"] != float64(2) {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
}

func TestACKCheckHelpExposesCreateControlsAndNoRunAction(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("ack", "check", "create", "--help")
	if code != 0 {
		t.Fatalf("ack check create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster string", "--type string", "--no-wait", "--timeout duration"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("ack", "check", "--help")
	if code != 0 {
		t.Fatalf("ack check --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\n  run ") || strings.Contains(stdout, "\n  run\t") {
		t.Fatalf("ack check help should not list run action:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("ack", "check", "run")
	if code == 0 {
		t.Fatalf("ack check run should not be exposed; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "UnknownCommand" {
		t.Fatalf("ack check run error.code = %q, want UnknownCommand; stdout=%s", got, stdout)
	}
}
