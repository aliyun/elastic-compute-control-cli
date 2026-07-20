package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func fakeAssociatedTransferSettingResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-list",
		"AssociatedTransferSetting": map[string]any{
			"AccountId":                       "123456789",
			"Status":                          "Enable",
			"EnableExistingResourcesTransfer": "true",
			"RuleSettings": map[string]any{
				"RuleSetting": []any{
					map[string]any{
						"AssociatedResourceType": "disk",
						"AssociatedService":      "ecs",
						"MasterResourceType":     "instance",
						"MasterService":          "ecs",
						"Status":                 "Enable",
					},
				},
			},
		},
	}
}

func TestRGAssociatedTransferEnableCallsEnableAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-enable"},
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "associated-transfer", "enable", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg associated-transfer enable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "EnableAssociatedTransfer" || fake.calls[1].operation != "ListAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["associated_transfer_setting"].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGAssociatedTransferDisableCallsDisableAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-disable"},
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "associated-transfer", "disable", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg associated-transfer disable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DisableAssociatedTransfer" || fake.calls[1].operation != "ListAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	setting, _ := decodeObject(t, stdout)["associated_transfer_setting"].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGAssociatedTransferUpdateCallsUpdateAndReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "associated-transfer", "update", "--region", "cn-beijing", "--status", "Enable")
	if code != 0 {
		t.Fatalf("rg associated-transfer update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateAssociatedTransferSetting" || fake.calls[1].operation != "ListAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["Status"] != "Enable" {
		t.Fatalf("update request = %#v", fake.calls[0].request)
	}
	setting, _ := decodeObject(t, stdout)["associated_transfer_setting"].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGAssociatedTransferUpdateWithRuleSettingMapsFields(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "associated-transfer", "update", "--region", "cn-beijing",
		"--status", "Enable",
		"--rule-setting", "associated-resource-type=disk,associated-service=ecs,master-resource-type=instance,master-service=ecs,status=Enable",
	)
	if code != 0 {
		t.Fatalf("rg associated-transfer update with rule-setting exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Status"] != "Enable" {
		t.Fatalf("top-level Status missing: %#v", request)
	}
	if request["RuleSettings.1.AssociatedResourceType"] != "disk" ||
		request["RuleSettings.1.AssociatedService"] != "ecs" ||
		request["RuleSettings.1.MasterResourceType"] != "instance" ||
		request["RuleSettings.1.MasterService"] != "ecs" ||
		request["RuleSettings.1.Status"] != "Enable" {
		t.Fatalf("RuleSettings fields not mapped correctly: %#v", request)
	}
	setting, _ := decodeObject(t, stdout)["associated_transfer_setting"].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestRGAssociatedTransferListCallsListAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "associated-transfer", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg associated-transfer list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	items, _ := out["associated_transfer_settings"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
	setting, _ := items[0].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected setting: %#v; stdout=%s", setting, stdout)
	}
}

func TestRGAssociatedTransferAlias(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeAssociatedTransferSettingResponse(),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.Resource != "associated-transfer" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %#v, want rg/associated-transfer with ResourceManager API product", resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("rg", "at", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg at list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListAssociatedTransferSetting" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	items, _ := out["associated_transfer_settings"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
	setting, _ := items[0].(map[string]any)
	if setting == nil || setting["status"] != "Enable" {
		t.Fatalf("unexpected setting: %#v; stdout=%s", setting, stdout)
	}
}
