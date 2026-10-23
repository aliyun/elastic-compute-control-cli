package sandbox

import (
	"slices"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestTemplateCacheSpecContract(t *testing.T) {
	product, err := spec.LoadProduct("..", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(product.Resources, "template-cache") {
		t.Fatal("sandbox product must register template-cache")
	}
	resource, err := spec.LoadResource("..", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Kind != "regional" {
		t.Fatalf("wrong FC route: provider=%q product=%q kind=%q", resource.Provider, resource.APIProduct, resource.Kind)
	}
	if len(resource.Operations) != 4 {
		t.Fatalf("operations = %v, want only create/get/list/delete", resource.Operations)
	}
	for action, api := range map[string]string{
		"create": "CreateTemplateCache",
		"get":    "DescribeTemplateCache",
		"list":   "ListTemplateCache",
		"delete": "DeleteTemplateCache",
	} {
		if _, ok := resource.Operations[action]; !ok {
			t.Errorf("missing %s operation", action)
		}
		found := false
		for _, binding := range resource.Bindings {
			if binding.API == api {
				found = true
			}
		}
		for _, probe := range resource.Probes {
			if probe.API == api {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s API mapping", api)
		}
	}
	if resource.Identity.OutputRoot.One != "template_cache" || resource.Identity.OutputRoot.Many != "template_caches" {
		t.Fatalf("wrong response roots: %+v", resource.Identity.OutputRoot)
	}
	if got := resource.Probes["list"].Response.NextToken; got != "$.nextToken" {
		t.Errorf("pagination token source = %q", got)
	}
	if got := resource.Probes["detail"].Response.State; got != "$.status" {
		t.Errorf("cache state source = %q, want status instead of template build status", got)
	}
	if description := resource.Schema.Fields["status"].Description; len(description) != 2 || description["en"] != "cache state, separate from template build state; provider creating from Create and Describe is shown as InProgress, and ready as Success" {
		t.Errorf("status description has malformed locale keys: %+v", description)
	}
}
