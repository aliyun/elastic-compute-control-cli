package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRegionClassifiesMissingAndInvalid(t *testing.T) {
	tests := []struct {
		name        string
		explicit    string
		env         string
		wantRegion  string
		wantErrCode string
	}{
		{name: "explicit", explicit: "us-west-1", wantRegion: "us-west-1"},
		{name: "env", env: "cn-hangzhou", wantRegion: "cn-hangzhou"},
		{name: "missing", wantErrCode: "MissingRegion"},
		{name: "invalid", explicit: "not-a-real-region", wantErrCode: "InvalidRegion"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRegion(tc.explicit, func(string) string { return tc.env })
			if tc.wantErrCode != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if err.Payload().Code != tc.wantErrCode {
					t.Fatalf("error code = %q, want %q", err.Payload().Code, tc.wantErrCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveRegion: %v", err)
			}
			if got != tc.wantRegion {
				t.Fatalf("region = %q, want %q", got, tc.wantRegion)
			}
		})
	}
}

func TestTelemetryEnabledIsGlobalAndDefaultsOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if !TelemetryEnabled(path) {
		t.Fatal("missing config should default telemetry to enabled")
	}
	store, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetValue("profile-a", "telemetry.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if TelemetryEnabled(path) {
		t.Fatal("stored global telemetry false was ignored")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	telemetryConfig, _ := document["telemetry"].(map[string]any)
	if telemetryConfig["enabled"] != false {
		t.Fatalf("top-level telemetry config = %#v", telemetryConfig)
	}
	profiles, _ := document["profiles"].([]any)
	for _, rawProfile := range profiles {
		profile, _ := rawProfile.(map[string]any)
		if _, exists := profile["telemetry.enabled"]; exists {
			t.Fatalf("telemetry setting leaked into profile: %#v", profile)
		}
	}
}

func TestTelemetryDescriptionDisclosesPseudonymousCollection(t *testing.T) {
	item, ok := lookupConfigItem("telemetry.enabled")
	if !ok {
		t.Fatal("telemetry.enabled config item is missing")
	}
	if !strings.Contains(item.Description, "pseudonymous") || strings.Contains(item.Description, "anonymous") {
		t.Fatalf("telemetry description = %q, want accurate pseudonymous disclosure", item.Description)
	}
}

func TestTelemetryEnabledRejectsInvalidValue(t *testing.T) {
	store, err := LoadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"TRUE", "1", "yes", ""} {
		if _, err := store.SetValue("", "telemetry.enabled", value); err == nil {
			t.Fatalf("telemetry.enabled %q was accepted", value)
		}
	}
}

func TestTelemetryEnabledFailsClosedForMalformedConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		wantEnabled   bool
		wantValue     string
		wantLoadError bool
		wantGetError  bool
	}{
		{name: "absent telemetry", raw: `{}`, wantEnabled: true, wantValue: "true"},
		{name: "object without enabled", raw: `{"telemetry":{}}`, wantEnabled: true, wantValue: "true"},
		{name: "enabled true", raw: `{"telemetry":{"enabled":true}}`, wantEnabled: true, wantValue: "true"},
		{name: "enabled false", raw: `{"telemetry":{"enabled":false}}`, wantValue: "false"},
		{name: "top-level null", raw: `null`, wantEnabled: true, wantValue: "true"},
		{name: "top-level array", raw: `[]`, wantLoadError: true},
		{name: "top-level string", raw: `"invalid"`, wantLoadError: true},
		{name: "top-level number", raw: `1`, wantLoadError: true},
		{name: "top-level boolean", raw: `true`, wantLoadError: true},
		{name: "telemetry null", raw: `{"telemetry":null}`, wantGetError: true},
		{name: "telemetry array", raw: `{"telemetry":[]}`, wantGetError: true},
		{name: "telemetry string", raw: `{"telemetry":"invalid"}`, wantGetError: true},
		{name: "telemetry number", raw: `{"telemetry":1}`, wantGetError: true},
		{name: "telemetry boolean", raw: `{"telemetry":true}`, wantGetError: true},
		{name: "enabled null", raw: `{"telemetry":{"enabled":null}}`, wantGetError: true},
		{name: "enabled string", raw: `{"telemetry":{"enabled":"false"}}`, wantGetError: true},
		{name: "enabled number", raw: `{"telemetry":{"enabled":1}}`, wantGetError: true},
		{name: "enabled object", raw: `{"telemetry":{"enabled":{}}}`, wantGetError: true},
		{name: "enabled array", raw: `{"telemetry":{"enabled":[]}}`, wantGetError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := TelemetryEnabled(path); got != tc.wantEnabled {
				t.Fatalf("TelemetryEnabled = %t, want %t", got, tc.wantEnabled)
			}
			store, err := LoadStore(path)
			if tc.wantLoadError {
				if err == nil {
					t.Fatal("LoadStore accepted malformed top-level config")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			value, err := store.GetValue("", "telemetry.enabled", false)
			if tc.wantGetError {
				if err == nil {
					t.Fatalf("GetValue accepted malformed telemetry config: %#v", value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if value.Value != tc.wantValue {
				t.Fatalf("telemetry.enabled = %q, want %q", value.Value, tc.wantValue)
			}
		})
	}
}

func TestSetTelemetryEnabledPreservesSiblingFieldsAndRepairsMalformedEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, map[string]any{
		"telemetry": map[string]any{
			"enabled":     "malformed",
			"sample_rate": 0.25,
			"future":      map[string]any{"mode": "keep"},
		},
	})
	store, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetValue("ignored-profile", "telemetry.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	telemetryConfig, ok := document["telemetry"].(map[string]any)
	if !ok {
		t.Fatalf("telemetry config = %#v", document["telemetry"])
	}
	if telemetryConfig["enabled"] != false || telemetryConfig["sample_rate"] != 0.25 {
		t.Fatalf("telemetry sibling fields were not preserved: %#v", telemetryConfig)
	}
	if future, _ := telemetryConfig["future"].(map[string]any); future["mode"] != "keep" {
		t.Fatalf("nested telemetry sibling was not preserved: %#v", telemetryConfig)
	}
}

func TestSetTelemetryEnabledRejectsNonObjectContainerWithoutMutation(t *testing.T) {
	for _, container := range []any{nil, "invalid", []any{"invalid"}, true, float64(1)} {
		t.Run(fmt.Sprintf("%T", container), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeConfig(t, path, map[string]any{"telemetry": container})
			beforeFile, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store, err := LoadStore(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeData, err := json.Marshal(store.data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetValue("", "telemetry.enabled", "true"); err == nil {
				t.Fatal("SetValue accepted a non-object telemetry container")
			}
			afterData, err := json.Marshal(store.data)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterData) != string(beforeData) {
				t.Fatalf("SetValue mutated store on error: before=%s after=%s", beforeData, afterData)
			}
			afterFile, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterFile) != string(beforeFile) {
				t.Fatalf("SetValue changed file on error: before=%s after=%s", beforeFile, afterFile)
			}
		})
	}
}

func TestLoadAliyunCLIConfigPreservesProfilesAndResolvesRegion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, map[string]any{
		"current": "prod",
		"profiles": []any{
			map[string]any{
				"name":              "prod",
				"mode":              "AK",
				"access_key_id":     "keep-id",
				"access_key_secret": "keep-secret",
				"region_id":         "us-west-1",
				"output_format":     "json",
			},
			map[string]any{
				"name":      "staging",
				"mode":      "AK",
				"region_id": "cn-hangzhou",
			},
		},
	})

	cfg, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	profile, ok := cfg.Profile("")
	if !ok {
		t.Fatal("current profile not found")
	}
	if profile.Name != "prod" || profile.Region != "us-west-1" || profile.Mode != "AK" {
		t.Fatalf("unexpected current profile: %#v", profile)
	}

	gotRegion, appErr := ResolveRegionForProfile("", "", path, func(string) string { return "" })
	if appErr != nil {
		t.Fatalf("ResolveRegionForProfile: %v", appErr)
	}
	if gotRegion != "us-west-1" {
		t.Fatalf("region = %q, want us-west-1", gotRegion)
	}
}

func TestSetRegionPreservesAliyunCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{
				"name":              "default",
				"mode":              "AK",
				"access_key_id":     "keep-id",
				"access_key_secret": "keep-secret",
				"region_id":         "us-west-1",
			},
		},
	})

	cfg, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if err := cfg.SetRegion("default", "cn-hangzhou"); err != nil {
		t.Fatalf("SetRegion: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("saved config is invalid JSON: %v", err)
	}
	profiles := decoded["profiles"].([]any)
	profile := profiles[0].(map[string]any)
	if profile["access_key_id"] != "keep-id" || profile["access_key_secret"] != "keep-secret" {
		t.Fatalf("credentials were not preserved: %#v", profile)
	}
	if profile["region_id"] != "cn-hangzhou" {
		t.Fatalf("region_id = %v, want cn-hangzhou", profile["region_id"])
	}
}

func TestUseProfileRequiresExistingAliyunProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfig(t, path, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "region_id": "us-west-1"},
			map[string]any{"name": "prod", "region_id": "cn-hangzhou"},
		},
	})

	cfg, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if err := cfg.UseProfile("prod"); err != nil {
		t.Fatalf("UseProfile existing: %v", err)
	}
	profile, ok := cfg.Profile("")
	if !ok || profile.Name != "prod" {
		t.Fatalf("current profile = %#v ok=%v, want prod", profile, ok)
	}
	if err := cfg.UseProfile("missing"); err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestConfigPathUsesExplicitEnvironmentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ecctl.json")

	got := ConfigPath(func(name string) string {
		if name == "ECCTL_CONFIG_PATH" {
			return path
		}
		return ""
	})
	if got != path {
		t.Fatalf("ConfigPath() = %q, want %q", got, path)
	}
}

func TestProfileNamePrefersExplicitThenEnvironment(t *testing.T) {
	if got := ProfileName("prod", func(string) string { return "staging" }); got != "prod" {
		t.Fatalf("ProfileName explicit = %q, want prod", got)
	}

	got := ProfileName("", func(name string) string {
		if name == "ALIBABACLOUD_PROFILE" {
			return "staging"
		}
		return ""
	})
	if got != "staging" {
		t.Fatalf("ProfileName env = %q, want staging", got)
	}
}

func TestEffectiveConfigValuesMaskSecretsAndDefaultOutput(t *testing.T) {
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "missing-aliyun.json")
	writeConfig(t, ecctlPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{
				"name":              "default",
				"access_key_secret": "secret",
				"region_id":         "cn-hangzhou",
			},
		},
	})

	secret, err := EffectiveValue("", "access-key-secret", false, ecctlPath, aliyunPath)
	if err != nil {
		t.Fatalf("EffectiveValue secret: %v", err)
	}
	if secret.Value != "********" || !secret.Sensitive {
		t.Fatalf("masked secret = %#v, want masked sensitive value", secret)
	}

	output, err := EffectiveValue("", "output", false, ecctlPath, aliyunPath)
	if err != nil {
		t.Fatalf("EffectiveValue output: %v", err)
	}
	if output.Value != "json" {
		t.Fatalf("default output = %q, want json", output.Value)
	}

	items, err := EffectiveItems("", false, ecctlPath, aliyunPath)
	if err != nil {
		t.Fatalf("EffectiveItems: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("EffectiveItems returned no items")
	}
}

func TestSupportedItemsReturnsCopy(t *testing.T) {
	items := SupportedItems()
	if len(items) == 0 {
		t.Fatal("SupportedItems returned no items")
	}
	items[0].Value = "mutated"

	fresh := SupportedItems()
	if fresh[0].Value == "mutated" {
		t.Fatal("SupportedItems must return a copy")
	}
}

func TestStoreConfigItemsAndCurrentProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.json")
	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if got := store.Current(); got != DefaultProfileName {
		t.Fatalf("Current() = %q, want %q", got, DefaultProfileName)
	}
	if value, err := store.GetValue("", "output", false); err != nil || value.Value != "json" {
		t.Fatalf("GetValue output = %#v, %v; want json", value, err)
	}

	if _, err := store.SetValue("prod", "access-key-secret", "secret"); err != nil {
		t.Fatalf("SetValue secret: %v", err)
	}
	items, err := store.ConfigItems("prod", false)
	if err != nil {
		t.Fatalf("ConfigItems: %v", err)
	}
	foundSecret := false
	for _, item := range items {
		if item.Key == "access-key-secret" {
			foundSecret = true
			if item.Value != "********" || !item.Sensitive {
				t.Fatalf("secret item = %#v, want masked sensitive value", item)
			}
			break
		}
	}
	if !foundSecret {
		t.Fatal("ConfigItems missing access-key-secret")
	}

	if err := store.SetCurrentProfile("prod"); err != nil {
		t.Fatalf("SetCurrentProfile: %v", err)
	}
	if got := store.Current(); got != "prod" {
		t.Fatalf("Current() = %q, want prod", got)
	}
	if err := store.SetCurrentProfile(""); err == nil {
		t.Fatal("expected empty profile error")
	}
}

func TestResolveRegionForProfileUsesFallbackEnvironmentNames(t *testing.T) {
	missingAliyunPath := filepath.Join(t.TempDir(), "missing-aliyun.json")
	got, err := ResolveRegionForProfile("", "", filepath.Join(t.TempDir(), "missing.json"), func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return missingAliyunPath
		}
		if name == "ALIBABA_CLOUD_REGION_ID" {
			return "ap-southeast-1"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("ResolveRegionForProfile: %v", err)
	}
	if got != "ap-southeast-1" {
		t.Fatalf("region = %q, want ap-southeast-1", got)
	}
}

func TestResolveRegionForProfileReportsConfigAndMissingErrors(t *testing.T) {
	missingAliyunPath := filepath.Join(t.TempDir(), "missing-aliyun.json")
	brokenPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(brokenPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	getenv := func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return missingAliyunPath
		}
		return ""
	}
	if _, appErr := ResolveRegionForProfile("", "", brokenPath, getenv); appErr == nil || appErr.Payload().Code != "InvalidConfig" {
		t.Fatalf("broken config error = %#v, want InvalidConfig", appErr)
	}

	if _, appErr := ResolveRegionForProfile("", "", filepath.Join(t.TempDir(), "missing.json"), getenv); appErr == nil || appErr.Payload().Code != "MissingRegion" {
		t.Fatalf("missing region error = %#v, want MissingRegion", appErr)
	}
}

func TestValidRegionRejectsMalformedValues(t *testing.T) {
	for _, region := range []string{"cn", "cn-", "-hangzhou"} {
		t.Run(region, func(t *testing.T) {
			if ValidRegion(region) {
				t.Fatalf("ValidRegion(%q) = true, want false", region)
			}
		})
	}
}

func TestAliyunConfigPathUsesSupportedEnvironmentNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliyun.json")
	got := AliyunConfigPath(func(name string) string {
		if name == "ALIBABACLOUD_CONFIG_PATH" {
			return path
		}
		return ""
	})
	if got != path {
		t.Fatalf("AliyunConfigPath() = %q, want %q", got, path)
	}
}

func TestLoadStoreHandlesMissingAndNullAndRejectsInvalidFiles(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "new.json")
	store, err := LoadStore(missingPath)
	if err != nil {
		t.Fatalf("LoadStore missing: %v", err)
	}
	if got := store.Current(); got != DefaultProfileName {
		t.Fatalf("missing store current = %q", got)
	}

	nullPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(nullPath, []byte("null"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err = LoadStore(nullPath)
	if err != nil {
		t.Fatalf("LoadStore null: %v", err)
	}
	if got := store.Current(); got != DefaultProfileName {
		t.Fatalf("null store current = %q, want %q", got, DefaultProfileName)
	}

	brokenPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(brokenPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadStore(brokenPath); err == nil {
		t.Fatal("LoadStore succeeded for invalid JSON")
	}
}

func TestLoadExistingStoreReportsMissingAndInvalidFiles(t *testing.T) {
	if store, ok, err := LoadExistingStore(filepath.Join(t.TempDir(), "missing.json")); err != nil || ok || store != nil {
		t.Fatalf("LoadExistingStore missing = (%#v, %v, %v), want nil false nil", store, ok, err)
	}

	brokenPath := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(brokenPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := LoadExistingStore(brokenPath); err == nil {
		t.Fatal("LoadExistingStore succeeded for invalid JSON")
	}

	nullPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(nullPath, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, exists, err := LoadExistingStore(nullPath)
	if err != nil || !exists || store == nil {
		t.Fatalf("LoadExistingStore null = (%#v, %t, %v), want non-nil true nil", store, exists, err)
	}
	if got := store.Current(); got != DefaultProfileName {
		t.Fatalf("null existing store current = %q, want %q", got, DefaultProfileName)
	}
}

func TestEffectiveProfileTreatsTopLevelNullAsExistingEmptyStore(t *testing.T) {
	ecctlPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(ecctlPath, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, found, err := EffectiveProfile("", ecctlPath, filepath.Join(t.TempDir(), "missing-aliyun.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !found || profile.Name != DefaultProfileName || profile.Output != "json" {
		t.Fatalf("EffectiveProfile null = %#v found=%t", profile, found)
	}
}

func TestSaveReportsDirectoryError(t *testing.T) {
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "file")
	if err := os.WriteFile(parentFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := &Store{path: filepath.Join(parentFile, "config.json"), data: map[string]any{}}

	if err := store.Save(); err == nil {
		t.Fatal("Save succeeded when parent path is a file")
	}
}

func TestEffectiveProfileMergesAliyunAndEcctlProfiles(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	ecctlPath := filepath.Join(dir, "ecctl.json")
	writeConfig(t, aliyunPath, map[string]any{
		"current": "prod",
		"profiles": []any{map[string]any{
			"name":              "prod",
			"mode":              "AK",
			"access_key_id":     "aliyun-id",
			"access_key_secret": "aliyun-secret",
			"region_id":         "us-west-1",
		}},
	})
	writeConfig(t, ecctlPath, map[string]any{
		"current": "prod",
		"profiles": []any{map[string]any{
			"name":          "prod",
			"region_id":     "cn-hangzhou",
			"output_format": "text",
		}},
	})

	profile, found, err := EffectiveProfile("", ecctlPath, aliyunPath)
	if err != nil {
		t.Fatalf("EffectiveProfile: %v", err)
	}
	if !found {
		t.Fatal("EffectiveProfile found = false, want true")
	}
	if profile.AccessKeyID != "aliyun-id" || profile.Region != "cn-hangzhou" || profile.Output != "text" {
		t.Fatalf("merged profile = %#v", profile)
	}
}

func TestConfigValueFromProfileAliasesAndSecretDisplay(t *testing.T) {
	profile := Profile{AccessKeySecret: "secret", Output: "text"}

	secret, err := ConfigValueFromProfile(profile, "access_key_secret", true)
	if err != nil {
		t.Fatalf("ConfigValueFromProfile alias: %v", err)
	}
	if secret.Key != "access-key-secret" || secret.Value != "secret" || !secret.Sensitive {
		t.Fatalf("secret value = %#v", secret)
	}
	if _, err := ConfigValueFromProfile(profile, "missing", false); err == nil {
		t.Fatal("ConfigValueFromProfile succeeded for unknown key")
	}
}

func TestStoreGetAndSetValidateKeysAndValues(t *testing.T) {
	store := newStore(filepath.Join(t.TempDir(), "config.json"))

	if _, err := store.GetValue("", "missing", false); err == nil {
		t.Fatal("GetValue succeeded for unknown key")
	}
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "region", value: "not-a-real-region"},
		{key: "lang", value: "fr"},
		{key: "output", value: "table"},
		{key: "access-key-id", value: ""},
		{key: "missing", value: "value"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if _, err := store.SetValue("", tc.key, tc.value); err == nil {
				t.Fatalf("SetValue(%q, %q) succeeded", tc.key, tc.value)
			}
		})
	}

	value, err := store.SetValue("", "security-token", "token")
	if err != nil {
		t.Fatalf("SetValue security-token: %v", err)
	}
	if value.Value != "********" || !value.Sensitive {
		t.Fatalf("security-token display = %#v", value)
	}
	profile, ok := store.Profile("")
	if !ok || profile.Mode != "AK" || profile.SecurityToken != "token" {
		t.Fatalf("profile after security-token = %#v ok=%v", profile, ok)
	}
}

func TestUseProfileRequiresName(t *testing.T) {
	store := newStore(filepath.Join(t.TempDir(), "config.json"))

	if err := store.UseProfile(""); err == nil {
		t.Fatal("UseProfile succeeded with empty name")
	}
}

func TestEnsureShapeFiltersInvalidProfiles(t *testing.T) {
	store := &Store{data: map[string]any{
		"current":  12,
		"profiles": []any{"bad", map[string]any{"name": "prod", "region_id": "cn-hangzhou"}},
	}}

	profiles := store.profileMaps()
	if got := store.Current(); got != DefaultProfileName {
		t.Fatalf("Current() = %q, want default", got)
	}
	if len(profiles) != 1 || stringField(profiles[0], "name") != "prod" {
		t.Fatalf("profileMaps() = %#v", profiles)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "first", "second"); got != "first" {
		t.Fatalf("firstNonEmpty = %q, want first", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty empty = %q, want empty", got)
	}
}

func writeConfig(t *testing.T, path string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
