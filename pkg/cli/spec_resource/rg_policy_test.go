package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

// ---------- helpers ----------

func rgPolicyCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "rg" || resource.APIProduct != "ResourceManager" {
			t.Fatalf("resource = %s/%s api=%s, want rg/* with ResourceManager", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

// ---------- policy create ----------

func TestRGPolicyCreateCallsCreatePolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "Policy": map[string]any{
			"PolicyName":  "my-policy",
			"PolicyType":  "Custom",
			"Description": "test policy",
		}},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "create",
		"--region", "cn-beijing",
		"--name", "my-policy",
		"--policy-document", `{"Statement":[]}`,
		"--description", "test policy",
	)
	if code != 0 {
		t.Fatalf("rg policy create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreatePolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyDocument"] != `{"Statement":[]}` || req["Description"] != "test policy" {
		t.Fatalf("CreatePolicy request = %#v", req)
	}
	policy, _ := decodeObject(t, stdout)["policy"].(map[string]any)
	if policy == nil || policy["name"] != "my-policy" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

// ---------- policy delete ----------

func TestRGPolicyDeleteCallsDeletePolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-delete"},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "delete", "my-policy", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("rg policy delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeletePolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PolicyName"] != "my-policy" {
		t.Fatalf("DeletePolicy request = %#v", fake.calls[0].request)
	}
	policy, _ := decodeObject(t, stdout)["policy"].(map[string]any)
	if policy == nil || policy["name"] != "my-policy" {
		t.Fatalf("delete output missing policy name: %s", stdout)
	}
}

// ---------- policy get ----------

func TestRGPolicyGetCallsGetPolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-get",
			"Policy": map[string]any{
				"PolicyName":      "my-policy",
				"PolicyType":      "Custom",
				"Description":     "test policy",
				"DefaultVersion":  "v1",
				"AttachmentCount": float64(2),
				"CreateDate":      "2025-01-02T03:04:05Z",
				"UpdateDate":      "2025-06-01T00:00:00Z",
				"PolicyDocument":  `{"Statement":[]}`,
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "get", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
	)
	if code != 0 {
		t.Fatalf("rg policy get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyType"] != "Custom" {
		t.Fatalf("GetPolicy request = %#v", req)
	}
	policy, _ := decodeObject(t, stdout)["policy"].(map[string]any)
	if policy == nil || policy["name"] != "my-policy" || policy["policy_type"] != "Custom" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

// ---------- policy list ----------

func TestRGPolicyListCallsListPolicies(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId":  "req-list",
			"TotalCount": 1,
			"Policies": map[string]any{
				"Policy": []any{
					map[string]any{
						"PolicyName":      "my-policy",
						"PolicyType":      "Custom",
						"Description":     "test policy",
						"DefaultVersion":  "v1",
						"AttachmentCount": float64(2),
						"CreateDate":      "2025-01-02T03:04:05Z",
					},
				},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "list",
		"--region", "cn-beijing",
		"--filter", "policy-type=Custom",
	)
	if code != 0 {
		t.Fatalf("rg policy list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListPolicies" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyType"] != "Custom" || req["PageSize"] != 100 || req["PageNumber"] != 1 {
		t.Fatalf("ListPolicies request = %#v", req)
	}
	out := decodeObject(t, stdout)
	policies, _ := out["policies"].([]any)
	if out["total"] != float64(1) || len(policies) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	first, _ := policies[0].(map[string]any)
	if first["name"] != "my-policy" {
		t.Fatalf("policy = %#v; stdout=%s", first, stdout)
	}
}

// ---------- policy list attachments ----------

func TestRGPolicyListAttachmentsCallsListPolicyAttachments(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId":  "req-list-attachments",
			"TotalCount": 1,
			"PolicyAttachments": map[string]any{
				"PolicyAttachment": []any{
					map[string]any{
						"PolicyName":      "my-policy",
						"PolicyType":      "Custom",
						"PrincipalName":   "user1",
						"PrincipalType":   "IMSUser",
						"ResourceGroupId": "rg-123",
						"AttachDate":      "2025-01-02T03:04:05Z",
					},
				},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "list",
		"--region", "cn-beijing",
		"--resource-group", "rg-123",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
	)
	if code != 0 {
		t.Fatalf("rg policy list attachments exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListPolicyAttachments" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["ResourceGroupId"] != "rg-123" || req["PrincipalType"] != "IMSUser" || req["PrincipalName"] != "user1" {
		t.Fatalf("ListPolicyAttachments request = %#v", req)
	}
}

// ---------- policy attach ----------

func TestRGPolicyAttachCallsAttachPolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
		{
			"RequestId":  "req-list-after-attach",
			"TotalCount": 1,
			"PolicyAttachments": map[string]any{
				"PolicyAttachment": []any{
					map[string]any{
						"PolicyName":      "my-policy",
						"PolicyType":      "Custom",
						"PrincipalName":   "user1",
						"PrincipalType":   "IMSUser",
						"ResourceGroupId": "rg-123",
						"AttachDate":      "2025-01-02T03:04:05Z",
					},
				},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "attach", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
		"--resource-group", "rg-123",
	)
	if code != 0 {
		t.Fatalf("rg policy attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy,ListPolicyAttachments" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyType"] != "Custom" ||
		req["PrincipalType"] != "IMSUser" || req["PrincipalName"] != "user1" ||
		req["ResourceGroupId"] != "rg-123" {
		t.Fatalf("AttachPolicy request = %#v", req)
	}
}

func TestRGPolicyAttachNoWaitSkipsAttachmentProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "attach", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
		"--resource-group", "rg-123",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("rg policy attach --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "AttachPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestRGPolicyAttachTimesOutWhenRelationshipIsNotVisible(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-attach"},
		{
			"RequestId":  "req-list-after-attach",
			"TotalCount": 0,
			"PolicyAttachments": map[string]any{
				"PolicyAttachment": []any{},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, _, code := runCLI("rg", "policy", "attach", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
		"--resource-group", "rg-123",
		"--timeout", "1ms",
	)
	if code != 3 {
		t.Fatalf("rg policy attach invisible relationship exit %d, want 3; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error.code = %q, want WaitTimeout; stdout=%s", got, stdout)
	}
}

// ---------- policy detach ----------

func TestRGPolicyDetachCallsDetachPolicy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-detach"},
		{
			"RequestId":  "req-list-after-detach",
			"TotalCount": 0,
			"PolicyAttachments": map[string]any{
				"PolicyAttachment": []any{},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "detach", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
		"--resource-group", "rg-123",
	)
	if code != 0 {
		t.Fatalf("rg policy detach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "DetachPolicy,ListPolicyAttachments" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyType"] != "Custom" ||
		req["PrincipalType"] != "IMSUser" || req["PrincipalName"] != "user1" ||
		req["ResourceGroupId"] != "rg-123" {
		t.Fatalf("DetachPolicy request = %#v", req)
	}
}

func TestRGPolicyDetachTimesOutWhenRelationshipIsStillVisible(t *testing.T) {
	t.Parallel()
	relationshipStillVisible := map[string]any{
		"RequestId":  "req-list-after-detach",
		"TotalCount": 1,
		"PolicyAttachments": map[string]any{
			"PolicyAttachment": []any{
				map[string]any{
					"PolicyName":      "my-policy",
					"PolicyType":      "Custom",
					"PrincipalName":   "user1",
					"PrincipalType":   "IMSUser",
					"ResourceGroupId": "rg-123",
				},
			},
		},
	}
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-detach"},
			relationshipStillVisible,
		},
		responseWhenExhausted: relationshipStillVisible,
	}
	runCLI := rgPolicyCaller(t, fake)

	stdout, _, code := runCLI("rg", "policy", "detach", "my-policy",
		"--region", "cn-beijing",
		"--policy-type", "Custom",
		"--principal-type", "IMSUser",
		"--principal-name", "user1",
		"--resource-group", "rg-123",
		"--timeout", "1ms",
	)
	if code != 3 {
		t.Fatalf("rg policy detach visible relationship exit %d, want 3; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error.code = %q, want WaitTimeout; stdout=%s", got, stdout)
	}
}

// ---------- policy-version create ----------

func TestRGPolicyVersionCreateCallsCreatePolicyVersion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-create",
			"PolicyVersion": map[string]any{
				"VersionId":        "v2",
				"IsDefaultVersion": false,
				"CreateDate":       "2025-06-01T00:00:00Z",
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "version", "create",
		"--region", "cn-beijing",
		"--policy-name", "my-policy",
		"--policy-document", `{"Statement":[]}`,
		"--set-as-default",
	)
	if code != 0 {
		t.Fatalf("rg policy version create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreatePolicyVersion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyDocument"] != `{"Statement":[]}` || req["SetAsDefault"] != true {
		t.Fatalf("CreatePolicyVersion request = %#v", req)
	}
	version, _ := decodeObject(t, stdout)["version"].(map[string]any)
	if version == nil || version["version_id"] != "v2" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

// ---------- policy-version delete ----------

func TestRGPolicyVersionDeleteCallsDeletePolicyVersion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-delete"},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "version", "delete", "v2",
		"--region", "cn-beijing",
		"--policy-name", "my-policy",
	)
	if code != 0 {
		t.Fatalf("rg policy version delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeletePolicyVersion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["VersionId"] != "v2" {
		t.Fatalf("DeletePolicyVersion request = %#v", req)
	}
	version, _ := decodeObject(t, stdout)["version"].(map[string]any)
	if version == nil || version["version_id"] != "v2" {
		t.Fatalf("delete output missing version_id: %s", stdout)
	}
}

// ---------- policy-version get ----------

func TestRGPolicyVersionGetCallsGetPolicyVersion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-get",
			"PolicyVersion": map[string]any{
				"VersionId":        "v1",
				"IsDefaultVersion": true,
				"PolicyDocument":   `{"Statement":[]}`,
				"CreateDate":       "2025-01-02T03:04:05Z",
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "version", "get", "v1",
		"--region", "cn-beijing",
		"--policy-name", "my-policy",
		"--policy-type", "Custom",
	)
	if code != 0 {
		t.Fatalf("rg policy version get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetPolicyVersion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyType"] != "Custom" || req["VersionId"] != "v1" {
		t.Fatalf("GetPolicyVersion request = %#v", req)
	}
	version, _ := decodeObject(t, stdout)["version"].(map[string]any)
	if version == nil || version["version_id"] != "v1" || version["is_default_version"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

// ---------- policy-version list ----------

func TestRGPolicyVersionListCallsListPolicyVersions(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId": "req-list",
			"PolicyVersions": map[string]any{
				"PolicyVersion": []any{
					map[string]any{
						"VersionId":        "v1",
						"IsDefaultVersion": true,
						"CreateDate":       "2025-01-02T03:04:05Z",
					},
					map[string]any{
						"VersionId":        "v2",
						"IsDefaultVersion": false,
						"CreateDate":       "2025-06-01T00:00:00Z",
					},
				},
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "version", "list",
		"--region", "cn-beijing",
		"--policy-name", "my-policy",
		"--policy-type", "Custom",
	)
	if code != 0 {
		t.Fatalf("rg policy version list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListPolicyVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PolicyName"] != "my-policy" || req["PolicyType"] != "Custom" {
		t.Fatalf("ListPolicyVersions request = %#v", req)
	}
	out := decodeObject(t, stdout)
	versions, _ := out["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("unexpected version count: %s", stdout)
	}
}

// ---------- policy-version update ----------

func TestRGPolicyVersionUpdateCallsSetDefaultPolicyVersion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-set-default"},
		{
			"RequestId": "req-get-policy",
			"Policy": map[string]any{
				"PolicyName":     "my-policy",
				"PolicyType":     "Custom",
				"Description":    "test policy",
				"DefaultVersion": "v2",
				"CreateDate":     "2025-01-02T03:04:05Z",
				"UpdateDate":     "2025-06-01T00:00:00Z",
			},
		},
	}}
	runCLI := rgPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("rg", "policy", "version", "update", "v2",
		"--region", "cn-beijing",
		"--policy-name", "my-policy",
		"--set-as-default",
	)
	if code != 0 {
		t.Fatalf("rg policy version update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "SetDefaultPolicyVersion,GetPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PolicyName"] != "my-policy" || fake.calls[0].request["VersionId"] != "v2" {
		t.Fatalf("SetDefaultPolicyVersion request = %#v", fake.calls[0].request)
	}
	version, _ := decodeObject(t, stdout)["version"].(map[string]any)
	if version == nil || version["default_version"] != "v2" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

// ---------- help tests ----------

func TestRGPolicyHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "rg", "policy", "--help")
	if code != 0 {
		t.Fatalf("rg policy --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"list", "get", "create", "delete", "attach", "detach"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "get", "--help")
	if code != 0 {
		t.Fatalf("rg policy get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--policy-type"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy get help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "list", "--help")
	if code != 0 {
		t.Fatalf("rg policy list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--filter", "--limit", "--page"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy list help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "attach", "--help")
	if code != 0 {
		t.Fatalf("rg policy attach --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--policy-type", "--principal-type", "--principal-name", "--resource-group"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy attach help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "version", "--help")
	if code != 0 {
		t.Fatalf("rg policy version --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"list", "get", "create", "delete", "update"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy version help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "version", "create", "--help")
	if code != 0 {
		t.Fatalf("rg policy version create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--policy-name", "--policy-document", "--set-as-default"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy version create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "rg", "policy", "version", "list", "--help")
	if code != 0 {
		t.Fatalf("rg policy version list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--policy-name", "--policy-type"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("rg policy version list help missing %q:\n%s", want, stdout)
		}
	}
	// ListPolicyVersions has no pagination, so --limit and --page should not appear
	for _, notWant := range []string{"--limit", "--page"} {
		if strings.Contains(stdout, notWant) {
			t.Fatalf("rg policy version list help should not have %q:\n%s", notWant, stdout)
		}
	}
}
