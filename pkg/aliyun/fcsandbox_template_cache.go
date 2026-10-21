package aliyun

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCSandboxTemplateCache() bool {
	return c.Resource == "template-cache" && strings.EqualFold(c.Product, "FCSandbox")
}

func templateCacheAbsenceCode(code string) bool {
	return code == "TemplateCacheNotFound" || code == "404"
}

// Keep response diagnostics to JSON type names from this closed set. Provider
// values and arbitrary field names must not become part of an error payload.
func templateCacheJSONKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32, int, int32, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

var templateCacheCredentialAssignment = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(?:access[_-]?key[_-]?(?:id|secret)|security[_-]?token|sts[_-]?token|bearer[_-]?token|oauth[_-]?(?:access|refresh)[_-]?token|(?:access|refresh|id|auth|session)[_-]?token|signature(?:nonce)?|sig|authorization|password|passwd|pwd|token|api[_-]?key|client[_-]?secret)["']?\s*[:=]`)
var templateCacheBareAuthorization = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(?:bearer|basic)\s+\S+`)
var templateCacheRequestID = regexp.MustCompile(`^(?:req-[a-z0-9-]{1,48}|[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12})$`)

func sanitizeTemplateCacheDiagnostic(value string) string {
	// Provider diagnostics do not delimit unquoted values. Partial replacement
	// can leave a secret suffix behind, so discard the whole diagnostic.
	if strings.Contains(value, "[REDACTED_CREDENTIAL]") || templateCacheCredentialAssignment.MatchString(value) || templateCacheBareAuthorization.MatchString(value) {
		return "[REDACTED]"
	}
	return callerSanitizeCloudError(errors.New(value))
}

func sanitizeTemplateCacheRequestID(value string) string {
	value = validTelemetryRequestID(sanitizeTemplateCacheDiagnostic(value))
	if !templateCacheRequestID.MatchString(value) || !safeTemplateCacheTokenContent(value) {
		return ""
	}
	return value
}

func safeTemplateCacheTokenContent(token string) bool {
	lower := strings.ToLower(token)
	for _, prefix := range []string{"ltai", "akia", "asia"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	if len(token) >= 16 && strings.IndexAny(token, "0123456789") >= 0 && !strings.Contains(token, "-") {
		return false
	}
	for _, marker := range []string{"token", "secret", "password", "passwd", "auth", "key", "credential", "signature", "bearer", "oauth", "sts", "jwt"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func scrubTemplateCacheResponse(response map[string]any, api string) map[string]any {
	scrubCache := func(value any, allowPendingAlias bool) any {
		cache, ok := value.(map[string]any)
		if !ok {
			return value
		}
		reason, hasReason := cache["statusReason"].(string)
		status, _ := cache["status"].(string)
		if !hasReason && status != "ready" && !(allowPendingAlias && status == "creating") {
			return value
		}
		clean := make(map[string]any, len(cache))
		for key, item := range cache {
			clean[key] = item
		}
		if hasReason {
			clean["statusReason"] = sanitizeTemplateCacheDiagnostic(reason)
		}
		if status == "ready" {
			// The provider also returns this success state outside its published enum.
			clean["status"] = "Success"
		} else if allowPendingAlias && status == "creating" {
			// Create and Describe can both return this pending state.
			clean["status"] = "InProgress"
		}
		return clean
	}
	clean := make(map[string]any, len(response))
	for key, value := range response {
		clean[key] = value
	}
	for _, key := range []string{"code", "message"} {
		if value, ok := clean[key].(string); ok {
			clean[key] = sanitizeTemplateCacheDiagnostic(value)
		}
	}
	if value, ok := clean["requestId"].(string); ok {
		clean["requestId"] = sanitizeTemplateCacheRequestID(value)
	}
	if cache, ok := clean["templateCache"]; ok {
		clean["templateCache"] = scrubCache(cache, api == "CreateTemplateCache" || api == "DescribeTemplateCache")
	}
	if items, ok := clean["templateCaches"].([]any); ok {
		scrubbed := make([]any, len(items))
		for index, item := range items {
			scrubbed[index] = scrubCache(item, false)
		}
		clean["templateCaches"] = scrubbed
	}
	return clean
}

func (c *OpenAPICaller) validateTemplateCacheRequest(req *openAPIRequest) error {
	invalid := func(field string) error {
		return ecerrors.Client("InvalidTemplateCacheInput", i18n.NewLocalizer("en").Message("InvalidTemplateCacheInput"), ecerrors.WithField(field))
	}
	if id := req.PathParams["templateID"]; id != "" && !validTeamIdentity(id) {
		return invalid("id")
	}
	for _, field := range []string{"teamID", "templateID"} {
		if value := req.QueryParams[field]; value != "" && !validTeamIdentity(value) {
			return invalid(field)
		}
	}
	if req.ApiName == "CreateTemplateCache" {
		body, ok := req.BodyValue().(map[string]any)
		if !ok {
			return invalid("body")
		}
		id, _ := body["templateID"].(string)
		if !validTeamIdentity(id) {
			return invalid("template_id")
		}
		if team, exists := body["teamID"]; exists {
			text, ok := team.(string)
			if !ok || !validTeamIdentity(text) {
				return invalid("team_id")
			}
		}
	}
	if req.ApiName == "ListTemplateCache" {
		if status := req.QueryParams["status"]; status != "" {
			switch status {
			case "InProgress", "Success", "Failed", "Deleting", "Evicted":
			default:
				return invalid("status")
			}
		}
		if size := req.QueryParams["maxResults"]; size != "" {
			n, err := strconv.Atoi(size)
			if err != nil || n < 1 || n > 100 {
				return invalid("limit")
			}
		}
	}
	return nil
}

func (c *OpenAPICaller) fcTemplateCacheResponseError(req *openAPIRequest, response map[string]any) error {
	requestID, _ := response["requestId"].(string)
	requestID = sanitizeTemplateCacheRequestID(requestID)
	invalid := func(field, reason string) error {
		return ecerrors.Service("InvalidTemplateCacheResponse", i18n.NewLocalizer("en").Message("InvalidTemplateCacheResponse"), false, ecerrors.WithField(field), ecerrors.WithDetail(reason), ecerrors.WithRequestID(requestID))
	}
	invalidType := func(field, expected string, value any) error {
		return invalid(field, "expected_"+expected+"_got_"+templateCacheJSONKind(value))
	}
	if value, exists := response["requestId"]; exists {
		if _, ok := value.(string); !ok {
			return invalidType("requestId", "string", value)
		}
	}
	codeValue, exists := response["code"]
	if !exists {
		return invalid("code", "missing")
	}
	code, ok := codeValue.(string)
	if !ok {
		return invalidType("code", "string", codeValue)
	}
	if code == "" {
		return invalid("code", "empty")
	}
	if code != "200" {
		message, _ := response["message"].(string)
		if message == "" {
			message = i18n.NewLocalizer("en").Message("CloudAPIError")
		}
		message = sanitizeTemplateCacheDiagnostic(message)
		code = sanitizeTemplateCacheDiagnostic(code)
		options := []ecerrors.Option{ecerrors.WithRequestID(requestID), ecerrors.WithRawCause(code, message)}
		if req.ApiName == "DescribeTemplateCache" && templateCacheAbsenceCode(code) {
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("TemplateCacheNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	requestedID, requestedTeam := req.PathParams["templateID"], req.QueryParams["teamID"]
	requestedStatus := ""
	if req.ApiName == "CreateTemplateCache" {
		body, ok := req.BodyValue().(map[string]any)
		if !ok {
			return invalid("body", "invalid_request_shape")
		}
		requestedID, _ = body["templateID"].(string)
		requestedTeam, _ = body["teamID"].(string)
	} else if req.ApiName == "ListTemplateCache" {
		requestedID = req.QueryParams["templateID"]
		requestedStatus = req.QueryParams["status"]
	}
	validCache := func(value any, root string) (string, string, error) {
		cache, ok := value.(map[string]any)
		if !ok {
			return "", "", invalidType(root, "object", value)
		}
		cacheString := func(name string) (string, error) {
			field := root + "." + name
			item, exists := cache[name]
			if !exists {
				return "", invalid(field, "missing")
			}
			text, ok := item.(string)
			if !ok {
				return "", invalidType(field, "string", item)
			}
			return text, nil
		}
		id, err := cacheString("templateID")
		if err != nil {
			return "", "", err
		}
		if !validTeamIdentity(id) {
			return "", "", invalid(root+".templateID", "invalid_identity")
		}
		if requestedID != "" && requestedID != id {
			return "", "", invalid(root+".templateID", "identity_mismatch")
		}
		team, err := cacheString("teamID")
		if err != nil {
			return "", "", err
		}
		if !validTeamIdentity(team) {
			return "", "", invalid(root+".teamID", "invalid_identity")
		}
		if requestedTeam != "" && requestedTeam != team {
			return "", "", invalid(root+".teamID", "identity_mismatch")
		}
		state, err := cacheString("status")
		if err != nil {
			return "", "", err
		}
		switch state {
		case "InProgress", "Success", "Failed", "Deleting", "Evicted":
		default:
			return "", "", invalid(root+".status", "invalid_enum")
		}
		if requestedStatus != "" && requestedStatus != state {
			return "", "", invalid(root+".status", "filter_mismatch")
		}
		for _, field := range []string{"statusReason", "imageDigest", "createdTime", "readyTime"} {
			if value, exists := cache[field]; exists {
				if _, ok := value.(string); !ok {
					return "", "", invalidType(root+"."+field, "string", value)
				}
			}
		}
		for _, field := range []string{"progress", "imageSizeMB"} {
			if value, exists := cache[field]; exists {
				n, ok := teamPageInteger(value)
				if !ok {
					if templateCacheJSONKind(value) != "number" {
						return "", "", invalidType(root+"."+field, "nonnegative_integer", value)
					}
					return "", "", invalid(root+"."+field, "invalid_integer")
				}
				if field == "progress" && n > 100 {
					return "", "", invalid(root+"."+field, "out_of_range")
				}
			}
		}
		return id, team, nil
	}
	switch req.ApiName {
	case "CreateTemplateCache", "DescribeTemplateCache":
		value, exists := response["templateCache"]
		if !exists {
			return invalid("templateCache", "missing")
		}
		if _, _, err := validCache(value, "templateCache"); err != nil {
			return err
		}
	case "ListTemplateCache":
		value, exists := response["templateCaches"]
		if !exists {
			return invalid("templateCaches", "missing")
		}
		items, ok := value.([]any)
		if !ok {
			return invalidType("templateCaches", "array", value)
		}
		if requested := req.QueryParams["maxResults"]; requested != "" {
			limit, err := strconv.Atoi(requested)
			if err != nil || limit < 1 || limit > 100 {
				return invalid("maxResults", "invalid_request_shape")
			}
			if len(items) > limit {
				return invalid("templateCaches", "over_requested_limit")
			}
		}
		if token, exists := response["nextToken"]; exists {
			text, ok := token.(string)
			if !ok {
				return invalidType("nextToken", "string", token)
			}
			if text != "" && text == req.QueryParams["nextToken"] {
				return invalid("nextToken", "repeated_token")
			}
		}
		if size, exists := response["maxResults"]; exists {
			n, ok := teamPageInteger(size)
			if !ok {
				if templateCacheJSONKind(size) != "number" {
					return invalidType("maxResults", "positive_integer", size)
				}
				return invalid("maxResults", "invalid_integer")
			}
			if n < 1 || n > 100 || len(items) > n {
				return invalid("maxResults", "out_of_range")
			}
			if requested := req.QueryParams["maxResults"]; requested != "" && requested != strconv.Itoa(n) {
				return invalid("maxResults", "request_mismatch")
			}
		}
		seen := map[string]bool{}
		for _, item := range items {
			id, team, err := validCache(item, "templateCaches[]")
			if err != nil {
				return err
			}
			if seen[team+"/"+id] {
				return invalid("templateCaches", "duplicate_identity")
			}
			seen[team+"/"+id] = true
		}
	}
	return nil
}
