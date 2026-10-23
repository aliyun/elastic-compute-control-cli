package sweeper

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
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

type cacheReconcileFixture struct {
	journal, nativeIntent, calls string
	opt                          TemplateCacheReconcileOptions
}

func reconcileDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func newCacheReconcileFixture(t *testing.T) cacheReconcileFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ecctl uses a POSIX shell")
	}
	dir := t.TempDir()
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	calls := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_RECONCILE_CALLS", calls)
	journalBin := filepath.Join(dir, "journal-ecctl")
	probeBin := filepath.Join(dir, "probe-ecctl")
	script := `#!/bin/sh
printf '%s %s\n' "${0##*/}" "$*" >> "$FAKE_RECONCILE_CALLS"
case "$*" in
  *"call sts GetCallerIdentity"*)
    if [ -z "$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" ]; then exit 2; fi
    printf '{"response":{"AccountId":"%s","IdentityType":"RAMUser","UserId":"user-a"}}\n' "${FAKE_IDENTITY_ACCOUNT:-1234567890123456}"
    ;;
  *"sandbox template-cache get"*)
    if [ -z "$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" ]; then exit 2; fi
    if [ -n "$FAKE_RECONCILE_MUTATE_JOURNAL" ]; then printf '\n' >> "$FAKE_RECONCILE_MUTATE_JOURNAL"; fi
    if [ -n "$FAKE_RECONCILE_MUTATE_BIN" ]; then printf '# changed\n' >> "$FAKE_RECONCILE_MUTATE_BIN"; fi
    if [ -n "$FAKE_CACHE_BODY" ]; then printf '%s\n' "$FAKE_CACHE_BODY"; exit 0; fi
    if [ "$FAKE_CACHE_EXIT" = 4 ]; then exit 4; fi
    printf '{"template_cache":{"id":"%s","template_id":"%s","team_id":"%s","status":"%s"}}\n' "${FAKE_CACHE_ID:-tpl-1}" "${FAKE_CACHE_ID:-tpl-1}" "${FAKE_CACHE_TEAM:-team-1}" "${FAKE_CACHE_STATUS:-Success}"
    ;;
  *" delete "*) printf '{"deleted":true}\n' ;;
  *) exit 2 ;;
esac
`
	for _, path := range []string{journalBin, probeBin} {
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	journal := filepath.Join(dir, "cleanup.json")
	intentPath := journal + ".template-create-0123456789abcdef01234567.json"
	exit := 0
	paths, err := journalfile.CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	intent := journalfile.TemplateCreateIntent{
		Version: 2, RunID: "run-1", ExecutionID: "execution-1", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: journalBin,
		TeamID: "team-1", Name: "ecctl-p-0123456789abcdef01234567", Profile: "account-a", Selectors: map[string]string{}, ConfigPaths: paths,
		Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}, CreatedAt: time.Now().UTC(),
		State: "journaled", Exit: &exit,
		Deletes: []string{"ecctl sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1 --profile account-a"},
	}
	for _, name := range journalfile.TemplateSelectorNames {
		intent.Selectors[name] = journalfile.TemplateEnvironmentValue(name, nil)
	}
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WriteExclusiveDurable(intentPath, body); err != nil {
		t.Fatal(err)
	}
	meta := report.CleanupJournal{
		Version: 2, RunID: intent.RunID, ExecutionID: intent.ExecutionID, RegionRole: intent.RegionRole, Region: intent.Region, Surface: intent.Surface, EcctlBin: journalBin,
		NativeJournalPath: journal, NativeIntentPaths: []string{intentPath},
		Entries: []report.Resource{
			{Scope: "case", Teardown: "ecctl sandbox team delete team-1 --region cn-hangzhou", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: intent.ExecutionID},
			{Scope: "case", Teardown: intent.Deletes[0], RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: intent.ExecutionID},
			{Scope: "case", Teardown: "ecctl sandbox template-cache delete tpl-1 --team-id team-1 --region cn-hangzhou --profile account-a --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: intent.ExecutionID, CacheCreatePending: true},
		},
	}
	body, err = json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	if err := journalfile.WriteDurable(journal, body); err != nil {
		t.Fatal(err)
	}
	return cacheReconcileFixture{journal: journal, nativeIntent: intentPath, calls: calls, opt: TemplateCacheReconcileOptions{
		ReplayOptions: ReplayOptions{Config: execpkg.Config{Bin: journalBin, Region: "cn-hangzhou"}, RunID: "run-1", Surface: "public"},
		JournalSHA256: reconcileDigest(body), JournalBinSHA256: reconcileDigest([]byte(script)), ProbeBin: probeBin, ProbeBinSHA256: reconcileDigest([]byte(script)), Profile: "account-a",
	}}
}

func readCacheReconcileJournal(t *testing.T, path string) report.CleanupJournal {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestReconcilePendingTemplateCacheProvesTerminalStateBeforeReplay(t *testing.T) {
	for _, status := range []string{"Success", "Failed", "Evicted"} {
		t.Run(status, func(t *testing.T) {
			fixture := newCacheReconcileFixture(t)
			t.Setenv("FAKE_CACHE_STATUS", status)
			if _, err := ReplayJournalWithOptions(context.Background(), fixture.journal, fixture.opt.ReplayOptions); !errors.Is(err, journalfile.ErrTemplateCacheCreatePending) {
				t.Fatalf("pending cache replay was not fenced: %v", err)
			}
			result, err := ReconcilePendingTemplateCache(context.Background(), fixture.journal, fixture.opt)
			if err != nil || result == nil || result.Status != status || result.TemplateID != "tpl-1" || result.TeamID != "team-1" {
				t.Fatalf("terminal cache reconciliation: result=%+v err=%v", result, err)
			}
			entries := readCacheReconcileJournal(t, fixture.journal).Entries
			if len(entries) != 3 || entries[2].CacheCreatePending {
				t.Fatalf("terminal cache did not settle exactly one finalizer: %+v", entries)
			}
			info, err := os.Stat(result.EvidencePath)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("reconciliation evidence is not private: %v %+v", err, info)
			}
			calls, err := os.ReadFile(fixture.calls)
			if err != nil || strings.Contains(string(calls), " delete ") {
				t.Fatalf("reconciliation ran a delete: %s %v", calls, err)
			}
			replayed, err := ReplayJournalWithOptions(context.Background(), fixture.journal, fixture.opt.ReplayOptions)
			if err != nil || replayed.Deleted != 3 || len(readCacheReconcileJournal(t, fixture.journal).Entries) != 0 {
				t.Fatalf("inverse replay after reconciliation: %+v %v", replayed, err)
			}
			calls, _ = os.ReadFile(fixture.calls)
			cacheAt, templateAt, teamAt := strings.Index(string(calls), "template-cache delete"), strings.Index(string(calls), "template delete"), strings.Index(string(calls), "team delete")
			if cacheAt < 0 || templateAt <= cacheAt || teamAt <= templateAt {
				t.Fatalf("replay did not retain child-before-parent order: %s", calls)
			}
		})
	}
}

func TestReconcilePendingTemplateCacheRejectsUncertainOrForeignEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(t *testing.T, fixture *cacheReconcileFixture)
	}{
		{"pending", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_CACHE_STATUS", "InProgress") }},
		{"deleting", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_CACHE_STATUS", "Deleting") }},
		{"absent", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_CACHE_EXIT", "4") }},
		{"foreign ID", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_CACHE_ID", "tpl-other") }},
		{"foreign Team", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_CACHE_TEAM", "team-other") }},
		{"malformed get", func(t *testing.T, _ *cacheReconcileFixture) {
			t.Setenv("FAKE_CACHE_BODY", `{"template_cache":{"id":"tpl-1"}}`)
		}},
		{"foreign identity", func(t *testing.T, _ *cacheReconcileFixture) { t.Setenv("FAKE_IDENTITY_ACCOUNT", "9999999999999999") }},
		{"wrong run", func(_ *testing.T, fixture *cacheReconcileFixture) { fixture.opt.RunID = "other-run" }},
		{"changed journal", func(t *testing.T, fixture *cacheReconcileFixture) {
			body, err := os.ReadFile(fixture.journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.journal, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed binary", func(t *testing.T, fixture *cacheReconcileFixture) {
			file, err := os.OpenFile(fixture.opt.Config.Bin, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("# changed\n"); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{"journal changed during probe", func(t *testing.T, fixture *cacheReconcileFixture) {
			t.Setenv("FAKE_RECONCILE_MUTATE_JOURNAL", fixture.journal)
		}},
		{"binary changed during probe", func(t *testing.T, fixture *cacheReconcileFixture) {
			t.Setenv("FAKE_RECONCILE_MUTATE_BIN", fixture.opt.ProbeBin)
		}},
		{"corrupt native intent", func(t *testing.T, fixture *cacheReconcileFixture) {
			if err := os.WriteFile(fixture.nativeIntent, []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCacheReconcileFixture(t)
			tc.modify(t, &fixture)
			if _, err := ReconcilePendingTemplateCache(context.Background(), fixture.journal, fixture.opt); err == nil {
				t.Fatal("uncertain or foreign cache was reconciled")
			}
			if entries := readCacheReconcileJournal(t, fixture.journal).Entries; len(entries) != 3 || !entries[2].CacheCreatePending {
				t.Fatalf("rejected reconciliation changed the pending finalizer: %+v", entries)
			}
			if calls, err := os.ReadFile(fixture.calls); err == nil && strings.Contains(string(calls), " delete ") {
				t.Fatalf("rejected reconciliation ran a delete: %s", calls)
			}
		})
	}
}

func TestReconcilePendingTemplateCacheRetriesAfterEvidenceWrite(t *testing.T) {
	fixture := newCacheReconcileFixture(t)
	previous := reconcileTemplateCacheAfterEvidence
	reconcileTemplateCacheAfterEvidence = func(string) error { return errors.New("injected interruption") }
	t.Cleanup(func() { reconcileTemplateCacheAfterEvidence = previous })
	if _, err := ReconcilePendingTemplateCache(context.Background(), fixture.journal, fixture.opt); err == nil {
		t.Fatal("interrupted journal commit succeeded")
	}
	if entries := readCacheReconcileJournal(t, fixture.journal).Entries; !entries[2].CacheCreatePending {
		t.Fatal("interrupted reconciliation cleared pending marker")
	}
	reconcileTemplateCacheAfterEvidence = previous
	t.Setenv("FAKE_CACHE_STATUS", "Failed")
	if _, err := ReconcilePendingTemplateCache(context.Background(), fixture.journal, fixture.opt); err == nil {
		t.Fatal("a conflicting terminal status replaced durable evidence")
	}
	if entries := readCacheReconcileJournal(t, fixture.journal).Entries; !entries[2].CacheCreatePending {
		t.Fatal("conflicting evidence cleared pending marker")
	}
	t.Setenv("FAKE_CACHE_STATUS", "Success")
	if _, err := ReconcilePendingTemplateCache(context.Background(), fixture.journal, fixture.opt); err != nil {
		t.Fatalf("compatible durable evidence did not permit retry: %v", err)
	}
	if entries := readCacheReconcileJournal(t, fixture.journal).Entries; entries[2].CacheCreatePending {
		t.Fatal("retry left the pending marker")
	}
}
