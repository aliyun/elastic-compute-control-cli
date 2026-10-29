package journalfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

var quotaPublicAuthFields = []string{"mode", "access_key_id", "ram_role_name", "ram_role_arn", "ram_session_name", "source_profile", "sts_endpoint", "sts_region", "oidc_provider_arn", "oidc_token_file", "oauth_site_type", "oauth_generation", "oauth_account_id", "cloud_sso_account_id", "cloud_sso_access_config"}
var quotaSecretAuthFields = []string{"access_key_secret", "sts_token", "oauth_refresh_token", "oauth_access_token", "bearer_token", "external_id", "policy", "process_command", "credentials_uri"}
var quotaCredentialModes = map[string]string{
	"ak": "AK", "ststoken": "StsToken", "ramrolearn": "RamRoleArn", "ecsramrole": "EcsRamRole",
	"chainableramrolearn": "ChainableRamRoleArn", "oidc": "OIDC", "cloudsso": "CloudSSO",
	"oauth": "OAuth", "external": "External", "credentialsuri": "CredentialsURI", "bearertoken": "BearerToken",
}

func quotaField(profile map[string]any, key string) string {
	value, _ := profile[key].(string)
	return strings.TrimSpace(value)
}

func quotaEnv(env []string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(TemplateEnvironmentValue(name, env)); value != "" {
			return value
		}
	}
	return ""
}

func quotaFirst(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func quotaSecurityToken(env []string) string {
	return quotaEnv(env, "ALIBABA_CLOUD_SECURITY_TOKEN", "ALIBABACLOUD_SECURITY_TOKEN", "ALICLOUD_SECURITY_TOKEN", "SECURITY_TOKEN")
}

// ResolveQuotaCredentialMode mirrors the product's profile precedence and
// mode inference without acquiring credentials. Raw values are read only for
// this decision and never enter a Quota journal or error.
func ResolveQuotaCredentialMode(profile, ecctlPath, aliyunPath string, env []string) (string, error) {
	_, native, err := quotaProfileConfigState(ecctlPath, profile)
	if err != nil {
		return "", err
	}
	_, aliyun, err := quotaProfileConfigState(aliyunPath, profile)
	if err != nil {
		return "", err
	}
	if native != nil && quotaCredentialModes[strings.ToLower(quotaField(native, "mode"))] == "OAuth" && quotaField(native, "oauth_generation") != "" {
		return "OAuth", nil
	}
	if native != nil && (quotaField(native, "access_key_id") != "" || quotaField(native, "access_key_secret") != "" || quotaField(native, "sts_token") != "") {
		if quotaFirst(quotaField(native, "sts_token"), quotaField(aliyun, "sts_token"), quotaSecurityToken(env)) != "" {
			return "StsToken", nil
		}
		return "AK", nil
	}
	if aliyun == nil {
		return "", fmt.Errorf("protected Quota selected profile has no configured credential mode")
	}
	if raw := quotaField(aliyun, "mode"); raw != "" {
		if mode := quotaCredentialModes[strings.ToLower(raw)]; mode != "" {
			if mode == "AK" && quotaFirst(quotaField(aliyun, "sts_token"), quotaSecurityToken(env)) != "" {
				return "StsToken", nil
			}
			return mode, nil
		}
		return "", fmt.Errorf("protected Quota selected profile has an unsupported credential mode")
	}
	keyID := quotaFirst(quotaField(aliyun, "access_key_id"), quotaEnv(env, "ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_ID", "ACCESS_KEY_ID"))
	secret := quotaFirst(quotaField(aliyun, "access_key_secret"), quotaEnv(env, "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABACLOUD_ACCESS_KEY_SECRET", "ALICLOUD_ACCESS_KEY_SECRET", "ACCESS_KEY_SECRET"))
	if keyID != "" && secret != "" {
		if quotaFirst(quotaField(aliyun, "sts_token"), quotaSecurityToken(env)) != "" {
			return "StsToken", nil
		}
		if quotaFirst(quotaField(aliyun, "ram_role_arn"), quotaEnv(env, "ALIBABA_CLOUD_ROLE_ARN")) != "" {
			return "RamRoleArn", nil
		}
		return "AK", nil
	}
	for _, field := range []struct{ key, mode string }{
		{"ram_role_name", "EcsRamRole"}, {"process_command", "External"},
		{"credentials_uri", "CredentialsURI"}, {"oidc_provider_arn", "OIDC"},
		{"cloud_sso_sign_in_url", "CloudSSO"}, {"oauth_site_type", "OAuth"},
		{"bearer_token", "BearerToken"},
	} {
		if quotaField(aliyun, field.key) != "" {
			return field.mode, nil
		}
	}
	return "", fmt.Errorf("protected Quota selected profile has no configured credential mode")
}

func quotaConfigState(path, profile string) (report.CredentialConfigState, error) {
	state, _, err := quotaProfileConfigState(path, profile)
	return state, err
}

func quotaProfileConfigState(path, profile string) (report.CredentialConfigState, map[string]any, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return report.CredentialConfigState{}, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return report.CredentialConfigState{}, nil, fmt.Errorf("protected Quota configuration target is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report.CredentialConfigState{}, nil, fmt.Errorf("protected Quota configuration target is unreadable")
	}
	var config struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return report.CredentialConfigState{}, nil, fmt.Errorf("protected Quota configuration target is invalid")
	}
	state := report.CredentialConfigState{Exists: true, Mode: uint32(info.Mode().Perm())}
	seen := false
	var selected map[string]any
	for _, candidate := range config.Profiles {
		if candidate["name"] != profile {
			continue
		}
		if seen {
			return report.CredentialConfigState{}, nil, fmt.Errorf("protected Quota credential profile is duplicated")
		}
		seen = true
		selected = candidate
		state.AuthSelectors = make(map[string]string)
		state.SensitiveFields = make(map[string]bool)
		for _, key := range quotaPublicAuthFields {
			if value, ok := candidate[key].(string); ok {
				state.AuthSelectors[key] = value
			}
		}
		if raw, ok := candidate["cloud_sso_sign_in_url"].(string); ok {
			parsed, parseErr := url.Parse(raw)
			if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return report.CredentialConfigState{}, nil, fmt.Errorf("protected Quota CloudSSO origin is invalid")
			}
			// CloudSSO fetch uses only this origin. Path, query, and fragment
			// may contain secrets and never enter a recovery journal.
			state.AuthSelectors["cloud_sso_origin"] = parsed.Scheme + "://" + parsed.Host
		}
		for _, key := range quotaSecretAuthFields {
			if value, exists := candidate[key]; exists && value != nil {
				state.SensitiveFields[key] = true
			}
		}
		if len(state.AuthSelectors) == 0 {
			state.AuthSelectors = nil
		}
		if len(state.SensitiveFields) == 0 {
			state.SensitiveFields = nil
		}
	}
	return state, selected, nil
}

func quotaSourceProfiles(path, selected, mode string) (map[string]report.CredentialConfigState, error) {
	if !strings.EqualFold(strings.TrimSpace(mode), "ChainableRamRoleArn") {
		return nil, nil
	}
	states := make(map[string]report.CredentialConfigState)
	seen := map[string]bool{selected: true}
	name := selected
	for {
		state, raw, err := quotaProfileConfigState(path, name)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, fmt.Errorf("protected Quota source profile is missing")
		}
		if name != selected {
			states[name] = state
		}
		if !strings.EqualFold(strings.TrimSpace(state.AuthSelectors["mode"]), "ChainableRamRoleArn") {
			break
		}
		next := strings.TrimSpace(state.AuthSelectors["source_profile"])
		if next == "" {
			return nil, fmt.Errorf("protected Quota source profile is missing")
		}
		if seen[next] {
			return nil, fmt.Errorf("protected Quota source profile chain contains a cycle")
		}
		seen[next] = true
		name = next
	}
	return states, nil
}

// CaptureQuotaBinding stores only public profile identity and configuration
// selectors. Selected-profile public fields and sensitive-field presence
// detect routine auth edits without storing a credential digest or secret.
func CaptureQuotaBinding(profile, mode string, identity execpkg.IdentityPin, env []string) (*report.QuotaCredentialBinding, error) {
	if profile == "" || mode == "" || strings.ContainsAny(profile+mode, "\x00\r\n\t") {
		return nil, fmt.Errorf("protected Quota requires a configured profile and auth mode")
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	paths, err := CaptureTemplateConfig(env)
	if err != nil {
		return nil, err
	}
	binding := &report.QuotaCredentialBinding{Profile: profile, Mode: mode, Identity: identity, Selectors: map[string]string{}, ConfigPaths: map[string]report.CredentialConfigPath{}, ConfigState: map[string]report.CredentialConfigState{}}
	for _, key := range TemplateSelectorNames {
		binding.Selectors[key] = TemplateEnvironmentValue(key, env)
	}
	for key, path := range paths {
		binding.ConfigPaths[key] = report.CredentialConfigPath{Requested: path.Requested, Target: path.Target}
	}
	for _, key := range []string{"ecctl", "aliyun"} {
		state, err := quotaConfigState(binding.ConfigPaths[key].Target, profile)
		if err != nil {
			return nil, err
		}
		binding.ConfigState[key] = state
	}
	binding.SourceProfiles, err = quotaSourceProfiles(binding.ConfigPaths["aliyun"].Target, profile, mode)
	if err != nil {
		return nil, err
	}
	binding.EffectiveSTS, err = quotaEffectiveSTS(binding.ConfigPaths, profile, mode, env, binding.SourceProfiles)
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func ValidateQuotaBinding(binding *report.QuotaCredentialBinding, env []string) error {
	if binding == nil || binding.Profile == "" || binding.Mode == "" || strings.ContainsAny(binding.Profile+binding.Mode, "\x00\r\n\t") {
		return fmt.Errorf("protected Quota credential binding is missing")
	}
	if err := binding.Identity.Validate(); err != nil {
		return err
	}
	for _, key := range TemplateSelectorNames {
		if binding.Selectors[key] != TemplateEnvironmentValue(key, env) {
			return fmt.Errorf("protected Quota configuration selector changed: %s", key)
		}
	}
	paths, err := CaptureTemplateConfig(env)
	if err != nil {
		return err
	}
	if len(paths) != len(binding.ConfigPaths) {
		return fmt.Errorf("protected Quota configuration paths changed")
	}
	for key, path := range paths {
		if binding.ConfigPaths[key] != (report.CredentialConfigPath{Requested: path.Requested, Target: path.Target}) {
			return fmt.Errorf("protected Quota configuration target changed: %s", key)
		}
	}
	for _, key := range []string{"ecctl", "aliyun"} {
		state, err := quotaConfigState(binding.ConfigPaths[key].Target, binding.Profile)
		captured := binding.ConfigState[key]
		// `configure use B` can create the native file after A was captured
		// from Aliyun. A newly created, private native file with no A auth
		// fields cannot change A's credential resolution. All other state
		// changes retain the exact selected-profile comparison.
		newNativeSettingsOnly := key == "ecctl" && !captured.Exists && state.Exists && len(captured.AuthSelectors) == 0 && len(captured.SensitiveFields) == 0 && len(state.AuthSelectors) == 0 && len(state.SensitiveFields) == 0 && quotaNativeNewFilePrivate(binding.ConfigPaths[key].Target)
		if err != nil || !reflect.DeepEqual(state, captured) && !newNativeSettingsOnly {
			return fmt.Errorf("protected Quota configuration state changed: %s", key)
		}
	}
	sources, err := quotaSourceProfiles(binding.ConfigPaths["aliyun"].Target, binding.Profile, binding.Mode)
	if err != nil || !reflect.DeepEqual(sources, binding.SourceProfiles) {
		return fmt.Errorf("protected Quota source profile chain changed")
	}
	effectiveSTS, err := quotaEffectiveSTS(binding.ConfigPaths, binding.Profile, binding.Mode, env, sources)
	if err != nil || !reflect.DeepEqual(effectiveSTS, binding.EffectiveSTS) {
		return fmt.Errorf("protected Quota effective STS settings changed")
	}
	if binding.ResolvedMode {
		mode, err := ResolveQuotaCredentialMode(binding.Profile, binding.ConfigPaths["ecctl"].Target, binding.ConfigPaths["aliyun"].Target, env)
		if err != nil || mode != binding.Mode {
			return fmt.Errorf("protected Quota resolved credential mode changed")
		}
	}
	return nil
}

func quotaEffectiveSTS(paths map[string]report.CredentialConfigPath, profile, mode string, env []string, sources map[string]report.CredentialConfigState) (map[string]string, error) {
	_, native, err := quotaProfileConfigState(paths["ecctl"].Target, profile)
	if err != nil {
		return nil, err
	}
	_, aliyun, err := quotaProfileConfigState(paths["aliyun"].Target, profile)
	if err != nil {
		return nil, err
	}
	selected := aliyun
	if native != nil && ((mode == "OAuth" && quotaField(native, "oauth_generation") != "") || quotaField(native, "access_key_id") != "" || quotaField(native, "access_key_secret") != "" || quotaField(native, "sts_token") != "") {
		selected = native
	}
	settings := map[string]string{}
	if err := quotaAddEffectiveSTS(settings, "selected", selected, mode, env); err != nil {
		return nil, err
	}
	for name := range sources {
		_, source, err := quotaProfileConfigState(paths["aliyun"].Target, name)
		if err != nil || source == nil {
			return nil, fmt.Errorf("protected Quota STS source profile is unavailable")
		}
		sourceMode := quotaCredentialModes[strings.ToLower(quotaField(source, "mode"))]
		if sourceMode == "" {
			switch {
			case quotaField(source, "oidc_provider_arn") != "":
				sourceMode = "OIDC"
			case quotaField(source, "ram_role_arn") != "":
				sourceMode = "RamRoleArn"
			case quotaField(source, "source_profile") != "":
				sourceMode = "ChainableRamRoleArn"
			default:
				sourceMode = "AK"
			}
		}
		if err := quotaAddEffectiveSTS(settings, "source:"+name, source, sourceMode, env); err != nil {
			return nil, err
		}
	}
	if len(settings) == 0 {
		return nil, nil
	}
	return settings, nil
}

func quotaAddEffectiveSTS(settings map[string]string, key string, profile map[string]any, mode string, env []string) error {
	if mode == "AK" || mode == "StsToken" || mode == "BearerToken" {
		return nil
	}
	region := quotaFirst(quotaField(profile, "sts_region"), quotaEnv(env, "ALIBABA_CLOUD_STS_REGION"))
	vpc, _ := profile["enable_vpc"].(bool)
	if value, ok := profile["enable_vpc"].(string); ok {
		vpc = value == "1" || strings.EqualFold(value, "true")
	}
	envVPC := strings.TrimSpace(TemplateEnvironmentValue("ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED", env))
	vpc = vpc || envVPC == "1" || strings.EqualFold(envVPC, "true")
	settings[key+".region"] = region
	settings[key+".vpc"] = fmt.Sprint(vpc)
	if mode != "OIDC" && mode != "RamRoleArn" && mode != "ChainableRamRoleArn" {
		return nil
	}
	endpoint := quotaFirst(quotaField(profile, "sts_endpoint"), quotaEnv(env, "ALIBABA_CLOUD_STS_ENDPOINT"))
	if endpoint != "" {
		candidate := endpoint
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
			return fmt.Errorf("protected Quota STS endpoint is invalid")
		}
		endpoint = parsed.Host
	}
	settings[key+".endpoint"] = endpoint
	return nil
}

func BoundQuotaConfig(binding *report.QuotaCredentialBinding, cfg execpkg.Config) execpkg.Config {
	cfg.Env = append(append([]string{}, cfg.Env...), "ECCTL_CONFIG_PATH="+binding.ConfigPaths["ecctl"].Target, "ECCTL_ALIYUN_CONFIG_PATH="+binding.ConfigPaths["aliyun"].Target)
	cfg.Profile = binding.Profile
	pin := binding.Identity
	cfg.ExpectedIdentity = &pin
	return cfg
}

// ProbeQuotaBinding checks the selected profile and trusted STS principal
// before each protected provider call. The CLI also receives the pinned
// profile and expected identity for its own pre-request guard.
func ProbeQuotaBinding(ctx context.Context, binding *report.QuotaCredentialBinding, cfg execpkg.Config) (execpkg.Config, error) {
	if err := ValidateQuotaBinding(binding, cfg.Env); err != nil {
		return cfg, err
	}
	bound := BoundQuotaConfig(binding, cfg)
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := execpkg.Run(probeCtx, bound, "ecctl configure get --profile "+quoteQuotaArgument(binding.Profile)+" --output=json --agent-envelope=false")
	root, _ := result.JSON.(map[string]any)
	profile, _ := root["profile"].(string)
	mode, _ := root["mode"].(string)
	// Resolved bindings already checked the effective mode against the selected
	// config above. configure get reports a presentation mode that can differ
	// from that effective mode (for aliases or native static overrides).
	if result.Err != nil || result.Exit != 0 || profile != binding.Profile || !binding.ResolvedMode && mode != binding.Mode {
		return cfg, fmt.Errorf("protected Quota selected profile or auth mode changed")
	}
	if _, err := execpkg.ReadIdentity(probeCtx, bound, binding.Profile); err != nil {
		return cfg, err
	}
	return bound, nil
}

func quoteQuotaArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// PrepareQuotaCleanup reads the current journal under the caller's replay or
// cleanup fence; an unbound journal is returned unchanged for unrelated cases.
func PrepareQuotaCleanup(ctx context.Context, path string, meta report.CleanupJournal, cfg execpkg.Config) (execpkg.Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && meta.QuotaBinding == nil {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		if meta.QuotaBinding != nil {
			return cfg, fmt.Errorf("protected Quota binding requires a version 2 journal")
		}
		var entries []report.Resource
		if err := json.Unmarshal(data, &entries); err != nil {
			return cfg, err
		}
		if err := CheckJournalPath(path, report.CleanupJournal{Entries: entries}); err != nil {
			return cfg, err
		}
		for _, entry := range entries {
			if IsQuotaCommandCandidate(entry.Teardown) {
				return cfg, fmt.Errorf("protected Quota binding is missing")
			}
		}
		return cfg, nil
	}
	var current report.CleanupJournal
	if err := json.Unmarshal(data, &current); err != nil {
		return cfg, err
	}
	if err := CheckJournalPath(path, current); err != nil {
		return cfg, err
	}
	if current.QuotaBinding == nil && meta.QuotaBinding == nil {
		return cfg, nil
	}
	if current.Version != 2 || current.RunID != meta.RunID || current.ExecutionID != meta.ExecutionID || current.RegionRole != meta.RegionRole || current.Region != meta.Region || current.Surface != meta.Surface || current.EcctlBin != meta.EcctlBin {
		return cfg, fmt.Errorf("protected Quota journal metadata changed")
	}
	if meta.QuotaBinding != nil && !reflect.DeepEqual(meta.QuotaBinding, current.QuotaBinding) {
		return cfg, fmt.Errorf("protected Quota credential binding changed")
	}
	if current.QuotaBinding == nil {
		return cfg, nil
	}
	return ProbeQuotaBinding(ctx, current.QuotaBinding, cfg)
}
