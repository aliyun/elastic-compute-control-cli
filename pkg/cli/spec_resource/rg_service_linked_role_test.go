package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestRGServiceLinkedRoleCreateCallsCreateServiceLinkedRole(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Role": map[string]any{
					"Arn":                      "acs:ram::123456789:role/AliyunServiceRoleForPolarDB",
					"AssumeRolePolicyDocument": "{\"Statement\":[]}",
					"CreateDate":               "2025-01-02T03:04:05Z",
					"Description":              "China Site China Mainland",
					"IsServiceLinkedRole":      true,
					"RoleId":                   "role-123",
					"RoleName":                 "AliyunServiceRoleForPolarDB",
					"RolePrincipalName":        "polardb.aliyuncs.com",
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "service-linked-role" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/service-linked-role with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "service-linked-role", "create", "--region", "cn-beijing", "--service-name", "polardb.aliyuncs.com")
	if code != 0 {
		t.Fatalf("rg service-linked-role create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateServiceLinkedRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ServiceName"] != "polardb.aliyuncs.com" {
		t.Fatalf("create request = %#v", fake.calls[0].request)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil || role["service_name"] != "polardb.aliyuncs.com" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGServiceLinkedRoleDeleteCallsDeleteAndWaits(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":      "req-delete",
				"DeletionTaskId": "task-abc",
			},
			{
				"RequestId": "req-status",
				"Status":    "SUCCEEDED",
				"Reason":    nil,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "service-linked-role" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/service-linked-role with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "service-linked-role", "delete", "AliyunServiceRoleForPolarDB", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg service-linked-role delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %#v", fake.calls)
	}
	if fake.calls[0].operation != "DeleteServiceLinkedRole" {
		t.Fatalf("first call = %s, want DeleteServiceLinkedRole", fake.calls[0].operation)
	}
	if fake.calls[0].request["RoleName"] != "AliyunServiceRoleForPolarDB" {
		t.Fatalf("delete request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].operation != "GetServiceLinkedRoleDeletionStatus" {
		t.Fatalf("second call = %s, want GetServiceLinkedRoleDeletionStatus", fake.calls[1].operation)
	}
	if fake.calls[1].request["DeletionTaskId"] != "task-abc" {
		t.Fatalf("status request = %#v", fake.calls[1].request)
	}
	role, _ := decodeObject(t, stdout)["role"].(map[string]any)
	if role == nil || role["name"] != "AliyunServiceRoleForPolarDB" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGServiceLinkedRoleAliasWorks(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Role": map[string]any{
					"RoleName":          "AliyunServiceRoleForPolarDB",
					"RolePrincipalName": "polardb.aliyuncs.com",
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "service-linked-role" {
			t.Fatalf("resource = %s/%s, want rg/service-linked-role", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "slr", "create", "--region", "cn-beijing", "--service-name", "polardb.aliyuncs.com")
	if code != 0 {
		t.Fatalf("rg slr create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateServiceLinkedRole" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestRGServiceLinkedRoleHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "rg", "service-linked-role", "create", "--help")
	if code != 0 {
		t.Fatalf("rg service-linked-role create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--service-name") {
		t.Fatalf("create help missing --service-name:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--custom-suffix") {
		t.Fatalf("create help missing --custom-suffix:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--description") {
		t.Fatalf("create help missing --description:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "service-linked-role", "delete", "--help")
	if code != 0 {
		t.Fatalf("rg service-linked-role delete --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "NAME") && !strings.Contains(stdout, "name") {
		t.Fatalf("delete help missing name argument:\n%s", stdout)
	}
}
