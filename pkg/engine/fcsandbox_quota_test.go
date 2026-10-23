package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func quotaPage(id, next, requestID string) map[string]any {
	return map[string]any{"quotas": []any{map[string]any{"tagValue": id, "cpuCores": 32, "memoryGB": 64}}, "nextToken": next, "requestId": requestID}
}

func TestQuotaListAllFollowsOpaqueTokenToOwnedTeam(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{responses: []map[string]any{quotaPage("unrelated-team", "opaque-page-2", "req-1"), quotaPage("owned-team", "", "req-2")}}
	result, err := NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ListQuota" || fake.calls[1].operation != "ListQuota" || fake.calls[0].request["maxResults"] != 1 || fake.calls[1].request["maxResults"] != 1 {
		t.Fatalf("quota list page requests = %+v", fake.calls)
	}
	if first := fake.calls[0].request["nextToken"]; first != nil && first != "" {
		t.Fatalf("first page sent a token: %+v", fake.calls[0])
	}
	if fake.calls[1].request["nextToken"] != "opaque-page-2" || len(result.Items) != 2 || result.Items[0]["id"] != "unrelated-team" || result.Items[1]["id"] != "owned-team" || result.NextToken != "" {
		t.Fatalf("owned quota was not found through the opaque token: %+v; result=%+v", fake.calls, result)
	}
	if len(result.Actions) != 2 || result.Actions[0].RequestID != "req-1" || result.Actions[1].RequestID != "req-2" {
		t.Fatalf("page action trace = %+v", result.Actions)
	}
}

func TestQuotaListAllContinuesThroughEmptyTokenPage(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{responses: []map[string]any{
		{"quotas": []any{}, "nextToken": "opaque-page-2", "requestId": "req-empty"},
		quotaPage("owned-team", "", "req-final"),
	}}
	result, err := NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 100, "all": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 || fake.calls[1].request["nextToken"] != "opaque-page-2" || len(result.Items) != 1 || result.Items[0]["id"] != "owned-team" || len(result.Actions) != 2 || result.Actions[0].RequestID != "req-empty" || result.Actions[1].RequestID != "req-final" {
		t.Fatalf("empty continuation lost data or evidence: calls=%+v result=%+v", fake.calls, result)
	}
}

func TestQuotaListAllRejectsIncompleteTokenPages(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		pages []map[string]any
	}{
		{"repeated token", []map[string]any{quotaPage("team-1", "same-token", "req-1"), quotaPage("team-2", "same-token", "req-2")}},
		{"empty repeated token", []map[string]any{{"quotas": []any{}, "nextToken": "page-2", "requestId": "req-1"}, {"quotas": []any{}, "nextToken": "page-2", "requestId": "req-2"}}},
		{"duplicate team", []map[string]any{quotaPage("team-1", "page-2", "req-1"), quotaPage("team-1", "", "req-2")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCaller{responses: tc.pages}
			_, err := NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true}})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "IncompletePagination" || len(app.Actions()) != 2 {
				t.Fatalf("incomplete token pagination = %v; calls=%+v", err, fake.calls)
			}
		})
	}
	pages := make([]map[string]any, 101)
	for i := range pages {
		pages[i] = quotaPage(fmt.Sprintf("team-%d", i), fmt.Sprintf("token-%d", i+1), fmt.Sprintf("req-%d", i))
	}
	fake := &fakeCaller{responses: pages}
	_, err = NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "IncompletePagination" || len(fake.calls) != 100 {
		t.Fatalf("token page limit = %v, calls=%d", err, len(fake.calls))
	}
}

func TestQuotaListAllRejectsStartingTokenAndKeepsFailingPage(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCaller{}
	_, err = NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true, "next_token": "resume-token"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "InvalidAllPages" || len(fake.calls) != 0 {
		t.Fatalf("all-pages starting token reached provider: %v; calls=%+v", err, fake.calls)
	}
	fake = &fakeCaller{responses: []map[string]any{quotaPage("other-team", "page-2", "req-1")}, errors: []error{nil, ecerrors.Service("CloudAPIError", "second page failed", false, ecerrors.WithRawCause("QuotaBusy", "second page failed"), ecerrors.WithRequestID("req-2"))}}
	_, err = NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true}})
	if !errors.As(err, &app) || app.Payload().Code != "CloudAPIError" || len(app.Actions()) != 2 || app.Actions()[0].RequestID != "req-1" || app.Actions()[1].RequestID != "req-2" || app.Actions()[1].Code != "QuotaBusy" {
		t.Fatalf("failing second page evidence = %v, calls=%+v", err, fake.calls)
	}
}

func TestQuotaDeleteWaitsForOwnedIDAbsence(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	wait := resource.Waiters["absent_after_delete"]
	wait.Interval, wait.Timeout = "1ms", "50ms"
	resource.Waiters["absent_after_delete"] = wait
	stale := map[string]any{"code": "200", "requestId": "req-stale", "quota": map[string]any{"tagValue": "team-1", "cpuCores": 32, "memoryGB": 64}}
	fake := &fakeCaller{responses: []map[string]any{{"code": "200", "requestId": "req-delete"}, stale}, errors: []error{nil, nil, ecerrors.NotFound("NotFound", "gone", ecerrors.WithRawCause("ResourceQuotaNotFound", "gone"), ecerrors.WithRequestID("req-absent"))}}
	result, err := NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "delete", Input: map[string]any{"id": "team-1"}})
	if err != nil || !result.Deleted || len(fake.calls) != 3 || len(result.Actions) != 3 {
		t.Fatalf("quota delete did not wait for absence: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	for _, call := range fake.calls[1:] {
		if call.operation != "DescribeQuota" || call.request["tagValue"] != "team-1" {
			t.Fatalf("absence probe changed target: %+v", call)
		}
	}
	if result.Actions[0].RequestID != "req-delete" || result.Actions[2].Code != "ResourceQuotaNotFound" || result.Actions[2].RequestID != "req-absent" {
		t.Fatalf("delete/terminal provider evidence lost: %+v", result.Actions)
	}
}

func TestQuotaDeleteAbsenceWaitIsBounded(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	wait := resource.Waiters["absent_after_delete"]
	wait.Interval, wait.Timeout = "1ms", "10ms"
	resource.Waiters["absent_after_delete"] = wait
	responses := []map[string]any{{"code": "200", "requestId": "req-delete"}}
	for i := 0; i < 30; i++ {
		responses = append(responses, map[string]any{"code": "200", "requestId": fmt.Sprintf("req-stale-%d", i), "quota": map[string]any{"tagValue": "team-1", "cpuCores": 32, "memoryGB": 64}})
	}
	fake := &fakeCaller{responses: responses}
	_, err = NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "delete", Input: map[string]any{"id": "team-1"}, Timeout: 10 * time.Millisecond})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "WaitTimeout" || len(fake.calls) < 2 || len(fake.calls) > 12 || len(app.Actions()) < 2 || app.Actions()[0].RequestID != "req-delete" {
		t.Fatalf("still-present quota did not time out with evidence: err=%v calls=%+v", err, fake.calls)
	}
}
