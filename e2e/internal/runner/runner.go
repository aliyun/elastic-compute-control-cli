// Package runner orchestrates an E2E run: provision the shared stack, run cases
// in bounded parallel with per-case isolation, tear everything down (two-level,
// signal-safe), and produce a result model.
package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/match"
	paramspkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/params"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

// stepRetryDelays paces retries for transient network errors during step
// execution. Like cleanupRetryDelays, each delay is a best-effort back-off
// bounded by the step context.
var stepRetryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}

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
	Session            *Session

	quotaLifecycleForbidden bool // set from the current suite before rendering steps
	quotaLifecycle          bool // owned Team and Quota commands share one credential binding
	quotaLifecycleKnown     bool // runCase classified all steps before allocation
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

// Run executes the suites and returns the result model. It records per-case
// failures and continues unless a pending Quota update has fenced cleanup;
// later cases are then skipped before allocation. An error is returned only
// for fatal setup problems (cannot load cases / fixtures).
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
	requestedJournal := opt.CleanupJournal
	journalPaths := map[string]string{}
	if requestedJournal != "" {
		for role := range regions {
			path, err := journalfile.ResolveJournalPath(cleanupJournalPath(requestedJournal, role))
			if err != nil {
				return setupErr(fmt.Errorf("bind cleanup journal for %s: %w", role, err))
			}
			journalPaths[role] = path
		}
		opt.CleanupJournal = journalPaths["primary"]
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
	if opt.Session != nil {
		if err := opt.Session.checkRetainedQuotaOwnership(); err != nil {
			return setupErr(err)
		}
	}
	for _, suite := range suites {
		for _, step := range suite.Steps {
			if step.Local == nil && journalcmd.RawQuotaMutationCandidate(step.Run) {
				return setupErr(fmt.Errorf("case %s step %s: raw FCSandbox Quota mutation is unsupported in E2E cases", suite.Resource, step.Name))
			}
			if !opt.DryRun && quotaUpdateCandidate(step, step.Run) && (!suite.Serial || suite.Execution == scenario.ExecutionDAG) {
				return setupErr(fmt.Errorf("case %s: Quota update requires a serial sequential lifecycle", suite.Resource))
			}
			if quotaUpdateCandidate(scenario.Step{}, step.Teardown) || quotaDeleteCandidate(step.Teardown) && !quotaUpdateCandidate(step, step.Run) && !unresolvedCommandPath(step.Run) {
				return setupErr(fmt.Errorf("case %s step %s: Quota mutation cannot be an ordinary teardown", suite.Resource, step.Name))
			}
		}
	}
	workDir, err := os.MkdirTemp("", "ecctl-e2e-")
	if err != nil {
		return setupErr(fmt.Errorf("create run work directory: %w", err))
	}
	defer os.RemoveAll(workDir)

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
	operations := newOperationRuntime(opt.Parallel)
	if opt.Session != nil {
		operations, err = opt.Session.operationRuntime(opt.Parallel)
		if err != nil {
			return setupErr(err)
		}
	}
	cl := newCleanup(execCfgs, operations, opt.Keep, opt.CleanupJournal, report.CleanupJournal{RunID: opt.RunID, ExecutionID: opt.ExecutionID, Surface: opt.Surface}, opt.Logf)
	cl.requestedJournal, cl.journalPaths = requestedJournal, journalPaths
	cl.session = opt.Session
	if !opt.DryRun {
		if cl.journal != "" {
			paths := make([]string, 0, len(journalPaths))
			seen := make(map[string]bool, len(journalPaths))
			for _, path := range journalPaths {
				if !seen[path] {
					paths = append(paths, path)
					seen[path] = true
				}
			}
			sort.Strings(paths)
			releases := make([]func() error, 0, len(paths))
			releaseAll := func() {
				for i := len(releases) - 1; i >= 0; i-- {
					if err := releases[i](); err != nil {
						opt.Logf("release cleanup journal run lease: %v", err)
					}
				}
			}
			for _, path := range paths {
				release, err := journalfile.AcquireRunLease(path)
				if err != nil {
					releaseAll()
					return setupErr(fmt.Errorf("admit cleanup journal before allocation: %w", err))
				}
				releases = append(releases, release)
			}
			defer releaseAll()
		}
		if err := cl.checkDurablePendingQuotaUpdate(ctx); err != nil {
			return setupErr(fmt.Errorf("admit cleanup journal before allocation: %w", err))
		}
	}
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
		"oss_bucket_name":   ossBucketName(opt.RunID, opt.ExecutionID, run.StartedAt),
		"work_dir":          workDir,
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
		executionFixture, runFixture := splitFixtureLifetimes(fix)
		if opt.Session != nil && len(runFixture.Provision) > 0 {
			sessionFailures, acquireErr := opt.Session.acquire(ctx, opt, execCfg, cl, base, stackVars, runFixture, &sc)
			if acquireErr != nil {
				return setupErr(acquireErr)
			}
			for id, failure := range sessionFailures {
				stackFailures[id] = failure
			}
		} else if len(runFixture.Provision) > 0 {
			localFailures := provisionStack(ctx, opt, execCfg, cl, &stackScope, base, stackVars, runFixture, &sc, nil)
			for id, failure := range localFailures {
				stackFailures[id] = failure
			}
		}
		if len(executionFixture.Provision) > 0 {
			localFailures := provisionStack(ctx, opt, execCfg, cl, &stackScope, base, stackVars, executionFixture, &sc, stackFailures)
			for id, failure := range localFailures {
				stackFailures[id] = failure
			}
		}
		sc.DurationMs = time.Since(start).Milliseconds()
		if len(stackFailures) > 0 {
			if hasStackFailures(stackFailures) {
				sc.Status = report.StatusFail
				sc.Error = formatStackFailures(fix.Provision, stackFailures)
				opt.Logf("shared stack has failed branches (dependent cases skipped): %s", sc.Error)
			}
		}
		stackCase = &sc
	}
	base["stack"] = stackVars

	caseResults := make([]report.Case, len(suites))
	var wg sync.WaitGroup

	// runAndLog runs one case, bracketing it with start/finish lines so progress
	// is visible in real time (cases otherwise run silently until the summary).
	runAndLog := func(i int, s *scenario.Suite) report.Case {
		opt.Logf("RUN   %s", s.Resource)
		res := runCase(ctx, opt, execCfg, cl, operations, base, fix, stackFailures, s)
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
		go func(i int, s *scenario.Suite) {
			defer wg.Done()
			caseResults[i] = runAndLog(i, s)
		}(i, s)
	}
	wg.Wait()
	if failures := cl.runDeferredCleanup(); len(failures) > 0 {
		caseResults = append(caseResults, report.Case{Name: "(deferred case cleanup)", Resource: "cleanup", Status: report.StatusFail, Error: strings.Join(failures, "; ")})
	}
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
	if opt.Session != nil && cl.hasRetainedQuotaOwnership() {
		opt.Session.retainQuotaOwnership()
	}
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
		case paramspkg.IsNoCompatibleACKAddonLifecycle(err):
			for _, suite := range active {
				if containsRequirement(suite.RequiresParams, "ack.addon_name") ||
					containsRequirement(suite.RequiresParams, "ack.addon_version") ||
					containsRequirement(suite.RequiresParams, "ack.addon_upgrade_version") {
					skippedNow = append(skippedNow, suite)
				}
			}
			reason = "no ACK addon lifecycle satisfies dynamic parameters: " + err.Error()
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
		if opt.PreResolvedLingjun != nil && containsRequirement(requirements, "lingjun.hpn_zone") {
			parameters["lingjun"] = lingjunParameters(*opt.PreResolvedLingjun)
		} else if containsRequirement(requirements, "lingjun.hpn_zone") {
			nodeGroupIDs, ok := nestedStringSlice(prerequisites, "lingjun", "cluster", "node_group_ids")
			if !ok {
				// The base cluster lifecycle does not consume node groups; its
				// descriptor only needs a public node-group profile. Scaling
				// steps stay gated on the lingjun.cluster prerequisite.
				lingjunResult, err := paramspkg.ResolveLingjunNodeGroupProfile(ctx, query, opt.Region)
				if err != nil {
					return nil, err
				}
				parameters["lingjun"] = lingjunParameters(lingjunResult)
			} else {
				lingjunResult, err := paramspkg.ResolveLingjun(ctx, query, opt.Region, "Lite", nodeGroupIDs)
				if err != nil {
					return nil, err
				}
				parameters["lingjun"] = lingjunParameters(lingjunResult)
			}
		} else {
			lingjunResult, err := paramspkg.ResolveLingjunNodeGroupProfile(ctx, query, opt.Region)
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
			AddonName: "<ack-addon-name>", AddonVersion: "<ack-addon-version>", AddonUpgradeVersion: "<ack-addon-upgrade-version>",
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
		case "ack.addon_name":
			values["addon_name"] = result.AddonName
		case "ack.addon_version":
			values["addon_version"] = result.AddonVersion
		case "ack.addon_upgrade_version":
			values["addon_upgrade_version"] = result.AddonUpgradeVersion
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

func ossBucketName(runID, executionID string, startedAt time.Time) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", runID, executionID, startedAt.UnixNano())))
	return fmt.Sprintf("ecctl-e2e-%s-%x", startedAt.UTC().Format("20060102t150405"), digest[:6])
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

func validateStackTemplateCommand(command string) error {
	if quotaUpdateCandidate(scenario.Step{}, command) || quotaDeleteCandidate(command) {
		return fmt.Errorf("Quota shared-stack mutations are unsupported; use an owned case lifecycle")
	}
	native, err := nativeTemplateCommandStatus(command)
	if err != nil {
		return fmt.Errorf("cannot classify shared-stack Template command")
	}
	if !native {
		return nil
	}
	path, err := commandAction(command)
	if err == nil && len(path) == 3 {
		switch path[2] {
		case "get", "show", "describe", "list", "ls":
			return nil
		}
	}
	// Shared fixtures do not participate in the case-owned native intent and
	// cleanup protocol. Reject before allocation, including session leases.
	return fmt.Errorf("native Template shared-stack mutations are unsupported; use an owned case lifecycle")
}

// A shared Team's finalizer must be registerable before the create can reach
// the provider. Substitute safe IDs for this step's captures to check the
// exact ordinary delete grammar without depending on a provider response.
func validateStackTeamFinalizer(step ProvisionStep, command string, data map[string]any, region string) error {
	if !isTeamCreate(command) {
		return nil
	}
	args, err := shlex.Split(command)
	if err != nil {
		return fmt.Errorf("shared Team create command is invalid")
	}
	flags := teamCreateFlagSet()
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("shared Team create command is invalid")
	}
	if flags.Changed("profile") {
		return fmt.Errorf("shared Team create cannot select an unbound profile")
	}
	if flags.Changed("region") {
		selected, _ := flags.GetString("region")
		if selected != region {
			return fmt.Errorf("shared Team create region differs from cleanup region")
		}
	}
	if step.Teardown == "" {
		return fmt.Errorf("shared Team creation requires a cleanup finalizer")
	}
	values := vars.Clone(data)
	ownedTargets := map[string]bool{}
	index := 0
	for name, field := range step.Capture {
		index++
		placeholder := fmt.Sprintf("ecctl-pending-team-%d", index)
		values[name] = placeholder
		if field == "id" {
			ownedTargets[placeholder] = true
		}
	}
	finalizer, err := vars.Render(step.Teardown, values)
	if err != nil {
		return fmt.Errorf("shared Team finalizer: %w", err)
	}
	if rawTeamDeleteCandidate(finalizer) {
		return fmt.Errorf("shared Team finalizer cannot use an unattended raw delete")
	}
	target, err := teamDeleteTargetForOwnership(finalizer, region)
	if err != nil || !ownedTargets[target] {
		return fmt.Errorf("shared Team finalizer requires a captured ID and validated ordinary delete")
	}
	return nil
}

// provisionStack creates the selected shared-stack resources and records a
// failure per node. Independent branches continue after a failure; nodes whose
// dependencies failed are skipped.
func provisionStack(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, base, stackVars map[string]any, fix *Fixture, sc *report.Case, inheritedFailures map[string]string) map[string]string {
	failures := make(map[string]string, len(inheritedFailures))
	for id, failure := range inheritedFailures {
		failures[id] = failure
	}
	for _, ps := range fix.Provision {
		if quotaUpdateCandidate(scenario.Step{}, ps.Teardown) || quotaDeleteCandidate(ps.Teardown) {
			message := "Quota shared-stack teardown is unsupported; use an owned case lifecycle"
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: message})
			continue
		}
		if failed := directFailedDependency(ps, failures); failed != "" {
			message := fmt.Sprintf("dependency %q failed", failed)
			if stackStepSkipped(failures[failed]) {
				message = fmt.Sprintf("dependency %q unavailable", failed)
				failures[ps.ID] = stackSkippedPrefix + message
			} else {
				failures[ps.ID] = message
			}
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
		if missing := missingPrerequisites(data, ps.RequiresPrerequisites); len(missing) > 0 {
			message := "prerequisites unavailable: " + strings.Join(missing, ", ")
			failures[ps.ID] = stackSkippedPrefix + message
			sc.Steps = append(sc.Steps, report.Step{
				Name: ps.ID, Status: report.StatusSkipped, Error: message,
				RequiresPrerequisites: append([]string(nil), ps.RequiresPrerequisites...),
			})
			continue
		}

		cmd, err := vars.Render(ps.Run, data)
		if err != nil {
			message := "render: " + err.Error()
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: message})
			continue
		}
		if journalcmd.RawQuotaMutationCandidate(cmd) {
			message := "raw FCSandbox Quota mutation is unsupported in E2E stack steps"
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: message})
			continue
		}
		if err := validateStackTemplateCommand(cmd); err != nil {
			failures[ps.ID] = err.Error()
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: err.Error()})
			continue
		}
		if err := validateStackTeamFinalizer(ps, cmd, data, execCfg.Region); err != nil {
			failures[ps.ID] = err.Error()
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: err.Error()})
			continue
		}
		display, err := renderReportCommand(ps.Run, data)
		if err != nil {
			message := "render display: " + err.Error()
			failures[ps.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Status: report.StatusError, Error: message})
			continue
		}
		if opt.DryRun {
			opt.Logf("stack %q (dry): %s", ps.ID, report.Scrub(display))
			sc.Steps = append(sc.Steps, report.Step{Name: ps.ID, Command: display, Status: report.StatusSkipped})
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

		opt.Logf("stack %q: %s", ps.ID, report.Scrub(display))
		sctx, cancel := context.WithTimeout(ctx, opt.StepTimeout)
		res := execpkg.Run(sctx, execCfg, cmd)
		cancel()

		step := report.Step{
			Name: ps.ID, Command: res.Command, Exit: res.Exit,
			DurationMs: res.Duration.Milliseconds(),
			Stdout:     strings.TrimSpace(res.Stdout), Stderr: strings.TrimSpace(res.Stderr),
			Status: report.StatusPass,
		}
		if display != cmd {
			step.Command = report.Scrub(display)
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
			} else if err := cl.push(scope, "stack", td, "primary", nil); err != nil {
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

const stackSkippedPrefix = "skipped: "

func stackStepSkipped(message string) bool {
	return strings.HasPrefix(message, stackSkippedPrefix)
}

func hasStackFailures(failures map[string]string) bool {
	for _, message := range failures {
		if !stackStepSkipped(message) {
			return true
		}
	}
	return false
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
		if message, failed := failures[step.ID]; failed && !stackStepSkipped(message) {
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

func runCase(
	ctx context.Context,
	opt Options,
	execCfg execpkg.Config,
	cl *cleanup,
	operations *operationRuntime,
	base map[string]any,
	fix *Fixture,
	stackFailures map[string]string,
	s *scenario.Suite,
) report.Case {
	start := time.Now()
	cr := report.Case{Name: caseName(s), Resource: s.Resource, Path: s.Path, Status: report.StatusPass}
	if cl.hasUnresolvedQuotaTeamCreate() {
		cr.Status, cr.Error = report.StatusSkipped, "unresolved Quota-bound Team create blocks later cases"
		return cr
	}
	if cl.hasPendingQuotaUpdate() {
		cr.Status, cr.Error = report.StatusSkipped, "pending Quota update blocks later cases"
		return cr
	}
	opt.quotaLifecycleForbidden = !s.Serial || s.Execution == scenario.ExecutionDAG
	opt.quotaLifecycleKnown = true
	// Admission follows the operation rather than the suite's resource label:
	// mixed serial suites can own a Team and then update its Quota.
	opt.quotaLifecycle = false
	for _, step := range s.Steps {
		if step.Local == nil && journalcmd.RawQuotaMutationCandidate(step.Run) {
			cr.Status, cr.Error = report.StatusError, "raw FCSandbox Quota mutation is unsupported in E2E cases"
			return cr
		}
	}
	for _, step := range s.Steps {
		if quotaUpdateCandidate(step, step.Run) {
			opt.quotaLifecycle = true
			break
		}
	}

	inputs, err := loadInputs(opt.InputsDir, s.Resource)
	if err != nil {
		cr.Status, cr.Error = report.StatusError, err.Error()
		cr.DurationMs = time.Since(start).Milliseconds()
		return cr
	}

	data := vars.Clone(base)
	data["run_name"] = opt.RunName + "-" + caseSlug(s.Resource)
	data["inputs"] = inputs
	if s.Resource == "sandbox/team" || s.Resource == "sandbox/template-cache" || s.Resource == "sandbox/quota" {
		if err := teamOwnership(data); err != nil {
			cr.Status, cr.Error = report.StatusError, err.Error()
			return cr
		}
	}

	stepTimeout := opt.StepTimeout
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err == nil {
			stepTimeout = d
		}
	}

	var scope []*cleanupItem
	defer func() { cl.runCaseCleanup(scope) }()

	if s.Execution == scenario.ExecutionDAG {
		runDAGCase(ctx, opt, execCfg, cl, operations, &scope, data, fix, stackFailures, s, stepTimeout, &cr)
	} else {
		runSequentialCase(ctx, opt, execCfg, cl, operations, &scope, data, s, stepTimeout, &cr)
	}
	if opt.DryRun {
		cr.Status = report.StatusSkipped
	}
	if failures := cl.runCaseCleanup(scope); len(failures) > 0 {
		cr.Status = report.StatusFail
		if cr.Error != "" {
			cr.Error += "; "
		}
		cr.Error += strings.Join(failures, "; ")
	}
	cr.DurationMs = time.Since(start).Milliseconds()
	return cr
}

func runSequentialCase(
	ctx context.Context,
	opt Options,
	execCfg execpkg.Config,
	cl *cleanup,
	operations *operationRuntime,
	scope *[]*cleanupItem,
	data map[string]any,
	s *scenario.Suite,
	stepTimeout time.Duration,
	cr *report.Case,
) {
	teamCreates := 0
	for _, st := range s.Steps {
		if opt.DryRun {
			sr := renderDryStep(data, st)
			annotateStep(&sr, st)
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
		// A Quota pagination case needs two independently owned Teams. Rotate
		// the nonce before the second protected create, never its captured ID.
		if opt.quotaLifecycle && s.Resource == "sandbox/quota" && isTeamCreate(st.Run) {
			if teamCreates > 0 {
				if err := teamOwnership(data); err != nil {
					cr.Steps = append(cr.Steps, report.Step{Name: st.Name, Status: report.StatusError, Error: err.Error()})
					cr.Status = report.StatusFail
					break
				}
			}
			teamCreates++
		}
		var sr report.Step
		var ok bool
		if err := operations.execute(ctx, data, st.Locks, func() {
			sr, ok = runStep(ctx, opt, execCfg, cl, scope, data, st, stepTimeout)
		}); err != nil {
			sr = report.Step{Name: st.Name, Status: report.StatusError, Error: err.Error()}
		}
		annotateStep(&sr, st)
		cr.Steps = append(cr.Steps, sr)
		if !ok {
			cr.Status = report.StatusFail
			break // abort this case; teardown still runs via defer
		}
	}
}

// Only a template in the ecctl command path can make raw setup classification
// inconclusive. Templates in IDs or flags do not hide an ordinary action.
func unresolvedCommandPath(command string) bool {
	path, err := commandAction(command)
	if err != nil {
		return strings.Contains(command, "{{")
	}
	for _, part := range path {
		if strings.Contains(part, "{{") {
			return true
		}
	}
	return false
}

func ordinaryQuotaTeardown(step scenario.Step, renderedRun string) bool {
	return quotaUpdateCandidate(scenario.Step{}, step.Teardown) ||
		quotaDeleteCandidate(step.Teardown) && !quotaUpdateCandidate(step, renderedRun)
}

type dagStepOutcome struct {
	step     report.Step
	success  bool
	captures map[string]any
}

var dagStepAfterCompletion = func(string) {}

func runDAGCase(
	ctx context.Context,
	opt Options,
	execCfg execpkg.Config,
	cl *cleanup,
	operations *operationRuntime,
	scope *[]*cleanupItem,
	base map[string]any,
	fix *Fixture,
	stackFailures map[string]string,
	s *scenario.Suite,
	stepTimeout time.Duration,
	cr *report.Case,
) {
	indexByName := make(map[string]int, len(s.Steps))
	for i, step := range s.Steps {
		indexByName[step.Name] = i
	}
	success := make([]bool, len(s.Steps))
	outcomes := make([]dagStepOutcome, len(s.Steps))
	capturesByStep := make(map[string]map[string]any, len(s.Steps))
	remainingDependencies := make([]int, len(s.Steps))
	dependents := make([][]int, len(s.Steps))
	for index, step := range s.Steps {
		remainingDependencies[index] = len(step.DependsOn)
		for _, dependency := range step.DependsOn {
			parent := indexByName[dependency]
			dependents[parent] = append(dependents[parent], index)
		}
	}
	type completion struct {
		index   int
		outcome dagStepOutcome
	}
	completions := make(chan completion, len(s.Steps))
	schedule := func(index int) {
		step := s.Steps[index]
		blockedBy := ""
		for _, dependency := range step.DependsOn {
			if !success[indexByName[dependency]] {
				blockedBy = dependency
				break
			}
		}
		if blockedBy != "" {
			outcome := dagStepOutcome{step: report.Step{
				Name: step.Name, Status: report.StatusSkipped,
				Error: fmt.Sprintf("dependency %q did not pass", blockedBy),
			}}
			annotateStep(&outcome.step, step)
			completions <- completion{index: index, outcome: outcome}
			return
		}

		data := dagStepData(base, step, indexByName, s.Steps, capturesByStep)
		if failed := failedStackDependencies(fix, step.Needs, stackFailures); len(failed) > 0 {
			outcome := dagStepOutcome{step: report.Step{
				Name: step.Name, Status: report.StatusSkipped,
				Error: "shared stack dependencies failed: " + strings.Join(failed, ", "),
			}}
			annotateStep(&outcome.step, step)
			completions <- completion{index: index, outcome: outcome}
			return
		}
		if missing := missingPrerequisites(data, step.RequiresPrerequisites); len(missing) > 0 {
			outcome := dagStepOutcome{step: report.Step{
				Name: step.Name, Status: report.StatusSkipped,
				Error: "prerequisites unavailable: " + strings.Join(missing, ", "),
			}}
			annotateStep(&outcome.step, step)
			completions <- completion{index: index, outcome: outcome}
			return
		}

		go func() {
			if opt.DryRun {
				sr := renderDryStep(data, step)
				annotateStep(&sr, step)
				completions <- completion{index: index, outcome: dagStepOutcome{
					step: sr, success: sr.Status != report.StatusError, captures: capturedData(data, step),
				}}
				return
			}
			var sr report.Step
			var ok bool
			err := operations.execute(ctx, data, step.Locks, func() {
				sr, ok = runStep(ctx, opt, execCfg, cl, scope, data, step, stepTimeout)
			})
			if err != nil {
				sr = report.Step{Name: step.Name, Status: report.StatusError, Error: err.Error()}
				ok = false
			}
			annotateStep(&sr, step)
			completions <- completion{
				index:   index,
				outcome: dagStepOutcome{step: sr, success: ok, captures: capturedData(data, step)},
			}
		}()
	}

	for index, count := range remainingDependencies {
		if count == 0 {
			schedule(index)
		}
	}
	for completed := 0; completed < len(s.Steps); completed++ {
		result := <-completions
		outcomes[result.index] = result.outcome
		success[result.index] = result.outcome.success
		dagStepAfterCompletion(s.Steps[result.index].Name)
		if result.outcome.success {
			capturesByStep[s.Steps[result.index].Name] = result.outcome.captures
		}
		for _, dependent := range dependents[result.index] {
			remainingDependencies[dependent]--
			if remainingDependencies[dependent] == 0 {
				schedule(dependent)
			}
		}
	}

	passed := 0
	skipped := 0
	for i := range s.Steps {
		result := outcomes[i].step
		cr.Steps = append(cr.Steps, result)
		switch result.Status {
		case report.StatusPass:
			passed++
		case report.StatusSkipped:
			skipped++
		default:
			cr.Status = report.StatusFail
			if cr.Error == "" {
				cr.Error = fmt.Sprintf("%s: %s", result.Name, result.Error)
			}
		}
	}
	if passed == 0 && skipped == len(s.Steps) {
		cr.Status = report.StatusSkipped
	}
}

func dagStepData(base map[string]any, step scenario.Step, indexByName map[string]int, steps []scenario.Step, capturesByStep map[string]map[string]any) map[string]any {
	data := vars.Clone(base)
	visited := map[string]bool{}
	var include func(string)
	include = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dependency := range steps[indexByName[name]].DependsOn {
			include(dependency)
		}
		for key, value := range capturesByStep[name] {
			data[key] = value
		}
	}
	for _, dependency := range step.DependsOn {
		include(dependency)
	}
	return data
}

func capturedData(data map[string]any, step scenario.Step) map[string]any {
	captures := make(map[string]any, len(step.Capture))
	for name := range step.Capture {
		if value, ok := data[name]; ok {
			captures[name] = value
		}
	}
	return captures
}

func missingPrerequisites(data map[string]any, requirements []string) []string {
	prerequisites, _ := data["prerequisites"].(map[string]any)
	missing := make([]string, 0)
	for _, requirement := range requirements {
		var current any = prerequisites
		found := true
		for _, part := range strings.Split(requirement, ".") {
			values, ok := current.(map[string]any)
			if !ok {
				found = false
				break
			}
			current, ok = values[part]
			if !ok {
				found = false
				break
			}
		}
		if !found {
			missing = append(missing, requirement)
		}
	}
	return missing
}

func annotateStep(result *report.Step, step scenario.Step) {
	result.DependsOn = append([]string(nil), step.DependsOn...)
	result.DirectNeeds = append([]string(nil), step.Needs...)
	result.RequiresPrerequisites = append([]string(nil), step.RequiresPrerequisites...)
	result.Locks = append([]string(nil), step.Locks...)
}

// Test synchronization points for observing admission to the Team replay
// fence. Production keeps both no-op defaults.
var teamCreateBeforeReplayLock = func(string) {}
var teamDeleteBeforeReplayLock = func() {}

func runStep(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, timeout time.Duration) (report.Step, bool) {
	if st.Local == nil && cl.journal != "" {
		command, err := vars.Render(st.Run, data)
		if err == nil && isTeamCreate(command) {
			path, err := cl.checkedJournalPath("primary")
			if err == nil {
				var result report.Step
				var ok bool
				teamCreateBeforeReplayLock(command)
				known, parentErr := cl.rememberedJournalParent(path)
				if parentErr != nil {
					return report.Step{Name: st.Name, Status: report.StatusError, Error: parentErr.Error()}, false
				}
				lock := journalfile.WithReplayLock
				if known != nil {
					lock = journalfile.WithExistingReplayLock
				}
				err = lock(ctx, path, func() error {
					if err := checkJournalParentIdentity(path, known); err != nil {
						return err
					}
					if err := cl.checkDurablePendingQuotaUpdate(ctx); err != nil {
						return err
					}
					result, ok = runStepUnfenced(ctx, opt, execCfg, cl, scope, data, st, timeout)
					return nil
				})
				if err == nil {
					return result, ok
				}
				if result.Name != "" {
					result.Status = report.StatusError
					if result.Error == "" {
						result.Error = err.Error()
					} else {
						result.Error = errors.Join(errors.New(result.Error), err).Error()
					}
					return result, false
				}
			}
			return report.Step{Name: st.Name, Status: report.StatusError, Error: err.Error()}, false
		}
	}
	return runStepUnfenced(ctx, opt, execCfg, cl, scope, data, st, timeout)
}

func runStepUnfenced(ctx context.Context, opt Options, execCfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, timeout time.Duration) (report.Step, bool) {
	if opt.quotaLifecycle {
		cl.mu.Lock()
		cl.quotaLifecycleRequired = true
		cl.mu.Unlock()
	}
	wantExit := 0
	if st.Exit != nil {
		wantExit = *st.Exit
	}
	// not_found_ok accepts the NotFound exit code as success, mirroring the
	// teardown cleanup semantics: the resource is already gone.
	exitOK := func(code int) bool {
		return code == wantExit || (st.NotFoundOK && code == exitNotFound)
	}
	sr := report.Step{Name: st.Name, WantExit: wantExit, Status: report.StatusPass}

	lockKeys, err := renderLockKeys(data, st.Locks)
	if err != nil {
		sr.Status, sr.Error = report.StatusError, err.Error()
		return sr, false
	}

	var res execpkg.Result
	var renderedCommand string
	var privateDisplay string
	var teamIntent *teamCreateIntent
	var templateIntent *templateCreateIntent
	var cacheReservation *cleanupItem
	var quotaReservation *cleanupItem
	var ownedTeamDelete bool
	if st.Local != nil {
		sctx, cancel := context.WithTimeout(ctx, timeout)
		res = runLocalAction(sctx, data, *st.Local)
		cancel()
		sr.Command = report.Scrub(res.Command)
		sr.Exit = res.Exit
		sr.DurationMs = res.Duration.Milliseconds()
		sr.Stdout = strings.TrimSpace(res.Stdout)
		sr.Stderr = strings.TrimSpace(res.Stderr)
	} else {
		cmd, renderErr := vars.Render(st.Run, data)
		if renderErr != nil {
			sr.Status, sr.Error = report.StatusError, "render: "+renderErr.Error()
			return sr, false
		}
		if journalcmd.RawQuotaMutationCandidate(cmd) {
			sr.Status, sr.Error = report.StatusError, "raw FCSandbox Quota mutation is unsupported in E2E cases"
			return sr, false
		}
		display, displayErr := renderReportCommand(st.Run, data)
		if displayErr != nil {
			sr.Status, sr.Error = report.StatusError, "render display: "+displayErr.Error()
			return sr, false
		}
		if display != cmd {
			privateDisplay = report.Scrub(display)
		}
		renderedCommand = cmd
		if opt.quotaLifecycle {
			if path, pathErr := commandAction(cmd); pathErr == nil && len(path) >= 2 && (path[0] == "sandbox" || path[0] == "sbx") && path[1] == "quota" {
				cl.mu.Lock()
				binding := cl.quotaBinding
				cl.mu.Unlock()
				if binding == nil {
					sr.Status, sr.Error = report.StatusError, "protected Quota requires its owned Team credential binding"
					return sr, false
				}
				var verified execpkg.Config
				verified, err = journalfile.ProbeQuotaBinding(ctx, binding, execCfg)
				if err != nil {
					sr.Status, sr.Error = report.StatusError, err.Error()
					return sr, false
				}
				// Keep the original selectors for later journal checks. A read
				// executes outside the cleanup guard, so it needs the pinned config.
				if !quotaUpdateCandidate(st, cmd) && !quotaDeleteCandidate(cmd) {
					execCfg = verified
				}
			}
		}
		quotaCandidate := quotaUpdateCandidate(st, cmd)
		quotaDelete := quotaDeleteCandidate(cmd)
		teamDelete := ownedTeamDeleteCandidate(cmd)
		if quotaCandidate && opt.quotaLifecycleKnown && !opt.quotaLifecycle {
			sr.Status, sr.Error = report.StatusError, "Quota update was not identifiable before its Team allocation"
			return sr, false
		}
		if ordinaryQuotaTeardown(st, cmd) {
			sr.Status, sr.Error = report.StatusError, "Quota mutation cannot be an ordinary teardown"
			return sr, false
		}
		if quotaCandidate && opt.quotaLifecycleForbidden {
			sr.Status, sr.Error = report.StatusError, "Quota update requires a serial sequential lifecycle"
			return sr, false
		}
		if !quotaCandidate && !quotaDelete {
			nativeCreate, parseErr := nativeTemplateCreateStatus(cmd)
			if parseErr != nil {
				sr.Status, sr.Error = report.StatusError, parseErr.Error()
				return sr, false
			}
			if isTeamCreate(cmd) {
				var quotaBinding *report.QuotaCredentialBinding
				if opt.quotaLifecycle {
					quotaBinding, execCfg, err = captureQuotaCredentialBinding(ctx, execCfg, timeout)
					if err != nil {
						sr.Status, sr.Error = report.StatusError, err.Error()
						return sr, false
					}
				}
				cl.mu.Lock()
				for _, existing := range *scope {
					if !existing.done && !existing.teamCreateConfirmed && ownedTeamDeleteCandidate(existing.cmd) {
						cl.mu.Unlock()
						sr.Status, sr.Error = report.StatusError, "protected Team creation conflicts with an ordinary Team finalizer"
						return sr, false
					}
				}
				cl.teamCreatesPending++
				cl.mu.Unlock()
				defer func() {
					cl.mu.Lock()
					cl.teamCreatesPending--
					cl.mu.Unlock()
				}()
				teamIntent, err = reserveTeamCreate(cmd, data, execCfg, cl, st, quotaBinding)
				if err != nil {
					sr.Status, sr.Error = report.StatusError, err.Error()
					return sr, false
				}
				cmd = teamIntent.command()
			} else if nativeCreate {
				templateIntent, err = reserveTemplateCreate(ctx, cmd, execCfg, cl, st, timeout)
				if err != nil {
					sr.Status, sr.Error = report.StatusError, err.Error()
					return sr, false
				}
				cmd, execCfg = templateIntent.command, templateIntent.cfg
			}
			if teamIntent == nil && templateIntent == nil {
				cmd, execCfg, cacheReservation, err = cl.reserveTemplateCacheCreate(ctx, scope, cmd, execCfg, data, st, lockKeys)
				if err != nil {
					sr.Status, sr.Error = report.StatusError, err.Error()
					return sr, false
				}
			}
			cmd, err = cl.bindNativeTemplateDelete(*scope, cmd, "primary")
			if err != nil {
				sr.Status, sr.Error = report.StatusError, err.Error()
				return sr, false
			}
			cmd, execCfg, err = cl.bindOwnedNativeTemplate(ctx, cmd, "primary", execCfg, timeout)
			if err != nil {
				sr.Status, sr.Error = report.StatusError, err.Error()
				return sr, false
			}
			cmd, err = cl.bindOwnedTemplateCacheDelete(*scope, cmd, execCfg)
			if err != nil {
				sr.Status, sr.Error = report.StatusError, err.Error()
				return sr, false
			}
		}
		renderedCommand = cmd
		invoked := false
		invokeAttempts := func() error {
			for attempt := 0; ; attempt++ {
				sctx, cancel := context.WithTimeout(ctx, timeout)
				invokeCommand := func(bound execpkg.Config) error {
					if teamIntent != nil {
						if err := checkJournalParentIdentity(teamIntent.journalPath, teamIntent.parent); err != nil {
							return err
						}
					}
					if quotaDelete {
						if err := cl.verifyOwnedQuotaDelete(*scope, data, cmd, "primary"); err != nil {
							return err
						}
					}
					var teamItem *cleanupItem
					if teamDelete {
						var err error
						teamItem, err = cl.prepareOwnedTeamDelete(*scope, cmd, "primary", nil)
						if err != nil {
							return err
						}
						ownedTeamDelete = teamItem != nil
					}
					cacheCommand, recognized, parseErr := parseTemplateCacheCommand(cmd)
					if parseErr != nil {
						return parseErr
					}
					if recognized && cacheCommand.action == "delete" {
						cl.mu.Lock()
						defer cl.mu.Unlock()
						if err := cl.verifyReservedTemplateCacheDelete(*scope, cmd); err != nil {
							return err
						}
					}
					if quotaCandidate && opt.quotaLifecycle {
						cl.mu.Lock()
						binding := cl.quotaBinding
						cl.mu.Unlock()
						verified, err := journalfile.ProbeQuotaBinding(sctx, binding, cl.execCfg["primary"])
						if err != nil {
							return err
						}
						bound = verified
					}
					invoked = true
					res = cl.runCommand(sctx, bound, cmd)
					if quotaDelete && res.Err == nil && (res.Exit == 0 || res.Exit == exitNotFound && exitOK(res.Exit)) {
						return cl.consumeOwnedQuotaDelete(*scope, cmd, "primary")
					}
					if teamItem != nil && res.Err == nil && (res.Exit == 0 || res.Exit == exitNotFound && exitOK(res.Exit)) {
						if rawTeamDeleteCandidate(cmd) {
							if res.Exit != 0 {
								return fmt.Errorf("raw Team delete did not confirm business success")
							}
							if err := acceptedRawTeamDelete(res, bound.Region); err != nil {
								return err
							}
							// Raw code 200 accepts deletion but does not wait for Team
							// absence. Keep the marked finalizer for first-class cleanup.
							return nil
						}
						return cl.consumeOwnedTeamDelete(teamItem)
					}
					return nil
				}
				invoke := func(bound execpkg.Config) error {
					if quotaDelete || teamDelete {
						claim := cmd
						if teamDelete {
							claim = ownedTeamClaimKey(cmd, cl.execCfg["primary"].Region)
						}
						return cl.withDeleteClaim("primary", claim, func() error { return invokeCommand(bound) })
					}
					return invokeCommand(bound)
				}
				if isDestructiveStep(cmd) {
					if teamDelete {
						teamDeleteBeforeReplayLock()
					}
					if err := cl.withTemplateCleanupGuard(sctx, "primary", invoke, cmd); err != nil {
						cancel()
						return err
					}
				} else {
					if err := invoke(execCfg); err != nil {
						cancel()
						return err
					}
				}
				cancel()
				if teamIntent != nil || templateIntent != nil || cacheReservation != nil || quotaReservation != nil || res.Exit == 0 || !isTransientNetworkError(res) || attempt >= len(stepRetryDelays) {
					break
				}
				shown := report.Scrub(renderedCommand)
				if privateDisplay != "" {
					shown = privateDisplay
				}
				opt.Logf("step retry %d/%d after transient network error: %s", attempt+1, len(stepRetryDelays), shown)
				select {
				case <-ctx.Done():
				case <-time.After(stepRetryDelays[attempt]):
				}
				if ctx.Err() != nil {
					break
				}
			}
			return nil
		}
		if quotaCandidate {
			if cl.journal == "" {
				sr.Status, sr.Error = report.StatusError, "Quota update requires a run-bound cleanup journal"
				return sr, false
			}
			_, _, requested := cl.journalValues(&cleanupItem{role: "primary"})
			path, resolveErr := cl.checkedJournalPath("primary")
			if resolveErr != nil {
				sr.Status, sr.Error = report.StatusError, resolveErr.Error()
				return sr, false
			}
			if path != requested {
				sr.Status, sr.Error = report.StatusError, "Quota update requires a canonical cleanup journal path"
				return sr, false
			}
			known, parentErr := cl.rememberedJournalParent(path)
			if parentErr != nil {
				sr.Status, sr.Error = report.StatusError, parentErr.Error()
				return sr, false
			}
			if known == nil {
				sr.Status, sr.Error = report.StatusError, "Quota update requires a remembered owned Team journal parent"
				return sr, false
			}
			quotaBeforeReplayLock()
			if err := checkJournalParentIdentity(path, known); err != nil {
				sr.Status, sr.Error = report.StatusError, err.Error()
				return sr, false
			}
			err = journalfile.WithExistingReplayLock(ctx, path, func() error {
				if err := checkJournalParentIdentity(path, known); err != nil {
					return err
				}
				var reserveErr error
				quotaReservation, reserveErr = cl.reserveOwnedQuotaUpdate(ctx, path, scope, data, st, cmd, execCfg, lockKeys)
				if reserveErr != nil {
					return reserveErr
				}
				if quotaReservation == nil {
					return fmt.Errorf("Quota update requires a durable owned reservation before invocation")
				}
				invokeErr := invokeAttempts()
				if !res.Started {
					cl.recordUnstartedQuotaUpdate(quotaReservation)
					if retireErr := cl.retireUnstartedQuotaUpdate(quotaReservation); retireErr != nil {
						return errors.Join(invokeErr, res.Err, fmt.Errorf("retire unstarted Quota reservation: %w", retireErr))
					}
				}
				if invokeErr != nil {
					return invokeErr
				}
				if quotaReservation != nil && res.Err == nil && res.Exit == 0 {
					return cl.settleOwnedQuotaUpdate(quotaReservation)
				}
				return nil
			})
		} else {
			err = invokeAttempts()
		}
		// A journal error after a subprocess must not erase its provider evidence.
		// Preflight errors have no command result to record.
		if invoked {
			sr.Command = report.Scrub(res.Command)
			if privateDisplay != "" {
				sr.Command = privateDisplay
			}
			sr.Exit = res.Exit
			sr.DurationMs = res.Duration.Milliseconds()
			sr.Stdout = strings.TrimSpace(res.Stdout)
			sr.Stderr = strings.TrimSpace(res.Stderr)
		}
		if err != nil {
			sr.Status, sr.Error = report.StatusError, err.Error()
			if quotaCandidate {
				sr.Error = "Quota update coordination: " + sr.Error
			} else if quotaDelete {
				sr.Error = "Quota delete coordination: " + sr.Error
			}
			return sr, false
		}
	}
	if quotaReservation != nil && !res.Started {
		// The exact pending child was retired under the exclusive replay fence.
		// A retirement error returned above with the in-memory fence retained.
		sr.Status, sr.Error = report.StatusError, "Quota update did not start"
		if res.Err != nil {
			sr.Error = res.Err.Error()
		}
		return sr, false
	}
	if quotaReservation != nil && cl.isQuotaUpdatePending(quotaReservation) {
		sr.Status, sr.Error = report.StatusError, "Quota update remains pending after an uncertain result"
		if res.Err != nil {
			sr.Error += ": " + res.Err.Error()
		} else if res.Exit != 0 {
			sr.Error += ": " + failureDetail(res)
		}
		return sr, false
	}
	if cacheReservation != nil && !res.Started {
		if err := cl.retireUnstartedCacheCreate(cacheReservation); err != nil {
			sr.Status, sr.Error = report.StatusError, "retire unstarted Template Cache create: "+err.Error()
			return sr, false
		}
	}
	if cacheReservation != nil && res.Err == nil && res.Exit == 0 {
		if !confirmedTemplateCacheReady(res, cacheReservation) {
			sr.Status, sr.Error = report.StatusError, "Template Cache create did not confirm a ready owned cache"
			return sr, false
		}
		if err := cl.settleCacheCreate(cacheReservation); err != nil {
			sr.Status, sr.Error = report.StatusError, "settle Template Cache create: "+err.Error()
			return sr, false
		}
	}
	if templateIntent != nil {
		if err := finishTemplateCreate(res, cl, scope, data, lockKeys, timeout, templateIntent); err != nil {
			sr.Status = report.StatusFail
			sr.Error = failureDetail(res)
			if res.Err != nil {
				sr.Error = res.Err.Error()
			}
			if sr.Error != "" {
				sr.Error += "; "
			}
			sr.Error += err.Error()
			return sr, false
		}
	}
	teamID, validTeamID := teamCreateIdentity(res)
	if teamIntent != nil && (!validTeamID || res.Err != nil || res.Exit != 0) {
		if teamIntent.QuotaBinding != nil {
			cl.mu.Lock()
			cl.unresolvedQuotaTeamCreate = true
			cl.mu.Unlock()
		}
		verifiedReturnedID := ""
		if validTeamID {
			verifiedReturnedID = teamID
		}
		recoveryErr := reconcileTeamCreate(execCfg, cl, scope, data, st, lockKeys, timeout, teamIntent, verifiedReturnedID)
		sr.Status = report.StatusFail
		sr.Error = failureDetail(res)
		if res.Err != nil {
			sr.Error = res.Err.Error()
		}
		if sr.Error != "" {
			sr.Error += "; "
		}
		sr.Error += recoveryErr.Error()
		return sr, false
	}

	// Register teardown as soon as the create-ish step ran (even if asserts
	// later fail) so the resource it produced is cleaned up.
	if st.Teardown != "" && templateIntent == nil && cacheReservation == nil {
		td, terr := vars.Render(st.Teardown, mergeCaptures(data, res, st))
		if teamIntent != nil {
			// The normal success path must use the same safe, exact target as
			// reconciliation, independently of case capture expressions.
			teamData := vars.Clone(data)
			teamData["team_id"] = teamID
			td, terr = renderTeamRecoveryDelete(st, teamData)
			if terr != nil {
				sr.Status, sr.Error = report.StatusError, "Team teardown: "+terr.Error()
				return sr, false
			}
		}
		if terr != nil {
			// Failed creates commonly have no resource ID. Preserve the command
			// error. A structured recovery delete from ecctl is safe to journal
			// after the same strict validation used for ordinary teardown.
			if res.Exit != 0 {
				if recovery, ok := recoveryDeleteCommand(res.JSON); ok {
					if err := cl.push(scope, caseScope(data), recovery, st.TeardownRegion, lockKeys); err != nil {
						sr.Status, sr.Error = report.StatusError, "cleanup journal: "+err.Error()
						return sr, false
					}
				} else if hasRecoveryCommand(res.JSON) {
					// Keep the original allocation evidence in stdout, but never
					// persist or execute a recovery outside the delete whitelist.
					sr.Status = report.StatusError
					sr.Error = failureDetail(res) + "; cleanup recovery rejected; original target retained in stdout"
					return sr, false
				}
			}
			// Teardown rendering is authoritative only after the command reached
			// its expected exit status.
			if res.Err == nil && exitOK(res.Exit) {
				sr.Status, sr.Error = report.StatusError, "render teardown: "+terr.Error()
				return sr, false
			}
		} else {
			if quotaReservation != nil {
				if !commandsEquivalent(td, quotaReservation.cmd) {
					sr.Status, sr.Error = report.StatusError, "Quota teardown differs from its durable reservation"
					return sr, false
				}
			} else if teamIntent != nil {
				confirmed := res.Err == nil && res.Exit == 0
				_, err = cl.pushItem(scope, caseScope(data), td, st.TeardownRegion, lockKeys, false, confirmed, teamIntent.QuotaBinding != nil)
				if err != nil {
					sr.Status, sr.Error = report.StatusError, "cleanup journal: "+err.Error()
					return sr, false
				}
			} else if err := cl.push(scope, caseScope(data), td, st.TeardownRegion, lockKeys); err != nil {
				sr.Status, sr.Error = report.StatusError, "cleanup journal: "+err.Error()
				return sr, false
			}
		}
	}

	if res.Err != nil {
		sr.Status, sr.Error = report.StatusError, res.Err.Error()
		return sr, false
	}
	if !exitOK(res.Exit) {
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
		return sr, false
	}
	if res.Exit == 0 && st.Local == nil && !quotaDeleteCandidate(renderedCommand) && !ownedTeamDelete {
		if err := cl.satisfy(*scope, renderedCommand, "primary"); err != nil {
			sr.Status, sr.Error = report.StatusError, "cleanup journal: "+err.Error()
			return sr, false
		}
	}
	return sr, true
}

// Structured recovery signals a partial mutation even if its argv cannot be
// safely replayed. Safety validation must not decide whether to create again.
func hasRecoveryCommand(doc any) bool {
	root, ok := doc.(map[string]any)
	if !ok {
		return false
	}
	payload, ok := root["error"].(map[string]any)
	if !ok {
		return false
	}
	args, ok := payload["recovery_command"].([]any)
	return ok && len(args) != 0
}

func recoveryDeleteCommand(doc any) (string, bool) {
	root, ok := doc.(map[string]any)
	if !ok {
		return "", false
	}
	payload, ok := root["error"].(map[string]any)
	if !ok {
		return "", false
	}
	rawArgs, ok := payload["recovery_command"].([]any)
	if !ok || len(rawArgs) == 0 {
		return "", false
	}
	args := make([]string, 0, len(rawArgs))
	for _, rawArg := range rawArgs {
		arg, ok := rawArg.(string)
		if !ok || arg == "" || strings.ContainsAny(arg, "\x00\t\r\n") {
			return "", false
		}
		args = append(args, teamCommandArgument(arg))
	}
	command := strings.Join(args, " ")
	if !isReplayableTeardown(command) {
		return "", false
	}
	return command, true
}

// isTransientNetworkError reports whether the step failure looks like a
// transport-level network error that may succeed on retry. It checks stdout
// and stderr for Go net package error markers such as "dial tcp", "bad file
// descriptor", and "connection refused". Non-network failures (assertion
// mismatches, permission errors, quota limits) are not retried.
func isTransientNetworkError(res execpkg.Result) bool {
	if res.Exit == 0 {
		return false
	}
	if hasRecoveryCommand(res.JSON) {
		// Preserve the first allocation even if cleanup validation later fails;
		// replaying the mutation would replace its only recovery evidence.
		return false
	}
	if root, ok := res.JSON.(map[string]any); ok {
		if failure, ok := root["error"].(map[string]any); ok {
			if retryable, explicit := failure["retryable"].(bool); explicit && !retryable {
				return false
			}
			if failure["code"] == "FCTemplateBuildFailed" {
				return false
			}
		}
	}
	text := strings.ToLower(strings.Join([]string{res.Stdout, res.Stderr}, " "))
	for _, marker := range []string{
		"dial tcp",
		"bad file descriptor",
		"connection refused",
		"connection reset",
		"i/o timeout",
		"no such host",
		"tls: handshake",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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

func renderReportCommand(source string, data map[string]any) (string, error) {
	return report.RenderCommand(source, func(text string) (string, error) {
		return vars.Render(text, data)
	})
}

func renderDryStep(data map[string]any, st scenario.Step) report.Step {
	sr := report.Step{Name: st.Name, Status: report.StatusSkipped}
	if st.Local != nil {
		source, sourceErr := vars.Render(st.Local.Source, data)
		destination, destinationErr := vars.Render(st.Local.Destination, data)
		if sourceErr != nil || destinationErr != nil {
			if sourceErr != nil {
				sr.Status, sr.Error = report.StatusError, "render local source: "+sourceErr.Error()
			} else {
				sr.Status, sr.Error = report.StatusError, "render local destination: "+destinationErr.Error()
			}
			return sr
		}
		sr.Command = fmt.Sprintf("local %s %s %s", st.Local.Action, source, destination)
	} else {
		rendered, err := vars.Render(st.Run, data)
		if err != nil {
			sr.Status, sr.Error = report.StatusError, "render: "+err.Error()
			return sr
		}
		if ordinaryQuotaTeardown(st, rendered) {
			sr.Status, sr.Error = report.StatusError, "Quota mutation cannot be an ordinary teardown"
			return sr
		}
		cmd, err := renderReportCommand(st.Run, data)
		if err != nil {
			sr.Status, sr.Error = report.StatusError, "render: "+err.Error()
			return sr
		}
		sr.Command = cmd
	}
	for v := range st.Capture {
		data[v] = "<" + v + ">"
	}
	return sr
}

// captureValue resolves a capture path (relative to At) from a JSON document.
func captureValue(doc any, at, path string) (any, bool) {
	return jsonGet(doc, at, path)
}
