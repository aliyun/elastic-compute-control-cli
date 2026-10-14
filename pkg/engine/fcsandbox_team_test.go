package engine

import (
	"context"
	"errors"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestTeamCreateReturnsCreatedPayloadWithoutReadback(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "team")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{{"code": "200", "requestId": "req-create", "team": map[string]any{"teamID": "created-team-1", "status": "active", "teamName": "dev", "readOnly": false}}},
		errors:    []error{nil, ecerrors.Service("CloudAPIError", "dial tcp: readback unavailable", true)},
	}
	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "dev"}})
	if err != nil || result.ID != "created-team-1" || result.Item["id"] != "created-team-1" || result.Item["status"] != "active" || result.Item["read_only"] != false || len(caller.calls) != 1 || caller.calls[0].operation != "CreateTeam" {
		t.Fatalf("create lost validated resource to readback: %#v %v calls=%#v", result, err, caller.calls)
	}
}

func TestProbeAllPagesPreservesFailingAction(t *testing.T) {
	caller := &fakeCaller{
		responses: []map[string]any{{"requestId": "req-page-1", "total": 2, "teams": []any{map[string]any{"teamID": "team-1"}}}},
		errors:    []error{nil, ecerrors.Service("CloudAPIError", "provider rejected page 2", false, ecerrors.WithRawCause("409", "provider rejected page 2"), ecerrors.WithRequestID("req-page-2"))},
	}
	resource := spec.ResourceSpec{Controls: map[string]spec.SchemaField{"all": {Type: "boolean"}}, Probes: map[string]spec.Probe{"list": {API: "ListTeams", Request: map[string]any{"pageNumber": "$.page", "pageSize": "$.limit"}, Response: spec.ProbeResponse{Items: "$.teams", Total: "$.total", RequestID: "$.requestId", Fields: map[string]spec.ProbeField{"id": {Path: "$.teamID"}}}}}}
	_, err := NewExecutor(resource, caller).runProbe(context.Background(), "list", ExecutionContext{Input: map[string]any{"all": true, "page": 1, "limit": 1}}, nil)
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected failed page, got %v", err)
	}
	actions := appErr.Actions()
	if len(actions) != 2 || actions[0].RequestID != "req-page-1" || actions[1].RequestID != "req-page-2" || actions[1].Code != "409" || actions[1].Message != "provider rejected page 2" {
		t.Fatalf("failing page evidence lost: %#v", actions)
	}
}

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
