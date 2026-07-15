package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestVSwitchCreateAliasUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "VSwitchId": "vsw-123"},
			{
				"RequestId":               "req-attr",
				"VSwitchId":               "vsw-123",
				"VSwitchName":             "prod-a",
				"VpcId":                   "vpc-123",
				"ZoneId":                  "cn-beijing-a",
				"CidrBlock":               "10.0.1.0/24",
				"Status":                  "Available",
				"AvailableIpAddressCount": 247,
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vswitch" {
			t.Fatalf("resource = %s/%s, want vpc/vswitch", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "vsw", "create", "--region", "cn-beijing", "--vpc", "vpc-123", "--zone", "cn-beijing-a", "--cidr", "10.0.1.0/24", "--name", "prod-a")
	if code != 0 {
		t.Fatalf("vpc vsw create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateVSwitch" || fake.calls[1].operation != "DescribeVSwitchAttributes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["VpcId"] != "vpc-123" || createReq["ZoneId"] != "cn-beijing-a" || createReq["CidrBlock"] != "10.0.1.0/24" || createReq["VSwitchName"] != "prod-a" {
		t.Fatalf("CreateVSwitch request = %#v", createReq)
	}
	vswitch, _ := decodeObject(t, stdout)["vswitch"].(map[string]any)
	if vswitch == nil || vswitch["id"] != "vsw-123" || vswitch["vpc"] != "vpc-123" || vswitch["available_ip_count"] != float64(247) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestVSwitchListDefaultsLimitTo50(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":  "req-list",
				"TotalCount": 51,
				"VSwitches": map[string]any{"VSwitch": []any{
					map[string]any{
						"VSwitchId": "vsw-123",
						"VpcId":     "vpc-123",
						"ZoneId":    "cn-beijing-a",
						"CidrBlock": "10.0.1.0/24",
						"Status":    "Available",
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vswitch" {
			t.Fatalf("resource = %s/%s, want vpc/vswitch", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "vswitch", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("vpc vswitch list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeVSwitches" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["PageSize"] != 50 || request["PageNumber"] != 1 {
		t.Fatalf("DescribeVSwitches request = %#v", request)
	}
	pagination, _ := decodeObject(t, stdout)["pagination"].(map[string]any)
	if pagination["limit"] != float64(50) || pagination["has_more"] != true {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
}

func TestVSwitchCreateDoesNotSupportDryRun(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("vpc", "vsw", "create", "--region", "cn-beijing", "--vpc", "vpc-123", "--zone", "cn-beijing-a", "--cidr", "10.0.1.0/24", "--dry-run")
	if code != 1 {
		t.Fatalf("vpc vsw create --dry-run exit %d, want 1 stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownCommand" {
		t.Fatalf("error.code = %q, want UnknownCommand; stdout=%s", got, stdout)
	}
}

func TestVSwitchDeleteSupportsDryRun(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"DryRun": true}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vswitch" {
			t.Fatalf("resource = %s/%s, want vpc/vswitch", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("vpc", "vsw", "delete", "vsw-123", "--region", "cn-beijing", "--dry-run")
	if code != 0 {
		t.Fatalf("vpc vsw delete --dry-run exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteVSwitch" || fake.calls[0].request["DryRun"] != true {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	if out["dry_run"] != "passed" || out["deleted"] == true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
