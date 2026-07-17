package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

const associatedRuleName = "rule:AttachEni-DetachEni-TagInstance:Ecs-Instance:Ecs-Eni"

func fakeAssociatedResourceRulesResponse(requestID string, nextToken string, status string) map[string]any {
	response := map[string]any{
		"RequestId": requestID,
		"Rules": []any{
			map[string]any{
				"SettingName": associatedRuleName,
				"Status":      status,
				"TagKeys":     []any{"env", "owner"},
			},
		},
	}
	if nextToken != "" {
		response["NextToken"] = nextToken
	}
	return response
}

func associatedResourceRuleCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "associated-resource-rule" {
			t.Fatalf("resource = %s/%s, want tag/associated-resource-rule", resource.Product, resource.Resource)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})
}

func TestTagAssociatedResourceRuleListUsesAliasAndTokenPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeAssociatedResourceRulesResponse("req-list", "next-token", "Enable"),
		},
	}
	runCLI := associatedResourceRuleCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "arr", "list", associatedRuleName, "--region", "cn-hangzhou", "--filter", "status=Enable", "--limit", "100")
	if code != 0 {
		t.Fatalf("tag arr list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListAssociatedResourceRules" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"RegionId":      "cn-hangzhou",
		"SettingName.1": associatedRuleName,
		"Status":        "Enable",
		"MaxResult":     100,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}

	out := decodeObject(t, stdout)
	rules, _ := out["associated_resource_rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("associated_resource_rules len = %d, want 1; stdout=%s", len(rules), stdout)
	}
	rule, _ := rules[0].(map[string]any)
	if rule["setting_name"] != associatedRuleName || rule["status"] != "Enable" {
		t.Fatalf("rule = %#v", rule)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil || pagination["next_token"] != "next-token" || pagination["has_more"] != true {
		t.Fatalf("pagination = %#v; stdout=%s", pagination, stdout)
	}
	if _, ok := pagination["page"]; ok {
		t.Fatalf("token pagination must not include page: %#v", pagination)
	}
	if _, ok := pagination["next_page"]; ok {
		t.Fatalf("token pagination must not include next_page: %#v", pagination)
	}
}

func TestTagAssociatedResourceRuleCreateMapsRuleListWithoutReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create"},
		},
	}
	runCLI := associatedResourceRuleCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "associated-resource-rule", "create",
		"--region", "cn-hangzhou",
		"--setting-name", associatedRuleName,
		"--tag-keys", "env,owner",
		"--existing-status", "Enable",
	)
	if code != 0 {
		t.Fatalf("tag associated-resource-rule create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateAssociatedResourceRules" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"RegionId":                         "cn-hangzhou",
		"CreateRulesList.1.SettingName":    associatedRuleName,
		"CreateRulesList.1.Status":         "Enable",
		"CreateRulesList.1.TagKeys.1":      "env",
		"CreateRulesList.1.TagKeys.2":      "owner",
		"CreateRulesList.1.ExistingStatus": "Enable",
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}

	out := decodeObject(t, stdout)
	rule, _ := out["associated_resource_rule"].(map[string]any)
	if rule["setting_name"] != associatedRuleName || rule["status"] != "Enable" || rule["existing_status"] != "Enable" {
		t.Fatalf("associated_resource_rule = %#v; stdout=%s", rule, stdout)
	}
}

func TestTagAssociatedResourceRuleUpdateReadsBackRule(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			fakeAssociatedResourceRulesResponse("req-list", "", "Disable"),
		},
	}
	runCLI := associatedResourceRuleCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "associated-resource-rule", "update", associatedRuleName,
		"--region", "cn-hangzhou",
		"--status", "Disable",
		"--tag-keys", "env,owner",
		"--existing-status", "Enable",
	)
	if code != 0 {
		t.Fatalf("tag associated-resource-rule update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateAssociatedResourceRule" || fake.calls[1].operation != "ListAssociatedResourceRules" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"RegionId":       "cn-hangzhou",
		"SettingName":    associatedRuleName,
		"Status":         "Disable",
		"TagKeys.1":      "env",
		"TagKeys.2":      "owner",
		"ExistingStatus": "Enable",
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
	if fake.calls[1].request["SettingName.1"] != associatedRuleName {
		t.Fatalf("readback request = %#v", fake.calls[1].request)
	}

	out := decodeObject(t, stdout)
	rule, _ := out["associated_resource_rule"].(map[string]any)
	if rule["setting_name"] != associatedRuleName || rule["status"] != "Disable" {
		t.Fatalf("associated_resource_rule = %#v; stdout=%s", rule, stdout)
	}
}

func TestTagAssociatedResourceRuleUpdateAllowsExistingStatusOnly(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeAssociatedResourceRulesResponse("req-before-list", "", "Disable"),
			{"RequestId": "req-update-existing"},
			fakeAssociatedResourceRulesResponse("req-list", "", "Enable"),
		},
	}
	runCLI := associatedResourceRuleCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "associated-resource-rule", "update", associatedRuleName,
		"--region", "cn-hangzhou",
		"--existing-status", "Enable",
	)
	if code != 0 {
		t.Fatalf("tag associated-resource-rule update existing status exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "ListAssociatedResourceRules" ||
		fake.calls[1].operation != "UpdateAssociatedResourceRule" || fake.calls[2].operation != "ListAssociatedResourceRules" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[1].request
	if request["SettingName"] != associatedRuleName || request["ExistingStatus"] != "Enable" ||
		request["Status"] != "Disable" || request["TagKeys.1"] != "env" || request["TagKeys.2"] != "owner" {
		t.Fatalf("request = %#v", request)
	}
	if fake.calls[0].request["SettingName.1"] != associatedRuleName {
		t.Fatalf("pre-update read request = %#v", fake.calls[0].request)
	}
}

func TestTagAssociatedResourceRuleDeleteMapsSettingName(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := associatedResourceRuleCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "associated-resource-rule", "delete", associatedRuleName, "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("tag associated-resource-rule delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteAssociatedResourceRule" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["RegionId"] != "cn-hangzhou" || request["SettingName"] != associatedRuleName {
		t.Fatalf("request = %#v", request)
	}

	out := decodeObject(t, stdout)
	if out["deleted"] != true {
		t.Fatalf("deleted = %#v; stdout=%s", out["deleted"], stdout)
	}
	rule, _ := out["associated_resource_rule"].(map[string]any)
	if rule["setting_name"] != associatedRuleName {
		t.Fatalf("associated_resource_rule = %#v; stdout=%s", rule, stdout)
	}
}
