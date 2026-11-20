package aliyun

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCSandboxVolume() bool {
	return c.Resource == "volume" && strings.EqualFold(c.Product, "FCSandbox")
}

// Text parsed from an arbitrary error is a diagnostic, not proof that the
// provider returned a Volume-specific absence code.
func volumeStructuredTransportAbsence(err error, code, requestID string) bool {
	return volumeStructuredErrorProofWithEnvelope(err, 404, code, "VolumeNotFound", requestID, volume404ErrorEnvelopeProof) ||
		volumeStructuredErrorProof(err, 400, code, "VolumeNotFound", requestID)
}

func volumeStructuredCreateConflict(err error, code, requestID string) bool {
	return volumeStructuredErrorProof(err, 409, code, "VolumeConflict", requestID)
}

func ramFixtureStructuredAbsence(err error, operation, code, requestID string) bool {
	wantCode := map[string]string{"GetRole": "EntityNotExist.Role", "DeleteRole": "EntityNotExist.Role", "GetPolicy": "EntityNotExist.Policy", "DeletePolicy": "EntityNotExist.Policy"}[operation]
	return wantCode != "" && volumeStructuredErrorProof(err, 404, code, wantCode, requestID)
}

func volumeStructuredErrorProof(err error, wantStatus int, code, wantCode, requestID string) bool {
	return volumeStructuredErrorProofWithEnvelope(err, wantStatus, code, wantCode, requestID, volumeErrorEnvelopeProof)
}

func volumeStructuredErrorProofWithEnvelope(err error, wantStatus int, code, wantCode, requestID string, envelopeProof func(map[string]any, string, string) bool) bool {
	var unverified *unverifiedProviderBodyError
	if errors.As(err, &unverified) {
		return false
	}
	if code != wantCode || validTelemetryRequestID(requestID) != requestID || requestID == "" {
		return false
	}
	var responseErr dara.ResponseError
	var teaErr *tea.SDKError
	var daraErr *dara.SDKError
	var status int
	var data map[string]any
	var encoded string
	switch {
	case errors.As(err, &responseErr):
		if responseErr.GetStatusCode() == nil {
			return false
		}
		status, data = *responseErr.GetStatusCode(), responseErr.GetData()
	case errors.As(err, &teaErr):
		if teaErr.StatusCode == nil {
			return false
		}
		status, encoded = *teaErr.StatusCode, tea.StringValue(teaErr.Data)
	case errors.As(err, &daraErr):
		if daraErr.StatusCode == nil {
			return false
		}
		status, encoded = *daraErr.StatusCode, tea.StringValue(daraErr.Data)
	default:
		return false
	}
	if status != wantStatus {
		return false
	}
	if encoded != "" {
		decoded, err := decodeStrictJSON([]byte(encoded))
		if err != nil {
			return false
		}
		if wrapped, ok := decoded.(string); ok {
			decoded, err = decodeStrictJSON([]byte(wrapped))
			if err != nil {
				return false
			}
		}
		var ok bool
		data, ok = decoded.(map[string]any)
		if !ok {
			return false
		}
	}
	return envelopeProof(data, wantCode, requestID)
}

// A typed HTTP 404 may include transport outcome fields around its error
// envelope. Both fields must agree with the 404 before they are removed for
// the existing complete-envelope proof.
func volume404ErrorEnvelopeProof(data map[string]any, code, requestID string) bool {
	if volumeErrorEnvelopeProof(data, code, requestID) {
		return true
	}
	if data["success"] != false {
		return false
	}
	var bodyStatus float64
	switch value := data["httpStatusCode"].(type) {
	case float64:
		bodyStatus = value
	case json.Number:
		var err error
		bodyStatus, err = value.Float64()
		if err != nil {
			return false
		}
	default:
		return false
	}
	if bodyStatus != 404 {
		return false
	}
	remainder := make(map[string]any, len(data)-2)
	for key, value := range data {
		if key != "httpStatusCode" && key != "success" {
			remainder[key] = value
		}
	}
	return volumeErrorEnvelopeProof(remainder, code, requestID)
}

func volumeErrorEnvelopeProof(data map[string]any, code, requestID string) bool {
	if data == nil || validTelemetryRequestID(requestID) != requestID || requestID == "" {
		return false
	}
	var bodyCode, bodyRequestID string
	var seenCode, seenRequestID bool
	for key, value := range data {
		// Negative proof is an error envelope, not a partially successful
		// resource response. Normalize aliases before examining the complete
		// top-level body so plural resources and success flags fail closed too.
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		switch normalized {
		case "code":
			item, ok := value.(string)
			if !ok || seenCode {
				return false
			}
			bodyCode = item
			seenCode = true
		case "requestid":
			item, ok := value.(string)
			if !ok || seenRequestID {
				return false
			}
			bodyRequestID = item
			seenRequestID = true
		case "message", "hostid", "recommend":
			if _, ok := value.(string); !ok {
				return false
			}
		default:
			return false
		}
	}
	return bodyCode == code && bodyRequestID == requestID
}

func (c *OpenAPICaller) validateVolumeRequest(req *openAPIRequest) error {
	invalid := func(field string) error {
		return ecerrors.Client("InvalidVolumeInput", i18n.NewLocalizer("en").Message("InvalidVolumeInput"), ecerrors.WithField(field))
	}
	if req.ApiName == "GetVolume" || req.ApiName == "DeleteVolume" {
		if id := req.PathParams["volumeID"]; !validTeamIdentity(id) {
			return invalid("id")
		}
	}
	if team := req.QueryParams["teamID"]; team != "" && !validTeamIdentity(team) {
		return invalid("team_id")
	}
	if req.ApiName == "CreateVolume" {
		body, ok := req.BodyValue().(map[string]any)
		if !ok {
			return invalid("body")
		}
		if team, present := body["teamID"]; present {
			id, ok := team.(string)
			if !ok || !validTeamIdentity(id) {
				return invalid("team_id")
			}
		}
	}
	return nil
}

func scrubVolumeResponse(response map[string]any) map[string]any {
	if response == nil {
		return nil
	}
	clean := make(map[string]any, len(response))
	for key, value := range response {
		clean[key] = value
	}
	scrub := func(value any) any {
		volume, ok := value.(map[string]any)
		if !ok {
			return value
		}
		reason, hasReason := volume["statusReason"].(string)
		if !hasReason {
			return value
		}
		copy := make(map[string]any, len(volume))
		for key, item := range volume {
			copy[key] = item
		}
		copy["statusReason"] = sanitizeTemplateCacheDiagnostic(reason)
		return copy
	}
	if volume, ok := clean["volume"]; ok {
		clean["volume"] = scrub(volume)
	}
	if volumes, ok := clean["volumes"].([]any); ok {
		copies := make([]any, len(volumes))
		for index, volume := range volumes {
			copies[index] = scrub(volume)
		}
		clean["volumes"] = copies
	}
	return clean
}

// A positive business code cannot establish a mutation when another field
// reports failure or names a different request. DryRun is handled separately
// so a returned create ID can remain an unconfirmed recovery hint.
func volumePositiveEnvelopeConsistent(response map[string]any) bool {
	for key, value := range response {
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		switch normalized {
		case "requestid":
			if key != "requestId" {
				return false
			}
			id, ok := value.(string)
			if !ok || id == "" || sanitizeTemplateCacheRequestID(id) != id {
				return false
			}
		case "success":
			if value != true {
				return false
			}
		case "error", "recoverycommand":
			return false
		}
	}
	return true
}

// The Volume ROA API carries its business result in a lower-case envelope.
// A successful HTTP status or the metadata's synchronous marker does not
// establish that a new Volume is ready or a deleted Volume is absent.
func volumeCreateAcceptance(req *openAPIRequest, response map[string]any) (ecerrors.AcceptedVolume, bool) {
	if req == nil || req.ApiName != "CreateVolume" || response["code"] != "200" || !volumePositiveEnvelopeConsistent(response) {
		return ecerrors.AcceptedVolume{}, false
	}
	for key, value := range response {
		if key != "code" && strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key)) == "code" && value != "200" {
			return ecerrors.AcceptedVolume{}, false
		}
	}
	requestID, ok := response["requestId"].(string)
	if !ok || requestID == "" || sanitizeTemplateCacheRequestID(requestID) != requestID {
		return ecerrors.AcceptedVolume{}, false
	}
	body, ok := req.BodyValue().(map[string]any)
	if !ok {
		return ecerrors.AcceptedVolume{}, false
	}
	name, ok := body["volumeName"].(string)
	if !ok || name == "" {
		return ecerrors.AcceptedVolume{}, false
	}
	team, teamRequested := body["teamID"].(string)
	if _, present := body["teamID"]; present && !teamRequested || team != "" && !validTeamIdentity(team) || req.RegionId == "" {
		return ecerrors.AcceptedVolume{}, false
	}
	volume, ok := response["volume"].(map[string]any)
	if !ok || volume["volumeName"] != name {
		return ecerrors.AcceptedVolume{}, false
	}
	id, idOK := volume["volumeID"].(string)
	status, statusOK := volume["status"].(string)
	responseTeam, teamOK := volume["teamID"].(string)
	if !idOK || !validTeamIdentity(id) || !statusOK || strings.TrimSpace(status) == "" || !teamOK || !validTeamIdentity(responseTeam) || team != "" && responseTeam != team {
		return ecerrors.AcceptedVolume{}, false
	}
	confirmed := true
	for key := range response {
		if strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key)) == "dryrun" {
			confirmed = false
		}
	}
	return ecerrors.AcceptedVolume{ID: id, Name: name, TeamID: responseTeam, Region: req.RegionId, RequestID: requestID, Operation: req.ApiName, AllocationConfirmed: confirmed}, true
}

func (c *OpenAPICaller) fcVolumeResponseError(req *openAPIRequest, response map[string]any) error {
	providerRequestID, _ := response["requestId"].(string)
	requestID := sanitizeTemplateCacheRequestID(providerRequestID)
	invalid := func() error {
		return ecerrors.Service("InvalidVolumeResponse", i18n.NewLocalizer("en").Message("InvalidVolumeResponse"), false, ecerrors.WithRequestID(requestID))
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
	// The SDK normalizes JSON into a map. Do not let an alternate spelling
	// contradict the canonical business code after that normalization.
	for key, value := range response {
		if key == "code" {
			continue
		}
		if strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key)) == "code" {
			alias, ok := value.(string)
			if !ok || alias != code {
				return invalid()
			}
		}
	}
	if code != "200" {
		publicCode := sanitizeTemplateCacheDiagnostic(code)
		message, _ := response["message"].(string)
		if message == "" {
			message = i18n.NewLocalizer("en").Message("CloudAPIError")
		}
		message = sanitizeTemplateCacheDiagnostic(message)
		options := []ecerrors.Option{ecerrors.WithRequestID(requestID), ecerrors.WithRawCause(publicCode, message)}
		if req.ApiName == "CreateVolume" && volumeErrorEnvelopeProof(response, "VolumeConflict", providerRequestID) {
			options = append(options, ecerrors.WithVerifiedProviderResponse())
		}
		if req.ApiName == "DeleteVolume" && (volumeErrorEnvelopeProof(response, "Forbidden", providerRequestID) || volumeErrorEnvelopeProof(response, "NoPermission", providerRequestID)) {
			options = append(options, ecerrors.WithVerifiedProviderResponse())
		}
		if req.ApiName == "GetVolume" && volumeErrorEnvelopeProof(response, "VolumeNotFound", providerRequestID) {
			options = append(options, ecerrors.WithVerifiedProviderResponse())
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("VolumeNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	if !volumePositiveEnvelopeConsistent(response) {
		return invalid()
	}
	if req.ApiName == "CreateVolume" || req.ApiName == "DeleteVolume" {
		for key := range response {
			if strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key)) == "dryrun" {
				return invalid()
			}
		}
	}
	validVolume := func(value any, expectedID, expectedTeam, expectedName string) (string, bool) {
		volume, ok := value.(map[string]any)
		if !ok {
			return "", false
		}
		id, _ := volume["volumeID"].(string)
		status, _ := volume["status"].(string)
		if !validTeamIdentity(id) || strings.TrimSpace(status) == "" || expectedID != "" && id != expectedID {
			return "", false
		}
		for _, field := range []string{"volumeName", "teamID", "resourceGroupID", "storageClass", "createdAt", "updatedAt", "userID", "statusReason"} {
			if item, present := volume[field]; present {
				if _, ok := item.(string); !ok {
					return "", false
				}
			}
		}
		team, _ := volume["teamID"].(string)
		name, _ := volume["volumeName"].(string)
		if expectedTeam != "" && team != expectedTeam || expectedName != "" && name != expectedName || team != "" && !validTeamIdentity(team) {
			return "", false
		}
		for _, field := range []string{"ossVolumeConfig", "agenticFSVolumeConfig", "agenticBucketVolumeConfig", "mountConfig"} {
			if item, present := volume[field]; present {
				if _, ok := item.(map[string]any); !ok {
					return "", false
				}
			}
		}
		return id, true
	}
	switch req.ApiName {
	case "CreateVolume", "GetVolume":
		expectedID, expectedTeam, expectedName := req.PathParams["volumeID"], req.QueryParams["teamID"], ""
		if req.ApiName == "CreateVolume" {
			body, ok := req.BodyValue().(map[string]any)
			if !ok {
				return invalid()
			}
			if team, ok := body["teamID"].(string); ok {
				expectedTeam = team
			}
			expectedName, _ = body["volumeName"].(string)
		}
		if _, ok := validVolume(response["volume"], expectedID, expectedTeam, expectedName); !ok {
			return invalid()
		}
		if req.ApiName == "CreateVolume" {
			volume := response["volume"].(map[string]any)
			if team, _ := volume["teamID"].(string); team == "" {
				return invalid()
			}
		}
	case "ListVolumes":
		items, ok := response["volumes"].([]any)
		if !ok {
			return invalid()
		}
		limit, err := strconv.Atoi(req.QueryParams["maxResults"])
		if err != nil || limit < 1 || len(items) > limit {
			return invalid()
		}
		if token, present := response["nextToken"]; present {
			if _, ok := token.(string); !ok {
				return invalid()
			}
		}
		if size, present := response["maxResults"]; present {
			number, ok := volumePageInteger(size)
			if !ok || number != limit {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, item := range items {
			id, valid := validVolume(item, "", req.QueryParams["teamID"], "")
			if !valid || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	case "DeleteVolume":
		// The response contains only the business envelope.
		if _, present := response["volume"]; present {
			return invalid()
		}
		if _, present := response["volumes"]; present {
			return invalid()
		}
	}
	return nil
}

func volumePageInteger(value any) (int, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseInt(number.String(), 10, 32)
		return int(parsed), err == nil && parsed >= 0
	}
	return teamPageInteger(value)
}
