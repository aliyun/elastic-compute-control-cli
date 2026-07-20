package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestACKNodeHelpShowsDesignedCommandShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		want      []string
		notWanted []string
	}{
		{
			name: "get",
			args: []string{"ack", "node", "get", "--help"},
			want: []string{"get <node-id>", "--cluster string"},
		},
		{
			name: "list",
			args: []string{"ack", "node", "list", "--help"},
			want: []string{"list", "--cluster string", "--nodepool string"},
		},
		{
			name: "delete",
			args: []string{"ack", "node", "delete", "--help"},
			want: []string{"delete [ids...]", "--cluster string", "--release", "--force"},
		},
		{
			name: "attach",
			args: []string{"ack", "node", "attach", "--help"},
			want: []string{"attach", "--cluster string", "--instance string", "--nodepool string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("ack node %s --help exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
			}
			for _, snippet := range tt.want {
				if !strings.Contains(stdout, snippet) {
					t.Fatalf("help missing %q: %s", snippet, stdout)
				}
			}
			for _, snippet := range tt.notWanted {
				if strings.Contains(stdout, snippet) {
					t.Fatalf("help should not contain %q: %s", snippet, stdout)
				}
			}
		})
	}
}

func TestACKNodeGetUsesDescribeClusterNodes(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKNodeListResponse("i-node-1", "Ready")}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "node" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api=%s, want ack/node api=CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ack", "node", "get", "i-node-1", "--region", "cn-hangzhou", "--cluster", "c-123")
	if code != 0 {
		t.Fatalf("ack node get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["instanceIds"] != "i-node-1" {
		t.Fatalf("DescribeClusterNodes request = %#v", request)
	}
	node, _ := decodeObject(t, stdout)["node"].(map[string]any)
	if node == nil || node["id"] != "i-node-1" || node["status"] != "Ready" || node["nodepool"] != "np-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodeListUsesClusterNodepoolAndPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKNodeListResponse("i-node-1", "Ready")}}
	runCLI := ackNodeCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "node", "list", "--region", "cn-hangzhou", "--cluster", "c-123", "--nodepool", "np-123", "--page", "2", "--limit", "20")
	if code != 0 {
		t.Fatalf("ack node list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["nodepool_id"] != "np-123" || request["pageNumber"] != 2 || request["pageSize"] != 20 {
		t.Fatalf("DescribeClusterNodes request = %#v", request)
	}
	out := decodeObject(t, stdout)
	nodes, _ := out["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("unexpected nodes output: %s", stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["page"] != float64(2) || pagination["limit"] != float64(20) || pagination["has_more"] != false {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
}

func TestACKNodeListDefaultsLimitTo100(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKNodeListResponse("i-node-1", "Ready")}}
	runCLI := ackNodeCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "node", "list", "--region", "cn-hangzhou", "--cluster", "c-123")
	if code != 0 {
		t.Fatalf("ack node list default limit exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["pageNumber"] != 1 || request["pageSize"] != 100 {
		t.Fatalf("DescribeClusterNodes default pagination request = %#v", request)
	}
}

func TestACKNodeDeleteUsesDeleteClusterNodesAndWaitsAbsent(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-delete", "task_id": "T-123", "cluster_id": "c-123"},
		{"nodes": []any{}, "page": map[string]any{"page_number": 1, "page_size": 100, "total_count": 0}},
	}}
	runCLI := ackNodeCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "node", "delete", "i-node-1", "i-node-2", "--region", "cn-hangzhou", "--cluster", "c-123", "--release")
	if code != 0 {
		t.Fatalf("ack node delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DeleteClusterNodes" || fake.calls[1].operation != "DescribeClusterNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	deleteReq := fake.calls[0].request
	if deleteReq["ClusterId"] != "c-123" || deleteReq["body.release_node"] != true {
		t.Fatalf("DeleteClusterNodes request = %#v", deleteReq)
	}
	nodes, ok := deleteReq["body.nodes"].([]string)
	if !ok || len(nodes) != 2 || nodes[0] != "i-node-1" || nodes[1] != "i-node-2" {
		t.Fatalf("DeleteClusterNodes body.nodes = %#v; request=%#v", deleteReq["body.nodes"], deleteReq)
	}
	if _, ok := deleteReq["nodes"]; ok {
		t.Fatalf("DeleteClusterNodes must not send top-level nodes: %#v", deleteReq)
	}
	if _, ok := deleteReq["force"]; ok {
		t.Fatalf("DeleteClusterNodes must not pass CLI safety --force to ACK API: %#v", deleteReq)
	}
	waitReq := fake.calls[1].request
	if waitReq["ClusterId"] != "c-123" {
		t.Fatalf("DescribeClusterNodes wait request = %#v", waitReq)
	}
	waitIDs, ok := waitReq["instanceIds"].([]string)
	if !ok || len(waitIDs) != 2 || waitIDs[0] != "i-node-1" || waitIDs[1] != "i-node-2" {
		t.Fatalf("DescribeClusterNodes wait instanceIds = %#v; request=%#v", waitReq["instanceIds"], waitReq)
	}
	out := decodeObject(t, stdout)
	if out["deleted"] != true {
		t.Fatalf("delete output missing deleted=true: %s", stdout)
	}
	node, _ := out["node"].(map[string]any)
	if node == nil || node["id"] != "i-node-1" {
		t.Fatalf("delete output should include requested node id: %s", stdout)
	}
}

func TestACKNodeAttachUsesAttachInstancesWithNodepool(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"task_id": "T-attach", "list": []any{map[string]any{"code": "200", "instanceId": "i-node-1", "message": "successful"}}},
		{"task_id": "T-attach", "state": "success", "task_type": "cluster_attach"},
		fakeACKNodeListResponse("i-node-1", "Ready"),
	}}
	runCLI := ackNodeCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "node", "attach", "--region", "cn-hangzhou", "--cluster", "c-123", "--nodepool", "np-123", "--instance", "i-node-1")
	if code != 0 {
		t.Fatalf("ack node attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "AttachInstances" || fake.calls[1].operation != "DescribeTaskInfo" || fake.calls[2].operation != "DescribeClusterNodes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	attachReq := fake.calls[0].request
	if attachReq["ClusterId"] != "c-123" {
		t.Fatalf("AttachInstances request = %#v", attachReq)
	}
	instances, ok := attachReq["body.instances"].([]string)
	if !ok || len(instances) != 1 || instances[0] != "i-node-1" {
		t.Fatalf("AttachInstances body.instances = %#v; request=%#v", attachReq["body.instances"], attachReq)
	}
	if _, ok := attachReq["instances"]; ok {
		t.Fatalf("AttachInstances must not send top-level instances: %#v", attachReq)
	}
	if attachReq["body.nodepool_id"] != "np-123" {
		t.Fatalf("AttachInstances body.nodepool_id = %#v; request=%#v", attachReq["body.nodepool_id"], attachReq)
	}
	if _, ok := attachReq["nodepool_id"]; ok {
		t.Fatalf("AttachInstances must not send top-level nodepool_id: %#v", attachReq)
	}
	if fake.calls[1].request["task_id"] != "T-attach" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[1].request)
	}
	readReq := fake.calls[2].request
	if readReq["ClusterId"] != "c-123" || readReq["instanceIds"] != "i-node-1" {
		t.Fatalf("DescribeClusterNodes readback request = %#v", readReq)
	}
	node, _ := decodeObject(t, stdout)["node"].(map[string]any)
	if node == nil || node["id"] != "i-node-1" || node["status"] != "Ready" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKNodeAttachFailsFastWhenAttachTaskFails(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"task_id": "T-failed", "list": []any{map[string]any{"code": "200", "instanceId": "i-node-1", "message": "successful"}}},
		{"task_id": "T-failed", "state": "failed", "task_type": "cluster_attach", "error": map[string]any{"message": "join timeout"}},
	}}
	runCLI := ackNodeCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "node", "attach", "--region", "cn-hangzhou", "--cluster", "c-123", "--instance", "i-node-1", "--timeout", "20ms")
	if code == 0 {
		t.Fatalf("failed attach task must fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[1].operation != "DescribeTaskInfo" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if !strings.Contains(stdout, "WaitFailed") {
		t.Fatalf("attach task failure is not surfaced: %s", stdout)
	}
}

func ackNodeCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "node" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api=%s, want ack/node api=CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})
}

func fakeACKNodeListResponse(id string, nodeStatus string) map[string]any {
	return map[string]any{
		"nodes": []any{
			map[string]any{
				"instance_id":          id,
				"instance_name":        "worker-1",
				"instance_role":        "Worker",
				"instance_status":      "Running",
				"instance_type":        "ecs.c7.large",
				"instance_type_family": "ecs.c7",
				"ip_address":           []any{"192.168.0.10"},
				"is_aliyun_node":       true,
				"node_name":            "cn-hangzhou.192.168.0.10",
				"node_status":          nodeStatus,
				"nodepool_id":          "np-123",
				"source":               "ess_attach",
				"state":                "running",
				"spot_strategy":        "NoSpot",
				"instance_charge_type": "PostPaid",
				"image_id":             "aliyun_3_x64_20G_alibase_20241218.vhd",
				"creation_time":        "2026-01-01T00:00:00Z",
				"expired_time":         "2099-12-31T15:59:00Z",
				"host_name":            "worker-host",
				"error_message":        "",
			},
		},
		"page": map[string]any{
			"page_number": 1,
			"page_size":   100,
			"total_count": 21,
		},
	}
}
