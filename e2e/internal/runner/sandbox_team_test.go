package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestRunTeamCreateJournalsIdentityBeforeLaterReadbackFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *" team create "* ]]; then
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--name" ]]; then name="$2"; break; fi
    shift
  done
  printf '{"team":{"id":"created-team-1","name":"%s","status":"active","read_only":false,"description":"dial tcp: readback unavailable"},"actions":[{"action_name":"CreateTeam","request_id":"req-create"}]}\n' "$name"
  exit 0
fi
if [[ ! -f "$FAKE_GET_MARKER" ]]; then
  : > "$FAKE_GET_MARKER"
  echo '{"error":{"code":"CloudAPIError","message":"dial tcp: readback unavailable"}}'
  exit 1
fi
echo '{"error":{"code":"CloudAPIError","message":"readback rejected"}}'
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	casesDir := filepath.Join(dir, "cases", "sandbox")
	if err := os.MkdirAll(casesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "team-lifecycle.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_GET_MARKER", filepath.Join(dir, "get-attempted"))
	journal := filepath.Join(dir, "cleanup-journal.json")
	run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunName: "team-repair", RunID: "test", ExecutionID: "team-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Keep: true, CleanupJournal: journal, StepTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Failed != 1 {
		t.Fatalf("expected later GetTeam failure: %+v", run.Summary)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "team create ") != 1 || strings.Count(string(calls), "team get created-team-1") != 2 {
		t.Fatalf("successful create replayed on readback failure: %s", calls)
	}
	entries := readJournalEntries(t, journal)
	if len(entries) != 1 || entries[0].Teardown != "ecctl sandbox team delete created-team-1 --timeout 5m" {
		t.Fatalf("known Team identity not journaled: %+v", entries)
	}
	data, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	var metadata report.CleanupJournal
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.RunID != "test" || metadata.ExecutionID != "team-execution" || metadata.Surface != "public" || metadata.Region != "cn-hangzhou" || metadata.EcctlBin != fake {
		t.Fatalf("cleanup journal lost run/binary/surface/region binding: %+v", metadata)
	}
}
