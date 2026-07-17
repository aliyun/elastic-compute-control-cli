package schema

import (
	"slices"
	"strings"

	"ecctl/pkg/i18n"
	"ecctl/pkg/spec"
)

type apiCallCache struct {
	probe         string
	ids           string
	condition     string
	branch        apiCallCacheBranch
	baseCondition string
}

type apiCallCacheBranch struct {
	kind string
	expr string
}

func operationAPICalls(resource spec.ResourceSpec, operation spec.Operation, lang string) []APICall {
	localizer := i18n.NewLocalizer(lang)
	if len(operation.Workflow) == 0 {
		return describeAPICalls(resource, operation, probeAPICall(resource, operation.Call.Probe, "", localizer), lang)
	}

	calls := make([]APICall, 0)
	var cache *apiCallCache
	dryRunPaths := make([]string, 0)
	contextIDMayBeSet := false
	for _, step := range operation.Workflow {
		condition := workflowStepAPICondition(step)
		switch {
		case step.Binding != "":
			previousCache := cache
			binding, ok := resource.Bindings[step.Binding]
			if !ok {
				cache = nil
				continue
			}
			for _, hookCall := range binding.Hooks.APICalls {
				calls = append(calls, APICall{
					API:       hookCall.API,
					Phase:     hookCall.Phase,
					Condition: combineAPIConditions(condition, hookCall.Condition),
					Purpose:   hookCall.Purpose.Text(lang),
				})
			}
			calls = append(calls, APICall{
				API:       binding.API,
				Phase:     "operation",
				Condition: condition,
				Purpose:   localizer.Message("SchemaAPICallPurpose.Operation"),
			})
			cache = nil
			bindingDryRun := operationHasDryRunControl(operation) && bindingRequestUsesDryRun(binding.Request)
			if binding.Wait != "" {
				baseCondition := combineAPIConditions(negatedAPICondition(step.WaitUnless), bindingDryRunAPICondition(bindingDryRun))
				waitCondition := combineAPIConditions(condition, baseCondition)
				if call, probe, ok := waiterAPICall(resource, binding.Wait, waitCondition, localizer); ok {
					calls = append(calls, call)
					cache = mergeExhaustiveBranchAPICache(previousCache, &apiCallCache{
						probe:         probe,
						ids:           bindingWaitAPIIDs(resource, operation, binding, contextIDMayBeSet),
						condition:     waitCondition,
						branch:        exhaustiveAPICacheBranch(step),
						baseCondition: baseCondition,
					}, operation)
				}
			}
			if binding.IDFrom != "" {
				contextIDMayBeSet = true
			}
			if bindingDryRun {
				dryRunPaths = appendUniqueAPICondition(dryRunPaths, condition)
			}
		case step.Wait != "":
			waitCondition := combineAPIConditions(condition, dryRunStateAPICondition(dryRunPaths, condition))
			if call, probe, ok := waiterAPICall(resource, step.Wait, waitCondition, localizer); ok {
				calls = append(calls, call)
				cache = &apiCallCache{probe: probe, ids: canonicalAPIIDs(step.IDs), condition: waitCondition}
			} else {
				cache = nil
			}
		case step.Probe != "":
			probe, ok := resource.Probes[step.Probe]
			if !ok || probe.API == "" {
				cache = nil
				continue
			}
			probeCondition := combineAPIConditions(condition, dryRunStateAPICondition(dryRunPaths, condition))
			ids := canonicalAPIIDs(step.IDs)
			cached := cache != nil && cache.probe == step.Probe && cache.ids == ids && cache.condition == probeCondition
			purposeID := "SchemaAPICallPurpose.Readback"
			if cached {
				purposeID = "SchemaAPICallPurpose.CachedReadback"
			}
			calls = append(calls, APICall{
				API:       probe.API,
				Phase:     "readback",
				Condition: probeCondition,
				Purpose:   localizer.Message(purposeID),
				Cached:    cached,
			})
			cache = &apiCallCache{probe: step.Probe, ids: ids, condition: probeCondition}
		}
	}
	return describeAPICalls(resource, operation, calls, lang)
}

func describeAPICalls(resource spec.ResourceSpec, operation spec.Operation, calls []APICall, lang string) []APICall {
	for index := range calls {
		if description, ok := describeAPICondition(resource, operation, calls[index].Condition, lang); ok {
			calls[index].ConditionDescription = description
		}
	}
	return calls
}

func probeAPICall(resource spec.ResourceSpec, name, condition string, localizer *i18n.Localizer) []APICall {
	probe, ok := resource.Probes[name]
	if !ok || probe.API == "" {
		return nil
	}
	return []APICall{{
		API:       probe.API,
		Phase:     "readback",
		Condition: condition,
		Purpose:   localizer.Message("SchemaAPICallPurpose.Readback"),
	}}
}

func waiterAPICall(resource spec.ResourceSpec, name, condition string, localizer *i18n.Localizer) (APICall, string, bool) {
	waiter, ok := resource.Waiters[name]
	if !ok {
		return APICall{}, "", false
	}
	probe, ok := resource.Probes[waiter.Probe]
	if !ok || probe.API == "" {
		return APICall{}, "", false
	}
	return APICall{
		API:       probe.API,
		Phase:     "wait",
		Condition: condition,
		Purpose:   localizer.Message("SchemaAPICallPurpose.Wait"),
		Repeated:  true,
	}, waiter.Probe, true
}

func workflowStepAPICondition(step spec.WorkflowStep) string {
	whenAny := ""
	if len(step.WhenAny) > 0 {
		whenAny = "(" + strings.Join(step.WhenAny, " || ") + ")"
	}
	return combineAPIConditions(step.When, whenAny, negatedAPICondition(step.Unless))
}

func combineAPIConditions(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(parts, value) {
			parts = append(parts, value)
		}
	}
	if len(parts) > 1 {
		for index, part := range parts {
			parts[index] = groupDisjunctiveAPICondition(part)
		}
	}
	return strings.Join(parts, " && ")
}

func groupDisjunctiveAPICondition(value string) string {
	if !apiConditionHasTopLevelDisjunction(value) {
		return value
	}
	return "(" + value + ")"
}

func apiConditionHasTopLevelDisjunction(value string) bool {
	depth := 0
	var quote byte
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 && index+1 < len(value) && value[index+1] == '|' {
				return true
			}
		}
	}
	return false
}

func negatedAPICondition(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "!(" + value + ")"
}

func operationHasDryRunControl(operation spec.Operation) bool {
	for _, control := range operation.Input.Controls {
		if control.Name == "dry_run" {
			return true
		}
	}
	return false
}

func canonicalAPIIDs(ids []string) string {
	if len(ids) == 0 {
		return "explicit-empty"
	}
	return "explicit:" + strings.Join(ids, "\x00")
}

func bindingWaitAPIIDs(resource spec.ResourceSpec, operation spec.Operation, binding spec.Binding, contextIDMayBeSet bool) string {
	if binding.IDFrom != "" {
		return canonicalAPIIDs([]string{"$context.id"})
	}
	if contextIDMayBeSet {
		return "binding-current-unknown"
	}
	inputs := make(map[string]spec.OperationFieldRef, len(operation.Input.Fields))
	for _, field := range operation.Input.Fields {
		inputs[field.Name] = field
	}
	if guaranteedAPIIdentityInput(inputs["id"]) {
		return canonicalAPIIDs([]string{"$input.id"})
	}
	if resource.Identity.Field == "name" && guaranteedAPIIdentityInput(inputs["name"]) {
		return canonicalAPIIDs([]string{"$input.name"})
	}
	if guaranteedAPIIdentityInput(inputs["ids"]) {
		return canonicalAPIIDs([]string{"$input.ids"})
	}
	return "binding-current-unknown"
}

func guaranteedAPIIdentityInput(field spec.OperationFieldRef) bool {
	return field.Name != "" && field.Required && !field.AllowEmpty
}

func exhaustiveAPICacheBranch(step spec.WorkflowStep) apiCallCacheBranch {
	if len(step.WhenAny) > 0 || step.Unless != "" {
		return apiCallCacheBranch{}
	}
	condition := strings.TrimSpace(step.When)
	for _, kind := range []string{"single", "multiple"} {
		prefix := kind + "("
		if strings.HasPrefix(condition, prefix) && strings.HasSuffix(condition, ")") {
			expr := strings.TrimSpace(condition[len(prefix) : len(condition)-1])
			if expr != "" && !strings.ContainsAny(expr, "&|,()") {
				return apiCallCacheBranch{kind: kind, expr: expr}
			}
		}
	}
	return apiCallCacheBranch{}
}

func mergeExhaustiveBranchAPICache(previous, current *apiCallCache, operation spec.Operation) *apiCallCache {
	if previous == nil || current == nil ||
		previous.branch.kind == "" || current.branch.kind == "" ||
		previous.branch.kind == current.branch.kind ||
		previous.branch.expr != current.branch.expr ||
		previous.probe != current.probe || previous.ids != current.ids || previous.baseCondition != current.baseCondition ||
		!guaranteedAPISelectorInput(operation, current.branch.expr) {
		return current
	}
	return &apiCallCache{probe: current.probe, ids: current.ids, condition: current.baseCondition}
}

func guaranteedAPISelectorInput(operation spec.Operation, expr string) bool {
	expr = strings.TrimPrefix(strings.TrimSpace(expr), "$")
	if !strings.HasPrefix(expr, "input.") {
		return false
	}
	name := strings.TrimPrefix(expr, "input.")
	if name == "" || strings.Contains(name, ".") {
		return false
	}
	for _, field := range operation.Input.Fields {
		if field.Name == name {
			return guaranteedAPIIdentityInput(field)
		}
	}
	return false
}

func bindingDryRunAPICondition(supported bool) string {
	if !supported {
		return ""
	}
	return "!(input.dry_run)"
}

func bindingRequestUsesDryRun(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "$.dry_run" || typed == "$input.dry_run"
	case map[string]any:
		for _, child := range typed {
			if bindingRequestUsesDryRun(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if bindingRequestUsesDryRun(child) {
				return true
			}
		}
	}
	return false
}

func appendUniqueAPICondition(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func dryRunStateAPICondition(paths []string, currentCondition string) string {
	if len(paths) == 0 {
		return ""
	}
	if slices.Contains(paths, "") {
		return "!(input.dry_run)"
	}
	for _, path := range paths {
		if apiConditionIncludesPath(currentCondition, path) {
			return "!(input.dry_run)"
		}
	}
	return "!(input.dry_run && (" + strings.Join(paths, " || ") + "))"
}

func apiConditionIncludesPath(condition, path string) bool {
	return condition == path ||
		strings.HasPrefix(condition, path+" && ") ||
		strings.HasSuffix(condition, " && "+path) ||
		strings.Contains(condition, " && "+path+" && ")
}
