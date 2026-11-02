package drift

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestBaselineReadWriteFailuresPreserveExistingEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "baseline.json")
	for _, raw := range []string{"broken", `{}`, `{"language":"en","bindings":[{"product":"ecs"},{"product":"ecs"}]}`} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBaseline(path); err == nil {
			t.Fatalf("invalid baseline accepted: %s", raw)
		}
	}
	if err := WriteBaseline(filepath.Join(root, "missing", "baseline"), Baseline{Language: "en"}); err == nil {
		t.Fatal("missing baseline parent accepted")
	}
	if err := WriteBaseline(root, Baseline{Language: "en"}); err == nil {
		t.Fatal("directory replaced by baseline")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(root), ".drift-baseline-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatalf("failed baseline rename leaked staging: %v,%v", files, err)
	}
	if _, err := DetectResources(nil, Baseline{}, Options{}); err == nil {
		t.Fatal("drift compared without baseline language")
	}
	if err := WriteBaseline(path, Baseline{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Detect(filepath.Join(root, "missing"), path, Options{}); err == nil {
		t.Fatal("missing spec directory ignored")
	}
	productDir := filepath.Join(root, "ecs")
	if err := os.Mkdir(productDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(productDir, "invalid.yaml"), []byte("schema_version: 2\nproduct: ecs\nresource: invalid\nkind: unsupported\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadResources(root); err == nil {
		t.Fatal("invalid resource loaded for drift comparison")
	}
}

func TestDriftReportsAndBaselinesHaveStableCrossResourceOrdering(t *testing.T) {
	dir := t.TempDir()
	for _, product := range []string{"z-last", "a-first"} {
		writeDriftResourceFixture(t, dir, product, "ecs", "CloneDisks")
	}
	baseline, err := CollectBaseline(dir, Options{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Bindings) != 2 || baseline.Bindings[0].Product != "a-first" {
		t.Fatalf("baseline order=%+v", baseline.Bindings)
	}
	resources, err := loadResources(dir)
	if err != nil {
		t.Fatal(err)
	}
	resources = append(resources, spec.ResourceSpec{Product: "a-first", Resource: "another", APIProduct: "ecs", Bindings: map[string]spec.Binding{"z": {API: "CloneDisks"}, "a": {API: "CloneDisks"}}})
	for _, r := range resources {
		for name, binding := range r.Bindings {
			if binding.Request == nil {
				binding.Request = map[string]any{}
			}
			binding.Request["RetiredField"] = "$.retired"
			r.Bindings[name] = binding
			if _, found := baseline.entryFor(r.Product, r.Resource, name, binding.API); !found {
				baseline.Bindings = append(baseline.Bindings, BaselineBinding{Product: r.Product, Resource: r.Resource, Binding: name, API: binding.API, Parameters: []string{"RetiredField"}})
			}
		}
	}
	for i := range baseline.Bindings {
		baseline.Bindings[i].Parameters = []string{"RetiredField"}
	}
	first, err := DetectResources(resources, baseline, Options{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) == 0 || len(first.Missing()) == 0 || len(first.Removed()) == 0 {
		t.Fatalf("synthetic metadata changes missing: %d missing, %d removed", len(first.Missing()), len(first.Removed()))
	}
	slices.Reverse(resources)
	second, err := DetectResources(resources, baseline, Options{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("drift report order depends on resource enumeration")
	}
	if _, err := DetectResources(resources, Baseline{Language: "zh"}, Options{Language: "en"}); err == nil || !strings.Contains(err.Error(), "language") {
		t.Fatalf("cross-language baseline accepted: %v", err)
	}
}
