// Package match evaluates scenario matchers and assert expressions against a
// decoded JSON document (the parsed stdout of an ecctl command).
package match

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"ecctl/e2e/internal/jsonq"
	"ecctl/e2e/internal/scenario"
)

// Result is the outcome of one path matcher or assert expression, for reports.
type Result struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Step evaluates every expect matcher and assert expression of a step against
// doc, with `at` as the base path. It returns one Result per check and whether
// all passed.
func Step(doc any, at string, exps scenario.Expectations, asserts []string) ([]Result, bool) {
	var results []Result
	allOK := true
	for _, pm := range exps {
		val, found := jsonq.Get(doc, at, pm.Path)
		ok, detail := matcher(pm.Matcher, found, val)
		if !ok {
			allOK = false
		}
		results = append(results, Result{Path: jsonq.Join(at, pm.Path), OK: ok, Detail: detail})
	}
	for _, expr := range asserts {
		ok, err := assert(doc, expr)
		detail := ""
		if err != nil {
			ok, detail = false, err.Error()
		}
		if !ok {
			allOK = false
		}
		results = append(results, Result{Path: "assert: " + expr, OK: ok, Detail: detail})
	}
	return results, allOK
}

func matcher(m scenario.Matcher, found bool, val any) (bool, string) {
	if m.Exists != nil {
		if found != *m.Exists {
			return false, fmt.Sprintf("exists: want %v, got %v", *m.Exists, found)
		}
		if !*m.Exists {
			return true, ""
		}
	}
	if !found {
		return false, "path not found"
	}
	if m.Type != "" && !matchType(m.Type, val) {
		return false, fmt.Sprintf("type: want %s, got %T", m.Type, val)
	}
	if m.HasEq && !valueEqual(m.Eq, val) {
		return false, fmt.Sprintf("eq: want %v, got %v", m.Eq, val)
	}
	if m.HasNe && valueEqual(m.Ne, val) {
		return false, fmt.Sprintf("ne: should not equal %v", m.Ne)
	}
	if m.HasPrefix || m.HasSuffix || m.HasContains || m.Regex != "" {
		s, ok := val.(string)
		if !ok {
			return false, fmt.Sprintf("string matcher on non-string %T", val)
		}
		if m.HasPrefix && !strings.HasPrefix(s, m.Prefix) {
			return false, fmt.Sprintf("prefix: %q lacks %q", s, m.Prefix)
		}
		if m.HasSuffix && !strings.HasSuffix(s, m.Suffix) {
			return false, fmt.Sprintf("suffix: %q lacks %q", s, m.Suffix)
		}
		if m.HasContains && !strings.Contains(s, m.Contains) {
			return false, fmt.Sprintf("contains: %q lacks %q", s, m.Contains)
		}
		if m.Regex != "" {
			re, err := regexp.Compile(m.Regex)
			if err != nil {
				return false, "regex: " + err.Error()
			}
			if !re.MatchString(s) {
				return false, fmt.Sprintf("regex: %q !~ %q", s, m.Regex)
			}
		}
	}
	if m.HasOneOf {
		matched := false
		for _, c := range m.OneOf {
			if valueEqual(c, val) {
				matched = true
				break
			}
		}
		if !matched {
			return false, fmt.Sprintf("oneof: %v not in %v", val, m.OneOf)
		}
	}
	if m.Gt != nil || m.Ge != nil || m.Lt != nil || m.Le != nil {
		f, ok := toFloat(val)
		if !ok {
			return false, fmt.Sprintf("numeric matcher on non-number %T", val)
		}
		if m.Gt != nil && !(f > *m.Gt) {
			return false, fmt.Sprintf("gt: %v !> %v", f, *m.Gt)
		}
		if m.Ge != nil && !(f >= *m.Ge) {
			return false, fmt.Sprintf("ge: %v !>= %v", f, *m.Ge)
		}
		if m.Lt != nil && !(f < *m.Lt) {
			return false, fmt.Sprintf("lt: %v !< %v", f, *m.Lt)
		}
		if m.Le != nil && !(f <= *m.Le) {
			return false, fmt.Sprintf("le: %v !<= %v", f, *m.Le)
		}
	}
	if m.Len != nil || m.MinLen != nil || m.MaxLen != nil {
		n, ok := lengthOf(val)
		if !ok {
			return false, fmt.Sprintf("len matcher on %T", val)
		}
		if m.Len != nil && n != *m.Len {
			return false, fmt.Sprintf("len: want %d, got %d", *m.Len, n)
		}
		if m.MinLen != nil && n < *m.MinLen {
			return false, fmt.Sprintf("min_len: %d < %d", n, *m.MinLen)
		}
		if m.MaxLen != nil && n > *m.MaxLen {
			return false, fmt.Sprintf("max_len: %d > %d", n, *m.MaxLen)
		}
	}
	if m.Each != nil {
		arr, ok := val.([]any)
		if !ok {
			return false, fmt.Sprintf("each on non-array %T", val)
		}
		for i, el := range arr {
			if ok, detail := matcher(*m.Each, true, el); !ok {
				return false, fmt.Sprintf("each[%d]: %s", i, detail)
			}
		}
	}
	return true, ""
}

func matchType(t string, val any) bool {
	switch t {
	case "string":
		_, ok := val.(string)
		return ok
	case "bool":
		_, ok := val.(bool)
		return ok
	case "number":
		_, ok := toFloat(val)
		return ok
	case "integer":
		f, ok := toFloat(val)
		return ok && f == math.Trunc(f)
	case "array":
		_, ok := val.([]any)
		return ok
	case "object":
		_, ok := val.(map[string]any)
		return ok
	case "null":
		return val == nil
	}
	return false
}

func valueEqual(a, b any) bool {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		value, err := n.Float64()
		return value, err == nil
	}
	return 0, false
}

func lengthOf(v any) (int, bool) {
	switch x := v.(type) {
	case string:
		return len(x), true
	case []any:
		return len(x), true
	case map[string]any:
		return len(x), true
	}
	return 0, false
}
