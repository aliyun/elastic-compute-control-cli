package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeGap(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs", "ecs")
	cases := filepath.Join(root, "cases", "ecs")
	mustMkdir(t, specs)
	mustMkdir(t, cases)

	mustWrite(t, filepath.Join(specs, "instance.yaml"), `
product: ecs
resource: instance
operations:
  create: {}
  delete: {}
  list: {}
`)
	mustWrite(t, filepath.Join(cases, "instance.yaml"), `
resource: ecs/instance
steps:
  - name: c
    run: ecctl ecs instance create --name x
  - name: d
    run: ecctl ecs instance delete i-1
`)

	rep, err := Analyze(filepath.Join(root, "specs"), filepath.Join(root, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Declared != 3 || rep.Covered != 2 || len(rep.Gaps) != 1 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if rep.DeclaredResources != 1 || rep.CoveredResources != 1 || len(rep.ResourceGaps) != 0 {
		t.Fatalf("unexpected resource coverage: %+v", rep)
	}
	if rep.Gaps[0].Verb != "list" {
		t.Fatalf("expected list gap, got %+v", rep.Gaps)
	}
}

func TestAnalyzeReportsResourcesWithNoCase(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs", "ecs")
	cases := filepath.Join(root, "cases", "ecs")
	mustMkdir(t, specs)
	mustMkdir(t, cases)

	mustWrite(t, filepath.Join(specs, "instance.yaml"), `
product: ecs
resource: instance
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(specs, "disk.yaml"), `
product: ecs
resource: disk
operations:
  get: {}
  list: {}
`)
	mustWrite(t, filepath.Join(cases, "instance.yaml"), `
resource: ecs/instance
steps:
  - name: list
    run: ecctl ecs instance list
`)

	rep, err := Analyze(filepath.Join(root, "specs"), filepath.Join(root, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.DeclaredResources != 2 || rep.CoveredResources != 1 {
		t.Fatalf("resource counts = %d/%d, want 1/2", rep.CoveredResources, rep.DeclaredResources)
	}
	if len(rep.ResourceGaps) != 1 || rep.ResourceGaps[0] != "ecs/disk" {
		t.Fatalf("resource gaps = %#v, want [ecs/disk]", rep.ResourceGaps)
	}
}

func TestAnalyzeDoesNotCountForeignSetupStepAsResourceCase(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs", "ecs")
	cases := filepath.Join(root, "cases", "ecs")
	mustMkdir(t, specs)
	mustMkdir(t, cases)

	mustWrite(t, filepath.Join(specs, "instance.yaml"), `
product: ecs
resource: instance
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(specs, "disk.yaml"), `
product: ecs
resource: disk
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(cases, "instance.yaml"), `
resource: ecs/instance
steps:
  - name: list disks as setup
    run: ecctl ecs disk list
  - name: list instances
    run: ecctl ecs instance list
`)

	rep, err := Analyze(filepath.Join(root, "specs"), filepath.Join(root, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Covered != 2 {
		t.Fatalf("operation coverage = %d, want 2", rep.Covered)
	}
	if rep.CoveredResources != 1 {
		t.Fatalf("resource coverage = %d, want 1", rep.CoveredResources)
	}
	if len(rep.ResourceGaps) != 1 || rep.ResourceGaps[0] != "ecs/disk" {
		t.Fatalf("resource gaps = %#v, want [ecs/disk]", rep.ResourceGaps)
	}
}

func TestAnalyzeCountsExplicitSecondaryLifecycleCoverage(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs", "ack")
	cases := filepath.Join(root, "cases", "ack")
	mustMkdir(t, specs)
	mustMkdir(t, cases)

	mustWrite(t, filepath.Join(specs, "diagnosis.yaml"), `
product: ack
resource: diagnosis
operations:
  create: {}
`)
	mustWrite(t, filepath.Join(specs, "check-item.yaml"), `
product: ack
resource: check-item
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(cases, "diagnosis.yaml"), `
resource: ack/diagnosis
covers: [ack/check-item]
steps:
  - name: create
    run: ecctl ack diagnosis create --cluster c --type node
  - name: list check items
    run: ecctl ack diagnosis check-item list d-1 --cluster c
`)

	rep, err := Analyze(filepath.Join(root, "specs"), filepath.Join(root, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.CoveredResources != 2 || len(rep.ResourceGaps) != 0 {
		t.Fatalf("explicit secondary coverage = %+v", rep)
	}
}

func TestAnalyzeMapsNestedParentResourceCommands(t *testing.T) {
	root := t.TempDir()
	rgSpecs := filepath.Join(root, "specs", "rg")
	ackSpecs := filepath.Join(root, "specs", "ack")
	rgCases := filepath.Join(root, "cases", "rg")
	ackCases := filepath.Join(root, "cases", "ack")
	mustMkdir(t, rgSpecs)
	mustMkdir(t, ackSpecs)
	mustMkdir(t, rgCases)
	mustMkdir(t, ackCases)

	mustWrite(t, filepath.Join(rgSpecs, "policy-version.yaml"), `
product: rg
resource: version
operations:
  create: {}
`)
	mustWrite(t, filepath.Join(ackSpecs, "diagnosis-check-item.yaml"), `
product: ack
resource: check-item
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(rgCases, "policy-version.yaml"), `
resource: rg/version
steps:
  - name: create version
    run: ecctl rg policy version create --policy-name p --policy-document '{}'
`)
	mustWrite(t, filepath.Join(ackCases, "check-item.yaml"), `
resource: ack/check-item
steps:
  - name: list check items
    run: ecctl ack diagnosis check-item list --cluster c --type node
`)

	rep, err := Analyze(filepath.Join(root, "specs"), filepath.Join(root, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Declared != 2 || rep.Covered != 2 || len(rep.Gaps) != 0 {
		t.Fatalf("expected nested commands to cover both capabilities, got %+v", rep)
	}
}

func TestAnalyzeForCapabilitiesIgnoresHiddenResources(t *testing.T) {
	root := t.TempDir()
	specs := filepath.Join(root, "specs", "ack")
	cases := filepath.Join(root, "cases", "ack")
	mustMkdir(t, specs)
	mustMkdir(t, cases)
	mustWrite(t, filepath.Join(specs, "kubeconfig.yaml"), `
product: ack
resource: kubeconfig
operations:
  list: {}
`)
	mustWrite(t, filepath.Join(specs, "operation-plan.yaml"), `
product: ack
resource: operation-plan
operations:
  list: {}
  get: {}
  cancel: {}
`)
	mustWrite(t, filepath.Join(cases, "kubeconfig.yaml"), `
resource: ack/kubeconfig
steps:
  - name: list
    run: ecctl ack kubeconfig list
`)

	filter := map[Capability]bool{
		{Resource: "ack/kubeconfig", Verb: "list"}: true,
	}
	rep, err := AnalyzeForCapabilities(filepath.Join(root, "specs"), filepath.Join(root, "cases"), filter)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Declared != 1 || rep.Covered != 1 || len(rep.Gaps) != 0 || rep.DeclaredResources != 1 {
		t.Fatalf("filtered public coverage = %+v", rep)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
