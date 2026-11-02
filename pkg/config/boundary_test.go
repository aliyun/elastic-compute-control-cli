package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreUnavailableAndMalformedInputs(t *testing.T) {
	var empty *Store
	if empty.ResolvedPath() != "" || empty.RequestedPath() != "" || empty.Save() == nil || empty.PreflightNativeOAuthWrite(context.Background()) == nil {
		t.Fatal("unavailable store accepted")
	}
	for _, raw := range []string{`{"current":"default","profiles":[]} {}`, `{"current":"default","profiles":[]} invalid`, `{"current":"default","profiles":[null]}`, `{"current":"default","profiles":[{"name":" padded "}]}`} {
		if _, err := decodeNativeStoreData([]byte(raw)); err == nil {
			t.Fatalf("malformed native config accepted: %s", raw)
		}
	}
	for _, value := range []map[string]any{{"bad": make(chan int)}, {"profiles": true}} {
		cloned, err := cloneConfigMap(value)
		if _, bad := value["bad"]; bad && err == nil {
			t.Fatal("unserializable config cloned")
		} else if !bad && (err != nil || cloned["profiles"] != true) {
			t.Fatalf("clone changed data: %v, %v", cloned, err)
		}
	}
	if ValidRegion("cn--hangzhou") {
		t.Fatal("repeated hyphen accepted")
	}
	store := &Store{}
	store.ensureShape()
	if store.Current() != DefaultProfileName {
		t.Fatal("default current profile lost")
	}
	if err := store.applyMutation(storeMutation{kind: "unknown"}, false); err == nil {
		t.Fatal("unknown mutation accepted")
	}
	for _, input := range []struct{ name, site, generation, account string }{{"bad\nname", "CN", "gen", ""}, {"default", "OTHER", "gen", ""}, {"default", "CN", "", ""}, {"default", "CN", "gen", "bad"}} {
		if err := store.SetNativeOAuthProfile(NativeOAuthProfileState{Name: input.name}, input.site, input.generation, input.account); err == nil {
			t.Fatalf("invalid OAuth mutation accepted: %+v", input)
		}
	}
}

func TestConfigLoadersRejectDirectoriesAndUnsafePreflightTargets(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadStore(dir); err == nil {
		t.Fatal("directory loaded as config")
	}
	if _, _, err := LoadExistingStore(dir); err == nil {
		t.Fatal("directory loaded as existing config")
	}
	if _, err := LoadNativeOAuthStore(dir); err == nil {
		t.Fatal("directory loaded as OAuth config")
	}
	path := filepath.Join(dir, "config.json")
	store := newStore(path)
	if store.Current() != DefaultProfileName {
		t.Fatal("unset current lost default")
	}
	store.data["current"] = ""
	if store.Current() != DefaultProfileName {
		t.Fatal("empty current lost default")
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.PreflightNativeOAuthWrite(context.Background()); err == nil {
		t.Fatal("changed target directory accepted")
	}
	store.data["telemetry"] = "invalid"
	if _, err := store.ConfigItems("default", false); err == nil {
		t.Fatal("malformed telemetry enumerated")
	}
}

func TestConfigPathsUseRelativeFallbackWhenHomeUnavailable(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if got := EcctlConfigPath(nil); got != filepath.Join(".ecctl", "config.json") {
		t.Fatalf("ecctl fallback=%s", got)
	}
	if got := AliyunConfigPath(nil); got != filepath.Join(".aliyun", "config.json") {
		t.Fatalf("aliyun fallback=%s", got)
	}
	store := newStore("fixture")
	if value, err := store.GetValue("", "output", false); err != nil || value.Value != "json" {
		t.Fatalf("empty output default=%+v,%v", value, err)
	}
}

func TestStoreSavePreservesConcurrentDataAndRejectsUnsafeTargets(t *testing.T) {
	for _, fault := range []string{"none", "readonly", "corrupt", "directory", "unserializable"} {
		t.Run(fault, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			store := newStore(path)
			if fault == "unserializable" {
				store.data["bad"] = make(chan int)
				if err := store.Save(); err == nil {
					t.Fatal("invalid data saved")
				}
				return
			}
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"current":"other","profiles":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if fault == "none" {
				if err := store.Save(); err != nil || store.Current() != "other" {
					t.Fatalf("concurrent data lost: %s, %v", store.Current(), err)
				}
				return
			}
			if _, err := store.SetValue("default", "region", "cn-hangzhou"); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "readonly":
				if err := os.Chmod(path, 0o400); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
				if err := store.PreflightNativeOAuthWrite(context.Background()); err == nil {
					t.Fatal("read-only preflight succeeded")
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Save(); err == nil {
				t.Fatalf("%s target overwritten", fault)
			}
		})
	}
}

func TestEffectiveConfigErrorsAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", path)
	store, err := LoadStore("")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := store.GetValue("", "output", false); err != nil || value.Value != "json" {
		t.Fatalf("output default=%+v, %v", value, err)
	}
	if value, err := ConfigValueFromProfile(Profile{}, "telemetry.enabled", false); err != nil || value.Value != "true" {
		t.Fatalf("telemetry default=%+v, %v", value, err)
	}
	if _, _, err := LoadExistingStore(""); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativeOAuthStore(""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrentProfile("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetValue("", "security-token", "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetValue("", "access-key-id", "fake-id"); err != nil {
		t.Fatal(err)
	}
	if profile, _ := store.Profile("default"); profile.Mode != "StsToken" {
		t.Fatalf("token mode lost: %+v", profile)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := store.UseProfile("default"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := EffectiveValue("", "telemetry.enabled", false, path, missing); err == nil {
		t.Fatal("malformed telemetry config ignored")
	}
	if _, err := EffectiveValue("", "region", false, path, missing); err == nil {
		t.Fatal("malformed profile ignored")
	}
	if _, err := EffectiveItems("", false, path, missing); err == nil {
		t.Fatal("malformed items config ignored")
	}
	if _, _, err := EffectiveProfile("", missing, path); err == nil {
		t.Fatal("malformed fallback ignored")
	}
	if _, _, err := LoadExistingStore(path); err == nil {
		t.Fatal("malformed store loaded")
	}
	if _, err := LoadStore(filepath.Dir(path)); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory config=%v", err)
	}
}
