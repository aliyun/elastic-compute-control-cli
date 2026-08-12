// Package journalcmd validates commands before they are persisted or replayed
// by the unattended E2E cleanup journal.
package journalcmd

import (
	"fmt"
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
	if len(tokens) >= 4 && tokens[1] == "rg" && tokens[2] == "associated-transfer" && tokens[3] == "update" {
		return validateAssociatedTransferRestore(tokens)
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
		if strings.TrimSpace(value) == "" {
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
