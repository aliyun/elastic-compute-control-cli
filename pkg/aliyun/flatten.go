package aliyun

import "strings"

// OpenAPIOperationLeaves returns the flattened leaf request parameters of an
// OpenAPI operation. Group parameters are expanded into their children:
//
//   - a parameter with sub-parameters (for example RepeatList groups such as
//     DataDisk) contributes one leaf per child, named Parent.Child;
//   - a Struct placeholder whose children are declared as dotted flat names
//     (for example SystemDisk plus SystemDisk.Category) contributes only the
//     dotted leaves, not the bare parent;
//   - a parameter with no child information (a bare Struct or RepeatList)
//     contributes a single leaf under its own name.
//
// The legacy metadata snapshot is preferred because it carries sub-parameter
// trees that the newer snapshot expresses only as FlatName dotted parameters
// or bare group placeholders. Legacy detail is consulted first, with the
// merged accessor as the fallback when the legacy file is missing.
func OpenAPIOperationLeaves(lang string, product OpenAPIProduct, operation string) ([]OpenAPIParameter, bool) {
	detail, ok := legacyOpenAPIOperationDetail(lang, product, operation)
	if !ok {
		detail, ok = OpenAPIOperationDetailFor(lang, product, operation)
		if !ok {
			return nil, false
		}
	}
	return flattenOpenAPIParameters(detail), true
}

func flattenOpenAPIParameters(detail OpenAPIOperationDetail) []OpenAPIParameter {
	// ancestors records every dotted prefix that appears in the flat parameter
	// list, so a bare parent placeholder is skipped when its children are
	// declared as dotted leaves elsewhere in the same operation detail.
	ancestors := map[string]bool{}
	for _, param := range detail.Parameters {
		if index := strings.LastIndex(param.Name, "."); index >= 0 {
			ancestors[param.Name[:index]] = true
		}
	}

	var leaves []OpenAPIParameter
	var walk func(params []OpenAPIParameter, prefix string)
	walk = func(params []OpenAPIParameter, prefix string) {
		for _, param := range params {
			full := param.Name
			if prefix != "" {
				full = prefix + "." + param.Name
			}
			if len(param.SubParameters) > 0 {
				walk(param.SubParameters, full)
				continue
			}
			if ancestors[full] {
				continue
			}
			leaf := param
			leaf.Name = full
			leaves = append(leaves, leaf)
		}
	}
	walk(detail.Parameters, "")
	return leaves
}
