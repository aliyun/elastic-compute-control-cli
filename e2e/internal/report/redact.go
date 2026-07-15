package report

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// Sensitive flag values and key=value pairs are scrubbed from commands, stderr
// and manifests before any report is written or uploaded.
var redactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(--password[ =])\S+`),
	regexp.MustCompile(`(?i)(--user-data[ =])\S+`),
	regexp.MustCompile(`(?i)(--key-pair-name[ =])\S+`),
	regexp.MustCompile(`(?i)((?:access[_-]?key(?:id|secret)?|secret|security[_-]?token|password|token|client-key-data|client-certificate-data|certificate-authority-data)\s*[:=]\s*)\S+`),
}

func scrub(s string) string {
	if structured, ok := scrubStructuredJSON(s); ok {
		return structured
	}
	return scrubText(s)
}

// Scrub removes sensitive values from text before it is emitted outside the
// report rendering pipeline, for example in cleanup diagnostics and logs.
func Scrub(s string) string {
	return scrub(s)
}

func scrubText(s string) string {
	for _, re := range redactors {
		s = re.ReplaceAllString(s, "${1}***")
	}
	return s
}

func scrubStructuredJSON(s string) (string, bool) {
	decoder := json.NewDecoder(bytes.NewBufferString(s))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", false
	}
	value = redactJSONValue(value, nil)
	data, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func redactJSONValue(value any, parents []string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := normalizeSensitiveKey(key)
			if isSensitiveKey(normalized) || (normalized == "config" && containsKey(parents, "kubeconfig")) {
				typed[key] = "***"
				continue
			}
			typed[key] = redactJSONValue(child, append(parents, normalized))
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = redactJSONValue(typed[i], parents)
		}
		return typed
	case string:
		return scrubText(typed)
	default:
		return value
	}
}

func normalizeSensitiveKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	return strings.ToLower(replacer.Replace(key))
}

func isSensitiveKey(key string) bool {
	switch key {
	case "accesskeyid", "accesskeysecret", "securitytoken", "password", "token", "secret",
		"clientkeydata", "clientcertificatedata", "certificateauthoritydata":
		return true
	default:
		return false
	}
}

func containsKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

// Redact scrubs sensitive material in place. Call before writing reports.
func (r *Run) Redact() {
	redactCases(r.Cases)
	redactManifest(r.Manifest)
	for i := range r.RegionAttempts {
		r.RegionAttempts[i].Reason = scrub(r.RegionAttempts[i].Reason)
	}
	for i := range r.Executions {
		execution := &r.Executions[i]
		execution.Error = scrub(execution.Error)
		redactCases(execution.Cases)
		redactManifest(execution.Manifest)
		for j := range execution.Attempts {
			execution.Attempts[j].Reason = scrub(execution.Attempts[j].Reason)
		}
	}
}

func redactCases(cases []Case) {
	for i := range cases {
		cases[i].Error = scrub(cases[i].Error)
		for j := range cases[i].Steps {
			st := &cases[i].Steps[j]
			st.Command = scrub(st.Command)
			st.Stdout = scrub(st.Stdout)
			st.Stderr = scrub(st.Stderr)
			st.Error = scrub(st.Error)
			for k := range st.Checks {
				st.Checks[k].Detail = scrub(st.Checks[k].Detail)
			}
		}
	}
}

func redactManifest(manifest []Resource) {
	for i := range manifest {
		manifest[i].Teardown = scrub(manifest[i].Teardown)
	}
}
