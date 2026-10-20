package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func TestNativeCreateUsesFinalCLIBooleanSemantics(t *testing.T) {
	for _, flags := range []string{"--openapi=1", "--openapi=t", "--openapi=T", "--openapi=TRUE", "--openapi=True", "--openapi=false --openapi=1"} {
		if !isNativeTemplateCreate("ecctl --profile rm sbx tpl create --name remove --team-id team-1 " + flags) {
			t.Errorf("legal native boolean bypassed reservation: %s", flags)
		}
	}
	for _, flags := range []string{"--openapi=true --openapi=0", "--openapi=true --openapi=false", "--openapi=FALSE"} {
		if isNativeTemplateCreate("ecctl sandbox template create " + flags) {
			t.Errorf("final false classified as native: %s", flags)
		}
	}
}

func TestNativeTextResponseLossInvokesOnceWithDurableIntent(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(body), `echo '{"template":{"id":"tpl-1","team_id":"team-1"}}'; exit 0`, `echo 'i/o timeout first response lost'; exit 2`, 1)
	script = strings.Replace(script, "echo '{}'", `if [[ "$*" == *"template list"* ]]; then echo '{"templates":[],"pagination":{"has_more":false}}'; exit 0; fi
echo '{}'`, 1)
	if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := stepRetryDelays
	stepRetryDelays = []time.Duration{0}
	defer func() { stepRetryDelays = old }()
	var scope []*cleanupItem
	step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --output text --openapi=0 --openapi=TRUE --team-id team-1 --name delete", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
	result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
	if ok || strings.Count(string(calls), "create profile=") != 1 || len(paths) != 1 || !strings.Contains(result.Stdout, "first response lost") || !strings.Contains(result.Error, "pending intent") {
		t.Errorf("lost native text response was replayed or unfenced: %+v %s %v", result, calls, paths)
	}
}

func TestNativeUnstartedProcessReleasesOnlyProvenNoAllocationFence(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, true)
	intent, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi=1 --team-id team-1 --name native", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.Bin); err != nil {
		t.Fatal(err)
	}
	result := execpkg.Run(context.Background(), intent.cfg, intent.command)
	var scope []*cleanupItem
	if result.Started {
		t.Fatal("missing binary started")
	}
	if err := finishTemplateCreate(result, cl, &scope, map[string]any{}, nil, time.Second, intent); err == nil || intent.State != "not_started" {
		t.Fatalf("unstarted allocation remained uncertain: %v %+v", err, intent)
	}
	meta, _, path := cl.journalValues(&cleanupItem{role: "primary"})
	if err := journalfile.CheckTemplateCreateIntents(path, meta, cfg.Env); err != nil {
		t.Fatalf("proven unstarted intent blocked cleanup: %v", err)
	}
}

func TestNativeDeleteEquivalentFinalValuesRetireOnce(t *testing.T) {
	for _, suffix := range []string{"--profile account-b --profile account-a --", "--team-id other-team --team-id team-1 --region other-region --region cn-hangzhou --"} {
		t.Run(suffix, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			step := scenario.Step{Name: "delete", Run: "ecctl sbx tpl rm tpl-1 --openapi=1 --team-id team-1 " + suffix}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if !ok || len(readJournalEntries(t, cl.journal)) != 0 || strings.Count(string(calls), "template-delete") != 1 {
				t.Errorf("equivalent final values failed retirement: %+v %s", result, calls)
			}
		})
	}
}

func TestNativePrincipalSwitchFencesAllCleanup(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	var scope []*cleanupItem
	nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
	paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
	body, _ := os.ReadFile(paths[0])
	var intent map[string]any
	_ = json.Unmarshal(body, &intent)
	if strings.Contains(string(body), "dummy-unused-secret") || strings.Contains(string(body), "ACCESS_KEY") || intent["identity"] == nil {
		t.Errorf("intent omitted public pin or persisted credential data: %s", body)
	}
	t.Setenv("FAKE_PRINCIPAL_USER", "user-b")
	step := scenario.Step{Name: "delete", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1"}
	if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second); ok || result.Command != "" {
		t.Errorf("changed principal reached explicit deletion: %+v", result)
	}
	cl.keep = false
	if failures := cl.run(scope); len(failures) != 1 || scope[0].done {
		t.Errorf("changed principal reached finalizer: %v %+v", failures, scope[0])
	}
	if result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err == nil {
		t.Errorf("changed principal reached replay: %+v", result)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(calls), "template-delete") {
		t.Errorf("wrong identity sent destructive request: %s", calls)
	}
}

func TestNativeProfileActionWordsDoNotStrandCreation(t *testing.T) {
	for _, name := range []string{"rm", "remove", "delete"} {
		t.Run(name, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			t.Setenv("ECCTL_PROFILE", name)
			var scope []*cleanupItem
			step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --name remove --team-id team-1 --openapi", At: "$.template", Capture: map[string]string{"template_id": "id"}, Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if !ok || strings.Count(string(calls), "create profile=") != 1 {
				t.Fatalf("flag value became action and stranded intent: %+v %s", result, calls)
			}
		})
	}
}

func TestNativeDeleteFinalSelectorsCannotBypassOwnership(t *testing.T) {
	for _, suffix := range []string{"--profile account-b --", "--profile account-a --profile account-b", "--team-id team-1 --team-id other-team", "--region cn-hangzhou --region other-region"} {
		t.Run(suffix, func(t *testing.T) {
			cfg, cl, dir := nativeCleanupFixture(t, true)
			t.Setenv("ECCTL_PROFILE", "account-a")
			var scope []*cleanupItem
			nativeSuccessfulCreate(t, cfg, cl, &scope, map[string]any{})
			step := scenario.Step{Name: "delete", Run: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1 --region cn-hangzhou " + suffix}
			result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if ok || result.Command != "" || strings.Contains(string(calls), "template-delete") || scope[0].done {
				t.Errorf("recognized unsafe native layout was executed: %+v %s", result, calls)
			}
		})
	}
}

func TestNativeCreateRejectsRelativeConfigurationBeforeReservation(t *testing.T) {
	cfg, cl, _ := nativeCleanupFixture(t, true)
	t.Setenv("ECCTL_CONFIG_PATH", "./config.json")
	_, err := reserveTemplateCreate(context.Background(), "ecctl sandbox template create --openapi --team-id team-1 --name native", cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
	paths, _ := filepath.Glob(cl.journal + ".template-create-*.json")
	if err == nil || len(paths) != 0 {
		t.Errorf("relative selector reserved mutable binding: %v %v", err, paths)
	}
}

func TestNativeReplayRejectsConfigurationSymlinkRebinding(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	a, b, link := filepath.Join(dir, "config-a.json"), filepath.Join(dir, "config-b.json"), filepath.Join(dir, "config.json")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte(`{"current":"account-a"}`), 0o600); err != nil {
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
	result, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg)
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if err == nil || strings.Contains(string(calls), "template-delete") {
		t.Errorf("changed resolved target permitted replay: %+v %v %s", result, err, calls)
	}
}
