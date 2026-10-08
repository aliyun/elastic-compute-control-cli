package sandbox

import (
	"context"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	spechooks "github.com/aliyun/elastic-compute-control-cli/specs"
)

func init() {
	spechooks.RegisterBeforeOperation("sandbox", "team", "validate_update", validateTeamUpdate)
	spechooks.RegisterBeforeOperation("sandbox", "team", "validate_delete", validateTeamDelete)
}

func validateTeamUpdate(ctx context.Context, caller spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	return validateTeamMutation(ctx, caller, request, true)
}

func validateTeamDelete(ctx context.Context, caller spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	return validateTeamMutation(ctx, caller, request, false)
}

func validateTeamMutation(ctx context.Context, caller spechooks.OperationCaller, request map[string]any, update bool) (map[string]any, error) {
	response, err := caller.CallRaw(ctx, "GetTeam", map[string]any{"teamID": request["teamID"]})
	if err != nil {
		return nil, ecerrors.WithActions(err, []ecerrors.Action{ecerrors.ActionFromError("GetTeam", err)})
	}
	requestID, _ := response["requestId"].(string)
	fail := func(code string) (map[string]any, error) {
		err := ecerrors.Client(code, i18n.NewLocalizer("en").Message(code))
		return nil, ecerrors.WithActions(err, []ecerrors.Action{{ActionName: "GetTeam", RequestID: requestID}})
	}
	team, ok := response["team"].(map[string]any)
	if !ok || team["teamID"] != request["teamID"] {
		return fail("InvalidTeamResponse")
	}
	readonly, ok := team["readOnly"].(bool)
	if !ok {
		return fail("InvalidTeamResponse")
	}
	if readonly {
		return fail("TeamReadOnly")
	}
	if update {
		if name, changed := request["body.teamName"]; changed && name != team["teamName"] {
			allowed, ok := team["allowUpdateTeamName"].(bool)
			if !ok {
				return fail("InvalidTeamResponse")
			}
			if !allowed {
				return fail("TeamRenameNotAllowed")
			}
		}
	}
	return request, nil
}
