package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	openapiClient "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

type interruptedProofHTTPClient struct{}

func (interruptedProofHTTPClient) Call(*http.Request, *http.Transport) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(io.MultiReader(strings.NewReader(`{"partial":`), failedProofBodyReader{err: io.ErrUnexpectedEOF}))}, nil
}

func TestInterruptedStrictProofBodyPreservesReadError(t *testing.T) {
	client := &credentialFinalHTTPClient{delegate: interruptedProofHTTPClient{}, strictProofJSON: true}
	request, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Call(request, http.DefaultTransport.(*http.Transport))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, err = io.ReadAll(response.Body)
	if !client.proofBodyInvalid || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("interrupted proof body became an ordinary EOF: invalid=%t err=%v", client.proofBodyInvalid, err)
	}
}

func TestProtectedMutationsDoNotRetryAfterAmbiguousThrottle(t *testing.T) {
	for _, tc := range []struct {
		product, resource, operation string
		request                      map[string]any
	}{
		{"FCSandbox", "volume", "DeleteVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"}},
		{"ram", "", "CreateRole", map[string]any{"RoleName": "owned-role", "AssumeRolePolicyDocument": `{}`}},
		{"ram", "", "DeleteRole", map[string]any{"RoleName": "owned-role"}},
		{"ram", "", "CreatePolicy", map[string]any{"PolicyName": "owned-policy", "PolicyDocument": `{}`}},
		{"ram", "", "DeletePolicy", map[string]any{"PolicyName": "owned-policy"}},
		{"ram", "", "AttachPolicyToRole", map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"}},
		{"ram", "", "DetachPolicyFromRole", map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{callErrors: []error{errors.New("SDK.ServerError\nErrorCode: Throttling.User\nMessage: flow control")}, response: `{"code":"200","requestId":"req-later"}`}
			caller := &OpenAPICaller{Product: tc.product, Resource: tc.resource, Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 4, sleepFn: func(context.Context, time.Duration) error { return nil }}
			_, err := caller.CallRaw(context.Background(), tc.operation, tc.request)
			if err == nil || len(fake.requests) != 1 || ecerrors.ActionFromError(tc.operation, err).ProviderVerified {
				t.Fatalf("ambiguous mutation was retried or proved: attempts=%d err=%v", len(fake.requests), err)
			}
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Retryable {
				t.Fatalf("ambiguous mutation was offered for retry: %v", err)
			}
		})
	}
}

func TestProtectedMutationGenericTyped404DoesNotProveProviderRejection(t *testing.T) {
	for _, tc := range []struct {
		product, resource, operation string
		request                      map[string]any
		code                         string
	}{
		{"FCSandbox", "volume", "CreateVolume", map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}, "Unexpected"},
		{"FCSandbox", "volume", "DeleteVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"}, "Unexpected"},
		{"ram", "", "CreateRole", map[string]any{"RoleName": "owned-role", "AssumeRolePolicyDocument": `{}`}, "Unexpected"},
		{"ram", "", "DeleteRole", map[string]any{"RoleName": "owned-role"}, "Unexpected"},
		{"ram", "", "CreatePolicy", map[string]any{"PolicyName": "owned-policy", "PolicyDocument": `{}`}, "Unexpected"},
		{"ram", "", "DeletePolicy", map[string]any{"PolicyName": "owned-policy"}, "Unexpected"},
		{"ram", "", "AttachPolicyToRole", map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"}, "Unexpected"},
		{"ram", "", "CreateRole", map[string]any{"RoleName": "owned-role", "AssumeRolePolicyDocument": `{}`}, "NotFound"},
		{"ram", "", "CreatePolicy", map[string]any{"PolicyName": "owned-policy", "PolicyDocument": `{}`}, "DependencyViolation"},
	} {
		t.Run(tc.operation+"/"+tc.code, func(t *testing.T) {
			providerErr := &tea.SDKError{Code: tea.String(tc.code), StatusCode: tea.Int(404), Message: tea.String("opaque"), Data: tea.String(`{"Code":"` + tc.code + `","RequestId":"req-generic"}`)}
			caller := &OpenAPICaller{Product: tc.product, Resource: tc.resource, Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: providerErr}, retryMaxAttempts: 1}
			_, err := caller.CallRaw(context.Background(), tc.operation, tc.request)
			if err == nil || ecerrors.ActionFromError(tc.operation, err).ProviderVerified {
				t.Fatalf("generic typed 404 proved %s rejection: %v", tc.operation, err)
			}
		})
	}
}

func TestProtectedDeleteVolumeExactTypedRejectionRetainsProof(t *testing.T) {
	providerErr := &tea.SDKError{Code: tea.String("Forbidden"), StatusCode: tea.Int(403), Message: tea.String("denied"), Data: tea.String(`{"Code":"Forbidden","RequestId":"req-delete"}`)}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: providerErr}}
	_, err := caller.CallRaw(context.Background(), "DeleteVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"})
	if err == nil || !ecerrors.ActionFromError("DeleteVolume", err).ProviderVerified {
		t.Fatalf("exact typed DeleteVolume rejection lost provider proof: %v", err)
	}
}

func TestRAMMutationRetriesOnlyVerifiedProviderThrottle(t *testing.T) {
	request := map[string]any{"RoleName": "owned-role", "AssumeRolePolicyDocument": `{}`}
	for _, tc := range []struct {
		name         string
		err          error
		wantAttempts int
	}{
		{"verified 429", &tea.SDKError{Code: tea.String("Throttling.User"), StatusCode: tea.Int(429), Message: tea.String("flow control"), Data: tea.String(`{"Code":"Throttling.User","RequestId":"req-throttle"}`)}, 2},
		{"unverified body", &tea.SDKError{Code: tea.String("Throttling.User"), StatusCode: tea.Int(429), Message: tea.String("flow control"), Data: tea.String(`{"Code":"Throttling.User"}`)}, 1},
		{"unverified status", &tea.SDKError{Code: tea.String("Throttling.User"), StatusCode: tea.Int(403), Message: tea.String("flow control"), Data: tea.String(`{"Code":"Throttling.User","RequestId":"req-throttle"}`)}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{callErrors: []error{tc.err}, response: `{"RequestId":"req-ok"}`}
			caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 3, sleepFn: func(context.Context, time.Duration) error { return nil }}
			_, err := caller.CallRaw(context.Background(), "CreateRole", request)
			if got := len(fake.requests); got != tc.wantAttempts {
				t.Fatalf("attempts=%d, want %d; err=%v", got, tc.wantAttempts, err)
			}
			if tc.wantAttempts == 2 && err != nil {
				t.Fatalf("verified throttle retry failed: %v", err)
			}
		})
	}
}

func TestRAMDetachRetriesOnlyVerifiedProviderThrottle(t *testing.T) {
	providerErr := &tea.SDKError{Code: tea.String("Throttling.User"), StatusCode: tea.Int(429), Message: tea.String("flow control"), Data: tea.String(`{"Code":"Throttling.User","RequestId":"req-detach"}`)}
	fake := &fakeOpenAPIExecutor{callErrors: []error{providerErr}, response: `{"RequestId":"req-later"}`}
	caller := &OpenAPICaller{Product: "ram", Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 3, sleepFn: func(context.Context, time.Duration) error { return nil }}
	_, err := caller.CallRaw(context.Background(), "DetachPolicyFromRole", map[string]any{"RoleName": "owned-role", "PolicyName": "owned-policy", "PolicyType": "Custom"})
	if err != nil || len(fake.requests) != 2 {
		t.Fatalf("DetachPolicyFromRole attempts=%d, err=%v", len(fake.requests), err)
	}
}

func TestProviderProofTupleDoesNotCoverUnrelatedAPIs(t *testing.T) {
	for _, tc := range []struct {
		product, version, operation string
		want                        bool
	}{
		{"FCSandbox", "2026-05-09", "GetVolume", true},
		{"FCSandbox", "2026-05-09", "ListVolumes", true},
		{"ram", "2015-05-01", "CreateRole", true},
		{"ram", "2015-05-01", "DetachPolicyFromRole", true},
		{"ram", "2015-05-01", "ListPoliciesForRole", true},
		{"ResourceManager", "2020-03-31", "GetPolicyVersion", false},
		{"ResourceManager", "2015-05-01", "GetPolicyVersion", false},
		{"ECS", "2014-05-26", "GetVolume", false},
		{"FCSandbox", "2025-01-01", "GetVolume", false},
		{"ResourceManager", "2015-05-01", "GetRole", false},
	} {
		request := &openAPIRequest{Product: tc.product, Version: tc.version, ApiName: tc.operation}
		if got := strictProviderProofOperation(request); got != tc.want {
			t.Errorf("proof tuple %+v = %v, want %v", request, got, tc.want)
		}
	}
}

func TestVolumeHTTPRedirectCannotProveBusinessResponse(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	executor.client.HttpClient = sdkErrorHTTPClient{status: 302, body: `{"code":"VolumeNotFound","message":"gone","requestId":"req-redirect"}`}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
	_, err = caller.CallRaw(context.Background(), "GetVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"})
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status") || ecerrors.ActionFromError("GetVolume", err).ProviderVerified {
		t.Fatalf("redirect body proved Volume absence: %v", err)
	}
}

func TestListVolumesRejectsNonSuccessTransportStatus(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	executor.client.HttpClient = sdkErrorHTTPClient{status: 302, body: `{"code":"200","volumes":[],"requestId":"req-list"}`}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
	_, err = caller.CallRaw(context.Background(), "ListVolumes", map[string]any{"maxResults": 1})
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status") {
		t.Fatalf("ListVolumes accepted a non-success transport status: %v", err)
	}
}

func TestUnrelatedRawAPIKeepsSDKXMLError(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	executor.client.HttpClient = xmlProofErrorHTTPClient{}
	req := &openAPIRequest{Product: "ResourceManager", Version: "2022-01-01", ApiName: "GetRole", RegionId: "cn-hangzhou", Domain: "resourcemanager.aliyuncs.com", Scheme: "https", Method: "GET", Style: "ROA"}
	_, err = executor.ExecuteOpenAPI(context.Background(), req)
	var sdk *openapiClient.ClientError
	if !errors.As(err, &sdk) || tea.StringValue(sdk.GetCode()) != "Forbidden" || tea.StringValue(sdk.GetRequestId()) != "req-xml" {
		t.Fatalf("unrelated SDK XML error lost its provider code: %v", err)
	}
}

func TestProtectedRawAPIKeepsSDKXMLErrorWithoutCleanupProof(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	executor.client.HttpClient = xmlProofErrorHTTPClient{}
	req := &openAPIRequest{Product: "ram", Version: "2015-05-01", ApiName: "GetRole", RegionId: "cn-hangzhou", Domain: "ram.aliyuncs.com", Scheme: "https", Method: "GET", Style: "RPC"}
	_, err = executor.ExecuteOpenAPI(context.Background(), req)
	var sdk *openapiClient.ClientError
	if !errors.As(err, &sdk) || tea.StringValue(sdk.GetCode()) != "Forbidden" || tea.StringValue(sdk.GetRequestId()) != "req-xml" {
		t.Fatalf("protected SDK XML error lost its provider diagnostics: %v", err)
	}
	if volumeStructuredErrorProof(err, 403, "Forbidden", "Forbidden", "req-xml") {
		t.Fatalf("XML error became cleanup proof: %v", err)
	}
}

type xmlProofErrorHTTPClient struct{}

func (xmlProofErrorHTTPClient) Call(*http.Request, *http.Transport) (*http.Response, error) {
	return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"text/xml;charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(`<Error><Code>Forbidden</Code><Message>denied</Message><RequestId>req-xml</RequestId></Error>`))}, nil
}

var _ dara.HttpClient = xmlProofErrorHTTPClient{}
