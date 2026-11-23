package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

type VolumeFixtureReconcileOptions struct {
	RunID         string
	Surface       string
	Region        string
	Bucket        string
	Kind          string
	JournalSHA256 string
}

type VolumeFixtureReconcileResult struct {
	RunID              string `json:"run_id"`
	Bucket             string `json:"bucket"`
	Kind               string `json:"kind"`
	Settled            bool   `json:"settled"`
	ReservationRetired bool   `json:"reservation_retired,omitempty"`
}

// ReconcilePendingVolumeFixture completes only a role or policy allocation
// whose exact successful invocation receipt was durably recorded before the
// process stopped. Bucket and attachment outcomes require a separate
// provider observation before their pending reservations can be settled.
func ReconcilePendingVolumeFixture(ctx context.Context, path string, opt VolumeFixtureReconcileOptions) (result *VolumeFixtureReconcileResult, err error) {
	if opt.RunID == "" || opt.Surface != "public" && opt.Surface != "full" || opt.Region != "cn-hangzhou" || opt.Bucket == "" || opt.Kind != "bucket" && opt.Kind != "role" && opt.Kind != "policy" && opt.Kind != "attachment" || !validTeamReconcileSHA256(opt.JournalSHA256) {
		return nil, fmt.Errorf("Volume fixture reconciliation requires exact run, surface, region, bucket, kind and journal digest")
	}
	resolved, err := journalfile.ResolveJournalPath(path)
	if err != nil {
		return nil, err
	}
	parent, err := journalParentIdentity(resolved)
	if err != nil || !parent.IsDir() {
		return nil, fmt.Errorf("Volume fixture reconciliation journal parent is unavailable: %v", err)
	}
	release, err := journalfile.AcquireExistingRunLease(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()
	err = journalfile.WithExistingReplayLock(ctx, resolved, func() error {
		return journalfile.WithExistingParentLock(ctx, resolved, func() error {
			root, err := os.OpenRoot(filepath.Dir(resolved))
			if err != nil {
				return err
			}
			defer root.Close()
			rootInfo, err := root.Stat(".")
			if err != nil || !os.SameFile(parent, rootInfo) {
				return fmt.Errorf("Volume fixture reconciliation journal parent changed")
			}
			body, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(body)
			journal, legacy, err := decodeRunnerJournal(resolved, body, true)
			if err != nil {
				return err
			}
			if legacy || journal.Version != 2 || journal.JournalPath != resolved || journal.RunID != opt.RunID || journal.Surface != opt.Surface || journal.Region != opt.Region || journal.RegionRole != "primary" || journal.ExecutionID == "" || !validTeamReconcileSHA256(journal.EcctlBinSHA256) || journal.VolumeFixtureBucket != opt.Bucket || journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding) != nil {
				return fmt.Errorf("Volume fixture reconciliation journal identity changed")
			}
			if hex.EncodeToString(digest[:]) != opt.JournalSHA256 {
				key := retirementReceiptKey("fixture", opt.Kind)
				retired, err := reaffirmVolumeRetirement(root, resolved, body, journal, key, opt.JournalSHA256, func(prior *report.CleanupJournal, receipt report.VolumeRetirementReceipt) error {
					entry := receipt.FixtureEntry
					if receipt.TeamName != "" || entry == nil || entry.VolumeFixtureKind != opt.Kind || !entry.VolumeFixturePending || entry.VolumeFixtureDispatchProtocol != 1 || entry.VolumeFixtureDispatched || entry.VolumeFixtureCreateRequestID != "" || entry.VolumeFixtureCreateResponseSHA256 != "" || entry.VolumeFixtureCreateValidated || entry.VolumeFixtureRoleID != "" || entry.VolumeFixturePolicyCreateDate != "" || entry.VolumeFixtureBucketCreationDate != "" || receipt.FixtureIndex < 0 || receipt.FixtureIndex > len(prior.Entries) || journalfile.ValidateVolumeFixtureTeardown(*entry, *prior) != nil {
						return fmt.Errorf("Volume fixture retirement receipt changed")
					}
					for _, candidate := range prior.Entries {
						if candidate.VolumeFixtureKind == opt.Kind {
							return fmt.Errorf("Volume fixture retirement target still exists")
						}
					}
					prior.Entries = append(prior.Entries, report.Resource{})
					copy(prior.Entries[receipt.FixtureIndex+1:], prior.Entries[receipt.FixtureIndex:])
					prior.Entries[receipt.FixtureIndex] = *entry
					return nil
				})
				if err != nil || !retired {
					if retryErr := journalfile.ReaffirmInstalledVolumeReconcileTransition(root, resolved, opt.JournalSHA256, opt.Kind, body, "fixture-settle"); retryErr != nil {
						return fmt.Errorf("Volume fixture reconciliation journal digest changed: %v", errors.Join(err, retryErr))
					}
					result = &VolumeFixtureReconcileResult{RunID: journal.RunID, Bucket: opt.Bucket, Kind: opt.Kind, Settled: true}
					return verifyVolumeFixtureReconcileParent(resolved, parent)
				}
				if err := verifyVolumeFixtureReconcileParent(resolved, parent); err != nil {
					return err
				}
				result = &VolumeFixtureReconcileResult{RunID: journal.RunID, Bucket: opt.Bucket, Kind: opt.Kind, Settled: true, ReservationRetired: true}
				return nil
			}
			index := -1
			for i, entry := range journal.Entries {
				if entry.VolumeFixtureKind != opt.Kind {
					continue
				}
				if index >= 0 || entry.Scope != "stack" || entry.ExecutionID != journal.ExecutionID || entry.VolumeFixtureDeleteAttempted || journalfile.ValidateVolumeFixtureTeardown(entry, journal) != nil {
					return fmt.Errorf("Volume fixture reconciliation receipt or target is ambiguous")
				}
				index = i
			}
			if index < 0 {
				return fmt.Errorf("Volume fixture reconciliation has no invocation-linked receipt")
			}
			entry := journal.Entries[index]
			if entry.VolumeFixturePending && entry.VolumeFixtureDispatchProtocol == 1 && !entry.VolumeFixtureDispatched && entry.VolumeFixtureCreateRequestID == "" && entry.VolumeFixtureCreateResponseSHA256 == "" && !entry.VolumeFixtureCreateValidated && entry.VolumeFixtureRoleID == "" && entry.VolumeFixturePolicyCreateDate == "" && entry.VolumeFixtureBucketCreationDate == "" {
				canonical, err := runnerJournalBytes(journal)
				if err != nil || !bytes.Equal(body, canonical) {
					return fmt.Errorf("Volume fixture retirement requires original canonical journal bytes: %v", err)
				}
				key := retirementReceiptKey("fixture", opt.Kind)
				if journal.VolumeRetirementReceipts == nil {
					journal.VolumeRetirementReceipts = make(map[string]report.VolumeRetirementReceipt)
				}
				if _, exists := journal.VolumeRetirementReceipts[key]; exists {
					return fmt.Errorf("Volume fixture retirement receipt already exists")
				}
				journal.VolumeRetirementReceipts[key] = report.VolumeRetirementReceipt{OriginalSHA256: opt.JournalSHA256, FixtureEntry: &entry, FixtureIndex: index}
				journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
				anchorVolumeReconcileJournal(&journal, body)
				desired, err := runnerJournalBytes(journal)
				if err != nil {
					return err
				}
				if err := journalfile.StageVolumeReconcileTransition(root, resolved, "fixture-retire", opt.Kind, body, desired, ""); err != nil {
					return err
				}
				if err := writeVolumeReconcileRootBytes(root, resolved, desired); err != nil {
					return err
				}
				installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
				if err != nil {
					return err
				}
				installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
				if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
					return fmt.Errorf("Volume fixture pre-dispatch retirement was not installed exactly")
				}
				if err := verifyVolumeFixtureReconcileParent(resolved, parent); err != nil {
					return err
				}
				result = &VolumeFixtureReconcileResult{RunID: journal.RunID, Bucket: opt.Bucket, Kind: opt.Kind, Settled: true, ReservationRetired: true}
				return nil
			}
			if opt.Kind != "role" && opt.Kind != "policy" || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureCreateRequestID == "" || entry.VolumeFixtureCreateResponseSHA256 == "" || !entry.VolumeFixtureCreateValidated {
				return fmt.Errorf("Volume fixture reconciliation lacks an invocation-linked receipt")
			}
			result = &VolumeFixtureReconcileResult{RunID: journal.RunID, Bucket: opt.Bucket, Kind: opt.Kind, Settled: !journal.Entries[index].VolumeFixturePending}
			if result.Settled {
				if err := journalfile.ReaffirmDurableRoot(root, filepath.Base(resolved), body); err != nil {
					return fmt.Errorf("Volume fixture settled receipt durability is uncertain: %w", err)
				}
				installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
				if err != nil {
					return err
				}
				installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
				if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
					return fmt.Errorf("Volume fixture settled receipt changed after durability barrier")
				}
				return verifyVolumeFixtureReconcileParent(resolved, parent)
			}
			journal.Entries[index].VolumeFixturePending = false
			anchorVolumeReconcileJournal(&journal, body)
			desired, err := runnerJournalBytes(journal)
			if err != nil {
				return err
			}
			if err := journalfile.StageVolumeReconcileTransition(root, resolved, "fixture-settle", opt.Kind, body, desired, ""); err != nil {
				return err
			}
			if err := writeVolumeReconcileRootBytes(root, resolved, desired); err != nil {
				return err
			}
			installedBody, err := readVolumeReconcileRootFile(root, filepath.Base(resolved), resolved)
			if err != nil {
				return err
			}
			installed, legacy, err := decodeRunnerJournal(resolved, installedBody, true)
			if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
				return fmt.Errorf("Volume fixture receipt settlement was not installed exactly")
			}
			result.Settled = true
			return verifyVolumeFixtureReconcileParent(resolved, parent)
		})
	})
	return result, err
}

func verifyVolumeFixtureReconcileParent(path string, expected os.FileInfo) error {
	current, err := journalParentIdentity(path)
	if err != nil || !os.SameFile(expected, current) {
		return fmt.Errorf("Volume fixture reconciliation journal parent changed")
	}
	return nil
}
