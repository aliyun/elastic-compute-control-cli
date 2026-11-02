package schema

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSchemaPositionalVisibilityAndInputOverrides(t *testing.T) {
	r := spec.ResourceSpec{Product: "test", Resource: "thing", Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"visible": {Type: "string"}, "hidden": {Type: "string"}}}}
	op := spec.Operation{Input: spec.OperationInput{Fields: spec.OperationFields{
		{Name: "hidden", Positional: true, HasSchema: true, Schema: false},
		{Name: "hidden", Positional: true, HasBrief: true, Brief: false},
		{Name: "missing", Positional: true},
		{Name: "visible", Positional: true},
	}}}
	if got := commandPositionals(r, op, "en", CommandSchemaBrief); len(got) != 1 || got[0].Name != "visible" {
		t.Fatalf("brief positionals=%+v", got)
	}
	if got := commandPositionals(r, op, "en", CommandSchemaFull); len(got) != 2 {
		t.Fatalf("full positionals=%+v", got)
	}
	usage := commandUsage(r, "inspect", []PositionalParam{{Name: "name"}})
	if !strings.Contains(usage, "[name]") {
		t.Fatalf("optional argument became required: %s", usage)
	}
	param := Param{Repeatable: true, Input: "json"}
	applyOperationParamOverrides(&param, spec.OperationFieldRef{HasRepeatable: true, Repeatable: false, InputStyle: "key_value"}, "en")
	if param.Repeatable || param.Input != "key_value" {
		t.Fatalf("input overrides lost: %+v", param)
	}
	if got := commandParams(r, "missing", "en", CommandSchemaBrief); got != nil {
		t.Fatalf("missing command has parameters: %v", got)
	}
	if got := singularInputName("addresses"); got != "address" {
		t.Fatalf("singular address=%s", got)
	}
}

func TestSchemaOutputProbeRefusesAmbiguousOrContextDependentSources(t *testing.T) {
	for _, test := range []struct {
		operation spec.Operation
		want      string
	}{
		{spec.Operation{Call: spec.OperationCall{Probe: "read"}}, "read"},
		{spec.Operation{Call: spec.OperationCall{Probe: "read"}, Workflow: []spec.WorkflowStep{{Probe: "other"}}}, ""},
		{spec.Operation{Workflow: []spec.WorkflowStep{{Probe: "read"}, {Probe: "other"}}}, ""},
	} {
		if got := commandOutputProbe(test.operation); got != test.want {
			t.Fatalf("output probe=%s, want %s", got, test.want)
		}
	}
	for _, value := range []any{"$context.", []any{map[string]any{"value": "$context.token"}}} {
		if !requestNeedsOperationContext(value) {
			t.Fatalf("operation context discarded: %#v", value)
		}
	}
	if requestNeedsOperationContext(map[string]any{"id": "$context.id", "region": "$context.region"}) {
		t.Fatal("generic poll selectors rejected")
	}
	if _, ok := ResourceForLanguageName("too.many.schema.name.parts", "en"); ok {
		t.Fatal("invalid schema path accepted")
	}
	if _, ok := ResourceForLanguage("unknown", "thing", "en"); ok {
		t.Fatal("unknown product accepted")
	}
	if _, ok := resourceForLanguagePath("", "thing", "one", "en"); ok {
		t.Fatal("missing product accepted")
	}
	if !hasAlias([]string{"first", "second"}, "second") {
		t.Fatal("resource alias lost")
	}
	if got := findParent("ecs", "instance"); got != "" {
		t.Fatalf("top-level parent=%s", got)
	}
	if got := findParent("missing", "thing"); got != "" {
		t.Fatalf("missing parent=%s", got)
	}
}

func TestSchemaFallbackDescriptionsHonorControlsAndResourceMetadata(t *testing.T) {
	r := spec.ResourceSpec{Product: "test", Resource: "test", Description: spec.LocalizedText{"en": "Product description"}, Controls: map[string]spec.SchemaField{"control": {Type: "boolean", Description: spec.LocalizedText{"en": "Control description"}}}}
	if got := resourceInputType(r, "control"); got != "boolean" {
		t.Fatalf("control type=%s", got)
	}
	if got := resourceInputType(r, "missing"); got != "" {
		t.Fatalf("missing type=%s", got)
	}
	if got := localizedInputDescription(r, "en", "control", "fallback"); got != "Control description" {
		t.Fatalf("control description=%s", got)
	}
	if got := productDescription("test", spec.ProductSpec{}, []spec.ResourceSpec{r}, "en"); got != "Product description" {
		t.Fatalf("product description=%s", got)
	}
	if got := filterDescription(r, "tag.", spec.Filter{}, "en"); got != "tag.<key> filter." {
		t.Fatalf("dynamic filter=%s", got)
	}
	if got := filterDescription(r, "name", spec.Filter{}, "en"); got != "name filter." {
		t.Fatalf("plain filter=%s", got)
	}
	for _, action := range []string{"list", "get", "update", "delete"} {
		if got := commandDescription(r, action, "test.test."+action, spec.Operation{}, "en"); got == "" || !strings.Contains(strings.ToLower(got), action) {
			t.Fatalf("%s description=%s", action, got)
		}
	}
}
