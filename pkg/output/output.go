package output

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/goccy/go-yaml"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

const (
	ModeJSON = "json"
	ModeText = "text"
)

type TextOptions struct {
	Color       bool
	CompactJSON bool
}

func IsSupportedMode(mode string) bool {
	return mode == "" || mode == ModeJSON || mode == ModeText
}

func Write(w io.Writer, mode string, v any, options TextOptions) error {
	if mode == ModeText {
		return WriteText(w, v, options)
	}
	return writeJSON(w, v, options)
}

func WriteJSON(w io.Writer, v any) error {
	return writeJSON(w, v, TextOptions{})
}

func writeJSON(w io.Writer, v any, options TextOptions) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if !options.CompactJSON {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(v); err != nil {
		return err
	}
	text := buf.String()
	if options.Color {
		text = colorizeJSON(text)
	}
	_, err := io.WriteString(w, text)
	return err
}

func WriteText(w io.Writer, v any, options TextOptions) error {
	normalized, err := normalizeTextValue(v)
	if err != nil {
		return err
	}
	raw, err := yaml.MarshalWithOptions(normalized, yaml.Indent(2))
	if err != nil {
		return err
	}
	text := string(raw)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if options.Color {
		text = colorizeText(text)
	}
	_, err = io.WriteString(w, text)
	return err
}

func WriteError(w io.Writer, err *ecerrors.AppError) int {
	return WriteErrorMode(w, err, ModeJSON, TextOptions{})
}

func WriteErrorMode(w io.Writer, err *ecerrors.AppError, mode string, options TextOptions) int {
	if err == nil {
		err = ecerrors.Client("InternalError", "internal error")
	}
	payload := map[string]any{"error": err.Payload()}
	if actions := err.Actions(); len(actions) > 0 {
		payload["actions"] = actions
	}
	_ = Write(w, mode, payload, options)
	return err.ExitCode()
}

func normalizeTextValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	return normalizeJSONNumbers(normalized), nil
}

func normalizeJSONNumbers(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeJSONNumbers(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = normalizeJSONNumbers(item)
		}
		return typed
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if float, err := typed.Float64(); err == nil {
			return float
		}
		return typed.String()
	default:
		return value
	}
}

func colorizeText(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		lines[i] = colorizeTextKey(line)
	}
	return strings.Join(lines, "")
}

func colorizeTextKey(line string) string {
	start := 0
	for start < len(line) && line[start] == ' ' {
		start++
	}
	if strings.HasPrefix(line[start:], "- ") {
		start += 2
	}
	keyEnd := plainKeyEnd(line, start)
	if keyEnd < 0 {
		return line
	}
	code := "36"
	key := line[start:keyEnd]
	if key == "error" || key == "code" {
		code = "1;31"
	}
	return line[:start] + colorize(code, key) + line[keyEnd:]
}

func plainKeyEnd(line string, start int) int {
	for i := start; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		if i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '\n' {
			continue
		}
		if i == start || !isPlainKey(line[start:i]) {
			return -1
		}
		return i
	}
	return -1
}

func isPlainKey(key string) bool {
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

func colorize(code string, value string) string {
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func colorizeJSON(text string) string {
	var buf strings.Builder
	buf.Grow(len(text) + len(text)/4)
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '"':
			end := jsonStringEnd(text, i)
			next := end
			for next < len(text) && (text[next] == ' ' || text[next] == '\t') {
				next++
			}
			if next < len(text) && text[next] == ':' {
				buf.WriteString(colorize("36", text[i:end]))
			} else {
				buf.WriteString(colorize("32", text[i:end]))
			}
			i = end
		case c == 't' && i+4 <= len(text) && text[i:i+4] == "true":
			buf.WriteString(colorize("33", "true"))
			i += 4
		case c == 'f' && i+5 <= len(text) && text[i:i+5] == "false":
			buf.WriteString(colorize("33", "false"))
			i += 5
		case c == 'n' && i+4 <= len(text) && text[i:i+4] == "null":
			buf.WriteString(colorize("90", "null"))
			i += 4
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			if text[j] == '-' {
				j++
			}
			for j < len(text) {
				r := text[j]
				if (r >= '0' && r <= '9') || r == '.' || r == 'e' || r == 'E' || r == '+' || r == '-' {
					j++
					continue
				}
				break
			}
			buf.WriteString(text[i:j])
			i = j
		default:
			buf.WriteByte(c)
			i++
		}
	}
	return buf.String()
}

func jsonStringEnd(text string, start int) int {
	j := start + 1
	for j < len(text) {
		switch text[j] {
		case '\\':
			if j+1 < len(text) {
				j += 2
				continue
			}
			j++
		case '"':
			return j + 1
		default:
			j++
		}
	}
	return j
}
