package runner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

type templateCreateIntent struct {
	journalfile.TemplateCreateIntent
	path, journal, command string
	cfg                    execpkg.Config
	sourceEnv              []string
}

var writeTemplateCreateIntent = journalfile.WriteExclusiveDurable

func templateCreateFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("native Template create", pflag.ContinueOnError)
	for _, name := range []string{"region", "profile", "lang", "output", "name", "team-id", "runtime-config", "build-config", "timeout"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "steps", "start-cmd", "ready-cmd", "tag"} {
		flags.String(name, "", "")
	}
	flags.Bool("force", false, "")
	for _, name := range []string{"openapi", "no-wait", "json", "agent-envelope", "no-color", "help", "version"} {
		flags.Bool(name, false, "")
	}
	return flags
}

// Classification shares flag types across family, mode and action scanners.
// Operation-specific parsers still enforce the actual native command grammar.
func commandPathFlags() *pflag.FlagSet {
	flags := templateCreateFlags()
	for _, name := range []string{"step", "build-id", "cursor", "limit", "next-token", "filter", "alias", "disk-size-mb", "fields", "description", "resource-group", "plan", "request", "api-param", "aliyun-arg", "status", "policy-name", "policy-type", "fallback-default-version", "cluster", "instance-name", "cpu-cores", "memory-gb"} {
		if flags.Lookup(name) == nil {
			flags.String(name, "", "")
		}
	}
	flags.Int("page", 0, "")
	flags.Bool("public", false, "")
	flags.Bool("dry-run", false, "")
	flags.Bool("all", false, "")
	return flags
}

func isNativeTemplateCreate(command string) bool {
	native, _ := nativeTemplateCreateStatus(command)
	return native
}

// Stop as soon as a different product/resource is known; its own flags and
// default-resource command layout are outside Template grammar.
func templateCommandFamily(command string) (bool, error) {
	args, err := shlex.Split(command)
	if err != nil {
		return false, err
	}
	if len(args) == 0 || args[0] != "ecctl" {
		return false, nil
	}
	flags := commandPathFlags()
	pos := []string{}
	afterDash := false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if arg == "--" && !afterDash {
			afterDash = true
			continue
		}
		if !afterDash && strings.HasPrefix(arg, "-") {
			key, _, inline := strings.Cut(arg, "=")
			flag := flags.Lookup(strings.TrimLeft(key, "-"))
			if key == "-o" {
				flag = flags.Lookup("output")
			}
			if key == "-h" {
				flag = flags.Lookup("help")
			}
			if key == "-v" {
				flag = flags.Lookup("version")
			}
			if flag == nil {
				return false, fmt.Errorf("cannot classify ecctl flag before command family")
			}
			if !inline && flag.NoOptDefVal == "" {
				index++
				if index >= len(args) {
					return false, fmt.Errorf("missing command flag value")
				}
			}
			continue
		}
		pos = append(pos, arg)
		if len(pos) == 1 && pos[0] != "sandbox" && pos[0] != "sbx" {
			return false, nil
		}
		if len(pos) == 2 {
			return pos[1] == "template" || pos[1] == "tpl", nil
		}
	}
	return false, nil
}

func nativeTemplateCommandStatus(command string) (bool, error) {
	family, familyErr := templateCommandFamily(command)
	if familyErr == nil && !family {
		return false, nil
	}
	native, err := templateNativeSelector(command)
	if err != nil || !native {
		return false, err
	}
	// Uncertainty must not become a different family and skip native binding.
	return true, familyErr
}

// Select transport before imposing native grammar. Unknown E2B flags remain
// the CLI's responsibility; known string flags consume selector-like values.
func templateNativeSelector(command string) (bool, error) {
	args, err := shlex.Split(command)
	if err != nil {
		return false, err
	}
	flags := commandPathFlags()
	flags.ParseErrorsWhitelist.UnknownFlags = true
	if err := flags.Parse(args[1:]); err != nil {
		return false, fmt.Errorf("invalid Template transport selector")
	}
	return flags.GetBool("openapi")
}

func (c *cleanup) bindOwnedNativeTemplate(ctx context.Context, command, role string, cfg execpkg.Config, timeout time.Duration) (string, execpkg.Config, error) {
	native, err := nativeTemplateCommandStatus(command)
	if err != nil || !native {
		return command, cfg, err
	}
	args, err := shlex.Split(command)
	if err != nil {
		return "", cfg, fmt.Errorf("unsafe owned Template grammar")
	}
	flags := templateCreateFlags()
	for _, key := range []string{"limit", "next-token", "fields"} {
		flags.String(key, "", "")
	}
	if flags.Parse(args[1:]) != nil {
		return "", cfg, fmt.Errorf("unsafe owned Template grammar")
	}
	pos := flags.Args()
	native, _ = flags.GetBool("openapi")
	if !native || len(pos) < 3 {
		return command, cfg, nil
	}
	action := pos[2]
	switch action {
	case "show", "describe":
		action = "get"
	case "ls":
		action = "list"
	case "rm", "remove":
		action = "delete"
	}
	if action != "get" && action != "list" && action != "update" && action != "delete" {
		return command, cfg, nil
	}
	if action == "list" && len(pos) != 3 || action != "list" && len(pos) != 4 {
		return "", cfg, fmt.Errorf("unsafe owned Template target")
	}
	team, _ := flags.GetString("team-id")
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	if role == "" {
		role = "primary"
	}
	var binding *journalfile.TemplateCreateIntent
	var bindingDone, ambiguous, ownedList bool
	c.mu.Lock()
	for _, item := range c.ownedTemplates {
		if item.role != role || item.templateBinding == nil {
			continue
		}
		owned, ok, parseErr := parseNativeTemplateDelete(item.cmd)
		if parseErr != nil || !ok {
			continue
		}
		if action == "list" {
			if team != owned.team {
				continue
			}
			ownedList = true
			if flags.Lookup("profile").Changed && profile != item.templateBinding.Profile {
				continue
			}
			// Prefer active ownership, retaining completed evidence as a
			// fallback for an explicitly selected historical profile.
			if binding != nil && bindingDone != item.done {
				if item.done {
					continue
				}
				binding, ambiguous = nil, false
			}
		} else if pos[3] != owned.id {
			continue
		}
		copy := *item.templateBinding
		if binding != nil && (binding.Profile != copy.Profile || binding.Identity != copy.Identity || binding.TeamID != copy.TeamID) {
			ambiguous = true
		}
		binding = &copy
		bindingDone = item.done
	}
	c.mu.Unlock()
	if ambiguous {
		return "", cfg, fmt.Errorf("ambiguous owned Template binding")
	}
	if binding == nil {
		if ownedList && flags.Lookup("profile").Changed {
			return "", cfg, fmt.Errorf("owned Template operation conflicts with its Team, region or profile")
		}
		return command, cfg, nil
	}
	if team != binding.TeamID || flags.Lookup("region").Changed && region != binding.Region || flags.Lookup("profile").Changed && profile != binding.Profile || cfg.Region != binding.Region {
		return "", cfg, fmt.Errorf("owned Template operation conflicts with its Team, region or profile")
	}
	if err := binding.ValidateConfig(cfg.Env); err != nil {
		return "", cfg, err
	}
	bound := binding.BoundConfig(cfg)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	_, err = execpkg.ReadIdentity(probeCtx, bound, binding.Profile)
	cancel()
	if err != nil {
		return "", cfg, err
	}
	normalized := []string{"ecctl", "sandbox", "template", action}
	if action != "list" {
		normalized = append(normalized, pos[3])
	}
	flags.Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "profile", "region":
		default:
			normalized = append(normalized, "--"+flag.Name+"="+flag.Value.String())
		}
	})
	normalized = append(normalized, "--region", binding.Region, "--profile", binding.Profile)
	return quotedTemplateCommand(normalized), bound, nil
}

func nativeTemplateCreateStatus(command string) (bool, error) {
	native, selectorErr := nativeTemplateCommandStatus(command)
	if selectorErr != nil || !native {
		return false, selectorErr
	}
	path, pathErr := commandAction(command)
	if pathErr != nil {
		return false, pathErr
	}
	if len(path) != 3 || (path[0] != "sandbox" && path[0] != "sbx") || (path[1] != "template" && path[1] != "tpl") || (path[2] != "create" && path[2] != "new") {
		return false, nil
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 || args[0] != "ecctl" {
		return false, fmt.Errorf("unsafe native Template create grammar")
	}
	flags := templateCreateFlags()
	if flags.Parse(args[1:]) != nil {
		return false, fmt.Errorf("unsafe native Template create grammar")
	}
	pos := flags.Args()
	native, _ = flags.GetBool("openapi")
	if len(pos) != 3 {
		return false, fmt.Errorf("unsafe native Template create target")
	}
	return native, nil
}

func reserveTemplateCreate(ctx context.Context, command string, cfg execpkg.Config, cl *cleanup, st scenario.Step, timeout time.Duration) (*templateCreateIntent, error) {
	if cl.session != nil && cl.session.hasRunTeamParent() {
		return nil, fmt.Errorf("native Template allocation with a shared run-lifetime Team is unsupported; use a case-owned lifecycle")
	}
	args, err := shlex.Split(command)
	if err != nil {
		return nil, err
	}
	flags := templateCreateFlags()
	if err := flags.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("invalid native Template recovery input")
	}
	pos := flags.Args()
	if len(pos) != 3 || (pos[0] != "sandbox" && pos[0] != "sbx") || (pos[1] != "template" && pos[1] != "tpl") || (pos[2] != "create" && pos[2] != "new") {
		return nil, fmt.Errorf("Template recovery requires exactly native Template create")
	}
	native, _ := flags.GetBool("openapi")
	help, _ := flags.GetBool("help")
	version, _ := flags.GetBool("version")
	team, _ := flags.GetString("team-id")
	region, _ := flags.GetString("region")
	if !native || help || version || !safeTeamIdentity(team) || flags.Lookup("region").Changed && region != cfg.Region || st.Teardown == "" || st.TeardownRegion != "" && st.TeardownRegion != "primary" {
		return nil, fmt.Errorf("Template create requires explicit Team, matching region, JSON output and primary cleanup")
	}
	meta, _, journal := cl.journalValues(&cleanupItem{role: "primary"})
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(journal); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("native creation requires the real cleanup journal path")
	}
	canonicalJournal, err := journalfile.ResolveJournalPath(journal)
	if err != nil {
		return nil, err
	}
	cleanupCfg := cl.execCfg["primary"]
	if cl.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || cfg.Region == "" || cfg.Bin == "" || !scenario.Surface(meta.Surface).Valid() || cleanupCfg.Region != cfg.Region || cleanupCfg.Bin != cfg.Bin {
		return nil, fmt.Errorf("Template create requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	// Freeze inherited selectors for the create and its read-only reconciliation.
	cfg.Env = append(append([]string{}, os.Environ()...), cfg.Env...)
	sourceEnv := append([]string{}, cfg.Env...)
	paths, err := journalfile.CaptureTemplateConfig(sourceEnv)
	if err != nil {
		return nil, err
	}
	preflight := journalfile.TemplateCreateIntent{ConfigPaths: paths}
	cfg.Env = preflight.BoundConfig(cfg).Env
	profile, _ := flags.GetString("profile")
	if profile == "" {
		for _, name := range []string{"ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE"} {
			if value := journalfile.TemplateEnvironmentValue(name, cfg.Env); value != "" {
				profile = value
				break
			}
		}
	}
	profileCtx, cancel := context.WithTimeout(ctx, timeout)
	profile, err = configuredTemplateProfile(profileCtx, cfg, profile)
	cancel()
	if err != nil {
		return nil, err
	}
	identityCtx, identityCancel := context.WithTimeout(ctx, timeout)
	identity, err := execpkg.ReadIdentity(identityCtx, cfg, profile)
	identityCancel()
	if err != nil {
		return nil, err
	}
	cfg.ExpectedIdentity = &identity
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	owner := hex.EncodeToString(nonce[:])
	name := "ecctl-p-" + owner
	intent := &templateCreateIntent{TemplateCreateIntent: journalfile.TemplateCreateIntent{Version: 2, RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin, TeamID: team, Name: name, Profile: profile, Selectors: map[string]string{}, ConfigPaths: paths, Identity: identity, CreatedAt: time.Now().UTC(), State: "pending"}, path: journal + ".template-create-" + owner + ".json", journal: journal, cfg: cfg, sourceEnv: sourceEnv}
	for _, key := range journalfile.TemplateSelectorNames {
		intent.Selectors[key] = journalfile.TemplateEnvironmentValue(key, sourceEnv)
	}
	actual := []string{"ecctl", "--output=json", "--agent-envelope=false", "--region", cfg.Region}
	if profile != "" {
		actual = append(actual, "--profile", profile)
	}
	actual = append(actual, "sandbox", "template", "create", "--name", name)
	flags.Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "name", "profile", "region", "output", "json", "agent-envelope", "help", "version":
		default:
			actual = append(actual, "--"+flag.Name+"="+flag.Value.String())
		}
	})

	intent.command = quotedTemplateCommand(actual)
	body, _ := json.MarshalIndent(intent.TemplateCreateIntent, "", "  ")
	err = journalfile.WithReplayLock(ctx, journal, func() error {
		required, _, err := cl.requiredTemplateJournal("primary", meta, journal)
		if err != nil {
			return err
		}
		resolved, err := journalfile.ResolveJournalPath(journal)
		if err != nil || resolved != canonicalJournal {
			return fmt.Errorf("native creation journal target changed before reservation")
		}
		if err := journalfile.CheckTemplateCreateIdentity(journal, required, sourceEnv, identity); err != nil {
			return err
		}
		// Retained intents still require their original profiles at cleanup,
		// including after an earlier delete. Prove those prerequisites before
		// another allocation, outside the short journal transaction lock.
		cleanupCfg := cfg
		cleanupCfg.Env = sourceEnv
		if _, err := journalfile.PrepareTemplateCleanup(ctx, journal, required, cleanupCfg); err != nil {
			return fmt.Errorf("validate retained native Template cleanup credentials before allocation: %w", err)
		}
		var reserved report.CleanupJournal
		err = journalfile.WithLock(context.Background(), journal, func() error {
			existing, legacy, err := readRunnerJournal(journal)
			if os.IsNotExist(err) {
				if required.NativeJournalPath != "" {
					return fmt.Errorf("required native ownership journal is missing")
				}
				existing = meta
				if err := writeRunnerJournal(journal, meta); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if legacy || existing.Version != 2 || existing.RunID != meta.RunID || existing.ExecutionID != meta.ExecutionID || existing.RegionRole != meta.RegionRole || existing.Region != meta.Region || existing.Surface != meta.Surface || existing.EcctlBin != meta.EcctlBin {
				return fmt.Errorf("existing Template journal binding differs")
			}
			if err := journalfile.CheckTemplateCreateIdentity(journal, required, sourceEnv, identity); err != nil {
				return err
			}
			if err := writeTemplateCreateIntent(intent.path, append(body, '\n')); err != nil {
				return err
			}
			reference, err := journalfile.ResolveJournalPath(intent.path)
			if err != nil {
				return err
			}
			if existing.NativeJournalPath != "" && existing.NativeJournalPath != canonicalJournal {
				return fmt.Errorf("native ownership journal anchor changed")
			}
			existing.NativeJournalPath = canonicalJournal
			existing.NativeIntentPaths = append(existing.NativeIntentPaths, reference)
			if err := writeRunnerJournal(journal, existing); err != nil {
				return err
			}
			reserved = existing
			return nil
		})
		if err != nil {
			return err
		}
		// Keep the exclusive replay lock, but release the file lock before
		// taking the cleanup mutex used by append/retirement transactions.
		cl.mu.Lock()
		if cl.nativeJournals == nil {
			cl.nativeJournals = make(map[string]report.CleanupJournal)
		}
		reserved.NativeIntentPaths = append([]string(nil), reserved.NativeIntentPaths...)
		cl.nativeJournals["primary"] = reserved
		cl.mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve Template create intent: %w", err)
	}
	return intent, nil
}

func configuredTemplateProfile(ctx context.Context, cfg execpkg.Config, requested string) (string, error) {
	for _, key := range []string{"ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"} {
		if strings.EqualFold(strings.TrimSpace(journalfile.TemplateEnvironmentValue(key, cfg.Env)), "true") {
			return "", fmt.Errorf("native Template creation requires a bound configured profile; ignored profiles are unsafe for cleanup")
		}
	}
	// This public read-only command reports only configuration metadata. Pin its
	// selected current profile before mutation; never persist credential values.
	args := []string{"ecctl", "configure", "get", "--output=json", "--agent-envelope=false"}
	if requested != "" {
		args = append(args, "--profile", requested)
	}
	result := execpkg.Run(ctx, cfg, quotedTemplateCommand(args))
	root, _ := result.JSON.(map[string]any)
	profile, _ := root["profile"].(string)
	mode, _ := root["mode"].(string)
	if result.Err != nil || result.Exit != 0 || strings.TrimSpace(profile) == "" || strings.ContainsAny(profile, "\x00\r\n\t") || mode == "" || requested != "" && profile != requested {
		return "", fmt.Errorf("native Template creation cannot resolve a bound configured profile before mutation")
	}
	return profile, nil
}

func quotedTemplateCommand(args []string) string {
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = teamCommandArgument(arg)
	}
	return strings.Join(quoted, " ")
}

func (intent *templateCreateIntent) deleteCommand(id string) string {
	args := []string{"ecctl", "sandbox", "template", "delete", id, "--openapi", "--region", intent.Region, "--team-id", intent.TeamID}
	if intent.Profile != "" {
		args = append(args, "--profile", intent.Profile)
	}
	return quotedTemplateCommand(args)
}

func (intent *templateCreateIntent) save() error {
	body, err := json.MarshalIndent(intent.TemplateCreateIntent, "", "  ")
	if err != nil {
		return err
	}
	return journalfile.WithExistingParentLock(context.Background(), intent.journal, func() error { return journalfile.WriteDurable(intent.path, append(body, '\n')) })
}

func finishTemplateCreate(res execpkg.Result, cl *cleanup, scope *[]*cleanupItem, data map[string]any, lockKeys []string, timeout time.Duration, intent *templateCreateIntent) error {
	digest := func(text string) string { sum := sha256.Sum256([]byte(text)); return hex.EncodeToString(sum[:]) }
	intent.Exit = &res.Exit
	intent.Started = &res.Started
	intent.StdoutSHA256 = digest(res.Stdout)
	intent.StderrSHA256 = digest(res.Stderr)
	if !res.Started {
		intent.State = "not_started"
		if err := intent.save(); err != nil {
			return fmt.Errorf("retain unstarted Template intent %s: %w", intent.path, err)
		}
		return fmt.Errorf("Template create did not start; intent %s records no allocation", intent.path)
	}
	if code := templatePreMutationFailure(res); code != "" {
		intent.State, intent.NoAllocationCode = "not_allocated", code
		if err := intent.save(); err != nil {
			return fmt.Errorf("retain no-allocation Template evidence %s: %w", intent.path, err)
		}
		// No allocation needs recovery. The authored exit and assertions still
		// decide whether this retained client failure is an expected result.
		return nil
	}
	if err := intent.save(); err != nil {
		return fmt.Errorf("retain first Template result in intent %s: %w", intent.path, err)
	}
	root, _ := res.JSON.(map[string]any)
	template, _ := root["template"].(map[string]any)
	id, _ := template["id"].(string)
	commands := []string{}
	if safeTeamIdentity(id) {
		if team, exists := template["team_id"]; exists && team != intent.TeamID {
			return fmt.Errorf("Template response Team mismatch; pending intent %s", intent.path)
		}
		commands = append(commands, intent.deleteCommand(id))
	} else if hasRecoveryCommand(res.JSON) {
		command, ok := recoveryDeleteCommand(res.JSON)
		if !ok || intent.ValidateDelete(command) != nil {
			return fmt.Errorf("cleanup recovery rejected; original target retained in stdout; pending intent %s", intent.path)
		}
		commands = append(commands, command)
	} else {
		if intent.Profile == "" {
			return fmt.Errorf("unknown Template creation with unbound current profile; pending intent %s", intent.path)
		}
		if err := intent.ValidateConfig(intent.sourceEnv); err != nil {
			return fmt.Errorf("pending intent %s: %w", intent.path, err)
		}
		identityCtx, identityCancel := context.WithTimeout(context.Background(), timeout)
		_, identityErr := execpkg.ReadIdentity(identityCtx, intent.cfg, intent.Profile)
		identityCancel()
		if identityErr != nil {
			return fmt.Errorf("pending intent %s: %w", intent.path, identityErr)
		}
		args := []string{"ecctl", "sandbox", "template", "list", "--openapi", "--team-id", intent.TeamID, "--region", intent.Region, "--profile", intent.Profile, "--limit", "100", "--output=json", "--agent-envelope=false"}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		listed := execpkg.Run(ctx, intent.cfg, quotedTemplateCommand(args))
		cancel()
		if listed.Err != nil || listed.Exit != 0 {
			return fmt.Errorf("Template reconciliation failed; pending intent %s", intent.path)
		}
		list, _ := listed.JSON.(map[string]any)
		items, validItems := list["templates"].([]any)
		pagination, _ := list["pagination"].(map[string]any)
		more, validPage := pagination["has_more"].(bool)
		if !validItems || !validPage || more {
			return fmt.Errorf("Template reconciliation incomplete; pending intent %s", intent.path)
		}
		seen := map[string]bool{}
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Template reconciliation item; pending intent %s", intent.path)
			}
			if item["name"] != intent.Name {
				continue
			}
			id, _ := item["id"].(string)
			if item["team_id"] != intent.TeamID || !safeTeamIdentity(id) || seen[id] {
				return fmt.Errorf("Template reconciliation ownership mismatch; pending intent %s", intent.path)
			}
			seen[id] = true
			commands = append(commands, intent.deleteCommand(id))
		}
		if len(commands) == 0 {
			return fmt.Errorf("Template absence unproven; pending intent %s", intent.path)
		}
	}
	for _, command := range commands {
		if err := intent.ValidateDelete(command); err != nil {
			return fmt.Errorf("Template cleanup evidence rejected; pending intent %s: %w", intent.path, err)
		}
		if err := cl.push(scope, caseScope(data), command, "primary", lockKeys, &intent.TemplateCreateIntent); err != nil {
			return fmt.Errorf("Template cleanup journal failed; pending intent %s: %w", intent.path, err)
		}
	}
	intent.State = "journaled"
	intent.Deletes = commands
	if err := intent.save(); err != nil {
		return fmt.Errorf("Template intent commit uncertainty %s: %w", intent.path, err)
	}
	if res.Err == nil && res.Exit == 0 && safeTeamIdentity(id) {
		return nil
	}
	return fmt.Errorf("first Template create failed; intent %s journaled %d owned cleanup target(s)", intent.path, len(commands))
}

func templatePreMutationFailure(res execpkg.Result) string {
	if res.Err != nil || !res.Started || res.Exit != 1 || hasRecoveryCommand(res.JSON) {
		return ""
	}
	root, _ := res.JSON.(map[string]any)
	if _, exists := root["template"]; exists {
		return ""
	}
	failure, _ := root["error"].(map[string]any)
	code, _ := failure["code"].(string)
	retryable, valid := failure["retryable"].(bool)
	if failure["kind"] != "client" || !valid || retryable || !journalfile.TemplatePreMutationFailureCode(code) {
		return ""
	}
	return code
}

// All destructive cleanup for the same journal is deferred while an allocation
// remains unknown. The replay lock serializes this check with new reservations.
func (c *cleanup) withTemplateCleanupGuard(ctx context.Context, role string, run func(execpkg.Config) error, command ...string) error {
	return c.withTemplateCleanupGuardBefore(ctx, role, false, nil, run, command...)
}

func quotaOwnedCleanupCommand(journal report.CleanupJournal, command string) bool {
	if journalfile.IsQuotaDeleteCandidate(command) {
		return true
	}
	target, err := teamDeleteTargetForOwnership(command, journal.Region)
	if err != nil {
		return false
	}
	for _, entry := range journal.Entries {
		id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
		if parseErr == nil && id == target && entry.TeamCreateConfirmed && entry.QuotaBound {
			return true
		}
	}
	return false
}

func templateOwnedCleanupTeam(journal report.CleanupJournal, command string) (string, bool) {
	target, err := teamDeleteTargetForOwnership(command, journal.Region)
	if err != nil {
		return "", false
	}
	for _, entry := range journal.Entries {
		if entry.QuotaBound {
			continue
		}
		id, parseErr := journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, journal.Region)
		if parseErr == nil && id == target {
			return target, true
		}
	}
	return "", false
}

// The in-memory journal template deliberately has no entries. Ownership of a
// retained Team must be decided from the durable journal under the replay lock.
func readGuardJournal(path string, expected report.CleanupJournal) (report.CleanupJournal, error) {
	current, legacy, err := readRunnerJournal(path)
	if os.IsNotExist(err) {
		if expected.QuotaBinding != nil || expected.NativeJournalPath != "" {
			return current, fmt.Errorf("protected cleanup journal is missing")
		}
		return expected, nil
	}
	if err != nil {
		return current, err
	}
	if legacy {
		if expected.QuotaBinding != nil || expected.NativeJournalPath != "" {
			return current, fmt.Errorf("protected cleanup requires a version 2 journal")
		}
		expected.Entries = current.Entries
		return expected, nil
	}
	if current.Version != expected.Version || current.RunID != expected.RunID || current.ExecutionID != expected.ExecutionID || current.RegionRole != expected.RegionRole || current.Region != expected.Region || current.Surface != expected.Surface || current.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(current.QuotaBinding, expected.QuotaBinding) || current.QuotaBinding != nil && current.JournalPath != path {
		return current, fmt.Errorf("protected cleanup journal binding changed")
	}
	if expected.JournalPath != "" && current.JournalPath != expected.JournalPath {
		return current, fmt.Errorf("protected cleanup journal path changed")
	}
	return current, nil
}

func (c *cleanup) withTemplateCleanupGuardBefore(ctx context.Context, role string, existingParent bool, before func() error, run func(execpkg.Config) error, command ...string) error {
	if role == "" {
		role = "primary"
	}
	if c.journal == "" {
		return run(c.execCfg[role])
	}
	if _, err := c.checkedJournalPath(role); err != nil {
		return err
	}
	meta, _, requested := c.journalValues(&cleanupItem{role: role})
	meta, path, err := c.requiredTemplateJournal(role, meta, requested)
	if err != nil {
		return err
	}
	knownParent, err := c.rememberedJournalParent(path)
	if err != nil {
		return err
	}
	lock := journalfile.WithCleanupLock
	if existingParent || knownParent != nil {
		lock = journalfile.WithExistingCleanupLock
	}
	return lock(ctx, path, func() error {
		if err := checkJournalParentIdentity(path, knownParent); err != nil {
			return err
		}
		if _, err := c.checkedJournalPath(role); err != nil {
			return err
		}
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		meta, canonical, err := c.requiredTemplateJournal(role, meta, requested)
		if err != nil {
			return err
		}
		current, err := readGuardJournal(canonical, meta)
		if err != nil {
			return err
		}
		if err := checkJournalParentIdentity(path, knownParent); err != nil {
			return err
		}
		// Every destructive command shares this pending-intent and historical
		// identity check. Only owned commands receive the returned binding.
		templateBound, err := journalfile.PrepareTemplateCleanup(ctx, canonical, meta, c.execCfg[role])
		if err != nil {
			return err
		}
		if len(command) > 0 {
			if err := checkJournalParentIdentity(path, knownParent); err != nil {
				return err
			}
			repaired, err := c.repairTeamBeforeCleanupBinding(role, command[0])
			if err != nil {
				return err
			}
			if repaired {
				current, err = readGuardJournal(canonical, meta)
				if err != nil {
					return err
				}
			}
		}
		bound := c.execCfg[role]
		nativeDelete := false
		if len(command) > 0 {
			_, nativeDelete, _ = parseNativeTemplateDelete(command[0])
		}
		if len(command) > 0 && quotaOwnedCleanupCommand(current, command[0]) {
			if current.QuotaBinding == nil {
				return fmt.Errorf("protected Quota credential binding is missing")
			}
			if err := checkJournalParentIdentity(path, knownParent); err != nil {
				return err
			}
			bound, err = journalfile.PrepareQuotaCleanup(ctx, canonical, current, c.execCfg[role])
		} else if len(command) > 0 && (journalfile.IsTemplateCacheDelete(command[0]) || nativeDelete) {
			bound = templateBound
		} else if len(command) > 0 {
			if teamID, owned := templateOwnedCleanupTeam(current, command[0]); owned {
				bound, _, err = journalfile.TemplateTeamCleanupBinding(canonical, current, c.execCfg[role], teamID)
			}
		}
		if err != nil {
			return err
		}
		if err := checkJournalParentIdentity(path, knownParent); err != nil {
			return err
		}
		return run(bound)
	})
}

func (c *cleanup) requiredTemplateJournal(role string, meta report.CleanupJournal, requested string) (report.CleanupJournal, string, error) {
	if _, err := c.checkedJournalPath(role); err != nil {
		return meta, "", err
	}
	c.mu.Lock()
	retained := c.nativeJournals[role]
	retained.NativeIntentPaths = append([]string(nil), retained.NativeIntentPaths...)
	c.mu.Unlock()
	if retained.NativeJournalPath == "" {
		return meta, requested, nil
	}
	resolved, err := journalfile.ResolveJournalPath(requested)
	if err != nil || resolved != retained.NativeJournalPath {
		return meta, "", fmt.Errorf("retained native ownership journal target changed")
	}
	meta.NativeJournalPath, meta.NativeIntentPaths = retained.NativeJournalPath, retained.NativeIntentPaths
	return meta, retained.NativeJournalPath, nil
}

// Parse the command path, not arbitrary values after it. Root/resource string
// flags consume their value even when that value is itself an action name.
func commandAction(command string) ([]string, error) {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 || args[0] != "ecctl" {
		return nil, fmt.Errorf("invalid ecctl command grammar")
	}
	flags := commandPathFlags()
	pos := []string{}
	afterDash := false
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if arg == "--" && !afterDash {
			afterDash = true
			continue
		}
		if !afterDash && strings.HasPrefix(arg, "-") {
			key, _, inline := strings.Cut(arg, "=")
			flag := flags.Lookup(strings.TrimLeft(key, "-"))
			if key == "-o" {
				flag = flags.Lookup("output")
			}
			if key == "-h" {
				flag = flags.Lookup("help")
			}
			if key == "-v" {
				flag = flags.Lookup("version")
			}
			if flag == nil {
				return nil, fmt.Errorf("cannot classify ecctl command flag before action")
			}
			if !inline && flag.NoOptDefVal == "" {
				index++
				if index >= len(args) {
					return nil, fmt.Errorf("missing command flag value")
				}
			}
			continue
		}
		pos = append(pos, arg)
		if len(pos) == 3 && pos[0] == "call" {
			return pos, nil
		}
		if len(pos) >= 2 {
			switch arg {
			case "create", "new", "get", "show", "describe", "list", "ls", "update", "delete", "rm", "remove":
				// Resource-parent nouns may precede the action. Stop at the
				// action so its IDs and flag values cannot become verbs.
				return pos, nil
			}
		}
	}
	return nil, fmt.Errorf("cannot classify ecctl command action")
}

func isDestructiveStep(command string) bool {
	path, err := commandAction(command)
	if err != nil {
		// An unclassified ecctl command can still mutate. Let the shared
		// journal guard decide; parsing failure is never a safe-operation proof.
		args, _ := shlex.Split(command)
		return len(args) > 0 && args[0] == "ecctl"
	}
	if len(path) < 2 {
		return false
	}
	if path[0] == "call" {
		return len(path) == 3 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(path[2])), "delete")
	}
	action := path[len(path)-1]
	return action == "delete" || action == "rm" || action == "remove"
}

type nativeTemplateDeleteTarget struct {
	id, team, region, profile string
	regionSet, profileSet     bool
	args                      []string
}

func parseNativeTemplateDelete(command string) (nativeTemplateDeleteTarget, bool, error) {
	native, selectorErr := nativeTemplateCommandStatus(command)
	if selectorErr != nil || !native {
		return nativeTemplateDeleteTarget{}, false, selectorErr
	}
	path, pathErr := commandAction(command)
	recognized := pathErr == nil && len(path) == 3 && (path[0] == "sandbox" || path[0] == "sbx") && (path[1] == "template" || path[1] == "tpl") && (path[2] == "delete" || path[2] == "remove" || path[2] == "rm")
	if !recognized {
		return nativeTemplateDeleteTarget{}, false, pathErr
	}
	args, err := shlex.Split(command)
	if err != nil {
		return nativeTemplateDeleteTarget{}, true, fmt.Errorf("unsafe native Template delete grammar")
	}
	flags := pflag.NewFlagSet("native Template delete", pflag.ContinueOnError)
	for _, key := range []string{"region", "profile", "team-id", "lang", "output"} {
		flags.String(key, "", "")
	}
	for _, key := range []string{"openapi", "json", "agent-envelope", "no-color"} {
		flags.Bool(key, false, "")
	}
	if flags.Parse(args[1:]) != nil {
		return nativeTemplateDeleteTarget{}, true, fmt.Errorf("unsafe native Template delete grammar")
	}
	native, _ = flags.GetBool("openapi")
	if !native {
		return nativeTemplateDeleteTarget{}, false, nil
	}
	pos := flags.Args()
	if len(pos) != 4 || !safeTeamIdentity(pos[3]) {
		return nativeTemplateDeleteTarget{}, true, fmt.Errorf("unsafe native Template delete target")
	}
	team, _ := flags.GetString("team-id")
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	if team != "" && !safeTeamIdentity(team) {
		return nativeTemplateDeleteTarget{}, true, fmt.Errorf("unsafe native Template delete Team")
	}
	normalized := []string{"ecctl", "sandbox", "template", "delete", pos[3]}
	flags.Visit(func(flag *pflag.Flag) { normalized = append(normalized, "--"+flag.Name+"="+flag.Value.String()) })
	return nativeTemplateDeleteTarget{id: pos[3], team: team, region: region, profile: profile, regionSet: flags.Lookup("region").Changed, profileSet: flags.Lookup("profile").Changed, args: normalized}, true, nil
}

// Bind a case's explicit deletion to its already proven native allocation.
// Missing selectors use the creation binding; conflicting explicit selectors
// are errors before execution. Presentation flags and command aliases survive.
func (c *cleanup) bindNativeTemplateDelete(scope []*cleanupItem, command, role string) (string, error) {
	target, ok, err := parseNativeTemplateDelete(command)
	if err != nil {
		return "", err
	}
	if !ok {
		return command, nil
	}
	if role == "" {
		role = "primary"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range scope {
		owned, ok, parseErr := parseNativeTemplateDelete(item.cmd)
		if parseErr != nil || !ok || item.role != role || owned.id != target.id || item.templateBinding == nil {
			continue
		}
		if err := item.templateBinding.ValidateDelete(item.cmd); err != nil {
			return "", err
		}
		region := target.region
		if !target.regionSet {
			region = c.execCfg[role].Region
		}
		if owned.profile == "" || owned.region == "" || target.team != owned.team || region != owned.region || target.profileSet && target.profile != owned.profile {
			return "", fmt.Errorf("native Template deletion conflicts with its owned Team, region or profile")
		}
		args := append([]string{}, target.args...)
		if !target.regionSet {
			args = append(args, "--region", owned.region)
		}
		if !target.profileSet {
			args = append(args, "--profile", owned.profile)
		}
		return quotedTemplateCommand(args), nil
	}
	return "", fmt.Errorf("native Template deletion requires exact retained creation ownership")
}

func nativeTemplateDeletesEquivalent(left, right string, cfg execpkg.Config) bool {
	a, aOK, aErr := parseNativeTemplateDelete(left)
	b, bOK, bErr := parseNativeTemplateDelete(right)
	if aErr != nil || bErr != nil || !aOK || !bOK || a.profile == "" || a.region == "" || !safeTeamIdentity(a.team) {
		return false
	}
	if !b.regionSet {
		b.region = cfg.Region
	}
	return a.id == b.id && a.team == b.team && a.region == b.region && a.profile == b.profile
}
