// Package coverage cross-checks the verbs (operations) declared by resource
// specs against the verbs actually exercised by E2E cases, and reports the gap
// — capabilities that exist but have no case. It is a reminder, not
// a generator.
package coverage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/shlex"
	"gopkg.in/yaml.v3"

	"ecctl/e2e/internal/scenario"
)

// Capability is one (resource, verb) pair.
type Capability struct {
	Resource string `json:"resource"` // "product/resource"
	Verb     string `json:"verb"`
}

func (c Capability) String() string { return c.Resource + " " + c.Verb }

// Report is the coverage result.
type Report struct {
	Declared int          `json:"declared"`
	Covered  int          `json:"covered"`
	Gaps     []Capability `json:"gaps"`
}

type specFile struct {
	Product    string               `yaml:"product"`
	Resource   string               `yaml:"resource"`
	Operations map[string]yaml.Node `yaml:"operations"`
}

// Analyze loads specs and cases and computes the coverage gap.
func Analyze(specsDir, casesDir string) (*Report, error) {
	declared, index, err := loadDeclared(specsDir)
	if err != nil {
		return nil, err
	}
	covered, err := loadCovered(casesDir, index)
	if err != nil {
		return nil, err
	}

	var gaps []Capability
	for cap := range declared {
		if !covered[cap] {
			gaps = append(gaps, cap)
		}
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Resource != gaps[j].Resource {
			return gaps[i].Resource < gaps[j].Resource
		}
		return gaps[i].Verb < gaps[j].Verb
	})
	return &Report{Declared: len(declared), Covered: len(declared) - len(gaps), Gaps: gaps}, nil
}

// loadDeclared returns the set of declared capabilities and a product->resource
// index used to resolve case commands.
func loadDeclared(specsDir string) (map[Capability]bool, map[string]map[string]bool, error) {
	declared := map[Capability]bool{}
	index := map[string]map[string]bool{} // product -> set of resource names

	err := filepath.WalkDir(specsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (filepath.Ext(p) != ".yaml" && filepath.Ext(p) != ".yml") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var sf specFile
		if yaml.Unmarshal(data, &sf) != nil {
			return nil // skip non-resource specs (product.yaml etc.)
		}
		if sf.Product == "" || sf.Resource == "" || len(sf.Operations) == 0 {
			return nil
		}
		resource := sf.Product + "/" + sf.Resource
		if index[sf.Product] == nil {
			index[sf.Product] = map[string]bool{}
		}
		index[sf.Product][sf.Resource] = true
		for verb := range sf.Operations {
			declared[Capability{Resource: resource, Verb: verb}] = true
		}
		return nil
	})
	return declared, index, err
}

func loadCovered(casesDir string, index map[string]map[string]bool) (map[Capability]bool, error) {
	suites, err := scenario.LoadDir(casesDir)
	if err != nil {
		return nil, err
	}
	covered := map[Capability]bool{}
	for _, s := range suites {
		for _, st := range s.Steps {
			if cap, ok := commandCapability(st.Run, index); ok {
				covered[cap] = true
			}
		}
	}
	return covered, nil
}

// commandCapability maps a full "ecctl ..." command line to a (resource, verb),
// using the spec index to decide whether the second token is a sub-resource.
// Raw `ecctl call ...` invocations are intentionally ignored.
func commandCapability(run string, index map[string]map[string]bool) (Capability, bool) {
	toks, err := shlex.Split(run)
	if err != nil || len(toks) == 0 || toks[0] != "ecctl" {
		return Capability{}, false
	}
	var pos []string
	for _, t := range toks[1:] {
		if strings.HasPrefix(t, "-") {
			break // stop at the first flag; positional verbs precede flags
		}
		pos = append(pos, t)
	}
	if len(pos) < 2 || pos[0] == "call" {
		return Capability{}, false
	}
	product := pos[0]
	resources := index[product]
	if resources == nil {
		return Capability{}, false
	}
	// <product> <parent> <resource> <verb>, for nested spec resources such as
	// `rg policy version create` and `ack diagnosis check-item list`.
	if len(pos) >= 4 && resources[pos[2]] {
		return Capability{Resource: product + "/" + pos[2], Verb: pos[3]}, true
	}
	// <product> <resource> <verb>
	if len(pos) >= 3 && resources[pos[1]] {
		return Capability{Resource: product + "/" + pos[1], Verb: pos[2]}, true
	}
	// <product> <verb> where the resource shares the product name
	if resources[product] {
		return Capability{Resource: product + "/" + product, Verb: pos[1]}, true
	}
	return Capability{}, false
}
