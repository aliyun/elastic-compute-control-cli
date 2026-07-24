package spec_resource

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
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

func TestRGResourceUpdateWaiterFiltersEveryMovedResource(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "SUCCESS",
					},
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"ResourceId":   "d-456",
						"ResourceType": "disk",
						"Service":      "ecs",
						"Status":       "SUCCESS",
					},
				},
			},
			{
				"RequestId": "req-list-instance",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"ResourceGroupId": "rg-target",
						"ResourceId":      "i-123",
						"ResourceType":    "instance",
						"Service":         "ecs",
					},
				}},
			},
			{
				"RequestId": "req-list-disk",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"ResourceGroupId": "rg-target",
						"ResourceId":      "d-456",
						"ResourceType":    "disk",
						"Service":         "ecs",
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-hangzhou",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
		"--timeout", "10ms",
	)
	if code != 0 {
		t.Fatalf("rg resource batch update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("calls = %#v, want MoveResources plus one filtered ListResources call per moved resource", fake.calls)
	}
	actions, _ := decodeObject(t, stdout)["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v, want one audit action per cloud API call; stdout=%s", actions, stdout)
	}
	for index, requestID := range []string{"req-move", "req-list-instance", "req-list-disk"} {
		action, _ := actions[index].(map[string]any)
		if action["request_id"] != requestID {
			t.Fatalf("action %d = %#v, want request_id %s; stdout=%s", index, action, requestID, stdout)
		}
	}
	for index, want := range []struct {
		id           string
		resourceType string
	}{
		{id: "i-123", resourceType: "instance"},
		{id: "d-456", resourceType: "disk"},
	} {
		call := fake.calls[index+1]
		if call.operation != "ListResources" ||
			call.request["ResourceGroupId"] != "rg-target" ||
			call.request["ResourceId"] != want.id ||
			call.request["ResourceType"] != want.resourceType ||
			call.request["Service"] != "ecs" ||
			call.request["Region"] != "cn-hangzhou" {
			t.Fatalf("waiter call %d = %#v", index, call)
		}
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

func TestRGResourceUpdateSurfacesEmbeddedMoveFailureBeforeWait(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"ErrorCode":    "NoPermission",
						"ErrorMsg":     "You are not authorized to move this resource.",
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-move-inner",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "FAIL",
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

	stdout, _, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-beijing",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--timeout", "1ms",
	)
	if code != 2 {
		t.Fatalf("rg resource update embedded failure exit %d, want 2; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "CloudAPIError" {
		t.Fatalf("error.code = %q, want CloudAPIError; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "MoveResources" {
		t.Fatalf("embedded failure must stop before waiter; calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one failed MoveResources action; stdout=%s", actions, stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["action_name"] != "MoveResources" ||
		action["code"] != "NoPermission" ||
		!strings.Contains(fmt.Sprint(action["message"]), "resource_id=i-123") ||
		!strings.Contains(fmt.Sprint(action["message"]), "You are not authorized to move this resource.") ||
		action["request_id"] != "req-move-inner" {
		t.Fatalf("action = %#v; stdout=%s", action, stdout)
	}
}

func TestRGResourceUpdateReportsAndReconcilesMixedMoveResults(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-item-success",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "SUCCESS",
					},
					map[string]any{
						"ErrorCode":    "NoPermission",
						"ErrorMsg":     "You are not authorized to move this resource.",
						"RegionId":     "cn-hangzhou",
						"RequestId":    "req-item-fail",
						"ResourceId":   "d-456",
						"ResourceType": "disk",
						"Service":      "ecs",
						"Status":       "FAIL",
					},
				},
			},
			{
				"RequestId": "req-list-success",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"RegionId":        "cn-hangzhou",
						"ResourceGroupId": "rg-target",
						"ResourceId":      "i-123",
						"ResourceType":    "instance",
						"Service":         "ecs",
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, _, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-hangzhou",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
		"--timeout", "10ms",
	)
	if code != 2 {
		t.Fatalf("rg resource mixed update exit %d, want 2; stdout=%s", code, stdout)
	}
	if len(fake.calls) != 2 ||
		fake.calls[0].operation != "MoveResources" ||
		fake.calls[1].operation != "ListResources" ||
		fake.calls[1].request["ResourceId"] != "i-123" {
		t.Fatalf("calls = %#v, want MoveResources then exact reconciliation of the successful resource", fake.calls)
	}
	out := decodeObject(t, stdout)
	actions, _ := out["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v, want successful and failed resource outcomes plus reconciliation; stdout=%s", actions, stdout)
	}
	success, _ := actions[0].(map[string]any)
	failure, _ := actions[1].(map[string]any)
	reconciliation, _ := actions[2].(map[string]any)
	if success["action_name"] != "MoveResources" ||
		success["code"] != "SUCCESS" ||
		!strings.Contains(fmt.Sprint(success["message"]), "resource_id=i-123") {
		t.Fatalf("success action = %#v; stdout=%s", success, stdout)
	}
	if failure["action_name"] != "MoveResources" ||
		failure["code"] != "NoPermission" ||
		!strings.Contains(fmt.Sprint(failure["message"]), "resource_id=d-456") {
		t.Fatalf("failure action = %#v; stdout=%s", failure, stdout)
	}
	if reconciliation["action_name"] != "ListResources" ||
		reconciliation["request_id"] != "req-list-success" {
		t.Fatalf("reconciliation action = %#v; stdout=%s", reconciliation, stdout)
	}
}

func TestRGResourceUpdateNoWaitStillReportsEveryMixedMoveResult(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-move",
			"Responses": []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				map[string]any{
					"ErrorCode":    "NoPermission",
					"ErrorMsg":     "disk move denied",
					"RegionId":     "cn-hangzhou",
					"ResourceId":   "d-456",
					"ResourceType": "disk",
					"Service":      "ecs",
					"Status":       "FAIL",
				},
			},
		},
	}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, _, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-hangzhou",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
		"--no-wait",
	)
	if code != 2 || len(fake.calls) != 1 {
		t.Fatalf("no-wait mixed update exit=%d calls=%#v stdout=%s", code, fake.calls, stdout)
	}
	actions, _ := decodeObject(t, stdout)["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("actions = %#v, want both provider outcomes without reconciliation; stdout=%s", actions, stdout)
	}
}

func TestRGResourceUpdateRejectsIncompleteEmbeddedMoveSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		responses []any
	}{
		{
			name: "truncated",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-1",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
			},
		},
		{
			name: "duplicate",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-1",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
			},
		},
		{
			name: "unexpected",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-1",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"ResourceId":   "i-unexpected",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
			},
		},
		{
			name: "malformed trailing item",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-1",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				"not-an-object",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{
				{
					"RequestId": "req-move",
					"Responses": tt.responses,
				},
			}}
			runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				return fake, nil
			})

			stdout, _, code := runCLI(
				"rg", "resource", "update", "rg-target",
				"--region", "cn-hangzhou",
				"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
				"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
				"--no-wait",
			)
			if code != 2 {
				t.Fatalf("rg resource update %s response exit %d, want 2; stdout=%s", tt.name, code, stdout)
			}
			if got := errorCode(t, stdout); got != "CloudAPIError" {
				t.Fatalf("error.code = %q, want CloudAPIError; stdout=%s", got, stdout)
			}
			out := decodeObject(t, stdout)
			actions, _ := out["actions"].([]any)
			if len(actions) != 2 {
				t.Fatalf("actions = %#v, want the known successful outcome plus structural failure; stdout=%s", actions, stdout)
			}
			success, _ := actions[0].(map[string]any)
			if success["action_name"] != "MoveResources" ||
				success["code"] != "SUCCESS" ||
				!strings.Contains(fmt.Sprint(success["message"]), "resource_id=i-123") {
				t.Fatalf("success action = %#v; stdout=%s", success, stdout)
			}
			action, _ := actions[1].(map[string]any)
			if action["action_name"] != "MoveResources" ||
				action["code"] != "InvalidResponse" ||
				action["request_id"] != "req-move" {
				t.Fatalf("action = %#v; stdout=%s", action, stdout)
			}
		})
	}
}

func TestRGResourceUpdateReconcilesKnownSuccessFromTruncatedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		responses []any
	}{
		{
			name: "truncated",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-success",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
			},
		},
		{
			name: "missing status",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-success",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"ResourceId":   "d-456",
					"ResourceType": "disk",
					"Service":      "ecs",
				},
			},
		},
		{
			name: "non-object",
			responses: []any{
				map[string]any{
					"RegionId":     "cn-hangzhou",
					"RequestId":    "req-item-success",
					"ResourceId":   "i-123",
					"ResourceType": "instance",
					"Service":      "ecs",
					"Status":       "SUCCESS",
				},
				"not-an-object",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{
				{
					"RequestId": "req-move",
					"Responses": tt.responses,
				},
				{
					"RequestId": "req-list-success",
					"Resources": map[string]any{"Resource": []any{
						map[string]any{
							"RegionId":        "cn-hangzhou",
							"ResourceGroupId": "rg-target",
							"ResourceId":      "i-123",
							"ResourceType":    "instance",
							"Service":         "ecs",
						},
					}},
				},
			}}
			runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				return fake, nil
			})

			stdout, _, code := runCLI(
				"rg", "resource", "update", "rg-target",
				"--region", "cn-hangzhou",
				"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
				"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
				"--timeout", "10ms",
			)
			if code != 2 {
				t.Fatalf("rg resource %s update exit %d, want 2; stdout=%s", tt.name, code, stdout)
			}
			if len(fake.calls) != 2 ||
				fake.calls[1].operation != "ListResources" ||
				fake.calls[1].request["ResourceGroupId"] != "rg-target" ||
				fake.calls[1].request["ResourceId"] != "i-123" ||
				fake.calls[1].request["ResourceType"] != "instance" ||
				fake.calls[1].request["Service"] != "ecs" ||
				fake.calls[1].request["Region"] != "cn-hangzhou" {
				t.Fatalf("calls = %#v, want exact reconciliation of the known successful resource", fake.calls)
			}
			actions, _ := decodeObject(t, stdout)["actions"].([]any)
			if len(actions) != 3 {
				t.Fatalf("actions = %#v, want success, invalid response, and reconciliation; stdout=%s", actions, stdout)
			}
			reconciliation, _ := actions[2].(map[string]any)
			if reconciliation["action_name"] != "ListResources" || reconciliation["request_id"] != "req-list-success" {
				t.Fatalf("reconciliation action = %#v; stdout=%s", reconciliation, stdout)
			}
		})
	}
}

func TestRGResourceUpdateRetriesTransientPerResourceProbe(t *testing.T) {
	t.Parallel()
	transient := ecerrors.Service("CloudAPIError", "throttled", true, ecerrors.WithRawCause("Throttling", "slow down"))
	fake := &fakeSpecCaller{
		errors: []error{nil, nil, transient, nil, nil},
		responses: []map[string]any{
			{
				"RequestId": "req-move",
				"Responses": []any{
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"ResourceId":   "i-123",
						"ResourceType": "instance",
						"Service":      "ecs",
						"Status":       "SUCCESS",
					},
					map[string]any{
						"RegionId":     "cn-hangzhou",
						"ResourceId":   "d-456",
						"ResourceType": "disk",
						"Service":      "ecs",
						"Status":       "SUCCESS",
					},
				},
			},
			{
				"RequestId": "req-list-instance-1",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"ResourceGroupId": "rg-target",
						"ResourceId":      "i-123",
						"ResourceType":    "instance",
						"Service":         "ecs",
					},
				}},
			},
			{
				"RequestId": "req-list-instance-2",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"ResourceGroupId": "rg-target",
						"ResourceId":      "i-123",
						"ResourceType":    "instance",
						"Service":         "ecs",
					},
				}},
			},
			{
				"RequestId": "req-list-disk-2",
				"Resources": map[string]any{"Resource": []any{
					map[string]any{
						"ResourceGroupId": "rg-target",
						"ResourceId":      "d-456",
						"ResourceType":    "disk",
						"Service":         "ecs",
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "resource", "update", "rg-target",
		"--region", "cn-hangzhou",
		"--resource", "resource-id=i-123,resource-type=instance,service=ecs,region-id=cn-hangzhou",
		"--resource", "resource-id=d-456,resource-type=disk,service=ecs,region-id=cn-hangzhou",
		"--timeout", "10ms",
	)
	if code != 0 {
		t.Fatalf("rg resource transient waiter exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 5 ||
		fake.calls[2].operation != "ListResources" ||
		fake.calls[2].request["ResourceId"] != "d-456" ||
		fake.calls[3].request["ResourceId"] != "i-123" ||
		fake.calls[4].request["ResourceId"] != "d-456" {
		t.Fatalf("calls = %#v, want the full exact probe set retried after transient failure", fake.calls)
	}
	actions, _ := decodeObject(t, stdout)["actions"].([]any)
	if len(actions) != 5 {
		t.Fatalf("actions = %#v, want every initial, failed, and retried cloud API call; stdout=%s", actions, stdout)
	}
	failedProbe, _ := actions[2].(map[string]any)
	if failedProbe["action_name"] != "ListResources" || failedProbe["code"] != "Throttling" {
		t.Fatalf("failed probe action = %#v; stdout=%s", failedProbe, stdout)
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
