package schema

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestConditionDescriptionsRejectUnsupportedExpressions(t *testing.T) {
	for _, expression := range []string{
		"unknown", "has(unknown)", "has(unknown(input.ids))", "specified(context.ids)", "!single(input.ids)", "multiple(context.ids)",
		"starts_with(input.ids)", `starts_with(context.ids,"i-")`, `has($prefixed_values(input.ids))`, `has($prefixed_values(context.ids,"+"))`,
		`has($matching_prefix(input.ids))`, `has($matching_prefix(context.ids,"i-"))`,
		`!has($except($matching_prefix(input.ids,"i-"),input.old))`, `has($except(input.ids))`, `has($except(input.ids,input.old))`,
		`has($except($matching_prefix(input.ids),input.old))`, `has($except($matching_prefix(context.ids,"i-"),input.old))`,
		"input.ids && unknown", "context.id == 'value'",
	} {
		if text, ok := describeAPICondition(spec.ResourceSpec{}, spec.Operation{}, expression, "en"); ok || text != "" {
			t.Fatalf("unsupported %q described as %q", expression, text)
		}
	}
}

func TestConditionDescriptionsPreservePrefixAndNegationMeaning(t *testing.T) {
	for _, expression := range []string{
		`!has($prefixed_values(input.ids,"+"))`, `!has($prefixed_kv_objects(input.ids,"+"))`,
		`!has($matching_prefix(input.ids,"i-"))`, `!has($not_matching_prefix(input.ids,"i-"))`,
		`!(has(input.ids) || has(context.existing))`, `!(has(input.ids) && has(context.existing))`,
		`!specified(input.ids)`, `!starts_with(input.ids,"i-")`, `!(input.ids == "i-one")`, `!(input.ids != "i-one")`,
	} {
		text, ok := describeAPICondition(spec.ResourceSpec{}, spec.Operation{}, expression, "en")
		if !ok || text == "" || !strings.Contains(text, "--ids") || strings.Contains(text, "SchemaAPICallCondition.") {
			t.Fatalf("condition %q = %q, %t", expression, text, ok)
		}
	}
	if _, ok := inputPathName("input.bad(name)"); ok {
		t.Fatal("function syntax accepted as input path")
	}
}

func TestConditionDescriptionsRejectMismatchedInputsAndPreserveEscapedLiterals(t *testing.T) {
	if _, _, ok := explicitValueCondition("specified(input.one) && has(input.two)"); ok {
		t.Fatal("mismatched presence check merged")
	}
	if _, ok := describeAPICondition(spec.ResourceSpec{}, spec.Operation{}, "!()", "en"); ok {
		t.Fatal("empty expression described")
	}
	if name, ok := inputPathName("$input.name"); !ok || name != "name" {
		t.Fatalf("input path=%s,%t", name, ok)
	}
	if _, ok := inputPathName("$input."); ok {
		t.Fatal("empty input name accepted")
	}
	parts := splitTopLevelCondition(`input.name == "a\" && b" && has(input.id)`, "&&")
	if len(parts) != 2 || parts[1] != "has(input.id)" {
		t.Fatalf("escaped condition split incorrectly: %v", parts)
	}
}
