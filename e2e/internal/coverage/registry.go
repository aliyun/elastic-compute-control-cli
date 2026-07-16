package coverage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"ecctl/e2e/internal/scenario"
)

const (
	StatusMissing      = "missing"
	StatusPlanned      = "planned"
	StatusDrafted      = "drafted"
	StatusOfflineValid = "offline-valid"
	StatusLivePass     = "live-pass"
	StatusManualOnly   = "manual-only"
	StatusQuarantined  = "quarantined"
	StatusRetired      = "retired"
)

var allowedStatuses = map[string]bool{
	StatusMissing:      true,
	StatusPlanned:      true,
	StatusDrafted:      true,
	StatusOfflineValid: true,
	StatusLivePass:     true,
	StatusManualOnly:   true,
	StatusQuarantined:  true,
	StatusRetired:      true,
}

var allowedManualReasons = map[string]bool{
	"cost":                      true,
	"quota":                     true,
	"requires-prepaid":          true,
	"requires-existing-cluster": true,
	"requires-human-approval":   true,
	"unsafe-delete":             true,
	"unsupported-region":        true,
	"provider-limitation":       true,
}

var allowedQuarantineReasons = map[string]bool{
	"flaky-provider":    true,
	"known-product-bug": true,
	"known-ecctl-bug":   true,
	"quota-blocked":     true,
	"cleanup-risk":      true,
	"credential-scope":  true,
}

// Registry is e2e/coverage.yaml.
type Registry struct {
	Version   int                         `yaml:"version" json:"version"`
	Generated RegistryGenerated           `yaml:"generated,omitempty" json:"generated,omitempty"`
	Resources map[string]RegistryResource `yaml:"resources" json:"resources"`
}

type RegistryGenerated struct {
	SpecsDir string `yaml:"specs_dir,omitempty" json:"specs_dir,omitempty"`
	CasesDir string `yaml:"cases_dir,omitempty" json:"cases_dir,omitempty"`
}

type RegistryResource struct {
	Operations map[string]RegistryOperation `yaml:"operations" json:"operations"`
}

type RegistryOperation struct {
	Status       string           `yaml:"status" json:"status"`
	Case         string           `yaml:"case,omitempty" json:"case,omitempty"`
	Steps        []string         `yaml:"steps,omitempty" json:"steps,omitempty"`
	Reason       string           `yaml:"reason,omitempty" json:"reason,omitempty"`
	ReviewAfter  string           `yaml:"review_after,omitempty" json:"review_after,omitempty"`
	Owner        string           `yaml:"owner,omitempty" json:"owner,omitempty"`
	TargetPhase  int              `yaml:"target_phase,omitempty" json:"target_phase,omitempty"`
	Issue        string           `yaml:"issue,omitempty" json:"issue,omitempty"`
	LastChecked  string           `yaml:"last_checked,omitempty" json:"last_checked,omitempty"`
	RemovedAfter string           `yaml:"removed_after,omitempty" json:"removed_after,omitempty"`
	Evidence     RegistryEvidence `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

type RegistryEvidence struct {
	CoverageSource string `yaml:"coverage_source,omitempty" json:"coverage_source,omitempty"`
	Report         string `yaml:"report,omitempty" json:"report,omitempty"`
	RunID          string `yaml:"run_id,omitempty" json:"run_id,omitempty"`
	VerifiedAt     string `yaml:"verified_at,omitempty" json:"verified_at,omitempty"`
}

type RegistryCheckOptions struct {
	AllowStale       bool
	FailOnMissing    bool
	FailOnNotLive    bool
	ResourceFilter   map[string]bool
	CapabilityFilter map[Capability]bool
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
}

// LoadRegistryFile reads a coverage registry from YAML.
func LoadRegistryFile(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	if reg.Resources == nil {
		reg.Resources = map[string]RegistryResource{}
	}
	return &reg, nil
}

// WriteRegistryFile writes a coverage registry with stable YAML ordering.
func WriteRegistryFile(path string, reg *Registry) error {
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

func InitRegistry(specsDir, casesDir string, existing *Registry) (*Registry, error) {
	declared, index, err := loadDeclared(specsDir)
	if err != nil {
		return nil, err
	}
	covered, err := loadCoveredDetails(casesDir, index)
	if err != nil {
		return nil, err
	}
	reg := &Registry{
		Version:   1,
		Generated: RegistryGenerated{SpecsDir: specsDir, CasesDir: casesDir},
		Resources: map[string]RegistryResource{},
	}
	for _, cap := range sortedCapabilities(declared) {
		if reg.Resources[cap.Resource].Operations == nil {
			reg.Resources[cap.Resource] = RegistryResource{Operations: map[string]RegistryOperation{}}
		}
		entry, keep := existingEntry(existing, cap)
		info, isCovered := covered[cap]
		if keep && entry.Status == StatusPlanned && isCovered {
			keep = false
		}
		if keep && entry.Status == StatusLivePass && isCovered {
			if !sameCoverageCasePath(existing, entry.Case, info.Case, casesDir) || !equalStrings(entry.Steps, info.Steps) {
				keep = false
			}
		}
		if !keep {
			if isCovered {
				entry = RegistryOperation{
					Status: StatusOfflineValid,
					Case:   info.Case,
					Steps:  append([]string(nil), info.Steps...),
					Evidence: RegistryEvidence{
						CoverageSource: "command",
					},
				}
			} else {
				entry = RegistryOperation{Status: StatusMissing}
			}
		}
		res := reg.Resources[cap.Resource]
		res.Operations[cap.Verb] = entry
		reg.Resources[cap.Resource] = res
	}
	return reg, nil
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

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

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
	base := filepath.Dir(registryPath)
	return CheckRegistry(reg, declared, covered, casesDir, base, opts), nil
}

func CheckRegistry(reg *Registry, declared map[Capability]bool, covered map[Capability]coverageInfo, casesDir, registryDir string, opts RegistryCheckOptions) *RegistryCheckReport {
	rep := &RegistryCheckReport{ByStatus: map[string]int{}}
	for cap := range declared {
		if capabilitySelected(cap, opts) {
			rep.Declared++
		}
	}
	if reg.Version != 1 {
		rep.add("", "", "invalid_version", "registry version must be 1")
	}

	entries := map[Capability]RegistryOperation{}
	for _, resource := range sortedRegistryResources(reg.Resources) {
		if !resourceSelected(resource, opts.ResourceFilter) {
			continue
		}
		rr := reg.Resources[resource]
		for _, verb := range sortedRegistryOperations(rr.Operations) {
			op := rr.Operations[verb]
			cap := Capability{Resource: resource, Verb: verb}
			if !capabilitySelected(cap, opts) {
				continue
			}
			entries[cap] = op
			rep.Entries++
			rep.ByStatus[op.Status]++
			validateEntry(rep, cap, op, declared, covered, casesDir, registryDir, opts)
		}
	}
	for cap := range declared {
		if !capabilitySelected(cap, opts) {
			continue
		}
		if _, ok := entries[cap]; !ok {
			rep.add(cap.Resource, cap.Verb, "missing_entry", "declared operation is missing from registry")
		}
	}
	rep.Invalid = len(rep.Errors)
	return rep
}

func SummarizeRegistry(reg *Registry, filters ...map[Capability]bool) RegistrySummary {
	sum := RegistrySummary{ByStatus: map[string]int{}}
	var filter map[Capability]bool
	if len(filters) > 0 {
		filter = filters[0]
	}
	for resource, rr := range reg.Resources {
		for verb, op := range rr.Operations {
			if filter != nil && !filter[Capability{Resource: resource, Verb: verb}] {
				continue
			}
			sum.Entries++
			sum.ByStatus[op.Status]++
		}
	}
	return sum
}

func (r *RegistryCheckReport) add(resource, operation, code, msg string) {
	r.Errors = append(r.Errors, RegistryValidationError{
		Resource: resource, Operation: operation, Code: code, Message: msg,
	})
}

func validateEntry(rep *RegistryCheckReport, cap Capability, op RegistryOperation, declared map[Capability]bool, covered map[Capability]coverageInfo, casesDir, registryDir string, opts RegistryCheckOptions) {
	if !allowedStatuses[op.Status] {
		rep.add(cap.Resource, cap.Verb, "unknown_status", fmt.Sprintf("unknown status %q", op.Status))
		return
	}
	exists := declared[cap]
	if !exists && op.Status != StatusRetired {
		rep.add(cap.Resource, cap.Verb, "stale_entry", "registry operation no longer exists in specs")
	}
	if exists && op.Status == StatusRetired {
		rep.add(cap.Resource, cap.Verb, "retired_existing", "retired operation still exists in specs")
	}
	if opts.FailOnNotLive && op.Status != StatusLivePass {
		rep.add(cap.Resource, cap.Verb, "not_live", "selected operation is not live-pass")
	}

	switch op.Status {
	case StatusMissing:
		if opts.FailOnMissing {
			rep.add(cap.Resource, cap.Verb, "missing_status", "operation is still marked missing")
		}
		if _, ok := covered[cap]; ok && !opts.AllowStale {
			rep.add(cap.Resource, cap.Verb, "covered_missing", "missing operation is covered by current cases")
		}
	case StatusPlanned:
		if op.Owner == "" {
			rep.add(cap.Resource, cap.Verb, "missing_owner", "planned operation requires owner")
		}
		if op.TargetPhase <= 0 {
			rep.add(cap.Resource, cap.Verb, "invalid_target_phase", "planned operation requires positive target_phase")
		}
	case StatusDrafted:
		validateCasePath(rep, cap, op, casesDir)
	case StatusOfflineValid:
		validateCasePath(rep, cap, op, casesDir)
		if len(op.Steps) == 0 {
			rep.add(cap.Resource, cap.Verb, "missing_steps", "offline-valid operation requires steps")
		}
		if _, ok := covered[cap]; !ok {
			rep.add(cap.Resource, cap.Verb, "not_covered", "offline-valid operation is not covered by current cases")
		}
	case StatusLivePass:
		validateCasePath(rep, cap, op, casesDir)
		if len(op.Steps) == 0 {
			rep.add(cap.Resource, cap.Verb, "missing_steps", "live-pass operation requires steps")
		}
		if op.Evidence.Report == "" || op.Evidence.RunID == "" || op.Evidence.VerifiedAt == "" {
			rep.add(cap.Resource, cap.Verb, "missing_evidence", "live-pass operation requires report, run_id and verified_at")
		}
	case StatusManualOnly:
		if op.Reason == "" {
			rep.add(cap.Resource, cap.Verb, "missing_reason", "manual-only operation requires reason")
		} else if !allowedManualReasons[op.Reason] {
			rep.add(cap.Resource, cap.Verb, "invalid_reason", fmt.Sprintf("manual-only reason %q is not allowed", op.Reason))
		}
		validateDate(rep, cap, "review_after", op.ReviewAfter)
	case StatusQuarantined:
		if op.Reason == "" {
			rep.add(cap.Resource, cap.Verb, "missing_reason", "quarantined operation requires reason")
		} else if !allowedQuarantineReasons[op.Reason] {
			rep.add(cap.Resource, cap.Verb, "invalid_reason", fmt.Sprintf("quarantine reason %q is not allowed", op.Reason))
		}
		if op.Issue == "" {
			rep.add(cap.Resource, cap.Verb, "missing_issue", "quarantined operation requires issue")
		}
		validateDate(rep, cap, "last_checked", op.LastChecked)
	case StatusRetired:
		validateDate(rep, cap, "removed_after", op.RemovedAfter)
	}
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

func validateCasePath(rep *RegistryCheckReport, cap Capability, op RegistryOperation, casesDir string) {
	if op.Case == "" {
		rep.add(cap.Resource, cap.Verb, "missing_case", fmt.Sprintf("%s operation requires case", op.Status))
		return
	}
	if _, err := os.Stat(resolvePath(op.Case, filepath.Dir(casesDir))); err != nil {
		rep.add(cap.Resource, cap.Verb, "case_missing", fmt.Sprintf("case %s not found", op.Case))
	}
}

func validateDate(rep *RegistryCheckReport, cap Capability, field, value string) {
	if value == "" {
		rep.add(cap.Resource, cap.Verb, "missing_"+field, fmt.Sprintf("%s is required", field))
		return
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		rep.add(cap.Resource, cap.Verb, "invalid_date", fmt.Sprintf("%s must use YYYY-MM-DD", field))
	}
}

func resolvePath(path, base string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join(base, path)
}

func existingEntry(existing *Registry, cap Capability) (RegistryOperation, bool) {
	if existing == nil {
		return RegistryOperation{}, false
	}
	rr, ok := existing.Resources[cap.Resource]
	if !ok {
		return RegistryOperation{}, false
	}
	op, ok := rr.Operations[cap.Verb]
	if !ok {
		return RegistryOperation{}, false
	}
	switch op.Status {
	case StatusManualOnly, StatusQuarantined, StatusPlanned, StatusLivePass:
		return op, true
	default:
		return RegistryOperation{}, false
	}
}

func sortedCapabilities(m map[Capability]bool) []Capability {
	out := make([]Capability, 0, len(m))
	for cap := range m {
		out = append(out, cap)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Verb < out[j].Verb
	})
	return out
}

func sortedRegistryResources(resources map[string]RegistryResource) []string {
	out := make([]string, 0, len(resources))
	for resource := range resources {
		out = append(out, resource)
	}
	sort.Strings(out)
	return out
}

func sortedRegistryOperations(ops map[string]RegistryOperation) []string {
	out := make([]string, 0, len(ops))
	for op := range ops {
		out = append(out, op)
	}
	sort.Strings(out)
	return out
}

func loadCoveredDetails(casesDir string, index map[string]map[string]bool) (map[Capability]coverageInfo, error) {
	suites, err := scenario.LoadDir(casesDir)
	if err != nil {
		return nil, err
	}
	covered := map[Capability]coverageInfo{}
	for _, s := range suites {
		for _, st := range s.Steps {
			cap, ok := commandCapability(st.Run, index)
			if !ok {
				continue
			}
			info := covered[cap]
			switch {
			case info.Case == "":
				info = coverageInfo{
					Case:          s.Path,
					SuiteResource: s.Resource,
				}
			case info.Case == s.Path:
			case s.Resource == cap.Resource && info.SuiteResource != cap.Resource:
				info = coverageInfo{
					Case:          s.Path,
					SuiteResource: s.Resource,
				}
			default:
				continue
			}
			info.Steps = append(info.Steps, st.Name)
			covered[cap] = info
		}
	}
	for cap, info := range covered {
		sort.Strings(info.Steps)
		covered[cap] = info
	}
	return covered, nil
}

func (r RegistryEvidence) IsZero() bool {
	return r.CoverageSource == "" && r.Report == "" && r.RunID == "" && r.VerifiedAt == ""
}

func (r RegistryEvidence) MarshalYAML() (any, error) {
	if r.IsZero() {
		return nil, nil
	}
	type alias RegistryEvidence
	return alias(r), nil
}

func (r RegistryEvidence) MarshalJSON() ([]byte, error) {
	if r.IsZero() {
		return []byte(`{}`), nil
	}
	type alias RegistryEvidence
	return json.Marshal(alias(r))
}

func (r RegistryOperation) MarshalYAML() (any, error) {
	type alias RegistryOperation
	return alias(r), nil
}

func (r RegistryOperation) MarshalJSON() ([]byte, error) {
	type alias RegistryOperation
	return json.Marshal(alias(r))
}

func (r RegistryOperation) String() string {
	var parts []string
	if r.Status != "" {
		parts = append(parts, "status="+r.Status)
	}
	if r.Case != "" {
		parts = append(parts, "case="+r.Case)
	}
	return strings.Join(parts, " ")
}
