package credentialenv

import (
	"slices"
	"testing"
)

func TestWithoutSensitivePreservesDesktopStateAndRemovesCredentials(t *testing.T) {
	env := []string{
		"DISPLAY=:1",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/bus",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET=secret",
		"OSS_SESSION_TOKEN=token",
		"ECCTL_CONFIG_PATH=/tmp/config.json",
	}
	cleaned := WithoutKeysForOS(env, "linux", SensitiveKeys...)
	if !slices.Contains(cleaned, "DISPLAY=:1") || !slices.Contains(cleaned, "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/bus") {
		t.Fatalf("desktop environment was removed: %#v", cleaned)
	}
	for _, item := range cleaned {
		if item == "ALIBABA_CLOUD_ACCESS_KEY_SECRET=secret" || item == "OSS_SESSION_TOKEN=token" || item == "ECCTL_CONFIG_PATH=/tmp/config.json" {
			t.Fatalf("credential environment remained: %q", item)
		}
	}
}

func TestWithoutKeysForOSMatchesWindowsKeysCaseInsensitively(t *testing.T) {
	cleaned := WithoutKeysForOS([]string{"Path=C:\\Windows", "alibaba_cloud_access_key_secret=secret"}, "windows", SensitiveKeys...)
	if len(cleaned) != 1 || cleaned[0] != "Path=C:\\Windows" {
		t.Fatalf("windows environment = %#v", cleaned)
	}
}

func TestWithoutSensitiveRemovesEveryCredentialsGoSelector(t *testing.T) {
	selectors := []string{
		"ALIBABA_CLOUD_ACCESS_KEY_ID",
		"ALIBABA_CLOUD_ACCESS_KEY_Id",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET",
		"ALIBABA_CLOUD_CREDENTIALS_FILE",
		"ALIBABA_CLOUD_CONFIG_FILE",
		"ALIBABA_CLOUD_CLI_PROFILE_DISABLED",
		"ALIBABA_CLOUD_CREDENTIALS_URI",
		"ALIBABA_CLOUD_ECS_IMDSV2_ENABLE",
		"ALIBABA_CLOUD_ECS_METADATA",
		"ALIBABA_CLOUD_ECS_METADATA_DISABLED",
		"ALIBABA_CLOUD_IMDSV1_DISABLED",
		"ALIBABA_CLOUD_OIDC_PROVIDER_ARN",
		"ALIBABA_CLOUD_OIDC_TOKEN_FILE",
		"ALIBABA_CLOUD_CONFIG_PATH",
		"ALIBABA_CLOUD_PROFILE",
		"ALIBABA_CLOUD_ROLE_ARN",
		"ALIBABA_CLOUD_ROLE_SESSION_NAME",
		"ALIBABA_CLOUD_SECURITY_TOKEN",
		"ALIBABA_CLOUD_STS_REGION",
		"ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED",
		"ECCTL_CONFIG_PATH",
		"ECCTL_PROFILE",
	}
	env := []string{"DISPLAY=:1"}
	for _, key := range selectors {
		env = append(env, key+"=sensitive-selector")
	}
	cleaned := WithoutKeysForOS(env, "linux", SensitiveKeys...)
	if len(cleaned) != 1 || cleaned[0] != "DISPLAY=:1" {
		t.Fatalf("selector environment remained: %#v", cleaned)
	}
}
