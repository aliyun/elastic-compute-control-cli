package aliyun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

func TestPreparedCredentialCacheCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "entry.json")
	first := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "first", OAuthRefreshToken: "first-token"}
	second := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "second", OAuthRefreshToken: "second-token"}
	if err := storeCredentialCacheEntry(ctx, path, first); err != nil {
		t.Fatal(err)
	}
	target, err := configfile.Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := target.BeginPrivateReplace(credentialCacheMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	if err := commitPreparedCredentialCacheEntryIfGeneration(ctx, path, "wrong", prepared, second); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("wrong owner error = %v", err)
	}
	if err := commitPreparedCredentialCacheEntryIfGeneration(ctx, path, "first", prepared, second); err != nil {
		t.Fatal(err)
	}
	entry, found, err := loadCredentialCacheEntry(ctx, path, credentialModeOAuth, "second")
	if err != nil || !found || entry.OAuthRefreshToken != "second-token" {
		t.Fatalf("committed entry=%+v found=%t err=%v", entry, found, err)
	}
	if err := rollbackCredentialCacheEntry(ctx, path, "wrong", first, true); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("rollback wrong owner = %v", err)
	}
	if err := rollbackCredentialCacheEntry(ctx, path, "second", first, true); err != nil {
		t.Fatal(err)
	}
	entry, found, err = loadCredentialCacheEntry(ctx, path, credentialModeOAuth, "first")
	if err != nil || !found || entry.OAuthRefreshToken != "first-token" {
		t.Fatalf("rollback entry=%+v found=%t err=%v", entry, found, err)
	}
	if err := rollbackCredentialCacheEntry(ctx, path, "first", credentialCacheEntry{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback left new cache: %v", err)
	}
	if err := rollbackCredentialCacheEntry(ctx, path, "first", first, true); err != nil {
		t.Fatal(err)
	}
	if err := rollbackCredentialCacheEntry(ctx, filepath.Join(t.TempDir(), "missing", "entry"), "first", first, false); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialCacheRejectsOversizedTransactionsWithoutChangingEntry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "entry.json")
	entry := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "current", OAuthRefreshToken: "original"}
	if err := storeCredentialCacheEntry(ctx, path, entry); err != nil {
		t.Fatal(err)
	}
	prepared, err := beginCredentialCacheWrite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	oversized := entry
	oversized.OAuthRefreshToken = strings.Repeat("x", credentialCacheMaxBytes)
	for _, write := range []func() error{
		func() error { return storeCredentialCacheEntry(ctx, path, oversized) },
		func() error { return storeCredentialCacheEntryIfGeneration(ctx, path, "current", oversized) },
		func() error {
			return storeCredentialCacheEntryIfMissing(ctx, filepath.Join(filepath.Dir(path), "missing.json"), oversized)
		},
		func() error { return commitPreparedCredentialCacheEntry(prepared, oversized) },
		func() error {
			return commitPreparedCredentialCacheEntryIfGeneration(ctx, path, "current", prepared, oversized)
		},
		func() error { return commitPreparedCredentialCacheEntryIfMissing(ctx, path, prepared, oversized) },
	} {
		if err := write(); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("oversized write error=%v", err)
		}
		got, err := readCredentialCache(path)
		if err != nil || got.OAuthRefreshToken != "original" {
			t.Fatalf("failed write changed entry: %+v, %v", got, err)
		}
	}
	if err := commitPreparedCredentialCacheEntryIfMissing(ctx, path, prepared, entry); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("existing generation overwritten: %v", err)
	}
}

func TestCredentialCacheMissingAndMalformedPathsFailClosed(t *testing.T) {
	ctx := context.Background()
	entry := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "current"}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing", "entry")} {
		if _, found, err := loadAnyCredentialCacheEntry(ctx, path); found || err != nil {
			t.Fatalf("absent cache found=%t, %v", found, err)
		}
		if _, match, err := loadCredentialCacheEntryState(ctx, path, entry.Mode, entry.SourceGeneration); match != credentialCacheMissing || err != nil {
			t.Fatalf("absent cache match=%v, %v", match, err)
		}
	}
	if _, err := beginCredentialCacheWrite(ctx, ""); err == nil {
		t.Fatal("empty reservation path accepted")
	}
	if storeCredentialCacheEntry(ctx, "", entry) == nil || storeCredentialCacheEntryIfGeneration(ctx, "", "", entry) == nil || storeCredentialCacheEntryIfMissing(ctx, "", entry) == nil {
		t.Fatal("empty write path accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := beginCredentialCacheWrite(canceled, filepath.Join(t.TempDir(), "entry")); !errors.Is(err, context.Canceled) {
		t.Fatalf("reservation ignored cancellation: %v", err)
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(blocker, "entry"), filepath.Join(blocker, "child", "entry")} {
		if _, _, err := loadAnyCredentialCacheEntry(ctx, path); err == nil {
			t.Fatal("unsafe cache parent accepted")
		}
		if _, _, err := loadCredentialCacheEntryState(ctx, path, entry.Mode, entry.SourceGeneration); err == nil {
			t.Fatal("unsafe cache parent accepted")
		}
		if _, err := beginCredentialCacheWrite(ctx, path); err == nil {
			t.Fatal("unsafe reservation accepted")
		}
		if storeCredentialCacheEntry(ctx, path, entry) == nil || storeCredentialCacheEntryIfGeneration(ctx, path, "current", entry) == nil || storeCredentialCacheEntryIfMissing(ctx, path, entry) == nil {
			t.Fatal("unsafe write accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "private", "entry")
	if err := configfile.PreparePrivateDirectory(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"broken", `{}`, strings.Repeat("x", credentialCacheMaxBytes+1)} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readCredentialCache(path); err == nil {
			t.Fatal("invalid cache accepted")
		}
		if _, _, err := loadAnyCredentialCacheEntry(ctx, path); err == nil {
			t.Fatal("invalid cache loaded")
		}
		if err := storeCredentialCacheEntryIfGeneration(ctx, path, "current", entry); err == nil {
			t.Fatal("invalid source overwritten")
		}
	}
	if credentialCacheEntryPath("", "source", "profile") != "" || credentialCacheError("read", nil) != nil {
		t.Fatal("absent cache converted to a path or error")
	}
	if credentialSourceGeneration(map[string]any{"name": "test"}, "AK") == "" {
		t.Fatal("nonrenewable source generation missing")
	}
}

func TestPreparedCredentialCacheRejectsUnavailableState(t *testing.T) {
	ctx := context.Background()
	entry := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "generation"}
	if commitPreparedCredentialCacheEntry(nil, entry) == nil || commitPreparedCredentialCacheEntryIfGeneration(ctx, "path", "generation", nil, entry) == nil || commitPreparedCredentialCacheEntryIfMissing(ctx, "path", nil, entry) == nil || rollbackCredentialCacheEntry(ctx, "", "generation", entry, false) == nil {
		t.Fatal("unavailable cache transaction accepted")
	}
}
