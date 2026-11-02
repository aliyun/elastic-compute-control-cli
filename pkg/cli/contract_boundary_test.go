package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestAPIPassthroughNormalizesShortFlagsWithoutLosingValues(t *testing.T) {
	for _, test := range []struct{ args, want []string }{
		{[]string{"call", "Ecs", "DescribeInstances", "-p", "test"}, []string{"call", "Ecs", "DescribeInstances", "--aliyun-arg=-p", "--aliyun-arg=test"}},
		{[]string{"call", "Ecs", "DescribeInstances", "--waiter", "expr=value", "extra=value"}, []string{"call", "Ecs", "DescribeInstances", "--aliyun-arg=--waiter", "--aliyun-arg=expr=value", "--aliyun-arg=extra=value"}},
	} {
		if got := normalizeAPICallParameterFlags(test.args); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("normalization=%q, want %q", got, test.want)
		}
	}
	for _, test := range []struct {
		args []string
		want string
	}{{[]string{"--profile"}, ""}, {[]string{"-p=test"}, "test"}} {
		if got, ok := apiCallPassthroughFlagValue(test.args, "profile"); !ok || got != test.want {
			t.Fatalf("profile passthrough=%s,%t", got, ok)
		}
	}
	for _, flag := range []string{"-p", "-e", "-o"} {
		if !apiCallAliyunCLIValueFlag(flag) {
			t.Fatalf("short value flag lost: %s", flag)
		}
	}
	for _, flag := range []string{"value", "-x"} {
		if apiCallAliyunCLIValueFlag(flag) {
			t.Fatalf("non-value flag accepted: %s", flag)
		}
	}
	if apiCallAliyunCLIFlagAcceptsExtraValues("-p") {
		t.Fatal("profile swallowed extra arguments")
	}
	if got := appendAliyunCLIFlagArg(nil, "value"); !reflect.DeepEqual(got, []string{"--aliyun-arg=value"}) {
		t.Fatalf("argument changed: %q", got)
	}
	if _, ok := apiCallParameterType("unknown", "MissingOperation", "Absent"); ok {
		t.Fatal("unknown operation parameter resolved")
	}
	if _, ok := apiCallParameterType("Ecs", "DescribeInstances", "Absent"); ok {
		t.Fatal("absent parameter resolved")
	}
}

func TestResourceContractsHandleAbsentDefinitionsAndRequirements(t *testing.T) {
	r := spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{"value": {Type: "string"}}}}
	op := spec.Operation{Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "missing"}, {Name: "value"}}}, RequireAny: []spec.Requirement{{Each: "input.value"}, {Each: "input.value"}, {Raw: "unknown"}}}
	if got := operationFieldDisplayName(r, op, "missing"); got != "--missing" {
		t.Fatalf("missing field display=%s", got)
	}
	if got := operationFieldDisplayName(r, op, "other"); got != "--other" {
		t.Fatalf("unknown field display=%s", got)
	}
	if _, ok := updateInputParam(r, op, "value"); !ok {
		t.Fatal("later update parameter ignored")
	}
	if err := validateOperationRequireAny(op, map[string]any{"value": "present"}); err != nil {
		t.Fatalf("each requirement ignored: %v", err)
	}
	if got := operationRequireAnyFlags(r, op); !reflect.DeepEqual(got, []string{"--value"}) {
		t.Fatalf("duplicate requirement flags=%v", got)
	}
	if got := requirementInputName(spec.Requirement{Raw: "unknown"}); got != "" {
		t.Fatalf("unknown requirement name=%s", got)
	}
	if _, ok := operationInputParam(r, spec.OperationFieldRef{Name: "absent"}); ok {
		t.Fatal("absent operation parameter resolved")
	}
	param := spec.Param{}
	applyOperationParamOverrides(&param, spec.OperationFieldRef{HasRepeatable: true, Repeatable: true, Enum: []string{"first", "second"}})
	if !param.Repeatable || len(param.Enum) != 2 {
		t.Fatalf("parameter constraints lost: %+v", param)
	}
	if !publicCLICommandAllowed([]string{"help"}) || !publicCLIExample(" ") {
		t.Fatal("empty help/example rejected")
	}
	if _, _, ok := publicCLIExampleResourceAction(nil); ok {
		t.Fatal("empty resource example resolved")
	}
	if _, _, ok := publicCLIExampleResourceAction([]string{"unknown"}); ok {
		t.Fatal("unknown resource example resolved")
	}
	if !publicCLIExample("ecctl sandbox") {
		t.Fatal("default resource example rejected")
	}
	if got := keyExamples([]string{"", "ecctl ecs instance list", "ecctl ecs instance list"}); len(got) != 1 {
		t.Fatalf("duplicate examples retained: %v", got)
	}
	if got := schemaFieldForInput(r, "missing"); got.Type != "" {
		t.Fatalf("missing schema field=%+v", got)
	}
	fields := outputFieldSet(r, spec.Operation{Workflow: []spec.WorkflowStep{{}, {Probe: "missing"}}, Output: spec.OperationOutput{Fields: map[string]any{"custom": "$result.extra.custom"}}})
	if !fields["custom"] {
		t.Fatal("explicit output field lost")
	}
	if got := paramUsage(spec.ResourceSpec{Controls: map[string]spec.SchemaField{"value": {Description: spec.LocalizedText{"en": "Control text"}}}}, "value", spec.Param{}, "en"); !strings.Contains(got, "Control text") {
		t.Fatalf("control help=%s", got)
	}
}
