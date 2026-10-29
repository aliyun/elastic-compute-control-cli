package runner

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
)

var writeQuotaReservationJournal = writeRunnerJournal
var writeQuotaSettlementJournal = writeRunnerJournal
var writeQuotaConsumptionJournal = writeRunnerJournal
var quotaBeforeReplayLock = func() {}

// A Quota unit may return to the public CLI while cleanup remains uncertain.
// Successful journal consumption is the only path that marks these items done.
func (c *cleanup) hasRetainedQuotaOwnership() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unresolvedQuotaTeamCreate {
		return true
	}
	// A confirmed bound parent can survive a failure before the first child
	// reservation. It still belongs to this execution until cleanup consumes it.
	for _, item := range c.ownedTeams {
		if !item.done && item.teamCreateConfirmed && item.quotaBound {
			return true
		}
	}
	// Suite labels and covers alone do not establish ownership.
	if len(c.quotaReservations) == 0 {
		return false
	}
	for _, item := range c.ownedTeams {
		if !item.done {
			return true
		}
	}
	for _, item := range c.quotaReservations {
		if !item.done {
			return true
		}
	}
	return false
}

func (c *cleanup) hasUnresolvedQuotaTeamCreate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unresolvedQuotaTeamCreate
}

// A failed process start is a permanent fact, even if retiring its pending
// journal child fails and must be retried by automatic cleanup.
func (c *cleanup) recordUnstartedQuotaUpdate(item *cleanupItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item != nil && item.quotaUpdatePending && !item.done {
		item.quotaReservationUnstarted = true
	}
}

func quotaUpdateCandidate(_ scenario.Step, command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && (path[0] == "sandbox" || path[0] == "sbx") && path[1] == "quota" && path[2] == "update"
	}
	// A command-path error inside this resource family is a rejection
	// candidate, never evidence that a Quota mutation is an ordinary step.
	return unclassifiedQuotaFamily(command)
}

func quotaDeleteCandidate(command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && (path[0] == "sandbox" || path[0] == "sbx") && path[1] == "quota" && (path[2] == "delete" || path[2] == "rm" || path[2] == "remove")
	}
	return unclassifiedQuotaFamily(command)
}

func (c *cleanup) quotaDeleteItem(scope []*cleanupItem, data map[string]any, command, role string) (*cleanupItem, error) {
	if !journalfile.IsSandboxQuotaDelete(command) {
		return nil, fmt.Errorf("Quota delete requires the exact owned finalizer command")
	}
	args, _ := shlex.Split(command)
	if data != nil {
		id, ok := data["team_id"].(string)
		if !ok || id != args[4] {
			return nil, fmt.Errorf("Quota delete target differs from captured Team ID")
		}
	}
	var child, parent *cleanupItem
	for _, item := range scope {
		if item.done || item.role != role || item.scope == "" {
			continue
		}
		if item.cmd == command {
			if child != nil {
				return nil, fmt.Errorf("ambiguous in-memory Quota reservation")
			}
			child = item
		}
		if teamID, err := journalfile.TeamDeleteTarget(item.cmd); err == nil && item.ownedTeamID == args[4] && teamID == args[4] {
			if parent != nil {
				return nil, fmt.Errorf("ambiguous in-memory Team ownership")
			}
			parent = item
		}
	}
	if child == nil || parent == nil || !child.quotaReserved || !parent.teamCreateConfirmed || child.scope != parent.scope || child.quotaUpdatePending || parent.teamReplayAttempted {
		return nil, fmt.Errorf("Quota delete lacks a settled run-owned reservation")
	}
	return child, nil
}

func (c *cleanup) verifyOwnedQuotaDelete(scope []*cleanupItem, data map[string]any, command, role string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, err := c.quotaDeleteItem(scope, data, command, role)
	if err != nil {
		return err
	}
	return c.withOwnedQuotaJournal(item, nil)
}

func (c *cleanup) consumeOwnedQuotaDelete(scope []*cleanupItem, command, role string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, err := c.quotaDeleteItem(scope, nil, command, role)
	if err != nil {
		return err
	}
	if err := c.withOwnedQuotaJournal(item, func(path string, journal *report.CleanupJournal, entry report.Resource) error {
		for index, current := range journal.Entries {
			if current == entry {
				journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
				return writeQuotaConsumptionJournal(path, *journal)
			}
		}
		return fmt.Errorf("settled Quota reservation disappeared")
	}); err != nil {
		return err
	}
	item.done = true
	return nil
}

func (c *cleanup) withOwnedQuotaJournal(item *cleanupItem, consume func(string, *report.CleanupJournal, report.Resource) error) error {
	if c.journal == "" {
		return fmt.Errorf("Quota delete requires a run-bound cleanup journal")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, entry, _ := c.journalValues(item)
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		if _, err := c.checkedJournalParentLocked(path); err != nil {
			return err
		}
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("Quota delete journal binding changed")
		}
		if c.quotaLifecycleRequired && template.QuotaBinding == nil || template.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, template.QuotaBinding) {
			return fmt.Errorf("Quota delete credential binding changed")
		}
		if err := journalfile.CheckOwnedQuotaDelete(journal, entry); err != nil {
			return err
		}
		if consume != nil {
			return consume(path, &journal, entry)
		}
		return nil
	})
}

func unclassifiedQuotaFamily(command string) bool {
	return unclassifiedSandboxFamily(command, "quota")
}

func unclassifiedSandboxFamily(command, resource string) bool {
	args, err := shlex.Split(command)
	if err != nil {
		// Malformed quoting is still rejected before invocation when the
		// visible command path identifies the Quota family.
		args = strings.Fields(command)
	}
	if len(args) == 0 || args[0] != "ecctl" {
		return false
	}
	flags := commandPathFlags()
	product := ""
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
				return sandboxFamilyTail(args[index+1:], product, resource)
			}
			if !inline && flag.NoOptDefVal == "" {
				index++
			}
			continue
		}
		if product == "" {
			if arg != "sandbox" && arg != "sbx" {
				return false
			}
			product = arg
			continue
		}
		return arg == resource
	}
	return false
}

func sandboxFamilyTail(args []string, product, resource string) bool {
	for _, arg := range args {
		if product == "" && (arg == "sandbox" || arg == "sbx") {
			product = arg
			continue
		}
		if product == "sandbox" || product == "sbx" {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			return arg == resource
		}
	}
	return false
}

// reserveOwnedQuotaUpdate places the Quota delete after the already confirmed
// Team delete in the case scope, before UpdateQuota can change cloud state.
// Caller holds the resolved journal's exclusive replay lock until UpdateQuota
// finishes and its successful result is durably settled.
func (c *cleanup) reserveOwnedQuotaUpdate(ctx context.Context, journalPath string, scope *[]*cleanupItem, data map[string]any, step scenario.Step, command string, cfg execpkg.Config, lockKeys []string) (*cleanupItem, error) {
	path, err := commandAction(command)
	if err != nil {
		return nil, fmt.Errorf("Quota update command cannot be classified: %w", err)
	}
	if len(path) != 3 || path[0] != "sandbox" || path[1] != "quota" || path[2] != "update" {
		return nil, fmt.Errorf("Quota update requires the canonical owned sandbox quota update command")
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) != 10 || args[0] != "ecctl" || args[1] != "sandbox" || args[2] != "quota" || args[3] != "update" || args[5] != "--cpu-cores" || args[7] != "--memory-gb" || args[9] != "--output=json" {
		return nil, fmt.Errorf("Quota update command is outside the owned lifecycle grammar")
	}
	id, ok := data["team_id"].(string)
	if !ok || !safeTeamIdentity(id) || args[4] != id {
		return nil, fmt.Errorf("Quota update requires the captured owned Team ID")
	}
	if step.TeardownRegion != "" && step.TeardownRegion != "primary" {
		return nil, fmt.Errorf("Quota cleanup must use the primary Team region")
	}
	deleteCommand, err := vars.Render(step.Teardown, data)
	if err != nil || deleteCommand != "ecctl sandbox quota delete "+id+" --output=json" || !isReplayableTeardown(deleteCommand) || !journalfile.IsSandboxQuotaDelete(deleteCommand) {
		return nil, fmt.Errorf("Quota cleanup requires an exact replayable Team-ID delete")
	}
	keys, err := normalizeLockKeys(lockKeys)
	if err != nil {
		return nil, fmt.Errorf("Quota cleanup lock keys: %w", err)
	}
	bound, exists := c.execCfg["primary"]
	if !exists || c.journal == "" || c.journalMeta.RunID == "" || c.journalMeta.ExecutionID == "" || !scenario.Surface(c.journalMeta.Surface).Valid() || cfg.Bin == "" || cfg.Region == "" || bound.Bin != cfg.Bin || bound.Region != cfg.Region {
		return nil, fmt.Errorf("Quota cleanup requires a run-bound primary journal, binary, surface and region")
	}

	item := &cleanupItem{scope: caseScope(data), cmd: deleteCommand, role: "primary", lockKeys: keys, quotaReserved: true, quotaUpdatePending: true}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quotaLifecycleRequired && c.quotaBinding == nil {
		return nil, fmt.Errorf("Quota update lacks its Team creation credential binding")
	}
	for _, existing := range *scope {
		if existing.quotaReserved && !existing.done && existing.role == "primary" && existing.cmd == deleteCommand {
			return nil, fmt.Errorf("Quota cleanup has an unfinished in-memory reservation for this Team")
		}
	}
	var team *cleanupItem
	for _, candidate := range *scope {
		if candidate.done || candidate.role != "primary" || candidate.scope != item.scope || candidate.ownedTeamID != id || !candidate.teamCreateConfirmed || candidate.teamReplayAttempted {
			continue
		}
		teamID, err := journalfile.TeamDeleteTarget(candidate.cmd)
		if err != nil || teamID != id || team != nil {
			return nil, fmt.Errorf("Quota cleanup has ambiguous owned Team evidence")
		}
		team = candidate
	}
	if team == nil {
		return nil, fmt.Errorf("Quota update has no confirmed run-owned Team")
	}
	var priorTeam *cleanupItem
	for _, registered := range c.registeredItems {
		if registered == team || registered.done {
			continue
		}
		if priorTeam != nil || registered.role != "primary" || registered.scope != item.scope ||
			!registered.teamCreateConfirmed || !registered.quotaBound || registered.teamReplayAttempted ||
			registered.ownedTeamID == id {
			return nil, fmt.Errorf("Quota update has an unrelated active cleanup entry")
		}
		priorTeam = registered
	}
	var priorQuota *cleanupItem
	for _, reserved := range c.quotaReservations {
		if reserved.done {
			continue
		}
		if priorQuota != nil || priorTeam == nil || reserved.role != "primary" || reserved.scope != item.scope ||
			!reserved.quotaReserved || reserved.quotaUpdatePending ||
			reserved.cmd != "ecctl sandbox quota delete "+priorTeam.ownedTeamID+" --output=json" {
			return nil, fmt.Errorf("Quota update has an unsettled or unrelated reservation")
		}
		priorQuota = reserved
	}
	if (priorTeam == nil) != (priorQuota == nil) {
		return nil, fmt.Errorf("Quota update requires a settled prior Team and Quota pair")
	}
	template, entry, _ := c.journalValues(item)
	resolved, err := c.checkedJournalPath(item.role)
	if err != nil || resolved != journalPath {
		return nil, fmt.Errorf("Quota cleanup journal target changed")
	}
	_, teamEntry, _ := c.journalValues(team)
	var priorTeamEntry, priorQuotaEntry report.Resource
	if priorTeam != nil {
		_, priorTeamEntry, _ = c.journalValues(priorTeam)
		_, priorQuotaEntry, _ = c.journalValues(priorQuota)
	}
	writeAttempted := false
	err = journalfile.WithExistingParentLock(ctx, journalPath, func() error {
		if _, err := c.checkedJournalParentLocked(journalPath); err != nil {
			return err
		}
		journal, legacy, err := readRunnerJournal(journalPath)
		if err != nil {
			return err
		}
		if legacy || journal.Version != 2 || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("Quota cleanup journal metadata differs from its owned Team")
		}
		if c.quotaLifecycleRequired && template.QuotaBinding == nil || template.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, template.QuotaBinding) {
			return fmt.Errorf("Quota cleanup credential binding changed")
		}
		if err := journalfile.CheckTemplateCreateIntents(journalPath, journal, cfg.Env); err != nil {
			return err
		}
		teamCount, priorTeamCount, priorQuotaCount := 0, 0, 0
		for _, existing := range journal.Entries {
			if existing == teamEntry {
				teamCount++
			}
			if priorTeam != nil && existing == priorTeamEntry {
				priorTeamCount++
			}
			if priorQuota != nil && existing == priorQuotaEntry {
				priorQuotaCount++
			}
			if existing.Teardown == entry.Teardown {
				return fmt.Errorf("Quota cleanup reservation already exists")
			}
		}
		wantEntries := 1
		if priorTeam != nil {
			wantEntries = 3
			if priorTeamCount != 1 || priorQuotaCount != 1 || journalfile.CheckOwnedQuotaDelete(journal, priorQuotaEntry) != nil {
				return fmt.Errorf("Quota update lacks a settled durable prior Team and Quota pair")
			}
		}
		if teamCount != 1 || len(journal.Entries) != wantEntries {
			return fmt.Errorf("Quota update has unexpected active cleanup entries")
		}
		journal.Entries = append(journal.Entries, entry)
		writeAttempted = true
		return writeQuotaReservationJournal(journalPath, journal)
	})
	if err != nil && !writeAttempted {
		return nil, err
	}
	if err != nil {
		item.quotaReservationUnstarted = true
	}
	*scope = append(*scope, item)
	c.quotaReservations = append(c.quotaReservations, item)
	c.manifest = append(c.manifest, report.Resource{Scope: item.scope, Teardown: item.cmd, RegionRole: item.role, Region: cfg.Region, ExecutionID: c.journalMeta.ExecutionID})
	// A durability error after rename is uncertain. UpdateQuota has not
	// started. Keep the exact Quota item before Team in in-process cleanup,
	// and retain any installed journal entry for crash recovery.
	return item, err
}

// settleOwnedQuotaUpdate is called only for a confirmed successful command,
// while the exclusive replay lock still excludes every cleanup consumer.
func (c *cleanup) settleOwnedQuotaUpdate(item *cleanupItem) error {
	return c.finishOwnedQuotaUpdate(item, false)
}

// A proven subprocess non-start leaves no Quota mutation to reconcile. Retire
// its exact child while replay is still excluded, so the Team can be cleaned.
func (c *cleanup) retireUnstartedQuotaUpdate(item *cleanupItem) error {
	return c.finishOwnedQuotaUpdate(item, true)
}

func (c *cleanup) finishOwnedQuotaUpdate(item *cleanupItem, unstarted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !item.quotaUpdatePending || item.done {
		return fmt.Errorf("Quota update has no pending reservation")
	}
	template, entry, _ := c.journalValues(item)
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		if _, err := c.checkedJournalParentLocked(path); err != nil {
			return err
		}
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != 2 || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("Quota settlement journal binding changed")
		}
		if c.quotaLifecycleRequired && template.QuotaBinding == nil || template.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, template.QuotaBinding) {
			return fmt.Errorf("Quota settlement credential binding changed")
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate == entry {
				if index >= 0 {
					return fmt.Errorf("ambiguous pending Quota reservation")
				}
				index = i
			}
		}
		if index < 0 {
			if unstarted && item.quotaReservationUnstarted {
				for _, candidate := range journal.Entries {
					if candidate.Teardown == entry.Teardown {
						return fmt.Errorf("conflicting Quota reservation remains")
					}
				}
				// A prior rename may have installed a removal before its durability
				// barrier failed. Re-establish that barrier before retiring memory.
				return writeQuotaSettlementJournal(path, journal)
			}
			return fmt.Errorf("pending Quota reservation disappeared")
		}
		if unstarted {
			journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		} else {
			journal.Entries[index].QuotaUpdatePending = false
		}
		return writeQuotaSettlementJournal(path, journal)
	})
	if err == nil {
		item.quotaUpdatePending = false
		item.done = unstarted
	}
	return err
}
