package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/output"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/sandbox"
)

type teamSuccessHTTPSequence struct {
	bodies []string
	calls  int
}

func TestFCSandboxTeamRejectsUnsafeSuccessfulIdentity(t *testing.T) {
	for _, id := range []string{"foreign-team --region cn-beijing", "foreign-team --profile victim", "foreign-team extra", "foreign-team\n--region cn-beijing", "--region", "team/a", `team"quote`, "团队"} {
		t.Run(id, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"code": "200", "requestId": "securityToken=private-id", "team": map[string]any{"teamID": id, "status": "active"}})
			if err != nil {
				t.Fatal(err)
			}
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{status: 200, body: string(body)}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: executor}
			response, err := caller.CallRaw(context.Background(), "CreateTeam", map[string]any{"body.teamName": "dev"})
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) || appErr.Payload().Code != "InvalidTeamResponse" || response != nil {
				t.Fatalf("unsafe created identity crossed provider boundary: %q response=%+v err=%v", id, response, err)
			}
			if action := ecerrors.ActionFromError("CreateTeam", err); action.RequestID != "Alibaba Cloud API request failed" {
				t.Fatalf("invalid response metadata was not sanitized: %+v", action)
			}
		})
	}
}

func TestFCSandboxTeamSuccessfulIdentityAndAbsenceCompatibility(t *testing.T) {
	for _, id := range []string{"created-Team_1", "1-team", "ed3c4a86-425d-40c4-a9db-d5ef407b2d97"} {
		t.Run(id, func(t *testing.T) {
			encoded, err := json.Marshal(map[string]any{"code": "200", "requestId": "req-create", "team": map[string]any{"teamID": id, "status": "active"}})
			if err != nil {
				t.Fatal(err)
			}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: string(encoded)}}
			response, err := caller.CallRaw(context.Background(), "CreateTeam", map[string]any{"body.teamName": "dev"})
			if err != nil || response["team"].(map[string]any)["teamID"] != id {
				t.Fatalf("ordinary successful identity rejected: %+v %v", response, err)
			}
		})
	}
	for _, code := range []string{"TeamNotFound", "404"} {
		t.Run(code, func(t *testing.T) {
			encoded, err := json.Marshal(map[string]any{"code": code, "message": "https://example.com/?Signature=private-signature", "requestId": "securityToken=private-token"})
			if err != nil {
				t.Fatal(err)
			}
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{status: 200, body: string(encoded)}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: executor}
			_, err = caller.CallRaw(context.Background(), "GetTeam", map[string]any{"teamID": "team-1"})
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) || appErr.Payload().Kind != "not_found" || appErr.Payload().Code != "NotFound" || appErr.Payload().Message != i18n.NewLocalizer("en").Message("TeamNotFound") {
				t.Fatalf("canonical Team absence changed: %v", err)
			}
			action := ecerrors.ActionFromError("GetTeam", err)
			if action.Code != code || action.Message != "https://example.com/?[REDACTED]" || action.RequestID != "Alibaba Cloud API request failed" {
				t.Fatalf("absence provider evidence changed: %+v", action)
			}
		})
	}
}

func (s *teamSuccessHTTPSequence) Call(_ *http.Request, _ *http.Transport) (*http.Response, error) {
	body := s.bodies[s.calls]
	s.calls++
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestFCSandboxTeamSuccessRequestIDPublicErrors(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "team")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []struct{ raw, want string }{
		{"req-ordinary", "req-ordinary"},
		{"securityToken=private-token", "Alibaba Cloud API request failed"},
		{"https://example.com/?Signature=private-signature", "https://example.com/?[REDACTED]"},
		{"Deny: private-principal|source ip: 127.0.0.1", "Deny: [REDACTED]|source ip: 127.0.0.1"},
	} {
		for _, tc := range []struct {
			name, action, code string
			input              map[string]any
			readonly           bool
			pages              bool
		}{
			{"readonly", "delete", "TeamReadOnly", map[string]any{"id": "team-1", "no_wait": true}, true, false},
			{"rename", "update", "TeamRenameNotAllowed", map[string]any{"id": "team-1", "name": "new-name"}, false, false},
			{"later page", "list", "CloudAPIError", map[string]any{"limit": 1, "page": 1, "all": true}, false, true},
		} {
			t.Run(tc.name+"/"+id.want, func(t *testing.T) {
				team := map[string]any{"teamID": "team-1", "teamName": "old-name", "status": "active", "readOnly": tc.readonly, "allowUpdateTeamName": false}
				body := map[string]any{"code": "200", "requestId": id.raw, "team": team}
				if tc.pages {
					body = map[string]any{"code": "200", "requestId": id.raw, "teams": []any{team}, "total": 2, "pageNumber": 1, "pageSize": 1}
				}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				sequence := &teamSuccessHTTPSequence{bodies: []string{string(encoded), `{"code":"Forbidden","message":"page rejected","requestId":"req-page-2"}`}}
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.HttpClient = sequence
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: executor}
				_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
				var appErr *ecerrors.AppError
				if !errors.As(err, &appErr) || appErr.Payload().Code != tc.code {
					t.Fatalf("actual hook/page error = %v", err)
				}
				actions := appErr.Actions()
				if len(actions) == 0 || actions[0].RequestID != id.want {
					t.Fatalf("success action request ID = %+v, want %q", actions, id.want)
				}
				if tc.pages && (len(actions) != 2 || actions[1].RequestID != "req-page-2" || sequence.calls != 2) {
					t.Fatalf("page actions lost: %+v", actions)
				}
				for _, mode := range []string{output.ModeJSON, output.ModeText} {
					var rendered bytes.Buffer
					public := map[string]any{"error": i18n.NewLocalizer("en").ErrorPayload(appErr.Payload(), true), "actions": actions}
					if err := output.Write(&rendered, mode, public, output.TextOptions{}); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(rendered.String(), "private-") {
						t.Fatalf("%s public output leaked request ID: %s", mode, rendered.String())
					}
				}
			})
		}
	}
}

func TestFCSandboxTeamBusinessErrorFieldsAreSanitized(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "team")
	if err != nil {
		t.Fatal(err)
	}
	localizer := i18n.NewLocalizer("en")
	invalidMessage := localizer.Message("InvalidTeamResponse")
	for _, tc := range []struct {
		name, code, message, requestID                    string
		wantCode, wantMessage, wantRequestID, payloadCode string
	}{
		{"ordinary fields", "409", "team has API keys", "req-business", "409", "team has API keys", "req-business", "CloudAPIError"},
		{"credential assignment message", "409", "securityToken=private-token", "req-business", "409", "Alibaba Cloud API request failed", "req-business", "CloudAPIError"},
		{"signed URL message", "409", "request https://example.com/?AccessKeyId=private-ak&Signature=private-signature failed", "req-business", "409", "request https://example.com/?[REDACTED] failed", "req-business", "CloudAPIError"},
		{"Deny principal message", "409", "Deny: private-principal|source ip: 127.0.0.1", "req-business", "409", "Deny: [REDACTED]|source ip: 127.0.0.1", "req-business", "CloudAPIError"},
		{"credential assignment code", "securityToken=private-code", "provider rejected request", "req-business", "Alibaba Cloud API request failed", "provider rejected request", "req-business", "CloudAPIError"},
		{"credential assignment request ID", "409", "provider rejected request", "securityToken=private-request", "409", "provider rejected request", "Alibaba Cloud API request failed", "CloudAPIError"},
		{"invalid missing code request ID", "", "unused provider message", "securityToken=private-request", "InvalidTeamResponse", invalidMessage, "Alibaba Cloud API request failed", "InvalidTeamResponse"},
		{"invalid success signed URL request ID", "200", "success", "https://example.com/?AccessKeyId=private-ak&Signature=private-signature", "InvalidTeamResponse", invalidMessage, "https://example.com/?[REDACTED]", "InvalidTeamResponse"},
		{"invalid success Deny request ID", "200", "success", "Deny: private-principal|source ip: 127.0.0.1", "InvalidTeamResponse", invalidMessage, "Deny: [REDACTED]|source ip: 127.0.0.1", "InvalidTeamResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"message": tc.message, "requestId": tc.requestID}
			if tc.code != "" {
				body["code"] = tc.code
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{status: 200, body: string(encoded)}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: executor}
			_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "list", Input: map[string]any{"limit": 1, "page": 1}})
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("business failure did not produce AppError: %v", err)
			}
			payload := appErr.Payload()
			actions := appErr.Actions()
			if payload.Kind != "service" || payload.Code != tc.payloadCode || payload.Message != tc.wantMessage || payload.Retryable {
				t.Errorf("business payload = %+v", payload)
			}
			if len(actions) != 1 || actions[0].ActionName != "ListTeams" || actions[0].Code != tc.wantCode || actions[0].Message != tc.wantMessage || actions[0].RequestID != tc.wantRequestID {
				t.Errorf("business actions = %+v", actions)
			}
			rawPayload, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(rawPayload), "private-") {
				t.Errorf("business payload leaked fixture secret: %s", rawPayload)
			}
			for _, mode := range []string{output.ModeJSON, output.ModeText} {
				var rendered bytes.Buffer
				public := map[string]any{"error": localizer.ErrorPayload(payload, len(actions) > 0), "actions": actions}
				if err := output.Write(&rendered, mode, public, output.TextOptions{}); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(rendered.String(), "private-") {
					t.Errorf("%s public output leaked fixture secret: %s", mode, rendered.String())
				}
				if mode == output.ModeJSON {
					var decoded struct {
						Error   ecerrors.ErrorPayload `json:"error"`
						Actions []ecerrors.Action     `json:"actions"`
					}
					if err := json.Unmarshal(rendered.Bytes(), &decoded); err != nil {
						t.Fatal(err)
					}
					if decoded.Error.Code != tc.payloadCode || len(decoded.Actions) != 1 || decoded.Actions[0].Code != tc.wantCode || decoded.Actions[0].Message != tc.wantMessage || decoded.Actions[0].RequestID != tc.wantRequestID {
						t.Errorf("public provider metadata changed: %s", rendered.String())
					}
				}
			}
		})
	}
}

func TestFCSandboxTeamBusinessErrorsAndInvalidSuccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, api, response, kind string }{
		{"business error", "DeleteTeam", `{"code":"409","message":"team has API keys","requestId":"req-business"}`, "service"},
		{"missing code", "DeleteTeam", `{"message":"success","requestId":"req-business"}`, "service"},
		{"missing team", "GetTeam", `{"code":"200","message":"success","requestId":"req-business"}`, "service"},
		{"wrong team", "GetTeam", `{"code":"200","team":{"teamID":"other","status":"active"},"requestId":"req-business"}`, "service"},
		{"missing status", "GetTeam", `{"code":"200","team":{"teamID":"team-1"},"requestId":"req-business"}`, "service"},
		{"missing teams", "ListTeams", `{"code":"200","total":0,"pageNumber":1,"pageSize":50,"requestId":"req-business"}`, "service"},
		{"missing total", "ListTeams", `{"code":"200","teams":[],"pageNumber":1,"pageSize":50,"requestId":"req-business"}`, "service"},
		{"invalid permission", "GetTeam", `{"code":"200","team":{"teamID":"team-1","status":"active","readOnly":"false"},"requestId":"req-business"}`, "service"},
		{"fractional total", "ListTeams", `{"code":"200","teams":[],"total":1.5,"pageNumber":1,"pageSize":50,"requestId":"req-business"}`, "service"},
		{"oversize page", "ListTeams", `{"code":"200","teams":[],"total":0,"pageNumber":1,"pageSize":51,"requestId":"req-business"}`, "service"},
		{"duplicate team", "ListTeams", `{"code":"200","teams":[{"teamID":"team-1","status":"active"},{"teamID":"team-1","status":"active"}],"total":2,"pageNumber":1,"pageSize":50,"requestId":"req-business"}`, "service"},
		{"not found", "GetTeam", `{"code":"TeamNotFound","message":"team missing","requestId":"req-business"}`, "not_found"},
		{"other missing dependency", "GetTeam", `{"code":"ResourceGroupNotFound","message":"resource group missing","requestId":"req-business"}`, "service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{}
			if tc.api != "ListTeams" {
				request["teamID"] = "team-1"
			}
			_, err := caller.Call(context.Background(), tc.api, request)
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) || appErr.Payload().Kind != tc.kind {
				t.Fatalf("error = %v, want %s", err, tc.kind)
			}
			if ecerrors.ActionFromError(tc.api, err).RequestID != "req-business" {
				t.Fatalf("request ID lost: %#v", appErr.Payload())
			}
			if tc.name == "business error" {
				action := ecerrors.ActionFromError(tc.api, err)
				if action.Code != "409" || action.Message != "team has API keys" || action.RequestID != "req-business" {
					t.Fatalf("provider error lost: %#v", action)
				}
			}
		})
	}
}

func TestFCSandboxTeamSuccessAndGenericCompatibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ api, response string }{
		{"GetTeam", `{"code":"200","team":{"teamID":"team-1","status":"active"}}`},
		{"ListTeams", `{"code":"200","teams":[],"total":0,"pageNumber":1,"pageSize":50}`},
		{"DeleteTeam", `{"code":"200","message":"success"}`},
	} {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
		if _, err := caller.Call(context.Background(), tc.api, map[string]any{"teamID": "team-1"}); err != nil {
			t.Fatalf("valid %s response rejected: %v", tc.api, err)
		}
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"409","message":"provider failure"}`}}
	if response, err := caller.Call(context.Background(), "DeleteTeam", map[string]any{"teamID": "team-1"}); err != nil || response["code"] != "409" {
		t.Fatalf("generic raw response changed: %#v %v", response, err)
	}
}

func TestFCSandboxTeamHTTPAbsenceIsScopedToTeam(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, kind string }{{"TeamNotFound", "not_found"}, {"404", "not_found"}, {"ResourceGroupNotFound", "service"}} {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: errors.New("SDK.ServerError\nErrorCode: " + tc.code + "\nMessage: missing\nRequestId: req-missing")}}
		_, err := caller.Call(context.Background(), "GetTeam", map[string]any{"teamID": "team-1"})
		var appErr *ecerrors.AppError
		if !errors.As(err, &appErr) || appErr.Payload().Kind != tc.kind || ecerrors.ActionFromError("GetTeam", err).RequestID != "req-missing" {
			t.Fatalf("%s error = %v, want %s with request ID", tc.code, err, tc.kind)
		}
	}
}

func TestFCSandboxTeamBusinessFailureTelemetry(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"409","message":"team has API keys","requestId":"req-business"}`}}
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	if _, err := caller.Call(ctx, "DeleteTeam", map[string]any{"teamID": "team-1"}); err == nil {
		t.Fatal("business failure succeeded")
	}
	session.Finish("ecctl sandbox team delete", 1)
	for _, span := range exporter.GetSpans() {
		if span.Name == "ecctl.cloud.api.request" {
			if outcome := testSpanAttributes(span.Attributes)["ecctl.cloud.outcome"]; outcome != "error" {
				t.Fatalf("business failure telemetry outcome = %#v", outcome)
			}
			return
		}
	}
	t.Fatal("missing cloud API span")
}

func TestFCSandboxTeamROARequests(t *testing.T) {
	t.Parallel()
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	for _, tc := range []struct {
		api, method, path string
		input             map[string]any
	}{
		{"CreateTeam", "POST", "/pop/2026-05-09/teams", map[string]any{"body.teamName": "dev", "body.description": "description", "body.resourceGroupID": "rg-test", "body.plan": "eco"}},
		{"GetTeam", "GET", "/pop/2026-05-09/teams/team-1", map[string]any{"teamID": "team-1"}},
		{"UpdateTeam", "PUT", "/pop/2026-05-09/teams/team-1", map[string]any{"teamID": "team-1", "body.description": "updated", "body.teamName": "new", "body.resourceGroupID": "rg-other", "body.plan": "std"}},
		{"DeleteTeam", "DELETE", "/pop/2026-05-09/teams/team-1", map[string]any{"teamID": "team-1"}},
		{"ListTeams", "GET", "/pop/2026-05-09/teams", map[string]any{"pageNumber": 2, "pageSize": 50, "teamName": "dev", "resourceGroupID": "rg-test", "plan": "eco"}},
	} {
		req, err := caller.commonRequest(tc.api, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if req.Version != "2026-05-09" || req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-hangzhou.aliyuncs.com" {
			t.Fatalf("%s request = %#v", tc.api, req)
		}
		if tc.api == "ListTeams" && (req.QueryParams["pageNumber"] != "2" || req.QueryParams["pageSize"] != "50" || req.QueryParams["teamName"] != "dev" || req.QueryParams["resourceGroupID"] != "rg-test" || req.QueryParams["plan"] != "eco") {
			t.Fatalf("list queries = %#v", req.QueryParams)
		}
		if tc.api == "CreateTeam" || tc.api == "UpdateTeam" {
			body := req.BodyValue().(map[string]any)
			for key, value := range tc.input {
				if len(key) > 5 && key[:5] == "body." && body[key[5:]] != value {
					t.Fatalf("%s body = %#v", tc.api, body)
				}
			}
		}
	}
}
