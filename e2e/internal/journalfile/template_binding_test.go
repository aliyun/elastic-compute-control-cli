package journalfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestTemplateTeamCleanupBindingRequiresOneExactJournaledOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	meta := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl-public"}
	paths, err := CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	exit := 0
	intent := TemplateCreateIntent{Version: 2, RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin,
		TeamID: "team-1", Name: "ecctl-p-0123456789abcdef01234567", Profile: "account-a", ConfigPaths: paths, Selectors: map[string]string{},
		Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}, CreatedAt: time.Now().UTC(), State: "journaled", Exit: &exit,
		Deletes: []string{"ecctl sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1 --profile account-a"}}
	for _, name := range TemplateSelectorNames {
		intent.Selectors[name] = TemplateEnvironmentValue(name, nil)
	}
	writeIntent := func(intent TemplateCreateIntent) {
		t.Helper()
		body, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		intentPath := path + ".template-create-" + intent.Name[8:] + ".json"
		if err := WriteExclusiveDurable(intentPath, body); err != nil {
			t.Fatal(err)
		}
	}
	writeIntent(intent)
	base := execpkg.Config{Bin: meta.EcctlBin, Region: meta.Region}
	bound, found, err := TemplateTeamCleanupBinding(path, meta, base, "team-1")
	if err != nil || !found || bound.Profile != "account-a" || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != intent.Identity {
		t.Fatalf("exact native Team binding was not selected: bound=%+v found=%t err=%v", bound, found, err)
	}
	ordinary, found, err := TemplateTeamCleanupBinding(path, meta, base, "team-other")
	if err != nil || found || ordinary.Profile != "" || ordinary.ExpectedIdentity != nil {
		t.Fatalf("ordinary Team inherited an unrelated native binding: bound=%+v found=%t err=%v", ordinary, found, err)
	}
	second := intent
	second.Name = "ecctl-p-89abcdef0123456701234567"
	second.Profile = "account-b"
	second.Deletes = []string{"ecctl sandbox template delete tpl-2 --openapi --region cn-hangzhou --team-id team-1 --profile account-b"}
	writeIntent(second)
	bound, found, err = TemplateTeamCleanupBinding(path, meta, base, "team-1")
	if err != nil || !found || bound.ExpectedIdentity == nil || *bound.ExpectedIdentity != intent.Identity || (bound.Profile != "account-a" && bound.Profile != "account-b") {
		t.Fatalf("same-principal profile history stranded Team: bound=%+v found=%t err=%v", bound, found, err)
	}
	conflict := second
	conflict.Name = "ecctl-p-fedcba987654321001234567"
	conflict.Identity.UserID = "user-b"
	writeIntent(conflict)
	if _, _, err := TemplateTeamCleanupBinding(path, meta, base, "team-1"); err == nil {
		t.Fatal("different principal was accepted for Team cleanup")
	}
	if err := os.Remove(path + ".template-create-" + conflict.Name[8:] + ".json"); err != nil {
		t.Fatal(err)
	}
	conflict.Identity = intent.Identity
	conflict.ConfigPaths = make(map[string]TemplateConfigPath, len(intent.ConfigPaths))
	for key, value := range intent.ConfigPaths {
		conflict.ConfigPaths[key] = value
	}
	changed := conflict.ConfigPaths["ecctl"]
	changed.Target = filepath.Join(dir, "different-config.json")
	conflict.ConfigPaths["ecctl"] = changed
	writeIntent(conflict)
	if _, _, err := TemplateTeamCleanupBinding(path, meta, base, "team-1"); err == nil {
		t.Fatal("changed configuration source was accepted for Team cleanup")
	}
}

func TestTemplateConfigBindingsFreezeAbsoluteTargets(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "source.json")
	if runtime.GOOS == "windows" {
		link = a
	} else if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	env := []string{"ECCTL_CONFIG_PATH=" + link, "ECCTL_ALIYUN_CONFIG_PATH=" + a}
	paths, err := CaptureTemplateConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	intent := TemplateCreateIntent{ConfigPaths: paths}
	bound := intent.BoundConfig(execpkg.Config{Env: env})
	realA, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatal(err)
	}
	if TemplateEnvironmentValue("ECCTL_CONFIG_PATH", bound.Env) != realA || paths["ecctl"].Requested != link || paths["ecctl"].Target != realA {
		t.Fatalf("child config path was not frozen: %+v %v", paths, bound.Env)
	}
	if err := intent.ValidateConfig(env); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(b, link); err != nil {
			t.Fatal(err)
		}
		if intent.ValidateConfig(env) == nil {
			t.Fatal("symlink target changed without failing closed")
		}
	}
	if _, err := CaptureTemplateConfig([]string{"ECCTL_CONFIG_PATH=./config.json", "ECCTL_ALIYUN_CONFIG_PATH=" + a}); err == nil {
		t.Fatal("cwd-sensitive config selector accepted")
	}
	missing := filepath.Join(dir, "missing", "nested", "config.json")
	path, err := resolveTemplateConfigPath(missing)
	realDir, dirErr := filepath.EvalSymlinks(dir)
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err != nil || path.Requested != missing || path.Target != filepath.Join(realDir, "missing", "nested", "config.json") {
		t.Fatalf("missing source binding: %+v %v", path, err)
	}
}
