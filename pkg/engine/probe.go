package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	spechooks "github.com/aliyun/elastic-compute-control-cli/specs"
)

type ProbeResult struct {
	Items     []map[string]any
	Extra     map[string]any
	Total     int
	HasTotal  bool
	NextToken string
	RequestID string
	Actions   []ecerrors.Action
}

const topLevelArrayResponseMarker = "__ecctl_top_level_array_response"

func (e *Executor) runProbe(ctx context.Context, name string, execCtx ExecutionContext, ids []string) (ProbeResult, error) {
	probe, ok := e.spec.Probes[name]
	if !ok {
		return ProbeResult{}, ecerrors.Client("UnknownProbe", fmt.Sprintf("probe %q is not configured", name))
	}

	input := cloneMap(execCtx.Input)
	switch len(ids) {
	case 0:
	case 1:
		input["id"] = ids[0]
		input["ids"] = []string{ids[0]}
	default:
		input["ids"] = append([]string(nil), ids...)
	}
	probeCtx := ExecutionContext{
		Input:    input,
		Context:  cloneMap(execCtx.Context),
		Current:  execCtx.Current,
		Captures: execCtx.Captures,
	}
	request, _, err := ResolveResourceBindingRequest(e.spec, spec.Binding{Request: probe.Request}, probeCtx)
	if err != nil {
		return ProbeResult{}, err
	}
	response, err := e.caller.Call(ctx, probe.API, request)
	if err != nil {
		return ProbeResult{}, ecerrors.WithActions(err, []ecerrors.Action{ecerrors.ActionFromError(probe.API, err)})
	}
	result := mapProbeResponse(e.spec, probe, response)
	result.Actions = []ecerrors.Action{{RequestID: result.RequestID, ActionName: probe.API}}
	return result, nil
}

func mapProbeResponse(resource spec.ResourceSpec, probe spec.Probe, response map[string]any) ProbeResult {
	result := ProbeResult{
		Extra:     mapProbeExtraFields(resource, probe, response),
		Total:     intValue(ExtractString(response, probe.Response.Total)),
		HasTotal:  probe.Response.Total != "",
		NextToken: ExtractString(response, probe.Response.NextToken),
		RequestID: ExtractString(response, probe.Response.RequestID),
	}
	if totalValue, ok := ExtractPath(response, probe.Response.Total); ok {
		result.Total = intValue(totalValue)
	}

	if probe.Response.Item != "" {
		source, ok := probeResponseItem(response, probe.Response.Item)
		if !ok {
			return result
		}
		if probe.Response.ID != "" && ExtractString(source, probe.Response.ID) == "" {
			return result
		}
		item := mapProbeItem(resource, probe, source)
		if len(item) > 0 {
			result.Items = append(result.Items, item)
			result.Total = 1
		}
		return result
	}

	rawItems, ok := probeResponseItems(response, probe.Response.Items)
	if !ok {
		return result
	}
	for _, rawItem := range anySlice(rawItems) {
		source, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		item := mapProbeItem(resource, probe, source)
		result.Items = append(result.Items, item)
	}
	if result.Total == 0 {
		result.Total = len(result.Items)
	}
	return result
}

func probeResponseItem(response map[string]any, path string) (map[string]any, bool) {
	rawItem, ok := ExtractPath(response, path)
	if !ok && path == "$.items" {
		rawItem = response
		ok = true
	}
	if !ok {
		return nil, false
	}
	if source, ok := rawItem.(map[string]any); ok {
		return source, true
	}
	for _, raw := range anySlice(rawItem) {
		source, ok := raw.(map[string]any)
		if ok {
			return source, true
		}
	}
	return nil, false
}

func probeResponseItems(response map[string]any, path string) (any, bool) {
	rawItems, ok := ExtractPath(response, path)
	if ok {
		return rawItems, true
	}
	if path != "$.items" && response[topLevelArrayResponseMarker] == true {
		return ExtractPath(response, "$.items")
	}
	return nil, false
}

func mapProbeExtraFields(resource spec.ResourceSpec, probe spec.Probe, response map[string]any) map[string]any {
	extra := map[string]any{}
	for field, mapping := range probe.Response.ExtraFields {
		value, ok := probeFieldValue(mapping, response)
		if !ok {
			continue
		}
		if mapped, ok := spechooks.MapField(resource.Product, resource.Resource, field, value); ok {
			if shouldEmitProbeField(mapped, mapping) {
				extra[field] = mapped
			}
			continue
		}
		value = normalizeProbeOutputValue(value)
		if shouldEmitProbeField(value, mapping) {
			extra[field] = value
		}
	}
	return extra
}

func mapProbeItem(resource spec.ResourceSpec, probe spec.Probe, source map[string]any) map[string]any {
	item := map[string]any{}
	for field, mapping := range probe.Response.Fields {
		value, ok := probeFieldValue(mapping, source)
		if !ok {
			continue
		}
		if mapped, ok := spechooks.MapField(resource.Product, resource.Resource, field, value); ok {
			if shouldEmitProbeField(mapped, mapping) {
				item[field] = mapped
			}
			continue
		}
		value = normalizeProbeOutputValue(value)
		if shouldEmitProbeField(value, mapping) {
			item[field] = value
		}
	}
	if _, ok := item["id"]; !ok {
		if id := ExtractString(source, probe.Response.ID); id != "" {
			item["id"] = id
		}
	}
	if _, ok := item["status"]; !ok {
		if state := ExtractString(source, probe.Response.State); state != "" {
			item["status"] = state
		}
	}
	return item
}

func probeFieldValue(field spec.ProbeField, source map[string]any) (any, bool) {
	switch {
	case field.Path != "":
		value, ok := ExtractPath(source, field.Path)
		if !ok && field.DefaultEmptyArray {
			return []any{}, true
		}
		return value, ok
	case field.From != "":
		raw, ok := ExtractPath(source, field.From)
		if !ok {
			return nil, false
		}
		items := make([]map[string]any, 0)
		for _, rawItem := range anySlice(raw) {
			sourceItem, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			mapped := map[string]any{}
			for name, child := range field.Each {
				value, ok := probeFieldValue(child, sourceItem)
				if ok && !isEmpty(value) {
					value = normalizeProbeOutputValue(value)
					if !isEmptyProbeOutput(value) {
						mapped[name] = value
					}
				}
			}
			if len(mapped) > 0 {
				items = append(items, mapped)
			}
		}
		return items, true
	case field.Lower != "":
		value, ok := ExtractStringValue(source, field.Lower)
		if !ok {
			return nil, false
		}
		return strings.ToLower(value), true
	case len(field.First) > 0:
		for _, path := range field.First {
			value, ok := ExtractPath(source, path)
			if ok && !isEmpty(value) {
				return value, true
			}
		}
		return nil, false
	case field.Int != "":
		value, ok := ExtractPath(source, field.Int)
		if !ok {
			return nil, false
		}
		return intValue(value), true
	case field.Port != "":
		value, ok := ExtractStringValue(source, field.Port)
		if !ok {
			return nil, false
		}
		return normalizeProbePort(value), true
	default:
		return nil, false
	}
}

func shouldEmitProbeField(value any, field spec.ProbeField) bool {
	return !isEmptyProbeOutput(value) || field.DefaultEmptyArray
}

func normalizeProbeOutputValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if unwrapped, ok := unwrapOpenAPIListWrapper(typed); ok {
			return normalizeProbeOutputValue(unwrapped)
		}
		normalized := map[string]any{}
		for key, item := range typed {
			value := normalizeProbeOutputValue(item)
			if !isEmptyProbeOutput(value) {
				normalized[snakeCaseOpenAPIName(key)] = value
			}
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, item := range typed {
			value := normalizeProbeOutputValue(item)
			if !isEmptyProbeOutput(value) {
				normalized = append(normalized, value)
			}
		}
		return normalized
	case []map[string]any:
		normalized := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			value := normalizeProbeOutputValue(item)
			if mapped, ok := value.(map[string]any); ok && !isEmptyProbeOutput(mapped) {
				normalized = append(normalized, mapped)
			}
		}
		return normalized
	default:
		return value
	}
}

func unwrapOpenAPIListWrapper(value map[string]any) (any, bool) {
	if len(value) != 1 {
		return nil, false
	}
	for _, item := range value {
		switch item.(type) {
		case []any, []map[string]any:
			return item, true
		}
	}
	return nil, false
}

func isEmptyProbeOutput(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case []map[string]any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	case map[string]string:
		return len(typed) == 0
	default:
		return false
	}
}

func snakeCaseOpenAPIName(value string) string {
	if value == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(value) + len(value)/4)
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '-' || c == ' ' {
			if out.Len() > 0 && lastByte(&out) != '_' {
				out.WriteByte('_')
			}
			continue
		}
		if isASCIICapital(c) {
			if i > 0 && shouldSplitBeforeCapital(value, i) && lastByte(&out) != '_' {
				out.WriteByte('_')
			}
			c += 'a' - 'A'
		}
		out.WriteByte(c)
	}
	result := out.String()
	replacements := map[string]string{
		"v_switch": "vswitch",
	}
	for old, next := range replacements {
		result = strings.ReplaceAll(result, old, next)
	}
	return result
}

func shouldSplitBeforeCapital(value string, index int) bool {
	previous := value[index-1]
	if previous == '_' || previous == '-' || previous == ' ' {
		return false
	}
	if isASCIILower(previous) || isASCIIDigit(previous) {
		return true
	}
	return isASCIICapital(previous) && index+1 < len(value) && isASCIILower(value[index+1])
}

func isASCIICapital(value byte) bool {
	return value >= 'A' && value <= 'Z'
}

func isASCIILower(value byte) bool {
	return value >= 'a' && value <= 'z'
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func lastByte(builder *strings.Builder) byte {
	if builder.Len() == 0 {
		return 0
	}
	text := builder.String()
	return text[len(text)-1]
}

func ExtractStringValue(value any, path string) (string, bool) {
	raw, ok := ExtractPath(value, path)
	if !ok {
		return "", false
	}
	switch typed := raw.(type) {
	case string:
		return typed, true
	case fmt.Stringer:
		return typed.String(), true
	default:
		return fmt.Sprint(typed), true
	}
}

func stringRequestMapping(mapping map[string]any) (map[string]string, error) {
	result := map[string]string{}
	for key, value := range mapping {
		expr, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("request mapping %q must be a string", key)
		}
		result[key] = expr
	}
	return result, nil
}

func anySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(typed)
		return n
	default:
		return 0
	}
}

func normalizeProbePort(value string) string {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		if parts[0] == parts[1] {
			return parts[0]
		}
		return parts[0] + "-" + parts[1]
	}
	return value
}

func cloneMap(values map[string]any) map[string]any {
	cloned := map[string]any{}
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}
