package aliyun

import (
	"encoding/json"
	"sort"
	"strings"

	aliyunopenapimeta "github.com/aliyun/aliyun-openapi-meta"
)

type OpenAPIProduct struct {
	Code         string
	Name         string
	Version      string
	EndpointType string
	Style        string
	Endpoints    map[string]OpenAPIEndpoint
	APINames     []string
}

type OpenAPIEndpoint struct {
	RegionID string
	Name     string
	Public   string
	VPC      string
}

type OpenAPIOperationSummary struct {
	Title      string
	Summary    string
	Deprecated bool
}

type OpenAPIOperationDetail struct {
	Name        string
	Deprecated  bool
	Protocol    string
	Method      string
	PathPattern string
	Style       string
	Parameters  []OpenAPIParameter
}

type OpenAPIParameter struct {
	Name          string
	Description   string
	Position      string
	Type          string
	Required      bool
	SubParameters []OpenAPIParameter
}

func OpenAPIProducts(lang string) ([]OpenAPIProduct, error) {
	metadataLang := openAPIMetadataLanguage(lang)
	legacy := legacyOpenAPIProducts(lang)
	content, err := readOpenAPINewMetadata(metadataLang, "/products.json")
	if err != nil {
		if products := openAPIProductMapValues(legacy); len(products) > 0 {
			return products, nil
		}
		return nil, err
	}
	var set openAPINewProductSet
	if err := json.Unmarshal(content, &set); err != nil {
		return nil, err
	}
	products := make([]OpenAPIProduct, 0, len(set.Products))
	for _, product := range set.Products {
		converted, err := openAPIProductFromNewMeta(metadataLang, product)
		if err != nil {
			continue
		}
		if legacyProduct, ok := legacy[strings.ToLower(converted.Code)]; ok {
			converted.APINames = mergeSortedStrings(converted.APINames, legacyProduct.APINames)
			if converted.Style == "" {
				converted.Style = legacyProduct.Style
			}
		}
		products = append(products, converted)
		delete(legacy, strings.ToLower(converted.Code))
	}
	for _, product := range openAPIProductMapValues(legacy) {
		products = append(products, product)
	}
	return products, nil
}

func OpenAPIProductByCode(code string, lang string) (OpenAPIProduct, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	products, err := OpenAPIProducts(lang)
	if err != nil {
		return OpenAPIProduct{}, false
	}
	for _, product := range products {
		if strings.ToLower(product.Code) == code {
			return product, true
		}
	}
	return OpenAPIProduct{}, false
}

func OpenAPIOperationSummaryFor(lang string, productCode string, operation string) (OpenAPIOperationSummary, bool) {
	api, err := readOpenAPINewAPI(openAPIMetadataLanguage(lang), productCode, operation)
	if err != nil || api == nil {
		return OpenAPIOperationSummary{}, false
	}
	return OpenAPIOperationSummary{
		Title:      api.Title,
		Summary:    api.Summary,
		Deprecated: api.Deprecated,
	}, true
}

func OpenAPIOperationDetailFor(lang string, product OpenAPIProduct, operation string) (OpenAPIOperationDetail, bool) {
	detail, err := readOpenAPINewAPIDetail(openAPIMetadataLanguage(lang), product.Code, operation)
	if err != nil || detail == nil {
		return legacyOpenAPIOperationDetail(lang, product, operation)
	}
	return openAPIOperationDetailFromNewMeta(product, detail), true
}

func OpenAPIOperationName(product OpenAPIProduct, operation string) (string, bool) {
	operation = strings.TrimSpace(operation)
	for _, name := range product.APINames {
		if name == operation {
			return name, true
		}
	}
	for _, name := range product.APINames {
		if strings.EqualFold(name, operation) {
			return name, true
		}
	}
	return "", false
}

func (d *OpenAPIOperationDetail) FindParameter(name string) *OpenAPIParameter {
	if d == nil {
		return nil
	}
	return findOpenAPIParameter(d.Parameters, name)
}

func findOpenAPIParameter(params []OpenAPIParameter, name string) *OpenAPIParameter {
	for i := range params {
		param := &params[i]
		if param.Name == name {
			return param
		}
		if len(param.SubParameters) > 0 && strings.HasPrefix(name, param.Name+".") {
			suffix := name[len(param.Name):]
			if len(suffix) >= 4 && suffix[0] == '.' && strings.Count(suffix, ".") >= 2 {
				index := strings.Index(name[len(param.Name)+1:], ".")
				index += 2
				return findOpenAPIParameter(param.SubParameters, name[len(param.Name)+index:])
			}
			return nil
		}
		if param.Type == "RepeatList" && strings.HasPrefix(name, param.Name+".") {
			return param
		}
	}
	return nil
}

func openAPIProductFromNewMeta(metadataLang string, product openAPINewProduct) (OpenAPIProduct, error) {
	content, err := readOpenAPINewMetadata(metadataLang, "/"+strings.ToLower(product.Code)+"/version.json")
	if err != nil {
		return OpenAPIProduct{}, err
	}
	var version openAPINewVersion
	if err := json.Unmarshal(content, &version); err != nil {
		return OpenAPIProduct{}, err
	}
	names := make([]string, 0, len(version.APIs))
	for name := range version.APIs {
		names = append(names, name)
	}
	sort.Strings(names)
	endpoints := make(map[string]OpenAPIEndpoint, len(product.Endpoints))
	for region, endpoint := range product.Endpoints {
		endpoints[region] = OpenAPIEndpoint{
			RegionID: endpoint.RegionID,
			Name:     endpoint.Name,
			Public:   endpoint.Public,
			VPC:      endpoint.VPC,
		}
	}
	return OpenAPIProduct{
		Code:         product.Code,
		Name:         strings.TrimSpace(product.Name),
		Version:      firstNonEmptyString(product.Version, version.Version),
		EndpointType: product.EndpointType,
		Style:        version.Style,
		Endpoints:    endpoints,
		APINames:     names,
	}, nil
}

func openAPIOperationDetailFromNewMeta(product OpenAPIProduct, detail *openAPINewDetail) OpenAPIOperationDetail {
	params := make([]OpenAPIParameter, 0, len(detail.Parameters))
	for _, param := range detail.Parameters {
		params = append(params, OpenAPIParameter{
			Name:        param.Name,
			Description: strings.TrimSpace(param.Description),
			Position:    param.Position,
			Type:        param.Type,
			Required:    param.Required,
		})
	}
	return OpenAPIOperationDetail{
		Name:        detail.Name,
		Deprecated:  detail.Deprecated,
		Protocol:    detail.Protocol,
		Method:      detail.Method,
		PathPattern: detail.PathPattern,
		Style:       product.Style,
		Parameters:  params,
	}
}

func openAPIMetadataLanguage(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return "zh"
	}
	return "en"
}

type legacyOpenAPIProductSet struct {
	Products []legacyOpenAPIProduct `json:"products"`
}

type legacyOpenAPIProduct struct {
	Code                    string            `json:"code"`
	Version                 string            `json:"version"`
	Name                    map[string]string `json:"name"`
	RegionalEndpoints       map[string]string `json:"regional_endpoints"`
	RegionalVpcEndpoints    map[string]string `json:"regional_vpc_endpoints"`
	GlobalEndpoint          string            `json:"global_endpoint"`
	RegionalEndpointPattern string            `json:"regional_endpoint_patterns"`
	Style                   string            `json:"api_style"`
	APINames                []string          `json:"apis"`
}

type legacyOpenAPIDetail struct {
	Name        string                   `json:"name"`
	Protocol    string                   `json:"protocol"`
	Method      string                   `json:"method"`
	PathPattern string                   `json:"pathPattern"`
	Parameters  []legacyOpenAPIParameter `json:"parameters"`
}

type legacyOpenAPIParameter struct {
	Name          string                   `json:"name"`
	Description   map[string]string        `json:"description,omitempty"`
	Position      string                   `json:"position"`
	Type          string                   `json:"type"`
	Required      bool                     `json:"required"`
	SubParameters []legacyOpenAPIParameter `json:"sub_parameters,omitempty"`
}

func legacyOpenAPIProducts(lang string) map[string]OpenAPIProduct {
	content, err := aliyunopenapimeta.Metadatas.ReadFile("metadatas/products.json")
	if err != nil {
		return map[string]OpenAPIProduct{}
	}
	var set legacyOpenAPIProductSet
	if err := json.Unmarshal(content, &set); err != nil {
		return map[string]OpenAPIProduct{}
	}
	out := make(map[string]OpenAPIProduct, len(set.Products))
	for _, product := range set.Products {
		endpoints := map[string]OpenAPIEndpoint{}
		for region, endpoint := range product.RegionalEndpoints {
			ep := endpoints[region]
			ep.RegionID = region
			ep.Public = endpoint
			endpoints[region] = ep
		}
		for region, endpoint := range product.RegionalVpcEndpoints {
			ep := endpoints[region]
			ep.RegionID = region
			ep.VPC = endpoint
			endpoints[region] = ep
		}
		if product.GlobalEndpoint != "" {
			endpoints[""] = OpenAPIEndpoint{Public: product.GlobalEndpoint}
		}
		names := append([]string(nil), product.APINames...)
		sort.Strings(names)
		out[strings.ToLower(product.Code)] = OpenAPIProduct{
			Code:      product.Code,
			Name:      localizedOpenAPIText(product.Name, lang),
			Version:   product.Version,
			Style:     product.Style,
			Endpoints: endpoints,
			APINames:  names,
		}
	}
	return out
}

func legacyOpenAPIOperationDetail(lang string, product OpenAPIProduct, operation string) (OpenAPIOperationDetail, bool) {
	content, err := aliyunopenapimeta.Metadatas.ReadFile("metadatas/" + strings.ToLower(product.Code) + "/" + operation + ".json")
	if err != nil {
		return OpenAPIOperationDetail{}, false
	}
	var detail legacyOpenAPIDetail
	if err := json.Unmarshal(content, &detail); err != nil {
		return OpenAPIOperationDetail{}, false
	}
	params := make([]OpenAPIParameter, 0, len(detail.Parameters))
	for _, param := range detail.Parameters {
		params = append(params, openAPIParameterFromLegacy(param, lang))
	}
	return OpenAPIOperationDetail{
		Name:        detail.Name,
		Protocol:    detail.Protocol,
		Method:      detail.Method,
		PathPattern: detail.PathPattern,
		Style:       product.Style,
		Parameters:  params,
	}, true
}

func openAPIParameterFromLegacy(param legacyOpenAPIParameter, lang string) OpenAPIParameter {
	out := OpenAPIParameter{
		Name:        param.Name,
		Description: localizedOpenAPIText(param.Description, lang),
		Position:    param.Position,
		Type:        param.Type,
		Required:    param.Required,
	}
	if len(param.SubParameters) > 0 {
		out.SubParameters = make([]OpenAPIParameter, 0, len(param.SubParameters))
		for _, sub := range param.SubParameters {
			out.SubParameters = append(out.SubParameters, openAPIParameterFromLegacy(sub, lang))
		}
	}
	return out
}

func localizedOpenAPIText(values map[string]string, lang string) string {
	if len(values) == 0 {
		return ""
	}
	keys := []string{"en", "zh", "zh-CN", "zh-Hans"}
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		keys = []string{"zh", "zh-CN", "zh-Hans", "en"}
	}
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func openAPIProductMapValues(products map[string]OpenAPIProduct) []OpenAPIProduct {
	out := make([]OpenAPIProduct, 0, len(products))
	for _, product := range products {
		out = append(out, product)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Code) < strings.ToLower(out[j].Code)
	})
	return out
}

func mergeSortedStrings(primary []string, secondary []string) []string {
	seen := make(map[string]bool, len(primary)+len(secondary))
	out := make([]string, 0, len(primary)+len(secondary))
	for _, values := range [][]string{primary, secondary} {
		for _, value := range values {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
