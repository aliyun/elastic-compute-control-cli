package aliyun

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthLoginUsesLoopbackPKCEAndReturnsTokens(t *testing.T) {
	fixedNow := time.Unix(1_700_000_000, 0)
	var authorizationQuery url.Values
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/token" || request.Method != http.MethodPost {
			t.Fatalf("token request = %s %s", request.Method, request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		verifier := request.Form.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		if got, want := authorizationQuery.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(digest[:]); got != want {
			t.Fatalf("PKCE challenge = %q, want %q", got, want)
		}
		if request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("code") != "test-code" || request.Form.Get("redirect_uri") != authorizationQuery.Get("redirect_uri") {
			t.Fatalf("token form = %#v", request.Form)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"refresh_token_expires_in":7200,"token_type":"Bearer"}`))
	}))
	defer tokenServer.Close()
	restore := setOAuthLoginEndpointForTest(t, tokenServer.URL)
	defer restore()

	browserURL := ""
	result, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType: "cn",
		OpenBrowser: func(raw string) error {
			browserURL = raw
			parsed, err := url.Parse(raw)
			if err != nil {
				return err
			}
			authorizationQuery = parsed.Query()
			if parsed.Scheme != "https" || parsed.Host != "signin.example.com" || parsed.Path != "/oauth2/v1/auth" || authorizationQuery.Get("code_challenge_method") != "S256" {
				return fmt.Errorf("authorization URL is invalid: %s", raw)
			}
			redirect := authorizationQuery.Get("redirect_uri")
			go func() {
				invalid, _ := http.Get(redirect + "?code=ignored&state=wrong")
				if invalid != nil {
					_ = invalid.Body.Close()
				}
				valid, _ := http.Get(redirect + "?code=test-code&state=" + url.QueryEscape(authorizationQuery.Get("state")))
				if valid != nil {
					_ = valid.Body.Close()
				}
			}()
			return nil
		},
		OnAuthorizationURL: func(string) error {
			return errors.New("manual authorization URL must not be emitted after browser launch")
		},
		SuccessPage: "done",
	}, oauthLoginDependencies{
		listen:               testOAuthListener,
		client:               tokenServer.Client(),
		now:                  func() time.Time { return fixedNow },
		authorizationTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.BrowserLaunched || browserURL != result.AuthorizationURL {
		t.Fatalf("browser launched=%t url=%q result=%q", result.BrowserLaunched, browserURL, result.AuthorizationURL)
	}
	if result.SiteType != "CN" || result.AccessToken != "access" || result.RefreshToken != "refresh" || result.AccessTokenExpire != fixedNow.Unix()+3600 || result.RefreshTokenExpire != fixedNow.Unix()+7200 {
		t.Fatalf("OAuth result = %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "access") || strings.Contains(string(encoded), "refresh") || strings.Contains(string(encoded), "signin.example.com") {
		t.Fatalf("marshaled OAuth result leaked sensitive state: %s err=%v", encoded, err)
	}
}

func TestOAuthLoginManualModeSkipsBrowser(t *testing.T) {
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	restore := setOAuthLoginEndpointForTest(t, tokenServer.URL)
	defer restore()
	browserCalls := 0
	result, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType: "CN", Manual: true,
		OpenBrowser:        func(string) error { browserCalls++; return nil },
		OnAuthorizationURL: authorizeOAuthCallback,
	}, oauthLoginDependencies{
		listen: testOAuthListener, client: tokenServer.Client(), now: time.Now,
		authorizationTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if browserCalls != 0 || result.BrowserLaunched {
		t.Fatalf("manual browser calls=%d launched=%t", browserCalls, result.BrowserLaunched)
	}
}

func TestOAuthLoginBrowserFailureStillAllowsManualAuthorization(t *testing.T) {
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	restore := setOAuthLoginEndpointForTest(t, tokenServer.URL)
	defer restore()

	result, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType:           "CN",
		OpenBrowser:        func(string) error { return errors.New("browser unavailable") },
		OnAuthorizationURL: authorizeOAuthCallback,
	}, oauthLoginDependencies{
		listen: testOAuthListener, client: tokenServer.Client(), now: time.Now,
		authorizationTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BrowserLaunched {
		t.Fatal("browser failure was reported as opened")
	}
}

func TestOAuthLoginTimeoutAndInvalidInputs(t *testing.T) {
	for _, site := range []string{"", "EU"} {
		if _, err := LoginOAuth(context.Background(), OAuthLoginOptions{SiteType: site}); err == nil {
			t.Fatalf("site %q was accepted", site)
		}
	}

	restore := setOAuthLoginEndpointForTest(t, "https://oauth.example.com")
	defer restore()
	_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType: "CN", OpenBrowser: func(string) error { return nil },
	}, oauthLoginDependencies{
		listen: testOAuthListener, client: http.DefaultClient, now: time.Now,
		authorizationTimeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, ErrOAuthAuthorizationTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
	_, err = loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN"}, oauthLoginDependencies{
		listen: func() (net.Listener, error) { return net.Listen("tcp4", "0.0.0.0:0") },
		client: http.DefaultClient, now: time.Now, authorizationTimeout: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback listener error = %v", err)
	}
}

func TestOAuthLoginDoesNotExposeTokenErrorDescription(t *testing.T) {
	tokenServer := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte(`{"error":"token-secret-value","error_description":"description-secret-value"}`))
	}))
	defer tokenServer.Close()
	restore := setOAuthLoginEndpointForTest(t, tokenServer.URL)
	defer restore()

	_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType: "CN", OnAuthorizationURL: authorizeOAuthCallback,
	}, oauthLoginDependencies{
		listen: testOAuthListener, client: tokenServer.Client(), now: time.Now,
		authorizationTimeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || strings.Contains(err.Error(), "token-secret-value") || strings.Contains(err.Error(), "description-secret-value") {
		t.Fatalf("token error = %v", err)
	}
}

func TestOAuthLoginDoesNotExposeCallbackErrorValue(t *testing.T) {
	restore := setOAuthLoginEndpointForTest(t, "https://oauth.example.com")
	defer restore()
	_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{
		SiteType: "CN",
		OnAuthorizationURL: func(raw string) error {
			parsed, parseErr := url.Parse(raw)
			if parseErr != nil {
				return parseErr
			}
			redirect := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")
			go func() {
				response, _ := http.Get(redirect + "?error=callback-secret-value&state=" + url.QueryEscape(state))
				if response != nil {
					_ = response.Body.Close()
				}
			}()
			return nil
		},
	}, oauthLoginDependencies{
		listen: testOAuthListener, client: http.DefaultClient, now: time.Now,
		authorizationTimeout: 2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "authorization_error") || strings.Contains(err.Error(), "callback-secret-value") {
		t.Fatalf("callback error = %v", err)
	}
}

func TestStandardOAuthErrorCodeAllowlist(t *testing.T) {
	if got := standardOAuthErrorCode("invalid_grant"); got != "invalid_grant" {
		t.Fatalf("standard code = %q", got)
	}
	for _, raw := range []string{"secret-value", "INVALID_GRANT", "invalid grant", ""} {
		if got := standardOAuthErrorCode(raw); got != "" {
			t.Fatalf("untrusted code %q = %q", raw, got)
		}
	}
}

func testOAuthListener() (net.Listener, error) {
	return net.Listen("tcp4", "127.0.0.1:0")
}

func authorizeOAuthCallback(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	redirect := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")
	go func() {
		response, _ := http.Get(redirect + "?code=test-code&state=" + url.QueryEscape(state))
		if response != nil {
			_ = response.Body.Close()
		}
	}()
	return nil
}

func setOAuthLoginEndpointForTest(t *testing.T, baseURL string) func() {
	t.Helper()
	original := oauthEndpoints["CN"]
	oauthEndpoints["CN"] = struct {
		baseURL   string
		signInURL string
		clientID  string
	}{baseURL: baseURL, signInURL: "https://signin.example.com", clientID: "test-client"}
	return func() { oauthEndpoints["CN"] = original }
}
