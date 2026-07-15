package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestTagResourceApplyCallsTagResources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-tag"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "apply",
		"--region", "cn-hangzhou",
		"--arn", "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
		"--arn", "arn:acs:ecs:cn-hangzhou:1234567890123456:instance/i-1",
		"--tag", "env=prod",
		"--tag", "team=platform",
	)
	if code != 0 {
		t.Fatalf("tag resource apply exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "TagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceARN.1"] != "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1" ||
		request["ResourceARN.2"] != "arn:acs:ecs:cn-hangzhou:1234567890123456:instance/i-1" {
		t.Fatalf("resource ARN request = %#v", request)
	}
	if request["Tags"] != `{"env":"prod","team":"platform"}` {
		t.Fatalf("Tags = %#v, request=%#v", request["Tags"], request)
	}
	out := decodeObject(t, stdout)
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions missing from output: %s", stdout)
	}
}

func TestTagResourceApplyRejectsDuplicateTagKeys(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-tag"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, _, code := runCLI(
		"tag", "resource", "apply",
		"--region", "cn-hangzhou",
		"--arn", "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
		"--tag", "env=prod",
		"--tag", "env=stage",
	)
	if code != 1 {
		t.Fatalf("duplicate tag key exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidParameter" {
		t.Fatalf("error.code = %q, want InvalidParameter; stdout=%s", got, stdout)
	}
	if !strings.Contains(errorMessage(t, stdout), "duplicate tag key") {
		t.Fatalf("error must mention duplicate tag key: %s", stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("duplicate tag key must fail before API call, calls=%#v", fake.calls)
	}
}

func TestTagResourceRemoveCallsUntagResources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-untag"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "remove",
		"--region", "cn-hangzhou",
		"--arn", "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
		"--tag-key", "env",
		"--tag-key", "team",
	)
	if code != 0 {
		t.Fatalf("tag resource remove exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "UntagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceARN.1"] != "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1" ||
		request["TagKey.1"] != "env" || request["TagKey.2"] != "team" {
		t.Fatalf("unexpected request = %#v", request)
	}
	out := decodeObject(t, stdout)
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions missing from output: %s", stdout)
	}
}

func TestTagResourceListDefaultsToListTagResourcesForARNs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-list",
		"NextToken": "token-2",
		"TagResources": []any{
			map[string]any{
				"ResourceARN": "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
				"Tags": []any{
					map[string]any{"Key": "env", "Value": "prod", "Category": "Custom"},
				},
			},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "arn=arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
		"--filter", "category=Custom",
		"--limit", "10",
		"--next-token", "token-1",
	)
	if code != 0 {
		t.Fatalf("tag resource list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListTagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceARN.1"] != "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1" ||
		request["Category"] != "Custom" || request["PageSize"] != 10 || request["NextToken"] != "token-1" {
		t.Fatalf("unexpected request = %#v", request)
	}
	out := decodeObject(t, stdout)
	resources, _ := out["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources len = %d, want 1; output=%s", len(resources), stdout)
	}
	resource, _ := resources[0].(map[string]any)
	if resource["arn"] != "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1" {
		t.Fatalf("unexpected resource output: %s", stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil || pagination["next_token"] != "token-2" || pagination["has_more"] != true {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
	if _, ok := pagination["page"]; ok {
		t.Fatalf("token pagination must not include page: %#v", pagination)
	}
	if _, ok := pagination["next_page"]; ok {
		t.Fatalf("token pagination must not include next_page: %#v", pagination)
	}
}

func TestTagResourceListAllowsCategoryOnlyFilter(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":    "req-category",
		"TagResources": []any{},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "category=Custom",
	)
	if code != 0 {
		t.Fatalf("tag resource list by category exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListTagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if request := fake.calls[0].request; request["Category"] != "Custom" {
		t.Fatalf("unexpected request = %#v", request)
	}
}

func TestTagResourceListAllowsNextTokenOnly(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":    "req-next",
		"TagResources": []any{},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--next-token", "token-2",
	)
	if code != 0 {
		t.Fatalf("tag resource list by next token exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListTagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if request := fake.calls[0].request; request["NextToken"] != "token-2" {
		t.Fatalf("unexpected request = %#v", request)
	}
}

func TestTagResourceListAllowsNoFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":    "req-list",
		"TagResources": []any{},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
	)
	if code != 0 {
		t.Fatalf("tag resource list without filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListTagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if request := fake.calls[0].request; request["ResourceARN.1"] != nil || request["Tags"] != nil || request["Category"] != nil || request["NextToken"] != nil {
		t.Fatalf("unexpected request = %#v", request)
	}
}

func TestTagResourceListUsesListTagResourcesForTagFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-list-tags",
		"TagResources": []any{
			map[string]any{
				"ResourceARN": "arn:acs:vpc:cn-hangzhou:1234567890123456:vpc/vpc-1",
				"Tags": []any{
					map[string]any{"Key": "env", "Value": "prod", "Category": "Custom"},
					map[string]any{"Key": "team", "Value": "platform", "Category": "Custom"},
				},
			},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "tag.env=prod",
		"--filter", "tag.team=platform",
		"--filter", "category=Custom",
	)
	if code != 0 {
		t.Fatalf("tag resource list by tags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListTagResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Tags"] != `{"env":"prod","team":"platform"}` || request["Category"] != "Custom" {
		t.Fatalf("unexpected request = %#v", request)
	}
	resources, _ := decodeObject(t, stdout)["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources len = %d, want 1; output=%s", len(resources), stdout)
	}
}

func TestTagResourceListUsesListResourcesByTagForTagFilter(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-by-tag",
		"Resources": []any{
			map[string]any{
				"ResourceId": "vpc-1",
				"Tags": []any{
					map[string]any{"Key": "env", "Value": "prod", "Category": "custom"},
				},
			},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "tag.env=prod",
		"--resource-type", "ALIYUN::VPC::VPC",
		"--include-all-tags",
	)
	if code != 0 {
		t.Fatalf("tag resource list by tag exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListResourcesByTag" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ResourceType"] != "ALIYUN::VPC::VPC" || request["TagFilter.Key"] != "env" ||
		request["TagFilter.Value"] != "prod" || request["IncludeAllTags"] != true || request["MaxResult"] != 100 {
		t.Fatalf("unexpected request = %#v", request)
	}
	resources, _ := decodeObject(t, stdout)["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources len = %d, want 1; output=%s", len(resources), stdout)
	}
	resource, _ := resources[0].(map[string]any)
	if resource["id"] != "vpc-1" {
		t.Fatalf("unexpected resource output: %s", stdout)
	}
}

func TestTagResourceListRejectsMultipleTagsForReverseLookup(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-by-tag",
		"Resources": []any{
			map[string]any{"ResourceId": "vpc-1"},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "tag" || resource.Resource != "resource" {
			t.Fatalf("resource = %s/%s, want tag/resource", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, _, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "tag.env=prod",
		"--filter", "tag.team=platform",
		"--resource-type", "ALIYUN::VPC::VPC",
	)
	if code != 1 {
		t.Fatalf("multiple reverse tags exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidParameter" {
		t.Fatalf("error.code = %q, want InvalidParameter; stdout=%s", got, stdout)
	}
	if !strings.Contains(errorMessage(t, stdout), "one tag") {
		t.Fatalf("error must mention one tag: %s", stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("multiple reverse tags must fail before API call, calls=%#v", fake.calls)
	}
}

func TestTagResourceListRejectsCategoryForReverseLookup(t *testing.T) {
	t.Parallel()
	stdout, _, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--filter", "tag.env=prod",
		"--resource-type", "ALIYUN::VPC::VPC",
		"--filter", "category=Custom",
	)
	if code != 1 {
		t.Fatalf("reverse lookup with category exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
}

func TestTagResourceListRequiresTagForReverseLookup(t *testing.T) {
	t.Parallel()
	stdout, _, code := runCLI(
		"tag", "resource", "list",
		"--region", "cn-hangzhou",
		"--resource-type", "ALIYUN::VPC::VPC",
	)
	if code != 1 {
		t.Fatalf("tag resource reverse lookup without tag exit %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
}

func TestTagResourceListRequiresResourceTypeForReverseLookupOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "fuzzy type",
			args: []string{
				"tag", "resource", "list",
				"--region", "cn-hangzhou",
				"--filter", "tag.env=prod",
				"--fuzzy-type", "NOT",
			},
		},
		{
			name: "include all tags",
			args: []string{
				"tag", "resource", "list",
				"--region", "cn-hangzhou",
				"--filter", "tag.env=prod",
				"--include-all-tags",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, code := runCLI(tt.args...)
			if code != 1 {
				t.Fatalf("tag resource reverse lookup option exit %d, want 1; stdout=%s", code, stdout)
			}
			if got := errorCode(t, stdout); got != "MissingParameter" {
				t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
			}
		})
	}
}

func TestTagResourceListHelpDoesNotExposeAssignmentTagFlag(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "tag", "resource", "list", "--help")
	if code != 0 {
		t.Fatalf("tag resource list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--filter") {
		t.Fatalf("list help missing --filter:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--next-token") {
		t.Fatalf("list help missing --next-token:\n%s", stdout)
	}
	if strings.Contains(stdout, "--page") {
		t.Fatalf("token-paginated list help must not expose --page:\n%s", stdout)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "--tag ") || strings.Contains(line, "--tag\t") {
			t.Fatalf("list help exposes assignment --tag flag:\n%s", stdout)
		}
	}
}
