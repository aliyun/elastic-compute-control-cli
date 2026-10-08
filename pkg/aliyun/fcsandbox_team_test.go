package aliyun

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

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
