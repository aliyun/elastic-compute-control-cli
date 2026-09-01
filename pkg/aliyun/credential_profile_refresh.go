package aliyun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
	"github.com/gofrs/flock"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

var ErrCredentialProfileChanged = errors.New("credential profile changed during refresh")
var ErrCredentialStatePersistenceFailed = errors.New("credential state could not be persisted")

type OAuthReauthenticationError struct{ Reason string }

func (e *OAuthReauthenticationError) Error() string {
	if e == nil || e.Reason == "" {
		return "OAuth authentication must be renewed"
	}
	return e.Reason
}

type credentialCacheWriteError struct{ err error }

func (e *credentialCacheWriteError) Error() string {
	if e == nil || e.err == nil {
		return "credential cache write failed"
	}
	return e.err.Error()
}

func (e *credentialCacheWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

const (
	credentialProfileLockTimeout = 2 * time.Second
	credentialProfileLockRetry   = 25 * time.Millisecond
	credentialResponseLimit      = 1 << 20
	oauthPersistenceTimeout      = 2 * time.Second
	credentialHTTPTimeout        = 15 * time.Second
	credentialRefreshLockGrace   = 5 * time.Second
	credentialRefreshLockTimeout = 2*credentialHTTPTimeout + oauthPersistenceTimeout + credentialRefreshLockGrace
)

var oauthEndpoints = map[string]struct {
	baseURL   string
	signInURL string
	clientID  string
}{
	"CN":   {baseURL: "https://oauth.aliyun.com", signInURL: "https://signin.aliyun.com", clientID: "4038181954557748008"},
	"INTL": {baseURL: "https://oauth.alibabacloud.com", signInURL: "https://signin.alibabacloud.com", clientID: "4103531455503354461"},
}

func cachedProfileCredentials(profile map[string]any) (*credentialproviders.Credentials, time.Time) {
	expiration := time.Unix(int64MapField(profile, "sts_expiration"), 0)
	if !expiration.After(time.Now()) {
		return nil, time.Time{}
	}
	credentials := &credentialproviders.Credentials{
		AccessKeyId:     stringMapField(profile, "access_key_id"),
		AccessKeySecret: stringMapField(profile, "access_key_secret"),
		SecurityToken:   stringMapField(profile, "sts_token"),
	}
	if credentials.AccessKeyId == "" || credentials.AccessKeySecret == "" || credentials.SecurityToken == "" {
		return nil, time.Time{}
	}
	return credentials, expiration
}

type oauthCredentialProfileUpdate struct {
	refreshToken      string
	accessToken       string
	accessKeyID       string
	accessKeySecret   string
	securityToken     string
	accessTokenExpire int64
	stsExpire         int64
}

type oauthProfileCredentialsProvider struct {
	name       string
	configPath string
	cachePath  string
	client     credentialHTTPClient
	generation string
	siteType   string
	native     bool
	cacheEntry credentialCacheEntry

	gate      contextGate
	cached    *credentialproviders.Credentials
	expiresAt time.Time
}

func (*oauthProfileCredentialsProvider) Renewable() bool { return true }

type oauthRefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type oauthCredentialResponse struct {
	AccessKeyID     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	SecurityToken   string `json:"securityToken"`
	Expiration      string `json:"expiration"`
}

type oauthTokenCommitFunc func(*oauthCredentialProfileUpdate) error
type oauthRefreshFunc func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error)

var refreshOAuthCredential oauthRefreshFunc = refreshOAuthCredentialWithHTTP

func refreshOAuthCredentialWithHTTP(ctx context.Context, profile map[string]any, client credentialHTTPClient, commitToken oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
	endpoint, ok := oauthEndpoints[strings.ToUpper(stringMapField(profile, "oauth_site_type"))]
	if !ok {
		return nil, nil, errors.New("OAuth site type must be CN or INTL")
	}
	parsed, err := url.Parse(endpoint.baseURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, nil, errors.New("OAuth endpoint must be an absolute HTTPS URL without user information")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = newCredentialHTTPClient(credentialHTTPTimeout)
	}
	refreshToken := stringMapField(profile, "oauth_refresh_token")
	refreshTokenExpire := int64MapField(profile, "oauth_refresh_token_expire")
	accessToken := stringMapField(profile, "oauth_access_token")
	accessTokenExpire := int64MapField(profile, "oauth_access_token_expire")
	tokenRefreshed := false
	if refreshToken != "" && refreshTokenExpire > 0 && refreshTokenExpire <= time.Now().Unix() {
		return nil, nil, &OAuthReauthenticationError{Reason: "OAuth refresh token is expired"}
	}
	if refreshToken != "" && (accessToken == "" || accessTokenExpire == 0 || accessTokenExpire-time.Now().Unix() <= 1200) {
		form := url.Values{
			"grant_type":    []string{"refresh_token"},
			"refresh_token": []string{refreshToken},
			"client_id":     []string{endpoint.clientID},
			"Timestamp":     []string{time.Now().UTC().Format("2006-01-02T15:04:05Z")},
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.baseURL+"/v1/token", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, nil, errors.New("OAuth token refresh request is invalid")
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, nil, &OAuthRemoteError{Stage: "refresh", Err: err}
		}
		body, status, err := readCredentialHTTPResponse(response)
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK {
			return nil, nil, oauthRemoteResponseError("refresh", status, body)
		}
		var payload oauthRefreshTokenResponse
		if err := json.Unmarshal(body, &payload); err != nil || payload.AccessToken == "" || payload.RefreshToken == "" || payload.ExpiresIn <= 0 {
			return nil, nil, errors.New("OAuth token refresh response is invalid")
		}
		accessToken = payload.AccessToken
		refreshToken = payload.RefreshToken
		accessTokenExpire = time.Now().Unix() + payload.ExpiresIn
		tokenRefreshed = true
	}
	if accessToken == "" {
		return nil, nil, &OAuthReauthenticationError{Reason: "OAuth access token is unavailable"}
	}
	if tokenRefreshed && commitToken != nil {
		if err := commitToken(&oauthCredentialProfileUpdate{
			refreshToken: refreshToken, accessToken: accessToken, accessTokenExpire: accessTokenExpire,
		}); err != nil {
			return nil, nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.baseURL+"/v1/exchange", http.NoBody)
	if err != nil {
		return nil, nil, errors.New("OAuth credential exchange request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, &OAuthRemoteError{Stage: "exchange", Err: err}
	}
	body, status, err := readCredentialHTTPResponse(response)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return nil, nil, oauthRemoteResponseError("exchange", status, body)
	}
	var payload oauthCredentialResponse
	if err := json.Unmarshal(body, &payload); err != nil || payload.AccessKeyID == "" || payload.AccessKeySecret == "" || payload.SecurityToken == "" {
		return nil, nil, errors.New("OAuth credential exchange response is invalid")
	}
	expiration, err := time.Parse(time.RFC3339, payload.Expiration)
	if err != nil || !expiration.After(time.Now()) {
		return nil, nil, errors.New("OAuth credential exchange expiration is invalid")
	}
	credentials := &credentialproviders.Credentials{
		AccessKeyId: payload.AccessKeyID, AccessKeySecret: payload.AccessKeySecret,
		SecurityToken: payload.SecurityToken, ProviderName: "oauth",
	}
	return credentials, &oauthCredentialProfileUpdate{
		refreshToken: refreshToken, accessToken: accessToken,
		accessKeyID: payload.AccessKeyID, accessKeySecret: payload.AccessKeySecret, securityToken: payload.SecurityToken,
		accessTokenExpire: accessTokenExpire, stsExpire: expiration.Unix(),
	}, nil
}

func readCredentialHTTPResponse(response *http.Response) ([]byte, int, error) {
	if response == nil || response.Body == nil {
		return nil, 0, errors.New("credential response is unavailable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, credentialResponseLimit+1))
	if err != nil {
		return nil, response.StatusCode, errors.New("credential response is unreadable")
	}
	if len(body) > credentialResponseLimit {
		return nil, response.StatusCode, errors.New("credential response is too large")
	}
	return body, response.StatusCode, nil
}

func oauthRemoteResponseError(stage string, status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	return &OAuthRemoteError{Stage: stage, StatusCode: status, Code: standardOAuthErrorCode(payload.Error)}
}

func newOAuthProfileCredentialsProvider(profile map[string]any, name, configPath string, cachePaths ...string) (*oauthProfileCredentialsProvider, error) {
	return newOAuthProfileCredentialsProviderWithClient(profile, name, configPath, nil, cachePaths...)
}

func newNativeOAuthProfileCredentialsProvider(profile map[string]any, name, configPath string, cachePaths ...string) (*oauthProfileCredentialsProvider, error) {
	return newOAuthProfileCredentialsProviderWithCacheSource(profile, name, configPath, nativeOAuthCacheSource, true, nil, cachePaths...)
}

func newOAuthProfileCredentialsProviderWithClient(profile map[string]any, name, configPath string, client credentialHTTPClient, cachePaths ...string) (*oauthProfileCredentialsProvider, error) {
	return newOAuthProfileCredentialsProviderWithCacheSource(profile, name, configPath, configPath, false, client, cachePaths...)
}

func newOAuthProfileCredentialsProviderWithCacheSource(profile map[string]any, name, configPath, cacheSource string, native bool, client credentialHTTPClient, cachePaths ...string) (*oauthProfileCredentialsProvider, error) {
	siteType := strings.ToUpper(stringMapField(profile, "oauth_site_type"))
	if _, ok := oauthEndpoints[siteType]; !ok {
		return nil, errors.New("OAuth site type must be CN or INTL")
	}
	if client == nil {
		client = newCredentialHTTPClient(credentialHTTPTimeout)
	} else {
		client = rejectCredentialRedirects(client)
	}
	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	configPath = filepath.Clean(absoluteConfigPath)
	cacheRoot := ""
	if len(cachePaths) > 0 && cachePaths[0] != "" {
		cacheRoot = cachePaths[0]
	} else {
		var rootErr error
		cacheRoot, rootErr = credentialCacheRootPath()
		if rootErr != nil {
			return nil, rootErr
		}
	}
	generation := credentialSourceGeneration(profile, credentialModeOAuth)
	cachePath := credentialCacheEntryPath(cacheRoot, cacheSource, name)
	if native {
		if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, name); err != nil {
			return nil, err
		}
	}
	provider := &oauthProfileCredentialsProvider{
		name: name, configPath: configPath, cachePath: cachePath, client: client,
		generation: generation, siteType: siteType, native: native,
	}
	if entry, match, cacheErr := loadCredentialCacheEntryState(context.Background(), cachePath, credentialModeOAuth, generation); cacheErr != nil {
		return nil, credentialCacheError("read", cacheErr)
	} else if match == credentialCacheMismatched && native {
		return nil, ErrCredentialProfileChanged
	} else if match == credentialCacheMatching {
		provider.cacheEntry = entry
		profile = cacheEntryProfile(profile, entry)
	}
	provider.cached, provider.expiresAt = cachedProfileCredentials(profile)
	return provider, nil
}

func (p *oauthProfileCredentialsProvider) GetProviderName() string { return "oauth" }

func (p *oauthProfileCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	snapshot, err := p.Acquire(context.Background())
	if err != nil {
		return nil, err
	}
	return providerCredentialsFromSnapshot(snapshot), nil
}

func (p *oauthProfileCredentialsProvider) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if err := p.gate.Lock(ctx); err != nil {
		return nil, err
	}
	defer p.gate.Unlock()
	if p.cached != nil && time.Until(p.expiresAt) > credentialRefreshWindow {
		return snapshotFromProviderCredentialsWithExpiration(p.cached, p.expiresAt)
	}
	transaction, err := lockCredentialProfile(ctx, p.configPath, p.name, p.cachePath)
	if err != nil {
		return nil, fmt.Errorf("persist refreshed OAuth credential: %w", err)
	}
	defer transaction.close()
	if p.native {
		if err := recoverNativeOAuthTransactionWithProfileLockHeld(ctx, p.cachePath, p.name); err != nil {
			return nil, err
		}
	}
	if credentialSourceGeneration(transaction.profile, credentialModeOAuth) != p.generation {
		return nil, ErrCredentialProfileChanged
	}
	if entry, match, cacheErr := loadCredentialCacheEntryState(ctx, p.cachePath, credentialModeOAuth, p.generation); cacheErr != nil {
		return nil, credentialCacheError("read", cacheErr)
	} else if match == credentialCacheMismatched && p.native {
		p.cacheEntry = credentialCacheEntry{}
		p.cached, p.expiresAt = nil, time.Time{}
		return nil, ErrCredentialProfileChanged
	} else if match == credentialCacheMissing && p.native {
		p.cacheEntry = credentialCacheEntry{}
		p.cached, p.expiresAt = nil, time.Time{}
		return nil, p.withRecovery(&OAuthReauthenticationError{Reason: "native OAuth credential cache is unavailable"})
	} else if match == credentialCacheMatching {
		p.cacheEntry = entry
	}
	effectiveProfile := cacheEntryProfile(transaction.profile, p.cacheEntry)
	if cached, expiration := cachedProfileCredentials(effectiveProfile); cached != nil && time.Until(expiration) > credentialRefreshWindow {
		p.cached, p.expiresAt = cached, expiration
		return snapshotFromProviderCredentialsWithExpiration(cached, expiration)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	preflightCtx, cancelPreflight := credentialPersistenceContext(ctx)
	prepared, prepareErr := beginCredentialCacheWrite(preflightCtx, p.cachePath)
	if prepareErr != nil {
		cancelPreflight()
		return nil, fmt.Errorf("%w: %v", ErrCredentialStatePersistenceFailed, credentialCacheError("prepare", prepareErr))
	}
	cancelPreflight()
	defer prepared.Abort()
	credentials, update, err := refreshOAuthCredential(ctx, effectiveProfile, p.client, func(tokenUpdate *oauthCredentialProfileUpdate) error {
		persistenceCtx, cancelPersistence := credentialPersistenceContext(ctx)
		defer cancelPersistence()
		entry := p.cacheEntry
		entry.Mode = credentialModeOAuth
		entry.SourceGeneration = p.generation
		entry.OAuthRefreshToken = tokenUpdate.refreshToken
		entry.OAuthAccessToken = tokenUpdate.accessToken
		entry.OAuthAccessExpire = tokenUpdate.accessTokenExpire
		if err := contextError(persistenceCtx); err != nil {
			return err
		}
		var err error
		if p.native {
			err = commitPreparedCredentialCacheEntryIfGeneration(persistenceCtx, p.cachePath, p.generation, prepared, entry)
		} else {
			err = commitPreparedCredentialCacheEntry(prepared, entry)
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCredentialStatePersistenceFailed, credentialCacheError("write OAuth token state", err))
		}
		p.cacheEntry = entry
		// Persist the one-time remote result before checking for an external
		// profile rewrite. A changed source still fails closed below, and the
		// generation-bound entry cannot be consumed by the new profile.
		if err := transaction.verifySourceGeneration(persistenceCtx, credentialModeOAuth, p.generation); err != nil {
			return fmt.Errorf("verify refreshed OAuth credential source: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, p.withRecovery(err)
	}
	expiration := time.Unix(update.stsExpire, 0)
	if credentials == nil || credentials.AccessKeyId == "" || credentials.AccessKeySecret == "" || credentials.SecurityToken == "" || !expiration.After(time.Now()) {
		return nil, errors.New("OAuth refresh returned incomplete or expired STS credentials")
	}
	persistenceCtx, cancelPersistence := credentialPersistenceContext(ctx)
	defer cancelPersistence()
	if err := transaction.verifySourceGeneration(persistenceCtx, credentialModeOAuth, p.generation); err != nil {
		return nil, fmt.Errorf("verify refreshed OAuth credential source: %w", err)
	}
	entry := credentialCacheEntry{
		Mode: credentialModeOAuth, SourceGeneration: p.generation,
		OAuthRefreshToken: update.refreshToken, OAuthAccessToken: update.accessToken,
		OAuthRefreshExpire: p.cacheEntry.OAuthRefreshExpire, OAuthAccessExpire: update.accessTokenExpire, AccessKeyID: update.accessKeyID,
		AccessKeySecret: update.accessKeySecret, SecurityToken: update.securityToken,
		STSExpiration: update.stsExpire,
	}
	var storeErr error
	if p.native {
		storeErr = storeCredentialCacheEntryIfGeneration(persistenceCtx, p.cachePath, p.generation, entry)
	} else {
		storeErr = storeCredentialCacheEntry(persistenceCtx, p.cachePath, entry)
	}
	if storeErr != nil {
		return nil, fmt.Errorf("persist refreshed OAuth credential: %w", credentialCacheError("write", storeErr))
	}
	p.cacheEntry = entry
	p.cached, p.expiresAt = credentials, expiration
	return snapshotFromProviderCredentialsWithExpiration(credentials, expiration)
}

func (p *oauthProfileCredentialsProvider) withRecovery(err error) error {
	if err == nil {
		return nil
	}
	var reauthentication *OAuthReauthenticationError
	var remote *OAuthRemoteError
	requiresRecovery := errors.As(err, &reauthentication)
	if errors.As(err, &remote) {
		requiresRecovery = requiresRecovery || remote.Code == "invalid_grant" || remote.Code == "invalid_token" || remote.StatusCode == http.StatusUnauthorized || remote.StatusCode == http.StatusForbidden
	}
	if !requiresRecovery {
		return err
	}
	binary := "aliyun"
	if p.native {
		binary = "ecctl"
	}
	command := []string{binary, "configure", "--mode", "OAuth", "--profile", p.name}
	if p.siteType != "" {
		command = append(command, "--oauth-site-type", p.siteType)
	}
	if p.configPath != "" {
		command = append(command, "--config-path", p.configPath)
	}
	return &credentialRecoveryError{err: err, command: command}
}

func credentialPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, oauthPersistenceTimeout)
}

func snapshotFromProviderCredentialsWithExpiration(credentials *credentialproviders.Credentials, expiration time.Time) (*credentialSnapshot, error) {
	snapshot, err := snapshotFromProviderCredentials(credentials)
	if err != nil {
		return nil, err
	}
	snapshot.ExpiresAt = expiration
	return snapshot, nil
}

type cloudSSOCredentialResponse struct {
	CloudCredential *struct {
		AccessKeyID     string `json:"AccessKeyId"`
		AccessKeySecret string `json:"AccessKeySecret"`
		SecurityToken   string `json:"SecurityToken"`
		Expiration      string `json:"Expiration"`
	} `json:"CloudCredential"`
}

type cloudSSOProfileCredentialsProvider struct {
	name              string
	configPath        string
	cachePath         string
	client            credentialHTTPClient
	generation        string
	expectedAccountID string
	verifyAccount     func(context.Context, *credentialSnapshot, string) error
	identityPolicy    credentialIdentityPolicy
	cacheEntry        credentialCacheEntry
	verifiedIdentity  string
	verifiedProof     *credentialIdentityProof

	gate      contextGate
	cached    *credentialproviders.Credentials
	expiresAt time.Time
}

func (*cloudSSOProfileCredentialsProvider) Renewable() bool { return true }

func newCloudSSOProfileCredentialsProvider(profile map[string]any, name, configPath string, client credentialHTTPClient, cachePaths ...string) (*cloudSSOProfileCredentialsProvider, error) {
	return newCloudSSOProfileCredentialsProviderWithVerifier(profile, name, configPath, client, nil, cachePaths...)
}

func newCloudSSOProfileCredentialsProviderWithVerifier(profile map[string]any, name, configPath string, client credentialHTTPClient, verifier func(context.Context, *credentialSnapshot, string) error, cachePaths ...string) (*cloudSSOProfileCredentialsProvider, error) {
	signInURL := stringMapField(profile, "cloud_sso_sign_in_url")
	parsed, err := url.Parse(signInURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("CloudSSO sign-in URL must be an absolute HTTPS URL without user information")
	}
	if stringMapField(profile, "cloud_sso_account_id") == "" || stringMapField(profile, "cloud_sso_access_config") == "" {
		return nil, errors.New("CloudSSO account ID and access configuration are required")
	}
	if client == nil {
		client = newCredentialHTTPClient(credentialHTTPTimeout)
	} else {
		client = rejectCredentialRedirects(client)
	}
	cacheRoot := ""
	if len(cachePaths) > 0 && cachePaths[0] != "" {
		cacheRoot = cachePaths[0]
	} else {
		var rootErr error
		cacheRoot, rootErr = credentialCacheRootPath()
		if rootErr != nil {
			return nil, rootErr
		}
	}
	generation := credentialSourceGeneration(profile, credentialModeCloudSSO)
	cachePath := credentialCacheEntryPath(cacheRoot, configPath, name)
	provider := &cloudSSOProfileCredentialsProvider{
		name: name, configPath: configPath, cachePath: cachePath, client: client, generation: generation,
		expectedAccountID: stringMapField(profile, "cloud_sso_account_id"), verifyAccount: verifier,
		identityPolicy: identityPolicyFromProfile(profile, nil),
	}
	if entry, ok, cacheErr := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeCloudSSO, generation); cacheErr != nil {
		return nil, credentialCacheError("read", cacheErr)
	} else if ok {
		provider.cacheEntry = entry
		profile = cacheEntryProfile(profile, entry)
	}
	provider.cached, provider.expiresAt = cachedProfileCredentials(profile)
	return provider, nil
}

func (p *cloudSSOProfileCredentialsProvider) GetProviderName() string { return "cloud_sso" }

func (p *cloudSSOProfileCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	snapshot, err := p.Acquire(context.Background())
	if err != nil {
		return nil, err
	}
	return providerCredentialsFromSnapshot(snapshot), nil
}

func (p *cloudSSOProfileCredentialsProvider) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	if err := p.gate.Lock(ctx); err != nil {
		return nil, err
	}
	defer p.gate.Unlock()
	if p.cached != nil && time.Until(p.expiresAt) > credentialRefreshWindow {
		return p.verifiedSnapshot(ctx, p.cached, p.expiresAt)
	}
	transaction, err := lockCredentialProfile(ctx, p.configPath, p.name, p.cachePath)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: err}
	}
	defer transaction.close()
	if credentialSourceGeneration(transaction.profile, credentialModeCloudSSO) != p.generation {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: ErrCredentialProfileChanged}
	}
	if entry, ok, cacheErr := loadCredentialCacheEntry(ctx, p.cachePath, credentialModeCloudSSO, p.generation); cacheErr != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: credentialCacheError("read", cacheErr)}
	} else if ok {
		p.cacheEntry = entry
	}
	profile := cacheEntryProfile(transaction.profile, p.cacheEntry)
	if cached, expiration := cachedProfileCredentials(profile); cached != nil && time.Until(expiration) > credentialRefreshWindow {
		p.cached, p.expiresAt = cached, expiration
		return p.verifiedSnapshot(ctx, cached, expiration)
	}
	credentials, expiration, err := p.fetch(ctx, profile)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: err}
	}
	snapshot, err := p.verifiedSnapshot(ctx, credentials, expiration)
	if err != nil {
		return nil, err
	}
	if verifyErr := transaction.verifySourceGeneration(ctx, credentialModeCloudSSO, p.generation); verifyErr != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: verifyErr}
	}
	entry := credentialCacheEntry{
		Mode: credentialModeCloudSSO, SourceGeneration: p.generation,
		AccessKeyID: credentials.AccessKeyId, AccessKeySecret: credentials.AccessKeySecret,
		SecurityToken: credentials.SecurityToken, STSExpiration: expiration.Unix(),
	}
	persistErr := storeCredentialCacheEntry(ctx, p.cachePath, entry)
	if persistErr != nil {
		persistErr = &credentialCacheWriteError{err: credentialCacheError("write", persistErr)}
	}
	if persistErr != nil {
		var cacheWriteErr *credentialCacheWriteError
		if !errors.As(persistErr, &cacheWriteErr) {
			return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: persistErr}
		}
		if session := telemetry.FromContext(ctx); session != nil {
			session.RecordCredentialOutcome("cloudsso_cache_persist_failed")
		}
	} else {
		p.cacheEntry = entry
	}
	p.cached, p.expiresAt = credentials, expiration
	return snapshot, nil
}

func (p *cloudSSOProfileCredentialsProvider) verifiedSnapshot(ctx context.Context, credentials *credentialproviders.Credentials, expiration time.Time) (*credentialSnapshot, error) {
	snapshot, err := snapshotFromProviderCredentialsWithExpiration(credentials, expiration)
	if err != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: err}
	}
	fingerprint := credentialSnapshotFingerprint(snapshot)
	if fingerprint != "" && fingerprint == p.verifiedIdentity {
		snapshot.IdentityProof = p.verifiedProof
		return snapshot, nil
	}
	var verifyErr error
	if p.verifyAccount != nil {
		verifyErr = p.verifyAccount(ctx, snapshot, p.expectedAccountID)
	} else {
		endpoint, err := p.identityPolicy.endpoint(credentialOperationRegion(ctx))
		if err != nil {
			return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: err}
		}
		verifyErr = verifyCredentialAccountAt(ctx, snapshot, p.expectedAccountID, endpoint)
	}
	if verifyErr != nil {
		return nil, &credentialProviderError{mode: credentialModeCloudSSO, err: verifyErr}
	}
	p.verifiedIdentity = fingerprint
	p.verifiedProof = snapshot.IdentityProof
	return snapshot, nil
}

func (p *cloudSSOProfileCredentialsProvider) fetch(ctx context.Context, profile map[string]any) (*credentialproviders.Credentials, time.Time, error) {
	accessToken := stringMapField(profile, "access_token")
	if accessToken == "" || int64MapField(profile, "cloud_sso_access_token_expire") <= time.Now().Unix() {
		return nil, time.Time{}, errors.New("CloudSSO access token is expired, please re-login with cli")
	}
	parsed, _ := url.Parse(stringMapField(profile, "cloud_sso_sign_in_url"))
	endpoint := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/cloud-credentials"}).String()
	body, err := json.Marshal(map[string]string{
		"AccountId":             stringMapField(profile, "cloud_sso_account_id"),
		"AccessConfigurationId": stringMapField(profile, "cloud_sso_access_config"),
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, time.Time{}, errors.New("CloudSSO credential request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, time.Time{}, ctx.Err()
		}
		return nil, time.Time{}, errors.New("CloudSSO credential request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, time.Time{}, fmt.Errorf("CloudSSO access token requires re-login (HTTP %d)", response.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, credentialResponseLimit))
	if err != nil {
		return nil, time.Time{}, errors.New("CloudSSO credential response is unreadable")
	}
	if response.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("CloudSSO credential request returned HTTP %d", response.StatusCode)
	}
	var payload cloudSSOCredentialResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil || payload.CloudCredential == nil {
		return nil, time.Time{}, errors.New("CloudSSO credential response is invalid")
	}
	cloudCredential := payload.CloudCredential
	expiration, err := time.Parse(time.RFC3339, cloudCredential.Expiration)
	if err != nil || !expiration.After(time.Now()) {
		return nil, time.Time{}, errors.New("CloudSSO credential response has invalid expiration")
	}
	credentials := &credentialproviders.Credentials{
		AccessKeyId: cloudCredential.AccessKeyID, AccessKeySecret: cloudCredential.AccessKeySecret,
		SecurityToken: cloudCredential.SecurityToken, ProviderName: "cloud_sso",
	}
	if credentials.AccessKeyId == "" || credentials.AccessKeySecret == "" || credentials.SecurityToken == "" {
		return nil, time.Time{}, errors.New("CloudSSO credential response is incomplete")
	}
	return credentials, expiration, nil
}

type credentialProfileTransaction struct {
	target      *configfile.Target
	profileName string
	profile     map[string]any
	profileLock *flock.Flock
}

func lockCredentialProfile(ctx context.Context, configPath, profileName string, lockPaths ...string) (*credentialProfileTransaction, error) {
	if configPath == "" || profileName == "" {
		return nil, errors.New("credential profile path and name are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := configfile.Resolve(configPath, false)
	if err != nil {
		return nil, err
	}
	lockBase := target.Path()
	if len(lockPaths) > 0 && lockPaths[0] != "" {
		if err := configfile.PreparePrivateDirectory(filepath.Dir(lockPaths[0])); err != nil {
			return nil, err
		}
		lockTarget, lockErr := configfile.Resolve(lockPaths[0], false)
		if lockErr != nil {
			return nil, lockErr
		}
		lockBase = lockTarget.Path()
	}
	profileLock, err := acquireCredentialFileLockWithTimeout(ctx, credentialProfileLockPath(lockBase, profileName), credentialRefreshLockTimeout)
	if err != nil {
		return nil, err
	}
	transaction, err := readCredentialProfileTransaction(ctx, target, profileName, profileLock)
	if err != nil {
		_ = profileLock.Unlock()
		return nil, err
	}
	return transaction, nil
}

func credentialProfileLockPath(base, profileName string) string {
	profileDigest := sha256.Sum256([]byte(profileName))
	return base + ".profile-" + fmt.Sprintf("%x", profileDigest[:8]) + ".lock"
}

func acquireCredentialFileLockWithTimeout(ctx context.Context, path string, timeout time.Duration) (*flock.Flock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lock := flock.New(path)
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	locked, err := lock.TryLockContext(lockCtx, credentialProfileLockRetry)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("timed out acquiring credential profile lock")
	}
	return lock, nil
}

func readCredentialProfileTransaction(ctx context.Context, target *configfile.Target, profileName string, profileLock *flock.Flock) (*credentialProfileTransaction, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := target.Verify(); err != nil {
		if errors.Is(err, configfile.ErrTargetReplaced) {
			return nil, fmt.Errorf("%w: target was replaced", ErrCredentialProfileChanged)
		}
		return nil, err
	}
	config, _, err := readCredentialConfig(target)
	if err != nil {
		return nil, err
	}
	profile, ok := configProfile(config, profileName)
	if !ok {
		return nil, fmt.Errorf("credential profile %s no longer exists", profileName)
	}
	transaction := &credentialProfileTransaction{
		target: target, profileName: profileName,
		profile: profile, profileLock: profileLock,
	}
	if err := target.Verify(); errors.Is(err, configfile.ErrTargetReplaced) {
		return nil, fmt.Errorf("%w: target was replaced", ErrCredentialProfileChanged)
	} else if err != nil {
		return nil, err
	}
	return transaction, nil
}

func readCredentialConfig(target *configfile.Target) (map[string]any, os.FileInfo, error) {
	raw, info, err := target.Read()
	if err != nil {
		return nil, nil, err
	}
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return nil, nil, err
	}
	if config == nil {
		return nil, nil, errors.New("credential configuration is empty")
	}
	return config, info, nil
}

var credentialProfileIdentityKeys = []string{
	"mode", "ram_role_name", "ram_role_arn", "ram_session_name", "source_profile", "expired_seconds", "policy", "external_id",
	"sts_endpoint", "sts_region", "enable_vpc", "oidc_provider_arn", "oidc_token_file",
	"oauth_site_type", "oauth_generation", "oauth_account_id", "cloud_sso_sign_in_url", "cloud_sso_account_id", "cloud_sso_access_config",
	"process_command", "credentials_uri", "bearer_token_header_key",
}

func credentialProfileAuthDigest(profile map[string]any) [sha256.Size]byte {
	return ecconfig.CredentialProfileAuthDigest(profile)
}

func credentialProfileIdentityDigest(profile map[string]any) [sha256.Size]byte {
	identity := make(map[string]any, len(credentialProfileIdentityKeys)+1)
	identity["name"] = stringMapField(profile, "name")
	for _, key := range credentialProfileIdentityKeys {
		if value, ok := profile[key]; ok {
			identity[key] = value
		}
	}
	raw, _ := json.Marshal(identity)
	return sha256.Sum256(raw)
}

func (t *credentialProfileTransaction) close() {
	if t != nil && t.profileLock != nil {
		_ = t.profileLock.Unlock()
	}
}

func (t *credentialProfileTransaction) verifySourceGeneration(ctx context.Context, mode, expected string) error {
	if t == nil || t.target == nil {
		return errors.New("credential profile transaction is unavailable")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := t.target.Verify(); err != nil {
		if errors.Is(err, configfile.ErrTargetReplaced) {
			return fmt.Errorf("%w: target was replaced", ErrCredentialProfileChanged)
		}
		return err
	}
	config, _, err := readCredentialConfig(t.target)
	if err != nil {
		return err
	}
	profile, ok := configProfile(config, t.profileName)
	if !ok || credentialSourceGeneration(profile, mode) != expected {
		return ErrCredentialProfileChanged
	}
	if err := t.target.Verify(); errors.Is(err, configfile.ErrTargetReplaced) {
		return fmt.Errorf("%w: target was replaced", ErrCredentialProfileChanged)
	} else if err != nil {
		return err
	}
	return nil
}

func cloneStringAnyMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func providerCredentialsFromSnapshot(snapshot *credentialSnapshot) *credentialproviders.Credentials {
	if snapshot == nil {
		return nil
	}
	return &credentialproviders.Credentials{
		AccessKeyId: snapshot.AccessKeyID, AccessKeySecret: snapshot.AccessKeySecret,
		SecurityToken: snapshot.SecurityToken, ProviderName: snapshot.ProviderName,
	}
}
