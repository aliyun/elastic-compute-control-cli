// Package runner orchestrates an E2E run: provision the shared stack, run cases
// in bounded parallel with per-case isolation, tear everything down (two-level,
// signal-safe), and produce a result model.
package runner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	execpkg "ecctl/e2e/internal/exec"
	"ecctl/e2e/internal/fixtureconfig"
	"ecctl/e2e/internal/match"
	paramspkg "ecctl/e2e/internal/params"
	"ecctl/e2e/internal/report"
	"ecctl/e2e/internal/scenario"
	"ecctl/e2e/internal/vars"
)

// Options configures a run.
type Options struct {
	CasesDir           string
	StackFile          string
	InputsDir          string
	GlobalFixture      string
	ParameterMode      string
	ParameterPolicy    string
	CleanupJournal     string
	Region             string
	Regions            map[string]Region
	Zone               string
	RunName            string
	RunID              string
	ExecutionID        string
	Surface            string
	Parallel           int
	EcctlBin           string
	StepTimeout        time.Duration
	Keep               bool
	DryRun             bool
	Suites             []*scenario.Suite // pre-selected cases; if nil, load from CasesDir
	Env                []string
	Logf               func(string, ...any)
	PreResolvedLingjun *paramspkg.LingjunResult
}

// Region is one role's selected region profile and resolved prerequisite
// bundle namespace.
type Region struct {
	ID            string
	Prerequisites map[string]any
}

func normalizeRegions(opt *Options) (map[string]Region, error) {
	regions := make(map[string]Region, len(opt.Regions)+1)
	for role, region := range opt.Regions {
		role = strings.TrimSpace(role)
		region.ID = strings.TrimSpace(region.ID)
		if role == "" {
			return nil, fmt.Errorf("region role is required")
		}
		if role != "primary" && region.ID == "" {
			return nil, fmt.Errorf("region role %q id is required", role)
		}
		regions[role] = region
	}
	primary, ok := regions["primary"]
	if !ok {
		primary = Region{ID: strings.TrimSpace(opt.Region)}
	}
	if opt.Region != "" && primary.ID != "" && strings.TrimSpace(opt.Region) != primary.ID {
		return nil, fmt.Errorf("primary region %q conflicts with legacy region %q", primary.ID, opt.Region)
	}
	if primary.ID == "" {
		primary.ID = strings.TrimSpace(opt.Region)
	}
	regions["primary"] = primary
	opt.Region = primary.ID
	return regions, nil
}

// Run executes the suite and returns the result model. Like pytest, it never
// aborts early on test failures: shared-stack provisioning failures and per-case
// failures are recorded in the report and the run continues (cases that don't
// depend on the stack still run). An error is returned only for fatal setup
// problems (cannot load cases / fixtures).
func Run(ctx context.Context, opt Options) (*report.Run, error) {
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	if opt.Parallel <= 0 {
		opt.Parallel = 20
	}
	if opt.StepTimeout <= 0 {
		opt.StepTimeout = 10 * time.Minute
	}

	regions, err := normalizeRegions(&opt)
	if err != nil {
		return nil, err
	}
	run := &report.Run{RunID: opt.RunID, Region: opt.Region, Surface: opt.Surface, EcctlBin: opt.EcctlBin, StartedAt: time.Now()}
	// setupErr records a pre-run failure into run (so a report is still produced)
	// and returns the partial model alongside the error.
	setupErr := func(err error) (*report.Run, error) {
		run.Cases = append(run.Cases, report.Case{
			Name: "(setup)", Resource: "setup", Status: report.StatusError, Error: err.Error(),
		})
		run.Summary = summarize(run.Cases)
		run.FinishedAt = time.Now()
		return run, err
	}

	suites := opt.Suites
	if suites == nil {
		var err error
		if suites, err = scenario.LoadDir(opt.CasesDir); err != nil {
			return setupErr(fmt.Errorf("load cases: %w", err))
		}
	}
	if len(suites) == 0 {
		return setupErr(fmt.Errorf("no cases selected"))
	}

	// Load the shared-stack fixture once. Region and zone are selected by the
	// run-level config and dynamic parameter resolver, never by the stack DAG.
	var fix *Fixture
	if opt.StackFile != "" {
		if _, statErr := os.Stat(opt.StackFile); statErr == nil {
			f, err := loadFixture(opt.StackFile)
			if err != nil {
				return setupErr(err)
			}
			fix = f
		}
	}
	requestedStack := suiteNeeds(suites)
	if len(requestedStack) > 0 && fix == nil {
		return setupErr(fmt.Errorf("shared stack is required by selected cases"))
	}
	if fix != nil {
		planned, err := fix.plan(requestedStack)
		if err != nil {
			return setupErr(fmt.Errorf("plan shared stack: %w", err))
		}
		selected := *fix
		selected.Provision = planned
		fix = &selected
	}
	run.Region = opt.Region
	stackNeeded := fix != nil && len(fix.Provision) > 0 && len(requestedStack) > 0
	global, err := resolveGlobalFixture(opt, suites)
	if err != nil {
		return setupErr(err)
	}
	run.Region = opt.Region

	execCfgs := make(map[string]execpkg.Config, len(regions))
	for role, region := range regions {
		execCfgs[role] = execpkg.Config{Bin: opt.EcctlBin, Region: region.ID, Env: opt.Env}
	}
	execCfg := execCfgs["primary"]
	primaryPrerequisites := regions["primary"].Prerequisites
	if primaryPrerequisites == nil {
		primaryPrerequisites = map[string]any{}
	}
	parameters, parameterSkips, err := resolveParameters(ctx, opt, suites, fix, execCfg, primaryPrerequisites)
	if err != nil {
		return setupErr(err)
	}
	if len(parameterSkips) > 0 && fix != nil {
		runnableSuites := make([]*scenario.Suite, 0, len(suites)-len(parameterSkips))
		for _, suite := range suites {
			if parameterSkips[suite] == "" {
				runnableSuites = append(runnableSuites, suite)
			}
		}
		requestedStack = suiteNeeds(runnableSuites)
		planned, err := fix.plan(requestedStack)
		if err != nil {
			return setupErr(fmt.Errorf("replan shared stack after parameter skips: %w", err))
		}
		selected := *fix
		selected.Provision = planned
		fix = &selected
		stackNeeded = len(fix.Provision) > 0 && len(requestedStack) > 0
	}
	// Dynamic ECS resolution owns the selected zone. Keep the legacy top-level
	// .zone namespace synchronized so cases that predate .params.ecs.zone use
	// the same zone as the shared stack. An explicit --zone remains authoritative
	// because ECS resolution used it as a hard constraint above.
	if opt.Zone == "" {
		if selectedZone, ok := nestedString(parameters, "ecs", "zone"); ok {
			opt.Zone = selectedZone
		}
	}
	run.Parameters = parameters
	cl := newCleanup(execCfgs, opt.Keep, opt.CleanupJournal, report.CleanupJournal{RunID: opt.RunID, ExecutionID: opt.ExecutionID, Surface: opt.Surface}, opt.Logf)
	regionValues := make(map[string]any, len(regions))
	for role, region := range regions {
		prerequisites := region.Prerequisites
		if prerequisites == nil {
			prerequisites = map[string]any{}
		}
		regionValues[role] = map[string]any{"id": region.ID, "prerequisites": prerequisites}
	}
	base := map[string]any{
		"run_name":          opt.RunName,
		"run_id":            opt.RunID,
		"resource_prefix":   resourcePrefix(opt.RunName),
		"oss_export_prefix": ossExportPrefix(opt.RunID),
		"time": map[string]any{
			"monitor_start": run.StartedAt.Add(-time.Hour).UTC().Format(time.RFC3339),
			"monitor_end":   run.StartedAt.UTC().Format(time.RFC3339),
		},
		"region":        opt.Region,
		"regions":       regionValues,
		"zone":          opt.Zone,
		"global":        global,
		"prerequisites": primaryPrerequisites,
		"params":        parameters,
	}

	// Provision the shared stack only if some selected case needs it. A failure
	// is recorded as a pseudo-case and does NOT abort the run.
	stackVars := map[string]any{}
	var stackScope []*cleanupItem
	stackFailures := map[string]string{}
	var stackCase *report.Case
	if stackNeeded {
		sc := report.Case{Name: "(shared stack)", Resource: "stack", Status: report.StatusPass}
		start := time.Now()
		stackFailures = provisionStack(ctx, opt, execCfg, cl, &stackScope, base, stackVars, fix, &sc)
		sc.DurationMs = time.Since(start).Milliseconds()
		if len(stackFailures) > 0 {
			sc.Status = report.StatusFail
			sc.Error = formatStackFailures(fix.Provision, stackFailures)
			opt.Logf("shared stack has failed branches (dependent cases skipped): %s", sc.Error)
		}
		stackCase = &sc
	}
	base["stack"] = stackVars

	caseResults := make([]report.Case, len(suites))
	sem := make(chan struct{}, opt.Parallel)
	var wg sync.WaitGroup

	// runAndLog runs one case, bracketing it with start/finish lines so progress
	// is visible in real time (cases otherwise run silently until the summary).
	runAndLog := func(i int, s *scenario.Suite) report.Case {
		opt.Logf("RUN   %s", s.Resource)
		res := runCase(ctx, opt, execCfg, cl, base, s)
		opt.Logf("%-5s %s (%s)", caseStatusLabel(res.Status), s.Resource,
			(time.Duration(res.DurationMs) * time.Millisecond).Round(time.Second))
		return res
	}

	for i, s := range suites {
		if ctx.Err() != nil {
			caseResults[i] = report.Case{Name: caseName(s), Resource: s.Resource, Path: s.Path, Status: report.StatusSkipped, Error: "run cancelled"}
			opt.Logf("SKIP  %s (run cancelled)", s.Resource)
			continue
		}
		if reason := parameterSkips[s]; reason != "" {
			caseResults[i] = report.Case{Name: caseName(s), Resource: s.Resource, Path: s.Path, Status: report.StatusSkipped, Error: reason}
			opt.Logf("SKIP  %s (%s)", s.Resource, reason)
			continue
		}
		if failed := failedStackDependencies(fix, s.Needs, stackFailures); len(failed) > 0 {
			caseResults[i] = report.Case{Name: caseName(s), Resource: s.Resource, Path: s.Path, Status: report.StatusSkipped, Error: "shared stack dependencies failed: " + strings.Join(failed, ", ")}
			opt.Logf("SKIP  %s (shared stack dependencies failed: %s)", s.Resource, strings.Join(failed, ", "))
			continue
		}
		if s.Serial {
			// Serial cases are barriers: all earlier parallel cases must finish
			// before a case that may mutate shared account state begins.
			wg.Wait()
			caseResults[i] = runAndLog(i, s)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, s *scenario.Suite) {
			defer wg.Done()
			defer func() { <-sem }()
			caseResults[i] = runAndLog(i, s)
		}(i, s)
	}
	wg.Wait()
	if failures := cl.run(stackScope); len(failures) > 0 {
		if stackCase == nil {
			stackCase = &report.Case{Name: "(shared stack cleanup)", Resource: "stack", Status: report.StatusFail}
		} else {
			stackCase.Status = report.StatusFail
			if stackCase.Error != "" {
				stackCase.Error += "; "
			}
		}
		stackCase.Error += strings.Join(failures, "; ")
	}

	if stackCase != nil {
		run.Cases = append(run.Cases, *stackCase)
	}
	run.Cases = append(run.Cases, caseResults...)
	run.Manifest = cl.manifest
	run.Summary = summarize(run.Cases)
	run.FinishedAt = time.Now()
	return run, nil
}

func resolveGlobalFixture(opt Options, suites []*scenario.Suite) (map[string]any, error) {
	requirements := make([]string, 0)
	for _, suite := range suites {
		requirements = append(requirements, suite.RequiresGlobal...)
	}
	if len(requirements) == 0 {
		return map[string]any{}, nil
	}
	if opt.GlobalFixture == "" {
		return nil, fmt.Errorf("top-level run config is required by selected cases")
	}
	config, err := fixtureconfig.Load(opt.GlobalFixture)
	if err != nil {
		return nil, fmt.Errorf("load run config values: %w", err)
	}
	values, err := config.Resolve(requirements)
	if err != nil {
		return nil, fmt.Errorf("resolve run config values: %w", err)
	}
	return values, nil
}

func resolveParameters(ctx context.Context, opt Options, suites []*scenario.Suite, fix *Fixture, execCfg execpkg.Config, prerequisites map[string]any) (map[string]any, map[*scenario.Suite]string, error) {
	active := append([]*scenario.Suite(nil), suites...)
	skips := map[*scenario.Suite]string{}
	for {
		constraints, constrainedSuites, impossible := combinedECSConstraints(active)
		var values map[string]any
		var err error
		if impossible {
			err = &paramspkg.NoCompatibleECSCombinationError{Region: opt.Region, ExplicitZone: opt.Zone}
		} else {
			values, err = resolveParametersWithConstraints(ctx, opt, active, fix, execCfg, prerequisites, constraints)
		}
		if err == nil {
			if len(skips) == 0 {
				skips = nil
			}
			return values, skips, nil
		}

		var skippedNow []*scenario.Suite
		reason := ""
		switch {
		case paramspkg.IsNoCompatibleECSCombination(err) && len(constrainedSuites) > 0:
			skippedNow = constrainedSuites
			reason = "no ECS inventory combination satisfies parameter constraints: " + err.Error()
		case paramspkg.IsNoCompatibleACKUpgradePath(err):
			for _, suite := range active {
				if containsRequirement(suite.RequiresParams, "ack.upgrade_version") {
					skippedNow = append(skippedNow, suite)
				}
			}
			reason = "no ACK upgrade path satisfies dynamic parameters: " + err.Error()
		}
		if len(skippedNow) == 0 {
			return nil, nil, err
		}
		remove := make(map[*scenario.Suite]bool, len(skippedNow))
		for _, suite := range skippedNow {
			remove[suite] = true
			skips[suite] = reason
		}
		next := make([]*scenario.Suite, 0, len(active)-len(skippedNow))
		for _, suite := range active {
			if !remove[suite] {
				next = append(next, suite)
			}
		}
		active = next
	}
}

func resolveParametersWithConstraints(ctx context.Context, opt Options, suites []*scenario.Suite, fix *Fixture, execCfg execpkg.Config, prerequisites map[string]any, constraints paramspkg.ECSConstraints) (map[string]any, error) {
	requirements := make([]string, 0)
	for _, suite := range suites {
		requirements = append(requirements, suite.RequiresParams...)
	}
	if fix != nil {
		requirements = append(requirements, fix.requirements()...)
	}
	requirements = uniqueRequirements(requirements)
	if len(requirements) == 0 {
		return map[string]any{}, nil
	}
	var ecsRequirements []string
	needsACK := false
	needsLingjun := false
	for _, requirement := range requirements {
		switch {
		case strings.HasPrefix(requirement, "ecs."):
			ecsRequirements = append(ecsRequirements, requirement)
		case strings.HasPrefix(requirement, "ack."):
			needsACK = true
			if ecsRequirement := ecsRequirementForACK(requirement); ecsRequirement != "" {
				ecsRequirements = append(ecsRequirements, ecsRequirement)
			}
		case strings.HasPrefix(requirement, "lingjun."):
			needsLingjun = true
		}
	}
	ecsRequirements = uniqueRequirements(ecsRequirements)
	needsECS := len(ecsRequirements) > 0
	if opt.ParameterPolicy == "" {
		return nil, fmt.Errorf("parameter policy is required by selected cases")
	}
	policy, err := paramspkg.LoadPolicy(opt.ParameterPolicy)
	if err != nil {
		return nil, fmt.Errorf("load parameter policy: %w", err)
	}
	for _, requirement := range requirements {
		if !policy.Supports(requirement) {
			return nil, fmt.Errorf("unsupported dynamic parameter requirement %q", requirement)
		}
	}
	if err := policy.ValidateFor(requirements); err != nil {
		return nil, fmt.Errorf("validate parameter policy: %w", err)
	}
	mode := opt.ParameterMode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "static" {
		return nil, fmt.Errorf("parameter mode must be \"auto\" or \"static\"")
	}
	if mode == "static" || opt.DryRun {
		return staticParameters(policy, opt.Region, opt.Zone, needsECS, needsACK, needsLingjun, requirements, ecsRequirements), nil
	}
	query := func(ctx context.Context, command string) (any, error) {
		result := execpkg.Run(ctx, execCfg, command)
		if result.Err != nil {
			return nil, paramspkg.MarkFatalQueryError(result.Err)
		}
		if result.Exit != 0 {
			queryText := strings.TrimSpace(strings.Join([]string{
				firstJSONString(result.JSON, "code"), firstJSONString(result.JSON, "message"), result.Stderr,
			}, " "))
			err := fmt.Errorf("%s exited %d: %s", result.Command, result.Exit, queryText)
			if fatalParameterQuery(result.JSON) || fatalParameterText(queryText) {
				return nil, paramspkg.MarkFatalQueryError(err)
			}
			if !paramspkg.IsCandidateUnavailableText(queryText) {
				return nil, paramspkg.MarkFatalQueryError(err)
			}
			return nil, err
		}
		if result.JSON == nil {
			return nil, fmt.Errorf("%s returned no JSON", result.Command)
		}
		return result.JSON, nil
	}
	parameters := map[string]any{}
	var ecsResult paramspkg.ECSResult
	if needsECS {
		resolved, err := paramspkg.ResolveECSForWithConstraints(ctx, policy.ECS, query, opt.Region, opt.Zone, ecsRequirements, constraints)
		if err != nil {
			return nil, err
		}
		ecsResult = resolved
		parameters = ecsParametersFor(resolved, ecsRequirements)
	}
	if needsACK {
		ackResult, err := paramspkg.ResolveACK(ctx, query, opt.Region, []string{"ManagedKubernetes"}, requirements, ecsResult)
		if err != nil {
			return nil, err
		}
		parameters["ack"] = ackParametersFor(ackResult, requirements)
	}
	if needsLingjun {
		if opt.PreResolvedLingjun != nil {
			parameters["lingjun"] = lingjunParameters(*opt.PreResolvedLingjun)
		} else {
			nodeGroupIDs, ok := nestedStringSlice(prerequisites, "lingjun", "cluster", "node_group_ids")
			if !ok {
				return nil, fmt.Errorf("prerequisite lingjun.cluster.node_group_ids is required by selected Lingjun cases")
			}
			lingjunResult, err := paramspkg.ResolveLingjun(ctx, query, opt.Region, "Lite", nodeGroupIDs)
			if err != nil {
				return nil, err
			}
			parameters["lingjun"] = lingjunParameters(lingjunResult)
		}
	}
	return parameters, nil
}

func combinedECSConstraints(suites []*scenario.Suite) (paramspkg.ECSConstraints, []*scenario.Suite, bool) {
	constraints := paramspkg.ECSConstraints{}
	var constrained []*scenario.Suite
	var allowedSystem []string
	var allowedData []string
	for _, suite := range suites {
		if !hasECSParameterConstraints(suite) {
			continue
		}
		constrained = append(constrained, suite)
		current := suite.ParameterConstraints.ECS
		if current.MinENIQuantity > constraints.MinENIQuantity {
			constraints.MinENIQuantity = current.MinENIQuantity
		}
		if current.MinENIPrivateIPAddressQuantity > constraints.MinENIPrivateIPAddressQuantity {
			constraints.MinENIPrivateIPAddressQuantity = current.MinENIPrivateIPAddressQuantity
		}
		if len(current.AllowedSystemDiskCategories) > 0 {
			if allowedSystem == nil {
				allowedSystem = append([]string(nil), current.AllowedSystemDiskCategories...)
			} else {
				allowedSystem = intersectStrings(allowedSystem, current.AllowedSystemDiskCategories)
				if len(allowedSystem) == 0 {
					return constraints, constrained, true
				}
			}
		}
		if len(current.AllowedDataDiskCategories) > 0 {
			if allowedData == nil {
				allowedData = append([]string(nil), current.AllowedDataDiskCategories...)
			} else {
				allowedData = intersectStrings(allowedData, current.AllowedDataDiskCategories)
				if len(allowedData) == 0 {
					return constraints, constrained, true
				}
			}
		}
	}
	constraints.AllowedSystemDiskCategories = allowedSystem
	constraints.AllowedDataDiskCategories = allowedData
	return constraints, constrained, false
}

func hasECSParameterConstraints(suite *scenario.Suite) bool {
	constraints := suite.ParameterConstraints.ECS
	return constraints.MinENIQuantity > 0 || constraints.MinENIPrivateIPAddressQuantity > 0 || len(constraints.AllowedSystemDiskCategories) > 0 || len(constraints.AllowedDataDiskCategories) > 0
}

func intersectStrings(left, right []string) []string {
	wanted := make(map[string]bool, len(right))
	for _, value := range right {
		wanted[strings.ToLower(value)] = true
	}
	result := make([]string, 0, len(left))
	for _, value := range left {
		if wanted[strings.ToLower(value)] {
			result = append(result, value)
		}
	}
	return result
}

func containsRequirement(requirements []string, wanted string) bool {
	for _, requirement := range requirements {
		if requirement == wanted {
			return true
		}
	}
	return false
}

func uniqueRequirements(requirements []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		if !seen[requirement] {
			seen[requirement] = true
			result = append(result, requirement)
		}
	}
	return result
}

func ecsRequirementForACK(requirement string) string {
	switch requirement {
	case "ack.zone":
		return "ecs.zone"
	case "ack.instance_type":
		return "ecs.instance_type"
	case "ack.image_id":
		return "ecs.image_id"
	case "ack.system_disk_category":
		return "ecs.system_disk_category"
	case "ack.data_disk_category":
		return "ecs.data_disk_category"
	default:
		return ""
	}
}

func fatalParameterQuery(value any) bool {
	code := strings.ToLower(firstJSONString(value, "code"))
	message := strings.ToLower(firstJSONString(value, "message"))
	return fatalParameterText(code + " " + message)
}

func fatalParameterText(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{
		"accessdenied", "unauthorized", "invalidaccesskey",
		"signaturedoesnotmatch", "missingsecuritytoken", "securitytoken",
		"accountnotfound", "credential", "quota", "limitexceed", "internalerror",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func firstJSONString(value any, key string) string {
	switch v := value.(type) {
	case map[string]any:
		if text, ok := v[key].(string); ok {
			return text
		}
		for _, child := range v {
			if text := firstJSONString(child, key); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range v {
			if text := firstJSONString(child, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func staticParameters(policy paramspkg.Policy, region, zone string, needsECS, needsACK, needsLingjun bool, requirements, ecsRequirements []string) map[string]any {
	parameters := map[string]any{}
	if needsECS {
		parameters = ecsParametersFor(paramspkg.ECSResult{
			Region: region, Zone: zoneOrPlaceholder(zone), InstanceType: "<instance-type>",
			ImageID: policy.ECS.StaticImageID, SystemDiskCategory: "<system-disk-category>",
			DataDiskCategory: "<data-disk-category>",
		}, ecsRequirements)
	}
	if needsACK {
		parameters["ack"] = ackParametersFor(paramspkg.ACKResult{
			Region: region, ClusterType: "ManagedKubernetes", Version: "<ack-version>", UpgradeVersion: "<ack-upgrade-version>",
			Edition: "ack.standard", Profile: "Default", Runtime: "containerd", RuntimeVersion: "<ack-runtime-version>",
			Zone: zoneOrPlaceholder(zone), InstanceType: "<instance-type>", ImageID: policy.ECS.StaticImageID,
			SystemDiskCategory: "<system-disk-category>", DataDiskCategory: "<data-disk-category>",
		}, requirements)
	}
	if needsLingjun {
		parameters["lingjun"] = map[string]any{
			"region": region, "cluster_type": "Lite", "hpn_zone": "<hpn-zone>",
			"zone": zoneOrPlaceholder(zone), "machine_type": "<machine-type>", "image_id": "<image-id>",
		}
	}
	return parameters
}

func zoneOrPlaceholder(zone string) string {
	if zone == "" {
		return "<zone>"
	}
	return zone
}

func ecsParametersFor(result paramspkg.ECSResult, requirements []string) map[string]any {
	values := map[string]any{"region": result.Region}
	if result.Zone != "" {
		values["zone"] = result.Zone
	}
	for _, requirement := range requirements {
		switch requirement {
		case "ecs.instance_type":
			values["instance_type"] = result.InstanceType
		case "ecs.image_id":
			values["image_id"] = result.ImageID
		case "ecs.system_disk_category":
			values["system_disk_category"] = result.SystemDiskCategory
		case "ecs.data_disk_category":
			values["data_disk_category"] = result.DataDiskCategory
		}
	}
	return map[string]any{"ecs": values}
}

func ackParametersFor(result paramspkg.ACKResult, requirements []string) map[string]any {
	values := map[string]any{}
	for _, requirement := range requirements {
		switch requirement {
		case "ack.region":
			values["region"] = result.Region
		case "ack.cluster_type":
			values["cluster_type"] = result.ClusterType
		case "ack.version":
			values["version"] = result.Version
		case "ack.upgrade_version":
			values["upgrade_version"] = result.UpgradeVersion
		case "ack.edition":
			values["edition"] = result.Edition
		case "ack.profile":
			values["profile"] = result.Profile
		case "ack.runtime":
			values["runtime"] = result.Runtime
		case "ack.runtime_version":
			values["runtime_version"] = result.RuntimeVersion
		case "ack.zone":
			values["zone"] = result.Zone
		case "ack.instance_type":
			values["instance_type"] = result.InstanceType
		case "ack.image_id":
			values["image_id"] = result.ImageID
		case "ack.system_disk_category":
			values["system_disk_category"] = result.SystemDiskCategory
		case "ack.data_disk_category":
			values["data_disk_category"] = result.DataDiskCategory
		}
	}
	return values
}

func lingjunParameters(result paramspkg.LingjunResult) map[string]any {
	return map[string]any{
		"region": result.Region, "cluster_type": result.ClusterType, "hpn_zone": result.HPNZone,
		"zone": result.Zone, "machine_type": result.MachineType, "image_id": result.ImageID,
	}
}

func nestedString(values map[string]any, path ...string) (string, bool) {
	var current any = values
	for _, part := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = m[part]
		if !ok {
			return "", false
		}
	}
	s, ok := current.(string)
	return s, ok && s != ""
}

func nestedStringSlice(values map[string]any, path ...string) ([]string, bool) {
	var current any = values
	for _, part := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	switch list := current.(type) {
	case []any:
		result := make([]string, 0, len(list))
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	case []string:
		return append([]string(nil), list...), true
	default:
		return nil, false
	}
}

func ossExportPrefix(runID string) string {
	digest := sha256.Sum256([]byte(runID))
	return fmt.Sprintf("E2E%x", digest)[:27]
}

func resourcePrefix(runName string) string {
	const maxRunes = 40
	runes := []rune(strings.TrimSpace(runName))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	digest := sha256.Sum256([]byte(runName))
	suffix := fmt.Sprintf("-%x", digest[:4])
	prefixRunes := runes[:maxRunes-len([]rune(suffix))]
	prefix := strings.TrimRight(string(prefixRunes), "-")
	return prefix + suffix
}

// provisionStack creates the selected shared-stack resources and records a
// failure per node. Independent branches continue after a failure; nodes whose
// dependencies failed are skipped.
func provisionStack(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, base, stackVars map[string]any, fix *Fixture, sc *report.Case) map[string]string {
	failures := map[string]string{}
	for _, ps := range fix.Provision {
		if failed := directFailedDependency(ps, failures); failed != "" {
			message := fmt.Sprintf("dependency %q failed", failed)
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusSkipped, Error: message})
			continue
		}
		// Stack authors may reference earlier captures by bare name ({{.vpc}})
		// or via the namespace ({{.stack.vpc}}); expose both.
		data := vars.Clone(base)
		for k, v := range stackVars {
			data[k] = v
		}
		data["stack"] = stackVars

		cmd, err := vars.Render(ps.Run, data)
		if err != nil {
			message := "render: " + err.Error()
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: message})
			continue
		}
		if opt.DryRun {
			opt.Logf("stack %q (dry): %s", ps.ID, cmd)
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Command: cmd, Status: report.StatusSkipped})
			for v := range ps.Capture {
				stackVars[v] = "<" + v + ">"
				data[v] = "<" + v + ">"
			}
			if ps.Teardown != "" {
				if _, err := vars.Render(ps.Teardown, data); err != nil {
					message := "render teardown: " + err.Error()
					failures[ps.ID] = message
					sc.Steps[len(sc.Steps)-1].Status = report.StatusError
					sc.Steps[len(sc.Steps)-1].Error = message
				}
			}
			continue
		}

		opt.Logf("stack %q: %s", ps.ID, cmd)
		sctx, cancel := context.WithTimeout(ctx, opt.StepTimeout)
		res := execpkg.Run(sctx, execCfg, cmd)
		cancel()

		step := report.Step{
			Name: ps.ID, Command: res.Command, Exit: res.Exit,
			DurationMs: res.Duration.Milliseconds(),
			Stdout:     strings.TrimSpace(res.Stdout), Stderr: strings.TrimSpace(res.Stderr),
			Status: report.StatusPass,
		}
		// Register teardown as soon as the resource may exist.
		if ps.Teardown != "" {
			td, terr := vars.Render(ps.Teardown, captureInto(data, res, ps.At, ps.Capture))
			if terr != nil {
				if res.Err == nil && res.Exit == 0 {
					step.Status, step.Error = report.StatusError, "render teardown: "+terr.Error()
					sc.Steps = append(sc.Steps, step)
					failures[ps.ID] = step.Error
					continue
				}
			} else if err := cl.push(scope, "stack", td, "primary"); err != nil {
				step.Status, step.Error = report.StatusError, "cleanup journal: "+err.Error()
				sc.Steps = append(sc.Steps, step)
				failures[ps.ID] = step.Error
				continue
			}
		}
		if res.Err != nil {
			step.Status, step.Error = report.StatusError, res.Err.Error()
			sc.Steps = append(sc.Steps, step)
			failures[ps.ID] = step.Error
			continue
		}
		if res.Exit != 0 {
			step.Status, step.Error = report.StatusFail, failureDetail(res)
			sc.Steps = append(sc.Steps, step)
			failures[ps.ID] = fmt.Sprintf("exit %d", res.Exit)
			continue
		}
		captureFailed := false
		for v, path := range ps.Capture {
			val, ok := captureValue(res.JSON, ps.At, path)
			if !ok {
				step.Status, step.Error = report.StatusFail, fmt.Sprintf("capture %q: path %q not found", v, path)
				failures[ps.ID] = step.Error
				captureFailed = true
				break
			}
			stackVars[v] = val
			data[v] = val
		}
		sc.Steps = append(sc.Steps, step)
		if captureFailed {
			continue
		}
	}
	return failures
}

func directFailedDependency(step ProvisionStep, failures map[string]string) string {
	for _, dependency := range step.Needs {
		if _, failed := failures[dependency]; failed {
			return dependency
		}
	}
	return ""
}

func failedStackDependencies(fix *Fixture, requested []string, failures map[string]string) []string {
	if fix == nil || len(requested) == 0 {
		return nil
	}
	planned, err := fix.plan(requested)
	if err != nil {
		return requested
	}
	failed := make([]string, 0)
	for _, step := range planned {
		if _, exists := failures[step.ID]; exists {
			failed = append(failed, step.ID)
		}
	}
	return failed
}

func formatStackFailures(steps []ProvisionStep, failures map[string]string) string {
	parts := make([]string, 0, len(failures))
	for _, step := range steps {
		if message, failed := failures[step.ID]; failed {
			parts = append(parts, fmt.Sprintf("%s: %s", step.ID, message))
		}
	}
	return strings.Join(parts, "; ")
}

// captureInto returns data plus this provision step's captures, for rendering a
// teardown that references the just-created id.
func captureInto(data map[string]any, res execpkg.Result, at string, capture map[string]string) map[string]any {
	if len(capture) == 0 {
		return data
	}
	m := vars.Clone(data)
	for v, path := range capture {
		if val, ok := captureValue(res.JSON, at, path); ok {
			m[v] = val
		}
	}
	return m
}

func runCase(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, base map[string]any, s *scenario.Suite) report.Case {
	start := time.Now()
	cr := report.Case{Name: caseName(s), Resource: s.Resource, Path: s.Path, Status: report.StatusPass}

	inputs, err := loadInputs(opt.InputsDir, s.Resource)
	if err != nil {
		cr.Status, cr.Error = report.StatusError, err.Error()
		cr.DurationMs = time.Since(start).Milliseconds()
		return cr
	}

	data := vars.Clone(base)
	data["run_name"] = opt.RunName + "-" + caseSlug(s.Resource)
	data["inputs"] = inputs

	stepTimeout := opt.StepTimeout
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err == nil {
			stepTimeout = d
		}
	}

	var scope []*cleanupItem
	defer func() { cl.run(scope) }()

	for _, st := range s.Steps {
		if opt.DryRun {
			sr := renderDryStep(data, st)
			cr.Steps = append(cr.Steps, sr)
			if sr.Status == report.StatusError {
				cr.Status, cr.Error = report.StatusError, sr.Error
				break
			}
			continue
		}
		if ctx.Err() != nil {
			cr.Steps = append(cr.Steps, report.Step{Name: st.Name, Status: report.StatusSkipped, Error: "run cancelled"})
			cr.Status = report.StatusFail
			break
		}
		sr, ok := runStep(ctx, opt, execCfg, cl, &scope, data, st, stepTimeout)
		cr.Steps = append(cr.Steps, sr)
		if !ok {
			cr.Status = report.StatusFail
			break // abort this case; teardown still runs via defer
		}
	}
	if opt.DryRun {
		cr.Status = report.StatusSkipped
	}
	if failures := cl.run(scope); len(failures) > 0 {
		cr.Status = report.StatusFail
		if cr.Error != "" {
			cr.Error += "; "
		}
		cr.Error += strings.Join(failures, "; ")
	}
	cr.DurationMs = time.Since(start).Milliseconds()
	return cr
}

func runStep(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, timeout time.Duration) (report.Step, bool) {
	wantExit := 0
	if st.Exit != nil {
		wantExit = *st.Exit
	}
	sr := report.Step{Name: st.Name, WantExit: wantExit, Status: report.StatusPass}

	cmd, err := vars.Render(st.Run, data)
	if err != nil {
		sr.Status, sr.Error = report.StatusError, "render: "+err.Error()
		return sr, false
	}
	sr.Command = cmd

	sctx, cancel := context.WithTimeout(ctx, timeout)
	res := execpkg.Run(sctx, execCfg, cmd)
	cancel()
	sr.Command = res.Command
	sr.Exit = res.Exit
	sr.DurationMs = res.Duration.Milliseconds()
	sr.Stdout = strings.TrimSpace(res.Stdout)
	sr.Stderr = strings.TrimSpace(res.Stderr)

	// Register teardown as soon as the create-ish step ran (even if asserts
	// later fail) so the resource it produced is cleaned up.
	if st.Teardown != "" {
		td, terr := vars.Render(st.Teardown, mergeCaptures(data, res, st))
		if terr != nil {
			// Failed creates commonly have no resource ID. Preserve the command
			// error; teardown rendering is authoritative only after the command
			// reached its expected exit status.
			if res.Err == nil && res.Exit == wantExit {
				sr.Status, sr.Error = report.StatusError, "render teardown: "+terr.Error()
				return sr, false
			}
		} else if err := cl.push(scope, caseScope(data), td, st.TeardownRegion); err != nil {
			sr.Status, sr.Error = report.StatusError, "cleanup journal: "+err.Error()
			return sr, false
		}
	}

	if res.Err != nil {
		sr.Status, sr.Error = report.StatusError, res.Err.Error()
		return sr, false
	}
	if res.Exit == 0 {
		cl.satisfy(*scope, cmd, "primary")
	}
	if res.Exit != wantExit {
		if d := failureDetail(res); d != "" {
			sr.Error = d
		}
		sr.Status = report.StatusFail
		return sr, false
	}
	ok := true
	if len(st.Expect) > 0 || len(st.Assert) > 0 {
		exps, asserts, rerr := renderExpectations(st.Expect, st.Assert, data)
		if rerr != nil {
			sr.Status, sr.Error = report.StatusError, "render expect: "+rerr.Error()
			return sr, false
		}
		checks, allOK := match.Step(res.JSON, st.At, exps, asserts)
		for _, c := range checks {
			sr.Checks = append(sr.Checks, report.Check{Path: c.Path, OK: c.OK, Detail: c.Detail})
		}
		if !allOK {
			ok = false
		}
	}
	// Apply captures for subsequent steps.
	for v, path := range st.Capture {
		val, found := captureValue(res.JSON, st.At, path)
		if !found {
			sr.Status = report.StatusFail
			sr.Error = fmt.Sprintf("capture %q: path %q not found", v, path)
			return sr, false
		}
		data[v] = val
	}
	if !ok {
		sr.Status = report.StatusFail
	}
	return sr, ok
}

// mergeCaptures returns data augmented with this step's captures so a teardown
// referencing e.g. {{.instance_id}} renders even before the main capture loop.
func mergeCaptures(data map[string]any, res execpkg.Result, st scenario.Step) map[string]any {
	if len(st.Capture) == 0 {
		return data
	}
	m := vars.Clone(data)
	for v, path := range st.Capture {
		if val, found := captureValue(res.JSON, st.At, path); found {
			m[v] = val
		}
	}
	return m
}

func renderDryStep(data map[string]any, st scenario.Step) report.Step {
	sr := report.Step{Name: st.Name, Status: report.StatusSkipped}
	cmd, err := vars.Render(st.Run, data)
	if err != nil {
		sr.Status, sr.Error = report.StatusError, "render: "+err.Error()
		return sr
	}
	sr.Command = cmd
	for v := range st.Capture {
		data[v] = "<" + v + ">"
	}
	return sr
}

// captureValue resolves a capture path (relative to At) from a JSON document.
func captureValue(doc any, at, path string) (any, bool) {
	return jsonGet(doc, at, path)
}
