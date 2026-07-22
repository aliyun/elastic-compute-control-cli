package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
)

type ErrorKind string

const (
	ErrorUnavailable   ErrorKind = "unavailable"
	ErrorIntegrity     ErrorKind = "integrity"
	ErrorInvalidTarget ErrorKind = "invalid_target"
	ErrorPermission    ErrorKind = "permission"
	ErrorBusy          ErrorKind = "busy"
	ErrorTimeout       ErrorKind = "timeout"
	ErrorCanceled      ErrorKind = "canceled"
	ErrorInstallation  ErrorKind = "installation"
)

type updateError struct {
	kind ErrorKind
	err  error
}

func (e *updateError) Error() string { return e.err.Error() }
func (e *updateError) Unwrap() error { return e.err }

func WrapError(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &updateError{kind: kind, err: err}
}

func ErrorKindOf(err error) ErrorKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ErrorCanceled
	}
	var typed *updateError
	if errors.As(err, &typed) {
		return typed.kind
	}
	if errors.Is(err, os.ErrPermission) {
		return ErrorPermission
	}
	return ErrorInstallation
}

func ErrorRetryable(kind ErrorKind) bool {
	return kind == ErrorUnavailable || kind == ErrorBusy
}

func ensureInstallError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var typed *updateError
	if errors.As(err, &typed) {
		return err
	}
	if errors.Is(err, os.ErrPermission) {
		return WrapError(ErrorPermission, err)
	}
	return WrapError(ErrorInstallation, err)
}

func unavailableOrIntegrityError(source string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var unavailable *sourceUnavailableError
	if errors.As(err, &unavailable) {
		return WrapError(ErrorUnavailable, fmt.Errorf("%s: %w", source, err))
	}
	return WrapError(ErrorIntegrity, fmt.Errorf("%s: %w", source, err))
}
