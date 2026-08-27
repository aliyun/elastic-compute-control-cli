//go:build windows

package aliyun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
	"golang.org/x/sys/windows"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

func TestCredentialBrokerProfileUsesRestrictedWindowsDACL(t *testing.T) {
	broker, err := startCredentialBroker(context.Background(), &staticCredentialAcquirer{snapshot: credentialSnapshot{
		AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "sts", Type: "sts",
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	if _, err := broker.CommandArgs([]string{"ossutil", "version"}, "cn-hangzhou"); err != nil {
		t.Fatal(err)
	}
	requireRestrictedWindowsCredentialFile(t, broker.config)
}

func TestCLICommandTempConfigUsesRestrictedWindowsDACL(t *testing.T) {
	path, cleanup, err := writeTempConfig(map[string]any{
		"current": "default", "profiles": []any{map[string]any{
			"name": "default", "mode": "AK", "access_key_id": "id", "access_key_secret": "secret",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	requireRestrictedWindowsCredentialFile(t, path)
}

func TestCredentialCacheUsesRestrictedWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials-v2", "entry.json")
	if err := storeCredentialCacheEntry(context.Background(), path, credentialCacheEntry{
		Mode: credentialModeOAuth, SourceGeneration: "generation", OAuthRefreshToken: "refresh",
	}); err != nil {
		t.Fatal(err)
	}
	requireRestrictedWindowsCredentialFile(t, path)
}

func TestOAuthFirstRefreshCreatesRestrictedWindowsCacheRoot(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "aliyun.json")
	profile := map[string]any{
		"name": "oauth", "mode": "OAuth", "oauth_site_type": "CN", "oauth_refresh_token": "refresh-before",
	}
	writeJSONFile(t, configPath, map[string]any{"current": "oauth", "profiles": []any{profile}})
	cacheRoot := filepath.Join(dir, "credentials-v2")
	if _, err := os.Stat(cacheRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache root unexpectedly exists: %v", err)
	}
	provider, err := newOAuthProfileCredentialsProvider(profile, "oauth", configPath, cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	originalRefresh := refreshOAuthCredential
	refreshOAuthCredential = func(context.Context, map[string]any, credentialHTTPClient, oauthTokenCommitFunc) (*credentialproviders.Credentials, *oauthCredentialProfileUpdate, error) {
		expiration := time.Now().Add(time.Hour).Unix()
		return &credentialproviders.Credentials{AccessKeyId: "id", AccessKeySecret: "secret", SecurityToken: "sts"}, &oauthCredentialProfileUpdate{
			refreshToken: "refresh-after", accessToken: "access-after", accessTokenExpire: expiration,
			accessKeyID: "id", accessKeySecret: "secret", securityToken: "sts", stsExpire: expiration,
		}, nil
	}
	t.Cleanup(func() { refreshOAuthCredential = originalRefresh })
	if _, err := provider.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := configfile.ValidatePrivateFile(provider.cachePath); err != nil {
		t.Fatalf("first OAuth refresh cache is not private: %v", err)
	}
}

func requireRestrictedWindowsCredentialFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("credential temporary file DACL is not protected: %s, err=%v", descriptor.String(), err)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatalf("credential temporary file owner = %v, want %s, err=%v", owner, user.User.Sid, err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("credential DACL = %v, err=%v, want exactly two ACEs", dacl, err)
	}
	foundUser, foundSystem := false, false
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			t.Fatal(err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("credential DACL contains a non-allow ACE: %s", descriptor.String())
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(user.User.Sid):
			foundUser = true
		case sid.Equals(system):
			foundSystem = true
		default:
			t.Fatalf("credential DACL grants an unexpected principal %s: %s", sid, descriptor.String())
		}
	}
	if !foundUser || !foundSystem {
		t.Fatalf("credential DACL does not contain current user and SYSTEM: %s", descriptor.String())
	}
}
