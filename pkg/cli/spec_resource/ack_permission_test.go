package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestACKPermissionUpdateAPIRouting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		wantCalls string
		want      map[string]any
		forbid    string
	}{
		{
			name: "incremental update",
			args: []string{
				"--user-id", "2088",
				"--permission", "cluster=c-123,role-type=cluster,role-name=dev",
			},
			wantCalls: "UpdateUserPermissions,DescribeUserPermission",
			want: map[string]any{
				"uid":              "2088",
				"mode":             "patch",
				"body.1.cluster":   "c-123",
				"body.1.role_type": "cluster",
				"body.1.role_name": "dev",
			},
		},
		{
			name: "replace",
			args: []string{
				"--user-id", "2088",
				"--replace",
				"--permission", `{"cluster":"c-123","role_type":"cluster","role_name":"ops","is_custom":false}`,
			},
			wantCalls: "GrantPermissions,DescribeUserPermission",
			want: map[string]any{
				"uid":              "2088",
				"body.1.cluster":   "c-123",
				"body.1.role_type": "cluster",
				"body.1.role_name": "ops",
				"body.1.is_custom": false,
			},
			forbid: "mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{
				responses: []map[string]any{
					{"RequestId": "req-mutate"},
					fakeACKUserPermissionResponse("req-describe"),
				},
			}
			runCLI := withACKPermissionCaller(t, fake)
			args := append([]string{"ack", "permission", "update", "--region", "cn-beijing"}, tt.args...)

			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ack permission update exit %d stderr=%s stdout=%s", code, stderr, stdout)
			}
			if got := ackCallNames(fake.calls); strings.Join(got, ",") != tt.wantCalls {
				t.Fatalf("calls = %#v", fake.calls)
			}
			request := fake.calls[0].request
			for key, want := range tt.want {
				if got := request[key]; got != want {
					t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
				}
			}
			if tt.forbid != "" {
				if _, ok := request[tt.forbid]; ok {
					t.Fatalf("request must not include %s: %#v", tt.forbid, request)
				}
			}
		})
	}
}

func TestACKPermissionDeleteAPIRouting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		wantCalls string
		want      map[string]any
		forbid    []string
	}{
		{
			name:      "single cluster",
			args:      []string{"--user-id", "2088", "--cluster", "c-123"},
			wantCalls: "CleanClusterUserPermissions,DescribeUserPermission",
			want: map[string]any{
				"Uid":       "2088",
				"ClusterId": "c-123",
				"Force":     false,
			},
		},
		{
			name:      "all clusters",
			args:      []string{"--user-id", "2088", "--all-clusters", "--force"},
			wantCalls: "CleanUserPermissions,DescribeUserPermission",
			want: map[string]any{
				"Uid":   "2088",
				"Force": true,
			},
			forbid: []string{"ClusterId", "ClusterIds"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{
				responses: []map[string]any{
					{"RequestId": "req-clean"},
					fakeACKUserPermissionResponse("req-describe"),
				},
			}
			runCLI := withACKPermissionCaller(t, fake)
			args := append([]string{"ack", "permission", "delete", "--region", "cn-beijing"}, tt.args...)

			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("ack permission delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
			}
			if got := ackCallNames(fake.calls); strings.Join(got, ",") != tt.wantCalls {
				t.Fatalf("calls = %#v", fake.calls)
			}
			request := fake.calls[0].request
			for key, want := range tt.want {
				if got := request[key]; got != want {
					t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
				}
			}
			for _, key := range tt.forbid {
				if _, ok := request[key]; ok {
					t.Fatalf("request must not include %s: %#v", key, request)
				}
			}
		})
	}
}

func TestACKPermissionHelpShape(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ack", "permission", "update", "--help")
	if code != 0 {
		t.Fatalf("ack permission update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Resource Flags (* required):",
		"* --user-id string",
		"* --permission stringArray",
		"--replace",
		"inline key=value, JSON object, or @file",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("update help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "permission", "delete", "--help")
	if code != 0 {
		t.Fatalf("ack permission delete --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"* --user-id string", "--cluster string", "--all-clusters", "--force"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("delete help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ack.permission.update")
	if code != 0 {
		t.Fatalf("schema ack.permission.update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	permission, _ := params["permission"].(map[string]any)
	if permission == nil || permission["input"] != "inline-key-value|json|@file" {
		t.Fatalf("permission schema = %#v; stdout=%s", permission, stdout)
	}
	fields, _ := permission["fields"].(map[string]any)
	for _, name := range []string{"cluster", "role_type", "role_name", "namespace", "is_custom", "is_ram_role"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("permission schema missing field %s: %s", name, stdout)
		}
	}
}

func withACKPermissionCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "permission" {
			t.Fatalf("resource = %s/%s, want ack/permission", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func fakeACKUserPermissionResponse(requestID string) map[string]any {
	return map[string]any{
		"RequestId": requestID,
		"body": []any{
			map[string]any{
				"resource_id":   "c-123",
				"resource_type": "cluster",
				"role_name":     "dev",
				"role_type":     "cluster",
			},
		},
	}
}

func ackCallNames(calls []fakeSpecCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.operation)
	}
	return names
}
