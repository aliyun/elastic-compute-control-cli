package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ecctl/e2e/internal/scenario"

	"gopkg.in/yaml.v3"
)

// Fixture is the shared stack definition (fixtures/stack.yaml). Its provision
// steps create reusable resources requested by selected cases.
type Fixture struct {
	Tags      map[string]string `yaml:"tags"`
	Provision []ProvisionStep   `yaml:"provision"`
}

// ProvisionStep creates one shared-stack resource.
type ProvisionStep struct {
	ID                    string            `yaml:"id"`
	Needs                 []string          `yaml:"needs"`
	RequiresParams        []string          `yaml:"requires_params"`
	RequiresPrerequisites []string          `yaml:"requires_prerequisites"`
	Run                   string            `yaml:"run"`
	At                    string            `yaml:"at"`
	Capture               map[string]string `yaml:"capture"`
	Teardown              string            `yaml:"teardown"`
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
		for _, need := range suite.Needs {
			if seen[need] {
				continue
			}
			seen[need] = true
			requested = append(requested, need)
		}
	}
	return fixture.plan(requested)
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
