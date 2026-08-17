package spec

// BindingRequestCoverage returns the set of API request parameter paths that a
// binding request mapping covers. The result mirrors how the executor consumes
// the mapping:
//
//   - a plain value covers the parameter name itself;
//   - `each` with `fields` covers Parent.Child paths for every child;
//   - a bare `each` or `from` covers the group path itself;
//   - a `raw` node contributes nothing (it is a passthrough escape hatch);
//   - nested `fields` are flattened recursively.
//
// Callers use this set to compare a binding's request coverage against the
// OpenAPI metadata parameter list for the bound operation.
func BindingRequestCoverage(request map[string]any) map[string]bool {
	covered := map[string]bool{}
	collectBindingRequestCoverage(covered, "", request)
	return covered
}

func collectBindingRequestCoverage(covered map[string]bool, prefix string, request map[string]any) {
	for key, value := range request {
		if key == "capture" || key == "raw" {
			continue
		}
		nextPrefix := key
		if prefix != "" {
			nextPrefix = prefix + "." + key
		}
		node, ok := value.(map[string]any)
		if !ok {
			covered[nextPrefix] = true
			continue
		}
		if _, ok := node["raw"]; ok {
			continue
		}
		if fields, ok := node["fields"].(map[string]any); ok {
			collectBindingRequestCoverage(covered, nextPrefix, fields)
			continue
		}
		if _, ok := node["from"]; ok {
			covered[nextPrefix] = true
			continue
		}
		if _, ok := node["each"]; ok {
			covered[nextPrefix] = true
			continue
		}
		collectBindingRequestCoverage(covered, nextPrefix, node)
	}
}
