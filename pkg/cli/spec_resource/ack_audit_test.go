package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func ackAuditCaller(t *testing.T, fake *fakeSpecCaller, wantResource string, wantParent string) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.APIProduct != "cs" || resource.Resource != wantResource || resource.Parent != wantParent {
			t.Fatalf("resource = product:%s api:%s resource:%s parent:%s, want ack/cs/%s parent %q", resource.Product, resource.APIProduct, resource.Resource, resource.Parent, wantResource, wantParent)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func requireHelpContains(t *testing.T, stdout string, snippets ...string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(stdout, snippet) {
			t.Fatalf("help missing %q:\n%s", snippet, stdout)
		}
	}
}

func TestACKAuditHelpExposesAuditAndControlPlaneLogFlags(t *testing.T) {
	stdout, stderr, code := runCLI("ack", "audit", "update", "--help")
	if code != 0 {
		t.Fatalf("ack audit update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireHelpContains(t, stdout, "Update ACK cluster API Server audit log", "--cluster string", "--enabled", "--project string")

	stdout, stderr, code = runCLI("ack", "audit", "control-plane-log", "update", "--help")
	if code != 0 {
		t.Fatalf("ack audit control-plane-log update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	requireHelpContains(t, stdout, "Update ACK managed cluster control plane component log", "--cluster string", "--enabled", "--components", "--project string")
}

func TestACKAuditUpdateAndGetUseClusterAuditAPIs(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id": "req-update",
			"cluster_id": "c-ack1",
			"task_id":    "task-audit",
		},
		{
			"request_id":       "req-get",
			"audit_enabled":    true,
			"sls_project_name": "k8s-log-c-ack1",
		},
	}}
	runCLI := ackAuditCaller(t, fake, "audit", "")

	stdout, stderr, code := runCLI(
		"ack", "audit", "update",
		"--region", "cn-beijing",
		"--cluster", "c-ack1",
		"--enabled",
		"--project", "k8s-log-c-ack1",
	)
	if code != 0 {
		t.Fatalf("ack audit update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateClusterAuditLogConfig" || fake.calls[1].operation != "GetClusterAuditProject" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["clusterid"] != "c-ack1" || request["sls_project_name"] != "k8s-log-c-ack1" || request["disable"] != false {
		t.Fatalf("UpdateClusterAuditLogConfig request = %#v", request)
	}
	audit, _ := decodeObject(t, stdout)["audit"].(map[string]any)
	if audit == nil || audit["enabled"] != true || audit["project"] != "k8s-log-c-ack1" {
		t.Fatalf("unexpected audit update output: %s", stdout)
	}

	fake = &fakeSpecCaller{responses: []map[string]any{{
		"request_id":       "req-get",
		"audit_enabled":    false,
		"sls_project_name": "k8s-log-c-ack1",
	}}}
	runCLI = ackAuditCaller(t, fake, "audit", "")
	stdout, stderr, code = runCLI("ack", "audit", "get", "--region", "cn-beijing", "--cluster", "c-ack1")
	if code != 0 {
		t.Fatalf("ack audit get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "GetClusterAuditProject" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["clusterid"] != "c-ack1" {
		t.Fatalf("GetClusterAuditProject request = %#v", fake.calls[0].request)
	}
	audit, _ = decodeObject(t, stdout)["audit"].(map[string]any)
	if audit == nil || audit["enabled"] != false || audit["project"] != "k8s-log-c-ack1" {
		t.Fatalf("unexpected audit get output: %s", stdout)
	}
}

func TestACKAuditControlPlaneLogUpdateAndGetUseControlPlaneLogAPIs(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id": "req-update-control-plane",
			"cluster_id": "c-ack1",
			"task_id":    "task-control-plane",
		},
		{
			"request_id":  "req-check-control-plane",
			"log_project": "k8s-log-c-ack1",
			"log_ttl":     float64(30),
			"aliuid":      "1234567890",
			"components":  []any{"apiserver", "kcm"},
		},
	}}
	runCLI := ackAuditCaller(t, fake, "control-plane-log", "audit")

	stdout, stderr, code := runCLI(
		"ack", "audit", "control-plane-log", "update",
		"--region", "cn-beijing",
		"--cluster", "c-ack1",
		"--enabled",
		"--components", "apiserver,kcm",
		"--project", "k8s-log-c-ack1",
		"--log-ttl", "30",
	)
	if code != 0 {
		t.Fatalf("ack audit control-plane-log update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateControlPlaneLog" || fake.calls[1].operation != "CheckControlPlaneLogEnable" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-ack1" || request["log_project"] != "k8s-log-c-ack1" || request["log_ttl"] != 30 {
		t.Fatalf("UpdateControlPlaneLog request = %#v", request)
	}
	components, _ := request["components"].([]string)
	if len(components) != 2 || components[0] != "apiserver" || components[1] != "kcm" {
		t.Fatalf("UpdateControlPlaneLog components = %#v; request=%#v", request["components"], request)
	}
	controlPlaneLog, _ := decodeObject(t, stdout)["control_plane_log"].(map[string]any)
	if controlPlaneLog == nil || controlPlaneLog["project"] != "k8s-log-c-ack1" {
		t.Fatalf("unexpected control-plane-log update output: %s", stdout)
	}

	fake = &fakeSpecCaller{responses: []map[string]any{{
		"request_id":  "req-check-control-plane",
		"log_project": "k8s-log-c-ack1",
		"log_ttl":     float64(30),
		"aliuid":      "1234567890",
		"components":  []any{"apiserver", "scheduler"},
	}}}
	runCLI = ackAuditCaller(t, fake, "control-plane-log", "audit")
	stdout, stderr, code = runCLI("ack", "audit", "control-plane-log", "get", "--region", "cn-beijing", "--cluster", "c-ack1")
	if code != 0 {
		t.Fatalf("ack audit control-plane-log get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CheckControlPlaneLogEnable" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ClusterId"] != "c-ack1" {
		t.Fatalf("CheckControlPlaneLogEnable request = %#v", fake.calls[0].request)
	}
	controlPlaneLog, _ = decodeObject(t, stdout)["control_plane_log"].(map[string]any)
	if controlPlaneLog == nil || controlPlaneLog["project"] != "k8s-log-c-ack1" {
		t.Fatalf("unexpected control-plane-log get output: %s", stdout)
	}
}

func TestACKAuditControlPlaneLogDisableSendsEmptyComponents(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-disable-control-plane"},
		{
			"request_id": "req-check-control-plane",
			"components": []any{},
		},
	}}
	runCLI := ackAuditCaller(t, fake, "control-plane-log", "audit")

	stdout, stderr, code := runCLI(
		"ack", "audit", "control-plane-log", "update",
		"--region", "cn-beijing",
		"--cluster", "c-ack1",
		"--enabled=false",
	)
	if code != 0 {
		t.Fatalf("ack audit control-plane-log disable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateControlPlaneLog" || fake.calls[1].operation != "CheckControlPlaneLogEnable" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ClusterId"] != "c-ack1" {
		t.Fatalf("UpdateControlPlaneLog request = %#v", request)
	}
	components, ok := request["components"].([]string)
	if !ok || len(components) != 0 {
		t.Fatalf("disable components = %#v, want empty []string; request=%#v", request["components"], request)
	}
}
