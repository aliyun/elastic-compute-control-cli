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
// OpenAPIOperationDetailFor supplies the authoritative current parameter set
// and falls back to legacy metadata only when the current operation is absent.
// When the current detail exposes a bare group without dotted children, the
// legacy subtree enriches that group so drift detection retains field-level
// coverage. Legacy-only top-level parameters are never copied into the current
// set, so newly added and removed parameters remain observable.
func OpenAPIOperationLeaves(lang string, product OpenAPIProduct, operation string) ([]OpenAPIParameter, bool) {
	detail, ok := OpenAPIOperationDetailFor(lang, product, operation)
	if !ok {
		return nil, false
	}
	return enrichedOpenAPIOperationLeaves(lang, product, operation, detail), true
}

func enrichedOpenAPIOperationLeaves(lang string, product OpenAPIProduct, operation string, detail OpenAPIOperationDetail) []OpenAPIParameter {
	if legacy, legacyOK := legacyOpenAPIOperationDetail(lang, product, operation); legacyOK {
		detail.Parameters = enrichCurrentParameters(detail.Parameters, legacy.Parameters)
	}
	return flattenOpenAPIParameters(detail)
}

func enrichCurrentParameters(current, legacy []OpenAPIParameter) []OpenAPIParameter {
	legacyByName := make(map[string]OpenAPIParameter, len(legacy))
	for _, param := range legacy {
		legacyByName[param.Name] = param
	}
	currentAncestors := map[string]bool{}
	for _, param := range current {
		name := param.Name
		for index := strings.LastIndex(name, "."); index >= 0; index = strings.LastIndex(name, ".") {
			name = name[:index]
			currentAncestors[name] = true
		}
	}

	out := append([]OpenAPIParameter(nil), current...)
	for index, param := range out {
		if len(param.SubParameters) > 0 || currentAncestors[param.Name] {
			continue
		}
		legacyParam, ok := legacyByName[param.Name]
		if !ok || len(legacyParam.SubParameters) == 0 ||
			(param.Type != "" && legacyParam.Type != "" && !strings.EqualFold(param.Type, legacyParam.Type)) {
			continue
		}
		out[index].SubParameters = legacyParam.SubParameters
	}
	return out
}

func flattenOpenAPIParameters(detail OpenAPIOperationDetail) []OpenAPIParameter {
	// ancestors records every dotted prefix that appears in the flat parameter
	// list, so a bare parent placeholder is skipped when its children are
	// declared as dotted leaves elsewhere in the same operation detail. Every
	// ancestor level is registered so multi-level names such as a.b.c also
	// suppress bare a and a.b placeholders.
	ancestors := map[string]bool{}
	for _, param := range detail.Parameters {
		name := param.Name
		for index := strings.LastIndex(name, "."); index >= 0; index = strings.LastIndex(name, ".") {
			name = name[:index]
			ancestors[name] = true
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
