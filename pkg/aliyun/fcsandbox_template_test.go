package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestFCSandboxTemplateRejectsBusinessFailureAndInvalidSuccess(t *testing.T) {
	for _, tc := range []struct{ operation, body, want string }{
		{"CreateTemplate", `{"code":"409","message":"failure","requestId":"req"}`, "CloudAPIError"},
		{"CreateTemplate", `{"code":"200","templateID":""}`, "InvalidFCTemplateResponse"},
		{"CreateTemplate", `{"code":200,"templateID":"tpl-1"}`, "InvalidFCTemplateResponse"},
		{"CreateTemplate", `{"code":"200","templateID":"tpl-1 --region other"}`, "InvalidFCTemplateResponse"},
		{"CreateTemplate", `{"code":"200","templateID":"tpl-1","requestId":false}`, "InvalidFCTemplateResponse"},
		{"UpdateTemplate", `{"code":"200","templateID":"tpl-other"}`, "InvalidFCTemplateResponse"},
		{"GetTemplate", `{"code":"200","templateID":"tpl-1","teamID":"other","status":{"state":"ready"}}`, "InvalidFCTemplateResponse"},
		{"GetTemplate", `{"code":"200","templateID":"tpl-1","teamID":"team-1"}`, "InvalidFCTemplateResponse"},
		{"ListTemplates", `{"code":"200","templates":{}}`, "InvalidFCTemplateResponse"},
		{"ListTemplates", `{"code":"200","templates":[],"nextToken":false}`, "InvalidFCTemplateResponse"},
		{"ListTemplates", `{"code":"200","templates":[{"templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}},{"templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}]}`, "InvalidFCTemplateResponse"},
		{"DeleteTemplate", `{"code":"409","message":"busy"}`, "CloudAPIError"},
	} {
		t.Run(tc.operation+tc.body, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.body}}
			request := map[string]any{"templateID": "tpl-1", "teamID": "team-1"}
			if tc.operation == "CreateTemplate" {
				request["body.name"] = "native"
			}
			if tc.operation == "UpdateTemplate" {
				request["body.runtimeConfig"] = map[string]any{"cpu": 0.5}
			}
			resp, err := caller.CallRaw(context.Background(), tc.operation, request)
			var app *ecerrors.AppError
			if resp != nil || !errors.As(err, &app) || app.Payload().Code != tc.want {
				t.Fatalf("response=%#v error=%v want=%s", resp, err, tc.want)
			}
		})
	}
}

func TestFCSandboxTemplateROARequestContract(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	for _, tc := range []struct {
		name, method, path string
		values             map[string]any
	}{
		{"CreateTemplate", "POST", "/pop/2026-05-09/templates", map[string]any{"body.name": "native", "body.teamID": "team-1", "body.runtimeConfig": map[string]any{"cpu": 0.5, "memorySize": float64(1024), "internetAccess": false}}},
		{"GetTemplate", "GET", "/pop/2026-05-09/templates/tpl-1", map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
		{"ListTemplates", "GET", "/pop/2026-05-09/templates", map[string]any{"teamID": "team-1", "maxResults": 2, "nextToken": "next"}},
		{"UpdateTemplate", "PUT", "/pop/2026-05-09/templates/tpl-1", map[string]any{"templateID": "tpl-1", "teamID": "team-1", "body.buildConfig": map[string]any{"envdInject": map[string]any{"enabled": false}}, "body.runtimeConfig": map[string]any{"cpu": 0.5}}},
		{"DeleteTemplate", "DELETE", "/pop/2026-05-09/templates/tpl-1", map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := caller.commonRequest(tc.name, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if req.Version != "2026-05-09" || req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-hangzhou.aliyuncs.com" {
				t.Fatalf("ROA=%+v", req)
			}
			if tc.name == "CreateTemplate" {
				if req.BodyValue().(map[string]any)["teamID"] != "team-1" || req.QueryParams["teamID"] != "" {
					t.Fatalf("create scope=%+v", req)
				}
			} else if req.QueryParams["teamID"] != "team-1" {
				t.Fatalf("query scope=%+v", req)
			}
			if tc.name == "UpdateTemplate" {
				body := req.BodyValue().(map[string]any)
				if body["name"] != nil || body["public"] != nil || body["runtimeConfig"].(map[string]any)["cpu"] != 0.5 {
					t.Fatalf("PublicUpdateTemplateInput=%#v", body)
				}
			}
		})
	}
}

func TestFCSandboxTemplateScrubsSecretsInResponsesAndLaterErrors(t *testing.T) {
	response := `{"code":"200","requestId":"req","templateID":"tpl-1","teamID":"team-1","runtimeConfig":{"cpu":0.5,"internetAccess":false,"sandboxConfig":{"registryConfig":{"authConfig":{"password":"private-password","userName":"private-user"}}}},"status":{"state":"failed","reason":{"message":"password=private-password"}}}`
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: response}}
	resp, err := caller.CallRaw(context.Background(), "GetTemplate", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(resp)
	if strings.Contains(string(encoded), "private-password") || strings.Contains(string(encoded), "private-user") {
		t.Fatalf("secret response=%s", encoded)
	}
	caller.executor = &fakeOpenAPIExecutor{response: `{"code":"200","templateID":"tpl-1"}`}
	_, err = caller.CallRaw(context.Background(), "CreateTemplate", map[string]any{"body.name": "native", "body.runtimeConfig": map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": "echoed-secret"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	caller.executor = &fakeOpenAPIExecutor{response: `{"code":"409","message":"pull failed using echoed-secret","requestId":"echoed-secret"}`}
	_, err = caller.CallRaw(context.Background(), "GetTemplate", map[string]any{"templateID": "tpl-1"})
	if err == nil || strings.Contains(err.Error(), "echoed-secret") || strings.Contains(ecerrors.ActionFromError("GetTemplate", err).RequestID, "echoed-secret") {
		t.Fatalf("waiter error leaked secret: %v", err)
	}
}

func TestFCSandboxTemplateRejectsInvalidReferencedConfigBeforeAPI(t *testing.T) {
	for _, tc := range []struct {
		operation      string
		runtime, build map[string]any
	}{
		{"CreateTemplate", map[string]any{"cpu": "0.5"}, nil},
		{"CreateTemplate", map[string]any{"memorySize": 1.5}, nil},
		{"CreateTemplate", map[string]any{"internetAccess": "false"}, nil},
		{"CreateTemplate", map[string]any{"vpcConfig": map[string]any{"vSwitchIds": []any{float64(1)}}}, nil},
		{"UpdateTemplate", map[string]any{"sandboxConfig": map[string]any{"steps": []any{map[string]any{"type": "RUN", "args": []any{"true"}}}}}, nil},
		{"UpdateTemplate", nil, map[string]any{"envdInject": map[string]any{"enabled": "false"}}},
		{"CreateTemplate", map[string]any{"sandboxConfig.image": "example/image:v1"}, nil},
		{"UpdateTemplate", map[string]any{"sandboxConfig.image": "example/image:v1"}, nil},
		{"CreateTemplate", nil, map[string]any{"envdInject.enabled": false}},
		{"UpdateTemplate", nil, map[string]any{"envdInject.enabled": false}},
		{"CreateTemplate", map[string]any{"sandboxConfig": map[string]any{"steps": []any{map[string]any{"type": "RUN", "args": []any{"true"}, "registryConfig.authConfig.password": "private"}}}}, nil},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","templateID":"tpl-1"}`}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
			req := map[string]any{"templateID": "tpl-1", "body.name": "native"}
			if tc.operation == "UpdateTemplate" {
				delete(req, "body.name")
			}
			if tc.runtime != nil {
				req["body.runtimeConfig"] = tc.runtime
			}
			if tc.build != nil {
				req["body.buildConfig"] = tc.build
			}
			_, err := caller.CallRaw(context.Background(), tc.operation, req)
			if err == nil || len(fake.requests) != 0 {
				t.Fatalf("invalid native configuration reached API: %v requests=%+v", err, fake.requests)
			}
		})
	}
}

func TestFCSandboxTemplateAcceptsLiteralNestedConfig(t *testing.T) {
	for _, operation := range []string{"CreateTemplate", "UpdateTemplate"} {
		fake := &fakeOpenAPIExecutor{response: `{"code":"200","templateID":"tpl-1"}`}
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
		request := map[string]any{"templateID": "tpl-1", "body.runtimeConfig": map[string]any{"sandboxConfig": map[string]any{"image": "example/image:v1"}, "vpcConfig": map[string]any{"vSwitchIds": []any{"vsw-1"}}, "internetAccess": false}, "body.buildConfig": map[string]any{"envdInject": map[string]any{"enabled": false}}}
		if operation == "CreateTemplate" {
			request["body.name"] = "native"
		}
		if _, err := caller.CallRaw(context.Background(), operation, request); err != nil || len(fake.requests) != 1 {
			t.Fatalf("valid nested %s config rejected: %v %+v", operation, err, fake.requests)
		}
	}
	// Each list element is another literal Struct node, not a dotted path.
	parameter := &OpenAPIParameter{Type: "RepeatList", Element: &OpenAPIParameter{Type: "Struct", SubParameters: []OpenAPIParameter{{Name: "nested", Type: "Struct", SubParameters: []OpenAPIParameter{{Name: "enabled", Type: "Boolean"}}}}}}
	if invalidTemplateParameter([]any{map[string]any{"nested.enabled": false}}, parameter, "steps") == "" {
		t.Fatal("dotted literal member bypassed list element schema")
	}
	if bad := invalidTemplateParameter([]any{map[string]any{"nested": map[string]any{"enabled": false}}}, parameter, "steps"); bad != "" {
		t.Fatalf("valid list object rejected: %s", bad)
	}
}

func TestFCSandboxTemplateBuildFailurePreservesSanitizedReason(t *testing.T) {
	resource, err := spec.LoadResource("", "sandbox", "template")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{responses: []string{
		`{"code":"200","requestId":"req-create","templateID":"tpl-1"}`,
		`{"code":"200","requestId":"req-get","templateID":"tpl-1","teamID":"team-1","status":{"state":"failed","reason":{"message":"pull failed with echoed-secret","step":"build"}}}`,
	}}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
	_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"openapi": true, "name": "native", "team_id": "team-1", "runtime_config": map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": "echoed-secret"}}}}}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "FCTemplateBuildFailed" {
		t.Fatalf("native build result=%v", err)
	}
	encoded, _ := json.Marshal(struct {
		Error   ecerrors.ErrorPayload `json:"error"`
		Actions []ecerrors.Action     `json:"actions"`
	}{app.Payload(), app.Actions()})
	if strings.Contains(string(encoded), "echoed-secret") || !strings.Contains(string(encoded), "pull failed") || !strings.Contains(string(encoded), "req-get") {
		t.Fatalf("build failure evidence=%s", encoded)
	}
	recoverer, ok := any(caller).(interface{ TemplateCreateRecovery(error) error })
	if !ok {
		t.Fatal("a failed native build must retain the allocated ID for scoped cleanup")
	}
	err = recoverer.TemplateCreateRecovery(err)
	if !errors.As(err, &app) {
		t.Fatal(err)
	}
	want := []string{"ecctl", "sandbox", "template", "delete", "tpl-1", "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1"}
	if strings.Join(app.Payload().RecoveryCommand, " ") != strings.Join(want, " ") {
		t.Fatalf("cleanup scope=%+v", app.Payload().RecoveryCommand)
	}
}
