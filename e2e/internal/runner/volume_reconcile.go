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
	surfacepkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/surface"
)

type VolumeReconcileOptions struct {
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

type VolumeReconcileResult struct {
	RunID         string `json:"run_id"`
	VolumeName    string `json:"volume_name"`
	VolumeID      string `json:"volume_id"`
	TeamID        string `json:"team_id"`
	JournalSHA256 string `json:"journal_sha256"`
}

func validateRetainedVolumeFixture(journal report.CleanupJournal) error {
	if journal.VolumeFixtureBinding == nil {
		return fmt.Errorf("Volume fixture credential binding is missing")
	}
	oss, err := json.Marshal(map[string]any{"bucketName": journal.VolumeFixtureBucket, "bucketPath": "/", "endpoint": "https://oss-cn-hangzhou.aliyuncs.com", "readOnly": true})
	if err != nil {
		return err
	}
	mount, err := json.Marshal(map[string]any{"role": "acs:ram::" + journal.VolumeFixtureBinding.Identity.AccountID + ":role/" + journal.VolumeFixtureBucket + "-role"})
	if err != nil {
		return err
	}
	return validateOwnedVolumeBacking(journal, string(oss), "", "", string(mount))
}

func readVolumeReconcileJournal(path string, opt VolumeReconcileOptions, accepted *journalfile.RecoverySnapshot) (report.CleanupJournal, error) {
	snapshot, err := journalfile.ReadPrivateRecoverySnapshot(path)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	hash := sha256.Sum256(snapshot.Body)
	digest := hex.EncodeToString(hash[:])
	if digest != opt.JournalSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume reconciliation journal digest changed")
	}
	journal, legacy, err := decodeRunnerJournal(path, snapshot.Body, true)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	if err := validateVolumeReconcileJournal(path, opt, journal, legacy); err != nil {
		return report.CleanupJournal{}, err
	}
	if accepted != nil {
		*accepted = snapshot
	}
	return journal, nil
}

func readVolumeReconcileJournalRoot(root *os.Root, path string, opt VolumeReconcileOptions) (report.CleanupJournal, error) {
	body, err := readVolumeReconcileRootFile(root, filepath.Base(path), path)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	hash := sha256.Sum256(body)
	if hex.EncodeToString(hash[:]) != opt.JournalSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Volume reconciliation journal digest changed")
	}
	journal, legacy, err := decodeRunnerJournal(path, body, true)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	if err := validateVolumeReconcileJournal(path, opt, journal, legacy); err != nil {
		return report.CleanupJournal{}, err
	}
	return journal, nil
}

func validateVolumeReconcileJournal(path string, opt VolumeReconcileOptions, journal report.CleanupJournal, legacy bool) error {
	if legacy || journal.Version != 2 || journal.JournalPath != path || journal.NativeJournalPath != path || journal.RunID != opt.RunID || journal.RegionRole != "primary" || journal.ExecutionID == "" || journal.Surface != opt.Surface || journal.Region != opt.Config.Region || !filepath.IsAbs(journal.EcctlBin) || journal.EcctlBinSHA256 == "" || journal.EcctlBinSHA256 != opt.JournalBinSHA256 {
		return fmt.Errorf("Volume reconciliation journal binding changed")
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return err
	}
	if err := validateRetainedVolumeFixture(journal); err != nil {
		return err
	}
	return nil
}

func verifyArchivedVolumeBinarySurface(ctx context.Context, cfg execpkg.Config, originalPath, digest, expectedSurface string) error {
	if cfg.Bin == originalPath {
		return nil
	}
	bound := cfg
	bound.ExpectedBinSHA256 = digest
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	caps, err := surfacepkg.LoadFromConfig(probeCtx, bound)
	if err != nil {
		return fmt.Errorf("archived Volume reconciliation binary capabilities are unavailable: %w", err)
	}
	if caps.Surface != expectedSurface {
		return fmt.Errorf("archived Volume reconciliation binary surface %q differs from journal surface %q", caps.Surface, expectedSurface)
	}
	return nil
}

func readVolumeReconcileIntent(path string, journal report.CleanupJournal, opt VolumeReconcileOptions, selected *[]byte, roots ...*os.Root) (journalfile.TemplateCreateIntent, string, error) {
	intentPath := path + ".template-create-" + opt.Name[8:] + ".json"
	var body []byte
	var err error
	if len(roots) != 0 {
		body, err = readVolumeReconcileRootFile(roots[0], filepath.Base(intentPath), intentPath)
	} else {
		snapshot, readErr := journalfile.ReadPrivateRecoverySnapshot(intentPath)
		body, err = snapshot.Body, readErr
	}
	if err != nil {
		return journalfile.TemplateCreateIntent{}, "", err
	}
	return decodeVolumeReconcileIntent(path, journal, opt, body, selected)
}

func decodeVolumeReconcileIntent(path string, journal report.CleanupJournal, opt VolumeReconcileOptions, body []byte, selected *[]byte) (journalfile.TemplateCreateIntent, string, error) {
	intentPath := path + ".template-create-" + opt.Name[8:] + ".json"
	if _, err := execpkg.DecodeStrictJSON(body); err != nil {
		return journalfile.TemplateCreateIntent{}, "", fmt.Errorf("Volume reconciliation intent is ambiguous: %w", err)
	}
	var intent journalfile.TemplateCreateIntent
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return intent, "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return intent, "", fmt.Errorf("Volume reconciliation intent has trailing data")
	}
	if intent.Version != 2 || intent.Resource != "volume" || intent.Name != opt.Name || intent.RunID != journal.RunID || intent.ExecutionID != journal.ExecutionID || intent.RegionRole != journal.RegionRole || intent.Region != journal.Region || intent.Surface != journal.Surface || intent.EcctlBin != journal.EcctlBin || intent.Profile != opt.Profile || !safeTeamIdentity(intent.TeamID) || intent.CaseScope == "" || strings.ContainsAny(intent.CaseScope, "\r\n") || intent.CreatedAt.IsZero() || intent.Identity.Validate() != nil {
		return intent, "", fmt.Errorf("Volume reconciliation intent binding changed")
	}
	if intent.State != "pending" && intent.State != "settling" && intent.State != "journaled" {
		return intent, "", fmt.Errorf("Volume reconciliation intent is not recoverable")
	}
	if intent.NoAllocationCode != "" || intent.NoAllocationVerified || intent.AcceptedVolumeID != "" && (!safeTeamIdentity(intent.AcceptedVolumeID) || intent.Started == nil || !*intent.Started || intent.Exit == nil || !validTeamReconcileSHA256(intent.StdoutSHA256) || !validTeamReconcileSHA256(intent.StderrSHA256)) {
		return intent, "", fmt.Errorf("Volume reconciliation lacks accepted create evidence")
	}
	if intent.State != "pending" && intent.AcceptedVolumeID == "" {
		return intent, "", fmt.Errorf("Volume settlement lacks an invocation-linked accepted ID")
	}
	if intent.State == "settling" && (!intent.Recovered || !intent.VolumeObserved || !validTeamReconcileSHA256(intent.ReconcileSHA256)) {
		return intent, "", fmt.Errorf("Volume settling record lacks positive durable observation")
	}
	if intent.State != "pending" {
		if len(intent.Deletes) != 1 || intent.ValidateDelete(intent.Deletes[0]) != nil || intent.Recovered && !validTeamReconcileSHA256(intent.ReconcileSHA256) || !intent.Recovered && (intent.State != "journaled" || intent.ReconcileSHA256 != "") {
			return intent, "", fmt.Errorf("Volume reconciliation settlement evidence is invalid")
		}
	}
	if intent.State != "pending" && volumeReconcileID(intent.Deletes[0]) != intent.AcceptedVolumeID {
		return intent, "", fmt.Errorf("Volume settlement target differs from accepted create ID")
	}
	if intent.State == "pending" && (intent.Recovered || intent.ReconcileSHA256 != "" || len(intent.Deletes) != 0) {
		return intent, "", fmt.Errorf("Volume pending intent carries unexpected settlement evidence")
	}
	if intent.Started != nil && !*intent.Started {
		return intent, "", fmt.Errorf("Volume create did not start")
	}
	if err := intent.ValidateConfig(opt.Config.Env); err != nil {
		return intent, "", err
	}
	for _, name := range journalfile.TemplateSelectorNames {
		if value, ok := intent.Selectors[name]; !ok || value != journalfile.TemplateEnvironmentValue(name, opt.Config.Env) {
			return intent, "", fmt.Errorf("Volume reconciliation config selector changed: %s", name)
		}
	}
	count := 0
	for _, reference := range journal.NativeIntentPaths {
		if reference == intentPath {
			count++
		}
	}
	if count != 1 || journalfile.CheckVolumeTeamCreatorSource(journal, intent.TeamID, intent) != nil {
		return intent, "", fmt.Errorf("Volume reconciliation lacks one retained Team and sidecar binding")
	}
	if intent.State == "journaled" {
		changed, err := volumeReconcileFinalizer(&journal, intent, intent.Deletes[0], intent.CaseScope)
		if err != nil || changed {
			return intent, "", fmt.Errorf("journaled Volume lacks its exact durable finalizer: %v", err)
		}
	}
	if selected != nil {
		*selected = bytes.Clone(body)
	}
	return intent, intentPath, nil
}

func readVolumeReconcileRootFile(root *os.Root, name, path string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	body, private, readErr := journalfile.ReadRecoveryFileRoot(root, file, name)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	if !private {
		return nil, fmt.Errorf("Volume reconciliation file is not private")
	}
	return body, nil
}

func volumeReconcileIntentBytes(intent journalfile.TemplateCreateIntent) ([]byte, error) {
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

var writeVolumeReconcileRootBytes = func(root *os.Root, path string, body []byte) error {
	name := filepath.Base(path)
	writeErr := journalfile.WriteDurableRoot(root, name, body)
	installed, readErr := readVolumeReconcileRootFile(root, name, path)
	if readErr != nil || !bytes.Equal(installed, body) {
		return errors.Join(writeErr, fmt.Errorf("Volume reconciliation replacement was not installed exactly: %v", readErr))
	}
	if err := journalfile.ReaffirmDurableRoot(root, name, body); err != nil {
		return errors.Join(writeErr, fmt.Errorf("Volume reconciliation replacement durability is uncertain: %w", err))
	}
	return nil
}

func anchorVolumeReconcileJournal(journal *report.CleanupJournal, predecessor []byte) {
	if journal.OriginalJournalSHA256 == "" {
		digest := sha256.Sum256(predecessor)
		journal.OriginalJournalSHA256 = hex.EncodeToString(digest[:])
	}
}

func volumeReconcileParent(journal report.CleanupJournal, teamID string) (report.Resource, error) {
	var parent report.Resource
	count := 0
	for _, entry := range journal.Entries {
		if !entry.TeamCreateConfirmed {
			continue
		}
		id, err := journalfile.TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return parent, err
		}
		if id != teamID {
			continue
		}
		if !entry.VolumeTeamBound || entry.TeamReplayAttempted || entry.RegionRole != journal.RegionRole || entry.Region != journal.Region || entry.ExecutionID != journal.ExecutionID {
			return parent, fmt.Errorf("Volume reconciliation Team parent changed")
		}
		parent = entry
		count++
	}
	if count != 1 {
		return parent, fmt.Errorf("Volume reconciliation Team parent is missing or ambiguous")
	}
	return parent, nil
}

func probePendingVolume(ctx context.Context, cfg execpkg.Config, journal report.CleanupJournal, intent journalfile.TemplateCreateIntent) (string, string, error) {
	listCommand := quotedTemplateCommand([]string{"ecctl", "sandbox", "volume", "list", "--team-id", intent.TeamID, "--name", intent.Name, "--region", intent.Region, "--profile", intent.Profile, "--limit", "100", "--all", "--output=json", "--agent-envelope=false", "--lang=en"})
	list := execpkg.Run(ctx, cfg, listCommand)
	if list.Err != nil || list.Exit != 0 {
		return "", "", fmt.Errorf("Volume reconciliation list did not confirm a target")
	}
	id, err := volumeReconcileListID(list.JSON, intent)
	if err != nil {
		return "", "", err
	}
	getCommand := quotedTemplateCommand([]string{"ecctl", "sandbox", "volume", "get", id, "--team-id", intent.TeamID, "--region", intent.Region, "--profile", intent.Profile, "--output=json", "--agent-envelope=false", "--lang=en"})
	get := execpkg.Run(ctx, cfg, getCommand)
	if get.Err != nil || get.Exit != 0 {
		return "", "", fmt.Errorf("Volume reconciliation get did not confirm the exact target")
	}
	if err := volumeReconcileGetProof(get.JSON, id, journal, intent); err != nil {
		return "", "", err
	}
	digest := sha256.Sum256([]byte(list.Stdout + "\n" + get.Stdout))
	return id, hex.EncodeToString(digest[:]), nil
}

func volumeReconcileListID(raw any, intent journalfile.TemplateCreateIntent) (string, error) {
	root, _ := raw.(map[string]any)
	items, ok := root["volumes"].([]any)
	pagination, _ := root["pagination"].(map[string]any)
	if !ok || pagination["has_more"] != false {
		return "", fmt.Errorf("Volume reconciliation list is incomplete")
	}
	var id string
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("Volume reconciliation list contains an invalid item")
		}
		if item["name"] != intent.Name {
			continue
		}
		candidate, _ := item["id"].(string)
		if item["team_id"] != intent.TeamID || !safeTeamIdentity(candidate) || id != "" {
			return "", fmt.Errorf("Volume nonce matched an ambiguous or foreign target")
		}
		id = candidate
	}
	if id == "" {
		return "", fmt.Errorf("Volume nonce has zero confirmed matches")
	}
	return id, nil
}

func volumeReconcileGetProof(raw any, id string, journal report.CleanupJournal, intent journalfile.TemplateCreateIntent) error {
	getRoot, _ := raw.(map[string]any)
	volume, ok := getRoot["volume"].(map[string]any)
	if !ok || volume["id"] != id || volume["team_id"] != intent.TeamID || volume["name"] != intent.Name {
		return fmt.Errorf("Volume reconciliation get changed exact identity")
	}
	oss, okOSS := volume["oss_volume_config"].(map[string]any)
	mount, okMount := volume["mount_config"].(map[string]any)
	if !okOSS || !okMount {
		return fmt.Errorf("Volume reconciliation backing is missing")
	}
	ossJSON, err := json.Marshal(oss)
	if err != nil {
		return err
	}
	mountJSON, err := json.Marshal(mount)
	if err != nil {
		return err
	}
	alternate := func(field string) (string, error) {
		value, present := volume[field]
		if !present || value == nil {
			return "", nil
		}
		config, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("Volume reconciliation %s backing is malformed", field)
		}
		if len(config) == 0 {
			return "", nil
		}
		encoded, err := json.Marshal(config)
		return string(encoded), err
	}
	fs, err := alternate("agentic_fs_volume_config")
	if err != nil {
		return err
	}
	bucket, err := alternate("agentic_bucket_volume_config")
	if err != nil {
		return err
	}
	return validateOwnedVolumeBacking(journal, string(ossJSON), fs, bucket, string(mountJSON))
}

func volumeReconcileFinalizer(journal *report.CleanupJournal, intent journalfile.TemplateCreateIntent, command, scope string) (bool, error) {
	if journal == nil || intent.ValidateDelete(command) != nil {
		return false, fmt.Errorf("Volume reconciliation finalizer changed")
	}
	id, err := journalfile.ParseVolumeDeleteRequest(command)
	if err != nil {
		return false, err
	}
	if intent.AcceptedVolumeID == "" || id.ID != intent.AcceptedVolumeID {
		return false, fmt.Errorf("Volume finalizer lacks the accepted create ID")
	}
	count := 0
	for _, entry := range journal.Entries {
		if !journalfile.IsNativeVolumeDelete(entry.Teardown) {
			continue
		}
		candidate, err := journalfile.ParseVolumeDeleteRequest(entry.Teardown)
		if err != nil {
			return false, err
		}
		if candidate.ID != id.ID {
			continue
		}
		count++
		if entry.Teardown != command || entry.Scope != scope || entry.RegionRole != journal.RegionRole || entry.Region != journal.Region || entry.ExecutionID != journal.ExecutionID || entry.VolumeDeleteAttempted {
			return false, fmt.Errorf("Volume finalizer collided with a different creation")
		}
	}
	if count > 1 {
		return false, fmt.Errorf("Volume finalizer is ambiguous")
	}
	if count == 1 {
		return false, nil
	}
	journal.Entries = append(journal.Entries, report.Resource{Scope: scope, Teardown: command, RegionRole: journal.RegionRole, Region: journal.Region, ExecutionID: journal.ExecutionID})
	return true, nil
}

func checkVolumeReconcileIDCollision(journal report.CleanupJournal, intentPath, id string, roots ...*os.Root) error {
	for _, otherPath := range journal.NativeIntentPaths {
		if otherPath == intentPath {
			continue
		}
		if filepath.Dir(otherPath) != filepath.Dir(journal.JournalPath) || !strings.HasPrefix(filepath.Base(otherPath), filepath.Base(journal.JournalPath)+".template-create-") || !strings.HasSuffix(otherPath, ".json") {
			return fmt.Errorf("other native ownership sidecar path is outside the journal")
		}
		var body []byte
		var err error
		if len(roots) != 0 {
			body, err = readVolumeReconcileRootFile(roots[0], filepath.Base(otherPath), otherPath)
		} else {
			snapshot, readErr := journalfile.ReadPrivateRecoverySnapshot(otherPath)
			body, err = snapshot.Body, readErr
		}
		if err != nil {
			return fmt.Errorf("other native ownership sidecar is unavailable: %w", err)
		}
		if _, err := execpkg.DecodeStrictJSON(body); err != nil {
			return fmt.Errorf("other native ownership sidecar is ambiguous: %w", err)
		}
		var other journalfile.TemplateCreateIntent
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&other); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("other native ownership sidecar has trailing data")
		}
		if other.Resource != "volume" {
			continue
		}
		for _, command := range other.Deletes {
			request, err := journalfile.ParseVolumeDeleteRequest(command)
			if err != nil {
				return err
			}
			if request.ID == id {
				return fmt.Errorf("Volume ID is claimed by another native creation")
			}
		}
	}
	return nil
}

// ReconcilePendingVolume completes a two-file settlement: the sidecar first
// records a read-only proof while still fenced, the journal gains the exact
// finalizer, then the sidecar becomes replayable. Every stage is repeatable.
func ReconcilePendingVolume(ctx context.Context, path string, opt VolumeReconcileOptions) (result *VolumeReconcileResult, err error) {
	if opt.RunID == "" || (opt.Surface != "public" && opt.Surface != "full") || opt.Config.Region == "" || opt.Profile == "" || len(opt.Name) != 32 || !strings.HasPrefix(opt.Name, "ecctl-p-") || !filepath.IsAbs(opt.Config.Bin) || !filepath.IsAbs(opt.ProbeBin) || !validTeamReconcileSHA256(opt.JournalSHA256) || !validTeamReconcileSHA256(opt.JournalBinSHA256) || !validTeamReconcileSHA256(opt.ProbeBinSHA256) {
		return nil, fmt.Errorf("Volume reconciliation requires exact run, surface, region, profile, nonce and binary digests")
	}
	if _, err := hex.DecodeString(opt.Name[8:]); err != nil {
		return nil, fmt.Errorf("Volume reconciliation nonce is invalid")
	}
	resolved, err := journalfile.ResolveJournalPath(path)
	if err != nil {
		return nil, err
	}
	parent, err := journalParentIdentity(resolved)
	if err != nil || !parent.IsDir() {
		return nil, fmt.Errorf("Volume reconciliation journal parent is unavailable: %v", err)
	}
	release, err := journalfile.AcquireExistingRunLease(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()
	checkParent := func() error {
		current, statErr := journalParentIdentity(resolved)
		if statErr != nil || !os.SameFile(parent, current) {
			return fmt.Errorf("Volume reconciliation journal parent changed")
		}
		return nil
	}
	err = journalfile.WithExistingReplayLock(ctx, resolved, func() error {
		if err := checkParent(); err != nil {
			return err
		}
		for _, pair := range [][2]string{{opt.Config.Bin, opt.JournalBinSHA256}, {opt.ProbeBin, opt.ProbeBinSHA256}} {
			digest, err := journalfile.ExecutableSHA256(pair[0])
			if err != nil {
				return err
			}
			if digest != pair[1] {
				return fmt.Errorf("Volume reconciliation binary digest changed")
			}
		}
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			root, err := os.OpenRoot(filepath.Dir(resolved))
			if err != nil {
				return err
			}
			defer root.Close()
			rootInfo, err := root.Stat(".")
			if err != nil || !os.SameFile(parent, rootInfo) {
				return fmt.Errorf("Volume reconciliation journal parent changed")
			}
			body, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(body)
			if hex.EncodeToString(digest[:]) == opt.JournalSHA256 {
				prior, legacy, err := decodeRunnerJournal(resolved, body, true)
				if err != nil {
					return err
				}
				if err := validateVolumeReconcileJournal(resolved, opt, prior, legacy); err != nil {
					return err
				}
				if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, prior.EcctlBin, opt.JournalBinSHA256, prior.Surface); err != nil {
					return err
				}
				if _, _, err := readVolumeReconcileIntent(resolved, prior, opt, nil, root); err != nil {
					return err
				}
				validateSuccessor := func(nextBody, finalSidecar []byte) error {
					next, legacy, err := decodeRunnerJournal(resolved, nextBody, true)
					if err != nil {
						return err
					}
					if err := validateVolumeReconcileJournal(resolved, opt, next, legacy); err != nil {
						return err
					}
					if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, next.EcctlBin, opt.JournalBinSHA256, next.Surface); err != nil {
						return err
					}
					if len(finalSidecar) == 0 {
						return fmt.Errorf("Volume reconciliation successor lacks its exact final sidecar")
					}
					intent, _, err := decodeVolumeReconcileIntent(resolved, next, opt, finalSidecar, nil)
					if err != nil || intent.State != "journaled" || !intent.VolumeObserved || intent.AcceptedVolumeID == "" {
						return fmt.Errorf("Volume reconciliation successor lacks an exact final sidecar: %v", err)
					}
					return nil
				}
				matched, err := journalfile.ReaffirmCurrentVolumeReconcileTransition(root, resolved, opt.Name, body, validateSuccessor, "volume-register", "volume-observe", "volume-settle")
				if err != nil {
					return fmt.Errorf("Volume reconciliation installed transition is invalid: %w", err)
				}
				if !matched {
					matched, err = journalfile.InstallUntouchedVolumeObserveTransition(root, resolved, opt.Name, body, validateSuccessor)
					if err != nil {
						return fmt.Errorf("Volume reconciliation staged observation is invalid: %w", err)
					}
					if matched {
						body, err = readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
						if err != nil {
							return err
						}
					}
				}
				if matched {
					installed, legacy, err := decodeRunnerJournal(resolved, body, true)
					if err != nil {
						return err
					}
					if err := validateVolumeReconcileJournal(resolved, opt, installed, legacy); err != nil {
						return err
					}
					if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, installed.EcctlBin, opt.JournalBinSHA256, installed.Surface); err != nil {
						return err
					}
					intent, _, err := readVolumeReconcileIntent(resolved, installed, opt, nil, root)
					if err != nil || intent.State != "journaled" || !intent.VolumeObserved || intent.AcceptedVolumeID == "" {
						return fmt.Errorf("Volume reconciliation retry lacks an exact final sidecar: %v", err)
					}
					if err := journalfile.CheckTemplateCreateIntents(resolved, installed, opt.Config.Env); err != nil {
						return err
					}
					finalSHA := sha256.Sum256(body)
					result = &VolumeReconcileResult{RunID: installed.RunID, VolumeName: intent.Name, VolumeID: intent.AcceptedVolumeID, TeamID: intent.TeamID, JournalSHA256: hex.EncodeToString(finalSHA[:])}
				}
				return nil
			}
			installed, legacy, err := decodeRunnerJournal(resolved, body, true)
			if err != nil {
				return err
			}
			if err := validateVolumeReconcileJournal(resolved, opt, installed, legacy); err != nil {
				return err
			}
			predecessor, err := journalfile.ReadHistoricalJournalOriginal(root, resolved, opt.JournalSHA256)
			if err != nil {
				return err
			}
			prior, legacy, err := decodeRunnerJournal(resolved, predecessor, true)
			if err != nil {
				return err
			}
			if err := validateVolumeReconcileJournal(resolved, opt, prior, legacy); err != nil {
				return err
			}
			intentPath := resolved + ".template-create-" + opt.Name[8:] + ".json"
			intentBody, err := readVolumeReconcileRootFile(root, filepath.Base(intentPath), intentPath)
			if err != nil {
				return err
			}
			if _, err := execpkg.DecodeStrictJSON(intentBody); err != nil {
				return err
			}
			var stagedIntent journalfile.TemplateCreateIntent
			if err := json.Unmarshal(intentBody, &stagedIntent); err != nil {
				return err
			}
			binding := prior
			if stagedIntent.State == "journaled" {
				binding = installed
			}
			if _, _, err := readVolumeReconcileIntent(resolved, binding, opt, nil, root); err != nil {
				return err
			}
			if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, installed.EcctlBin, opt.JournalBinSHA256, installed.Surface); err != nil {
				return err
			}
			if err := journalfile.ReaffirmInstalledVolumeReconcileTransition(root, resolved, opt.JournalSHA256, opt.Name, body, "volume-register", "volume-observe", "volume-settle"); err != nil {
				return fmt.Errorf("Volume reconciliation journal digest changed: %w", err)
			}
			intent, _, err := readVolumeReconcileIntent(resolved, installed, opt, nil, root)
			if err != nil || intent.State != "journaled" || !intent.VolumeObserved || intent.AcceptedVolumeID == "" {
				return fmt.Errorf("Volume reconciliation retry lacks an exact final sidecar: %v", err)
			}
			if err := journalfile.CheckTemplateCreateIntents(resolved, installed, opt.Config.Env); err != nil {
				return err
			}
			if err := checkParent(); err != nil {
				return err
			}
			result = &VolumeReconcileResult{RunID: installed.RunID, VolumeName: intent.Name, VolumeID: intent.AcceptedVolumeID, TeamID: intent.TeamID, JournalSHA256: hex.EncodeToString(digest[:])}
			return nil
		}); err != nil {
			return err
		}
		if result != nil {
			return nil
		}
		var journal report.CleanupJournal
		var accepted journalfile.RecoverySnapshot
		var intent journalfile.TemplateCreateIntent
		var selectedIntentBody []byte
		var intentPath string
		var scope string
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			var err error
			journal, err = readVolumeReconcileJournal(resolved, opt, &accepted)
			if err != nil {
				return err
			}
			intent, intentPath, err = readVolumeReconcileIntent(resolved, journal, opt, &selectedIntentBody)
			if err != nil {
				return err
			}
			if _, err := volumeReconcileParent(journal, intent.TeamID); err != nil {
				return err
			}
			scope = intent.CaseScope
			return nil
		}); err != nil {
			return err
		}
		if err := verifyArchivedVolumeBinarySurface(ctx, opt.Config, journal.EcctlBin, opt.JournalBinSHA256, journal.Surface); err != nil {
			return err
		}
		if intent.State == "journaled" && intent.VolumeObserved {
			return journalfile.WithExistingParentLock(ctx, resolved, func() error {
				root, err := os.OpenRoot(filepath.Dir(resolved))
				if err != nil {
					return err
				}
				defer root.Close()
				rootInfo, err := root.Stat(".")
				if err != nil || !os.SameFile(parent, rootInfo) {
					return fmt.Errorf("Volume reconciliation journal parent changed")
				}
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				current, err := readVolumeReconcileJournalRoot(root, resolved, opt)
				if err != nil || !reflect.DeepEqual(current, journal) {
					return fmt.Errorf("journaled Volume changed before durability retry: %v", err)
				}
				currentIntent, currentPath, err := readVolumeReconcileIntent(resolved, current, opt, nil, root)
				if err != nil || currentPath != intentPath || !reflect.DeepEqual(currentIntent, intent) {
					return fmt.Errorf("journaled Volume sidecar changed before durability retry: %v", err)
				}
				currentBody, err := readVolumeReconcileRootFile(root, filepath.Base(intentPath), intentPath)
				if err != nil || !bytes.Equal(currentBody, selectedIntentBody) {
					return fmt.Errorf("journaled Volume sidecar bytes changed before durability retry: %v", err)
				}
				if err := checkVolumeReconcileIDCollision(current, intentPath, volumeReconcileID(intent.Deletes[0]), root); err != nil {
					return err
				}
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				for _, file := range []string{intentPath, resolved} {
					body, err := readVolumeReconcileRootFile(root, filepath.Base(file), file)
					if err != nil {
						return err
					}
					if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(file), body); err != nil {
						return err
					}
				}
				installed, err := readVolumeReconcileJournalRoot(root, resolved, opt)
				if err != nil || !reflect.DeepEqual(installed, current) {
					return fmt.Errorf("journaled Volume changed after durability retry: %v", err)
				}
				installedIntent, installedPath, err := readVolumeReconcileIntent(resolved, installed, opt, nil, root)
				if err != nil || installedPath != intentPath || !reflect.DeepEqual(installedIntent, currentIntent) {
					return fmt.Errorf("journaled Volume sidecar changed after durability retry: %v", err)
				}
				if err := journalfile.CheckTemplateCreateIntents(resolved, installed, opt.Config.Env); err != nil {
					return err
				}
				if err := checkParent(); err != nil {
					return err
				}
				result = &VolumeReconcileResult{RunID: installed.RunID, VolumeName: intent.Name, VolumeID: volumeReconcileID(intent.Deletes[0]), TeamID: intent.TeamID, JournalSHA256: opt.JournalSHA256}
				return nil
			})
		}
		var id, probeDigest string
		if intent.State == "settling" {
			// The accepted create ID and positive owned-scope observation were
			// already committed. Restore custody only; a later delete still
			// needs its normal fresh identity and scope checks.
			id, probeDigest = intent.AcceptedVolumeID, intent.ReconcileSHA256
		} else {
			probeCfg := execpkg.Config{Bin: opt.ProbeBin, ExpectedBinSHA256: opt.ProbeBinSHA256, Region: journal.Region, Env: opt.Config.Env}
			probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			bound, err := journalfile.ProbeQuotaBinding(probeCtx, journal.VolumeFixtureBinding, probeCfg)
			cancel()
			if err != nil {
				return err
			}
			bound = intent.BoundConfig(bound)
			probeCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
			_, err = execpkg.ReadIdentity(probeCtx, bound, intent.Profile)
			cancel()
			if err != nil {
				return err
			}
			probeCtx, cancel = context.WithTimeout(ctx, time.Minute)
			id, probeDigest, err = probePendingVolume(probeCtx, bound, journal, intent)
			cancel()
			if err != nil {
				return err
			}
		}
		if intent.AcceptedVolumeID == "" || id != intent.AcceptedVolumeID {
			return fmt.Errorf("Volume name lookup is read-only without this invocation's accepted ID")
		}
		command := quotedTemplateCommand([]string{"ecctl", "sandbox", "volume", "delete", id, "--region", intent.Region, "--profile", intent.Profile})
		if intent.State != "pending" && (len(intent.Deletes) != 1 || intent.Deletes[0] != command) {
			return fmt.Errorf("Volume reconciliation target changed during settlement")
		}
		if err := journalfile.WithExistingParentLock(ctx, resolved, func() error {
			root, err := os.OpenRoot(filepath.Dir(resolved))
			if err != nil {
				return err
			}
			defer root.Close()
			rootInfo, err := root.Stat(".")
			if err != nil || !os.SameFile(parent, rootInfo) {
				return fmt.Errorf("Volume reconciliation journal parent changed")
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
					return fmt.Errorf("Volume reconciliation binary changed after probe")
				}
			}
			current, err := readVolumeReconcileJournalRoot(root, resolved, opt)
			if err != nil {
				return err
			}
			currentIntent, currentPath, err := readVolumeReconcileIntent(resolved, current, opt, nil, root)
			if err != nil || currentPath != intentPath || !reflect.DeepEqual(currentIntent, intent) || !reflect.DeepEqual(current, journal) {
				return fmt.Errorf("Volume reconciliation ownership changed before settlement")
			}
			if err := checkVolumeReconcileIDCollision(current, intentPath, id, root); err != nil {
				return err
			}
			intentBefore, err := readVolumeReconcileRootFile(root, filepath.Base(intentPath), intentPath)
			if err != nil {
				return err
			}
			if !bytes.Equal(intentBefore, selectedIntentBody) {
				return fmt.Errorf("Volume reconciliation sidecar bytes changed during probe")
			}
			plannedIntent := currentIntent
			if plannedIntent.State == "pending" {
				plannedIntent.State = "settling"
				plannedIntent.Deletes = []string{command}
				plannedIntent.Recovered = true
				plannedIntent.ReconcileSHA256 = probeDigest
				plannedIntent.VolumeObserved = true
			}
			plannedJournal := current
			plannedJournal.Entries = append([]report.Resource(nil), current.Entries...)
			changed, err := volumeReconcileFinalizer(&plannedJournal, plannedIntent, command, scope)
			if err != nil {
				return err
			}
			if changed && plannedIntent.State == "journaled" {
				return fmt.Errorf("journaled Volume lacks its durable finalizer")
			}
			finalIntent := plannedIntent
			finalIntent.State = "journaled"
			finalIntent.VolumeObserved = true
			states := [][]byte{intentBefore}
			if plannedIntent.State == "settling" && currentIntent.State == "pending" {
				settlingBody, err := volumeReconcileIntentBytes(plannedIntent)
				if err != nil {
					return err
				}
				states = append(states, settlingBody)
			}
			finalIntentBody, err := volumeReconcileIntentBytes(finalIntent)
			if err != nil {
				return err
			}
			states = append(states, finalIntentBody)
			observe := !changed && currentIntent.State == "journaled" && !currentIntent.VolumeObserved
			// An exact finalizer may have been registered before the pending
			// sidecar was settled. Both its first transition and a retry from the
			// intermediate settling sidecar use the same staged registration edge.
			register := changed || currentIntent.State == "pending" || currentIntent.State == "settling"
			if observe || register && !changed {
				want := sha256.Sum256(finalIntentBody)
				digest := hex.EncodeToString(want[:])
				if existing, present := plannedJournal.VolumeObservationReceipts[finalIntent.Name]; present {
					if register || existing != digest {
						return fmt.Errorf("Volume observation receipt changed")
					}
					observe = false
				} else {
					if plannedJournal.VolumeObservationReceipts == nil {
						plannedJournal.VolumeObservationReceipts = make(map[string]string)
					}
					plannedJournal.VolumeObservationReceipts[finalIntent.Name] = digest
				}
			}
			if register || observe {
				if err := accepted.VerifyCurrent(resolved); err != nil {
					return err
				}
				anchorVolumeReconcileJournal(&plannedJournal, accepted.Body)
				body, err := runnerJournalBytes(plannedJournal)
				if err != nil {
					return err
				}
				kind := "volume-register"
				if observe {
					kind = "volume-observe"
				}
				states, err = journalfile.StageVolumeReconcileTransitionSelected(root, resolved, kind, plannedIntent.Name, accepted.Body, body, intentPath, states...)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(states[len(states)-1], &finalIntent); err != nil {
					return err
				}
				if currentIntent.State == "pending" {
					if len(states) != 3 {
						return fmt.Errorf("Volume reconciliation staged settlement skipped its intermediate sidecar")
					}
					if err := journalfile.WriteDurableRoot(root, filepath.Base(intentPath), states[1]); err != nil {
						return err
					}
				}
				if err := writeVolumeReconcileRootBytes(root, resolved, body); err != nil {
					return err
				}
				current = plannedJournal
			} else if err := accepted.VerifyCurrent(resolved); err != nil {
				return err
			}
			finalizerBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(resolved), finalizerBody); err != nil {
				return fmt.Errorf("Volume reconciliation finalizer durability is uncertain: %w", err)
			}
			installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
			if err != nil || legacy || !reflect.DeepEqual(installed, current) {
				return fmt.Errorf("Volume reconciliation finalizer was not installed exactly")
			}
			if err := journalfile.WriteDurableRoot(root, filepath.Base(intentPath), states[len(states)-1]); err != nil {
				return err
			}
			installedIntent, _, err := readVolumeReconcileIntent(resolved, current, opt, nil, root)
			if err != nil || !reflect.DeepEqual(installedIntent, finalIntent) {
				return fmt.Errorf("Volume reconciliation sidecar was not installed exactly")
			}
			if err := journalfile.CheckTemplateCreateIntents(resolved, current, opt.Config.Env); err != nil {
				return err
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
			result = &VolumeReconcileResult{RunID: current.RunID, VolumeName: finalIntent.Name, VolumeID: id, TeamID: finalIntent.TeamID, JournalSHA256: digest}
			return nil
		}); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func volumeReconcileID(command string) string {
	request, err := journalfile.ParseVolumeDeleteRequest(command)
	if err != nil {
		return ""
	}
	return request.ID
}
