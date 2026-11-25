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
	updateBinding := resource.Bindings["update_volume"]
	updateBody, _ := updateBinding.Request["body"].(map[string]any)
	if len(updateBody) != 4 {
		t.Fatalf("UpdateVolume body must expose only the four storage configurations: %+v", updateBody)
	}
	for field, key := range map[string]string{
		"oss_volume_config":            "ossVolumeConfig",
		"agentic_fs_volume_config":     "agenticFSVolumeConfig",
		"agentic_bucket_volume_config": "agenticBucketVolumeConfig",
		"mount_config":                 "mountConfig",
	} {
		entry, ok := updateBody[key].(map[string]any)
		if !ok || entry["from"] != "$."+field {
			t.Fatalf("UpdateVolume body %s must map %s: %+v", key, field, updateBody[key])
		}
	}
	if updateBinding.Request["volumeID"] != "$.id" || updateBinding.Request["teamID"] != "$.team_id" {
		t.Fatalf("UpdateVolume must keep the path identity and the Team query scope: %+v", updateBinding.Request)
	}
	if len(updateBinding.RequireAll) != 2 || updateBinding.RequireAll[0].Request != "volumeID" || updateBinding.RequireAll[1].Request != "teamID" {
		t.Fatalf("UpdateVolume must require the path identity and the Team query scope: %+v", updateBinding.RequireAll)
	}
	if len(updateBinding.RequireAny) != 4 {
		t.Fatalf("UpdateVolume must require at least one storage configuration: %+v", updateBinding.RequireAny)
	}
	if resource.Probes["update_detail"].Request["teamID"] != "$context.updated_team_id" {
		t.Fatal("UpdateVolume readback must use the returned Team scope")
	}
	if len(update.Input.Fields) != 6 {
		t.Fatalf("update input must expose id, the Team scope and the four configurations: %+v", update.Input.Fields)
	}
	for _, ref := range update.Input.Fields {
		if ref.Name == "team_id" && !ref.Required {
			t.Fatal("update must require the Team scope")
		}
		if ref.Name == "status" {
			t.Fatal("update must not expose the provider status")
		}
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
