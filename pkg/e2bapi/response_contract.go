package e2bapi

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func hasResponseContract(name string) bool {
	switch name {
	case "GetTemplateBuildStatus", "GetTemplateBuildLogs", "ListTemplateTags", "GetSandbox":
		return true
	default:
		return false
	}
}

// Validate before normalization: an object cannot masquerade as a top-level
// array by returning our private marker, and missing fields cannot become [].
func decodeContractResponse(body io.Reader, name string, request map[string]any) (map[string]any, error) {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	value, err := decodeUnambiguousJSON(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON content")
	}
	invalid := errors.New("response does not match the operation contract")
	if name == "ListTemplateTags" {
		items, ok := value.([]any)
		if !ok || !validObjectArray(items, "tag", "buildID", "createdAt") {
			return nil, invalid
		}
		return map[string]any{"items": items, arrayMarker: true}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, invalid
	}
	switch name {
	case "GetSandbox":
		id, ok := object["sandboxID"].(string)
		requestedID := stringValue(firstValue(request, "path.sandboxID", "sandboxID"))
		if !ok || strings.TrimSpace(id) == "" || (requestedID != "" && id != requestedID) {
			return nil, invalid
		}
	case "GetTemplateBuildStatus":
		if !nonemptyStrings(object, "templateID", "buildID", "status") {
			return nil, invalid
		}
		buildID := stringValue(firstValue(request, "path.buildID", "buildID"))
		if buildID != "" && object["buildID"] != buildID {
			return nil, invalid
		}
		switch object["status"] {
		case "building", "waiting", "ready", "error":
		default:
			return nil, invalid
		}
		logs, ok := object["logs"].([]any)
		if !ok {
			return nil, invalid
		}
		for _, log := range logs {
			if _, ok := log.(string); !ok {
				return nil, invalid
			}
		}
		entries, ok := object["logEntries"].([]any)
		if !ok || !validObjectArray(entries, "timestamp", "message", "level") {
			return nil, invalid
		}
	case "GetTemplateBuildLogs":
		logs, ok := object["logs"].([]any)
		if !ok || !validObjectArray(logs, "timestamp", "message", "level") {
			return nil, invalid
		}
	}
	return object, nil
}

func nonemptyStrings(object map[string]any, fields ...string) bool {
	for _, field := range fields {
		value, ok := object[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func validObjectArray(items []any, fields ...string) bool {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return false
		}
		for _, field := range fields {
			if _, ok := object[field].(string); !ok {
				return false
			}
		}
	}
	return true
}
