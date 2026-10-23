package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/caselint"
	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/match"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/surface"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

func TestSandboxQuotaCaseReservesTeamAndReadsAfterDelete(t *testing.T) {
	suite, err := scenario.Load(filepath.Join("testdata", "quota-lifecycle.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Resource != "sandbox/quota" || len(suite.Steps) != 7 {
		t.Fatalf("quota lifecycle steps = %+v", suite.Steps)
	}
	create := suite.Steps[0]
	data := map[string]any{}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	command, err := vars.Render(create.Run, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "--plan eco") {
		t.Fatalf("owned Team create must select the provider plan: %q", command)
	}
	dir := canonicalCleanupTestDir(t)
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, filepath.Join(dir, "journal.json"), report.CleanupJournal{RunID: "quota-run", ExecutionID: "quota-execution", Surface: "public"}, func(string, ...any) {})
	intent, err := reserveTeamCreate(command, data, cfg, cl, create)
	if err != nil {
		t.Fatalf("quota Team create cannot reserve recovery before cloud call: %v", err)
	}
	body, err := os.ReadFile(intent.path)
	if err != nil {
		t.Fatalf("Team recovery intent was not persisted: %v", err)
	}
	var persisted teamCreateIntent
	if err := json.Unmarshal(body, &persisted); err != nil || !slices.Equal(persisted.Input, intent.Input) || !strings.Contains(strings.Join(persisted.Input, " "), "--plan eco") {
		t.Fatalf("Team recovery intent lost original plan input: %+v, %v", persisted.Input, err)
	}
	data["team_id"] = "team-owned"
	if got, err := renderTeamRecoveryDelete(create, data); err != nil || got != "ecctl sandbox team delete team-owned --timeout 5m" {
		t.Fatalf("Team recovery delete = %q, %v", got, err)
	}
	list := suite.Steps[3]
	if list.Name != "list" || list.Run != "ecctl sandbox quota list --limit 100 --all --output=json" {
		t.Fatalf("quota case does not traverse all token pages: %+v", list)
	}
	expect, assertions, err := renderExpectations(list.Expect, list.Assert, data)
	if err != nil {
		t.Fatal(err)
	}
	other := map[string]any{"id": "other-team"}
	owned := map[string]any{"id": "team-owned"}
	for _, tc := range []struct {
		name   string
		quotas []any
		more   bool
		want   bool
	}{
		{"owned quota on later page", []any{other, owned}, false, true},
		{"unrelated quota only", []any{other}, false, false},
		{"unfinished token chain", []any{other, owned}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks, ok := match.Step(map[string]any{"quotas": tc.quotas, "pagination": map[string]any{"has_more": tc.more, "returned": len(tc.quotas), "limit": 100}}, list.At, expect, assertions)
			if ok != tc.want {
				t.Fatalf("quota case assertion accepted=%t, want %t; checks=%+v expressions=%q", ok, tc.want, checks, assertions)
			}
		})
	}

	if suite.Steps[4].Name != "delete" || suite.Steps[6].Name != "delete owned Team" {
		t.Fatalf("quota and Team deletion order changed: %+v", suite.Steps)
	}
	post := suite.Steps[5]
	if post.Name != "get after delete" || post.Exit == nil || *post.Exit != 4 || post.Run != "ecctl sandbox quota get {{.team_id}} --output=json" {
		t.Fatalf("post-delete server read is missing: %+v", post)
	}
	want := map[string]string{"$.error.kind": "not_found", "$.error.code": "NotFound", "$.actions[0].action_name": "DescribeQuota"}
	for _, expectation := range post.Expect {
		if expected, exists := want[expectation.Path]; exists {
			if !expectation.Matcher.HasEq || expectation.Matcher.Eq != expected {
				t.Fatalf("post-delete %s assertion = %+v", expectation.Path, expectation.Matcher)
			}
			delete(want, expectation.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("post-delete absence assertions missing: %+v", want)
	}
	for _, path := range []string{"$.actions[0].code", "$.actions[0].request_id"} {
		found := false
		for _, expectation := range post.Expect {
			if expectation.Path == path && expectation.Matcher.Type == "string" && expectation.Matcher.MinLen != nil && *expectation.Matcher.MinLen == 1 {
				found = true
			}
		}
		if !found {
			t.Fatalf("post-delete %s must capture nonempty provider evidence", path)
		}
	}
}

func TestSandboxQuotaLiveCaseRequiresTwoPageTokenChain(t *testing.T) {
	suite, err := scenario.Load("../../cases/sandbox/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if suite.Resource != "sandbox/quota" || !suite.Serial || len(suite.Steps) != 14 {
		t.Fatalf("live quota lifecycle changed: %+v", suite)
	}
	if !isTeamCreate(suite.Steps[0].Run) || !isTeamCreate(suite.Steps[2].Run) {
		t.Fatal("owned Team creates would not rotate the second ownership nonce")
	}
	first, second, all := suite.Steps[4], suite.Steps[5], suite.Steps[6]
	if first.Name != "first page" || first.Run != "ecctl sandbox quota list --limit 1 --output=json" ||
		first.Capture["quota_next_token"] != "$.pagination.next_token" || first.Capture["first_page_id"] != "$.quotas[0].id" ||
		second.Name != "second page from captured token" || second.Run != "ecctl sandbox quota list --limit 1 --next-token {{.quota_next_token}} --output=json" ||
		all.Name != "all pages contain both owned Quotas" || all.Run != "ecctl sandbox quota list --limit 1 --all --output=json" {
		t.Fatalf("live token chain changed: first=%+v second=%+v all=%+v", first, second, all)
	}
	for _, index := range []int{8, 12} {
		step := suite.Steps[index]
		if step.Exit == nil || *step.Exit != 4 || step.Expect[0].Path != "$.error.kind" {
			t.Fatalf("post-delete server absence missing at step %d: %+v", index, step)
		}
	}
	if suite.Steps[7].Name != "delete second Quota" || suite.Steps[9].Name != "delete second owned Team" ||
		suite.Steps[11].Name != "delete first Quota" || suite.Steps[13].Name != "delete first owned Team" {
		t.Fatalf("owned cleanup order changed: %+v", suite.Steps)
	}
	for _, index := range []int{7, 9, 11, 13} {
		if !strings.Contains(suite.Steps[index].Run, "{{.team_id}}") || strings.Contains(suite.Steps[index].Run, "page_id") {
			t.Fatalf("cleanup step %d can target a listed foreign Quota: %+v", index, suite.Steps[index])
		}
	}
	data := map[string]any{
		"first_team_id": "owned-first", "second_team_id": "owned-second",
		"first_page_id": "foreign-first",
	}
	secondExpect, secondAssertions, err := renderExpectations(second.Expect, second.Assert, data)
	if err != nil {
		t.Fatal(err)
	}
	secondPage := map[string]any{
		"quotas":     []any{map[string]any{"id": "foreign-second"}},
		"pagination": map[string]any{"limit": 1, "returned": 1, "has_more": true, "next_token": "opaque-third-page"},
	}
	if checks, ok := match.Step(secondPage, second.At, secondExpect, secondAssertions); !ok {
		t.Fatalf("valid second page with further foreign records rejected: %+v", checks)
	}
	allExpect, allAssertions, err := renderExpectations(all.Expect, all.Assert, data)
	if err != nil {
		t.Fatal(err)
	}
	allPages := map[string]any{
		"quotas": []any{map[string]any{"id": "foreign-first"}, map[string]any{"id": "foreign-second"},
			map[string]any{"id": "owned-first"}, map[string]any{"id": "owned-second"}},
		"pagination": map[string]any{"limit": 1, "returned": 4, "has_more": false},
	}
	if checks, ok := match.Step(allPages, all.At, allExpect, allAssertions); !ok {
		t.Fatalf("terminal list with foreign Quotas rejected: %+v", checks)
	}
	allPages["quotas"] = []any{map[string]any{"id": "foreign-first"}, map[string]any{"id": "foreign-second"}, map[string]any{"id": "owned-first"}}
	allPages["pagination"].(map[string]any)["returned"] = 3
	if checks, ok := match.Step(allPages, all.At, allExpect, allAssertions); ok {
		t.Fatalf("list without second owned Quota accepted: %+v", checks)
	}
}

func TestSandboxQuotaExplicitTeamDeleteConsumesCleanupJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configDir := filepath.Join(dir, ".ecctl")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"OAuth","oauth_site_type":"International","oauth_generation":"dummy","oauth_account_id":"1234567890123456"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "aliyun.json"))
	t.Setenv("ECCTL_PROFILE", "quota-test")
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"OAuth"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
echo "$*" >> "$FAKE_LOG"
while [[ $# -gt 0 && "$1" == --* ]]; do
  case "$1" in
    --output=json|--agent-envelope=false)
      shift
      ;;
    --region)
      if [[ $# -lt 2 || "$2" != "cn-hangzhou" ]]; then
        echo '{"error":{"message":"unexpected region"}}'
        exit 1
      fi
      shift 2
      ;;
    *)
      echo '{"error":{"message":"unexpected leading flag"}}'
      exit 1
      ;;
  esac
done
case "$1 $2 $3" in
  "sandbox team create")
    echo '{"team":{"id":"team-owned","status":"active"}}'
    ;;
  "sandbox quota update"|"sandbox quota get")
    if [[ "$1 $2 $3" == "sandbox quota get" && -e "$FAKE_QUOTA_DELETED" ]]; then
      echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"DescribeQuota","code":"QuotaNotFound","request_id":"req-post"}]}'
      exit 4
    fi
    echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}'
    ;;
  "sandbox quota list")
    echo '{"quotas":[{"id":"team-owned"}],"pagination":{"has_more":false,"limit":100,"returned":1}}'
    ;;
  "sandbox quota delete")
    touch "$FAKE_QUOTA_DELETED"
    echo '{"deleted":true}'
    ;;
  "sandbox team delete")
    echo '{"deleted":true}'
    ;;
  *)
    echo '{"error":{"message":"unexpected command"}}'
    exit 1
    ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	caseBody, err := os.ReadFile("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := filepath.Join(dir, "cases", "sandbox")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, "quota-lifecycle.yaml"), caseBody, 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	t.Setenv("FAKE_QUOTA_DELETED", filepath.Join(dir, "quota-deleted"))
	journal := filepath.Join(dir, "cleanup-journal.json")
	run, err := Run(context.Background(), Options{
		CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"),
		RunName: "quota-cleanup", RunID: "quota-run", ExecutionID: "quota-execution",
		Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		CleanupJournal: journal, StepTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Summary.Passed != 1 || len(run.Cases) != 1 || len(run.Cases[0].Steps) != 7 {
		t.Fatalf("quota case did not complete: %+v", run)
	}
	journalBody, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	var retained report.CleanupJournal
	if err := json.Unmarshal(journalBody, &retained); err != nil || retained.QuotaBinding == nil || retained.QuotaBinding.Profile != "quota-test" || retained.QuotaBinding.Identity.AccountID != "1234567890123456" {
		t.Fatalf("owned Quota credential binding was not retained: %+v %v", retained.QuotaBinding, err)
	}
	if state := retained.QuotaBinding.ConfigState["ecctl"]; state.SensitiveFields != nil || state.AuthSelectors["oauth_generation"] != "dummy" {
		t.Fatalf("native OAuth journal round trip changed empty secret metadata: %+v", state)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	protectedCreates := 0
	for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if strings.Contains(call, "sandbox team create ") {
			protectedCreates++
			if !strings.HasPrefix(call, "--output=json --agent-envelope=false --region cn-hangzhou sandbox team create ") {
				t.Fatalf("Team create bypassed protected command: %q", call)
			}
		}
	}
	if protectedCreates != 1 {
		t.Fatalf("protected Team create ran %d times; calls:\n%s", protectedCreates, calls)
	}
	const teamDelete = "sandbox team delete team-owned --timeout 5m --region cn-hangzhou --profile quota-test"
	teamDeletes := 0
	for _, call := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if strings.Contains(call, "sandbox team delete ") {
			teamDeletes++
			if call != teamDelete {
				t.Fatalf("Team delete differs from registered teardown: %q", call)
			}
		}
	}
	if teamDeletes != 1 {
		t.Fatalf("Team delete ran %d times; calls:\n%s", teamDeletes, calls)
	}
	if entries := readJournalEntries(t, journal); len(entries) != 0 {
		t.Fatalf("explicit Team and Quota deletes did not retire journal entries: %+v", entries)
	}
	// A new process with the same run signature must refuse the retained
	// binding before an ordinary provider call can create another Team.
	beforeCalls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ordinary := &scenario.Suite{Resource: "sandbox/team", Serial: true, Steps: []scenario.Step{{Name: "create ordinary Team", Run: `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}"`, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}}}
	_, err = Run(context.Background(), Options{Suites: []*scenario.Suite{ordinary}, RunName: "quota-cleanup", RunID: "quota-run", ExecutionID: "quota-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, StepTimeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "Quota credential binding is not owned") {
		t.Fatalf("fresh ordinary run was not refused before allocation: %v", err)
	}
	afterCalls, err := os.ReadFile(logPath)
	if err != nil || string(afterCalls) != string(beforeCalls) {
		t.Fatalf("fresh ordinary run called provider: before=%q after=%q err=%v", beforeCalls, afterCalls, err)
	}
}

func TestMixedTeamQuotaKeepsLaterOrdinaryCleanupProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_PROFILE", "quota-test")
	configDir := filepath.Join(dir, ".ecctl")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"current":"quota-test","profiles":[{"name":"quota-test","mode":"OAuth","oauth_site_type":"International","oauth_generation":"dummy","oauth_account_id":"1234567890123456"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "aliyun.json"))
	fake := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"OAuth"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"sandbox team create"*) echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox quota update"*) echo '{"quota":{"id":"team-owned"}}';;
  *"sandbox quota delete"*|*"sandbox team delete"*|*"t thing delete"*) echo '{"deleted":true}';;
  *"t thing create"*) echo '{"id":"ordinary"}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	suite := &scenario.Suite{Resource: "sandbox/team", Covers: []string{"sandbox/quota"}, Serial: true, Steps: []scenario.Step{
		{Name: "create Team", Run: `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}" --output=json`, At: "$.team", Capture: map[string]string{"team_id": "id"}, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"},
		{Name: "update Quota", Run: "ecctl sandbox quota update {{.team_id}} --cpu-cores 32 --memory-gb 64 --output=json", Teardown: "ecctl sandbox quota delete {{.team_id}} --output=json"},
		{Name: "create ordinary", Run: "ecctl t thing create --profile other", Teardown: "ecctl t thing delete ordinary --profile other"},
	}}
	journalPath := filepath.Join(dir, "journal.json")
	run, err := Run(context.Background(), Options{Suites: []*scenario.Suite{suite}, RunID: "mixed-quota", ExecutionID: "mixed-execution", RunName: "mixed-quota", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journalPath, StepTimeout: 5 * time.Second})
	if err != nil || run.Summary.Passed != 1 {
		t.Fatalf("mixed owned lifecycle failed: run=%+v err=%v", run, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(calls), "t thing delete ordinary --profile other") || strings.Contains(string(calls), "t thing delete ordinary --profile quota-test") {
		t.Fatalf("ordinary cleanup lost its authored profile: %q err=%v", calls, err)
	}
	if entries := readJournalEntries(t, journalPath); len(entries) != 0 {
		t.Fatalf("mixed cleanup retained entries: %+v", entries)
	}
}

func TestRunPendingQuotaStopsLaterOrdinaryAllocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, mode := range []string{"unknown", "settled"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			configureQuotaProfileFixture(t, dir)
			fake := filepath.Join(dir, "ecctl")
			script := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
printf '%s\n' "$*" >> "$FAKE_LOG"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output=*|--agent-envelope=*) shift ;;
    --region) shift 2 ;;
    *) break ;;
  esac
done
case "$1 $2 $3" in
  "sandbox team create") echo '{"team":{"id":"team-owned","status":"active"}}' ;;
  "sandbox quota update")
    if [ "$FAKE_QUOTA_RESULT" = unknown ]; then
      echo 'connection reset by peer' >&2
      exit 1
    fi
    echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}'
    ;;
  "sandbox quota delete"|"sandbox team delete"|"vpc delete "*) echo '{"deleted":true}' ;;
  "vpc create "*) echo '{"vpc":{"id":"vpc-owned"}}' ;;
  *) echo '{"error":{"message":"unexpected command"}}'; exit 1 ;;
esac
`
			if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_LOG", filepath.Join(dir, "calls"))
			t.Setenv("FAKE_QUOTA_RESULT", mode)
			quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			quota.Steps = quota.Steps[:2]
			vpc := &scenario.Suite{Resource: "vpc/vpc", Steps: []scenario.Step{{
				Name: "create", Run: "ecctl vpc create --name mixed-case", At: "$.vpc",
				Capture: map[string]string{"vpc_id": "id"}, Teardown: "ecctl vpc delete {{.vpc_id}}",
			}}}
			journal := filepath.Join(dir, "cleanup.json")
			run, err := Run(context.Background(), Options{RunName: "mixed", RunID: "run", ExecutionID: "execution",
				Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal,
				InputsDir: filepath.Join(dir, "inputs"), StepTimeout: 5 * time.Second, Suites: []*scenario.Suite{quota, vpc}})
			if err != nil {
				t.Fatal(err)
			}
			var quotaCase, vpcCase *report.Case
			for index := range run.Cases {
				switch run.Cases[index].Resource {
				case "sandbox/quota":
					quotaCase = &run.Cases[index]
				case "vpc/vpc":
					vpcCase = &run.Cases[index]
				}
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || quotaCase == nil || vpcCase == nil || !strings.Contains(string(calls), "sandbox team create") || !strings.Contains(string(calls), "sandbox quota update") {
				t.Fatalf("mixed run missed the quota allocation: run=%+v calls=%s err=%v", run, calls, err)
			}
			entries := readJournalEntries(t, journal)
			if mode == "unknown" {
				if quotaCase.Status == report.StatusPass || vpcCase.Status != report.StatusSkipped || len(vpcCase.Steps) != 0 || !strings.Contains(vpcCase.Error, "pending Quota update") || strings.Contains(string(calls), "vpc create") || strings.Contains(string(calls), "vpc delete") || len(entries) != 2 || !entries[0].TeamCreateConfirmed || !entries[1].QuotaUpdatePending {
					t.Fatalf("uncertain quota stranded a later VPC: run=%+v calls=%s entries=%+v", run, calls, entries)
				}
			} else if quotaCase.Status != report.StatusPass || vpcCase.Status != report.StatusPass || !strings.Contains(string(calls), "vpc create") || !strings.Contains(string(calls), "vpc delete") || len(entries) != 0 {
				t.Fatalf("settled quota blocked an ordinary lifecycle: run=%+v calls=%s entries=%+v", run, calls, entries)
			}
		})
	}
}

func TestRunPublicRawDeleteTeamOutputFlagCannotSkipOwnedQuotaChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	configureQuotaProfileFixture(t, dir)
	cases := filepath.Join(dir, "cases", "sandbox")
	inputs := filepath.Join(dir, "inputs")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, found := strings.Cut(string(body), "  - name: get\n")
	if !found {
		t.Fatal("quota lifecycle prerequisite changed")
	}
	casePath := filepath.Join(cases, "quota-lifecycle.yaml")
	caseBody := prefix + `  - name: raw delete owned Team
    run: >
      ecctl call -o json fcsandbox DeleteTeam
      --request '{"teamID":"{{.team_id}}"}'
`
	if err := os.WriteFile(casePath, []byte(caseBody), 0o600); err != nil {
		t.Fatal(err)
	}
	lint, err := caselint.Check(caselint.Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: inputs})
	if err != nil || lint.Invalid != 0 {
		t.Fatalf("public raw call failed caselint admission: %+v %v", lint, err)
	}
	suite, err := scenario.Load(casePath)
	if err != nil {
		t.Fatal(err)
	}
	suite.Surface = scenario.SurfacePublic
	caps := surface.Capabilities{Products: []surface.Product{{Name: "sandbox", Resources: []surface.Resource{{Name: "team", Actions: []string{"create", "delete"}}, {Name: "quota", Actions: []string{"update", "delete"}}}}}}
	if issues := surface.ValidateSuites([]*scenario.Suite{suite}, caps); len(issues) != 0 {
		t.Fatalf("public raw call failed capability admission: %+v", issues)
	}
	fake := writeMixedQuotaFake(t, dir)
	logPath := filepath.Join(dir, "calls")
	t.Setenv("FAKE_LOG", logPath)
	t.Setenv("FAKE_QUOTA_RESULT", "settled")
	journal := filepath.Join(dir, "cleanup.json")
	run, err := Run(context.Background(), Options{RunName: "raw-quota", RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, Keep: true, InputsDir: inputs, StepTimeout: 5 * time.Second, Suites: []*scenario.Suite{suite}})
	if err != nil || len(run.Cases) == 0 {
		t.Fatalf("public run failed setup: %+v %v", run, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := readJournalEntries(t, journal)
	if run.Cases[0].Status == report.StatusPass || len(run.Cases[0].Steps) != 3 || !strings.Contains(run.Cases[0].Steps[2].Error, "Quota child") || strings.Contains(string(calls), "call -o json fcsandbox DeleteTeam") || len(entries) != 2 || !entries[0].TeamCreateConfirmed || entries[0].TeamReplayAttempted || !entries[1].QuotaReserved || entries[1].QuotaUpdatePending {
		t.Fatalf("public raw delete bypassed owner: run=%+v calls=%s entries=%+v", run, calls, entries)
	}
}

func TestPublicRawQuotaMutationRejectedBeforeProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, command := range []string{
		`ecctl call fcsandbox UpdateQuota --request '{"tagValue":"foreign-team"}' --region cn-hangzhou --output=json`,
		`ecctl --region cn-hangzhou call -o json FCSandbox DeleteQuota --request '{"tagValue":"foreign-team"}'`,
		`ecctl call --future-flag value sbx --request '{}' DeleteQuota`,
	} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			cases := filepath.Join(dir, "cases", "sandbox")
			if err := os.MkdirAll(cases, 0o755); err != nil {
				t.Fatal(err)
			}
			casePath := filepath.Join(cases, "quota-lifecycle.yaml")
			if err := os.WriteFile(casePath, []byte("resource: sandbox/quota\nsteps:\n  - name: raw quota\n    run: >\n      "+command+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			lint, err := caselint.Check(caselint.Options{CasesDir: filepath.Join(dir, "cases")})
			found := false
			if err == nil {
				for _, issue := range lint.Errors {
					found = found || issue.Code == "unsupported_raw_quota_mutation"
				}
			}
			if err != nil || !found {
				t.Fatalf("public case admitted raw Quota mutation: lint=%+v err=%v", lint, err)
			}
			suite, err := scenario.Load(casePath)
			if err != nil {
				t.Fatal(err)
			}
			suite.Surface = scenario.SurfacePublic
			caps := surface.Capabilities{Products: []surface.Product{{Name: "sandbox", Resources: []surface.Resource{{Name: "quota", Actions: []string{"update", "delete"}}}}}}
			issues := surface.ValidateSuites([]*scenario.Suite{suite}, caps)
			if len(issues) != 1 || issues[0].Code != "unsupported_raw_quota_mutation" {
				t.Fatalf("public surface admitted raw Quota mutation: %+v", issues)
			}
			bin := filepath.Join(dir, "ecctl")
			calls := filepath.Join(dir, "provider-calls")
			t.Setenv("FAKE_LOG", calls)
			if err := os.WriteFile(bin, []byte("#!/bin/sh\necho called >> \"$FAKE_LOG\"\nexit 98\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			run, err := Run(context.Background(), Options{Suites: []*scenario.Suite{suite}, RunName: "raw-quota", RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, CleanupJournal: filepath.Join(dir, "cleanup.json"), StepTimeout: 5 * time.Second})
			if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || run.Cases[0].Status != report.StatusError || !strings.Contains(run.Cases[0].Error, "raw FCSandbox Quota mutation") {
				t.Fatalf("runner admitted raw Quota mutation: run=%+v err=%v", run, err)
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("raw Quota mutation reached CLI: %v", err)
			}
		})
	}
}

func TestRunRejectsConcurrentQuotaLifecycleBeforeAllocation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		serial bool
		mode   scenario.ExecutionMode
	}{{"parallel suite", false, scenario.ExecutionSequential}, {"DAG steps", true, scenario.ExecutionDAG}} {
		t.Run(tc.name, func(t *testing.T) {
			quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			quota.Serial, quota.Execution = tc.serial, tc.mode
			run, err := Run(context.Background(), Options{Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(t.TempDir(), "must-not-run"), Suites: []*scenario.Suite{quota}})
			if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "serial sequential") {
				t.Fatalf("concurrent quota lifecycle reached allocation: run=%+v err=%v", run, err)
			}
		})
	}
}

func TestRunRejectsRenderedQuotaUpdateInDAG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	cases := filepath.Join(dir, "cases", "sandbox")
	inputs := filepath.Join(dir, "inputs")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inputs, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both public entry checks accept this raw template; surface capability
	// matching uses the sandbox default-resource fallback. The runner must
	// authorize the rendered action.
	caseBody := `resource: sandbox/sandbox
serial: true
execution: dag
steps:
  - name: templated quota update
    run: ecctl sandbox {{.inputs.resource}} update team-owned --cpu-cores 32 --memory-gb 64 --output=json
    teardown: ecctl sandbox {{.inputs.resource}} delete team-owned --output=json
`
	if err := os.WriteFile(filepath.Join(cases, "templated.yaml"), []byte(caseBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inputs, "sandbox-sandbox.yaml"), []byte("resource: quota\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check, err := caselint.Check(caselint.Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: inputs})
	if err != nil || check.Invalid != 0 {
		t.Fatalf("templated command did not reach the runner through caselint: report=%+v err=%v", check, err)
	}
	loaded, err := scenario.Load(filepath.Join(cases, "templated.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	caps := surface.Capabilities{Products: []surface.Product{{Name: "sandbox", Resources: []surface.Resource{{Name: "sandbox", Actions: []string{"update", "delete"}}}}}}
	if issues := surface.ValidateSuites([]*scenario.Suite{loaded}, caps); len(issues) != 0 {
		t.Fatalf("templated command did not reach the runner through capabilities: %+v", issues)
	}

	quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	quota.Execution = scenario.ExecutionDAG
	quota.Steps = quota.Steps[:2]
	quota.Steps[1].DependsOn = []string{quota.Steps[0].Name}
	quota.Steps[1].Run = "ecctl sandbox {{.inputs.resource}} update {{.team_id}} --cpu-cores 32 --memory-gb 64 --output=json"
	quota.Steps[1].Teardown = "ecctl sandbox {{.inputs.resource}} delete {{.team_id}} --output=json"
	quota.Steps = append(quota.Steps, scenario.Step{
		Name: "create VPC", DependsOn: []string{quota.Steps[0].Name},
		Run: "ecctl vpc create --name mixed-case", At: "$.vpc", Capture: map[string]string{"vpc_id": "id"},
		Teardown: "ecctl vpc delete {{.vpc_id}}",
	})
	if err := os.WriteFile(filepath.Join(inputs, "sandbox-quota.yaml"), []byte("resource: quota\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := writeMixedQuotaFake(t, dir)
	logPath := filepath.Join(dir, "calls")
	t.Setenv("FAKE_LOG", logPath)
	t.Setenv("FAKE_QUOTA_RESULT", "settled")
	journal := filepath.Join(dir, "cleanup.json")
	run, err := Run(context.Background(), Options{RunName: "mixed-dag", RunID: "run", ExecutionID: "execution",
		Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal,
		InputsDir: inputs, StepTimeout: 5 * time.Second, Parallel: 2, Suites: []*scenario.Suite{quota}})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 3 || run.Cases[0].Steps[1].Status != report.StatusError ||
		!strings.Contains(run.Cases[0].Steps[1].Error, "not identifiable before its Team allocation") ||
		!strings.Contains(string(calls), "sandbox team create") || !strings.Contains(string(calls), "vpc create") ||
		!strings.Contains(string(calls), "vpc delete") || strings.Contains(string(calls), "sandbox quota update") ||
		len(readJournalEntries(t, journal)) != 0 {
		t.Fatalf("rendered quota update escaped DAG authorization: run=%+v calls=%s journal=%+v", run, calls, readJournalEntries(t, journal))
	}
}

func TestRunClassifiesTemplatedRunBeforeLiteralQuotaTeardown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, tc := range []struct {
		name, resource, action string
		dry                    bool
	}{
		{"owned update", "quota", "update", false},
		{"owned update dry-run", "quota", "update", true},
		{"ordinary action", "team", "get", false},
		{"ordinary action dry-run", "team", "get", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			inputs := filepath.Join(dir, "inputs")
			if err := os.MkdirAll(inputs, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(inputs, "sandbox-quota.yaml"), []byte("resource: "+tc.resource+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			quota.Steps = quota.Steps[:2]
			quota.Steps[1].Run = "ecctl sandbox {{.inputs.resource}} " + tc.action + " {{.team_id}} --cpu-cores 32 --memory-gb 64 --output=json"
			if tc.action == "get" {
				quota.Steps[1].Run = "ecctl sandbox {{.inputs.resource}} get {{.team_id}} --output=json"
			}
			if quota.Steps[1].Teardown != "ecctl sandbox quota delete {{.team_id}} --output=json" {
				t.Fatalf("expected a literal Quota teardown, got %q", quota.Steps[1].Teardown)
			}
			fake := writeMixedQuotaFake(t, dir)
			logPath := filepath.Join(dir, "calls")
			t.Setenv("FAKE_LOG", logPath)
			run, err := Run(context.Background(), Options{RunName: "templated-quota", RunID: "run", ExecutionID: "execution",
				Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: filepath.Join(dir, "cleanup.json"),
				InputsDir: inputs, StepTimeout: 5 * time.Second, Suites: []*scenario.Suite{quota}, DryRun: tc.dry})
			if err != nil || run == nil || len(run.Cases) != 1 || len(run.Cases[0].Steps) != 2 {
				t.Fatalf("templated Run failed before rendered admission: run=%+v err=%v", run, err)
			}
			step := run.Cases[0].Steps[1]
			calls, readErr := os.ReadFile(logPath)
			if tc.dry && !os.IsNotExist(readErr) {
				t.Fatalf("dry-run reached provider: calls=%s err=%v", calls, readErr)
			}
			if !tc.dry && readErr != nil {
				t.Fatal(readErr)
			}
			if tc.action == "update" {
				if tc.dry {
					if step.Status == report.StatusError || !strings.Contains(step.Command, "sandbox quota update") {
						t.Fatalf("dry-run did not render Quota update: %+v", step)
					}
				} else if step.Status != report.StatusError || !strings.Contains(step.Error, "not identifiable before its Team allocation") || strings.Contains(string(calls), "sandbox quota update") || len(readJournalEntries(t, filepath.Join(dir, "cleanup.json"))) != 0 {
					t.Fatalf("unclassified Quota update reached mutation or retained cleanup: step=%+v calls=%s", step, calls)
				}
			} else if step.Status != report.StatusError || !strings.Contains(step.Error, "ordinary teardown") || strings.Contains(string(calls), "sandbox team get") {
				t.Fatalf("rendered ordinary action reached provider or escaped rejection: step=%+v calls=%s", step, calls)
			}
		})
	}
}

func TestRunRejectsLiteralOrdinaryRunWithQuotaTeardownBeforeAllocation(t *testing.T) {
	quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	quota.Steps = quota.Steps[:2]
	quota.Steps[1].Run = "ecctl sandbox team get {{.team_id}} --output=json"
	run, err := Run(context.Background(), Options{Region: "cn-hangzhou", Surface: "public", Suites: []*scenario.Suite{quota}})
	if err == nil || run == nil || len(run.Cases) != 1 || run.Cases[0].Resource != "setup" || !strings.Contains(run.Cases[0].Error, "ordinary teardown") {
		t.Fatalf("definite ordinary Run reached allocation: run=%+v err=%v", run, err)
	}
}

func TestQuotaBindingAcceptsInferredAKModeFromSelectedProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(config, []byte(`{"profiles":[{"name":"owned","access_key_id":"public-dummy","access_key_secret":"dummy-private","region_id":"cn-hangzhou"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "native.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", config)
	t.Setenv("ECCTL_PROFILE", "owned")
	for _, key := range []string{"ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH"} {
		t.Setenv(key, "")
	}
	logPath := filepath.Join(dir, "probes")
	t.Setenv("FAKE_MODE_CALLS", logPath)
	bin := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_MODE_CALLS"
case "$*" in
  *"configure get"*) printf '{"profile":"owned","mode":"%s"}\n' "${FAKE_MODE_VALUE:-}";;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}';;
  *) exit 91;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	binding, _, err := captureQuotaCredentialBinding(context.Background(), cfg, time.Second)
	if err != nil || binding == nil || binding.Profile != "owned" || binding.Mode != "AK" || !binding.ResolvedMode {
		t.Fatalf("mode-omitted configured profile was rejected: binding=%+v err=%v", binding, err)
	}
	if _, err := journalfile.ProbeQuotaBinding(context.Background(), binding, cfg); err != nil {
		t.Fatalf("mode-omitted profile failed protected probe: %v", err)
	}
	t.Setenv("FAKE_MODE_VALUE", "StsToken")
	if _, err := journalfile.ProbeQuotaBinding(context.Background(), binding, cfg); err != nil {
		t.Fatalf("configure display mode overrode unchanged effective AK mode: %v", err)
	}
	t.Setenv("FAKE_MODE_VALUE", "")
	before, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"profiles":[{"name":"owned","access_key_id":"public-dummy","access_key_secret":""}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := journalfile.ProbeQuotaBinding(context.Background(), binding, cfg); err == nil {
		t.Fatal("changed inferred credential source was accepted")
	}
	after, err := os.ReadFile(logPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("credential probe launched before inferred-mode rejection: before=%q after=%q err=%v", before, after, err)
	}
}

func TestRunExpectedNonzeroQuotaCannotContinueAllocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a shell script")
	}
	for _, mode := range []string{"nonzero", "settled"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			configureQuotaProfileFixture(t, dir)
			fake := writeMixedQuotaFake(t, dir)
			logPath := filepath.Join(dir, "calls")
			t.Setenv("FAKE_LOG", logPath)
			t.Setenv("FAKE_QUOTA_RESULT", mode)
			quota, err := scenario.Load("testdata/quota-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			quota.Steps = quota.Steps[:2]
			quota.Steps[1].Expect = nil
			if mode == "nonzero" {
				one := 1
				quota.Steps[1].Exit = &one
			}
			quota.Steps = append(quota.Steps, scenario.Step{
				Name: "create VPC", Run: "ecctl vpc create --name later-case", At: "$.vpc",
				Capture: map[string]string{"vpc_id": "id"}, Teardown: "ecctl vpc delete {{.vpc_id}}",
			})
			journal := filepath.Join(dir, "cleanup.json")
			run, err := Run(context.Background(), Options{RunName: "nonzero-quota", RunID: "run", ExecutionID: "execution",
				Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal,
				InputsDir: filepath.Join(dir, "inputs"), StepTimeout: 5 * time.Second, Suites: []*scenario.Suite{quota}})
			if err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			entries := readJournalEntries(t, journal)
			if mode == "nonzero" {
				if len(run.Cases) != 2 || run.Cases[0].Resource != "sandbox/quota" || len(run.Cases[0].Steps) != 2 || run.Cases[0].Steps[1].Status != report.StatusError ||
					!strings.Contains(run.Cases[0].Steps[1].Error, "remains pending") || !strings.Contains(run.Cases[0].Steps[1].Error, "Transient") ||
					run.Cases[1].Name != "(deferred case cleanup)" || run.Cases[1].Resource != "cleanup" || run.Cases[1].Status != report.StatusFail ||
					!strings.Contains(run.Cases[1].Error, "pending Quota update") || run.Summary.Failed != 2 ||
					!strings.Contains(string(calls), "sandbox quota update") || strings.Contains(string(calls), "vpc create") || strings.Contains(string(calls), "vpc delete") ||
					len(entries) != 2 || !entries[0].TeamCreateConfirmed || !entries[1].QuotaReserved || !entries[1].QuotaUpdatePending {
					t.Fatalf("nonzero Quota result escaped its pending cleanup fence: run=%+v calls=%s entries=%+v", run, calls, entries)
				}
			} else if len(run.Cases) != 1 || run.Cases[0].Status != report.StatusPass || len(run.Cases[0].Steps) != 3 ||
				!strings.Contains(string(calls), "vpc create") || !strings.Contains(string(calls), "vpc delete") || len(entries) != 0 {
				t.Fatalf("settled quota blocked later allocation: run=%+v calls=%s entries=%+v", run, calls, entries)
			}
		})
	}
}

func writeMixedQuotaFake(t *testing.T, dir string) string {
	t.Helper()
	t.Setenv("HOME", dir)
	fake := filepath.Join(dir, "ecctl")
	script := `#!/bin/sh
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
printf '%s\n' "$*" >> "$FAKE_LOG"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output=*|--agent-envelope=*) shift ;;
    --region) shift 2 ;;
    *) break ;;
  esac
done
case "$1 $2 $3" in
  "sandbox team create") echo '{"team":{"id":"team-owned","status":"active"}}' ;;
  "sandbox quota update")
    if [ "$FAKE_QUOTA_RESULT" = nonzero ]; then
      echo '{"error":{"code":"Transient"}}'
      exit 1
    fi
    echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}'
    ;;
  "sandbox quota delete"|"sandbox team delete"|"vpc delete "*) echo '{"deleted":true}' ;;
  "vpc create "*) echo '{"vpc":{"id":"vpc-owned"}}' ;;
  *) echo '{"error":{"message":"unexpected command"}}'; exit 1 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestRunLaterTeamReaderCannotBypassRetainedQuotaChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses bash")
	}
	dir := t.TempDir()
	configureQuotaProfileFixture(t, dir)
	fake := filepath.Join(dir, "ecctl")
	script := `#!/usr/bin/env bash
case "$*" in
  *"configure get"*) echo '{"profile":"quota-test","mode":"AK"}'; exit 0;;
  *"call sts GetCallerIdentity"*) echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"quota-user"}}'; exit 0;;
esac
echo "$*" >> "$FAKE_LOG"
case "$*" in
  *"sandbox team create"*) echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox quota update"*) echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}';;
  *"sandbox quota get"*) echo '{"quota":{"id":"team-owned","team_id":"team-owned","cpu_cores":32,"memory_gb":64}}';;
  *"sandbox quota list"*) echo '{"quotas":[{"id":"team-owned"}],"pagination":{"has_more":false,"limit":100,"returned":1}}';;
  *"sandbox quota delete"*) echo '{"error":{"kind":"client","code":"Denied","message":"retained child"}}'; exit 1;;
  *"sandbox team get"*) echo '{"team":{"id":"team-owned","status":"active"}}';;
  *"sandbox team delete"*) echo '{"deleted":true}';;
  *) echo '{"error":{"message":"unexpected command"}}'; exit 1;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := filepath.Join(dir, "cases", "sandbox")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	quota, err := os.ReadFile("testdata/quota-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, "a-quota.yaml"), quota, 0o600); err != nil {
		t.Fatal(err)
	}
	reader := `resource: sandbox/team
serial: true
steps:
  - name: read retained Team
    run: ecctl sandbox team get team-owned --output=json
    at: $.team
    capture: {team_id: id}
    teardown: ecctl sandbox team delete {{.team_id}} --timeout 1m
`
	if err := os.WriteFile(filepath.Join(cases, "b-team-reader.yaml"), []byte(reader), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath, journal := filepath.Join(dir, "calls"), filepath.Join(dir, "journal.json")
	t.Setenv("FAKE_LOG", logPath)
	run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"),
		RunName: "retained", RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake,
		CleanupJournal: journal, StepTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Cases) != 2 || run.Cases[0].Status == report.StatusPass || run.Cases[1].Status == report.StatusPass || len(run.Cases[1].Steps) != 1 || !strings.Contains(run.Cases[1].Steps[0].Error, "Team ownership") {
		t.Fatalf("later reader bypassed retained parent: %+v", run.Cases)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "sandbox team get team-owned") || strings.Contains(string(calls), "sandbox team delete team-owned") {
		t.Fatalf("later case launched protected Team delete: %s", calls)
	}
	entries := readJournalEntries(t, journal)
	if len(entries) != 2 || !entries[0].TeamCreateConfirmed || !entries[1].QuotaReserved {
		t.Fatalf("failed Quota cleanup lost parent/child evidence: %+v", entries)
	}
}

func TestExistingTemplateCacheLifecycleConsumesOwnedTeamWithJSONFlag(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, false)
	bin := cfg.Bin
	script := `#!/usr/bin/env bash
echo "$*" >> "$FAKE_NATIVE_DIR/full-calls"
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}'; exit 0; fi
if [[ "$*" == *"configure get"* ]]; then echo '{"profile":"account-a","mode":"AK"}'; exit 0; fi
if [[ "$*" == *"team create"* ]]; then echo '{"team":{"id":"team-owned","status":"active"}}'; exit 0; fi
if [[ "$*" == *"template-cache create"* ]]; then echo '{"template_cache":{"id":"tpl-1","team_id":"team-owned","status":"Success"}}'; exit 0; fi
if [[ "$*" == *"template-cache get"* ]]; then echo '{"template_cache":{"id":"tpl-1","team_id":"team-owned","status":"Success"}}'; exit 0; fi
if [[ "$*" == *"template-cache list"* ]]; then echo '{"template_caches":[{"id":"tpl-1","team_id":"team-owned","status":"Success"}]}'; exit 0; fi
if [[ "$*" == *"template-cache delete"* ]]; then echo '{"deleted":true}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then echo '{"template":{"id":"tpl-1","team_id":"team-owned","build_status":"ready"}}'; exit 0; fi
if [[ "$*" == *"template get"* ]]; then echo '{"template":{"id":"tpl-1","team_id":"team-owned","build_status":"ready"}}'; exit 0; fi
if [[ "$*" == *"template delete"* ]]; then echo '{"deleted":true}'; exit 0; fi
if [[ "$*" == *"team delete"* ]]; then echo '{"deleted":true}'; exit 0; fi
echo '{"error":{"code":"UnexpectedCommand"}}'; exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	suite, err := scenario.Load("../../cases/sandbox/template-cache-lifecycle.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "runtime.json")
	if err := os.WriteFile(config, []byte(`{"language":"python"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"resource_prefix": "owned", "prerequisites": map[string]any{"sandbox": map[string]any{"template": map[string]any{"runtime_config_file": config}}}}
	opt := Options{RunName: "template-cache", InputsDir: filepath.Join(dir, "inputs"), StepTimeout: 5 * time.Second, Logf: func(string, ...any) {}}
	result := runCase(context.Background(), opt, cfg, cl, cl.operations, base, nil, nil, suite)
	if result.Status != report.StatusPass || len(result.Steps) != len(suite.Steps) || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("existing Template Cache lifecycle failed at owned Team delete: %+v journal=%+v", result, readJournalEntries(t, cl.journal))
	}
	calls, err := os.ReadFile(filepath.Join(dir, "full-calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "sandbox team delete team-owned --timeout 5m --output=json") != 1 {
		t.Fatalf("existing final Team command was not executed exactly once: %s", calls)
	}
}
