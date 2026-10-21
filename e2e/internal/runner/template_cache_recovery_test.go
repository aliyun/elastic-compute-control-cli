package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func cacheRecoverySteps() (scenario.Step, scenario.Step) {
	deleteCommand := "ecctl sandbox template-cache delete {{.native_template_id}} --team-id {{.team_id}} --timeout 5m --output=json"
	return scenario.Step{
		Name: "create cache", Run: "ecctl sandbox template-cache create --template-id {{.native_template_id}} --team-id {{.team_id}} --timeout 20m --output=json",
		Teardown: deleteCommand,
	}, scenario.Step{Name: "delete cache", Run: deleteCommand}
}

func TestTemplateCacheFailedJournalReservationDoesNotRegisterCleanup(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.json")
	if err := os.Mkdir(journal, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: filepath.Join(dir, "unused-ecctl"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, journal, report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var scope []*cleanupItem
	item, err := cl.pushPendingCacheCreate(&scope, "cache", "ecctl sandbox template-cache delete tpl-1 --team-id team-1 --timeout 5m --output=json", "primary", nil)
	if err == nil || item != nil || len(scope) != 0 || len(cl.manifest) != 0 {
		t.Fatalf("failed journal reservation registered in-memory cleanup: item=%+v err=%v scope=%+v manifest=%+v", item, err, scope, cl.manifest)
	}
	calls := 0
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
		calls++
		return execpkg.Result{Exit: 0}
	}
	if failures := cl.run(scope); len(failures) != 0 || calls != 0 {
		t.Fatalf("failed reservation launched cleanup: failures=%v calls=%d", failures, calls)
	}
}

func TestTemplateCacheReplayCannotConsumePreCreateReservation(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, _ := cacheRecoverySteps()
	command := "ecctl sandbox template-cache create --template-id tpl-1 --team-id team-1 --timeout 20m --output=json"
	if _, _, reserved, err := cl.reserveTemplateCacheCreate(context.Background(), &scope, command, cfg, data, create, nil); err != nil || reserved == nil {
		t.Fatalf("reserve before create: %v", err)
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || !entries[1].CacheCreatePending {
		t.Fatalf("reservation was not durably pending: %+v", entries)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); !errors.Is(err, journalfile.ErrTemplateCacheCreatePending) {
		t.Fatalf("replay consumed the pre-create finalizer: %v", err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "cache-delete") || strings.Contains(string(calls), "template-delete") || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("replay ran before cache create: %s", calls)
	}
}

func TestTemplateCacheCreateNoStartRetiresPendingFinalizer(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process", true: "replay"}[replay], func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			data := map[string]any{"team_id": "team-1"}
			nativeSuccessfulCreate(t, cfg, cl, &scope, data)
			data["native_template_id"] = data["template_id"]
			create, _ := cacheRecoverySteps()
			cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
				return execpkg.Result{Exit: -1, Err: errors.New("local process could not start"), Started: false}
			}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second)
			if ok || result.Exit != -1 {
				t.Fatal("no-start cache create did not fail")
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 2 || len(scope) != 3 || !scope[2].done {
				t.Fatal("no-start cache reservation was not durably retired")
			}
			cl.runCommand = execpkg.Run
			if replay {
				if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil {
					t.Fatal(err)
				}
			} else {
				cl.keep = false
				if failures := cl.run(scope); len(failures) != 0 {
					t.Fatal(failures)
				}
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil || strings.Contains(string(calls), "cache-delete") || !strings.Contains(string(calls), "template-delete") || !strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Fatal("parent cleanup remained fenced after proven no-start")
			}
		})
	}
}

func TestTemplateCacheReplayRejectsChangedTarget(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, _ := cacheRecoverySteps()
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
		t.Fatalf("cache create: %+v", result)
	}
	body, err := os.ReadFile(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		t.Fatal(err)
	}
	journal.Entries[1].Teardown = strings.Replace(journal.Entries[1].Teardown, "tpl-1", "tpl-other", 1)
	body, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cl.journal, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err == nil {
		t.Fatal("replayed a Template Cache delete for an unowned target")
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "cache-delete") || strings.Contains(string(calls), "template-delete") {
		t.Fatalf("replay executed a delete before ownership validation: %s", calls)
	}
}

func TestTemplateCacheCreateJournalsBeforeLaunchAndRetiresAfterDelete(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, remove := cacheRecoverySteps()
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		journal, _ := os.ReadFile(cl.journal)
		t.Fatalf("cache create: %+v calls=%s journal=%s", result, calls, journal)
	}
	entries := readJournalEntries(t, cl.journal)
	if len(entries) != 2 || !strings.Contains(entries[1].Teardown, "'template-cache' 'delete'") || !strings.Contains(entries[1].Teardown, "--profile") || !strings.Contains(entries[1].Teardown, "--region") {
		t.Fatalf("pre-create cache finalizer missing or unbound: %+v", entries)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !strings.Contains(string(calls), "cache-create journaled profile=account-a") {
		t.Fatalf("cache create ran before durable journal or under wrong profile: %s", calls)
	}
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, remove, 5*time.Second); !ok {
		t.Fatalf("cache delete: %+v", result)
	}
	entries = readJournalEntries(t, cl.journal)
	if len(entries) != 1 || !scope[1].done || strings.Contains(entries[0].Teardown, "template-cache") {
		t.Fatalf("confirmed absence did not retire cache finalizer: %+v scope=%+v", entries, scope)
	}
}

func TestTemplateCacheCreateRefusesUnownedOrUnjournaledTarget(t *testing.T) {
	for _, tc := range []struct {
		name, command, teardown string
		breakJournal            bool
	}{
		{"foreign template", "ecctl sandbox template-cache create --template-id tpl-other --team-id team-1 --output=json", "", false},
		{"foreign teardown", "", "ecctl sandbox template-cache delete tpl-other --team-id team-1 --output=json", false},
		{"invalid timeout", "", "ecctl sbx tcache delete tpl-1 --team-id team-1 --timeout 0 --output=json", false},
		{"empty timeout", "", "ecctl sbx tcache delete tpl-1 --team-id team-1 --timeout= --output=json", false},
		{"journal conflict", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			data := map[string]any{"team_id": "team-1"}
			nativeSuccessfulCreate(t, cfg, cl, &scope, data)
			data["native_template_id"] = data["template_id"]
			create, _ := cacheRecoverySteps()
			if tc.command != "" {
				create.Run = tc.command
			}
			if tc.teardown != "" {
				create.Teardown = tc.teardown
			}
			if tc.breakJournal {
				cl.journalMeta.RunID = "another-run"
			}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if ok || result.Command != "" || strings.Contains(string(calls), "cache-create") || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Fatalf("unsafe cache create reached cloud or journal: result=%+v calls=%s", result, calls)
			}
		})
	}
}

func TestTemplateCacheAliasDefaultTimeoutDeleteRetiresAndReplays(t *testing.T) {
	for _, mode := range []string{"explicit", "replay"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			data := map[string]any{"team_id": "team-1"}
			nativeSuccessfulCreate(t, cfg, cl, &scope, data)
			data["native_template_id"] = data["template_id"]
			create, remove := cacheRecoverySteps()
			aliasDelete := "ecctl sbx tcache delete {{.native_template_id}} --team-id {{.team_id}} --output=json"
			create.Teardown, remove.Run = aliasDelete, aliasDelete
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
				t.Fatalf("alias cache create: %+v", result)
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 2 || !strings.Contains(entries[1].Teardown, "'sbx' 'tcache' 'delete'") || strings.Contains(entries[1].Teardown, "--timeout") {
				t.Fatalf("alias default-timeout delete was not reserved: %+v", entries)
			}
			if mode == "explicit" {
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, remove, 5*time.Second); !ok {
					t.Fatalf("alias cache delete: %+v", result)
				}
				if entries := readJournalEntries(t, cl.journal); len(entries) != 1 || !scope[1].done {
					t.Fatalf("alias delete did not retire its finalizer: %+v", entries)
				}
				return
			}
			if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil {
				t.Fatalf("alias cache replay: %v", err)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			cacheAt, templateAt := strings.Index(string(calls), "cache-delete"), strings.Index(string(calls), "template-delete")
			if cacheAt < 0 || templateAt < 0 || cacheAt > templateAt || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Fatalf("alias recovery violated cache-before-template order: %s", calls)
			}
		})
	}
}

func TestTemplateCacheLostResponseDoesNotRetryCreate(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	t.Setenv("FAKE_CACHE_CREATE_LOST_RESPONSE", "1")
	original := stepRetryDelays
	stepRetryDelays = []time.Duration{0}
	t.Cleanup(func() { stepRetryDelays = original })
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, _ := cacheRecoverySteps()
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if ok || result.Exit != 1 || strings.Count(string(calls), "cache-create journaled") != 1 {
		t.Fatalf("uncertain cache create was retried or reported successful: result=%+v calls=%s", result, calls)
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 2 || scope[1].done {
		t.Fatalf("lost response discarded durable cache cleanup: %+v", entries)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); !errors.Is(err, journalfile.ErrTemplateCacheCreatePending) {
		t.Fatalf("lost-response replay did not preserve uncertainty: %v", err)
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 2 || !entries[1].CacheCreatePending {
		t.Fatalf("lost-response replay consumed the cache finalizer: %+v", entries)
	}
	cl.keep = false
	if failures := cl.run(scope); len(failures) == 0 {
		t.Fatal("in-process cleanup ignored pending cache allocation")
	}
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "cache-delete") || strings.Contains(string(calls), "template-delete") || len(readJournalEntries(t, cl.journal)) != 2 {
		t.Fatalf("in-process cleanup consumed uncertain cache: %s", calls)
	}
}

func TestTemplateCacheAcceptedOnlyResultKeepsPendingIntent(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, true)
	t.Setenv("FAKE_CACHE_CREATE_ACCEPTED_ONLY", "1")
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, _ := cacheRecoverySteps()
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second)
	if ok || result.Status != report.StatusError {
		t.Fatalf("accepted-only response claimed ready cache: %+v", result)
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 2 || !entries[1].CacheCreatePending {
		t.Fatalf("accepted-only response discarded pending intent: %+v", entries)
	}
}

func TestTemplateCacheRunBoundDeleteRequiresReservedOwnership(t *testing.T) {
	for _, mode := range []string{"unreserved", "wrong target", "wrong team", "missing journal entry"} {
		t.Run(mode, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			data := map[string]any{"team_id": "team-1"}
			nativeSuccessfulCreate(t, cfg, cl, &scope, data)
			data["native_template_id"] = data["template_id"]
			create, remove := cacheRecoverySteps()
			if mode != "unreserved" {
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
					t.Fatalf("cache create: %+v", result)
				}
			}
			if mode == "wrong target" {
				remove.Run = "ecctl sandbox template-cache delete tpl-other --team-id team-1 --timeout 5m --output=json"
			}
			if mode == "wrong team" {
				remove.Run = "ecctl sandbox template-cache delete tpl-1 --team-id team-other --timeout 5m --output=json"
			}
			if mode == "missing journal entry" {
				body, err := os.ReadFile(cl.journal)
				if err != nil {
					t.Fatal(err)
				}
				var journal report.CleanupJournal
				if err := json.Unmarshal(body, &journal); err != nil {
					t.Fatal(err)
				}
				journal.Entries = journal.Entries[:1]
				body, err = json.Marshal(journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cl.journal, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, remove, 5*time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if ok || result.Status != report.StatusError || strings.Contains(string(calls), "cache-delete") {
				t.Fatalf("unowned cache delete reached cloud: result=%+v calls=%s", result, calls)
			}
		})
	}
}

func TestTemplateCacheCrashReplayDeletesBeforeNativeTemplate(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	data := map[string]any{"team_id": "team-1"}
	nativeSuccessfulCreate(t, cfg, cl, &scope, data)
	data["native_template_id"] = data["template_id"]
	create, _ := cacheRecoverySteps()
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		journal, _ := os.ReadFile(cl.journal)
		t.Fatalf("cache create: %+v calls=%s journal=%s", result, calls, journal)
	}
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil {
		t.Fatalf("replay pending cache deletion: %v", err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	cacheAt := strings.Index(string(calls), "cache-delete")
	templateAt := strings.Index(string(calls), "template-delete")
	if cacheAt < 0 || templateAt < 0 || cacheAt > templateAt || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("crash recovery violated cache-before-template order: %s", calls)
	}
}

func TestTemplateCacheDeleteFailureKeepsNativeParents(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "case cleanup"
		if replay {
			name = "journal replay"
		}
		t.Run(name, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, replay)
			var scope []*cleanupItem
			if err := cl.push(&scope, "team", "ecctl sandbox team delete team-1 --timeout 5m", "primary", nil); err != nil {
				t.Fatal(err)
			}
			data := map[string]any{"team_id": "team-1"}
			nativeSuccessfulCreate(t, cfg, cl, &scope, data)
			data["native_template_id"] = data["template_id"]
			create, _ := cacheRecoverySteps()
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, create, 5*time.Second); !ok {
				t.Fatalf("cache create: %+v", result)
			}
			t.Setenv("FAKE_CACHE_DELETE_FAILURE", "1")
			if replay {
				result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
				if err != nil || result.Errors != 1 || result.Deleted != 0 {
					t.Fatalf("failed replay: result=%+v err=%v", result, err)
				}
			} else {
				if failures := cl.runCaseCleanup(scope); len(failures) != 1 {
					t.Fatalf("failed case cleanup: %v", failures)
				}
				// The runner's deferred second call must not skip the failed child.
				if failures := cl.runCaseCleanup(scope); len(failures) != 1 {
					t.Fatalf("second case cleanup: %v", failures)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if !strings.Contains(string(calls), "cache-delete") || strings.Contains(string(calls), "template-delete") || strings.Contains(string(calls), "team-delete") {
				t.Fatalf("failed child permitted parent deletion: %s", calls)
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 3 || !strings.Contains(entries[0].Teardown, "team delete") || !strings.Contains(entries[1].Teardown, "template") || !strings.Contains(entries[2].Teardown, "template-cache") {
				t.Fatalf("dependency journal was not retained in order: %+v", entries)
			}
			t.Setenv("FAKE_CACHE_DELETE_FAILURE", "")
			result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
			if err != nil || result.Errors != 0 || result.Deleted != 3 {
				t.Fatalf("retry replay: result=%+v err=%v", result, err)
			}
			calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
			cacheAt, templateAt, teamAt := strings.LastIndex(string(calls), "cache-delete"), strings.Index(string(calls), "template-delete"), strings.Index(string(calls), "team-delete")
			if cacheAt < 0 || templateAt < cacheAt || teamAt < templateAt || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Fatalf("retry did not retire descendants before parents: %s", calls)
			}
		})
	}
}
