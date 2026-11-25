package sandbox

import (
	"slices"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestApiKeySpecContract(t *testing.T) {
	product, err := spec.LoadProduct("..", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(product.Resources, "api-key") {
		t.Fatal("sandbox product must register api-key")
	}
	resource, err := spec.LoadResource("..", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Kind != "regional" {
		t.Fatalf("wrong FC route: %+v", resource)
	}
	if len(resource.Operations) != 6 {
		t.Fatalf("api-key operations = %d, want six", len(resource.Operations))
	}
	for action, api := range map[string]string{
		"create": "CreateApiKey", "get": "DescribeApiKey", "list": "ListApiKeys",
		"update": "UpdateApiKey", "delete": "DeleteApiKey", "reset": "ResetApiKey",
	} {
		if _, ok := resource.Operations[action]; !ok {
			t.Errorf("missing %s", action)
		}
		found := false
		for _, binding := range resource.Bindings {
			found = found || binding.API == api
		}
		for _, probe := range resource.Probes {
			found = found || probe.API == api
		}
		if !found {
			t.Errorf("missing %s mapping", api)
		}
	}
	if resource.Probes["detail"].Request["apiKeyID"] != "$.id" || resource.Bindings["delete_api_key"].Request["apiKeyID"] != "$.id" || resource.Bindings["reset_api_key"].Request["apiKeyID"] != "$.id" {
		t.Fatal("apiKeyID must be the path identity")
	}
	if resource.Probes["list"].Request["pageNumber"] != "$.page" || resource.Probes["list"].Request["pageSize"] != "$.limit" || resource.Probes["list"].Response.Total != "$.total" {
		t.Fatal("api-key page pagination mapping changed")
	}
	if resource.Bindings["delete_api_key"].Wait != "absent_after_delete" || resource.Waiters["absent_after_delete"].Probe != "detail" || resource.Waiters["absent_after_delete"].Target != "absent" {
		t.Fatal("api-key delete must wait for bounded read-only absence confirmation")
	}
	deactivate := resource.Bindings["deactivate_api_key"]
	if deactivate.API != "UpdateApiKey" || deactivate.Request["apiKeyID"] != "$.id" {
		t.Fatalf("deactivate binding must target UpdateApiKey by path identity: %+v", deactivate)
	}
	body, ok := deactivate.Request["body"].(map[string]any)
	if !ok || body["status"] != "inactive" {
		t.Fatalf("deactivate binding must send a literal inactive status: %+v", deactivate.Request)
	}
	workflow := resource.Operations["delete"].Workflow
	if len(workflow) < 3 || workflow[0].Probe != "detail" || workflow[0].As != "current_api_key" || workflow[0].NotFound != "error" ||
		workflow[1].Binding != "deactivate_api_key" || workflow[1].When != "context.current_api_key.status == active" || workflow[2].Binding != "delete_api_key" {
		t.Fatalf("delete must deactivate only active keys before DeleteApiKey: %+v", workflow)
	}
	for _, action := range []string{"create", "reset"} {
		operation := resource.Operations[action]
		if !slices.Contains(operationControlNames(operation), "show_value") {
			t.Errorf("%s must expose the show_value control", action)
		}
	}
	for _, action := range []string{"get", "list", "update", "delete"} {
		if slices.Contains(operationControlNames(resource.Operations[action]), "show_value") {
			t.Errorf("%s must not expose a plaintext reveal control", action)
		}
	}
	if resource.Bindings["create_api_key"].Request["ShowValue"] != "$.show_value" || resource.Bindings["reset_api_key"].Request["ShowValue"] != "$.show_value" {
		t.Fatal("create/reset must route the reveal control to the caller")
	}
	if resource.Identity.OutputRoot.One != "api_key" || resource.Identity.OutputRoot.Many != "api_keys" {
		t.Fatalf("wrong output roots: %+v", resource.Identity.OutputRoot)
	}
	if got := resource.Schema.Fields["ip_whitelist"]; got.Type != "array" || got.Items == nil || got.Items.Type != "object" {
		t.Fatalf("ip_whitelist must be an object array: %+v", got)
	}
}

func operationControlNames(operation spec.Operation) []string {
	names := make([]string, 0, len(operation.Input.Controls))
	for _, control := range operation.Input.Controls {
		names = append(names, control.Name)
	}
	return names
}
