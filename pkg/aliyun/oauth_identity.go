package aliyun

import (
	"context"
	"errors"
	"time"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

type oauthLoginCredential struct {
	AccountID    string
	IdentityType string
	Entry        credentialCacheEntry
}

var validateOAuthLoginCredential = exchangeAndValidateOAuthLoginCredential

func exchangeAndValidateOAuthLoginCredential(ctx context.Context, login *OAuthLoginResult) (*oauthLoginCredential, error) {
	if login == nil {
		return nil, errors.New("OAuth login result is unavailable")
	}
	profile := map[string]any{
		"name":                       "oauth-login",
		"mode":                       credentialModeOAuth,
		"oauth_site_type":            login.SiteType,
		"oauth_refresh_token":        login.RefreshToken,
		"oauth_refresh_token_expire": login.RefreshTokenExpire,
		"oauth_access_token":         login.AccessToken,
		"oauth_access_token_expire":  login.AccessTokenExpire,
	}
	credentials, update, err := refreshOAuthCredentialWithHTTP(ctx, profile, newCredentialHTTPClient(credentialHTTPTimeout), nil)
	if err != nil {
		return nil, err
	}
	if credentials == nil || update == nil {
		return nil, errors.New("OAuth credential validation result is incomplete")
	}
	snapshot, err := snapshotFromProviderCredentialsWithExpiration(credentials, time.Unix(update.stsExpire, 0))
	if err != nil {
		return nil, err
	}
	executor, err := newStaticDarabonbaExecutor(resolvedOpenAPIProfile{AuthType: "AK"}, snapshot)
	if err != nil {
		return nil, &OAuthLocalError{Stage: "identity client", Err: err}
	}
	identity, accountID, err := resolveIdentityAt(ctx, executor, "sts.aliyuncs.com")
	if err != nil {
		return nil, &OAuthRemoteError{Stage: "identity", Err: err}
	}
	accountID, err = ecconfig.NormalizeOAuthAccountID(accountID)
	if err != nil {
		return nil, err
	}
	return &oauthLoginCredential{
		AccountID: accountID, IdentityType: identity.Type,
		Entry: credentialCacheEntry{
			Mode:              credentialModeOAuth,
			OAuthRefreshToken: update.refreshToken, OAuthRefreshExpire: login.RefreshTokenExpire,
			OAuthAccessToken: update.accessToken, OAuthAccessExpire: update.accessTokenExpire,
			AccessKeyID: update.accessKeyID, AccessKeySecret: update.accessKeySecret,
			SecurityToken: update.securityToken, STSExpiration: update.stsExpire,
		},
	}, nil
}
