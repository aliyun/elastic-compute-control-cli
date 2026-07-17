package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ecctl/e2e/internal/scenario"
)

func TestPublicACKUserFixtureUsesGenericRAMCall(t *testing.T) {
	fixture, err := loadFixture(filepath.Join("..", "..", "fixtures", "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range fixture.Provision {
		if step.ID != "ack_test_user" {
			continue
		}
		for label, command := range map[string]string{"run": step.Run, "teardown": step.Teardown} {
			if !strings.Contains(command, "call ram") {
				t.Fatalf("ack_test_user %s must stay executable through ecctl-public generic RAM calls: %q", label, command)
			}
			if !strings.Contains(command, "--region cn-hangzhou") {
				t.Fatalf("ack_test_user %s must pin the RAM global endpoint region before call: %q", label, command)
			}
		}
		if !strings.Contains(step.Run, "CreateUser") || !strings.Contains(step.Run, "--request") || strings.Contains(step.Run, "--api-param") {
			t.Fatalf("ack_test_user create must use a structured RAM CreateUser request: %q", step.Run)
		}
		if step.At != "$.response.User" || step.Capture["ack_test_user_id"] != "UserId" || step.Capture["ack_test_user_name"] != "UserName" {
			t.Fatalf("ack_test_user response mapping = at %q capture %#v", step.At, step.Capture)
		}
		return
	}
	t.Fatal("ack_test_user fixture step not found")
}

func TestACKKubeconfigRootOnlyUpdateIsIsolated(t *testing.T) {
	base, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "kubeconfig-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range base.Steps {
		if strings.Contains(step.Run, "kubeconfig update") {
			t.Fatalf("base kubeconfig lifecycle must stay runnable by RAM users: %q", step.Run)
		}
	}

	update, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "kubeconfig-update-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(update.RequiresPrerequisites, []string{"ack.root_account"}) {
		t.Fatalf("kubeconfig update prerequisites = %#v, want ack.root_account", update.RequiresPrerequisites)
	}
	if !reflect.DeepEqual(update.Needs, []string{"ack_shared_cluster", "ack_test_user"}) {
		t.Fatalf("kubeconfig update stack needs = %#v", update.Needs)
	}
	found := false
	for _, step := range update.Steps {
		if strings.Contains(step.Run, "kubeconfig update") {
			found = true
		}
	}
	if !found {
		t.Fatal("root-only kubeconfig update case has no update operation")
	}
}

func TestLingjunClusterScalingPrerequisiteIsIsolated(t *testing.T) {
	base, err := scenario.Load(filepath.Join("..", "..", "cases", "lingjun", "cluster-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(base.RequiresPrerequisites) != 0 {
		t.Fatalf("base Lingjun cluster prerequisites = %#v, want none", base.RequiresPrerequisites)
	}
	if len(base.RequiresParams) != 0 {
		t.Fatalf("base Lingjun cluster parameters = %#v, want none", base.RequiresParams)
	}
	for _, step := range base.Steps {
		if strings.Contains(step.Run, "--node-groups") || strings.Contains(step.Run, "cluster update") {
			t.Fatalf("base Lingjun cluster step %q still depends on scaling inventory: %q", step.Name, step.Run)
		}
	}

	scaling, err := scenario.Load(filepath.Join("..", "..", "cases", "lingjun", "cluster-scaling-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scaling.RequiresPrerequisites, []string{"lingjun.cluster"}) {
		t.Fatalf("Lingjun scaling prerequisites = %#v, want lingjun.cluster", scaling.RequiresPrerequisites)
	}
	foundShrink := false
	foundExtend := false
	for _, step := range scaling.Steps {
		foundShrink = foundShrink || strings.Contains(step.Run, "--shrink")
		foundExtend = foundExtend || strings.Contains(step.Run, "--extend")
	}
	if !foundShrink || !foundExtend {
		t.Fatalf("Lingjun scaling operations = shrink %t, extend %t", foundShrink, foundExtend)
	}
}

func TestACKPermissionLifecycleUsesProvisionedRAMUser(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "permission-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(suite.Needs, []string{"ack_shared_cluster", "ack_test_user"}) {
		t.Fatalf("permission stack needs = %#v", suite.Needs)
	}
	for _, step := range suite.Steps {
		if strings.Contains(step.Run, "ack permission") {
			if !strings.Contains(step.Run, "{{.stack.ack_test_user_id}}") {
				t.Fatalf("permission step %q does not target the provisioned RAM user: %q", step.Name, step.Run)
			}
			if strings.Contains(step.Run, "is_ram_role=true") {
				t.Fatalf("permission step %q misclassifies the RAM user as a role: %q", step.Name, step.Run)
			}
		}
	}
}

func TestFixturePlanReturnsOnlyRequestedNodesAndDependencies(t *testing.T) {
	fixture := &Fixture{Provision: []ProvisionStep{
		{ID: "vpc"},
		{ID: "vswitch", Needs: []string{"vpc"}},
		{ID: "security_group", Needs: []string{"vpc"}},
		{ID: "image"},
	}}

	steps, err := fixture.plan([]string{"vswitch"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	if want := []string{"vpc", "vswitch"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("planned ids = %v, want %v", ids, want)
	}
}

func TestStackPrerequisitesBySuiteIncludesOnlySelectedClosure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(`
provision:
  - id: vpc
    requires_prerequisites: [lingjun.cluster]
    run: ecctl vpc create
  - id: cluster
    needs: [vpc]
    requires_prerequisites: [lingjun.cluster]
    run: ecctl lingjun cluster create
  - id: image
    requires_prerequisites: [ecs.image]
    run: ecctl ecs image describe m-example
`), 0o644); err != nil {
		t.Fatal(err)
	}
	suites := []*scenario.Suite{
		{Path: "ack/cluster.yaml", Needs: []string{"cluster"}},
		{Path: "ecs/region.yaml"},
	}

	got, err := StackPrerequisitesBySuite(path, suites)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"ack/cluster.yaml": {"lingjun.cluster"},
		"ecs/region.yaml":  {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prerequisites = %#v, want %#v", got, want)
	}
}

func TestStackStepsForSuitesIncludesOnlySelectedClosure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(`
provision:
  - id: vpc
    run: ecctl vpc create
    teardown: ecctl vpc delete vpc-example
  - id: cluster
    needs: [vpc]
    run: ecctl ack create
    teardown: ecctl ack delete c-example
  - id: unused
    run: ecctl ecs instance create
`), 0o644); err != nil {
		t.Fatal(err)
	}

	steps, err := StackStepsForSuites(path, []*scenario.Suite{{Needs: []string{"cluster"}}})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	if want := []string{"vpc", "cluster"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("planned ids = %v, want %v", ids, want)
	}
}

func TestFixturePlanRejectsUnknownRequestedNode(t *testing.T) {
	fixture := &Fixture{Provision: []ProvisionStep{{ID: "vpc"}}}
	_, err := fixture.plan([]string{"image"})
	if err == nil || !strings.Contains(err.Error(), `unknown dependency "image"`) {
		t.Fatalf("error = %v, want unknown dependency", err)
	}
}

func TestFixtureRequirementsComeOnlyFromPlannedNodes(t *testing.T) {
	fixture := &Fixture{Provision: []ProvisionStep{
		{ID: "vpc"},
		{ID: "vswitch", Needs: []string{"vpc"}, RequiresParams: []string{"ecs.zone"}},
		{ID: "image", RequiresParams: []string{"ecs.image_id"}},
	}}
	planned, err := fixture.plan([]string{"vswitch"})
	if err != nil {
		t.Fatal(err)
	}
	selected := &Fixture{Provision: planned}
	if got, want := selected.requirements(), []string{"ecs.zone"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requirements = %v, want %v", got, want)
	}
}

func TestTopoSortRejectsDuplicateNodeID(t *testing.T) {
	_, err := topoSort([]ProvisionStep{{ID: "vpc"}, {ID: "vpc"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate provision id "vpc"`) {
		t.Fatalf("error = %v, want duplicate provision id", err)
	}
}

func TestLoadFixtureRejectsDuplicateCaptureProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(`
provision:
  - id: vpc
    run: ecctl vpc create
    capture: { shared: id }
  - id: image
    run: ecctl call ecs DescribeImages
    capture: { shared: ImageId }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadFixture(path)
	if err == nil || !strings.Contains(err.Error(), `stack capture "shared" is provided by both "vpc" and "image"`) {
		t.Fatalf("error = %v, want duplicate capture provider", err)
	}
}
