package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var ErrTeamCreatePending = errors.New("protected Team creation pending")
var ErrTeamRegistrationPending = errors.New("confirmed Team registration pending")
var ErrTeamVolumeChildPending = errors.New("owned Team Volume child pending")

// Injectable at the durable attempt write for post-rename failure tests.
var writeTeamAttemptJournalRoot = writeRunnerJournalRoot

// Injectable only for the never-started retirement retry barrier.
var reaffirmVolumeTeamRetirementJournal = reaffirmRunnerJournal

func ownedTeamDeleteCandidate(command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && ((path[0] == "sandbox" || path[0] == "sbx") && path[1] == "team" && (path[2] == "delete" || path[2] == "rm" || path[2] == "remove") || path[0] == "call" && strings.EqualFold(strings.TrimSpace(path[1]), "fcsandbox") && strings.EqualFold(strings.TrimSpace(path[2]), "DeleteTeam"))
	}
	if unclassifiedSandboxFamily(command, "team") {
		return true
	}
	args, splitErr := shlex.Split(command)
	if splitErr != nil {
		args = strings.Fields(command)
	}
	return possibleRawTeamDelete(args)
}

// An unknown flag must not turn a possible raw DeleteTeam into an ordinary
// call. The strict target parser still decides whether it can be invoked.
func possibleRawTeamDelete(args []string) bool {
	if len(args) < 2 {
		return false
	}
	part := 0
	for _, arg := range args[1:] {
		switch part {
		case 0:
			if arg == "call" {
				part++
			}
		case 1:
			if strings.EqualFold(strings.TrimSpace(arg), "fcsandbox") {
				part++
			}
		case 2:
			if strings.EqualFold(strings.TrimSpace(arg), "DeleteTeam") {
				return true
			}
		}
	}
	return false
}

// The generic raw caller intentionally returns business errors in response.
// This owned-delete path must confirm request acceptance independently of the
// subprocess exit code. Acceptance does not establish Team absence.
func acceptedRawTeamDelete(result execpkg.Result, region string) error {
	root, ok := result.JSON.(map[string]any)
	if !ok || root["product"] != "fcsandbox" || root["operation"] != "DeleteTeam" || root["region"] != region {
		return fmt.Errorf("raw Team delete lacks a matching response envelope")
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		return fmt.Errorf("raw Team delete lacks a response object")
	}
	if requestID, present := response["requestId"]; present {
		if _, ok := requestID.(string); !ok {
			return fmt.Errorf("raw Team delete has an invalid response request ID")
		}
	}
	if code, ok := response["code"].(string); !ok || code != "200" {
		return fmt.Errorf("raw Team delete did not confirm business success")
	}
	return nil
}

func ownedTeamClaimKey(command, region string) string {
	id, err := teamDeleteTargetForOwnership(command, region)
	if err != nil {
		return command
	}
	return "ecctl sandbox team delete " + id
}

// Ordinary Team deletes may omit the protected finalizer's timeout or use an
// action alias. Normalize only for target classification; ownership itself
// still requires the original command to pass TeamDeleteTarget.
func teamDeleteTargetForOwnership(command, region string) (string, error) {
	if rawTeamDeleteCandidate(command) {
		return journalfile.RawTeamDeleteTarget(command, region)
	}
	return journalfile.TeamDeleteCandidateTargetForRegion(command, region)
}

// The shared replay fence is held. A first registration write may have failed
// after provider creation, leaving only the pre-create journal and its binding.
// Install that exact confirmed finalizer before the guard selects credentials.
func (c *cleanup) repairTeamBeforeCleanupBinding(role, command string) (bool, error) {
	target, err := teamDeleteTargetForOwnership(command, c.execCfg[role].Region)
	if err != nil {
		return false, nil // Other commands retain their normal validation path.
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.teamCreatesPending > 0 {
		return false, fmt.Errorf("Team delete must wait for protected Team creation: %w", ErrTeamCreatePending)
	}
	var owned *cleanupItem
	for _, item := range c.ownedTeams {
		if item.role != role || !item.teamCreateConfirmed || item.ownedTeamID != target || item.done {
			continue
		}
		if owned != nil {
			return false, fmt.Errorf("owned Team cleanup is ambiguous")
		}
		owned = item
	}
	if owned == nil || !owned.teamRegistrationFailed {
		return false, nil
	}
	if err := c.repairTeamRegistration(owned); err != nil {
		return false, err
	}
	return true, nil
}

func rawTeamDeleteCandidate(command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && path[0] == "call" && strings.EqualFold(strings.TrimSpace(path[1]), "fcsandbox") && strings.EqualFold(strings.TrimSpace(path[2]), "DeleteTeam")
	}
	args, splitErr := shlex.Split(command)
	if splitErr != nil {
		args = strings.Fields(command)
	}
	return possibleRawTeamDelete(args)
}

// Caller holds c.mu. The per-role journal is shared by every case in this
// execution, including cases whose in-memory cleanup scope has already run.
// Only a first ordinary registration may create a journal parent; owned
// deletion and every later lookup require the directory already observed.
func (c *cleanup) retainedTeamParent(role, target string, allowInitialParent bool) (report.Resource, bool, error) {
	if c.journal == "" {
		return report.Resource{}, false, nil
	}
	var owner report.Resource
	found := false
	for _, currentRole := range c.coLocatedTeamRoles(role) {
		if _, err := c.checkedJournalPath(currentRole); err != nil {
			return report.Resource{}, false, err
		}
		template, _, path := c.journalValues(&cleanupItem{role: currentRole})
		lock := journalfile.WithExistingParentLock
		firstForRole := allowInitialParent && c.journalParents[path] == nil && c.nativeJournals[currentRole].NativeJournalPath == ""
		for _, item := range c.registeredItems {
			if item.role == currentRole {
				firstForRole = false
			}
		}
		for _, item := range c.quotaReservations {
			if item.role == currentRole {
				firstForRole = false
			}
		}
		if firstForRole {
			lock = journalfile.WithLock
		}
		var parent os.FileInfo
		journalPresent := false
		err := lock(context.Background(), path, func() error {
			var parentErr error
			parent, parentErr = c.checkedJournalParentLocked(path)
			if parentErr != nil {
				return parentErr
			}
			journal, legacy, err := readRunnerJournal(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
				return fmt.Errorf("Team ownership journal binding changed")
			}
			journalPresent = true
			for _, entry := range journal.Entries {
				if journalfile.IsQuotaCommandCandidate(entry.Teardown) {
					childID, parseErr := journalfile.SandboxQuotaDeleteTarget(entry.Teardown)
					if parseErr != nil || target == "" || childID == target {
						return fmt.Errorf("Team delete must wait for its Quota child")
					}
				}
				if !entry.TeamCreateConfirmed {
					continue
				}
				id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
				if parseErr != nil || entry.RegionRole != currentRole || entry.Region != template.Region || entry.ExecutionID != template.ExecutionID {
					return fmt.Errorf("invalid confirmed Team ownership in journal")
				}
				if target == "" || id == target {
					if found {
						return fmt.Errorf("ambiguous retained Team ownership")
					}
					owner, found = entry, true
				}
			}
			return nil
		})
		if err != nil {
			return report.Resource{}, false, err
		}
		c.rememberJournalParentLocked(path, parent)
		if journalPresent {
			c.rememberEstablishedJournalLocked(path)
		}
	}
	return owner, found, nil
}

// prepareOwnedTeamDelete runs under the shared replay fence. It records the
// attempt before a Team subprocess can invalidate a later Quota reservation.
func (c *cleanup) prepareOwnedTeamDelete(scope []*cleanupItem, command, role string, expected *cleanupItem, caseScopes ...string) (*cleanupItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	caseScope := ""
	if len(caseScopes) != 0 {
		caseScope = caseScopes[0]
		if caseScope == "" || strings.ContainsAny(caseScope, "\r\n") {
			return nil, fmt.Errorf("Volume Team deletion lacks a stable case scope")
		}
	}
	if c.teamCreatesPending > 0 {
		return nil, fmt.Errorf("Team delete must wait for protected Team creation: %w", ErrTeamCreatePending)
	}
	target, targetErr := teamDeleteTargetForOwnership(command, c.execCfg[role].Region)
	if targetErr != nil {
		return nil, fmt.Errorf("Team delete requires a validated target and region: %w", targetErr)
	}
	var owned *cleanupItem
	for _, item := range c.ownedTeams {
		if !c.teamTargetsMayOverlap(role, item.role) || !item.teamCreateConfirmed || item.ownedTeamID == "" {
			continue
		}
		if item.ownedTeamID == target {
			if item.role != role {
				return nil, fmt.Errorf("Team delete target belongs to protected Team in a co-located region role")
			}
			if owned != nil || item.done {
				return nil, fmt.Errorf("owned Team delete is ambiguous or already consumed")
			}
			owned = item
		}
	}
	parent, retained, err := c.retainedTeamParent(role, target, false)
	if err != nil {
		return nil, err
	}
	if c.journal != "" {
		_, _, path := c.journalValues(&cleanupItem{role: role})
		current, legacy, readErr := readRunnerJournal(path)
		if readErr != nil && !(os.IsNotExist(readErr) && !retained) {
			return nil, readErr
		}
		if readErr == nil && !legacy && len(current.NativeIntentPaths) > 0 {
			finalizer := command
			if owned != nil {
				finalizer = owned.cmd
			}
			if err := journalfile.CheckNativeCleanupDependencies(path, current, c.execCfg[role].Env, finalizer); err != nil {
				return nil, err
			}
		}
	}
	if owned == nil {
		if caseScope != "" {
			return nil, fmt.Errorf("Volume case Team delete lacks an active case-owned creation")
		}
		if retained {
			return nil, fmt.Errorf("Team delete target belongs to a retained Team in another scope")
		}
		return nil, nil
	}
	withinScope := false
	for _, item := range scope {
		if item == owned {
			withinScope = true
			break
		}
	}
	if !withinScope {
		return nil, fmt.Errorf("Team delete target belongs to a retained Team in another scope")
	}
	if caseScope != "" && (owned.scope != caseScope || !owned.volumeTeamBound || !reflect.DeepEqual(owned.volumeTeamBinding, c.volumeFixtureBinding)) {
		return nil, fmt.Errorf("Volume case Team delete differs from its owned creator")
	}
	if expected != nil && owned != expected {
		return nil, fmt.Errorf("ordinary Team finalizer cannot consume retained Team ownership")
	}
	if owned.teamRegistrationFailed {
		if err := c.repairTeamRegistration(owned); err != nil {
			return nil, err
		}
		parent, retained, err = c.retainedTeamParent(role, target, false)
		if err != nil {
			return nil, err
		}
	}
	if !rawTeamDeleteCandidate(command) {
		if _, err := journalfile.TeamDeleteTarget(command); err != nil {
			return nil, fmt.Errorf("owned Team delete requires a validated finalizer: %w", err)
		}
	}
	_, entry, _ := c.journalValues(owned)
	if !retained || parent.Teardown != entry.Teardown || parent.Scope != entry.Scope || parent.RegionRole != entry.RegionRole || parent.Region != entry.Region || parent.ExecutionID != entry.ExecutionID || parent.QuotaBound != entry.QuotaBound || parent.VolumeTeamBound != entry.VolumeTeamBound {
		return nil, fmt.Errorf("Team delete lacks matching durable ownership")
	}
	if owned.volumeTeamBound && !c.testSyntheticVolumeReservation {
		journal, legacy, err := readRunnerJournal(c.journal)
		if err != nil || legacy || journalfile.CheckVolumeTeamCreationLink(journal, target, &parent) != nil ||
			!reflect.DeepEqual(journal.VolumeTeamCreators[target], owned.volumeTeamBinding) {
			return nil, fmt.Errorf("Volume Team delete lacks its pinned accepted creator: %v", err)
		}
	}
	if err := c.checkOwnedVolumeChildren(role); err != nil {
		return nil, err
	}
	if err := c.withOwnedTeamJournal(owned, true); err != nil {
		return nil, err
	}
	return owned, nil
}

func (c *cleanup) volumeTeamChildContext(item *cleanupItem) (string, report.CleanupJournal, report.Resource, error) {
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return "", report.CleanupJournal{}, report.Resource{}, err
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		return "", report.CleanupJournal{}, report.Resource{}, fmt.Errorf("Volume Team child journal is unavailable: %v", err)
	}
	index := -1
	for i, entry := range journal.Entries {
		if entry.Teardown == item.cmd && entry.Scope == item.scope && entry.VolumeTeamBound && entry.TeamCreateConfirmed {
			if index >= 0 {
				return "", report.CleanupJournal{}, report.Resource{}, fmt.Errorf("Volume Team child finalizer is ambiguous")
			}
			index = i
		}
	}
	if index < 0 {
		return "", report.CleanupJournal{}, report.Resource{}, fmt.Errorf("Volume Team child finalizer is missing")
	}
	return path, journal, journal.Entries[index], nil
}

// Caller holds c.mu and the replay fence. Only deletion paths use this
// conservative check: an uncertain Volume transfer can involve either of two
// Teams owned by this execution. Creation and read-only Team lookups stay open.
func (c *cleanup) checkOwnedVolumeChildren(role string) error {
	for _, currentRole := range c.coLocatedTeamRoles(role) {
		path, err := c.checkedJournalPath(currentRole)
		if err != nil {
			return err
		}
		err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
			journal, legacy, err := readRunnerJournal(path)
			if os.IsNotExist(err) {
				required := c.journalEstablished[path] || c.nativeJournals[currentRole].NativeJournalPath != ""
				for _, item := range c.registeredItems {
					required = required || item.role == currentRole
				}
				for _, item := range c.quotaReservations {
					required = required || item.role == currentRole
				}
				if required {
					return fmt.Errorf("owned Team journal disappeared before Volume child check")
				}
				return nil
			}
			if err != nil {
				return err
			}
			expected, _, _ := c.journalValues(&cleanupItem{role: currentRole})
			if legacy || journal.Version != expected.Version || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin {
				return fmt.Errorf("owned Team Volume child journal binding changed")
			}
			c.rememberEstablishedJournalLocked(path)
			outstanding, err := journalfile.OutstandingVolumeChild(path, journal, c.execCfg[currentRole].Env)
			if err != nil {
				return err
			}
			if outstanding {
				return fmt.Errorf("Team delete must wait for its Volume child: %w", ErrTeamVolumeChildPending)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Caller holds c.mu. A confirmed create can outlive a failed first journal
// append; reconcile that exact finalizer before allowing its first delete.
func (c *cleanup) repairTeamRegistration(item *cleanupItem) error {
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, expected, _ := c.journalValues(item)
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.JournalPath != path || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("confirmed Team registration journal binding changed")
		}
		count := 0
		for _, entry := range journal.Entries {
			if journalfile.IsTeamDeleteCandidate(entry.Teardown) && !entry.TeamCreateConfirmed {
				id, parseErr := journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, template.Region)
				if parseErr != nil || id == item.ownedTeamID {
					return fmt.Errorf("confirmed Team registration conflicts with ordinary ownership")
				}
			}
			if !entry.TeamCreateConfirmed {
				continue
			}
			id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
			if parseErr != nil {
				return fmt.Errorf("invalid confirmed Team ownership in journal: %w", parseErr)
			}
			if id == item.ownedTeamID {
				if entry.VolumeTransferIntent != "" {
					if journal.VolumeTeamBindings[id] != entry.VolumeTransferIntent {
						return fmt.Errorf("confirmed Team transfer binding is missing")
					}
					entry.VolumeTransferIntent = "" // The transfer association was installed after registration.
				}
				if entry != expected {
					return fmt.Errorf("confirmed Team registration conflicts with retained ownership")
				}
				count++
			}
		}
		if count > 1 {
			return fmt.Errorf("ambiguous confirmed Team registration")
		}
		if item.volumeTeamBound {
			if !reflect.DeepEqual(journal.VolumeFixtureBinding, item.volumeTeamBinding) {
				return fmt.Errorf("confirmed Volume Team creator differs from fixture credential")
			}
			prior, exists := journal.VolumeTeamCreators[item.ownedTeamID]
			if count == 1 && !reflect.DeepEqual(prior, item.volumeTeamBinding) || count == 0 && exists {
				return fmt.Errorf("confirmed Volume Team creator binding changed")
			}
			if journal.VolumeTeamCreators == nil {
				journal.VolumeTeamCreators = make(map[string]*report.QuotaCredentialBinding)
			}
			journal.VolumeTeamCreators[item.ownedTeamID] = item.volumeTeamBinding
		}
		if count == 0 {
			if journal.VolumeTeamBindings[item.ownedTeamID] != "" {
				return fmt.Errorf("confirmed Team transfer binding lost its finalizer")
			}
			journal.Entries = append(journal.Entries, expected)
		}
		if item.volumeTeamBound {
			if err := settleVolumeTeamRegistration(&journal, item.teamCreateIntent, item.ownedTeamID, path); err != nil {
				return err
			}
		}
		if err := writeTeamRegistrationJournal(path, journal); err != nil {
			return errors.Join(ErrTeamRegistrationPending, err)
		}
		installed, _, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if item.volumeTeamBound && !reflect.DeepEqual(installed.VolumeTeamCreators[item.ownedTeamID], item.volumeTeamBinding) {
			return fmt.Errorf("confirmed Volume Team creator binding was not installed")
		}
		if item.volumeTeamBound && installed.VolumeTeamCreateIDs[item.teamCreateIntent.Name] != item.ownedTeamID {
			return fmt.Errorf("confirmed Volume Team pending name was not settled")
		}
		if len(installed.Entries) != len(journal.Entries) {
			return fmt.Errorf("confirmed Team registration was not installed")
		}
		found := 0
		for _, entry := range installed.Entries {
			if entry.Teardown == expected.Teardown && entry.VolumeTransferIntent != "" && installed.VolumeTeamBindings[item.ownedTeamID] == entry.VolumeTransferIntent {
				entry.VolumeTransferIntent = ""
			}
			if entry == expected {
				found++
			}
		}
		if found != 1 {
			return fmt.Errorf("confirmed Team registration was not installed exactly once")
		}
		return nil
	})
	if err == nil {
		item.teamRegistrationFailed = false
	}
	return err
}

func (c *cleanup) consumeOwnedTeamDelete(item *cleanupItem, outcome execpkg.Result, readbacks ...execpkg.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item.done || !item.teamReplayAttempted {
		return fmt.Errorf("owned Team delete has no active attempt")
	}
	if err := c.withOwnedTeamJournal(item, false, append([]execpkg.Result{outcome}, readbacks...)...); err != nil {
		return err
	}
	item.done = true
	return nil
}

// Caller holds c.mu and the shared cleanup/replay fence.
func (c *cleanup) withOwnedTeamJournal(item *cleanupItem, mark bool, proofs ...execpkg.Result) error {
	teamID, teamErr := journalfile.TeamDeleteTarget(item.cmd)
	if c.journal == "" || !item.teamCreateConfirmed || item.ownedTeamID == "" || teamErr != nil || teamID != item.ownedTeamID {
		return fmt.Errorf("Team delete lacks confirmed creation ownership")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, expected, _ := c.journalValues(item)
	if err := c.checkOwnedVolumeChildren(item.role); err != nil {
		return err
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("owned Team journal binding changed")
		}
		outstanding, err := journalfile.OutstandingVolumeChild(path, journal, c.execCfg[item.role].Env)
		if err != nil {
			return err
		}
		if outstanding {
			return fmt.Errorf("Team delete must wait for its Volume child: %w", ErrTeamVolumeChildPending)
		}
		index := -1
		for i, entry := range journal.Entries {
			if journalfile.IsQuotaCommandCandidate(entry.Teardown) {
				childID, parseErr := journalfile.SandboxQuotaDeleteTarget(entry.Teardown)
				if parseErr != nil || childID == item.ownedTeamID {
					return fmt.Errorf("Team delete must wait for its Quota child")
				}
			}
			if entry.Teardown == expected.Teardown {
				if index >= 0 || !entry.TeamCreateConfirmed || entry.Scope != expected.Scope || entry.RegionRole != expected.RegionRole || entry.Region != expected.Region || entry.ExecutionID != expected.ExecutionID || entry.QuotaBound != expected.QuotaBound || entry.VolumeTeamBound != expected.VolumeTeamBound {
					return fmt.Errorf("ambiguous confirmed Team finalizer")
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("confirmed Team finalizer disappeared")
		}
		if journal.Entries[index].VolumeTeamBound {
			if err := journalfile.CheckVolumeTeamCreationLink(journal, teamID, &journal.Entries[index]); err != nil {
				return err
			}
		}
		if mark {
			if journal.Entries[index].TeamReplayAttempted {
				item.teamReplayAttempted = true
				if item.teamAttemptFresh {
					// A previous post-rename error did not launch the command.
					// Re-establish the exact marker's write barrier before launch.
					want, err := runnerJournalBytes(journal)
					if err != nil {
						return err
					}
					current, err := root.ReadFile(filepath.Base(path))
					if err != nil || !bytes.Equal(current, want) {
						return fmt.Errorf("owned Team attempt bytes changed before durability barrier: %v", err)
					}
					if err := writeTeamAttemptJournalRoot(root, path, journal); err != nil {
						return err
					}
					installed, legacy, err := readRunnerJournalRoot(root, path)
					current, readErr := root.ReadFile(filepath.Base(path))
					if err != nil || readErr != nil || legacy || !reflect.DeepEqual(installed, journal) || !bytes.Equal(current, want) {
						return fmt.Errorf("owned Team attempt changed after durability barrier: %v", err)
					}
				}
				return teamCompletionRootMatchesPath(root, path)
			}
			journal.Entries[index].TeamReplayAttempted = true
			if err := writeTeamAttemptJournalRoot(root, path, journal); err != nil {
				installed, legacy, readErr := readRunnerJournalRoot(root, path)
				want, encodeErr := runnerJournalBytes(journal)
				current, bytesErr := root.ReadFile(filepath.Base(path))
				if readErr == nil && encodeErr == nil && bytesErr == nil && !legacy && reflect.DeepEqual(installed, journal) && bytes.Equal(current, want) {
					item.teamReplayAttempted = true
					item.teamAttemptFresh = true
				}
				return err
			}
			item.teamReplayAttempted = true
			item.teamAttemptFresh = true
			return teamCompletionRootMatchesPath(root, path)
		}
		if !journal.Entries[index].TeamReplayAttempted {
			return fmt.Errorf("owned Team delete attempt was not recorded")
		}
		if journal.Entries[index].VolumeTeamBound {
			if len(proofs) != 2 {
				return fmt.Errorf("Volume Team deletion lacks its outcome and retained absence readback")
			}
			if err := teamCompletionRootMatchesPath(root, path); err != nil {
				return err
			}
			entry := journal.Entries[index]
			if err := journalfile.RecordTeamCompletion(root, path, journal, entry, proofs[1]); err != nil {
				return err
			}
			if err := journalfile.RecordHistoricalCompletionAt(root, path, &journal, index, entry, []byte(proofs[1].Stdout), "verified-absence", proofs[1]); err != nil {
				return err
			}
		} else if journal.Entries[index].QuotaBound && len(proofs) != 1 {
			return fmt.Errorf("Quota Team deletion lacks its invocation result")
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		body, err := runnerJournalBytes(journal)
		if err != nil {
			return err
		}
		if err := journalfile.WriteDurableRoot(root, filepath.Base(path), body); err != nil {
			return err
		}
		installed, err := root.ReadFile(filepath.Base(path))
		if err != nil || !bytes.Equal(installed, body) {
			return fmt.Errorf("protected Team completion journal changed after rooted write: %v", err)
		}
		return teamCompletionRootMatchesPath(root, path)
	})
}

func teamCompletionRootMatchesPath(root *os.Root, path string) error {
	return journalRootMatchesPath(root, path)
}

func journalRootMatchesPath(root *os.Root, path string) error {
	anchored, err := root.Stat(".")
	if err != nil {
		return err
	}
	current, err := journalParentIdentity(path)
	if err != nil || !os.SameFile(anchored, current) {
		return fmt.Errorf("cleanup journal parent changed: %v", err)
	}
	return nil
}

// Team has no create idempotency key and no tag sweep. Keep uncertain create
// ownership separate from the strictly delete-only cleanup journal.
type teamCreateIntent struct {
	Version          int                            `json:"version"`
	RunID            string                         `json:"run_id"`
	ExecutionID      string                         `json:"execution_id"`
	Region           string                         `json:"region"`
	Surface          string                         `json:"surface"`
	EcctlBin         string                         `json:"ecctl_bin"`
	Name             string                         `json:"name"`
	Description      string                         `json:"description"`
	Input            []string                       `json:"input"`
	CaseScope        string                         `json:"case_scope,omitempty"`
	Teardown         string                         `json:"teardown,omitempty"`
	AcceptedID       string                         `json:"accepted_id,omitempty"`
	ProbeBinSHA256   string                         `json:"probe_bin_sha256,omitempty"`
	Dispatched       bool                           `json:"dispatched,omitempty"`
	DispatchProtocol int                            `json:"dispatch_protocol,omitempty"`
	QuotaBinding     *report.QuotaCredentialBinding `json:"quota_binding,omitempty"`
	VolumeBinding    *report.QuotaCredentialBinding `json:"volume_binding,omitempty"`
	CreatedAt        time.Time                      `json:"created_at"`
	path             string
	journalPath      string
	parent           os.FileInfo
}

var (
	writeTeamCreateIntent            = journalfile.WriteExclusiveDurable
	writeVolumeTeamCreateReceiptRoot = journalfile.WriteDurableRoot
	writeTeamCreateJournal           = writeRunnerJournal
	writeVolumeTeamSettlementRoot    = writeRunnerJournalRoot
)

// The private create intent binds a pending Volume Team name to this exact
// journal and original credential. Keep it until terminal registration or an
// explicitly never-started retirement has been established durably.
func checkVolumeTeamIntent(intent *teamCreateIntent, path string, journal report.CleanupJournal, roots ...*os.Root) error {
	if intent == nil || intent.VolumeBinding == nil || intent.parent == nil || intent.journalPath != path || len(intent.Name) != 32 || !strings.HasPrefix(intent.Name, "ecctl-t-") || intent.path != path+".team-create-"+intent.Name[8:]+".json" {
		return fmt.Errorf("Volume Team create intent target changed")
	}
	if err := checkJournalParentIdentity(path, intent.parent); err != nil {
		return err
	}
	var body []byte
	var err error
	if len(roots) != 0 && roots[0] != nil {
		parent, statErr := roots[0].Stat(".")
		if statErr != nil || !os.SameFile(parent, intent.parent) {
			return fmt.Errorf("Volume Team intent parent changed: %v", statErr)
		}
		var snapshot journalfile.RecoverySnapshot
		snapshot, err = journalfile.ReadPrivateRecoverySnapshotRoot(roots[0], filepath.Base(intent.path))
		body = snapshot.Body
	} else {
		body, err = os.ReadFile(intent.path)
	}
	if err != nil {
		return err
	}
	return checkVolumeTeamIntentBytes(intent, path, journal, body)
}

func checkVolumeTeamIntentBytes(intent *teamCreateIntent, path string, journal report.CleanupJournal, body []byte) error {
	var recorded teamCreateIntent
	if err := execpkg.UnmarshalStrictJSON(body, &recorded); err != nil {
		return err
	}
	expected := *intent
	expected.path, expected.journalPath, expected.parent = "", "", nil
	if !reflect.DeepEqual(recorded, expected) || recorded.Version != 1 || recorded.Description != "ecctl E2E Team owner="+intent.Name[8:] || recorded.AcceptedID != "" && !safeTeamIdentity(recorded.AcceptedID) {
		return fmt.Errorf("Volume Team create intent changed")
	}
	if journal.Version != 2 || journal.JournalPath != path || journal.RunID != intent.RunID || journal.ExecutionID != intent.ExecutionID || journal.RegionRole != "primary" || journal.Region != intent.Region || journal.Surface != intent.Surface || journal.EcctlBin != intent.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, intent.VolumeBinding) || intent.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, intent.QuotaBinding) {
		return fmt.Errorf("Volume Team create journal binding changed")
	}
	return nil
}

// The dispatch transition is durable before the process can reach ecctl.
// A missing result after this point is uncertain even if no child PID was
// observed by the caller.
func markVolumeTeamCreateDispatch(cl *cleanup, intent *teamCreateIntent) error {
	if intent == nil || intent.VolumeBinding == nil || intent.Dispatched {
		return nil
	}
	path, err := cl.checkedJournalPath("primary")
	if err != nil || path != intent.journalPath {
		return fmt.Errorf("Volume Team dispatch journal changed: %v", err)
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := cl.openRememberedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy {
			return fmt.Errorf("Volume Team dispatch journal is unavailable: %v", err)
		}
		if err := checkVolumeTeamIntent(intent, path, journal, root); err != nil {
			return err
		}
		if state, exists := journal.VolumeTeamCreateIDs[intent.Name]; !exists || state != "" {
			return fmt.Errorf("Volume Team dispatch reservation changed")
		}
		originalSnapshot, readErr := journalfile.ReadPrivateRecoverySnapshotRoot(root, filepath.Base(intent.path))
		if readErr != nil {
			return readErr
		}
		original := originalSnapshot.Body
		var recorded teamCreateIntent
		if execpkg.UnmarshalStrictJSON(original, &recorded) != nil || recorded.Dispatched || recorded.AcceptedID != "" {
			return fmt.Errorf("Volume Team dispatch original intent changed")
		}
		dispatched := *intent
		dispatched.Dispatched = true
		dispatched.path, dispatched.journalPath, dispatched.parent = "", "", nil
		body, err := json.MarshalIndent(dispatched, "", "  ")
		if err != nil {
			return err
		}
		body = append(body, '\n')
		if err := journalRootMatchesPath(root, path); err != nil {
			return err
		}
		writeErr := journalfile.WriteDurableRoot(root, filepath.Base(intent.path), body)
		installed, readErr := root.ReadFile(filepath.Base(intent.path))
		if writeErr != nil {
			if readErr == nil && bytes.Equal(installed, original) {
				return writeErr
			}
			if readErr != nil || !bytes.Equal(installed, body) {
				return errors.Join(writeErr, fmt.Errorf("Volume Team dispatch marker is uncertain: %v", readErr))
			}
			return errors.Join(writeErr, journalfile.RestoreUninvokedMarkerRoot(root, filepath.Base(intent.path), body, original))
		}
		if readErr != nil || !bytes.Equal(installed, body) {
			return errors.Join(writeErr, fmt.Errorf("Volume Team dispatch was not installed exactly: %v", readErr))
		}
		if err := journalRootMatchesPath(root, path); err != nil {
			return err
		}
		intent.Dispatched = true
		return nil
	})
}

// A confirmed finalizer and its pending-name settlement are one durable
// transition. No other name in the journal may be modified by this helper.
func settleVolumeTeamRegistration(journal *report.CleanupJournal, intent *teamCreateIntent, teamID, path string) error {
	if err := checkVolumeTeamIntent(intent, path, *journal); err != nil {
		return err
	}
	if !safeTeamIdentity(teamID) || !reflect.DeepEqual(journal.VolumeTeamCreators[teamID], intent.VolumeBinding) {
		return fmt.Errorf("Volume Team registration lacks exact creator identity")
	}
	state, exists := journal.VolumeTeamCreateIDs[intent.Name]
	if !exists || state != "" && state != teamID {
		return fmt.Errorf("Volume Team pending name changed")
	}
	matches := 0
	for _, entry := range journal.Entries {
		if !entry.TeamCreateConfirmed || !entry.VolumeTeamBound {
			continue
		}
		id, err := journalfile.TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return err
		}
		if id == teamID && entry.RegionRole == "primary" && entry.Region == intent.Region && entry.ExecutionID == intent.ExecutionID {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("Volume Team finalizer was not registered exactly once")
	}
	journal.VolumeTeamCreateIDs[intent.Name] = teamID
	return nil
}

// A process that never started cannot have allocated a Team. The caller holds
// the Team create replay lock; retire only its empty name under the parent lock.
func retireUnstartedVolumeTeamCreate(cl *cleanup, intent *teamCreateIntent, exact ...report.CleanupJournal) (retErr error) {
	return retireVolumeTeamCreateReservation(cl, intent, false, exact...)
}

func definitiveVolumeTeamCreateRejection(result execpkg.Result) bool {
	if result.Err != nil || !result.Started || result.Exit != 2 {
		return false
	}
	root, ok := result.JSON.(map[string]any)
	if !ok {
		return false
	}
	failure, ok := root["error"].(map[string]any)
	if !ok || failure["kind"] != "service" || failure["code"] != "CloudAPIError" || failure["retryable"] != false {
		return false
	}
	for _, key := range []string{"team", "team_id", "id", "resource", "target", "response", "product", "operation", "recovery_command", "accepted_team"} {
		if _, present := root[key]; present {
			return false
		}
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action_name"] != "CreateTeam" || action["provider_verified"] != true || action["code"] != "Forbidden" && action["code"] != "NoPermission" && action["code"] != "InvalidParameter" {
		return false
	}
	if _, present := action["target"]; present {
		return false
	}
	requestID, ok := action["request_id"].(string)
	return ok && requestID != "" && !strings.ContainsAny(requestID, " \t\r\n")
}

// Called only by the originating create invocation after its dispatch marker
// was installed and its process result was observed.
func retireObservedVolumeTeamCreate(cl *cleanup, intent *teamCreateIntent, result execpkg.Result) error {
	if intent == nil || !intent.Dispatched ||
		!(result.Err != nil && !result.Started && result.Exit == -1) && !definitiveVolumeTeamCreateRejection(result) {
		return fmt.Errorf("Volume Team create has no definitive non-allocation result")
	}
	return retireVolumeTeamCreateReservation(cl, intent, true)
}

func retireVolumeTeamCreateReservation(cl *cleanup, intent *teamCreateIntent, observed bool, exact ...report.CleanupJournal) (retErr error) {
	if intent == nil || intent.VolumeBinding == nil {
		return nil
	}
	if intent.Dispatched != observed {
		return fmt.Errorf("dispatched Volume Team has an uncertain create result")
	}
	defer func() {
		if retErr != nil {
			cl.mu.Lock()
			cl.volumeTeamRetirementUncertain = true
			cl.allocationFence = "Volume Team never-started retirement failed"
			cl.mu.Unlock()
		}
	}()
	path, err := cl.checkedJournalPath("primary")
	if err != nil || path != intent.journalPath {
		return fmt.Errorf("Volume Team retirement journal target changed: %v", err)
	}
	if err := checkJournalParentIdentity(path, intent.parent); err != nil {
		return err
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.volumeTeamRetirementUncertain {
		return fmt.Errorf("earlier Volume Team retirement is uncertain")
	}
	if checked, err := cl.checkedJournalPath("primary"); err != nil || checked != path {
		return fmt.Errorf("Volume Team retirement journal target changed: %v", err)
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		if _, err := cl.checkedJournalParentLocked(path); err != nil {
			return err
		}
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy {
			return fmt.Errorf("Volume Team reservation requires a protected journal")
		}
		if err := checkVolumeTeamIntent(intent, path, journal); err != nil {
			return err
		}
		if len(exact) > 0 && !reflect.DeepEqual(journal, exact[0]) {
			return fmt.Errorf("failed Volume Team reservation was not installed exactly")
		}
		state, exists := journal.VolumeTeamCreateIDs[intent.Name]
		if !exists || state != "" {
			return fmt.Errorf("Volume Team never-started reservation changed")
		}
		delete(journal.VolumeTeamCreateIDs, intent.Name)
		if len(journal.VolumeTeamCreateIDs) == 0 {
			journal.VolumeTeamCreateIDs = nil
		}
		if err := writeTeamCreateJournal(path, journal); err != nil {
			if barrierErr := reaffirmVolumeTeamRetirementJournal(path, journal); barrierErr != nil {
				return errors.Join(err, barrierErr)
			}
		}
		installed, legacy, err := readRunnerJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume Team never-started retirement was not installed exactly")
		}
		return nil
	})
}

func teamCommandArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (intent *teamCreateIntent) command() string {
	// Pin the verified region and machine output before user values: early
	// raw-token checks must see the actual flags, independent of profile defaults.
	args := append([]string{intent.Input[0], "--output=json", "--agent-envelope=false", "--region", intent.Region}, intent.Input[1:]...)
	for i, value := range args {
		args[i] = teamCommandArgument(value)
	}
	return strings.Join(args, " ")
}

func teamOwnership(data map[string]any) error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	owner := hex.EncodeToString(nonce[:])
	data["team_name"] = "ecctl-t-" + owner // 32 characters, independent of run labels.
	data["team_owner"] = "ecctl E2E Team owner=" + owner
	return nil
}

func isTeamCreate(command string) bool {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 || args[0] != "ecctl" {
		return false
	}
	// Find the command before validating flag values, just as the public CLI
	// does. Even invalid Team inputs must enter reservation, not generic retry.
	root := &cobra.Command{Use: "ecctl"}
	root.PersistentFlags().AddFlagSet(teamCreateFlagSet())
	product := &cobra.Command{Use: "sandbox", Aliases: []string{"sbx"}}
	resource := &cobra.Command{Use: "team"}
	create := &cobra.Command{Use: "create"}
	resource.AddCommand(create)
	product.AddCommand(resource)
	root.AddCommand(product)
	// A separator cannot make an unsupported Team layout bypass reservation.
	// Classify conservatively here; reservation rejects the original separator.
	commandArgs := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			continue
		}
		commandArgs = append(commandArgs, arg)
		name, _, inline := strings.Cut(arg, "=")
		flag := root.PersistentFlags().Lookup(strings.TrimPrefix(name, "--"))
		if strings.HasPrefix(arg, "--") && !inline && flag != nil && flag.NoOptDefVal == "" && i+1 < len(args) {
			// A string flag consumes its next token even when that value is --.
			i++
			commandArgs = append(commandArgs, args[i])
		}
	}
	found, _, err := root.Find(commandArgs)
	return err == nil && found == create
}

func teamCreateFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("Team create", pflag.ContinueOnError)
	// Match the public root and Team create flag types. pflag handles prefix,
	// interspersed and inline values using the same grammar as Cobra.
	for _, name := range []string{"region", "profile", "lang", "output", "name", "description", "resource-group", "plan"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"json", "agent-envelope", "no-color"} {
		flags.Bool(name, false, "")
	}
	flags.BoolP("help", "h", false, "")
	flags.BoolP("version", "v", false, "")
	return flags
}

// Keep restart admission identical to the original create reservation.
func validateTeamCreateInput(args []string, name, description, expectedRegion string) error {
	if len(args) == 0 || args[0] != "ecctl" {
		return fmt.Errorf("Team recovery command must start with ecctl")
	}
	parsed := teamCreateFlagSet()
	if err := parsed.Parse(args[1:]); err != nil {
		return fmt.Errorf("invalid Team recovery input: %w", err)
	}
	positionals := parsed.Args()
	if len(positionals) != 3 || (positionals[0] != "sandbox" && positionals[0] != "sbx") || positionals[1] != "team" || positionals[2] != "create" {
		return fmt.Errorf("Team recovery requires exactly the Team create command path")
	}
	flags := map[string]string{}
	for i := 1; i < len(args); i++ {
		if args[i] == "--" || strings.HasPrefix(args[i], "-") && !strings.HasPrefix(args[i], "--") {
			return fmt.Errorf("unsupported Team recovery input %s", args[i])
		}
		if strings.HasPrefix(args[i], "--") {
			key, value, inline := strings.Cut(args[i], "=")
			switch key {
			case "--name", "--description", "--plan", "--region", "--lang", "--output", "--json", "--no-color":
			default:
				return fmt.Errorf("unsupported Team recovery input %s", key)
			}
			if !inline && parsed.Lookup(strings.TrimPrefix(key, "--")).NoOptDefVal == "" {
				i++
				if i >= len(args) {
					return fmt.Errorf("missing %s value", key)
				}
				value = args[i]
			}
			if _, exists := flags[key]; exists {
				return fmt.Errorf("duplicate Team create flag %s", key)
			}
			flags[key] = value
		}
	}
	if flags["--output"] != "" && flags["--output"] != "json" {
		return fmt.Errorf("Team recovery requires JSON output")
	}
	if plan, supplied := flags["--plan"]; supplied {
		switch plan {
		case "eco", "std", "pro", "ent":
		default:
			return fmt.Errorf("unsupported Team recovery plan %q", plan)
		}
	}
	region, regionSupplied := flags["--region"]
	if flags["--name"] != name || flags["--description"] != description || regionSupplied && region != expectedRegion {
		return fmt.Errorf("Team create command does not match recovery ownership/region")
	}
	return nil
}

func reserveTeamCreate(cmd string, data map[string]any, cfg execpkg.Config, cl *cleanup, st scenario.Step, sourceEnv []string, credentialBindings ...*report.QuotaCredentialBinding) (*teamCreateIntent, error) {
	args, err := shlex.Split(cmd)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 || args[0] != "ecctl" {
		return nil, fmt.Errorf("Team recovery command must start with ecctl")
	}
	name, _ := data["team_name"].(string)
	description, _ := data["team_owner"].(string)
	if len(name) != 32 || !strings.HasPrefix(name, "ecctl-t-") || description != "ecctl E2E Team owner="+strings.TrimPrefix(name, "ecctl-t-") {
		return nil, fmt.Errorf("Team create requires execution-owned name and description")
	}
	if _, err := hex.DecodeString(name[8:]); err != nil {
		return nil, fmt.Errorf("invalid Team owner: %w", err)
	}
	if err := validateTeamCreateInput(args, name, description, cfg.Region); err != nil {
		return nil, err
	}
	if st.TeardownRegion != "" && st.TeardownRegion != "primary" {
		return nil, fmt.Errorf("Team create teardown region must be primary")
	}
	meta := cl.journalMeta
	if cl.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || cfg.Region == "" || cfg.Bin == "" || !scenario.Surface(meta.Surface).Valid() {
		return nil, fmt.Errorf("Team create requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	cleanupCfg, ok := cl.execCfg["primary"]
	if !ok || cleanupCfg.Region != cfg.Region || cleanupCfg.Bin != cfg.Bin {
		return nil, fmt.Errorf("Team create and cleanup targets differ")
	}
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		return nil, err
	}
	validationData := vars.Clone(data)
	validationData["team_id"] = "intent-validation"
	teardown, err := renderTeamRecoveryDelete(st, validationData)
	if err != nil {
		return nil, err
	}
	if strings.Count(teardown, "intent-validation") != 1 || caseScope(data) == "" || strings.ContainsAny(caseScope(data), "\r\n") {
		return nil, fmt.Errorf("Team recovery teardown or case scope is not stable")
	}
	if len(credentialBindings) > 2 {
		return nil, fmt.Errorf("unsupported Team credential binding count")
	}
	var binding, volumeBinding *report.QuotaCredentialBinding
	if len(credentialBindings) > 0 {
		binding = credentialBindings[0]
	}
	if len(credentialBindings) > 1 {
		volumeBinding = credentialBindings[1]
	}
	if binding != nil && volumeBinding != nil && !reflect.DeepEqual(binding, volumeBinding) {
		return nil, fmt.Errorf("Team Quota and Volume credential bindings differ")
	}
	if volumeBinding != nil {
		if err := journalfile.ValidateVolumeFixtureIdentity(volumeBinding); err != nil {
			return nil, err
		}
		if cfg.Profile != volumeBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != volumeBinding.Identity {
			return nil, fmt.Errorf("Volume Team create did not retain its verified credential")
		}
		if err := journalfile.ValidateQuotaBinding(volumeBinding, sourceEnv); err != nil {
			return nil, err
		}
	}
	intent := &teamCreateIntent{Version: 1, RunID: meta.RunID, ExecutionID: meta.ExecutionID, Region: cfg.Region, Surface: meta.Surface, EcctlBin: cfg.Bin, Name: name, Description: description, Input: args, CaseScope: caseScope(data), Teardown: teardown, QuotaBinding: binding, VolumeBinding: volumeBinding, CreatedAt: time.Now().UTC(), path: cl.journal + ".team-create-" + name[8:] + ".json"}
	if volumeBinding != nil {
		intent.DispatchProtocol = 1
	}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	desired, _, journalPath := cl.journalValues(&cleanupItem{role: "primary"})
	desired.JournalPath = journalPath
	intent.journalPath = journalPath
	known, err := cl.rememberedJournalParent(journalPath)
	if err != nil {
		return nil, err
	}
	lock := journalfile.WithLock
	if known != nil {
		lock = journalfile.WithExistingParentLock
	}
	var nonceWriteFailed bool
	var nonceExpected report.CleanupJournal
	var nonceOriginalBody, nonceMarkedBody []byte
	if err := lock(context.Background(), journalPath, func() error {
		if err := checkJournalParentIdentity(journalPath, known); err != nil {
			return err
		}
		parent, err := journalParentIdentity(journalPath)
		if err != nil {
			return err
		}
		intent.parent = parent
		journal, legacy, err := readRunnerJournal(journalPath)
		if err == nil {
			if legacy || journal.Version != 2 {
				return fmt.Errorf("Team create requires a version 2 cleanup journal")
			}
			if journal.RunID != desired.RunID || journal.ExecutionID != desired.ExecutionID || journal.RegionRole != desired.RegionRole || journal.Region != desired.Region || journal.Surface != desired.Surface || journal.EcctlBin != desired.EcctlBin {
				return fmt.Errorf("existing Team cleanup journal does not match run, execution, role, region, surface and binary")
			}
			if volumeBinding != nil && !reflect.DeepEqual(journal.VolumeFixtureBinding, volumeBinding) {
				return fmt.Errorf("Volume Team creator differs from fixture credential binding")
			}
			if journal.JournalPath == "" {
				journal.JournalPath = journalPath
				if err := writeTeamCreateJournal(journalPath, journal); err != nil {
					return fmt.Errorf("anchor Team cleanup journal: %w", err)
				}
			}
			if binding != nil {
				if journal.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, binding) {
					return fmt.Errorf("Team cleanup journal Quota credential binding changed")
				}
				journal.QuotaBinding = binding
				if err := writeTeamCreateJournal(journalPath, journal); err != nil {
					return fmt.Errorf("bind Team cleanup credential identity: %w", err)
				}
			}
		} else if os.IsNotExist(err) {
			if volumeBinding != nil {
				return fmt.Errorf("Volume Team requires its durable fixture credential binding")
			}
			// Bind the journal before another run can reserve or either create
			// can launch. A failed claim must not degrade to in-memory cleanup.
			desired.QuotaBinding = binding
			if err := writeTeamCreateJournal(journalPath, desired); err != nil {
				return fmt.Errorf("claim Team cleanup journal: %w", err)
			}
		} else {
			return err
		}
		if volumeBinding != nil {
			if _, exists := journal.VolumeTeamCreateIDs[name]; exists {
				return fmt.Errorf("Volume Team create ownership is already reserved")
			}
			snapshot, snapshotLegacy, original, readErr := readRunnerJournalSnapshot(journalPath)
			if readErr != nil {
				return readErr
			}
			if snapshotLegacy || !reflect.DeepEqual(snapshot, journal) {
				return fmt.Errorf("Volume Team nonce source journal changed")
			}
			nonceOriginalBody = original
		}
		if err := writeTeamCreateIntent(intent.path, append(body, '\n')); err != nil {
			return err
		}
		if volumeBinding == nil {
			return nil
		}
		if journal.VolumeTeamCreateIDs == nil {
			journal.VolumeTeamCreateIDs = make(map[string]string)
		}
		journal.VolumeTeamCreateIDs[name] = ""
		nonceExpected = journal
		nonceMarkedBody, err = runnerJournalBytes(journal)
		if err != nil {
			return err
		}
		if err := writeTeamCreateJournal(journalPath, journal); err != nil {
			nonceWriteFailed = true
			return fmt.Errorf("reserve pending Volume Team ownership: %w", err)
		}
		installed, legacy, err := readRunnerJournal(journalPath)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("pending Volume Team ownership was not installed exactly")
		}
		return nil
	}); err != nil {
		retiredNonce := false
		if volumeBinding != nil && nonceWriteFailed {
			installedExact := false
			_ = journalfile.WithExistingParentLock(context.Background(), journalPath, func() error {
				installed, readErr := os.ReadFile(journalPath)
				if readErr != nil {
					return readErr
				}
				retiredNonce = bytes.Equal(installed, nonceOriginalBody)
				installedExact = bytes.Equal(installed, nonceMarkedBody)
				return nil
			})
			if installedExact {
				retiredNonce = retireUnstartedVolumeTeamCreate(cl, intent, nonceExpected) == nil
			}
		}
		if binding != nil || volumeBinding != nil {
			// Journal replacement can succeed even when its durability barrier
			// reports an error. Read the installed state before choosing the
			// in-memory cleanup binding, and fence all later allocations.
			if !retiredNonce {
				cl.mu.Lock()
				cl.allocationFence = "Team create recovery reservation failed"
				cl.mu.Unlock()
			}
			_ = lock(context.Background(), journalPath, func() error {
				if err := checkJournalParentIdentity(journalPath, known); err != nil {
					return err
				}
				current, legacy, readErr := readRunnerJournal(journalPath)
				if readErr != nil || legacy || !reflect.DeepEqual(current.QuotaBinding, binding) {
					return fmt.Errorf("Team cleanup binding cannot be reconciled")
				}
				cl.mu.Lock()
				cl.quotaBinding = binding
				cl.mu.Unlock()
				return nil
			})
		}
		return nil, fmt.Errorf("reserve Team create intent: %w", err)
	}
	if binding != nil {
		cl.mu.Lock()
		cl.quotaBinding = binding
		cl.mu.Unlock()
	}
	return intent, nil
}

// Only a confirmed, exactly registered Team finalizer can turn a pending
// Volume Team creation into terminal history for fixture cleanup.
func settleVolumeTeamCreate(cl *cleanup, intent *teamCreateIntent, teamID string) error {
	if intent == nil || intent.VolumeBinding == nil {
		return nil
	}
	if !safeTeamIdentity(teamID) || intent.AcceptedID != teamID {
		return fmt.Errorf("Volume Team registration lacks a durable invocation-linked identity")
	}
	path, err := cl.checkedJournalPath("primary")
	if err != nil || path != intent.journalPath {
		return fmt.Errorf("Volume Team registration journal target changed: %v", err)
	}
	if err := checkJournalParentIdentity(path, intent.parent); err != nil {
		return err
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		if err := checkJournalParentIdentity(path, intent.parent); err != nil {
			return err
		}
		root, err := cl.openRememberedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != 2 || journal.JournalPath != path || journal.RunID != intent.RunID || journal.ExecutionID != intent.ExecutionID || journal.RegionRole != "primary" || journal.Region != intent.Region || journal.Surface != intent.Surface || journal.EcctlBin != intent.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, intent.VolumeBinding) || !reflect.DeepEqual(journal.VolumeTeamCreators[teamID], intent.VolumeBinding) {
			return fmt.Errorf("Volume Team registration binding changed")
		}
		state, exists := journal.VolumeTeamCreateIDs[intent.Name]
		if !exists || state != "" && state != teamID {
			return fmt.Errorf("pending Volume Team ownership changed")
		}
		matches := 0
		for _, entry := range journal.Entries {
			if !entry.TeamCreateConfirmed || !entry.VolumeTeamBound {
				continue
			}
			id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
			if parseErr != nil {
				return parseErr
			}
			if id == teamID && entry.RegionRole == "primary" && entry.Region == intent.Region && entry.ExecutionID == intent.ExecutionID {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("Volume Team finalizer was not registered exactly once")
		}
		if state == teamID {
			return journalRootMatchesPath(root, path)
		}
		journal.VolumeTeamCreateIDs[intent.Name] = teamID
		if err := journalRootMatchesPath(root, path); err != nil {
			return err
		}
		if err := writeVolumeTeamSettlementRoot(root, path, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume Team terminal ownership was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
}

func teamCreateIdentity(result execpkg.Result) (string, bool) {
	root, _ := result.JSON.(map[string]any)
	team, _ := root["team"].(map[string]any)
	id, _ := team["id"].(string)
	return id, safeTeamIdentity(id)
}

// The returned ID is the only invocation-linked authority for a Volume Team.
// Persist it before a read-only name lookup can lead to a delete finalizer.
func recordVolumeTeamCreateID(cl *cleanup, intent *teamCreateIntent, id string, result execpkg.Result) error {
	if intent == nil || intent.VolumeBinding == nil || !safeTeamIdentity(id) {
		return fmt.Errorf("Volume Team create receipt is invalid")
	}
	root, _ := result.JSON.(map[string]any)
	team, _ := root["team"].(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || team["id"] != id {
		return fmt.Errorf("Volume Team create receipt does not confirm its returned ID")
	}
	if name, present := team["name"]; present && name != intent.Name {
		return fmt.Errorf("Volume Team returned name differs from its reservation")
	}
	if owner, present := team["description"]; present && owner != intent.Description {
		return fmt.Errorf("Volume Team returned owner differs from its reservation")
	}
	path, err := cl.checkedJournalPath("primary")
	if err != nil || path != intent.journalPath {
		return fmt.Errorf("Volume Team create receipt journal changed: %v", err)
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		if err := checkJournalParentIdentity(path, intent.parent); err != nil {
			return err
		}
		root, err := cl.openRememberedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy {
			return fmt.Errorf("Volume Team create receipt journal is unavailable: %v", err)
		}
		if err := checkVolumeTeamIntent(intent, path, journal, root); err != nil {
			return err
		}
		if state, exists := journal.VolumeTeamCreateIDs[intent.Name]; !exists || state != "" {
			return fmt.Errorf("Volume Team create receipt reservation changed")
		}
		if intent.AcceptedID != "" && intent.AcceptedID != id {
			return fmt.Errorf("Volume Team create receipt identity changed")
		}
		if intent.AcceptedID == id {
			return journalRootMatchesPath(root, path)
		}
		recorded := *intent
		recorded.AcceptedID = id
		body, err := json.MarshalIndent(&recorded, "", "  ")
		if err != nil {
			return err
		}
		body = append(body, '\n')
		if err := journalRootMatchesPath(root, path); err != nil {
			return err
		}
		if err := writeVolumeTeamCreateReceiptRoot(root, filepath.Base(intent.path), body); err != nil {
			return fmt.Errorf("persist Volume Team create receipt: %w", err)
		}
		installed, err := root.ReadFile(filepath.Base(intent.path))
		if err != nil || !bytes.Equal(installed, body) {
			return fmt.Errorf("Volume Team create receipt was not installed exactly: %v", err)
		}
		if err := journalRootMatchesPath(root, path); err != nil {
			return err
		}
		intent.AcceptedID = id
		return nil
	})
}

// A successful create response can omit ownership fields. Confirm the exact
// reserved name and description under the original creator identity before a
// Volume Team finalizer can be registered for its returned ID.
func confirmVolumeTeamCreate(cfg execpkg.Config, intent *teamCreateIntent, result execpkg.Result, timeout time.Duration) error {
	if intent == nil || intent.VolumeBinding == nil || cfg.Bin != intent.EcctlBin || cfg.Region != intent.Region || cfg.Profile != intent.VolumeBinding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != intent.VolumeBinding.Identity {
		return fmt.Errorf("Volume Team create proof lacks its original credential binding")
	}
	returnedID, ok := teamCreateIdentity(result)
	if !ok || intent.AcceptedID != returnedID {
		return fmt.Errorf("Volume Team create returned no durable invocation-linked identity")
	}
	root, _ := result.JSON.(map[string]any)
	team, _ := root["team"].(map[string]any)
	if name, present := team["name"]; present && name != intent.Name {
		return fmt.Errorf("Volume Team returned name differs from its reservation")
	}
	if description, present := team["description"]; present && description != intent.Description {
		return fmt.Errorf("Volume Team returned owner differs from its reservation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	confirmedID, err := lookupReservedTeamID(ctx, cfg, intent)
	if err != nil {
		return err
	}
	if confirmedID != returnedID {
		return fmt.Errorf("Volume Team returned ID differs from its reserved-name read")
	}
	return nil
}

func safeTeamIdentity(id string) bool {
	if id == "" || id[0] == '-' || id[0] == '_' {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}

func renderTeamRecoveryDelete(st scenario.Step, data map[string]any) (string, error) {
	id, ok := data["team_id"].(string)
	if !ok || !safeTeamIdentity(id) {
		return "", fmt.Errorf("Team delete requires a safe identity")
	}
	command, err := vars.Render(st.Teardown, data)
	if err != nil {
		return "", err
	}
	target, err := journalfile.TeamDeleteTarget(command)
	if err != nil || target != id || !isReplayableTeardown(command) {
		return "", fmt.Errorf("recovered Team requires a validated Team delete teardown")
	}
	return command, nil
}

// A read can discover ownership, but cannot establish that a failed create did
// not commit. Never resend CreateTeam, and keep its original failure visible.
func reconcileTeamCreate(cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, locks []string, timeout time.Duration, intent *teamCreateIntent, returnedID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	matchedID, err := lookupReservedTeamID(ctx, cfg, intent)
	if err != nil {
		return err
	}
	if returnedID != "" && matchedID != returnedID {
		return fmt.Errorf("returned Team identity differs from intent-bound reconciliation; retained recovery intent %s", intent.path)
	}
	if intent.VolumeBinding != nil && intent.AcceptedID != matchedID {
		return fmt.Errorf("Volume Team name lookup is diagnostic without a durable invocation-linked ID; retained recovery intent %s", intent.path)
	}
	// Render the declared delete with only the strictly matched identity.
	recoveryData := make(map[string]any, len(data)+1)
	for k, v := range data {
		recoveryData[k] = v
	}
	recoveryData["team_id"] = matchedID
	command, err := renderTeamRecoveryDelete(st, recoveryData)
	if err != nil {
		return err
	}
	if _, err := cl.pushItemWithFixture(scope, caseScope(data), command, st.TeardownRegion, locks, false, true, intent.QuotaBinding != nil, intent.VolumeBinding, "", intent); err != nil {
		return fmt.Errorf("journal recovered Team: %w; retained recovery intent %s", err, intent.path)
	}
	if err := settleVolumeTeamCreate(cl, intent, matchedID); err != nil {
		return fmt.Errorf("settle recovered Volume Team: %w; retained recovery intent %s", err, intent.path)
	}
	if intent.QuotaBinding != nil {
		cl.mu.Lock()
		cl.unresolvedQuotaTeamCreate = false
		cl.mu.Unlock()
	}
	return fmt.Errorf("journaled owned Team for cleanup; CreateTeam remains failed; recovery intent %s", intent.path)
}

func lookupReservedTeamID(ctx context.Context, cfg execpkg.Config, intent *teamCreateIntent) (string, error) {
	result := execpkg.Run(ctx, cfg, "ecctl --output=json --agent-envelope=false --region "+teamCommandArgument(intent.Region)+" sandbox team list --filter name="+intent.Name+" --all")
	if result.Err != nil || result.Exit != 0 {
		return "", fmt.Errorf("Team reconciliation failed; retained recovery intent %s", intent.path)
	}
	return reservedTeamIDFromList(result.JSON, intent)
}

func reservedTeamIDFromList(raw any, intent *teamCreateIntent) (string, error) {
	root, ok := raw.(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid Team reconciliation; retained recovery intent %s", intent.path)
	}
	teams, ok := root["teams"].([]any)
	pagination, _ := root["pagination"].(map[string]any)
	hasMore, complete := pagination["has_more"].(bool)
	if !ok || !complete || hasMore {
		return "", fmt.Errorf("incomplete Team reconciliation; retained recovery intent %s", intent.path)
	}
	var matchedID string
	for _, value := range teams {
		team, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid Team reconciliation item; retained recovery intent %s", intent.path)
		}
		if team["name"] != intent.Name {
			continue
		}
		if team["description"] != intent.Description {
			return "", fmt.Errorf("reserved Team name collided with a different owner; retained recovery intent %s", intent.path)
		}
		id, ok := team["id"].(string)
		if !ok || !safeTeamIdentity(id) {
			return "", fmt.Errorf("unsafe recovered Team identity; retained recovery intent %s", intent.path)
		}
		if matchedID != "" {
			return "", fmt.Errorf("ambiguous owned Team reconciliation; retained recovery intent %s", intent.path)
		}
		matchedID = id
	}
	if matchedID == "" {
		return "", fmt.Errorf("no owned Team found; absence is unproven; retained recovery intent %s", intent.path)
	}
	return matchedID, nil
}
