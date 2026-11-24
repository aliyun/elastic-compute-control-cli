package sweeper

import (
	"context"
	"encoding/json"
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

func TestReplayOwnedTeamRebuildsRunIdentityAndClearsProviderTemplates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := canonicalJournalTestDir(t)
	binding := quotaReplayBinding(t, dir)
	path, bin, calls := filepath.Join(dir, "cleanup.json"), filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
	t.Setenv("FAKE_CALLS", calls)
	t.Setenv("FAKE_CREATED", filepath.Join(dir, "created-at"))
	t.Setenv("FAKE_TEMPLATE_DELETED", filepath.Join(dir, "template-deleted"))
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_CALLS"
case "$*" in *"--profile quota-test"*) ;; *) exit 90;; esac
if [ ! -f "$FAKE_CREATED" ]; then date -u +%Y-%m-%dT%H:%M:%SZ > "$FAKE_CREATED"; fi
CREATED=$(cat "$FAKE_CREATED")
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}';;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}';;
  *"sandbox team get"*) printf '{"team":{"id":"team-owned","name":"ecctl-t-0123456789abcdef01234567","description":"ecctl E2E Team owner=0123456789abcdef01234567","created_time":"%s"}}\n' "$CREATED";;
  *"sandbox template list"*)
    if [ -f "$FAKE_TEMPLATE_DELETED" ]; then echo '{"templates":[],"pagination":{"has_more":false}}'; else printf '{"templates":[{"id":"tpl-child","name":"base","team_id":"team-owned","team_name":"ecctl-t-0123456789abcdef01234567","created_time":"%s"}],"pagination":{"has_more":false}}\n' "$CREATED"; fi;;
  *"sandbox template get"*)
    if [ -f "$FAKE_TEMPLATE_DELETED" ]; then echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTemplate","code":"TemplateNotFound","request_id":"req-1"}]}'; exit 4; fi
    printf '{"template":{"id":"tpl-child","name":"base","team_id":"team-owned","team_name":"ecctl-t-0123456789abcdef01234567","created_time":"%s"}}\n' "$CREATED";;
  *"sandbox template delete"*) touch "$FAKE_TEMPLATE_DELETED"; echo '{"deleted":true,"template":{"id":"tpl-child"}}';;
  *"sandbox team delete"*) echo '{"deleted":true}';;
  *) exit 91;;
esac
`
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := journalfile.ExecutableSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	name := "ecctl-t-0123456789abcdef01234567"
	entry := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, TeamProviderTemplateCleanup: true}
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, EcctlBinSHA256: digest, Entries: []report.Resource{entry}}
	body, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := map[string]any{
		"version": 1, "run_id": "run", "execution_id": "execution", "region": "cn-hangzhou", "surface": "public", "ecctl_bin": bin,
		"name": name, "description": "ecctl E2E Team owner=" + name[8:], "input": []string{"ecctl", "sandbox", "team", "create"},
		"case_scope": "case", "teardown": "ecctl sandbox team delete intent-validation --timeout 5m --output=json",
		"owner_binding": binding, "team_provider_template_cleanup": true, "created_at": time.Now().Add(-time.Minute).UTC(),
	}
	sidecar, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WriteExclusiveDurable(path+".team-create-"+name[8:]+".json", append(sidecar, '\n')); err != nil {
		t.Fatal(err)
	}
	opt := ReplayOptions{Config: execpkg.Config{Bin: bin, ExpectedBinSHA256: digest, Region: "cn-hangzhou", Env: []string{"HOME=" + dir}}, RunID: "run", Surface: "public"}
	result, err := ReplayJournalWithOptions(context.Background(), path, opt)
	if err != nil || result == nil || result.Deleted != 1 || result.Errors != 0 {
		t.Fatalf("owned Team replay did not self-close: result=%+v err=%v", result, err)
	}
	log, _ := os.ReadFile(calls)
	for _, want := range []string{"sandbox template list", "sandbox template delete", "sandbox team delete"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("owned Team replay skipped %q: calls=%s", want, log)
		}
	}
	stored, _, err := readReplayJournal(path)
	if err != nil || len(stored.Entries) != 0 {
		t.Fatalf("owned Team replay retained its finalizer: entries=%+v err=%v", stored.Entries, err)
	}
}
