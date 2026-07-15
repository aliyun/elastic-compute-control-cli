package coverage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCheckAcceptsValidRegistry(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWrite(t, registry, `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`)

	rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || len(rep.Errors) != 0 {
		t.Fatalf("expected valid registry, got %+v", rep)
	}
	if rep.Declared != 3 || rep.Entries != 3 {
		t.Fatalf("unexpected counts: %+v", rep)
	}
	if rep.ByStatus["offline-valid"] != 2 || rep.ByStatus["missing"] != 1 {
		t.Fatalf("unexpected status summary: %+v", rep.ByStatus)
	}
}

func TestRegistryCheckFiltersCapabilitiesAndRequiresLivePass(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWrite(t, registry, `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: live-pass
        case: cases/ecs/instance.yaml
        steps: [create]
        evidence: {report: reports/e2e-report.json, run_id: run-1, verified_at: 2026-07-10}
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`)
	rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{
		CapabilityFilter: map[Capability]bool{{Resource: "ecs/instance", Verb: "create"}: true},
		FailOnNotLive:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Declared != 1 || rep.Entries != 1 || rep.Invalid != 0 || rep.ByStatus[StatusLivePass] != 1 {
		t.Fatalf("filtered report = %+v", rep)
	}

	rep, err = CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{
		CapabilityFilter: map[Capability]bool{{Resource: "ecs/instance", Verb: "delete"}: true},
		FailOnNotLive:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(rep, "not_live") {
		t.Fatalf("expected not_live for offline operation, got %+v", rep.Errors)
	}
}

func TestRegistryCheckRejectsInvalidRegistries(t *testing.T) {
	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "unknown status",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: maybe
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`,
			code: "unknown_status",
		},
		{
			name: "missing declared operation",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
`,
			code: "missing_entry",
		},
		{
			name: "stale non retired operation",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
      reboot:
        status: missing
`,
			code: "stale_entry",
		},
		{
			name: "manual only missing reason",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: manual-only
        review_after: 2026-08-03
`,
			code: "missing_reason",
		},
		{
			name: "manual only invalid date",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: manual-only
        reason: cost
        review_after: soon
`,
			code: "invalid_date",
		},
		{
			name: "offline valid without case",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`,
			code: "missing_case",
		},
		{
			name: "offline valid uncovered",
			body: `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [list]
`,
			code: "not_covered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, specs, cases := writeRegistryFixture(t)
			registry := filepath.Join(root, "coverage.yaml")
			mustWrite(t, registry, tt.body)
			rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reportHasCode(rep, tt.code) {
				t.Fatalf("expected error code %q, got %+v", tt.code, rep.Errors)
			}
		})
	}
}

func TestRegistryCheckAllowsStaleRetiredOperation(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWrite(t, registry, `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
      reboot:
        status: retired
        removed_after: 2026-07-03
`)

	rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 {
		t.Fatalf("expected retired stale entry to be allowed, got %+v", rep.Errors)
	}
}

func TestRegistryCheckCanFailOnMissingAndFilterResources(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWrite(t, registry, `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [create]
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`)

	rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{FailOnMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reportHasCode(rep, "missing_status") {
		t.Fatalf("expected fail-on-missing error, got %+v", rep.Errors)
	}

	rep, err = CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{
		FailOnMissing: true,
		ResourceFilter: map[string]bool{
			"ecs/other": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 || rep.Entries != 0 || rep.Declared != 0 {
		t.Fatalf("resource filter should exclude registry entries, got %+v", rep)
	}
}

func TestRegistryCheckDoesNotReadLiveEvidenceReport(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	registry := filepath.Join(root, "coverage.yaml")
	mustWrite(t, registry, `
version: 1
resources:
  ecs/instance:
    operations:
      create:
        status: live-pass
        case: cases/ecs/instance.yaml
        steps: [create]
        evidence:
          report: reports/local-only/e2e-report.json
          run_id: run-1
          verified_at: "2026-07-06T17:00:00+08:00"
      delete:
        status: offline-valid
        case: cases/ecs/instance.yaml
        steps: [delete]
      list:
        status: missing
`)

	rep, err := CheckRegistryFile(specs, cases, registry, RegistryCheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid != 0 {
		t.Fatalf("registry check should not require local report files, got %+v", rep.Errors)
	}
}

func TestInitRegistryUsesCoverageAndPreservesManualMetadata(t *testing.T) {
	root, specs, cases := writeRegistryFixture(t)
	existing := &Registry{
		Version: 1,
		Resources: map[string]RegistryResource{
			"ecs/instance": {
				Operations: map[string]RegistryOperation{
					"create": {
						Status:      StatusPlanned,
						Owner:       "e2e",
						TargetPhase: 1,
					},
					"delete": {
						Status: StatusLivePass,
						Case:   filepath.Join(cases, "ecs", "instance.yaml"),
						Steps:  []string{"delete"},
						Evidence: RegistryEvidence{
							Report: "reports/live.json",
						},
					},
					"list": {
						Status:      StatusManualOnly,
						Reason:      "cost",
						ReviewAfter: "2026-08-03",
					},
				},
			},
		},
	}

	reg, err := InitRegistry(specs, cases, existing)
	if err != nil {
		t.Fatal(err)
	}
	ops := reg.Resources["ecs/instance"].Operations
	if ops["create"].Status != StatusOfflineValid {
		t.Fatalf("covered planned operation should become offline-valid: %+v", ops)
	}
	if ops["create"].Case != filepath.Join(cases, "ecs", "instance.yaml") {
		t.Fatalf("unexpected case path: %q", ops["create"].Case)
	}
	if ops["delete"].Status != StatusLivePass || ops["delete"].Case != filepath.Join(cases, "ecs", "instance.yaml") || ops["delete"].Evidence.Report != "reports/live.json" {
		t.Fatalf("live evidence should be preserved while case metadata refreshes: %+v", ops["delete"])
	}
	if ops["list"].Status != StatusManualOnly || ops["list"].Reason != "cost" || ops["list"].ReviewAfter != "2026-08-03" {
		t.Fatalf("manual metadata not preserved: %+v", ops["list"])
	}

	first := filepath.Join(root, "first.yaml")
	second := filepath.Join(root, "second.yaml")
	if err := WriteRegistryFile(first, reg); err != nil {
		t.Fatal(err)
	}
	if err := WriteRegistryFile(second, reg); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("registry output is not deterministic:\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}

func TestInitRegistryDowngradesLiveEvidenceWhenCaseProvenanceChanges(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	existing := &Registry{
		Version: 1,
		Resources: map[string]RegistryResource{
			"ecs/instance": {Operations: map[string]RegistryOperation{
				"delete": {
					Status: StatusLivePass,
					Case:   "cases/ecs/renamed-old.yaml",
					Steps:  []string{"old delete"},
					Evidence: RegistryEvidence{
						Report: "reports/live.json", RunID: "old", VerifiedAt: "2026-07-01T00:00:00Z",
					},
				},
			}},
		},
	}

	reg, err := InitRegistry(specs, cases, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Resources["ecs/instance"].Operations["delete"]
	if got.Status != StatusOfflineValid || got.Case != filepath.Join(cases, "ecs", "instance.yaml") || got.Evidence.CoverageSource != "command" {
		t.Fatalf("renamed live provenance should require fresh evidence: %+v", got)
	}
}

func TestInitRegistryPreservesLiveEvidenceAcrossEquivalentCasesDirSpelling(t *testing.T) {
	_, specs, cases := writeRegistryFixture(t)
	existing := &Registry{
		Version:   1,
		Generated: RegistryGenerated{CasesDir: "cases"},
		Resources: map[string]RegistryResource{
			"ecs/instance": {Operations: map[string]RegistryOperation{
				"delete": {
					Status: StatusLivePass,
					Case:   "cases/ecs/instance.yaml",
					Steps:  []string{"delete"},
					Evidence: RegistryEvidence{
						Report: "reports/live.json", RunID: "run-1", VerifiedAt: "2026-07-01T00:00:00Z",
					},
				},
			}},
		},
	}

	reg, err := InitRegistry(specs, cases, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Resources["ecs/instance"].Operations["delete"]
	if got.Status != StatusLivePass || got.Evidence.RunID != "run-1" {
		t.Fatalf("equivalent cases roots must preserve live evidence: %+v", got)
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

	reg, err := InitRegistry(specs, cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	create := reg.Resources["ecs/instance"].Operations["create"]
	if create.Case != filepath.Join(cases, "ecs", "instance.yaml") {
		t.Fatalf("expected matching case path, got %+v", create)
	}
	if len(create.Steps) != 1 || create.Steps[0] != "create" {
		t.Fatalf("expected only matching case steps, got %+v", create.Steps)
	}
}

func TestRegistrySummaryJSON(t *testing.T) {
	reg := &Registry{
		Version: 1,
		Resources: map[string]RegistryResource{
			"ecs/instance": {
				Operations: map[string]RegistryOperation{
					"create": {Status: StatusOfflineValid},
					"delete": {Status: StatusOfflineValid},
					"list":   {Status: StatusMissing},
				},
			},
		},
	}
	sum := SummarizeRegistry(reg)
	if sum.Entries != 3 || sum.ByStatus[StatusOfflineValid] != 2 || sum.ByStatus[StatusMissing] != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"entries":3`) {
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

func reportHasCode(rep *RegistryCheckReport, code string) bool {
	for _, e := range rep.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}
