package spec_resource

import (
	"errors"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func agentRunCaller(t *testing.T, fake *fakeSpecCaller, wantResource string) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "agentrun" || resource.APIProduct != "AgentRun" || resource.Resource != wantResource {
			t.Fatalf("resource = %s/%s api=%s, want agentrun/%s api=AgentRun", resource.Product, resource.Resource, resource.APIProduct, wantResource)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})
}

func fakeAgentRunTemplate(name, status string) map[string]any {
	return map[string]any{
		"templateId":           "tmpl-id-123",
		"templateName":         name,
		"templateType":         "CodeInterpreter",
		"templateVersion":      "1",
		"status":               status,
		"statusReason":         "",
		"cpu":                  float64(2),
		"memory":               float64(4096),
		"description":          "test template",
		"createdAt":            "2026-08-18T10:00:00+08:00",
		"lastUpdatedAt":        "2026-08-18T10:01:00+08:00",
		"mcpState":             map[string]any{"status": "READY", "accessEndpoint": "/mcp"},
		"mcpOptions":           map[string]any{"transport": "streamable-http", "enabledTools": []any{"execute_code"}},
		"networkConfiguration": map[string]any{"networkMode": "PUBLIC"},
	}
}

func fakeAgentRunTemplateResult(name, status, requestID string) map[string]any {
	return map[string]any{"code": "SUCCESS", "requestId": requestID, "data": fakeAgentRunTemplate(name, status)}
}

func fakeAgentRunSandbox(id, status string) map[string]any {
	return map[string]any{
		"sandboxId":                 id,
		"templateId":                "tmpl-id-123",
		"templateName":              "code-template",
		"status":                    status,
		"createdAt":                 "2026-08-18T10:00:00+08:00",
		"lastUpdatedAt":             "2026-08-18T10:01:00+08:00",
		"sandboxIdleTimeoutSeconds": float64(1800),
		"metadata":                  map[string]any{"run": "test"},
	}
}

func fakeAgentRunSandboxResult(id, status, requestID string) map[string]any {
	return map[string]any{"code": "SUCCESS", "requestId": requestID, "data": fakeAgentRunSandbox(id, status)}
}

func TestAgentRunPublicHelpExposesTemplateAndSandboxResources(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "agentrun", "--help")
	if code != 0 {
		t.Fatalf("agentrun --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"template", "sandbox"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("agentrun help missing %q: %s", want, stdout)
		}
	}
	for _, want := range []string{"Examples:", "ecctl agentrun template list", "ecctl agentrun sandbox list"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("agentrun help missing example %q: %s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "agentrun", "template", "--help")
	if code != 0 {
		t.Fatalf("agentrun template --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"create", "update", "delete", "get", "list", "enable", "disable"} {
		if !strings.Contains(stdout, "\n  "+want+" ") {
			t.Fatalf("template help missing %s: %s", want, stdout)
		}
	}
	for _, want := range []string{"Examples:", "ecctl agentrun template list", "ecctl agentrun template create"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("template help missing example %q: %s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "agentrun", "sandbox", "--help")
	if code != 0 {
		t.Fatalf("agentrun sandbox --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"create", "delete", "get", "list", "stop"} {
		if !strings.Contains(stdout, "\n  "+want+" ") {
			t.Fatalf("sandbox help missing %s: %s", want, stdout)
		}
	}
	for _, removed := range []string{"pause", "resume"} {
		if strings.Contains(stdout, "\n  "+removed+" ") {
			t.Fatalf("sandbox help still exposes removed operation %s: %s", removed, stdout)
		}
	}
	for _, want := range []string{"Examples:", "ecctl agentrun sandbox list", "ecctl agentrun sandbox create"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("sandbox help missing example %q: %s", want, stdout)
		}
	}
}

func TestAgentRunTemplateCreateUpdateAndMCPActions(t *testing.T) {
	t.Parallel()

	createFake := &fakeSpecCaller{responses: []map[string]any{
		fakeAgentRunTemplateResult("code-template", "CREATING", "req-create"),
		fakeAgentRunTemplateResult("code-template", "READY", "req-get"),
	}}
	runCreate := agentRunCaller(t, createFake, "template")
	stdout, stderr, code := runCreate(
		"agentrun", "template", "create",
		"--region", "cn-hangzhou",
		"--name", "code-template",
		"--template-type", "CodeInterpreter",
		"--cpu", "2",
		"--memory", "4096",
		"--network-configuration", `{"networkMode":"PUBLIC"}`,
		"--environment-variables", `{"HOME":"/tmp"}`,
		"--oss-configuration", `{"bucketName":"bucket-a","mountPoint":"/mnt/a"}`,
		"--oss-configuration", `{"bucketName":"bucket-b","mountPoint":"/mnt/b"}`,
		"--timeout", "1s",
	)
	if code != 0 {
		t.Fatalf("template create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(createFake.calls); strings.Join(got, ",") != "CreateTemplate,GetTemplate" {
		t.Fatalf("create calls = %#v", createFake.calls)
	}
	request := createFake.calls[0].request
	if request["body.templateName"] != "code-template" || request["body.templateType"] != "CodeInterpreter" || request["body.memory"] != 4096 {
		t.Fatalf("CreateTemplate request = %#v", request)
	}
	if request["body.networkConfiguration"] != `{"networkMode":"PUBLIC"}` || request["body.environmentVariables"] != `{"HOME":"/tmp"}` {
		t.Fatalf("CreateTemplate JSON fields = %#v", request)
	}
	oss, _ := request["body.ossConfiguration"].([]any)
	if len(oss) != 2 || oss[0].(map[string]any)["bucketName"] != "bucket-a" || oss[1].(map[string]any)["bucketName"] != "bucket-b" {
		t.Fatalf("CreateTemplate OSS configuration = %#v", request["body.ossConfiguration"])
	}
	template, _ := decodeObject(t, stdout)["template"].(map[string]any)
	if template == nil || template["name"] != "code-template" || template["status"] != "READY" || template["template_id"] != "tmpl-id-123" || template["created_at"] == nil || template["updated_at"] == nil {
		t.Fatalf("template create output: %s", stdout)
	}

	updateFake := &fakeSpecCaller{responses: []map[string]any{
		fakeAgentRunTemplateResult("code-template", "UPDATING", "req-update"),
		fakeAgentRunTemplateResult("code-template", "READY", "req-get"),
	}}
	runUpdate := agentRunCaller(t, updateFake, "template")
	stdout, stderr, code = runUpdate("agentrun", "template", "update", "code-template", "--region", "cn-hangzhou", "--description", "updated", "--timeout", "1s")
	if code != 0 {
		t.Fatalf("template update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(updateFake.calls); strings.Join(got, ",") != "UpdateTemplate,GetTemplate" {
		t.Fatalf("update calls = %#v", updateFake.calls)
	}
	updateRequest := updateFake.calls[0].request
	if updateRequest["templateName"] != "code-template" || updateRequest["body.description"] != "updated" {
		t.Fatalf("UpdateTemplate request = %#v", updateRequest)
	}
	if token, _ := updateRequest["clientToken"].(string); !strings.HasPrefix(token, "ect-template-") {
		t.Fatalf("UpdateTemplate clientToken = %#v", updateRequest["clientToken"])
	}

	for _, tc := range []struct {
		action string
		api    string
		args   []string
	}{
		{action: "enable", api: "ActivateTemplateMCP", args: []string{"--transport", "streamable-http", "--enabled-tools", `["execute_code"]`}},
		{action: "disable", api: "StopTemplateMCP"},
	} {
		fake := &fakeSpecCaller{responses: []map[string]any{
			fakeAgentRunTemplateResult("code-template", "READY", "req-"+tc.action),
			fakeAgentRunTemplateResult("code-template", "READY", "req-get"),
		}}
		runAction := agentRunCaller(t, fake, "template")
		args := []string{"agentrun", "template", tc.action, "code-template", "--region", "cn-hangzhou"}
		args = append(args, tc.args...)
		stdout, stderr, code = runAction(args...)
		if code != 0 {
			t.Fatalf("template %s exit %d stderr=%s stdout=%s", tc.action, code, stderr, stdout)
		}
		if got := callNames(fake.calls); strings.Join(got, ",") != tc.api+",GetTemplate" {
			t.Fatalf("%s calls = %#v", tc.action, fake.calls)
		}
		if fake.calls[0].request["templateName"] != "code-template" {
			t.Fatalf("%s request = %#v", tc.action, fake.calls[0].request)
		}
	}
}

func TestAgentRunTemplateDisableRetriesOnlyWhileMCPCreating(t *testing.T) {
	t.Parallel()

	t.Run("creating conflict", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{
			errors: []error{
				errors.New("code: 409, template is not ready for stop, current MCP status is CREATING"),
				nil,
				nil,
			},
			responses: []map[string]any{
				fakeAgentRunTemplateResult("code-template", "READY", "req-disable"),
				fakeAgentRunTemplateResult("code-template", "READY", "req-get"),
			},
		}
		runAction := agentRunCaller(t, fake, "template")
		stdout, stderr, code := runAction("agentrun", "template", "disable", "code-template", "--region", "cn-hangzhou")
		if code != 0 {
			t.Fatalf("template disable exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if got := strings.Join(callNames(fake.calls), ","); got != "StopTemplateMCP,StopTemplateMCP,GetTemplate" {
			t.Fatalf("disable calls = %s", got)
		}
	})

	t.Run("other conflict", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSpecCaller{errors: []error{
			errors.New("code: 409, template cannot be stopped in the current state"),
		}}
		runAction := agentRunCaller(t, fake, "template")
		stdout, _, code := runAction("agentrun", "template", "disable", "code-template", "--region", "cn-hangzhou")
		if code == 0 {
			t.Fatalf("template disable unexpectedly succeeded: %s", stdout)
		}
		if got := strings.Join(callNames(fake.calls), ","); got != "StopTemplateMCP" {
			t.Fatalf("non-matching conflict calls = %s", got)
		}
	})
}

func TestAgentRunTemplateListAndDelete(t *testing.T) {
	t.Parallel()

	listFake := &fakeSpecCaller{responses: []map[string]any{{
		"code": "SUCCESS", "requestId": "req-list",
		"data": map[string]any{"pageNumber": float64(2), "pageSize": float64(20), "total": float64(1), "items": []any{fakeAgentRunTemplate("code-template", "READY")}},
	}}}
	runList := agentRunCaller(t, listFake, "template")
	stdout, stderr, code := runList("agentrun", "template", "list", "--region", "cn-hangzhou", "--filter", "status=READY", "--filter", "type=CodeInterpreter", "--limit", "20", "--page", "2")
	if code != 0 {
		t.Fatalf("template list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	request := listFake.calls[0].request
	if request["status"] != "READY" || request["templateType"] != "CodeInterpreter" || request["pageSize"] != 20 || request["pageNumber"] != 2 {
		t.Fatalf("ListTemplates request = %#v", request)
	}
	if decodeObject(t, stdout)["total"] != float64(1) {
		t.Fatalf("template list output: %s", stdout)
	}

	deleteFake := &fakeSpecCaller{responses: []map[string]any{
		fakeAgentRunTemplateResult("code-template", "DELETING", "req-delete"),
		{"code": "SUCCESS", "requestId": "req-list", "data": map[string]any{"total": float64(0), "items": []any{}}},
	}}
	runDelete := agentRunCaller(t, deleteFake, "template")
	stdout, stderr, code = runDelete("agentrun", "template", "delete", "code-template", "--region", "cn-hangzhou", "--timeout", "1s")
	if code != 0 {
		t.Fatalf("template delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(deleteFake.calls); strings.Join(got, ",") != "DeleteTemplate,ListTemplates" {
		t.Fatalf("delete calls = %#v", deleteFake.calls)
	}
	if decodeObject(t, stdout)["deleted"] != true {
		t.Fatalf("template delete output: %s", stdout)
	}
}

func TestAgentRunSandboxLifecycleAndList(t *testing.T) {
	t.Parallel()

	createFake := &fakeSpecCaller{responses: []map[string]any{
		fakeAgentRunSandboxResult("sb-123", "CREATING", "req-create"),
		fakeAgentRunSandboxResult("sb-123", "READY", "req-get"),
	}}
	runCreate := agentRunCaller(t, createFake, "sandbox")
	stdout, stderr, code := runCreate(
		"agentrun", "sandbox", "create",
		"--region", "cn-hangzhou",
		"--id", "sb-client-123",
		"--template-name", "code-template",
		"--idle-timeout", "1800",
		"--nas-config", `{"groupId":100,"userId":100}`,
		"--timeout", "1s",
	)
	if code != 0 {
		t.Fatalf("sandbox create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(createFake.calls); strings.Join(got, ",") != "CreateSandbox,GetSandbox" {
		t.Fatalf("create calls = %#v", createFake.calls)
	}
	request := createFake.calls[0].request
	if request["body.sandboxId"] != "sb-client-123" || request["body.templateName"] != "code-template" || request["body.sandboxIdleTimeoutInSeconds"] != 1800 || request["body.nasConfig"] != `{"groupId":100,"userId":100}` {
		t.Fatalf("CreateSandbox request = %#v", request)
	}
	sandbox, _ := decodeObject(t, stdout)["sandbox"].(map[string]any)
	if sandbox == nil || sandbox["id"] != "sb-123" || sandbox["status"] != "READY" {
		t.Fatalf("sandbox create output: %s", stdout)
	}

	listFake := &fakeSpecCaller{responses: []map[string]any{{
		"code": "SUCCESS", "requestId": "req-list",
		"data": map[string]any{"nextToken": "next-1", "items": []any{fakeAgentRunSandbox("sb-123", "READY")}},
	}}}
	runList := agentRunCaller(t, listFake, "sandbox")
	stdout, stderr, code = runList("agentrun", "sandbox", "list", "--region", "cn-hangzhou", "--filter", "template-name=code-template", "--filter", "status=READY", "--limit", "25", "--next-token", "prev")
	if code != 0 {
		t.Fatalf("sandbox list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	listRequest := listFake.calls[0].request
	if listRequest["templateName"] != "code-template" || listRequest["status"] != "READY" || listRequest["maxResults"] != 25 || listRequest["nextToken"] != "prev" {
		t.Fatalf("ListSandboxes request = %#v", listRequest)
	}
	pagination, _ := decodeObject(t, stdout)["pagination"].(map[string]any)
	if pagination["next_token"] != "next-1" {
		t.Fatalf("sandbox list output: %s", stdout)
	}

	for _, tc := range []struct {
		action string
		api    string
		state  string
	}{
		{action: "stop", api: "StopSandbox", state: "TERMINATED"},
	} {
		fake := &fakeSpecCaller{responses: []map[string]any{
			fakeAgentRunSandboxResult("sb-123", tc.state, "req-"+tc.action),
			fakeAgentRunSandboxResult("sb-123", tc.state, "req-get"),
		}}
		runAction := agentRunCaller(t, fake, "sandbox")
		stdout, stderr, code = runAction("agentrun", "sandbox", tc.action, "sb-123", "--region", "cn-hangzhou", "--timeout", "1s")
		if code != 0 {
			t.Fatalf("sandbox %s exit %d stderr=%s stdout=%s", tc.action, code, stderr, stdout)
		}
		if got := callNames(fake.calls); strings.Join(got, ",") != tc.api+",GetSandbox" {
			t.Fatalf("%s calls = %#v", tc.action, fake.calls)
		}
		sandbox, _ = decodeObject(t, stdout)["sandbox"].(map[string]any)
		if sandbox == nil || sandbox["status"] != tc.state {
			t.Fatalf("sandbox %s output: %s", tc.action, stdout)
		}
	}

	deleteFake := &fakeSpecCaller{responses: []map[string]any{
		fakeAgentRunSandboxResult("sb-123", "DELETING", "req-delete"),
		{"code": "SUCCESS", "requestId": "req-list", "data": map[string]any{"items": []any{}}},
	}}
	runDelete := agentRunCaller(t, deleteFake, "sandbox")
	stdout, stderr, code = runDelete("agentrun", "sandbox", "delete", "sb-123", "--region", "cn-hangzhou", "--timeout", "1s")
	if code != 0 {
		t.Fatalf("sandbox delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := callNames(deleteFake.calls); strings.Join(got, ",") != "DeleteSandbox,ListSandboxes" {
		t.Fatalf("delete calls = %#v", deleteFake.calls)
	}
	if decodeObject(t, stdout)["deleted"] != true {
		t.Fatalf("sandbox delete output: %s", stdout)
	}
}
