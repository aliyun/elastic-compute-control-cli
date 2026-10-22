package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCTemplate() bool {
	return c.Resource == "template" && strings.EqualFold(c.Product, "FCSandbox")
}

// A resource caller is created for one command. Keep the sensitive values for
// its later build probes too, since provider errors may echo registry inputs.
func (c *OpenAPICaller) ValidateOperation(_ context.Context, resource, action string, input map[string]any) error {
	if !c.isFCTemplate() || resource != "template" {
		return nil
	}
	noWait, _ := input["no_wait"].(bool)
	c.templateMu.Lock()
	c.templateWait = (action == "create" || action == "update") && !noWait
	c.templateCreatedID, c.templateCreatedTeam, c.templateCreatedRegion = "", "", ""
	c.templateMu.Unlock()
	c.rememberTemplateSecrets(input, false)
	return nil
}

func templateAbsenceCode(code string) bool { return code == "404" || code == "TemplateNotFound" }

func (c *OpenAPICaller) validateFCTemplateRequest(req *openAPIRequest) error {
	invalid := func(field string) error {
		return ecerrors.Client("InvalidFCTemplateConfig", i18n.NewLocalizer("en").Message("InvalidFCTemplateConfig"), ecerrors.WithField(field))
	}
	if id, exists := req.PathParams["templateID"]; exists && !validTeamIdentity(id) {
		return invalid("id")
	}
	if team := req.QueryParams["teamID"]; team != "" && !validTeamIdentity(team) {
		return invalid("team_id")
	}
	if req.ApiName != "CreateTemplate" && req.ApiName != "UpdateTemplate" {
		return nil
	}
	product, ok := OpenAPIProductByCode(c.Product, c.Profile.Language)
	if !ok {
		return invalid("body")
	}
	detail, ok := OpenAPIOperationDetailFor(c.Profile.Language, product, req.ApiName)
	if !ok {
		return invalid("body")
	}
	for index := range detail.Parameters {
		param := &detail.Parameters[index]
		if param.Name == "body" {
			if body, ok := req.BodyValue().(map[string]any); ok {
				if team, exists := body["teamID"]; exists {
					text, ok := team.(string)
					if !ok || !validTeamIdentity(text) {
						return invalid("team_id")
					}
				}
			}
			if field := invalidTemplateParameter(req.BodyValue(), param, "body"); field != "" {
				return invalid(field)
			}
			return nil
		}
	}
	return invalid("body")
}

// The CLI attaches this only to a failed create. Updates never suggest deleting
// a pre-existing template, and no recovery ID comes from a failed response.
func (c *OpenAPICaller) TemplateCreateRecovery(err error) error {
	if err == nil || !c.isFCTemplate() {
		return err
	}
	c.templateMu.Lock()
	id, team, region := c.templateCreatedID, c.templateCreatedTeam, c.templateCreatedRegion
	c.templateMu.Unlock()
	if !validTeamIdentity(id) || region == "" {
		return err
	}
	args := []string{"ecctl", "sandbox", "template", "delete", id, "--openapi", "--region", region}
	if team != "" {
		args = append(args, "--team-id", team)
	}
	if c.Profile.ProfileBound && c.Profile.Name != "" {
		args = append(args, "--profile", c.Profile.Name)
	}
	return ecerrors.WithDetails(err, ecerrors.WithRecoveryCommand(args...))
}

// Validate the actual API's referenced body tree without coercing JSON values.
// In particular PublicUpdateTemplateInput does not contain create-only steps.
func invalidTemplateParameter(value any, param *OpenAPIParameter, path string) string {
	if param == nil {
		return path
	}
	switch param.Type {
	case "Struct":
		object, ok := value.(map[string]any)
		if !ok {
			return path
		}
		for key, child := range object {
			// JSON keys are literal members, unlike raw OpenAPI dotted paths.
			var field *OpenAPIParameter
			for index := range param.SubParameters {
				if param.SubParameters[index].Name == key {
					field = &param.SubParameters[index]
					break
				}
			}
			if bad := invalidTemplateParameter(child, field, path+"."+key); bad != "" {
				return bad
			}
		}
		for _, field := range param.SubParameters {
			if field.Required {
				if _, ok := object[field.Name]; !ok {
					return path + "." + field.Name
				}
			}
		}
	case "RepeatList":
		items, ok := value.([]any)
		if !ok {
			return path
		}
		for _, item := range items {
			if bad := invalidTemplateParameter(item, param.Element, path); bad != "" {
				return bad
			}
		}
	case "String":
		if _, ok := value.(string); !ok {
			return path
		}
	case "Boolean":
		if _, ok := value.(bool); !ok {
			return path
		}
	case "Integer", "Float":
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return path
		}
		if param.Type == "Integer" && (math.Trunc(number) != number || number < math.MinInt32 || number > math.MaxInt32) {
			return path
		}
	default:
		return path
	}
	return ""
}

func (c *OpenAPICaller) fcTemplateResponseError(req *openAPIRequest, response map[string]any) error {
	requestID, _ := response["requestId"].(string)
	requestID = c.scrubTemplateRequestID(requestID)
	invalid := func() error {
		return ecerrors.Service("InvalidFCTemplateResponse", i18n.NewLocalizer("en").Message("InvalidNativeFCTemplateResponse"), false, ecerrors.WithRequestID(requestID))
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
		message, _ := response["message"].(string)
		if message == "" {
			message = i18n.NewLocalizer("en").Message("CloudAPIError")
		}
		message = callerSanitizeCloudError(errors.New(c.scrubTemplateText(message)))
		options := []ecerrors.Option{ecerrors.WithRequestID(requestID), ecerrors.WithRawCause(c.scrubTemplateCode(code), message)}
		if req.ApiName == "GetTemplate" && templateAbsenceCode(code) {
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("FCTemplateNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	validTemplate := func(template map[string]any, expectedID string) bool {
		id, _ := template["templateID"].(string)
		if !validTeamIdentity(id) || expectedID != "" && id != expectedID {
			return false
		}
		team, teamOK := template["teamID"].(string)
		if requested := req.QueryParams["teamID"]; requested != "" && team != requested {
			return false
		}
		if _, exists := template["teamID"]; exists && (!teamOK || !validTeamIdentity(team)) {
			return false
		}
		status, ok := template["status"].(map[string]any)
		if !ok {
			return false
		}
		state, ok := status["state"].(string)
		if !ok || strings.TrimSpace(state) == "" {
			return false
		}
		if runtime, exists := template["runtimeConfig"]; exists {
			if _, ok := runtime.(map[string]any); !ok {
				return false
			}
		}
		return true
	}
	switch req.ApiName {
	case "CreateTemplate", "UpdateTemplate":
		id, _ := response["templateID"].(string)
		if !validTeamIdentity(id) || req.ApiName == "UpdateTemplate" && id != req.PathParams["templateID"] {
			return invalid()
		}
	case "GetTemplate":
		if !validTemplate(response, req.PathParams["templateID"]) {
			return invalid()
		}
		status := response["status"].(map[string]any)
		state := status["state"].(string)
		c.templateMu.Lock()
		wait := c.templateWait
		c.templateMu.Unlock()
		if wait && (strings.EqualFold(state, "error") || strings.EqualFold(state, "failed")) {
			reason, _ := json.Marshal(c.scrubTemplateValue(status["reason"], false))
			return ecerrors.Service("FCTemplateBuildFailed", i18n.NewLocalizer("en").Message("FCTemplateBuildFailed"), false, ecerrors.WithRequestID(requestID), ecerrors.WithCurrentState(state), ecerrors.WithDetail(string(reason)))
		}
	case "ListTemplates":
		items, ok := response["templates"].([]any)
		if !ok {
			return invalid()
		}
		if token, exists := response["nextToken"]; exists {
			if _, ok := token.(string); !ok {
				return invalid()
			}
		}
		if size, exists := response["maxResults"]; exists {
			n, ok := teamPageInteger(size)
			if !ok || n < 1 || len(items) > n {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, item := range items {
			template, ok := item.(map[string]any)
			if !ok || !validTemplate(template, "") {
				return invalid()
			}
			id := template["templateID"].(string)
			if seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	}
	return nil
}

func templateSecretKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", "")) {
	case "password", "accesskeysecret", "securitytoken", "accesstoken", "refreshtoken":
		return true
	}
	return false
}

func (c *OpenAPICaller) rememberTemplateSecrets(value any, auth bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			c.rememberTemplateSecrets(child, auth || templateSecretKey(key) || strings.EqualFold(key, "authConfig"))
		}
	case []any:
		for _, child := range typed {
			c.rememberTemplateSecrets(child, auth)
		}
	case string:
		if auth && typed != "" {
			c.templateMu.Lock()
			defer c.templateMu.Unlock()
			for _, known := range c.templateSecrets {
				if known == typed {
					return
				}
			}
			c.templateSecrets = append(c.templateSecrets, typed)
		}
	}
}

var templateSecretAssignment = regexp.MustCompile(`(?i)(?:password|userName|accessKeySecret|securityToken|access[_-]?token|refresh[_-]?token)["']?\s*[:=]\s*`)

func scrubTemplateAssignments(value string) string {
	if templateSecretAssignment.MatchString(value) {
		// Free-form provider text supplies no trustworthy credential boundary,
		// even when quote characters occur inside an unquoted password.
		return "[REDACTED]"
	}
	return value
}

func (c *OpenAPICaller) scrubTemplateText(value string) string {
	// Parse assignments before a short remembered auth value can rewrite a
	// label such as password and hide an unknown stored credential from it.
	value = scrubTemplateAssignments(value)
	c.templateMu.Lock()
	secrets := append([]string(nil), c.templateSecrets...)
	c.templateMu.Unlock()
	// Provider descriptions may embed JSON text rather than the decoded value.
	// Replace all representations longest first so a shorter auth value cannot
	// split a longer escaped password before it is scrubbed.
	for _, secret := range secrets {
		encoded, _ := json.Marshal(secret)
		escaped := string(encoded[1 : len(encoded)-1])
		if escaped != secret {
			secrets = append(secrets, escaped)
		}
		if strings.ContainsAny(secret, "<>&") {
			var plain bytes.Buffer
			encoder := json.NewEncoder(&plain)
			encoder.SetEscapeHTML(false)
			_ = encoder.Encode(secret)
			quoted := strings.TrimSuffix(plain.String(), "\n")
			secrets = append(secrets, quoted[1:len(quoted)-1])
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

func (c *OpenAPICaller) scrubTemplateValue(value any, auth bool) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = c.scrubTemplateValue(child, auth || templateSecretKey(key) || strings.EqualFold(key, "authConfig"))
			if key == "requestId" || key == "code" || key == "message" {
				if text, ok := out[key].(string); ok && text != "" {
					out[key] = callerSanitizeCloudError(errors.New(text))
				}
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = c.scrubTemplateValue(child, auth)
		}
		return out
	case string:
		if auth {
			return "[REDACTED]"
		}
		return c.scrubTemplateText(typed)
	default:
		if auth {
			return "[REDACTED]"
		}
		return value
	}
}

func (c *OpenAPICaller) scrubTemplateError(err error) error {
	if err == nil {
		return nil
	}
	// Read typed SDK fields before changing free-form display text. Neither
	// classification nor action evidence is recovered from the scrubbed string.
	code, message, requestID := callerCloudErrorFields(err)
	return &nativeTemplateCloudError{code: c.scrubTemplateCode(code), message: c.scrubTemplateText(message), requestID: c.scrubTemplateRequestID(requestID)}
}

// Codes are provider-controlled text too. Callers classify the original error;
// only this sanitized copy enters public errors, actions and telemetry.
func (c *OpenAPICaller) scrubTemplateCode(value string) string {
	return callerSanitizeCloudError(errors.New(c.scrubTemplateText(value)))
}

type nativeTemplateCloudError struct{ code, message, requestID string }

func (e *nativeTemplateCloudError) Error() string {
	return "ErrorCode: " + e.code + "\nMessage: " + e.message + "\nRequestId: " + e.requestID
}

func (c *OpenAPICaller) scrubTemplateRequestID(value string) string {
	// Opaque request IDs are protocol metadata, not free-form descriptions.
	// A short registry username must not corrupt an unrelated ID substring.
	c.templateMu.Lock()
	defer c.templateMu.Unlock()
	normalizedID := strings.TrimSpace(value)
	for _, secret := range c.templateSecrets {
		// Header parsing and telemetry normalize surrounding whitespace. Match
		// that same form so normalization cannot resurrect a known secret.
		if value == secret || (normalizedID != "" && normalizedID == strings.TrimSpace(secret)) {
			return "[REDACTED]"
		}
	}
	return callerSanitizeCloudError(errors.New(scrubTemplateAssignments(value)))
}

func (c *OpenAPICaller) scrubTemplateResponse(req *openAPIRequest, response map[string]any) map[string]any {
	public := c.scrubTemplateValue(response, false).(map[string]any)
	// This runs after validation and allocation capture. Preserve only the
	// protocol members at their documented response/template locations.
	restore := func(raw, clean map[string]any) {
		for _, key := range []string{"code", "templateID", "teamID"} {
			if value, ok := raw[key]; ok {
				clean[key] = value
			}
		}
		if id, ok := raw["requestId"].(string); ok {
			clean["requestId"] = c.scrubTemplateRequestID(id)
		}
		if status, ok := raw["status"].(map[string]any); ok {
			if normalized, ok := clean["status"].(map[string]any); ok {
				state, _ := status["state"].(string)
				// Keep protocol tokens intact even when an auth value overlaps.
				// Unknown states retain their sanitized presentation.
				switch strings.ToLower(state) {
				case "building", "waiting", "ready", "error", "failed":
					normalized["state"] = state
				}
			}
		}
	}
	restore(response, public)
	if req.ApiName == "ListTemplates" {
		if token, ok := response["nextToken"].(string); ok {
			public["nextToken"] = token
		}
	}
	if templates, ok := response["templates"].([]any); ok {
		clean := public["templates"].([]any)
		for index, item := range templates {
			if raw, ok := item.(map[string]any); ok {
				if normalized, ok := clean[index].(map[string]any); ok {
					restore(raw, normalized)
				}
			}
		}
	}
	return public
}
