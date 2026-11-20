package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	openapiClient "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

type sdkErrorHTTPClient struct {
	status int
	body   string
}

func TestFixtureAbsenceRequiresStructuredHTTP404(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"diagnostic text", errors.New("Error Code: EntityNotExist.Role\nRequest ID: req-spoof"), false},
		{"typed non-404", &tea.SDKError{Code: tea.String("EntityNotExist.Role"), StatusCode: tea.Int(403)}, false},
		{"typed 404", &tea.SDKError{Code: tea.String("EntityNotExist.Role"), StatusCode: tea.Int(404)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := ecerrors.NotFound("NotFound", "role not found", cloudErrorOptions(tc.err)...)
			if got := ecerrors.HasVerifiedProviderResponse(wrapped); got != tc.want {
				t.Fatalf("structured provider proof = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRAMFixtureTypedAbsenceReachesRawCaller(t *testing.T) {
	for _, tc := range []struct{ operation, code string }{
		{"GetRole", "EntityNotExist.Role"}, {"DeleteRole", "EntityNotExist.Role"},
		{"GetPolicy", "EntityNotExist.Policy"}, {"DeletePolicy", "EntityNotExist.Policy"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"Code": tc.code, "RequestId": "req-ram"})
			if err != nil {
				t.Fatal(err)
			}
			caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: &tea.SDKError{Code: tea.String(tc.code), StatusCode: tea.Int(404), Message: tea.String("opaque"), Data: tea.String(string(body))}}}
			request := map[string]any{"RoleName": "owned-role"}
			if strings.Contains(tc.operation, "Policy") {
				request = map[string]any{"PolicyName": "owned-policy", "PolicyType": "Custom"}
			}
			_, err = caller.CallRaw(context.Background(), tc.operation, request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Kind != "not_found" || !ecerrors.ActionFromError(tc.operation, err).ProviderVerified || ecerrors.ActionFromError(tc.operation, err).Code != tc.code {
				t.Fatalf("exact typed RAM absence did not reach caller: %v", err)
			}
		})
	}
}

func TestRAMFixtureTypedPreallocationRejection(t *testing.T) {
	for _, operation := range []string{"CreatePolicy", "CreateRole", "AttachPolicyToRole"} {
		for _, tc := range []struct {
			status   int
			code     string
			body     string
			verified bool
		}{
			{403, "Forbidden", `{"Code":"Forbidden","RequestId":"req-fixture"}`, true},
			{403, "NoPermission", `{"Code":"NoPermission","RequestId":"req-fixture"}`, true},
			{400, "InvalidParameter", `{"Code":"InvalidParameter","RequestId":"req-fixture"}`, true},
			{500, "Forbidden", `{"Code":"Forbidden","RequestId":"req-fixture"}`, false},
			{403, "Forbidden", `{"Code":"Forbidden","RequestId":"req-fixture","request_id":"req-other"}`, false},
			{403, "Forbidden", `{"Code":"Forbidden","RequestId":"req-fixture","Role":{"RoleId":"allocated"}}`, false},
		} {
			t.Run(operation+"/"+tc.code+"/"+fmt.Sprint(tc.status)+"/"+tc.body, func(t *testing.T) {
				caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: &tea.SDKError{Code: tea.String(tc.code), StatusCode: tea.Int(tc.status), Message: tea.String("rejected"), Data: tea.String(tc.body)}}}
				request := map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"}
				_, err := caller.CallRaw(context.Background(), operation, request)
				action := ecerrors.ActionFromError(operation, err)
				if err == nil || action.ProviderVerified != tc.verified {
					t.Fatalf("%s rejection proof = %+v, err=%v", operation, action, err)
				}
			})
		}
	}
}

func TestRAMFixtureHTTPProofRejectsDuplicateFields(t *testing.T) {
	for _, tc := range []struct {
		operation string
		request   map[string]any
	}{
		{"CreateRole", map[string]any{"RoleName": "owned-role", "AssumeRolePolicyDocument": `{}`}},
		{"AttachPolicyToRole", map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			for _, disableSDKError := range []bool{false, true} {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.DisableSDKError = &disableSDKError
				executor.client.HttpClient = sdkErrorHTTPClient{status: 403, body: `{"Code":"Forbidden","RequestId":"req-ram","RequestId":"req-ram"}`}
				caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", Profile: profile, executor: executor}
				_, err = caller.CallRaw(context.Background(), tc.operation, tc.request)
				if err == nil || ecerrors.ActionFromError(tc.operation, err).ProviderVerified {
					t.Fatalf("duplicate provider fields proved %s rejection: %v", tc.operation, err)
				}
			}
		})
	}
}

func TestRAMFixtureAbsenceRejectsDiagnosticAndWrongStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"diagnostic text", errors.New("EntityNotExist.Role: role not found, Request ID: req-ram")},
		{"wrong status", &tea.SDKError{Code: tea.String("EntityNotExist.Role"), StatusCode: tea.Int(403), Message: tea.String("role not found"), Data: tea.String(`{"Code":"EntityNotExist.Role","RequestId":"req-ram"}`)}},
		{"missing request ID", &tea.SDKError{Code: tea.String("EntityNotExist.Role"), StatusCode: tea.Int(404), Message: tea.String("role not found"), Data: tea.String(`{"Code":"EntityNotExist.Role"}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: tc.err}}
			_, err := caller.CallRaw(context.Background(), "GetRole", map[string]any{"RoleName": "owned-role"})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Kind != "not_found" || ecerrors.ActionFromError("GetRole", err).ProviderVerified {
				t.Fatalf("unproven RAM absence changed public classification or proved fixture absence: %v", err)
			}
		})
	}
}

func TestRAMFixtureAbsenceRejectsContradictoryEnvelope(t *testing.T) {
	for _, tc := range []struct {
		operation, code, field, request string
	}{
		{"GetRole", "EntityNotExist.Role", "RoleName", "owned-role"},
		{"GetPolicy", "EntityNotExist.Policy", "PolicyName", "owned-policy"},
	} {
		for _, body := range []string{
			`{"Code":"` + tc.code + `","RequestId":"req-ram","success":true}`,
			`{"Code":"` + tc.code + `","RequestId":"req-ram","Role":{"RoleId":"role-present"}}`,
			`{"Code":"` + tc.code + `","RequestId":"req-ram","Policy":{"PolicyName":"policy-present"}}`,
			`{"Code":"` + tc.code + `","RequestId":"req-ram","request_id":"req-other"}`,
		} {
			t.Run(tc.operation+"/"+body, func(t *testing.T) {
				caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: &tea.SDKError{Code: tea.String(tc.code), StatusCode: tea.Int(404), Message: tea.String("opaque"), Data: tea.String(body)}}}
				_, err := caller.CallRaw(context.Background(), tc.operation, map[string]any{tc.field: tc.request})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Kind != "service" || ecerrors.ActionFromError(tc.operation, err).ProviderVerified {
					t.Fatalf("contradictory RAM absence proved terminal deletion: %v", err)
				}
			})
		}
	}
}

func (c sdkErrorHTTPClient) Call(_ *http.Request, _ *http.Transport) (*http.Response, error) {
	return &http.Response{StatusCode: c.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(c.body))}, nil
}

type sdkErrorCapturingExecutor struct {
	delegate openAPIExecutor
	err      error
}

func (e *sdkErrorCapturingExecutor) ExecuteOpenAPI(ctx context.Context, req *openAPIRequest) (map[string]any, error) {
	response, err := e.delegate.ExecuteOpenAPI(ctx, req)
	e.err = err
	return response, err
}

func TestOpenAPICallerSDKResponseErrors(t *testing.T) {
	for _, disableSDKError := range []bool{true, false} {
		for _, tc := range []struct {
			name, api, code, message, kind string
			status                         int
		}{
			{"observed create rejection", "CreateTeam", "400", "teamName is invalid", "service", 400},
			{"numeric team absence", "GetTeam", "404", "team missing", "not_found", 404},
			{"named team absence", "GetTeam", "TeamNotFound", "team missing", "not_found", 404},
			{"lowercase numeric team absence", "getteam", "404", "team missing", "not_found", 404},
			{"lowercase named team absence", "getteam", "TeamNotFound", "team missing", "not_found", 404},
			{"mixed-case numeric team absence", "gEtTeAm", "404", "team missing", "not_found", 404},
			{"mixed-case named team absence", "gEtTeAm", "TeamNotFound", "team missing", "not_found", 404},
			{"whitespace numeric team absence", " \tGetTeam\n ", "404", "team missing", "not_found", 404},
			{"whitespace named team absence", " \tGetTeam\n ", "TeamNotFound", "team missing", "not_found", 404},
			{"auth mentioning absence", "GetTeam", "Forbidden", "team not found: permission denied", "service", 403},
			{"auth with HTTP 404", "GetTeam", "Forbidden", "team not found: permission denied", "service", 404},
			{"lowercase auth with HTTP 404", "getteam", "Forbidden", "team not found: permission denied", "service", 404},
			{"unrelated absence", "GetTeam", "ResourceGroupNotFound", "resource group not found", "service", 404},
			{"whitespace unrelated absence", " GetTeam ", "ResourceGroupNotFound", "resource group not found", "service", 404},
			{"delete is not absence confirmation", "DeleteTeam", "404", "team not found", "service", 404},
			{"lowercase delete is not absence confirmation", "deleteteam", "404", "team not found", "service", 404},
			{"list unrelated absence", "ListTeams", "TeamNotFound", "team not found", "service", 404},
			{"mixed-case list unrelated absence", " lIsTtEaMs ", "TeamNotFound", "team not found", "service", 404},
		} {
			t.Run(fmt.Sprintf("%s/disableSDKError=%t", tc.name, disableSDKError), func(t *testing.T) {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.DisableSDKError = tea.Bool(disableSDKError)
				body, err := json.Marshal(map[string]any{"code": tc.code, "message": tc.message, "requestId": "req-sdk"})
				if err != nil {
					t.Fatal(err)
				}
				executor.client.HttpClient = sdkErrorHTTPClient{tc.status, string(body)}
				capture := &sdkErrorCapturingExecutor{delegate: executor}
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: capture}
				_, err = caller.CallRaw(context.Background(), tc.api, map[string]any{"teamID": "team-1", "teamName": "test-team"})
				if disableSDKError {
					var sdkErr *openapiClient.ClientError
					if !errors.As(capture.err, &sdkErr) {
						t.Fatalf("actual SDK shape = %T, want ClientError: %v", capture.err, capture.err)
					}
				} else {
					var sdkErr *tea.SDKError
					if !errors.As(capture.err, &sdkErr) {
						t.Fatalf("converted SDK shape = %T, want tea.SDKError: %v", capture.err, capture.err)
					}
				}
				var appErr *ecerrors.AppError
				if !errors.As(err, &appErr) || appErr.Payload().Kind != tc.kind {
					t.Fatalf("error = %v, want %s", err, tc.kind)
				}
				action := ecerrors.ActionFromError(tc.api, err)
				if action.Code != tc.code || action.Message != tc.message || action.RequestID != "req-sdk" {
					t.Fatalf("SDK provider fields lost: %+v", action)
				}
			})
		}
	}
}

func TestFCSandboxTeamMutationContradictoryReplyCannotProveRejection(t *testing.T) {
	for _, tc := range []struct {
		operation string
		status    int
		code      string
	}{
		{"CreateTeam", http.StatusNotFound, "InvalidParameter"},
		{"DeleteTeam", http.StatusBadRequest, "TeamNotFound"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{tc.status, `{"code":"` + tc.code + `","message":"rejected","requestId":"req-team"}`}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "team", Region: "cn-hangzhou", executor: executor}
			_, err = caller.CallRaw(context.Background(), tc.operation, map[string]any{"teamID": "team-1", "teamName": "owned"})
			if err == nil {
				t.Fatal("contradictory Team mutation reply was accepted")
			}
			if action := ecerrors.ActionFromError(tc.operation, err); action.ProviderVerified {
				t.Fatalf("contradictory Team mutation was treated as definitive: %+v", action)
			}
		})
	}
}

func TestTemplateCacheSDKResponseFieldsRedactCredentialAssignments(t *testing.T) {
	for _, disableSDKError := range []bool{true, false} {
		for _, tc := range []struct {
			name, code, message, requestID       string
			wantCode, wantMessage, wantRequestID string
			status                               int
			wantErrorCode                        string
		}{
			{"authorization message", "Forbidden", "Authorization=Bearer reviewer-canary-blue reviewer-canary-river", "req-ordinary", "Forbidden", "[REDACTED]", "req-ordinary", http.StatusForbidden, "CloudAPIError"},
			{"multiword password message", "Forbidden", "Password=reviewer-canary-blue reviewer-canary-river", "req-ordinary", "Forbidden", "[REDACTED]", "req-ordinary", http.StatusForbidden, "CloudAPIError"},
			{"multiword token code", "Token=reviewer-canary-blue reviewer-canary-river", "provider unavailable", "req-ordinary", "[REDACTED]", "provider unavailable", "req-ordinary", http.StatusForbidden, "CloudAPIError"},
			{"auth token code", "AuthToken=reviewer-canary", "provider unavailable", "req-ordinary", "[REDACTED]", "provider unavailable", "req-ordinary", http.StatusForbidden, "CloudAPIError"},
			{"session token request ID", "Forbidden", "provider unavailable", "SessionToken=reviewer-canary", "Forbidden", "provider unavailable", "", http.StatusForbidden, "CloudAPIError"},
			{"bare access key request ID", "Forbidden", "provider unavailable", "LTAIabcdefghijklmnop", "Forbidden", "provider unavailable", "", http.StatusForbidden, "CloudAPIError"},
			{"compact token request ID", "Forbidden", "provider unavailable", "AbC_def-123", "Forbidden", "provider unavailable", "", http.StatusForbidden, "CloudAPIError"},
			{"ordinary diagnostics", "Forbidden", "provider unavailable", "req-ordinary", "Forbidden", "provider unavailable", "req-ordinary", http.StatusForbidden, "CloudAPIError"},
			{"cache absent", "TemplateCacheNotFound", "cache absent", "req-ordinary", "TemplateCacheNotFound", "cache absent", "req-ordinary", http.StatusNotFound, "NotFound"},
		} {
			t.Run(fmt.Sprintf("%s/disableSDKError=%t", tc.name, disableSDKError), func(t *testing.T) {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal("could not set up local SDK executor")
				}
				executor.client.DisableSDKError = tea.Bool(disableSDKError)
				body, err := json.Marshal(map[string]any{"code": tc.code, "message": tc.message, "requestId": tc.requestID})
				if err != nil {
					t.Fatal("could not encode local SDK response")
				}
				executor.client.HttpClient = sdkErrorHTTPClient{status: tc.status, body: string(body)}
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: profile, executor: executor}
				_, err = caller.CallRaw(context.Background(), "DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != tc.wantErrorCode {
					t.Fatal("non-2xx Template Cache response changed error classification")
				}
				action := ecerrors.ActionFromError("DescribeTemplateCache", err)
				public, marshalErr := json.Marshal(map[string]any{"error": app.Payload(), "action": action, "message": err.Error()})
				if marshalErr != nil || action.Code != tc.wantCode || action.Message != tc.wantMessage || action.RequestID != tc.wantRequestID || strings.Contains(string(public), "reviewer-canary") {
					t.Fatal("structured SDK diagnostic was not safely retained")
				}
			})
		}
	}
}

func TestOpenAPICallerWrappedSDKErrors(t *testing.T) {
	body := map[string]any{"Code": "InvalidParameter", "Message": "original provider message", "RequestId": "req-body", "securityToken": "unrelated-private-token"}
	daraErr := dara.NewSDKError(map[string]any{"code": "InvalidParameter", "message": "code: 400, formatted message request id: req-body", "data": body})
	for _, source := range []error{daraErr, dara.TeaSDKError(daraErr)} {
		t.Run(fmt.Sprintf("%T", source), func(t *testing.T) {
			wrapped := fmt.Errorf("API invocation: %w", source)
			caller := &OpenAPICaller{Product: "Vpc", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: wrapped}}
			_, err := caller.Call(context.Background(), "DescribeVpcs", map[string]any{})
			action := ecerrors.ActionFromError("DescribeVpcs", err)
			if action.Code != "InvalidParameter" || action.Message != "original provider message" || action.RequestID != "req-body" {
				t.Fatalf("wrapped SDK provider fields lost: %+v", action)
			}
			if strings.Contains(err.Error(), "unrelated-private-token") || strings.Contains(fmt.Sprint(action), "unrelated-private-token") {
				t.Fatal("unselected SDK Data leaked")
			}
		})
	}
}

func TestSDKCloudErrorFieldsFallbackAndSanitization(t *testing.T) {
	for _, tc := range []struct {
		name, data, message, wantMessage, wantRequestID string
	}{
		{"missing data", "", "SDK fallback message", "SDK fallback message", ""},
		{"malformed data", "{broken", "SDK fallback message", "SDK fallback message", ""},
		{"quoted body", `"{\"code\":\"Forbidden\",\"message\":\"body message\",\"requestId\":\"req-quoted\"}"`, "formatted SDK message", "body message", "req-quoted"},
		{"credential body message", `{"message":"securityToken=private-token","requestId":"req-secret"}`, "formatted SDK message", "Alibaba Cloud API request failed", "req-secret"},
		{"signed URL body message", `{"message":"request https://example.com/?AccessKeyId=private-ak&Signature=private-signature failed","requestId":"req-url"}`, "formatted SDK message", "request https://example.com/?[REDACTED] failed", "req-url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &tea.SDKError{Code: tea.String("Forbidden"), Message: tea.String(tc.message), Data: tea.String(tc.data), StatusCode: tea.Int(404)}
			err := ecerrors.Service("CloudAPIError", callerCloudErrorMessage(source), false, cloudErrorOptions(source)...)
			action := ecerrors.ActionFromError("GetTeam", err)
			if action.Code != "Forbidden" || action.Message != tc.wantMessage || action.RequestID != tc.wantRequestID {
				t.Fatalf("SDK fallback/sanitization = %+v", action)
			}
			if strings.Contains(fmt.Sprint(action), "private-") || strings.Contains(err.Error(), "private-") {
				t.Fatal("SDK fields leaked credentials")
			}
		})
	}
}
