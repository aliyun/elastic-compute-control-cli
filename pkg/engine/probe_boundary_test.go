package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestProbeRejectsInvalidPageCountsAndUnsupportedAllPages(t *testing.T) {
	for _, test := range []struct {
		total, limit int
		token        bool
	}{{1, 2, false}, {3, 3, false}, {0, 1, true}} {
		probe := spec.Probe{API: "ListThings", Request: map[string]any{"page": "$.page", "limit": "$.limit"}, Response: spec.ProbeResponse{Items: "$.items", Total: "$.total", Fields: map[string]spec.ProbeField{"id": {Path: "$.id"}}}}
		if test.token {
			probe.Request = map[string]any{"next": "$.next_token", "limit": "$.limit"}
			probe.Response.NextToken = "$.next"
		}
		caller := &fakeCaller{responses: []map[string]any{{"items": []any{map[string]any{"id": "one"}, map[string]any{"id": "two"}}, "total": test.total}}}
		resource := spec.ResourceSpec{Controls: map[string]spec.SchemaField{"all": {Type: "boolean"}}, Probes: map[string]spec.Probe{"list": probe}}
		_, err := NewExecutor(resource, caller).runProbe(context.Background(), "list", ExecutionContext{Input: map[string]any{"all": true, "page": 1, "limit": test.limit}}, nil)
		var app *ecerrors.AppError
		if !errors.As(err, &app) || app.Payload().Code != "IncompletePagination" || len(app.Actions()) != 1 {
			t.Fatalf("invalid pagination lost failure/evidence: %v", err)
		}
	}
	caller := &fakeCaller{}
	executor := NewExecutor(spec.ResourceSpec{}, caller)
	if _, err := executor.runProbe(context.Background(), "missing", ExecutionContext{}, nil); err == nil {
		t.Fatal("missing probe accepted")
	}
	if _, err := executor.runProbe(context.Background(), "missing", ExecutionContext{Input: map[string]any{"all": true}}, nil); err == nil || len(caller.calls) != 0 {
		t.Fatal("unconfigured pagination reached caller")
	}
}

func TestProbeMappingsDropMissingChildrenAndPreserveScalarTypes(t *testing.T) {
	for _, field := range []spec.ProbeField{{Lower: "$.missing"}, {Int: "$.missing"}, {Port: "$.missing"}, {First: []string{"$.missing"}}, {}} {
		if _, ok := probeFieldValue(field, map[string]any{}); ok {
			t.Fatalf("missing probe field mapped: %+v", field)
		}
	}
	value, ok := probeFieldValue(spec.ProbeField{From: "$.children", Each: map[string]spec.ProbeField{"id": {Path: "$.id"}}}, map[string]any{"children": []any{1, map[string]any{"id": "one"}}})
	if !ok || !reflect.DeepEqual(value, []map[string]any{{"id": "one"}}) {
		t.Fatalf("invalid nested item retained: %#v", value)
	}
	probe := spec.Probe{Response: spec.ProbeResponse{Items: "$.items", Fields: map[string]spec.ProbeField{"id": {Path: "$.id"}}, ExtraFields: map[string]spec.ProbeField{"missing": {Path: "$.missing"}}}}
	result := mapProbeResponse(spec.ResourceSpec{}, probe, map[string]any{"items": []any{true, map[string]any{"id": "one"}}})
	if len(result.Items) != 1 || len(result.Extra) != 0 {
		t.Fatalf("invalid mapping produced data: %+v", result)
	}
	for _, test := range []struct {
		value any
		want  string
	}{{json.Number("42"), "42"}, {42, "42"}} {
		if got, ok := ExtractStringValue(map[string]any{"value": test.value}, "$.value"); !ok || got != test.want {
			t.Fatalf("string extraction=%s,%t", got, ok)
		}
	}
	if _, ok := ExtractStringValue(nil, "$.missing"); ok {
		t.Fatal("missing scalar extracted")
	}
	if stringFromMap(map[string]any{"value": json.Number("42")}, "value") != "42" || stringFromMap(map[string]any{"value": true}, "value") != "" {
		t.Fatal("probe string conversion changed")
	}
	if !isEmptyProbeOutput([]string{}) || isEmptyProbeOutput([]string{"one"}) {
		t.Fatal("empty string-list normalization changed")
	}
	for _, test := range []struct{ raw, want string }{{"", ""}, {"HTTP Status-Code", "http_status_code"}, {"some_Key", "some_key"}} {
		if got := snakeCaseOpenAPIName(test.raw); got != test.want {
			t.Fatalf("field name %s=%s", test.raw, got)
		}
	}
	if normalizeProbePort("80/443") != "80-443" || normalizeProbePort("8080") != "8080" {
		t.Fatal("port normalization lost range")
	}
}
