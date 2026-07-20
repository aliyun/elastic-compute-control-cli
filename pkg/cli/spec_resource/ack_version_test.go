package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func ackVersionCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "version" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api_product=%s, want ack/version api_product=CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		return fake, nil
	})
}

func TestACKVersionListHelpShowsClusterTypeFilters(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "version", "list", "--help")
	if code != 0 {
		t.Fatalf("ack version list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"--cluster-type",
		"--filter",
		"cluster-type",
		"kubernetes-version",
		"runtime",
		"query-upgradable-version",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q:\n%s", want, stdout)
		}
	}
	for _, forbidden := range []string{"--limit", "--page", "--next-token"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("non-paginated version list help must not show %q:\n%s", forbidden, stdout)
		}
	}
}

func TestACKVersionListRoutesToDescribeKubernetesVersionMetadata(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"versions": []any{map[string]any{
			"version":             "1.31.1-aliyun.1",
			"release_date":        "2025-01-01T00:00:00Z",
			"expiration_date":     "2026-01-01T00:00:00Z",
			"creatable":           true,
			"upgradable_versions": []any{"1.32.0-aliyun.1"},
			"meta_data":           map[string]any{"SubClass": "default"},
			"runtimes":            []any{map[string]any{"name": "containerd", "version": "1.6.28"}},
		}},
	}}}
	runCLI := ackVersionCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "version", "list",
		"--region", "cn-beijing",
		"--filter", "cluster-type=ManagedKubernetes",
		"--filter", "kubernetes-version=1.31.1-aliyun.1",
		"--runtime", "containerd",
		"--query-upgradable-version",
	)
	if code != 0 {
		t.Fatalf("ack version list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeKubernetesVersionMetadata" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"Region":                 "cn-beijing",
		"ClusterType":            "ManagedKubernetes",
		"KubernetesVersion":      "1.31.1-aliyun.1",
		"runtime":                "containerd",
		"QueryUpgradableVersion": true,
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	out := decodeObject(t, stdout)
	versions, _ := out["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions = %#v; stdout=%s", versions, stdout)
	}
	first, _ := versions[0].(map[string]any)
	runtimes, _ := first["runtimes"].([]any)
	runtime, _ := runtimes[0].(map[string]any)
	if first["id"] != "1.31.1-aliyun.1" || runtime["name"] != "containerd" {
		t.Fatalf("unexpected version output: %#v; stdout=%s", first, stdout)
	}
}

func TestACKVersionListRejectsFilterThatDuplicatesExplicitFlag(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := ackVersionCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "version", "list",
		"--region", "cn-beijing",
		"--cluster-type", "Kubernetes",
		"--filter", "cluster-type=ManagedKubernetes",
	)
	if code == 0 {
		t.Fatalf("ack version list duplicate filter succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("duplicate filter should fail before API call: %#v", fake.calls)
	}
	message := errorMessage(t, stdout)
	for _, want := range []string{"--cluster-type", "--filter"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error missing %q: %s", want, message)
		}
	}
}
