package aliyun

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthRefreshRejectsMalformedRemoteResponses(t *testing.T) {
	for _, stage := range []string{"refresh", "exchange"} {
		for _, body := range []string{"invalid JSON", `{}`, `{"access_token":"token","refresh_token":"rotated","expires_in":0}`, `{"accessKeyId":"id","accessKeySecret":"secret","securityToken":"token","expiration":"invalid"}`, `{"accessKeyId":"id","accessKeySecret":"secret","securityToken":"token","expiration":"2000-01-01T00:00:00Z"}`} {
			profile := map[string]any{"oauth_site_type": "CN", "oauth_refresh_token": "refresh"}
			if stage == "exchange" {
				profile["oauth_access_token"], profile["oauth_access_token_expire"] = "access", time.Now().Add(time.Hour).Unix()
			}
			client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			if _, _, err := refreshOAuthCredentialWithHTTP(nil, profile, client, nil); err == nil {
				t.Fatalf("%s accepted malformed response: %s", stage, body)
			}
		}
	}
	for _, response := range []*http.Response{nil, {}, {StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", credentialResponseLimit+1)))}} {
		if _, _, err := readCredentialHTTPResponse(response); err == nil {
			t.Fatal("unbounded or unavailable response accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { cancel(); return nil, errors.New("network failure") })
	for _, stage := range []string{"refresh", "exchange"} {
		profile := map[string]any{"oauth_site_type": "CN", "oauth_refresh_token": "refresh"}
		if stage == "exchange" {
			profile["oauth_access_token"], profile["oauth_access_token_expire"] = "access", time.Now().Add(time.Hour).Unix()
		}
		if _, _, err := refreshOAuthCredentialWithHTTP(ctx, profile, client, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost at %s: %v", stage, err)
		}
	}
}

func TestOAuthLoginRejectsInvalidTokenPayloads(t *testing.T) {
	for _, body := range []string{"invalid", `{}`, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"unknown"}`, `{"access_token":"access","refresh_token":"refresh","expires_in":9223372036854775807}`, strings.Repeat("x", credentialResponseLimit+1)} {
		client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN", Manual: true, OnAuthorizationURL: authorizeOAuthCallback}, oauthLoginDependencies{listen: testOAuthListener, client: client, now: func() time.Time { return time.Unix(1700000000, 0) }, authorizationTimeout: time.Second})
		if err == nil {
			t.Fatal("invalid OAuth token payload accepted")
		}
	}
	cause := errors.New("client unavailable")
	client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, cause })
	_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN", Manual: true, OnAuthorizationURL: authorizeOAuthCallback}, oauthLoginDependencies{listen: testOAuthListener, client: client, authorizationTimeout: time.Second})
	if !errors.Is(err, cause) {
		t.Fatalf("client cause lost: %v", err)
	}
}

func TestOAuthCallbackRejectsInvalidRequestsBeforeAcceptingCode(t *testing.T) {
	client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))}, nil
	})
	_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN", Manual: true, OnAuthorizationURL: func(raw string) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		query := parsed.Query()
		redirect := query.Get("redirect_uri")
		for _, tt := range []struct {
			method, host, code, state string
			status                    int
		}{
			{http.MethodPost, "", "test", query.Get("state"), http.StatusMethodNotAllowed},
			{http.MethodGet, "attacker.example", "test", query.Get("state"), http.StatusForbidden},
			{http.MethodGet, "", "test", "wrong", http.StatusBadRequest},
			{http.MethodGet, "", "", query.Get("state"), http.StatusBadRequest},
			{http.MethodGet, "", strings.Repeat("x", 16*1024+1), query.Get("state"), http.StatusBadRequest},
		} {
			request, err := http.NewRequest(tt.method, redirect+"?code="+url.QueryEscape(tt.code)+"&state="+url.QueryEscape(tt.state), nil)
			if err != nil {
				return err
			}
			if tt.host != "" {
				request.Host = tt.host
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return err
			}
			response.Body.Close()
			if response.StatusCode != tt.status {
				t.Errorf("callback request %s %s got %d, want %d", tt.method, tt.host, response.StatusCode, tt.status)
			}
		}
		return authorizeOAuthCallback(raw)
	}}, oauthLoginDependencies{listen: testOAuthListener, client: client, authorizationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOAuthListenerAndSetupFailures(t *testing.T) {
	listener, err := listenOAuthCallback()
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	cause := errors.New("listener unavailable")
	_, err = loginOAuthWithDependencies(nil, OAuthLoginOptions{SiteType: "CN"}, oauthLoginDependencies{listen: func() (net.Listener, error) { return nil, cause }})
	if !errors.Is(err, cause) {
		t.Fatalf("listener cause lost: %v", err)
	}
	_, err = loginOAuthWithDependencies(nil, OAuthLoginOptions{SiteType: "CN", Manual: true}, oauthLoginDependencies{listen: testOAuthListener})
	if !errors.Is(err, ErrOAuthManualAuthorizationRequired) {
		t.Fatalf("manual flow without output: %v", err)
	}
	if _, err := randomOAuthValue(0); err == nil {
		t.Fatal("zero-size state accepted")
	}
	if _, err := exchangeAndValidateOAuthLoginCredential(nil, nil); err == nil {
		t.Fatal("missing OAuth result accepted")
	}
}

func TestOAuthLoginFailsWhenAuthorizationURLCannotBeEmitted(t *testing.T) {
	cause := errors.New("authorization output unavailable")
	for _, browser := range []func(string) error{nil, func(string) error { return cause }} {
		_, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN", OpenBrowser: browser, OnAuthorizationURL: func(string) error { return cause }}, oauthLoginDependencies{listen: testOAuthListener})
		if !errors.Is(err, cause) {
			t.Fatalf("authorization output cause lost: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := loginOAuthWithDependencies(ctx, OAuthLoginOptions{SiteType: "CN", Manual: true, OnAuthorizationURL: func(string) error { cancel(); return nil }}, oauthLoginDependencies{listen: testOAuthListener})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("authorization cancellation lost: %v", err)
	}
	for _, body := range []string{`{"error":"access_denied"}`, "unreadable"} {
		client := credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			if body == "unreadable" {
				return &http.Response{StatusCode: 200, Body: unreadableCredentialBody{}}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		if _, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN", Manual: true, OnAuthorizationURL: authorizeOAuthCallback}, oauthLoginDependencies{listen: testOAuthListener, client: client}); err == nil {
			t.Fatal("failed token response accepted")
		}
	}
}

func TestOAuthLoginRejectsInvalidEndpointConfigurationBeforeListening(t *testing.T) {
	original := oauthEndpoints["CN"]
	t.Cleanup(func() { oauthEndpoints["CN"] = original })
	for _, fault := range []string{"token", "signin", "client"} {
		endpoint := original
		switch fault {
		case "token":
			endpoint.baseURL = "http://unsafe.example"
		case "signin":
			endpoint.signInURL = "https://user@unsafe.example"
		case "client":
			endpoint.clientID = " "
		}
		oauthEndpoints["CN"] = endpoint
		if _, err := loginOAuthWithDependencies(context.Background(), OAuthLoginOptions{SiteType: "CN"}, oauthLoginDependencies{listen: func() (net.Listener, error) { t.Fatal("invalid OAuth config opened listener"); return nil, nil }}); err == nil {
			t.Fatalf("invalid %s config accepted", fault)
		}
	}
	oauthEndpoints["CN"] = original
	if remoteAddressIsLoopback("invalid%host") {
		t.Fatal("invalid callback host accepted")
	}
}
