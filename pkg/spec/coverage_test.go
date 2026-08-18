package spec

import (
	"testing"
)

func TestBindingRequestCoverage(t *testing.T) {
	request := map[string]any{
		"RegionId":   "$context.region",
		"InstanceId": "$.id",
		"DataDisk": map[string]any{
			"each": "$.data_disks",
			"fields": map[string]any{
				"Size":     "$.size",
				"Category": "$.category",
			},
		},
		"System": map[string]any{
			"from": "$.system",
			"fields": map[string]any{
				"Size": "$.size",
			},
		},
		"ClockOptions": map[string]any{"from": "$.clock_options"},
		"Ipv6Address":  map[string]any{"each": "$.ipv6_addresses"},
		"Tag":          map[string]any{"raw": "$.api_param"},
	}
	got := BindingRequestCoverage(request)
	want := map[string]bool{
		"RegionId":          true,
		"InstanceId":        true,
		"DataDisk.Size":     true,
		"DataDisk.Category": true,
		"System.Size":       true,
		"ClockOptions":      true,
		"Ipv6Address":       true,
	}
	for name := range want {
		if !got[name] {
			t.Errorf("BindingRequestCoverage missing %q", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("BindingRequestCoverage produced unexpected %q", name)
		}
	}
}

func TestBindingRequestCoverageEmpty(t *testing.T) {
	got := BindingRequestCoverage(map[string]any{})
	if len(got) != 0 {
		t.Fatalf("empty request coverage = %#v, want empty", got)
	}
}
