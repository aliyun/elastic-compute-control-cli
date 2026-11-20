// Package journalcmd validates commands before they are persisted or replayed
// by the unattended E2E cleanup journal.
package journalcmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
)

// ValidateDelete accepts first-class ecctl delete commands, the two exact OSS
// cleanup calls needed by E2E-owned buckets, and the exact account-setting
// restore used by the associated-transfer lifecycle. Other mutations remain
// forbidden because cleanup journals can be replayed unattended.
func ValidateDelete(command string) error {
	tokens, err := shlex.Split(command)
	if err != nil {
		return err
	}
	if len(tokens) < 2 || tokens[0] != "ecctl" {
		return fmt.Errorf("teardown must be an ecctl command")
	}
	if volumeDeleteCandidate(tokens) {
		return validateVolumeDelete(tokens)
	}
	if len(tokens) >= 4 && tokens[1] == "rg" && tokens[2] == "associated-transfer" && tokens[3] == "update" {
		return validateAssociatedTransferRestore(tokens)
	}
	if ordinaryTeamDeletePath(tokens) {
		return nil
	}
	if tokens[1] != "call" {
		for _, token := range tokens[1:] {
			if strings.HasPrefix(token, "-") {
				break
			}
			if token == "delete" {
				return nil
			}
		}
		return fmt.Errorf("teardown must be a delete command")
	}
	return validateOSSDelete(tokens)
}

// Normalize only spellings exposed by the public command tree. The original
// tokens remain unchanged for execution and exact ownership comparisons.
func canonicalReplayPositionals(positionals []string) []string {
	if len(positionals) == 0 {
		return positionals
	}
	canonical := append([]string(nil), positionals...)
	if canonical[0] == "sbx" {
		canonical[0] = "sandbox"
	}
	if canonical[0] == "ack" && len(canonical) >= 2 {
		if canonical[1] == "cluster" {
			canonical = append(canonical[:1], canonical[2:]...)
		} else if canonical[1] == "np" {
			canonical[1] = "nodepool"
		}
	}
	if canonical[0] == "lingjun" && len(canonical) >= 2 && canonical[1] == "ng" {
		canonical[1] = "node-group"
	}
	if canonical[0] == "rg" && len(canonical) >= 2 && canonical[1] == "slr" {
		canonical[1] = "service-linked-role"
	}
	if canonical[0] == "vpc" && len(canonical) >= 2 && canonical[1] == "vsw" {
		canonical[1] = "vswitch"
	}
	if canonical[0] == "sandbox" && len(canonical) >= 2 {
		if len(canonical) >= 3 && canonical[1] == "sandbox" {
			canonical = append(canonical[:1], canonical[2:]...)
		}
		switch canonical[1] {
		case "tpl":
			canonical[1] = "template"
		case "tcache":
			canonical[1] = "template-cache"
		}
		if len(canonical) >= 3 && (canonical[1] == "team" || canonical[1] == "template") && (canonical[2] == "rm" || canonical[2] == "remove") {
			canonical[2] = "delete"
		}
	}
	if canonical[0] == "tag" && len(canonical) >= 2 && canonical[1] == "arr" {
		canonical[1] = "associated-resource-rule"
	}
	return canonical
}

// Native Volume cleanup is reserved by the runner. Only its canonical exact
// target command can enter a replay journal; optional Team selectors would be
// stale after the lifecycle transfers ownership.
func volumeDeleteCandidate(tokens []string) bool {
	for i := 1; i+1 < len(tokens); i++ {
		if (tokens[i] != "sandbox" && tokens[i] != "sbx") || tokens[i+1] != "volume" {
			continue
		}
		for _, token := range tokens[i+2:] {
			if token == "delete" || token == "rm" || token == "remove" {
				return true
			}
		}
	}
	return false
}

func validateVolumeDelete(tokens []string) error {
	if len(tokens) != 9 || tokens[1] != "sandbox" || tokens[2] != "volume" || tokens[3] != "delete" || tokens[5] != "--region" || tokens[7] != "--profile" || tokens[6] == "" || tokens[8] == "" || !safeVolumeDeleteID(tokens[4]) {
		return fmt.Errorf("native Volume teardown requires one exact ID, region and profile")
	}
	return nil
}

func safeVolumeDeleteID(id string) bool {
	return id != "" && id[0] != '-' && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}

// A known global flag may precede or separate the ordinary Team delete path.
// Target, region, and ownership validation remain with the journalfile parser.
func ordinaryTeamDeletePath(tokens []string) bool {
	pathParts := 0
	for index := 1; index < len(tokens); index++ {
		arg := tokens[index]
		if arg == "--" {
			return false
		}
		if strings.HasPrefix(arg, "-") {
			key, value, inline := strings.Cut(arg, "=")
			switch key {
			case "--region", "--lang", "--output":
				if !inline {
					index++
					if index >= len(tokens) {
						return false
					}
					value = tokens[index]
				}
				if value == "" {
					return false
				}
			case "--json", "--no-color", "--agent-envelope":
				if inline {
					if _, err := strconv.ParseBool(value); err != nil {
						return false
					}
				}
			default:
				return false
			}
			continue
		}
		switch pathParts {
		case 0:
			if arg != "sandbox" && arg != "sbx" {
				return false
			}
		case 1:
			if arg != "team" {
				return false
			}
		case 2:
			return arg == "delete" || arg == "rm" || arg == "remove"
		}
		pathParts++
	}
	return false
}

func validateAssociatedTransferRestore(tokens []string) error {
	allowed := map[string]bool{
		"--status":                             true,
		"--enable-existing-resources-transfer": true,
	}
	seen := map[string]string{}
	for index := 4; index < len(tokens); index++ {
		flag, value, hasEquals := strings.Cut(tokens[index], "=")
		if !allowed[flag] {
			return fmt.Errorf("associated-transfer restore parameter %q is not allowed", tokens[index])
		}
		if _, duplicated := seen[flag]; duplicated {
			return fmt.Errorf("associated-transfer restore parameter %q is duplicated", flag)
		}
		if !hasEquals {
			index++
			if index >= len(tokens) {
				return fmt.Errorf("associated-transfer restore parameter %q requires a value", flag)
			}
			value = tokens[index]
		}
		seen[flag] = value
	}
	if status := seen["--status"]; status != "Enable" && status != "Disable" {
		return fmt.Errorf("associated-transfer restore requires --status Enable or Disable")
	}
	if existing := seen["--enable-existing-resources-transfer"]; existing != "true" && existing != "false" {
		return fmt.Errorf("associated-transfer restore requires --enable-existing-resources-transfer true or false")
	}
	return nil
}

func validateOSSDelete(tokens []string) error {
	if len(tokens) < 4 || tokens[2] != "oss" {
		return fmt.Errorf("raw API cleanup is only allowed for OSS")
	}
	operation := tokens[3]
	required := map[string]bool{"--Bucket": true}
	switch operation {
	case "DeleteBucket":
	case "DeleteObject":
		required["--Key"] = true
	default:
		return fmt.Errorf("OSS cleanup operation %q is not allowed", operation)
	}
	allowed := map[string]bool{"--Bucket": true, "--Key": operation == "DeleteObject", "--region": true}
	seen := map[string]bool{}
	for index := 4; index < len(tokens); index++ {
		flag, value, hasEquals := strings.Cut(tokens[index], "=")
		if !strings.HasPrefix(flag, "--") || !allowed[flag] {
			return fmt.Errorf("OSS cleanup parameter %q is not allowed", tokens[index])
		}
		if seen[flag] {
			return fmt.Errorf("OSS cleanup parameter %q is duplicated", flag)
		}
		seen[flag] = true
		if !hasEquals {
			index++
			if index >= len(tokens) {
				return fmt.Errorf("OSS cleanup parameter %q requires a value", flag)
			}
			value = tokens[index]
		}
		if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") && (flag != "--Key" || !hasEquals) {
			return fmt.Errorf("OSS cleanup parameter %q requires a value", flag)
		}
	}
	for flag := range required {
		if !seen[flag] {
			return fmt.Errorf("OSS cleanup operation %s requires %s", operation, flag)
		}
	}
	return nil
}
