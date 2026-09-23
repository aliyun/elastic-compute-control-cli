package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/openapimeta"
	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func loadInstanceSpec(t *testing.T) spec.ResourceSpec {
	t.Helper()
	loaded, err := spec.LoadFile("../../specs/ecs/instance.yaml")
	if err != nil {
		t.Fatalf("LoadFile(instance.yaml): %v", err)
	}
	return loaded
}

// metadataLeaves returns the current OpenAPI metadata leaves for an operation,
// in sorted order.
func metadataLeaves(t *testing.T, productCode, api string) []string {
	t.Helper()
	product, ok := aliyun.OpenAPIProductByCode(productCode, "en")
	if !ok {
		t.Fatalf("OpenAPIProductByCode(%s) failed", productCode)
	}
	operation, ok := aliyun.OpenAPIOperationName(product, api)
	if !ok {
		t.Fatalf("OpenAPIOperationName(%s) failed", api)
	}
	leaves, ok := aliyun.OpenAPIOperationLeaves("en", product, operation)
	if !ok {
		t.Fatalf("OpenAPIOperationLeaves(%s) failed", api)
	}
	names := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		names = append(names, leaf.Name)
	}
	sort.Strings(names)
	return names
}

// baselineEntryFor records a baseline entry for one binding of the given
// resource: metadata leaves (with the given names dropped to simulate a
// metadata change) plus the binding's current request coverage.
func baselineEntryFor(t *testing.T, resource spec.ResourceSpec, binding, productCode, api string, dropParams ...string) BaselineBinding {
	t.Helper()
	bindingSpec := resource.Bindings[binding]
	names := metadataLeaves(t, productCode, api)
	dropped := map[string]bool{}
	for _, name := range dropParams {
		dropped[name] = true
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if !dropped[name] {
			kept = append(kept, name)
		}
	}
	covered := make([]string, 0)
	for key := range spec.BindingRequestCoverage(bindingSpec.Request) {
		covered = append(covered, key)
	}
	sort.Strings(covered)
	return BaselineBinding{
		Product:    resource.Product,
		Resource:   resource.Resource,
		Binding:    binding,
		API:        api,
		Parameters: kept,
		Covered:    covered,
	}
}

func baselineOf(entry BaselineBinding) Baseline {
	return Baseline{Language: "en", Bindings: []BaselineBinding{entry}}
}

func TestDetectReportsMetadataAddition(t *testing.T) {
	loaded := loadInstanceSpec(t)
	// The run_command binding does not model TerminationMode, so a baseline
	// recorded before the metadata gained it must surface it as missing.
	entry := baselineEntryFor(t, loaded, "run_command", "ecs", "RunCommand", "TerminationMode")

	report, err := DetectResources([]spec.ResourceSpec{loaded}, baselineOf(entry), Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if report.BindingsChecked != 1 {
		t.Fatalf("BindingsChecked = %d, want 1", report.BindingsChecked)
	}
	missing := report.Missing()
	if len(missing) != 1 {
		t.Fatalf("expected exactly one missing parameter, got %d: %#v", len(missing), missing)
	}
	item := missing[0]
	if item.Param != "TerminationMode" {
		t.Fatalf("missing param = %q, want TerminationMode", item.Param)
	}
	if item.Binding != "run_command" || item.API != "RunCommand" {
		t.Fatalf("missing item context = %q/%q, want run_command/RunCommand", item.Binding, item.API)
	}
	if item.Type != "String" {
		t.Fatalf("missing type = %q, want String", item.Type)
	}
	if removed := report.Removed(); len(removed) != 0 {
		t.Fatalf("unexpected removed parameters: %#v", removed)
	}
	if uncovered := report.Uncovered(); len(uncovered) != 0 {
		t.Fatalf("unexpected uncovered parameters: %#v", uncovered)
	}
}

func TestFrameworkHandledIdempotencyTokenNotReported(t *testing.T) {
	loaded := loadInstanceSpec(t)
	// ClientToken is new in the metadata but the create binding declares it as
	// the idempotency field, so the executor injects it: it must not be
	// reported as missing.
	entry := baselineEntryFor(t, loaded, "create_to_running", "ecs", "RunInstances", "ClientToken")

	report, err := DetectResources([]spec.ResourceSpec{loaded}, baselineOf(entry), Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if missing := report.Missing(); len(missing) != 0 {
		t.Fatalf("unexpected missing parameters: %#v", missing)
	}
	if removed := report.Removed(); len(removed) != 0 {
		t.Fatalf("unexpected removed parameters: %#v", removed)
	}
	if uncovered := report.Uncovered(); len(uncovered) != 0 {
		t.Fatalf("unexpected uncovered parameters: %#v", uncovered)
	}
}

func TestDetectReportsUncoveredRegression(t *testing.T) {
	// Baseline is recorded from the intact binding; the binding under test has
	// had its ZoneId mapping deleted. The metadata is unchanged, so a purely
	// incremental diff would be silent — the symmetric baseline must flag the
	// lost mapping as uncovered.
	intact := loadInstanceSpec(t)
	entry := baselineEntryFor(t, intact, "create_to_running", "ecs", "RunInstances")

	modified := loadInstanceSpec(t)
	binding := modified.Bindings["create_to_running"]
	delete(binding.Request, "ZoneId")
	modified.Bindings["create_to_running"] = binding

	report, err := DetectResources([]spec.ResourceSpec{modified}, baselineOf(entry), Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	uncovered := report.Uncovered()
	if len(uncovered) != 1 {
		t.Fatalf("expected exactly one uncovered parameter, got %d: %#v", len(uncovered), uncovered)
	}
	if uncovered[0].Param != "ZoneId" {
		t.Fatalf("uncovered param = %q, want ZoneId", uncovered[0].Param)
	}
	if missing := report.Missing(); len(missing) != 0 {
		t.Fatalf("unexpected missing parameters: %#v", missing)
	}
	if removed := report.Removed(); len(removed) != 0 {
		t.Fatalf("unexpected removed parameters: %#v", removed)
	}
}

func TestUnrelatedBaselineCoverageDoesNotReportUncovered(t *testing.T) {
	resource := spec.ResourceSpec{Product: "lingjun", Resource: "node-group"}
	binding := spec.Binding{API: "UpdateNodeGroup", Request: map[string]any{}}
	report := &Report{}

	report.diffBinding(
		resource,
		"update_node_group",
		binding,
		"UpdateNodeGroup",
		[]aliyun.OpenAPIParameter{{Name: "NodeGroupName"}},
		[]string{"NodeGroupName"},
		[]string{"RamRoleName"},
	)

	if uncovered := report.Uncovered(); len(uncovered) != 0 {
		t.Fatalf("unrelated stale mapping reported uncovered: %#v", uncovered)
	}
}

func TestEffectiveCoveredFiltersUnrelatedAndFrameworkManagedPaths(t *testing.T) {
	binding := spec.Binding{Idempotency: spec.Idempotency{Field: "ClientToken"}}
	leaves := []aliyun.OpenAPIParameter{
		{Name: "ZoneId"},
		{Name: "Tag.Key"},
		{Name: "RegionId"},
		{Name: "ClientToken"},
	}
	covered := map[string]bool{
		"ZoneId":      true,
		"Tag":         true,
		"RamRoleName": true,
		"RegionId":    true,
		"ClientToken": true,
	}

	got := effectiveCovered(leaves, covered, binding)
	want := []string{"Tag", "ZoneId"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("effectiveCovered = %v, want %v", got, want)
	}
}

func TestDetectReportsMissingBaselineEntry(t *testing.T) {
	loaded := loadInstanceSpec(t)
	// A diffable binding with no baseline entry escapes the diff entirely: it
	// must be counted as a baseline gap, not silently skipped.
	report, err := DetectResources([]spec.ResourceSpec{loaded}, Baseline{Language: "en"}, Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if report.BaselineGaps == 0 {
		t.Fatal("expected BaselineGaps > 0 for bindings with no baseline entry")
	}
	if report.BindingsChecked != 0 {
		t.Fatalf("BindingsChecked = %d, want 0", report.BindingsChecked)
	}
	if missing := report.Missing(); len(missing) != 0 {
		t.Fatalf("unexpected missing parameters: %#v", missing)
	}
}

func TestDetectFailsWhenBaselineMetadataDisappears(t *testing.T) {
	resource := spec.ResourceSpec{
		Product: "missing-product", Resource: "thing",
		Bindings: map[string]spec.Binding{"create": {API: "MissingOperation"}},
	}
	baseline := Baseline{Language: "en", Bindings: []BaselineBinding{{
		Product: resource.Product, Resource: resource.Resource,
		Binding: "create", API: "MissingOperation",
	}}}

	_, err := DetectResources([]spec.ResourceSpec{resource}, baseline, Options{})
	if err == nil || !strings.Contains(err.Error(), "required OpenAPI product") {
		t.Fatalf("DetectResources error = %v, want missing baseline metadata error", err)
	}
}

func TestDetectIgnoresNonAliyunProviders(t *testing.T) {
	resource := spec.ResourceSpec{
		Product: "sandbox", Provider: "e2b", Resource: "sandbox",
		Bindings: map[string]spec.Binding{"create": {API: "CreateSandbox"}},
	}

	report, err := DetectResources([]spec.ResourceSpec{resource}, Baseline{Language: "en"}, Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if report.BindingsChecked != 0 || report.BaselineGaps != 0 || len(report.Skipped) != 0 {
		t.Fatalf("non-Aliyun resource affected drift report: %#v", report)
	}
}

func writeDriftResourceFixture(t *testing.T, dir, product, apiProduct, api string) {
	t.Helper()
	productDir := filepath.Join(dir, product)
	if err := os.MkdirAll(productDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf(`schema_version: 2
product: %s
api_product: %s
resource: thing
kind: regional
schema:
  fields:
    id:
      type: string
operations: {}
`, product, apiProduct)
	if api != "" {
		contents += fmt.Sprintf("bindings:\n  create:\n    api: %s\n    request: {}\n", api)
	}
	if err := os.WriteFile(filepath.Join(productDir, "thing.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectBaselineRejectsUnresolvableBinding(t *testing.T) {
	for _, tt := range []struct {
		name, product, apiProduct, api, wantError string
	}{
		{"missing product", "missing-product", "", "MissingOperation", "required OpenAPI product"},
		{"missing operation", "ecs", "", "MissingOperation", "MissingOperation"},
		{"no fallback from api_product", "ecs", "missing-product", "RunInstances", "required OpenAPI product"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDriftResourceFixture(t, dir, tt.product, tt.apiProduct, tt.api)
			if _, err := CollectBaseline(dir, Options{Language: "en"}); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("CollectBaseline error = %v, want %s", err, tt.wantError)
			}

			resources, err := loadResources(dir)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a previously accepted operation that the current metadata
			// no longer declares. A baseline is not permission to use legacy or
			// missing metadata, even if its parameters and coverage are empty.
			for _, parameters := range [][]string{nil, {"OldParameter"}} {
				baseline := baselineOf(BaselineBinding{
					Product: tt.product, Resource: "thing", Binding: "create", API: tt.api,
					Parameters: parameters, Covered: parameters,
				})
				if _, err := DetectResources(resources, baseline, Options{Language: "en"}); err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("DetectResources error = %v, want %s", err, tt.wantError)
				}
			}
		})
	}
}

func TestCollectBaselineSkipsDescriptorsAndKeepsBindingsWithoutOperations(t *testing.T) {
	dir := t.TempDir()
	writeDriftResourceFixture(t, dir, "descriptor-alias", "", "")
	writeDriftResourceFixture(t, dir, "ecs-alias", "EcS", "CloneDisks")

	baseline, err := CollectBaseline(dir, Options{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Bindings) != 1 || baseline.Bindings[0].Product != "ecs-alias" || baseline.Bindings[0].API != "CloneDisks" {
		t.Fatalf("baseline = %#v, want only ecs-alias CloneDisks binding", baseline)
	}
	resources, err := loadResources(dir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := DetectResources(resources, baseline, Options{Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if report.BindingsChecked != 1 || report.BaselineGaps != 0 || len(report.Skipped) != 0 || len(report.Items) != 0 {
		t.Fatalf("unexpected drift report: %#v", report)
	}
}

func TestResolveLeavesRequiresCanonicalOperation(t *testing.T) {
	resolver, err := aliyun.NewOpenAPIMetadataResolver("en", []string{"ecs"})
	if err != nil {
		t.Fatal(err)
	}
	for _, api := range []string{"CloneDisks", "cLoNeDiSkS"} {
		leaves, operation, err := resolveLeaves(resolver, "EcS", api)
		if err != nil {
			t.Fatalf("resolveLeaves(%s): %v", api, err)
		}
		if operation != "CloneDisks" || len(leaves) == 0 {
			t.Fatalf("resolveLeaves(%s) = %q, %d leaves", api, operation, len(leaves))
		}
	}
	if _, _, err := resolveLeaves(resolver, "ecs", "MissingOperation"); err == nil {
		t.Fatal("unknown operation must not resolve")
	}
}

// Every API declaration is checked, including bindings not currently referenced
// by operations. Operation calls/workflows and waiters only reference these
// probes and bindings; hook preflight calls are the other API-bearing field.
func resourceAPICalls(resource spec.ResourceSpec) map[string]string {
	calls := map[string]string{}
	for name, probe := range resource.Probes {
		calls["probes."+name] = probe.API
	}
	for name, binding := range resource.Bindings {
		calls["bindings."+name] = binding.API
		for index, call := range binding.Hooks.APICalls {
			calls[fmt.Sprintf("bindings.%s.hooks.api_calls.%d", name, index)] = call.API
		}
	}
	return calls
}

func TestRepoAliyunOperationsResolveStrictly(t *testing.T) {
	resources, err := loadResources("../../specs")
	if err != nil {
		t.Fatal(err)
	}
	resources = aliyunResources(resources)
	codes := resourceProductCodes(resources)
	resolver, err := aliyun.NewOpenAPIMetadataResolver("en", codes)
	if err != nil {
		t.Fatal(err)
	}

	// Independently verify that the public product selection uses the pinned
	// catalog's default version, not an older version with legacy-only APIs.
	raw, err := openapimeta.ReadFile("metadatas/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Products []struct {
			Code    string `json:"code"`
			Version string `json:"version"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{}
	for _, product := range catalog.Products {
		defaults[strings.ToLower(product.Code)] = product.Version
	}
	products, err := aliyun.OpenAPIProducts("en")
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	for _, product := range products {
		versions[strings.ToLower(product.Code)] = product.Version
	}
	manifests := map[string]map[string]json.RawMessage{}
	for _, code := range codes {
		version := defaults[code]
		if version == "" || versions[code] != version {
			t.Fatalf("product %s version = %q, want catalog default %q", code, versions[code], version)
		}
		raw, err := openapimeta.ReadFile("canonical/" + code + "/" + version + "/version.json")
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			APIs map[string]json.RawMessage `json:"apis"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifests[code] = manifest.APIs
	}

	operations := map[string]map[string]bool{}
	checked := 0
	for _, resource := range resources {
		code := resource.APIProduct
		if code == "" {
			code = resource.Product
		}
		key := strings.ToLower(code)
		calls := resourceAPICalls(resource)
		paths := make([]string, 0, len(calls))
		for path := range calls {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			api := calls[path]
			t.Run(resource.Product+"/"+resource.Resource+"/"+path, func(t *testing.T) {
				// Exercise both the declared casing and case-insensitive lookup.
				for _, productCode := range []string{code, strings.ToUpper(code)} {
					_, operation, err := resolver.OperationLeaves(productCode, api, false)
					if err != nil {
						t.Errorf("strict OperationLeaves(%q, %q): %v", productCode, api, err)
						continue
					}
					if _, ok := manifests[key][operation]; !ok {
						t.Errorf("%s.%s is absent from the default-version canonical manifest", productCode, operation)
					}
				}
			})
			if operations[key] == nil {
				operations[key] = map[string]bool{}
			}
			operations[key][strings.ToLower(api)] = true
			checked++
		}
	}
	// Guard against silently losing an entire product or a class of API calls.
	// New operations remain allowed, but all current ones must stay covered.
	for code, minimum := range map[string]int{"resourcemanager": 38, "eflo-controller": 33} {
		if got := len(operations[code]); got < minimum {
			t.Errorf("%s checked %d unique operations, want at least %d", code, got, minimum)
		}
	}
	t.Logf("checked %d API declarations; ResourceManager=%d, eflo-controller=%d unique operations",
		checked, len(operations["resourcemanager"]), len(operations["eflo-controller"]))
}

func TestResourceProductCodesIncludesOnlyAPIResources(t *testing.T) {
	resources := []spec.ResourceSpec{
		{Product: "descriptor-alias", Resource: "descriptor"},
		{Product: "descriptor", APIProduct: "unused-api-alias", Resource: "descriptor"},
		{Product: "local", Operations: map[string]spec.Operation{"show": {Emit: "local"}}},
		{Product: "alias", APIProduct: "EcS", Bindings: map[string]spec.Binding{"clone": {API: "CloneDisks"}}},
		{Product: "ecs", Bindings: map[string]spec.Binding{"create": {API: "RunInstances"}}},
		{Product: "rg", APIProduct: "ResourceManager", Probes: map[string]spec.Probe{"list": {API: "ListResourceGroups"}}},
	}
	want := []string{"ecs", "resourcemanager"}
	if got := resourceProductCodes(resources); !reflect.DeepEqual(got, want) {
		t.Fatalf("resourceProductCodes = %v, want %v", got, want)
	}
	if resources[3].APIProduct != "EcS" || resources[5].APIProduct != "ResourceManager" {
		t.Fatal("product lookup must not rewrite the spec's display/API casing")
	}
}

func TestResourceAPICallsIncludesAllCallSites(t *testing.T) {
	resource, err := spec.Load([]byte(`schema_version: 2
product: ecs
resource: fixture
kind: regional
schema:
  fields:
    id:
      type: string
probes:
  call:
    api: DescribeInstances
  workflow:
    api: DescribeDisks
  waiter:
    api: DescribeTasks
bindings:
  used:
    api: RunInstances
    request: {}
    wait: ready
    hooks:
      before: [preflight]
      api_calls:
        - hook: preflight
          api: DescribeImages
          phase: preflight
          purpose: Check the image
  unreferenced:
    api: CloneDisks
    request: {}
waiters:
  ready:
    probe: waiter
    target: Ready
operations:
  list:
    call:
      probe: call
  create:
    examples: [ecctl ecs fixture create]
    workflow:
      - binding: used
      - probe: workflow
      - wait: ready
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(resource); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"probes.call":                     "DescribeInstances",
		"probes.workflow":                 "DescribeDisks",
		"probes.waiter":                   "DescribeTasks",
		"bindings.used":                   "RunInstances",
		"bindings.used.hooks.api_calls.0": "DescribeImages",
		"bindings.unreferenced":           "CloneDisks",
	}
	if got := resourceAPICalls(resource); !reflect.DeepEqual(got, want) {
		t.Fatalf("resourceAPICalls = %v, want %v", got, want)
	}
}

func TestDetectCleanAgainstCurrentBaseline(t *testing.T) {
	loaded := loadInstanceSpec(t)
	entry := baselineEntryFor(t, loaded, "run_command", "ecs", "RunCommand")

	report, err := DetectResources([]spec.ResourceSpec{loaded}, baselineOf(entry), Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if missing := report.Missing(); len(missing) != 0 {
		t.Fatalf("unexpected missing parameters: %#v", missing)
	}
	if removed := report.Removed(); len(removed) != 0 {
		t.Fatalf("unexpected removed parameters: %#v", removed)
	}
	if uncovered := report.Uncovered(); len(uncovered) != 0 {
		t.Fatalf("unexpected uncovered parameters: %#v", uncovered)
	}
}

func TestFrameworkHandledRegionIdNotReported(t *testing.T) {
	intact := loadInstanceSpec(t)
	entry := baselineEntryFor(t, intact, "create_to_running", "ecs", "RunInstances")

	// Drop the explicit RegionId mapping: the caller injects RegionId from the
	// resolved region, so the lost mapping is not a real coverage regression.
	modified := loadInstanceSpec(t)
	binding := modified.Bindings["create_to_running"]
	delete(binding.Request, "RegionId")
	modified.Bindings["create_to_running"] = binding

	report, err := DetectResources([]spec.ResourceSpec{modified}, baselineOf(entry), Options{})
	if err != nil {
		t.Fatalf("DetectResources: %v", err)
	}
	if uncovered := report.Uncovered(); len(uncovered) != 0 {
		t.Fatalf("expected RegionId to be framework-exempt, uncovered: %#v", uncovered)
	}
	if missing := report.Missing(); len(missing) != 0 {
		t.Fatalf("unexpected missing parameters: %#v", missing)
	}
}

func TestRemovedSkipsCollapsedGroup(t *testing.T) {
	resource := spec.ResourceSpec{Product: "ecs", Resource: "instance"}
	binding := spec.Binding{API: "RunInstances", Request: map[string]any{"Tag": "$.tag"}}

	// The metadata folded Tag.Key/Tag.Value into a bare Tag group: removed must
	// not report the folded children as dropped.
	report := &Report{}
	report.diffBinding(resource, "create", binding, "RunInstances",
		[]aliyun.OpenAPIParameter{{Name: "Tag"}},
		[]string{"Tag.Key", "Tag.Value"},
		[]string{"Tag"},
	)
	if removed := report.Removed(); len(removed) != 0 {
		t.Fatalf("collapsed group reported removed: %#v", removed)
	}

	// With no live Tag group, the children were genuinely removed.
	report = &Report{}
	report.diffBinding(resource, "create", binding, "RunInstances",
		nil,
		[]string{"Tag.Key", "Tag.Value"},
		[]string{"Tag"},
	)
	removed := report.Removed()
	if len(removed) != 2 {
		t.Fatalf("genuinely removed children = %#v, want 2", removed)
	}
}

func TestRepoSpecsHaveNoDrift(t *testing.T) {
	report, err := Detect("../../specs", "../../drift-baseline.json", Options{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	missing, removed, uncovered := report.Missing(), report.Removed(), report.Uncovered()
	if len(missing) == 0 && len(removed) == 0 && len(uncovered) == 0 && report.BaselineGaps == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "bindings checked: %d, skipped: %d, missing: %d, removed: %d, uncovered: %d, baseline gaps: %d (first 20):\n",
		report.BindingsChecked, len(report.Skipped), len(missing), len(removed), len(uncovered), report.BaselineGaps)
	items := append(append(append([]Item{}, missing...), removed...), uncovered...)
	for i, item := range items {
		if i == 20 {
			break
		}
		fmt.Fprintf(&b, "  %s.%s %s %s %s (%s)\n",
			item.Product, item.Resource, item.Binding, item.API, item.Param, item.Kind)
	}
	t.Fatalf("specs have drift or missing baseline entries: %s", b.String())
}

func TestLeafCoveredRules(t *testing.T) {
	tests := []struct {
		name    string
		leaf    string
		covered map[string]bool
		want    bool
	}{
		{"exact leaf", "ZoneId", map[string]bool{"ZoneId": true}, true},
		{"ancestor group covers child", "Tag.Key", map[string]bool{"Tag": true}, true},
		{"child covers ancestor leaf details", "SystemDisk", map[string]bool{"SystemDisk.Category": true}, true},
		{"body group covered by body children", "body", map[string]bool{"body.name": true}, true},
		{"unrelated", "KeepAliveTimeout", map[string]bool{"ZoneId": true}, false},
		{"deep descendant", "a.b", map[string]bool{"a.b.c.d": true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := leafCovered(tt.leaf, tt.covered); got != tt.want {
				t.Errorf("leafCovered(%q) = %t, want %t", tt.leaf, got, tt.want)
			}
		})
	}
}
