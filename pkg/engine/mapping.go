package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

type ExecutionContext struct {
	Input    map[string]any
	Context  map[string]any
	Current  any
	Captures map[string]CaptureResult
}

func ResolveRequest(mapping map[string]string, ctx ExecutionContext) (map[string]any, error) {
	resolved := map[string]any{}
	for key, expr := range mapping {
		value, ok, err := ResolveExpression(expr, ctx)
		if err != nil {
			return nil, err
		}
		if !ok || isEmpty(value) {
			continue
		}
		resolved[key] = value
	}
	return resolved, nil
}

func ResolveExpression(expr string, ctx ExecutionContext) (any, bool, error) {
	switch {
	case expr == "$":
		if ctx.Current != nil {
			return ctx.Current, true, nil
		}
		return ctx.Input, true, nil
	case strings.HasPrefix(expr, "$kv_"):
		name, arg, ok := conditionFunction(expr)
		if !ok {
			return nil, false, fmt.Errorf("invalid mapping expression %q", expr)
		}
		return resolveKeyValueFunction(name, arg, ctx)
	case strings.HasPrefix(expr, "$json(") && strings.HasSuffix(expr, ")"):
		value, ok, err := resolveExpressionArgument(strings.TrimSpace(expr[len("$json("):len(expr)-1]), ctx)
		if err != nil || !ok || isEmpty(value) {
			return nil, ok, err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, false, err
		}
		return string(raw), true, nil
	case strings.HasPrefix(expr, "$list(") && strings.HasSuffix(expr, ")"):
		value, ok, err := resolveExpressionArgument(strings.TrimSpace(expr[len("$list("):len(expr)-1]), ctx)
		if err != nil || !ok || isEmpty(value) {
			return nil, ok, err
		}
		items := stringListValue(value)
		if len(items) == 0 {
			return []string{fmt.Sprint(value)}, true, nil
		}
		return items, true, nil
	case strings.HasPrefix(expr, "$except(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$except(") : len(expr)-1])
		if len(args) != 2 {
			return nil, false, fmt.Errorf("mapping expression %q expects two arguments", expr)
		}
		left, ok, err := resolveExpressionArgument(strings.TrimSpace(args[0]), ctx)
		if err != nil || !ok {
			return nil, ok, err
		}
		right, ok, err := resolveExpressionArgument(strings.TrimSpace(args[1]), ctx)
		if err != nil {
			return nil, false, err
		}
		return exceptStrings(stringListValue(left), stringListValue(right)), true, nil
	case strings.HasPrefix(expr, "$prefixed_values(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$prefixed_values(") : len(expr)-1])
		if len(args) != 2 {
			return nil, false, fmt.Errorf("mapping expression %q expects two arguments", expr)
		}
		source, ok, err := resolveExpressionArgument(strings.TrimSpace(args[0]), ctx)
		if err != nil || !ok || isEmpty(source) {
			return nil, ok, err
		}
		prefix := strings.Trim(strings.TrimSpace(args[1]), `"'`)
		if prefix == "" {
			return nil, false, fmt.Errorf("mapping expression %q has empty prefix", expr)
		}
		return prefixedStringValues(source, prefix), true, nil
	case strings.HasPrefix(expr, "$matching_prefix(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$matching_prefix(") : len(expr)-1])
		if len(args) != 2 {
			return nil, false, fmt.Errorf("mapping expression %q expects two arguments", expr)
		}
		source, ok, err := resolveExpressionArgument(strings.TrimSpace(args[0]), ctx)
		if err != nil || !ok || isEmpty(source) {
			return nil, ok, err
		}
		prefix := strings.Trim(strings.TrimSpace(args[1]), `"'`)
		if prefix == "" {
			return nil, false, fmt.Errorf("mapping expression %q has empty prefix", expr)
		}
		return stringValuesByPrefix(source, prefix, true), true, nil
	case strings.HasPrefix(expr, "$not_matching_prefix(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$not_matching_prefix(") : len(expr)-1])
		if len(args) != 2 {
			return nil, false, fmt.Errorf("mapping expression %q expects two arguments", expr)
		}
		source, ok, err := resolveExpressionArgument(strings.TrimSpace(args[0]), ctx)
		if err != nil || !ok || isEmpty(source) {
			return nil, ok, err
		}
		prefix := strings.Trim(strings.TrimSpace(args[1]), `"'`)
		if prefix == "" {
			return nil, false, fmt.Errorf("mapping expression %q has empty prefix", expr)
		}
		return stringValuesByPrefix(source, prefix, false), true, nil
	case strings.HasPrefix(expr, "$captured_field(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$captured_field(") : len(expr)-1])
		if len(args) != 2 {
			return nil, false, fmt.Errorf("mapping expression %q expects two arguments", expr)
		}
		captureName := strings.Trim(strings.TrimSpace(args[0]), `"'`)
		fieldName := strings.Trim(strings.TrimSpace(args[1]), `"'`)
		if captureName == "" || fieldName == "" {
			return nil, false, fmt.Errorf("mapping expression %q expects capture and field names", expr)
		}
		return capturedFieldValues(ctx.Captures[captureName], fieldName), true, nil
	case strings.HasPrefix(expr, "$prefixed_kv_objects(") && strings.HasSuffix(expr, ")"):
		args := splitExpressionArgs(expr[len("$prefixed_kv_objects(") : len(expr)-1])
		if len(args) < 2 || len(args) > 3 {
			return nil, false, fmt.Errorf("mapping expression %q expects two or three arguments", expr)
		}
		source, ok, err := resolveExpressionArgument(strings.TrimSpace(args[0]), ctx)
		if err != nil || !ok || isEmpty(source) {
			return nil, ok, err
		}
		prefix := strings.Trim(strings.TrimSpace(args[1]), `"'`)
		if prefix == "" {
			return nil, false, fmt.Errorf("mapping expression %q has empty prefix", expr)
		}
		defaultKey := "cidr"
		if len(args) == 3 {
			defaultKey = strings.Trim(strings.TrimSpace(args[2]), `"'`)
		}
		if defaultKey == "" {
			return nil, false, fmt.Errorf("mapping expression %q has empty default key", expr)
		}
		objects, err := prefixedKeyValueObjects(source, prefix, defaultKey)
		return objects, true, err
	case strings.HasPrefix(expr, "$first(") && strings.HasSuffix(expr, ")"):
		value, ok, err := resolveExpressionArgument(strings.TrimSpace(expr[len("$first("):len(expr)-1]), ctx)
		if err != nil || !ok || isEmpty(value) {
			return nil, ok, err
		}
		items := listValue(value)
		if len(items) == 0 {
			return nil, false, nil
		}
		return items[0], true, nil
	case strings.HasPrefix(expr, "$."):
		source := any(ctx.Input)
		if ctx.Current != nil {
			source = ctx.Current
		}
		value, ok := ExtractPath(source, expr)
		return value, ok, nil
	case strings.HasPrefix(expr, "$input."):
		key := strings.TrimPrefix(expr, "$input.")
		if key == "" {
			return nil, false, fmt.Errorf("invalid mapping expression %q", expr)
		}
		if strings.Contains(key, ".") {
			value, ok := ExtractPath(ctx.Input, "$."+key)
			return value, ok, nil
		}
		value, ok := ctx.Input[key]
		return value, ok, nil
	case strings.HasPrefix(expr, "$context."):
		key := strings.TrimPrefix(expr, "$context.")
		if key == "" {
			return nil, false, fmt.Errorf("invalid mapping expression %q", expr)
		}
		if strings.Contains(key, ".") {
			value, ok := ExtractPath(ctx.Context, "$."+key)
			return value, ok, nil
		}
		value, ok := ctx.Context[key]
		return value, ok, nil
	case strings.HasPrefix(expr, "$"):
		return nil, false, fmt.Errorf("unsupported mapping expression %q", expr)
	default:
		return expr, true, nil
	}
}

func resolveKeyValueFunction(name string, arg string, ctx ExecutionContext) (any, bool, error) {
	value, ok, err := resolveExpressionArgument(arg, ctx)
	if err != nil || !ok || isEmpty(value) {
		return nil, ok, err
	}
	assignments, err := keyValueAssignments(value)
	if err != nil {
		return nil, false, err
	}
	if len(assignments) == 0 {
		return nil, false, nil
	}
	switch name {
	case "$kv_json":
		values := map[string]string{}
		for _, assignment := range assignments {
			values[assignment.key] = assignment.value
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return nil, false, err
		}
		return string(raw), true, nil
	case "$kv_pairs":
		values := make([]map[string]string, 0, len(assignments))
		for _, assignment := range assignments {
			values = append(values, map[string]string{"key": assignment.key, "value": assignment.value})
		}
		return values, true, nil
	case "$kv_key":
		if len(assignments) != 1 {
			return nil, false, ecerrors.Client("InvalidParameter", "expected exactly one tag")
		}
		return assignments[0].key, true, nil
	case "$kv_value":
		if len(assignments) != 1 {
			return nil, false, ecerrors.Client("InvalidParameter", "expected exactly one tag")
		}
		return assignments[0].value, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported mapping expression %q", name+"("+arg+")")
	}
}

type keyValueAssignment struct {
	key   string
	value string
}

func keyValueAssignments(value any) ([]keyValueAssignment, error) {
	values := stringListValue(value)
	assignments := make([]keyValueAssignment, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		key, val, ok := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !ok || key == "" {
			return nil, ecerrors.Client("InvalidParameter", "expected key=value")
		}
		if seen[key] {
			return nil, ecerrors.Client("InvalidParameter", "duplicate tag key: "+key)
		}
		seen[key] = true
		assignments = append(assignments, keyValueAssignment{key: key, value: val})
	}
	return assignments, nil
}

func splitExpressionArgs(raw string) []string {
	var args []string
	start := 0
	depth := 0
	var quote rune
	for index, char := range raw {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '(':
			depth++
		case char == ')' && depth > 0:
			depth--
		case char == ',' && depth == 0:
			args = append(args, strings.TrimSpace(raw[start:index]))
			start = index + 1
		}
	}
	args = append(args, strings.TrimSpace(raw[start:]))
	return args
}

func prefixedStringValues(source any, prefix string) []string {
	var values []string
	for _, item := range stringListValue(source) {
		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(item, prefix))
		if strings.TrimSpace(value) != "" {
			values = append(values, value)
		}
	}
	return values
}

func stringValuesByPrefix(source any, prefix string, matching bool) []string {
	var values []string
	for _, item := range stringListValue(source) {
		item = strings.TrimSpace(item)
		if item == "" || strings.HasPrefix(item, prefix) != matching {
			continue
		}
		values = append(values, item)
	}
	return values
}

func capturedFieldValues(capture CaptureResult, field string) []string {
	values := make([]string, 0, len(capture.Items))
	for _, item := range capture.Items {
		value := strings.TrimSpace(stringValue(item[field]))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func prefixedKeyValueObjects(source any, prefix string, defaultKey string) ([]any, error) {
	var objects []any
	for _, item := range stringListValue(source) {
		item = strings.TrimSpace(item)
		if !strings.HasPrefix(item, prefix) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(item, prefix))
		if body == "" {
			return nil, ecerrors.Client("InvalidParameter", "entry change must not be empty")
		}
		object, err := inlineKeyValueObject(body, defaultKey)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func inlineKeyValueObject(raw string, defaultKey string) (map[string]any, error) {
	object := map[string]any{}
	for index, entry := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
		value = strings.TrimSpace(value)
		if !ok {
			if index == 0 && key != "" {
				object[defaultKey] = key
				continue
			}
			return nil, ecerrors.Client("InvalidParameter", "entry change entries must be key=value")
		}
		if key == "" {
			return nil, ecerrors.Client("InvalidParameter", "entry change entries must include a key")
		}
		if _, exists := object[key]; exists {
			return nil, ecerrors.Client("InvalidParameter", "duplicate entry change key: "+key)
		}
		object[key] = value
	}
	return object, nil
}

func exceptStrings(left, right []string) []string {
	exclude := map[string]bool{}
	for _, value := range right {
		exclude[value] = true
	}
	result := make([]string, 0, len(left))
	for _, value := range left {
		if !exclude[value] {
			result = append(result, value)
		}
	}
	return result
}

func resolveExpressionArgument(arg string, ctx ExecutionContext) (any, bool, error) {
	switch {
	case strings.HasPrefix(arg, "$"):
		return ResolveExpression(arg, ctx)
	case strings.HasPrefix(arg, "input."), strings.HasPrefix(arg, "context."):
		return ResolveExpression("$"+arg, ctx)
	default:
		return nil, false, fmt.Errorf("unsupported mapping expression argument %q", arg)
	}
}

func ExtractPath(value any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	if path == "$" {
		return value, true
	}
	if !strings.HasPrefix(path, "$") {
		return nil, false
	}
	if !strings.HasPrefix(path, "$.") {
		return nil, false
	}
	current := value
	parts := strings.Split(strings.TrimPrefix(path, "$."), ".")
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, "[]") {
			return nil, false
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = next
		default:
			return nil, false
		}
	}
	return current, true
}

func ExtractString(value any, path string) string {
	raw, ok := ExtractPath(value, path)
	if !ok {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case []any:
		if len(typed) == 0 {
			return ""
		}
		value, _ := typed[0].(string)
		return value
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func isEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}
