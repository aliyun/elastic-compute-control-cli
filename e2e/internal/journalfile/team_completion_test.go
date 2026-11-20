package journalfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// Existing private Quota receipts remain readable, including the retired
// independent-reader field. New writes are limited to Volume-owned Teams.
func TestRetainedQuotaTeamCompletionReceiptRemainsReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, TeamReplayAttempted: true, QuotaBound: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("a", 64), QuotaBinding: &report.QuotaCredentialBinding{Profile: "owner"}, Entries: []report.Resource{entry}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	journalBody, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, filepath.Base(path), journalBody); err != nil {
		t.Fatal(err)
	}
	journalHash := sha256.Sum256(journalBody)
	journalDigest := hex.EncodeToString(journalHash[:])
	if err := EnsureHistoricalJournalOriginal(root, path, journalDigest, journalBody); err != nil {
		t.Fatal(err)
	}
	evidence := `{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTeam","code":"TeamNotFound","request_id":"req-exact","provider_verified":true}]}`
	entryBody, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	entryHash, evidenceHash := sha256.Sum256(entryBody), sha256.Sum256([]byte(evidence))
	receipt := teamCompletionReceipt{Version: 1, JournalPath: path, JournalSHA256: journalDigest, EcctlBinSHA256: journal.EcctlBinSHA256, ReadBinSHA256: strings.Repeat("b", 64), RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EntrySHA256: hex.EncodeToString(entryHash[:]), TeamID: "team-owned", Profile: "owner", Proof: "verified-absence", Evidence: evidence, EvidenceSHA256: hex.EncodeToString(evidenceHash[:])}
	receiptBody, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(teamCompletionPath(path, entry))
	if err := WriteExclusiveDurableRoot(root, name, append(receiptBody, '\n')); err != nil {
		t.Fatal(err)
	}
	if staged, err := ReadTeamCompletion(root, path, journal, entry); err != nil || !staged {
		t.Fatalf("retained Quota receipt rejected: staged=%t err=%v", staged, err)
	}
	if err := RecordTeamCompletion(root, path, journal, entry, execpkg.Result{}); err == nil {
		t.Fatal("new Quota receipt writer was accepted")
	}
	receipt.ReadBinSHA256 = "invalid"
	tampered, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, name, tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTeamCompletion(root, path, journal, entry); err == nil {
		t.Fatal("invalid retained reader digest was accepted")
	}
}

func TestHistoricalVolumeTeamPreDispatchRetirementKeepsOriginalLineage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	name := "ecctl-t-0123456789abcdef01234567"
	binding := &report.QuotaCredentialBinding{Profile: "owner"}
	original := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", VolumeFixtureBinding: binding, VolumeTeamCreateIDs: map[string]string{name: ""}}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	originalSHA := hex.EncodeToString(digest[:])
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := EnsureHistoricalJournalOriginal(root, path, originalSHA, body); err != nil {
		t.Fatal(err)
	}
	intent := volumeTeamReceipt{Version: 1, RunID: original.RunID, ExecutionID: original.ExecutionID, Region: original.Region, Surface: original.Surface, EcctlBin: original.EcctlBin, Name: name, Description: "ecctl E2E Team owner=" + name[8:], CaseScope: "case", VolumeBinding: binding, DispatchProtocol: 1, CreatedAt: time.Now().UTC()}
	intentBody, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Base(path) + ".team-create-" + name[8:] + ".json"
	contents := map[string][]byte{sidecar: intentBody}
	authenticated := map[string]bool{sidecar: true}
	retired := original
	retired.VolumeTeamCreateIDs = nil
	retired.VolumeRetirementReceipts = map[string]report.VolumeRetirementReceipt{"team:" + name: {OriginalSHA256: originalSHA, TeamName: name}}
	if err := validateHistoricalSuccessor(root, path, original, retired, contents, authenticated, filepath.Base(path)); err != nil {
		t.Fatalf("exact pre-dispatch retirement rejected: %v", err)
	}
	changed := retired
	changed.VolumeRetirementReceipts = map[string]report.VolumeRetirementReceipt{"team:" + name: {OriginalSHA256: originalSHA, TeamName: "other"}}
	if err := validateHistoricalSuccessor(root, path, original, changed, contents, authenticated, filepath.Base(path)); err == nil {
		t.Fatal("changed retired nonce was accepted")
	}
}

func TestHistoricalVolumeTeamSettlementAcceptsOnlyBoundFinalizer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	name, id := "ecctl-t-0123456789abcdef01234567", "team-owned"
	binding := &report.QuotaCredentialBinding{Profile: "owner"}
	original := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", VolumeFixtureBinding: binding, VolumeTeamCreateIDs: map[string]string{name: ""}}
	intent := volumeTeamReceipt{Version: 1, RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl", "sandbox", "team", "create"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", AcceptedID: id, Dispatched: true, DispatchProtocol: 1, VolumeBinding: binding, CreatedAt: time.Now().UTC()}
	intentBody, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Base(path) + ".team-create-" + name[8:] + ".json"
	contents := map[string][]byte{sidecar: intentBody}
	authenticated := map[string]bool{sidecar: true}
	current := original
	current.VolumeTeamCreateIDs = map[string]string{name: id}
	current.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{id: binding}
	current.Entries = []report.Resource{{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTeamBound: true}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := validateHistoricalSuccessor(root, path, original, current, contents, authenticated, filepath.Base(path)); err != nil {
		t.Fatalf("accepted Team transition rejected: %v", err)
	}
	tampered := current
	tampered.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{id: {Profile: "other"}}
	if err := validateHistoricalSuccessor(root, path, original, tampered, contents, authenticated, filepath.Base(path)); err == nil {
		t.Fatal("changed Team creator identity was accepted")
	}
	tampered = current
	tampered.Entries = append(append([]report.Resource(nil), current.Entries...), report.Resource{Teardown: "ecctl ecs instance delete i-foreign"})
	if err := validateHistoricalSuccessor(root, path, original, tampered, contents, authenticated, filepath.Base(path)); err == nil {
		t.Fatal("unrelated cleanup target was accepted")
	}
	current.EcctlBinSHA256 = strings.Repeat("a", 64)
	current.Entries[0].TeamReplayAttempted = true
	journalBody, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(path, append(journalBody, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(filepath.Join(dir, sidecar), intentBody); err != nil {
		t.Fatal(err)
	}
	absenceBody := `{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTeam","code":"TeamNotFound","request_id":"req-exact","provider_verified":true}]}`
	var absence any
	if err := json.Unmarshal([]byte(absenceBody), &absence); err != nil {
		t.Fatal(err)
	}
	readback := execpkg.Result{Command: "/inert/ecctl sandbox team get team-owned --region cn-hangzhou --profile owner --output=json --agent-envelope=false", Argv: []string{"sandbox", "team", "get", "team-owned", "--region", "cn-hangzhou", "--profile", "owner", "--output=json", "--agent-envelope=false"}, VerifiedBinSHA256: current.EcctlBinSHA256, Started: true, Exit: 4, Stdout: absenceBody, JSON: absence}
	if err := RecordTeamCompletion(root, path, current, current.Entries[0], readback); err != nil {
		t.Fatalf("stage accepted Team completion: %v", err)
	}
	consumed := current
	consumed.Entries = nil
	if err := validateHistoricalSuccessor(root, path, original, consumed, contents, authenticated, filepath.Base(path)); err != nil {
		t.Fatalf("completed accepted Team transition rejected: %v", err)
	}
}
