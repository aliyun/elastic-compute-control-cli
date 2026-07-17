package tag

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

type fakeOperationCaller struct {
	responses []map[string]any
	err       error
	calls     []fakeOperationCall
}

type fakeOperationCall struct {
	operation string
	request   map[string]any
}

func (f *fakeOperationCaller) CallRaw(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, fakeOperationCall{operation: operation, request: request})
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) == 0 {
		return map[string]any{}, nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestPreserveAssociatedResourceRuleUpdateFieldsBackfillsMissingValues(t *testing.T) {
	caller := &fakeOperationCaller{responses: []map[string]any{{
		"Rules": []any{
			map[string]any{"SettingName": "other", "Status": "Disable"},
			map[string]any{
				"SettingName":    "target",
				"Status":         "Enable",
				"ExistingStatus": "Keep",
				"TagKeys":        []string{"env", "team"},
			},
		},
	}}}
	request := map[string]any{"RegionId": "cn-hangzhou", "SettingName": " target "}

	got, err := preserveAssociatedResourceRuleUpdateFields(context.Background(), caller, request)
	if err != nil {
		t.Fatalf("preserveAssociatedResourceRuleUpdateFields: %v", err)
	}
	if got["Status"] != "Enable" || got["ExistingStatus"] != "Keep" || got["TagKeys.1"] != "env" || got["TagKeys.2"] != "team" {
		t.Fatalf("request = %#v", got)
	}
	if _, ok := request["Status"]; ok {
		t.Fatalf("original request mutated: %#v", request)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "ListAssociatedResourceRules" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if caller.calls[0].request["RegionId"] != "cn-hangzhou" || caller.calls[0].request["SettingName.1"] != "target" {
		t.Fatalf("lookup request = %#v", caller.calls[0].request)
	}
}

func TestPreserveAssociatedResourceRuleUpdateFieldsSkipsLookupWhenNothingNeeded(t *testing.T) {
	caller := &fakeOperationCaller{}
	request := map[string]any{
		"SettingName":    "target",
		"Status":         "Enable",
		"ExistingStatus": "Keep",
		"TagKeys.1":      "env",
	}

	got, err := preserveAssociatedResourceRuleUpdateFields(context.Background(), caller, request)
	if err != nil {
		t.Fatalf("preserveAssociatedResourceRuleUpdateFields: %v", err)
	}
	if !reflect.DeepEqual(got, request) {
		t.Fatalf("request = %#v, want %#v", got, request)
	}
	if len(caller.calls) != 0 {
		t.Fatalf("calls = %#v, want none", caller.calls)
	}
}

func TestPreserveAssociatedResourceRuleUpdateFieldsReturnsOriginalWhenRuleMissing(t *testing.T) {
	caller := &fakeOperationCaller{responses: []map[string]any{{"Rules": []any{
		map[string]any{"SettingName": "other", "Status": "Enable"},
	}}}}
	request := map[string]any{"SettingName": "target"}

	got, err := preserveAssociatedResourceRuleUpdateFields(context.Background(), caller, request)
	if err != nil {
		t.Fatalf("preserveAssociatedResourceRuleUpdateFields: %v", err)
	}
	if !reflect.DeepEqual(got, request) {
		t.Fatalf("request = %#v, want %#v", got, request)
	}
}

func TestPreserveAssociatedResourceRuleUpdateFieldsReturnsCallerError(t *testing.T) {
	wantErr := errors.New("list failed")
	_, err := preserveAssociatedResourceRuleUpdateFields(context.Background(), &fakeOperationCaller{err: wantErr}, map[string]any{
		"SettingName": "target",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestAssociatedResourceRuleHelpers(t *testing.T) {
	if got := associatedResourceRule(map[string]any{"Rules": []map[string]any{{"SettingName": "target"}}}, "target"); got == nil {
		t.Fatal("associatedResourceRule did not find map slice rule")
	}
	if got := associatedResourceRule(map[string]any{"Rules": []any{"bad"}}, "target"); got != nil {
		t.Fatalf("associatedResourceRule = %#v, want nil", got)
	}
	if !tagRequestHasValue(map[string]any{"Status": testStringer(" Enable ")}, "Status") {
		t.Fatal("tagRequestHasValue did not accept stringer")
	}
	if !tagRequestHasPrefix(map[string]any{"TagKeys": "env"}, "TagKeys") {
		t.Fatal("tagRequestHasPrefix did not accept exact prefix key")
	}
	if got := tagStringListValue([]any{"env", " ", testStringer("team")}); !reflect.DeepEqual(got, []string{"env", "team"}) {
		t.Fatalf("tagStringListValue = %#v", got)
	}
	if got := tagStringValue(nil); got != "" {
		t.Fatalf("tagStringValue(nil) = %q", got)
	}
}

type testStringer string

func (s testStringer) String() string { return fmt.Sprint(string(s)) }
