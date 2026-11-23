package errors

import (
	stderrors "errors"
	"strings"
)

func WithActions(source error, actions []Action) error {
	var appErr *AppError
	if stderrors.As(source, &appErr) {
		cloned := *appErr
		cloned.actions = cloneActions(actions)
		return &cloned
	}
	return Service("CloudAPIError", source.Error(), false, WithActionsOption(actions...))
}

func WithActionsOption(actions ...Action) Option {
	return func(err *AppError) {
		err.actions = cloneActions(actions)
	}
}

func (e *AppError) Actions() []Action {
	if e == nil {
		return nil
	}
	return cloneActions(e.actions)
}

func cloneActions(actions []Action) []Action {
	cloned := append([]Action(nil), actions...)
	for index := range cloned {
		if cloned[index].Target == nil {
			continue
		}
		target := *cloned[index].Target
		cloned[index].Target = &target
	}
	return cloned
}

func ActionFromError(actionName string, err error) Action {
	action := Action{ActionName: actionName}
	if err == nil {
		return action
	}
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		action.RequestID = appErr.requestID
		action.Code = firstNonEmpty(appErr.rawCode, appErr.payload.Code)
		action.Message = firstNonEmpty(appErr.rawMessage, appErr.payload.Message)
		action.ProviderVerified = appErr.verifiedProviderResponse
		return action
	}
	code, message, requestID := ParseCloudError(err.Error())
	action.Code = code
	action.Message = message
	action.RequestID = requestID
	return action
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ParseCloudError(raw string) (code string, message string, requestID string) {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "errorcode", "error code", "code":
			code = value
		case "error message", "message":
			message = value
		case "requestid", "request id":
			requestID = value
		}
	}
	if message == "" {
		message = strings.TrimSpace(raw)
	}
	return code, message, requestID
}
