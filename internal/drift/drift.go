package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

// Item kinds.
const (
	// KindMissing marks an OpenAPI request parameter that appeared in the
	// metadata after the drift baseline was recorded and that the binding
	// request does not cover. A newly added API parameter shows up here until
	// the resource spec models it.
	KindMissing = "missing"
	// KindRemoved marks a request parameter the binding still maps although the
	// OpenAPI metadata no longer declares it after the baseline was recorded.
	// This can indicate a renamed or dropped parameter, or simply that the
	// embedded metadata snapshot lags the live API, so it is reported without
	// failing the check.
	KindRemoved = "removed"
	// KindUncovered marks a request parameter the binding covered when the
	// baseline was recorded but no longer maps. It catches coverage
	// regressions — a mapping deleted from an existing binding — that a purely
	// incremental metadata diff could not observe.
	KindUncovered = "uncovered"
)

// Item is one baseline-versus-current difference for a single binding.
type Item struct {
	Product     string `json:"product"`
	Resource    string `json:"resource"`
	Binding     string `json:"binding"`
	API         string `json:"api"`
	Param       string `json:"param"`
	Kind        string `json:"kind"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Position    string `json:"position,omitempty"`
	Description string `json:"description,omitempty"`
}

// Skipped records a binding that could not be diffed because the OpenAPI
// metadata or the drift baseline does not cover it.
type Skipped struct {
	Product  string `json:"product"`
	Resource string `json:"resource"`
	Binding  string `json:"binding"`
	API      string `json:"api"`
	Reason   string `json:"reason"`
}

// Report is the complete result of a drift detection run.
type Report struct {
	Language        string    `json:"language"`
	Items           []Item    `json:"items"`
	Skipped         []Skipped `json:"skipped_bindings,omitempty"`
	BindingsChecked int       `json:"bindings_checked"`
	// BaselineGaps counts bindings whose OpenAPI metadata resolves but whose
	// baseline entry is missing, so they escaped the diff entirely.
	BaselineGaps int `json:"baseline_gaps,omitempty"`
}

// Missing returns the items whose kind is KindMissing, in report order.
func (r Report) Missing() []Item { return r.itemsByKind(KindMissing) }

// Removed returns the items whose kind is KindRemoved, in report order.
func (r Report) Removed() []Item { return r.itemsByKind(KindRemoved) }

// Uncovered returns the items whose kind is KindUncovered, in report order.
func (r Report) Uncovered() []Item { return r.itemsByKind(KindUncovered) }

func (r Report) itemsByKind(kind string) []Item {
	var out []Item
	for _, item := range r.Items {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out
}

// BaselineBinding records both sides of a binding's state when the baseline
// was written: the OpenAPI metadata parameter leaves, and the request keys the
// binding actually covered. Freezing both lets the detector observe coverage
// regressions (mapped before, not mapped now) in addition to metadata change.
type BaselineBinding struct {
	Product    string   `json:"product"`
	Resource   string   `json:"resource"`
	Binding    string   `json:"binding"`
	API        string   `json:"api"`
	Parameters []string `json:"parameters"`
	Covered    []string `json:"covered"`
}

// Baseline is the checked-in snapshot of the OpenAPI metadata leaf sets and
// request coverage for every diffable binding.
type Baseline struct {
	Language string            `json:"language"`
	Bindings []BaselineBinding `json:"bindings"`
}

func (b Baseline) entryFor(product, resource, binding, api string) (BaselineBinding, bool) {
	for _, entry := range b.Bindings {
		if entry.Product == product && entry.Resource == resource &&
			entry.Binding == binding && entry.API == api {
			return entry, true
		}
	}
	return BaselineBinding{}, false
}

// LoadBaseline reads a baseline file.
func LoadBaseline(path string) (Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Baseline{}, err
	}
	var baseline Baseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return Baseline{}, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return baseline, nil
}

// WriteBaseline persists a baseline file atomically: it writes a temporary
// file in the target directory and renames it over the destination, so an
// interrupted write never leaves a truncated baseline.
func WriteBaseline(path string, baseline Baseline) error {
	raw, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".drift-baseline-*.json")
	if err != nil {
		return fmt.Errorf("create baseline temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write baseline temp file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod baseline temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close baseline temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace baseline %s: %w", path, err)
	}
	return nil
}

// Options configures a detection run.
type Options struct {
	// Language selects the OpenAPI metadata language used for parameter
	// descriptions. Defaults to "en".
	Language string
}

// CollectBaseline walks every resource spec under specDir and records the
// current OpenAPI metadata parameter leaves and request coverage for each
// diffable binding.
func CollectBaseline(specDir string, opts Options) (Baseline, error) {
	resources, err := loadResources(specDir)
	if err != nil {
		return Baseline{}, err
	}
	lang := normalizeLanguage(opts.Language)
	baseline := Baseline{Language: lang}
	products := map[string]aliyun.OpenAPIProduct{}
	productFor := productResolver(products, lang)

	for _, resource := range resources {
		code := resource.APIProduct
		if code == "" {
			code = resource.Product
		}
		for bindingName, binding := range resource.Bindings {
			leaves, _, ok := resolveLeaves(productFor, code, binding.API, lang)
			if !ok {
				continue
			}
			baseline.Bindings = append(baseline.Bindings, BaselineBinding{
				Product:    resource.Product,
				Resource:   resource.Resource,
				Binding:    bindingName,
				API:        binding.API,
				Parameters: leafNames(leaves),
				Covered:    sortedKeys(spec.BindingRequestCoverage(binding.Request)),
			})
		}
	}
	sort.Slice(baseline.Bindings, func(i, j int) bool {
		a, b := baseline.Bindings[i], baseline.Bindings[j]
		if a.Product != b.Product {
			return a.Product < b.Product
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Binding != b.Binding {
			return a.Binding < b.Binding
		}
		return a.API < b.API
	})
	return baseline, nil
}

// Detect loads every resource spec under specDir, compares each binding's
// request coverage and the recorded baseline against the current OpenAPI
// metadata, and returns the drift report.
func Detect(specDir, baselinePath string, opts Options) (Report, error) {
	baseline, err := LoadBaseline(baselinePath)
	if err != nil {
		return Report{}, fmt.Errorf("load drift baseline: %w", err)
	}
	resources, err := loadResources(specDir)
	if err != nil {
		return Report{}, err
	}
	return DetectResources(resources, baseline, opts)
}

// DetectResources diffs the given resource specs against the metadata,
// relative to the recorded baseline.
func DetectResources(resources []spec.ResourceSpec, baseline Baseline, opts Options) (Report, error) {
	lang := normalizeLanguage(opts.Language)
	report := Report{Language: lang}
	products := map[string]aliyun.OpenAPIProduct{}
	productFor := productResolver(products, lang)

	for _, resource := range resources {
		code := resource.APIProduct
		if code == "" {
			code = resource.Product
		}
		for bindingName, binding := range resource.Bindings {
			leaves, operation, ok := resolveLeaves(productFor, code, binding.API, lang)
			if !ok {
				report.addSkipped(resource, bindingName, binding, reasonNoMetadata(code, binding.API))
				continue
			}
			entry, hasBaseline := baseline.entryFor(resource.Product, resource.Resource, bindingName, binding.API)
			if !hasBaseline {
				report.BaselineGaps++
				report.addSkipped(resource, bindingName, binding, "no drift baseline entry; refresh the baseline")
				continue
			}
			report.BindingsChecked++
			report.diffBinding(resource, bindingName, binding, operation, leaves, entry.Parameters, entry.Covered)
		}
	}

	sort.Slice(report.Items, func(i, j int) bool {
		a, b := report.Items[i], report.Items[j]
		if a.Product != b.Product {
			return a.Product < b.Product
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Binding != b.Binding {
			return a.Binding < b.Binding
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Param < b.Param
	})
	sort.Slice(report.Skipped, func(i, j int) bool {
		a, b := report.Skipped[i], report.Skipped[j]
		if a.Product != b.Product {
			return a.Product < b.Product
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		return a.Binding < b.Binding
	})
	return report, nil
}

func (r *Report) addSkipped(resource spec.ResourceSpec, bindingName string, binding spec.Binding, reason string) {
	r.Skipped = append(r.Skipped, Skipped{
		Product:  resource.Product,
		Resource: resource.Resource,
		Binding:  bindingName,
		API:      binding.API,
		Reason:   reason,
	})
}

func (r *Report) diffBinding(resource spec.ResourceSpec, bindingName string, binding spec.Binding,
	operation string, liveLeaves []aliyun.OpenAPIParameter, baselineParams, baselineCovered []string) {

	covered := spec.BindingRequestCoverage(binding.Request)
	liveSet := stringSet(liveLeaves, func(leaf aliyun.OpenAPIParameter) string { return leaf.Name })
	baselineSet := stringSet(baselineParams, func(param string) string { return param })
	liveByName := map[string]aliyun.OpenAPIParameter{}
	for _, leaf := range liveLeaves {
		liveByName[leaf.Name] = leaf
	}

	for _, name := range sortedKeys(liveSet) {
		if baselineSet[name] || leafCovered(name, covered) || frameworkHandled(name, binding) {
			continue
		}
		leaf := liveByName[name]
		r.Items = append(r.Items, Item{
			Product:     resource.Product,
			Resource:    resource.Resource,
			Binding:     bindingName,
			API:         operation,
			Param:       name,
			Kind:        KindMissing,
			Type:        leaf.Type,
			Required:    leaf.Required,
			Position:    leaf.Position,
			Description: leaf.Description,
		})
	}

	for _, name := range sortedKeys(baselineSet) {
		if liveSet[name] || !leafCovered(name, covered) || frameworkHandled(name, binding) || liveHasAncestor(name, liveSet) {
			continue
		}
		r.Items = append(r.Items, Item{
			Product:  resource.Product,
			Resource: resource.Resource,
			Binding:  bindingName,
			API:      operation,
			Param:    name,
			Kind:     KindRemoved,
		})
	}

	for _, name := range sortedKeys(stringSet(baselineCovered, func(key string) string { return key })) {
		if leafCovered(name, covered) || frameworkHandled(name, binding) {
			continue
		}
		r.Items = append(r.Items, Item{
			Product:  resource.Product,
			Resource: resource.Resource,
			Binding:  bindingName,
			API:      operation,
			Param:    name,
			Kind:     KindUncovered,
		})
	}
}

// leafCovered reports whether a metadata leaf is covered by the binding
// request: either the leaf path itself, any ancestor group, or any descendant
// key is mapped. The ancestor rule lets a first-class group mapping such as
// `Tag: $.tag` satisfy metadata leaves Tag.Key and Tag.Value; the descendant
// rule lets a detailed mapping such as `body.name` satisfy a metadata group
// that the snapshot only declares as the bare parameter `body`.
func leafCovered(name string, covered map[string]bool) bool {
	if covered[name] {
		return true
	}
	for ancestor := name; ; {
		index := strings.LastIndex(ancestor, ".")
		if index < 0 {
			break
		}
		ancestor = ancestor[:index]
		if covered[ancestor] {
			return true
		}
	}
	for key := range covered {
		if strings.HasPrefix(key, name+".") {
			return true
		}
	}
	return false
}

// liveHasAncestor reports whether the live metadata declares an ancestor group
// of name. A parameter disappears from the leaf list both when it is genuinely
// removed and when the snapshot folds its children into a bare parent group
// (for example Tag.Key/Tag.Value collapsing into a bare Tag). The latter keeps
// the parameter covered, so a removed report on any descendant with a live
// ancestor would be a representation-change false positive.
func liveHasAncestor(name string, liveSet map[string]bool) bool {
	for {
		index := strings.LastIndex(name, ".")
		if index < 0 {
			return false
		}
		name = name[:index]
		if liveSet[name] {
			return true
		}
	}
}

// frameworkHandled reports whether the parameter is set by the execution
// framework rather than the binding request mapping, so it must not be
// reported as uncovered. RegionId is injected by the caller from the resolved
// region when absent from the request (pkg/aliyun/caller.go); the idempotency
// token is generated from binding.Idempotency.Field.
func frameworkHandled(name string, binding spec.Binding) bool {
	if name == "RegionId" {
		return true
	}
	return binding.Idempotency.Field != "" && name == binding.Idempotency.Field
}

func loadResources(specDir string) ([]spec.ResourceSpec, error) {
	refs, err := spec.ListResources(specDir)
	if err != nil {
		return nil, err
	}
	resources := make([]spec.ResourceSpec, 0, len(refs))
	for _, ref := range refs {
		resource, err := spec.LoadResourceWithParent(specDir, ref.Product, ref.Resource, ref.Parent)
		if err != nil {
			return nil, fmt.Errorf("load %s/%s: %w", ref.Product, ref.Resource, err)
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

func productResolver(products map[string]aliyun.OpenAPIProduct, lang string) func(string) (aliyun.OpenAPIProduct, bool) {
	return func(code string) (aliyun.OpenAPIProduct, bool) {
		if product, ok := products[code]; ok {
			return product, true
		}
		product, ok := aliyun.OpenAPIProductByCode(code, lang)
		if ok {
			products[code] = product
		}
		return product, ok
	}
}

func resolveLeaves(productFor func(string) (aliyun.OpenAPIProduct, bool), code, api, lang string) ([]aliyun.OpenAPIParameter, string, bool) {
	product, ok := productFor(code)
	if !ok {
		return nil, "", false
	}
	operation, ok := aliyun.OpenAPIOperationName(product, api)
	if !ok {
		return nil, "", false
	}
	leaves, ok := aliyun.OpenAPIOperationLeaves(lang, product, operation)
	if !ok {
		return nil, "", false
	}
	return leaves, operation, true
}

func reasonNoMetadata(code, api string) string {
	return "OpenAPI metadata unavailable for " + code + "." + api
}

func normalizeLanguage(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "en"
	}
	return lang
}

func leafNames(leaves []aliyun.OpenAPIParameter) []string {
	names := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		names = append(names, leaf.Name)
	}
	sort.Strings(names)
	return names
}

func stringSet[T any](values []T, name func(T) string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		set[name(value)] = true
	}
	return set
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
