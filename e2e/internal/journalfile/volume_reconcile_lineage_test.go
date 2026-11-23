package journalfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestHistoricalVolumeRegistryIgnoresOtherRunFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for i := 0; i < 4097; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("other-run-%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "cleanup.json")
	journal := report.CleanupJournal{OriginalJournalSHA256: strings.Repeat("a", 64)}
	entry := report.Resource{Teardown: "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"}
	found, err := historicalRegisteredVolumeFinalizer(root, path, journal, entry)
	if err != nil || found {
		t.Fatalf("other runs exhausted the local archive bound: found=%t err=%v", found, err)
	}
}

func TestVolumeReconcileLineageRejectsDuplicateFinalizerIDs(t *testing.T) {
	team := report.Resource{Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", TeamCreateConfirmed: true, VolumeTeamBound: true}
	journal := report.CleanupJournal{VolumeTeamCreateIDs: map[string]string{"ecctl-t-0123456789abcdef01234567": "team-owned"}, Entries: []report.Resource{team, team}}
	if err := validateVolumeReconcileUniqueIDs(journal); err == nil {
		t.Fatal("duplicate Team finalizer ID was accepted")
	}
	volume := report.Resource{Teardown: "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"}
	journal.Entries = []report.Resource{team, volume, volume}
	if err := validateVolumeReconcileUniqueIDs(journal); err == nil {
		t.Fatal("duplicate Volume finalizer ID was accepted")
	}
}

func TestReplaySidecarSnapshotIncludesNativeIntentRequiredByLineage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	sidecar := path + ".template-create-0123456789abcdef01234567.json"
	body := []byte(`{"resource":"volume"}`)
	if err := WriteDurable(sidecar, body); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	contents, authenticated, err := volumeReplayProtectedSidecars(root, path, map[string]string{sidecar: volumeReconcileDigest(body)})
	if err != nil || !authenticated[filepath.Base(sidecar)] || string(contents[filepath.Base(sidecar)]) != string(body) {
		t.Fatalf("native sidecar missing from private snapshot: %v", err)
	}
	if _, _, err := volumeReplayProtectedSidecars(root, path, map[string]string{filepath.Join(filepath.Dir(path), "other.json"): volumeReconcileDigest(body)}); err == nil {
		t.Fatal("unrelated expected sidecar path was accepted")
	}
	if err := WriteDurable(sidecar, []byte(`{"resource":"volume","resource":"template"}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := volumeReplayProtectedSidecars(root, path); err == nil {
		t.Fatal("ambiguous native sidecar JSON was accepted")
	}
}

func TestVolumeTeamNonceOnlyTransitionKeepsExistingFinalizer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	name, id := "ecctl-t-0123456789abcdef01234567", "team-owned"
	sidecarPath := path + ".team-create-" + name[8:] + ".json"
	binding := &report.QuotaCredentialBinding{Profile: "owner"}
	command := "ecctl sandbox team delete team-owned --timeout 5m --output=json"
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("a", 64), VolumeFixtureBinding: binding, VolumeTeamCreateIDs: map[string]string{name: ""}, VolumeTeamCreators: map[string]*report.QuotaCredentialBinding{id: binding}, Entries: []report.Resource{{Scope: "case", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTeamBound: true}}}
	intent := volumeTeamReceipt{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, EcctlBin: journal.EcctlBin, Name: name, Description: "ecctl E2E Team owner=" + name[8:], Input: []string{"ecctl"}, CaseScope: "case", Teardown: "ecctl sandbox team delete intent-validation --timeout 5m --output=json", AcceptedID: id, Dispatched: true, DispatchProtocol: 1, VolumeBinding: binding, CreatedAt: time.Now().UTC()}
	before, _ := json.Marshal(journal)
	sidecar, _ := json.Marshal(intent)
	if err := WriteDurable(path, before); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurable(sidecarPath, sidecar); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	journal.VolumeTeamCreateIDs[name] = id
	journal.OriginalJournalSHA256 = volumeReconcileDigest(before)
	after, _ := json.Marshal(journal)
	if err := StageVolumeReconcileTransition(root, path, "team-accept", name, before, after, sidecarPath, sidecar); err != nil {
		t.Fatalf("nonce-only transition rejected: %v", err)
	}
	changedProof := intent
	changedProof.ProbeBinSHA256 = strings.Repeat("b", 64)
	changedSidecar, _ := json.Marshal(changedProof)
	selected, err := StageVolumeReconcileTransitionSelected(root, path, "team-accept", name, before, after, sidecarPath, changedSidecar)
	if err != nil || len(selected) != 1 || string(selected[0]) != string(sidecar) {
		t.Fatalf("retry did not select archived sidecar bytes: %v %q", err, selected)
	}
	if _, err := StageVolumeReconcileTransitionSelected(root, path, "team-retire", name, before, after, sidecarPath, sidecar); err == nil {
		t.Fatal("changed staged operation reused Team proof")
	}
	if err := WriteDurableRoot(root, filepath.Base(path), after); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, filepath.Base(sidecarPath), []byte(`{"changed":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := ReaffirmInstalledVolumeReconcileTransition(root, path, volumeReconcileDigest(before), name, after, "team-accept"); err == nil {
		t.Fatal("retry accepted a sidecar outside the staged edge")
	}
	if err := WriteDurableRoot(root, filepath.Base(sidecarPath), sidecar); err != nil {
		t.Fatal(err)
	}
	forked := journal
	forked.VolumeTeamCreateIDs[name] = "team-other"
	forkedBody, _ := json.Marshal(forked)
	if err := StageVolumeReconcileTransition(root, path, "team-accept", name, before, forkedBody, sidecarPath, sidecar); err == nil || !strings.Contains(err.Error(), "installed journal state") {
		t.Fatalf("second successor from committed predecessor was admitted: %v", err)
	}
	if len(journal.Entries) != 1 || journal.Entries[0].Teardown != command {
		t.Fatal("nonce-only transition changed finalizer position")
	}
}

func TestVolumeReconcileRetiresUnusedCompetingEdge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.json")
	journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("a", 64), Entries: []report.Resource{
		{Scope: "first", Teardown: "ecctl ecs instance delete i-first", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
		{Scope: "second", Teardown: "ecctl ecs instance delete i-second", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"},
	}}
	before, _ := json.Marshal(journal)
	if err := WriteDurable(path, before); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	first, err := cloneVolumeReconcileJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	first.OriginalJournalSHA256 = volumeReconcileDigest(before)
	if err := EnsureHistoricalJournalOriginal(root, path, first.OriginalJournalSHA256, before); err != nil {
		t.Fatal(err)
	}
	first.Entries[0].TeamReplayAttempted = true
	firstBody, _ := json.Marshal(first)
	if err := StageVolumeReconcileTransition(root, path, "replay-entry", "replay", before, firstBody, ""); err != nil {
		t.Fatal(err)
	}
	second, err := cloneVolumeReconcileJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	second.OriginalJournalSHA256 = volumeReconcileDigest(before)
	second.Entries[1].TeamReplayAttempted = true
	secondBody, _ := json.Marshal(second)
	if err := StageVolumeReconcileTransition(root, path, "replay-entry", "replay", before, secondBody, ""); err != nil {
		t.Fatalf("unused edge did not retire durably: %v", err)
	}
	if _, err := root.Lstat(volumeReconcileTransitionName(path, volumeReconcileDigest(firstBody))); !os.IsNotExist(err) {
		t.Fatalf("unused edge remains active: %v", err)
	}
	if _, err := root.Lstat(volumeReconcileTransitionName(path, volumeReconcileDigest(secondBody))); err != nil {
		t.Fatalf("replacement edge is missing: %v", err)
	}
	installed, err := root.ReadFile(filepath.Base(path))
	if err != nil || string(installed) != string(before) {
		t.Fatalf("edge retirement changed the predecessor: %v", err)
	}
}

func TestVolumeSettleTransitionRequiresSameAcceptedFinalizer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	name := "ecctl-p-0123456789abcdef01234567"
	started, exit := true, 0
	before := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", Name: name, Profile: "owner", TeamID: "team-owned", CaseScope: "case", State: "settling", Started: &started, Exit: &exit, StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64), AcceptedVolumeID: "vol-owned", Recovered: true, VolumeObserved: true, ReconcileSHA256: strings.Repeat("c", 64), Deletes: []string{"ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"}, CreatedAt: time.Now().UTC()}
	after := before
	after.State = "journaled"
	beforeBody, _ := json.Marshal(before)
	afterBody, _ := json.Marshal(after)
	transition := volumeReconcileTransition{Kind: "volume-settle", JournalPath: path, Target: name, RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", SidecarPath: path + ".template-create-" + name[8:] + ".json", SidecarStates: []string{volumeReconcileDigest(beforeBody), volumeReconcileDigest(afterBody)}}
	if err := validateVolumeReconcileSidecar(transition, [][]byte{beforeBody, afterBody}); err != nil {
		t.Fatalf("exact sidecar-only settlement was rejected: %v", err)
	}
	after.AcceptedVolumeID = "vol-other"
	changed, _ := json.Marshal(after)
	transition.SidecarStates[1] = volumeReconcileDigest(changed)
	if err := validateVolumeReconcileSidecar(transition, [][]byte{beforeBody, changed}); err == nil {
		t.Fatal("sidecar-only settlement changed accepted Volume ID")
	}
}

func TestVolumeReconcileLineageRequiresAcceptedVolumeID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	name := "ecctl-p-0123456789abcdef01234567"
	started, exit := true, 0
	before := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", Name: name, Profile: "owner", State: "pending", Started: &started, Exit: &exit, StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64), AcceptedVolumeID: "vol-other", CreatedAt: time.Now().UTC()}
	after := before
	after.State, after.Recovered, after.VolumeObserved, after.ReconcileSHA256 = "journaled", true, true, strings.Repeat("c", 64)
	after.Deletes = []string{"ecctl sandbox volume delete vol-one --region cn-hangzhou --profile owner"}
	settling := after
	settling.State = "settling"
	beforeBody, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	settlingBody, err := json.Marshal(settling)
	if err != nil {
		t.Fatal(err)
	}
	afterBody, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	transition := volumeReconcileTransition{Kind: "volume-register", JournalPath: path, Target: name, RunID: "run", ExecutionID: "execution", Region: "cn-hangzhou", Surface: "public", SidecarPath: path + ".template-create-" + name[8:] + ".json", SidecarStates: []string{volumeReconcileDigest(beforeBody), volumeReconcileDigest(settlingBody), volumeReconcileDigest(afterBody)}}
	if err := validateVolumeReconcileSidecar(transition, [][]byte{beforeBody, settlingBody, afterBody}); err == nil {
		t.Fatal("Volume finalizer for a different accepted ID was admitted")
	}
}

func TestVolumeRegisterAnchorsExistingFinalizerToExactSidecarReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := "ecctl-p-0123456789abcdef01234567"
	sidecar := path + ".template-create-" + name[8:] + ".json"
	command := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	started, exit := true, 0
	pending := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", Name: name, Profile: "owner", TeamID: "team-owned", CaseScope: "case", State: "pending", Started: &started, Exit: &exit, StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64), AcceptedVolumeID: "vol-owned", CreatedAt: time.Now().UTC()}
	settling := pending
	settling.State, settling.Recovered, settling.VolumeObserved, settling.ReconcileSHA256 = "settling", true, true, strings.Repeat("c", 64)
	settling.Deletes = []string{command}
	journaled := settling
	journaled.State = "journaled"
	states := make([][]byte, 0, 3)
	for _, intent := range []TemplateCreateIntent{pending, settling, journaled} {
		body, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, body)
	}
	entry := report.Resource{Scope: "case", Teardown: command, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
	prior := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("e", 64), OriginalJournalSHA256: strings.Repeat("f", 64), NativeIntentPaths: []string{sidecar}, VolumeTeamCreateIDs: map[string]string{"ecctl-t-0123456789abcdef01234567": "team-owned"}, Entries: []report.Resource{
		{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTeamBound: true},
		entry,
	}}
	next := prior
	next.VolumeObservationReceipts = map[string]string{name: volumeReconcileDigest(states[2])}
	transition := volumeReconcileTransition{Version: 1, JournalPath: path, RunID: prior.RunID, ExecutionID: prior.ExecutionID, Region: prior.Region, Surface: prior.Surface, OriginalSHA256: prior.OriginalJournalSHA256, Kind: "volume-register", Target: name, SidecarPath: sidecar, SidecarStates: []string{volumeReconcileDigest(states[0]), volumeReconcileDigest(states[1]), volumeReconcileDigest(states[2])}}
	check := func(before, after report.CleanupJournal) error {
		beforeBody, _ := json.Marshal(before)
		afterBody, _ := json.Marshal(after)
		transition.BeforeSHA256, transition.AfterSHA256 = volumeReconcileDigest(beforeBody), volumeReconcileDigest(afterBody)
		return validateVolumeReconcileTransition(root, transition, before, after, states, nil)
	}
	if err := check(prior, next); err != nil {
		t.Fatalf("anchored existing finalizer was rejected: %v", err)
	}
	if err := check(prior, prior); err == nil {
		t.Fatal("existing finalizer settled without a journal receipt")
	}
	conflict := prior
	conflict.Entries = append([]report.Resource(nil), prior.Entries...)
	conflict.Entries[1].Scope = "foreign"
	if err := check(conflict, next); err == nil {
		t.Fatal("different finalizer scope was accepted")
	}
	duplicate := prior
	duplicate.Entries = append(append([]report.Resource(nil), prior.Entries...), entry)
	if err := check(duplicate, next); err == nil {
		t.Fatal("duplicate exact finalizers were accepted")
	}
}

func TestRegisteredVolumeFinalizerCanRecordReplayAttemptButNotRetireWithoutProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.json")
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := "ecctl-p-0123456789abcdef01234567"
	sidecar := path + ".template-create-" + name[8:] + ".json"
	deleteCommand := "ecctl sandbox volume delete vol-owned --region cn-hangzhou --profile owner"
	started, exit := true, 0
	pending := TemplateCreateIntent{Version: 2, Resource: "volume", RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", Name: name, Profile: "owner", TeamID: "team-owned", CaseScope: "case", State: "pending", Started: &started, Exit: &exit, StdoutSHA256: strings.Repeat("a", 64), StderrSHA256: strings.Repeat("b", 64), AcceptedVolumeID: "vol-owned", CreatedAt: time.Now().UTC()}
	settling := pending
	settling.State, settling.Recovered, settling.VolumeObserved, settling.ReconcileSHA256 = "settling", true, true, strings.Repeat("c", 64)
	settling.Deletes = []string{deleteCommand}
	journaled := settling
	journaled.State = "journaled"
	states := make([][]byte, 0, 3)
	for _, intent := range []TemplateCreateIntent{pending, settling, journaled} {
		body, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, body)
	}
	team := report.Resource{Scope: "case", Teardown: "ecctl sandbox team delete team-owned --timeout 5m --output=json", RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution", TeamCreateConfirmed: true, VolumeTeamBound: true}
	volume := report.Resource{Scope: "case", Teardown: deleteCommand, RegionRole: "primary", Region: "cn-hangzhou", ExecutionID: "execution"}
	before := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", ExecutionID: "execution", RegionRole: "primary", Region: "cn-hangzhou", Surface: "public", EcctlBin: "/inert/ecctl", EcctlBinSHA256: strings.Repeat("e", 64), NativeIntentPaths: []string{sidecar}, VolumeTeamCreateIDs: map[string]string{"ecctl-t-0123456789abcdef01234567": "team-owned"}, Entries: []report.Resource{team}}
	beforeBody, _ := json.Marshal(before)
	if err := WriteDurableRoot(root, filepath.Base(path), beforeBody); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, filepath.Base(sidecar), states[0]); err != nil {
		t.Fatal(err)
	}
	registered, err := cloneVolumeReconcileJournal(before)
	if err != nil {
		t.Fatal(err)
	}
	registered.OriginalJournalSHA256 = volumeReconcileDigest(beforeBody)
	registered.Entries = append(registered.Entries, volume)
	registeredBody, _ := json.Marshal(registered)
	if err := StageVolumeReconcileTransition(root, path, "volume-register", name, beforeBody, registeredBody, sidecar, states...); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, filepath.Base(path), registeredBody); err != nil {
		t.Fatal(err)
	}
	if err := WriteDurableRoot(root, filepath.Base(sidecar), states[2]); err != nil {
		t.Fatal(err)
	}
	marked, err := cloneVolumeReconcileJournal(registered)
	if err != nil {
		t.Fatal(err)
	}
	marked.Entries[1].VolumeDeleteAttempted = true
	markedBody, _ := json.Marshal(marked)
	if err := StageVolumeReconcileTransition(root, path, "replay-entry", "replay", registeredBody, markedBody, ""); err != nil {
		t.Fatalf("registered Volume could not record its first replay attempt: %v", err)
	}
	removed, err := cloneVolumeReconcileJournal(marked)
	if err != nil {
		t.Fatal(err)
	}
	removed.Entries = removed.Entries[:1]
	removedBody, _ := json.Marshal(removed)
	if err := StageVolumeReconcileTransition(root, path, "replay-entry", "replay", markedBody, removedBody, ""); err == nil {
		t.Fatal("registered Volume retired without a terminal proof")
	}
}
