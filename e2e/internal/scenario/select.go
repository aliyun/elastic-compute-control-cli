// Selection turns positional targets and a -k keyword expression into the subset
// of cases to run, modeled on pytest: `path`, `dir/`, and `path::step` node ids
// select by location; `-k "vpc or eni"` selects by a boolean keyword expression.
package scenario

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Selection describes which cases to keep out of a loaded set.
type Selection struct {
	Targets []string // positional node ids: dir, file, or file::step
	Keyword string   // -k boolean expression over resource/file name
	Surface Surface  // command surface; empty keeps all suites
}

// Select applies Targets then Keyword to all, preserving input order. A target
// of the form file::step truncates that case to the steps up to and including
// the named step (lifecycle steps are a stateful sequence, so a node id runs the
// prefix that produces the state, not the step in isolation).
func Select(all []*Suite, sel Selection) ([]*Suite, error) {
	if sel.Surface != "" && !sel.Surface.Valid() {
		return nil, fmt.Errorf("surface must be %q or %q", SurfacePublic, SurfaceFull)
	}
	out := all
	if sel.Surface != "" {
		var kept []*Suite
		for _, s := range out {
			if s.Surface == sel.Surface {
				kept = append(kept, s)
			}
		}
		out = kept
	}
	if len(sel.Targets) > 0 {
		var err error
		if out, err = selectTargets(out, sel.Targets); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(sel.Keyword) != "" {
		match, err := compileKeyword(sel.Keyword)
		if err != nil {
			return nil, err
		}
		var kept []*Suite
		for _, s := range out {
			if match(keywordHaystack(s)) {
				kept = append(kept, s)
			}
		}
		out = kept
	}
	return out, nil
}

func selectTargets(all []*Suite, targets []string) ([]*Suite, error) {
	var out []*Suite
	seen := make(map[string]bool)
	for _, s := range all {
		step, ok := matchAnyTarget(s.Path, targets)
		if !ok || seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		if step == "" {
			out = append(out, s)
			continue
		}
		trimmed, err := truncateAfter(s, step)
		if err != nil {
			return nil, err
		}
		out = append(out, trimmed)
	}
	return out, nil
}

// matchAnyTarget reports whether suitePath is selected by any target, returning
// the step name from the first matching file::step node id (empty for dir/file).
func matchAnyTarget(suitePath string, targets []string) (string, bool) {
	sp := filepath.Clean(suitePath)
	for _, t := range targets {
		path, step := splitNode(t)
		cp := filepath.Clean(path)
		if cp == sp {
			return step, true
		}
		// Directory target: suitePath lives under it.
		if rel, err := filepath.Rel(cp, sp); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "", true
		}
	}
	return "", false
}

// splitNode splits a "path::step" node id into its parts.
func splitNode(target string) (path, step string) {
	if i := strings.Index(target, "::"); i >= 0 {
		return target[:i], target[i+2:]
	}
	return target, ""
}

// truncateAfter returns a copy of s containing only the steps up to and
// including the named step.
func truncateAfter(s *Suite, step string) (*Suite, error) {
	for i, st := range s.Steps {
		if st.Name == step {
			clone := *s
			clone.Steps = s.Steps[:i+1]
			return &clone, nil
		}
	}
	return nil, fmt.Errorf("%s: no step named %q", s.Path, step)
}

// keywordHaystack is the lowercased text a -k identifier matches against:
// resource, file base name, and full path.
func keywordHaystack(s *Suite) string {
	base := strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	return strings.ToLower(s.Resource + "\x00" + base + "\x00" + s.Path)
}
