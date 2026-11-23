package journalcmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
)

// SandboxDeleteRoute resolves the CLI path and effective Template transport
// without treating a profile selector as an OpenAPI switch. Validation of the
// full mutation grammar remains the caller's responsibility.
type SandboxDeleteRoute struct {
	Resource string
	Target   string
	Profile  string
	Region   string
	Native   bool
}

// ParseSandboxDeleteRoute recognizes the default Sandbox and Template delete
// paths even while a case target is still an unrendered template expression.
func ParseSandboxDeleteRoute(command string) (SandboxDeleteRoute, bool, error) {
	tokens, err := shlex.Split(command)
	if err != nil || len(tokens) < 3 || tokens[0] != "ecctl" {
		return SandboxDeleteRoute{}, false, err
	}
	positionals := make([]string, 0, 4)
	var route SandboxDeleteRoute
	seen := map[string]bool{}
	teamSelected := false
	for index := 1; index < len(tokens); index++ {
		flag, value, inline := strings.Cut(tokens[index], "=")
		if !strings.HasPrefix(flag, "-") {
			positionals = append(positionals, tokens[index])
			continue
		}
		if seen[flag] {
			return route, false, fmt.Errorf("Sandbox delete repeats %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--profile", "--region", "--team-id", "--output", "--lang", "--timeout":
			if !inline {
				index++
				if index >= len(tokens) {
					return route, false, fmt.Errorf("Sandbox delete lacks %s value", flag)
				}
				value = tokens[index]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return route, false, fmt.Errorf("Sandbox delete has an invalid %s value", flag)
			}
			switch flag {
			case "--profile":
				route.Profile = value
			case "--region":
				route.Region = value
			case "--team-id":
				teamSelected = true
			}
		case "--openapi":
			route.Native = true
			if inline {
				route.Native, err = strconv.ParseBool(value)
				if err != nil {
					return route, false, fmt.Errorf("Sandbox delete has an invalid OpenAPI selector")
				}
			}
		case "--json", "--agent-envelope", "--no-color", "--force":
			if inline {
				if _, err := strconv.ParseBool(value); err != nil {
					return route, false, fmt.Errorf("Sandbox delete has an invalid %s value", flag)
				}
			}
		default:
			return route, false, fmt.Errorf("Sandbox delete option %s is unsupported", flag)
		}
	}
	positionals = canonicalReplayPositionals(positionals)
	switch {
	case len(positionals) == 3 && positionals[0] == "sandbox" && positionals[1] == "delete":
		route.Resource, route.Target = "sandbox", positionals[2]
		if route.Native || teamSelected || seen["--openapi"] {
			return route, true, fmt.Errorf("default Sandbox delete cannot select Template transport")
		}
	case len(positionals) == 4 && positionals[0] == "sandbox" && positionals[1] == "template" && positionals[2] == "delete":
		route.Resource, route.Target = "template", positionals[3]
		if teamSelected && !route.Native {
			return route, true, fmt.Errorf("E2B Template delete cannot select a Team")
		}
	default:
		return SandboxDeleteRoute{}, false, nil
	}
	return route, true, nil
}
