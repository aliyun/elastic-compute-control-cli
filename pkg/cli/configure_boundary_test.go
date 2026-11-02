package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
)

func TestConfigureOAuthMapsEveryFailureKind(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	original := configureOAuthLogin
	t.Cleanup(func() { configureOAuthLogin = original })
	for _, tt := range []struct {
		kind aliyun.OAuthConfigureErrorKind
		code string
	}{
		{aliyun.OAuthConfigureCanceled, "OAuthLoginCanceled"}, {aliyun.OAuthConfigureLocal, "OAuthCallbackUnavailable"},
		{aliyun.OAuthConfigureManual, "OAuthManualAuthorizationRequired"}, {aliyun.OAuthConfigureAccountMismatch, "OAuthAccountMismatch"},
		{aliyun.OAuthConfigureProfileChanged, "CredentialIdentityChanged"}, {aliyun.OAuthConfigurePersistence, "ConfigWriteFailed"},
		{aliyun.OAuthConfigurePersistenceUncertain, "OAuthPersistenceUncertain"}, {aliyun.OAuthConfigureInvalid, "InvalidParameter"},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			configureOAuthLogin = func(context.Context, aliyun.OAuthConfigureOptions) (*aliyun.OAuthConfigureResult, error) {
				return nil, &aliyun.OAuthConfigureError{Kind: tt.kind, Err: errors.New("fixture failure")}
			}
			out, _, exit := runCLI("--lang", "en", "configure", "--mode", "OAuth", "--profile", "test", "--expected-account-id", "1234567890123456")
			if exit == 0 || errorCode(t, out) != tt.code {
				t.Fatalf("failure %s = exit %d, %s", tt.kind, exit, out)
			}
			if !containsStringValue(errorObject(t, out)["recovery_command"], "test") {
				t.Fatalf("missing recovery profile: %s", out)
			}
		})
	}
	configureOAuthLogin = func(context.Context, aliyun.OAuthConfigureOptions) (*aliyun.OAuthConfigureResult, error) {
		return nil, errors.New("untyped failure")
	}
	if out, _, exit := runCLI("--lang", "en", "configure", "--mode", "OAuth"); exit == 0 || errorCode(t, out) != "OAuthLoginFailed" {
		t.Fatalf("untyped error = %d, %s", exit, out)
	}
}

func TestConfigureOAuthValidatesSelectorsAndAuthorizationOutput(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	original := configureOAuthLogin
	t.Cleanup(func() { configureOAuthLogin = original })
	configureOAuthLogin = func(context.Context, aliyun.OAuthConfigureOptions) (*aliyun.OAuthConfigureResult, error) {
		t.Fatal("invalid selector reached login")
		return nil, nil
	}
	for _, extra := range [][]string{{"--oauth-site-type", "invalid"}, {"--expected-account-id", "invalid"}} {
		args := append([]string{"--lang", "en", "configure", "--mode", "OAuth"}, extra...)
		if out, _, exit := runCLI(args...); exit == 0 || errorCode(t, out) != "InvalidParameter" {
			t.Fatalf("invalid selector = %d, %s", exit, out)
		}
	}
	configureOAuthLogin = func(_ context.Context, options aliyun.OAuthConfigureOptions) (*aliyun.OAuthConfigureResult, error) {
		if err := options.ConfirmAccount("1234567890123456", "RAMUser"); err != nil {
			t.Fatal(err)
		}
		if err := options.OnAuthorizationURL("https://example.invalid/one-time"); !errors.Is(err, aliyun.ErrOAuthManualAuthorizationRequired) {
			t.Fatalf("noninteractive URL output = %v", err)
		}
		return nil, &aliyun.OAuthConfigureError{Kind: aliyun.OAuthConfigureManual, Err: aliyun.ErrOAuthManualAuthorizationRequired}
	}
	if out, _, exit := runCLI("--lang", "en", "configure", "--mode", "OAuth", "--expected-account-id", "1234567890123456"); exit == 0 || errorCode(t, out) != "OAuthManualAuthorizationRequired" {
		t.Fatalf("manual authorization = %d, %s", exit, out)
	}
}
