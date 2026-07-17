// Package fixtureconfig loads the top-level E2E run configuration.
package fixtureconfig

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 2

type sourceFile struct {
	Version int                    `yaml:"version"`
	Regions RegionConfig           `yaml:"regions"`
	Values  map[string]ValueSource `yaml:"values"`
	Paths   Paths                  `yaml:"paths"`
}

// RegionConfig describes ordered E2E region profiles. A profile is an atomic
// region plus the account resources that are usable in that region.
type RegionConfig struct {
	Candidates []RegionProfile `yaml:"candidates"`
}

// RegionProfile binds prerequisite resource bundles to a region. Supported
// bundles and fields are documented in e2e.yaml and README.md; the loader keeps
// this map schema-free so adding a case does not require another config schema.
type RegionProfile struct {
	ID            string                    `yaml:"id"`
	Prerequisites map[string]map[string]any `yaml:"prerequisites"`
}

// ResolvePrerequisites returns only the requested bundles as a nested map for
// the runner's .prerequisites template namespace.
func (p RegionProfile) ResolvePrerequisites(requirements []string) (map[string]any, error) {
	resolved := map[string]any{}
	seen := map[string]bool{}
	for _, name := range requirements {
		name = strings.TrimSpace(name)
		if seen[name] {
			continue
		}
		seen[name] = true
		bundle, ok := p.Prerequisites[name]
		if !ok {
			return nil, fmt.Errorf("region %q does not declare prerequisite bundle %q", p.ID, name)
		}
		if err := setNested(resolved, name, bundle); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

// HasPrerequisites reports whether every named bundle is declared by the
// profile. It is used by the execution planner before any cloud command runs.
func (p RegionProfile) HasPrerequisites(requirements []string) bool {
	for _, name := range requirements {
		if _, ok := p.Prerequisites[name]; !ok {
			return false
		}
	}
	return true
}

// Paths contains repository-relative paths used by the E2E runner.
type Paths struct {
	Cases           string `yaml:"cases"`
	Stack           string `yaml:"stack"`
	Inputs          string `yaml:"inputs"`
	Coverage        string `yaml:"coverage"`
	ParameterPolicy string `yaml:"parameter_policy"`
}

// RunConfig is the top-level E2E configuration loaded from e2e/e2e.yaml.
type RunConfig struct {
	Version int
	Regions RegionConfig
	Values  *Config
	Paths   Paths
}

// ValueSource supplies one literal run-level value. Environment-backed values
// are intentionally unsupported so E2E prerequisites have one reviewable
// source of truth.
type ValueSource struct {
	Value any `yaml:"value"`
}

// Config is the literal run-level value contract.
type Config struct {
	values map[string]ValueSource
}

// Load reads the literal run-level values from a top-level config file.
func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("run config path is required")
	}
	file, err := read(path)
	if err != nil {
		return nil, err
	}
	config := &Config{values: file.Values}
	if config.values == nil {
		config.values = map[string]ValueSource{}
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return config, nil
}

// LoadRunConfig loads and validates the complete top-level E2E configuration.
func LoadRunConfig(path string) (*RunConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("run config path is required")
	}
	file, err := read(path)
	if err != nil {
		return nil, err
	}
	if file.Version != CurrentVersion {
		return nil, fmt.Errorf("run config %q must use version %d", path, CurrentVersion)
	}
	if len(file.Regions.Candidates) == 0 {
		return nil, fmt.Errorf("run config %q requires at least one region profile", path)
	}
	seen := make(map[string]struct{}, len(file.Regions.Candidates))
	for i := range file.Regions.Candidates {
		profile := &file.Regions.Candidates[i]
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			return nil, fmt.Errorf("run config %q region profile %d id is empty", path, i)
		}
		if _, ok := seen[profile.ID]; ok {
			return nil, fmt.Errorf("run config %q contains duplicate region %q", path, profile.ID)
		}
		seen[profile.ID] = struct{}{}
		for name := range profile.Prerequisites {
			if err := validateKey(name); err != nil {
				return nil, fmt.Errorf("region %q prerequisite bundle: %w", profile.ID, err)
			}
		}
	}
	config := &Config{values: file.Values}
	if config.values == nil {
		config.values = map[string]ValueSource{}
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &RunConfig{
		Version: file.Version,
		Regions: file.Regions,
		Values:  config,
		Paths:   file.Paths,
	}, nil
}

// Keys returns the declared dotted run-level keys in stable order.
func (c *Config) Keys() []string {
	out := make([]string, 0, len(c.values))
	for key := range c.values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Has reports whether a dotted key is declared by the run config.
func (c *Config) Has(key string) bool {
	_, ok := c.values[key]
	return ok
}

// Resolve returns just the requested values as a nested template map.
func (c *Config) Resolve(requirements []string) (map[string]any, error) {
	resolved := map[string]any{}
	seen := map[string]bool{}
	for _, key := range requirements {
		if seen[key] {
			continue
		}
		seen[key] = true
		source, ok := c.values[key]
		if !ok {
			return nil, fmt.Errorf("run config does not declare %q", key)
		}
		if err := setNested(resolved, key, source.Value); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func read(path string) (sourceFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sourceFile{}, err
	}
	var file sourceFile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return sourceFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

func (c *Config) validate() error {
	for key, source := range c.values {
		if err := validateKey(key); err != nil {
			return err
		}
		if source.Value == nil {
			return fmt.Errorf("run config %q must declare value", key)
		}
	}
	return nil
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}
	for _, part := range strings.Split(key, ".") {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("invalid key %q", key)
		}
	}
	return nil
}

func setNested(values map[string]any, key string, value any) error {
	parts := strings.Split(key, ".")
	current := values
	for _, part := range parts[:len(parts)-1] {
		if child, ok := current[part]; ok {
			next, ok := child.(map[string]any)
			if !ok {
				return fmt.Errorf("run config key %q conflicts with %q", key, part)
			}
			current = next
			continue
		}
		next := map[string]any{}
		current[part] = next
		current = next
	}
	current[parts[len(parts)-1]] = value
	return nil
}
