package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestACKClusterDefaultResourceHelp(t *testing.T) {
	t.Parallel()

	checks := []struct {
		args []string
		want []string
	}{
		{[]string{"ack", "create", "--help"}, []string{"Create ACK cluster", "--name string", "--type string", "--profile string", "--vswitch strings", "--tag stringArray", "--no-wait", "--timeout duration"}},
		{[]string{"ack", "get", "--help"}, []string{"Get ACK cluster", "--with-resources", "--with-tags", "--with-policy-governance"}},
		{[]string{"ack", "list", "--help"}, []string{"List ACK clusters", "--cross-account", "--filter", "--limit int"}},
		{[]string{"ack", "update", "--help"}, []string{"Update ACK cluster", "--api-server-eip-id string", "--to-edition string", "--tag stringArray", "--remove-tag strings", "--tag-replace stringArray"}},
		{[]string{"ack", "delete", "--help"}, []string{"Delete ACK cluster", "--force", "--no-wait", "--timeout duration"}},
		{[]string{"ack", "upgrade", "--help"}, []string{"Upgrade ACK cluster", "--version string", "--no-wait", "--timeout duration"}},
		{[]string{"ack", "cluster", "create", "--help"}, []string{"Create ACK cluster", "--name string", "--type string", "--profile string", "--vswitch strings", "--tag stringArray", "--no-wait", "--timeout duration"}},
		{[]string{"ack", "cluster", "get", "--help"}, []string{"Get ACK cluster", "--with-resources", "--with-tags", "--with-policy-governance"}},
		{[]string{"ack", "cluster", "list", "--help"}, []string{"List ACK clusters", "--cross-account", "--filter", "--limit int"}},
	}
	for _, check := range checks {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, check.args...)...)
		if code != 0 {
			t.Fatalf("%v exit %d stderr=%s stdout=%s", check.args, code, stderr, stdout)
		}
		for _, want := range check.want {
			if !strings.Contains(stdout, want) {
				t.Fatalf("%v help missing %q:\n%s", check.args, want, stdout)
			}
		}
	}
}

func TestACKClusterCreateUsesDirectDefaultResource(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-create",
		"cluster_id": "c-123",
		"task_id":    "T-create",
	}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "create",
		"--region", "cn-beijing",
		"--name", "prod",
		"--type", "ManagedKubernetes",
		"--version", "1.35.2-aliyun.1",
		"--vswitch", "vsw-1",
		"--vswitch", "vsw-2",
		"--pod-cidr", "172.20.0.0/16",
		"--service-cidr", "172.21.0.0/20",
		"--resource-group", "rg-123",
		"--profile", "Default",
		"--api-server-public",
		"--snat-entry",
		"--zone", "cn-beijing-a",
		"--zone", "cn-beijing-b",
		"--tag", "env=prod",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"CreateCluster"})
	request := fake.calls[0].request
	if request["body.name"] != "prod" || request["body.cluster_type"] != "ManagedKubernetes" || request["body.kubernetes_version"] != "1.35.2-aliyun.1" || request["body.resource_group_id"] != "rg-123" || request["body.region_id"] != "cn-beijing" {
		t.Fatalf("CreateCluster request = %#v", request)
	}
	if request["body.profile"] != "Default" || request["body.endpoint_public_access"] != true || request["body.snat_entry"] != true {
		t.Fatalf("CreateCluster app-ready request = %#v", request)
	}
	if request["body.container_cidr"] != "172.20.0.0/16" || request["body.service_cidr"] != "172.21.0.0/20" {
		t.Fatalf("CreateCluster request = %#v", request)
	}
	requireStringValues(t, request["body.vswitch_ids"], []string{"vsw-1", "vsw-2"})
	requireStringValues(t, request["body.zone_ids"], []string{"cn-beijing-a", "cn-beijing-b"})
	createTags, _ := request["body.tags"].([]map[string]string)
	if len(createTags) != 1 || createTags[0]["key"] != "env" || createTags[0]["value"] != "prod" {
		t.Fatalf("CreateCluster body.tags = %#v; request = %#v", request["body.tags"], request)
	}
	cluster, _ := decodeObject(t, stdout)["cluster"].(map[string]any)
	if cluster == nil || cluster["id"] != "c-123" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestACKClusterCreateUsesClusterAlias(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-create",
		"cluster_id": "c-123",
	}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "cluster", "create",
		"--region", "cn-beijing",
		"--name", "prod",
		"--type", "ManagedKubernetes",
		"--edition", "ack.pro.small",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack cluster create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"CreateCluster"})
	request := fake.calls[0].request
	if request["body.name"] != "prod" || request["body.cluster_type"] != "ManagedKubernetes" || request["body.cluster_spec"] != "ack.pro.small" {
		t.Fatalf("CreateCluster request = %#v", request)
	}
}

func TestACKClusterCreateProfileDoesNotSelectConfigProfile(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"ack", "create"},
		{"ack", "cluster", "create"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{{
				"request_id": "req-create",
				"cluster_id": "c-123",
			}}}
			runCLI := withCaller(func(profileName string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ack" || resource.Resource != "ack" {
					t.Fatalf("resource = %s/%s, want ack/ack", resource.Product, resource.Resource)
				}
				if profileName != "" {
					t.Fatalf("config profile = %q, want empty; --profile after %s must be the ACK CreateCluster profile", profileName, strings.Join(args, " "))
				}
				if region != "cn-beijing" {
					t.Fatalf("region = %q, want cn-beijing", region)
				}
				return fake, nil
			})

			stdout, stderr, code := runCLI(append(args,
				"--region", "cn-beijing",
				"--name", "prod",
				"--type", "ManagedKubernetes",
				"--profile", "Default",
				"--no-wait",
			)...)
			if code != 0 {
				t.Fatalf("%s --profile exit %d stderr=%s stdout=%s", strings.Join(args, " "), code, stderr, stdout)
			}
			requireACKOperations(t, fake.calls, []string{"CreateCluster"})
			if fake.calls[0].request["body.profile"] != "Default" {
				t.Fatalf("CreateCluster request = %#v", fake.calls[0].request)
			}
		})
	}
}

func TestACKClusterCreateAllowsLingjunProfile(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-create",
		"cluster_id": "c-lingjun",
	}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "create",
		"--region", "cn-beijing",
		"--name", "lingjun-prod",
		"--type", "ManagedKubernetes",
		"--profile", "Lingjun",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack create --profile Lingjun exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"CreateCluster"})
	if fake.calls[0].request["body.profile"] != "Lingjun" {
		t.Fatalf("CreateCluster request = %#v", fake.calls[0].request)
	}
}

func TestACKClusterGetMergesRequestedViews(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeACKClusterDetail("c-123", "prod", "running"),
		{
			"__ecctl_top_level_array_response": true,
			"items": []any{
				map[string]any{"resource_id": "vpc-1", "resource_type": "VPC"},
			},
		},
		{
			"request_id": "req-tags",
			"tag_resources": map[string]any{"tag_resource": []any{
				map[string]any{"resource_id": "c-123", "resource_type": "CLUSTER", "tag_key": "env", "tag_value": "prod"},
			}},
		},
		{
			"request_id": "req-policy",
			"enabled":    true,
			"violations": float64(2),
		},
	}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "get", "c-123",
		"--region", "cn-beijing",
		"--with-resources",
		"--with-tags",
		"--with-policy-governance",
	)
	if code != 0 {
		t.Fatalf("ack get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{
		"DescribeClusterDetail",
		"DescribeClusterResources",
		"ListTagResources",
		"DescribePolicyGovernanceInCluster",
	})
	if fake.calls[0].request["ClusterId"] != "c-123" || fake.calls[1].request["ClusterId"] != "c-123" {
		t.Fatalf("cluster detail/resource requests = %#v", fake.calls)
	}
	if fake.calls[2].request["region_id"] != "cn-beijing" || fake.calls[2].request["resource_type"] != "CLUSTER" {
		t.Fatalf("ListTagResources request = %#v", fake.calls[2].request)
	}
	requireStringValues(t, fake.calls[2].request["resource_ids"], []string{"c-123"})
	if fake.calls[3].request["cluster_id"] != "c-123" {
		t.Fatalf("DescribePolicyGovernanceInCluster request = %#v", fake.calls[3].request)
	}
	cluster, _ := decodeObject(t, stdout)["cluster"].(map[string]any)
	if cluster == nil || cluster["id"] != "c-123" || cluster["resources"] == nil || cluster["tags"] == nil || cluster["policy_governance"] == nil {
		t.Fatalf("unexpected get output: %s", stdout)
	}
	resources, ok := cluster["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("cluster resources = %#v, want one resource", cluster["resources"])
	}
	resource, ok := resources[0].(map[string]any)
	if !ok || resource["resource_id"] != "vpc-1" || resource["resource_type"] != "VPC" {
		t.Fatalf("cluster resources = %#v, want preserved VPC resource", resources)
	}
}

func TestACKClusterListCrossAccountUsesForRegionAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"request_id": "req-list",
		"clusters": []any{
			map[string]any{"cluster_id": "c-123", "name": "prod", "state": "running"},
		},
		"page_info": map[string]any{"total_count": float64(1), "page_number": float64(1), "page_size": float64(20)},
	}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "list", "--region", "cn-beijing", "--filter", "name=prod", "--limit", "20", "--cross-account")
	if code != 0 {
		t.Fatalf("ack list --cross-account exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"DescribeClustersForRegion"})
	request := fake.calls[0].request
	if request["region_id"] != "cn-beijing" || request["name"] != "prod" || request["page_size"] != 20 || request["page_number"] != 1 {
		t.Fatalf("DescribeClustersForRegion request = %#v", request)
	}
	if out := decodeObject(t, stdout); out["total"] != float64(1) {
		t.Fatalf("unexpected list output: %s", stdout)
	}
}

func TestACKClusterUpdateToEditionUsesMigrateCluster(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-migrate", "cluster_id": "c-123", "task_id": "T-migrate"}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "update", "c-123", "--region", "cn-beijing", "--to-edition", "ack.pro.small", "--no-wait")
	if code != 0 {
		t.Fatalf("ack update --to-edition exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"MigrateCluster"})
	if fake.calls[0].request["cluster_id"] != "c-123" || fake.calls[0].request["body.cluster_spec"] != "ack.pro.small" {
		t.Fatalf("MigrateCluster request = %#v", fake.calls[0].request)
	}
}

func TestACKClusterCreateEditionAllowsForwardCompatibleEdition(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-create", "cluster_id": "c-123"}}}
	runCLI := ackClusterCaller(t, fake)

	futureEdition := "ack.pro.future-region"
	stdout, stderr, code := runCLI("ack", "create",
		"--region", "cn-beijing",
		"--name", "prod",
		"--type", "ManagedKubernetes",
		"--edition", futureEdition,
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack create with forward-compatible edition exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateCluster" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["body.cluster_spec"] != futureEdition {
		t.Fatalf("body.cluster_spec = %#v, want %q; request=%#v", fake.calls[0].request["body.cluster_spec"], futureEdition, fake.calls[0].request)
	}
}

func TestACKClusterUpdateAPIServerPublicEIPIDMapsBody(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-modify", "task_id": "T-modify"}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "update", "c-123",
		"--region", "cn-beijing",
		"--api-server-public",
		"--api-server-eip-id", "eip-123",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack update --api-server-public --api-server-eip-id exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"ModifyCluster"})
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" || request["body.api_server_eip"] != true || request["body.api_server_eip_id"] != "eip-123" {
		t.Fatalf("ModifyCluster request = %#v", request)
	}
}

func TestACKClusterUpdateTagsUsesTagResourcesBody(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-tags"}}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "update", "c-123", "--region", "cn-beijing", "--tag", "env=prod", "--tag", "team=platform", "--no-wait")
	if code != 0 {
		t.Fatalf("ack update --tag exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{"TagResources"})
	request := fake.calls[0].request
	if request["body.region_id"] != "cn-beijing" || request["body.resource_type"] != "CLUSTER" {
		t.Fatalf("TagResources request = %#v", request)
	}
	requireStringValues(t, request["body.resource_ids"], []string{"c-123"})
	tags, _ := request["body.tags"].([]map[string]string)
	if len(tags) != 2 || tags[0]["key"] != "env" || tags[0]["value"] != "prod" || tags[1]["key"] != "team" || tags[1]["value"] != "platform" {
		t.Fatalf("TagResources body.tags = %#v", request["body.tags"])
	}
}

func TestACKClusterUpgradeMapsRequestBody(t *testing.T) {
	t.Parallel()
	clusterUpgrading := fakeACKClusterDetail("c-123", "prod", "upgrading")
	clusterUpgrading["current_version"] = "1.36.1-aliyun.1"
	clusterDetail := fakeACKClusterDetail("c-123", "prod", "running")
	clusterDetail["current_version"] = "1.36.1-aliyun.1"
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id": "req-upgrade",
			"cluster_id": "c-123",
			"task_id":    "T-upgrade",
		},
		{
			"request_id": "req-task",
			"task_id":    "T-upgrade",
			"state":      "success",
			"task_type":  "cluster_upgrade",
		},
		clusterUpgrading,
		clusterDetail,
	}}
	runCLI := ackClusterCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "upgrade", "c-123",
		"--region", "cn-beijing",
		"--version", "1.36.1-aliyun.1",
		"--master-only",
	)
	if code != 0 {
		t.Fatalf("ack upgrade exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireACKOperations(t, fake.calls, []string{
		"UpgradeCluster",
		"DescribeTaskInfo",
		"DescribeClusterDetail",
		"DescribeClusterDetail",
	})
	request := fake.calls[0].request
	if request["ClusterId"] != "c-123" ||
		request["body.next_version"] != "1.36.1-aliyun.1" ||
		request["body.master_only"] != true {
		t.Fatalf("UpgradeCluster request = %#v", request)
	}
	if _, ok := request["next_version"]; ok {
		t.Fatalf("next_version must be nested in the request body: %#v", request)
	}
	if _, ok := request["master_only"]; ok {
		t.Fatalf("master_only must be nested in the request body: %#v", request)
	}
	if fake.calls[1].request["task_id"] != "T-upgrade" {
		t.Fatalf("DescribeTaskInfo request = %#v", fake.calls[1].request)
	}
}

func ackClusterCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "ack" {
			t.Fatalf("resource = %s/%s, want ack/ack", resource.Product, resource.Resource)
		}
		if resource.APIProduct != "cs" {
			t.Fatalf("api_product = %q, want cs", resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		for name, waiter := range resource.Waiters {
			waiter.Interval = "1ms"
			resource.Waiters[name] = waiter
		}
		return fake, nil
	})
}

func fakeACKClusterDetail(id string, name string, state string) map[string]any {
	return map[string]any{
		"request_id":      "req-detail",
		"cluster_id":      id,
		"name":            name,
		"state":           state,
		"cluster_type":    "ManagedKubernetes",
		"cluster_spec":    "ack.pro.small",
		"current_version": "1.30.1-aliyun.1",
		"region_id":       "cn-beijing",
	}
}

func requireACKOperations(t *testing.T, calls []fakeSpecCall, want []string) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want operations %v", calls, want)
	}
	for i, operation := range want {
		if calls[i].operation != operation {
			t.Fatalf("call %d operation = %s, want %s; calls=%#v", i, calls[i].operation, operation, calls)
		}
	}
}
