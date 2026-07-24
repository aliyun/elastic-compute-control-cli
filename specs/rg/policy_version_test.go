package rg

import (
	"context"
	stderrors "errors"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func TestValidateFallbackDefaultRejectsDeleteTarget(t *testing.T) {
	_, err := validateFallbackDefault(context.Background(), nil, map[string]any{
		"VersionId":               "v2",
		fallbackDeleteTargetField: "v2",
	})
	if err == nil {
		t.Fatal("validateFallbackDefault accepted the version being deleted")
	}
}

func TestPreserveDeleteTargetOnRestoreNotFoundChangesExitCategory(t *testing.T) {
	caller := &policyVersionHookCaller{response: map[string]any{
		"RequestId": "req-target",
		"PolicyVersion": map[string]any{
			"VersionId": "v2",
		},
	}}
	err := preserveDeleteTargetOnRestoreNotFound(context.Background(), caller, map[string]any{
		"PolicyName":              "my-policy",
		fallbackDeleteTargetField: "v2",
	},
		ecerrors.NotFound("NotFound", "fallback missing", ecerrors.WithRequestID("req-fallback")),
	)
	appErr, ok := err.(*ecerrors.AppError)
	if !ok || appErr.ExitCode() != 2 || appErr.Payload().Code != "CleanupPrerequisiteNotFound" {
		t.Fatalf("error = %#v, want service CleanupPrerequisiteNotFound", err)
	}
	actions := appErr.Actions()
	if len(actions) != 2 || actions[0].ActionName != "SetDefaultPolicyVersion" ||
		actions[1].ActionName != "GetPolicyVersion" || actions[1].RequestID != "req-target" {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestPreserveDeleteTargetOnRestoreNotFoundKeepsNotFoundWhenTargetIsAbsent(t *testing.T) {
	caller := &policyVersionHookCaller{err: ecerrors.NotFound("NotFound", "target missing", ecerrors.WithRequestID("req-target"))}
	err := preserveDeleteTargetOnRestoreNotFound(context.Background(), caller, map[string]any{
		"PolicyName":              "my-policy",
		fallbackDeleteTargetField: "v2",
	}, ecerrors.NotFound("NotFound", "fallback missing", ecerrors.WithRequestID("req-fallback")))

	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) || appErr.ExitCode() != 4 {
		t.Fatalf("error = %#v, want replay-safe not found", err)
	}
	actions := appErr.Actions()
	if len(actions) != 2 || actions[1].ActionName != "GetPolicyVersion" || actions[1].RequestID != "req-target" {
		t.Fatalf("actions = %#v", actions)
	}
}

func TestPreserveDeleteTargetOnRestoreNotFoundRejectsUnverifiedSuccessResponse(t *testing.T) {
	caller := &policyVersionHookCaller{response: map[string]any{"RequestId": "req-target"}}
	err := preserveDeleteTargetOnRestoreNotFound(context.Background(), caller, map[string]any{
		"PolicyName":              "my-policy",
		fallbackDeleteTargetField: "v2",
	}, ecerrors.NotFound("NotFound", "fallback missing", ecerrors.WithRequestID("req-fallback")))

	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) || appErr.ExitCode() != 2 ||
		appErr.Payload().Code != "CleanupTargetVerificationFailed" {
		t.Fatalf("error = %#v, want target verification service failure", err)
	}
	if actions := appErr.Actions(); len(actions) != 2 || actions[1].RequestID != "req-target" {
		t.Fatalf("actions = %#v", appErr.Actions())
	}
}

type policyVersionHookCaller struct {
	response map[string]any
	err      error
	calls    int
}

func (c *policyVersionHookCaller) CallRaw(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	c.calls++
	if operation != "GetPolicyVersion" || request["PolicyName"] != "my-policy" ||
		request["PolicyType"] != "Custom" || request["VersionId"] != "v2" {
		return nil, ecerrors.Client("UnexpectedCall", "unexpected target verification call")
	}
	return c.response, c.err
}
