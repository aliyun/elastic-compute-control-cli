package journalfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func ownedTeamChildrenFixture(t *testing.T) (string, report.CleanupJournal, report.Resource, execpkg.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cleanup.json")
	name, id := "ecctl-t-0123456789abcdef01234567", "team-owned"
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete " + id + " --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, TeamProviderTemplateCleanup: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: entry.Region, Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("a", 64), Entries: []report.Resource{entry}}
	receipt := volumeTeamReceipt{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin, Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl", "sandbox", "team", "create"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", TeamProviderTemplateCleanup: true, CreatedAt: time.Now().Add(-time.Minute).UTC()}
	receipt.OwnerBinding = &report.QuotaCredentialBinding{Profile: "primary", Identity: identity}
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusiveDurable(path+".team-create-"+name[8:]+".json", append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: journal.EcctlBin, ExpectedBinSHA256: journal.EcctlBinSHA256, Region: journal.Region, Profile: "primary", ExpectedIdentity: &identity}
	return path, journal, entry, cfg
}

func TestOwnedTeamChildrenClearProviderTemplatesBeforeDelete(t *testing.T) {
	path, journal, entry, cfg := ownedTeamChildrenFixture(t)
	created := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	deleteCalls := 0
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		switch {
		case strings.Contains(command, "team get"):
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"team": map[string]any{"id": "team-owned", "name": "ecctl-t-0123456789abcdef01234567", "description": "ecctl E2E Team owner=0123456789abcdef01234567", "created_time": created}})
		case strings.Contains(command, "template list"):
			if deleteCalls == 0 {
				return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": created}}, "pagination": map[string]any{"has_more": false}})
			}
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{}, "pagination": map[string]any{"has_more": false}})
		case strings.Contains(command, "template get"):
			if deleteCalls == 0 {
				return mockVolumeTeamChildResult(journal, 0, map[string]any{"template": map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": created}})
			}
			return mockVolumeTeamChildResult(journal, 4, map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetTemplate", "code": "TemplateNotFound", "request_id": "req-1"}}})
		case strings.Contains(command, "template delete"):
			deleteCalls++
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"deleted": true, "template": map[string]any{"id": "tpl-child"}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}
	if !OwnedTeamChildCleanupApplicable(journal, entry, cfg) {
		t.Fatal("flagged owned Team with a retained create intent must qualify for child cleanup")
	}
	absent, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run)
	if err != nil || absent || deleteCalls != 1 {
		t.Fatalf("owned Team provider Template cleanup failed: absent=%t deletes=%d err=%v", absent, deleteCalls, err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err != nil {
		t.Fatalf("owned Team delete reservation failed after child cleanup: %v", err)
	}
}

func TestOwnedTeamChildrenWithoutCreateIntentStayPlainDelete(t *testing.T) {
	path, journal, entry, cfg := ownedTeamChildrenFixture(t)
	if err := os.Remove(path + ".team-create-0123456789abcdef01234567.json"); err != nil {
		t.Fatal(err)
	}
	if OwnedTeamChildCleanupApplicable(journal, entry, cfg) {
		t.Fatal("owned Team without a retained create intent must not authorize child cleanup")
	}
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("no provider command may run without create-intent proof: %q", command)
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); !errors.Is(err, ErrOwnedTeamChildNoIntent) {
		t.Fatalf("missing create intent must signal a plain Team delete, got %v", err)
	}
}

func TestOwnedTeamChildrenRejectDifferentRunIdentity(t *testing.T) {
	path, journal, entry, cfg := ownedTeamChildrenFixture(t)
	other := *cfg.ExpectedIdentity
	other.UserID = "user-b"
	cfg.ExpectedIdentity = &other
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		t.Fatalf("no provider command may run under a different identity: %q", command)
		return execpkg.Result{}
	}
	if OwnedTeamChildCleanupApplicable(journal, entry, cfg) {
		t.Fatal("a different run identity must not authorize child cleanup")
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil || !strings.Contains(err.Error(), "differs from its retained create identity") {
		t.Fatalf("changed identity must fail closed, got %v", err)
	}
	cfg = execpkg.Config{Bin: journal.EcctlBin, ExpectedBinSHA256: journal.EcctlBinSHA256, Region: journal.Region}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil || !strings.Contains(err.Error(), "lacks its pinned binary and identity") {
		t.Fatalf("unbound cleanup credential must fail closed, got %v", err)
	}
}

func TestOwnedTeamChildrenLegacyIntentWithoutOwnerBindingStaysPlainDelete(t *testing.T) {
	path, journal, entry, cfg := ownedTeamChildrenFixture(t)
	sidecar := path + ".team-create-0123456789abcdef01234567.json"
	snapshot, err := ReadPrivateRecoverySnapshot(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	var receipt volumeTeamReceipt
	if err := json.Unmarshal(snapshot.Body, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.OwnerBinding = nil
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusiveDurable(sidecar, append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	if OwnedTeamChildCleanupApplicable(journal, entry, cfg) {
		t.Fatal("legacy intent without a retained owner binding must not authorize child cleanup")
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, nil); !errors.Is(err, ErrOwnedTeamChildNoIntent) {
		t.Fatalf("legacy intent must signal a plain Team delete, got %v", err)
	}
}

func TestOwnedTeamCreatorConfigRebuildsRunIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	for _, path := range []string{ecctlPath, aliyunPath} {
		if err := os.WriteFile(path, []byte(`{"current":"profile-a","profiles":[{"name":"profile-a","mode":"AK","access_key_id":"key-a"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "ecctl")
	script := []byte(`#!/bin/sh
case "$*" in *"--profile profile-a"*) ;; *) exit 90;; esac
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`)
	if err := os.WriteFile(bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + ecctlPath, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyunPath}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("profile-a", "AK", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cleanup.json")
	name, id := "ecctl-t-0123456789abcdef01234567", "team-owned"
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete " + id + " --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, TeamProviderTemplateCleanup: true}
	digest := sha256.Sum256(script)
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: entry.Region, Surface: "public", EcctlBin: bin, EcctlBinSHA256: hex.EncodeToString(digest[:]), Entries: []report.Resource{entry}}
	receipt := volumeTeamReceipt{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin, Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl", "sandbox", "team", "create"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", OwnerBinding: binding, TeamProviderTemplateCleanup: true, CreatedAt: time.Now().Add(-time.Minute).UTC()}
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := path + ".team-create-" + name[8:] + ".json"
	if err := WriteExclusiveDurable(sidecar, append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, ExpectedBinSHA256: journal.EcctlBinSHA256, Region: journal.Region, Env: env}
	bound, found, err := OwnedTeamCreatorConfig(context.Background(), journal, cfg, id)
	if err != nil || !found || bound.Profile != "profile-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != identity {
		t.Fatalf("owned Team creator binding was not rebuilt: found=%t bound=%+v err=%v", found, bound, err)
	}
	if !OwnedTeamChildCleanupApplicable(journal, entry, bound) {
		t.Fatal("rebuilt creator binding must authorize child cleanup")
	}
	changed := []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"profile-a","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-b"}}';;
  *) exit 91;;
esac
`)
	changedBin := filepath.Join(dir, "ecctl-other")
	if err := os.WriteFile(changedBin, changed, 0o755); err != nil {
		t.Fatal(err)
	}
	changedDigest := sha256.Sum256(changed)
	changedCfg := execpkg.Config{Bin: changedBin, ExpectedBinSHA256: hex.EncodeToString(changedDigest[:]), Region: journal.Region, Env: env}
	if _, _, err := OwnedTeamCreatorConfig(context.Background(), journal, changedCfg, id); err == nil {
		t.Fatal("changed trusted principal was accepted for owned Team cleanup")
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	if unbound, found, err := OwnedTeamCreatorConfig(context.Background(), journal, cfg, id); err != nil || found || unbound.ExpectedIdentity != nil {
		t.Fatalf("missing owner intent must stay a plain delete: found=%t err=%v", found, err)
	}
}
