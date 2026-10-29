package aliyun

import (
	"errors"
	"math"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCSandboxQuota() bool {
	return c.Resource == "quota" && strings.EqualFold(c.Product, "FCSandbox")
}

func quotaAbsenceCode(code string) bool {
	return code == "ResourceQuotaNotFound" || code == "QuotaNotFound" || code == "404"
}

// The ROA Quota API uses a lower-case business envelope even on HTTP 200.
// A successful HTTP status alone must never turn a failed update or delete
// into a successful CLI result.
func (c *OpenAPICaller) fcQuotaResponseError(req *openAPIRequest, response map[string]any) error {
	requestID, _ := response["requestId"].(string)
	requestID = callerSanitizeCloudError(errors.New(requestID))
	invalid := func() error {
		return ecerrors.Service("InvalidQuotaResponse", i18n.NewLocalizer("en").Message("InvalidQuotaResponse"), false, ecerrors.WithRequestID(requestID))
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
		if req.ApiName == "DescribeQuota" && quotaAbsenceCode(code) {
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("QuotaNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	validQuota := func(value any, expectedID string) (string, bool) {
		quota, ok := value.(map[string]any)
		if !ok {
			return "", false
		}
		id, _ := quota["tagValue"].(string)
		if !validTeamIdentity(id) || expectedID != "" && id != expectedID {
			return "", false
		}
		for _, field := range []string{"cpuCores", "memoryGB"} {
			number, ok := quota[field].(float64)
			if !ok || number < 0 || number > math.MaxInt32 || math.Trunc(number) != number {
				return "", false
			}
		}
		return id, true
	}
	switch req.ApiName {
	case "DescribeQuota":
		if _, ok := validQuota(response["quota"], req.QueryParams["tagValue"]); !ok {
			return invalid()
		}
	case "UpdateQuota":
		body, ok := req.BodyValue().(map[string]any)
		if !ok {
			return invalid()
		}
		id, _ := body["tagValue"].(string)
		if _, ok := validQuota(response["quota"], id); !ok {
			return invalid()
		}
	case "ListQuota":
		items, ok := response["quotas"].([]any)
		if !ok {
			return invalid()
		}
		if token, present := response["nextToken"]; present {
			if _, ok := token.(string); !ok {
				return invalid()
			}
		}
		if size, present := response["maxResults"]; present {
			number, ok := size.(float64)
			if !ok || number < 1 || number > math.MaxInt32 || math.Trunc(number) != number || len(items) > int(number) {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, item := range items {
			id, valid := validQuota(item, "")
			if !valid || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	case "DeleteQuota":
		// A synchronous delete returns only the business envelope.
	}
	return nil
}
