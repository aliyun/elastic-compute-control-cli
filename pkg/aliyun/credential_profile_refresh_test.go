package aliyun

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestOAuthProfileReusesValidCachedSTSWithoutNetwork(t *testing.T) {
	profile := cachedInteractiveProfile("OAuth")
	profile["oauth_site_type"] = "CN"
	provider, err := newOAuthProfileCredentialsProvider(profile, "oauth", filepath.Join(t.TempDir(), "missing.json"), testCredentialCacheRoot(t))
	if err != nil {
		t.Fatalf("newOAuthProfileCredentialsProvider: %v", err)
	}
	credentials, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if credentials.AccessKeyId != "cached-id" || credentials.SecurityToken != "cached-token" {
		t.Fatalf("credentials = %#v", credentials)
	}
}

func TestOAuthHelperProcessDoesNotLeakCredentials(t *testing.T) {
	const (
		refreshSentinel = "oauth-refresh-sentinel-cycle2"
		accessSentinel  = "oauth-access-sentinel-cycle2"
		secretSentinel  = "oauth-secret-sentinel-cycle2"
	)
	if os.Getenv("ECCTL_OAUTH_HELPER") == "1" {
		oauthEndpoints["CN"] = struct {
			baseURL   string
			signInURL string
			clientID  string
		}{baseURL: os.Getenv("ECCTL_OAUTH_ENDPOINT"), signInURL: "https://signin.example.com", clientID: "test-client"}
		transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- isolated httptest server
		client := &http.Client{Transport: transport}
		config, _, err := loadConfigObject(os.Getenv("ECCTL_OAUTH_CONFIG"))
		if err != nil {
			t.Fatal(err)
		}
		profile, _ := configProfile(config, "oauth")
		provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", os.Getenv("ECCTL_OAUTH_CONFIG"), client, os.Getenv("ECCTL_OAUTH_CACHE_ROOT"))
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := provider.Acquire(context.Background())
		if err != nil || snapshot.AccessKeySecret != secretSentinel {
			t.Fatalf("OAuth acquisition failed: %v", err)
		}
		return
	}

	var mu sync.Mutex
	receivedRefresh := false
	receivedAccess := false
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/token":
			_ = request.ParseForm()
			mu.Lock()
			receivedRefresh = request.Form.Get("refresh_token") == refreshSentinel
			mu.Unlock()
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, `{"access_token":"`+accessSentinel+`","refresh_token":"rotated-refresh","expires_in":3600}`)
		case "/v1/exchange":
			mu.Lock()
			receivedAccess = request.Header.Get("Authorization") == "Bearer "+accessSentinel
			mu.Unlock()
			response.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(response, `{"accessKeyId":"oauth-id","accessKeySecret":"`+secretSentinel+`","securityToken":"oauth-sts","expiration":"`+expiration.Format(time.RFC3339)+`"}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeJSONFile(t, configPath, map[string]any{
		"current": "oauth", "profiles": []any{map[string]any{
			"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": refreshSentinel,
		}},
	})
	command := exec.Command(os.Args[0], "-test.run=^TestOAuthHelperProcessDoesNotLeakCredentials$")
	command.Env = append(os.Environ(),
		"DEBUG=credential", "ECCTL_OAUTH_HELPER=1", "ECCTL_OAUTH_ENDPOINT="+server.URL, "ECCTL_OAUTH_CONFIG="+configPath,
		"ECCTL_OAUTH_CACHE_ROOT="+filepath.Join(dir, "credentials-v2"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("OAuth helper failed: %v\n%s", err, output)
	}
	for _, secret := range []string{refreshSentinel, accessSentinel, secretSentinel} {
		if bytes.Contains(output, []byte(secret)) {
			t.Fatalf("OAuth helper output leaked credential material: %s", output)
		}
	}
	mu.Lock()
	refreshOK, accessOK := receivedRefresh, receivedAccess
	mu.Unlock()
	if !refreshOK || !accessOK {
		t.Fatalf("OAuth server did not receive expected sentinels: refresh=%t access=%t", refreshOK, accessOK)
	}
}

func TestOAuthRedirectTargetNeverReceivesCredential(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls++ }))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	original := oauthEndpoints["CN"]
	oauthEndpoints["CN"] = struct {
		baseURL   string
		signInURL string
		clientID  string
	}{baseURL: source.URL, signInURL: "https://signin.example.com", clientID: "test-client"}
	t.Cleanup(func() { oauthEndpoints["CN"] = original })
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Acquire(context.Background()); err == nil {
		t.Fatal("redirecting OAuth endpoint succeeded")
	}
	if destinationCalls != 0 {
		t.Fatalf("OAuth redirect destination received %d requests", destinationCalls)
	}
}

func TestOAuthCancellationBeforeResponseLeavesConfigUnchanged(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	original, _ := os.ReadFile(configPath)
	started := make(chan struct{})
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := provider.Acquire(ctx)
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("OAuth cancellation error = %v", err)
	}
	current, _ := os.ReadFile(configPath)
	if string(current) != string(original) {
		t.Fatalf("OAuth cancellation changed config: %q", current)
	}
}

func TestOAuthValidResponsePersistsAfterRequestContextCancellation(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	ctx, cancel := context.WithCancel(context.Background())
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	calls := 0
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return jsonHTTPResponse(http.StatusOK, `{"access_token":"access-after","refresh_token":"refresh-after","expires_in":3600}`), nil
		}
		cancel()
		return jsonHTTPResponse(http.StatusOK, `{"accessKeyId":"id-after","accessKeySecret":"secret-after","securityToken":"sts-after","expiration":"`+expiration.Format(time.RFC3339)+`"}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := provider.Acquire(ctx)
	if err != nil || snapshot.AccessKeyID != "id-after" {
		t.Fatalf("OAuth acquisition = %#v, %v", snapshot, err)
	}
	updated, _, err := loadConfigObject(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedProfile, _ := configProfile(updated, "oauth")
	if stringMapField(updatedProfile, "oauth_refresh_token") != "refresh-before" || stringMapField(updatedProfile, "access_key_id") != "" {
		t.Fatalf("OAuth source profile was modified: %#v", updatedProfile)
	}
	if provider.cacheEntry.OAuthRefreshToken != "refresh-after" || provider.cacheEntry.AccessKeyID != "id-after" {
		t.Fatalf("rotated OAuth credential was not cached privately: %#v", provider.cacheEntry)
	}
	if sidecars, err := filepath.Glob(configPath + ".*"); err != nil || len(sidecars) != 0 {
		t.Fatalf("shared aliyun config sidecars = %#v, %v", sidecars, err)
	}
}

func TestOAuthPersistenceFailureReturnsNoCredentialSuccess(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	calls := 0
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if err := os.Chmod(cacheDir, 0o500); err != nil {
				t.Fatal(err)
			}
			return jsonHTTPResponse(http.StatusOK, `{"access_token":"access-after","refresh_token":"refresh-after","expires_in":3600}`), nil
		}
		t.Fatal("OAuth exchange ran after token persistence failed")
		return nil, nil
	}), cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cacheDir, 0o700) })
	snapshot, err := provider.Acquire(context.Background())
	if snapshot != nil || !errors.Is(err, ErrCredentialStatePersistenceFailed) || calls != 1 {
		t.Fatalf("OAuth read-only persistence = %#v, %v", snapshot, err)
	}
}

func TestOAuthPersistsRotatedTokenBeforeCredentialExchange(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	cacheRoot := filepath.Join(t.TempDir(), "credentials-v2")
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	tokenCalls, exchangeCalls := 0, 0
	firstClient := credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v1/token":
			tokenCalls++
			return jsonHTTPResponse(http.StatusOK, `{"access_token":"access-after","refresh_token":"refresh-after","expires_in":3600}`), nil
		case "/v1/exchange":
			exchangeCalls++
			return jsonHTTPResponse(http.StatusInternalServerError, `{}`), nil
		default:
			t.Fatalf("unexpected OAuth path %q", request.URL.Path)
			return nil, nil
		}
	})
	first, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, firstClient, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := first.Acquire(context.Background()); snapshot != nil || err == nil {
		t.Fatalf("first Acquire = %#v, %v", snapshot, err)
	}
	entry, ok, err := loadCredentialCacheEntry(context.Background(), first.cachePath, credentialModeOAuth, first.generation)
	if err != nil || !ok || entry.OAuthRefreshToken != "refresh-after" || entry.OAuthAccessToken != "access-after" {
		t.Fatalf("durable token entry = %#v, ok=%t err=%v", entry, ok, err)
	}
	second, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v1/token" {
			t.Fatal("durable rotated token triggered another token refresh")
		}
		exchangeCalls++
		return jsonHTTPResponse(http.StatusOK, `{"accessKeyId":"id-after","accessKeySecret":"secret-after","securityToken":"sts-after","expiration":"`+expiration.Format(time.RFC3339)+`"}`), nil
	}), cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := second.Acquire(context.Background())
	if err != nil || snapshot.AccessKeyID != "id-after" {
		t.Fatalf("second Acquire = %#v, %v", snapshot, err)
	}
	if tokenCalls != 1 || exchangeCalls != 2 {
		t.Fatalf("OAuth calls token=%d exchange=%d", tokenCalls, exchangeCalls)
	}
}

func writeOAuthRefreshProfile(t *testing.T, refreshToken string) (string, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configPath := filepath.Join(dir, "config.json")
	writeJSONFile(t, configPath, map[string]any{
		"current": "oauth", "profiles": []any{map[string]any{
			"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": refreshToken,
		}},
	})
	config, _, err := loadConfigObject(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := configProfile(config, "oauth")
	return configPath, profile
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func acceptCloudSSOAccount(context.Context, *credentialSnapshot, string) error { return nil }

func testCredentialCacheRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "credentials-v2")
}

func TestCloudSSOProfileReusesCachedSTSAfterLoginTokenExpires(t *testing.T) {
	profile := cachedInteractiveProfile("CloudSSO")
	profile["cloud_sso_sign_in_url"] = "https://signin.example.com"
	profile["cloud_sso_account_id"] = "123"
	profile["cloud_sso_access_config"] = "ac-1"
	profile["access_token"] = "expired-login-token"
	profile["cloud_sso_access_token_expire"] = time.Now().Add(-time.Hour).Unix()
	calls := 0
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", filepath.Join(t.TempDir(), "missing.json"), credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	}), acceptCloudSSOAccount, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatalf("newCloudSSOProfileCredentialsProvider: %v", err)
	}
	credentials, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if calls != 0 || credentials.AccessKeyId != "cached-id" {
		t.Fatalf("calls = %d credentials = %#v", calls, credentials)
	}
}

func TestCloudSSORefreshPersistsSTSWithoutOverwritingProfile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeJSONFile(t, configPath, map[string]any{
		"current": "sso",
		"custom":  "keep-top-level",
		"profiles": []any{map[string]any{
			"name": "sso", "mode": "CloudSSO", "region_id": "cn-hangzhou", "custom_profile_field": "keep-profile",
			"cloud_sso_sign_in_url": "https://signin.example.com/base", "cloud_sso_account_id": "123",
			"cloud_sso_access_config": "ac-1", "access_token": "login-token",
			"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
		}},
	})
	config, _, err := loadConfigObject(configPath)
	if err != nil {
		t.Fatalf("loadConfigObject: %v", err)
	}
	profile, _ := configProfile(config, "sso")
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", configPath, credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/cloud-credentials" || request.Header.Get("Authorization") != "Bearer login-token" {
			t.Fatalf("request = %#v", request)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"CloudCredential":{"AccessKeyId":"new-id","AccessKeySecret":"new-secret","SecurityToken":"new-token","Expiration":"` + expiration.Format(time.RFC3339) + `"}}`)),
		}, nil
	}), acceptCloudSSOAccount, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatalf("newCloudSSOProfileCredentialsProvider: %v", err)
	}
	credentials, err := provider.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if credentials.AccessKeyId != "new-id" || credentials.SecurityToken != "new-token" {
		t.Fatalf("credentials = %#v", credentials)
	}
	updated, _, err := loadConfigObject(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	updatedProfile, ok := configProfile(updated, "sso")
	if !ok || stringMapField(updatedProfile, "access_key_id") != "" || int64MapField(updatedProfile, "sts_expiration") != 0 {
		t.Fatalf("updated profile = %#v", updatedProfile)
	}
	if updated["custom"] != "keep-top-level" || updatedProfile["custom_profile_field"] != "keep-profile" || updatedProfile["region_id"] != "cn-hangzhou" {
		t.Fatalf("unrelated configuration was overwritten: %#v", updated)
	}
	if provider.cacheEntry.AccessKeyID != "new-id" || provider.cacheEntry.STSExpiration != expiration.Unix() {
		t.Fatalf("CloudSSO credential cache = %#v", provider.cacheEntry)
	}
}

func TestCloudSSORejectsUnexpectedAccountBeforeCacheWrite(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	profile := map[string]any{
		"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
		"cloud_sso_account_id": "expected-account", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
		"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
	}
	writeJSONFile(t, configPath, map[string]any{"current": "sso", "profiles": []any{profile}})
	cacheRoot := filepath.Join(dir, "credentials-v2")
	verified := false
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", configPath, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"CloudCredential":{"AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"sts","Expiration":"`+expiration.Format(time.RFC3339)+`"}}`), nil
	}), func(_ context.Context, _ *credentialSnapshot, expected string) error {
		verified = true
		if expected != "expected-account" {
			t.Fatalf("expected account = %q", expected)
		}
		return ErrCredentialAccountMismatch
	}, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := provider.Acquire(context.Background()); snapshot != nil || !errors.Is(err, ErrCredentialAccountMismatch) {
		t.Fatalf("Acquire = %#v, %v", snapshot, err)
	}
	if !verified {
		t.Fatal("CloudSSO account was not verified")
	}
	if _, err := os.Stat(provider.cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched CloudSSO credential was cached: %v", err)
	}
}

func TestCloudSSOIdentityProofAvoidsDuplicateOperationProbe(t *testing.T) {
	profile := cachedInteractiveProfile(credentialModeCloudSSO)
	profile["cloud_sso_sign_in_url"] = "https://signin.example.com"
	profile["cloud_sso_account_id"] = "1234567890123456"
	profile["cloud_sso_access_config"] = "ac-1"
	verifyCalls := 0
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", filepath.Join(t.TempDir(), "missing.json"), nil, func(_ context.Context, snapshot *credentialSnapshot, expected string) error {
		verifyCalls++
		identity := telemetry.Identity{Hash: "cloudsso-hash", Type: "AssumedRoleUser"}
		snapshot.IdentityProof = &credentialIdentityProof{
			identity: identity, accountID: expected, endpoint: "sts.aliyuncs.com", fingerprint: credentialSnapshotFingerprint(snapshot),
		}
		return nil
	}, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := provider.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	originalResolver := resolveCredentialSnapshotIdentity
	operationCalls := 0
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		operationCalls++
		return telemetry.Identity{}, errors.New("duplicate operation identity probe")
	}
	defer func() { resolveCredentialSnapshotIdentity = originalResolver }()
	resolved := resolvedOpenAPIProfile{
		Acquirer: provider, ExpectedAccountID: "1234567890123456", ExpectedIdentityType: "AssumedRoleUser",
		IdentityPolicy: credentialIdentityPolicy{}, PinCredentialIdentity: true,
	}
	if guard, err := newOperationIdentityGuard(context.Background(), resolved, snapshot); err != nil || guard == nil || verifyCalls != 1 || operationCalls != 0 {
		t.Fatalf("guard=%#v verify=%d operation=%d error=%v", guard, verifyCalls, operationCalls, err)
	}
}

func TestOAuthRefreshDetectsExternalRewriteAndPreservesExactBytes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeJSONFile(t, configPath, map[string]any{
		"current": "oauth",
		"profiles": []any{map[string]any{
			"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "old-token",
		}},
	})
	config, _, err := loadConfigObject(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := configProfile(config, "oauth")
	provider, err := newOAuthProfileCredentialsProvider(profile, "oauth", configPath, filepath.Join(dir, "credentials-v2"))
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	originalRefresh := refreshOAuthCredential
	refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		close(started)
		<-release
		expires := time.Now().Add(time.Hour).Unix()
		return &credentialproviders.Credentials{AccessKeyId: "new-id", AccessKeySecret: "new-secret", SecurityToken: "new-sts"}, &oauthCredentialProfileUpdate{
			refreshToken: "new-refresh", accessToken: "new-access", accessKeyID: "new-id", accessKeySecret: "new-secret", securityToken: "new-sts",
			accessTokenExpire: expires, stsExpire: expires,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	result := make(chan error, 1)
	go func() {
		_, err := provider.Acquire(context.Background())
		result <- err
	}()
	<-started
	external := []byte("{\n  \"current\": \"external\",\n  \"profiles\": []\n}\n")
	if err := os.WriteFile(configPath, external, 0o600); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("refresh error = %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil || string(got) != string(external) {
		t.Fatalf("external bytes changed: %q, %v", got, err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".ecctl-credential-*.json"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("refresh temp files = %#v, %v", temps, err)
	}
}

func TestConcurrentOAuthRefreshesSharePrivateCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeJSONFile(t, path, map[string]any{
		"current": "oauth", "profiles": []any{map[string]any{
			"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "token-0",
		}},
	})
	config, _, _ := loadConfigObject(path)
	profile, _ := configProfile(config, "oauth")
	cacheRoot := filepath.Join(dir, "credentials-v2")
	first, _ := newOAuthProfileCredentialsProvider(profile, "oauth", path, cacheRoot)
	second, _ := newOAuthProfileCredentialsProvider(profile, "oauth", path, cacheRoot)
	originalRefresh := refreshOAuthCredential
	var mu sync.Mutex
	var seen []string
	refreshOAuthCredential = func(_ context.Context, profile map[string]any, _ credentialHTTPClient, _ oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		mu.Lock()
		seen = append(seen, stringMapField(profile, "oauth_refresh_token"))
		n := len(seen)
		mu.Unlock()
		expires := time.Now().Add(time.Hour).Unix()
		return &credentialproviders.Credentials{AccessKeyId: fmt.Sprintf("id-%d", n), AccessKeySecret: "secret", SecurityToken: "sts"}, &oauthCredentialProfileUpdate{
			refreshToken: fmt.Sprintf("token-%d", n), accessToken: "access", accessKeyID: fmt.Sprintf("id-%d", n), accessKeySecret: "secret", securityToken: "sts",
			accessTokenExpire: expires, stsExpire: expires,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for _, provider := range []*oauthProfileCredentialsProvider{first, second} {
		provider := provider
		go func() {
			<-start
			_, err := provider.Acquire(context.Background())
			errorsCh <- err
		}()
	}
	close(start)
	succeeded := 0
	for i := 0; i < 2; i++ {
		err := <-errorsCh
		switch {
		case err == nil:
			succeeded++
		default:
			t.Fatalf("concurrent OAuth refresh error = %v", err)
		}
	}
	if succeeded != 2 || len(seen) != 1 || seen[0] != "token-0" {
		t.Fatalf("succeeded=%d refresh token sequence=%#v", succeeded, seen)
	}
}

func TestOAuthProviderRejectsExternalReloginForSameSite(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-account-a")
	provider, err := newOAuthProfileCredentialsProviderWithClient(profile, "oauth", configPath, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("credential endpoint must not be called")
	}))
	if err != nil {
		t.Fatal(err)
	}
	relogged := cloneStringAnyMap(profile)
	relogged["oauth_refresh_token"] = "refresh-account-b"
	relogged["oauth_access_token"] = "access-account-b"
	relogged["access_key_id"] = "id-account-b"
	relogged["access_key_secret"] = "secret-account-b"
	relogged["sts_token"] = "sts-account-b"
	relogged["sts_expiration"] = time.Now().Add(time.Hour).Unix()
	writeJSONFile(t, configPath, map[string]any{"current": "oauth", "profiles": []any{relogged}})

	snapshot, err := provider.Acquire(context.Background())
	if snapshot != nil || !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("external OAuth relogin = %#v, %v", snapshot, err)
	}
}

func TestOAuthProviderAdvancesGenerationAfterOwnRefresh(t *testing.T) {
	configPath, profile := writeOAuthRefreshProfile(t, "refresh-before")
	provider, err := newOAuthProfileCredentialsProvider(profile, "oauth", configPath, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	originalRefresh := refreshOAuthCredential
	calls := 0
	refreshOAuthCredential = func(_ context.Context, profile map[string]any, _ credentialHTTPClient, _ oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		calls++
		expires := time.Now().Add(time.Minute).Unix()
		return &credentialproviders.Credentials{AccessKeyId: fmt.Sprintf("id-%d", calls), AccessKeySecret: "secret", SecurityToken: "sts"}, &oauthCredentialProfileUpdate{
			refreshToken: fmt.Sprintf("refresh-%d", calls), accessToken: "access", accessKeyID: fmt.Sprintf("id-%d", calls), accessKeySecret: "secret", securityToken: "sts",
			accessTokenExpire: expires, stsExpire: expires,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	if _, err := provider.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Acquire(context.Background()); err != nil {
		t.Fatalf("second provider-owned refresh = %v", err)
	}
	if calls != 2 {
		t.Fatalf("provider-owned refresh calls = %d, want 2", calls)
	}
}

func TestOAuthRefreshesForDifferentProfilesOverlapAndMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeJSONFile(t, path, map[string]any{
		"current": "one", "profiles": []any{
			map[string]any{"name": "one", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "token-one"},
			map[string]any{"name": "two", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "token-two"},
		},
	})
	config, _, _ := loadConfigObject(path)
	oneProfile, _ := configProfile(config, "one")
	twoProfile, _ := configProfile(config, "two")
	cacheRoot := testCredentialCacheRoot(t)
	one, _ := newOAuthProfileCredentialsProvider(oneProfile, "one", path, cacheRoot)
	two, _ := newOAuthProfileCredentialsProvider(twoProfile, "two", path, cacheRoot)
	originalRefresh := refreshOAuthCredential
	started := make(chan string, 2)
	release := make(chan struct{})
	refreshOAuthCredential = func(_ context.Context, profile map[string]any, _ credentialHTTPClient, _ oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		name := stringMapField(profile, "name")
		started <- name
		<-release
		expires := time.Now().Add(time.Hour).Unix()
		return &credentialproviders.Credentials{AccessKeyId: "id-" + name, AccessKeySecret: "secret", SecurityToken: "sts"}, &oauthCredentialProfileUpdate{
			refreshToken: "rotated-" + name, accessToken: "access", accessKeyID: "id-" + name, accessKeySecret: "secret", securityToken: "sts",
			accessTokenExpire: expires, stsExpire: expires,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	errorsCh := make(chan error, 2)
	for _, provider := range []*oauthProfileCredentialsProvider{one, two} {
		provider := provider
		go func() {
			_, err := provider.Acquire(context.Background())
			errorsCh <- err
		}()
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatalf("different profile refreshes did not overlap: %#v", seen)
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		name     string
		provider *oauthProfileCredentialsProvider
	}{{"one", one}, {"two", two}} {
		if item.provider.cacheEntry.OAuthRefreshToken != "rotated-"+item.name {
			t.Fatalf("profile %s private cache missing: %#v", item.name, item.provider.cacheEntry)
		}
	}
}

func TestOAuthRefreshPreservesConcurrentUnrelatedEdits(t *testing.T) {
	path, profile := writeOAuthRefreshProfile(t, "refresh-before")
	provider, _ := newOAuthProfileCredentialsProvider(profile, "oauth", path, testCredentialCacheRoot(t))
	originalRefresh := refreshOAuthCredential
	started := make(chan struct{})
	release := make(chan struct{})
	refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		close(started)
		<-release
		expires := time.Now().Add(time.Hour).Unix()
		return &credentialproviders.Credentials{AccessKeyId: "new-id", AccessKeySecret: "secret", SecurityToken: "sts"}, &oauthCredentialProfileUpdate{
			refreshToken: "refresh-after", accessToken: "access", accessKeyID: "new-id", accessKeySecret: "secret", securityToken: "sts",
			accessTokenExpire: expires, stsExpire: expires,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	result := make(chan error, 1)
	go func() {
		_, err := provider.Acquire(context.Background())
		result <- err
	}()
	<-started
	concurrent, _, _ := loadConfigObject(path)
	concurrent["custom"] = "preserve-me"
	concurrentProfile, _ := configProfile(concurrent, "oauth")
	concurrentProfile["region_id"] = "cn-shanghai"
	upsertConfigProfile(concurrent, concurrentProfile)
	writeJSONFile(t, path, concurrent)
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	updated, _, _ := loadConfigObject(path)
	updatedProfile, _ := configProfile(updated, "oauth")
	if updated["custom"] != "preserve-me" || updatedProfile["region_id"] != "cn-shanghai" || stringMapField(updatedProfile, "oauth_refresh_token") != "refresh-before" {
		t.Fatalf("unrelated edits were lost: %#v", updated)
	}
	if provider.cacheEntry.OAuthRefreshToken != "refresh-after" {
		t.Fatalf("OAuth private cache = %#v", provider.cacheEntry)
	}
}

func TestCloudSSOCachePersistenceFailureReturnsCredentialsAndRecordsOutcome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	writeJSONFile(t, path, map[string]any{
		"current": "sso", "profiles": []any{map[string]any{
			"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
			"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
			"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
		}},
	})
	config, _, _ := loadConfigObject(path)
	profile, _ := configProfile(config, "sso")
	cacheDir := filepath.Join(dir, "cache")
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", path, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		if err := os.Chmod(cacheDir, 0o500); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"CloudCredential":{"AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"sts","Expiration":"` + expiration.Format(time.RFC3339) + `"}}`))}, nil
	}), acceptCloudSSOAccount, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cacheDir, 0o700) })
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{Enabled: true, Surface: "public", ConfigPath: filepath.Join(dir, "telemetry.json")})
	snapshot, err := provider.Acquire(ctx)
	if err != nil || snapshot.AccessKeyID != "id" {
		t.Fatalf("Acquire = %#v, %v", snapshot, err)
	}
	session.Finish("ecctl call", 0)
	found := false
	for _, span := range exporter.GetSpans() {
		if span.Name == "ecctl.command" && testSpanAttributes(span.Attributes)["ecctl.credential.outcome"] == "cloudsso_cache_persist_failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("CloudSSO cache persistence outcome was not recorded")
	}
}

func TestCloudSSORequiresAbsoluteHTTPSWithoutUserInfo(t *testing.T) {
	base := map[string]any{
		"name": "sso", "mode": "CloudSSO", "cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1",
	}
	for _, rawURL := range []string{"http://signin.example.com", "/relative", "https://user:password@signin.example.com"} {
		profile := cloneStringAnyMap(base)
		profile["cloud_sso_sign_in_url"] = rawURL
		if _, err := newCloudSSOProfileCredentialsProvider(profile, "sso", filepath.Join(t.TempDir(), "missing.json"), nil); err == nil {
			t.Fatalf("CloudSSO URL %q was accepted", rawURL)
		}
	}
}

func TestCloudSSOUnauthorizedAndForbiddenRequireReauthentication(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			profile := map[string]any{
				"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
				"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
				"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
			}
			writeJSONFile(t, configPath, map[string]any{"current": "sso", "profiles": []any{profile}})
			provider, err := newCloudSSOProfileCredentialsProvider(profile, "sso", configPath, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("denied"))}, nil
			}), testCredentialCacheRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Acquire(context.Background())
			err = credentialResolutionError(err)
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialReauthenticationRequired" {
				t.Fatalf("HTTP %d error = %T %v", status, err, err)
			}
		})
	}
}

func TestCloudSSOCredentialRequestRejectsCrossOriginAndDowngradeRedirects(t *testing.T) {
	for _, targetTLS := range []bool{true, false} {
		t.Run(fmt.Sprintf("target-tls=%t", targetTLS), func(t *testing.T) {
			destinationCalls := 0
			destinationHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls++ })
			var destination *httptest.Server
			if targetTLS {
				destination = httptest.NewTLSServer(destinationHandler)
			} else {
				destination = httptest.NewServer(destinationHandler)
			}
			defer destination.Close()
			source := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Location", destination.URL)
				response.WriteHeader(http.StatusFound)
			}))
			defer source.Close()
			profile := map[string]any{
				"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": source.URL,
				"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
				"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
			}
			configPath := filepath.Join(t.TempDir(), "config.json")
			writeJSONFile(t, configPath, map[string]any{"current": "sso", "profiles": []any{profile}})
			provider, err := newCloudSSOProfileCredentialsProvider(profile, "sso", configPath, source.Client(), testCredentialCacheRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Acquire(context.Background()); err == nil {
				t.Fatal("redirecting CloudSSO endpoint succeeded")
			}
			if destinationCalls != 0 {
				t.Fatalf("credential redirect reached destination %d times", destinationCalls)
			}
		})
	}
}

func TestCloudSSOProfileRemovalFailsBeforeCredentialRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	profile := map[string]any{
		"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
		"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
		"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
	}
	writeJSONFile(t, path, map[string]any{"current": "sso", "profiles": []any{profile}})
	calls := 0
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", path, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected request")
	}), acceptCloudSSOAccount, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, path, map[string]any{"current": "other", "profiles": []any{map[string]any{"name": "other", "mode": "AK"}}})
	if _, err := provider.Acquire(context.Background()); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("profile removal error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("credential endpoint calls = %d, want zero", calls)
	}
}

func TestCloudSSOIdentityChangeFailsBeforeCredentialRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	profile := map[string]any{
		"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
		"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
		"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
	}
	writeJSONFile(t, path, map[string]any{"current": "sso", "profiles": []any{profile}})
	calls := 0
	provider, err := newCloudSSOProfileCredentialsProvider(profile, "sso", path, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected request")
	}), testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	changed := cloneStringAnyMap(profile)
	changed["cloud_sso_account_id"] = "456"
	writeJSONFile(t, path, map[string]any{"current": "sso", "profiles": []any{changed}})
	if _, err := provider.Acquire(context.Background()); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("identity change error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("credential endpoint calls = %d, want zero", calls)
	}
}

func TestCloudSSOIdentityChangeDuringFetchFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	profile := map[string]any{
		"name": "sso", "mode": "CloudSSO", "cloud_sso_sign_in_url": "https://signin.example.com",
		"cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1", "access_token": "login-token",
		"cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(),
	}
	writeJSONFile(t, path, map[string]any{"current": "sso", "profiles": []any{profile}})
	started := make(chan struct{})
	release := make(chan struct{})
	expiration := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	provider, err := newCloudSSOProfileCredentialsProviderWithVerifier(profile, "sso", path, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return jsonHTTPResponse(http.StatusOK, `{"CloudCredential":{"AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"sts","Expiration":"`+expiration.Format(time.RFC3339)+`"}}`), nil
	}), acceptCloudSSOAccount, testCredentialCacheRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := provider.Acquire(context.Background())
		result <- err
	}()
	<-started
	changed := cloneStringAnyMap(profile)
	changed["cloud_sso_account_id"] = "456"
	writeJSONFile(t, path, map[string]any{"current": "sso", "profiles": []any{changed}})
	close(release)
	if err := <-result; !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("identity change during fetch error = %v", err)
	}
	if provider.cached != nil || !provider.expiresAt.IsZero() {
		t.Fatalf("provider cached conflicting credential: cached=%#v expires=%v", provider.cached, provider.expiresAt)
	}
}

func TestCredentialProfileDigestsSeparateLogicalSettingsFromOAuthGeneration(t *testing.T) {
	base := map[string]any{
		"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN",
		"oauth_refresh_token": "refresh-one", "oauth_access_token": "access-one",
		"access_key_id": "id-one", "access_key_secret": "secret-one", "sts_token": "sts-one",
	}
	rotated := cloneStringAnyMap(base)
	rotated["oauth_refresh_token"] = "refresh-two"
	rotated["oauth_access_token"] = "access-two"
	rotated["access_key_id"] = "id-two"
	rotated["access_key_secret"] = "secret-two"
	rotated["sts_token"] = "sts-two"
	if credentialProfileIdentityDigest(base) != credentialProfileIdentityDigest(rotated) {
		t.Fatal("OAuth token rotation changed the logical settings digest")
	}
	if credentialProfileAuthDigest(base) == credentialProfileAuthDigest(rotated) {
		t.Fatal("OAuth token rotation did not change the auth generation digest")
	}
	changed := cloneStringAnyMap(rotated)
	changed["oauth_site_type"] = "INTL"
	if credentialProfileIdentityDigest(base) == credentialProfileIdentityDigest(changed) {
		t.Fatal("OAuth site change did not change the logical identity digest")
	}
	nativeOne := map[string]any{"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_generation": "one"}
	nativeTwo := cloneStringAnyMap(nativeOne)
	nativeTwo["oauth_generation"] = "two"
	if credentialProfileIdentityDigest(nativeOne) == credentialProfileIdentityDigest(nativeTwo) || credentialProfileAuthDigest(nativeOne) == credentialProfileAuthDigest(nativeTwo) {
		t.Fatal("native OAuth login generation did not change identity and auth digests")
	}
}

func TestCredentialRefreshLockBudgetCoversOAuthRequestsAndPersistence(t *testing.T) {
	minimum := 2*credentialHTTPTimeout + oauthPersistenceTimeout
	if credentialRefreshLockTimeout <= minimum {
		t.Fatalf("refresh lock timeout %s does not cover OAuth budget %s", credentialRefreshLockTimeout, minimum)
	}
}

func TestNativeOAuthStaleProviderCannotOverwriteNewCanonicalGeneration(t *testing.T) {
	dir := t.TempDir()
	cacheRoot := filepath.Join(dir, "credentials-v2")
	configA := filepath.Join(dir, "config-a.json")
	profileA := map[string]any{
		"name": "production", "mode": "OAuth", "oauth_site_type": "CN",
		"oauth_generation": "login-a", "oauth_account_id": "1111111111111111",
	}
	profileB := map[string]any{
		"name": "production", "mode": "OAuth", "oauth_site_type": "CN",
		"oauth_generation": "login-b", "oauth_account_id": "2222222222222222",
	}
	writeCredentialConfigForTest(t, configA, profileA)
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "production")
	generationA := credentialSourceGeneration(profileA, credentialModeOAuth)
	generationB := credentialSourceGeneration(profileB, credentialModeOAuth)
	if err := storeCredentialCacheEntry(context.Background(), cachePath, credentialCacheEntry{
		Mode: credentialModeOAuth, SourceGeneration: generationA,
		OAuthRefreshToken: "refresh-a", OAuthAccessToken: "access-a", OAuthAccessExpire: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	provider, err := newNativeOAuthProfileCredentialsProvider(profileA, "production", configA, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeCredentialCacheEntry(context.Background(), cachePath, credentialCacheEntry{
		Mode: credentialModeOAuth, SourceGeneration: generationB,
		OAuthRefreshToken: "refresh-b", OAuthAccessToken: "access-b", OAuthAccessExpire: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	originalRefresh := refreshOAuthCredential
	refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		refreshCalls++
		return nil, nil, errors.New("stale provider must not refresh")
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	if _, err := provider.Acquire(context.Background()); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("stale provider error = %v", err)
	}
	if refreshCalls != 0 {
		t.Fatalf("stale provider refresh calls = %d", refreshCalls)
	}
	entry, found, err := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeOAuth, generationB)
	if err != nil || !found || entry.OAuthRefreshToken != "refresh-b" {
		t.Fatalf("new canonical entry = %#v found=%t err=%v", entry, found, err)
	}
}

func TestOAuthRefreshUsesTypedExpiryAndServiceErrors(t *testing.T) {
	profile := map[string]any{
		"oauth_site_type": "CN", "oauth_refresh_token": "expired-refresh",
		"oauth_refresh_token_expire": time.Now().Add(-time.Minute).Unix(),
	}
	calls := 0
	client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not call")
	})
	_, _, err := refreshOAuthCredentialWithHTTP(context.Background(), profile, client, nil)
	var reauthentication *OAuthReauthenticationError
	if !errors.As(err, &reauthentication) || calls != 0 {
		t.Fatalf("expired refresh error=%v calls=%d", err, calls)
	}
	profile["oauth_refresh_token_expire"] = time.Now().Add(time.Hour).Unix()
	client = credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`), nil
	})
	_, _, err = refreshOAuthCredentialWithHTTP(context.Background(), profile, client, nil)
	var remote *OAuthRemoteError
	if !errors.As(err, &remote) || remote.StatusCode != http.StatusServiceUnavailable || remote.Code != "temporarily_unavailable" {
		t.Fatalf("service refresh error = %#v raw=%v", remote, err)
	}
}

func TestOAuthProviderRecoveryCommandUsesAbsoluteConfigPath(t *testing.T) {
	dir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	provider, err := newOAuthProfileCredentialsProviderWithClient(
		map[string]any{"name": "production", "mode": "OAuth", "oauth_site_type": "CN"},
		"production", "relative.json", nil, filepath.Join(dir, "cache"),
	)
	if err != nil {
		t.Fatal(err)
	}
	recoveryErr := provider.withRecovery(&OAuthReauthenticationError{Reason: "expired"})
	var recovery interface{ RecoveryCommand() []string }
	if !errors.As(recoveryErr, &recovery) {
		t.Fatalf("recovery error = %v", recoveryErr)
	}
	want, err := filepath.Abs("relative.json")
	if err != nil {
		t.Fatal(err)
	}
	command := recovery.RecoveryCommand()
	if !slices.Contains(command, filepath.Clean(want)) {
		t.Fatalf("provider recovery command = %#v", command)
	}
}

func writeCredentialConfigForTest(t *testing.T, path string, profile map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"current": profile["name"], "profiles": []any{profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func cachedInteractiveProfile(mode string) map[string]any {
	return map[string]any{
		"name": "profile", "mode": mode,
		"access_key_id": "cached-id", "access_key_secret": "cached-secret", "sts_token": "cached-token",
		"sts_expiration": time.Now().Add(time.Hour).Unix(),
	}
}
