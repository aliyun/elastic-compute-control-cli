// Package surface validates that selected E2E cases only invoke operations
// exposed by the ecctl binary selected for the run.
package surface

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/shlex"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

type Capabilities struct {
	Products []Product `json:"products"`
	Surface  string    `json:"surface"`
}

type Product struct {
	Name      string     `json:"product"`
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

type Issue struct {
	Path    string
	Step    string
	Code    string
	Message string
}

type resourceActions map[string]map[string]map[string]bool

func Decode(data []byte) (Capabilities, error) {
	var caps Capabilities
	if err := json.Unmarshal(data, &caps); err != nil {
		return Capabilities{}, fmt.Errorf("capabilities JSON: %w", err)
	}
	if len(caps.Products) == 0 {
		return Capabilities{}, fmt.Errorf("capabilities JSON has no products")
	}
	return caps, nil
}

func LoadFromBinary(ctx context.Context, bin string) (Capabilities, error) {
	result := execpkg.Run(ctx, execpkg.Config{Bin: bin}, "ecctl capabilities --output json")
	if result.Err != nil {
		return Capabilities{}, result.Err
	}
	if result.Exit != 0 {
		return Capabilities{}, fmt.Errorf("capabilities command exited %d: %s", result.Exit, strings.TrimSpace(result.Stderr))
	}
	data, err := json.Marshal(result.JSON)
	if err != nil {
		return Capabilities{}, fmt.Errorf("encode capabilities output: %w", err)
	}
	return Decode(data)
}

func ValidateSuites(suites []*scenario.Suite, caps Capabilities) []Issue {
	index := indexCapabilities(caps)
	var issues []Issue
	for _, suite := range suites {
		for _, step := range suite.Steps {
			for _, command := range []struct {
				label string
				run   string
			}{
				{label: step.Name, run: step.Run},
				{label: step.Name + " teardown", run: step.Teardown},
			} {
				if strings.TrimSpace(command.run) == "" {
					continue
				}
				resource, action, code, err := commandCapability(command.run, index)
				if err != nil {
					issues = append(issues, Issue{Path: suite.Path, Step: command.label, Code: code, Message: err.Error()})
					continue
				}
				_ = resource
				_ = action
			}
		}
	}
	return issues
}

func indexCapabilities(caps Capabilities) resourceActions {
	index := resourceActions{}
	for _, product := range caps.Products {
		if index[product.Name] == nil {
			index[product.Name] = map[string]map[string]bool{}
		}
		for _, resource := range product.Resources {
			actions := map[string]bool{}
			for _, action := range resource.Actions {
				actions[action] = true
			}
			index[product.Name][resource.Name] = actions
		}
	}
	return index
}

func commandCapability(command string, index resourceActions) (resource, action, code string, err error) {
	tokens, splitErr := shlex.Split(command)
	if splitErr != nil {
		return "", "", "invalid_command", splitErr
	}
	if len(tokens) == 0 || tokens[0] != "ecctl" {
		return "", "", "invalid_command", fmt.Errorf("command must start with ecctl")
	}
	positionals := commandPositionals(tokens[1:])
	if len(positionals) == 0 || positionals[0] == "call" {
		return "", "", "", nil
	}
	product := positionals[0]
	resources, ok := index[product]
	if !ok {
		return "", "", "unsupported_product", fmt.Errorf("product %q is not exposed by selected binary", product)
	}

	resourceNames := make([]string, 0, len(resources))
	for name := range resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Slice(resourceNames, func(i, j int) bool { return len(resourceNames[i]) > len(resourceNames[j]) })

	// Nested resources are identified by their final resource token, for
	// example `ack diagnosis check-item list`. A resource sharing its product
	// name (for example `ack list`) is the default when no resource token is
	// present.
	for _, name := range resourceNames {
		if name == product {
			continue
		}
		for i := 1; i < len(positionals); i++ {
			if positionals[i] != name {
				continue
			}
			if verb := firstAction(positionals[i+1:], resources[name]); verb != "" {
				return product + "/" + name, verb, "", nil
			}
			return "", "", "unsupported_action", fmt.Errorf("action after resource %q is not exposed by selected binary", name)
		}
	}
	if actions, ok := resources[product]; ok {
		if verb := firstAction(positionals[1:], actions); verb != "" {
			return product + "/" + product, verb, "", nil
		}
	}

	// If a resource-looking token is present but not exposed, report that as a
	// resource error instead of misclassifying it as an action.
	if len(positionals) >= 2 {
		return "", "", "unsupported_resource", fmt.Errorf("resource %q is not exposed for product %q", positionals[1], product)
	}
	return "", "", "unsupported_action", fmt.Errorf("could not map command to an exposed action: %s", command)
}

func commandPositionals(tokens []string) []string {
	positionals := make([]string, 0, len(tokens))
	skipNext := false
	for _, token := range tokens {
		if skipNext {
			skipNext = false
			continue
		}
		if token == "--" {
			continue
		}
		if strings.HasPrefix(token, "--") {
			name := token
			hasInlineValue := false
			if index := strings.Index(token, "="); index >= 0 {
				name = token[:index]
				hasInlineValue = true
			}
			if commandValueFlag(name) && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		positionals = append(positionals, token)
	}
	return positionals
}

func commandValueFlag(name string) bool {
	switch name {
	case "--fields", "--filter", "--limit", "--region", "--profile", "--lang", "--output", "--request", "--api-param":
		return true
	default:
		return false
	}
}

func firstAction(tokens []string, actions map[string]bool) string {
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			break
		}
		if actions[token] {
			return token
		}
	}
	return ""
}
