package journalfile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// volumeTeamReceipt is the private, accepted create record. The nonce ledger
// alone cannot prove that the confirmed Team came from this execution.
type volumeTeamReceipt struct {
	Version          int                            `json:"version"`
	RunID            string                         `json:"run_id"`
	ExecutionID      string                         `json:"execution_id"`
	Region           string                         `json:"region"`
	Surface          string                         `json:"surface"`
	EcctlBin         string                         `json:"ecctl_bin"`
	Name             string                         `json:"name"`
	Description      string                         `json:"description"`
	Input            []string                       `json:"input"`
	CaseScope        string                         `json:"case_scope,omitempty"`
	Teardown         string                         `json:"teardown,omitempty"`
	AcceptedID       string                         `json:"accepted_id,omitempty"`
	ProbeBinSHA256   string                         `json:"probe_bin_sha256,omitempty"`
	Dispatched       bool                           `json:"dispatched,omitempty"`
	DispatchProtocol int                            `json:"dispatch_protocol,omitempty"`
	QuotaBinding     *report.QuotaCredentialBinding `json:"quota_binding,omitempty"`
	VolumeBinding    *report.QuotaCredentialBinding `json:"volume_binding,omitempty"`
	CreatedAt        time.Time                      `json:"created_at"`
}

// CheckVolumeTeamCreationLink requires one settled nonce and its private
// accepted create record before a Volume Team can authorize child work or
// deletion. A retired Team keeps the same link for terminal receipt checks.
func CheckVolumeTeamCreationLink(meta report.CleanupJournal, teamID string, active *report.Resource) error {
	if !safeTemplateID(teamID) || !filepath.IsAbs(meta.JournalPath) || filepath.Clean(meta.JournalPath) != meta.JournalPath {
		return fmt.Errorf("Volume Team creation link lacks a canonical journal or Team ID")
	}
	name := ""
	for nonce, id := range meta.VolumeTeamCreateIDs {
		if id != teamID {
			continue
		}
		if name != "" || len(nonce) != 32 || !strings.HasPrefix(nonce, "ecctl-t-") || !safeTemplateID(nonce) {
			return fmt.Errorf("Volume Team creation link has ambiguous nonce ownership")
		}
		if _, err := hex.DecodeString(nonce[8:]); err != nil {
			return fmt.Errorf("Volume Team creation link has invalid nonce")
		}
		name = nonce
	}
	if name == "" {
		return fmt.Errorf("Volume Team creation link lacks a settled nonce")
	}
	path := meta.JournalPath + ".team-create-" + name[8:] + ".json"
	snapshot, err := ReadPrivateRecoverySnapshot(path)
	if err != nil {
		return fmt.Errorf("Volume Team accepted create record is unavailable: %w", err)
	}
	var receipt volumeTeamReceipt
	if _, err := execpkg.DecodeStrictJSON(snapshot.Body); err != nil {
		return fmt.Errorf("Volume Team accepted create record is ambiguous: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return fmt.Errorf("Volume Team accepted create record is invalid: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("Volume Team accepted create record has trailing data")
	}
	if receipt.Version != 1 || receipt.CreatedAt.IsZero() || !receipt.Dispatched || receipt.DispatchProtocol != 1 || receipt.AcceptedID != teamID || receipt.Name != name || receipt.Description != "ecctl E2E Team owner="+name[8:] || receipt.RunID != meta.RunID || receipt.ExecutionID != meta.ExecutionID || receipt.Region != meta.Region || receipt.Surface != meta.Surface || receipt.EcctlBin != meta.EcctlBin || receipt.CaseScope == "" || strings.ContainsAny(receipt.CaseScope, "\r\n") || receipt.VolumeBinding == nil || !reflect.DeepEqual(receipt.VolumeBinding, meta.VolumeFixtureBinding) || !reflect.DeepEqual(receipt.VolumeBinding, meta.VolumeTeamCreators[teamID]) || !reflect.DeepEqual(receipt.QuotaBinding, meta.QuotaBinding) || receipt.ProbeBinSHA256 != "" && !validHistoricalDigest(receipt.ProbeBinSHA256) {
		return fmt.Errorf("Volume Team accepted create record differs from journal ownership")
	}
	if len(receipt.Input) == 0 || receipt.Input[0] != "ecctl" || strings.Count(receipt.Teardown, "intent-validation") != 1 {
		return fmt.Errorf("Volume Team accepted create command is invalid")
	}
	command := strings.Replace(receipt.Teardown, "intent-validation", teamID, 1)
	if target, err := TeamDeleteTarget(command); err != nil || target != teamID {
		return fmt.Errorf("Volume Team accepted delete target changed: %v", err)
	}
	if active != nil && (active.Teardown != command || active.Scope != receipt.CaseScope || active.ExecutionID != receipt.ExecutionID || active.RegionRole != meta.RegionRole || active.Region != receipt.Region || !active.TeamCreateConfirmed || !active.VolumeTeamBound || active.QuotaBound != (receipt.QuotaBinding != nil)) {
		return fmt.Errorf("Volume Team finalizer differs from accepted creation")
	}
	return nil
}
