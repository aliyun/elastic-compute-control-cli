package surface

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestLoadFromBinaryReadsSelectedCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	path := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '{\"products\":[{\"product\":\"ecs\",\"resources\":[{\"name\":\"instance\",\"actions\":[\"list\"]}]}]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	caps, err := LoadFromBinary(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps.Products) != 1 || caps.Products[0].Name != "ecs" {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestValidateSuitesRejectsOperationOutsideSelectedBinary(t *testing.T) {
	caps, err := Decode([]byte(`{
  "products": [{
    "product": "ecs",
    "resources": [{"name": "instance", "actions": ["list"]}]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Surface:  scenario.SurfacePublic,
		Resource: "ecs/instance",
		Path:     "cases/ecs/instance.yaml",
		Steps:    []scenario.Step{{Name: "create", Run: "ecctl ecs instance create"}},
	}}
	errs := ValidateSuites(suites, caps)
	if len(errs) != 1 || errs[0].Code != "unsupported_action" {
		t.Fatalf("errors = %+v, want unsupported_action", errs)
	}
}

func TestValidateSuitesRejectsTeardownOutsideSelectedBinary(t *testing.T) {
	caps, err := Decode([]byte(`{
  "products": [{
    "product": "ecs",
    "resources": [{"name": "instance", "actions": ["create"]}]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Path: "cases/ecs/instance.yaml",
		Steps: []scenario.Step{{
			Name: "create", Run: "ecctl ecs instance create",
			Teardown: "ecctl ecs instance delete i-example",
		}},
	}}
	errs := ValidateSuites(suites, caps)
	if len(errs) != 1 || errs[0].Code != "unsupported_action" || errs[0].Step != "create teardown" {
		t.Fatalf("errors = %+v, want teardown unsupported_action", errs)
	}
}

func TestValidateSuitesAcceptsGlobalFlagsBeforeRawCall(t *testing.T) {
	caps, err := Decode([]byte(`{
  "products": [{
    "product": "ecs",
    "resources": [{"name": "instance", "actions": ["list"]}]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Path: "fixtures/stack.yaml",
		Steps: []scenario.Step{{
			Name: "ram user", Run: "ecctl --region cn-hangzhou call ram CreateUser",
		}},
	}}
	if errs := ValidateSuites(suites, caps); len(errs) != 0 {
		t.Fatalf("errors = %+v, want none", errs)
	}
}

func TestValidateSuitesAcceptsDefaultAndNestedResources(t *testing.T) {
	caps, err := Decode([]byte(`{
  "products": [{
    "product": "ack",
    "resources": [
      {"name": "ack", "actions": ["list"]},
      {"name": "check-item", "actions": ["list"]}
    ]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Surface:  scenario.SurfaceFull,
		Resource: "ack/check-item",
		Path:     "cases/ack/check-item.yaml",
		Steps: []scenario.Step{
			{Name: "clusters", Run: "ecctl ack list"},
			{Name: "items", Run: "ecctl ack diagnosis check-item list --cluster c"},
		},
	}}
	if errs := ValidateSuites(suites, caps); len(errs) != 0 {
		t.Fatalf("errors = %+v, want none", errs)
	}
}
