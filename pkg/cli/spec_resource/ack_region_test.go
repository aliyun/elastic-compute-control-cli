package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestACKRegionListHelpIsGeneratedFromSpec(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("ack", "region", "list", "--help")
	if code != 0 {
		t.Fatalf("ack region list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "List ACK-supported regions") || !strings.Contains(stdout, "ecctl ack region list [flags]") {
		t.Fatalf("ack region list help did not come from spec: %s", stdout)
	}
}

func TestACKRegionListInvokesDescribeRegions(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"requestId": "req-regions",
		"regions": []any{
			map[string]any{"regionId": "cn-hangzhou", "localName": "华东1（杭州）"},
			map[string]any{"regionId": "cn-beijing", "localName": "华北2（北京）"},
		},
	}}}
	runCLI := withCaller(func(_ string, _ string, res spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if res.Product != "ack" || res.Resource != "region" {
			t.Fatalf("resource = %s/%s, want ack/region", res.Product, res.Resource)
		}
		if res.APIProduct != "CS" {
			t.Fatalf("api_product = %q, want CS", res.APIProduct)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ack", "region", "list", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ack region list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeRegions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["RegionId"] != "cn-hangzhou" {
		t.Fatalf("DescribeRegions RegionId = %#v, want cn-hangzhou", fake.calls[0].request["RegionId"])
	}
	regions, _ := decodeObject(t, stdout)["regions"].([]any)
	if len(regions) != 2 {
		t.Fatalf("regions = %#v; stdout=%s", regions, stdout)
	}
	first, _ := regions[0].(map[string]any)
	if first["id"] != "cn-hangzhou" || first["local_name"] != "华东1（杭州）" {
		t.Fatalf("unexpected first region: %#v", first)
	}
}
