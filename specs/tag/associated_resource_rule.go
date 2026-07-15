package tag

import (
	"context"
	"fmt"
	"strings"

	spechooks "ecctl/specs"
)

func init() {
	spechooks.RegisterBeforeOperation("tag", "associated-resource-rule", "preserve_update_fields", preserveAssociatedResourceRuleUpdateFields)
}

func preserveAssociatedResourceRuleUpdateFields(ctx context.Context, caller spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	settingName := tagStringValue(request["SettingName"])
	if settingName == "" {
		return request, nil
	}
	needsStatus := !tagRequestHasValue(request, "Status")
	needsExistingStatus := !tagRequestHasValue(request, "ExistingStatus")
	needsTagKeys := !tagRequestHasPrefix(request, "TagKeys")
	if !needsStatus && !needsExistingStatus && !needsTagKeys {
		return request, nil
	}

	response, err := caller.CallRaw(ctx, "ListAssociatedResourceRules", map[string]any{
		"RegionId":      tagStringValue(request["RegionId"]),
		"SettingName.1": settingName,
	})
	if err != nil {
		return nil, err
	}
	rule := associatedResourceRule(response, settingName)
	if rule == nil {
		return request, nil
	}

	resolved := tagCloneMap(request)
	if needsStatus {
		if status := tagStringValue(rule["Status"]); status != "" {
			resolved["Status"] = status
		}
	}
	if needsExistingStatus {
		if existingStatus := tagStringValue(rule["ExistingStatus"]); existingStatus != "" {
			resolved["ExistingStatus"] = existingStatus
		}
	}
	if needsTagKeys {
		for index, value := range tagStringListValue(rule["TagKeys"]) {
			resolved[fmt.Sprintf("TagKeys.%d", index+1)] = value
		}
	}
	return resolved, nil
}

func associatedResourceRule(response map[string]any, settingName string) map[string]any {
	for _, item := range tagListValue(response["Rules"]) {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tagStringValue(rule["SettingName"]) == settingName {
			return rule
		}
	}
	return nil
}

func tagRequestHasValue(request map[string]any, key string) bool {
	value, ok := request[key]
	return ok && tagStringValue(value) != ""
}

func tagRequestHasPrefix(request map[string]any, prefix string) bool {
	for key, value := range request {
		if (key == prefix || strings.HasPrefix(key, prefix+".")) && tagStringValue(value) != "" {
			return true
		}
	}
	return false
}

func tagCloneMap(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func tagStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func tagStringListValue(value any) []string {
	items := tagListValue(value)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value := tagStringValue(item); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func tagListValue(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	default:
		return nil
	}
}
