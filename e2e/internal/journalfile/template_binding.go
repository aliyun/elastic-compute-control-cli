package journalfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

type TemplateConfigPath struct {
	Requested string `json:"requested"`
	Target    string `json:"target"`
}

func resolveTemplateConfigPath(requested string) (TemplateConfigPath, error) {
	if !filepath.IsAbs(requested) {
		return TemplateConfigPath{}, fmt.Errorf("native Template configuration paths must be absolute")
	}
	requested = filepath.Clean(requested)
	current := requested
	missing := []string{}
	for {
		target, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				target = filepath.Join(target, missing[index])
			}
			return TemplateConfigPath{Requested: requested, Target: target}, nil
		}
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return TemplateConfigPath{}, fmt.Errorf("cannot bind dangling native Template configuration symlink")
		}
		if !os.IsNotExist(err) || filepath.Dir(current) == current {
			return TemplateConfigPath{}, fmt.Errorf("cannot resolve native Template configuration target")
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func CaptureTemplateConfig(env []string) (map[string]TemplateConfigPath, error) {
	home := TemplateEnvironmentValue("HOME", env)
	if runtime.GOOS == "windows" {
		home = TemplateEnvironmentValue("USERPROFILE", env)
	}
	ecctl := TemplateEnvironmentValue("ECCTL_CONFIG_PATH", env)
	if ecctl == "" {
		if !filepath.IsAbs(home) {
			return nil, fmt.Errorf("native Template default configuration requires an absolute home")
		}
		ecctl = filepath.Join(home, ".ecctl", "config.json")
	}
	aliyun := ""
	paths := map[string]TemplateConfigPath{}
	for _, name := range []string{"ECCTL_ALIYUN_CONFIG_PATH", "ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
		value := TemplateEnvironmentValue(name, env)
		if value == "" {
			continue
		}
		bound, err := resolveTemplateConfigPath(value)
		if err != nil {
			return nil, err
		}
		paths[name] = bound
		if aliyun == "" {
			aliyun = value
		}
	}
	if aliyun == "" {
		if !filepath.IsAbs(home) {
			return nil, fmt.Errorf("native Template default configuration requires an absolute home")
		}
		aliyun = filepath.Join(home, ".aliyun", "config.json")
	}
	for name, value := range map[string]string{"ecctl": ecctl, "aliyun": aliyun} {
		bound, err := resolveTemplateConfigPath(value)
		if err != nil {
			return nil, err
		}
		paths[name] = bound
	}
	return paths, nil
}

func (intent TemplateCreateIntent) ValidateConfig(env []string) error {
	paths, err := CaptureTemplateConfig(env)
	if err != nil {
		return err
	}
	if len(paths) != len(intent.ConfigPaths) {
		return fmt.Errorf("native Template configuration binding differs")
	}
	for key, path := range paths {
		if stored, ok := intent.ConfigPaths[key]; !ok || stored != path {
			return fmt.Errorf("native Template configuration target changed: %s", key)
		}
	}
	return nil
}

func (intent TemplateCreateIntent) BoundConfig(cfg execpkg.Config) execpkg.Config {
	cfg.Env = append(append([]string{}, cfg.Env...), "ECCTL_CONFIG_PATH="+intent.ConfigPaths["ecctl"].Target, "ECCTL_ALIYUN_CONFIG_PATH="+intent.ConfigPaths["aliyun"].Target)
	pin := intent.Identity
	cfg.ExpectedIdentity = &pin
	return cfg
}

func PrepareTemplateCleanup(ctx context.Context, path string, meta report.CleanupJournal, cfg execpkg.Config) (execpkg.Config, error) {
	intents, err := readTemplateCreateIntents(path, meta, cfg.Env)
	if err != nil {
		return cfg, err
	}
	var expected *execpkg.IdentityPin
	probed := map[string]bool{}
	for _, intent := range intents {
		if intent.State == "not_started" || intent.State == "not_allocated" {
			continue
		}
		if expected != nil && *expected != intent.Identity {
			return cfg, fmt.Errorf("native Template journal contains different credential principals")
		}
		if err := ctx.Err(); err != nil {
			return cfg, err
		}
		// Every intent was validated above. Only an identical selected binding
		// can share a successful probe, and only within this preflight call.
		binding, err := json.Marshal(struct {
			Profile     string
			ConfigPaths map[string]TemplateConfigPath
			Identity    execpkg.IdentityPin
			Selectors   map[string]string
		}{intent.Profile, intent.ConfigPaths, intent.Identity, intent.Selectors})
		if err != nil {
			return cfg, err
		}
		bound := intent.BoundConfig(cfg)
		if !probed[string(binding)] {
			probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := execpkg.ReadIdentity(probeCtx, bound, intent.Profile)
			cancel()
			if err != nil {
				return cfg, err
			}
			probed[string(binding)] = true
		}
		pin := intent.Identity
		expected = &pin
		cfg = bound
	}
	cfg.ExpectedIdentity = expected
	return cfg, nil
}

// TemplateTeamCleanupBinding selects an exact Team target's native Template owner.
// Callers first run PrepareTemplateCleanup under the shared replay fence, which
// checks pending intents and probes every retained profile. Same-principal
// profiles with the same source binding can share one original Team finalizer.
// A transferred Team keeps the exact source profile as well as its principal.
func TemplateTeamCleanupBinding(path string, meta report.CleanupJournal, cfg execpkg.Config, teamID string) (execpkg.Config, bool, error) {
	intents, err := readTemplateCreateIntents(path, meta, cfg.Env)
	if err != nil {
		return cfg, false, err
	}
	transferName, transferred := meta.VolumeTeamBindings[teamID]
	marked := 0
	confirmed := 0
	markerName := ""
	for _, entry := range meta.Entries {
		if !entry.TeamCreateConfirmed {
			continue
		}
		id, parseErr := TeamDeleteTarget(entry.Teardown)
		if parseErr != nil {
			return cfg, false, fmt.Errorf("invalid confirmed Team finalizer: %w", parseErr)
		}
		if id == teamID {
			if transferred && (entry.RegionRole != meta.RegionRole || entry.Region != meta.Region || entry.ExecutionID != meta.ExecutionID) {
				return cfg, false, fmt.Errorf("Volume destination Team journal selectors changed")
			}
			confirmed++
			if entry.VolumeTransferIntent != "" {
				marked++
				markerName = entry.VolumeTransferIntent
			}
		}
	}
	if transferred != (marked == 1) || marked > 1 || transferred && (transferName == "" || transferName != markerName || confirmed != 1) {
		return cfg, false, fmt.Errorf("Volume destination Team credential association is missing or ambiguous")
	}
	var selected *TemplateCreateIntent
	for i := range intents {
		intent := &intents[i]
		if intent.State != "journaled" || intent.TeamID != teamID && (!transferred || intent.Name != transferName) {
			continue
		}
		if intent.Name == transferName && transferred && intent.Resource != "volume" {
			return cfg, false, fmt.Errorf("Volume destination Team binding lacks a retained Volume intent")
		}
		if selected != nil && !sameTeamCredentialBinding(*selected, *intent, transferred) {
			return cfg, false, fmt.Errorf("native Template Team credential bindings conflict")
		}
		if !transferred || selected == nil || intent.Name == transferName {
			selected = intent
		}
	}
	if transferred && (selected == nil || selected.Name != transferName) {
		return cfg, false, fmt.Errorf("Volume destination Team binding lacks its retained creation intent")
	}
	if selected == nil {
		return cfg, false, nil
	}
	bound := selected.BoundConfig(cfg)
	bound.Profile = selected.Profile
	return bound, true, nil
}

func sameTeamCredentialBinding(left, right TemplateCreateIntent, exactProfile bool) bool {
	return left.Identity == right.Identity && reflect.DeepEqual(left.ConfigPaths, right.ConfigPaths) && reflect.DeepEqual(left.Selectors, right.Selectors) && (!exactProfile || left.Profile == right.Profile)
}

// VolumeTeamCreatorConfig selects the credential that created this exact Team.
// The marker and binding must have been journaled with its confirmed finalizer.
func VolumeTeamCreatorConfig(ctx context.Context, meta report.CleanupJournal, cfg execpkg.Config, teamID string) (execpkg.Config, bool, error) {
	marked, binding, err := volumeTeamCreator(meta, teamID)
	if err != nil || !marked {
		return cfg, false, err
	}
	bound, err := ProbeQuotaBinding(ctx, binding, cfg)
	return bound, true, err
}

func volumeTeamCreator(meta report.CleanupJournal, teamID string) (bool, *report.QuotaCredentialBinding, error) {
	count := 0
	marked := false
	var finalizer report.Resource
	for _, entry := range meta.Entries {
		if !entry.TeamCreateConfirmed {
			continue
		}
		id, err := TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return false, nil, err
		}
		if id == teamID {
			count++
			marked = entry.VolumeTeamBound
			finalizer = entry
		}
	}
	if count > 1 {
		return false, nil, fmt.Errorf("Volume Team creator is ambiguous")
	}
	binding, associated := meta.VolumeTeamCreators[teamID]
	if count == 0 {
		return false, nil, nil
	}
	if marked != associated || marked && (binding == nil || !reflect.DeepEqual(binding, meta.VolumeFixtureBinding)) {
		return false, nil, fmt.Errorf("Volume Team creator credential association is missing or ambiguous")
	}
	if marked {
		if err := ValidateVolumeFixtureIdentity(binding); err != nil {
			return false, nil, err
		}
		if err := CheckVolumeTeamCreationLink(meta, teamID, &finalizer); err != nil {
			return false, nil, err
		}
	}
	return marked, binding, nil
}

// CheckVolumeTeamCreatorSource compares a native Volume's retained source to
// the Team creator before a child allocation or transfer association is saved.
func CheckVolumeTeamCreatorSource(meta report.CleanupJournal, teamID string, source TemplateCreateIntent) error {
	if meta.VolumeFixtureBinding == nil && len(meta.VolumeTeamCreators) == 0 {
		return nil // Unbound executions retain existing native Team rules.
	}
	marked, binding, err := volumeTeamCreator(meta, teamID)
	if err != nil {
		return err
	}
	if !marked {
		return fmt.Errorf("Volume source lacks an active Team creator")
	}
	return checkVolumeSourceCreatorBinding(meta, source, binding)
}

// A terminal receipt retains the creator association after its Team finalizer
// has been consumed. It cannot authorize a new child allocation.
func CheckRetiredVolumeTeamCreatorSource(meta report.CleanupJournal, teamID string, source TemplateCreateIntent) error {
	if meta.VolumeFixtureBinding == nil && len(meta.VolumeTeamCreators) == 0 {
		return nil
	}
	binding, associated := meta.VolumeTeamCreators[teamID]
	if !associated || binding == nil || !reflect.DeepEqual(binding, meta.VolumeFixtureBinding) {
		return fmt.Errorf("Volume terminal creator association is missing or ambiguous")
	}
	if err := ValidateVolumeFixtureIdentity(binding); err != nil {
		return err
	}
	if err := CheckVolumeTeamCreationLink(meta, teamID, nil); err != nil {
		return err
	}
	return checkVolumeSourceCreatorBinding(meta, source, binding)
}

func checkVolumeSourceCreatorBinding(meta report.CleanupJournal, source TemplateCreateIntent, binding *report.QuotaCredentialBinding) error {
	if binding == nil || source.Resource != "volume" || source.RunID != meta.RunID || source.ExecutionID != meta.ExecutionID || source.RegionRole != meta.RegionRole || source.Region != meta.Region || source.Surface != meta.Surface || source.EcctlBin != meta.EcctlBin || source.Profile != binding.Profile || source.Identity != binding.Identity || !reflect.DeepEqual(source.Selectors, binding.Selectors) || len(source.ConfigPaths) != len(binding.ConfigPaths) {
		return fmt.Errorf("Volume source differs from Team creator credential binding")
	}
	for key, path := range source.ConfigPaths {
		stored, ok := binding.ConfigPaths[key]
		if !ok || stored.Requested != path.Requested || stored.Target != path.Target {
			return fmt.Errorf("Volume source configuration differs from Team creator: %s", key)
		}
	}
	return nil
}

// CheckTemplateChildTeamBinding keeps a transferred Team's exact source
// profile usable before a new native Template or Volume child is allocated.
func CheckTemplateChildTeamBinding(path string, meta report.CleanupJournal, cfg execpkg.Config, teamID string, prospective TemplateCreateIntent) error {
	if !safeTemplateID(teamID) || prospective.TeamID != teamID {
		return fmt.Errorf("native child Team binding is invalid")
	}
	name := meta.VolumeTeamBindings[teamID]
	if name == "" {
		return nil
	}
	if _, owned, err := TemplateTeamCleanupBinding(path, meta, cfg, teamID); err != nil {
		return err
	} else if !owned {
		return fmt.Errorf("transferred Team lacks a retained native source")
	}
	intents, err := readTemplateCreateIntents(path, meta, cfg.Env)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.Name != name {
			continue
		}
		if intent.Resource != "volume" || intent.State != "journaled" || !sameTeamCredentialBinding(intent, prospective, true) {
			return fmt.Errorf("native child conflicts with transferred Team credential binding")
		}
		return nil
	}
	return fmt.Errorf("transferred Team source intent is missing")
}

// CheckVolumeTransferTeamBinding verifies the candidate against retained
// creation evidence before the runner records a destination Team association.
// The caller holds the exclusive replay lock until the provider call ends.
func CheckVolumeTransferTeamBinding(path string, meta report.CleanupJournal, cfg execpkg.Config, teamID string, source TemplateCreateIntent) error {
	if source.Resource != "volume" || !safeTemplateID(teamID) {
		return fmt.Errorf("Volume transfer lacks a confirmed native creation binding")
	}
	if err := CheckVolumeTeamCreatorSource(meta, teamID, source); err != nil {
		return err
	}
	if binding := meta.VolumeTeamCreators[teamID]; binding != nil {
		if err := ValidateQuotaBinding(binding, cfg.Env); err != nil {
			return fmt.Errorf("Volume destination Team creator credential changed: %w", err)
		}
	}
	if _, _, err := TemplateTeamCleanupBinding(path, meta, cfg, teamID); err != nil {
		return err
	}
	intents, err := readTemplateCreateIntents(path, meta, cfg.Env)
	if err != nil {
		return err
	}
	sourceFound := false
	previousFound := meta.VolumeTeamBindings[teamID] == ""
	for _, intent := range intents {
		if intent.Name == source.Name {
			if intent.Resource != "volume" || intent.State != "journaled" || intent.RunID != source.RunID || intent.ExecutionID != source.ExecutionID || intent.RegionRole != source.RegionRole || intent.Region != source.Region || intent.Surface != source.Surface || intent.EcctlBin != source.EcctlBin || intent.TeamID != source.TeamID || !sameTeamCredentialBinding(intent, source, true) {
				return fmt.Errorf("Volume transfer source creation binding changed")
			}
			sourceFound = true
		}
		if intent.Name == meta.VolumeTeamBindings[teamID] {
			previousFound = true
		}
		if intent.State == "journaled" && (intent.TeamID == teamID || intent.Name == meta.VolumeTeamBindings[teamID]) && !sameTeamCredentialBinding(intent, source, true) {
			return fmt.Errorf("Volume destination Team has conflicting native credentials")
		}
	}
	if !sourceFound || !previousFound {
		return fmt.Errorf("Volume transfer lacks retained source or destination credential evidence")
	}
	return nil
}
