package aliyun

import (
	"context"
	"errors"
	"strings"
	"testing"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

func TestOAuthErrorsPreserveCausesAndSafeFallbackMessages(t *testing.T) {
	cause := errors.New("fixture unavailable")
	for _, err := range []error{(*OAuthLocalError)(nil), &OAuthLocalError{}, (*OAuthRemoteError)(nil), &OAuthRemoteError{Stage: "token"}, (*OAuthConfigureError)(nil), &OAuthConfigureError{}} {
		if err.Error() == "" || errors.Unwrap(err) != nil {
			t.Fatalf("missing safe error fallback: %T", err)
		}
	}
	for _, err := range []error{&OAuthLocalError{Stage: "callback", Err: cause}, &OAuthRemoteError{Stage: "token", Err: cause}, &OAuthConfigureError{Err: cause}} {
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), cause.Error()) {
			t.Fatalf("error cause lost: %v", err)
		}
	}
	if got := (&OAuthAccountMismatchError{Expected: "private-one", Actual: "private-two"}).Error(); strings.Contains(got, "private-") || got == "" {
		t.Fatal("account identifiers leaked in error")
	}
	if _, err := nativeOAuthTransactionConfigPath(nil); err == nil {
		t.Fatal("nil config store accepted")
	}
}

func TestOAuthErrorClassificationDistinguishesCancellationAndRetryability(t *testing.T) {
	for _, tc := range []struct {
		cause error
		kind  OAuthConfigureErrorKind
		retry bool
	}{
		{context.Canceled, OAuthConfigureCanceled, false},
		{context.DeadlineExceeded, OAuthConfigureTimeout, false},
		{ErrOAuthAuthorizationTimeout, OAuthConfigureTimeout, false},
		{ErrOAuthAccountConfirmationRequired, OAuthConfigureConfirmation, false},
		{&OAuthAuthorizationDeniedError{Code: "invalid_scope"}, OAuthConfigureInvalid, false},
		{&OAuthRemoteError{StatusCode: 503}, OAuthConfigureService, true},
		{&OAuthRemoteError{StatusCode: 403}, OAuthConfigureDenied, false},
	} {
		err := classifyOAuthLoginError(tc.cause)
		var classified *OAuthConfigureError
		if !errors.As(err, &classified) || classified.Kind != tc.kind || classified.Retryable != tc.retry || !errors.Is(err, tc.cause) {
			t.Fatalf("login cause %v: %+v", tc.cause, err)
		}
	}
	for _, tc := range []struct {
		cause error
		kind  OAuthConfigureErrorKind
	}{
		{context.DeadlineExceeded, OAuthConfigureTimeout},
		{ecconfig.ErrCredentialProfileChanged, OAuthConfigureProfileChanged},
		{ErrCredentialProfileChanged, OAuthConfigureProfileChanged},
	} {
		var classified *OAuthConfigureError
		err := classifyOAuthPersistenceError(tc.cause)
		if !errors.As(err, &classified) || classified.Kind != tc.kind || classified.Retryable || !errors.Is(err, tc.cause) {
			t.Fatalf("persistence classification: %v", err)
		}
	}
}
