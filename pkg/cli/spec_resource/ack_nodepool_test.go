package spec_resource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestACKNodepoolHelpShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			name:    "create",
			args:    []string{"ack", "nodepool", "create", "--help"},
			want:    []string{"create", "--cluster string", "--config string", "--name string", "--vswitch strings", "--instance-type strings", "--desired-size int", "--system-disk-category string", "--system-disk-size int", "--internet-max-bandwidth-out int", "--runtime string", "--runtime-version string"},
			notWant: []string{"--nodepool"},
		},
		{
			name:    "get",
			args:    []string{"ack", "np", "get", "--help"},
			want:    []string{"get <id>", "--cluster string", "--with-vuls"},
			notWant: []string{"--nodepool"},
		},
		{
			name:    "update",
			args:    []string{"ack", "nodepool", "update", "--help"},
			want:    []string{"update <id>", "--cluster string", "--desired-size int", "--with-node-config", "--no-wait", "--timeout duration"},
			notWant: []string{"--nodepool", "--tag", "--untag"},
		},
		{
			name:    "attach",
			args:    []string{"ack", "np", "attach", "--help"},
			want:    []string{"attach <id>", "--cluster string", "--instance", "--print-script-only", "--no-wait", "--timeout duration"},
			notWant: []string{"--nodepool"},
		},
		{
			name:    "detach",
			args:    []string{"ack", "nodepool", "detach", "--help"},
			want:    []string{"detach <id>", "--cluster string", "--node", "--force", "--no-wait", "--timeout duration"},
			notWant: []string{"--nodepool"},
		},
		{
			name:    "repair",
			args:    []string{"ack", "np", "repair", "--help"},
			want:    []string{"repair <id>", "--cluster string", "--node", "--vulnerabilities", "--no-wait", "--timeout duration"},
			notWant: []string{"--nodepool"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("%s exit %d stderr=%s stdout=%s", strings.Join(tt.args, " "), code, stderr, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("help missing %q:\n%s", want, stdout)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(stdout, notWant) {
					t.Fatalf("help should not expose %q:\n%s", notWant, stdout)
				}
			}
		})
	}
}

func TestACKNodepoolCreateHighLevelFieldsMapCreateBody(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-create", "nodepool_id": "np-123"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "create",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--name", "workers",
		"--vswitch", "vsw-a",
		"--vswitch", "vsw-b",
		"--instance-type", "ecs.g7.large",
		"--instance-type", "ecs.g7.xlarge",
		"--desired-size", "3",
		"--system-disk-category", "cloud_essd",
		"--system-disk-size", "120",
		"--internet-max-bandwidth-out", "10",
		"--runtime", "containerd",
		"--runtime-version", "1.6.28",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack nodepool create high-level exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateClusterNodePool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["body.nodepool_info.name"] != "workers" {
		t.Fatalf("nodepool create request = %#v", request)
	}
	requireStringValues(t, request["body.scaling_group.vswitch_ids"], []string{"vsw-a", "vsw-b"})
	requireStringValues(t, request["body.scaling_group.instance_types"], []string{"ecs.g7.large", "ecs.g7.xlarge"})
	if request["body.scaling_group.desired_size"] != 3 ||
		request["body.scaling_group.system_disk_category"] != "cloud_essd" ||
		request["body.scaling_group.system_disk_size"] != 120 ||
		request["body.scaling_group.internet_max_bandwidth_out"] != 10 ||
		request["body.kubernetes_config.runtime"] != "containerd" ||
		request["body.kubernetes_config.runtime_version"] != "1.6.28" {
		t.Fatalf("nodepool create request = %#v", request)
	}
	nodepool, _ := decodeObject(t, stdout)["nodepool"].(map[string]any)
	if nodepool == nil || nodepool["id"] != "np-123" || nodepool["cluster"] != "c-123" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestACKNodepoolCreateConfigPathStillUsesRawBody(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-create", "nodepool_id": "np-123"}}}
	runCLI := ackNodepoolRunner(t, fake)

	raw := `{"nodepool_info":{"name":"raw-pool"},"scaling_group":{"vswitch_ids":["vsw-a"],"instance_types":["ecs.g7.large"],"desired_size":2}}`
	stdout, stderr, code := runCLI("ack", "nodepool", "create",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--config", raw,
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack nodepool create --config exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateClusterNodePool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	bodyText, _ := request["body"].(string)
	var body map[string]any
	if err := json.Unmarshal([]byte(bodyText), &body); err != nil {
		t.Fatalf("nodepool raw config body is not JSON: %#v", request)
	}
	nodepoolInfo, _ := body["nodepool_info"].(map[string]any)
	scalingGroup, _ := body["scaling_group"].(map[string]any)
	if request["ClusterId"] != "c-123" || nodepoolInfo["name"] != "raw-pool" || scalingGroup["desired_size"] != float64(2) {
		t.Fatalf("nodepool raw config request = %#v", request)
	}
}

func TestACKNodepoolCreateRejectsConfigWithHighLevelFields(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "create",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--config", `{"nodepool_info":{"name":"raw-pool"}}`,
		"--name", "workers",
	)
	if code == 0 {
		t.Fatalf("ack nodepool create config+name should fail stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if !strings.Contains(stdout, "--config") || !strings.Contains(stdout, "--name") {
		t.Fatalf("conflict error should mention both flags: %s", stdout)
	}
}

func TestACKNodepoolCreateHighLevelRequiresACKRequiredFields(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "create",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--desired-size", "3",
	)
	if code == 0 {
		t.Fatalf("ack nodepool create incomplete high-level body should fail stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("incomplete high-level body should fail before API calls: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
}

func TestACKNodepoolCreateAPIParamOnlyDoesNotSatisfyBodyInput(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "create",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--api-param", "x=y",
		"--no-wait",
	)
	if code == 0 {
		t.Fatalf("ack nodepool create api-param-only should fail stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("api-param-only should fail before API calls: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	message := errorMessage(t, stdout)
	for _, want := range []string{"--config", "--name", "--vswitch", "--instance-type"} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing input error should mention %s: %s", want, message)
		}
	}
	if strings.Contains(message, "body.") {
		t.Fatalf("missing input error should mention CLI inputs, not binding paths: %s", message)
	}
}

func TestACKNodepoolGetWithVulsRoutesDetailAndVuls(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeACKNodepoolDetail("np-123"),
			{"request_id": "req-vuls", "vuls": []any{map[string]any{"cve_id": "CVE-2026-0001"}}},
		},
	}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "np", "get", "np-123", "--cluster", "c-123", "--region", "cn-beijing", "--with-vuls")
	if code != 0 {
		t.Fatalf("ack np get --with-vuls exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeClusterNodePoolDetail" || fake.calls[1].operation != "DescribeNodePoolVuls" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ClusterId"] != "c-123" || fake.calls[0].request["NodepoolId"] != "np-123" {
		t.Fatalf("detail request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["cluster_id"] != "c-123" || fake.calls[1].request["nodepool_id"] != "np-123" {
		t.Fatalf("vuls request = %#v", fake.calls[1].request)
	}
	nodepool, _ := decodeObject(t, stdout)["nodepool"].(map[string]any)
	if nodepool == nil || nodepool["id"] != "np-123" || len(nodepool["vuls"].([]any)) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodepoolUpdateDesiredSizeRoutesScale(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-scale", "task_id": "task-123"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "update", "np-123", "--cluster", "c-123", "--region", "cn-beijing", "--desired-size", "3", "--no-wait")
	if code != 0 {
		t.Fatalf("ack nodepool update --desired-size exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ScaleClusterNodePool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["NodepoolId"] != "np-123" || request["body.desired_size"] != 3 {
		t.Fatalf("scale request = %#v", request)
	}
}

func TestACKNodepoolUpdateConfigWaitsForTask(t *testing.T) {
	t.Parallel()

	detail := fakeACKNodepoolDetail("np-123")
	detail["nodepool_info"].(map[string]any)["name"] = "workers-updated"
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-update", "nodepool_id": "np-123", "task_id": "T-update"},
		{"request_id": "req-task", "task_id": "T-update", "state": "success", "task_type": "nodepool_update"},
		detail,
	}}
	runCLI := ackNodepoolRunnerWithResource(t, fake, func(resource *spec.ResourceSpec) {
		waiter := resource.Waiters["task_succeeded"]
		waiter.Interval = "1ms"
		waiter.Timeout = "20ms"
		resource.Waiters["task_succeeded"] = waiter
	})

	stdout, stderr, code := runCLI("ack", "nodepool", "update", "np-123",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--config", `{"nodepool_info":{"name":"workers-updated"}}`,
	)
	if code != 0 {
		t.Fatalf("ack nodepool update --config exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "ModifyClusterNodePool" ||
		fake.calls[1].operation != "DescribeTaskInfo" ||
		fake.calls[2].operation != "DescribeClusterNodePoolDetail" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["task_id"] != "T-update" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[1].request)
	}
	nodepool, _ := decodeObject(t, stdout)["nodepool"].(map[string]any)
	if nodepool == nil || nodepool["name"] != "workers-updated" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodepoolCreateWaitsForNestedStatusState(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-create", "nodepool_id": "np-123"},
		fakeACKNodepoolDetail("np-123"),
	}}
	runCLI := ackNodepoolRunnerWithResource(t, fake, func(resource *spec.ResourceSpec) {
		waiter := resource.Waiters["active_after_change"]
		waiter.Interval = "1ms"
		waiter.Timeout = "20ms"
		resource.Waiters["active_after_change"] = waiter
	})

	stdout, stderr, code := runCLI("ack", "nodepool", "create", "--cluster", "c-123", "--region", "cn-beijing", "--config", `{"nodepool_info":{"name":"workers"}}`)
	if code != 0 {
		t.Fatalf("ack nodepool create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateClusterNodePool" || fake.calls[1].operation != "DescribeClusterNodePoolDetail" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["ClusterId"] != "c-123" || fake.calls[1].request["NodepoolId"] != "np-123" {
		t.Fatalf("detail request = %#v", fake.calls[1].request)
	}
	nodepool, _ := decodeObject(t, stdout)["nodepool"].(map[string]any)
	if nodepool == nil || nodepool["id"] != "np-123" || nodepool["status"] != "active" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodepoolListUsesNestedStatusState(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKNodepoolListResponse("np-123")}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "list", "--cluster", "c-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack nodepool list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterNodePools" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ClusterId"] != "c-123" {
		t.Fatalf("list request = %#v", fake.calls[0].request)
	}
	nodepools, _ := decodeObject(t, stdout)["nodepools"].([]any)
	if len(nodepools) != 1 {
		t.Fatalf("unexpected nodepools output: %s", stdout)
	}
	nodepool, _ := nodepools[0].(map[string]any)
	if nodepool["id"] != "np-123" || nodepool["status"] != "active" {
		t.Fatalf("unexpected nodepool output: %#v; stdout=%s", nodepool, stdout)
	}
}

func TestACKNodepoolAttachPrintScriptOnlyRoutesScriptAPI(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"request_id": "req-script",
			"script":     "curl -fsSL https://example.invalid/attach.sh | sh",
		}},
	}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "np", "attach", "np-123", "--cluster", "c-123", "--region", "cn-beijing", "--print-script-only")
	if code != 0 {
		t.Fatalf("ack np attach --print-script-only exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterAttachScripts" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ClusterId"] != "c-123" {
		t.Fatalf("script request = %#v", fake.calls[0].request)
	}
	script, _ := decodeObject(t, stdout)["attach_script"].(map[string]any)
	if script == nil || script["script"] == "" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodepoolAttachMapsInstancesIntoRequestBody(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-attach",
		"task_id":    "T-attach",
	}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "attach", "np-123",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--instance", "i-123",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack nodepool attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "AttachInstancesToNodePool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["NodepoolId"] != "np-123" {
		t.Fatalf("AttachInstancesToNodePool request = %#v", request)
	}
	instances, ok := request["body.instances"].([]string)
	if !ok || len(instances) != 1 || instances[0] != "i-123" {
		t.Fatalf("body.instances = %#v; request=%#v", request["body.instances"], request)
	}
	if _, ok := request["instances"]; ok {
		t.Fatalf("instances must not be sent at the request top level: %#v", request)
	}
}

func TestACKNodepoolAttachWaitsForTaskBeforeReadback(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-attach", "task_id": "T-attach"},
		{"request_id": "req-task", "task_id": "T-attach", "state": "success", "task_type": "nodepool_attach"},
		fakeACKNodepoolDetail("np-123"),
	}}
	runCLI := ackNodepoolRunnerWithResource(t, fake, func(resource *spec.ResourceSpec) {
		waiter := resource.Waiters["task_succeeded"]
		waiter.Interval = "1ms"
		waiter.Timeout = "20ms"
		resource.Waiters["task_succeeded"] = waiter
	})

	stdout, stderr, code := runCLI("ack", "nodepool", "attach", "np-123",
		"--cluster", "c-123",
		"--region", "cn-beijing",
		"--instance", "i-123",
	)
	if code != 0 {
		t.Fatalf("ack nodepool attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "AttachInstancesToNodePool" ||
		fake.calls[1].operation != "DescribeTaskInfo" ||
		fake.calls[2].operation != "DescribeClusterNodePoolDetail" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["task_id"] != "T-attach" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[1].request)
	}
	nodepool, _ := decodeObject(t, stdout)["nodepool"].(map[string]any)
	if nodepool == nil || nodepool["id"] != "np-123" || nodepool["cluster"] != "c-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodepoolRepairMapsNodesIntoBody(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-repair", "task_id": "T-repair"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "repair", "np-123",
		"--cluster", "c-123",
		"--node", "cn-beijing.10.0.0.1",
		"--region", "cn-beijing",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack nodepool repair exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RepairClusterNodePool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	requireStringValues(t, request["body.nodes"], []string{"cn-beijing.10.0.0.1"})
	if _, ok := request["nodes"]; ok {
		t.Fatalf("nodes must be nested in the request body: %#v", request)
	}
}

func TestACKNodepoolDetachMapsInstanceIDsAndControlsIntoQuery(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-detach", "task_id": "T-detach"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "detach", "np-123",
		"--cluster", "c-123",
		"--instance", "i-123",
		"--force",
		"--drain-node",
		"--concurrency",
		"--region", "cn-beijing",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack nodepool detach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RemoveNodePoolNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	requireStringValues(t, request["instance_ids"], []string{"i-123"})
	if request["release_node"] != true || request["drain_node"] != true || request["concurrency"] != true {
		t.Fatalf("detach request = %#v", request)
	}
	for _, key := range []string{"body.instance_ids", "body.release_node", "body.drain_node", "body.concurrency"} {
		if _, ok := request[key]; ok {
			t.Fatalf("%s must be sent as a query parameter: %#v", key, request)
		}
	}
}

func TestACKNodepoolDeleteForceDefaultsFalse(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-delete"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "delete", "np-123", "--cluster", "c-123", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ack nodepool delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteClusterNodepool" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["NodepoolId"] != "np-123" || request["force"] != false {
		t.Fatalf("delete request = %#v", request)
	}
}

func TestACKNodepoolDeleteMapsForce(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-delete"}}}
	runCLI := ackNodepoolRunner(t, fake)

	stdout, stderr, code := runCLI("ack", "nodepool", "delete", "np-123", "--cluster", "c-123", "--force", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ack nodepool delete --force exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].request["force"] != true {
		t.Fatalf("delete --force request = %#v", fake.calls)
	}
}

func TestACKNodepoolDeleteWaitsForTaskThenAbsence(t *testing.T) {
	t.Parallel()

	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-delete", "task_id": "T-delete"},
		{"request_id": "req-task", "task_id": "T-delete", "state": "success", "task_type": "nodepool_delete"},
		{"request_id": "req-list", "nodepools": []any{}},
	}}
	runCLI := ackNodepoolRunnerWithResource(t, fake, func(resource *spec.ResourceSpec) {
		for _, name := range []string{"task_succeeded", "absent_after_delete"} {
			waiter := resource.Waiters[name]
			waiter.Interval = "1ms"
			waiter.Timeout = "20ms"
			resource.Waiters[name] = waiter
		}
	})

	stdout, stderr, code := runCLI("ack", "nodepool", "delete", "np-123",
		"--cluster", "c-123",
		"--region", "cn-beijing",
	)
	if code != 0 {
		t.Fatalf("ack nodepool delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "DeleteClusterNodepool" ||
		fake.calls[1].operation != "DescribeTaskInfo" ||
		fake.calls[2].operation != "DescribeClusterNodePools" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["task_id"] != "T-delete" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[1].request)
	}
	if out := decodeObject(t, stdout); out["deleted"] != true {
		t.Fatalf("delete output missing deleted=true: %s", stdout)
	}
}

func ackNodepoolRunner(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return ackNodepoolRunnerWithResource(t, fake, nil)
}

func ackNodepoolRunnerWithResource(t *testing.T, fake *fakeSpecCaller, mutate func(*spec.ResourceSpec)) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "nodepool" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api=%s, want ack/nodepool api=CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		if mutate != nil {
			mutate(&resource)
		}
		return fake, nil
	})
}

func fakeACKNodepoolDetail(id string) map[string]any {
	return map[string]any{
		"request_id": "req-detail",
		"status": map[string]any{
			"state": "active",
		},
		"nodepool_info": map[string]any{
			"nodepool_id": id,
			"name":        "workers",
		},
	}
}

func fakeACKNodepoolListResponse(id string) map[string]any {
	return map[string]any{
		"request_id": "req-list",
		"nodepools": []any{
			map[string]any{
				"status": map[string]any{
					"state": "active",
				},
				"nodepool_info": map[string]any{
					"nodepool_id": id,
					"name":        "workers",
				},
			},
		},
	}
}
