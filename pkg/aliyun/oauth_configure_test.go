package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

func TestConfigureOAuthProfileStoresTokensOnlyInNativePrivateCache(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	raw := `{"current":"production","profiles":[{"name":"production","mode":"AK","access_key_id":"old-id","access_key_secret":"old-secret","region_id":"cn-hangzhou"}]}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(dir, "credentials-v2")
	restore := stubNativeOAuthConfigure(t, cacheRoot, func(_ context.Context, options OAuthLoginOptions) (*OAuthLoginResult, error) {
		if options.SiteType != "INTL" {
			t.Fatalf("site type = %q", options.SiteType)
		}
		return &OAuthLoginResult{
			SiteType: "INTL", AccessToken: "access-secret", RefreshToken: "refresh-secret",
			AccessTokenExpire: 4102444800, BrowserLaunched: true,
		}, nil
	})
	defer restore()
	result, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{
		ProfileName: "production", SiteType: "INTL",
		ConfigPath: configPath, AliyunConfigPath: aliyunPath,
		ConfirmAccount: acceptOAuthAccount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProfileName != "production" || result.SiteType != "INTL" || result.AccountID != "1234567890123456" || !result.BrowserLaunched {
		t.Fatalf("result = %#v", result)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, forbidden := range []string{"access-secret", "refresh-secret", "old-secret", "access_key_id"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("metadata config contains %q: %s", forbidden, text)
		}
	}
	store, err := ecconfig.LoadNativeOAuthStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	profile := nativeOAuthProfileMap(state.Name, state.SiteType, state.Generation, state.AccountID)
	generation := credentialSourceGeneration(profile, credentialModeOAuth)
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "production")
	entry, ok, err := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeOAuth, generation)
	if err != nil || !ok || entry.OAuthRefreshToken != "refresh-secret" || entry.OAuthAccessToken != "access-secret" {
		t.Fatalf("cache entry = %#v ok=%t err=%v", entry, ok, err)
	}
	if err := configfile.ValidatePrivateFile(cachePath); err != nil {
		t.Fatalf("cache is not private: %v", err)
	}
	resolved, err := resolveOpenAPIProfile("production", configPath, explicitRegion("cn-hangzhou"), mapGetenv(map[string]string{
		"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath,
	}))
	if err != nil || resolved.Mode != credentialModeOAuth || resolved.ExpectedAccountID != "1234567890123456" {
		t.Fatalf("resolved native OAuth profile = %#v err=%v", resolved, err)
	}
	provider, ok := resolved.Acquirer.(*oauthProfileCredentialsProvider)
	if !ok || provider.cacheEntry.OAuthRefreshToken != "refresh-secret" {
		t.Fatalf("resolved provider = %#v", resolved.Acquirer)
	}
}

func TestConfigureOAuthProfileRejectsAliyunConfigAsNativeTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loginCalls := 0
	restore := stubNativeOAuthConfigure(t, filepath.Join(t.TempDir(), "cache"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		loginCalls++
		return nil, errors.New("must not run")
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: path})
	if err == nil || loginCalls != 0 {
		t.Fatalf("same config target error=%v loginCalls=%d", err, loginCalls)
	}
}

func TestConfigureOAuthProfileDefaultsAliyunConfigGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliyun.json")
	if err := os.WriteFile(path, []byte(`{"current":"default","profiles":[{"name":"default","mode":"AK","access_key_id":"id","access_key_secret":"secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", path)
	loginCalls := 0
	restore := stubNativeOAuthConfigure(t, filepath.Join(t.TempDir(), "cache"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		loginCalls++
		return nil, errors.New("must not run")
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{ConfigPath: path})
	if err == nil || loginCalls != 0 {
		t.Fatalf("default Aliyun guard error=%v loginCalls=%d", err, loginCalls)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "access_key_secret") {
		t.Fatalf("Aliyun config was modified: %s", raw)
	}
}

func TestNativeOAuthTransactionConfigPathIsAbsoluteForRelativeInput(t *testing.T) {
	dir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.WriteFile("config.json", []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := ecconfig.LoadNativeOAuthStore("config.json")
	if err != nil {
		t.Fatal(err)
	}
	path, err := nativeOAuthTransactionConfigPath(store)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "config.json" {
		t.Fatalf("transaction config path = %q", path)
	}
}

func TestConfigureOAuthProfileRejectsAliasedAliyunConfigTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	alias := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(path, []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(path), alias); err != nil {
		t.Fatal(err)
	}
	loginCalls := 0
	restore := stubNativeOAuthConfigure(t, filepath.Join(dir, "cache"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		loginCalls++
		return nil, errors.New("must not run")
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: alias})
	if err == nil || loginCalls != 0 {
		t.Fatalf("aliased config target error=%v loginCalls=%d", err, loginCalls)
	}
}

func TestConfigureOAuthProfileRollsBackCacheOnConcurrentAuthChange(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ecctl.json")
	if err := os.WriteFile(configPath, []byte(`{"current":"production","profiles":[{"name":"production","mode":"AK","access_key_id":"old","access_key_secret":"old-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(dir, "cache")
	restore := stubNativeOAuthConfigure(t, cacheRoot, func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		concurrent := `{"current":"production","profiles":[{"name":"production","mode":"AK","access_key_id":"new","access_key_secret":"new-secret"}]}`
		if err := os.WriteFile(configPath, []byte(concurrent), 0o600); err != nil {
			t.Fatal(err)
		}
		return &OAuthLoginResult{SiteType: "CN", AccessToken: "access", RefreshToken: "refresh", AccessTokenExpire: 4102444800}, nil
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{ProfileName: "production", ConfigPath: configPath, AliyunConfigPath: filepath.Join(dir, "aliyun.json"), ConfirmAccount: acceptOAuthAccount})
	var configureErr *OAuthConfigureError
	if !errors.As(err, &configureErr) || configureErr.Kind != OAuthConfigureProfileChanged {
		t.Fatalf("concurrent auth error = %v", err)
	}
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "production")
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan cache remained: %v", err)
	}
	updated, _ := os.ReadFile(configPath)
	if !strings.Contains(string(updated), "new-secret") {
		t.Fatalf("concurrent config changed: %s", updated)
	}
}

func TestConfigureOAuthProfileClassifiesServiceAndDeniedErrors(t *testing.T) {
	tests := []struct {
		name      string
		loginErr  error
		kind      OAuthConfigureErrorKind
		retryable bool
	}{
		{name: "service", loginErr: &OAuthRemoteError{Stage: "token", StatusCode: 503}, kind: OAuthConfigureService, retryable: true},
		{name: "denied", loginErr: &OAuthAuthorizationDeniedError{Code: "access_denied"}, kind: OAuthConfigureDenied},
		{name: "callback service", loginErr: &OAuthAuthorizationDeniedError{Code: "server_error"}, kind: OAuthConfigureService, retryable: true},
		{name: "invalid grant", loginErr: &OAuthRemoteError{Stage: "token", StatusCode: 400, Code: "invalid_grant"}, kind: OAuthConfigureDenied},
		{name: "local callback", loginErr: &OAuthLocalError{Stage: "callback listener", Err: errors.New("ports unavailable")}, kind: OAuthConfigureLocal},
		{name: "manual required", loginErr: ErrOAuthManualAuthorizationRequired, kind: OAuthConfigureManual},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			restore := stubNativeOAuthConfigure(t, filepath.Join(dir, "cache"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
				return nil, test.loginErr
			})
			defer restore()
			_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: filepath.Join(dir, "aliyun.json")})
			var configureErr *OAuthConfigureError
			if !errors.As(err, &configureErr) || configureErr.Kind != test.kind || configureErr.Retryable != test.retryable {
				t.Fatalf("classified error = %#v raw=%v", configureErr, err)
			}
		})
	}
}

func TestConfigureOAuthProfileClassifiesPostCommitPersistenceUncertainty(t *testing.T) {
	postCommit := &configfile.PostCommitError{Err: errors.New("directory sync failed")}
	err := classifyOAuthPersistenceError(errors.Join(ErrCredentialProfileChanged, postCommit))
	var configureErr *OAuthConfigureError
	if !errors.As(err, &configureErr) || configureErr.Kind != OAuthConfigurePersistenceUncertain {
		t.Fatalf("post-commit classification = %#v raw=%v", configureErr, err)
	}
}

func TestNativeOAuthConfigPostCommitFailureRetainsRecoveryJournal(t *testing.T) {
	postCommit := &configfile.PostCommitError{Err: errors.New("config directory sync failed")}
	recoveryCalls := 0
	err := nativeOAuthConfigSaveFailure(postCommit, func() error {
		recoveryCalls++
		return nil
	})
	if !configfile.ReplacementApplied(err) || recoveryCalls != 0 {
		t.Fatalf("post-commit error=%v recoveryCalls=%d", err, recoveryCalls)
	}
	ordinary := errors.New("write failed before rename")
	err = nativeOAuthConfigSaveFailure(ordinary, func() error {
		recoveryCalls++
		return errors.New("recovered")
	})
	if !errors.Is(err, ordinary) || recoveryCalls != 1 {
		t.Fatalf("ordinary error=%v recoveryCalls=%d", err, recoveryCalls)
	}
}

func TestConfigureOAuthProfileHonorsCancellationBeforePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"current":"default","profiles":[{"name":"default"}]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	restore := stubNativeOAuthConfigure(t, filepath.Join(dir, "cache"), func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		cancel()
		return &OAuthLoginResult{SiteType: "CN", AccessToken: "access", RefreshToken: "refresh", AccessTokenExpire: 4102444800}, nil
	})
	defer restore()
	_, err := ConfigureOAuthProfile(ctx, OAuthConfigureOptions{ConfigPath: path, AliyunConfigPath: filepath.Join(dir, "aliyun.json"), ConfirmAccount: acceptOAuthAccount})
	var configureErr *OAuthConfigureError
	if !errors.As(err, &configureErr) || configureErr.Kind != OAuthConfigureCanceled {
		t.Fatalf("canceled configure error = %v", err)
	}
	updated, _ := os.ReadFile(path)
	if string(updated) != string(original) {
		t.Fatalf("canceled configure changed metadata: %s", updated)
	}
}

func TestRecoverNativeOAuthTransactionChoosesCompleteOldOrNewPair(t *testing.T) {
	tests := []struct {
		name             string
		configGeneration string
		activeGeneration string
		wantGeneration   string
	}{
		{name: "restore previous when config is old", configGeneration: "login-old", activeGeneration: "source-new", wantGeneration: "source-old"},
		{name: "install next when config is new", configGeneration: "login-new", activeGeneration: "source-old", wantGeneration: "source-new"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalCacheSync := syncNativeOAuthCacheReplacement
			cacheSyncCalls := 0
			syncNativeOAuthCacheReplacement = func(path string) error {
				cacheSyncCalls++
				return originalCacheSync(path)
			}
			t.Cleanup(func() { syncNativeOAuthCacheReplacement = originalCacheSync })

			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			cachePath := filepath.Join(dir, "credentials-v2", "native.json")
			oldProfile := nativeOAuthProfileMap("production", "CN", "login-old", "1111111111111111")
			newProfile := nativeOAuthProfileMap("production", "CN", "login-new", "2222222222222222")
			if test.configGeneration == "login-old" {
				writeNativeOAuthConfigForTest(t, configPath, oldProfile)
			} else {
				writeNativeOAuthConfigForTest(t, configPath, newProfile)
			}
			configStore, err := ecconfig.LoadNativeOAuthStore(configPath)
			if err != nil {
				t.Fatal(err)
			}
			previous := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-old", OAuthRefreshToken: "refresh-old"}
			next := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-new", OAuthRefreshToken: "refresh-new"}
			active := previous
			if test.activeGeneration == "source-new" {
				active = next
			}
			if err := storeCredentialCacheEntry(context.Background(), cachePath, active); err != nil {
				t.Fatal(err)
			}
			prepared, err := beginNativeOAuthTransactionWrite(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Abort()
			record := nativeOAuthTransactionRecord{
				ProfileName: "production", ConfigPath: configPath, ResolvedConfigPath: configStore.ResolvedPath(),
				OldConfigExisted: true, OldProfileExists: true, OldLoginGeneration: "login-old", OldAuthGeneration: nativeOAuthAuthGeneration(oldProfile),
				NewLoginGeneration: "login-new", NewAuthGeneration: nativeOAuthAuthGeneration(newProfile),
				HadPrevious: true, Previous: previous, Next: next,
			}
			if err := commitPreparedNativeOAuthTransaction(context.Background(), prepared, record); err != nil {
				t.Fatal(err)
			}
			if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production"); err != nil {
				t.Fatal(err)
			}
			entry, found, err := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeOAuth, test.wantGeneration)
			if err != nil || !found {
				t.Fatalf("recovered entry = %#v found=%t err=%v", entry, found, err)
			}
			if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("transaction journal remained: %v", err)
			}
			if cacheSyncCalls == 0 {
				t.Fatal("recovery removed the journal without confirming cache durability")
			}
		})
	}
}

func TestRecoverNativeOAuthTransactionRetainsJournalUntilConfigParentSync(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cachePath := filepath.Join(dir, "credentials-v2", "native.json")
	oldProfile := nativeOAuthProfileMap("production", "CN", "login-old", "1111111111111111")
	newProfile := nativeOAuthProfileMap("production", "CN", "login-new", "2222222222222222")
	writeNativeOAuthConfigForTest(t, configPath, newProfile)
	configStore, err := ecconfig.LoadNativeOAuthStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-old", OAuthRefreshToken: "refresh-old"}
	next := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-new", OAuthRefreshToken: "refresh-new"}
	if err := storeCredentialCacheEntry(context.Background(), cachePath, next); err != nil {
		t.Fatal(err)
	}
	prepared, err := beginNativeOAuthTransactionWrite(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	record := nativeOAuthTransactionRecord{
		ProfileName: "production", ConfigPath: configPath, ResolvedConfigPath: configStore.ResolvedPath(),
		OldConfigExisted: true, OldProfileExists: true, OldLoginGeneration: "login-old", OldAuthGeneration: nativeOAuthAuthGeneration(oldProfile),
		NewLoginGeneration: "login-new", NewAuthGeneration: nativeOAuthAuthGeneration(newProfile),
		HadPrevious: true, Previous: previous, Next: next,
	}
	if err := commitPreparedNativeOAuthTransaction(context.Background(), prepared, record); err != nil {
		t.Fatal(err)
	}
	originalSync := syncNativeOAuthConfigReplacement
	syncNativeOAuthConfigReplacement = func(string) error { return errors.New("config replacement sync failed") }
	t.Cleanup(func() { syncNativeOAuthConfigReplacement = originalSync })
	err = recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production")
	if !configfile.ReplacementApplied(err) {
		t.Fatalf("config sync recovery error = %v", err)
	}
	if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); err != nil {
		t.Fatalf("journal was removed after config sync failure: %v", err)
	}
	if _, found, err := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeOAuth, next.SourceGeneration); err != nil || !found {
		t.Fatalf("next cache changed before config durability: found=%t err=%v", found, err)
	}
	syncNativeOAuthConfigReplacement = func(string) error {
		writeNativeOAuthConfigForTest(t, configPath, oldProfile)
		return nil
	}
	if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production"); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("config changed after sync error = %v", err)
	}
	if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); err != nil {
		t.Fatalf("journal was removed after post-sync config change: %v", err)
	}
	writeNativeOAuthConfigForTest(t, configPath, newProfile)
	syncNativeOAuthConfigReplacement = originalSync
	if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := loadCredentialCacheEntry(context.Background(), cachePath, credentialModeOAuth, next.SourceGeneration); err != nil || !found {
		t.Fatalf("next cache was not installed after config durability: found=%t err=%v", found, err)
	}
	if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal remained after durable recovery: %v", err)
	}
}

func TestRecoverNativeOAuthTransactionRetainsJournalUntilCacheReplacementSync(t *testing.T) {
	tests := []struct {
		name              string
		configGeneration  string
		desiredGeneration string
	}{
		{name: "install next", configGeneration: "login-new", desiredGeneration: "source-new"},
		{name: "restore previous", configGeneration: "login-old", desiredGeneration: "source-old"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			cachePath := filepath.Join(dir, "credentials-v2", "native.json")
			oldProfile := nativeOAuthProfileMap("production", "CN", "login-old", "1111111111111111")
			newProfile := nativeOAuthProfileMap("production", "CN", "login-new", "2222222222222222")
			if test.configGeneration == "login-new" {
				writeNativeOAuthConfigForTest(t, configPath, newProfile)
			} else {
				writeNativeOAuthConfigForTest(t, configPath, oldProfile)
			}
			configStore, err := ecconfig.LoadNativeOAuthStore(configPath)
			if err != nil {
				t.Fatal(err)
			}
			previous := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-old", OAuthRefreshToken: "refresh-old"}
			next := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "source-new", OAuthRefreshToken: "refresh-new"}
			desired, other := previous, next
			if test.desiredGeneration == next.SourceGeneration {
				desired, other = next, previous
			}
			if err := storeCredentialCacheEntry(context.Background(), cachePath, desired); err != nil {
				t.Fatal(err)
			}
			prepared, err := beginNativeOAuthTransactionWrite(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Abort()
			record := nativeOAuthTransactionRecord{
				ProfileName: "production", ConfigPath: configPath, ResolvedConfigPath: configStore.ResolvedPath(),
				OldConfigExisted: true, OldProfileExists: true, OldLoginGeneration: "login-old", OldAuthGeneration: nativeOAuthAuthGeneration(oldProfile),
				NewLoginGeneration: "login-new", NewAuthGeneration: nativeOAuthAuthGeneration(newProfile),
				HadPrevious: true, Previous: previous, Next: next,
			}
			if err := commitPreparedNativeOAuthTransaction(context.Background(), prepared, record); err != nil {
				t.Fatal(err)
			}

			originalCacheSync := syncNativeOAuthCacheReplacement
			syncNativeOAuthCacheReplacement = func(string) error {
				return errors.New("cache replacement sync failed")
			}
			t.Cleanup(func() { syncNativeOAuthCacheReplacement = originalCacheSync })
			err = recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production")
			if !configfile.ReplacementApplied(err) {
				t.Fatalf("cache sync recovery error = %v", err)
			}
			if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); err != nil {
				t.Fatalf("journal was removed after cache sync failure: %v", err)
			}

			syncNativeOAuthCacheReplacement = func(string) error {
				return storeCredentialCacheEntry(context.Background(), cachePath, other)
			}
			if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production"); !errors.Is(err, ErrCredentialProfileChanged) {
				t.Fatalf("cache changed after sync barrier = %v", err)
			}
			if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); err != nil {
				t.Fatalf("journal was removed after post-sync cache change: %v", err)
			}

			if err := storeCredentialCacheEntry(context.Background(), cachePath, desired); err != nil {
				t.Fatal(err)
			}
			syncNativeOAuthCacheReplacement = originalCacheSync
			if err := recoverNativeOAuthTransactionWithProfileLock(context.Background(), cachePath, "production"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(nativeOAuthTransactionPath(cachePath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal remained after durable cache confirmation: %v", err)
			}
		})
	}
}

func TestConfigureOAuthProfileRejectsUnexpectedAccountBeforePersistence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	original := []byte(`{"current":"production","profiles":[{"name":"production"}]}`)
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(dir, "cache")
	restore := stubNativeOAuthConfigure(t, cacheRoot, func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		return &OAuthLoginResult{SiteType: "CN", AccessToken: "access", RefreshToken: "refresh", AccessTokenExpire: 4102444800}, nil
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{
		ProfileName: "production", ConfigPath: configPath, AliyunConfigPath: filepath.Join(dir, "aliyun.json"),
		ExpectedAccountID: "9999999999999999",
	})
	var configureErr *OAuthConfigureError
	if !errors.As(err, &configureErr) || configureErr.Kind != OAuthConfigureAccountMismatch {
		t.Fatalf("account mismatch error = %v", err)
	}
	updated, _ := os.ReadFile(configPath)
	if string(updated) != string(original) {
		t.Fatalf("account mismatch changed config: %s", updated)
	}
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "production")
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("account mismatch wrote cache: %v", err)
	}
}

func TestConfigureOAuthProfileRequiresCoreAccountConfirmation(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"current":"default","profiles":[{"name":"default"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(dir, "cache")
	restore := stubNativeOAuthConfigure(t, cacheRoot, func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error) {
		return &OAuthLoginResult{SiteType: "CN", AccessToken: "access", RefreshToken: "refresh", AccessTokenExpire: 4102444800}, nil
	})
	defer restore()
	_, err := ConfigureOAuthProfile(context.Background(), OAuthConfigureOptions{
		ConfigPath: configPath, AliyunConfigPath: filepath.Join(dir, "aliyun.json"),
	})
	var configureErr *OAuthConfigureError
	if !errors.As(err, &configureErr) || configureErr.Kind != OAuthConfigureConfirmation || !errors.Is(err, ErrOAuthAccountConfirmationRequired) {
		t.Fatalf("missing core confirmation error = %v", err)
	}
	cachePath := credentialCacheEntryPath(cacheRoot, nativeOAuthCacheSource, "default")
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing confirmation wrote cache: %v", err)
	}
}

func acceptOAuthAccount(string, string) error { return nil }

func writeNativeOAuthConfigForTest(t *testing.T, path string, profile map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"current": profile["name"], "profiles": []any{profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func stubNativeOAuthConfigure(t *testing.T, cacheRoot string, login func(context.Context, OAuthLoginOptions) (*OAuthLoginResult, error)) func() {
	t.Helper()
	originalLogin := performOAuthLogin
	originalRoot := nativeOAuthCacheRootPath
	originalResolveRoot := openAPICredentialCacheRoot
	originalValidate := validateOAuthLoginCredential
	performOAuthLogin = login
	nativeOAuthCacheRootPath = func() (string, error) { return cacheRoot, nil }
	openAPICredentialCacheRoot = func() string { return cacheRoot }
	validateOAuthLoginCredential = func(_ context.Context, result *OAuthLoginResult) (*oauthLoginCredential, error) {
		if result == nil {
			return nil, errors.New("OAuth login result is unavailable")
		}
		return &oauthLoginCredential{
			AccountID: "1234567890123456", IdentityType: "RAMUser",
			Entry: credentialCacheEntry{
				Mode:              credentialModeOAuth,
				OAuthRefreshToken: result.RefreshToken, OAuthRefreshExpire: result.RefreshTokenExpire,
				OAuthAccessToken: result.AccessToken, OAuthAccessExpire: result.AccessTokenExpire,
				AccessKeyID: "sts-id", AccessKeySecret: "sts-secret", SecurityToken: "sts-token", STSExpiration: 4102444800,
			},
		}, nil
	}
	return func() {
		performOAuthLogin = originalLogin
		nativeOAuthCacheRootPath = originalRoot
		openAPICredentialCacheRoot = originalResolveRoot
		validateOAuthLoginCredential = originalValidate
	}
}
