package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want int
	}{
		{name: "client", err: Client("MissingRegion", "region is required"), want: 1},
		{name: "service", err: Service("Throttling", "retry budget exhausted", true), want: 2},
		{name: "timeout", err: Timeout("WaitTimeout", "timed out"), want: 3},
		{name: "not found", err: NotFound("NotFound", "instance not found"), want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.ExitCode(); got != tt.want {
				t.Fatalf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrorPayloadIncludesStableAndOptionalFields(t *testing.T) {
	err := Client("InstanceStateConflict", "instance is running",
		WithField("state"),
		WithAcceptedValues("stopped"),
		WithCurrentState("running"),
		WithExpectedStates("stopped"),
		WithSuggestion("stop the instance before deleting it"),
	)

	payload := err.Payload()
	if payload.Kind != "client" {
		t.Fatalf("Kind = %q, want client", payload.Kind)
	}
	if payload.Code != "InstanceStateConflict" {
		t.Fatalf("Code = %q", payload.Code)
	}
	if payload.Message != "instance is running" {
		t.Fatalf("Message = %q", payload.Message)
	}
	if payload.Retryable {
		t.Fatal("client errors must not be retryable by default")
	}
	if payload.CurrentState != "running" {
		t.Fatalf("CurrentState = %q", payload.CurrentState)
	}
	if len(payload.ExpectedStates) != 1 || payload.ExpectedStates[0] != "stopped" {
		t.Fatalf("ExpectedStates = %#v", payload.ExpectedStates)
	}
	if payload.Suggestion == "" {
		t.Fatal("Suggestion must be present")
	}
	if payload.SuggestedAction != payload.Suggestion {
		t.Fatalf("SuggestedAction = %q, want suggestion compatibility %q", payload.SuggestedAction, payload.Suggestion)
	}
	if payload.Field != "state" {
		t.Fatalf("Field = %q, want state", payload.Field)
	}
	if len(payload.AcceptedValues) != 1 || payload.AcceptedValues[0] != "stopped" {
		t.Fatalf("AcceptedValues = %#v", payload.AcceptedValues)
	}
}

func TestErrorPayloadKindReflectsCategory(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{name: "service", err: Service("CloudAPIError", "request failed", false), want: "service"},
		{name: "timeout", err: Timeout("WaitTimeout", "timed out"), want: "timeout"},
		{name: "not found", err: NotFound("NotFound", "missing"), want: "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Payload().Kind; got != tt.want {
				t.Fatalf("Kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppErrorStringIncludesCodeAndMessage(t *testing.T) {
	err := Service("CloudAPIError", "request failed", true)

	if got := err.Error(); got != "CloudAPIError: request failed" {
		t.Fatalf("Error() = %q, want code and message", got)
	}
}

func TestExitCodeDefaultsToClientForUnknownCategory(t *testing.T) {
	err := &AppError{category: Category("unknown")}

	if got := err.ExitCode(); got != 1 {
		t.Fatalf("ExitCode() = %d, want 1", got)
	}
}

func TestActionsReturnsCopy(t *testing.T) {
	err := Service("CloudAPIError", "request failed", false,
		WithActionsOption(Action{ActionName: "create", Code: "Throttling"}),
	)

	actions := err.Actions()
	actions[0].Code = "mutated"

	if got := err.Actions()[0].Code; got != "Throttling" {
		t.Fatalf("Actions() returned mutable backing slice, code = %q", got)
	}
	var nilErr *AppError
	if got := nilErr.Actions(); got != nil {
		t.Fatalf("nil Actions() = %#v, want nil", got)
	}
}

func TestWithActionsClonesAppError(t *testing.T) {
	base := Client("InvalidInput", "bad input", WithSuggestion("fix it"))
	actions := []Action{{ActionName: "delete", Code: "IncorrectStatus"}}

	wrapped := WithActions(base, actions)
	appErr, ok := wrapped.(*AppError)
	if !ok {
		t.Fatalf("WithActions returned %T, want *AppError", wrapped)
	}
	actions[0].Code = "mutated"

	if appErr == base {
		t.Fatal("WithActions must clone the AppError")
	}
	if got := base.Actions(); got != nil {
		t.Fatalf("base Actions() = %#v, want nil", got)
	}
	if got := appErr.Actions()[0].Code; got != "IncorrectStatus" {
		t.Fatalf("wrapped action code = %q", got)
	}
	if appErr.Payload().Suggestion != "fix it" {
		t.Fatalf("payload not preserved: %#v", appErr.Payload())
	}
}

func TestWithActionsWrapsPlainError(t *testing.T) {
	wrapped := WithActions(stderrors.New("request failed"), []Action{{ActionName: "create"}})
	var appErr *AppError
	if !stderrors.As(wrapped, &appErr) {
		t.Fatalf("WithActions returned %T, want AppError", wrapped)
	}
	if appErr.Payload().Code != "CloudAPIError" || appErr.Payload().Message != "request failed" {
		t.Fatalf("payload = %#v", appErr.Payload())
	}
	if got := appErr.Actions(); len(got) != 1 || got[0].ActionName != "create" {
		t.Fatalf("actions = %#v", got)
	}
}

func TestWithDetailsClonesWrappedAppError(t *testing.T) {
	base := Service("CloudAPIError", "request failed", false,
		WithActionsOption(Action{ActionName: "RunInstances", Code: "InvalidResourceType.NotSupported"}),
	)

	withDetails := WithDetails(fmt.Errorf("wrapped: %w", base),
		WithField("type"),
		WithSuggestedAction("ecctl call ecs DescribeAvailableResource"),
	)
	var appErr *AppError
	if !stderrors.As(withDetails, &appErr) {
		t.Fatalf("WithDetails returned %T, want AppError", withDetails)
	}
	if appErr == base {
		t.Fatal("WithDetails must clone the AppError")
	}
	if got := base.Payload().Field; got != "" {
		t.Fatalf("base field = %q, want unchanged empty field", got)
	}
	payload := appErr.Payload()
	if payload.Field != "type" || payload.SuggestedAction != "ecctl call ecs DescribeAvailableResource" {
		t.Fatalf("payload = %#v", payload)
	}
	if got := appErr.Actions(); len(got) != 1 || got[0].Code != "InvalidResourceType.NotSupported" {
		t.Fatalf("actions = %#v", got)
	}
}

func TestActionFromErrorUsesAppErrorMetadata(t *testing.T) {
	err := Service("CloudAPIError", "fallback message", false,
		WithRequestID("req-1"),
		WithRawCause("IncorrectStatus", "instance is running"),
	)

	action := ActionFromError("delete", err)

	if action.ActionName != "delete" || action.RequestID != "req-1" {
		t.Fatalf("action metadata = %#v", action)
	}
	if action.Code != "IncorrectStatus" || action.Message != "instance is running" {
		t.Fatalf("action cause = %#v", action)
	}
}

func TestActionFromErrorParsesCloudErrorText(t *testing.T) {
	err := stderrors.New("ErrorCode: Throttling\nMessage: slow down\nRequest ID: req-2")

	action := ActionFromError("create", err)

	if action.Code != "Throttling" || action.Message != "slow down" || action.RequestID != "req-2" {
		t.Fatalf("parsed action = %#v", action)
	}
}

func TestActionFromErrorHandlesNil(t *testing.T) {
	action := ActionFromError("get", nil)

	if action != (Action{ActionName: "get"}) {
		t.Fatalf("nil action = %#v", action)
	}
}

func TestParseCloudErrorFallsBackToRawMessage(t *testing.T) {
	code, message, requestID := ParseCloudError("plain failure")

	if code != "" || requestID != "" || message != "plain failure" {
		t.Fatalf("ParseCloudError = (%q, %q, %q)", code, message, requestID)
	}
}
