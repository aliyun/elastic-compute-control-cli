package spec_resource

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func catalogCaller(t *testing.T, product, resource string, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, res spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if res.Product != product || res.Resource != resource {
			t.Fatalf("resource = %s/%s, want %s/%s", res.Product, res.Resource, product, resource)
		}
		return fake, nil
	})
}

func TestECSRegionListInvokesDescribeRegions(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-regions",
		"Regions": map[string]any{"Region": []any{
			map[string]any{"RegionId": "cn-hangzhou", "LocalName": "华东1（杭州）", "RegionEndpoint": "ecs.cn-hangzhou.aliyuncs.com", "Status": "available"},
			map[string]any{"RegionId": "cn-beijing", "LocalName": "华北2（北京）", "RegionEndpoint": "ecs.cn-beijing.aliyuncs.com", "Status": "available"},
		}},
	}}}
	runCLI := catalogCaller(t, "ecs", "region", fake)

	stdout, stderr, code := runCLI("ecs", "region", "list", "--region", "cn-hangzhou", "--accept-language", "zh-CN")
	if code != 0 {
		t.Fatalf("ecs region list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeRegions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["AcceptLanguage"] != "zh-CN" {
		t.Fatalf("AcceptLanguage not propagated: %#v", fake.calls[0].request)
	}
	regions, _ := decodeObject(t, stdout)["regions"].([]any)
	if len(regions) != 2 {
		t.Fatalf("regions = %#v; stdout=%s", regions, stdout)
	}
	first, _ := regions[0].(map[string]any)
	if first["id"] != "cn-hangzhou" || first["local_name"] != "华东1（杭州）" || first["status"] != "available" {
		t.Fatalf("unexpected first region: %#v", first)
	}
}

func TestECSRegionListSupportsChargeTypeFilter(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"Regions": map[string]any{"Region": []any{map[string]any{"RegionId": "cn-hangzhou"}}},
	}}}
	runCLI := catalogCaller(t, "ecs", "region", fake)

	stdout, stderr, code := runCLI("ecs", "region", "list", "--region", "cn-hangzhou", "--filter", "charge-type=PrePaid", "--filter", "resource-type=instance")
	if code != 0 {
		t.Fatalf("ecs region list with filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	req := fake.calls[0].request
	if req["InstanceChargeType"] != "PrePaid" || req["ResourceType"] != "instance" {
		t.Fatalf("DescribeRegions filters not propagated: %#v", req)
	}
}

func TestECSZoneListInvokesDescribeZones(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-zones",
		"Zones": map[string]any{"Zone": []any{
			map[string]any{
				"ZoneId":                    "cn-hangzhou-h",
				"LocalName":                 "华东1可用区H",
				"AvailableResourceCreation": map[string]any{"ResourceTypes": []any{"Instance", "Disk"}},
			},
			map[string]any{"ZoneId": "cn-hangzhou-i", "LocalName": "华东1可用区I"},
		}},
	}}}
	runCLI := catalogCaller(t, "ecs", "zone", fake)

	stdout, stderr, code := runCLI("ecs", "zone", "list", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs zone list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeZones" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	// DescribeZones requires RegionId — confirm it is forwarded from the region.
	if fake.calls[0].request["RegionId"] != "cn-hangzhou" {
		t.Fatalf("DescribeZones RegionId = %#v, want cn-hangzhou", fake.calls[0].request["RegionId"])
	}
	zones, _ := decodeObject(t, stdout)["zones"].([]any)
	if len(zones) != 2 {
		t.Fatalf("zones = %#v; stdout=%s", zones, stdout)
	}
	first, _ := zones[0].(map[string]any)
	if first["id"] != "cn-hangzhou-h" || first["local_name"] != "华东1可用区H" {
		t.Fatalf("unexpected first zone: %#v", first)
	}
}

func TestECSZoneListEmitsStableAvailabilityCollections(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-zones",
		"Zones": map[string]any{"Zone": []any{
			map[string]any{
				"ZoneId":                 "cn-shanghai-g",
				"AvailableInstanceTypes": map[string]any{"InstanceTypes": []any{"ecs.g5.large"}},
				"AvailableResources": map[string]any{"ResourcesInfo": []any{map[string]any{
					"InstanceTypes": []any{"ecs.g5.large"},
				}}},
			},
			map[string]any{
				"ZoneId": "cn-shanghai-a",
				"AvailableResources": map[string]any{"ResourcesInfo": []any{map[string]any{
					"DataDiskCategories":   []any{"cloud"},
					"SystemDiskCategories": []any{"cloud"},
				}}},
			},
		}},
	}}}
	runCLI := catalogCaller(t, "ecs", "zone", fake)

	stdout, stderr, code := runCLI("ecs", "zone", "list", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("ecs zone list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	zones, _ := decodeObject(t, stdout)["zones"].([]any)
	if len(zones) != 2 {
		t.Fatalf("zones = %#v; stdout=%s", zones, stdout)
	}
	for _, raw := range zones {
		zone, _ := raw.(map[string]any)
		for _, key := range []string{
			"available_resource_creation",
			"available_instance_types",
			"available_disk_categories",
			"available_resources",
			"available_volume_categories",
			"dedicated_host_generations",
		} {
			if _, ok := zone[key]; !ok {
				t.Fatalf("zone %s missing stable key %q: %#v", zone["id"], key, zone)
			}
		}
	}
	sparse, _ := zones[1].(map[string]any)
	instanceTypes, _ := sparse["available_instance_types"].([]any)
	if len(instanceTypes) != 0 {
		t.Fatalf("sparse available_instance_types = %#v, want empty array", sparse["available_instance_types"])
	}
}

func TestECSZoneListPropagatesVerboseAndStrategy(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"Zones": map[string]any{"Zone": []any{map[string]any{"ZoneId": "cn-hangzhou-h"}}},
	}}}
	runCLI := catalogCaller(t, "ecs", "zone", fake)

	stdout, stderr, code := runCLI("ecs", "zone", "list", "--region", "cn-hangzhou", "--filter", "spot-strategy=SpotAsPriceGo", "--verbose")
	if code != 0 {
		t.Fatalf("ecs zone list verbose exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	req := fake.calls[0].request
	if req["SpotStrategy"] != "SpotAsPriceGo" {
		t.Fatalf("SpotStrategy not propagated: %#v", req)
	}
	if req["Verbose"] != true {
		t.Fatalf("Verbose not propagated as bool true: %#v", req)
	}
}
