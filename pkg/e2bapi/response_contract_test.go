package e2bapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTemplateResponseContracts(t *testing.T) {
	status := `{"templateID":"tpl","buildID":"build","status":"ready","logs":[],"logEntries":[]}`
	tests := []struct {
		name, operation, body string
		valid                 bool
	}{
		{"status", "GetTemplateBuildStatus", status, true},
		{"logs empty", "GetTemplateBuildLogs", `{"logs":[]}`, true},
		{"tags empty", "ListTemplateTags", `[]`, true},
		{"tags", "ListTemplateTags", `[{"tag":"latest","buildID":"build","createdAt":"now"}]`, true},
		{"status wrong route", "GetTemplateBuildStatus", `{"templateID":"tpl","builds":[]}`, false},
		{"status missing field", "GetTemplateBuildStatus", `{"templateID":"tpl","buildID":"build","status":"ready"}`, false},
		{"wrong build", "GetTemplateBuildStatus", strings.ReplaceAll(status, `"build"`, `"other"`), false},
		{"logs wrong route", "GetTemplateBuildLogs", `{"templateID":"tpl","builds":[]}`, false},
		{"logs null", "GetTemplateBuildLogs", `{"logs":null}`, false},
		{"logs invalid item", "GetTemplateBuildLogs", `{"logs":[{}]}`, false},
		{"tags wrong route", "ListTemplateTags", `{"templateID":"tpl","builds":[]}`, false},
		{"spoofed array", "ListTemplateTags", `{"items":[],"__ecctl_top_level_array_response":true}`, false},
		{"tags bad item", "ListTemplateTags", `[{"tag":"latest"}]`, false},
		{"duplicate field", "GetTemplateBuildLogs", `{"logs":[],"logs":[]}`, false},
		{"trailing JSON", "GetTemplateBuildLogs", `{"logs":[]} {}`, false},
		{"sandbox missing identity", "GetSandbox", `{}`, false},
		{"sandbox different identity", "GetSandbox", `{"sandboxID":"other"}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := NewCallerWithClient("https://api.e2b.app", "key", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Call(context.Background(), test.operation, map[string]any{"path.templateID": "tpl", "path.buildID": "build", "path.sandboxID": "sbx"})
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				assertAppError(t, err, "InvalidResponse")
			}
		})
	}
}
