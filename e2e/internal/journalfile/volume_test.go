package journalfile

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
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestVolumeIntentFencesReplayAndBindsExactOwnedDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	name := "ecctl-p-0123456789abcdef01234567"
	intentPath := path + ".template-create-0123456789abcdef01234567.json"
	command := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	meta := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run-1", ExecutionID: "execution-1", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "ecctl-public", NativeJournalPath: path, NativeIntentPaths: []string{intentPath}}
	paths, err := CaptureTemplateConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	intent := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: meta.RunID, ExecutionID: meta.ExecutionID, RegionRole: meta.RegionRole, Region: meta.Region, Surface: meta.Surface, EcctlBin: meta.EcctlBin, TeamID: "team-original", Name: name, CaseScope: "case", Profile: "owner", Selectors: map[string]string{}, ConfigPaths: paths, Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}, CreatedAt: time.Now().UTC(), State: "pending"}
	for _, key := range TemplateSelectorNames {
		intent.Selectors[key] = TemplateEnvironmentValue(key, nil)
	}
	write := func() {
		t.Helper()
		journalBody, _ := json.Marshal(meta)
		intentBody, _ := json.Marshal(intent)
		if err := WriteDurable(path, journalBody); err != nil {
			t.Fatal(err)
		}
		if err := WriteDurable(intentPath, intentBody); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); !errors.Is(err, ErrTemplateCreatePending) {
		t.Fatalf("pending Volume create did not fence restart replay: %v", err)
	}
	intent.State, intent.Recovered, intent.ReconcileSHA256, intent.Deletes = "settling", true, strings.Repeat("a", 64), []string{command}
	meta.Entries = []report.Resource{{Scope: "case", Teardown: command, RegionRole: "primary", Region: meta.Region, ExecutionID: meta.ExecutionID}}
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); !errors.Is(err, ErrTemplateCreatePending) {
		t.Fatalf("interrupted two-file settlement released replay: %v", err)
	}
	intent.State = "journaled"
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err == nil {
		t.Fatal("legacy name-only recovery released the Volume ownership fence")
	}
	started, exit := true, 1
	intent.Started, intent.Exit = &started, &exit
	intent.StdoutSHA256, intent.StderrSHA256 = strings.Repeat("b", 64), strings.Repeat("c", 64)
	intent.AcceptedVolumeID, intent.CreateRequestID = "vol-owned", "req-create"
	intent.VolumeObserved = true
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err != nil {
		t.Fatalf("invocation-linked recovered Volume remained fenced: %v", err)
	}
	intent.Recovered, intent.ReconcileSHA256 = false, ""
	intent.State, intent.Exit, intent.Deletes = "journaled", &exit, []string{command}
	meta.Entries = []report.Resource{{Scope: "case", Teardown: command, RegionRole: "primary", Region: meta.Region, ExecutionID: meta.ExecutionID}}
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err != nil {
		t.Fatalf("journaled exact Volume delete did not survive restart: %v", err)
	}
	if err := CheckTemplateDeleteOwnership(path, meta, nil, intent, command); err != nil {
		t.Fatalf("exact owned Volume delete lost: %v", err)
	}
	other := intent
	other.Name = "ecctl-p-89abcdef0123456701234567"
	other.State, other.AcceptedVolumeID, other.CreateRequestID = "not_started", "", ""
	other.Deletes = nil
	other.VolumeObserved = false
	other.StdoutSHA256, other.StderrSHA256 = "", ""
	otherStarted, otherExit := false, -1
	other.Started, other.Exit = &otherStarted, &otherExit
	otherPath := path + ".template-create-" + other.Name[8:] + ".json"
	otherBody, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(otherPath, otherBody); err != nil {
		t.Fatal(err)
	}
	meta.NativeIntentPaths = []string{otherPath}
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err == nil {
		t.Fatal("unreferenced Volume owner released its finalizer")
	}
	if _, err := VolumeDeleteBinding(path, meta, nil, command); err == nil {
		t.Fatal("unreferenced Volume owner bound a replay delete")
	}
	meta.NativeIntentPaths = []string{intentPath, otherPath}
	write()
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err != nil {
		t.Fatalf("restored exact Volume reference remained fenced: %v", err)
	}
	for _, unsafe := range []string{
		"ecctl sandbox volume delete vol-other --region cn-hangzhou --profile owner",
		"ecctl sandbox volume delete vol-owned --region cn-beijing --profile owner",
		"ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile other",
		"ecctl sandbox volume delete vol-owned --team-id team-original --region cn-hangzhou --profile owner",
	} {
		meta.Entries[0].Teardown = unsafe
		write()
		if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err == nil {
			t.Fatalf("foreign or stale-Team Volume delete was replayable: %s", unsafe)
		}
	}
	meta.Entries[0].Teardown = command
	write()
	if err := os.Remove(intentPath); err != nil {
		t.Fatal(err)
	}
	if err := CheckTemplateCreateIntents(path, report.CleanupJournal{}, nil); err == nil {
		t.Fatal("missing Volume ownership sidecar did not fence replay")
	}
}

func TestVolumeGeneric404CannotProveScopedAbsence(t *testing.T) {
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "owner", ExpectedIdentity: &pin}
	probe := func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
		if bound.ExpectedIdentity != &pin || bound.Region != "cn-hangzhou" || bound.Profile != "owner" || command != "ecctl sandbox volume get 'vol-owned' --team-id 'team-original' --region 'cn-hangzhou' --profile 'owner' --output=json --agent-envelope=false" {
			t.Fatalf("absence probe changed exact target or credential pin: command=%q config=%+v", command, bound)
		}
		return execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{
			"error":   map[string]any{"kind": "not_found", "code": "NotFound"},
			"actions": []any{map[string]any{"action_name": "GetVolume", "code": "404", "request_id": "req-generic"}},
		}}
	}
	if _, absent, err := ProbeVolumeScopes(context.Background(), cfg, "vol-owned", "reserved-name", cfg.Region, cfg.Profile, []string{"team-original"}, probe); err == nil || absent {
		t.Fatalf("generic GetVolume 404 passed scope validation: absent=%v err=%v", absent, err)
	}
	confirmed := func(ctx context.Context, bound execpkg.Config, command string) execpkg.Result {
		result := probe(ctx, bound, command)
		result.JSON.(map[string]any)["actions"] = []any{map[string]any{"action_name": "GetVolume", "code": "VolumeNotFound", "request_id": "req-specific", "provider_verified": true}}
		return result
	}
	if _, absent, err := ProbeVolumeScopes(context.Background(), cfg, "vol-owned", "reserved-name", cfg.Region, cfg.Profile, []string{"team-original"}, confirmed); err != nil || !absent {
		t.Fatalf("specific VolumeNotFound evidence was rejected: absent=%v err=%v", absent, err)
	}
}

func TestVolumeDeleteGrammarRejectsUnsupportedOutputShorthand(t *testing.T) {
	if _, err := ParseVolumeDeleteRequest("ecctl sandbox volume delete vol-owned -o json"); err == nil {
		t.Fatal("unsupported -o shorthand entered owned Volume delete")
	}
	if _, err := ParseVolumeDeleteRequest("ecctl sandbox volume delete vol-owned --output=json"); err != nil {
		t.Fatalf("supported --output spelling was rejected: %v", err)
	}
}
