package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/sweeper"
)

func TestNativeTemplateResponseLossRetainsOwnershipAndCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	oldDelays := stepRetryDelays
	stepRetryDelays = []time.Duration{0}
	defer func() { stepRetryDelays = oldDelays }()
	for _, tc := range []struct {
		name, list  string
		owned, keep bool
	}{
		{"owned-journal", `{"templates":[{"id":"tpl-owned","name":"OWNER","team_id":"team-1"},{"id":"tpl-foreign","name":"other","team_id":"team-1"}],"pagination":{"has_more":false}}`, true, true},
		{"owned-cleanup", `{"templates":[{"id":"tpl-owned","name":"OWNER","team_id":"team-1"}],"pagination":{"has_more":false}}`, true, false},
		{"no-match", `{"templates":[],"pagination":{"has_more":false}}`, false, false},
		{"wrong-team", `{"templates":[{"id":"tpl-owned","name":"OWNER","team_id":"team-other"}],"pagination":{"has_more":false}}`, false, false},
		{"incomplete", `{"templates":[{"id":"tpl-owned","name":"OWNER","team_id":"team-1"}],"pagination":{"has_more":true}}`, false, false},
		{"list-failure", `{"error":{"message":"list i/o timeout"}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("FAKE_DIR", dir)
			t.Setenv("ECCTL_PROFILE", "prod asia")
			if err := os.WriteFile(filepath.Join(dir, "list.json"), []byte(tc.list), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(dir, "ecctl")
			body := `#!/usr/bin/env bash
if [[ "$*" == *"call sts GetCallerIdentity"* ]]; then printf '{"response":{"AccountId":"1234567890123456","IdentityType":"RAMUser","UserId":"%s","AccessKeySecret":"dummy-unused-secret"}}\n' "${FAKE_PRINCIPAL_USER:-user-a}"; exit 0; fi
if [[ "$*" == *"configure get"* ]]; then echo '{"profile":"prod asia","mode":"AK"}'; exit 0; fi
if [[ "$*" == *" template create "* ]]; then
 echo create >> "$FAKE_DIR/calls"
 printf '%s\n' "$@" > "$FAKE_DIR/create-args"
 prev=''; for arg in "$@"; do [[ "$prev" == '--name' ]] && echo "$arg" > "$FAKE_DIR/name"; prev="$arg"; done
 for intent in "$FAKE_DIR"/journal.json.template-create-*.json; do [[ -f "$intent" ]] && cp "$intent" "$FAKE_DIR/intent-at-create.json"; done
 echo '{"error":{"code":"CloudAPIError","message":"first response i/o timeout","retryable":false}}'; exit 2
fi
if [[ "$*" == *" template list "* ]]; then
 echo list >> "$FAKE_DIR/calls"; printf '%s\n' "$@" > "$FAKE_DIR/list-args"
 sed "s/OWNER/$(cat "$FAKE_DIR/name")/g" "$FAKE_DIR/list.json"
 [[ "$FAKE_CASE" == 'list-failure' ]] && exit 2; exit 0
fi
if [[ "$*" == *" delete "* ]]; then echo "$*" >> "$FAKE_DIR/deletes"; echo '{"deleted":true}'; exit 0; fi
echo '{}'
`
			if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_CASE", tc.name)
			cfg := execpkg.Config{Bin: fake, Region: "cn-hangzhou"}
			journalPath := filepath.Join(dir, "journal.json")
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), tc.keep, journalPath, report.CleanupJournal{RunID: "owned-run", ExecutionID: "execution-1", Surface: "public"}, func(string, ...any) {})
			var scope []*cleanupItem
			if err := cl.push(&scope, "sandbox/team", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --openapi --name native --team-id team-1 --runtime-config '{\"internetAccess\":false}'", At: "$.template", Capture: map[string]string{"template_id": "id"}, Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			sr, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			cl.run(scope)
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if strings.Count(string(calls), "create\n") != 1 {
				t.Errorf("non-idempotent create replayed: %s", calls)
			}
			if ok || !strings.Contains(sr.Stdout, "first response i/o timeout") || !strings.Contains(sr.Error, "intent") {
				t.Errorf("first unknown result disappeared: %+v", sr)
			}
			reserved, err := os.ReadFile(filepath.Join(dir, "intent-at-create.json"))
			if err != nil || strings.Contains(string(reserved), "internetAccess") {
				t.Errorf("nonsecret durable intent missing before launch: %s %v", reserved, err)
			}
			paths, _ := filepath.Glob(journalPath + ".template-create-*.json")
			if len(paths) != 1 {
				t.Fatalf("lost uncertainty evidence: %v", paths)
			}
			var intent map[string]any
			encoded, _ := os.ReadFile(paths[0])
			if json.Unmarshal(encoded, &intent) != nil {
				t.Fatal(string(encoded))
			}
			if intent["run_id"] != "owned-run" || intent["execution_id"] != "execution-1" || intent["region"] != cfg.Region || intent["surface"] != "public" || intent["ecctl_bin"] != fake || intent["team_id"] != "team-1" {
				t.Errorf("intent lost binding: %s", encoded)
			}
			deletes, _ := os.ReadFile(filepath.Join(dir, "deletes"))
			if strings.Contains(string(deletes), "tpl-foreign") {
				t.Errorf("foreign allocation deleted: %s", deletes)
			}
			if tc.owned {
				if intent["state"] != "journaled" || len(cl.manifest) != 2 || !strings.Contains(cl.manifest[1].Teardown, "tpl-owned") {
					t.Errorf("owned target not journaled: %s %+v", encoded, cl.manifest)
				}
				listArgs, _ := os.ReadFile(filepath.Join(dir, "list-args"))
				if !strings.Contains(string(listArgs), "--team-id\nteam-1\n") || !strings.Contains(string(listArgs), "--profile\nprod asia\n") || !strings.Contains(string(listArgs), "--region\ncn-hangzhou\n") {
					t.Errorf("unscoped reconciliation: %s", listArgs)
				}
				if !tc.keep && (!strings.Contains(string(deletes), "tpl-owned") || !strings.Contains(string(deletes), "team delete team-1")) {
					t.Errorf("owned resource not cleaned: %s", deletes)
				}
				if tc.keep {
					result, err := sweeper.ReplayJournal(context.Background(), journalPath, cfg)
					if err != nil || result.Deleted != 2 {
						t.Errorf("validated owned recovery did not replay: %+v %v", result, err)
					}
				}
			} else {
				if intent["state"] != "pending" || len(deletes) != 0 {
					t.Errorf("uncertainty lost or parent deleted: %s %s", encoded, deletes)
				}
				if _, err := sweeper.ReplayJournal(context.Background(), journalPath, cfg); err == nil {
					t.Error("crash replay ignored pending creation")
				}
				explicit := scenario.Step{Name: "delete-team", Run: "ecctl sandbox team delete team-1"}
				if _, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, explicit, time.Second); ok {
					t.Error("explicit delete ignored pending creation")
				}
				remove := scenario.Step{Name: "remove-native", Run: "ecctl --region cn-hangzhou sbx tpl remove tpl-unproven --openapi --team-id team-1"}
				if result, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, remove, time.Second); ok || (!strings.Contains(result.Error, "intent") && !strings.Contains(result.Error, "creation ownership")) {
					t.Errorf("remove alias bypassed pending fence: %+v", result)
				}
				deletes, _ = os.ReadFile(filepath.Join(dir, "deletes"))
				if len(deletes) != 0 {
					t.Errorf("fenced cleanup reached a subprocess: %s", deletes)
				}
			}
		})
	}
}

func TestNativeTemplateReservationFailurePreventsLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	old := writeTemplateCreateIntent
	defer func() { writeTemplateCreateIntent = old }()
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprint(installed), func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nif [[ \"$*\" == *\"call sts GetCallerIdentity\"* ]]; then echo '{\"response\":{\"AccountId\":\"1234567890123456\",\"IdentityType\":\"RAMUser\",\"UserId\":\"user-a\"}}'; exit 0; fi\nif [[ \"$*\" == *\"configure get\"* ]]; then echo '{\"profile\":\"prod-native\",\"mode\":\"AK\"}'; exit 0; fi\nexit 2\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ECCTL_PROFILE", "prod-native")
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), false, filepath.Join(dir, "journal.json"), report.CleanupJournal{RunID: "owned-run", ExecutionID: "execution-1", Surface: "public"}, func(string, ...any) {})
			var scope []*cleanupItem
			if err := cl.push(&scope, "parent", "ecctl sandbox team delete team-1", "primary", nil); err != nil {
				t.Fatal(err)
			}
			writeTemplateCreateIntent = func(path string, body []byte) error {
				if installed {
					if err := journalfile.WriteExclusiveDurable(path, body); err != nil {
						return err
					}
				}
				return fmt.Errorf("intent barrier failed")
			}
			step := scenario.Step{Name: "create", Run: "ecctl sandbox template create --openapi --team-id team-1 --name native", Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}
			sr, ok := runStep(context.Background(), Options{Logf: func(string, ...any) {}}, cfg, cl, &scope, map[string]any{}, step, time.Second)
			if ok || sr.Command != "" || !strings.Contains(sr.Error, "intent barrier failed") {
				t.Fatalf("launched without durable intent: %+v", sr)
			}
			if installed {
				if _, err := sweeper.ReplayJournal(context.Background(), cl.journal, cfg); err == nil {
					t.Fatal("partially installed intent failed to fence replay")
				}
			}
		})
	}
}

func TestNativeTemplateCreateClassificationAndPinnedFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	for _, command := range []string{
		"ecctl --profile 'prod asia' --region cn-hangzhou sandbox template create --openapi --name native --team-id team-1",
		"ecctl sbx --profile='prod asia' tpl new --openapi=true --name=native --team-id=team-1 --region=cn-hangzhou",
		"ecctl sandbox template create --name '--openapi' --openapi --team-id team-1",
	} {
		t.Run(command, func(t *testing.T) {
			if !isNativeTemplateCreate(command) {
				t.Fatal("native create bypassed reservation")
			}
			dir := t.TempDir()
			bin := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\nif [[ \"$*\" == *\"call sts GetCallerIdentity\"* ]]; then echo '{\"response\":{\"AccountId\":\"1234567890123456\",\"IdentityType\":\"RAMUser\",\"UserId\":\"user-a\"}}'; exit 0; fi\necho '{\"profile\":\"prod asia\",\"mode\":\"AK\"}'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ECCTL_PROFILE", "prod asia")
			cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, newOperationRuntime(1), true, filepath.Join(dir, "journal.json"), report.CleanupJournal{RunID: "owned-run", ExecutionID: "execution-1", Surface: "public"}, func(string, ...any) {})
			intent, err := reserveTemplateCreate(context.Background(), command, cfg, cl, scenario.Step{Teardown: "ecctl sandbox template delete {{.template_id}} --openapi --team-id team-1"}, time.Second)
			if err != nil || intent == nil {
				t.Fatal(err)
			}
			if !strings.Contains(intent.command, intent.Name) || intent.Name == "native" || !strings.Contains(intent.command, "--output=json") {
				t.Fatalf("ownership/output not pinned: %s", intent.command)
			}
		})
	}
	for _, command := range []string{"ecctl sandbox template create --name '--openapi'", "ecctl sandbox template create --openapi=false", "ecctl sandbox template update tpl-1 --openapi"} {
		if isNativeTemplateCreate(command) {
			t.Fatalf("incorrect native create classification: %s", command)
		}
	}
}
