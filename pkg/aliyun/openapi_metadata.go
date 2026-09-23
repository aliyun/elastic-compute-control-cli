package aliyun

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type OpenAPIProduct struct {
	Code         string
	Name         string
	Version      string
	EndpointType string
	Style        string
	Endpoints    map[string]OpenAPIEndpoint
	APINames     []string

	// Preserve manifest provenance for fail-closed detail resolution.
	currentMetadata bool
	currentAPINames map[string]bool
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
	ParamStyle    string
	Element       *OpenAPIParameter
	Value         *OpenAPIParameter
	SubParameters []OpenAPIParameter
}

func OpenAPIProducts(lang string) ([]OpenAPIProduct, error) {
	return openAPIProductsWithReader(lang, false, readOpenAPINewMetadata, nil)
}

// OpenAPIMetadataResolver is an immutable, fail-closed view of the current
// OpenAPI metadata for a declared product set. Construct it with
// NewOpenAPIMetadataResolver to retain manifest provenance.
type OpenAPIMetadataResolver struct {
	language string
	products map[string]OpenAPIProduct
	read     openAPINewMetadataReader
}

// NewOpenAPIMetadataResolver loads every required product from the current
// catalog. Missing products and broken current version manifests are fatal;
// unrelated incomplete catalog entries are not read.
func NewOpenAPIMetadataResolver(lang string, requiredCodes []string) (*OpenAPIMetadataResolver, error) {
	return newOpenAPIMetadataResolverWithReader(lang, requiredCodes, readOpenAPINewMetadata)
}

func newOpenAPIMetadataResolverWithReader(lang string, requiredCodes []string, read openAPINewMetadataReader) (*OpenAPIMetadataResolver, error) {
	products, err := openAPIProductsWithReader(lang, true, read, requiredCodes)
	if err != nil {
		return nil, err
	}
	index := make(map[string]OpenAPIProduct, len(products))
	for _, product := range products {
		index[strings.ToLower(strings.TrimSpace(product.Code))] = product
	}
	return &OpenAPIMetadataResolver{language: lang, products: index, read: read}, nil
}

// OperationLeaves resolves and flattens one operation from the snapshot.
// The last argument remains for source compatibility; legacy approval cannot
// enable another metadata source or restore an absent canonical operation.
func (r *OpenAPIMetadataResolver) OperationLeaves(productCode, operation string, _ bool) ([]OpenAPIParameter, string, error) {
	if r == nil {
		return nil, "", fmt.Errorf("OpenAPI metadata resolver is nil")
	}
	product, ok := r.products[strings.ToLower(strings.TrimSpace(productCode))]
	if !ok {
		return nil, "", fmt.Errorf("OpenAPI product %q not found", productCode)
	}
	canonical, ok := OpenAPIOperationName(product, operation)
	if !ok {
		return nil, "", fmt.Errorf("OpenAPI operation %s.%s not found", productCode, operation)
	}
	detail, err := openAPIOperationDetailForStrictWithReader(r.language, product, canonical, r.read)
	if err != nil {
		return nil, "", err
	}
	return flattenOpenAPIParameters(detail), canonical, nil
}

func openAPIProductsWithReader(lang string, strict bool, read openAPINewMetadataReader, requiredCodes []string) ([]OpenAPIProduct, error) {
	metadataLang := openAPIMetadataLanguage(lang)
	content, err := read(metadataLang, "/products.json")
	if err != nil {
		return nil, fmt.Errorf("read current OpenAPI product catalog: %w", err)
	}
	var set openAPINewProductSet
	if err := json.Unmarshal(content, &set); err != nil {
		return nil, fmt.Errorf("parse current OpenAPI product catalog: %w", err)
	}
	if set.Products == nil {
		return nil, fmt.Errorf("current OpenAPI product catalog omits products")
	}
	required := map[string]bool{}
	for _, code := range requiredCodes {
		code = strings.ToLower(strings.TrimSpace(code))
		if code != "" {
			required[code] = true
		}
	}
	currentCodes := make(map[string]bool, len(set.Products))
	for _, product := range set.Products {
		currentCodes[strings.ToLower(strings.TrimSpace(product.Code))] = true
	}
	if strict {
		for code := range required {
			if code != ossUtilProductCode && !currentCodes[code] {
				return nil, fmt.Errorf("required OpenAPI product %q is absent from the current catalog", code)
			}
		}
	}
	products := make([]OpenAPIProduct, 0, len(set.Products))
	for _, product := range set.Products {
		if strict && !required[strings.ToLower(strings.TrimSpace(product.Code))] {
			continue
		}
		converted, err := openAPIProductFromNewMetaWithReader(metadataLang, product, read)
		if err != nil {
			return nil, fmt.Errorf("load current OpenAPI product %q: %w", product.Code, err)
		}
		products = append(products, converted)
	}
	products = withoutOpenAPIProduct(products, ossUtilProductCode)
	products = append(products, ossUtilProduct(lang))
	sort.SliceStable(products, func(i, j int) bool {
		return strings.ToLower(products[i].Code) < strings.ToLower(products[j].Code)
	})
	return products, nil
}

func withoutOpenAPIProduct(products []OpenAPIProduct, code string) []OpenAPIProduct {
	out := make([]OpenAPIProduct, 0, len(products))
	for _, product := range products {
		if strings.EqualFold(product.Code, code) {
			continue
		}
		out = append(out, product)
	}
	return out
}

func OpenAPIProductByCode(code string, lang string) (OpenAPIProduct, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	products, err := openAPIProductsWithReader(lang, true, readOpenAPINewMetadata, []string{code})
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
	if strings.EqualFold(strings.TrimSpace(productCode), ossUtilProductCode) {
		return ossUtilOperationSummary(lang, operation)
	}
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
	if strings.EqualFold(strings.TrimSpace(product.Code), ossUtilProductCode) {
		return ossUtilOperationDetail(operation)
	}
	canonical, ok := OpenAPIOperationName(product, operation)
	if !ok {
		return OpenAPIOperationDetail{}, false
	}
	detail, err := openAPIOperationDetailForStrictWithReader(lang, product, canonical, readOpenAPINewMetadata)
	return detail, err == nil
}

func openAPIOperationDetailForStrictWithReader(
	lang string,
	product OpenAPIProduct,
	operation string,
	read openAPINewMetadataReader,
) (OpenAPIOperationDetail, error) {
	if strings.EqualFold(strings.TrimSpace(product.Code), ossUtilProductCode) {
		detail, ok := ossUtilOperationDetail(operation)
		if !ok {
			return OpenAPIOperationDetail{}, fmt.Errorf("OSS operation %q not found", operation)
		}
		return detail, nil
	}

	metadataLang := openAPIMetadataLanguage(lang)
	if !product.currentMetadata || !product.currentAPINames[operation] {
		return OpenAPIOperationDetail{}, fmt.Errorf("operation %s.%s is absent from the current OpenAPI manifest", product.Code, operation)
	}

	detail, err := readOpenAPINewAPIDetailWithReader(metadataLang, product.Code, operation, read)
	if err != nil {
		return OpenAPIOperationDetail{}, fmt.Errorf("read current OpenAPI detail for %s.%s: %w", product.Code, operation, err)
	}
	if detail == nil || strings.TrimSpace(detail.Name) == "" {
		return OpenAPIOperationDetail{}, fmt.Errorf("current OpenAPI detail for %s.%s is empty", product.Code, operation)
	}
	if detail.Name != operation {
		return OpenAPIOperationDetail{}, fmt.Errorf("current OpenAPI detail for %s.%s names operation %q", product.Code, operation, detail.Name)
	}
	if detail.Parameters == nil {
		return OpenAPIOperationDetail{}, fmt.Errorf("current OpenAPI detail for %s.%s omits the parameters array", product.Code, operation)
	}
	return openAPIOperationDetailFromNewMeta(product, detail), nil
}

func OpenAPIOperationName(product OpenAPIProduct, operation string) (string, bool) {
	operation = strings.TrimSpace(operation)
	// Prefer the canonical spelling from the current manifest even when a
	// legacy compatibility name differs only by case. Strict detail loading
	// must not mistake that alias for a legacy-only operation.
	for _, name := range product.APINames {
		if product.currentAPINames[name] && name == operation {
			return name, true
		}
	}
	for _, name := range product.APINames {
		if product.currentAPINames[name] && strings.EqualFold(name, operation) {
			return name, true
		}
	}
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
	// Dotted raw names can coexist with an object of the same prefix.
	for i := range params {
		if params[i].Name == name {
			return &params[i]
		}
	}
	for i := range params {
		param := &params[i]
		if !strings.HasPrefix(name, param.Name+".") {
			continue
		}
		suffix := strings.TrimPrefix(name, param.Name+".")
		if param.Type == "Struct" || param.Type == "Json" {
			if child := findOpenAPIParameter(param.SubParameters, suffix); child != nil {
				return child
			}
			if param.Value != nil {
				name, _, _ := strings.Cut(suffix, ".")
				if name != "" {
					value := *param.Value
					value.Name = name
					if value.ParamStyle == "" {
						value.ParamStyle = param.ParamStyle
					}
					if child := findOpenAPIParameter([]OpenAPIParameter{value}, suffix); child != nil {
						return child
					}
				}
			}
			continue
		}
		// Repeat lists consume exactly one positive index; untyped historical
		// groups keep their existing indexed lookup contract.
		index, rest, _ := strings.Cut(suffix, ".")
		n, err := strconv.Atoi(index)
		if err != nil || n < 1 {
			continue
		}
		if param.Type == "RepeatList" && param.Element != nil {
			item := *param.Element
			item.Name = index
			if item.ParamStyle == "" {
				item.ParamStyle = param.ParamStyle
			}
			if child := findOpenAPIParameter([]OpenAPIParameter{item}, suffix); child != nil {
				return child
			}
			continue
		}
		if len(param.SubParameters) > 0 {
			if child := findOpenAPIParameter(param.SubParameters, rest); child != nil {
				return child
			}
		} else if param.Type == "RepeatList" && rest == "" {
			return param
		}
	}
	return nil
}

func openAPIProductFromNewMetaWithReader(metadataLang string, product openAPINewProduct, read openAPINewMetadataReader) (OpenAPIProduct, error) {
	content, err := read(metadataLang, "/"+strings.ToLower(product.Code)+"/version.json")
	if err != nil {
		return OpenAPIProduct{}, err
	}
	var version openAPINewVersion
	if err := json.Unmarshal(content, &version); err != nil {
		return OpenAPIProduct{}, err
	}
	if version.APIs == nil {
		return OpenAPIProduct{}, fmt.Errorf("current OpenAPI product %q manifest has no APIs map", product.Code)
	}
	names := make([]string, 0, len(version.APIs))
	currentNames := make(map[string]bool, len(version.APIs))
	for name := range version.APIs {
		names = append(names, name)
		currentNames[name] = true
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
		Code:            product.Code,
		Name:            strings.TrimSpace(product.Name),
		Version:         product.Version,
		EndpointType:    product.EndpointType,
		Style:           version.Style,
		Endpoints:       endpoints,
		APINames:        names,
		currentMetadata: true,
		currentAPINames: currentNames,
	}, nil
}

func openAPIOperationDetailFromNewMeta(product OpenAPIProduct, detail *openAPINewDetail) OpenAPIOperationDetail {
	params := make([]OpenAPIParameter, 0, len(detail.Parameters))
	for _, param := range detail.Parameters {
		params = append(params, openAPIParameterFromNewMeta(param))
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

func openAPIParameterFromNewMeta(param openAPINewRequestParameter) OpenAPIParameter {
	out := OpenAPIParameter{
		Name:        param.Name,
		Description: strings.TrimSpace(param.Description),
		Position:    param.Position,
		Type:        param.Type,
		Required:    param.Required,
		ParamStyle:  param.ParamStyle,
	}
	if len(param.SubParameters) > 0 {
		out.SubParameters = make([]OpenAPIParameter, 0, len(param.SubParameters))
		for _, sub := range param.SubParameters {
			out.SubParameters = append(out.SubParameters, openAPIParameterFromNewMeta(sub))
		}
	}
	if param.Element != nil {
		element := openAPIParameterFromNewMeta(*param.Element)
		out.Element = &element
	}
	if param.Value != nil {
		value := openAPIParameterFromNewMeta(*param.Value)
		out.Value = &value
	}
	return out
}

func openAPIMetadataLanguage(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return "zh"
	}
	return "en"
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
