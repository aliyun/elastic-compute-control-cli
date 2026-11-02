package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

type unreadableCredentialBody struct{}

func (unreadableCredentialBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (unreadableCredentialBody) Close() error             { return nil }

func TestOAuthRefreshTransportAndBodyFailures(t *testing.T) {
	for _, stage := range []string{"refresh", "exchange"} {
		for _, fault := range []string{"transport", "body"} {
			profile := map[string]any{"oauth_site_type": "CN", "oauth_refresh_token": "refresh"}
			if stage == "exchange" {
				profile["oauth_access_token"], profile["oauth_access_token_expire"] = "access", time.Now().Add(time.Hour).Unix()
			}
			cause := errors.New("offline transport failed")
			client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
				if fault == "transport" {
					return nil, cause
				}
				return &http.Response{StatusCode: 200, Body: unreadableCredentialBody{}}, nil
			})
			_, _, err := refreshOAuthCredentialWithHTTP(nil, profile, client, nil)
			if err == nil || fault == "transport" && !errors.Is(err, cause) {
				t.Fatalf("%s/%s failure lost: %v", stage, fault, err)
			}
		}
	}
	if _, _, err := refreshOAuthCredentialWithHTTP(nil, map[string]any{"oauth_site_type": "invalid"}, nil, nil); err == nil {
		t.Fatal("invalid OAuth site accepted")
	}
	if _, _, err := refreshOAuthCredentialWithHTTP(nil, map[string]any{"oauth_site_type": "CN"}, nil, nil); err == nil {
		t.Fatal("missing access token accepted")
	}
}

func TestCloudSSORejectsMalformedCredentialsWithoutPersisting(t *testing.T) {
	for _, body := range []string{"invalid", `{}`, `{"CloudCredential":{"Expiration":"invalid"}}`, `{"CloudCredential":{"Expiration":"2000-01-01T00:00:00Z"}}`, `{"CloudCredential":{"Expiration":"2099-01-01T00:00:00Z"}}`} {
		provider := &cloudSSOProfileCredentialsProvider{client: credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		profile := map[string]any{"access_token": "access", "cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(), "cloud_sso_sign_in_url": "https://signin.example.invalid"}
		if _, _, err := provider.fetch(nil, profile); err == nil {
			t.Fatalf("malformed CloudSSO accepted: %s", body)
		}
	}
	for _, fault := range []string{"transport", "body", "canceled"} {
		ctx, cancel := context.WithCancel(context.Background())
		provider := &cloudSSOProfileCredentialsProvider{client: credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			if fault == "body" {
				return &http.Response{StatusCode: 200, Body: unreadableCredentialBody{}}, nil
			}
			if fault == "canceled" {
				cancel()
			}
			return nil, errors.New("offline transport failed")
		})}
		profile := map[string]any{"access_token": "access", "cloud_sso_access_token_expire": time.Now().Add(time.Hour).Unix(), "cloud_sso_sign_in_url": "https://signin.example.invalid"}
		_, _, err := provider.fetch(ctx, profile)
		cancel()
		if err == nil || fault == "canceled" && !errors.Is(err, context.Canceled) {
			t.Fatalf("CloudSSO %s failure lost: %v", fault, err)
		}
	}
	if (&oauthProfileCredentialsProvider{}).GetProviderName() != "oauth" || (&cloudSSOProfileCredentialsProvider{}).GetProviderName() != "cloud_sso" {
		t.Fatal("provider identity lost")
	}
}

func TestCredentialProfileTransactionsRejectUnavailableAndMalformedSources(t *testing.T) {
	for _, args := range [][2]string{{"", "name"}, {"path", ""}} {
		if _, err := lockCredentialProfile(nil, args[0], args[1]); err == nil {
			t.Fatal("missing transaction identity accepted")
		}
	}
	var empty *credentialProfileTransaction
	if empty.verifySourceGeneration(context.Background(), "OAuth", "") == nil || (&credentialProfileTransaction{}).verifySourceGeneration(nil, "OAuth", "") == nil {
		t.Fatal("missing transaction verified")
	}
	if providerCredentialsFromSnapshot(nil) != nil {
		t.Fatal("nil snapshot converted")
	}
	for _, raw := range []string{"broken", "null"} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		target, err := configfile.Resolve(path, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := readCredentialConfig(target); err == nil {
			t.Fatalf("malformed credential config accepted: %s", raw)
		}
		transaction := &credentialProfileTransaction{target: target, profileName: "test"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := transaction.verifySourceGeneration(ctx, "OAuth", ""); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
		if err := transaction.verifySourceGeneration(context.Background(), "OAuth", ""); err == nil {
			t.Fatal("malformed source verified")
		}
	}
}
