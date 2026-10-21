package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestTemplateCacheCreateWaitsForCacheSuccess(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{
			{"code": "200", "requestId": "req-create", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
			{"code": "200", "requestId": "req-pending", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
			{"code": "200", "requestId": "req-ready", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Success"}},
		},
	}
	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
	if err != nil || result.Item["status"] != "Success" || len(caller.calls) != 3 || caller.calls[0].operation != "CreateTemplateCache" || caller.calls[1].operation != "DescribeTemplateCache" {
		t.Fatalf("cache readiness: result=%+v err=%v calls=%+v", result, err, caller.calls)
	}
	if caller.calls[1].request["templateID"] != "tpl-1" || caller.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("wait lost Team or Template identity: %+v", caller.calls[1].request)
	}
}

func TestTemplateCacheCreateNoWaitOnlyClaimsAcceptance(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{responses: []map[string]any{{"code": "200", "requestId": "req-create", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}}}}
	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1", "no_wait": true}})
	if err != nil || result.Item["status"] != "InProgress" || len(caller.calls) != 1 {
		t.Fatalf("no-wait must return accepted state: result=%+v err=%v calls=%+v", result, err, caller.calls)
	}
}

func TestTemplateCacheCreateWaitsThroughInitialNotFound(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{
			{"code": "200", "requestId": "req-create", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
			{"code": "200", "requestId": "req-ready", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Success"}},
		},
		errors: []error{nil, ecerrors.NotFound("NotFound", "cache not visible yet"), nil},
	}
	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
	if err != nil || result.Item["status"] != "Success" || len(caller.calls) != 3 {
		t.Fatalf("eventual visibility: result=%+v err=%v calls=%+v", result, err, caller.calls)
	}
}

func TestTemplateCacheCreateFailureStateIsNotSuccess(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{responses: []map[string]any{
		{"code": "200", "requestId": "req-create", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "InProgress"}},
		{"code": "200", "requestId": "req-failed", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Failed", "statusReason": "image unavailable"}},
	}}
	_, err = NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" || app.Payload().CurrentState != "Failed" {
		t.Fatalf("failed cache reported ready: %v", err)
	}
}

func TestNativeTemplateUpdateWaitPropagatesNotFound(t *testing.T) {
	restore := SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template")
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{{"code": "200", "requestId": "req-update", "templateID": "tpl-1"}},
		errors:    []error{nil, ecerrors.NotFound("NotFound", "template was deleted")},
	}
	_, err = NewExecutor(resource, caller).Execute(context.Background(), Request{Action: "update", Input: map[string]any{
		"id": "tpl-1", "team_id": "team-1", "openapi": true, "runtime_config": map[string]any{"internetAccess": false},
	}, Timeout: time.Millisecond})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Kind != "not_found" || len(caller.calls) != 2 {
		t.Fatalf("native update swallowed deletion: err=%v calls=%+v", err, caller.calls)
	}
}
