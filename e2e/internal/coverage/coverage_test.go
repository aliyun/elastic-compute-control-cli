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
	if rep.Gaps[0].Verb != "list" {
		t.Fatalf("expected list gap, got %+v", rep.Gaps)
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
