package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type rejectedOutputWriter struct{ err error }

func (w rejectedOutputWriter) Write([]byte) (int, error) { return 0, w.err }

func TestOutputPropagatesEncodingAndWriterFailures(t *testing.T) {
	cause := errors.New("sink unavailable")
	for _, mode := range []string{ModeJSON, ModeText} {
		var out bytes.Buffer
		if err := Write(&out, mode, make(chan int), TextOptions{}); err == nil || out.Len() != 0 {
			t.Fatal("unencodable value emitted partial output")
		}
		if err := Write(rejectedOutputWriter{cause}, mode, map[string]any{"value": 1}, TextOptions{}); !errors.Is(err, cause) {
			t.Fatalf("writer cause lost: %v", err)
		}
	}
}

func TestTextNumbersAndColoredJSONPreservePayload(t *testing.T) {
	if got := normalizeJSONNumbers(json.Number("1.25")); got != 1.25 {
		t.Fatalf("decimal converted to %v", got)
	}
	if got := normalizeJSONNumbers(json.Number("1e999")); got != "1e999" {
		t.Fatalf("overflowing number lost: %v", got)
	}
	raw := "{\"escaped\\\"key\" \t: false, \"amount\": -1.25, \"value\": \"a\\\\b\"}"
	colored := colorizeJSON(raw)
	for _, code := range []string{"36", "32", "33"} {
		colored = strings.ReplaceAll(colored, "\x1b["+code+"m", "")
	}
	colored = strings.ReplaceAll(colored, "\x1b[0m", "")
	if colored != raw {
		t.Fatalf("coloring changed payload: %q", colored)
	}
	for _, raw := range []string{"\"unterminated", "\"escape\\"} {
		if got := jsonStringEnd(raw, 0); got != len(raw) {
			t.Fatalf("truncated string end=%d", got)
		}
	}
}
