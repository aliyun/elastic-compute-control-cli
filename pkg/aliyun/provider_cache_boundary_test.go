package aliyun

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
)

func TestInteractiveProvidersRejectCancellationAndUnavailableSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	oauth := &oauthProfileCredentialsProvider{}
	sso := &cloudSSOProfileCredentialsProvider{}
	if err := oauth.gate.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sso.gate.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := oauth.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("OAuth ignored cancellation: %v", err)
	}
	if _, err := sso.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("SSO ignored cancellation: %v", err)
	}
	oauth.gate.Unlock()
	sso.gate.Unlock()
	if _, err := oauth.GetCredentials(); err == nil {
		t.Fatal("unavailable OAuth source accepted")
	}
	if _, err := sso.GetCredentials(); err == nil {
		t.Fatal("unavailable SSO source accepted")
	}
	if credentials, _ := cachedProfileCredentials(map[string]any{"sts_expiration": time.Now().Add(time.Hour).Unix()}); credentials != nil {
		t.Fatal("incomplete cached STS accepted")
	}
	if oauth.withRecovery(nil) != nil {
		t.Fatal("nil error converted to reauthentication failure")
	}
}

func TestNativeOAuthMissingOrMismatchedCacheFailsBeforeRefresh(t *testing.T) {
	for _, fault := range []string{"missing after construction", "mismatched at construction", "corrupt at construction", "corrupt after construction"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "config.json")
			cacheRoot := filepath.Join(root, "private")
			profile := map[string]any{"name": "production", "mode": "OAuth", "oauth_site_type": "CN", "oauth_generation": "login"}
			writeCredentialConfigForTest(t, configPath, profile)
			path := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "production")
			entry := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: credentialSourceGeneration(profile, credentialModeOAuth), OAuthRefreshToken: "refresh"}
			if fault == "mismatched at construction" {
				entry.SourceGeneration = "other-login"
			}
			if err := storeCredentialCacheEntry(context.Background(), path, entry); err != nil {
				t.Fatal(err)
			}
			if fault == "corrupt at construction" {
				if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			provider, err := newNativeOAuthProfileCredentialsProvider(profile, "production", configPath, cacheRoot)
			if fault == "mismatched at construction" {
				if !errors.Is(err, ErrCredentialProfileChanged) {
					t.Fatalf("foreign cache generation accepted: %v", err)
				}
				return
			}
			if fault == "corrupt at construction" {
				if err == nil {
					t.Fatal("corrupt cache accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fault == "missing after construction" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			original := refreshOAuthCredential
			refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
				calls++
				return nil, nil, errors.New("must not refresh")
			}
			t.Cleanup(func() { refreshOAuthCredential = original })
			if _, err := provider.Acquire(context.Background()); err == nil || calls != 0 {
				t.Fatalf("invalid native cache refreshed: %d calls, %v", calls, err)
			}
		})
	}
}

func TestCloudSSOProvidersSharePersistedSTSAndReuseIdentityProof(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	cacheRoot := filepath.Join(root, "private")
	profile := map[string]any{"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.invalid", "cloud_sso_account_id": "account", "cloud_sso_access_config": "access"}
	writeCredentialConfigForTest(t, configPath, profile)
	calls, verifications := 0, 0
	client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not fetch") })
	verify := func(context.Context, *credentialSnapshot, string) error { verifications++; return nil }
	first, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", configPath, client, verify, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	entry := credentialCacheEntry{Mode: credentialModeCloudSSO, SourceGeneration: credentialSourceGeneration(profile, credentialModeCloudSSO), AccessKeyID: "cached-id", AccessKeySecret: "cached-secret", SecurityToken: "cached-token", STSExpiration: time.Now().Add(time.Hour).Unix()}
	path := credentialCacheEntryPath(cacheRoot, configPath, "sso")
	if err := storeCredentialCacheEntry(context.Background(), path, entry); err != nil {
		t.Fatal(err)
	}
	second, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", configPath, client, verify, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []*cloudSSOProfileCredentialsProvider{first, second} {
		for attempt := 0; attempt < 2; attempt++ {
			if got, err := provider.Acquire(context.Background()); err != nil || got.AccessKeyID != "cached-id" {
				t.Fatalf("persisted SSO credential not reused: %v", err)
			}
		}
	}
	if calls != 0 || verifications != 2 {
		t.Fatalf("cache reuse fetched or reverified unchanged identity: fetch=%d verify=%d", calls, verifications)
	}
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", configPath, client, verify, cacheRoot); err == nil {
		t.Fatal("malformed SSO cache accepted")
	}
}

func TestOAuthRejectsIncompleteRefreshAndSSOInvalidIdentityPolicy(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh")
	original := refreshOAuthCredential
	t.Cleanup(func() { refreshOAuthCredential = original })
	refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		return &credentialproviders.Credentials{AccessKeyId: "id"}, &oauthCredentialProfileUpdate{stsExpire: time.Now().Add(time.Hour).Unix()}, nil
	}
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, nil, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Acquire(context.Background()); err == nil {
		t.Fatal("incomplete refreshed STS returned as credentials")
	}
	sso := &cloudSSOProfileCredentialsProvider{identityPolicy: credentialIdentityPolicy{enableVPC: true}}
	if _, err := sso.verifiedSnapshot(context.Background(), nil, time.Now()); err == nil {
		t.Fatal("nil SSO credentials verified")
	}
	if _, err := sso.verifiedSnapshot(context.Background(), &credentialproviders.Credentials{AccessKeyId: "id", AccessKeySecret: "secret", SecurityToken: "token"}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("missing VPC identity region accepted")
	}
}
