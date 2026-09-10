package e2bapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

const (
	defaultDomain   = "cn-hangzhou.e2b.fc.aliyuncs.com"
	defaultTimeout  = 30 * time.Second
	maxErrorBodyLen = 64 << 10
	arrayMarker     = "__ecctl_top_level_array_response"
)

type operation struct {
	method string
	path   string
	// FC's legacy list must be a single, unambiguous complete array. Keep
	// strict decoding scoped to that operation; native E2B stays unchanged.
	completeArray bool
}

var operations = map[string]operation{
	"CreateSandbox":          {method: http.MethodPost, path: "/sandboxes"},
	"ListSandboxes":          {method: http.MethodGet, path: "/v2/sandboxes"},
	"GetSandbox":             {method: http.MethodGet, path: "/sandboxes/{sandboxID}"},
	"KillSandbox":            {method: http.MethodDelete, path: "/sandboxes/{sandboxID}"},
	"PauseSandbox":           {method: http.MethodPost, path: "/sandboxes/{sandboxID}/pause"},
	"ConnectSandbox":         {method: http.MethodPost, path: "/sandboxes/{sandboxID}/connect"},
	"UpdateSandboxTimeout":   {method: http.MethodPost, path: "/sandboxes/{sandboxID}/timeout"},
	"UpdateSandboxNetwork":   {method: http.MethodPut, path: "/sandboxes/{sandboxID}/network"},
	"RefreshSandbox":         {method: http.MethodPost, path: "/sandboxes/{sandboxID}/refreshes"},
	"ForkSandbox":            {method: http.MethodPost, path: "/sandboxes/{sandboxID}/fork"},
	"GetSandboxLogs":         {method: http.MethodGet, path: "/v2/sandboxes/{sandboxID}/logs"},
	"GetSandboxMetrics":      {method: http.MethodGet, path: "/sandboxes/{sandboxID}/metrics"},
	"CreateSandboxSnapshot":  {method: http.MethodPost, path: "/sandboxes/{sandboxID}/snapshots"},
	"CreateTemplate":         {method: http.MethodPost, path: "/v3/templates"},
	"StartTemplateBuild":     {method: http.MethodPost, path: "/v2/templates/{templateID}/builds/{buildID}"},
	"ListTemplates":          {method: http.MethodGet, path: "/v2/templates"},
	"GetTemplate":            {method: http.MethodGet, path: "/templates/{templateID}"},
	"DeleteTemplate":         {method: http.MethodDelete, path: "/templates/{templateID}"},
	"UpdateTemplate":         {method: http.MethodPatch, path: "/v2/templates/{templateID}"},
	"GetTemplateBuildStatus": {method: http.MethodGet, path: "/templates/{templateID}/builds/{buildID}/status"},
	"GetTemplateBuildLogs":   {method: http.MethodGet, path: "/templates/{templateID}/builds/{buildID}/logs"},
	"ListTemplateTags":       {method: http.MethodGet, path: "/templates/{templateID}/tags"},
	"AssignTemplateTags":     {method: http.MethodPost, path: "/templates/tags"},
	"DeleteTemplateTags":     {method: http.MethodDelete, path: "/templates/tags"},
}

// Caller adapts the E2B REST API to ecctl's spec execution engine.
type Caller struct {
	endpoint      *url.URL
	apiKey        string
	client        *http.Client
	lookupCNAME   cnameLookup
	backendMu     sync.Mutex
	backendResult *backendDetection
	backend       backendKind
}

// NewCaller resolves the E2B endpoint and project API key from the standard
// E2B environment variables.
func NewCaller(getenv func(string) string) (*Caller, error) {
	return NewCallerWithRegion(context.Background(), "", getenv)
}

// NewCallerWithRegion uses a supported FC sandbox region only when neither
// E2B_API_URL nor E2B_DOMAIN is set. Missing or unsupported regions use Hangzhou.
func NewCallerWithRegion(ctx context.Context, region string, getenv func(string) string) (*Caller, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	apiKey := strings.TrimSpace(getenv("E2B_API_KEY"))
	if apiKey == "" {
		return nil, ecerrors.Client("MissingCredential", "E2B_API_KEY is required",
			ecerrors.WithSuggestion("Set E2B_API_KEY to an E2B project API key."))
	}
	rawEndpoint := strings.TrimSpace(getenv("E2B_API_URL"))
	if rawEndpoint == "" {
		domain := strings.TrimSpace(getenv("E2B_DOMAIN"))
		if domain == "" {
			domain = defaultFCDomain(ctx, region)
		}
		if strings.Contains(domain, "://") || strings.ContainsAny(domain, "/?#@") {
			return nil, invalidEndpoint("E2B_DOMAIN must be a hostname")
		}
		rawEndpoint = "https://api." + domain
	}
	backend := backendKind(strings.TrimSpace(getenv("ECCTL_SANDBOX_BACKEND")))
	switch backend {
	case "", backendAuto, backendE2B, backendFC, backendACS:
	default:
		return nil, ecerrors.Client("InvalidSandboxBackend", e2bMessage("InvalidSandboxBackend"),
			ecerrors.WithField("ECCTL_SANDBOX_BACKEND"), ecerrors.WithAcceptedValues("auto", "e2b", "fc", "acs"))
	}
	client, err := clientWithCA(strings.TrimSpace(getenv("ECCTL_SANDBOX_CA_FILE")))
	if err != nil {
		return nil, err
	}
	caller, err := NewCallerWithClient(rawEndpoint, apiKey, client)
	if err != nil {
		return nil, err
	}
	caller.backend = backend
	return caller, nil
}

// NewCallerWithClient is an explicit constructor used by tests and self-hosted
// E2B installations.
func NewCallerWithClient(rawEndpoint, apiKey string, client *http.Client) (*Caller, error) {
	endpoint, err := validateEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, ecerrors.Client("MissingCredential", "E2B_API_KEY is required")
	}
	if len(apiKey) > maxErrorBodyLen {
		return nil, ecerrors.Client("InvalidCredential", "E2B_API_KEY exceeds the supported length")
	}
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	// Never follow redirects after attaching the project API key. Go forwards
	// custom headers such as X-API-Key across redirects, including to another
	// origin, unless the client explicitly prevents it.
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Caller{endpoint: endpoint, apiKey: apiKey, client: &clientCopy, lookupCNAME: lookupSystemCNAME}, nil
}

func validateEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, invalidEndpoint("E2B_API_URL must be an absolute URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, invalidEndpoint("E2B_API_URL cannot contain user info, query parameters, or a fragment")
	}
	if parsed.Scheme != "https" {
		host := strings.Trim(parsed.Hostname(), "[]")
		ip := net.ParseIP(host)
		if parsed.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, invalidEndpoint("E2B_API_URL must use HTTPS except for a literal loopback address")
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func invalidEndpoint(message string) error {
	return ecerrors.Client("InvalidEndpoint", message, ecerrors.WithField("E2B_API_URL"))
}

func (c *Caller) Call(ctx context.Context, operationName string, request map[string]any) (map[string]any, error) {
	op, ok := operations[operationName]
	if !ok {
		return nil, ecerrors.Client("UnsupportedOperation", fmt.Sprintf("E2B operation %q is not supported", operationName))
	}
	if err := c.validateAPIOperation(operationName, request); err != nil {
		return nil, err
	}
	if operationName == "ListTemplates" {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}
		return c.listTemplates(ctx, request)
	}
	result, err := c.call(ctx, operationName, op, request)
	if err != nil {
		if operationName == "StartTemplateBuild" {
			templateID := stringValue(firstValue(request, "path.templateID", "templateID"))
			buildID := stringValue(firstValue(request, "path.buildID", "buildID"))
			if templateID != "" {
				err = ecerrors.WithDetails(err,
					ecerrors.WithDetail(fmt.Sprintf("E2B allocated template %q and build %q before the build start failed", templateID, buildID)),
					ecerrors.WithSuggestion("Inspect or delete the allocated template before retrying the create operation."),
					ecerrors.WithRecoveryCommand("ecctl", "sandbox", "template", "delete", templateID),
				)
			}
		}
		return nil, err
	}
	if operationName == "ForkSandbox" {
		if err := forkResultError(result, c.apiKey); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (c *Caller) call(ctx context.Context, operationName string, op operation, request map[string]any) (map[string]any, error) {
	requestCtx := ctx
	if _, ok := requestCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(requestCtx, defaultTimeout)
		defer cancel()
	}
	path, err := expandPath(op.path, request)
	if err != nil {
		return nil, err
	}
	target := *c.endpoint
	escapedPath := strings.TrimRight(c.endpoint.EscapedPath(), "/") + path
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, ecerrors.Client("InvalidRequest", "failed to encode E2B request path")
	}
	target.Path = decodedPath
	target.RawPath = escapedPath
	target.RawQuery = encodeQuery(requestObject(request, "query")).Encode()

	var body io.Reader
	bodyObject := requestObject(request, "body")
	if op.method != http.MethodGet && op.method != http.MethodHead && len(bodyObject) > 0 {
		encoded, err := json.Marshal(bodyObject)
		if err != nil {
			return nil, ecerrors.Client("InvalidParameter", "failed to encode E2B request body")
		}
		body = bytes.NewReader(encoded)
	}
	httpRequest, err := http.NewRequestWithContext(requestCtx, op.method, target.String(), body)
	if err != nil {
		return nil, ecerrors.Client("InvalidRequest", err.Error())
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-API-Key", c.apiKey)
	httpRequest.Header.Set("User-Agent", "ecctl/e2b")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(httpRequest)
	if err != nil {
		if requestCtx.Err() != nil {
			return nil, ecerrors.Timeout("E2BRequestTimeout", requestCtx.Err().Error())
		}
		return nil, ecerrors.Service("E2BRequestFailed", err.Error(), true)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, c.responseError(operationName, response)
	}

	result := map[string]any{}
	if op.completeArray {
		items, err := decodeCompleteJSONArray(response.Body)
		if err != nil {
			return nil, c.invalidTemplateResponse()
		}
		result = map[string]any{"items": items, arrayMarker: true}
	} else if hasResponseContract(operationName) {
		var err error
		result, err = decodeContractResponse(response.Body, operationName, request)
		if err != nil {
			return nil, ecerrors.Service("InvalidResponse", e2bMessage("InvalidSandboxResponse"), false,
				ecerrors.WithDetail(operationName), ecerrors.WithRequestID(redactString(firstHeader(response.Header, "X-Request-ID", "Request-ID"), c.apiKey)))
		}
	} else if response.StatusCode != http.StatusNoContent {
		decoder := json.NewDecoder(response.Body)
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil && err != io.EOF {
			return nil, ecerrors.Service("InvalidResponse", "E2B returned invalid JSON", false)
		}
		switch value := decoded.(type) {
		case map[string]any:
			result = value
		case []any:
			result = map[string]any{"items": value, arrayMarker: true}
		case nil:
		default:
			result["value"] = value
		}
	}
	if next := strings.TrimSpace(response.Header.Get("X-Next-Token")); next != "" {
		result["nextToken"] = next
	}
	if total := strings.TrimSpace(response.Header.Get("X-Total-Running")); total != "" {
		if parsed, err := strconv.Atoi(total); err == nil {
			result["total"] = parsed
		}
	}
	if requestID := firstHeader(response.Header, "X-Request-ID", "Request-ID"); requestID != "" {
		result["requestID"] = requestID
	}
	redactValue(result, c.apiKey)
	return result, nil
}

func forkResultError(result map[string]any, apiKey string) error {
	rawItems, ok := result["items"]
	if !ok {
		return nil
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil
	}
	var failures []string
	var successfulIDs []string
	var actions []ecerrors.Action
	requestID := stringValue(result["requestID"])
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		if sandbox, _ := item["sandbox"].(map[string]any); sandbox != nil {
			if id := stringValue(sandbox["sandboxID"]); id != "" {
				actions = append(actions, ecerrors.Action{ActionName: "ForkSandbox", RequestID: requestID, Code: "created", Message: "created sandbox " + id})
				if !strings.Contains(id, "[REDACTED]") {
					successfulIDs = append(successfulIDs, id)
				}
			}
		}
		errorObject, _ := item["error"].(map[string]any)
		if errorObject == nil {
			continue
		}
		code := stringValue(firstValue(errorObject, "error_code", "code"))
		message := stringValue(errorObject["message"])
		if code == "" {
			code = "fork_failed"
		}
		if message == "" {
			message = "E2B failed to start a forked sandbox"
		}
		if apiKey != "" {
			message = strings.ReplaceAll(message, apiKey, "[REDACTED]")
		}
		failures = append(failures, code+": "+message)
		actions = append(actions, ecerrors.Action{ActionName: "ForkSandbox", RequestID: requestID, Code: code, Message: message})
	}
	if len(failures) == 0 {
		return nil
	}
	detail := fmt.Sprintf("E2B reported %d failed fork result(s): %s", len(failures), strings.Join(failures, "; "))
	options := []ecerrors.Option{
		ecerrors.WithDetail(detail),
		ecerrors.WithSuggestion("Inspect the fork errors and delete any successfully created sandboxes before retrying."),
	}
	if len(successfulIDs) > 0 {
		detail += "; successfully created sandbox IDs: " + strings.Join(successfulIDs, ", ")
		options[0] = ecerrors.WithDetail(detail)
		if len(successfulIDs) == 1 {
			options = append(options, ecerrors.WithRecoveryCommand("ecctl", "sandbox", "delete", successfulIDs[0]))
		}
	}
	err := ecerrors.Service("E2BForkPartialFailure", "one or more E2B sandbox forks failed", false, options...)
	return ecerrors.WithActions(err, actions)
}

func (c *Caller) responseError(operationName string, response *http.Response) error {
	apiKey := c.apiKey
	readLimit := int64(maxErrorBodyLen)
	if keyLength := len(apiKey); keyLength > 1 && keyLength <= maxErrorBodyLen {
		// Read enough look-ahead to redact a key that starts immediately before
		// the retained-body boundary, then enforce the public error-size limit.
		readLimit += int64(keyLength - 1)
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, readLimit))
	if apiKey != "" {
		raw = []byte(redactString(string(raw), apiKey))
	}
	if len(raw) > maxErrorBodyLen {
		raw = raw[:maxErrorBodyLen]
	}
	message := strings.TrimSpace(string(raw))
	var payload map[string]any
	if json.Unmarshal(raw, &payload) == nil {
		for _, key := range []string{"message", "error", "detail"} {
			if candidate := stringValue(payload[key]); candidate != "" {
				message = candidate
				break
			}
		}
	}
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	message = redactString(message, apiKey)
	code := fmt.Sprintf("E2BHTTP%d", response.StatusCode)
	options := []ecerrors.Option{
		ecerrors.WithRawCause(code, message),
		ecerrors.WithDetail(fmt.Sprintf("%s returned HTTP %d", operationName, response.StatusCode)),
	}
	if requestID := redactString(firstHeader(response.Header, "X-Request-ID", "Request-ID"), apiKey); requestID != "" {
		options = append(options, ecerrors.WithRequestID(requestID))
	}
	if c.backend == backendACS && operationName == "DeleteTemplate" &&
		(response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) &&
		strings.Contains(message, "Deleting SandboxSet-backed templates through the E2B API is not supported") {
		return ecerrors.Client("UnsupportedACSTemplateDeletion", e2bMessage("UnsupportedACSTemplateDeletion"), options...)
	}
	switch response.StatusCode {
	case http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity:
		return ecerrors.Client(code, message, options...)
	case http.StatusUnauthorized, http.StatusForbidden:
		options = append(options, ecerrors.WithSuggestion("Check that E2B_API_KEY is a valid project API key with access to this resource."))
		return ecerrors.Client(code, message, options...)
	case http.StatusNotFound:
		return ecerrors.NotFound(code, message, options...)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return ecerrors.Timeout(code, message, options...)
	case http.StatusTooManyRequests:
		return ecerrors.Service(code, message, true, options...)
	default:
		return ecerrors.Service(code, message, response.StatusCode >= 500, options...)
	}
}

func redactValue(value any, secret string) {
	if secret == "" {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			redactedKey := redactString(key, secret)
			if redactedKey != key {
				delete(typed, key)
				typed[redactedKey] = child
			}
			switch childTyped := child.(type) {
			case string:
				typed[redactedKey] = redactString(childTyped, secret)
			default:
				redactValue(childTyped, secret)
			}
		}
	case []any:
		for index, child := range typed {
			switch childTyped := child.(type) {
			case string:
				typed[index] = redactString(childTyped, secret)
			default:
				redactValue(childTyped, secret)
			}
		}
	}
}

func redactString(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

func expandPath(pattern string, request map[string]any) (string, error) {
	result := pattern
	for {
		start := strings.IndexByte(result, '{')
		if start < 0 {
			return result, nil
		}
		endRelative := strings.IndexByte(result[start:], '}')
		if endRelative < 0 {
			return "", ecerrors.Client("InvalidRequest", "invalid E2B operation path")
		}
		end := start + endRelative
		name := result[start+1 : end]
		value := stringValue(firstValue(request, "path."+name, name))
		if value == "" {
			return "", ecerrors.Client("MissingParameter", "missing required path parameter "+name, ecerrors.WithField(name))
		}
		result = result[:start] + url.PathEscape(value) + result[end+1:]
	}
}

func requestObject(request map[string]any, prefix string) map[string]any {
	if direct, ok := request[prefix]; ok {
		if object, ok := decodeJSONValue(direct).(map[string]any); ok {
			return cloneObject(object)
		}
	}
	flat := map[string]any{}
	needle := prefix + "."
	for key, value := range request {
		if strings.HasPrefix(key, needle) {
			flat[strings.TrimPrefix(key, needle)] = value
		}
	}
	root := map[string]any{}
	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		insertObject(root, strings.Split(key, "."), decodeJSONValue(flat[key]))
	}
	normalized, _ := normalizeObject(root).(map[string]any)
	return normalized
}

func insertObject(target map[string]any, path []string, value any) {
	if len(path) == 1 {
		target[path[0]] = value
		return
	}
	child, _ := target[path[0]].(map[string]any)
	if child == nil {
		child = map[string]any{}
		target[path[0]] = child
	}
	insertObject(child, path[1:], value)
}

func normalizeObject(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	indices := make([]int, 0, len(object))
	allNumeric := len(object) > 0
	for key := range object {
		index, err := strconv.Atoi(key)
		if err != nil || index < 1 {
			allNumeric = false
			break
		}
		indices = append(indices, index)
	}
	if allNumeric {
		sort.Ints(indices)
		items := make([]any, 0, len(indices))
		for _, index := range indices {
			items = append(items, normalizeObject(object[strconv.Itoa(index)]))
		}
		return items
	}
	for key, child := range object {
		object[key] = normalizeObject(child)
	}
	return object
}

func decodeJSONValue(value any) any {
	raw, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if decoder.Decode(&decoded) == nil {
		return decoded
	}
	return value
}

func encodeQuery(object map[string]any) url.Values {
	values := url.Values{}
	for key, value := range object {
		if value == nil {
			continue
		}
		if key == "metadata" {
			if pairs, ok := metadataQuery(value); ok {
				values.Set(key, pairs.Encode())
				continue
			}
		}
		switch typed := value.(type) {
		case []any:
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, stringValue(item))
			}
			values.Set(key, strings.Join(parts, ","))
		case []string:
			values.Set(key, strings.Join(typed, ","))
		default:
			values.Set(key, stringValue(value))
		}
	}
	return values
}

func metadataQuery(value any) (url.Values, bool) {
	pairs := url.Values{}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			pairs.Set(key, stringValue(item))
		}
	case []string:
		for _, item := range typed {
			key, value, ok := strings.Cut(item, "=")
			if !ok || key == "" {
				return nil, false
			}
			pairs.Set(key, value)
		}
	case []any:
		for _, raw := range typed {
			key, value, ok := strings.Cut(stringValue(raw), "=")
			if !ok || key == "" {
				return nil, false
			}
			pairs.Set(key, value)
		}
	default:
		return nil, false
	}
	return pairs, true
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func cloneObject(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = normalizeObject(decodeJSONValue(value))
	}
	return result
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}
