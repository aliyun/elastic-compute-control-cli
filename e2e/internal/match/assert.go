package match

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/PaesslerAG/gval"
	"github.com/PaesslerAG/jsonpath"
)

// assertLang is gval's full expression language extended with JSONPath
// placeholders ($, @) and a few string/length helpers: the assert escape hatch
// uses gval expressions rather than CEL.
//
// Supported beyond gval builtins:
//
//	$.a.b            JSONPath into the document
//	len(x)           length of string/array/object
//	hasPrefix(s, p)  / hasSuffix(s, p) / contains(s, sub)
//	matches(s, re)   regexp match
var assertLang = gval.Full(
	jsonpath.PlaceholderExtension(),
	gval.Function("len", gvalLen),
	gval.Function("hasPrefix", strings.HasPrefix),
	gval.Function("hasSuffix", strings.HasSuffix),
	gval.Function("contains", strings.Contains),
	gval.Function("matches", func(s, re string) (bool, error) {
		return regexp.MatchString(re, s)
	}),
)

func gvalLen(v any) (float64, error) {
	switch x := v.(type) {
	case string:
		return float64(len(x)), nil
	case []any:
		return float64(len(x)), nil
	case map[string]any:
		return float64(len(x)), nil
	}
	return 0, fmt.Errorf("len: unsupported type %T", v)
}

// assert evaluates a boolean expression against doc.
func assert(doc any, expr string) (bool, error) {
	v, err := assertLang.Evaluate(expr, doc)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("assert did not evaluate to a bool (got %T)", v)
	}
	return b, nil
}
