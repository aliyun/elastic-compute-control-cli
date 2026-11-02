package aliyun

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

type credentialReadFailure struct{}

func (credentialReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("private unreadable response")
}

func TestCredentialsURIRejectsUnreadableAndInvalidExpirationResponses(t *testing.T) {
	for _, body := range []string{"invalid", `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"bad"}`, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"2000-01-01T00:00:00Z"}`} {
		client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		provider, err := newSafeURLCredentialsProvider("https://example.invalid/credentials?token=secret", client)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Acquire(nil); err == nil || strings.Contains(err.Error(), "token=secret") {
			t.Fatalf("URI error ignored or secret leaked: %v", err)
		}
	}
	provider, err := newSafeURLCredentialsProvider("https://example.invalid/credentials", credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(credentialReadFailure{})}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetCredentials(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unreadable response error=%v", err)
	}
	for _, uri := range []string{"", "://", "file:///credentials", "https://"} {
		if _, err := newSafeURLCredentialsProvider(uri, nil); err == nil {
			t.Fatalf("invalid URI accepted: %q", uri)
		}
	}
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	provider, err = newSafeURLCredentialsProvider("https://example.invalid/credentials", credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return metadataHTTPResponse(200, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"`+expiration+`"}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := provider.Acquire(nil); err != nil || snapshot.Type != "sts" || snapshot.ProviderName != "credential_uri" || snapshot.ExpiresAt.IsZero() {
		t.Fatalf("URI snapshot=%+v, %v", snapshot, err)
	}
}

func TestEnvironmentCredentialResolutionRejectsIncompleteAndConflictingSources(t *testing.T) {
	for _, env := range []map[string]string{
		{"ALIBABA_CLOUD_ACCESS_KEY_ID": "id"},
		{"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "provider"},
		{"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "invalid", "ALIBABA_CLOUD_OIDC_TOKEN_FILE": "unused", "ALIBABA_CLOUD_ROLE_ARN": "acs:ram::1111111111111111:role/test"},
		{"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1111111111111111:oidc-provider/test", "ALIBABA_CLOUD_OIDC_TOKEN_FILE": "unused", "ALIBABA_CLOUD_ROLE_ARN": "invalid"},
		{"ALIBABA_CLOUD_OIDC_PROVIDER_ARN": "acs:ram::1111111111111111:oidc-provider/test", "ALIBABA_CLOUD_OIDC_TOKEN_FILE": "unused", "ALIBABA_CLOUD_ROLE_ARN": "acs:ram::2222222222222222:role/test"},
		{"ALIBABA_CLOUD_CREDENTIALS_URI": "http://remote.invalid/credentials"},
		{"ALIBABA_CLOUD_CREDENTIALS_URI": "https://example.invalid/credentials", credentialExternalDisabledEnv: "true"},
	} {
		if _, err := resolveEnvironmentCredential(mapGetenv(env)); err == nil {
			t.Fatalf("invalid environment source accepted: %#v", env)
		}
	}
	for _, profile := range []map[string]any{{}, {"ram_role_arn": "invalid"}} {
		if _, err := assumeRoleCredential(&staticCredentialAcquirer{}, profile, mapGetenv(nil), credentialModeRamRoleArn); err == nil {
			t.Fatal("invalid role ARN accepted")
		}
	}
	if _, err := assumeRoleCredential(nil, nil, mapGetenv(nil), credentialModeRamRoleArn); err == nil {
		t.Fatal("missing role source accepted")
	}
}

func TestProfileResolutionReportsMalformedSourcesAndRegionProvenance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	ecctlPath, aliyunPath := filepath.Join(dir, "ecctl.json"), filepath.Join(dir, "aliyun.json")
	for _, path := range []string{ecctlPath, aliyunPath} {
		if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := resolveOpenAPIProfile("test", ecctlPath, ecconfig.ResolvedRegion{}, mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath}))
		if appErrorCode(err) != "InvalidConfig" {
			t.Fatalf("malformed source %s=%v", path, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, ignore := range []bool{false, true} {
		for _, alias := range []string{"ECCTL_REGION", "ALIBABA_CLOUD_REGION_ID"} {
			env := map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath, "ALIBABA_CLOUD_ACCESS_KEY_ID": "id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "secret", alias: "cn-hangzhou"}
			if ignore {
				env["ALIBABA_CLOUD_IGNORE_PROFILE"] = "true"
			}
			resolved, err := resolveOpenAPIProfile("", ecctlPath, ecconfig.ResolvedRegion{}, mapGetenv(env))
			want := ecconfig.RegionSourceAlibabaEnv
			if alias == "ECCTL_REGION" {
				want = ecconfig.RegionSourceECCTL
			}
			if err != nil || resolved.RegionID != "cn-hangzhou" || resolved.RegionSource != want {
				t.Fatalf("region provenance=%+v, %v", resolved, err)
			}
		}
	}
	if _, err := resolveOpenAPIProfile("", "", ecconfig.ResolvedRegion{}, nil); err == nil {
		t.Fatal("missing default credentials accepted")
	}
	raw, err := json.Marshal(map[string]any{"current": "ecctl", "profiles": []any{map[string]any{"name": "ecctl", "mode": "AK", "access_key_id": "id", "access_key_secret": "secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ecctlPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved, err := resolveOpenAPIProfile("", ecctlPath, ecconfig.ResolvedRegion{}, mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath})); err != nil || resolved.Name != "ecctl" {
		t.Fatalf("current profile=%+v, %v", resolved, err)
	}
}
