package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

const cleanupTimeoutPadding = time.Minute

var deleteClaimRejected = func(string) {}
var writeTeamRegistrationJournal = writeRunnerJournal

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
	scope                     string
	cmd                       string
	role                      string
	lockKeys                  []string
	done                      bool
	running                   bool
	deferred                  bool
	cacheCreatePending        bool
	teamCreateConfirmed       bool
	quotaBound                bool
	teamRegistrationFailed    bool
	quotaReserved             bool
	quotaUpdatePending        bool
	quotaReservationUnstarted bool
	teamReplayAttempted       bool
	ownedTeamID               string
	templateBinding           *journalfile.TemplateCreateIntent
}

// cleanup is the two-level teardown registry. Case scopes run when
// their case ends; the stack scope runs after all cases join. Teardown always
// executes on a fresh background context so it completes even when the run's
// context was cancelled (CI cancel / timeout).
type cleanup struct {
	mu                        sync.Mutex
	session                   *Session
	keep                      bool
	journal                   string
	requestedJournal          string
	journalPaths              map[string]string
	journalParents            map[string]os.FileInfo
	execCfg                   map[string]execpkg.Config
	runCommand                func(context.Context, execpkg.Config, string) execpkg.Result
	operations                *operationRuntime
	logf                      func(string, ...any)
	manifest                  []report.Resource
	journalMeta               report.CleanupJournal
	nativeJournals            map[string]report.CleanupJournal
	ownedTemplates            []*cleanupItem
	ownedTeams                []*cleanupItem
	registeredItems           []*cleanupItem
	quotaReservations         []*cleanupItem
	quotaBinding              *report.QuotaCredentialBinding
	unresolvedQuotaTeamCreate bool
	allocationFence           string
	quotaLifecycleRequired    bool
	deferredScopes            [][]*cleanupItem
	deleteClaims              map[string]bool
	teamCreatesPending        int
}

// A reservation remains active until its exact journal child is durably
// settled or retired. A later case must not allocate into that cleanup fence.
func (c *cleanup) hasPendingQuotaUpdate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.quotaReservations {
		if item.quotaUpdatePending {
			return true
		}
	}
	return false
}

// Check the retained journal as well as the current process's reservations.
// Callers that may create a Team hold the journal replay lock across this
// check, provider invocation, and registration.
func (c *cleanup) checkDurablePendingQuotaUpdate(ctx context.Context) error {
	c.mu.Lock()
	fence := c.allocationFence
	unresolvedTeam := c.unresolvedQuotaTeamCreate
	binding := c.quotaBinding
	c.mu.Unlock()
	if unresolvedTeam {
		return fmt.Errorf("unresolved Quota-bound Team create blocks new allocation")
	}
	if fence != "" {
		return fmt.Errorf("Team allocation is fenced after uncertain recovery reservation: %s", fence)
	}
	if c.journal == "" {
		return nil
	}
	path, err := c.checkedJournalPath("primary")
	if err != nil {
		return err
	}
	template, _, _ := c.journalValues(&cleanupItem{role: "primary"})
	known, err := c.rememberedJournalParent(path)
	if err != nil {
		return err
	}
	lock := journalfile.WithLock
	if known != nil {
		lock = journalfile.WithExistingParentLock
	}
	return lock(ctx, path, func() error {
		if err := checkJournalParentIdentity(path, known); err != nil {
			return err
		}
		journal, legacy, err := readRunnerJournal(path)
		if os.IsNotExist(err) {
			if binding != nil {
				return fmt.Errorf("protected cleanup journal is missing for bound Team allocation")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("existing cleanup journal does not match run, execution, role, region, surface and binary")
		}
		// A fresh ordinary run cannot safely inherit a completed Quota run's
		// credential binding. Refuse it before any provider allocation.
		if journal.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, binding) {
			return fmt.Errorf("existing cleanup journal Quota credential binding is not owned by this run")
		}
		return journalfile.CheckQuotaUpdatePending(journal)
	})
}

func (c *cleanup) isQuotaUpdatePending(item *cleanupItem) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return item.quotaUpdatePending
}

func newCleanup(cfg map[string]execpkg.Config, operations *operationRuntime, keep bool, journal string, meta report.CleanupJournal, logf func(string, ...any)) *cleanup {
	meta.Version = 2
	return &cleanup{keep: keep, journal: journal, requestedJournal: journal, execCfg: cfg, runCommand: execpkg.Run, operations: operations, logf: logf, journalMeta: meta}
}

// withDeleteClaim serializes one owned target across concurrent case steps and
// case cleanup, without blocking unrelated resource deletes.
func (c *cleanup) withDeleteClaim(role, command string, run func() error) error {
	key := role + "\x00" + command
	c.mu.Lock()
	if c.deleteClaims == nil {
		c.deleteClaims = make(map[string]bool)
	}
	if c.deleteClaims[key] {
		c.mu.Unlock()
		deleteClaimRejected(command)
		return fmt.Errorf("owned delete is already running: %s", report.Scrub(command))
	}
	c.deleteClaims[key] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.deleteClaims, key)
		c.mu.Unlock()
	}()
	return run()
}

func (c *cleanup) checkedJournalPath(role string) (string, error) {
	if c.journal == "" {
		return "", nil
	}
	want := cleanupJournalPath(c.journal, role)
	if c.journalPaths != nil {
		want = c.journalPaths[role]
	}
	requested := cleanupJournalPath(c.requestedJournal, role)
	current, err := journalfile.ResolveJournalPath(requested)
	if err != nil || current != want {
		return "", fmt.Errorf("cleanup journal target changed")
	}
	return want, nil
}

// Caller holds c.mu. Once this cleanup has observed a journal directory,
// later reads and writes must not accept a different directory at that path.
func (c *cleanup) checkedJournalParentLocked(path string) (os.FileInfo, error) {
	parent := filepath.Dir(path)
	current, err := os.Stat(parent)
	if err != nil {
		return nil, fmt.Errorf("cleanup journal parent %s: %w", parent, err)
	}
	if !current.IsDir() {
		return nil, fmt.Errorf("cleanup journal parent %s is not a directory", parent)
	}
	if known := c.journalParents[path]; known != nil && !os.SameFile(known, current) {
		return nil, fmt.Errorf("cleanup journal parent %s changed", parent)
	}
	return current, nil
}

// Take the remembered inode before acquiring a journal lock. Callers must
// compare it again within the lock and before a provider create is launched.
func (c *cleanup) rememberedJournalParent(path string) (os.FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	known := c.journalParents[path]
	if known != nil {
		if _, err := c.checkedJournalParentLocked(path); err != nil {
			return nil, err
		}
	}
	return known, nil
}

func checkJournalParentIdentity(path string, known os.FileInfo) error {
	parent := filepath.Dir(path)
	current, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("cleanup journal parent %s: %w", parent, err)
	}
	if !current.IsDir() || known != nil && !os.SameFile(known, current) {
		return fmt.Errorf("cleanup journal parent %s changed", parent)
	}
	return nil
}

// Caller holds c.mu and the transaction lock for path.
func (c *cleanup) rememberJournalParentLocked(path string, parent os.FileInfo) {
	if c.journalParents == nil {
		c.journalParents = make(map[string]os.FileInfo)
	}
	if c.journalParents[path] == nil {
		c.journalParents[path] = parent
	}
}

// Region identity is the minimum proof of separation available to cleanup.
// A different binary or environment may still address the same account, so
// equal or unknown regions conservatively share Team ownership fences.
func (c *cleanup) teamTargetsMayOverlap(first, second string) bool {
	a, aOK := c.execCfg[first]
	b, bOK := c.execCfg[second]
	return !aOK || !bOK || a.Region == "" || b.Region == "" || a.Region == b.Region
}

func (c *cleanup) coLocatedTeamRoles(role string) []string {
	roles := make([]string, 0, len(c.execCfg))
	for candidate := range c.execCfg {
		if c.teamTargetsMayOverlap(role, candidate) {
			roles = append(roles, candidate)
		}
	}
	sort.Strings(roles)
	return roles
}

// push registers a teardown command in a scope and records it in the manifest.
func (c *cleanup) push(scope *[]*cleanupItem, name, cmd, role string, lockKeys []string, binding ...*journalfile.TemplateCreateIntent) error {
	_, err := c.pushItem(scope, name, cmd, role, lockKeys, false, false, false, binding...)
	return err
}

func (c *cleanup) pushPendingCacheCreate(scope *[]*cleanupItem, name, cmd, role string, lockKeys []string) (*cleanupItem, error) {
	return c.pushItem(scope, name, cmd, role, lockKeys, true, false, false)
}

func (c *cleanup) pushItem(scope *[]*cleanupItem, name, cmd, role string, lockKeys []string, cacheCreatePending, teamCreateConfirmed, quotaBound bool, binding ...*journalfile.TemplateCreateIntent) (*cleanupItem, error) {
	if strings.TrimSpace(cmd) == "" {
		return nil, fmt.Errorf("cleanup finalizer is empty")
	}
	if role == "" {
		role = "primary"
	}
	cfg, ok := c.execCfg[role]
	if !ok {
		return nil, fmt.Errorf("cleanup region role %q is not configured", role)
	}
	keys, err := normalizeLockKeys(lockKeys)
	if err != nil {
		return nil, fmt.Errorf("cleanup lock keys: %w", err)
	}
	if quotaUpdateCandidate(scenario.Step{}, cmd) || quotaDeleteCandidate(cmd) {
		return nil, fmt.Errorf("Quota mutation cannot be registered as an ordinary finalizer")
	}
	teamID, teamErr := journalfile.TeamDeleteTarget(cmd)
	if teamCreateConfirmed && teamErr != nil {
		return nil, fmt.Errorf("confirmed Team creation requires a validated Team finalizer: %w", teamErr)
	}
	it := &cleanupItem{scope: name, cmd: cmd, role: role, lockKeys: keys, cacheCreatePending: cacheCreatePending, teamCreateConfirmed: teamCreateConfirmed, quotaBound: quotaBound}
	if teamCreateConfirmed {
		it.ownedTeamID = teamID
	}
	if len(binding) > 0 {
		copy := *binding[0]
		it.templateBinding = &copy
	}
	c.mu.Lock()
	if _, err := c.checkedJournalPath(role); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if !teamCreateConfirmed && ownedTeamDeleteCandidate(cmd) {
		if rawTeamDeleteCandidate(cmd) {
			c.mu.Unlock()
			return nil, fmt.Errorf("raw Team delete cannot be an unattended finalizer")
		}
		candidateID, candidateErr := teamDeleteTargetForOwnership(cmd, cfg.Region)
		if candidateErr != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("ordinary Team finalizer requires a validated target and region: %w", candidateErr)
		}
		if c.teamCreatesPending > 0 {
			c.mu.Unlock()
			return nil, fmt.Errorf("ordinary Team finalizer cannot race a protected Team creation")
		}
		for _, existing := range c.ownedTeams {
			if c.teamTargetsMayOverlap(role, existing.role) && existing.ownedTeamID == candidateID {
				c.mu.Unlock()
				return nil, fmt.Errorf("ordinary Team finalizer cannot duplicate retained Team ownership")
			}
		}
		if _, found, err := c.retainedTeamParent(role, candidateID, true); err != nil {
			c.mu.Unlock()
			return nil, err
		} else if found {
			c.mu.Unlock()
			return nil, fmt.Errorf("ordinary Team finalizer conflicts with durable Team ownership")
		}
	}
	if cacheCreatePending {
		// The cache create has not launched. A failed journal reservation must
		// not leave an in-memory delete eligible for case cleanup.
		if err := c.appendJournalItemLocked(it); err != nil {
			c.mu.Unlock()
			return nil, err
		}
	}
	*scope = append(*scope, it)
	c.registeredItems = append(c.registeredItems, it)
	if teamCreateConfirmed {
		c.ownedTeams = append(c.ownedTeams, it)
	}
	if it.templateBinding != nil {
		// Creation ownership outlives a case scope, including --keep runs.
		c.ownedTemplates = append(c.ownedTemplates, it)
	}
	c.manifest = append(c.manifest, report.Resource{
		Scope: name, Teardown: cmd, RegionRole: role, Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID,
	})
	if !cacheCreatePending {
		err = c.appendJournalItemLocked(it)
		if err != nil && teamCreateConfirmed {
			it.teamRegistrationFailed = true
		}
	}
	c.mu.Unlock()
	return it, err
}

// settleCacheCreate clears the durable replay fence only after the default
// create command has waited for a visible, ready cache.
func (c *cleanup) settleCacheCreate(item *cleanupItem) error {
	return c.finishCacheCreate(item, false)
}

// A subprocess that did not start cannot have allocated a cache. Retire its
// exact reservation durably before parent resource cleanup may proceed.
func (c *cleanup) retireUnstartedCacheCreate(item *cleanupItem) error {
	return c.finishCacheCreate(item, true)
}

func (c *cleanup) finishCacheCreate(item *cleanupItem, unstarted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !item.cacheCreatePending || item.done || c.journal == "" {
		return fmt.Errorf("Template Cache create has no pending journal reservation")
	}
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	template, entry, path := c.journalValues(item)
	err := journalfile.WithExistingParentLock(context.Background(), path, func() error {
		journal, _, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if err := mergeJournalMetadata(&journal, template); err != nil {
			return err
		}
		index := -1
		for i := range journal.Entries {
			if journal.Entries[i] == entry {
				if index >= 0 {
					return fmt.Errorf("ambiguous pending Template Cache finalizer")
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("pending Template Cache finalizer is missing")
		}
		if unstarted {
			journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		} else {
			journal.Entries[index].CacheCreatePending = false
		}
		return writeRunnerJournal(path, journal)
	})
	if err == nil {
		if unstarted {
			item.done = true
		} else {
			item.cacheCreatePending = false
		}
	}
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
		if !item.done && item.role == role && (commandsEquivalent(item.cmd, command) || nativeTemplateDeletesEquivalent(item.cmd, command, c.execCfg[role])) {
			if err := c.removeJournalItemLocked(item); err != nil {
				return err
			}
			item.done = true
			return nil
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
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	template, entry, path := c.journalValues(item)
	lock := journalfile.WithLock
	if c.journalParents[path] != nil || item.teamCreateConfirmed {
		lock = journalfile.WithExistingParentLock
	}
	var parent os.FileInfo
	err := lock(context.Background(), path, func() error {
		var parentErr error
		parent, parentErr = c.checkedJournalParentLocked(path)
		if parentErr != nil {
			return parentErr
		}
		journal, _, err := readRunnerJournal(path)
		if os.IsNotExist(err) {
			journal = template
		} else if err != nil {
			return err
		}
		if err := mergeJournalMetadata(&journal, template); err != nil {
			return err
		}
		if item.teamCreateConfirmed && journal.JournalPath != path {
			return fmt.Errorf("confirmed Team requires its anchored cleanup journal")
		}
		if !isReplayableTeardown(item.cmd) {
			return writeRunnerJournal(path, journal)
		}
		journal.Entries = append(journal.Entries, entry)
		if item.teamCreateConfirmed {
			return writeTeamRegistrationJournal(path, journal)
		}
		return writeRunnerJournal(path, journal)
	})
	if err == nil {
		c.rememberJournalParentLocked(path, parent)
	}
	return err
}

func (c *cleanup) removeJournalItemLocked(item *cleanupItem) error {
	if c.journal == "" || !isReplayableTeardown(item.cmd) {
		return nil
	}
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	template, entry, path := c.journalValues(item)
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
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
	if item.role == "primary" {
		journal.QuotaBinding = c.quotaBinding
	}
	journal.Entries = make([]report.Resource, 0)
	entry := report.Resource{
		Scope: item.scope, Teardown: item.cmd, RegionRole: item.role,
		Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID, CacheCreatePending: item.cacheCreatePending,
		TeamCreateConfirmed: item.teamCreateConfirmed, QuotaBound: item.quotaBound, QuotaReserved: item.quotaReserved,
		QuotaUpdatePending: item.quotaUpdatePending, TeamReplayAttempted: item.teamReplayAttempted,
	}
	path := cleanupJournalPath(c.journal, item.role)
	if c.journalPaths != nil {
		path = c.journalPaths[item.role]
	}
	return journal, entry, path
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
	if err := journalfile.CheckJournalPath(path, journal); err != nil {
		return report.CleanupJournal{}, false, err
	}
	return journal, legacy, nil
}

func mergeJournalMetadata(journal *report.CleanupJournal, desired report.CleanupJournal) error {
	if desired.QuotaBinding != nil {
		if journal.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, desired.QuotaBinding) {
			return fmt.Errorf("cleanup journal Quota credential binding changed")
		}
		journal.QuotaBinding = desired.QuotaBinding
	}
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
	if left.Scope != right.Scope || left.Teardown != right.Teardown || left.CacheCreatePending != right.CacheCreatePending || left.QuotaUpdatePending != right.QuotaUpdatePending {
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
	return journalfile.WriteDurable(path, append(data, '\n'))
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
	return c.runCleanup(scope, false)
}

func (c *cleanup) runCaseCleanup(scope []*cleanupItem) []string {
	return c.runCleanup(scope, true)
}

// Retry only unattempted case cleanup after all allocation steps have settled.
// Each retained scope keeps its original LIFO dependency order and all guards.
func (c *cleanup) runDeferredCleanup() []string {
	c.mu.Lock()
	scopes := c.deferredScopes
	c.deferredScopes = nil
	c.mu.Unlock()
	var failures []string
	for _, scope := range scopes {
		c.mu.Lock()
		for _, item := range scope {
			item.deferred = false
		}
		c.mu.Unlock()
		failures = append(failures, c.run(scope)...)
	}
	return failures
}

func (c *cleanup) runCleanup(scope []*cleanupItem, deferPending bool) []string {
	var failures []string
	if c.keep {
		return nil
	}
	for i := len(scope) - 1; i >= 0; i-- {
		it := scope[i]
		cacheDelete := journalfile.IsTemplateCacheDelete(it.cmd)
		quotaDelete := quotaDeleteCandidate(it.cmd)
		teamDelete := ownedTeamDeleteCandidate(it.cmd)
		orderedChildDelete := cacheDelete || quotaDelete
		c.mu.Lock()
		if it.done {
			c.mu.Unlock()
			continue
		}
		if it.running || it.deferred {
			c.mu.Unlock()
			if orderedChildDelete {
				break
			}
			continue
		}
		pendingQuota := it.quotaUpdatePending
		unstartedQuota := it.quotaReservationUnstarted
		it.running = true
		c.mu.Unlock()

		var res execpkg.Result
		var itemTimeout time.Duration
		var timedOut bool
		var guardErr error
		var journalErr error
		coordinationErr := c.operations.executeKeys(context.Background(), it.lockKeys, func() {
			// Resource/parallel queue time consumes neither the journal preflight
			// budget nor the separate command timeout. A long delete retains the
			// shared lock until completion, even after preflight's deadline.
			preflightCtx, cancelPreflight := context.WithTimeout(context.Background(), min(cleanupTimeout, 30*time.Second))
			defer cancelPreflight()
			runGuard := func() error {
				before := func() error {
					if unstartedQuota && pendingQuota {
						if err := c.retireUnstartedQuotaUpdate(it); err != nil {
							return errors.Join(journalfile.ErrQuotaUpdatePending, fmt.Errorf("retire unstarted Quota reservation: %w", err))
						}
						pendingQuota = false
					}
					return nil
				}
				if teamDelete {
					teamDeleteBeforeReplayLock()
				}
				return c.withTemplateCleanupGuardBefore(preflightCtx, it.role, true, before, func(bound execpkg.Config) error {
					if unstartedQuota {
						return nil
					}
					if quotaUpdateCandidate(scenario.Step{}, it.cmd) && !journalfile.IsSandboxQuotaDelete(it.cmd) {
						return fmt.Errorf("Quota update cannot run as an ordinary finalizer")
					}
					if pendingQuota {
						return journalfile.ErrQuotaUpdatePending
					}
					if teamDelete {
						owned, err := c.prepareOwnedTeamDelete(scope, it.cmd, it.role, it)
						if err != nil {
							return err
						}
						if owned != nil && owned != it {
							return fmt.Errorf("owned Team finalizer changed before cleanup")
						}
					}
					if quotaDelete {
						if err := c.verifyOwnedQuotaDelete(scope, nil, it.cmd, it.role); err != nil {
							return err
						}
					}
					command, journalItem := it.cmd, it
					target, native, err := parseNativeTemplateDelete(command)
					if err != nil {
						return err
					}
					if it.templateBinding != nil && !native {
						return fmt.Errorf("bound native Template finalizer requires a native delete")
					}
					if native {
						if it.templateBinding == nil || c.journal == "" {
							return fmt.Errorf("native Template finalizer requires exact retained creation ownership")
						}
						owned := *it
						intent := templateCreateIntent{TemplateCreateIntent: *it.templateBinding}
						owned.cmd = intent.deleteCommand(target.id)
						meta, _, requested := c.journalValues(it)
						meta, path, err := c.requiredTemplateJournal(it.role, meta, requested)
						if err != nil {
							return err
						}
						if err := journalfile.CheckTemplateDeleteOwnership(path, meta, c.execCfg[it.role].Env, *it.templateBinding, owned.cmd); err != nil {
							return err
						}
						command, err = c.bindNativeTemplateDelete([]*cleanupItem{&owned}, command, it.role)
						if err != nil {
							return err
						}
						// Execute the normalized spelling, but consume the original
						// canonical ownership entry after successful deletion.
						journalItem = &owned
					}
					if binding := it.templateBinding; binding != nil {
						if bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != binding.Identity {
							return fmt.Errorf("native finalizer credential binding differs from retained ownership")
						}
						if err := binding.ValidateConfig(c.execCfg[it.role].Env); err != nil {
							return err
						}
						bound = binding.BoundConfig(c.execCfg[it.role])
					}
					itemTimeout = cleanupCommandTimeout(command)
					commandCtx, cancel := context.WithTimeout(context.Background(), itemTimeout)
					res = c.runCommand(commandCtx, bound, command)
					// A refused guard/lock or a failure to start is not an attempt.
					// Keep those finalizers eligible for the later cleanup pass.
					c.mu.Lock()
					it.done = res.Err == nil && res.Exit >= 0 && !quotaDelete && !it.teamCreateConfirmed
					c.mu.Unlock()
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
							if teamDelete {
								if _, err := c.prepareOwnedTeamDelete(scope, it.cmd, it.role, it); err != nil {
									cancel()
									return err
								}
							}
							if quotaDelete {
								if err := c.verifyOwnedQuotaDelete(scope, nil, it.cmd, it.role); err != nil {
									cancel()
									return err
								}
							}
							if quotaDelete || teamDelete && it.quotaBound {
								// The outer cleanup fence is still held. A retry launches a
								// fresh credential provider, so check the original binding
								// again without recursively acquiring that fence.
								c.mu.Lock()
								binding := c.quotaBinding
								c.mu.Unlock()
								if binding == nil {
									cancel()
									return fmt.Errorf("protected Quota retry lacks its original credential binding")
								}
								verified, err := journalfile.ProbeQuotaBinding(commandCtx, binding, c.execCfg[it.role])
								if err != nil {
									cancel()
									return err
								}
								bound = verified
							}
							res = c.runCommand(commandCtx, bound, command)
						}
						if commandCtx.Err() != nil {
							break
						}
					}
					timedOut = commandCtx.Err() == context.DeadlineExceeded
					cancel()
					if res.Exit == 0 || res.Exit == exitNotFound {
						if quotaDelete {
							journalErr = c.consumeOwnedQuotaDelete(scope, it.cmd, it.role)
						} else if it.teamCreateConfirmed {
							journalErr = c.consumeOwnedTeamDelete(it)
						} else {
							journalErr = c.completeJournalItem(journalItem)
						}
					}
					return nil
				}, it.cmd)
			}
			if quotaDelete || teamDelete {
				claim := it.cmd
				if teamDelete {
					claim = ownedTeamClaimKey(it.cmd, c.execCfg[it.role].Region)
				}
				guardErr = c.withDeleteClaim(it.role, claim, runGuard)
			} else {
				guardErr = runGuard()
			}
		})
		c.mu.Lock()
		it.running = false
		c.mu.Unlock()
		if guardErr != nil {
			coordinationErr = guardErr
		}
		if coordinationErr != nil {
			if deferPending && !res.Started && (errors.Is(coordinationErr, journalfile.ErrTemplateCreatePending) || errors.Is(coordinationErr, journalfile.ErrTemplateCacheCreatePending) || errors.Is(coordinationErr, journalfile.ErrQuotaUpdatePending) || errors.Is(coordinationErr, ErrTeamCreatePending) || errors.Is(coordinationErr, ErrTeamRegistrationPending) || errors.Is(coordinationErr, context.DeadlineExceeded)) {
				// Defer remaining ancestors with this child, even if another
				// allocation settles before the next item could be checked.
				remaining := append([]*cleanupItem(nil), scope[:i+1]...)
				c.mu.Lock()
				for _, item := range remaining {
					item.deferred = true
				}
				c.deferredScopes = append(c.deferredScopes, remaining)
				c.mu.Unlock()
				c.logf("cleanup deferred until allocations settle: %s", report.Scrub(it.cmd))
				break
			}
			failure := fmt.Sprintf("cleanup coordination: %s: %v", report.Scrub(it.cmd), coordinationErr)
			if res.Started {
				failure += "; prior invocation: " + cleanupFailure(res, it.cmd, timedOut, itemTimeout)
			}
			c.logf("cleanup failed: %s", failure)
			failures = append(failures, failure)
			if orderedChildDelete {
				// The remaining entries may be this cache's Template and Team.
				break
			}
			continue
		}
		switch {
		case res.Exit == 0:
			c.logf("cleanup ok: %s", it.cmd)
			if journalErr != nil {
				failure := fmt.Sprintf("cleanup journal rewrite: %s: %v", report.Scrub(it.cmd), journalErr)
				c.logf("cleanup failed: %s", failure)
				failures = append(failures, failure)
			}
		case res.Exit == exitNotFound:
			// The resource is already gone — typically the case's own delete step
			// removed it and this teardown is the safety net. That is the desired
			// end state, not a failure worth flagging.
			c.logf("cleanup ok (already gone): %s", it.cmd)
			if journalErr != nil {
				failure := fmt.Sprintf("cleanup journal rewrite: %s: %v", report.Scrub(it.cmd), journalErr)
				c.logf("cleanup failed: %s", failure)
				failures = append(failures, failure)
			}
		default:
			failure := cleanupFailure(res, it.cmd, timedOut, itemTimeout)
			c.logf("cleanup failed: %s", failure)
			failures = append(failures, failure)
		}
		if orderedChildDelete && (coordinationErr != nil || res.Err != nil || (res.Exit != 0 && res.Exit != exitNotFound) || journalErr != nil) {
			// A later in-process cleanup pass must retry the child first.
			c.mu.Lock()
			it.done = false
			c.mu.Unlock()
			break
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
