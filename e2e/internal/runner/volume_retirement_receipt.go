package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func retirementReceiptKey(kind, name string) string { return kind + ":" + name }

// An original-run digest authenticates the exact reverse transition. A
// receipt cannot authorize a different reservation or a later journal edit.
func reaffirmVolumeRetirement(root *os.Root, path string, body []byte, current report.CleanupJournal, key, originalSHA256 string, restore func(*report.CleanupJournal, report.VolumeRetirementReceipt) error) (bool, error) {
	receipt, exists := current.VolumeRetirementReceipts[key]
	if !exists {
		return false, nil
	}
	if !validTeamReconcileSHA256(originalSHA256) || receipt.OriginalSHA256 != originalSHA256 || current.JournalPath != path {
		return false, fmt.Errorf("Volume retirement original journal digest changed")
	}
	copyBody, err := runnerJournalBytes(current)
	if err != nil {
		return false, err
	}
	var prior report.CleanupJournal
	if err := json.Unmarshal(copyBody, &prior); err != nil {
		return false, err
	}
	delete(prior.VolumeRetirementReceipts, key)
	if len(prior.VolumeRetirementReceipts) == 0 {
		prior.VolumeRetirementReceipts = nil
	}
	if err := restore(&prior, receipt); err != nil {
		return false, err
	}
	if current.OriginalJournalSHA256 == originalSHA256 {
		prior.OriginalJournalSHA256 = ""
	}
	priorBody, err := runnerJournalBytes(prior)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(priorBody)
	if hex.EncodeToString(digest[:]) != originalSHA256 {
		return false, fmt.Errorf("Volume retirement does not reconstruct the original journal")
	}
	retirementKind, target, ok := strings.Cut(key, ":")
	if !ok || retirementKind != "team" && retirementKind != "fixture" {
		return false, fmt.Errorf("Volume retirement kind changed")
	}
	if err := journalfile.ReaffirmInstalledVolumeReconcileTransition(root, path, originalSHA256, target, body, retirementKind+"-retire"); err != nil {
		return false, err
	}
	name := filepath.Base(path)
	if err := journalfile.ReaffirmDurableRoot(root, name, body); err != nil {
		return false, fmt.Errorf("Volume retirement durability is uncertain: %w", err)
	}
	installed, err := readVolumeReconcileRootFile(root, name, path)
	if err != nil || !bytes.Equal(installed, body) {
		return false, fmt.Errorf("Volume retirement changed after durability barrier: %v", err)
	}
	return true, nil
}
