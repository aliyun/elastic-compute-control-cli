package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestRunTeamTextProfileRegistersCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI launcher uses a shell script")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ecctl-fake-caller")
	goBin, err := osexec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	// go run uses the toolchain's executable path; macOS can reject a freshly
	// installed standalone binary before it reaches the fake caller.
	launcher := "#!/bin/sh\ncd " + teamCommandArgument(root) + "\nexec " + teamCommandArgument(goBin) + " run ./e2e/internal/runner/testdata/team_text_profile.go \"$@\"\n"
	if err := os.WriteFile(bin, []byte(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"current":"default","profiles":[{"name":"default","language":"en","output_format":"text"}]}`)
	configure := func(t *testing.T, dir string) string {
		t.Helper()
		config := filepath.Join(dir, "text-profile.json")
		if err := os.WriteFile(config, profile, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ECCTL_CONFIG_PATH", config)
		t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
		t.Setenv("ECCTL_PROFILE", "")
		t.Setenv("ALIBABA_CLOUD_PROFILE", "")
		t.Setenv("ECCTL_REGION", "cn-beijing")
		t.Setenv("FAKE_TEAM_DIR", dir)
		t.Setenv("FAKE_TEAM_JOURNAL", "")
		t.Setenv("FAKE_TEAM_LOST_RESPONSE", "")
		t.Setenv("FAKE_TEAM_DENY_LIST", "")
		return config
	}
	if !t.Run("profile-control", func(t *testing.T) {
		dir := t.TempDir()
		configure(t, dir)
		cfg := execpkg.Config{Bin: bin, Region: "cn-hangzhou"}
		for _, command := range []string{
			"ecctl --lang en sandbox team create --name control --description control",
			"ecctl --lang en sandbox team list --filter name=control --all",
		} {
			result := execpkg.Run(context.Background(), cfg, command)
			if result.Exit != 0 || result.JSON != nil || !strings.Contains(result.Stdout, "team-owned") {
				t.Fatalf("text profile no longer reproduces YAML: %+v", result)
			}
		}
		result := execpkg.Run(context.Background(), cfg, "ecctl --output=json --lang en sandbox team list --filter name=control --all")
		if result.Exit != 0 || result.JSON == nil {
			t.Fatalf("explicit JSON control failed: %+v", result)
		}
	}) {
		t.Fatal("real CLI output controls failed; cannot establish the regression")
	}
	for _, command := range []string{
		`ecctl sandbox team create`,
		`ecctl --lang "--output=text" sbx team create`,
		`ecctl sandbox team create --lang "quote' --output=text" --output=json`,
		`ecctl --lang "--region=cn-beijing" sandbox team create --output json --json=false`,
		`ecctl --lang "--agent-envelope" sandbox team create`,
		`ecctl sandbox team create --lang "--agent-envelope=true"`,
	} {
		for _, mode := range []string{"success-keep", "success-cleanup", "lost-response-keep", "lost-response-cleanup"} {
			t.Run(command+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				config := configure(t, dir)
				lost, keep := strings.HasPrefix(mode, "lost-response"), strings.HasSuffix(mode, "keep")
				if lost {
					t.Setenv("FAKE_TEAM_LOST_RESPONSE", "1")
				} else {
					// A known create ID must register directly even without list permission.
					t.Setenv("FAKE_TEAM_DENY_LIST", "1")
				}
				journal := filepath.Join(dir, "journal.json")
				t.Setenv("FAKE_TEAM_JOURNAL", journal)
				body, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
				if err != nil {
					t.Fatal(err)
				}
				create := strings.Split(string(body), "  - name: get")[0]
				create = strings.Replace(create, "ecctl sandbox team create", command, 1)
				cases := filepath.Join(dir, "cases", "sandbox")
				if err := os.MkdirAll(cases, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cases, "team.yaml"), []byte(create), 0o600); err != nil {
					t.Fatal(err)
				}
				result, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "text-profile", ExecutionID: "text-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: bin, Keep: keep, CleanupJournal: journal, StepTimeout: 5 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				step := result.Cases[0].Steps[0]
				if !lost && (result.Summary.Passed != 1 || !json.Valid([]byte(step.Stdout))) || lost && (result.Summary.Failed != 1 || !strings.Contains(step.Error, "journaled 1 owned Team")) {
					t.Errorf("protected JSON create/reconciliation failed: summary=%+v step=%+v", result.Summary, step)
				}
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil {
					t.Fatal(err)
				}
				want := "CreateTeam cn-hangzhou\n"
				if lost {
					want += "ListTeams cn-hangzhou\n"
				}
				if !keep {
					want += "GetTeam cn-hangzhou\nDeleteTeam cn-hangzhou\nGetTeam cn-hangzhou\n"
				}
				if string(calls) != want {
					t.Errorf("replay or cleanup target mismatch: got %q want %q", calls, want)
				}
				if _, err := os.Stat(journal); err != nil {
					t.Errorf("created Team has no registered finalizer: %v", err)
					return
				}
				entries := readJournalEntries(t, journal)
				encoded, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				var metadata report.CleanupJournal
				if err := json.Unmarshal(encoded, &metadata); err != nil || metadata.EcctlBin != bin || metadata.Region != "cn-hangzhou" {
					t.Fatalf("cleanup journal target differs: %s %v", encoded, err)
				}
				if keep {
					if len(entries) != 1 || entries[0].Teardown != "ecctl sandbox team delete team-owned --timeout 5m" || entries[0].Region != "cn-hangzhou" {
						t.Errorf("known/recovered identity not registered: %+v", entries)
					}
				} else if _, err := os.Stat(filepath.Join(dir, "team.json")); !os.IsNotExist(err) || len(entries) != 0 {
					t.Errorf("cleanup left an owned Team: err=%v entries=%+v", err, entries)
				}
				if actual, err := os.ReadFile(config); err != nil || string(actual) != string(profile) {
					t.Errorf("protected execution changed profile defaults: %s %v", actual, err)
				}
			})
		}
	}
}

func TestRunTeamReservationClaimsJournalBeforeCreate(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, changed := range []string{"same", "run", "execution", "region", "binary", "surface"} {
			t.Run("existing="+strconv.FormatBool(existing)+"/"+changed, func(t *testing.T) {
				dir := t.TempDir()
				journal := filepath.Join(dir, "journal.json")
				cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
				meta := report.CleanupJournal{RunID: "first-run", ExecutionID: "first-execution", Surface: "public"}
				first := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, journal, meta, func(string, ...any) {})
				desired, _, _ := first.journalValues(&cleanupItem{role: "primary"})
				var existingBytes []byte
				if existing {
					desired.Entries = []report.Resource{{Scope: "existing", Teardown: "ecctl sandbox team delete existing-team --timeout 5m", RegionRole: "primary", Region: cfg.Region, ExecutionID: meta.ExecutionID}}
					if err := writeRunnerJournal(journal, desired); err != nil {
						t.Fatal(err)
					}
					var err error
					existingBytes, err = os.ReadFile(journal)
					if err != nil {
						t.Fatal(err)
					}
				}
				makeInput := func() (map[string]any, string) {
					data := map[string]any{}
					if err := teamOwnership(data); err != nil {
						t.Fatal(err)
					}
					return data, `ecctl sandbox team create --name ` + data["team_name"].(string) + ` --description "` + data["team_owner"].(string) + `"`
				}
				data, command := makeInput()
				st := scenario.Step{Name: "create", Run: command, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
				intent, err := reserveTeamCreate(command, data, cfg, first, st)
				if err != nil {
					t.Fatal(err)
				}
				before, readErr := os.ReadFile(journal)
				if existing && string(before) != string(existingBytes) {
					t.Errorf("first reservation changed existing metadata/entries: %s", before)
				}
				var claimed report.CleanupJournal
				if readErr != nil || json.Unmarshal(before, &claimed) != nil || claimed.Version != 2 || claimed.RunID != meta.RunID || claimed.ExecutionID != meta.ExecutionID || claimed.RegionRole != "primary" || claimed.Region != cfg.Region || claimed.EcctlBin != cfg.Bin || claimed.Surface != meta.Surface || len(claimed.Entries) != len(desired.Entries) {
					t.Errorf("reservation returned before durable metadata claim: %s %v", before, readErr)
				}
				firstIntent, err := os.ReadFile(intent.path)
				if err != nil {
					t.Fatal(err)
				}
				switch changed {
				case "run":
					meta.RunID = "second-run"
				case "execution":
					meta.ExecutionID = "second-execution"
				case "region":
					cfg.Region = "cn-beijing"
				case "binary":
					cfg.Bin = filepath.Join(dir, "other-must-not-launch")
				case "surface":
					meta.Surface = "full"
				}
				second := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, journal, meta, func(string, ...any) {})
				data, command = makeInput()
				st.Run = command
				if changed == "same" {
					if _, err := reserveTeamCreate(command, data, cfg, second, st); err != nil {
						t.Errorf("compatible reservation rejected: %v", err)
					}
				} else {
					var scope []*cleanupItem
					step, ok := runStep(context.Background(), Options{}, cfg, second, &scope, data, st, time.Second)
					if ok || step.Command != "" || step.Status != report.StatusError || !strings.Contains(step.Error, "does not match") || len(scope) != 0 {
						t.Errorf("incompatible interleaved run reached a command: %+v", step)
					}
				}
				after, err := os.ReadFile(journal)
				if err != nil || string(after) != string(before) {
					t.Errorf("reservation rewrote existing metadata/entries: %s %v", after, err)
				}
				if actual, err := os.ReadFile(intent.path); err != nil || string(actual) != string(firstIntent) {
					t.Errorf("interleaving changed the first immutable intent: %s %v", actual, err)
				}
				intents, err := filepath.Glob(journal + ".team-create-*.json")
				want := 1
				if changed == "same" {
					want = 2
				}
				if err != nil || len(intents) != want {
					t.Errorf("incompatible owner acquired an intent: %v %v", intents, err)
				}
			})
		}
	}
}

func TestRunTeamJournalClaimFailurePreventsLaunch(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run("installed="+strconv.FormatBool(installed), func(t *testing.T) {
			dir := t.TempDir()
			journal := filepath.Join(dir, "journal.json")
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, journal, report.CleanupJournal{RunID: "claim-run", ExecutionID: "claim-execution", Surface: "public"}, func(string, ...any) {})
			data := map[string]any{}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			previous := writeTeamCreateJournal
			t.Cleanup(func() { writeTeamCreateJournal = previous })
			failure := errors.New("claim directory sync failed")
			called := false
			writeTeamCreateJournal = func(path string, metadata report.CleanupJournal) error {
				called = true
				if path != journal || metadata.Version != 2 || metadata.RunID != "claim-run" || metadata.ExecutionID != "claim-execution" || metadata.RegionRole != "primary" || metadata.Region != cfg.Region || metadata.EcctlBin != cfg.Bin || metadata.Surface != "public" || len(metadata.Entries) != 0 {
					t.Fatalf("claim writer received incorrect metadata: %+v", metadata)
				}
				if installed {
					if err := previous(path, metadata); err != nil {
						t.Fatal(err)
					}
				}
				return failure
			}
			st := scenario.Step{Name: "create", Run: `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}"`, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
			var scope []*cleanupItem
			step, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, st, time.Second)
			if !called || ok || step.Command != "" || step.Status != report.StatusError || !strings.Contains(step.Error, failure.Error()) || len(scope) != 0 {
				t.Fatalf("failed durable claim permitted launch: %+v", step)
			}
			intents, err := filepath.Glob(journal + ".team-create-*.json")
			if err != nil || len(intents) != 0 {
				t.Fatalf("failed claim acquired an intent: %v %v", intents, err)
			}
			if _, err := os.Stat(journal); installed && err != nil || !installed && !os.IsNotExist(err) {
				t.Fatalf("post-install uncertainty discarded or fabricated the claim: %v", err)
			}
		})
	}
}

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

func TestRunTeamSuccessfulCreateCleanupUsesStrictIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	for _, id := range []string{"created-Team_1", "foreign-team --region cn-beijing", "foreign-team --profile victim", "foreign-team extra", "foreign-team\n--region cn-beijing", "--region", "team/a", "capture-divergence"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			log := filepath.Join(dir, "calls.log")
			if err := os.WriteFile(fake, []byte(`#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *" team create "* ]]; then
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--name" ]]; then name="$2"; break; fi
    shift
  done
  printf '{"team":{"id":%s,"name":"%s","status":"active","read_only":false}}\n' "$FAKE_TEAM_ID_JSON" "$name"
  exit 0
fi
if [[ "$*" == *" team list "* ]]; then echo '{"teams":[],"pagination":{"has_more":false}}'; exit 0; fi
if [[ "$*" == *" team delete "* ]]; then echo '{"deleted":true}'; exit 0; fi
echo '{"error":{"message":"unexpected command"}}'; exit 1
`), 0o755); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			body = []byte(strings.Split(string(body), "\n  - name: get")[0] + "\n")
			createdID := id
			if id == "capture-divergence" {
				createdID = "created-Team_1"
				body = []byte(strings.ReplaceAll(string(body), "team_id: id", "team_id: name"))
			}
			cases := filepath.Join(dir, "cases", "sandbox")
			if err := os.MkdirAll(cases, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cases, "team.yaml"), body, 0o644); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(createdID)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_LOG", log)
			t.Setenv("FAKE_TEAM_ID_JSON", string(encoded))
			journal := filepath.Join(dir, "reports", "fresh", "nested", "journal.json")
			run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunName: "normal-success", RunID: "normal-run", ExecutionID: "normal-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, StepTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if id == "created-Team_1" || id == "capture-divergence" {
				if run.Summary.Passed != 1 || strings.Count(string(calls), "team delete created-Team_1 --timeout 5m --region cn-hangzhou") != 1 {
					t.Fatalf("normal create cleanup changed target: %+v %s", run.Summary, calls)
				}
				if entries := readJournalEntries(t, journal); len(entries) != 0 {
					t.Fatalf("normal cleanup did not complete: %+v", entries)
				}
			} else {
				if run.Summary.Failed != 1 || strings.Contains(string(calls), "team delete ") {
					t.Fatalf("unsafe successful identity launched a delete: %+v %s", run.Summary, calls)
				}
				if entries := readJournalEntries(t, journal); len(entries) != 0 {
					t.Fatalf("unsafe identity got a delete entry: %+v", entries)
				}
			}
		})
	}
}

func TestRunTeamAncestorBarrierFailurePreventsCreateLaunch(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "new", "nested", "reports", "journal.json")
	data := map[string]any{"run_name": "barrier-run"}
	if err := teamOwnership(data); err != nil {
		t.Fatal(err)
	}
	cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
	cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, journal, report.CleanupJournal{RunID: "barrier-run", ExecutionID: "barrier-execution", Surface: "public"}, func(string, ...any) {})
	st := scenario.Step{Name: "create", Run: `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}"`, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
	barrierFailure := errors.New("sync recovery intent ancestor failed")
	previous := writeTeamCreateIntent
	t.Cleanup(func() { writeTeamCreateIntent = previous })
	called := false
	writeTeamCreateIntent = func(path string, _ []byte) error {
		called = true
		if parent, err := os.Stat(filepath.Dir(path)); err != nil || !parent.IsDir() {
			t.Fatalf("not testing freshly created directory chain: %v", err)
		}
		return barrierFailure
	}
	var scope []*cleanupItem
	sr, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, st, time.Second)
	if !called || ok || sr.Command != "" || sr.Status != report.StatusError || !strings.Contains(sr.Error, barrierFailure.Error()) || len(scope) != 0 {
		t.Fatalf("ancestor barrier failed open: %+v", sr)
	}
	intents, err := filepath.Glob(journal + ".team-create-*.json")
	if err != nil || len(intents) != 0 {
		t.Fatalf("failed barrier created an intent: %v %v", intents, err)
	}
}

func TestRunTeamAmbiguousCreateNeverReplaysAndRecoversOwnedIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	oldDelays := stepRetryDelays
	stepRetryDelays = []time.Duration{time.Millisecond}
	defer func() { stepRetryDelays = oldDelays }()
	names := map[string]bool{}
	for _, mode := range []string{"retry-success", "duplicate-rejection", "success-no-id", "cleanup", "wrong-owner", "empty", "list-failure", "unsafe-id", "flag-id", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			body := `#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
if [[ "$*" == *" team create "* ]]; then
  if ! compgen -G "$FAKE_JOURNAL.team-create-*.json" >/dev/null; then
    echo '{"error":{"message":"create ran before recovery intent"}}'; exit 1
  fi
  if [[ -f "$FAKE_REMOTE" ]]; then
    if [[ "$FAKE_MODE" == "retry-success" ]]; then
      echo '{"team":{"id":"team-B","status":"active","read_only":false}}'; exit 0
    fi
    echo '{"error":{"message":"duplicate team name"}}'; exit 1
  fi
  while [[ $# -gt 0 ]]; do
    case "$1" in --name) name="$2"; shift;; --description) description="$2"; shift;; esac
    shift
  done
  printf '{"teams":[{"id":"team-A","name":"%s","description":"%s","status":"active","read_only":false}],"pagination":{"has_more":false}}\n' "$name" "$description" > "$FAKE_REMOTE"
  if [[ "$FAKE_MODE" == "success-no-id" ]]; then echo '{}'; exit 0; fi
  echo '{"error":{"message":"connection reset by peer"}}'; exit 1
fi
if [[ "$*" == *" team list "* ]]; then
  case "$FAKE_MODE" in
    empty) echo '{"teams":[],"pagination":{"has_more":false}}';;
    list-failure) echo '{"error":{"message":"permission denied"}}'; exit 1;;
    wrong-owner) sed 's/ecctl E2E Team/ecctl foreign Team/' "$FAKE_REMOTE";;
    unsafe-id) sed 's/team-A/..\/foreign/' "$FAKE_REMOTE";;
    flag-id) sed 's/team-A/--foreign/' "$FAKE_REMOTE";;
    incomplete) sed 's/"has_more":false/"has_more":true/' "$FAKE_REMOTE";;
    *) cat "$FAKE_REMOTE";;
  esac
  exit 0
fi
if [[ "$*" == *" team delete team-A "* && "$FAKE_MODE" == "cleanup" ]]; then
  if [[ ! -s "$FAKE_JOURNAL" ]]; then echo 'delete ran before durable journal'; exit 1; fi
  echo '{"deleted":true}'; exit 0
fi
echo '{"error":{"message":"unexpected command"}}'; exit 1
`
			if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			cases := filepath.Join(dir, "cases", "sandbox")
			if err := os.MkdirAll(cases, 0o755); err != nil {
				t.Fatal(err)
			}
			caseBody, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cases, "team.yaml"), caseBody, 0o644); err != nil {
				t.Fatal(err)
			}
			log, remote, journal := filepath.Join(dir, "calls"), filepath.Join(dir, "remote"), filepath.Join(dir, "journal.json")
			t.Setenv("FAKE_LOG", log)
			t.Setenv("FAKE_REMOTE", remote)
			t.Setenv("FAKE_MODE", mode)
			t.Setenv("FAKE_JOURNAL", journal)
			run, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunName: "same-reused-label", RunID: "owned-run", ExecutionID: "owned-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Keep: mode != "cleanup", CleanupJournal: journal, StepTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(calls), "team create ") != 1 {
				t.Errorf("ambiguous CreateTeam was replayed: %s", calls)
			}
			if strings.Count(string(calls), "team list ") != 1 {
				t.Errorf("missing bounded read-only reconciliation: %s", calls)
			}
			if run.Summary.Failed != 1 || run.Cases[0].Steps[0].Status == report.StatusPass {
				t.Errorf("uncertain CreateTeam fabricated pass: %+v", run)
			}
			intents, err := filepath.Glob(journal + ".team-create-*.json")
			if err != nil || len(intents) != 1 {
				t.Fatalf("durable recovery intent missing: %v %v", intents, err)
			}
			intentBody, err := os.ReadFile(intents[0])
			if err != nil {
				t.Fatal(err)
			}
			var intent map[string]any
			if err := json.Unmarshal(intentBody, &intent); err != nil {
				t.Fatal(err)
			}
			if intent["run_id"] != "owned-run" || intent["execution_id"] != "owned-execution" || intent["region"] != "cn-hangzhou" || intent["surface"] != "public" || intent["ecctl_bin"] != fake || intent["created_at"] == nil {
				t.Fatalf("intent binding missing: %s", intentBody)
			}
			name, _ := intent["name"].(string)
			if len(name) != 32 || !strings.HasPrefix(name, "ecctl-t-") {
				t.Errorf("name not bounded and execution owned: %q", name)
			}
			if names[name] {
				t.Fatalf("reused label reused Team ownership: %q", name)
			}
			names[name] = true
			if mode == "retry-success" || mode == "duplicate-rejection" || mode == "success-no-id" || mode == "cleanup" {
				entries := readJournalEntries(t, journal)
				if mode == "cleanup" {
					if len(entries) != 0 || strings.Count(string(calls), "team delete team-A ") != 1 {
						t.Fatalf("owned recovery cleanup failed: %+v %s", entries, calls)
					}
				} else if len(entries) != 1 || entries[0].Teardown != "ecctl sandbox team delete team-A --timeout 5m" {
					t.Fatalf("original owned Team missing from journal: %+v", entries)
				}
				body, err := os.ReadFile(journal)
				if err != nil {
					t.Fatal(err)
				}
				var metadata report.CleanupJournal
				if err := json.Unmarshal(body, &metadata); err != nil {
					t.Fatal(err)
				}
				if metadata.RunID != "owned-run" || metadata.ExecutionID != "owned-execution" || metadata.Region != "cn-hangzhou" || metadata.Surface != "public" || metadata.EcctlBin != fake {
					t.Fatalf("recovered journal target changed: %s", body)
				}
			} else if entries := readJournalEntries(t, journal); len(entries) != 0 {
				t.Fatalf("unsafe/unmatched Team got a delete entry: %+v", entries)
			}
		})
	}
}

func TestRunTeamCreateFailsBeforeLaunchWithoutSafeRecoveryReservation(t *testing.T) {
	for _, mode := range []string{"no-journal", "mismatched-journal", "unbound-journal", "bad-teardown", "reused-intent", "unwritable-intent"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			data := map[string]any{"run_name": "owned-run"}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			st := scenario.Step{Name: "create", Run: `ecctl sandbox team create --name {{.team_name}} --description "{{.team_owner}}"`, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
			journal := filepath.Join(dir, "journal.json")
			if mode == "no-journal" {
				journal = ""
			}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, journal, report.CleanupJournal{RunID: "owned-run", ExecutionID: "owned-execution", Surface: "public"}, func(string, ...any) {})
			if mode == "mismatched-journal" {
				if err := writeRunnerJournal(journal, report.CleanupJournal{Version: 2, RunID: "foreign-run", Region: "cn-hangzhou"}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unbound-journal" {
				if err := writeRunnerJournal(journal, report.CleanupJournal{Version: 2}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "bad-teardown" {
				st.Teardown = "ecctl sandbox team delete {{.team_id}} --region foreign"
			}
			if mode == "unwritable-intent" {
				if err := os.WriteFile(journal, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				cl.journal = filepath.Join(journal, "child")
			}
			if mode == "reused-intent" {
				cmd := `ecctl sandbox team create --name ` + data["team_name"].(string) + ` --description "` + data["team_owner"].(string) + `"`
				if _, err := reserveTeamCreate(cmd, data, cfg, cl, st); err != nil {
					t.Fatal(err)
				}
			}
			var scope []*cleanupItem
			sr, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, st, time.Second)
			if ok || sr.Status != report.StatusError || sr.Command != "" || strings.Contains(sr.Error, "executable") {
				t.Fatalf("create reached CLI before safe reservation: %+v", sr)
			}
		})
	}
}

func TestRunTeamCreateFlagLayoutsCannotBypassRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	oldDelays := stepRetryDelays
	stepRetryDelays = []time.Duration{time.Millisecond}
	defer func() { stepRetryDelays = oldDelays }()
	for _, command := range []string{
		`ecctl sandbox team create --lang en`,
		`ecctl sbx team create --lang=en`,
		`ecctl --lang en sandbox team create`,
		`ecctl sandbox --lang=en team create`,
		`ecctl sandbox team --lang en create`,
		`ecctl --json --no-color=true sbx team --output=json create --lang=en`,
		`ecctl --name={{.team_name}} sandbox team create --lang=en`,
		`ecctl --region=cn-hangzhou sbx --lang=en team create`,
		`ecctl --lang -- sandbox team create`,
		`ecctl sandbox team --lang "--" create`,
		`ecctl --lang=-- sbx team create`,
	} {
		for _, reserved := range []bool{false, true} {
			t.Run(command+"/journal="+strconv.FormatBool(reserved), func(t *testing.T) {
				dir := t.TempDir()
				fake := filepath.Join(dir, "ecctl")
				body := `#!/usr/bin/env bash
echo "$*" >> "$FAKE_LOG"
op=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    create|list) op="$1";;
    --name) name="$2"; shift;;
    --name=*) name="${1#*=}";;
    --description) description="$2"; shift;;
  esac
  shift
done
if [[ "$op" == "create" ]]; then
  if ! compgen -G "$FAKE_JOURNAL.team-create-*.json" >/dev/null; then : > "$FAKE_UNRESERVED"; fi
  if [[ -f "$FAKE_REMOTE" ]]; then
    printf '{"team":{"id":"team-B","name":"%s","status":"active","read_only":false}}\n' "$name"; exit 0
  fi
  printf '{"teams":[{"id":"team-A","name":"%s","description":"%s","status":"active"}],"pagination":{"has_more":false}}\n' "$name" "$description" > "$FAKE_REMOTE"
  echo '{"error":{"message":"connection reset by peer"}}'; exit 1
fi
if [[ "$op" == "list" ]]; then cat "$FAKE_REMOTE"; exit 0; fi
echo '{"error":{"message":"unexpected command"}}'; exit 1
`
				if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				caseBody, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
				if err != nil {
					t.Fatal(err)
				}
				create := strings.Split(string(caseBody), "  - name: get")[0]
				run := command
				if !strings.Contains(command, "--name=") {
					run += " --name={{.team_name}}"
				}
				run += ` --description "{{.team_owner}}"`
				create = strings.Replace(create, "ecctl sandbox team create\n      --name {{.team_name}}\n      --description \"{{.team_owner}}\"", run, 1)
				cases := filepath.Join(dir, "cases", "sandbox")
				if err := os.MkdirAll(cases, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cases, "custom.yaml"), []byte(create), 0o644); err != nil {
					t.Fatal(err)
				}
				log, journal := filepath.Join(dir, "calls"), filepath.Join(dir, "journal.json")
				t.Setenv("FAKE_LOG", log)
				t.Setenv("FAKE_JOURNAL", journal)
				t.Setenv("FAKE_REMOTE", filepath.Join(dir, "remote"))
				unreserved := filepath.Join(dir, "unreserved")
				t.Setenv("FAKE_UNRESERVED", unreserved)
				opt := Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "owned-run", ExecutionID: "owned-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Keep: true, StepTimeout: time.Second}
				if reserved {
					opt.CleanupJournal = journal
				}
				result, err := Run(context.Background(), opt)
				if err != nil {
					t.Fatal(err)
				}
				if result.Summary.Failed != 1 || result.Cases[0].Steps[0].Status == report.StatusPass {
					t.Errorf("uncertain/unreserved create reported pass: %+v", result)
				}
				if !reserved {
					if _, err := os.Stat(log); !os.IsNotExist(err) || result.Cases[0].Steps[0].Command != "" {
						t.Fatalf("create layout bypassed reservation: %+v, log error %v", result, err)
					}
					return
				}
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(calls), "create ") != 1 || strings.Count(string(calls), "team list ") != 1 {
					t.Errorf("create replayed or reconciliation missing: %s", calls)
				}
				if _, err := os.Stat(unreserved); !os.IsNotExist(err) {
					t.Errorf("create launched without a durable intent: %v", err)
				}
				intents, err := filepath.Glob(journal + ".team-create-*.json")
				if err != nil || len(intents) != 1 {
					t.Errorf("owned intent missing: %v %v", intents, err)
				}
				entries := readJournalEntries(t, journal)
				if len(entries) != 1 || entries[0].Teardown != "ecctl sandbox team delete team-A --timeout 5m" {
					t.Fatalf("original owned Team lost from journal: %+v", entries)
				}
			})
		}
	}
}

func TestRunTeamCreateRejectsUnboundFlagLayoutsBeforeLaunch(t *testing.T) {
	for _, command := range []string{
		"ecctl --profile=victim sandbox team create",
		"ecctl sandbox team --profile victim create",
		"ecctl --output=text sbx team create",
		"ecctl sandbox team --agent-envelope create",
		"ecctl --resource-group=foreign sandbox team create",
		"ecctl sandbox --plan=foreign team create",
		"ecctl --unsupported=value sandbox team create",
		"ecctl --json=invalid sandbox team create",
		"ecctl sandbox team --no-color=invalid create",
		"ecctl sandbox team create extra",
		"ecctl sandbox team create --name foreign",
		"ecctl --region=cn-beijing sandbox team create",
		"ecctl -- sandbox team create",
		"ecctl --lang -- -- sandbox team create",
		"ecctl --name -- sandbox team create",
	} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			cfg := execpkg.Config{Bin: filepath.Join(dir, "must-not-launch"), Region: "cn-hangzhou"}
			data := map[string]any{}
			if err := teamOwnership(data); err != nil {
				t.Fatal(err)
			}
			cl := newCleanup(map[string]execpkg.Config{"primary": cfg}, nil, true, filepath.Join(dir, "journal.json"), report.CleanupJournal{RunID: "owned-run", ExecutionID: "owned-execution", Surface: "public"}, func(string, ...any) {})
			st := scenario.Step{Name: "create", Run: command + ` --name={{.team_name}} --description "{{.team_owner}}"`, Teardown: "ecctl sandbox team delete {{.team_id}} --timeout 5m"}
			var scope []*cleanupItem
			sr, ok := runStep(context.Background(), Options{}, cfg, cl, &scope, data, st, time.Second)
			if ok || sr.Status != report.StatusError || sr.Command != "" || strings.Contains(sr.Error, "executable") || len(scope) != 0 {
				t.Fatalf("unbound create reached CLI/generic retry: %+v", sr)
			}
			intents, err := filepath.Glob(cl.journal + ".team-create-*.json")
			if err != nil || len(intents) != 0 {
				t.Fatalf("invalid input acquired recovery intent: %v %v", intents, err)
			}
		})
	}
}

func TestRunTeamCreateRejectsEmptyExplicitRegionBeforeLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	t.Setenv("ECCTL_REGION", "cn-beijing")
	for _, regionFlag := range []string{`--region=`, `--region ""`} {
		t.Run(regionFlag, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			body := `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    create|delete) op="$1";;
    --name) name="$2"; shift;;
    --region) region="$2"; shift;;
    --region=*) region="${1#*=}";;
  esac
  shift
done
region="${region:-$ECCTL_REGION}"
printf '%s %s\n' "$op" "$region" >> "$FAKE_LOG"
if [[ "$op" == "create" ]]; then
  printf '%s\n' "$region" > "$FAKE_REMOTE"
  printf '{"team":{"id":"team-A","name":"%s","status":"active","read_only":false}}\n' "$name"; exit 0
fi
if [[ "$op" == "delete" ]]; then
  echo '{"error":{"kind":"not_found","code":"NotFound","message":"Team not found"}}'; exit 4
fi
echo '{"error":{"message":"unexpected command"}}'; exit 1
`
			if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			caseBody, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
			if err != nil {
				t.Fatal(err)
			}
			create := strings.Split(string(caseBody), "  - name: get")[0]
			create = strings.Replace(create, `--description "{{.team_owner}}"`, `--description "{{.team_owner}}" `+regionFlag, 1)
			cases := filepath.Join(dir, "cases", "sandbox")
			if err := os.MkdirAll(cases, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cases, "custom.yaml"), []byte(create), 0o644); err != nil {
				t.Fatal(err)
			}
			log, remote, journal := filepath.Join(dir, "calls"), filepath.Join(dir, "remote"), filepath.Join(dir, "journal.json")
			t.Setenv("FAKE_LOG", log)
			t.Setenv("FAKE_REMOTE", remote)
			result, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "owned-run", ExecutionID: "owned-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, StepTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			step := result.Cases[0].Steps[0]
			if result.Summary.Failed != 1 || step.Status != report.StatusError || step.Command != "" || !strings.Contains(step.Error, "ownership/region") || len(result.Manifest) != 0 {
				t.Errorf("empty explicit region bypassed target reservation: %+v", result)
			}
			if calls, err := os.ReadFile(log); !os.IsNotExist(err) {
				t.Errorf("unsafe create/cleanup launched: %s (read error %v)", calls, err)
			}
			for _, file := range []string{remote, journal} {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Errorf("unsafe create left a remote Team or misbound journal: %s %v", file, err)
				}
			}
			intents, err := filepath.Glob(journal + ".team-create-*.json")
			if err != nil || len(intents) != 0 {
				t.Errorf("empty region acquired a recovery intent: %v %v", intents, err)
			}
		})
	}
}

func TestRunTeamProtectedCommandsUseReservedRegion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	t.Setenv("ECCTL_REGION", "cn-beijing")
	for _, layout := range []struct{ command, language string }{
		{`ecctl sandbox team create --lang "--region=cn-beijing"`, "--region=cn-beijing"},
		{`ecctl --lang "--region" sbx team create`, "--region"},
		{`ecctl sandbox team --lang="--region=cn-beijing" create`, "--region=cn-beijing"},
		{`ecctl --lang "quote' --region=cn-beijing" sandbox team create`, "quote' --region=cn-beijing"},
		{`ecctl --lang "--region=" sandbox team create`, "--region="},
		{`ecctl --lang "--region=cn-beijing" sandbox team create --region cn-hangzhou`, "--region=cn-beijing"},
	} {
		for _, mode := range []string{"success", "lost-response-keep", "lost-response-cleanup"} {
			t.Run(layout.command+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				fake := filepath.Join(dir, "ecctl")
				body := `#!/usr/bin/env bash
while [[ $# -gt 0 ]]; do
  case "$1" in
    create|list|delete) op="$1";;
    --lang) language="$2"; shift;;
    --lang=*) language="${1#*=}";;
    --name) name="$2"; shift;;
    --description) description="$2"; shift;;
    --region) region="$2"; shift;;
    --region=*) region="${1#*=}";;
  esac
  shift
done
region="${region:-$ECCTL_REGION}"
printf '%s %s\n' "$op" "$region" >> "$FAKE_LOG"
if [[ "$op" == "create" ]]; then
  if ! compgen -G "$FAKE_JOURNAL.team-create-*.json" >/dev/null; then echo 'no durable intent'; exit 1; fi
  if [[ "$language" != "$FAKE_LANGUAGE" ]]; then echo 'string value changed'; exit 1; fi
  if [[ -f "$FAKE_REMOTE" ]]; then echo 'create replayed'; exit 1; fi
  printf '%s\n' "$region" > "$FAKE_REMOTE"
  printf '{"teams":[{"id":"team-A","name":"%s","description":"%s","status":"active"}],"pagination":{"has_more":false}}\n' "$name" "$description" > "$FAKE_LIST"
  if [[ "$FAKE_MODE" != "success" ]]; then echo '{"error":{"message":"connection reset by peer"}}'; exit 1; fi
  printf '{"team":{"id":"team-A","name":"%s","status":"active","read_only":false}}\n' "$name"; exit 0
fi
if [[ "$op" == "list" ]]; then
  if [[ -f "$FAKE_REMOTE" && "$(cat "$FAKE_REMOTE")" == "$region" ]]; then cat "$FAKE_LIST"; else echo '{"teams":[],"pagination":{"has_more":false}}'; fi
  exit 0
fi
if [[ "$op" == "delete" ]]; then
  if [[ ! -s "$FAKE_JOURNAL" ]]; then echo 'delete before journal'; exit 1; fi
  if [[ -f "$FAKE_REMOTE" && "$(cat "$FAKE_REMOTE")" == "$region" ]]; then rm "$FAKE_REMOTE"; echo '{"deleted":true}'; exit 0; fi
  echo '{"error":{"kind":"not_found","code":"NotFound","message":"Team not found in this region"}}'; exit 4
fi
echo 'unexpected command'; exit 1
`
				if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				caseBody, err := os.ReadFile("../../cases/sandbox/team-lifecycle.yaml")
				if err != nil {
					t.Fatal(err)
				}
				create := strings.Split(string(caseBody), "  - name: get")[0]
				create = strings.Replace(create, "ecctl sandbox team create", layout.command, 1)
				cases := filepath.Join(dir, "cases", "sandbox")
				if err := os.MkdirAll(cases, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cases, "custom.yaml"), []byte(create), 0o644); err != nil {
					t.Fatal(err)
				}
				log, remote, journal := filepath.Join(dir, "calls"), filepath.Join(dir, "remote"), filepath.Join(dir, "journal.json")
				t.Setenv("FAKE_LOG", log)
				t.Setenv("FAKE_REMOTE", remote)
				t.Setenv("FAKE_LIST", filepath.Join(dir, "list.json"))
				t.Setenv("FAKE_JOURNAL", journal)
				t.Setenv("FAKE_LANGUAGE", layout.language)
				t.Setenv("FAKE_MODE", mode)
				keep := mode == "lost-response-keep"
				result, err := Run(context.Background(), Options{CasesDir: filepath.Join(dir, "cases"), InputsDir: filepath.Join(dir, "inputs"), RunID: "owned-run", ExecutionID: "owned-execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, CleanupJournal: journal, Keep: keep, StepTimeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				wantCalls := "create cn-hangzhou\n"
				if mode != "success" {
					wantCalls += "list cn-hangzhou\n"
				}
				if !keep {
					wantCalls += "delete cn-hangzhou\n"
				}
				if string(calls) != wantCalls {
					t.Errorf("protected subprocess targets/replay differ: calls=%q want=%q", calls, wantCalls)
				}
				if mode == "success" && result.Summary.Passed != 1 || mode != "success" && result.Summary.Failed != 1 {
					t.Errorf("create outcome changed: %+v", result.Summary)
				}
				intents, err := filepath.Glob(journal + ".team-create-*.json")
				if err != nil || len(intents) != 1 {
					t.Fatalf("durable owned intent missing: %v %v", intents, err)
				}
				encoded, err := os.ReadFile(intents[0])
				if err != nil {
					t.Fatal(err)
				}
				var intent teamCreateIntent
				if err := json.Unmarshal(encoded, &intent); err != nil || intent.Region != "cn-hangzhou" || intent.EcctlBin != fake || intent.ExecutionID != "owned-execution" {
					t.Fatalf("intent target mismatch: %s %v", encoded, err)
				}
				if _, err := os.Stat(journal); err != nil {
					t.Errorf("owned resource was never journaled: %v", err)
					return
				}
				entries := readJournalEntries(t, journal)
				if keep {
					actual, err := os.ReadFile(remote)
					if err != nil || string(actual) != "cn-hangzhou\n" || len(entries) != 1 || entries[0].Region != intent.Region || entries[0].Teardown != "ecctl sandbox team delete team-A --timeout 5m" {
						t.Errorf("owned recovery/journal target mismatch: remote=%q err=%v entries=%+v", actual, err, entries)
					}
				} else if _, err := os.Stat(remote); !os.IsNotExist(err) || len(entries) != 0 {
					t.Errorf("false successful cleanup/empty journal with surviving resource: err=%v entries=%+v result=%+v", err, entries, result.Summary)
				}
			})
		}
	}
}
