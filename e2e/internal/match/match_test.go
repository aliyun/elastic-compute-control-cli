package match_test

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"

	"ecctl/e2e/internal/match"
	"ecctl/e2e/internal/scenario"
)

func parseStep(t *testing.T, y string) scenario.Step {
	t.Helper()
	var st scenario.Step
	if err := yaml.Unmarshal([]byte(y), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return st
}

func TestNumberMatcherAcceptsJSONNumber(t *testing.T) {
	doc := map[string]any{"default_version": json.Number("1")}
	st := parseStep(t, `
expect:
  default_version: { type: number, eq: 1 }
`)
	results, ok := match.Step(doc, st.At, st.Expect, st.Assert)
	if !ok {
		t.Fatalf("json.Number should match number: %+v", results)
	}
}

func TestMatchersPass(t *testing.T) {
	doc := map[string]any{"vpc": map[string]any{
		"id":     "vpc-123",
		"status": "Available",
		"cidr":   "10.0.0.0/16",
		"ids":    []any{"vsw-1", "vsw-2"},
		"count":  float64(3),
		"flag":   true,
	}}
	st := parseStep(t, `
at: $.vpc
expect:
  id: { type: string, prefix: vpc- }
  status: Available
  cidr: "10.0.0.0/16"
  ids: { type: array, min_len: 1, each: { prefix: vsw- } }
  count: { ge: 1, le: 10 }
  flag: { type: bool }
  missing: { exists: false }
assert:
  - $.vpc.cidr == "10.0.0.0/16" && len($.vpc.ids) == 2
`)
	results, ok := match.Step(doc, st.At, st.Expect, st.Assert)
	if !ok {
		for _, r := range results {
			if !r.OK {
				t.Errorf("unexpected fail: %s: %s", r.Path, r.Detail)
			}
		}
		t.Fatal("expected all matchers to pass")
	}
}

func TestMatchersFail(t *testing.T) {
	doc := map[string]any{"vpc": map[string]any{"id": "vpc-123", "status": "Pending"}}
	st := parseStep(t, `
at: $.vpc
expect:
  id: { prefix: vsw- }
  status: Available
  absent_field: { type: string }
`)
	results, ok := match.Step(doc, st.At, st.Expect, st.Assert)
	if ok {
		t.Fatal("expected failures")
	}
	fails := 0
	for _, r := range results {
		if !r.OK {
			fails++
		}
	}
	if fails != 3 {
		t.Fatalf("expected 3 failing checks, got %d (%+v)", fails, results)
	}
}
