package aliyun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func TestResolveOpenAPIProfileSupportsDocumentedAliyunCredentialModes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	aliyunPath := filepath.Join(dir, "aliyun.json")
	ecctlPath := filepath.Join(dir, "ecctl.json")
	future := time.Now().Add(time.Hour).Unix()
	profiles := []any{
		map[string]any{"name": "ak", "mode": "AK", "access_key_id": "ak-id", "access_key_secret": "ak-secret", "region_id": "cn-hangzhou"},
		map[string]any{"name": "sts", "mode": "StsToken", "access_key_id": "sts-id", "access_key_secret": "sts-secret", "sts_token": "sts-token", "region_id": "cn-hangzhou"},
		map[string]any{"name": "role", "mode": "RamRoleArn", "access_key_id": "role-id", "access_key_secret": "role-secret", "ram_role_arn": "acs:ram::1234567890123456:role/admin", "ram_session_name": "ecctl", "region_id": "cn-hangzhou"},
		map[string]any{"name": "ecs", "mode": "EcsRamRole", "ram_role_name": "ecs-role", "region_id": "cn-hangzhou"},
		map[string]any{"name": "chain", "mode": "ChainableRamRoleArn", "source_profile": "ak", "ram_role_arn": "acs:ram::2109876543210987:role/admin", "ram_session_name": "ecctl", "region_id": "cn-hangzhou"},
		map[string]any{"name": "oidc", "mode": "OIDC", "oidc_provider_arn": "acs:ram::1234567890123456:oidc-provider/provider", "oidc_token_file": filepath.Join(dir, "token"), "ram_role_arn": "acs:ram::1234567890123456:role/oidc", "ram_session_name": "ecctl", "region_id": "cn-hangzhou"},
		map[string]any{"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com", "cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "sso-token", "cloud_sso_access_token_expire": future, "region_id": "cn-hangzhou"},
		map[string]any{"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "refresh", "region_id": "cn-hangzhou"},
		map[string]any{"name": "external", "mode": "External", "process_command": "/usr/bin/printf '{}'"},
		map[string]any{"name": "uri", "mode": "CredentialsURI", "credentials_uri": "https://credentials.example.com/token?secret=value", "region_id": "cn-hangzhou"},
		map[string]any{"name": "bearer", "mode": "BearerToken", "bearer_token": "bearer-value", "region_id": "cn-hangzhou"},
	}
	writeJSONFile(t, aliyunPath, map[string]any{"current": "ak", "profiles": profiles})
	getenv := mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "fallback-id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "fallback-secret",
	})

	for _, expectedMode := range []string{
		"AK", "StsToken", "RamRoleArn", "EcsRamRole", "ChainableRamRoleArn", "OIDC",
		"CloudSSO", "OAuth", "External", "CredentialsURI", "BearerToken",
	} {
		profileName := map[string]string{
			"AK": "ak", "StsToken": "sts", "RamRoleArn": "role", "EcsRamRole": "ecs",
			"ChainableRamRoleArn": "chain", "OIDC": "oidc", "CloudSSO": "sso", "OAuth": "oauth",
			"External": "external", "CredentialsURI": "uri", "BearerToken": "bearer",
		}[expectedMode]
		t.Run(expectedMode, func(t *testing.T) {
			resolved, err := resolveOpenAPIProfile(profileName, ecctlPath, ecconfig.ResolvedRegion{}, getenv)
			if err != nil {
				t.Fatalf("resolveOpenAPIProfile(%s): %v", profileName, err)
			}
			if resolved.Mode != expectedMode || resolved.Acquirer == nil {
				t.Fatalf("resolved = %#v, want mode %s with credential", resolved, expectedMode)
			}
			if expectedMode != "BearerToken" && resolved.CredentialPrincipal == "" {
				t.Fatalf("credential principal is empty for %s", expectedMode)
			}
			if expectedMode == "CloudSSO" && resolved.ExpectedAccountID != "123" {
				t.Fatalf("CloudSSO expected account = %q", resolved.ExpectedAccountID)
			}
			roleAccounts := map[string]string{
				"RamRoleArn": "1234567890123456", "ChainableRamRoleArn": "2109876543210987", "OIDC": "1234567890123456",
			}
			if want := roleAccounts[expectedMode]; want != "" && (resolved.ExpectedAccountID != want || resolved.ExpectedIdentityType != "AssumedRoleUser") {
				t.Fatalf("%s expected identity = %q/%q", expectedMode, resolved.ExpectedAccountID, resolved.ExpectedIdentityType)
			}
		})
	}
}

func TestResolveOpenAPIProfileUsesDynamicCredentialsWithoutFetchingThem(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "ecs",
		"profiles": []any{map[string]any{
			"name": "ecs", "mode": "EcsRamRole", "ram_role_name": "role-name", "region_id": "cn-hangzhou",
		}},
	})
	resolved, err := resolveOpenAPIProfile("ecs", filepath.Join(dir, "ecctl.json"), ecconfig.ResolvedRegion{}, mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath}))
	if err != nil {
		t.Fatalf("resolveOpenAPIProfile: %v", err)
	}
	if resolved.Mode != "EcsRamRole" || resolved.Acquirer == nil {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveOpenAPIProfileUsesEcctlPrivateCredentialCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	ecctlPath := filepath.Join(dir, ".ecctl", "config.json")
	aliyunPath := filepath.Join(dir, ".aliyun", "config.json")
	if err := os.MkdirAll(filepath.Dir(aliyunPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "oauth",
		"profiles": []any{map[string]any{
			"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "refresh",
			"access_key_id": "id", "access_key_secret": "secret", "sts_token": "sts",
			"sts_expiration": time.Now().Add(time.Hour).Unix(), "region_id": "cn-hangzhou",
		}},
	})
	resolved, err := resolveOpenAPIProfile("oauth", ecctlPath, explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath,
	}))
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := resolved.Acquirer.(*oauthProfileCredentialsProvider)
	if !ok {
		t.Fatalf("credential source = %T", resolved.Acquirer)
	}
	want := credentialCacheEntryPath(filepath.Join(dir, ".ecctl", "credentials-v2"), aliyunPath, "oauth")
	if provider.cachePath != want || strings.HasPrefix(provider.cachePath, filepath.Dir(aliyunPath)+string(filepath.Separator)) {
		t.Fatalf("credential cache path = %q, want %q", provider.cachePath, want)
	}
	secondResolved, err := resolveOpenAPIProfile("oauth", filepath.Join(dir, "other", "config.json"), explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath,
	}))
	if err != nil {
		t.Fatal(err)
	}
	secondProvider, ok := secondResolved.Acquirer.(*oauthProfileCredentialsProvider)
	if !ok || secondProvider.cachePath != provider.cachePath || credentialProfileLockPath(secondProvider.cachePath, "oauth") != credentialProfileLockPath(provider.cachePath, "oauth") {
		t.Fatalf("different ecctl paths created different rotation owners: first=%q second=%q", provider.cachePath, secondProvider.cachePath)
	}
	if !resolved.PinCredentialIdentity {
		t.Fatal("renewable profile did not enable operation identity pinning")
	}
}

func TestResolveOpenAPIProfileHonorsProfileAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "profile-ak",
		"profiles": []any{map[string]any{
			"name": "profile-ak", "mode": "AK", "access_key_id": "profile-id", "access_key_secret": "profile-secret", "region_id": "cn-hangzhou",
		}},
	})
	baseEnv := map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "env-id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "env-secret",
		"ALIBABA_CLOUD_REGION_ID":         "cn-beijing",
	}

	profileResolved, err := resolveOpenAPIProfile("profile-ak", filepath.Join(dir, "ecctl.json"), ecconfig.ResolvedRegion{}, mapGetenv(baseEnv))
	if err != nil {
		t.Fatalf("resolve profile: %v", err)
	}
	model, err := profileResolved.Acquirer.Acquire(context.Background())
	if err != nil {
		t.Fatalf("profile credential: %v", err)
	}
	if model.AccessKeyID != "profile-id" || profileResolved.RegionID != "cn-hangzhou" {
		t.Fatalf("profile resolution = %#v model=%#v", profileResolved, model)
	}

	baseEnv["ALIBABA_CLOUD_IGNORE_PROFILE"] = "TRUE"
	envResolved, err := resolveOpenAPIProfile("profile-ak", filepath.Join(dir, "ecctl.json"), ecconfig.ResolvedRegion{}, mapGetenv(baseEnv))
	if err != nil {
		t.Fatalf("resolve ignored profile: %v", err)
	}
	model, err = envResolved.Acquirer.Acquire(context.Background())
	if err != nil {
		t.Fatalf("environment credential: %v", err)
	}
	if model.AccessKeyID != "env-id" || envResolved.RegionID != "cn-beijing" {
		t.Fatalf("environment resolution = %#v model=%#v", envResolved, model)
	}
}

func TestIgnoreProfileBypassesMalformedConfigFiles(t *testing.T) {
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	for _, path := range []string{ecctlPath, aliyunPath} {
		if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
			t.Fatalf("write malformed config: %v", err)
		}
	}
	resolved, err := resolveOpenAPIProfile("ignored", ecctlPath, explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
		"ALIBABA_CLOUD_IGNORE_PROFILE":    "TRUE",
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "env-id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "env-secret",
	}))
	if err != nil {
		t.Fatalf("resolveOpenAPIProfile: %v", err)
	}
	model, err := resolved.Acquirer.Acquire(context.Background())
	if err != nil || model.AccessKeyID != "env-id" {
		t.Fatalf("credential = %#v err=%v", model, err)
	}
}

func TestDanglingCurrentProfileDoesNotFallBackToEnvironment(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current":  "missing",
		"profiles": []any{map[string]any{"name": "other", "mode": "AK", "access_key_id": "id", "access_key_secret": "secret"}},
	})
	_, err := resolveOpenAPIProfile("", filepath.Join(dir, "ecctl.json"), explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "fallback-id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "fallback-secret",
	}))
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "ProfileNotFound" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestExistingEmptyConfigStillAllowsEnvironmentCredentials(t *testing.T) {
	for _, contents := range []string{"{}", "null"} {
		t.Run(contents, func(t *testing.T) {
			dir := t.TempDir()
			ecctlPath := filepath.Join(dir, "ecctl.json")
			aliyunPath := filepath.Join(dir, "aliyun.json")
			if err := os.WriteFile(ecctlPath, []byte(contents), 0o600); err != nil {
				t.Fatalf("write ecctl config: %v", err)
			}
			if err := os.WriteFile(aliyunPath, []byte(contents), 0o600); err != nil {
				t.Fatalf("write aliyun config: %v", err)
			}
			resolved, err := resolveOpenAPIProfile("", ecctlPath, explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
				"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
				"ALIBABA_CLOUD_ACCESS_KEY_ID":     "env-id",
				"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "env-secret",
			}))
			if err != nil {
				t.Fatalf("resolveOpenAPIProfile: %v", err)
			}
			model, err := resolved.Acquirer.Acquire(context.Background())
			if err != nil || model.AccessKeyID != "env-id" {
				t.Fatalf("credential = %#v err=%v", model, err)
			}
		})
	}
}

func TestResolveOpenAPIProfileSupportsEnvironmentCredentialModes(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name     string
		env      map[string]string
		wantMode string
	}{
		{
			name: "ram role",
			env: map[string]string{
				"ALIBABA_CLOUD_ACCESS_KEY_ID": "id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "secret",
				"ALIBABA_CLOUD_ROLE_ARN": "acs:ram::1234567890123456:role/admin",
			},
			wantMode: "RamRoleArn",
		},
		{name: "ecs role", env: map[string]string{"ALIBABA_CLOUD_ECS_METADATA": "ecs-role"}, wantMode: "EcsRamRole"},
		{
			name: "oidc",
			env: map[string]string{
				"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1234567890123456:oidc-provider/provider",
				"ALIBABA_CLOUD_OIDC_TOKEN_FILE":   filepath.Join(dir, "oidc-token"),
				"ALIBABA_CLOUD_ROLE_ARN":          "acs:ram::1234567890123456:role/oidc",
			},
			wantMode: "OIDC",
		},
		{name: "credentials URI", env: map[string]string{"ALIBABA_CLOUD_CREDENTIALS_URI": "https://credentials.example.com/token"}, wantMode: "CredentialsURI"},
		{name: "bearer", env: map[string]string{"ALIBABA_CLOUD_BEARER_TOKEN": "token"}, wantMode: "BearerToken"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.env["ALIBABA_CLOUD_IGNORE_PROFILE"] = "TRUE"
			resolved, err := resolveOpenAPIProfile("", filepath.Join(dir, "ecctl.json"), explicitRegion("cn-hangzhou"), mapGetenv(tc.env))
			if err != nil {
				t.Fatalf("resolveOpenAPIProfile: %v", err)
			}
			if resolved.Mode != tc.wantMode || resolved.Acquirer == nil {
				t.Fatalf("resolved = %#v, want mode %s", resolved, tc.wantMode)
			}
		})
	}
}

func TestResolveOpenAPIProfileRejectsCredentialProfileCycles(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "first",
		"profiles": []any{
			map[string]any{"name": "first", "mode": "ChainableRamRoleArn", "source_profile": "second", "ram_role_arn": "acs:ram::1234567890123456:role/first"},
			map[string]any{"name": "second", "mode": "ChainableRamRoleArn", "source_profile": "first", "ram_role_arn": "acs:ram::1234567890123456:role/second"},
		},
	})
	_, err := resolveOpenAPIProfile("first", filepath.Join(dir, "ecctl.json"), explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath}))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestResolveOpenAPIProfileRejectsMissingAndUnsupportedProfiles(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current":  "legacy",
		"profiles": []any{map[string]any{"name": "legacy", "mode": "RsaKeyPair"}},
	})
	getenv := mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":        aliyunPath,
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "fallback-id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "fallback-secret",
	})

	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "missing", code: "ProfileNotFound"},
		{name: "legacy", code: "UnsupportedCredentialMode"},
	} {
		_, err := resolveOpenAPIProfile(tc.name, filepath.Join(dir, "ecctl.json"), ecconfig.ResolvedRegion{}, getenv)
		var appErr *ecerrors.AppError
		if !errors.As(err, &appErr) || appErr.Payload().Code != tc.code {
			t.Fatalf("resolve %s error = %T %v, want %s", tc.name, err, err, tc.code)
		}
		if tc.code == "UnsupportedCredentialMode" && len(appErr.Payload().AcceptedValues) != len(supportedCredentialModeValues) {
			t.Fatalf("accepted modes = %#v", appErr.Payload().AcceptedValues)
		}
	}
}

func TestResolveOpenAPIProfileRejectsDisabledExternalSources(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "external",
		"profiles": []any{
			map[string]any{"name": "external", "mode": "External", "process_command": "/bin/echo '{}'"},
			map[string]any{"name": "uri", "mode": "CredentialsURI", "credentials_uri": "https://example.com"},
		},
	})
	getenv := mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH":               aliyunPath,
		"ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS": "true",
	})
	for _, name := range []string{"external", "uri"} {
		_, err := resolveOpenAPIProfile(name, filepath.Join(dir, "ecctl.json"), ecconfig.ResolvedRegion{}, getenv)
		var appErr *ecerrors.AppError
		if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialSourceDisabled" {
			t.Fatalf("resolve %s error = %T %v", name, err, err)
		}
	}
}

func TestBearerCredentialRejectsHeaderInjection(t *testing.T) {
	for _, header := range []string{"x-token: injected", "x-token\r\nInjected: true", "x-token ü"} {
		if _, err := bearerCredential("token", header, credentialModeBearerToken); err == nil {
			t.Fatalf("bearer header %q was accepted", header)
		}
	}
}

func TestResolveOpenAPIProfileClassifiesExpiredCloudSSO(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "sso",
		"profiles": []any{map[string]any{
			"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
			"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "expired",
			"cloud_sso_access_token_expire": time.Now().Add(-time.Minute).Unix(),
		}},
	})
	resolved, err := resolveOpenAPIProfile("sso", filepath.Join(dir, "ecctl.json"), explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath}))
	if err != nil {
		t.Fatalf("resolveOpenAPIProfile: %v", err)
	}
	_, err = resolved.Acquirer.Acquire(context.Background())
	err = credentialResolutionError(err)
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialReauthenticationRequired" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestCredentialResolutionClassifiesOAuthRefreshRejection(t *testing.T) {
	err := credentialResolutionError(&credentialProviderError{
		mode: credentialModeOAuth,
		err:  errors.New("failed to refresh token, status code: 400"),
	})
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialReauthenticationRequired" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestCredentialResolutionPreservesTypedOAuthRecoveryAndRetry(t *testing.T) {
	recovery := []string{"ecctl", "configure", "--mode", "OAuth", "--profile", "production"}
	err := credentialResolutionError(&credentialProviderError{
		mode: credentialModeOAuth,
		err: &credentialRecoveryError{
			err:     &OAuthRemoteError{Stage: "refresh", StatusCode: http.StatusBadRequest, Code: "invalid_grant"},
			command: recovery,
		},
	})
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialReauthenticationRequired" || !slices.Equal(appErr.Payload().RecoveryCommand, recovery) {
		t.Fatalf("typed reauthentication error = %#v", appErr)
	}
	err = credentialResolutionError(&credentialProviderError{
		mode: credentialModeOAuth,
		err:  &OAuthRemoteError{Stage: "refresh", StatusCode: http.StatusServiceUnavailable},
	})
	if !errors.As(err, &appErr) || appErr.Payload().Code != "OAuthServiceUnavailable" || !appErr.Payload().Retryable {
		t.Fatalf("typed service error = %#v", appErr)
	}
}

func TestCredentialResolutionClassifiesAccountAndPersistenceFailures(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{err: ErrCredentialAccountMismatch, code: "CredentialAccountMismatch"},
		{err: ErrCredentialStatePersistenceFailed, code: "CredentialStatePersistenceFailed"},
	} {
		err := credentialResolutionError(tc.err)
		var appErr *ecerrors.AppError
		if !errors.As(err, &appErr) || appErr.Payload().Code != tc.code {
			t.Fatalf("%v classified as %T %v", tc.err, err, err)
		}
	}
}

func TestCredentialResolutionPrioritizesPostCommitUncertainty(t *testing.T) {
	err := credentialResolutionError(errors.Join(
		ErrCredentialProfileChanged,
		&configfile.PostCommitError{Err: errors.New("directory sync failed")},
	))
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "OAuthPersistenceUncertain" {
		t.Fatalf("post-commit runtime classification = %#v raw=%v", appErr, err)
	}
}

func TestCredentialResolutionPreservesContextTimeoutContract(t *testing.T) {
	for _, source := range []error{context.DeadlineExceeded, context.Canceled} {
		err := credentialResolutionError(&credentialProviderError{mode: credentialModeOIDC, err: source})
		var appErr *ecerrors.AppError
		if !errors.As(err, &appErr) || appErr.Payload().Code != "WaitTimeout" || appErr.ExitCode() != 3 {
			t.Fatalf("context error %v classified as %T %v", source, err, err)
		}
	}
}

func TestStaticSTSProfilePreservesConfiguredExpiration(t *testing.T) {
	expiration := time.Now().Add(time.Hour).Unix()
	resolved, err := resolveAliyunProfileCredential(map[string]any{}, map[string]any{
		"name": "sts", "mode": "StsToken", "access_key_id": "id", "access_key_secret": "secret",
		"sts_token": "token", "sts_expiration": float64(expiration),
	}, "sts", "/unused/config.json", mapGetenv(nil), map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := resolved.Acquirer.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ExpiresAt.Unix() != expiration || credentialAcquirerIsRenewable(resolved.Acquirer) {
		t.Fatalf("static STS snapshot=%#v renewable=%t", snapshot, credentialAcquirerIsRenewable(resolved.Acquirer))
	}
}

func TestSafeExternalCredentialsProviderExecutesArgvWithoutShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper fixture")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "credential helper")
	contents := "#!/bin/sh\nprintf '%s' '{\"mode\":\"StsToken\",\"access_key_id\":\"id\",\"access_key_secret\":\"secret\",\"sts_token\":\"token\"}'\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	provider, err := newSafeExternalCredentialsProvider("\""+script+"\"", mapGetenv(nil))
	if err != nil {
		t.Fatalf("newSafeExternalCredentialsProvider: %v", err)
	}
	credentials, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if credentials.AccessKeyId != "id" || credentials.AccessKeySecret != "secret" || credentials.SecurityToken != "token" {
		t.Fatalf("credentials = %#v", credentials)
	}
}

func TestSafeExternalCredentialsProviderDoesNotEchoInvalidOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper fixture")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "bad-helper")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' 'secret-output-not-json'\n"), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	provider, err := newSafeExternalCredentialsProvider(script, mapGetenv(nil))
	if err != nil {
		t.Fatalf("newSafeExternalCredentialsProvider: %v", err)
	}
	_, err = provider.GetCredentials()
	if err == nil || strings.Contains(err.Error(), "secret-output-not-json") {
		t.Fatalf("error = %v", err)
	}
}

func TestSafeExternalCredentialsProviderRefreshesWhenExpirationIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper fixture")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "refresh-helper")
	counter := filepath.Join(dir, "counter")
	contents := "#!/bin/sh\nn=0\nif [ -f \"$1\" ]; then n=$(tr -d '\\n' < \"$1\"); fi\nn=$((n + 1))\nprintf '%s' \"$n\" > \"$1\"\nprintf '{\"mode\":\"AK\",\"access_key_id\":\"id-%s\",\"access_key_secret\":\"secret\"}' \"$n\"\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	provider, err := newSafeExternalCredentialsProvider("\""+script+"\" \""+counter+"\"", mapGetenv(nil))
	if err != nil {
		t.Fatalf("newSafeExternalCredentialsProvider: %v", err)
	}
	first, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("first GetCredentials: %v", err)
	}
	second, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("second GetCredentials: %v", err)
	}
	if first.AccessKeyId != "id-1" || second.AccessKeyId != "id-2" {
		t.Fatalf("credentials were cached without expiration: first=%q second=%q", first.AccessKeyId, second.AccessKeyId)
	}
}

func TestSplitCredentialProcessCommandSupportsQuotesAndRejectsShellSyntaxAsLiteral(t *testing.T) {
	got, err := splitCredentialProcessCommand(`tool "arg with space" '$HOME'`, "linux")
	if err != nil {
		t.Fatalf("splitCredentialProcessCommand: %v", err)
	}
	if want := []string{"tool", "arg with space", "$HOME"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	if _, err := splitCredentialProcessCommand(`tool "unterminated`, "linux"); err == nil {
		t.Fatal("unclosed quote was accepted")
	}
	windowsArgs, err := splitCredentialProcessCommand(`"C:\Program Files\credential.exe" "arg with space" C:\tokens\oidc`, "windows")
	if err != nil {
		t.Fatalf("split Windows command: %v", err)
	}
	if want := []string{`C:\Program Files\credential.exe`, "arg with space", `C:\tokens\oidc`}; !reflect.DeepEqual(windowsArgs, want) {
		t.Fatalf("Windows args = %#v, want %#v", windowsArgs, want)
	}
}

func TestSafeURLCredentialsProviderRedactsURIQuery(t *testing.T) {
	provider, err := newSafeURLCredentialsProvider(
		"https://user:password@example.com/token?custom_secret=value",
		credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") }),
	)
	if err != nil {
		t.Fatalf("newSafeURLCredentialsProvider: %v", err)
	}
	_, err = provider.GetCredentials()
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "custom_secret") || strings.Contains(err.Error(), "value") {
		t.Fatalf("sanitized error = %v", err)
	}
	if !strings.Contains(err.Error(), "https://example.com") || strings.Contains(err.Error(), "/token") {
		t.Fatalf("sanitized error lost safe endpoint: %v", err)
	}
}

func TestSafeURLCredentialsProviderRequiresSuccessContract(t *testing.T) {
	calls := 0
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	provider, err := newSafeURLCredentialsProvider(
		"https://credentials.example.com/token",
		credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"Code":"Success","AccessKeyId":"id-%d","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"%s"}`, calls, expiration))),
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("newSafeURLCredentialsProvider: %v", err)
	}
	credentials, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if credentials.AccessKeyId != "id-1" || credentials.AccessKeySecret != "secret" || credentials.SecurityToken != "token" {
		t.Fatalf("credentials = %#v", credentials)
	}
	credentials, err = provider.GetCredentials()
	if err != nil {
		t.Fatalf("second GetCredentials: %v", err)
	}
	if calls != 1 || credentials.AccessKeyId != "id-1" {
		t.Fatalf("credentials URI response was not cached: calls=%d credentials=%#v", calls, credentials)
	}
}

func TestCredentialsURIRejectsMissingAndFailureCodeWithoutLeakingResponse(t *testing.T) {
	for _, payload := range []string{
		`{"AccessKeyId":"id","AccessKeySecret":"response-secret","SecurityToken":"token"}`,
		`{"Code":"Failure","AccessKeyId":"id","AccessKeySecret":"response-secret","SecurityToken":"token"}`,
		`{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"response-secret","SecurityToken":"token"}`,
	} {
		provider, err := newSafeURLCredentialsProvider("https://credentials.example.com/private/tenant?secret=query-secret", credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return jsonHTTPResponse(http.StatusOK, payload), nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = provider.Acquire(context.Background())
		if err == nil {
			t.Fatalf("CredentialsURI payload without Code Success was accepted: %s", payload)
		}
		for _, secret := range []string{"response-secret", "query-secret", "/private/tenant"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("CredentialsURI error leaked %q: %v", secret, err)
			}
		}
	}
}

func TestCredentialsURIRejectsRemotePlaintextHTTP(t *testing.T) {
	if _, err := newSafeURLCredentialsProvider("http://credentials.example.com/token", nil); err == nil || !strings.Contains(err.Error(), "requires HTTPS") {
		t.Fatalf("remote plaintext CredentialsURI error = %v", err)
	}
	if _, err := newSafeURLCredentialsProvider("http://127.0.0.1:8080/token", credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unused")
	})); err != nil {
		t.Fatalf("literal loopback CredentialsURI = %v", err)
	}
}

func TestIgnoreProfileRegionPolicyUsesProvenance(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "prod",
		"profiles": []any{map[string]any{
			"name": "prod", "mode": "AK", "region_id": "cn-profile-1",
			"access_key_id": "profile-id", "access_key_secret": "profile-secret",
		}},
	})
	env := map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath, "ALIBABA_CLOUD_IGNORE_PROFILE": "true",
		"ALIBABA_CLOUD_ACCESS_KEY_ID": "env-id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "env-secret",
		"ALIBABA_CLOUD_REGION_ID": "cn-alibaba-1",
	}
	for _, tc := range []struct {
		name   string
		region ecconfig.ResolvedRegion
		want   string
	}{
		{name: "explicit survives", region: ecconfig.ResolvedRegion{Value: "cn-explicit-1", Source: ecconfig.RegionSourceExplicit}, want: "cn-explicit-1"},
		{name: "ecctl survives", region: ecconfig.ResolvedRegion{Value: "cn-ecctl-1", Source: ecconfig.RegionSourceECCTL}, want: "cn-ecctl-1"},
		{name: "profile discarded", region: ecconfig.ResolvedRegion{Value: "cn-profile-1", Source: ecconfig.RegionSourceProfile}, want: "cn-alibaba-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := resolveOpenAPIProfile("prod", filepath.Join(dir, "ecctl.json"), tc.region, mapGetenv(env))
			if err != nil || resolved.RegionID != tc.want {
				t.Fatalf("resolved region = %q, %v; want %q", resolved.RegionID, err, tc.want)
			}
		})
	}
}

func TestEnvironmentCredentialChainChoosesCompleteOIDCThenECS(t *testing.T) {
	complete, err := resolveEnvironmentCredential(mapGetenv(map[string]string{
		"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1234567890123456:oidc-provider/provider",
		"ALIBABA_CLOUD_OIDC_TOKEN_FILE":   "/tmp/token",
		"ALIBABA_CLOUD_ROLE_ARN":          "acs:ram::1234567890123456:role/oidc",
		"ALIBABA_CLOUD_ECS_METADATA":      "ecs-role",
	}))
	if err != nil || complete.Mode != credentialModeOIDC {
		t.Fatalf("complete OIDC = %#v, %v", complete, err)
	}
	partial, err := resolveEnvironmentCredential(mapGetenv(map[string]string{
		"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1234567890123456:oidc-provider/provider",
		"ALIBABA_CLOUD_ECS_METADATA":      "ecs-role",
	}))
	if err != nil || partial.Mode != credentialModeEcsRamRole {
		t.Fatalf("partial OIDC fallback = %#v, %v", partial, err)
	}
}

func TestProfileEnableVPCIsIndependentOfSTSRegion(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "role",
		"profiles": []any{map[string]any{
			"name": "role", "mode": "RamRoleArn", "access_key_id": "id", "access_key_secret": "secret",
			"ram_role_arn": "acs:ram::1234567890123456:role/admin", "enable_vpc": true,
		}},
	})
	resolved, err := resolveOpenAPIProfile("role", filepath.Join(dir, "ecctl.json"), explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath}))
	if err != nil {
		t.Fatal(err)
	}
	acquirer, ok := resolved.Acquirer.(*ramRoleCredentialAcquirer)
	if !ok || !acquirer.enableVPC || acquirer.stsRegion != "" || acquirer.stsEndpoint != "" {
		t.Fatalf("RAM role STS options = %#v", acquirer)
	}
}

func TestRAMRoleMaterializesAndValidatesSTSEnvironment(t *testing.T) {
	values := map[string]string{
		"ALIBABA_CLOUD_ACCESS_KEY_ID":        "id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET":    "secret",
		"ALIBABA_CLOUD_ROLE_ARN":             "acs:ram::1234567890123456:role/admin",
		"ALIBABA_CLOUD_STS_REGION":           "cn-shanghai",
		"ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED": "true",
	}
	resolved, err := resolveEnvironmentCredential(mapGetenv(values))
	if err != nil {
		t.Fatal(err)
	}
	acquirer, ok := resolved.Acquirer.(*ramRoleCredentialAcquirer)
	if !ok || acquirer.stsRegion != "cn-shanghai" || !acquirer.enableVPC {
		t.Fatalf("RAM role STS settings = %#v", acquirer)
	}

	values["ALIBABA_CLOUD_STS_REGION"] = "cn-hangzhou.aliyuncs.com@attacker.example/escape"
	resolved, err = resolveEnvironmentCredential(mapGetenv(values))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolved.Acquirer.Acquire(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid STS region") {
		t.Fatalf("unsafe STS environment error = %v", err)
	}
}

func TestOIDCRejectsUnsafeSTSEnvironmentBeforeReadingToken(t *testing.T) {
	resolved, err := resolveEnvironmentCredential(mapGetenv(map[string]string{
		"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1234567890123456:oidc-provider/provider",
		"ALIBABA_CLOUD_OIDC_TOKEN_FILE":   filepath.Join(t.TempDir(), "missing-token"),
		"ALIBABA_CLOUD_ROLE_ARN":          "acs:ram::1234567890123456:role/oidc",
		"ALIBABA_CLOUD_STS_REGION":        "cn-hangzhou.aliyuncs.com@attacker.example/escape",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolved.Acquirer.Acquire(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid STS region") {
		t.Fatalf("unsafe OIDC STS environment error = %v", err)
	}
}

func TestBearerHeaderUsesRFCFieldNameTokenGrammar(t *testing.T) {
	for _, valid := range []string{"x-token", "X_Custom.Token", "!#$%&'*+-.^_`|~"} {
		if err := validateBearerTokenHeaderKey(valid); err != nil {
			t.Fatalf("valid field name %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "x token", "x(token)", "x/token", "x@token", "x:token", "x\r\ny"} {
		if err := validateBearerTokenHeaderKey(invalid); err == nil {
			t.Fatalf("invalid field name %q accepted", invalid)
		}
	}
}

func TestSplitWindowsCredentialCommandLineRules(t *testing.T) {
	got, err := splitCredentialProcessCommand(`tool O'Brien "arg with space" "" "say \"hi\"" "a&b|c"`, "windows")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tool", "O'Brien", "arg with space", "", `say "hi"`, "a&b|c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestCredentialIdentityCacheKeySeparatesDynamicPrincipals(t *testing.T) {
	profile := resolvedOpenAPIProfile{Mode: credentialModeExternal, CredentialPrincipal: "helper"}
	first := credentialIdentityCacheKey(profile, &credentialSnapshot{AccessKeyID: "principal-a", AccessKeySecret: "secret"})
	second := credentialIdentityCacheKey(profile, &credentialSnapshot{AccessKeyID: "principal-b", AccessKeySecret: "secret"})
	if first == "" || second == "" || first == second {
		t.Fatalf("identity keys were not principal-specific: %q %q", first, second)
	}
}

func TestECSCredentialIdentityKeyIncludesHashedReturnedAccessKey(t *testing.T) {
	profile := resolvedOpenAPIProfile{Mode: credentialModeEcsRamRole, CredentialPrincipal: "ecs-role"}
	first := credentialIdentityCacheKey(profile, &credentialSnapshot{AccessKeyID: "ecs-returned-ak-one", AccessKeySecret: "secret"})
	second := credentialIdentityCacheKey(profile, &credentialSnapshot{AccessKeyID: "ecs-returned-ak-two", AccessKeySecret: "secret"})
	if first == "" || second == "" || first == second {
		t.Fatalf("ECS identity keys = %q %q", first, second)
	}
	if strings.Contains(first, "ecs-returned-ak-one") || strings.Contains(second, "ecs-returned-ak-two") {
		t.Fatal("raw ECS AccessKey ID appeared in identity cache key")
	}
}

func TestCanceledContextCredentialAcquisitionDoesNotLeakGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	acquirer := &ecsCredentialAcquirer{roleName: "role"}
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := acquirer.Acquire(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Acquire cancellation %d = %v", i, err)
		}
	}
	runtime.Gosched()
	if current := runtime.NumGoroutine(); current > baseline+2 {
		t.Fatalf("goroutines grew from %d to %d", baseline, current)
	}
}

func TestSDKCredentialSourcesResetProviderPerOperation(t *testing.T) {
	provider := &rotatingCredentialsProvider{}
	ecsClient := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") })
	ecs := &ecsCredentialAcquirer{roleName: "role", client: ecsClient, cached: &credentialSnapshot{AccessKeyID: "old"}}
	ecsOperation, err := ecs.ForOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cloned := ecsOperation.(*ecsCredentialAcquirer); cloned == ecs || cloned.cached != nil || cloned.roleName != ecs.roleName || cloned.client == nil {
		t.Fatalf("ECS operation source = %#v", cloned)
	}
	oidc := &oidcCredentialAcquirer{mode: credentialModeOIDC, providerARN: "provider", tokenFile: "token", roleArn: "role", provider: provider}
	oidcOperation, err := oidc.ForOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cloned := oidcOperation.(*oidcCredentialAcquirer); cloned == oidc || cloned.provider != nil || cloned.roleArn != oidc.roleArn {
		t.Fatalf("OIDC operation source = %#v", cloned)
	}
	sourceChild := &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret"}}
	sourceFactory := &operationFactoryCredentialAcquirer{child: sourceChild}
	ramRole := &ramRoleCredentialAcquirer{source: sourceFactory, mode: credentialModeChainableRamRoleArn, roleArn: "role", provider: provider}
	ramOperation, err := ramRole.ForOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clonedRAM := ramOperation.(*ramRoleCredentialAcquirer)
	if clonedRAM == ramRole || clonedRAM.provider != nil || clonedRAM.source != sourceChild || sourceFactory.operations != 1 {
		t.Fatalf("RAM role operation source = %#v factory operations=%d", clonedRAM, sourceFactory.operations)
	}
}

func TestEffectiveSTSEndpointUsesOperationRegionForVPC(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		explicitEndpoint, explicitRegion          string
		enableVPC                                 bool
		operationRegion, wantEndpoint, wantRegion string
	}{
		{name: "default", operationRegion: "cn-hangzhou", wantEndpoint: "sts.aliyuncs.com", wantRegion: ""},
		{name: "vpc operation region", enableVPC: true, operationRegion: "cn-hangzhou", wantEndpoint: "sts-vpc.cn-hangzhou.aliyuncs.com", wantRegion: "cn-hangzhou"},
		{name: "explicit region", explicitRegion: "cn-shanghai", enableVPC: true, operationRegion: "cn-hangzhou", wantEndpoint: "sts-vpc.cn-shanghai.aliyuncs.com", wantRegion: "cn-shanghai"},
		{name: "explicit endpoint", explicitEndpoint: "custom.sts.example.com", explicitRegion: "cn-beijing", enableVPC: true, operationRegion: "cn-hangzhou", wantEndpoint: "custom.sts.example.com", wantRegion: "cn-beijing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, region, err := effectiveSTSEndpoint(tc.explicitEndpoint, tc.explicitRegion, tc.enableVPC, tc.operationRegion)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint != tc.wantEndpoint || region != tc.wantRegion {
				t.Fatalf("endpoint, region = %q, %q; want %q, %q", endpoint, region, tc.wantEndpoint, tc.wantRegion)
			}
		})
	}
}

func TestEffectiveSTSEndpointRejectsUnsafeAuthorityInputs(t *testing.T) {
	for _, test := range []struct {
		name             string
		explicitEndpoint string
		explicitRegion   string
		operationRegion  string
		enableVPC        bool
	}{
		{name: "operation userinfo", operationRegion: "x@attacker.example-a", enableVPC: true},
		{name: "operation fragment", operationRegion: "x#attacker.example-a", enableVPC: true},
		{name: "profile path", explicitRegion: "cn/path-a", enableVPC: true},
		{name: "endpoint userinfo", explicitEndpoint: "https://user@attacker.example"},
		{name: "endpoint path", explicitEndpoint: "https://sts.example.com/path"},
		{name: "endpoint query", explicitEndpoint: "https://sts.example.com?token=value"},
		{name: "endpoint insecure scheme", explicitEndpoint: "http://sts.example.com"},
		{name: "vpc missing region", enableVPC: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if endpoint, region, err := effectiveSTSEndpoint(test.explicitEndpoint, test.explicitRegion, test.enableVPC, test.operationRegion); err == nil {
				t.Fatalf("unsafe STS input accepted: endpoint=%q region=%q", endpoint, region)
			}
		})
	}
}

func TestRamRoleAndOIDCUseExpectedSTSTransportHost(t *testing.T) {
	// credentials-go reads ALIBABA_CLOUD_STS_REGION itself when ecctl leaves
	// the final endpoint empty. Keep a hostile ambient value present so every
	// default-path case proves ecctl has fully materialized the target.
	t.Setenv("ALIBABA_CLOUD_STS_REGION", "cn-hangzhou.aliyuncs.com@attacker.example/escape")
	source, err := staticCredentialFromValues("source-id", "source-secret", "", credentialModeAK, "source-id")
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "oidc-token")
	if err := os.WriteFile(tokenFile, []byte("oidc-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		make func(proxy string) credentialAcquirer
		want string
	}{
		{
			name: "RAM role default",
			make: func(proxy string) credentialAcquirer {
				return &ramRoleCredentialAcquirer{source: source.Acquirer, mode: credentialModeRamRoleArn, roleArn: "acs:ram::1234567890123456:role/admin", roleSessionName: "ecctl", proxy: proxy}
			},
			want: "sts.aliyuncs.com:443",
		},
		{
			name: "RAM role VPC operation region",
			make: func(proxy string) credentialAcquirer {
				return &ramRoleCredentialAcquirer{source: source.Acquirer, mode: credentialModeRamRoleArn, roleArn: "acs:ram::1234567890123456:role/admin", roleSessionName: "ecctl", enableVPC: true, proxy: proxy}
			},
			want: "sts-vpc.cn-hangzhou.aliyuncs.com:443",
		},
		{
			name: "RAM role explicit STS region",
			make: func(proxy string) credentialAcquirer {
				return &ramRoleCredentialAcquirer{source: source.Acquirer, mode: credentialModeRamRoleArn, roleArn: "acs:ram::1234567890123456:role/admin", roleSessionName: "ecctl", enableVPC: true, stsRegion: "cn-shanghai", proxy: proxy}
			},
			want: "sts-vpc.cn-shanghai.aliyuncs.com:443",
		},
		{
			name: "OIDC VPC operation region",
			make: func(proxy string) credentialAcquirer {
				return &oidcCredentialAcquirer{mode: credentialModeOIDC, providerARN: "acs:ram::1234567890123456:oidc-provider/provider", tokenFile: tokenFile, roleArn: "acs:ram::1234567890123456:role/oidc", roleSessionName: "ecctl", enableVPC: true, proxy: proxy}
			},
			want: "sts-vpc.cn-hangzhou.aliyuncs.com:443",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hosts := make(chan string, 1)
			proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				hosts <- request.Host
				response.WriteHeader(http.StatusBadGateway)
			}))
			defer proxy.Close()
			ctx, cancel := context.WithTimeout(withCredentialOperationRegion(context.Background(), "cn-hangzhou"), time.Second)
			defer cancel()
			_, _ = tc.make(proxy.URL).Acquire(ctx)
			select {
			case host := <-hosts:
				if host != tc.want {
					t.Fatalf("STS transport host = %q, want %q", host, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("STS request did not reach recording proxy")
			}
		})
	}
}

func TestCredentialsURIAcquisitionHonorsCancellation(t *testing.T) {
	provider, err := newSafeURLCredentialsProvider("https://credentials.example.com/private/path", credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestCredentialsURIRejectsRedirectBeforeDestination(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls++ }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	provider, err := newSafeURLCredentialsProvider(source.URL+"/private/path?secret=value", source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Acquire(context.Background()); err == nil {
		t.Fatal("redirecting credentials URI succeeded")
	}
	if destinationCalls != 0 {
		t.Fatalf("credentials redirect reached destination %d times", destinationCalls)
	}
}

type credentialHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f credentialHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func mapGetenv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func explicitRegion(value string) ecconfig.ResolvedRegion {
	return ecconfig.ResolvedRegion{Value: value, Source: ecconfig.RegionSourceExplicit}
}
