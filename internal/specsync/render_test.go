package specsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func TestRenderMarkdownIsBoundedAndEscapesCells(t *testing.T) {
	dir := t.TempDir()
	driftPath := filepath.Join(dir, "drift.json")
	planPath := filepath.Join(dir, "drift-plan.json")
	report := drift.Report{
		Language:        "en",
		BindingsChecked: 2,
		BaselineGaps:    1,
		Items: []drift.Item{
			{Product: "fake", Resource: "one", Binding: "create", API: "CreateOne", Param: "Name|Alias", Kind: drift.KindMissing, Type: "String"},
			{Product: "fake", Resource: "two", Binding: "update", API: "UpdateTwo", Param: "OldName", Kind: drift.KindRemoved},
		},
		Skipped: []drift.Skipped{{Product: "fake", Resource: "three", Binding: "list", API: "ListThree", Reason: "missing\nmetadata"}},
	}
	writeJSONFixture(t, driftPath, report)
	writeJSONFixture(t, planPath, planFile{Items: []planItemJSON{
		{Product: "fake", Resource: "one", Binding: "create", API: "CreateOne", Param: "Name|Alias", Kind: drift.KindMissing, Action: "flagged", FlagKind: "invalid_parameter_name"},
		{Product: "fake", Resource: "two", Binding: "update", API: "UpdateTwo", Param: "OldName", Kind: drift.KindRemoved, Action: "flagged", FlagKind: "removed"},
	}})

	raw, err := RenderMarkdown(driftPath, planPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		DriftIssueMarker,
		"drift: missing=1, removed=1, uncovered=0, skipped=1",
		"sync plan: patched=0, flagged=2, already-synced=0",
		"Name\\|Alias",
		"Showing 1 of 3 rows",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "OldName") || strings.Contains(text, "missing metadata") {
		t.Fatalf("render limit leaked rows after the first:\n%s", text)
	}
}

func TestRenderMarkdownRejectsUnsafeLimit(t *testing.T) {
	if _, err := RenderMarkdown("unused", "", 0); err == nil {
		t.Fatal("RenderMarkdown accepted zero limit")
	}
	if _, err := RenderMarkdown("unused", "", 201); err == nil {
		t.Fatal("RenderMarkdown accepted limit above 200")
	}
}

func TestRenderMarkdownAcceptsBaselineGapOnlyReport(t *testing.T) {
	driftPath := filepath.Join(t.TempDir(), "drift.json")
	writeJSONFixture(t, driftPath, drift.Report{Language: "en", BaselineGaps: 2})
	raw, err := RenderMarkdown(driftPath, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "baseline gaps: 2") {
		t.Fatalf("baseline gap summary missing:\n%s", raw)
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
