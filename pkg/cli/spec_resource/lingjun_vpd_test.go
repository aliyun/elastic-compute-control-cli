package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestLingjunVPDCreateMapsRequestAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"VpdId": "vpd-123",
				},
			},
			fakeLingjunVPDResponse("vpd-123", "train-vpd", "Available"),
		},
	}
	runCLI := lingjunVPDCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vpd", "create",
		"--region", "cn-wulanchabu",
		"--name", "train-vpd",
		"--cidr", "10.0.0.0/16",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
		"--subnet", "cidr=10.0.1.0/24,region=cn-wulanchabu,zone=cn-wulanchabu-b,name=train-subnet,type=OOB",
	)
	if code != 0 {
		t.Fatalf("lingjun vpd create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateVpd" || fake.calls[1].operation != "GetVpd" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["RegionId"] != "cn-wulanchabu" ||
		request["VpdName"] != "train-vpd" ||
		request["Cidr"] != "10.0.0.0/16" ||
		request["ResourceGroupId"] != "rg-123" ||
		request["Subnets.1.Cidr"] != "10.0.1.0/24" ||
		request["Subnets.1.RegionId"] != "cn-wulanchabu" ||
		request["Subnets.1.ZoneId"] != "cn-wulanchabu-b" ||
		request["Subnets.1.SubnetName"] != "train-subnet" ||
		request["Subnets.1.Type"] != "OOB" {
		t.Fatalf("create request = %#v", request)
	}
	requireStringValues(t, request["Tag"], []string{"env=prod"})
	if fake.calls[1].request["VpdId"] != "vpd-123" {
		t.Fatalf("GetVpd request = %#v", fake.calls[1].request)
	}
	vpd, _ := decodeObject(t, stdout)["vpd"].(map[string]any)
	if vpd == nil || vpd["id"] != "vpd-123" || vpd["name"] != "train-vpd" || vpd["status"] != "Available" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunVPDUpdateAddRemoveCidrReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-add"},
			{"RequestId": "req-remove"},
			fakeLingjunVPDResponse("vpd-123", "train-vpd", "Available"),
		},
	}
	runCLI := lingjunVPDCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vpd", "update", "vpd-123",
		"--region", "cn-wulanchabu",
		"--cidr", "+172.16.0.0/16",
		"--cidr", "-192.168.0.0/16",
	)
	if code != 0 {
		t.Fatalf("lingjun vpd update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "AssociateVpdCidrBlock" ||
		fake.calls[1].operation != "UnAssociateVpdCidrBlock" ||
		fake.calls[2].operation != "GetVpd" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["VpdId"] != "vpd-123" || fake.calls[0].request["SecondaryCidrBlock"] != "172.16.0.0/16" {
		t.Fatalf("associate request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["VpdId"] != "vpd-123" || fake.calls[1].request["SecondaryCidrBlock"] != "192.168.0.0/16" {
		t.Fatalf("unassociate request = %#v", fake.calls[1].request)
	}
	vpd, _ := decodeObject(t, stdout)["vpd"].(map[string]any)
	if vpd == nil || vpd["id"] != "vpd-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunVPDUpdateWaitsForRequestedFieldsAndCidrs(t *testing.T) {
	t.Parallel()
	stale := fakeLingjunVPDResponse("vpd-123", "old-name", "Available")
	stale["Content"].(map[string]any)["SecondaryCidrBlocks"] = []any{"192.168.0.0/16"}
	converged := fakeLingjunVPDResponse("vpd-123", "new-name", "Available")
	converged["Content"].(map[string]any)["SecondaryCidrBlocks"] = []any{"172.16.0.0/16"}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-update"},
		{"RequestId": "req-add"},
		{"RequestId": "req-remove"},
		stale,
		converged,
	}}
	runCLI := lingjunVPDCaller(t, fake)

	stdout, stderr, code := runCLI(
		"lingjun", "vpd", "update", "vpd-123",
		"--region", "cn-wulanchabu",
		"--name", "new-name",
		"--cidr", "+172.16.0.0/16",
		"--cidr", "-192.168.0.0/16",
		"--timeout", "1s",
	)
	if code != 0 {
		t.Fatalf("lingjun vpd update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 5 || fake.calls[3].operation != "GetVpd" || fake.calls[4].operation != "GetVpd" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	vpd, _ := decodeObject(t, stdout)["vpd"].(map[string]any)
	if vpd == nil || vpd["name"] != "new-name" {
		t.Fatalf("unexpected converged output: %s", stdout)
	}
}

func TestLingjunVPDUpdateHelpUsesUnifiedCidrFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "lingjun", "vpd", "update", "--help")
	if code != 0 {
		t.Fatalf("lingjun vpd update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--cidr") {
		t.Fatalf("update help missing --cidr: %s", stdout)
	}
	for _, forbidden := range []string{"--add-cidr", "--remove-cidr"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("update help should not expose %s: %s", forbidden, stdout)
		}
	}
}

func TestLingjunVPDGetWithRoutesAndGrantsMergesOutput(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeLingjunVPDResponse("vpd-123", "train-vpd", "Available"),
			{
				"RequestId": "req-routes",
				"Content": map[string]any{
					"Total": float64(1),
					"Data": []any{
						map[string]any{
							"VpdRouteEntryId":      "rte-123",
							"DestinationCidrBlock": "0.0.0.0/0",
							"NextHopId":            "er-123",
							"NextHopType":          "ER",
							"RouteType":            "BGP",
							"Status":               "Available",
						},
					},
				},
			},
			{
				"RequestId": "req-grants",
				"Content": map[string]any{
					"Total": float64(1),
					"Data": []any{
						map[string]any{
							"GrantRuleId":   "grant-123",
							"InstanceId":    "vpd-123",
							"InstanceName":  "train-vpd",
							"ErId":          "er-123",
							"GrantTenantId": "tenant-2",
							"Product":       "VPD",
							"Used":          true,
						},
					},
				},
			},
		},
	}
	runCLI := lingjunVPDCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vpd", "get", "vpd-123", "--region", "cn-wulanchabu", "--with-routes", "--with-grants")
	if code != 0 {
		t.Fatalf("lingjun vpd get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "GetVpd" ||
		fake.calls[1].operation != "ListVpdRouteEntries" ||
		fake.calls[2].operation != "ListVpdGrantRules" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["VpdId"] != "vpd-123" || fake.calls[2].request["InstanceId"] != "vpd-123" {
		t.Fatalf("extra requests = %#v %#v", fake.calls[1].request, fake.calls[2].request)
	}
	vpd, _ := decodeObject(t, stdout)["vpd"].(map[string]any)
	routes, _ := vpd["routes"].([]any)
	grants, _ := vpd["grants"].([]any)
	if len(routes) != 1 || len(grants) != 1 {
		t.Fatalf("routes/grants missing: %s", stdout)
	}
	route, _ := routes[0].(map[string]any)
	grant, _ := grants[0].(map[string]any)
	if route["id"] != "rte-123" || route["destination_cidr"] != "0.0.0.0/0" {
		t.Fatalf("route = %#v; stdout=%s", route, stdout)
	}
	if grant["id"] != "grant-123" || grant["grant_tenant_id"] != "tenant-2" || grant["used"] != true {
		t.Fatalf("grant = %#v; stdout=%s", grant, stdout)
	}
}

func TestLingjunVPDListMapsPaginationAndFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-list",
				"Content": map[string]any{
					"Total": float64(101),
					"Data": []any{
						map[string]any{
							"VpdId":           "vpd-123",
							"VpdName":         "train-vpd",
							"Cidr":            "10.0.0.0/16",
							"Status":          "Available",
							"ResourceGroupId": "rg-123",
							"RegionId":        "cn-wulanchabu",
						},
					},
				},
			},
		},
	}
	runCLI := lingjunVPDCaller(t, fake)

	stdout, stderr, code := runCLI("lingjun", "vpd", "list",
		"--region", "cn-wulanchabu",
		"--page", "2",
		"--filter", "id=vpd-123",
		"--filter", "name=train-vpd",
		"--filter", "status=Available",
		"--filter", "resource-group=rg-123",
		"--filter", "tag.env=prod",
		"--filter", "er=er-123",
	)
	if code != 0 {
		t.Fatalf("lingjun vpd list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListVpds" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["RegionId"] != "cn-wulanchabu" ||
		request["PageNumber"] != 2 ||
		request["PageSize"] != 100 ||
		request["VpdId"] != "vpd-123" ||
		request["VpdName"] != "train-vpd" ||
		request["Status"] != "Available" ||
		request["ResourceGroupId"] != "rg-123" ||
		request["FilterErId"] != "er-123" {
		t.Fatalf("list request = %#v", request)
	}
	requireStringValues(t, request["Tag"], []string{"env=prod"})
	out := decodeObject(t, stdout)
	if out["total"] != float64(101) {
		t.Fatalf("unexpected total: %s", stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["page"] != float64(2) || pagination["limit"] != float64(100) || pagination["has_more"] != false {
		t.Fatalf("pagination = %#v; stdout=%s", pagination, stdout)
	}
}

func lingjunVPDCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "vpd" || resource.APIProduct != "eflo" {
			t.Fatalf("resource = %#v, want lingjun/vpd with eflo API product", resource)
		}
		if region != "cn-wulanchabu" {
			t.Fatalf("region = %q, want cn-wulanchabu", region)
		}
		return fake, nil
	})
}

func fakeLingjunVPDResponse(id string, name string, status string) map[string]any {
	return map[string]any{
		"RequestId": "req-get",
		"Content": map[string]any{
			"VpdId":               id,
			"VpdName":             name,
			"Cidr":                "10.0.0.0/16",
			"Status":              status,
			"RegionId":            "cn-wulanchabu",
			"ResourceGroupId":     "rg-123",
			"AttachErStatus":      true,
			"SecondaryCidrBlocks": []any{"172.16.0.0/16"},
			"Tags": []any{
				map[string]any{"TagKey": "env", "TagValue": "prod"},
			},
		},
	}
}
