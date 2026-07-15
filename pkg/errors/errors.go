package errors

import stderrors "errors"

type Category string

const (
	CategoryClient   Category = "client"
	CategoryService  Category = "service"
	CategoryTimeout  Category = "timeout"
	CategoryNotFound Category = "not_found"
)

type AppError struct {
	category   Category
	payload    ErrorPayload
	actions    []Action
	requestID  string
	rawCode    string
	rawMessage string
}

type ErrorPayload struct {
	Kind            string   `json:"kind"`
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	Retryable       bool     `json:"retryable"`
	Suggestion      string   `json:"suggestion,omitempty"`
	SuggestedAction string   `json:"suggested_action,omitempty"`
	Field           string   `json:"field,omitempty"`
	AcceptedValues  []string `json:"accepted_values,omitempty"`
	CurrentState    string   `json:"current_state,omitempty"`
	ExpectedStates  []string `json:"expected_states,omitempty"`
}

type Action struct {
	RequestID  string `json:"request_id,omitempty"`
	ActionName string `json:"action_name"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type Option func(*AppError)

func Client(code, message string, options ...Option) *AppError {
	return newAppError(CategoryClient, code, message, false, options...)
}

func Service(code, message string, retryable bool, options ...Option) *AppError {
	return newAppError(CategoryService, code, message, retryable, options...)
}

func Timeout(code, message string, options ...Option) *AppError {
	return newAppError(CategoryTimeout, code, message, false, options...)
}

func NotFound(code, message string, options ...Option) *AppError {
	return newAppError(CategoryNotFound, code, message, false, options...)
}

func newAppError(category Category, code, message string, retryable bool, options ...Option) *AppError {
	payload := ErrorPayload{
		Kind:      string(category),
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
	err := &AppError{category: category, payload: payload}
	for _, option := range options {
		option(err)
	}
	return err
}

func WithSuggestion(suggestion string) Option {
	return func(err *AppError) {
		err.payload.Suggestion = suggestion
		err.payload.SuggestedAction = suggestion
	}
}

func WithSuggestedAction(action string) Option {
	return func(err *AppError) {
		err.payload.SuggestedAction = action
	}
}

func WithField(field string) Option {
	return func(err *AppError) {
		err.payload.Field = field
	}
}

func WithAcceptedValues(values ...string) Option {
	return func(err *AppError) {
		err.payload.AcceptedValues = append([]string(nil), values...)
	}
}

func WithCurrentState(state string) Option {
	return func(err *AppError) {
		err.payload.CurrentState = state
	}
}

func WithExpectedStates(states ...string) Option {
	return func(err *AppError) {
		err.payload.ExpectedStates = append([]string(nil), states...)
	}
}

func WithRequestID(requestID string) Option {
	return func(err *AppError) {
		err.requestID = requestID
	}
}

func WithRawCause(code, message string) Option {
	return func(err *AppError) {
		err.rawCode = code
		err.rawMessage = message
	}
}

func WithDetails(source error, options ...Option) error {
	var appErr *AppError
	if !stderrors.As(source, &appErr) {
		return source
	}
	cloned := *appErr
	for _, option := range options {
		option(&cloned)
	}
	return &cloned
}

func (e *AppError) Error() string {
	return e.payload.Code + ": " + e.payload.Message
}

func (e *AppError) Payload() ErrorPayload {
	return e.payload
}

func (e *AppError) ExitCode() int {
	switch e.category {
	case CategoryClient:
		return 1
	case CategoryService:
		return 2
	case CategoryTimeout:
		return 3
	case CategoryNotFound:
		return 4
	default:
		return 1
	}
}
