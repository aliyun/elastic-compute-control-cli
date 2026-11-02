package cli

import (
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/spf13/cobra"
)

func TestProductDiscoveryPreservesSpecFailuresAndSkipsOrphanChildren(t *testing.T) {
	cause := errors.New("invalid fixture spec")
	options := &globalOptions{lang: "en", fullSurface: true}
	broken := newProductCommand(options, io.Discard, "demo", discoveredProduct{err: cause}, nil, productBuildTarget{})
	if err := broken.RunE(broken, nil); err == nil {
		t.Fatal("invalid product spec ignored")
	}
	resources := []discoveredResource{
		{ref: spec.ResourceRef{Product: "demo", Resource: "demo"}, resource: spec.ResourceSpec{Product: "demo", Resource: "demo", Aliases: []string{"demo", "parent", "alternate"}}},
		{ref: spec.ResourceRef{Product: "demo", Resource: "parent"}, resource: spec.ResourceSpec{Product: "demo", Resource: "parent"}},
		{ref: spec.ResourceRef{Product: "demo", Resource: "broken"}, err: cause},
		{ref: spec.ResourceRef{Product: "demo", Resource: "child", Parent: "parent"}, err: cause},
		{ref: spec.ResourceRef{Product: "demo", Resource: "orphan", Parent: "absent"}, err: cause},
	}
	cmd := newProductCommand(options, io.Discard, "demo", discoveredProduct{}, resources, productBuildTarget{buildAll: true})
	if findDirectCommand(cmd, "alternate") == nil || findDirectCommand(cmd, "demo") != nil || findDirectCommand(cmd, "orphan") != nil {
		t.Fatal("aliases or orphan child routing changed")
	}
	parent := findDirectCommand(cmd, "parent")
	if parent == nil {
		t.Fatal("parent missing")
	}
	for _, failed := range []*cobra.Command{findDirectCommand(cmd, "broken"), findDirectCommand(parent, "child")} {
		if failed == nil || failed.RunE == nil || failed.RunE(failed, nil) == nil {
			t.Fatal("invalid resource spec hidden")
		}
	}
	for _, tc := range []struct {
		product, alias string
		want           bool
	}{{"ack", "cluster", true}, {"ack", "kc", true}, {"ack", "np", true}, {"lingjun", "ng", true}, {"unknown", "list", false}} {
		got := publicCLIResourceIdentifier(tc.product, tc.alias)
		if tc.product == "unknown" {
			got = publicCLIDefaultResourceAction(tc.product, tc.alias)
		}
		if got != tc.want {
			t.Fatalf("resource alias %s/%s=%t", tc.product, tc.alias, got)
		}
	}
	if got := resourceActionGroup(nil); got != "" {
		t.Fatalf("ungrouped actions assigned %s", got)
	}
}

func TestResourceImportanceSortsDeclaredResourcesThenUnknownNames(t *testing.T) {
	var resources []discoveredResource
	for _, name := range []string{"zzz", "first", "aaa", "ecs", "second"} {
		resources = append(resources, discoveredResource{ref: spec.ResourceRef{Product: "ecs", Resource: name}})
	}
	got := sortResourcesByImportance(resources, "ecs", []string{"second", "first"})
	var names []string
	for _, item := range got {
		names = append(names, item.ref.Resource)
	}
	if !reflect.DeepEqual(names, []string{"ecs", "second", "first", "aaa", "zzz"}) {
		t.Fatalf("resource importance order=%v", names)
	}
	if resources[0].ref.Resource != "zzz" {
		t.Fatal("resource sorting mutated shared discovery order")
	}
}

func TestResourceInputRejectsJSONIDsAndInvalidEnumBeforeDispatch(t *testing.T) {
	resource := spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"ids": {Type: "string_array"}, "value": {Type: "string", Enum: []string{"allowed"}}}}}
	for _, mode := range []string{"ids", "enum"} {
		cmd := &cobra.Command{}
		operation := spec.Operation{}
		want := "InvalidIDs"
		if mode == "ids" {
			operation.Input.Fields = spec.OperationFields{{Name: "ids", PositionalMany: true}}
			cmd.Flags().String("ids", `["one"]`, "")
		} else {
			operation.Input.Fields = spec.OperationFields{{Name: "value", Enum: []string{"allowed"}}}
			cmd.Flags().String("value", "", "")
			if err := cmd.Flags().Set("value", "rejected"); err != nil {
				t.Fatal(err)
			}
			want = "InvalidParameter"
		}
		if _, _, err := resourceActionInput(cmd, resource, "delete", operation, nil); err == nil {
			t.Fatalf("invalid %s input accepted", mode)
		} else if code, _, ok := appErrorCodeMessage(err); !ok || code != want {
			t.Fatalf("invalid %s classification=%v", mode, err)
		}
	}
	if err := applyFilterInput(resource, "list", map[string]any{"filter": []string{"=value"}}); err == nil {
		t.Fatal("empty filter key accepted")
	}
	if _, ok := operationInputSpecs(resource, "missing"); ok {
		t.Fatal("missing operation has inputs")
	}
	if got := operationDescription(resource, "missing", "en"); got != "" {
		t.Fatalf("missing operation description=%s", got)
	}
	resource.Operations = map[string]spec.Operation{"list": {Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "absent"}}}}}
	if inputs, ok := operationInputSpecs(resource, "list"); !ok {
		t.Fatal("existing operation not found")
	} else {
		for _, input := range inputs {
			if input.name == "absent" {
				t.Fatal("undefined operation field exposed")
			}
		}
	}
	if err := validateResourceActionInput(resource, "create", spec.Operation{RequireAny: []spec.Requirement{{Raw: "$result.missing"}}}, nil); err == nil {
		t.Fatal("invalid required expression accepted")
	}
	if err := validateResourcePagination(map[string]any{"page": 1}); err != nil {
		t.Fatalf("page-only pagination did not default limit: %v", err)
	}
}

func TestResourceOutputConditionsAndFieldNamesPreserveFallbackBehavior(t *testing.T) {
	ctx := actionOutputContext{result: engine.Result{Items: []map[string]any{{"id": "one"}}}}
	if !outputConditionMatches("$result.items", ctx) || outputConditionMatches("$result.unknown", ctx) {
		t.Fatal("result presence condition changed")
	}
	resource := spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"value": {Description: spec.LocalizedText{"en": "Value description"}}}}}
	if got := paramUsage(resource, "value", spec.Param{}, "en"); got != "Value description" {
		t.Fatalf("field description lost: %s", got)
	}
	if got := operationFieldDisplayNames(resource, spec.Operation{}, []string{"value", "value"}); !reflect.DeepEqual(got, []string{"--value"}) {
		t.Fatalf("duplicate field display names=%v", got)
	}
}
