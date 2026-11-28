package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func acrInstanceCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "acr" || resource.Resource != "instance" || resource.APIProduct != "cr" {
			t.Fatalf("resource = %#v, want acr/instance with cr API product", resource)
		}
		return fake, nil
	})
}

func fakeACRInstance(id, name, status, spec string) map[string]any {
	return map[string]any{
		"InstanceId":            id,
		"InstanceName":          name,
		"InstanceStatus":        status,
		"InstanceSpecification": spec,
		"RegionId":              "cn-beijing",
		"ResourceGroupId":       "rg-acfm2xwmxvrzq6q",
		"CreateTime":            "1571926439000",
		"ModifiedTime":          "1571926440000",
		"Tags": []any{
			map[string]any{"TagKey": "env", "TagValue": "prod"},
		},
	}
}

func TestACRInstanceListUsesListInstance(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 1,
		"Instances":  []any{fakeACRInstance("cri-123", "test12333", "RUNNING", "Enterprise_Basic")},
	}}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, stderr, code := runCLI("acr", "instance", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("acr instance list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListInstance" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["PageNo"] != 1 || request["PageSize"] != 100 {
		t.Fatalf("list request = %#v", request)
	}
	if _, ok := request["InstanceStatus"]; ok {
		t.Fatalf("unfiltered list must not send InstanceStatus: %#v", request)
	}
	out := decodeObject(t, stdout)
	instances, _ := out["instances"].([]any)
	if out["total"] != float64(1) || len(instances) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	first, _ := instances[0].(map[string]any)
	if first["id"] != "cri-123" || first["name"] != "test12333" || first["status"] != "RUNNING" ||
		first["spec"] != "Enterprise_Basic" || first["region"] != "cn-beijing" ||
		first["resource_group"] != "rg-acfm2xwmxvrzq6q" || first["create_time"] != "1571926439000" {
		t.Fatalf("instance = %#v; stdout=%s", first, stdout)
	}
	tags, _ := first["tags"].(map[string]any)
	if tags["env"] != "prod" {
		t.Fatalf("tags = %#v; stdout=%s", first["tags"], stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["page"] != float64(1) || pagination["limit"] != float64(100) || pagination["returned"] != float64(1) {
		t.Fatalf("pagination = %#v; stdout=%s", pagination, stdout)
	}
}

func TestACRInstanceListMapsFiltersAndPage(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 2,
		"Instances":  []any{fakeACRInstance("cri-123", "test12333", "RUNNING", "Enterprise_Basic")},
	}}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, stderr, code := runCLI(
		"acr", "instance", "list", "--region", "cn-beijing",
		"--filter", "status=RUNNING",
		"--filter", "resource-group=rg-acfm2xwmxvrzq6q",
		"--page", "1", "--limit", "1",
	)
	if code != 0 {
		t.Fatalf("acr instance list filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	request := fake.calls[0].request
	if request["InstanceStatus"] != "RUNNING" || request["ResourceGroupId"] != "rg-acfm2xwmxvrzq6q" ||
		request["PageNo"] != 1 || request["PageSize"] != 1 {
		t.Fatalf("filtered list request = %#v", request)
	}
	out := decodeObject(t, stdout)
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["has_more"] != true || pagination["next_page"] != float64(2) {
		t.Fatalf("pagination = %#v; stdout=%s", pagination, stdout)
	}
}

func TestACRInstanceListEmptyReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-empty",
		"TotalCount": 0,
		"Instances":  []any{},
	}}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, stderr, code := runCLI("acr", "instance", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("acr instance list empty exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	instances, ok := out["instances"].([]any)
	if !ok || len(instances) != 0 || out["total"] != float64(0) {
		t.Fatalf("empty list output = %#v; stdout=%s", out, stdout)
	}
}

func TestACRInstanceGetUsesGetInstance(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":             "req-get",
		"InstanceId":            "cri-123",
		"InstanceName":          "test12333",
		"InstanceStatus":        "RUNNING",
		"InstanceSpecification": "Enterprise_Basic",
		"ResourceGroupId":       "rg-acfm2xwmxvrzq6q",
		"CreateTime":            1571926439000,
		"ModifiedTime":          1571926440000,
		"Tags": []any{
			map[string]any{"TagKey": "env", "TagValue": "prod"},
		},
	}}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, stderr, code := runCLI("acr", "instance", "get", "cri-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("acr instance get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetInstance" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InstanceId"] != "cri-123" {
		t.Fatalf("get request = %#v", fake.calls[0].request)
	}
	instance, _ := decodeObject(t, stdout)["instance"].(map[string]any)
	if instance == nil || instance["id"] != "cri-123" || instance["status"] != "RUNNING" ||
		instance["spec"] != "Enterprise_Basic" || instance["resource_group"] != "rg-acfm2xwmxvrzq6q" {
		t.Fatalf("instance = %#v; stdout=%s", instance, stdout)
	}
	if instance["create_time"] != float64(1571926439000) {
		t.Fatalf("create_time = %#v; stdout=%s", instance["create_time"], stdout)
	}
	if _, ok := instance["region"]; ok {
		t.Fatalf("GetInstance output must not carry region: %#v", instance)
	}
	tags, _ := instance["tags"].(map[string]any)
	if tags["env"] != "prod" {
		t.Fatalf("tags = %#v; stdout=%s", instance["tags"], stdout)
	}
}

func TestACRInstanceGetEmptyResponseReturnsNotFound(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-missing",
		"InstanceId": "",
	}}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, _, code := runCLI("acr", "instance", "get", "cri-missing", "--region", "cn-beijing")
	if code != 4 {
		t.Fatalf("acr instance get missing exit %d, want 4; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "NotFound" {
		t.Fatalf("error.code = %q, want NotFound; stdout=%s", got, stdout)
	}
}

func TestACRInstanceGetNotFoundPassesThrough(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{errors: []error{
		ecerrors.NotFound("InstanceNotFound", "instance cri-missing does not exist", ecerrors.WithRequestID("req-nf")),
	}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, _, code := runCLI("acr", "instance", "get", "cri-missing", "--region", "cn-beijing")
	if code != 4 {
		t.Fatalf("acr instance get not found exit %d, want 4; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "InstanceNotFound" {
		t.Fatalf("error.code = %q, want InstanceNotFound; stdout=%s", got, stdout)
	}
}

func TestACRInstanceGetServiceErrorPassesThrough(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{errors: []error{
		ecerrors.Service("SystemError", "internal system error", false, ecerrors.WithRequestID("req-sys")),
	}}
	runCLI := acrInstanceCaller(t, fake)

	stdout, _, code := runCLI("acr", "instance", "get", "cri-123", "--region", "cn-beijing")
	if code != 2 {
		t.Fatalf("acr instance get system error exit %d, want 2; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "SystemError" {
		t.Fatalf("error.code = %q, want SystemError; stdout=%s", got, stdout)
	}
	actions, _ := decodeObject(t, stdout)["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %#v; stdout=%s", actions, stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["action_name"] != "GetInstance" || action["request_id"] != "req-sys" {
		t.Fatalf("action = %#v; stdout=%s", action, stdout)
	}
}
