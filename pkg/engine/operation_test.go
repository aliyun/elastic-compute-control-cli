package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestRequireAnyBindingInputMatchesRequestPrefixAndExpressions(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"raw":   []string{"DryRun=true"},
		"items": []string{"sg-a"},
	}}

	cases := []struct {
		name       string
		requireAny []spec.Requirement
		request    map[string]any
	}{
		{
			name:       "request prefix",
			requireAny: []spec.Requirement{{Request: "Tag"}},
			request:    map[string]any{"Tag.1.Key": "env"},
		},
		{
			name:       "raw expression",
			requireAny: []spec.Requirement{{Raw: "$.raw"}},
			request:    map[string]any{},
		},
		{
			name:       "each expression",
			requireAny: []spec.Requirement{{Each: "$.items"}},
			request:    map[string]any{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireAnyBindingInput(spec.Binding{RequireAny: tc.requireAny}, tc.request, ctx)
			if err != nil {
				t.Fatalf("requireAnyBindingInput: %v", err)
			}
		})
	}
}

func TestConditionSpecifiedMatchesExplicitEmptyInput(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"ram_role": ""}}
	matched, err := conditionMatches("specified(input.ram_role) && !has(input.ram_role)", ctx)
	if err != nil {
		t.Fatalf("conditionMatches: %v", err)
	}
	if !matched {
		t.Fatal("specified empty input should match detach condition")
	}

	matched, err = conditionMatches("specified(input.key_pair)", ctx)
	if err != nil {
		t.Fatalf("conditionMatches omitted key: %v", err)
	}
	if matched {
		t.Fatal("omitted input should not match specified()")
	}
}

func TestConditionMatchesStartsWith(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"target":  "lt-web",
		"targets": []string{"web-lt", "lt-id"},
	}}

	matched, err := conditionMatches(`starts_with(input.target,"lt-")`, ctx)
	if err != nil {
		t.Fatalf("conditionMatches starts_with: %v", err)
	}
	if !matched {
		t.Fatal("starts_with should match scalar target")
	}

	matched, err = conditionMatches(`starts_with(input.targets,"lt-")`, ctx)
	if err != nil {
		t.Fatalf("conditionMatches starts_with list: %v", err)
	}
	if !matched {
		t.Fatal("starts_with should match one list item")
	}

	matched, err = conditionMatches(`starts_with(input.target,"pl-")`, ctx)
	if err != nil {
		t.Fatalf("conditionMatches starts_with miss: %v", err)
	}
	if matched {
		t.Fatal("starts_with should not match different prefix")
	}
}

func TestExecuteOperationCallMapsSingleProbeResult(t *testing.T) {
	resource := fakeVPCSpecForEngine(t)
	resource.Operations["call_get"] = spec.Operation{
		Call: spec.OperationCall{
			Probe:    "list",
			IDs:      []string{"$.id"},
			NotFound: "error",
		},
		Emit: "vpc_from_probe",
	}
	caller := &fakeCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 1,
		"Vpcs": map[string]any{"Vpc": []any{map[string]any{
			"VpcId":     "vpc-1",
			"VpcName":   "prod",
			"CidrBlock": "10.0.0.0/16",
			"Status":    "Available",
			"RegionId":  "cn-beijing",
		}}},
	}}}

	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{
		Action:  "call_get",
		Input:   map[string]any{"id": "vpc-1", "limit": 1, "page": 1},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ID != "vpc-1" || result.Item["name"] != "prod" || result.Total != 1 || !result.HasTotal || result.RequestID != "req-list" {
		t.Fatalf("result = %#v", result)
	}
	if len(caller.calls) != 1 || !reflect.DeepEqual(caller.calls[0].request["VpcId"], []string{"vpc-1"}) {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestExecuteOperationCallReturnsNotFoundForEmptyRequiredProbe(t *testing.T) {
	resource := fakeVPCSpecForEngine(t)
	resource.Operations["call_get"] = spec.Operation{
		Call: spec.OperationCall{
			Probe:    "list",
			IDs:      []string{"$.id"},
			NotFound: "error",
		},
	}
	caller := &fakeCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 0,
		"Vpcs":       map[string]any{"Vpc": []any{}},
	}}}

	_, err := NewExecutor(resource, caller).Execute(context.Background(), Request{
		Action:  "call_get",
		Input:   map[string]any{"id": "vpc-missing", "limit": 1, "page": 1},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err == nil || !strings.Contains(err.Error(), "NotFound") {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestExecuteWorkflowCarriesProbeExtraFields(t *testing.T) {
	loaded, err := spec.Load([]byte(`
schema_version: 2
product: rg
resource: group
kind: regional
identity:
  field: id
  output_root:
    one: group
    many: groups
schema:
  fields:
    id:
      type: string
probes:
  list:
    api: ListResourceGroupsWithAuthDetails
    response:
      items: $.ResourceGroups
      total: $.TotalCount
      request_id: $.RequestId
      id: $.Id
      fields:
        id: $.Id
      extra_fields:
        auth_details: $.AuthDetails
operations:
  list:
    workflow:
      - probe: list
        many: true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{{
		"RequestId":  "req-list",
		"TotalCount": 1,
		"ResourceGroups": []any{
			map[string]any{"Id": "rg-123"},
		},
		"AuthDetails": []any{
			map[string]any{"AccountScopeAuth": true},
		},
	}}}

	result, err := NewExecutor(loaded, caller).Execute(context.Background(), Request{Action: "list"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0]["id"] != "rg-123" {
		t.Fatalf("items = %#v", result.Items)
	}
	authDetails, _ := result.Extra["auth_details"].([]any)
	if len(authDetails) != 1 {
		t.Fatalf("auth_details = %#v; result=%#v", result.Extra["auth_details"], result)
	}
	first, _ := authDetails[0].(map[string]any)
	if first["account_scope_auth"] != true {
		t.Fatalf("auth detail = %#v", first)
	}
}

func TestRequireAnyBindingInputRejectsMissingValues(t *testing.T) {
	err := requireAnyBindingInput(spec.Binding{
		RequireAny: []spec.Requirement{
			{Request: "Tag"},
			{Raw: "$.raw"},
			{Each: "$.items"},
		},
	}, map[string]any{"Tag.1.Key": ""}, ExecutionContext{Input: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "MissingParameter") {
		t.Fatalf("err = %v, want MissingParameter", err)
	}
}

func TestShouldRunAndShouldSkipEvaluateConditions(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"status":  "running",
		"enabled": true,
		"tags":    []string{"env"},
	}}

	run, err := ShouldRun(`input.status == "running"`, []string{"input.missing", "input.tags"}, ctx)
	if err != nil || !run {
		t.Fatalf("ShouldRun = %v, %v; want true nil", run, err)
	}
	run, err = ShouldRun(`input.status != "running"`, nil, ctx)
	if err != nil || run {
		t.Fatalf("ShouldRun mismatch = %v, %v; want false nil", run, err)
	}
	run, err = ShouldRun("", []string{"input.missing"}, ctx)
	if err != nil || run {
		t.Fatalf("ShouldRun when_any missing = %v, %v; want false nil", run, err)
	}

	skip, err := ShouldSkip("input.enabled", ctx)
	if err != nil || !skip {
		t.Fatalf("ShouldSkip = %v, %v; want true nil", skip, err)
	}
	skip, err = ShouldSkip("", ctx)
	if err != nil || skip {
		t.Fatalf("ShouldSkip empty = %v, %v; want false nil", skip, err)
	}
}

func TestRequireAllBindingInputMatchesRequirementKinds(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"raw":   "value",
		"items": []string{"one"},
	}}
	binding := spec.Binding{RequireAll: []spec.Requirement{
		{Request: "Tag"},
		{Raw: "$.raw"},
		{Each: "$.items"},
	}}
	if err := requireAllBindingInput(binding, map[string]any{"Tag.1.Key": "env"}, ctx); err != nil {
		t.Fatalf("requireAllBindingInput valid input: %v", err)
	}
	if err := requireAllBindingInput(binding, map[string]any{"Tag.1.Key": "env"}, ExecutionContext{Input: map[string]any{}}); err == nil {
		t.Fatal("requireAllBindingInput missing expressions returned nil error")
	}
	if requirementMatches(spec.Requirement{}, nil, ctx) {
		t.Fatal("empty requirement should not match")
	}
}

func TestBindingEachItemsResolvesListsAndEmptyValues(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"items": []string{"one", "two"},
		"empty": []string{},
	}}
	got, err := bindingEachItems("$.items", ctx)
	if err != nil {
		t.Fatalf("bindingEachItems: %v", err)
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("bindingEachItems = %#v", got)
	}
	got, err = bindingEachItems("$.empty", ctx)
	if err != nil || got != nil {
		t.Fatalf("bindingEachItems empty = %#v, %v; want nil nil", got, err)
	}
}

func TestMergeProbeResultMergesNonEmptyFields(t *testing.T) {
	result := Result{
		Item:      map[string]any{"name": "old"},
		Extra:     map[string]any{"kept": "value"},
		RequestID: "",
	}
	mergeProbeResult(&result, ProbeResult{
		Items: []map[string]any{{"id": "i-1", "name": "new", "empty": ""}},
		Extra: map[string]any{"added": "extra", "blank": ""},
		Total: 1, NextToken: "token-2", RequestID: "req-1",
	})

	if result.ID != "i-1" || result.Item["name"] != "new" || result.Item["empty"] != nil {
		t.Fatalf("merged item = %#v id=%q", result.Item, result.ID)
	}
	if result.Extra["kept"] != "value" || result.Extra["added"] != "extra" || result.Extra["blank"] != nil {
		t.Fatalf("merged extra = %#v", result.Extra)
	}
	if result.NextToken != "token-2" || result.RequestID != "req-1" {
		t.Fatalf("merged metadata next=%q request=%q", result.NextToken, result.RequestID)
	}
}

func TestMergeProbeResultInitializesMapsAndIgnoresEmptyInputs(t *testing.T) {
	var result Result
	mergeResultExtra(&result, map[string]any{})
	if result.Extra != nil {
		t.Fatalf("empty extra initialized map: %#v", result.Extra)
	}

	mergeProbeResult(&result, ProbeResult{Extra: map[string]any{"added": "extra"}})
	if result.Extra["added"] != "extra" {
		t.Fatalf("extra = %#v", result.Extra)
	}
	mergeProbeResult(&result, ProbeResult{})
	if result.Item != nil {
		t.Fatalf("empty probe initialized item: %#v", result.Item)
	}
}

func TestAppendWorkflowProbeResultAppendsItemsAndTotals(t *testing.T) {
	result := Result{
		Items:     []map[string]any{{"id": "lt-1"}},
		Extra:     map[string]any{"kept": "value"},
		Total:     1,
		HasTotal:  true,
		RequestID: "req-1",
	}

	appendWorkflowProbeResult(&result, ProbeResult{
		Items:     []map[string]any{{"id": "lt-2"}},
		Extra:     map[string]any{"added": "extra"},
		Total:     1,
		HasTotal:  true,
		NextToken: "token-2",
		RequestID: "req-2",
	})

	if len(result.Items) != 2 || result.Items[0]["id"] != "lt-1" || result.Items[1]["id"] != "lt-2" {
		t.Fatalf("items = %#v", result.Items)
	}
	if result.Total != 2 || !result.HasTotal || result.NextToken != "token-2" || result.RequestID != "req-1" {
		t.Fatalf("metadata total=%d hasTotal=%t next=%q request=%q", result.Total, result.HasTotal, result.NextToken, result.RequestID)
	}
	if result.Extra["kept"] != "value" || result.Extra["added"] != "extra" {
		t.Fatalf("extra = %#v", result.Extra)
	}
}

func TestProbeConversionHelpersCoverSupportedTypes(t *testing.T) {
	mapping, err := stringRequestMapping(map[string]any{"Name": "$.name"})
	if err != nil || mapping["Name"] != "$.name" {
		t.Fatalf("stringRequestMapping = %#v err=%v", mapping, err)
	}
	if _, err := stringRequestMapping(map[string]any{"Name": 1}); err == nil {
		t.Fatal("stringRequestMapping accepted non-string value")
	}

	if got := anySlice([]map[string]any{{"id": "i-1"}}); len(got) != 1 {
		t.Fatalf("anySlice map slice = %#v", got)
	}
	if got := anySlice("bad"); got != nil {
		t.Fatalf("anySlice unsupported = %#v", got)
	}

	for _, value := range []any{int8(1), int16(2), int32(3), int64(4), uint(5), uint8(6), uint16(7), uint32(8), uint64(9), float32(10), float64(11), "12"} {
		if got := intValue(value); got == 0 {
			t.Fatalf("intValue(%#v) = 0", value)
		}
	}
	if got := intValue(false); got != 0 {
		t.Fatalf("intValue unsupported = %d", got)
	}
	if !isASCIIDigit('5') || isASCIIDigit('x') {
		t.Fatal("isASCIIDigit returned unexpected result")
	}
}

func TestExceptStringsExcludesRightSide(t *testing.T) {
	got := exceptStrings([]string{"id", "name", "status"}, []string{"name"})
	if !reflect.DeepEqual(got, []string{"id", "status"}) {
		t.Fatalf("exceptStrings = %#v", got)
	}
}

func TestExecuteWorkflowRoutesStepsByConditions(t *testing.T) {
	loaded, err := spec.Load([]byte(`
schema_version: 2
product: demo
resource: widget
kind: regional
identity:
  field: id
  output_root:
    one: widget
    many: widgets
schema:
  fields:
    id:
      type: string
    name:
      type: string
    direction:
      type: string
bindings:
  update_ingress:
    api: UpdateIngress
    request:
      WidgetId: $.id
      Name: $.name
    request_id_from: $.RequestId
  update_egress:
    api: UpdateEgress
    request:
      WidgetId: $.id
      Name: $.name
    request_id_from: $.RequestId
probes:
  attribute:
    api: DescribeWidget
    request:
      WidgetId: $.id
    response:
      item: $
      request_id: $.RequestId
      id: $.WidgetId
      fields:
        id: $.WidgetId
        name: $.Name
operations:
  update:
    examples:
      - ecctl widget update <id>
    input:
      fields: [id, name, direction]
    workflow:
      - binding: update_ingress
        unless: input.direction == egress
        when_any: [input.name]
      - binding: update_egress
        when: input.direction == egress
        when_any: [input.name]
      - probe: attribute
        ids:
          - $input.id
        as: final
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{
		{"RequestId": "req-egress"},
		{"RequestId": "req-get", "WidgetId": "w-1", "Name": "web"},
	}}

	result, err := NewExecutor(loaded, caller).Execute(context.Background(), Request{
		Action: "update",
		Input: map[string]any{
			"id":        "w-1",
			"name":      "web",
			"direction": "egress",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "UpdateEgress" || caller.calls[1].operation != "DescribeWidget" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.Item["id"] != "w-1" || result.Item["name"] != "web" {
		t.Fatalf("result = %#v", result)
	}
	if final, ok := result.Captures["final"]; !ok || len(final.Items) != 1 || final.Items[0]["id"] != "w-1" {
		t.Fatalf("named final result = %#v", result.Captures)
	}
}

func TestApplyNamedEmitFillsVpcFromContextWithoutOverwriting(t *testing.T) {
	result := Result{Item: map[string]any{"name": "existing"}}
	ctx := ExecutionContext{
		Input:   map[string]any{"id": "input-id", "name": "prod", "cidr": "10.0.0.0/16"},
		Context: map[string]any{"id": "ctx-id", "region": "cn-beijing"},
	}

	err := applyNamedEmit(&result, "vpc_from_context", ctx)
	if err != nil {
		t.Fatalf("applyNamedEmit: %v", err)
	}
	want := map[string]any{
		"id":     "ctx-id",
		"name":   "existing",
		"cidr":   "10.0.0.0/16",
		"region": "cn-beijing",
	}
	if !reflect.DeepEqual(result.Item, want) || result.ID != "ctx-id" {
		t.Fatalf("result = %#v, want item %#v and id ctx-id", result, want)
	}
}

func TestApplyEmitMapSetsDeletedAndResolvedFields(t *testing.T) {
	result := Result{}
	ctx := ExecutionContext{Input: map[string]any{"id": "vpc-1", "name": "prod"}}

	err := applyEmit(&result, map[string]any{
		"deleted": true,
		"fields": map[string]any{
			"id":     "$.id",
			"name":   "$.name",
			"static": "literal",
			"empty":  "$.missing",
		},
	}, ctx)
	if err != nil {
		t.Fatalf("applyEmit: %v", err)
	}
	if !result.Deleted || result.ID != "vpc-1" || result.Item["static"] != "literal" {
		t.Fatalf("result = %#v", result)
	}
	if _, ok := result.Item["empty"]; ok {
		t.Fatalf("empty field should be skipped: %#v", result.Item)
	}
}

func TestApplyEmitRejectsUnsupportedName(t *testing.T) {
	err := applyNamedEmit(&Result{}, "unknown", ExecutionContext{})
	if err == nil || !strings.Contains(err.Error(), "UnsupportedEmit") {
		t.Fatalf("err = %v, want UnsupportedEmit", err)
	}
}

func TestOperationHookCallerUsesRawCallerWhenAvailable(t *testing.T) {
	caller := &fakeRawCaller{response: map[string]any{"ok": true}}

	got, err := (operationHookCaller{caller: caller}).CallRaw(context.Background(), "Describe", map[string]any{"id": "vpc-1"})
	if err != nil {
		t.Fatalf("CallRaw: %v", err)
	}
	if got["ok"] != true || len(caller.rawCalls) != 1 || len(caller.calls) != 0 {
		t.Fatalf("caller = %#v got = %#v", caller, got)
	}
}

func TestOperationHookCallerFallsBackToCall(t *testing.T) {
	caller := &fakeCaller{responses: []map[string]any{{"ok": true}}}

	got, err := (operationHookCaller{caller: caller}).CallRaw(context.Background(), "Describe", map[string]any{"id": "vpc-1"})
	if err != nil {
		t.Fatalf("CallRaw: %v", err)
	}
	if got["ok"] != true || len(caller.calls) != 1 {
		t.Fatalf("caller = %#v got = %#v", caller, got)
	}
}

func TestAppendActionCopiesAndCoalescesLastAction(t *testing.T) {
	actions := []ecerrors.Action{{ActionName: "Create", RequestID: "req-1"}}

	got := appendAction(actions, ecerrors.Action{ActionName: "Create", RequestID: "req-2"})
	if actions[0].RequestID != "req-1" {
		t.Fatalf("appendAction mutated original actions: %#v", actions)
	}
	if len(got) != 1 || got[0].RequestID != "req-2" {
		t.Fatalf("appendAction = %#v", got)
	}
	got = appendAction(got, ecerrors.Action{ActionName: "Describe", RequestID: "req-3"})
	if len(got) != 2 || got[1].ActionName != "Describe" {
		t.Fatalf("appendAction second append = %#v", got)
	}
}

func TestApplyAfterErrorBindingHooksRejectsUnknownHook(t *testing.T) {
	executor := NewExecutor(spec.ResourceSpec{Product: "ecs", Resource: "instance"}, &fakeCaller{})
	raw := errors.New("raw")

	err := executor.applyAfterErrorBindingHooks(context.Background(), spec.Binding{
		Hooks: spec.BindingHooks{AfterError: []string{"missing_hook"}},
	}, map[string]any{}, raw)
	if err == nil || !strings.Contains(err.Error(), "UnknownHook") {
		t.Fatalf("err = %v, want UnknownHook", err)
	}
}

type fakeRawCaller struct {
	calls    []callRecord
	rawCalls []callRecord
	response map[string]any
}

func (f *fakeRawCaller) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, callRecord{operation: operation, request: request})
	return f.response, nil
}

func (f *fakeRawCaller) CallRaw(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.rawCalls = append(f.rawCalls, callRecord{operation: operation, request: request})
	return f.response, nil
}
