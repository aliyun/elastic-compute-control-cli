package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func fakeLingjunVccResponse(id, name string) map[string]any {
	return fakeLingjunVccResponseWithStatus(id, name, "Available")
}

func fakeLingjunVccResponseWithStatus(id, name, status string) map[string]any {
	return map[string]any{
		"RequestId": "req-get-vcc",
		"Content": map[string]any{
			"VccId":           id,
			"VccName":         name,
			"Status":          status,
			"VpdId":           "vpd-123",
			"VpcId":           "vpc-123",
			"VSwitchId":       "vsw-123",
			"CenId":           "cen-123",
			"ResourceGroupId": "rg-123",
		},
	}
}

func fakeLingjunVccListResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-list-vccs",
		"Content": map[string]any{
			"Total": int64(1),
			"Data": []any{
				map[string]any{
					"VccId":           "vcc-123",
					"VccName":         "train-vcc",
					"Status":          "Available",
					"VpdId":           "vpd-123",
					"VpcId":           "vpc-123",
					"ResourceGroupId": "rg-123",
				},
			},
		},
	}
}

func fakeLingjunVccRoutesResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-list-routes",
		"Content": map[string]any{
			"Total": int64(1),
			"Data": []any{
				map[string]any{
					"VccRouteEntryId":      "vcc-rte-123",
					"VccId":                "vcc-123",
					"DestinationCidrBlock": "10.0.0.0/24",
					"Status":               "Available",
				},
			},
		},
	}
}

func fakeLingjunVccRouteEntriesResponse(entries []any) map[string]any {
	return map[string]any{
		"RequestId": "req-list-routes",
		"Content": map[string]any{
			"Total": int64(len(entries)),
			"Data":  entries,
		},
	}
}

func fakeLingjunVccGrantRulesResponse(entries []any) map[string]any {
	return map[string]any{
		"RequestId": "req-list-grants",
		"Content": map[string]any{
			"Total": int64(len(entries)),
			"Data":  entries,
		},
	}
}

func fakeLingjunVccGrantsResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-list-grants",
		"Content": map[string]any{
			"Total": int64(1),
			"Data": []any{
				map[string]any{
					"GrantRuleId":   "grant-rule-123",
					"ErId":          "er-123",
					"InstanceId":    "vcc-123",
					"GrantTenantId": "1234567890",
					"Used":          true,
				},
			},
		},
	}
}

func fakeLingjunVccFlowsResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-list-flows",
		"Content": map[string]any{
			"Total": int64(1),
			"Data": []any{
				map[string]any{
					"VccId":      "vcc-123",
					"MetricName": "InternetOut",
					"Direction":  "out",
					"Timestamp":  int64(1710000000),
					"Value":      12.5,
				},
			},
		},
	}
}

func TestLingjunVccSchemaRegistration(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("schema", "lingjun.vcc.create")
	if code != 0 {
		t.Fatalf("schema lingjun.vcc.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{`"vpd"`, `"bandwidth"`, `"resource-group"`, `"tag"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("schema missing %s: %s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("lingjun", "vcc", "get", "--help")
	if code != 0 {
		t.Fatalf("lingjun vcc get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--with-routes") || !strings.Contains(stdout, "--with-grants") || !strings.Contains(stdout, "--with-flows") {
		t.Fatalf("vcc get help missing extra probes: %s", stdout)
	}
}

func TestLingjunVccCreateMapsCreateVccAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "Content": map[string]any{"VccId": "vcc-123"}},
		fakeLingjunVccResponse("vcc-123", "train-vcc"),
	}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--name", "train-vcc",
		"--bandwidth", "1000",
		"--connection-type", "VPC",
		"--vpc", "vpc-123",
		"--vswitch", "vsw-123",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateVcc" || fake.calls[1].operation != "GetVcc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["VpdId"] != "vpd-123" || req["VccName"] != "train-vcc" || req["Bandwidth"] != 1000 || req["VpcId"] != "vpc-123" || req["VSwitchId"] != "vsw-123" || req["ResourceGroupId"] != "rg-123" {
		t.Fatalf("CreateVcc request = %#v", req)
	}
	if req["Tag.1.Key"] != "env" || req["Tag.1.Value"] != "prod" {
		t.Fatalf("CreateVcc tag mapping wrong: %#v", req)
	}
	if fake.calls[1].request["VccId"] != "vcc-123" {
		t.Fatalf("GetVcc readback request = %#v", fake.calls[1].request)
	}
	vcc, _ := decodeObject(t, stdout)["vcc"].(map[string]any)
	if vcc == nil || vcc["id"] != "vcc-123" || vcc["name"] != "train-vcc" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestLingjunVccCreateWaitsUntilAvailable(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "Content": map[string]any{"VccId": "vcc-123"}},
		fakeLingjunVccResponseWithStatus("vcc-123", "train-vcc", "Executing"),
		fakeLingjunVccResponseWithStatus("vcc-123", "train-vcc", "Available"),
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "vcc" {
			t.Fatalf("resource = %s/%s, want lingjun/vcc", resource.Product, resource.Resource)
		}
		waiter := resource.Waiters["available_after_change"]
		waiter.Interval = "1ms"
		waiter.Timeout = "50ms"
		resource.Waiters["available_after_change"] = waiter
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--name", "train-vcc",
		"--bandwidth", "1000",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	operations := []string{}
	for _, call := range fake.calls {
		operations = append(operations, call.operation)
	}
	wantOps := []string{"CreateVcc", "GetVcc", "GetVcc"}
	if strings.Join(operations, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("operations = %#v", operations)
	}
	vcc, _ := decodeObject(t, stdout)["vcc"].(map[string]any)
	if vcc == nil || vcc["status"] != "Available" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestLingjunVccCreateNoWaitSkipsReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "Content": map[string]any{"VccId": "vcc-123"}},
	}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--name", "train-vcc",
		"--bandwidth", "1000",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateVcc" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	vcc, _ := decodeObject(t, stdout)["vcc"].(map[string]any)
	if vcc == nil || vcc["id"] != "vcc-123" || vcc["name"] != "train-vcc" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestLingjunVccUpdateRoutesRouteAndGrantApis(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-update", "Content": map[string]any{"VccId": "vcc-123"}},
		{"RequestId": "req-add-route", "Content": map[string]any{"VccRouteEntryId": "vcc-rte-123"}},
		{"RequestId": "req-remove-route"},
		{"RequestId": "req-add-grant", "Content": map[string]any{"GrantRuleId": "grant-rule-123"}},
		{"RequestId": "req-remove-grant"},
		fakeLingjunVccRouteEntriesResponse([]any{
			map[string]any{
				"VccRouteEntryId":      "vcc-rte-123",
				"VccId":                "vcc-123",
				"DestinationCidrBlock": "10.0.0.0/24",
			},
		}),
		fakeLingjunVccRouteEntriesResponse([]any{
			map[string]any{
				"VccRouteEntryId":      "vcc-rte-123",
				"VccId":                "vcc-123",
				"DestinationCidrBlock": "10.0.0.0/24",
			},
		}),
		fakeLingjunVccGrantRulesResponse([]any{
			map[string]any{
				"GrantRuleId":   "grant-rule-123",
				"ErId":          "er-123",
				"GrantTenantId": "1234567890",
				"InstanceId":    "vcc-123",
			},
		}),
		fakeLingjunVccGrantRulesResponse([]any{
			map[string]any{
				"GrantRuleId":   "grant-rule-123",
				"ErId":          "er-123",
				"GrantTenantId": "1234567890",
				"InstanceId":    "vcc-123",
			},
		}),
		fakeLingjunVccResponse("vcc-123", "train-vcc-2"),
	}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "update", "vcc-123",
		"--region", "cn-wulanchabu",
		"--name", "train-vcc-2",
		"--bandwidth", "2000",
		"--route", "+destination-cidr=10.0.0.0/24",
		"--route", "-id=vcc-rte-old,destination-cidr=10.0.1.0/24",
		"--grant", "+er=er-123,tenant=1234567890,instance=vcc-123",
		"--grant", "-id=grant-rule-old,er=er-123,instance=vcc-123",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	operations := []string{}
	for _, call := range fake.calls {
		operations = append(operations, call.operation)
	}
	wantOps := []string{
		"UpdateVcc",
		"CreateVccRouteEntry",
		"DeleteVccRouteEntry",
		"CreateVccGrantRule",
		"DeleteVccGrantRule",
		"ListVccRouteEntries",
		"ListVccRouteEntries",
		"ListVccGrantRules",
		"ListVccGrantRules",
		"GetVcc",
	}
	if strings.Join(operations, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("operations = %#v", operations)
	}
	if fake.calls[1].request["DestinationCidrBlock"] != "10.0.0.0/24" || fake.calls[2].request["VccRouteEntryId"] != "vcc-rte-old" {
		t.Fatalf("route requests = %#v %#v", fake.calls[1].request, fake.calls[2].request)
	}
	if fake.calls[3].request["ErId"] != "er-123" || fake.calls[3].request["GrantTenantId"] != "1234567890" || fake.calls[4].request["GrantRuleId"] != "grant-rule-old" {
		t.Fatalf("grant requests = %#v %#v", fake.calls[3].request, fake.calls[4].request)
	}
	if fake.calls[5].request["VccId"] != "vcc-123" || fake.calls[7].request["InstanceId"] != "vcc-123" {
		t.Fatalf("wait requests = %#v %#v", fake.calls[5].request, fake.calls[7].request)
	}
}

func TestLingjunVccUpdateWaitsForReturnedRouteID(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-add-route", "Content": map[string]any{"VccRouteEntryId": "vcc-rte-new"}},
		fakeLingjunVccRouteEntriesResponse([]any{
			map[string]any{
				"VccRouteEntryId":      "vcc-rte-old",
				"VccId":                "vcc-123",
				"DestinationCidrBlock": "10.0.0.0/24",
			},
		}),
		fakeLingjunVccRouteEntriesResponse([]any{
			map[string]any{
				"VccRouteEntryId":      "vcc-rte-new",
				"VccId":                "vcc-123",
				"DestinationCidrBlock": "10.0.0.0/24",
			},
		}),
		fakeLingjunVccResponse("vcc-123", "train-vcc"),
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "vcc" {
			t.Fatalf("resource = %s/%s, want lingjun/vcc", resource.Product, resource.Resource)
		}
		for _, name := range []string{"routes_visible", "available_after_change"} {
			waiter := resource.Waiters[name]
			waiter.Interval = "1ms"
			waiter.Timeout = "50ms"
			resource.Waiters[name] = waiter
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "update", "vcc-123",
		"--region", "cn-wulanchabu",
		"--route", "+destination-cidr=10.0.0.0/24",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc update --add-route exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	operations := []string{}
	for _, call := range fake.calls {
		operations = append(operations, call.operation)
	}
	wantOps := []string{"CreateVccRouteEntry", "ListVccRouteEntries", "ListVccRouteEntries", "GetVcc"}
	if strings.Join(operations, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestLingjunVccUpdateNoWaitSkipsWaitProbes(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-update", "Content": map[string]any{"VccId": "vcc-123"}},
		{"RequestId": "req-add-route", "Content": map[string]any{"VccRouteEntryId": "vcc-rte-123"}},
		{"RequestId": "req-add-grant", "Content": map[string]any{"GrantRuleId": "grant-rule-123"}},
	}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "update", "vcc-123",
		"--region", "cn-wulanchabu",
		"--name", "train-vcc-2",
		"--route", "+destination-cidr=10.0.0.0/24",
		"--grant", "+er=er-123,tenant=1234567890,instance=vcc-123",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc update --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	operations := []string{}
	for _, call := range fake.calls {
		operations = append(operations, call.operation)
	}
	wantOps := []string{"UpdateVcc", "CreateVccRouteEntry", "CreateVccGrantRule"}
	if strings.Join(operations, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("operations = %#v", operations)
	}
	vcc, _ := decodeObject(t, stdout)["vcc"].(map[string]any)
	if vcc == nil || vcc["id"] != "vcc-123" || vcc["name"] != "train-vcc-2" {
		t.Fatalf("unexpected update output: %s", stdout)
	}
}

func TestLingjunVccUpdateHelpUsesUnifiedFlags(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "lingjun", "vcc", "update", "--help")
	if code != 0 {
		t.Fatalf("lingjun vcc update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--route") || !strings.Contains(stdout, "--grant") {
		t.Fatalf("update help missing --route or --grant: %s", stdout)
	}
	for _, forbidden := range []string{"--add-route", "--remove-route", "--add-grant", "--remove-grant"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("update help should not expose %s: %s", forbidden, stdout)
		}
	}
}

func TestLingjunVccGetWithExtras(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLingjunVccResponse("vcc-123", "train-vcc"),
		fakeLingjunVccRoutesResponse(),
		fakeLingjunVccGrantsResponse(),
		fakeLingjunVccFlowsResponse(),
	}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI("lingjun", "vcc", "get", "vcc-123", "--region", "cn-wulanchabu", "--with-routes", "--with-grants", "--with-flows")
	if code != 0 {
		t.Fatalf("lingjun vcc get with extras exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	operations := []string{}
	for _, call := range fake.calls {
		operations = append(operations, call.operation)
	}
	wantOps := []string{"GetVcc", "ListVccRouteEntries", "ListVccGrantRules", "ListVccFlowInfos"}
	if strings.Join(operations, ",") != strings.Join(wantOps, ",") {
		t.Fatalf("operations = %#v", operations)
	}
	vcc, _ := decodeObject(t, stdout)["vcc"].(map[string]any)
	if vcc == nil || len(vcc["routes"].([]any)) != 1 || len(vcc["grants"].([]any)) != 1 || len(vcc["flows"].([]any)) != 1 {
		t.Fatalf("unexpected get output: %s", stdout)
	}
}

func TestLingjunVccDeleteMapsRefundVcc(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI("lingjun", "vcc", "delete", "vcc-123", "--region", "cn-wulanchabu")
	if code != 0 {
		t.Fatalf("lingjun vcc delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "RefundVcc" || fake.calls[1].operation != "ListVccs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["VccId"] != "vcc-123" {
		t.Fatalf("RefundVcc request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	if out["deleted"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunVccListPaginationAndFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeLingjunVccListResponse()}}
	runCLI := catalogCaller(t, "lingjun", "vcc", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vcc", "list",
		"--region", "cn-wulanchabu",
		"--page", "2",
		"--limit", "50",
		"--filter", "id=vcc-123",
		"--filter", "vpd=vpd-123",
		"--filter", "status=Available",
		"--filter", "resource-group=rg-123",
		"--filter", "tag.env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun vcc list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListVccs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PageNumber"] != 2 || req["PageSize"] != 50 || req["VccId"] != "vcc-123" || req["VpdId"] != "vpd-123" || req["Status"] != "Available" || req["ResourceGroupId"] != "rg-123" {
		t.Fatalf("ListVccs request = %#v", req)
	}
	if req["Tag.1.Key"] != "env" || req["Tag.1.Value"] != "prod" {
		t.Fatalf("ListVccs tag filter mapping wrong: %#v", req)
	}
	vccs, _ := decodeObject(t, stdout)["vccs"].([]any)
	if len(vccs) != 1 {
		t.Fatalf("vccs = %#v; stdout=%s", vccs, stdout)
	}
}
