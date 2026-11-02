package aliyun

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestProfileModeInferenceThroughCredentialResolution(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "aliyun.json")
	cacheRoot := filepath.Join(dir, "private")
	for _, tt := range []struct {
		name, mode string
		profile    map[string]any
	}{
		{"AK", credentialModeAK, map[string]any{"access_key_id": "id", "access_key_secret": "secret"}},
		{"STS", credentialModeStsToken, map[string]any{"access_key_id": "id", "access_key_secret": "secret", "sts_token": "token"}},
		{"role", credentialModeRamRoleArn, map[string]any{"access_key_id": "id", "access_key_secret": "secret", "ram_role_arn": "acs:ram::1234567890123456:role/test"}},
		{"ECS", credentialModeEcsRamRole, map[string]any{"ram_role_name": "role"}},
		{"external", credentialModeExternal, map[string]any{"process_command": "credential-helper --json"}},
		{"URI", credentialModeCredentialsURI, map[string]any{"credentials_uri": "https://example.com/credentials"}},
		{"OIDC", credentialModeOIDC, map[string]any{"oidc_provider_arn": "acs:ram::1234567890123456:oidc-provider/test", "oidc_token_file": "token", "ram_role_arn": "acs:ram::1234567890123456:role/test"}},
		{"SSO", credentialModeCloudSSO, map[string]any{"cloud_sso_sign_in_url": "https://signin.example.com", "cloud_sso_account_id": "123", "cloud_sso_access_config": "ac-1"}},
		{"OAuth", credentialModeOAuth, map[string]any{"oauth_site_type": "CN"}},
		{"bearer", credentialModeBearerToken, map[string]any{"bearer_token": "token"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := resolveAliyunProfileCredentialWithCache(nil, tt.profile, "test", configPath, cacheRoot, mapGetenv(nil), map[string]bool{})
			if err != nil || resolved.Mode != tt.mode || resolved.Acquirer == nil {
				t.Fatalf("resolution = %+v, %v", resolved, err)
			}
		})
	}
}

func TestCredentialProfilesRejectIncompleteSourcesBeforeAcquisition(t *testing.T) {
	dir := t.TempDir()
	for _, profile := range []map[string]any{
		{}, {"mode": "unknown"}, {"mode": "AK"}, {"mode": "StsToken", "access_key_id": "id", "access_key_secret": "secret"},
		{"mode": "RamRoleArn"},
		{"mode": "ChainableRamRoleArn"}, {"mode": "ChainableRamRoleArn", "source_profile": "absent"},
		{"mode": "OIDC"},
		{"mode": "OIDC", "oidc_provider_arn": "invalid", "oidc_token_file": "token", "ram_role_arn": "acs:ram::1234567890123456:role/test"},
		{"mode": "OIDC", "oidc_provider_arn": "acs:ram::1234567890123456:oidc-provider/test", "oidc_token_file": "token", "ram_role_arn": "invalid"},
		{"mode": "OIDC", "oidc_provider_arn": "acs:ram::2109876543210987:oidc-provider/test", "oidc_token_file": "token", "ram_role_arn": "acs:ram::1234567890123456:role/test"},
		{"mode": "OAuth", "oauth_site_type": "invalid"},
		{"mode": "CloudSSO", "cloud_sso_sign_in_url": "http://example.com"},
		{"mode": "CloudSSO", "cloud_sso_sign_in_url": "https://example.com"},
		{"mode": "CredentialsURI", "credentials_uri": "http://example.com"},
		{"mode": "External", "process_command": ""}, {"mode": "BearerToken"},
	} {
		if _, err := resolveAliyunProfileCredentialWithCache(nil, profile, "test", filepath.Join(dir, "aliyun.json"), filepath.Join(dir, "private"), mapGetenv(nil), map[string]bool{}); err == nil {
			t.Fatalf("incomplete profile accepted: %#v", profile)
		}
	}
}

func TestCredentialCommandEscapesRemainArguments(t *testing.T) {
	for _, tt := range []struct {
		command, goos string
		want          []string
	}{
		{"cmd \"a\\\"b\"", "linux", []string{"cmd", "a\"b"}},
		{"cmd \"a\\$b\\`c\\\\d\"", "linux", []string{"cmd", "a$b`c\\d"}},
		{"cmd \"a\\\nb\"", "linux", []string{"cmd", "ab"}},
		{"cmd a\\\nb", "linux", []string{"cmd", "ab"}},
		{"cmd \"a\\qb\"", "linux", []string{"cmd", "a\\qb"}},
		{"cmd a\\ b", "linux", []string{"cmd", "a b"}},
		{"  cmd   arg  ", "windows", []string{"cmd", "arg"}},
		{`cmd a\\\"b`, "windows", []string{"cmd", `a\"b`}},
	} {
		got, err := splitCredentialProcessCommand(tt.command, tt.goos)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("split %q (%s) = %#v, %v; want %#v", tt.command, tt.goos, got, err, tt.want)
		}
	}
	for _, input := range []string{"", "  ", "''", `""`, `cmd 'unterminated`, `cmd "unterminated`, "cmd trailing\\"} {
		if _, err := splitCredentialProcessCommand(input, "linux"); err == nil {
			t.Fatalf("invalid argv accepted: %q", input)
		}
	}
}
