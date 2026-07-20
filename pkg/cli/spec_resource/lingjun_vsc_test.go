package spec_resource

import (
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func fakeLingjunVSCResponse(id, name, node, vscType, status string) map[string]any {
	return map[string]any{
		"RequestId":       "req-describe",
		"VscId":           id,
		"VscName":         name,
		"NodeId":          node,
		"VscType":         vscType,
		"Status":          status,
		"ResourceGroupId": "rg-123",
	}
}

func lingjunVSCCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "vsc" || resource.APIProduct != "eflo-controller" {
			t.Fatalf("resource = %s/%s api=%s, want lingjun/vsc api eflo-controller", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func TestLingjunVSCCreateMapsRequestAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "VscId": "vsc-123"},
		fakeLingjunVSCResponse("vsc-123", "serial-a", "node-123", "primary", "Normal"),
	}}
	runCLI := lingjunVSCCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vsc", "create",
		"--region", "cn-beijing",
		"--node", "node-123",
		"--name", "serial-a",
		"--type", "primary",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun vsc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateVsc" || fake.calls[1].operation != "DescribeVsc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	want := map[string]any{
		"NodeId":          "node-123",
		"VscName":         "serial-a",
		"VscType":         "primary",
		"ResourceGroupId": "rg-123",
		"Tag":             []string{"env=prod"},
	}
	for key, value := range want {
		if !reflect.DeepEqual(req[key], value) {
			t.Fatalf("CreateVsc request[%s] = %#v, want %#v; request=%#v", key, req[key], value, req)
		}
	}
	if _, ok := req["ClientToken"]; !ok {
		t.Fatalf("CreateVsc must receive ClientToken: %#v", req)
	}
	if fake.calls[1].request["VscId"] != "vsc-123" {
		t.Fatalf("DescribeVsc readback request = %#v", fake.calls[1].request)
	}
	vsc, _ := decodeObject(t, stdout)["vsc"].(map[string]any)
	if vsc == nil || vsc["id"] != "vsc-123" || vsc["name"] != "serial-a" || vsc["node"] != "node-123" || vsc["type"] != "primary" || vsc["status"] != "Normal" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunVSCDeleteMapsRequest(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := lingjunVSCCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vsc", "delete", "vsc-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("lingjun vsc delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteVsc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["VscId"] != "vsc-123" {
		t.Fatalf("DeleteVsc request = %#v", req)
	}
	if _, ok := req["ClientToken"]; !ok {
		t.Fatalf("DeleteVsc must receive ClientToken: %#v", req)
	}
}

func TestLingjunVSCGetExtractsDescribeVsc(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLingjunVSCResponse("vsc-123", "serial-a", "node-123", "standard", "Normal"),
	}}
	runCLI := lingjunVSCCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vsc", "get", "vsc-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("lingjun vsc get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeVsc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["VscId"] != "vsc-123" {
		t.Fatalf("DescribeVsc request = %#v", fake.calls[0].request)
	}
	vsc, _ := decodeObject(t, stdout)["vsc"].(map[string]any)
	if vsc == nil || vsc["id"] != "vsc-123" || vsc["name"] != "serial-a" || vsc["node"] != "node-123" || vsc["type"] != "standard" || vsc["resource_group"] != "rg-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunVSCListFiltersNodeAndPaginatesWithToken(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"NextToken":  "next-2",
		"TotalCount": 1,
		"Vscs": []any{
			map[string]any{
				"VscId":           "vsc-123",
				"VscName":         "serial-a",
				"NodeId":          "node-123",
				"VscType":         "primary",
				"Status":          "Normal",
				"ResourceGroupId": "rg-123",
				"Tags": []any{
					map[string]any{"TagKey": "env", "TagValue": "prod"},
				},
			},
		},
	}}}
	runCLI := lingjunVSCCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vsc", "list",
		"--region", "cn-beijing",
		"--filter", "node=node-456",
		"--limit", "50",
		"--next-token", "next-1",
		"--filter", "name=serial-a",
		"--filter", "resource-group=rg-123",
		"--filter", "tag.env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun vsc list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListVscs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if !reflect.DeepEqual(req["NodeIds"], []string{"node-456"}) || req["MaxResults"] != 50 || req["NextToken"] != "next-1" || req["VscName"] != "serial-a" || req["ResourceGroupId"] != "rg-123" || !reflect.DeepEqual(req["Tag"], []string{"env=prod"}) {
		t.Fatalf("ListVscs request = %#v", req)
	}
	out := decodeObject(t, stdout)
	vscs, _ := out["vscs"].([]any)
	pagination, _ := out["pagination"].(map[string]any)
	if out["total"] != float64(1) || len(vscs) != 1 || pagination["next_token"] != "next-2" {
		t.Fatalf("unexpected list output: %s", stdout)
	}
}
