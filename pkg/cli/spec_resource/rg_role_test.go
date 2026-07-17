package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestRGRoleCreateUsesResourceManagerSpec(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Role": map[string]any{
					"Arn":                      "acs:ram::123456789:role/my-role",
					"AssumeRolePolicyDocument": `{"Statement":[]}`,
					"CreateDate":               "2025-06-01T00:00:00Z",
					"Description":              "test role",
					"MaxSessionDuration":       float64(3600),
					"RoleId":                   "role-abc123",
					"RoleName":                 "my-role",
					"RolePrincipalName":        "123456789@role.China",
				},
			},
			fakeRoleResponse("my-role"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "role" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/role with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "role", "create",
		"--region", "cn-beijing",
		"--name", "my-role",
		"--assume-role-policy-document", `{"Statement":[]}`,
		"--description", "test role",
		"--max-session-duration", "3600",
	)
	if code != 0 {
		t.Fatalf("rg role create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateRole" || fake.calls[1].operation != "GetRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["RoleName"] != "my-role" || req["Description"] != "test role" || req["AssumeRolePolicyDocument"] != `{"Statement":[]}` {
		t.Fatalf("create request = %#v", req)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil || role["name"] != "my-role" || role["role_id"] != "role-abc123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGRoleUpdateCallsUpdateRoleAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-update",
				"Role": map[string]any{
					"RoleName":    "my-role",
					"Description": "updated desc",
				},
			},
			fakeRoleResponse("my-role"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "role" {
			t.Fatalf("resource = %s/%s, want rg/role", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "role", "update", "my-role", "--region", "cn-beijing", "--description", "updated desc")
	if code != 0 {
		t.Fatalf("rg role update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateRole" || fake.calls[1].operation != "GetRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["RoleName"] != "my-role" || fake.calls[0].request["NewDescription"] != "updated desc" {
		t.Fatalf("update request = %#v", fake.calls[0].request)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil || role["name"] != "my-role" || role["role_id"] != "role-abc123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGRoleDeleteCallsDeleteRole(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-delete"},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "role" {
			t.Fatalf("resource = %s/%s, want rg/role", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "role", "delete", "my-role", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg role delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["RoleName"] != "my-role" {
		t.Fatalf("delete request = %#v", fake.calls[0].request)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil || role["name"] != "my-role" {
		t.Fatalf("delete output missing role name: %s", stdout)
	}
}

func TestRGRoleGetCallsGetRole(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeRoleResponse("my-role"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "role" {
			t.Fatalf("resource = %s/%s, want rg/role", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "role", "get", "my-role", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg role get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["RoleName"] != "my-role" {
		t.Fatalf("get request = %#v", fake.calls[0].request)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if role["name"] != "my-role" {
		t.Fatalf("role.name = %v, want my-role; stdout=%s", role["name"], stdout)
	}
	if role["arn"] != "acs:ram::123456789:role/my-role" {
		t.Fatalf("role.arn = %v; stdout=%s", role["arn"], stdout)
	}
	if role["role_id"] != "role-abc123" {
		t.Fatalf("role.role_id = %v; stdout=%s", role["role_id"], stdout)
	}
	if role["description"] != "test role" {
		t.Fatalf("role.description = %v; stdout=%s", role["description"], stdout)
	}
	if role["max_session_duration"] != float64(3600) {
		t.Fatalf("role.max_session_duration = %v; stdout=%s", role["max_session_duration"], stdout)
	}
	if role["create_date"] != "2025-06-01T00:00:00Z" {
		t.Fatalf("role.create_date = %v; stdout=%s", role["create_date"], stdout)
	}
}

func TestRGRoleListCallsListRoles(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"PageNumber": 2,
				"PageSize":   50,
				"Roles": map[string]any{
					"Role": []any{
						map[string]any{
							"Arn":                 "acs:ram::123456789:role/my-role",
							"CreateDate":          "2025-06-01T00:00:00Z",
							"Description":         "test role",
							"IsServiceLinkedRole": false,
							"MaxSessionDuration":  float64(3600),
							"RoleId":              "role-abc123",
							"RoleName":            "my-role",
							"RolePrincipalName":   "123456789@role.China",
							"UpdateDate":          "2025-06-02T00:00:00Z",
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "role" {
			t.Fatalf("resource = %s/%s, want rg/role", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "role", "list", "--region", "cn-beijing", "--limit", "50", "--page", "2")
	if code != 0 {
		t.Fatalf("rg role list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListRoles" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["PageSize"] != 50 || request["PageNumber"] != 2 {
		t.Fatalf("list request = %#v", request)
	}
	out := decodeObject(t, stdout)
	roles, _ := out["roles"].([]any)
	if out["total"] != float64(1) || len(roles) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	firstRole, _ := roles[0].(map[string]any)
	if firstRole["name"] != "my-role" || firstRole["role_id"] != "role-abc123" {
		t.Fatalf("role = %#v; stdout=%s", firstRole, stdout)
	}
}

func TestRGRoleHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "rg", "role", "create", "--help")
	if code != 0 {
		t.Fatalf("rg role create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, flag := range []string{"--name", "--assume-role-policy-document", "--description", "--max-session-duration"} {
		if !strings.Contains(stdout, flag) {
			t.Fatalf("create help missing %s:\n%s", flag, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "role", "list", "--help")
	if code != 0 {
		t.Fatalf("rg role list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, flag := range []string{"--limit", "--page"} {
		if !strings.Contains(stdout, flag) {
			t.Fatalf("list help missing %s:\n%s", flag, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "role", "update", "--help")
	if code != 0 {
		t.Fatalf("rg role update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, flag := range []string{"--description", "--assume-role-policy-document", "--max-session-duration"} {
		if !strings.Contains(stdout, flag) {
			t.Fatalf("update help missing %s:\n%s", flag, stdout)
		}
	}
}

func fakeRoleResponse(name string) map[string]any {
	return map[string]any{
		"RequestId": "req-get",
		"Role": map[string]any{
			"Arn":                      "acs:ram::123456789:role/" + name,
			"AssumeRolePolicyDocument": `{"Statement":[]}`,
			"CreateDate":               "2025-06-01T00:00:00Z",
			"Description":              "test role",
			"IsServiceLinkedRole":      false,
			"MaxSessionDuration":       float64(3600),
			"RoleId":                   "role-abc123",
			"RoleName":                 name,
			"RolePrincipalName":        "123456789@role.China",
			"UpdateDate":               "2025-06-02T00:00:00Z",
		},
	}
}
