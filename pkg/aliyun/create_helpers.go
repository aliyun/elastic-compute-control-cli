package aliyun

import (
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"strconv"
	"strings"
	"time"

	ecerrors "ecctl/pkg/errors"
)

type tagAssignment struct {
	Key   string
	Value string
}

func clientToken(prefix string, _ any) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(raw[:])
	}
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func parseTagAssignments(tags []string) ([]tagAssignment, error) {
	assignments := make([]tagAssignment, 0, len(tags))
	for _, tag := range tags {
		key, value, ok := strings.Cut(tag, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			return nil, invalidTagError()
		}
		assignments = append(assignments, tagAssignment{Key: key, Value: value})
	}
	return assignments, nil
}

func isDryRunPassed(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.TrimSpace(err.Error())
	code, message, _ := ecerrors.ParseCloudError(raw)
	if strings.EqualFold(strings.TrimSpace(code), "DryRunOperation") {
		return true
	}
	lowerRaw := strings.ToLower(raw)
	if lowerRaw == "dryrunoperation" || strings.HasPrefix(lowerRaw, "dryrunoperation:") {
		return true
	}
	return isDryRunValidationPassed(raw) ||
		isDryRunValidationPassed(code) ||
		(strings.TrimSpace(code) == "400" && isDryRunValidationPassed(message))
}

func isDryRunValidationPassed(value string) bool {
	const passed = "request validation has been passed with dryrun flag set"
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSpace(strings.TrimPrefix(value, "code:"))
	value = strings.TrimSpace(strings.TrimPrefix(value, "400,"))
	if index := strings.LastIndex(value, "request id:"); index >= 0 {
		if strings.TrimSpace(value[index+len("request id:"):]) == "" {
			return false
		}
		value = strings.TrimSpace(value[:index])
	}
	value = strings.TrimSuffix(value, ".")
	return strings.TrimSpace(value) == passed
}

func isAppNotFound(err error) bool {
	var appErr *ecerrors.AppError
	return stderrors.As(err, &appErr) && appErr.Payload().Code == "NotFound"
}

func isCloudNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "notfound") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "not exist")
}

func isDependencyViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "dependency") ||
		strings.Contains(message, "dependent") ||
		strings.Contains(message, "in use") ||
		strings.Contains(message, "inuse")
}

// isThrottling reports whether err is an Alibaba Cloud throttling / flow-control
// response, which is transient and safe to retry with backoff. It matches the
// Throttling* error-code family as well as the common flow-control messages.
func isThrottling(err error) bool {
	if err == nil {
		return false
	}
	code, _, _ := ecerrors.ParseCloudError(err.Error())
	if strings.Contains(strings.ToLower(code), "throttl") {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "throttl") ||
		strings.Contains(message, "flow control") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "request was denied due to")
}

// isTransientNetworkError reports whether err is a transient transport-level
// failure (connection reset, unexpected EOF, timeout) that is safe to retry on
// read-only calls. Unlike throttling, the server may have partially processed
// the request, so callers must gate this to idempotent operations.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "i/o timeout") ||
		strings.Contains(message, "tls handshake timeout") ||
		strings.Contains(message, "client.timeout exceeded") ||
		strings.Contains(message, "no such host") ||
		strings.Contains(message, "eof")
}

// isReadOperation reports whether operation is a side-effect-free query, so a
// transient network error can be retried without risking duplicate writes.
func isReadOperation(operation string) bool {
	return strings.HasPrefix(operation, "Describe") ||
		strings.HasPrefix(operation, "List") ||
		strings.HasPrefix(operation, "Get") ||
		strings.HasPrefix(operation, "Query") ||
		strings.HasPrefix(operation, "Check")
}

func invalidTagError() error {
	return ecerrors.Client("InvalidTag", "--tag must be key=value")
}
