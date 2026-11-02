package specsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestMarkdownRenderingRejectsBrokenPlanAndBoundsUnicodeCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if _, err := RenderMarkdown("testdata/top-level-string/drift.json", path, 10); err == nil {
		t.Fatal("missing plan ignored")
	}
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderMarkdown("testdata/top-level-string/drift.json", path, 10); err == nil {
		t.Fatal("malformed plan ignored")
	}
	cell := markdownCell(strings.Repeat("界", 150))
	if utf8.RuneCountInString(cell) != 120 || !strings.HasSuffix(cell, "…") {
		t.Fatalf("unbounded unicode cell: %d", utf8.RuneCountInString(cell))
	}
}

func TestSyncFlagsChangesThatNeedManualModeling(t *testing.T) {
	for _, tt := range []struct {
		item drift.Item
		kind string
	}{
		{drift.Item{Param: "ChargeType", Type: "String"}, "needs_review"},
		{drift.Item{Param: "Mode", Type: "String", Description: "Valid values: safe, strict"}, "enum"},
		{drift.Item{Param: "A", Type: "String"}, ""},
		{drift.Item{Param: "Parent..Name", Type: "String"}, "invalid_parameter_name"},
	} {
		kind, _, flagged := flagForItem(tt.item)
		if kind != tt.kind || flagged != (tt.kind != "") {
			t.Fatalf("manual classification %s = %q, %t", tt.item.Param, kind, flagged)
		}
	}
	report := loadTestReport(t, "testdata/top-level-string/drift.json")
	for _, kind := range []string{drift.KindRemoved, drift.KindUncovered} {
		report.Items[0].Kind = kind
		run, err := planReport(report, "testdata/top-level-string/specs")
		if err != nil || len(run.flags) != 1 || run.flags[0].flagKind != kind || len(run.files) != 0 {
			t.Fatalf("nonmechanical change = %+v, %v", run, err)
		}
	}
	report.Items[0].Kind = drift.KindMissing
	run, err := planReport(report, filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(run.flags) != 1 || run.flags[0].flagKind != "resource_not_found" {
		t.Fatalf("absent resource = %+v, %v", run, err)
	}
}

func TestSyncDetectsIncompletePlannedEdits(t *testing.T) {
	item := drift.Item{Product: "fake", Resource: "thing", Binding: "create", API: "CreateThing", Param: "Name", Kind: drift.KindMissing}
	base := func() *resourceFile {
		return &resourceFile{path: "fixture.yaml", resource: spec.ResourceSpec{
			Bindings:   map[string]spec.Binding{"create": {API: "CreateThing", Request: map[string]any{"Name": "$.name"}}},
			Schema:     spec.ResourceSchema{Fields: map[string]spec.SchemaField{"name": {Type: "string"}}},
			Operations: map[string]spec.Operation{"create": {Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "name"}}}}},
		}}
	}
	for _, fault := range []string{"binding missing", "wrong API", "request missing", "schema missing", "operation missing", "parent missing", "child missing"} {
		t.Run(fault, func(t *testing.T) {
			rf := base()
			candidate := item
			switch fault {
			case "binding missing":
				delete(rf.resource.Bindings, "create")
			case "wrong API":
				rf.resource.Bindings["create"] = spec.Binding{API: "Wrong"}
			case "request missing":
				rf.resource.Bindings["create"] = spec.Binding{API: "CreateThing", Request: map[string]any{}}
			case "schema missing":
				rf.resource.Schema.Fields = nil
			case "operation missing":
				rf.resource.Operations = nil
			case "parent missing", "child missing":
				candidate.Param = "Settings.Name"
				rf.resource.Bindings["create"] = spec.Binding{API: "CreateThing", Request: map[string]any{"Settings.Name": "$.name"}}
				if fault == "child missing" {
					rf.resource.Schema.Fields = map[string]spec.SchemaField{"settings": {Type: "object", Fields: map[string]spec.SchemaField{}}}
				}
			}
			if err := verifyPlannedItem(rf, candidate, []string{"create"}); err == nil {
				t.Fatalf("incomplete %s edit passed verification", fault)
			}
		})
	}
	if err := verifyPlannedItem(base(), item, []string{"create"}); err != nil {
		t.Fatal(err)
	}
	run := &syncRun{files: map[string]*resourceFile{"fake/thing": base()}, collisions: map[string]bool{}}
	item.Binding = "missing"
	if plan, err := run.planItem("unused", item); err != nil || plan.flagKind != "binding_not_found" {
		t.Fatalf("missing binding = %+v, %v", plan, err)
	}
	item.Binding = "create"
	run.files["fake/thing"].resource.Operations = nil
	if plan, err := run.planItem("unused", item); err != nil || plan.flagKind != "binding_unused" {
		t.Fatalf("unused binding = %+v, %v", plan, err)
	}
}

func TestSyncRejectsMalformedReportsAndMissingPlanDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, raw := range []string{"invalid", `{}`} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadReport(path); err == nil {
			t.Fatalf("invalid report accepted: %s", raw)
		}
	}
	if err := Run("testdata/top-level-string/drift.json", "testdata/top-level-string/specs", true, filepath.Join(t.TempDir(), "missing", "plan.json")); err == nil {
		t.Fatal("missing plan directory ignored")
	}
	for _, tt := range []struct{ word, want string }{{"policy", "policies"}, {"key", "keys"}, {"analysis", "analyses"}, {"x", "xs"}, {"disk_policy", "disk_policies"}} {
		if got := pluralizeSnakeWord(tt.word); got != tt.want {
			t.Fatalf("plural of %s = %s, want %s", tt.word, got, tt.want)
		}
	}
}

func TestSyncStagingAndRollbackFailuresPreserveEvidence(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "directory")
	if err := os.WriteFile(first, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := commitFileUpdates([]fileUpdate{{path: first, data: []byte("changed"), mode: 0o600}, {path: second, data: []byte("changed"), mode: 0o600}}); err == nil {
		t.Fatal("directory output accepted")
	}
	raw, err := os.ReadFile(first)
	if err != nil || string(raw) != "original" {
		t.Fatalf("failed stage changed earlier output: %q, %v", raw, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("staging leaked files: %v, %v", entries, err)
	}
	cause := errors.New("rename refused")
	if err := rollbackFileUpdates([]stagedFileUpdate{{fileUpdate: fileUpdate{path: first}, existed: true, original: []byte("original"), originalMode: 0o600}}, func(string, string) error { return cause }); !errors.Is(err, cause) {
		t.Fatalf("rollback cause lost: %v", err)
	}
	if _, _, _, err := canonicalOutputPath(""); err == nil {
		t.Fatal("empty output accepted")
	}
	if _, err := writeSiblingTemp(filepath.Join(dir, "missing", "file"), nil, 0o600); err == nil {
		t.Fatal("missing output directory accepted")
	}
	if err := rollbackFileUpdates([]stagedFileUpdate{{fileUpdate: fileUpdate{path: filepath.Join(dir, "missing", "file")}, existed: true}}, os.Rename); err == nil || !strings.Contains(err.Error(), "temporary") {
		t.Fatalf("rollback staging failure=%v", err)
	}
}
