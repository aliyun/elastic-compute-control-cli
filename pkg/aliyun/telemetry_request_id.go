package aliyun

import (
	"errors"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

// telemetryRequestIDContextKey carries a per-attempt response-header capture.
type telemetryRequestIDContextKey struct{}

// Only the matching request may write the capture. Credential acquisition can
// execute nested STS requests with the same context before the business request.
type telemetryRequestIDCapture struct {
	request          *openAPIRequest
	responseObserved bool
	headerRequestID  string
}

func telemetryRequestIDForRequest(request *openAPIRequest, response map[string]any, err error, capture *telemetryRequestIDCapture) string {
	responseObserved := capture != nil && capture.request == request && capture.responseObserved
	if responseObserved {
		if requestID := capture.headerRequestID; requestID != "" {
			return requestID
		}
	}
	if request == nil || len(request.responseRequestIDFields) == 0 {
		return ""
	}
	// Credential resolution can return an SDK error from an internal STS call
	// before the business transport runs. Only this request's HTTP response can
	// establish that a structured error ID belongs to the business request.
	if responseObserved {
		if requestID := telemetryErrorRequestID(err); requestID != "" {
			return requestID
		}
	}
	return telemetryResponseRequestID(response, request.responseRequestIDFields...)
}

func telemetryErrorRequestID(err error) string {
	if err != nil {
		var sdkError interface{ GetRequestId() *string }
		if errors.As(err, &sdkError) {
			if value := sdkError.GetRequestId(); value != nil {
				if requestID := validTelemetryRequestID(*value); requestID != "" {
					return requestID
				}
			}
			var sdkData interface{ GetData() map[string]interface{} }
			if errors.As(err, &sdkData) {
				if requestID := telemetryResponseRequestID(sdkData.GetData()); requestID != "" {
					return requestID
				}
			}
			// SDK Error() includes the server's free-form Message. A label in
			// that text is not trusted request metadata.
			return ""
		}
		var appError *ecerrors.AppError
		if errors.As(err, &appError) {
			return validTelemetryRequestID(ecerrors.ActionFromError("", appError).RequestID)
		}
		var templateError *nativeTemplateCloudError
		if errors.As(err, &templateError) {
			return validTelemetryRequestID(templateError.requestID)
		}
	}
	return ""
}

func telemetryResponseRequestID(response map[string]any, fields ...string) string {
	if len(fields) == 0 {
		fields = []string{"RequestId", "requestId", "RequestID", "requestID", "request_id", "requestid"}
	}
	for _, key := range fields {
		if value, ok := response[key].(string); ok {
			if requestID := validTelemetryRequestID(value); requestID != "" {
				return requestID
			}
		}
	}
	return ""
}

// Keep only a bounded opaque identifier, never a formatted error or payload
// accidentally placed in an ID field or appended to an error label.
func validTelemetryRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 256 || strings.Contains(value, "://") {
		return ""
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("-._:/+=", character) {
			continue
		}
		return ""
	}
	return value
}
