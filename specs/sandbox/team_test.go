package sandbox

import (
	"context"
	"errors"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	spechooks "github.com/aliyun/elastic-compute-control-cli/specs"
)

func TestFCSandboxTeamSpecIsValid(t *testing.T) {
	if _, err := spec.LoadResource("..", "sandbox", "team"); err != nil {
		t.Fatalf("Team spec must be valid for catalog inclusion: %v", err)
	}
}

type teamPreflightCaller struct {
	response map[string]any
	calls    int
}

func (c *teamPreflightCaller) CallRaw(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	c.calls++
	return c.response, nil
}

func TestTeamMutationPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, hook, returnedID string
		readonly, rename       any
		newName, wantCode      string
	}{
		{"update allowed", "validate_update", "team-1", false, true, "new", ""},
		{"delete allowed", "validate_delete", "team-1", false, false, "", ""},
		{"read only update", "validate_update", "team-1", true, true, "", "TeamReadOnly"},
		{"read only delete", "validate_delete", "team-1", true, true, "", "TeamReadOnly"},
		{"rename denied", "validate_update", "team-1", false, false, "new", "TeamRenameNotAllowed"},
		{"same name allowed", "validate_update", "team-1", false, false, "dev", ""},
		{"unknown readonly", "validate_delete", "team-1", nil, true, "", "InvalidTeamResponse"},
		{"unknown rename", "validate_update", "team-1", false, nil, "new", "InvalidTeamResponse"},
		{"wrong identity", "validate_delete", "other", false, true, "", "InvalidTeamResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook, ok := spechooks.BeforeOperationHook("sandbox", "team", tc.hook)
			if !ok {
				t.Fatalf("Team hook %s is missing", tc.hook)
			}
			caller := &teamPreflightCaller{response: map[string]any{"code": "200", "requestId": "req-preflight", "team": map[string]any{"teamID": tc.returnedID, "teamName": "dev", "readOnly": tc.readonly, "allowUpdateTeamName": tc.rename}}}
			request := map[string]any{"teamID": "team-1", "body.description": "updated"}
			if tc.newName != "" {
				request["body.teamName"] = tc.newName
			}
			result, err := hook(context.Background(), caller, request)
			if tc.wantCode == "" {
				if err != nil || result["teamID"] != "team-1" {
					t.Fatalf("allowed mutation = %#v %v", result, err)
				}
			} else {
				var appErr *ecerrors.AppError
				if !errors.As(err, &appErr) || appErr.Payload().Code != tc.wantCode {
					t.Fatalf("error = %v, want %s", err, tc.wantCode)
				}
			}
			if caller.calls != 1 {
				t.Fatalf("GetTeam calls = %d", caller.calls)
			}
		})
	}
}
