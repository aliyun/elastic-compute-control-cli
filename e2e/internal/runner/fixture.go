package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"

	"gopkg.in/yaml.v3"
)

// Fixture is the shared stack definition (fixtures/stack.yaml). Its provision
// steps create reusable resources requested by selected cases.
type Fixture struct {
	Tags      map[string]string `yaml:"tags"`
	Provision []ProvisionStep   `yaml:"provision"`
}

const (
	FixtureLifetimeExecution = "execution"
	FixtureLifetimeRun       = "run"
)

// ProvisionStep creates one shared-stack resource.
type ProvisionStep struct {
	ID                    string            `yaml:"id"`
	Resource              string            `yaml:"resource"`
	Mode                  string            `yaml:"mode"`
	Lifetime              string            `yaml:"lifetime"`
	Needs                 []string          `yaml:"needs"`
	RequiresParams        []string          `yaml:"requires_params"`
	RequiresPrerequisites []string          `yaml:"requires_prerequisites"`
	Run                   string            `yaml:"run"`
	At                    string            `yaml:"at"`
	Capture               map[string]string `yaml:"capture"`
	Teardown              string            `yaml:"teardown"`
}

// FixtureDependency is the stable, inspectable identity of one fixture node.
// Resource is canonical product/resource metadata; Mode distinguishes a cloud
// resource created by the fixture from a read-only inventory lookup.
type FixtureDependency struct {
	ID       string   `json:"id"`
	Resource string   `json:"resource"`
	Mode     string   `json:"mode"`
	Lifetime string   `json:"lifetime"`
	Needs    []string `json:"needs,omitempty"`
}

// SuiteFixtureDependencies exposes both the case's direct needs and the full
// transitive fixture closure in provisioning order.
type SuiteFixtureDependencies struct {
	Direct   []string            `json:"direct"`
	Steps    map[string][]string `json:"steps,omitempty"`
	Fixtures []FixtureDependency `json:"fixtures"`
}

func loadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Fixture
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ordered, err := topoSort(f.Provision)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateCaptureProviders(ordered); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.Provision = ordered
	byID := make(map[string]ProvisionStep, len(f.Provision))
	for i := range f.Provision {
		if strings.TrimSpace(f.Provision[i].Resource) == "" {
			return nil, fmt.Errorf("%s: provision step %q is missing resource metadata", path, f.Provision[i].ID)
		}
		if f.Provision[i].Mode == "" {
			f.Provision[i].Mode = "create"
		}
		switch f.Provision[i].Mode {
		case "create", "lookup":
		default:
			return nil, fmt.Errorf("%s: provision step %q has unsupported mode %q", path, f.Provision[i].ID, f.Provision[i].Mode)
		}
		if f.Provision[i].Lifetime == "" {
			f.Provision[i].Lifetime = FixtureLifetimeExecution
		}
		switch f.Provision[i].Lifetime {
		case FixtureLifetimeExecution, FixtureLifetimeRun:
		default:
			return nil, fmt.Errorf("%s: provision step %q has unsupported lifetime %q", path, f.Provision[i].ID, f.Provision[i].Lifetime)
		}
		byID[f.Provision[i].ID] = f.Provision[i]
	}
	for _, step := range f.Provision {
		if step.Lifetime != FixtureLifetimeRun {
			continue
		}
		for _, dependency := range step.Needs {
			if byID[dependency].Lifetime != FixtureLifetimeRun {
				return nil, fmt.Errorf("%s: run lifetime provision step %q depends on execution lifetime step %q", path, step.ID, dependency)
			}
		}
	}
	return &f, nil
}

// plan returns the requested provision steps and their transitive dependencies
// in the fixture's topological order.
func (f *Fixture) plan(requested []string) ([]ProvisionStep, error) {
	byID := make(map[string]ProvisionStep, len(f.Provision))
	for _, step := range f.Provision {
		byID[step.ID] = step
	}

	selected := make(map[string]bool, len(requested))
	var selectStep func(string) error
	selectStep = func(id string) error {
		if selected[id] {
			return nil
		}
		step, ok := byID[id]
		if !ok {
			return fmt.Errorf("unknown dependency %q", id)
		}
		for _, dependency := range step.Needs {
			if err := selectStep(dependency); err != nil {
				return err
			}
		}
		selected[id] = true
		return nil
	}
	for _, id := range requested {
		if err := selectStep(id); err != nil {
			return nil, err
		}
	}

	planned := make([]ProvisionStep, 0, len(selected))
	for _, step := range f.Provision {
		if selected[step.ID] {
			planned = append(planned, step)
		}
	}
	return planned, nil
}

func (f *Fixture) requirements() []string {
	seen := map[string]bool{}
	var requirements []string
	for _, step := range f.Provision {
		for _, requirement := range step.RequiresParams {
			if !seen[requirement] {
				seen[requirement] = true
				requirements = append(requirements, requirement)
			}
		}
	}
	return requirements
}

func (f *Fixture) prerequisiteRequirements() []string {
	seen := map[string]bool{}
	requirements := make([]string, 0)
	for _, step := range f.Provision {
		for _, requirement := range step.RequiresPrerequisites {
			if !seen[requirement] {
				seen[requirement] = true
				requirements = append(requirements, requirement)
			}
		}
	}
	return requirements
}

func splitFixtureLifetimes(fixture *Fixture) (execution, run *Fixture) {
	execution = &Fixture{Tags: fixture.Tags}
	run = &Fixture{Tags: fixture.Tags}
	for _, step := range fixture.Provision {
		if step.Lifetime == FixtureLifetimeRun {
			run.Provision = append(run.Provision, step)
		} else {
			execution.Provision = append(execution.Provision, step)
		}
	}
	return execution, run
}

// StackPrerequisitesBySuite resolves each selected suite's stack closure and
// returns the primary-region prerequisite bundles needed by that closure.
func StackPrerequisitesBySuite(path string, suites []*scenario.Suite) (map[string][]string, error) {
	fixture, err := loadFixture(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(suites))
	for _, suite := range suites {
		planned, err := fixture.plan(suite.Needs)
		if err != nil {
			return nil, fmt.Errorf("plan shared stack for %s: %w", suite.Path, err)
		}
		selected := &Fixture{Provision: planned}
		result[suite.Path] = selected.prerequisiteRequirements()
	}
	return result, nil
}

// StackOptionalPrerequisitesBySuite returns prerequisite bundles used only by
// step-level fixture needs. They influence assignment preference but do not
// make the entire case unschedulable.
func StackOptionalPrerequisitesBySuite(path string, suites []*scenario.Suite) (map[string][]string, error) {
	fixture, err := loadFixture(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(suites))
	for _, suite := range suites {
		hard, err := fixture.plan(suite.Needs)
		if err != nil {
			return nil, fmt.Errorf("plan shared stack for %s: %w", suite.Path, err)
		}
		hardRequirements := map[string]bool{}
		for _, requirement := range (&Fixture{Provision: hard}).prerequisiteRequirements() {
			hardRequirements[requirement] = true
		}
		stepNeeds := make([]string, 0)
		for _, step := range suite.Steps {
			stepNeeds = append(stepNeeds, step.Needs...)
		}
		optional, err := fixture.plan(stepNeeds)
		if err != nil {
			return nil, fmt.Errorf("plan step shared stack for %s: %w", suite.Path, err)
		}
		for _, requirement := range (&Fixture{Provision: optional}).prerequisiteRequirements() {
			if !hardRequirements[requirement] {
				result[suite.Path] = append(result[suite.Path], requirement)
			}
		}
	}
	return result, nil
}

// StackStepsForSuites returns the shared-stack closure needed by the selected
// suites. Callers use it for preflight checks that must cover provision and
// teardown commands before any cloud mutation starts.
func StackStepsForSuites(path string, suites []*scenario.Suite) ([]ProvisionStep, error) {
	fixture, err := loadFixture(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	requested := make([]string, 0)
	for _, suite := range suites {
		needs := append([]string(nil), suite.Needs...)
		for _, step := range suite.Steps {
			needs = append(needs, step.Needs...)
		}
		for _, need := range needs {
			if seen[need] {
				continue
			}
			seen[need] = true
			requested = append(requested, need)
		}
	}
	return fixture.plan(requested)
}

// StackDependenciesBySuite expands every suite's direct needs into the
// topologically ordered fixture closure used by an actual run.
func StackDependenciesBySuite(path string, suites []*scenario.Suite) (map[string]SuiteFixtureDependencies, error) {
	result := make(map[string]SuiteFixtureDependencies, len(suites))
	if strings.TrimSpace(path) == "" {
		for _, suite := range suites {
			stepDirect := make(map[string][]string, len(suite.Steps))
			for _, step := range suite.Steps {
				stepDirect[step.Name] = append([]string(nil), step.Needs...)
			}
			result[suite.Path] = SuiteFixtureDependencies{Direct: append([]string(nil), suite.Needs...), Steps: stepDirect}
		}
		return result, nil
	}
	fixture, err := loadFixture(path)
	if err != nil {
		return nil, err
	}
	for _, suite := range suites {
		requested := append([]string(nil), suite.Needs...)
		stepDirect := make(map[string][]string, len(suite.Steps))
		for _, step := range suite.Steps {
			stepDirect[step.Name] = append([]string(nil), step.Needs...)
			requested = append(requested, step.Needs...)
		}
		planned, err := fixture.plan(requested)
		if err != nil {
			return nil, fmt.Errorf("plan shared stack for %s: %w", suite.Path, err)
		}
		dependencies := make([]FixtureDependency, 0, len(planned))
		for _, step := range planned {
			dependencies = append(dependencies, FixtureDependency{
				ID:       step.ID,
				Resource: step.Resource,
				Mode:     step.Mode,
				Lifetime: step.Lifetime,
				Needs:    append([]string(nil), step.Needs...),
			})
		}
		result[suite.Path] = SuiteFixtureDependencies{
			Direct:   append([]string(nil), suite.Needs...),
			Steps:    stepDirect,
			Fixtures: dependencies,
		}
	}
	return result, nil
}

// topoSort orders provision steps so each step's needs precede it.
func topoSort(steps []ProvisionStep) ([]ProvisionStep, error) {
	byID := make(map[string]ProvisionStep, len(steps))
	for _, s := range steps {
		if s.ID == "" {
			return nil, fmt.Errorf("provision step missing id")
		}
		if _, exists := byID[s.ID]; exists {
			return nil, fmt.Errorf("duplicate provision id %q", s.ID)
		}
		byID[s.ID] = s
	}
	var ordered []ProvisionStep
	state := map[string]int{} // 0=unseen 1=visiting 2=done
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("dependency cycle at %q", id)
		}
		s, ok := byID[id]
		if !ok {
			return fmt.Errorf("unknown dependency %q", id)
		}
		state[id] = 1
		for _, n := range s.Needs {
			if err := visit(n); err != nil {
				return err
			}
		}
		state[id] = 2
		ordered = append(ordered, s)
		return nil
	}
	for _, s := range steps {
		if err := visit(s.ID); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func validateCaptureProviders(steps []ProvisionStep) error {
	providers := map[string]string{}
	for _, step := range steps {
		for name := range step.Capture {
			if previous, exists := providers[name]; exists {
				return fmt.Errorf("stack capture %q is provided by both %q and %q", name, previous, step.ID)
			}
			providers[name] = step.ID
		}
	}
	return nil
}

// loadInputs reads fixtures/inputs/<product>-<resource>.yaml for a resource
// like "ecs/instance" -> "ecs-instance.yaml". Missing file yields an empty map.
func loadInputs(dir, resource string) (map[string]any, error) {
	name := strings.ReplaceAll(resource, "/", "-")
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(dir, name+ext)
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var m map[string]any
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if m == nil {
			m = map[string]any{}
		}
		return m, nil
	}
	return map[string]any{}, nil
}
