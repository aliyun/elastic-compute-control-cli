package exec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const maxEvidenceJSONDepth = 64

// DecodeStrictJSON accepts exactly one recovery JSON value and rejects object
// members that are duplicates or would alias under Go's struct field matching.
func DecodeStrictJSON(data []byte) (any, error) {
	return decodeJSON(data, true)
}

// DecodeCommandJSON retains distinct case-sensitive keys in ordinary command
// results. Recovery receipts use DecodeStrictJSON before typed decoding.
func DecodeCommandJSON(data []byte) (any, error) {
	return decodeJSON(data, false)
}

func decodeJSON(data []byte, foldKeys bool) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := strictEvidenceValue(decoder, 0, foldKeys)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

// UnmarshalStrictJSON validates the original bytes before decoding into the
// caller's expected journal or sidecar shape.
func UnmarshalStrictJSON(data []byte, destination any) error {
	if _, err := DecodeStrictJSON(data); err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}

func strictEvidenceValue(decoder *json.Decoder, depth int, foldKeys bool) (any, error) {
	if depth > maxEvidenceJSONDepth {
		return nil, fmt.Errorf("JSON nesting exceeds the recovery limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]any)
		folded := make(map[string]bool)
		for decoder.More() {
			member, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := member.(string)
			if !ok {
				return nil, fmt.Errorf("invalid JSON member")
			}
			alias := key
			if foldKeys {
				alias = foldEvidenceKey(key)
			}
			if folded[alias] {
				return nil, fmt.Errorf("ambiguous JSON member")
			}
			folded[alias] = true
			object[key], err = strictEvidenceValue(decoder, depth+1, foldKeys)
			if err != nil {
				return nil, err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("invalid JSON object end: %v", err)
		}
		return object, nil
	case json.Delim('['):
		array := make([]any, 0)
		for decoder.More() {
			item, err := strictEvidenceValue(decoder, depth+1, foldKeys)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("invalid JSON array end: %v", err)
		}
		return array, nil
	default:
		return token, nil
	}
}

func foldEvidenceKey(key string) string {
	var folded strings.Builder
	folded.Grow(len(key))
	for _, initial := range key {
		minimum := initial
		for next := unicode.SimpleFold(initial); next != initial; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		folded.WriteRune(minimum)
	}
	return folded.String()
}
