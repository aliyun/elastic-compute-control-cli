package journalfile

import (
	"context"
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

func TestVolumeTeamAbsenceReadProofUsesVerifiedArgvWithSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inert subprocess fixture uses a POSIX shell")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake ecctl")
	script := `#!/bin/sh
[ "$1" = sandbox ] && [ "$2" = team ] && [ "$3" = get ] && [ "$4" = team-owned ] && [ "$5" = --region ] && [ "$6" = cn-hangzhou ] && [ "$7" = --profile ] && [ "$8" = "owner profile" ] && [ "$9" = --output=json ] && [ "${10}" = --agent-envelope=false ] || exit 91
echo '{"error":{"kind":"not_found","code":"NotFound"},"actions":[{"action_name":"GetTeam","code":"TeamNotFound","request_id":"req-absent","provider_verified":true}]}'
exit 4
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	digest, err := ExecutableSHA256(bin)
	if err != nil {
		t.Fatal(err)
	}
	profile := "owner profile"
	command := volumeTeamAbsenceCommand("team-owned", "cn-hangzhou", profile)
	cfg := execpkg.Config{Bin: bin, ExpectedBinSHA256: digest, Region: "cn-hangzhou", Profile: profile, ExpectedIdentity: &execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	readback := execpkg.Run(context.Background(), cfg, command)
	if err := volumeTeamAbsenceResult(readback); err != nil || !teamAbsenceCommandMatches(readback, "team-owned", cfg.Region, profile, digest) {
		t.Fatalf("exact Team absence readback lost its command identity: %v, %+v", err, readback)
	}
	changed := readback
	changed.Argv = append([]string(nil), readback.Argv...)
	changed.Argv[3] = "team-other"
	if teamAbsenceCommandMatches(changed, "team-owned", cfg.Region, profile, digest) {
		t.Fatal("altered structured Team selector passed")
	}
	changed = readback
	changed.VerifiedBinSHA256 = strings.Repeat("b", 64)
	if teamAbsenceCommandMatches(changed, "team-owned", cfg.Region, profile, digest) {
		t.Fatal("different verified executable passed")
	}
}

func TestVolumeDefinitiveDeleteRejectionRequiresUncontradictedProof(t *testing.T) {
	root := map[string]any{
		"error":   map[string]any{"kind": "service", "code": "CloudAPIError", "retryable": false},
		"actions": []any{map[string]any{"action_name": "DeleteVolume", "code": "Forbidden", "request_id": "req-denied", "provider_verified": true}},
	}
	result := execpkg.Result{Started: true, Exit: 2, JSON: root}
	if !VolumeDefinitiveDeleteRejection(result) {
		t.Fatal("verified permission denial was not retryable")
	}
	root["volume"] = map[string]any{"id": "vol-owned"}
	if VolumeDefinitiveDeleteRejection(result) {
		t.Fatal("contradictory response restored delete attempt")
	}
	delete(root, "volume")
	root["actions"].([]any)[0].(map[string]any)["provider_verified"] = false
	if VolumeDefinitiveDeleteRejection(result) {
		t.Fatal("diagnostic denial restored delete attempt")
	}
}

func TestWaitVolumeAbsentReportsScopedAbsence(t *testing.T) {
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "account-a", ExpectedIdentity: &execpkg.IdentityPin{}}
	reads := 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	absent := WaitVolumeAbsent(ctx, cfg, "vol-owned", "ecctl-p-owned", cfg.Region, cfg.Profile, []string{"team-owned"}, func(_ context.Context, _ execpkg.Config, _ string) execpkg.Result {
		reads++
		if reads == 1 {
			return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": "ecctl-p-owned", "team_id": "team-owned", "status": "DELETING"}}}
		}
		return execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetVolume", "code": "VolumeNotFound", "request_id": "req-absent", "provider_verified": true}}}}
	})
	if !absent || reads != 2 {
		t.Fatalf("bounded scoped wait absent=%v reads=%d", absent, reads)
	}
}

func TestWaitVolumeAbsentStopsOnErrorState(t *testing.T) {
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "account-a", ExpectedIdentity: &execpkg.IdentityPin{}}
	reads := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if WaitVolumeAbsent(ctx, cfg, "vol-owned", "ecctl-p-owned", cfg.Region, cfg.Profile, []string{"team-owned"}, func(_ context.Context, _ execpkg.Config, _ string) execpkg.Result {
		reads++
		return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": "vol-owned", "name": "ecctl-p-owned", "team_id": "team-owned", "status": "ERROR"}}}
	}) || reads != 1 {
		t.Fatalf("ERROR state was polled as a pending deletion: reads=%d", reads)
	}
}

func TestVolumeSingleTeamTerminalEligibility(t *testing.T) {
	command := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	intent := TemplateCreateIntent{
		Resource: "volume", State: "journaled", Name: "ecctl-p-0123456789abcdef01234567",
		RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou",
		Surface: "public", EcctlBin: "/bin/ecctl", Profile: "owner", TeamID: "team-owned", AcceptedVolumeID: "vol-owned",
		Identity:       execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-owned"},
		VolumeObserved: true, Deletes: []string{command},
	}
	team := report.Resource{Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", TeamCreateConfirmed: true, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
	entry := report.Resource{Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeDeleteAttempted: true}
	journal := report.CleanupJournal{RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/bin/ecctl", Entries: []report.Resource{team, entry}}
	if !VolumeAbsenceTerminal(journal, intent, entry) {
		t.Fatal("exact single-Team Volume was not eligible for terminal GetVolume absence proof")
	}
	for _, tc := range []struct {
		name string
		edit func(*report.CleanupJournal, *TemplateCreateIntent, *report.Resource)
	}{
		{"unobserved", func(_ *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) { i.VolumeObserved = false }},
		{"different ID", func(_ *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) {
			i.AcceptedVolumeID = "vol-other"
		}},
		{"different region", func(_ *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) {
			i.Region = "cn-zhangjiakou"
		}},
		{"different scope", func(_ *report.CleanupJournal, _ *TemplateCreateIntent, e *report.Resource) { e.Scope = "another-case" }},
		{"missing Team", func(j *report.CleanupJournal, _ *TemplateCreateIntent, _ *report.Resource) { j.Entries = j.Entries[1:] }},
		{"transfer marker", func(j *report.CleanupJournal, _ *TemplateCreateIntent, _ *report.Resource) {
			j.Entries[0].VolumeTransferPending = true
		}},
		{"transfer history", func(j *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) {
			j.VolumeTransferHistory = map[string]map[string]bool{i.Name: {"team-other": false}}
		}},
		{"another scope", func(j *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) {
			j.VolumeScopeBindings = map[string][]string{i.Name: {"team-other"}}
		}},
		{"original Team has another transfer binding", func(j *report.CleanupJournal, i *TemplateCreateIntent, _ *report.Resource) {
			j.VolumeTeamBindings = map[string]string{i.TeamID: "other-volume"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyJournal, copyIntent, copyEntry := journal, intent, entry
			copyJournal.Entries = append([]report.Resource(nil), journal.Entries...)
			tc.edit(&copyJournal, &copyIntent, &copyEntry)
			if VolumeAbsenceTerminal(copyJournal, copyIntent, copyEntry) {
				t.Fatal("unbound or transferred Volume was eligible for terminal absence")
			}
		})
	}
}

func TestHistoricalVolumeTransferIsScopedToTheBoundVolume(t *testing.T) {
	intent := TemplateCreateIntent{Name: "ecctl-p-0123456789abcdef01234567", TeamID: "team-owned"}
	entry := report.Resource{Teardown: "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"}
	for _, tc := range []struct {
		name    string
		journal report.CleanupJournal
		want    bool
	}{
		{name: "destination scope", journal: report.CleanupJournal{
			VolumeScopeBindings: map[string][]string{intent.Name: {"team-destination"}},
		}, want: true},
		{name: "pending history", journal: report.CleanupJournal{
			VolumeTransferHistory: map[string]map[string]bool{intent.Name: {"team-destination": true}},
		}, want: true},
		{name: "settled history", journal: report.CleanupJournal{
			VolumeTransferHistory: map[string]map[string]bool{intent.Name: {"team-destination": false}},
		}, want: true},
		{name: "destination Team binding", journal: report.CleanupJournal{
			VolumeTeamBindings: map[string]string{"team-destination": intent.Name},
		}, want: true},
		{name: "retained transfer marker", journal: report.CleanupJournal{
			Entries: []report.Resource{{VolumeTransferIntent: intent.Name}},
		}, want: true},
		{name: "pending finalizer", journal: report.CleanupJournal{
			Entries: []report.Resource{{Teardown: entry.Teardown, VolumeTransferPending: true}},
		}, want: true},
		{name: "independent Volume", journal: report.CleanupJournal{
			VolumeScopeBindings:   map[string][]string{"other-volume": {"team-other"}},
			VolumeTransferHistory: map[string]map[string]bool{"other-volume": {"team-other": false}},
			VolumeTeamBindings:    map[string]string{"team-other": "other-volume"},
			Entries:               []report.Resource{{Teardown: "other-finalizer", VolumeTransferIntent: "other-volume", VolumeTransferPending: true}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasHistoricalVolumeTransfer(tc.journal, intent, entry); got != tc.want {
				t.Fatalf("retained transfer for bound Volume = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestVolumeTransferNeedsDestinationReadBeforeSettlement(t *testing.T) {
	intent := TemplateCreateIntent{Resource: "volume", Name: "ecctl-p-0123456789abcdef01234567"}
	journal := report.CleanupJournal{Entries: []report.Resource{{Teardown: "ecctl sandbox team delete team-destination --timeout 5m --output=json", VolumeTransferIntent: intent.Name, VolumeTransferPending: true}}}
	if changed, err := ResolveVolumeTransferObservation(&journal, intent, "team-original", "AVAILABLE"); err == nil || changed || !journal.Entries[0].VolumeTransferPending {
		t.Fatalf("source read resolved pending transfer: changed=%v err=%v", changed, err)
	}
	for _, status := range []string{"CREATING", "DELETING"} {
		if changed, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", status); err == nil || changed || !journal.Entries[0].VolumeTransferPending {
			t.Fatalf("destination %s settled pending transfer: changed=%v err=%v", status, changed, err)
		}
	}
	if changed, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", "AVAILABLE"); err != nil || !changed || journal.Entries[0].VolumeTransferPending {
		t.Fatalf("destination read failed to settle transfer: changed=%v err=%v", changed, err)
	}
	journal.Entries[0].VolumeTransferPending = true
	if changed, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", "ERROR"); err != nil || !changed || journal.Entries[0].VolumeTransferPending {
		t.Fatalf("exact ERROR destination was not retained for safe cleanup: changed=%v err=%v", changed, err)
	}
}

func TestVolumePossibleTeamsRequiresRetainedDestination(t *testing.T) {
	intent := TemplateCreateIntent{Resource: "volume", RunID: "run", ExecutionID: "execution", TeamID: "team-original", Name: "ecctl-p-0123456789abcdef01234567"}
	team := func(id, transfer string) report.Resource {
		return report.Resource{Teardown: "ecctl sandbox team delete " + id + " --timeout 5m --output=json", TeamCreateConfirmed: true, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeTransferIntent: transfer}
	}
	meta := report.CleanupJournal{RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Entries: []report.Resource{team("team-original", ""), team("team-destination", intent.Name)}, VolumeTeamBindings: map[string]string{"team-destination": intent.Name}, VolumeScopeBindings: map[string][]string{intent.Name: {"team-destination"}}}
	if teams, err := VolumePossibleTeams(meta, intent); err != nil || len(teams) != 2 || teams[0] != "team-original" || teams[1] != "team-destination" {
		t.Fatalf("retained scopes = %v, %v", teams, err)
	}
	for _, mutate := range []struct {
		name string
		fn   func(*report.CleanupJournal)
	}{
		{"missing destination scope", func(j *report.CleanupJournal) { j.VolumeScopeBindings = nil }},
		{"missing destination Team", func(j *report.CleanupJournal) { j.Entries = j.Entries[:1] }},
		{"duplicate scope", func(j *report.CleanupJournal) {
			j.VolumeScopeBindings[intent.Name] = []string{"team-destination", "team-destination"}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			copy := meta
			copy.Entries = append([]report.Resource(nil), meta.Entries...)
			copy.VolumeScopeBindings = map[string][]string{intent.Name: {"team-destination"}}
			mutate.fn(&copy)
			if _, err := VolumePossibleTeams(copy, intent); err == nil {
				t.Fatal("unretained or ambiguous Team scope was accepted")
			}
		})
	}
}

func TestSecondVolumeTransferRetainsFirstTeamAnchorAndOwnScope(t *testing.T) {
	first := "ecctl-p-0123456789abcdef01234567"
	second := TemplateCreateIntent{Resource: "volume", RunID: "run", ExecutionID: "execution", TeamID: "team-original", Name: "ecctl-p-abcdef0123456789abcdef01", AcceptedVolumeID: "vol-second", VolumeObserved: true}
	team := func(id, transfer string) report.Resource {
		return report.Resource{Teardown: "ecctl sandbox team delete " + id + " --timeout 5m --output=json", TeamCreateConfirmed: true, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", VolumeTransferIntent: transfer}
	}
	journal := report.CleanupJournal{RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Entries: []report.Resource{team("team-original", ""), team("team-destination", first)}, VolumeTeamBindings: map[string]string{"team-destination": first}, VolumeScopeBindings: map[string][]string{first: {"team-destination"}, second.Name: {"team-destination"}}, VolumeTransferHistory: map[string]map[string]bool{second.Name: {"team-destination": true}}}
	if teams, err := VolumePossibleTeams(journal, second); err != nil || len(teams) != 2 {
		t.Fatalf("second Volume lost its retained destination: %v, %v", teams, err)
	}
	if VolumeAbsenceTerminal(journal, second, report.Resource{}) {
		t.Fatal("pending second transfer permitted terminal absence")
	}
	if changed, err := ResolveVolumeTransferObservation(&journal, second, "team-original", "AVAILABLE"); err == nil || changed || !journal.VolumeTransferHistory[second.Name]["team-destination"] {
		t.Fatalf("source observation settled second transfer: changed=%v err=%v", changed, err)
	}
	if changed, err := ResolveVolumeTransferObservation(&journal, second, "team-destination", "AVAILABLE"); err != nil || !changed || journal.VolumeTransferHistory[second.Name]["team-destination"] || journal.VolumeTeamBindings["team-destination"] != first {
		t.Fatalf("second observation changed the first Team anchor: changed=%v err=%v journal=%+v", changed, err, journal)
	}
	if VolumeAbsenceTerminal(journal, second, report.Resource{}) {
		t.Fatal("settled transfer did not make scoped absence authoritative")
	}
	delete(journal.VolumeTransferHistory, second.Name)
	if _, err := VolumePossibleTeams(journal, second); err == nil {
		t.Fatal("second Volume scope without its transfer history was accepted")
	}
}

func TestProbeVolumeScopesRequiresExactIdentityAndEveryAbsence(t *testing.T) {
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "owner", ExpectedIdentity: &pin}
	const id, name = "vol-owned", "ecctl-p-0123456789abcdef01234567"
	get := func(current, shownName string, generic bool) func(context.Context, execpkg.Config, string) execpkg.Result {
		return func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
			if bound.ExpectedIdentity != &pin || !strings.Contains(command, "--region 'cn-hangzhou' --profile 'owner'") || !strings.Contains(command, "--team-id '") {
				t.Fatalf("scope probe lost pinned command: %s", command)
			}
			team := "team-original"
			if strings.Contains(command, "--team-id 'team-destination'") {
				team = "team-destination"
			}
			if team == current || current == "both" {
				return execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"volume": map[string]any{"id": id, "name": shownName, "team_id": team, "status": "AVAILABLE"}}}
			}
			code := "VolumeNotFound"
			if generic {
				code = "404"
			}
			return execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetVolume", "code": code, "request_id": "req-specific", "provider_verified": !generic}}}}
		}
	}
	teams := []string{"team-original", "team-destination"}
	if current, absent, err := ProbeVolumeScopes(context.Background(), cfg, id, name, cfg.Region, cfg.Profile, teams, get("team-destination", name, false)); err != nil || absent || current != "team-destination" {
		t.Fatalf("transferred Volume scope = %q, %v, %v", current, absent, err)
	}
	if _, absent, err := ProbeVolumeScopes(context.Background(), cfg, id, name, cfg.Region, cfg.Profile, teams, get("", name, false)); err != nil || !absent {
		t.Fatalf("all scoped absences = %v, %v", absent, err)
	}
	if _, _, err := ProbeVolumeScopes(context.Background(), cfg, id, name, cfg.Region, cfg.Profile, teams, get("both", name, false)); err == nil {
		t.Fatal("Volume visible in two Teams was accepted")
	}
	for _, tc := range []struct {
		name, current, shownName string
		generic                  bool
	}{
		{"wrong reserved name", "team-original", "foreign-name", false},
		{"generic absence", "", name, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ProbeVolumeScopes(context.Background(), cfg, id, name, cfg.Region, cfg.Profile, teams, get(tc.current, tc.shownName, tc.generic)); err == nil {
				t.Fatal("uncertain Volume scope was accepted")
			}
		})
	}
}

func TestProbeVolumeScopesRequiresProviderProvenUncontradictedAbsence(t *testing.T) {
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "owner", ExpectedIdentity: &pin}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing provenance", func(root map[string]any) { delete(root["actions"].([]any)[0].(map[string]any), "provider_verified") }},
		{"missing request ID", func(root map[string]any) { delete(root["actions"].([]any)[0].(map[string]any), "request_id") }},
		{"malformed request ID", func(root map[string]any) { root["actions"].([]any)[0].(map[string]any)["request_id"] = "bad\x00id" }},
		{"conflicting Volume", func(root map[string]any) { root["volume"] = map[string]any{"id": "vol-owned"} }},
		{"conflicting response", func(root map[string]any) { root["response"] = map[string]any{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := func(context.Context, execpkg.Config, string) execpkg.Result {
				root := map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetVolume", "code": "VolumeNotFound", "request_id": "req-absent", "provider_verified": true}}}
				tc.change(root)
				return execpkg.Result{Started: true, Exit: 4, JSON: root}
			}
			if _, absent, err := ProbeVolumeScopes(context.Background(), cfg, "vol-owned", "reserved-name", cfg.Region, cfg.Profile, []string{"team-owned"}, probe); err == nil || absent {
				t.Fatalf("unproven Volume absence passed scope validation: absent=%v err=%v", absent, err)
			}
		})
	}
}

func TestProbeVolumeTeamAbsentRequiresExactProviderProof(t *testing.T) {
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "owner", ExpectedIdentity: &pin}
	proof := func() map[string]any {
		return map[string]any{"error": map[string]any{"kind": "not_found", "code": "NotFound"}, "actions": []any{map[string]any{"action_name": "GetTeam", "code": "TeamNotFound", "request_id": "req-exact", "provider_verified": true}}}
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		ok     bool
	}{
		{"exact absence", func(map[string]any) {}, true},
		{"generic 404", func(root map[string]any) { root["actions"].([]any)[0].(map[string]any)["code"] = "404" }, false},
		{"unverified", func(root map[string]any) { root["actions"].([]any)[0].(map[string]any)["provider_verified"] = false }, false},
		{"wrong action", func(root map[string]any) { root["actions"].([]any)[0].(map[string]any)["action_name"] = "DeleteTeam" }, false},
		{"missing request", func(root map[string]any) { delete(root["actions"].([]any)[0].(map[string]any), "request_id") }, false},
		{"contradictory Team", func(root map[string]any) { root["team"] = map[string]any{"id": "team-owned"} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
				if bound.ExpectedIdentity != &pin || !strings.Contains(command, "team get 'team-owned' --region 'cn-hangzhou' --profile 'owner'") {
					t.Fatalf("Team probe lost creator binding: %s", command)
				}
				root := proof()
				tc.change(root)
				return execpkg.Result{Started: true, Exit: 4, JSON: root}
			}
			err := ProbeVolumeTeamAbsent(context.Background(), cfg, "team-owned", cfg.Region, cfg.Profile, run)
			if (err == nil) != tc.ok {
				t.Fatalf("Team absence proof accepted=%v, want %v: %v", err == nil, tc.ok, err)
			}
		})
	}
}

func TestProbeVolumeTeamAbsentAcceptsProductionEnriched404CLIOutput(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "team-enriched404-cli.json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(fixture, &root); err != nil {
		t.Fatal(err)
	}
	pin := execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}
	cfg := execpkg.Config{Region: "cn-hangzhou", Profile: "owner", ExpectedIdentity: &pin}
	run := func(_ context.Context, bound execpkg.Config, command string) execpkg.Result {
		if bound.ExpectedIdentity != &pin || command != volumeTeamAbsenceCommand("team-1", cfg.Region, cfg.Profile) {
			t.Fatalf("Team probe lost creator binding: %s", command)
		}
		return execpkg.Result{Started: true, Exit: 4, JSON: root}
	}
	if err := ProbeVolumeTeamAbsent(context.Background(), cfg, "team-1", cfg.Region, cfg.Profile, run); err != nil {
		t.Fatalf("production CLI Team absence did not satisfy journal proof: %v", err)
	}
}
