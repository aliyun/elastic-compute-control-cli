package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func ecsKeypairCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "keypair" {
			t.Fatalf("resource = %s/%s, want ecs/keypair", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func TestECSKeypairListDefaultLimitFitsAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 0,
		"KeyPairs":   map[string]any{"KeyPair": []any{}},
	}}}
	runCLI := ecsKeypairCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "keypair", "list", "web-key", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs keypair list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeKeyPairs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PageSize"] != 50 {
		t.Fatalf("DescribeKeyPairs PageSize = %#v, want 50; request=%#v", fake.calls[0].request["PageSize"], fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	keypairs, ok := out["keypairs"].([]any)
	if !ok || len(keypairs) != 0 {
		t.Fatalf("keypairs = %#v, want empty array; stdout=%s", out["keypairs"], stdout)
	}
}

func TestECSKeypairDeleteSendsJSONKeyPairNames(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := ecsKeypairCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "keypair", "delete", "web-key", "ops-key", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs keypair delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteKeyPairs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["KeyPairNames"] != `["web-key","ops-key"]` {
		t.Fatalf("DeleteKeyPairs request = %#v", fake.calls[0].request)
	}
}
