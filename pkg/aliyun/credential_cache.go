package aliyun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

const (
	credentialCacheVersion  = 2
	credentialCacheMaxBytes = 1 << 20
	nativeOAuthCacheSource  = "ecctl://native-oauth/v1"
)

type credentialCacheEntry struct {
	Mode               string `json:"mode"`
	SourceGeneration   string `json:"source_generation"`
	OAuthRefreshToken  string `json:"oauth_refresh_token,omitempty"`
	OAuthRefreshExpire int64  `json:"oauth_refresh_token_expire,omitempty"`
	OAuthAccessToken   string `json:"oauth_access_token,omitempty"`
	OAuthAccessExpire  int64  `json:"oauth_access_token_expire,omitempty"`
	AccessKeyID        string `json:"access_key_id,omitempty"`
	AccessKeySecret    string `json:"access_key_secret,omitempty"`
	SecurityToken      string `json:"sts_token,omitempty"`
	STSExpiration      int64  `json:"sts_expiration,omitempty"`
	UpdatedAt          int64  `json:"updated_at"`
}

type credentialCacheFile struct {
	Version int                  `json:"version"`
	Entry   credentialCacheEntry `json:"entry"`
}

type credentialCacheMatch uint8

const (
	credentialCacheMissing credentialCacheMatch = iota
	credentialCacheMatching
	credentialCacheMismatched
)

func credentialCacheRootPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("credential cache user home is unavailable")
	}
	return credentialCacheRootPathFromHome(home)
}

func credentialCacheRootPathFromHome(home string) (string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("credential cache user home must be absolute")
	}
	return filepath.Join(filepath.Clean(home), ".ecctl", "credentials-v2"), nil
}

func credentialCacheEntryPath(rootPath, sourceConfigPath, profileName string) string {
	if rootPath == "" {
		return ""
	}
	return filepath.Join(rootPath, credentialCacheEntryKey(sourceConfigPath, profileName)+".json")
}

func credentialCacheEntryKey(sourceConfigPath, profileName string) string {
	canonical := sourceConfigPath
	if strings.HasPrefix(sourceConfigPath, "ecctl://") {
		canonical = sourceConfigPath
	} else if target, err := configfile.Resolve(sourceConfigPath, false); err == nil {
		canonical = target.Path()
	} else if absolute, absErr := filepath.Abs(sourceConfigPath); absErr == nil {
		canonical = absolute
	}
	digest := sha256.Sum256([]byte(canonical + "\x00" + profileName))
	return hex.EncodeToString(digest[:])
}

func credentialSourceGeneration(profile map[string]any, mode string) string {
	keys := []string{"name", "mode"}
	switch mode {
	case credentialModeOAuth:
		keys = append(keys, "oauth_site_type", "oauth_refresh_token", "oauth_refresh_token_expire", "oauth_access_token", "oauth_access_token_expire", "oauth_generation", "oauth_account_id")
	case credentialModeCloudSSO:
		keys = append(keys, "cloud_sso_sign_in_url", "cloud_sso_account_id", "cloud_sso_access_config", "access_token", "cloud_sso_access_token_expire")
	default:
		return fmt.Sprintf("%x", ecconfig.CredentialProfileAuthDigest(profile))
	}
	values := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := profile[key]; ok {
			values[key] = value
		}
	}
	raw, _ := json.Marshal(values)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func loadCredentialCacheEntry(ctx context.Context, cachePath, mode, generation string) (credentialCacheEntry, bool, error) {
	entry, match, err := loadCredentialCacheEntryState(ctx, cachePath, mode, generation)
	return entry, match == credentialCacheMatching, err
}

func loadCredentialCacheEntryState(ctx context.Context, cachePath, mode, generation string) (credentialCacheEntry, credentialCacheMatch, error) {
	if cachePath == "" {
		return credentialCacheEntry{}, credentialCacheMissing, nil
	}
	dir := filepath.Dir(cachePath)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return credentialCacheEntry{}, credentialCacheMissing, nil
	} else if err != nil {
		return credentialCacheEntry{}, credentialCacheMissing, err
	}
	if err := configfile.PreparePrivateDirectory(dir); err != nil {
		return credentialCacheEntry{}, credentialCacheMissing, err
	}
	if _, err := os.Lstat(cachePath); errors.Is(err, os.ErrNotExist) {
		return credentialCacheEntry{}, credentialCacheMissing, nil
	} else if err != nil {
		return credentialCacheEntry{}, credentialCacheMissing, err
	}
	target, err := configfile.Resolve(cachePath, false)
	if err != nil {
		return credentialCacheEntry{}, credentialCacheMissing, err
	}
	var entry credentialCacheEntry
	match := credentialCacheMissing
	err = target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		candidate, err := readCredentialCache(target.Path())
		if err != nil {
			return err
		}
		if candidate.Mode != mode || candidate.SourceGeneration != generation {
			match = credentialCacheMismatched
			return nil
		}
		entry, match = candidate, credentialCacheMatching
		return nil
	})
	return entry, match, err
}

func loadAnyCredentialCacheEntry(ctx context.Context, cachePath string) (credentialCacheEntry, bool, error) {
	if cachePath == "" {
		return credentialCacheEntry{}, false, nil
	}
	dir := filepath.Dir(cachePath)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return credentialCacheEntry{}, false, nil
	} else if err != nil {
		return credentialCacheEntry{}, false, err
	}
	if err := configfile.PreparePrivateDirectory(dir); err != nil {
		return credentialCacheEntry{}, false, err
	}
	if _, err := os.Lstat(cachePath); errors.Is(err, os.ErrNotExist) {
		return credentialCacheEntry{}, false, nil
	} else if err != nil {
		return credentialCacheEntry{}, false, err
	}
	target, err := configfile.Resolve(cachePath, false)
	if err != nil {
		return credentialCacheEntry{}, false, err
	}
	var entry credentialCacheEntry
	err = target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		candidate, err := readCredentialCache(target.Path())
		if err != nil {
			return err
		}
		entry = candidate
		return nil
	})
	return entry, err == nil, err
}

func beginCredentialCacheWrite(ctx context.Context, cachePath string) (*configfile.PrivateReplace, error) {
	if cachePath == "" {
		return nil, errors.New("credential cache path is unavailable")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := configfile.PreparePrivateDirectory(filepath.Dir(cachePath)); err != nil {
		return nil, err
	}
	target, err := configfile.Resolve(cachePath, true)
	if err != nil {
		return nil, err
	}
	return target.BeginPrivateReplace(credentialCacheMaxBytes)
}

func storeCredentialCacheEntry(ctx context.Context, cachePath string, entry credentialCacheEntry) error {
	if cachePath == "" {
		return errors.New("credential cache path is unavailable")
	}
	if err := configfile.PreparePrivateDirectory(filepath.Dir(cachePath)); err != nil {
		return err
	}
	target, err := configfile.Resolve(cachePath, true)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		raw, err := marshalCredentialCacheEntry(entry)
		if err != nil {
			return err
		}
		return target.AtomicWritePrivate(raw)
	})
}

func storeCredentialCacheEntryIfGeneration(ctx context.Context, cachePath, expectedGeneration string, entry credentialCacheEntry) error {
	if cachePath == "" || expectedGeneration == "" {
		return errors.New("credential cache path and expected generation are required")
	}
	target, err := configfile.Resolve(cachePath, false)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		current, err := readCredentialCache(target.Path())
		if err != nil {
			return err
		}
		if current.SourceGeneration != expectedGeneration || current.Mode != entry.Mode {
			return ErrCredentialProfileChanged
		}
		raw, err := marshalCredentialCacheEntry(entry)
		if err != nil {
			return err
		}
		return target.AtomicWritePrivate(raw)
	})
}

func storeCredentialCacheEntryIfMissing(ctx context.Context, cachePath string, entry credentialCacheEntry) error {
	if cachePath == "" {
		return errors.New("credential cache path is unavailable")
	}
	if err := configfile.PreparePrivateDirectory(filepath.Dir(cachePath)); err != nil {
		return err
	}
	target, err := configfile.Resolve(cachePath, true)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		if _, err := os.Lstat(target.Path()); err == nil {
			return ErrCredentialProfileChanged
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		raw, err := marshalCredentialCacheEntry(entry)
		if err != nil {
			return err
		}
		return target.AtomicWritePrivate(raw)
	})
}

func commitPreparedCredentialCacheEntry(replacement *configfile.PrivateReplace, entry credentialCacheEntry) error {
	if replacement == nil {
		return errors.New("prepared credential cache replacement is unavailable")
	}
	raw, err := marshalCredentialCacheEntry(entry)
	if err != nil {
		return err
	}
	return replacement.Commit(raw)
}

func commitPreparedCredentialCacheEntryIfGeneration(
	ctx context.Context,
	cachePath, expectedGeneration string,
	replacement *configfile.PrivateReplace,
	entry credentialCacheEntry,
) error {
	if replacement == nil || cachePath == "" || expectedGeneration == "" {
		return errors.New("prepared credential cache replacement and expected generation are required")
	}
	raw, err := marshalCredentialCacheEntry(entry)
	if err != nil {
		return err
	}
	target, err := configfile.Resolve(cachePath, false)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		current, err := readCredentialCache(target.Path())
		if err != nil {
			return err
		}
		if current.Mode != entry.Mode || current.SourceGeneration != expectedGeneration {
			return ErrCredentialProfileChanged
		}
		return replacement.Commit(raw)
	})
}

func commitPreparedCredentialCacheEntryIfMissing(
	ctx context.Context,
	cachePath string,
	replacement *configfile.PrivateReplace,
	entry credentialCacheEntry,
) error {
	if replacement == nil || cachePath == "" {
		return errors.New("prepared credential cache replacement is unavailable")
	}
	raw, err := marshalCredentialCacheEntry(entry)
	if err != nil {
		return err
	}
	target, err := configfile.Resolve(cachePath, true)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		if _, err := os.Lstat(target.Path()); err == nil {
			return ErrCredentialProfileChanged
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return replacement.Commit(raw)
	})
}

func rollbackCredentialCacheEntry(ctx context.Context, cachePath, replacedGeneration string, previous credentialCacheEntry, hadPrevious bool) error {
	if cachePath == "" {
		return errors.New("credential cache path is unavailable")
	}
	target, err := configfile.Resolve(cachePath, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		current, err := readCredentialCache(target.Path())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return configfile.RemovePrivateFile(target.Path())
			}
			return err
		}
		if current.SourceGeneration != replacedGeneration {
			return ErrCredentialProfileChanged
		}
		if hadPrevious {
			raw, err := marshalCredentialCacheEntry(previous)
			if err != nil {
				return err
			}
			return target.AtomicWritePrivate(raw)
		}
		return configfile.RemovePrivateFile(target.Path())
	})
}

func marshalCredentialCacheEntry(entry credentialCacheEntry) ([]byte, error) {
	entry.UpdatedAt = time.Now().UTC().Unix()
	raw, err := json.MarshalIndent(credentialCacheFile{Version: credentialCacheVersion, Entry: entry}, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > credentialCacheMaxBytes {
		return nil, errors.New("credential cache entry is too large")
	}
	return append(raw, '\n'), nil
}

func readCredentialCache(path string) (credentialCacheEntry, error) {
	if err := configfile.ValidatePrivateFile(path); err != nil {
		return credentialCacheEntry{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return credentialCacheEntry{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return credentialCacheEntry{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > credentialCacheMaxBytes {
		return credentialCacheEntry{}, errors.New("credential cache is unsafe or too large")
	}
	raw, err := io.ReadAll(io.LimitReader(file, credentialCacheMaxBytes+1))
	if err != nil {
		return credentialCacheEntry{}, err
	}
	if len(raw) > credentialCacheMaxBytes {
		return credentialCacheEntry{}, errors.New("credential cache is too large")
	}
	var cache credentialCacheFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cache); err != nil || cache.Version != credentialCacheVersion || cache.Entry.Mode == "" || cache.Entry.SourceGeneration == "" {
		return credentialCacheEntry{}, errors.New("credential cache is invalid")
	}
	return cache.Entry, nil
}

func cacheEntryProfile(source map[string]any, entry credentialCacheEntry) map[string]any {
	profile := cloneStringAnyMap(source)
	for key, value := range map[string]any{
		"oauth_refresh_token":        entry.OAuthRefreshToken,
		"oauth_refresh_token_expire": entry.OAuthRefreshExpire,
		"oauth_access_token":         entry.OAuthAccessToken,
		"oauth_access_token_expire":  entry.OAuthAccessExpire,
		"access_key_id":              entry.AccessKeyID,
		"access_key_secret":          entry.AccessKeySecret,
		"sts_token":                  entry.SecurityToken,
		"sts_expiration":             entry.STSExpiration,
	} {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				profile[key] = typed
			}
		case int64:
			if typed > 0 {
				profile[key] = typed
			}
		}
	}
	return profile
}

func credentialCacheError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s credential cache: %w", operation, err)
}
