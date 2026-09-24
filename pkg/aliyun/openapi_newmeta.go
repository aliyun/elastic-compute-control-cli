package aliyun

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/aliyun/elastic-compute-control-cli/internal/openapimeta"
)

// These normalized types are the boundary between the canonical snapshot and
// the existing metadata consumers. They are not another metadata source.
type openAPINewProductSet struct {
	Products []openAPINewProduct `json:"products"`
}

type openAPINewProduct struct {
	Code         string                        `json:"code"`
	Name         string                        `json:"name"`
	Version      string                        `json:"version"`
	EndpointType string                        `json:"endpointType"`
	Endpoints    map[string]openAPINewEndpoint `json:"endpoints"`
}

type openAPINewEndpoint struct {
	RegionID string `json:"regionId"`
	Name     string `json:"regionName"`
	Public   string `json:"public"`
	VPC      string `json:"vpc"`
}

type openAPINewVersion struct {
	Version string                   `json:"version"`
	Style   string                   `json:"style"`
	APIs    map[string]openAPINewAPI `json:"apis"`
}

type openAPINewAPI struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Deprecated bool   `json:"deprecated"`
}

type openAPINewDetail struct {
	Name        string                       `json:"name"`
	Deprecated  bool                         `json:"deprecated"`
	Protocol    string                       `json:"protocol"`
	Method      string                       `json:"method"`
	PathPattern string                       `json:"pathPattern"`
	Parameters  []openAPINewRequestParameter `json:"parameters"`
}

type openAPINewRequestParameter struct {
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	Position      string                       `json:"position"`
	Type          string                       `json:"type"`
	Required      bool                         `json:"required"`
	ParamStyle    string                       `json:"param_style,omitempty"`
	Element       *openAPINewRequestParameter  `json:"element,omitempty"`
	Value         *openAPINewRequestParameter  `json:"value,omitempty"`
	SubParameters []openAPINewRequestParameter `json:"sub_parameters,omitempty"`
}

type canonicalOpenAPIProduct struct {
	Code                 string            `json:"code"`
	Name                 map[string]string `json:"name"`
	DefaultVersion       string            `json:"plugin_default_version"`
	Style                string            `json:"api_style"`
	RegionalEndpoints    map[string]string `json:"regional_endpoints"`
	RegionalVPCEndpoints map[string]string `json:"regional_vpc_endpoints"`
	GlobalEndpoint       string            `json:"global_endpoint"`
}

type canonicalOpenAPIParameter struct {
	RawName    string                      `json:"raw_name"`
	HelpEN     string                      `json:"help_en"`
	HelpZH     string                      `json:"help_zh"`
	Location   string                      `json:"location"`
	Type       string                      `json:"type"`
	Required   bool                        `json:"required"`
	ParamStyle string                      `json:"param_style"`
	DirectBody bool                        `json:"direct_body"`
	Fields     []canonicalOpenAPIParameter `json:"fields"`
	Element    *canonicalOpenAPIParameter  `json:"element"`
	Value      *canonicalOpenAPIParameter  `json:"value"`
}

type openAPINewMetadataReader func(language string, path string) ([]byte, error)

func readOpenAPINewAPI(language string, code string, name string) (*openAPINewAPI, error) {
	content, err := readOpenAPINewMetadata(language, "/"+strings.ToLower(code)+"/version.json")
	if err != nil {
		return nil, err
	}
	var version openAPINewVersion
	if err := json.Unmarshal(content, &version); err != nil {
		return nil, err
	}
	api, ok := version.APIs[name]
	if !ok {
		return nil, nil
	}
	return &api, nil
}

func readOpenAPINewAPIDetailWithReader(language string, code string, name string, read openAPINewMetadataReader) (*openAPINewDetail, error) {
	content, err := read(language, "/"+strings.ToLower(code)+"/"+name+".json")
	if err != nil {
		return nil, err
	}
	var detail openAPINewDetail
	if err := json.Unmarshal(content, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// The embedded catalog is immutable. Cache its decoding, not mutable public
// products, so a caller cannot change the version used by subsequent reads.
var canonicalOpenAPIProducts = sync.OnceValues(func() ([]canonicalOpenAPIProduct, error) {
	content, err := openapimeta.ReadFile("metadatas/products.json")
	if err != nil {
		return nil, err
	}
	var set struct {
		Products []canonicalOpenAPIProduct `json:"products"`
	}
	if err := json.Unmarshal(content, &set); err != nil {
		return nil, err
	}
	if set.Products == nil {
		return nil, fmt.Errorf("current OpenAPI catalog omits products")
	}
	return set.Products, nil
})

func readOpenAPINewMetadata(language string, path string) ([]byte, error) {
	products, err := canonicalOpenAPIProducts()
	if err != nil {
		return nil, err
	}
	return readCanonicalOpenAPIMetadata(language, path, products, openapimeta.ReadFile)
}

func readCanonicalOpenAPIMetadata(language, path string, products []canonicalOpenAPIProduct, read func(string) ([]byte, error)) ([]byte, error) {
	if path == "/products.json" {
		set := openAPINewProductSet{Products: make([]openAPINewProduct, 0, len(products))}
		for _, product := range products {
			endpoints := map[string]openAPINewEndpoint{}
			for region, endpoint := range product.RegionalEndpoints {
				endpoints[region] = openAPINewEndpoint{RegionID: region, Public: endpoint}
			}
			for region, endpoint := range product.RegionalVPCEndpoints {
				ep := endpoints[region]
				ep.RegionID, ep.VPC = region, endpoint
				endpoints[region] = ep
			}
			endpointType := "regional"
			if product.GlobalEndpoint != "" {
				endpoints[""] = openAPINewEndpoint{Public: product.GlobalEndpoint}
				endpointType = "global"
			}
			set.Products = append(set.Products, openAPINewProduct{
				Code: product.Code, Name: localizedOpenAPIText(product.Name, language),
				Version: product.DefaultVersion, EndpointType: endpointType, Endpoints: endpoints,
			})
		}
		return json.Marshal(set)
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid OpenAPI metadata path %q", path)
	}
	for _, product := range products {
		if !strings.EqualFold(product.Code, parts[0]) {
			continue
		}
		if product.DefaultVersion == "" {
			return nil, fmt.Errorf("OpenAPI product %q omits plugin_default_version", product.Code)
		}
		content, err := read("canonical/" + strings.ToLower(product.Code) + "/" + product.DefaultVersion + "/" + parts[1])
		if err != nil {
			return nil, err
		}
		if parts[1] == "version.json" {
			var version struct {
				Version string `json:"version"`
				Style   string `json:"style"`
				APIs    map[string]struct {
					DescriptionEN string `json:"description_en"`
					DescriptionZH string `json:"description_zh"`
					Deprecated    bool   `json:"deprecated"`
				} `json:"apis"`
			}
			if err := json.Unmarshal(content, &version); err != nil {
				return nil, err
			}
			if version.APIs == nil || version.Version != product.DefaultVersion {
				return nil, fmt.Errorf("invalid current OpenAPI manifest for %s/%s", product.Code, product.DefaultVersion)
			}
			out := openAPINewVersion{Version: product.DefaultVersion, Style: version.Style, APIs: make(map[string]openAPINewAPI, len(version.APIs))}
			for name, api := range version.APIs {
				out.APIs[name] = openAPINewAPI{Title: name, Summary: localizedOpenAPIText(map[string]string{"en": api.DescriptionEN, "zh": api.DescriptionZH}, language), Deprecated: api.Deprecated}
			}
			return json.Marshal(out)
		}
		var detail struct {
			Name        string                      `json:"name"`
			Deprecated  bool                        `json:"deprecated"`
			Protocol    string                      `json:"protocol"`
			Method      string                      `json:"method"`
			PathPattern string                      `json:"pathPattern"`
			Parameters  []canonicalOpenAPIParameter `json:"parameters"`
		}
		if err := json.Unmarshal(content, &detail); err != nil {
			return nil, err
		}
		out := openAPINewDetail{Name: detail.Name, Deprecated: detail.Deprecated, Protocol: detail.Protocol, Method: detail.Method, PathPattern: detail.PathPattern}
		if detail.Parameters != nil {
			out.Parameters = make([]openAPINewRequestParameter, 0, len(detail.Parameters))
		}
		var bodyFields []openAPINewRequestParameter
		for _, param := range detail.Parameters {
			converted := normalizeCanonicalOpenAPIParameter(language, param, "Query")
			if openAPIStyle(product.Style) == "ROA" && converted.Position == "Body" && !param.DirectBody {
				bodyFields = append(bodyFields, converted)
			} else {
				out.Parameters = append(out.Parameters, converted)
			}
		}
		// Canonical body parameters are promoted object members, except for an
		// explicitly marked whole body (including arrays and primitive bodies).
		if len(bodyFields) > 0 {
			out.Parameters = append(out.Parameters, openAPINewRequestParameter{Name: "body", Position: "Body", Type: "Struct", SubParameters: bodyFields})
		}
		// Some operations omit path fields after promoting a body field of
		// the same name (for example CS CreateTrigger). Recover only what the
		// current path template declares, never from a historical snapshot.
		if detail.Parameters != nil {
			for _, match := range canonicalPathPlaceholder.FindAllStringSubmatch(detail.PathPattern, -1) {
				name := firstNonEmptyString(match[1], match[2])
				found := false
				for _, param := range out.Parameters {
					if param.Name == name {
						if param.Position != "Path" {
							return nil, fmt.Errorf("OpenAPI path parameter %s.%s.%s conflicts with %s", product.Code, detail.Name, name, param.Position)
						}
						found = true
					}
				}
				if !found {
					out.Parameters = append(out.Parameters, openAPINewRequestParameter{Name: name, Position: "Path", Type: "String", Required: true})
				}
			}
		}
		return json.Marshal(out)
	}
	return nil, fmt.Errorf("OpenAPI product %q not found", parts[0])
}

var canonicalPathPlaceholder = regexp.MustCompile(`\{([^{}]+)\}|\[([^\[\]]+)\]`)

func normalizeCanonicalOpenAPIParameter(language string, param canonicalOpenAPIParameter, parentPosition string) openAPINewRequestParameter {
	position := parentPosition
	if param.Location != "" {
		position = map[string]string{"query": "Query", "formData": "FormData", "body": "Body", "path": "Path", "header": "Header", "host": "Domain"}[param.Location]
		if position == "" {
			position = param.Location // Preserve unsupported locations for validation.
		}
	}
	typeName := map[string]string{"string": "String", "int": "Integer", "float": "Float", "bool": "Boolean", "object": "Struct", "array": "RepeatList", "map": "Json", "any": "Json"}[param.Type]
	if typeName == "" {
		typeName = param.Type
	}
	if param.Type == "map" && (param.ParamStyle == "flat" || param.ParamStyle == "repeatList") {
		typeName = "Struct"
	}
	if param.ParamStyle == "json" && (position == "Query" || position == "FormData") {
		typeName = "Json"
	}
	out := openAPINewRequestParameter{
		Name: param.RawName, Position: position, Type: typeName, Required: param.Required, ParamStyle: param.ParamStyle,
		Description: localizedOpenAPIText(map[string]string{"en": param.HelpEN, "zh": param.HelpZH}, language),
	}
	normalizeChild := func(child canonicalOpenAPIParameter) openAPINewRequestParameter {
		if child.ParamStyle == "" {
			child.ParamStyle = param.ParamStyle
		}
		return normalizeCanonicalOpenAPIParameter(language, child, position)
	}
	for _, field := range param.Fields {
		out.SubParameters = append(out.SubParameters, normalizeChild(field))
	}
	if param.Element != nil {
		element := normalizeChild(*param.Element)
		out.Element = &element
		// Keep the field projection used by operation-leaf consumers, but
		// retain the complete element schema for request serialization.
		if param.Fields == nil && param.Element.Fields != nil {
			out.SubParameters = element.SubParameters
		}
	}
	if param.Value != nil {
		value := normalizeChild(*param.Value)
		out.Value = &value
	}
	return out
}
