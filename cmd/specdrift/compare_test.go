package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func TestCompareCommandUsesFixedPreparedSources(t *testing.T) {
	specDir := t.TempDir()
	raw, err := os.ReadFile("../../specs/ecs/disk.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "disk.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	source := "../../internal/openapimeta"
	code, out, diagnostic := captureCommand(t, "compare", "-base-dir", source, "-candidate-dir", source, "-spec-dir", specDir, "-format", "json")
	if code != 0 || diagnostic != "" {
		t.Fatalf("compare identical fixed sources = %d, %q", code, diagnostic)
	}
	var report drift.MetadataDiffReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.BaseRevision == "" || report.BaseRevision != report.CandidateRevision || len(report.Changes) != 0 {
		t.Fatalf("invalid unchanged report: %+v", report)
	}
}

func TestCompareCommandRejectsInvalidInputs(t *testing.T) {
	for _, args := range [][]string{
		{"compare", "-format", "yaml"},
		{"compare", "-base-dir", "missing", "-candidate-dir", "missing", "-spec-dir", "missing"},
	} {
		code, _, diagnostic := captureCommand(t, args...)
		if code != 2 || !strings.Contains(diagnostic, "specdrift compare") {
			t.Fatalf("%v = %d, %q", args, code, diagnostic)
		}
	}
}
