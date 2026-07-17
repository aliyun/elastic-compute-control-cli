package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "ok.yaml", `
resource: vpc/vpc
needs: [vpc]
steps:
  - name: create
    run: ecctl vpc create --name x --cidr 10.0.0.0/16
    at: $.vpc
    expect:
      id: { type: string, prefix: vpc- }
      cidr: "10.0.0.0/16"
    capture:
      vpc_id: id
    teardown: ecctl vpc delete {{.vpc_id}}
`)
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Resource != "vpc/vpc" || len(s.Steps) != 1 {
		t.Fatalf("unexpected suite: %+v", s)
	}
	if s.Surface != SurfacePublic {
		t.Fatalf("surface = %q, want default %q", s.Surface, SurfacePublic)
	}
	if len(s.Steps[0].Expect) != 2 || s.Steps[0].Expect[0].Path != "id" {
		t.Fatalf("expect order/parse wrong: %+v", s.Steps[0].Expect)
	}
}

func TestLoadParameterConstraints(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "constraints.yaml", `
resource: ecs/eni
requires_params: [ecs.instance_type, ecs.system_disk_category, ecs.data_disk_category]
parameter_constraints:
  ecs:
    min_eni_quantity: 2
    min_eni_private_ip_address_quantity: 6
    allowed_system_disk_categories: [cloud_essd, cloud_auto]
    allowed_data_disk_categories: [cloud_essd]
steps:
  - name: list
    run: ecctl ecs eni list
`)

	suite, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if suite.ParameterConstraints.ECS.MinENIQuantity != 2 {
		t.Fatalf("min ENI quantity = %d", suite.ParameterConstraints.ECS.MinENIQuantity)
	}
	if suite.ParameterConstraints.ECS.MinENIPrivateIPAddressQuantity != 6 {
		t.Fatalf("min ENI private IP quantity = %d", suite.ParameterConstraints.ECS.MinENIPrivateIPAddressQuantity)
	}
	if got := suite.ParameterConstraints.ECS.AllowedSystemDiskCategories; len(got) != 2 || got[0] != "cloud_essd" {
		t.Fatalf("allowed system disk categories = %#v", got)
	}
	if got := suite.ParameterConstraints.ECS.AllowedDataDiskCategories; len(got) != 1 || got[0] != "cloud_essd" {
		t.Fatalf("allowed data disk categories = %#v", got)
	}
}

func TestLoadRejectsInvalidParameterConstraints(t *testing.T) {
	dir := t.TempDir()
	for name, constraints := range map[string]string{
		"negative-eni":    "min_eni_quantity: -1",
		"negative-eni-ip": "min_eni_private_ip_address_quantity: -1",
		"empty-disk":      "allowed_system_disk_categories: [cloud_essd, '']",
		"duplicate":       "allowed_system_disk_categories: [cloud_essd, cloud_essd]",
		"empty-data":      "allowed_data_disk_categories: [cloud_essd, '']",
		"duplicate-data":  "allowed_data_disk_categories: [cloud_essd, cloud_essd]",
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, dir, name+".yaml", `
resource: ecs/eni
parameter_constraints:
  ecs:
    `+constraints+`
steps:
  - name: list
    run: ecctl ecs eni list
`)
			if _, err := Load(p); err == nil {
				t.Fatal("expected invalid parameter constraint error")
			}
		})
	}
}

func TestLoadRejectsUnknownSurface(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "bad-surface.yaml", `
surface: private
resource: vpc/vpc
steps:
  - name: list
    run: ecctl vpc list
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unknown surface")
	}
}

func TestRejectUnknownMatcher(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "bad.yaml", `
resource: vpc/vpc
steps:
  - name: c
    run: ecctl vpc create
    expect:
      id: { startswith: vpc- }
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for unknown matcher")
	}
}

func TestRejectNonEcctlRun(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "bad.yaml", `
resource: vpc/vpc
steps:
  - name: c
    run: aliyun vpc CreateVpc
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for non-ecctl run")
	}
}

func TestLoadRegionPrerequisiteRequirements(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "image-copy.yaml", `
resource: ecs/image
requires_prerequisites: [ecs.image]
region_requirements:
  destination:
    requires_prerequisites: [ecs.image]
    distinct_from: primary
steps:
  - name: copy
    run: ecctl ecs image copy {{.prerequisites.ecs.image.image_id}} --destination-region {{.regions.destination.id}}
    teardown: ecctl ecs image delete {{.image_id}}
    teardown_region: destination
`)

	suite, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := suite.RequiresPrerequisites; len(got) != 1 || got[0] != "ecs.image" {
		t.Fatalf("primary prerequisites = %#v", got)
	}
	destination, ok := suite.RegionRequirements["destination"]
	if !ok {
		t.Fatalf("region requirements = %#v", suite.RegionRequirements)
	}
	if destination.DistinctFrom != "primary" || len(destination.RequiresPrerequisites) != 1 {
		t.Fatalf("destination requirement = %#v", destination)
	}
	if got := suite.Steps[0].TeardownRegion; got != "destination" {
		t.Fatalf("teardown region = %q", got)
	}
}

func TestLoadRejectsUnknownTeardownRegion(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "bad-region.yaml", `
resource: ecs/image
steps:
  - name: delete
    run: ecctl ecs image delete m-example
    teardown: ecctl ecs image delete m-example
    teardown_region: destination
`)

	if _, err := Load(p); err == nil {
		t.Fatal("expected unknown teardown region error")
	}
}
