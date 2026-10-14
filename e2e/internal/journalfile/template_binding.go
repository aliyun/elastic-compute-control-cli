package journalfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
