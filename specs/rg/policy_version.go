package rg

import (
	"context"
	stderrors "errors"
	"fmt"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	spechooks "github.com/aliyun/elastic-compute-control-cli/specs"
)

const fallbackDeleteTargetField = "DeleteTargetVersionId"

func init() {
	spechooks.RegisterBeforeOperation("rg", "version", "validate_fallback_default", validateFallbackDefault)
	spechooks.RegisterAfterOperationError("rg", "version", "preserve_delete_target_on_restore_not_found", preserveDeleteTargetOnRestoreNotFound)
}

func validateFallbackDefault(_ context.Context, _ spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	fallback := fmt.Sprint(request["VersionId"])
	target := fmt.Sprint(request[fallbackDeleteTargetField])
	if fallback != "" && fallback == target {
		return nil, ecerrors.Client("InvalidParameter", "fallback default version must differ from the version being deleted",
			ecerrors.WithField("fallback_default_version"),
		)
	}
	resolved := make(map[string]any, len(request))
	for key, value := range request {
		if key != fallbackDeleteTargetField {
			resolved[key] = value
		}
	}
	return resolved, nil
}

func preserveDeleteTargetOnRestoreNotFound(ctx context.Context, caller spechooks.OperationCaller, request map[string]any, err error) error {
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) || appErr.Payload().Kind != string(ecerrors.CategoryNotFound) {
		return err
	}
	restoreAction := ecerrors.ActionFromError("SetDefaultPolicyVersion", err)
	actions := []ecerrors.Action{restoreAction}
	target, _ := request[fallbackDeleteTargetField].(string)
	policyName, _ := request["PolicyName"].(string)
	if target == "" || policyName == "" {
		return ecerrors.WithActions(ecerrors.WithDetails(err,
			ecerrors.WithDetail("could not verify whether the requested policy version still exists"),
		), actions)
	}

	response, targetErr := caller.CallRaw(ctx, "GetPolicyVersion", map[string]any{
		"PolicyName": policyName,
		"PolicyType": "Custom",
		"VersionId":  target,
	})
	if targetErr != nil {
		actions = append(actions, ecerrors.ActionFromError("GetPolicyVersion", targetErr))
		var targetAppErr *ecerrors.AppError
		if stderrors.As(targetErr, &targetAppErr) && targetAppErr.Payload().Kind == string(ecerrors.CategoryNotFound) {
			return ecerrors.WithActions(err, actions)
		}
		return ecerrors.WithActions(ecerrors.WithDetails(targetErr,
			ecerrors.WithDetail("could not verify whether the requested policy version still exists"),
		), actions)
	}
	requestID, _ := response["RequestId"].(string)
	actions = append(actions, ecerrors.Action{
		ActionName: "GetPolicyVersion",
		RequestID:  requestID,
	})
	version, _ := response["PolicyVersion"].(map[string]any)
	if fmt.Sprint(version["VersionId"]) != target {
		return ecerrors.WithActions(ecerrors.Service("CleanupTargetVerificationFailed", "could not verify that the requested policy version no longer exists", false,
			ecerrors.WithRequestID(requestID),
		), actions)
	}

	return ecerrors.WithActions(ecerrors.Service("CleanupPrerequisiteNotFound", "fallback default policy version was not found; the requested version was not deleted", false,
		ecerrors.WithRequestID(restoreAction.RequestID),
		ecerrors.WithRawCause(restoreAction.Code, restoreAction.Message),
	), actions)
}
