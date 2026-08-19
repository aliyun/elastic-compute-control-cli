package aliyun

import (
	"reflect"
	"testing"
)

func TestAgentRunManualMetadataExposesSandboxOperations(t *testing.T) {
	t.Parallel()

	resolver, err := NewOpenAPIMetadataResolver("en", []string{"AgentRun"})
	if err != nil {
		t.Fatalf("NewOpenAPIMetadataResolver: %v", err)
	}
	leaves, canonical, err := resolver.OperationLeaves("AgentRun", "CreateTemplate", false)
	if err != nil {
		t.Fatalf("OperationLeaves: %v", err)
	}
	if canonical != "CreateTemplate" {
		t.Fatalf("canonical = %q, want CreateTemplate", canonical)
	}
	names := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		names = append(names, leaf.Name)
	}
	for _, want := range []string{"body.cpu", "body.memory", "body.networkConfiguration", "body.templateName", "body.templateType"} {
		if !containsString(names, want) {
			t.Fatalf("CreateTemplate metadata missing %s: %v", want, names)
		}
	}

	product, ok := OpenAPIProductByCode("agentrun", "en")
	if !ok {
		t.Fatal("OpenAPIProductByCode(agentrun) failed")
	}
	if product.Version != "2025-09-10" || !stringsEqualFold(product.Style, "ROA") {
		t.Fatalf("AgentRun product = version:%q style:%q", product.Version, product.Style)
	}
	detail, ok := OpenAPIOperationDetailFor("en", product, "ActivateTemplateMCP")
	if !ok {
		t.Fatal("ActivateTemplateMCP detail missing")
	}
	if detail.Method != "PATCH" || detail.PathPattern != "/2025-09-10/templates/{templateName}/mcp/activate" {
		t.Fatalf("ActivateTemplateMCP detail = %#v", detail)
	}
	for _, removed := range []string{"PauseSandbox", "ResumeSandbox"} {
		if _, ok := OpenAPIOperationDetailFor("en", product, removed); ok {
			t.Fatalf("removed AgentRun operation %s is still exposed", removed)
		}
	}
}

func TestAgentRunROARequestBuildsPatchAndTypedJSONBody(t *testing.T) {
	t.Parallel()

	caller := &OpenAPICaller{
		Product: "AgentRun",
		Region:  "cn-hangzhou",
		Profile: resolvedOpenAPIProfile{Language: "en"},
	}
	patchRequest, err := caller.commonRequest("ActivateTemplateMCP", map[string]any{
		"templateName":      "code-template",
		"body.enabledTools": []string{"execute_code"},
		"body.transport":    "streamable-http",
	})
	if err != nil {
		t.Fatalf("ActivateTemplateMCP request: %v", err)
	}
	if patchRequest.Method != "PATCH" || patchRequest.Domain != "agentrun.cn-hangzhou.aliyuncs.com" {
		t.Fatalf("PATCH request = method:%q domain:%q", patchRequest.Method, patchRequest.Domain)
	}
	patchRequest.TransToAcsRequest()
	if got := patchRequest.BuildQueries(); got != "/2025-09-10/templates/code-template/mcp/activate" {
		t.Fatalf("PATCH path = %q", got)
	}
	if got := patchRequest.BodyValue(); !reflect.DeepEqual(got, map[string]any{
		"enabledTools": []any{"execute_code"},
		"transport":    "streamable-http",
	}) {
		t.Fatalf("PATCH body = %#v", got)
	}

	createRequest, err := caller.commonRequest("CreateTemplate", map[string]any{
		"body.templateName":         "code-template",
		"body.templateType":         "CodeInterpreter",
		"body.cpu":                  2.0,
		"body.memory":               4096,
		"body.networkConfiguration": `{"networkMode":"PUBLIC"}`,
	})
	if err != nil {
		t.Fatalf("CreateTemplate request: %v", err)
	}
	createRequest.TransToAcsRequest()
	body, _ := createRequest.BodyValue().(map[string]any)
	network, _ := body["networkConfiguration"].(map[string]any)
	if network["networkMode"] != "PUBLIC" {
		t.Fatalf("typed CreateTemplate body = %#v", body)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringsEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a >= 'a' && a <= 'z' {
			a -= 'a' - 'A'
		}
		if b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
