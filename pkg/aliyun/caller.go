package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	openapiClient "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

// Throttle-retry defaults: Alibaba Cloud throttles per-API per-account, so any
// call can transiently fail with a Throttling* code. These bound an
// exponential backoff with full jitter applied to every API call.
const (
	defaultThrottleMaxAttempts  = 6
	defaultThrottleBaseInterval = 500 * time.Millisecond
	defaultThrottleMaxInterval  = 10 * time.Second
)

type openAPIExecutor interface {
	ExecuteOpenAPI(context.Context, *openAPIRequest) (map[string]any, error)
}

type resolvedOpenAPIProfile struct {
	Name            string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	RegionID        string
	Language        string
}

const topLevelArrayResponseMarker = "__ecctl_top_level_array_response"

type OpenAPICaller struct {
	Product          string
	Resource         string
	Region           string
	Profile          resolvedOpenAPIProfile
	executor         openAPIExecutor
	endpointResolver func(OpenAPIProduct, string, string) (string, error)

	// Throttle-retry knobs; zero values fall back to the defaults above. Exposed
	// (unexported, same-package) so tests can shrink intervals and inject sleep.
	retryMaxAttempts  int
	retryBaseInterval time.Duration
	retryMaxInterval  time.Duration
	sleepFn           func(context.Context, time.Duration) error
}

func NewOpenAPICaller(profileName, configPath, product, region string, getenv func(string) string) (*OpenAPICaller, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	profile, _, err := ecconfig.EffectiveProfile(profileName, configPath, ecconfig.AliyunConfigPath(getenv))
	if err != nil {
		return nil, ecerrors.Client("InvalidConfig", err.Error())
	}
	openAPIProfile := toOpenAPIProfile(profile, region, getenv)
	if openAPIProfile.AccessKeyID == "" || openAPIProfile.AccessKeySecret == "" {
		return nil, ecerrors.Client("MissingCredentials", "Alibaba Cloud access key is required")
	}
	executor, err := newDarabonbaExecutor(openAPIProfile)
	if err != nil {
		return nil, ecerrors.Client("InvalidCredentials", callerSanitizeCloudError(err))
	}
	return &OpenAPICaller{
		Product:  product,
		Region:   openAPIProfile.RegionID,
		Profile:  openAPIProfile,
		executor: executor,
	}, nil
}

func toOpenAPIProfile(profile ecconfig.Profile, region string, getenv func(string) string) resolvedOpenAPIProfile {
	accessKeyID := firstNonEmptyString(profile.AccessKeyID, getenv("ALIBABA_CLOUD_ACCESS_KEY_ID"), getenv("ALIBABACLOUD_ACCESS_KEY_ID"), getenv("ALICLOUD_ACCESS_KEY_ID"))
	accessKeySecret := firstNonEmptyString(profile.AccessKeySecret, getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET"), getenv("ALIBABACLOUD_ACCESS_KEY_SECRET"), getenv("ALICLOUD_ACCESS_KEY_SECRET"))
	securityToken := firstNonEmptyString(profile.SecurityToken, getenv("ALIBABA_CLOUD_SECURITY_TOKEN"), getenv("ALIBABACLOUD_SECURITY_TOKEN"), getenv("ALICLOUD_SECURITY_TOKEN"))
	return resolvedOpenAPIProfile{
		Name:            firstNonEmptyString(profile.Name, ecconfig.DefaultProfileName),
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		SecurityToken:   securityToken,
		RegionID:        firstNonEmptyString(region, profile.Region, getenv("ECCTL_REGION")),
		Language:        firstNonEmptyString(profile.Language, "en"),
	}
}

type darabonbaExecutor struct {
	client *openapiClient.Client
	mu     sync.Mutex
}

func newDarabonbaExecutor(profile resolvedOpenAPIProfile) (*darabonbaExecutor, error) {
	config := &openapiClient.Config{
		AccessKeyId:     tea.String(profile.AccessKeyID),
		AccessKeySecret: tea.String(profile.AccessKeySecret),
		SecurityToken:   tea.String(profile.SecurityToken),
		RegionId:        tea.String(profile.RegionID),
		UserAgent:       tea.String("ecctl"),
	}
	client, err := openapiClient.NewClient(config)
	if err != nil {
		return nil, err
	}
	client.DisableSDKError = tea.Bool(true)
	return &darabonbaExecutor{client: client}, nil
}

func (e *darabonbaExecutor) ExecuteOpenAPI(ctx context.Context, req *openAPIRequest) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	originalEndpoint := e.client.Endpoint
	originalRegionID := e.client.RegionId
	originalProductID := e.client.ProductId
	defer func() {
		e.client.Endpoint = originalEndpoint
		e.client.RegionId = originalRegionID
		e.client.ProductId = originalProductID
	}()
	body := req.BodyValue()
	reqBodyType := "json"
	if body == nil && len(req.FormParams) > 0 {
		body = stringMapAny(req.FormParams)
		reqBodyType = "formData"
	}
	openReq := &openapiClient.OpenApiRequest{
		Query:   stringPointerMap(req.QueryParams),
		Headers: stringPointerMap(req.Headers),
		Body:    body,
	}
	if strings.TrimSpace(req.Domain) != "" {
		e.client.Endpoint = tea.String(req.Domain)
		openReq.EndpointOverride = tea.String(req.Domain)
	}
	if strings.TrimSpace(req.RegionId) != "" {
		e.client.RegionId = tea.String(req.RegionId)
	}
	if strings.TrimSpace(req.Product) != "" {
		e.client.ProductId = tea.String(req.Product)
	}
	params := &openapiClient.Params{
		Action:      tea.String(req.ApiName),
		Version:     tea.String(req.Version),
		Protocol:    tea.String(req.Scheme),
		Pathname:    tea.String(req.BuildPath()),
		Method:      tea.String(req.Method),
		AuthType:    tea.String("AK"),
		BodyType:    tea.String("string"),
		ReqBodyType: tea.String(reqBodyType),
		Style:       tea.String(openAPIStyle(req.Style)),
	}
	response, err := e.client.CallApiWithCtx(ctx, params, openReq, &dara.RuntimeOptions{})
	if err != nil {
		return nil, err
	}
	return normalizeDarabonbaResponse(response)
}

func (c *OpenAPICaller) Call(ctx context.Context, operation string, request map[string]any) (map[string]any, error) {
	return c.CallRaw(ctx, operation, request)
}

func (c *OpenAPICaller) CallRaw(ctx context.Context, operation string, request map[string]any) (map[string]any, error) {
	req, err := c.commonRequest(operation, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.executeOpenAPIRequest(ctx, operation, req)
	if err != nil {
		if callerBoolMapValue(request, "DryRun") && isDryRunPassed(err) {
			return map[string]any{"DryRun": true}, nil
		}
		if isCloudNotFound(err) {
			code, _, _ := ecerrors.ParseCloudError(err.Error())
			return nil, ecerrors.NotFound("NotFound", cloudNotFoundMessage(request, code), cloudErrorOptions(err)...)
		}
		if isDependencyViolation(err) {
			return nil, ecerrors.Service("DependencyViolation", callerCloudErrorMessage(err), false, cloudErrorOptions(err)...)
		}
		retryable := isThrottling(err) || (isReadOperation(operation) && isTransientNetworkError(err))
		return nil, ecerrors.Service("CloudAPIError", callerCloudErrorMessage(err), retryable, cloudErrorOptions(err)...)
	}
	return resp, nil
}

// executeOpenAPIRequest calls OpenAPI, retrying with exponential backoff and
// full jitter while the cloud reports throttling (safe for any operation, since
// throttled requests are rejected before processing) or a transient network
// error on a read-only operation (where a duplicate has no side effect). Other
// errors return immediately. Retrying stops at the attempt limit or when ctx is
// done, in which case the last error is surfaced (it is the actionable cause).
func (c *OpenAPICaller) executeOpenAPIRequest(ctx context.Context, operation string, req *openAPIRequest) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.executor == nil {
		return nil, ecerrors.Client("OpenAPINotInitialized", "OpenAPI caller is not initialised")
	}
	attempts := c.retryMaxAttempts
	if attempts <= 0 {
		attempts = defaultThrottleMaxAttempts
	}
	base := c.retryBaseInterval
	if base <= 0 {
		base = defaultThrottleBaseInterval
	}
	maxInterval := c.retryMaxInterval
	if maxInterval < base {
		maxInterval = defaultThrottleMaxInterval
	}
	if maxInterval < base {
		maxInterval = base
	}
	sleep := c.sleepFn
	if sleep == nil {
		sleep = sleepWithContext
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := c.executor.ExecuteOpenAPI(ctx, req)
		if err == nil {
			return resp, nil
		}
		retryable := isThrottling(err) || (isReadOperation(operation) && isTransientNetworkError(err))
		if !retryable {
			return nil, err
		}
		lastErr = err
		if attempt == attempts-1 {
			break
		}
		if serr := sleep(ctx, throttleBackoff(attempt, base, maxInterval)); serr != nil {
			break
		}
	}
	return nil, lastErr
}

// throttleBackoff returns base*2^attempt capped at max, with full jitter applied
// (a uniformly random duration in [d/2, d]) to spread out retries.
func throttleBackoff(attempt int, base, max time.Duration) time.Duration {
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= max {
			d = max
			break
		}
	}
	if d > max {
		d = max
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *OpenAPICaller) commonRequest(operation string, values map[string]any) (*openAPIRequest, error) {
	product, ok := OpenAPIProductByCode(c.Product, c.Profile.Language)
	if !ok {
		return nil, ecerrors.Client("UnsupportedProduct", "product "+c.Product+" is not supported")
	}
	operationName, ok := OpenAPIOperationName(product, operation)
	if !ok {
		return nil, ecerrors.Client("UnsupportedOperation", "operation "+operation+" is not supported")
	}
	api, ok := OpenAPIOperationDetailFor(c.Profile.Language, product, operationName)
	if !ok {
		return nil, ecerrors.Client("UnsupportedOperation", "operation "+operation+" is not supported")
	}
	req := newOpenAPIRequest()
	req.Product = product.Code
	req.Version = product.Version
	req.ApiName = operationName
	req.RegionId = firstNonEmptyString(callerStringMapValue(values, "RegionId"), c.Region)
	endpoint, err := c.productEndpoint(product, req.RegionId)
	if err != nil {
		return nil, err
	}
	req.Domain = endpoint
	req.Scheme = openAPIProtocol(api.Protocol)
	req.Method = openAPIMethod(api.Method)
	req.PathPattern = api.PathPattern
	req.Style = api.Style
	body, bodyKeys, err := openAPIRequestBody(&api, values)
	if err != nil {
		return nil, err
	}
	if req.RegionId != "" && api.FindParameter("RegionId") != nil {
		if err := setOpenAPIParam(req, &api, "RegionId", req.RegionId); err != nil {
			return nil, err
		}
	}
	for key, value := range values {
		if key == "RegionId" {
			continue
		}
		if bodyKeys[key] {
			continue
		}
		if err := setOpenAPIParam(req, &api, key, value); err != nil {
			return nil, err
		}
	}
	if body != nil {
		if err := setOpenAPIBody(req, body); err != nil {
			return nil, err
		}
	}
	return req, nil
}

func openAPIMethod(methodText string) string {
	method := strings.ToUpper(methodText)
	if strings.Contains(method, "POST") {
		return "POST"
	}
	if strings.Contains(method, "GET") {
		return "GET"
	}
	if strings.Contains(method, "PUT") {
		return "PUT"
	}
	if strings.Contains(method, "DELETE") {
		return "DELETE"
	}
	return "GET"
}

func openAPIProtocol(protocol string) string {
	lowered := strings.ToLower(protocol)
	if strings.HasPrefix(lowered, "https") {
		return "https"
	}
	for _, part := range strings.Split(lowered, "|") {
		if strings.TrimSpace(part) == "https" {
			return "https"
		}
	}
	return "http"
}

func (c *OpenAPICaller) productEndpoint(product OpenAPIProduct, region string) (string, error) {
	productCode := firstNonEmptyString(product.Code, c.Product)
	if c.endpointResolver != nil {
		endpoint, err := c.endpointResolver(product, region, "")
		if err != nil {
			return "", ecerrors.Client("UnknownEndpoint", err.Error())
		}
		return normalizeProductEndpoint(productCode, region, "", endpoint), nil
	}
	endpoint := metadataEndpoint(product.Endpoints, region, "")
	endpoint = normalizeProductEndpoint(productCode, region, "", endpoint)
	if endpoint == "" {
		endpoint = inferRegionalEndpoint(productCode, region, "")
	}
	return endpoint, nil
}

func setOpenAPIParam(req *openAPIRequest, api *OpenAPIOperationDetail, key string, value any) error {
	out := req.QueryParams
	if param := api.FindParameter(key); param != nil {
		switch param.Position {
		case "", "Query":
			out = req.QueryParams
		case "Body", "FormData":
			if req.FormParams == nil {
				req.FormParams = map[string]string{}
			}
			out = req.FormParams
		case "Path":
			out = req.PathParams
		case "Header", "header":
			out = req.Headers
		case "Domain":
			return nil
		default:
			return fmt.Errorf("unknown parameter position; %s is %s", param.Name, param.Position)
		}
		return setOpenAPIParamValue(out, param, key, value)
	}
	return setQueryParam(out, key, value)
}

func setOpenAPIParamValue(out map[string]string, param *OpenAPIParameter, key string, value any) error {
	if param != nil && strings.EqualFold(param.Type, "Json") {
		return setJSONParam(out, key, value)
	}
	return setQueryParam(out, key, value)
}

func setJSONParam(out map[string]string, key string, value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(typed) != "" {
			out[key] = typed
		}
		return nil
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		out[key] = string(raw)
		return nil
	}
}

func openAPIRequestBody(api *OpenAPIOperationDetail, values map[string]any) (any, map[string]bool, error) {
	used := map[string]bool{}
	bodyParam := api.FindParameter("body")
	if bodyParam == nil || bodyParam.Position != "Body" {
		return nil, used, nil
	}
	if body, ok := values["body"]; ok {
		used["body"] = true
		if isEmptyOpenAPIBody(body) {
			return nil, used, nil
		}
		return body, used, nil
	}
	body, err := flatOpenAPIBody(values, used)
	if err != nil || isEmptyOpenAPIBody(body) {
		return nil, used, err
	}
	return body, used, nil
}

func flatOpenAPIBody(values map[string]any, used map[string]bool) (any, error) {
	var object map[string]any
	var items []any
	for key, value := range values {
		if !strings.HasPrefix(key, "body.") || isEmptyOpenAPIBody(value) {
			continue
		}
		used[key] = true
		parts := strings.Split(strings.TrimPrefix(key, "body."), ".")
		if len(parts) == 0 || parts[0] == "" {
			return nil, fmt.Errorf("invalid body field %q", key)
		}
		index, err := strconv.Atoi(parts[0])
		if err == nil {
			if index < 1 {
				return nil, fmt.Errorf("invalid body array index in %q", key)
			}
			if object != nil {
				return nil, fmt.Errorf("body cannot mix object and array fields")
			}
			for len(items) < index {
				items = append(items, nil)
			}
			if len(parts) == 1 {
				items[index-1] = value
				continue
			}
			item, ok := items[index-1].(map[string]any)
			if !ok {
				item = map[string]any{}
				items[index-1] = item
			}
			assignNestedOpenAPIField(item, parts[1:], value)
			continue
		}
		if items != nil {
			return nil, fmt.Errorf("body cannot mix object and array fields")
		}
		if object == nil {
			object = map[string]any{}
		}
		assignNestedOpenAPIField(object, parts, value)
	}
	if items != nil {
		return items, nil
	}
	return object, nil
}

func assignNestedOpenAPIField(object map[string]any, parts []string, value any) {
	current := object
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func setOpenAPIBody(req *openAPIRequest, value any) error {
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		req.SetContent([]byte(text))
		req.SetContentType("application/json")
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req.SetContent(raw)
	req.SetContentType("application/json")
	return nil
}

func isEmptyOpenAPIBody(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []map[string]any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func normalizeProductEndpoint(productCode string, region string, endpointType string, endpoint string) string {
	if !strings.EqualFold(productCode, "Tag") || region == "" {
		return endpoint
	}
	if strings.EqualFold(endpointType, "vpc") {
		return "tag-vpc." + region + ".aliyuncs.com"
	}
	if endpoint == "" || endpoint == "tag.aliyuncs.com" {
		return "tag." + region + ".aliyuncs.com"
	}
	return endpoint
}

func inferRegionalEndpoint(productCode string, region string, endpointType string) string {
	if endpoint := fallbackGlobalEndpoint(productCode); endpoint != "" {
		return endpoint
	}
	if region != "" {
		code := strings.ToLower(productCode)
		if strings.EqualFold(endpointType, "vpc") {
			return code + "-vpc." + region + ".aliyuncs.com"
		}
		return code + "." + region + ".aliyuncs.com"
	}
	return ""
}

func metadataEndpoint(endpoints map[string]OpenAPIEndpoint, region string, endpointType string) string {
	if len(endpoints) == 0 {
		return ""
	}
	endpoint := endpoints[region]
	if strings.EqualFold(endpointType, "vpc") {
		return firstNonEmptyString(endpoint.VPC, endpoint.Public)
	}
	return endpoint.Public
}

func fallbackGlobalEndpoint(productCode string) string {
	switch strings.ToLower(productCode) {
	case "resourcemanager":
		return "resourcemanager.aliyuncs.com"
	default:
		return ""
	}
}

func setQueryParam(out map[string]string, key string, value any) error {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			out[key] = typed
		}
	case bool:
		out[key] = strconv.FormatBool(typed)
	case int:
		out[key] = strconv.Itoa(typed)
	case int64:
		out[key] = strconv.FormatInt(typed, 10)
	case float64:
		out[key] = strconv.FormatFloat(typed, 'f', -1, 64)
	case []string:
		if key == "Tag" || key == "TemplateTag" {
			return setTagParams(out, key, typed)
		}
		if key == "ResourceGroupIds" {
			setRepeatedStringParams(out, key, typed)
			return nil
		}
		if isJSONArrayStringListParam(key) {
			raw, err := json.Marshal(typed)
			if err != nil {
				return err
			}
			out[key] = string(raw)
			return nil
		}
		if len(typed) > 0 {
			out[key] = strings.Join(typed, ",")
		}
	case []any:
		if isJSONArrayStringListParam(key) {
			values := make([]string, len(typed))
			for i, item := range typed {
				stringValue, ok := item.(string)
				if !ok {
					out[key] = fmt.Sprint(typed)
					return nil
				}
				values[i] = stringValue
			}
			raw, err := json.Marshal(values)
			if err != nil {
				return err
			}
			out[key] = string(raw)
			return nil
		}
		out[key] = fmt.Sprint(value)
	default:
		if value != nil {
			out[key] = fmt.Sprint(value)
		}
	}
	return nil
}

func isJSONArrayStringListParam(key string) bool {
	return strings.HasSuffix(key, "Ids") || key == "NodeIdList" || key == "Nodes"
}

func setRepeatedStringParams(out map[string]string, key string, values []string) {
	for i, value := range values {
		if value == "" {
			continue
		}
		out[key+"."+strconv.Itoa(i+1)] = value
	}
}

func setTagParams(out map[string]string, key string, tags []string) error {
	assignments, err := parseTagAssignments(tags)
	if err != nil {
		return err
	}
	for i, tag := range assignments {
		index := strconv.Itoa(i + 1)
		out[key+"."+index+".Key"] = tag.Key
		out[key+"."+index+".Value"] = tag.Value
	}
	return nil
}

func decodeOpenAPIResponse(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, ecerrors.Service("CloudAPIError", "Alibaba Cloud API returned non-JSON response", false)
	}
	switch typed := decoded.(type) {
	case map[string]any:
		return typed, nil
	case []any:
		return map[string]any{
			"items":                     typed,
			topLevelArrayResponseMarker: true,
		}, nil
	default:
		return nil, ecerrors.Service("CloudAPIError", "Alibaba Cloud API returned unsupported JSON response", false)
	}
}

func normalizeDarabonbaResponse(response map[string]any) (map[string]any, error) {
	if response == nil {
		return map[string]any{}, nil
	}
	if body, ok := response["body"]; ok {
		switch typed := body.(type) {
		case map[string]any:
			return typed, nil
		case []any:
			return map[string]any{
				"items":                     typed,
				topLevelArrayResponseMarker: true,
			}, nil
		case string:
			return decodeOpenAPIResponse(typed)
		case nil:
			return map[string]any{}, nil
		default:
			return nil, ecerrors.Service("CloudAPIError", "Alibaba Cloud API returned unsupported JSON response", false)
		}
	}
	return response, nil
}

func stringPointerMap(values map[string]string) map[string]*string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*string, len(values))
	for key, value := range values {
		out[key] = tea.String(value)
	}
	return out
}

func stringMapAny(values map[string]string) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func openAPIStyle(style string) string {
	switch strings.ToLower(style) {
	case "roa", "restful":
		return "ROA"
	default:
		return "RPC"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func callerStringMapValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func callerBoolMapValue(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func callerSanitizeCloudError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, marker := range []string{"AccessKeySecret", "access_key_secret"} {
		if strings.Contains(message, marker) {
			return "Alibaba Cloud API request failed"
		}
	}
	message = redactCredentialBearingURLs(message)
	message = redactSensitiveAssignments(message)
	message = redactDenyPrincipal(message)
	return message
}

func callerCloudErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	sanitized := callerSanitizeCloudError(err)
	if sanitized != err.Error() {
		return sanitized
	}
	_, message, _ := ecerrors.ParseCloudError(err.Error())
	if message != "" {
		return message
	}
	return sanitized
}

func cloudErrorOptions(err error) []ecerrors.Option {
	code, message, requestID := ecerrors.ParseCloudError(callerSanitizeCloudError(err))
	options := make([]ecerrors.Option, 0, 2)
	if requestID != "" {
		options = append(options, ecerrors.WithRequestID(requestID))
	}
	if code != "" || message != "" {
		options = append(options, ecerrors.WithRawCause(code, message))
	}
	return options
}

var (
	credentialURLPattern       = regexp.MustCompile(`https?://[^\s"]+`)
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)(^|[\s?&])(?:AccessKeyId|SignatureNonce|Signature|SecurityToken)=[^&\s"]+`)
	sensitiveCredentialURLKeys = map[string]bool{"accesskeyid": true, "signature": true, "signaturenonce": true, "securitytoken": true}
)

func redactCredentialBearingURLs(message string) string {
	return credentialURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil || !urlHasSensitiveQuery(parsed) {
			return raw
		}
		parsed.RawQuery = ""
		parsed.ForceQuery = false
		return parsed.String() + "?[REDACTED]"
	})
}

func urlHasSensitiveQuery(parsed *url.URL) bool {
	if parsed == nil || parsed.RawQuery == "" {
		return false
	}
	for key := range parsed.Query() {
		if sensitiveCredentialURLKeys[strings.ToLower(key)] {
			return true
		}
	}
	return false
}

func redactSensitiveAssignments(message string) string {
	return sensitiveAssignmentPattern.ReplaceAllString(message, `${1}[REDACTED_CREDENTIAL]`)
}

func cloudNotFoundMessage(request map[string]any, code string) string {
	if id := cloudNotFoundResourceID(request, code); id != "" {
		return id + " not found"
	}
	return "resource not found"
}

func cloudNotFoundResourceID(request map[string]any, code string) string {
	prefix, _, _ := strings.Cut(code, ".")
	field := strings.TrimPrefix(prefix, "Invalid")
	if !strings.HasSuffix(field, "Id") {
		return ""
	}
	return callerStringMapValue(request, field)
}

func redactDenyPrincipal(message string) string {
	const prefix = "Deny: "
	start := strings.Index(message, prefix)
	if start < 0 {
		return message
	}
	valueStart := start + len(prefix)
	valueEnd := strings.Index(message[valueStart:], "|")
	if valueEnd < 0 {
		return message
	}
	valueEnd += valueStart
	return message[:valueStart] + "[REDACTED]" + message[valueEnd:]
}
