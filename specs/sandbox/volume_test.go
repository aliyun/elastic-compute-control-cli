package sandbox

import (
	"slices"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestVolumeSpecContract(t *testing.T) {
	product, err := spec.LoadProduct("..", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(product.Resources, "volume") {
		t.Fatal("sandbox product must register volume")
	}
	resource, err := spec.LoadResource("..", "sandbox", "volume")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Kind != "regional" {
		t.Fatalf("wrong FC route: %+v", resource)
	}
	if len(resource.Operations) != 4 {
		t.Fatalf("Volume operations = %d, want four", len(resource.Operations))
	}
	for action, api := range map[string]string{"create": "CreateVolume", "get": "GetVolume", "list": "ListVolumes", "delete": "DeleteVolume"} {
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
	if _, present := resource.Operations["update"]; present {
		t.Fatal("cross-Team Volume update is deferred from the first release")
	}
	if resource.Probes["detail"].Request["volumeID"] != "$.id" || resource.Bindings["delete_volume"].Request["volumeID"] != "$.id" {
		t.Fatal("Volume ID must be the path identity")
	}
	if resource.Probes["list"].Response.NextToken != "$.nextToken" || resource.Probes["list"].Request["maxResults"] != "$.limit" {
		t.Fatal("Volume token pagination mapping changed")
	}
	if resource.Bindings["create_volume"].Wait != "ready" || resource.Bindings["delete_volume"].Wait != "absent_after_delete" {
		t.Fatal("create/delete waiters must distinguish acceptance from completion")
	}
	for _, field := range []string{"oss_volume_config", "agentic_fs_volume_config", "agentic_bucket_volume_config", "mount_config"} {
		if got := resource.Schema.Fields[field]; got.Type != "object" || got.InputStyle != "json|@file" {
			t.Errorf("%s must accept JSON or @file objects: %+v", field, got)
		}
	}
	if resource.Identity.OutputRoot.One != "volume" || resource.Identity.OutputRoot.Many != "volumes" {
		t.Fatalf("wrong output roots: %+v", resource.Identity.OutputRoot)
	}
}
