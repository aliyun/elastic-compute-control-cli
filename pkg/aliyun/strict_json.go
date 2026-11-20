package aliyun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeStrictJSON rejects ambiguous object structure before SDK or caller
// normalization can discard an earlier member of a provider proof envelope.
const maxStrictJSONDepth = 64

func decodeStrictJSON(raw []byte) (any, error) {
	return decodeStrictJSONWithNumbers(raw, false)
}

func decodeStrictJSONWithNumbers(raw []byte, preserveNumbers bool) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if preserveNumbers {
		decoder.UseNumber()
	}
	value, err := strictJSONValue(decoder, 0)
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

// DecodeStrictJSONWithNumbers keeps literal JSON members and numbers while
// rejecting duplicate members at every object depth.
func DecodeStrictJSONWithNumbers(raw []byte) (any, error) {
	return decodeStrictJSONWithNumbers(raw, true)
}

func strictJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxStrictJSONDepth {
		return nil, fmt.Errorf("JSON nesting exceeds the supported depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid JSON object member")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate JSON object member")
			}
			object[key], err = strictJSONValue(decoder, depth+1)
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
			item, err := strictJSONValue(decoder, depth+1)
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
