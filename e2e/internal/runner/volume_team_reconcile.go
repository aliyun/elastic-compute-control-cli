package runner

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
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// VolumeTeamReconcileOptions binds an operator's read-only restart probe to
// the original journal and binary, while allowing a separately pinned probe
// binary. Reconciliation never sends CreateTeam or a delete.
type VolumeTeamReconcileOptions struct {
	Config           execpkg.Config
	RunID            string
	Surface          string
	Name             string
	Profile          string
	JournalSHA256    string
	JournalBinSHA256 string
	ProbeBin         string
	ProbeBinSHA256   string
}

type VolumeTeamReconcileResult struct {
	RunID              string `json:"run_id"`
	TeamName           string `json:"team_name"`
	TeamID             string `json:"team_id"`
	JournalSHA256      string `json:"journal_sha256"`
	ReservationRetired bool   `json:"reservation_retired,omitempty"`
}

func teamReconcileSHA256(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Team reconciliation input is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validTeamReconcileSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func readVolumeTeamReconcileJournal(path string, opt VolumeTeamReconcileOptions, accepted *journalfile.RecoverySnapshot) (report.CleanupJournal, error) {
	snapshot, err := journalfile.ReadPrivateRecoverySnapshot(path)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	hash := sha256.Sum256(snapshot.Body)
	digest := hex.EncodeToString(hash[:])
	if digest != opt.JournalSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume Team reconciliation journal digest changed")
	}
	journal, legacy, err := decodeRunnerJournal(path, snapshot.Body, true)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	if legacy || journal.Version != 2 || journal.JournalPath != path || journal.RunID != opt.RunID || journal.RegionRole != "primary" || journal.ExecutionID == "" || journal.Surface != opt.Surface || journal.Region != opt.Config.Region || !filepath.IsAbs(journal.EcctlBin) || journal.EcctlBinSHA256 == "" || journal.EcctlBinSHA256 != opt.JournalBinSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume Team reconciliation journal binding changed")
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return report.CleanupJournal{}, err
	}
	if err := validateRetainedVolumeFixture(journal); err != nil {
		return report.CleanupJournal{}, err
	}
	if accepted != nil {
		*accepted = snapshot
	}
	return journal, nil
}

func readVolumeTeamReconcileJournalRoot(root *os.Root, path string, opt VolumeTeamReconcileOptions) (report.CleanupJournal, error) {
	body, err := readVolumeReconcileRootFile(root, filepath.Base(path), path)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	hash := sha256.Sum256(body)
	if hex.EncodeToString(hash[:]) != opt.JournalSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume Team reconciliation journal digest changed")
	}
	journal, legacy, err := decodeRunnerJournal(path, body, true)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	if legacy || journal.Version != 2 || journal.JournalPath != path || journal.RunID != opt.RunID || journal.RegionRole != "primary" || journal.ExecutionID == "" || journal.Surface != opt.Surface || journal.Region != opt.Config.Region || !filepath.IsAbs(journal.EcctlBin) || journal.EcctlBinSHA256 == "" || journal.EcctlBinSHA256 != opt.JournalBinSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume Team reconciliation journal binding changed")
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return report.CleanupJournal{}, err
	}
	if err := validateRetainedVolumeFixture(journal); err != nil {
		return report.CleanupJournal{}, err
	}
	return journal, nil
}

func readVolumeTeamReconcileIntent(path string, journal report.CleanupJournal, opt VolumeTeamReconcileOptions, roots ...*os.Root) (*teamCreateIntent, error) {
	intentPath := path + ".team-create-" + opt.Name[8:] + ".json"
	var body []byte
	var err error
	if len(roots) != 0 {
		body, err = readVolumeReconcileRootFile(roots[0], filepath.Base(intentPath), intentPath)
	} else {
		snapshot, readErr := journalfile.ReadPrivateRecoverySnapshot(intentPath)
		body, err = snapshot.Body, readErr
	}
	if err != nil {
		return nil, err
	}
	var intent teamCreateIntent
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return nil, fmt.Errorf("Volume Team reconciliation intent is ambiguous: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("Volume Team reconciliation intent has trailing data")
	}
	var parent os.FileInfo
	if len(roots) != 0 {
		parent, err = roots[0].Stat(".")
	} else {
		parent, err = journalParentIdentity(path)
	}
	if err != nil {
		return nil, err
	}
	intent.path, intent.journalPath, intent.parent = intentPath, path, parent
	if intent.Version != 1 || intent.CreatedAt.IsZero() || intent.Name != opt.Name || intent.Description != "ecctl E2E Team owner="+opt.Name[8:] || intent.RunID != journal.RunID || intent.ExecutionID != journal.ExecutionID || intent.Region != journal.Region || intent.Surface != journal.Surface || intent.EcctlBin != journal.EcctlBin || intent.VolumeBinding == nil || !reflect.DeepEqual(intent.VolumeBinding, journal.VolumeFixtureBinding) || intent.VolumeBinding.Profile != opt.Profile || intent.QuotaBinding != nil && !reflect.DeepEqual(intent.QuotaBinding, journal.QuotaBinding) || intent.AcceptedID != "" && !safeTeamIdentity(intent.AcceptedID) || intent.ProbeBinSHA256 != "" && !validTeamReconcileSHA256(intent.ProbeBinSHA256) {
		return nil, fmt.Errorf("Volume Team reconciliation intent binding changed")
	}
	if err := checkVolumeTeamIntent(&intent, path, journal); err != nil {
		return nil, err
	}
	if err := validateTeamCreateInput(intent.Input, intent.Name, intent.Description, intent.Region); err != nil {
		return nil, fmt.Errorf("Volume Team reconciliation create input changed: %w", err)
	}
	if intent.CaseScope == "" || strings.ContainsAny(intent.CaseScope, "\r\n") {
		return nil, fmt.Errorf("Volume Team reconciliation case scope changed")
	}
	if _, err := renderReservedTeamDelete(&intent, "intent-validation"); err != nil {
		return nil, err
	}
	return &intent, nil
}

func renderReservedTeamDelete(intent *teamCreateIntent, id string) (string, error) {
	if intent == nil || !safeTeamIdentity(id) || strings.Count(intent.Teardown, "intent-validation") != 1 {
		return "", fmt.Errorf("Volume Team reserved teardown is invalid")
	}
	command := strings.Replace(intent.Teardown, "intent-validation", id, 1)
	target, err := journalfile.TeamDeleteTarget(command)
	if err != nil || target != id || !isReplayableTeardown(command) {
		return "", fmt.Errorf("Volume Team reserved teardown target changed")
	}
	return command, nil
}

// settleReconciledVolumeTeam writes the finalizer, creator binding and nonce
// settlement in one journal replacement. A repeated call accepts only the
// same exact finalizer and binding.
func settleReconciledVolumeTeam(journal *report.CleanupJournal, intent *teamCreateIntent, id string) (bool, error) {
	if !safeTeamIdentity(id) || journal == nil || intent == nil || intent.VolumeBinding == nil || intent.AcceptedID != id || intent.DispatchProtocol != 1 || !intent.Dispatched {
		return false, fmt.Errorf("Volume Team reconciliation identity is invalid")
	}
	state, reserved := journal.VolumeTeamCreateIDs[intent.Name]
	if !reserved || state != "" && state != id {
		return false, fmt.Errorf("Volume Team nonce reservation changed")
	}
	for name, otherID := range journal.VolumeTeamCreateIDs {
		if name != intent.Name && otherID == id {
			return false, fmt.Errorf("Volume Team ID is reserved by another nonce")
		}
	}
	if binding, exists := journal.VolumeTeamCreators[id]; exists && !reflect.DeepEqual(binding, intent.VolumeBinding) {
		return false, fmt.Errorf("Volume Team creator binding collided")
	}
	command, err := renderReservedTeamDelete(intent, id)
	if err != nil {
		return false, err
	}
	count := 0
	for _, entry := range journal.Entries {
		if !journalfile.IsTeamDeleteCandidate(entry.Teardown) {
			continue
		}
		target, err := journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, intent.Region)
		if err != nil {
			return false, err
		}
		if target != id {
			continue
		}
		count++
		if !entry.TeamCreateConfirmed || entry.Teardown != command || entry.Scope != intent.CaseScope || entry.RegionRole != "primary" || entry.Region != intent.Region || entry.ExecutionID != intent.ExecutionID || !entry.VolumeTeamBound || entry.QuotaBound != (intent.QuotaBinding != nil) || entry.TeamReplayAttempted {
			return false, fmt.Errorf("Volume Team finalizer collision")
		}
	}
	if count > 1 || state == id && count != 1 {
		return false, fmt.Errorf("Volume Team finalizer is missing or ambiguous")
	}
	if state == id {
		if binding, exists := journal.VolumeTeamCreators[id]; !exists || !reflect.DeepEqual(binding, intent.VolumeBinding) {
			return false, fmt.Errorf("Volume Team settled creator association is missing")
		}
		return false, nil
	}
	if count == 0 {
		journal.Entries = append(journal.Entries, report.Resource{Scope: intent.CaseScope, Teardown: command, RegionRole: "primary", Region: intent.Region, ExecutionID: intent.ExecutionID, TeamCreateConfirmed: true, VolumeTeamBound: true, QuotaBound: intent.QuotaBinding != nil})
	}
	if journal.VolumeTeamCreators == nil {
		journal.VolumeTeamCreators = make(map[string]*report.QuotaCredentialBinding)
	}
	journal.VolumeTeamCreators[id] = intent.VolumeBinding
	journal.VolumeTeamCreateIDs[intent.Name] = id
	return true, nil
}

// ReconcilePendingVolumeTeam reads one nonce-bound Team through the original
// credential identity, then durably registers its existing cleanup finalizer.
func ReconcilePendingVolumeTeam(ctx context.Context, path string, opt VolumeTeamReconcileOptions) (result *VolumeTeamReconcileResult, err error) {
	if opt.RunID == "" || (opt.Surface != "public" && opt.Surface != "full") || opt.Config.Region == "" || opt.Profile == "" || len(opt.Name) != 32 || !strings.HasPrefix(opt.Name, "ecctl-t-") || !safeTeamIdentity(opt.Name) || !filepath.IsAbs(opt.Config.Bin) || !filepath.IsAbs(opt.ProbeBin) || !validTeamReconcileSHA256(opt.JournalSHA256) || !validTeamReconcileSHA256(opt.JournalBinSHA256) || !validTeamReconcileSHA256(opt.ProbeBinSHA256) {
		return nil, fmt.Errorf("Volume Team reconciliation requires exact run, surface, region, profile, nonce and binary digests")
	}
	if _, err := hex.DecodeString(opt.Name[8:]); err != nil {
		return nil, fmt.Errorf("Volume Team reconciliation nonce is invalid")
	}
	resolved, err := journalfile.ResolveJournalPath(path)
	if err != nil {
		return nil, err
	}
	parent, err := journalParentIdentity(resolved)
	if err != nil || !parent.IsDir() {
		return nil, fmt.Errorf("Volume Team reconciliation journal parent is unavailable: %v", err)
	}
	release, err := journalfile.AcquireExistingRunLease(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()
	checkParent := func() error {
		current, statErr := journalParentIdentity(resolved)
		if statErr != nil || !os.SameFile(parent, current) {
			return fmt.Errorf("Volume Team reconciliation journal parent changed")
		}
		return nil
	}
	err = journalfile.WithExistingReplayLock(ctx, resolved, func() error {
		if err := checkParent(); err != nil {
			return err
		}
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			root, err := os.OpenRoot(filepath.Dir(resolved))
			if err != nil {
				return err
			}
			defer root.Close()
			rootInfo, err := root.Stat(".")
			if err != nil || !os.SameFile(parent, rootInfo) {
				return fmt.Errorf("Volume Team retirement journal parent changed")
			}
			body, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(body)
			if hex.EncodeToString(digest[:]) == opt.JournalSHA256 {
				return nil
			}
			current, legacy, err := decodeRunnerJournal(resolved, body, true)
			if err != nil || legacy || current.Version != 2 || current.RunID != opt.RunID || current.Surface != opt.Surface || current.Region != opt.Config.Region || current.RegionRole != "primary" || current.ExecutionID == "" || !filepath.IsAbs(current.EcctlBin) || current.EcctlBinSHA256 != opt.JournalBinSHA256 || journalfile.ValidateVolumeFixtureIdentity(current.VolumeFixtureBinding) != nil || validateRetainedVolumeFixture(current) != nil {
				return fmt.Errorf("Volume Team retirement journal binding changed: %v", err)
			}
			key := retirementReceiptKey("team", opt.Name)
			retired, err := reaffirmVolumeRetirement(root, resolved, body, current, key, opt.JournalSHA256, func(prior *report.CleanupJournal, receipt report.VolumeRetirementReceipt) error {
				_, stillReserved := prior.VolumeTeamCreateIDs[opt.Name]
				if receipt.TeamName != opt.Name || receipt.FixtureEntry != nil || stillReserved {
					return fmt.Errorf("Volume Team retirement receipt changed")
				}
				if prior.VolumeTeamCreateIDs == nil {
					prior.VolumeTeamCreateIDs = make(map[string]string)
				}
				prior.VolumeTeamCreateIDs[opt.Name] = ""
				intent, err := readVolumeTeamReconcileIntent(resolved, *prior, opt, root)
				if err != nil || intent.DispatchProtocol != 1 || intent.Dispatched || intent.AcceptedID != "" {
					return fmt.Errorf("Volume Team retirement lacks the original unlaunched intent: %v", err)
				}
				return nil
			})
			if err != nil || !retired {
				settled, settleErr := retryAcceptedVolumeTeamSettlement(root, resolved, body, current, opt)
				if settleErr != nil || settled == nil {
					return fmt.Errorf("Volume Team reconciliation journal digest changed: %v", errors.Join(err, settleErr))
				}
				if err := checkParent(); err != nil {
					return err
				}
				result = settled
				return nil
			}
			if err := checkParent(); err != nil {
				return err
			}
			result = &VolumeTeamReconcileResult{RunID: current.RunID, TeamName: opt.Name, JournalSHA256: hex.EncodeToString(digest[:]), ReservationRetired: true}
			return nil
		}); err != nil {
			return err
		}
		if result != nil {
			return nil
		}
		for _, pair := range [][2]string{{opt.Config.Bin, opt.JournalBinSHA256}, {opt.ProbeBin, opt.ProbeBinSHA256}} {
			digest, err := journalfile.ExecutableSHA256(pair[0])
			if err != nil {
				return err
			}
			if digest != pair[1] {
				return fmt.Errorf("Volume Team reconciliation binary digest changed")
			}
		}
		var journal report.CleanupJournal
		var accepted journalfile.RecoverySnapshot
		var intent *teamCreateIntent
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			var err error
			journal, err = readVolumeTeamReconcileJournal(resolved, opt, &accepted)
			if err != nil {
				return err
			}
			intent, err = readVolumeTeamReconcileIntent(resolved, journal, opt)
			return err
		}); err != nil {
			return err
		}
		if intent.DispatchProtocol == 1 && !intent.Dispatched && intent.AcceptedID == "" {
			return journalfile.WithExistingParentLock(ctx, resolved, func() error {
				root, err := os.OpenRoot(filepath.Dir(resolved))
				if err != nil {
					return err
				}
				defer root.Close()
				rootInfo, err := root.Stat(".")
				if err != nil || !os.SameFile(parent, rootInfo) {
					return fmt.Errorf("Volume Team retirement journal parent changed")
				}
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				current, err := readVolumeTeamReconcileJournalRoot(root, resolved, opt)
				if err != nil || !reflect.DeepEqual(current, journal) {
					return fmt.Errorf("Volume Team retirement journal changed: %v", err)
				}
				canonical, err := runnerJournalBytes(current)
				if err != nil || !bytes.Equal(accepted.Body, canonical) {
					return fmt.Errorf("Volume Team retirement requires original canonical journal bytes: %v", err)
				}
				currentIntent, err := readVolumeTeamReconcileIntent(resolved, current, opt, root)
				if err != nil {
					return err
				}
				oldValue, newValue := *intent, *currentIntent
				oldValue.path, oldValue.journalPath, oldValue.parent = "", "", nil
				newValue.path, newValue.journalPath, newValue.parent = "", "", nil
				if !reflect.DeepEqual(oldValue, newValue) || currentIntent.DispatchProtocol != 1 || currentIntent.Dispatched || currentIntent.AcceptedID != "" {
					return fmt.Errorf("Volume Team pre-dispatch receipt changed")
				}
				if state, exists := current.VolumeTeamCreateIDs[currentIntent.Name]; !exists || state != "" {
					return fmt.Errorf("Volume Team pre-dispatch reservation changed")
				}
				key := retirementReceiptKey("team", currentIntent.Name)
				if current.VolumeRetirementReceipts == nil {
					current.VolumeRetirementReceipts = make(map[string]report.VolumeRetirementReceipt)
				}
				if _, exists := current.VolumeRetirementReceipts[key]; exists {
					return fmt.Errorf("Volume Team retirement receipt already exists")
				}
				current.VolumeRetirementReceipts[key] = report.VolumeRetirementReceipt{OriginalSHA256: opt.JournalSHA256, TeamName: currentIntent.Name}
				delete(current.VolumeTeamCreateIDs, currentIntent.Name)
				if len(current.VolumeTeamCreateIDs) == 0 {
					current.VolumeTeamCreateIDs = nil
				}
				anchorVolumeReconcileJournal(&current, accepted.Body)
				body, err := runnerJournalBytes(current)
				if err != nil {
					return err
				}
				intentBody, err := readVolumeReconcileRootFile(root, filepath.Base(currentIntent.path), currentIntent.path)
				if err != nil {
					return err
				}
				if err := journalfile.StageVolumeReconcileTransition(root, resolved, "team-retire", currentIntent.Name, accepted.Body, body, currentIntent.path, intentBody); err != nil {
					return err
				}
				if err := journalfile.EnsureHistoricalJournalOriginal(root, resolved, opt.JournalSHA256, accepted.Body); err != nil {
					return fmt.Errorf("Volume Team retirement predecessor is not durable: %w", err)
				}
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				if err := writeVolumeReconcileRootBytes(root, resolved, body); err != nil {
					return err
				}
				installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
				if err != nil {
					return err
				}
				installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
				if err != nil || legacy || !reflect.DeepEqual(installed, current) {
					return fmt.Errorf("Volume Team pre-dispatch retirement was not installed exactly")
				}
				if err := checkParent(); err != nil {
					return err
				}
				digest := sha256.Sum256(installedBody)
				result = &VolumeTeamReconcileResult{RunID: current.RunID, TeamName: currentIntent.Name, JournalSHA256: hex.EncodeToString(digest[:]), ReservationRetired: true}
				return nil
			})
		}
		if intent.DispatchProtocol != 1 || !intent.Dispatched || intent.AcceptedID == "" {
			return fmt.Errorf("Volume Team reconciliation lacks durable dispatch and accepted create proof")
		}
		if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, journal.EcctlBin, opt.JournalBinSHA256, journal.Surface); err != nil {
			return err
		}
		probeCfg := execpkg.Config{Bin: opt.ProbeBin, ExpectedBinSHA256: opt.ProbeBinSHA256, Region: journal.Region, Env: opt.Config.Env}
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := checkParent(); err != nil {
			cancel()
			return err
		}
		bound, err := journalfile.ProbeQuotaBinding(probeCtx, intent.VolumeBinding, probeCfg)
		cancel()
		if err != nil {
			return err
		}
		probeCtx, cancel = context.WithTimeout(ctx, time.Minute)
		id, err := lookupReservedTeamID(probeCtx, bound, intent)
		cancel()
		if err != nil {
			return err
		}
		if intent.AcceptedID != id {
			return fmt.Errorf("Volume Team name lookup is diagnostic without a durable invocation-linked ID")
		}
		if err := checkParent(); err != nil {
			return err
		}
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			root, err := os.OpenRoot(filepath.Dir(resolved))
			if err != nil {
				return err
			}
			defer root.Close()
			rootInfo, err := root.Stat(".")
			if err != nil || !os.SameFile(parent, rootInfo) {
				return fmt.Errorf("Volume Team reconciliation journal parent changed")
			}
			if err := checkParent(); err != nil {
				return err
			}
			if err := accepted.VerifyCurrent(resolved); err != nil {
				return err
			}
			for _, pair := range [][2]string{{opt.Config.Bin, opt.JournalBinSHA256}, {opt.ProbeBin, opt.ProbeBinSHA256}} {
				digest, err := teamReconcileSHA256(pair[0])
				if err != nil {
					return err
				}
				if digest != pair[1] {
					return fmt.Errorf("Volume Team reconciliation binary changed after probe")
				}
			}
			current, err := readVolumeTeamReconcileJournalRoot(root, resolved, opt)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, journal) {
				return fmt.Errorf("Volume Team journal changed during read-only reconciliation")
			}
			currentIntent, err := readVolumeTeamReconcileIntent(resolved, current, opt, root)
			if err != nil {
				return err
			}
			oldValue, newValue := *intent, *currentIntent
			oldValue.path, oldValue.journalPath, oldValue.parent = "", "", nil
			newValue.path, newValue.journalPath, newValue.parent = "", "", nil
			if !reflect.DeepEqual(oldValue, newValue) {
				return fmt.Errorf("Volume Team intent changed during reconciliation")
			}
			changed, err := settleReconciledVolumeTeam(&current, currentIntent, id)
			if err != nil {
				return err
			}
			if currentIntent.ProbeBinSHA256 != "" && currentIntent.ProbeBinSHA256 != opt.ProbeBinSHA256 {
				return fmt.Errorf("Volume Team accepted probe binary changed")
			}
			if err := accepted.VerifyCurrent(resolved); err != nil {
				return err
			}
			if changed {
				proofBefore, err := readVolumeReconcileRootFile(root, filepath.Base(currentIntent.path), currentIntent.path)
				if err != nil {
					return err
				}
				pinProbe := currentIntent.ProbeBinSHA256 == ""
				proofAfter := proofBefore
				if pinProbe {
					currentIntent.ProbeBinSHA256 = opt.ProbeBinSHA256
					proofBody, err := json.MarshalIndent(currentIntent, "", "  ")
					if err != nil {
						return err
					}
					proofAfter = append(proofBody, '\n')
				}
				anchorVolumeReconcileJournal(&current, accepted.Body)
				body, err := runnerJournalBytes(current)
				if err != nil {
					return err
				}
				states := [][]byte{proofBefore}
				if !bytes.Equal(proofBefore, proofAfter) {
					states = append(states, proofAfter)
				}
				states, err = journalfile.StageVolumeReconcileTransitionSelected(root, resolved, "team-accept", currentIntent.Name, accepted.Body, body, currentIntent.path, states...)
				if err != nil {
					return err
				}
				proofAfter = states[len(states)-1]
				if _, err := execpkg.DecodeStrictJSON(proofAfter); err != nil {
					return fmt.Errorf("Volume Team staged probe proof is ambiguous: %w", err)
				}
				var selectedProof teamCreateIntent
				if err := json.Unmarshal(proofAfter, &selectedProof); err != nil {
					return err
				}
				expectedProof := *currentIntent
				expectedProof.path, expectedProof.journalPath, expectedProof.parent = "", "", nil
				if selectedProof.ProbeBinSHA256 != opt.ProbeBinSHA256 || !reflect.DeepEqual(selectedProof, expectedProof) {
					return fmt.Errorf("Volume Team staged probe proof differs from the requested authority")
				}
				if !bytes.Equal(proofBefore, proofAfter) {
					if err := journalfile.WriteDurableRoot(root, filepath.Base(currentIntent.path), proofAfter); err != nil {
						return fmt.Errorf("Volume Team accepted probe pin is not durable: %w", err)
					}
				}
				intentBody, err := readVolumeReconcileRootFile(root, filepath.Base(currentIntent.path), currentIntent.path)
				if err != nil {
					return err
				}
				if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(currentIntent.path), intentBody); err != nil {
					return fmt.Errorf("Volume Team accepted create proof is not durable: %w", err)
				}
				reaffirmed, err := readVolumeTeamReconcileIntent(resolved, journal, opt, root)
				if err != nil {
					return err
				}
				prior, after := *currentIntent, *reaffirmed
				prior.path, prior.journalPath, prior.parent = "", "", nil
				after.path, after.journalPath, after.parent = "", "", nil
				if !reflect.DeepEqual(prior, after) || reaffirmed.DispatchProtocol != 1 || !reaffirmed.Dispatched || reaffirmed.AcceptedID != id {
					return fmt.Errorf("Volume Team accepted create proof changed after durability retry")
				}
				if err := journalfile.EnsureHistoricalJournalOriginal(root, resolved, opt.JournalSHA256, accepted.Body); err != nil {
					return fmt.Errorf("Volume Team accepted predecessor is not durable: %w", err)
				}
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				if err := writeVolumeReconcileRootBytes(root, resolved, body); err != nil {
					return err
				}
				installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
				if err != nil {
					return err
				}
				installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
				if err != nil || legacy || !reflect.DeepEqual(installed, current) {
					return fmt.Errorf("Volume Team finalizer was not durably installed exactly")
				}
			} else {
				for _, file := range []string{currentIntent.path, resolved} {
					body, err := readVolumeReconcileRootFile(root, filepath.Base(file), file)
					if err != nil {
						return err
					}
					if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(file), body); err != nil {
						return err
					}
				}
				installed, err := readVolumeTeamReconcileJournalRoot(root, resolved, opt)
				if err != nil || !reflect.DeepEqual(installed, current) {
					return fmt.Errorf("Volume Team journal changed after durability retry: %v", err)
				}
				installedIntent, err := readVolumeTeamReconcileIntent(resolved, installed, opt, root)
				if err != nil {
					return err
				}
				oldValue, newValue := *currentIntent, *installedIntent
				oldValue.path, oldValue.journalPath, oldValue.parent = "", "", nil
				newValue.path, newValue.journalPath, newValue.parent = "", "", nil
				if !reflect.DeepEqual(oldValue, newValue) {
					return fmt.Errorf("Volume Team sidecar changed after durability retry")
				}
			}
			finalBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			finalHash := sha256.Sum256(finalBody)
			digest := hex.EncodeToString(finalHash[:])
			if err := checkParent(); err != nil {
				return err
			}
			result = &VolumeTeamReconcileResult{RunID: current.RunID, TeamName: currentIntent.Name, TeamID: id, JournalSHA256: digest}
			return nil
		}); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

// A post-rename failure leaves the accepted finalizer installed while the
// operator still holds the predecessor digest. The archived exact bytes and
// accepted intent must reconstruct precisely the installed successor.
func retryAcceptedVolumeTeamSettlement(root *os.Root, path string, body []byte, current report.CleanupJournal, opt VolumeTeamReconcileOptions) (*VolumeTeamReconcileResult, error) {
	if opt.Config.Bin == "" || opt.ProbeBin == "" || !validTeamReconcileSHA256(opt.JournalBinSHA256) || !validTeamReconcileSHA256(opt.ProbeBinSHA256) || current.EcctlBinSHA256 != opt.JournalBinSHA256 || current.Surface != opt.Surface {
		return nil, fmt.Errorf("Volume Team settlement binary binding changed")
	}
	for _, pair := range [][2]string{{opt.Config.Bin, opt.JournalBinSHA256}, {opt.ProbeBin, opt.ProbeBinSHA256}} {
		digest, err := journalfile.ExecutableSHA256(pair[0])
		if err != nil || digest != pair[1] {
			return nil, fmt.Errorf("Volume Team settlement binary digest changed: %v", err)
		}
	}
	if err := verifyArchivedVolumeBinarySurface(context.Background(), opt.Config, current.EcctlBin, opt.JournalBinSHA256, current.Surface); err != nil {
		return nil, err
	}
	predecessor, err := journalfile.ReadHistoricalJournalOriginal(root, path, opt.JournalSHA256)
	if err != nil {
		return nil, err
	}
	prior, legacy, err := decodeRunnerJournal(path, predecessor, true)
	if err != nil || legacy || prior.Version != 2 || prior.JournalPath != path || prior.RunID != opt.RunID || prior.Surface != opt.Surface || prior.Region != opt.Config.Region || prior.EcctlBinSHA256 != opt.JournalBinSHA256 || journalfile.ValidateVolumeFixtureIdentity(prior.VolumeFixtureBinding) != nil || validateRetainedVolumeFixture(prior) != nil {
		return nil, fmt.Errorf("Volume Team predecessor binding changed: %v", err)
	}
	intent, err := readVolumeTeamReconcileIntent(path, prior, opt, root)
	if err != nil || intent.DispatchProtocol != 1 || !intent.Dispatched || intent.AcceptedID == "" || intent.ProbeBinSHA256 != opt.ProbeBinSHA256 {
		return nil, fmt.Errorf("Volume Team accepted predecessor intent changed: %v", err)
	}
	changed, err := settleReconciledVolumeTeam(&prior, intent, intent.AcceptedID)
	anchorVolumeReconcileJournal(&prior, predecessor)
	if err != nil || !changed || !reflect.DeepEqual(prior, current) {
		return nil, fmt.Errorf("Volume Team installed settlement differs from accepted predecessor: %v", err)
	}
	if err := journalfile.ReaffirmInstalledVolumeReconcileTransition(root, path, opt.JournalSHA256, opt.Name, body, "team-accept"); err != nil {
		return nil, err
	}
	intentBody, err := readVolumeReconcileRootFile(root, filepath.Base(intent.path), intent.path)
	if err != nil {
		return nil, err
	}
	if err := journalfile.EnsureHistoricalJournalOriginal(root, path, opt.JournalSHA256, predecessor); err != nil {
		return nil, fmt.Errorf("Volume Team predecessor durability is uncertain: %w", err)
	}
	if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(intent.path), intentBody); err != nil {
		return nil, fmt.Errorf("Volume Team accepted create proof durability is uncertain: %w", err)
	}
	if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(path), body); err != nil {
		return nil, fmt.Errorf("Volume Team settlement durability is uncertain: %w", err)
	}
	installed, err := readVolumeReconcileRootFile(root, filepath.Base(path), path)
	if err != nil || !bytes.Equal(installed, body) {
		return nil, fmt.Errorf("Volume Team settlement changed after durability retry: %v", err)
	}
	if _, err := readVolumeTeamReconcileIntent(path, current, opt, root); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	return &VolumeTeamReconcileResult{RunID: current.RunID, TeamName: intent.Name, TeamID: intent.AcceptedID, JournalSHA256: hex.EncodeToString(digest[:])}, nil
}
