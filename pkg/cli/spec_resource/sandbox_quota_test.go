package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSandboxQuotaListAllIncludesOwnedQuotaOnLaterPage(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"code": "200", "requestId": "req-1", "nextToken": "opaque-next", "quotas": []any{map[string]any{"tagValue": "other-team", "cpuCores": 1, "memoryGB": 2}}},
		{"code": "200", "requestId": "req-2", "quotas": []any{map[string]any{"tagValue": "owned-team", "cpuCores": 32, "memoryGB": 64}}},
	}}
	run := withCaller(func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "quota" || region != "cn-hangzhou" {
			t.Fatalf("wrong quota target: %s/%s %s", resource.Product, resource.Resource, region)
		}
		return fake, nil
	})
	stdout, stderr, code := run("--lang", "en", "sandbox", "quota", "list", "--limit", "1", "--all", "--region", "cn-hangzhou", "--output=json")
	if code != 0 {
		t.Fatalf("quota list --all exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ListQuota" || fake.calls[1].operation != "ListQuota" || fake.calls[1].request["nextToken"] != "opaque-next" {
		t.Fatalf("quota token requests = %+v", fake.calls)
	}
	out := decodeObject(t, stdout)
	quotas, ok := out["quotas"].([]any)
	if !ok || len(quotas) != 2 || quotas[1].(map[string]any)["id"] != "owned-team" {
		t.Fatalf("owned quota absent from merged output: %s", stdout)
	}
	pagination := out["pagination"].(map[string]any)
	if pagination["has_more"] != false || pagination["returned"] != float64(2) || pagination["limit"] != float64(1) || pagination["page"] != nil || pagination["next_token"] != nil {
		t.Fatalf("token pagination output = %+v", pagination)
	}
}

func TestSandboxQuotaListExplicitTokenAndLimitReachAPI(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"code": "200", "requestId": "req-page-2", "quotas": []any{map[string]any{"tagValue": "owned-second", "cpuCores": 32, "memoryGB": 64}},
	}}}
	run := withCaller(func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "quota" || region != "cn-hangzhou" {
			t.Fatalf("wrong quota target: %s/%s %s", resource.Product, resource.Resource, region)
		}
		return fake, nil
	})
	stdout, stderr, code := run("--lang", "en", "sandbox", "quota", "list", "--limit", "1", "--next-token", "opaque-first-page", "--region", "cn-hangzhou", "--output=json")
	if code != 0 {
		t.Fatalf("quota next page exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListQuota" || fake.calls[0].request["nextToken"] != "opaque-first-page" || fake.calls[0].request["maxResults"] != 1 {
		t.Fatalf("explicit page request lost token or limit: %+v", fake.calls)
	}
}
