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
	"strconv"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
)

// teamCompletionReceipt retains the exact protected Team target and the
// pre-consumption journal, whether or not that journal has historical lineage.
type teamCompletionReceipt struct {
	Version        int    `json:"version"`
	JournalPath    string `json:"journal_path"`
	JournalSHA256  string `json:"journal_sha256"`
	EcctlBinSHA256 string `json:"ecctl_bin_sha256"`
	ReadBinSHA256  string `json:"read_bin_sha256,omitempty"`
	RunID          string `json:"run_id"`
	ExecutionID    string `json:"execution_id"`
	Region         string `json:"region"`
	Surface        string `json:"surface"`
	EntrySHA256    string `json:"entry_sha256"`
	TeamID         string `json:"team_id"`
	Profile        string `json:"profile"`
	Proof          string `json:"proof"`
	Evidence       string `json:"evidence"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

func teamCompletionPath(path string, entry report.Resource) string {
	digest := sha256.Sum256([]byte(entry.Teardown))
	return path + ".team-completion-" + hex.EncodeToString(digest[:]) + ".json"
}

func teamAbsenceCommandMatches(result execpkg.Result, id, region, profile, binSHA256 string) bool {
	expected, err := shlex.Split(volumeTeamAbsenceCommand(id, region, profile))
	if err != nil || len(expected) < 2 || !validHistoricalDigest(binSHA256) || result.VerifiedBinSHA256 != binSHA256 {
		return false
	}
	if reflect.DeepEqual(result.Argv, expected[1:]) {
		return true
	}
	return reflect.DeepEqual(result.Argv, append(expected[1:], "--no-color=true"))
}

func teamCompletionBinding(journal report.CleanupJournal, entry report.Resource) (string, string, error) {
	if !entry.TeamCreateConfirmed || !entry.TeamReplayAttempted || !entry.QuotaBound && !entry.VolumeTeamBound || journal.Version != 2 || journal.JournalPath == "" || journal.EcctlBinSHA256 == "" {
		return "", "", fmt.Errorf("protected Team completion lacks a settled attempted finalizer")
	}
	id, err := TeamDeleteTarget(entry.Teardown)
	if err != nil {
		return "", "", err
	}
	binding := journal.QuotaBinding
	if entry.VolumeTeamBound {
		binding = journal.VolumeTeamCreators[id]
		if err := CheckVolumeTeamCreationLink(journal, id, &entry); err != nil {
			return "", "", err
		}
	}
	if binding == nil || binding.Profile == "" {
		return "", "", fmt.Errorf("protected Team completion lacks its creator profile")
	}
	return id, binding.Profile, nil
}

func readTeamCompletion(root *os.Root, path string, journal report.CleanupJournal, entry report.Resource) (*teamCompletionReceipt, error) {
	if root == nil {
		return nil, fmt.Errorf("protected Team completion lacks an anchored journal")
	}
	full := teamCompletionPath(path, entry)
	snapshot, err := ReadPrivateRecoverySnapshotRoot(root, filepath.Base(full))
	if err != nil {
		return nil, err
	}
	var receipt teamCompletionReceipt
	if _, err := execpkg.DecodeStrictJSON(snapshot.Body); err != nil {
		return nil, fmt.Errorf("protected Team completion receipt is ambiguous: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("protected Team completion receipt is invalid: %v", err)
	}
	id, profile, err := teamCompletionBinding(journal, entry)
	if err != nil {
		return nil, err
	}
	entryBody, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	entryHash := sha256.Sum256(entryBody)
	evidenceHash := sha256.Sum256([]byte(receipt.Evidence))
	if receipt.Version != 1 || receipt.JournalPath != path || !validHistoricalDigest(receipt.JournalSHA256) || receipt.EcctlBinSHA256 != journal.EcctlBinSHA256 || receipt.ReadBinSHA256 != "" && (!validHistoricalDigest(receipt.ReadBinSHA256) || receipt.Proof != "verified-absence" || entry.VolumeTeamBound) || receipt.RunID != journal.RunID || receipt.ExecutionID != journal.ExecutionID || receipt.Region != journal.Region || receipt.Surface != journal.Surface || receipt.EntrySHA256 != hex.EncodeToString(entryHash[:]) || receipt.TeamID != id || receipt.Profile != profile || receipt.EvidenceSHA256 != hex.EncodeToString(evidenceHash[:]) {
		return nil, fmt.Errorf("protected Team completion selectors changed")
	}
	var payload any
	if unmarshalRecoveryJSON([]byte(receipt.Evidence), &payload) != nil {
		return nil, fmt.Errorf("protected Team completion evidence is invalid")
	}
	if receipt.Proof == "verified-absence" {
		if volumeTeamAbsenceResult(execpkg.Result{Started: true, Exit: 4, JSON: payload}) != nil {
			return nil, fmt.Errorf("protected Team absence evidence is invalid")
		}
	} else if receipt.Proof == "delete-result" {
		root, ok := payload.(map[string]any)
		team, _ := root["team"].(map[string]any)
		if !ok || root["deleted"] != true || root["dry_run"] != nil || team["id"] != id {
			return nil, fmt.Errorf("protected Team delete result differs from its target")
		}
	} else {
		return nil, fmt.Errorf("protected Team completion proof is unsupported")
	}
	predecessor, err := readHistoricalOriginal(root, path, receipt.JournalSHA256)
	if err != nil {
		return nil, err
	}
	var prior report.CleanupJournal
	if unmarshalRecoveryJSON(predecessor, &prior) != nil || prior.RunID != journal.RunID || prior.ExecutionID != journal.ExecutionID || prior.Region != journal.Region || prior.Surface != journal.Surface || prior.EcctlBinSHA256 != journal.EcctlBinSHA256 || !teamCompletionAnchorCompatible(prior.OriginalJournalSHA256, journal.OriginalJournalSHA256, receipt.JournalSHA256) {
		return nil, fmt.Errorf("protected Team completion predecessor changed")
	}
	count := 0
	for _, candidate := range prior.Entries {
		if reflect.DeepEqual(candidate, entry) {
			count++
		}
	}
	if count != 1 || snapshot.VerifyRoot(root, filepath.Base(full)) != nil {
		return nil, fmt.Errorf("protected Team completion target is absent or ambiguous")
	}
	return &receipt, nil
}

func teamCompletionAnchorCompatible(before, after, predecessor string) bool {
	return before == after || before == "" && after == predecessor
}

func teamCompletionEntryProgress(before, after, target report.Resource) bool {
	if reflect.DeepEqual(before, after) {
		return true
	}
	if reflect.DeepEqual(before, target) || reflect.DeepEqual(after, target) || before.TeamReplayAttempted || !after.TeamReplayAttempted || !before.TeamCreateConfirmed {
		return false
	}
	before.TeamReplayAttempted = true
	return reflect.DeepEqual(before, after)
}

func teamCompletionSuccessorPreservesEntry(root *os.Root, path string, prior, current report.CleanupJournal, entry report.Resource) bool {
	candidate := prior
	candidate.Entries = current.Entries
	anchor := prior.OriginalJournalSHA256
	if anchor != current.OriginalJournalSHA256 {
		if anchor != "" || current.OriginalJournalSHA256 == "" {
			return false
		}
		archived, err := readHistoricalOriginal(root, path, current.OriginalJournalSHA256)
		var retained report.CleanupJournal
		if err != nil || unmarshalRecoveryJSON(archived, &retained) != nil || !reflect.DeepEqual(retained, prior) {
			return false
		}
		anchor = current.OriginalJournalSHA256
		candidate.OriginalJournalSHA256 = anchor
	}
	if !reflect.DeepEqual(prior.HistoricalCompletionReceipts, current.HistoricalCompletionReceipts) {
		if anchor == "" {
			return false
		}
		originalBody, err := readHistoricalOriginal(root, path, anchor)
		if err != nil {
			return false
		}
		var original report.CleanupJournal
		if unmarshalRecoveryJSON(originalBody, &original) != nil {
			return false
		}
		for key, retained := range prior.HistoricalCompletionReceipts {
			if current.HistoricalCompletionReceipts[key] != retained {
				return false
			}
		}
		for key, receipt := range current.HistoricalCompletionReceipts {
			if _, retained := prior.HistoricalCompletionReceipts[key]; retained {
				continue
			}
			index, err := strconv.Atoi(key)
			if err != nil || key != strconv.Itoa(index) || index < 0 || index >= len(original.Entries) || reflect.DeepEqual(original.Entries[index], entry) {
				return false
			}
			stored, err := readHistoricalCompletion(root, path, historicalCompletionName(path, anchor, index))
			entryBody, marshalErr := json.Marshal(original.Entries[index])
			entryHash := sha256.Sum256(entryBody)
			if err != nil || marshalErr != nil || stored != receipt || receipt.OriginalIndex != index || receipt.JournalPath != path || receipt.OriginalJournalSHA256 != anchor || receipt.EntrySHA256 != hex.EncodeToString(entryHash[:]) || receipt.RunID != prior.RunID || receipt.ExecutionID != prior.ExecutionID || receipt.EcctlBinSHA256 != prior.EcctlBinSHA256 {
				return false
			}
		}
		candidate.HistoricalCompletionReceipts = current.HistoricalCompletionReceipts
	}
	if !reflect.DeepEqual(candidate, current) {
		return false
	}
	index, exact := 0, 0
	for _, candidate := range current.Entries {
		if reflect.DeepEqual(candidate, entry) {
			exact++
		}
		for index < len(prior.Entries) && !teamCompletionEntryProgress(prior.Entries[index], candidate, entry) {
			index++
		}
		if index == len(prior.Entries) {
			return false
		}
		index++
	}
	return exact == 1
}

// ReadTeamCompletion distinguishes an absent stage from an invalid one.
func ReadTeamCompletion(root *os.Root, path string, journal report.CleanupJournal, entry report.Resource) (bool, error) {
	_, err := readTeamCompletion(root, path, journal, entry)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// RecordTeamCompletion retains a private Volume Team absence receipt before
// removing its finalizer. An existing exact stage is durably reaffirmed.
func RecordTeamCompletion(root *os.Root, path string, journal report.CleanupJournal, entry report.Resource, readback execpkg.Result) error {
	if !entry.VolumeTeamBound {
		return fmt.Errorf("new Team completion receipts require Volume ownership")
	}
	id, profile, err := teamCompletionBinding(journal, entry)
	if err != nil {
		return err
	}
	journalBody, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return err
	}
	var installed report.CleanupJournal
	if unmarshalRecoveryJSON(journalBody, &installed) != nil || !reflect.DeepEqual(installed, journal) {
		return fmt.Errorf("protected Team completion journal changed before staging")
	}
	journalHash := sha256.Sum256(journalBody)
	digest := hex.EncodeToString(journalHash[:])
	if existing, err := readTeamCompletion(root, path, journal, entry); err == nil {
		if existing.JournalSHA256 != digest {
			predecessor, err := readHistoricalOriginal(root, path, existing.JournalSHA256)
			if err != nil {
				return err
			}
			var prior report.CleanupJournal
			if unmarshalRecoveryJSON(predecessor, &prior) != nil || !teamCompletionSuccessorPreservesEntry(root, path, prior, journal, entry) {
				return fmt.Errorf("protected Team completion journal changed")
			}
		}
		full := teamCompletionPath(path, entry)
		body, readErr := root.ReadFile(filepath.Base(full))
		if readErr != nil {
			return readErr
		}
		return ReaffirmDurableRoot(root, filepath.Base(full), body)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := EnsureHistoricalJournalOriginal(root, path, digest, journalBody); err != nil {
		return err
	}
	if volumeTeamAbsenceResult(readback) != nil || !teamAbsenceCommandMatches(readback, id, journal.Region, profile, journal.EcctlBinSHA256) {
		return fmt.Errorf("Volume Team completion lacks an exact creator-bound absence readback")
	}
	proof, evidence := "verified-absence", readback.Stdout
	if len(evidence) == 0 || len(evidence) > 1<<19 || !json.Valid([]byte(evidence)) {
		return fmt.Errorf("protected Team completion evidence is invalid")
	}
	var parsed any
	if unmarshalRecoveryJSON([]byte(evidence), &parsed) != nil {
		return fmt.Errorf("protected Team completion evidence is invalid")
	}
	if volumeTeamAbsenceResult(execpkg.Result{Started: true, Exit: 4, JSON: parsed}) != nil {
		return fmt.Errorf("protected Team absence evidence is invalid")
	}
	entryBody, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	entryHash, evidenceHash := sha256.Sum256(entryBody), sha256.Sum256([]byte(evidence))
	receipt := teamCompletionReceipt{Version: 1, JournalPath: path, JournalSHA256: digest, EcctlBinSHA256: journal.EcctlBinSHA256, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EntrySHA256: hex.EncodeToString(entryHash[:]), TeamID: id, Profile: profile, Proof: proof, Evidence: evidence, EvidenceSHA256: hex.EncodeToString(evidenceHash[:])}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	full := teamCompletionPath(path, entry)
	if err := WriteExclusiveDurableRoot(root, filepath.Base(full), append(body, '\n')); err != nil {
		return err
	}
	_, err = readTeamCompletion(root, path, journal, entry)
	return err
}
