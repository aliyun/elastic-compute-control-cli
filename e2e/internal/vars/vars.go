// Package vars renders Go-template strings ({{.x}}, {{.stack.y}}) used in case
// commands, captures and teardowns. Missing keys are errors so typos surface
// immediately rather than rendering as "<no value>".
package vars

import (
	"bytes"
	"fmt"
	"regexp"
	"text/template"
)

var renderErrorLocation = regexp.MustCompile(`^template: v(:[0-9]+(?::[0-9]+)?):`)

// Render expands tmpl against data. data is a flat map whose values may be
// nested maps (e.g. data["stack"] = map[string]any{...}).
func Render(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("v").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", safeRenderError("parse", err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", safeRenderError("evaluation", err)
	}
	return b.String(), nil
}

// Template errors include action text and values. Retain only the numeric
// location in our fixed root template, never the original error or its cause.
func safeRenderError(phase string, cause error) error {
	if location := renderErrorLocation.FindStringSubmatch(cause.Error()); location != nil {
		return fmt.Errorf("template: v%s: %s error", location[1], phase)
	}
	return fmt.Errorf("template: %s error", phase)
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
