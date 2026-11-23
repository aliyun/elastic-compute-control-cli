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

func TestValidateSuitesRejectsRawVolumeWritesButAllowsReads(t *testing.T) {
	caps, err := Decode([]byte(`{"products":[{"product":"sandbox","resources":[{"name":"volume","actions":["get"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"ecctl call fcsandbox CreateVolume --request '{}'",
		"ecctl --region cn-hangzhou call FCSandbox UpdateVolume --request '{}'",
		"ecctl call sbx DeleteVolume --request '{}'",
		"ecctl call --request '{}' -- FCSandbox CreateVolume",
		"ecctl call --api-param name=owned -- FCSandbox CreateVolume",
		"ecctl call --request '{}' -- FCSandbox UpdateVolume",
		"ecctl call --api-param teamID=team-other -- FCSandbox UpdateVolume",
		`ecctl call --request '{"volumeID":"vol-other"}' -- FCSandbox DeleteVolume`,
		"ecctl call --api-param volumeID=vol-other -- FCSandbox DeleteVolume",
		"ecctl call --schema FCSandbox CreateVolume --schema=false --request '{}'",
		"ecctl call --list FCSandbox UpdateVolume --list=false --request '{}'",
		"ecctl call --generate-request FCSandbox DeleteVolume --generate-request=false --request '{}'",
	} {
		suite := &scenario.Suite{Path: "cases/sandbox/volume-lifecycle.yaml", Steps: []scenario.Step{{Name: "raw", Run: command}}}
		issues := ValidateSuites([]*scenario.Suite{suite}, caps)
		if len(issues) != 1 || issues[0].Code != "unsupported_raw_volume_mutation" {
			t.Errorf("raw Volume write %q: issues=%+v", command, issues)
		}
	}
	read := &scenario.Suite{Path: "cases/sandbox/volume-lifecycle.yaml", Steps: []scenario.Step{{Name: "raw read", Run: "ecctl call fcsandbox GetVolume --request '{}'"}}}
	if issues := ValidateSuites([]*scenario.Suite{read}, caps); len(issues) != 0 {
		t.Errorf("read-only raw Volume call was rejected: %+v", issues)
	}
	read.Steps[0].Run = "ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value CreateVolume"
	if issues := ValidateSuites([]*scenario.Suite{read}, caps); len(issues) != 0 {
		t.Errorf("read-only ECS call with Volume-looking argument was rejected: %+v", issues)
	}
	read.Steps[0].Run = "ecctl call ecs DescribeInstances --Tag.1.Key sandbox --Tag.1.Value volume"
	if issues := ValidateSuites([]*scenario.Suite{read}, caps); len(issues) != 0 {
		t.Errorf("read-only ECS call with Volume family values was rejected: %+v", issues)
	}
	read.Steps[0].Run = "ecctl call --api-param Tag.1.Key=FCSandbox --api-param Tag.1.Value=CreateVolume -- ecs DescribeInstances"
	if issues := ValidateSuites([]*scenario.Suite{read}, caps); len(issues) != 0 {
		t.Errorf("read-only ECS call after delimiter was rejected: %+v", issues)
	}
}

func TestVolumeSurfaceRejectsUnsupportedOutputShorthand(t *testing.T) {
	caps, err := Decode([]byte(`{"products":[{"product":"sandbox","resources":[{"name":"volume","actions":["create","get","update","delete"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"ecctl sandbox volume create --name x --team-id team-1 -o json",
		"ecctl sandbox -o json volume create --name x --team-id team-1",
		"ecctl sandbox volume get vol-1 -o json",
		"ecctl sandbox volume update vol-1 --status=AVAILABLE -o json",
		"ecctl sandbox volume delete vol-1 -o json",
	} {
		suite := &scenario.Suite{Path: "cases/sandbox/volume-lifecycle.yaml", Steps: []scenario.Step{{Name: "output alias", Run: command}}}
		issues := ValidateSuites([]*scenario.Suite{suite}, caps)
		if len(issues) != 1 || issues[0].Code != "invalid_volume_command" {
			t.Errorf("unsupported output alias passed surface validation: %s: %+v", command, issues)
		}
	}
	if issues := ValidateSuites([]*scenario.Suite{{Path: "cases/sandbox/volume-lifecycle.yaml", Steps: []scenario.Step{{Name: "supported output", Run: "ecctl sandbox volume get vol-1 --output=json"}}}}, caps); len(issues) != 0 {
		t.Errorf("supported --output spelling was rejected: %+v", issues)
	}
}

func TestVolumeParserProbeRejectsUnsupportedOutputShorthand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	fake := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	suite := &scenario.Suite{Path: "cases/sandbox/volume-lifecycle.yaml", Steps: []scenario.Step{{Name: "output alias", Run: "ecctl sandbox volume get vol-1 -o json"}}}
	issues := ValidateCommandParsers(context.Background(), fake, []*scenario.Suite{suite})
	if len(issues) != 1 || issues[0].Code != "invalid_cli_command" {
		t.Fatalf("help-based parser probe accepted unsupported -o: %+v", issues)
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

func TestValidateSuitesPrefersExecutableChildOverGroupingParent(t *testing.T) {
	caps, err := Decode([]byte(`{
  "products": [{
    "product": "ack",
    "resources": [
      {"name": "inspect", "actions": null},
      {"name": "config", "parent": "inspect", "actions": ["update"]}
    ]
  }]
}`))
	if err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Surface:  scenario.SurfaceFull,
		Resource: "ack/config",
		Path:     "cases/ack/inspect-config-lifecycle.yaml",
		Steps: []scenario.Step{{
			Name: "update", Run: "ecctl ack inspect config update --cluster c",
		}},
	}}
	if issues := ValidateSuites(suites, caps); len(issues) != 0 {
		t.Fatalf("issues = %+v, want child resource accepted", issues)
	}
}

func TestValidateCommandParsersRejectsUnknownFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	path := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(path, []byte(`#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "--unknown" ]; then
    echo "unknown flag: --unknown" >&2
    exit 2
  fi
done
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Path: "cases/ecs/instance.yaml",
		Steps: []scenario.Step{{
			Name: "list", Run: "ecctl ecs instance list --unknown value",
		}},
	}}
	issues := ValidateCommandParsers(context.Background(), path, suites)
	if len(issues) != 1 || issues[0].Code != "invalid_cli_command" ||
		issues[0].Step != "list" {
		t.Fatalf("issues = %+v, want invalid_cli_command for list", issues)
	}
}

func TestValidateCommandParsersSkipsRawCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a shell script")
	}
	path := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{{
		Path: "fixtures/stack.yaml",
		Steps: []scenario.Step{{
			Name: "raw call", Run: "ecctl --region cn-hangzhou call ram CreateUser --request '{}'",
		}},
	}}
	if issues := ValidateCommandParsers(context.Background(), path, suites); len(issues) != 0 {
		t.Fatalf("issues = %+v, want raw call skipped", issues)
	}
}
