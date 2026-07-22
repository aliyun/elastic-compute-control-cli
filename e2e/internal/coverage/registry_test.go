package coverage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	parseTime  = time.Date(2026, 7, 15, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	changeTime = parseTime.Add(time.Hour)
)

func TestRegistryCheckAcceptsValidV3Registry(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	operation := validRegistryOperation(t, cases, StatusOffline, ReasonNotRun)
	writeRegistry(t, registryPath, map[string]RegistryOperation{
		"create": operation,
		"delete": withStatus(operation, StatusLivePass, ReasonLiveVerified),
	})

	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Invalid != 0 || len(report.Errors) != 0 {
		t.Fatalf("expected valid registry, got %+v", report)
	}
	if report.Declared != 3 || report.Entries != 2 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.ByStatus[StatusOffline] != 1 || report.ByStatus[StatusLivePass] != 1 {
		t.Fatalf("unexpected status summary: %+v", report.ByStatus)
	}
}

func TestRegistryCheckRequiresSelectedCapabilitiesToBeLive(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	operation := validRegistryOperation(t, cases, StatusLivePass, ReasonLiveVerified)
	writeRegistry(t, registryPath, map[string]RegistryOperation{"create": operation})

	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{
		CapabilityFilter: map[Capability]bool{{Resource: "ecs/instance", Verb: "create"}: true},
		FailOnNotLive:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Invalid != 0 || report.ByStatus[StatusLivePass] != 1 {
		t.Fatalf("live filtered report = %+v", report)
	}

	report, err = CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{
		CapabilityFilter: map[Capability]bool{{Resource: "ecs/instance", Verb: "delete"}: true},
		FailOnNotLive:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "not_live") {
		t.Fatalf("absent selected capability must be not_live: %+v", report.Errors)
	}

	operation = withStatus(operation, StatusOffline, ReasonTestFailed)
	writeRegistry(t, registryPath, map[string]RegistryOperation{"create": operation})
	report, err = CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{FailOnNotLive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "not_live") {
		t.Fatalf("offline operation must be not_live: %+v", report.Errors)
	}
}

func TestRegistryCheckRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RegistryOperation)
		code   string
	}{
		{name: "unknown status", mutate: func(op *RegistryOperation) { op.Status = "maybe" }, code: "unknown_status"},
		{name: "missing case", mutate: func(op *RegistryOperation) { op.Case = "" }, code: "missing_case"},
		{name: "wrong case", mutate: func(op *RegistryOperation) { op.Case = "cases/ecs/other.yaml" }, code: "case_mismatch"},
		{name: "missing fingerprint", mutate: func(op *RegistryOperation) { op.Fingerprint = "" }, code: "missing_fingerprint"},
		{name: "invalid fingerprint", mutate: func(op *RegistryOperation) { op.Fingerprint = "sha256:ABC" }, code: "invalid_fingerprint"},
		{name: "stale fingerprint", mutate: func(op *RegistryOperation) { op.Fingerprint = "sha256:" + strings.Repeat("0", 64) }, code: "fingerprint_mismatch"},
		{name: "missing time", mutate: func(op *RegistryOperation) { op.Time = "" }, code: "missing_time"},
		{name: "invalid time", mutate: func(op *RegistryOperation) { op.Time = "soon" }, code: "invalid_time"},
		{name: "missing reason", mutate: func(op *RegistryOperation) { op.Reason = "" }, code: "missing_reason"},
		{name: "live with offline reason", mutate: func(op *RegistryOperation) { op.Status, op.Reason = StatusLivePass, ReasonNotRun }, code: "invalid_reason"},
		{name: "offline with live reason", mutate: func(op *RegistryOperation) { op.Reason = ReasonLiveVerified }, code: "invalid_reason"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, specs, cases := writeRegistryFixture(t)
			registryPath := filepath.Join(root, "coverage.yaml")
			operation := validRegistryOperation(t, cases, StatusOffline, ReasonNotRun)
			test.mutate(&operation)
			writeRegistry(t, registryPath, map[string]RegistryOperation{"create": operation})
			report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reportHasCode(report, test.code) {
				t.Fatalf("expected %q, got %+v", test.code, report.Errors)
			}
		})
	}
}

func TestRegistryCheckRejectsStaleOperation(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	writeRegistry(t, registryPath, map[string]RegistryOperation{
		"reboot": validRegistryOperation(t, cases, StatusOffline, ReasonNotRun),
	})
	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "stale_entry") {
		t.Fatalf("expected stale_entry, got %+v", report.Errors)
	}
}

func TestRegistryCheckRejectsResourceAlias(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	operation := validRegistryOperation(t, cases, StatusOffline, ReasonNotRun)
	registry := &Registry{
		Version: RegistryVersion,
		Summary: RegistryPublicSummary{Surface: "public", Resources: 1, Operations: 3, MissingCases: 3},
		Resources: map[string]RegistryProduct{
			"ecs": {"vm": {Operations: map[string]RegistryOperation{"create": operation}}},
		},
	}
	if err := WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatal(err)
	}
	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "stale_entry") {
		t.Fatalf("resource aliases must not be accepted as canonical keys: %+v", report.Errors)
	}
}

func TestRegistryCheckRejectsEmptyResource(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	writeRegistry(t, registryPath, map[string]RegistryOperation{})
	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "empty_resource") {
		t.Fatalf("expected empty_resource, got %+v", report.Errors)
	}
}

func TestRegistryCheckRejectsStalePublicSummary(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registryPath := filepath.Join(root, "coverage.yaml")
	operation := validRegistryOperation(t, cases, StatusLivePass, ReasonLiveVerified)
	writeRegistry(t, registryPath, map[string]RegistryOperation{"create": operation})
	registry, err := LoadRegistryFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry.Summary.Passed = 0
	registry.Summary.MissingCases = 3
	if err := WriteRegistryFile(registryPath, registry); err != nil {
		t.Fatal(err)
	}
	public := fixturePublicSurface()
	report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{PublicSurface: &public})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(report, "stale_summary") {
		t.Fatalf("expected stale_summary, got %+v", report.Errors)
	}
}

func TestRegistryCheckRejectsInvalidPublicSummary(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RegistryPublicSummary)
		code   string
	}{
		{name: "surface", mutate: func(summary *RegistryPublicSummary) { summary.Surface = "full" }, code: "invalid_summary_surface"},
		{name: "negative count", mutate: func(summary *RegistryPublicSummary) { summary.Resources = -1 }, code: "invalid_summary_count"},
		{name: "unbalanced total", mutate: func(summary *RegistryPublicSummary) { summary.Operations++ }, code: "invalid_summary_total"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, specs, cases := writeRegistryFixture(t)
			registryPath := filepath.Join(root, "coverage.yaml")
			writeRegistry(t, registryPath, map[string]RegistryOperation{})
			registry, err := LoadRegistryFile(registryPath)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&registry.Summary)
			if err := WriteRegistryFile(registryPath, registry); err != nil {
				t.Fatal(err)
			}
			report, err := CheckRegistryFile(specs, cases, registryPath, RegistryCheckOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reportHasCode(report, test.code) {
				t.Fatalf("expected %s, got %+v", test.code, report.Errors)
			}
		})
	}
}

func TestRegistryV3LoaderRejectsLegacyAndUnknownFields(t *testing.T) {
	root, _, cases := writeRegistryFixture(t)
	fingerprint := mustFingerprint(t, filepath.Join(cases, "ecs", "instance.yaml"))
	legacyPath := filepath.Join(root, "legacy.yaml")
	mustWrite(t, legacyPath, `
version: 1
resources: {}
`)
	if _, err := LoadRegistryFile(legacyPath); err == nil || !strings.Contains(err.Error(), "version must be 3") {
		t.Fatalf("strict loader must reject v1: %v", err)
	}
	if migrated, err := LoadRegistryForInit(legacyPath); err != nil || migrated.Version != 1 {
		t.Fatalf("init loader must accept v1: version=%v err=%v", migrated, err)
	}

	unknownPath := filepath.Join(root, "unknown.yaml")
	mustWrite(t, unknownPath, fmt.Sprintf(`
version: 3
resources:
  ecs:
    instance:
      operations:
        create:
          status: offline
          case: %s
          fingerprint: %s
          time: "2026-07-15T12:00:00+08:00"
          reason: not-run
          steps: [create]
`, filepath.Join(cases, "ecs", "instance.yaml"), fingerprint))
	if _, err := LoadRegistryFile(unknownPath); err == nil || !strings.Contains(err.Error(), "field steps not found") {
		t.Fatalf("strict loader must reject unknown fields: %v", err)
	}

	partialSummaryPath := filepath.Join(root, "partial-summary.yaml")
	mustWrite(t, partialSummaryPath, `
version: 3
summary:
  surface: public
resources: {}
`)
	if _, err := LoadRegistryFile(partialSummaryPath); err == nil || !strings.Contains(err.Error(), "summary requires field") {
		t.Fatalf("strict loader must reject incomplete summary: %v", err)
	}

	validPath := filepath.Join(root, "valid.yaml")
	writeRegistry(t, validPath, map[string]RegistryOperation{})
	validData, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, validPath, string(validData)+"---\nversion: 3\nresources: {}\n")
	if _, err := LoadRegistryFile(validPath); err == nil || !strings.Contains(err.Error(), "exactly one YAML document") {
		t.Fatalf("strict loader must reject trailing YAML documents: %v", err)
	}
}

func TestWriteRegistryFileRejectsMigrationIntermediate(t *testing.T) {
	registry := &Registry{
		Version: 2,
		Resources: map[string]RegistryProduct{
			"ecs": {"instance": {Operations: map[string]RegistryOperation{}}},
		},
	}
	err := WriteRegistryFile(filepath.Join(t.TempDir(), "coverage.yaml"), registry)
	if err == nil || !strings.Contains(err.Error(), "version must be 3") {
		t.Fatalf("write must reject a migrated registry before init produces v3: %v", err)
	}
}

func TestInitRegistryCreatesCaseBackedEntriesOnlyAndIsIdempotent(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	mustMkdir(t, filepath.Join(specs, "vpc"))
	mustWrite(t, filepath.Join(specs, "vpc", "vpc.yaml"), `
product: vpc
resource: vpc
operations:
  create: {}
`)

	first, err := initRegistry(specs, cases, nil, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != RegistryVersion || len(first.Resources) != 1 {
		t.Fatalf("unexpected registry shape: %+v", first)
	}
	wantSummary := RegistryPublicSummary{Surface: "public", Resources: 1, Operations: 3, MissingCases: 1, NotPassed: 2}
	if first.Summary != wantSummary {
		t.Fatalf("public summary = %+v, want %+v", first.Summary, wantSummary)
	}
	operations := first.Resources["ecs"]["instance"].Operations
	if len(operations) != 2 {
		t.Fatalf("expected only create/delete case operations, got %+v", operations)
	}
	for name, operation := range operations {
		if operation.Status != StatusOffline || operation.Reason != ReasonNotRun || operation.Time != parseTime.Format(time.RFC3339Nano) {
			t.Fatalf("%s = %+v", name, operation)
		}
		if operation.Fingerprint != mustFingerprint(t, operation.Case) {
			t.Fatalf("%s fingerprint mismatch: %+v", name, operation)
		}
	}

	second, err := initRegistry(specs, cases, first, fixturePublicSurface(), changeTime)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("unchanged init must preserve the registry:\nfirst=%+v\nsecond=%+v", first, second)
	}

	firstPath := filepath.Join(root, "first.yaml")
	secondPath := filepath.Join(root, "second.yaml")
	if err := WriteRegistryFile(firstPath, first); err != nil {
		t.Fatal(err)
	}
	if err := WriteRegistryFile(secondPath, second); err != nil {
		t.Fatal(err)
	}
	firstData, _ := os.ReadFile(firstPath)
	secondData, _ := os.ReadFile(secondPath)
	if string(firstData) != string(secondData) {
		t.Fatalf("unchanged output must be byte-identical")
	}

	var raw map[string]any
	if err := yaml.Unmarshal(firstData, &raw); err != nil {
		t.Fatal(err)
	}
	operationMap := raw["resources"].(map[string]any)["ecs"].(map[string]any)["instance"].(map[string]any)["operations"].(map[string]any)["create"].(map[string]any)
	if len(operationMap) != 5 {
		t.Fatalf("operation must have exactly five fields: %#v", operationMap)
	}
	if _, exists := raw["resources"].(map[string]any)["ecs/instance"]; exists {
		t.Fatalf("registry must not use flat product/resource keys: %#v", raw["resources"])
	}
	if _, exists := raw["resources"].(map[string]any)["ecs"].(map[string]any)["vm"]; exists {
		t.Fatalf("registry must use the canonical spec resource, not its alias: %#v", raw["resources"])
	}
}

func TestInitRegistryMigratesV2FlatKeysAndPreservesLiveEvidence(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	casePath := filepath.Join(cases, "ecs", "instance.yaml")
	legacyPath := filepath.Join(root, "coverage-v2.yaml")
	mustWrite(t, legacyPath, fmt.Sprintf(`
version: 2
generated:
  cases_dir: %s
summary:
  surface: public
  resources: 1
  operations: 3
  missing_cases: 1
  passed: 1
  not_passed: 1
resources:
  ecs/instance:
    operations:
      create:
        status: live-pass
        case: %s
        fingerprint: %s
        time: "2026-07-15T12:00:00+08:00"
        reason: live-verified
`, cases, casePath, mustFingerprint(t, casePath)))

	existing, err := LoadRegistryForInit(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if existing.Version != 2 {
		t.Fatalf("loaded version = %d, want 2", existing.Version)
	}
	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	trailingPath := filepath.Join(root, "coverage-v2-trailing.yaml")
	mustWrite(t, trailingPath, string(legacyData)+"---\nversion: 2\nresources: {}\n")
	if _, err := LoadRegistryForInit(trailingPath); err == nil || !strings.Contains(err.Error(), "exactly one YAML document") {
		t.Fatalf("v2 migration must reject trailing YAML documents: %v", err)
	}
	migrated, err := initRegistry(specs, cases, existing, fixturePublicSurface(), changeTime)
	if err != nil {
		t.Fatal(err)
	}
	create := migrated.Resources["ecs"]["instance"].Operations["create"]
	want := RegistryOperation{
		Status:      StatusLivePass,
		Case:        normalizeCasePath(casePath),
		Fingerprint: mustFingerprint(t, casePath),
		Time:        "2026-07-15T12:00:00+08:00",
		Reason:      ReasonLiveVerified,
	}
	if migrated.Version != RegistryVersion || create != want {
		t.Fatalf("v2 live evidence was not preserved: version=%d create=%+v want=%+v", migrated.Version, create, want)
	}
	create.Status = StatusOffline
	create.Reason = "invalid-reason"
	existing.Resources["ecs"]["instance"].Operations["create"] = create
	if _, err := initRegistry(specs, cases, existing, fixturePublicSurface(), changeTime); err == nil || !strings.Contains(err.Error(), "invalid_reason") {
		t.Fatalf("init must reject invalid preserved v2 operations: %v", err)
	}
}

func TestInitRegistryResetsStatusWhenCaseContentChanges(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	registry, err := initRegistry(specs, cases, nil, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	create := registry.Resources["ecs"]["instance"].Operations["create"]
	create.Status = StatusLivePass
	create.Reason = ReasonLiveVerified
	registry.Resources["ecs"]["instance"].Operations["create"] = create
	oldFingerprint := create.Fingerprint

	casePath := filepath.Join(cases, "ecs", "instance.yaml")
	data, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, casePath, string(data)+"\n# changed\n")
	refreshed, err := initRegistry(specs, cases, registry, fixturePublicSurface(), changeTime)
	if err != nil {
		t.Fatal(err)
	}
	for name, operation := range refreshed.Resources["ecs"]["instance"].Operations {
		if operation.Status != StatusOffline || operation.Reason != ReasonCaseChanged || operation.Time != changeTime.Format(time.RFC3339Nano) {
			t.Fatalf("%s must reset after content change: %+v", name, operation)
		}
		if operation.Fingerprint == oldFingerprint {
			t.Fatalf("%s fingerprint did not change", name)
		}
	}
}

func TestInitRegistryResetsStatusWhenCasePathChanges(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	registry, err := initRegistry(specs, cases, nil, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	create := registry.Resources["ecs"]["instance"].Operations["create"]
	create.Status = StatusLivePass
	create.Reason = ReasonLiveVerified
	create.Case = filepath.Join(cases, "ecs", "old-name.yaml")
	registry.Resources["ecs"]["instance"].Operations["create"] = create

	refreshed, err := initRegistry(specs, cases, registry, fixturePublicSurface(), changeTime)
	if err != nil {
		t.Fatal(err)
	}
	got := refreshed.Resources["ecs"]["instance"].Operations["create"]
	if got.Status != StatusOffline || got.Reason != ReasonCaseChanged || got.Time != changeTime.Format(time.RFC3339Nano) {
		t.Fatalf("path change must reset status: %+v", got)
	}
}

func TestInitRegistryRemovesMissingCasesAndEmptyResources(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	registry, err := initRegistry(specs, cases, nil, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(cases, "ecs", "instance.yaml")); err != nil {
		t.Fatal(err)
	}
	refreshed, err := initRegistry(specs, cases, registry, fixturePublicSurface(), changeTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Resources) != 0 {
		t.Fatalf("empty resources must be omitted: %+v", refreshed.Resources)
	}
}

func TestInitRegistryMigratesV1AndTrustsLocalLivePass(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	mustWrite(t, filepath.Join(specs, "ecs", "instance.yaml"), `
product: ecs
resource: instance
aliases: [vm]
operations:
  create: {}
  delete: {}
  get: {}
  list: {}
  update: {}
`)
	legacyPath := filepath.Join(root, "coverage-v1.yaml")
	mustWrite(t, legacyPath, fmt.Sprintf(`
version: 1
generated:
  cases_dir: %s
resources:
  ecs/instance:
    operations:
      create:
        status: live-pass
        case: %s
        steps: [create]
        evidence:
          report: reports/live.json
          run_id: run-1
          verified_at: "2026-07-14T16:30:43+08:00"
      delete:
        status: offline-valid
        case: %s
        steps: [delete]
        evidence:
          coverage_source: command
      list:
        status: manual-only
        reason: requires-existing-cluster
        review_after: "2026-10-06"
      get:
        status: planned
        owner: e2e
        target_phase: 9
      update:
        status: quarantined
        reason: known-ecctl-bug
        issue: local
        last_checked: "2026-07-15"
`, cases, filepath.Join(cases, "ecs", "instance.yaml"), filepath.Join(cases, "ecs", "instance.yaml")))
	legacy, err := LoadRegistryForInit(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := initRegistry(specs, cases, legacy, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	operations := migrated.Resources["ecs"]["instance"].Operations
	if len(operations) != 2 {
		t.Fatalf("manual-only, planned, and quarantined operations without cases must be omitted: %+v", operations)
	}
	create := operations["create"]
	if create.Status != StatusLivePass || create.Reason != ReasonLiveVerified || create.Time != "2026-07-14T16:30:43+08:00" {
		t.Fatalf("local v1 live-pass must be trusted: %+v", create)
	}
	if create.Fingerprint != mustFingerprint(t, create.Case) {
		t.Fatalf("migrated live fingerprint mismatch: %+v", create)
	}
	deleted := operations["delete"]
	if deleted.Status != StatusOffline || deleted.Reason != ReasonNotRun || deleted.Time != parseTime.Format(time.RFC3339Nano) {
		t.Fatalf("offline-valid migration = %+v", deleted)
	}
}

func TestInitRegistryPreservesLiveAcrossEquivalentCasesDirSpelling(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	existing := &Registry{
		Version:   1,
		Generated: RegistryGenerated{CasesDir: "cases"},
		Resources: map[string]RegistryProduct{
			"ecs": {
				"instance": {Operations: map[string]RegistryOperation{
					"create": {Status: StatusLivePass, Case: "cases/ecs/instance.yaml", Time: "2026-07-01T00:00:00Z"},
				}},
			},
		},
	}
	migrated, err := initRegistry(specs, cases, existing, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	got := migrated.Resources["ecs"]["instance"].Operations["create"]
	if got.Status != StatusLivePass || got.Time != "2026-07-01T00:00:00Z" {
		t.Fatalf("equivalent path spelling must preserve live-pass: %+v", got)
	}
}

func TestInitRegistryPrefersMatchingCaseOverHelperSteps(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	mustWrite(t, filepath.Join(cases, "ecs", "aaa-helper.yaml"), `
resource: ecs/disk
steps:
  - name: helper create instance
    run: ecctl ecs instance create --name helper
`)
	registry, err := initRegistry(specs, cases, nil, fixturePublicSurface(), parseTime)
	if err != nil {
		t.Fatal(err)
	}
	create := registry.Resources["ecs"]["instance"].Operations["create"]
	want := normalizeCasePath(filepath.Join(cases, "ecs", "instance.yaml"))
	if create.Case != want {
		t.Fatalf("expected matching resource case %q, got %+v", want, create)
	}
}

func TestRegistrySummaryJSON(t *testing.T) {
	registry := &Registry{
		Version: RegistryVersion,
		Resources: map[string]RegistryProduct{
			"ecs": {
				"instance": {Operations: map[string]RegistryOperation{
					"create": {Status: StatusOffline},
					"delete": {Status: StatusLivePass},
				}},
			},
		},
	}
	summary := SummarizeRegistry(registry)
	if summary.Entries != 2 || summary.ByStatus[StatusOffline] != 1 || summary.ByStatus[StatusLivePass] != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"entries":2`) {
		t.Fatalf("summary JSON missing entries: %s", raw)
	}
}

func writeRegistryFixture(t *testing.T) (root, specs, cases string) {
	t.Helper()
	root = t.TempDir()
	specs = filepath.Join(root, "specs")
	cases = filepath.Join(root, "cases")
	mustMkdir(t, filepath.Join(specs, "ecs"))
	mustMkdir(t, filepath.Join(cases, "ecs"))
	mustWrite(t, filepath.Join(specs, "ecs", "instance.yaml"), `
product: ecs
resource: instance
aliases: [vm]
operations:
  create: {}
  delete: {}
  list: {}
`)
	mustWrite(t, filepath.Join(cases, "ecs", "instance.yaml"), `
resource: ecs/instance
steps:
  - name: create
    run: ecctl ecs instance create --name x
  - name: delete
    run: ecctl ecs instance delete i-1
`)
	return root, specs, cases
}

func validRegistryOperation(t *testing.T, cases, status, reason string) RegistryOperation {
	t.Helper()
	casePath := filepath.Join(cases, "ecs", "instance.yaml")
	return RegistryOperation{
		Status:      status,
		Case:        normalizeCasePath(casePath),
		Fingerprint: mustFingerprint(t, casePath),
		Time:        parseTime.Format(time.RFC3339Nano),
		Reason:      reason,
	}
}

func withStatus(operation RegistryOperation, status, reason string) RegistryOperation {
	operation.Status = status
	operation.Reason = reason
	return operation
}

func writeRegistry(t *testing.T, path string, operations map[string]RegistryOperation) {
	t.Helper()
	registry := &Registry{
		Version: RegistryVersion,
		Resources: map[string]RegistryProduct{
			"ecs": {"instance": {Operations: operations}},
		},
	}
	summary, err := SummarizePublicSurface(registry, fixturePublicSurface())
	if err != nil {
		summary = RegistryPublicSummary{Surface: "public", Resources: 1, Operations: 3, MissingCases: 3}
	}
	registry.Summary = summary
	if err := WriteRegistryFile(path, registry); err != nil {
		t.Fatal(err)
	}
}

func fixturePublicSurface() PublicSurface {
	return PublicSurface{
		ResourceCount: 1,
		Capabilities: map[Capability]bool{
			{Resource: "ecs/instance", Verb: "create"}: true,
			{Resource: "ecs/instance", Verb: "delete"}: true,
			{Resource: "ecs/instance", Verb: "list"}:   true,
		},
	}
}

func mustFingerprint(t *testing.T, path string) string {
	t.Helper()
	fingerprint, err := caseFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func reportHasCode(report *RegistryCheckReport, code string) bool {
	for _, validationError := range report.Errors {
		if validationError.Code == code {
			return true
		}
	}
	return false
}
