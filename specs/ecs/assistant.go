package ecs

import (
	"strconv"
	"strings"

	spechooks "ecctl/specs"
)

func init() {
	spechooks.RegisterFieldMapper("ecs", "assistant", "agent_upgrade_enabled", normalizeAssistantAgentUpgradeEnabled)
}

// DescribeCloudAssistantSettings omits Enabled when the effective value is
// false. Keep the public boolean stable so callers can safely read-modify-write
// the setting without turning an omitted default into a missing field.
func normalizeAssistantAgentUpgradeEnabled(value any) any {
	config, ok := value.(map[string]any)
	if !ok {
		return false
	}
	raw, ok := config["Enabled"]
	if !ok {
		return false
	}
	if enabled, ok := raw.(bool); ok {
		return enabled
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(spechookString(raw)))
	return err == nil && enabled
}

func spechookString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
