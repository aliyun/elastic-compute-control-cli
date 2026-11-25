package journalfile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// Provider-created Templates have no CreateTemplate intent. This private
// inventory records only children of an accepted, run-owned Volume Team.
type volumeTeamChildren struct {
	Version            int               `json:"version"`
	JournalPath        string            `json:"journal_path"`
	RunID              string            `json:"run_id"`
	ExecutionID        string            `json:"execution_id"`
	Region             string            `json:"region"`
	Surface            string            `json:"surface"`
	EcctlBin           string            `json:"ecctl_bin"`
	EcctlBinSHA256     string            `json:"ecctl_bin_sha256"`
	TeamID             string            `json:"team_id"`
	TeamName           string            `json:"team_name"`
	TeamDescription    string            `json:"team_description"`
	TeamCreatedAfter   time.Time         `json:"team_created_after"`
	TeamCreatedAt      time.Time         `json:"team_created_at"`
	InitialListSHA256  string            `json:"initial_list_sha256"`
	Children           []volumeTeamChild `json:"children"`
	EmptyListSHA256    string            `json:"empty_list_sha256,omitempty"`
	EmptyGeneration    int               `json:"empty_generation,omitempty"`
	TeamAttempts       int               `json:"team_attempts,omitempty"`
	ConflictSHA256     string            `json:"conflict_sha256,omitempty"`
	ConflictEvidence   string            `json:"conflict_evidence,omitempty"`
	ConflictAt         time.Time         `json:"conflict_at,omitempty"`
	ConflictGeneration int               `json:"conflict_generation,omitempty"`
}

type volumeTeamChild struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"` // pending, attempted, absent
}

func volumeTeamDefaultName(name string) bool {
	return name == "base" || name == "code-interpreter-v1"
}

// ErrOwnedTeamChildNoIntent marks a confirmed plain Team that has no retained
// create-intent proof. Provider-default Template cleanup is only authorized by
// that proof; without it the finalizer remains a plain Team delete.
var ErrOwnedTeamChildNoIntent = errors.New("owned Team child cleanup has no retained create intent")

// teamChildrenReceipt resolves the accepted creation link for any run-owned
// Team. Volume Teams keep their pinned fixture credential; other confirmed
// Teams prove ownership through their retained create intent and the run
// identity, so provider-default Template cleanup applies to every owned Team.
func teamChildrenReceipt(meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config) (volumeTeamReceipt, string, error) {
	if entry.VolumeTeamBound {
		return volumeTeamChildrenReceipt(meta, entry, cfg)
	}
	return ownedTeamChildrenReceipt(meta, entry, cfg)
}

// ownedTeamCreateIntent proves a confirmed non-Volume Team came from this
// execution by matching its retained create intent sidecar. The intent is
// written before CreateTeam dispatch and kept for the run, so the rendered
// delete, nonce name, case scope and run binding pin one exact owner without a
// fixture credential. The intent must also retain the run credential binding
// captured before dispatch; legacy intents without it stay plain deletes.
func ownedTeamCreateIntent(meta report.CleanupJournal, entry report.Resource) (volumeTeamReceipt, string, error) {
	id, err := TeamDeleteTarget(entry.Teardown)
	if err != nil {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup lacks a validated delete target: %v", err)
	}
	if !entry.TeamCreateConfirmed || entry.VolumeTeamBound || !safeTemplateID(id) || !filepath.IsAbs(meta.JournalPath) || filepath.Clean(meta.JournalPath) != meta.JournalPath {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup lacks confirmed creation ownership")
	}
	if entry.ExecutionID != meta.ExecutionID || entry.Region != meta.Region || entry.RegionRole != meta.RegionRole {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup entry differs from journal ownership")
	}
	root, err := os.OpenRoot(filepath.Dir(meta.JournalPath))
	if err != nil {
		return volumeTeamReceipt{}, "", err
	}
	defer root.Close()
	entries, err := readRootInventory(root)
	if err != nil {
		return volumeTeamReceipt{}, "", err
	}
	base := filepath.Base(meta.JournalPath)
	var receipt volumeTeamReceipt
	matches := 0
	for _, dirEntry := range entries {
		name := dirEntry.Name()
		if !strings.HasPrefix(name, base+".team-create-") || !strings.HasSuffix(name, ".json") || !dirEntry.Type().IsRegular() {
			continue
		}
		nonce := strings.TrimSuffix(strings.TrimPrefix(name, base+".team-create-"), ".json")
		if len(nonce) != 24 {
			continue
		}
		if _, err := hex.DecodeString(nonce); err != nil {
			continue
		}
		snapshot, err := ReadPrivateRecoverySnapshotRoot(root, name)
		if err != nil {
			return volumeTeamReceipt{}, "", err
		}
		if _, err := execpkg.DecodeStrictJSON(snapshot.Body); err != nil {
			return volumeTeamReceipt{}, "", err
		}
		var candidate volumeTeamReceipt
		decoder := json.NewDecoder(bytes.NewReader(snapshot.Body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&candidate); err != nil {
			return volumeTeamReceipt{}, "", err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return volumeTeamReceipt{}, "", fmt.Errorf("owned Team create intent has trailing data")
		}
		if candidate.Version != 1 || candidate.CreatedAt.IsZero() || candidate.Dispatched || candidate.DispatchProtocol != 0 || candidate.AcceptedID != "" || candidate.VolumeBinding != nil || candidate.OwnerBinding == nil || candidate.ProbeBinSHA256 != "" || candidate.Name != "ecctl-t-"+nonce || candidate.Description != "ecctl E2E Team owner="+nonce || candidate.RunID != meta.RunID || candidate.ExecutionID != meta.ExecutionID || candidate.Region != meta.Region || candidate.Surface != meta.Surface || candidate.EcctlBin != meta.EcctlBin || candidate.CaseScope == "" || strings.ContainsAny(candidate.CaseScope, "\r\n") || candidate.CaseScope != entry.Scope {
			continue
		}
		if len(candidate.Input) == 0 || candidate.Input[0] != "ecctl" || strings.Count(candidate.Teardown, "intent-validation") != 1 {
			continue
		}
		command := strings.Replace(candidate.Teardown, "intent-validation", id, 1)
		if target, err := TeamDeleteTarget(command); err != nil || target != id || command != entry.Teardown {
			continue
		}
		receipt = candidate
		matches++
	}
	if matches == 0 {
		return volumeTeamReceipt{}, "", ErrOwnedTeamChildNoIntent
	}
	if matches != 1 {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup has ambiguous accepted creation links")
	}
	return receipt, id, nil
}

// ownedTeamChildrenReceipt additionally pins the presented cleanup credential
// to the run identity retained in the matched create intent.
func ownedTeamChildrenReceipt(meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config) (volumeTeamReceipt, string, error) {
	if cfg.Bin != meta.EcctlBin || cfg.ExpectedBinSHA256 != meta.EcctlBinSHA256 || !validHistoricalDigest(meta.EcctlBinSHA256) || cfg.Region != meta.Region || cfg.ExpectedIdentity == nil {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup lacks its pinned binary and identity")
	}
	receipt, id, err := ownedTeamCreateIntent(meta, entry)
	if err != nil {
		return volumeTeamReceipt{}, "", err
	}
	if cfg.Profile != receipt.OwnerBinding.Profile || *cfg.ExpectedIdentity != receipt.OwnerBinding.Identity {
		return volumeTeamReceipt{}, "", fmt.Errorf("owned Team child cleanup credential differs from its retained create identity")
	}
	return receipt, id, nil
}

// OwnedTeamCreatorConfig rebuilds the pinned run credential for a confirmed
// opt-in plain Team from its retained create intent, probing the selected
// profile and trusted principal before any provider call. Volume Teams keep
// their fixture path; Teams without a retained owner binding stay plain
// deletes.
func OwnedTeamCreatorConfig(ctx context.Context, meta report.CleanupJournal, cfg execpkg.Config, teamID string) (execpkg.Config, bool, error) {
	count := 0
	var finalizer report.Resource
	for _, entry := range meta.Entries {
		if !entry.TeamCreateConfirmed {
			continue
		}
		id, err := TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return cfg, false, err
		}
		if id == teamID {
			count++
			finalizer = entry
		}
	}
	if count > 1 {
		return cfg, false, fmt.Errorf("owned Team creator is ambiguous")
	}
	if count == 0 || finalizer.VolumeTeamBound || !finalizer.TeamProviderTemplateCleanup {
		return cfg, false, nil
	}
	receipt, _, err := ownedTeamCreateIntent(meta, finalizer)
	if errors.Is(err, ErrOwnedTeamChildNoIntent) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	bound, err := ProbeQuotaBinding(ctx, receipt.OwnerBinding, cfg)
	return bound, true, err
}

// OwnedTeamChildCleanupApplicable reports whether a confirmed Team finalizer
// holds the ownership proof required for provider-default Template cleanup.
// Volume Teams always qualify through their fixture receipt; plain Teams
// require a retained create intent, so legacy finalizers stay plain deletes.
func OwnedTeamChildCleanupApplicable(meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config) bool {
	if entry.VolumeTeamBound {
		return true
	}
	_, _, err := ownedTeamChildrenReceipt(meta, entry, cfg)
	return err == nil
}

func volumeTeamChildrenPath(path, teamID string) string {
	digest := sha256.Sum256([]byte(teamID))
	return path + ".team-children-" + hex.EncodeToString(digest[:]) + ".json"
}

// VolumeTeamChildInventoryPresent distinguishes an older attempted finalizer
// from a new finalizer that has recorded its child inventory before mutation.
func VolumeTeamChildInventoryPresent(path, teamID string) (bool, error) {
	if !safeTemplateID(teamID) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false, fmt.Errorf("Volume Team child inventory path is invalid")
	}
	_, err := ReadPrivateRecoverySnapshot(volumeTeamChildrenPath(path, teamID))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func volumeTeamChildrenReceipt(meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config) (volumeTeamReceipt, string, error) {
	id, err := TeamDeleteTarget(entry.Teardown)
	if err != nil {
		return volumeTeamReceipt{}, "", fmt.Errorf("Volume Team child cleanup lacks its accepted creation link: %v", err)
	}
	if err := CheckVolumeTeamCreationLink(meta, id, &entry); err != nil {
		return volumeTeamReceipt{}, "", fmt.Errorf("Volume Team child cleanup lacks its accepted creation link: %w", err)
	}
	binding := meta.VolumeTeamCreators[id]
	if binding == nil || cfg.Bin != meta.EcctlBin || cfg.ExpectedBinSHA256 != meta.EcctlBinSHA256 || !validHistoricalDigest(meta.EcctlBinSHA256) || cfg.Region != meta.Region || cfg.Profile != binding.Profile || cfg.ExpectedIdentity == nil || *cfg.ExpectedIdentity != binding.Identity {
		return volumeTeamReceipt{}, "", fmt.Errorf("Volume Team child cleanup lacks its pinned creator and binary")
	}
	var name string
	for candidate, target := range meta.VolumeTeamCreateIDs {
		if target == id {
			name = candidate
			break
		}
	}
	if name == "" {
		return volumeTeamReceipt{}, "", fmt.Errorf("Volume Team child cleanup lacks its nonce")
	}
	snapshot, err := ReadPrivateRecoverySnapshot(meta.JournalPath + ".team-create-" + name[8:] + ".json")
	if err != nil {
		return volumeTeamReceipt{}, "", err
	}
	var receipt volumeTeamReceipt
	if err := json.Unmarshal(snapshot.Body, &receipt); err != nil || receipt.AcceptedID != id {
		return volumeTeamReceipt{}, "", fmt.Errorf("Volume Team child cleanup accepted receipt changed: %v", err)
	}
	return receipt, id, nil
}

func volumeTeamChildResult(result execpkg.Result, digest string) (map[string]any, error) {
	root, ok := result.JSON.(map[string]any)
	if !result.Started || result.Err != nil || !ok || result.VerifiedBinSHA256 != digest {
		return nil, fmt.Errorf("Volume Team child command lacks a verified result")
	}
	return root, nil
}

func volumeTeamChildList(ctx context.Context, meta report.CleanupJournal, cfg execpkg.Config, teamID, teamName string, run func(context.Context, execpkg.Config, string) execpkg.Result, lower time.Time) ([]volumeTeamChild, string, error) {
	var children []volumeTeamChild
	var pages []any
	seenIDs, seenNames, seenTokens := map[string]bool{}, map[string]bool{}, map[string]bool{}
	token := ""
	for page := 0; page < 16; page++ {
		command := "ecctl sandbox template list --openapi --team-id " + quoteVolumeArg(teamID) + " --limit 100 --region " + quoteVolumeArg(meta.Region) + " --profile " + quoteVolumeArg(cfg.Profile) + " --output=json --agent-envelope=false"
		if token != "" {
			command += " --next-token " + quoteVolumeArg(token)
		}
		result := run(ctx, cfg, command)
		root, err := volumeTeamChildResult(result, meta.EcctlBinSHA256)
		if err != nil {
			return nil, "", err
		}
		if result.Exit != 0 || root["error"] != nil {
			return nil, "", fmt.Errorf("Volume Team Template list did not succeed")
		}
		items, ok := root["templates"].([]any)
		pagination, pageOK := root["pagination"].(map[string]any)
		hasMore, moreOK := pagination["has_more"].(bool)
		if !ok || !pageOK || !moreOK || len(children)+len(items) > 64 {
			return nil, "", fmt.Errorf("Volume Team Template inventory is incomplete or unbounded")
		}
		for _, value := range items {
			item, ok := value.(map[string]any)
			id, idOK := item["id"].(string)
			name, nameOK := item["name"].(string)
			created, timeOK := item["created_time"].(string)
			createdAt, parseErr := time.Parse(time.RFC3339Nano, created)
			if !ok || !idOK || !safeTemplateID(id) || seenIDs[id] || !nameOK || !volumeTeamDefaultName(name) || seenNames[name] || item["team_id"] != teamID || item["team_name"] != teamName || !timeOK || parseErr != nil || createdAt.Before(lower) || createdAt.After(lower.Add(2*time.Minute)) || createdAt.After(time.Now().Add(2*time.Minute)) {
				return nil, "", fmt.Errorf("Volume Team Template is not proven to be a new child of the accepted Team")
			}
			seenIDs[id] = true
			seenNames[name] = true
			children = append(children, volumeTeamChild{ID: id, Name: name, CreatedAt: createdAt.UTC(), State: "pending"})
		}
		pages = append(pages, root)
		if !hasMore {
			if next, present := pagination["next_token"]; present && next != "" {
				return nil, "", fmt.Errorf("terminal Volume Team Template page has a continuation")
			}
			body, _ := json.Marshal(pages)
			digest := sha256.Sum256(body)
			return children, hex.EncodeToString(digest[:]), nil
		}
		next, ok := pagination["next_token"].(string)
		if !ok || next == "" || len(next) > 4096 || seenTokens[next] {
			return nil, "", fmt.Errorf("Volume Team Template pagination is incomplete or cyclic")
		}
		seenTokens[next] = true
		token = next
	}
	return nil, "", fmt.Errorf("Volume Team Template inventory exceeds page limit")
}

func readVolumeTeamChildren(path string, meta report.CleanupJournal, receipt volumeTeamReceipt, teamID string) (volumeTeamChildren, error) {
	snapshot, err := ReadPrivateRecoverySnapshot(volumeTeamChildrenPath(path, teamID))
	if err != nil {
		return volumeTeamChildren{}, err
	}
	if _, err := execpkg.DecodeStrictJSON(snapshot.Body); err != nil {
		return volumeTeamChildren{}, err
	}
	var state volumeTeamChildren
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return volumeTeamChildren{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return volumeTeamChildren{}, fmt.Errorf("Volume Team child inventory has trailing data: %v", err)
	}
	if state.Version != 1 || state.JournalPath != path || state.RunID != meta.RunID || state.ExecutionID != meta.ExecutionID || state.Region != meta.Region || state.Surface != meta.Surface || state.EcctlBin != meta.EcctlBin || state.EcctlBinSHA256 != meta.EcctlBinSHA256 || state.TeamID != teamID || state.TeamName != receipt.Name || state.TeamDescription != receipt.Description || !state.TeamCreatedAfter.Equal(receipt.CreatedAt) || state.TeamCreatedAt.Before(receipt.CreatedAt.Add(-2*time.Second)) || state.TeamCreatedAt.After(receipt.CreatedAt.Add(10*time.Minute)) || !validHistoricalDigest(state.InitialListSHA256) || len(state.Children) > 64 || state.TeamAttempts < 0 || state.TeamAttempts > 2 || state.EmptyGeneration < 0 || state.ConflictGeneration < 0 || state.ConflictGeneration > state.EmptyGeneration {
		return volumeTeamChildren{}, fmt.Errorf("Volume Team child inventory ownership changed")
	}
	seen, seenNames := map[string]bool{}, map[string]bool{}
	for _, child := range state.Children {
		if !safeTemplateID(child.ID) || seen[child.ID] || !volumeTeamDefaultName(child.Name) || seenNames[child.Name] || child.CreatedAt.Before(state.TeamCreatedAt) || child.CreatedAt.After(state.TeamCreatedAt.Add(2*time.Minute)) || child.State != "pending" && child.State != "attempted" && child.State != "absent" {
			return volumeTeamChildren{}, fmt.Errorf("Volume Team child inventory target changed")
		}
		seen[child.ID] = true
		seenNames[child.Name] = true
	}
	if state.EmptyListSHA256 != "" && !validHistoricalDigest(state.EmptyListSHA256) || state.ConflictSHA256 != "" && (!validHistoricalDigest(state.ConflictSHA256) || state.TeamAttempts < 1 || state.ConflictAt.IsZero()) {
		return volumeTeamChildren{}, fmt.Errorf("Volume Team child inventory proof changed")
	}
	if state.ConflictSHA256 != "" {
		digest := sha256.Sum256([]byte(state.ConflictEvidence))
		if hex.EncodeToString(digest[:]) != state.ConflictSHA256 {
			return volumeTeamChildren{}, fmt.Errorf("Volume Team conflict evidence changed")
		}
	} else if state.ConflictEvidence != "" {
		return volumeTeamChildren{}, fmt.Errorf("Volume Team conflict evidence is unbound")
	}
	return state, nil
}

func writeVolumeTeamChildren(path string, state volumeTeamChildren, exclusive bool) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	full := volumeTeamChildrenPath(path, state.TeamID)
	if exclusive {
		err = WriteExclusiveDurable(full, append(body, '\n'))
	} else {
		err = WriteDurable(full, append(body, '\n'))
	}
	if err != nil {
		return err
	}
	snapshot, err := ReadPrivateRecoverySnapshot(full)
	if err != nil || !reflect.DeepEqual(snapshot.Body, append(body, '\n')) {
		return fmt.Errorf("Volume Team child inventory durability is unproven: %v", err)
	}
	return nil
}

func volumeTeamChildGet(ctx context.Context, meta report.CleanupJournal, cfg execpkg.Config, teamID, teamName string, child volumeTeamChild, run func(context.Context, execpkg.Config, string) execpkg.Result) (bool, error) {
	command := "ecctl sandbox template get " + quoteVolumeArg(child.ID) + " --openapi --team-id " + quoteVolumeArg(teamID) + " --region " + quoteVolumeArg(meta.Region) + " --profile " + quoteVolumeArg(cfg.Profile) + " --output=json --agent-envelope=false"
	res := run(ctx, cfg, command)
	root, err := volumeTeamChildResult(res, meta.EcctlBinSHA256)
	if err != nil {
		return false, err
	}
	if res.Exit == 4 {
		failure, _ := root["error"].(map[string]any)
		actions, _ := root["actions"].([]any)
		if len(actions) != 1 || root["template"] != nil || failure["kind"] != "not_found" || failure["code"] != "NotFound" {
			return false, fmt.Errorf("Volume Team child absence is contradictory")
		}
		action, _ := actions[0].(map[string]any)
		requestID, _ := action["request_id"].(string)
		if action["action_name"] != "GetTemplate" || action["code"] != "TemplateNotFound" || !validVolumeProvenanceID(requestID) {
			return false, fmt.Errorf("Volume Team child absence lacks exact provider provenance")
		}
		return true, nil
	}
	item, _ := root["template"].(map[string]any)
	created, _ := item["created_time"].(string)
	createdAt, parseErr := time.Parse(time.RFC3339Nano, created)
	if res.Exit != 0 || item["id"] != child.ID || item["name"] != child.Name || item["team_id"] != teamID || item["team_name"] != teamName || parseErr != nil || !createdAt.Equal(child.CreatedAt) {
		return false, fmt.Errorf("Volume Team child read differs from its durable inventory")
	}
	return false, nil
}

// PrepareVolumeTeamChildren inventories every page before the first mutation,
// then reconciles exact attempted children read-only. It reports a Team already
// absent so callers can consume their Team finalizer without a new delete.
func PrepareVolumeTeamChildren(ctx context.Context, path string, meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config, run func(context.Context, execpkg.Config, string) execpkg.Result) (bool, error) {
	receipt, teamID, err := teamChildrenReceipt(meta, entry, cfg)
	if errors.Is(err, ErrOwnedTeamChildNoIntent) {
		return false, err
	}
	if err != nil || path != meta.JournalPath {
		return false, fmt.Errorf("Volume Team child journal is invalid: %v", err)
	}
	teamCommand := volumeTeamAbsenceCommand(teamID, meta.Region, cfg.Profile)
	teamResult := run(ctx, cfg, teamCommand)
	root, err := volumeTeamChildResult(teamResult, meta.EcctlBinSHA256)
	if err != nil {
		return false, err
	}
	if teamResult.Exit == 4 {
		if err := volumeTeamAbsenceResult(teamResult); err != nil {
			return false, err
		}
		return true, nil
	}
	team, _ := root["team"].(map[string]any)
	if teamResult.Exit != 0 || team["id"] != teamID || team["name"] != receipt.Name || team["description"] != receipt.Description {
		return false, fmt.Errorf("Volume Team read differs from its accepted creation")
	}
	created, _ := team["created_time"].(string)
	teamCreatedAt, parseErr := time.Parse(time.RFC3339Nano, created)
	if parseErr != nil || teamCreatedAt.Before(receipt.CreatedAt.Add(-2*time.Second)) || teamCreatedAt.After(receipt.CreatedAt.Add(10*time.Minute)) || teamCreatedAt.After(time.Now().Add(2*time.Minute)) {
		return false, fmt.Errorf("Volume Team creation time differs from its accepted creation")
	}
	err = WithExistingParentLock(ctx, path, func() error {
		state, err := readVolumeTeamChildren(path, meta, receipt, teamID)
		if errors.Is(err, os.ErrNotExist) {
			children, digest, err := volumeTeamChildList(ctx, meta, cfg, teamID, receipt.Name, run, teamCreatedAt)
			if err != nil {
				return err
			}
			state = volumeTeamChildren{Version: 1, JournalPath: path, RunID: meta.RunID, ExecutionID: meta.ExecutionID, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin, EcctlBinSHA256: meta.EcctlBinSHA256, TeamID: teamID, TeamName: receipt.Name, TeamDescription: receipt.Description, TeamCreatedAfter: receipt.CreatedAt, TeamCreatedAt: teamCreatedAt, InitialListSHA256: digest, Children: children}
			if err := writeVolumeTeamChildren(path, state, true); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if !state.TeamCreatedAt.Equal(teamCreatedAt) {
			return fmt.Errorf("Volume Team creation time changed during child cleanup")
		}
		if state.EmptyListSHA256 != "" {
			// A prior empty read cannot authorize fixture teardown while a new
			// read is in progress or has failed.
			state.EmptyListSHA256 = ""
			if err := writeVolumeTeamChildren(path, state, false); err != nil {
				return err
			}
		}
		for index, child := range state.Children {
			if child.State == "absent" {
				continue
			}
			absent, err := volumeTeamChildGet(ctx, meta, cfg, teamID, receipt.Name, child, run)
			if err != nil {
				return err
			}
			if absent {
				state.Children[index].State = "absent"
				if err := writeVolumeTeamChildren(path, state, false); err != nil {
					return err
				}
				continue
			}
			if child.State == "attempted" {
				return fmt.Errorf("Volume Team child delete was attempted and remains present")
			}
			state.Children[index].State = "attempted"
			if err := writeVolumeTeamChildren(path, state, false); err != nil {
				return err
			}
			command := "ecctl sandbox template delete " + quoteVolumeArg(child.ID) + " --openapi --team-id " + quoteVolumeArg(teamID) + " --region " + quoteVolumeArg(meta.Region) + " --profile " + quoteVolumeArg(cfg.Profile) + " --output=json --agent-envelope=false"
			result := run(ctx, cfg, command)
			payload, err := volumeTeamChildResult(result, meta.EcctlBinSHA256)
			if err != nil || result.Exit != 0 || payload["deleted"] != true {
				return fmt.Errorf("Volume Team child delete outcome is unproven: %v", err)
			}
			if target, present := payload["template"]; present && target != nil {
				item, ok := target.(map[string]any)
				if !ok || item["id"] != child.ID {
					return fmt.Errorf("Volume Team child delete returned a different target")
				}
			}
			absent, err = volumeTeamChildGet(ctx, meta, cfg, teamID, receipt.Name, child, run)
			if err != nil || !absent {
				return fmt.Errorf("Volume Team child absence is unproven: %v", err)
			}
			state.Children[index].State = "absent"
			if err := writeVolumeTeamChildren(path, state, false); err != nil {
				return err
			}
		}
		remaining, digest, err := volumeTeamChildList(ctx, meta, cfg, teamID, receipt.Name, run, teamCreatedAt)
		if err != nil || len(remaining) != 0 {
			return fmt.Errorf("Volume Team Template inventory is not empty after child cleanup: %v", err)
		}
		state.EmptyListSHA256 = digest
		state.EmptyGeneration++
		return writeVolumeTeamChildren(path, state, false)
	})
	return false, err
}

// VolumeTeamChildrenReady is a durable dependency gate for the independent
// bucket and RAM fixture finalizers after all owned Volumes are absent.
func VolumeTeamChildrenReady(path string, meta report.CleanupJournal, entry report.Resource) error {
	id, err := TeamDeleteTarget(entry.Teardown)
	if err != nil || path != meta.JournalPath {
		return fmt.Errorf("Volume Team child fixture gate lacks accepted ownership: %v", err)
	}
	if err := CheckVolumeTeamCreationLink(meta, id, &entry); err != nil {
		return fmt.Errorf("Volume Team child fixture gate lacks accepted ownership: %w", err)
	}
	var name string
	for candidate, target := range meta.VolumeTeamCreateIDs {
		if target == id {
			name = candidate
		}
	}
	if len(name) < 9 {
		return fmt.Errorf("Volume Team child fixture gate lacks a settled nonce")
	}
	snapshot, err := ReadPrivateRecoverySnapshot(path + ".team-create-" + name[8:] + ".json")
	if err != nil {
		return err
	}
	var receipt volumeTeamReceipt
	if err := json.Unmarshal(snapshot.Body, &receipt); err != nil {
		return err
	}
	state, err := readVolumeTeamChildren(path, meta, receipt, id)
	if err != nil || state.EmptyListSHA256 == "" || state.EmptyGeneration == 0 {
		return fmt.Errorf("Volume Team child absence is not durable: %v", err)
	}
	for _, child := range state.Children {
		if child.State != "absent" {
			return fmt.Errorf("Volume Team child %s is still pending", child.ID)
		}
	}
	return nil
}

// ReserveVolumeTeamDelete durably bounds the Team mutation to one attempt and
// one retry after a verified dependency conflict and a later empty list.
func ReserveVolumeTeamDelete(ctx context.Context, path string, meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config) error {
	receipt, teamID, err := teamChildrenReceipt(meta, entry, cfg)
	if errors.Is(err, ErrOwnedTeamChildNoIntent) {
		return err
	}
	if err != nil || path != meta.JournalPath {
		return fmt.Errorf("Volume Team delete reservation lacks ownership: %v", err)
	}
	return WithExistingParentLock(ctx, path, func() error {
		state, err := readVolumeTeamChildren(path, meta, receipt, teamID)
		if err != nil {
			return err
		}
		if state.EmptyListSHA256 == "" || state.EmptyGeneration == 0 {
			return fmt.Errorf("Volume Team child absence is not durable")
		}
		for _, child := range state.Children {
			if child.State != "absent" {
				return fmt.Errorf("Volume Team child %s is still pending", child.ID)
			}
		}
		switch state.TeamAttempts {
		case 0:
		case 1:
			if state.ConflictSHA256 == "" || state.EmptyGeneration <= state.ConflictGeneration {
				return fmt.Errorf("Volume Team delete outcome is unresolved; fresh empty child inventory is required")
			}
			if delay := 5*time.Second - time.Since(state.ConflictAt); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		default:
			return fmt.Errorf("Volume Team delete attempt limit is exhausted")
		}
		state.TeamAttempts++
		return writeVolumeTeamChildren(path, state, false)
	})
}

// RecordVolumeTeamConflict permits only the observed provider conflict to
// unlock the single bounded retry. The failed result remains a failure to the
// caller and is never turned into a synthetic success.
func RecordVolumeTeamConflict(ctx context.Context, path string, meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config, result execpkg.Result) error {
	receipt, teamID, err := teamChildrenReceipt(meta, entry, cfg)
	if errors.Is(err, ErrOwnedTeamChildNoIntent) {
		return err
	}
	if err != nil || path != meta.JournalPath {
		return fmt.Errorf("Volume Team conflict journal is invalid: %v", err)
	}
	root, err := volumeTeamChildResult(result, meta.EcctlBinSHA256)
	if err != nil || result.Exit == 0 {
		return fmt.Errorf("Volume Team conflict result is unverified: %v", err)
	}
	actions, _ := root["actions"].([]any)
	failure, _ := root["error"].(map[string]any)
	if len(actions) != 1 {
		return fmt.Errorf("Volume Team conflict lacks one provider action")
	}
	action, _ := actions[0].(map[string]any)
	requestID, _ := action["request_id"].(string)
	if failure["kind"] != "service" || failure["code"] != "CloudAPIError" || !volumeTeamDeleteDependency(entry, action) || !validVolumeProvenanceID(requestID) {
		return fmt.Errorf("Volume Team conflict is not the observed child dependency")
	}
	body, err := json.Marshal(root)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return WithExistingParentLock(commitCtx, path, func() error {
		state, err := readVolumeTeamChildren(path, meta, receipt, teamID)
		if err != nil {
			return err
		}
		if state.TeamAttempts == 2 {
			return nil // the second failure is retained; it never unlocks a third attempt
		}
		if state.TeamAttempts != 1 || state.ConflictSHA256 != "" || state.EmptyListSHA256 == "" {
			return fmt.Errorf("Volume Team conflict cannot authorize another delete")
		}
		state.ConflictSHA256 = hex.EncodeToString(digest[:])
		state.ConflictEvidence = string(body)
		state.ConflictAt = time.Now().UTC()
		state.ConflictGeneration = state.EmptyGeneration
		return writeVolumeTeamChildren(path, state, false)
	})
}

// RetryVolumeTeamDeleteAfterConflict performs the one permitted retry in the
// same cleanup pass. A fresh full empty list must follow the retained first
// provider conflict before the second Team delete can be reserved.
func RetryVolumeTeamDeleteAfterConflict(ctx context.Context, path string, meta report.CleanupJournal, entry report.Resource, cfg execpkg.Config, run func(context.Context, execpkg.Config, string) execpkg.Result) (execpkg.Result, error) {
	absent, err := PrepareVolumeTeamChildren(ctx, path, meta, entry, cfg, run)
	if err != nil {
		return execpkg.Result{}, err
	}
	if absent {
		return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}, nil
	}
	if err := ReserveVolumeTeamDelete(ctx, path, meta, entry, cfg); err != nil {
		return execpkg.Result{}, err
	}
	result := run(ctx, cfg, entry.Teardown)
	if IsVolumeTeamChildConflict(entry, result) {
		if err := RecordVolumeTeamConflict(ctx, path, meta, entry, cfg, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

// IsVolumeTeamChildConflict identifies a failed DeleteTeam result that needs a
// durable conflict receipt before the single bounded retry.
func IsVolumeTeamChildConflict(entry report.Resource, result execpkg.Result) bool {
	root, ok := result.JSON.(map[string]any)
	if !ok || result.Err != nil || !result.Started || result.Exit == 0 {
		return false
	}
	actions, _ := root["actions"].([]any)
	if len(actions) != 1 {
		return false
	}
	action, _ := actions[0].(map[string]any)
	return volumeTeamDeleteDependency(entry, action)
}

func volumeTeamDeleteDependency(entry report.Resource, action map[string]any) bool {
	if action["action_name"] != "DeleteTeam" {
		return false
	}
	switch action["code"] {
	case "Conflict.TeamHasTemplates":
		return true
	case "Conflict.TeamHasApiKeys":
		return entry.TeamProviderTemplateCleanup && !entry.VolumeTeamBound
	default:
		return false
	}
}
