package runner

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/google/shlex"
)

func volumeUpdateCandidate(command string) bool {
	action, err := nativeVolumeMutationAction(command)
	return err == nil && action == "update"
}

// volumeTransferIntent binds an authored UpdateVolume team transfer to its
// retained creation evidence and a case-owned destination Team. The durable
// pending markers are written before the provider call; finishVolumeTransfer
// observes the destination and re-anchors the cleanup scope to it.
type volumeTransferIntent struct {
	request journalfile.VolumeUpdateRequest
	binding journalfile.TemplateCreateIntent
	item    *cleanupItem
	dest    string
	journal string
	cfg     execpkg.Config
	command string
}

func reserveVolumeTransfer(ctx context.Context, command string, cfg execpkg.Config, cl *cleanup, st scenario.Step, timeout time.Duration, caseScopeValue string) (*volumeTransferIntent, error) {
	request, err := journalfile.ParseVolumeUpdateRequest(command)
	if err != nil {
		return nil, err
	}
	if st.Teardown != "" || st.TeardownRegion != "" {
		return nil, fmt.Errorf("Volume update reuses the retained create finalizer and cannot register another")
	}
	cl.mu.Lock()
	var found *cleanupItem
	for _, item := range cl.ownedTemplates {
		if item.role != "primary" || item.templateBinding == nil || item.templateBinding.Resource != "volume" {
			continue
		}
		owned, parseErr := journalfile.ParseVolumeDeleteRequest(item.cmd)
		if parseErr != nil {
			cl.mu.Unlock()
			return nil, parseErr
		}
		if owned.ID != request.ID {
			continue
		}
		if item.done {
			cl.mu.Unlock()
			return nil, fmt.Errorf("Volume update target is already consumed")
		}
		if found != nil {
			cl.mu.Unlock()
			return nil, fmt.Errorf("ambiguous owned Volume update target")
		}
		found = item
	}
	cl.mu.Unlock()
	if found == nil {
		return nil, fmt.Errorf("Volume update lacks an active run-owned creation binding")
	}
	meta, _, journal := cl.journalValues(&cleanupItem{role: "primary"})
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		return nil, err
	}
	canonicalJournal, err := journalfile.ResolveJournalPath(journal)
	if err != nil {
		return nil, err
	}
	if cl.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || cfg.Region == "" || cfg.Bin == "" ||
		!scenario.Surface(meta.Surface).Valid() || meta.Region != cfg.Region || meta.EcctlBin != cfg.Bin {
		return nil, fmt.Errorf("Volume update requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	knownParent, err := cl.rememberedJournalParent(journal)
	if err != nil || knownParent == nil {
		return nil, fmt.Errorf("Volume update journal parent is unavailable: %v", err)
	}
	// The retained finalizer snapshot is taken before the create intent is
	// journaled and observed, so admission reads the durable creation binding
	// instead of the possibly stale in-memory copy.
	var binding journalfile.TemplateCreateIntent
	var durableJournal report.CleanupJournal
	err = journalfile.WithExistingReplayLock(ctx, journal, func() error {
		if err := checkJournalParentIdentity(journal, knownParent); err != nil {
			return err
		}
		return journalfile.WithExistingParentLock(context.Background(), journal, func() error {
			root, err := cl.openRememberedJournalRoot(journal)
			if err != nil {
				return err
			}
			defer root.Close()
			current, legacy, err := readRunnerJournalRoot(root, journal)
			if err != nil {
				return err
			}
			if legacy || current.Version != 2 || current.RunID != meta.RunID || current.ExecutionID != meta.ExecutionID || current.RegionRole != meta.RegionRole || current.Region != meta.Region || current.Surface != meta.Surface || current.EcctlBin != meta.EcctlBin {
				return fmt.Errorf("existing Volume update journal binding differs")
			}
			durable, err := journalfile.VolumeDeleteBinding(journal, current, cl.execCfg[found.role].Env, found.cmd)
			if err != nil {
				return fmt.Errorf("Volume update lacks a durable creation binding: %w", err)
			}
			binding = durable
			durableJournal = current
			return journalRootMatchesPath(root, journal)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("read Volume update creation binding: %w", err)
	}
	if binding.State != "journaled" || !binding.VolumeObserved || binding.AcceptedVolumeID != request.ID || len(binding.Deletes) != 1 {
		return nil, fmt.Errorf("Volume update requires a journaled, observed creation binding")
	}
	if request.TeamID == binding.TeamID {
		return nil, fmt.Errorf("Volume update destination equals its current Team")
	}
	if request.RegionSet && request.Region != binding.Region || request.ProfileSet && request.Profile != binding.Profile {
		return nil, fmt.Errorf("owned Volume update changed region or profile")
	}
	if request.OutputSet && request.Output != "json" || request.AgentEnvelopeSet && request.AgentEnvelope {
		return nil, fmt.Errorf("owned Volume update requires plain JSON output")
	}
	if binding.RunID != meta.RunID || binding.ExecutionID != meta.ExecutionID || binding.Region != meta.Region || binding.Surface != meta.Surface || binding.EcctlBin != meta.EcctlBin {
		return nil, fmt.Errorf("Volume update requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	if caseScopeValue == "" || strings.ContainsAny(caseScopeValue, "\r\n") || binding.CaseScope != caseScopeValue {
		return nil, fmt.Errorf("Volume update requires the source case scope")
	}
	cl.mu.Lock()
	parent, owned, ownErr := cl.retainedTeamParent("primary", request.TeamID, false)
	cl.mu.Unlock()
	if ownErr != nil {
		return nil, ownErr
	}
	if !owned || !parent.TeamCreateConfirmed || parent.RegionRole != "primary" || parent.TeamReplayAttempted {
		return nil, fmt.Errorf("Volume update destination lacks active run ownership")
	}
	if parent.Scope != caseScopeValue || !parent.VolumeTeamBound {
		return nil, fmt.Errorf("Volume update destination is not an active creator in this case")
	}
	cfg.Profile = binding.Profile
	pin := binding.Identity
	cfg.ExpectedIdentity = &pin
	bound := binding.BoundConfig(cfg)
	bound.Region, bound.Profile = binding.Region, binding.Profile
	if err := binding.ValidateConfig(bound.Env); err != nil {
		return nil, err
	}
	identityCtx, identityCancel := context.WithTimeout(ctx, timeout)
	_, err = execpkg.ReadIdentity(identityCtx, bound, binding.Profile)
	identityCancel()
	if err != nil {
		return nil, err
	}
	if err := journalfile.CheckVolumeTransferTeamBinding(journal, durableJournal, bound, request.TeamID, binding); err != nil {
		return nil, err
	}
	err = journalfile.WithExistingReplayLock(ctx, journal, func() error {
		if err := checkJournalParentIdentity(journal, knownParent); err != nil {
			return err
		}
		return journalfile.WithExistingParentLock(context.Background(), journal, func() error {
			root, err := cl.openRememberedJournalRoot(journal)
			if err != nil {
				return err
			}
			defer root.Close()
			current, legacy, err := readRunnerJournalRoot(root, journal)
			if err != nil {
				return err
			}
			if legacy || current.Version != 2 || current.RunID != meta.RunID || current.ExecutionID != meta.ExecutionID || current.RegionRole != meta.RegionRole || current.Region != meta.Region || current.Surface != meta.Surface || current.EcctlBin != meta.EcctlBin {
				return fmt.Errorf("existing Volume update journal binding differs")
			}
			if resolved, resolveErr := journalfile.ResolveJournalPath(journal); resolveErr != nil || resolved != canonicalJournal {
				return fmt.Errorf("Volume update journal target changed before reservation")
			}
			if err := journalfile.CheckVolumeTransferTeamBinding(journal, current, bound, request.TeamID, binding); err != nil {
				return err
			}
			durable, err := journalfile.VolumeDeleteBinding(journal, current, cl.execCfg[found.role].Env, found.cmd)
			if err != nil || !reflect.DeepEqual(durable, binding) {
				return fmt.Errorf("Volume update creation binding changed before reservation")
			}
			if err := journalfile.ReserveVolumeTransfer(&current, binding, request.TeamID); err != nil {
				return err
			}
			if err := checkJournalParentIdentity(journal, knownParent); err != nil {
				return err
			}
			if err := writeVolumeTransferReservationJournalRoot(root, journal, current); err != nil {
				return err
			}
			installed, legacy, err := readRunnerJournalRoot(root, journal)
			if err != nil || legacy || !reflect.DeepEqual(installed, current) {
				return fmt.Errorf("Volume transfer reservation was not installed exactly")
			}
			return journalRootMatchesPath(root, journal)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("reserve Volume transfer: %w", err)
	}
	args, err := shlex.Split(command)
	if err != nil {
		return nil, err
	}
	if !request.RegionSet {
		args = append(args, "--region", binding.Region)
	}
	if !request.ProfileSet {
		args = append(args, "--profile", binding.Profile)
	}
	if !request.OutputSet {
		args = append(args, "--output=json")
	}
	if !request.AgentEnvelopeSet {
		args = append(args, "--agent-envelope=false")
	}
	return &volumeTransferIntent{request: request, binding: binding, item: found, dest: request.TeamID, journal: journal, cfg: bound, command: quotedTemplateCommand(args)}, nil
}

// finishVolumeTransfer settles the transfer after the provider call. A started
// invocation must be observed at the destination under the original credential
// before the cleanup scope is re-anchored; an unstarted invocation restores the
// reservation so the Volume remains anchored to its source Team.
func finishVolumeTransfer(ctx context.Context, res execpkg.Result, cl *cleanup, transfer *volumeTransferIntent, timeout time.Duration) error {
	if transfer == nil {
		return fmt.Errorf("Volume transfer lacks its reservation")
	}
	if !res.Started {
		if err := cl.restoreUnstartedVolumeTransfer(transfer); err != nil {
			return fmt.Errorf("restore unstarted Volume transfer: %w", err)
		}
		return fmt.Errorf("Volume update did not start; no transfer was sent")
	}
	root, _ := res.JSON.(map[string]any)
	volume, _ := root["volume"].(map[string]any)
	status, _ := volume["status"].(string)
	if res.Err != nil || res.Exit != 0 || volume["id"] != transfer.request.ID || volume["team_id"] != transfer.dest || status != "AVAILABLE" && status != "ERROR" {
		return fmt.Errorf("Volume update outcome is unproven; pending transfer retained for reconciliation")
	}
	if !volumeTransferAccepted(root) {
		return fmt.Errorf("Volume update response contradicts its UpdateVolume action; pending transfer retained")
	}
	knownParent, err := cl.rememberedJournalParent(transfer.journal)
	if err != nil || knownParent == nil {
		return fmt.Errorf("Volume transfer journal parent is unavailable: %v", err)
	}
	err = journalfile.WithExistingParentLock(context.Background(), transfer.journal, func() error {
		if err := checkJournalParentIdentity(transfer.journal, knownParent); err != nil {
			return err
		}
		root, err := cl.openRememberedJournalRoot(transfer.journal)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, transfer.journal)
		if err != nil {
			return err
		}
		if legacy || journal.RunID != transfer.binding.RunID || journal.ExecutionID != transfer.binding.ExecutionID || journal.Region != transfer.binding.Region || journal.EcctlBin != transfer.binding.EcctlBin {
			return fmt.Errorf("Volume transfer journal binding changed before settlement")
		}
		changed, err := journalfile.ResolveVolumeTransferObservation(&journal, transfer.binding, transfer.dest, status)
		if err != nil || !changed {
			return fmt.Errorf("Volume transfer destination observation is unavailable: %v", err)
		}
		reanchored := transfer.binding
		if err := journalfile.ReanchorVolumeTransfer(&journal, &reanchored, transfer.dest); err != nil {
			return err
		}
		if err := rewriteVolumeIntentTeamRoot(root, transfer.journal, reanchored); err != nil {
			return err
		}
		if err := checkJournalParentIdentity(transfer.journal, knownParent); err != nil {
			return err
		}
		if err := writeRunnerJournalRoot(root, transfer.journal, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, transfer.journal)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume transfer settlement was not installed exactly")
		}
		return journalRootMatchesPath(root, transfer.journal)
	})
	if err != nil {
		return fmt.Errorf("settle Volume transfer: %w", err)
	}
	transfer.item.templateBinding.TeamID = transfer.dest
	transfer.item.volumeTransferTeam = transfer.dest
	return nil
}

func (c *cleanup) restoreUnstartedVolumeTransfer(transfer *volumeTransferIntent) error {
	knownParent, err := c.rememberedJournalParent(transfer.journal)
	if err != nil || knownParent == nil {
		return fmt.Errorf("Volume transfer journal parent is unavailable: %v", err)
	}
	return journalfile.WithExistingParentLock(context.Background(), transfer.journal, func() error {
		if err := checkJournalParentIdentity(transfer.journal, knownParent); err != nil {
			return err
		}
		root, err := c.openRememberedJournalRoot(transfer.journal)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, transfer.journal)
		if err != nil {
			return err
		}
		if legacy || journal.RunID != transfer.binding.RunID || journal.ExecutionID != transfer.binding.ExecutionID {
			return fmt.Errorf("Volume transfer restore journal binding changed")
		}
		if err := journalfile.RestoreUnstartedVolumeTransfer(&journal, transfer.binding, transfer.dest); err != nil {
			return err
		}
		if err := writeRunnerJournalRoot(root, transfer.journal, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, transfer.journal)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume transfer restore was not installed exactly")
		}
		return journalRootMatchesPath(root, transfer.journal)
	})
}

func volumeTransferAccepted(root map[string]any) bool {
	rawActions, present := root["actions"]
	if !present {
		return false
	}
	actions, ok := rawActions.([]any)
	if !ok {
		return false
	}
	found := false
	for _, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok || action["action_name"] != "UpdateVolume" {
			continue
		}
		if found || action["provider_verified"] != true || action["code"] != nil && action["code"] != "" && action["code"] != "200" {
			return false
		}
		requestID, _ := action["request_id"].(string)
		if requestID == "" {
			return false
		}
		found = true
	}
	return found
}

// rewriteVolumeIntentTeamRoot durably re-anchors the retained creation sidecar
// to the destination Team. Only TeamID changes; every other retained field and
// the private regular-file shape are preserved and read back exactly.
func rewriteVolumeIntentTeamRoot(root *os.Root, journal string, intent journalfile.TemplateCreateIntent) error {
	name := filepath.Base(journal) + ".template-create-" + strings.TrimPrefix(intent.Name, "ecctl-p-") + ".json"
	body, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	var retained journalfile.TemplateCreateIntent
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&retained); err != nil {
		return err
	}
	if retained.Name != intent.Name || retained.AcceptedVolumeID != intent.AcceptedVolumeID || retained.Resource != "volume" || retained.State != "journaled" {
		return fmt.Errorf("Volume transfer sidecar differs from its retained creation binding")
	}
	retained.TeamID = intent.TeamID
	updated, err := json.MarshalIndent(retained, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := journalfile.WriteDurableRoot(root, name, updated); err != nil {
		return err
	}
	installed, err := root.ReadFile(name)
	if err != nil || !bytes.Equal(installed, updated) {
		return fmt.Errorf("Volume transfer sidecar was not installed exactly")
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Volume transfer sidecar is not a regular file")
	}
	return nil
}
