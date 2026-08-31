package aliyun

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

type OAuthConfigureErrorKind string

const (
	OAuthConfigureInvalid              OAuthConfigureErrorKind = "invalid"
	OAuthConfigureDenied               OAuthConfigureErrorKind = "denied"
	OAuthConfigureTimeout              OAuthConfigureErrorKind = "timeout"
	OAuthConfigureCanceled             OAuthConfigureErrorKind = "canceled"
	OAuthConfigureService              OAuthConfigureErrorKind = "service"
	OAuthConfigureLocal                OAuthConfigureErrorKind = "local"
	OAuthConfigureManual               OAuthConfigureErrorKind = "manual_required"
	OAuthConfigurePersistence          OAuthConfigureErrorKind = "persistence"
	OAuthConfigurePersistenceUncertain OAuthConfigureErrorKind = "persistence_uncertain"
	OAuthConfigureProfileChanged       OAuthConfigureErrorKind = "profile_changed"
	OAuthConfigureAccountMismatch      OAuthConfigureErrorKind = "account_mismatch"
	OAuthConfigureConfirmation         OAuthConfigureErrorKind = "confirmation_required"
)

var ErrOAuthAccountConfirmationRequired = errors.New("OAuth account confirmation is required")

type OAuthAccountMismatchError struct {
	Expected string
	Actual   string
}

func (e *OAuthAccountMismatchError) Error() string {
	return "OAuth login account does not match the expected account"
}

type OAuthConfigureError struct {
	Kind      OAuthConfigureErrorKind
	Retryable bool
	Err       error
}

func (e *OAuthConfigureError) Error() string {
	if e == nil || e.Err == nil {
		return "OAuth configuration failed"
	}
	return e.Err.Error()
}

func (e *OAuthConfigureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type OAuthConfigureOptions struct {
	ProfileName        string
	SiteType           string
	ConfigPath         string
	AliyunConfigPath   string
	OpenBrowser        func(string) error
	OnAuthorizationURL func(string) error
	SuccessPage        string
	Manual             bool
	ExpectedAccountID  string
	ConfirmAccount     func(accountID, identityType string) error
}

type OAuthConfigureResult struct {
	ProfileName     string
	SiteType        string
	ConfigPath      string
	AccountID       string
	BrowserLaunched bool
}

var (
	performOAuthLogin        = LoginOAuth
	nativeOAuthCacheRootPath = credentialCacheRootPath
)

func ConfigureOAuthProfile(ctx context.Context, options OAuthConfigureOptions) (*OAuthConfigureResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	store, err := ecconfig.LoadNativeOAuthStore(options.ConfigPath)
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, err)
	}
	aliyunConfigPath := options.AliyunConfigPath
	if strings.TrimSpace(aliyunConfigPath) == "" {
		aliyunConfigPath = ecconfig.AliyunConfigPath(os.Getenv)
	}
	sameTarget, err := configfile.SameTarget(store.ResolvedPath(), aliyunConfigPath)
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, err)
	}
	if sameTarget {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, errors.New("native OAuth config path must not be the Aliyun CLI config path"))
	}
	if err := store.PreflightNativeOAuthWrite(ctx); err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	transactionConfigPath, err := nativeOAuthTransactionConfigPath(store)
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, err)
	}
	state := store.NativeOAuthProfileState(options.ProfileName)
	name, err := ecconfig.NormalizeOAuthProfileName(state.Name)
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, err)
	}
	if name != state.Name {
		state = store.NativeOAuthProfileState(name)
	}
	state.Name = name
	siteType := strings.ToUpper(strings.TrimSpace(options.SiteType))
	if siteType == "" {
		siteType = state.SiteType
	}
	if siteType == "" {
		siteType = "CN"
	}
	if siteType != "CN" && siteType != "INTL" {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, errors.New("OAuth site type must be CN or INTL"))
	}
	expectedAccountID := strings.TrimSpace(options.ExpectedAccountID)
	if expectedAccountID != "" {
		expectedAccountID, err = ecconfig.NormalizeOAuthAccountID(expectedAccountID)
		if err != nil {
			return nil, oauthConfigureError(OAuthConfigureInvalid, false, err)
		}
	}
	generation, err := randomOAuthValue(32)
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigureInvalid, false, fmt.Errorf("generate OAuth login generation: %w", err))
	}
	cacheRoot, err := nativeOAuthCacheRootPath()
	if err != nil {
		return nil, oauthConfigureError(OAuthConfigurePersistence, false, err)
	}
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, name)
	if err := recoverNativeOAuthTransactionWithProfileLock(ctx, cachePath, name); err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	_, _, err = loadAnyCredentialCacheEntry(ctx, cachePath)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	prepared, err := beginCredentialCacheWrite(ctx, cachePath)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	defer prepared.Abort()
	preparedTransaction, err := beginNativeOAuthTransactionWrite(cachePath)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	defer preparedTransaction.Abort()

	login, err := performOAuthLogin(ctx, OAuthLoginOptions{
		SiteType: siteType, OpenBrowser: options.OpenBrowser,
		OnAuthorizationURL: options.OnAuthorizationURL, SuccessPage: options.SuccessPage,
		Manual: options.Manual,
	})
	if err != nil {
		return nil, classifyOAuthLoginError(err)
	}
	validated, err := validateOAuthLoginCredential(ctx, login)
	if err != nil {
		return nil, classifyOAuthLoginError(err)
	}
	accountExpectation := expectedAccountID
	if accountExpectation == "" {
		accountExpectation = state.AccountID
	}
	if accountExpectation != "" && validated.AccountID != accountExpectation {
		return nil, oauthConfigureError(OAuthConfigureAccountMismatch, false, &OAuthAccountMismatchError{Expected: accountExpectation, Actual: validated.AccountID})
	}
	if accountExpectation == "" {
		if options.ConfirmAccount == nil {
			return nil, oauthConfigureError(OAuthConfigureConfirmation, false, ErrOAuthAccountConfirmationRequired)
		}
		if err := options.ConfirmAccount(validated.AccountID, validated.IdentityType); err != nil {
			return nil, oauthConfigureError(OAuthConfigureConfirmation, false, err)
		}
	}
	nativeProfile := nativeOAuthProfileMap(name, siteType, generation, validated.AccountID)
	sourceGeneration := credentialSourceGeneration(nativeProfile, credentialModeOAuth)
	entry := validated.Entry
	entry.Mode = credentialModeOAuth
	entry.SourceGeneration = sourceGeneration

	profileLock, err := acquireCredentialFileLockWithTimeout(ctx, credentialProfileLockPath(cachePath, name), credentialRefreshLockTimeout)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	defer profileLock.Unlock()
	if err := recoverNativeOAuthTransaction(ctx, cachePath, name); err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	latestStore, err := ecconfig.LoadNativeOAuthStore(options.ConfigPath)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	latestState := latestStore.NativeOAuthProfileState(name)
	if latestStore.ResolvedPath() != store.ResolvedPath() || latestState.ConfigExisted != state.ConfigExisted || latestState.Exists != state.Exists || latestState.AuthGeneration != state.AuthGeneration {
		return nil, oauthConfigureError(OAuthConfigureProfileChanged, false, ecconfig.ErrCredentialProfileChanged)
	}
	previous, hadPrevious, err := loadAnyCredentialCacheEntry(ctx, cachePath)
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	record := nativeOAuthTransactionRecord{
		ProfileName: name, ConfigPath: transactionConfigPath, ResolvedConfigPath: store.ResolvedPath(),
		OldConfigExisted: state.ConfigExisted, OldProfileExists: state.Exists, OldLoginGeneration: state.Generation,
		OldAuthGeneration:  hex.EncodeToString(state.AuthGeneration[:]),
		NewLoginGeneration: generation, NewAuthGeneration: nativeOAuthAuthGeneration(nativeProfile),
		HadPrevious: hadPrevious, Previous: previous, Next: entry,
	}
	persistenceCtx, cancelPersistence := credentialPersistenceContext(ctx)
	err = commitPreparedNativeOAuthTransaction(persistenceCtx, preparedTransaction, record)
	cancelPersistence()
	if err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	persistenceCtx, cancelPersistence = credentialPersistenceContext(ctx)
	if hadPrevious {
		err = commitPreparedCredentialCacheEntryIfGeneration(persistenceCtx, cachePath, previous.SourceGeneration, prepared, entry)
	} else {
		err = commitPreparedCredentialCacheEntryIfMissing(persistenceCtx, cachePath, prepared, entry)
	}
	cancelPersistence()
	if err != nil {
		recoverErr := recoverNativeOAuthTransactionWithProfileLockHeld(ctx, cachePath, name)
		return nil, classifyOAuthPersistenceError(errors.Join(err, recoverErr))
	}
	if err := store.SetNativeOAuthProfile(state, siteType, generation, validated.AccountID); err != nil {
		recoverErr := recoverNativeOAuthTransactionWithProfileLockHeld(ctx, cachePath, name)
		return nil, classifyOAuthPersistenceError(errors.Join(err, recoverErr))
	}
	if err := store.SaveContext(ctx); err != nil {
		err = nativeOAuthConfigSaveFailure(err, func() error {
			return recoverNativeOAuthTransactionWithProfileLockHeld(ctx, cachePath, name)
		})
		return nil, classifyOAuthPersistenceError(err)
	}
	if err := recoverNativeOAuthTransactionWithProfileLockHeld(ctx, cachePath, name); err != nil {
		return nil, classifyOAuthPersistenceError(err)
	}
	return &OAuthConfigureResult{
		ProfileName: name, SiteType: siteType, ConfigPath: options.ConfigPath, AccountID: validated.AccountID,
		BrowserLaunched: login.BrowserLaunched,
	}, nil
}

func nativeOAuthTransactionConfigPath(store *ecconfig.Store) (string, error) {
	if store == nil || store.RequestedPath() == "" {
		return "", errors.New("native OAuth configuration path is unavailable")
	}
	path, err := filepath.Abs(store.RequestedPath())
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

func nativeOAuthConfigSaveFailure(saveErr error, recoverFn func() error) error {
	if saveErr == nil || configfile.ReplacementApplied(saveErr) || recoverFn == nil {
		return saveErr
	}
	return errors.Join(saveErr, recoverFn())
}

func recoverNativeOAuthTransactionWithProfileLock(ctx context.Context, cachePath, profileName string) error {
	if err := configfile.PreparePrivateDirectory(filepath.Dir(cachePath)); err != nil {
		return err
	}
	profileLock, err := acquireCredentialFileLockWithTimeout(ctx, credentialProfileLockPath(cachePath, profileName), credentialRefreshLockTimeout)
	if err != nil {
		return err
	}
	defer profileLock.Unlock()
	return recoverNativeOAuthTransactionWithProfileLockHeld(ctx, cachePath, profileName)
}

func recoverNativeOAuthTransactionWithProfileLockHeld(ctx context.Context, cachePath, profileName string) error {
	persistenceCtx, cancel := credentialPersistenceContext(ctx)
	defer cancel()
	return recoverNativeOAuthTransaction(persistenceCtx, cachePath, profileName)
}

func nativeOAuthProfileMap(name, siteType, generation, accountID string) map[string]any {
	return map[string]any{
		"name": name, "mode": credentialModeOAuth,
		"oauth_site_type": siteType, "oauth_generation": generation,
		"oauth_account_id": accountID,
	}
}

func classifyOAuthPersistenceError(err error) error {
	if configfile.ReplacementApplied(err) {
		return oauthConfigureError(OAuthConfigurePersistenceUncertain, false, err)
	}
	if errors.Is(err, context.Canceled) {
		return oauthConfigureError(OAuthConfigureCanceled, false, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return oauthConfigureError(OAuthConfigureTimeout, false, err)
	}
	if errors.Is(err, ecconfig.ErrCredentialProfileChanged) || errors.Is(err, ErrCredentialProfileChanged) {
		return oauthConfigureError(OAuthConfigureProfileChanged, false, err)
	}
	return oauthConfigureError(OAuthConfigurePersistence, false, err)
}

func classifyOAuthLoginError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return oauthConfigureError(OAuthConfigureCanceled, false, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrOAuthAuthorizationTimeout):
		return oauthConfigureError(OAuthConfigureTimeout, false, err)
	case errors.Is(err, ErrOAuthAccountConfirmationRequired):
		return oauthConfigureError(OAuthConfigureConfirmation, false, err)
	case errors.Is(err, ErrOAuthManualAuthorizationRequired):
		return oauthConfigureError(OAuthConfigureManual, false, err)
	}
	var local *OAuthLocalError
	if errors.As(err, &local) {
		return oauthConfigureError(OAuthConfigureLocal, false, err)
	}
	var denied *OAuthAuthorizationDeniedError
	if errors.As(err, &denied) {
		switch denied.Code {
		case "access_denied":
			return oauthConfigureError(OAuthConfigureDenied, false, err)
		case "server_error", "temporarily_unavailable":
			return oauthConfigureError(OAuthConfigureService, true, err)
		default:
			return oauthConfigureError(OAuthConfigureInvalid, false, err)
		}
	}
	var remote *OAuthRemoteError
	if errors.As(err, &remote) {
		retryable := remote.StatusCode == 0 || remote.StatusCode == 429 || remote.StatusCode >= 500
		if !retryable {
			return oauthConfigureError(OAuthConfigureDenied, false, err)
		}
		return oauthConfigureError(OAuthConfigureService, retryable, err)
	}
	return oauthConfigureError(OAuthConfigureService, true, err)
}

func oauthConfigureError(kind OAuthConfigureErrorKind, retryable bool, err error) error {
	return &OAuthConfigureError{Kind: kind, Retryable: retryable, Err: err}
}
