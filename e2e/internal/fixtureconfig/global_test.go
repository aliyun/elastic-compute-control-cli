package fixtureconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRunConfigReadsRegionProfilesAndLiteralValues(t *testing.T) {
	path := writeFixture(t, "e2e.yaml", `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
      prerequisites:
        ecs.image:
          oss_bucket: e2e-images
        lingjun.cluster:
          node_group_ids: [ng-a, ng-b]
    - id: cn-zhangjiakou
values:
  runtime.mode:
    value: standard
paths:
  cases: cases
  stack: fixtures/stack.yaml
  parameter_policy: fixtures/parameter-policy.yaml
`)

	config, err := LoadRunConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Regions.Candidates; len(got) != 2 || got[0].ID != "cn-hangzhou" || got[1].ID != "cn-zhangjiakou" {
		t.Fatalf("regions = %#v", got)
	}
	prerequisites, err := config.Regions.Candidates[0].ResolvePrerequisites([]string{"ecs.image", "lingjun.cluster"})
	if err != nil {
		t.Fatal(err)
	}
	ecs := prerequisites["ecs"].(map[string]any)
	image := ecs["image"].(map[string]any)
	if got := image["oss_bucket"]; got != "e2e-images" {
		t.Fatalf("oss bucket = %#v", got)
	}
	lingjun := prerequisites["lingjun"].(map[string]any)
	if got := lingjun["cluster"].(map[string]any)["node_group_ids"]; len(got.([]any)) != 2 {
		t.Fatalf("node groups = %#v", got)
	}
	values, err := config.Values.Resolve([]string{"runtime.mode"})
	if err != nil {
		t.Fatal(err)
	}
	if got := values["runtime"].(map[string]any)["mode"]; got != "standard" {
		t.Fatalf("runtime mode = %#v", got)
	}
	if config.Paths.Stack != "fixtures/stack.yaml" || config.Paths.ParameterPolicy != "fixtures/parameter-policy.yaml" {
		t.Fatalf("paths = %#v", config.Paths)
	}
}

func TestLoadRunConfigRejectsEnvironmentBackedValues(t *testing.T) {
	path := writeFixture(t, "e2e.yaml", `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
values:
  ecs.image.oss_bucket:
    env: E2E_OSS_BUCKET
`)

	_, err := LoadRunConfig(path)
	if err == nil {
		t.Fatal("expected environment-backed value to be rejected")
	}
	if !strings.Contains(err.Error(), "field env not found") {
		t.Fatalf("error = %q, want unknown env field", err)
	}
}

func TestLoadRunConfigRejectsInvalidVersionOrRegionProfiles(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "version", body: "version: 1\nregions:\n  candidates:\n    - id: cn-hangzhou\n", want: "version 2"},
		{name: "empty", body: "version: 2\nregions:\n  candidates: []\n", want: "at least one region profile"},
		{name: "missing id", body: "version: 2\nregions:\n  candidates:\n    - prerequisites: {}\n", want: "profile 0 id is empty"},
		{name: "duplicate", body: "version: 2\nregions:\n  candidates:\n    - id: cn-hangzhou\n    - id: cn-hangzhou\n", want: "duplicate region"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFixture(t, "e2e.yaml", tt.body)
			_, err := LoadRunConfig(path)
			if err == nil {
				t.Fatal("expected invalid run config error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadRunConfigDoesNotValidatePrerequisiteFields(t *testing.T) {
	path := writeFixture(t, "e2e.yaml", `
version: 2
regions:
  candidates:
    - id: cn-hangzhou
      prerequisites:
        ecs.instance_renew: {}
        future.bundle:
          future_field: value
`)

	config, err := LoadRunConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Regions.Candidates[0].HasPrerequisites([]string{"ecs.instance_renew", "future.bundle"}) {
		t.Fatal("declared bundles should be routable without field-schema validation")
	}
}

func TestRegionProfileResolvePrerequisitesRejectsMissingBundle(t *testing.T) {
	profile := RegionProfile{ID: "cn-hangzhou", Prerequisites: map[string]map[string]any{
		"ecs.image": {"oss_bucket": "e2e-images"},
	}}

	_, err := profile.ResolvePrerequisites([]string{"ecs.instance_renew"})
	if err == nil {
		t.Fatal("expected missing prerequisite bundle error")
	}
	if !strings.Contains(err.Error(), `region "cn-hangzhou" does not declare prerequisite bundle "ecs.instance_renew"`) {
		t.Fatalf("error = %q", err)
	}
}
