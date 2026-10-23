package runner

import (
	"context"
	"fmt"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

type templateCacheCommand struct {
	action, id, team, region, profile string
	hasRegion, hasProfile             bool
	args                              []string
}

// The cache has the Template's ID. Only a retained native Template creation
// can authorize a pre-create cache cleanup target in this E2E run.
func (c *cleanup) ownedTemplateForCache(scope []*cleanupItem, id, team, role string) (*journalfile.TemplateCreateIntent, error) {
	var binding *journalfile.TemplateCreateIntent
	for _, item := range scope {
		if item.done || item.role != role || item.templateBinding == nil {
			continue
		}
		owned, native, err := parseNativeTemplateDelete(item.cmd)
		if err != nil {
			return nil, err
		}
		if !native || owned.id != id || owned.team != team {
			continue
		}
		if binding != nil {
			return nil, fmt.Errorf("ambiguous owned Template Cache target")
		}
		copy := *item.templateBinding
		copy.Deletes = []string{item.cmd}
		binding = &copy
	}
	if binding == nil || binding.TeamID != team || binding.Region != c.execCfg[role].Region || binding.Profile == "" {
		return nil, fmt.Errorf("Template Cache requires exact retained native Template ownership")
	}
	return binding, nil
}

func parseTemplateCacheCommand(command string) (templateCacheCommand, bool, error) {
	path, pathErr := commandAction(command)
	if pathErr != nil {
		if strings.Contains(command, "template-cache") || strings.Contains(command, "tcache") {
			return templateCacheCommand{}, true, pathErr
		}
		return templateCacheCommand{}, false, nil
	}
	if len(path) != 3 || (path[0] != "sandbox" && path[0] != "sbx") || (path[1] != "template-cache" && path[1] != "tcache") || (path[2] != "create" && path[2] != "delete") {
		return templateCacheCommand{}, false, nil
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 4 || args[0] != "ecctl" {
		return templateCacheCommand{}, true, fmt.Errorf("unsafe Template Cache command")
	}
	flags := pflag.NewFlagSet("Template Cache", pflag.ContinueOnError)
	for _, name := range []string{"template-id", "team-id", "region", "profile", "timeout", "output"} {
		flags.String(name, "", "")
	}
	seen := map[string]bool{}
	for _, arg := range args[1:] {
		key, _, _ := strings.Cut(arg, "=")
		if flags.Lookup(strings.TrimPrefix(key, "--")) != nil && strings.HasPrefix(key, "--") {
			if seen[key] {
				return templateCacheCommand{}, true, fmt.Errorf("duplicate Template Cache flag %s", key)
			}
			seen[key] = true
		}
	}
	if err := flags.Parse(args[1:]); err != nil {
		return templateCacheCommand{}, true, fmt.Errorf("unsafe Template Cache flags: %w", err)
	}
	positions := flags.Args()
	if len(positions) < 3 || positions[0] != path[0] || positions[1] != path[1] || positions[2] != path[2] {
		return templateCacheCommand{}, true, fmt.Errorf("Template Cache command path is invalid")
	}
	team, _ := flags.GetString("team-id")
	if !safeTeamIdentity(team) {
		return templateCacheCommand{}, true, fmt.Errorf("Template Cache command requires a safe Team ID")
	}
	output, _ := flags.GetString("output")
	if output != "json" {
		return templateCacheCommand{}, true, fmt.Errorf("Template Cache recovery requires JSON output")
	}
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	parsed := templateCacheCommand{action: path[2], team: team, region: region, profile: profile, hasRegion: flags.Lookup("region").Changed, hasProfile: flags.Lookup("profile").Changed, args: args}
	if parsed.action == "create" {
		if len(positions) != 3 {
			return templateCacheCommand{}, true, fmt.Errorf("Template Cache create has unexpected positionals")
		}
		parsed.id, _ = flags.GetString("template-id")
	} else {
		if len(positions) != 4 || flags.Lookup("template-id").Changed {
			return templateCacheCommand{}, true, fmt.Errorf("Template Cache delete has unexpected target")
		}
		parsed.id = positions[3]
	}
	if !safeTeamIdentity(parsed.id) {
		return templateCacheCommand{}, true, fmt.Errorf("Template Cache command requires a safe Template ID")
	}
	return parsed, true, nil
}

func boundTemplateCacheCommand(parsed templateCacheCommand, binding *journalfile.TemplateCreateIntent) string {
	args := append(append([]string{}, parsed.args...), "--region", binding.Region, "--profile", binding.Profile)
	return quotedTemplateCommand(args)
}

func confirmedTemplateCacheReady(result execpkg.Result, reservation *cleanupItem) bool {
	if result.Err != nil || result.Exit != 0 {
		return false
	}
	target, recognized, err := parseTemplateCacheCommand(reservation.cmd)
	if err != nil || !recognized || target.action != "delete" {
		return false
	}
	root, ok := result.JSON.(map[string]any)
	if !ok {
		return false
	}
	cache, ok := root["template_cache"].(map[string]any)
	return ok && cache["id"] == target.id && cache["team_id"] == target.team && cache["status"] == "Success"
}

func (c *cleanup) reserveTemplateCacheCreate(ctx context.Context, scope *[]*cleanupItem, command string, cfg execpkg.Config, data map[string]any, step scenario.Step, lockKeys []string) (string, execpkg.Config, *cleanupItem, error) {
	create, recognized, err := parseTemplateCacheCommand(command)
	if err != nil || !recognized || create.action != "create" {
		return command, cfg, nil, err
	}
	id, _ := data["native_template_id"].(string)
	team, _ := data["team_id"].(string)
	if id != create.id || team != create.team || create.hasRegion || create.hasProfile || step.Teardown == "" || step.TeardownRegion != "" && step.TeardownRegion != "primary" {
		return "", cfg, nil, fmt.Errorf("Template Cache create does not match the run-owned Team and Template")
	}
	binding, err := c.ownedTemplateForCache(*scope, id, team, "primary")
	if err != nil {
		return "", cfg, nil, err
	}
	teardown, err := vars.Render(step.Teardown, data)
	if err != nil {
		return "", cfg, nil, err
	}
	remove, recognized, err := parseTemplateCacheCommand(teardown)
	if err != nil || !recognized || remove.action != "delete" || remove.id != id || remove.team != team || remove.hasRegion || remove.hasProfile {
		return "", cfg, nil, fmt.Errorf("Template Cache create requires one exact delete finalizer")
	}
	boundDelete := boundTemplateCacheCommand(remove, binding)
	if err := journalfile.ValidateOwnedTemplateCacheDelete(boundDelete, *binding); err != nil {
		return "", cfg, nil, fmt.Errorf("Template Cache create finalizer is not replayable: %w", err)
	}
	meta := c.journalMeta
	if c.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || meta.Surface == "" || cfg.Region == "" || cfg.Bin == "" {
		return "", cfg, nil, fmt.Errorf("Template Cache create requires a run-bound cleanup journal")
	}
	if err := binding.ValidateConfig(cfg.Env); err != nil {
		return "", cfg, nil, err
	}
	var bound execpkg.Config
	var reservation *cleanupItem
	err = c.withTemplateCleanupGuard(ctx, "primary", func(verified execpkg.Config) error {
		if verified.ExpectedIdentity == nil || *verified.ExpectedIdentity != binding.Identity {
			return fmt.Errorf("Template Cache principal differs from retained Template ownership")
		}
		bound = verified
		var pushErr error
		reservation, pushErr = c.pushPendingCacheCreate(scope, caseScope(data), boundDelete, "primary", lockKeys)
		return pushErr
	}, boundDelete)
	if err != nil {
		return "", cfg, nil, fmt.Errorf("reserve Template Cache cleanup: %w", err)
	}
	return boundTemplateCacheCommand(create, binding), bound, reservation, nil
}

func (c *cleanup) bindOwnedTemplateCacheDelete(scope []*cleanupItem, command string, cfg execpkg.Config) (string, error) {
	remove, recognized, err := parseTemplateCacheCommand(command)
	if err != nil || !recognized || remove.action != "delete" {
		return command, err
	}
	if c.journal == "" {
		return "", fmt.Errorf("Template Cache deletion requires a run-bound cleanup journal")
	}
	for _, item := range scope {
		if item.done || item.role != "primary" || !strings.Contains(item.cmd, "template-cache") && !strings.Contains(item.cmd, "tcache") {
			continue
		}
		reserved, yes, parseErr := parseTemplateCacheCommand(item.cmd)
		if parseErr != nil {
			return "", parseErr
		}
		if !yes || reserved.id != remove.id || reserved.team != remove.team {
			continue
		}
		if item.cacheCreatePending {
			return "", fmt.Errorf("Template Cache create remains pending")
		}
		binding, err := c.ownedTemplateForCache(scope, remove.id, remove.team, "primary")
		if err != nil {
			return "", err
		}
		if err := binding.ValidateConfig(cfg.Env); err != nil {
			return "", err
		}
		if remove.hasRegion || remove.hasProfile || !reserved.hasRegion || !reserved.hasProfile || reserved.region != binding.Region || reserved.profile != binding.Profile {
			return "", fmt.Errorf("Template Cache deletion conflicts with retained region or profile")
		}
		bound := boundTemplateCacheCommand(remove, binding)
		if !commandsEquivalent(item.cmd, bound) {
			return "", fmt.Errorf("Template Cache deletion differs from its reserved finalizer")
		}
		if err := journalfile.ValidateOwnedTemplateCacheDelete(bound, *binding); err != nil {
			return "", err
		}
		return bound, nil
	}
	return "", fmt.Errorf("Template Cache deletion requires exact retained creation ownership")
}

// The caller holds c.mu and the shared replay fence through the cloud call.
func (c *cleanup) verifyReservedTemplateCacheDelete(scope []*cleanupItem, command string) error {
	remove, recognized, err := parseTemplateCacheCommand(command)
	if err != nil || !recognized || remove.action != "delete" {
		return fmt.Errorf("expected an exact Template Cache delete")
	}
	for _, item := range scope {
		if item.done || item.cacheCreatePending || item.role != "primary" || !commandsEquivalent(item.cmd, command) {
			continue
		}
		binding, err := c.ownedTemplateForCache(scope, remove.id, remove.team, "primary")
		if err != nil {
			return err
		}
		if err := journalfile.ValidateOwnedTemplateCacheDelete(command, *binding); err != nil {
			return err
		}
		if _, err := c.checkedJournalPath(item.role); err != nil {
			return err
		}
		template, entry, path := c.journalValues(item)
		return journalfile.WithExistingParentLock(context.Background(), path, func() error {
			journal, _, err := readRunnerJournal(path)
			if err != nil {
				return err
			}
			if err := mergeJournalMetadata(&journal, template); err != nil {
				return err
			}
			matches := 0
			for _, candidate := range journal.Entries {
				if candidate == entry && !candidate.CacheCreatePending {
					matches++
				}
			}
			if matches != 1 {
				return fmt.Errorf("Template Cache deletion lacks one retained journal finalizer")
			}
			return nil
		})
	}
	return fmt.Errorf("Template Cache deletion lacks one active finalizer")
}
