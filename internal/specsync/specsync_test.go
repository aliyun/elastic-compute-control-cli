package specsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func loadTestReport(t *testing.T, path string) drift.Report {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report drift.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestGoldenPatches(t *testing.T) {
	cases := []string{"top-level-string", "nested-object-field", "array-item-field"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join("testdata", name)
			report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
			srun, err := planReport(report, filepath.Join(caseDir, "specs"))
			if err != nil {
				t.Fatal(err)
			}
			if len(srun.flags) != 0 {
				t.Fatalf("planReport flags = %#v, want none", srun.flags)
			}
			rf := srun.files["fake/fakeres"]
			if rf == nil {
				t.Fatalf("resource file for fake/fakeres not loaded")
			}
			got := string(joinLines(rf.lines))
			want, err := os.ReadFile(filepath.Join(caseDir, "patched-fakeres.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("patched spec mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

func TestFlaggedItemLeavesSpecUnchanged(t *testing.T) {
	caseDir := filepath.Join("testdata", "flag-name-to-id")
	report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
	srun, err := planReport(report, filepath.Join(caseDir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srun.plan) != 1 || srun.plan[0].action != "flagged" {
		t.Fatalf("plan = %#v, want exactly one flagged item", srun.plan)
	}
	if srun.plan[0].flagKind != "name_to_id" {
		t.Fatalf("flag kind = %q, want name_to_id", srun.plan[0].flagKind)
	}
	rf := srun.files["fake/fakeres"]
	if rf == nil {
		t.Fatal("resource file for fake/fakeres not loaded")
	}
	got := string(joinLines(rf.lines))
	want, err := os.ReadFile(filepath.Join(caseDir, "specs", "fake", "fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("flagged item must not modify the spec:\n%s", got)
	}
}

func TestRequiredItemIsFlaggedWithoutChangingSpec(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
	report.Items[0].Required = true

	srun, err := planReport(report, filepath.Join(caseDir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srun.plan) != 1 || srun.plan[0].action != "flagged" || srun.plan[0].flagKind != "required_parameter" {
		t.Fatalf("plan = %#v, want one required_parameter flag", srun.plan)
	}
	rf := srun.files["fake/fakeres"]
	if rf == nil {
		t.Fatal("resource file was not loaded")
	}
	if got := string(joinLines(rf.lines)); got != string(rf.original) {
		t.Fatalf("required item changed spec:\n%s", got)
	}
}

func TestCompositeItemIsFlaggedWithoutChangingSpec(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	for _, apiType := range []string{"RepeatList", "Struct"} {
		t.Run(apiType, func(t *testing.T) {
			report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
			report.Items[0].Type = apiType

			srun, err := planReport(report, filepath.Join(caseDir, "specs"))
			if err != nil {
				t.Fatal(err)
			}
			if len(srun.plan) != 1 || srun.plan[0].action != "flagged" || srun.plan[0].flagKind != "unsupported_type" {
				t.Fatalf("plan = %#v, want one unsupported_type flag", srun.plan)
			}
		})
	}
}

func TestUnsafeParameterNameIsFlagged(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
	report.Items[0].Param = "Name: injected"

	srun, err := planReport(report, filepath.Join(caseDir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srun.plan) != 1 || srun.plan[0].action != "flagged" || srun.plan[0].flagKind != "invalid_parameter_name" {
		t.Fatalf("plan = %#v, want invalid_parameter_name", srun.plan)
	}
}

func TestStaleReportAPIMismatchIsFlagged(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
	report.Items[0].API = "CreateDifferentResource"

	srun, err := planReport(report, filepath.Join(caseDir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srun.plan) != 1 || srun.plan[0].action != "flagged" || srun.plan[0].flagKind != "api_mismatch" {
		t.Fatalf("plan = %#v, want api_mismatch", srun.plan)
	}
	rf := srun.files["fake/fakeres"]
	if rf == nil {
		t.Fatal("resource file was not loaded")
	}
	if got := string(joinLines(rf.lines)); got != string(rf.original) {
		t.Fatalf("stale report changed spec:\n%s", got)
	}
}

func TestLoadReportRejectsNonEnglishLanguage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	raw, err := json.Marshal(drift.Report{
		Language:        "zh-CN",
		BindingsChecked: 1,
		Items: []drift.Item{{
			Product: "fake", Resource: "fakeres", Binding: "create_fakeres",
			API: "CreateFakeres", Param: "Name", Kind: drift.KindMissing, Type: "String",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReport(path); err == nil || !strings.Contains(err.Error(), "language") {
		t.Fatalf("loadReport error = %v, want language rejection", err)
	}
}

func TestFindPathDoesNotCrossSiblingBlocks(t *testing.T) {
	doc := newDoc([]string{
		"operations:",
		"  create:",
		"    input:",
		"      controls:",
		"        - timeout",
		"  update:",
		"    input:",
		"      fields:",
		"        - id",
	})
	if index, ok := doc.findPath("operations", "create", "input", "fields"); ok {
		t.Fatalf("findPath crossed into update.fields at line %d", index)
	}
}

func TestNormalizedFieldCollisionFlagsWholeBatch(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	report := loadTestReport(t, filepath.Join(caseDir, "drift.json"))
	first := report.Items[0]
	first.Param = "FooBar"
	second := first
	second.Param = "Foo_Bar"
	report.Items = []drift.Item{first, second}

	srun, err := planReport(report, filepath.Join(caseDir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srun.plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(srun.plan))
	}
	for _, item := range srun.plan {
		if item.action != "flagged" || item.flagKind != "normalized_name_collision" {
			t.Fatalf("plan item = %#v, want normalized_name_collision", item)
		}
	}
	rf := srun.files["fake/fakeres"]
	if rf == nil {
		t.Fatal("resource file was not loaded")
	}
	if got := string(joinLines(rf.lines)); got != string(rf.original) {
		t.Fatalf("collision batch changed spec:\n%s", got)
	}
}

func TestCommitFileUpdatesRollsBackEarlierFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte("first-old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second-old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	renames := 0
	err := commitFileUpdatesWithRename([]fileUpdate{
		{path: first, data: []byte("first-new\n"), mode: 0o644},
		{path: second, data: []byte("second-new\n"), mode: 0o644},
	}, func(oldPath, newPath string) error {
		renames++
		if renames == 2 {
			return errors.New("injected rename failure")
		}
		return os.Rename(oldPath, newPath)
	})
	if err == nil || !strings.Contains(err.Error(), "injected rename failure") {
		t.Fatalf("commit error = %v, want injected failure", err)
	}
	for path, want := range map[string]string{first: "first-old\n", second: "second-old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want rollback %q", path, got, want)
		}
	}
}

func TestRunRejectsPlanPathThatOverlapsPatchedSpec(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	dir := t.TempDir()
	copySpecTree(t, caseDir, dir)
	specPath := filepath.Join(dir, "specs", "fake", "fakeres.yaml")
	original, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Run(filepath.Join(dir, "drift.json"), filepath.Join(dir, "specs"), false, specPath)
	if err == nil || !strings.Contains(err.Error(), "duplicate output path") {
		t.Fatalf("Run error = %v, want duplicate output path", err)
	}
	got, readErr := os.ReadFile(specPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("overlapping plan path changed spec:\n%s", got)
	}
}

func TestRunRejectsAbsolutePlanAliasOfRelativeSpec(t *testing.T) {
	caseDir := filepath.Join("testdata", "top-level-string")
	dir := t.TempDir()
	copySpecTree(t, caseDir, dir)
	absoluteSpec := filepath.Join(dir, "specs", "fake", "fakeres.yaml")
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeSpecs, err := filepath.Rel(workingDir, filepath.Join(dir, "specs"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(absoluteSpec)
	if err != nil {
		t.Fatal(err)
	}

	err = Run(filepath.Join(dir, "drift.json"), relativeSpecs, false, absoluteSpec)
	if err == nil || !strings.Contains(err.Error(), "duplicate output path") {
		t.Fatalf("Run error = %v, want duplicate output path", err)
	}
	got, readErr := os.ReadFile(absoluteSpec)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("absolute plan alias changed spec:\n%s", got)
	}
}

func TestCommitFileUpdatesRejectsFilesystemAliases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hardlink := filepath.Join(dir, "hardlink.yaml")
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := commitFileUpdates([]fileUpdate{
		{path: target, data: []byte("target\n"), mode: 0o644},
		{path: hardlink, data: []byte("hardlink\n"), mode: 0o644},
	}); err == nil || !strings.Contains(err.Error(), "duplicate output path") {
		t.Fatalf("hardlink alias error = %v, want duplicate output path", err)
	}

	symlink := filepath.Join(dir, "symlink.yaml")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := commitFileUpdates([]fileUpdate{{path: symlink, data: []byte("bad\n"), mode: 0o644}}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink output error = %v, want symbolic link rejection", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Fatalf("alias checks changed target: %q", got)
	}
}

func TestRunModeContractDryRunAndWrite(t *testing.T) {
	// Contract through Run(): -dry-run must leave every spec file unchanged,
	// -write must produce the golden output, and -plan-out must serialize the
	// structured plan attached by the report-only api-sync monitor.
	caseDir := filepath.Join("testdata", "top-level-string")
	want, err := os.ReadFile(filepath.Join(caseDir, "patched-fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(filepath.Join(caseDir, "specs", "fake", "fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	dry := t.TempDir()
	copySpecTree(t, caseDir, dry)
	if err := Run(filepath.Join(dry, "drift.json"), filepath.Join(dry, "specs"), true, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dry, "specs", "fake", "fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(orig) {
		t.Fatalf("dry run modified the spec:\n%s", got)
	}

	outDir := t.TempDir()
	copySpecTree(t, caseDir, outDir)
	if err := Run(filepath.Join(outDir, "drift.json"), filepath.Join(outDir, "specs"), false, ""); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(outDir, "specs", "fake", "fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("write output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	planDir := t.TempDir()
	copySpecTree(t, caseDir, planDir)
	planPath := filepath.Join(planDir, "drift-plan.json")
	if err := Run(filepath.Join(planDir, "drift.json"), filepath.Join(planDir, "specs"), true, planPath); err != nil {
		t.Fatal(err)
	}
	var plan planFile
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != "patched" || !plan.Items[0].TopLevel {
		t.Fatalf("unexpected plan: %+v", plan.Items)
	}
}

func copySpecTree(t *testing.T, caseDir, dst string) {
	t.Helper()
	src := filepath.Join(caseDir, "specs")
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, "specs", rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(caseDir, "drift.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "drift.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
