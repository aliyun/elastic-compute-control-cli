// Package scenario defines the E2E case schema and loads case YAML from disk.
//
// A case file (e.g. cases/ecs/instance-lifecycle.yaml) describes one resource's
// lifecycle as an ordered list of steps. Each step is a full `ecctl` command
// line plus declarative assertions (matchers), variable captures and an
// optional teardown command.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Surface identifies the ecctl command surface a case is allowed to exercise.
// Public cases run with the default CLI; full cases are reserved for the
// explicit full-surface binary.
type Surface string

const (
	SurfacePublic Surface = "public"
	SurfaceFull   Surface = "full"
)

func (s Surface) Valid() bool {
	return s == SurfacePublic || s == SurfaceFull
}

// ExecutionMode controls whether a case preserves the historical ordered,
// fail-fast lifecycle or schedules its steps as an operation dependency DAG.
type ExecutionMode string

const (
	ExecutionSequential ExecutionMode = "sequential"
	ExecutionDAG        ExecutionMode = "dag"
)

func (m ExecutionMode) Valid() bool {
	return m == ExecutionSequential || m == ExecutionDAG
}

// Suite is one case file: a resource and the steps that exercise it.
type Suite struct {
	Surface               Surface                      `yaml:"surface"` // public (default) or full
	Execution             ExecutionMode                `yaml:"execution"`
	Resource              string                       `yaml:"resource"`
	Covers                []string                     `yaml:"covers"`                 // additional resources exercised as part of this lifecycle
	Timeout               string                       `yaml:"timeout"`                // optional per-case step timeout override
	Serial                bool                         `yaml:"serial"`                 // run outside the parallel pool
	Needs                 []string                     `yaml:"needs"`                  // requested shared-stack node IDs; dependencies are included transitively
	RequiresGlobal        []string                     `yaml:"requires_global"`        // dotted run-level value keys
	RequiresParams        []string                     `yaml:"requires_params"`        // dotted dynamic parameter keys
	ParameterConstraints  ParameterConstraints         `yaml:"parameter_constraints"`  // capability requirements for dynamic parameters
	RequiresPrerequisites []string                     `yaml:"requires_prerequisites"` // primary-region prerequisite bundles
	RegionRequirements    map[string]RegionRequirement `yaml:"region_requirements"`    // additional named region roles
	Steps                 []Step                       `yaml:"steps"`
	Path                  string                       `yaml:"-"` // source file, for reports
}

// ParameterConstraints narrows dynamic inventory selection for cases whose
// APIs require more than basic stock availability.
type ParameterConstraints struct {
	ECS ECSParameterConstraints `yaml:"ecs"`
}

// ECSParameterConstraints describes capabilities that must be satisfied by
// the instance/disk tuple selected before the case starts.
type ECSParameterConstraints struct {
	MinENIQuantity                 int      `yaml:"min_eni_quantity"`
	MinENIPrivateIPAddressQuantity int      `yaml:"min_eni_private_ip_address_quantity"`
	AllowedSystemDiskCategories    []string `yaml:"allowed_system_disk_categories"`
	AllowedDataDiskCategories      []string `yaml:"allowed_data_disk_categories"`
}

// RegionRequirement declares the prerequisite bundles required by one named
// region role and an optional role it must not share a region with.
type RegionRequirement struct {
	RequiresPrerequisites []string `yaml:"requires_prerequisites"`
	DistinctFrom          string   `yaml:"distinct_from"`
}

// Step is a single command invocation with its expectations.
type Step struct {
	Name                  string            `yaml:"name"`
	DependsOn             []string          `yaml:"depends_on"`
	Needs                 []string          `yaml:"needs"`
	RequiresPrerequisites []string          `yaml:"requires_prerequisites"`
	Locks                 []string          `yaml:"locks"`
	Run                   string            `yaml:"run"`   // full "ecctl ..." command line
	Local                 *LocalAction      `yaml:"local"` // constrained runner-owned local action
	At                    string            `yaml:"at"`    // base jsonpath for expect/capture
	Exit                  *int              `yaml:"exit"`
	NotFoundOK            bool              `yaml:"not_found_ok"` // exit 4 (NotFound) also passes
	Expect                Expectations      `yaml:"expect"`
	Assert                []string          `yaml:"assert"`  // expression escape hatch
	Capture               map[string]string `yaml:"capture"` // var -> jsonpath (relative to At)
	Teardown              string            `yaml:"teardown"`
	TeardownRegion        string            `yaml:"teardown_region"` // primary (default) or a named region role
}

// LocalAction is a runner-owned operation that does not invoke a shell. The
// action and both paths are validated before execution, and paths are confined
// to the run's private temporary directory.
type LocalAction struct {
	Action      string `yaml:"action"`
	Source      string `yaml:"source"`
	Destination string `yaml:"destination"`
}

// Expectations preserves authoring order of the path->matcher mapping.
type Expectations []PathMatcher

// PathMatcher binds a jsonpath to a matcher.
type PathMatcher struct {
	Path    string
	Matcher Matcher
}

// UnmarshalYAML decodes the `expect` mapping while keeping key order.
func (e *Expectations) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("expect must be a mapping of path->matcher")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		path := n.Content[i].Value
		var m Matcher
		if err := n.Content[i+1].Decode(&m); err != nil {
			return fmt.Errorf("expect[%q]: %w", path, err)
		}
		*e = append(*e, PathMatcher{Path: path, Matcher: m})
	}
	return nil
}

// Load reads and validates a single case file.
func Load(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Suite
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Surface == "" {
		s.Surface = SurfacePublic
	}
	if s.Execution == "" {
		s.Execution = ExecutionSequential
	}
	s.Path = path
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// LoadDir loads every *.yaml/*.yml case under dir (recursively), sorted by path.
func LoadDir(dir string) ([]*Suite, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml":
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var suites []*Suite
	for _, p := range paths {
		s, err := Load(p)
		if err != nil {
			return nil, err
		}
		suites = append(suites, s)
	}
	return suites, nil
}

// Validate checks structural invariants offline (used by `validate`).
func (s *Suite) Validate() error {
	if !s.Surface.Valid() {
		return fmt.Errorf("surface must be %q or %q", SurfacePublic, SurfaceFull)
	}
	if !s.Execution.Valid() {
		return fmt.Errorf("execution must be %q or %q", ExecutionSequential, ExecutionDAG)
	}
	if s.Resource == "" {
		return fmt.Errorf("resource is required")
	}
	if err := validateResourceIdentity(s.Resource); err != nil {
		return fmt.Errorf("resource: %w", err)
	}
	covered := map[string]bool{s.Resource: true}
	for _, resource := range s.Covers {
		if err := validateResourceIdentity(resource); err != nil {
			return fmt.Errorf("covers: %w", err)
		}
		if covered[resource] {
			return fmt.Errorf("covers contains duplicate resource %q", resource)
		}
		covered[resource] = true
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}
	if err := s.ParameterConstraints.Validate(); err != nil {
		return fmt.Errorf("parameter_constraints: %w", err)
	}
	if (s.ParameterConstraints.ECS.MinENIQuantity > 0 || s.ParameterConstraints.ECS.MinENIPrivateIPAddressQuantity > 0) &&
		!containsString(s.RequiresParams, "ecs.instance_type") && !containsString(s.RequiresParams, "ack.instance_type") {
		return fmt.Errorf("ECS ENI parameter constraints require ecs.instance_type or ack.instance_type")
	}
	if len(s.ParameterConstraints.ECS.AllowedSystemDiskCategories) > 0 && !containsString(s.RequiresParams, "ecs.system_disk_category") {
		return fmt.Errorf("parameter_constraints.ecs.allowed_system_disk_categories requires ecs.system_disk_category")
	}
	if len(s.ParameterConstraints.ECS.AllowedDataDiskCategories) > 0 && !containsString(s.RequiresParams, "ecs.data_disk_category") {
		return fmt.Errorf("parameter_constraints.ecs.allowed_data_disk_categories requires ecs.data_disk_category")
	}
	if err := validatePrerequisiteNames(s.RequiresPrerequisites); err != nil {
		return fmt.Errorf("requires_prerequisites: %w", err)
	}
	roles := map[string]bool{"primary": true}
	for role, requirement := range s.RegionRequirements {
		if err := validateRegionRole(role); err != nil {
			return fmt.Errorf("region_requirements[%q]: %w", role, err)
		}
		if role == "primary" {
			return fmt.Errorf("region_requirements must not redeclare primary; use requires_prerequisites")
		}
		if err := validatePrerequisiteNames(requirement.RequiresPrerequisites); err != nil {
			return fmt.Errorf("region_requirements[%q].requires_prerequisites: %w", role, err)
		}
		roles[role] = true
	}
	for role, requirement := range s.RegionRequirements {
		distinctFrom := strings.TrimSpace(requirement.DistinctFrom)
		if distinctFrom == "" {
			continue
		}
		if distinctFrom == role {
			return fmt.Errorf("region_requirements[%q].distinct_from must name another role", role)
		}
		if !roles[distinctFrom] {
			return fmt.Errorf("region_requirements[%q].distinct_from references unknown role %q", role, distinctFrom)
		}
		requirement.DistinctFrom = distinctFrom
		s.RegionRequirements[role] = requirement
	}
	stepNames := make(map[string]int, len(s.Steps))
	for i := range s.Steps {
		st := &s.Steps[i]
		st.Name = strings.TrimSpace(st.Name)
		if s.Execution == ExecutionDAG {
			if st.Name == "" {
				return fmt.Errorf("steps[%d]: name is required for dag execution", i)
			}
			if previous, exists := stepNames[st.Name]; exists {
				return fmt.Errorf("steps[%d] (%s): duplicate step name first declared at steps[%d]", i, st.Name, previous)
			}
			stepNames[st.Name] = i
		}
		if err := validatePrerequisiteNames(st.RequiresPrerequisites); err != nil {
			return fmt.Errorf("steps[%d] (%s) requires_prerequisites: %w", i, st.Name, err)
		}
		if s.Execution == ExecutionSequential {
			switch {
			case len(st.DependsOn) > 0:
				return fmt.Errorf("steps[%d] (%s): depends_on requires execution: dag", i, st.Name)
			case len(st.Needs) > 0:
				return fmt.Errorf("steps[%d] (%s): needs requires execution: dag or suite-level needs", i, st.Name)
			case len(st.RequiresPrerequisites) > 0:
				return fmt.Errorf("steps[%d] (%s): requires_prerequisites requires execution: dag or suite-level requires_prerequisites", i, st.Name)
			}
		}
		for _, lock := range st.Locks {
			if strings.TrimSpace(lock) == "" {
				return fmt.Errorf("steps[%d] (%s): lock entries must not be empty", i, st.Name)
			}
		}
		hasRun := strings.TrimSpace(st.Run) != ""
		hasLocal := st.Local != nil
		if hasRun == hasLocal {
			return fmt.Errorf("steps[%d] (%s): exactly one of run or local is required", i, st.Name)
		}
		if hasRun && !strings.HasPrefix(strings.TrimSpace(st.Run), "ecctl") {
			return fmt.Errorf("steps[%d] (%s): run must be a full `ecctl ...` command", i, st.Name)
		}
		if hasLocal {
			if st.Local.Action != "extract-tar-gzip" {
				return fmt.Errorf("steps[%d] (%s): unsupported local action %q", i, st.Name, st.Local.Action)
			}
			if strings.TrimSpace(st.Local.Source) == "" || strings.TrimSpace(st.Local.Destination) == "" {
				return fmt.Errorf("steps[%d] (%s): local source and destination are required", i, st.Name)
			}
			if strings.TrimSpace(st.Teardown) != "" || strings.TrimSpace(st.TeardownRegion) != "" {
				return fmt.Errorf("steps[%d] (%s): local actions cannot declare teardown", i, st.Name)
			}
		}
		if role := strings.TrimSpace(st.TeardownRegion); role != "" && !roles[role] {
			return fmt.Errorf("steps[%d] (%s): teardown_region references unknown role %q", i, st.Name, role)
		}
		for _, pm := range st.Expect {
			if err := pm.Matcher.Validate(); err != nil {
				return fmt.Errorf("steps[%d] (%s) expect[%q]: %w", i, st.Name, pm.Path, err)
			}
		}
	}
	if s.Execution == ExecutionDAG {
		state := make(map[string]int, len(s.Steps))
		var visit func(string) error
		visit = func(name string) error {
			switch state[name] {
			case 1:
				return fmt.Errorf("dependency cycle at step %q", name)
			case 2:
				return nil
			}
			index, exists := stepNames[name]
			if !exists {
				return fmt.Errorf("unknown step %q", name)
			}
			state[name] = 1
			seen := map[string]bool{}
			for _, dependency := range s.Steps[index].DependsOn {
				dependency = strings.TrimSpace(dependency)
				if dependency == "" {
					return fmt.Errorf("steps[%d] (%s): depends_on entries must not be empty", index, name)
				}
				if seen[dependency] {
					return fmt.Errorf("steps[%d] (%s): duplicate dependency %q", index, name, dependency)
				}
				seen[dependency] = true
				if _, exists := stepNames[dependency]; !exists {
					return fmt.Errorf("steps[%d] (%s): depends_on references unknown step %q", index, name, dependency)
				}
				if err := visit(dependency); err != nil {
					return err
				}
			}
			state[name] = 2
			return nil
		}
		for name := range stepNames {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// CoversResource reports whether the suite owns coverage for a resource. A
// secondary resource must be declared explicitly in covers; merely invoking a
// foreign command for setup does not make it part of the case contract.
func (s *Suite) CoversResource(resource string) bool {
	if s.Resource == resource {
		return true
	}
	return containsString(s.Covers, resource)
}

// Validate checks that constraints are meaningful and deterministic.
func (c ParameterConstraints) Validate() error {
	if c.ECS.MinENIQuantity < 0 {
		return fmt.Errorf("ecs.min_eni_quantity must not be negative")
	}
	if c.ECS.MinENIPrivateIPAddressQuantity < 0 {
		return fmt.Errorf("ecs.min_eni_private_ip_address_quantity must not be negative")
	}
	seen := map[string]bool{}
	for _, category := range c.ECS.AllowedSystemDiskCategories {
		category = strings.TrimSpace(category)
		if category == "" {
			return fmt.Errorf("ecs.allowed_system_disk_categories must not contain empty values")
		}
		if seen[category] {
			return fmt.Errorf("ecs.allowed_system_disk_categories must not contain duplicate values")
		}
		seen[category] = true
	}
	seen = map[string]bool{}
	for _, category := range c.ECS.AllowedDataDiskCategories {
		category = strings.TrimSpace(category)
		if category == "" {
			return fmt.Errorf("ecs.allowed_data_disk_categories must not contain empty values")
		}
		if seen[category] {
			return fmt.Errorf("ecs.allowed_data_disk_categories must not contain duplicate values")
		}
		seen[category] = true
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateResourceIdentity(resource string) error {
	parts := strings.Split(resource, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("%q must be a canonical product/resource identity", resource)
	}
	return nil
}

func validatePrerequisiteNames(names []string) error {
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		parts := strings.Split(name, ".")
		if len(parts) < 2 {
			return fmt.Errorf("bundle %q must be a dotted name", name)
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				return fmt.Errorf("invalid bundle %q", name)
			}
		}
		if seen[name] {
			return fmt.Errorf("duplicate bundle %q", name)
		}
		seen[name] = true
	}
	return nil
}

func validateRegionRole(role string) error {
	if role == "" {
		return fmt.Errorf("role is required")
	}
	for i, r := range role {
		if (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') || (i > 0 && (r == '_' || r == '-')) {
			continue
		}
		return fmt.Errorf("invalid role name")
	}
	return nil
}
