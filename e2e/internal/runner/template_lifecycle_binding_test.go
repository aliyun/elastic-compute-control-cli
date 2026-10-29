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
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/caselint"
	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
)

func TestE2BTemplateRunnerCompatibility(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := t.TempDir()
	bin, calls := filepath.Join(dir, "ecctl"), filepath.Join(dir, "calls")
	t.Setenv("FAKE_E2B_CALLS", calls)
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_E2B_CALLS\"\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, "", report.CleanupJournal{}, func(string, ...any) {})
	commands := []string{
		"ecctl sandbox template create --name native --from-image alpine --step '{\"type\":\"RUN\",\"args\":[\"true\"]}'",
		"ecctl sandbox template update tpl-1 --public=false",
		"ecctl sandbox template update tpl-1 --openapi=false --public=false",
		"ecctl sandbox template update tpl-1 --openapi --openapi=false --public=false",
		"ecctl sandbox template build-status tpl-1 --build-id build-1",
		"ecctl sandbox template build-logs tpl-1 --build-id build-1 --cursor 1",
		"ecctl sandbox template build-status tpl-1 --build-id '--openapi'",
		"ecctl sandbox template show tpl-1",
		"ecctl sbx tpl describe tpl-1 --openapi=false",
		"ecctl sbx tpl ls",
		"ecctl --fields=id sbx tpl show tpl-1 --openapi=false",
		"ecctl --limit=1 sandbox template ls",
	}
	for _, command := range commands {
		var scope []*cleanupItem
		result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: command, Run: command}, time.Second)
		if !ok || result.Command == "" {
			t.Errorf("E2B command was rejected before execution: %+v", result)
		}
	}
	data, _ := os.ReadFile(calls)
	if strings.Count(string(data), "\n") != len(commands) {
		t.Errorf("E2B lifecycle did not reach child: %s", data)
	}
	for _, command := range commands[len(commands)-3:] {
		if !strings.Contains(string(data), strings.TrimPrefix(command, "ecctl ")+"\n") {
			t.Errorf("E2B alias changed before child execution: %s", data)
		}
	}
}

func TestNativeTemplateJournalRejectsDifferentPrincipalBeforeCreate(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "retired"}[retired], func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, false)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			if retired {
				if failures := cl.run(scope); len(failures) != 0 {
					t.Fatal(failures)
				}
			}
			t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
			step := scenario.Step{Name: "second", Run: "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name second", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if ok || !strings.Contains(result.Error, "different credential principal") || len(paths) != 1 || strings.Count(string(calls), "create profile=") != 1 {
				t.Errorf("second principal created an uncleanable allocation: %+v intents=%v calls=%s", result, paths, calls)
			}
			t.Setenv("FAKE_PRINCIPAL_USER", "user-a")
			if failures := cl.run(scope); len(failures) != 0 || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Errorf("rejected second creation blocked original cleanup: %v", failures)
			}
		})
	}
}

func TestNativeTemplateJournalAllowsSamePrincipalReservation(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	intent, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name second", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
	if err != nil {
		t.Fatalf("same principal's other profile was rejected: %v", err)
	}
	_ = finishTemplateCreate(execpkg.Result{Exit: -1}, cl, &scope, map[string]any{}, nil, time.Second, intent)
	if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil {
		t.Fatalf("proven unstarted second creation fenced first allocation: %v", err)
	}
}

func TestNativeTemplateUnsupportedPlanNeverReservesOrCreates(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox template create --openapi --team-id team-1 --name x --plan /tmp/unused.json",
		"ecctl sbx tpl new --openapi --team-id team-1 --name x --plan=/tmp/unused.json",
		"ecctl --plan /tmp/unused.json sandbox template create --openapi --team-id team-1 --name x",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			step := scenario.Step{Name: "unsupported plan", Run: command, Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			if ok || result.Command != "" || result.Error == "" || strings.Contains(string(calls), "create profile=") || len(paths) != 0 || len(scope) != 0 || len(cl.nativeJournals) != 0 {
				t.Errorf("unsupported flag was dropped before a real allocation: %+v calls=%s intents=%v", result, calls, paths)
			}
		})
	}
}

func TestNativeTemplateRetiredProfileMustNotStrandNextAllocation(t *testing.T) {
	for _, cleanupMode := range []string{"runtime", "explicit", "replay"} {
		t.Run(cleanupMode, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			firstIdentity := scope[0].templateBinding.Identity
			oldDelete := scenario.Step{Name: "retire A", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1"}
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, oldDelete, time.Second); !ok {
				t.Fatalf("initial deletion failed: %+v", result)
			}
			if entries := readJournalEntries(t, cl.journal); len(entries) != 0 {
				t.Fatalf("A cleanup entry was not consumed: %+v", entries)
			}
			body, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.Replace(string(body), "#!/usr/bin/env bash\n", `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* && "$*" == *"--profile account-a"* && -f "$FAKE_NATIVE_DIR/revoked-a" ]]; then
 echo '{"error":{"code":"ProfileNotFound"}}'; exit 1
fi
`, 1)
			script = strings.ReplaceAll(script, `"id":"tpl-1"`, `"id":"tpl-2"`)
			script = strings.Replace(script, `"$profile" != account-a || -f "$FAKE_NATIVE_DIR/deleted"`, `"$profile" != account-b || -f "$FAKE_NATIVE_DIR/deleted-b"`, 1)
			script = strings.Replace(script, `touch "$FAKE_NATIVE_DIR/deleted"`, `touch "$FAKE_NATIVE_DIR/deleted-b"`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			revoked := filepath.Join(dir, "revoked-a")
			if err := os.WriteFile(revoked, []byte("removed"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := execpkg.ReadIdentity(context.Background(), cfg, "account-a"); err == nil {
				t.Fatal("revocation control did not fail A")
			}
			if identity, err := execpkg.ReadIdentity(context.Background(), cfg, "account-b"); err != nil || identity != firstIdentity {
				t.Fatalf("B is not valid for the same principal: %+v %v", identity, err)
			}
			second := scenario.Step{Name: "create B", Run: "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name second", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, second, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			if ok || !strings.Contains(result.Error, "cleanup credentials") || strings.Count(string(calls), "create profile=") != 1 || len(paths) != 1 || len(readJournalEntries(t, cl.journal)) != 0 || len(scope) != 1 {
				t.Fatalf("unavailable historical profile admitted another allocation: %+v calls=%s intents=%v scope=%d", result, calls, paths, len(scope))
			}
			// The conservative admission gate keeps historical proofs intact.
			// Restore that prerequisite, then prove B can allocate and be deleted.
			if err := os.Remove(revoked); err != nil {
				t.Fatal(err)
			}
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, second, time.Second); !ok {
				t.Fatalf("restored cleanup prerequisites did not permit B: %+v", result)
			}
			entries := readJournalEntries(t, cl.journal)
			if len(entries) != 1 || !strings.Contains(entries[0].Teardown, "tpl-2") || !strings.Contains(entries[0].Teardown, "account-b") {
				t.Fatalf("B allocation ownership was not retained: %+v", entries)
			}
			switch cleanupMode {
			case "runtime":
				cl.keep = false
				if failures := cl.run(scope); len(failures) != 0 {
					t.Fatal(failures)
				}
			case "explicit":
				step := scenario.Step{Name: "delete B", Run: "ecctl sandbox template delete tpl-2 --openapi --team-id team-1 --profile account-b"}
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second); !ok {
					t.Fatalf("explicit cleanup failed: %+v", result)
				}
			case "replay":
				if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || result.Deleted != 1 {
					t.Fatalf("replay failed: %+v %v", result, err)
				}
			}
			calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "create profile=") != 2 || strings.Count(string(calls), "template-delete profile=account-b flag=account-b") != 1 || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Errorf("B cleanup was not executed exactly once: %s", calls)
			}
			if _, err := os.Stat(filepath.Join(dir, "deleted-b")); err != nil {
				t.Errorf("B delete child did not complete: %v", err)
			}
		})
	}
}

func TestNativeTemplateSamePrincipalProfilesAllowTeamParentCleanup(t *testing.T) {
	for _, cleanupMode := range []string{"runtime", "replay"} {
		t.Run(cleanupMode, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			if err := cl.push(&scope, "team", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "delete A", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1 --profile account-a"}, time.Second); !ok {
				t.Fatalf("A child deletion failed: %+v", result)
			}
			body, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(string(body), `"id":"tpl-1"`, `"id":"tpl-2"`)
			script = strings.Replace(script, `"$profile" != account-a || -f "$FAKE_NATIVE_DIR/deleted"`, `"$profile" != account-b || -f "$FAKE_NATIVE_DIR/deleted-b"`, 1)
			script = strings.Replace(script, `touch "$FAKE_NATIVE_DIR/deleted"`, `touch "$FAKE_NATIVE_DIR/deleted-b"`, 1)
			script = strings.Replace(script, `if [[ "$*" == *"team delete"* ]]; then echo team-delete >> "$FAKE_NATIVE_DIR/calls"; echo '{"deleted":true}'; exit 0; fi`, `if [[ "$*" == *"team delete"* ]]; then
 if [[ -z "$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" ]]; then exit 2; fi
 echo "team-delete profile=$profile pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/calls"
 echo '{"deleted":true}'; exit 0
fi`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			second := scenario.Step{Name: "create B", Run: "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name second", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, second, time.Second); !ok {
				t.Fatalf("same-principal B allocation failed: %+v", result)
			}
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "delete B", Run: "ecctl sandbox template delete tpl-2 --openapi --team-id team-1 --profile account-b"}, time.Second); !ok {
				t.Fatalf("B child deletion failed: %+v", result)
			}
			intents, err := filepath.Glob(cl.journal + ".template-create-*.json")
			if err != nil || len(intents) != 2 || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Fatalf("A/B history or Team parent was lost: intents=%v entries=%+v err=%v", intents, readJournalEntries(t, cl.journal), err)
			}
			if cleanupMode == "runtime" {
				cl.keep = false
				if failures := cl.run(scope); len(failures) != 0 {
					t.Fatalf("runtime Team parent cleanup failed: %v", failures)
				}
			} else if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err != nil || result.Deleted != 1 {
				t.Fatalf("replay Team parent cleanup failed: %+v %v", result, err)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if !strings.Contains(string(calls), "team-delete profile=account-") || !strings.Contains(string(calls), `"UserId":"user-a"`) || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Fatalf("Team parent stayed after A/B cleanup: calls=%s", calls)
			}
		})
	}
}

func TestNativeTemplateLocalValidationAllowsParentCleanup(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime", true: "replay"}[replay], func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, replay)
			body, _ := os.ReadFile(cfg.Bin)
			script := strings.Replace(string(body), `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"template create"* ]]; then
 echo rejected-create >> "$FAKE_NATIVE_DIR/calls"
 echo '{"error":{"kind":"client","code":"InvalidFCTemplateConfig","retryable":false,"field":"body.runtimeConfig.internetAccess"}}'; exit 1
fi
if [[ "$*" == *"template list"* ]]; then echo reconcile-list >> "$FAKE_NATIVE_DIR/calls"; echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			step := scenario.Step{Name: "invalid", Run: "ecctl sandbox template create --openapi --team-id team-1 --name invalid --runtime-config '{\"internetAccess\":\"wrong\"}'", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			if ok || !strings.Contains(result.Stdout, "InvalidFCTemplateConfig") {
				t.Fatalf("lost the original validation failure: %+v", result)
			}
			if replay {
				deleted, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
				if err != nil || deleted.Deleted != 1 {
					t.Errorf("local validation blocked parent replay: %+v %v", deleted, err)
				}
			} else if failures := cl.run(scope); len(failures) != 0 {
				t.Errorf("local validation blocked runtime parent cleanup: %v", failures)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Contains(string(calls), "reconcile-list") || !strings.Contains(string(calls), "team-delete") || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Errorf("deterministic failure was treated as unknown allocation: %s", calls)
			}
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			var evidence map[string]any
			encoded, _ := os.ReadFile(paths[0])
			if json.Unmarshal(encoded, &evidence) != nil || evidence["state"] != "not_allocated" || evidence["no_allocation_code"] != "InvalidFCTemplateConfig" || evidence["started"] != true || evidence["exit"] != float64(1) || len(evidence["stdout_sha256"].(string)) != 64 {
				t.Errorf("no-allocation evidence was not retained: %s", encoded)
			}
		})
	}
}

func TestNativeTemplateUnknownFailureEvidenceStaysFenced(t *testing.T) {
	for _, failure := range []string{
		`{"kind":"service","code":"InvalidFCTemplateConfig","retryable":false}`,
		`{"kind":"client","code":"InvalidParameter","retryable":false}`,
		`{"kind":"client","code":"InvalidFCTemplateConfig","retryable":true}`,
	} {
		t.Run(failure, func(t *testing.T) {
			cfg, cl, _ := nativeCleanupFixture(t, false)
			var scope []*cleanupItem
			if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			intent, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --profile account-a --team-id team-1 --name invalid", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			stdout := `{"error":` + failure + `}`
			var decoded any
			if json.Unmarshal([]byte(stdout), &decoded) != nil {
				t.Fatal(stdout)
			}
			_ = finishTemplateCreate(execpkg.Result{Started: true, Exit: 1, Stdout: stdout, JSON: decoded}, cl, &scope, map[string]any{}, nil, time.Second, intent)
			if intent.State != "pending" || journalfile.CheckTemplateCreateIntents(cl.journal, cl.journalMeta, cfg.Env) == nil {
				t.Error("unknown failure was accepted as no allocation")
			}
		})
	}
}

func TestNativeTemplateNoAllocationProofFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{{"exit", 2}, {"started", false}, {"no_allocation_code", "InvalidParameter"}, {"stdout_sha256", "invalid"}, {"stderr_sha256", ""}, {"deletes", []string{"ecctl sandbox template delete tpl-foreign --openapi --region cn-hangzhou --team-id team-1 --profile account-a"}}} {
		t.Run(tc.field, func(t *testing.T) {
			cfg, cl, _ := nativeCleanupFixture(t, true)
			intent, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --team-id team-1 --name invalid", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			stdout := `{"error":{"kind":"client","code":"InvalidFCTemplateConfig","retryable":false}}`
			var decoded any
			if json.Unmarshal([]byte(stdout), &decoded) != nil {
				t.Fatal(stdout)
			}
			var scope []*cleanupItem
			_ = finishTemplateCreate(execpkg.Result{Started: true, Exit: 1, Stdout: stdout, JSON: decoded}, cl, &scope, map[string]any{}, nil, time.Second, intent)
			if err := journalfile.CheckTemplateCreateIntents(cl.journal, cl.journalMeta, cfg.Env); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(intent.path)
			var evidence map[string]any
			if json.Unmarshal(data, &evidence) != nil {
				t.Fatal(string(data))
			}
			evidence[tc.field] = tc.value
			data, _ = json.Marshal(evidence)
			if err := os.WriteFile(intent.path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if journalfile.CheckTemplateCreateIntents(cl.journal, cl.journalMeta, cfg.Env) == nil {
				t.Error("invalid no-allocation evidence released cleanup fence")
			}
		})
	}
}

func TestDefaultResourceCommandsReachActualRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "ecctl")
	calls := filepath.Join(dir, "calls")
	t.Setenv("FAKE_DEFAULT_CALLS", calls)
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_DEFAULT_CALLS\"\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, "", report.CleanupJournal{}, func(string, ...any) {})
	for _, command := range []string{"ecctl vpc create --name x --cidr 10.20.0.0/16", "ecctl ack create --name x --type ManagedKubernetes", "ecctl --lang en vpc delete vpc-1 --force", "ecctl --profile remove ack delete c-1 --force"} {
		var scope []*cleanupItem
		result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: command, Run: command}, time.Second)
		if !ok || result.Command == "" {
			t.Errorf("Template grammar rejected default-resource command: %+v", result)
		}
	}
	data, _ := os.ReadFile(calls)
	if strings.Count(string(data), "\n") != 4 {
		t.Errorf("default commands never reached child: %s", data)
	}
}

func TestPendingNativeTemplateFencesDestructiveLayouts(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	_, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --team-id team-1 --name pending", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, path := cl.journalValues(&cleanupItem{role: "primary"})
	if journalfile.CheckTemplateCreateIntents(path, meta, cfg.Env) == nil {
		t.Fatal("fixture lacks a real unresolved creation fence")
	}
	// Record every child, including ones no longer recognizable after argv
	// normalization, so a misclassified delete cannot disappear in the fake.
	calls := filepath.Join(dir, "guard-calls")
	t.Setenv("FAKE_GUARD_CALLS", calls)
	if err := os.WriteFile(cfg.Bin, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_GUARD_CALLS\"\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := []string{
		"ecctl --lang en vpc delete vpc-owned --force",
		"ecctl --profile delete vpc rm vpc-owned --force",
		"ecctl vpc --profile=remove remove vpc-owned --force",
		"ecctl --profile -- ack delete c-owned --force",
		"ecctl --no-color --agent-envelope=false ack rm c-owned --force",
		"ecctl sandbox remove sbx-owned",
		"ecctl sbx rm sbx-owned",
		"ecctl vpc vpc delete vpc-owned --force",
		"ecctl --profile rm ecs instance remove i-owned --force",
		"ecctl sandbox team delete team-1",
		"ecctl sandbox template rm tpl-owned",
		"ecctl call ecs DeleteInstance --request '{\"InstanceId\":\"i-owned\"}'",
		"ecctl --profile=DeleteInstance call ecs DeleteInstance --request '{}'",
		"ecctl -- vpc delete vpc-owned --force",
		"ecctl vpc -- delete vpc-owned --force",
		"ecctl vpc delete -- vpc-owned",
		"ecctl vpc delete vpc-owned -- --profile get",
		"ecctl ecs instance -- delete i-owned --force",
		"ecctl call ecs -- DeleteInstance --request '{}'",
		"ecctl call fcsandbox deleteteam --request '{\"teamID\":\"team-1\"}'",
		"ecctl --profile delete call fcsandbox deleteTemplate --request '{}'",
		"ecctl call ecs DELETEINSTANCE --request '{}'",
		"ecctl vpc vpc --dry-run=false delete vpc-owned --force",
		"ecctl --dry-run=0 vpc rm vpc-owned --force",
		"ecctl vpc --dry-run=false vpc remove vpc-owned --force",
		"ecctl vpc vpc --future-leaf=false delete vpc-owned --force",
		"ecctl --future-leaf delete vpc delete vpc-owned --force",
		"ecctl rg policy version delete v2 --policy-name p --fallback-default-version v1",
		"ecctl --profile delete rg policy version rm v2 --policy-name p --fallback-default-version v1",
		"ecctl rg policy version --policy-name delete remove v2 --fallback-default-version v1",
		"ecctl ack inspect config delete --cluster c-owned",
		"ecctl ack policy instance delete ACKAllowedRepos --cluster c-owned --instance-name owned",
		"ecctl ack --profile remove policy instance rm ACKAllowedRepos --cluster c-owned --instance-name delete",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: command, Run: command}, time.Second)
			data, _ := os.ReadFile(calls)
			if ok || result.Command != "" || !strings.Contains(result.Error, "pending Template create intent") || len(data) != 0 {
				t.Errorf("pending allocation did not fence delete before child: %+v calls=%s", result, data)
			}
		})
	}
	for _, command := range []string{
		"ecctl --profile delete vpc get vpc-owned",
		"ecctl --profile=rm vpc get -- delete",
		"ecctl --profile -- vpc show remove",
		"ecctl -- vpc list --filter name=delete",
		"ecctl --profile remove ecs instance get i-owned --fields delete",
		"ecctl sandbox template get delete --openapi=false",
		"ecctl call ecs DescribeInstances --request '{\"Name\":\"DeleteInstance\"}'",
		"ecctl rg policy version get delete --policy-name delete --policy-type Custom",
		"ecctl --profile remove rg policy version list --policy-name delete",
		"ecctl rg policy --policy-name delete version get remove --policy-type Custom",
		"ecctl ack inspect config get --cluster delete",
		"ecctl ack policy instance get delete --cluster rm --instance-name remove",
		"ecctl --profile delete ack policy instance show remove --cluster delete",
	} {
		t.Run("read/"+command, func(t *testing.T) {
			if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: command, Run: command}, time.Second)
			data, _ := os.ReadFile(calls)
			if !ok || len(data) == 0 {
				t.Errorf("argument/flag value became a destructive operation: %+v calls=%s", result, data)
			}
		})
	}
	if journalfile.CheckTemplateCreateIntents(path, meta, cfg.Env) == nil {
		t.Error("classification discarded unresolved ownership evidence")
	}
}

func TestPendingNativeTemplateFencesOrdinaryAutomaticFinalizer(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, false)
	var scope []*cleanupItem
	if err := cl.push(&scope, "stack", "ecctl t thing delete ordinary", "primary", nil); err != nil {
		t.Fatal(err)
	}
	intent, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --team-id team-1 --name pending", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
		providerCalls++
		return execpkg.Result{Started: true, Exit: 0}
	}
	if failures := cl.run(scope); len(failures) == 0 || providerCalls != 0 || scope[0].done {
		t.Fatalf("ordinary automatic finalizer crossed pending native intent: failures=%v calls=%d done=%v", failures, providerCalls, scope[0].done)
	}
	if _, err := os.Stat(intent.path); err != nil {
		t.Fatalf("pending native intent disappeared: %v", err)
	}
	if got := readJournalEntries(t, cl.journal); len(got) != 1 || got[0].Teardown != scope[0].cmd {
		t.Fatalf("ordinary finalizer was consumed despite pending native allocation: %+v", got)
	}
}

func TestNativeReplayAliasUsesOriginalEvidenceAndRejectsWrongTarget(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	alias := filepath.Join(dir, "alias.json")
	if err := os.Symlink(cl.journal, alias); err != nil {
		t.Fatal(err)
	}
	result, err := sweeper.ReplayJournal(context.Background(), alias, cfg)
	if err != nil || result.Deleted != 1 || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("valid alias lost original evidence/retirement: %+v %v", result, err)
	}
	cfg, cl, dir = nativeCleanupFixture(t, true)
	scope = nil
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	body, err := os.ReadFile(cl.journal)
	if err != nil {
		t.Fatal(err)
	}
	var journal report.CleanupJournal
	if json.Unmarshal(body, &journal) != nil {
		t.Fatal(string(body))
	}
	journal.Entries[0].Teardown = strings.ReplaceAll(journal.Entries[0].Teardown, "tpl-1", "tpl-foreign")
	body, _ = json.Marshal(journal)
	if err := os.WriteFile(cl.journal, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err == nil {
		t.Errorf("foreign target used another create's proof: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "template-delete") {
		t.Errorf("foreign delete reached child: %s", calls)
	}
}

func TestOwnedNativeOperationRejectsReboundConfiguration(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	a, b, link := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json"), filepath.Join(dir, "source.json")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", link)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"get", "update", "show", "describe", "ls"} {
		command := "ecctl sbx tpl " + action
		if action != "update" {
			command = "ecctl --fields=id sbx tpl " + action
		}
		if action != "ls" {
			command += " tpl-1"
		}
		command += " --openapi --team-id team-1"
		if action == "update" {
			command += " --runtime-config '{\"internetAccess\":false}'"
		} else {
			command += " --fields id"
		}
		result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: action, Run: command}, time.Second)
		if ok || result.Command != "" || !strings.Contains(result.Error, "configuration target changed") {
			t.Errorf("rebound config reached owned %s: %+v", action, result)
		}
	}
}

func TestNativeReplayRequiresExactEvidenceAfterPathChanges(t *testing.T) {
	for _, kind := range []string{"copy", "rename", "symlink", "missing-sidecar"} {
		t.Run(kind, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			path := cl.journal
			other := filepath.Join(dir, "alias.json")
			switch kind {
			case "copy":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(other, data, 0o600); err != nil {
					t.Fatal(err)
				}
				path = other
			case "rename":
				if err := os.Rename(path, other); err != nil {
					t.Fatal(err)
				}
				path = other
			case "symlink":
				if err := os.Symlink(path, other); err != nil {
					t.Fatal(err)
				}
				path = other
			case "missing-sidecar":
				paths, _ := filepath.Glob(path + ".template-create-*.json")
				if err := os.Remove(paths[0]); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
			result, err := sweeper.ReplayJournal(context.Background(), path, cfg)
			data, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if err == nil || strings.Contains(string(data), "template-delete") {
				t.Errorf("missing/rebound proof bypassed identity check: kind=%s result=%+v error=%v calls=%s", kind, result, err, data)
			}
		})
	}
}

func TestOwnedNativeReadsAndUpdatesKeepCreationBinding(t *testing.T) {
	for _, operation := range []struct{ path, action string }{
		{"sandbox template", "get"}, {"sandbox template", "list"}, {"sandbox template", "update"},
		{"sandbox template", "show"}, {"sandbox template", "describe"}, {"sandbox template", "ls"},
		{"sbx tpl", "show"}, {"sbx tpl", "describe"}, {"sbx tpl", "ls"},
		{"--fields=id sandbox template", "get"}, {"--fields id sbx tpl", "describe"},
		{"sandbox --fields id tpl", "show"}, {"--limit=1 sbx tpl", "ls"},
		{"--limit=1 --next-token delete sandbox template", "list"},
	} {
		for _, change := range []string{"current", "principal", "profile"} {
			t.Run(operation.path+"/"+operation.action+"/"+change, func(t *testing.T) {
				action := operation.action
				cfg, cl, dir := nativeCleanupFixture(t, true)
				body, err := os.ReadFile(cfg.Bin)
				if err != nil {
					t.Fatal(err)
				}
				script := strings.Replace(string(body), "echo '{}'", `if [[ "$*" =~ (template|tpl)\ (get|list|update|show|describe|ls)\  ]]; then echo "owned-operation profile=$profile flag=$given pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/calls"; echo '{}'; exit 0; fi
echo '{}'`, 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				var scope []*cleanupItem
				nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
				if change == "principal" {
					t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
				}
				command := "ecctl " + operation.path + " " + action
				if action != "list" && action != "ls" {
					command += " tpl-1"
				}
				command += " --openapi --team-id team-1"
				if action == "update" {
					command += " --runtime-config '{\"internetAccess\":false}'"
				} else {
					command += " --fields id"
				}
				if change == "profile" {
					command += " --profile account-b"
				}
				result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: action, Run: command}, time.Second)
				data, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if change != "current" {
					if ok || strings.Contains(string(data), "owned-operation") {
						t.Errorf("changed %s reached owned %s: %+v %s", change, action, result, data)
					}
				} else if !ok || !strings.Contains(string(data), "owned-operation profile=account-a flag=account-a pin={") {
					t.Errorf("owned %s lost creation binding after current switch: %+v %s", action, result, data)
				}
			})
		}
	}
}

func TestOwnedNativeTemplateBindingSurvivesKeptCase(t *testing.T) {
	for _, action := range []string{"list", "get", "update"} {
		for _, change := range []string{"same-principal", "principal", "profile", "region", "config-selector", "config-target"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, true)
				first, second, config := filepath.Join(dir, "first.json"), filepath.Join(dir, "second.json"), filepath.Join(dir, "config.json")
				for _, path := range []string{first, second} {
					if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(first, config); err != nil {
					t.Fatal(err)
				}
				cfg.Env = []string{"ECCTL_CONFIG_PATH=" + config, "ECCTL_ALIYUN_CONFIG_PATH=" + filepath.Join(dir, "aliyun.json")}
				cl.execCfg["primary"] = cfg
				body, err := os.ReadFile(cfg.Bin)
				if err != nil {
					t.Fatal(err)
				}
				script := strings.Replace(string(body), "echo '{}'", `if [[ "$*" =~ (template|tpl)\ (get|list|update|show|describe|ls)\  ]]; then
 echo "owned-operation profile=$profile flag=$given config=$ECCTL_CONFIG_PATH pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/calls"
 echo '{"templates":[{"id":"tpl-1"}],"template":{"id":"tpl-1"}}'; exit 0
fi
echo '{}'`, 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				opt := Options{RunName: "kept-cases", InputsDir: filepath.Join(dir, "inputs"), StepTimeout: time.Second, Logf: func(string, ...any) {}}
				create := &scenario.Suite{Resource: "t/create-template", Steps: []scenario.Step{{Name: "create A", Run: "ecctl sandbox template create --openapi --team-id team-1 --name first", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}}}
				// runCase creates and discards its own local scope, while --keep
				// leaves the allocation and its creation evidence in this run.
				if result := runCase(context.Background(), opt, cfg, cl, cl.operations, map[string]any{}, nil, nil, create); result.Status != report.StatusPass {
					t.Fatalf("first case did not create: %+v", result)
				}
				readCfg := cfg
				extra := ""
				switch change {
				case "principal":
					t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
				case "profile":
					extra = " --profile account-b"
				case "region":
					extra = " --region cn-shanghai"
				case "config-selector":
					readCfg.Env = append(append([]string{}, cfg.Env...), "ECCTL_CONFIG_PATH="+second)
				case "config-target":
					if err := os.Remove(config); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(second, config); err != nil {
						t.Fatal(err)
					}
				}
				command := "ecctl sandbox template " + action
				if action == "list" {
					command = "ecctl --limit=1 sbx tpl ls"
				} else {
					command += " tpl-1"
				}
				command += " --openapi --team-id team-1" + extra
				if action == "update" {
					command += " --runtime-config '{\"internetAccess\":false}'"
				}
				steps := []scenario.Step{{Name: action + " in case B", Run: command}}
				if action == "list" {
					steps[0].At, steps[0].Capture = "$.templates[0]", map[string]string{"owned_template_id": "id"}
					steps = append(steps,
						scenario.Step{Name: "get captured", Run: "ecctl sandbox template get {{.owned_template_id}} --openapi --team-id team-1"},
						scenario.Step{Name: "update captured", Run: "ecctl sandbox template update {{.owned_template_id}} --openapi --team-id team-1 --runtime-config '{\"internetAccess\":false}'"})
				}
				result := runCase(context.Background(), opt, readCfg, cl, cl.operations, map[string]any{}, nil, nil, &scenario.Suite{Resource: "t/use-template", Steps: steps})
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if change == "same-principal" {
					target, err := filepath.EvalSymlinks(first)
					if err != nil {
						t.Fatal(err)
					}
					boundCall := "owned-operation profile=account-a flag=account-a config=" + target + " pin={"
					if result.Status != report.StatusPass || strings.Count(string(calls), boundCall) != len(steps) || !strings.Contains(string(calls), `"UserId":"user-a"`) {
						t.Errorf("later case lost creation profile/config/STS binding: %+v calls=%s", result, calls)
					}
				} else if result.Status != report.StatusFail || strings.Contains(string(calls), "owned-operation") || len(result.Steps) != 1 || result.Steps[0].Command != "" {
					t.Errorf("later case bypassed %s binding before %s: %+v calls=%s", change, action, result, calls)
				}
				paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
				if len(paths) != 1 || len(readJournalEntries(t, cl.journal)) != 1 || strings.Contains(string(calls), "template-delete") {
					t.Error("kept cross-case operation altered creation ownership or deleted the allocation")
				}
			})
		}
	}
}

func TestOwnedNativeListSelectsExplicitProfileBeforeAmbiguity(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(fmt.Sprint(retired), func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			if retired {
				step := scenario.Step{Name: "delete A", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1"}
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second); !ok {
					t.Fatalf("A deletion failed: %+v", result)
				}
			}
			body, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(string(body), `"id":"tpl-1"`, `"id":"tpl-2"`)
			script = strings.Replace(script, "echo '{}'", `if [[ "$*" =~ (template|tpl)\ (list|ls)\  ]]; then
 echo "owned-list profile=$profile flag=$given pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" >> "$FAKE_NATIVE_DIR/calls"; echo '{}'; exit 0
fi
echo '{}'`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			second := scenario.Step{Name: "create B", Run: "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name second", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, second, time.Second); !ok {
				t.Fatalf("same-principal B creation failed: %+v", result)
			}
			read := func(command, profile string, readCfg execpkg.Config, wantOK bool) {
				t.Helper()
				before, _ := os.ReadFile(filepath.Join(dir, "calls"))
				result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, readCfg, cl, &scope, map[string]any{}, scenario.Step{Name: "list", Run: command}, time.Second)
				after, _ := os.ReadFile(filepath.Join(dir, "calls"))
				delta := string(after[len(before):])
				if wantOK {
					if !ok || !strings.Contains(delta, "owned-list profile="+profile+" flag="+profile+" pin={") || !strings.Contains(delta, `"UserId":"user-a"`) {
						t.Errorf("explicit/active profile lost its creation pin: %+v calls=%s", result, delta)
					}
				} else if ok || strings.Contains(delta, "owned-list") {
					t.Errorf("ambiguous/conflicting binding reached list: %+v calls=%s", result, delta)
				}
			}
			for _, path := range []string{"sandbox template list", "sbx tpl ls", "--limit=1 --fields=id sbx tpl ls"} {
				read("ecctl "+path+" --openapi --team-id team-1 --profile account-b", "account-b", cfg, true)
			}
			read("ecctl sandbox template list --openapi --team-id team-1 --profile account-a", "account-a", cfg, true)
			read("ecctl sandbox template list --openapi --team-id team-1", "account-b", cfg, retired)
			read("ecctl sandbox template list --openapi --team-id team-1 --profile missing", "", cfg, false)
			t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
			read("ecctl sandbox template list --openapi --team-id team-1 --profile account-b", "", cfg, false)
			t.Setenv("FAKE_PRINCIPAL_USER", "user-a")
			rebound := cfg
			rebound.Env = append(append([]string{}, cfg.Env...), "ECCTL_CONFIG_PATH="+filepath.Join(dir, "changed-config.json"))
			read("ecctl sandbox template list --openapi --team-id team-1 --profile account-b", "", rebound, false)
			paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
			if len(paths) != 2 || len(readJournalEntries(t, cl.journal)) != map[bool]int{false: 2, true: 1}[retired] {
				t.Error("read selection changed historical ownership or cleanup entries")
			}
		})
	}
}

func TestNativeExplicitDeleteRequiresExactCreationOwnership(t *testing.T) {
	for _, created := range []bool{false, true} {
		for _, path := range []string{"sandbox template delete", "sbx tpl rm", "sbx tpl remove"} {
			t.Run(fmt.Sprint(created)+"/"+path, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, true)
				var scope []*cleanupItem
				if created {
					nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
				}
				command := "ecctl " + path + " tpl-foreign --openapi --team-id team-1 --profile account-a"
				result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "foreign delete", Run: command}, time.Second)
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if ok || result.Command != "" || strings.Contains(string(calls), "template-delete") {
					t.Errorf("foreign native target reached delete with matching selectors: %+v calls=%s", result, calls)
				}
				if created && !ok && !strings.Contains(string(calls), "template-delete") {
					owned := strings.Replace(command, "tpl-foreign", "tpl-1", 1)
					result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "owned delete", Run: owned}, time.Second)
					if !ok || !scope[0].done || len(readJournalEntries(t, cl.journal)) != 0 {
						t.Errorf("exact owned target lost safe deletion: %+v", result)
					}
				}
			})
		}
	}
}

func TestNativeUnknownLeadingFlagRefusesDispatch(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	body, _ := os.ReadFile(cfg.Bin)
	script := strings.Replace(string(body), "echo '{}'", `echo unbound-read >> "$FAKE_NATIVE_DIR/calls"
echo '{}'`, 1)
	if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "unknown native prefix", Run: "ecctl --unknown-read-mode=opaque sandbox template get tpl-1 --openapi --team-id team-1"}, time.Second)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if ok || result.Command != "" || strings.Contains(string(calls), "unbound-read") {
		t.Errorf("unknown prefix became another family and escaped binding: %+v calls=%s", result, calls)
	}
}

func TestNativeCleanupDeclarationIsNotCreationEvidence(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	cl.journal = ""
	var scope []*cleanupItem
	command := "ecctl sandbox template delete tpl-foreign --openapi --region cn-hangzhou --team-id team-1 --profile account-a"
	if err := cl.push(&scope, "declared only", command, "primary", nil); err != nil {
		t.Fatal(err)
	}
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "unproven cleanup", Run: command}, time.Second)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if ok || result.Command != "" || strings.Contains(string(calls), "template-delete") {
		t.Errorf("a cleanup declaration substituted for creation proof: %+v calls=%s", result, calls)
	}
}

func TestNativeRuntimeFinalizerRejectsUnboundDeclarations(t *testing.T) {
	for _, journal := range []bool{false, true} {
		for _, command := range []string{
			"ecctl sandbox template delete tpl-foreign --openapi --region cn-hangzhou --team-id team-1 --profile account-a",
			"ecctl --profile account-a sandbox template delete tpl-foreign --openapi --region cn-hangzhou --team-id team-1",
			"ecctl sandbox --lang en template --profile=account-a delete tpl-foreign --openapi --region cn-hangzhou --team-id team-1 --output=json",
		} {
			t.Run(fmt.Sprint(journal)+"/"+command, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, false)
				body, _ := os.ReadFile(cfg.Bin)
				script := strings.Replace(string(body), "#!/usr/bin/env bash\n", "#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_NATIVE_DIR/all-invocations\"\n", 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				if !journal {
					cl.journal = ""
				}
				var scope []*cleanupItem
				step := scenario.Step{Name: "read existing", Run: "ecctl sandbox template get tpl-foreign --openapi --team-id team-1", Teardown: command}
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second); !ok || len(scope) != 1 {
					t.Fatalf("read did not register the declared runtime teardown: %+v", result)
				}
				invocations := filepath.Join(dir, "all-invocations")
				if err := os.WriteFile(invocations, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				failures := cl.run(scope)
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				allCalls, _ := os.ReadFile(invocations)
				if len(failures) == 0 || len(allCalls) != 0 || strings.Contains(string(calls), "template-delete") || scope[0].done {
					t.Errorf("unbound runtime finalizer reached child: failures=%v done=%v argv=%s calls=%s", failures, scope[0].done, allCalls, calls)
				}
			})
		}
	}
	// Legacy E2B cleanup does not require native creation evidence.
	cfg, cl, dir := nativeCleanupFixture(t, false)
	cl.journal = ""
	var scope []*cleanupItem
	step := scenario.Step{Name: "legacy read", Run: "ecctl sandbox template get e2b-owned", Teardown: "ecctl sandbox template delete e2b-owned --openapi=false --profile account-a"}
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second); !ok {
		t.Fatalf("legacy read control failed: %+v", result)
	}
	if failures := cl.run(scope); len(failures) != 0 {
		t.Fatalf("legacy cleanup control failed: %v", failures)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(calls), "template-delete") != 1 {
		t.Errorf("legacy finalizer did not execute once: %s", calls)
	}
}

func TestNativeRuntimeFinalizerNormalizesOwnedDeletes(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1 --profile account-a",
		"ecctl --profile account-a sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1",
		"ecctl sandbox --lang en template --profile=account-a delete tpl-1 --openapi --region cn-hangzhou --team-id team-1 --output=json --no-color",
	} {
		t.Run(command, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, false)
			body, _ := os.ReadFile(cfg.Bin)
			script := strings.Replace(string(body), `template-delete profile=$profile flag=$given`, `template-delete profile=$profile flag=$given pin=$ECCTL_EXPECTED_CREDENTIAL_IDENTITY`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			scope[0].cmd = command
			if failures := cl.run(scope); len(failures) != 0 {
				t.Fatalf("valid owned finalizer failed: %v", failures)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "template-delete") != 1 || !strings.Contains(string(calls), `profile=account-a flag=account-a pin={`) || !strings.Contains(string(calls), `"UserId":"user-a"`) || !scope[0].done || len(readJournalEntries(t, cl.journal)) != 0 {
				t.Errorf("owned normalized deletion lost identity or canonical retirement: %s done=%v", calls, scope[0].done)
			}
		})
	}
}

func TestNativeRuntimeFinalizerRejectsConflictingBindings(t *testing.T) {
	for _, conflict := range []string{"missing", "target", "profile", "team", "region", "transport", "owner"} {
		t.Run(conflict, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, false)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			item := scope[0]
			item.cmd = "ecctl --profile account-a sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1"
			switch conflict {
			case "missing":
				item.templateBinding = nil
			case "target":
				item.cmd = strings.Replace(item.cmd, "tpl-1", "tpl-foreign", 1)
			case "profile":
				item.cmd = strings.Replace(item.cmd, "account-a", "account-b", 1)
			case "team":
				item.cmd = strings.Replace(item.cmd, "team-1", "team-foreign", 1)
			case "region":
				item.cmd = strings.Replace(item.cmd, "cn-hangzhou", "cn-beijing", 1)
			case "transport":
				item.cmd = strings.Replace(item.cmd, "--openapi", "--openapi=false", 1)
			case "owner":
				binding := *item.templateBinding
				binding.Name = "ecctl-p-000000000000000000000000"
				item.templateBinding = &binding
			}
			failures := cl.run(scope)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if len(failures) == 0 || strings.Contains(string(calls), "template-delete") || item.done || len(readJournalEntries(t, cl.journal)) != 1 {
				t.Errorf("conflicting finalizer reached deletion or lost ownership: %v done=%v calls=%s", failures, item.done, calls)
			}
		})
	}
}

func TestNativeInlineConfigurationNeverEntersReports(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		for _, suffix := range []string{" plain with spaces", " quote\" apostrophe' slash\\ comma,tail", " malformed"} {
			t.Run(fmt.Sprint(expanded)+suffix, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, true)
				const marker = "dummy-report-secret-12"
				payload, err := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"image": "example/image:v1", "registryConfig": map[string]any{"authConfig": map[string]string{"password": marker + suffix, "userName": "dummy-user"}}}})
				if err != nil {
					t.Fatal(err)
				}
				if suffix == " malformed" {
					payload = []byte("{} " + marker + " opaque private suffix")
				}
				body, _ := os.ReadFile(cfg.Bin)
				script := strings.Replace(string(body), `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"template create"* ]]; then
 printf '%s\n' "$@" > "$FAKE_NATIVE_DIR/create-args"`, 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				command := "ecctl sandbox template create --openapi --team-id team-1 --name inline --runtime-config " + teamCommandArgument(string(payload))
				data := map[string]any{}
				if expanded {
					command = "ecctl sandbox template create --openapi --team-id team-1 --name inline --runtime-config {{.private_config}}"
					data["private_config"] = teamCommandArgument(string(payload))
				}
				var scope []*cleanupItem
				step, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, data, scenario.Step{Name: "inline config", Run: command, Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
				if !ok {
					t.Fatalf("fixture did not create: %+v", step)
				}
				argv, _ := os.ReadFile(filepath.Join(dir, "create-args"))
				if !strings.Contains(string(argv), "--runtime-config="+string(payload)) {
					t.Fatal("redaction changed execution argv")
				}
				if strings.Contains(step.Command, marker) {
					t.Error("private configuration entered the recorded command")
				}
				if strings.Contains(report.Scrub("retry: ecctl --runtime-config="+teamCommandArgument(string(payload))+" --team-id team-1"), marker) {
					t.Error("private configuration entered diagnostic text")
				}
				run := &report.Run{Cases: []report.Case{{Name: "native", Resource: "sandbox/template", Status: report.StatusPass, Steps: []report.Step{step}}}}
				run.Redact()
				if command := run.Cases[0].Steps[0].Command; !strings.Contains(command, "--team-id=team-1") && !strings.Contains(command, "--team-id team-1") {
					t.Error("repeated redaction discarded following public selectors")
				}
				for _, render := range []struct {
					name  string
					write func(*report.Run, string) error
				}{{"report.json", report.WriteJSON}, {"report.html", report.WriteHTML}} {
					path := filepath.Join(dir, render.name)
					if err := render.write(run, path); err != nil {
						t.Fatal(err)
					}
					encoded, _ := os.ReadFile(path)
					if strings.Contains(string(encoded), marker) {
						t.Errorf("private configuration survived %s", render.name)
					}
				}
			})
		}
	}
}

func TestNativeDryRunRedactsTemplateSourceComments(t *testing.T) {
	const marker = "dummy-comment-private-113"
	payload, err := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]string{"password": "prefix' " + marker + " 'suffix"}}}})
	if err != nil {
		t.Fatal(err)
	}
	const suffix = "\n--region cn-hangzhou --profile account-a"
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		separated := fmt.Sprintf(`#{{printf "\n%s '%%s'" .inputs.private_config}}`, flag)
		for _, form := range []struct {
			name, source, comment string
			eof, conservative     bool
		}{
			{"generated-eof", separated + " --team-id public-suffix", "", true, false},
			{"generated-separated", separated + suffix, "", false, false},
			{"generated-inline", fmt.Sprintf(`#{{printf "\n%s='%%s'" .inputs.private_config}}`, flag) + suffix, "", false, false},
			{"generated-newline", `#{{printf "\n"}}` + flag + ` '{{.inputs.private_config}}'` + suffix, "", false, false},
			{"comment-as-value", flag + ` #{{printf "\n'%s'" .inputs.private_config}}` + suffix, "", false, true},
			{"ordinary-value-comment", flag + " # {{printf \"%s\" \"public-note\"}}\n'{{.inputs.private_config}}'" + suffix, "# public-note", false, false},
			{"ordinary-comment", "# ordinary shell comment\n" + flag + ` '{{.inputs.private_config}}'` + suffix, "# ordinary shell comment", false, false},
			{"ordinary-template-comment", "# {{printf \"%s\" \"public-note\"}}\n" + flag + ` '{{.inputs.private_config}}'` + suffix, "# public-note", false, false},
		} {
			for _, file := range []bool{false, true} {
				if form.eof && file {
					// The EOF reproducer emits additional argv after the value;
					// the other forms prove an unambiguous @file interval.
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/file=%t", flag, form.name, file), func(t *testing.T) {
					dir := t.TempDir()
					cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
					for _, path := range []string{cases, inputs} {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					value := string(payload)
					if file {
						value = "@./public config.json"
					}
					input, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(inputs, "sandbox-template.yaml"), []byte("private_config: "+string(input)+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					command := "ecctl sandbox template update tpl-1 --openapi --team-id public-team " + form.source
					quoted, err := json.Marshal(command)
					if err != nil {
						t.Fatal(err)
					}
					authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: private source comment\n    run: %s\n", quoted)
					if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
						t.Fatal(err)
					}
					lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
					if err != nil || lint.Invalid != 0 {
						t.Fatalf("authored comment case was not accepted: %v %+v", err, lint)
					}
					raw, err := vars.Render(command, map[string]any{"inputs": map[string]any{"private_config": value}})
					if err != nil {
						t.Fatal(err)
					}
					words, err := shlex.Split(raw)
					if err != nil {
						t.Fatalf("fixture did not render valid shell words: %v", err)
					}
					markerWord, fileValue := false, ""
					for index, word := range words {
						markerWord = markerWord || word == marker
						if word == flag && index+1 < len(words) {
							fileValue = words[index+1]
						} else if strings.HasPrefix(word, flag+"=") {
							fileValue = strings.TrimPrefix(word, flag+"=")
						}
					}
					if file {
						if fileValue != value {
							t.Fatal("fixture lost its valid @file argv control")
						}
					} else if !markerWord || !strings.Contains(report.Scrub(raw), marker) {
						t.Fatal("fixture did not expose the private fragment outside the raw fallback mask")
					}
					run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "source-comment", RunID: "source-comment", Region: "cn-hangzhou", DryRun: true, EcctlBin: filepath.Join(dir, "must-not-execute")})
					if err != nil {
						t.Fatal(err)
					}
					if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 || run.Summary.Skipped != 1 {
						t.Fatalf("accepted case did not reach dry-run reporting: %+v", run)
					}
					assertNativePrivateArtifacts(t, run, dir, marker)
					shown := run.Cases[0].Steps[0].Command
					if !strings.Contains(shown, "--team-id public-team") || !form.eof && (!form.conservative || file) && !strings.Contains(shown, "--region cn-hangzhou --profile account-a") {
						t.Errorf("known public flags disappeared: %s", shown)
					}
					if form.comment != "" && !strings.Contains(shown, form.comment) {
						t.Errorf("ordinary comment changed: %s", shown)
					}
					if file && shown != raw {
						t.Error("valid @file comment expansion changed")
					}
				})
			}
		}
	}
}

func TestNativeDryRunRedactsTemplateActions(t *testing.T) {
	const marker = "dummy-template-action-private-83"
	payload, err := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": marker + " }}"}}}})
	if err != nil {
		t.Fatal(err)
	}
	prerequisite := `{{printf "%q" .prerequisites.sandbox.template.inline_config}}`
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, separator := range []string{"=", " "} {
			for _, action := range []struct {
				name, source string
				file         bool
			}{
				{"literal", fmt.Sprintf(`{{printf "%%q" %q}}`, string(payload)), false},
				{"prerequisite", prerequisite, false},
				{"nested", `{{printf "%s" (printf "%q" .prerequisites.sandbox.template.inline_config)}}`, false},
				{"raw-literal", "{{printf \"%q\" `" + string(payload) + "`}}", false},
				{"comment", `{{/* keep }} inside the comment */}}` + prerequisite, false},
				{"file", `{{printf "%q" "@./public config.json"}}`, true},
			} {
				t.Run(flag+"/"+separator+"/"+action.name, func(t *testing.T) {
					dir := t.TempDir()
					cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
					for _, path := range []string{cases, inputs} {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					command := `ecctl sandbox template update tpl-1 --openapi --team-id public-team --name {{printf "%s" "public-name"}} ` + flag + separator + action.source + ` --region {{printf "%s" "cn-hangzhou"}} --profile account-a`
					quoted, err := json.Marshal(command)
					if err != nil {
						t.Fatal(err)
					}
					authored := fmt.Sprintf("resource: sandbox/template\nrequires_prerequisites: [sandbox.template]\nsteps:\n  - name: private action\n    run: %s\n", quoted)
					if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
						t.Fatal(err)
					}
					lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
					if err != nil || lint.Invalid != 0 {
						t.Fatalf("template action case was not accepted: %v %+v", err, lint)
					}
					prerequisites := map[string]any{"sandbox": map[string]any{"template": map[string]any{"inline_config": string(payload)}}}
					raw, err := vars.Render(command, map[string]any{"prerequisites": prerequisites})
					if err != nil {
						t.Fatal(err)
					}
					words, err := shlex.Split(raw)
					if err != nil {
						t.Fatalf("fixture did not render valid execution argv: %v", err)
					}
					wantValue := string(payload)
					if action.file {
						wantValue = "@./public config.json"
					}
					value := ""
					for index, word := range words {
						if word == flag && index+1 < len(words) {
							value = words[index+1]
						} else if strings.HasPrefix(word, flag+"=") {
							value = strings.TrimPrefix(word, flag+"=")
						}
					}
					if value != wantValue || !action.file && !strings.Contains(raw, marker) {
						t.Fatal("private fixture lost its positive expansion or argv-value control")
					}
					run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "private-action", RunID: "private-action", DryRun: true, Regions: map[string]Region{"primary": {ID: "cn-hangzhou", Prerequisites: prerequisites}}})
					if err != nil {
						t.Fatal(err)
					}
					if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 || run.Summary.Skipped != 1 {
						t.Fatalf("accepted case did not reach actual dry-run rendering: %+v", run)
					}
					if strings.Contains(run.Cases[0].Steps[0].Command, marker) {
						t.Error("private action entered the recorded dry command")
					}
					previous := ""
					for pass := 0; pass < 2; pass++ {
						run.Redact()
						shown := run.Cases[0].Steps[0].Command
						if strings.Contains(shown, marker) || !strings.Contains(shown, "--team-id public-team --name public-name") || !strings.Contains(shown, "--region cn-hangzhou --profile account-a") {
							t.Errorf("private action leaked or public actions changed: %s", shown)
						}
						if action.file && shown != raw || pass > 0 && shown != previous {
							t.Error("valid @file or repeated redaction changed the command")
						}
						previous = shown
						for _, artifact := range []struct {
							name  string
							write func(*report.Run, string) error
						}{{"json", report.WriteJSON}, {"html", report.WriteHTML}} {
							path := filepath.Join(dir, fmt.Sprintf("report-%d.%s", pass, artifact.name))
							if err := artifact.write(run, path); err != nil {
								t.Fatal(err)
							}
							body, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							if strings.Contains(string(body), marker) || !strings.Contains(string(body), "public-team") || !strings.Contains(string(body), "cn-hangzhou") {
								t.Errorf("template action leaked or public data vanished from %s at pass %d", artifact.name, pass)
							}
						}
					}
				})
			}
		}
	}
}

func TestNativeGeneratedShellBoundariesStayPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	const marker = "dummy-generated-private-96"
	payload, _ := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]string{"password": "prefix " + marker}}}})
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, separator := range []string{"=", " "} {
			for _, mode := range []string{"generated-escape", "generated-quote", "literal-escape", "file"} {
				for _, dry := range []bool{true, false} {
					t.Run(fmt.Sprintf("%s/%s/%s/dry=%t", flag, separator, mode, dry), func(t *testing.T) {
						dir := t.TempDir()
						cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
						for _, path := range []string{cases, inputs} {
							if err := os.Mkdir(path, 0o700); err != nil {
								t.Fatal(err)
							}
						}
						if err := os.WriteFile(filepath.Join(inputs, "sandbox-template.yaml"), []byte("secret: "+marker+"\n"), 0o600); err != nil {
							t.Fatal(err)
						}
						source := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `'`, `\'`, `{`, `\{`, `}`, `\}`, " ", `\ `).Replace(string(payload))
						source = strings.Replace(source, marker, `{{.inputs.secret}}`, 1)
						expected := string(payload)
						switch mode {
						case "generated-escape":
							source = strings.Replace(source, `\ {{.inputs.secret}}`, `{{"\\"}} {{.inputs.secret}}`, 1)
						case "generated-quote":
							source = strings.Replace(source, `\ {{.inputs.secret}}`, `{{"\""}} {{.inputs.secret}}{{"\""}}`, 1)
						case "file":
							source, expected = `'@./public config.json'`, "@./public config.json"
						}
						command := "ecctl sandbox template update tpl-1 --openapi --team-id public-team " + flag + separator + source + " --region cn-hangzhou --profile account-a"
						quoted, _ := json.Marshal(command)
						authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: generated boundary\n    run: %s\n", quoted)
						if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
							t.Fatal(err)
						}
						lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
						if err != nil || lint.Invalid != 0 {
							t.Fatalf("authored shell boundary case rejected: %v %+v", err, lint)
						}
						raw, err := vars.Render(command, map[string]any{"inputs": map[string]any{"secret": marker}})
						args, splitErr := shlex.Split(raw)
						if err != nil || splitErr != nil {
							t.Fatalf("invalid rendered shell positive control: %v %v %s", err, splitErr, raw)
						}
						value := ""
						for i, arg := range args {
							if arg == flag && i+1 < len(args) {
								value = args[i+1]
							} else if strings.HasPrefix(arg, flag+"=") {
								value = strings.TrimPrefix(arg, flag+"=")
							}
						}
						if value != expected {
							t.Fatalf("escape/quote did not produce one complete private argv value: %q want %q", value, expected)
						}
						bin, argsPath := filepath.Join(dir, "ecctl"), filepath.Join(dir, "args")
						t.Setenv("FAKE_GENERATED_ARGS", argsPath)
						if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" > \"$FAKE_GENERATED_ARGS\"\necho '{\"error\":{\"message\":\"fixture rejected\"}}'\nexit 1\n"), 0o755); err != nil {
							t.Fatal(err)
						}
						run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "generated-boundary", RunID: "generated-boundary", EcctlBin: bin, Region: "cn-hangzhou", DryRun: dry})
						if err != nil || len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 {
							t.Fatalf("actual dry/live runner did not reach case: %v %+v", err, run)
						}
						step := run.Cases[0].Steps[0]
						want := report.StatusFail
						if dry {
							want = report.StatusSkipped
						}
						if step.Status != want || !strings.Contains(step.Command, "--team-id public-team") {
							t.Errorf("runner result/public prefix lost: %+v", step)
						}
						if (mode == "literal-escape" || mode == "file") && !strings.Contains(step.Command, "--region cn-hangzhou --profile account-a") {
							t.Error("complete ordinary boundary lost public selectors")
						}
						if mode == "file" && !strings.Contains(step.Command, expected) {
							t.Error("valid file reference lost")
						}
						actual, readErr := os.ReadFile(argsPath)
						if dry {
							if !os.IsNotExist(readErr) {
								t.Error("dry run launched child")
							}
						} else if readErr != nil || string(actual) != strings.Join(args[1:], "\n")+"\n" {
							t.Errorf("masking changed actual argv: %s %v", actual, readErr)
						}
						assertNativePrivateArtifacts(t, run, dir, marker)
					})
				}
			}
		}
	}
}

func TestNativeCleanupDeduplicatesHistoricalBindingProbes(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, false)
	body, _ := os.ReadFile(cfg.Bin)
	probe := `#!/usr/bin/env bash
profile=account-a; prev=''
for arg in "$@"; do [[ "$prev" == --profile ]] && profile="$arg"; [[ "$arg" == --profile=* ]] && profile="${arg#--profile=}"; prev="$arg"; done
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then
 echo "$profile" >> "$FAKE_NATIVE_DIR/probes"
 [[ -f "$FAKE_NATIVE_DIR/slow" ]] && sleep 0.04
 if [[ "$profile" == account-a && -f "$FAKE_NATIVE_DIR/revoked" ]]; then echo '{}'; exit 2; fi
 echo '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"user-a"}}'; exit 0
fi
if [[ "$*" == *"template create"* ]]; then
 count=0; [[ -f "$FAKE_NATIVE_DIR/count" ]] && read -r count < "$FAKE_NATIVE_DIR/count"
 count=$((count+1)); echo "$count" > "$FAKE_NATIVE_DIR/count"
 printf '{"template":{"id":"tpl-%s","team_id":"team-1"}}\n' "$count"; exit 0
fi
if [[ "$*" == *"template delete"* ]]; then
 [[ "$ECCTL_EXPECTED_CREDENTIAL_IDENTITY" == *'"UserId":"user-a"'* ]] || { echo '{}'; exit 2; }
 echo delete >> "$FAKE_NATIVE_DIR/deletes"; echo '{"deleted":true}'; exit 0
fi
`
	if err := os.WriteFile(cfg.Bin, []byte(strings.Replace(string(body), "#!/usr/bin/env bash\n", probe, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	var active []*cleanupItem
	for i := 0; i < 31; i++ {
		var scope []*cleanupItem
		step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --openapi --profile account-a --team-id team-1 --name repeated", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
		if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, 5*time.Second); !ok {
			t.Fatalf("healthy repeated creation %d failed: %+v", i, result)
		}
		if i < 30 {
			if failures := cl.run(scope); len(failures) != 0 {
				t.Fatalf("historical cleanup %d failed: %v", i, failures)
			}
		} else {
			active = scope
		}
	}
	paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
	if len(paths) != 31 || len(readJournalEntries(t, cl.journal)) != 1 {
		t.Fatal("test did not retain completed intents plus one active child")
	}
	for name, data := range map[string]string{"probes": "", "slow": "enabled"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := cleanupTimeout
	cleanupTimeout = 500 * time.Millisecond
	defer func() { cleanupTimeout = old }()
	if failures := cl.run(active); len(failures) != 0 {
		t.Errorf("duplicate healthy history consumed implicit cleanup budget: %v", failures)
	}
	probes, _ := os.ReadFile(filepath.Join(dir, "probes"))
	deletes, _ := os.ReadFile(filepath.Join(dir, "deletes"))
	if len(strings.Fields(string(probes))) != 1 || len(strings.Fields(string(deletes))) != 31 || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Errorf("equivalent binding was not probed once with active SDK pin retained: probes=%s deletes=%s", probes, deletes)
	}
	if err := os.WriteFile(filepath.Join(dir, "revoked"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	step := scenario.Step{Name: "new profile", Run: "ecctl sandbox template create --openapi --profile account-b --team-id team-1 --name blocked", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
	var scope []*cleanupItem
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, 5*time.Second); ok || !strings.Contains(result.Error, "cleanup credentials") {
		t.Errorf("fresh preflight reused a stale identity result or skipped original profile: %+v", result)
	}
	var parent []*cleanupItem
	if err := cl.push(&parent, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
		t.Fatal(err)
	}
	if failures := cl.run(parent); len(failures) == 0 {
		t.Error("original profile revocation did not block parent cleanup")
	}
	count, _ := os.ReadFile(filepath.Join(dir, "count"))
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.TrimSpace(string(count)) != "31" || strings.Contains(string(calls), "team-delete") {
		t.Errorf("revoked historical binding reached allocation or parent delete: %s %s", count, calls)
	}
}

func TestNativeSelectorErrorsHideAuthoredPrivateValues(t *testing.T) {
	const marker = "dummy-selector-private-85"
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, selector := range []string{"openapi", "json"} {
			t.Run(flag+"/"+selector, func(t *testing.T) {
				dir := t.TempDir()
				cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
				for _, path := range []string{cases, inputs} {
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				password := "prefix' --" + selector + "=" + marker + " 'suffix"
				payload, _ := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]string{"password": password}}}})
				input, _ := json.Marshal(string(payload))
				if err := os.WriteFile(filepath.Join(inputs, "sandbox-template.yaml"), []byte("private_config: "+string(input)+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				command := "ecctl sandbox template update tpl-1 --openapi --team-id team-1 " + flag + " '{{.inputs.private_config}}'"
				quoted, _ := json.Marshal(command)
				authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: private selector\n    run: %s\n", quoted)
				if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
					t.Fatal(err)
				}
				lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
				if err != nil || lint.Invalid != 0 {
					t.Fatalf("authored selector case was not accepted: %v %+v", err, lint)
				}
				raw, err := vars.Render(command, map[string]any{"inputs": map[string]any{"private_config": string(payload)}})
				if _, splitErr := shlex.Split(raw); err != nil || splitErr != nil || !strings.Contains(raw, "--"+selector+"="+marker) {
					t.Fatalf("invalid balanced-quote positive control: %v %v %s", err, splitErr, raw)
				}
				run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunID: "selector", RunName: "selector", Region: "cn-hangzhou", EcctlBin: filepath.Join(dir, "must-not-execute")})
				if err != nil {
					t.Fatal(err)
				}
				if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 || run.Cases[0].Steps[0].Status != report.StatusError || !strings.Contains(run.Cases[0].Steps[0].Error, "invalid Template transport selector") {
					t.Fatalf("did not reach the pre-launch selector error: %+v", run)
				}
				assertNativePrivateArtifacts(t, run, dir, marker)
			})
		}
	}
}

func TestNativeAllocationRejectsSharedRunTeamAcrossExecutions(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprintf("discard=%t", discard), func(t *testing.T) {
			cfg, _, dir := nativeCleanupFixture(t, false)
			body, _ := os.ReadFile(cfg.Bin)
			script := strings.Replace(string(body), `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"team create"* ]]; then echo shared-team-create >> "$FAKE_NATIVE_DIR/calls"; echo '{"team":{"id":"team-1"}}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then echo unreserved-shared-child >> "$FAKE_NATIVE_DIR/calls"; echo '{"error":{"message":"first response lost"}}'; exit 2; fi
if [[ "$*" == *"template list"* ]]; then echo '{"error":{"message":"reconciliation unavailable"}}'; exit 2; fi
if [[ "$*" == *"template create"* ]]; then`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			stack := filepath.Join(dir, "stack.yaml")
			fixture := "provision:\n  - id: shared-team\n    resource: sandbox/team\n    lifetime: run\n    run: ecctl sandbox team create --name shared-team\n    at: $.team\n    capture: {shared_team: id}\n    teardown: ecctl sandbox team delete {{.shared_team}}\n"
			if err := os.WriteFile(stack, []byte(fixture), 0o600); err != nil {
				t.Fatal(err)
			}
			session := NewSession()
			defer session.Close()
			options := Options{StackFile: stack, Session: session, RunName: "shared-team", RunID: "shared-team", Surface: "public", EcctlBin: cfg.Bin, Region: cfg.Region, StepTimeout: time.Second}
			options.ExecutionID, options.CleanupJournal = "execution-a", filepath.Join(dir, "a.json")
			options.Suites = []*scenario.Suite{{Resource: "sandbox/template", Needs: []string{"shared-team"}, Steps: []scenario.Step{{Name: "read", Run: "ecctl sandbox template get tpl-external --openapi --team-id {{.stack.shared_team}}"}}}}
			a, err := Run(context.Background(), options)
			if err != nil || a.Summary.Failed != 0 {
				t.Fatalf("first execution did not acquire the Team: %v %+v", err, a)
			}
			options.ExecutionID, options.CleanupJournal = "execution-b", filepath.Join(dir, "b.json")
			options.Suites = []*scenario.Suite{{Resource: "sandbox/template", Needs: []string{"shared-team"}, Steps: []scenario.Step{{Name: "child", Run: "ecctl sandbox template create --openapi --team-id {{.stack.shared_team}} --name child", Teardown: "ecctl sandbox template delete never-created --openapi --team-id {{.stack.shared_team}}"}}}}
			b, err := Run(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			refused := false
			for _, c := range b.Cases {
				for _, step := range c.Steps {
					if step.Name == "child" && step.Status == report.StatusError && strings.Contains(step.Error, "shared run-lifetime Team") {
						refused = true
					}
				}
			}
			if !refused {
				t.Errorf("later execution did not refuse the unsupported allocation: %+v", b)
			}
			pending, _ := filepath.Glob(options.CleanupJournal + ".template-create-*.json")
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if len(pending) != 0 || strings.Contains(string(calls), "unreserved-shared-child") || strings.Count(string(calls), "shared-team-create") != 1 {
				t.Errorf("reused Team admitted a later unknown allocation: pending=%v calls=%s", pending, calls)
			}
			var failures []string
			if discard {
				failures = session.Discard("public", cfg.Bin, cfg.Region)
			} else {
				failures = session.Close()
			}
			calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
			if len(pending) > 0 && strings.Contains(string(calls), "team-delete") {
				t.Error("shared parent deletion ignored the later pending journal")
			}
			if len(failures) != 0 || len(pending) == 0 && strings.Count(string(calls), "team-delete") != 1 {
				t.Errorf("safe rejection stranded or duplicated parent cleanup: %v %s", failures, calls)
			}
		})
	}
}

func TestNativeRuntimeCleanupReportsHeldReplayLockDeadline(t *testing.T) {
	cfg, _, dir := nativeCleanupFixture(t, false)
	old := cleanupTimeout
	cleanupTimeout = 40 * time.Millisecond
	defer func() { cleanupTimeout = old }()
	path := filepath.Join(dir, "held-journal.json")
	held, release, lockDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	released := false
	unlock := func() {
		if !released {
			close(release)
			released = true
		}
	}
	defer unlock()
	go func() {
		lockDone <- journalfile.WithReplayLock(context.Background(), path, func() error { close(held); <-release; return nil })
	}()
	select {
	case <-held:
	case err := <-lockDone:
		t.Fatalf("exclusive holder failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("exclusive holder did not start")
	}
	type outcome struct {
		run *report.Run
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		run, err := Run(context.Background(), Options{RunName: "held-lock", RunID: "held-lock", ExecutionID: "execution", Surface: "public", EcctlBin: cfg.Bin, Region: cfg.Region, CleanupJournal: path, Suites: []*scenario.Suite{{Resource: "test/parent", Steps: []scenario.Step{{Name: "read", Run: "ecctl test parent get parent-1", Teardown: "ecctl sandbox team delete team-1"}}}}})
		done <- outcome{run, err}
	}()
	var result outcome
	select {
	case result = <-done:
	case <-time.After(3 * time.Second):
		t.Error("runtime cleanup ignored its finite wait budget")
		unlock() // Ensure the old-code red test cannot hang the Linux suite.
		select {
		case result = <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("runner did not finish after releasing test lock")
		}
	}
	cleanupFailures := 0
	if result.run != nil {
		for _, caseResult := range result.run.Cases {
			if caseResult.Resource == "cleanup" && caseResult.Status == report.StatusFail && strings.Contains(caseResult.Error, "cleanup coordination") && strings.Contains(caseResult.Error, "deadline exceeded") {
				cleanupFailures++
			}
		}
	}
	if result.err != nil || result.run == nil || result.run.Summary.Failed != 1 || cleanupFailures != 1 {
		t.Errorf("lock refusal did not reach the completed report: %v %+v", result.err, result.run)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "team-delete") {
		t.Error("cleanup launched a delete while the exclusive guard was held")
	}
	unlock()
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
}

func TestNativeExpectedValidationExitRunsAssertions(t *testing.T) {
	for _, mode := range []string{"expected", "wrong-assertion", "default-exit"} {
		t.Run(mode, func(t *testing.T) {
			cfg, _, dir := nativeCleanupFixture(t, false)
			body, _ := os.ReadFile(cfg.Bin)
			script := strings.Replace(string(body), `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"template create"* ]]; then echo rejected-create >> "$FAKE_NATIVE_DIR/calls"; echo '{"error":{"kind":"client","code":"InvalidFCTemplateConfig","retryable":false}}'; exit 1; fi
if [[ "$*" == *"template list"* ]]; then echo unexpected-list >> "$FAKE_NATIVE_DIR/calls"; echo '{}'; exit 0; fi
if [[ "$*" == *"template create"* ]]; then`, 1)
			if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
			for _, p := range []string{cases, inputs} {
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			exit, code := "    exit: 1\n", "InvalidFCTemplateConfig"
			if mode == "wrong-assertion" {
				code = "UnexpectedError"
			} else if mode == "default-exit" {
				exit = ""
			}
			authored := "resource: sandbox/template\nsteps:\n  - name: parent\n    run: ecctl sandbox team get team-1\n    teardown: ecctl sandbox team delete team-1\n  - name: validation\n    run: ecctl sandbox template create --openapi --team-id team-1 --name {{.run_name}} --runtime-config '{\"internetAccess\":\"wrong\"}'\n    teardown: ecctl sandbox template delete never-created --openapi --team-id team-1\n" + exit + "    assert:\n      - '$.error.code == \"" + code + "\"'\n"
			if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
				t.Fatal(err)
			}
			lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
			if err != nil || lint.Invalid != 0 {
				t.Fatalf("authored exit/assertion case rejected: %v %+v", err, lint)
			}
			journal := filepath.Join(dir, "expected.json")
			run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "expected-exit", RunID: "expected-exit", ExecutionID: "execution", Surface: "public", EcctlBin: cfg.Bin, Region: cfg.Region, CleanupJournal: journal})
			if err != nil || len(run.Cases) != 1 || len(run.Cases[0].Steps) != 2 {
				t.Fatalf("actual runner did not reach validation: %v %+v", err, run)
			}
			step := run.Cases[0].Steps[1]
			want := report.StatusFail
			if mode == "expected" {
				want = report.StatusPass
			}
			if step.Status != want || step.Exit != 1 || !strings.Contains(step.Stdout, "InvalidFCTemplateConfig") || mode != "default-exit" && len(step.Checks) != 1 {
				t.Errorf("expected-exit/assertion pipeline was bypassed: %+v", step)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			paths, _ := filepath.Glob(journal + ".template-create-*.json")
			if len(paths) != 1 || strings.Count(string(calls), "rejected-create") != 1 || strings.Count(string(calls), "team-delete") != 1 || strings.Contains(string(calls), "unexpected-list") || strings.Contains(string(calls), "template-delete") {
				t.Fatalf("proven rejection lost evidence/parent cleanup: %v %s", paths, calls)
			}
			var evidence map[string]any
			encoded, _ := os.ReadFile(paths[0])
			if json.Unmarshal(encoded, &evidence) != nil || evidence["state"] != "not_allocated" || evidence["no_allocation_code"] != "InvalidFCTemplateConfig" {
				t.Errorf("durable no-allocation evidence lost: %s", encoded)
			}
		})
	}
}

func TestNativeReplayAliasesRequireCreationEvidence(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, alias := range []string{"rm", "remove"} {
			t.Run(fmt.Sprintf("v%d/%s", version, alias), func(t *testing.T) {
				cfg, _, dir := nativeCleanupFixture(t, false)
				body, _ := os.ReadFile(cfg.Bin)
				script := strings.Replace(string(body), "#!/usr/bin/env bash\n", "#!/usr/bin/env bash\necho child-started >> \"$FAKE_NATIVE_DIR/alias-child\"\n", 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "alias.json")
				meta := report.CleanupJournal{Version: version, RunID: "alias", ExecutionID: "execution", Region: cfg.Region, Surface: "public", EcctlBin: cfg.Bin, Entries: []report.Resource{{Teardown: "ecctl sandbox template " + alias + " delete --openapi --team-id team-1 --profile account-a"}}}
				encoded, _ := json.Marshal(meta)
				if err := os.WriteFile(path, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				result, err := sweeper.ReplayJournal(context.Background(), path, cfg)
				if err == nil || result != nil && result.Deleted != 0 {
					t.Errorf("unproven alias passed native ownership gate: %+v %v", result, err)
				}
				if _, err := os.Stat(filepath.Join(dir, "alias-child")); !os.IsNotExist(err) {
					t.Errorf("unproven alias reached a child: %v", err)
				}
			})
		}
	}
}

func assertNativePrivateArtifacts(t *testing.T, run *report.Run, dir, marker string) {
	t.Helper()
	var previous string
	for pass := 0; pass <= 2; pass++ {
		if pass > 0 {
			run.Redact()
		}
		body, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), marker) {
			t.Errorf("private authored value survived report fields at pass %d", pass)
		}
		if pass == 2 && string(body) != previous {
			t.Error("private display redaction was not stable")
		}
		previous = string(body)
		for _, artifact := range []struct {
			name  string
			write func(*report.Run, string) error
		}{{"report.json", report.WriteJSON}, {"report.html", report.WriteHTML}, {"report.xml", report.WriteJUnit}} {
			path := filepath.Join(dir, fmt.Sprintf("%d-%s", pass, artifact.name))
			if err := artifact.write(run, path); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), marker) {
				t.Errorf("private authored value survived %s at pass %d", artifact.name, pass)
			}
		}
	}
}

func TestNativeLiveAuthoredPrivateDisplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	const marker = "dummy-live-private-58"
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, sink := range []string{"case", "stack"} {
			for _, mode := range []string{"well-quoted", "injected-flags", "file"} {
				for _, startFailure := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/start-failure=%t", flag, sink, mode, startFailure), func(t *testing.T) {
						dir := t.TempDir()
						cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
						for _, path := range []string{cases, inputs} {
							if err := os.Mkdir(path, 0o700); err != nil {
								t.Fatal(err)
							}
						}
						password := marker
						if mode == "injected-flags" {
							other := "--build-config"
							if flag == other {
								other = "--runtime-config"
							}
							password = "prefix' --name '" + marker + "' " + other + " 'suffix"
						}
						payload, err := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]string{"password": password}}}})
						if err != nil {
							t.Fatal(err)
						}
						value := string(payload)
						if mode == "file" {
							value = "@./public config.json"
						}
						encoded, _ := json.Marshal(value)
						if err := os.WriteFile(filepath.Join(inputs, "sandbox-template.yaml"), []byte("private_config: "+string(encoded)+"\n"), 0o600); err != nil {
							t.Fatal(err)
						}
						command := `ecctl sandbox template update tpl-1 --openapi --team-id public-team ` + flag + ` '{{.inputs.private_config}}' --region cn-hangzhou --profile account-a`
						data := map[string]any{"inputs": map[string]any{"private_config": value}}
						prerequisites := map[string]any{"sandbox": map[string]any{"template": map[string]any{"inline_config": value}}}
						stack := ""
						if sink == "stack" {
							// A read-only dispatch isolates display masking from the
							// separate native stack-mutation rejection regression.
							command = strings.Replace(command, "update tpl-1", "get tpl-1", 1)
							command = strings.Replace(command, ".inputs.private_config", ".prerequisites.sandbox.template.inline_config", 1)
							data = map[string]any{"prerequisites": prerequisites}
							stack = filepath.Join(dir, "stack.yaml")
						}
						quoted, _ := json.Marshal(command)
						authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: private-display\n    run: %s\n", quoted)
						if stack != "" {
							fixture := fmt.Sprintf("provision:\n  - id: private-display\n    resource: sandbox/template\n    mode: lookup\n    requires_prerequisites: [sandbox.template]\n    run: %s\n", quoted)
							if err := os.WriteFile(stack, []byte(fixture), 0o600); err != nil {
								t.Fatal(err)
							}
							authored = "resource: sandbox/template\nneeds: [private-display]\nsteps:\n  - name: dependent list\n    run: ecctl sandbox template list --openapi --team-id public-team\n"
						}
						if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
							t.Fatal(err)
						}
						lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs, StackFile: stack})
						if err != nil || lint.Invalid != 0 {
							t.Fatalf("actual authored inputs/case/stack lint failed: %v %+v", err, lint)
						}
						argsPath, bin := filepath.Join(dir, "args"), filepath.Join(dir, "ecctl")
						t.Setenv("FAKE_PRIVATE_ARGS", argsPath)
						if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" > \"$FAKE_PRIVATE_ARGS\"\necho '{\"error\":{\"message\":\"fixture rejected\"}}'\nexit 1\n"), 0o755); err != nil {
							t.Fatal(err)
						}
						if startFailure {
							bin = filepath.Join(dir, "missing-child")
						}
						var logs strings.Builder
						run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, StackFile: stack, RunName: "private-display", RunID: "private-display", EcctlBin: bin, Regions: map[string]Region{"primary": {ID: "cn-hangzhou", Prerequisites: prerequisites}}, Logf: func(format string, args ...any) { fmt.Fprintf(&logs, format, args...) }})
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, c := range run.Cases {
							for _, step := range c.Steps {
								if step.Name != "private-display" {
									continue
								}
								found = true
								want := report.StatusFail
								if startFailure {
									want = report.StatusError
								}
								if step.Status != want || !strings.Contains(step.Command, "--region cn-hangzhou --profile account-a") {
									t.Errorf("child result/public selectors lost: %+v", step)
								}
								if mode == "file" && !strings.Contains(step.Command, value) {
									t.Error("valid @file reference disappeared from display")
								}
							}
						}
						if !found || strings.Contains(logs.String(), marker) {
							t.Errorf("private stack log or missing child result: found=%t logs=%s", found, logs.String())
						}
						raw, err := vars.Render(command, data)
						if err != nil {
							t.Fatal(err)
						}
						args, err := shlex.Split(raw)
						if err != nil || mode != "file" && !strings.Contains(raw, marker) {
							t.Fatalf("invalid positive execution control: %v %s", err, raw)
						}
						actual, err := os.ReadFile(argsPath)
						if startFailure {
							if !os.IsNotExist(err) {
								t.Errorf("failed-start child unexpectedly recorded argv: %s %v", actual, err)
							}
						} else if err != nil || string(actual) != strings.Join(args[1:], "\n")+"\n" {
							t.Errorf("display masking changed actual argv: got=%s want=%q err=%v", actual, args[1:], err)
						}
						assertNativePrivateArtifacts(t, run, dir, marker)
					})
				}
			}
		}
	}
}

func TestNativeSharedStackMutationRejectedBeforeLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	const marker = "dummy-stack-private-63"
	for _, path := range []string{"sandbox template create", "--profile account-a sbx tpl new", "sandbox template update tpl-1", "sbx tpl rm tpl-1"} {
		for _, lifetime := range []string{"execution", "run"} {
			for _, response := range []string{"success", "lost"} {
				for _, keep := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/keep=%t", path, lifetime, response, keep), func(t *testing.T) {
						dir := t.TempDir()
						cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
						for _, name := range []string{cases, inputs} {
							if err := os.Mkdir(name, 0o700); err != nil {
								t.Fatal(err)
							}
						}
						t.Setenv("FAKE_STACK_DIR", dir)
						t.Setenv("FAKE_STACK_RESPONSE", response)
						bin := filepath.Join(dir, "ecctl")
						script := `#!/usr/bin/env bash
if [[ "$*" == *"test parent create"* ]]; then echo '{"parent":{"id":"team-1"}}'; exit 0; fi
if [[ "$*" == *"test parent delete"* ]]; then echo delete >> "$FAKE_STACK_DIR/deletes"; echo '{}'; exit 0; fi
echo native-started >> "$FAKE_STACK_DIR/native-started"
if [[ "$FAKE_STACK_RESPONSE" == 'lost' ]]; then echo '{"error":{"message":"first response i/o timeout"}}'; exit 2; fi
echo '{"template":{"id":"tpl-1","team_id":"team-1"}}'
`
						if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
							t.Fatal(err)
						}
						command := "ecctl " + path + ` --openapi --team-id {{.parent_id}} --runtime-config '{"sandboxConfig":{"registryConfig":{"authConfig":{"password":"` + marker + `"}}}}' --region cn-hangzhou`
						quoted, _ := json.Marshal(command)
						stack := filepath.Join(dir, "stack.yaml")
						fixture := fmt.Sprintf("provision:\n  - id: parent\n    resource: test/parent\n    lifetime: %s\n    run: ecctl test parent create\n    at: $.parent\n    capture: {parent_id: id}\n    teardown: ecctl test parent delete {{.parent_id}}\n  - id: private-template\n    resource: sandbox/template\n    lifetime: %s\n    needs: [parent]\n    run: %s\n    at: $.template\n    capture: {template_id: id}\n    teardown: ecctl sandbox template delete {{.template_id}} --openapi --team-id {{.parent_id}}\n", lifetime, lifetime, quoted)
						if err := os.WriteFile(stack, []byte(fixture), 0o600); err != nil {
							t.Fatal(err)
						}
						authored := "resource: sandbox/template\nneeds: [private-template]\nsteps:\n  - name: dependent lookup\n    run: ecctl sandbox template list --openapi --team-id team-1\n"
						if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
							t.Fatal(err)
						}
						lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs, StackFile: stack})
						if err != nil || lint.Invalid != 0 {
							t.Fatalf("selected authored stack was not accepted: %v %+v", err, lint)
						}
						journal := ""
						if keep {
							journal = filepath.Join(dir, "journal.json")
						}
						var session *Session
						if lifetime == "run" {
							session = NewSession()
							defer session.Close()
						}
						var logs strings.Builder
						run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, StackFile: stack, RunName: "stack-reject", RunID: "stack-reject", ExecutionID: "execution-1", Surface: "public", EcctlBin: bin, Region: "cn-hangzhou", Keep: keep, CleanupJournal: journal, Session: session, Logf: func(format string, args ...any) { fmt.Fprintf(&logs, format, args...) }})
						if err != nil {
							t.Fatal(err)
						}
						if session != nil {
							if failures := session.Close(); len(failures) != 0 {
								t.Errorf("parent fixture cleanup failed: %v", failures)
							}
						}
						if _, err := os.Stat(filepath.Join(dir, "native-started")); !os.IsNotExist(err) {
							t.Errorf("native allocation/mutation reached a child without ownership: %v", err)
						}
						found := false
						for _, c := range run.Cases {
							for _, step := range c.Steps {
								if step.Name == "private-template" {
									found = step.Status == report.StatusError && strings.Contains(step.Error, "shared-stack mutations are unsupported") && step.Command == ""
								}
							}
						}
						if !found || len(run.Manifest) != 1 || strings.Contains(logs.String(), marker) {
							t.Errorf("missing safe rejection or unowned cleanup entry/log: %+v logs=%s", run, logs.String())
						}
						intents, err := filepath.Glob(filepath.Join(dir, "*.template-create-*.json"))
						if err != nil || len(intents) != 0 {
							t.Errorf("rejected dispatch created a native intent: %v %v", intents, err)
						}
						if keep {
							entries := readJournalEntries(t, journal)
							if len(entries) != 1 || strings.Contains(entries[0].Teardown, "template") {
								t.Fatalf("unowned native command entered replay journal: %+v", entries)
							}
							result, err := sweeper.ReplayJournal(context.Background(), journal, execpkg.Config{Bin: bin, Region: "cn-hangzhou"})
							if err != nil || result.Deleted != 1 || len(readJournalEntries(t, journal)) != 0 {
								t.Errorf("safe unrelated parent replay was fenced: %+v %v", result, err)
							}
						}
						deletes, _ := os.ReadFile(filepath.Join(dir, "deletes"))
						if string(deletes) != "delete\n" {
							t.Errorf("unrelated parent was leaked or deleted twice: %s", deletes)
						}
						assertNativePrivateArtifacts(t, run, dir, marker)
					})
				}
			}
		}
	}
}

func TestNativeDryReportAdjacentControlActions(t *testing.T) {
	const marker = "dummy-control-private-74"
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, control := range []string{`{{if true}}`, `{{with "public-scope"}}`, `{{range .inputs.repetitions}}`} {
			for _, file := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/file=%t", flag, control, file), func(t *testing.T) {
					dir := t.TempDir()
					cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
					for _, name := range []string{cases, inputs} {
						if err := os.Mkdir(name, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(inputs, "sandbox-template.yaml"), []byte("repetitions: [one]\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					value := `{"sandboxConfig":{"registryConfig":{"authConfig":{"password":"` + marker + `"}}}}`
					if file {
						value = "@./public config.json"
					}
					command := "ecctl sandbox template update tpl-1 --openapi " + control + flag + " '" + value + "'{{end}} --team-id public-team --region cn-hangzhou --profile account-a"
					quoted, _ := json.Marshal(command)
					authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: adjacent control\n    run: %s\n", quoted)
					if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
						t.Fatal(err)
					}
					lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
					if err != nil || lint.Invalid != 0 {
						t.Fatalf("valid control-flow YAML rejected by lint: %v %+v", err, lint)
					}
					run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "control", RunID: "control", Region: "cn-hangzhou", DryRun: true})
					if err != nil {
						t.Fatal(err)
					}
					if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 || run.Cases[0].Steps[0].Status != report.StatusSkipped || run.Cases[0].Steps[0].Error != "" {
						t.Fatalf("private display masking broke valid control structure: %+v", run)
					}
					if file && (!strings.Contains(run.Cases[0].Steps[0].Command, value) || !strings.Contains(run.Cases[0].Steps[0].Command, "--region cn-hangzhou --profile account-a")) {
						t.Error("public @file/control selectors were lost")
					}
					assertNativePrivateArtifacts(t, run, dir, marker)
				})
			}
		}
	}
}

func TestNativeTemplateErrorsNeverEnterReports(t *testing.T) {
	const marker = "dummy-template-error-private-47"
	payload := `{"sandboxConfig":{"registryConfig":{"authConfig":{"password":"` + marker + `"}}}}`
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, builtin := range []string{"index", "slice"} {
			for _, sink := range []string{"case", "stack"} {
				for _, dry := range []bool{true, false} {
					t.Run(fmt.Sprintf("%s/%s/%s/dry=%t", flag, builtin, sink, dry), func(t *testing.T) {
						dir := t.TempDir()
						cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
						for _, path := range []string{cases, inputs} {
							if err := os.Mkdir(path, 0o700); err != nil {
								t.Fatal(err)
							}
						}
						command := fmt.Sprintf(`ecctl sandbox template update tpl-1 --openapi --team-id public-team %s={{%s %q 999}} --region cn-hangzhou --profile account-a`, flag, builtin, payload)
						quoted, err := json.Marshal(command)
						if err != nil {
							t.Fatal(err)
						}
						authored := fmt.Sprintf("resource: sandbox/template\nsteps:\n  - name: rejected private action\n    run: %s\n", quoted)
						stack := ""
						if sink == "stack" {
							stack = filepath.Join(dir, "stack.yaml")
							fixture := fmt.Sprintf("provision:\n  - id: private-action\n    resource: sandbox/template\n    run: %s\n", quoted)
							if err := os.WriteFile(stack, []byte(fixture), 0o600); err != nil {
								t.Fatal(err)
							}
							authored = "resource: sandbox/template\nneeds: [private-action]\nsteps:\n  - name: dependent list\n    run: ecctl sandbox template list --openapi --team-id public-team\n"
						}
						if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
							t.Fatal(err)
						}
						lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs, StackFile: stack})
						if err != nil || lint.Invalid != 0 {
							t.Fatalf("authored failing action did not pass actual lint: %v %+v", err, lint)
						}
						// Rendering must fail before any child can be launched,
						// including in the normal execution and stack paths.
						var logs strings.Builder
						run, err := Run(context.Background(), Options{
							CasesDir: cases, InputsDir: inputs, StackFile: stack,
							RunName: "private-action", RunID: "private-action", DryRun: dry,
							EcctlBin: filepath.Join(dir, "must-not-execute"), Region: "cn-hangzhou",
							Logf: func(format string, args ...any) { fmt.Fprintf(&logs, format, args...) },
						})
						if err != nil {
							t.Fatal(err)
						}
						found := false
						for _, c := range run.Cases {
							for _, step := range c.Steps {
								if step.Status != report.StatusError {
									continue
								}
								found = true
								if !strings.Contains(step.Error, "template: v:1:") || !strings.Contains(step.Error, "evaluation error") {
									t.Errorf("template failure lost its safe position/category: %s", step.Error)
								}
								if (sink == "stack" || dry) && !strings.Contains(c.Error, "evaluation error") {
									t.Errorf("case did not retain its safe template error: %+v", c)
								}
							}
						}
						if !found {
							t.Fatalf("accepted action did not reach the intended render failure: %+v", run)
						}
						if strings.Contains(logs.String(), marker) {
							t.Error("private action entered runner logs")
						}
						var previous string
						for pass := 0; pass <= 2; pass++ {
							if pass > 0 {
								run.Redact()
							}
							body, err := json.Marshal(run)
							if err != nil {
								t.Fatal(err)
							}
							if strings.Contains(string(body), marker) {
								t.Errorf("private literal survived in report fields at pass %d", pass)
							}
							if pass == 2 && string(body) != previous {
								t.Error("template-error redaction was not stable")
							}
							previous = string(body)
							for _, artifact := range []struct {
								name  string
								write func(*report.Run, string) error
							}{{"report.json", report.WriteJSON}, {"report.html", report.WriteHTML}} {
								path := filepath.Join(dir, fmt.Sprintf("%d-%s", pass, artifact.name))
								if err := artifact.write(run, path); err != nil {
									t.Fatal(err)
								}
								body, err := os.ReadFile(path)
								if err != nil {
									t.Fatal(err)
								}
								if strings.Contains(string(body), marker) {
									t.Errorf("private literal survived %s at pass %d", artifact.name, pass)
								}
							}
						}
					})
				}
			}
		}
	}
	t.Run("parse-error", func(t *testing.T) {
		_, err := vars.Render("{{printf "+fmt.Sprintf("%q", payload), nil)
		if err == nil || strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "parse error") {
			t.Fatalf("unsafe or missing parse failure: %v", err)
		}
	})
}

func TestNativeDryRunRedactsMalformedInterpolatedConfiguration(t *testing.T) {
	const marker = "dummy-interpolated-private-36"
	const placeholder = "{{.prerequisites.sandbox.template.inline_config}}"
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		prefix := strings.TrimSuffix(flag, "-config")
		for _, form := range []struct{ name, source string }{
			{"literal", flag + "='" + placeholder + "'"},
			{"segmented", prefix + `"-config"='` + placeholder + "'"},
			{"escaped", prefix + `\-config='` + placeholder + "'"},
			{"escaped-equals", prefix + `"-config"\='` + placeholder + "'"},
			{"quoted-prefix", `'-''` + flag[1:] + "=" + placeholder + "'"},
			{"separated", prefix + `"-config" '` + placeholder + "'"},
			{"generated-literal", fmt.Sprintf(`{{printf "%%s" %q}}='`, flag) + placeholder + "'"},
			{"generated-prerequisite", `{{.prerequisites.sandbox.template.flag}}='` + placeholder + "'"},
			{"generated-variable", `{{$privateFlag}}='{{$privateValue}}'`},
			{"comment-prefixed", `{{/* key }} comment */}}` + flag + "='" + placeholder + "'"},
			{"comment-middle", prefix + `{{/* key */}}-config='` + placeholder + "'"},
			{"generated-delimiter", `{{printf "%s=" $privateFlag}}'{{$privateValue}}'`},
			{"generated-separated", `{{$privateFlag}} '{{$privateValue}}'`},
			{"whole-generated-inline", `{{printf "%s='%s'" $privateFlag $privateValue}}`},
			{"whole-generated-separated", `{{printf "%s '%s'" $privateFlag $privateValue}}`},
			{"trim-right", prefix + `{{"" -}} -config='` + placeholder + "'"},
			{"trim-left", prefix + ` {{- ""}}-config='` + placeholder + "'"},
			{"trim-both", prefix + `{{- "" -}} -config='` + placeholder + "'"},
			{"trim-whitespace", prefix + " \t\r\n" + `{{- "" -}}` + " \t\r\n-config='" + placeholder + "'"},
			{"trim-comment", prefix + ` {{- /* key }} comment */ -}} -config='` + placeholder + "'"},
			{"trim-generated-delimiter", `{{$privateFlag}} {{- "=" -}} '{{$privateValue}}'`},
		} {
			for _, mode := range []string{"valid", "apostrophe", "apostrophe-space", "balanced-apostrophes", "file", "file-spaces", "file-injection"} {
				t.Run(flag+"/"+form.name+"/"+mode, func(t *testing.T) {
					malformed := strings.HasPrefix(mode, "apostrophe")
					file := mode == "file" || mode == "file-spaces"
					dir := t.TempDir()
					cases, inputs := filepath.Join(dir, "cases"), filepath.Join(dir, "inputs")
					for _, path := range []string{cases, inputs} {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					command := `ecctl sandbox template update tpl-1 --openapi {{$privateFlag := .prerequisites.sandbox.template.flag}}{{$privateValue := .prerequisites.sandbox.template.inline_config}}{{$publicRegion := "cn-hangzhou"}}--team-id public-team ` + form.source + ` --re{{"" -}} gion {{$publicRegion}} --pro {{- ""}}file account-a`
					quoted, err := json.Marshal(command)
					if err != nil {
						t.Fatal(err)
					}
					authored := fmt.Sprintf("resource: sandbox/template\nrequires_prerequisites: [sandbox.template]\nsteps:\n  - name: interpolate private config\n    run: %s\n", quoted)
					if err := os.WriteFile(filepath.Join(cases, "template.yaml"), []byte(authored), 0o600); err != nil {
						t.Fatal(err)
					}
					lint, err := caselint.Check(caselint.Options{CasesDir: cases, InputsDir: inputs})
					if err != nil || lint.Invalid != 0 {
						t.Fatalf("authored case did not pass actual lint: %v %+v", err, lint)
					}
					password := marker
					switch mode {
					case "apostrophe":
						password = "prefix'" + marker
					case "apostrophe-space":
						password = "prefix' " + marker
					case "balanced-apostrophes":
						password = "prefix' " + marker + " 'suffix"
					}
					payload, err := json.Marshal(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": password}}}})
					if err != nil {
						t.Fatal(err)
					}
					value := string(payload)
					if mode == "file" {
						value = "@./public-config.json"
					} else if mode == "file-spaces" {
						value = "@./public config.json"
					} else if mode == "file-injection" {
						value = "@./public' " + marker + " 'config.json"
					}
					prerequisites := map[string]any{"sandbox": map[string]any{"template": map[string]any{"inline_config": value, "flag": flag}}}
					run, err := Run(context.Background(), Options{CasesDir: cases, InputsDir: inputs, RunName: "private-interpolation", RunID: "private-interpolation", DryRun: true, Regions: map[string]Region{"primary": {ID: "cn-hangzhou", Prerequisites: prerequisites}}})
					if err != nil {
						t.Fatal(err)
					}
					if len(run.Cases) != 1 || len(run.Cases[0].Steps) != 1 || run.Summary.Skipped != 1 {
						t.Fatalf("actual dry-run did not render the accepted case: %+v", run)
					}
					raw, err := vars.Render(command, map[string]any{"prerequisites": prerequisites})
					if err != nil {
						t.Fatal(err)
					}
					_, parseErr := shlex.Split(raw)
					if (parseErr != nil) != malformed || !file && !strings.Contains(raw, marker) {
						t.Fatalf("runtime interpolation did not form the intended control: parse=%v command=%s", parseErr, raw)
					}
					if strings.Contains(run.Cases[0].Steps[0].Command, marker) {
						t.Error("private substitution entered the recorded dry-run command")
					}
					run.Redact()
					redacted := run.Cases[0].Steps[0].Command
					// Incomplete rendered quoting cannot prove an argument end;
					// fail-closed masking may consume the following public suffix.
					if strings.Contains(redacted, marker) || !strings.Contains(redacted, "--team-id public-team") || !malformed && !strings.Contains(redacted, "--region cn-hangzhou --profile account-a") {
						t.Errorf("authored private boundary or public selectors were lost: %s", redacted)
					}
					// Retain the existing raw-text fallback controls. Balanced
					// substitutions require the authored boundary tested above.
					if mode != "balanced-apostrophes" && mode != "file-injection" {
						text := report.Scrub("retry: " + raw)
						if strings.Contains(text, marker) || !strings.Contains(text, "--team-id public-team") || !malformed && !strings.Contains(text, "--region cn-hangzhou --profile account-a") {
							t.Errorf("interpolated private value leaked or known public flags disappeared: %s", text)
						}
					}
					if file && redacted != raw {
						t.Error("redaction changed a valid interpolated @file reference")
					}
					run.Redact()
					if run.Cases[0].Steps[0].Command != redacted {
						t.Error("interpolated configuration redaction was not stable")
					}
					for _, artifact := range []struct {
						name  string
						write func(*report.Run, string) error
					}{{"report.json", report.WriteJSON}, {"report.html", report.WriteHTML}} {
						path := filepath.Join(dir, artifact.name)
						if err := artifact.write(run, path); err != nil {
							t.Fatal(err)
						}
						body, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if strings.Contains(string(body), marker) || !strings.Contains(string(body), "public-team") || !malformed && !strings.Contains(string(body), "cn-hangzhou") {
							t.Errorf("malformed interpolated credential survived %s", artifact.name)
						}
					}
				})
			}
		}
	}
}

func TestNativeDryRunRedactsCompletePrivateArguments(t *testing.T) {
	const marker = "dummy-private-Q15"
	privateConfig := `{"sandboxConfig":{"registryConfig":{"authConfig":{"password":"` + marker + `"}}}}`
	values := []string{
		"'{}''" + marker + "'", "'{}'" + marker, "'{}'\"" + marker + " suffix\"",
		"{}'" + marker + "'", "\"{}\"'" + marker + "'", "'{}'\\ " + marker,
		"'***''" + marker + "'", "***\"" + marker + "\"", "'malformed " + marker + "'",
		`{"number":1e10000,"value":"` + marker + ` with spaces"}`,
		`'{"value":"quote'"'"'` + marker + `"}'`,
	}
	var commands []string
	for _, flag := range []string{"--runtime-config", "--build-config"} {
		for _, separator := range []string{" ", "="} {
			for _, value := range values {
				commands = append(commands, "ecctl sandbox template update tpl-1 --openapi "+flag+separator+value+" --team-id public-team --name public-name")
			}
		}
		for _, value := range []string{"{}", "***"} {
			commands = append(commands, "ecctl sandbox template update tpl-1 --openapi '"+flag+"="+value+"''"+marker+"' --team-id public-team --name public-name")
		}
		// shlex accepts space, Tab, CR and LF. Quoted YAML scalars below
		// retain CRLF verbatim instead of normalizing it to a single LF.
		for _, separator := range []string{" ", "=", "\t", "\r", "\n", "\r\n", " \t\r\n ", " # ignored\n"} {
			prefix := strings.TrimSuffix(flag, "-config")
			for _, flagToken := range []string{flag, "'" + flag + "'", "\"" + flag + "\"", prefix + `"-config"`, prefix + `\-config`, prefix + `'-config'`, strings.Replace(flag, "--", `'-''-'`, 1)} {
				for _, value := range []string{"'" + privateConfig + "'", "'" + privateConfig + "''" + marker + "'", "'***''" + marker + "'", "@./public-config.json"} {
					commands = append(commands, "ecctl sandbox template update tpl-1 --openapi "+flagToken+separator+value+" --team-id public-team --name public-name")
				}
			}
		}
	}
	commands = append(commands, "ecctl sandbox template update tpl-1 --openapi --runtime-config @./public-config.json --team-id public-team --name public-name")
	dir := t.TempDir()
	cases := filepath.Join(dir, "cases", "t")
	if err := os.MkdirAll(cases, 0o700); err != nil {
		t.Fatal(err)
	}
	var suite strings.Builder
	suite.WriteString("resource: t/thing\nsteps:\n")
	for index, command := range commands {
		quoted, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&suite, "  - name: private-%d\n    run: %s\n", index, quoted)
	}
	// Reproduce the literal block form used by real multiline case files.
	for index, flag := range []string{"--runtime-config", "--build-config"} {
		head := "ecctl sandbox template update tpl-1 --openapi " + flag
		tail := "'" + privateConfig + "' --team-id public-team --name public-name"
		fmt.Fprintf(&suite, "  - name: literal-block-%d\n    run: |\n      %s\n      %s\n", index, head, tail)
		commands = append(commands, head+"\n"+tail+"\n")
	}
	if err := os.WriteFile(filepath.Join(cases, "thing.yaml"), []byte(suite.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunName: "private-dry-run", RunID: "private-dry-run", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Cases) != 1 || len(run.Cases[0].Steps) != len(commands) || run.Summary.Skipped != 1 {
		t.Fatalf("actual dry-run did not render all private arguments: %+v", run)
	}
	for index, step := range run.Cases[0].Steps {
		if !strings.Contains(commands[index], marker) && !strings.Contains(commands[index], "@./public-config.json") {
			t.Fatalf("private argument fixture lost its positive marker control at %d", index)
		}
		if strings.Contains(step.Command, marker) || !strings.Contains(step.Command, "--team-id public-team --name public-name") {
			t.Fatalf("dry-run recorded a private value or lost public flags at %d: %q", index, step.Command)
		}
	}
	run.Redact()
	var redacted []string
	for index, step := range run.Cases[0].Steps {
		if strings.Contains(step.Command, marker) || !strings.Contains(step.Command, "--team-id public-team --name public-name") {
			t.Errorf("private token leaked or public flags disappeared at %d: %s", index, step.Command)
		}
		if strings.Contains(commands[index], "@./public-config.json") && step.Command != commands[index] {
			t.Error("redaction changed the public @file reference")
		}
		diagnostic := report.Scrub("retry: " + commands[index])
		if strings.Contains(diagnostic, marker) || !strings.Contains(diagnostic, "--team-id public-team --name public-name") {
			t.Errorf("private flag spelling bypassed diagnostic redaction at %d: %s", index, diagnostic)
		}
		redacted = append(redacted, step.Command)
	}
	run.Redact()
	for index, step := range run.Cases[0].Steps {
		if step.Command != redacted[index] {
			t.Errorf("repeated private argument redaction changed public suffix: %s", step.Command)
		}
	}
	for _, artifact := range []struct {
		name  string
		write func(*report.Run, string) error
	}{{"report.json", report.WriteJSON}, {"report.html", report.WriteHTML}} {
		path := filepath.Join(dir, artifact.name)
		if err := artifact.write(run, path); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), marker) || !strings.Contains(string(body), "public-team") {
			t.Errorf("dry-run artifact leaked private token or lost public flags: %s", artifact.name)
		}
	}
}

func TestNativeFinalizerRequiresOriginalLedgerBundle(t *testing.T) {
	for _, kind := range []string{"missing-bundle", "erased-references", "ancestor-rebound"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprint(explicit)+"/"+kind, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, false)
				var originalDir, otherDir, link string
				if kind == "ancestor-rebound" {
					originalDir, otherDir, link = filepath.Join(dir, "original"), filepath.Join(dir, "other"), filepath.Join(dir, "journal-parent")
					for _, path := range []string{originalDir, otherDir} {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(originalDir, link); err != nil {
						t.Fatal(err)
					}
					requested := filepath.Join(link, "journal.json")
					canonical, err := journalfile.ResolveJournalPath(requested)
					if err != nil {
						t.Fatal(err)
					}
					cl.requestedJournal = requested
					cl.journalPaths = map[string]string{"primary": canonical}
					cl.journal = canonical
				}
				var scope []*cleanupItem
				nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
				paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
				moved := t.TempDir()
				if kind != "ancestor-rebound" {
					var ledger report.CleanupJournal
					body, _ := os.ReadFile(cl.journal)
					if json.Unmarshal(body, &ledger) != nil {
						t.Fatal(string(body))
					}
					for _, path := range append(paths, cl.journal) {
						if err := os.Rename(path, filepath.Join(moved, filepath.Base(path))); err != nil {
							t.Fatal(err)
						}
					}
					if kind == "erased-references" {
						ledger.NativeIntentPaths, ledger.Entries = nil, nil
						body, _ = json.Marshal(ledger)
						if err := os.WriteFile(cl.journal, body, 0o600); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(otherDir, link); err != nil {
						t.Fatal(err)
					}
				}
				calls := 0
				cl.runCommand = func(_ context.Context, bound execpkg.Config, _ string) execpkg.Result {
					calls++
					if bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != scope[0].templateBinding.Identity {
						t.Error("finalizer lost its retained STS expectation")
					}
					return execpkg.Result{Started: true, Exit: 0}
				}
				if explicit {
					result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: "delete", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1"}, time.Second)
					argv, _ := os.ReadFile(filepath.Join(dir, "calls"))
					if ok || result.Command != "" || strings.Contains(string(argv), "template-delete") || scope[0].done {
						t.Errorf("missing/rebound evidence allowed explicit delete: %+v calls=%s", result, argv)
					}
				} else {
					t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
					if failures := cl.run(scope); len(failures) == 0 || calls != 0 || scope[0].done {
						t.Errorf("missing/rebound evidence allowed native finalizer: calls=%d failures=%v done=%v", calls, failures, scope[0].done)
					}
					t.Setenv("FAKE_PRINCIPAL_USER", "user-a")
				}
				if kind != "ancestor-rebound" {
					for _, path := range append(paths, cl.journal) {
						if err := os.Rename(filepath.Join(moved, filepath.Base(path)), path); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(originalDir, link); err != nil {
						t.Fatal(err)
					}
				}
				if failures := cl.run(scope); len(failures) != 0 || calls != 1 || !scope[0].done {
					t.Errorf("restored original evidence could not resume: calls=%d failures=%v done=%v", calls, failures, scope[0].done)
				}
			})
		}
	}
}

func TestTeamFinalizerRejectsMissingJournalParentBeforeLock(t *testing.T) {
	cfg, _, dir := nativeCleanupFixture(t, false)
	parent := filepath.Join(dir, "journals")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(parent, "journal.json"),
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	var scope []*cleanupItem
	if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
		t.Fatal(err)
	}
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	calls := 0
	cl.runCommand = func(context.Context, execpkg.Config, string) execpkg.Result {
		calls++
		return execpkg.Result{Started: true, Exit: 0}
	}
	if failures := cl.run(scope); len(failures) == 0 || calls != 0 || scope[0].done {
		t.Fatalf("missing journal parent allowed finalizer: failures=%v calls=%d done=%v", failures, calls, scope[0].done)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("finalizer recreated missing journal parent: %v", err)
	}
	if err := os.Rename(moved, parent); err != nil {
		t.Fatal(err)
	}
	if failures := cl.run(scope); len(failures) != 0 || calls != 1 || !scope[0].done || len(readJournalEntries(t, cl.journal)) != 0 {
		t.Fatalf("restored parent could not retire finalizer: failures=%v calls=%d done=%v", failures, calls, scope[0].done)
	}
}

func TestOrdinaryTeamRegistrationPinsJournalParent(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		name := "missing"
		if replaced {
			name = "replaced"
		}
		t.Run(name, func(t *testing.T) {
			cfg, _, dir := nativeCleanupFixture(t, false)
			parent := filepath.Join(dir, "journals")
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(parent, "journal.json"),
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			var scope []*cleanupItem
			if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatalf("first ordinary Team registration: %v", err)
			}
			if got := readJournalEntries(t, cl.journal); len(got) != 1 || got[0].Teardown != scope[0].cmd {
				t.Fatalf("first registration did not read and write the journal: %+v", got)
			}
			moved := parent + "-moved"
			if err := os.Rename(parent, moved); err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-2", "primary", nil); err == nil || len(scope) != 1 {
				t.Fatalf("lost journal parent accepted a second finalizer: err=%v scope=%d", err, len(scope))
			}
			if _, err := os.Stat(cl.journal); !os.IsNotExist(err) {
				t.Fatalf("lost journal parent received a new journal: %v", err)
			}
			if replaced {
				if err := os.RemoveAll(parent); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Stat(parent); !os.IsNotExist(err) {
				t.Fatalf("registration recreated the missing parent: %v", err)
			}
			if err := os.Rename(moved, parent); err != nil {
				t.Fatal(err)
			}
			if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-2", "primary", nil); err != nil {
				t.Fatalf("restored parent could not register finalizer: %v", err)
			}
			if got := readJournalEntries(t, cl.journal); len(got) != 2 || got[0].Teardown != scope[0].cmd || got[1].Teardown != scope[1].cmd {
				t.Fatalf("restored journal lost Team finalizers: %+v", got)
			}
		})
	}
}

func TestOrdinaryTeamRegistrationRejectsMissingNativeJournalParent(t *testing.T) {
	cfg, _, dir := nativeCleanupFixture(t, false)
	parent := filepath.Join(dir, "journals")
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(parent, "journal.json"),
		report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
	cl.nativeJournals = map[string]report.CleanupJournal{"primary": {NativeJournalPath: cl.journal}}
	var scope []*cleanupItem
	if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-1", "primary", nil); err == nil || len(scope) != 0 {
		t.Fatalf("retained native journal with missing parent accepted ordinary Team finalizer: err=%v scope=%d", err, len(scope))
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("retained native journal parent was recreated: %v", err)
	}
}

func TestNativeParentFinalizerRejectsJournalDirectoryLostAfterRead(t *testing.T) {
	for _, kind := range []string{"required", "loaded", "references-only", "delete-entry-only", "legacy-v2", "legacy-array"} {
		t.Run(kind, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, false)
			parent := filepath.Join(dir, "journals")
			cl = newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(parent, "journal.json"),
				report.CleanupJournal{RunID: "run", ExecutionID: "execution", Surface: "public"}, func(string, ...any) {})
			var scope []*cleanupItem
			if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			legacy := strings.HasPrefix(kind, "legacy-")
			var intent *templateCreateIntent
			if !legacy {
				var err error
				intent, err = reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --team-id team-1 --name pending", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
				if err != nil {
					t.Fatal(err)
				}
			}
			original, err := os.ReadFile(cl.journal)
			if err != nil {
				t.Fatal(err)
			}
			var ledger report.CleanupJournal
			if err := json.Unmarshal(original, &ledger); err != nil {
				t.Fatal(err)
			}
			if kind != "required" {
				// The guard must also honor evidence learned from disk alone.
				cl.nativeJournals = nil
			}
			switch kind {
			case "references-only":
				ledger.NativeJournalPath = ""
			case "delete-entry-only":
				ledger.NativeJournalPath, ledger.NativeIntentPaths = "", nil
				ledger.Entries = append(ledger.Entries, report.Resource{Teardown: intent.deleteCommand("tpl-1")})
			}
			body, err := json.Marshal(ledger)
			if kind == "legacy-array" {
				body, err = json.Marshal(ledger.Entries)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(cl.journal); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("mkfifo", "-m", "600", cl.journal).CombinedOutput(); err != nil {
				t.Fatalf("create local FIFO: %v: %s", err, output)
			}
			moved := parent + "-moved"
			moveDone := make(chan error, 1)
			go func() {
				// Opening the writer requires loadTemplateLedger's reader.
				// Withhold EOF until after rename so ReadDir sees ENOENT.
				f, err := os.OpenFile(cl.journal, os.O_WRONLY, 0)
				if err == nil {
					_, err = f.Write(body)
					if err == nil {
						err = os.Rename(parent, moved)
					}
					if closeErr := f.Close(); err == nil {
						err = closeErr
					}
				}
				moveDone <- err
			}()
			restore := func() {
				if err := os.Remove(filepath.Join(moved, "journal.json")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(moved, "journal.json"), original, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(moved, parent); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			cl.runCommand = func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
				calls++
				if legacy {
					if bound.ExpectedIdentity != nil {
						t.Error("legacy cleanup acquired an unrelated native identity pin")
					}
				}
				return execpkg.Run(ctx, bound, command)
			}
			failures := cl.run(scope)
			select {
			case err := <-moveDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ledger reader did not complete the deterministic directory move")
			}
			argv, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if len(failures) == 0 || calls != 0 || scope[0].done || scope[0].running || strings.Contains(string(argv), "team-delete") {
				t.Fatalf("lost journal directory launched parent cleanup: calls=%d failures=%v done=%v argv=%s", calls, failures, scope[0].done, argv)
			}
			if _, err := os.Stat(parent); !os.IsNotExist(err) {
				t.Fatalf("cleanup recreated the lost journal parent: %v", err)
			}
			if legacy {
				if _, err := cl.prepareOwnedTeamDelete(scope, scope[0].cmd, "primary", scope[0]); err == nil {
					t.Fatal("Team ownership lookup accepted the lost journal parent")
				}
				if _, err := os.Stat(parent); !os.IsNotExist(err) {
					t.Fatalf("Team ownership lookup recreated the lost journal parent: %v", err)
				}
			}
			restore()
			if legacy {
				if failures := cl.run(scope); len(failures) != 0 || calls != 1 || !scope[0].done {
					t.Fatalf("restored legacy parent cleanup refused: calls=%d failures=%v done=%v", calls, failures, scope[0].done)
				}
				argv, _ = os.ReadFile(filepath.Join(dir, "calls"))
				if strings.Count(string(argv), "team-delete\n") != 1 {
					t.Fatalf("restored legacy parent cleanup did not run exactly once: %s", argv)
				}
				return
			}
			// Restoring the bundle alone must still preserve its pending fence.
			if failures := cl.run(scope); len(failures) == 0 || calls != 0 || scope[0].done {
				t.Fatalf("restored pending allocation allowed cleanup: calls=%d failures=%v", calls, failures)
			}
			started, exit := false, 1
			intent.State, intent.Started, intent.Exit = "not_started", &started, &exit
			if err := intent.save(); err != nil {
				t.Fatal(err)
			}
			if failures := cl.run(scope); len(failures) != 0 || calls != 1 || !scope[0].done {
				t.Fatalf("resolved original bundle could not resume cleanup: calls=%d failures=%v done=%v", calls, failures, scope[0].done)
			}
			argv, _ = os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(argv), "team-delete\n") != 1 {
				t.Fatalf("parent cleanup did not launch exactly once after resolution: %s", argv)
			}
		})
	}
}

func TestNativeReadFieldsPassThroughRunner(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, action := range []string{"get", "show", "describe", "list", "ls"} {
			t.Run(fmt.Sprint(owned)+"/"+action, func(t *testing.T) {
				cfg, cl, dir := nativeCleanupFixture(t, true)
				var scope []*cleanupItem
				if owned {
					nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
				}
				body, _ := os.ReadFile(cfg.Bin)
				script := strings.Replace(string(body), "echo '{}'", `echo "$*" >> "$FAKE_NATIVE_DIR/projection-calls"
echo '{}'`, 1)
				if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				command := "ecctl sbx tpl " + action
				if action != "list" && action != "ls" {
					command += " tpl-1"
				}
				command += " --openapi --team-id team-1 --fields id"
				result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, scenario.Step{Name: action, Run: command}, time.Second)
				calls, _ := os.ReadFile(filepath.Join(dir, "projection-calls"))
				if !ok || !(strings.Contains(string(calls), "--fields=id") || strings.Contains(string(calls), "--fields id")) {
					t.Errorf("valid projection rejected/lost: %+v calls=%s", result, calls)
				}
			})
		}
	}
}
