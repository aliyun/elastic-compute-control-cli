package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestRGGroupCreateUsesResourceManagerSpec(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"ResourceGroup": map[string]any{
					"Id":          "rg-123",
					"Name":        "prod-rg",
					"DisplayName": "Production",
					"Status":      "OK",
				},
			},
			fakeResourceGroupResponse("rg-123", "prod-rg", "Production"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/group with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "create", "--region", "cn-beijing", "--name", "prod-rg", "--display-name", "Production")
	if code != 0 {
		t.Fatalf("rg group create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateResourceGroup" || fake.calls[1].operation != "GetResourceGroup" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["Name"] != "prod-rg" || fake.calls[0].request["DisplayName"] != "Production" {
		t.Fatalf("create request = %#v", fake.calls[0].request)
	}
	group, _ := decodeObject(t, stdout)["group"].(map[string]any)
	if group == nil || group["id"] != "rg-123" || group["name"] != "prod-rg" || group["display_name"] != "Production" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGGroupGetWithCountsUsesCountsProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeResourceGroupResponse("rg-123", "prod-rg", "Production"),
			{
				"RequestId": "req-counts",
				"ResourceCounts": []any{
					map[string]any{"Count": float64(3), "GroupByKey": "ResourceGroupId", "ResourceGroupId": "rg-123"},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "get", "rg-123", "--region", "cn-beijing", "--with-counts")
	if code != 0 {
		t.Fatalf("rg group get --with-counts exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "GetResourceGroup" || fake.calls[1].operation != "GetResourceGroupResourceCounts" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["ResourceGroupId"] != "rg-123" || fake.calls[1].request["GroupByKey"] != "ResourceGroupId" {
		t.Fatalf("counts request = %#v", fake.calls[1].request)
	}
	group, _ := decodeObject(t, stdout)["group"].(map[string]any)
	counts, _ := group["resource_counts"].([]any)
	if len(counts) != 1 {
		t.Fatalf("resource_counts = %#v; stdout=%s", group["resource_counts"], stdout)
	}
	first, _ := counts[0].(map[string]any)
	if first["count"] != float64(3) || first["resource_group_id"] != "rg-123" {
		t.Fatalf("count = %#v; stdout=%s", first, stdout)
	}
}

func TestRGGroupListUsesListResourceGroupsByDefault(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"ResourceGroups": map[string]any{
					"ResourceGroup": []any{
						map[string]any{
							"Id":          "rg-123",
							"Name":        "prod-rg",
							"DisplayName": "Production",
							"Status":      "OK",
							"AccountId":   "123456789",
							"CreateDate":  "2025-01-02T03:04:05Z",
							"Tags": map[string]any{
								"Tag": []any{
									map[string]any{"TagKey": "env", "TagValue": "prod"},
								},
							},
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "list", "--region", "cn-beijing", "--filter", "name=prod-rg")
	if code != 0 {
		t.Fatalf("rg group list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResourceGroups" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Name"] != "prod-rg" || request["PageSize"] != 100 || request["PageNumber"] != 1 {
		t.Fatalf("list request = %#v", request)
	}
	out := decodeObject(t, stdout)
	groups, _ := out["groups"].([]any)
	if out["total"] != float64(1) || len(groups) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	firstGroup, _ := groups[0].(map[string]any)
	if firstGroup["id"] != "rg-123" || firstGroup["name"] != "prod-rg" {
		t.Fatalf("group = %#v; stdout=%s", firstGroup, stdout)
	}
}

func TestRGGroupListPassesIDAndTagFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"RequestId":      "req-list",
			"TotalCount":     0,
			"ResourceGroups": map[string]any{"ResourceGroup": []any{}},
		}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "group", "list", "rg-123", "rg-456",
		"--region", "cn-beijing",
		"--filter", "tag.env=prod",
		"--filter", "tag.team=platform",
	)
	if code != 0 {
		t.Fatalf("rg group list ids/tags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResourceGroups" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	requireStringValues(t, request["ResourceGroupIds"], []string{"rg-123", "rg-456"})
	requireStringValues(t, request["Tag"], []string{"env=prod", "team=platform"})
}

func TestRGGroupListWithAuthDetailsUsesAuthAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"ResourceGroups": []any{
					map[string]any{
						"Id":          "rg-123",
						"Name":        "prod-rg",
						"DisplayName": "Production",
						"Status":      "OK",
						"AccountId":   "123456789",
						"CreateDate":  "2025-01-02T03:04:05Z",
						"Tags": []any{
							map[string]any{"TagKey": "env", "TagValue": "prod"},
						},
					},
				},
				"AuthDetails": []any{
					map[string]any{
						"AccountScopeAuth": true,
						"AuthOfResourceGroups": []any{
							map[string]any{"ResourceGroupId": "rg-123"},
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "list", "--region", "cn-beijing", "--with-auth-details", "--filter", "status=OK", "--limit", "20", "--page", "2")
	if code != 0 {
		t.Fatalf("rg group list --with-auth-details exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResourceGroupsWithAuthDetails" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Status"] != "OK" || request["PageSize"] != 20 || request["PageNumber"] != 2 {
		t.Fatalf("list request = %#v", request)
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(1) {
		t.Fatalf("total = %#v; stdout=%s", out["total"], stdout)
	}
	groups, _ := out["groups"].([]any)
	authDetails, _ := out["auth_details"].([]any)
	if len(groups) != 1 || len(authDetails) != 1 {
		t.Fatalf("groups/auth_details = %#v/%#v; stdout=%s", groups, authDetails, stdout)
	}
	firstGroup, _ := groups[0].(map[string]any)
	if firstGroup["id"] != "rg-123" || firstGroup["display_name"] != "Production" {
		t.Fatalf("group = %#v; stdout=%s", firstGroup, stdout)
	}
	firstAuth, _ := authDetails[0].(map[string]any)
	if firstAuth["account_scope_auth"] != true {
		t.Fatalf("auth_details = %#v; stdout=%s", firstAuth, stdout)
	}
}

func TestRGGroupListWithAuthDetailsPassesIDAndTagFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"RequestId":      "req-list",
			"TotalCount":     0,
			"ResourceGroups": []any{},
			"AuthDetails":    []any{},
		}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"rg", "group", "list", "rg-123", "rg-456",
		"--region", "cn-beijing",
		"--with-auth-details",
		"--filter", "tag.env=prod",
		"--filter", "tag.team=platform",
	)
	if code != 0 {
		t.Fatalf("rg group list --with-auth-details ids/tags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResourceGroupsWithAuthDetails" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	requireStringValues(t, request["ResourceGroupIds"], []string{"rg-123", "rg-456"})
	requireStringValues(t, request["Tag"], []string{"env=prod", "team=platform"})
}

func TestRGGroupUpdateAndDeleteUseResourceGroupAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			fakeResourceGroupResponse("rg-123", "prod-rg", "Production 2"),
			{"RequestId": "req-delete"},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "update", "rg-123", "--region", "cn-beijing", "--display-name", "Production 2")
	if code != 0 {
		t.Fatalf("rg group update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateResourceGroup" || fake.calls[1].operation != "GetResourceGroup" {
		t.Fatalf("update calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ResourceGroupId"] != "rg-123" || fake.calls[0].request["NewDisplayName"] != "Production 2" {
		t.Fatalf("update request = %#v", fake.calls[0].request)
	}

	stdout, stderr, code = runCLI("rg", "group", "delete", "rg-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg group delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[2].operation != "DeleteResourceGroup" {
		t.Fatalf("delete calls = %#v", fake.calls)
	}
	if fake.calls[2].request["ResourceGroupId"] != "rg-123" {
		t.Fatalf("delete request = %#v", fake.calls[2].request)
	}
	group, _ := decodeObject(t, stdout)["group"].(map[string]any)
	if group == nil || group["id"] != "rg-123" {
		t.Fatalf("delete output missing group id: %s", stdout)
	}
}

func TestRGGroupUpdateAllowsAPIParamEscapeHatch(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			fakeResourceGroupResponse("rg-123", "prod-rg", "Production"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "group" {
			t.Fatalf("resource = %s/%s, want rg/group", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "group", "update", "rg-123", "--region", "cn-beijing", "--api-param", "Foo=Bar")
	if code != 0 {
		t.Fatalf("rg group update --api-param exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateResourceGroup" || fake.calls[1].operation != "GetResourceGroup" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceGroupId"] != "rg-123" || request["Foo"] != "Bar" {
		t.Fatalf("update request = %#v", request)
	}
	if _, ok := request["NewDisplayName"]; ok {
		t.Fatalf("NewDisplayName should be omitted when only api-param is supplied: %#v", request)
	}
}

func TestUpdateAPIParamDoesNotBypassConditionalWorkflows(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, _, code := runCLI("ecs", "instance", "update", "i-123", "--region", "cn-beijing", "--api-param", "Foo=Bar")
	if code != 1 {
		t.Fatalf("ecs instance update --api-param exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("api-param-only conditional update should fail before API calls: %#v", fake.calls)
	}
}

func TestRGGroupHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "rg", "group", "get", "--help")
	if code != 0 {
		t.Fatalf("rg group get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--with-counts") {
		t.Fatalf("get help missing --with-counts:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "group", "list", "--help")
	if code != 0 {
		t.Fatalf("rg group list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--with-auth-details") || !strings.Contains(stdout, "--filter") {
		t.Fatalf("list help missing designed flags:\n%s", stdout)
	}
}

func fakeResourceGroupResponse(id, name, displayName string) map[string]any {
	return map[string]any{
		"RequestId": "req-get",
		"ResourceGroup": map[string]any{
			"Id":          id,
			"Name":        name,
			"DisplayName": displayName,
			"Status":      "OK",
			"AccountId":   "123456789",
			"CreateDate":  "2025-01-02T03:04:05Z",
		},
	}
}

func requireStringValues(t *testing.T, got any, want []string) {
	t.Helper()
	values, ok := got.([]string)
	if !ok {
		t.Fatalf("value = %#v, want []string%#v", got, want)
	}
	if len(values) != len(want) {
		t.Fatalf("value = %#v, want %#v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("value = %#v, want %#v", values, want)
		}
	}
}
