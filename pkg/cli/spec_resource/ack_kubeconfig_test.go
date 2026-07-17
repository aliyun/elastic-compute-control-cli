package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestACKKubeconfigHelpShape(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "kubeconfig", "create", "--help")
	if code != 0 {
		t.Fatalf("ack kubeconfig create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Issue kubeconfig",
		"* --cluster string",
		"--user-id string",
		"--expire-time int",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "kubeconfig", "list", "--help")
	if code != 0 {
		t.Fatalf("ack kubeconfig list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"List kubeconfig states",
		"--cluster string",
		"--scope string",
		"--user-id string",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list help missing %q:\n%s", want, stdout)
		}
	}
}

func TestACKKubeconfigCreateRoutesSelfAndUserID(t *testing.T) {
	t.Parallel()

	t.Run("self", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"config":     "apiVersion: v1\ncurrent-context: self\n",
			"expiration": "2026-06-02T00:00:00Z",
		}}}
		runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
			if resource.Product != "ack" || resource.Resource != "kubeconfig" || resource.APIProduct != "CS" {
				t.Fatalf("resource = %#v, want ack/kubeconfig with CS API product", resource)
			}
			return fake, nil
		})

		stdout, stderr, code := runCLI("ack", "kubeconfig", "create", "--region", "cn-beijing", "--cluster", "c-123", "--expire-time", "60")
		if code != 0 {
			t.Fatalf("ack kubeconfig create exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterUserKubeconfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		request := fake.calls[0].request
		if request["ClusterId"] != "c-123" || request["TemporaryDurationMinutes"] != 60 {
			t.Fatalf("self create request = %#v", request)
		}
		kubeconfig, _ := decodeObject(t, stdout)["kubeconfig"].(map[string]any)
		if kubeconfig == nil || kubeconfig["cluster"] != "c-123" || kubeconfig["config"] != "apiVersion: v1\ncurrent-context: self\n" {
			t.Fatalf("unexpected output: %s", stdout)
		}
	})

	t.Run("user id", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"config":     "apiVersion: v1\ncurrent-context: user\n",
			"expiration": "2026-06-02T00:00:00Z",
		}}}
		runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
			return fake, nil
		})

		stdout, stderr, code := runCLI("ack", "kc", "create", "--region", "cn-beijing", "--cluster", "c-123", "--user-id", "26562443851650")
		if code != 0 {
			t.Fatalf("ack kc create --user-id exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeSubaccountK8sClusterUserConfig" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		request := fake.calls[0].request
		if request["ClusterId"] != "c-123" || request["Uid"] != "26562443851650" {
			t.Fatalf("user-id create request = %#v", request)
		}
		kubeconfig, _ := decodeObject(t, stdout)["kubeconfig"].(map[string]any)
		if kubeconfig == nil || kubeconfig["user_id"] != "26562443851650" || kubeconfig["config"] != "apiVersion: v1\ncurrent-context: user\n" {
			t.Fatalf("unexpected output: %s", stdout)
		}
	})
}

func TestACKKubeconfigListScopeRoutesStateAPI(t *testing.T) {
	t.Parallel()

	t.Run("cluster scope", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"states": []any{map[string]any{
				"account_id":       "26562443851650",
				"account_name":     "alice",
				"account_type":     "RamUser",
				"cert_expire_time": "2026-06-02T00:00:00Z",
				"cert_state":       "Unexpired",
				"revokable":        true,
			}},
			"page": map[string]any{"total_count": float64(1)},
		}}}
		runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
			return fake, nil
		})

		stdout, stderr, code := runCLI("ack", "kubeconfig", "list", "--region", "cn-beijing", "--cluster", "c-123", "--page", "2", "--limit", "25")
		if code != 0 {
			t.Fatalf("ack kubeconfig list exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "ListClusterKubeconfigStates" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		request := fake.calls[0].request
		if request["ClusterId"] != "c-123" || request["pageNumber"] != 2 || request["pageSize"] != 25 {
			t.Fatalf("cluster-scope request = %#v", request)
		}
		if total := decodeObject(t, stdout)["total"]; total != float64(1) {
			t.Fatalf("total = %#v, want 1; stdout=%s", total, stdout)
		}
	})

	t.Run("user scope", func(t *testing.T) {
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"states": []any{map[string]any{
				"cluster_id":       "c-456",
				"cluster_name":     "prod",
				"cluster_state":    "running",
				"cert_expire_time": "2026-06-02T00:00:00Z",
				"cert_state":       "Unexpired",
			}},
			"page": map[string]any{"total_count": float64(1)},
		}}}
		runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
			return fake, nil
		})

		stdout, stderr, code := runCLI("ack", "kubeconfig", "list", "--region", "cn-beijing", "--scope", "user", "--user-id", "26562443851650")
		if code != 0 {
			t.Fatalf("ack kubeconfig list --scope user exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "ListUserKubeConfigStates" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		request := fake.calls[0].request
		if request["Uid"] != "26562443851650" || request["page_number"] != 1 || request["page_size"] != 50 {
			t.Fatalf("user-scope request = %#v", request)
		}
		if _, ok := request["ClusterId"]; ok {
			t.Fatalf("user-scope request should not include ClusterId: %#v", request)
		}
		kubeconfigs, _ := decodeObject(t, stdout)["kubeconfigs"].([]any)
		if len(kubeconfigs) != 1 {
			t.Fatalf("kubeconfigs = %#v; stdout=%s", kubeconfigs, stdout)
		}
	})
}

func TestACKKubeconfigRevokeRoutesCurrentUserAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-revoke"}}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		return fake, nil
	})

	stdout, stderr, code := runCLI("ack", "kubeconfig", "revoke", "--region", "cn-beijing", "--cluster", "c-123")
	if code != 0 {
		t.Fatalf("ack kubeconfig revoke exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "RevokeK8sClusterKubeConfig" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ClusterId"] != "c-123" {
		t.Fatalf("revoke request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	if out["revoked"] != true {
		t.Fatalf("revoked = %#v, want true; stdout=%s", out["revoked"], stdout)
	}
}
