package aliyun

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictJSONKeepsNormalNestedResponseAndBoundsDepth(t *testing.T) {
	value, err := decodeStrictJSON([]byte(`{"result":{"items":[{"id":"item-1"}]}}`))
	if err != nil {
		t.Fatalf("normal nested response was rejected: %v", err)
	}
	root, ok := value.(map[string]any)
	if !ok || root["result"] == nil {
		t.Fatalf("normal nested response changed: %#v", value)
	}
	decoder := json.NewDecoder(strings.NewReader(`{"ok":true}`))
	if _, err := strictJSONValue(decoder, maxStrictJSONDepth+1); err == nil {
		t.Fatal("parser accepted a call beyond its depth bound")
	}
}
