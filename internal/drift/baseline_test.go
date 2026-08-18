package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBaselineAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift-baseline.json")

	baseline := Baseline{
		Language: "en",
		Bindings: []BaselineBinding{{
			Product:    "ecs",
			Resource:   "instance",
			Binding:    "create_to_running",
			API:        "RunInstances",
			Parameters: []string{"ZoneId"},
			Covered:    []string{"ZoneId"},
		}},
	}
	if err := WriteBaseline(path, baseline); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	firstInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline after write: %v", err)
	}
	if len(loaded.Bindings) != 1 || loaded.Bindings[0].Parameters[0] != "ZoneId" {
		t.Fatalf("round-tripped baseline = %#v", loaded)
	}
	assertSingleFile(t, dir)

	// Overwrite must replace the file in place, still atomically.
	empty := Baseline{Language: "en"}
	if err := WriteBaseline(path, empty); err != nil {
		t.Fatalf("WriteBaseline overwrite: %v", err)
	}
	secondInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(firstInfo, secondInfo) {
		t.Fatal("baseline overwrite reused the destination file instead of atomically replacing it")
	}
	loaded, err = LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline after overwrite: %v", err)
	}
	if len(loaded.Bindings) != 0 {
		t.Fatalf("overwritten baseline has %d bindings, want 0", len(loaded.Bindings))
	}
	assertSingleFile(t, dir)
}

func TestValidateBaselineRejectsDuplicateBindings(t *testing.T) {
	entry := BaselineBinding{
		Product: "ecs", Resource: "instance", Binding: "create", API: "RunInstances",
	}
	err := validateBaseline(Baseline{
		Language: "en",
		Bindings: []BaselineBinding{entry, entry},
	}, "en")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("validateBaseline error = %v, want duplicate binding error", err)
	}
}

func TestValidateBaselineRejectsLanguageMismatch(t *testing.T) {
	err := validateBaseline(Baseline{Language: "zh-CN"}, "en")
	if err == nil || !strings.Contains(err.Error(), "language") {
		t.Fatalf("validateBaseline error = %v, want language mismatch", err)
	}
}

func assertSingleFile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory contains %d entries, want only the baseline: %v", len(entries), names)
	}
}
