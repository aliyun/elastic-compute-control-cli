package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestLingjunENICreateMapsRequestAndReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"ElasticNetworkInterfaceId": "leni-123",
					"NodeId":                    "e01-cn-test",
				},
			},
			fakeLingjunENIGetResponse("leni-123", "Available"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "eni" || resource.APIProduct != "eflo" {
			t.Fatalf("resource = %s/%s api=%s, want lingjun/eni api=eflo", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-wulanchabu" {
			t.Fatalf("region = %q, want cn-wulanchabu", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "eni", "create",
		"--region", "cn-wulanchabu",
		"--subnet", "subnet-123",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--security-group", "sg-123",
		"--description", "training eni",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
	)
	if code != 0 {
		t.Fatalf("lingjun eni create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateElasticNetworkInterface" || fake.calls[1].operation != "GetElasticNetworkInterface" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	want := map[string]any{
		"RegionId":        "cn-wulanchabu",
		"VSwitchId":       "subnet-123",
		"VpcId":           "vpd-123",
		"ZoneId":          "cn-wulanchabu-b",
		"SecurityGroupId": "sg-123",
		"Description":     "training eni",
		"ResourceGroupId": "rg-123",
		"Tag.1.Key":       "env",
		"Tag.1.Value":     "prod",
	}
	for key, value := range want {
		if createReq[key] != value {
			t.Fatalf("CreateElasticNetworkInterface request[%s] = %#v, want %#v; request=%#v", key, createReq[key], value, createReq)
		}
	}
	if _, ok := createReq["ClientToken"]; !ok {
		t.Fatalf("CreateElasticNetworkInterface must receive ClientToken: %#v", createReq)
	}
	if fake.calls[1].request["ElasticNetworkInterfaceId"] != "leni-123" {
		t.Fatalf("GetElasticNetworkInterface request = %#v", fake.calls[1].request)
	}
	eni, _ := decodeObject(t, stdout)["eni"].(map[string]any)
	if eni == nil || eni["id"] != "leni-123" || eni["status"] != "Available" || eni["subnet"] != "subnet-123" || eni["vpd"] != "vpd-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunENICreateNoWaitDoesNotEmitSyntheticStatus(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"Content": map[string]any{
					"ElasticNetworkInterfaceId": "leni-123",
				},
			},
		},
	}
	runCLI := catalogCaller(t, "lingjun", "eni", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "eni", "create",
		"--region", "cn-wulanchabu",
		"--subnet", "subnet-123",
		"--vpd", "vpd-123",
		"--zone", "cn-wulanchabu-b",
		"--security-group", "sg-123",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("lingjun eni create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateElasticNetworkInterface" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	eni, _ := decodeObject(t, stdout)["eni"].(map[string]any)
	if eni == nil || eni["id"] != "leni-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if _, ok := eni["status"]; ok {
		t.Fatalf("no-wait create should not emit synthetic status: %s", stdout)
	}
}

func TestLingjunENIUpdateAddRemoveIPCallsSelectedAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			{"RequestId": "req-assign"},
			{"RequestId": "req-unassign"},
			fakeLingjunENIGetResponse("leni-123", "Available"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want lingjun/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "eni", "update", "leni-123",
		"--region", "cn-wulanchabu",
		"--description", "updated",
		"--security-group", "sg-456",
		"--ip", "+10.0.0.10",
		"--ip", "-sip123",
	)
	if code != 0 {
		t.Fatalf("lingjun eni update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "UpdateElasticNetworkInterface" ||
		fake.calls[1].operation != "AssignLeniPrivateIpAddress" ||
		fake.calls[2].operation != "UnassignLeniPrivateIpAddress" ||
		fake.calls[3].operation != "GetElasticNetworkInterface" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	updateReq := fake.calls[0].request
	if updateReq["ElasticNetworkInterfaceId"] != "leni-123" || updateReq["Description"] != "updated" || updateReq["SecurityGroupId"] != "sg-456" {
		t.Fatalf("UpdateElasticNetworkInterface request = %#v", updateReq)
	}
	assignReq := fake.calls[1].request
	if assignReq["ElasticNetworkInterfaceId"] != "leni-123" || assignReq["PrivateIpAddress"] != "10.0.0.10" {
		t.Fatalf("AssignLeniPrivateIpAddress request = %#v", assignReq)
	}
	if _, ok := assignReq["Description"]; ok {
		t.Fatalf("AssignLeniPrivateIpAddress should not reuse ENI description: %#v", assignReq)
	}
	unassignReq := fake.calls[2].request
	if unassignReq["ElasticNetworkInterfaceId"] != "leni-123" || unassignReq["IpName"] != "sip123" {
		t.Fatalf("UnassignLeniPrivateIpAddress request = %#v", unassignReq)
	}
	eni, _ := decodeObject(t, stdout)["eni"].(map[string]any)
	if eni == nil || eni["id"] != "leni-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunENIUpdateRequiresMutation(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("empty ENI update should fail before creating a caller")
		return nil, nil
	})

	stdout, stderr, code := runCLI("lingjun", "eni", "update", "leni-123", "--region", "cn-wulanchabu")
	if code == 0 {
		t.Fatalf("lingjun eni update without mutation succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
}

func TestLingjunENIGetWithIPs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeLingjunENIGetResponse("leni-123", "Available"),
			{
				"RequestId": "req-ips",
				"Content": map[string]any{
					"Total": float64(1),
					"Data": []any{
						map[string]any{
							"IpName":                    "sip123",
							"PrivateIpAddress":          "10.0.0.10",
							"ElasticNetworkInterfaceId": "leni-123",
							"Status":                    "Available",
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want lingjun/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("lingjun", "eni", "get", "leni-123", "--region", "cn-wulanchabu", "--with-ips")
	if code != 0 {
		t.Fatalf("lingjun eni get --with-ips exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "GetElasticNetworkInterface" || fake.calls[1].operation != "ListLeniPrivateIpAddresses" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["ElasticNetworkInterfaceId"] != "leni-123" {
		t.Fatalf("ListLeniPrivateIpAddresses request = %#v", fake.calls[1].request)
	}
	out := decodeObject(t, stdout)
	eni, _ := out["eni"].(map[string]any)
	ips, _ := out["private_ips"].([]any)
	if eni == nil || eni["id"] != "leni-123" || len(ips) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunENIListFiltersAndPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-list",
				"Content": map[string]any{
					"Total": float64(1),
					"Data": []any{
						map[string]any{
							"ElasticNetworkInterfaceId": "leni-123",
							"VpcId":                     "vpd-123",
							"VSwitchId":                 "subnet-123",
							"Status":                    "Available",
						},
					},
				},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "lingjun" || resource.Resource != "eni" {
			t.Fatalf("resource = %s/%s, want lingjun/eni", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"lingjun", "eni", "list",
		"--region", "cn-wulanchabu",
		"--filter", "id=leni-123",
		"--filter", "vpd=vpd-123",
		"--filter", "subnet=subnet-123",
		"--filter", "status=Available",
		"--filter", "resource-group=rg-123",
		"--filter", "tag.env=prod",
		"--limit", "5",
		"--page", "2",
	)
	if code != 0 {
		t.Fatalf("lingjun eni list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListElasticNetworkInterfaces" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"ElasticNetworkInterfaceId": "leni-123",
		"VpcId":                     "vpd-123",
		"VSwitchId":                 "subnet-123",
		"Status":                    "Available",
		"ResourceGroupId":           "rg-123",
		"Tag.1.Key":                 "env",
		"Tag.1.Value":               "prod",
		"PageSize":                  5,
		"PageNumber":                2,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("ListElasticNetworkInterfaces request[%s] = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
	out := decodeObject(t, stdout)
	enis, _ := out["enis"].([]any)
	if out["total"] != float64(1) || len(enis) != 1 {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestLingjunENIAttachDetachRequestMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		action    string
		operation string
	}{
		{name: "attach", action: "attach", operation: "AttachElasticNetworkInterface"},
		{name: "detach", action: "detach", operation: "DetachElasticNetworkInterface"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-" + tt.name}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "lingjun" || resource.Resource != "eni" {
					t.Fatalf("resource = %s/%s, want lingjun/eni", resource.Product, resource.Resource)
				}
				return fake, nil
			})

			stdout, stderr, code := runCLI("lingjun", "eni", tt.action, "leni-123", "--node", "e01-cn-test", "--region", "cn-wulanchabu", "--no-wait")
			if code != 0 {
				t.Fatalf("lingjun eni %s exit %d stderr=%s stdout=%s", tt.action, code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.operation {
				t.Fatalf("calls = %#v", fake.calls)
			}
			request := fake.calls[0].request
			if request["ElasticNetworkInterfaceId"] != "leni-123" || request["NodeId"] != "e01-cn-test" || request["RegionId"] != "cn-wulanchabu" {
				t.Fatalf("%s request = %#v", tt.operation, request)
			}
		})
	}
}

func fakeLingjunENIGetResponse(id string, status string) map[string]any {
	return map[string]any{
		"RequestId": "req-get",
		"Content": map[string]any{
			"ElasticNetworkInterfaceId": id,
			"VpcId":                     "vpd-123",
			"VSwitchId":                 "subnet-123",
			"ZoneId":                    "cn-wulanchabu-b",
			"NodeId":                    "e01-cn-test",
			"Ip":                        "10.0.0.5",
			"Mac":                       "00:16:3e:00:00:01",
			"SecurityGroupId":           "sg-123",
			"ResourceGroupId":           "rg-123",
			"Status":                    status,
			"Type":                      "CUSTOM",
		},
	}
}
