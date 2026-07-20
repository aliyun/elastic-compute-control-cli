package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/output"
)

type APICallerFactory func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error)

type apiCallerFactoryKey struct{}

type apiCallerWithArgs interface {
	CallWithArgs(ctx context.Context, operation string, request map[string]any, passthrough []string) (map[string]any, error)
}

type apiListOptions struct {
	filter string
	limit  int
}

type apiProductItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

type apiProductList struct {
	Count     int              `json:"count"`
	Total     int              `json:"total"`
	Truncated bool             `json:"truncated"`
	Filter    string           `json:"filter,omitempty"`
	Limit     int              `json:"limit,omitempty"`
	Products  []apiProductItem `json:"products"`
}

type apiOperationItem struct {
	Name    string `json:"name"`
	Product string `json:"product,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type apiOperationList struct {
	Product          string             `json:"product"`
	CanonicalProduct string             `json:"canonical_product,omitempty"`
	Products         []string           `json:"products,omitempty"`
	Aliases          []string           `json:"aliases,omitempty"`
	Count            int                `json:"count"`
	Total            int                `json:"total"`
	Truncated        bool               `json:"truncated"`
	Filter           string             `json:"filter,omitempty"`
	Limit            int                `json:"limit,omitempty"`
	APIs             []apiOperationItem `json:"apis"`
}

type apiOperationSchema struct {
	Product    string               `json:"product"`
	Operation  string               `json:"operation"`
	Deprecated bool                 `json:"deprecated"`
	Title      string               `json:"title,omitempty"`
	Summary    string               `json:"summary,omitempty"`
	Parameters []apiParameterSchema `json:"parameters"`
}

type apiParameterSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required"`
	Position    string `json:"position,omitempty"`
	Description string `json:"description,omitempty"`
}

type apiProductSelection struct {
	Requested string
	Products  []aliyun.OpenAPIProduct
	Alias     bool
}

func apiProductAliasCodes(productCode string) []string {
	switch strings.ToLower(strings.TrimSpace(productCode)) {
	case "ack":
		return []string{"cs"}
	case "lingjun":
		return []string{"eflo", "eflo-cnp", "eflo-controller"}
	default:
		return nil
	}
}

func apiProductAliases(productCode string) []string {
	switch strings.ToLower(productCode) {
	case "cs":
		return []string{"ack"}
	case "eflo", "eflo-cnp", "eflo-controller":
		return []string{"lingjun"}
	default:
		return nil
	}
}

func WithAPICallerFactory(ctx context.Context, factory APICallerFactory) context.Context {
	return context.WithValue(ctx, apiCallerFactoryKey{}, factory)
}

func apiCallerFactoryFromContext(ctx context.Context) APICallerFactory {
	if factory, ok := ctx.Value(apiCallerFactoryKey{}).(APICallerFactory); ok {
		return factory
	}
	return defaultAPICallerFactory
}

func defaultAPICallerFactory(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
	return aliyun.NewOpenAPICaller(profileName, configPath, product, region, getenv)
}

func newAPICommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	var list bool
	var schema bool
	var generateRequest bool
	var filter string
	var limit int
	var requestText string
	var requestParams []string
	var aliyunArgs []string
	cmd := &cobra.Command{
		Use:   "call [--list [product] | --schema <product> <operation> | <product> <operation> [OpenAPI parameters]]",
		Short: "Call Alibaba Cloud OpenAPI operations",
		Long: `Call Alibaba Cloud OpenAPI operations.

Usage forms:
  ecctl call --list [--filter <keyword>] [--limit <n>]
  ecctl call --list <product> [--filter <keyword>] [--limit <n>]
  ecctl call --schema <product> <operation> [--generate-request]
  ecctl call <product> <operation> [OpenAPI parameters] [flags]

OpenAPI parameters may be passed as --Parameter value or --Parameter=value.
Use --request for a JSON object or @file when structured input is clearer.`,
		Example: `  ecctl call --list
  ecctl call --list --filter ecs
  ecctl call --list ecs
  ecctl call --list ecs --filter Instance --limit 20
  ecctl call --schema ecs DescribeInstances
  ecctl call --schema ecs DescribeInstances --generate-request
  ecctl call ecs DescribeInstances --region cn-hangzhou --request '{"PageSize":10}'
  ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10
  ecctl call cs InstallClusterAddons --request @install-addons.json`,
		Args: cobra.ArbitraryArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			options.output = output.ModeJSON
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			listOptions := apiListOptions{filter: filter, limit: limit}
			if generateRequest && !schema {
				return ecerrors.Client("InvalidParameter", "--generate-request requires --schema",
					ecerrors.WithField("generate-request"),
					ecerrors.WithSuggestion(apiMessage(options.lang, "SuggestionCallGenerateRequestWithSchema", nil)),
				)
			}
			if schema {
				if list {
					return ecerrors.Client("InvalidParameter", "call --schema cannot be combined with --list",
						ecerrors.WithField("schema"),
					)
				}
				if len(args) != 2 {
					return ecerrors.Client("MissingOperation", "call --schema requires product and operation",
						ecerrors.WithSuggestion(apiMessage(options.lang, "SuggestionCallSchemaProductOperation", nil)),
					)
				}
				return writeAPIOperationSchema(options, stdout, args[0], args[1], generateRequest)
			}
			if list {
				if err := validateAPIListOptions(cmd, listOptions); err != nil {
					return err
				}
				if len(args) == 0 {
					return writeCommandOutput(options, stdout, apiProducts(options.lang, listOptions))
				}
				if len(args) == 1 {
					return writeAPIProductOperations(options, stdout, args[0], listOptions)
				}
				return ecerrors.Client("InvalidParameter", "call --list accepts at most one product",
					ecerrors.WithSuggestion(apiMessage(options.lang, "SuggestionCallUseListForms", nil)),
				)
			}
			if len(args) != 2 {
				product := ""
				if len(args) > 0 {
					product = args[0]
				}
				return missingAPIOperationError(product, options.lang)
			}
			return runAPICall(cmd, options, stdout, strings.ToLower(args[0]), args[1], requestText, requestParams, aliyunArgs)
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completeAPICallArgs(options.lang, list, args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list supported products or APIs")
	cmd.Flags().BoolVar(&schema, "schema", false, "inspect an OpenAPI operation schema")
	cmd.Flags().BoolVar(&generateRequest, "generate-request", false, "generate a request JSON template from --schema")
	cmd.Flags().StringVar(&filter, "filter", "", "filter products or APIs by keyword")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum products or APIs to return")
	cmd.Flags().StringVar(&requestText, "request", "{}", "request JSON object or @file containing a JSON object")
	cmd.Flags().StringArrayVar(&requestParams, "api-param", nil, "OpenAPI request parameter key=value")
	cmd.Flags().StringArrayVar(&aliyunArgs, "aliyun-arg", nil, "aliyun CLI argument")
	_ = cmd.Flags().MarkHidden("api-param")
	_ = cmd.Flags().MarkHidden("aliyun-arg")
	return cmd
}

func completeAPICallArgs(lang string, list bool, args []string, toComplete string) []string {
	if list {
		if len(args) == 0 {
			return apiProductCompletions(lang, toComplete)
		}
		return nil
	}
	if len(args) == 0 {
		return apiProductCompletions(lang, toComplete)
	}
	if len(args) == 1 {
		return apiOperationCompletions(lang, args[0], toComplete)
	}
	return nil
}

func apiProductCompletions(lang string, prefix string) []string {
	products := apiProducts(lang, apiListOptions{}).Products
	completions := make([]string, 0, len(products))
	for _, product := range products {
		if matchesAPICompletionPrefix(prefix, product.Name) {
			completions = append(completions, product.Name)
		}
		for _, alias := range product.Aliases {
			if matchesAPICompletionPrefix(prefix, alias) {
				completions = append(completions, alias)
			}
		}
	}
	sort.Strings(completions)
	return completions
}

func apiOperationCompletions(lang string, productCode string, prefix string) []string {
	selection, ok := apiProductSelectionByCode(productCode, lang)
	if !ok {
		return nil
	}
	operations := apiProductSelectionOperations(selection, lang, apiListOptions{}).APIs
	completions := make([]string, 0, len(operations))
	for _, operation := range operations {
		if matchesAPICompletionPrefix(prefix, operation.Name) {
			completions = append(completions, operation.Name)
		}
	}
	return completions
}

func matchesAPICompletionPrefix(prefix string, value string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(value), prefix)
}

func validateAPIListOptions(cmd *cobra.Command, options apiListOptions) error {
	if cmd.Flags().Changed("limit") && options.limit <= 0 {
		return ecerrors.Client("InvalidParameter", "limit must be greater than zero", ecerrors.WithField("limit"))
	}
	return nil
}

func apiProducts(lang string, options apiListOptions) apiProductList {
	products, _ := aliyun.OpenAPIProducts(lang)
	items := make([]apiProductItem, 0, len(products))
	for _, product := range products {
		if !apiProductHasCallableOperations(product, lang) {
			continue
		}
		aliases := apiProductAliases(product.Code)
		item := apiProductItem{
			Name:        strings.ToLower(product.Code),
			Description: apiProductDescription(product, lang),
			Aliases:     aliases,
		}
		if !matchesAPIFilter(options.filter, append([]string{item.Name, item.Description}, aliases...)...) {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	total := len(items)
	items = limitAPIItems(items, options.limit)
	list := apiProductList{
		Count:     len(items),
		Total:     total,
		Truncated: isAPIListTruncated(total, options.limit),
		Products:  items,
	}
	applyAPIListMetadata(&list.Filter, &list.Limit, options)
	return list
}

func writeAPIProductOperations(options *globalOptions, stdout io.Writer, productCode string, listOptions apiListOptions) error {
	selection, ok := apiProductSelectionByCode(productCode, options.lang)
	if !ok {
		return ecerrors.Client("UnknownProduct", "call product is not supported",
			ecerrors.WithSuggestion(apiMessage(options.lang, "SuggestionCallListProducts", nil)),
		)
	}
	return writeCommandOutput(options, stdout, apiProductSelectionOperations(selection, options.lang, listOptions))
}

func apiProductOperations(product aliyun.OpenAPIProduct, lang string, options apiListOptions) apiOperationList {
	operations := apiProductOperationItems(product, lang, options, false)
	total := len(operations)
	operations = limitAPIItems(operations, options.limit)
	list := apiOperationList{
		Product:   strings.ToLower(product.Code),
		Count:     len(operations),
		Total:     total,
		Truncated: isAPIListTruncated(total, options.limit),
		APIs:      operations,
	}
	applyAPIListMetadata(&list.Filter, &list.Limit, options)
	return list
}

func apiProductSelectionOperations(selection apiProductSelection, lang string, options apiListOptions) apiOperationList {
	if len(selection.Products) == 1 {
		product := selection.Products[0]
		list := apiProductOperations(product, lang, options)
		if selection.Alias {
			list.Product = selection.Requested
			list.CanonicalProduct = strings.ToLower(product.Code)
			list.Aliases = []string{selection.Requested}
		}
		return list
	}
	operations := make([]apiOperationItem, 0)
	for _, product := range selection.Products {
		operations = append(operations, apiProductOperationItems(product, lang, options, true)...)
	}
	sort.SliceStable(operations, func(i, j int) bool {
		return apiOperationItemsLess(operations[i], operations[j], options.filter)
	})
	total := len(operations)
	operations = limitAPIItems(operations, options.limit)
	list := apiOperationList{
		Product:   selection.Requested,
		Products:  apiSelectionProductCodes(selection),
		Aliases:   []string{selection.Requested},
		Count:     len(operations),
		Total:     total,
		Truncated: isAPIListTruncated(total, options.limit),
		APIs:      operations,
	}
	applyAPIListMetadata(&list.Filter, &list.Limit, options)
	return list
}

func apiProductOperationItems(product aliyun.OpenAPIProduct, lang string, options apiListOptions, includeProduct bool) []apiOperationItem {
	operations := make([]apiOperationItem, 0, len(product.APINames))
	for _, name := range product.APINames {
		api, ok := aliyun.OpenAPIOperationSummaryFor(lang, product.Code, name)
		if ok {
			if api.Deprecated {
				continue
			}
			if !matchesAPIFilter(options.filter, name, api.Summary) {
				continue
			}
			operations = append(operations, apiOperationItem{Name: name, Product: apiOperationItemProduct(product.Code, includeProduct), Summary: api.Summary})
			continue
		}
		if !matchesAPIFilter(options.filter, name) {
			continue
		}
		operations = append(operations, apiOperationItem{Name: name, Product: apiOperationItemProduct(product.Code, includeProduct)})
	}
	sort.SliceStable(operations, func(i, j int) bool {
		return apiOperationItemsLess(operations[i], operations[j], options.filter)
	})
	return operations
}

func apiProductHasCallableOperations(product aliyun.OpenAPIProduct, lang string) bool {
	for _, name := range product.APINames {
		api, ok := aliyun.OpenAPIOperationSummaryFor(lang, product.Code, name)
		if ok && api.Deprecated {
			continue
		}
		return true
	}
	return false
}

func apiOperationItemProduct(productCode string, include bool) string {
	if !include {
		return ""
	}
	return strings.ToLower(productCode)
}

func apiOperationItemsLess(left, right apiOperationItem, filter string) bool {
	leftRank := apiOperationFilterRank(left, filter)
	rightRank := apiOperationFilterRank(right, filter)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.Product < right.Product
}

func apiSelectionProductCodes(selection apiProductSelection) []string {
	codes := make([]string, 0, len(selection.Products))
	for _, product := range selection.Products {
		codes = append(codes, strings.ToLower(product.Code))
	}
	return codes
}

func apiProductByCode(productCode string, lang string) (aliyun.OpenAPIProduct, bool) {
	return aliyun.OpenAPIProductByCode(productCode, lang)
}

func apiProductSelectionByCode(productCode string, lang string) (apiProductSelection, bool) {
	requested := strings.ToLower(strings.TrimSpace(productCode))
	if requested == "" {
		return apiProductSelection{}, false
	}
	if aliasCodes := apiProductAliasCodes(requested); len(aliasCodes) > 0 {
		products := make([]aliyun.OpenAPIProduct, 0, len(aliasCodes))
		for _, aliasCode := range aliasCodes {
			product, ok := apiProductByCode(aliasCode, lang)
			if !ok || !apiProductHasCallableOperations(product, lang) {
				continue
			}
			products = append(products, product)
		}
		if len(products) == 0 {
			return apiProductSelection{}, false
		}
		return apiProductSelection{Requested: requested, Products: products, Alias: true}, true
	}
	product, ok := apiProductByCode(requested, lang)
	if !ok {
		return apiProductSelection{}, false
	}
	if !apiProductHasCallableOperations(product, lang) {
		return apiProductSelection{}, false
	}
	return apiProductSelection{Requested: strings.ToLower(product.Code), Products: []aliyun.OpenAPIProduct{product}}, true
}

func apiProductDescription(product aliyun.OpenAPIProduct, lang string) string {
	if strings.TrimSpace(product.Name) != "" {
		return strings.TrimSpace(product.Name)
	}
	return product.Code
}

func localizedAPIText(values map[string]string, lang string) string {
	for _, key := range apiTextKeys(lang) {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func apiTextKeys(lang string) []string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return []string{"zh", "zh-CN", "zh-Hans", "en"}
	}
	return []string{"en", "zh", "zh-CN", "zh-Hans"}
}

func apiProductAPIsSuggestion(lang string, product string) string {
	return apiMessage(lang, "SuggestionCallListProductAPIs", map[string]any{"Product": strings.ToLower(product)})
}

func apiMessage(lang string, id string, data map[string]any) string {
	message := i18n.NewLocalizer(lang).MessageData(id, data)
	if message == id {
		return ""
	}
	return message
}

func matchesAPIFilter(filter string, values ...string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), filter) {
			return true
		}
	}
	return false
}

func apiOperationFilterRank(operation apiOperationItem, filter string) int {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return 2
	}
	name := strings.ToLower(operation.Name)
	switch {
	case name == filter:
		return 0
	case strings.HasPrefix(name, filter):
		return 1
	default:
		return 2
	}
}

func limitAPIItems[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func isAPIListTruncated(total, limit int) bool {
	return limit > 0 && total > limit
}

func applyAPIListMetadata(filterOut *string, limitOut *int, options apiListOptions) {
	if filter := strings.TrimSpace(options.filter); filter != "" {
		*filterOut = filter
	}
	if options.limit > 0 {
		*limitOut = options.limit
	}
}

func writeAPIOperationSchema(options *globalOptions, stdout io.Writer, productCode, operation string, generateRequest bool) error {
	schema, err := apiOperationSchemaFor(options.lang, productCode, operation)
	if err != nil {
		return err
	}
	if generateRequest {
		return writeCommandOutput(options, stdout, apiRequestTemplate(schema.Parameters))
	}
	return writeCommandOutput(options, stdout, schema)
}

func apiOperationSchemaFor(lang string, productCode, operation string) (apiOperationSchema, error) {
	product, operationName, err := resolveAPIOperation(productCode, operation, lang)
	if err != nil {
		return apiOperationSchema{}, err
	}
	api, hasSummary := aliyun.OpenAPIOperationSummaryFor(lang, product.Code, operationName)
	detail, hasDetail := aliyun.OpenAPIOperationDetailFor(lang, product, operationName)
	if !hasSummary && !hasDetail {
		return apiOperationSchema{}, ecerrors.Client("UnknownOperation", "call operation metadata is not available",
			ecerrors.WithField("operation"),
			ecerrors.WithSuggestion(apiProductAPIsSuggestion(lang, product.Code)),
		)
	}
	out := apiOperationSchema{
		Product:    strings.ToLower(product.Code),
		Operation:  operationName,
		Parameters: apiParameterSchemas(detail),
	}
	if hasSummary {
		out.Title = api.Title
		out.Summary = api.Summary
		out.Deprecated = api.Deprecated
	}
	if hasDetail && detail.Deprecated {
		out.Deprecated = true
	}
	return out, nil
}

func apiOperationName(product aliyun.OpenAPIProduct, operation string) (string, bool) {
	return aliyun.OpenAPIOperationName(product, operation)
}

func apiParameterSchemas(detail aliyun.OpenAPIOperationDetail) []apiParameterSchema {
	parameters := make([]apiParameterSchema, 0, len(detail.Parameters))
	for _, parameter := range detail.Parameters {
		parameters = append(parameters, apiParameterSchema{
			Name:        parameter.Name,
			Type:        parameter.Type,
			Required:    parameter.Required,
			Position:    parameter.Position,
			Description: strings.TrimSpace(parameter.Description),
		})
	}
	return parameters
}

func apiRequestTemplate(parameters []apiParameterSchema) map[string]any {
	request := map[string]any{}
	for _, parameter := range parameters {
		if !parameter.Required {
			continue
		}
		request[parameter.Name] = apiParameterPlaceholder(parameter)
	}
	return request
}

func apiParameterPlaceholder(parameter apiParameterSchema) any {
	switch strings.ToLower(parameter.Type) {
	case "boolean", "bool":
		return false
	case "integer", "int", "int32", "int64", "long", "number":
		return 0
	case "float", "double":
		return 0.0
	case "array", "list", "repeatlist":
		return []any{"<" + parameter.Name + ">"}
	case "object", "map", "struct":
		return map[string]any{}
	default:
		return "<" + parameter.Name + ">"
	}
}

func missingAPIOperationError(product string, lang string) error {
	suggestion := apiProductAPIsSuggestion(lang, "<product>")
	if product != "" {
		suggestion = apiProductAPIsSuggestion(lang, product)
	}
	return ecerrors.Client("MissingOperation", "call operation is required", ecerrors.WithSuggestion(suggestion))
}

func runAPICall(cmd *cobra.Command, options *globalOptions, stdout io.Writer, product, operation, requestText string, requestParams []string, aliyunArgs []string) error {
	resolvedProduct, resolvedOperation, err := resolveRunnableAPIOperation(product, operation, options.lang)
	if err != nil {
		return err
	}
	request, err := parseAPIRequest(requestText)
	if err != nil {
		return err
	}
	if err := mergeAPIRequestParams(request, requestParams); err != nil {
		return err
	}
	region, err := resolveAPIRegion(options, request, aliyunArgs)
	if err != nil {
		return err
	}
	callerFactory := apiCallerFactoryFromContext(cmd.Context())
	caller, err := callerFactory(config.ProfileName(options.profile, os.Getenv), config.ConfigPath(os.Getenv), resolvedProduct, region, os.Getenv)
	if err != nil {
		return err
	}
	var response map[string]any
	if withArgs, ok := caller.(apiCallerWithArgs); ok {
		response, err = withArgs.CallWithArgs(cmd.Context(), resolvedOperation, request, aliyunArgs)
	} else {
		if len(aliyunArgs) > 0 {
			return ecerrors.Client("UnsupportedParameter", "--aliyun-arg is not supported by the built-in OpenAPI caller", ecerrors.WithField("aliyun-arg"))
		}
		response, err = caller.Call(cmd.Context(), resolvedOperation, request)
	}
	if err != nil {
		return err
	}
	return writeCommandOutput(options, stdout, apiCallPayload(resolvedProduct, resolvedOperation, region, response))
}

func resolveRunnableAPIOperation(productCode, operation, lang string) (string, string, error) {
	product, operationName, err := resolveAPIOperation(productCode, operation, lang)
	if err != nil {
		return "", "", err
	}
	if err := deprecatedAPIOperationError(product.Code, operationName, lang); err != nil {
		return "", "", err
	}
	return strings.ToLower(product.Code), operationName, nil
}

func resolveAPIOperation(productCode, operation, lang string) (aliyun.OpenAPIProduct, string, error) {
	selection, ok := apiProductSelectionByCode(productCode, lang)
	if !ok {
		return aliyun.OpenAPIProduct{}, "", ecerrors.Client("UnknownProduct", "call product is not supported",
			ecerrors.WithSuggestion(apiMessage(lang, "SuggestionCallListProducts", nil)),
		)
	}
	matches := make([]struct {
		product aliyun.OpenAPIProduct
		name    string
	}, 0, 1)
	for _, product := range selection.Products {
		operationName, ok := apiOperationName(product, operation)
		if ok {
			matches = append(matches, struct {
				product aliyun.OpenAPIProduct
				name    string
			}{product: product, name: operationName})
		}
	}
	if len(matches) == 0 {
		return aliyun.OpenAPIProduct{}, "", ecerrors.Client("UnknownOperation", "call operation is not supported",
			ecerrors.WithField("operation"),
			ecerrors.WithSuggestion(apiProductAPIsSuggestion(lang, selection.Requested)),
		)
	}
	if len(matches) > 1 {
		return aliyun.OpenAPIProduct{}, "", ecerrors.Client("AmbiguousOperation", "call operation matches multiple products",
			ecerrors.WithField("operation"),
			ecerrors.WithSuggestion(apiProductAPIsSuggestion(lang, selection.Requested)),
		)
	}
	return matches[0].product, matches[0].name, nil
}

func deprecatedAPIOperationError(productCode, operation, lang string) error {
	api, ok := aliyun.OpenAPIOperationSummaryFor(lang, productCode, operation)
	if !ok || !api.Deprecated {
		return nil
	}
	product := strings.ToLower(productCode)
	return ecerrors.Client("DeprecatedOperation", "call operation is deprecated",
		ecerrors.WithField("operation"),
		ecerrors.WithSuggestion(apiProductAPIsSuggestion(lang, product)),
	)
}

func resolveAPIRegion(options *globalOptions, request map[string]any, aliyunArgs []string) (string, error) {
	if options.region != "" {
		return resolveRegion(options)
	}
	if region := apiRequestRegion(request); region != "" {
		resolved, appErr := config.ResolveRegion(region, nil)
		if appErr != nil {
			return "", appErr
		}
		return resolved, nil
	}
	if region, ok := apiCallPassthroughFlagValue(aliyunArgs, "region"); ok {
		resolved, appErr := config.ResolveRegion(region, nil)
		if appErr != nil {
			return "", appErr
		}
		return resolved, nil
	}
	if apiCallPassthroughHasFlag(aliyunArgs, "profile") || apiCallPassthroughHasFlag(aliyunArgs, "config-path") {
		return "", nil
	}
	region, appErr := config.ResolveRegionForProfile("", config.ProfileName(options.profile, os.Getenv), config.ConfigPath(os.Getenv), os.Getenv)
	if appErr != nil {
		if code, _, ok := appErrorCodeMessage(appErr); ok && code == "MissingRegion" {
			return "", nil
		}
		return "", appErr
	}
	return region, nil
}

func apiRequestRegion(request map[string]any) string {
	region, _ := request["RegionId"].(string)
	return strings.TrimSpace(region)
}

func parseAPIRequest(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	if strings.HasPrefix(raw, "@") {
		path := strings.TrimPrefix(raw, "@")
		if path == "" {
			return nil, ecerrors.Client("InvalidParameter", "--request @file could not be read", ecerrors.WithField("request"))
		}
		loaded, err := os.ReadFile(path)
		if err != nil {
			return nil, ecerrors.Client("InvalidParameter", "--request @file could not be read", ecerrors.WithField("request"))
		}
		raw = strings.TrimSpace(string(loaded))
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return nil, invalidAPIRequestError()
	}
	if request == nil {
		return nil, invalidAPIRequestError()
	}
	return request, nil
}

func invalidAPIRequestError() error {
	return ecerrors.Client("InvalidParameter", "--request must be a JSON object or @file containing a JSON object", ecerrors.WithField("request"))
}

func mergeAPIRequestParams(request map[string]any, requestParams []string) error {
	for _, raw := range requestParams {
		key, value, ok := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return ecerrors.Client("InvalidParameter", "OpenAPI parameter flags must be --Parameter value or --Parameter=value", ecerrors.WithField("request"))
		}
		request[key] = parseAPIRequestParamValue(value)
	}
	return nil
}

func parseAPIRequestParamValue(raw string) any {
	var decoded any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded); err == nil {
		return decoded
	}
	return raw
}

func apiCallPayload(product, operation, region string, response map[string]any) map[string]any {
	if response == nil {
		response = map[string]any{}
	}
	return map[string]any{
		"product":   product,
		"operation": operation,
		"region":    region,
		"response":  response,
	}
}
