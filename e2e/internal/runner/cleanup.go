package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

const cleanupTimeoutPadding = time.Minute

var (
	cleanupTimeout     = 10 * time.Minute
	cleanupRetryDelays = []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second, 30 * time.Second}
)

// exitNotFound is ecctl's process exit code for a NotFound error (see
// ecerrors.AppError.ExitCode). During teardown it means the resource is already
// gone, which is success for cleanup purposes.
const exitNotFound = 4

// cleanupItem is one teardown command and whether it has already run.
type cleanupItem struct {
	scope    string
	cmd      string
	role     string
	lockKeys []string
	done     bool
}

// cleanup is the two-level teardown registry. Case scopes run when
// their case ends; the stack scope runs after all cases join. Teardown always
// executes on a fresh background context so it completes even when the run's
// context was cancelled (CI cancel / timeout).
type cleanup struct {
	mu          sync.Mutex
	keep        bool
	journal     string
	execCfg     map[string]execpkg.Config
	operations  *operationRuntime
	logf        func(string, ...any)
	manifest    []report.Resource
	journalMeta report.CleanupJournal
}

func newCleanup(cfg map[string]execpkg.Config, operations *operationRuntime, keep bool, journal string, meta report.CleanupJournal, logf func(string, ...any)) *cleanup {
	meta.Version = 2
	return &cleanup{keep: keep, journal: journal, execCfg: cfg, operations: operations, logf: logf, journalMeta: meta}
}

// push registers a teardown command in a scope and records it in the manifest.
func (c *cleanup) push(scope *[]*cleanupItem, name, cmd, role string, lockKeys []string) error {
	if role == "" {
		role = "primary"
	}
	cfg, ok := c.execCfg[role]
	if !ok {
		return fmt.Errorf("cleanup region role %q is not configured", role)
	}
	keys, err := normalizeLockKeys(lockKeys)
	if err != nil {
		return fmt.Errorf("cleanup lock keys: %w", err)
	}
	it := &cleanupItem{scope: name, cmd: cmd, role: role, lockKeys: keys}
	c.mu.Lock()
	*scope = append(*scope, it)
	c.manifest = append(c.manifest, report.Resource{
		Scope: name, Teardown: cmd, RegionRole: role, Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID,
	})
	err = c.appendJournalItemLocked(it)
	c.mu.Unlock()
	return err
}

// satisfy marks a previously registered finalizer complete when a later case
// step performs the exact same cleanup successfully. This preserves explicit
// delete/revoke/detach coverage without running the operation twice at scope
// teardown.
func (c *cleanup) satisfy(scope []*cleanupItem, command, role string) error {
	if role == "" {
		role = "primary"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(scope) - 1; i >= 0; i-- {
		item := scope[i]
		if !item.done && item.role == role && commandsEquivalent(item.cmd, command) {
			item.done = true
			return c.removeJournalItemLocked(item)
		}
	}
	return nil
}

func commandsEquivalent(left, right string) bool {
	leftTokens, leftErr := shlex.Split(left)
	rightTokens, rightErr := shlex.Split(right)
	if leftErr != nil || rightErr != nil || len(leftTokens) != len(rightTokens) {
		return false
	}
	for i := range leftTokens {
		if leftTokens[i] != rightTokens[i] {
			return false
		}
	}
	return true
}

func (c *cleanup) appendJournalItemLocked(item *cleanupItem) error {
	if c.journal == "" {
		return nil
	}
	template, entry, path := c.journalValues(item)
	return journalfile.WithLock(context.Background(), path, func() error {
		journal, _, err := readRunnerJournal(path)
		if os.IsNotExist(err) {
			journal = template
		} else if err != nil {
			return err
		}
		if err := mergeJournalMetadata(&journal, template); err != nil {
			return err
		}
		if !isReplayableTeardown(item.cmd) {
			return writeRunnerJournal(path, journal)
		}
		journal.Entries = append(journal.Entries, entry)
		return writeRunnerJournal(path, journal)
	})
}

func (c *cleanup) removeJournalItemLocked(item *cleanupItem) error {
	if c.journal == "" || !isReplayableTeardown(item.cmd) {
		return nil
	}
	template, entry, path := c.journalValues(item)
	return journalfile.WithLock(context.Background(), path, func() error {
		journal, _, err := readRunnerJournal(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := mergeJournalMetadata(&journal, template); err != nil {
			return err
		}
		index := -1
		for i := len(journal.Entries) - 1; i >= 0; i-- {
			if journalEntriesMatch(journal.Entries[i], entry) {
				index = i
				break
			}
		}
		if index < 0 {
			return nil
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		return writeRunnerJournal(path, journal)
	})
}

func (c *cleanup) journalValues(item *cleanupItem) (report.CleanupJournal, report.Resource, string) {
	cfg := c.execCfg[item.role]
	journal := c.journalMeta
	journal.RegionRole = item.role
	journal.Region = cfg.Region
	journal.EcctlBin = cfg.Bin
	journal.Entries = make([]report.Resource, 0)
	entry := report.Resource{
		Scope: item.scope, Teardown: item.cmd, RegionRole: item.role,
		Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID,
	}
	return journal, entry, cleanupJournalPath(c.journal, item.role)
}

func readRunnerJournal(path string) (report.CleanupJournal, bool, error) {
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

func mergeJournalMetadata(journal *report.CleanupJournal, desired report.CleanupJournal) error {
	for _, field := range []struct {
		name    string
		current *string
		desired string
	}{
		{name: "run id", current: &journal.RunID, desired: desired.RunID},
		{name: "execution id", current: &journal.ExecutionID, desired: desired.ExecutionID},
		{name: "region role", current: &journal.RegionRole, desired: desired.RegionRole},
		{name: "region", current: &journal.Region, desired: desired.Region},
		{name: "surface", current: &journal.Surface, desired: desired.Surface},
		{name: "binary", current: &journal.EcctlBin, desired: desired.EcctlBin},
	} {
		if *field.current != "" && field.desired != "" && *field.current != field.desired {
			return fmt.Errorf("cleanup journal %s %q does not match %q", field.name, *field.current, field.desired)
		}
		if *field.current == "" {
			*field.current = field.desired
		}
	}
	if journal.Version == 0 {
		journal.Version = desired.Version
	}
	return nil
}

func journalEntriesMatch(left, right report.Resource) bool {
	if left.Scope != right.Scope || left.Teardown != right.Teardown {
		return false
	}
	for _, values := range [][2]string{
		{left.RegionRole, right.RegionRole},
		{left.Region, right.Region},
		{left.ExecutionID, right.ExecutionID},
	} {
		if values[0] != "" && values[1] != "" && values[0] != values[1] {
			return false
		}
	}
	return true
}

func writeRunnerJournal(path string, journal report.CleanupJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// isReplayableTeardown keeps crash-recovery journals within the repository's
// strict safety contract. Runtime finalizers such as revoke or detach still run
// in-process, but they are never persisted for later unattended replay.
func isReplayableTeardown(command string) bool {
	return journalcmd.ValidateDelete(command) == nil
}

func cleanupJournalPath(base, role string) string {
	if role == "" || role == "primary" {
		return base
	}
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + role + ext
}

// run tears down a scope in LIFO order, tolerating already-deleted errors. It
// returns concrete failures so a successful test step cannot hide a leaked
// resource from the final report.
func (c *cleanup) run(scope []*cleanupItem) []string {
	var failures []string
	if c.keep {
		return nil
	}
	for i := len(scope) - 1; i >= 0; i-- {
		it := scope[i]
		c.mu.Lock()
		if it.done {
			c.mu.Unlock()
			continue
		}
		it.done = true
		c.mu.Unlock()

		var res execpkg.Result
		var itemTimeout time.Duration
		var timedOut bool
		coordinationErr := c.operations.executeKeys(context.Background(), it.lockKeys, func() {
			itemTimeout = cleanupCommandTimeout(it.cmd)
			commandCtx, cancel := context.WithTimeout(context.Background(), itemTimeout)
			res = execpkg.Run(commandCtx, c.execCfg[it.role], it.cmd)
			for attempt, delay := range cleanupRetryDelays {
				if res.Exit == 0 || res.Exit == exitNotFound || !cleanupRetryable(res) {
					break
				}
				c.logf("cleanup retry %d/%d after transient resource status: %s", attempt+1, len(cleanupRetryDelays), it.cmd)
				timer := time.NewTimer(delay)
				select {
				case <-commandCtx.Done():
					timer.Stop()
				case <-timer.C:
					res = execpkg.Run(commandCtx, c.execCfg[it.role], it.cmd)
				}
				if commandCtx.Err() != nil {
					break
				}
			}
			timedOut = commandCtx.Err() == context.DeadlineExceeded
			cancel()
		})
		if coordinationErr != nil {
			failure := fmt.Sprintf("cleanup coordination: %s: %v", report.Scrub(it.cmd), coordinationErr)
			c.logf("cleanup failed: %s", failure)
			failures = append(failures, failure)
			continue
		}
		switch {
		case res.Exit == 0:
			c.logf("cleanup ok: %s", it.cmd)
			if err := c.completeJournalItem(it); err != nil {
				failure := fmt.Sprintf("cleanup journal rewrite: %s: %v", report.Scrub(it.cmd), err)
				c.logf("cleanup failed: %s", failure)
				failures = append(failures, failure)
			}
		case res.Exit == exitNotFound:
			// The resource is already gone — typically the case's own delete step
			// removed it and this teardown is the safety net. That is the desired
			// end state, not a failure worth flagging.
			c.logf("cleanup ok (already gone): %s", it.cmd)
			if err := c.completeJournalItem(it); err != nil {
				failure := fmt.Sprintf("cleanup journal rewrite: %s: %v", report.Scrub(it.cmd), err)
				c.logf("cleanup failed: %s", failure)
				failures = append(failures, failure)
			}
		default:
			failure := cleanupFailure(res, it.cmd, timedOut, itemTimeout)
			c.logf("cleanup failed: %s", failure)
			failures = append(failures, failure)
		}
	}
	return failures
}

func (c *cleanup) completeJournalItem(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.removeJournalItemLocked(item)
}

// cleanupCommandTimeout keeps the cleanup process alive long enough for an
// ecctl waiter requested by the teardown command to return. The extra minute
// avoids racing the child command's own deadline while preserving the existing
// bounded default for commands without an explicit timeout.
func cleanupCommandTimeout(command string) time.Duration {
	tokens, err := shlex.Split(command)
	if err != nil {
		return cleanupTimeout
	}
	for i, token := range tokens {
		value := ""
		switch {
		case token == "--timeout" && i+1 < len(tokens):
			value = tokens[i+1]
		case strings.HasPrefix(token, "--timeout="):
			value = strings.TrimPrefix(token, "--timeout=")
		}
		if value == "" {
			continue
		}
		waitTimeout, err := time.ParseDuration(value)
		if err == nil && waitTimeout >= cleanupTimeout {
			return waitTimeout + cleanupTimeoutPadding
		}
		return cleanupTimeout
	}
	return cleanupTimeout
}

func cleanupRetryable(res execpkg.Result) bool {
	if res.Exit == 0 || res.Exit == exitNotFound {
		return false
	}
	text := strings.ToLower(strings.Join([]string{res.Stdout, res.Stderr, firstActionCode(res.JSON)}, " "))
	for _, marker := range []string{
		"status does not support this operation",
		"cannot operate cluster where state is updating",
		"incorrectinstancestatus",
		"incorrectdiskstatus",
		"operationconflict",
		"taskconflict",
		// ModifyInstanceChargeType is async: a DeleteInstance issued right after
		// the conversion request fails while the charge-type token is processing.
		"the last token request is processing",
		"there is still instance(s) in the specified security group",
		"depends on [networkinterface]",
		"dependencyviolation",
		"dependent resources",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func cleanupFailure(res execpkg.Result, command string, timedOut bool, timeout time.Duration) string {
	parts := []string{fmt.Sprintf("cleanup exit=%d: %s", res.Exit, report.Scrub(command))}
	if timedOut {
		parts = append(parts, "timeout="+timeout.String())
	}
	jsonValue := res.JSON
	if jsonValue == nil && strings.TrimSpace(res.Stdout) != "" {
		decoder := json.NewDecoder(strings.NewReader(res.Stdout))
		decoder.UseNumber()
		_ = decoder.Decode(&jsonValue)
	}
	cloudCode, actionCode := cleanupErrorCodes(jsonValue)
	if cloudCode != "" {
		parts = append(parts, "cloud_code="+report.Scrub(cloudCode))
	}
	if actionCode != "" {
		parts = append(parts, "action_code="+report.Scrub(actionCode))
	}
	if res.Err != nil {
		parts = append(parts, "process_error="+report.Scrub(res.Err.Error()))
	}
	if stdout := strings.TrimSpace(res.Stdout); stdout != "" {
		parts = append(parts, "stdout="+report.Scrub(stdout))
	}
	if stderr := strings.TrimSpace(res.Stderr); stderr != "" {
		parts = append(parts, "stderr="+report.Scrub(stderr))
	}
	return strings.Join(parts, "; ")
}

func cleanupErrorCodes(value any) (string, string) {
	root, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	cloudCode := stringField(root, "code")
	if errorValue, ok := root["error"].(map[string]any); ok {
		if code := stringField(errorValue, "code"); code != "" {
			cloudCode = code
		}
	}
	return cloudCode, firstActionCode(root["actions"])
}

func firstActionCode(value any) string {
	switch current := value.(type) {
	case map[string]any:
		if code := stringField(current, "code"); code != "" {
			return code
		}
		for _, child := range current {
			if code := firstActionCode(child); code != "" {
				return code
			}
		}
	case []any:
		for _, child := range current {
			if code := firstActionCode(child); code != "" {
				return code
			}
		}
	}
	return ""
}

func stringField(value map[string]any, key string) string {
	for currentKey, child := range value {
		if strings.EqualFold(currentKey, key) {
			if text, ok := child.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}
