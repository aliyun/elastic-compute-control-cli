package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

var writeVolumeFixtureRoleIdentityJournal = writeRunnerJournalRoot
var writeVolumeFixturePolicyIdentityJournal = writeRunnerJournalRoot
var writeVolumeFixtureBucketEvidenceJournal = writeRunnerJournalRoot
var writeVolumeFixtureCreateReceiptJournal = writeRunnerJournalRoot
var writeVolumeFixtureDeleteAttemptJournal = writeRunnerJournalRoot

func volumeFixtureKind(id string) string {
	switch id {
	case "volume_oss_bucket":
		return "bucket"
	case "volume_ram_role":
		return "role"
	case "volume_ram_policy":
		return "policy"
	case "volume_ram_attachment":
		return "attachment"
	default:
		return ""
	}
}

func validateVolumeFixtureBucketLookup(step ProvisionStep, command, bucket string) error {
	if step.Mode != "lookup" || step.Teardown != "" {
		return fmt.Errorf("Volume fixture bucket lookup changed")
	}
	return journalfile.ValidateVolumeFixtureOSSCommand("GetBucketInfo", command, bucket)
}

func (c *cleanup) boundVolumeFixtureConfig(ctx context.Context, cfg execpkg.Config, bucket string, timeout time.Duration) (execpkg.Config, error) {
	if c.journal == "" || cfg.Region != "cn-hangzhou" {
		return cfg, fmt.Errorf("Volume fixtures require a durable cleanup journal in cn-hangzhou")
	}
	c.mu.Lock()
	binding := c.volumeFixtureBinding
	if c.volumeFixtureBucket != "" && c.volumeFixtureBucket != bucket {
		c.mu.Unlock()
		return cfg, fmt.Errorf("Volume fixture bucket changed")
	}
	c.mu.Unlock()
	if binding == nil {
		captured, bound, err := captureQuotaCredentialBinding(ctx, cfg, timeout)
		if err != nil {
			return cfg, err
		}
		if err := journalfile.ValidateVolumeFixtureIdentity(captured); err != nil {
			return cfg, err
		}
		c.mu.Lock()
		if c.volumeFixtureBinding != nil && !reflect.DeepEqual(c.volumeFixtureBinding, captured) {
			c.mu.Unlock()
			return cfg, fmt.Errorf("Volume fixture credential binding changed")
		}
		c.volumeFixtureBinding = captured
		c.volumeFixtureBucket = bucket
		c.mu.Unlock()
		return bound, nil
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(binding); err != nil {
		return cfg, err
	}
	return journalfile.ProbeQuotaBinding(ctx, binding, cfg)
}

// A failed fixture step may have captured an identity without installing its
// reservation. Keep only bindings that a durable journal has already claimed.
func (c *cleanup) discardUnreservedVolumeFixtureBinding() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.volumeFixtureBinding == nil {
		return
	}
	path, err := c.checkedJournalPath("primary")
	if err != nil {
		return
	}
	journal, legacy, err := readRunnerJournal(path)
	if err == nil {
		if legacy || journal.Version != 2 || journal.RunID != c.journalMeta.RunID || journal.ExecutionID != c.journalMeta.ExecutionID || journal.VolumeFixtureBinding != nil {
			return
		}
	} else if !os.IsNotExist(err) {
		return
	}
	c.volumeFixtureBinding = nil
	c.volumeFixtureBucket = ""
}

func (c *cleanup) reserveVolumeFixture(scope *[]*cleanupItem, kind, createCommand, teardownCommand string) (*cleanupItem, error) {
	c.mu.Lock()
	if c.journal == "" || c.volumeFixtureBinding == nil || c.volumeFixtureBucket == "" {
		c.mu.Unlock()
		return nil, fmt.Errorf("Volume fixture ownership is not pinned")
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(c.volumeFixtureBinding); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	pending := &cleanupItem{scope: "stack", cmd: teardownCommand, role: "primary", volumeFixtureKind: kind, volumeFixturePending: true, volumeFixtureDispatchProtocol: 1}
	if journalfile.VolumeFixtureFencedKind(kind) {
		pending.volumeFixtureDeleteProtocol = 1
	}
	journal, entry, _ := c.journalValues(pending)
	c.mu.Unlock()
	if err := journalfile.ValidateVolumeFixtureCreate(kind, createCommand, journal); err != nil {
		return nil, err
	}
	if err := journalfile.ValidateVolumeFixtureTeardown(entry, journal); err != nil {
		return nil, err
	}
	return c.pushItemWithFixture(scope, "stack", teardownCommand, "primary", nil, false, false, false, nil, kind, nil)
}

// The pending entry and its remembered parent must still be the ones reserved
// before a fixture create reaches the provider or is marked settled.
func (c *cleanup) reservedVolumeFixtureJournal(path string, expected report.CleanupJournal, entry report.Resource, roots ...*os.Root) (report.CleanupJournal, int, error) {
	if c.journalParents[path] == nil {
		return report.CleanupJournal{}, -1, fmt.Errorf("Volume fixture journal parent was not remembered")
	}
	if _, err := c.checkedJournalParentLocked(path); err != nil {
		return report.CleanupJournal{}, -1, err
	}
	var journal report.CleanupJournal
	var legacy bool
	var err error
	if len(roots) != 0 {
		journal, legacy, err = readRunnerJournalRoot(roots[0], path)
	} else {
		journal, legacy, err = readRunnerJournal(path)
	}
	if err != nil {
		return report.CleanupJournal{}, -1, err
	}
	if legacy || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
		return report.CleanupJournal{}, -1, fmt.Errorf("Volume fixture reservation journal binding changed")
	}
	if err := journalfile.ValidateVolumeFixtureIdentity(journal.VolumeFixtureBinding); err != nil {
		return report.CleanupJournal{}, -1, err
	}
	index := -1
	for i, candidate := range journal.Entries {
		if candidate != entry {
			continue
		}
		if index >= 0 {
			return report.CleanupJournal{}, -1, fmt.Errorf("Volume fixture reservation is ambiguous")
		}
		index = i
	}
	if index < 0 {
		return report.CleanupJournal{}, -1, fmt.Errorf("Volume fixture reservation is missing")
	}
	return journal, index, nil
}

func (c *cleanup) checkReservedVolumeFixture(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind == "" || !item.volumeFixturePending || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no pending reservation")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		_, _, err := c.reservedVolumeFixtureJournal(path, expected, entry)
		return err
	})
}

func (c *cleanup) markVolumeFixtureDispatch(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind == "" || !item.volumeFixturePending || item.done || item.volumeFixtureDispatched {
		return fmt.Errorf("Volume fixture dispatch lacks a fresh reservation")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture dispatch journal changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, expected, entry, root)
		if err != nil {
			return err
		}
		original, err := runnerJournalBytes(journal)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		installed, err := root.ReadFile(name)
		if err != nil || !bytes.Equal(installed, original) {
			return fmt.Errorf("Volume fixture dispatch original journal changed: %v", err)
		}
		journal.Entries[index].VolumeFixtureDispatched = true
		marked, err := runnerJournalBytes(journal)
		if err != nil {
			return err
		}
		if writeErr := journalfile.WriteDurableRoot(root, name, marked); writeErr != nil {
			installed, readErr := root.ReadFile(name)
			if readErr == nil && bytes.Equal(installed, original) {
				return writeErr
			}
			if readErr != nil || !bytes.Equal(installed, marked) {
				return errors.Join(writeErr, fmt.Errorf("Volume fixture dispatch marker is uncertain: %v", readErr))
			}
			return errors.Join(writeErr, journalfile.RestoreUninvokedMarkerRoot(root, name, marked, original))
		}
		decoded, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(decoded, journal) {
			return fmt.Errorf("Volume fixture dispatch was not installed exactly")
		}
		return teamCompletionRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixtureDispatched = true
	}
	return err
}

func (c *cleanup) volumeFixtureAttachmentParents(item *cleanupItem) (report.CleanupJournal, report.Resource, report.Resource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind != "attachment" || !item.volumeFixturePending || item.done {
		return report.CleanupJournal{}, report.Resource{}, report.Resource{}, fmt.Errorf("Volume fixture attachment reservation is unavailable")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return report.CleanupJournal{}, report.Resource{}, report.Resource{}, err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return report.CleanupJournal{}, report.Resource{}, report.Resource{}, fmt.Errorf("Volume fixture attachment journal target changed")
	}
	var journal report.CleanupJournal
	var role, policy report.Resource
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		var err error
		journal, _, err = c.reservedVolumeFixtureJournal(path, expected, entry)
		if err != nil {
			return err
		}
		role, policy, err = journalfile.VolumeFixtureRAMParents(journal)
		return err
	})
	return journal, role, policy, err
}

func (c *cleanup) currentVolumeFixtureJournal(item *cleanupItem) (report.CleanupJournal, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind == "" || c.journal == "" {
		return report.CleanupJournal{}, fmt.Errorf("Volume fixture journal is unavailable")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return report.CleanupJournal{}, fmt.Errorf("Volume fixture journal target changed")
	}
	var journal report.CleanupJournal
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		var err error
		journal, _, err = c.reservedVolumeFixtureJournal(path, expected, entry)
		return err
	})
	return journal, err
}

// recordVolumeFixtureCreateReceipt persists the exact invocation response
// before capture processing or a second provider read can fail. The receipt
// identifies the original call; it does not authorize a name-based delete.
func (c *cleanup) recordVolumeFixtureCreateReceipt(item *cleanupItem, result execpkg.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || !item.volumeFixturePending || item.done || item.volumeFixtureCreateRequestID != "" || result.Err != nil || !result.Started || result.Exit != 0 || !json.Valid([]byte(result.Stdout)) {
		return fmt.Errorf("Volume fixture has no successful pending create invocation")
	}
	root, ok := result.JSON.(map[string]any)
	operation := map[string]string{"bucket": "PutBucket", "role": "CreateRole", "policy": "CreatePolicy", "attachment": "AttachPolicyToRole"}[item.volumeFixtureKind]
	product := "ram"
	if item.volumeFixtureKind == "bucket" {
		product = "oss"
	}
	response, responseOK := root["response"].(map[string]any)
	requestID, idOK := response["RequestId"].(string)
	if !ok || !responseOK || !idOK || operation == "" || root["product"] != product || root["operation"] != operation || root["region"] != c.execCfg[item.role].Region || root["error"] != nil || root["actions"] != nil || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil || requestID == "" || len(requestID) > 128 || strings.IndexFunc(requestID, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return fmt.Errorf("Volume fixture create lacks an exact provider response receipt")
	}
	bucketDate := ""
	if product == "ram" {
		if err := validVolumeFixtureRAMCreateOutcome(c.volumeFixtureBinding, c.volumeFixtureBucket, item.volumeFixtureKind, result); err != nil {
			return err
		}
	} else {
		bucket, bucketOK := response["Bucket"].(map[string]any)
		owner, ownerOK := bucket["Owner"].(map[string]any)
		bucketDate, _ = bucket["CreationDate"].(string)
		if bucketDate != "" && !journalfile.ValidVolumeFixtureCreationDate(bucketDate) {
			bucketDate = ""
		}
		if response["Message"] != nil || response["message"] != nil || response["Success"] != nil || response["success"] != nil ||
			!bucketOK || !ownerOK || bucket["Name"] != c.volumeFixtureBucket || bucket["Location"] != "oss-cn-hangzhou" ||
			bucket["ExtranetEndpoint"] != "oss-cn-hangzhou.aliyuncs.com" || owner["ID"] != c.volumeFixtureBinding.Identity.AccountID {
			return fmt.Errorf("Volume fixture create lacks the named bucket response")
		}
	}
	roleID, policyDate := "", ""
	if item.volumeFixtureKind == "role" {
		role, ok := response["Role"].(map[string]any)
		roleID, _ = role["RoleId"].(string)
		if strings.TrimSpace(roleID) != roleID {
			roleID = ""
		}
		wantARN := "acs:ram::" + c.volumeFixtureBinding.Identity.AccountID + ":role/" + c.volumeFixtureBucket + "-role"
		arn, _ := role["Arn"].(string)
		if !ok || role["RoleName"] != c.volumeFixtureBucket+"-role" || !strings.EqualFold(arn, wantARN) {
			return fmt.Errorf("Volume fixture role receipt lacks its assigned name")
		}
	}
	if item.volumeFixtureKind == "policy" {
		policy, ok := response["Policy"].(map[string]any)
		policyDate, _ = policy["CreateDate"].(string)
		if policyDate != "" && !journalfile.ValidVolumeFixtureCreationDate(policyDate) {
			policyDate = ""
		}
		if !ok || policy["PolicyName"] != c.volumeFixtureBucket+"-read" || policy["PolicyType"] != "Custom" {
			return fmt.Errorf("Volume fixture policy receipt lacks its assigned name")
		}
	}
	digest := sha256.Sum256([]byte(result.Stdout))
	responseSHA := hex.EncodeToString(digest[:])
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, template, entry, root)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixtureCreateRequestID = requestID
		journal.Entries[index].VolumeFixtureCreateResponseSHA256 = responseSHA
		journal.Entries[index].VolumeFixtureCreateValidated = true
		journal.Entries[index].VolumeFixtureRoleID = roleID
		journal.Entries[index].VolumeFixturePolicyCreateDate = policyDate
		journal.Entries[index].VolumeFixtureBucketCreationDate = bucketDate
		if err := writeVolumeFixtureCreateReceiptJournal(root, path, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture create receipt was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixtureCreateRequestID = requestID
		item.volumeFixtureCreateResponseSHA256 = responseSHA
		item.volumeFixtureCreateValidated = true
		item.volumeFixtureRoleID = roleID
		item.volumeFixturePolicyCreateDate = policyDate
		item.volumeFixtureBucketCreationDate = bucketDate
	}
	return err
}

func (c *cleanup) settleVolumeFixture(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind == "" || !item.volumeFixturePending || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no pending reservation")
	}
	if item.volumeFixtureKind != "role" && item.volumeFixtureRoleID != "" {
		return fmt.Errorf("Volume fixture creation ID belongs only to a role")
	}
	if (item.volumeFixtureKind != "policy" && item.volumeFixturePolicyCreateDate != "") ||
		item.volumeFixtureBucketCreationDate != "" && (item.volumeFixtureKind != "bucket" || !journalfile.ValidVolumeFixtureCreationDate(item.volumeFixtureBucketCreationDate)) {
		return fmt.Errorf("Volume fixture creation evidence belongs to a different fixture")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, entry, requested := c.journalValues(item)
	// The reservation was written before CreateRole returned its RoleId.
	if item.volumeFixtureCreateRequestID == "" {
		entry.VolumeFixtureRoleID = ""
		entry.VolumeFixturePolicyCreateDate = ""
	}
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, template, entry, root)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixturePending = false
		if item.volumeFixtureKind == "role" {
			journal.Entries[index].VolumeFixtureRoleID = item.volumeFixtureRoleID
			if err := writeVolumeFixtureRoleIdentityJournal(root, path, journal); err != nil {
				return err
			}
		}
		if item.volumeFixtureKind == "policy" {
			journal.Entries[index].VolumeFixturePolicyCreateDate = item.volumeFixturePolicyCreateDate
			if err := writeVolumeFixturePolicyIdentityJournal(root, path, journal); err != nil {
				return err
			}
		}
		if item.volumeFixtureKind != "role" && item.volumeFixtureKind != "policy" {
			if err := writeRunnerJournalRoot(root, path, journal); err != nil {
				return err
			}
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture settlement was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixturePending = false
	}
	return err
}

// Keep the server date when available for diagnostics. The dedicated test
// account identifies this fixture by its run-assigned bucket name.
func (c *cleanup) recordVolumeFixtureBucketEvidence(item *cleanupItem, creationDate string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind != "bucket" || item.volumeFixturePending || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture bucket reservation is unavailable")
	}
	if creationDate == "" {
		return nil
	}
	if !journalfile.ValidVolumeFixtureCreationDate(creationDate) {
		return fmt.Errorf("Volume fixture bucket date is malformed")
	}
	if item.volumeFixtureBucketCreationDate != "" {
		return nil
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture bucket journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, template, entry, root)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixtureBucketCreationDate = creationDate
		if err := writeVolumeFixtureBucketEvidenceJournal(root, path, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture bucket creation date was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixtureBucketCreationDate = creationDate
	}
	return err
}

// The first fixture teardown is the only destructive attempt.
// An uncertain write or a later retry leaves the entry for read-only absence.
func (c *cleanup) markVolumeFixtureDeleteAttempt(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || !journalfile.VolumeFixtureFencedKind(item.volumeFixtureKind) || item.volumeFixtureDeleteProtocol != 1 || item.volumeFixturePending || item.volumeFixtureDeleteAttempted || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no fresh delete attempt")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
			return fmt.Errorf("Volume fixture journal binding changed")
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate != entry {
				continue
			}
			if index >= 0 {
				return fmt.Errorf("Volume fixture delete entry is ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture delete entry changed")
		}
		originalBody, err := root.ReadFile(filepath.Base(path))
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixtureDeleteAttempted = true
		body, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		markedBody := append(body, '\n')
		if err := writeVolumeFixtureDeleteAttemptJournal(root, path, journal); err != nil {
			if installed, readErr := root.ReadFile(filepath.Base(path)); readErr == nil && bytes.Equal(installed, markedBody) {
				return errors.Join(err, journalfile.RestoreUninvokedMarkerRoot(root, filepath.Base(path), markedBody, originalBody))
			}
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture delete attempt was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixtureDeleteAttempted = true
	}
	return err
}

// Only the caller that just installed this marker may restore it, and only
// after its own subprocess demonstrably failed to start. The journal remains
// fenced if the restoration write or its readback is uncertain.
func (c *cleanup) restoreUnstartedVolumeFixtureDeleteAttempt(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || !journalfile.VolumeFixtureFencedKind(item.volumeFixtureKind) || item.volumeFixtureDeleteProtocol != 1 || item.volumeFixturePending || !item.volumeFixtureDeleteAttempted || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no newly marked delete attempt")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, expected, entry, root)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixtureDeleteAttempted = false
		if err := writeVolumeFixtureRestoreJournal(root, path, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture unused delete marker was not restored exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.volumeFixtureDeleteAttempted = false
	}
	return err
}

// A trusted absence read can settle the exact fixture finalizer without
// entering the name-based mutation path or spending its one delete attempt.
func (c *cleanup) retireAbsentVolumeFixture(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || !journalfile.VolumeFixtureFencedKind(item.volumeFixtureKind) || item.volumeFixturePending || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no settled finalizer")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	expected, entry, requested := c.journalValues(item)
	if path != requested {
		return fmt.Errorf("Volume fixture journal target changed")
	}
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, index, err := c.reservedVolumeFixtureJournal(path, expected, entry, root)
		if err != nil {
			return err
		}
		if err := journalfile.VolumeFixtureCleanupReady(path, journal, entry, c.execCfg[item.role].Env); err != nil {
			return err
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		if err := writeVolumeFixtureConsumptionJournal(root, path, journal); err != nil {
			return err
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("absent Volume fixture finalizer was not retired exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err == nil {
		item.done = true
	}
	return err
}

// An unstarted subprocess or a verified preallocation provider rejection
// cannot have allocated its fixture. Retire only the exact pending entry;
// an uncertain journal write keeps it pending.
func (c *cleanup) retireUnstartedVolumeFixture(item *cleanupItem) error {
	return c.retireVolumeFixtureReservation(item, false)
}

// Only the invocation that just marked this reservation may provide its
// observed no-start or exact provider rejection to retire a dispatch marker.
func (c *cleanup) retireObservedVolumeFixture(item *cleanupItem, result execpkg.Result) error {
	c.mu.Lock()
	bucket := c.volumeFixtureBucket
	c.mu.Unlock()
	if item == nil || !item.volumeFixtureDispatched ||
		!(result.Err != nil && !result.Started && result.Exit == -1) &&
			!journalfile.VolumeFixtureDefinitiveCreateRejection(item.volumeFixtureKind, bucket, result) {
		return fmt.Errorf("Volume fixture create has no definitive non-allocation result")
	}
	return c.retireVolumeFixtureReservation(item, true)
}

func (c *cleanup) retireVolumeFixtureReservation(item *cleanupItem, observed bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item == nil || item.volumeFixtureKind == "" || !item.volumeFixturePending || item.volumeFixtureDispatched != observed || item.done || c.journal == "" {
		return fmt.Errorf("Volume fixture has no pending reservation to retire")
	}
	if _, err := c.checkedJournalPath(item.role); err != nil {
		return err
	}
	expected, entry, path := c.journalValues(item)
	err := journalfile.WithExistingParentLock(context.Background(), path, func() error {
		root, err := c.openLockedJournalRoot(path)
		if err != nil {
			return err
		}
		defer root.Close()
		journal, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
			return fmt.Errorf("Volume fixture retirement journal binding changed")
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate != entry {
				continue
			}
			if index >= 0 {
				return fmt.Errorf("Volume fixture retirement is ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture pending reservation is missing")
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		if err := writeRunnerJournalRoot(root, path, journal); err != nil {
			body, bodyErr := runnerJournalBytes(journal)
			if bodyErr != nil {
				return errors.Join(err, bodyErr)
			}
			if barrierErr := journalfile.ReaffirmDurableRoot(root, filepath.Base(path), body); barrierErr != nil {
				return errors.Join(err, barrierErr)
			}
		}
		installed, legacy, err := readRunnerJournalRoot(root, path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture retirement was not installed exactly")
		}
		return journalRootMatchesPath(root, path)
	})
	if err != nil {
		return err
	}
	item.done = true
	item.volumeFixturePending = false
	return nil
}

func validVolumeFixtureRole(c *cleanup, stackVars map[string]any, result execpkg.Result) error {
	c.mu.Lock()
	binding, bucket := c.volumeFixtureBinding, c.volumeFixtureBucket
	c.mu.Unlock()
	if binding == nil {
		return fmt.Errorf("Volume fixture trusted account is missing")
	}
	if err := validVolumeFixtureRAMCreateOutcome(binding, bucket, "role", result); err != nil {
		return err
	}
	arn, _ := stackVars["volume_role_arn"].(string)
	name, _ := stackVars["volume_role_name"].(string)
	want := "acs:ram::" + binding.Identity.AccountID + ":role/" + bucket + "-role"
	if !strings.EqualFold(arn, want) || name != bucket+"-role" {
		return fmt.Errorf("Volume fixture created role identity does not match the trusted account and owned role")
	}
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || !ok || root["product"] != "ram" || root["operation"] != "CreateRole" || root["region"] != "cn-hangzhou" || root["error"] != nil || root["actions"] != nil {
		return fmt.Errorf("Volume fixture CreateRole outcome is unproven")
	}
	response, ok := root["response"].(map[string]any)
	if !ok || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil || response["Success"] != nil || response["success"] != nil {
		return fmt.Errorf("Volume fixture CreateRole outcome is unproven")
	}
	requestID, _ := response["RequestId"].(string)
	role, ok := response["Role"].(map[string]any)
	if !ok || strings.TrimSpace(requestID) == "" || role["RoleName"] != name || role["Arn"] != arn {
		return fmt.Errorf("Volume fixture CreateRole returned a different role identity")
	}
	return nil
}

// The raw call envelope identifies the requested operation, while these
// response fields are the RAM service's evidence that creation succeeded.
func validVolumeFixtureRAMCreate(c *cleanup, kind string, result execpkg.Result) error {
	c.mu.Lock()
	binding, bucket := c.volumeFixtureBinding, c.volumeFixtureBucket
	c.mu.Unlock()
	return validVolumeFixtureRAMCreateOutcome(binding, bucket, kind, result)
}

func validVolumeFixtureRAMCreateOutcome(binding *report.QuotaCredentialBinding, bucket, kind string, result execpkg.Result) error {
	operation := map[string]string{"role": "CreateRole", "policy": "CreatePolicy", "attachment": "AttachPolicyToRole"}[kind]
	root, ok := result.JSON.(map[string]any)
	if binding == nil || operation == "" || result.Err != nil || !result.Started || result.Exit != 0 || !ok ||
		root["product"] != "ram" || root["operation"] != operation || root["region"] != "cn-hangzhou" || root["error"] != nil || root["actions"] != nil {
		return fmt.Errorf("Volume fixture RAM create outcome is unproven")
	}
	response, ok := root["response"].(map[string]any)
	if !ok || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil || response["Message"] != nil || response["message"] != nil || response["Success"] != nil || response["success"] != nil {
		return fmt.Errorf("Volume fixture RAM create outcome is unproven")
	}
	requestID, ok := response["RequestId"].(string)
	if !ok || strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("Volume fixture RAM create lacks a trusted request ID")
	}
	if kind == "policy" {
		policy, ok := response["Policy"].(map[string]any)
		if !ok || policy["PolicyName"] != bucket+"-read" || policy["PolicyType"] != "Custom" {
			return fmt.Errorf("Volume fixture created policy identity changed")
		}
	}
	if kind == "role" {
		role, ok := response["Role"].(map[string]any)
		arn, _ := role["Arn"].(string)
		wantARN := "acs:ram::" + binding.Identity.AccountID + ":role/" + bucket + "-role"
		if !ok || role["RoleName"] != bucket+"-role" || !strings.EqualFold(arn, wantARN) {
			return fmt.Errorf("Volume fixture created role identity changed")
		}
	}
	return nil
}

func validVolumeFixtureBucketInfo(c *cleanup, stackVars map[string]any, result execpkg.Result) error {
	c.mu.Lock()
	binding, bucket := c.volumeFixtureBinding, c.volumeFixtureBucket
	c.mu.Unlock()
	if binding == nil || stackVars["volume_bucket_name"] != bucket || fmt.Sprint(stackVars["volume_bucket_owner_id"]) != binding.Identity.AccountID || stackVars["volume_bucket_location"] != "oss-cn-hangzhou" || stackVars["volume_bucket_endpoint"] != "oss-cn-hangzhou.aliyuncs.com" {
		return fmt.Errorf("Volume fixture bucket does not match the trusted account and Hangzhou endpoint")
	}
	root, ok := result.JSON.(map[string]any)
	if result.Err != nil || !result.Started || result.Exit != 0 || !ok || root["product"] != "oss" || root["operation"] != "GetBucketInfo" || root["region"] != "cn-hangzhou" || root["error"] != nil || root["actions"] != nil {
		return fmt.Errorf("Volume fixture original bucket response is unproven")
	}
	response, ok := root["response"].(map[string]any)
	if !ok || response["Code"] != nil || response["code"] != nil || response["Error"] != nil || response["error"] != nil {
		return fmt.Errorf("Volume fixture original bucket response is unproven")
	}
	created, ok := response["Bucket"].(map[string]any)
	if !ok || created["Name"] != bucket || created["Location"] != "oss-cn-hangzhou" || created["ExtranetEndpoint"] != "oss-cn-hangzhou.aliyuncs.com" {
		return fmt.Errorf("Volume fixture original bucket name is unproven")
	}
	owner, ok := created["Owner"].(map[string]any)
	if !ok || owner["ID"] != binding.Identity.AccountID {
		return fmt.Errorf("Volume fixture original bucket owner is unproven")
	}
	return nil
}
