package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeOAuthStoreCreatesNonSecretProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	if err := store.SetNativeOAuthProfile(state, "cn", "generation-one", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secretField := range []string{"oauth_access_token", "oauth_refresh_token", "access_key_secret", "sts_token"} {
		if strings.Contains(text, secretField) {
			t.Fatalf("native OAuth config contains %s: %s", secretField, text)
		}
	}
	for _, want := range []string{`"current": "production"`, `"mode": "OAuth"`, `"oauth_site_type": "CN"`, `"oauth_generation": "generation-one"`, `"oauth_account_id": "1234567890123456"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("native OAuth config missing %s: %s", want, text)
		}
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
}

func TestNativeOAuthProfileClearsStaticAuthAndPreservesUnrelatedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"current":"production","custom_id":9007199254740993,"profiles":[{"name":"production","region_id":"cn-hangzhou","mode":"StsToken","access_key_id":"old-id","access_key_secret":"old-secret","sts_token":"old-sts","sts_expiration":4102444800}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	if err := store.SetNativeOAuthProfile(state, "INTL", "generation-two", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, "9007199254740993") || !strings.Contains(text, `"region_id": "cn-hangzhou"`) {
		t.Fatalf("unrelated fields changed: %s", text)
	}
	for _, stale := range []string{"old-id", "old-secret", "old-sts", "sts_expiration"} {
		if strings.Contains(text, stale) {
			t.Fatalf("stale auth %q remained: %s", stale, text)
		}
	}
}

func TestNativeOAuthStoreMergesUnrelatedConcurrentEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"current":"production","custom":"before","profiles":[{"name":"production","region_id":"cn-hangzhou"}]}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	concurrent := `{"current":"production","custom":"after","profiles":[{"name":"production","region_id":"cn-shanghai"}]}`
	if err := os.WriteFile(path, []byte(concurrent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNativeOAuthProfile(state, "CN", "generation-three", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if !strings.Contains(string(updated), `"custom": "after"`) || !strings.Contains(string(updated), `"region_id": "cn-shanghai"`) {
		t.Fatalf("concurrent unrelated edit was lost: %s", updated)
	}
}

func TestNativeOAuthStoreRejectsConcurrentAuthChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"current":"production","profiles":[{"name":"production","mode":"AK","access_key_id":"old","access_key_secret":"old-secret"}]}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	concurrent := `{"current":"production","profiles":[{"name":"production","mode":"AK","access_key_id":"new","access_key_secret":"new-secret"}]}`
	if err := os.WriteFile(path, []byte(concurrent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNativeOAuthProfile(state, "CN", "generation-four", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("concurrent auth change error = %v", err)
	}
	updated, _ := os.ReadFile(path)
	if !strings.Contains(string(updated), "new-secret") || strings.Contains(string(updated), "generation-four") {
		t.Fatalf("concurrent auth file was changed: %s", updated)
	}
}

func TestLoadNativeOAuthStoreRejectsMalformedOrOversizedConfig(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"current":1,"profiles":[]}`, `{"current":"default","profiles":null}`, `{"current":"default","profiles":{}}`, `{"current":"default","profiles":[{"name":"default"},{"name":"default"}]}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadNativeOAuthStore(path); err == nil {
			t.Fatalf("malformed config was accepted: %s", raw)
		}
	}
	largePath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(largePath, []byte(strings.Repeat("x", nativeOAuthConfigLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativeOAuthStore(largePath); err == nil {
		t.Fatal("oversized config was accepted")
	}
}

func TestNativeOAuthStoreRejectsReplacedTarget(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	link := filepath.Join(dir, "config.json")
	valid := []byte(`{"current":"default","profiles":[{"name":"default"}]}`)
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, valid, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Base(first), link); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(link)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("default")
	if err := store.SetNativeOAuthProfile(state, "CN", "generation-five", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(second), link); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err == nil {
		t.Fatal("replaced target was accepted")
	}
}

func TestNativeOAuthStoreRejectsDeletedProfileDuringLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"current":"production","profiles":[{"name":"production","region_id":"cn-hangzhou"}]}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	if err := os.WriteFile(path, []byte(`{"current":"production","profiles":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNativeOAuthProfile(state, "CN", "new-generation", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("deleted profile error = %v", err)
	}
}

func TestNativeOAuthStoreRejectsDeletedConfigFileDuringLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"current":"default","custom":"preserve","profiles":[{"name":"default"}]}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("default")
	if !state.ConfigExisted {
		t.Fatal("existing config was not recorded")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNativeOAuthProfile(state, "CN", "new-generation", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); !errors.Is(err, ErrCredentialProfileChanged) {
		t.Fatalf("deleted config error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted config was recreated: %v", err)
	}
}

func TestNativeOAuthStorePreservesConcurrentCurrentProfileSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := `{"current":"production","profiles":[{"name":"production"},{"name":"other"}]}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadNativeOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.NativeOAuthProfileState("production")
	if err := os.WriteFile(path, []byte(`{"current":"other","profiles":[{"name":"production"},{"name":"other"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNativeOAuthProfile(state, "CN", "new-generation", "1234567890123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if !strings.Contains(string(updated), `"current": "other"`) {
		t.Fatalf("concurrent current-profile switch was lost: %s", updated)
	}
}

func TestNormalizeOAuthAccountID(t *testing.T) {
	if got, err := NormalizeOAuthAccountID(" 1234567890123456 "); err != nil || got != "1234567890123456" {
		t.Fatalf("normalized account = %q err=%v", got, err)
	}
	for _, invalid := range []string{"", "123", "123456789012345x"} {
		if _, err := NormalizeOAuthAccountID(invalid); err == nil {
			t.Fatalf("invalid account %q was accepted", invalid)
		}
	}
}
