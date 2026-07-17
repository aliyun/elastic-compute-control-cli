package specs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

type testHookCaller struct{}

func (testHookCaller) CallRaw(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestOperationHookRegistriesLookupByExplicitName(t *testing.T) {
	before := func(_ context.Context, _ OperationCaller, request map[string]any) (map[string]any, error) {
		request["before"] = true
		return request, nil
	}
	RegisterBeforeOperation("test-product", "test-resource", "before_hook", before)
	gotBefore, ok := BeforeOperationHook("test-product", "test-resource", "before_hook")
	if !ok {
		t.Fatal("BeforeOperationHook returned ok=false")
	}
	request, err := gotBefore(context.Background(), testHookCaller{}, map[string]any{})
	if err != nil || request["before"] != true {
		t.Fatalf("before hook request=%#v err=%v", request, err)
	}

	afterErr := errors.New("original")
	after := func(_ context.Context, _ OperationCaller, _ map[string]any, err error) error {
		return fmt.Errorf("wrapped: %w", err)
	}
	RegisterAfterOperationError("test-product", "test-resource", "after_hook", after)
	gotAfter, ok := AfterOperationErrorHook("test-product", "test-resource", "after_hook")
	if !ok {
		t.Fatal("AfterOperationErrorHook returned ok=false")
	}
	if err := gotAfter(context.Background(), testHookCaller{}, nil, afterErr); err == nil || !errors.Is(err, afterErr) {
		t.Fatalf("after hook err=%v, want wrapping original", err)
	}
}

func TestFieldMapperUsesSpecificThenGlobalMapper(t *testing.T) {
	RegisterFieldMapper("test-product", "test-resource", "name", func(value any) any {
		return "specific:" + hookStringValue(value)
	})
	if got, ok := MapField("test-product", "test-resource", "name", "value"); !ok || got != "specific:value" {
		t.Fatalf("specific mapper got=%#v ok=%v", got, ok)
	}

	if got, ok := MapField("unknown", "unknown", "tags", []any{
		map[string]any{"TagKey": "env", "TagValue": "prod"},
		map[string]any{"Key": "team", "Value": "platform"},
	}); !ok || !reflect.DeepEqual(got, map[string]string{"env": "prod", "team": "platform"}) {
		t.Fatalf("global tags mapper got=%#v ok=%v", got, ok)
	}
}

func TestBindingItemNormalizerRegistry(t *testing.T) {
	RegisterBindingItemNormalizer("test-product", "test-resource", "normalize", func(value map[string]any, index int) (map[string]any, error) {
		value["index"] = index
		return value, nil
	})
	got, ok, err := NormalizeBindingItem("test-product", "test-resource", "normalize", map[string]any{"name": "one"}, 3)
	if err != nil || !ok || got["index"] != 3 || got["name"] != "one" {
		t.Fatalf("NormalizeBindingItem got=%#v ok=%v err=%v", got, ok, err)
	}

	got, ok, err = NormalizeBindingItem("test-product", "test-resource", "missing", nil, 0)
	if err != nil || ok || got != nil {
		t.Fatalf("missing normalizer got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestHookConversionHelpers(t *testing.T) {
	if got := hookAnySlice([]map[string]any{{"key": "value"}}); len(got) != 1 {
		t.Fatalf("hookAnySlice map slice = %#v", got)
	}
	if got := hookAnySlice("not-a-slice"); got != nil {
		t.Fatalf("hookAnySlice unsupported = %#v, want nil", got)
	}
	if got := hookStringValue(testStringer("value")); got != "value" {
		t.Fatalf("hookStringValue stringer = %q", got)
	}
}

type testStringer string

func (s testStringer) String() string { return string(s) }
