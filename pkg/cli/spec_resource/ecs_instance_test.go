package spec_resource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestECSInstanceCreatePassesAPIParamsToRunInstances(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-run", "InstanceIdSets": map[string]any{"InstanceIdSet": []any{"i-123"}}},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
		"--no-wait",
		"--api-param", "DataDisk.1.Category=cloud_essd",
		"--api-param", "DataDisk.1.Size=40",
	)
	if code != 0 {
		t.Fatalf("ecs instance create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunInstances" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["DataDisk.1.Category"] != "cloud_essd" || request["DataDisk.1.Size"] != "40" {
		t.Fatalf("RunInstances request = %#v", request)
	}
}

func TestECSInstanceCreatePassesUserDataThrough(t *testing.T) {
	t.Parallel()
	const encodedUserData = "IyEvYmluL3NoCmVjaG8gaGVsbG8K"
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-run", "InstanceIdSets": map[string]any{"InstanceIdSet": []any{"i-123"}}},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
		"--no-wait",
		"--user-data", encodedUserData,
	)
	if code != 0 {
		t.Fatalf("ecs instance create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunInstances" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got, want := fake.calls[0].request["UserData"], encodedUserData; got != want {
		t.Fatalf("UserData = %#v, want %#v; request=%#v", got, want, fake.calls[0].request)
	}
}

func TestECSInstanceCreateExpandsJSONListFlags(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-run", "InstanceIdSets": map[string]any{"InstanceIdSet": []any{"i-123"}}},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
		"--no-wait",
		"--data-disk", `{"category":"cloud_essd","size":40}`,
		"--host-names", `["web-a","web-b"]`,
	)
	if code != 0 {
		t.Fatalf("ecs instance create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunInstances" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"DataDisk.1.Category": "cloud_essd",
		"DataDisk.1.Size":     40,
		"HostNames.1":         "web-a",
		"HostNames.2":         "web-b",
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
}

func TestECSInstanceCreateSuggestsAvailabilityQueryForStockErrors(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		errors: []error{
			ecerrors.Service("CloudAPIError", "resource type not supported", false,
				ecerrors.WithRequestID("req-run"),
				ecerrors.WithRawCause("InvalidResourceType.NotSupported", "instance type ecs.g6.large not exists in [cn-shanghai-g]")),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "create",
		"--region", "cn-shanghai",
		"--zone", "cn-shanghai-g",
		"--type", "ecs.g6.large",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
	)
	if code == 0 {
		t.Fatalf("ecs instance create unexpectedly succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	decoded := decodeObject(t, stdout)
	errObj, _ := decoded["error"].(map[string]any)
	if errObj["field"] != "type" {
		t.Fatalf("error.field = %#v, want type; stdout=%s", errObj["field"], stdout)
	}
	suggested, _ := errObj["suggested_action"].(string)
	for _, want := range []string{"ecctl call ecs DescribeAvailableResource", "--DestinationResource InstanceType", "--ZoneId cn-shanghai-g", "--InstanceType ecs.g6.large"} {
		if !strings.Contains(suggested, want) {
			t.Fatalf("suggested_action missing %q: %q", want, suggested)
		}
	}
	actions, _ := decoded["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %#v; stdout=%s", actions, stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["code"] != "InvalidResourceType.NotSupported" {
		t.Fatalf("action.code = %#v, want raw provider code; stdout=%s", action["code"], stdout)
	}
}

func TestECSInstanceLifecycleActionsUseDocumentedAPIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		action    string
		id        string
		operation string
	}{
		{name: "start", action: "start", id: "i-start", operation: "StartInstance"},
		{name: "stop", action: "stop", id: "i-stop", operation: "StopInstance"},
		{name: "reboot", action: "reboot", id: "i-reboot", operation: "RebootInstance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-" + tt.action}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ecs" || resource.Resource != "instance" {
					t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
				}
				if region != "cn-beijing" {
					t.Fatalf("region = %q, want cn-beijing", region)
				}
				return fake, nil
			})

			stdout, stderr, code := runCLI("ecs", "instance", tt.action, tt.id, "--region", "cn-beijing", "--no-wait")
			if code != 0 {
				t.Fatalf("ecs instance %s exit %d stderr=%s stdout=%s", tt.action, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			if fake.calls[0].request["InstanceId"] != tt.id {
				t.Fatalf("%s request = %#v", tt.operation, fake.calls[0].request)
			}
			actions, _ := decodeObject(t, stdout)["actions"].([]any)
			if len(actions) != 1 {
				t.Fatalf("actions missing from output: %s", stdout)
			}
		})
	}
}

func TestECSInstanceStartBatchUsesBatchAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-start-batch"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "start", "i-1", "i-2", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance start batch exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "StartInstances" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InstanceId.1"] != "i-1" || fake.calls[0].request["InstanceId.2"] != "i-2" {
		t.Fatalf("StartInstances request = %#v", fake.calls[0].request)
	}
}

func TestECSInstanceDeleteBatchUsesDeleteInstances(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete-batch"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "delete", "i-1", "i-2", "--region", "cn-beijing", "--force", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance delete batch exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteInstances" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InstanceId.1"] != "i-1" || request["InstanceId.2"] != "i-2" || request["Force"] != true {
		t.Fatalf("DeleteInstances request = %#v", request)
	}
}

func TestECSInstanceGetWithAutoRenewCallsExtraAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":    "req-instance",
				"InstanceId":   "i-123",
				"InstanceName": "web",
				"Status":       "Running",
			},
			{
				"RequestId": "req-renew",
				"InstanceRenewAttributes": map[string]any{"InstanceRenewAttribute": []any{
					map[string]any{"InstanceId": "i-123", "AutoRenewEnabled": true, "Duration": float64(1), "PeriodUnit": "Month"},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "get", "i-123", "--region", "cn-beijing", "--with-auto-renew")
	if code != 0 {
		t.Fatalf("ecs instance get --with-auto-renew exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeInstanceAttribute" || fake.calls[1].operation != "DescribeInstanceAutoRenewAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	instance, _ := decodeObject(t, stdout)["instance"].(map[string]any)
	if instance["auto_renew"] != true || instance["auto_renew_period_unit"] != "Month" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSInstanceGetMaintenanceAndPluginUseInstanceIds(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId":    "req-instance",
				"InstanceId":   "i-123",
				"InstanceName": "web",
				"Status":       "Running",
			},
			{"RequestId": "req-maintenance"},
			{"RequestId": "req-plugin", "PluginStatusList": map[string]any{"PluginStatus": []any{}}},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "get", "i-123", "--region", "cn-beijing", "--with-maintenance", "--with-plugin-status")
	if code != 0 {
		t.Fatalf("ecs instance get --with-maintenance --with-plugin-status exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[1].operation != "DescribeInstanceMaintenanceAttributes" || fake.calls[2].operation != "ListPluginStatus" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	for _, call := range fake.calls[1:] {
		if call.request["InstanceId.1"] != "i-123" {
			t.Fatalf("%s request = %#v", call.operation, call.request)
		}
		if _, ok := call.request["InstanceId"]; ok {
			t.Fatalf("%s request should not use InstanceId: %#v", call.operation, call.request)
		}
	}
}

func TestECSInstanceGetHelpShowsDocumentedWithFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "instance", "get", "--help")
	if code != 0 {
		t.Fatalf("ecs instance get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, flag := range []string{
		"--with-auto-renew",
		"--with-maintenance",
		"--with-ram-role",
		"--with-user-data",
		"--with-vnc-url",
		"--with-assistant",
		"--with-plugin-status",
		"--with-tags",
	} {
		if !strings.Contains(stdout, flag) {
			t.Fatalf("get help missing %s: %s", flag, stdout)
		}
	}
}

func TestECSInstanceUpdateRoutesAutoReleaseAndAutoRenew(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-auto-release"},
		{"RequestId": "req-auto-renew"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--auto-release-time", "2026-05-28T00:00:00Z",
		"--auto-renew=false",
		"--auto-renew-period", "1",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update auto settings exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifyInstanceAutoReleaseTime" || fake.calls[1].operation != "ModifyInstanceAutoRenewAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	releaseReq := fake.calls[0].request
	if releaseReq["InstanceId"] != "i-123" || releaseReq["AutoReleaseTime"] != "2026-05-28T00:00:00Z" {
		t.Fatalf("ModifyInstanceAutoReleaseTime request = %#v", releaseReq)
	}
	renewReq := fake.calls[1].request
	if renewReq["InstanceId"] != "i-123" || renewReq["AutoRenew"] != false || renewReq["Duration"] != 1 {
		t.Fatalf("ModifyInstanceAutoRenewAttribute request = %#v", renewReq)
	}
}

func TestECSInstanceUpdateRoutesHostNameAttribute(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-attribute"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--host-name", "web-01",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update host name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyInstanceAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["HostName"] != "web-01" {
		t.Fatalf("ModifyInstanceAttribute request = %#v", fake.calls[0].request)
	}
}

func TestECSInstanceUpdateRoutesDocumentedSingleAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-charge"},
		{"RequestId": "req-clock"},
		{"RequestId": "req-maintenance"},
		{"RequestId": "req-metadata"},
		{"RequestId": "req-network-options"},
		{"RequestId": "req-network-spec"},
		{"RequestId": "req-vnc"},
		{"RequestId": "req-vpc"},
		{"RequestId": "req-public-ip"},
		{"RequestId": "req-system-disk"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--instance-charge-type", "PrePaid",
		"--period", "1",
		"--period-unit", "Month",
		"--auto-pay",
		"--clock-options", `{"ptp_status":"disabled"}`,
		"--maintenance-options", `{"action_on_maintenance":"AutoRecover","notify_on_maintenance":false,"maintenance_windows":[{"start_time":"02:00:00","end_time":"04:00:00"}]}`,
		"--http-endpoint", "enabled",
		"--http-tokens", "required",
		"--http-put-response-hop-limit", "2",
		"--network-options", `{"enable_jumbo_frame":true}`,
		"--internet-bandwidth-out", "10",
		"--internet-charge-type", "PayByTraffic",
		"--vnc-password", "secret",
		"--vswitch", "vsw-456",
		"--private-ip", "10.0.0.8",
		"--allocate-public-ip",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update documented APIs exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	wantOperations := []string{
		"ModifyInstanceChargeType",
		"ModifyInstanceClockOptions",
		"ModifyInstanceMaintenanceAttributes",
		"ModifyInstanceMetadataOptions",
		"ModifyInstanceNetworkOptions",
		"ModifyInstanceNetworkSpec",
		"ModifyInstanceVncPasswd",
		"ModifyInstanceVpcAttribute",
		"AllocatePublicIpAddress",
		"ReplaceSystemDisk",
	}
	if len(fake.calls) != len(wantOperations) {
		t.Fatalf("calls = %#v, want %d operations", fake.calls, len(wantOperations))
	}
	for i, want := range wantOperations {
		if fake.calls[i].operation != want {
			t.Fatalf("call %d operation = %s, want %s; calls=%#v", i, fake.calls[i].operation, want, fake.calls)
		}
		if want == "ModifyInstanceMaintenanceAttributes" {
			if fake.calls[i].request["InstanceId.1"] != "i-123" {
				t.Fatalf("%s request missing instance id: %#v", want, fake.calls[i].request)
			}
			continue
		}
		if fake.calls[i].request["InstanceId"] != "i-123" {
			t.Fatalf("%s request missing instance id: %#v", want, fake.calls[i].request)
		}
	}
	if fake.calls[0].request["InstanceChargeType"] != "PrePaid" || fake.calls[0].request["Period"] != 1 || fake.calls[0].request["AutoPay"] != true {
		t.Fatalf("ModifyInstanceChargeType request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["PtpStatus"] != "disabled" {
		t.Fatalf("ModifyInstanceClockOptions request = %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["ActionOnMaintenance"] != "AutoRecover" || fake.calls[2].request["NotifyOnMaintenance"] != false ||
		fake.calls[2].request["MaintenanceWindow.1.StartTime"] != "02:00:00" || fake.calls[2].request["MaintenanceWindow.1.EndTime"] != "04:00:00" {
		t.Fatalf("ModifyInstanceMaintenanceAttributes request = %#v", fake.calls[2].request)
	}
	if fake.calls[3].request["HttpEndpoint"] != "enabled" || fake.calls[3].request["HttpTokens"] != "required" || fake.calls[3].request["HttpPutResponseHopLimit"] != 2 {
		t.Fatalf("ModifyInstanceMetadataOptions request = %#v", fake.calls[3].request)
	}
	if fake.calls[5].request["InternetMaxBandwidthOut"] != 10 || fake.calls[5].request["InternetChargeType"] != "PayByTraffic" {
		t.Fatalf("ModifyInstanceNetworkSpec request = %#v", fake.calls[5].request)
	}
	if fake.calls[6].request["VncPassword"] != "secret" {
		t.Fatalf("ModifyInstanceVncPasswd request = %#v", fake.calls[6].request)
	}
	if fake.calls[7].request["VSwitchId"] != "vsw-456" || fake.calls[7].request["PrivateIpAddress"] != "10.0.0.8" {
		t.Fatalf("ModifyInstanceVpcAttribute request = %#v", fake.calls[7].request)
	}
	if fake.calls[9].request["ImageId"] != "aliyun_3_x64_20G_alibase_20240528.vhd" {
		t.Fatalf("ReplaceSystemDisk request = %#v", fake.calls[9].request)
	}
}

func TestECSInstanceUpdateRoutesPostpaidAndPrepaidSpecChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		operation string
		want      map[string]any
	}{
		{
			name:      "postpaid",
			args:      []string{"--type", "ecs.g6.large"},
			operation: "ModifyInstanceSpec",
			want:      map[string]any{"InstanceType": "ecs.g6.large"},
		},
		{
			name:      "prepaid",
			args:      []string{"--type", "ecs.g6.large", "--instance-charge-type", "PrePaid", "--period", "1"},
			operation: "ModifyPrepayInstanceSpec",
			want:      map[string]any{"InstanceType": "ecs.g6.large", "Period": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-spec"}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ecs" || resource.Resource != "instance" {
					t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
				}
				return fake, nil
			})

			args := append([]string{"ecs", "instance", "update", "i-123", "--region", "cn-beijing"}, tt.args...)
			args = append(args, "--no-wait")
			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ecs instance update %s spec exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v, want %s", fake.calls, tt.operation)
			}
			for key, want := range tt.want {
				if fake.calls[0].request[key] != want {
					t.Fatalf("%s = %#v, want %#v; request=%#v", key, fake.calls[0].request[key], want, fake.calls[0].request)
				}
			}
		})
	}
}

func TestECSInstanceUpdateRoutesRelationshipAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-ram-role"},
		{"RequestId": "req-keypair"},
		{"RequestId": "req-resource-group"},
		{"RequestId": "req-tag"},
		{"RequestId": "req-untag"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--ram-role", "web-role",
		"--key-pair", "kp-1",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
		"--remove-tag", "old",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update relationships exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	wantOperations := []string{
		"AttachInstanceRamRole",
		"AttachKeyPair",
		"JoinResourceGroup",
		"TagResources",
		"UntagResources",
	}
	if len(fake.calls) != len(wantOperations) {
		t.Fatalf("calls = %#v, want %d operations", fake.calls, len(wantOperations))
	}
	for i, want := range wantOperations {
		if fake.calls[i].operation != want {
			t.Fatalf("call %d operation = %s, want %s; calls=%#v", i, fake.calls[i].operation, want, fake.calls)
		}
	}
	if got, ok := fake.calls[0].request["InstanceIds"].([]string); !ok || len(got) != 1 || got[0] != "i-123" || fake.calls[0].request["RamRoleName"] != "web-role" {
		t.Fatalf("AttachInstanceRamRole request = %#v", fake.calls[0].request)
	}
	if got, ok := fake.calls[1].request["InstanceIds"].([]string); !ok || len(got) != 1 || got[0] != "i-123" || fake.calls[1].request["KeyPairName"] != "kp-1" {
		t.Fatalf("AttachKeyPair request = %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["ResourceGroupId"] != "rg-123" || fake.calls[2].request["ResourceId"] != "i-123" {
		t.Fatalf("JoinResourceGroup request = %#v", fake.calls[2].request)
	}
	if tags, ok := fake.calls[3].request["Tag"].([]string); !ok || len(tags) != 1 || tags[0] != "env=prod" || fake.calls[3].request["ResourceId.1"] != "i-123" {
		t.Fatalf("TagResources request = %#v", fake.calls[3].request)
	}
	if fake.calls[4].request["TagKey.1"] != "old" || fake.calls[4].request["ResourceId.1"] != "i-123" {
		t.Fatalf("UntagResources request = %#v", fake.calls[4].request)
	}
}

func TestECSInstanceUpdateRoutesRelationshipDetachAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-detach-ram-role"},
		{"RequestId": "req-detach-keypair"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--ram-role", "",
		"--key-pair", "",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update detach relationships exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DetachInstanceRamRole" || fake.calls[1].operation != "DetachKeyPair" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got, ok := fake.calls[0].request["InstanceIds"].([]string); !ok || len(got) != 1 || got[0] != "i-123" {
		t.Fatalf("DetachInstanceRamRole request = %#v", fake.calls[0].request)
	}
	if got, ok := fake.calls[1].request["InstanceIds"].([]string); !ok || len(got) != 1 || got[0] != "i-123" {
		t.Fatalf("DetachKeyPair request = %#v", fake.calls[1].request)
	}
}

func TestECSInstanceUpdateDiffsSecurityGroupMembership(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId":        "req-current",
			"InstanceId":       "i-123",
			"Status":           "Running",
			"SecurityGroupIds": map[string]any{"SecurityGroupId": []any{"sg-old", "sg-keep"}},
		},
		{"RequestId": "req-join"},
		{"RequestId": "req-leave"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "update", "i-123",
		"--region", "cn-beijing",
		"--security-group-ids", `["sg-keep","sg-new"]`,
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs instance update security groups exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "DescribeInstanceAttribute" || fake.calls[1].operation != "JoinSecurityGroup" || fake.calls[2].operation != "LeaveSecurityGroup" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["SecurityGroupId"] != "sg-new" {
		t.Fatalf("JoinSecurityGroup request = %#v", fake.calls[1].request)
	}
	if fake.calls[1].request["InstanceId"] != "i-123" {
		t.Fatalf("JoinSecurityGroup request = %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["SecurityGroupId"] != "sg-old" {
		t.Fatalf("LeaveSecurityGroup request = %#v", fake.calls[2].request)
	}
	if fake.calls[2].request["InstanceId"] != "i-123" {
		t.Fatalf("LeaveSecurityGroup request = %#v", fake.calls[2].request)
	}
}

func TestECSInstanceRenewUsesRenewInstance(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-renew"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "renew", "i-123", "--region", "cn-beijing", "--period", "1", "--period-unit", "Month")
	if code != 0 {
		t.Fatalf("ecs instance renew exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RenewInstance" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InstanceId"] != "i-123" || request["Period"] != 1 || request["PeriodUnit"] != "Month" {
		t.Fatalf("RenewInstance request = %#v", request)
	}
}

func TestECSInstanceExecUsesRunCommand(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-run-command", "InvokeId": "inv-123"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "exec", "i-123", "--region", "cn-beijing", "--command", "uptime", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance exec exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunCommand" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InstanceId.1"] != "i-123" || request["CommandContent"] != "uptime" || request["Type"] != "RunShellScript" {
		t.Fatalf("RunCommand request = %#v", request)
	}
}

func fakeECSInvocationStatusResponse(invokeID string, status string) map[string]any {
	invokeStatus := status
	if status == "Success" {
		invokeStatus = "Finished"
	}
	return map[string]any{
		"RequestId":  "req-describe-invocations",
		"TotalCount": 1,
		"Invocations": map[string]any{"Invocation": []any{
			map[string]any{
				"InvokeId":         invokeID,
				"CommandId":        "c-cmd1",
				"CommandName":      "uptime",
				"CommandType":      "RunShellScript",
				"InvokeStatus":     invokeStatus,
				"InvocationStatus": status,
				"CreationTime":     "2026-05-27T10:00:00Z",
			},
		}},
	}
}

func TestECSInstanceExecDefaultWaitsForCompletion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-run-command", "InvokeId": "inv-123"},
		fakeECSInvocationStatusResponse("inv-123", "Running"),
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "exec", "i-123", "--region", "cn-beijing", "--command", "uptime", "--timeout", "1ms")
	if code == 0 {
		t.Fatalf("ecs instance exec should wait for completion and time out; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error code = %q, want WaitTimeout; stdout=%s stderr=%s", got, stdout, stderr)
	}
	// The 1ms timeout can elapse after one or more polls, so assert at least one
	// DescribeInvocations poll rather than an exact call count.
	if len(fake.calls) < 2 || fake.calls[0].operation != "RunCommand" || fake.calls[1].operation != "DescribeInvocations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSInstanceExecResultsIncludeDecodedOutputText(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-run-command", "InvokeId": "inv-123"},
		fakeECSInvocationStatusResponse("inv-123", "Success"),
		{
			"RequestId": "req-results",
			"Invocation": map[string]any{"InvocationResults": map[string]any{"InvocationResult": []any{
				map[string]any{
					"InvokeId":         "inv-123",
					"InstanceId":       "i-123",
					"InvocationStatus": "Success",
					"ExitCode":         0,
					"Output":           "aGVsbG8K",
				},
			}}},
		},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "exec", "i-123", "--region", "cn-beijing", "--command", "printf hello")
	if code != 0 {
		t.Fatalf("ecs instance exec exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "RunCommand" || fake.calls[1].operation != "DescribeInvocations" || fake.calls[2].operation != "DescribeInvocationResults" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	results, _ := decodeObject(t, stdout)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("unexpected results: %s", stdout)
	}
	result, _ := results[0].(map[string]any)
	if result["output"] != "aGVsbG8K" || result["output_text"] != "hello\n" {
		t.Fatalf("exec output mapping = %#v; stdout=%s", result, stdout)
	}
}

func TestECSInstanceExecResultsKeepRawOutputWhenOutputIsInvalidBase64(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-run-command", "InvokeId": "inv-123"},
		fakeECSInvocationStatusResponse("inv-123", "Success"),
		{
			"RequestId": "req-results",
			"Invocation": map[string]any{"InvocationResults": map[string]any{"InvocationResult": []any{
				map[string]any{
					"InvokeId":         "inv-123",
					"InstanceId":       "i-123",
					"InvocationStatus": "Success",
					"ExitCode":         0,
					"Output":           "not-base64!",
				},
			}}},
		},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "exec", "i-123", "--region", "cn-beijing", "--command", "printf hello")
	if code != 0 {
		t.Fatalf("ecs instance exec exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	results, _ := decodeObject(t, stdout)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("unexpected results: %s", stdout)
	}
	result, _ := results[0].(map[string]any)
	if result["output"] != "not-base64!" {
		t.Fatalf("raw output = %#v, want not-base64!; stdout=%s", result["output"], stdout)
	}
	if outputText, ok := result["output_text"]; ok && outputText != "" {
		t.Fatalf("output_text = %#v, want omitted or empty on invalid base64; stdout=%s", outputText, stdout)
	}
}

func TestECSInstanceExecCommandCanLoadFile(t *testing.T) {
	t.Parallel()
	commandPath := filepath.Join(t.TempDir(), "command.sh")
	command := "set -e\nuptime\n"
	if err := os.WriteFile(commandPath, []byte(command), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-run-command", "InvokeId": "inv-123"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "exec", "i-123", "--region", "cn-beijing", "--command", "@"+commandPath, "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance exec --command @file exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RunCommand" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["CommandContent"] != command {
		t.Fatalf("RunCommand request = %#v", fake.calls[0].request)
	}
}

func TestECSInstanceSendfileUsesSendFile(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-sendfile", "InvokeId": "sf-123"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "sendfile", "i-123", "--region", "cn-beijing", "--file-name", "app.conf", "--target-dir", "/tmp", "--content", "hello", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance sendfile exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "SendFile" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InstanceId.1"] != "i-123" || request["Name"] != "app.conf" || request["TargetDir"] != "/tmp" || request["Content"] != "hello" {
		t.Fatalf("SendFile request = %#v", request)
	}
}

func fakeECSSendFileResultsResponse(invokeID string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-sendfile-results",
		"TotalCount": 1,
		"Invocations": map[string]any{"Invocation": []any{
			map[string]any{
				"InvokeId":         invokeID,
				"Name":             "app.conf",
				"TargetDir":        "/tmp",
				"InvocationStatus": status,
				"StartTime":        "2026-05-27T10:00:00Z",
				"FinishTime":       "2026-05-27T10:00:05Z",
			},
		}},
	}
}

func TestECSInstanceSendfileDefaultWaitsForCompletion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-sendfile", "InvokeId": "sf-123"},
		fakeECSSendFileResultsResponse("sf-123", "Running"),
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "sendfile", "i-123", "--region", "cn-beijing", "--file-name", "app.conf", "--target-dir", "/tmp", "--content", "hello", "--timeout", "1ms")
	if code == 0 {
		t.Fatalf("ecs instance sendfile should wait for completion and time out; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error code = %q, want WaitTimeout; stdout=%s stderr=%s", got, stdout, stderr)
	}
	// The 1ms timeout can elapse after one or more poll attempts, so assert that
	// SendFile is followed by at least one DescribeSendFileResults poll rather
	// than an exact call count.
	if len(fake.calls) < 2 || fake.calls[0].operation != "SendFile" || fake.calls[1].operation != "DescribeSendFileResults" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSInstanceMonitorUsesDescribeInstanceMonitorData(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-monitor",
		"MonitorData": map[string]any{"InstanceMonitorData": []any{
			map[string]any{"InstanceId": "i-123", "CPU": "3", "TimeStamp": "2026-05-27T00:00:00Z"},
		}},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "instance", "monitor", "i-123", "--region", "cn-beijing", "--start-time", "2026-05-27T00:00:00Z", "--end-time", "2026-05-27T00:01:00Z")
	if code != 0 {
		t.Fatalf("ecs instance monitor exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeInstanceMonitorData" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["InstanceId"] != "i-123" || request["StartTime"] != "2026-05-27T00:00:00Z" || request["EndTime"] != "2026-05-27T00:01:00Z" {
		t.Fatalf("DescribeInstanceMonitorData request = %#v", request)
	}
	out := decodeObject(t, stdout)
	if items, _ := out["monitor_data"].([]any); len(items) != 1 {
		t.Fatalf("unexpected monitor output: %s", stdout)
	}
}
