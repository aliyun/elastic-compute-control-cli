package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func ownedTeamBinaryPinSuite(resource string, providerTemplates bool) *scenario.Suite {
	return &scenario.Suite{
		Path:                     "owned-team-binary-pin.yaml",
		Resource:                 resource,
		Serial:                   true,
		CleanupProviderTemplates: providerTemplates,
		Steps: []scenario.Step{{
			Name:     "create owned Team",
			Run:      `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}" --output=json`,
			At:       "$.team",
			Capture:  map[string]string{"team_id": "id"},
			Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m --output=json",
		}},
	}
}

func writeOwnedTeamBinaryPinFake(t *testing.T, dir, bin string) {
	t.Helper()
	t.Setenv("FAKE_CALLS", filepath.Join(dir, "calls"))
	t.Setenv("FAKE_TEAM_NAME", filepath.Join(dir, "team-name"))
	t.Setenv("FAKE_TEAM_DESC", filepath.Join(dir, "team-desc"))
	t.Setenv("FAKE_CREATED", filepath.Join(dir, "created-at"))
	t.Setenv("FAKE_TEMPLATE_DELETED", filepath.Join(dir, "template-deleted"))
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_CALLS"
name=""; desc=""; prev=""
for arg in "$@"; do
  [[ "$prev" == --name ]] && name="$arg"
  [[ "$prev" == --description ]] && desc="$arg"
  prev="$arg"
done
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
if [[ "$*" == *"sandbox team create"* ]]; then
  printf '%s' "$name" > "$FAKE_TEAM_NAME"
  printf '%s' "$desc" > "$FAKE_TEAM_DESC"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$FAKE_CREATED"
  echo '{"team":{"id":"team-owned","status":"active"}}'
  exit 0
fi
CREATED=$(cat "$FAKE_CREATED" 2>/dev/null)
NAME=$(cat "$FAKE_TEAM_NAME" 2>/dev/null)
DESC=$(cat "$FAKE_TEAM_DESC" 2>/dev/null)
case "$*" in
  *"sandbox team get"*) printf '{"team":{"id":"team-owned","name":"%s","description":"%s","created_time":"%s"}}\n' "$NAME" "$DESC" "$CREATED";;
  *"sandbox template list"*)
    if [ -f "$FAKE_TEMPLATE_DELETED" ]; then echo '{"templates":[],"pagination":{"has_more":false}}'; else printf '{"templates":[{"id":"tpl-child","name":"base","team_id":"team-owned","team_name":"%s","created_time":"%s"}],"pagination":{"has_more":false}}\n' "$NAME" "$CREATED"; fi;;
  *"sandbox template get"*)
    if [ -f "$FAKE_TEMPLATE_DELETED" ]; then echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTemplate","code":"TemplateNotFound","request_id":"req-1"}]}'; exit 4; fi
    printf '{"template":{"id":"tpl-child","name":"base","team_id":"team-owned","team_name":"%s","created_time":"%s"}}\n' "$NAME" "$CREATED";;
  *"sandbox template delete"*) touch "$FAKE_TEMPLATE_DELETED"; echo '{"deleted":true,"template":{"id":"tpl-child"}}';;
  *"sandbox team delete"*)
    if [ ! -f "$FAKE_TEMPLATE_DELETED" ] && [ "$FAKE_PLAIN" != 1 ]; then exit 92; fi
    echo '{"deleted":true}';;
  *) exit 91;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func ownedTeamBinaryPinOptions(dir, bin, journal string, suite *scenario.Suite, keep bool) Options {
	return Options{
		Suites:         []*scenario.Suite{suite},
		InputsDir:      filepath.Join(dir, "inputs"),
		CleanupJournal: journal,
		RunName:        "owned-run",
		RunID:          "owned-run",
		ExecutionID:    "execution",
		Region:         "cn-hangzhou",
		Surface:        "public",
		EcctlBin:       bin,
		Parallel:       1,
		Keep:           keep,
		StepTimeout:    30 * time.Second,
	}
}

func TestRunPinsBinaryForOwnedProviderTemplateTeam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, mode := range []string{"teardown", "replay"} {
		t.Run(mode, func(t *testing.T) {
			dir := canonicalCleanupTestDir(t)
			bin := filepath.Join(dir, "ecctl")
			writeOwnedTeamBinaryPinFake(t, dir, bin)
			configureQuotaProfileFixture(t, dir)
			journalPath := filepath.Join(dir, "cleanup-journal.json")
			suite := ownedTeamBinaryPinSuite("sandbox/api-key", true)
			run, err := Run(context.Background(), ownedTeamBinaryPinOptions(dir, bin, journalPath, suite, mode == "replay"))
			if err != nil || run.Summary.Passed != 1 || run.Summary.Failed != 0 {
				t.Fatalf("owned Team run failed: summary=%+v err=%v cases=%+v", run.Summary, err, run.Cases)
			}
			digest, err := journalfile.ExecutableSHA256(bin)
			if err != nil {
				t.Fatal(err)
			}
			journal := quotaJournal(t, journalPath)
			if journal.EcctlBinSHA256 != digest {
				t.Fatalf("non-Volume owned Team journal lost its binary pin: %q want %q", journal.EcctlBinSHA256, digest)
			}
			if mode == "teardown" {
				if len(journal.Entries) != 0 {
					t.Fatalf("teardown retained its finalizer: %+v", journal.Entries)
				}
			} else {
				if len(journal.Entries) != 1 || !journal.Entries[0].TeamCreateConfirmed || !journal.Entries[0].TeamProviderTemplateCleanup {
					t.Fatalf("kept run lost its owned Team finalizer: %+v", journal.Entries)
				}
				result, err := sweeper.ReplayJournalWithOptions(context.Background(), journalPath, sweeper.ReplayOptions{
					Config:  execpkg.Config{Bin: bin, Region: "cn-hangzhou"},
					RunID:   "owned-run",
					Surface: "public",
				})
				if err != nil || result == nil || result.Deleted != 1 || result.Errors != 0 {
					t.Fatalf("replay of a runner-produced journal did not self-close: result=%+v err=%v", result, err)
				}
				if stored := quotaJournal(t, journalPath); len(stored.Entries) != 0 {
					t.Fatalf("replay retained its finalizer: %+v", stored.Entries)
				}
			}
			log, _ := os.ReadFile(filepath.Join(dir, "calls"))
			templateDelete := strings.Index(string(log), "sandbox template delete")
			teamDelete := strings.Index(string(log), "sandbox team delete")
			if templateDelete < 0 || teamDelete < 0 || templateDelete > teamDelete {
				t.Fatalf("provider Template cleanup did not precede Team delete: calls=%s", log)
			}
		})
	}
}

func TestRunKeepsPlainTeamJournalWithoutBinaryPin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	bin := filepath.Join(dir, "ecctl")
	writeOwnedTeamBinaryPinFake(t, dir, bin)
	configureQuotaProfileFixture(t, dir)
	t.Setenv("FAKE_PLAIN", "1")
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	suite := ownedTeamBinaryPinSuite("sandbox/team", false)
	run, err := Run(context.Background(), ownedTeamBinaryPinOptions(dir, bin, journalPath, suite, false))
	if err != nil || run.Summary.Passed != 1 || run.Summary.Failed != 0 {
		t.Fatalf("plain Team run failed: summary=%+v err=%v cases=%+v", run.Summary, err, run.Cases)
	}
	journal := quotaJournal(t, journalPath)
	if journal.EcctlBinSHA256 != "" || len(journal.Entries) != 0 {
		t.Fatalf("plain Team run changed its unpinned journal contract: %+v", journal)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(log), "sandbox template") || !strings.Contains(string(log), "sandbox team delete") {
		t.Fatalf("plain Team delete was not a plain delete: calls=%s", log)
	}
}

func TestRunRejectsOwnedTeamBinaryChangeAfterSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalCleanupTestDir(t)
	bin := filepath.Join(dir, "ecctl")
	writeOwnedTeamBinaryPinFake(t, dir, bin)
	configureQuotaProfileFixture(t, dir)
	journalPath := filepath.Join(dir, "cleanup-journal.json")
	suite := ownedTeamBinaryPinSuite("sandbox/api-key", true)
	opt := ownedTeamBinaryPinOptions(dir, bin, journalPath, suite, false)
	opt.EcctlBinSHA256 = strings.Repeat("0", 64)
	run, err := Run(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "cleanup executable changed after run selection") {
		t.Fatalf("changed owned Team executable was not rejected: err=%v", err)
	}
	if run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" {
		t.Fatalf("binary change was not a setup failure: %+v", run)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(statErr) {
		t.Fatalf("binary change reached the provider: stat=%v", statErr)
	}
}
