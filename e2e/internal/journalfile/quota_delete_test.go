package journalfile

import (
	"errors"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestCheckQuotaUpdatePendingRequiresExactDurableParent(t *testing.T) {
	parent := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true}
	child := report.Resource{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true, QuotaUpdatePending: true}
	journal := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/tmp/ecctl", Entries: []report.Resource{parent, child}}
	if err := CheckQuotaUpdatePending(journal); !errors.Is(err, ErrQuotaUpdatePending) {
		t.Fatalf("valid pending child was not fenced: %v", err)
	}
	journal.Entries = []report.Resource{child}
	if err := CheckQuotaUpdatePending(journal); err == nil || errors.Is(err, ErrQuotaUpdatePending) {
		t.Fatalf("orphan pending child was accepted: %v", err)
	}
	journal.Entries = []report.Resource{parent, child}
	journal.Entries[1].QuotaUpdatePending = false
	if err := CheckQuotaUpdatePending(journal); err != nil {
		t.Fatalf("settled child remained fenced: %v", err)
	}
}

func TestIsQuotaParentTeamDeleteValidatedTarget(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --timeout 5m",
		"ecctl sandbox team delete team-owned --timeout 300s --output=json",
		"ecctl sbx team delete team-owned --output=json --timeout=1m",
	} {
		id, err := TeamDeleteTarget(command)
		if err != nil || id != "team-owned" || !IsQuotaParentTeamDelete(command) {
			t.Fatalf("validated Team finalizer was not recognized: %q id=%q err=%v", command, id, err)
		}
	}
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --timeout 0s",
		"ecctl sandbox team delete team-owned --timeout 5m --force",
		"ecctl sandbox team delete team-owned --timeout 5m --profile foreign",
		"ecctl sandbox team delete team-owned --timeout 5m --region cn-beijing",
		"ecctl sandbox team delete team-owned --timeout 5m --output=text",
		"ecctl sandbox team delete team-owned --timeout 5m --timeout 1m",
		"ecctl sandbox team delete team-owned=other --timeout 5m",
		"ecctl sandbox team delete ../other --timeout 5m",
	} {
		if IsQuotaParentTeamDelete(command) {
			t.Fatalf("unsafe Team finalizer recognized: %q", command)
		}
	}
}

func TestOrdinaryTeamDeleteTargetAcceptsHarmlessGlobalFlags(t *testing.T) {
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --timeout 5m --lang en --output=json",
		"ecctl --region cn-hangzhou sandbox team delete team-owned --lang=en",
		"ecctl sandbox --lang en team --region=cn-hangzhou remove team-owned --output json",
		"ecctl --output=json sbx team rm team-owned --no-color --json=true",
	} {
		id, err := TeamDeleteCandidateTargetForRegion(command, "cn-hangzhou")
		if err != nil || id != "team-owned" {
			t.Fatalf("ordinary Team target was lost: %q id=%q err=%v", command, id, err)
		}
	}
	for _, command := range []string{
		"ecctl sandbox team delete team-owned --region cn-beijing",
		"ecctl --region cn-hangzhou sandbox team delete team-owned --region cn-hangzhou",
		"ecctl --profile other sandbox team delete team-owned",
		"ecctl sandbox team delete team-owned --team-id foreign",
		"ecctl sandbox team delete team-owned foreign",
		"ecctl sandbox team delete team-owned --future-flag=true",
		"ecctl sandbox team delete team-owned --lang",
		"ecctl sandbox team delete team-owned --",
	} {
		if id, err := TeamDeleteCandidateTargetForRegion(command, "cn-hangzhou"); err == nil {
			t.Fatalf("unsafe ordinary Team target accepted: %q id=%q", command, id)
		}
	}
	if _, err := TeamDeleteTarget("ecctl sandbox team delete team-owned --timeout 5m --lang en"); err == nil {
		t.Fatal("confirmed Team finalizer accepted ordinary language flag")
	}
}

func TestCheckOwnedQuotaDeleteRequiresExactSettledParentAndChild(t *testing.T) {
	parent := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true}
	child := report.Resource{Scope: "case", Teardown: "ecctl sandbox quota delete team-owned --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", QuotaReserved: true}
	base := report.CleanupJournal{Version: 2, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/tmp/ecctl", Entries: []report.Resource{parent, child}}
	if err := CheckOwnedQuotaDelete(base, child); err != nil {
		t.Fatal(err)
	}
	variant := base
	variant.Entries = append([]report.Resource(nil), base.Entries...)
	variant.Entries[0].Teardown = "ecctl sbx team delete team-owned --timeout=300s --output=json"
	if err := CheckOwnedQuotaDelete(variant, child); err != nil {
		t.Fatalf("accepted Team finalizer spelling lost ownership: %v", err)
	}
	for _, mutate := range []func(*report.CleanupJournal){
		func(j *report.CleanupJournal) { j.Entries[0].TeamCreateConfirmed = false },
		func(j *report.CleanupJournal) { j.Entries[1].QuotaReserved = false },
		func(j *report.CleanupJournal) { j.Entries = j.Entries[1:] },
		func(j *report.CleanupJournal) { j.Entries = append(j.Entries, child) },
		func(j *report.CleanupJournal) { j.Entries[1].QuotaUpdatePending = true },
		func(j *report.CleanupJournal) { j.Entries[0].TeamReplayAttempted = true },
		func(j *report.CleanupJournal) { j.Entries[0].Scope = "foreign" },
		func(j *report.CleanupJournal) { j.Entries[1].Region = "foreign" },
	} {
		journal := base
		journal.Entries = append([]report.Resource(nil), base.Entries...)
		mutate(&journal)
		if err := CheckOwnedQuotaDelete(journal, child); err == nil {
			t.Fatalf("invalid Quota ownership accepted: %+v", journal)
		}
	}
	for _, command := range []string{
		"ecctl sandbox quota delete foreign-team --output=json",
		"ecctl sbx quota delete foreign-team --output=json",
		"ecctl --region cn-hangzhou sandbox quota delete foreign-team --output=json",
		"ecctl sandbox quota --output=json delete foreign-team",
		"ecctl sandbox quota --unclassified=1 delete foreign-team",
		"ecctl sandbox quota -- delete foreign-team",
	} {
		if !IsQuotaDeleteCandidate(command) {
			t.Fatalf("executable Quota delete escaped journal guard: %s", command)
		}
	}
}
