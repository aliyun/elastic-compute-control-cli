package journalfile

import (
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func transferTestIntent() TemplateCreateIntent {
	command := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	return TemplateCreateIntent{
		Resource: "volume", State: "journaled", Name: "ecctl-p-0123456789abcdef01234567",
		RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou",
		Surface: "public", EcctlBin: "/bin/ecctl", Profile: "owner", TeamID: "team-source", AcceptedVolumeID: "vol-owned",
		CaseScope: "case", VolumeObserved: true, Deletes: []string{command},
		Identity: execpkg.IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "user-owned"},
	}
}

func transferTestJournal() report.CleanupJournal {
	intent := transferTestIntent()
	source := report.Resource{Teardown: "ecctl sandbox team delete team-source --timeout 5m --output=json", TeamCreateConfirmed: true, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", Scope: "case"}
	dest := report.Resource{Teardown: "ecctl sandbox team delete team-destination --timeout 5m --output=json", TeamCreateConfirmed: true, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", Scope: "case"}
	finalizer := report.Resource{Teardown: intent.Deletes[0], RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", Scope: "case"}
	return report.CleanupJournal{RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/bin/ecctl", Entries: []report.Resource{source, dest, finalizer}}
}

func TestParseVolumeUpdateRequest(t *testing.T) {
	request, err := ParseVolumeUpdateRequest("ecctl sandbox volume update vol-owned --team-id team-destination --region cn-hangzhou --output=json")
	if err != nil || request.ID != "vol-owned" || request.TeamID != "team-destination" || !request.TeamSet || !request.RegionSet || request.Output != "json" {
		t.Fatalf("valid transfer parse = %+v, %v", request, err)
	}
	for _, command := range []string{
		"ecctl sandbox volume update vol-owned",
		"ecctl sandbox volume update vol-owned --status AVAILABLE",
		"ecctl sandbox volume update vol-owned --team-id team-destination -o json",
		"ecctl sandbox volume get vol-owned --team-id team-destination",
		"ecctl sandbox volume update --team-id team-destination",
		"ecctl sandbox volume update vol-owned --team-id team-destination --timeout 0s",
		"ecctl sandbox volume update vol-owned --team-id 'bad team'",
		"ecctl call fcsandbox UpdateVolume --request '{}'",
	} {
		if _, err := ParseVolumeUpdateRequest(command); err == nil {
			t.Errorf("invalid transfer was accepted: %s", command)
		}
	}
}

func TestReserveVolumeTransfer(t *testing.T) {
	intent := transferTestIntent()
	journal := transferTestJournal()
	if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err != nil {
		t.Fatalf("reserve failed: %v", err)
	}
	if journal.VolumeTeamBindings["team-destination"] != intent.Name ||
		len(journal.VolumeScopeBindings[intent.Name]) != 1 ||
		!journal.VolumeTransferHistory[intent.Name]["team-destination"] ||
		!journal.Entries[1].VolumeTransferPending || journal.Entries[1].VolumeTransferIntent != intent.Name {
		t.Fatalf("reserve markers missing: %+v", journal)
	}
	if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err == nil {
		t.Fatal("second reserve on the same Volume was accepted")
	}
	for _, tc := range []struct {
		name string
		edit func(*report.CleanupJournal, *TemplateCreateIntent)
		dest string
	}{
		{"same Team", func(_ *report.CleanupJournal, _ *TemplateCreateIntent) {}, "team-source"},
		{"unobserved", func(_ *report.CleanupJournal, i *TemplateCreateIntent) { i.VolumeObserved = false }, "team-destination"},
		{"not journaled", func(_ *report.CleanupJournal, i *TemplateCreateIntent) { i.State = "pending" }, "team-destination"},
		{"missing destination Team", func(j *report.CleanupJournal, _ *TemplateCreateIntent) {
			j.Entries = append(j.Entries[:1], j.Entries[2])
		}, "team-destination"},
		{"destination replayed", func(j *report.CleanupJournal, _ *TemplateCreateIntent) { j.Entries[1].TeamReplayAttempted = true }, "team-destination"},
		{"existing scope", func(j *report.CleanupJournal, i *TemplateCreateIntent) {
			j.VolumeScopeBindings = map[string][]string{i.Name: {"team-other"}}
		}, "team-destination"},
		{"destination bound", func(j *report.CleanupJournal, i *TemplateCreateIntent) {
			j.VolumeTeamBindings = map[string]string{"team-destination": i.Name}
		}, "team-destination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyJournal, copyIntent := transferTestJournal(), transferTestIntent()
			tc.edit(&copyJournal, &copyIntent)
			if err := ReserveVolumeTransfer(&copyJournal, copyIntent, tc.dest); err == nil {
				t.Fatal("invalid reserve was accepted")
			}
		})
	}
}

func TestReanchorVolumeTransfer(t *testing.T) {
	intent := transferTestIntent()
	journal := transferTestJournal()
	if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err != nil {
		t.Fatal(err)
	}
	if err := ReanchorVolumeTransfer(&journal, &intent, "team-destination"); err == nil {
		t.Fatal("reanchor before settlement was accepted")
	}
	if changed, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", "AVAILABLE"); err != nil || !changed {
		t.Fatalf("settlement failed: %v %v", changed, err)
	}
	if err := ReanchorVolumeTransfer(&journal, &intent, "team-destination"); err != nil {
		t.Fatalf("reanchor failed: %v", err)
	}
	if intent.TeamID != "team-destination" || len(journal.VolumeTeamBindings) != 0 ||
		len(journal.VolumeScopeBindings) != 0 || len(journal.VolumeTransferHistory) != 0 ||
		journal.Entries[1].VolumeTransferIntent != "" || journal.Entries[1].VolumeTransferPending {
		t.Fatalf("reanchor left stale markers: intent=%+v journal=%+v", intent, journal)
	}
	teams, err := VolumePossibleTeams(journal, intent)
	if err != nil || len(teams) != 1 || teams[0] != "team-destination" {
		t.Fatalf("reanchored scope = %v, %v", teams, err)
	}
	if !VolumeAbsenceTerminal(journal, intent, journal.Entries[2]) {
		t.Fatal("reanchored Volume is not eligible for single-Team terminal absence")
	}
}

func TestReanchorVolumeTransferRejections(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*report.CleanupJournal, *TemplateCreateIntent)
	}{
		{"wrong binding", func(j *report.CleanupJournal, i *TemplateCreateIntent) {
			j.VolumeTeamBindings["team-destination"] = "ecctl-p-ffffffffffffffffffffffff"
		}},
		{"missing marker", func(j *report.CleanupJournal, _ *TemplateCreateIntent) { j.Entries[1].VolumeTransferIntent = "" }},
		{"ambiguous marker", func(j *report.CleanupJournal, i *TemplateCreateIntent) {
			j.Entries[0].VolumeTransferIntent = i.Name
			j.Entries[0].VolumeTransferPending = false
		}},
		{"same Team", func(_ *report.CleanupJournal, i *TemplateCreateIntent) { i.TeamID = "team-destination" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := transferTestIntent()
			journal := transferTestJournal()
			if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err != nil {
				t.Fatal(err)
			}
			if _, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", "AVAILABLE"); err != nil {
				t.Fatal(err)
			}
			tc.edit(&journal, &intent)
			if err := ReanchorVolumeTransfer(&journal, &intent, "team-destination"); err == nil {
				t.Fatal("invalid reanchor was accepted")
			}
		})
	}
}

func TestRestoreUnstartedVolumeTransfer(t *testing.T) {
	intent := transferTestIntent()
	journal := transferTestJournal()
	if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err != nil {
		t.Fatal(err)
	}
	if err := RestoreUnstartedVolumeTransfer(&journal, intent, "team-destination"); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if len(journal.VolumeTeamBindings) != 0 || len(journal.VolumeScopeBindings) != 0 || len(journal.VolumeTransferHistory) != 0 ||
		journal.Entries[1].VolumeTransferIntent != "" || journal.Entries[1].VolumeTransferPending {
		t.Fatalf("restore left markers: %+v", journal)
	}
	if err := RestoreUnstartedVolumeTransfer(&journal, intent, "team-destination"); err == nil {
		t.Fatal("restore without a pending reservation was accepted")
	}
	if err := ReserveVolumeTransfer(&journal, intent, "team-destination"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveVolumeTransferObservation(&journal, intent, "team-destination", "AVAILABLE"); err != nil {
		t.Fatal(err)
	}
	if err := RestoreUnstartedVolumeTransfer(&journal, intent, "team-destination"); err == nil {
		t.Fatal("restore of a settled transfer was accepted")
	}
}
