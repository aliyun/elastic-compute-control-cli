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

func TestStrictJSONRejectsAmbiguousProviderEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"duplicate root code", `{"code":"VolumeNotFound","code":"200","requestId":"req-1"}`},
		{"duplicate nested request ID", `{"error":{"requestId":"req-1","requestId":"req-2"}}`},
		{"duplicate member in array", `{"volumes":[{"volumeID":"vol-1","volumeID":"vol-2"}]}`},
		{"second JSON value", `{"code":"200"} {"code":"VolumeNotFound"}`},
		{"trailing malformed value", `{"code":"200"} <partial`},
		{"truncated object", `{"code":"200"`},
		{"truncated member value", `{"code":`},
		{"truncated array", `{"volumes":[{"volumeID":"vol-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if value, err := DecodeStrictJSONWithNumbers([]byte(tc.body)); err == nil {
				t.Fatalf("ambiguous provider envelope accepted: %#v", value)
			}
		})
	}

	value, err := DecodeStrictJSONWithNumbers([]byte(`{"volume":{"userID":9007199254740993},"volumes":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	root, ok := value.(map[string]any)
	if !ok || root["volume"].(map[string]any)["userID"] != json.Number("9007199254740993") {
		t.Fatalf("provider integer changed while checking envelope: %#v", value)
	}
}
