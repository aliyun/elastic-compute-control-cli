package engine

import "testing"

func TestResolveExpressionKeyValueHelpers(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"tag": []string{"team=platform", "env=prod"},
	}}

	value, ok, err := ResolveExpression("$kv_json($.tag)", ctx)
	if err != nil || !ok {
		t.Fatalf("ResolveExpression kv_json: value=%#v ok=%v err=%v", value, ok, err)
	}
	if value != `{"env":"prod","team":"platform"}` {
		t.Fatalf("kv_json = %#v", value)
	}

	single := ExecutionContext{Input: map[string]any{
		"tag": []string{"team=platform"},
	}}
	value, ok, err = ResolveExpression("$kv_key($.tag)", single)
	if err != nil || !ok || value != "team" {
		t.Fatalf("kv_key = %#v ok=%v err=%v, want team", value, ok, err)
	}

	value, ok, err = ResolveExpression("$kv_value($.tag)", single)
	if err != nil || !ok || value != "platform" {
		t.Fatalf("kv_value = %#v ok=%v err=%v, want platform", value, ok, err)
	}
}

func TestResolveExpressionKeyValueHelpersRejectInvalidAssignments(t *testing.T) {
	_, _, err := ResolveExpression("$kv_json($.tag)", ExecutionContext{Input: map[string]any{
		"tag": []string{"missing-separator"},
	}})
	if err == nil {
		t.Fatal("kv_json accepted invalid key=value input")
	}
}

func TestResolveExpressionKeyValueHelpersRejectDuplicateKeys(t *testing.T) {
	_, _, err := ResolveExpression("$kv_json($.tag)", ExecutionContext{Input: map[string]any{
		"tag": []string{"env=prod", "env=stage"},
	}})
	if err == nil {
		t.Fatal("kv_json accepted duplicate tag keys")
	}
}

func TestResolveExpressionJSONMarshalsInputValue(t *testing.T) {
	value, ok, err := ResolveExpression("$json($.ids)", ExecutionContext{Input: map[string]any{
		"ids": []string{"web-key", "ops-key"},
	}})
	if err != nil || !ok {
		t.Fatalf("ResolveExpression $json: value=%#v ok=%v err=%v", value, ok, err)
	}
	if value != `["web-key","ops-key"]` {
		t.Fatalf("$json($.ids) = %#v", value)
	}
}

func TestResolveExpressionKeyValuePairs(t *testing.T) {
	value, ok, err := ResolveExpression("$kv_pairs($.tag)", ExecutionContext{Input: map[string]any{
		"tag": []string{"env=prod", "team=platform"},
	}})
	if err != nil || !ok {
		t.Fatalf("ResolveExpression kv_pairs: value=%#v ok=%v err=%v", value, ok, err)
	}
	want := []map[string]string{
		{"key": "env", "value": "prod"},
		{"key": "team", "value": "platform"},
	}
	if got, ok := value.([]map[string]string); !ok || len(got) != len(want) || got[0]["key"] != want[0]["key"] || got[1]["value"] != want[1]["value"] {
		t.Fatalf("kv_pairs = %#v, want %#v", value, want)
	}
}
