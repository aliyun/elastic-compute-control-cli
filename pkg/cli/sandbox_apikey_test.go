package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestFCSandboxApiKeyPublicCommandSurface(t *testing.T) {
	for _, args := range [][]string{
		{"sandbox", "api-key", "--help"},
		{"sbx", "apikey", "--help"},
		{"sandbox", "api-key", "reset", "--help"},
		{"schema", "--list", "sandbox"},
	} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code != 0 || !strings.Contains(stdout, "api") {
			t.Fatalf("public api-key command %v: exit %d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
	}
}

func TestFCSandboxApiKeyUsesAliyunProfile(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"test","profiles":[{"name":"test","mode":"AK","region_id":"cn-beijing","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(key string) string {
		switch key {
		case "ECCTL_ALIYUN_CONFIG_PATH":
			return aliyunPath
		case "E2B_API_KEY":
			return "test-e2b-key"
		case "ECCTL_SANDBOX_BACKEND":
			return "e2b"
		}
		return ""
	}
	resource, err := spec.LoadResource("", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := defaultResourceCallerFactoryResolved(context.Background(), "test", filepath.Join(dir, "ecctl.json"), resource, config.ResolvedRegion{Value: "cn-hangzhou", Source: config.RegionSourceExplicit}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	fc, ok := caller.(*aliyun.OpenAPICaller)
	if !ok || fc.Product != "FCSandbox" || fc.Resource != "api-key" || fc.Region != "cn-hangzhou" || fc.Profile.Mode != "AK" {
		t.Fatalf("api-key route must ignore E2B overrides: %T %+v", caller, resource)
	}
}

func TestFCSandboxApiKeyShowValuePlumbing(t *testing.T) {
	keyResponse := map[string]any{"code": "200", "requestId": "req-1", "apiKey": map[string]any{
		"apiKeyID": "key-1", "apiKeyName": "dev", "apiKeyMask": "e2b_xxxx****xxxx", "teamID": "team-1", "status": "active",
	}}
	for _, tc := range []struct {
		name       string
		args       []string
		operation  string
		wantReveal any
	}{
		{"create default", []string{"sandbox", "api-key", "create", "--name", "dev", "--team-id", "team-1"}, "CreateApiKey", nil},
		{"create reveal", []string{"sandbox", "api-key", "create", "--name", "dev", "--team-id", "team-1", "--show-value"}, "CreateApiKey", true},
		{"reset default", []string{"sandbox", "api-key", "reset", "key-1"}, "ResetApiKey", nil},
		{"reset reveal", []string{"sandbox", "api-key", "reset", "key-1", "--show-value"}, "ResetApiKey", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeAPICaller{response: keyResponse}
			factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
				return fake, nil
			}
			args := append([]string{"--lang", "en"}, tc.args...)
			args = append(args, "--region", "cn-hangzhou", "--output=json")
			stdout, stderr, code := runCLIWith(factory, args...)
			if code != 0 || stderr != "" || fake.operation != tc.operation {
				t.Fatalf("%s: code=%d stdout=%s stderr=%s op=%s", tc.name, code, stdout, stderr, fake.operation)
			}
			if fake.request["ShowValue"] != tc.wantReveal {
				t.Fatalf("%s: ShowValue=%v want %v (request=%#v)", tc.name, fake.request["ShowValue"], tc.wantReveal, fake.request)
			}
			if !strings.Contains(stdout, `"api_key_mask"`) || strings.Contains(stdout, `"api_key_value"`) {
				t.Fatalf("%s: output must carry the mask and no plaintext field: %s", tc.name, stdout)
			}
		})
	}
}

func TestFCSandboxApiKeyRevealFlagRejectedOnReadOnlyOps(t *testing.T) {
	for _, args := range [][]string{
		{"sandbox", "api-key", "get", "key-1", "--show-value"},
		{"sandbox", "api-key", "list", "--show-value"},
		{"sandbox", "api-key", "update", "key-1", "--status", "inactive", "--show-value"},
	} {
		fake := &fakeAPICaller{response: map[string]any{"code": "200", "requestId": "req-1"}}
		factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
			return fake, nil
		}
		full := append([]string{"--lang", "en"}, args...)
		full = append(full, "--region", "cn-hangzhou", "--output=json")
		_, _, code := runCLIWith(factory, full...)
		if code == 0 {
			t.Fatalf("--show-value must be rejected on %v", args)
		}
	}
}
