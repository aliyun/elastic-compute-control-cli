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

func resolveCachedIdentity(ctx context.Context, configPath, accessKeyID string, resolver IdentityResolver) (Identity, error) {
	if accessKeyID == "" || resolver == nil {
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
	key := identityCacheKey(accessKeyID)
	if entry, ok := cache.Entries[key]; ok {
		if dirty {
			_ = handle.write(cache)
		}
		return Identity{Hash: entry.PrincipalHash, Type: entry.IdentityType}, nil
	}

	identity, resolveErr := resolver(ctx)
	if resolveErr == nil && identity.Hash != "" && identity.Type != "" {
		cache.Entries[key] = identityCacheEntry{PrincipalHash: identity.Hash, IdentityType: identity.Type, ResolvedAt: now}
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

func identityCacheKey(accessKeyID string) string {
	digest := sha256.Sum256([]byte("ak\x00" + accessKeyID))
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
		if entry.ResolvedAt.IsZero() || entry.ResolvedAt.Before(oldest) || entry.ResolvedAt.After(latest) || entry.PrincipalHash == "" || entry.IdentityType == "" {
			delete(cache.Entries, key)
			dirty = true
		}
	}
	return dirty
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
