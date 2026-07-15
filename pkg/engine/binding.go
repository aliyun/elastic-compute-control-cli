package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/spec"
	spechooks "ecctl/specs"
)

type CaptureResult struct {
	Items   []map[string]any
	Request map[string]any
}

func ResolveBindingRequest(binding spec.Binding, ctx ExecutionContext) (map[string]any, map[string]CaptureResult, error) {
	return ResolveResourceBindingRequest(spec.ResourceSpec{}, binding, ctx)
}

func ResolveResourceBindingRequest(resource spec.ResourceSpec, binding spec.Binding, ctx ExecutionContext) (map[string]any, map[string]CaptureResult, error) {
	resolved := map[string]any{}
	captures := map[string]CaptureResult{}
	if err := expandBindingFields(resource, resolved, captures, "", binding.Request, ctx); err != nil {
		return nil, nil, err
	}
	return resolved, captures, nil
}

func expandBindingFields(resource spec.ResourceSpec, request map[string]any, captures map[string]CaptureResult, prefix string, fields map[string]any, ctx ExecutionContext) error {
	for key, node := range fields {
		if isRawBindingNode(node) {
			continue
		}
		nextPrefix := key
		if prefix != "" {
			nextPrefix = prefix + "." + key
		}
		if err := expandBindingNode(resource, request, captures, nextPrefix, node, ctx); err != nil {
			return err
		}
	}
	for key, node := range fields {
		if !isRawBindingNode(node) {
			continue
		}
		nextPrefix := key
		if prefix != "" {
			nextPrefix = prefix + "." + key
		}
		if err := expandBindingNode(resource, request, captures, nextPrefix, node, ctx); err != nil {
			return err
		}
	}
	return nil
}

func expandBindingNode(resource spec.ResourceSpec, request map[string]any, captures map[string]CaptureResult, prefix string, node any, ctx ExecutionContext) error {
	switch typed := node.(type) {
	case string:
		value, ok, err := ResolveExpression(typed, ctx)
		if err != nil {
			return err
		}
		if ok && !isEmpty(value) {
			request[prefix] = value
		}
		return nil
	case map[string]any:
		if rawExpr, ok := typed["raw"].(string); ok {
			return expandRawBinding(request, rawExpr, ctx)
		}
		if fromExpr, ok := typed["from"].(string); ok {
			return expandFromBinding(resource, request, captures, prefix, fromExpr, mapValue(typed["fields"]), ctx)
		}
		if eachSpec, ok := typed["each"]; ok {
			return expandEachBinding(resource, request, captures, prefix, eachSpec, mapValue(typed["fields"]), typed["capture"], ctx)
		}
		return expandBindingFields(resource, request, captures, prefix, typed, ctx)
	default:
		if !isEmpty(typed) {
			request[prefix] = typed
		}
		return nil
	}
}

func expandFromBinding(resource spec.ResourceSpec, request map[string]any, captures map[string]CaptureResult, prefix, expr string, fields map[string]any, ctx ExecutionContext) error {
	value, ok, err := ResolveExpression(expr, ctx)
	if err != nil {
		return err
	}
	if !ok || isEmpty(value) {
		return nil
	}
	object, ok := objectValue(value)
	if !ok {
		return ecerrors.Client("InvalidParameter", fmt.Sprintf("%s must resolve to an object", expr))
	}
	if len(fields) == 0 {
		request[prefix] = jsonObjectString(value, object)
		return nil
	}
	return expandBindingFields(resource, request, captures, prefix, fields, withCurrent(ctx, object))
}

func expandEachBinding(resource spec.ResourceSpec, request map[string]any, captures map[string]CaptureResult, prefix string, eachSpec any, fields map[string]any, captureSpec any, ctx ExecutionContext) error {
	items, err := eachItems(resource, eachSpec, ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	before := requestKeys(request)
	for i, item := range items {
		itemPrefix := prefix + "." + strconv.Itoa(i+1)
		if len(fields) == 0 {
			if !isEmpty(item) {
				request[itemPrefix] = item
			}
			continue
		}
		current := item
		if object, ok := objectValue(item); ok {
			current = object
		}
		if err := expandBindingFields(resource, request, captures, itemPrefix, fields, withCurrent(ctx, current)); err != nil {
			return err
		}
	}

	if captureName, captureFields, ok := parseCaptureSpec(captureSpec); ok {
		capturedItems, err := captureItems(items, captureFields, ctx)
		if err != nil {
			return err
		}
		captures[captureName] = CaptureResult{
			Items:   capturedItems,
			Request: requestDelta(request, before),
		}
	}
	return nil
}

func eachItems(resource spec.ResourceSpec, eachSpec any, ctx ExecutionContext) ([]any, error) {
	switch typed := eachSpec.(type) {
	case string:
		value, ok, err := ResolveExpression(typed, ctx)
		if err != nil || !ok || isEmpty(value) {
			return nil, err
		}
		return listValue(value), nil
	case map[string]any:
		return enhancedEachItems(resource, typed, ctx)
	default:
		return nil, fmt.Errorf("each must be a string expression or object")
	}
}

func enhancedEachItems(resource spec.ResourceSpec, spec map[string]any, ctx ExecutionContext) ([]any, error) {
	sources := listValue(spec["sources"])
	defaults := mapValue(spec["defaults"])
	enum := enumMap(spec["enum"])
	normalize, _ := spec["normalize"].(string)

	var items []any
	for sourceIndex, source := range sources {
		sourceMap, ok := source.(map[string]any)
		if !ok {
			return nil, ecerrors.Client("InvalidParameter", fmt.Sprintf("each source %d must be an object", sourceIndex+1))
		}
		sourceItems, err := enhancedSourceItems(sourceMap, ctx)
		if err != nil {
			return nil, err
		}
		for i, item := range sourceItems {
			normalized, err := normalizeEnhancedEachItem(resource, normalize, item, defaults, enum, i)
			if err != nil {
				return nil, err
			}
			items = append(items, normalized)
		}
	}
	return items, nil
}

func enhancedSourceItems(source map[string]any, ctx ExecutionContext) ([]map[string]any, error) {
	if expr, ok := source["source"].(string); ok {
		value, found, err := ResolveExpression(expr, ctx)
		if err != nil || !found || isEmpty(value) {
			return nil, err
		}
		values := stringListValue(value)
		items := make([]map[string]any, 0, len(values))
		for _, raw := range values {
			items = append(items, map[string]any{"value": raw})
		}
		return items, nil
	}
	if fields := mapValue(source["from_fields"]); len(fields) > 0 {
		item := map[string]any{}
		for field, exprValue := range fields {
			expr, ok := exprValue.(string)
			if !ok {
				return nil, fmt.Errorf("from_fields.%s must be a string expression", field)
			}
			value, found, err := ResolveExpression(expr, ctx)
			if err != nil {
				return nil, err
			}
			if found && !isEmpty(value) {
				item[field] = value
			}
		}
		if !whenAnyMatched(item, listValue(source["when_any"])) {
			return nil, nil
		}
		return []map[string]any{item}, nil
	}
	return nil, fmt.Errorf("each source must define source or from_fields")
}

func normalizeEnhancedEachItem(resource spec.ResourceSpec, normalize string, item map[string]any, defaults map[string]any, enum map[string][]string, index int) (map[string]any, error) {
	normalized := mergeFields(defaults, item)
	if normalize != "" {
		mapped, ok, err := spechooks.NormalizeBindingItem(resource.Product, resource.Resource, normalize, normalized, index)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ecerrors.Client("UnknownHook", fmt.Sprintf("binding item normalizer %q is not registered", normalize))
		}
		normalized = mapped
	}
	for field, allowed := range enum {
		value := stringValue(normalized[field])
		if value == "" || len(allowed) == 0 {
			continue
		}
		if !containsString(allowed, value) {
			return nil, ecerrors.Client("InvalidParameter", fmt.Sprintf("rule entry %d %s must be one of %s", index+1, field, strings.Join(allowed, ", ")))
		}
	}
	return normalized, nil
}

func expandRawBinding(request map[string]any, expr string, ctx ExecutionContext) error {
	value, ok, err := ResolveExpression(expr, ctx)
	if err != nil || !ok || isEmpty(value) {
		return err
	}
	for _, item := range stringListValue(value) {
		key, raw, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return ecerrors.Client("InvalidParameter", fmt.Sprintf("%s must be key=value", expr))
		}
		if _, exists := request[key]; exists {
			return ecerrors.Client("DuplicateParameter", fmt.Sprintf("raw API parameter %q duplicates an existing request field", key))
		}
		request[key] = strings.TrimSpace(raw)
	}
	return nil
}

func captureItems(items []any, fields map[string]any, ctx ExecutionContext) ([]map[string]any, error) {
	captured := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if len(fields) == 0 {
			if object, ok := objectValue(item); ok {
				captured = append(captured, cloneMap(object))
			} else {
				captured = append(captured, map[string]any{"value": item})
			}
			continue
		}
		capture := map[string]any{}
		for key, exprValue := range fields {
			expr, ok := exprValue.(string)
			if !ok {
				continue
			}
			value, found, err := ResolveExpression(expr, withCurrent(ctx, item))
			if err != nil {
				return nil, err
			}
			if found && !isEmpty(value) {
				capture[key] = value
			}
		}
		captured = append(captured, capture)
	}
	return captured, nil
}

func parseCaptureSpec(spec any) (string, map[string]any, bool) {
	switch typed := spec.(type) {
	case string:
		return typed, nil, typed != ""
	case map[string]any:
		name, _ := typed["name"].(string)
		if name == "" {
			return "", nil, false
		}
		return name, mapValue(typed["fields"]), true
	default:
		return "", nil, false
	}
}

func requestKeys(request map[string]any) map[string]struct{} {
	keys := make(map[string]struct{}, len(request))
	for key := range request {
		keys[key] = struct{}{}
	}
	return keys
}

func requestDelta(request map[string]any, before map[string]struct{}) map[string]any {
	result := map[string]any{}
	for key, value := range request {
		if _, ok := before[key]; !ok {
			result[key] = value
		}
	}
	return result
}

func isRawBindingNode(node any) bool {
	fields, ok := node.(map[string]any)
	if !ok {
		return false
	}
	_, ok = fields["raw"]
	return ok
}

func withCurrent(ctx ExecutionContext, current any) ExecutionContext {
	return ExecutionContext{
		Input:    ctx.Input,
		Context:  ctx.Context,
		Current:  current,
		Captures: ctx.Captures,
	}
}

func objectValue(value any) (map[string]any, bool) {
	value = decodeJSONValue(value)
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func listValue(value any) []any {
	value = decodeJSONValue(value)
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return typed
	case []string:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return []any{typed}
	}
}

func decodeJSONValue(value any) any {
	raw, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || (trimmed[0] != '[' && trimmed[0] != '{') {
		return value
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return value
	}
	return decoded
}

func jsonObjectString(original any, object map[string]any) string {
	if raw, ok := original.(string); ok {
		return strings.TrimSpace(raw)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return fmt.Sprint(original)
	}
	return string(encoded)
}

func mapValue(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	case map[string]string:
		result := map[string]any{}
		for key, value := range typed {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

func enumMap(value any) map[string][]string {
	raw := mapValue(value)
	if len(raw) == 0 {
		return nil
	}
	result := map[string][]string{}
	for field, values := range raw {
		for _, value := range listValue(values) {
			if text := stringValue(value); text != "" {
				result[field] = append(result[field], text)
			}
		}
	}
	return result
}

func whenAnyMatched(item map[string]any, fields []any) bool {
	if len(fields) == 0 {
		return true
	}
	for _, field := range fields {
		if !isEmpty(item[stringValue(field)]) {
			return true
		}
	}
	return false
}

func stringListValue(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				result = append(result, item)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			value := stringValue(item)
			if strings.TrimSpace(value) != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func mergeFields(values ...map[string]any) map[string]any {
	result := map[string]any{}
	for _, fields := range values {
		for key, value := range fields {
			result[key] = value
		}
	}
	return result
}

func stringValue(value any) string {
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
