package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackDiagnosisCaller(t *testing.T, fake *fakeSpecCaller, wantResource string, wantParent string) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.APIProduct != "CS" || resource.Resource != wantResource || resource.Parent != wantParent {
			t.Fatalf("resource = product:%s api:%s resource:%s parent:%s, want ack/CS/%s parent %q",
				resource.Product, resource.APIProduct, resource.Resource, resource.Parent, wantResource, wantParent)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func TestACKDiagnosisHelpExposesDesignedSurface(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ack", "diagnosis", "--help")
	if code != 0 {
		t.Fatalf("ack diagnosis --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"create", "get", "check-item"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack diagnosis help missing %q:\n%s", want, stdout)
		}
	}
	for _, notWant := range []string{"delete", "list"} {
		if strings.Contains(stdout, notWant) {
			t.Fatalf("ack diagnosis help should not expose %q:\n%s", notWant, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "diag", "create", "--help")
	if code != 0 {
		t.Fatalf("ack diag create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster", "--type", "--target", "--no-wait", "--timeout", "inline key=value, JSON object, or @file"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack diag create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "diagnosis", "get", "--help")
	if code != 0 {
		t.Fatalf("ack diagnosis get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster", "--language"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack diagnosis get help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "diagnosis", "check-item", "list", "--help")
	if code != 0 {
		t.Fatalf("ack diagnosis check-item list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster", "--type", "--language"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack diagnosis check-item list help missing %q:\n%s", want, stdout)
		}
	}
}

func TestACKDiagnosisCreateWaitsAndGetFetchesResult(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id":   "req-create",
			"cluster_id":   "c-123",
			"diagnosis_id": "diag-123",
		},
		{
			"request_id":   "req-get",
			"diagnosis_id": "diag-123",
			"message":      "success",
			"status":       float64(2),
			"type":         "node",
			"target":       map[string]any{"name": "node-1"},
			"result":       map[string]any{"phase": float64(5)},
		},
	}}
	runCLI := ackDiagnosisCaller(t, fake, "diagnosis", "")

	stdout, stderr, code := runCLI("ack", "diag", "create",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--type", "node",
		"--target", "name=node-1",
		"--timeout", "1s",
	)
	if code != 0 {
		t.Fatalf("ack diag create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "CreateClusterDiagnosis,GetClusterDiagnosisResult" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	if createReq["cluster_id"] != "c-123" || createReq["body.type"] != "node" || createReq["body.target"] != `{"name":"node-1"}` {
		t.Fatalf("CreateClusterDiagnosis request = %#v", createReq)
	}
	getReq := fake.calls[1].request
	if getReq["cluster_id"] != "c-123" || getReq["diagnosis_id"] != "diag-123" {
		t.Fatalf("GetClusterDiagnosisResult request = %#v", getReq)
	}
	diagnosis, _ := decodeObject(t, stdout)["diagnosis"].(map[string]any)
	if diagnosis == nil || diagnosis["id"] != "diag-123" || diagnosis["status"] != "success" {
		t.Fatalf("unexpected create output: %s", stdout)
	}
}

func TestACKDiagnosisCreateNoWaitReturnsDiagnosisIDOnly(t *testing.T) {
	fake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id":   "req-create",
			"cluster_id":   "c-123",
			"diagnosis_id": "diag-123",
		},
	}}
	runCLI := ackDiagnosisCaller(t, fake, "diagnosis", "")

	stdout, stderr, code := runCLI("ack", "diagnosis", "create",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--type", "node",
		"--target", `{"name":"node-1"}`,
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ack diagnosis create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(fake.calls); strings.Join(got, ",") != "CreateClusterDiagnosis" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	diagnosis, _ := decodeObject(t, stdout)["diagnosis"].(map[string]any)
	if diagnosis == nil || diagnosis["id"] != "diag-123" {
		t.Fatalf("unexpected no-wait output: %s", stdout)
	}
}

func TestACKDiagnosisGetAndCheckItemListCallDesignedAPIs(t *testing.T) {
	getFake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id":   "req-get",
			"diagnosis_id": "diag-123",
			"message":      "success",
			"status":       float64(2),
			"type":         "node",
		},
	}}
	runGet := ackDiagnosisCaller(t, getFake, "diagnosis", "")

	stdout, stderr, code := runGet("ack", "diag", "get", "diag-123",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--language", "en",
	)
	if code != 0 {
		t.Fatalf("ack diag get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(getFake.calls) != 1 || getFake.calls[0].operation != "GetClusterDiagnosisResult" {
		t.Fatalf("get calls = %#v", getFake.calls)
	}
	getReq := getFake.calls[0].request
	if getReq["cluster_id"] != "c-123" || getReq["diagnosis_id"] != "diag-123" || getReq["language"] != "en" {
		t.Fatalf("GetClusterDiagnosisResult request = %#v", getReq)
	}

	listFake := &fakeSpecCaller{responses: []map[string]any{
		{
			"request_id": "req-items",
			"code":       "success",
			"is_success": true,
			"check_items": []any{
				map[string]any{
					"name":    "HostDNS",
					"display": "HostDNS",
					"group":   "Node",
					"level":   "normal",
					"message": "success",
					"refer":   "True",
					"value":   "True",
					"desc":    "Check whether the node can access host dns service",
				},
			},
		},
	}}
	runList := ackDiagnosisCaller(t, listFake, "check-item", "diagnosis")

	stdout, stderr, code = runList("ack", "diagnosis", "check-item", "list",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--type", "node",
		"--language", "en",
	)
	if code != 0 {
		t.Fatalf("ack diagnosis check-item list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(listFake.calls) != 1 || listFake.calls[0].operation != "GetClusterDiagnosisCheckItems" {
		t.Fatalf("list calls = %#v", listFake.calls)
	}
	listReq := listFake.calls[0].request
	if listReq["cluster_id"] != "c-123" || listReq["diagnosis_id"] != "node" || listReq["language"] != "en" {
		t.Fatalf("GetClusterDiagnosisCheckItems request = %#v", listReq)
	}
	items, _ := decodeObject(t, stdout)["check_items"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected check-item output: %s", stdout)
	}
}
