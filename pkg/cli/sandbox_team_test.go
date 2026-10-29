package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/e2bapi"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSandboxQuotaUpdateRequiresExplicitNumericFlagsBeforeProvider(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		flags      []string
	}{
		{"neither", "missing required parameters: --cpu-cores, --memory-gb", nil},
		{"memory missing", "missing required parameters: --memory-gb", []string{"--cpu-cores", "32"}},
		{"cpu missing", "missing required parameters: --cpu-cores", []string{"--memory-gb", "64"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
				calls++
				return &fakeAPICaller{}, nil
			}
			args := append([]string{"--lang", "en", "sandbox", "quota", "update", "team-1", "--region", "cn-beijing", "--output=json"}, tc.flags...)
			stdout, stderr, code := runCLIWith(factory, args...)
			if code != 1 || errorCode(t, stdout) != "MissingParameter" || errorMessage(t, stdout) != tc.want || calls != 0 || stderr != "" {
				t.Fatalf("missing Quota flags reached provider: code=%d stdout=%s stderr=%s calls=%d", code, stdout, stderr, calls)
			}
		})
	}
	zero := &fakeAPICaller{response: map[string]any{"code": "200", "quota": map[string]any{"tagValue": "team-1", "cpuCores": 0, "memoryGB": 0}}}
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return zero, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "quota", "update", "team-1", "--cpu-cores=0", "--memory-gb=0", "--region", "cn-beijing", "--output=json")
	if code != 0 || zero.operation != "UpdateQuota" || stderr != "" {
		t.Fatalf("explicit zero Quota flags were rejected: code=%d stdout=%s stderr=%s operation=%s", code, stdout, stderr, zero.operation)
	}
}

func TestFCSandboxTeamIsPublic(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"sandbox", "team", "--help"}, {"sbx", "team", "--help"}, {"schema", "--list", "sandbox"}, {"examples", "sandbox.team"}} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code != 0 || !strings.Contains(stdout, "team") {
			t.Fatalf("public command %v: exit %d %s %s", args, code, stdout, stderr)
		}
	}
}

func TestFCSandboxQuotaIsPublicAndUsesProfile(t *testing.T) {
	for _, args := range [][]string{{"sandbox", "quota", "--help"}, {"sbx", "quota", "--help"}, {"schema", "--list", "sandbox"}, {"examples", "sandbox.quota"}} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code != 0 || !strings.Contains(stdout, "quota") {
			t.Fatalf("public command %v: exit %d %s %s", args, code, stdout, stderr)
		}
	}
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"test","profiles":[{"name":"test","mode":"AK","region_id":"cn-beijing","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(key string) string {
		if key == "ECCTL_ALIYUN_CONFIG_PATH" {
			return aliyunPath
		}
		if key == "E2B_API_KEY" {
			return "test-e2b-key"
		}
		return ""
	}
	resource, err := spec.LoadResource("", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := defaultResourceCallerFactoryResolved(context.Background(), "test", filepath.Join(dir, "ecctl.json"), resource, config.ResolvedRegion{Value: "cn-hangzhou", Source: config.RegionSourceExplicit}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	fc, ok := caller.(*aliyun.OpenAPICaller)
	if !ok || fc.Product != "FCSandbox" || fc.Resource != "quota" || fc.Region != "cn-hangzhou" || fc.Profile.Mode != "AK" {
		t.Fatalf("quota must use regional Aliyun profile: %T %#v", caller, resource)
	}
}

func TestSandboxTeamCreateAcceptsGlobalFlagLayouts(t *testing.T) {
	for _, args := range [][]string{
		{"sandbox", "team", "create", "--lang", "en"},
		{"sbx", "team", "create", "--lang=en"},
		{"--lang", "en", "sandbox", "team", "create"},
		{"sandbox", "--lang=en", "team", "create"},
		{"sandbox", "team", "--lang", "en", "create"},
		{"--json", "--no-color=true", "sbx", "team", "--output=json", "create", "--lang=en"},
		{"--name=dev", "sandbox", "team", "create", "--lang=en"},
		{"--region=cn-hangzhou", "sbx", "--lang=en", "team", "create"},
		{"--profile=test", "sandbox", "team", "create", "--lang=en"},
		{"--lang", "--", "sandbox", "team", "create"},
		{"sandbox", "team", "--lang", "--", "create"},
		{"--lang=--", "sbx", "team", "create"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runCLI(append(args, "--help")...)
			if code != 0 || !strings.Contains(stdout, "Create an FC Agent Sandbox team") {
				t.Fatalf("public create parser %v: exit %d %s %s", args, code, stdout, stderr)
			}
		})
	}
}

func TestSandboxTeamProviderDispatch(t *testing.T) {
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"test","profiles":[{"name":"test","mode":"AK","region_id":"cn-beijing","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": aliyunPath, "E2B_API_KEY": "test-e2b-key", "E2B_API_URL": "https://api.e2b.app", "ECCTL_SANDBOX_BACKEND": "e2b"}
	getenv := func(key string) string { return env[key] }
	for _, name := range []string{"team", "sandbox", "template"} {
		resource, err := spec.LoadResource("", "sandbox", name)
		if err != nil {
			t.Fatal(err)
		}
		caller, err := defaultResourceCallerFactoryResolved(context.Background(), "test", filepath.Join(dir, "ecctl.json"), resource, config.ResolvedRegion{Value: "cn-hangzhou", Source: config.RegionSourceExplicit}, getenv)
		if err != nil {
			t.Fatalf("%s caller: %v", name, err)
		}
		if name == "team" {
			fc, ok := caller.(*aliyun.OpenAPICaller)
			if !ok || resource.Provider != "aliyun" || resource.Kind != "regional" || fc.Product != "FCSandbox" || fc.Resource != "team" || fc.Region != "cn-hangzhou" || fc.Profile.Mode != "AK" {
				t.Fatalf("Team must use regional profile OpenAPI despite E2B overrides: %T %#v", caller, resource)
			}
		} else if _, ok := caller.(*e2bapi.Caller); !ok || resource.Provider != "e2b" || resource.Kind != "global" {
			t.Fatalf("%s changed E2B transport: %T %#v", name, caller, resource)
		}
	}
	team, err := spec.LoadResource("", "sandbox", "team")
	if err != nil {
		t.Fatal(err)
	}
	env["ECCTL_ALIYUN_CONFIG_PATH"] = filepath.Join(dir, "missing-aliyun.json")
	if _, err := defaultResourceCallerFactoryResolved(context.Background(), "test", filepath.Join(dir, "missing.json"), team, config.ResolvedRegion{Value: "cn-hangzhou", Source: config.RegionSourceExplicit}, getenv); err == nil {
		t.Fatal("E2B credentials must not satisfy Team profile authentication")
	}
}
