package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
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

func TestACKKubeconfigLifecycleDoesNotExposeUpdate(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "kubeconfig-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.RequiresPrerequisites) != 0 {
		t.Fatalf("suite prerequisites = %#v, want none", suite.RequiresPrerequisites)
	}
	if !reflect.DeepEqual(suite.Needs, []string{"ack_shared_cluster"}) {
		t.Fatalf("kubeconfig stack needs = %#v", suite.Needs)
	}
	for _, step := range suite.Steps {
		if strings.Contains(step.Run, "kubeconfig update") {
			t.Fatalf("kubeconfig lifecycle exposes unsupported update operation: %q", step.Run)
		}
		if len(step.RequiresPrerequisites) != 0 {
			t.Fatalf("step %q prerequisites = %#v, want none", step.Name, step.RequiresPrerequisites)
		}
	}
}

func TestACKAddonLifecycleUsesDiscoveredParameters(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "addon-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wantParams := []string{"ack.addon_name", "ack.addon_version", "ack.addon_upgrade_version"}
	if !reflect.DeepEqual(suite.RequiresParams, wantParams) {
		t.Fatalf("addon parameters = %#v, want %#v", suite.RequiresParams, wantParams)
	}
	if len(suite.RequiresPrerequisites) != 0 {
		t.Fatalf("addon prerequisites = %#v, want none", suite.RequiresPrerequisites)
	}
	upgradeIndex := -1
	updateIndex := -1
	for index, step := range suite.Steps {
		if len(step.RequiresPrerequisites) != 0 {
			t.Fatalf("step %q prerequisites = %#v, want none", step.Name, step.RequiresPrerequisites)
		}
		if strings.Contains(step.Run, ".prerequisites.ack.addon_lifecycle") {
			t.Fatalf("step %q still uses manual addon prerequisite: %q", step.Name, step.Run)
		}
		if step.Name == "upgrade" {
			upgradeIndex = index
		}
		if step.Name == "update" {
			updateIndex = index
		}
	}
	if upgradeIndex < 0 || updateIndex < 0 || upgradeIndex >= updateIndex {
		t.Fatalf("addon must upgrade before update because the discovered base version may not support Modify: upgrade=%d update=%d", upgradeIndex, updateIndex)
	}
}

func TestACKSharedFixtureRemovesPolicyAndCheckClusters(t *testing.T) {
	fixture, err := loadFixture(filepath.Join("..", "..", "fixtures", "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ProvisionStep{}
	for _, step := range fixture.Provision {
		byID[step.ID] = step
	}
	for _, removed := range []string{
		"ack_policy_cluster", "ack_policy_nodepool", "ack_check_cluster", "ack_check_nodepool",
		"ack_task_cluster", "ack_cancel_task_cluster",
		"ack_nodepool_upgrade_cluster", "ack_nodepool_upgrade_nodepool",
	} {
		if _, exists := byID[removed]; exists {
			t.Fatalf("dedicated fixture %q still exists", removed)
		}
	}
	for _, shared := range []string{
		"vpc", "vswitch", "image", "ack_shared_cluster", "ack_shared_nodepool",
		"ack_test_instance", "ack_diagnosis_instance", "ack_diagnosis_node",
		"ack_test_user", "ack_policy_gatekeeper_catalog", "ack_policy_template_controller_catalog",
		"ack_policy_gatekeeper", "ack_policy_template_controller",
	} {
		if got := byID[shared].Lifetime; got != FixtureLifetimeRun {
			t.Fatalf("fixture %q lifetime = %q, want run", shared, got)
		}
	}
	sharedCluster := byID["ack_shared_cluster"]
	for _, want := range []string{"--type ManagedKubernetes", "--edition ack.pro.small", "--auto-mode", "--pod-vswitch {{.vswitch}}", "--deletion-protection=false"} {
		if !strings.Contains(sharedCluster.Run, want) {
			t.Fatalf("shared ACK cluster must be Auto Mode Pro; run missing %q: %q", want, sharedCluster.Run)
		}
	}
	if strings.Contains(sharedCluster.Run, "--edition {{.params.ack.edition}}") {
		t.Fatalf("shared ACK cluster must pin the Pro edition: %q", sharedCluster.Run)
	}
	if strings.Contains(sharedCluster.Run, "--pod-cidr") {
		t.Fatalf("Auto Mode cluster must not combine Terway pod VSwitches with a Flannel pod CIDR: %q", sharedCluster.Run)
	}
	for _, unwanted := range []string{"ack.cluster_type", "ack.edition"} {
		if slicesContain(sharedCluster.RequiresParams, unwanted) {
			t.Fatalf("shared ACK cluster still requires dynamic %s: %#v", unwanted, sharedCluster.RequiresParams)
		}
	}
	if !strings.Contains(byID["ack_policy_gatekeeper"].Run, "addon create managed-gatekeeper") {
		t.Fatalf("policy fixture does not install managed-gatekeeper: %q", byID["ack_policy_gatekeeper"].Run)
	}
	if !reflect.DeepEqual(byID["ack_policy_gatekeeper"].Needs, []string{"ack_policy_gatekeeper_catalog"}) {
		t.Fatalf("managed gatekeeper must use its cluster-aware catalog lookup: %#v", byID["ack_policy_gatekeeper"].Needs)
	}
	if !strings.Contains(byID["ack_policy_gatekeeper"].Run, "--version {{.ack_policy_gatekeeper_version}}") {
		t.Fatalf("managed gatekeeper version is not discovered: %q", byID["ack_policy_gatekeeper"].Run)
	}
	if !strings.Contains(byID["ack_policy_template_controller"].Run, "addon create managed-policy-template-controller") {
		t.Fatalf("policy fixture does not install managed-policy-template-controller: %q", byID["ack_policy_template_controller"].Run)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func caseStepsByName(t *testing.T, suite *scenario.Suite) map[string]scenario.Step {
	t.Helper()
	steps := make(map[string]scenario.Step, len(suite.Steps))
	for _, step := range suite.Steps {
		if _, exists := steps[step.Name]; exists {
			t.Fatalf("duplicate step name %q", step.Name)
		}
		steps[step.Name] = step
	}
	return steps
}

func TestACKDestructiveLifecycleUsesOneClusterForNodepoolAndTask(t *testing.T) {
	fixture, err := loadFixture(filepath.Join("..", "..", "fixtures", "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureClusterCreates := 0
	for _, step := range fixture.Provision {
		if step.Resource == "ack/ack" && strings.Contains(step.Run, "ecctl ack create") {
			fixtureClusterCreates++
		}
	}
	if fixtureClusterCreates != 1 {
		t.Fatalf("ACK fixture cluster creates = %d, want only ack_shared_cluster", fixtureClusterCreates)
	}

	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "ack-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !suite.Serial || suite.Execution != scenario.ExecutionSequential {
		t.Fatalf("ACK destructive lifecycle = serial %t execution %q", suite.Serial, suite.Execution)
	}
	if !reflect.DeepEqual(suite.Covers, []string{"ack/nodepool", "ack/task"}) {
		t.Fatalf("ACK destructive covers = %#v", suite.Covers)
	}
	if !reflect.DeepEqual(suite.Needs, []string{"vpc", "vswitch"}) {
		t.Fatalf("ACK destructive stack needs = %#v", suite.Needs)
	}

	clusterCreates := 0
	nodepoolCreates := 0
	startTaskIndex := -1
	pauseIndex := -1
	resumeIndex := -1
	cancelIndex := -1
	detachIndex := -1
	attachIndex := -1
	repairIndex := -1
	for i, step := range suite.Steps {
		switch {
		case strings.Contains(step.Run, "ecctl ack create"):
			clusterCreates++
			if !strings.Contains(step.Run, "--version {{.params.ack.version}}") {
				t.Fatalf("cluster create is not dynamically versioned: %q", step.Run)
			}
		case strings.Contains(step.Run, "ecctl ack nodepool create"):
			nodepoolCreates++
			fields := strings.Fields(step.Run)
			for j, field := range fields {
				if field != "--name" || j+1 >= len(fields) {
					continue
				}
				name := strings.ReplaceAll(fields[j+1], "{{.resource_prefix}}", strings.Repeat("r", 40))
				if got := len([]rune(name)); got > 63 {
					t.Fatalf("nodepool name length = %d, want <= 63: %q", got, name)
				}
			}
		case step.Name == "start task nodepool upgrade":
			startTaskIndex = i
			if step.Capture["nodepool_upgrade_task_id"] != "task_id" || !strings.Contains(step.Run, "--no-wait") {
				t.Fatalf("task upgrade capture = %#v run %q", step.Capture, step.Run)
			}
		case step.Name == "pause task":
			pauseIndex = i
		case step.Name == "resume task":
			resumeIndex = i
		case step.Name == "cancel task":
			cancelIndex = i
		case step.Name == "detach node":
			detachIndex = i
			if !strings.Contains(step.Teardown, "ecctl ecs instance delete {{.attach_instance_id}} --force") {
				t.Fatalf("detached worker has no leak-safe teardown: %q", step.Teardown)
			}
		case step.Name == "attach node":
			attachIndex = i
		case step.Name == "repair nodepool":
			repairIndex = i
		}
		if strings.Contains(step.Run, "ecctl ecs instance create") {
			t.Fatalf("ACK lifecycle creates an ECS prerequisite in case steps: %q", step.Run)
		}
		if strings.Contains(step.Run, "1.35.") || strings.Contains(step.Run, "1.36.") {
			t.Fatalf("step %q hard-codes an ACK version: %q", step.Name, step.Run)
		}
	}
	if clusterCreates != 1 || nodepoolCreates != 2 {
		t.Fatalf("destructive lifecycle creates = cluster %d nodepool %d, want 1 and 2", clusterCreates, nodepoolCreates)
	}
	if !(startTaskIndex < pauseIndex && pauseIndex < resumeIndex && resumeIndex < cancelIndex) {
		t.Fatalf("task sequence indexes = start %d pause %d resume %d cancel %d", startTaskIndex, pauseIndex, resumeIndex, cancelIndex)
	}
	if !(detachIndex < attachIndex && attachIndex < repairIndex) {
		t.Fatalf("node restore sequence indexes = detach %d attach %d repair %d", detachIndex, attachIndex, repairIndex)
	}
	for _, index := range []int{pauseIndex, resumeIndex, cancelIndex} {
		if !strings.Contains(suite.Steps[index].Run, "{{.nodepool_upgrade_task_id}}") {
			t.Fatalf("task step %q does not reuse the same task ID: %q", suite.Steps[index].Name, suite.Steps[index].Run)
		}
	}
}

func TestACKAutoModeResourceCasesDoNotRequireNodepoolParameters(t *testing.T) {
	for _, path := range []string{"addon-lifecycle.yaml", "check-lifecycle.yaml", "instance-lifecycle.yaml"} {
		suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", path))
		if err != nil {
			t.Fatal(err)
		}
		if slicesContain(suite.RequiresParams, "ack.instance_type") {
			t.Fatalf("%s unexpectedly requires a nodepool instance type: %#v", path, suite.RequiresParams)
		}
		constraints := suite.ParameterConstraints.ECS
		if constraints.MinENIQuantity != 0 || constraints.MinENIPrivateIPAddressQuantity != 0 {
			t.Fatalf("%s unexpectedly carries nodepool ENI constraints: %+v", path, constraints)
		}
	}
	check, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "check-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(check.Needs, []string{"ack_shared_cluster"}) {
		t.Fatalf("ACK check must run directly on Auto Mode Pro: %#v", check.Needs)
	}
}

func TestECSCasesDoNotRequireAccountResourceConfiguration(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "cases", "ecs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join("..", "..", "cases", "ecs", entry.Name())
		loaded, loadErr := scenario.Load(path)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if len(loaded.RequiresPrerequisites) != 0 {
			t.Fatalf("%s suite prerequisites = %#v", entry.Name(), loaded.RequiresPrerequisites)
		}
		for _, step := range loaded.Steps {
			if len(step.RequiresPrerequisites) != 0 {
				t.Fatalf("%s step %q prerequisites = %#v", entry.Name(), step.Name, step.RequiresPrerequisites)
			}
			if strings.Contains(step.Run, ".prerequisites.") || (step.Local != nil && (strings.Contains(step.Local.Source, ".prerequisites.") || strings.Contains(step.Local.Destination, ".prerequisites."))) {
				t.Fatalf("%s step %q references account prerequisites", entry.Name(), step.Name)
			}
		}
	}
}

func TestECSImageBuildsItsOwnOSSTransferChain(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ecs", "image-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Execution != scenario.ExecutionDAG {
		t.Fatalf("ECS image execution = %q, want dag", suite.Execution)
	}
	if len(suite.Needs) != 0 {
		t.Fatalf("ECS image suite needs = %#v, want step-level fixture needs", suite.Needs)
	}
	steps := caseStepsByName(t, suite)
	if !strings.Contains(steps["create OSS bucket"].Run, "call oss PutBucket --Bucket {{.oss_bucket_name}}") || !isReplayableTeardown(steps["create OSS bucket"].Teardown) {
		t.Fatalf("OSS bucket lifecycle = run %q teardown %q", steps["create OSS bucket"].Run, steps["create OSS bucket"].Teardown)
	}
	if !strings.Contains(steps["export"].Run, "--oss-bucket {{.oss_bucket_name}}") || !strings.Contains(steps["export"].Run, "--image-format QCOW2") {
		t.Fatalf("export does not target the generated bucket as QCOW2: %q", steps["export"].Run)
	}
	if steps["export"].At != "$.image" {
		t.Fatalf("export assertion root = %q, want $.image", steps["export"].At)
	}
	if steps["list exported object"].Capture["export_object_key"] != "$.response.Contents.Key" {
		t.Fatalf("exported object capture = %#v", steps["list exported object"].Capture)
	}
	if !strings.Contains(steps["download exported archive"].Run, "--File {{.work_dir}}/export.qcow2") || steps["download exported archive"].Capture["import_file"] != "File" {
		t.Fatalf("QCOW2 download/capture = run %q capture %#v", steps["download exported archive"].Run, steps["download exported archive"].Capture)
	}
	if !reflect.DeepEqual(steps["upload import object"].DependsOn, []string{"download exported archive"}) {
		t.Fatalf("QCOW2 upload dependencies = %#v", steps["upload import object"].DependsOn)
	}
	if !reflect.DeepEqual(steps["import"].DependsOn, []string{"upload import object"}) || !strings.Contains(steps["import"].Run, "format=QCOW2") {
		t.Fatalf("import step = depends %#v run %q", steps["import"].DependsOn, steps["import"].Run)
	}
	for _, name := range []string{"list exported object", "download exported archive", "upload import object", "delete import object", "delete exported object", "delete OSS bucket"} {
		if !strings.Contains(steps[name].Run, "call oss") {
			t.Fatalf("OSS step %q run = %q", name, steps[name].Run)
		}
	}
	for _, name := range []string{"list exported object", "upload import object"} {
		if !isReplayableTeardown(steps[name].Teardown) {
			t.Fatalf("OSS step %q teardown is not replayable: %q", name, steps[name].Teardown)
		}
	}
	if !reflect.DeepEqual(steps["delete"].DependsOn, []string{"get", "list", "copy"}) {
		t.Fatalf("ordinary image delete dependencies = %#v", steps["delete"].DependsOn)
	}
	if !reflect.DeepEqual(steps["delete export source image"].DependsOn, []string{"export"}) {
		t.Fatalf("export image delete dependencies = %#v", steps["delete export source image"].DependsOn)
	}
	if !reflect.DeepEqual(steps["delete OSS bucket"].DependsOn, []string{"delete import object", "delete exported object"}) {
		t.Fatalf("OSS bucket delete dependencies = %#v", steps["delete OSS bucket"].DependsOn)
	}
}

func TestECSRenewLifecycleCreatesDisposablePrepaidInstance(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ecs", "instance-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Execution != scenario.ExecutionDAG {
		t.Fatalf("ECS instance execution = %q, want dag", suite.Execution)
	}
	if len(suite.RequiresPrerequisites) != 0 {
		t.Fatalf("ECS instance suite prerequisites = %#v, want none", suite.RequiresPrerequisites)
	}
	steps := caseStepsByName(t, suite)
	for _, step := range suite.Steps {
		if slicesContain(step.RequiresPrerequisites, "ecs.instance_renew") || strings.Contains(step.Run, ".prerequisites.ecs.instance_renew") {
			t.Fatalf("renewal step %q still uses an account-owned instance: prerequisites=%#v run=%q", step.Name, step.RequiresPrerequisites, step.Run)
		}
	}

	create := steps["create renewal instance"]
	for _, want := range []string{
		"ecs instance create",
		"--instance-charge-type PrePaid",
		"--period 1",
		"--period-unit Month",
		"--auto-pay",
		"--tag ecctl-e2e=1",
		"--tag run-id={{.run_id}}",
	} {
		if !strings.Contains(create.Run, want) {
			t.Fatalf("renewal create command missing %q: %q", want, create.Run)
		}
	}
	if !reflect.DeepEqual(create.Needs, []string{"vpc", "vswitch", "security_group", "image"}) {
		t.Fatalf("renewal create needs = %#v", create.Needs)
	}
	if create.Capture["renew_instance_id"] != "id" || !strings.Contains(create.Teardown, "ecs instance delete {{.renew_instance_id}} --force") {
		t.Fatalf("renewal create capture/teardown = %#v / %q", create.Capture, create.Teardown)
	}

	getPrepaid := steps["get renewal instance"]
	if !reflect.DeepEqual(getPrepaid.DependsOn, []string{"create renewal instance"}) {
		t.Fatalf("prepaid read-back dependencies = %#v", getPrepaid.DependsOn)
	}
	// The read-back teardown converts the instance to PostPaid first so the
	// create step's delete --force finalizer can release it (LIFO order).
	if !strings.Contains(getPrepaid.Teardown, "ecs instance update {{.renew_instance_id}} --instance-charge-type PostPaid") {
		t.Fatalf("prepaid read-back lacks conversion finalizer: %q", getPrepaid.Teardown)
	}
	if !reflect.DeepEqual(steps["renew"].DependsOn, []string{"get renewal instance"}) || !strings.Contains(steps["renew"].Run, "renew {{.renew_instance_id}}") {
		t.Fatalf("renew step = depends %#v run %q", steps["renew"].DependsOn, steps["renew"].Run)
	}
	if !reflect.DeepEqual(steps["get renewed instance"].DependsOn, []string{"renew"}) {
		t.Fatalf("renew read-back dependencies = %#v", steps["get renewed instance"].DependsOn)
	}
}

func TestACKPolicyInstanceUsesAllowedReposSchema(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "ack", "instance-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range suite.Steps {
		if !strings.Contains(step.Run, "--parameters") {
			continue
		}
		if !strings.Contains(step.Run, `"repos"`) || strings.Contains(step.Run, "restrictedNamespaces") {
			t.Fatalf("policy step %q does not follow ACKAllowedRepos schema: %q", step.Name, step.Run)
		}
	}
}

func TestLingjunClusterScalingSharesResourceLifecycle(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "lingjun", "cluster-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Execution != scenario.ExecutionDAG {
		t.Fatalf("Lingjun cluster execution = %q, want dag", suite.Execution)
	}
	if len(suite.RequiresPrerequisites) != 0 {
		t.Fatalf("Lingjun cluster suite prerequisites = %#v, want step-level gates", suite.RequiresPrerequisites)
	}
	if !reflect.DeepEqual(suite.RequiresParams, []string{"lingjun.cluster_type", "lingjun.hpn_zone"}) {
		t.Fatalf("Lingjun cluster parameters = %#v", suite.RequiresParams)
	}
	steps := caseStepsByName(t, suite)
	if !reflect.DeepEqual(steps["create"].RequiresPrerequisites, []string{"lingjun.cluster_network"}) {
		t.Fatalf("base create prerequisites = %#v", steps["create"].RequiresPrerequisites)
	}
	if !reflect.DeepEqual(steps["create"].Needs, []string{
		"lingjun_node_group_keypair", "vpc", "security_group", "lingjun_cluster_network_vswitch", "lingjun_cluster_network_eni",
	}) {
		t.Fatalf("base create needs = %#v, want the shared network fixture", steps["create"].Needs)
	}
	for _, variable := range []string{"{{.stack.vpc}}", "{{.stack.security_group}}", "{{.stack.lingjun_cluster_network_vswitch}}"} {
		if !strings.Contains(steps["create"].Run, variable) {
			t.Fatalf("base create must consume %s from the shared stack, got:\n%s", variable, steps["create"].Run)
		}
	}
	if strings.Contains(steps["create"].Run, "cluster_network.vpc_id") || strings.Contains(steps["create"].Run, "cluster_network.vswitch_id") || strings.Contains(steps["create"].Run, "cluster_network.security_group_id") {
		t.Fatalf("base create must not consume fixture-provided network fields from prerequisites:\n%s", steps["create"].Run)
	}
	if !reflect.DeepEqual(steps["create scaling fixture"].RequiresPrerequisites, []string{"lingjun.cluster"}) {
		t.Fatalf("scaling create prerequisites = %#v", steps["create scaling fixture"].RequiresPrerequisites)
	}
	if !reflect.DeepEqual(steps["delete"].DependsOn, []string{"get", "list"}) {
		t.Fatalf("base delete dependencies = %#v", steps["delete"].DependsOn)
	}
	if !reflect.DeepEqual(steps["delete scaling fixture"].DependsOn, []string{"update extend"}) {
		t.Fatalf("scaling delete dependencies = %#v", steps["delete scaling fixture"].DependsOn)
	}
	if !strings.Contains(steps["update shrink"].Run, "--shrink") || !strings.Contains(steps["update extend"].Run, "--extend") {
		t.Fatal("Lingjun scaling lifecycle must retain shrink and extend operations")
	}
}

func TestRGNotificationPrerequisiteOnlyGuardsMutations(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("..", "..", "cases", "rg", "notification-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Execution != scenario.ExecutionDAG {
		t.Fatalf("RG notification execution = %q, want dag", suite.Execution)
	}
	if len(suite.RequiresPrerequisites) != 0 {
		t.Fatalf("RG notification suite prerequisites = %#v, want step-level gates", suite.RequiresPrerequisites)
	}
	steps := caseStepsByName(t, suite)
	if got := steps["get original notification setting"].RequiresPrerequisites; len(got) != 0 {
		t.Fatalf("notification get prerequisites = %#v", got)
	}
	for _, name := range []string{"enable", "disable"} {
		if got := steps[name].RequiresPrerequisites; !reflect.DeepEqual(got, []string{"rg.notification_disabled"}) {
			t.Fatalf("notification step %q prerequisites = %#v", name, got)
		}
	}
	if !reflect.DeepEqual(steps["enable"].DependsOn, []string{"get original notification setting"}) {
		t.Fatalf("notification enable dependencies = %#v", steps["enable"].DependsOn)
	}
	if !reflect.DeepEqual(steps["disable"].DependsOn, []string{"enable"}) {
		t.Fatalf("notification disable dependencies = %#v", steps["disable"].DependsOn)
	}
}

func TestGovernanceResourceGroupNamesUseBoundedPrefix(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "cases", "rg", "group-lifecycle.yaml"),
		filepath.Join("..", "..", "cases", "rg", "policy-lifecycle.yaml"),
		filepath.Join("..", "..", "cases", "rg", "resource-lifecycle.yaml"),
		filepath.Join("..", "..", "cases", "rg", "role-lifecycle.yaml"),
	} {
		suite, err := scenario.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range suite.Steps {
			if !strings.Contains(step.Run, "rg group create") {
				continue
			}
			if !strings.Contains(step.Run, "--name {{.resource_prefix}}") {
				t.Fatalf("%s step %q must use the bounded resource prefix: %q", path, step.Name, step.Run)
			}
		}
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
    resource: vpc/vpc
    requires_prerequisites: [lingjun.cluster]
    run: ecctl vpc create
  - id: cluster
    resource: lingjun/cluster
    needs: [vpc]
    requires_prerequisites: [lingjun.cluster]
    run: ecctl lingjun cluster create
  - id: image
    resource: ecs/image
    mode: lookup
    requires_prerequisites: [test.optional]
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
    resource: vpc/vpc
    run: ecctl vpc create
    teardown: ecctl vpc delete vpc-example
  - id: cluster
    resource: ack/ack
    needs: [vpc]
    run: ecctl ack create
    teardown: ecctl ack delete c-example
  - id: unused
    resource: ecs/instance
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

func TestStackDependenciesBySuiteExposesDirectAndTransitiveResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(`
provision:
  - id: vpc
    resource: vpc/vpc
    run: ecctl vpc create
  - id: cluster
    resource: ack/ack
    needs: [vpc]
    run: ecctl ack create
  - id: image
    resource: ecs/image
    mode: lookup
    run: ecctl call ecs DescribeImages
`), 0o644); err != nil {
		t.Fatal(err)
	}
	suite := &scenario.Suite{Path: "ack/diagnosis.yaml", Needs: []string{"cluster"}}
	got, err := StackDependenciesBySuite(path, []*scenario.Suite{suite})
	if err != nil {
		t.Fatal(err)
	}
	dependency := got[suite.Path]
	if want := []string{"cluster"}; !reflect.DeepEqual(dependency.Direct, want) {
		t.Fatalf("direct needs = %#v, want %#v", dependency.Direct, want)
	}
	if len(dependency.Fixtures) != 2 ||
		dependency.Fixtures[0].ID != "vpc" || dependency.Fixtures[0].Resource != "vpc/vpc" ||
		dependency.Fixtures[1].ID != "cluster" || dependency.Fixtures[1].Resource != "ack/ack" {
		t.Fatalf("fixture closure = %#v", dependency.Fixtures)
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

func TestFixtureRunLifetimeMayOnlyDependOnRunLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(`
provision:
  - id: execution_parent
    resource: vpc/vpc
    run: ecctl vpc create
  - id: run_child
    resource: ack/ack
    lifetime: run
    needs: [execution_parent]
    run: ecctl ack create
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFixture(path); err == nil || !strings.Contains(err.Error(), "run lifetime") {
		t.Fatalf("error = %v, want run-lifetime dependency error", err)
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
