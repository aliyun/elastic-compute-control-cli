package aliyun

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
)

const (
	nativeOAuthTransactionVersion  = 1
	nativeOAuthTransactionMaxBytes = 3 * credentialCacheMaxBytes
)

var (
	syncNativeOAuthConfigReplacement = configfile.SyncReplacement
	syncNativeOAuthCacheReplacement  = configfile.SyncReplacement
)

type nativeOAuthTransactionRecord struct {
	Version            int                  `json:"version"`
	ProfileName        string               `json:"profile_name"`
	ConfigPath         string               `json:"config_path"`
	ResolvedConfigPath string               `json:"resolved_config_path"`
	OldConfigExisted   bool                 `json:"old_config_existed"`
	OldProfileExists   bool                 `json:"old_profile_exists"`
	OldLoginGeneration string               `json:"old_login_generation,omitempty"`
	OldAuthGeneration  string               `json:"old_auth_generation"`
	NewLoginGeneration string               `json:"new_login_generation"`
	NewAuthGeneration  string               `json:"new_auth_generation"`
	HadPrevious        bool                 `json:"had_previous"`
	Previous           credentialCacheEntry `json:"previous,omitempty"`
	Next               credentialCacheEntry `json:"next"`
}

func nativeOAuthTransactionPath(cachePath string) string {
	if cachePath == "" {
		return ""
	}
	return cachePath + ".oauth-transaction.json"
}

func beginNativeOAuthTransactionWrite(cachePath string) (*configfile.PrivateReplace, error) {
	path := nativeOAuthTransactionPath(cachePath)
	if path == "" {
		return nil, errors.New("native OAuth transaction path is unavailable")
	}
	target, err := configfile.Resolve(path, true)
	if err != nil {
		return nil, err
	}
	return target.BeginPrivateReplace(nativeOAuthTransactionMaxBytes)
}

func commitPreparedNativeOAuthTransaction(
	ctx context.Context,
	replacement *configfile.PrivateReplace,
	record nativeOAuthTransactionRecord,
) error {
	if replacement == nil {
		return errors.New("native OAuth transaction replacement is unavailable")
	}
	raw, err := marshalNativeOAuthTransaction(record)
	if err != nil {
		return err
	}
	return replacement.CommitWithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, raw)
}

func marshalNativeOAuthTransaction(record nativeOAuthTransactionRecord) ([]byte, error) {
	record.Version = nativeOAuthTransactionVersion
	if err := validateNativeOAuthTransaction(record); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > nativeOAuthTransactionMaxBytes {
		return nil, errors.New("native OAuth transaction is too large")
	}
	return append(raw, '\n'), nil
}

func loadNativeOAuthTransaction(ctx context.Context, cachePath string) (nativeOAuthTransactionRecord, bool, error) {
	path := nativeOAuthTransactionPath(cachePath)
	if path == "" {
		return nativeOAuthTransactionRecord{}, false, nil
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nativeOAuthTransactionRecord{}, false, nil
	} else if err != nil {
		return nativeOAuthTransactionRecord{}, false, err
	}
	target, err := configfile.Resolve(path, false)
	if err != nil {
		return nativeOAuthTransactionRecord{}, false, err
	}
	var record nativeOAuthTransactionRecord
	err = target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		if err := configfile.ValidatePrivateFile(target.Path()); err != nil {
			return err
		}
		file, err := os.Open(target.Path())
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > nativeOAuthTransactionMaxBytes {
			return errors.New("native OAuth transaction is unsafe or too large")
		}
		raw, err := io.ReadAll(io.LimitReader(file, nativeOAuthTransactionMaxBytes+1))
		if err != nil || len(raw) > nativeOAuthTransactionMaxBytes {
			return errors.New("native OAuth transaction is unreadable or too large")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&record); err != nil {
			return errors.New("native OAuth transaction is invalid")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return errors.New("native OAuth transaction contains trailing data")
		}
		return validateNativeOAuthTransaction(record)
	})
	return record, err == nil, err
}

func validateNativeOAuthTransaction(record nativeOAuthTransactionRecord) error {
	if record.Version != nativeOAuthTransactionVersion || record.ProfileName == "" || record.ConfigPath == "" || record.ResolvedConfigPath == "" ||
		record.NewLoginGeneration == "" || record.NewAuthGeneration == "" ||
		record.Next.Mode != credentialModeOAuth || record.Next.SourceGeneration == "" {
		return errors.New("native OAuth transaction is invalid")
	}
	for _, digest := range []string{record.OldAuthGeneration, record.NewAuthGeneration} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 {
			return errors.New("native OAuth transaction auth generation is invalid")
		}
	}
	if record.HadPrevious && (record.Previous.Mode == "" || record.Previous.SourceGeneration == "") {
		return errors.New("native OAuth transaction previous entry is invalid")
	}
	return nil
}

// recoverNativeOAuthTransaction must run while the canonical profile lock is
// held. It makes the config generation and active cache agree without ever
// discarding a newer, unrelated cache owner.
func recoverNativeOAuthTransaction(ctx context.Context, cachePath, profileName string) error {
	record, found, err := loadNativeOAuthTransaction(ctx, cachePath)
	if err != nil || !found {
		return err
	}
	if record.ProfileName != profileName {
		return errors.New("native OAuth transaction profile does not match the cache owner")
	}
	store, err := ecconfig.LoadNativeOAuthStore(record.ConfigPath)
	if err != nil {
		return err
	}
	if store.ResolvedPath() != record.ResolvedConfigPath {
		if restoreErr := restoreNativeOAuthTransactionPrevious(ctx, cachePath, record); restoreErr != nil {
			return errors.Join(ErrCredentialProfileChanged, restoreErr)
		}
		return errors.Join(ErrCredentialProfileChanged, deleteNativeOAuthTransaction(ctx, cachePath))
	}
	state := store.NativeOAuthProfileState(profileName)
	authGeneration := hex.EncodeToString(state.AuthGeneration[:])
	newConfig := state.ConfigExisted && state.Exists && state.Generation == record.NewLoginGeneration && authGeneration == record.NewAuthGeneration
	oldConfig := state.ConfigExisted == record.OldConfigExisted && state.Exists == record.OldProfileExists && state.Generation == record.OldLoginGeneration && authGeneration == record.OldAuthGeneration

	switch {
	case newConfig:
		if err := syncNativeOAuthConfigReplacement(record.ResolvedConfigPath); err != nil {
			return &configfile.PostCommitError{Err: fmt.Errorf("sync native OAuth config replacement: %w", err)}
		}
		confirmedStore, err := ecconfig.LoadNativeOAuthStore(record.ConfigPath)
		if err != nil {
			return err
		}
		if confirmedStore.ResolvedPath() != record.ResolvedConfigPath {
			return ErrCredentialProfileChanged
		}
		confirmedState := confirmedStore.NativeOAuthProfileState(profileName)
		confirmedAuthGeneration := hex.EncodeToString(confirmedState.AuthGeneration[:])
		if !confirmedState.ConfigExisted || !confirmedState.Exists || confirmedState.Generation != record.NewLoginGeneration || confirmedAuthGeneration != record.NewAuthGeneration {
			return ErrCredentialProfileChanged
		}
		if err := installNativeOAuthTransactionNext(ctx, cachePath, record); err != nil {
			return err
		}
		return deleteNativeOAuthTransaction(ctx, cachePath)
	case oldConfig:
		if err := restoreNativeOAuthTransactionPrevious(ctx, cachePath, record); err != nil {
			return err
		}
		return deleteNativeOAuthTransaction(ctx, cachePath)
	default:
		restoreErr := restoreNativeOAuthTransactionPrevious(ctx, cachePath, record)
		if restoreErr != nil {
			return errors.Join(ErrCredentialProfileChanged, restoreErr)
		}
		return errors.Join(ErrCredentialProfileChanged, deleteNativeOAuthTransaction(ctx, cachePath))
	}
}

func installNativeOAuthTransactionNext(ctx context.Context, cachePath string, record nativeOAuthTransactionRecord) error {
	current, found, err := loadAnyCredentialCacheEntry(ctx, cachePath)
	if err != nil {
		return err
	}
	if found && current.SourceGeneration == record.Next.SourceGeneration {
		if current.Mode != record.Next.Mode {
			return ErrCredentialProfileChanged
		}
		return confirmNativeOAuthCacheReplacement(ctx, cachePath, record.Next.Mode, record.Next.SourceGeneration)
	}
	if found && (!record.HadPrevious || current.SourceGeneration != record.Previous.SourceGeneration) {
		return ErrCredentialProfileChanged
	}
	if found {
		err = storeCredentialCacheEntryIfGeneration(ctx, cachePath, record.Previous.SourceGeneration, record.Next)
	} else {
		err = storeCredentialCacheEntryIfMissing(ctx, cachePath, record.Next)
	}
	if err != nil {
		return err
	}
	return confirmNativeOAuthCacheReplacement(ctx, cachePath, record.Next.Mode, record.Next.SourceGeneration)
}

func restoreNativeOAuthTransactionPrevious(ctx context.Context, cachePath string, record nativeOAuthTransactionRecord) error {
	current, found, err := loadAnyCredentialCacheEntry(ctx, cachePath)
	if err != nil {
		return err
	}
	if record.HadPrevious {
		if found && current.SourceGeneration == record.Previous.SourceGeneration {
			if current.Mode != record.Previous.Mode {
				return ErrCredentialProfileChanged
			}
			return confirmNativeOAuthCacheReplacement(ctx, cachePath, record.Previous.Mode, record.Previous.SourceGeneration)
		}
		if found && current.SourceGeneration != record.Next.SourceGeneration {
			return ErrCredentialProfileChanged
		}
		if found {
			err = storeCredentialCacheEntryIfGeneration(ctx, cachePath, record.Next.SourceGeneration, record.Previous)
		} else {
			err = storeCredentialCacheEntryIfMissing(ctx, cachePath, record.Previous)
		}
		if err != nil {
			return err
		}
		return confirmNativeOAuthCacheReplacement(ctx, cachePath, record.Previous.Mode, record.Previous.SourceGeneration)
	}
	if !found {
		return configfile.RemovePrivateFile(cachePath)
	}
	if current.SourceGeneration != record.Next.SourceGeneration {
		return ErrCredentialProfileChanged
	}
	return rollbackCredentialCacheEntry(ctx, cachePath, record.Next.SourceGeneration, credentialCacheEntry{}, false)
}

func confirmNativeOAuthCacheReplacement(ctx context.Context, cachePath, expectedMode, expectedGeneration string) error {
	if err := syncNativeOAuthCacheReplacement(cachePath); err != nil {
		return &configfile.PostCommitError{Err: fmt.Errorf("sync native OAuth cache replacement: %w", err)}
	}
	current, found, err := loadAnyCredentialCacheEntry(ctx, cachePath)
	if err != nil {
		return err
	}
	if !found || current.Mode != expectedMode || current.SourceGeneration != expectedGeneration {
		return ErrCredentialProfileChanged
	}
	return nil
}

func deleteNativeOAuthTransaction(ctx context.Context, cachePath string) error {
	path := nativeOAuthTransactionPath(cachePath)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return configfile.RemovePrivateFile(path)
	} else if err != nil {
		return err
	}
	target, err := configfile.Resolve(path, false)
	if err != nil {
		return err
	}
	return target.WithLock(ctx, credentialProfileLockTimeout, credentialProfileLockRetry, func() error {
		if err := configfile.ValidatePrivateFile(target.Path()); err != nil {
			return err
		}
		if err := configfile.RemovePrivateFile(target.Path()); err != nil {
			return fmt.Errorf("remove native OAuth transaction: %w", err)
		}
		return nil
	})
}

func nativeOAuthAuthGeneration(profile map[string]any) string {
	digest := ecconfig.CredentialProfileAuthDigest(profile)
	return hex.EncodeToString(digest[:])
}
