package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/shlex"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

func TestNativeTeamLifecycleBuildAndListWithBuiltins(t *testing.T) {
	cfg, cl, dir := nativeCleanupFixture(t, true)
	suites, err := scenario.LoadDir("../../cases/sandbox")
	if err != nil {
		t.Fatal(err)
	}
	var create, list scenario.Step
	for _, suite := range suites {
		if suite.Resource != "sandbox/team" {
			continue
		}
		for _, step := range suite.Steps {
			switch step.Name {
			case "create native template":
				create = step
			case "list native templates":
				list = step
			}
		}
	}
	if create.Run == "" || list.Run == "" {
		t.Fatal("actual Team lifecycle lacks its native create/list steps")
	}
	runtimeConfig := filepath.Join(dir, "runtime.json")
	if err := os.WriteFile(runtimeConfig, []byte(`{"internetAccess":false,"sandboxConfig":{"image":"fc-e2b-registry.cn-hangzhou.cr.aliyuncs.com/runtime/code-interpreter-v1:v0.0.53"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"team_id": "team-1", "resource_prefix": "ecctl-local", "prerequisites": map[string]any{"sandbox": map[string]any{"template": map[string]any{"runtime_config_file": runtimeConfig}}}}
	command, err := vars.Render(create.Run, data)
	if err != nil {
		t.Fatal(err)
	}
	argv, err := shlex.Split(command)
	if err != nil {
		t.Fatal(err)
	}
	flags := templateCreateFlags()
	if err := flags.Parse(argv[1:]); err != nil {
		t.Fatal(err)
	}
	if flags.Lookup("build-config").Changed {
		t.Error("official-image lifecycle must omit explicit build configuration")
	}
	if value, _ := flags.GetString("runtime-config"); value != "@"+runtimeConfig {
		t.Errorf("official-image lifecycle lost its private runtime fixture: %q", value)
	}
	body, err := os.ReadFile(cfg.Bin)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(body), `{"template":{"id":"tpl-1","team_id":"team-1"}}`, `{"template":{"id":"tpl-1","team_id":"team-1","build_status":"ready"}}`, 1)
	script = strings.Replace(script, `if [[ "$*" == *"template create"* ]]; then`, `if [[ "$*" == *"template create"* ]]; then
 printf '%s\n' "$@" > "$FAKE_NATIVE_DIR/lifecycle-create-args"`, 1)
	script = strings.Replace(script, "echo '{}'", `if [[ "$*" == *"template list"* ]]; then
 printf '%s\n' "$@" > "$FAKE_NATIVE_DIR/lifecycle-list-args"
 if [[ "$*" == *"--limit=1 "* || "$*" == *"--limit 1 "* ]]; then
  echo '{"templates":[{"id":"builtin-base","name":"base","team_id":"team-1"}],"pagination":{"has_more":true,"next_token":"builtin-next"}}'
 else cat "$FAKE_NATIVE_DIR/lifecycle-list.json"; fi
 exit 0
fi
echo '{}'`, 1)
	if err := os.WriteFile(cfg.Bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var scope []*cleanupItem
	opt := Options{Logf: func(string, ...any) {}}
	if result, ok := runStep(context.Background(), opt, cfg, cl, &scope, data, create, time.Second); !ok {
		t.Fatalf("actual native create fixture failed: %+v", result)
	}
	if data["native_template_id"] != "tpl-1" {
		t.Fatalf("actual creation did not capture its owned ID: %+v", data)
	}
	createdArgs, err := os.ReadFile(filepath.Join(dir, "lifecycle-create-args"))
	if err != nil {
		t.Fatal(err)
	}
	actualFlags := templateCreateFlags()
	if err := actualFlags.Parse(strings.Split(strings.TrimSuffix(string(createdArgs), "\n"), "\n")); err != nil {
		t.Fatal(err)
	}
	if value, _ := actualFlags.GetString("runtime-config"); value != "@"+runtimeConfig || actualFlags.Lookup("build-config").Changed {
		t.Error("official-image child must receive only the private runtime fixture, without build-config")
	}
	base := map[string]any{"id": "builtin-base", "name": "base", "team_id": "team-1"}
	interpreter := map[string]any{"id": "builtin-interpreter", "name": "code-interpreter-v1", "team_id": "team-1"}
	owned := map[string]any{"id": "tpl-1", "team_id": "team-1"}
	for _, tc := range []struct {
		name  string
		items []any
		more  bool
		ok    bool
	}{
		{"owned-first", []any{owned, base, interpreter}, false, true},
		{"builtins-first", []any{base, interpreter, owned}, false, true},
		{"more-pages", []any{base, owned, interpreter}, true, true},
		{"missing-owned", []any{base, interpreter}, false, false},
		{"wrong-team", []any{base, map[string]any{"id": "tpl-1", "team_id": "other-team"}}, false, false},
		{"wrong-id", []any{base, map[string]any{"id": "other-template", "team_id": "team-1"}}, false, false},
		{"duplicate-owned", []any{base, owned, owned}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := json.Marshal(map[string]any{"templates": tc.items, "pagination": map[string]any{"has_more": tc.more, "next_token": "opaque-next"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "lifecycle-list.json"), response, 0o600); err != nil {
				t.Fatal(err)
			}
			result, ok := runStep(context.Background(), opt, cfg, cl, &scope, data, list, time.Second)
			if result.Command == "" {
				t.Fatalf("actual list was refused before its response assertions: %+v", result)
			}
			if ok != tc.ok {
				t.Errorf("actual list assertions did not distinguish the owned item among builtins: want=%v result=%+v", tc.ok, result)
			}
			args, err := os.ReadFile(filepath.Join(dir, "lifecycle-list-args"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), "--limit=100\n") || !strings.Contains(string(args), "--team-id=team-1\n") {
				t.Errorf("actual list did not request the full supported Team-scoped page: %s", args)
			}
		})
	}
	if entries := readJournalEntries(t, cl.journal); len(entries) != 1 {
		t.Fatalf("list assertion checks altered retained native cleanup ownership: %+v", entries)
	}
}

func TestNativeTemplateAcceptedRecoveryKeepsComplexProfile(t *testing.T) {
	for _, profile := range []string{"prod asia", `prod's "$name"`} {
		for _, keep := range []bool{false, true} {
			t.Run(profile+map[bool]string{true: "/journal", false: "/cleanup"}[keep], func(t *testing.T) {
				code := "CloudAPIError"
				if keep {
					code = "WaitTimeout"
				}
				args := []any{"ecctl", "sandbox", "template", "delete", "tpl-1", "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1", "--profile", profile}
				run, dir := runNativeRecoveryFixture(t, args, code, keep)
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil || strings.Count(string(calls), "create\n") != 1 {
					t.Fatalf("accepted create was replayed: %s %v", calls, err)
				}
				if run.Summary.Failed != 1 || !strings.Contains(run.Cases[0].Steps[0].Stdout, code) || len(run.Manifest) != 1 {
					t.Fatalf("lost allocation/failure: %+v", run)
				}
				want := make([]string, len(args))
				for index, value := range args {
					want[index] = value.(string)
				}
				actual, err := shlex.Split(run.Manifest[0].Teardown)
				if err != nil || !reflect.DeepEqual(actual, want) || !isReplayableTeardown(run.Manifest[0].Teardown) {
					t.Fatalf("recovery changed exact argv: %q %v; want %q", actual, err, want)
				}
				data, err := os.ReadFile(filepath.Join(dir, "journal.json"))
				var journal report.CleanupJournal
				if err != nil || json.Unmarshal(data, &journal) != nil || journal.RunID != "owned-run" || journal.Region != "cn-hangzhou" || journal.Surface != "public" || journal.EcctlBin != filepath.Join(dir, "ecctl") {
					t.Fatalf("lost journal metadata: %s %v", data, err)
				}
				if keep {
					if len(journal.Entries) != 1 || journal.Entries[0].Teardown != run.Manifest[0].Teardown {
						t.Fatalf("accepted target was not durably journaled: %s", data)
					}
				} else {
					deleted, err := os.ReadFile(filepath.Join(dir, "delete-args"))
					if err != nil {
						t.Fatal(err)
					}
					argv := strings.Split(strings.TrimSuffix(string(deleted), "\n"), "\n")
					values := map[string]string{}
					for index := 4; index < len(argv); index++ {
						key, value, inline := strings.Cut(argv[index], "=")
						if !inline {
							value = "true"
							if key != "--openapi" {
								index++
								if index >= len(argv) {
									t.Fatalf("missing cleanup flag value: %s", deleted)
								}
								value = argv[index]
							}
						}
						if _, duplicate := values[key]; duplicate {
							t.Fatalf("duplicate cleanup flag: %s", deleted)
						}
						values[key] = value
					}
					wantValues := map[string]string{"--openapi": "true", "--region": "cn-hangzhou", "--team-id": "team-1", "--profile": profile}
					if len(argv) < 4 || !reflect.DeepEqual(argv[:4], want[1:5]) || !reflect.DeepEqual(values, wantValues) || len(journal.Entries) != 0 {
						t.Fatalf("cleanup did not use original binding: %s %v %s", deleted, err, data)
					}
				}
			})
		}
	}
}

func TestNativeTemplateUnsafeRecoveryDoesNotReplayOrDisappear(t *testing.T) {
	run, dir := runNativeRecoveryFixture(t, []any{"ecctl", "sandbox", "template", "create", "tpl-1"}, "CloudAPIError", false)
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || strings.Count(string(calls), "create\n") != 1 {
		t.Fatalf("allocation was replayed before recovery safety: %s %v", calls, err)
	}
	step := run.Cases[0].Steps[0]
	if !strings.Contains(step.Error, "cleanup recovery") || !strings.Contains(step.Stdout, "tpl-1") || !strings.Contains(step.Stdout, "recovery_command") || len(run.Manifest) != 0 {
		t.Fatalf("unsafe recovery was executed or allocation disappeared: %+v", run)
	}
	if _, err := os.Stat(filepath.Join(dir, "delete-args")); !os.IsNotExist(err) {
		t.Fatalf("unsafe target reached cleanup: %v", err)
	}
}

func runNativeRecoveryFixture(t *testing.T, recovery []any, code string, keep bool) (*report.Run, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := t.TempDir()
	response, err := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": "probe i/o timeout", "recovery_command": recovery}, "actions": []any{map[string]any{"api": "GetTemplate", "message": "connection reset"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "response.json"), response, 0o600); err != nil {
		t.Fatal(err)
	}
	body := `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s","AccessKeySecret":"dummy-unused-secret"}}\n' "${FAKE_PRINCIPAL_USER:-user-a}"; exit 0; fi
if [[ "$*" == *"configure get"* ]]; then cat "$FAKE_DIR/profile.json"; exit 0; fi
if [[ "$*" == *" template create "* ]]; then
 n=0; [[ -f "$FAKE_DIR/count" ]] && n=$(cat "$FAKE_DIR/count"); n=$((n+1)); echo "$n" > "$FAKE_DIR/count"
 echo create >> "$FAKE_DIR/calls"
 if [[ "$n" == 1 ]]; then cat "$FAKE_DIR/response.json"; else echo '{"error":{"code":"CreateRejected","message":"second creation lost first cleanup target"}}'; fi
 exit 2
fi
if [[ "$*" == *" template delete "* ]]; then printf '%s\n' "$@" > "$FAKE_DIR/delete-args"; echo '{"deleted":true}'; exit 0; fi
echo '{}'
`
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := filepath.Join(dir, "cases", "sandbox")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	caseBody := `resource: sandbox/template
steps:
  - name: create
    run: ecctl sandbox template create --openapi --name native --team-id team-1 --region cn-hangzhou
    at: $.template
    capture:
      template_id: id
    teardown: ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1
`
	if err := os.WriteFile(filepath.Join(cases, "template-lifecycle.yaml"), []byte(caseBody), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DIR", dir)
	profile := ""
	for index, arg := range recovery {
		if arg == "--profile" && index+1 < len(recovery) {
			profile, _ = recovery[index+1].(string)
		}
	}
	t.Setenv("ECCTL_PROFILE", profile)
	configured := profile
	if configured == "" {
		configured = "prod-native"
	}
	metadata, err := json.Marshal(map[string]any{"profile": configured, "mode": "AK"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "owned-run", ExecutionID: "native-recovery", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: filepath.Join(dir, "journal.json"), Keep: keep, StepTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return run, dir
}

func TestNativeTemplateAcceptedCreateFailureIsNotRetried(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	log := filepath.Join(dir, "calls")
	count := filepath.Join(dir, "count")
	body := `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s","AccessKeySecret":"dummy-unused-secret"}}\n' "${FAKE_PRINCIPAL_USER:-user-a}"; exit 0; fi
if [[ "$*" == *"configure get"* ]]; then echo '{"profile":"prod-native","mode":"AK"}'; exit 0; fi
if [[ "$*" == *" template create "* ]]; then
 n=0; [[ -f "$FAKE_COUNT" ]] && n=$(cat "$FAKE_COUNT"); n=$((n+1)); echo "$n" > "$FAKE_COUNT"
 echo "create tpl-$n" >> "$FAKE_LOG"
 if [[ "$n" == 1 ]]; then
   echo '{"error":{"kind":"service","code":"FCTemplateBuildFailed","message":"image pull failed: i/o timeout","retryable":false,"recovery_command":["ecctl","sandbox","template","delete","tpl-1","--openapi","--region","cn-hangzhou","--team-id","team-1","--profile","prod-native"]}}'
 else
   echo '{"error":{"code":"CreateRejected","message":"second creation lost first cleanup target"}}'
 fi
 exit 2
fi
if [[ "$*" == *" template delete "* ]]; then echo "$*" >> "$FAKE_LOG"; echo '{"deleted":true}'; exit 0; fi
echo '{}'
`
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := filepath.Join(dir, "cases", "sandbox")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	caseBody := `resource: sandbox/template
steps:
  - name: create
    run: ecctl sandbox template create --openapi --name native --team-id team-1 --region cn-hangzhou
    at: $.template
    capture:
      template_id: id
    teardown: ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1
`
	if err := os.WriteFile(filepath.Join(cases, "template-lifecycle.yaml"), []byte(caseBody), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_COUNT", count)
	t.Setenv("ECCTL_PROFILE", "prod-native")
	result, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "owned-run", ExecutionID: "native-recovery", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: filepath.Join(dir, "journal.json"), StepTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(actual), "create tpl-") != 1 || !strings.Contains(string(actual), "template delete tpl-1") || strings.Contains(string(actual), "tpl-2") {
		t.Fatalf("accepted create replayed/lost cleanup: %s", actual)
	}
	if result.Summary.Failed != 1 || !strings.Contains(result.Cases[0].Steps[0].Stdout, "FCTemplateBuildFailed") {
		t.Fatalf("terminal failure lost: %+v", result)
	}
}
