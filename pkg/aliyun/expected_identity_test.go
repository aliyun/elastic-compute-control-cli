package aliyun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestExpectedIdentityRejectsChangedStaticPrincipalBeforeAPI(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"current":"ci","profiles":[{"name":"ci","mode":"AK"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pin, _ := json.Marshal(map[string]any{"version": 1, "AccountId": "1234567890123456", "IdentityType": "RAMUser", "UserId": "user-a"})
	env := map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": filepath.Join(dir, "absent.json"), "ALIBABA_CLOUD_ACCESS_KEY_ID": "dummy-ak-b", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "dummy-secret-b", "ECCTL_EXPECTED_CREDENTIAL_IDENTITY": string(pin)}
	old := resolveCredentialSnapshotIdentity
	defer func() { resolveCredentialSnapshotIdentity = old }()
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		return canonicalIdentity(map[string]any{"AccountId": "1234567890123456", "IdentityType": "RAMUser", "UserId": "user-b"})
	}
	caller, err := NewOpenAPICaller("ci", config, "FCSandbox", "cn-hangzhou", func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{response: `{"code":"200","templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}`}
	caller.Resource = "template"
	caller.executor = fake
	_, err = caller.CallRaw(context.Background(), "GetTemplate", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
	if err == nil || len(fake.requests) != 0 {
		t.Fatalf("same named profile changed principal before API: %v requests=%d", err, len(fake.requests))
	}
}

func TestExpectedIdentityRejectsMalformedInputWithoutEcho(t *testing.T) {
	for _, raw := range []string{`{`, `{"version":1,"AccountId":"1234567890123456","IdentityType":"Account","SecurityToken":"dummy-secret"}`, `{"version":1,"AccountId":"other","IdentityType":"Account"}`, `{"version":1,"AccountId":"1234567890123456","IdentityType":"Unsupported"}`} {
		caller, err := NewOpenAPICaller("", filepath.Join(t.TempDir(), "unused"), "FCSandbox", "cn-hangzhou", func(name string) string {
			if name == expectedCredentialIdentityEnv {
				return raw
			}
			return ""
		})
		if err == nil || caller != nil || strings.Contains(err.Error(), "dummy-secret") {
			t.Errorf("invalid expectation did not fail safely: %v", err)
		}
	}
}

func TestExpectedIdentityCanonicalAccountUserAndRole(t *testing.T) {
	for _, pin := range []expectedCredentialIdentity{{Version: 1, AccountID: "1234567890123456", Type: "Account"}, {Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}, {Version: 1, AccountID: "1234567890123456", Type: "AssumedRoleUser", RoleID: "role-a"}} {
		body, _ := json.Marshal(pin)
		parsed, hash, err := parseExpectedCredentialIdentity(string(body))
		if err != nil {
			t.Fatal(err)
		}
		profile := resolvedOpenAPIProfile{ExpectedAccountID: parsed.AccountID, ExpectedIdentityType: parsed.Type, ExpectedIdentityHash: hash}
		expected := map[string]any{"AccountId": pin.AccountID, "IdentityType": pin.Type, "UserId": pin.UserID, "RoleId": pin.RoleID, "PrincipalId": "role-a:new-session"}
		fake := &fakeOpenAPIExecutor{response: string(mustIdentityJSON(t, expected))}
		if _, err := resolveCredentialIdentityWithExecutor(context.Background(), profile, fake, "sts.aliyuncs.com"); err != nil {
			t.Fatal(err)
		}
		expected["AccountId"] = "6543210987654321"
		fake.response = string(mustIdentityJSON(t, expected))
		if _, err := resolveCredentialIdentityWithExecutor(context.Background(), profile, fake, "sts.aliyuncs.com"); err == nil {
			t.Fatal("account switch accepted")
		}
		if pin.Type != "Account" {
			expected["AccountId"] = pin.AccountID
			if pin.Type == "RAMUser" {
				expected["UserId"] = "user-b"
			} else {
				expected["RoleId"] = "role-b"
			}
			fake.response = string(mustIdentityJSON(t, expected))
			if _, err := resolveCredentialIdentityWithExecutor(context.Background(), profile, fake, "sts.aliyuncs.com"); err == nil {
				t.Fatal("stable user/role switch accepted")
			}
		}
	}
}

func mustIdentityJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
