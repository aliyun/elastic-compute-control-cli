package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackTriggerCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "trigger" || resource.APIProduct != "cs" {
			t.Fatalf("resource = %s/%s api=%s, want ack/trigger api=cs", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})
}

func fakeACKTriggerListResponse(id string) map[string]any {
	return map[string]any{
		"items": []any{
			map[string]any{
				"id":         id,
				"name":       "webhook",
				"cluster_id": "c-ack",
				"project_id": "default/web",
				"type":       "deployment",
				"action":     "redeploy",
				"token":      "https://example.invalid/token",
			},
		},
	}
}

func TestACKTriggerHelpShape(t *testing.T) {
	stdout, stderr, code := runCLI("ack", "trigger", "--help")
	if code != 0 {
		t.Fatalf("ack trigger --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, action := range []string{"list", "get", "create", "delete"} {
		if !strings.Contains(stdout, action) {
			t.Fatalf("ack trigger help missing %q: %s", action, stdout)
		}
	}
	if strings.Contains(stdout, "update") {
		t.Fatalf("ack trigger help must not expose update: %s", stdout)
	}

	stdout, stderr, code = runCLI("ack", "trigger", "create", "--help")
	if code != 0 {
		t.Fatalf("ack trigger create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Create trigger",
		"Resource Flags (* required):",
		"* --cluster string",
		"* --project string",
		"* --action string",
		"--type string",
		"--api-param stringArray",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack trigger create help missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "--no-wait") || strings.Contains(stdout, "--timeout") {
		t.Fatalf("synchronous trigger create help must not expose wait controls: %s", stdout)
	}
}

func TestACKTriggerRoutesToCreateDeleteDescribe(t *testing.T) {
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"id": "tr-123"},
			{},
			fakeACKTriggerListResponse("tr-123"),
			fakeACKTriggerListResponse("tr-456"),
		},
	}
	runCLI := ackTriggerCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "trigger", "create",
		"--region", "cn-beijing",
		"--cluster", "c-ack",
		"--project", "default/web",
		"--action", "redeploy",
		"--type", "deployment",
	)
	if code != 0 {
		t.Fatalf("ack trigger create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateTrigger" {
		t.Fatalf("create calls = %#v", fake.calls)
	}
	createRequest := fake.calls[0].request
	for key, want := range map[string]any{
		"cluster_id":      "c-ack",
		"body.project_id": "default/web",
		"body.action":     "redeploy",
		"body.type":       "deployment",
	} {
		if got := createRequest[key]; got != want {
			t.Fatalf("CreateTrigger request[%s] = %#v, want %#v; request=%#v", key, got, want, createRequest)
		}
	}
	created, _ := decodeObject(t, stdout)["trigger"].(map[string]any)
	if created == nil || created["id"] != "tr-123" || created["project"] != "default/web" {
		t.Fatalf("unexpected create output: %s", stdout)
	}

	stdout, stderr, code = runCLI(
		"ack", "trigger", "delete", "tr-123",
		"--region", "cn-beijing",
		"--cluster", "c-ack",
	)
	if code != 0 {
		t.Fatalf("ack trigger delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[1].operation != "DeleteTrigger" {
		t.Fatalf("delete calls = %#v", fake.calls)
	}
	if fake.calls[1].request["cluster_id"] != "c-ack" || fake.calls[1].request["Id"] != "tr-123" {
		t.Fatalf("DeleteTrigger request = %#v", fake.calls[1].request)
	}

	stdout, stderr, code = runCLI(
		"ack", "trigger", "get", "tr-123",
		"--region", "cn-beijing",
		"--cluster", "c-ack",
		"--namespace", "default",
		"--name", "web",
	)
	if code != 0 {
		t.Fatalf("ack trigger get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[2].operation != "DescribeTrigger" {
		t.Fatalf("get calls = %#v", fake.calls)
	}
	getRequest := fake.calls[2].request
	for key, want := range map[string]any{
		"cluster_id": "c-ack",
		"Id":         "tr-123",
		"Namespace":  "default",
		"Name":       "web",
	} {
		if got := getRequest[key]; got != want {
			t.Fatalf("DescribeTrigger get request[%s] = %#v, want %#v; request=%#v", key, got, want, getRequest)
		}
	}
	gotTrigger, _ := decodeObject(t, stdout)["trigger"].(map[string]any)
	if gotTrigger == nil || gotTrigger["id"] != "tr-123" {
		t.Fatalf("unexpected get output: %s", stdout)
	}

	stdout, stderr, code = runCLI(
		"ack", "trigger", "list",
		"--region", "cn-beijing",
		"--cluster", "c-ack",
		"--namespace", "default",
		"--name", "web",
		"--type", "deployment",
		"--action", "redeploy",
	)
	if code != 0 {
		t.Fatalf("ack trigger list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 || fake.calls[3].operation != "DescribeTrigger" {
		t.Fatalf("list calls = %#v", fake.calls)
	}
	listRequest := fake.calls[3].request
	for key, want := range map[string]any{
		"cluster_id": "c-ack",
		"Namespace":  "default",
		"Name":       "web",
		"Type":       "deployment",
		"action":     "redeploy",
	} {
		if got := listRequest[key]; got != want {
			t.Fatalf("DescribeTrigger list request[%s] = %#v, want %#v; request=%#v", key, got, want, listRequest)
		}
	}
	if _, ok := listRequest["Id"]; ok {
		t.Fatalf("list must not send trigger ID: %#v", listRequest)
	}
	listed, _ := decodeObject(t, stdout)["triggers"].([]any)
	if len(listed) != 1 {
		t.Fatalf("unexpected list output: %s", stdout)
	}
}
