package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestAckPolicyMVPHelpIsGeneratedFromSpec(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "policy", "--help")
	if code != 0 {
		t.Fatalf("ack policy --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"list", "get", "instance"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack policy help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "policy", "instance", "--help")
	if code != 0 {
		t.Fatalf("ack policy instance --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"create", "update", "delete", "get", "list"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack policy instance help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "policy", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("ack policy instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster", "--action", "--namespaces", "--parameters"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack policy instance create help missing %q:\n%s", want, stdout)
		}
	}
}

func TestAckPolicyTopLevelRoutesToPolicyCatalogAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id": "req-list",
			"policies": []any{
				map[string]any{"name": "ACKAllowedRepos", "category": "k8s-general", "severity": "high"},
			},
		},
		{
			"request_id":  "req-detail",
			"name":        "ACKAllowedRepos",
			"category":    "k8s-general",
			"severity":    "high",
			"action":      "deny",
			"description": "Requires images to use allowed repos",
			"template":    "ConstraintTemplate",
		},
	}}
	runCLI := ackPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "policy", "list", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("ack policy list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	stdout, stderr, code = runCLI("ack", "policy", "get", "ACKAllowedRepos", "--region", "cn-shanghai")
	if code != 0 {
		t.Fatalf("ack policy get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "DescribePolicies,DescribePolicyDetails" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if len(fake.calls[0].request) != 0 {
		t.Fatalf("DescribePolicies request = %#v, want empty", fake.calls[0].request)
	}
	if fake.calls[1].request["policy_name"] != "ACKAllowedRepos" {
		t.Fatalf("DescribePolicyDetails request = %#v", fake.calls[1].request)
	}
}

func TestAckPolicyInstanceCRUDAndListRouteToInstanceAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-deploy", "instances": []any{"allowed-repos-1"}},
		{"request_id": "req-status-create", "policy_instances": []any{
			map[string]any{"policy_name": "ACKAllowedRepos", "policy_instances_count": float64(1), "policy_severity": "high"},
		}},
		{"request_id": "req-modify", "instances": []any{"allowed-repos-1"}},
		{"request_id": "req-status-update", "policy_instances": []any{
			map[string]any{"policy_name": "ACKAllowedRepos", "policy_instances_count": float64(1), "policy_severity": "high"},
		}},
		{"request_id": "req-delete", "instances": []any{"allowed-repos-1"}},
		{"request_id": "req-list-after-delete", "instances": []any{}},
		{"request_id": "req-list", "instances": []any{
			map[string]any{"instance_name": "allowed-repos-1", "policy_name": "ACKAllowedRepos", "policy_action": "deny"},
		}},
		{"request_id": "req-status-get", "policy_instances": []any{
			map[string]any{"policy_name": "ACKAllowedRepos", "policy_instances_count": float64(1), "policy_severity": "high"},
		}},
	}}
	runCLI := ackPolicyCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "policy", "instance", "create", "ACKAllowedRepos",
		"--region", "cn-shanghai",
		"--cluster", "c-123",
		"--action", "warn",
		"--namespaces", `["default"]`,
		"--parameters", `{"restrictedNamespaces":["kube-system"]}`,
	)
	if code != 0 {
		t.Fatalf("ack policy instance create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "policy", "instance", "update", "ACKAllowedRepos",
		"--region", "cn-shanghai",
		"--cluster", "c-123",
		"--instance-name", "allowed-repos-1",
		"--action", "deny",
		"--parameters", `{"restrictedNamespaces":["default"]}`,
	)
	if code != 0 {
		t.Fatalf("ack policy instance update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "policy", "instance", "delete", "ACKAllowedRepos",
		"--region", "cn-shanghai",
		"--cluster", "c-123",
		"--instance-name", "allowed-repos-1",
	)
	if code != 0 {
		t.Fatalf("ack policy instance delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "policy", "instance", "list",
		"--region", "cn-shanghai",
		"--cluster", "c-123",
		"--policy-name", "ACKAllowedRepos",
	)
	if code != 0 {
		t.Fatalf("ack policy instance list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "policy", "instance", "get", "ACKAllowedRepos",
		"--region", "cn-shanghai",
		"--cluster", "c-123",
	)
	if code != 0 {
		t.Fatalf("ack policy instance get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	wantCalls := "DeployPolicyInstance,DescribePolicyInstancesStatus,ModifyPolicyInstance,DescribePolicyInstancesStatus,DeletePolicyInstance,DescribePolicyInstances,DescribePolicyInstances,DescribePolicyInstancesStatus"
	if got := callNames(fake.calls); strings.Join(got, ",") != wantCalls {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["cluster_id"] != "c-123" || createReq["policy_name"] != "ACKAllowedRepos" || createReq["action"] != "warn" {
		t.Fatalf("DeployPolicyInstance request = %#v", createReq)
	}
	parameters, _ := createReq["parameters"].(map[string]any)
	if _, ok := parameters["restrictedNamespaces"].([]any); !ok {
		t.Fatalf("DeployPolicyInstance parameters = %#v", createReq["parameters"])
	}
	if fake.calls[2].request["instance_name"] != "allowed-repos-1" || fake.calls[2].request["action"] != "deny" {
		t.Fatalf("ModifyPolicyInstance request = %#v", fake.calls[2].request)
	}
	if fake.calls[4].request["instance_name"] != "allowed-repos-1" {
		t.Fatalf("DeletePolicyInstance request = %#v", fake.calls[4].request)
	}
	if fake.calls[6].request["cluster_id"] != "c-123" || fake.calls[6].request["policy_name"] != "ACKAllowedRepos" {
		t.Fatalf("DescribePolicyInstances request = %#v", fake.calls[6].request)
	}
}

func ackPolicyCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || (resource.Resource != "policy" && resource.Resource != "instance") {
			t.Fatalf("resource = %s/%s, want ack/policy or ack/instance", resource.Product, resource.Resource)
		}
		if resource.Resource == "instance" && resource.Parent != "policy" {
			t.Fatalf("resource parent = %q, want policy", resource.Parent)
		}
		if resource.APIProduct != "CS" {
			t.Fatalf("api_product = %q, want CS", resource.APIProduct)
		}
		if region != "cn-shanghai" {
			t.Fatalf("region = %q, want cn-shanghai", region)
		}
		return fake, nil
	})
}
