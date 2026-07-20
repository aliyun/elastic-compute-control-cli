package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func TestWriteErrorRendersStdoutJSON(t *testing.T) {
	var out bytes.Buffer
	err := ecerrors.Client("DependencyConflict", "vpc still has dependencies",
		ecerrors.WithSuggestion("delete vswitch resources first"),
	)

	if got := WriteError(&out, err); got != 1 {
		t.Fatalf("WriteError exit code = %d, want 1", got)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("JSON output contains ANSI escape: %q", out.String())
	}

	var decoded struct {
		Error struct {
			Code       string `json:"code"`
			Message    string `json:"message"`
			Retryable  bool   `json:"retryable"`
			Suggestion string `json:"suggestion"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("WriteError output is not JSON: %v", err)
	}
	if decoded.Error.Code != "DependencyConflict" {
		t.Fatalf("error.code = %q", decoded.Error.Code)
	}
	if decoded.Error.Suggestion == "" {
		t.Fatal("error.suggestion missing")
	}
}

func TestWriteJSONRendersPrettyObject(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, map[string]any{"dry_run": "passed"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	want := "{\n  \"dry_run\": \"passed\"\n}\n"
	if got := out.String(); got != want {
		t.Fatalf("WriteJSON = %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("WriteJSON without color contains ANSI escape: %q", out.String())
	}
}

func TestWriteJSONColorsWhenEnabled(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, ModeJSON, map[string]any{
		"id":      "vpc-123",
		"enabled": true,
		"count":   3,
		"parent":  nil,
	}, TextOptions{Color: true}); err != nil {
		t.Fatalf("Write json: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "\x1b[36m\"id\"\x1b[0m") {
		t.Fatalf("colored JSON missing cyan key: %q", got)
	}
	if !strings.Contains(got, "\x1b[32m\"vpc-123\"\x1b[0m") {
		t.Fatalf("colored JSON missing green string: %q", got)
	}
	if !strings.Contains(got, "\x1b[33mtrue\x1b[0m") {
		t.Fatalf("colored JSON missing yellow bool: %q", got)
	}
	if !strings.Contains(got, "\x1b[90mnull\x1b[0m") {
		t.Fatalf("colored JSON missing dim null: %q", got)
	}
}

func TestWriteJSONCompactsWhenRequested(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, ModeJSON, map[string]any{
		"id":   "vpc-123",
		"tags": []string{"prod"},
	}, TextOptions{CompactJSON: true}); err != nil {
		t.Fatalf("Write compact json: %v", err)
	}
	want := "{\"id\":\"vpc-123\",\"tags\":[\"prod\"]}\n"
	if got := out.String(); got != want {
		t.Fatalf("compact JSON = %q, want %q", got, want)
	}
}

func TestWriteTextRendersHumanReadableObject(t *testing.T) {
	var out bytes.Buffer
	err := WriteText(&out, map[string]any{
		"vpcs": []map[string]any{{
			"id":     "vpc-123",
			"name":   "prod",
			"status": "available",
		}},
		"total": 1,
	}, TextOptions{})
	if err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "{") || strings.Contains(got, "\"vpcs\"") {
		t.Fatalf("text output should not be JSON: %q", got)
	}
	for _, want := range []string{"vpcs:", "- id: vpc-123", "name: prod", "status: available", "total: 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("text output without color option contains ANSI escape: %q", got)
	}
}

func TestWriteTextRendersWholeNumbersWithoutFloatSuffix(t *testing.T) {
	var out bytes.Buffer
	err := WriteText(&out, map[string]any{
		"pagination": map[string]any{
			"limit":     2,
			"next_page": float64(2),
			"returned":  float64(2),
		},
		"security_groups": []map[string]any{{
			"rule_count":                float64(3),
			"group_to_group_rule_count": float64(0),
		}},
		"total": float64(161),
	}, TextOptions{})
	if err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got := out.String()
	for _, forbidden := range []string{"2.0", "3.0", "0.0", "161.0"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("text output contains float suffix %q: %q", forbidden, got)
		}
	}
	for _, want := range []string{"limit: 2", "next_page: 2", "returned: 2", "rule_count: 3", "group_to_group_rule_count: 0", "total: 161"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q: %q", want, got)
		}
	}
}

func TestWriteTextColorsWhenEnabled(t *testing.T) {
	var out bytes.Buffer
	err := WriteText(&out, map[string]any{"error": map[string]any{"code": "MissingRegion", "message": "region is required"}}, TextOptions{Color: true})
	if err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("colored text output missing ANSI escape: %q", out.String())
	}
}

func TestWriteDispatchesTextAndJSONModes(t *testing.T) {
	var textOut bytes.Buffer
	if err := Write(&textOut, ModeText, map[string]any{"id": "vpc-123"}, TextOptions{}); err != nil {
		t.Fatalf("Write text: %v", err)
	}
	if got := textOut.String(); !strings.Contains(got, "id: vpc-123") {
		t.Fatalf("text output = %q", got)
	}

	var jsonOut bytes.Buffer
	if err := Write(&jsonOut, "", map[string]any{"id": "vpc-123"}, TextOptions{}); err != nil {
		t.Fatalf("Write json: %v", err)
	}
	want := "{\n  \"id\": \"vpc-123\"\n}\n"
	if got := jsonOut.String(); got != want {
		t.Fatalf("json output = %q, want %q", got, want)
	}
}

func TestWriteErrorModeHandlesNilErrorAndActions(t *testing.T) {
	var nilOut bytes.Buffer
	if got := WriteErrorMode(&nilOut, nil, ModeText, TextOptions{}); got != 1 {
		t.Fatalf("nil error exit code = %d, want 1", got)
	}
	if !strings.Contains(nilOut.String(), "code: InternalError") {
		t.Fatalf("nil error output = %q", nilOut.String())
	}

	var actionOut bytes.Buffer
	err := ecerrors.WithActions(
		ecerrors.Service("CloudAPIError", "request failed", false),
		[]ecerrors.Action{{ActionName: "CreateVpc", RequestID: "req-1"}},
	)
	appErr, ok := err.(*ecerrors.AppError)
	if !ok {
		t.Fatalf("WithActions returned %T", err)
	}
	if got := WriteErrorMode(&actionOut, appErr, ModeJSON, TextOptions{}); got != 2 {
		t.Fatalf("action error exit code = %d, want 2", got)
	}
	if !strings.Contains(actionOut.String(), `"actions"`) || !strings.Contains(actionOut.String(), `"CreateVpc"`) {
		t.Fatalf("action error output = %q", actionOut.String())
	}
}

func TestWriteTextReturnsMarshalError(t *testing.T) {
	var out bytes.Buffer
	if err := WriteText(&out, map[string]any{"bad": func() {}}, TextOptions{}); err == nil {
		t.Fatal("WriteText succeeded for unsupported value")
	}
}

func TestColorizeTextKeyLeavesNonPlainKeysUnchanged(t *testing.T) {
	for _, line := range []string{
		"not a key: value\n",
		"url:http://example.com\n",
		": empty\n",
		"plain value\n",
	} {
		t.Run(line, func(t *testing.T) {
			if got := colorizeTextKey(line); got != line {
				t.Fatalf("colorizeTextKey(%q) = %q", line, got)
			}
		})
	}
}

func TestColorizeTextKeyHandlesListItems(t *testing.T) {
	got := colorizeTextKey("  - id: vpc-123\n")
	if !strings.Contains(got, "\x1b[36mid\x1b[0m: vpc-123") {
		t.Fatalf("colored list item = %q", got)
	}
}

func TestIsSupportedModeAcceptsDocumentedModesOnly(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{mode: "", want: true},
		{mode: ModeJSON, want: true},
		{mode: ModeText, want: true},
		{mode: "table", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			if got := IsSupportedMode(tt.mode); got != tt.want {
				t.Fatalf("IsSupportedMode(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}
