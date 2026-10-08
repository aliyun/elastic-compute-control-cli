package engine

import (
	"context"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestAbsenceWaiterRetainsFailureState(t *testing.T) {
	result := ProbeResult{Items: []map[string]any{{"id": "team-1", "status": "delete_failed"}}}
	wait := spec.Waiter{Target: "absent", Failure: spec.WaiterFailure{States: []string{"delete_failed"}}}
	if got := waiterState(wait, spec.Probe{}, result, []string{"team-1"}); got != "delete_failed" {
		t.Fatalf("absence waiter state = %q, want delete_failed", got)
	}
}

func TestProbeAllPagesPreservesFiltersAndRejectsIncompleteResults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		second    map[string]any
		wantError bool
	}{
		{"complete", map[string]any{"total": 2, "teams": []any{map[string]any{"teamID": "team-2"}}}, false},
		{"empty middle page", map[string]any{"total": 2, "teams": []any{}}, true},
		{"duplicate", map[string]any{"total": 2, "teams": []any{map[string]any{"teamID": "team-1"}}}, true},
		{"total changed", map[string]any{"total": 3, "teams": []any{map[string]any{"teamID": "team-2"}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &fakeCaller{responses: []map[string]any{{"total": 2, "teams": []any{map[string]any{"teamID": "team-1"}}}, tc.second}}
			resource := spec.ResourceSpec{Controls: map[string]spec.SchemaField{"all": {Type: "boolean"}}, Probes: map[string]spec.Probe{"list": {
				API: "ListTeams", Request: map[string]any{"pageNumber": "$.page", "pageSize": "$.limit", "teamName": "$.name"},
				Response: spec.ProbeResponse{Items: "$.teams", Total: "$.total", ID: "$.teamID", Fields: map[string]spec.ProbeField{"id": {Path: "$.teamID"}}},
			}}}
			result, err := NewExecutor(resource, caller).runProbe(context.Background(), "list", ExecutionContext{Input: map[string]any{"all": true, "page": 1, "limit": 1, "name": "dev"}}, nil)
			if tc.wantError {
				if err == nil {
					t.Fatalf("incomplete results succeeded: %#v", result)
				}
				return
			}
			if err != nil || len(result.Items) != 2 || len(caller.calls) != 2 {
				t.Fatalf("aggregation: %#v %v calls=%#v", result, err, caller.calls)
			}
			if caller.calls[1].request["pageNumber"] != 2 || caller.calls[1].request["teamName"] != "dev" {
				t.Fatalf("second page = %#v", caller.calls[1])
			}
		})
	}
}
