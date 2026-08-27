package aliyun

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCredentialCacheIsPrivateAndGenerationBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials-v2", "entry.json")
	entry := credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: "generation-one", OAuthRefreshToken: "refresh", AccessKeySecret: "secret"}
	if err := storeCredentialCacheEntry(context.Background(), path, entry); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode=%v err=%v", info.Mode().Perm(), err)
	}
	loaded, ok, err := loadCredentialCacheEntry(context.Background(), path, credentialModeOAuth, "generation-one")
	if err != nil || !ok || loaded.OAuthRefreshToken != "refresh" {
		t.Fatalf("cache entry=%#v ok=%t err=%v", loaded, ok, err)
	}
	if _, ok, err := loadCredentialCacheEntry(context.Background(), path, credentialModeOAuth, "generation-two"); err != nil || ok {
		t.Fatalf("stale generation ok=%t err=%v", ok, err)
	}
}

func TestCredentialCacheConcurrentProfilesUseIndependentEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "credentials-v2")
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for _, key := range []string{"one", "two"} {
		key := key
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			path := credentialCacheEntryPath(root, "/source/config.json", key)
			errorsCh <- storeCredentialCacheEntry(context.Background(), path, credentialCacheEntry{Mode: credentialModeOAuth, SourceGeneration: key, OAuthRefreshToken: "refresh-" + key})
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"one", "two"} {
		path := credentialCacheEntryPath(root, "/source/config.json", key)
		entry, ok, err := loadCredentialCacheEntry(context.Background(), path, credentialModeOAuth, key)
		if err != nil || !ok || entry.OAuthRefreshToken != "refresh-"+key {
			t.Fatalf("entry %s=%#v ok=%t err=%v", key, entry, ok, err)
		}
	}
}

func TestCredentialCacheRejectsBroadPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "entry.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"entry":{"mode":"OAuth","source_generation":"generation"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCredentialCacheEntry(context.Background(), path, credentialModeOAuth, "generation"); err == nil {
		t.Fatal("broad credential cache permissions were accepted")
	}
}

func TestCredentialCacheEntryPathIsIndependentOfEcctlConfigPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "credentials-v2")
	first := credentialCacheEntryPath(root, "/home/user/.aliyun/config.json", "oauth")
	second := credentialCacheEntryPath(root, "/home/user/.aliyun/config.json", "oauth")
	if first != second {
		t.Fatalf("canonical entry paths differ: %q != %q", first, second)
	}
	if first == credentialCacheEntryPath(root, "/other/config.json", "oauth") || first == credentialCacheEntryPath(root, "/home/user/.aliyun/config.json", "other") {
		t.Fatal("distinct credential sources shared a cache entry")
	}
}

func TestCredentialCacheRootRequiresAbsoluteUserHome(t *testing.T) {
	for _, home := range []string{"", "relative-home"} {
		if root, err := credentialCacheRootPathFromHome(home); err == nil || root != "" {
			t.Fatalf("home %q produced root %q, %v", home, root, err)
		}
	}
	root, err := credentialCacheRootPathFromHome(t.TempDir())
	if err != nil || !filepath.IsAbs(root) || filepath.Base(root) != "credentials-v2" {
		t.Fatalf("absolute credential root = %q, %v", root, err)
	}
}
