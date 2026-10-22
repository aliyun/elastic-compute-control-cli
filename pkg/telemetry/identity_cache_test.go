package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func secureConfigPath(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "ecctl")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "config.json")
}

func seedIdentityCache(t *testing.T, configPath string, cache identityCacheFile) string {
	t.Helper()
	directory := filepath.Join(filepath.Dir(configPath), "telemetry")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := encodeIdentityCache(cache)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "identity-v1.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadIdentityCacheForTest(t *testing.T, path string) identityCacheFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeIdentityCache(raw)
}

func TestIdentityCacheHitPermissionsAndNoRawCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	configPath := secureConfigPath(t)
	calls := 0
	resolver := func(context.Context) (Identity, error) {
		calls++
		return Identity{Hash: "hashed-principal", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"}, nil
	}
	for i := 0; i < 2; i++ {
		identity, err := resolveCachedIdentity(context.Background(), configPath, "raw-access-key", resolver)
		if err != nil || identity.Hash != "hashed-principal" {
			t.Fatalf("resolveCachedIdentity = %#v, %v", identity, err)
		}
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
	directory := filepath.Join(filepath.Dir(configPath), "telemetry")
	cachePath := filepath.Join(directory, "identity-v1.json")
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "raw-access-key") {
		t.Fatalf("cache contains raw AccessKey: %s", raw)
	}
	for _, path := range []string{cachePath, filepath.Join(directory, "identity-v1.lock")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	if leftovers, err := filepath.Glob(filepath.Join(directory, ".identity-v1-*.tmp")); err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary cache files = %#v, err=%v", leftovers, err)
	}
}

func TestIdentityCacheHitRetainsCloudIdentifiers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	for _, want := range []Identity{
		{Hash: "account-hash", Type: "Account", AccountID: "1234567890123456"},
		{Hash: "user-hash", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"},
		{Hash: "role-hash", Type: "AssumedRoleUser", AccountID: "1234567890123456", RoleID: "3456789012345678"},
	} {
		t.Run(want.Type, func(t *testing.T) {
			configPath := secureConfigPath(t)
			calls := 0
			resolver := func(context.Context) (Identity, error) {
				calls++
				return want, nil
			}
			for i := 0; i < 2; i++ {
				got, err := resolveCachedIdentity(context.Background(), configPath, "ak", resolver)
				if err != nil || got != want {
					t.Fatalf("resolveCachedIdentity round %d = %#v, %v; want %#v", i, got, err, want)
				}
			}
			if calls != 1 {
				t.Fatalf("resolver calls = %d, want 1", calls)
			}
		})
	}
}

func TestIdentityCacheRefreshesIncompleteEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	for _, want := range []Identity{
		{Hash: "account-hash", Type: "Account", AccountID: "1234567890123456"},
		{Hash: "user-hash", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"},
		{Hash: "role-hash", Type: "AssumedRoleUser", AccountID: "1234567890123456", RoleID: "3456789012345678"},
	} {
		missingFields := []string{"hash-only"}
		if want.Type == "RAMUser" {
			missingFields = append(missingFields, "account-id", "user-id")
		} else if want.Type == "AssumedRoleUser" {
			missingFields = append(missingFields, "account-id", "role-id")
		}
		for _, missing := range missingFields {
			for _, failResolution := range []bool{false, true} {
				name := want.Type + "/" + missing
				if failResolution {
					name += "/resolver-failure"
				}
				t.Run(name, func(t *testing.T) {
					configPath := secureConfigPath(t)
					key := identityCacheKey("ak")
					incomplete := identityCacheEntry{
						PrincipalHash: want.Hash,
						IdentityType:  want.Type,
						AccountID:     want.AccountID,
						UserID:        want.UserID,
						RoleID:        want.RoleID,
						ResolvedAt:    time.Now().Add(-time.Hour),
					}
					switch missing {
					case "hash-only":
						incomplete.AccountID, incomplete.UserID, incomplete.RoleID = "", "", ""
					case "account-id":
						incomplete.AccountID = ""
					case "user-id":
						incomplete.UserID = ""
					case "role-id":
						incomplete.RoleID = ""
					}
					cachePath := seedIdentityCache(t, configPath, identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{key: incomplete}})
					calls := 0
					wantErr := errors.New("STS unavailable")
					resolver := func(context.Context) (Identity, error) {
						calls++
						if failResolution {
							return Identity{}, wantErr
						}
						return want, nil
					}
					if failResolution {
						got, err := resolveCachedIdentity(context.Background(), configPath, "ak", resolver)
						if !errors.Is(err, wantErr) || got != (Identity{}) {
							t.Fatalf("resolveCachedIdentity = %#v, %v; want empty identity and %v", got, err, wantErr)
						}
						if cache := loadIdentityCacheForTest(t, cachePath); len(cache.Entries) != 0 {
							t.Fatalf("incomplete entry remained after resolver failure: %#v", cache.Entries)
						}
					} else {
						for i := 0; i < 2; i++ {
							got, err := resolveCachedIdentity(context.Background(), configPath, "ak", resolver)
							if err != nil || got != want {
								t.Fatalf("resolveCachedIdentity round %d = %#v, %v; want %#v", i, got, err, want)
							}
						}
						cache := loadIdentityCacheForTest(t, cachePath)
						entry := cache.Entries[key]
						if cache.Version != 1 || entry.AccountID != want.AccountID || entry.UserID != want.UserID || entry.RoleID != want.RoleID {
							t.Fatalf("refreshed cache = %#v, want identifiers %#v in version 1", cache, want)
						}
					}
					if calls != 1 {
						t.Fatalf("resolver calls = %d, want 1 refresh", calls)
					}
				})
			}
		}
	}
}

func TestIdentityCacheConcurrentCallersShareOneResolvedIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	parent := filepath.Join(t.TempDir(), "missing-config-parent")
	configPath := filepath.Join(parent, "config.json")
	var calls atomic.Int32
	resolver := func(context.Context) (Identity, error) {
		calls.Add(1)
		time.Sleep(5 * time.Millisecond)
		return Identity{Hash: "concurrent-hash", Type: "Account", AccountID: "1234567890123456"}, nil
	}
	const workers = 4
	start := make(chan struct{})
	results := make(chan Identity, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			identity, err := resolveCachedIdentity(context.Background(), configPath, "shared-ak", resolver)
			results <- identity
			errors <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for identity := range results {
		if identity.Hash != "concurrent-hash" || identity.Type != "Account" || identity.AccountID != "1234567890123456" {
			t.Fatalf("identity = %#v", identity)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls.Load())
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("created config parent mode = %o, want 700", info.Mode().Perm())
	}
}

func TestIdentityCachePrunesInvalidEntriesAndPersistsOnHit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	configPath := secureConfigPath(t)
	now := time.Now().UTC()
	targetKey := identityCacheKey("valid-ak")
	cachePath := seedIdentityCache(t, configPath, identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{
		targetKey: {PrincipalHash: "valid", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now.Add(-time.Hour)},
		"expired": {PrincipalHash: "old", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now.Add(-identityCacheTTL - time.Hour)},
		"future":  {PrincipalHash: "future", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now.Add(2 * time.Minute)},
		"zero":    {PrincipalHash: "zero", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"},
		"no-hash": {IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now},
		"no-type": {PrincipalHash: "hash", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now},
	}})
	identity, err := resolveCachedIdentity(context.Background(), configPath, "valid-ak", func(context.Context) (Identity, error) {
		t.Fatal("valid target should have been a cache hit")
		return Identity{}, nil
	})
	if err != nil || identity.Hash != "valid" {
		t.Fatalf("identity = %#v, err=%v", identity, err)
	}
	cache := loadIdentityCacheForTest(t, cachePath)
	if len(cache.Entries) != 1 {
		t.Fatalf("pruned entries = %#v, want only valid target", cache.Entries)
	}
}

func TestIdentityCachePersistsPruneWhenExpiredTargetResolverFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	configPath := secureConfigPath(t)
	targetKey := identityCacheKey("expired-ak")
	cachePath := seedIdentityCache(t, configPath, identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{
		targetKey: {PrincipalHash: "expired", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: time.Now().Add(-identityCacheTTL - time.Hour)},
	}})
	wantErr := errors.New("STS unavailable")
	if _, err := resolveCachedIdentity(context.Background(), configPath, "expired-ak", func(context.Context) (Identity, error) {
		return Identity{}, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("resolve error = %v, want %v", err, wantErr)
	}
	cache := loadIdentityCacheForTest(t, cachePath)
	if len(cache.Entries) != 0 {
		t.Fatalf("expired target remained after resolver failure: %#v", cache.Entries)
	}
}

func TestIdentityCacheTrimsAfterAddingFreshEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	configPath := secureConfigPath(t)
	now := time.Now().UTC()
	entries := map[string]identityCacheEntry{}
	for i := 0; i < identityCacheMaxEntries; i++ {
		entries[string(rune('a'+i))] = identityCacheEntry{PrincipalHash: "hash", IdentityType: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567", ResolvedAt: now.Add(-time.Duration(i+1) * time.Minute)}
	}
	cachePath := seedIdentityCache(t, configPath, identityCacheFile{Version: 1, Entries: entries})
	if _, err := resolveCachedIdentity(context.Background(), configPath, "new-ak", func(context.Context) (Identity, error) {
		return Identity{Hash: "fresh", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	cache := loadIdentityCacheForTest(t, cachePath)
	if len(cache.Entries) != identityCacheMaxEntries {
		t.Fatalf("cache entries = %d, want %d", len(cache.Entries), identityCacheMaxEntries)
	}
	if _, ok := cache.Entries[identityCacheKey("new-ak")]; !ok {
		t.Fatal("fresh cache entry was trimmed")
	}
}

func TestIdentityCacheTreatsCorruptionAndLockContentionAsMiss(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables identity cache persistence")
	}
	configPath := secureConfigPath(t)
	cachePath := seedIdentityCache(t, configPath, identityCacheFile{Version: 1, Entries: map[string]identityCacheEntry{}})
	if err := os.WriteFile(cachePath, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	resolver := func(context.Context) (Identity, error) {
		calls++
		return Identity{Hash: "fresh", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"}, nil
	}
	if _, err := resolveCachedIdentity(context.Background(), configPath, "ak-1", resolver); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("resolver calls after corrupt cache = %d, want 1", calls)
	}

	lock := flock.New(filepath.Join(filepath.Dir(cachePath), "identity-v1.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Unlock() }()
	if _, err := resolveCachedIdentity(context.Background(), configPath, "ak-2", resolver); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("resolver calls under lock contention = %d, want 2", calls)
	}
	cache := loadIdentityCacheForTest(t, cachePath)
	if _, ok := cache.Entries[identityCacheKey("ak-2")]; ok {
		t.Fatal("lock-contention fallback unexpectedly wrote cache without the lock")
	}
}
