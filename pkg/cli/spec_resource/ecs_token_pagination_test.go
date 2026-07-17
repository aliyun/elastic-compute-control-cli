package spec_resource

import (
	"strconv"
	"testing"
)

func TestECSListCommandsUseTokenPagination(t *testing.T) {
	for _, tt := range []struct {
		name      string
		resource  string
		operation string
		args      []string
		response  map[string]any
		limit     int
	}{
		{
			name: "command templates", resource: "command", operation: "DescribeCommands", limit: 50,
			args:     []string{"ecs", "command", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "Commands": map[string]any{"Command": []any{}}},
		},
		{
			name: "command invocations", resource: "command", operation: "DescribeInvocations", limit: 50,
			args:     []string{"ecs", "command", "list", "--region", "cn-hangzhou", "--invocations"},
			response: map[string]any{"NextToken": "token-2", "Invocations": map[string]any{"Invocation": []any{}}},
		},
		{
			name: "disks", resource: "disk", operation: "DescribeDisks", limit: 500,
			args:     []string{"ecs", "disk", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "Disks": map[string]any{"Disk": []any{}}},
		},
		{
			name: "network interfaces", resource: "eni", operation: "DescribeNetworkInterfaces", limit: 500,
			args:     []string{"ecs", "eni", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "NetworkInterfaceSets": map[string]any{"NetworkInterfaceSet": []any{}}},
		},
		{
			name: "instances", resource: "instance", operation: "DescribeInstances", limit: 100,
			args:     []string{"ecs", "instance", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "Instances": map[string]any{"Instance": []any{}}},
		},
		{
			name: "security groups", resource: "sg", operation: "DescribeSecurityGroups", limit: 100,
			args:     []string{"ecs", "sg", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "SecurityGroups": map[string]any{"SecurityGroup": []any{}}},
		},
		{
			name: "snapshots", resource: "snapshot", operation: "DescribeSnapshots", limit: 100,
			args:     []string{"ecs", "snapshot", "list", "--region", "cn-hangzhou"},
			response: map[string]any{"NextToken": "token-2", "Snapshots": map[string]any{"Snapshot": []any{}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{tt.response}}
			runCLI := catalogCaller(t, "ecs", tt.resource, fake)
			args := append(append([]string{}, tt.args...), "--limit", strconv.Itoa(tt.limit), "--next-token", "token-1")

			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ecs %s list exit %d stderr=%s stdout=%s", tt.resource, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			request := fake.calls[0].request
			if request["MaxResults"] != tt.limit || request["NextToken"] != "token-1" {
				t.Fatalf("%s token request = %#v", tt.operation, request)
			}
			if _, ok := request["PageNumber"]; ok {
				t.Fatalf("%s request still contains PageNumber: %#v", tt.operation, request)
			}
			if _, ok := request["PageSize"]; ok {
				t.Fatalf("%s request still contains PageSize: %#v", tt.operation, request)
			}

			out := decodeObject(t, stdout)
			pagination, _ := out["pagination"].(map[string]any)
			if pagination["next_token"] != "token-2" || pagination["has_more"] != true {
				t.Fatalf("token pagination = %#v; stdout=%s", pagination, stdout)
			}
			if _, ok := pagination["page"]; ok {
				t.Fatalf("token pagination must not include page: %#v", pagination)
			}
			if _, ok := out["total"]; ok {
				t.Fatalf("token pagination must omit meaningless total: %s", stdout)
			}
		})
	}
}
