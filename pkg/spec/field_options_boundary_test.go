package spec

import (
	"strings"
	"testing"
)

func TestOperationFieldOptionsRejectWrongTypes(t *testing.T) {
	for _, tt := range []struct{ option, value string }{
		{"required", "wrong"}, {"repeatable", "wrong"}, {"positional", "wrong"},
		{"positional_many", "wrong"}, {"allow_empty", "wrong"}, {"brief", "wrong"}, {"schema", "wrong"},
		{"flag_name", "true"}, {"flag_name", `" "`}, {"input_style", "true"},
		{"enum", "scalar"}, {"enum", "[1]"}, {"description", "true"}, {"description", "{en: true}"}, {"unsupported", "true"},
	} {
		t.Run(tt.option+"/"+tt.value, func(t *testing.T) {
			raw := "schema_version: 2\nproduct: fake\nresource: item\noperations:\n  list:\n    input:\n      fields:\n        - id:\n            " + tt.option + ": " + tt.value + "\n"
			if _, err := Load([]byte(raw)); err == nil || !strings.Contains(err.Error(), tt.option) {
				t.Fatalf("invalid field option accepted or unclear error: %v", err)
			}
		})
	}
}
