package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestReconcilePendingVolumeFixtureRequiresExactCreateReceipt(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	bucket := "ecctl-e2e-20261104t120000-123456abcdef"
	journal := report.CleanupJournal{
		Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary",
		Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(dir, "ecctl"), EcctlBinSHA256: strings.Repeat("a", 64),
		VolumeFixtureBucket: bucket, VolumeFixtureBinding: &report.QuotaCredentialBinding{Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}},
		Entries: []report.Resource{{Scope: "stack", Teardown: `ecctl call ram DeleteRole --region cn-hangzhou --request '{"RoleName":"` + bucket + `-role"}' --output=json`,
			RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeFixtureKind: "role", VolumeFixturePending: true, VolumeFixtureDeleteProtocol: 1,
			VolumeFixtureRoleID: "role-owned", VolumeFixtureCreateRequestID: "req-create", VolumeFixtureCreateResponseSHA256: strings.Repeat("b", 64), VolumeFixtureCreateValidated: true}},
	}
	write := func() string {
		t.Helper()
		body, err := runnerJournalBytes(journal)
		if err != nil {
			t.Fatal(err)
		}
		if err := journalfile.WriteDurable(path, body); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		return hex.EncodeToString(digest[:])
	}
	opt := VolumeFixtureReconcileOptions{RunID: "run", Surface: "public", Region: "cn-hangzhou", Bucket: bucket, Kind: "role", JournalSHA256: write()}
	result, err := ReconcilePendingVolumeFixture(context.Background(), path, opt)
	if err != nil || result == nil || !result.Settled {
		t.Fatalf("durable role receipt did not settle: result=%+v err=%v", result, err)
	}
	installed, _, err := readRunnerJournal(path)
	if err != nil || len(installed.Entries) != 1 || installed.Entries[0].VolumeFixturePending {
		t.Fatalf("settled role was not installed: journal=%+v err=%v", installed, err)
	}
	journal.Entries[0].VolumeFixtureCreateValidated = false
	journal.Entries[0].VolumeFixturePending = true
	opt.JournalSHA256 = write()
	if _, err := ReconcilePendingVolumeFixture(context.Background(), path, opt); err == nil {
		t.Fatal("older receipt without a validated create outcome settled a pending role")
	}
	journal.Entries[0].VolumeFixtureCreateValidated = true
	journal.Entries[0].VolumeFixtureCreateRequestID = ""
	journal.Entries[0].VolumeFixtureCreateResponseSHA256 = ""
	journal.Entries[0].VolumeFixturePending = true
	opt.JournalSHA256 = write()
	if _, err := ReconcilePendingVolumeFixture(context.Background(), path, opt); err == nil {
		t.Fatal("missing invocation receipt settled a pending role")
	}
	journal.Entries[0].VolumeFixtureRoleID = ""
	journal.Entries[0].VolumeFixtureCreateValidated = false
	journal.Entries[0].VolumeFixtureDispatchProtocol = 1
	opt.JournalSHA256 = write()
	originalWrite := writeVolumeReconcileRootBytes
	t.Cleanup(func() { writeVolumeReconcileRootBytes = originalWrite })
	writeVolumeReconcileRootBytes = func(root *os.Root, path string, body []byte) error {
		if err := originalWrite(root, path, body); err != nil {
			return err
		}
		return errors.New("injected post-rename fixture retirement error")
	}
	if _, err := ReconcilePendingVolumeFixture(context.Background(), path, opt); err == nil {
		t.Fatal("post-rename fixture retirement error was ignored")
	}
	writeVolumeReconcileRootBytes = originalWrite
	retired, err := ReconcilePendingVolumeFixture(context.Background(), path, opt)
	if err != nil || retired == nil || !retired.ReservationRetired || !retired.Settled {
		t.Fatalf("pre-dispatch role reservation was not retired: %+v %v", retired, err)
	}
	installed, _, err = readRunnerJournal(path)
	if err != nil || len(installed.Entries) != 0 {
		t.Fatalf("pre-dispatch role reservation remains: %+v %v", installed, err)
	}
	journal.Entries[0].VolumeFixtureDispatched = true
	opt.JournalSHA256 = write()
	if _, err := ReconcilePendingVolumeFixture(context.Background(), path, opt); err == nil {
		t.Fatal("dispatched role without a create receipt was retired")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
