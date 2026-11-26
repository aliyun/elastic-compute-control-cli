package aliyun

import (
	"errors"
	"strconv"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCSandboxAPIKey() bool {
	return c.Resource == "api-key" && strings.EqualFold(c.Product, "FCSandbox")
}

func apiKeyAbsenceCode(code string) bool {
	return code == "ApiKeyNotFound" || code == "404"
}

// Only create and reset may return a plaintext key, and only when the caller
// explicitly requests it. Every other operation always masks the value.
func apiKeyRevealOperation(api string) bool {
	return api == "CreateApiKey" || api == "ResetApiKey"
}

// extractAPIKeyReveal removes the caller-consumed ShowValue control before the
// request is built so it never reaches the wire, and reports whether the
// plaintext key was explicitly requested.
func extractAPIKeyReveal(request map[string]any) (map[string]any, bool) {
	if _, present := request["ShowValue"]; !present {
		return request, false
	}
	reveal := callerBoolMapValue(request, "ShowValue")
	clean := make(map[string]any, len(request)-1)
	for key, value := range request {
		if key != "ShowValue" {
			clean[key] = value
		}
	}
	return clean, reveal
}

// scrubAPIKeyResponse removes the plaintext apiKeyValue unless the operation
// may reveal it and the caller explicitly requested it. The masked display
// value is always retained.
func scrubAPIKeyResponse(response map[string]any, api string, reveal bool) map[string]any {
	if response == nil {
		return nil
	}
	keep := reveal && apiKeyRevealOperation(api)
	clean := make(map[string]any, len(response))
	for key, value := range response {
		clean[key] = value
	}
	strip := func(value any) any {
		key, ok := value.(map[string]any)
		if !ok {
			return value
		}
		if _, present := key["apiKeyValue"]; !present {
			return value
		}
		scrubbed := make(map[string]any, len(key))
		for field, item := range key {
			if field != "apiKeyValue" || keep {
				scrubbed[field] = item
			}
		}
		return scrubbed
	}
	if key, ok := clean["apiKey"]; ok {
		clean["apiKey"] = strip(key)
	}
	if keys, ok := clean["apiKeys"].([]any); ok {
		copies := make([]any, len(keys))
		for index, key := range keys {
			copies[index] = strip(key)
		}
		clean["apiKeys"] = copies
	}
	return clean
}

// The ROA API Key API uses a lower-case business envelope even on HTTP 200.
// A successful HTTP status alone must never turn a failed mutation into a
// successful CLI result.
func (c *OpenAPICaller) fcApiKeyResponseError(req *openAPIRequest, response map[string]any) error {
	requestID, _ := response["requestId"].(string)
	requestID = callerSanitizeCloudError(errors.New(requestID))
	invalid := func() error {
		return ecerrors.Service("InvalidApiKeyResponse", i18n.NewLocalizer("en").Message("InvalidApiKeyResponse"), false, ecerrors.WithRequestID(requestID))
	}
	if value, present := response["requestId"]; present {
		if _, ok := value.(string); !ok {
			return invalid()
		}
	}
	code, ok := response["code"].(string)
	if !ok || code == "" {
		return invalid()
	}
	if code != "200" {
		code = callerSanitizeCloudError(errors.New(code))
		message, _ := response["message"].(string)
		if message == "" {
			message = i18n.NewLocalizer("en").Message("CloudAPIError")
		}
		message = callerSanitizeCloudError(errors.New(message))
		options := []ecerrors.Option{ecerrors.WithRequestID(requestID), ecerrors.WithRawCause(code, message)}
		if req.ApiName == "DescribeApiKey" && apiKeyAbsenceCode(code) {
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("ApiKeyNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	validKey := func(value any, expectedID string) (string, bool) {
		key, ok := value.(map[string]any)
		if !ok {
			return "", false
		}
		id, _ := key["apiKeyID"].(string)
		status, _ := key["status"].(string)
		if !validTeamIdentity(id) || strings.TrimSpace(status) == "" || expectedID != "" && id != expectedID {
			return "", false
		}
		for _, field := range []string{"apiKeyName", "apiKeyMask", "apiKeyValue", "teamID", "teamName", "teamPlan", "resourceGroupID", "userID", "username", "source", "createdTime", "expireTime", "lastUsedTime"} {
			if item, present := key[field]; present {
				if _, ok := item.(string); !ok {
					return "", false
				}
			}
		}
		return id, true
	}
	switch req.ApiName {
	case "CreateApiKey", "ResetApiKey":
		if _, ok := validKey(response["apiKey"], ""); !ok {
			return invalid()
		}
	case "DescribeApiKey", "UpdateApiKey":
		if _, ok := validKey(response["apiKey"], req.PathParams["apiKeyID"]); !ok {
			return invalid()
		}
	case "ListApiKeys":
		items, ok := response["apiKeys"].([]any)
		if !ok {
			return invalid()
		}
		total, totalOK := teamPageInteger(response["total"])
		page, pageOK := teamPageInteger(response["pageNumber"])
		size, sizeOK := teamPageInteger(response["pageSize"])
		if !totalOK || !pageOK || !sizeOK || page < 1 || size < 1 || total < len(items) || len(items) > size {
			return invalid()
		}
		for field, value := range map[string]int{"pageNumber": page, "pageSize": size} {
			if requested := req.QueryParams[field]; requested != "" && requested != strconv.Itoa(value) {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, item := range items {
			id, valid := validKey(item, "")
			if !valid || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	case "DeleteApiKey":
		// A synchronous delete returns only the business envelope.
	}
	return nil
}
