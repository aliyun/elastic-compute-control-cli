package aliyun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

func TestOAuthConfigurationRejectsInvalidInputsBeforeAuthorization(t *testing.T) {
	for _, fault := range []string{"invalid config", "invalid name", "invalid site", "invalid account", "cache root error", "cache root blocked"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			root := filepath.Join(dir, "private")
			loginCalls := 0
			restore := stubNativeOAuthConfigure(t, root, func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
				loginCalls++
				return nil, errors.New("must not authorize")
			})
			defer restore()
			options := OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: filepath.Join(dir, "aliyun.json")}
			want := OAuthConfigureInvalid
			switch fault {
			case "invalid config":
				if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "invalid name":
				options.ProfileName = "bad\nname"
			case "invalid site":
				options.SiteType = "bad"
			case "invalid account":
				options.ExpectedAccountID = "bad"
			case "cache root error":
				nativeOAuthCacheRootPath = func() (string, error) { return "", errors.New("root unavailable") }
				want = OAuthConfigurePersistence
			case "cache root blocked":
				if err := os.WriteFile(root, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				want = OAuthConfigurePersistence
			}
			_, err := ConfigureOAuthProfile(nil, options)
			var failure *OAuthConfigureError
			if !errors.As(err, &failure) || failure.Kind != want || loginCalls != 0 {
				t.Fatalf("preflight kind=%v, calls=%d, error=%v", failure, loginCalls, err)
			}
		})
	}
}

func TestOAuthConfigurationRetainsSiteAndAccountAcrossReauthentication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	root := filepath.Join(dir, "private")
	refresh := "first"
	restore := stubNativeOAuthConfigure(t, root, func(_ context.Context, options OAuthLoginOptions) (*OAuthLoginResult, error) {
		if options.SiteType != "INTL" {
			t.Fatalf("reauthentication lost site: %s", options.SiteType)
		}
		return &OAuthLoginResult{SiteType: "INTL", AccessToken: "access", RefreshToken: refresh, AccessTokenExpire: 4102444800}, nil
	})
	defer restore()
	options := OAuthConfigureOptions{ProfileName: " production ", SiteType: "INTL", ConfigPath: path, AliyunConfigPath: filepath.Join(dir, "aliyun.json"), ConfirmAccount: acceptOAuthAccount}
	first, err := ConfigureOAuthProfile(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ecconfig.LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	firstState := store.NativeOAuthProfileState("production")
	options.ProfileName, options.SiteType, options.ConfirmAccount = "production", "", func(string, string) error { t.Fatal("existing account was not pinned"); return nil }
	refresh = "second"
	second, err := ConfigureOAuthProfile(context.Background(), options)
	if err != nil || second.AccountID != first.AccountID || second.SiteType != "INTL" {
		t.Fatalf("second login=%+v, %v", second, err)
	}
	store, err = ecconfig.LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	if state.Generation == firstState.Generation {
		t.Fatal("reauthentication reused old login generation")
	}
	cachePath := credentialCacheEntryPath(root, nativeOAuthCacheSource, "production")
	entry, found, err := loadAnyCredentialCacheEntry(context.Background(), cachePath)
	if err != nil || !found || entry.OAuthRefreshToken != "second" {
		t.Fatalf("reauthenticated cache=%+v, %t, %v", entry, found, err)
	}
}

func TestOAuthConfigurationPropagatesValidationAndConfirmationFailures(t *testing.T) {
	for _, fault := range []string{"credential validation", "confirmation", "config changed"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			cause := errors.New("fixture failure")
			restore := stubNativeOAuthConfigure(t, filepath.Join(dir, "private"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
				return &OAuthLoginResult{SiteType: "CN", AccessToken: "access", RefreshToken: "refresh"}, nil
			})
			defer restore()
			options := OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: filepath.Join(dir, "aliyun.json"), ConfirmAccount: acceptOAuthAccount}
			if fault == "credential validation" {
				validateOAuthLoginCredential = func(context.Context, *OAuthLoginResult) (*oauthLoginCredential, error) { return nil, cause }
			}
			if fault == "confirmation" {
				options.ConfirmAccount = func(string, string) error { return cause }
			}
			if fault == "config changed" {
				options.ConfirmAccount = func(string, string) error { return os.WriteFile(path, []byte("invalid"), 0o600) }
			}
			_, err := ConfigureOAuthProfile(context.Background(), options)
			if err == nil {
				t.Fatal("configuration failure ignored")
			}
			if fault != "config changed" && !errors.Is(err, cause) {
				t.Fatalf("failure cause lost: %v", err)
			}
			if _, statErr := os.Stat(credentialCacheEntryPath(filepath.Join(dir, "private"), nativeOAuthCacheSource, "default")); !os.IsNotExist(statErr) {
				t.Fatalf("failed login installed credentials: %v", statErr)
			}
		})
	}
}
