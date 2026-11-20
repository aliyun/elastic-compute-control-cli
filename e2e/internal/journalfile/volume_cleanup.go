package journalfile

import (
	"context"
	"fmt"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

// VolumeDeleteRequest is the supported spelling of an authored Volume delete.
// The finalizer itself has stricter validation through TemplateCreateIntent.
type VolumeDeleteRequest struct {
	ID, TeamID, Region, Profile, Output                         string
	TeamSet, RegionSet, ProfileSet, OutputSet, AgentEnvelopeSet bool
	AgentEnvelope, NoWait                                       bool
}

func ParseVolumeDeleteRequest(command string) (VolumeDeleteRequest, error) {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" {
		return VolumeDeleteRequest{}, fmt.Errorf("invalid Volume delete grammar")
	}
	flags := pflag.NewFlagSet("owned Volume delete", pflag.ContinueOnError)
	for _, name := range []string{"region", "profile", "team-id", "lang", "timeout"} {
		flags.String(name, "", "")
	}
	flags.String("output", "", "")
	for _, name := range []string{"no-wait", "json", "agent-envelope", "no-color"} {
		flags.Bool(name, false, "")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return VolumeDeleteRequest{}, fmt.Errorf("invalid Volume delete flags: %w", err)
	}
	if flags.Lookup("timeout").Changed {
		value, _ := flags.GetString("timeout")
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return VolumeDeleteRequest{}, fmt.Errorf("invalid Volume delete timeout")
		}
	}
	pos := flags.Args()
	if len(pos) != 4 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" || (pos[2] != "delete" && pos[2] != "rm" && pos[2] != "remove") || !safeTemplateID(pos[3]) {
		return VolumeDeleteRequest{}, fmt.Errorf("invalid Volume delete target")
	}
	team, _ := flags.GetString("team-id")
	if flags.Lookup("team-id").Changed && !safeTemplateID(team) {
		return VolumeDeleteRequest{}, fmt.Errorf("invalid Volume delete Team selector")
	}
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	output, _ := flags.GetString("output")
	agentEnvelope, _ := flags.GetBool("agent-envelope")
	noWait, _ := flags.GetBool("no-wait")
	return VolumeDeleteRequest{ID: pos[3], TeamID: team, TeamSet: flags.Lookup("team-id").Changed, Region: region, Profile: profile, Output: output, RegionSet: flags.Lookup("region").Changed, ProfileSet: flags.Lookup("profile").Changed, OutputSet: flags.Lookup("output").Changed, AgentEnvelopeSet: flags.Lookup("agent-envelope").Changed, AgentEnvelope: agentEnvelope, NoWait: noWait}, nil
}

// A verified permission denial cannot have accepted DeleteVolume. It is safe
// to restore the attempt marker only when the exact owned ID remains present.
func VolumeDefinitiveDeleteRejection(result execpkg.Result) bool {
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
	for _, key := range []string{"volume", "response", "product", "operation", "recovery_command"} {
		if _, present := root[key]; present {
			return false
		}
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action_name"] != "DeleteVolume" || action["provider_verified"] != true || action["code"] != "Forbidden" && action["code"] != "NoPermission" {
		return false
	}
	requestID, ok := action["request_id"].(string)
	return ok && validVolumeProvenanceID(requestID)
}

func volumeActionCode(root map[string]any, action string) bool {
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return false
	}
	entry, ok := actions[0].(map[string]any)
	if !ok || entry["action_name"] != action {
		return false
	}
	code, _ := entry["code"].(string)
	requestID, _ := entry["request_id"].(string)
	return code == "VolumeNotFound" && entry["provider_verified"] == true && validVolumeProvenanceID(requestID)
}

func validVolumeProvenanceID(value string) bool {
	if value == "" || len(value) > 256 || strings.Contains(value, "://") {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("-._:/+=", character) {
			continue
		}
		return false
	}
	return true
}

func quoteVolumeArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// VolumePossibleTeams includes the creation Team and every destination
// journaled before a transfer. A missing Team finalizer or inconsistent marker
// is ambiguous even when another scope returns NotFound.
func VolumePossibleTeams(meta report.CleanupJournal, intent TemplateCreateIntent) ([]string, error) {
	if intent.Resource != "volume" || !safeTemplateID(intent.TeamID) || intent.Name == "" || intent.RunID != meta.RunID || intent.ExecutionID != meta.ExecutionID {
		return nil, fmt.Errorf("Volume scope lacks its retained creation binding")
	}
	teams := append([]string{intent.TeamID}, meta.VolumeScopeBindings[intent.Name]...)
	seen := make(map[string]bool, len(teams))
	for _, team := range teams {
		if !safeTemplateID(team) || seen[team] {
			return nil, fmt.Errorf("Volume scope has an invalid or duplicate Team")
		}
		seen[team] = true
		matches := 0
		for _, entry := range meta.Entries {
			if !entry.TeamCreateConfirmed {
				continue
			}
			id, err := TeamDeleteTarget(entry.Teardown)
			if err != nil {
				return nil, err
			}
			if id == team && (entry.VolumeTransferPending && entry.VolumeTransferIntent == "" || entry.VolumeTransferIntent != "" && meta.VolumeTeamBindings[team] != entry.VolumeTransferIntent) {
				return nil, fmt.Errorf("Volume transfer marker differs from its Team binding")
			}
			if id == team && entry.RegionRole == meta.RegionRole && entry.Region == meta.Region && entry.ExecutionID == meta.ExecutionID && !entry.TeamReplayAttempted {
				matches++
			}
		}
		if matches != 1 {
			return nil, fmt.Errorf("Volume scope Team ownership is missing or ambiguous: %s", team)
		}
	}
	for team, name := range meta.VolumeTeamBindings {
		if name == intent.Name && !seen[team] {
			return nil, fmt.Errorf("Volume transfer marker lacks a possible scope")
		}
	}
	history := meta.VolumeTransferHistory[intent.Name]
	for team := range history {
		if !seen[team] {
			return nil, fmt.Errorf("Volume transfer history lacks a possible scope")
		}
	}
	for _, team := range teams[1:] {
		if _, recorded := history[team]; !recorded && meta.VolumeTeamBindings[team] != intent.Name {
			return nil, fmt.Errorf("Volume possible scope lacks transfer evidence")
		}
	}
	return teams, nil
}

// CheckVolumeDeleteTeamEvidence keeps each possible scope tied to the
// original Team creator and its current credential configuration.
func CheckVolumeDeleteTeamEvidence(meta report.CleanupJournal, intent TemplateCreateIntent, teams []string, env []string) error {
	if len(teams) == 0 {
		return fmt.Errorf("Volume delete lacks retained Team scopes")
	}
	for _, team := range teams {
		if err := CheckVolumeTeamCreatorSource(meta, team, intent); err != nil {
			return err
		}
		if meta.VolumeFixtureBinding != nil || len(meta.VolumeTeamCreators) != 0 {
			if err := ValidateQuotaBinding(meta.VolumeTeamCreators[team], env); err != nil {
				return err
			}
		}
	}
	return nil
}

// A scoped GetVolume absence can retire only a Volume whose journal still
// records exactly its original Team and no possible transfer. Callers must
// obtain a fresh, provider-verified VolumeNotFound for that exact ID and Team
// under the original credential before using this eligibility check.
func VolumeAbsenceTerminal(meta report.CleanupJournal, intent TemplateCreateIntent, entry report.Resource) bool {
	if intent.Resource != "volume" || intent.State != "journaled" || !intent.VolumeObserved ||
		intent.AcceptedVolumeID == "" || len(intent.Deletes) != 1 || intent.Identity.Validate() != nil ||
		intent.RunID != meta.RunID || intent.ExecutionID != meta.ExecutionID ||
		intent.RegionRole != meta.RegionRole || intent.Region != meta.Region ||
		intent.Surface != meta.Surface || intent.EcctlBin != meta.EcctlBin ||
		entry.Scope != intent.CaseScope || entry.ExecutionID != meta.ExecutionID || entry.RegionRole != meta.RegionRole || entry.Region != meta.Region ||
		intent.ValidateDelete(entry.Teardown) != nil || !sameTemplateDelete(intent.Deletes[0], entry.Teardown) ||
		len(meta.VolumeScopeBindings[intent.Name]) != 0 || len(meta.VolumeTransferHistory[intent.Name]) != 0 {
		return false
	}
	for team, name := range meta.VolumeTeamBindings {
		if name == intent.Name || team == intent.TeamID && name != "" {
			return false
		}
	}
	for _, candidate := range meta.Entries {
		if candidate.VolumeTransferIntent == intent.Name || candidate.VolumeTransferPending && candidate.Teardown == entry.Teardown {
			return false
		}
	}
	request, err := ParseVolumeDeleteRequest(entry.Teardown)
	if err != nil || request.ID != intent.AcceptedVolumeID {
		return false
	}
	count := 0
	for _, candidate := range meta.Entries {
		if candidate == entry {
			count++
		}
	}
	if count != 1 {
		return false
	}
	teams, err := VolumePossibleTeams(meta, intent)
	return err == nil && len(teams) == 1 && teams[0] == intent.TeamID
}

func volumeTerminalReceipt(meta report.CleanupJournal, intent TemplateCreateIntent) report.VolumeTerminalReceipt {
	return report.VolumeTerminalReceipt{
		VolumeID: intent.AcceptedVolumeID, TeamID: intent.TeamID, Teardown: intent.Deletes[0],
		RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole,
		Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin,
		Profile: intent.Profile, Identity: intent.Identity,
	}
}

// RetireVolumeFinalizer updates the terminal receipt and finalizer together;
// its caller persists the modified journal in one durable replacement. The
// caller must first establish exact scoped VolumeNotFound under the original
// credential, then preserve that proof in the completion receipt.
func RetireVolumeFinalizer(meta *report.CleanupJournal, intent TemplateCreateIntent, entry report.Resource) error {
	if meta == nil || intent.Resource != "volume" || intent.State != "journaled" || len(intent.Deletes) != 1 ||
		intent.AcceptedVolumeID == "" || intent.ValidateDelete(entry.Teardown) != nil ||
		!VolumeAbsenceTerminal(*meta, intent, entry) || CheckVolumeTeamCreatorSource(*meta, intent.TeamID, intent) != nil {
		return fmt.Errorf("Volume terminal proof or retained creator binding is unavailable")
	}
	if _, exists := meta.VolumeTerminalReceipts[intent.Name]; exists {
		return fmt.Errorf("Volume terminal receipt already exists")
	}
	index := -1
	for i, candidate := range meta.Entries {
		if !sameTemplateDelete(candidate.Teardown, entry.Teardown) {
			continue
		}
		if candidate != entry || index >= 0 {
			return fmt.Errorf("Volume finalizer changed or became ambiguous")
		}
		index = i
	}
	if index < 0 {
		return fmt.Errorf("Volume finalizer is missing")
	}
	if meta.VolumeTerminalReceipts == nil {
		meta.VolumeTerminalReceipts = make(map[string]report.VolumeTerminalReceipt)
	}
	meta.VolumeTerminalReceipts[intent.Name] = volumeTerminalReceipt(*meta, intent)
	meta.Entries = append(meta.Entries[:index], meta.Entries[index+1:]...)
	for team := range meta.VolumeTransferHistory[intent.Name] {
		meta.VolumeTransferHistory[intent.Name][team] = false
	}
	for i := range meta.Entries {
		if meta.Entries[i].VolumeTransferIntent == intent.Name {
			meta.Entries[i].VolumeTransferPending = false
		}
	}
	return nil
}

// A verified GetVolume in the recorded destination resolves an uncertain
// transfer without resending UpdateVolume. A read in the source Team does not.
func ResolveVolumeTransferObservation(meta *report.CleanupJournal, intent TemplateCreateIntent, currentTeam, status string) (bool, error) {
	if meta == nil || intent.Resource != "volume" || !safeTemplateID(currentTeam) {
		return false, fmt.Errorf("Volume transfer observation lacks a current owned Team")
	}
	pendingTeam := ""
	for team, pending := range meta.VolumeTransferHistory[intent.Name] {
		if !pending {
			continue
		}
		if pendingTeam != "" {
			return false, fmt.Errorf("Volume has multiple unresolved transfers")
		}
		pendingTeam = team
	}
	index := -1
	for i, entry := range meta.Entries {
		if !entry.VolumeTransferPending || entry.VolumeTransferIntent != intent.Name {
			continue
		}
		if index >= 0 {
			return false, fmt.Errorf("Volume has multiple unresolved transfers")
		}
		index = i
	}
	if index < 0 && pendingTeam == "" {
		return false, nil
	}
	// The exact destination owner is sufficient for cleanup. Readiness
	// remains visible to the caller but does not change that ownership.
	if status != "AVAILABLE" && status != "ERROR" {
		return false, fmt.Errorf("Volume transfer destination is not yet terminal for cleanup")
	}
	if pendingTeam != "" && pendingTeam != currentTeam {
		return false, fmt.Errorf("Volume transfer destination is not yet observed")
	}
	if index >= 0 {
		team, err := TeamDeleteTarget(meta.Entries[index].Teardown)
		if err != nil || team != currentTeam {
			return false, fmt.Errorf("Volume transfer destination is not yet observed")
		}
		if recorded, exists := meta.VolumeTransferHistory[intent.Name][team]; exists && !recorded {
			return false, fmt.Errorf("Volume transfer pending markers disagree")
		}
		meta.Entries[index].VolumeTransferPending = false
	}
	if pendingTeam != "" {
		meta.VolumeTransferHistory[intent.Name][pendingTeam] = false
	}
	return true, nil
}

// ProbeVolumeScopes uses the original credential and an exact Team selector
// for every scope that could hold the ID. Only a specific GetVolume provider
// absence response proves a scope empty. A present Volume must retain the
// reserved name as well as its ID and Team. A fresh delete must not be sent
// while an observed Volume is already deleting.
func ProbeVolumeScopes(ctx context.Context, cfg execpkg.Config, id, name, region, profile string, teams []string, run func(context.Context, execpkg.Config, string) execpkg.Result) (string, bool, error) {
	current, status, absent, err := ProbeVolumeScopesWithStatus(ctx, cfg, id, name, region, profile, teams, run)
	if err == nil && status == "DELETING" {
		return "", false, fmt.Errorf("Volume is already deleting")
	}
	return current, absent, err
}

func volumeTransitionStatus(status string) bool {
	return status != "" && status != "ERROR" && status != "DELETING"
}

// ProbeVolumeScopesWithStatus returns the status from the same exact scope
// observation that establishes the Volume's current Team.
func ProbeVolumeScopesWithStatus(ctx context.Context, cfg execpkg.Config, id, name, region, profile string, teams []string, run func(context.Context, execpkg.Config, string) execpkg.Result) (string, string, bool, error) {
	if !safeTemplateID(id) || name == "" || region == "" || profile == "" || cfg.ExpectedIdentity == nil || len(teams) == 0 || cfg.Region != region || cfg.Profile != profile {
		return "", "", false, fmt.Errorf("Volume scope probe lacks pinned selectors")
	}
	current := ""
	status := ""
	present := 0
	seen := make(map[string]bool, len(teams))
	for _, team := range teams {
		if !safeTemplateID(team) || seen[team] {
			return "", "", false, fmt.Errorf("Volume scope probe has an invalid Team")
		}
		seen[team] = true
		command := "ecctl sandbox volume get " + quoteVolumeArg(id) + " --team-id " + quoteVolumeArg(team) + " --region " + quoteVolumeArg(region) + " --profile " + quoteVolumeArg(profile) + " --output=json --agent-envelope=false"
		result := run(ctx, cfg, command)
		root, ok := result.JSON.(map[string]any)
		if result.Err != nil || !result.Started || !ok {
			return "", "", false, fmt.Errorf("Volume scope read is unavailable")
		}
		if result.Exit == 4 {
			failure, _ := root["error"].(map[string]any)
			_, hasVolume := root["volume"]
			_, hasResponse := root["response"]
			if hasVolume || hasResponse || root["product"] != nil || root["operation"] != nil || failure["kind"] != "not_found" || failure["code"] != "NotFound" || !volumeActionCode(root, "GetVolume") {
				return "", "", false, fmt.Errorf("Volume scope absence is unproven")
			}
			continue
		}
		volume, _ := root["volume"].(map[string]any)
		if result.Exit != 0 || volume["id"] != id || volume["name"] != name || volume["team_id"] != team || root["error"] != nil {
			return "", "", false, fmt.Errorf("Volume scope identity is missing or ambiguous")
		}
		seenStatus, _ := volume["status"].(string)
		if present >= 2 || present > 0 && (!volumeTransitionStatus(status) || !volumeTransitionStatus(seenStatus)) {
			return "", "", false, fmt.Errorf("Volume scope identity is missing or ambiguous")
		}
		if present == 0 {
			current, status = team, seenStatus
		}
		present++
	}
	if present == 2 {
		return "", "", false, fmt.Errorf("Volume is visible in two transfer scopes")
	}
	return current, status, current == "", nil
}

// WaitVolumeAbsent observes an accepted or previously attempted delete until
// every pinned Team scope reports specific absence. That read remains
// provisional across external Team changes and never issues a delete.
func WaitVolumeAbsent(ctx context.Context, cfg execpkg.Config, id, name, region, profile string, teams []string, run func(context.Context, execpkg.Config, string) execpkg.Result) bool {
	for {
		if ctx.Err() != nil {
			return false
		}
		_, status, absent, err := ProbeVolumeScopesWithStatus(ctx, cfg, id, name, region, profile, teams, run)
		if err != nil {
			return false
		}
		if status == "ERROR" {
			return false
		}
		if absent {
			return true
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// A Volume-owned Team finalizer is consumed only after a fresh GetTeam under
// its retained creator credential reports the exact provider absence.
func ProbeVolumeTeamAbsent(ctx context.Context, cfg execpkg.Config, id, region, profile string, run func(context.Context, execpkg.Config, string) execpkg.Result) error {
	if !safeTemplateID(id) || region == "" || profile == "" || cfg.ExpectedIdentity == nil || cfg.Region != region || cfg.Profile != profile {
		return fmt.Errorf("Volume Team absence probe lacks pinned selectors")
	}
	command := volumeTeamAbsenceCommand(id, region, profile)
	result := run(ctx, cfg, command)
	return volumeTeamAbsenceResult(result)
}

func volumeTeamAbsenceCommand(id, region, profile string) string {
	return "ecctl sandbox team get " + quoteVolumeArg(id) + " --region " + quoteVolumeArg(region) + " --profile " + quoteVolumeArg(profile) + " --output=json --agent-envelope=false"
}

func volumeTeamAbsenceResult(result execpkg.Result) error {
	root, ok := result.JSON.(map[string]any)
	if !result.Started || result.Err != nil || !ok || result.Exit != 4 {
		return fmt.Errorf("Volume Team absence is unproven")
	}
	failure, _ := root["error"].(map[string]any)
	_, hasTeam := root["team"]
	_, hasResponse := root["response"]
	if hasTeam || hasResponse || root["product"] != nil || root["operation"] != nil || failure["kind"] != "not_found" || failure["code"] != "NotFound" {
		return fmt.Errorf("Volume Team absence response is contradictory")
	}
	actions, ok := root["actions"].([]any)
	if !ok || len(actions) != 1 {
		return fmt.Errorf("Volume Team absence lacks one provider action")
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["action_name"] != "GetTeam" || action["code"] != "TeamNotFound" || action["provider_verified"] != true {
		return fmt.Errorf("Volume Team absence lacks exact provider provenance")
	}
	requestID, _ := action["request_id"].(string)
	if !validVolumeProvenanceID(requestID) {
		return fmt.Errorf("Volume Team absence lacks a valid provider request ID")
	}
	return nil
}

// OutstandingVolumeChild validates the native ledger before deciding whether
// an owned Team may be deleted. The conservative fence covers uncertain
// transfers between any two Teams owned by the same execution.
func OutstandingVolumeChild(path string, meta report.CleanupJournal, env []string) (bool, error) {
	resolved, err := ResolveJournalPath(path)
	if err != nil {
		return false, err
	}
	actual, _, err := loadTemplateLedger(resolved, meta)
	if err != nil {
		return false, err
	}
	intents, err := readTemplateCreateIntents(resolved, actual, env)
	if err != nil {
		return false, err
	}
	acceptedDeletes := make([]string, 0)
	seenReceipts := make(map[string]bool)
	for _, intent := range intents {
		if intent.Resource != "volume" || intent.AcceptedVolumeID == "" {
			continue
		}
		if intent.State != "journaled" || len(intent.Deletes) != 1 || !oneNativeIntentReference(actual, resolved, intent.Name) {
			return false, fmt.Errorf("accepted Volume lacks one retained finalizer")
		}
		for _, earlier := range acceptedDeletes {
			if sameTemplateDelete(earlier, intent.Deletes[0]) {
				return false, fmt.Errorf("accepted Volumes share an ambiguous finalizer")
			}
		}
		acceptedDeletes = append(acceptedDeletes, intent.Deletes[0])
		matches := 0
		for _, entry := range actual.Entries {
			if !sameTemplateDelete(intent.Deletes[0], entry.Teardown) {
				continue
			}
			if entry.ExecutionID != intent.ExecutionID || entry.RegionRole != intent.RegionRole || entry.Region != intent.Region {
				return false, fmt.Errorf("accepted Volume finalizer binding changed")
			}
			matches++
		}
		receipt, retired := actual.VolumeTerminalReceipts[intent.Name]
		if matches == 1 && retired {
			return false, fmt.Errorf("accepted Volume has both a finalizer and terminal receipt")
		}
		if matches == 0 {
			if !retired || !intent.VolumeObserved || len(actual.VolumeScopeBindings[intent.Name]) != 0 || len(actual.VolumeTransferHistory[intent.Name]) != 0 ||
				receipt != volumeTerminalReceipt(actual, intent) || CheckRetiredVolumeTeamCreatorSource(actual, intent.TeamID, intent) != nil {
				return false, fmt.Errorf("accepted Volume lacks an exact terminal receipt")
			}
			seenReceipts[intent.Name] = true
		} else if matches != 1 {
			return false, fmt.Errorf("accepted Volume finalizer is ambiguous")
		}
	}
	if len(seenReceipts) != len(actual.VolumeTerminalReceipts) {
		return false, fmt.Errorf("Volume terminal receipt lacks an accepted creation binding")
	}
	for _, destinations := range actual.VolumeTransferHistory {
		for _, pending := range destinations {
			if pending {
				return true, nil
			}
		}
	}
	for _, entry := range actual.Entries {
		if IsNativeVolumeDelete(entry.Teardown) || entry.VolumeTransferPending {
			return true, nil
		}
	}
	return false, nil
}
