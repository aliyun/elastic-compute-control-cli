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

func loadVolumeSpec(t *testing.T) spec.ResourceSpec {
	t.Helper()
	resource, err := spec.LoadResource("../../specs", "sandbox", "volume")
	if err != nil {
		t.Fatal(err)
	}
	return resource
}

func volumeResponse(status, requestID string) map[string]any {
	return map[string]any{"code": "200", "requestId": requestID, "volume": map[string]any{
		"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": status,
		"statusReason": "provider reason", "ossVolumeConfig": map[string]any{"bucketPath": "/run-1", "readOnly": true},
	}}
}

func TestVolumeCreateWaitsForAvailableAndRetainsScope(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	resource := loadVolumeSpec(t)
	fake := &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create"), volumeResponse("CREATING", "req-pending"), volumeResponse("AVAILABLE", "req-ready")}}
	result, err := NewExecutor(resource, fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{
		"name": "workspace", "team_id": "team-1", "oss_volume_config": map[string]any{"bucketPath": "/run-1", "readOnly": true},
	}})
	if err != nil || result.Item["status"] != "AVAILABLE" || result.Item["id"] != "vol-1" || len(fake.calls) != 3 {
		t.Fatalf("Volume readiness: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	if fake.calls[0].operation != "CreateVolume" || fake.calls[0].request["body.teamID"] != "team-1" || fake.calls[0].request["body.volumeName"] != "workspace" {
		t.Fatalf("create body: %+v", fake.calls[0])
	}
	for _, call := range fake.calls[1:] {
		if call.operation != "GetVolume" || call.request["volumeID"] != "vol-1" || call.request["teamID"] != "team-1" {
			t.Fatalf("wait probe lost Volume scope: %+v", call)
		}
	}
}

func TestVolumeCreateUsesAcceptedTeamWhenSelectorOmitted(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	fake := &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create"), volumeResponse("ERROR", "req-error")}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" || result.ID != "vol-1" || len(fake.calls) != 2 {
		t.Fatalf("omitted Team lost accepted create evidence: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	if _, sent := fake.calls[0].request["body.teamID"]; sent || fake.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("waiter did not use accepted Team: %+v", fake.calls)
	}
}

func TestVolumeCreateInitialFailureWithOmittedTeamRetainsAcceptance(t *testing.T) {
	fake := &fakeCaller{responses: []map[string]any{volumeResponse("ERROR", "req-create")}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" || result.ID != "vol-1" || result.RequestID != "req-create" || len(fake.calls) != 1 {
		t.Fatalf("initial failure lost accepted Team create: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
}

func TestVolumeCreateNoWaitReportsPendingState(t *testing.T) {
	fake := &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create")}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1", "no_wait": true}})
	if err != nil || result.Item["status"] != "CREATING" || result.ID != "vol-1" || len(fake.calls) != 1 {
		t.Fatalf("no-wait claimed readiness: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
}

func TestVolumeCreateErrorStateFailsWithReason(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	fake := &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create"), volumeResponse("ERROR", "req-error")}}
	_, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" || app.Payload().CurrentState != "ERROR" || app.Payload().Detail != "provider reason" {
		t.Fatalf("ERROR was not a failed create: %v", err)
	}
}

func TestVolumeCreateInitialErrorPreservesAcceptanceAndReason(t *testing.T) {
	for _, state := range []string{"ERROR", "Error", "error"} {
		for _, noWait := range []bool{false, true} {
			t.Run(state+"/"+map[bool]string{false: "default", true: "no_wait"}[noWait], func(t *testing.T) {
				fake := &fakeCaller{responses: []map[string]any{volumeResponse(state, "req-create")}}
				result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1", "no_wait": noWait}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" || app.Payload().CurrentState != state || app.Payload().Detail != "provider reason" || result.ID != "vol-1" || result.RequestID != "req-create" || len(result.Actions) != 1 || len(fake.calls) != 1 {
					t.Fatalf("initial ERROR lost accepted identity or reason: result=%+v err=%v calls=%+v", result, err, fake.calls)
				}
			})
		}
	}
}

func TestVolumeCreateRetainsAcceptedIDWhenReadinessFails(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	for _, test := range []struct {
		name string
		fake *fakeCaller
	}{
		{name: "failed state", fake: &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create"), volumeResponse("ERROR", "req-error")}}},
		{name: "transient read", fake: &fakeCaller{responses: []map[string]any{volumeResponse("CREATING", "req-create")}, errors: []error{nil, ecerrors.Service("TransportError", "connection reset", true)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := NewExecutor(loadVolumeSpec(t), test.fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1"}, Timeout: 10 * time.Millisecond})
			if err == nil || result.ID != "vol-1" || len(test.fake.calls) < 2 || test.fake.calls[0].operation != "CreateVolume" || test.fake.calls[1].operation != "GetVolume" {
				t.Fatalf("accepted Volume ID lost: result=%+v err=%v call count=%d", result, err, len(test.fake.calls))
			}
		})
	}
}

type cancelledVolumeWaitCaller struct{ cancel context.CancelFunc }

func (c cancelledVolumeWaitCaller) Call(_ context.Context, operation string, _ map[string]any) (map[string]any, error) {
	if operation == "CreateVolume" {
		c.cancel()
		return volumeResponse("CREATING", "req-create"), nil
	}
	return nil, context.Canceled
}

func TestVolumeCreateRetainsAcceptedIDAfterInterruption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := NewExecutor(loadVolumeSpec(t), cancelledVolumeWaitCaller{cancel: cancel}).Execute(ctx, Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1"}})
	if err == nil || result.ID != "vol-1" {
		t.Fatalf("interrupted wait lost accepted Volume ID: result=%+v err=%v", result, err)
	}
}

func TestVolumeCreateDoesNotClaimIDBeforeAcceptance(t *testing.T) {
	fake := &fakeCaller{errors: []error{ecerrors.Service("TransportError", "connection reset", true)}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1"}})
	if err == nil || result.ID != "" || len(fake.calls) != 1 || fake.calls[0].operation != "CreateVolume" {
		t.Fatalf("unaccepted Volume create claimed an ID: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
}

func TestVolumeDeleteWaitsForScopedAbsence(t *testing.T) {
	restore := SetTimeScaleForTest(0.001)
	defer restore()
	fake := &fakeCaller{responses: []map[string]any{{"code": "200", "requestId": "req-delete"}, volumeResponse("DELETING", "req-pending")},
		errors: []error{nil, nil, ecerrors.NotFound("NotFound", "gone", ecerrors.WithRawCause("VolumeNotFound", "gone"), ecerrors.WithRequestID("req-absent"))}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "delete", Input: map[string]any{"id": "vol-1", "team_id": "team-1"}})
	if err != nil || result.Deleted || result.Item["absent_in_scope"] != true || len(fake.calls) != 3 || len(result.Actions) != 3 || result.Actions[2].Code != "VolumeNotFound" {
		t.Fatalf("delete did not confirm absence: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	for _, call := range fake.calls[1:] {
		if call.operation != "GetVolume" || call.request["volumeID"] != "vol-1" || call.request["teamID"] != "team-1" {
			t.Fatalf("delete probe lost scope: %+v", call)
		}
	}
	noWait := &fakeCaller{responses: []map[string]any{{"code": "200", "requestId": "req-delete"}}}
	result, err = NewExecutor(loadVolumeSpec(t), noWait).Execute(context.Background(), Request{Action: "delete", Input: map[string]any{"id": "vol-1", "no_wait": true}})
	if err != nil || result.Deleted || len(noWait.calls) != 1 {
		t.Fatalf("no-wait claimed deletion: result=%+v err=%v", result, err)
	}
}

func TestVolumeRejectsExplicitEmptyTeamAndExactID(t *testing.T) {
	for _, tc := range []struct {
		action string
		input  map[string]any
	}{
		{"create", map[string]any{"name": "workspace", "team_id": ""}},
		{"list", map[string]any{"team_id": ""}},
		{"get", map[string]any{"id": "vol-1", "team_id": ""}},
		{"update", map[string]any{"id": "vol-1", "team_id": ""}},
		{"delete", map[string]any{"id": "vol-1", "team_id": ""}},
		{"get", map[string]any{"id": ""}},
		{"update", map[string]any{"id": "", "team_id": "team-1", "oss_volume_config": map[string]any{"bucketName": "bucket"}}},
		{"delete", map[string]any{"id": ""}},
	} {
		t.Run(tc.action+"/"+fmt.Sprint(tc.input), func(t *testing.T) {
			fake := &fakeCaller{}
			_, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: tc.action, Input: tc.input})
			if err == nil || len(fake.calls) != 0 {
				t.Fatalf("invalid selector reached provider: err=%v calls=%+v", err, fake.calls)
			}
		})
	}
}

func TestVolumeListAllFollowsOpaqueToken(t *testing.T) {
	page := func(id, token string) map[string]any {
		return map[string]any{"code": "200", "volumes": []any{map[string]any{"volumeID": id, "teamID": "team-1", "status": "AVAILABLE"}}, "nextToken": token}
	}
	fake := &fakeCaller{responses: []map[string]any{page("vol-1", "opaque-page-2"), page("vol-2", "")}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"team_id": "team-1", "limit": 1, "all": true}})
	if err != nil || len(result.Items) != 2 || len(fake.calls) != 2 || fake.calls[1].request["nextToken"] != "opaque-page-2" || fake.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("token pagination: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	for _, pages := range [][]map[string]any{{page("vol-1", "same"), page("vol-2", "same")}, {page("vol-1", "next"), page("vol-1", "")}} {
		fake = &fakeCaller{responses: pages}
		_, err = NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "list", Input: map[string]any{"limit": 1, "all": true}})
		var app *ecerrors.AppError
		if !errors.As(err, &app) || app.Payload().Code != "IncompletePagination" {
			t.Fatalf("incomplete token list succeeded: %v", err)
		}
	}
}

func volumeTeamResponse(team, status, requestID string) map[string]any {
	return map[string]any{"code": "200", "requestId": requestID, "volume": map[string]any{
		"volumeID": "vol-1", "volumeName": "workspace", "teamID": team, "status": status,
	}}
}

func TestVolumeUpdateReplacesConfigAndReadsBackSameTeam(t *testing.T) {
	ossConfig := map[string]any{"bucketName": "bucket", "bucketPath": "/", "endpoint": "https://oss.example", "readOnly": false}
	fake := &fakeCaller{responses: []map[string]any{volumeTeamResponse("team-1", "AVAILABLE", "req-update"), volumeTeamResponse("team-1", "AVAILABLE", "req-read")}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "update", Input: map[string]any{"id": "vol-1", "team_id": "team-1", "oss_volume_config": ossConfig}})
	if err != nil || result.Item["team_id"] != "team-1" || result.ID != "vol-1" || len(fake.calls) != 2 {
		t.Fatalf("Volume config update: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
	if fake.calls[0].operation != "UpdateVolume" || fake.calls[0].request["volumeID"] != "vol-1" || fake.calls[0].request["teamID"] != "team-1" {
		t.Fatalf("update request lost the path identity or Team query scope: %+v", fake.calls[0])
	}
	if sent, _ := fake.calls[0].request["body.ossVolumeConfig"].(string); sent != `{"bucketName":"bucket","bucketPath":"/","endpoint":"https://oss.example","readOnly":false}` {
		t.Fatalf("update did not send the storage configuration body: %+v", fake.calls[0])
	}
	for _, key := range []string{"body.teamID", "body.status"} {
		if _, sent := fake.calls[0].request[key]; sent {
			t.Fatalf("update sent the unsupported body field %s: %+v", key, fake.calls[0])
		}
	}
	if fake.calls[1].operation != "GetVolume" || fake.calls[1].request["volumeID"] != "vol-1" || fake.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("readback lost the same-Team scope: %+v", fake.calls[1])
	}
}

func TestVolumeUpdateRequiresTeamScope(t *testing.T) {
	fake := &fakeCaller{}
	_, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "update", Input: map[string]any{"id": "vol-1", "oss_volume_config": map[string]any{"bucketName": "bucket"}}})
	if err == nil || len(fake.calls) != 0 {
		t.Fatalf("update without the Team scope reached provider: err=%v calls=%+v", err, fake.calls)
	}
}

func TestVolumeUpdateRequiresConfiguration(t *testing.T) {
	fake := &fakeCaller{}
	_, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "update", Input: map[string]any{"id": "vol-1", "team_id": "team-1"}})
	if err == nil || len(fake.calls) != 0 {
		t.Fatalf("update without a storage configuration reached provider: err=%v calls=%+v", err, fake.calls)
	}
}

func TestVolumeUpdateBusinessErrorFails(t *testing.T) {
	fake := &fakeCaller{errors: []error{ecerrors.Service("CloudAPIError", "denied", false, ecerrors.WithRequestID("req-update"))}}
	result, err := NewExecutor(loadVolumeSpec(t), fake).Execute(context.Background(), Request{Action: "update", Input: map[string]any{"id": "vol-1", "team_id": "team-1", "oss_volume_config": map[string]any{"bucketName": "bucket"}}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || result.Item != nil || len(fake.calls) != 1 || fake.calls[0].operation != "UpdateVolume" {
		t.Fatalf("business error became success: result=%+v err=%v calls=%+v", result, err, fake.calls)
	}
}
