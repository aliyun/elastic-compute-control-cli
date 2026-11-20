package exec

import "testing"

func TestDecodeStrictJSONRejectsAmbiguousRecoveryDocuments(t *testing.T) {
	for _, body := range []string{
		`{"deleted":true,"deleted":false}`,
		`{"Deleted":true,"deleted":false}`,
		`{"outer":{"ID":"a","id":"b"}}`,
		`{"K":"a","K":"b"}`,
		`{"deleted":true}{"deleted":false}`,
		`{"deleted":true} trailing`,
	} {
		if _, err := DecodeStrictJSON([]byte(body)); err == nil {
			t.Errorf("ambiguous document accepted: %s", body)
		}
	}
	if _, err := DecodeStrictJSON([]byte(" {\"deleted\":true} \n")); err != nil {
		t.Fatalf("single document rejected: %v", err)
	}
}

func TestOrdinaryCommandJSONPreservesCaseSensitiveMetadata(t *testing.T) {
	stdout := []byte(`{"metadata":{"Key":"upper","key":"lower"}}`)
	decoded, err := DecodeCommandJSON(stdout)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("ordinary result lost JSON: %+v", decoded)
	}
	metadata, ok := root["metadata"].(map[string]any)
	if !ok || metadata["Key"] != "upper" || metadata["key"] != "lower" {
		t.Fatalf("case-sensitive metadata was conflated: %+v", metadata)
	}
	if _, err := DecodeStrictJSON(stdout); err == nil {
		t.Fatal("typed recovery decoder accepted case-fold collision")
	}
}
