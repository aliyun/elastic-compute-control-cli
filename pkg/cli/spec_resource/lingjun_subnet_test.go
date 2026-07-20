package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestLingjunSubnetCreateMapsRequestAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"SubnetId": "subnet-123",
				},
			},
			fakeLingjunSubnetResponse("subnet-123", "train-subnet"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" || resource.APIProduct != "eflo" {
			t.Fatalf("resource = %#v, want lingjun/subnet with eflo API product", resource)
		}
		if region != "cn-wulanchabu" {
			t.Fatalf("region = %q, want cn-wulanchabu", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--cidr", "10.0.1.0/24",
		"--name", "train-subnet",
		"--type", "OOB",
		"--tag", "env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateSubnet" || fake.calls[1].operation != "GetSubnet" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["VpdId"] != "vpd-123" || createReq["ZoneId"] != "cn-wulanchabu-b" || createReq["Cidr"] != "10.0.1.0/24" || createReq["SubnetName"] != "train-subnet" || createReq["Type"] != "OOB" {
		t.Fatalf("CreateSubnet request = %#v", createReq)
	}
	requireStringValues(t, createReq["Tag"], []string{"env=prod"})
	getReq := fake.calls[1].request
	if getReq["SubnetId"] != "subnet-123" || getReq["VpdId"] != "vpd-123" {
		t.Fatalf("GetSubnet readback request = %#v", getReq)
	}
	subnet, _ := decodeObject(t, stdout)["subnet"].(map[string]any)
	if subnet == nil || subnet["id"] != "subnet-123" || subnet["name"] != "train-subnet" || subnet["vpd"] != "vpd-123" || subnet["available_ips"] != float64(1024) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunSubnetCreateWaitsUntilAvailable(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"SubnetId": "subnet-123",
				},
			},
			fakeLingjunSubnetResponseWithStatus("subnet-123", "train-subnet", "Executing"),
			fakeLingjunSubnetResponse("subnet-123", "train-subnet"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		waiter := resource.Waiters["available_after_change"]
		waiter.Interval = "1ms"
		waiter.Timeout = "50ms"
		resource.Waiters["available_after_change"] = waiter
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--cidr", "10.0.1.0/24",
		"--name", "train-subnet",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "CreateSubnet" || fake.calls[1].operation != "GetSubnet" || fake.calls[2].operation != "GetSubnet" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	subnet, _ := decodeObject(t, stdout)["subnet"].(map[string]any)
	if subnet == nil || subnet["status"] != "Available" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunSubnetCreateNoWaitSkipsReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"SubnetId": "subnet-123",
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "create",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--cidr", "10.0.1.0/24",
		"--name", "train-subnet",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateSubnet" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	subnet, _ := decodeObject(t, stdout)["subnet"].(map[string]any)
	if subnet == nil || subnet["id"] != "subnet-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunSubnetUpdateMapsRequiredScopeAndName(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-update",
				"Content": map[string]any{
					"SubnetId": "subnet-123",
				},
			},
			fakeLingjunSubnetResponse("subnet-123", "renamed-subnet"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "update", "subnet-123",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--name", "renamed-subnet",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateSubnet" || fake.calls[1].operation != "GetSubnet" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	updateReq := fake.calls[0].request
	if updateReq["SubnetId"] != "subnet-123" || updateReq["VpdId"] != "vpd-123" || updateReq["ZoneId"] != "cn-wulanchabu-b" || updateReq["SubnetName"] != "renamed-subnet" {
		t.Fatalf("UpdateSubnet request = %#v", updateReq)
	}
	if fake.calls[1].request["SubnetId"] != "subnet-123" || fake.calls[1].request["VpdId"] != "vpd-123" {
		t.Fatalf("GetSubnet readback request = %#v", fake.calls[1].request)
	}
}

func TestLingjunSubnetDeleteMapsRequiredScope(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{"RequestId": "req-delete"}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "delete", "subnet-123",
		"--region", "cn-wulanchabu",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DeleteSubnet" || fake.calls[1].operation != "ListSubnets" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	deleteReq := fake.calls[0].request
	if deleteReq["SubnetId"] != "subnet-123" || deleteReq["VpdId"] != "vpd-123" || deleteReq["ZoneId"] != "cn-wulanchabu-b" {
		t.Fatalf("DeleteSubnet request = %#v", deleteReq)
	}
	waitReq := fake.calls[1].request
	if waitReq["SubnetId"] != "subnet-123" || waitReq["VpdId"] != "vpd-123" || waitReq["ZoneId"] != "cn-wulanchabu-b" {
		t.Fatalf("ListSubnets wait request = %#v", waitReq)
	}
	out := decodeObject(t, stdout)
	if out["deleted"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunSubnetGetExtractsContentFields(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{fakeLingjunSubnetResponse("subnet-123", "train-subnet")},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "get", "subnet-123", "--region", "cn-wulanchabu", "--vpd", "vpd-123")
	if code != 0 {
		t.Fatalf("lingjun subnet get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetSubnet" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SubnetId"] != "subnet-123" || fake.calls[0].request["VpdId"] != "vpd-123" {
		t.Fatalf("GetSubnet request = %#v", fake.calls[0].request)
	}
	subnet, _ := decodeObject(t, stdout)["subnet"].(map[string]any)
	if subnet == nil || subnet["id"] != "subnet-123" || subnet["cidr"] != "10.0.1.0/24" || subnet["status"] != "Available" || subnet["resource_group"] != "rg-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunSubnetListMapsFiltersAndPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{{
			"RequestId": "req-list",
			"Content": map[string]any{
				"Total": 101,
				"Data": []any{
					lingjunSubnetContent("subnet-123", "train-subnet"),
				},
			},
		}},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "subnet" {
			t.Fatalf("resource = %s/%s, want lingjun/subnet", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "subnet", "list",
		"--region", "cn-wulanchabu",
		"--filter", "vpd=vpd-123",
		"--filter", "id=subnet-123",
		"--filter", "name=train-subnet",
		"--filter", "status=Available",
		"--filter", "zone=cn-wulanchabu-b",
		"--filter", "type=OOB",
		"--filter", "resource-group=rg-123",
		"--filter", "tag.env=prod",
		"--page", "2",
	)
	if code != 0 {
		t.Fatalf("lingjun subnet list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListSubnets" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["EnablePage"] != true || request["PageNumber"] != 2 || request["PageSize"] != 100 ||
		request["VpdId"] != "vpd-123" || request["SubnetId"] != "subnet-123" || request["SubnetName"] != "train-subnet" ||
		request["Status"] != "Available" || request["ZoneId"] != "cn-wulanchabu-b" || request["Type"] != "OOB" ||
		request["ResourceGroupId"] != "rg-123" {
		t.Fatalf("ListSubnets request = %#v", request)
	}
	requireStringValues(t, request["Tag"], []string{"env=prod"})
	out := decodeObject(t, stdout)
	subnets, _ := out["subnets"].([]any)
	if out["total"] != float64(101) || len(subnets) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["page"] != float64(2) || pagination["limit"] != float64(100) || pagination["has_more"] != false {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
}

func fakeLingjunSubnetResponse(id string, name string) map[string]any {
	return fakeLingjunSubnetResponseWithStatus(id, name, "Available")
}

func fakeLingjunSubnetResponseWithStatus(id string, name string, status string) map[string]any {
	content := lingjunSubnetContent(id, name)
	content["Status"] = status
	return map[string]any{
		"RequestId": "req-get",
		"Content":   content,
	}
}

func lingjunSubnetContent(id string, name string) map[string]any {
	return map[string]any{
		"SubnetId":              id,
		"SubnetName":            name,
		"VpdId":                 "vpd-123",
		"ZoneId":                "cn-wulanchabu-b",
		"Cidr":                  "10.0.1.0/24",
		"Status":                "Available",
		"Type":                  "OOB",
		"AvailableIps":          1024,
		"NetworkInterfaceCount": 2,
		"PrivateIpCount":        3,
		"NcCount":               4,
		"LbCount":               5,
		"ResourceGroupId":       "rg-123",
		"RegionId":              "cn-wulanchabu",
		"TenantId":              "tenant-123",
		"CreateTime":            "1678273219000",
		"GmtModified":           "1678273220000",
		"Message":               "success",
		"Tags": []any{
			map[string]any{"TagKey": "env", "TagValue": "prod"},
		},
		"VpdBaseInfo": map[string]any{
			"VpdId":      "vpd-123",
			"VpdName":    "train-vpd",
			"Cidr":       "10.0.0.0/16",
			"CreateTime": "1678273200000",
		},
	}
}
