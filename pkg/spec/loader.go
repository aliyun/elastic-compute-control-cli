package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"

	"github.com/goccy/go-yaml"
)

type resourceKey struct {
	product  string
	resource string
	parent   string
}

type specCacheEntry struct {
	refs      []ResourceRef
	resources map[resourceKey]ResourceSpec
	products  map[string]ProductSpec
}

var (
	cacheMu    sync.Mutex
	cacheDir   string
	cacheEntry *specCacheEntry
)

func ResetCacheForTest() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheDir = ""
	cacheEntry = nil
}

func getCachedEntry(specDir string) *specCacheEntry {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheEntry != nil && cacheDir == specDir {
		return cacheEntry
	}
	return nil
}

func setCachedEntry(specDir string, entry *specCacheEntry) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheDir = specDir
	cacheEntry = entry
}

type ResourceSpec struct {
	SchemaVersion int                      `yaml:"schema_version"`
	Product       string                   `yaml:"product"`
	APIProduct    string                   `yaml:"api_product"`
	Resource      string                   `yaml:"resource"`
	Parent        string                   `yaml:"parent"`
	Kind          string                   `yaml:"kind"`
	Aliases       []string                 `yaml:"aliases"`
	DisplayName   LocalizedText            `yaml:"display_name"`
	Description   LocalizedText            `yaml:"description"`
	Help          LocalizedText            `yaml:"help"`
	Examples      []string                 `yaml:"examples"`
	Messages      map[string]LocalizedText `yaml:"messages"`
	Identity      Identity                 `yaml:"identity"`
	Schema        ResourceSchema           `yaml:"schema"`
	Controls      map[string]SchemaField   `yaml:"controls"`
	Probes        map[string]Probe         `yaml:"probes"`
	Waiters       map[string]Waiter        `yaml:"waiters"`
	Bindings      map[string]Binding       `yaml:"bindings"`
	Operations    map[string]Operation     `yaml:"operations"`
}

type ProductSpec struct {
	SchemaVersion int           `yaml:"schema_version"`
	Product       string        `yaml:"product"`
	Priority      int           `yaml:"priority"`
	Resources     []string      `yaml:"resources"`
	Description   LocalizedText `yaml:"description"`
	Examples      []string      `yaml:"examples"`
}

type ResourceRef struct {
	Product  string
	Resource string
	Parent   string
}

type Identity struct {
	Field      string     `yaml:"field"`
	Prefix     string     `yaml:"prefix"`
	OutputRoot OutputRoot `yaml:"output_root"`
}

type OutputRoot struct {
	One  string `yaml:"one"`
	Many string `yaml:"many"`
}

type Param struct {
	Type           string        `yaml:"type"`
	Description    LocalizedText `yaml:"description"`
	Required       bool          `yaml:"required"`
	Repeatable     bool          `yaml:"repeatable"`
	Positional     bool          `yaml:"positional"`
	PositionalMany bool          `yaml:"positional_many"`
	AllowEmpty     bool          `yaml:"allow_empty"`
	FlagName       string        `yaml:"-"`
	Split          string        `yaml:"split"`
	Use            string        `yaml:"use"`
	Default        any           `yaml:"default"`
	Max            *int          `yaml:"max"`
	Enum           []string      `yaml:"enum"`
}

type ResourceSchema struct {
	Fields map[string]SchemaField `yaml:"fields"`
}

type SchemaField struct {
	Type           string                 `yaml:"type"`
	Description    LocalizedText          `yaml:"description"`
	Required       bool                   `yaml:"required"`
	Repeatable     bool                   `yaml:"repeatable"`
	Positional     bool                   `yaml:"positional"`
	PositionalMany bool                   `yaml:"positional_many"`
	AllowEmpty     bool                   `yaml:"allow_empty"`
	InputStyle     string                 `yaml:"input_style"`
	Default        any                    `yaml:"default"`
	Max            *int                   `yaml:"max"`
	Enum           []string               `yaml:"enum"`
	Items          *SchemaField           `yaml:"items"`
	Fields         map[string]SchemaField `yaml:"fields"`
}

type Probe struct {
	API      string            `yaml:"api"`
	Request  map[string]any    `yaml:"request"`
	Response ProbeResponse     `yaml:"response"`
	Errors   map[string]string `yaml:"errors"`
}

type ProbeResponse struct {
	Items       string                `yaml:"items"`
	Item        string                `yaml:"item"`
	Total       string                `yaml:"total"`
	NextToken   string                `yaml:"next_token"`
	RequestID   string                `yaml:"request_id"`
	ID          string                `yaml:"id"`
	State       string                `yaml:"state"`
	Normalize   map[string]string     `yaml:"normalize"`
	Fields      map[string]ProbeField `yaml:"fields"`
	ExtraFields map[string]ProbeField `yaml:"extra_fields"`
	Absent      AbsentRule            `yaml:"absent"`
}

type AbsentRule struct {
	WhenEmptyForRequestedIDs bool `yaml:"when_empty_for_requested_ids"`
}

type ProbeField struct {
	Path              string                `yaml:"path"`
	From              string                `yaml:"from"`
	Each              map[string]ProbeField `yaml:"each"`
	Lower             string                `yaml:"lower"`
	First             []string              `yaml:"first"`
	Int               string                `yaml:"int"`
	Port              string                `yaml:"port"`
	DefaultEmptyArray bool                  `yaml:"default_empty_array"`
}

func (f *ProbeField) UnmarshalYAML(unmarshal func(any) error) error {
	var path string
	if err := unmarshal(&path); err == nil {
		*f = ProbeField{Path: path}
		return nil
	}
	type rawProbeField ProbeField
	var field rawProbeField
	if err := unmarshal(&field); err != nil {
		return err
	}
	*f = ProbeField(field)
	if f.Each == nil {
		f.Each = map[string]ProbeField{}
	}
	return nil
}

type Waiter struct {
	Probe    string          `yaml:"probe"`
	Target   string          `yaml:"target"`
	Interval string          `yaml:"interval"`
	Timeout  string          `yaml:"timeout"`
	Failure  WaiterFailure   `yaml:"failure"`
	Pending  []WaiterPending `yaml:"pending"`
	Match    WaiterMatch     `yaml:"match"`
}

type WaiterFailure struct {
	States []string `yaml:"states"`
}

type WaiterMatch struct {
	Capture          string            `yaml:"capture"`
	By               []string          `yaml:"by"`
	ProbeEachCapture bool              `yaml:"probe_each_capture"`
	Fields           map[string]string `yaml:"fields"`
	Contains         map[string]string `yaml:"contains"`
	Excludes         map[string]string `yaml:"excludes"`
}

type WaiterPending struct {
	Field  string   `yaml:"field"`
	Values []string `yaml:"values"`
}

type Transition struct {
	Call          string          `yaml:"-"`
	Request       map[string]any  `yaml:"-"`
	Idempotency   Idempotency     `yaml:"-"`
	Retry         TransitionRetry `yaml:"-"`
	IDFrom        string          `yaml:"-"`
	RequestIDFrom string          `yaml:"-"`
	Wait          string          `yaml:"-"`
}

type TransitionRetry struct {
	Policy          string   `yaml:"policy"`
	Errors          []string `yaml:"errors"`
	InitialInterval string   `yaml:"initial_interval"`
	MaxInterval     string   `yaml:"max_interval"`
	Timeout         string   `yaml:"timeout"`
	// When optionally gates the retry on a condition evaluated against the
	// execution context (e.g. "input.force"). When set and the condition is
	// falsy, the matched errors surface immediately instead of being retried.
	When string `yaml:"when"`
}

type Idempotency struct {
	Field  string `yaml:"field"`
	Prefix string `yaml:"prefix"`
}

func OperationIdempotency(resource ResourceSpec, operation Operation) (Idempotency, bool) {
	for _, step := range operation.Workflow {
		if step.Binding == "" {
			continue
		}
		binding, ok := resource.Bindings[step.Binding]
		if ok && binding.Idempotency.Field != "" {
			return binding.Idempotency, true
		}
	}
	return Idempotency{}, false
}

type Binding struct {
	API           string            `yaml:"api"`
	Each          string            `yaml:"each"`
	Capture       any               `yaml:"capture"`
	Hooks         BindingHooks      `yaml:"hooks"`
	Request       map[string]any    `yaml:"request"`
	Response      BindingResponse   `yaml:"response"`
	RequireAny    []Requirement     `yaml:"require_any"`
	RequireAll    []Requirement     `yaml:"require_all"`
	Idempotency   Idempotency       `yaml:"idempotency"`
	Retry         TransitionRetry   `yaml:"retry"`
	IDFrom        string            `yaml:"id_from"`
	RequestIDFrom string            `yaml:"request_id_from"`
	ContextFrom   map[string]string `yaml:"context_from"`
	Wait          string            `yaml:"wait"`
}

type BindingResponse struct {
	Items        string               `yaml:"items"`
	Status       string               `yaml:"status"`
	Success      []string             `yaml:"success"`
	ErrorCode    string               `yaml:"error_code"`
	ErrorMessage string               `yaml:"error_message"`
	RequestID    string               `yaml:"request_id"`
	Match        BindingResponseMatch `yaml:"match"`
}

type BindingResponseMatch struct {
	Capture string            `yaml:"capture"`
	By      []string          `yaml:"by"`
	Fields  map[string]string `yaml:"fields"`
}

type BindingHooks struct {
	Before     []string      `yaml:"before"`
	AfterError []string      `yaml:"after_error"`
	APICalls   []HookAPICall `yaml:"api_calls"`
}

type HookAPICall struct {
	Hook      string        `yaml:"hook"`
	API       string        `yaml:"api"`
	Phase     string        `yaml:"phase"`
	Condition string        `yaml:"condition"`
	Purpose   LocalizedText `yaml:"purpose"`
}

type Requirement struct {
	Request string `yaml:"request"`
	Raw     string `yaml:"raw"`
	Each    string `yaml:"each"`
}

type Operation struct {
	Description LocalizedText            `yaml:"description"`
	Help        LocalizedText            `yaml:"help"`
	Input       OperationInput           `yaml:"input"`
	RequireAny  []Requirement            `yaml:"require_any"`
	RequireWhen []ConditionalRequirement `yaml:"require_when"`
	Conflicts   []Conflict               `yaml:"conflicts"`
	Filters     map[string]Filter        `yaml:"filters"`
	Call        OperationCall            `yaml:"call"`
	Workflow    []WorkflowStep           `yaml:"workflow"`
	Emit        any                      `yaml:"emit"`
	Output      OperationOutput          `yaml:"output"`
	Examples    []string                 `yaml:"examples"`
	Aliases     []string                 `yaml:"aliases"`
}

type Conflict struct {
	Any     []string `yaml:"any"`
	WithAny []string `yaml:"with_any"`
}

type ConditionalRequirement struct {
	WhenAny    []string `yaml:"when_any"`
	RequireAny []string `yaml:"require_any"`
}

type OperationInput struct {
	Fields   OperationFields `yaml:"fields"`
	Controls OperationFields `yaml:"controls"`
}

type OperationFields []OperationFieldRef

type OperationFieldRef struct {
	Name           string
	Required       bool
	HasRequired    bool
	Repeatable     bool
	HasRepeatable  bool
	Positional     bool
	PositionalMany bool
	AllowEmpty     bool
	Brief          bool
	HasBrief       bool
	Schema         bool
	HasSchema      bool
	FlagName       string
	InputStyle     string
	Default        any
	Enum           []string
	Description    LocalizedText
}

type OperationOutput struct {
	Fields map[string]any `yaml:"fields"`
	Select []OutputSelect `yaml:"select"`
}

type OutputSelect struct {
	From                string   `yaml:"from"`
	Match               string   `yaml:"match"`
	By                  []string `yaml:"by"`
	Fields              []string `yaml:"fields"`
	SingleKey           string   `yaml:"single_key"`
	ManyKey             string   `yaml:"many_key"`
	First               bool     `yaml:"first"`
	UseMatchWhenMissing bool     `yaml:"use_match_when_missing"`
	FallbackToMatch     bool     `yaml:"-"`
}

type Filter struct {
	Target      string        `yaml:"target"`
	Type        string        `yaml:"type"`
	KeyPrefix   string        `yaml:"key_prefix"`
	Description LocalizedText `yaml:"description"`
}

type LocalizedText map[string]string

func (fields *OperationFields) UnmarshalYAML(unmarshal func(any) error) error {
	var values []any
	if err := unmarshal(&values); err != nil {
		return err
	}
	refs := make(OperationFields, 0, len(values))
	for index, value := range values {
		ref, err := operationFieldRefFromYAMLValue(value)
		if err != nil {
			return fmt.Errorf("operation input field %d: %w", index, err)
		}
		refs = append(refs, ref)
	}
	*fields = refs
	return nil
}

func operationFieldRefFromYAMLValue(value any) (OperationFieldRef, error) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return OperationFieldRef{}, fmt.Errorf("name is required")
		}
		return OperationFieldRef{Name: strings.TrimSpace(typed), Brief: true}, nil
	case map[string]any:
		if len(typed) != 1 {
			return OperationFieldRef{}, fmt.Errorf("expected a single field name")
		}
		for name, rawOptions := range typed {
			ref := OperationFieldRef{Name: strings.TrimSpace(name), Brief: true}
			if ref.Name == "" {
				return OperationFieldRef{}, fmt.Errorf("name is required")
			}
			if rawOptions == nil {
				return ref, nil
			}
			options, ok := rawOptions.(map[string]any)
			if !ok {
				return OperationFieldRef{}, fmt.Errorf("options for %q must be an object", ref.Name)
			}
			if err := applyOperationFieldOptions(&ref, options); err != nil {
				return OperationFieldRef{}, fmt.Errorf("%s: %w", ref.Name, err)
			}
			return ref, nil
		}
	}
	return OperationFieldRef{}, fmt.Errorf("expected field name or single-key object")
}

func applyOperationFieldOptions(ref *OperationFieldRef, options map[string]any) error {
	for key, value := range options {
		switch key {
		case "required":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("required must be a boolean")
			}
			ref.Required = parsed
			ref.HasRequired = true
		case "repeatable":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("repeatable must be a boolean")
			}
			ref.Repeatable = parsed
			ref.HasRepeatable = true
		case "positional":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("positional must be a boolean")
			}
			ref.Positional = parsed
		case "positional_many":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("positional_many must be a boolean")
			}
			ref.PositionalMany = parsed
		case "allow_empty":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("allow_empty must be a boolean")
			}
			ref.AllowEmpty = parsed
		case "brief":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("brief must be a boolean")
			}
			ref.Brief = parsed
			ref.HasBrief = true
		case "schema":
			parsed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("schema must be a boolean")
			}
			ref.Schema = parsed
			ref.HasSchema = true
		case "flag_name":
			parsed, ok := value.(string)
			if !ok {
				return fmt.Errorf("flag_name must be a string")
			}
			ref.FlagName = strings.TrimSpace(parsed)
			if ref.FlagName == "" {
				return fmt.Errorf("flag_name is required")
			}
		case "input_style":
			parsed, ok := value.(string)
			if !ok {
				return fmt.Errorf("input_style must be a string")
			}
			ref.InputStyle = parsed
		case "default":
			ref.Default = value
		case "enum":
			values, err := stringSliceFromAny(value)
			if err != nil {
				return fmt.Errorf("enum: %w", err)
			}
			ref.Enum = values
		case "description":
			text, err := localizedTextFromAny(value)
			if err != nil {
				return fmt.Errorf("description: %w", err)
			}
			ref.Description = text
		default:
			return fmt.Errorf("unsupported option %q", key)
		}
	}
	return nil
}

func stringSliceFromAny(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entries must be strings")
		}
		result = append(result, text)
	}
	return result, nil
}

func localizedTextFromAny(value any) (LocalizedText, error) {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil, nil
		}
		return LocalizedText{"en": typed}, nil
	case map[string]any:
		result := LocalizedText{}
		for key, value := range typed {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", key)
			}
			result[key] = text
		}
		if len(result) == 0 {
			return nil, nil
		}
		return result, nil
	default:
		return nil, fmt.Errorf("must be a string or language map")
	}
}

func (t *LocalizedText) UnmarshalYAML(unmarshal func(any) error) error {
	var text string
	if err := unmarshal(&text); err == nil {
		if text == "" {
			*t = nil
			return nil
		}
		*t = LocalizedText{"en": text}
		return nil
	}
	values := map[string]string{}
	if err := unmarshal(&values); err != nil {
		return err
	}
	if len(values) == 0 {
		*t = nil
		return nil
	}
	*t = LocalizedText(values)
	return nil
}

func (t LocalizedText) Text(lang string) string {
	if len(t) == 0 {
		return ""
	}
	if value := t[lang]; value != "" {
		return value
	}
	normalized := normalizeLanguageTag(lang)
	for key, value := range t {
		if value != "" && normalizeLanguageTag(key) == normalized {
			return value
		}
	}
	base := languageBase(normalized)
	if base != "" {
		for key, value := range t {
			if value != "" && languageBase(normalizeLanguageTag(key)) == base {
				return value
			}
		}
	}
	if value := t["en"]; value != "" {
		return value
	}
	keys := make([]string, 0, len(t))
	for key := range t {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return t[keys[0]]
}

func (t LocalizedText) String() string {
	return t.Text("en")
}

func normalizeLanguageTag(lang string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
}

func languageBase(lang string) string {
	if lang == "" {
		return ""
	}
	if index := strings.Index(lang, "-"); index >= 0 {
		return lang[:index]
	}
	return lang
}

type OperationCall struct {
	Probe    string   `yaml:"probe"`
	Many     bool     `yaml:"many"`
	IDs      []string `yaml:"ids"`
	NotFound string   `yaml:"not_found"`
}

type WorkflowStep struct {
	Binding    string   `yaml:"binding"`
	WaitUnless string   `yaml:"wait_unless"`
	Wait       string   `yaml:"wait"`
	Probe      string   `yaml:"probe"`
	Many       bool     `yaml:"many"`
	IDs        []string `yaml:"ids"`
	As         string   `yaml:"as"`
	NotFound   string   `yaml:"not_found"`
	When       string   `yaml:"when"`
	WhenAny    []string `yaml:"when_any"`
	Merge      bool     `yaml:"merge"`
	Append     bool     `yaml:"append"`
	Unless     string   `yaml:"unless"`
	Emit       any      `yaml:"emit"`
}

func Load(raw []byte) (ResourceSpec, error) {
	var loaded ResourceSpec
	err := yaml.UnmarshalWithOptions(raw, &loaded, yaml.Strict())
	loaded.ensureMaps()
	if err == nil {
		registerMessages(loaded.Messages)
	}
	return loaded, err
}

func LoadProductSpec(raw []byte) (ProductSpec, error) {
	var loaded ProductSpec
	err := yaml.UnmarshalWithOptions(raw, &loaded, yaml.Strict())
	return loaded, err
}

func registerMessages(messages map[string]LocalizedText) {
	for id, text := range messages {
		i18n.RegisterMessage(i18n.MessageSpec{ID: id, Text: map[string]string(text)})
	}
}

func LoadFile(path string) (ResourceSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ResourceSpec{}, err
	}
	return Load(raw)
}

func LoadResource(specDir, product, resource string) (ResourceSpec, error) {
	return LoadResourceWithParent(specDir, product, resource, "")
}

func LoadResourceWithParent(specDir, product, resource, parent string) (ResourceSpec, error) {
	if specDir == "" {
		entry, err := populateCache("")
		if err != nil {
			return ResourceSpec{}, err
		}
		if cached, ok := entry.resources[resourceKey{product: product, resource: resource, parent: parent}]; ok {
			return cached, nil
		}
		return ResourceSpec{}, os.ErrNotExist
	}

	resolved := resolveSpecDir(specDir)
	if entry := getCachedEntry(resolved); entry != nil {
		if cached, ok := entry.resources[resourceKey{product: product, resource: resource, parent: parent}]; ok {
			return cached, nil
		}
	}

	fileName := resource
	if parent != "" {
		fileName = parent + "-" + resource
	}
	relativePath := filepath.Join(product, fileName+".yaml")
	loaded, err := LoadFile(filepath.Join(specDir, relativePath))
	if err != nil {
		return ResourceSpec{}, err
	}
	if loaded.Product != product || loaded.Resource != resource {
		return ResourceSpec{}, fmt.Errorf("loaded %s/%s from %s/%s", loaded.Product, loaded.Resource, product, resource)
	}
	if err := Validate(loaded); err != nil {
		return ResourceSpec{}, err
	}
	return loaded, nil
}

func LoadProduct(specDir, product string) (ProductSpec, error) {
	if specDir == "" {
		entry, err := populateCache("")
		if err != nil {
			return ProductSpec{}, err
		}
		if cached, ok := entry.products[product]; ok {
			return cached, nil
		}
		return ProductSpec{}, os.ErrNotExist
	}

	resolved := resolveSpecDir(specDir)
	if entry := getCachedEntry(resolved); entry != nil {
		if cached, ok := entry.products[product]; ok {
			return cached, nil
		}
	}

	relativePath := filepath.Join(product, "product.yaml")
	loaded, err := LoadProductFile(filepath.Join(specDir, relativePath))
	if err != nil {
		return ProductSpec{}, err
	}
	if loaded.Product != product {
		return ProductSpec{}, fmt.Errorf("loaded product %s from %s", loaded.Product, product)
	}
	if err := ValidateProduct(loaded); err != nil {
		return ProductSpec{}, err
	}
	return loaded, nil
}

func LoadProductFile(path string) (ProductSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ProductSpec{}, err
	}
	return LoadProductSpec(raw)
}

func resolveSpecDir(specDir string) string {
	if specDir != "" {
		return specDir
	}
	if defaultDir, err := DefaultDir(); err == nil {
		return defaultDir
	}
	return ""
}

func populateCache(specDir string) (*specCacheEntry, error) {
	if entry := getCachedEntry(specDir); entry != nil {
		return entry, nil
	}

	if specDir == "" {
		entry := generatedCatalog()
		registerCatalogMessages(entry.resources)
		setCachedEntry("", entry)
		return entry, nil
	}

	entry := &specCacheEntry{
		resources: map[resourceKey]ResourceSpec{},
		products:  map[string]ProductSpec{},
	}
	refSet := map[ResourceRef]bool{}

	load := func(raw []byte) error {
		loaded, err := Load(raw)
		if err != nil {
			var product ProductSpec
			if perr := yaml.UnmarshalWithOptions(raw, &product, yaml.Strict()); perr == nil && product.Product != "" {
				if ValidateProduct(product) == nil {
					entry.products[product.Product] = product
				}
				return nil
			}
			return err
		}
		if loaded.Product != "" && loaded.Resource != "" {
			ref := ResourceRef{Product: loaded.Product, Resource: loaded.Resource, Parent: loaded.Parent}
			refSet[ref] = true
			if Validate(loaded) == nil {
				entry.resources[resourceKey{product: loaded.Product, resource: loaded.Resource, parent: loaded.Parent}] = loaded
			}
			return nil
		}
		if loaded.Product != "" && loaded.Resource == "" {
			var product ProductSpec
			if err := yaml.UnmarshalWithOptions(raw, &product, yaml.Strict()); err == nil {
				if ValidateProduct(product) == nil {
					entry.products[product.Product] = product
				}
			}
		}
		return nil
	}

	err := filepath.WalkDir(specDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return load(raw)
	})
	if err != nil {
		return nil, err
	}
	resources := make([]ResourceSpec, 0, len(entry.resources))
	for _, resource := range entry.resources {
		resources = append(resources, resource)
	}
	if err := ValidateSchemaPaths(resources); err != nil {
		return nil, err
	}

	entry.refs = sortedResourceRefs(refSet)
	setCachedEntry(specDir, entry)
	return entry, nil
}

func registerCatalogMessages(resources map[resourceKey]ResourceSpec) {
	for _, resource := range resources {
		registerMessages(resource.Messages)
	}
}

func ListResources(specDir string) ([]ResourceRef, error) {
	resolved := specDir
	if specDir != "" {
		resolved = resolveSpecDir(specDir)
	}
	entry, err := populateCache(resolved)
	if err != nil {
		return nil, err
	}
	return entry.refs, nil
}

func sortedResourceRefs(values map[ResourceRef]bool) []ResourceRef {
	refs := make([]ResourceRef, 0, len(values))
	for ref := range values {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Product != refs[j].Product {
			return refs[i].Product < refs[j].Product
		}
		return refs[i].Resource < refs[j].Resource
	})
	return refs
}

func DefaultDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		goMod := filepath.Join(wd, "go.mod")
		if info, err := os.Stat(goMod); err == nil && !info.IsDir() {
			return filepath.Join(wd, "specs"), nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("go.mod not found")
		}
		wd = parent
	}
}

func Validate(spec ResourceSpec) error {
	if spec.SchemaVersion == 0 {
		return fmt.Errorf("schema_version is required")
	}
	if spec.SchemaVersion != 2 {
		return fmt.Errorf("schema_version must be 2")
	}
	if spec.Product == "" {
		return fmt.Errorf("product is required")
	}
	if spec.Resource == "" {
		return fmt.Errorf("resource is required")
	}
	if spec.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if len(spec.Schema.Fields) == 0 {
		return fmt.Errorf("schema.fields is required")
	}
	for name, field := range spec.Schema.Fields {
		if err := validateSchemaField(field); err != nil {
			return fmt.Errorf("schema field %q: %w", name, err)
		}
	}
	for name, control := range spec.Controls {
		if err := validateSchemaField(control); err != nil {
			return fmt.Errorf("control %q: %w", name, err)
		}
	}

	for name, probe := range spec.Probes {
		if probe.API == "" {
			return fmt.Errorf("probe %q api is required", name)
		}
		if probe.Response.Items != "" && probe.Response.Item != "" {
			return fmt.Errorf("probe %q cannot set both response.items and response.item", name)
		}
		for fieldName, field := range probe.Response.Fields {
			if err := validateProbeField(field); err != nil {
				return fmt.Errorf("probe %q response field %q: %w", name, fieldName, err)
			}
		}
		for fieldName, field := range probe.Response.ExtraFields {
			if err := validateProbeField(field); err != nil {
				return fmt.Errorf("probe %q response extra field %q: %w", name, fieldName, err)
			}
		}
	}
	for name, waiter := range spec.Waiters {
		if waiter.Probe == "" {
			return fmt.Errorf("waiter %q probe is required", name)
		}
		if _, ok := spec.Probes[waiter.Probe]; !ok {
			return fmt.Errorf("waiter %q references unknown probe %q", name, waiter.Probe)
		}
		if waiter.Target == "" {
			return fmt.Errorf("waiter %q target is required", name)
		}
		if waiter.Match.Capture != "" && len(waiter.Match.By) == 0 {
			return fmt.Errorf("waiter %q match by is required", name)
		}
		if waiter.Match.ProbeEachCapture && waiter.Match.Capture == "" {
			return fmt.Errorf("waiter %q match probe_each_capture requires capture", name)
		}
		if waiter.Match.Capture != "" && (len(waiter.Match.Fields) > 0 || len(waiter.Match.Contains) > 0 || len(waiter.Match.Excludes) > 0) {
			return fmt.Errorf("waiter %q match capture cannot be combined with field expressions", name)
		}
		for kind, fields := range map[string]map[string]string{
			"fields": waiter.Match.Fields, "contains": waiter.Match.Contains, "excludes": waiter.Match.Excludes,
		} {
			for field, expr := range fields {
				if strings.TrimSpace(field) == "" || strings.TrimSpace(expr) == "" {
					return fmt.Errorf("waiter %q match %s requires non-empty field and expression", name, kind)
				}
			}
		}
		for i, pending := range waiter.Pending {
			if pending.Field == "" {
				return fmt.Errorf("waiter %q pending entry %d field is required", name, i)
			}
			if len(pending.Values) == 0 {
				return fmt.Errorf("waiter %q pending entry %d values are required", name, i)
			}
		}
	}
	for name, binding := range spec.Bindings {
		if binding.API == "" {
			return fmt.Errorf("binding %q api is required", name)
		}
		if binding.Request == nil {
			return fmt.Errorf("binding %q request is required", name)
		}
		requestCaptures, err := bindingRequestCaptureFields(binding.Request)
		if err != nil {
			return fmt.Errorf("binding %q request capture: %w", name, err)
		}
		if binding.Response.Items != "" {
			if !validResponsePath(binding.Response.Items) {
				return fmt.Errorf("binding %q response items path %q is invalid", name, binding.Response.Items)
			}
			if binding.Response.Status == "" {
				return fmt.Errorf("binding %q response status is required when response items is set", name)
			}
			if !validResponsePath(binding.Response.Status) {
				return fmt.Errorf("binding %q response status path %q is invalid", name, binding.Response.Status)
			}
			if len(binding.Response.Success) == 0 {
				return fmt.Errorf("binding %q response success is required when response items is set", name)
			}
			for _, success := range binding.Response.Success {
				if strings.TrimSpace(success) == "" {
					return fmt.Errorf("binding %q response success entries must not be empty", name)
				}
			}
			if binding.Response.ErrorCode == "" {
				return fmt.Errorf("binding %q response error_code is required when response items is set", name)
			}
			if !validResponsePath(binding.Response.ErrorCode) {
				return fmt.Errorf("binding %q response error_code path %q is invalid", name, binding.Response.ErrorCode)
			}
			if binding.Response.ErrorMessage == "" {
				return fmt.Errorf("binding %q response error_message is required when response items is set", name)
			}
			if !validResponsePath(binding.Response.ErrorMessage) {
				return fmt.Errorf("binding %q response error_message path %q is invalid", name, binding.Response.ErrorMessage)
			}
			if binding.Response.RequestID != "" && !validResponsePath(binding.Response.RequestID) {
				return fmt.Errorf("binding %q response request_id path %q is invalid", name, binding.Response.RequestID)
			}
			if binding.Response.Match.Capture != "" {
				if len(binding.Response.Match.By) == 0 {
					return fmt.Errorf("binding %q response match by is required when capture is set", name)
				}
				captureFields, ok := requestCaptures[binding.Response.Match.Capture]
				if !ok {
					return fmt.Errorf("binding %q response match capture %q is not declared in the binding request", name, binding.Response.Match.Capture)
				}
				for _, field := range binding.Response.Match.By {
					if !validResponsePath(binding.Response.Match.Fields[field]) {
						return fmt.Errorf("binding %q response match field %q is required", name, field)
					}
					if !captureFields[field] {
						return fmt.Errorf("binding %q response match field %q is not declared by capture %q", name, field, binding.Response.Match.Capture)
					}
				}
				for field, path := range binding.Response.Match.Fields {
					if !validResponsePath(path) {
						return fmt.Errorf("binding %q response match field %q path %q is invalid", name, field, path)
					}
				}
			} else if len(binding.Response.Match.By) > 0 || len(binding.Response.Match.Fields) > 0 {
				return fmt.Errorf("binding %q response match capture is required when match is configured", name)
			}
		} else if binding.Response.Status != "" ||
			len(binding.Response.Success) > 0 ||
			binding.Response.ErrorCode != "" ||
			binding.Response.ErrorMessage != "" ||
			binding.Response.RequestID != "" ||
			binding.Response.Match.Capture != "" ||
			len(binding.Response.Match.By) > 0 ||
			len(binding.Response.Match.Fields) > 0 {
			return fmt.Errorf("binding %q response items is required when response validation is configured", name)
		}
		if binding.Idempotency.Field == "" && binding.Idempotency.Prefix != "" {
			return fmt.Errorf("binding %q idempotency prefix requires field", name)
		}
		if binding.Idempotency.Field != "" && binding.Idempotency.Prefix == "" {
			return fmt.Errorf("binding %q idempotency field requires prefix", name)
		}
		if err := validateRetry(binding.Retry); err != nil {
			return fmt.Errorf("binding %q: %w", name, err)
		}
		if binding.Wait != "" {
			if _, ok := spec.Waiters[binding.Wait]; !ok {
				return fmt.Errorf("binding %q references unknown waiter %q", name, binding.Wait)
			}
		}
		attachedHooks := make(map[string]bool, len(binding.Hooks.Before))
		for _, hook := range binding.Hooks.Before {
			attachedHooks[hook] = true
		}
		for i, call := range binding.Hooks.APICalls {
			if call.Hook == "" {
				return fmt.Errorf("binding %q hook api call %d hook is required", name, i)
			}
			if !attachedHooks[call.Hook] {
				return fmt.Errorf("binding %q hook api call %d references unattached before hook %q", name, i, call.Hook)
			}
			if call.API == "" {
				return fmt.Errorf("binding %q hook api call %d api is required", name, i)
			}
			if call.Phase != "preflight" {
				return fmt.Errorf("binding %q hook api call %d phase %q is not supported", name, i, call.Phase)
			}
			if len(call.Purpose) == 0 || strings.TrimSpace(call.Purpose.Text("en")) == "" {
				return fmt.Errorf("binding %q hook api call %d purpose is required", name, i)
			}
		}
		for i, requirement := range binding.RequireAny {
			if requirement.Request == "" && requirement.Raw == "" && requirement.Each == "" {
				return fmt.Errorf("binding %q require_any entry %d target is required", name, i)
			}
		}
		for i, requirement := range binding.RequireAll {
			if requirement.Request == "" && requirement.Raw == "" && requirement.Each == "" {
				return fmt.Errorf("binding %q require_all entry %d target is required", name, i)
			}
		}
	}
	for name, operation := range spec.Operations {
		inputs := operationInputNames(operation)
		for i, requirement := range operation.RequireAny {
			if requirement.Request == "" && requirement.Raw == "" && requirement.Each == "" {
				return fmt.Errorf("operation %q require_any entry %d target is required", name, i)
			}
		}
		for i, requirement := range operation.RequireWhen {
			if len(requirement.WhenAny) == 0 || len(requirement.RequireAny) == 0 {
				return fmt.Errorf("operation %q require_when entry %d requires when_any and require_any", name, i)
			}
			for _, field := range append(append([]string{}, requirement.WhenAny...), requirement.RequireAny...) {
				if !inputs[field] {
					return fmt.Errorf("operation %q require_when entry %d references unknown input %q", name, i, field)
				}
			}
		}
		seenFlags := map[string]string{}
		for _, field := range append(append(OperationFields{}, operation.Input.Fields...), operation.Input.Controls...) {
			flag := operationFieldCLIFlagName(field)
			if previous, ok := seenFlags[flag]; ok {
				return fmt.Errorf("operation %q uses duplicate flag name %q for %q and %q", name, flag, previous, field.Name)
			}
			seenFlags[flag] = field.Name
		}
		for i, conflict := range operation.Conflicts {
			if len(conflict.Any) == 0 || len(conflict.WithAny) == 0 {
				return fmt.Errorf("operation %q conflicts entry %d requires any and with_any", name, i)
			}
			for _, field := range append(append([]string{}, conflict.Any...), conflict.WithAny...) {
				if !inputs[field] {
					return fmt.Errorf("operation %q conflicts entry %d references unknown input %q", name, i, field)
				}
			}
		}
		if operation.Call.Probe != "" {
			if _, ok := spec.Probes[operation.Call.Probe]; !ok {
				return fmt.Errorf("operation %q references unknown probe %q", name, operation.Call.Probe)
			}
		}
		for _, field := range operation.Input.Fields {
			if _, ok := spec.Schema.Fields[field.Name]; !ok {
				return fmt.Errorf("operation %q references unknown schema field %q", name, field.Name)
			}
		}
		for _, control := range operation.Input.Controls {
			if _, ok := spec.Controls[control.Name]; !ok {
				return fmt.Errorf("operation %q references unknown control %q", name, control.Name)
			}
		}
		for filterName, filter := range operation.Filters {
			if filter.Target == "" {
				return fmt.Errorf("operation %q filter %q target is required", name, filterName)
			}
			_, fieldOK := spec.Schema.Fields[filter.Target]
			_, controlOK := spec.Controls[filter.Target]
			if !fieldOK && !controlOK && filter.Type == "" {
				return fmt.Errorf("operation %q filter %q references unknown input %q", name, filterName, filter.Target)
			}
		}
		for i, step := range operation.Workflow {
			if step.Binding != "" {
				if _, ok := spec.Bindings[step.Binding]; !ok {
					return fmt.Errorf("operation %q workflow step %d references unknown binding %q", name, i, step.Binding)
				}
			}
			if step.Wait != "" {
				if _, ok := spec.Waiters[step.Wait]; !ok {
					return fmt.Errorf("operation %q workflow step %d references unknown waiter %q", name, i, step.Wait)
				}
			}
			if step.Probe != "" {
				if _, ok := spec.Probes[step.Probe]; !ok {
					return fmt.Errorf("operation %q workflow step %d references unknown probe %q", name, i, step.Probe)
				}
			}
		}
		for i, selectSpec := range operation.Output.Select {
			if selectSpec.From == "" {
				return fmt.Errorf("operation %q output select %d from is required", name, i)
			}
			if selectSpec.SingleKey == "" && selectSpec.ManyKey == "" {
				return fmt.Errorf("operation %q output select %d single_key or many_key is required", name, i)
			}
			if selectSpec.Match != "" && len(selectSpec.By) == 0 {
				return fmt.Errorf("operation %q output select %d by is required when match is set", name, i)
			}
		}
		// Mutating operations must declare at least one example so
		// `ecctl examples <product>.<resource>.<action>` always returns a
		// pasteable invocation. Checked last so other targeted operation
		// errors (require_*/conflicts/workflow/...) surface first.
		if isMutatingActionName(name) && len(operation.Examples) == 0 {
			return fmt.Errorf("operation %q is mutating and must declare at least one example", name)
		}
	}
	return nil
}

// ValidateSchemaPaths rejects resource layouts whose dotted schema IDs would
// have two meanings. A nested resource product.parent.resource shares its
// three-segment shape with a top-level command product.resource.action, so the
// child resource name must not also be an action on its parent resource.
func ValidateSchemaPaths(resources []ResourceSpec) error {
	topLevel := make(map[string]ResourceSpec, len(resources))
	for _, resource := range resources {
		if resource.Parent == "" {
			topLevel[resource.Product+"."+resource.Resource] = resource
		}
	}
	for _, resource := range resources {
		if resource.Parent == "" {
			continue
		}
		parent, ok := topLevel[resource.Product+"."+resource.Parent]
		if !ok {
			continue
		}
		if _, collides := parent.Operations[resource.Resource]; collides {
			return fmt.Errorf("schema ID %q is ambiguous: nested resource conflicts with action %q on parent resource %q",
				resource.Product+"."+resource.Parent+"."+resource.Resource, resource.Resource, resource.Parent)
		}
	}
	return nil
}

func operationInputNames(operation Operation) map[string]bool {
	names := map[string]bool{}
	for _, field := range append(append(OperationFields{}, operation.Input.Fields...), operation.Input.Controls...) {
		names[field.Name] = true
	}
	if names["filter"] {
		for _, filter := range operation.Filters {
			if filter.Target != "" {
				names[filter.Target] = true
			}
		}
	}
	return names
}

// mutatingActionNames lists the operation names treated as state-changing.
// Spec lint requires every mutating operation to declare at least one example
// so that `ecctl examples <product>.<resource>.<action>` always returns a
// pasteable invocation for the actions agents most often need to discover.
var mutatingActionNames = map[string]bool{
	"create":    true,
	"update":    true,
	"delete":    true,
	"authorize": true,
	"revoke":    true,
	"start":     true,
	"stop":      true,
	"reboot":    true,
	"restart":   true,
	"attach":    true,
	"detach":    true,
	"invoke":    true,
	"upgrade":   true,
	"apply":     true,
	"cancel":    true,
	"clone":     true,
	"copy":      true,
	"disable":   true,
	"enable":    true,
	"exec":      true,
	"export":    true,
	"import":    true,
	"install":   true,
	"pause":     true,
	"reinit":    true,
	"remove":    true,
	"renew":     true,
	"repair":    true,
	"reset":     true,
	"resume":    true,
	"sendfile":  true,
}

func isMutatingActionName(name string) bool {
	return mutatingActionNames[name]
}

func operationFieldCLIFlagName(field OperationFieldRef) string {
	name := field.Name
	if field.FlagName != "" {
		name = field.FlagName
	}
	return strings.ReplaceAll(name, "_", "-")
}

func validateSchemaField(field SchemaField) error {
	if field.Type == "" {
		return fmt.Errorf("type is required")
	}
	switch field.Type {
	case "string", "integer", "number", "boolean", "duration", "cidr", "key_value", "string_array":
		if field.Items != nil {
			return fmt.Errorf("items is only supported for array fields")
		}
	case "array":
		if field.Items == nil {
			return fmt.Errorf("array items is required")
		}
		if err := validateSchemaField(*field.Items); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	case "object":
		if field.Items != nil {
			return fmt.Errorf("object fields cannot set items")
		}
		for name, child := range field.Fields {
			if err := validateSchemaField(child); err != nil {
				return fmt.Errorf("field %q: %w", name, err)
			}
		}
	default:
		return fmt.Errorf("unsupported type %q", field.Type)
	}
	return nil
}

func bindingRequestCaptureFields(request map[string]any) (map[string]map[string]bool, error) {
	captures := map[string]map[string]bool{}
	var walkFields func(map[string]any, bool) error
	var walkNode func(any, bool) error
	walkFields = func(fields map[string]any, insideEach bool) error {
		for _, node := range fields {
			if err := walkNode(node, insideEach); err != nil {
				return err
			}
		}
		return nil
	}
	walkNode = func(node any, insideEach bool) error {
		typed, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		if _, ok := typed["raw"].(string); ok {
			if _, hasCapture := typed["capture"]; hasCapture {
				return fmt.Errorf("capture is only supported on an each request node")
			}
			return nil
		}
		if _, ok := typed["from"].(string); ok {
			if _, hasCapture := typed["capture"]; hasCapture {
				return fmt.Errorf("capture is only supported on an each request node")
			}
			fields, _ := typed["fields"].(map[string]any)
			return walkFields(fields, insideEach)
		}
		if _, hasEach := typed["each"]; hasEach {
			if rawCapture, hasCapture := typed["capture"]; hasCapture {
				if insideEach {
					return fmt.Errorf("capture-bearing each request nodes cannot be nested")
				}
				name, fields, err := bindingRequestCapture(rawCapture)
				if err != nil {
					return err
				}
				if _, exists := captures[name]; exists {
					return fmt.Errorf("capture %q is declared more than once", name)
				}
				captures[name] = fields
			}
			fields, _ := typed["fields"].(map[string]any)
			return walkFields(fields, true)
		}
		if _, hasCapture := typed["capture"]; hasCapture {
			return fmt.Errorf("capture is only supported on an each request node")
		}
		return walkFields(typed, insideEach)
	}
	if err := walkFields(request, false); err != nil {
		return nil, err
	}
	return captures, nil
}

func validResponsePath(path string) bool {
	if path == "$" {
		return true
	}
	if strings.TrimSpace(path) != path || !strings.HasPrefix(path, "$.") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if part == "" || strings.ContainsAny(part, "[]") {
			return false
		}
	}
	return true
}

func bindingRequestCapture(raw any) (string, map[string]bool, error) {
	switch capture := raw.(type) {
	case string:
		if strings.TrimSpace(capture) == "" {
			return "", nil, fmt.Errorf("capture name must not be empty")
		}
		return capture, map[string]bool{}, nil
	case map[string]any:
		name, _ := capture["name"].(string)
		if strings.TrimSpace(name) == "" {
			return "", nil, fmt.Errorf("capture name must not be empty")
		}
		fields := map[string]bool{}
		if rawFields, configured := capture["fields"]; configured {
			fieldMappings, ok := rawFields.(map[string]any)
			if !ok {
				return "", nil, fmt.Errorf("capture %q fields must be an object", name)
			}
			for field, rawExpr := range fieldMappings {
				expr, ok := rawExpr.(string)
				if !ok || strings.TrimSpace(expr) == "" {
					return "", nil, fmt.Errorf("capture %q field %q must be a non-empty expression", name, field)
				}
				fields[field] = true
			}
		}
		return name, fields, nil
	default:
		return "", nil, fmt.Errorf("capture must be a name or object")
	}
}

func validateRetry(retry TransitionRetry) error {
	if retry.Policy != "" && retry.Policy != "initializing_grace" {
		return fmt.Errorf("retry policy %q is not supported", retry.Policy)
	}
	return nil
}

func ValidateProduct(spec ProductSpec) error {
	if spec.SchemaVersion == 0 {
		return fmt.Errorf("schema_version is required")
	}
	if spec.Product == "" {
		return fmt.Errorf("product is required")
	}
	if spec.Description.Text("en") == "" {
		return fmt.Errorf("description is required")
	}
	if len(spec.Examples) < 2 || len(spec.Examples) > 4 {
		return fmt.Errorf("examples must contain 2 to 4 entries")
	}
	return nil
}

func validateProbeField(field ProbeField) error {
	hasScalar := field.Path != "" || field.Lower != "" || len(field.First) > 0 || field.Int != "" || field.Port != ""
	if field.From != "" {
		if len(field.Each) == 0 {
			return fmt.Errorf("each is required when from is set")
		}
		for name, child := range field.Each {
			if err := validateProbeField(child); err != nil {
				return fmt.Errorf("each field %q: %w", name, err)
			}
		}
		return nil
	}
	if len(field.Each) > 0 {
		return fmt.Errorf("from is required when each is set")
	}
	if !hasScalar {
		return fmt.Errorf("mapping expression is required")
	}
	return nil
}

func (spec *ResourceSpec) ensureMaps() {
	if spec.Schema.Fields == nil {
		spec.Schema.Fields = map[string]SchemaField{}
	}
	if spec.Controls == nil {
		spec.Controls = map[string]SchemaField{}
	}
	if spec.Bindings == nil {
		spec.Bindings = map[string]Binding{}
	}
	if spec.Operations == nil {
		spec.Operations = map[string]Operation{}
	}
	if spec.Probes == nil {
		spec.Probes = map[string]Probe{}
	}
	if spec.Waiters == nil {
		spec.Waiters = map[string]Waiter{}
	}
	if spec.Messages == nil {
		spec.Messages = map[string]LocalizedText{}
	}

	for name, probe := range spec.Probes {
		if probe.Request == nil {
			probe.Request = map[string]any{}
		}
		if probe.Response.Normalize == nil {
			probe.Response.Normalize = map[string]string{}
		}
		if probe.Response.Fields == nil {
			probe.Response.Fields = map[string]ProbeField{}
		}
		if probe.Response.ExtraFields == nil {
			probe.Response.ExtraFields = map[string]ProbeField{}
		}
		for fieldName, field := range probe.Response.Fields {
			if field.Each == nil {
				field.Each = map[string]ProbeField{}
			}
			probe.Response.Fields[fieldName] = field
		}
		for fieldName, field := range probe.Response.ExtraFields {
			if field.Each == nil {
				field.Each = map[string]ProbeField{}
			}
			probe.Response.ExtraFields[fieldName] = field
		}
		if probe.Errors == nil {
			probe.Errors = map[string]string{}
		}
		spec.Probes[name] = probe
	}
	for name, binding := range spec.Bindings {
		if binding.Request == nil {
			binding.Request = map[string]any{}
		}
		spec.Bindings[name] = binding
	}
	for name, operation := range spec.Operations {
		if operation.Filters == nil {
			operation.Filters = map[string]Filter{}
		}
		if operation.Output.Fields == nil {
			operation.Output.Fields = map[string]any{}
		}
		for i, selectSpec := range operation.Output.Select {
			selectSpec.FallbackToMatch = selectSpec.UseMatchWhenMissing
			operation.Output.Select[i] = selectSpec
		}
		spec.Operations[name] = operation
	}
}
