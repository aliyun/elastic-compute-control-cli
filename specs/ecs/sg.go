package ecs

import (
	"context"
	"fmt"
	"strings"

	ecerrors "ecctl/pkg/errors"
	spechooks "ecctl/specs"
)

func init() {
	spechooks.RegisterBindingItemNormalizer("ecs", "sg", "security_group_rule", normalizeSecurityGroupRule)
	spechooks.RegisterBeforeOperation("ecs", "sg", "normalize_security_group_rule_request", normalizeSecurityGroupRuleRequest)
}

func normalizeSecurityGroupRule(item map[string]any, index int) (map[string]any, error) {
	normalized := cloneMap(item)
	if raw := strings.TrimSpace(sgStringValue(normalized["value"])); raw != "" {
		defaultDirection := strings.TrimSpace(sgStringValue(normalized["direction"]))
		parsed, ok := parseSecurityGroupRule(raw)
		if !ok {
			return nil, ecerrors.Client("InvalidParameter", fmt.Sprintf("rule entry %d must match a supported rule format", index+1))
		}
		if defaultDirection != "" && !securityGroupRuleHasExplicitDirection(raw) {
			parsed["direction"] = defaultDirection
		}
		delete(normalized, "value")
		for key, value := range parsed {
			normalized[key] = value
		}
	}

	for _, field := range []string{"direction", "protocol", "policy"} {
		if value := strings.TrimSpace(sgStringValue(normalized[field])); value != "" {
			normalized[field] = strings.ToLower(value)
		}
	}
	if value := strings.TrimSpace(sgStringValue(normalized["port"])); value != "" {
		port := normalizeSecurityGroupRulePort(value)
		normalized["port"] = port
		normalized["port_range"] = securityGroupRulePortRange(port)
	}
	if value, ok := intHookValue(normalized["priority"]); ok {
		normalized["priority"] = value
	}
	for _, field := range []string{"protocol", "port", "cidr"} {
		if sgStringValue(normalized[field]) == "" {
			return nil, ecerrors.Client("MissingParameter", fmt.Sprintf("rule entry %d missing %s", index+1, field))
		}
	}
	return normalized, nil
}

func normalizeSecurityGroupRuleRequest(_ context.Context, _ spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	normalized := cloneMap(request)
	for _, field := range []string{"IpProtocol", "Policy"} {
		if value := strings.TrimSpace(sgStringValue(normalized[field])); value != "" {
			normalized[field] = strings.ToLower(value)
		}
	}
	if value := strings.TrimSpace(sgStringValue(normalized["PortRange"])); value != "" {
		normalized["PortRange"] = securityGroupRulePortRange(normalizeSecurityGroupRulePort(value))
	}
	if value, ok := intHookValue(normalized["Priority"]); ok {
		normalized["Priority"] = value
	}
	return normalized, nil
}

func parseSecurityGroupRule(value string) (map[string]any, bool) {
	if parsed, ok := parseColonSecurityGroupRule(value); ok {
		return parsed, true
	}
	left, cidr, ok := strings.Cut(value, "@")
	if !ok {
		return nil, false
	}
	parts := strings.Split(left, ":")
	if len(parts) != 2 {
		return nil, false
	}
	return map[string]any{
		"direction": "ingress",
		"protocol":  parts[0],
		"port":      parts[1],
		"cidr":      cidr,
	}, true
}

func parseColonSecurityGroupRule(value string) (map[string]any, bool) {
	parts := strings.Split(value, ":")
	switch len(parts) {
	case 3:
		return map[string]any{
			"direction": "ingress",
			"protocol":  parts[0],
			"port":      parts[1],
			"cidr":      parts[2],
		}, true
	case 4:
		return map[string]any{
			"direction": parts[0],
			"protocol":  parts[1],
			"port":      parts[2],
			"cidr":      parts[3],
		}, true
	default:
		return nil, false
	}
}

func securityGroupRuleHasExplicitDirection(value string) bool {
	if strings.Contains(value, "@") {
		return false
	}
	return len(strings.Split(value, ":")) == 4
}

func normalizeSecurityGroupRulePort(value string) string {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		if parts[0] == parts[1] {
			return parts[0]
		}
		return parts[0] + "-" + parts[1]
	}
	return value
}

func securityGroupRulePortRange(value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(value, "/") {
		return value
	}
	// Skip leading minus (negative ports like -1 for ICMP) when looking for range separator.
	search := value
	offset := 0
	if strings.HasPrefix(search, "-") {
		search = search[1:]
		offset = 1
	}
	if idx := strings.Index(search, "-"); idx >= 0 {
		return value[:offset+idx] + "/" + value[offset+idx+1:]
	}
	return value + "/" + value
}

func intHookValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case uint64:
		return int(typed), true
	case uint:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		var parsed int
		if _, err := fmt.Sscan(typed, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func sgStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}
