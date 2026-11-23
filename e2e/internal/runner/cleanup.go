package runner

import (
	"bytes"
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
var writeVolumeFixtureConsumptionJournal = writeRunnerJournalRoot
var writeVolumeFixtureRestoreJournal = writeRunnerJournalRoot
var writeVolumeFixtureReservationJournal = writeRunnerJournal
var ensureVolumeFixtureRecoveryDirectory = journalfile.EnsureRecoveryDirectory

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
	volumeTeamBound           bool
	volumeTeamBinding         *report.QuotaCredentialBinding
	quotaBound                bool
	teamRegistrationFailed    bool
	quotaReserved             bool
	quotaUpdatePending        bool
	quotaReservationUnstarted bool
	teamReplayAttempted       bool
	teamAttemptFresh          bool
	ownedTeamID               string
	templateBinding           *journalfile.TemplateCreateIntent
	// Team provenance is durable in the journal; this records only a
	// successful transfer of this Volume during the active run.
	volumeTransferTeam                string
	volumeDeleteAttempted             bool
	volumeFixtureKind                 string
	volumeFixturePending              bool
	volumeFixtureDispatchProtocol     int
	volumeFixtureDispatched           bool
	volumeFixtureCreateRequestID      string
	volumeFixtureCreateResponseSHA256 string
	volumeFixtureCreateValidated      bool
	volumeFixtureRoleID               string
	volumeFixturePolicyCreateDate     string
	volumeFixtureBucketCreationDate   string
	volumeFixtureDeleteProtocol       int
	volumeFixtureDeleteAttempted      bool
	teamCreateIntent                  *teamCreateIntent
}

// cleanup is the two-level teardown registry. Case scopes run when
// their case ends; the stack scope runs after all cases join. Teardown always
// executes on a fresh background context so it completes even when the run's
// context was cancelled (CI cancel / timeout).
type cleanup struct {
	mu                            sync.Mutex
	session                       *Session
	keep                          bool
	journal                       string
	requestedJournal              string
	journalPaths                  map[string]string
	journalParents                map[string]os.FileInfo
	journalEstablished            map[string]bool
	execCfg                       map[string]execpkg.Config
	runCommand                    func(context.Context, execpkg.Config, string) execpkg.Result
	operations                    *operationRuntime
	logf                          func(string, ...any)
	manifest                      []report.Resource
	journalMeta                   report.CleanupJournal
	nativeJournals                map[string]report.CleanupJournal
	ownedTemplates                []*cleanupItem
	ownedTeams                    []*cleanupItem
	registeredItems               []*cleanupItem
	quotaReservations             []*cleanupItem
	quotaBinding                  *report.QuotaCredentialBinding
	volumeFixtureBinding          *report.QuotaCredentialBinding
	volumeFixtureBucket           string
	volumeTeamRetirementUncertain bool
	// Direct reservation unit tests use a fake Team without provisioned OSS/RAM.
	// Runtime cleanup registries leave this false, so every create is checked.
	testSyntheticVolumeReservation bool
	unresolvedQuotaTeamCreate      bool
	allocationFence                string
	quotaLifecycleRequired         bool
	deferredScopes                 [][]*cleanupItem
	deleteClaims                   map[string]bool
	teamCreatesPending             int
	// Only package tests can install a synthetic exclusive mutation scope.
	// Production runs have no such scope and refuse name-based fixture writes.
	fixtureMutationScope func(phase, kind, bucket string, cfg execpkg.Config) bool
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
		// Only primary Volume ownership carries a pinned executable digest.
		volumeJournal := journal.VolumeFixtureBinding != nil || journal.VolumeFixtureBucket != "" || len(journal.VolumeTeamCreateIDs) != 0 || len(journal.VolumeTeamCreators) != 0
		for _, entry := range journal.Entries {
			volumeJournal = volumeJournal || entry.VolumeFixtureKind != "" || entry.VolumeFixturePending || entry.VolumeTeamBound
		}
		if volumeJournal && (template.EcctlBinSHA256 == "" || journal.EcctlBinSHA256 != template.EcctlBinSHA256) {
			return fmt.Errorf("existing Volume cleanup journal binary digest is missing or changed")
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

func (c *cleanup) fixtureMutationAllowed(phase, kind, bucket string, item *cleanupItem, cfg execpkg.Config) bool {
	c.mu.Lock()
	scope, binding, currentBucket := c.fixtureMutationScope, c.volumeFixtureBinding, c.volumeFixtureBucket
	c.mu.Unlock()
	if binding == nil || currentBucket != bucket || item == nil || item.volumeFixtureKind != kind ||
		(phase != "create" && phase != "delete") || !journalfile.VolumeFixtureFencedKind(kind) ||
		cfg.Bin != c.execCfg["primary"].Bin || cfg.Region != "cn-hangzhou" ||
		cfg.Profile != binding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != binding.Identity {
		return false
	}
	journal, err := c.currentVolumeFixtureJournal(item)
	if err != nil {
		return false
	}
	c.mu.Lock()
	_, entry, _ := c.journalValues(item)
	c.mu.Unlock()
	if journalfile.VolumeFixtureMutationAllowed(phase, journal, entry, cfg) {
		return true
	}
	return scope != nil && scope(phase, kind, bucket, cfg)
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

// A parent directory can be observed before this role has ever owned a
// journal. Keep actual file provenance separate from the parent inode.
func (c *cleanup) rememberEstablishedJournalLocked(path string) {
	if c.journalEstablished == nil {
		c.journalEstablished = make(map[string]bool)
	}
	c.journalEstablished[path] = true
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
	return c.pushItemWithFixture(scope, name, cmd, role, lockKeys, cacheCreatePending, teamCreateConfirmed, quotaBound, nil, "", nil, binding...)
}

func (c *cleanup) pushItemWithFixture(scope *[]*cleanupItem, name, cmd, role string, lockKeys []string, cacheCreatePending, teamCreateConfirmed, quotaBound bool, teamBinding *report.QuotaCredentialBinding, fixtureKind string, teamIntent *teamCreateIntent, binding ...*journalfile.TemplateCreateIntent) (*cleanupItem, error) {
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
	volumeDelete, err := classifyVolumeFinalizer(cmd)
	if err != nil {
		return nil, err
	}
	if volumeDelete && (len(binding) == 0 || binding[0] == nil || binding[0].Resource != "volume") {
		return nil, fmt.Errorf("native Volume finalizer requires exact retained creation ownership")
	}
	if len(binding) > 0 && binding[0] != nil && binding[0].Resource == "volume" && !volumeDelete {
		return nil, fmt.Errorf("bound native finalizer requires its resource delete")
	}
	teamID, teamErr := journalfile.TeamDeleteTarget(cmd)
	if teamCreateConfirmed && teamErr != nil {
		return nil, fmt.Errorf("confirmed Team creation requires a validated Team finalizer: %w", teamErr)
	}
	if teamBinding != nil && !teamCreateConfirmed {
		return nil, fmt.Errorf("Volume Team credential binding requires confirmed creation")
	}
	if teamBinding != nil && (teamIntent == nil || !reflect.DeepEqual(teamIntent.VolumeBinding, teamBinding)) || teamIntent != nil && !teamCreateConfirmed {
		return nil, fmt.Errorf("Volume Team finalizer lost its exact create intent")
	}
	it := &cleanupItem{scope: name, cmd: cmd, role: role, lockKeys: keys, cacheCreatePending: cacheCreatePending, teamCreateConfirmed: teamCreateConfirmed, quotaBound: quotaBound, volumeTeamBound: teamBinding != nil, volumeTeamBinding: teamBinding, volumeFixtureKind: fixtureKind, volumeFixturePending: fixtureKind != "", teamCreateIntent: teamIntent}
	if fixtureKind != "" {
		it.volumeFixtureDispatchProtocol = 1
	}
	if journalfile.VolumeFixtureFencedKind(fixtureKind) {
		it.volumeFixtureDeleteProtocol = 1
	}
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
	if cacheCreatePending || it.volumeFixturePending {
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
	if !cacheCreatePending && !it.volumeFixturePending {
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
		if item.volumeFixtureKind != "" {
			if err := ensureVolumeFixtureRecoveryDirectory(path); err != nil {
				return fmt.Errorf("durable Volume fixture journal directory: %w", err)
			}
			current, err := c.checkedJournalParentLocked(path)
			if err != nil {
				return err
			}
			if !os.SameFile(parent, current) {
				return fmt.Errorf("Volume fixture journal parent changed during durability barrier")
			}
		}
		journal, _, existingBody, err := readRunnerJournalSnapshot(path)
		journalMissing := os.IsNotExist(err)
		if journalMissing {
			journal = template
		} else if err != nil {
			return err
		}
		var originalBody []byte
		if item.volumeFixturePending && !journalMissing {
			originalBody = existingBody
		}
		if err := mergeJournalMetadata(&journal, template); err != nil {
			return err
		}
		if item.teamCreateConfirmed && journal.JournalPath != path {
			return fmt.Errorf("confirmed Team requires its anchored cleanup journal")
		}
		if item.volumeTeamBound {
			if !reflect.DeepEqual(journal.VolumeFixtureBinding, item.volumeTeamBinding) {
				return fmt.Errorf("Volume Team creator differs from fixture credential binding")
			}
			if journal.VolumeTeamCreators == nil {
				journal.VolumeTeamCreators = make(map[string]*report.QuotaCredentialBinding)
			}
			if prior, exists := journal.VolumeTeamCreators[item.ownedTeamID]; exists && !reflect.DeepEqual(prior, item.volumeTeamBinding) {
				return fmt.Errorf("Volume Team creator credential binding changed")
			}
			journal.VolumeTeamCreators[item.ownedTeamID] = item.volumeTeamBinding
		}
		if item.volumeFixtureKind != "" {
			if err := journalfile.CheckVolumeFixtureParents(item.volumeFixtureKind, journal); err != nil {
				return err
			}
		}
		if item.volumeFixturePending && journalMissing {
			// Establish a base journal so a failed first reservation can restore
			// exact prelaunch bytes without removing an unanchored file.
			if err := writeRunnerJournal(path, journal); err != nil {
				if barrierErr := reaffirmRunnerJournal(path, journal); barrierErr != nil {
					return errors.Join(err, barrierErr)
				}
			}
			originalBody, err = runnerJournalBytes(journal)
			if err != nil {
				return err
			}
		}
		if item.volumeFixtureKind == "" && !isReplayableTeardown(item.cmd) {
			return writeRunnerJournal(path, journal)
		}
		journal.Entries = append(journal.Entries, entry)
		if item.teamCreateConfirmed {
			if item.volumeTeamBound {
				if err := settleVolumeTeamRegistration(&journal, item.teamCreateIntent, item.ownedTeamID, path); err != nil {
					return err
				}
			}
			if err := writeTeamRegistrationJournal(path, journal); err != nil {
				return err
			}
			if item.volumeTeamBound {
				installed, legacy, err := readRunnerJournal(path)
				if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
					return fmt.Errorf("Volume Team registration was not installed exactly")
				}
			}
			return nil
		}
		if item.volumeFixturePending {
			body, err := runnerJournalBytes(journal)
			if err != nil {
				return err
			}
			if err := writeVolumeFixtureReservationJournal(path, journal); err != nil {
				if installed, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(installed, body) {
					return errors.Join(err, journalfile.RestoreUninvokedMarker(path, body, originalBody))
				}
				return err
			}
			return nil
		}
		return writeRunnerJournal(path, journal)
	})
	if err == nil {
		c.rememberJournalParentLocked(path, parent)
		c.rememberEstablishedJournalLocked(path)
	}
	return err
}

func (c *cleanup) removeJournalItemLocked(item *cleanupItem) error {
	if c.journal == "" || item.volumeFixtureKind == "" && !isReplayableTeardown(item.cmd) {
		return nil
	}
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	template, entry, path := c.journalValues(item)
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		var root *os.Root
		var err error
		if item.volumeFixtureKind != "" {
			root, err = c.openLockedJournalRoot(path)
			if err != nil {
				return err
			}
			defer root.Close()
		}
		var journal report.CleanupJournal
		if root != nil {
			journal, _, err = readRunnerJournalRoot(root, path)
		} else {
			journal, _, err = readRunnerJournal(path)
		}
		if os.IsNotExist(err) {
			if item.volumeFixtureKind != "" {
				return fmt.Errorf("Volume fixture journal disappeared before consumption")
			}
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
			if item.volumeFixtureKind != "" && journal.Entries[i] == entry || item.volumeFixtureKind == "" && journalEntriesMatch(journal.Entries[i], entry) {
				index = i
				break
			}
		}
		if index < 0 {
			if item.volumeFixtureKind != "" {
				return fmt.Errorf("Volume fixture finalizer changed before consumption")
			}
			return nil
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		if item.volumeFixtureKind != "" {
			if err := writeVolumeFixtureConsumptionJournal(root, path, journal); err != nil {
				return err
			}
			installed, legacy, err := readRunnerJournalRoot(root, path)
			if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
				return fmt.Errorf("Volume fixture consumption was not installed exactly: %v", err)
			}
			return journalRootMatchesPath(root, path)
		}
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
		journal.VolumeFixtureBinding = c.volumeFixtureBinding
		journal.VolumeFixtureBucket = c.volumeFixtureBucket
	}
	if journal.VolumeFixtureBinding != nil {
		journal.JournalPath = cleanupJournalPath(c.journal, item.role)
		if c.journalPaths != nil {
			journal.JournalPath = c.journalPaths[item.role]
		}
	}
	journal.Entries = make([]report.Resource, 0)
	entry := report.Resource{
		Scope: item.scope, Teardown: item.cmd, RegionRole: item.role,
		Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID, CacheCreatePending: item.cacheCreatePending,
		TeamCreateConfirmed: item.teamCreateConfirmed, QuotaBound: item.quotaBound, QuotaReserved: item.quotaReserved,
		VolumeTeamBound:       item.volumeTeamBound,
		VolumeDeleteAttempted: item.volumeDeleteAttempted,
		QuotaUpdatePending:    item.quotaUpdatePending, TeamReplayAttempted: item.teamReplayAttempted,
		VolumeFixtureKind: item.volumeFixtureKind, VolumeFixturePending: item.volumeFixturePending, VolumeFixtureDispatchProtocol: item.volumeFixtureDispatchProtocol, VolumeFixtureDispatched: item.volumeFixtureDispatched,
		VolumeFixtureCreateRequestID: item.volumeFixtureCreateRequestID, VolumeFixtureCreateResponseSHA256: item.volumeFixtureCreateResponseSHA256, VolumeFixtureCreateValidated: item.volumeFixtureCreateValidated, VolumeFixtureRoleID: item.volumeFixtureRoleID,
		VolumeFixturePolicyCreateDate: item.volumeFixturePolicyCreateDate, VolumeFixtureBucketCreationDate: item.volumeFixtureBucketCreationDate,
		VolumeFixtureDeleteProtocol: item.volumeFixtureDeleteProtocol, VolumeFixtureDeleteAttempted: item.volumeFixtureDeleteAttempted,
	}
	path := cleanupJournalPath(c.journal, item.role)
	if c.journalPaths != nil {
		path = c.journalPaths[item.role]
	}
	return journal, entry, path
}

func readRunnerJournal(path string) (report.CleanupJournal, bool, error) {
	journal, legacy, _, err := readRunnerJournalSnapshot(path)
	return journal, legacy, err
}

// The bytes returned here are the same opened, validated bytes used for the
// journal decision. Reservation rollback may restore only this snapshot.
func readRunnerJournalSnapshot(path string) (report.CleanupJournal, bool, []byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return report.CleanupJournal{}, false, nil, err
	}
	defer root.Close()
	return readRunnerJournalRootSnapshot(root, path)
}

func (c *cleanup) openLockedJournalRoot(path string) (*os.Root, error) {
	parent, err := c.checkedJournalParentLocked(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(parent, anchored) {
		root.Close()
		return nil, fmt.Errorf("cleanup journal parent changed before rooted transition: %v", err)
	}
	return root, nil
}

func (c *cleanup) openRememberedJournalRoot(path string) (*os.Root, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.openLockedJournalRoot(path)
}

// Take the remembered inode before acquiring a journal lock. Callers must
// compare it again within the lock and before a provider create is launched.

func readRunnerJournalRoot(root *os.Root, path string) (report.CleanupJournal, bool, error) {
	journal, legacy, _, err := readRunnerJournalRootSnapshot(root, path)
	return journal, legacy, err
}

func readRunnerJournalRootSnapshot(root *os.Root, path string) (report.CleanupJournal, bool, []byte, error) {
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return report.CleanupJournal{}, false, nil, err
	}
	data, private, readErr := journalfile.ReadRecoveryFileRoot(root, file, filepath.Base(path))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return report.CleanupJournal{}, false, nil, err
	}
	journal, legacy, err := decodeRunnerJournal(path, data, private)
	if err != nil {
		return report.CleanupJournal{}, false, nil, err
	}
	return journal, legacy, data, nil
}

func decodeRunnerJournal(path string, data []byte, private bool) (report.CleanupJournal, bool, error) {
	var journal report.CleanupJournal
	legacy := strings.HasPrefix(strings.TrimSpace(string(data)), "[")
	if legacy {
		if err := execpkg.UnmarshalStrictJSON(data, &journal.Entries); err != nil {
			return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
		}
	} else if err := execpkg.UnmarshalStrictJSON(data, &journal); err != nil {
		return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
	}
	if !legacy && journalfile.ProtectedRecoveryJournal(journal) && !private {
		return report.CleanupJournal{}, false, fmt.Errorf("protected cleanup journal is not a private regular file")
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
	if desired.VolumeFixtureBinding != nil {
		if journal.VolumeFixtureBinding != nil && !reflect.DeepEqual(journal.VolumeFixtureBinding, desired.VolumeFixtureBinding) {
			return fmt.Errorf("cleanup journal Volume fixture credential binding changed")
		}
		journal.VolumeFixtureBinding = desired.VolumeFixtureBinding
		if journal.VolumeFixtureBucket != "" && journal.VolumeFixtureBucket != desired.VolumeFixtureBucket {
			return fmt.Errorf("cleanup journal Volume fixture bucket changed")
		}
		journal.VolumeFixtureBucket = desired.VolumeFixtureBucket
		if desired.JournalPath == "" {
			return fmt.Errorf("Volume fixture journal anchor is missing")
		}
		if journal.JournalPath != "" && journal.JournalPath != desired.JournalPath {
			return fmt.Errorf("Volume fixture journal anchor changed")
		}
		journal.JournalPath = desired.JournalPath
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
	if left.Scope != right.Scope || left.Teardown != right.Teardown || left.CacheCreatePending != right.CacheCreatePending || left.QuotaUpdatePending != right.QuotaUpdatePending || left.VolumeFixtureKind != right.VolumeFixtureKind || left.VolumeFixturePending != right.VolumeFixturePending || left.VolumeFixtureDispatchProtocol != right.VolumeFixtureDispatchProtocol || left.VolumeFixtureDispatched != right.VolumeFixtureDispatched || left.VolumeFixtureCreateRequestID != right.VolumeFixtureCreateRequestID || left.VolumeFixtureCreateResponseSHA256 != right.VolumeFixtureCreateResponseSHA256 || left.VolumeFixtureCreateValidated != right.VolumeFixtureCreateValidated || left.VolumeFixtureRoleID != right.VolumeFixtureRoleID || left.VolumeFixturePolicyCreateDate != right.VolumeFixturePolicyCreateDate || left.VolumeFixtureBucketCreationDate != right.VolumeFixtureBucketCreationDate || left.VolumeFixtureDeleteProtocol != right.VolumeFixtureDeleteProtocol || left.VolumeFixtureDeleteAttempted != right.VolumeFixtureDeleteAttempted || left.VolumeTeamBound != right.VolumeTeamBound {
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
	data, err := runnerJournalBytes(journal)
	if err != nil {
		return err
	}
	return journalfile.WriteDurable(path, data)
}

func writeRunnerJournalRoot(root *os.Root, path string, journal report.CleanupJournal) error {
	data, err := runnerJournalBytes(journal)
	if err != nil {
		return err
	}
	return journalfile.WriteDurableRoot(root, filepath.Base(path), data)
}

func runnerJournalBytes(journal report.CleanupJournal) ([]byte, error) {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func reaffirmRunnerJournal(path string, journal report.CleanupJournal) error {
	data, err := runnerJournalBytes(journal)
	if err != nil {
		return err
	}
	return journalfile.ReaffirmDurable(path, data)
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
// Each retained scope keeps its dependency order and all guards.
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
	}
	// A parent registered before its child may be first in this queue. Only
	// an untouched Team/Volume child dependency waits for a later scope; an
	// attempted delete or any other failure must remain a hard failure.
	maxPasses := len(scopes) + 1
	for pass := 0; len(scopes) > 0 && pass < maxPasses; pass++ {
		pending := make([][]*cleanupItem, 0, len(scopes))
		progress := false
		for _, scope := range scopes {
			before := c.completedCleanupItems(scope)
			var waiting bool
			scopeFailures := c.runCleanup(scope, false, &waiting)
			failures = append(failures, scopeFailures...)
			if c.completedCleanupItems(scope) > before {
				progress = true
			}
			if waiting && len(scopeFailures) == 0 {
				pending = append(pending, scope)
			}
		}
		if len(pending) == 0 {
			return failures
		}
		if !progress {
			for _, scope := range pending {
				failures = append(failures, deferredChildFailure(scope))
			}
			return failures
		}
		scopes = pending
	}
	for _, scope := range scopes {
		failures = append(failures, deferredChildFailure(scope))
	}
	return failures
}

func deferredChildFailure(scope []*cleanupItem) string {
	for _, item := range scope {
		if !item.done {
			return fmt.Sprintf("cleanup coordination: %s: %v", report.Scrub(item.cmd), ErrTeamVolumeChildPending)
		}
	}
	return fmt.Sprintf("cleanup coordination: %v", ErrTeamVolumeChildPending)
}

func (c *cleanup) completedCleanupItems(scope []*cleanupItem) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var completed int
	for _, item := range scope {
		if item.done {
			completed++
		}
	}
	return completed
}

func (c *cleanup) runCleanup(scope []*cleanupItem, deferPending bool, pendingWait ...*bool) []string {
	var failures []string
	volumeChildFailed := false
	if c.keep {
		return nil
	}
	// A Volume can be transferred to a Team registered after the Volume. Its
	// retained delete must run before either Team's finalizer.
	ordered := volumeFirstCleanupOrder(scope)
	for i, it := range ordered {
		cacheDelete := journalfile.IsTemplateCacheDelete(it.cmd)
		volumeDelete, volumeErr := classifyVolumeFinalizer(it.cmd)
		quotaDelete := quotaDeleteCandidate(it.cmd)
		teamDelete := ownedTeamDeleteCandidate(it.cmd)
		boundVolume := it.templateBinding != nil && it.templateBinding.Resource == "volume"
		volumeChildDelete := volumeDelete || boundVolume
		orderedChildDelete := cacheDelete || quotaDelete || volumeDelete || volumeErr != nil || boundVolume || it.volumeFixtureKind != ""
		if volumeChildFailed && (teamDelete || it.volumeFixtureKind != "") {
			continue
		}
		c.mu.Lock()
		if it.done {
			c.mu.Unlock()
			continue
		}
		if it.running || it.deferred {
			c.mu.Unlock()
			if orderedChildDelete && !volumeChildDelete {
				break
			}
			if volumeChildDelete {
				volumeChildFailed = true
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
				if volumeErr != nil {
					return volumeErr
				}
				if volumeDelete && !boundVolume {
					return fmt.Errorf("native Volume finalizer requires exact retained creation ownership")
				}
				if boundVolume && !volumeDelete {
					return fmt.Errorf("bound native finalizer requires its resource delete")
				}
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
					volumeTeamAlreadyAbsent := false
					var volumeTeamPath string
					var volumeTeamJournal report.CleanupJournal
					var volumeTeamEntry report.Resource
					bucketPresence := journalfile.VolumeFixtureBucketUnknown
					if it.volumeFixtureKind != "" {
						c.mu.Lock()
						uncertainRetirement := c.volumeTeamRetirementUncertain
						c.mu.Unlock()
						if uncertainRetirement {
							return fmt.Errorf("Volume Team never-started retirement is uncertain")
						}
						if it.volumeFixturePending {
							return fmt.Errorf("Volume fixture creation outcome is uncertain")
						}
						meta, entry, path := c.journalValues(it)
						journal, legacy, err := readRunnerJournal(path)
						if err != nil || legacy || !reflect.DeepEqual(journal.VolumeFixtureBinding, meta.VolumeFixtureBinding) || journal.VolumeFixtureBucket != meta.VolumeFixtureBucket || journal.JournalPath != path {
							return fmt.Errorf("Volume fixture journal ownership changed")
						}
						bound, err = journalfile.ProbeQuotaBinding(preflightCtx, journal.VolumeFixtureBinding, bound)
						if err != nil {
							return err
						}
						if err := journalfile.VolumeFixtureCleanupReady(path, journal, entry, c.execCfg[it.role].Env); err != nil {
							return err
						}
						bucketPresence, err = journalfile.VolumeFixtureBucketEmpty(preflightCtx, bound, journal.VolumeFixtureBucket, c.runCommand)
						if err != nil {
							return err
						}
						if it.volumeFixtureKind != "bucket" && bucketPresence != journalfile.VolumeFixtureBucketVerifiedAbsent {
							return fmt.Errorf("Volume fixture RAM teardown requires verified bucket absence")
						}
					}
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
						if it.volumeTeamBound {
							_, _, path := c.journalValues(it)
							journal, legacy, err := readRunnerJournal(path)
							if err != nil || legacy {
								return fmt.Errorf("Volume Team creator journal is unavailable: %v", err)
							}
							var found bool
							bound, found, err = journalfile.VolumeTeamCreatorConfig(preflightCtx, journal, c.execCfg[it.role], it.ownedTeamID)
							if err != nil || !found {
								return fmt.Errorf("Volume Team creator credential is unavailable: %v", err)
							}
							volumeTeamPath, volumeTeamJournal, volumeTeamEntry, err = c.volumeTeamChildContext(it)
							if err != nil {
								return err
							}
							childCtx, cancelChildren := context.WithTimeout(context.Background(), cleanupCommandTimeout(it.cmd))
							volumeTeamAlreadyAbsent, err = journalfile.PrepareVolumeTeamChildren(childCtx, volumeTeamPath, volumeTeamJournal, volumeTeamEntry, bound, c.runCommand)
							cancelChildren()
							if err != nil {
								return err
							}
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
					volumeNative, err := classifyVolumeFinalizer(command)
					if err != nil {
						return err
					}
					if volumeNative && (it.templateBinding == nil || it.templateBinding.Resource != "volume") {
						return fmt.Errorf("native Volume finalizer requires exact retained creation ownership")
					}
					if it.templateBinding != nil && (it.templateBinding.Resource == "volume" && !volumeNative || it.templateBinding.Resource == "" && !native) {
						return fmt.Errorf("bound native finalizer requires its resource delete")
					}
					if volumeNative && it.templateBinding != nil && it.templateBinding.Resource == "volume" {
						args, splitErr := shlex.Split(command)
						if splitErr != nil || len(args) < 5 {
							return fmt.Errorf("invalid owned Volume delete grammar")
						}
						meta, _, requested := c.journalValues(it)
						meta, path, err := c.requiredTemplateJournal(it.role, meta, requested)
						if err != nil {
							return err
						}
						if err := journalfile.CheckTemplateDeleteOwnership(path, meta, c.execCfg[it.role].Env, *it.templateBinding, command); err != nil {
							return err
						}
						intent := templateCreateIntent{TemplateCreateIntent: *it.templateBinding}
						if command != intent.deleteCommand(args[4]) {
							return fmt.Errorf("Volume finalizer differs from retained exact delete")
						}
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
						bound.Region, bound.Profile = binding.Region, binding.Profile
					}
					if volumeNative {
						command += " --output=json --agent-envelope=false"
					}
					if it.volumeFixtureKind == "bucket" && bucketPresence == journalfile.VolumeFixtureBucketVerifiedAbsent {
						res = execpkg.Result{Exit: 0}
						return c.retireAbsentVolumeFixture(it)
					}
					fencedFixture := journalfile.VolumeFixtureFencedKind(it.volumeFixtureKind)
					fixtureReadOnly := fencedFixture && (it.volumeFixtureDeleteAttempted || it.volumeFixtureDeleteProtocol != 1)
					freshFixtureAttempt := false
					freshVolumeAttempt := false
					if fencedFixture && !fixtureReadOnly {
						meta, entry, _ := c.journalValues(it)
						if !c.fixtureMutationAllowed("delete", it.volumeFixtureKind, meta.VolumeFixtureBucket, it, bound) {
							return fmt.Errorf("Volume fixture name mutation requires a provider-bound target or an exclusive mutation scope")
						}
						switch it.volumeFixtureKind {
						case "role":
							var roleRead execpkg.Result
							roleRead, err = journalfile.VolumeFixtureRoleMatches(preflightCtx, bound, entry, meta, c.runCommand)
							if err != nil && roleRead.Err == nil && roleRead.Started && roleRead.Exit == exitNotFound {
								if _, absentErr := journalfile.VolumeFixtureRoleAbsent(preflightCtx, bound, entry, meta, c.runCommand); absentErr == nil {
									res = execpkg.Result{Exit: 0}
									return c.retireAbsentVolumeFixture(it)
								} else {
									err = absentErr
								}
							}
						case "policy":
							var policyRead execpkg.Result
							policyRead, err = journalfile.VolumeFixturePolicyMatches(preflightCtx, bound, entry, meta, c.runCommand)
							if err != nil && policyRead.Err == nil && policyRead.Started && policyRead.Exit == exitNotFound {
								if _, absentErr := journalfile.VolumeFixturePolicyAbsent(preflightCtx, bound, entry, meta, func(context.Context, execpkg.Config, string) execpkg.Result { return policyRead }); absentErr == nil {
									res = execpkg.Result{Exit: 0}
									return c.retireAbsentVolumeFixture(it)
								} else {
									err = absentErr
								}
							}
						case "bucket":
							_, err = journalfile.VolumeFixtureBucketMatches(preflightCtx, bound, entry, meta, c.runCommand)
						case "attachment":
							var current report.CleanupJournal
							current, err = c.currentVolumeFixtureJournal(it)
							if err == nil {
								var role, policy report.Resource
								role, policy, err = journalfile.VolumeFixtureRAMParents(current)
								if err == nil {
									var roleRead execpkg.Result
									roleRead, err = journalfile.VolumeFixtureRoleMatches(preflightCtx, bound, role, current, c.runCommand)
									if err != nil && roleRead.Err == nil && roleRead.Started && roleRead.Exit == exitNotFound {
										if _, absentErr := journalfile.VolumeFixtureRoleAbsent(preflightCtx, bound, role, current, func(context.Context, execpkg.Config, string) execpkg.Result { return roleRead }); absentErr == nil {
											res = execpkg.Result{Exit: 0}
											return c.retireAbsentVolumeFixture(it)
										} else {
											err = absentErr
										}
									}
								}
								if err == nil {
									var policyRead execpkg.Result
									policyRead, err = journalfile.VolumeFixturePolicyMatches(preflightCtx, bound, policy, current, c.runCommand)
									if err != nil && policyRead.Err == nil && policyRead.Started && policyRead.Exit == exitNotFound {
										if _, absentErr := journalfile.VolumeFixturePolicyAbsent(preflightCtx, bound, policy, current, func(context.Context, execpkg.Config, string) execpkg.Result { return policyRead }); absentErr == nil {
											res = execpkg.Result{Exit: 0}
											return c.retireAbsentVolumeFixture(it)
										} else {
											err = absentErr
										}
									}
								}
							}
						}
						if err != nil {
							return err
						}
						if err := c.markVolumeFixtureDeleteAttempt(it); err != nil {
							return err
						}
						freshFixtureAttempt = true
					}
					itemTimeout = cleanupCommandTimeout(command)
					commandCtx, cancel := context.WithTimeout(context.Background(), itemTimeout)
					volumeReadOnly := false
					if volumeNative {
						request, parseErr := journalfile.ParseVolumeDeleteRequest(it.cmd)
						if parseErr != nil {
							cancel()
							return parseErr
						}
						teams, scopeErr := c.ownedVolumeDeleteScopes(it)
						if scopeErr != nil {
							cancel()
							return scopeErr
						}
						current, absent, probeErr := journalfile.ProbeVolumeScopes(commandCtx, bound, request.ID, it.templateBinding.Name, it.templateBinding.Region, it.templateBinding.Profile, teams, c.runCommand)
						if probeErr != nil {
							cancel()
							return probeErr
						}
						if absent {
							if err := c.requireTerminalVolumeAbsence(it); err != nil {
								cancel()
								return err
							}
							volumeReadOnly = true
							res = execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
						} else {
							if it.volumeDeleteAttempted {
								waitCtx, cancelWait := context.WithTimeout(commandCtx, 45*time.Second)
								terminal := journalfile.WaitVolumeAbsent(waitCtx, bound, request.ID, it.templateBinding.Name, it.templateBinding.Region, it.templateBinding.Profile, teams, c.runCommand)
								cancelWait()
								if !terminal {
									cancel()
									return fmt.Errorf("Volume delete was already attempted and remains present")
								}
								if err := c.requireTerminalVolumeAbsence(it); err != nil {
									cancel()
									return err
								}
								volumeReadOnly = true
								res = execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
							} else {
								command += " --team-id " + teamCommandArgument(current)
								if err := preflightCtx.Err(); err != nil {
									cancel()
									return err
								}
								if err := c.markOwnedVolumeDeleteAttempt(it); err != nil {
									cancel()
									return err
								}
								freshVolumeAttempt = true
							}
						}
					}
					if fixtureReadOnly {
						meta, entry, _ := c.journalValues(it)
						switch it.volumeFixtureKind {
						case "role":
							res, err = journalfile.VolumeFixtureRoleAbsent(commandCtx, bound, entry, meta, c.runCommand)
						case "bucket":
							res, err = journalfile.VolumeFixtureBucketAbsent(commandCtx, bound, entry, meta, c.runCommand)
						case "policy":
							res, err = journalfile.VolumeFixturePolicyAbsent(commandCtx, bound, entry, meta, c.runCommand)
						case "attachment":
							var current report.CleanupJournal
							current, err = c.currentVolumeFixtureJournal(it)
							if err == nil {
								res, err = journalfile.VolumeFixtureAttachmentAbsent(commandCtx, bound, entry, current, c.runCommand)
							}
						}
						if err != nil {
							cancel()
							return err
						}
					} else if volumeTeamAlreadyAbsent {
						res = execpkg.Result{Started: true, Exit: exitNotFound, JSON: map[string]any{"deleted": true}}
					} else if !volumeReadOnly {
						if it.volumeTeamBound && teamDelete {
							if err := journalfile.ReserveVolumeTeamDelete(commandCtx, volumeTeamPath, volumeTeamJournal, volumeTeamEntry, bound); err != nil {
								cancel()
								return err
							}
						}
						res = c.runCommand(commandCtx, bound, command)
						if it.volumeTeamBound && teamDelete && journalfile.IsVolumeTeamChildConflict(res) {
							if err := journalfile.RecordVolumeTeamConflict(commandCtx, volumeTeamPath, volumeTeamJournal, volumeTeamEntry, bound, res); err != nil {
								cancel()
								return err
							}
							retry, err := journalfile.RetryVolumeTeamDeleteAfterConflict(commandCtx, volumeTeamPath, volumeTeamJournal, volumeTeamEntry, bound, c.runCommand)
							if err != nil {
								cancel()
								return err
							}
							res = retry
						}
					}
					if freshVolumeAttempt && !res.Started && res.Exit == -1 && res.Err != nil {
						cancel()
						if err := c.restoreUnstartedOwnedVolumeDeleteAttempt(it); err != nil {
							return fmt.Errorf("restore unused Volume delete marker: %w", err)
						}
						return fmt.Errorf("Volume delete did not start: %w", res.Err)
					}
					if freshVolumeAttempt && journalfile.VolumeDefinitiveDeleteRejection(res) {
						if err := c.restoreRejectedOwnedVolumeDeleteAttempt(commandCtx, it, bound); err != nil {
							cancel()
							return fmt.Errorf("retain rejected Volume delete marker: %w", err)
						}
						cancel()
						return fmt.Errorf("Volume delete was definitively rejected; retry is available")
					}
					if freshFixtureAttempt && !res.Started && res.Err != nil && res.Exit == -1 {
						cancel()
						if err := c.restoreUnstartedVolumeFixtureDeleteAttempt(it); err != nil {
							return fmt.Errorf("restore unused Volume fixture delete marker: %w", err)
						}
						return fmt.Errorf("Volume fixture delete did not start: %w", res.Err)
					}
					if freshFixtureAttempt && it.volumeFixtureKind == "attachment" {
						current, readErr := c.currentVolumeFixtureJournal(it)
						if readErr != nil {
							cancel()
							return readErr
						}
						_, entry, _ := c.journalValues(it)
						res, err = journalfile.VolumeFixtureAttachmentAbsent(commandCtx, bound, entry, current, c.runCommand)
						if err != nil {
							cancel()
							return err
						}
					}
					if freshFixtureAttempt && it.volumeFixtureKind != "attachment" {
						meta, entry, _ := c.journalValues(it)
						if err := journalfile.VolumeFixtureDeleteConfirmed(entry, meta, bound, res); err != nil {
							cancel()
							return err
						}
						proofCtx, cancelProof := context.WithTimeout(context.Background(), 30*time.Second)
						res, err = journalfile.VolumeFixtureTerminalAbsent(proofCtx, bound, entry, meta, c.runCommand)
						cancelProof()
						if err != nil {
							cancel()
							return err
						}
					}
					// A refused guard/lock or a provably unstarted subprocess
					// makes no remote attempt; uncertain markers stay fenced.
					c.mu.Lock()
					it.done = res.Err == nil && res.Exit >= 0 && !quotaDelete && !it.teamCreateConfirmed && !volumeDelete && !fencedFixture
					c.mu.Unlock()
					for attempt, delay := range cleanupRetryDelays {
						if fencedFixture || volumeNative || it.volumeTeamBound && teamDelete || res.Exit == 0 || res.Exit == exitNotFound || !cleanupRetryable(res) {
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
							if quotaDelete || teamDelete && (it.quotaBound || it.volumeTeamBound) {
								// The outer cleanup fence is still held. A retry launches a
								// fresh credential provider, so check the original binding
								// again without recursively acquiring that fence.
								var verified execpkg.Config
								var err error
								if it.volumeTeamBound {
									_, _, path := c.journalValues(it)
									journal, legacy, readErr := readRunnerJournal(path)
									if readErr != nil || legacy {
										cancel()
										return fmt.Errorf("Volume Team retry journal is unavailable: %v", readErr)
									}
									var found bool
									verified, found, err = journalfile.VolumeTeamCreatorConfig(commandCtx, journal, c.execCfg[it.role], it.ownedTeamID)
									if !found && err == nil {
										err = fmt.Errorf("Volume Team retry lost its creator binding")
									}
								} else {
									c.mu.Lock()
									binding := c.quotaBinding
									c.mu.Unlock()
									if binding == nil {
										cancel()
										return fmt.Errorf("protected Quota retry lacks its original credential binding")
									}
									verified, err = journalfile.ProbeQuotaBinding(commandCtx, binding, c.execCfg[it.role])
								}
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
					if volumeDelete && it.templateBinding != nil && it.templateBinding.Resource == "volume" {
						request, parseErr := journalfile.ParseVolumeDeleteRequest(it.cmd)
						teams, scopeErr := c.ownedVolumeDeleteScopes(it)
						probeCtx, cancelProbe := context.WithTimeout(context.Background(), 45*time.Second)
						absent := parseErr == nil && scopeErr == nil && journalfile.WaitVolumeAbsent(probeCtx, bound, request.ID, it.templateBinding.Name, it.templateBinding.Region, it.templateBinding.Profile, teams, c.runCommand)
						cancelProbe()
						if !absent {
							cancel()
							return fmt.Errorf("owned Volume absence is unproven after cleanup delete")
						}
						if res.Exit != 0 {
							res.Exit = exitNotFound
						}
					}
					var teamReadback execpkg.Result
					if teamDelete && it.volumeTeamBound && (res.Exit == 0 || res.Exit == exitNotFound) {
						proofCtx, cancelProof := context.WithTimeout(context.Background(), 30*time.Second)
						teamReadback, err = c.ownedTeamAbsence(proofCtx, it)
						cancelProof()
						if err != nil {
							cancel()
							return fmt.Errorf("owned Volume Team absence is unproven: %w", err)
						}
					}
					cancel()
					if res.Exit == 0 || res.Exit == exitNotFound {
						if quotaDelete {
							journalErr = c.consumeOwnedQuotaDelete(scope, it.cmd, it.role)
						} else if it.teamCreateConfirmed {
							if it.volumeTeamBound {
								journalErr = c.consumeOwnedTeamDelete(it, res, teamReadback)
							} else {
								journalErr = c.consumeOwnedTeamDelete(it, res)
							}
						} else if volumeDelete {
							journalErr = c.consumeOwnedVolumeDelete(it)
						} else {
							journalErr = c.completeJournalItem(journalItem)
							if journalErr == nil && fencedFixture {
								c.mu.Lock()
								it.done = true
								c.mu.Unlock()
							}
						}
					}
					return nil
				}, it.cmd)
			}
			if quotaDelete || teamDelete || volumeDelete {
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
			if !deferPending && len(pendingWait) == 1 && pendingWait[0] != nil && !res.Started && errors.Is(coordinationErr, ErrTeamVolumeChildPending) {
				*pendingWait[0] = true
				continue
			}
			if deferPending && !res.Started && (errors.Is(coordinationErr, journalfile.ErrTemplateCreatePending) || errors.Is(coordinationErr, journalfile.ErrTemplateCacheCreatePending) || errors.Is(coordinationErr, journalfile.ErrQuotaUpdatePending) || errors.Is(coordinationErr, ErrTeamCreatePending) || errors.Is(coordinationErr, ErrTeamRegistrationPending) || errors.Is(coordinationErr, ErrTeamVolumeChildPending) || errors.Is(coordinationErr, context.DeadlineExceeded)) {
				// Store the unvisited subset in registration order. The deferred
				// pass applies Volume-first reverse order again when it retries.
				pending := make(map[*cleanupItem]bool, len(ordered)-i)
				for _, item := range ordered[i:] {
					pending[item] = true
				}
				remaining := make([]*cleanupItem, 0, len(pending))
				for _, item := range scope {
					if pending[item] {
						remaining = append(remaining, item)
					}
				}
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
				c.mu.Lock()
				it.done = false
				c.mu.Unlock()
				if !volumeChildDelete {
					// The remaining entries may be this cache's Template and Team.
					break
				}
				volumeChildFailed = true
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
			if !volumeChildDelete {
				break
			}
			volumeChildFailed = true
		}
	}
	return failures
}

func volumeFirstCleanupOrder(scope []*cleanupItem) []*cleanupItem {
	volumes := make([]*cleanupItem, 0, len(scope))
	others := make([]*cleanupItem, 0, len(scope))
	buckets := make([]*cleanupItem, 0, 1)
	ramFixtures := make([]*cleanupItem, 0, 3)
	for i := len(scope) - 1; i >= 0; i-- {
		item := scope[i]
		volume, err := classifyVolumeFinalizer(item.cmd)
		switch {
		case volume || err != nil || item.templateBinding != nil && item.templateBinding.Resource == "volume":
			volumes = append(volumes, item)
		case item.volumeFixtureKind == "bucket":
			buckets = append(buckets, item)
		case item.volumeFixtureKind == "attachment" || item.volumeFixtureKind == "policy" || item.volumeFixtureKind == "role":
			ramFixtures = append(ramFixtures, item)
		default:
			others = append(others, item)
		}
	}
	return append(append(append(volumes, others...), buckets...), ramFixtures...)
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
