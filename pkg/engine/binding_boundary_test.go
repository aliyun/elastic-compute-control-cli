package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestBindingExpansionPropagatesInvalidNestedExpressionsAndCaptures(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"items": []any{map[string]any{"value": "one"}}}}
	for _, node := range []any{
		map[string]any{"from": "$unknown"},
		map[string]any{"each": true},
		map[string]any{"each": "$.items", "fields": map[string]any{"Value": "$unknown"}},
		map[string]any{"each": "$.items", "capture": map[string]any{"name": "items", "fields": map[string]any{"value": "$unknown"}}},
	} {
		if _, _, err := ResolveBindingRequest(spec.Binding{Request: map[string]any{"Items": node}}, ctx); err == nil {
			t.Fatalf("invalid nested mapping accepted: %#v", node)
		}
	}
	if _, err := enhancedSourceItems(map[string]any{"from_fields": map[string]any{"value": "$unknown"}}, ctx); err == nil {
		t.Fatal("invalid source expression accepted")
	}
	if _, err := enhancedSourceItems(map[string]any{}, ctx); err == nil {
		t.Fatal("empty enhanced source accepted")
	}
	if _, err := normalizeEnhancedEachItem(spec.ResourceSpec{}, "missing-normalizer", nil, nil, nil, 0); err == nil {
		t.Fatal("unknown normalizer accepted")
	}
	if _, err := normalizeEnhancedEachItem(spec.ResourceSpec{}, "", map[string]any{"value": "one"}, nil, map[string][]string{"missing": {"valid"}, "value": {}}, 0); err != nil {
		t.Fatalf("optional enum without value failed: %v", err)
	}
	request, _, err := ResolveBindingRequest(spec.Binding{Request: map[string]any{"Outer": map[string]any{"Value": "$.value", "ApiParam": map[string]any{"raw": "$.raw"}}}}, ExecutionContext{Input: map[string]any{"value": "one", "raw": []string{"Extra=two"}}})
	if err != nil || request["Outer.Value"] != "one" || request["Extra"] != "two" {
		t.Fatalf("nested raw parameter expansion=%v,%v", request, err)
	}
	captured, err := captureItems([]any{"one"}, nil, ExecutionContext{})
	if err != nil || !reflect.DeepEqual(captured, []map[string]any{{"value": "one"}}) {
		t.Fatalf("scalar capture=%v,%v", captured, err)
	}
	if got, err := captureItems([]any{"one"}, map[string]any{"invalid": true}, ExecutionContext{}); err != nil || len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("non-expression capture field=%v,%v", got, err)
	}
	if _, _, ok := parseCaptureSpec(map[string]any{"name": ""}); ok {
		t.Fatal("empty capture name accepted")
	}
}

func TestBindingScalarFallbacksPreserveValuesWithoutPanics(t *testing.T) {
	if got := decodeJSONValue("{invalid"); got != "{invalid" {
		t.Fatalf("invalid JSON scalar changed: %v", got)
	}
	if got := jsonObjectString("original", map[string]any{"invalid": make(chan int)}); got != "original" {
		t.Fatalf("unencodable object lost original: %s", got)
	}
	if got := jsonObjectString(42, map[string]any{"invalid": make(chan int)}); got != "42" {
		t.Fatalf("unencodable object lost fallback: %s", got)
	}
	if got := mapValue(map[string]string{"value": "one"}); !reflect.DeepEqual(got, map[string]any{"value": "one"}) {
		t.Fatalf("string map conversion=%v", got)
	}
	if mapValue(true) != nil || stringListValue(" ") != nil {
		t.Fatal("invalid map or blank list produced values")
	}
	for _, test := range []struct {
		value any
		want  string
	}{{nil, ""}, {json.Number("42"), "42"}, {42, "42"}} {
		if got := stringValue(test.value); got != test.want {
			t.Fatalf("scalar conversion=%s", got)
		}
	}
}

func TestWaiterCollectionMatchingAndLegacyEmitsPreserveContracts(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"wanted": []string{"one"}}}
	for _, match := range []spec.WaiterMatch{{Contains: map[string]string{"values": "$.wanted"}}, {Excludes: map[string]string{"values": "$.wanted"}}, {Contains: map[string]string{"values": "$.absent"}}, {Excludes: map[string]string{"values": "$.absent"}}} {
		items := []map[string]any{{"values": []string{}}}
		if len(match.Excludes) > 0 {
			items[0]["values"] = []string{"one"}
		}
		if waiterRequestedFieldsMatched(match, items, ctx) {
			t.Fatalf("unmatched collection declared complete: %+v", match)
		}
	}
	if waiterRequestedFieldsMatched(spec.WaiterMatch{}, nil, ctx) || captureItemsMatched(nil, CaptureResult{}, nil) {
		t.Fatal("empty waiter data matched")
	}
	if got := waiterState(spec.Waiter{Target: "ready", Pending: []spec.WaiterPending{{Field: "phase", Values: []string{"preparing"}}}}, spec.Probe{}, ProbeResult{Items: []map[string]any{{"status": "pending", "phase": "preparing"}}}, nil); got != "preparing" {
		t.Fatalf("pending state lost: %s", got)
	}
	if got := waiterState(spec.Waiter{Match: spec.WaiterMatch{StateOnly: true}}, spec.Probe{}, ProbeResult{}, nil); got != "" {
		t.Fatalf("empty state-only result=%s", got)
	}
	if _, _, err := conditionStartsWith("input.absent,'prefix'", ExecutionContext{}); err != nil {
		t.Fatalf("missing starts-with operand=%v", err)
	}
	if got := maxAttempts(time.Duration(1<<63-1), time.Nanosecond); got < 1 {
		t.Fatalf("overflowed retry count=%d", got)
	}
	result := Result{Items: []map[string]any{{"id": "one"}}}
	if err := applyNamedEmit(&result, "vpc_from_probe", ExecutionContext{}); err != nil || result.ID != "one" || result.Item["id"] != "one" {
		t.Fatalf("legacy probe emit=%+v,%v", result, err)
	}
	result = Result{}
	if err := applyNamedEmit(&result, "vpc_from_context", ExecutionContext{Input: map[string]any{"id": "one"}}); err != nil || result.Item["id"] != "one" {
		t.Fatalf("legacy context emit=%+v,%v", result, err)
	}
	if err := applyEmit(&result, 42, ExecutionContext{}); err != nil {
		t.Fatal(err)
	}
	if err := applyEmit(&result, "", ExecutionContext{}); err != nil {
		t.Fatal(err)
	}
	if err := applyEmitMap(&result, map[string]any{"fields": map[string]any{"invalid": "$unknown"}}, ExecutionContext{}); err == nil {
		t.Fatal("invalid emitted expression ignored")
	}
	if err := applyEmitMap(&result, map[string]any{"deleted": true}, ExecutionContext{}); err != nil || !result.Deleted {
		t.Fatalf("delete emit=%+v,%v", result, err)
	}
	result = Result{}
	appendWorkflowProbeResult(&result, ProbeResult{Items: []map[string]any{{"id": "one"}}})
	if result.Total != 1 {
		t.Fatalf("total without reported count=%d", result.Total)
	}
	var actions []ecerrors.Action
	recordAction(&actions, ecerrors.Action{})
	if len(actions) != 0 || len(appendCapability(nil, "")) != 0 {
		t.Fatal("empty evidence recorded")
	}
	if !strings.Contains(jsonObjectString(map[string]any{}, map[string]any{"value": "one"}), "value") {
		t.Fatal("valid object lost")
	}
}
