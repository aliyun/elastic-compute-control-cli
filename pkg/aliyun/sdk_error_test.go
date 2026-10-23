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
