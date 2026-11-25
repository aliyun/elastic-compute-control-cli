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
	if len(resource.Operations) != 5 {
		t.Fatalf("Volume operations = %d, want five", len(resource.Operations))
	}
	for action, api := range map[string]string{"create": "CreateVolume", "get": "GetVolume", "list": "ListVolumes", "delete": "DeleteVolume", "update": "UpdateVolume"} {
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
	update := resource.Operations["update"]
	updateBody, _ := resource.Bindings["update_volume"].Request["body"].(map[string]any)
	if len(updateBody) != 2 || updateBody["status"] != "$.status" || updateBody["teamID"] != "$.team_id" {
		t.Fatalf("UpdateVolume body must expose only status and teamID: %+v", updateBody)
	}
	if resource.Bindings["update_volume"].Request["volumeID"] != "$.id" {
		t.Fatal("UpdateVolume must keep the path identity")
	}
	if resource.Probes["update_detail"].Request["teamID"] != "$context.updated_team_id" {
		t.Fatal("UpdateVolume readback must use the destination Team scope")
	}
	if len(update.Input.Fields) != 3 {
		t.Fatalf("update input must expose only id, status and team_id: %+v", update.Input.Fields)
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
