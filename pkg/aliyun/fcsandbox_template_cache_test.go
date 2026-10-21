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

func TestFCSandboxTemplateCacheStatusReasonRedactsURLUserinfo(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"https", "HTTPS", "HtTpS", "hTtP"} {
		t.Run(scheme, func(t *testing.T) {
			credentialURL := scheme + "://user-secret:password-secret@registry.example/image?mode=warm"
			cache := `{"templateID":"tpl-1","teamID":"team-1","status":"Success","statusReason":"` + credentialURL + `"}`
			for _, tc := range []struct {
				name, action, response string
				input                  map[string]any
				item                   func(engine.Result) map[string]any
			}{
				{"get", "get", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
				{"list", "list", `{"code":"200","templateCaches":[` + cache + `]}`, map[string]any{"team_id": "team-1", "template_id": "tpl-1"}, func(result engine.Result) map[string]any { return result.Items[0] }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: tc.response}}
					result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
					if err != nil {
						t.Fatal(err)
					}
					reason, _ := tc.item(result)["status_reason"].(string)
					if strings.Contains(reason, "user-secret") || strings.Contains(reason, "password-secret") || !strings.Contains(reason, "registry.example/image?mode=warm") {
						t.Fatal("status_reason did not safely retain the URL without userinfo")
					}
				})
			}
			t.Run("failed create wait", func(t *testing.T) {
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: []string{
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Failed","statusReason":"` + credentialURL + `"}}`,
				}}}
				_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" {
					t.Fatalf("failed create did not return WaitFailed: %v", err)
				}
				detail := app.Payload().Detail
				if strings.Contains(detail, "user-secret") || strings.Contains(detail, "password-secret") || !strings.Contains(detail, "registry.example/image?mode=warm") {
					t.Fatal("failed create wait detail exposed URL userinfo")
				}
			})
		})
	}
	for _, raw := range []string{
		"https://user-secret:password-secret@registry.example/%zz",
		"https://user-secret:password-secret@registry.example/image?SecurityToken=token-secret",
	} {
		sanitized := callerSanitizeCloudError(errors.New(raw))
		if strings.Contains(sanitized, "user-secret") || strings.Contains(sanitized, "password-secret") || strings.Contains(sanitized, "token-secret") {
			t.Fatal("sanitizer exposed URL userinfo or query credential")
		}
	}
}

func TestFCSandboxTemplateCacheStatusReasonRedactsURLCredentials(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, suffix string }{
		{"password", "?password=reviewer-canary"},
		{"token", "?token=reviewer-canary"},
		{"access_token", "?access_token=reviewer-canary"},
		{"refreshToken", "?refreshToken=reviewer-canary"},
		{"api_key", "?api_key=reviewer-canary"},
		{"client_secret", "?client_secret=reviewer-canary"},
		{"sas_query_signature", "?sp=r&sig=reviewer-canary"},
		{"oss_v4_credential", "?x-oss-credential=LTAIreviewer-canary%2F20261021%2Fcn-hangzhou%2Foss%2Faliyun_v4_request"},
		{"oss_v4_signature", "?x-oss-signature=reviewer-canary"},
		{"oss_v4_security_token", "?x-oss-security-token=reviewer-canary"},
		{"oss_v4_combined", "?x-oss-date=20261021T000000Z&x-oss-credential=LTAIreviewer-canary%2F20261021%2Fcn-hangzhou%2Foss%2Faliyun_v4_request&x-oss-signature=reviewer-canary&x-oss-security-token=reviewer-canary"},
		{"s3_v4_signature", "?X-Amz-Signature=reviewer-canary"},
		{"gcs_v4_signature", "?X-Goog-Signature=reviewer-canary"},
		{"fragment_token", "#token=reviewer-canary"},
		{"fragment_password", "#password=reviewer-canary"},
		{"fragment_signature", "#sig=reviewer-canary"},
		{"fragment_after_query", "?mode=warm#section?access_token=reviewer-canary"},
		{"fragment_nested_signature", "?mode=warm#section?sig=reviewer-canary"},
		{"fragment_before_nested_query", "#token=reviewer-canary?mode=warm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentialURL := "https://registry.example/image" + tc.suffix
			cache := `{"templateID":"tpl-1","teamID":"team-1","status":"Success","statusReason":"` + credentialURL + `"}`
			for _, action := range []struct {
				name, action, response string
				input                  map[string]any
				item                   func(engine.Result) map[string]any
			}{
				{"get", "get", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
				{"list", "list", `{"code":"200","templateCaches":[` + cache + `]}`, map[string]any{"team_id": "team-1", "template_id": "tpl-1"}, func(result engine.Result) map[string]any { return result.Items[0] }},
			} {
				t.Run(action.name, func(t *testing.T) {
					caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: action.response}}
					result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: action.action, Input: action.input})
					if err != nil {
						t.Fatal(err)
					}
					reason, _ := action.item(result)["status_reason"].(string)
					if reason == "" || strings.Contains(reason, "reviewer-canary") {
						t.Fatalf("status_reason exposed URL query credential: %q", reason)
					}
					encoded, err := json.Marshal(result)
					if err != nil || strings.Contains(string(encoded), "reviewer-canary") {
						t.Fatalf("result JSON exposed URL query credential: %s, %v", encoded, err)
					}
				})
			}
			t.Run("failed create wait", func(t *testing.T) {
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: []string{
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Failed","statusReason":"` + credentialURL + `"}}`,
				}}}
				_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" {
					t.Fatalf("failed create did not return WaitFailed: %v", err)
				}
				if detail := app.Payload().Detail; detail == "" || strings.Contains(detail, "reviewer-canary") {
					t.Fatalf("failed create wait detail exposed URL query credential: %q", detail)
				}
			})
		})
	}
	malformed := callerSanitizeCloudError(errors.New("https://registry.example/image?password=reviewer-canary%zz"))
	if strings.Contains(malformed, "reviewer-canary") {
		t.Fatalf("malformed URL query exposed credential: %q", malformed)
	}
	for _, ordinary := range []string{
		"https://registry.example/image#section",
		"https://registry.example/image?mode=warm#section",
	} {
		if got := callerSanitizeCloudError(errors.New(ordinary)); got != ordinary {
			t.Fatalf("ordinary URL changed: %q", got)
		}
	}
}

func TestFCSandboxTemplateCacheStatusReasonRedactsBareCredentials(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, reason string }{
		{"password", "image pull failed: password=reviewer-canary retry_after=30"},
		{"Password", "image pull failed: Password=reviewer-canary retry_after=30"},
		{"passwd", "image pull failed: passwd=reviewer-canary retry_after=30"},
		{"pwd", "image pull failed: pwd=reviewer-canary retry_after=30"},
		{"token", "image pull failed: token=reviewer-canary retry_after=30"},
		{"api_key", "image pull failed: api_key=reviewer-canary retry_after=30"},
		{"api-key", "image pull failed: api-key=reviewer-canary retry_after=30"},
		{"apiKey", "image pull failed: apiKey=reviewer-canary retry_after=30"},
		{"client_secret", "image pull failed: client_secret=reviewer-canary retry_after=30"},
		{"relative_signed_target", "image pull failed: GET /v2/image?sp=r&sig=reviewer-canary retry_after=30"},
		{"bare_signed_target", "image pull failed: image?sig=reviewer-canary retry_after=30"},
		{"bare_sig_assignment", "image pull failed: SIG=reviewer-canary retry_after=30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := `{"templateID":"tpl-1","teamID":"team-1","status":"Failed","statusReason":"` + tc.reason + `"}`
			for _, action := range []struct {
				name, command, response string
				input                   map[string]any
				item                    func(engine.Result) map[string]any
			}{
				{"get", "get", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
				{"list", "list", `{"code":"200","templateCaches":[` + cache + `]}`, map[string]any{"team_id": "team-1", "template_id": "tpl-1"}, func(result engine.Result) map[string]any { return result.Items[0] }},
			} {
				t.Run(action.name, func(t *testing.T) {
					caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: action.response}}
					result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: action.command, Input: action.input})
					if err != nil {
						t.Fatal(err)
					}
					statusReason, _ := action.item(result)["status_reason"].(string)
					encoded, err := json.Marshal(result)
					if err != nil || statusReason != "[REDACTED]" || strings.Contains(string(encoded), "reviewer-canary") {
						t.Fatalf("status_reason did not fail closed: %q, JSON=%s, err=%v", statusReason, encoded, err)
					}
				})
			}
			t.Run("failed create wait", func(t *testing.T) {
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: []string{
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
					`{"code":"200","templateCache":` + cache + `}`,
				}}}
				_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" {
					t.Fatalf("failed create did not return WaitFailed: %v", err)
				}
				public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "actions": app.Actions(), "action": ecerrors.ActionFromError("CreateTemplateCache", err), "message": err.Error()})
				if detail := app.Payload().Detail; detail != "[REDACTED]" || marshalErr != nil || strings.Contains(string(public), "reviewer-canary") {
					t.Fatalf("failed create wait did not fail closed: detail=%q, JSON=%s, err=%v", detail, public, marshalErr)
				}
			})
		})
	}
	ordinary := "image pull failed: retry_after=30"
	if got := callerSanitizeCloudError(errors.New(ordinary)); got != ordinary {
		t.Fatalf("ordinary diagnostic changed: %q", got)
	}
}

func TestFCSandboxTemplateCacheStatusReasonRedactsQuotedAssignments(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, reason string }{
		{"password", `pull failed Password="reviewer-canary"`},
		{"token", `pull failed Token="reviewer-canary"`},
		{"single_quoted_token", `pull failed Token='reviewer-canary'`},
		{"spaced_api_key", `pull failed api_key = "reviewer-canary"`},
		{"unquoted_password_with_spaces", `pull failed Password=reviewer-canary-blue reviewer-canary-river`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache, err := json.Marshal(map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Failed", "statusReason": tc.reason})
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []struct {
				name, command, response string
				input                   map[string]any
				item                    func(engine.Result) map[string]any
			}{
				{"get", "get", `{"code":"200","templateCache":` + string(cache) + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
				{"list", "list", `{"code":"200","templateCaches":[` + string(cache) + `]}`, map[string]any{"team_id": "team-1", "template_id": "tpl-1"}, func(result engine.Result) map[string]any { return result.Items[0] }},
			} {
				t.Run(action.name, func(t *testing.T) {
					caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: action.response}}
					result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: action.command, Input: action.input})
					if err != nil {
						t.Fatal(err)
					}
					reason, _ := action.item(result)["status_reason"].(string)
					encoded, err := json.Marshal(result)
					if err != nil || reason == "" || strings.Contains(reason, "reviewer-canary") || strings.Contains(string(encoded), "reviewer-canary") {
						t.Fatalf("status_reason exposed a quoted credential or was empty: %q, JSON=%s, err=%v", reason, encoded, err)
					}
				})
			}
			t.Run("failed create wait", func(t *testing.T) {
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: []string{
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
					`{"code":"200","templateCache":` + string(cache) + `}`,
				}}}
				_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" {
					t.Fatalf("failed create did not return WaitFailed: %v", err)
				}
				public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "actions": app.Actions(), "action": ecerrors.ActionFromError("CreateTemplateCache", err), "message": err.Error()})
				if detail := app.Payload().Detail; detail == "" || strings.Contains(detail, "reviewer-canary") || marshalErr != nil || strings.Contains(string(public), "reviewer-canary") {
					t.Fatalf("failed create wait exposed a credential: detail=%q, JSON=%s, err=%v", detail, public, marshalErr)
				}
			})
		})
	}
}

func TestFCSandboxTemplateCacheBusinessErrorsRedactCredentials(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, action string
		input        map[string]any
	}{
		{"get", "get", map[string]any{"id": "tpl-1", "team_id": "team-1"}},
		{"list", "list", map[string]any{"team_id": "team-1", "template_id": "tpl-1"}},
		{"create", "create", map[string]any{"template_id": "tpl-1", "team_id": "team-1"}},
		{"delete", "delete", map[string]any{"id": "tpl-1", "team_id": "team-1"}},
	} {
		for _, diagnostic := range []struct{ name, message, want string }{
			{"quoted_password", `provider rejected Password="reviewer-canary-blue reviewer-canary-river"`, "[REDACTED]"},
			{"quoted_token", `provider rejected Token="reviewer-canary-blue reviewer-canary-river"`, "[REDACTED]"},
			{"bare_password", `provider rejected Password=reviewer-canary-blue reviewer-canary-river`, "[REDACTED]"},
			{"signature_colon", "provider rejected Signature: reviewer-canary", "[REDACTED]"},
			{"access_key_id_colon", "provider rejected AccessKeyId: reviewer-canary", "[REDACTED]"},
			{"authorization_assignment", "provider rejected Authorization=Bearer reviewer-canary-blue reviewer-canary-river", "[REDACTED]"},
			{"authorization_spaced_assignment", "provider rejected authorization = Bearer reviewer-canary", "[REDACTED]"},
			{"bare_bearer", "provider rejected Bearer reviewer-canary-blue reviewer-canary-river", "[REDACTED]"},
			{"bare_basic", "provider rejected Basic dXNlcjpwYXNz", "[REDACTED]"},
			{"relative_signed_target", "provider rejected GET /v2/image?sp=r&sig=reviewer-canary retry_after=30", "[REDACTED]"},
			{"bare_signed_target", "provider rejected image?SIG=reviewer-canary retry_after=30", "[REDACTED]"},
			{"bare_sig_assignment", "provider rejected sig=reviewer-canary retry_after=30", "[REDACTED]"},
			{"ordinary", "provider not ready", "provider not ready"},
			{"ordinary_colon", "provider status: warming failed", "provider status: warming failed"},
		} {
			t.Run(tc.name+"/"+diagnostic.name, func(t *testing.T) {
				body, err := json.Marshal(map[string]any{"code": "409", "message": diagnostic.message, "requestId": "req-1"})
				if err != nil {
					t.Fatal(err)
				}
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: string(body)}}
				_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "CloudAPIError" {
					t.Fatal("expected CloudAPIError")
				}
				action := ecerrors.ActionFromError(tc.action, err)
				if action.Code != "409" || action.RequestID != "req-1" || action.Message != diagnostic.want {
					t.Fatal("business action metadata changed")
				}
				public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "actions": app.Actions(), "action": action, "message": err.Error()})
				if marshalErr != nil || strings.Contains(string(public), "reviewer-canary") || strings.Contains(string(public), "dXNlcjpwYXNz") {
					t.Fatal("business error exposed a credential")
				}
			})
		}
	}
}

func TestFCSandboxTemplateCacheStatusReasonRedactsCredentials(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, reason, want string }{
		{"password", "image pull failed: password: reviewer-canary", ""},
		{"api_key", "image pull failed: api_key: reviewer-canary", ""},
		{"authorization_bearer", "image pull failed: Authorization: Bearer reviewer-canary", "[REDACTED]"},
		{"authorization_assignment", "image pull failed: Authorization=Bearer reviewer-canary-blue reviewer-canary-river", "[REDACTED]"},
		{"authorization_spaced_assignment", "image pull failed: authorization = Bearer reviewer-canary", "[REDACTED]"},
		{"bare_bearer", "image pull failed: Bearer reviewer-canary-blue reviewer-canary-river", "[REDACTED]"},
		{"bare_basic", "image pull failed: Basic dXNlcjpwYXNz", "[REDACTED]"},
		{"bearer_token_assignment", "image pull failed: BearerToken=reviewer-canary", "[REDACTED]"},
		{"signature", "image pull failed: Signature: reviewer-canary", "[REDACTED]"},
		{"access_key_id", "image pull failed: AccessKeyId: reviewer-canary", "[REDACTED]"},
		{"signature_nonce", "image pull failed: SignatureNonce : reviewer-canary", "[REDACTED]"},
		{"ordinary_colon", "image pull failed: registry unavailable", "image pull failed: registry unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := `{"templateID":"tpl-1","teamID":"team-1","status":"Failed","statusReason":"` + tc.reason + `"}`
			for _, action := range []struct {
				name, command, response string
				input                   map[string]any
				item                    func(engine.Result) map[string]any
			}{
				{"get", "get", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
				{"list", "list", `{"code":"200","templateCaches":[` + cache + `]}`, map[string]any{"team_id": "team-1", "template_id": "tpl-1"}, func(result engine.Result) map[string]any { return result.Items[0] }},
			} {
				t.Run(action.name, func(t *testing.T) {
					caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: action.response}}
					result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: action.command, Input: action.input})
					if err != nil {
						t.Fatal(err)
					}
					reason, _ := action.item(result)["status_reason"].(string)
					encoded, err := json.Marshal(result)
					if err != nil || reason == "" || strings.Contains(reason, "reviewer-canary") || strings.Contains(string(encoded), "reviewer-canary") || strings.Contains(string(encoded), "dXNlcjpwYXNz") || tc.want != "" && reason != tc.want {
						t.Fatal("status_reason exposed credential or was empty")
					}
				})
			}
			t.Run("failed create wait", func(t *testing.T) {
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: []string{
					`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
					`{"code":"200","templateCache":` + cache + `}`,
				}}}
				_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "WaitFailed" {
					t.Fatal("failed create did not return WaitFailed")
				}
				public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "actions": app.Actions(), "action": ecerrors.ActionFromError("CreateTemplateCache", err), "message": err.Error()})
				if detail := app.Payload().Detail; detail == "" || tc.want != "" && detail != tc.want || marshalErr != nil || strings.Contains(string(public), "reviewer-canary") || strings.Contains(string(public), "dXNlcjpwYXNz") {
					t.Fatal("failed create wait exposed credential or lost detail")
				}
			})
		})
	}
}

func TestFCSandboxTemplateCacheCreateResponseDiagnosticsDoNotLeakValues(t *testing.T) {
	for _, tc := range []struct {
		name, response, field, detail string
	}{
		{"missing code", `{"requestId":"request-secret","message":"message-secret"}`, "code", "missing"},
		{"wrong request ID type", `{"code":"200","requestId":false}`, "requestId", "expected_string_got_boolean"},
		{"numeric code", `{"code":200,"requestId":"request-secret"}`, "code", "expected_string_got_number"},
		{"missing cache", `{"code":"200","requestId":"request-secret"}`, "templateCache", "missing"},
		{"array cache", `{"code":"200","templateCache":["value-secret"],"requestId":"request-secret"}`, "templateCache", "expected_object_got_array"},
		{"missing template", `{"code":"200","templateCache":{"teamID":"team-1","status":"Success","statusReason":"message-secret"},"requestId":"request-secret"}`, "templateCache.templateID", "missing"},
		{"wrong team", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-secret","status":"Success"},"requestId":"request-secret"}`, "templateCache.teamID", "identity_mismatch"},
		{"numeric status", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":1},"requestId":"request-secret"}`, "templateCache.status", "expected_string_got_number"},
		{"unknown status", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"state-secret"},"requestId":"request-secret"}`, "templateCache.status", "invalid_enum"},
		{"invalid progress type", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Success","progress":"value-secret"},"requestId":"request-secret"}`, "templateCache.progress", "expected_nonnegative_integer_got_string"},
		{"invalid reason type", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Success","statusReason":{"key-secret":"value-secret"}},"requestId":"request-secret"}`, "templateCache.statusReason", "expected_string_got_object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := strings.ReplaceAll(tc.response, "request-secret", "req-123")
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: response}}
			_, err := caller.CallRaw(context.Background(), "CreateTemplateCache", map[string]any{"body.templateID": "tpl-1", "body.teamID": "team-1"})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheResponse" || app.Payload().Field != tc.field || app.Payload().Detail != tc.detail {
				t.Fatalf("diagnostic = %+v, err=%v; want field=%q detail=%q", app, err, tc.field, tc.detail)
			}
			action := ecerrors.ActionFromError("CreateTemplateCache", app)
			wantID := ""
			if strings.Contains(response, `"requestId":"req-123"`) {
				wantID = "req-123"
			}
			if action.RequestID != wantID {
				t.Fatalf("invalid response request ID = %q, want %q", action.RequestID, wantID)
			}
			serialized, err := json.Marshal(map[string]any{"error": app.Payload(), "action": action})
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"request-secret", "message-secret", "team-secret", "value-secret", "state-secret", "key-secret", "tpl-1", "team-1"} {
				if strings.Contains(string(serialized), secret) || strings.Contains(app.Error(), secret) {
					t.Fatalf("diagnostic exposed response value %q: %s", secret, serialized)
				}
			}
		})
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"request-secret","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Success"}}`}}
	if response, err := caller.CallRaw(context.Background(), "CreateTemplateCache", map[string]any{"body.templateID": "tpl-1", "body.teamID": "team-1"}); err != nil || response == nil {
		t.Fatalf("valid create response rejected: response=%v err=%v", response, err)
	}
}

func TestFCSandboxTemplateCacheInvalidResponseRetainsRequestID(t *testing.T) {
	for _, tc := range []struct {
		name, api, response string
		request             map[string]any
	}{
		{"create", "CreateTemplateCache", `{"code":"200","requestId":"req-123","message":"message-secret"}`, map[string]any{"body.templateID": "tpl-1", "body.teamID": "team-1"}},
		{"get", "DescribeTemplateCache", `{"code":"200","requestId":"req-123","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"ready-secret"}}`, map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
		{"list", "ListTemplateCache", `{"code":"200","requestId":"req-123","templateCaches":[],"nextToken":false,"message":"message-secret"}`, map[string]any{"teamID": "team-1"}},
		{"delete", "DeleteTemplateCache", `{"code":200,"requestId":"req-123","message":"message-secret"}`, map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: tc.response}}
			_, err := caller.CallRaw(context.Background(), tc.api, tc.request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheResponse" {
				t.Fatalf("invalid response was accepted: %v", err)
			}
			action := ecerrors.ActionFromError(tc.api, app)
			serialized, err := json.Marshal(map[string]any{"error": app.Payload(), "action": action})
			if err != nil || action.RequestID != "req-123" || !strings.Contains(string(serialized), `"request_id":"req-123"`) || strings.Contains(string(serialized), "message-secret") || strings.Contains(string(serialized), "ready-secret") {
				t.Fatalf("invalid response diagnostic lost request ID or exposed data: action=%+v output=%s err=%v", action, serialized, err)
			}
		})
	}
}

func TestFCSandboxTemplateCacheROARequestContract(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	for _, tc := range []struct {
		api, method, path string
		values            map[string]any
	}{
		{"CreateTemplateCache", "POST", "/pop/2026-05-09/template-caches", map[string]any{"body.teamID": "team-1", "body.templateID": "tpl-1"}},
		{"DescribeTemplateCache", "GET", "/pop/2026-05-09/template-caches/tpl-1", map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
		{"ListTemplateCache", "GET", "/pop/2026-05-09/template-caches", map[string]any{"teamID": "team-1", "templateID": "tpl-1", "status": "InProgress", "maxResults": 2, "nextToken": "opaque-token"}},
		{"DeleteTemplateCache", "DELETE", "/pop/2026-05-09/template-caches/tpl-1", map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
	} {
		t.Run(tc.api, func(t *testing.T) {
			req, err := caller.commonRequest(tc.api, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-hangzhou.aliyuncs.com" || req.Version != "2026-05-09" {
				t.Fatalf("ROA request = %+v", req)
			}
			if tc.api == "CreateTemplateCache" {
				body, ok := req.BodyValue().(map[string]any)
				if !ok || body["teamID"] != "team-1" || body["templateID"] != "tpl-1" || req.QueryParams["teamID"] != "" {
					t.Fatalf("create body or scope = %+v", req)
				}
			} else if req.QueryParams["teamID"] != "team-1" {
				t.Fatalf("query Team scope = %+v", req)
			}
			if tc.api == "ListTemplateCache" && (req.QueryParams["nextToken"] != "opaque-token" || req.QueryParams["maxResults"] != "2" || req.QueryParams["status"] != "InProgress" || req.QueryParams["templateID"] != "tpl-1") {
				t.Fatalf("list filters and token = %+v", req.QueryParams)
			}
		})
	}
}

func TestFCSandboxTemplateCacheScrubsResponseReason(t *testing.T) {
	for _, tc := range []struct{ name, diagnostic string }{
		{"security_token", "securityToken=private-token"},
		{"authorization_assignment", "Authorization=Bearer private-token trailing-private-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"code": "200", "requestId": tc.diagnostic, "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Failed", "statusReason": "pull failed " + tc.diagnostic}})
			if err != nil {
				t.Fatal(err)
			}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: string(body)}}
			response, err := caller.CallRaw(context.Background(), "DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
			if err != nil {
				t.Fatal(err)
			}
			if response["requestId"] != "" || response["templateCache"].(map[string]any)["statusReason"] != "[REDACTED]" {
				t.Fatalf("provider response leaked credential text: %#v", response)
			}
		})
	}
}

func TestFCSandboxTemplateCacheScrubsBusinessEnvelopeAuthorization(t *testing.T) {
	response := `{"code":"Authorization=Bearer private-token","message":"provider failed","requestId":"Authorization=Bearer private-token"}`
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: response}}
	_, err := caller.CallRaw(context.Background(), "DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "CloudAPIError" {
		t.Fatalf("business failure was not retained: %v", err)
	}
	action := ecerrors.ActionFromError("DescribeTemplateCache", err)
	public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "action": action, "message": err.Error()})
	if marshalErr != nil || action.Code != "[REDACTED]" || action.RequestID != "" || strings.Contains(string(public), "private-token") {
		t.Fatalf("business envelope leaked bearer authorization: action=%+v public=%s err=%v", action, public, marshalErr)
	}
}

func TestFCSandboxTemplateCacheRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct{ name, api, body, code, kind string }{
		{"business failure", "CreateTemplateCache", `{"code":"409","message":"not ready","requestId":"req-1"}`, "CloudAPIError", "service"},
		{"missing code", "DeleteTemplateCache", `{"message":"success"}`, "InvalidTemplateCacheResponse", "service"},
		{"wrong code type", "DeleteTemplateCache", `{"code":200}`, "InvalidTemplateCacheResponse", "service"},
		{"wrong request ID type", "DeleteTemplateCache", `{"code":"200","requestId":false}`, "InvalidTemplateCacheResponse", "service"},
		{"missing created cache", "CreateTemplateCache", `{"code":"200"}`, "InvalidTemplateCacheResponse", "service"},
		{"wrong template", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-other","teamID":"team-1","status":"Success"}}`, "InvalidTemplateCacheResponse", "service"},
		{"wrong Team", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"other","status":"Success"}}`, "InvalidTemplateCacheResponse", "service"},
		{"template build state", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}}`, "InvalidTemplateCacheResponse", "service"},
		{"unknown cache state", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"unknown"}}`, "InvalidTemplateCacheResponse", "service"},
		{"missing list", "ListTemplateCache", `{"code":"200","nextToken":""}`, "InvalidTemplateCacheResponse", "service"},
		{"wrong token type", "ListTemplateCache", `{"code":"200","templateCaches":[],"nextToken":false}`, "InvalidTemplateCacheResponse", "service"},
		{"duplicate template", "ListTemplateCache", `{"code":"200","templateCaches":[{"templateID":"tpl-1","teamID":"team-1","status":"Success"},{"templateID":"tpl-1","teamID":"team-1","status":"Failed"}]}`, "InvalidTemplateCacheResponse", "service"},
		{"cache absent", "DescribeTemplateCache", `{"code":"TemplateCacheNotFound","message":"gone","requestId":"req-1"}`, "NotFound", "not_found"},
		{"template absent is not cache absent", "DescribeTemplateCache", `{"code":"TemplateNotFound","message":"template gone","requestId":"req-1"}`, "CloudAPIError", "service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.body}}
			request := map[string]any{"templateID": "tpl-1", "teamID": "team-1"}
			if tc.api == "CreateTemplateCache" {
				request = map[string]any{"body.templateID": "tpl-1", "body.teamID": "team-1"}
			}
			response, err := caller.CallRaw(context.Background(), tc.api, request)
			var app *ecerrors.AppError
			if response != nil || !errors.As(err, &app) || app.Payload().Code != tc.code || app.Payload().Kind != tc.kind {
				t.Fatalf("response=%#v error=%v want=%s/%s", response, err, tc.code, tc.kind)
			}
		})
	}
}

func TestFCSandboxTemplateCacheAcceptsStatesAndTokenPage(t *testing.T) {
	for _, status := range []string{"InProgress", "Success", "Failed", "Deleting", "Evicted"} {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"` + status + `","progress":50,"imageSizeMB":512}}`}}
		response, err := caller.CallRaw(context.Background(), "DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
		if err != nil || response["templateCache"].(map[string]any)["status"] != status {
			t.Fatalf("status %q lost: %#v %v", status, response, err)
		}
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-1","templateCaches":[{"templateID":"tpl-1","teamID":"team-1","status":"Success"}],"nextToken":"opaque-token","maxResults":2}`}}
	response, err := caller.CallRaw(context.Background(), "ListTemplateCache", map[string]any{"teamID": "team-1", "maxResults": 2})
	if err != nil || response["nextToken"] != "opaque-token" {
		t.Fatalf("page token lost: %#v %v", response, err)
	}
}

func TestFCSandboxTemplateCacheNormalizesProviderReady(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	cache := `{"templateID":"tpl-1","teamID":"team-1","status":"ready"}`
	for _, tc := range []struct {
		name, action, response string
		input                  map[string]any
		item                   func(engine.Result) map[string]any
	}{
		{"get", "get", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"id": "tpl-1", "team_id": "team-1"}, func(result engine.Result) map[string]any { return result.Item }},
		{"list", "list", `{"code":"200","templateCaches":[` + cache + `]}`, map[string]any{"team_id": "team-1", "status": "Success"}, func(result engine.Result) map[string]any { return result.Items[0] }},
		{"create no wait", "create", `{"code":"200","templateCache":` + cache + `}`, map[string]any{"template_id": "tpl-1", "team_id": "team-1", "no_wait": true}, func(result engine.Result) map[string]any { return result.Item }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: tc.response}}
			result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.item(result)["status"]; got != "Success" {
				t.Fatalf("status = %v, want Success", got)
			}
		})
	}
	t.Run("create wait", func(t *testing.T) {
		fake := &fakeOpenAPIExecutor{responses: []string{
			`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
			`{"code":"200","templateCache":` + cache + `}`,
		}}
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: fake}
		result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
		if err != nil || result.Item["status"] != "Success" {
			t.Fatalf("provider ready was not reported as a completed create: result=%+v err=%v", result, err)
		}
		if len(fake.requests) != 2 {
			t.Fatalf("calls = %d, want create and one readiness probe", len(fake.requests))
		}
	})
}

func TestFCSandboxTemplateCacheNormalizesObservedCreatePending(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0.001)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	create := `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"creating"}}`
	t.Run("no wait", func(t *testing.T) {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: create}}
		result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1", "no_wait": true}})
		if err != nil || result.Item["status"] != "InProgress" {
			t.Fatal("create response was not normalized to the pending cache state")
		}
	})
	t.Run("wait", func(t *testing.T) {
		fake := &fakeOpenAPIExecutor{responses: []string{
			create,
			`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"creating"}}`,
			`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Success"}}`,
		}}
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: fake}
		result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"template_id": "tpl-1", "team_id": "team-1"}})
		if err != nil || result.Item["status"] != "Success" || len(fake.requests) != 3 || fake.requests[1].ApiName != "DescribeTemplateCache" || fake.requests[2].ApiName != "DescribeTemplateCache" {
			t.Fatalf("create waiter did not keep Describe creating pending until Success: result=%+v err=%v calls=%d", result, err, len(fake.requests))
		}
	})
	t.Run("get", func(t *testing.T) {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: create}}
		result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "get", Input: map[string]any{"id": "tpl-1", "team_id": "team-1"}})
		if err != nil || result.Item["status"] != "InProgress" {
			t.Fatal("Describe creating was not reported as pending by get")
		}
	})
	for _, tc := range []struct{ name, api, response string }{
		{"describe near miss remains strict", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Creating"}}`},
		{"describe unknown remains strict", "DescribeTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Queued"}}`},
		{"list remains strict", "ListTemplateCache", `{"code":"200","templateCaches":[{"templateID":"tpl-1","teamID":"team-1","status":"creating"}]}`},
		{"create near miss remains strict", "CreateTemplateCache", `{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"Creating"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			_, err := caller.CallRaw(context.Background(), tc.api, map[string]any{"templateID": "tpl-1", "teamID": "team-1", "body.templateID": "tpl-1", "body.teamID": "team-1"})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheResponse" || app.Payload().Detail != "invalid_enum" {
				t.Fatal("unknown cache state was not rejected")
			}
			if strings.Contains(err.Error(), "creating") || strings.Contains(err.Error(), "Creating") {
				t.Fatal("unknown provider state leaked into the error")
			}
		})
	}
}

func TestFCSandboxTemplateCacheRejectsInvalidInputBeforeAPI(t *testing.T) {
	for _, tc := range []struct {
		api     string
		request map[string]any
	}{
		{"CreateTemplateCache", map[string]any{"body.templateID": "tpl/other", "body.teamID": "team-1"}},
		{"DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team other"}},
		{"DeleteTemplateCache", map[string]any{"templateID": "--profile", "teamID": "team-1"}},
		{"ListTemplateCache", map[string]any{"teamID": "team-1", "status": "ready"}},
		{"ListTemplateCache", map[string]any{"teamID": "team-1", "maxResults": 101}},
	} {
		t.Run(tc.api, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","templateCaches":[]}`}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: fake}
			_, err := caller.CallRaw(context.Background(), tc.api, tc.request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheInput" || len(fake.requests) != 0 {
				t.Fatalf("invalid request reached API: request=%+v err=%v calls=%d", tc.request, err, len(fake.requests))
			}
		})
	}
}

func TestFCSandboxTemplateCacheRejectsOutOfFilterListItems(t *testing.T) {
	for _, tc := range []struct {
		name, item string
	}{
		{"template", `{"templateID":"tpl-other","teamID":"team-1","status":"Success"}`},
		{"status", `{"templateID":"tpl-1","teamID":"team-1","status":"Failed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","templateCaches":[` + tc.item + `]}`}}
			_, err := caller.CallRaw(context.Background(), "ListTemplateCache", map[string]any{"teamID": "team-1", "templateID": "tpl-1", "status": "Success"})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheResponse" {
				t.Fatalf("out-of-filter item accepted: %v", err)
			}
		})
	}
}

func TestFCSandboxTemplateCacheRejectsPageOverRequestedLimitWithoutEcho(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","templateCaches":[{"templateID":"tpl-1","teamID":"team-1","status":"Success"},{"templateID":"tpl-2","teamID":"team-1","status":"Success"}]}`}}
	_, err := caller.CallRaw(context.Background(), "ListTemplateCache", map[string]any{"teamID": "team-1", "maxResults": 1})
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Code != "InvalidTemplateCacheResponse" {
		t.Fatalf("overfull page without maxResults echo accepted: %v", err)
	}
}
