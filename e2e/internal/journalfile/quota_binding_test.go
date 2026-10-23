package journalfile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestQuotaBindingFreezesSelectedProfileConfigAndPrincipal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	for _, path := range []string{ecctlPath, aliyunPath} {
		if err := os.WriteFile(path, []byte(`{"current":"profile-a","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"key-a"},{"name":"profile-b","mode":"AK","access_key_id":"key-b"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in *"--profile profile-a"*) ;; *) exit 90;; esac
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + ecctlPath, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyunPath}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("profile-a", "AK", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Env: append(env, "ECCTL_PROFILE=profile-b")}
	bound, err := ProbeQuotaBinding(context.Background(), binding, cfg)
	if err != nil || bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
		t.Fatalf("unrelated current-profile switch lost selected identity: bound=%+v err=%v", bound, err)
	}
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"key-a"},{"name":"profile-b","mode":"AK","access_key_id":"key-b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeQuotaBinding(context.Background(), binding, cfg); err != nil {
		t.Fatalf("current-profile switch changed selected binding: %v", err)
	}
	changedPrincipal := filepath.Join(dir, "ecctl-other")
	if err := os.WriteFile(changedPrincipal, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-b"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeQuotaBinding(context.Background(), binding, execpkg.Config{Bin: changedPrincipal, Env: env}); err == nil {
		t.Fatal("changed trusted principal was accepted")
	}
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"key-changed"},{"name":"profile-b","mode":"AK","access_key_id":"key-b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeQuotaBinding(context.Background(), binding, cfg); err == nil {
		t.Fatal("selected auth configuration edit was accepted")
	}
	if err := ValidateQuotaBinding(binding, append(env, "ECCTL_ALIYUN_CONFIG_PATH="+filepath.Join(dir, "other.json"))); err == nil {
		t.Fatal("selected configuration target changed without rejection")
	}
}

func TestQuotaProbeUsesEffectiveModeAcrossConfigureDisplayVariants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, tc := range []struct {
		name, aliyunProfile, nativeProfile, displayMode string
	}{
		{"Aliyun mode alias", `"mode":" aK ","access_key_id":"public-a","access_key_secret":"dummy-secret"`, "", " aK "},
		{"native static override", `"mode":"RamRoleArn","ram_role_arn":"acs:ram::1234567890123456:role/owned"`, `"access_key_id":"public-native","access_key_secret":"dummy-secret"`, "RamRoleArn"},
		{"inferred mode", `"access_key_id":"public-a","access_key_secret":"dummy-secret"`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			aliyun := filepath.Join(dir, "aliyun.json")
			native := filepath.Join(dir, "native.json")
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned",`+tc.aliyunProfile+`}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.nativeProfile != "" {
				if err := os.WriteFile(native, []byte(`{"profiles":[{"name":"owned",`+tc.nativeProfile+`}]}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(dir, "calls")
			bin := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_QUOTA_PROBE_LOG"
case "$*" in
  *"configure get"*) printf '{"profile":"%s","mode":"%s"}\n' "${FAKE_QUOTA_DISPLAY_PROFILE:-owned}" "$FAKE_QUOTA_DISPLAY_MODE";;
  *"call sts GetCallerIdentity"*)
    [ -n "$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" ] || exit 92
    printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "${FAKE_QUOTA_USER:-user-a}";;
  *) exit 91;;
esac
`), 0o755); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun,
				"FAKE_QUOTA_DISPLAY_MODE=" + tc.displayMode, "FAKE_QUOTA_PROBE_LOG=" + logPath}
			mode, err := ResolveQuotaCredentialMode("owned", native, aliyun, env)
			if err != nil || mode != "AK" {
				t.Fatalf("effective mode=%q, want AK: %v", mode, err)
			}
			identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
			binding, err := CaptureQuotaBinding("owned", mode, identity, env)
			if err != nil {
				t.Fatal(err)
			}
			binding.ResolvedMode = true
			cfg := execpkg.Config{Bin: bin, Env: env}
			bound, err := ProbeQuotaBinding(context.Background(), binding, cfg)
			if err != nil || bound.Profile != "owned" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
				t.Fatalf("unchanged effective mode rejected: bound=%+v err=%v", bound, err)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil || strings.Count(string(calls), "configure get") != 1 || strings.Count(string(calls), "GetCallerIdentity") != 1 {
				t.Fatalf("protected probe did not check profile then trusted STS: calls=%q err=%v", calls, err)
			}
			if tc.name == "inferred mode" {
				changed := cfg
				changed.Env = append(append([]string{}, env...), "ALIBABA_CLOUD_SECURITY_TOKEN=dummy-token")
				if actual, err := ResolveQuotaCredentialMode("owned", native, aliyun, changed.Env); err != nil || actual != "StsToken" {
					t.Fatalf("fixture did not change effective mode: %q %v", actual, err)
				}
				if _, err := ProbeQuotaBinding(context.Background(), binding, changed); err == nil {
					t.Fatal("changed effective mode from credential environment was accepted")
				}
				after, err := os.ReadFile(logPath)
				if err != nil || string(after) != string(calls) {
					t.Fatalf("changed effective mode reached probe subprocess: before=%q after=%q err=%v", calls, after, err)
				}
				return
			}
			if tc.name != "Aliyun mode alias" {
				return
			}
			legacy := *binding
			legacy.ResolvedMode = false
			if _, err := ProbeQuotaBinding(context.Background(), &legacy, cfg); err == nil {
				t.Fatal("legacy unresolved binding accepted a mismatched display mode")
			}
			calls, err = os.ReadFile(logPath)
			if err != nil || strings.Count(string(calls), "configure get") != 2 || strings.Count(string(calls), "GetCallerIdentity") != 1 {
				t.Fatalf("legacy display mismatch reached STS: calls=%q err=%v", calls, err)
			}
			exact := cfg
			exact.Env = append(append([]string{}, env...), "FAKE_QUOTA_DISPLAY_MODE=AK")
			if _, err := ProbeQuotaBinding(context.Background(), &legacy, exact); err != nil {
				t.Fatalf("legacy exact display mode rejected: %v", err)
			}
			wrongProfile := cfg
			wrongProfile.Env = append(append([]string{}, env...), "FAKE_QUOTA_DISPLAY_PROFILE=other")
			if _, err := ProbeQuotaBinding(context.Background(), binding, wrongProfile); err == nil {
				t.Fatal("configure get changed the selected profile without rejection")
			}
			wrongPrincipal := cfg
			wrongPrincipal.Env = append(append([]string{}, env...), "FAKE_QUOTA_USER=user-b")
			if _, err := ProbeQuotaBinding(context.Background(), binding, wrongPrincipal); err == nil {
				t.Fatal("trusted STS principal changed without rejection")
			}
			wrongTarget := cfg
			wrongTarget.Env = append(append([]string{}, env...), "ECCTL_ALIYUN_CONFIG_PATH="+filepath.Join(dir, "other.json"))
			beforeTarget, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ProbeQuotaBinding(context.Background(), binding, wrongTarget); err == nil {
				t.Fatal("selected configuration target changed without rejection")
			}
			afterTarget, err := os.ReadFile(logPath)
			if err != nil || string(afterTarget) != string(beforeTarget) {
				t.Fatalf("changed target reached probe subprocess: before=%q after=%q err=%v", beforeTarget, afterTarget, err)
			}
			changed := []byte(`{"profiles":[{"name":"owned","mode":"StsToken","access_key_id":"public-a","access_key_secret":"dummy-secret","sts_token":"dummy-token"}]}`)
			if err := os.WriteFile(aliyun, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if actual, err := ResolveQuotaCredentialMode("owned", native, aliyun, env); err != nil || actual != "StsToken" {
				t.Fatalf("fixture did not change effective mode: %q %v", actual, err)
			}
			before, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ProbeQuotaBinding(context.Background(), binding, cfg); err == nil {
				t.Fatal("actual credential mode change was accepted")
			}
			after, err := os.ReadFile(logPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("changed effective mode reached probe subprocess: before=%q after=%q err=%v", before, after, err)
			}
		})
	}
}

func TestQuotaBindingAcceptsNewNativeCurrentWithoutSelectedAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity probe fixture uses a shell script")
	}
	dir := t.TempDir()
	native := filepath.Join(dir, "ecctl.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	aliyunA := []byte(`{"current":"profile-a","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"public-a"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`)
	if err := os.WriteFile(aliyun, aliyunA, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("profile-a", "AK", identity, env)
	if err != nil || binding.ConfigState["ecctl"].Exists {
		t.Fatalf("initial native config was not absent: binding=%+v err=%v", binding, err)
	}
	// SetCurrentProfile("profile-b") on a missing native store saves the
	// default settings and B's settings with the new current selector.
	nativeB := []byte(`{"current":"profile-b","profiles":[{"name":"default","output_format":"json"},{"name":"profile-b","output_format":"json"}]}`)
	if err := os.WriteFile(native, nativeB, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "ecctl")
	t.Setenv("FAKE_QUOTA_USER", "user-a")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"--profile profile-a"*) ;;
  *) exit 90;;
esac
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "$FAKE_QUOTA_USER";;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou", Env: env}
	bound, err := ProbeQuotaBinding(context.Background(), binding, cfg)
	if err != nil || bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
		t.Fatalf("safe current switch lost selected profile: bound=%+v err=%v", bound, err)
	}
	if err := os.Chmod(native, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, env); err == nil {
		t.Fatal("unsafe new native file permissions were accepted")
	}
	if err := os.Chmod(native, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"override"},{"name":"profile-b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, env); err == nil {
		t.Fatal("new native selected-profile auth override was accepted")
	}
	if err := os.WriteFile(native, nativeB, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		[]byte(`{"current":"profile-b","profiles":[{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`),
		[]byte(`{"current":"profile-b","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"changed"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`),
	} {
		if err := os.WriteFile(aliyun, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateQuotaBinding(binding, env); err == nil {
			t.Fatal("selected Aliyun A deletion or auth change was accepted")
		}
	}
	if err := os.WriteFile(aliyun, aliyunA, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, append(env, "ECCTL_CONFIG_PATH="+filepath.Join(dir, "other.json"))); err == nil {
		t.Fatal("native target change was accepted")
	}
	t.Setenv("FAKE_QUOTA_USER", "user-b")
	if _, err := ProbeQuotaBinding(context.Background(), binding, cfg); err == nil {
		t.Fatal("trusted principal drift was accepted")
	}
}

func TestPrepareQuotaCleanupPreservesOrdinaryLegacyArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	for _, tc := range []struct {
		name      string
		entry     report.Resource
		wantError bool
	}{
		{"ordinary", report.Resource{Scope: "stack", Teardown: "ecctl t thing delete ordinary"}, false},
		{"protected marker", report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete owned --timeout 5m", TeamCreateConfirmed: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal([]report.Resource{tc.entry})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = PrepareQuotaCleanup(context.Background(), path, report.CleanupJournal{}, execpkg.Config{})
			if (err != nil) != tc.wantError {
				t.Fatalf("legacy Quota preflight err=%v", err)
			}
		})
	}
}

func TestQuotaProbeUsesOriginalSelectorsAlongsideTemplateBinding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", "")
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", "")
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("profile-a", "AK", identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin}
	templateBound := (TemplateCreateIntent{ConfigPaths: paths, Identity: identity}).BoundConfig(cfg)
	if err := ValidateQuotaBinding(binding, templateBound.Env); err == nil {
		t.Fatal("test did not establish the rewritten-selector regression")
	}
	if _, err := ProbeQuotaBinding(context.Background(), binding, cfg); err != nil {
		t.Fatalf("unchanged original configuration was rejected: %v", err)
	}
}

func TestQuotaBindingRejectsCloudSSOOriginBeforeProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("provider marker uses a shell script")
	}
	dir := t.TempDir()
	aliyun := filepath.Join(dir, "aliyun.json")
	native := filepath.Join(dir, "native.json")
	write := func(origin, token string) {
		t.Helper()
		body := `{"profiles":[{"name":"owned","mode":"CloudSSO","cloud_sso_sign_in_url":"` + origin + `/login?private=never-journal","cloud_sso_account_id":"account-a","cloud_sso_access_config":"access-a","access_token":"` + token + `"}]}`
		if err := os.WriteFile(aliyun, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("https://original.example", "token-a")
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("owned", "CloudSSO", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "token-a") || strings.Contains(string(body), "never-journal") || strings.Contains(string(body), "/login") {
		t.Fatalf("CloudSSO secret URL content entered binding: %s", body)
	}
	write("https://original.example", "token-b")
	if err := ValidateQuotaBinding(binding, env); err != nil {
		t.Fatalf("normal token renewal changed source: %v", err)
	}
	for _, changed := range []string{
		`{"profiles":[{"name":"owned","mode":"CloudSSO","cloud_sso_sign_in_url":"https://original.example/login","cloud_sso_account_id":"account-b","cloud_sso_access_config":"access-a"}]}`,
		`{"profiles":[{"name":"owned","mode":"CloudSSO","cloud_sso_sign_in_url":"https://original.example/login","cloud_sso_account_id":"account-a","cloud_sso_access_config":"access-b"}]}`,
	} {
		if err := os.WriteFile(aliyun, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateQuotaBinding(binding, env); err == nil {
			t.Fatal("changed CloudSSO account or access configuration was accepted")
		}
	}
	write("https://changed.example", "token-b")
	marker := filepath.Join(dir, "provider-called")
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeQuotaBinding(context.Background(), binding, execpkg.Config{Bin: bin, Env: env}); err == nil {
		t.Fatal("changed CloudSSO origin was accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("provider launched before source rejection: %v", err)
	}
}

func TestQuotaBindingTracksSourceProfileChain(t *testing.T) {
	dir := t.TempDir()
	aliyun := filepath.Join(dir, "aliyun.json")
	native := filepath.Join(dir, "native.json")
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	write := func(profiles string) {
		t.Helper()
		if err := os.WriteFile(aliyun, []byte(`{"profiles":[`+profiles+`]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const owned = `{"name":"owned","mode":"ChainableRamRoleArn","source_profile":"middle","ram_role_arn":"acs:ram::1234567890123456:role/owned"}`
	const middle = `{"name":"middle","mode":"ChainableRamRoleArn","source_profile":"base","ram_role_arn":"acs:ram::1234567890123456:role/middle"}`
	const base = `{"name":"base","mode":"AK","access_key_id":"public-key","access_key_secret":"secret-never-journal"}`
	write(owned + `,` + middle + `,` + base)
	binding, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env)
	if err != nil || len(binding.SourceProfiles) != 2 {
		t.Fatalf("source chain capture failed: %+v %v", binding, err)
	}
	body, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret-never-journal") {
		t.Fatalf("source secret entered binding: %s", body)
	}
	var recovered report.QuotaCredentialBinding
	if err := json.Unmarshal(body, &recovered); err != nil || len(recovered.SourceProfiles) != 2 {
		t.Fatalf("journal round trip lost source chain: %+v %v", recovered, err)
	}
	write(owned + `,` + middle + `,` + base + `,{"name":"unrelated","mode":"External","process_command":"unrelated-command"}`)
	if err := ValidateQuotaBinding(&recovered, env); err != nil {
		t.Fatalf("unrelated profile changed binding: %v", err)
	}
	write(owned + `,` + middle + `,{"name":"base","mode":"AK","access_key_id":"public-key","access_key_secret":"renewed-secret"}`)
	if err := ValidateQuotaBinding(&recovered, env); err != nil {
		t.Fatalf("source secret renewal changed binding: %v", err)
	}
	for _, changed := range []string{
		owned + `,` + middle,
		owned + `,` + middle + `,{"name":"base","mode":"External","process_command":"new-command"}`,
		owned + `,` + middle + `,{"name":"base","mode":"AK","access_key_id":"changed"}`,
		owned + `,` + middle + `,{"name":"base","mode":"AK","access_key_id":"public-key"}`,
		owned + `,` + middle + `,` + base + `,` + base,
		owned + `,{"name":"middle","mode":"ChainableRamRoleArn","source_profile":"owned"},` + base,
	} {
		write(changed)
		if err := ValidateQuotaBinding(&recovered, env); err == nil {
			t.Fatalf("changed source chain was accepted: %s", changed)
		}
	}
	if runtime.GOOS != "windows" {
		write(owned + `,` + middle + `,{"name":"base","mode":"External","process_command":"new-command"}`)
		marker := filepath.Join(dir, "source-provider-called")
		bin := filepath.Join(dir, "ecctl")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := ProbeQuotaBinding(context.Background(), &recovered, execpkg.Config{Bin: bin, Env: env}); err == nil {
			t.Fatal("changed source profile was accepted before provider")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("provider launched before source rejection: %v", err)
		}
	}
	write(owned + `,` + middle + `,` + base + `,` + base)
	if _, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env); err == nil {
		t.Fatal("duplicate source profile was captured")
	}
	write(owned + `,{"name":"middle","mode":"ChainableRamRoleArn","source_profile":"owned"},` + base)
	if _, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env); err == nil {
		t.Fatal("cyclic source chain was captured")
	}
	write(owned + `,` + middle + `,{"name":"base","mode":"External","process_command":"private-command"}`)
	external, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env)
	if err != nil {
		t.Fatalf("supported External source could not be bound: %v", err)
	}
	body, err = json.Marshal(external)
	if err != nil || strings.Contains(string(body), "private-command") {
		t.Fatalf("source command entered binding: %s %v", body, err)
	}
	if err := ValidateQuotaBinding(external, env); err != nil {
		t.Fatalf("unchanged External source was rejected: %v", err)
	}
	padded := strings.Replace(middle, `"source_profile":"base"`, `"source_profile":" base "`, 1)
	write(owned + `,` + padded + `,` + base)
	paddedBinding, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env)
	if err != nil || len(paddedBinding.SourceProfiles) != 2 || paddedBinding.SourceProfiles["base"].AuthSelectors["access_key_id"] != "public-key" {
		t.Fatalf("padded source link did not resolve base: %+v %v", paddedBinding, err)
	}
	if err := ValidateQuotaBinding(paddedBinding, env); err != nil {
		t.Fatalf("unchanged padded source link was rejected: %v", err)
	}
	write(owned + `,` + middle + `,` + base)
	if err := ValidateQuotaBinding(paddedBinding, env); err == nil {
		t.Fatal("raw source selector change was hidden by traversal normalization")
	}
	write(owned + `,{"name":"middle","mode":"ChainableRamRoleArn","source_profile":"   "},` + base)
	if _, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env); err == nil {
		t.Fatal("whitespace-only source link was accepted")
	}
	write(owned + `,{"name":"middle","mode":"ChainableRamRoleArn","source_profile":" owned "},` + base)
	if _, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env); err == nil {
		t.Fatal("padded source cycle was accepted")
	}
}

func TestQuotaBindingPermitsOAuthTokenRenewal(t *testing.T) {
	dir := t.TempDir()
	aliyun := filepath.Join(dir, "aliyun.json")
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + filepath.Join(dir, "native.json"), "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun}
	write := func(token string) {
		t.Helper()
		body := `{"profiles":[{"name":"owned","mode":"OAuth","oauth_site_type":"International","oauth_generation":"generation-a","oauth_account_id":"1234567890123456","oauth_refresh_token":"` + token + `"}]}`
		if err := os.WriteFile(aliyun, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("token-a")
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("owned", "OAuth", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	write("token-b")
	if err := ValidateQuotaBinding(binding, env); err != nil {
		t.Fatalf("normal OAuth token renewal changed binding: %v", err)
	}
}

func TestQuotaCredentialModeUsesConfiguredInferenceWithoutPersistingSecrets(t *testing.T) {
	dir := t.TempDir()
	native := filepath.Join(dir, "native.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	for _, tc := range []struct {
		name, fields, want string
	}{
		{"inferred AK", `"access_key_id":"public-a","access_key_secret":"dummy-private"`, "AK"},
		{"inferred STS", `"access_key_id":"public-a","access_key_secret":"dummy-private","sts_token":"dummy-token"`, "StsToken"},
		{"inferred role", `"access_key_id":"public-a","access_key_secret":"dummy-private","ram_role_arn":"acs:ram::1234567890123456:role/owned"`, "RamRoleArn"},
		{"explicit", `"mode":"EcsRamRole","ram_role_name":"owned-role"`, "EcsRamRole"},
		{"unconfigured", `"region_id":"cn-hangzhou"`, ""},
		{"invalid", `"mode":"UnknownMode","access_key_id":"public-a","access_key_secret":"dummy-private"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned",`+tc.fields+`}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			mode, err := ResolveQuotaCredentialMode("owned", native, aliyun, env)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("unconfigured/invalid mode resolved to %q", mode)
				}
				return
			}
			if err != nil || mode != tc.want {
				t.Fatalf("resolved mode=%q want=%q err=%v", mode, tc.want, err)
			}
			binding, err := CaptureQuotaBinding("owned", mode, identity, env)
			if err != nil {
				t.Fatal(err)
			}
			binding.ResolvedMode = true
			body, err := json.Marshal(binding)
			if err != nil || strings.Contains(string(body), "dummy-private") || strings.Contains(string(body), "dummy-token") {
				t.Fatalf("secret entered inferred-mode binding: %s %v", body, err)
			}
			if err := ValidateQuotaBinding(binding, env); err != nil {
				t.Fatalf("unchanged inferred mode rejected: %v", err)
			}
		})
	}
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned","access_key_id":"public-a","access_key_secret":"dummy-private"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, err := CaptureQuotaBinding("owned", "AK", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	binding.ResolvedMode = true
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned","access_key_id":"public-a","access_key_secret":""}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, env); err == nil {
		t.Fatal("inferred mode change hidden by unchanged secret-field presence")
	}
}

func TestQuotaEffectiveStaticTokenModeDriftStopsBeforeProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, source := range []struct{ name, native, aliyun string }{
		{"native override", `"access_key_id":"native-id","access_key_secret":"native-secret"`, `"mode":"RamRoleArn","ram_role_arn":"role"`},
		{"explicit Aliyun AK", "", `"mode":"AK","access_key_id":"aliyun-id","access_key_secret":"aliyun-secret"`},
		{"inferred Aliyun AK", "", `"access_key_id":"aliyun-id","access_key_secret":"aliyun-secret"`},
	} {
		for _, alias := range []string{"ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN"} {
			for _, initiallyPresent := range []bool{false, true} {
				name := source.name + "/" + alias + "/absent-to-present"
				if initiallyPresent {
					name = source.name + "/" + alias + "/present-to-absent"
				}
				t.Run(name, func(t *testing.T) {
					for _, tokenName := range []string{"ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN"} {
						t.Setenv(tokenName, "")
					}
					dir := t.TempDir()
					native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
					if source.native != "" {
						if err := os.WriteFile(native, []byte(`{"profiles":[{"name":"owned",`+source.native+`}]}`), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned",`+source.aliyun+`}]}`), 0o600); err != nil {
						t.Fatal(err)
					}
					logPath, bin := filepath.Join(dir, "calls"), filepath.Join(dir, "ecctl")
					if err := os.WriteFile(bin, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_QUOTA_PROBE_LOG"
case "$*" in
  *"configure get"*) echo '{"profile":"owned","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
						t.Fatal(err)
					}
					base := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "FAKE_QUOTA_PROBE_LOG=" + logPath}
					withToken := append(append([]string(nil), base...), alias+"=token-one")
					initial, changed := base, withToken
					want := "AK"
					if initiallyPresent {
						initial, changed, want = withToken, base, "StsToken"
					}
					mode, err := ResolveQuotaCredentialMode("owned", native, aliyun, initial)
					if err != nil || mode != want {
						t.Fatalf("initial effective mode=%q want=%q err=%v", mode, want, err)
					}
					identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
					binding, err := CaptureQuotaBinding("owned", mode, identity, initial)
					if err != nil {
						t.Fatal(err)
					}
					binding.ResolvedMode = true
					body, err := json.Marshal(binding)
					if err != nil || strings.Contains(string(body), "token-one") || strings.Contains(string(body), "native-secret") || strings.Contains(string(body), "aliyun-secret") {
						t.Fatalf("secret persisted in effective-mode binding: %s %v", body, err)
					}
					if initiallyPresent {
						rotated := append(append([]string(nil), base...), alias+"=token-two")
						if err := ValidateQuotaBinding(binding, rotated); err != nil {
							t.Fatalf("same-mode token rotation rejected: %v", err)
						}
					}
					if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned",`+source.aliyun+`,"region_id":"cn-hangzhou"}]}`), 0o600); err != nil {
						t.Fatal(err)
					}
					unchanged := initial
					if initiallyPresent {
						unchanged = append(append([]string(nil), base...), alias+"=token-two")
					}
					if _, err := ProbeQuotaBinding(context.Background(), binding, execpkg.Config{Bin: bin, Env: unchanged}); err != nil {
						t.Fatalf("same effective mode and unrelated setting rejected: %v", err)
					}
					before, err := os.ReadFile(logPath)
					if err != nil || strings.Count(string(before), "configure get") != 1 || strings.Count(string(before), "GetCallerIdentity") != 1 {
						t.Fatalf("normal probe did not verify selected profile and identity: calls=%q err=%v", before, err)
					}
					if _, err := ProbeQuotaBinding(context.Background(), binding, execpkg.Config{Bin: bin, Env: changed}); err == nil {
						t.Fatal("effective token-presence drift reached credential probe")
					}
					if calls, err := os.ReadFile(logPath); err != nil || string(calls) != string(before) {
						t.Fatalf("credential subprocess ran after mode drift: calls=%q err=%v", calls, err)
					}
				})
			}
		}
	}
}

func TestQuotaBindingChecksEffectiveSTSBeforeCredentialProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "credential-probe-was-launched")
	bin := filepath.Join(dir, "ecctl")
	t.Setenv("FAKE_LOG", marker)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho probe >> \"$FAKE_LOG\"\nexit 98\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(dir, "native.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	profile := `{"profiles":[{"name":"owned","mode":"OIDC","oidc_provider_arn":"acs:ram::1234567890123456:oidc-provider/owned","ram_role_arn":"acs:ram::1234567890123456:role/owned","oidc_token_file":"/unused/token"}]}`
	if err := os.WriteFile(aliyun, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ALIBABA_CLOUD_STS_ENDPOINT=sts.cn-hangzhou.aliyuncs.com", "ALIBABA_CLOUD_STS_REGION=cn-hangzhou", "ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED=false"}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("owned", "OIDC", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(dir, "cleanup.json")
	journal := report.CleanupJournal{Version: 2, JournalPath: journalPath, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, QuotaBinding: binding}
	journalBody, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journalBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, env); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ALIBABA_CLOUD_STS_ENDPOINT", "other.example"},
		{"ALIBABA_CLOUD_STS_REGION", "cn-shanghai"},
		{"ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED", "true"},
	} {
		changed := append([]string{}, env...)
		changed = append(changed, tc.key+"="+tc.value)
		if err := ValidateQuotaBinding(binding, changed); err == nil || !strings.Contains(err.Error(), "effective STS") {
			t.Fatalf("%s drift was accepted: %v", tc.key, err)
		}
		if _, err := ProbeQuotaBinding(context.Background(), binding, execpkg.Config{Bin: bin, Env: changed}); err == nil {
			t.Fatalf("%s drift reached credential probe", tc.key)
		}
		if _, err := PrepareQuotaCleanup(context.Background(), journalPath, journal, execpkg.Config{Bin: bin, Env: changed}); err == nil {
			t.Fatalf("%s drift reached cleanup provider", tc.key)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("credential probe marker unexpectedly exists: %v", err)
		}
	}
	profile = `{"profiles":[{"name":"owned","mode":"OIDC","sts_endpoint":"sts.cn-hangzhou.aliyuncs.com","sts_region":"cn-hangzhou","enable_vpc":true,"oidc_provider_arn":"acs:ram::1234567890123456:oidc-provider/owned","ram_role_arn":"acs:ram::1234567890123456:role/owned","oidc_token_file":"/unused/token"}]}`
	if err := os.WriteFile(aliyun, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, err = CaptureQuotaBinding("owned", "OIDC", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	overridden := append(append([]string{}, env...), "ALIBABA_CLOUD_STS_ENDPOINT=other.example", "ALIBABA_CLOUD_STS_REGION=cn-shanghai", "ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED=true")
	if err := ValidateQuotaBinding(binding, overridden); err != nil {
		t.Fatalf("explicit profile STS settings lost precedence: %v", err)
	}
	if err := os.WriteFile(native, []byte(`{"profiles":[{"name":"owned","mode":"AK","access_key_id":"native-key","access_key_secret":"private-placeholder"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binding, err = CaptureQuotaBinding("owned", "AK", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, overridden); err != nil {
		t.Fatalf("native static override incorrectly inherited Aliyun OIDC STS fallback: %v", err)
	}
}

func TestQuotaBindingPinsSTSForSourceProfileChain(t *testing.T) {
	dir := t.TempDir()
	native := filepath.Join(dir, "native.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"owned","mode":"ChainableRamRoleArn","source_profile":"source","ram_role_arn":"acs:ram::1234567890123456:role/owned","sts_endpoint":"sts.cn-hangzhou.aliyuncs.com"},{"name":"source","mode":"OIDC","oidc_provider_arn":"acs:ram::1234567890123456:oidc-provider/source","ram_role_arn":"acs:ram::1234567890123456:role/source","oidc_token_file":"/unused/token"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ALIBABA_CLOUD_STS_ENDPOINT=sts.cn-hangzhou.aliyuncs.com"}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("owned", "ChainableRamRoleArn", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	if binding.EffectiveSTS["source:source.endpoint"] == "" {
		t.Fatal("source profile STS fallback was not bound")
	}
	changed := append(append([]string{}, env...), "ALIBABA_CLOUD_STS_ENDPOINT=other.example")
	if err := ValidateQuotaBinding(binding, changed); err == nil {
		t.Fatal("source profile STS endpoint drift was accepted")
	}
}
