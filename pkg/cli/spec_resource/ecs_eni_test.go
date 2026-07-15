package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestECSENICreateUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "NetworkInterfaceId": "eni-123"},
			fakeNetworkInterfaceAttributeResponse("eni-123", "Available"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "create", "--region", "cn-beijing", "--vswitch", "vsw-123", "--sg", "sg-123", "--sg", "sg-456", "--name", "web-eni")
	if code != 0 {
		t.Fatalf("ecs eni create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateNetworkInterface" || fake.calls[1].operation != "DescribeNetworkInterfaceAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["VSwitchId"] != "vsw-123" || createReq["SecurityGroupIds.1"] != "sg-123" || createReq["SecurityGroupIds.2"] != "sg-456" || createReq["NetworkInterfaceName"] != "web-eni" {
		t.Fatalf("CreateNetworkInterface request = %#v", createReq)
	}
	if _, ok := createReq["SecurityGroupId"]; ok {
		t.Fatalf("CreateNetworkInterface must not use SecurityGroupId: %#v", createReq)
	}
	if _, ok := createReq["ClientToken"]; !ok {
		t.Fatalf("CreateNetworkInterface must receive ClientToken: %#v", createReq)
	}
	eni, _ := decodeObject(t, stdout)["eni"].(map[string]any)
	if eni == nil || eni["id"] != "eni-123" || eni["status"] != "Available" || eni["vswitch"] != "vsw-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSENICreateSupportsCompleteStructuredParameters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "NetworkInterfaceId": "eni-123"},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"ecs", "eni", "create",
		"--region", "cn-beijing",
		"--vswitch", "vsw-123",
		"--sg", "sg-123",
		"--type", "Secondary",
		"--traffic-config", "network-interface-traffic-mode=HighPerformance,queue-number=2,queue-pair-number=1,rx-queue-size=8192,tx-queue-size=8192",
		"--enhanced-network", "enable-sriov=true,enable-rss=true,virtual-function-quantity=2,virtual-function-total-queue-number=8",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs eni create structured params exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateNetworkInterface" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"SecurityGroupIds.1": "sg-123",
		"InstanceType":       "Secondary",
		"NetworkInterfaceTrafficConfig.NetworkInterfaceTrafficMode": "HighPerformance",
		"NetworkInterfaceTrafficConfig.QueueNumber":                 2,
		"NetworkInterfaceTrafficConfig.QueuePairNumber":             1,
		"NetworkInterfaceTrafficConfig.RxQueueSize":                 8192,
		"NetworkInterfaceTrafficConfig.TxQueueSize":                 8192,
		"EnhancedNetwork.EnableSriov":                               true,
		"EnhancedNetwork.EnableRss":                                 true,
		"EnhancedNetwork.VirtualFunctionQuantity":                   2,
		"EnhancedNetwork.VirtualFunctionTotalQueueNumber":           8,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("CreateNetworkInterface request[%s] = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
}

func TestECSENISchemaDeclaresStructuredParameters(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.eni.create")
	if code != 0 {
		t.Fatalf("schema ecs.eni.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	createParams, _ := decodeObject(t, stdout)["params"].(map[string]any)
	sgParam, _ := createParams["sg"].(map[string]any)
	if sgParam == nil || sgParam["type"] != "string_array" {
		t.Fatalf("sg schema = %#v; stdout=%s", sgParam, stdout)
	}
	if _, ok := createParams["security-group-ids"]; ok {
		t.Fatalf("create schema must not expose security-group-ids: %s", stdout)
	}
	trafficConfig, _ := createParams["traffic-config"].(map[string]any)
	if trafficConfig == nil || trafficConfig["input"] != "inline-key-value|json|@file" {
		t.Fatalf("traffic-config schema = %#v; stdout=%s", trafficConfig, stdout)
	}
	trafficFields, _ := trafficConfig["fields"].(map[string]any)
	for _, name := range []string{"network_interface_traffic_mode", "queue_number", "queue_pair_number", "rx_queue_size", "tx_queue_size"} {
		if _, ok := trafficFields[name]; !ok {
			t.Fatalf("traffic-config schema missing %s: %s", name, stdout)
		}
	}
	enhancedNetwork, _ := createParams["enhanced-network"].(map[string]any)
	enhancedFields, _ := enhancedNetwork["fields"].(map[string]any)
	for _, name := range []string{"enable_sriov", "enable_rss", "virtual_function_quantity", "virtual_function_total_queue_number"} {
		if _, ok := enhancedFields[name]; !ok {
			t.Fatalf("enhanced-network schema missing %s: %s", name, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ecs.eni.update")
	if code != 0 {
		t.Fatalf("schema ecs.eni.update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	updateParams, _ := decodeObject(t, stdout)["params"].(map[string]any)
	updateSGParam, _ := updateParams["sg"].(map[string]any)
	if updateSGParam == nil || updateSGParam["type"] != "string_array" {
		t.Fatalf("update sg schema = %#v; stdout=%s", updateSGParam, stdout)
	}
	if _, ok := updateParams["security-group-ids"]; ok {
		t.Fatalf("update schema must not expose security-group-ids: %s", stdout)
	}
	qos, _ := updateParams["qos"].(map[string]any)
	if qos == nil || qos["input"] != "inline-key-value|json|@file" {
		t.Fatalf("qos schema = %#v; stdout=%s", qos, stdout)
	}
	qosFields, _ := qos["fields"].(map[string]any)
	for _, name := range []string{"status", "bandwidth_tx", "bandwidth_rx", "pps_tx", "pps_rx", "concurrent_connections"} {
		if _, ok := qosFields[name]; !ok {
			t.Fatalf("qos schema missing %s: %s", name, stdout)
		}
	}
	for _, name := range []string{"private-ip", "ipv4-prefix", "ipv6-address", "ipv6-prefix"} {
		param, _ := updateParams[name].(map[string]any)
		if param == nil || param["type"] != "string_array" || param["input"] != "+value|-value" {
			t.Fatalf("%s schema = %#v; stdout=%s", name, param, stdout)
		}
	}
	for _, removed := range []string{
		"assign-private-ip", "unassign-private-ip",
		"assign-private-ip-count",
		"assign-ipv4-prefix", "unassign-ipv4-prefix",
		"assign-ipv4-prefix-count",
		"assign-ipv6", "unassign-ipv6",
		"assign-ipv6-count",
		"assign-ipv6-prefix", "unassign-ipv6-prefix",
		"assign-ipv6-prefix-count",
		"attach-instance-id", "detach-instance-id",
		"enable-qos", "disable-qos",
	} {
		if _, ok := updateParams[removed]; ok {
			t.Fatalf("update schema must not expose %s: %s", removed, stdout)
		}
	}
}

func TestECSENIUpdateHelpShowsPrefixDeltaSyntax(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "eni", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs eni update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, snippet := range []string{"--private-ip", "--ipv4-prefix", "--ipv6-address", "--ipv6-prefix", "+value assigns, -value unassigns"} {
		if !strings.Contains(stdout, snippet) {
			t.Fatalf("help missing %q: %s", snippet, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "zh-CN", "ecs", "eni", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs eni update --help zh-CN exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "+值表示分配，-值表示回收") {
		t.Fatalf("Chinese help missing localized input style: %s", stdout)
	}
	if strings.Contains(stdout, "+value") {
		t.Fatalf("Chinese help must not use English input style: %s", stdout)
	}
	if !strings.Contains(stdout, "内联 key=value、JSON 对象或 @file") {
		t.Fatalf("Chinese help missing localized structured input style: %s", stdout)
	}
	if strings.Contains(stdout, "inline key=value") {
		t.Fatalf("Chinese help must not use English structured input style: %s", stdout)
	}
}

func TestECSENIUpdateAssignPrivateIPCallsOnlySelectedBinding(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-assign"},
			fakeNetworkInterfaceAttributeResponse("eni-123", "InUse"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "update", "eni-123", "--region", "cn-beijing", "--private-ip", "+10.0.0.10")
	if code != 0 {
		t.Fatalf("ecs eni update assign private IP exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "AssignPrivateIpAddresses" || fake.calls[1].operation != "DescribeNetworkInterfaceAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["NetworkInterfaceId"] != "eni-123" || request["PrivateIpAddress.1"] != "10.0.0.10" {
		t.Fatalf("AssignPrivateIpAddresses request = %#v", request)
	}
	if _, ok := request["ClientToken"]; !ok {
		t.Fatalf("AssignPrivateIpAddresses must receive ClientToken: %#v", request)
	}
	eni, _ := decodeObject(t, stdout)["eni"].(map[string]any)
	if eni == nil || eni["id"] != "eni-123" || eni["status"] != "InUse" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSENIUpdateFlagsSelectDocumentedAPIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		operation string
		request   map[string]any
	}{
		{
			name:      "attributes",
			args:      []string{"--name", "web-2"},
			operation: "ModifyNetworkInterfaceAttribute",
			request:   map[string]any{"NetworkInterfaceName": "web-2"},
		},
		{
			name:      "traffic config",
			args:      []string{"--traffic-config", "network-interface-traffic-mode=HighPerformance,queue-pair-number=4"},
			operation: "ModifyNetworkInterfaceAttribute",
			request: map[string]any{
				"NetworkInterfaceTrafficConfig.NetworkInterfaceTrafficMode": "HighPerformance",
				"NetworkInterfaceTrafficConfig.QueuePairNumber":             4,
			},
		},
		{
			name:      "security groups",
			args:      []string{"--sg", "sg-123", "--sg", "sg-456"},
			operation: "ModifyNetworkInterfaceAttribute",
			request: map[string]any{
				"SecurityGroupIds.1": "sg-123",
				"SecurityGroupIds.2": "sg-456",
			},
		},
		{
			name:      "unassign private ip",
			args:      []string{"--private-ip=-10.0.0.10"},
			operation: "UnassignPrivateIpAddresses",
			request:   map[string]any{"PrivateIpAddress.1": "10.0.0.10"},
		},
		{
			name:      "assign ipv4 prefix",
			args:      []string{"--ipv4-prefix", "+10.0.0.0/28"},
			operation: "AssignPrivateIpAddresses",
			request:   map[string]any{"Ipv4Prefix.1": "10.0.0.0/28"},
		},
		{
			name:      "unassign ipv4 prefix",
			args:      []string{"--ipv4-prefix=-10.0.0.0/28"},
			operation: "UnassignPrivateIpAddresses",
			request:   map[string]any{"Ipv4Prefix.1": "10.0.0.0/28"},
		},
		{
			name:      "assign ipv6 address",
			args:      []string{"--ipv6-address", "+2408::1"},
			operation: "AssignIpv6Addresses",
			request:   map[string]any{"Ipv6Address.1": "2408::1"},
		},
		{
			name:      "unassign ipv6 address",
			args:      []string{"--ipv6-address=-2408::1"},
			operation: "UnassignIpv6Addresses",
			request:   map[string]any{"Ipv6Address.1": "2408::1"},
		},
		{
			name:      "assign ipv6 prefix",
			args:      []string{"--ipv6-prefix", "+2408::/64"},
			operation: "AssignIpv6Addresses",
			request:   map[string]any{"Ipv6Prefix.1": "2408::/64"},
		},
		{
			name:      "unassign ipv6 prefix",
			args:      []string{"--ipv6-prefix=-2408::/64"},
			operation: "UnassignIpv6Addresses",
			request:   map[string]any{"Ipv6Prefix.1": "2408::/64"},
		},
		{
			name:      "enable qos",
			args:      []string{"--qos", "status=enable,bandwidth-tx=50000,pps-rx=10000,concurrent-connections=20000"},
			operation: "EnableNetworkInterfaceQoS",
			request: map[string]any{
				"QoS.BandwidthTx":           50000,
				"QoS.PpsRx":                 10000,
				"QoS.ConcurrentConnections": 20000,
			},
		},
		{
			name:      "disable qos",
			args:      []string{"--qos", "status=disable"},
			operation: "DisableNetworkInterfaceQoS",
			request:   map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-update"}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ecs" || resource.Resource != "eni" {
					t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
				}
				return fake, nil
			})

			args := append([]string{"ecs", "eni", "update", "eni-123", "--region", "cn-beijing", "--no-wait"}, tt.args...)
			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ecs eni update %s exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			if fake.calls[0].request["NetworkInterfaceId"] != "eni-123" {
				t.Fatalf("%s request = %#v", tt.operation, fake.calls[0].request)
			}
			for key, want := range tt.request {
				if got := fake.calls[0].request[key]; got != want {
					t.Fatalf("%s request[%s] = %#v, want %#v; request=%#v", tt.operation, key, got, want, fake.calls[0].request)
				}
			}
		})
	}
}

func TestECSENIAttachDetachUseDesignedAPIs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		operation string
		request   map[string]any
	}{
		{
			name:      "attach",
			args:      []string{"attach", "eni-123", "--instance", "i-123", "--network-card-index", "1", "--wait-for-network-configuration-ready"},
			operation: "AttachNetworkInterface",
			request: map[string]any{
				"InstanceId":                       "i-123",
				"NetworkCardIndex":                 1,
				"WaitForNetworkConfigurationReady": true,
			},
		},
		{
			name:      "detach",
			args:      []string{"detach", "eni-123", "--instance", "i-123", "--trunk-network-instance-id", "i-trunk"},
			operation: "DetachNetworkInterface",
			request: map[string]any{
				"InstanceId":             "i-123",
				"TrunkNetworkInstanceId": "i-trunk",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-" + tt.name}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ecs" || resource.Resource != "eni" {
					t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
				}
				return fake, nil
			})

			args := append([]string{"ecs", "eni"}, tt.args...)
			args = append(args, "--region", "cn-beijing", "--no-wait")
			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ecs eni %s exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			request := fake.calls[0].request
			if request["NetworkInterfaceId"] != "eni-123" {
				t.Fatalf("%s request = %#v", tt.operation, request)
			}
			for key, want := range tt.request {
				if got := request[key]; got != want {
					t.Fatalf("%s request[%s] = %#v, want %#v; request=%#v", tt.operation, key, got, want, request)
				}
			}
		})
	}
}

func TestECSENIUpdateDoesNotExposeAttachDetachFlags(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("removed attach and detach flags should fail before creating a caller")
		return nil, nil
	})

	for _, removed := range []string{"--attach-instance-id", "--detach-instance-id", "--network-card-index", "--trunk-network-instance-id", "--wait-for-network-configuration-ready"} {
		stdout, stderr, code := runCLI("ecs", "eni", "update", "eni-123", "--region", "cn-beijing", removed, "i-123", "--no-wait")
		if code == 0 {
			t.Fatalf("ecs eni update %s succeeded stdout=%s stderr=%s", removed, stdout, stderr)
		}
	}
}

func TestECSENIUpdateRequiresSignedAddressDeltaBeforeCloudCall(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("unsigned address delta should fail before creating a caller")
		return nil, nil
	})

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "eni", "update", "eni-123", "--region", "cn-beijing", "--private-ip", "10.0.0.10", "--no-wait")
	if code == 0 {
		t.Fatalf("ecs eni update unsigned private ip succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "InvalidParameter" {
		t.Fatalf("error.code = %q, want InvalidParameter; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "+value or -value") {
		t.Fatalf("message should mention +value or -value, got %q", message)
	}
}

func TestECSENIUpdateRequiresQOSStatusBeforeCloudCall(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("qos status validation should fail before creating a caller")
		return nil, nil
	})

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "eni", "update", "eni-123", "--region", "cn-beijing", "--qos", "bandwidth-tx=50000", "--no-wait")
	if code == 0 {
		t.Fatalf("ecs eni update --qos without status succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "status") {
		t.Fatalf("message should mention status, got %q", message)
	}
}

func TestECSENIListUsesFiltersAndPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"RequestId":  "req-list",
			"TotalCount": 1,
			"NetworkInterfaceSets": map[string]any{"NetworkInterfaceSet": []any{
				map[string]any{
					"NetworkInterfaceId":   "eni-123",
					"NetworkInterfaceName": "web-eni",
					"Status":               "Available",
					"VpcId":                "vpc-123",
					"VSwitchId":            "vsw-123",
				},
			}},
		}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "list", "--region", "cn-beijing", "--filter", "status=Available", "--filter", "vpc=vpc-123", "--filter", "private-ip=10.0.0.10", "--filter", "ipv6-address=2408::1", "--filter", "service-managed=true", "--limit", "5")
	if code != 0 {
		t.Fatalf("ecs eni list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeNetworkInterfaces" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Status"] != "Available" || request["VpcId"] != "vpc-123" || request["PrivateIpAddress.1"] != "10.0.0.10" || request["Ipv6Address.1"] != "2408::1" || request["ServiceManaged"] != true || request["MaxResults"] != 5 {
		t.Fatalf("DescribeNetworkInterfaces request = %#v", request)
	}
	if _, ok := request["NextToken"]; ok {
		t.Fatalf("first DescribeNetworkInterfaces page must omit NextToken: %#v", request)
	}
	out := decodeObject(t, stdout)
	enis, _ := out["enis"].([]any)
	if len(enis) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if _, ok := out["total"]; ok {
		t.Fatalf("token pagination must omit meaningless total: %s", stdout)
	}
}

func TestECSENIGetWithMonitorUsesAttributeAndMonitorAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeNetworkInterfaceAttributeResponse("eni-123", "InUse"),
			{
				"RequestId":  "req-monitor",
				"TotalCount": 1,
				"MonitorData": map[string]any{"EniMonitorData": []any{
					map[string]any{
						"EniId":      "eni-123",
						"TimeStamp":  "2026-05-27T00:00:00Z",
						"IntranetRx": float64(10),
						"IntranetTx": float64(20),
					},
				}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "get", "eni-123", "--region", "cn-beijing", "--attribute", "connectionTrackingConfiguration", "--with-monitor", "--instance", "i-123", "--start-time", "2026-05-27T00:00:00Z", "--end-time", "2026-05-27T00:05:00Z", "--period", "60")
	if code != 0 {
		t.Fatalf("ecs eni get --with-monitor exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeNetworkInterfaceAttribute" || fake.calls[1].operation != "DescribeEniMonitorData" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["Attribute"] != "connectionTrackingConfiguration" {
		t.Fatalf("DescribeNetworkInterfaceAttribute request = %#v", fake.calls[0].request)
	}
	monitorReq := fake.calls[1].request
	if monitorReq["EniId"] != "eni-123" || monitorReq["InstanceId"] != "i-123" || monitorReq["StartTime"] != "2026-05-27T00:00:00Z" || monitorReq["Period"] != 60 {
		t.Fatalf("DescribeEniMonitorData request = %#v", monitorReq)
	}
	out := decodeObject(t, stdout)
	eni, _ := out["eni"].(map[string]any)
	monitorData, _ := out["monitor_data"].([]any)
	if eni == nil || eni["id"] != "eni-123" || len(monitorData) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSENIDeleteUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "delete", "eni-123", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs eni delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteNetworkInterface" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["NetworkInterfaceId"] != "eni-123" {
		t.Fatalf("DeleteNetworkInterface request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	eni, _ := out["eni"].(map[string]any)
	if out["deleted"] != true || eni == nil || eni["id"] != "eni-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSENIDeleteWaitsForRequestedID(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-delete"},
		{
			"RequestId":  "req-list",
			"TotalCount": 0,
			"NetworkInterfaceSets": map[string]any{
				"NetworkInterfaceSet": []any{},
			},
		},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want ecs/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "eni", "delete", "eni-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs eni delete wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DeleteNetworkInterface" || fake.calls[1].operation != "DescribeNetworkInterfaces" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["NetworkInterfaceId.1"] != "eni-123" {
		t.Fatalf("DescribeNetworkInterfaces waiter request = %#v", fake.calls[1].request)
	}
}
