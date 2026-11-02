package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestOperationExecutionPropagatesInvalidSelectorsConditionsAndEmit(t *testing.T) {
	resource := spec.ResourceSpec{Probes: map[string]spec.Probe{"read": {API: "Read"}}}
	for _, operation := range []spec.Operation{
		{Call: spec.OperationCall{Probe: "read", IDs: []string{"$unknown"}}},
		{Call: spec.OperationCall{Probe: "missing"}},
		{Call: spec.OperationCall{Probe: "read"}, Emit: map[string]any{"fields": map[string]any{"bad": "$unknown"}}},
	} {
		if _, err := NewExecutor(resource, &fakeCaller{}).executeOperationCall(context.Background(), Request{}, operation); err == nil {
			t.Fatalf("invalid operation call accepted: %+v", operation)
		}
	}
	for _, step := range []spec.WorkflowStep{{When: "unknown"}, {Unless: "unknown"}, {Wait: "wait", IDs: []string{"$unknown"}}, {Probe: "read", IDs: []string{"$unknown"}}, {Binding: "missing"}} {
		if _, err := NewExecutor(resource, &fakeCaller{}).executeOperationWorkflow(context.Background(), Request{}, spec.Operation{Workflow: []spec.WorkflowStep{step}}); err == nil {
			t.Fatalf("invalid workflow step accepted: %+v", step)
		}
	}
	for _, binding := range []spec.Binding{
		{Each: "$unknown"},
		{Each: "$.items", Capture: map[string]any{"name": "items", "fields": map[string]any{"bad": "$unknown"}}},
		{Each: "$.items", Request: map[string]any{"Bad": "$unknown"}},
		{RequireAny: []spec.Requirement{{Raw: "$.missing"}}},
		{Hooks: spec.BindingHooks{Before: []string{"missing"}}},
	} {
		resource.Bindings = map[string]spec.Binding{"change": binding}
		ctx := ExecutionContext{Input: map[string]any{"items": []any{"one"}}, Context: map[string]any{}}
		if _, err := NewExecutor(resource, &fakeCaller{}).runBinding(context.Background(), Request{}, spec.WorkflowStep{Binding: "change"}, ctx, &Result{}); err == nil {
			t.Fatalf("invalid binding accepted: %+v", binding)
		}
	}
	for _, expression := range []string{"foo()", "bad name(input.id)"} {
		if _, _, ok := conditionFunction(expression); ok {
			t.Fatalf("invalid function syntax accepted: %s", expression)
		}
	}
	result := Result{}
	recordCapture(&result, ExecutionContext{}, "items", CaptureResult{Items: []map[string]any{{"id": "one"}}})
	if len(result.Captures["items"].Items) != 1 {
		t.Fatal("initial capture lost")
	}
}

func TestBindingResponseValidationRejectsMissingEvidenceAndReportsFallbackStatus(t *testing.T) {
	binding := spec.Binding{API: "Change", Response: spec.BindingResponse{Items: "$.items", Status: "$.status", Success: []string{"Success"}}}
	for _, response := range []map[string]any{{}, {"items": []any{}}} {
		if check := checkBindingResponse(binding, response, ExecutionContext{}); check.err == nil {
			t.Fatalf("missing response items accepted: %#v", response)
		}
	}
	binding.Response.Match = spec.BindingResponseMatch{Capture: "requested", By: []string{"id"}, Fields: map[string]string{"id": "$.id"}}
	if check := checkBindingResponse(binding, map[string]any{"items": []any{map[string]any{"id": "one", "status": "Success"}}}, ExecutionContext{}); check.err == nil {
		t.Fatal("unrecorded request capture accepted")
	}
	binding.Response.Match = spec.BindingResponseMatch{}
	check := checkBindingResponse(binding, map[string]any{"items": []any{map[string]any{"status": "Failed"}}}, ExecutionContext{})
	if check.err == nil || len(check.actions) != 1 || check.actions[0].Code != "Failed" || !strings.Contains(check.actions[0].Message, "Change returned status Failed") {
		t.Fatalf("missing diagnostic fallback: %+v", check)
	}
}

func TestMappingArgumentsRejectAmbiguousEntriesAndPreserveFallbackTypes(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"value": 42, "tags": []string{"one=a", "two=b"}, "empty": []string{" "}}}
	if got, ok, err := ResolveExpression("$", ctx); err != nil || !ok || !reflect.DeepEqual(got, ctx.Input) {
		t.Fatalf("root input mapping=%v,%t,%v", got, ok, err)
	}
	if got, ok, err := ResolveExpression("$list(input.value)", ctx); err != nil || !ok || !reflect.DeepEqual(got, []string{"42"}) {
		t.Fatalf("scalar list mapping=%v,%t,%v", got, ok, err)
	}
	for _, expression := range []string{"$kv_value(input.tags)", "$kv_unknown(input.tags)", "$kv_json(invalid)"} {
		if _, _, err := ResolveExpression(expression, ctx); err == nil {
			t.Fatalf("invalid mapping accepted: %s", expression)
		}
	}
	if _, found, err := resolveKeyValueFunction("$kv_json", "input.empty", ctx); err != nil || found {
		t.Fatalf("empty tags produced mapping: %t,%v", found, err)
	}
	for _, raw := range []string{"a,b", "=value", "key=one,key=two"} {
		if _, err := inlineKeyValueObject(raw, "key"); err == nil {
			t.Fatalf("ambiguous inline object accepted: %s", raw)
		}
	}
	for _, raw := range []string{"+", "+key=one,key=two"} {
		if _, err := prefixedKeyValueObjects([]string{raw}, "+", "key"); err == nil {
			t.Fatalf("invalid prefixed object accepted: %s", raw)
		}
	}
	for _, path := range []string{"plain", "$invalid"} {
		if _, ok := ExtractPath(ctx.Input, path); ok {
			t.Fatalf("non-path accepted: %s", path)
		}
	}
	if ExtractString(map[string]any{"value": []any{}}, "$.value") != "" || ExtractString(map[string]any{"value": json.Number("42")}, "$.value") != "42" {
		t.Fatal("string extraction fallback changed")
	}
}
