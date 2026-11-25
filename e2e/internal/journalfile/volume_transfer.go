package journalfile

import (
	"fmt"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

// VolumeUpdateRequest is the supported spelling of an authored Volume team
// transfer. E2E exercises the ownership-changing transfer form; the status-only
// body variant is covered by the offline CLI tests and never reallocates a
// cleanup scope here.
type VolumeUpdateRequest struct {
	ID, TeamID, Region, Profile, Output                         string
	TeamSet, RegionSet, ProfileSet, OutputSet, AgentEnvelopeSet bool
	AgentEnvelope                                               bool
}

func ParseVolumeUpdateRequest(command string) (VolumeUpdateRequest, error) {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update grammar")
	}
	flags := pflag.NewFlagSet("owned Volume update", pflag.ContinueOnError)
	for _, name := range []string{"region", "profile", "team-id", "lang", "timeout"} {
		flags.String(name, "", "")
	}
	flags.String("output", "", "")
	for _, name := range []string{"json", "agent-envelope", "no-color"} {
		flags.Bool(name, false, "")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update flags: %w", err)
	}
	if flags.Lookup("timeout").Changed {
		value, _ := flags.GetString("timeout")
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update timeout")
		}
	}
	pos := flags.Args()
	if len(pos) != 4 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" || pos[2] != "update" || !safeTemplateID(pos[3]) {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update target")
	}
	team, _ := flags.GetString("team-id")
	if !flags.Lookup("team-id").Changed || !safeTemplateID(team) {
		return VolumeUpdateRequest{}, fmt.Errorf("Volume update requires a destination Team selector")
	}
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	output, _ := flags.GetString("output")
	agentEnvelope, _ := flags.GetBool("agent-envelope")
	return VolumeUpdateRequest{ID: pos[3], TeamID: team, TeamSet: true, Region: region, Profile: profile, Output: output, RegionSet: flags.Lookup("region").Changed, ProfileSet: flags.Lookup("profile").Changed, OutputSet: flags.Lookup("output").Changed, AgentEnvelopeSet: flags.Lookup("agent-envelope").Changed, AgentEnvelope: agentEnvelope}, nil
}

// ReserveVolumeTransfer records the destination scope and a pending transfer
// marker before UpdateVolume can reach the provider. The caller persists the
// modified journal in one durable replacement under the replay lock. A crash
// after this write leaves a pending transfer that replay resolves by probing
// every possible Team.
func ReserveVolumeTransfer(meta *report.CleanupJournal, intent TemplateCreateIntent, dest string) error {
	if meta == nil || intent.Resource != "volume" || intent.State != "journaled" || !intent.VolumeObserved ||
		intent.AcceptedVolumeID == "" || len(intent.Deletes) != 1 || intent.Identity.Validate() != nil ||
		intent.RunID != meta.RunID || intent.ExecutionID != meta.ExecutionID ||
		intent.RegionRole != meta.RegionRole || intent.Region != meta.Region ||
		intent.Surface != meta.Surface || intent.EcctlBin != meta.EcctlBin ||
		!safeTemplateID(dest) || dest == intent.TeamID {
		return fmt.Errorf("Volume transfer lacks a journaled single-Team source and a distinct destination")
	}
	if len(meta.VolumeScopeBindings[intent.Name]) != 0 || len(meta.VolumeTransferHistory[intent.Name]) != 0 {
		return fmt.Errorf("Volume already retains a transfer scope")
	}
	for team, name := range meta.VolumeTeamBindings {
		if name == intent.Name || team == dest {
			return fmt.Errorf("Volume transfer destination is already bound")
		}
	}
	matches := 0
	index := -1
	for i, entry := range meta.Entries {
		if entry.VolumeTransferIntent == intent.Name {
			return fmt.Errorf("Volume transfer marker already exists")
		}
		if !entry.TeamCreateConfirmed {
			continue
		}
		id, err := TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return err
		}
		if id == dest && entry.RegionRole == meta.RegionRole && entry.Region == meta.Region && entry.ExecutionID == meta.ExecutionID && !entry.TeamReplayAttempted {
			matches++
			index = i
		}
	}
	if matches != 1 || index < 0 {
		return fmt.Errorf("Volume transfer destination lacks one confirmed Team ownership")
	}
	if err := CheckVolumeTeamCreatorSource(*meta, dest, intent); err != nil {
		return err
	}
	if meta.VolumeTeamBindings == nil {
		meta.VolumeTeamBindings = map[string]string{}
	}
	if meta.VolumeScopeBindings == nil {
		meta.VolumeScopeBindings = map[string][]string{}
	}
	if meta.VolumeTransferHistory == nil {
		meta.VolumeTransferHistory = map[string]map[string]bool{}
	}
	meta.VolumeTeamBindings[dest] = intent.Name
	meta.VolumeScopeBindings[intent.Name] = []string{dest}
	meta.VolumeTransferHistory[intent.Name] = map[string]bool{dest: true}
	meta.Entries[index].VolumeTransferIntent = intent.Name
	meta.Entries[index].VolumeTransferPending = true
	return nil
}

// ReanchorVolumeTransfer settles a confirmed transfer by re-anchoring the
// retained creation intent to the destination Team and clearing the multi-Team
// markers, restoring the single-Team invariant the terminal absence and
// retirement primitives require. The caller must first observe the Volume at
// the destination under its original credential and persist the intent sidecar
// and journal together.
func ReanchorVolumeTransfer(meta *report.CleanupJournal, intent *TemplateCreateIntent, dest string) error {
	if meta == nil || intent == nil || intent.Resource != "volume" || intent.State != "journaled" || !intent.VolumeObserved ||
		intent.AcceptedVolumeID == "" || len(intent.Deletes) != 1 || intent.Identity.Validate() != nil ||
		intent.RunID != meta.RunID || intent.ExecutionID != meta.ExecutionID ||
		intent.RegionRole != meta.RegionRole || intent.Region != meta.Region ||
		intent.Surface != meta.Surface || intent.EcctlBin != meta.EcctlBin ||
		!safeTemplateID(dest) || dest == intent.TeamID {
		return fmt.Errorf("Volume reanchor lacks a journaled source and a distinct destination")
	}
	if meta.VolumeTeamBindings[dest] != intent.Name {
		return fmt.Errorf("Volume reanchor destination binding differs")
	}
	if pending, recorded := meta.VolumeTransferHistory[intent.Name][dest]; !recorded || pending {
		return fmt.Errorf("Volume reanchor requires a settled transfer observation")
	}
	index := -1
	for i, entry := range meta.Entries {
		if entry.VolumeTransferIntent != intent.Name {
			continue
		}
		if entry.VolumeTransferPending || !entry.TeamCreateConfirmed || index >= 0 {
			return fmt.Errorf("Volume reanchor transfer marker is unresolved or ambiguous")
		}
		id, err := TeamDeleteTarget(entry.Teardown)
		if err != nil || id != dest || entry.RegionRole != meta.RegionRole || entry.Region != meta.Region || entry.ExecutionID != meta.ExecutionID {
			return fmt.Errorf("Volume reanchor destination Team entry differs")
		}
		index = i
	}
	if index < 0 {
		return fmt.Errorf("Volume reanchor lacks its destination Team marker")
	}
	if err := CheckVolumeTeamCreatorSource(*meta, dest, *intent); err != nil {
		return err
	}
	intent.TeamID = dest
	delete(meta.VolumeTeamBindings, dest)
	delete(meta.VolumeScopeBindings, intent.Name)
	delete(meta.VolumeTransferHistory, intent.Name)
	pruneEmptyVolumeTransferMarkers(meta)
	meta.Entries[index].VolumeTransferIntent = ""
	meta.Entries[index].VolumeTransferPending = false
	teams, err := VolumePossibleTeams(*meta, *intent)
	if err != nil || len(teams) != 1 || teams[0] != dest {
		return fmt.Errorf("Volume reanchor did not restore a single destination scope")
	}
	return nil
}

// RestoreUnstartedVolumeTransfer reverses a reservation whose UpdateVolume
// invocation never started. The caller must prove no provider call occurred
// (res.Started == false) before clearing the pending markers, leaving the
// Volume anchored to its original Team.
func RestoreUnstartedVolumeTransfer(meta *report.CleanupJournal, intent TemplateCreateIntent, dest string) error {
	if meta == nil || intent.Resource != "volume" || !safeTemplateID(dest) || dest == intent.TeamID {
		return fmt.Errorf("Volume transfer restore lacks its reserved source and destination")
	}
	if meta.VolumeTeamBindings[dest] != intent.Name || meta.VolumeTransferHistory[intent.Name][dest] != true {
		return fmt.Errorf("Volume transfer restore requires the exact pending reservation")
	}
	index := -1
	for i, entry := range meta.Entries {
		if entry.VolumeTransferIntent != intent.Name {
			continue
		}
		if !entry.VolumeTransferPending || index >= 0 {
			return fmt.Errorf("Volume transfer restore marker is resolved or ambiguous")
		}
		id, err := TeamDeleteTarget(entry.Teardown)
		if err != nil || id != dest {
			return fmt.Errorf("Volume transfer restore destination differs")
		}
		index = i
	}
	if index < 0 {
		return fmt.Errorf("Volume transfer restore lacks its destination marker")
	}
	delete(meta.VolumeTeamBindings, dest)
	delete(meta.VolumeScopeBindings, intent.Name)
	delete(meta.VolumeTransferHistory, intent.Name)
	pruneEmptyVolumeTransferMarkers(meta)
	meta.Entries[index].VolumeTransferIntent = ""
	meta.Entries[index].VolumeTransferPending = false
	return nil
}

// The transfer marker maps are omitempty, so a settlement that clears the last
// key must nil the empty map to keep the installed journal byte-identical to
// its decoded form.
func pruneEmptyVolumeTransferMarkers(meta *report.CleanupJournal) {
	if len(meta.VolumeTeamBindings) == 0 {
		meta.VolumeTeamBindings = nil
	}
	if len(meta.VolumeScopeBindings) == 0 {
		meta.VolumeScopeBindings = nil
	}
	if len(meta.VolumeTransferHistory) == 0 {
		meta.VolumeTransferHistory = nil
	}
}
