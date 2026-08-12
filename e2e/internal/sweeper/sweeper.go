// Package sweeper deletes leftover tagged resources — the third cleanup layer.
// It is config-driven: each sweepable kind declares a list command
// (filtered by the e2e tag) and a delete command template. Orphans are selected
// by TTL or by "owning GitHub run already finished".
package sweeper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/jsonq"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

// defaultConcurrency bounds parallel deletes within a single kind when the
// caller does not set Options.Concurrency.
const defaultConcurrency = 20

// Transient delete-failure retry: a freshly emptied network resource can report
// DependencyViolation until its ENIs finish releasing (seconds to ~a minute).
const (
	sweepDeleteRetries         = 4
	sweepRetryInterval         = 15 * time.Second
	replayJournalCommitTimeout = 30 * time.Second
)

// sweepRetrySleep is a package var so tests can stub out the backoff.
var sweepRetrySleep = time.Sleep

// replayJournalAfterRead is a test synchronization point for deterministic
// writer/replayer interleavings. Production keeps the no-op default.
var replayJournalAfterRead = func(string) {}

// replayJournalAfterSuccess is a test synchronization point for the narrow
// window between a successful external command and its journal commit.
var replayJournalAfterSuccess = func(string) {}

// isTransientSweepFailure reports whether a failed delete is worth retrying:
// dependency-still-releasing or throttling. Both clear on their own shortly.
func isTransientSweepFailure(dr execpkg.Result) bool {
	reason := strings.ToLower(failureReason(dr))
	return strings.Contains(reason, "dependency") || strings.Contains(reason, "throttl")
}

// Config is sweep.yaml.
type Config struct {
	Kinds        []Kind             `yaml:"kinds"`
	NonSweepable []NonSweepableKind `yaml:"non_sweepable"`
}

// Kind is one sweepable resource type.
type Kind struct {
	Name         string            `yaml:"name"`
	Resource     string            `yaml:"resource"`      // optional checker metadata: product/resource
	List         string            `yaml:"list"`          // full ecctl list command (tag-filtered)
	ItemsPath    string            `yaml:"items_path"`    // jsonpath to the array of items
	IDField      string            `yaml:"id_field"`      // relative path within an item
	RunIDField   string            `yaml:"runid_field"`   // optional
	CreatedField string            `yaml:"created_field"` // optional; RFC3339 or Aliyun minute-precision
	DeleteFields map[string]string `yaml:"delete_fields"` // template field -> relative item path
	DependsOn    []string          `yaml:"depends_on"`    // providers that must be deleted after this kind
	Delete       string            `yaml:"delete"`        // template with {{.id}} and explicit delete_fields
}

// NonSweepableKind documents a live-created resource that has an explicit,
// reviewable reason why `ecctl-e2e sweep` cannot reclaim it automatically.
type NonSweepableKind struct {
	Resource    string `yaml:"resource"`
	Reason      string `yaml:"reason"`
	ReviewAfter string `yaml:"review_after"`
}

// Options configures a sweep.
type Options struct {
	ConfigFile    string
	Region        string
	EcctlBin      string
	TTL           time.Duration
	ByFinishedRun bool
	CheckFinished RunChecker // required when ByFinishedRun
	DryRun        bool
	Concurrency   int // parallel deletes within a kind; <1 means defaultConcurrency
	Env           []string
	Logf          func(string, ...any)
}

// RunChecker reports whether a GitHub run id has finished.
type RunChecker func(runID string) (bool, error)

// Result tallies a sweep.
type Result struct {
	Deleted int      `json:"deleted"`
	Skipped int      `json:"skipped"`
	Errors  int      `json:"errors"`
	Details []string `json:"details"`
}

// ReplayOptions identifies the run that may be replayed. Empty values use the
// metadata recorded in the journal; conflicting values are rejected.
type ReplayOptions struct {
	Config  execpkg.Config
	RunID   string
	Surface string
}

// ReplayJournal replays registered teardown commands in reverse creation order.
// It is the crash-recovery counterpart to the tag-based sweeper: journals can
// reclaim resources even when their APIs do not support tag-filtered listing.
func ReplayJournal(ctx context.Context, path string, cfg execpkg.Config) (*Result, error) {
	return ReplayJournalWithOptions(ctx, path, ReplayOptions{Config: cfg})
}

// ReplayJournalWithOptions replays a journal after validating its run identity.
// Raw []Resource journals from older runners remain readable, but new journals
// always carry metadata and are never replayed against conflicting options.
func ReplayJournalWithOptions(ctx context.Context, path string, opt ReplayOptions) (*Result, error) {
	var result *Result
	err := journalfile.WithReplayLock(ctx, path, func() error {
		var journal report.CleanupJournal
		err := journalfile.WithLock(ctx, path, func() error {
			var err error
			journal, _, err = readReplayJournal(path)
			if err != nil {
				return err
			}
			return validateJournalIdentity(journal, &opt)
		})
		if err != nil {
			return err
		}
		replayJournalAfterRead(path)
		result, err = replayJournalEntries(ctx, path, opt, journal.Entries)
		return err
	})
	return result, err
}

func readReplayJournal(path string) (report.CleanupJournal, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return report.CleanupJournal{}, false, err
	}
	var journal report.CleanupJournal
	legacy := strings.HasPrefix(strings.TrimSpace(string(data)), "[")
	if legacy {
		if err := json.Unmarshal(data, &journal.Entries); err != nil {
			return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
		}
	} else if err := json.Unmarshal(data, &journal); err != nil {
		return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
	}
	return journal, legacy, nil
}

func replayJournalEntries(ctx context.Context, path string, opt ReplayOptions, entries []report.Resource) (*Result, error) {
	result := &Result{}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if strings.TrimSpace(entry.Teardown) == "" {
			continue
		}
		if err := validateJournalTeardown(entry.Teardown); err != nil {
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: invalid teardown: %v", entry.Scope, err))
			continue
		}
		res := execpkg.Run(ctx, opt.Config, entry.Teardown)
		if res.Err != nil || (res.Exit != 0 && res.Exit != 4) {
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: delete exit=%d %s", entry.Scope, res.Exit, failureReason(res)))
			continue
		}
		result.Deleted++
		replayJournalAfterSuccess(path)
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replayJournalCommitTimeout)
		err := consumeReplayJournalEntry(commitCtx, path, opt, entry)
		cancel()
		if err != nil {
			return result, fmt.Errorf("rewrite cleanup journal %s after replay: %w", path, err)
		}
	}
	return result, nil
}

func consumeReplayJournalEntry(ctx context.Context, path string, opt ReplayOptions, completed report.Resource) error {
	return journalfile.WithLock(ctx, path, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if err := validateJournalIdentity(journal, &currentOpt); err != nil {
			return err
		}
		index := -1
		for i := len(journal.Entries) - 1; i >= 0; i-- {
			if journal.Entries[i] == completed {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		pending := append([]report.Resource(nil), journal.Entries...)
		pending = append(pending[:index], pending[index+1:]...)
		return rewriteReplayJournal(path, journal, pending, legacy)
	})
}

func rewriteReplayJournal(path string, journal report.CleanupJournal, pending []report.Resource, legacy bool) error {
	var value any
	if legacy {
		value = pending
	} else {
		journal.Entries = pending
		value = journal
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func validateJournalIdentity(journal report.CleanupJournal, opt *ReplayOptions) error {
	if opt.RunID != "" && journal.RunID != "" && opt.RunID != journal.RunID {
		return fmt.Errorf("cleanup journal run id %q does not match %q", journal.RunID, opt.RunID)
	}
	if opt.Surface != "" && journal.Surface != "" && opt.Surface != journal.Surface {
		return fmt.Errorf("cleanup journal surface %q does not match %q", journal.Surface, opt.Surface)
	}
	if opt.Config.Region != "" && journal.Region != "" && opt.Config.Region != journal.Region {
		return fmt.Errorf("cleanup journal region %q does not match %q", journal.Region, opt.Config.Region)
	}
	if opt.Config.Bin != "" && journal.EcctlBin != "" && opt.Config.Bin != journal.EcctlBin {
		return fmt.Errorf("cleanup journal binary %q does not match %q", journal.EcctlBin, opt.Config.Bin)
	}
	if opt.Config.Region == "" {
		opt.Config.Region = journal.Region
	}
	if opt.Config.Bin == "" {
		opt.Config.Bin = journal.EcctlBin
	}
	if opt.Config.Bin == "" {
		opt.Config.Bin = "ecctl"
	}
	return nil
}

func validateJournalTeardown(command string) error {
	return journalcmd.ValidateDelete(command)
}

// Sweep lists every configured kind and deletes the orphans.
func Sweep(ctx context.Context, opt Options) (*Result, error) {
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	cfg, err := loadConfig(opt.ConfigFile)
	if err != nil {
		return nil, err
	}
	if opt.ByFinishedRun && opt.CheckFinished == nil {
		return nil, fmt.Errorf("by-finished-run requires a run checker (GH_TOKEN + repo)")
	}
	concurrency := opt.Concurrency
	if concurrency < 1 {
		concurrency = defaultConcurrency
	}
	execCfg := execpkg.Config{Bin: opt.EcctlBin, Region: opt.Region, Env: opt.Env}
	res := &Result{}
	var mu sync.Mutex

	// Kinds are processed in config order so dependency ordering holds (e.g.
	// instances before the vswitches/vpcs they sit in); deletes *within* a kind
	// are independent, so they run concurrently with a per-kind barrier.
	for _, k := range cfg.Kinds {
		opt.Logf("scanning %s ...", k.Name)
		listRes := execpkg.Run(ctx, execCfg, k.List)
		if listRes.Exit != 0 || listRes.Err != nil {
			res.Errors++
			res.Details = append(res.Details, fmt.Sprintf("%s: list failed: exit=%d %s", k.Name, listRes.Exit, failureReason(listRes)))
			opt.Logf("%s: list failed (%s)", k.Name, failureReason(listRes))
			continue
		}
		items, _ := jsonq.Get(listRes.JSON, "$", k.ItemsPath)
		arr, _ := items.([]any)

		// Selection is sequential and cheap; collect the orphans to delete.
		var tasks []deleteTask
		for _, item := range arr {
			id, ok := stringAt(item, k.IDField)
			if !ok {
				continue
			}
			del, reason := shouldDelete(opt, item, k)
			if !del {
				res.Skipped++
				opt.Logf("%s %s: keep (%s)", k.Name, id, reason)
				continue
			}
			cmd, err := renderDeleteCommand(k, item, id)
			if err != nil {
				res.Errors++
				res.Details = append(res.Details, fmt.Sprintf("%s %s: render delete command failed: %v", k.Name, id, err))
				continue
			}
			tasks = append(tasks, deleteTask{kind: k.Name, id: id, reason: reason, cmd: cmd})
		}

		opt.Logf("%s: found %d, deleting %d", k.Name, len(arr), len(tasks))

		if opt.DryRun {
			for _, t := range tasks {
				opt.Logf("%s %s: would delete (%s): %s", t.kind, t.id, t.reason, t.cmd)
				res.Deleted++
			}
			continue
		}

		runDeletes(ctx, tasks, concurrency, res, &mu, opt.Logf, func(ctx context.Context, t deleteTask) execpkg.Result {
			return execpkg.Run(ctx, execCfg, t.cmd)
		})
	}
	return res, nil
}

func renderDeleteCommand(kind Kind, item any, id string) (string, error) {
	data := map[string]any{"id": id}
	for name, path := range kind.DeleteFields {
		value, ok := stringAt(item, path)
		if !ok {
			return "", fmt.Errorf("delete field %q path %q is missing or not a string", name, path)
		}
		data[name] = value
	}
	return vars.Render(kind.Delete, data)
}

// deleteTask is one orphan selected for deletion.
type deleteTask struct {
	kind   string
	id     string
	reason string
	cmd    string
}

// runDeletes executes the given deletes with at most `concurrency` in flight,
// updating res under mu, and blocks until all have finished (a barrier, so the
// caller can safely move on to a dependent kind). run performs a single delete;
// it is a parameter so tests can inject one without spawning processes.
func runDeletes(ctx context.Context, tasks []deleteTask, concurrency int, res *Result, mu *sync.Mutex, logf func(string, ...any), run func(context.Context, deleteTask) execpkg.Result) {
	if len(tasks) == 0 {
		return
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t deleteTask) {
			defer wg.Done()
			defer func() { <-sem }()
			dr := run(ctx, t)
			// A just-emptied network resource (vpc/vswitch/sg) often reports
			// DependencyViolation for a short while because its ENIs are still
			// being released; throttling is likewise transient. Retry both with a
			// linear backoff so a single sweep is self-sufficient.
			for attempt := 1; attempt < sweepDeleteRetries &&
				(dr.Exit != 0 || dr.Err != nil) && isTransientSweepFailure(dr) && ctx.Err() == nil; attempt++ {
				sweepRetrySleep(time.Duration(attempt) * sweepRetryInterval)
				dr = run(ctx, t)
			}
			mu.Lock()
			defer mu.Unlock()
			if dr.Exit != 0 || dr.Err != nil {
				res.Errors++
				res.Details = append(res.Details, fmt.Sprintf("%s %s: delete exit=%d %s", t.kind, t.id, dr.Exit, failureReason(dr)))
				return
			}
			res.Deleted++
			logf("%s %s: deleted (%s)", t.kind, t.id, t.reason)
		}(t)
	}
	wg.Wait()
}

// shouldDelete decides whether one item is an orphan to remove.
func shouldDelete(opt Options, item any, k Kind) (bool, string) {
	if opt.ByFinishedRun {
		runID, ok := stringAt(item, k.RunIDField)
		if !ok {
			return false, "no run-id tag"
		}
		finished, err := opt.CheckFinished(runID)
		if err != nil {
			return false, "run check error: " + err.Error()
		}
		if finished {
			return true, "run " + runID + " finished"
		}
		return false, "run " + runID + " active"
	}
	// TTL mode
	created, ok := stringAt(item, k.CreatedField)
	if !ok {
		return false, fmt.Sprintf("no %s field (skipped in ttl mode)", k.CreatedField)
	}
	t, err := parseTimestamp(created)
	if err != nil {
		return false, fmt.Sprintf("unparseable %s %q", k.CreatedField, created)
	}
	if time.Since(t) > opt.TTL {
		return true, fmt.Sprintf("age %s > ttl", time.Since(t).Round(time.Minute))
	}
	return false, "within ttl"
}

// parseTimestamp accepts the timestamp formats Alibaba Cloud returns. Most APIs
// emit UTC minute-precision (e.g. "2026-06-29T06:04Z"), which is not valid
// RFC3339, so several layouts are tried before giving up.
func parseTimestamp(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,             // 2026-06-29T06:04:05Z07:00
		"2006-01-02T15:04Z07:00", // 2026-06-29T06:04Z (Aliyun default)
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04Z",
		"2006-01-02 15:04:05",
	}
	var err error
	for _, layout := range layouts {
		var t time.Time
		if t, err = time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// failureReason extracts a concise, single-line reason from a failed ecctl
// invocation (list or delete) so the sweep summary can explain why, rather than
// just reporting an exit code. ecctl prints its structured error to stdout (as
// {"error": {...}}), so that is the primary signal; it falls back to raw
// stdout, then stderr, then the process-level error (which is nil for a clean
// non-zero exit).
func failureReason(r execpkg.Result) string {
	if msg := ecctlErrorMessage(r.JSON); msg != "" {
		return msg
	}
	if msg := flattenLine(r.Stdout); msg != "" {
		return msg
	}
	if msg := flattenLine(r.Stderr); msg != "" {
		return msg
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return "(no output)"
}

// ecctlErrorMessage formats the {"error": {...}} payload ecctl emits on failure
// into a "code: message (suggestion)" line. Returns "" when the parsed JSON
// is not an ecctl error object.
func ecctlErrorMessage(v any) string {
	root, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errObj["code"].(string)
	message, _ := errObj["message"].(string)
	out := strings.TrimSpace(strings.TrimPrefix(code+": "+message, ": "))
	if action, _ := errObj["suggested_action"].(string); action != "" {
		out += " (" + action + ")"
	}
	return flattenLine(out)
}

// flattenLine collapses multi-line, whitespace-heavy output into a single
// trimmed line, truncating to keep summaries readable.
func flattenLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	const max = 300
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func stringAt(item any, field string) (string, bool) {
	if field == "" {
		return "", false
	}
	v, ok := jsonq.Get(item, "$", field)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(s), s != ""
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Kinds) == 0 {
		return nil, fmt.Errorf("%s: no kinds defined", path)
	}
	return &c, nil
}
