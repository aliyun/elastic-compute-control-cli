package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestRGAdminSettingGet(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":      "req-get",
				"CreatorAsAdmin": true,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "admin-setting" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/admin-setting with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "admin-setting", "get", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg admin-setting get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetResourceGroupAdminSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["admin_setting"].(map[string]any)
	if setting == nil || setting["creator_as_admin"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGAdminSettingUpdate(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-update",
			},
			{
				"RequestId":      "req-get",
				"CreatorAsAdmin": true,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "admin-setting" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/admin-setting with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "admin-setting", "update", "--region", "cn-beijing", "--creator-as-admin=true")
	if code != 0 {
		t.Fatalf("rg admin-setting update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateResourceGroupAdminSetting" || fake.calls[1].operation != "GetResourceGroupAdminSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["CreatorAsAdmin"] != true {
		t.Fatalf("update request = %#v", fake.calls[0].request)
	}
	setting, _ := decodeObject(t, stdout)["admin_setting"].(map[string]any)
	if setting == nil || setting["creator_as_admin"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
