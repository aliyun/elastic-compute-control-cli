package runner

import (
	"encoding/json"
	"path/filepath"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestVolumeTeamRestartUsesAdmittedInputAndReservedTeardown(t *testing.T) {
	cl, intent, path := issue90VolumeTeamReservation(t)
	intent.Input = []string{"ecctl", "sbx", "team", "create", "--name=" + intent.Name, "--description=" + intent.Description, "--lang=zh-CN", "--json"}
	intent.CaseScope = "volume-case"
	intent.Teardown = "ecctl sbx team delete intent-validation --timeout 7m --output=json"
	body, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalfile.WriteDurable(intent.path, append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		t.Fatalf("read journal: %v", err)
	}
	loaded, err := readVolumeTeamReconcileIntent(path, journal, VolumeTeamReconcileOptions{Name: intent.Name, Profile: intent.VolumeBinding.Profile})
	if err != nil || loaded.CaseScope != "volume-case" {
		t.Fatalf("restart rejected admitted Team input: %+v %v", loaded, err)
	}
	if err := markVolumeTeamCreateDispatch(cl, intent); err != nil {
		t.Fatal(err)
	}
	if err := recordVolumeTeamCreateID(cl, intent, "team-owned", issue90TeamReceipt(intent, "team-owned")); err != nil {
		t.Fatal(err)
	}
	loaded, err = readVolumeTeamReconcileIntent(path, journal, VolumeTeamReconcileOptions{Name: intent.Name, Profile: intent.VolumeBinding.Profile})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := settleReconciledVolumeTeam(&journal, loaded, "team-owned")
	if err != nil || !changed || len(journal.Entries) != 1 || journal.Entries[0].Scope != "volume-case" || journal.Entries[0].Teardown != "ecctl sbx team delete team-owned --timeout 7m --output=json" {
		t.Fatalf("restart altered reserved Team finalizer: changed=%v err=%v entries=%+v", changed, err, journal.Entries)
	}
}

func TestVolumeTeamContradictoryReceiptCannotPromoteOnRestart(t *testing.T) {
	cl, intent, path := issue90VolumeTeamReservation(t)
	bad := execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"team": map[string]any{"id": "team-owned", "name": "other", "description": intent.Description}}}
	if err := recordVolumeTeamCreateID(cl, intent, "team-owned", bad); err == nil {
		t.Fatal("contradictory Team receipt was persisted")
	}
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		t.Fatalf("read journal: %v", err)
	}
	loaded, err := readVolumeTeamReconcileIntent(path, journal, VolumeTeamReconcileOptions{Name: intent.Name, Profile: intent.VolumeBinding.Profile})
	if err != nil || loaded.AcceptedID != "" {
		t.Fatalf("restart promoted contradictory Team receipt: %+v %v", loaded, err)
	}
}

func TestRestartVolumeTeamSettlementRequiresDurableCreateReceipt(t *testing.T) {
	cl, reserved, path := issue90VolumeTeamReservation(t)
	journal, legacy, err := readRunnerJournal(path)
	if err != nil || legacy {
		t.Fatalf("reserved Team journal: %+v %v", journal, err)
	}
	if err := markVolumeTeamCreateDispatch(cl, reserved); err != nil {
		t.Fatal(err)
	}
	opt := VolumeTeamReconcileOptions{Name: reserved.Name, Profile: reserved.VolumeBinding.Profile}
	loaded, err := readVolumeTeamReconcileIntent(path, journal, opt)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := settleReconciledVolumeTeam(&journal, loaded, "team-owned"); err == nil || changed || len(journal.Entries) != 0 {
		t.Fatalf("same-name Team gained restart delete authority without a receipt: changed=%v err=%v journal=%+v", changed, err, journal)
	}
	if err := recordVolumeTeamCreateID(cl, reserved, "team-owned", issue90TeamReceipt(reserved, "team-owned")); err != nil {
		t.Fatal(err)
	}
	loaded, err = readVolumeTeamReconcileIntent(path, journal, opt)
	if err != nil || loaded.AcceptedID != "team-owned" {
		t.Fatalf("restart lost accepted ID: %+v %v", loaded, err)
	}
	if changed, err := settleReconciledVolumeTeam(&journal, loaded, "team-foreign"); err == nil || changed || len(journal.Entries) != 0 {
		t.Fatalf("same-name lookup changed the accepted ID: changed=%v err=%v journal=%+v", changed, err, journal)
	}
	if changed, err := settleReconciledVolumeTeam(&journal, loaded, "team-owned"); err != nil || !changed || len(journal.Entries) != 1 || journal.VolumeTeamCreateIDs[reserved.Name] != "team-owned" {
		t.Fatalf("matching restart receipt did not settle exactly once: changed=%v err=%v journal=%+v", changed, err, journal)
	}
}

func TestSettleReconciledVolumeTeamIsExactAndIdempotent(t *testing.T) {
	binding := &report.QuotaCredentialBinding{Profile: "owned", Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-a"}}
	name := "ecctl-t-0123456789abcdef01234567"
	journalPath := filepath.Join(t.TempDir(), "cleanup.json")
	intent := &teamCreateIntent{Version: 1, RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", EcctlBin: filepath.Join(filepath.Dir(journalPath), "ecctl"), Name: name, AcceptedID: "team-owned", CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", VolumeBinding: binding, DispatchProtocol: 1, Dispatched: true}
	fresh := func() report.CleanupJournal {
		return report.CleanupJournal{Version: 2, JournalPath: journalPath, RunID: intent.RunID, ExecutionID: intent.ExecutionID, RegionRole: "primary", Region: intent.Region, Surface: intent.Surface, EcctlBin: intent.EcctlBin, VolumeTeamCreateIDs: map[string]string{name: ""}}
	}
	journal := fresh()
	intent.AcceptedID = ""
	if changed, err := settleReconciledVolumeTeam(&journal, intent, "team-owned"); err == nil || changed || len(journal.Entries) != 0 {
		t.Fatalf("name-only Team lookup granted delete authority: changed=%v err=%v journal=%+v", changed, err, journal)
	}
	intent.AcceptedID = "team-owned"
	changed, err := settleReconciledVolumeTeam(&journal, intent, "team-owned")
	if err != nil || !changed || len(journal.Entries) != 1 || journal.VolumeTeamCreateIDs[name] != "team-owned" || journal.VolumeTeamCreators["team-owned"] != binding {
		t.Fatalf("first settlement = changed %v, error %v, journal %+v", changed, err, journal)
	}
	changed, err = settleReconciledVolumeTeam(&journal, intent, "team-owned")
	if err != nil || changed || len(journal.Entries) != 1 {
		t.Fatalf("repeated settlement changed finalizer: %v %v %+v", changed, err, journal)
	}
	for _, tc := range []struct {
		name   string
		change func(*report.CleanupJournal)
	}{
		{"nonce missing", func(j *report.CleanupJournal) { delete(j.VolumeTeamCreateIDs, name) }},
		{"nonce collision", func(j *report.CleanupJournal) { j.VolumeTeamCreateIDs["other"] = "team-owned" }},
		{"creator collision", func(j *report.CleanupJournal) {
			j.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{"team-owned": &report.QuotaCredentialBinding{Profile: "foreign"}}
		}},
		{"finalizer collision", func(j *report.CleanupJournal) { j.Entries[0].Region = "cn-beijing" }},
		{"duplicate finalizer", func(j *report.CleanupJournal) { j.Entries = append(j.Entries, j.Entries[0]) }},
		{"settled without finalizer", func(j *report.CleanupJournal) { j.Entries = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := journal
			copy.VolumeTeamCreateIDs = map[string]string{name: "team-owned"}
			copy.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{"team-owned": binding}
			copy.Entries = append([]report.Resource(nil), journal.Entries...)
			tc.change(&copy)
			if changed, err := settleReconciledVolumeTeam(&copy, intent, "team-owned"); err == nil || changed {
				t.Fatalf("collision accepted: changed %v, err %v, journal %+v", changed, err, copy)
			}
		})
	}
	journal = fresh()
	journal.Entries = append(journal.Entries, report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", RegionRole: "primary", Region: intent.Region, ExecutionID: intent.ExecutionID, TeamCreateConfirmed: true, VolumeTeamBound: true})
	journal.VolumeTeamCreators = map[string]*report.QuotaCredentialBinding{"team-owned": binding}
	changed, err = settleReconciledVolumeTeam(&journal, intent, "team-owned")
	if err != nil || !changed || len(journal.Entries) != 1 || journal.VolumeTeamCreateIDs[name] != "team-owned" {
		t.Fatalf("restart between finalizer and nonce settlement = %v %v %+v", changed, err, journal)
	}
}

func TestReservedTeamListRejectsZeroMultipleAndOwnerCollision(t *testing.T) {
	intent := &teamCreateIntent{Name: "ecctl-t-0123456789abcdef01234567", Description: "ecctl E2E Team owner=0123456789abcdef01234567"}
	team := func(id, description string) any {
		return map[string]any{"id": id, "name": intent.Name, "description": description}
	}
	for _, tc := range []struct {
		name  string
		teams []any
		more  any
		want  string
	}{
		{"one exact", []any{team("team-owned", intent.Description)}, false, "team-owned"},
		{"zero", []any{}, false, ""},
		{"multiple", []any{team("team-one", intent.Description), team("team-two", intent.Description)}, false, ""},
		{"same name different owner", []any{team("team-foreign", "foreign")}, false, ""},
		{"collision beside exact", []any{team("team-owned", intent.Description), team("team-foreign", "foreign")}, false, ""},
		{"partial page", []any{team("team-owned", intent.Description)}, true, ""},
		{"missing page proof", []any{team("team-owned", intent.Description)}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reservedTeamIDFromList(map[string]any{"teams": tc.teams, "pagination": map[string]any{"has_more": tc.more}}, intent)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("Team list proof = %q, %v", got, err)
			}
		})
	}
}
