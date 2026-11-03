package drift

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/internal/openapimetaformat"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

// MetadataChange records one semantic difference for a spec binding or probe.
type MetadataChange struct {
	Product       string   `json:"product"`
	Resource      string   `json:"resource"`
	Binding       string   `json:"binding"`
	Operations    []string `json:"operations"`
	API           string   `json:"api"`
	Path          string   `json:"path"`
	Kind          string   `json:"kind"`
	Before        any      `json:"before,omitempty"`
	After         any      `json:"after,omitempty"`
	Compatibility string   `json:"compatibility"`
}

// MetadataDiffReport is tied to two complete, checksum-verified revisions.
type MetadataDiffReport struct {
	Repository        string           `json:"repository"`
	BaseRevision      string           `json:"base_revision"`
	CandidateRevision string           `json:"candidate_revision"`
	BatchKey          string           `json:"batch_key,omitempty"`
	Changes           []MetadataChange `json:"changes"`
	Skipped           []Skipped        `json:"skipped_bindings,omitempty"`
}

type metadataSnapshot struct {
	manifest openapimetaformat.Manifest
	reader   *openapimetaformat.Reader
	products map[string]openapimetaformat.DefaultVersion
	catalog  map[string]map[string]any
}

func regularMetadataInput(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("metadata input %s is not a regular %s", path, map[bool]string{true: "directory", false: "file"}[directory])
	}
	return nil
}

func loadMetadataSnapshot(root string) (*metadataSnapshot, error) {
	for _, item := range []struct {
		path      string
		directory bool
	}{
		{root, true}, {filepath.Join(root, "manifest.json"), false}, {filepath.Join(root, ".generated"), true},
	} {
		if err := regularMetadataInput(item.path, item.directory); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, err
	}
	manifest, err := openapimetaformat.DecodeManifest(raw)
	if err != nil {
		return nil, err
	}
	reader := openapimetaformat.NewReader(os.DirFS(filepath.Join(root, ".generated")), raw)
	if err := reader.Check(); err != nil {
		return nil, fmt.Errorf("verify metadata %s: %w", root, err)
	}
	products := make(map[string]openapimetaformat.DefaultVersion, len(manifest.DefaultVersions))
	for _, product := range manifest.DefaultVersions {
		code := strings.ToLower(strings.TrimSpace(product.Code))
		if code == "" || product.CanonicalCode == "" || product.Version == "" || products[code].Code != "" {
			return nil, fmt.Errorf("metadata %s: invalid or duplicate default product %q", root, product.Code)
		}
		products[code] = product
	}
	raw, err = reader.ReadFile("metadatas/products.json")
	if err != nil {
		return nil, fmt.Errorf("metadata %s product catalog: %w", root, err)
	}
	var productCatalog struct {
		Products []map[string]any `json:"products"`
	}
	if err := json.Unmarshal(raw, &productCatalog); err != nil || productCatalog.Products == nil {
		return nil, fmt.Errorf("metadata %s product catalog is invalid: %v", root, err)
	}
	var catalogShape struct {
		Products []struct {
			Code                 string            `json:"code"`
			DefaultVersion       string            `json:"plugin_default_version"`
			Style                string            `json:"api_style"`
			GlobalEndpoint       string            `json:"global_endpoint"`
			RegionalEndpoints    map[string]string `json:"regional_endpoints"`
			RegionalVPCEndpoints map[string]string `json:"regional_vpc_endpoints"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &catalogShape); err != nil {
		return nil, fmt.Errorf("metadata %s product catalog has invalid field types: %w", root, err)
	}
	catalog := make(map[string]map[string]any, len(productCatalog.Products))
	for i, product := range productCatalog.Products {
		code, _ := product["code"].(string)
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" || catalog[code] != nil {
			return nil, fmt.Errorf("metadata %s product catalog has missing or duplicate code %q", root, code)
		}
		if pinned, found := products[code]; found && catalogShape.Products[i].DefaultVersion != pinned.Version {
			return nil, fmt.Errorf("metadata %s product %s catalog default version does not match manifest", root, code)
		}
		semantic := map[string]any{}
		for _, key := range []string{"api_style", "global_endpoint", "regional_endpoints", "regional_vpc_endpoints"} {
			if value, found := product[key]; found {
				semantic[key] = value
			}
		}
		catalog[code] = semantic
	}
	for code := range products {
		if catalog[code] == nil {
			return nil, fmt.Errorf("metadata %s product %s missing from catalog", root, code)
		}
	}
	return &metadataSnapshot{manifest: manifest, reader: reader, products: products, catalog: catalog}, nil
}

type operationMetadata struct {
	name       string
	deprecated bool
	style      string
	detail     map[string]any
}

type metadataParameterShape struct {
	RawName    string                   `json:"raw_name"`
	Name       string                   `json:"name"`
	Location   string                   `json:"location"`
	Type       string                   `json:"type"`
	Required   bool                     `json:"required"`
	ParamStyle string                   `json:"param_style"`
	DirectBody bool                     `json:"direct_body"`
	Fields     []metadataParameterShape `json:"fields"`
	Element    *metadataParameterShape  `json:"element"`
	Value      *metadataParameterShape  `json:"value"`
}

func (s *metadataSnapshot) operation(code, api string) (*operationMetadata, error) {
	product, found := s.products[strings.ToLower(code)]
	if !found {
		return nil, nil
	}
	prefix := "canonical/" + strings.ToLower(product.CanonicalCode) + "/" + product.Version + "/"
	raw, err := s.reader.ReadFile(prefix + "version.json")
	if err != nil {
		return nil, fmt.Errorf("read %s version: %w", code, err)
	}
	var version struct {
		Version string                     `json:"version"`
		Style   string                     `json:"style"`
		APIs    map[string]json.RawMessage `json:"apis"`
	}
	if err := json.Unmarshal(raw, &version); err != nil || version.Version != product.Version || version.APIs == nil {
		return nil, fmt.Errorf("metadata %s version manifest is invalid: %v", code, err)
	}
	canonical := ""
	for name := range version.APIs {
		if strings.EqualFold(name, api) {
			if canonical != "" {
				return nil, fmt.Errorf("metadata %s has ambiguous API %s", code, api)
			}
			canonical = name
		}
	}
	if canonical == "" {
		return nil, nil
	}
	var summary map[string]any
	if err := json.Unmarshal(version.APIs[canonical], &summary); err != nil || summary == nil {
		return nil, fmt.Errorf("metadata %s.%s API summary is invalid: %v", code, canonical, err)
	}
	deprecated := false
	if value, found := summary["deprecated"]; found {
		var ok bool
		deprecated, ok = value.(bool)
		if !ok {
			return nil, fmt.Errorf("metadata %s.%s API summary deprecated flag is invalid", code, canonical)
		}
	}
	raw, err = s.reader.ReadFile(prefix + canonical + ".json")
	if err != nil {
		return nil, fmt.Errorf("read %s.%s: %w", code, canonical, err)
	}
	var shape struct {
		Name        string                   `json:"name"`
		Deprecated  bool                     `json:"deprecated"`
		Protocol    string                   `json:"protocol"`
		Method      string                   `json:"method"`
		PathPattern string                   `json:"pathPattern"`
		Parameters  []metadataParameterShape `json:"parameters"`
		Operation   struct {
			ResponseFormat string `json:"response_format"`
			IsSSE          bool   `json:"is_sse"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return nil, fmt.Errorf("metadata %s.%s detail has invalid field types: %w", code, canonical, err)
	}
	var detail map[string]any
	if err := json.Unmarshal(raw, &detail); err != nil || detail == nil {
		return nil, fmt.Errorf("metadata %s.%s detail is invalid: %v", code, canonical, err)
	}
	name, _ := detail["name"].(string)
	parameters, hasParameters := detail["parameters"].([]any)
	if !strings.EqualFold(name, canonical) || name == "" || !hasParameters || parameters == nil {
		return nil, fmt.Errorf("metadata %s.%s detail name or parameters invalid", code, canonical)
	}
	return &operationMetadata{name: canonical, deprecated: deprecated, style: version.Style, detail: detail}, nil
}

// CompareMetadata loads the spec directory and two independently prepared snapshots.
func CompareMetadata(baseDir, candidateDir, specDir string) (MetadataDiffReport, error) {
	resources, err := loadResources(specDir)
	if err != nil {
		return MetadataDiffReport{}, err
	}
	return CompareMetadataResources(baseDir, candidateDir, resources)
}

// CompareMetadataResources is the same comparison with already loaded specs.
func CompareMetadataResources(baseDir, candidateDir string, resources []spec.ResourceSpec) (MetadataDiffReport, error) {
	base, err := loadMetadataSnapshot(baseDir)
	if err != nil {
		return MetadataDiffReport{}, fmt.Errorf("base: %w", err)
	}
	candidate, err := loadMetadataSnapshot(candidateDir)
	if err != nil {
		return MetadataDiffReport{}, fmt.Errorf("candidate: %w", err)
	}
	report := MetadataDiffReport{Repository: openapimetaformat.Repository, BaseRevision: base.manifest.Revision, CandidateRevision: candidate.manifest.Revision, Changes: []MetadataChange{}}
	for _, resource := range resources {
		code := resource.APIProduct
		if code == "" {
			code = resource.Product
		}
		for _, ref := range resourceAPIRefs(resource) {
			if resource.Provider != "" && resource.Provider != "aliyun" {
				report.Skipped = append(report.Skipped, Skipped{Product: resource.Product, Resource: resource.Resource, Binding: ref.name, API: ref.api, Reason: "non-POP provider"})
				continue
			}
			before, err := base.operation(code, ref.api)
			if err != nil {
				return MetadataDiffReport{}, err
			}
			after, err := candidate.operation(code, ref.api)
			if err != nil {
				return MetadataDiffReport{}, err
			}
			if before == nil && after == nil {
				report.Skipped = append(report.Skipped, Skipped{Product: resource.Product, Resource: resource.Resource, Binding: ref.name, API: ref.api, Reason: "no POP metadata for referenced API"})
				continue
			}
			makeChange := func(path string, old, next any) {
				if reflect.DeepEqual(old, next) {
					return
				}
				compatibility := metadataCompatibility(path, old, next)
				if before == nil && after != nil {
					compatibility = "unknown"
				}
				report.Changes = append(report.Changes, MetadataChange{Product: resource.Product, Resource: resource.Resource, Binding: ref.name, Operations: ref.operations, API: ref.api, Path: path, Kind: changeKind(old, next), Before: old, After: next, Compatibility: compatibility})
			}
			makeChange("api_version", base.products[strings.ToLower(code)].Version, candidate.products[strings.ToLower(code)].Version)
			makeChange("version_style", operationStyle(before), operationStyle(after))
			diffMetadataMaps("product", base.catalog[strings.ToLower(code)], candidate.catalog[strings.ToLower(code)], makeChange)
			makeChange("api", operationName(before), operationName(after))
			makeChange("deprecated", operationDeprecated(before), operationDeprecated(after))
			makeChange("deprecated_detail", operationDetailDeprecated(before), operationDetailDeprecated(after))
			oldDoc := map[string]any{}
			if before != nil {
				oldDoc, err = semanticOperation(before.detail)
				if err != nil {
					return MetadataDiffReport{}, fmt.Errorf("%s/%s %s base: %w", resource.Product, resource.Resource, ref.api, err)
				}
			}
			newDoc := map[string]any{}
			if after != nil {
				newDoc, err = semanticOperation(after.detail)
				if err != nil {
					return MetadataDiffReport{}, fmt.Errorf("%s/%s %s candidate: %w", resource.Product, resource.Resource, ref.api, err)
				}
			}
			diffMetadataMaps("", oldDoc, newDoc, makeChange)
		}
	}
	sort.Slice(report.Changes, func(i, j int) bool {
		a, b := report.Changes[i], report.Changes[j]
		return strings.Join([]string{a.Product, a.Resource, a.Binding, a.API, a.Path}, "\x00") < strings.Join([]string{b.Product, b.Resource, b.Binding, b.API, b.Path}, "\x00")
	})
	sort.Slice(report.Skipped, func(i, j int) bool {
		a, b := report.Skipped[i], report.Skipped[j]
		return strings.Join([]string{a.Product, a.Resource, a.Binding, a.API}, "\x00") < strings.Join([]string{b.Product, b.Resource, b.Binding, b.API}, "\x00")
	})
	if len(report.Changes) > 0 {
		scope := map[string]bool{}
		for _, change := range report.Changes {
			scope[strings.Join([]string{change.Product, change.Resource, change.Binding, change.API}, "/")] = true
		}
		keys := sortedKeys(scope)
		report.BatchKey = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{report.Repository, report.BaseRevision, report.CandidateRevision, strings.Join(keys, "\n")}, "\x00"))))
	}
	return report, nil
}

type resourceAPIRef struct {
	name, api  string
	operations []string
}

func resourceAPIRefs(resource spec.ResourceSpec) []resourceAPIRef {
	refs := []resourceAPIRef{}
	for name, binding := range resource.Bindings {
		refs = append(refs, resourceAPIRef{name: name, api: binding.API, operations: operationNames(resource, name)})
		for _, call := range binding.Hooks.APICalls {
			refs = append(refs, resourceAPIRef{name: "hook:" + name + ":" + call.API, api: call.API, operations: operationNames(resource, name)})
		}
	}
	for name, probe := range resource.Probes {
		refs = append(refs, resourceAPIRef{name: "probe:" + name, api: probe.API, operations: operationNames(resource, name)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].name < refs[j].name })
	return refs
}

func operationNames(resource spec.ResourceSpec, ref string) []string {
	names := []string{}
	for name, operation := range resource.Operations {
		used := operation.Call.Probe == ref
		for _, step := range operation.Workflow {
			if step.Binding == ref || step.Probe == ref {
				used = true
			}
			if waiter, found := resource.Waiters[step.Wait]; found && waiter.Probe == ref {
				used = true
			}
			if binding, found := resource.Bindings[step.Binding]; found {
				if waiter, found := resource.Waiters[binding.Wait]; found && waiter.Probe == ref {
					used = true
				}
			}
		}
		if used {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func operationName(value *operationMetadata) any {
	if value == nil {
		return nil
	}
	return value.name
}

func operationStyle(value *operationMetadata) any {
	if value == nil {
		return nil
	}
	return value.style
}

func operationDeprecated(value *operationMetadata) any {
	if value == nil {
		return nil
	}
	return value.deprecated
}

func operationDetailDeprecated(value *operationMetadata) any {
	if value == nil {
		return nil
	}
	return value.detail["deprecated"]
}

func semanticOperation(detail map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, key := range []string{"protocol", "method", "pathPattern", "operation", "parameters", "responses", "components"} {
		if value, found := detail[key]; found {
			normalized, err := normalizeMetadata(value, key)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
	}
	return out, nil
}

func normalizeMetadata(value any, key string) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for name, child := range typed {
			if key != "properties" && key != "schemas" && ignoredMetadataKey(name) {
				continue
			}
			normalized, err := normalizeMetadata(child, name)
			if err != nil {
				return nil, err
			}
			out[name] = normalized
		}
		return out, nil
	case []any:
		if key == "parameters" || key == "fields" {
			out := map[string]any{}
			for _, child := range typed {
				parameter, ok := child.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s contains non-object parameter", key)
				}
				name, _ := parameter["raw_name"].(string)
				if name == "" {
					name, _ = parameter["name"].(string)
				}
				if name == "" || out[name] != nil {
					return nil, fmt.Errorf("%s contains unnamed or duplicate parameter", key)
				}
				normalized, err := normalizeMetadata(parameter, "parameter")
				if err != nil {
					return nil, err
				}
				delete(normalized.(map[string]any), "raw_name")
				delete(normalized.(map[string]any), "name")
				out[name] = normalized
			}
			return out, nil
		}
		out := make([]any, len(typed))
		for i, child := range typed {
			normalized, err := normalizeMetadata(child, "")
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		if key == "enum" || key == "required" || key == "allOf" || key == "anyOf" || key == "oneOf" {
			sort.Slice(out, func(i, j int) bool {
				a, _ := json.Marshal(out[i])
				b, _ := json.Marshal(out[j])
				return string(a) < string(b)
			})
		}
		return out, nil
	default:
		return value, nil
	}
}

func ignoredMetadataKey(key string) bool {
	return strings.HasPrefix(key, "description") || strings.HasPrefix(key, "help_") ||
		key == "example" || key == "examples" || key == "camel_example" || key == "kebab_example" ||
		key == "cmd_name" || key == "cmd_full_name" || key == "options" || key == "title" || key == "summary"
}

func diffMetadataMaps(prefix string, before, after map[string]any, add func(string, any, any)) {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for _, key := range sortedKeys(keys) {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		old, oldOK := before[key]
		next, nextOK := after[key]
		if !oldOK {
			add(path, nil, next)
			continue
		}
		if !nextOK {
			add(path, old, nil)
			continue
		}
		oldMap, oldIsMap := old.(map[string]any)
		nextMap, nextIsMap := next.(map[string]any)
		if oldIsMap && nextIsMap {
			diffMetadataMaps(path, oldMap, nextMap, add)
		} else {
			add(path, old, next)
		}
	}
}

func changeKind(old, next any) string {
	if old == nil {
		return "added"
	}
	if next == nil {
		return "removed"
	}
	return "changed"
}

func metadataCompatibility(path string, old, next any) string {
	if path == "api" {
		if next == nil {
			return "breaking"
		}
		return "unknown"
	}
	if path == "api_version" || path == "version_style" || path == "deprecated" || path == "deprecated_detail" || strings.HasPrefix(path, "product.") {
		return "unknown"
	}
	if path == "method" || path == "protocol" || path == "pathPattern" {
		return "breaking"
	}
	if strings.HasPrefix(path, "parameters.") {
		if strings.HasSuffix(path, ".location") {
			return "unknown"
		}
		if strings.HasSuffix(path, ".required") {
			if old != nil && old != true && old != false || next != nil && next != true && next != false {
				return "unknown"
			}
			if next == true {
				return "breaking"
			}
			return "compatible"
		}
		if strings.HasSuffix(path, ".enum") {
			if old == nil && next == nil {
				return "unknown"
			}
			if old == nil {
				return "breaking"
			}
			if next == nil {
				return "compatible"
			}
			oldValues, oldOK := old.([]any)
			newValues, newOK := next.([]any)
			if !oldOK || !newOK {
				return "unknown"
			}
			newSet := map[string]bool{}
			for _, value := range newValues {
				encoded, err := json.Marshal(value)
				if err != nil {
					return "unknown"
				}
				newSet[string(encoded)] = true
			}
			for _, value := range oldValues {
				encoded, err := json.Marshal(value)
				if err != nil {
					return "unknown"
				}
				if !newSet[string(encoded)] {
					return "breaking"
				}
			}
			return "compatible"
		}
		if strings.HasSuffix(path, ".type") {
			if old == nil || next == nil {
				return "unknown"
			}
			return "breaking"
		}
		if next == nil {
			return "breaking"
		}
		if old == nil {
			if param, ok := next.(map[string]any); ok && param["required"] == true {
				return "breaking"
			}
			if _, ok := next.(map[string]any); ok && strings.Count(path, ".") == 1 {
				return "compatible"
			}
			return "unknown"
		}
		return "unknown"
	}
	if strings.HasPrefix(path, "responses.") || strings.HasPrefix(path, "components.") {
		if next == nil || strings.HasSuffix(path, ".type") {
			return "breaking"
		}
		if old == nil {
			if strings.HasPrefix(path, "responses.") && strings.Count(path, ".") == 1 {
				return "unknown"
			}
			return "compatible"
		}
	}
	return "unknown"
}
