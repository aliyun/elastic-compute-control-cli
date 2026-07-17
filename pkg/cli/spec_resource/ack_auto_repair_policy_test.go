package spec_resource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestACKAutoRepairPolicyHelpAndAlias(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "auto-repair-policy", "--help")
	if code != 0 {
		t.Fatalf("ack auto-repair-policy --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"list", "get", "create", "update", "delete"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("resource help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "auto-repair-policy", "create", "--help")
	if code != 0 {
		t.Fatalf("ack auto-repair-policy create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster string", "--policy string", "JSON object or @file"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %q:\n%s", want, stdout)
		}
	}

	fake := &fakeSpecCaller{responses: []map[string]any{
		ackAutoRepairPolicyDetailResponse("r-123"),
	}}
	runCLI := ackAutoRepairPolicyCaller(t, fake)
	stdout, stderr, code = runCLI("ack", "arp", "get", "r-123", "--cluster", "c-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack arp get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "DescribeAutoRepairPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestACKAutoRepairPolicyCRUDRoutesToDesignedAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-create", "policy_id": "r-123"},
		ackAutoRepairPolicyDetailResponse("r-123"),
		{"request_id": "req-update"},
		ackAutoRepairPolicyDetailResponse("r-123"),
		{"request_id": "req-delete"},
		ackAutoRepairPolicyDetailResponse("r-123"),
		map[string]any{
			"items": []any{
				map[string]any{
					"id":                "r-123",
					"name":              "gpu-repair",
					"resource_type":     "nodepool",
					"resource_sub_type": "ess",
				},
			},
		},
	}}
	runCLI := ackAutoRepairPolicyCaller(t, fake)

	policy := `{"name":"gpu-repair","resource_type":"nodepool","resource_sub_type":"ess","rules":[{"incidents":[{"name":"Node.FaultNeedReboot.HOST","type":"system"}]}]}`
	stdout, stderr, code := runCLI("ack", "auto-repair-policy", "create",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--policy", policy,
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "auto-repair-policy", "update", "r-123",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--policy", `{"name":"gpu-repair-2","rules":[]}`,
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "auto-repair-policy", "delete", "r-123",
		"--region", "cn-beijing",
		"--cluster", "c-123",
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "auto-repair-policy", "get", "r-123",
		"--region", "cn-beijing",
		"--cluster", "c-123",
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "auto-repair-policy", "list",
		"--region", "cn-beijing",
		"--cluster", "c-123",
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "CreateAutoRepairPolicy,DescribeAutoRepairPolicy,ModifyAutoRepairPolicy,DescribeAutoRepairPolicy,DeleteAutoRepairPolicy,DescribeAutoRepairPolicy,ListAutoRepairPolicies" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["cluster_id"] != "c-123" {
		t.Fatalf("CreateAutoRepairPolicy cluster_id = %#v; request=%#v", createReq["cluster_id"], createReq)
	}
	body := decodeRequestBody(t, createReq)
	if body == nil || body["name"] != "gpu-repair" || body["resource_type"] != "nodepool" {
		t.Fatalf("CreateAutoRepairPolicy body = %#v; request=%#v", body, createReq)
	}
	updateReq := fake.calls[2].request
	if updateReq["cluster_id"] != "c-123" || updateReq["policy_id"] != "r-123" {
		t.Fatalf("ModifyAutoRepairPolicy request = %#v", updateReq)
	}
	updateBody := decodeRequestBody(t, updateReq)
	if updateBody == nil || updateBody["name"] != "gpu-repair-2" {
		t.Fatalf("ModifyAutoRepairPolicy body = %#v; request=%#v", updateBody, updateReq)
	}
	deleteReq := fake.calls[4].request
	if deleteReq["cluster_id"] != "c-123" || deleteReq["policy_id"] != "r-123" {
		t.Fatalf("DeleteAutoRepairPolicy request = %#v", deleteReq)
	}
}

func TestACKAutoRepairPolicyPolicyFlagReadsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"name":"file-policy","rules":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-create", "policy_id": "r-file"},
		ackAutoRepairPolicyDetailResponse("r-file"),
	}}
	runCLI := ackAutoRepairPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "auto-repair-policy", "create",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--policy", "@"+policyPath,
	)
	if code != 0 {
		t.Fatalf("ack auto-repair-policy create --policy @file exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	body := decodeRequestBody(t, fake.calls[0].request)
	if body == nil || body["name"] != "file-policy" {
		t.Fatalf("@file policy body = %#v; request=%#v", body, fake.calls[0].request)
	}
}

func ackAutoRepairPolicyCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "auto-repair-policy" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %#v, want ack/auto-repair-policy with CS API product", resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func ackAutoRepairPolicyDetailResponse(id string) map[string]any {
	return map[string]any{
		"id":                id,
		"name":              "gpu-repair",
		"resource_type":     "nodepool",
		"resource_sub_type": "ess",
		"resource_ids":      []any{"np-123"},
		"rules": []any{
			map[string]any{
				"incidents": []any{
					map[string]any{"name": "Node.FaultNeedReboot.HOST", "type": "system"},
				},
			},
		},
	}
}

func decodeRequestBody(t *testing.T, request map[string]any) map[string]any {
	t.Helper()
	switch body := request["body"].(type) {
	case map[string]any:
		return body
	case string:
		var decoded map[string]any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("request body is not JSON object: %#v", request["body"])
		}
		return decoded
	default:
		t.Fatalf("request body has type %T; request=%#v", request["body"], request)
	}
	return nil
}
