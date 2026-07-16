package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func fakeCommandListResponse(id string, name string) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe-commands",
		"TotalCount": 1,
		"Commands": map[string]any{"Command": []any{
			map[string]any{
				"CommandId":      id,
				"Name":           name,
				"Type":           "RunShellScript",
				"CommandContent": "uptime",
				"WorkingDir":     "/root",
				"Timeout":        float64(60),
				"CreationTime":   "2026-05-27T10:00:00Z",
			},
		}},
	}
}

func fakeInvocationListResponse(invokeID string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe-invocations",
		"TotalCount": 1,
		"Invocations": map[string]any{"Invocation": []any{
			map[string]any{
				"InvokeId":     invokeID,
				"CommandId":    "c-cmd1",
				"CommandName":  "uptime",
				"CommandType":  "RunShellScript",
				"InvokeStatus": status,
				"Timed":        false,
				"CreationTime": "2026-05-27T10:00:00Z",
			},
		}},
	}
}

func fakeInvocationResultsResponse(invokeID string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-invocation-results",
		"TotalCount": 1,
		"Invocation": map[string]any{
			"InvocationResults": map[string]any{
				"InvocationResult": []any{
					map[string]any{
						"InvokeId":         invokeID,
						"CommandId":        "c-cmd1",
						"InstanceId":       "i-123",
						"InvocationStatus": status,
						"ExitCode":         float64(0),
						"Output":           "dGVzdA==",
						"StartTime":        "2026-05-27T10:00:00Z",
						"FinishedTime":     "2026-05-27T10:00:05Z",
					},
				},
			},
		},
	}
}

func commandCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "command" {
			t.Fatalf("resource = %s/%s, want ecs/command", resource.Product, resource.Resource)
		}
		return fake, nil
	})
}

func TestECSCommandCreateInvokesCreateCommand(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "CommandId": "c-cmd1"},
			fakeCommandListResponse("c-cmd1", "uptime"),
		},
	}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "create",
		"--region", "cn-beijing",
		"--name", "uptime",
		"--type", "RunShellScript",
		"--command-content", "uptime",
	)
	if code != 0 {
		t.Fatalf("ecs command create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateCommand" || fake.calls[1].operation != "DescribeCommands" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"Name":           "uptime",
		"Type":           "RunShellScript",
		"CommandContent": "uptime",
	} {
		if got := request[key]; got != want {
			t.Fatalf("CreateCommand request[%s] = %#v, want %#v", key, got, want)
		}
	}
	if _, ok := request["ClientToken"]; !ok {
		t.Fatalf("CreateCommand must use idempotency ClientToken: %#v", request)
	}
	cmd, _ := decodeObject(t, stdout)["command"].(map[string]any)
	if cmd == nil || cmd["id"] != "c-cmd1" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSCommandUpdateRoutesTemplateAttributes(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-modify"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "update", "c-cmd1",
		"--region", "cn-beijing",
		"--name", "renamed",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs command update template exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyCommand" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["CommandId"] != "c-cmd1" || fake.calls[0].request["Name"] != "renamed" {
		t.Fatalf("ModifyCommand request = %#v", fake.calls[0].request)
	}
}

func TestECSCommandUpdateInvocationRoutesInvocationAttribute(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-modify-invocation"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "update",
		"--region", "cn-beijing",
		"--invocation-id", "t-inv-1",
		"--frequency", "0 0 * * *",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs command update invocation exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyInvocationAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InvokeId"] != "t-inv-1" || fake.calls[0].request["Frequency"] != "0 0 * * *" {
		t.Fatalf("ModifyInvocationAttribute request = %#v", fake.calls[0].request)
	}
}

func TestECSCommandUpdateRejectsTemplateMixedWithInvocation(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "update", "c-cmd1",
		"--region", "cn-beijing",
		"--invocation-id", "t-inv-1",
		"--name", "should-fail",
		"--no-wait",
	)
	if code == 0 {
		t.Fatalf("ecs command update template+invocation succeeded; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
}

func TestECSCommandDeleteInvokesDeleteCommand(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "command", "delete", "c-cmd1", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs command delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteCommand" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["CommandId"] != "c-cmd1" {
		t.Fatalf("DeleteCommand request = %#v", fake.calls[0].request)
	}
}

func TestECSCommandInvokeUsesInvokeCommand(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-invoke", "InvokeId": "t-inv-1"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "invoke", "c-cmd1",
		"--region", "cn-beijing",
		"--instance-ids", `["i-123"]`,
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs command invoke exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "InvokeCommand" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["CommandId"] != "c-cmd1" {
		t.Fatalf("InvokeCommand CommandId = %#v; request=%#v", request["CommandId"], request)
	}
	// InstanceId expanded to InstanceId.N by the `each:` binding.
	found := false
	for k, v := range request {
		if len(k) >= len("InstanceId") && k[:len("InstanceId")] == "InstanceId" && v == "i-123" {
			found = true
		}
	}
	if !found {
		t.Fatalf("InvokeCommand request must contain InstanceId.N = i-123: %#v", request)
	}
	if _, ok := request["ClientToken"]; !ok {
		t.Fatalf("InvokeCommand must use ClientToken idempotency: %#v", request)
	}
	out := decodeObject(t, stdout)
	if out["invoke_id"] != "t-inv-1" {
		t.Fatalf("invoke output missing invoke_id: %s", stdout)
	}
}

func TestECSCommandStopUsesStopInvocation(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-stop"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "stop", "t-inv-1",
		"--region", "cn-beijing",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs command stop exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "StopInvocation" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InvokeId"] != "t-inv-1" {
		t.Fatalf("StopInvocation request = %#v", request)
	}
	if got, ok := request["Force"]; ok && got == true {
		t.Fatalf("StopInvocation Force must default to false; request=%#v", request)
	}
}

func TestECSCommandStopForceIsOptIn(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-stop"}}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "stop", "t-inv-1",
		"--region", "cn-beijing",
		"--force",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs command stop --force exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "StopInvocation" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["Force"] != true {
		t.Fatalf("StopInvocation --force should propagate Force=true: %#v", fake.calls[0].request)
	}
}

func TestECSCommandListSwitchesToInvocationsOnFlag(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeInvocationListResponse("t-inv-1", "Running")}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "command", "list", "--region", "cn-beijing", "--invocations")
	if code != 0 {
		t.Fatalf("ecs command list --invocations exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeInvocations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["MaxResults"] != 50 {
		t.Fatalf("DescribeInvocations request = %#v", fake.calls[0].request)
	}
	if _, ok := fake.calls[0].request["NextToken"]; ok {
		t.Fatalf("first DescribeInvocations page must omit NextToken: %#v", fake.calls[0].request)
	}
}

func TestECSCommandListDefaultsToDescribeCommands(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeCommandListResponse("c-cmd1", "uptime")}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "command", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs command list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeCommands" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["MaxResults"] != 50 {
		t.Fatalf("DescribeCommands request = %#v", fake.calls[0].request)
	}
	if _, ok := fake.calls[0].request["NextToken"]; ok {
		t.Fatalf("first DescribeCommands page must omit NextToken: %#v", fake.calls[0].request)
	}
}

func TestECSCommandGetWithInvocationIDQueriesInvocation(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeInvocationListResponse("t-inv-1", "Finished")}}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "get",
		"--region", "cn-beijing",
		"--invocation-id", "t-inv-1",
	)
	if code != 0 {
		t.Fatalf("ecs command get --invocation-id exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeInvocations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSCommandGetWithResultsAlsoFetchesInvocationResult(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeInvocationListResponse("t-inv-1", "Finished"),
			fakeInvocationResultsResponse("t-inv-1", "Success"),
		},
	}
	runCLI := commandCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "command", "get",
		"--region", "cn-beijing",
		"--invocation-id", "t-inv-1",
		"--with-results",
	)
	if code != 0 {
		t.Fatalf("ecs command get --with-results exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeInvocations" || fake.calls[1].operation != "DescribeInvocationResults" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}
