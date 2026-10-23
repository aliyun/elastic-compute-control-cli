package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func canonicalCleanupTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNativeParallelCasesRetryDeferredCleanupAfterAllocation(t *testing.T) {
	for _, mode := range []string{"success", "unresolved", "principal-change"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _, dir := nativeCleanupFixture(t, false)
			t.Setenv("ECCTL_PROFILE", "account-a")
			t.Setenv("FAKE_PARALLEL_MODE", mode)
			for _, name := range []string{"a-ready", "b-started", "b-release"} {
				if output, err := exec.Command("mkfifo", "-m", "600", filepath.Join(dir, name)).CombinedOutput(); err != nil {
					t.Fatalf("create synchronization FIFO: %v: %s", err, output)
				}
			}
			script := `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s"}}\n' "${FAKE_PRINCIPAL_USER:-user-a}"; exit 0; fi
if [[ "$*" == *"configure get"* ]]; then echo '{"profile":"account-a","mode":"AK"}'; exit 0; fi
team=''; id=''; profile=account-a; prev=''
for arg in "$@"; do
 [[ "$prev" == --team-id ]] && team="$arg"
 [[ "$prev" == --profile ]] && profile="$arg"
 [[ "$arg" == --team-id=* ]] && team="${arg#--team-id=}"
 [[ "$arg" == --profile=* ]] && profile="${arg#--profile=}"
 [[ "$arg" == tpl-a || "$arg" == tpl-b ]] && id="$arg"
 prev="$arg"
done
if [[ "$*" == *"test get"* ]]; then read -r ready < "$FAKE_NATIVE_DIR/a-ready"; echo '{}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then
 echo "create $team" >> "$FAKE_NATIVE_DIR/parallel-calls"
 if [[ "$team" == team-b ]]; then
  printf 'started\n' > "$FAKE_NATIVE_DIR/b-started"
  read -r release < "$FAKE_NATIVE_DIR/b-release"
  if [[ "$FAKE_PARALLEL_MODE" == unresolved ]]; then echo '{"error":{"code":"CloudAPIError","message":"response lost"}}'; exit 2; fi
  echo '{"template":{"id":"tpl-b","team_id":"team-b"}}'; exit 0
 fi
 echo '{"template":{"id":"tpl-a","team_id":"team-a"}}'; exit 0
fi
if [[ "$*" == *"template get"* ]]; then
 printf 'ready\n' > "$FAKE_NATIVE_DIR/a-ready"
 read -r started < "$FAKE_NATIVE_DIR/b-started"
 echo '{"template":{"id":"tpl-a"}}'; exit 0
fi
if [[ "$*" == *"template list"* ]]; then echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *"template delete"* || "$*" == *"test parent delete"* ]]; then
 [[ -z "$id" ]] && id=parent-a
 echo "delete $id profile=$profile pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/parallel-calls"
 echo '{"deleted":true}'; exit 0
fi
echo '{}'
`
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			suites := []*scenario.Suite{
				{Resource: "t/a", Steps: []scenario.Step{
					{Name: "parent", Run: "ecctl test parent create", Teardown: "ecctl test parent delete parent-a"},
					{Name: "create A", Run: "ecctl sandbox template create --openapi --team-id team-a --name first", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-a"},
					{Name: "read A", Run: "ecctl sandbox template get tpl-a --openapi --team-id team-a"}}},
				{Resource: "t/b", Steps: []scenario.Step{
					{Name: "wait for A read", Run: "ecctl test get"},
					{Name: "create B", Run: "ecctl sandbox template create --openapi --team-id team-b --name second", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-b"}}},
			}
			aDone := make(chan struct{}, 1)
			type outcome struct {
				run *report.Run
				err error
			}
			done := make(chan outcome, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			journal := filepath.Join(dir, "parallel-journal.json")
			go func() {
				run, err := Run(ctx, Options{Suites: suites, InputsDir: filepath.Join(dir, "inputs"), EcctlBin: cfg.Bin, Region: cfg.Region, Parallel: 2, StepTimeout: 5 * time.Second, RunID: "parallel-run", ExecutionID: "execution", Surface: "public", CleanupJournal: journal, Logf: func(format string, args ...any) {
					if format == "%-5s %s (%s)" && len(args) > 1 && args[1] == "t/a" {
						select {
						case aDone <- struct{}{}:
						default:
						}
					}
				}})
				done <- outcome{run, err}
			}()
			select {
			case <-aDone: // Both of A's implicit cleanup passes have returned.
			case result := <-done:
				t.Fatalf("runner ended before pending overlap: %+v %v", result.run, result.err)
			case <-ctx.Done():
				t.Fatal("first case did not complete while B was pending")
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "parallel-calls"))
			if strings.Contains(string(calls), "delete ") {
				t.Fatalf("cleanup bypassed active pending allocation: %s", calls)
			}
			if mode == "principal-change" {
				t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
			}
			releaseCtx, releaseCancel := context.WithTimeout(ctx, 2*time.Second)
			output, err := exec.CommandContext(releaseCtx, "bash", "-c", `printf 'release\n' > "$1"`, "_", filepath.Join(dir, "b-release")).CombinedOutput()
			releaseCancel()
			if err != nil {
				t.Fatalf("release delayed B create: %v: %s", err, output)
			}
			var result outcome
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal("runner did not finish after releasing B")
			}
			if result.err != nil {
				t.Fatal(result.err)
			}
			calls, _ = os.ReadFile(filepath.Join(dir, "parallel-calls"))
			entries := readJournalEntries(t, journal)
			if mode == "success" {
				if result.run.Summary.Failed != 0 || result.run.Summary.Passed != 2 || len(entries) != 0 {
					t.Errorf("settled creates abandoned earlier cleanup: summary=%+v journal=%+v calls=%s", result.run.Summary, entries, calls)
				}
				for _, id := range []string{"tpl-a", "tpl-b"} {
					if strings.Count(string(calls), "delete "+id+" profile=account-a pin={") != 1 {
						t.Errorf("cleanup lost original binding or ran twice for %s: %s", id, calls)
					}
				}
				if strings.Count(string(calls), "delete parent-a profile=account-a pin=\n") != 1 {
					t.Errorf("unrelated generic parent inherited native identity or ran twice: %s", calls)
				}
				if strings.Index(string(calls), "delete tpl-a ") > strings.Index(string(calls), "delete parent-a ") || !strings.Contains(string(calls), `"UserId":"user-a"`) {
					t.Errorf("deferred cleanup lost child order or identity: %s", calls)
				}
			} else {
				wantEntries := map[string]int{"unresolved": 2, "principal-change": 3}[mode]
				if strings.Contains(string(calls), "delete ") || len(entries) != wantEntries || result.run.Summary.Failed == 0 {
					t.Errorf("unresolved/changed identity bypassed final cleanup guard: summary=%+v entries=%+v calls=%s", result.run.Summary, entries, calls)
				}
			}
		})
	}
}

func TestCleanupIndependentLongDeletesShareJournal(t *testing.T) {
	cl := newCleanup(map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-test"}}, newOperationRuntime(2), false, filepath.Join(canonicalCleanupTestDir(t), "journal.json"), report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	entered := make(chan string, 2)
	release := make(chan struct{})
	cl.runCommand = func(ctx context.Context, _ execpkg.Config, command string) execpkg.Result {
		entered <- command
		select {
		case <-release:
			return execpkg.Result{Exit: 0}
		case <-ctx.Done():
			return execpkg.Result{Exit: -1, Err: ctx.Err()}
		}
	}
	var scopes [2][]*cleanupItem
	for index := range scopes {
		if err := cl.push(&scopes[index], fmt.Sprint(index), fmt.Sprintf("ecctl test delete owned-%d", index), "primary", []string{fmt.Sprint(index)}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan []string, 2)
	var launched sync.WaitGroup
	launched.Add(2)
	for index := range scopes {
		go func(scope []*cleanupItem) { defer launched.Done(); done <- cl.run(scope) }(scopes[index])
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
		launched.Wait()
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first delete never entered")
	}
	// Cross the old 30-second lock acquisition boundary while both independent
	// deletes are active; neither may lose its cleanup claim to that timeout.
	select {
	case <-entered:
	case failures := <-done:
		t.Fatalf("independent delete exhausted journal lock: %v", failures)
	case <-time.After(33 * time.Second):
		t.Fatal("independent deletes were serialized")
	}
	select {
	case failures := <-done:
		t.Fatalf("delete finished before release: %v", failures)
	case <-time.After(31 * time.Second):
	}
	close(release)
	released = true
	for range scopes {
		if failures := <-done; len(failures) != 0 {
			t.Errorf("cleanup failed: %v", failures)
		}
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 0 {
		t.Errorf("long deletes left pending cleanup: %+v", entries)
	}
}

func TestCleanupRejectedGuardKeepsUnlaunchedFinalizer(t *testing.T) {
	dir := canonicalCleanupTestDir(t)
	path := filepath.Join(dir, "journal.json")
	cl := newCleanup(map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-test"}}, newOperationRuntime(1), false, path, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var scope []*cleanupItem
	if err := cl.push(&scope, "owned", "ecctl test delete owned", "primary", nil); err != nil {
		t.Fatal(err)
	}
	intent := path + ".template-create-corrupt.json"
	if err := os.WriteFile(intent, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result { calls++; return execpkg.Result{Exit: 0} }
	if failures := cl.run(scope); len(failures) != 1 || calls != 0 || scope[0].done {
		t.Fatalf("unlaunched finalizer was discarded: calls=%d done=%v failures=%v", calls, scope[0].done, failures)
	}
	if err := os.Remove(intent); err != nil {
		t.Fatal(err)
	}
	if failures := cl.run(scope); len(failures) != 0 || calls != 1 || len(readJournalEntries(t, path)) != 0 {
		t.Fatalf("unlaunched finalizer could not resume: calls=%d failures=%v", calls, failures)
	}
}

func TestCleanupStartFailureAndConcurrentClaim(t *testing.T) {
	cl := newCleanup(map[string]execpkg.Config{"primary": {Bin: "ecctl", Region: "cn-test"}}, newOperationRuntime(2), false, filepath.Join(canonicalCleanupTestDir(t), "journal.json"), report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var scope []*cleanupItem
	if err := cl.push(&scope, "owned", "ecctl test delete owned", "primary", nil); err != nil {
		t.Fatal(err)
	}
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
		return execpkg.Result{Exit: -1, Err: fmt.Errorf("unable to start process")}
	}
	if failures := cl.run(scope); len(failures) != 1 || scope[0].done {
		t.Fatalf("failed start lost finalizer: %v %+v", failures, scope[0])
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan []string, 1)
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
		close(entered)
		<-release
		return execpkg.Result{Exit: 0}
	}
	go func() { done <- cl.run(scope) }()
	<-entered
	if failures := cl.run(scope); len(failures) != 0 {
		close(release)
		<-done
		t.Fatalf("concurrent claim: %v", failures)
	}
	close(release)
	if failures := <-done; len(failures) != 0 || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("cleanup did not finish once: %v", failures)
	}
}

func nativeCleanupFixture(t *testing.T, keep bool) (execpkg.Config, *cleanup, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := canonicalCleanupTestDir(t)
	for _, key := range []string{"ECCTL_PROFILE", "ALIBABACLOUD_PROFILE", "ALIBABA_CLOUD_PROFILE", "ALICLOUD_PROFILE", "ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("FAKE_NATIVE_DIR", dir)
	t.Setenv("FAKE_CONFIG_RESPONSE", "")
	t.Setenv("FAKE_CONFIG_EXIT", "")
	t.Setenv("FAKE_CACHE_CREATE_LOST_RESPONSE", "")
	t.Setenv("FAKE_CACHE_CREATE_ACCEPTED_ONLY", "")
	if err := os.WriteFile(filepath.Join(dir, "current"), []byte("account-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "ecctl")
	body := `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s","AccessKeySecret":"dummy-unused-secret"}}\n' "${FAKE_PRINCIPAL_USER:-user-a}"; exit 0; fi
profile="${ECCTL_PROFILE:-$(cat "$FAKE_NATIVE_DIR/current")}"; given=''; prev=''
for arg in "$@"; do
 [[ "$prev" == '--profile' ]] && profile="$arg" && given="$arg"
 [[ "$arg" == --profile=* ]] && profile="${arg#--profile=}" && given="$profile"
 prev="$arg"
done
if [[ "$*" == *"configure get"* ]]; then
 if [[ -n "$FAKE_CONFIG_RESPONSE" ]]; then cat "$FAKE_CONFIG_RESPONSE"; exit "${FAKE_CONFIG_EXIT:-0}"; fi
 printf '{"profile":"%s","mode":"AK"}\n' "$profile"; exit 0
fi
if [[ "$*" == *"template create"* ]]; then
 echo "create profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 echo account-b > "$FAKE_NATIVE_DIR/current"
 echo '{"template":{"id":"tpl-1","team_id":"team-1"}}'; exit 0
fi
if [[ "$*" == *"template-cache create"* || "$*" == *"tcache create"* ]]; then
 journal="$(cat "$FAKE_NATIVE_DIR/journal.json")"
 if [[ "$journal" != *"'template-cache' 'delete'"* && "$journal" != *"'tcache' 'delete'"* ]] || [[ "$profile" != account-a ]]; then
  echo 'cache-create reached cloud without exact journal or profile' >> "$FAKE_NATIVE_DIR/calls"; exit 2
 fi
 echo "cache-create journaled profile=$profile" >> "$FAKE_NATIVE_DIR/calls"
 if [[ "$FAKE_CACHE_CREATE_LOST_RESPONSE" == 1 ]]; then echo 'connection reset by peer' >&2; exit 1; fi
 if [[ "$FAKE_CACHE_CREATE_ACCEPTED_ONLY" == 1 ]]; then echo '{"template_cache":{"id":"tpl-1","team_id":"team-1","status":"InProgress"}}'; exit 0; fi
 echo '{"template_cache":{"id":"tpl-1","team_id":"team-1","status":"Success"}}'; exit 0
fi
if [[ "$*" == *"template-cache delete"* || "$*" == *"tcache delete"* ]]; then
 echo "cache-delete profile=$profile" >> "$FAKE_NATIVE_DIR/calls"
 if [[ "$FAKE_CACHE_DELETE_FAILURE" == 1 ]]; then echo 'cache delete failed' >&2; exit 2; fi
 if [[ "$profile" != account-a ]]; then exit 2; fi
 echo '{"deleted":true}'; exit 0
fi
if [[ "$*" == *"template delete"* || "$*" == *"tpl rm"* || "$*" == *"tpl remove"* ]]; then
 echo "template-delete profile=$profile flag=$given" >> "$FAKE_NATIVE_DIR/calls"
 if [[ "$profile" != account-a || -f "$FAKE_NATIVE_DIR/deleted" ]]; then echo '{"error":{"code":"UnsafeOrDuplicateDelete"}}'; exit 2; fi
 touch "$FAKE_NATIVE_DIR/deleted"; echo '{"deleted":true}'; exit 0
fi
if [[ "$*" == *"team delete"* ]]; then echo team-delete >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi
echo '{}'
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), keep, filepath.Join(dir, "journal.json"), report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	return cfg, cl, dir
}

func TestNativeCreateRejectsUnboundConfiguredProfileBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		exit       string
		ignore     bool
	}{
		{"missing", `{}`, "0", false},
		{"unnamed", `{"profile":"","mode":"AK"}`, "0", false},
		{"fallback", `{"profile":"account-a","mode":""}`, "0", false},
		{"other", `{"profile":"account-b","mode":"AK"}`, "0", false},
		{"failed", `{"error":{"code":"ProfileNotFound"}}`, "2", false},
		{"ignored", `{"profile":"account-a","mode":"AK"}`, "0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			t.Setenv("ECCTL_PROFILE", "account-a")
			path := filepath.Join(dir, "metadata.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_CONFIG_RESPONSE", path)
			t.Setenv("FAKE_CONFIG_EXIT", tc.exit)
			if tc.ignore {
				t.Setenv("ALIBABA_CLOUD_IGNORE_PROFILE", "true")
			}
			var scope []*cleanupItem
			step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --openapi --name native --team-id team-1", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			if ok || result.Command != "" || !strings.Contains(result.Error, "configured profile") {
				t.Errorf("unbound source launched creation: %+v", result)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			if len(calls) != 0 || len(paths) != 0 || len(scope) != 0 {
				t.Errorf("unbound metadata reached mutation/journaling: %s %v %+v", calls, paths, scope)
			}
		})
	}
}

func TestNativeExplicitDeleteRejectsForeignBinding(t *testing.T) {
	for _, flag := range []string{"--profile account-b", "--region other-region", "--team-id other-team", "--profile=", "missing-team"} {
		t.Run(flag, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			team := "--team-id team-1"
			if strings.HasPrefix(flag, "--team-id") {
				team = ""
			}
			if flag == "missing-team" {
				team = ""
				flag = ""
			}
			step := scenario.Step{Name: "delete", Run: "ecctl sandbox template delete tpl-1 --openapi " + team + " " + flag}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			if ok || result.Command != "" || !strings.Contains(result.Error, "conflicts") || scope[0].done || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Errorf("foreign deletion changed finalizer: %+v %+v", result, scope)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "template-delete") {
				t.Errorf("foreign delete ran: %s", calls)
			}
		})
	}
}

func nativeSuccessfulCreate(t *testing.T, cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any) {
	t.Helper()
	step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --openapi --name native --team-id team-1", At: "$.template", Capture: map[string]string{"template_id": "id"}, Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, scope, data, step, time.Second); !ok {
		t.Fatalf("create: %+v", result)
	}
}

func TestNativeSuccessfulCreatePinsConfiguredCurrentForReplay(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
	body, _ := os.ReadFile(paths[0])
	var intent map[string]any
	_ = json.Unmarshal(body, &intent)
	if intent["profile"] != "account-a" || len(scope) != 1 || !strings.Contains(scope[0].cmd, "account-a") {
		t.Errorf("successful allocation lost configured selection: %s %+v", body, scope)
	}
	t.Setenv("ECCTL_PROFILE", "account-b")
	result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	if err != nil || result.Deleted != 1 {
		t.Errorf("configured current switch changed cleanup account: %+v %v", result, err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !strings.Contains(string(calls), "create profile=account-a flag=account-a") || !strings.Contains(string(calls), "template-delete profile=account-a flag=account-a") {
		t.Errorf("create/delete binding changed: %s", calls)
	}
}

func TestLegacyNativeTeamCleanupSelectsExactJournaledPrincipal(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, false)
	var scope []*cleanupItem
	if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1 --region cn-hangzhou", "primary", nil); err != nil {
		t.Fatal(err)
	}
	if err := cl.push(&scope, "ordinary", "ecctl sandbox team delete team-2", "primary", nil); err != nil {
		t.Fatal(err)
	}
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	journal := quotaJournal(t, cl.journal)
	if len(journal.Entries) != 3 || journal.Entries[0].TeamCreateConfirmed || journal.Entries[1].TeamCreateConfirmed {
		t.Fatalf("fixture lost pre-marker parent entries: %+v", journal.Entries)
	}
	t.Setenv("ECCTL_PROFILE", "account-b")
	seen := map[string]int{}
	cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		if strings.Contains(command, "sandbox team delete team-1") {
			seen["native"]++
			if bound.Profile != "account-a" || bound.ExpectedIdentity == nil || bound.ExpectedIdentity.UserID != "user-a" {
				t.Errorf("legacy native Team used a different principal: %+v", bound)
			}
		} else if strings.Contains(command, "sandbox team delete team-2") {
			seen["ordinary"]++
			if bound.Profile != "" || bound.ExpectedIdentity != nil {
				t.Errorf("unrelated ordinary Team inherited native principal: %+v", bound)
			}
		}
		return execpkg.Run(ctx, bound, command)
	}
	if failures := cl.run(scope); len(failures) != 0 || seen["native"] != 1 || seen["ordinary"] != 1 || len(readJournalEntries(t, cl.journal)) != 0 {
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		t.Fatalf("legacy native/ordinary cleanup failed: failures=%v seen=%v calls=%s", failures, seen, calls)
	}
}

func TestExecutionLifetimeTeamFinalizerKeepsNativeTemplatePrincipal(t *testing.T) {
	cfg, _, dir := nativeCleanupFixture(t, false)
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(body), `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"team create"* ]]; then echo '{"team":{"id":"team-1"}}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then`, 1)
	if script == string(body) {
		t.Fatal("native fixture has no Template create branch to extend")
	}
	withTeamDelete := strings.Replace(script, `if [[ "$*" == *"team delete"* ]]; then echo team-delete >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi`, `if [[ "$*" == *"team delete"* ]]; then echo "team-delete profile=$profile pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi`, 1)
	if withTeamDelete == script {
		t.Fatal("native fixture has no Team delete branch to instrument")
	}
	script = withTeamDelete
	if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(dir, "stack.yaml")
	fixture := `provision:
  - id: team
    resource: sandbox/team
    lifetime: execution
    run: ecctl sandbox team create --name fixture-team
    at: $.team
    capture: {team_id: id}
    teardown: ecctl sandbox team delete {{.team_id}} --region cn-hangzhou
`
	if err := os.WriteFile(stack, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	suite := &scenario.Suite{Resource: "test/native", Serial: true, Needs: []string{"team"}, Steps: []scenario.Step{{
		Name: "create native Template", Run: "ecctl sandbox template create --openapi --team-id {{.stack.team_id}} --name native",
		At: "$.template", Capture: map[string]string{"template_id": "id"},
		Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id {{.stack.team_id}}",
	}}}
	journal := filepath.Join(dir, "execution-journal.json")
	run, err := Run(context.Background(), Options{Suites: []*scenario.Suite{suite}, StackFile: stack, RunName: "native-stack", RunID: "run", ExecutionID: "execution",
		Surface: "public", Region: cfg.Region, EcctlBin: cfg.Bin, CleanupJournal: journal, StepTimeout: time.Second})
	calls, readErr := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || run == nil || run.Summary.Failed != 0 || readErr != nil || len(readJournalEntries(t, journal)) != 0 ||
		!strings.Contains(string(calls), "template-delete profile=account-a flag=account-a") ||
		strings.Count(string(calls), "team-delete profile=account-a pin={") != 1 {
		t.Fatalf("execution-lifetime Team lost native principal: run=%+v err=%v calls=%s read=%v", run, err, calls, readErr)
	}
}

func TestNativeExplicitDeleteRetiresBoundFinalizer(t *testing.T) {
	for _, keep := range []bool{false, true} {
		for _, command := range []string{"ecctl sandbox template delete tpl-1 --openapi --team-id team-1", "ecctl --region=cn-hangzhou sbx tpl remove tpl-1 --team-id=team-1 --openapi=true"} {
			t.Run(fmt.Sprintf("%t/%s", keep, command), func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, keep)
				t.Setenv("ECCTL_PROFILE", "account-a")
				var scope []*cleanupItem
				data := map[string]any{}
				if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
					t.Fatal(err)
				}
				nativeSuccessfulCreate(t, cfg, cl, &scope, data)
				for _, step := range []scenario.Step{{Name: "delete-native", Run: command}, {Name: "delete-team", Run: "ecctl sandbox team delete team-1"}} {
					if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, step, time.Second); !ok {
						t.Fatalf("explicit deletion: %+v", result)
					}
				}
				cl.run(scope)
				if entries := readJournalEntries(t, cl.journal); len(entries) != 0 {
					t.Errorf("satisfied finalizers remain in journal: %+v", entries)
				}
				if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || result.Deleted != 0 {
					t.Errorf("satisfied journal replayed: %+v %v", result, err)
				}
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if strings.Count(string(calls), "template-delete") != 1 {
					t.Errorf("second native deletion after Team removal: %s", calls)
				}
			})
		}
	}
}
