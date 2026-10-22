package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const (
	identityCacheTTL        = 30 * 24 * time.Hour
	identityCacheMaxEntries = 64
	identityLockTimeout     = 50 * time.Millisecond
)

type identityCacheEntry struct {
	PrincipalHash string    `json:"principal_hash"`
	IdentityType  string    `json:"identity_type"`
	AccountID     string    `json:"account_id"`
	UserID        string    `json:"user_id,omitempty"`
	RoleID        string    `json:"role_id,omitempty"`
	ResolvedAt    time.Time `json:"resolved_at"`
}

type identityCacheFile struct {
	Version int                           `json:"version"`
	Entries map[string]identityCacheEntry `json:"entries"`
}

type identityCacheHandle struct {
	cache identityCacheFile
	write func(identityCacheFile) error
	close func()
}

func resolveCachedIdentity(ctx context.Context, configPath, credentialKey string, resolver IdentityResolver) (Identity, error) {
	if credentialKey == "" || resolver == nil {
		return Identity{}, errors.New("identity unavailable")
	}
	handle, err := openIdentityCache(ctx, configPath)
	if err != nil {
		return resolver(ctx)
	}
	defer handle.close()

	now := time.Now().UTC()
	cache := handle.cache
	dirty := pruneIdentityCache(&cache, now)
	key := identityCacheKey(credentialKey)
	if entry, ok := cache.Entries[key]; ok {
		if dirty {
			_ = handle.write(cache)
		}
		return Identity{Hash: entry.PrincipalHash, Type: entry.IdentityType, AccountID: entry.AccountID, UserID: entry.UserID, RoleID: entry.RoleID}, nil
	}

	identity, resolveErr := resolver(ctx)
	entry := identityCacheEntry{
		PrincipalHash: identity.Hash,
		IdentityType:  identity.Type,
		AccountID:     identity.AccountID,
		UserID:        identity.UserID,
		RoleID:        identity.RoleID,
		ResolvedAt:    now,
	}
	if resolveErr == nil && validIdentityCacheEntry(entry) {
		cache.Entries[key] = entry
		dirty = true
		trimIdentityCache(&cache)
	}
	if dirty {
		_ = handle.write(cache)
	}
	if resolveErr != nil || identity.Hash == "" || identity.Type == "" {
		return Identity{}, resolveErr
	}
	return identity, nil
}

func identityCacheKey(credentialKey string) string {
	digest := sha256.Sum256([]byte("credential\x00" + credentialKey))
	return hex.EncodeToString(digest[:])
}

func decodeIdentityCache(raw []byte) identityCacheFile {
	cache := identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{}}
	if err := json.Unmarshal(raw, &cache); err != nil || cache.Version != 1 || cache.Entries == nil {
		return identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{}}
	}
	return cache
}

func pruneIdentityCache(cache *identityCacheFile, now time.Time) bool {
	if cache == nil {
		return false
	}
	if cache.Entries == nil {
		cache.Entries = map[string]identityCacheEntry{}
		return true
	}
	dirty := false
	oldest := now.Add(-identityCacheTTL)
	latest := now.Add(time.Minute)
	for key, entry := range cache.Entries {
		if entry.ResolvedAt.IsZero() || entry.ResolvedAt.Before(oldest) || entry.ResolvedAt.After(latest) || !validIdentityCacheEntry(entry) {
			delete(cache.Entries, key)
			dirty = true
		}
	}
	return dirty
}

func validIdentityCacheEntry(entry identityCacheEntry) bool {
	if entry.PrincipalHash == "" || entry.AccountID == "" {
		return false
	}
	switch entry.IdentityType {
	case "Account":
		return true
	case "RAMUser":
		return entry.UserID != ""
	case "AssumedRoleUser":
		return entry.RoleID != ""
	default:
		return false
	}
}

func trimIdentityCache(cache *identityCacheFile) {
	if cache == nil || len(cache.Entries) <= identityCacheMaxEntries {
		return
	}
	type keyedEntry struct {
		key string
		at  time.Time
	}
	entries := make([]keyedEntry, 0, len(cache.Entries))
	for key, entry := range cache.Entries {
		entries = append(entries, keyedEntry{key: key, at: entry.ResolvedAt})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	for len(cache.Entries) > identityCacheMaxEntries {
		delete(cache.Entries, entries[0].key)
		entries = entries[1:]
	}
}

func encodeIdentityCache(cache identityCacheFile) ([]byte, error) {
	return json.Marshal(cache)
}
