package cli

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/spf13/cobra"
)

func TestStructuredInputCoercionBoundaries(t *testing.T) {
	for _, tt := range []struct {
		kind        string
		input, want any
		bad         bool
	}{
		{"integer", 1, 1, false}, {"integer", true, true, false},
		{"number", 1.5, 1.5, false}, {"number", 2, float64(2), false}, {"number", true, true, false}, {"number", "bad", nil, true},
		{"boolean", "bad", nil, true}, {"boolean", 1, 1, false},
		{"array", "a,b", []string{"a", "b"}, false}, {"object", "raw", "raw", false},
		{"object", map[string]any{"nested-key": "value"}, map[string]any{"nested_key": "value"}, false},
	} {
		got, err := coerceStructuredValue(tt.input, spec.SchemaField{Type: tt.kind})
		if (err != nil) != tt.bad || !tt.bad && !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("coerce %s(%#v) = %#v, %v", tt.kind, tt.input, got, err)
		}
	}
	field := spec.SchemaField{Type: "object", Fields: map[string]spec.SchemaField{"count": {Type: "integer", Required: true}, "mode": {Type: "string", Enum: []string{"safe"}}}}
	for _, raw := range []string{"@" + filepath.Join(t.TempDir(), "missing"), "{invalid", `{"unknown":1}`, `{"count":"bad"}`, "=value", "count", "count=bad", "count=1,mode=unsafe"} {
		if _, _, err := structuredObjectFlagValue(raw, "settings", field, true); err == nil {
			t.Fatalf("invalid structured input accepted: %q", raw)
		}
	}
	apiField := field
	apiField.InputStyle = "json|@file"
	for _, raw := range []string{`{"mode":"safe"}`, `count=1`} {
		if _, _, err := structuredObjectFlagValue(raw, "settings", apiField, true); err == nil {
			t.Fatalf("invalid API-shaped input accepted: %q", raw)
		}
	}
	if _, _, err := structuredObjectArrayFlagValue([]string{"count=bad"}, "settings", spec.SchemaField{Items: &field}, true); err == nil {
		t.Fatal("invalid object array accepted")
	}
	if _, _, err := stringFlagValue("@"+filepath.Join(t.TempDir(), "missing"), "file", spec.Param{Use: "@file"}, true); err == nil {
		t.Fatal("missing input file accepted")
	}
	if got, assigned, err := jsonFlagValue(" ", "settings", "object", true); got != nil || !assigned || err != nil {
		t.Fatalf("empty JSON = %#v, %t, %v", got, assigned, err)
	}
	if _, _, err := jsonFlagValue("invalid", "settings", "object", true); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestTypedFilterAndParameterValidation(t *testing.T) {
	resource := spec.ResourceSpec{}
	for _, kind := range []string{"boolean", "integer", "number", "key_value"} {
		if err := applyFilterValue(resource, map[string]any{}, spec.Filter{Type: kind}, "", "value", "bad"); err == nil {
			t.Fatalf("invalid %s filter accepted", kind)
		}
	}
	input := map[string]any{}
	if err := applyFilterValue(resource, input, spec.Filter{Type: "number"}, "", "value", "1.25"); err != nil || input["value"] != 1.25 {
		t.Fatalf("numeric filter = %#v, %v", input, err)
	}
	if resourceInputType(resource, "missing") != "" {
		t.Fatal("missing input has a type")
	}
	max := 5
	if err := validateParamMax("size", spec.Param{Max: &max}, 6.0); err == nil {
		t.Fatal("oversized float accepted")
	}
	if err := validateParamMax("size", spec.Param{Max: &max}, "6"); err != nil {
		t.Fatal("unexpected scalar coercion")
	}
	if err := validateParamEnum("mode", spec.Param{Enum: []string{"safe"}}, "unsafe"); err == nil {
		t.Fatal("invalid enum accepted")
	}
	if err := validateParamEnum("mode", spec.Param{Enum: []string{"safe"}}, 1); err != nil {
		t.Fatal("non-string enum unexpectedly rejected")
	}
	if cmdActionFilters(nil) != nil || cmdActionFilters(&cobra.Command{Annotations: map[string]string{}}) != nil {
		t.Fatal("missing filter metadata produced filters")
	}
	if helpFilterableFieldsList(&cobra.Command{}) != "" {
		t.Fatal("empty filter help is nonempty")
	}
}

func TestSandboxTemplateTransportAndTimeoutBoundaries(t *testing.T) {
	resource := spec.ResourceSpec{Product: "sandbox", Resource: "template", Operations: map[string]spec.Operation{}}
	operation := spec.Operation{Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "public"}}, Controls: spec.OperationFields{{Name: "no_wait"}, {Name: "timeout"}}}}
	command := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().Bool("openapi", false, "")
		cmd.Flags().Bool("public", false, "")
		cmd.Flags().Bool("no-wait", false, "")
		cmd.Flags().Duration("timeout", 0, "")
		for _, name := range []string{"team-id", "runtime-config", "build-config", "region", "tag", "limit", "next-token"} {
			cmd.Flags().String(name, "", "")
		}
		return cmd
	}
	for _, name := range []string{"team-id", "runtime-config", "build-config", "no-wait", "timeout"} {
		cmd := command()
		cmd.Flags().Lookup(name).Changed = true
		if _, _, err := sandboxTemplateTransport(cmd, resource, "update", operation); err == nil {
			t.Fatalf("legacy accepted --%s", name)
		}
	}
	cmd := command()
	if _, _, err := sandboxTemplateTransport(cmd, resource, "update", operation); err == nil {
		t.Fatal("legacy update allowed implicit visibility")
	}
	if err := cmd.Flags().Set("public", "false"); err != nil {
		t.Fatal(err)
	}
	_, legacy, err := sandboxTemplateTransport(cmd, resource, "update", operation)
	if err != nil || !legacy.Input.Fields[0].Required || len(legacy.Input.Controls) != 0 || operation.Input.Fields[0].Required {
		t.Fatalf("legacy contract or source mutation: %+v, %v", legacy, err)
	}
	for _, name := range []string{"public", "tag", "limit", "next-token", "region"} {
		cmd := command()
		if err := cmd.Flags().Set("openapi", "true"); err != nil {
			t.Fatal(err)
		}
		cmd.Flags().Lookup(name).Changed = true
		if _, _, err := sandboxTemplateTransport(cmd, resource, "get", operation); err == nil {
			t.Fatalf("native get accepted incompatible --%s", name)
		}
	}
	cmd = command()
	if err := cmd.Flags().Set("openapi", "true"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"create", "update"} {
		resolved, op, err := sandboxTemplateTransport(cmd, resource, action, operation)
		if err != nil || resolved.Provider != "aliyun" || resolved.Kind != "regional" {
			t.Fatalf("native transport = %+v, %v", resolved, err)
		}
		if action == "update" && len(op.RequireAny) != 2 {
			t.Fatal("native update lost required configuration")
		}
	}
	input := map[string]any{"openapi": true}
	if _, err := sandboxTemplateRequestTimeout(cmd, resource, "create", input, 0); err == nil {
		t.Fatal("unbounded native build wait accepted")
	}
	input["no_wait"] = true
	if timeout, err := sandboxTemplateRequestTimeout(cmd, resource, "create", input, 0); err != nil || timeout != 30*time.Second || input["timeout"] != timeout {
		t.Fatalf("no-wait timeout = %s, %v", timeout, err)
	}
}
