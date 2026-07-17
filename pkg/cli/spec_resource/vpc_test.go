package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestVPCUpdateUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			{
				"RequestId": "req-attr",
				"VpcId":     "vpc-123",
				"VpcName":   "prod-network",
				"CidrBlock": "10.0.0.0/16",
				"Status":    "Available",
				"RegionId":  "cn-beijing",
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "update", "vpc-123", "--region", "cn-beijing", "--name", "prod-network")
	if code != 0 {
		t.Fatalf("vpc update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifyVpcAttribute" || fake.calls[1].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if _, ok := fake.calls[0].request["ClientToken"]; ok {
		t.Fatalf("ModifyVpcAttribute must not receive ClientToken: %#v", fake.calls[0].request)
	}
	vpc, _ := decodeObject(t, stdout)["vpc"].(map[string]any)
	if vpc == nil || vpc["id"] != "vpc-123" || vpc["name"] != "prod-network" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestVPCCreateAcceptsIdempotencyKey(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "VpcId": "vpc-123"},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "create", "--region", "cn-beijing", "--name", "prod-network", "--cidr", "10.0.0.0/16", "--no-wait", "--idempotency-key", "agent-retry-1")
	if code != 0 {
		t.Fatalf("vpc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateVpc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got := fake.calls[0].request["ClientToken"]; got != "agent-retry-1" {
		t.Fatalf("CreateVpc ClientToken = %#v, want explicit idempotency key; request=%#v", got, fake.calls[0].request)
	}
}

func TestVPCSelfResourceCreateAliasUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "VpcId": "vpc-123"},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "vpc", "create", "--region", "cn-beijing", "--name", "prod-network", "--cidr", "10.0.0.0/16", "--no-wait")
	if code != 0 {
		t.Fatalf("vpc vpc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateVpc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got := fake.calls[0].request["VpcName"]; got != "prod-network" {
		t.Fatalf("CreateVpc VpcName = %#v, request=%#v", got, fake.calls[0].request)
	}
}

func TestVPCListAcceptsPositionalIDs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 1,
				"Vpcs": map[string]any{"Vpc": []any{
					map[string]any{
						"VpcId":     "vpc-123",
						"VpcName":   "prod-network",
						"CidrBlock": "10.0.0.0/16",
						"Status":    "Available",
						"RegionId":  "cn-beijing",
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "list", "vpc-123", "--region", "cn-beijing", "--limit", "5")
	if code != 0 {
		t.Fatalf("vpc list <id> exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeVpcs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got, ok := fake.calls[0].request["VpcId"].([]string); !ok || len(got) != 1 || got[0] != "vpc-123" {
		t.Fatalf("DescribeVpcs request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(1) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestVPCUpdateRequiresAtLeastOneAttribute(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("update validation should fail before creating a caller")
		return nil, nil
	})

	stdout, stderr, code := runCLI("vpc", "update", "vpc-123", "--region", "cn-beijing")
	if code != 1 {
		t.Fatalf("vpc update exit %d, want 1 stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	message := errorMessage(t, stdout)
	if strings.Contains(message, "--api-param") || strings.Contains(message, "--timeout") || strings.Contains(message, "--no-wait") {
		t.Fatalf("message should only mention mutable fields, got %q", message)
	}
	if !strings.Contains(message, "--name") || !strings.Contains(message, "--description") {
		t.Fatalf("message should mention mutable fields, got %q", message)
	}
}

func TestVPCGetEmptyAttributeResponseReturnsNotFound(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"RequestId":                    "req-empty",
			"VpcId":                        "",
			"VpcName":                      "",
			"CidrBlock":                    "",
			"Status":                       "",
			"RegionId":                     "",
			"CloudResources":               map[string]any{"CloudResourceSetType": []any{}},
			"SecondaryCidrBlocks":          map[string]any{"SecondaryCidrBlock": []any{}},
			"UserCidrs":                    map[string]any{"UserCidr": []any{}},
			"VSwitchIds":                   map[string]any{"VSwitchId": []any{}},
			"AssociatedCens":               map[string]any{"AssociatedCen": []any{}},
			"AssociatedPropagationSources": map[string]any{"AssociatedPropagationSources": []any{}},
		}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "get", "vpc-missing", "--region", "cn-beijing")
	if code != 4 {
		t.Fatalf("vpc get missing exit %d, want 4 stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "NotFound" {
		t.Fatalf("error.code = %q, want NotFound; stdout=%s", got, stdout)
	}
}

func TestVPCDeleteSupportsDryRun(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"DryRun": true}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "delete", "vpc-123", "--region", "cn-beijing", "--dry-run")
	if code != 0 {
		t.Fatalf("vpc delete --dry-run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteVpc" || fake.calls[0].request["DryRun"] != true {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	if out["dry_run"] != "passed" || out["deleted"] == true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
