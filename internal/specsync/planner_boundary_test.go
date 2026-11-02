package specsync

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSyncPlannerRejectsMissingYamlBlocksAndReappliesIdempotently(t *testing.T) {
	item := drift.Item{Binding: "create", API: "CreateThing", Param: "Name", Type: "String"}
	base := func() *resourceFile {
		return &resourceFile{path: "fixture.yaml", resource: spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"name": {Type: "string"}}}, Bindings: map[string]spec.Binding{"create": {API: item.API, Request: map[string]any{"Name": "$.name"}}}, Operations: map[string]spec.Operation{"create": {Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "name"}}}}}}}
	}
	for _, fault := range []string{"schema", "request", "operation"} {
		rf := base()
		switch fault {
		case "schema":
			rf.resource.Schema.Fields = nil
		case "request":
			rf.resource.Bindings["create"] = spec.Binding{}
		case "operation":
			rf.resource.Operations = nil
		}
		if _, err := planTopLevel(newDoc(nil), rf, item, []string{"create"}); err == nil {
			t.Fatalf("missing %s block accepted", fault)
		}
	}
	doc := newDoc([]string{"unchanged"})
	if result, err := planTopLevel(doc, base(), item, []string{"create"}); err != nil || result.action != "already-synced" || !reflect.DeepEqual(doc.lines, []string{"unchanged"}) {
		t.Fatalf("repeated plan changed YAML: %+v,%v", result, err)
	}
	requestDoc := newDoc([]string{"bindings:", "  create:", "    request:", "      Existing: $.existing"})
	if err := addTopLevelRequestMapping(requestDoc, base(), item, "name"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(requestDoc.lines, "\n"); !strings.HasSuffix(got, "      Name: $.name") {
		t.Fatalf("new mapping not appended under request: %s", got)
	}
	if err := refreshResource(&resourceFile{path: "invalid.yaml", lines: []string{"schema_version: invalid"}}); err == nil {
		t.Fatal("invalid staged YAML accepted")
	}
}

func TestSyncNestedPlannerRequiresExistingParentsAndExposesRootInput(t *testing.T) {
	item := drift.Item{Binding: "create", API: "CreateThing", Param: "Settings.Name", Type: "String"}
	base := func() *resourceFile {
		return &resourceFile{path: "fixture.yaml", resource: spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"settings": {Type: "object", Fields: map[string]spec.SchemaField{"name": {Type: "string"}}}}}, Bindings: map[string]spec.Binding{"create": {Request: map[string]any{"Settings": map[string]any{"from": "$.settings", "fields": map[string]any{"Name": "$.name"}}}}}, Operations: map[string]spec.Operation{"create": {Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "settings"}}}}}}}
	}
	if result, err := planNested(newDoc(nil), base(), item, []string{"create"}); err != nil || result.action != "already-synced" {
		t.Fatalf("repeated nested plan=%+v,%v", result, err)
	}
	for _, fault := range []string{"parent", "schema block", "request block", "operation block"} {
		rf := base()
		switch fault {
		case "parent":
			rf.resource.Schema.Fields = nil
		case "schema block":
			rf.resource.Schema.Fields["settings"] = spec.SchemaField{Type: "object", Fields: map[string]spec.SchemaField{}}
		case "request block":
			rf.resource.Bindings["create"] = spec.Binding{}
		case "operation block":
			rf.resource.Operations = nil
		}
		result, err := planNested(newDoc(nil), rf, item, []string{"create"})
		if fault == "parent" {
			if err != nil || result.flagKind != "new_parent_group" {
				t.Fatalf("new parent not flagged: %+v,%v", result, err)
			}
		} else if err == nil {
			t.Fatalf("missing nested %s accepted", fault)
		}
	}
	rf := base()
	rf.resource.Operations = nil
	doc := newDoc([]string{"operations:", "  create:", "    input:", "      fields:"})
	if result, err := planNested(doc, rf, item, []string{"create"}); err != nil || result.action != "patched" || !strings.Contains(strings.Join(doc.lines, "\n"), "- settings:") {
		t.Fatalf("root input not exposed: %+v,%v", result, err)
	}
	for _, field := range []spec.SchemaField{{Type: "object"}, {Type: "array", Items: &spec.SchemaField{Type: "object"}}, {Type: "string"}} {
		if _, ok := schemaParentLocation(map[string]spec.SchemaField{"parent": field}, []string{"Parent"}); ok {
			t.Fatalf("unmodeled parent accepted: %+v", field)
		}
	}
}

func TestSyncYamlHelpersPreserveSupportedRequestShapes(t *testing.T) {
	for _, test := range []struct{ apiType, specType, kind string }{{"Double", "number", "scalar"}, {"RepeatList", "array", "array"}, {"Struct", "object", "object"}, {"unsupported", "", "unknown"}} {
		if got, kind := mapOpenAPIType(test.apiType); got != test.specType || kind != test.kind {
			t.Fatalf("API type %s=%s,%s", test.apiType, got, kind)
		}
	}
	if got := strings.Join(buildSchemaFieldLines(2, "names", "RepeatList", ""), "\n"); !strings.Contains(got, "    items:\n      type: string") {
		t.Fatalf("array schema shape=%s", got)
	}
	if got := strings.Join(buildRequestMappingLines(2, "Names", "names", "RepeatList"), "\n"); !strings.Contains(got, "each: $.names") {
		t.Fatalf("array request shape=%s", got)
	}
	if got := strings.Join(buildRequestMappingLines(2, "Options", "options", "Struct"), "\n"); !strings.Contains(got, "from: $.options") {
		t.Fatalf("object request shape=%s", got)
	}
	doc := newDoc([]string{"root:", "  child: value", "other: value"})
	before := append([]string(nil), doc.lines...)
	doc.insertAt(1)
	if !reflect.DeepEqual(before, doc.lines) || doc.indent(-1) != 0 || doc.indent(100) != 0 {
		t.Fatal("empty insertion or unavailable indent changed document")
	}
	if _, found := doc.findChildKey(0, "absent"); found {
		t.Fatal("sibling accepted as child")
	}
	if lineKey("- name: value") != "name" || lineKey("invalid") != "" {
		t.Fatal("YAML key boundary changed")
	}
}
