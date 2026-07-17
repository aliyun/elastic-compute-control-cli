package spec_resource

import (
	"strings"
	"testing"
)

func TestACKVulsHelpExposesCreateAndListControls(t *testing.T) {
	stdout, stderr, code := runCLI("ack", "vuls", "--help")
	if code != 0 {
		t.Fatalf("ack vuls --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "create") || !strings.Contains(stdout, "list") {
		t.Fatalf("ack vuls help must expose create/list actions:\n%s", stdout)
	}
	if strings.Contains(stdout, "\n  scan") {
		t.Fatalf("ack vuls help must not expose scan action:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("ack", "vuls", "create", "--help")
	if code != 0 {
		t.Fatalf("ack vuls create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster string", "--no-wait", "--timeout duration"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("ack", "vuls", "list", "--help")
	if code != 0 {
		t.Fatalf("ack vuls list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster string", "--nodepool string"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list help missing %q:\n%s", want, stdout)
		}
	}
}

func TestACKVulsCreateScansClusterAndReadsClusterVuls(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"request_id": "req-scan", "task_id": "T-vuls"},
		fakeACKClusterVulsResponse("np-1"),
	}}
	runCLI := catalogCaller(t, "ack", "vuls", fake)

	stdout, stderr, code := runCLI("ack", "vuls", "create", "--region", "cn-hangzhou", "--cluster", "c-123")
	if code != 0 {
		t.Fatalf("ack vuls create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "ScanClusterVuls,DescribeClusterVuls" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["cluster_id"] != "c-123" || fake.calls[1].request["cluster_id"] != "c-123" {
		t.Fatalf("cluster_id not forwarded to scan/readback: %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	if out["task_id"] != "T-vuls" {
		t.Fatalf("create output task_id = %#v, want T-vuls; stdout=%s", out["task_id"], stdout)
	}
	if _, ok := out["vuls"].([]any); !ok {
		t.Fatalf("create output must include cluster vuls readback: %s", stdout)
	}
}

func TestACKVulsCreateNoWaitOnlyReturnsScanTask(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{{"request_id": "req-scan", "task_id": "T-vuls"}}}
	runCLI := catalogCaller(t, "ack", "vuls", fake)

	stdout, stderr, code := runCLI("ack", "vuls", "create", "--region", "cn-hangzhou", "--cluster", "c-123", "--no-wait")
	if code != 0 {
		t.Fatalf("ack vuls create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "ScanClusterVuls" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	if out["task_id"] != "T-vuls" {
		t.Fatalf("create --no-wait output task_id = %#v, want T-vuls; stdout=%s", out["task_id"], stdout)
	}
	if _, ok := out["vuls"]; ok {
		t.Fatalf("create --no-wait must not emit readback vuls: %s", stdout)
	}
}

func TestACKVulsListRoutesClusterAndNodepoolViews(t *testing.T) {
	cluster := &fakeSpecCaller{responses: []map[string]any{fakeACKClusterVulsResponse("np-1")}}
	runCluster := catalogCaller(t, "ack", "vuls", cluster)

	stdout, stderr, code := runCluster("ack", "vuls", "list", "--region", "cn-hangzhou", "--cluster", "c-123")
	if code != 0 {
		t.Fatalf("ack vuls list cluster exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(cluster.calls); strings.Join(got, ",") != "DescribeClusterVuls" {
		t.Fatalf("cluster calls = %#v", cluster.calls)
	}
	if cluster.calls[0].request["cluster_id"] != "c-123" {
		t.Fatalf("DescribeClusterVuls request = %#v", cluster.calls[0].request)
	}
	clusterVuls, _ := decodeObject(t, stdout)["vuls"].([]any)
	if len(clusterVuls) != 1 {
		t.Fatalf("cluster vuls output = %s", stdout)
	}

	nodepool := &fakeSpecCaller{responses: []map[string]any{fakeACKNodepoolVulsResponse("i-1")}}
	runNodepool := catalogCaller(t, "ack", "vuls", nodepool)

	stdout, stderr, code = runNodepool(
		"ack", "vuls", "list",
		"--region", "cn-hangzhou",
		"--cluster", "c-123",
		"--nodepool", "np-1",
		"--severity", "asap",
	)
	if code != 0 {
		t.Fatalf("ack vuls list nodepool exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(nodepool.calls); strings.Join(got, ",") != "DescribeNodePoolVuls" {
		t.Fatalf("nodepool calls = %#v", nodepool.calls)
	}
	req := nodepool.calls[0].request
	if req["cluster_id"] != "c-123" || req["nodepool_id"] != "np-1" || req["necessity"] != "asap" {
		t.Fatalf("DescribeNodePoolVuls request = %#v", req)
	}
	nodepoolVuls, _ := decodeObject(t, stdout)["vuls"].([]any)
	if len(nodepoolVuls) != 1 {
		t.Fatalf("nodepool vuls output = %s", stdout)
	}
}

func fakeACKClusterVulsResponse(nodepool string) map[string]any {
	return map[string]any{
		"request_id": "req-cluster-vuls",
		"vul_records": []any{
			map[string]any{
				"nodepool_id":    nodepool,
				"nodepool_name":  "default",
				"node_count":     float64(1),
				"vul_name":       "oval:rhsa",
				"vul_alias_name": "CVE-2025-demo",
				"vul_type":       "cve",
				"necessity":      "asap",
				"cve_list":       []any{"CVE-2025-demo"},
			},
		},
	}
}

func fakeACKNodepoolVulsResponse(instance string) map[string]any {
	return map[string]any{
		"request_id": "req-nodepool-vuls",
		"vul_records": []any{
			map[string]any{
				"instance_id": instance,
				"node_name":   "cn-hangzhou.192.0.2.1",
				"vul_list": []any{
					map[string]any{
						"name":        "oval:rhsa",
						"alias_name":  "CVE-2025-demo",
						"necessity":   "asap",
						"cve_list":    []any{"CVE-2025-demo"},
						"need_reboot": false,
					},
				},
			},
		},
		"vuls_fix_service_purchased": false,
	}
}
