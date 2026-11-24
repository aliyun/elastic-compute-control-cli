package aliyun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/sandbox"
)

const apiKeyObject = `"apiKeyID":"key-1","apiKeyName":"dev","apiKeyMask":"e2b_xxxx****xxxx","apiKeyValue":"e2b_plaintext_secret","teamID":"team-1","teamName":"dev-team","teamPlan":"eco","resourceGroupID":"rg-1","userID":"12345","username":"user1","source":"","status":"active","createdTime":"2026-01-01T00:00:00Z","expireTime":"2027-01-01T00:00:00Z","lastUsedTime":"2026-02-01T00:00:00Z","ipWhitelist":[{"ipAddress":"203.0.113.10","description":"office"}],"ipBlacklist":[]`

func apiKeyCaller(fake *fakeOpenAPIExecutor) *OpenAPICaller {
	return &OpenAPICaller{Product: "FCSandbox", Resource: "api-key", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: fake}
}

func TestFCSandboxApiKeyROARequestContract(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "api-key", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	for _, tc := range []struct {
		api, method, path string
		values            map[string]any
	}{
		{"CreateApiKey", "POST", "/pop/2026-05-09/api-keys", map[string]any{"body.apiKeyName": "dev", "body.teamID": "team-1"}},
		{"DescribeApiKey", "GET", "/pop/2026-05-09/api-keys/key-1", map[string]any{"apiKeyID": "key-1"}},
		{"ListApiKeys", "GET", "/pop/2026-05-09/api-keys", map[string]any{"pageNumber": 1, "pageSize": 50, "teamID": "team-1", "status": "active"}},
		{"UpdateApiKey", "PUT", "/pop/2026-05-09/api-keys/key-1", map[string]any{"apiKeyID": "key-1", "body.status": "inactive"}},
		{"DeleteApiKey", "DELETE", "/pop/2026-05-09/api-keys/key-1", map[string]any{"apiKeyID": "key-1"}},
		{"ResetApiKey", "PUT", "/pop/2026-05-09/api-keys/key-1/reset", map[string]any{"apiKeyID": "key-1"}},
	} {
		t.Run(tc.api, func(t *testing.T) {
			req, err := caller.commonRequest(tc.api, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-hangzhou.aliyuncs.com" || req.Version != "2026-05-09" {
				t.Fatalf("wrong ROA request: %+v", req)
			}
			switch tc.api {
			case "CreateApiKey":
				body, ok := req.BodyValue().(map[string]any)
				if !ok || body["apiKeyName"] != "dev" || body["teamID"] != "team-1" || len(req.QueryParams) != 0 {
					t.Fatalf("create must send a JSON body: %+v", req)
				}
			case "UpdateApiKey":
				body, ok := req.BodyValue().(map[string]any)
				if !ok || body["status"] != "inactive" || req.PathParams["apiKeyID"] != "key-1" {
					t.Fatalf("update must send a JSON body and path ID: %+v", req)
				}
			case "ListApiKeys":
				if req.QueryParams["pageNumber"] != "1" || req.QueryParams["pageSize"] != "50" || req.QueryParams["teamID"] != "team-1" || req.QueryParams["status"] != "active" || req.BodyValue() != nil {
					t.Fatalf("list filters/pagination must use query: %+v", req)
				}
			case "ResetApiKey":
				if req.BodyValue() != nil || req.PathParams["apiKeyID"] != "key-1" {
					t.Fatalf("reset must be a bodyless PUT on the reset path: %+v", req)
				}
			default:
				if req.PathParams["apiKeyID"] != "key-1" || req.BodyValue() != nil {
					t.Fatalf("path ID must be the identity: %+v", req)
				}
			}
		})
	}
}

func TestFCSandboxApiKeyShowValueNeverReachesWire(t *testing.T) {
	for _, api := range []string{"CreateApiKey", "ResetApiKey"} {
		t.Run(api, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}
			caller := apiKeyCaller(fake)
			values := map[string]any{"body.apiKeyName": "dev", "ShowValue": true}
			if api == "ResetApiKey" {
				values = map[string]any{"apiKeyID": "key-1", "ShowValue": true}
			}
			if _, err := caller.CallRaw(context.Background(), api, values); err != nil {
				t.Fatal(err)
			}
			req := fake.requests[0]
			if _, present := req.QueryParams["ShowValue"]; present {
				t.Fatalf("ShowValue leaked into query: %+v", req.QueryParams)
			}
			if body, ok := req.BodyValue().(map[string]any); ok {
				if _, present := body["ShowValue"]; present {
					t.Fatalf("ShowValue leaked into body: %+v", body)
				}
			}
		})
	}
}

func TestFCSandboxApiKeyMasksPlaintextByDefault(t *testing.T) {
	for _, tc := range []struct {
		api, response string
		values        map[string]any
		list          bool
	}{
		{"DescribeApiKey", `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`, map[string]any{"apiKeyID": "key-1"}, false},
		{"UpdateApiKey", `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`, map[string]any{"apiKeyID": "key-1", "body.status": "inactive"}, false},
		{"CreateApiKey", `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`, map[string]any{"body.apiKeyName": "dev"}, false},
		{"ResetApiKey", `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`, map[string]any{"apiKeyID": "key-1"}, false},
		{"ListApiKeys", `{"code":"200","requestId":"req-1","apiKeys":[{` + apiKeyObject + `}],"pageNumber":1,"pageSize":50,"total":1}`, map[string]any{"pageNumber": 1, "pageSize": 50}, true},
	} {
		t.Run(tc.api, func(t *testing.T) {
			caller := apiKeyCaller(&fakeOpenAPIExecutor{response: tc.response})
			resp, err := caller.CallRaw(context.Background(), tc.api, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if tc.list {
				item := resp["apiKeys"].([]any)[0].(map[string]any)
				if _, present := item["apiKeyValue"]; present {
					t.Fatalf("list leaked plaintext: %+v", item)
				}
				if item["apiKeyMask"] != "e2b_xxxx****xxxx" {
					t.Fatalf("list lost mask: %+v", item)
				}
				return
			}
			item := resp["apiKey"].(map[string]any)
			if _, present := item["apiKeyValue"]; present {
				t.Fatalf("%s leaked plaintext by default: %+v", tc.api, item)
			}
			if item["apiKeyMask"] != "e2b_xxxx****xxxx" {
				t.Fatalf("%s lost mask: %+v", tc.api, item)
			}
		})
	}
}

func TestFCSandboxApiKeyRevealsPlaintextOnlyOnExplicitCreateReset(t *testing.T) {
	for _, api := range []string{"CreateApiKey", "ResetApiKey"} {
		t.Run(api, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}
			caller := apiKeyCaller(fake)
			values := map[string]any{"body.apiKeyName": "dev", "ShowValue": true}
			if api == "ResetApiKey" {
				values = map[string]any{"apiKeyID": "key-1", "ShowValue": true}
			}
			resp, err := caller.CallRaw(context.Background(), api, values)
			if err != nil {
				t.Fatal(err)
			}
			if resp["apiKey"].(map[string]any)["apiKeyValue"] != "e2b_plaintext_secret" {
				t.Fatalf("%s did not reveal plaintext on request: %+v", api, resp)
			}
		})
	}
	// Reveal is ignored for operations that must always mask.
	for _, api := range []string{"DescribeApiKey", "UpdateApiKey"} {
		t.Run(api+"/reveal-ignored", func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}
			caller := apiKeyCaller(fake)
			resp, err := caller.CallRaw(context.Background(), api, map[string]any{"apiKeyID": "key-1", "body.status": "inactive", "ShowValue": true})
			if err != nil {
				t.Fatal(err)
			}
			if _, present := resp["apiKey"].(map[string]any)["apiKeyValue"]; present {
				t.Fatalf("%s revealed plaintext despite always-mask contract: %+v", api, resp)
			}
		})
	}
}

func TestFCSandboxApiKeyWorkflow(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	describe := `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `},"permissionPolicy":{"schemaVersion":1,"allow":{"actions":["sandbox:*"]}},"effectivePermissions":{"actions":["sandbox:*"]}}`
	for _, tc := range []struct {
		action, api string
		input       map[string]any
		responses   []string
		wantCalls   int
	}{
		{"get", "DescribeApiKey", map[string]any{"id": "key-1"}, []string{describe}, 1},
		{"create", "CreateApiKey", map[string]any{"name": "dev", "team_id": "team-1"}, []string{`{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}, 1},
		{"reset", "ResetApiKey", map[string]any{"id": "key-1", "show_value": true}, []string{`{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}, 1},
		{"update", "UpdateApiKey", map[string]any{"id": "key-1", "status": "inactive"}, []string{`{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`, describe}, 2},
		{"list", "ListApiKeys", map[string]any{"limit": 50, "page": 1, "team_id": "team-1"}, []string{`{"code":"200","requestId":"req-1","apiKeys":[{` + apiKeyObject + `}],"pageNumber":1,"pageSize":50,"total":1}`}, 1},
		{"delete", "DescribeApiKey", map[string]any{"id": "key-1"}, []string{describe, `{"code":"200","requestId":"req-deactivate","apiKey":{` + apiKeyObject + `}}`, `{"code":"200","message":"success","requestId":"req-delete"}`, `{"code":"ApiKeyNotFound","message":"gone","requestId":"req-absent"}`}, 4},
	} {
		t.Run(tc.action, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{responses: tc.responses}
			result, err := engine.NewExecutor(resource, apiKeyCaller(fake)).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
			if err != nil {
				t.Fatal(err)
			}
			if len(fake.requests) != tc.wantCalls || fake.requests[0].ApiName != tc.api || result.Actions[0].RequestID != "req-1" {
				t.Fatalf("unexpected action trace: result=%+v requests=%+v", result, fake.requests)
			}
			switch tc.action {
			case "delete":
				if fake.requests[1].ApiName != "UpdateApiKey" || fake.requests[2].ApiName != "DeleteApiKey" || fake.requests[3].ApiName != "DescribeApiKey" || !result.Deleted || result.Item["id"] != "key-1" {
					t.Fatalf("delete absence evidence = %+v requests=%+v", result, fake.requests)
				}
				body, ok := fake.requests[1].BodyValue().(map[string]any)
				if !ok || body["status"] != "inactive" || fake.requests[1].PathParams["apiKeyID"] != "key-1" {
					t.Fatalf("delete must deactivate the active key first: %+v", fake.requests[1])
				}
				last := result.Actions[len(result.Actions)-1]
				if last.ActionName != "DescribeApiKey" || last.Code != "ApiKeyNotFound" {
					t.Fatalf("delete absence action = %+v", last)
				}
			case "list":
				if len(result.Items) != 1 || result.Items[0]["id"] != "key-1" || result.Items[0]["team_id"] != "team-1" || result.Total != 1 {
					t.Fatalf("list result = %+v", result)
				}
				if _, present := result.Items[0]["api_key_value"]; present {
					t.Fatalf("list workflow leaked plaintext: %+v", result.Items[0])
				}
			case "create":
				if result.Item["id"] != "key-1" || result.Item["api_key_mask"] != "e2b_xxxx****xxxx" {
					t.Fatalf("create result = %+v", result)
				}
				if _, present := result.Item["api_key_value"]; present {
					t.Fatalf("create workflow leaked plaintext without --show-value: %+v", result.Item)
				}
			case "reset":
				if result.Item["id"] != "key-1" || result.Item["api_key_value"] != "e2b_plaintext_secret" {
					t.Fatalf("reset with show_value must reveal plaintext: %+v", result.Item)
				}
			case "get":
				if result.Item["id"] != "key-1" || result.Item["ip_whitelist"].([]any)[0].(map[string]any)["ipAddress"] != "203.0.113.10" {
					t.Fatalf("get result = %+v", result)
				}
				if _, present := result.Item["api_key_value"]; present {
					t.Fatalf("get workflow leaked plaintext: %+v", result.Item)
				}
				if result.Extra["permission_policy"] == nil || result.Extra["effective_permissions"] == nil {
					t.Fatalf("get lost permission fields: %+v", result.Extra)
				}
			case "update":
				if fake.requests[1].ApiName != "DescribeApiKey" || result.Item["id"] != "key-1" {
					t.Fatalf("update read-back = %+v requests=%+v", result, fake.requests)
				}
			}
		})
	}
}

func TestFCSandboxApiKeyDeleteInactiveSkipsDeactivation(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	inactive := `{"code":"200","requestId":"req-1","apiKey":{` + strings.Replace(apiKeyObject, `"status":"active"`, `"status":"inactive"`, 1) + `}}`
	fake := &fakeOpenAPIExecutor{responses: []string{
		inactive,
		`{"code":"200","message":"success","requestId":"req-delete"}`,
		`{"code":"ApiKeyNotFound","message":"gone","requestId":"req-absent"}`,
	}}
	result, err := engine.NewExecutor(resource, apiKeyCaller(fake)).Execute(context.Background(), engine.Request{Action: "delete", Input: map[string]any{"id": "key-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 3 || fake.requests[0].ApiName != "DescribeApiKey" || fake.requests[1].ApiName != "DeleteApiKey" || fake.requests[2].ApiName != "DescribeApiKey" {
		t.Fatalf("inactive delete must skip UpdateApiKey: %+v", fake.requests)
	}
	if !result.Deleted || result.Item["id"] != "key-1" {
		t.Fatalf("inactive delete result = %+v", result)
	}
}

func TestFCSandboxApiKeyDeleteAbsentFailsBeforeMutation(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{responses: []string{`{"code":"ApiKeyNotFound","message":"gone","requestId":"req-absent"}`}}
	_, err = engine.NewExecutor(resource, apiKeyCaller(fake)).Execute(context.Background(), engine.Request{Action: "delete", Input: map[string]any{"id": "key-1"}})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.ExitCode() != 4 {
		t.Fatalf("absent delete must fail as not found before mutation: %v", err)
	}
	if len(fake.requests) != 1 || fake.requests[0].ApiName != "DescribeApiKey" {
		t.Fatalf("absent delete must not issue UpdateApiKey/DeleteApiKey: %+v", fake.requests)
	}
}

func TestFCSandboxApiKeyCreateRevealWorkflow(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "api-key")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","apiKey":{` + apiKeyObject + `}}`}
	result, err := engine.NewExecutor(resource, apiKeyCaller(fake)).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"name": "dev", "team_id": "team-1", "show_value": true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Item["api_key_value"] != "e2b_plaintext_secret" {
		t.Fatalf("create with show_value must reveal plaintext: %+v", result.Item)
	}
	if _, present := fake.requests[0].QueryParams["ShowValue"]; present {
		t.Fatalf("ShowValue leaked into query: %+v", fake.requests[0].QueryParams)
	}
}

func TestFCSandboxApiKeyRejectsBusinessAndMalformedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, api, response, code string
		values                    map[string]any
	}{
		{"business create failure", "CreateApiKey", `{"code":"InvalidParameter","message":"bad name","requestId":"req-1"}`, "CloudAPIError", map[string]any{"body.apiKeyName": "dev"}},
		{"business delete failure", "DeleteApiKey", `{"code":"ApiKeyInUse","message":"in use","requestId":"req-1"}`, "CloudAPIError", map[string]any{"apiKeyID": "key-1"}},
		{"missing code", "DeleteApiKey", `{"requestId":"req-1"}`, "InvalidApiKeyResponse", map[string]any{"apiKeyID": "key-1"}},
		{"numeric code", "DeleteApiKey", `{"code":200,"requestId":"req-1"}`, "InvalidApiKeyResponse", map[string]any{"apiKeyID": "key-1"}},
		{"missing get key", "DescribeApiKey", `{"code":"200","requestId":"req-1"}`, "InvalidApiKeyResponse", map[string]any{"apiKeyID": "key-1"}},
		{"wrong key identity", "DescribeApiKey", `{"code":"200","apiKey":{"apiKeyID":"key-2","status":"active"}}`, "InvalidApiKeyResponse", map[string]any{"apiKeyID": "key-1"}},
		{"missing status", "DescribeApiKey", `{"code":"200","apiKey":{"apiKeyID":"key-1"}}`, "InvalidApiKeyResponse", map[string]any{"apiKeyID": "key-1"}},
		{"missing list", "ListApiKeys", `{"code":"200","requestId":"req-1"}`, "InvalidApiKeyResponse", map[string]any{"pageNumber": 1, "pageSize": 50}},
		{"bad list page", "ListApiKeys", `{"code":"200","apiKeys":[],"pageNumber":0,"pageSize":50,"total":0}`, "InvalidApiKeyResponse", map[string]any{"pageNumber": 1, "pageSize": 50}},
		{"too many keys for page", "ListApiKeys", `{"code":"200","apiKeys":[{` + apiKeyObject + `},{` + apiKeyObject + `}],"pageNumber":1,"pageSize":1,"total":2}`, "InvalidApiKeyResponse", map[string]any{"pageNumber": 1, "pageSize": 1}},
		{"duplicate key", "ListApiKeys", `{"code":"200","apiKeys":[{` + apiKeyObject + `},{` + apiKeyObject + `}],"pageNumber":1,"pageSize":50,"total":2}`, "InvalidApiKeyResponse", map[string]any{"pageNumber": 1, "pageSize": 50}},
		{"key absent", "DescribeApiKey", `{"code":"ApiKeyNotFound","message":"gone","requestId":"req-1"}`, "NotFound", map[string]any{"apiKeyID": "key-1"}},
		{"key absent HTTP code", "DescribeApiKey", `{"code":"404","message":"gone","requestId":"req-1"}`, "NotFound", map[string]any{"apiKeyID": "key-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := apiKeyCaller(&fakeOpenAPIExecutor{response: tc.response})
			_, err := caller.CallRaw(context.Background(), tc.api, tc.values)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if tc.code == "NotFound" {
				action := ecerrors.ActionFromError(tc.api, err)
				if app.ExitCode() != 4 || action.ActionName != "DescribeApiKey" || action.RequestID != "req-1" {
					t.Fatalf("absence action = %+v, exit=%d", action, app.ExitCode())
				}
			}
		})
	}
}

func TestFCSandboxApiKeyHTTPAbsenceIsScopedToDescribe(t *testing.T) {
	for _, tc := range []struct {
		api, code, kind string
	}{
		{"DescribeApiKey", "ApiKeyNotFound", "not_found"},
		{"DescribeApiKey", "404", "not_found"},
		{"DescribeApiKey", "TeamNotFound", "service"},
		{"DeleteApiKey", "ApiKeyNotFound", "service"},
		{"UpdateApiKey", "ApiKeyNotFound", "service"},
	} {
		t.Run(tc.api+"/"+tc.code, func(t *testing.T) {
			caller := apiKeyCaller(&fakeOpenAPIExecutor{callError: errors.New("SDK.ServerError\nErrorCode: " + tc.code + "\nMessage: missing\nRequestId: req-missing")})
			values := map[string]any{"apiKeyID": "key-1"}
			if tc.api == "UpdateApiKey" {
				values["body.status"] = "inactive"
			}
			_, err := caller.CallRaw(context.Background(), tc.api, values)
			var app *ecerrors.AppError
			action := ecerrors.ActionFromError(tc.api, err)
			if !errors.As(err, &app) || app.Payload().Kind != tc.kind || action.Code != tc.code || action.RequestID != "req-missing" {
				t.Fatalf("HTTP absence = %v, action=%+v; want %s", err, action, tc.kind)
			}
		})
	}
}
