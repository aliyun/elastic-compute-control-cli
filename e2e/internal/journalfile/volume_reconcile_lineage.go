package journalfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// volumeReconcileTransition is staged before either file is replaced. Its
// archived snapshots are immutable; a partial installation cannot be imported.
type volumeReconcileTransition struct {
	Version        int      `json:"version"`
	JournalPath    string   `json:"journal_path"`
	RunID          string   `json:"run_id"`
	ExecutionID    string   `json:"execution_id"`
	Region         string   `json:"region"`
	Surface        string   `json:"surface"`
	OriginalSHA256 string   `json:"original_sha256"`
	BeforeSHA256   string   `json:"before_sha256"`
	AfterSHA256    string   `json:"after_sha256"`
	Kind           string   `json:"kind"`
	Target         string   `json:"target"`
	ProofSHA256    string   `json:"proof_sha256,omitempty"`
	SidecarPath    string   `json:"sidecar_path,omitempty"`
	SidecarStates  []string `json:"sidecar_states,omitempty"`
}

func volumeReconcileDigest(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func volumeReconcileTransitionName(path, afterSHA string) string {
	return filepath.Base(path) + ".volume-reconcile-" + afterSHA + ".json"
}

func volumeReconcileAnchor(before report.CleanupJournal, beforeSHA string) (string, error) {
	anchor := before.OriginalJournalSHA256
	if anchor == "" {
		anchor = beforeSHA
	}
	if !validHistoricalDigest(anchor) {
		return "", fmt.Errorf("Volume reconciliation original anchor is invalid")
	}
	return anchor, nil
}

// StageVolumeReconcileTransitionSelected returns the immutable sidecar bytes
// chosen by an existing edge. A retry must publish these bytes, even if a new
// read produced another valid representation of the same journal successor.
func StageVolumeReconcileTransitionSelected(root *os.Root, path, kind, target string, before, after []byte, sidecarPath string, proposed ...[]byte) ([][]byte, error) {
	if root == nil {
		return nil, fmt.Errorf("Volume reconciliation selected transition lacks a rooted parent")
	}
	selected := proposed
	name := volumeReconcileTransitionName(path, volumeReconcileDigest(after))
	if _, err := root.Lstat(name); err == nil {
		stored, err := readVolumeReconcileTransition(root, path, name)
		if err != nil || stored.BeforeSHA256 != volumeReconcileDigest(before) || stored.Kind != kind || stored.Target != target || stored.SidecarPath != sidecarPath || len(stored.SidecarStates) == 0 {
			return nil, fmt.Errorf("Volume reconciliation staged selector changed: %v", err)
		}
		selected = make([][]byte, 0, len(stored.SidecarStates))
		for _, digest := range stored.SidecarStates {
			body, err := readHistoricalOriginal(root, path, digest)
			if err != nil || volumeReconcileDigest(body) != digest {
				return nil, fmt.Errorf("Volume reconciliation staged sidecar archive changed: %v", err)
			}
			selected = append(selected, body)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := StageVolumeReconcileTransition(root, path, kind, target, before, after, sidecarPath, selected...); err != nil {
		return nil, err
	}
	return selected, nil
}

// StageVolumeReconcileTransition retains exact journal and sidecar states and
// a durable link before a reconciler installs its first replacement. Callers
// hold the replay and parent locks and use the same rooted parent for writes.
func StageVolumeReconcileTransition(root *os.Root, path, kind, target string, before, after []byte, sidecarPath string, sidecarStates ...[]byte) error {
	if root == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || len(before) == 0 || len(after) == 0 || len(sidecarStates) > 3 {
		return fmt.Errorf("Volume reconciliation transition lacks exact rooted bytes")
	}
	beforeSHA, afterSHA := volumeReconcileDigest(before), volumeReconcileDigest(after)
	var prior, next report.CleanupJournal
	if unmarshalRecoveryJSON(before, &prior) != nil || unmarshalRecoveryJSON(after, &next) != nil || prior.Version != 2 || next.Version != 2 || prior.JournalPath != path || next.JournalPath != path {
		return fmt.Errorf("Volume reconciliation transition journal is invalid")
	}
	anchor, err := volumeReconcileAnchor(prior, beforeSHA)
	if err != nil || next.OriginalJournalSHA256 != anchor {
		return fmt.Errorf("Volume reconciliation changed its original anchor: %v", err)
	}
	transition := volumeReconcileTransition{Version: 1, JournalPath: path, RunID: prior.RunID, ExecutionID: prior.ExecutionID, Region: prior.Region, Surface: prior.Surface, OriginalSHA256: anchor, BeforeSHA256: beforeSHA, AfterSHA256: afterSHA, Kind: kind, Target: target, SidecarPath: sidecarPath}
	var proof []byte
	if kind == "replay-transfer" && sidecarPath == "" && len(sidecarStates) == 1 {
		proof = bytes.Clone(sidecarStates[0])
		transition.ProofSHA256 = volumeReconcileDigest(proof)
		sidecarStates = nil
	}
	if sidecarPath != "" {
		if filepath.Dir(sidecarPath) != filepath.Dir(path) || filepath.Clean(sidecarPath) != sidecarPath || len(sidecarStates) == 0 {
			return fmt.Errorf("Volume reconciliation sidecar path is not rooted beside its journal")
		}
		for _, body := range sidecarStates {
			transition.SidecarStates = append(transition.SidecarStates, volumeReconcileDigest(body))
		}
	} else if len(sidecarStates) != 0 {
		return fmt.Errorf("Volume reconciliation sidecar states lack a path")
	}
	name := volumeReconcileTransitionName(path, afterSHA)
	if err := recoverPendingVolumeReconcileRetirement(root, path); err != nil {
		return err
	}
	entries, err := readRootInventory(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), filepath.Base(path)+".volume-reconcile-") || entry.Name() == name {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("Volume reconciliation transition registry contains a nonregular edge")
		}
		staged, err := readVolumeReconcileTransition(root, path, entry.Name())
		if err != nil {
			return err
		}
		if staged.BeforeSHA256 == beforeSHA {
			if err := retireUnusedVolumeReconcileTransition(root, path, entry.Name(), staged, before); err != nil {
				return err
			}
		}
	}
	encoded, err := json.MarshalIndent(transition, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if info, err := root.Lstat(name); err == nil && (!info.Mode().IsRegular() || info.Size() > 1<<20) {
		return fmt.Errorf("Volume reconciliation staged transition is not a bounded regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if existing, readErr := root.ReadFile(name); readErr == nil {
		stored, err := readVolumeReconcileTransition(root, path, name)
		comparison, retained := transition, stored
		if kind == "replay-transfer" {
			comparison.ProofSHA256 = retained.ProofSHA256
		}
		if err != nil || !reflect.DeepEqual(comparison, retained) {
			return fmt.Errorf("Volume reconciliation staged transition changed")
		}
		transition = stored
		encoded = existing
		if stored.ProofSHA256 != "" {
			proof, err = readHistoricalOriginal(root, path, stored.ProofSHA256)
			if err != nil {
				return err
			}
		}
		sidecarStates = nil
		for _, digest := range stored.SidecarStates {
			body, err := readHistoricalOriginal(root, path, digest)
			if err != nil {
				return err
			}
			sidecarStates = append(sidecarStates, body)
		}
		if stored.SidecarPath != "" {
			installed, err := root.ReadFile(filepath.Base(stored.SidecarPath))
			if err != nil {
				return err
			}
			found := false
			for _, state := range sidecarStates {
				found = found || bytes.Equal(installed, state)
			}
			if !found {
				return fmt.Errorf("Volume reconciliation staged sidecar changed")
			}
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	} else {
		installed, err := root.ReadFile(filepath.Base(path))
		if err != nil || !bytes.Equal(installed, before) {
			return fmt.Errorf("Volume reconciliation predecessor changed before staging: %v", err)
		}
		if sidecarPath != "" {
			installed, err = root.ReadFile(filepath.Base(sidecarPath))
			if err != nil || !bytes.Equal(installed, sidecarStates[0]) {
				return fmt.Errorf("Volume reconciliation sidecar predecessor changed before staging: %v", err)
			}
		}
	}
	if err := validateVolumeReconcileTransition(root, transition, prior, next, sidecarStates, proof); err != nil {
		return err
	}
	if _, err := root.Lstat(name); err == nil {
		if err := ReaffirmDurableRoot(root, name, encoded); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, snapshot := range []struct {
		path string
		body []byte
	}{{path, before}, {path, after}} {
		if err := EnsureHistoricalJournalOriginal(root, snapshot.path, volumeReconcileDigest(snapshot.body), snapshot.body); err != nil {
			return err
		}
	}
	for _, body := range sidecarStates {
		if err := EnsureHistoricalJournalOriginal(root, path, volumeReconcileDigest(body), body); err != nil {
			return err
		}
	}
	if len(proof) != 0 {
		if err := EnsureHistoricalJournalOriginal(root, path, transition.ProofSHA256, proof); err != nil {
			return err
		}
	}
	if _, err := root.Lstat(name); os.IsNotExist(err) {
		if err := WriteDurableRoot(root, name, encoded); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	installed, err := ReadPrivateRecoverySnapshotRoot(root, name)
	if err != nil || !bytes.Equal(installed.Body, encoded) {
		return fmt.Errorf("Volume reconciliation staged transition is not private or exact: %v", err)
	}
	return nil
}

// A different successor may be staged only after an unused edge is proven to
// have installed no journal or sidecar state. The archive remains immutable.
func retireUnusedVolumeReconcileTransition(root *os.Root, path, name string, staged volumeReconcileTransition, before []byte) error {
	if err := validateUnusedVolumeReconcileTransition(root, path, staged, before); err != nil {
		return err
	}
	if err := retireVolumeReconcileEdge(root, path, name); err != nil {
		return err
	}
	installed, err := root.ReadFile(filepath.Base(path))
	if err != nil || !bytes.Equal(installed, before) {
		return fmt.Errorf("Volume reconciliation predecessor changed after edge retirement: %v", err)
	}
	if staged.SidecarPath != "" {
		states, err := readHistoricalOriginal(root, path, staged.SidecarStates[0])
		if err != nil {
			return err
		}
		installed, err = root.ReadFile(filepath.Base(staged.SidecarPath))
		if err != nil || !bytes.Equal(installed, states) {
			return fmt.Errorf("Volume reconciliation sidecar changed after edge retirement: %v", err)
		}
	}
	return nil
}

func validateUnusedVolumeReconcileTransition(root *os.Root, path string, staged volumeReconcileTransition, before []byte) error {
	if staged.BeforeSHA256 != volumeReconcileDigest(before) {
		return fmt.Errorf("Volume reconciliation predecessor has another staged successor")
	}
	archivedBefore, err := readHistoricalOriginal(root, path, staged.BeforeSHA256)
	if err != nil || !bytes.Equal(archivedBefore, before) {
		return fmt.Errorf("Volume reconciliation competing predecessor archive changed: %v", err)
	}
	archivedAfter, err := readHistoricalOriginal(root, path, staged.AfterSHA256)
	if err != nil {
		return err
	}
	var prior, next report.CleanupJournal
	if unmarshalRecoveryJSON(before, &prior) != nil || unmarshalRecoveryJSON(archivedAfter, &next) != nil {
		return fmt.Errorf("Volume reconciliation competing transition journal is invalid")
	}
	var states [][]byte
	for _, digest := range staged.SidecarStates {
		body, err := readHistoricalOriginal(root, path, digest)
		if err != nil {
			return err
		}
		states = append(states, body)
	}
	var proof []byte
	if staged.ProofSHA256 != "" {
		proof, err = readHistoricalOriginal(root, path, staged.ProofSHA256)
		if err != nil {
			return err
		}
	}
	if err := validateVolumeReconcileTransition(root, staged, prior, next, states, proof); err != nil {
		return err
	}
	installed, err := root.ReadFile(filepath.Base(path))
	if err != nil || !bytes.Equal(installed, before) {
		return fmt.Errorf("Volume reconciliation competing edge has an installed journal state: %v", err)
	}
	if staged.SidecarPath != "" {
		if len(states) == 0 {
			return fmt.Errorf("Volume reconciliation competing edge has no sidecar predecessor")
		}
		installed, err = root.ReadFile(filepath.Base(staged.SidecarPath))
		if err != nil || !bytes.Equal(installed, states[0]) {
			return fmt.Errorf("Volume reconciliation competing edge has an installed sidecar state: %v", err)
		}
	}
	return nil
}

func cloneVolumeReconcileJournal(journal report.CleanupJournal) (report.CleanupJournal, error) {
	body, err := json.Marshal(journal)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	var cloned report.CleanupJournal
	err = unmarshalRecoveryJSON(body, &cloned)
	return cloned, err
}

func volumeReconcileSidecarName(path, kind, target string) string {
	if len(target) != 32 || !safeTemplateID(target) {
		return ""
	}
	switch kind {
	case "team-accept", "team-retire":
		if strings.HasPrefix(target, "ecctl-t-") {
			return path + ".team-create-" + target[8:] + ".json"
		}
	case "volume-register", "volume-observe", "volume-settle":
		if strings.HasPrefix(target, "ecctl-p-") {
			return path + ".template-create-" + target[8:] + ".json"
		}
	}
	return ""
}

func validateVolumeReconcileSidecar(transition volumeReconcileTransition, states [][]byte) error {
	if transition.SidecarPath == "" {
		if len(states) != 0 || len(transition.SidecarStates) != 0 {
			return fmt.Errorf("Volume reconciliation unexpectedly changed a sidecar")
		}
		return nil
	}
	if transition.SidecarPath != volumeReconcileSidecarName(transition.JournalPath, transition.Kind, transition.Target) || len(states) == 0 || len(states) > 3 || len(states) != len(transition.SidecarStates) {
		return fmt.Errorf("Volume reconciliation sidecar selector changed")
	}
	for i, body := range states {
		if transition.SidecarStates[i] != volumeReconcileDigest(body) {
			return fmt.Errorf("Volume reconciliation sidecar snapshot changed")
		}
	}
	switch transition.Kind {
	case "team-accept", "team-retire":
		if len(states) > 2 {
			return fmt.Errorf("Volume Team sidecar has extra states")
		}
		var before, after volumeTeamReceipt
		if unmarshalRecoveryJSON(states[0], &before) != nil || unmarshalRecoveryJSON(states[len(states)-1], &after) != nil || before.Name != transition.Target || after.Name != transition.Target || before.RunID != transition.RunID || after.RunID != transition.RunID || before.ExecutionID != transition.ExecutionID || after.ExecutionID != transition.ExecutionID || before.Region != transition.Region || after.Region != transition.Region || before.Surface != transition.Surface || after.Surface != transition.Surface {
			return fmt.Errorf("Volume Team sidecar selectors changed")
		}
		if transition.Kind == "team-retire" && !bytes.Equal(states[0], states[len(states)-1]) {
			return fmt.Errorf("Volume Team retirement changed its unlaunched intent")
		}
		copy := after
		copy.ProbeBinSHA256 = before.ProbeBinSHA256
		if !reflect.DeepEqual(copy, before) || after.ProbeBinSHA256 != before.ProbeBinSHA256 && !validHistoricalDigest(after.ProbeBinSHA256) {
			return fmt.Errorf("Volume Team accepted proof changed outside its probe pin")
		}
	case "volume-register":
		if len(states) < 2 {
			return fmt.Errorf("Volume sidecar has no settlement states")
		}
		var first, final TemplateCreateIntent
		if unmarshalRecoveryJSON(states[0], &first) != nil || unmarshalRecoveryJSON(states[len(states)-1], &final) != nil || first.Name != transition.Target || final.Name != transition.Target || first.Resource != "volume" || final.Resource != "volume" || first.RunID != transition.RunID || final.RunID != transition.RunID || first.ExecutionID != transition.ExecutionID || final.ExecutionID != transition.ExecutionID || first.Region != transition.Region || final.Region != transition.Region || first.Surface != transition.Surface || final.Surface != transition.Surface || final.State != "journaled" || !final.Recovered || !final.VolumeObserved || !validHistoricalDigest(final.ReconcileSHA256) || len(final.Deletes) != 1 || final.ValidateDelete(final.Deletes[0]) != nil || final.AcceptedVolumeID == "" || final.Started == nil || !*final.Started || final.Exit == nil || !validHistoricalDigest(final.StdoutSHA256) || !validHistoricalDigest(final.StderrSHA256) || final.CreatedAt.IsZero() {
			return fmt.Errorf("Volume sidecar lacks a valid final settlement")
		}
		request, err := ParseVolumeDeleteRequest(final.Deletes[0])
		if err != nil || request.ID != final.AcceptedVolumeID {
			return fmt.Errorf("Volume sidecar finalizer differs from the accepted ID")
		}
		if first.State == "pending" && len(states) != 3 || first.State == "settling" && len(states) != 2 {
			return fmt.Errorf("Volume sidecar settlement skipped a required stage")
		}
		for i, body := range states {
			var stage TemplateCreateIntent
			if unmarshalRecoveryJSON(body, &stage) != nil {
				return fmt.Errorf("Volume sidecar stage is invalid")
			}
			if i == 0 && stage.State != "pending" && stage.State != "settling" || i > 0 && i < len(states)-1 && stage.State != "settling" {
				return fmt.Errorf("Volume sidecar stage order changed")
			}
			copy := stage
			copy.State, copy.Deletes, copy.Recovered, copy.ReconcileSHA256, copy.VolumeObserved = first.State, first.Deletes, first.Recovered, first.ReconcileSHA256, first.VolumeObserved
			if !reflect.DeepEqual(copy, first) || stage.State == "settling" && (!stage.Recovered || !stage.VolumeObserved || stage.ReconcileSHA256 != final.ReconcileSHA256 || !reflect.DeepEqual(stage.Deletes, final.Deletes)) {
				return fmt.Errorf("Volume sidecar changed outside settlement fields")
			}
		}
	case "volume-observe":
		if len(states) != 2 {
			return fmt.Errorf("Volume observation lacks exact before and after sidecars")
		}
		var before, after TemplateCreateIntent
		if unmarshalRecoveryJSON(states[0], &before) != nil || unmarshalRecoveryJSON(states[1], &after) != nil || before.Resource != "volume" || before.State != "journaled" || before.VolumeObserved || after.State != "journaled" || !after.VolumeObserved || after.Name != transition.Target || before.Name != after.Name || before.RunID != transition.RunID || before.ExecutionID != transition.ExecutionID || before.Region != transition.Region || before.Surface != transition.Surface || after.AcceptedVolumeID == "" || after.Started == nil || !*after.Started || after.Exit == nil || len(after.Deletes) != 1 || after.ValidateDelete(after.Deletes[0]) != nil {
			return fmt.Errorf("Volume observation sidecar is invalid")
		}
		request, err := ParseVolumeDeleteRequest(after.Deletes[0])
		if err != nil || request.ID != after.AcceptedVolumeID {
			return fmt.Errorf("Volume observation finalizer differs from the accepted ID")
		}
		copy := after
		copy.VolumeObserved = false
		if !reflect.DeepEqual(copy, before) {
			return fmt.Errorf("Volume observation changed a sidecar field other than its readback marker")
		}
	case "volume-settle":
		if len(states) != 2 {
			return fmt.Errorf("Volume settlement lacks exact before and after sidecars")
		}
		var before, after TemplateCreateIntent
		if unmarshalRecoveryJSON(states[0], &before) != nil || unmarshalRecoveryJSON(states[1], &after) != nil ||
			before.Resource != "volume" || before.State != "settling" || !before.VolumeObserved || !before.Recovered ||
			after.State != "journaled" || after.Name != transition.Target || before.Name != after.Name ||
			before.RunID != transition.RunID || before.ExecutionID != transition.ExecutionID ||
			before.Region != transition.Region || before.Surface != transition.Surface ||
			after.AcceptedVolumeID == "" || len(after.Deletes) != 1 || after.ValidateDelete(after.Deletes[0]) != nil {
			return fmt.Errorf("Volume settlement sidecar is invalid")
		}
		copy := after
		copy.State = "settling"
		if !reflect.DeepEqual(copy, before) {
			return fmt.Errorf("Volume settlement changed fields other than sidecar state")
		}
	default:
		return fmt.Errorf("Volume reconciliation sidecar is unexpected")
	}
	return nil
}

func validateVolumeReconcileTransition(root *os.Root, transition volumeReconcileTransition, prior, next report.CleanupJournal, sidecarStates [][]byte, proof []byte) error {
	if transition.Version != 1 || transition.JournalPath == "" || transition.RunID == "" || transition.ExecutionID == "" || transition.Region == "" || transition.Surface == "" || transition.Target == "" || !validHistoricalDigest(transition.BeforeSHA256) || !validHistoricalDigest(transition.AfterSHA256) || !validHistoricalDigest(transition.OriginalSHA256) || transition.BeforeSHA256 == transition.AfterSHA256 ||
		prior.Version != 2 || next.Version != 2 || prior.JournalPath != transition.JournalPath || next.JournalPath != transition.JournalPath || prior.RunID != transition.RunID || next.RunID != transition.RunID || prior.ExecutionID != transition.ExecutionID || next.ExecutionID != transition.ExecutionID || prior.Region != transition.Region || next.Region != transition.Region || prior.Surface != transition.Surface || next.Surface != transition.Surface || prior.EcctlBinSHA256 != next.EcctlBinSHA256 || prior.EcctlBin != next.EcctlBin || prior.OriginalJournalSHA256 != "" && prior.OriginalJournalSHA256 != transition.OriginalSHA256 || next.OriginalJournalSHA256 != transition.OriginalSHA256 {
		return fmt.Errorf("Volume reconciliation transition selectors or anchor changed")
	}
	if err := validateVolumeReconcileSidecar(transition, sidecarStates); err != nil {
		return err
	}
	if (transition.Kind == "replay-transfer") != (len(proof) != 0) || len(proof) != 0 && transition.ProofSHA256 != volumeReconcileDigest(proof) || len(proof) == 0 && transition.ProofSHA256 != "" {
		return fmt.Errorf("Volume reconciliation replay proof changed")
	}
	expected, err := cloneVolumeReconcileJournal(prior)
	if err != nil {
		return err
	}
	expected.OriginalJournalSHA256 = transition.OriginalSHA256
	switch transition.Kind {
	case "team-accept":
		if transition.SidecarPath == "" || prior.VolumeTeamCreateIDs[transition.Target] != "" {
			return fmt.Errorf("Volume Team acceptance lacks a reserved nonce")
		}
		sidecar := filepath.Base(transition.SidecarPath)
		added, err := historicalVolumeTeamTransition(root, transition.JournalPath, prior, next, map[string][]byte{sidecar: sidecarStates[len(sidecarStates)-1]}, map[string]bool{sidecar: true}, filepath.Base(transition.JournalPath))
		if err != nil {
			return fmt.Errorf("Volume Team acceptance proof changed: %v", err)
		}
		id := next.VolumeTeamCreateIDs[transition.Target]
		if id == "" || !safeTemplateID(id) {
			return fmt.Errorf("Volume Team accepted ID is invalid")
		}
		for name, otherID := range prior.VolumeTeamCreateIDs {
			if name == transition.Target || otherID != "" || len(name) != 32 || !strings.HasPrefix(name, "ecctl-t-") {
				continue
			}
			otherPath := volumeReconcileSidecarName(transition.JournalPath, "team-accept", name)
			body, err := readVolumeReconcilePeerSidecar(root, transition.JournalPath, otherPath)
			if err != nil {
				return err
			}
			var other volumeTeamReceipt
			if unmarshalRecoveryJSON(body, &other) != nil || other.Name != name || other.AcceptedID == id {
				return fmt.Errorf("Volume Team accepted ID is claimed by another nonce")
			}
		}
		if expected.VolumeTeamCreateIDs == nil {
			expected.VolumeTeamCreateIDs = make(map[string]string)
		}
		expected.VolumeTeamCreateIDs[transition.Target] = id
		if expected.VolumeTeamCreators == nil {
			expected.VolumeTeamCreators = make(map[string]*report.QuotaCredentialBinding)
		}
		expected.VolumeTeamCreators[id] = next.VolumeTeamCreators[id]
		if added != nil {
			expected.Entries = append(expected.Entries, *added)
		}
	case "team-retire":
		var intent volumeTeamReceipt
		if transition.SidecarPath == "" || unmarshalRecoveryJSON(sidecarStates[len(sidecarStates)-1], &intent) != nil || intent.Name != transition.Target || intent.DispatchProtocol != 1 || intent.Dispatched || intent.AcceptedID != "" || intent.CreatedAt.IsZero() || intent.RunID != prior.RunID || intent.ExecutionID != prior.ExecutionID || intent.Region != prior.Region || intent.Surface != prior.Surface || intent.EcctlBin != prior.EcctlBin || intent.Description != "ecctl E2E Team owner="+transition.Target[8:] || intent.CaseScope == "" || intent.VolumeBinding == nil || !reflect.DeepEqual(intent.VolumeBinding, prior.VolumeFixtureBinding) || !reflect.DeepEqual(intent.QuotaBinding, prior.QuotaBinding) || len(intent.Input) == 0 || intent.Input[0] != "ecctl" || strings.Count(intent.Teardown, "intent-validation") != 1 {
			return fmt.Errorf("Volume Team retirement lacks an unlaunched intent")
		}
		if state, ok := prior.VolumeTeamCreateIDs[transition.Target]; !ok || state != "" {
			return fmt.Errorf("Volume Team retirement lacks its reserved nonce")
		}
		delete(expected.VolumeTeamCreateIDs, transition.Target)
		if len(expected.VolumeTeamCreateIDs) == 0 {
			expected.VolumeTeamCreateIDs = nil
		}
		if expected.VolumeRetirementReceipts == nil {
			expected.VolumeRetirementReceipts = make(map[string]report.VolumeRetirementReceipt)
		}
		key := "team:" + transition.Target
		if _, exists := expected.VolumeRetirementReceipts[key]; exists {
			return fmt.Errorf("Volume Team retirement receipt was already used")
		}
		expected.VolumeRetirementReceipts[key] = report.VolumeRetirementReceipt{OriginalSHA256: transition.BeforeSHA256, TeamName: transition.Target}
	case "fixture-settle", "fixture-retire":
		if transition.SidecarPath != "" || transition.Target != "role" && transition.Target != "policy" && transition.Target != "bucket" && transition.Target != "attachment" {
			return fmt.Errorf("Volume fixture transition selector is invalid")
		}
		index := -1
		for i, entry := range prior.Entries {
			if entry.VolumeFixtureKind == transition.Target {
				if index >= 0 || !entry.VolumeFixturePending || ValidateVolumeFixtureTeardown(entry, prior) != nil {
					return fmt.Errorf("Volume fixture transition has ambiguous ownership")
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture transition lacks its pending entry")
		}
		entry := prior.Entries[index]
		if transition.Kind == "fixture-settle" {
			if transition.Target != "role" && transition.Target != "policy" || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureCreateRequestID == "" || entry.VolumeFixtureCreateResponseSHA256 == "" || !entry.VolumeFixtureCreateValidated {
				return fmt.Errorf("Volume fixture settlement lacks accepted creation proof")
			}
			expected.Entries[index].VolumeFixturePending = false
		} else {
			if entry.VolumeFixtureDispatchProtocol != 1 || entry.VolumeFixtureDispatched || entry.VolumeFixtureCreateRequestID != "" || entry.VolumeFixtureCreateResponseSHA256 != "" || entry.VolumeFixtureCreateValidated || entry.VolumeFixtureRoleID != "" || entry.VolumeFixturePolicyCreateDate != "" || entry.VolumeFixtureBucketCreationDate != "" {
				return fmt.Errorf("Volume fixture retirement lacks a pre-dispatch receipt")
			}
			if expected.VolumeRetirementReceipts == nil {
				expected.VolumeRetirementReceipts = make(map[string]report.VolumeRetirementReceipt)
			}
			key := "fixture:" + transition.Target
			if _, exists := expected.VolumeRetirementReceipts[key]; exists {
				return fmt.Errorf("Volume fixture retirement receipt was already used")
			}
			expected.VolumeRetirementReceipts[key] = report.VolumeRetirementReceipt{OriginalSHA256: transition.BeforeSHA256, FixtureEntry: &entry, FixtureIndex: index}
			expected.Entries = append(expected.Entries[:index], expected.Entries[index+1:]...)
		}
	case "volume-register":
		if transition.SidecarPath == "" {
			return fmt.Errorf("Volume registration lacks a sidecar")
		}
		var intent TemplateCreateIntent
		if unmarshalRecoveryJSON(sidecarStates[len(sidecarStates)-1], &intent) != nil || intent.Name != transition.Target || intent.RunID != prior.RunID || intent.ExecutionID != prior.ExecutionID || intent.RegionRole != prior.RegionRole || intent.Region != prior.Region || intent.Surface != prior.Surface || intent.EcctlBin != prior.EcctlBin || intent.CaseScope == "" || intent.State != "journaled" || intent.AcceptedVolumeID == "" || !safeTemplateID(intent.AcceptedVolumeID) || len(intent.Deletes) != 1 || intent.ValidateDelete(intent.Deletes[0]) != nil {
			return fmt.Errorf("Volume registration sidecar is not bound to the journal")
		}
		references := 0
		for _, candidate := range prior.NativeIntentPaths {
			if candidate == transition.SidecarPath {
				references++
			}
		}
		if references != 1 || CheckVolumeTeamCreatorSource(prior, intent.TeamID, intent) != nil {
			return fmt.Errorf("Volume registration lacks one retained Team and sidecar")
		}
		for _, otherPath := range prior.NativeIntentPaths {
			if otherPath == transition.SidecarPath {
				continue
			}
			body, err := readVolumeReconcilePeerSidecar(root, transition.JournalPath, otherPath)
			if err != nil {
				return err
			}
			var other TemplateCreateIntent
			if unmarshalRecoveryJSON(body, &other) != nil {
				return fmt.Errorf("other Volume ownership sidecar is invalid")
			}
			if other.Resource == "volume" && other.AcceptedVolumeID == intent.AcceptedVolumeID {
				return fmt.Errorf("Volume ID is claimed by another creation")
			}
		}
		parents := 0
		for _, entry := range prior.Entries {
			if !entry.TeamCreateConfirmed {
				continue
			}
			id, err := TeamDeleteTarget(entry.Teardown)
			if err == nil && id == intent.TeamID && entry.VolumeTeamBound && !entry.TeamReplayAttempted && entry.RegionRole == prior.RegionRole && entry.Region == prior.Region && entry.ExecutionID == prior.ExecutionID {
				parents++
			}
		}
		if parents != 1 {
			return fmt.Errorf("Volume registration Team parent is missing or ambiguous")
		}
		finalizers := 0
		for _, entry := range prior.Entries {
			if !IsNativeVolumeDelete(entry.Teardown) {
				continue
			}
			request, err := ParseVolumeDeleteRequest(entry.Teardown)
			if err != nil {
				return fmt.Errorf("Volume registration has an invalid retained finalizer: %w", err)
			}
			if request.ID != intent.AcceptedVolumeID {
				continue
			}
			finalizers++
			if entry.Teardown != intent.Deletes[0] || entry.Scope != intent.CaseScope || entry.RegionRole != prior.RegionRole || entry.Region != prior.Region || entry.ExecutionID != prior.ExecutionID || entry.VolumeDeleteAttempted {
				return fmt.Errorf("Volume registration existing finalizer differs from its accepted creation")
			}
		}
		if finalizers > 1 {
			return fmt.Errorf("Volume registration existing finalizer is ambiguous")
		}
		if finalizers == 0 {
			expected.Entries = append(expected.Entries, report.Resource{Scope: intent.CaseScope, Teardown: intent.Deletes[0], RegionRole: prior.RegionRole, Region: prior.Region, ExecutionID: prior.ExecutionID})
		} else {
			if _, exists := expected.VolumeObservationReceipts[intent.Name]; exists {
				return fmt.Errorf("Volume registration reused an observation receipt")
			}
			if expected.VolumeObservationReceipts == nil {
				expected.VolumeObservationReceipts = make(map[string]string)
			}
			expected.VolumeObservationReceipts[intent.Name] = transition.SidecarStates[len(transition.SidecarStates)-1]
		}
	case "volume-observe", "volume-settle":
		var intent TemplateCreateIntent
		if transition.SidecarPath == "" || unmarshalRecoveryJSON(sidecarStates[len(sidecarStates)-1], &intent) != nil || intent.Name != transition.Target || intent.RunID != prior.RunID || intent.ExecutionID != prior.ExecutionID || intent.RegionRole != prior.RegionRole || intent.Region != prior.Region || intent.Surface != prior.Surface || intent.EcctlBin != prior.EcctlBin || intent.CaseScope == "" || intent.AcceptedVolumeID == "" || !safeTemplateID(intent.AcceptedVolumeID) || len(intent.Deletes) != 1 || intent.ValidateDelete(intent.Deletes[0]) != nil {
			return fmt.Errorf("Volume observation sidecar is not bound to the journal")
		}
		references, finalizers := 0, 0
		for _, candidate := range prior.NativeIntentPaths {
			if candidate == transition.SidecarPath {
				references++
			}
		}
		for _, entry := range prior.Entries {
			if entry.Teardown == intent.Deletes[0] && entry.Scope == intent.CaseScope && entry.RegionRole == prior.RegionRole && entry.Region == prior.Region && entry.ExecutionID == prior.ExecutionID {
				finalizers++
			}
		}
		if references != 1 || finalizers != 1 || CheckVolumeTeamCreatorSource(prior, intent.TeamID, intent) != nil {
			return fmt.Errorf("Volume observation lacks one retained owner and finalizer")
		}
		if expected.VolumeObservationReceipts == nil {
			expected.VolumeObservationReceipts = make(map[string]string)
		}
		if _, exists := expected.VolumeObservationReceipts[intent.Name]; exists {
			return fmt.Errorf("Volume observation receipt was already used")
		}
		expected.VolumeObservationReceipts[intent.Name] = transition.SidecarStates[len(transition.SidecarStates)-1]
	case "replay-entry":
		if transition.SidecarPath != "" || transition.Target != "replay" ||
			!reflect.DeepEqual(prior.VolumeRetirementReceipts, next.VolumeRetirementReceipts) ||
			!reflect.DeepEqual(prior.VolumeTransferHistory, next.VolumeTransferHistory) && reflect.DeepEqual(prior.VolumeTerminalReceipts, next.VolumeTerminalReceipts) {
			return fmt.Errorf("historical replay entry transition changed Volume ownership metadata")
		}
		contents, authenticated, err := volumeReplayProtectedSidecars(root, transition.JournalPath)
		if err != nil {
			return err
		}
		if err := validateReplayVolumeTerminal(prior, next, contents, filepath.Base(transition.JournalPath)); err != nil {
			return err
		}
		if err := validateHistoricalSuccessor(root, transition.JournalPath, prior, next, contents, authenticated, filepath.Base(transition.JournalPath)); err != nil {
			return fmt.Errorf("historical replay entry transition lacks exact receipts: %w", err)
		}
		expected = next
	case "replay-transfer":
		if transition.SidecarPath != "" || transition.Target != "replay" {
			return fmt.Errorf("Volume transfer replay selector changed")
		}
		if err := validateVolumeReplayTransferProof(root, transition, prior, next, proof); err != nil {
			return err
		}
		expected = next
	default:
		return fmt.Errorf("unsupported Volume reconciliation transition")
	}
	if !reflect.DeepEqual(expected, next) {
		return fmt.Errorf("Volume reconciliation changed an unrelated journal field or cleanup target")
	}
	return validateVolumeReconcileUniqueIDs(next)
}

func validateReplayVolumeTerminal(prior, next report.CleanupJournal, contents map[string][]byte, base string) error {
	if reflect.DeepEqual(prior.VolumeTerminalReceipts, next.VolumeTerminalReceipts) {
		return nil
	}
	if len(next.VolumeTerminalReceipts) != len(prior.VolumeTerminalReceipts)+1 {
		return fmt.Errorf("Volume replay terminal receipt changed ambiguously")
	}
	name := ""
	for candidate, receipt := range next.VolumeTerminalReceipts {
		if previous, exists := prior.VolumeTerminalReceipts[candidate]; exists {
			if previous != receipt {
				return fmt.Errorf("Volume replay changed an earlier terminal receipt")
			}
			continue
		}
		if name != "" || len(candidate) != 32 || !strings.HasPrefix(candidate, "ecctl-p-") {
			return fmt.Errorf("Volume replay terminal receipt is ambiguous")
		}
		name = candidate
	}
	if name == "" {
		return fmt.Errorf("Volume replay terminal receipt is missing")
	}
	var intent TemplateCreateIntent
	if unmarshalRecoveryJSON(contents[base+".template-create-"+name[8:]+".json"], &intent) != nil || intent.Name != name || !intent.VolumeObserved || len(intent.Deletes) != 1 {
		return fmt.Errorf("Volume replay terminal receipt lacks its observed creation")
	}
	var entry report.Resource
	matches := 0
	for _, candidate := range prior.Entries {
		if candidate.Teardown == intent.Deletes[0] {
			entry = candidate
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("Volume replay terminal finalizer is missing or ambiguous")
	}
	body, err := json.Marshal(prior)
	if err != nil {
		return err
	}
	var expected report.CleanupJournal
	if err := json.Unmarshal(body, &expected); err != nil {
		return err
	}
	if err := RetireVolumeFinalizer(&expected, intent, entry); err != nil || !reflect.DeepEqual(expected, next) {
		return fmt.Errorf("Volume replay terminal transition differs from exact retirement: %v", err)
	}
	return nil
}

func readVolumeReconcilePeerSidecar(root *os.Root, journalPath, sidecarPath string) ([]byte, error) {
	if root == nil || filepath.Dir(sidecarPath) != filepath.Dir(journalPath) || filepath.Clean(sidecarPath) != sidecarPath ||
		!strings.HasSuffix(sidecarPath, ".json") ||
		(!strings.HasPrefix(sidecarPath, journalPath+".team-create-") && !strings.HasPrefix(sidecarPath, journalPath+".template-create-")) {
		return nil, fmt.Errorf("Volume reconciliation peer sidecar is outside its journal")
	}
	file, err := root.Open(filepath.Base(sidecarPath))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		file.Close()
		return nil, fmt.Errorf("Volume reconciliation peer sidecar is not a bounded regular file: %v", err)
	}
	body, private, readErr := ReadRecoveryFileRoot(root, file, filepath.Base(sidecarPath))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !private || len(body) > 8<<20 {
		return nil, fmt.Errorf("Volume reconciliation peer sidecar is not private or exact: %v", readErr)
	}
	return body, nil
}

func validateVolumeReconcileUniqueIDs(journal report.CleanupJournal) error {
	teams, finalizers, volumes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, id := range journal.VolumeTeamCreateIDs {
		if id == "" {
			continue
		}
		if teams[id] {
			return fmt.Errorf("Volume reconciliation reused a Team ID")
		}
		teams[id] = true
	}
	for _, entry := range journal.Entries {
		if entry.TeamCreateConfirmed {
			id, err := TeamDeleteTarget(entry.Teardown)
			if err != nil {
				return err
			}
			if finalizers[id] {
				return fmt.Errorf("Volume reconciliation reused a Team finalizer ID")
			}
			finalizers[id] = true
			if entry.VolumeTeamBound && !teams[id] {
				return fmt.Errorf("Volume Team finalizer lacks a unique nonce")
			}
		}
		if !IsNativeVolumeDelete(entry.Teardown) {
			continue
		}
		request, err := ParseVolumeDeleteRequest(entry.Teardown)
		if err != nil || volumes[request.ID] {
			return fmt.Errorf("Volume reconciliation reused a Volume ID: %v", err)
		}
		volumes[request.ID] = true
	}
	return nil
}

func readVolumeReconcileTransition(root *os.Root, path, name string) (volumeReconcileTransition, error) {
	return readVolumeReconcileTransitionAt(root, path, name, name)
}

func readVolumeReconcileTransitionAt(root *os.Root, path, sourceName, activeName string) (volumeReconcileTransition, error) {
	if !strings.HasPrefix(activeName, filepath.Base(path)+".volume-reconcile-") || !strings.HasSuffix(activeName, ".json") {
		return volumeReconcileTransition{}, fmt.Errorf("Volume reconciliation transition is not private or scoped")
	}
	file, err := root.Open(sourceName)
	if err != nil {
		return volumeReconcileTransition{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		file.Close()
		return volumeReconcileTransition{}, fmt.Errorf("Volume reconciliation transition is not a bounded regular file: %v", err)
	}
	body, private, readErr := ReadRecoveryFileRoot(root, file, sourceName)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !private || len(body) > 1<<20 {
		return volumeReconcileTransition{}, fmt.Errorf("Volume reconciliation transition changed while read: %v", readErr)
	}
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return volumeReconcileTransition{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var transition volumeReconcileTransition
	if decoder.Decode(&transition) != nil || decoder.Decode(new(any)) != io.EOF || transition.Version != 1 || transition.JournalPath != path || !validHistoricalDigest(transition.BeforeSHA256) || !validHistoricalDigest(transition.AfterSHA256) || !validHistoricalDigest(transition.OriginalSHA256) || activeName != volumeReconcileTransitionName(path, transition.AfterSHA256) || transition.BeforeSHA256 == transition.AfterSHA256 || transition.SidecarPath != "" && len(transition.SidecarStates) == 0 || transition.SidecarPath == "" && len(transition.SidecarStates) != 0 || transition.ProofSHA256 != "" && !validHistoricalDigest(transition.ProofSHA256) {
		return volumeReconcileTransition{}, fmt.Errorf("Volume reconciliation transition record is invalid")
	}
	return transition, nil
}

// ReaffirmInstalledVolumeReconcileTransition resumes only the exact successor
// recorded before the first replacement. Callers hold the run and parent locks.
// A sidecar may still be at its permitted pre-final stage; no other state is
// synthesized from decoded fields.
func ReaffirmInstalledVolumeReconcileTransition(root *os.Root, path, beforeSHA, target string, installed []byte, kinds ...string) error {
	if root == nil || !validHistoricalDigest(beforeSHA) || len(installed) == 0 {
		return fmt.Errorf("Volume reconciliation retry lacks an exact predecessor")
	}
	afterSHA := volumeReconcileDigest(installed)
	name := volumeReconcileTransitionName(path, afterSHA)
	transition, err := readVolumeReconcileTransition(root, path, name)
	if err != nil || transition.BeforeSHA256 != beforeSHA || transition.AfterSHA256 != afterSHA || transition.Target != target {
		return fmt.Errorf("Volume reconciliation retry has no matching staged successor: %v", err)
	}
	allowed := false
	for _, kind := range kinds {
		allowed = allowed || kind == transition.Kind
	}
	if !allowed {
		return fmt.Errorf("Volume reconciliation retry changed its staged operation")
	}
	predecessor, err := readHistoricalOriginal(root, path, beforeSHA)
	if err != nil {
		return err
	}
	archivedAfter, err := readHistoricalOriginal(root, path, afterSHA)
	if err != nil || !bytes.Equal(archivedAfter, installed) {
		return fmt.Errorf("Volume reconciliation retry successor archive changed: %v", err)
	}
	var prior, next report.CleanupJournal
	if unmarshalRecoveryJSON(predecessor, &prior) != nil || unmarshalRecoveryJSON(installed, &next) != nil {
		return fmt.Errorf("Volume reconciliation retry journal is invalid")
	}
	var states [][]byte
	for _, digest := range transition.SidecarStates {
		body, err := readHistoricalOriginal(root, path, digest)
		if err != nil || volumeReconcileDigest(body) != digest {
			return fmt.Errorf("Volume reconciliation retry sidecar archive changed: %v", err)
		}
		states = append(states, body)
	}
	var proof []byte
	if transition.ProofSHA256 != "" {
		proof, err = readHistoricalOriginal(root, path, transition.ProofSHA256)
		if err != nil {
			return err
		}
	}
	if err := validateVolumeReconcileTransition(root, transition, prior, next, states, proof); err != nil {
		return err
	}
	var sidecarBefore []byte
	if transition.SidecarPath != "" {
		if len(states) == 0 {
			return fmt.Errorf("Volume reconciliation retry sidecar states are empty")
		}
		sidecarSnapshot, readErr := ReadPrivateRecoverySnapshotRoot(root, filepath.Base(transition.SidecarPath))
		if readErr != nil {
			return fmt.Errorf("Volume reconciliation retry sidecar is unavailable or not private: %v", readErr)
		}
		sidecarBefore = sidecarSnapshot.Body
		last := len(states) - 1
		permitted := bytes.Equal(sidecarBefore, states[last])
		if transition.Kind == "volume-register" && last > 0 {
			permitted = permitted || bytes.Equal(sidecarBefore, states[last-1])
		}
		if transition.Kind == "volume-observe" || transition.Kind == "volume-settle" {
			permitted = permitted || bytes.Equal(sidecarBefore, states[0])
		}
		if !permitted {
			return fmt.Errorf("Volume reconciliation retry sidecar is not an ordered installed state")
		}
	}
	if err := ReaffirmDurableRoot(root, filepath.Base(path), installed); err != nil {
		return err
	}
	currentJournal, err := root.ReadFile(filepath.Base(path))
	if err != nil || !bytes.Equal(currentJournal, installed) {
		return fmt.Errorf("Volume reconciliation retry journal changed before sidecar write: %v", err)
	}
	if transition.SidecarPath != "" {
		name := filepath.Base(transition.SidecarPath)
		current, err := root.ReadFile(name)
		if err != nil || !bytes.Equal(current, sidecarBefore) {
			return fmt.Errorf("Volume reconciliation retry sidecar changed before publication: %v", err)
		}
		last := len(states) - 1
		if !bytes.Equal(current, states[last]) {
			if err := WriteDurableRoot(root, name, states[last]); err != nil {
				installedSidecar, readErr := root.ReadFile(name)
				if readErr != nil || !bytes.Equal(installedSidecar, states[last]) || ReaffirmDurableRoot(root, name, states[last]) != nil {
					return fmt.Errorf("Volume reconciliation retry sidecar durability is uncertain: %w", err)
				}
			}
		} else if err := ReaffirmDurableRoot(root, name, states[last]); err != nil {
			return err
		}
		current, err = root.ReadFile(name)
		if err != nil || !bytes.Equal(current, states[last]) {
			return fmt.Errorf("Volume reconciliation retry sidecar changed after barrier: %v", err)
		}
	}
	if err := ReaffirmDurableRoot(root, filepath.Base(path), installed); err != nil {
		return err
	}
	current, err := root.ReadFile(filepath.Base(path))
	if err != nil || !bytes.Equal(current, installed) {
		return fmt.Errorf("Volume reconciliation retry journal changed after barrier: %v", err)
	}
	return nil
}

// historicalRegisteredVolumeFinalizer accepts an appended Volume finalizer
// only when an archived registration edge proves its exact owner and bytes.
// It does not authorize retirement; that requires a separate terminal proof.
func historicalRegisteredVolumeFinalizer(root *os.Root, path string, journal report.CleanupJournal, entry report.Resource) (bool, error) {
	if root == nil || !IsNativeVolumeDelete(entry.Teardown) || !validHistoricalDigest(journal.OriginalJournalSHA256) {
		return false, nil
	}
	request, err := ParseVolumeDeleteRequest(entry.Teardown)
	if err != nil {
		return false, err
	}
	directory, err := root.Open(".")
	if err != nil {
		return false, err
	}
	defer directory.Close()
	matches := 0
	matchingEntries := 0
	for {
		names, readErr := directory.ReadDir(128)
		if readErr != nil && readErr != io.EOF {
			return false, readErr
		}
		for _, file := range names {
			name := file.Name()
			if !strings.HasPrefix(name, filepath.Base(path)+".volume-reconcile-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			matchingEntries++
			if matchingEntries > 4096 {
				return false, fmt.Errorf("Volume registration archive registry is too large")
			}
			transition, err := readVolumeReconcileTransition(root, path, name)
			if err != nil {
				return false, err
			}
			if transition.Kind != "volume-register" || transition.OriginalSHA256 != journal.OriginalJournalSHA256 || transition.RunID != journal.RunID || transition.ExecutionID != journal.ExecutionID || transition.Region != journal.Region || transition.Surface != journal.Surface {
				continue
			}
			references := 0
			for _, retained := range journal.NativeIntentPaths {
				if retained == transition.SidecarPath {
					references++
				}
			}
			if references != 1 || len(transition.SidecarStates) == 0 {
				continue
			}
			finalSidecar, err := readHistoricalOriginal(root, path, transition.SidecarStates[len(transition.SidecarStates)-1])
			if err != nil {
				return false, err
			}
			var owner TemplateCreateIntent
			if unmarshalRecoveryJSON(finalSidecar, &owner) != nil || owner.AcceptedVolumeID != request.ID || len(owner.Deletes) != 1 || owner.Deletes[0] != entry.Teardown || owner.CaseScope != entry.Scope || owner.RegionRole != entry.RegionRole || owner.Region != entry.Region || owner.ExecutionID != entry.ExecutionID {
				continue
			}
			beforeBody, err := readHistoricalOriginal(root, path, transition.BeforeSHA256)
			if err != nil {
				return false, err
			}
			afterBody, err := readHistoricalOriginal(root, path, transition.AfterSHA256)
			if err != nil {
				return false, err
			}
			var before, after report.CleanupJournal
			if unmarshalRecoveryJSON(beforeBody, &before) != nil || unmarshalRecoveryJSON(afterBody, &after) != nil {
				return false, fmt.Errorf("Volume registration archive journal is invalid")
			}
			states := make([][]byte, 0, len(transition.SidecarStates))
			for _, digest := range transition.SidecarStates {
				body, err := readHistoricalOriginal(root, path, digest)
				if err != nil {
					return false, err
				}
				states = append(states, body)
			}
			if err := validateVolumeReconcileTransition(root, transition, before, after, states, nil); err != nil {
				return false, err
			}
			registered := 0
			for _, candidate := range after.Entries {
				if !historicalSameSelector(candidate, entry) {
					continue
				}
				copy := entry
				copy.VolumeDeleteAttempted = false
				if !reflect.DeepEqual(candidate, copy) {
					return false, fmt.Errorf("Volume registration finalizer fields changed")
				}
				registered++
			}
			if registered != 1 {
				continue
			}
			for _, candidate := range before.Entries {
				if historicalSameSelector(candidate, entry) {
					return false, fmt.Errorf("Volume registration finalizer was already present")
				}
			}
			matches++
		}
		if readErr == io.EOF {
			break
		}
	}
	if matches > 1 {
		return false, fmt.Errorf("Volume registration finalizer has ambiguous custody")
	}
	return matches == 1, nil
}

// ReaffirmCurrentVolumeReconcileTransition finds an incoming edge by the
// installed successor bytes. A retry may supply that successor digest rather
// than the original predecessor digest; only the archived edge supplies the
// predecessor used for the durability check.
func ReaffirmCurrentVolumeReconcileTransition(root *os.Root, path, target string, installed []byte, validate func([]byte, []byte) error, kinds ...string) (bool, error) {
	if root == nil || len(installed) == 0 {
		return false, fmt.Errorf("Volume reconciliation current successor is unavailable")
	}
	name := volumeReconcileTransitionName(path, volumeReconcileDigest(installed))
	if _, err := root.Lstat(name); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return true, err
	}
	transition, err := readVolumeReconcileTransition(root, path, name)
	if err != nil {
		return true, err
	}
	if transition.Target != target {
		return false, nil
	}
	allowed := false
	for _, kind := range kinds {
		allowed = allowed || transition.Kind == kind
	}
	if !allowed {
		return true, fmt.Errorf("Volume reconciliation installed transition changed its staged operation")
	}
	if validate != nil {
		var final []byte
		if len(transition.SidecarStates) != 0 {
			final, err = readHistoricalOriginal(root, path, transition.SidecarStates[len(transition.SidecarStates)-1])
			if err != nil {
				return true, err
			}
		}
		if err := validate(installed, final); err != nil {
			return true, err
		}
	}
	return true, ReaffirmInstalledVolumeReconcileTransition(root, path, transition.BeforeSHA256, target, installed, kinds...)
}

// reaffirmUnusedVolumeReconcileEdge makes the exact predecessor, edge and
// archived sidecars durable before an interrupted local transition publishes
// its successor. A missing archive is never reconstructed on retry.
func reaffirmUnusedVolumeReconcileEdge(root *os.Root, path string, staged volumeReconcileTransition, before []byte) error {
	if err := validateUnusedVolumeReconcileTransition(root, path, staged, before); err != nil {
		return err
	}
	name := volumeReconcileTransitionName(path, staged.AfterSHA256)
	edge, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	if err := ReaffirmDurableRoot(root, name, edge); err != nil {
		return err
	}
	for _, digest := range append([]string{staged.BeforeSHA256, staged.AfterSHA256, staged.OriginalSHA256, staged.ProofSHA256}, staged.SidecarStates...) {
		if digest == "" {
			continue
		}
		archived, err := readHistoricalOriginal(root, path, digest)
		if err != nil {
			return err
		}
		if err := ReaffirmDurableRoot(root, historicalOriginalName(path, digest), archived); err != nil {
			return err
		}
		if current, err := readHistoricalOriginal(root, path, digest); err != nil || !bytes.Equal(current, archived) {
			return fmt.Errorf("Volume reconciliation archive changed after barrier: %v", err)
		}
	}
	if staged.SidecarPath != "" {
		name := filepath.Base(staged.SidecarPath)
		body, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if err := ReaffirmDurableRoot(root, name, body); err != nil {
			return err
		}
	}
	if err := ReaffirmDurableRoot(root, filepath.Base(path), before); err != nil {
		return err
	}
	current, err := readVolumeReconcileTransition(root, path, name)
	if err != nil || !reflect.DeepEqual(current, staged) {
		return fmt.Errorf("Volume reconciliation edge changed after barrier: %v", err)
	}
	return validateUnusedVolumeReconcileTransition(root, path, staged, before)
}

// InstallUntouchedVolumeObserveTransition resumes an already staged positive
// observation from its exact predecessor without repeating a provider probe.
func InstallUntouchedVolumeObserveTransition(root *os.Root, path, target string, original []byte, validate func([]byte, []byte) error) (bool, error) {
	if root == nil || target == "" || len(original) == 0 {
		return false, fmt.Errorf("Volume observation transition lacks its predecessor")
	}
	entries, err := readRootInventory(root)
	if err != nil {
		return false, err
	}
	beforeSHA := volumeReconcileDigest(original)
	var matched *volumeReconcileTransition
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), filepath.Base(path)+".volume-reconcile-") {
			continue
		}
		transition, err := readVolumeReconcileTransition(root, path, entry.Name())
		if err != nil {
			return false, err
		}
		if transition.BeforeSHA256 != beforeSHA || transition.Target != target || transition.Kind != "volume-observe" {
			continue
		}
		if matched != nil || transition.SidecarPath == "" {
			return false, fmt.Errorf("Volume observation has an ambiguous staged successor")
		}
		matched = &transition
	}
	if matched == nil {
		return false, nil
	}
	if err := reaffirmUnusedVolumeReconcileEdge(root, path, *matched, original); err != nil {
		return false, err
	}
	after, err := readHistoricalOriginal(root, path, matched.AfterSHA256)
	if err != nil {
		return false, err
	}
	if validate != nil {
		final, err := readHistoricalOriginal(root, path, matched.SidecarStates[len(matched.SidecarStates)-1])
		if err != nil {
			return false, err
		}
		if err := validate(after, final); err != nil {
			return false, err
		}
	}
	if err := WriteDurableRoot(root, filepath.Base(path), after); err != nil {
		installed, readErr := root.ReadFile(filepath.Base(path))
		if readErr != nil || !bytes.Equal(installed, after) {
			return false, fmt.Errorf("Volume observation successor durability is uncertain: %w", err)
		}
	}
	if err := ReaffirmInstalledVolumeReconcileTransition(root, path, beforeSHA, target, after, "volume-observe"); err != nil {
		return false, err
	}
	return true, nil
}
