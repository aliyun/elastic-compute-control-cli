package coverage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

const (
	RegistryVersion = 3

	StatusOffline  = "offline"
	StatusLivePass = "live-pass"

	ReasonLiveVerified = "live-verified"
	ReasonNotRun       = "not-run"
	ReasonCaseChanged  = "case-changed"
	ReasonPrerequisite = "prerequisite"
	ReasonTestFailed   = "test-failed"
	ReasonUnknown      = "unknown"
)

const (
	legacyStatusOfflineValid = "offline-valid"
)

var allowedStatuses = map[string]bool{
	StatusOffline:  true,
	StatusLivePass: true,
}

var allowedOfflineReasons = map[string]bool{
	ReasonNotRun:       true,
	ReasonCaseChanged:  true,
	ReasonPrerequisite: true,
	ReasonTestFailed:   true,
	ReasonUnknown:      true,
}

// Registry is e2e/coverage.yaml.
type Registry struct {
	Version   int                        `yaml:"version" json:"version"`
	Generated RegistryGenerated          `yaml:"generated,omitempty" json:"generated,omitempty"`
	Summary   RegistryPublicSummary      `yaml:"summary" json:"summary"`
	Resources map[string]RegistryProduct `yaml:"resources" json:"resources"`
}

type RegistryGenerated struct {
	SpecsDir string `yaml:"specs_dir,omitempty" json:"specs_dir,omitempty"`
	CasesDir string `yaml:"cases_dir,omitempty" json:"cases_dir,omitempty"`
}

// RegistryProduct maps canonical resource names from specs to their coverage.
// The product itself is the parent key in Registry.Resources.
type RegistryProduct map[string]RegistryResource

type RegistryResource struct {
	Operations map[string]RegistryOperation `yaml:"operations" json:"operations"`
}

// RegistryOperation is the fixed version 3 operation schema. All fields are
// required; operations without a case are omitted from the registry.
type RegistryOperation struct {
	Status      string `yaml:"status" json:"status"`
	Case        string `yaml:"case" json:"case"`
	Fingerprint string `yaml:"fingerprint" json:"fingerprint"`
	Time        string `yaml:"time" json:"time"`
	Reason      string `yaml:"reason" json:"reason"`
}

// RegistryPublicSummary records completion counts for the public CLI surface,
// including operations omitted from Resources because they have no case.
type RegistryPublicSummary struct {
	Surface      string `yaml:"surface" json:"surface"`
	Resources    int    `yaml:"resources" json:"resources"`
	Operations   int    `yaml:"operations" json:"operations"`
	MissingCases int    `yaml:"missing_cases" json:"missing_cases"`
	Passed       int    `yaml:"passed" json:"passed"`
	NotPassed    int    `yaml:"not_passed" json:"not_passed"`
}

// PublicSurface is a capability snapshot supplied by ecctl-public.
type PublicSurface struct {
	ResourceCount int
	Capabilities  map[Capability]bool
}

type RegistryCheckOptions struct {
	FailOnNotLive    bool
	ResourceFilter   map[string]bool
	CapabilityFilter map[Capability]bool
	PublicSurface    *PublicSurface
}

type RegistryCheckReport struct {
	Declared int                       `json:"declared"`
	Entries  int                       `json:"entries"`
	Invalid  int                       `json:"invalid"`
	ByStatus map[string]int            `json:"by_status"`
	Errors   []RegistryValidationError `json:"errors"`
}

type RegistryValidationError struct {
	Resource  string `json:"resource"`
	Operation string `json:"operation"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type RegistrySummary struct {
	Entries  int            `json:"entries"`
	ByStatus map[string]int `json:"by_status"`
}

type coverageInfo struct {
	Case          string
	Steps         []string
	SuiteResource string
	Fingerprint   string
}

func (s *RegistryPublicSummary) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("summary must be a mapping")
	}
	required := map[string]bool{
		"surface":       false,
		"resources":     false,
		"operations":    false,
		"missing_cases": false,
		"passed":        false,
		"not_passed":    false,
	}
	for i := 0; i < len(node.Content); i += 2 {
		field := node.Content[i].Value
		seen, ok := required[field]
		if !ok {
			return fmt.Errorf("unknown summary field %q", field)
		}
		if seen {
			return fmt.Errorf("duplicate summary field %q", field)
		}
		required[field] = true
	}
	for field, seen := range required {
		if !seen {
			return fmt.Errorf("summary requires field %q", field)
		}
	}
	type plain RegistryPublicSummary
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*s = RegistryPublicSummary(decoded)
	return nil
}

type legacyRegistry struct {
	Version   int                               `yaml:"version"`
	Generated RegistryGenerated                 `yaml:"generated,omitempty"`
	Resources map[string]legacyRegistryResource `yaml:"resources"`
}

// registryV2 is the former product/resource-keyed schema. It is accepted only
// by registry init so status evidence can be migrated to the nested v3 shape.
type registryV2 struct {
	Version   int                         `yaml:"version"`
	Generated RegistryGenerated           `yaml:"generated,omitempty"`
	Summary   RegistryPublicSummary       `yaml:"summary"`
	Resources map[string]RegistryResource `yaml:"resources"`
}

type legacyRegistryResource struct {
	Operations map[string]legacyRegistryOperation `yaml:"operations"`
}

type legacyRegistryOperation struct {
	Status       string                 `yaml:"status"`
	Case         string                 `yaml:"case,omitempty"`
	Steps        []string               `yaml:"steps,omitempty"`
	Reason       string                 `yaml:"reason,omitempty"`
	ReviewAfter  string                 `yaml:"review_after,omitempty"`
	Owner        string                 `yaml:"owner,omitempty"`
	TargetPhase  int                    `yaml:"target_phase,omitempty"`
	Issue        string                 `yaml:"issue,omitempty"`
	LastChecked  string                 `yaml:"last_checked,omitempty"`
	RemovedAfter string                 `yaml:"removed_after,omitempty"`
	Evidence     legacyRegistryEvidence `yaml:"evidence,omitempty"`
}

type legacyRegistryEvidence struct {
	CoverageSource string `yaml:"coverage_source,omitempty"`
	Report         string `yaml:"report,omitempty"`
	RunID          string `yaml:"run_id,omitempty"`
	VerifiedAt     string `yaml:"verified_at,omitempty"`
}

// LoadRegistryFile loads a strict version 3 registry. Unknown and legacy
// fields are rejected so every consumer observes the same fixed schema.
func LoadRegistryFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeRegistryV3(data)
}

// LoadRegistryForInit loads the current version 3 schema or a legacy flat
// schema used only by coverage registry init during migration.
func LoadRegistryForInit(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var header struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Version {
	case RegistryVersion:
		return decodeRegistryV3(data)
	case 2:
		return decodeRegistryV2(data)
	case 1:
		return decodeLegacyRegistry(data)
	default:
		return nil, fmt.Errorf("unsupported registry version %d", header.Version)
	}
}

func decodeRegistryV3(data []byte) (*Registry, error) {
	var reg Registry
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&reg); err != nil {
		return nil, err
	}
	if err := requireYAMLEOF(dec); err != nil {
		return nil, err
	}
	if reg.Version != RegistryVersion {
		return nil, fmt.Errorf("registry version must be %d", RegistryVersion)
	}
	if reg.Resources == nil {
		reg.Resources = map[string]RegistryProduct{}
	}
	return &reg, nil
}

func decodeRegistryV2(data []byte) (*Registry, error) {
	var old registryV2
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&old); err != nil {
		return nil, err
	}
	if err := requireYAMLEOF(dec); err != nil {
		return nil, err
	}
	if old.Version != 2 {
		return nil, fmt.Errorf("legacy registry version must be 2")
	}
	reg := &Registry{
		Version:   2,
		Generated: old.Generated,
		Summary:   old.Summary,
		Resources: map[string]RegistryProduct{},
	}
	for resourceKey, resource := range old.Resources {
		if err := setRegistryResource(reg.Resources, resourceKey, resource); err != nil {
			return nil, fmt.Errorf("migrate registry v2 resource %q: %w", resourceKey, err)
		}
	}
	return reg, nil
}

func decodeLegacyRegistry(data []byte) (*Registry, error) {
	var legacy legacyRegistry
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&legacy); err != nil {
		return nil, err
	}
	if err := requireYAMLEOF(dec); err != nil {
		return nil, err
	}
	if legacy.Version != 1 {
		return nil, fmt.Errorf("legacy registry version must be 1")
	}
	reg := &Registry{
		Version:   1,
		Generated: legacy.Generated,
		Resources: map[string]RegistryProduct{},
	}
	for resource, legacyResource := range legacy.Resources {
		operations := make(map[string]RegistryOperation, len(legacyResource.Operations))
		for operation, legacyOperation := range legacyResource.Operations {
			operations[operation] = RegistryOperation{
				Status: legacyOperation.Status,
				Case:   legacyOperation.Case,
				Time:   legacyOperation.Evidence.VerifiedAt,
			}
		}
		if err := setRegistryResource(reg.Resources, resource, RegistryResource{Operations: operations}); err != nil {
			return nil, fmt.Errorf("migrate registry v1 resource %q: %w", resource, err)
		}
	}
	return reg, nil
}

func requireYAMLEOF(dec *yaml.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("registry must contain exactly one YAML document")
	}
	return nil
}

// WriteRegistryFile writes a coverage registry with stable YAML ordering.
func WriteRegistryFile(path string, reg *Registry) error {
	if reg == nil {
		return fmt.Errorf("registry must not be nil")
	}
	if reg.Version != RegistryVersion {
		return fmt.Errorf("registry version must be %d before writing", RegistryVersion)
	}
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(reg); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// InitRegistry creates or refreshes a version 3 registry from the declared
// specs and case commands.
func InitRegistry(specsDir, casesDir string, existing *Registry, public PublicSurface) (*Registry, error) {
	return initRegistry(specsDir, casesDir, existing, public, time.Now())
}

func initRegistry(specsDir, casesDir string, existing *Registry, public PublicSurface, now time.Time) (*Registry, error) {
	declared, index, err := loadDeclared(specsDir)
	if err != nil {
		return nil, err
	}
	covered, err := loadCoveredDetails(casesDir, index)
	if err != nil {
		return nil, err
	}
	reg := &Registry{
		Version:   RegistryVersion,
		Generated: RegistryGenerated{SpecsDir: specsDir, CasesDir: casesDir},
		Resources: map[string]RegistryProduct{},
	}
	timestamp := now.Format(time.RFC3339Nano)
	for _, cap := range sortedCapabilities(declared) {
		info, isCovered := covered[cap]
		if !isCovered {
			continue
		}
		entry := newOfflineEntry(info, timestamp, ReasonNotRun)
		if previous, ok := existingEntry(existing, cap); ok {
			sameCase := sameCoverageCasePath(existing, previous.Case, info.Case, casesDir)
			switch existing.Version {
			case RegistryVersion, 2:
				if sameCase && previous.Fingerprint == info.Fingerprint {
					entry = previous
					entry.Case = normalizeCasePath(info.Case)
				} else {
					entry = newOfflineEntry(info, timestamp, ReasonCaseChanged)
				}
			case 1:
				switch {
				case previous.Status == StatusLivePass && sameCase && previous.Time != "":
					entry = RegistryOperation{
						Status:      StatusLivePass,
						Case:        normalizeCasePath(info.Case),
						Fingerprint: info.Fingerprint,
						Time:        previous.Time,
						Reason:      ReasonLiveVerified,
					}
				case previous.Status == StatusLivePass && !sameCase:
					entry = newOfflineEntry(info, timestamp, ReasonCaseChanged)
				case previous.Status == legacyStatusOfflineValid:
					entry = newOfflineEntry(info, timestamp, ReasonNotRun)
				default:
					entry = newOfflineEntry(info, timestamp, ReasonNotRun)
				}
			}
		}
		productName, resourceName, ok := splitResourceKey(cap.Resource)
		if !ok {
			return nil, fmt.Errorf("invalid declared resource %q", cap.Resource)
		}
		product := reg.Resources[productName]
		if product == nil {
			product = RegistryProduct{}
		}
		resource := product[resourceName]
		if resource.Operations == nil {
			resource.Operations = map[string]RegistryOperation{}
		}
		resource.Operations[cap.Verb] = entry
		product[resourceName] = resource
		reg.Resources[productName] = product
	}
	reg.Summary, err = SummarizePublicSurface(reg, public)
	if err != nil {
		return nil, err
	}
	report := CheckRegistry(reg, declared, covered, casesDir, RegistryCheckOptions{PublicSurface: &public})
	if report.Invalid > 0 {
		first := report.Errors[0]
		return nil, fmt.Errorf("generated registry is invalid: %s %s: %s: %s", first.Resource, first.Operation, first.Code, first.Message)
	}
	return reg, nil
}

func newOfflineEntry(info coverageInfo, timestamp, reason string) RegistryOperation {
	return RegistryOperation{
		Status:      StatusOffline,
		Case:        normalizeCasePath(info.Case),
		Fingerprint: info.Fingerprint,
		Time:        timestamp,
		Reason:      reason,
	}
}

func normalizeCasePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func sameCoverageCasePath(existing *Registry, oldPath, currentPath, currentCasesDir string) bool {
	oldCasesDir := currentCasesDir
	if existing != nil && strings.TrimSpace(existing.Generated.CasesDir) != "" {
		oldCasesDir = existing.Generated.CasesDir
	}
	return casePathKey(oldPath, oldCasesDir) == casePathKey(currentPath, currentCasesDir)
}

func casePathKey(path, casesDir string) string {
	path = filepath.Clean(path)
	casesDir = filepath.Clean(casesDir)
	if casesDir != "." && casesDir != "" {
		if rel, err := filepath.Rel(casesDir, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
		base := filepath.Base(casesDir)
		if rel, ok := strings.CutPrefix(filepath.ToSlash(path), filepath.ToSlash(base)+"/"); ok {
			return rel
		}
	}
	return filepath.ToSlash(path)
}

// CheckRegistryFile validates a version 3 coverage registry against current
// specs and cases.
func CheckRegistryFile(specsDir, casesDir, registryPath string, opts RegistryCheckOptions) (*RegistryCheckReport, error) {
	reg, err := LoadRegistryFile(registryPath)
	if err != nil {
		return nil, err
	}
	declared, index, err := loadDeclared(specsDir)
	if err != nil {
		return nil, err
	}
	covered, err := loadCoveredDetails(casesDir, index)
	if err != nil {
		return nil, err
	}
	return CheckRegistry(reg, declared, covered, casesDir, opts), nil
}

func CheckRegistry(reg *Registry, declared map[Capability]bool, covered map[Capability]coverageInfo, casesDir string, opts RegistryCheckOptions) *RegistryCheckReport {
	rep := &RegistryCheckReport{ByStatus: map[string]int{}}
	for cap := range declared {
		if capabilitySelected(cap, opts) {
			rep.Declared++
		}
	}
	if reg.Version != RegistryVersion {
		rep.add("", "", "invalid_version", fmt.Sprintf("registry version must be %d", RegistryVersion))
	}
	validatePublicSummary(rep, reg, opts.PublicSurface)

	entries := map[Capability]RegistryOperation{}
	for _, productName := range sortedRegistryProducts(reg.Resources) {
		product := reg.Resources[productName]
		if len(product) == 0 {
			rep.add(productName, "", "empty_product", "registry products without resources must be omitted")
			continue
		}
		for _, resourceName := range sortedRegistryResources(product) {
			resourceKey := productName + "/" + resourceName
			if !resourceSelected(resourceKey, opts.ResourceFilter) {
				continue
			}
			resource := product[resourceName]
			if len(resource.Operations) == 0 {
				rep.add(resourceKey, "", "empty_resource", "registry resources without operations must be omitted")
				continue
			}
			for _, verb := range sortedRegistryOperations(resource.Operations) {
				op := resource.Operations[verb]
				cap := Capability{Resource: resourceKey, Verb: verb}
				if !capabilitySelected(cap, opts) {
					continue
				}
				entries[cap] = op
				rep.Entries++
				rep.ByStatus[op.Status]++
				validateEntry(rep, cap, op, declared, covered, casesDir, opts)
			}
		}
	}
	if opts.FailOnNotLive {
		for cap := range declared {
			if !capabilitySelected(cap, opts) {
				continue
			}
			if _, ok := entries[cap]; !ok {
				rep.add(cap.Resource, cap.Verb, "not_live", "selected operation has no case-backed registry entry")
			}
		}
	}
	rep.Invalid = len(rep.Errors)
	return rep
}

// SummarizePublicSurface derives persisted public completion counts from the
// public capability snapshot and the case-backed registry entries.
func SummarizePublicSurface(reg *Registry, public PublicSurface) (RegistryPublicSummary, error) {
	if public.ResourceCount < 0 {
		return RegistryPublicSummary{}, fmt.Errorf("public resource count must not be negative")
	}
	summary := RegistryPublicSummary{
		Surface:    string(scenario.SurfacePublic),
		Resources:  public.ResourceCount,
		Operations: len(public.Capabilities),
	}
	for capability := range public.Capabilities {
		resource, ok := registryResource(reg.Resources, capability.Resource)
		if !ok {
			summary.MissingCases++
			continue
		}
		operation, ok := resource.Operations[capability.Verb]
		if !ok {
			summary.MissingCases++
			continue
		}
		switch operation.Status {
		case StatusLivePass:
			summary.Passed++
		case StatusOffline:
			summary.NotPassed++
		default:
			return RegistryPublicSummary{}, fmt.Errorf("cannot summarize %s %s with status %q", capability.Resource, capability.Verb, operation.Status)
		}
	}
	return summary, nil
}

func validatePublicSummary(report *RegistryCheckReport, registry *Registry, public *PublicSurface) {
	summary := registry.Summary
	if summary.Surface != string(scenario.SurfacePublic) {
		report.add("", "", "invalid_summary_surface", `summary surface must be "public"`)
	}
	counts := []struct {
		name  string
		value int
	}{
		{name: "resources", value: summary.Resources},
		{name: "operations", value: summary.Operations},
		{name: "missing_cases", value: summary.MissingCases},
		{name: "passed", value: summary.Passed},
		{name: "not_passed", value: summary.NotPassed},
	}
	for _, count := range counts {
		if count.value < 0 {
			report.add("", "", "invalid_summary_count", fmt.Sprintf("summary %s must not be negative", count.name))
		}
	}
	if summary.Operations != summary.MissingCases+summary.Passed+summary.NotPassed {
		report.add("", "", "invalid_summary_total", "summary operations must equal missing_cases + passed + not_passed")
	}
	if public == nil {
		return
	}
	expected, err := SummarizePublicSurface(registry, *public)
	if err != nil {
		report.add("", "", "invalid_public_summary", err.Error())
		return
	}
	if summary != expected {
		report.add("", "", "stale_summary", fmt.Sprintf("stored public summary %+v does not match current capabilities %+v", summary, expected))
	}
}

func SummarizeRegistry(reg *Registry, filters ...map[Capability]bool) RegistrySummary {
	summary := RegistrySummary{ByStatus: map[string]int{}}
	var filter map[Capability]bool
	if len(filters) > 0 {
		filter = filters[0]
	}
	for productName, product := range reg.Resources {
		for resourceName, resource := range product {
			resourceKey := productName + "/" + resourceName
			for verb, operation := range resource.Operations {
				if filter != nil && !filter[Capability{Resource: resourceKey, Verb: verb}] {
					continue
				}
				summary.Entries++
				summary.ByStatus[operation.Status]++
			}
		}
	}
	return summary
}

func (r *RegistryCheckReport) add(resource, operation, code, message string) {
	r.Errors = append(r.Errors, RegistryValidationError{
		Resource: resource, Operation: operation, Code: code, Message: message,
	})
}

func validateEntry(rep *RegistryCheckReport, cap Capability, op RegistryOperation, declared map[Capability]bool, covered map[Capability]coverageInfo, casesDir string, opts RegistryCheckOptions) {
	if !allowedStatuses[op.Status] {
		rep.add(cap.Resource, cap.Verb, "unknown_status", fmt.Sprintf("unknown status %q", op.Status))
		return
	}
	if !declared[cap] {
		rep.add(cap.Resource, cap.Verb, "stale_entry", "registry operation no longer exists in specs")
	}
	if opts.FailOnNotLive && op.Status != StatusLivePass {
		rep.add(cap.Resource, cap.Verb, "not_live", "selected operation is not live-pass")
	}

	info, isCovered := covered[cap]
	if op.Case == "" {
		rep.add(cap.Resource, cap.Verb, "missing_case", "registry operation requires case")
	} else if !isCovered {
		rep.add(cap.Resource, cap.Verb, "not_covered", "registry operation is not covered by current cases")
	} else if casePathKey(op.Case, casesDir) != casePathKey(info.Case, casesDir) {
		rep.add(cap.Resource, cap.Verb, "case_mismatch", fmt.Sprintf("registry case %s does not match current case %s", op.Case, normalizeCasePath(info.Case)))
	}

	if op.Fingerprint == "" {
		rep.add(cap.Resource, cap.Verb, "missing_fingerprint", "registry operation requires fingerprint")
	} else if !validFingerprint(op.Fingerprint) {
		rep.add(cap.Resource, cap.Verb, "invalid_fingerprint", "fingerprint must use sha256:<64 lowercase hexadecimal digits>")
	} else if isCovered && op.Fingerprint != info.Fingerprint {
		rep.add(cap.Resource, cap.Verb, "fingerprint_mismatch", "registry fingerprint does not match current case contents")
	}

	if op.Time == "" {
		rep.add(cap.Resource, cap.Verb, "missing_time", "registry operation requires time")
	} else if _, err := time.Parse(time.RFC3339, op.Time); err != nil {
		rep.add(cap.Resource, cap.Verb, "invalid_time", "time must use RFC3339")
	}

	if op.Reason == "" {
		rep.add(cap.Resource, cap.Verb, "missing_reason", "registry operation requires reason")
	} else if !validReason(op.Status, op.Reason) {
		rep.add(cap.Resource, cap.Verb, "invalid_reason", fmt.Sprintf("reason %q is invalid for status %q", op.Reason, op.Status))
	}
}

func validReason(status, reason string) bool {
	switch status {
	case StatusLivePass:
		return reason == ReasonLiveVerified
	case StatusOffline:
		return allowedOfflineReasons[reason]
	default:
		return false
	}
}

func validFingerprint(fingerprint string) bool {
	value, ok := strings.CutPrefix(fingerprint, "sha256:")
	if !ok || len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func resourceSelected(resource string, filter map[string]bool) bool {
	return filter == nil || filter[resource]
}

func capabilitySelected(cap Capability, opts RegistryCheckOptions) bool {
	if !resourceSelected(cap.Resource, opts.ResourceFilter) {
		return false
	}
	return opts.CapabilityFilter == nil || opts.CapabilityFilter[cap]
}

func existingEntry(existing *Registry, cap Capability) (RegistryOperation, bool) {
	if existing == nil {
		return RegistryOperation{}, false
	}
	resource, ok := registryResource(existing.Resources, cap.Resource)
	if !ok {
		return RegistryOperation{}, false
	}
	operation, ok := resource.Operations[cap.Verb]
	return operation, ok
}

func sortedCapabilities(capabilities map[Capability]bool) []Capability {
	result := make([]Capability, 0, len(capabilities))
	for capability := range capabilities {
		result = append(result, capability)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Resource != result[j].Resource {
			return result[i].Resource < result[j].Resource
		}
		return result[i].Verb < result[j].Verb
	})
	return result
}

func sortedRegistryProducts(resources map[string]RegistryProduct) []string {
	result := make([]string, 0, len(resources))
	for product := range resources {
		result = append(result, product)
	}
	sort.Strings(result)
	return result
}

func sortedRegistryResources(product RegistryProduct) []string {
	result := make([]string, 0, len(product))
	for resource := range product {
		result = append(result, resource)
	}
	sort.Strings(result)
	return result
}

func splitResourceKey(resourceKey string) (product, resource string, ok bool) {
	product, resource, ok = strings.Cut(resourceKey, "/")
	return product, resource, ok && product != "" && resource != "" && !strings.Contains(resource, "/")
}

func registryResource(resources map[string]RegistryProduct, resourceKey string) (RegistryResource, bool) {
	productName, resourceName, ok := splitResourceKey(resourceKey)
	if !ok {
		return RegistryResource{}, false
	}
	product, ok := resources[productName]
	if !ok {
		return RegistryResource{}, false
	}
	resource, ok := product[resourceName]
	return resource, ok
}

func setRegistryResource(resources map[string]RegistryProduct, resourceKey string, resource RegistryResource) error {
	productName, resourceName, ok := splitResourceKey(resourceKey)
	if !ok {
		return fmt.Errorf("resource key must be product/resource")
	}
	product := resources[productName]
	if product == nil {
		product = RegistryProduct{}
	}
	if _, exists := product[resourceName]; exists {
		return fmt.Errorf("duplicate canonical resource %s/%s", productName, resourceName)
	}
	product[resourceName] = resource
	resources[productName] = product
	return nil
}

func sortedRegistryOperations(operations map[string]RegistryOperation) []string {
	result := make([]string, 0, len(operations))
	for operation := range operations {
		result = append(result, operation)
	}
	sort.Strings(result)
	return result
}

func loadCoveredDetails(casesDir string, index map[string]map[string]bool) (map[Capability]coverageInfo, error) {
	suites, err := scenario.LoadDir(casesDir)
	if err != nil {
		return nil, err
	}
	covered := map[Capability]coverageInfo{}
	for _, suite := range suites {
		fingerprint, err := caseFingerprint(suite.Path)
		if err != nil {
			return nil, err
		}
		for _, step := range suite.Steps {
			capability, ok := commandCapability(step.Run, index)
			if !ok {
				continue
			}
			info := covered[capability]
			switch {
			case info.Case == "":
				info = coverageInfo{
					Case:          suite.Path,
					SuiteResource: suite.Resource,
					Fingerprint:   fingerprint,
				}
			case info.Case == suite.Path:
			case suite.Resource == capability.Resource && info.SuiteResource != capability.Resource:
				info = coverageInfo{
					Case:          suite.Path,
					SuiteResource: suite.Resource,
					Fingerprint:   fingerprint,
				}
			default:
				continue
			}
			info.Steps = append(info.Steps, step.Name)
			covered[capability] = info
		}
	}
	for capability, info := range covered {
		sort.Strings(info.Steps)
		covered[capability] = info
	}
	return covered, nil
}

func caseFingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", digest), nil
}
