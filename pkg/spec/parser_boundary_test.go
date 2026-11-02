package spec

import (
	"errors"
	"reflect"
	"testing"
)

func TestSpecParsersPreserveDecodeErrorsAndRejectInvalidFieldShapes(t *testing.T) {
	cause := errors.New("invalid YAML value")
	decode := func(any) error { return cause }
	for _, parse := range []func() error{
		func() error { return new(ProbeField).UnmarshalYAML(decode) },
		func() error { return new(OperationFields).UnmarshalYAML(decode) },
	} {
		if err := parse(); !errors.Is(err, cause) {
			t.Fatalf("YAML decode error lost: %v", err)
		}
	}
	for _, value := range []any{map[string]any{" ": nil}, map[string]any{"name": true}, 42} {
		if _, err := operationFieldRefFromYAMLValue(value); err == nil {
			t.Fatalf("invalid input field accepted: %#v", value)
		}
	}
	if ref, err := operationFieldRefFromYAMLValue(map[string]any{"name": nil}); err != nil || ref.Name != "name" || !ref.Brief {
		t.Fatalf("empty field options=%+v,%v", ref, err)
	}
	for _, value := range []any{"", map[string]any{}} {
		if text, err := localizedTextFromAny(value); err != nil || text != nil {
			t.Fatalf("empty localization=%v,%v", text, err)
		}
	}
	if text, err := localizedTextFromAny("description"); err != nil || text.Text("en") != "description" {
		t.Fatalf("scalar localization=%v,%v", text, err)
	}
	text := LocalizedText{"en": "old"}
	if err := text.UnmarshalYAML(func(target any) error {
		if out, ok := target.(*map[string]string); ok {
			*out = map[string]string{}
			return nil
		}
		return cause
	}); err != nil || text != nil {
		t.Fatalf("empty map did not clear old localization: %v,%v", text, err)
	}
	if got := (LocalizedText{"ZH_cn": "description"}).Text("zh-CN"); got != "description" {
		t.Fatalf("normalized locale lost: %s", got)
	}
	if languageBase("") != "" {
		t.Fatal("empty locale has a language base")
	}
}

func TestSpecNestedSchemasAndCaptureNamesRejectAmbiguity(t *testing.T) {
	for _, field := range []SchemaField{{}, {Type: "array", Items: &SchemaField{}}, {Type: "object", Fields: map[string]SchemaField{"child": {}}}} {
		if validateSchemaField(field) == nil {
			t.Fatalf("invalid nested schema accepted: %+v", field)
		}
	}
	for _, capture := range []any{" ", map[string]any{"name": ""}, map[string]any{"name": "items", "fields": true}, true} {
		if _, _, err := bindingRequestCapture(capture); err == nil {
			t.Fatalf("invalid capture accepted: %#v", capture)
		}
	}
	for _, request := range []map[string]any{
		{"item": map[string]any{"raw": "$.item", "capture": "items"}},
		{"item": map[string]any{"from": "$.item", "capture": "items"}},
		{"first": map[string]any{"each": "$.first", "capture": "items"}, "second": map[string]any{"each": "$.second", "capture": "items"}},
	} {
		if _, err := bindingRequestCaptureFields(request); err == nil {
			t.Fatalf("ambiguous request capture accepted: %#v", request)
		}
	}
	if !validResponsePath("$") || validResponsePath("$.items..id") || validResponsePath("$.items[0]") {
		t.Fatal("response path boundary changed")
	}
	for _, field := range []ProbeField{{From: "$.items"}, {From: "$.items", Each: map[string]ProbeField{"invalid": {Each: map[string]ProbeField{"child": {Path: "$.id"}}}}}} {
		if validateProbeField(field) == nil {
			t.Fatalf("invalid nested probe field accepted: %+v", field)
		}
	}
	product := ProductSpec{Product: "test", Description: LocalizedText{"en": "description"}, Aliases: []string{" "}, Examples: []string{"one", "two"}}
	if ValidateProduct(product) == nil {
		t.Fatal("blank product alias accepted")
	}
	if got, err := bindingRequestCaptureFields(map[string]any{"items": map[string]any{"each": "$.items", "capture": "items"}}); err != nil || !reflect.DeepEqual(got, map[string]map[string]bool{"items": {}}) {
		t.Fatalf("valid bare capture=%v,%v", got, err)
	}
}
