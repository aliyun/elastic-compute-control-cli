// Command ecctl-e2e drives the ecctl end-to-end suite: provision a shared
// stack, run resource lifecycle cases against real Alibaba Cloud in bounded
// parallel, tear everything down, and emit logs + JSON/HTML/JUnit reports.
//
// Case selection follows pytest: positional targets are node ids (a dir, a
// case file, or file::step), and -k is a boolean keyword expression. Collection
// is a mode of `run` (--collect-only), not a separate command.
//
// Subcommands: run, sweep, coverage, lint, report.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/caselint"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/coverage"
	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	paramspkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/params"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/prereqcheck"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/regionselect"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/runner"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/runplan"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	surfacepkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/surface"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

// Exit codes, modeled on pytest.
const (
	exitOK          = 0
	exitTestsFailed = 1
	exitInterrupted = 2
	exitUsage       = 4
	exitNoCases     = 5
)

// exitError carries an explicit process exit code up to main, so commands never
// call os.Exit themselves (which would skip deferred teardown).
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func main() {
	root := &cobra.Command{
		Use:   "ecctl-e2e",
		Short: "End-to-end test runner for ecctl against real Alibaba Cloud",
		Long: "End-to-end test runner for ecctl against real Alibaba Cloud.\n\n" +
			"Exit codes: 0 ok, 1 cases failed, 2 interrupted, 4 usage error, 5 no cases selected.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{code: exitUsage, msg: err.Error()}
	})
	root.AddCommand(runCmd(), sweepCmd(), coverageCmd(), lintCmd(), reportCmd())
	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, "error:", ee.msg)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitTestsFailed)
	}
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05")+" "+format+"\n", args...)
}

// signalContext cancels on SIGINT/SIGTERM so the runner can run teardown (which
// uses a fresh background context) before the process exits.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func runCmd() *cobra.Command {
	var (
		configFile                     string
		casesDir, stackFile, inputsDir string
		parameterMode, parameterPolicy string
		cleanupJournal                 string
		region, zone, label, ecctlBin  string
		keyword, reportDir, output     string
		surface                        string
		regionProfiles                 []fixtureconfig.RegionProfile
		parallel                       int
		stepTimeout                    time.Duration
		keep, dryRun, exitZero         bool
		collectOnly, quiet, verbose    bool
	)
	cmd := &cobra.Command{
		Use:   "run [TARGET...]",
		Short: "Run E2E cases (provision stack, run cases, tear down, report)",
		Long: "Run E2E cases: provision the shared stack, run selected cases in\n" +
			"bounded parallel, tear everything down, and write reports.\n\n" +
			"TARGET is a pytest-style node id: a directory, a case file, or\n" +
			"file::step (which runs the case up to and including that step). With\n" +
			"no TARGET, all cases under --cases are selected, then narrowed by -k.",
		Example: "  ecctl-e2e run\n" +
			"  ecctl-e2e run cases/vpc/ -k \"eni or vpc\"\n" +
			"  ecctl-e2e run cases/ecs/instance-lifecycle.yaml::create\n" +
			"  ecctl-e2e run --collect-only",
		RunE: func(cmd *cobra.Command, args []string) error {
			runConfig, configPresent, err := loadRunConfigFile(configFile, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}
			if configPresent {
				if err := applyRunConfigPaths(cmd, runConfig, configFile); err != nil {
					return err
				}
				regionProfiles = append([]fixtureconfig.RegionProfile(nil), runConfig.Regions.Candidates...)
			}
			const namePrefix = "ecctl-e2e"
			if len(regionProfiles) == 0 {
				// Preserve the legacy stack/default-region behavior when a custom
				// run has no top-level config file.
				regionProfiles = []fixtureconfig.RegionProfile{{ID: region}}
			}
			stackFile = optionalDefaultPath(cmd, "stack", stackFile)
			parameterPolicy = optionalDefaultPath(cmd, "parameter-policy", parameterPolicy)
			globalFixture := ""
			if configPresent {
				globalFixture = configFile
			}
			if err := validateRunContracts(casesDir, stackFile, inputsDir, coveragePathForCases(casesDir), globalFixture, parameterPolicy,
				cmd.Flags().Changed("stack"), configPresent, cmd.Flags().Changed("parameter-policy")); err != nil {
				return err
			}

			all, err := scenario.LoadDir(casesDir)
			if err != nil {
				return err
			}
			suites, err := scenario.Select(all, scenario.Selection{
				Targets: args,
				Keyword: keyword,
				Surface: scenario.Surface(surface),
			})
			if err != nil {
				return err
			}

			if collectOnly {
				if err := printCollected(cmd.OutOrStdout(), suites, output, quiet, verbose); err != nil {
					return err
				}
				if len(suites) == 0 {
					return &exitError{code: exitNoCases, msg: "no cases selected"}
				}
				return nil
			}
			if len(suites) == 0 {
				return &exitError{code: exitNoCases, msg: "no cases selected"}
			}
			ctx, cancel := signalContext()
			defer cancel()
			regionProfiles, renewalConfigured := maskUnconfiguredRenewalPrerequisites(regionProfiles)
			primaryRegion := ""
			if cmd.Flags().Changed("region") {
				primaryRegion = region
				renewalConfigured = hasConfiguredRenewalTarget(regionProfiles, primaryRegion)
			}
			var renewalSkippedSuites []*scenario.Suite
			suites, renewalSkippedSuites = partitionRenewalSuites(suites, renewalConfigured)
			var unavailablePrerequisiteSuites []prerequisiteSkippedSuite
			prerequisiteResult := prereqcheck.Result{Profiles: regionProfiles}
			if len(suites) > 0 && !dryRun {
				caps, err := surfacepkg.LoadFromBinary(ctx, ecctlBin)
				if err != nil {
					return fmt.Errorf("load %s capabilities from %s: %w", surface, ecctlBin, err)
				}
				if caps.Surface != surface {
					return fmt.Errorf("selected binary %s reports surface %q, want %q", ecctlBin, caps.Surface, surface)
				}
				surfaceSuites := append([]*scenario.Suite(nil), suites...)
				if stackFile != "" {
					stackSteps, err := runner.StackStepsForSuites(stackFile, suites)
					if err != nil {
						return fmt.Errorf("plan shared stack surface: %w", err)
					}
					stackSuite := &scenario.Suite{Path: stackFile}
					for _, step := range stackSteps {
						stackSuite.Steps = append(stackSuite.Steps, scenario.Step{
							Name: step.ID, Run: step.Run, Teardown: step.Teardown,
						})
					}
					surfaceSuites = append(surfaceSuites, stackSuite)
				}
				if issues := surfacepkg.ValidateSuites(surfaceSuites, caps); len(issues) > 0 {
					return fmt.Errorf("selected binary cannot run %s cases: %s %s: %s", surface, issues[0].Path, issues[0].Code, issues[0].Message)
				}
				required := probedPrerequisites(suites)
				if len(required) > 0 {
					prerequisiteResult, err = prereqcheck.Check(ctx, prereqcheck.Options{
						Profiles: regionProfiles, Required: required, PrimaryRegion: primaryRegion, EcctlBin: ecctlBin,
					})
					if err != nil {
						return fmt.Errorf("check E2E prerequisites: %w", err)
					}
					regionProfiles = prerequisiteResult.Profiles
					for _, warning := range prerequisiteResult.Warnings {
						logf("warning: region %s prerequisite %s unavailable: %s", warning.Region, warning.Prerequisite, warning.Reason)
					}
					suites, unavailablePrerequisiteSuites = partitionUnavailablePrerequisiteSuites(suites, regionProfiles, primaryRegion)
					for _, skipped := range unavailablePrerequisiteSuites {
						logf("warning: skipping %s: %s", skipped.Suite.Path, skipped.Reason)
					}
				}
			}
			var executionUnits []runplan.ExecutionUnit
			if len(suites) > 0 {
				stackPrerequisites := map[string][]string{}
				if stackFile != "" {
					stackPrerequisites, err = runner.StackPrerequisitesBySuite(stackFile, suites)
					if err != nil {
						return fmt.Errorf("plan regional prerequisites: %w", err)
					}
				}
				executionUnits, err = runplan.Build(runplan.Request{
					Suites: suites, Profiles: regionProfiles, PrimaryRegion: primaryRegion,
					StackPrerequisites: stackPrerequisites,
				})
				if err != nil {
					return fmt.Errorf("plan E2E executions: %w", err)
				}
			}

			if label == "" {
				label = defaultLabel()
			}
			executions := make([]report.Execution, 0, len(executionUnits)+2)
			if len(renewalSkippedSuites) > 0 {
				executions = append(executions, renewalNotConfiguredExecution(renewalSkippedSuites))
				logf("skipped %d instance renewal case(s); no selected region profile configures ecs.instance_renew.instance_id", len(renewalSkippedSuites))
			}
			if len(unavailablePrerequisiteSuites) > 0 {
				executions = append(executions, unavailablePrerequisiteExecution(unavailablePrerequisiteSuites))
			}
			var firstExecutionErr error
			for _, unit := range executionUnits {
				if ctx.Err() != nil {
					break
				}
				attempts := make([]report.ExecutionAttempt, 0, len(unit.Assignments))
				var finalUnitRun *report.Run
				var finalUnitErr error
				var finalRegions map[string]string
				for attemptIndex, assignment := range unit.Assignments {
					if ctx.Err() != nil {
						break
					}
					selectedRegions, regionMapping, resolveErr := resolveRunnerRegions(unit, assignment)
					if resolveErr != nil {
						return fmt.Errorf("resolve %s prerequisites: %w", unit.ID, resolveErr)
					}
					attemptJournal := cleanupJournalForAttempt(
						cleanupJournal, reportDir, label, unit.ID, len(executionUnits), attemptIndex, len(unit.Assignments),
					)
					var preResolvedLingjun *paramspkg.LingjunResult
					if resolved, ok := prerequisiteResult.LingjunByRegion[regionMapping[runplan.PrimaryRole]]; ok {
						preResolvedLingjun = &resolved
					}
					opt := runner.Options{
						CasesDir:           casesDir,
						StackFile:          stackFile,
						InputsDir:          inputsDir,
						GlobalFixture:      globalFixture,
						ParameterMode:      parameterMode,
						ParameterPolicy:    parameterPolicy,
						CleanupJournal:     attemptJournal,
						Region:             regionMapping[runplan.PrimaryRole],
						Regions:            selectedRegions,
						Zone:               zone,
						RunName:            namePrefix + "-" + label,
						RunID:              label,
						ExecutionID:        unit.ID,
						Surface:            surface,
						Parallel:           parallel,
						EcctlBin:           ecctlBin,
						StepTimeout:        stepTimeout,
						Keep:               keep,
						DryRun:             dryRun,
						Suites:             unit.Suites,
						Logf:               logf,
						PreResolvedLingjun: preResolvedLingjun,
					}
					unitRun, runErr := runner.Run(ctx, opt)
					retry, reason := regionselect.Classify(unitRun, runErr)
					status := "pass"
					if runErr != nil || (unitRun != nil && unitRun.Failed()) {
						status = "fail"
					}
					attempts = append(attempts, report.ExecutionAttempt{Regions: regionMapping, Status: status, Reason: reason})
					if retry && !keep && attemptIndex+1 < len(unit.Assignments) {
						next := assignmentRegions(unit.Assignments[attemptIndex+1])
						logf("%s assignment %s unavailable; retrying with %s: %s", unit.ID, formatRegionMapping(regionMapping), formatRegionMapping(next), reason)
						continue
					}
					finalUnitRun, finalUnitErr, finalRegions = unitRun, runErr, regionMapping
					break
				}
				execution := report.Execution{ID: unit.ID, Signature: unit.Signature, Regions: finalRegions, Attempts: attempts}
				if finalUnitRun != nil {
					execution.StartedAt = finalUnitRun.StartedAt
					execution.FinishedAt = finalUnitRun.FinishedAt
					execution.Parameters = finalUnitRun.Parameters
					execution.Cases = finalUnitRun.Cases
					execution.Manifest = finalUnitRun.Manifest
				}
				if finalUnitErr != nil {
					execution.Error = finalUnitErr.Error()
					if firstExecutionErr == nil {
						firstExecutionErr = finalUnitErr
					}
				}
				executions = append(executions, execution)
			}
			finalRun := report.Aggregate(label, surface, ecctlBin, executions)
			// Always emit the report, even when a unit errored or cases failed.
			finalRun.Redact()
			if werr := writeReports(finalRun, reportDir); werr != nil {
				logf("warning: writing reports failed: %v", werr)
			} else if reportDir != "" {
				logf("report: %s/e2e-report.{json,html,xml}", reportDir)
			}
			logf("done: %d passed, %d failed, %d skipped of %d in %d executions",
				finalRun.Summary.Passed, finalRun.Summary.Failed, finalRun.Summary.Skipped, finalRun.Summary.Cases, len(executions))
			if firstExecutionErr != nil {
				return firstExecutionErr
			}
			if exitZero {
				return nil
			}
			if ctx.Err() != nil {
				return &exitError{code: exitInterrupted, msg: "interrupted"}
			}
			if finalRun != nil && finalRun.Failed() {
				return &exitError{code: exitTestsFailed}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&configFile, "config", "c", "e2e.yaml", "top-level E2E config (CLI flags still win)")
	f.StringVar(&casesDir, "cases", "cases", "directory of case YAML files")
	f.StringVar(&stackFile, "stack", "fixtures/stack.yaml", "shared fixture stack file")
	f.StringVar(&inputsDir, "inputs", "fixtures/inputs", "per-resource inputs directory")
	f.StringVar(&parameterMode, "parameter-mode", "auto", "dynamic parameter mode: auto|static")
	f.StringVar(&parameterPolicy, "parameter-policy", "fixtures/parameter-policy.yaml", "bounded dynamic parameter policy")
	f.StringVar(&cleanupJournal, "cleanup-journal", "", "persist registered teardown commands to this local JSON file")
	f.StringVar(&region, "region", "", "Alibaba Cloud region (overrides fixture)")
	f.StringVar(&zone, "zone", "", "availability zone (overrides fixture)")
	f.StringVar(&label, "label", "", "run label for resource names/tags/report (default $GITHUB_RUN_ID or timestamp)")
	f.StringVar(&ecctlBin, "ecctl-bin", "ecctl", "ecctl binary to invoke")
	f.StringVarP(&keyword, "keyword", "k", "", "only run cases matching this expression (e.g. \"vpc or eni\")")
	f.StringVar(&surface, "surface", string(scenario.SurfacePublic), "case command surface: public|full")
	f.IntVar(&parallel, "concurrency", 20, "max cases to run concurrently")
	f.DurationVar(&stepTimeout, "step-timeout", 10*time.Minute, "default per-step timeout")
	f.BoolVar(&keep, "keep", false, "do not tear down created resources (debug)")
	f.BoolVar(&dryRun, "dry-run", false, "render commands without calling the cloud")
	f.BoolVar(&exitZero, "exit-zero", false, "always exit 0 even on failures (nightly)")
	f.BoolVar(&collectOnly, "collect-only", false, "list selected cases without running them (also validates)")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	f.BoolVarP(&quiet, "quiet", "q", false, "terse output (with --collect-only: one path per line)")
	f.BoolVarP(&verbose, "verbose", "v", false, "verbose output (with --collect-only: list step node ids)")
	f.StringVar(&reportDir, "report-dir", "", "write e2e-report.{json,html,xml} here ($GITHUB_STEP_SUMMARY appended if set)")
	return cmd
}

const renewalPrerequisite = "ecs.instance_renew"

type prerequisiteSkippedSuite struct {
	Suite  *scenario.Suite
	Reason string
}

func probedPrerequisites(suites []*scenario.Suite) map[string]bool {
	required := map[string]bool{}
	for _, suite := range suites {
		for _, prerequisite := range suite.RequiresPrerequisites {
			if prerequisite == prereqcheck.ACKRootAccount || prerequisite == prereqcheck.ECSImage || prerequisite == prereqcheck.LingjunCluster {
				required[prerequisite] = true
			}
		}
	}
	return required
}

func partitionUnavailablePrerequisiteSuites(suites []*scenario.Suite, profiles []fixtureconfig.RegionProfile, primaryRegion string) (runnable []*scenario.Suite, skipped []prerequisiteSkippedSuite) {
	for _, suite := range suites {
		var required []string
		for _, prerequisite := range suite.RequiresPrerequisites {
			if prerequisite == prereqcheck.ACKRootAccount || prerequisite == prereqcheck.ECSImage || prerequisite == prereqcheck.LingjunCluster {
				required = append(required, prerequisite)
			}
		}
		if len(required) == 0 || hasAvailablePrerequisites(profiles, primaryRegion, required) {
			runnable = append(runnable, suite)
			continue
		}
		skipped = append(skipped, prerequisiteSkippedSuite{
			Suite:  suite,
			Reason: "no selected region profile has available prerequisites: " + strings.Join(required, ", "),
		})
	}
	return runnable, skipped
}

func hasAvailablePrerequisites(profiles []fixtureconfig.RegionProfile, primaryRegion string, required []string) bool {
	for _, profile := range profiles {
		if primaryRegion != "" && profile.ID != primaryRegion {
			continue
		}
		if profile.HasPrerequisites(required) {
			return true
		}
	}
	return false
}

func unavailablePrerequisiteExecution(suites []prerequisiteSkippedSuite) report.Execution {
	now := time.Now()
	execution := report.Execution{
		ID: "unavailable-prerequisites", Signature: "prerequisite=unavailable", StartedAt: now, FinishedAt: now,
	}
	for _, skipped := range suites {
		name := strings.TrimSuffix(filepath.Base(skipped.Suite.Path), filepath.Ext(skipped.Suite.Path))
		if name == "" {
			name = skipped.Suite.Resource
		}
		execution.Cases = append(execution.Cases, report.Case{
			Name: name, Resource: skipped.Suite.Resource, Path: skipped.Suite.Path,
			Status: report.StatusSkipped, Error: skipped.Reason,
		})
	}
	return execution
}

func partitionRenewalSuites(suites []*scenario.Suite, configured bool) (runnable, skipped []*scenario.Suite) {
	for _, suite := range suites {
		if !configured && requiresPrerequisite(suite, renewalPrerequisite) {
			skipped = append(skipped, suite)
			continue
		}
		runnable = append(runnable, suite)
	}
	return runnable, skipped
}

func requiresPrerequisite(suite *scenario.Suite, prerequisite string) bool {
	for _, required := range suite.RequiresPrerequisites {
		if required == prerequisite {
			return true
		}
	}
	return false
}

// maskUnconfiguredRenewalPrerequisites keeps the region-profile loader
// schema-free while ensuring an empty renewal bundle cannot be scheduled. The
// first get step in the case remains the live queryability gate before renew.
func maskUnconfiguredRenewalPrerequisites(profiles []fixtureconfig.RegionProfile) ([]fixtureconfig.RegionProfile, bool) {
	masked := append([]fixtureconfig.RegionProfile(nil), profiles...)
	configured := false
	for i, profile := range masked {
		bundle, declared := profile.Prerequisites[renewalPrerequisite]
		instanceID, valid := bundle["instance_id"].(string)
		if declared && valid && strings.TrimSpace(instanceID) != "" {
			configured = true
			continue
		}
		if !declared {
			continue
		}
		prerequisites := make(map[string]map[string]any, len(profile.Prerequisites))
		for name, value := range profile.Prerequisites {
			if name != renewalPrerequisite {
				prerequisites[name] = value
			}
		}
		masked[i].Prerequisites = prerequisites
	}
	return masked, configured
}

func hasConfiguredRenewalTarget(profiles []fixtureconfig.RegionProfile, region string) bool {
	for _, profile := range profiles {
		if profile.ID == region && profile.HasPrerequisites([]string{renewalPrerequisite}) {
			return true
		}
	}
	return false
}

func renewalNotConfiguredExecution(suites []*scenario.Suite) report.Execution {
	now := time.Now()
	execution := report.Execution{
		ID:         "configured-instance-renewal",
		Signature:  "prerequisite=ecs.instance_renew",
		StartedAt:  now,
		FinishedAt: now,
	}
	for _, suite := range suites {
		name := strings.TrimSuffix(filepath.Base(suite.Path), filepath.Ext(suite.Path))
		if name == "" {
			name = suite.Resource
		}
		execution.Cases = append(execution.Cases, report.Case{
			Name: name, Resource: suite.Resource, Path: suite.Path, Status: report.StatusSkipped,
			Error: "ecs.instance_renew.instance_id is not configured for the selected region profiles",
		})
	}
	return execution
}

func resolveRunnerRegions(unit runplan.ExecutionUnit, assignment runplan.Assignment) (map[string]runner.Region, map[string]string, error) {
	regions := make(map[string]runner.Region, len(assignment.Regions))
	mapping := make(map[string]string, len(assignment.Regions))
	for role, profile := range assignment.Regions {
		prerequisites, err := profile.ResolvePrerequisites(unit.Requirements[role])
		if err != nil {
			return nil, nil, err
		}
		regions[role] = runner.Region{ID: profile.ID, Prerequisites: prerequisites}
		mapping[role] = profile.ID
	}
	return regions, mapping, nil
}

func assignmentRegions(assignment runplan.Assignment) map[string]string {
	regions := make(map[string]string, len(assignment.Regions))
	for role, profile := range assignment.Regions {
		regions[role] = profile.ID
	}
	return regions
}

func formatRegionMapping(regions map[string]string) string {
	roles := make([]string, 0, len(regions))
	for role := range regions {
		if role != runplan.PrimaryRole {
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	if _, ok := regions[runplan.PrimaryRole]; ok {
		roles = append([]string{runplan.PrimaryRole}, roles...)
	}
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, role+"="+regions[role])
	}
	return strings.Join(parts, ",")
}

func cleanupJournalForAttempt(base, reportDir, label, executionID string, executionCount, attemptIndex, attemptCount int) string {
	if base == "" {
		if reportDir == "" {
			return ""
		}
		base = filepath.Join(reportDir, "cleanup-journal-"+label+".json")
	}
	var suffixes []string
	if executionCount > 1 {
		suffixes = append(suffixes, executionID)
	}
	if attemptCount > 1 {
		suffixes = append(suffixes, "attempt-"+strconv.Itoa(attemptIndex+1))
	}
	if len(suffixes) == 0 {
		return base
	}
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + strings.Join(suffixes, "-") + ext
}

// printCollected renders the selected cases for --collect-only. With -o json it
// emits a machine-readable array; otherwise text: -q is one path per line, -v
// also lists step node ids.
func printCollected(w io.Writer, suites []*scenario.Suite, output string, quiet, verbose bool) error {
	if output == "json" {
		type caseJSON struct {
			Resource string   `json:"resource"`
			Path     string   `json:"path"`
			Steps    []string `json:"steps"`
		}
		out := make([]caseJSON, len(suites))
		for i, s := range suites {
			steps := make([]string, len(s.Steps))
			for j, st := range s.Steps {
				steps[j] = st.Name
			}
			out[i] = caseJSON{Resource: s.Resource, Path: s.Path, Steps: steps}
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(b))
		return nil
	}
	if quiet {
		for _, s := range suites {
			fmt.Fprintln(w, s.Path)
		}
		return nil
	}
	for _, s := range suites {
		fmt.Fprintf(w, "%-24s %2d steps  %s\n", s.Resource, len(s.Steps), s.Path)
		if verbose {
			for _, st := range s.Steps {
				fmt.Fprintf(w, "    %s::%s\n", s.Path, st.Name)
			}
		}
	}
	fmt.Fprintf(w, "%d cases\n", len(suites))
	return nil
}

// loadRunConfigFile loads the structured top-level e2e.yaml. The default
// file is optional for compatibility with isolated/custom case directories;
// an explicitly supplied --config must exist and contain region candidates.
func loadRunConfigFile(path string, explicit bool) (*fixtureconfig.RunConfig, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) && !explicit {
			return nil, false, nil
		}
		return nil, false, err
	}
	config, err := fixtureconfig.LoadRunConfig(path)
	if err != nil {
		return nil, false, fmt.Errorf("load run config: %w", err)
	}
	return config, true, nil
}

func applyRunConfigPaths(cmd *cobra.Command, config *fixtureconfig.RunConfig, configPath string) error {
	paths := map[string]string{
		"cases":            config.Paths.Cases,
		"stack":            config.Paths.Stack,
		"inputs":           config.Paths.Inputs,
		"coverage":         config.Paths.Coverage,
		"parameter-policy": config.Paths.ParameterPolicy,
	}
	for flag, value := range paths {
		if cmd.Flags().Lookup(flag) == nil || value == "" || cmd.Flags().Changed(flag) {
			continue
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(filepath.Dir(configPath), value)
		}
		if err := cmd.Flags().Set(flag, value); err != nil {
			return fmt.Errorf("apply run config path %q: %w", flag, err)
		}
	}
	return nil
}

// writeReports writes e2e-report.{json,html,xml} into dir (created if needed)
// and, when running under GitHub Actions, appends a GFM summary to the step
// summary.
func writeReports(run *report.Run, dir string) error {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := report.WriteJSON(run, filepath.Join(dir, "e2e-report.json")); err != nil {
			return err
		}
		if err := report.WriteHTML(run, filepath.Join(dir, "e2e-report.html")); err != nil {
			return err
		}
		if err := report.WriteJUnit(run, filepath.Join(dir, "e2e-report.xml")); err != nil {
			return err
		}
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		if err := report.WriteSummary(run, summary); err != nil {
			return err
		}
	}
	return nil
}

func sweepCmd() *cobra.Command {
	var (
		configFile, region, ecctlBin, mode string
		ttl                                time.Duration
		dryRun                             bool
		concurrency                        int
	)
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Delete leftover tagged resources (cleanup safety net)",
		Long: "Delete leftover tagged resources. Two modes:\n" +
			"  ttl          delete resources older than --ttl (default)\n" +
			"  finished-run delete only resources whose GitHub run has finished\n" +
			"               (needs GITHUB_REPOSITORY and GH_TOKEN in the env)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			opt := sweeper.Options{
				ConfigFile:  configFile,
				Region:      region,
				EcctlBin:    ecctlBin,
				TTL:         ttl,
				DryRun:      dryRun,
				Concurrency: concurrency,
				Logf:        logf,
			}
			switch mode {
			case "ttl":
			case "finished-run":
				repo := os.Getenv("GITHUB_REPOSITORY")
				if repo == "" {
					return fmt.Errorf("--mode finished-run needs GITHUB_REPOSITORY")
				}
				opt.ByFinishedRun = true
				opt.CheckFinished = sweeper.GitHubRunChecker(repo, os.Getenv("GH_TOKEN"))
			default:
				return &exitError{code: exitUsage, msg: fmt.Sprintf("invalid --mode %q (want ttl|finished-run)", mode)}
			}
			selector := "ttl=" + ttl.String()
			if opt.ByFinishedRun {
				selector = "mode=finished-run"
			}
			regionLabel := region
			if regionLabel == "" {
				regionLabel = "(default)"
			}
			dryNote := ""
			if dryRun {
				dryNote = " [dry-run]"
			}
			logf("sweep: start region=%s %s concurrency=%d%s", regionLabel, selector, concurrency, dryNote)
			res, err := sweeper.Sweep(ctx, opt)
			if err != nil {
				return err
			}
			logf("sweep: done deleted=%d skipped=%d errors=%d", res.Deleted, res.Skipped, res.Errors)
			for _, d := range res.Details {
				logf("sweep: %s", d)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&configFile, "config", "sweep.yaml", "sweep config file")
	f.StringVar(&region, "region", "", "Alibaba Cloud region")
	f.StringVar(&ecctlBin, "ecctl-bin", "ecctl", "ecctl binary to invoke")
	f.StringVar(&mode, "mode", "ttl", "selection mode: ttl|finished-run")
	f.DurationVar(&ttl, "ttl", 0, "delete resources older than this (ttl mode); 0 deletes all matched")
	f.BoolVar(&dryRun, "dry-run", false, "list what would be deleted without deleting")
	f.IntVar(&concurrency, "concurrency", 20, "parallel deletes within a resource kind")
	cmd.AddCommand(sweepCheckCmd(), sweepReplayCmd())
	return cmd
}

func sweepReplayCmd() *cobra.Command {
	var region, ecctlBin, surface, runID string
	cmd := &cobra.Command{
		Use:   "replay JOURNAL",
		Short: "Replay a local cleanup journal in reverse creation order",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			res, err := sweeper.ReplayJournalWithOptions(ctx, args[0], sweeper.ReplayOptions{
				Config: execpkg.Config{Bin: ecctlBin, Region: region}, RunID: runID, Surface: surface,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sweep replay: deleted=%d errors=%d\n", res.Deleted, res.Errors)
			for _, detail := range res.Details {
				fmt.Fprintf(cmd.OutOrStdout(), "sweep replay: %s\n", detail)
			}
			if res.Errors > 0 {
				return &exitError{code: exitTestsFailed, msg: "cleanup journal replay failed"}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&region, "region", "", "Alibaba Cloud region")
	f.StringVar(&ecctlBin, "ecctl-bin", "", "ecctl binary to invoke (defaults to the journal binary)")
	f.StringVar(&surface, "surface", "", "expected command surface: public|full")
	f.StringVar(&runID, "run-id", "", "expected run id")
	return cmd
}

func sweepCheckCmd() *cobra.Command {
	var casesDir, configFile, output string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate sweep coverage for live E2E cases without cloud credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := sweeper.CheckConfig(sweeper.CheckOptions{
				CasesDir:   casesDir,
				ConfigFile: configFile,
			})
			if err != nil {
				return err
			}
			if output == "json" {
				b, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "sweep check: %d cases, %d sweep kinds, %d live creates, %d invalid\n",
					rep.Cases, rep.SweepKinds, rep.LiveCreates, rep.Invalid)
				for _, e := range rep.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %s %s %s: %s: %s\n", e.Path, e.Step, e.Resource, e.Code, e.Message)
				}
			}
			if rep.Invalid > 0 {
				return &exitError{code: exitTestsFailed, msg: fmt.Sprintf("%d sweep check errors", rep.Invalid)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&casesDir, "cases", "cases", "directory of case YAML files")
	f.StringVar(&configFile, "config", "sweep.yaml", "sweep config file")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	return cmd
}

func coverageCmd() *cobra.Command {
	var specsDir, casesDir, output string
	var failOnGap bool
	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Report ecctl capabilities (spec operations) with no E2E case",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := coverage.Analyze(specsDir, casesDir)
			if err != nil {
				return err
			}
			if output == "json" {
				b, _ := json.MarshalIndent(rep, "", "  ")
				fmt.Println(string(b))
			} else {
				fmt.Printf("coverage: %d/%d capabilities covered, %d gaps\n", rep.Covered, rep.Declared, len(rep.Gaps))
				for _, g := range rep.Gaps {
					fmt.Printf("  GAP  %s %s\n", g.Resource, g.Verb)
				}
			}
			if failOnGap && len(rep.Gaps) > 0 {
				return &exitError{code: exitTestsFailed, msg: fmt.Sprintf("%d uncovered capabilities", len(rep.Gaps))}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&specsDir, "specs", "../specs", "ecctl specs directory")
	f.StringVar(&casesDir, "cases", "cases", "E2E cases directory")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	f.BoolVar(&failOnGap, "fail-on-gap", false, "exit non-zero if any capability is uncovered")
	cmd.AddCommand(coverageRegistryCmd())
	return cmd
}

func coverageRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage the E2E coverage registry",
	}
	cmd.AddCommand(coverageRegistryInitCmd(), coverageRegistryCheckCmd(), coverageRegistrySummaryCmd())
	return cmd
}

func coverageRegistryInitCmd() *cobra.Command {
	var specsDir, casesDir, registryPath string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create or refresh e2e/coverage.yaml from specs and cases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var existing *coverage.Registry
			if _, err := os.Stat(registryPath); err == nil {
				reg, err := coverage.LoadRegistryFile(registryPath)
				if err != nil {
					return err
				}
				existing = reg
			}
			reg, err := coverage.InitRegistry(specsDir, casesDir, existing)
			if err != nil {
				return err
			}
			if err := coverage.WriteRegistryFile(registryPath, reg); err != nil {
				return err
			}
			sum := coverage.SummarizeRegistry(reg)
			fmt.Fprintf(cmd.OutOrStdout(), "registry: wrote %s (%d operations)\n", registryPath, sum.Entries)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&specsDir, "specs", "../specs", "ecctl specs directory")
	f.StringVar(&casesDir, "cases", "cases", "E2E cases directory")
	f.StringVar(&registryPath, "registry", "coverage.yaml", "coverage registry file")
	return cmd
}

func coverageRegistryCheckCmd() *cobra.Command {
	var specsDir, casesDir, registryPath, output, ecctlBin, capabilitiesPath, surface string
	var allowStale, failOnMissing, failOnStale, failOnNotLive bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate e2e/coverage.yaml against specs and cases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = failOnStale // stale entries are always invalid; the flag keeps the phase-8 command stable.
			filter, err := loadCapabilityFilter(capabilitiesPath, ecctlBin, surface)
			if err != nil {
				return err
			}
			rep, err := coverage.CheckRegistryFile(specsDir, casesDir, registryPath, coverage.RegistryCheckOptions{
				AllowStale:       allowStale,
				FailOnMissing:    failOnMissing,
				FailOnNotLive:    failOnNotLive,
				CapabilityFilter: filter,
			})
			if err != nil {
				return err
			}
			if output == "json" {
				b, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "registry: %d operations, %d live-pass, %d offline-valid, %d planned, %d missing, %d invalid\n",
					rep.Entries, rep.ByStatus[coverage.StatusLivePass], rep.ByStatus[coverage.StatusOfflineValid], rep.ByStatus[coverage.StatusPlanned], rep.ByStatus[coverage.StatusMissing], rep.Invalid)
				for _, e := range rep.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %s %s: %s: %s\n", e.Resource, e.Operation, e.Code, e.Message)
				}
			}
			if rep.Invalid > 0 {
				return &exitError{code: exitTestsFailed, msg: fmt.Sprintf("%d invalid registry entries", rep.Invalid)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&specsDir, "specs", "../specs", "ecctl specs directory")
	f.StringVar(&casesDir, "cases", "cases", "E2E cases directory")
	f.StringVar(&registryPath, "registry", "coverage.yaml", "coverage registry file")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	f.StringVar(&ecctlBin, "ecctl-bin", "", "ecctl binary whose capabilities define the selected surface")
	f.StringVar(&capabilitiesPath, "capabilities", "", "capabilities JSON file (overrides --ecctl-bin)")
	f.StringVar(&surface, "surface", string(scenario.SurfacePublic), "capability surface: public|full")
	f.BoolVar(&allowStale, "allow-stale", false, "allow stale missing/covered status during manual transitions")
	f.BoolVar(&failOnMissing, "fail-on-missing", false, "exit non-zero when operations are still marked missing")
	f.BoolVar(&failOnNotLive, "fail-on-not-live", false, "exit non-zero when selected operations are not live-pass")
	f.BoolVar(&failOnStale, "fail-on-stale", false, "kept for explicit completion gates; stale entries are always invalid")
	return cmd
}

func coverageRegistrySummaryCmd() *cobra.Command {
	var registryPath, output, capabilitiesPath, ecctlBin, surface string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Summarize e2e/coverage.yaml status counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := coverage.LoadRegistryFile(registryPath)
			if err != nil {
				return err
			}
			filter, err := loadCapabilityFilter(capabilitiesPath, ecctlBin, surface)
			if err != nil {
				return err
			}
			sum := coverage.SummarizeRegistry(reg, filter)
			if output == "json" {
				b, err := json.MarshalIndent(sum, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "registry: %d operations, %d live-pass, %d offline-valid, %d planned, %d missing\n",
					sum.Entries, sum.ByStatus[coverage.StatusLivePass], sum.ByStatus[coverage.StatusOfflineValid], sum.ByStatus[coverage.StatusPlanned], sum.ByStatus[coverage.StatusMissing])
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&registryPath, "registry", "coverage.yaml", "coverage registry file")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	f.StringVar(&capabilitiesPath, "capabilities", "", "capabilities JSON file used to filter the summary")
	f.StringVar(&ecctlBin, "ecctl-bin", "", "ecctl binary whose capabilities define the selected surface")
	f.StringVar(&surface, "surface", string(scenario.SurfacePublic), "capability surface: public|full")
	return cmd
}

func loadCapabilityFilter(path, bin, wantSurface string) (map[coverage.Capability]bool, error) {
	if path == "" && bin == "" {
		return nil, nil
	}
	var caps surfacepkg.Capabilities
	var err error
	if path != "" {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		caps, err = surfacepkg.Decode(data)
	} else {
		caps, err = surfacepkg.LoadFromBinary(context.Background(), bin)
	}
	if err != nil {
		return nil, err
	}
	if wantSurface != "" && caps.Surface != wantSurface {
		return nil, fmt.Errorf("capabilities surface %q does not match requested %q", caps.Surface, wantSurface)
	}
	filter := map[coverage.Capability]bool{}
	for _, product := range caps.Products {
		for _, resource := range product.Resources {
			for _, action := range resource.Actions {
				filter[coverage.Capability{Resource: product.Name + "/" + resource.Name, Verb: action}] = true
			}
		}
	}
	return filter, nil
}

func coveragePathForCases(casesDir string) string {
	candidate := filepath.Join(filepath.Dir(filepath.Clean(casesDir)), "coverage.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func optionalDefaultPath(cmd *cobra.Command, flag, path string) string {
	if path == "" || cmd.Flags().Changed(flag) {
		return path
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ""
	}
	return path
}

func validateRunContracts(casesDir, stackFile, inputsDir, coveragePath, globalFixture, parameterPolicy string, stackExplicit, globalExplicit, policyExplicit bool) error {
	if stackFile != "" {
		if _, err := os.Stat(stackFile); err != nil {
			if os.IsNotExist(err) && stackExplicit {
				return fmt.Errorf("validate E2E contracts: stack file %q does not exist", stackFile)
			}
			if !os.IsNotExist(err) {
				return err
			}
			stackFile = ""
		}
	}
	if globalFixture != "" {
		if _, err := os.Stat(globalFixture); err != nil {
			if os.IsNotExist(err) && globalExplicit {
				return fmt.Errorf("validate E2E contracts: global fixture %q does not exist", globalFixture)
			}
			if !os.IsNotExist(err) {
				return err
			}
			globalFixture = ""
		}
	}
	if parameterPolicy != "" {
		if _, err := os.Stat(parameterPolicy); err != nil {
			if os.IsNotExist(err) && policyExplicit {
				return fmt.Errorf("validate E2E contracts: parameter policy %q does not exist", parameterPolicy)
			}
			if !os.IsNotExist(err) {
				return err
			}
			parameterPolicy = ""
		}
	}
	rep, err := caselint.Check(caselint.Options{
		CasesDir: casesDir, InputsDir: inputsDir, CoveragePath: coveragePath,
		StackFile: stackFile, GlobalFixture: globalFixture,
		ParameterPolicy: parameterPolicy,
	})
	if err != nil {
		return fmt.Errorf("validate E2E contracts: %w", err)
	}
	if rep.Invalid > 0 {
		first := rep.Errors[0]
		return fmt.Errorf("validate E2E contracts: %d lint errors (%s: %s)", rep.Invalid, first.Code, first.Message)
	}
	return nil
}

func lintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Run offline deterministic checks for E2E artifacts",
	}
	cmd.AddCommand(lintCasesCmd())
	return cmd
}

func lintCasesCmd() *cobra.Command {
	var configFile, casesDir, inputsDir, coveragePath, stackFile, parameterPolicy, output string
	cmd := &cobra.Command{
		Use:   "cases",
		Short: "Validate E2E case files without cloud credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runConfig, configPresent, err := loadRunConfigFile(configFile, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}
			globalFixture := ""
			if configPresent {
				if err := applyRunConfigPaths(cmd, runConfig, configFile); err != nil {
					return err
				}
				globalFixture = configFile
			}
			if stackFile != "" {
				if _, err := os.Stat(stackFile); err != nil && os.IsNotExist(err) && !cmd.Flags().Changed("stack") {
					stackFile = ""
				}
			}
			rep, err := caselint.Check(caselint.Options{
				CasesDir:        casesDir,
				InputsDir:       inputsDir,
				CoveragePath:    coveragePath,
				StackFile:       stackFile,
				GlobalFixture:   globalFixture,
				ParameterPolicy: parameterPolicy,
			})
			if err != nil {
				return err
			}
			if output == "json" {
				b, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "lint: %d cases, %d steps, %d invalid\n",
					rep.Cases, rep.Steps, rep.Invalid)
				for _, e := range rep.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %s %s: %s: %s\n", e.Path, e.Step, e.Code, e.Message)
				}
			}
			if rep.Invalid > 0 {
				return &exitError{code: exitTestsFailed, msg: fmt.Sprintf("%d lint errors", rep.Invalid)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&configFile, "config", "c", "e2e.yaml", "top-level E2E config (CLI paths still win)")
	f.StringVar(&casesDir, "cases", "cases", "directory of case YAML files")
	f.StringVar(&inputsDir, "inputs", "fixtures/inputs", "per-resource inputs directory")
	f.StringVar(&coveragePath, "coverage", "coverage.yaml", "coverage registry file")
	f.StringVar(&stackFile, "stack", "fixtures/stack.yaml", "shared fixture stack file")
	f.StringVar(&parameterPolicy, "parameter-policy", "fixtures/parameter-policy.yaml", "bounded dynamic parameter policy")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	return cmd
}

func reportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Validate E2E report artifacts",
	}
	cmd.AddCommand(reportCheckCmd())
	return cmd
}

func reportCheckCmd() *cobra.Command {
	var failed int
	var output string
	cmd := &cobra.Command{
		Use:   "check REPORT",
		Short: "Validate an e2e-report.json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := report.CheckFile(args[0], report.CheckOptions{Failed: failed})
			if err != nil {
				return err
			}
			if output == "json" {
				b, err := json.MarshalIndent(rep, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "report: %d cases, %d failed, %d invalid\n",
					rep.Cases, rep.Failed, rep.Invalid)
				for _, e := range rep.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "error: %s: %s: %s\n", e.Case, e.Code, e.Message)
				}
			}
			if rep.Invalid > 0 {
				return &exitError{code: exitTestsFailed, msg: fmt.Sprintf("%d report check errors", rep.Invalid)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&failed, "failed", 0, "maximum allowed failed cases")
	f.StringVarP(&output, "output", "o", "text", "output format: text|json")
	return cmd
}

func defaultLabel() string {
	if id := os.Getenv("GITHUB_RUN_ID"); id != "" {
		return id
	}
	return time.Now().Format("20060102-150405")
}
