package drift

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/openapimetaformat"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

const baseRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const candidateRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func metadataFixture(t *testing.T, revision, version string, api map[string]any, operation map[string]any) string {
	return metadataFixtureWithOptions(t, revision, version, api, operation, "rpc", nil)
}

func metadataFixtureWithOptions(t *testing.T, revision, version string, api map[string]any, operation map[string]any, style string, product map[string]any, operationFileName ...string) string {
	t.Helper()
	fileName := "CreateWidget"
	if len(operationFileName) > 0 {
		fileName = operationFileName[0]
	}
	if product == nil {
		product = map[string]any{"code": "Ecs", "plugin_default_version": version, "api_style": "rpc", "global_endpoint": "ecs.aliyuncs.com", "regional_endpoints": map[string]any{"cn-hangzhou": "ecs.cn-hangzhou.aliyuncs.com"}}
	}
	root := t.TempDir()
	generated := filepath.Join(root, ".generated")
	if err := os.Mkdir(generated, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]any{
		"metadatas/products.json":                             map[string]any{"products": []any{product}},
		"canonical/ecs/" + version + "/version.json":          map[string]any{"version": version, "style": style, "apis": api},
		"canonical/ecs/" + version + "/" + fileName + ".json": operation,
	}
	var entries []openapimetaformat.Entry
	var size int64
	for name, value := range files {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(generated, name+".gz")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, openapimetaformat.Entry{Path: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Size: int64(len(raw))})
		size += int64(len(raw))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	index, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generated, "index.json"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := openapimetaformat.Manifest{
		FormatVersion: openapimetaformat.Version, Repository: openapimetaformat.Repository,
		Revision: revision, IndexSHA256: fmt.Sprintf("%x", sha256.Sum256(index)),
		Products: 1, Operations: 1, Files: len(entries), UncompressedBytes: size,
		DefaultVersions: []openapimetaformat.DefaultVersion{{Code: "ecs", CanonicalCode: "ecs", Version: version, Operations: 1}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func widgetOperation() map[string]any {
	return map[string]any{
		"name": "CreateWidget", "method": "POST", "protocol": "https",
		"parameters": []any{map[string]any{"raw_name": "Name", "type": "string", "required": false, "help_en": "widget name", "enum": []string{"alpha", "beta"}}},
		"responses":  map[string]any{"200": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"Id": map[string]any{"type": "string"}}}}},
	}
}

func widgetResources() []spec.ResourceSpec {
	return []spec.ResourceSpec{{Product: "ecs", Resource: "widget", Bindings: map[string]spec.Binding{"create": {API: "CreateWidget"}}, Operations: map[string]spec.Operation{"create": {Workflow: []spec.WorkflowStep{{Binding: "create"}}}}}}
}

func findChange(report MetadataDiffReport, path string) (MetadataChange, bool) {
	for _, change := range report.Changes {
		if change.Path == path {
			return change, true
		}
	}
	return MetadataChange{}, false
}

func TestMetadataCompareIgnoresDescriptionsAndParameterOrder(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	base["parameters"] = append(base["parameters"].([]any), map[string]any{"raw_name": "RegionId", "type": "string"})
	candidate["parameters"] = append([]any{map[string]any{"raw_name": "RegionId", "type": "string"}}, candidate["parameters"].([]any)...)
	candidate["description_en"] = "new prose"
	candidate["parameters"].([]any)[1].(map[string]any)["help_en"] = "rewritten help"
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false, "description_en": "old"}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false, "description_en": "new"}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 0 {
		t.Fatalf("description/order changes produced code tasks: %+v", report.Changes)
	}
	if report.BaseRevision != baseRevision || report.CandidateRevision != candidateRevision {
		t.Fatalf("revisions lost: %+v", report)
	}
}

func TestMetadataCompareReportsRequestAndResponseSemantics(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	name := candidate["parameters"].([]any)[0].(map[string]any)
	name["type"] = "integer"
	name["required"] = true
	name["enum"] = []string{"alpha"}
	candidate["parameters"] = append(candidate["parameters"].([]any), map[string]any{"raw_name": "ZoneId", "type": "string", "required": false})
	candidate["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"] = map[string]any{}
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": true}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"parameters.Name.type": "breaking", "parameters.Name.required": "breaking",
		"parameters.Name.enum": "breaking", "parameters.ZoneId": "compatible",
		"responses.200.schema.properties.Id": "breaking", "deprecated": "unknown",
	} {
		change, ok := findChange(report, path)
		if !ok || change.Compatibility != want || change.Resource != "widget" || change.Binding != "create" || len(change.Operations) != 1 || change.Operations[0] != "create" {
			t.Errorf("change %s = %+v, found=%t; want compatibility=%s and mapped operation", path, change, ok, want)
		}
	}
	if report.BatchKey == "" {
		t.Fatal("missing deterministic batch key")
	}
	again, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil || again.BatchKey != report.BatchKey {
		t.Fatalf("batch key changed: %v, %+v", err, again)
	}
}

func TestMetadataCompareClassifiesAddedRequiredAndRemovedParameters(t *testing.T) {
	base := widgetOperation()
	base["parameters"] = append(base["parameters"].([]any), map[string]any{"raw_name": "Legacy", "type": "string"})
	candidate := widgetOperation()
	name := candidate["parameters"].([]any)[0].(map[string]any)
	name["enum"] = []string{"gamma", "beta", "alpha"}
	candidate["parameters"] = append(candidate["parameters"].([]any), map[string]any{"raw_name": "ZoneId", "type": "string", "required": true})
	candidate["method"] = "POST"
	candidate["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"] = map[string]any{
		"Id":     map[string]any{"type": "integer"},
		"Status": map[string]any{"type": "string"},
	}
	base["method"] = "GET"
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"method":               "breaking",
		"parameters.Name.enum": "compatible",
		"parameters.Legacy":    "breaking",
		"parameters.ZoneId":    "breaking",
		"responses.200.schema.properties.Id.type": "breaking",
		"responses.200.schema.properties.Status":  "compatible",
	} {
		change, found := findChange(report, path)
		if !found || change.Compatibility != want {
			t.Errorf("%s = %+v, found %t; want %s", path, change, found, want)
		}
	}
}

func TestMetadataCompareRejectsSymlinkedOrMalformedSnapshot(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	candidate := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	link := filepath.Join(t.TempDir(), "linked-candidate")
	if err := os.Symlink(candidate, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareMetadataResources(base, link, widgetResources()); err == nil {
		t.Fatal("symlinked candidate directory was accepted")
	}
	manifest := filepath.Join(candidate, "manifest.json")
	if err := os.WriteFile(manifest, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareMetadataResources(base, candidate, widgetResources()); err == nil {
		t.Fatal("malformed manifest was accepted")
	}
}

func TestMetadataCompareFailsClosedOnMalformedReferencedAPI(t *testing.T) {
	valid := widgetOperation()
	for _, tt := range []struct {
		name    string
		badBase bool
		mutate  func(map[string]any)
	}{
		{"non-object parameter in base", true, func(op map[string]any) { op["parameters"] = []any{"bad"} }},
		{"unnamed parameter in candidate", false, func(op map[string]any) { op["parameters"] = []any{map[string]any{"type": "string"}} }},
		{"duplicate parameter in candidate", false, func(op map[string]any) {
			op["parameters"] = []any{map[string]any{"raw_name": "Name"}, map[string]any{"raw_name": "Name"}}
		}},
		{"mismatched API detail name", false, func(op map[string]any) { op["name"] = "OtherWidget" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bad := widgetOperation()
			tt.mutate(bad)
			baseOp, candidateOp := valid, bad
			if tt.badBase {
				baseOp, candidateOp = bad, valid
			}
			base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, baseOp)
			candidate := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidateOp)
			if _, err := CompareMetadataResources(base, candidate, widgetResources()); err == nil {
				t.Fatal("malformed referenced API produced a report")
			}
		})
	}
	badSummary := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": "invalid summary"}, valid)
	good := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, valid)
	if _, err := CompareMetadataResources(badSummary, good, widgetResources()); err == nil {
		t.Fatal("malformed API summary was accepted")
	}
}

func TestMetadataCompareDistinguishesNewAPIFromUncoveredAPI(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{}, widgetOperation())
	candidate := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	report, err := CompareMetadataResources(base, candidate, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(report, "api")
	if !found || change.Kind != "added" || change.Compatibility != "unknown" {
		t.Fatalf("new API is not reported conservatively: %+v", report.Changes)
	}
	for _, path := range []string{"method", "parameters", "responses"} {
		change, found := findChange(report, path)
		if !found || change.Compatibility != "unknown" {
			t.Errorf("new API omitted or misclassified %s semantics: %+v", path, report.Changes)
		}
	}
	missing := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{}, widgetOperation())
	report, err = CompareMetadataResources(base, missing, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 0 || len(report.Skipped) != 1 || report.Skipped[0].Reason != "no POP metadata for referenced API" {
		t.Fatalf("uncovered API was treated as unchanged: %+v", report)
	}
	if _, err := CompareMetadataResources(filepath.Join(t.TempDir(), "missing-base"), candidate, widgetResources()); err == nil {
		t.Fatal("missing base snapshot was accepted")
	}
}

func TestMetadataCompareLeavesUncertainSemanticChangesForReview(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	baseName := base["parameters"].([]any)[0].(map[string]any)
	baseName["required"] = true
	baseName["location"] = "query"
	candidateName := candidate["parameters"].([]any)[0].(map[string]any)
	candidateName["location"] = "body"
	candidateName["direct_body"] = true
	candidateName["param_style"] = "json"
	baseID := base["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["Id"].(map[string]any)
	baseID["format"] = "uuid"
	candidateID := candidate["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["Id"].(map[string]any)
	candidateID["format"] = "id"
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"parameters.Name.required":                  "compatible",
		"parameters.Name.location":                  "unknown",
		"parameters.Name.direct_body":               "unknown",
		"parameters.Name.param_style":               "unknown",
		"responses.200.schema.properties.Id.format": "unknown",
	} {
		change, found := findChange(report, path)
		if !found || change.Compatibility != want {
			t.Errorf("%s = %+v, found %t; want %s", path, change, found, want)
		}
	}
	resources := widgetResources()
	resources[0].APIProduct = "not-in-pop-snapshot"
	report, err = CompareMetadataResources(b, c, resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 0 || len(report.Skipped) != 1 || report.Skipped[0].Reason != "no POP metadata for referenced API" {
		t.Fatalf("unknown API product was treated as unchanged: %+v", report)
	}
}

func TestMetadataCompareIncludesTransportAndDetailMetadata(t *testing.T) {
	base := widgetOperation()
	base["operation"] = map[string]any{"response_format": "json", "is_sse": false}
	base["deprecated"] = false
	candidate := widgetOperation()
	candidate["operation"] = map[string]any{"response_format": "xml", "is_sse": true}
	candidate["deprecated"] = true
	baseProduct := map[string]any{"code": "Ecs", "plugin_default_version": "2020-01-01", "api_style": "rpc", "global_endpoint": "ecs.aliyuncs.com", "regional_endpoints": map[string]any{"cn-hangzhou": "ecs.a.example"}}
	candidateProduct := map[string]any{"code": "Ecs", "plugin_default_version": "2020-01-01", "api_style": "restful", "global_endpoint": "ecs-new.aliyuncs.com", "regional_endpoints": map[string]any{"cn-hangzhou": "ecs.b.example"}}
	b := metadataFixtureWithOptions(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false}}, base, "rpc", baseProduct)
	c := metadataFixtureWithOptions(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false}}, candidate, "restful", candidateProduct)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"version_style", "product.api_style", "product.global_endpoint", "product.regional_endpoints.cn-hangzhou",
		"deprecated_detail", "operation.response_format", "operation.is_sse",
	} {
		if _, found := findChange(report, path); !found {
			t.Errorf("missing transport/detail change %s: %+v", path, report.Changes)
		}
	}
}

func TestMetadataCompareReportsCaseOnlyAPIActionRename(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	candidate["name"] = "createWidget"
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixtureWithOptions(t, candidateRevision, "2020-01-01", map[string]any{"createWidget": map[string]any{}}, candidate, "rpc", nil, "createWidget")
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(report, "api")
	if !found || change.Before != "CreateWidget" || change.After != "createWidget" || report.BatchKey == "" {
		t.Fatalf("case-only action change was lost: %+v", report)
	}
}

func TestMetadataCompareRejectsNullAPISummary(t *testing.T) {
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": nil}, widgetOperation())
	if _, err := CompareMetadataResources(b, c, widgetResources()); err == nil {
		t.Fatal("checksum-consistent null API summary was accepted")
	}
}

func TestMetadataCompareRejectsNullDeprecatedSummaryField(t *testing.T) {
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": false}}, widgetOperation())
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{"deprecated": nil}}, widgetOperation())
	if _, err := CompareMetadataResources(b, c, widgetResources()); err == nil {
		t.Fatal("checksum-consistent null deprecated field was accepted")
	}
}

func TestMetadataComparePreservesNamedResponseProperties(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	baseProperties := base["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
	candidateProperties := candidate["responses"].(map[string]any)["200"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"summary", "description"} {
		baseProperties[name] = map[string]any{"type": "string"}
		candidateProperties[name] = map[string]any{"type": "object"}
	}
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary", "description"} {
		path := "responses.200.schema.properties." + name + ".type"
		if _, found := findChange(report, path); !found {
			t.Errorf("named response property %s was erased: %+v", path, report.Changes)
		}
	}
}

func TestMetadataCompareTreatsNewResponseStatusAsUnknown(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	candidate["responses"].(map[string]any)["202"] = map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{}}}
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(report, "responses.202")
	if !found || change.Compatibility != "unknown" {
		t.Fatalf("new response variant was treated as compatible: %+v", report.Changes)
	}
}

func TestMetadataCompareDoesNotCallAddedParameterLocationCompatible(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	candidate["parameters"].([]any)[0].(map[string]any)["location"] = "body"
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(report, "parameters.Name.location")
	if !found || change.Compatibility != "unknown" {
		t.Fatalf("request transport change was mislabeled: %+v", report.Changes)
	}
}

func TestMetadataCompareAttributesHookAndWaiterAPIsToOperations(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	candidate["parameters"].([]any)[0].(map[string]any)["required"] = true
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	resources := []spec.ResourceSpec{{
		Product: "ecs", Resource: "widget",
		Bindings: map[string]spec.Binding{"create": {API: "UnlistedAPI", Wait: "ready", Hooks: spec.BindingHooks{APICalls: []spec.HookAPICall{{Hook: "resolve", API: "CreateWidget", Phase: "preflight"}}}}},
		Probes:   map[string]spec.Probe{"status": {API: "CreateWidget"}},
		Waiters:  map[string]spec.Waiter{"ready": {Probe: "status"}},
		Operations: map[string]spec.Operation{
			"create":  {Workflow: []spec.WorkflowStep{{Binding: "create"}}},
			"clone":   {Workflow: []spec.WorkflowStep{{Wait: "ready"}}},
			"inspect": {Call: spec.OperationCall{Probe: "status"}},
		},
	}}
	report, err := CompareMetadataResources(b, c, resources)
	if err != nil {
		t.Fatal(err)
	}
	var hook, probe *MetadataChange
	for i := range report.Changes {
		change := &report.Changes[i]
		if change.Path != "parameters.Name.required" {
			continue
		}
		if strings.HasPrefix(change.Binding, "hook:") {
			hook = change
		}
		if change.Binding == "probe:status" {
			probe = change
		}
	}
	if hook == nil || !containsOperation(hook.Operations, "create") {
		t.Fatalf("hook API lost create operation: %+v", report.Changes)
	}
	for _, operation := range []string{"create", "clone", "inspect"} {
		if probe == nil || !containsOperation(probe.Operations, operation) {
			t.Errorf("probe API lost %s operation: %+v", operation, report.Changes)
		}
	}
}

func containsOperation(operations []string, name string) bool {
	for _, operation := range operations {
		if operation == name {
			return true
		}
	}
	return false
}

func TestMetadataCompareClassifiesEnumAndRequiredPresence(t *testing.T) {
	base := widgetOperation()
	name := base["parameters"].([]any)[0].(map[string]any)
	delete(name, "enum")
	name["required"] = true
	candidate := widgetOperation()
	candidateName := candidate["parameters"].([]any)[0].(map[string]any)
	delete(candidateName, "required")
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"parameters.Name.enum": "breaking", "parameters.Name.required": "compatible"} {
		change, found := findChange(report, path)
		if !found || change.Compatibility != want {
			t.Errorf("%s = %+v, found %t; want %s", path, change, found, want)
		}
	}
	reverse, err := CompareMetadataResources(c, b, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(reverse, "parameters.Name.enum")
	if !found || change.Compatibility != "compatible" {
		t.Fatalf("removed enum should widen acceptance: %+v", reverse.Changes)
	}
}

func TestMetadataCompareDistinguishesEnumJSONTypes(t *testing.T) {
	base := widgetOperation()
	base["parameters"].([]any)[0].(map[string]any)["enum"] = []any{"1"}
	candidate := widgetOperation()
	candidate["parameters"].([]any)[0].(map[string]any)["enum"] = []any{1}
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	report, err := CompareMetadataResources(b, c, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	change, found := findChange(report, "parameters.Name.enum")
	if !found || change.Compatibility != "breaking" {
		t.Fatalf("typed enum replacement was mislabeled: %+v", report.Changes)
	}
}

func TestMetadataCompareRejectsDetailWithoutName(t *testing.T) {
	base := widgetOperation()
	candidate := widgetOperation()
	delete(candidate, "name")
	b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
	c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
	if _, err := CompareMetadataResources(b, c, widgetResources()); err == nil {
		t.Fatal("checksum-consistent detail without an API name was accepted")
	}
}

func TestMetadataCompareRejectsMalformedParameterTypes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"required string", func(param map[string]any) { param["required"] = "true" }},
		{"direct body string", func(param map[string]any) { param["direct_body"] = "true" }},
		{"parameter style number", func(param map[string]any) { param["param_style"] = 3 }},
		{"location array", func(param map[string]any) { param["location"] = []any{"body"} }},
		{"nested field required string", func(param map[string]any) {
			param["fields"] = []any{map[string]any{"raw_name": "Child", "required": "true"}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := widgetOperation()
			candidate := widgetOperation()
			tt.mutate(candidate["parameters"].([]any)[0].(map[string]any))
			b := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, base)
			c := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, candidate)
			if _, err := CompareMetadataResources(b, c, widgetResources()); err == nil {
				t.Fatal("checksum-consistent malformed parameter was accepted")
			}
		})
	}
}

func TestMetadataCompareRejectsInvalidProductCatalogSemantics(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	for _, tt := range []struct {
		name    string
		product map[string]any
	}{
		{"wrong default version", map[string]any{"code": "Ecs", "plugin_default_version": "2021-01-01", "api_style": "rpc"}},
		{"invalid style type", map[string]any{"code": "Ecs", "plugin_default_version": "2020-01-01", "api_style": true}},
		{"invalid endpoint type", map[string]any{"code": "Ecs", "plugin_default_version": "2020-01-01", "regional_endpoints": map[string]any{"cn-hangzhou": 42}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := metadataFixtureWithOptions(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation(), "rpc", tt.product)
			if _, err := CompareMetadataResources(base, candidate, widgetResources()); err == nil {
				t.Fatal("checksum-consistent invalid product catalog was accepted")
			}
		})
	}
}

func TestMetadataCompareReportsDeletedAPIAndVersionChange(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	candidate := metadataFixture(t, candidateRevision, "2021-01-01", map[string]any{}, widgetOperation())
	report, err := CompareMetadataResources(base, candidate, widgetResources())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findChange(report, "api"); !ok {
		t.Fatalf("missing deleted API: %+v", report.Changes)
	}
	if _, ok := findChange(report, "api_version"); !ok {
		t.Fatalf("missing version change: %+v", report.Changes)
	}
	for _, path := range []string{"method", "parameters", "responses"} {
		if _, found := findChange(report, path); !found {
			t.Errorf("deleted API omitted %s semantics: %+v", path, report.Changes)
		}
	}
}

func TestMetadataCompareRejectsInvalidSourceBeforeReporting(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	candidate := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	if err := os.WriteFile(filepath.Join(candidate, ".generated", "canonical", "ecs", "2020-01-01", "CreateWidget.json.gz"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareMetadataResources(base, candidate, widgetResources()); err == nil {
		t.Fatal("corrupt candidate was accepted")
	}
}

func TestMetadataCompareRecordsNonPOPProviderAsUncovered(t *testing.T) {
	base := metadataFixture(t, baseRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	candidate := metadataFixture(t, candidateRevision, "2020-01-01", map[string]any{"CreateWidget": map[string]any{}}, widgetOperation())
	resources := append(widgetResources(), spec.ResourceSpec{Product: "oss", Resource: "bucket", Provider: "ossutil", Bindings: map[string]spec.Binding{"create": {API: "make-bucket"}}})
	report, err := CompareMetadataResources(base, candidate, resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Reason != "non-POP provider" {
		t.Fatalf("uncovered provider hidden: %+v", report.Skipped)
	}
}
