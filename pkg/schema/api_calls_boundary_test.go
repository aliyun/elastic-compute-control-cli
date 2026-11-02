package schema

import (
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestAPICallDescriptionsDoNotCacheAcrossUnavailableWorkflowSteps(t *testing.T) {
	resource := spec.ResourceSpec{Probes: map[string]spec.Probe{"read": {API: "ReadThing"}, "empty": {}}, Waiters: map[string]spec.Waiter{"wait": {Probe: "missing"}}}
	for _, step := range []spec.WorkflowStep{{Binding: "missing"}, {Wait: "missing"}, {Wait: "wait"}, {Probe: "missing"}, {Probe: "empty"}} {
		calls := operationAPICalls(resource, spec.Operation{Workflow: []spec.WorkflowStep{{Probe: "read"}, step, {Probe: "read"}}}, "en")
		if len(calls) != 2 || calls[1].Cached {
			t.Fatalf("unavailable workflow step retained API cache: %+v", calls)
		}
	}
	if calls := operationAPICalls(resource, spec.Operation{Call: spec.OperationCall{Probe: "read"}}, "en"); len(calls) != 1 || calls[0].API != "ReadThing" || calls[0].Phase != "readback" {
		t.Fatalf("single-probe readback trace=%+v", calls)
	}
	if calls := operationAPICalls(resource, spec.Operation{Call: spec.OperationCall{Probe: "missing"}}, "en"); len(calls) != 0 {
		t.Fatalf("missing probe invented API calls: %+v", calls)
	}
}

func TestAPIConditionsPreserveQuotedDisjunctionsAndDryRunAlternatives(t *testing.T) {
	for _, condition := range []string{`input.name == "escaped\" || still quoted"`, `input.name == 'escaped\' || still quoted'`} {
		if apiConditionHasTopLevelDisjunction(condition) {
			t.Fatalf("quoted disjunction grouped as executable expression: %s", condition)
		}
	}
	for _, expression := range []string{"context.id", "input.", "input.nested.id", "input.absent"} {
		if guaranteedAPISelectorInput(spec.Operation{}, expression) {
			t.Fatalf("optional selector assumed exhaustive: %s", expression)
		}
	}
	if !bindingRequestUsesDryRun([]any{map[string]any{"DryRun": "$.dry_run"}}) {
		t.Fatal("nested dry-run request not described")
	}
	if got := appendUniqueAPICondition([]string{"input.first"}, "input.first"); !reflect.DeepEqual(got, []string{"input.first"}) {
		t.Fatalf("duplicate condition=%v", got)
	}
	if got := dryRunStateAPICondition([]string{"input.first", "input.second"}, "input.other"); got != "!(input.dry_run && (input.first || input.second))" {
		t.Fatalf("alternative dry-run condition=%s", got)
	}
}
