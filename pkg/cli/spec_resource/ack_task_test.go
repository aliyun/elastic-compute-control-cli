package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackTaskCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "task" {
			t.Fatalf("resource = %s/%s, want ack/task", resource.Product, resource.Resource)
		}
		if resource.APIProduct != "CS" {
			t.Fatalf("api_product = %q, want CS", resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func fakeACKTaskInfoResponse(id string, state string) map[string]any {
	return map[string]any{
		"task_id":    id,
		"cluster_id": "c-123",
		"task_type":  "cluster_upgrade",
		"state":      state,
		"created":    "2026-06-01T10:00:00+08:00",
		"updated":    "2026-06-01T10:01:00+08:00",
	}
}

func fakeACKTaskListResponse(id string, state string) map[string]any {
	return map[string]any{
		"requestId": "req-list",
		"page_info": map[string]any{
			"page_size":   float64(100),
			"page_number": float64(1),
			"total_count": float64(1),
		},
		"tasks": []any{fakeACKTaskInfoResponse(id, state)},
	}
}

func TestACKTaskHelpShapeUsesTaskActions(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "task", "--help")
	if code != 0 {
		t.Fatalf("ack task --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, action := range []string{"get", "list", "pause", "resume", "cancel"} {
		if !strings.Contains(stdout, "\n  "+action+" ") {
			t.Fatalf("ack task help missing %s action: %s", action, stdout)
		}
	}
	for _, unsupported := range []string{"create", "update", "delete"} {
		if strings.Contains(stdout, "\n  "+unsupported+" ") {
			t.Fatalf("ack task help should not expose %s: %s", unsupported, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "task", "get", "--help")
	if code != 0 {
		t.Fatalf("ack task get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"Usage:\n  ecctl ack task get <task-id>", "--cluster string"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack task get help missing %q: %s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "task", "list", "--help")
	if code != 0 {
		t.Fatalf("ack task list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"Usage:\n  ecctl ack task list", "--cluster string", "--filter stringArray", "status", "type"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack task list help missing %q: %s", want, stdout)
		}
	}

	for _, action := range []string{"pause", "resume", "cancel"} {
		stdout, stderr, code = runCLI("--lang", "en", "ack", "task", action, "--help")
		if code != 0 {
			t.Fatalf("ack task %s --help exit %d stderr=%s stdout=%s", action, code, stderr, stdout)
		}
		if !strings.Contains(stdout, "Usage:\n  ecctl ack task "+action+" <task-id>") {
			t.Fatalf("ack task %s help should use task-id positional: %s", action, stdout)
		}
	}
}

func TestACKTaskGetRoutesToDescribeTaskInfo(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKTaskInfoResponse("T-123", "running")}}
	runCLI := ackTaskCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "task", "get", "T-123", "--cluster", "c-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack task get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeTaskInfo" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["task_id"] != "T-123" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[0].request)
	}
	task, _ := decodeObject(t, stdout)["task"].(map[string]any)
	if task == nil || task["id"] != "T-123" || task["status"] != "running" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKTaskListRoutesToDescribeClusterTasksWithFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKTaskListResponse("T-123", "running")}}
	runCLI := ackTaskCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "task", "list",
		"--cluster", "c-123",
		"--filter", "status=running",
		"--filter", "type=cluster_upgrade",
		"--region", "cn-beijing",
	)
	if code != 0 {
		t.Fatalf("ack task list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterTasks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"cluster_id":  "c-123",
		"state":       "running",
		"task_type":   "cluster_upgrade",
		"page_size":   100,
		"page_number": 1,
	} {
		if got := request[key]; got != want {
			t.Fatalf("DescribeClusterTasks request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(1) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKTaskPauseResumeCancelRouteToTaskAPIsAndReadBack(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action     string
		api        string
		finalState string
	}{
		{action: "pause", api: "PauseTask", finalState: "paused"},
		{action: "resume", api: "ResumeTask", finalState: "running"},
		{action: "cancel", api: "CancelTask", finalState: "canceled"},
	} {
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{
				responses: []map[string]any{
					{"requestId": "req-" + tc.action},
					fakeACKTaskInfoResponse("T-123", tc.finalState),
				},
			}
			runCLI := ackTaskCaller(t, fake)

			stdout, stderr, code := runCLI("ack", "task", tc.action, "T-123", "--region", "cn-beijing")
			if code != 0 {
				t.Fatalf("ack task %s exit %d stderr=%s stdout=%s", tc.action, code, stderr, stdout)
			}
			if len(fake.calls) != 2 || fake.calls[0].operation != tc.api || fake.calls[1].operation != "DescribeTaskInfo" {
				t.Fatalf("calls = %#v", fake.calls)
			}
			if fake.calls[0].request["task_id"] != "T-123" || fake.calls[1].request["task_id"] != "T-123" {
				t.Fatalf("%s requests = %#v", tc.action, fake.calls)
			}
			task, _ := decodeObject(t, stdout)["task"].(map[string]any)
			if task == nil || task["status"] != tc.finalState {
				t.Fatalf("unexpected output: %s", stdout)
			}
		})
	}
}
