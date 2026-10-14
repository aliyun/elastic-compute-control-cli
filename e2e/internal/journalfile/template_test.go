package journalfile

import (
	"encoding/json"
	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestTemplateRecoveryGuardRejectsCorruptForeignAndPendingIntents(t *testing.T) {
	meta := report.CleanupJournal{Version: 2, RunID: "run-1", ExecutionID: "execution-1", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl-public"}
	for _, tc := range []struct {
		name   string
		modify func(*TemplateCreateIntent)
	}{
		{"journaled", func(*TemplateCreateIntent) {}},
		{"pending", func(i *TemplateCreateIntent) { i.State = "pending" }},
		{"foreign-run", func(i *TemplateCreateIntent) { i.RunID = "other-run" }},
		{"foreign-execution", func(i *TemplateCreateIntent) { i.ExecutionID = "other-execution" }},
		{"foreign-binary", func(i *TemplateCreateIntent) { i.EcctlBin = "ecctl-full" }},
		{"foreign-selector", func(i *TemplateCreateIntent) { i.Selectors["ECCTL_CONFIG_PATH"] = "/other/config.json" }},
		{"unsafe-delete", func(i *TemplateCreateIntent) { i.Deletes = []string{"ecctl sandbox template create --name native"} }},
		{"wrong-team", func(i *TemplateCreateIntent) {
			i.Deletes = []string{strings.ReplaceAll(i.Deletes[0], "team-1", "team-other")}
		}},
		{"missing-output", func(i *TemplateCreateIntent) { i.Exit = nil }},
		{"unbound-profile", func(i *TemplateCreateIntent) {
			i.Profile = ""
			i.Deletes = []string{"ecctl sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.json")
			exit := 2
			intent := TemplateCreateIntent{Version: 2, RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin, TeamID: "team-1", Name: "ecctl-p-0123456789abcdef01234567", Profile: "prod asia", Selectors: map[string]string{}, CreatedAt: time.Now().UTC(), State: "journaled", Exit: &exit, Deletes: []string{"ecctl sandbox template delete tpl-1 --openapi --region cn-hangzhou --team-id team-1 --profile 'prod asia'"}}
			for _, key := range TemplateSelectorNames {
				intent.Selectors[key] = TemplateEnvironmentValue(key, nil)
			}
			paths, err := CaptureTemplateConfig(nil)
			if err != nil {
				t.Fatal(err)
			}
			intent.ConfigPaths = paths
			intent.Identity = execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
			tc.modify(&intent)
			body, _ := json.Marshal(intent)
			if err := WriteExclusiveDurable(path+".template-create-0123456789abcdef01234567.json", body); err != nil {
				t.Fatal(err)
			}
			err = CheckTemplateCreateIntents(path, meta, nil)
			if (err == nil) != (tc.name == "journaled") {
				t.Fatalf("guard %s: %v", tc.name, err)
			}
		})
	}
	for _, body := range []string{"{", `{"state":"journaled"}`, `{} {}`} {
		path := filepath.Join(t.TempDir(), "journal.json")
		if err := os.WriteFile(path+".template-create-broken.json", []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := CheckTemplateCreateIntents(path, meta, nil); err == nil {
			t.Fatal("corrupt intent bypassed guard")
		}
	}
}

func TestTemplateRecoveryGuardMissingDirectory(t *testing.T) {
	for _, kind := range []string{"legacy", "anchor", "reference", "native-delete"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing", "journal.json")
			meta := report.CleanupJournal{Version: 2}
			switch kind {
			case "anchor":
				meta.NativeJournalPath = path
			case "reference":
				meta.NativeIntentPaths = []string{path + ".template-create-0123456789abcdef01234567.json"}
			case "native-delete":
				meta.Entries = []report.Resource{{Teardown: "ecctl sandbox template delete tpl-1 --openapi --team-id team-1"}}
			}
			if err := CheckTemplateCreateIntents(path, meta, nil); (err == nil) != (kind == "legacy") {
				t.Fatalf("missing directory guard %s: %v", kind, err)
			}
		})
	}
}
