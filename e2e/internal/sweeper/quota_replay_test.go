package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func quotaReplayBinding(t *testing.T, dir string) *report.QuotaCredentialBinding {
	t.Helper()
	t.Setenv("HOME", dir)
	binding, err := journalfile.CaptureQuotaBinding("quota-test", "AK", execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}, []string{"HOME=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestReplayQuotaSTSDriftStopsBeforeCredentialProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "cleanup.json")
	aliyun := filepath.Join(dir, "aliyun.json")
	native := filepath.Join(dir, "native.json")
	fake := filepath.Join(dir, "ecctl")
	marker := filepath.Join(dir, "provider-calls")
	t.Setenv("FAKE_LOG", marker)
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"quota-test","mode":"OIDC","oidc_provider_arn":"acs:ram::1234567890123456:oidc-provider/owned","ram_role_arn":"acs:ram::1234567890123456:role/owned","oidc_token_file":"/unused/token"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho called >> \"$FAKE_LOG\"\nexit 98\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ALIBABA_CLOUD_STS_ENDPOINT=sts.cn-hangzhou.aliyuncs.com"}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "quota-user"}
	binding, err := journalfile.CaptureQuotaBinding("quota-test", "OIDC", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, QuotaBinding: binding, Entries: []report.Resource{
		{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true},
		{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true},
	}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := append(append([]string{}, env...), "ALIBABA_CLOUD_STS_ENDPOINT=other.example")
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou", Env: changed}, RunID: "run", Surface: "public"})
	if err == nil || !strings.Contains(err.Error(), "effective STS") || result == nil || result.Deleted != 0 {
		t.Fatalf("STS drift reached replay provider: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("credential probe or delete was launched: %v", err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || !slices.Equal(stored.Entries, journal.Entries) {
		t.Fatalf("rejected replay lost ownership: entries=%+v err=%v", stored.Entries, err)
	}
}

func TestReplayQuotaRejectsMissingCredentialBindingBeforeProvider(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(dir, "must-not-launch"), Entries: []report.Resource{
		{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true},
		{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true},
	}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: journal.EcctlBin, Region: journal.Region}})
	if err != nil || result.Errors != 1 || result.Deleted != 0 || !strings.Contains(result.Details[0], "credential binding") {
		t.Fatalf("unbound Quota reached provider: result=%+v err=%v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !slices.Equal(after, body) {
		t.Fatalf("rejected Quota journal changed: %v", err)
	}
}

func TestQuotaReplayTeamOnlySnapshotExcludesLateReservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup-journal.json")
	fake := filepath.Join(dir, "ecctl")
	entry := report.Resource{Scope: "case", Teardown: "ecctl sbx team delete team-owned --timeout=300s --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	replayRead := make(chan struct{})
	release := make(chan struct{})
	releaseReplay := sync.OnceFunc(func() { close(release) })
	defer releaseReplay()
	previous := replayJournalAfterRead
	replayJournalAfterRead = func(string) { close(replayRead); <-release }
	defer func() { replayJournalAfterRead = previous }()
	replayDone := make(chan error, 1)
	replayCtx, cancelReplay := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelReplay()
	go func() {
		_, err := ReplayJournalWithOptions(replayCtx, path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
		replayDone <- err
	}()
	select {
	case <-replayRead:
	case err := <-replayDone:
		t.Fatalf("replay returned before read hook: %v", err)
	case <-replayCtx.Done():
		t.Fatalf("replay did not reach read hook: %v", replayCtx.Err())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	reserved := false
	if err := journalfile.WithReplayLock(ctx, path, func() error { reserved = true; return nil }); !errors.Is(err, context.DeadlineExceeded) || reserved {
		t.Fatalf("late reservation entered Team-only replay snapshot: err=%v reserved=%t", err, reserved)
	}
	releaseReplay()
	if err := <-replayDone; err != nil {
		t.Fatal(err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("Team replay did not consume parent before reservation: entries=%+v err=%v", stored.Entries, err)
	}
}

func TestReplayTeamAttemptPostRenameFailureRequiresBarrierOnRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls")
	t.Setenv("FAKE_LOG", logPath)
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho delete >> \"$FAKE_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := writeTeamReplayAttemptJournal
	t.Cleanup(func() { writeTeamReplayAttemptJournal = previous })
	writes := 0
	writeTeamReplayAttemptJournal = func(path string, body []byte) error {
		writes++
		if err := previous(path, body); err != nil {
			return err
		}
		if writes <= 2 {
			return errors.New("injected post-rename Team attempt sync failure")
		}
		return nil
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"}
	for round := 1; round <= 2; round++ {
		if _, err := ReplayJournalWithOptions(context.Background(), path, opt); err == nil || !strings.Contains(err.Error(), "injected post-rename Team attempt sync failure") {
			t.Fatalf("round %d crossed uncertain attempt barrier: %v", round, err)
		}
		stored, _, err := readReplayJournal(path)
		if err != nil || len(stored.Entries) != 1 || !stored.Entries[0].TeamReplayAttempted {
			t.Fatalf("round %d lost Team attempt ownership: %+v %v", round, stored.Entries, err)
		}
		if _, err := os.Stat(logPath); !os.IsNotExist(err) {
			t.Fatalf("round %d launched delete without barrier: %v", round, err)
		}
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err != nil || result.Deleted != 1 || writes != 3 {
		t.Fatalf("barrier recovery did not allow replay: result=%+v err=%v writes=%d", result, err, writes)
	}
	stored, _, err := readReplayJournal(path)
	calls, readErr := os.ReadFile(logPath)
	if err != nil || len(stored.Entries) != 0 || readErr != nil || strings.Count(string(calls), "delete") != 1 {
		t.Fatalf("recovered Team delete not consumed exactly once: entries=%+v calls=%s err=%v read=%v", stored.Entries, calls, err, readErr)
	}
}

func TestReplayRejectsQuotaUpdateDisguisedAsDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	for _, legacy := range []bool{true, false} {
		for _, command := range []string{
			"ecctl sandbox quota update delete --cpu-cores 1 --memory-gb 1 --output=json",
			"ecctl --region cn-hangzhou sbx quota update delete --cpu-cores 1 --memory-gb 1 --output=json",
		} {
			t.Run(fmt.Sprintf("legacy=%t/%s", legacy, command), func(t *testing.T) {
				dir := canonicalJournalTestDir(t)
				path := filepath.Join(dir, "cleanup.json")
				fake := filepath.Join(dir, "ecctl")
				logPath := filepath.Join(dir, "calls")
				if err := os.WriteFile(fake, []byte("#!/bin/sh\necho called >> \"$FAKE_LOG\"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("FAKE_LOG", logPath)
				entry := report.Resource{Scope: "case", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
				journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
				var payload any = journal
				if legacy {
					payload = journal.Entries
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}})
				if err != nil || result.Errors != 1 || result.Deleted != 0 || !strings.Contains(result.Details[0], "invalid Quota teardown") {
					t.Fatalf("Quota mutation replayed: result=%+v err=%v", result, err)
				}
				if _, err := os.Stat(logPath); !os.IsNotExist(err) {
					t.Fatalf("Quota mutation reached CLI: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(body) {
					t.Fatalf("rejected journal changed: %v", err)
				}
			})
		}
	}
}

func TestReplayUnrelatedDeleteWithPositionalDeleteID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := canonicalJournalTestDir(t)
	path, fake := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox template delete delete --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}})
	if err != nil || result.Errors != 0 || result.Deleted != 1 {
		t.Fatalf("unrelated positional ID was blocked: result=%+v err=%v", result, err)
	}
	if calls, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(calls), "template delete delete") {
		t.Fatalf("unrelated delete was not invoked: calls=%q err=%v", calls, err)
	}
}

func TestReplayRejectsOrdinaryTeamRegionMismatch(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "must-not-launch")
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete independent-team --timeout 5m --region cn-beijing", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result.Errors != 1 || result.Deleted != 0 || !strings.Contains(result.Details[0], "region") {
		t.Fatalf("replay accepted conflicting Team region: result=%+v err=%v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(body) {
		t.Fatalf("rejected replay changed journal: %v", err)
	}
}

func TestTeamReplayRejectsVariantBeforeRetainedChildOrConfirmedParent(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		entries    []report.Resource
	}{
		{"parent before Quota child", "Quota child", []report.Resource{
			{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", QuotaReserved: true},
			{Scope: "case", Teardown: "ecctl sbx team delete team-owned --timeout=1m --output=json", TeamCreateConfirmed: true},
		}},
		{"parent before quoted Quota child", "Quota child", []report.Resource{
			{Scope: "case", Teardown: "ecctl sandbox quota delete  'team-owned' --output=json", QuotaReserved: true},
			{Scope: "case", Teardown: "ecctl sbx team delete team-owned --timeout=1m --output=json", TeamCreateConfirmed: true},
		}},
		{"ordinary variant before confirmed parent", "confirmed ownership", []report.Resource{
			{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", TeamCreateConfirmed: true},
			{Scope: "case", Teardown: "ecctl sbx team delete team-owned --timeout=1m --output=json"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			fake := filepath.Join(dir, "must-not-launch")
			for i := range tc.entries {
				tc.entries[i].RegionRole = "primary"
				tc.entries[i].Region = "cn-hangzhou"
				tc.entries[i].ExecutionID = "execution"
			}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: tc.entries}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
			if err == nil {
				if result == nil || result.Errors != 1 || len(result.Details) != 1 || !strings.Contains(result.Details[0], tc.want) {
					t.Fatalf("unsafe Team replay was not blocked: result=%+v err=%v", result, err)
				}
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unsafe Team replay was not blocked: result=%+v err=%v", result, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(body) {
				t.Fatalf("rejected Team replay changed journal: err=%v before=%s after=%s", err, body, after)
			}
		})
	}
}

func TestTeamReplayRejectsUnclassifiedOrdinaryDeleteBesideOwnedParent(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "must-not-launch")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		Entries: []report.Resource{
			{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true},
			{Scope: "case", Teardown: "ecctl sbx team delete team-owned", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
		}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result.Errors != 1 || result.Deleted != 0 {
		t.Fatalf("unclassified Team delete escaped replay fence: result=%+v err=%v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(body) {
		t.Fatalf("unclassified Team replay changed journal: err=%v before=%s after=%s", err, body, after)
	}
}

func TestTeamReplayFailureRetainsDurableAttemptFence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup-journal.json")
	fake := filepath.Join(dir, "ecctl")
	entry := report.Resource{Scope: "owned-quota-case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "quota-execution", TeamCreateConfirmed: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "quota-run", ExecutionID: "quota-execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho '{\"error\":{\"code\":\"DeleteUnknown\"}}'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "quota-run", Surface: "public"}
	result, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err != nil || result.Errors != 1 || result.Deleted != 0 {
		t.Fatalf("failed Team replay result=%+v err=%v", result, err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 1 || !stored.Entries[0].TeamReplayAttempted {
		t.Fatalf("Team replay failure lost attempt fence: entries=%+v err=%v", stored.Entries, err)
	}
	parent, err := replayParentIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markTeamReplayAttempt(context.Background(), path, opt, entry, parent); err == nil {
		// The old unmarked snapshot must not be reusable after the attempt.
		t.Fatalf("stale Team snapshot was accepted: %v", err)
	}
}

func TestReplayOwnedQuotaFailureKeepsTeamAndBothJournalEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	for _, mode := range []string{"failure", "unknown", "success"} {
		t.Run(mode, func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			journalPath := filepath.Join(dir, "cleanup-journal.json")
			entries := []report.Resource{
				{Scope: "owned-quota-case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "quota-execution", TeamCreateConfirmed: true, QuotaBound: true},
				{Scope: "owned-quota-case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "quota-execution", QuotaReserved: true},
			}
			journal := report.CleanupJournal{Version: 2, JournalPath: journalPath, RunID: "quota-run", ExecutionID: "quota-execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", Entries: entries, QuotaBinding: quotaReplayBinding(t, dir)}
			fake := filepath.Join(dir, "ecctl")
			journal.EcctlBin = fake
			data, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
echo "$*" >> "$FAKE_LOG"
if [[ "$1 $2 $3" == "sandbox quota delete" ]]; then
  case "$FAKE_MODE" in
    failure) echo '{"error":{"code":"QuotaCleanupRejected"}}'; exit 1 ;;
    unknown) kill -KILL "$$" ;;
    success) echo '{"deleted":true}'; exit 0 ;;
  esac
fi
if [[ "$1 $2 $3" == "sandbox team delete" ]]; then
  echo '{"deleted":true}'
  exit 0
fi
echo '{"error":{"code":"UnexpectedCommand"}}'
exit 1
`), 0o755); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "calls.log")
			t.Setenv("FAKE_LOG", logPath)
			t.Setenv("FAKE_MODE", mode)
			opt := ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "quota-run", Surface: "public"}
			result, err := ReplayJournalWithOptions(context.Background(), journalPath, opt)
			if err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			stored, _, err := readReplayJournal(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			const quotaCall = "sandbox quota delete team-owned --output=json --region cn-hangzhou --profile quota-test"
			const teamCall = "sandbox team delete team-owned --timeout 5m --region cn-hangzhou --profile quota-test"
			if mode != "success" {
				if result.Errors != 1 || result.Deleted != 0 || strings.TrimSpace(string(calls)) != quotaCall || !slices.Equal(stored.Entries, entries) {
					t.Fatalf("failed/unknown Quota replay reached Team or consumed recovery: result=%+v calls=%q entries=%+v", result, calls, stored.Entries)
				}
				return
			}
			if result.Errors != 0 || result.Deleted != 2 || strings.TrimSpace(string(calls)) != quotaCall+"\n"+teamCall || len(stored.Entries) != 0 {
				t.Fatalf("successful Quota replay did not consume each child/parent once: result=%+v calls=%q entries=%+v", result, calls, stored.Entries)
			}
			if _, err := ReplayJournalWithOptions(context.Background(), journalPath, opt); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(calls) {
				t.Fatalf("successful Quota replay ran cleanup twice: before=%q after=%q", calls, after)
			}
		})
	}
}

func TestReplayConsumptionPostRenameFailureStopsBeforeParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			fake := filepath.Join(dir, "ecctl")
			logPath := filepath.Join(dir, "calls.log")
			t.Setenv("FAKE_LOG", logPath)
			parent := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true}
			child := report.Resource{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true}
			if legacy {
				parent = report.Resource{Scope: "stack", Teardown: "ecctl t thing delete parent"}
				child = report.Resource{Scope: "stack", Teardown: "ecctl t thing delete child"}
			}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: []report.Resource{parent, child}}
			var payload any = journal
			if legacy {
				payload = journal.Entries
			} else {
				journal.QuotaBinding = quotaReplayBinding(t, dir)
				payload = journal
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fake, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
printf '%s\n' "$*" >> "$FAKE_LOG"
echo '{"deleted":true}'
`), 0o755); err != nil {
				t.Fatal(err)
			}
			previous := writeReplayConsumptionJournal
			t.Cleanup(func() { writeReplayConsumptionJournal = previous })
			writes := 0
			writeReplayConsumptionJournal = func(path string, body []byte) error {
				writes++
				if err := previous(path, body); err != nil {
					return err
				}
				if writes == 1 {
					return errors.New("injected post-rename consumption sync failure")
				}
				return nil
			}
			opt := ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"}
			result, err := ReplayJournalWithOptions(context.Background(), path, opt)
			if err == nil || !strings.Contains(err.Error(), "injected post-rename consumption sync failure") || result == nil || result.Deleted != 1 {
				t.Fatalf("post-rename consumption error was ignored: result=%+v err=%v", result, err)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil || strings.Count(string(calls), "delete") != 1 || !strings.Contains(string(calls), strings.TrimPrefix(child.Teardown, "ecctl ")) || strings.Contains(string(calls), strings.TrimPrefix(parent.Teardown, "ecctl ")) {
				t.Fatalf("parent delete crossed uncertain consumption: calls=%q err=%v", calls, err)
			}
			stored, wasLegacy, err := readReplayJournal(path)
			if err != nil || wasLegacy != legacy || !slices.Equal(stored.Entries, []report.Resource{parent}) {
				t.Fatalf("remaining parent or journal encoding lost: journal=%+v legacy=%t err=%v", stored, wasLegacy, err)
			}
			writeReplayConsumptionJournal = previous
			result, err = ReplayJournalWithOptions(context.Background(), path, opt)
			if err != nil || result == nil || result.Deleted != 1 {
				t.Fatalf("remaining parent did not replay: result=%+v err=%v", result, err)
			}
			calls, err = os.ReadFile(logPath)
			if err != nil || strings.Count(string(calls), "delete") != 2 || strings.Count(string(calls), strings.TrimPrefix(child.Teardown, "ecctl ")) != 1 || strings.Count(string(calls), strings.TrimPrefix(parent.Teardown, "ecctl ")) != 1 {
				t.Fatalf("replay repeated child or skipped parent: calls=%q err=%v", calls, err)
			}
			stored, wasLegacy, err = readReplayJournal(path)
			if err != nil || wasLegacy != legacy || len(stored.Entries) != 0 {
				t.Fatalf("replay did not consume remaining parent: journal=%+v legacy=%t err=%v", stored, wasLegacy, err)
			}
		})
	}
}

func TestReplayRejectsUnownedQuotaDeleteBeforeSubprocess(t *testing.T) {
	for _, teardown := range []string{
		"ecctl sandbox quota delete foreign-team --output=json",
		"ecctl sbx quota delete foreign-team --output=json",
		"ecctl --region cn-hangzhou sandbox quota delete foreign-team --output=json",
	} {
		t.Run(teardown, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cleanup.json")
			fake := filepath.Join(dir, "must-not-launch")
			journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
				Entries: []report.Resource{{Scope: "case", Teardown: teardown, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}}}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
			if err != nil || result.Errors != 1 || result.Deleted != 0 {
				t.Fatalf("unowned replay result=%+v err=%v", result, err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(body) {
				t.Fatalf("unowned replay changed journal: before=%s after=%s", body, after)
			}
		})
	}
}

func TestReplayRejectsPlainReadParentChildWithoutCreationProvenance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "must-not-launch")
	entries := []report.Resource{
		{Scope: "ordinary-read", Teardown: "ecctl sandbox team delete foreign-team --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
		{Scope: "ordinary-read", Teardown: "ecctl sandbox quota delete foreign-team --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
	}
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: entries}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result.Errors != 1 || result.Deleted != 0 {
		t.Fatalf("plain read replay result=%+v err=%v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(body) {
		t.Fatalf("plain read journal was consumed: before=%s after=%s", body, after)
	}
}

func TestReplayOrdinaryTemplateIDAndProfileDoNotBecomeQuotaDelete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		Entries: []report.Resource{{Scope: "template", Teardown: "ecctl sandbox template delete quota --profile remove", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result.Errors != 0 || result.Deleted != 1 {
		t.Fatalf("ordinary Template replay result=%+v err=%v", result, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "sandbox template delete quota --profile remove") {
		t.Fatalf("ordinary Template delete was not invoked: %s", calls)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("ordinary Template journal not consumed: %+v err=%v", stored.Entries, err)
	}
}

func TestReplayOrdinaryDeleteFailureStillContinues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	entries := []report.Resource{
		{Scope: "case", Teardown: "ecctl t thing delete earlier"},
		{Scope: "case", Teardown: "ecctl t thing delete later"},
	}
	data, err := json.Marshal(report.CleanupJournal{Version: 2, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$4" == "later" ]]; then exit 1; fi
echo '{"deleted":true}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	result, err := ReplayJournal(context.Background(), journalPath, execpkg.Config{Bin: fake})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	stored, _, err := readReplayJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Errors != 1 || result.Deleted != 1 || strings.TrimSpace(string(calls)) != "t thing delete later\nt thing delete earlier" || len(stored.Entries) != 1 || stored.Entries[0] != entries[1] {
		t.Fatalf("ordinary failure continuation changed: result=%+v calls=%q entries=%+v", result, calls, stored.Entries)
	}
}

func TestReplayRejectsCopiedOwnedTeamJournalAtDifferentPath(t *testing.T) {
	dir := canonicalJournalTestDir(t)
	pathA := filepath.Join(dir, "original.json")
	pathB := filepath.Join(dir, "copy.json")
	fake := filepath.Join(dir, "must-not-launch")
	journal := report.CleanupJournal{Version: 2, JournalPath: pathA, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		Entries: []report.Resource{{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true}}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pathA, pathB} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journal.Entries = append(journal.Entries, report.Resource{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true, QuotaUpdatePending: true})
	pending, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathA, pending, 0o600); err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"}
	if _, err := ReplayJournalWithOptions(context.Background(), pathB, opt); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("copied Team-only journal replayed at a different path: %v", err)
	}
	if result, err := ReplayJournalWithOptions(context.Background(), pathA, opt); err == nil && (result == nil || result.Errors == 0) {
		t.Fatalf("pending Quota update did not block original journal: result=%+v err=%v", result, err)
	}
}

func TestReplayOrdinaryTeamAfterDifferentOwnedQuota(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, QuotaBinding: quotaReplayBinding(t, dir),
		Entries: []report.Resource{
			{Scope: "stack", Teardown: "ecctl sandbox team delete stack-team", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
			{Scope: "stack", Teardown: "ecctl t thing delete ordinary --profile other", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
			{Scope: "case", Teardown: "ecctl sandbox team delete quota-team --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true},
			{Scope: "case", Teardown: "ecctl sandbox quota delete quota-team --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true},
		}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
echo "$*" >> "$FAKE_LOG"
echo '{"deleted":true}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result.Errors != 0 || result.Deleted != 4 {
		t.Fatalf("mixed Team replay failed: result=%+v err=%v", result, err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("mixed Team replay retained entries: journal=%+v err=%v", stored, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(calls), "sandbox team delete stack-team") || !strings.Contains(string(calls), "t thing delete ordinary --profile other") || strings.Contains(string(calls), "t thing delete ordinary --profile quota-test") {
		t.Fatalf("ordinary Team finalizer did not run: calls=%q err=%v", calls, err)
	}
}

func TestReplayQuotaTeamAfterChildConsumedKeepsCreationProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("identity probe fixture uses a shell script")
	}
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint("principal-drift=", drift), func(t *testing.T) {
			dir := canonicalJournalTestDir(t)
			path := filepath.Join(dir, "cleanup.json")
			bin := filepath.Join(dir, "ecctl")
			calls := filepath.Join(dir, "calls.log")
			t.Setenv("FAKE_QUOTA_REPLAY_CALLS", calls)
			user := "quota-user"
			if drift {
				user = "other-user"
			}
			t.Setenv("FAKE_QUOTA_REPLAY_USER", user)
			t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-config.json"))
			aliyun := filepath.Join(dir, "aliyun.json")
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyun)
			for _, name := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
				t.Setenv(name, "")
			}
			if err := os.WriteFile(aliyun, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"AK","access_key_id":"public-a"},{"name":"profile-b","mode":"AK","access_key_id":"public-b"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			binding := quotaReplayBinding(t, dir)
			if binding.ConfigState["ecctl"].Exists {
				t.Fatal("replay fixture did not capture absent native config")
			}
			if err := os.WriteFile(filepath.Join(dir, "missing-config.json"), []byte(`{"current":"profile-b","profiles":[{"name":"default","output_format":"json"},{"name":"profile-b","output_format":"json"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, QuotaBinding: binding, Entries: []report.Resource{
				{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, QuotaBound: true},
			}}
			body, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "$FAKE_QUOTA_REPLAY_USER"; exit 0;;
esac
echo "$*" >> "$FAKE_QUOTA_REPLAY_CALLS"
exit 4
`), 0o755); err != nil {
				t.Fatal(err)
			}
			result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: bin, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
			if drift {
				if err == nil || result == nil || result.Deleted != 0 {
					t.Fatalf("principal drift reached Team delete: result=%+v err=%v", result, err)
				}
				if after, readErr := os.ReadFile(path); readErr != nil || !slices.Equal(after, body) {
					t.Fatalf("principal drift changed journal: %v", readErr)
				}
				if _, readErr := os.Stat(calls); !os.IsNotExist(readErr) {
					t.Fatalf("principal drift launched provider: %v", readErr)
				}
				return
			}
			if err != nil || result.Errors != 0 || result.Deleted != 1 {
				t.Fatalf("Quota Team replay failed: result=%+v err=%v", result, err)
			}
			argv, readErr := os.ReadFile(calls)
			if readErr != nil || !strings.Contains(string(argv), "sandbox team delete team-owned --timeout 5m --region cn-hangzhou --profile quota-test") {
				t.Fatalf("Quota Team replay lost creation profile: calls=%q err=%v", argv, readErr)
			}
		})
	}
}

func TestReplayDoesNotRunOrdinaryDuplicateAfterConfirmedTeamConsumed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := canonicalJournalTestDir(t)
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		Entries: []report.Resource{
			{Scope: "ordinary", Teardown: "ecctl sandbox team delete same-team", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
			{Scope: "owned", Teardown: "ecctl sandbox team delete same-team --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true},
		}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err == nil || !strings.Contains(err.Error(), "ordinary finalizer") || result == nil || result.Deleted != 0 {
		t.Fatalf("ordinary duplicate replay result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("ordinary duplicate ran subprocess: %v", err)
	}
}

func TestReplayRejectsOrdinaryTeamWithUnclassifiedTargetGrammar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "must-not-launch")
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		Entries: []report.Resource{{Scope: "stack", Teardown: "ecctl --region cn-beijing sandbox team delete stack-team", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
	if err != nil || result == nil || result.Errors != 1 || result.Deleted != 0 {
		t.Fatalf("unclassified ordinary Team replay result=%+v err=%v", result, err)
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 1 {
		t.Fatalf("unclassified Team journal changed: journal=%+v err=%v", stored, err)
	}
}

func TestReplayMixedOrdinaryLegacyTeamJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	entries := []report.Resource{
		{Scope: "stack", Teardown: "ecctl t thing delete ordinary"},
		{Scope: "stack", Teardown: "ecctl sandbox team delete legacy-team --timeout 5m --region cn-hangzhou"},
	}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	result, err := ReplayJournal(context.Background(), path, execpkg.Config{Bin: fake, Region: "cn-hangzhou"})
	if err != nil || result == nil || result.Errors != 0 || result.Deleted != 2 {
		t.Fatalf("ordinary legacy Team replay failed: result=%+v err=%v", result, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(calls), "sandbox team delete legacy-team --timeout 5m --region cn-hangzhou") || !strings.Contains(string(calls), "t thing delete ordinary") {
		t.Fatalf("mixed legacy replay calls=%q err=%v", calls, err)
	}
	stored, legacy, err := readReplayJournal(path)
	if err != nil || !legacy || len(stored.Entries) != 0 {
		t.Fatalf("legacy journal was not consumed: journal=%+v legacy=%t err=%v", stored, legacy, err)
	}
}

func TestReplayLegacyTeamRejectsConflictingExplicitRegion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	entries := []report.Resource{{Scope: "stack", Teardown: "ecctl sandbox team delete legacy-team --timeout 5m --region cn-beijing"}}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ReplayJournal(context.Background(), path, execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"})
	if err != nil || result == nil || result.Errors != 1 || result.Deleted != 0 {
		t.Fatalf("conflicting legacy Team region reached provider: result=%+v err=%v", result, err)
	}
	if after, err := os.ReadFile(path); err != nil || !slices.Equal(after, body) {
		t.Fatalf("rejected legacy journal changed: %v", err)
	}
}

func TestReplayRejectsProtectedAndConflictingLegacyTeams(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []report.Resource
	}{
		{"protected", []report.Resource{{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", TeamCreateConfirmed: true}}},
		{"same-target conflict", []report.Resource{
			{Scope: "stack", Teardown: "ecctl sandbox team delete team-owned"},
			{Scope: "case", Teardown: "ecctl sbx team delete team-owned --timeout 5m"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cleanup.json")
			fake := filepath.Join(dir, "must-not-launch")
			body, err := json.Marshal(tc.entries)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := ReplayJournal(context.Background(), path, execpkg.Config{Bin: fake, Region: "cn-hangzhou"})
			if tc.name == "protected" {
				if err == nil || result != nil {
					t.Fatalf("protected legacy Team replayed: result=%+v err=%v", result, err)
				}
			} else if err != nil || result == nil || result.Errors != 1 || result.Deleted != 0 {
				t.Fatalf("conflicting legacy Teams replayed: result=%+v err=%v", result, err)
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(body) {
				t.Fatalf("rejected legacy journal changed: err=%v after=%s", err, after)
			}
		})
	}
}
