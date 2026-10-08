package aliyun

import (
	"errors"
	"math"
	"strconv"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
)

func (c *OpenAPICaller) isFCSandboxTeam() bool {
	return c.Resource == "team" && strings.EqualFold(c.Product, "FCSandbox")
}

func teamAbsenceCode(code string) bool {
	return code == "TeamNotFound" || code == "404"
}

func (c *OpenAPICaller) resourceResponseError(req *openAPIRequest, response map[string]any) error {
	if !c.isFCSandboxTeam() {
		return openAPIBusinessError(response)
	}
	requestID, _ := response["requestId"].(string)
	requestID = callerSanitizeCloudError(errors.New(requestID))
	invalid := func() error {
		return ecerrors.Service("InvalidTeamResponse", i18n.NewLocalizer("en").Message("InvalidTeamResponse"), false, ecerrors.WithRequestID(requestID))
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
		if req.ApiName == "GetTeam" && teamAbsenceCode(code) {
			return ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("TeamNotFound"), options...)
		}
		return ecerrors.Service("CloudAPIError", message, false, options...)
	}
	validTeam := func(value any, expectedID string) bool {
		team, ok := value.(map[string]any)
		if !ok {
			return false
		}
		id, _ := team["teamID"].(string)
		status, _ := team["status"].(string)
		if strings.TrimSpace(id) == "" || strings.TrimSpace(status) == "" || expectedID != "" && id != expectedID {
			return false
		}
		for _, field := range []string{"readOnly", "allowUpdateTeamName"} {
			if value, exists := team[field]; exists {
				if _, ok := value.(bool); !ok {
					return false
				}
			}
		}
		return true
	}
	switch req.ApiName {
	case "CreateTeam", "GetTeam", "UpdateTeam":
		if !validTeam(response["team"], req.PathParams["teamID"]) {
			return invalid()
		}
	case "ListTeams":
		teams, ok := response["teams"].([]any)
		total, totalOK := teamPageInteger(response["total"])
		page, pageOK := teamPageInteger(response["pageNumber"])
		size, sizeOK := teamPageInteger(response["pageSize"])
		if !ok || !totalOK || !pageOK || !sizeOK || page < 1 || size < 1 || size > 50 || total < len(teams) || len(teams) > size {
			return invalid()
		}
		for field, value := range map[string]int{"pageNumber": page, "pageSize": size} {
			if requested := req.QueryParams[field]; requested != "" && requested != strconv.Itoa(value) {
				return invalid()
			}
		}
		seen := map[string]bool{}
		for _, team := range teams {
			if !validTeam(team, "") {
				return invalid()
			}
			id := team.(map[string]any)["teamID"].(string)
			if seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	}
	return nil
}

func teamPageInteger(value any) (int, bool) {
	number, ok := value.(float64)
	if !ok || number < 0 || number > math.MaxInt32 || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}
