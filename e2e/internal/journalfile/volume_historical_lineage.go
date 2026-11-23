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

func historicalOriginalName(path, digest string) string {
	return filepath.Base(path) + ".historical-original-" + digest + ".json"
}

func readHistoricalOriginal(root *os.Root, path, digest string) ([]byte, error) {
	name := historicalOriginalName(path, digest)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("historical original archive is unavailable: %w", err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("historical original archive is not a regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	privateBefore := privateRecoveryDescriptor(file)
	body, readErr := io.ReadAll(io.LimitReader(file, 8<<20+1))
	privateAfter := privateRecoveryDescriptor(file)
	closeErr := file.Close()
	after, afterErr := root.Lstat(name)
	named, namedErr := os.Lstat(filepath.Join(filepath.Dir(path), name))
	hash := sha256.Sum256(body)
	if statErr != nil || readErr != nil || closeErr != nil || afterErr != nil || namedErr != nil || opened == nil || !os.SameFile(before, opened) || !os.SameFile(before, after) || !os.SameFile(opened, named) || len(body) > 8<<20 || !privateBefore || !privateAfter || hex.EncodeToString(hash[:]) != digest {
		return nil, fmt.Errorf("historical original archive differs from independent custody")
	}
	return body, nil
}

func ensureHistoricalOriginal(root *os.Root, path, digest string, body []byte) error {
	hash := sha256.Sum256(body)
	if !validHistoricalDigest(digest) || hex.EncodeToString(hash[:]) != digest {
		return fmt.Errorf("historical original archive lacks exact retained bytes")
	}
	if archived, err := readHistoricalOriginal(root, path, digest); err == nil {
		if !bytes.Equal(archived, body) {
			return fmt.Errorf("historical original archive changed")
		}
		if err := ReaffirmDurableRoot(root, historicalOriginalName(path, digest), body); err != nil {
			return fmt.Errorf("historical original archive barrier is uncertain: %w", err)
		}
		return nil
	} else if _, statErr := root.Lstat(historicalOriginalName(path, digest)); statErr == nil || !os.IsNotExist(statErr) {
		return fmt.Errorf("historical original archive is invalid: %w", err)
	}
	name := historicalOriginalName(path, digest)
	if err := WriteDurableRoot(root, name, body); err != nil {
		return fmt.Errorf("historical original archive durability is uncertain: %w", err)
	}
	archived, err := readHistoricalOriginal(root, path, digest)
	if err != nil || !bytes.Equal(archived, body) {
		return fmt.Errorf("historical original archive installation changed: %v", err)
	}
	return nil
}

// EnsureHistoricalJournalOriginal retains authenticated predecessor bytes
// before a reconciliation writes a successor. The caller holds the journal lock.
func EnsureHistoricalJournalOriginal(root *os.Root, path, digest string, body []byte) error {
	if root == nil || !filepath.IsAbs(path) {
		return fmt.Errorf("historical journal archive lacks an anchored path")
	}
	return ensureHistoricalOriginal(root, path, digest, body)
}

func ReadHistoricalJournalOriginal(root *os.Root, path, digest string) ([]byte, error) {
	if root == nil || !filepath.IsAbs(path) || !validHistoricalDigest(digest) {
		return nil, fmt.Errorf("historical journal original selector is invalid")
	}
	return readHistoricalOriginal(root, path, digest)
}

func historicalCompletionName(path, originalDigest string, index int) string {
	return filepath.Base(path) + ".replay-completion-" + originalDigest[:16] + "-" + fmt.Sprint(index) + ".json"
}

func decodeHistoricalCompletionReceipt(body []byte) (report.HistoricalCompletionReceipt, error) {
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return report.HistoricalCompletionReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var receipt report.HistoricalCompletionReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return receipt, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return receipt, fmt.Errorf("historical completion receipt has trailing data")
	}
	if (receipt.Version != 1 && receipt.Version != 2) || receipt.RunID == "" || receipt.ExecutionID == "" ||
		!filepath.IsAbs(receipt.JournalPath) || !validHistoricalDigest(receipt.EcctlBinSHA256) ||
		!validHistoricalDigest(receipt.OriginalJournalSHA256) || !validHistoricalDigest(receipt.EntrySHA256) ||
		!validHistoricalDigest(receipt.EvidenceSHA256) || receipt.OriginalIndex < 0 ||
		receipt.Proof != "delete-result" && receipt.Proof != "verified-absence" {
		return receipt, fmt.Errorf("historical completion receipt is incomplete")
	}
	if receipt.Version == 2 {
		if receipt.Evidence == "" || len(receipt.Evidence)+len(receipt.Readbacks) > 1<<19 {
			return receipt, fmt.Errorf("historical completion evidence is unavailable")
		}
		if receipt.Readbacks != "" {
			var readbacks []historicalReadbackEvidence
			if unmarshalRecoveryJSON([]byte(receipt.Readbacks), &readbacks) != nil || len(readbacks) == 0 {
				return receipt, fmt.Errorf("historical completion readbacks are invalid")
			}
			for _, readback := range readbacks {
				if readback.Command == "" || readback.Stdout == "" {
					return receipt, fmt.Errorf("historical completion readback is incomplete")
				}
				if _, err := execpkg.DecodeStrictJSON([]byte(readback.Stdout)); err != nil {
					return receipt, fmt.Errorf("historical completion readback is incomplete")
				}
			}
		}
		actual := sha256.Sum256([]byte(receipt.Evidence + "\x00" + receipt.Readbacks))
		if receipt.EvidenceSHA256 != hex.EncodeToString(actual[:]) {
			return receipt, fmt.Errorf("historical completion evidence changed")
		}
	}
	return receipt, nil
}

type historicalReadbackEvidence struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Stdout  string `json:"stdout"`
}

func readHistoricalCompletion(root *os.Root, path, name string) (report.HistoricalCompletionReceipt, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return report.HistoricalCompletionReceipt{}, fmt.Errorf("historical completion receipt is unavailable: %v", err)
	}
	file, err := root.Open(name)
	if err != nil {
		return report.HistoricalCompletionReceipt{}, err
	}
	opened, statErr := file.Stat()
	privateBefore := privateRecoveryDescriptor(file)
	body, readErr := io.ReadAll(io.LimitReader(file, 1<<20+1))
	privateAfter := privateRecoveryDescriptor(file)
	closeErr := file.Close()
	after, afterErr := root.Lstat(name)
	fullPath := filepath.Join(filepath.Dir(path), name)
	named, namedErr := os.Lstat(fullPath)
	if statErr != nil || readErr != nil || closeErr != nil || afterErr != nil || namedErr != nil ||
		opened == nil || !os.SameFile(before, opened) || !os.SameFile(before, after) ||
		!os.SameFile(opened, named) || len(body) > 1<<20 || !privateBefore || !privateAfter {
		return report.HistoricalCompletionReceipt{}, fmt.Errorf("historical completion receipt changed")
	}
	return decodeHistoricalCompletionReceipt(body)
}

// RecordHistoricalCompletionAt preserves the original ordinal of a repeated
// finalizer selected from the current journal before its removal.
func RecordHistoricalCompletionAt(root *os.Root, path string, journal *report.CleanupJournal, activeIndex int, entry report.Resource, evidence []byte, proof string, readbacks ...execpkg.Result) error {
	return recordHistoricalCompletion(root, path, journal, activeIndex, entry, evidence, proof, readbacks...)
}

func historicalActiveOrdinals(original, current report.CleanupJournal) []int {
	ordinals := make([]int, len(current.Entries))
	next := 0
	for currentIndex, entry := range current.Entries {
		ordinals[currentIndex] = -1
		candidates := 0
		for i := next; i < len(original.Entries); i++ {
			if _, completed := current.HistoricalCompletionReceipts[fmt.Sprint(i)]; !completed && historicalSameSelector(original.Entries[i], entry) {
				candidates++
			}
		}
		for originalIndex := next; originalIndex < len(original.Entries); originalIndex++ {
			prior := original.Entries[originalIndex]
			if _, completed := current.HistoricalCompletionReceipts[fmt.Sprint(originalIndex)]; completed ||
				!historicalSameSelector(prior, entry) ||
				candidates > 1 && !historicalStagedEntryMatches(prior, entry) {
				continue
			}
			ordinals[currentIndex] = originalIndex
			next = originalIndex + 1
			break
		}
	}
	return ordinals
}

func historicalSameSelector(a, b report.Resource) bool {
	return a.Teardown == b.Teardown && a.Scope == b.Scope && a.RegionRole == b.RegionRole &&
		a.Region == b.Region && a.ExecutionID == b.ExecutionID
}

func recordHistoricalCompletion(root *os.Root, path string, journal *report.CleanupJournal, activeIndex int, entry report.Resource, evidence []byte, proof string, readbacks ...execpkg.Result) error {
	if journal.OriginalJournalSHA256 == "" {
		return nil
	}
	if proof != "delete-result" && proof != "verified-absence" {
		return fmt.Errorf("historical completion proof is invalid")
	}
	body, err := readHistoricalOriginal(root, path, journal.OriginalJournalSHA256)
	if err != nil {
		return err
	}
	var original report.CleanupJournal
	if err := unmarshalRecoveryJSON(body, &original); err != nil {
		return err
	}
	if activeIndex < 0 || activeIndex >= len(journal.Entries) || journal.Entries[activeIndex] != entry {
		return fmt.Errorf("historical active cleanup ordinal changed")
	}
	index := historicalActiveOrdinals(original, *journal)[activeIndex]
	if index < 0 {
		if entry.TeamCreateConfirmed && entry.VolumeTeamBound {
			staged, err := ReadTeamCompletion(root, path, *journal, entry)
			if err != nil {
				return err
			}
			if staged {
				return nil
			}
		}
		return fmt.Errorf("historical cleanup entry has no unconsumed original target")
	}
	if journal.HistoricalCompletionReceipts == nil {
		journal.HistoricalCompletionReceipts = make(map[string]report.HistoricalCompletionReceipt)
	}
	entryBody, err := json.Marshal(original.Entries[index])
	if err != nil {
		return err
	}
	if len(evidence) == 0 {
		return fmt.Errorf("historical completion evidence is empty")
	}
	reads := make([]historicalReadbackEvidence, 0, len(readbacks))
	for _, read := range readbacks {
		if !read.Started || read.Err != nil || read.Stdout == "" {
			return fmt.Errorf("historical completion readback is incomplete")
		}
		reads = append(reads, historicalReadbackEvidence{Command: read.Command, Exit: read.Exit, Stdout: read.Stdout})
	}
	readbackBody := ""
	if len(reads) != 0 {
		encoded, err := json.Marshal(reads)
		if err != nil {
			return err
		}
		readbackBody = string(encoded)
	}
	if len(evidence)+len(readbackBody) > 1<<19 {
		return fmt.Errorf("historical completion evidence exceeds the private receipt limit")
	}
	entryHash, evidenceHash := sha256.Sum256(entryBody), sha256.Sum256([]byte(string(evidence)+"\x00"+readbackBody))
	predecessorSHA, predecessorEntrySHA := "", ""
	if original.Entries[index].VolumeFixtureKind != "" && original.Entries[index].VolumeFixturePending && !entry.VolumeFixturePending {
		installedBody, err := root.ReadFile(filepath.Base(path))
		if err != nil {
			return err
		}
		var installed report.CleanupJournal
		if unmarshalRecoveryJSON(installedBody, &installed) != nil || !reflect.DeepEqual(installed.Entries, journal.Entries) || installed.OriginalJournalSHA256 != journal.OriginalJournalSHA256 || installed.RunID != journal.RunID || installed.ExecutionID != journal.ExecutionID || installed.EcctlBinSHA256 != journal.EcctlBinSHA256 {
			return fmt.Errorf("historical fixture completion predecessor changed")
		}
		predecessorSHA = volumeReconcileDigest(installedBody)
		activeBody, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		predecessorEntrySHA = volumeReconcileDigest(activeBody)
		if err := EnsureHistoricalJournalOriginal(root, path, predecessorSHA, installedBody); err != nil {
			return err
		}
	}
	receipt := report.HistoricalCompletionReceipt{
		Version: 2, RunID: journal.RunID, ExecutionID: journal.ExecutionID,
		JournalPath: path, EcctlBinSHA256: journal.EcctlBinSHA256,
		OriginalJournalSHA256: journal.OriginalJournalSHA256, OriginalIndex: index,
		EntrySHA256: hex.EncodeToString(entryHash[:]), PredecessorJournalSHA256: predecessorSHA, PredecessorEntrySHA256: predecessorEntrySHA, EvidenceSHA256: hex.EncodeToString(evidenceHash[:]),
		Evidence: string(evidence), Readbacks: readbackBody, Proof: proof,
	}
	name := historicalCompletionName(path, journal.OriginalJournalSHA256, index)
	if retained, readErr := readHistoricalCompletion(root, path, name); readErr == nil {
		if retained.Version != receipt.Version || retained.RunID != receipt.RunID ||
			retained.ExecutionID != receipt.ExecutionID || retained.JournalPath != receipt.JournalPath ||
			retained.EcctlBinSHA256 != receipt.EcctlBinSHA256 ||
			retained.OriginalJournalSHA256 != receipt.OriginalJournalSHA256 ||
			retained.OriginalIndex != receipt.OriginalIndex || retained.EntrySHA256 != receipt.EntrySHA256 {
			return fmt.Errorf("historical completion receipt changed")
		}
		receipt = retained
		body, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if err := ReaffirmDurableRoot(root, name, body); err != nil {
			return fmt.Errorf("historical completion receipt barrier is uncertain: %w", err)
		}
		if installed, err := readHistoricalCompletion(root, path, name); err != nil || installed != receipt {
			return fmt.Errorf("historical completion receipt changed after barrier: %v", err)
		}
	} else {
		if _, statErr := root.Lstat(name); !os.IsNotExist(statErr) {
			return fmt.Errorf("historical completion receipt is invalid: %v", readErr)
		}
		body, err := json.MarshalIndent(receipt, "", "  ")
		if err != nil {
			return err
		}
		body = append(body, '\n')
		if err := WriteDurableRoot(root, name, body); err != nil {
			return fmt.Errorf("historical completion receipt durability is uncertain: %w", err)
		}
		if installed, err := readHistoricalCompletion(root, path, name); err != nil || installed != receipt {
			return fmt.Errorf("historical completion receipt installation changed: %v", err)
		}
	}
	journal.HistoricalCompletionReceipts[fmt.Sprint(index)] = receipt
	return nil
}

func historicalStagedEntryMatches(original, active report.Resource, cacheSettlement ...bool) bool {
	if original.TeamReplayAttempted && !active.TeamReplayAttempted ||
		original.VolumeDeleteAttempted && !active.VolumeDeleteAttempted ||
		original.VolumeFixtureDeleteAttempted && !active.VolumeFixtureDeleteAttempted ||
		!original.VolumeTransferPending && active.VolumeTransferPending ||
		!original.CacheCreatePending && active.CacheCreatePending ||
		original.CacheCreatePending && !active.CacheCreatePending && (len(cacheSettlement) == 0 || !cacheSettlement[0]) {
		return false
	}
	active.TeamReplayAttempted = original.TeamReplayAttempted
	active.VolumeDeleteAttempted = original.VolumeDeleteAttempted
	active.VolumeFixtureDeleteAttempted = original.VolumeFixtureDeleteAttempted
	active.VolumeTransferPending = original.VolumeTransferPending
	if original.CacheCreatePending && !active.CacheCreatePending {
		active.CacheCreatePending = true
	}
	if original.VolumeFixturePending && !active.VolumeFixturePending {
		if len(cacheSettlement) < 2 || !cacheSettlement[1] {
			return false
		}
		return true
	}
	return active == original
}

// A pending Volume Team may only disappear through a durable pre-dispatch
// retirement, or become the exact accepted Team named by its private intent.
// Other creator and nonce changes remain forbidden by the map comparison.
func historicalVolumeTeamTransition(root *os.Root, path string, original, current report.CleanupJournal, contents map[string][]byte, authenticated map[string]bool, base string) (*report.Resource, error) {
	ids := make(map[string]string, len(original.VolumeTeamCreateIDs))
	for name, id := range original.VolumeTeamCreateIDs {
		ids[name] = id
	}
	creators := make(map[string]*report.QuotaCredentialBinding, len(original.VolumeTeamCreators))
	for id, binding := range original.VolumeTeamCreators {
		creators[id] = binding
	}
	var added *report.Resource
	acceptedCount := 0
	for name, priorID := range original.VolumeTeamCreateIDs {
		id, retained := current.VolumeTeamCreateIDs[name]
		if retained && id == priorID {
			continue
		}
		if priorID != "" || len(name) != 32 || !strings.HasPrefix(name, "ecctl-t-") || !safeTemplateID(name) {
			return nil, fmt.Errorf("historical Volume Team reservation changed")
		}
		sidecar := base + ".team-create-" + name[8:] + ".json"
		var intent volumeTeamReceipt
		if !authenticated[sidecar] || unmarshalRecoveryJSON(contents[sidecar], &intent) != nil || intent.Version != 1 || intent.Name != name || intent.CreatedAt.IsZero() || intent.RunID != current.RunID || intent.ExecutionID != current.ExecutionID || intent.Region != current.Region || intent.Surface != current.Surface || intent.EcctlBin != current.EcctlBin || intent.Description != "ecctl E2E Team owner="+name[8:] || intent.CaseScope == "" || intent.VolumeBinding == nil || !reflect.DeepEqual(intent.VolumeBinding, current.VolumeFixtureBinding) || !reflect.DeepEqual(intent.QuotaBinding, current.QuotaBinding) || intent.DispatchProtocol != 1 {
			return nil, fmt.Errorf("historical Volume Team intent is unavailable or changed")
		}
		if !retained {
			if root == nil {
				return nil, fmt.Errorf("historical Volume Team retirement lacks an anchored predecessor")
			}
			receipt, ok := current.VolumeRetirementReceipts["team:"+name]
			if !ok || receipt.TeamName != name || receipt.FixtureEntry != nil || !validHistoricalDigest(receipt.OriginalSHA256) || intent.Dispatched || intent.AcceptedID != "" {
				return nil, fmt.Errorf("historical Volume Team retirement proof changed")
			}
			predecessor, err := readHistoricalOriginal(root, path, receipt.OriginalSHA256)
			if err != nil {
				return nil, fmt.Errorf("historical Volume Team retirement predecessor is unavailable: %w", err)
			}
			var prior report.CleanupJournal
			if unmarshalRecoveryJSON(predecessor, &prior) != nil || prior.VolumeTeamCreateIDs[name] != "" || prior.RunID != current.RunID || prior.ExecutionID != current.ExecutionID || prior.Region != current.Region || !reflect.DeepEqual(prior.VolumeFixtureBinding, current.VolumeFixtureBinding) {
				return nil, fmt.Errorf("historical Volume Team retirement predecessor changed")
			}
			delete(ids, name)
			continue
		}
		if id == "" || !safeTemplateID(id) || intent.AcceptedID != id || !intent.Dispatched || acceptedCount != 0 {
			return nil, fmt.Errorf("historical Volume Team accepted target changed")
		}
		acceptedCount++
		if len(intent.Input) == 0 || intent.Input[0] != "ecctl" || strings.Count(intent.Teardown, "intent-validation") != 1 {
			return nil, fmt.Errorf("historical Volume Team accepted command changed")
		}
		command := strings.Replace(intent.Teardown, "intent-validation", id, 1)
		if target, err := TeamDeleteTarget(command); err != nil || target != id {
			return nil, fmt.Errorf("historical Volume Team accepted finalizer changed: %v", err)
		}
		entry := report.Resource{Scope: intent.CaseScope, Teardown: command, RegionRole: "primary", Region: intent.Region, ExecutionID: intent.ExecutionID, TeamCreateConfirmed: true, VolumeTeamBound: true, QuotaBound: intent.QuotaBinding != nil}
		if existing, exists := creators[id]; exists {
			if !reflect.DeepEqual(existing, intent.VolumeBinding) {
				return nil, fmt.Errorf("historical Volume Team creator ID collided")
			}
			finalizers := 0
			for _, candidate := range original.Entries {
				if candidate.Teardown == command {
					if candidate != entry {
						return nil, fmt.Errorf("historical Volume Team finalizer changed")
					}
					finalizers++
				}
			}
			if finalizers != 1 {
				return nil, fmt.Errorf("historical Volume Team finalizer is missing or duplicated")
			}
		} else {
			for _, candidate := range original.Entries {
				if candidate.Teardown == command {
					return nil, fmt.Errorf("historical Volume Team finalizer has no creator")
				}
			}
			added = &entry
			creators[id] = intent.VolumeBinding
		}
		ids[name] = id
	}
	if len(ids) == 0 {
		ids = nil
	}
	if len(creators) == 0 {
		creators = nil
	}
	if !reflect.DeepEqual(ids, current.VolumeTeamCreateIDs) || !reflect.DeepEqual(creators, current.VolumeTeamCreators) {
		return nil, fmt.Errorf("historical Volume Team creator selectors changed")
	}
	return added, nil
}

func validateHistoricalSuccessor(root *os.Root, path string, original, current report.CleanupJournal, contents map[string][]byte, authenticated map[string]bool, base string) error {
	additionalTeam, err := historicalVolumeTeamTransition(root, path, original, current, contents, authenticated, base)
	if err != nil {
		return err
	}
	if original.Version != current.Version || original.RunID != current.RunID || original.ExecutionID != current.ExecutionID || original.RegionRole != current.RegionRole || original.Region != current.Region || original.Surface != current.Surface || original.EcctlBin != current.EcctlBin || original.EcctlBinSHA256 != "" && original.EcctlBinSHA256 != current.EcctlBinSHA256 || original.NativeJournalPath != current.NativeJournalPath ||
		!reflect.DeepEqual(original.NativeIntentPaths, current.NativeIntentPaths) || !reflect.DeepEqual(original.QuotaBinding, current.QuotaBinding) || !reflect.DeepEqual(original.VolumeFixtureBinding, current.VolumeFixtureBinding) || original.VolumeFixtureBucket != current.VolumeFixtureBucket || !reflect.DeepEqual(original.VolumeTeamBindings, current.VolumeTeamBindings) || !reflect.DeepEqual(original.VolumeScopeBindings, current.VolumeScopeBindings) || !reflect.DeepEqual(original.VolumeObservationReceipts, current.VolumeObservationReceipts) || !reflect.DeepEqual(original.VolumeTransferHistory, current.VolumeTransferHistory) {
		return fmt.Errorf("historical journal selectors changed")
	}
	anchor := original
	if root != nil && validHistoricalDigest(current.OriginalJournalSHA256) {
		body, err := readHistoricalOriginal(root, path, current.OriginalJournalSHA256)
		anchor = report.CleanupJournal{}
		if err != nil || unmarshalRecoveryJSON(body, &anchor) != nil {
			return fmt.Errorf("historical completion original is unavailable: %v", err)
		}
	}
	priorOrdinals := historicalActiveOrdinals(anchor, original)
	used := make([]bool, len(original.Entries))
	next := 0
	for _, entry := range current.Entries {
		if additionalTeam != nil {
			copy := entry
			copy.TeamReplayAttempted = false
			if copy == *additionalTeam {
				additionalTeam = nil
				continue
			}
		}
		matched := false
		for i := next; i < len(original.Entries); i++ {
			prior := original.Entries[i]
			ordinal := priorOrdinals[i]
			_, completed := current.HistoricalCompletionReceipts[fmt.Sprint(ordinal)]
			if used[i] || ordinal >= 0 && completed || entry.Teardown != prior.Teardown || entry.Scope != prior.Scope || entry.RegionRole != prior.RegionRole || entry.Region != prior.Region || entry.ExecutionID != prior.ExecutionID {
				continue
			}
			copy := entry
			if prior.TeamReplayAttempted && !copy.TeamReplayAttempted || prior.VolumeDeleteAttempted && !copy.VolumeDeleteAttempted || prior.VolumeFixtureDeleteAttempted && !copy.VolumeFixtureDeleteAttempted || prior.VolumeTransferPending != copy.VolumeTransferPending {
				continue
			}
			copy.TeamReplayAttempted = prior.TeamReplayAttempted
			copy.VolumeDeleteAttempted = prior.VolumeDeleteAttempted
			copy.VolumeFixtureDeleteAttempted = prior.VolumeFixtureDeleteAttempted
			if prior.CacheCreatePending && !copy.CacheCreatePending && IsTemplateCacheDelete(copy.Teardown) {
				digest := sha256.Sum256([]byte(copy.Teardown))
				name := base + ".template-cache-reconcile-" + hex.EncodeToString(digest[:8]) + ".json"
				if len(contents[name]) == 0 || !authenticated[name] {
					continue
				}
				copy.CacheCreatePending = true
			}
			if reflect.DeepEqual(copy, prior) {
				used[i], matched = true, true
				next = i + 1
				break
			}
		}
		if !matched {
			return fmt.Errorf("historical cleanup target or transition changed")
		}
	}
	if additionalTeam != nil {
		completed := *additionalTeam
		completed.TeamReplayAttempted = true
		staged, err := ReadTeamCompletion(root, path, current, completed)
		if err != nil || !staged {
			return fmt.Errorf("historical Volume Team finalizer lacks durable completion proof: %v", err)
		}
	}
	for key, prior := range original.HistoricalCompletionReceipts {
		if current.HistoricalCompletionReceipts[key] != prior {
			return fmt.Errorf("historical completion receipt changed")
		}
	}
	for i, prior := range original.Entries {
		index := priorOrdinals[i]
		if index < 0 {
			if registered, err := historicalRegisteredVolumeFinalizer(root, path, original, prior); err != nil {
				return err
			} else if registered {
				if !used[i] {
					return fmt.Errorf("historical Volume finalizer lacks terminal retirement proof")
				}
				continue
			}
			if !prior.TeamCreateConfirmed || !prior.VolumeTeamBound {
				return fmt.Errorf("historical cleanup target lacks original custody")
			}
			if !used[i] {
				completed := prior
				completed.TeamReplayAttempted = true
				staged, err := ReadTeamCompletion(root, path, current, completed)
				if err != nil || !staged {
					return fmt.Errorf("historical Volume Team finalizer lacks durable completion proof: %v", err)
				}
			}
			continue
		}
		if index >= len(anchor.Entries) || !historicalSameSelector(anchor.Entries[index], prior) {
			return fmt.Errorf("historical cleanup original ordinal changed")
		}
		if used[i] {
			if _, present := current.HistoricalCompletionReceipts[fmt.Sprint(index)]; present {
				return fmt.Errorf("historical completion receipt retains an active target")
			}
			continue
		}
		if _, present := current.HistoricalCompletionReceipts[fmt.Sprint(index)]; !present && !historicalRetiredVolume(current, prior, contents, base) {
			return fmt.Errorf("historical cleanup target lacks durable completion proof")
		}
	}
	for key, receipt := range current.HistoricalCompletionReceipts {
		index := receipt.OriginalIndex
		if index < 0 || index >= len(anchor.Entries) || key != fmt.Sprint(index) ||
			!validHistoricalDigest(receipt.OriginalJournalSHA256) || receipt.OriginalJournalSHA256 != current.OriginalJournalSHA256 ||
			!validHistoricalDigest(receipt.EvidenceSHA256) || receipt.Proof != "delete-result" && receipt.Proof != "verified-absence" {
			return fmt.Errorf("historical completion receipt has no original target")
		}
		entryBody, err := json.Marshal(anchor.Entries[index])
		if err != nil {
			return err
		}
		entryHash := sha256.Sum256(entryBody)
		name := historicalCompletionName(current.JournalPath, receipt.OriginalJournalSHA256, index)
		stored, err := decodeHistoricalCompletionReceipt(contents[name])
		if err != nil || !authenticated[name] || stored != receipt ||
			receipt.EntrySHA256 != hex.EncodeToString(entryHash[:]) ||
			receipt.RunID != current.RunID || receipt.ExecutionID != current.ExecutionID ||
			receipt.JournalPath != current.JournalPath || receipt.EcctlBinSHA256 != current.EcctlBinSHA256 {
			return fmt.Errorf("historical cleanup completion sidecar is unavailable")
		}
	}
	return nil
}

func historicalRetiredVolume(current report.CleanupJournal, entry report.Resource, contents map[string][]byte, base string) bool {
	if !IsNativeVolumeDelete(entry.Teardown) {
		return false
	}
	for name, receipt := range current.VolumeTerminalReceipts {
		if receipt.Teardown != entry.Teardown || len(name) != 32 || !strings.HasPrefix(name, "ecctl-p-") {
			continue
		}
		var intent TemplateCreateIntent
		if unmarshalRecoveryJSON(contents[base+".template-create-"+name[8:]+".json"], &intent) != nil ||
			intent.Name != name || !intent.VolumeObserved || len(intent.Deletes) != 1 ||
			receipt != volumeTerminalReceipt(current, intent) {
			continue
		}
		active := false
		for _, candidate := range current.Entries {
			active = active || sameTemplateDelete(candidate.Teardown, entry.Teardown)
		}
		if !active {
			return true
		}
	}
	return false
}

func validHistoricalDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
