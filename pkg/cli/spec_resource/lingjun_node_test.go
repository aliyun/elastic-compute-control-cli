package spec_resource

import (
	"reflect"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func lingjunNodeCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "node" || resource.APIProduct != "eflo-controller" {
			t.Fatalf("resource = %s/%s api=%s, want lingjun/node api eflo-controller", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func fakeLingjunNode(id string) map[string]any {
	return map[string]any{
		"RequestId":       "req-node",
		"NodeId":          id,
		"Hostname":        "train-01",
		"ClusterId":       "cluster-123",
		"ClusterName":     "train-cluster",
		"MachineType":     "efg1.nvga1",
		"NodeGroupId":     "ng-123",
		"NodeGroupName":   "workers",
		"OperatingState":  "Using",
		"ResourceGroupId": "rg-123",
		"ZoneId":          "cn-beijing-a",
		"Networks":        []any{map[string]any{"Ip": "10.0.0.10"}},
		"Disks":           []any{map[string]any{"DiskName": "system"}},
	}
}

func TestLingjunNodeGetExtractsDescribeNode(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeLingjunNode("node-123")}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "node", "get", "node-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("lingjun node get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeNode" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["NodeId"] != "node-123" {
		t.Fatalf("DescribeNode request = %#v", fake.calls[0].request)
	}
	node, _ := decodeObject(t, stdout)["node"].(map[string]any)
	if node == nil || node["id"] != "node-123" || node["hostname"] != "train-01" || node["cluster"] != "cluster-123" || node["machine_type"] != "efg1.nvga1" || node["status"] != "Using" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunNodeListSelectsHyperAPIAndPaginates(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"NextToken":  "next-2",
		"HyperNodes": []any{map[string]any{"HyperNodeId": "hyper-123", "Hostname": "hyper-01", "Status": "Using"}},
	}}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node", "list",
		"--region", "cn-beijing",
		"--hyper",
		"--filter", "hpn-zone=A1",
		"--filter", "machine-type=efg1.nvga1",
		"--filter", "resource-group=rg-123",
		"--limit", "50",
		"--next-token", "next-1",
	)
	if code != 0 {
		t.Fatalf("lingjun node list --hyper exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListHyperNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	for key, want := range map[string]any{
		"HpnZone":         "A1",
		"MachineType":     "efg1.nvga1",
		"ResourceGroupId": "rg-123",
		"MaxResults":      50,
		"NextToken":       "next-1",
	} {
		if !reflect.DeepEqual(req[key], want) {
			t.Fatalf("ListHyperNodes request[%s] = %#v, want %#v; request=%#v", key, req[key], want, req)
		}
	}
	out := decodeObject(t, stdout)
	nodes, _ := out["nodes"].([]any)
	pagination, _ := out["pagination"].(map[string]any)
	if len(nodes) != 1 || pagination["next_token"] != "next-2" {
		t.Fatalf("unexpected list output: %s", stdout)
	}
}

func TestLingjunNodeListSelectsFreeAndFreeHyperAPIs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		args      []string
		operation string
		want      map[string]any
		response  map[string]any
	}{
		{
			name:      "free",
			args:      []string{"lingjun", "node", "list", "--region", "cn-beijing", "--free", "--filter", "hpn-zone=A1", "--filter", "machine-type=efg1.nvga1", "--filter", "state=Unused"},
			operation: "ListFreeNodes",
			want: map[string]any{
				"HpnZone":           "A1",
				"MachineType":       "efg1.nvga1",
				"OperatingStates.1": "Unused",
			},
			response: map[string]any{"RequestId": "req-free", "Nodes": []any{fakeLingjunNode("node-free")}},
		},
		{
			name:      "free hyper",
			args:      []string{"lingjun", "node", "list", "--region", "cn-beijing", "--free", "--hyper", "--filter", "hpn-zone=A1", "--filter", "machine-type=efg1.nvga1", "--filter", "status=Using"},
			operation: "ListFreeHyperNodes",
			want: map[string]any{
				"HpnZone":     "A1",
				"MachineType": "efg1.nvga1",
				"Status.1":    "Using",
			},
			response: map[string]any{"RequestId": "req-hyper", "HyperNodes": []any{map[string]any{"HyperNodeId": "hyper-123", "Hostname": "hyper-01", "Status": "Using"}}},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{tt.response}}
			runCLI := lingjunNodeCaller(t, fake)

			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("lingjun node list exit %d stderr=%s stdout=%s", code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			for key, want := range tt.want {
				if !reflect.DeepEqual(fake.calls[0].request[key], want) {
					t.Fatalf("%s request[%s] = %#v, want %#v; request=%#v", tt.operation, key, fake.calls[0].request[key], want, fake.calls[0].request)
				}
			}
		})
	}
}

func TestLingjunNodeListValidatesFreeOrHyperRequired(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "free or hyper required", args: []string{"lingjun", "node", "list", "--region", "cn-beijing"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{}
			runCLI := lingjunNodeCaller(t, fake)

			stdout, stderr, code := runCLI(tt.args...)
			if code == 0 {
				t.Fatalf("expected validation failure, stdout=%s stderr=%s", stdout, stderr)
			}
			if len(fake.calls) != 0 {
				t.Fatalf("validation should fail before API calls: %#v", fake.calls)
			}
		})
	}
}

func TestLingjunNodeDeleteSelectsDefaultAndHyperAPIs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		args      []string
		operation string
		field     string
		wantIDs   []string
	}{
		{
			name:      "default",
			args:      []string{"lingjun", "node", "delete", "node-1", "node-2", "--region", "cn-beijing"},
			operation: "DeleteNode",
			field:     "NodeId",
			wantIDs:   []string{"node-1", "node-2"},
		},
		{
			name:      "hyper",
			args:      []string{"lingjun", "node", "delete", "hyper-1", "hyper-2", "--region", "cn-beijing", "--hyper"},
			operation: "DeleteHyperNode",
			field:     "HyperNodeId",
			wantIDs:   []string{"hyper-1", "hyper-2"},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-1"}, {"RequestId": "req-2"}}}
			runCLI := lingjunNodeCaller(t, fake)

			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("lingjun node delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
			}
			if len(fake.calls) != 2 {
				t.Fatalf("calls = %#v", fake.calls)
			}
			for i, call := range fake.calls {
				if call.operation != tt.operation || call.request[tt.field] != tt.wantIDs[i] {
					t.Fatalf("call[%d] = %#v, want %s %s=%s", i, call, tt.operation, tt.field, tt.wantIDs[i])
				}
			}
		})
	}
}

func TestLingjunNodeRebootAndStopMapNodeIDs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		action    string
		operation string
		extraArgs []string
		wantExtra map[string]any
	}{
		{
			action:    "reboot",
			operation: "RebootNodes",
			extraArgs: []string{"--cluster", "cluster-123", "--ignore-failed-node-tasks"},
			wantExtra: map[string]any{"ClusterId": "cluster-123", "IgnoreFailedNodeTasks": true},
		},
		{
			action:    "stop",
			operation: "StopNodes",
			extraArgs: []string{"--ignore-failed-node-tasks"},
			wantExtra: map[string]any{"IgnoreFailedNodeTasks": true},
		},
	} {
		tt := tt
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			taskID := "task-" + tt.action
			fake := &fakeSpecCaller{responses: []map[string]any{
				{"RequestId": "req-" + tt.action, "TaskId": taskID},
				{"RequestId": "req-task", "TaskId": taskID, "TaskState": "execution_success", "TaskType": tt.operation},
			}}
			runCLI := lingjunNodeCaller(t, fake)
			args := append([]string{"lingjun", "node", tt.action, "node-1", "node-2", "--region", "cn-beijing"}, tt.extraArgs...)

			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("lingjun node %s exit %d stderr=%s stdout=%s", tt.action, code, stderr, stdout)
			}
			if len(fake.calls) != 2 || fake.calls[0].operation != tt.operation || fake.calls[1].operation != "DescribeTask" {
				t.Fatalf("calls = %#v", fake.calls)
			}
			req := fake.calls[0].request
			if !reflect.DeepEqual(req["Nodes"], []string{"node-1", "node-2"}) {
				t.Fatalf("%s request = %#v", tt.operation, req)
			}
			for key, want := range tt.wantExtra {
				if !reflect.DeepEqual(req[key], want) {
					t.Fatalf("%s request[%s] = %#v, want %#v; request=%#v", tt.operation, key, req[key], want, req)
				}
			}
			if fake.calls[1].request["TaskId"] != taskID {
				t.Fatalf("DescribeTask request = %#v", fake.calls[1].request)
			}
		})
	}
}

func TestLingjunNodeRebootAndStopNoWaitSkipsTaskReadback(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		action    string
		operation string
	}{
		{action: "reboot", operation: "RebootNodes"},
		{action: "stop", operation: "StopNodes"},
	} {
		tt := tt
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			taskID := "task-" + tt.action
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-" + tt.action, "TaskId": taskID}}}
			runCLI := lingjunNodeCaller(t, fake)

			stdout, stderr, code := runCLI("lingjun", "node", tt.action, "node-1", "--region", "cn-beijing", "--no-wait")
			if code != 0 {
				t.Fatalf("lingjun node %s --no-wait exit %d stderr=%s stdout=%s", tt.action, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			node, _ := decodeObject(t, stdout)["node"].(map[string]any)
			if node == nil || node["task_id"] != taskID {
				t.Fatalf("unexpected output: %s", stdout)
			}
		})
	}
}

func fakeLingjunInvocationResponse(invokeID string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-invocations",
		"TotalCount": 1,
		"Invocations": map[string]any{"Invocation": []any{
			map[string]any{
				"InvokeId":         invokeID,
				"InvocationStatus": status,
				"Name":             "exec",
				"Content":          "echo ok",
			},
		}},
	}
}

func TestLingjunNodeExecMapsRunCommandAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-run", "InvokeId": "invoke-123"},
		fakeLingjunInvocationResponse("invoke-123", "Success"),
	}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node", "exec",
		"node-1", "node-2",
		"--region", "cn-beijing",
		"--command", "echo ok",
		"--username", "root",
		"--working-dir", "/tmp",
		"--timeout-seconds", "120",
		"--timeout", "30s",
	)
	if code != 0 {
		t.Fatalf("lingjun node exec exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "RunCommand" || fake.calls[1].operation != "DescribeInvocations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	for key, want := range map[string]any{
		"NodeIdList":     []string{"node-1", "node-2"},
		"CommandContent": "echo ok",
		"Username":       "root",
		"WorkingDir":     "/tmp",
		"Timeout":        120,
	} {
		if !reflect.DeepEqual(req[key], want) {
			t.Fatalf("RunCommand request[%s] = %#v, want %#v; request=%#v", key, req[key], want, req)
		}
	}
	if _, ok := req["ClientToken"]; !ok {
		t.Fatalf("RunCommand must receive ClientToken: %#v", req)
	}
	readback := fake.calls[1].request
	if readback["InvokeId"] != "invoke-123" || readback["IncludeOutput"] != true {
		t.Fatalf("DescribeInvocations request = %#v", readback)
	}
}

func TestLingjunNodeExecDefaultWaitsForCompletion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-run", "InvokeId": "invoke-123"},
		fakeLingjunInvocationResponse("invoke-123", "Running"),
	}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node", "exec",
		"node-1",
		"--region", "cn-beijing",
		"--command", "echo ok",
		"--timeout", "1ms",
	)
	if code == 0 {
		t.Fatalf("lingjun node exec should wait for completion and time out; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error code = %q, want WaitTimeout; stdout=%s stderr=%s", got, stdout, stderr)
	}
	// The 1ms timeout can elapse after one or more polls, so assert at least one
	// DescribeInvocations poll rather than an exact call count.
	if len(fake.calls) < 2 || fake.calls[0].operation != "RunCommand" || fake.calls[1].operation != "DescribeInvocations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestLingjunNodeSendfileMapsSendFileAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-send", "InvokeId": "invoke-123"},
		fakeLingjunInvocationResponse("invoke-123", "Success"),
	}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node", "sendfile",
		"node-1",
		"--region", "cn-beijing",
		"--content", "hello",
		"--name", "hello.txt",
		"--target", "/tmp",
		"--overwrite",
		"--timeout-seconds", "60",
		"--timeout", "30s",
	)
	if code != 0 {
		t.Fatalf("lingjun node sendfile exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "SendFile" || fake.calls[1].operation != "DescribeSendFileResults" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	for key, want := range map[string]any{
		"NodeIdList": []string{"node-1"},
		"Content":    "hello",
		"Name":       "hello.txt",
		"TargetDir":  "/tmp",
		"Overwrite":  true,
		"Timeout":    60,
	} {
		if !reflect.DeepEqual(req[key], want) {
			t.Fatalf("SendFile request[%s] = %#v, want %#v; request=%#v", key, req[key], want, req)
		}
	}
	if fake.calls[1].request["InvokeId"] != "invoke-123" {
		t.Fatalf("DescribeSendFileResults request = %#v", fake.calls[1].request)
	}
}

func TestLingjunNodeSendfileDefaultWaitsForCompletion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-send", "InvokeId": "invoke-123"},
		fakeLingjunInvocationResponse("invoke-123", "Running"),
	}}
	runCLI := lingjunNodeCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node", "sendfile",
		"node-1",
		"--region", "cn-beijing",
		"--content", "hello",
		"--name", "hello.txt",
		"--target", "/tmp",
		"--timeout", "1ms",
	)
	if code == 0 {
		t.Fatalf("lingjun node sendfile should wait for completion and time out; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error code = %q, want WaitTimeout; stdout=%s stderr=%s", got, stdout, stderr)
	}
	// The 1ms timeout can elapse after one or more polls, so assert at least one
	// DescribeSendFileResults poll rather than an exact call count.
	if len(fake.calls) < 2 || fake.calls[0].operation != "SendFile" || fake.calls[1].operation != "DescribeSendFileResults" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}
