package runplan

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func profile(id string, bundles ...string) fixtureconfig.RegionProfile {
	prerequisites := make(map[string]map[string]any, len(bundles))
	for _, bundle := range bundles {
		prerequisites[bundle] = completeBundle(bundle)
	}
	return fixtureconfig.RegionProfile{ID: id, Prerequisites: prerequisites}
}

func completeBundle(name string) map[string]any {
	switch name {
	case "test.primary":
		return map[string]any{"resource_id": "primary"}
	case "test.optional":
		return map[string]any{"instance_id": "i-test"}
	case "lingjun.cluster":
		return map[string]any{"node_group_ids": []any{"ng-a", "ng-b"}}
	default:
		return map[string]any{"id": name + "-id"}
	}
}

func TestBuildGroupsSuitesByRegionRequirementSignature(t *testing.T) {
	suites := []*scenario.Suite{
		{Path: "ecs/image.yaml", Resource: "ecs/image", RequiresPrerequisites: []string{"test.primary"}},
		{Path: "ecs/instance.yaml", Resource: "ecs/instance", RequiresPrerequisites: []string{"test.optional"}},
		{Path: "ecs/image-describe.yaml", Resource: "ecs/image", RequiresPrerequisites: []string{"test.primary"}},
	}
	profiles := []fixtureconfig.RegionProfile{
		profile("cn-hangzhou", "test.primary", "test.optional"),
		profile("cn-zhangjiakou", "test.primary"),
	}

	units, err := Build(Request{Suites: suites, Profiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("execution units = %d, want 2: %#v", len(units), units)
	}
	if got := len(units[0].Suites); got != 2 {
		t.Fatalf("image unit suites = %d, want 2", got)
	}
	if got := len(units[0].Assignments); got != 2 {
		t.Fatalf("image assignments = %d, want 2", got)
	}
	if got := len(units[1].Assignments); got != 1 || units[1].Assignments[0].Regions[PrimaryRole].ID != "cn-hangzhou" {
		t.Fatalf("renew assignments = %#v", units[1].Assignments)
	}
}

func TestBuildCreatesOrderedDistinctCrossRegionAssignments(t *testing.T) {
	suite := &scenario.Suite{
		Path:                  "ecs/image-copy.yaml",
		Resource:              "ecs/image",
		RequiresPrerequisites: []string{"test.primary"},
		RegionRequirements: map[string]scenario.RegionRequirement{
			"destination": {RequiresPrerequisites: []string{"test.primary"}, DistinctFrom: PrimaryRole},
		},
	}
	profiles := []fixtureconfig.RegionProfile{
		profile("cn-hangzhou", "test.primary"),
		profile("cn-zhangjiakou", "test.primary"),
		profile("cn-heyuan", "test.primary"),
	}

	units, err := Build(Request{Suites: []*scenario.Suite{suite}, Profiles: profiles, PrimaryRegion: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	assignments := units[0].Assignments
	if len(assignments) != 2 {
		t.Fatalf("assignments = %#v, want two destinations", assignments)
	}
	if got := assignments[0].Regions["destination"].ID; got != "cn-zhangjiakou" {
		t.Fatalf("first destination = %q", got)
	}
	if got := assignments[1].Regions["destination"].ID; got != "cn-heyuan" {
		t.Fatalf("second destination = %q", got)
	}
}

func TestBuildFailsBeforeExecutionWhenNoCompleteAssignmentExists(t *testing.T) {
	suite := &scenario.Suite{
		Path:                  "ack/permission.yaml",
		Resource:              "ack/permission",
		RequiresPrerequisites: []string{"lingjun.cluster"},
	}

	_, err := Build(Request{
		Suites:   []*scenario.Suite{suite},
		Profiles: []fixtureconfig.RegionProfile{profile("cn-hangzhou", "test.optional")},
	})
	if err == nil {
		t.Fatal("expected planning to fail")
	}
	if !strings.Contains(err.Error(), "lingjun.cluster") || !strings.Contains(err.Error(), "ack/permission.yaml") {
		t.Fatalf("error = %q", err)
	}
}

func TestBuildIncludesSelectedStackPrerequisites(t *testing.T) {
	suite := &scenario.Suite{Path: "ack/cluster.yaml", Resource: "ack/ack"}

	units, err := Build(Request{
		Suites: []*scenario.Suite{suite},
		Profiles: []fixtureconfig.RegionProfile{
			profile("cn-hangzhou", "lingjun.cluster"),
			profile("cn-zhangjiakou"),
		},
		StackPrerequisites: map[string][]string{
			"ack/cluster.yaml": {"lingjun.cluster"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(units[0].Assignments); got != 1 || units[0].Assignments[0].Regions[PrimaryRole].ID != "cn-hangzhou" {
		t.Fatalf("assignments = %#v", units[0].Assignments)
	}
}

func TestBuildSeparatesDynamicCapabilitySignatures(t *testing.T) {
	suites := []*scenario.Suite{
		{Path: "ecs/instance.yaml", Resource: "ecs/instance"},
		{
			Path: "ecs/eni.yaml", Resource: "ecs/eni",
			ParameterConstraints: scenario.ParameterConstraints{ECS: scenario.ECSParameterConstraints{MinENIQuantity: 2, MinENIPrivateIPAddressQuantity: 6}},
		},
		{
			Path: "ecs/snapshot-group.yaml", Resource: "ecs/snapshot-group",
			ParameterConstraints: scenario.ParameterConstraints{ECS: scenario.ECSParameterConstraints{AllowedSystemDiskCategories: []string{"cloud_essd", "cloud_auto"}}},
		},
		{
			Path: "ecs/disk.yaml", Resource: "ecs/disk",
			ParameterConstraints: scenario.ParameterConstraints{ECS: scenario.ECSParameterConstraints{AllowedDataDiskCategories: []string{"cloud_essd"}}},
		},
		{Path: "ack/upgrade.yaml", Resource: "ack/ack", RequiresParams: []string{"ack.upgrade_version"}},
	}

	units, err := Build(Request{Suites: suites, Profiles: []fixtureconfig.RegionProfile{profile("cn-hangzhou")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 5 {
		t.Fatalf("execution units = %d, want one per capability signature: %#v", len(units), units)
	}
	for _, unit := range units {
		if len(unit.Suites) != 1 {
			t.Fatalf("capability unit unexpectedly mixed suites: %#v", unit)
		}
	}
}

func TestBuildPrefersButDoesNotRequireStepPrerequisites(t *testing.T) {
	suite := &scenario.Suite{
		Path: "ecs/instance.yaml", Resource: "ecs/instance",
		Steps: []scenario.Step{
			{Name: "ordinary", Run: "ecctl ecs instance list"},
			{Name: "renew", Run: "ecctl ecs instance renew i-test", RequiresPrerequisites: []string{"test.optional"}},
		},
	}
	units, err := Build(Request{
		Suites: []*scenario.Suite{suite},
		Profiles: []fixtureconfig.RegionProfile{
			profile("cn-no-renew"),
			profile("cn-renew", "test.optional"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(units[0].Assignments); got != 2 {
		t.Fatalf("assignments = %d, want both profiles", got)
	}
	if got := units[0].Assignments[0].Regions[PrimaryRole].ID; got != "cn-renew" {
		t.Fatalf("first assignment = %q, want optional prerequisite profile", got)
	}
	if got := units[0].Requirements[PrimaryRole]; len(got) != 0 {
		t.Fatalf("hard requirements = %#v, want none", got)
	}
}
