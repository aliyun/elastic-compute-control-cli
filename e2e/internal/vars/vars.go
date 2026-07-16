// Package vars renders Go-template strings ({{.x}}, {{.stack.y}}) used in case
// commands, captures and teardowns. Missing keys are errors so typos surface
// immediately rather than rendering as "<no value>".
package vars

import (
	"bytes"
	"text/template"
)

// Render expands tmpl against data. data is a flat map whose values may be
// nested maps (e.g. data["stack"] = map[string]any{...}).
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("v").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Clone returns a shallow copy of m so callers can add per-scope keys without
// mutating the shared base.
func Clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+4)
	for k, v := range m {
		out[k] = v
	}
	return out
}
