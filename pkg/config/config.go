package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ecerrors "ecctl/pkg/errors"
)

const DefaultProfileName = "default"

type Store struct {
	path string
	data map[string]any
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
	if explicit != "" {
		return ResolveRegion(explicit, getenv)
	}
	if getenv != nil {
		if region := getenv("ECCTL_REGION"); region != "" {
			return ResolveRegion(region, nil)
		}
	}
	loaded, _, err := EffectiveProfile(profile, configPath, AliyunConfigPath(getenv))
	if err != nil {
		return "", ecerrors.Client("InvalidConfig", err.Error())
	}
	if loaded.Region != "" {
		return ResolveRegion(loaded.Region, nil)
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
				return ResolveRegion(region, nil)
			}
		}
	}
	return "", ecerrors.Client("MissingRegion", "region is required")
}

func ValidRegion(region string) bool {
	if region == "not-a-real-region" {
		return false
	}
	parts := strings.Split(region, "-")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
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
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newStore(path), nil
	}
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string]any{}
	}
	store := &Store{path: path, data: data}
	store.ensureShape()
	return store, nil
}

func LoadExistingStore(path string) (*Store, bool, error) {
	if path == "" {
		path = EcctlConfigPath(os.Getenv)
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, false, err
	}
	if data == nil {
		data = map[string]any{}
	}
	store := &Store{path: path, data: data}
	store.ensureShape()
	return store, true, nil
}

func (s *Store) Save() error {
	s.ensureShape()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o600)
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
	profile := s.profileMap(name)
	value := stringField(profile, item.StoredAs)
	if item.Key == "output" && value == "" {
		value = "json"
	}
	return ConfigValue{Key: item.Key, Value: displayConfigValue(item, value, showSecret), Sensitive: item.Sensitive}, nil
}

func (s *Store) SetValue(name string, key string, value string) (ConfigValue, error) {
	item, ok := lookupConfigItem(key)
	if !ok {
		return ConfigValue{}, fmt.Errorf("unknown config key %s", key)
	}
	if err := validateConfigValue(item, value); err != nil {
		return ConfigValue{}, err
	}
	if name == "" {
		name = s.Current()
	}
	profile := s.profileMap(name)
	profile[item.StoredAs] = value
	if item.Key == "access-key-id" || item.Key == "access-key-secret" || item.Key == "security-token" {
		profile["mode"] = "AK"
	}
	s.data["current"] = name
	return ConfigValue{Key: item.Key, Value: displayConfigValue(item, value, false), Sensitive: item.Sensitive}, nil
}

func (s *Store) SetRegion(name string, region string) error {
	_, err := s.SetValue(name, "region", region)
	return err
}

func (s *Store) UseProfile(name string) error {
	if name == "" {
		return fmt.Errorf("profile is required")
	}
	if _, ok := s.Profile(name); !ok {
		return fmt.Errorf("profile %s not found", name)
	}
	s.data["current"] = name
	return nil
}

func (s *Store) SetCurrentProfile(name string) error {
	if name == "" {
		return fmt.Errorf("profile is required")
	}
	s.profileMap(name)
	s.data["current"] = name
	return nil
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
	s.ensureShape()
	rawProfiles, _ := s.data["profiles"].([]any)
	for _, raw := range rawProfiles {
		profile, ok := raw.(map[string]any)
		if ok && stringField(profile, "name") == name {
			return profile
		}
	}
	profile := map[string]any{
		"name":          name,
		"output_format": "json",
	}
	s.data["profiles"] = append(rawProfiles, profile)
	return profile
}

func newStore(path string) *Store {
	store := &Store{path: path, data: map[string]any{}}
	store.ensureShape()
	return store
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
	}
	return nil
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
