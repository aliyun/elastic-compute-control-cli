package sandbox

import (
	"slices"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestQuotaSpecContract(t *testing.T) {
	product, err := spec.LoadProduct("..", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(product.Resources, "quota") {
		t.Fatal("sandbox product must register quota")
	}
	resource, err := spec.LoadResource("..", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Kind != "regional" {
		t.Fatalf("wrong quota route: %+v", resource)
	}
	if len(resource.Operations) != 4 {
		t.Fatalf("quota operations = %v, want get/list/update/delete only", resource.Operations)
	}
	for _, action := range []string{"get", "list", "update", "delete"} {
		if _, ok := resource.Operations[action]; !ok {
			t.Errorf("missing %s", action)
		}
	}
	if resource.Probes["detail"].Request["tagValue"] != "$.id" || resource.Bindings["delete_quota"].Request["tagValue"] != "$.id" {
		t.Fatal("Team ID must be a query tagValue for get and delete")
	}
	if resource.Bindings["delete_quota"].Wait != "absent_after_delete" || resource.Waiters["absent_after_delete"].Probe != "detail" || resource.Waiters["absent_after_delete"].Target != "absent" {
		t.Fatal("Quota delete must wait for bounded read-only absence confirmation")
	}
	if resource.Probes["list"].Response.NextToken != "$.nextToken" || resource.Probes["list"].Request["maxResults"] != "$.limit" {
		t.Fatal("Quota list token/limit mapping changed")
	}
	if resource.Schema.Fields["cpu_cores"].Type != "integer" || resource.Schema.Fields["memory_gb"].Type != "integer" {
		t.Fatal("CPU cores and memory GB must remain integer fields")
	}
}
