package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
)

// persistOwnedVolumeObservation commits a positive pre-delete observation for a
// journaled Volume that has not yet been observed, before a fresh automatic
// cleanup delete may mark its attempt or send DeleteVolume. It reads the durable
// creation binding rather than the possibly stale in-memory copy, and advances
// VolumeObserved through the same supported volume-observe transition the
// reconciler uses, so replay and reconciliation keep a verifiable lineage. The
// caller holds the cleanup (shared replay) lock; this takes the parent journal
// transaction lock. Any persistence failure refuses the fresh delete and leaves
// the entry intact for controlled reconciliation or replay.
func (c *cleanup) persistOwnedVolumeObservation(item *cleanupItem) error {
	if item == nil || item.templateBinding == nil || item.templateBinding.Resource != "volume" || item.done {
		return fmt.Errorf("Volume observation lacks an owned finalizer")
	}
	meta, expected, path := c.journalValues(item)
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openRememberedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, beforeBody, err := readRunnerJournalRootSnapshot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != meta.Version || journal.JournalPath != path || journal.RunID != meta.RunID || journal.ExecutionID != meta.ExecutionID || journal.RegionRole != meta.RegionRole || journal.Region != meta.Region || journal.Surface != meta.Surface || journal.EcctlBin != meta.EcctlBin {
			return fmt.Errorf("Volume observation journal binding changed")
		}
		index := -1
		for i, entry := range journal.Entries {
			if entry.Teardown != expected.Teardown {
				continue
			}
			if entry != expected || index >= 0 {
				return fmt.Errorf("Volume observation entry changed or became ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume observation entry disappeared")
		}
		intent, err := journalfile.VolumeDeleteBinding(path, journal, c.execCfg[item.role].Env, item.cmd)
		if err != nil {
			return fmt.Errorf("Volume observation lacks a durable creation binding: %w", err)
		}
		if intent.VolumeObserved {
			return nil
		}
		if !strings.HasPrefix(intent.Name, "ecctl-p-") || len(intent.Name) != 32 {
			return fmt.Errorf("Volume observation owner name is invalid")
		}
		intentPath := path + ".template-create-" + strings.TrimPrefix(intent.Name, "ecctl-p-") + ".json"
		intentBefore, err := readVolumeReconcileRootFile(root, filepath.Base(intentPath), intentPath)
		if err != nil {
			return err
		}
		var durable journalfile.TemplateCreateIntent
		if err := execpkg.UnmarshalStrictJSON(intentBefore, &durable); err != nil || !reflect.DeepEqual(durable, intent) {
			return fmt.Errorf("Volume observation sidecar differs from its durable binding")
		}
		observed := intent
		observed.VolumeObserved = true
		intentAfter, err := volumeReconcileIntentBytes(observed)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(intentAfter)
		digest := hex.EncodeToString(sum[:])
		if receipt, present := journal.VolumeObservationReceipts[intent.Name]; present {
			if receipt != digest {
				return fmt.Errorf("Volume observation receipt changed")
			}
		} else {
			planned := journal
			anchorVolumeReconcileJournal(&planned, beforeBody)
			if planned.VolumeObservationReceipts == nil {
				planned.VolumeObservationReceipts = make(map[string]string)
			}
			planned.VolumeObservationReceipts[intent.Name] = digest
			body, err := runnerJournalBytes(planned)
			if err != nil {
				return err
			}
			states, err := journalfile.StageVolumeReconcileTransitionSelected(root, path, "volume-observe", intent.Name, beforeBody, body, intentPath, intentBefore, intentAfter)
			if err != nil {
				return err
			}
			intentAfter = states[len(states)-1]
			if err := writeVolumeReconcileRootBytes(root, path, body); err != nil {
				return err
			}
			installed, legacy, err := readRunnerJournalRoot(root, path)
			if err != nil || legacy || !reflect.DeepEqual(installed, planned) {
				return fmt.Errorf("Volume observation journal was not installed exactly")
			}
		}
		if err := journalfile.WriteDurableRoot(root, filepath.Base(intentPath), intentAfter); err != nil {
			return err
		}
		installedSidecar, err := readVolumeReconcileRootFile(root, filepath.Base(intentPath), intentPath)
		if err != nil || !bytes.Equal(installedSidecar, intentAfter) {
			return fmt.Errorf("Volume observation sidecar was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
}
