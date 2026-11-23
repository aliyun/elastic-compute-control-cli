package journalfile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func volumeTeamChildrenFixture(t *testing.T, bin ...string) (string, report.CleanupJournal, report.Resource, execpkg.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cleanup.json")
	name, id := "ecctl-t-0123456789abcdef01234567", "team-owned"
	binding := &report.QuotaCredentialBinding{Profile: "fixture", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete " + id + " --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTeamBound: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: entry.Region, Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("a", 64), VolumeFixtureBinding: binding, VolumeTeamCreators: map[string]*report.QuotaCredentialBinding{id: binding}, VolumeTeamCreateIDs: map[string]string{name: id}, Entries: []report.Resource{entry}}

	if len(bin) != 0 && bin[0] != "" {
		journal.EcctlBin = bin[0]
		digest, err := ExecutableSHA256(bin[0])
		if err != nil {
			t.Fatal(err)
		}
		journal.EcctlBinSHA256 = digest
	}
	receipt := volumeTeamReceipt{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin, Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl", "sandbox", "team", "create"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", AcceptedID: id, Dispatched: true, DispatchProtocol: 1, VolumeBinding: binding, CreatedAt: time.Now().Add(-time.Minute).UTC()}
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusiveDurable(path+".team-create-"+name[8:]+".json", append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: journal.EcctlBin, ExpectedBinSHA256: journal.EcctlBinSHA256, Region: journal.Region, Profile: binding.Profile, ExpectedIdentity: &binding.Identity}
	return path, journal, entry, cfg
}

func mockVolumeTeamChildResult(journal report.CleanupJournal, exit int, payload map[string]any) execpkg.Result {
	return execpkg.Result{Started: true, Exit: exit, JSON: payload, VerifiedBinSHA256: journal.EcctlBinSHA256}
}

func mockVolumeTeamPresent(t *testing.T, path string, journal report.CleanupJournal) execpkg.Result {
	t.Helper()
	receipt := volumeTeamChildrenAcceptedReceipt(t, path)
	created := receipt.CreatedAt.Add(time.Second).Truncate(time.Second).Format(time.RFC3339)
	return mockVolumeTeamChildResult(journal, 0, map[string]any{"team": map[string]any{"id": "team-owned", "name": receipt.Name, "description": receipt.Description, "created_time": created}})
}

func TestVolumeTeamChildrenPersistExactTargetBeforeDelete(t *testing.T) {
	var bin string
	if runtime.GOOS != "windows" {
		t.Setenv("ECCTL_DISPLAY_MODE", "Human")
		bin = filepath.Join(t.TempDir(), "ecctl")
		const script = `#!/bin/sh
[ "$ECCTL_DISPLAY_MODE" = Human ] || exit 81
for flag in "$@"; do
    if [ "$flag" = --no-color=true ]; then
        printf '%s\n' "$ISSUE128_RESPONSE"
        exit "$ISSUE128_EXIT"
    fi
done
printf '\033[32m%s\033[0m\n' "$ISSUE128_RESPONSE"
exit "$ISSUE128_EXIT"
`
		if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path, journal, entry, cfg := volumeTeamChildrenFixture(t, bin)
	created := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	deleteCalls := 0
	mock := func(command string) execpkg.Result {
		switch {
		case strings.Contains(command, "team get"):
			return mockVolumeTeamPresent(t, path, journal)
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
			snapshot, err := ReadPrivateRecoverySnapshot(volumeTeamChildrenPath(path, "team-owned"))
			if err != nil || !strings.Contains(string(snapshot.Body), `"state": "attempted"`) || !strings.Contains(string(snapshot.Body), `"id": "tpl-child"`) {
				t.Errorf("exact child was not durably reserved before delete: %v %s", err, snapshot.Body)
			}
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"deleted": true, "template": map[string]any{"id": "tpl-child"}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}

	run := func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		reply := mock(command)
		if bin == "" {
			return reply
		}
		body, err := json.Marshal(reply.JSON)
		if err != nil {
			t.Fatal(err)
		}
		bound.Env = append(bound.Env, "ISSUE128_RESPONSE="+string(body), "ISSUE128_EXIT="+strconv.Itoa(reply.Exit))
		res := execpkg.Run(ctx, bound, command)
		if !slices.Contains(res.Argv, "--no-color=true") || !json.Valid([]byte(res.Stdout)) || res.Exit != reply.Exit {
			t.Errorf("child proof lost its machine output: argv=%q exit=%d want=%d stdout=%q err=%v", res.Argv, res.Exit, reply.Exit, res.Stdout, res.Err)
		}
		return res
	}
	absent, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run)
	if err != nil || absent || deleteCalls != 1 {
		t.Fatalf("owned child cleanup failed: absent=%t deletes=%d err=%v", absent, deleteCalls, err)
	}
	if err := VolumeTeamChildrenReady(path, journal, entry); err != nil {
		t.Fatalf("completed child inventory did not prove fixture readiness: %v", err)
	}
}

func TestVolumeTeamChildrenRejectIncompleteInventoryBeforeDelete(t *testing.T) {
	path, journal, entry, cfg := volumeTeamChildrenFixture(t)
	deletes := 0
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "team get") {
			return mockVolumeTeamPresent(t, path, journal)
		}
		if strings.Contains(command, "template list") {
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": time.Now().UTC().Format(time.RFC3339)}}, "pagination": map[string]any{"has_more": true}})
		}
		deletes++
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil || deletes != 0 {
		t.Fatalf("incomplete page authorized mutation: deletes=%d err=%v", deletes, err)
	}
	if _, err := os.Stat(volumeTeamChildrenPath(path, "team-owned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete inventory was persisted: %v", err)
	}
}

func TestVolumeTeamChildrenDoNotRepeatUnknownDelete(t *testing.T) {
	path, journal, entry, cfg := volumeTeamChildrenFixture(t)
	created := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	deletes := 0
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		switch {
		case strings.Contains(command, "team get"):
			return mockVolumeTeamPresent(t, path, journal)
		case strings.Contains(command, "template list"):
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": created}}, "pagination": map[string]any{"has_more": false}})
		case strings.Contains(command, "template get"):
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"template": map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": created}})
		case strings.Contains(command, "template delete"):
			deletes++
			return mockVolumeTeamChildResult(journal, 2, map[string]any{"error": map[string]any{"code": "CloudAPIError"}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil {
		t.Fatal("unknown delete result was accepted")
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil || deletes != 1 {
		t.Fatalf("attempted child was deleted twice: calls=%d err=%v", deletes, err)
	}
	if err := VolumeTeamChildrenReady(path, journal, entry); err == nil {
		t.Fatal("unknown child outcome unlocked independent fixtures")
	}
}

func TestVolumeTeamChildrenRejectForeignOrOlderTemplate(t *testing.T) {
	for _, tc := range []struct {
		name, teamID string
		created      time.Time
	}{
		{"foreign Team", "team-foreign", time.Now()},
		{"older Template", "team-owned", time.Now().Add(-24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, journal, entry, cfg := volumeTeamChildrenFixture(t)
			mutations := 0
			run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
				if strings.Contains(command, "team get") {
					return mockVolumeTeamPresent(t, path, journal)
				}
				if strings.Contains(command, "template list") {
					return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{map[string]any{"id": "tpl-child", "name": "base", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": tc.teamID, "created_time": tc.created.UTC().Format(time.RFC3339)}}, "pagination": map[string]any{"has_more": false}})
				}
				mutations++
				return execpkg.Result{}
			}
			if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil || mutations != 0 {
				t.Fatalf("unowned child authorized mutation: calls=%d err=%v", mutations, err)
			}
		})
	}
}

func TestVolumeTeamChildrenInvalidateStaleEmptyProofBeforeNewRead(t *testing.T) {
	path, journal, entry, cfg := volumeTeamChildrenFixture(t)
	listCalls := 0
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "team get") {
			return mockVolumeTeamPresent(t, path, journal)
		}
		if strings.Contains(command, "template list") {
			listCalls++
			if listCalls <= 2 {
				return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{}, "pagination": map[string]any{"has_more": false}})
			}
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{map[string]any{"id": "tpl-unowned", "name": "business", "team_name": "ecctl-t-0123456789abcdef01234567", "team_id": "team-owned", "created_time": time.Now().UTC().Format(time.RFC3339)}}, "pagination": map[string]any{"has_more": false}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err != nil {
		t.Fatal(err)
	}
	if err := VolumeTeamChildrenReady(path, journal, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err == nil {
		t.Fatal("new unowned child was accepted")
	}
	if err := VolumeTeamChildrenReady(path, journal, entry); err == nil {
		t.Fatal("old empty-list proof remained valid after failed refresh")
	}
}

func TestVolumeTeamDeleteRequiresConflictAndFreshEmptyListForSecondAttempt(t *testing.T) {
	path, journal, entry, cfg := volumeTeamChildrenFixture(t)
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "team get") {
			return mockVolumeTeamPresent(t, path, journal)
		}
		if strings.Contains(command, "template list") {
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{}, "pagination": map[string]any{"has_more": false}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err != nil {
		t.Fatal(err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err != nil {
		t.Fatal(err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err == nil {
		t.Fatal("unknown first Team outcome authorized a second delete")
	}
	conflict := mockVolumeTeamChildResult(journal, 2, map[string]any{"error": map[string]any{"kind": "service", "code": "CloudAPIError"}, "actions": []any{map[string]any{"action_name": "DeleteTeam", "code": "Conflict.TeamHasTemplates", "request_id": "req-2"}}})
	if err := RecordVolumeTeamConflict(context.Background(), path, journal, entry, cfg, conflict); err != nil {
		t.Fatal(err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err == nil {
		t.Fatal("conflict without a fresh empty list authorized a second delete")
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err != nil {
		t.Fatal(err)
	}
	state, err := readVolumeTeamChildren(path, journal, volumeTeamChildrenAcceptedReceipt(t, path), "team-owned")
	if err != nil {
		t.Fatal(err)
	}
	state.ConflictAt = time.Now().Add(-10 * time.Second)
	if err := writeVolumeTeamChildren(path, state, false); err != nil {
		t.Fatal(err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err != nil {
		t.Fatalf("fresh empty list did not permit bounded retry: %v", err)
	}
	if err := RecordVolumeTeamConflict(context.Background(), path, journal, entry, cfg, conflict); err != nil {
		t.Fatalf("second known failure was not retained for reconciliation: %v", err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err == nil {
		t.Fatal("third Team delete was authorized")
	}
}

func TestVolumeTeamConflictRetryUsesFreshEmptyListOnce(t *testing.T) {
	path, journal, entry, cfg := volumeTeamChildrenFixture(t)
	deleteCalls := 0
	run := func(_ context.Context, _ execpkg.Config, command string) execpkg.Result {
		switch {
		case strings.Contains(command, "team get"):
			return mockVolumeTeamPresent(t, path, journal)
		case strings.Contains(command, "template list"):
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"templates": []any{}, "pagination": map[string]any{"has_more": false}})
		case strings.Contains(command, "team delete"):
			deleteCalls++
			return mockVolumeTeamChildResult(journal, 0, map[string]any{"deleted": true, "team": map[string]any{"id": "team-owned"}})
		}
		t.Fatalf("unexpected command %q", command)
		return execpkg.Result{}
	}
	if _, err := PrepareVolumeTeamChildren(context.Background(), path, journal, entry, cfg, run); err != nil {
		t.Fatal(err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err != nil {
		t.Fatal(err)
	}
	conflict := mockVolumeTeamChildResult(journal, 2, map[string]any{"error": map[string]any{"kind": "service", "code": "CloudAPIError"}, "actions": []any{map[string]any{"action_name": "DeleteTeam", "code": "Conflict.TeamHasTemplates", "request_id": "req-2"}}})
	if err := RecordVolumeTeamConflict(context.Background(), path, journal, entry, cfg, conflict); err != nil {
		t.Fatal(err)
	}
	state, err := readVolumeTeamChildren(path, journal, volumeTeamChildrenAcceptedReceipt(t, path), "team-owned")
	if err != nil {
		t.Fatal(err)
	}
	state.ConflictAt = time.Now().Add(-10 * time.Second)
	if err := writeVolumeTeamChildren(path, state, false); err != nil {
		t.Fatal(err)
	}
	result, err := RetryVolumeTeamDeleteAfterConflict(context.Background(), path, journal, entry, cfg, run)
	if err != nil || result.Exit != 0 || deleteCalls != 1 {
		t.Fatalf("bounded retry failed: result=%+v calls=%d err=%v", result, deleteCalls, err)
	}
	if err := ReserveVolumeTeamDelete(context.Background(), path, journal, entry, cfg); err == nil {
		t.Fatal("successful retry left a third Team mutation available")
	}
}

func volumeTeamChildrenAcceptedReceipt(t *testing.T, path string) volumeTeamReceipt {
	t.Helper()
	snapshot, err := ReadPrivateRecoverySnapshot(path + ".team-create-0123456789abcdef01234567.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt volumeTeamReceipt
	if err := json.Unmarshal(snapshot.Body, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}
