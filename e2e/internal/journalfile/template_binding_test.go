package journalfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestVolumeTeamCreatorSourceRequiresOriginalPrincipal(t *testing.T) {
	path, err := ResolveJournalPath(filepath.Join(t.TempDir(), "cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	meta := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl-public"}
	paths, err := CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	creator := &report.QuotaCredentialBinding{Profile: "account-b", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-b"}, ConfigPaths: map[string]report.CredentialConfigPath{}, Selectors: map[string]string{}}
	for key, path := range paths {
		creator.ConfigPaths[key] = report.CredentialConfigPath{Requested: path.Requested, Target: path.Target}
	}
	for _, name := range TemplateSelectorNames {
		creator.Selectors[name] = TemplateEnvironmentValue(name, nil)
	}
	meta.VolumeFixtureBinding = creator
	meta.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{"team-b": creator}
	name := "ecctl-t-0123456789abcdef01234567"
	meta.VolumeTeamCreateIDs = map[string]string{name: "team-b"}
	meta.Entries = []report.Resource{{Scope: "case", Teardown: "ecctl sandbox team delete team-b --timeout 5m --output=json", RegionRole: meta.RegionRole, Region: meta.Region, ExecutionID: meta.ExecutionID, TeamCreateConfirmed: true, VolumeTeamBound: true}}
	receipt := volumeTeamReceipt{Version: 1, RunID: meta.RunID, ExecutionID: meta.ExecutionID, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin,
		Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl", "sandbox", "team", "create"}, CaseScope: "case",
		Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", AcceptedID: "team-b", Dispatched: true, DispatchProtocol: 1,
		VolumeBinding: creator, CreatedAt: time.Now().UTC()}
	body, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusiveDurable(path+".team-create-"+name[8:]+".json", body); err != nil {
		t.Fatal(err)
	}
	source := TemplateCreateIntent{Resource: "volume", RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin,
		Profile: creator.Profile, Identity: creator.Identity, ConfigPaths: paths, Selectors: creator.Selectors}
	if err := CheckVolumeTeamCreatorSource(meta, "team-b", source); err != nil {
		t.Fatalf("matching source was rejected: %v", err)
	}
	source.Profile = "account-a"
	source.Identity.UserID = "user-a"
	if err := CheckVolumeTeamCreatorSource(meta, "team-b", source); err == nil {
		t.Fatal("Volume created by another principal was accepted into Team B")
	}
	source.Profile, source.Identity = creator.Profile, creator.Identity
	meta.Entries[0].VolumeTeamBound = false
	if err := CheckVolumeTeamCreatorSource(meta, "team-b", source); err == nil {
		t.Fatal("missing Team creator marker was accepted")
	}
	meta.Entries[0].VolumeTeamBound = true
	changed := *creator
	changed.Identity.UserID = "user-a"
	meta.VolumeTeamCreators["team-b"] = &changed
	if err := CheckVolumeTeamCreatorSource(meta, "team-b", source); err == nil {
		t.Fatal("creator binding changed after Team registration")
	}
}

func TestVolumeTransferTeamBindingRejectsConflictingNativeProfile(t *testing.T) {
	dir := t.TempDir()
	path, err := ResolveJournalPath(filepath.Join(dir, "cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	meta := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl-public",
		VolumeTeamBindings: map[string]string{"team-2": "ecctl-p-0123456789abcdef01234567"},
		Entries:            []report.Resource{{Teardown: "ecctl sandbox team delete team-2 --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTransferIntent: "ecctl-p-0123456789abcdef01234567"}}}
	paths, err := CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	exit := 0
	started := true
	source := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin,
		TeamID: "team-1", Name: meta.VolumeTeamBindings["team-2"], Profile: "account-a", ConfigPaths: paths, Selectors: map[string]string{},
		Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}, CreatedAt: time.Now().UTC(), State: "journaled", Started: &started, Exit: &exit,
		AcceptedVolumeID: "vol-1", StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64),
		Deletes: []string{"ecctl sandbox volume delete vol-1 --region cn-hangzhou --profile account-a"}}
	for _, name := range TemplateSelectorNames {
		source.Selectors[name] = TemplateEnvironmentValue(name, nil)
	}
	other := source
	other.Name, other.TeamID, other.Profile = "ecctl-p-89abcdef0123456701234567", "team-2", "account-b"
	other.AcceptedVolumeID = "vol-2"
	other.Deletes = []string{"ecctl sandbox volume delete vol-2 --region cn-hangzhou --profile account-b"}
	for _, intent := range []TemplateCreateIntent{source, other} {
		body, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteExclusiveDurable(path+".template-create-"+intent.Name[8:]+".json", body); err != nil {
			t.Fatal(err)
		}
	}
	base := execpkg.Config{Bin: meta.EcctlBin, Region: meta.Region}
	if _, _, err := TemplateTeamCleanupBinding(path, meta, base, "team-2"); err == nil {
		t.Fatal("conflicting destination Team profile was accepted for cleanup")
	}
	if err := CheckVolumeTransferTeamBinding(path, meta, base, "team-2", source); err == nil {
		t.Fatal("conflicting destination Team profile was accepted for transfer")
	}
}

func TestVolumeCreatorOAuthGenerationDriftRejectedByBinding(t *testing.T) {
	dir := t.TempDir()
	native, aliyun := filepath.Join(dir, "native.json"), filepath.Join(dir, "aliyun.json")
	config := func(generation string) []byte {
		return []byte(`{"profiles":[{"name":"account-a","mode":"OAuth","oauth_site_type":"International","oauth_generation":"` + generation + `","oauth_account_id":"1234567890123456"}]}`)
	}
	if err := os.WriteFile(native, config("generation-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aliyun, []byte(`{"profiles":[{"name":"account-a","mode":"AK","access_key_id":"fake-a","access_key_secret":"fake-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + dir, "ECCTL_CONFIG_PATH=" + native, "ECCTL_ALIYUN_CONFIG_PATH=" + aliyun, "ECCTL_PROFILE=account-a"}
	identity := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	binding, err := CaptureQuotaBinding("account-a", "OAuth", identity, env)
	if err != nil {
		t.Fatal(err)
	}
	binding.ResolvedMode = true
	if err := ValidateQuotaBinding(binding, env); err != nil {
		t.Fatalf("unchanged Volume creator was rejected: %v", err)
	}
	if err := os.WriteFile(native, config("generation-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaBinding(binding, env); err == nil || !strings.Contains(err.Error(), "configuration state changed") {
		t.Fatalf("same-principal creator OAuth rotation was accepted: %v", err)
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
