// Package jsonq evaluates JSONPath expressions against decoded JSON documents
// (map[string]any / []any as produced by encoding/json), with support for a
// per-step base path (`at`) that relative keys are resolved against.
package jsonq

import (
	"strings"

	"github.com/PaesslerAG/jsonpath"
)

// Join combines a base path (the step `at`, default "$") with a key. A key that
// starts with "$" is absolute and ignores base. Bracket keys ("[0]") attach
// directly; dotted keys ("router.id") get a separating dot.
func Join(base, key string) string {
	if strings.HasPrefix(key, "$") {
		return key
	}
	if base == "" {
		base = "$"
	}
	if key == "" {
		return base
	}
	if strings.HasPrefix(key, "[") {
		return base + key
	}
	return base + "." + key
}

// Get evaluates Join(base, key) against doc. The boolean reports whether the
// path resolved; a missing path returns (nil, false, nil) rather than an error
// so callers (e.g. the `exists` matcher) can distinguish absence from failure.
func Get(doc any, base, key string) (any, bool) {
	v, err := jsonpath.Get(Join(base, key), doc)
	if err != nil {
		return nil, false
	}
	return v, true
}
