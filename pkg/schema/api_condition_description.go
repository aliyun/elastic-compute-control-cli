package schema

import (
	"strings"

	"ecctl/pkg/i18n"
	"ecctl/pkg/spec"
)

type apiConditionDescriber struct {
	resource  spec.ResourceSpec
	operation spec.Operation
	localizer *i18n.Localizer
}

func describeAPICondition(resource spec.ResourceSpec, operation spec.Operation, condition, lang string) (string, bool) {
	describer := apiConditionDescriber{
		resource:  resource,
		operation: operation,
		localizer: i18n.NewLocalizer(lang),
	}
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return describer.localizer.Message("SchemaAPICallCondition.Always"), true
	}
	if condition == `trim(input.image) != "" && !ends_with(lower(trim(input.image)), ".vhd")` {
		return describer.localizer.MessageData("SchemaAPICallCondition.ImageLookup", map[string]any{"Flag": "--image"}), true
	}
	if name, empty, ok := explicitValueCondition(condition); ok {
		id := "SchemaAPICallCondition.ExplicitNonEmpty"
		if empty {
			id = "SchemaAPICallCondition.ExplicitEmpty"
		}
		return describer.localizer.MessageData(id, map[string]any{"Flag": describer.inputDisplay(name)}), true
	}
	fragment, ok := describer.describe(condition, false)
	if !ok {
		return "", false
	}
	messageID := "SchemaAPICallCondition.When"
	if strings.HasSuffix(fragment, "`") {
		messageID = "SchemaAPICallCondition.WhenAfterCode"
	}
	return describer.localizer.MessageData(messageID, map[string]any{"Condition": fragment}), true
}

func explicitValueCondition(condition string) (name string, empty bool, ok bool) {
	parts := splitTopLevelCondition(condition, "&&")
	if len(parts) != 2 {
		return "", false, false
	}
	first, ok := conditionCallArgument(parts[0], "specified")
	if !ok {
		return "", false, false
	}
	second := strings.TrimSpace(parts[1])
	negated := strings.HasPrefix(second, "!")
	if negated {
		second = strings.TrimSpace(strings.TrimPrefix(second, "!"))
	}
	value, ok := conditionCallArgument(second, "has")
	if !ok || value != first {
		return "", false, false
	}
	name, ok = inputPathName(first)
	return name, negated, ok
}

func (d apiConditionDescriber) describe(expression string, negated bool) (string, bool) {
	expression = trimOuterConditionParentheses(strings.TrimSpace(expression))
	if expression == "" {
		return "", false
	}
	for _, operator := range []string{"||", "&&"} {
		parts := splitTopLevelCondition(expression, operator)
		if len(parts) < 2 {
			continue
		}
		if negated {
			if operator == "||" {
				operator = "&&"
			} else {
				operator = "||"
			}
		}
		fragments := make([]string, 0, len(parts))
		for _, part := range parts {
			fragment, ok := d.describe(part, negated)
			if !ok {
				return "", false
			}
			if operator == "&&" && !negated && len(splitTopLevelCondition(trimOuterConditionParentheses(strings.TrimSpace(part)), "||")) > 1 {
				fragment = d.localizer.MessageData("SchemaAPICallCondition.Group", map[string]any{"Condition": fragment})
			}
			fragments = append(fragments, fragment)
		}
		return d.join(fragments, operator), true
	}
	if strings.HasPrefix(expression, "!") {
		return d.describe(strings.TrimSpace(strings.TrimPrefix(expression, "!")), !negated)
	}
	if left, right, operator, ok := cutTopLevelComparison(expression); ok {
		if negated {
			if operator == "==" {
				operator = "!="
			} else {
				operator = "=="
			}
		}
		return d.describeComparison(left, right, operator)
	}
	if name, argument, ok := parseConditionCall(expression); ok {
		return d.describeCall(name, argument, negated)
	}
	if name, ok := inputPathName(expression); ok {
		return d.specified(name, negated), true
	}
	if name, ok := contextPathName(expression); ok {
		return d.contextAvailable(name, negated), true
	}
	return "", false
}

func (d apiConditionDescriber) describeCall(name, argument string, negated bool) (string, bool) {
	switch name {
	case "has":
		return d.describePresence(argument, negated)
	case "specified":
		input, ok := inputPathName(argument)
		if !ok {
			return "", false
		}
		id := "SchemaAPICallCondition.ExplicitlySpecified"
		if negated {
			id = "SchemaAPICallCondition.NotExplicitlySpecified"
		}
		return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(input)}), true
	case "single", "multiple":
		input, ok := inputPathName(argument)
		if !ok || negated {
			return "", false
		}
		id := "SchemaAPICallCondition.Single"
		if name == "multiple" {
			id = "SchemaAPICallCondition.Multiple"
		}
		return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(input)}), true
	case "starts_with":
		args := splitTopLevelCondition(argument, ",")
		if len(args) != 2 {
			return "", false
		}
		input, ok := inputPathName(args[0])
		if !ok {
			return "", false
		}
		id := "SchemaAPICallCondition.StartsWith"
		if negated {
			id = "SchemaAPICallCondition.NotStartsWith"
		}
		return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(input), "Prefix": conditionValue(args[1])}), true
	}
	return "", false
}

func (d apiConditionDescriber) describePresence(argument string, negated bool) (string, bool) {
	if input, ok := inputPathName(argument); ok {
		return d.specified(input, negated), true
	}
	if context, ok := contextPathName(argument); ok {
		return d.contextAvailable(context, negated), true
	}
	name, args, ok := parseConditionCall(strings.TrimSpace(argument))
	if !ok {
		return "", false
	}
	switch name {
	case "$prefixed_kv_objects", "$prefixed_values":
		parts := splitTopLevelCondition(args, ",")
		if len(parts) < 2 {
			return "", false
		}
		input, ok := inputPathName(parts[0])
		if !ok {
			return "", false
		}
		id := "SchemaAPICallCondition.ContainsPrefix"
		if negated {
			id = "SchemaAPICallCondition.NotContainsPrefix"
		}
		return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(input), "Prefix": conditionValue(parts[1])}), true
	case "$matching_prefix", "$not_matching_prefix":
		parts := splitTopLevelCondition(args, ",")
		if len(parts) != 2 {
			return "", false
		}
		input, ok := inputPathName(parts[0])
		if !ok {
			return "", false
		}
		matching := name == "$matching_prefix"
		if negated {
			matching = !matching
		}
		id := "SchemaAPICallCondition.ContainsMatchingPrefix"
		if !matching {
			id = "SchemaAPICallCondition.ContainsNonMatchingPrefix"
		}
		return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(input), "Prefix": conditionValue(parts[1])}), true
	case "$except":
		if negated {
			return "", false
		}
		parts := splitTopLevelCondition(args, ",")
		if len(parts) != 2 {
			return "", false
		}
		innerName, innerArgs, ok := parseConditionCall(parts[0])
		if !ok || innerName != "$matching_prefix" {
			return "", false
		}
		matching := splitTopLevelCondition(innerArgs, ",")
		if len(matching) != 2 {
			return "", false
		}
		input, ok := inputPathName(matching[0])
		if !ok {
			return "", false
		}
		return d.localizer.MessageData("SchemaAPICallCondition.ContainsUnmatchedPrefix", map[string]any{"Flag": d.inputDisplay(input), "Prefix": conditionValue(matching[1])}), true
	}
	return "", false
}

func (d apiConditionDescriber) describeComparison(left, right, operator string) (string, bool) {
	value := conditionValue(right)
	name, ok := inputPathName(left)
	if !ok {
		if function, argument, parsed := parseConditionCall(left); parsed && function == "length" {
			name, ok = inputPathName(argument)
			if ok && value == "0" {
				return d.specified(name, operator == "=="), true
			}
		}
	}
	if !ok {
		return "", false
	}
	if value == "" {
		return d.specified(name, operator == "=="), true
	}
	id := "SchemaAPICallCondition.Equals"
	if operator == "!=" {
		id = "SchemaAPICallCondition.NotEquals"
	}
	return d.localizer.MessageData(id, map[string]any{
		"Flag":  d.inputDisplay(name),
		"Field": inputNestedField(left),
		"Value": value,
	}), true
}

func (d apiConditionDescriber) specified(name string, negated bool) string {
	id := "SchemaAPICallCondition.Specified"
	if negated {
		id = "SchemaAPICallCondition.NotSpecified"
	}
	return d.localizer.MessageData(id, map[string]any{"Flag": d.inputDisplay(name)})
}

func (d apiConditionDescriber) contextAvailable(name string, negated bool) string {
	id := "SchemaAPICallCondition.ContextAvailable"
	if negated {
		id = "SchemaAPICallCondition.ContextUnavailable"
	}
	return d.localizer.MessageData(id, map[string]any{"Name": name})
}

func (d apiConditionDescriber) inputDisplay(name string) string {
	for _, field := range append(append(spec.OperationFields{}, d.operation.Input.Fields...), d.operation.Input.Controls...) {
		if field.Name != name {
			continue
		}
		value := "--" + schemaFlagName(d.resource, field)
		if field.Positional || field.PositionalMany {
			value = "<" + schemaFlagName(d.resource, field) + ">"
		}
		return value
	}
	return "--" + flagName(name)
}

func (d apiConditionDescriber) join(parts []string, operator string) string {
	id := "SchemaAPICallCondition.And"
	if operator == "||" {
		id = "SchemaAPICallCondition.Or"
	}
	result := parts[0]
	for _, part := range parts[1:] {
		messageID := id
		if strings.HasSuffix(result, ")") || strings.HasSuffix(result, "）") {
			messageID += "AfterGroup"
		} else if strings.HasSuffix(result, "`") {
			messageID += "AfterCode"
		}
		if strings.HasPrefix(part, "`") {
			messageID += "BeforeCode"
		}
		result = d.localizer.MessageData(messageID, map[string]any{"Left": result, "Right": part})
	}
	return result
}

func conditionCallArgument(expression, want string) (string, bool) {
	name, argument, ok := parseConditionCall(strings.TrimSpace(expression))
	return argument, ok && name == want
}

func parseConditionCall(expression string) (string, string, bool) {
	expression = strings.TrimSpace(expression)
	open := strings.IndexByte(expression, '(')
	if open <= 0 || !strings.HasSuffix(expression, ")") || !conditionOuterParenthesesCover(expression[open:]) {
		return "", "", false
	}
	return strings.TrimSpace(expression[:open]), strings.TrimSpace(expression[open+1 : len(expression)-1]), true
}

func splitTopLevelCondition(expression, separator string) []string {
	parts := make([]string, 0, 2)
	start, depth := 0, 0
	var quote byte
	escaped := false
	for index := 0; index <= len(expression)-len(separator); index++ {
		character := expression[index]
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
		}
		if depth == 0 && strings.HasPrefix(expression[index:], separator) {
			parts = append(parts, strings.TrimSpace(expression[start:index]))
			index += len(separator) - 1
			start = index + 1
		}
	}
	if len(parts) == 0 {
		return []string{strings.TrimSpace(expression)}
	}
	parts = append(parts, strings.TrimSpace(expression[start:]))
	return parts
}

func cutTopLevelComparison(expression string) (string, string, string, bool) {
	for _, operator := range []string{"==", "!="} {
		parts := splitTopLevelCondition(expression, operator)
		if len(parts) == 2 {
			return parts[0], parts[1], operator, true
		}
	}
	return "", "", "", false
}

func trimOuterConditionParentheses(expression string) string {
	for strings.HasPrefix(expression, "(") && strings.HasSuffix(expression, ")") && conditionOuterParenthesesCover(expression) {
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
	return expression
}

func conditionOuterParenthesesCover(expression string) bool {
	depth := 0
	var quote byte
	for index := 0; index < len(expression); index++ {
		character := expression[index]
		if quote != 0 {
			if character == quote && (index == 0 || expression[index-1] != '\\') {
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
			depth--
			if depth == 0 && index != len(expression)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func inputPathName(expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	switch {
	case strings.HasPrefix(expression, "$input."):
		expression = strings.TrimPrefix(expression, "$input.")
	case strings.HasPrefix(expression, "input."):
		expression = strings.TrimPrefix(expression, "input.")
	case strings.HasPrefix(expression, "$."):
		expression = strings.TrimPrefix(expression, "$.")
	default:
		return "", false
	}
	if expression == "" {
		return "", false
	}
	name := strings.SplitN(expression, ".", 2)[0]
	if strings.ContainsAny(name, "() ,\"'") {
		return "", false
	}
	return name, true
}

func inputNestedField(expression string) string {
	expression = strings.TrimSpace(expression)
	expression = strings.TrimPrefix(expression, "$input.")
	expression = strings.TrimPrefix(expression, "input.")
	expression = strings.TrimPrefix(expression, "$.")
	parts := strings.SplitN(expression, ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func contextPathName(expression string) (string, bool) {
	expression = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expression), "$"))
	if !strings.HasPrefix(expression, "context.") {
		return "", false
	}
	name := strings.TrimPrefix(expression, "context.")
	return name, name != "" && !strings.ContainsAny(name, "() ,\"'")
}

func conditionValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}
