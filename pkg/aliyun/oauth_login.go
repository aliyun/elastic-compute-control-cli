package aliyun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	oauthCallbackPortStart      = 12345
	oauthCallbackPortEnd        = 12349
	oauthAuthorizationTimeout   = 5 * time.Minute
	oauthCallbackShutdownBudget = 2 * time.Second
)

var ErrOAuthAuthorizationTimeout = errors.New("OAuth authorization timed out")
var ErrOAuthManualAuthorizationRequired = errors.New("manual OAuth authorization requires an explicit terminal flow")

type OAuthLocalError struct {
	Stage string
	Err   error
}

func (e *OAuthLocalError) Error() string {
	if e == nil || e.Err == nil {
		return "local OAuth setup failed"
	}
	return fmt.Sprintf("local OAuth %s failed: %v", e.Stage, e.Err)
}

func (e *OAuthLocalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type OAuthAuthorizationDeniedError struct{ Code string }

func (e *OAuthAuthorizationDeniedError) Error() string {
	code := "authorization_error"
	if e != nil && e.Code != "" {
		code = e.Code
	}
	return "OAuth authorization was denied: " + code
}

type OAuthRemoteError struct {
	Stage      string
	StatusCode int
	Code       string
	Err        error
}

func (e *OAuthRemoteError) Error() string {
	if e == nil {
		return "OAuth service request failed"
	}
	if e.StatusCode > 0 {
		if e.Code != "" {
			return fmt.Sprintf("OAuth %s request returned HTTP %d: %s", e.Stage, e.StatusCode, e.Code)
		}
		return fmt.Sprintf("OAuth %s request returned HTTP %d", e.Stage, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("OAuth %s request failed: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("OAuth %s request failed", e.Stage)
}

func (e *OAuthRemoteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type OAuthLoginOptions struct {
	SiteType           string
	OpenBrowser        func(string) error
	OnAuthorizationURL func(string) error
	SuccessPage        string
	Manual             bool
}

type OAuthLoginResult struct {
	SiteType           string `json:"oauth_site_type"`
	AccessToken        string `json:"-"`
	RefreshToken       string `json:"-"`
	AccessTokenExpire  int64  `json:"-"`
	RefreshTokenExpire int64  `json:"-"`
	AuthorizationURL   string `json:"-"`
	BrowserLaunched    bool   `json:"browser_launch_started"`
}

type oauthLoginDependencies struct {
	listen               func() (net.Listener, error)
	client               credentialHTTPClient
	now                  func() time.Time
	authorizationTimeout time.Duration
}

type oauthCallbackResult struct {
	code string
	err  error
}

type oauthAuthorizationCodeResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`
}

func LoginOAuth(ctx context.Context, options OAuthLoginOptions) (*OAuthLoginResult, error) {
	return loginOAuthWithDependencies(ctx, options, oauthLoginDependencies{
		listen:               listenOAuthCallback,
		client:               newCredentialHTTPClient(credentialHTTPTimeout),
		now:                  time.Now,
		authorizationTimeout: oauthAuthorizationTimeout,
	})
}

func loginOAuthWithDependencies(ctx context.Context, options OAuthLoginOptions, dependencies oauthLoginDependencies) (*OAuthLoginResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	siteType := strings.ToUpper(strings.TrimSpace(options.SiteType))
	endpoint, ok := oauthEndpoints[siteType]
	if !ok {
		return nil, errors.New("OAuth site type must be CN or INTL")
	}
	baseURL, err := validatedOAuthBaseURL(endpoint.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth token endpoint: %w", err)
	}
	signInURL, err := validatedOAuthBaseURL(endpoint.signInURL)
	if err != nil {
		return nil, fmt.Errorf("invalid OAuth sign-in endpoint: %w", err)
	}
	if strings.TrimSpace(endpoint.clientID) == "" {
		return nil, errors.New("OAuth client ID is unavailable")
	}
	if dependencies.listen == nil {
		dependencies.listen = listenOAuthCallback
	}
	if dependencies.client == nil {
		dependencies.client = newCredentialHTTPClient(credentialHTTPTimeout)
	} else {
		dependencies.client = rejectCredentialRedirects(dependencies.client)
	}
	if dependencies.now == nil {
		dependencies.now = time.Now
	}
	if dependencies.authorizationTimeout <= 0 {
		dependencies.authorizationTimeout = oauthAuthorizationTimeout
	}

	listener, err := dependencies.listen()
	if err != nil {
		return nil, &OAuthLocalError{Stage: "callback listener", Err: err}
	}
	defer listener.Close()
	callbackAddress, ok := listener.Addr().(*net.TCPAddr)
	if !ok || callbackAddress.IP == nil || !callbackAddress.IP.IsLoopback() {
		return nil, errors.New("OAuth callback listener must use a loopback address")
	}
	redirectURI := "http://" + listener.Addr().String() + "/cli/callback"

	state, err := randomOAuthValue(32)
	if err != nil {
		return nil, &OAuthLocalError{Stage: "state generation", Err: err}
	}
	codeVerifier, err := randomOAuthValue(64)
	if err != nil {
		return nil, &OAuthLocalError{Stage: "PKCE generation", Err: err}
	}
	challengeDigest := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])

	authorizationURL := *signInURL
	authorizationURL.Path = strings.TrimRight(authorizationURL.Path, "/") + "/oauth2/v1/auth"
	query := authorizationURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", endpoint.clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	authorizationURL.RawQuery = query.Encode()

	callbackCh := make(chan oauthCallbackResult, 1)
	serverErrCh := make(chan error, 1)
	var callbackOnce sync.Once
	callbackHost := listener.Addr().String()
	successPage := options.SuccessPage
	if successPage == "" {
		successPage = "Authorization received. Return to the terminal to finish login."
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/callback", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'")
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if request.Host != callbackHost || !remoteAddressIsLoopback(request.RemoteAddr) {
			http.Error(writer, "Forbidden", http.StatusForbidden)
			return
		}
		returnedState := request.URL.Query().Get("state")
		if subtle.ConstantTimeCompare([]byte(returnedState), []byte(state)) != 1 {
			http.Error(writer, "Invalid state", http.StatusBadRequest)
			return
		}
		if rawError := request.URL.Query().Get("error"); rawError != "" {
			errorCode := standardOAuthErrorCode(rawError)
			if errorCode == "" {
				errorCode = "authorization_error"
			}
			callbackOnce.Do(func() {
				callbackCh <- oauthCallbackResult{err: &OAuthAuthorizationDeniedError{Code: errorCode}}
			})
			http.Error(writer, "Authorization denied", http.StatusBadRequest)
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			http.Error(writer, "Authorization code is missing", http.StatusBadRequest)
			return
		}
		if len(code) > 16*1024 {
			http.Error(writer, "Authorization code is too large", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, successPage)
		callbackOnce.Do(func() {
			callbackCh <- oauthCallbackResult{code: code}
		})
	})
	server := &http.Server{
		Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 5 * time.Second, MaxHeaderBytes: 32 * 1024,
	}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case serverErrCh <- serveErr:
			default:
			}
		}
	}()

	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), oauthCallbackShutdownBudget)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}
	defer shutdown()

	result := &OAuthLoginResult{SiteType: siteType, AuthorizationURL: authorizationURL.String()}
	emitAuthorizationURL := func() error {
		if options.OnAuthorizationURL == nil {
			return ErrOAuthManualAuthorizationRequired
		}
		if err := options.OnAuthorizationURL(result.AuthorizationURL); err != nil {
			return fmt.Errorf("write OAuth authorization URL: %w", err)
		}
		return nil
	}
	if options.Manual {
		if err := emitAuthorizationURL(); err != nil {
			return nil, err
		}
	} else if options.OpenBrowser != nil {
		if err := options.OpenBrowser(result.AuthorizationURL); err == nil {
			result.BrowserLaunched = true
		} else if emitErr := emitAuthorizationURL(); emitErr != nil {
			return nil, errors.Join(&OAuthLocalError{Stage: "browser launch", Err: err}, emitErr)
		}
	} else if err := emitAuthorizationURL(); err != nil {
		return nil, err
	}

	timer := time.NewTimer(dependencies.authorizationTimeout)
	defer timer.Stop()
	var callback oauthCallbackResult
	select {
	case callback = <-callbackCh:
	case serveErr := <-serverErrCh:
		return nil, &OAuthLocalError{Stage: "callback server", Err: serveErr}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrOAuthAuthorizationTimeout
	}
	if callback.err != nil {
		return nil, callback.err
	}

	tokenURL := *baseURL
	tokenURL.Path = strings.TrimRight(tokenURL.Path, "/") + "/v1/token"
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", callback.code)
	form.Set("client_id", endpoint.clientID)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build OAuth token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "ecctl")
	response, err := dependencies.client.Do(request)
	if err != nil {
		return nil, &OAuthRemoteError{Stage: "token", Err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, credentialResponseLimit+1))
	if err != nil {
		return nil, errors.New("OAuth token response is unreadable")
	}
	if len(body) > credentialResponseLimit {
		return nil, errors.New("OAuth token response is too large")
	}
	if response.StatusCode != http.StatusOK {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &payload)
		if errorCode := standardOAuthErrorCode(payload.Error); errorCode != "" {
			return nil, &OAuthRemoteError{Stage: "token", StatusCode: response.StatusCode, Code: errorCode}
		}
		return nil, &OAuthRemoteError{Stage: "token", StatusCode: response.StatusCode}
	}
	var tokenResponse oauthAuthorizationCodeResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, errors.New("OAuth token response is invalid")
	}
	if tokenResponse.AccessToken == "" || tokenResponse.RefreshToken == "" || tokenResponse.ExpiresIn <= 0 {
		return nil, errors.New("OAuth token response is incomplete")
	}
	if tokenResponse.TokenType != "" && !strings.EqualFold(tokenResponse.TokenType, "Bearer") {
		return nil, errors.New("OAuth token response has an unsupported token type")
	}
	now := dependencies.now().Unix()
	if tokenResponse.ExpiresIn > (1<<63-1)-now {
		return nil, errors.New("OAuth access token expiration is invalid")
	}
	result.AccessToken = tokenResponse.AccessToken
	result.RefreshToken = tokenResponse.RefreshToken
	result.AccessTokenExpire = now + tokenResponse.ExpiresIn
	if tokenResponse.RefreshTokenExpiresIn > 0 && tokenResponse.RefreshTokenExpiresIn <= (1<<63-1)-now {
		result.RefreshTokenExpire = now + tokenResponse.RefreshTokenExpiresIn
	}
	return result, nil
}

func listenOAuthCallback() (net.Listener, error) {
	var lastErr error
	for port := oauthCallbackPortStart; port <= oauthCallbackPortEnd; port++ {
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
		if err == nil {
			return listener, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no loopback callback port is available in range %d-%d: %w", oauthCallbackPortStart, oauthCallbackPortEnd, lastErr)
}

func validatedOAuthBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("endpoint must be an absolute HTTPS URL without user information, query, or fragment")
	}
	return parsed, nil
}

func randomOAuthValue(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("random value size must be positive")
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func remoteAddressIsLoopback(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func standardOAuthErrorCode(raw string) string {
	code := strings.TrimSpace(raw)
	switch code {
	case "invalid_request", "unauthorized_client", "access_denied", "unsupported_response_type",
		"invalid_scope", "server_error", "temporarily_unavailable", "invalid_client",
		"invalid_grant", "unsupported_grant_type", "invalid_token":
		return code
	default:
		return ""
	}
}
