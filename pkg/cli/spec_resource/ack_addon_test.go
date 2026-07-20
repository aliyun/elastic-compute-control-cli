package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func ackAddonCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "addon" {
			t.Fatalf("resource = %s/%s, want ack/addon", resource.Product, resource.Resource)
		}
		return fake, nil
	})
}

func TestACKAddonHelpShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "create",
			args: []string{"ack", "addon", "create", "--help"},
			want: []string{"create <name>", "--cluster", "--version", "--config", "--no-wait", "--timeout"},
		},
		{
			name: "delete",
			args: []string{"ack", "addon", "delete", "--help"},
			want: []string{"delete [names...]", "--cluster", "--force", "--no-wait", "--timeout"},
		},
		{
			name: "get",
			args: []string{"ack", "addon", "get", "--help"},
			want: []string{"get <name>", "--cluster", "--catalog", "--with-resources", "--version"},
		},
		{
			name: "list",
			args: []string{"ack", "addon", "list", "--help"},
			want: []string{"--cluster", "--catalog", "--cluster-type", "--cluster-version", "--cluster-spec", "--cluster-profile"},
		},
		{
			name: "upgrade",
			args: []string{"ack", "addon", "upgrade", "--help"},
			want: []string{"upgrade <name>", "--cluster", "--version", "--config", "--no-wait", "--timeout"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%s exit %d stderr=%s stdout=%s", strings.Join(tt.args, " "), code, stderr, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("%s help missing %q:\n%s", tt.name, want, stdout)
				}
			}
		})
	}
}

func TestACKAddonCreateRoutesToInstallAndReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-install", "task_id": "task-install"},
		{"name": "coredns", "version": "v1.10.0", "state": "active", "config": `{"replicas":2}`},
	}}
	runCLI := ackAddonCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "addon", "create", "coredns", "--region", "cn-hangzhou",
		"--cluster", "c-123", "--version", "v1.10.0", "--config", `{"replicas":2}`)
	if code != 0 {
		t.Fatalf("ack addon create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "InstallClusterAddons" || fake.calls[1].operation != "GetClusterAddonInstance" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"ClusterId":      "c-123",
		"body.1.name":    "coredns",
		"body.1.version": "v1.10.0",
		"body.1.config":  `{"replicas":2}`,
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	readback := fake.calls[1].request
	if readback["cluster_id"] != "c-123" || readback["instance_name"] != "coredns" {
		t.Fatalf("readback request = %#v", readback)
	}
	addon, _ := decodeObject(t, stdout)["addon"].(map[string]any)
	if addon == nil || addon["name"] != "coredns" || addon["state"] != "active" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestACKAddonGetRoutesToInstanceAndOptionalResources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"name": "coredns", "version": "v1.10.0", "state": "active"},
		{"resources": []any{map[string]any{"kind": "Deployment", "name": "coredns"}}},
	}}
	runCLI := ackAddonCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "addon", "get", "coredns", "--region", "cn-hangzhou", "--cluster", "c-123", "--with-resources")
	if code != 0 {
		t.Fatalf("ack addon get --with-resources exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "GetClusterAddonInstance" || fake.calls[1].operation != "ListClusterAddonInstanceResources" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	for _, call := range fake.calls {
		if call.request["cluster_id"] != "c-123" || call.request["instance_name"] != "coredns" {
			t.Fatalf("%s request = %#v", call.operation, call.request)
		}
	}
	addon, _ := decodeObject(t, stdout)["addon"].(map[string]any)
	resources, _ := addon["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resources = %#v; stdout=%s", addon["resources"], stdout)
	}
}

func TestACKAddonGetInstanceRequiresCluster(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := ackAddonCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "addon", "get", "coredns", "--region", "cn-hangzhou")
	if code == 0 {
		t.Fatalf("ack addon get without cluster succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("missing cluster should fail before API call: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "--cluster") {
		t.Fatalf("error missing --cluster: %s", message)
	}
}

func TestACKAddonListInstancesRequiresCluster(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := ackAddonCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "addon", "list", "--region", "cn-hangzhou")
	if code == 0 {
		t.Fatalf("ack addon list without cluster succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("missing cluster should fail before API call: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "--cluster") {
		t.Fatalf("error missing --cluster: %s", message)
	}
}

func TestACKAddonCatalogFlagRoutesGetAndListToCatalogAPIs(t *testing.T) {
	t.Parallel()

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"name": "coredns", "version": "v1.10.0", "config_schema": "{}",
		}}}
		runCLI := ackAddonCaller(t, fake)

		stdout, stderr, code := runCLI("ack", "addon", "get", "coredns", "--region", "cn-hangzhou", "--catalog",
			"--cluster-type", "ManagedKubernetes", "--cluster-version", "1.28.3-aliyun.1", "--cluster-spec", "ack.pro.small", "--cluster-profile", "Default")
		if code != 0 {
			t.Fatalf("ack addon get --catalog exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeAddon" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		request := fake.calls[0].request
		if request["addon_name"] != "coredns" || request["cluster_type"] != "ManagedKubernetes" || request["profile"] != "Default" || request["region_id"] != "cn-hangzhou" {
			t.Fatalf("DescribeAddon request = %#v", request)
		}
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{responses: []map[string]any{{
			"addons": []any{map[string]any{"name": "coredns", "version": "v1.10.0"}},
		}}}
		runCLI := ackAddonCaller(t, fake)

		stdout, stderr, code := runCLI("ack", "addon", "list", "--region", "cn-hangzhou", "--catalog",
			"--cluster-type", "ManagedKubernetes", "--cluster-version", "1.28.3-aliyun.1", "--cluster-spec", "ack.pro.small", "--cluster-profile", "Default")
		if code != 0 {
			t.Fatalf("ack addon list --catalog exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if len(fake.calls) != 1 || fake.calls[0].operation != "ListAddons" {
			t.Fatalf("calls = %#v", fake.calls)
		}
		if fake.calls[0].request["cluster_type"] != "ManagedKubernetes" || fake.calls[0].request["profile"] != "Default" || fake.calls[0].request["region_id"] != "cn-hangzhou" {
			t.Fatalf("ListAddons request = %#v", fake.calls[0].request)
		}
		addons, _ := decodeObject(t, stdout)["addons"].([]any)
		if len(addons) != 1 {
			t.Fatalf("addons = %#v; stdout=%s", addons, stdout)
		}
	})
}

func TestACKAddonCatalogRoutesRequireCatalogShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "get",
			args: []string{"ack", "addon", "get", "coredns", "--region", "cn-hangzhou", "--catalog"},
		},
		{
			name: "list",
			args: []string{"ack", "addon", "list", "--region", "cn-hangzhou", "--catalog"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{}
			runCLI := ackAddonCaller(t, fake)

			stdout, stderr, code := runCLI(tt.args...)
			if code == 0 {
				t.Fatalf("%s succeeded stdout=%s stderr=%s", strings.Join(tt.args, " "), stdout, stderr)
			}
			if len(fake.calls) != 0 {
				t.Fatalf("missing catalog shape should fail before API call: %#v", fake.calls)
			}
			if got := errorCode(t, stdout); got != "MissingParameter" {
				t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
			}
			message := errorMessage(t, stdout)
			for _, want := range []string{"--cluster-type", "--cluster-version", "--cluster-spec", "--cluster-profile"} {
				if !strings.Contains(message, want) {
					t.Fatalf("error missing %q: %s", want, message)
				}
			}
		})
	}
}

func TestACKAddonCatalogWithResourcesConflicts(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := ackAddonCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "addon", "get", "coredns", "--region", "cn-hangzhou", "--catalog", "--with-resources",
		"--cluster-type", "ManagedKubernetes", "--cluster-version", "1.28.3-aliyun.1", "--cluster-spec", "ack.pro.small", "--cluster-profile", "Default")
	if code == 0 {
		t.Fatalf("ack addon get --catalog --with-resources succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API call: %#v", fake.calls)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "--catalog") || !strings.Contains(message, "--with-resources") {
		t.Fatalf("error missing conflict flags: %s", message)
	}
}
