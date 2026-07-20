package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestRGNotificationEnableCallsEnableAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-enable",
			},
			{
				"RequestId":                             "req-get",
				"ResourceGroupNotificationEnableStatus": true,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "notification" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/notification with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "notification", "enable", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg notification enable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "EnableResourceGroupNotification" || fake.calls[1].operation != "GetResourceGroupNotificationSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["notification_setting"].(map[string]any)
	if setting == nil || setting["status"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGNotificationDisableCallsDisableAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-disable",
			},
			{
				"RequestId":                             "req-get",
				"ResourceGroupNotificationEnableStatus": false,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "notification" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/notification with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "notification", "disable", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg notification disable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DisableResourceGroupNotification" || fake.calls[1].operation != "GetResourceGroupNotificationSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["notification_setting"].(map[string]any)
	if setting == nil || setting["status"] != false {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGNotificationGetCallsGetAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":                             "req-get",
				"ResourceGroupNotificationEnableStatus": true,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "notification" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/notification with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "notification", "get", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg notification get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetResourceGroupNotificationSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["notification_setting"].(map[string]any)
	if setting == nil || setting["status"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGNotificationAlias(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":                             "req-get",
				"ResourceGroupNotificationEnableStatus": true,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "notification" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/notification with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "notify", "get", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg notify get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetResourceGroupNotificationSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["notification_setting"].(map[string]any)
	if setting == nil || setting["status"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
