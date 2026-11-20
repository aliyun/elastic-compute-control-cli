package specs

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func deleteEmitContract(resource spec.ResourceSpec) error {
	operation, ok := resource.Operations["delete"]
	if !ok {
		return nil
	}
	_, outputScoped := operation.Output.Fields["absent_in_scope"]
	hasEmit := false
	hasDeleted := false
	emits := []any{operation.Emit}
	for _, step := range operation.Workflow {
		emits = append(emits, step.Emit)
	}
	for _, emitted := range emits {
		if emitted == nil {
			continue
		}
		hasEmit = true
		if emit, ok := emitted.(map[string]any); ok {
			if emit["deleted"] == true {
				hasDeleted = true
			}
			if fields, ok := emit["fields"].(map[string]any); ok {
				if _, ok := fields["absent_in_scope"]; ok {
					outputScoped = true
				}
			}
		}
	}
	if outputScoped {
		return scopedAbsenceDeleteContract(resource, operation)
	}
	if !hasEmit {
		return errors.New("delete operation missing emit")
	}
	if !hasDeleted {
		return errors.New("delete operation has emit but missing 'deleted: true'")
	}
	return nil
}

func scopedAbsenceDeleteContract(resource spec.ResourceSpec, operation spec.Operation) error {
	if operation.Emit != nil || len(operation.Workflow) != 2 {
		return errors.New("scoped absence requires one binding followed by one emit")
	}
	call, proof := operation.Workflow[0], operation.Workflow[1]
	if call.Binding == "" || call.WaitUnless != "input.no_wait" || call.Probe != "" || call.Emit != nil || call.When != "" || call.Unless != "" || call.Wait != "" || len(call.WhenAny) != 0 {
		return errors.New("scoped absence binding must wait by default")
	}
	if proof.Binding != "" || proof.Probe != "" || proof.Wait != "" || proof.When != "" || proof.Unless != "input.no_wait" || len(proof.WhenAny) != 0 {
		return errors.New("scoped absence proof must be emitted only after waiting")
	}
	expectedEmit := map[string]any{"fields": map[string]any{
		"id": "$input.id", "team_id": "$input.team_id", "absent_in_scope": true,
	}}
	if !reflect.DeepEqual(proof.Emit, expectedEmit) {
		return errors.New("scoped absence emit must carry only the queried ID, Team, and true proof")
	}
	noWait := false
	for _, control := range operation.Input.Controls {
		noWait = noWait || control.Name == "no_wait"
	}
	if !noWait || resource.Controls["no_wait"].Type != "boolean" {
		return errors.New("scoped absence requires a boolean no_wait control")
	}
	binding, ok := resource.Bindings[call.Binding]
	if !ok || binding.API != "DeleteVolume" || binding.Wait == "" {
		return errors.New("scoped absence requires a waiting DeleteVolume binding")
	}
	expectedRequest := map[string]any{"volumeID": "$.id", "teamID": "$.team_id"}
	if !reflect.DeepEqual(binding.Request, expectedRequest) {
		return errors.New("scoped absence delete request must identify the Volume and Team")
	}
	waiter, ok := resource.Waiters[binding.Wait]
	if !ok || waiter.Target != "absent" || waiter.Probe == "" || waiter.NotFoundPending {
		return errors.New("scoped absence requires an absent-target waiter")
	}
	probe, ok := resource.Probes[waiter.Probe]
	if !ok || probe.API != "GetVolume" || !reflect.DeepEqual(probe.Request, expectedRequest) {
		return errors.New("scoped absence waiter must probe the same Volume and Team")
	}
	if len(operation.Output.Select) != 0 {
		return errors.New("scoped absence cannot select an unverified output")
	}
	if _, ok := operation.Output.Fields["deleted"]; ok {
		return errors.New("scoped absence cannot claim global deletion")
	}
	if !reflect.DeepEqual(operation.Output.Fields["volume"], map[string]any{"id": "$input.id", "team_id": "$input.team_id"}) ||
		!reflect.DeepEqual(operation.Output.Fields["absent_in_scope"], map[string]any{"value": "$result.item.absent_in_scope", "unless": "$input.no_wait"}) ||
		!reflect.DeepEqual(operation.Output.Fields["deletion_requested"], map[string]any{"value": true, "when": "$input.no_wait"}) {
		return errors.New("scoped absence output must separate waited proof from request acceptance")
	}
	return nil
}

func TestDeleteEmitContractVariants(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		mutate   func(*spec.ResourceSpec)
		wantPass bool
	}{
		{name: "ordinary delete", file: "team.yaml", wantPass: true},
		{name: "ordinary delete missing emit", file: "team.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[1].Emit = nil
			resource.Operations["delete"] = operation
		}},
		{name: "ordinary delete false marker", file: "team.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[1].Emit.(map[string]any)["deleted"] = false
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence", file: "volume.yaml", wantPass: true},
		{name: "scoped absence missing proof", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			delete(operation.Workflow[1].Emit.(map[string]any)["fields"].(map[string]any), "absent_in_scope")
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence false proof", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[1].Emit.(map[string]any)["fields"].(map[string]any)["absent_in_scope"] = false
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence without wait", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			binding := resource.Bindings["delete_volume"]
			binding.Wait = ""
			resource.Bindings["delete_volume"] = binding
		}},
		{name: "scoped absence with wrong waiter target", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			waiter := resource.Waiters["absent_after_delete"]
			waiter.Target = "present"
			resource.Waiters["absent_after_delete"] = waiter
		}},
		{name: "scoped absence with wrong Team probe", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			probe := resource.Probes["detail"]
			probe.Request["teamID"] = "$.other_team_id"
			resource.Probes["detail"] = probe
		}},
		{name: "scoped absence without wait suppression", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[0].WaitUnless = ""
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence emitted during no wait", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[1].Unless = ""
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence missing output proof", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			delete(operation.Output.Fields, "absent_in_scope")
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence reports request without no wait", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Output.Fields["deletion_requested"] = map[string]any{"value": true}
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence claims global deleted", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Workflow[1].Emit.(map[string]any)["deleted"] = true
			resource.Operations["delete"] = operation
		}},
		{name: "scoped absence output claims global deleted", file: "volume.yaml", mutate: func(resource *spec.ResourceSpec) {
			operation := resource.Operations["delete"]
			operation.Output.Fields["deleted"] = true
			resource.Operations["delete"] = operation
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource, err := spec.LoadFile(filepath.Join(specsDir(), "sandbox", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(&resource)
			}
			err = deleteEmitContract(resource)
			if (err == nil) != tc.wantPass {
				t.Fatalf("delete contract pass=%t, want %t (error: %v)", err == nil, tc.wantPass, err)
			}
		})
	}
}
