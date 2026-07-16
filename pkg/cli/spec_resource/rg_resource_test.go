package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestRGResourceListCallsListResources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"Resources": map[string]any{
					"Resource": []any{
						map[string]any{
							"ResourceId":      "i-123",
							"ResourceType":    "instance",
							"Service":         "ecs",
							"RegionId":        "cn-hangzhou",
							"ResourceGroupId": "rg-123",
							"CreateDate":      "2025-01-02T03:04:05Z",
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "resource" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/resource with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "resource", "list", "rg-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg resource list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceGroupId"] != "rg-123" || request["PageSize"] != 100 || request["PageNumber"] != 1 {
		t.Fatalf("list request = %#v", request)
	}
	out := decodeObject(t, stdout)
	resources, _ := out["resources"].([]any)
	if out["total"] != float64(1) || len(resources) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	first, _ := resources[0].(map[string]any)
	if first["resource_id"] != "i-123" || first["service"] != "ecs" || first["resource_type"] != "instance" {
		t.Fatalf("resource = %#v; stdout=%s", first, stdout)
	}
}

func TestRGResourceListWithFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 0,
				"Resources":  map[string]any{"Resource": []any{}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want rg/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "resource", "list",
		"--region", "cn-beijing",
		"--filter", "service=ecs",
		"--filter", "resource-type=instance",
		"--filter", "resource-id=i-456",
		"--limit", "50",
		"--page", "2",
	)
	if code != 0 {
		t.Fatalf("rg resource list with filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Service"] != "ecs" || request["ResourceType"] != "instance" || request["ResourceId"] != "i-456" {
		t.Fatalf("filter request = %#v", request)
	}
	if request["PageSize"] != 50 || request["PageNumber"] != 2 {
		t.Fatalf("pagination request = %#v", request)
	}
}

func TestRGResourceUpdateCallsMoveResources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"ErrorCode":    "",
						"ErrorMsg":     "",
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-move-inner",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "Success",
					},
				},
			},
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"Resources": map[string]any{
					"Resource": []any{
						map[string]any{
							"ResourceId":      "i-123",
							"ResourceType":    "instance",
							"Service":         "ecs",
							"RegionId":        "cn-hangzhou",
							"ResourceGroupId": "rg-target",
							"CreateDate":      "2025-01-02T03:04:05Z",
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "resource" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/resource with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-beijing",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
	)
	if code != 0 {
		t.Fatalf("rg resource update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "MoveResources" || fake.calls[1].operation != "ListResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	moveRequest := fake.calls[0].request
	if moveRequest["ResourceGroupId"] != "rg-target" {
		t.Fatalf("move request ResourceGroupId = %#v", moveRequest["ResourceGroupId"])
	}
	if moveRequest["Resources.1.ResourceId"] != "i-123" ||
		moveRequest["Resources.1.ResourceType"] != "instance" ||
		moveRequest["Resources.1.Service"] != "ecs" ||
		moveRequest["Resources.1.RegionId"] != "cn-hangzhou" {
		t.Fatalf("move request resources = %#v", moveRequest)
	}
	out := decodeObject(t, stdout)
	actions, _ := out["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (MoveResources + ListResources), got %d: %s", len(actions), stdout)
	}
	resources, _ := out["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources missing from output: %s", stdout)
	}
	first, _ := resources[0].(map[string]any)
	if first["resource_id"] != "i-123" || first["resource_group_id"] != "rg-target" {
		t.Fatalf("resource = %#v; stdout=%s", first, stdout)
	}
}

func TestRGResourceUpdateNoWaitSkipsResourceProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-move-inner",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "Success",
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "resource" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/resource with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-beijing",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("rg resource update --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "MoveResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestRGResourceUpdateTimesOutWhenMovedResourceIsNotVisible(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-move-inner",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "Success",
					},
				},
			},
			{
				"RequestId":  "req-list",
				"TotalCount": 0,
				"Resources":  map[string]any{"Resource": []any{}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "resource" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/resource with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, _, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-beijing",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--timeout", "1ms",
	)
	if code != 3 {
		t.Fatalf("rg resource update invisible moved resource exit %d, want 3; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error.code = %q, want WaitTimeout; stdout=%s", got, stdout)
	}
}

func TestRGResourceHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "rg", "resource", "list", "--help")
	if code != 0 {
		t.Fatalf("rg resource list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--filter") {
		t.Fatalf("list help missing --filter:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--limit") {
		t.Fatalf("list help missing --limit:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "resource", "update", "--help")
	if code != 0 {
		t.Fatalf("rg resource update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--resource") {
		t.Fatalf("update help missing --resource:\n%s", stdout)
	}
}
