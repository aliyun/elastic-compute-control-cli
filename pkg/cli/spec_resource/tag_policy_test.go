package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestTagPolicyHelpIsGeneratedFromSpec(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "tag", "policy", "--help")
	if code != 0 {
		t.Fatalf("tag policy --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"list", "get", "create", "update", "delete", "attach", "detach"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("tag policy help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "tag", "policy", "get", "--help")
	if code != 0 {
		t.Fatalf("tag policy get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--with-status", "--with-effective", "--target-type", "--target"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("tag policy get help missing %q:\n%s", want, stdout)
		}
	}
}

func TestTagPolicyCreateUpdateDeleteUsePolicyAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "PolicyId": "p-123", "PolicyName": "cost"},
		{"RequestId": "req-update"},
		{"RequestId": "req-policy", "Policy": map[string]any{"PolicyName": "cost-2", "PolicyContent": `{"tags":{}}`, "UserType": "USER"}},
		{"RequestId": "req-delete"},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "create",
		"--region", "cn-shanghai",
		"--name", "cost",
		"--content", `{"tags":{}}`,
		"--user-type", "RD",
	)
	if code != 0 {
		t.Fatalf("tag policy create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "update", "p-123",
		"--region", "cn-shanghai",
		"--name", "cost-2",
		"--content", `{"tags":{}}`,
	)
	if code != 0 {
		t.Fatalf("tag policy update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "delete", "p-123", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("tag policy delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "CreatePolicy,ModifyPolicy,GetPolicy,DeletePolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PolicyName"] != "cost" || fake.calls[0].request["PolicyContent"] != `{"tags":{}}` || fake.calls[0].request["UserType"] != "RD" {
		t.Fatalf("CreatePolicy request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["PolicyId"] != "p-123" || fake.calls[1].request["PolicyName"] != "cost-2" {
		t.Fatalf("ModifyPolicy request = %#v", fake.calls[1].request)
	}
	if fake.calls[3].request["PolicyId"] != "p-123" {
		t.Fatalf("DeletePolicy request = %#v", fake.calls[3].request)
	}
}

func TestTagPolicyListSelectsPolicyTargetAndTargetPolicyAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-list",
			"NextToken": "next-1",
			"PolicyList": []any{
				map[string]any{"PolicyId": "p-1", "PolicyName": "cost", "UserType": "USER"},
			},
		},
		{
			"RequestId": "req-target-policies",
			"Data": []any{
				map[string]any{"PolicyId": "p-2", "PolicyName": "security", "UserType": "RD"},
			},
		},
		{
			"RequestId": "req-policy-targets",
			"Targets": []any{
				map[string]any{"TargetId": "154950938137****", "TargetType": "ACCOUNT"},
			},
			"IsRd": true,
			"RdId": "rd-123",
		},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "list", "--region", "cn-shanghai", "--filter", "name=cost")
	if code != 0 {
		t.Fatalf("tag policy list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	payload := decodeObject(t, stdout)
	pagination := payload["pagination"].(map[string]any)
	if pagination["next_token"] != "next-1" || pagination["has_more"] != true {
		t.Fatalf("pagination = %#v, want next_token next-1 and has_more true; stdout=%s", pagination, stdout)
	}
	if _, ok := pagination["page"]; ok {
		t.Fatalf("token pagination must not include page: %#v", pagination)
	}
	if _, ok := pagination["next_page"]; ok {
		t.Fatalf("token pagination must not include next_page: %#v", pagination)
	}

	stdout, stderr, code = runCLI("tag", "policy", "list",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
	)
	if code != 0 {
		t.Fatalf("tag policy list --target exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "list",
		"--region", "cn-shanghai",
		"--targets-for-policy", "p-2",
	)
	if code != 0 {
		t.Fatalf("tag policy list --targets-for-policy exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "ListPolicies,ListPoliciesForTarget,ListTargetsForPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PolicyNames.1"] != "cost" || fake.calls[0].request["MaxResult"] != 100 {
		t.Fatalf("ListPolicies request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["TargetId"] != "154950938137****" || fake.calls[1].request["TargetType"] != "ACCOUNT" {
		t.Fatalf("ListPoliciesForTarget request = %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["PolicyId"] != "p-2" {
		t.Fatalf("ListTargetsForPolicy request = %#v", fake.calls[2].request)
	}
}

func TestTagPolicyListTargetsForPolicyOutputsTargets(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-policy-targets",
		"Targets": []any{
			map[string]any{"TargetId": "154950938137****", "TargetType": "ACCOUNT"},
		},
	}}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "list",
		"--region", "cn-shanghai",
		"--targets-for-policy", "p-2",
	)
	if code != 0 {
		t.Fatalf("tag policy list --targets-for-policy exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	payload := decodeObject(t, stdout)
	if _, ok := payload["policies"]; ok {
		t.Fatalf("targets-for-policy output must not use policies root: %s", stdout)
	}
	targets, _ := payload["targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1; stdout=%s", len(targets), stdout)
	}
	target, _ := targets[0].(map[string]any)
	if target["id"] != "154950938137****" || target["target_type"] != "ACCOUNT" {
		t.Fatalf("target = %#v; stdout=%s", target, stdout)
	}
}

func TestTagPolicyListRejectsPolicyIDsWithRelationshipBranches(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := tagPolicyCaller(t, fake)

	stdout, _, code := runCLI("tag", "policy", "list", "p-1",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
	)
	if code != 1 {
		t.Fatalf("tag policy list id + target exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("validation should happen before API call: %#v", fake.calls)
	}

	stdout, _, code = runCLI("tag", "policy", "list", "p-1",
		"--region", "cn-shanghai",
		"--targets-for-policy", "p-2",
	)
	if code != 1 {
		t.Fatalf("tag policy list id + targets-for-policy exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("validation should happen before API call: %#v", fake.calls)
	}
}

func TestTagPolicyListValidatesTargetModeInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        []string
		wantCode    string
		wantMessage string
	}{
		{
			name:     "target requires target type",
			args:     []string{"tag", "policy", "list", "--region", "cn-shanghai", "--target", "154950938137****"},
			wantCode: "MissingParameter",
		},
		{
			name:     "target type requires target",
			args:     []string{"tag", "policy", "list", "--region", "cn-shanghai", "--target-type", "ACCOUNT"},
			wantCode: "MissingParameter",
		},
		{
			name:     "target mode rejects policy filters",
			args:     []string{"tag", "policy", "list", "--region", "cn-shanghai", "--target", "154950938137****", "--target-type", "ACCOUNT", "--filter", "name=cost"},
			wantCode: "ConflictingParameters",
		},
		{
			name:     "policy target mode rejects policy filters",
			args:     []string{"tag", "policy", "list", "--region", "cn-shanghai", "--targets-for-policy", "p-1", "--filter", "user-type=RD"},
			wantCode: "ConflictingParameters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, code := runCLI(tt.args...)
			if code != 1 {
				t.Fatalf("tag policy list exit %d, want 1; stdout=%s", code, stdout)
			}
			if got := errorCode(t, stdout); got != tt.wantCode {
				t.Fatalf("error.code = %q, want %s; stdout=%s", got, tt.wantCode, stdout)
			}
			if tt.wantMessage != "" && !strings.Contains(errorMessage(t, stdout), tt.wantMessage) {
				t.Fatalf("error.message must mention %s: %s", tt.wantMessage, stdout)
			}
		})
	}
}

func TestTagPolicyGetCanMergeStatusAndEffectivePolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-policy",
			"Policy": map[string]any{
				"PolicyName":    "cost",
				"PolicyDesc":    "cost policy",
				"PolicyContent": `{"tags":{}}`,
				"UserType":      "USER",
			},
		},
		{
			"RequestId": "req-status",
			"StatusModels": []any{
				map[string]any{"UserType": "USER", "Status": "Enabled"},
			},
		},
		{
			"RequestId":       "req-effective",
			"EffectivePolicy": `{"tags":{"costcenter":{}}}`,
			"PolicyAttachments": []any{
				map[string]any{"TagKey": "CostCenter"},
			},
		},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "get", "p-123",
		"--region", "cn-shanghai",
		"--with-status",
		"--with-effective",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
	)
	if code != 0 {
		t.Fatalf("tag policy get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	policy := decodeObject(t, stdout)["policy"].(map[string]any)
	if policy["name"] != "cost" || policy["effective_policy"] != `{"tags":{"costcenter":{}}}` {
		t.Fatalf("policy = %#v; stdout=%s", policy, stdout)
	}
	if _, ok := policy["status_models"].([]any); !ok {
		t.Fatalf("policy.status_models missing: %#v", policy)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "GetPolicy,GetPolicyEnableStatus,GetEffectivePolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestTagPolicyAttachDetachReadBackTargetPolicies(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
		{"RequestId": "req-list-after-attach", "Data": []any{
			map[string]any{"PolicyId": "p-1", "PolicyName": "cost"},
		}},
		{"RequestId": "req-detach"},
		{"RequestId": "req-list-after-detach", "Data": []any{}},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "attach", "p-1",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
	)
	if code != 0 {
		t.Fatalf("tag policy attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "detach", "p-1",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
	)
	if code != 0 {
		t.Fatalf("tag policy detach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy,ListPoliciesForTarget,DetachPolicy,ListPoliciesForTarget" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PolicyId"] != "p-1" || fake.calls[0].request["TargetId"] != "154950938137****" || fake.calls[0].request["TargetType"] != "ACCOUNT" {
		t.Fatalf("AttachPolicy request = %#v", fake.calls[0].request)
	}
	if fake.calls[2].request["PolicyId"] != "p-1" || fake.calls[2].request["TargetId"] != "154950938137****" || fake.calls[2].request["TargetType"] != "ACCOUNT" {
		t.Fatalf("DetachPolicy request = %#v", fake.calls[2].request)
	}
}

func TestTagPolicyAttachDetachCanOmitTargetForSingleAccountMode(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
		{"RequestId": "req-list-after-attach", "Data": []any{
			map[string]any{"PolicyId": "p-1", "PolicyName": "cost"},
		}},
		{"RequestId": "req-detach"},
		{"RequestId": "req-list-after-detach", "Data": []any{}},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "attach", "p-1", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("tag policy attach without target exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "detach", "p-1", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("tag policy detach without target exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy,ListPoliciesForTarget,DetachPolicy,ListPoliciesForTarget" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	for _, index := range []int{0, 1, 2, 3} {
		if _, ok := fake.calls[index].request["TargetId"]; ok {
			t.Fatalf("call %d should omit TargetId in single-account mode: %#v", index, fake.calls[index].request)
		}
		if _, ok := fake.calls[index].request["TargetType"]; ok {
			t.Fatalf("call %d should omit TargetType in single-account mode: %#v", index, fake.calls[index].request)
		}
	}
}

func TestTagPolicyAttachDetachValidateTargetPair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			name:        "attach target requires target type",
			args:        []string{"tag", "policy", "attach", "p-1", "--region", "cn-shanghai", "--target", "154950938137****"},
			wantMessage: "--target-type",
		},
		{
			name:        "detach target type requires target",
			args:        []string{"tag", "policy", "detach", "p-1", "--region", "cn-shanghai", "--target-type", "ACCOUNT"},
			wantMessage: "--target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, code := runCLI(tt.args...)
			if code != 1 {
				t.Fatalf("tag policy command exit %d, want 1; stdout=%s", code, stdout)
			}
			if got := errorCode(t, stdout); got != "MissingParameter" {
				t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
			}
			if !strings.Contains(errorMessage(t, stdout), tt.wantMessage) {
				t.Fatalf("error.message must mention %s: %s", tt.wantMessage, stdout)
			}
		})
	}
}

func TestTagPolicyAttachDetachNoWaitSkipsRelationshipReadBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
		{"RequestId": "req-list-after-attach", "Data": []any{
			map[string]any{"PolicyId": "p-1", "PolicyName": "cost"},
		}},
		{"RequestId": "req-detach"},
		{"RequestId": "req-list-after-detach", "Data": []any{}},
	}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "attach", "p-1",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("tag policy attach --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("tag", "policy", "detach", "p-1",
		"--region", "cn-shanghai",
		"--target", "154950938137****",
		"--target-type", "ACCOUNT",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("tag policy detach --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy,DetachPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestTagPolicyUpdateDryRunUsesDryRunPayload(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"DryRun": true}}}
	runCLI := tagPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("tag", "policy", "update", "p-123",
		"--region", "cn-shanghai",
		"--name", "cost-2",
		"--dry-run",
	)
	if code != 0 {
		t.Fatalf("tag policy update --dry-run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	payload := decodeObject(t, stdout)
	if payload["dry_run"] != "passed" || payload["requested_count"] != float64(1) || payload["available_count"] != float64(1) {
		t.Fatalf("dry-run payload = %#v; stdout=%s", payload, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "ModifyPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestTagPolicyAttachDetachNoWaitSkipsWaiter(t *testing.T) {
	t.Parallel()
	t.Run("attach", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{
			{"RequestId": "req-attach"},
			{"RequestId": "req-wait", "Data": []any{
				map[string]any{"PolicyId": "p-1", "PolicyName": "cost"},
			}},
		}}
		runCLI := tagPolicyCaller(t, fake)

		stdout, stderr, code := runCLI("tag", "policy", "attach", "p-1",
			"--region", "cn-shanghai",
			"--target", "154950938137****",
			"--target-type", "ACCOUNT",
			"--no-wait",
		)
		if code != 0 {
			t.Fatalf("tag policy attach --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy" {
			t.Fatalf("calls = %#v", fake.calls)
		}
	})

	t.Run("detach", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{
			{"RequestId": "req-detach"},
			{"RequestId": "req-wait", "Data": []any{}},
		}}
		runCLI := tagPolicyCaller(t, fake)

		stdout, stderr, code := runCLI("tag", "policy", "detach", "p-1",
			"--region", "cn-shanghai",
			"--target", "154950938137****",
			"--target-type", "ACCOUNT",
			"--no-wait",
		)
		if code != 0 {
			t.Fatalf("tag policy detach --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if got := callNames(fake.calls); strings.Join(got, ",") != "DetachPolicy" {
			t.Fatalf("calls = %#v", fake.calls)
		}
	})
}

func tagPolicyCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "policy" {
			t.Fatalf("resource = %s/%s, want tag/policy", resource.Product, resource.Resource)
		}
		if region != "cn-shanghai" {
			t.Fatalf("region = %q, want cn-shanghai", region)
		}
		return fake, nil
	})
}

func callNames(calls []fakeSpecCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.operation)
	}
	return out
}
