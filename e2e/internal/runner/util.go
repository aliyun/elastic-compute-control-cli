package runner

import (
	"encoding/json"
	"path/filepath"
	"strings"

	execpkg "ecctl/e2e/internal/exec"
	"ecctl/e2e/internal/jsonq"
	"ecctl/e2e/internal/report"
	"ecctl/e2e/internal/scenario"
	"ecctl/e2e/internal/vars"
)

// renderExpectations substitutes {{.var}} templates inside matcher values and
// assert expressions against the case data, so e.g. `eq: "{{.vpc_id}}"` compares
// the captured id, not the literal template. Returns rendered copies; the
// source suite is left unmodified.
func renderExpectations(exps scenario.Expectations, asserts []string, data map[string]any) (scenario.Expectations, []string, error) {
	rexps := make(scenario.Expectations, len(exps))
	for i, pm := range exps {
		m, err := renderMatcher(pm.Matcher, data)
		if err != nil {
			return nil, nil, err
		}
		rexps[i] = scenario.PathMatcher{Path: pm.Path, Matcher: m}
	}
	rasserts := make([]string, len(asserts))
	for i, a := range asserts {
		r, err := vars.Render(a, data)
		if err != nil {
			return nil, nil, err
		}
		rasserts[i] = r
	}
	return rexps, rasserts, nil
}

func renderMatcher(m scenario.Matcher, data map[string]any) (scenario.Matcher, error) {
	renderAny := func(v any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return v, nil
		}
		return vars.Render(s, data)
	}
	var err error
	if m.HasEq {
		if m.Eq, err = renderAny(m.Eq); err != nil {
			return m, err
		}
	}
	if m.HasNe {
		if m.Ne, err = renderAny(m.Ne); err != nil {
			return m, err
		}
	}
	if m.HasPrefix {
		if m.Prefix, err = vars.Render(m.Prefix, data); err != nil {
			return m, err
		}
	}
	if m.HasSuffix {
		if m.Suffix, err = vars.Render(m.Suffix, data); err != nil {
			return m, err
		}
	}
	if m.HasContains {
		if m.Contains, err = vars.Render(m.Contains, data); err != nil {
			return m, err
		}
	}
	if m.Regex != "" {
		if m.Regex, err = vars.Render(m.Regex, data); err != nil {
			return m, err
		}
	}
	if m.HasOneOf {
		oneof := make([]any, len(m.OneOf))
		for i, o := range m.OneOf {
			if oneof[i], err = renderAny(o); err != nil {
				return m, err
			}
		}
		m.OneOf = oneof
	}
	if m.Each != nil {
		em, err := renderMatcher(*m.Each, data)
		if err != nil {
			return m, err
		}
		m.Each = &em
	}
	return m, nil
}

// failureDetail returns the full failure output of an invocation. ecctl emits
// structured errors as JSON on stdout, with `actions` as a SIBLING of `error`
// ({"error":{...}, "actions":[...]}) — the actions carry the real API
// diagnostics. The entire JSON document is printed (un-truncated) so nothing is
// dropped. Falls back to raw stdout/stderr.
func failureDetail(res execpkg.Result) string {
	if res.JSON != nil {
		if b, err := json.MarshalIndent(res.JSON, "", "  "); err == nil {
			return string(b)
		}
	}
	var parts []string
	if s := strings.TrimSpace(res.Stdout); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(res.Stderr); s != "" {
		parts = append(parts, "stderr: "+s)
	}
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}

func suiteNeeds(suites []*scenario.Suite) []string {
	seen := map[string]bool{}
	var needs []string
	for _, s := range suites {
		for _, need := range s.Needs {
			if !seen[need] {
				seen[need] = true
				needs = append(needs, need)
			}
		}
	}
	return needs
}

// caseStatusLabel maps a case status to a short, fixed-width progress label.
func caseStatusLabel(status string) string {
	switch status {
	case report.StatusPass:
		return "PASS"
	case report.StatusFail:
		return "FAIL"
	case report.StatusSkipped:
		return "SKIP"
	default:
		return strings.ToUpper(status)
	}
}

// caseName derives a display name from the resource and file.
func caseName(s *scenario.Suite) string {
	base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	if base != "" {
		return base
	}
	return s.Resource
}

func caseSlug(resource string) string {
	return strings.ReplaceAll(resource, "/", "-")
}

func caseScope(data map[string]any) string {
	if v, ok := data["run_name"].(string); ok {
		return v
	}
	return "case"
}

func jsonGet(doc any, at, path string) (any, bool) {
	return jsonq.Get(doc, at, path)
}

func oneline(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func summarize(cases []report.Case) report.Summary {
	sum := report.Summary{Cases: len(cases)}
	for _, c := range cases {
		switch c.Status {
		case report.StatusPass:
			sum.Passed++
		case report.StatusSkipped:
			sum.Skipped++
		default:
			sum.Failed++
		}
	}
	return sum
}
