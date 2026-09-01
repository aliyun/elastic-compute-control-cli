package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

const DefaultProfileName = "default"

var ErrCredentialProfileChanged = errors.New("credential profile changed during OAuth login")

// RegionSource records which input won region resolution. Callers must keep
// this provenance until credential profile policy has been applied: ignoring
// credential profiles must discard a stored profile region, but it must not
// discard an explicit flag or ECCTL_REGION.
type RegionSource uint8

const (
	RegionSourceUnset RegionSource = iota
	RegionSourceExplicit
	RegionSourceECCTL
	RegionSourceAlibabaEnv
	RegionSourceProfile
)

type ResolvedRegion struct {
	Value  string
	Source RegionSource
}

type Store struct {
	path       string
	targetPath string
	existed    bool
	data       map[string]any
	pending    []storeMutation
}

func (s *Store) ResolvedPath() string {
	if s == nil {
		return ""
	}
	return s.targetPath
}

func (s *Store) RequestedPath() string {
	if s == nil {
		return ""
	}
	return s.path
}

type storeMutation struct {
	kind  string
	name  string
	key   string
	value string
	oauth nativeOAuthMutation
}

const (
	storeMutationSetValue   = "set-value"
	storeMutationUseProfile = "use-profile"
	storeMutationSetCurrent = "set-current"
	storeMutationSetOAuth   = "set-native-oauth"
	configWriteLockTimeout  = 2 * time.Second
	configWriteLockRetry    = 25 * time.Millisecond
	nativeOAuthConfigLimit  = 1 << 20
)

type NativeOAuthProfileState struct {
	Name           string
	SiteType       string
	Generation     string
	AccountID      string
	Current        string
	Exists         bool
	ConfigExisted  bool
	AuthGeneration [sha256.Size]byte
}

type nativeOAuthMutation struct {
	siteType               string
	generation             string
	accountID              string
	expectedCurrent        string
	expectedExists         bool
	expectedConfigExisted  bool
	expectedAuthGeneration [sha256.Size]byte
}

type Profile struct {
	Name            string
	Mode            string
	Region          string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Language        string
	Output          string
}

type ConfigItem struct {
	Key         string   `json:"key"`
	StoredAs    string   `json:"stored_as"`
	Description string   `json:"description"`
	Value       string   `json:"value,omitempty"`
	Type        string   `json:"type"`
	Allowed     []string `json:"allowed,omitempty"`
	Sensitive   bool     `json:"sensitive,omitempty"`
	SetExample  string   `json:"set_example"`
}

type ConfigValue struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

var configItems = []ConfigItem{
	{Key: "region", StoredAs: "region_id", Description: "Default Alibaba Cloud region.", Type: "string", SetExample: "ecctl configure set region cn-hangzhou"},
	{Key: "access-key-id", StoredAs: "access_key_id", Description: "Alibaba Cloud AccessKey ID.", Type: "string", SetExample: "ecctl configure set access-key-id <value>"},
	{Key: "access-key-secret", StoredAs: "access_key_secret", Description: "Alibaba Cloud AccessKey secret.", Type: "string", Sensitive: true, SetExample: "ecctl configure set access-key-secret <value>"},
	{Key: "security-token", StoredAs: "sts_token", Description: "Optional STS security token.", Type: "string", Sensitive: true, SetExample: "ecctl configure set security-token <value>"},
	{Key: "lang", StoredAs: "language", Description: "Default display language.", Type: "string", Allowed: []string{"en", "zh-CN"}, SetExample: "ecctl configure set lang zh-CN"},
	{Key: "output", StoredAs: "output_format", Description: "Default output format.", Type: "string", Allowed: []string{"json", "text"}, SetExample: "ecctl configure set output text"},
	{Key: "telemetry.enabled", StoredAs: "telemetry.enabled", Description: "Enable best-effort pseudonymous product telemetry.", Type: "boolean", Allowed: []string{"true", "false"}, SetExample: "ecctl configure set telemetry.enabled false"},
}

var credentialProfileAuthKeys = []string{
	"mode", "access_key_id", "access_key_secret", "sts_token", "sts_expiration",
	"ram_role_name", "ram_role_arn", "ram_session_name", "source_profile", "expired_seconds", "policy", "external_id",
	"sts_endpoint", "sts_region", "enable_vpc", "oidc_provider_arn", "oidc_token_file",
	"oauth_site_type", "oauth_refresh_token", "oauth_refresh_token_expire", "oauth_access_token", "oauth_access_token_expire", "oauth_generation", "oauth_account_id",
	"cloud_sso_sign_in_url", "cloud_sso_account_id", "cloud_sso_access_config", "access_token", "cloud_sso_access_token_expire",
	"process_command", "credentials_uri", "bearer_token", "bearer_token_header_key",
}

func CredentialProfileAuthDigest(profile map[string]any) [sha256.Size]byte {
	auth := make(map[string]any, len(credentialProfileAuthKeys)+1)
	auth["name"] = stringField(profile, "name")
	for _, key := range credentialProfileAuthKeys {
		if value, ok := profile[key]; ok {
			auth[key] = value
		}
	}
	raw, _ := json.Marshal(auth)
	return sha256.Sum256(raw)
}

var configKeyAliases = map[string]string{
	"region-id":         "region",
	"region_id":         "region",
	"access_key_id":     "access-key-id",
	"access_key_secret": "access-key-secret",
	"security_token":    "security-token",
	"sts-token":         "security-token",
	"sts_token":         "security-token",
	"language":          "lang",
	"output-format":     "output",
	"output_format":     "output",
	"telemetry-enabled": "telemetry.enabled",
}

func ResolveRegion(explicit string, getenv func(string) string) (string, *ecerrors.AppError) {
	region := explicit
	if region == "" && getenv != nil {
		region = getenv("ECCTL_REGION")
	}
	if region == "" {
		return "", ecerrors.Client("MissingRegion", "region is required")
	}
	if !ValidRegion(region) {
		return "", ecerrors.Client("InvalidRegion", "region is not supported")
	}
	return region, nil
}

func ResolveRegionForProfile(explicit string, profile string, configPath string, getenv func(string) string) (string, *ecerrors.AppError) {
	resolved, err := ResolveRegionForProfileWithSource(explicit, profile, configPath, getenv)
	return resolved.Value, err
}

func ResolveRegionForProfileWithSource(explicit string, profile string, configPath string, getenv func(string) string) (ResolvedRegion, *ecerrors.AppError) {
	if explicit != "" {
		return resolveRegionWithSource(explicit, RegionSourceExplicit)
	}
	if getenv != nil {
		if region := getenv("ECCTL_REGION"); region != "" {
			return resolveRegionWithSource(region, RegionSourceECCTL)
		}
	}
	ignoreProfile := getenv != nil && (strings.EqualFold(strings.TrimSpace(getenv("ALIBABA_CLOUD_IGNORE_PROFILE")), "true") || strings.EqualFold(strings.TrimSpace(getenv("ALIBABACLOUD_IGNORE_PROFILE")), "true"))
	if !ignoreProfile {
		loaded, _, err := EffectiveProfile(profile, configPath, AliyunConfigPath(getenv))
		if err != nil {
			return ResolvedRegion{}, ecerrors.Client("InvalidConfig", err.Error())
		}
		if loaded.Region != "" {
			return resolveRegionWithSource(loaded.Region, RegionSourceProfile)
		}
	}
	if getenv != nil {
		for _, name := range []string{
			"ALIBABA_CLOUD_REGION_ID",
			"ALIBABACLOUD_REGION_ID",
			"ALICLOUD_REGION_ID",
			"REGION_ID",
			"REGION",
		} {
			if region := getenv(name); region != "" {
				return resolveRegionWithSource(region, RegionSourceAlibabaEnv)
			}
		}
	}
	return ResolvedRegion{}, ecerrors.Client("MissingRegion", "region is required")
}

func resolveRegionWithSource(value string, source RegionSource) (ResolvedRegion, *ecerrors.AppError) {
	region, err := ResolveRegion(value, nil)
	if err != nil {
		return ResolvedRegion{}, err
	}
	return ResolvedRegion{Value: region, Source: source}, nil
}

func ValidRegion(region string) bool {
	if region == "not-a-real-region" || len(region) < 3 || len(region) > 63 || region[0] == '-' || region[len(region)-1] == '-' || !strings.Contains(region, "-") {
		return false
	}
	previousHyphen := false
	for _, char := range region {
		isHyphen := char == '-'
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && !isHyphen {
			return false
		}
		if isHyphen && previousHyphen {
			return false
		}
		previousHyphen = isHyphen
	}
	return true
}

func ConfigPath(getenv func(string) string) string {
	return EcctlConfigPath(getenv)
}

func EcctlConfigPath(getenv func(string) string) string {
	if getenv != nil {
		if path := getenv("ECCTL_CONFIG_PATH"); path != "" {
			return path
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".ecctl", "config.json")
	}
	return filepath.Join(home, ".ecctl", "config.json")
}

func AliyunConfigPath(getenv func(string) string) string {
	if getenv != nil {
		for _, name := range []string{"ECCTL_ALIYUN_CONFIG_PATH", "ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
			if path := getenv(name); path != "" {
				return path
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".aliyun", "config.json")
	}
	return filepath.Join(home, ".aliyun", "config.json")
}

func ProfileName(explicit string, getenv func(string) string) string {
	if explicit != "" {
		return explicit
	}
	if getenv != nil {
		for _, name := range []string{"ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
			if value := getenv(name); value != "" {
				return value
			}
		}
	}
	return ""
}

func LoadStore(path string) (*Store, error) {
	if path == "" {
		path = EcctlConfigPath(os.Getenv)
	}
	_, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newStore(path), nil
	}
	if err != nil {
		return nil, err
	}
	target, err := configfile.Resolve(path, false)
	if err != nil {
		return nil, err
	}
	raw, _, err := target.Read()
	if err != nil {
		return nil, err
	}
	data, err := decodeStoreData(raw)
	if err != nil {
		return nil, err
	}
	store := &Store{path: path, targetPath: target.Path(), existed: true, data: data}
	store.ensureShape()
	return store, nil
}

// LoadNativeOAuthStore freezes and strictly validates the ecctl configuration
// target before an interactive login begins. OAuth tokens are never stored in
// this general configuration file.
func LoadNativeOAuthStore(path string) (*Store, error) {
	if path == "" {
		path = EcctlConfigPath(os.Getenv)
	}
	target, err := configfile.Resolve(path, true)
	if err != nil {
		return nil, err
	}
	raw, _, err := target.ReadBoundedRegular(nativeOAuthConfigLimit)
	if os.IsNotExist(err) {
		store := newStore(path)
		store.targetPath = target.Path()
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := decodeNativeStoreData(raw)
	if err != nil {
		return nil, err
	}
	store := &Store{path: path, targetPath: target.Path(), existed: true, data: data}
	return store, nil
}

func (s *Store) PreflightNativeOAuthWrite(ctx context.Context) error {
	if s == nil || s.path == "" || s.targetPath == "" {
		return fmt.Errorf("configuration store is unavailable")
	}
	target, err := configfile.Resolve(s.path, true)
	if err != nil {
		return err
	}
	if target.Path() != s.targetPath {
		return configfile.ErrTargetReplaced
	}
	return target.WithLock(ctx, configWriteLockTimeout, configWriteLockRetry, func() error {
		_, info, readErr := target.ReadBoundedRegular(nativeOAuthConfigLimit)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if info != nil && info.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("configuration is read-only")
		}
		temp, err := configfile.CreateSensitiveTemp(filepath.Dir(target.Path()), ".ecctl-oauth-preflight-*.tmp")
		if err != nil {
			return err
		}
		path := temp.Name()
		closeErr := temp.Close()
		cleanupErr := configfile.CleanupSensitiveTemp(path)
		return errors.Join(closeErr, cleanupErr)
	})
}

func LoadExistingStore(path string) (*Store, bool, error) {
	if path == "" {
		path = EcctlConfigPath(os.Getenv)
	}
	_, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	target, err := configfile.Resolve(path, false)
	if err != nil {
		return nil, false, err
	}
	raw, _, err := target.Read()
	if err != nil {
		return nil, false, err
	}
	data, err := decodeStoreData(raw)
	if err != nil {
		return nil, false, err
	}
	store := &Store{path: path, targetPath: target.Path(), existed: true, data: data}
	store.ensureShape()
	return store, true, nil
}

func (s *Store) Save() error {
	return s.SaveContext(context.Background())
}

func (s *Store) SaveContext(ctx context.Context) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("configuration store is unavailable")
	}
	target, err := configfile.Resolve(s.path, true)
	if err != nil {
		return err
	}
	if s.targetPath != "" && s.targetPath != target.Path() {
		return fmt.Errorf("configuration target was replaced")
	}
	strictNativeOAuth := s.hasPendingNativeOAuthMutation()
	return target.WithLock(ctx, configWriteLockTimeout, configWriteLockRetry, func() error {
		var current *Store
		var raw []byte
		var info os.FileInfo
		var readErr error
		if strictNativeOAuth {
			raw, info, readErr = target.ReadBoundedRegular(nativeOAuthConfigLimit)
		} else {
			raw, info, readErr = target.Read()
		}
		switch {
		case readErr == nil:
			var data map[string]any
			var decodeErr error
			if strictNativeOAuth {
				data, decodeErr = decodeNativeStoreData(raw)
			} else {
				data, decodeErr = decodeStoreData(raw)
			}
			if decodeErr != nil {
				return decodeErr
			}
			current = &Store{path: s.path, targetPath: target.Path(), existed: true, data: data}
		case os.IsNotExist(readErr):
			current = newStore(s.path)
			current.targetPath = target.Path()
		default:
			return readErr
		}
		current.ensureShape()
		if len(s.pending) == 0 && s.existed {
			s.data = current.data
			s.targetPath = target.Path()
			return nil
		}
		if len(s.pending) == 0 {
			cloned, cloneErr := cloneConfigMap(s.data)
			if cloneErr != nil {
				return cloneErr
			}
			current.data = cloned
			current.ensureShape()
		} else {
			for _, mutation := range s.pending {
				if err := current.applyMutation(mutation, false); err != nil {
					return err
				}
			}
		}
		if info != nil && info.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("configuration is read-only")
		}
		encoded, err := json.MarshalIndent(current.data, "", "  ")
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if info != nil {
			mode = info.Mode().Perm()
		}
		if err := target.AtomicWrite(append(encoded, '\n'), mode); err != nil {
			return err
		}
		s.data = current.data
		s.targetPath = target.Path()
		s.existed = true
		s.pending = nil
		return nil
	})
}

func (s *Store) hasPendingNativeOAuthMutation() bool {
	for _, mutation := range s.pending {
		if mutation.kind == storeMutationSetOAuth {
			return true
		}
	}
	return false
}

func decodeStoreData(raw []byte) (map[string]any, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

func decodeNativeStoreData(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var data map[string]any
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("configuration must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("configuration contains multiple JSON values")
		}
		return nil, err
	}
	current, ok := data["current"].(string)
	if !ok || strings.TrimSpace(current) == "" || strings.TrimSpace(current) != current {
		return nil, fmt.Errorf("configuration current must be a non-empty string")
	}
	profiles, ok := data["profiles"].([]any)
	if !ok {
		return nil, fmt.Errorf("configuration profiles must be an array")
	}
	seen := map[string]bool{}
	for index, rawProfile := range profiles {
		profile, ok := rawProfile.(map[string]any)
		if !ok || profile == nil {
			return nil, fmt.Errorf("configuration profile %d must be an object", index)
		}
		rawName, ok := profile["name"].(string)
		name := strings.TrimSpace(rawName)
		if !ok || name == "" || name != rawName {
			return nil, fmt.Errorf("configuration profile %d name must be a non-empty string", index)
		}
		if seen[name] {
			return nil, fmt.Errorf("configuration profile %s is duplicated", name)
		}
		seen[name] = true
	}
	return data, nil
}

func (s *Store) Current() string {
	current, _ := s.data["current"].(string)
	if current == "" {
		return DefaultProfileName
	}
	return current
}

func (s *Store) Profile(name string) (Profile, bool) {
	if name == "" {
		name = s.Current()
	}
	for _, profile := range s.profileMaps() {
		if stringField(profile, "name") == name {
			return Profile{
				Name:            name,
				Mode:            stringField(profile, "mode"),
				Region:          stringField(profile, "region_id"),
				AccessKeyID:     stringField(profile, "access_key_id"),
				AccessKeySecret: stringField(profile, "access_key_secret"),
				SecurityToken:   stringField(profile, "sts_token"),
				Language:        stringField(profile, "language"),
				Output:          stringField(profile, "output_format"),
			}, true
		}
	}
	return Profile{Name: name}, false
}

func EffectiveProfile(name string, ecctlConfigPath string, aliyunConfigPath string) (Profile, bool, error) {
	ecctlStore, hasEcctl, err := LoadExistingStore(ecctlConfigPath)
	if err != nil {
		return Profile{}, false, err
	}
	aliyunStore, hasAliyun, err := LoadExistingStore(aliyunConfigPath)
	if err != nil {
		return Profile{}, false, err
	}
	if name == "" {
		switch {
		case hasEcctl:
			name = ecctlStore.Current()
		case hasAliyun:
			name = aliyunStore.Current()
		default:
			name = DefaultProfileName
		}
	}
	profile := Profile{Name: name}
	found := false
	if hasAliyun {
		if loaded, ok := aliyunStore.Profile(name); ok {
			profile = mergeProfile(profile, loaded)
			found = true
		}
	}
	if hasEcctl {
		if loaded, ok := ecctlStore.Profile(name); ok {
			profile = mergeProfile(profile, loaded)
			found = true
		}
	}
	return profile, found, nil
}

func EffectiveValue(name string, key string, showSecret bool, ecctlConfigPath string, aliyunConfigPath string) (ConfigValue, error) {
	if item, ok := lookupConfigItem(key); ok && item.Key == "telemetry.enabled" {
		store, err := LoadStore(ecctlConfigPath)
		if err != nil {
			return ConfigValue{}, err
		}
		return store.GetValue("", item.Key, showSecret)
	}
	profile, _, err := EffectiveProfile(name, ecctlConfigPath, aliyunConfigPath)
	if err != nil {
		return ConfigValue{}, err
	}
	return ConfigValueFromProfile(profile, key, showSecret)
}

func EffectiveItems(name string, showSecret bool, ecctlConfigPath string, aliyunConfigPath string) ([]ConfigItem, error) {
	profile, _, err := EffectiveProfile(name, ecctlConfigPath, aliyunConfigPath)
	if err != nil {
		return nil, err
	}
	items := SupportedItems()
	for i := range items {
		if items[i].Key == "telemetry.enabled" {
			store, loadErr := LoadStore(ecctlConfigPath)
			if loadErr != nil {
				return nil, loadErr
			}
			value, valueErr := store.GetValue("", items[i].Key, showSecret)
			if valueErr != nil {
				return nil, valueErr
			}
			items[i].Value = value.Value
			continue
		}
		value, err := ConfigValueFromProfile(profile, items[i].Key, showSecret)
		if err != nil {
			return nil, err
		}
		items[i].Value = value.Value
	}
	return items, nil
}

func ConfigValueFromProfile(profile Profile, key string, showSecret bool) (ConfigValue, error) {
	item, ok := lookupConfigItem(key)
	if !ok {
		return ConfigValue{}, fmt.Errorf("unknown config key %s", key)
	}
	value := profileField(profile, item.Key)
	if item.Key == "telemetry.enabled" {
		value = "true"
	}
	if item.Key == "output" && value == "" {
		value = "json"
	}
	return ConfigValue{Key: item.Key, Value: displayConfigValue(item, value, showSecret), Sensitive: item.Sensitive}, nil
}

func SupportedItems() []ConfigItem {
	items := make([]ConfigItem, len(configItems))
	copy(items, configItems)
	return items
}

func (s *Store) ConfigItems(name string, showSecret bool) ([]ConfigItem, error) {
	items := SupportedItems()
	for i := range items {
		value, err := s.GetValue(name, items[i].Key, showSecret)
		if err != nil {
			return nil, err
		}
		items[i].Value = value.Value
	}
	return items, nil
}

func (s *Store) GetValue(name string, key string, showSecret bool) (ConfigValue, error) {
	item, ok := lookupConfigItem(key)
	if !ok {
		return ConfigValue{}, fmt.Errorf("unknown config key %s", key)
	}
	if name == "" {
		name = s.Current()
	}
	if item.Key == "telemetry.enabled" {
		enabled, err := telemetryEnabledValue(s.data)
		if err != nil {
			return ConfigValue{}, err
		}
		return ConfigValue{Key: item.Key, Value: fmt.Sprintf("%t", enabled)}, nil
	}
	profile := s.profileMap(name)
	value := stringField(profile, item.StoredAs)
	if item.Key == "output" && value == "" {
		value = "json"
	}
	return ConfigValue{Key: item.Key, Value: displayConfigValue(item, value, showSecret), Sensitive: item.Sensitive}, nil
}

func (s *Store) SetValue(name string, key string, value string) (ConfigValue, error) {
	return s.setValue(name, key, value, true)
}

func (s *Store) setValue(name string, key string, value string, record bool) (ConfigValue, error) {
	item, ok := lookupConfigItem(key)
	if !ok {
		return ConfigValue{}, fmt.Errorf("unknown config key %s", key)
	}
	if err := validateConfigValue(item, value); err != nil {
		return ConfigValue{}, err
	}
	if item.Key == "telemetry.enabled" {
		enabled := value == "true"
		rawTelemetry, exists := s.data["telemetry"]
		if !exists {
			s.data["telemetry"] = map[string]any{"enabled": enabled}
			if record {
				s.pending = append(s.pending, storeMutation{kind: storeMutationSetValue, key: item.Key, value: value})
			}
			return ConfigValue{Key: item.Key, Value: value}, nil
		}
		telemetryMap, ok := rawTelemetry.(map[string]any)
		if !ok || telemetryMap == nil {
			return ConfigValue{}, fmt.Errorf("telemetry must be an object")
		}
		telemetryMap["enabled"] = enabled
		if record {
			s.pending = append(s.pending, storeMutation{kind: storeMutationSetValue, key: item.Key, value: value})
		}
		return ConfigValue{Key: item.Key, Value: value}, nil
	}
	if name == "" {
		name = s.Current()
	}
	profile := s.profileMap(name)
	profile[item.StoredAs] = value
	if item.Key == "access-key-id" || item.Key == "access-key-secret" {
		if stringField(profile, "sts_token") != "" {
			profile["mode"] = "StsToken"
		} else {
			profile["mode"] = "AK"
		}
	}
	if item.Key == "security-token" {
		profile["mode"] = "StsToken"
	}
	s.data["current"] = name
	if record {
		s.pending = append(s.pending, storeMutation{kind: storeMutationSetValue, name: name, key: item.Key, value: value})
	}
	return ConfigValue{Key: item.Key, Value: displayConfigValue(item, value, false), Sensitive: item.Sensitive}, nil
}

func (s *Store) SetRegion(name string, region string) error {
	_, err := s.SetValue(name, "region", region)
	return err
}

func (s *Store) UseProfile(name string) error {
	return s.useProfile(name, true)
}

func (s *Store) useProfile(name string, record bool) error {
	if name == "" {
		return fmt.Errorf("profile is required")
	}
	if _, ok := s.Profile(name); !ok {
		return fmt.Errorf("profile %s not found", name)
	}
	s.data["current"] = name
	if record {
		s.pending = append(s.pending, storeMutation{kind: storeMutationUseProfile, name: name})
	}
	return nil
}

func (s *Store) SetCurrentProfile(name string) error {
	return s.setCurrentProfile(name, true)
}

func (s *Store) setCurrentProfile(name string, record bool) error {
	if name == "" {
		return fmt.Errorf("profile is required")
	}
	s.profileMap(name)
	s.data["current"] = name
	if record {
		s.pending = append(s.pending, storeMutation{kind: storeMutationSetCurrent, name: name})
	}
	return nil
}

func (s *Store) NativeOAuthProfileState(name string) NativeOAuthProfileState {
	if name == "" {
		name = s.Current()
	}
	if name == "" {
		name = DefaultProfileName
	}
	profile, exists := s.profileMapExisting(name)
	if !exists {
		profile = map[string]any{"name": name}
	}
	siteType := strings.ToUpper(stringField(profile, "oauth_site_type"))
	if siteType != "CN" && siteType != "INTL" {
		siteType = ""
	}
	return NativeOAuthProfileState{
		Name: name, SiteType: siteType,
		Generation:     stringField(profile, "oauth_generation"),
		AccountID:      stringField(profile, "oauth_account_id"),
		Current:        s.Current(),
		Exists:         exists,
		ConfigExisted:  s.existed,
		AuthGeneration: CredentialProfileAuthDigest(profile),
	}
}

func (s *Store) SetNativeOAuthProfile(state NativeOAuthProfileState, siteType, generation, accountID string) error {
	return s.setNativeOAuthProfile(
		state.Name, siteType, generation, accountID,
		state.Current, state.Exists, state.ConfigExisted, state.AuthGeneration, true,
	)
}

func (s *Store) setNativeOAuthProfile(
	name, siteType, generation, accountID, expectedCurrent string,
	expectedExists, expectedConfigExisted bool,
	expectedAuthGeneration [sha256.Size]byte,
	record bool,
) error {
	var err error
	name, err = NormalizeOAuthProfileName(name)
	if err != nil {
		return err
	}
	siteType = strings.ToUpper(strings.TrimSpace(siteType))
	if siteType != "CN" && siteType != "INTL" {
		return fmt.Errorf("OAuth site type must be CN or INTL")
	}
	if strings.TrimSpace(generation) == "" {
		return fmt.Errorf("OAuth login generation is required")
	}
	if accountID != "" {
		accountID, err = NormalizeOAuthAccountID(accountID)
		if err != nil {
			return err
		}
	}
	if s.existed != expectedConfigExisted {
		return ErrCredentialProfileChanged
	}

	profile, exists := s.profileMapExisting(name)
	if exists != expectedExists {
		return ErrCredentialProfileChanged
	}
	if !exists {
		profile = map[string]any{"name": name, "output_format": "json"}
	}
	if CredentialProfileAuthDigest(profile) != expectedAuthGeneration {
		return ErrCredentialProfileChanged
	}
	for _, key := range credentialProfileAuthKeys {
		delete(profile, key)
	}
	profile["mode"] = "OAuth"
	profile["oauth_site_type"] = siteType
	profile["oauth_generation"] = generation
	if accountID != "" {
		profile["oauth_account_id"] = accountID
	}
	if !exists {
		rawProfiles, _ := s.data["profiles"].([]any)
		s.data["profiles"] = append(rawProfiles, profile)
	}
	if s.Current() == expectedCurrent {
		s.data["current"] = name
	}
	if record {
		s.pending = append(s.pending, storeMutation{
			kind: storeMutationSetOAuth, name: name,
			oauth: nativeOAuthMutation{
				siteType: siteType, generation: generation, accountID: accountID,
				expectedCurrent: expectedCurrent, expectedExists: expectedExists,
				expectedConfigExisted:  expectedConfigExisted,
				expectedAuthGeneration: expectedAuthGeneration,
			},
		})
	}
	return nil
}

func NormalizeOAuthProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("profile is required")
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return "", fmt.Errorf("profile contains control characters")
	}
	return name, nil
}

func NormalizeOAuthAccountID(accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if len(accountID) != 16 {
		return "", fmt.Errorf("OAuth account ID must contain exactly 16 digits")
	}
	for _, value := range accountID {
		if value < '0' || value > '9' {
			return "", fmt.Errorf("OAuth account ID must contain exactly 16 digits")
		}
	}
	return accountID, nil
}

func (s *Store) applyMutation(mutation storeMutation, record bool) error {
	switch mutation.kind {
	case storeMutationSetValue:
		_, err := s.setValue(mutation.name, mutation.key, mutation.value, record)
		return err
	case storeMutationUseProfile:
		return s.useProfile(mutation.name, record)
	case storeMutationSetCurrent:
		return s.setCurrentProfile(mutation.name, record)
	case storeMutationSetOAuth:
		return s.setNativeOAuthProfile(
			mutation.name, mutation.oauth.siteType, mutation.oauth.generation, mutation.oauth.accountID,
			mutation.oauth.expectedCurrent, mutation.oauth.expectedExists, mutation.oauth.expectedConfigExisted,
			mutation.oauth.expectedAuthGeneration, record,
		)
	default:
		return fmt.Errorf("unsupported configuration mutation %s", mutation.kind)
	}
}

func (s *Store) ensureShape() {
	if s.data == nil {
		s.data = map[string]any{}
	}
	if _, ok := s.data["current"].(string); !ok {
		s.data["current"] = DefaultProfileName
	}
	if _, ok := s.data["profiles"].([]any); !ok {
		s.data["profiles"] = []any{map[string]any{
			"name":          DefaultProfileName,
			"output_format": "json",
		}}
	}
}

func (s *Store) profileMaps() []map[string]any {
	s.ensureShape()
	rawProfiles, _ := s.data["profiles"].([]any)
	profiles := make([]map[string]any, 0, len(rawProfiles))
	for _, raw := range rawProfiles {
		if profile, ok := raw.(map[string]any); ok {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func (s *Store) profileMap(name string) map[string]any {
	if profile, ok := s.profileMapExisting(name); ok {
		return profile
	}
	rawProfiles, _ := s.data["profiles"].([]any)
	profile := map[string]any{
		"name":          name,
		"output_format": "json",
	}
	s.data["profiles"] = append(rawProfiles, profile)
	return profile
}

func (s *Store) profileMapExisting(name string) (map[string]any, bool) {
	s.ensureShape()
	rawProfiles, _ := s.data["profiles"].([]any)
	for _, raw := range rawProfiles {
		profile, ok := raw.(map[string]any)
		if ok && stringField(profile, "name") == name {
			return profile, true
		}
	}
	return nil, false
}

func newStore(path string) *Store {
	store := &Store{path: path, data: map[string]any{}}
	store.ensureShape()
	return store
}

func cloneConfigMap(values map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	cloned, err := decodeStoreData(raw)
	if err != nil {
		return nil, err
	}
	return cloned, nil
}

func lookupConfigItem(key string) (ConfigItem, bool) {
	key = strings.TrimSpace(key)
	if alias, ok := configKeyAliases[key]; ok {
		key = alias
	}
	for _, item := range configItems {
		if item.Key == key {
			return item, true
		}
	}
	return ConfigItem{}, false
}

func validateConfigValue(item ConfigItem, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", item.Key)
	}
	switch item.Key {
	case "region":
		if !ValidRegion(value) {
			return fmt.Errorf("invalid region %s", value)
		}
	case "lang":
		if value != "en" && value != "zh-CN" {
			return fmt.Errorf("language %s is not supported", value)
		}
	case "output":
		if value != "json" && value != "text" {
			return fmt.Errorf("output mode %s is not supported", value)
		}
	case "telemetry.enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("telemetry.enabled must be true or false")
		}
	}
	return nil
}

// TelemetryEnabled reads the global, profile-independent telemetry switch.
// Missing configuration defaults to true; malformed configuration fails
// closed so telemetry can never make an otherwise usable CLI fail.
func TelemetryEnabled(path string) bool {
	store, err := LoadStore(path)
	if err != nil {
		return false
	}
	value, err := store.GetValue("", "telemetry.enabled", false)
	return err == nil && value.Value != "false"
}

func telemetryEnabledValue(data map[string]any) (bool, error) {
	rawTelemetry, exists := data["telemetry"]
	if !exists {
		return true, nil
	}
	telemetryMap, ok := rawTelemetry.(map[string]any)
	if !ok || telemetryMap == nil {
		return false, fmt.Errorf("telemetry must be an object")
	}
	rawEnabled, exists := telemetryMap["enabled"]
	if !exists {
		return true, nil
	}
	enabled, ok := rawEnabled.(bool)
	if !ok {
		return false, fmt.Errorf("telemetry.enabled must be a boolean")
	}
	return enabled, nil
}

func displayConfigValue(item ConfigItem, value string, showSecret bool) string {
	if !item.Sensitive || showSecret || value == "" {
		return value
	}
	return "********"
}

func mergeProfile(base Profile, overlay Profile) Profile {
	if overlay.Name != "" {
		base.Name = overlay.Name
	}
	if overlay.Mode != "" {
		base.Mode = overlay.Mode
	}
	if overlay.Region != "" {
		base.Region = overlay.Region
	}
	if overlay.AccessKeyID != "" {
		base.AccessKeyID = overlay.AccessKeyID
	}
	if overlay.AccessKeySecret != "" {
		base.AccessKeySecret = overlay.AccessKeySecret
	}
	if overlay.SecurityToken != "" {
		base.SecurityToken = overlay.SecurityToken
	}
	if overlay.Language != "" {
		base.Language = overlay.Language
	}
	if overlay.Output != "" {
		base.Output = overlay.Output
	}
	return base
}

func profileField(profile Profile, key string) string {
	switch key {
	case "region":
		return profile.Region
	case "access-key-id":
		return profile.AccessKeyID
	case "access-key-secret":
		return profile.AccessKeySecret
	case "security-token":
		return profile.SecurityToken
	case "lang":
		return profile.Language
	case "output":
		return profile.Output
	default:
		return ""
	}
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
