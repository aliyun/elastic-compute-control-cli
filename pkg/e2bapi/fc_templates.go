package e2bapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

const fcTemplateTokenPrefix = "ecctl:fc-templates:v1:"
const acsTemplateTokenPrefix = "ecctl:acs-templates:v1:"

type fcTemplateCursor struct {
	Endpoint string `json:"endpoint"`
	After    string `json:"after"`
}

func e2bMessage(id string) string { return i18n.NewLocalizer("en").Message(id) }

func invalidFCToken() error {
	return ecerrors.Client("InvalidFCTemplateToken", e2bMessage("InvalidFCTemplateToken"), ecerrors.WithField("next_token"))
}

func invalidFCResponse() error {
	return ecerrors.Service("InvalidFCTemplateResponse", e2bMessage("InvalidFCTemplateResponse"), false)
}

func (c *Caller) templateEndpointFingerprint() string {
	u := *c.endpoint
	u.Host = strings.ToLower(u.Host)
	sum := sha256.Sum256([]byte(u.String()))
	return hex.EncodeToString(sum[:])
}

func (c *Caller) listTemplates(ctx context.Context, request map[string]any) (map[string]any, error) {
	query := requestObject(request, "query")
	token := stringValue(query["nextToken"])
	detected := c.detectBackend(ctx)
	if !detected.fc && !detected.acs {
		// A local FC cursor must never be sent to another backend. Native E2B
		// cursors remain opaque and are passed through without transformation.
		if strings.HasPrefix(token, "ecctl:acs-templates:") {
			return nil, ecerrors.Client("InvalidACSTemplateToken", e2bMessage("InvalidACSTemplateToken"), ecerrors.WithField("next_token"))
		}
		if strings.HasPrefix(token, "ecctl:fc-templates:") {
			return nil, invalidFCToken()
		}
		result, err := c.call(ctx, "ListTemplates", operations["ListTemplates"], request)
		if err != nil && detected.reason != "e2b_domain" && detected.reason != "explicit_e2b" {
			detail := i18n.NewLocalizer("en").MessageData("E2BBackendUnidentifiedDetail", map[string]any{"Host": redactString(c.endpoint.Hostname(), c.apiKey), "Reason": detected.reason})
			err = ecerrors.WithDetails(err, ecerrors.WithDetail(detail))
		}
		return result, err
	}
	tokenPrefix := fcTemplateTokenPrefix
	if detected.acs {
		tokenPrefix = acsTemplateTokenPrefix
	}
	limit := 100
	if raw, ok := query["limit"]; ok {
		parsed, err := strconv.Atoi(stringValue(raw))
		if err != nil || parsed < 1 || parsed > 100 {
			return nil, c.invalidTemplateLimit()
		}
		limit = parsed
	}
	cursor := fcTemplateCursor{Endpoint: c.templateEndpointFingerprint()}
	if token != "" {
		if !strings.HasPrefix(token, tokenPrefix) || len(token) > 8192 {
			return nil, c.invalidTemplateToken()
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, tokenPrefix))
		if err != nil {
			return nil, c.invalidTemplateToken()
		}
		var parsed fcTemplateCursor
		if json.Unmarshal(data, &parsed) != nil || parsed.Endpoint != cursor.Endpoint || parsed.After == "" {
			return nil, c.invalidTemplateToken()
		}
		cursor = parsed
	}
	// FC and ACS compatibility APIs list all templates. Do not send native v2
	// pagination controls, and do not silently truncate a server-paged response.
	delete(query, "limit")
	delete(query, "nextToken")
	result, err := c.call(ctx, "ListTemplates", operation{method: http.MethodGet, path: "/templates", completeArray: true}, map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	if result[arrayMarker] != true || stringValue(result["nextToken"]) != "" {
		return nil, c.invalidTemplateResponse()
	}
	items, ok := result["items"].([]any)
	if !ok {
		return nil, c.invalidTemplateResponse()
	}
	ids := make(map[string]bool, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, c.invalidTemplateResponse()
		}
		id, ok := item["templateID"].(string)
		if !ok || id == "" || len(id) > 1024 || ids[id] {
			return nil, c.invalidTemplateResponse()
		}
		ids[id] = true
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].(map[string]any)["templateID"].(string) < items[j].(map[string]any)["templateID"].(string)
	})
	start := sort.Search(len(items), func(i int) bool { return items[i].(map[string]any)["templateID"].(string) > cursor.After })
	end := min(start+limit, len(items))
	result["items"] = items[start:end]
	if end < len(items) {
		cursor.After = items[end-1].(map[string]any)["templateID"].(string)
		data, _ := json.Marshal(cursor)
		result["nextToken"] = tokenPrefix + base64.RawURLEncoding.EncodeToString(data)
	}
	return result, nil
}

// Decode before normalization can overwrite duplicate keys or accept an
// object containing the internal array marker. A second JSON document is
// also invalid: silently ignoring it could lose templates.
func decodeCompleteJSONArray(body io.Reader) ([]any, error) {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	value, err := decodeUnambiguousJSON(decoder, 0)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected JSON array")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON content")
	}
	return items, nil
}

func decodeUnambiguousJSON(decoder *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, errors.New("JSON nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("invalid JSON object key")
			}
			if _, exists := object[name]; exists {
				return nil, errors.New("duplicate JSON object key")
			}
			value, err := decodeUnambiguousJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case json.Delim('['):
		array := []any{}
		for decoder.More() {
			value, err := decodeUnambiguousJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return token, nil
	}
}

func (c *Caller) invalidTemplateToken() error {
	if c.backend == backendACS {
		return ecerrors.Client("InvalidACSTemplateToken", e2bMessage("InvalidACSTemplateToken"), ecerrors.WithField("next_token"))
	}
	return invalidFCToken()
}

func (c *Caller) invalidTemplateResponse() error {
	if c.backend == backendACS {
		return ecerrors.Service("InvalidACSTemplateResponse", e2bMessage("InvalidACSTemplateResponse"), false)
	}
	return invalidFCResponse()
}

func (c *Caller) invalidTemplateLimit() error {
	if c.backend == backendACS {
		return ecerrors.Client("InvalidACSTemplateLimit", e2bMessage("InvalidACSTemplateLimit"), ecerrors.WithField("limit"))
	}
	return ecerrors.Client("InvalidFCTemplateLimit", e2bMessage("InvalidFCTemplateLimit"), ecerrors.WithField("limit"))
}
