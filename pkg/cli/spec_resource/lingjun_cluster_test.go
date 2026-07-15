package spec_resource

import (
	"reflect"
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestLingjunClusterListUsesListClusters(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-list",
		"NextToken": "next-2",
		"Clusters": []any{
			map[string]any{
				"ClusterId":          "cluster-1",
				"ClusterName":        "train",
				"ClusterDescription": "training cluster",
				"ClusterType":        "Lite",
				"OperatingState":     "running",
			},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "cluster" || resource.APIProduct != "eflo-controller" {
			t.Fatalf("resource = %s/%s api=%s, want lingjun/cluster api eflo-controller", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "cluster", "list", "--region", "cn-beijing", "--limit", "25", "--next-token", "next-1", "--filter", "tag.env=prod")
	if code != 0 {
		t.Fatalf("lingjun cluster list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListClusters" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["MaxResults"] != 25 || request["NextToken"] != "next-1" || request["Tags.1.Key"] != "env" || request["Tags.1.Value"] != "prod" {
		t.Fatalf("ListClusters request = %#v", request)
	}
	payload := decodeObject(t, stdout)
	clusters, _ := payload["clusters"].([]any)
	if len(clusters) != 1 {
		t.Fatalf("clusters missing from output: %s", stdout)
	}
	pagination, _ := payload["pagination"].(map[string]any)
	if pagination["next_token"] != "next-2" {
		t.Fatalf("pagination = %#v, want next_token next-2", pagination)
	}
}

func TestLingjunRejectsDirectCommands(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-list", "Clusters": []any{}}}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	_, _, code := runCLI("lingjun", "list", "--region", "cn-beijing")
	if code == 0 {
		t.Fatalf("ecctl lingjun list should fail without cluster subcommand")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("no API calls expected: %#v", fake.calls)
	}
}

func TestLingjunClusterCreateMapsClusterFields(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-create",
		"ClusterId": "cluster-1",
	}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "cluster" || resource.APIProduct != "eflo-controller" {
			t.Fatalf("resource = %s/%s api=%s, want lingjun/cluster api eflo-controller", resource.Product, resource.Resource, resource.APIProduct)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "cluster", "create",
		"--region", "cn-beijing",
		"--name", "train",
		"--description", "training cluster",
		"--cluster-type", "Lite",
		"--hpn-zone", "A1",
		"--components", `[{"ComponentType":"monitor","ComponentConfig":{"BasicArgs":{"cpu":"2"}}}]`,
		"--networks", `{"VpdInfo":{"VpdId":"vpd-1"}}`,
		"--node-groups", `[{"NodeGroupId":"ng-1","NodeCount":2}]`,
		"--nimiz-vswitches", `["vsw-1","vsw-2"]`,
		"--resource-group", "rg-1",
		"--tag", "env=prod",
		"--open-eni-jumbo-frame",
		"--ignore-failed-node-tasks",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("lingjun cluster create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateCluster" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"ClusterName":           "train",
		"ClusterDescription":    "training cluster",
		"ClusterType":           "Lite",
		"HpnZone":               "A1",
		"ResourceGroupId":       "rg-1",
		"OpenEniJumboFrame":     true,
		"IgnoreFailedNodeTasks": true,
		"Components":            `[{"ComponentType":"monitor","ComponentConfig":{"BasicArgs":{"cpu":"2"}}}]`,
		"Networks":              `{"VpdInfo":{"VpdId":"vpd-1"}}`,
		"NodeGroups":            `[{"NodeGroupId":"ng-1","NodeCount":2}]`,
		"NimizVSwitches":        `["vsw-1","vsw-2"]`,
		"Tag":                   []string{"env=prod"},
	}
	for key, value := range want {
		if !reflect.DeepEqual(request[key], value) {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
}

func TestLingjunClusterUpdateSelectsExtendOrShrinkAndRejectsBoth(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCall  string
		wantField string
	}{
		{
			name:      "extend",
			args:      []string{"lingjun", "cluster", "update", "cluster-1", "--region", "cn-beijing", "--extend", `[{"NodeGroupId":"ng-1","NodeCount":1}]`, "--no-wait"},
			wantCall:  "ExtendCluster",
			wantField: "NodeGroups",
		},
		{
			name:      "shrink",
			args:      []string{"lingjun", "cluster", "update", "cluster-1", "--region", "cn-beijing", "--shrink", `[{"NodeGroupId":"ng-1","Nodes":[{"NodeId":"n-1"}]}]`, "--no-wait"},
			wantCall:  "ShrinkCluster",
			wantField: "NodeGroups",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-update"}}}
			runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				return fake, nil
			})

			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%s exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.wantCall {
				t.Fatalf("calls = %#v, want %s", fake.calls, tt.wantCall)
			}
			if fake.calls[0].request["ClusterId"] != "cluster-1" || fake.calls[0].request[tt.wantField] == nil {
				t.Fatalf("%s request = %#v", tt.wantCall, fake.calls[0].request)
			}
		})
	}

	fake := &fakeSpecCaller{}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})
	stdout, _, code := runCLI(
		"lingjun", "cluster", "update", "cluster-1",
		"--region", "cn-beijing",
		"--extend", `[{"NodeGroupId":"ng-1"}]`,
		"--shrink", `[{"NodeGroupId":"ng-1"}]`,
	)
	if code == 0 || errorCode(t, stdout) != "ConflictingParameters" {
		t.Fatalf("extend+shrink should fail with ConflictingParameters, code=%d stdout=%s", code, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
}

func TestLingjunClusterGetWithNodesRunsExtraProbe(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"RequestId":      "req-describe",
			"ClusterId":      "cluster-1",
			"ClusterName":    "train",
			"OperatingState": "running",
		},
		{
			"RequestId": "req-nodes",
			"Nodes": []any{
				map[string]any{"NodeId": "node-1", "Hostname": "worker-1"},
			},
		},
	}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "cluster", "get", "cluster-1", "--region", "cn-beijing", "--with-nodes")
	if code != 0 {
		t.Fatalf("lingjun cluster get --with-nodes exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	got := callNames(fake.calls)
	if strings.Join(got, ",") != "DescribeCluster,ListClusterNodes" {
		t.Fatalf("calls = %#v, want DescribeCluster,ListClusterNodes", got)
	}
	if fake.calls[1].request["ClusterId"] != "cluster-1" {
		t.Fatalf("ListClusterNodes request = %#v", fake.calls[1].request)
	}
	cluster, _ := decodeObject(t, stdout)["cluster"].(map[string]any)
	nodes, _ := cluster["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes missing from output: %s", stdout)
	}
}
