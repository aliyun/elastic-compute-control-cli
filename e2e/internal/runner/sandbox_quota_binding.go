package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// The owned Team is the Quota lifecycle's parent allocation. Resolve its
// configured profile and trusted principal before reserving that create.
func captureQuotaCredentialBinding(ctx context.Context, cfg execpkg.Config, timeout time.Duration) (*report.QuotaCredentialBinding, execpkg.Config, error) {
	sourceEnv := append(append([]string{}, os.Environ()...), cfg.Env...)
	for _, key := range []string{"ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"} {
		if strings.EqualFold(strings.TrimSpace(journalfile.TemplateEnvironmentValue(key, sourceEnv)), "true") {
			return nil, cfg, fmt.Errorf("protected Quota requires a configured selected profile")
		}
	}
	paths, err := journalfile.CaptureTemplateConfig(sourceEnv)
	if err != nil {
		return nil, cfg, err
	}
	probeCfg := cfg
	probeCfg.Env = append(sourceEnv, "ECCTL_CONFIG_PATH="+paths["ecctl"].Target, "ECCTL_ALIYUN_CONFIG_PATH="+paths["aliyun"].Target)
	requested := ""
	for _, key := range []string{"ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
		if value := journalfile.TemplateEnvironmentValue(key, sourceEnv); value != "" {
			requested = value
			break
		}
	}
	args := []string{"ecctl", "configure", "get", "--output=json", "--agent-envelope=false"}
	if requested != "" {
		args = append(args, "--profile", requested)
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := execpkg.Run(probeCtx, probeCfg, quotedTemplateCommand(args))
	root, _ := result.JSON.(map[string]any)
	profile, _ := root["profile"].(string)
	if result.Err != nil || result.Exit != 0 || profile == "" || strings.ContainsAny(profile, "\x00\r\n\t") || requested != "" && requested != profile {
		return nil, cfg, fmt.Errorf("protected Quota cannot resolve selected profile before Team allocation")
	}
	mode, err := journalfile.ResolveQuotaCredentialMode(profile, paths["ecctl"].Target, paths["aliyun"].Target, sourceEnv)
	if err != nil {
		return nil, cfg, err
	}
	identity, err := execpkg.ReadIdentity(probeCtx, probeCfg, profile)
	if err != nil {
		return nil, cfg, err
	}
	binding, err := journalfile.CaptureQuotaBinding(profile, mode, identity, sourceEnv)
	if err != nil {
		return nil, cfg, err
	}
	binding.ResolvedMode = true
	if err := journalfile.ValidateQuotaBinding(binding, sourceEnv); err != nil {
		return nil, cfg, err
	}
	return binding, journalfile.BoundQuotaConfig(binding, probeCfg), nil
}
