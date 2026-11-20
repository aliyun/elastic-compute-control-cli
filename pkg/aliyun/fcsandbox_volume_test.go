package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestFCSandboxVolumeROARequests(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	oss := map[string]any{"bucketName": "dedicated-test-bucket", "bucketPath": "/run-1", "readOnly": true}
	fs := map[string]any{"serverAddr": "test.example:/", "userID": 1000, "groupID": 1000}
	bucket := map[string]any{"agenticBucket": "owned-bucket", "bucketName": "space", "readOnly": false}
	mount := map[string]any{"role": "acs:ram::0000000000000000:role/test", "vpcConfig": map[string]any{"vpcId": "vpc-test", "vSwitchIds": []any{"vsw-test"}, "securityGroupId": "sg-test"}}
	for _, tc := range []struct {
		api, method, path string
		values            map[string]any
	}{
		{"CreateVolume", "POST", "/pop/2026-05-09/volumes", map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1", "body.ossVolumeConfig": oss, "body.agenticFSVolumeConfig": fs, "body.agenticBucketVolumeConfig": bucket, "body.mountConfig": mount}},
		{"GetVolume", "GET", "/pop/2026-05-09/volumes/vol-1", map[string]any{"volumeID": "vol-1", "teamID": "team-1"}},
		{"ListVolumes", "GET", "/pop/2026-05-09/volumes", map[string]any{"volumeName": "workspace", "status": "AVAILABLE", "teamID": "team-1", "resourceGroupID": "rg-test", "userID": "user-1", "maxResults": 1, "nextToken": "opaque-2"}},
		{"DeleteVolume", "DELETE", "/pop/2026-05-09/volumes/vol-1", map[string]any{"volumeID": "vol-1", "teamID": "team-1"}},
	} {
		t.Run(tc.api, func(t *testing.T) {
			req, err := caller.commonRequest(tc.api, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if req.Version != "2026-05-09" || req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-hangzhou.aliyuncs.com" || req.Scheme != "https" || req.Style != "restful" {
				t.Fatalf("wrong Volume route: %+v", req)
			}
			switch tc.api {
			case "CreateVolume":
				body, ok := req.BodyValue().(map[string]any)
				if !ok || body["volumeName"] != "workspace" || body["teamID"] != "team-1" || len(req.QueryParams) != 0 {
					t.Fatalf("create body: %+v", req)
				}
				for key, want := range map[string]any{"ossVolumeConfig": oss, "agenticFSVolumeConfig": fs, "agenticBucketVolumeConfig": bucket, "mountConfig": mount} {
					if !reflect.DeepEqual(body[key], jsonRoundTripValue(t, want)) {
						t.Errorf("%s = %#v, want %#v", key, body[key], want)
					}
				}
			case "ListVolumes":
				for key, value := range tc.values {
					if req.QueryParams[key] != valueToString(value) {
						t.Errorf("query %s = %q", key, req.QueryParams[key])
					}
				}
				if req.BodyValue() != nil {
					t.Fatal("list sent a body")
				}
			default:
				if req.QueryParams["teamID"] != "team-1" || req.BodyValue() != nil {
					t.Fatalf("get/delete team scope: %+v", req)
				}
			}
		})
	}
}

func TestVolumeCredentialLeaseRequiresEffectiveSignedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot credentialSnapshot
		mode     string
		wantOK   bool
	}{
		{name: "AK", snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", Type: "access_key"}, mode: "AK", wantOK: true},
		{name: "STS", snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token", Type: "sts"}, mode: "StsToken", wantOK: true},
		{name: "bearer", snapshot: credentialSnapshot{BearerToken: "token", Type: "bearer"}, mode: "BearerToken"},
		{name: "mixed", snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", BearerToken: "token", Type: "bearer"}, mode: "AK"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{
				Mode: tc.mode, AuthType: "AK", RegionID: "cn-hangzhou", Acquirer: &staticCredentialAcquirer{snapshot: tc.snapshot},
			}}
			lease, err := caller.acquireCredentialLease(context.Background())
			if tc.wantOK && (err != nil || lease == nil) || !tc.wantOK && err == nil {
				t.Fatalf("Volume lease = %+v, %v; want accepted=%t", lease, err, tc.wantOK)
			}
		})
	}
}

func TestVolumeCreatePreservesSignedInt32BoundaryInRequestBody(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	const id = "2147483647"
	req, err := caller.commonRequest("CreateVolume", map[string]any{
		"body.volumeName":            "workspace",
		"body.agenticFSVolumeConfig": `{"userID":` + id + `,"groupID":1000}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req.GetContent()), `"userID":`+id) {
		t.Fatalf("provider request changed supported integer: %s", req.GetContent())
	}
	body, ok := req.BodyValue().(map[string]any)
	config, valid := body["agenticFSVolumeConfig"].(map[string]any)
	if !ok || !valid || config["userID"] != json.Number(id) {
		t.Fatalf("request inspection changed supported integer: %#v", body)
	}
}

func TestNativeVolumeResponsePreservesLargeProviderIntegerWithoutRounding(t *testing.T) {
	const id = "9007199254740993"
	for _, api := range []string{"CreateVolume", "GetVolume", "ListVolumes"} {
		body := `{"code":"200","volume":{"volumeID":"vol-1","status":"AVAILABLE","agenticFSVolumeConfig":{"userID":` + id + `}},"volumes":[{"volumeID":"vol-1","status":"AVAILABLE","agenticFSVolumeConfig":{"userID":` + id + `}}],"maxResults":1}`
		response, err := normalizeDarabonbaResponseForRequest(map[string]any{"body": body}, true)
		if err != nil {
			t.Fatal(err)
		}
		var volume map[string]any
		if api == "ListVolumes" {
			volume = response["volumes"].([]any)[0].(map[string]any)
		} else {
			volume = response["volume"].(map[string]any)
		}
		config := volume["agenticFSVolumeConfig"].(map[string]any)
		if config["userID"] != json.Number(id) {
			t.Fatalf("%s userID changed: %#v", api, config["userID"])
		}
		if api == "ListVolumes" {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou"}
			req, err := caller.commonRequest(api, map[string]any{"maxResults": 1})
			if err != nil || caller.fcVolumeResponseError(req, response) != nil {
				t.Fatalf("ListVolumes response with lossless page size was rejected: %v", err)
			}
		}
	}
}

func TestVolumeListRejectsPageLargerThanOutboundLimit(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou"}
	req, err := caller.commonRequest("ListVolumes", map[string]any{"maxResults": 1})
	if err != nil {
		t.Fatal(err)
	}
	items := []any{
		map[string]any{"volumeID": "vol-1", "status": "AVAILABLE"},
		map[string]any{"volumeID": "vol-2", "status": "AVAILABLE"},
	}
	for _, echo := range []any{nil, 2, 1} {
		response := map[string]any{"code": "200", "volumes": items, "nextToken": "opaque"}
		if echo != nil {
			response["maxResults"] = echo
		}
		if err := caller.fcVolumeResponseError(req, response); err == nil {
			t.Fatalf("oversized page accepted with maxResults echo %#v", echo)
		}
	}
	if err := caller.fcVolumeResponseError(req, map[string]any{"code": "200", "volumes": items[:1], "maxResults": 2}); err == nil {
		t.Fatal("server limit echo changed the outbound page contract")
	}
}

func TestVolumeCreateAcceptanceRequiresCoherentRequestAndResponseTuple(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestBody    string
		changeRequest  func(*openAPIRequest)
		changeResponse func(map[string]any, map[string]any)
		accepted       bool
	}{
		{name: "accepted", accepted: true},
		{name: "malformed request body", requestBody: `{"volumeName":"workspace"`},
		{name: "request missing name", requestBody: `{"teamID":"team-1"}`},
		{name: "request team is not text", requestBody: `{"volumeName":"workspace","teamID":42}`},
		{name: "request team has path separator", requestBody: `{"volumeName":"workspace","teamID":"team/other"}`},
		{name: "request missing region", changeRequest: func(req *openAPIRequest) { req.RegionId = "" }},
		{name: "response missing ID", changeResponse: func(_ map[string]any, volume map[string]any) { delete(volume, "volumeID") }},
		{name: "response blank status", changeResponse: func(_ map[string]any, volume map[string]any) { volume["status"] = " " }},
		{name: "response wrong name", changeResponse: func(_ map[string]any, volume map[string]any) { volume["volumeName"] = "another" }},
		{name: "response missing team", changeResponse: func(_ map[string]any, volume map[string]any) { delete(volume, "teamID") }},
		{name: "response wrong team", changeResponse: func(_ map[string]any, volume map[string]any) { volume["teamID"] = "team-2" }},
		{name: "response invalid request ID", changeResponse: func(response, _ map[string]any) { response["requestId"] = "req\nother" }},
		{name: "response conflicting code alias", changeResponse: func(response, _ map[string]any) { response["Code"] = "409" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
			req, err := caller.commonRequest("CreateVolume", map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.requestBody != "" {
				req.SetContent([]byte(tc.requestBody))
			}
			if tc.changeRequest != nil {
				tc.changeRequest(req)
			}
			volume := map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING"}
			response := map[string]any{"code": "200", "requestId": "req-create", "volume": volume}
			if tc.changeResponse != nil {
				tc.changeResponse(response, volume)
			}
			accepted, ok := volumeCreateAcceptance(req, response)
			if ok != tc.accepted || ok && (accepted.ID != "vol-1" || accepted.TeamID != "team-1" || accepted.RequestID != "req-create" || !accepted.AllocationConfirmed) {
				t.Fatalf("allocation tuple accepted=%t: %+v", ok, accepted)
			}
		})
	}
}

func TestFCSandboxVolumePositiveResponseRequiresScopedPageAndMutation(t *testing.T) {
	for _, tc := range []struct {
		name, api, response string
	}{
		{"create without team", "CreateVolume", `{"code":"200","requestId":"req-1","volume":{"volumeID":"vol-1","volumeName":"workspace","status":"CREATING"}}`},
		{"get invalid status", "GetVolume", `{"code":"200","requestId":"req-1","volume":{"volumeID":"vol-1","teamID":"team-1","status":" "}}`},
		{"get malformed optional field", "GetVolume", `{"code":"200","requestId":"req-1","volume":{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE","userID":42}}`},
		{"list malformed item", "ListVolumes", `{"code":"200","requestId":"req-1","volumes":["vol-1"],"maxResults":1}`},
		{"list wrong team", "ListVolumes", `{"code":"200","requestId":"req-1","volumes":[{"volumeID":"vol-1","teamID":"team-2","status":"AVAILABLE"}],"maxResults":1}`},
		{"list fractional page echo", "ListVolumes", `{"code":"200","requestId":"req-1","volumes":[],"maxResults":1.5}`},
		{"list oversized page echo", "ListVolumes", `{"code":"200","requestId":"req-1","volumes":[],"maxResults":2147483648}`},
		{"list nontext continuation", "ListVolumes", `{"code":"200","requestId":"req-1","volumes":[],"nextToken":42,"maxResults":1}`},
		{"delete plural resource", "DeleteVolume", `{"code":"200","requestId":"req-1","volumes":[]}`},
		{"invalid request ID type", "DeleteVolume", `{"code":"200","requestId":42}`},
		{"invalid code type", "DeleteVolume", `{"code":200,"requestId":"req-1"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
			switch tc.api {
			case "CreateVolume":
				request = map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
			case "ListVolumes":
				request = map[string]any{"maxResults": 1, "teamID": "team-1"}
			}
			_, err := caller.CallRaw(context.Background(), tc.api, request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" || ecerrors.ActionFromError(tc.api, err).ProviderVerified {
				t.Fatalf("unscoped Volume result was accepted: %v", err)
			}
		})
	}
}

func TestVolumeTypedAbsenceRequiresOneCompleteSDKErrorEnvelope(t *testing.T) {
	const body = `{"code":"VolumeNotFound","requestId":"req-typed"}`
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"exact tea envelope", &tea.SDKError{StatusCode: tea.Int(404), Data: tea.String(body)}, true},
		{"JSON encoded envelope string", &tea.SDKError{StatusCode: tea.Int(404), Data: tea.String(`"{\"code\":\"VolumeNotFound\",\"requestId\":\"req-typed\"}"`)}, true},
		{"missing status", &tea.SDKError{Data: tea.String(body)}, false},
		{"wrong status", &tea.SDKError{StatusCode: tea.Int(403), Data: tea.String(body)}, false},
		{"duplicate member", &tea.SDKError{StatusCode: tea.Int(404), Data: tea.String(`{"code":"VolumeNotFound","code":"200","requestId":"req-typed"}`)}, false},
		{"malformed nested string", &tea.SDKError{StatusCode: tea.Int(404), Data: tea.String(`"{partial"`)}, false},
		{"scalar payload", &tea.SDKError{StatusCode: tea.Int(404), Data: tea.String(`42`)}, false},
		{"dara envelope", &dara.SDKError{StatusCode: tea.Int(404), Data: tea.String(body)}, true},
		{"dara missing status", &dara.SDKError{Data: tea.String(body)}, false},
		{"untyped error", errors.New(body), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := volumeStructuredTransportAbsence(tc.err, "VolumeNotFound", "req-typed"); got != tc.want {
				t.Fatalf("typed absence proof = %t, want %t", got, tc.want)
			}
		})
	}
	for _, status := range []json.Number{"404", "404.5", "not-a-number"} {
		body := map[string]any{"code": "VolumeNotFound", "requestId": "req-typed", "httpStatusCode": status, "success": false}
		if got := volume404ErrorEnvelopeProof(body, "VolumeNotFound", "req-typed"); got != (status == "404") {
			t.Fatalf("enriched numeric status %q proved absence=%t", status, got)
		}
	}
}

func TestVolumeRequestAndProofGuardsRejectAmbiguousCreateInput(t *testing.T) {
	for _, tc := range []struct {
		name, body, field string
	}{
		{"nonobject body", `[]`, "body"},
		{"nontext team", `{"volumeName":"workspace","teamID":42}`, "team_id"},
		{"path in team", `{"volumeName":"workspace","teamID":"team/other"}`, "team_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newOpenAPIRequest()
			req.Product, req.ApiName = "FCSandbox", "CreateVolume"
			req.SetContent([]byte(tc.body))
			var app *ecerrors.AppError
			if err := (&OpenAPICaller{}).validateVolumeRequest(req); !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeInput" || app.Payload().Field != tc.field {
				t.Fatalf("invalid %s request reached provider: %v", tc.field, err)
			}
		})
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	req, err := caller.commonRequest("CreateVolume", map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	req.SetContent([]byte(`{"volumeName":"workspace"`))
	response := map[string]any{"code": "200", "requestId": "req-create", "volume": map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING"}}
	var app *ecerrors.AppError
	if err := caller.fcVolumeResponseError(req, response); !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" {
		t.Fatalf("malformed outbound request proved an allocation: %v", err)
	}
	if volumeErrorEnvelopeProof(map[string]any{"code": "VolumeNotFound", "Code": "VolumeNotFound", "requestId": "req-1"}, "VolumeNotFound", "req-1") {
		t.Fatal("duplicate code aliases proved absence")
	}
	if scrubVolumeResponse(nil) != nil {
		t.Fatal("nil provider response became a resource")
	}
}

func TestCreateVolumeNeverRetriesAnAmbiguousThrottle(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callErrors: []error{errors.New("SDK.ServerError\nErrorCode: Throttling.User\nMessage: flow control")}, response: `{"code":"200","volume":{"volumeID":"vol-later"}}`}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 4, sleepFn: func(context.Context, time.Duration) error { return nil }}
	_, err := caller.Call(context.Background(), "CreateVolume", map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"})
	if err == nil || len(fake.requests) != 1 {
		t.Fatalf("CreateVolume attempts = %d, error = %v; accepted a second allocation attempt", len(fake.requests), err)
	}
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Retryable {
		t.Fatalf("ambiguous CreateVolume throttle was advertised as safe to retry: %v", err)
	}
}

func TestFCSandboxVolumeBusinessResponses(t *testing.T) {
	for _, tc := range []struct {
		name, api, response, kind string
	}{
		{"business error", "DeleteVolume", `{"code":"409","message":"volume busy","requestId":"req-business"}`, "service"},
		{"missing code", "DeleteVolume", `{"message":"success","requestId":"req-business"}`, "service"},
		{"missing volume", "GetVolume", `{"code":"200","requestId":"req-business"}`, "service"},
		{"wrong volume", "GetVolume", `{"code":"200","volume":{"volumeID":"other","teamID":"team-1","status":"AVAILABLE"},"requestId":"req-business"}`, "service"},
		{"wrong team", "GetVolume", `{"code":"200","volume":{"volumeID":"vol-1","teamID":"other","status":"AVAILABLE"},"requestId":"req-business"}`, "service"},
		{"missing status", "GetVolume", `{"code":"200","volume":{"volumeID":"vol-1","teamID":"team-1"},"requestId":"req-business"}`, "service"},
		{"bad config", "GetVolume", `{"code":"200","volume":{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE","mountConfig":"wrong"},"requestId":"req-business"}`, "service"},
		{"missing list", "ListVolumes", `{"code":"200","requestId":"req-business"}`, "service"},
		{"bad token", "ListVolumes", `{"code":"200","volumes":[],"nextToken":42,"requestId":"req-business"}`, "service"},
		{"duplicate ID", "ListVolumes", `{"code":"200","volumes":[{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE"},{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE"}],"requestId":"req-business"}`, "service"},
		{"not found", "GetVolume", `{"code":"VolumeNotFound","message":"gone","requestId":"req-business"}`, "not_found"},
		{"unrelated missing", "GetVolume", `{"code":"ResourceGroupNotFound","message":"group gone","requestId":"req-business"}`, "service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{}
			if tc.api == "GetVolume" || tc.api == "DeleteVolume" {
				request["volumeID"], request["teamID"] = "vol-1", "team-1"
			}
			_, err := caller.Call(context.Background(), tc.api, request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Kind != tc.kind || ecerrors.ActionFromError(tc.api, err).RequestID != "req-business" {
				t.Fatalf("Volume response error = %v, want %s with request ID", err, tc.kind)
			}
			if tc.name == "business error" && ecerrors.ActionFromError(tc.api, err).Code != "409" {
				t.Fatalf("business code was lost: %v", err)
			}
		})
	}
	for _, tc := range []struct{ api, response string }{
		{"CreateVolume", `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`},
		{"GetVolume", `{"code":"200","volume":{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE"}}`},
		{"ListVolumes", `{"code":"200","volumes":[],"nextToken":"","maxResults":1}`},
		{"DeleteVolume", `{"code":"200","message":"success"}`},
	} {
		t.Run("success "+tc.api, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{}
			switch tc.api {
			case "CreateVolume":
				request = map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
			case "GetVolume", "DeleteVolume":
				request = map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
			case "ListVolumes":
				request = map[string]any{"maxResults": 1}
			}
			if _, err := caller.Call(context.Background(), tc.api, request); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFCSandboxVolumeCreateHTTPStatusRequiresAcceptedIdentity(t *testing.T) {
	request := map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
	for _, tc := range []struct {
		name   string
		status int
		body   string
		accept bool
	}{
		{"created", 201, `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`, true},
		{"accepted", 202, `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`, true},
		{"wrong team", 201, `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-2","status":"CREATING"}}`, false},
		{"missing ID", 201, `{"code":"200","requestId":"req-create","volume":{"volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`, false},
		{"missing request ID", 201, `{"code":"200","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`, false},
		{"business failure", 201, `{"code":"VolumeConflict","requestId":"req-create"}`, false},
		{"partial response", 206, `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{status: tc.status, body: tc.body}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
			response, err := caller.CallRaw(context.Background(), "CreateVolume", request)
			if tc.accept {
				if err != nil {
					t.Fatalf("HTTP %d complete CreateVolume response rejected: %v", tc.status, err)
				}
				volume, _ := response["volume"].(map[string]any)
				if volume["volumeID"] != "vol-1" {
					t.Fatalf("accepted identity missing: %#v", response)
				}
			} else if err == nil {
				t.Fatalf("HTTP %d invalid CreateVolume response accepted: %#v", tc.status, response)
			}
		})
	}
	t.Run("GetVolume success remains HTTP 200 only", func(t *testing.T) {
		profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
		executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
		if err != nil {
			t.Fatal(err)
		}
		executor.client.HttpClient = sdkErrorHTTPClient{status: 201, body: `{"code":"200","requestId":"req-get","volume":{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE"}}`}
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
		if _, err := caller.CallRaw(context.Background(), "GetVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"}); err == nil {
			t.Fatal("GetVolume accepted HTTP 201")
		}
	})
}

func TestCreateVolumeRetainsAcceptedIdentityWhenOptionalOutputIsMalformed(t *testing.T) {
	for _, withTeam := range []bool{false, true} {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-create","volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING","resourceGroupID":42}}`}}
		request := map[string]any{"body.volumeName": "workspace"}
		if withTeam {
			request["body.teamID"] = "team-1"
		}
		_, err := caller.CallRaw(context.Background(), "CreateVolume", request)
		var app *ecerrors.AppError
		accepted, ok := ecerrors.AcceptedVolumeFrom(err)
		if !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" || !ok || accepted.ID != "vol-1" || accepted.Name != "workspace" || accepted.TeamID != "team-1" || accepted.Region != "cn-hangzhou" || accepted.RequestID != "req-create" || accepted.Operation != "CreateVolume" || !accepted.AllocationConfirmed {
			t.Fatalf("withTeam=%t: malformed optional output lost the accepted create tuple: %+v %v", withTeam, accepted, err)
		}
	}
}

func TestVolumeSuccessfulMutationRejectsUnsolicitedDryRunBeforeEngineReceipt(t *testing.T) {
	response := `{"code":"200","requestId":"req-create","DryRun":true,"volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}}`
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: response}}
	resource, err := spec.LoadResource("../../specs", "sandbox", "volume")
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"name": "workspace", "team_id": "team-1"}})
	var app *ecerrors.AppError
	accepted, hasAccepted := ecerrors.AcceptedVolumeFrom(err)
	if !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" || result.DryRun || !hasAccepted || accepted.ID != "vol-1" || accepted.TeamID != "team-1" || accepted.AllocationConfirmed || result.ID != "" {
		t.Fatalf("contradictory Volume response lost accepted allocation: result=%+v accepted=%+v err=%v", result, accepted, err)
	}
	for _, operation := range []string{"DeleteVolume"} {
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","DryRun":true,"requestId":"req-mutate","volume":{"volumeID":"vol-1","teamID":"team-1","status":"AVAILABLE"}}`}}
		request := map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
		if _, err := caller.CallRaw(context.Background(), operation, request); !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" {
			t.Fatalf("%s accepted unsolicited DryRun: %v", operation, err)
		}
	}
}

func TestVolumePositiveEnvelopeRejectsContradictoryMutationEvidence(t *testing.T) {
	volume := `"volume":{"volumeID":"vol-1","volumeName":"workspace","teamID":"team-1","status":"CREATING"}`
	for _, tc := range []struct {
		name, api, response string
	}{
		{"create failure flag", "CreateVolume", `{"code":"200","requestId":"req-create","success":false,` + volume + `}`},
		{"create partial error", "CreateVolume", `{"code":"200","requestId":"req-create","error":{"code":"rejected"},` + volume + `}`},
		{"create request alias", "CreateVolume", `{"code":"200","requestId":"req-create","request_id":"req-other",` + volume + `}`},
		{"delete error", "DeleteVolume", `{"code":"200","error":{"code":"busy"}}`},
		{"delete resource", "DeleteVolume", `{"code":"200","volume":{"volumeID":"vol-1"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{"volumeID": "vol-1", "teamID": "team-1", "body.teamID": "team-1"}
			if tc.api == "CreateVolume" {
				request = map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
			}
			_, err := caller.CallRaw(context.Background(), tc.api, request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeResponse" {
				t.Fatalf("contradictory positive response was accepted: %v", err)
			}
			if _, accepted := ecerrors.AcceptedVolumeFrom(err); accepted {
				t.Fatalf("contradictory response promoted an owned Volume: %v", err)
			}
		})
	}
}

func TestVolumeRejectsConflictingBusinessCodeAliases(t *testing.T) {
	for _, operation := range []string{"CreateVolume", "GetVolume", "ListVolumes", "DeleteVolume"} {
		t.Run(operation, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume"}
			req := &openAPIRequest{ApiName: operation}
			response := map[string]any{"code": "200", "Code": "409"}
			if err := caller.fcVolumeResponseError(req, response); err == nil {
				t.Fatal("conflicting business code was accepted")
			}
		})
	}
}

func TestVolumeStatusReasonIsScrubbedBeforeReturn(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-read","volume":{"volumeID":"vol-1","teamID":"team-1","status":"ERROR","statusReason":"token=reviewer-canary"}}`}}
	response, err := caller.CallRaw(context.Background(), "GetVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"})
	if err != nil {
		t.Fatal(err)
	}
	volume, _ := response["volume"].(map[string]any)
	if volume["statusReason"] != "[REDACTED]" {
		t.Fatalf("Volume status reason leaked: %#v", volume)
	}
}

func TestFCSandboxVolumeBusinessRejectionCarriesExactProviderProof(t *testing.T) {
	for _, tc := range []struct {
		api, code, response string
		verified            bool
	}{
		{"DeleteVolume", "Forbidden", `{"code":"Forbidden","message":"denied","requestId":"req-delete"}`, true},
		{"CreateVolume", "VolumeConflict", `{"code":"VolumeConflict","message":42,"requestId":"req-create"}`, false},
		{"DeleteVolume", "Forbidden", `{"code":"Forbidden","hostId":42,"requestId":"req-delete"}`, false},
	} {
		t.Run(tc.api+"/"+tc.response, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			request := map[string]any{"volumeID": "vol-1", "teamID": "team-1", "body.teamID": "team-2"}
			_, err := caller.Call(context.Background(), tc.api, request)
			action := ecerrors.ActionFromError(tc.api, err)
			if err == nil || action.Code != tc.code || action.ProviderVerified != tc.verified {
				t.Fatalf("business rejection proof = %+v, err=%v", action, err)
			}
		})
	}
}

func TestFCSandboxVolumeGeneric404IsService(t *testing.T) {
	request := map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
	for _, tc := range []struct {
		name   string
		caller *OpenAPICaller
	}{
		{
			name:   "HTTP 200 business code 404",
			caller: &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"404","message":"route missing","requestId":"req-generic"}`}},
		},
		{
			name: "HTTP 404 transport response",
			caller: func() *OpenAPICaller {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.HttpClient = sdkErrorHTTPClient{status: 404, body: `{"code":"404","message":"route missing","requestId":"req-generic"}`}
				return &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.caller.CallRaw(context.Background(), "GetVolume", request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Kind != "service" || app.Payload().Code != "CloudAPIError" {
				t.Fatalf("generic 404 proved Volume absence: %v", err)
			}
			if action := ecerrors.ActionFromError("GetVolume", err); action.Code != "404" || action.RequestID != "req-generic" {
				t.Fatalf("generic provider diagnostic was lost: %+v", action)
			}
		})
	}
}

func TestFCSandboxVolumeConflictIsVerifiedOnlyFromStructuredCreateResponse(t *testing.T) {
	request := map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
	for _, tc := range []struct {
		name, response string
		verified       bool
	}{
		{"exact conflict", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict"}`, true},
		{"generic conflict", `{"code":"409","message":"name exists","requestId":"req-conflict"}`, false},
		{"missing request ID", `{"code":"VolumeConflict","message":"name exists"}`, false},
		{"contradictory volume", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict","volume":{"volumeID":"vol-other"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.response}}
			_, err := caller.Call(context.Background(), "CreateVolume", request)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || ecerrors.ActionFromError("CreateVolume", err).ProviderVerified != tc.verified {
				t.Fatalf("CreateVolume conflict proof = %v, want %v", err, tc.verified)
			}
		})
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: errors.New("VolumeConflict requestId=req-conflict")}}
	_, err := caller.CallRaw(context.Background(), "CreateVolume", request)
	if ecerrors.ActionFromError("CreateVolume", err).ProviderVerified {
		t.Fatal("untyped diagnostic text proved a create rejection")
	}
	for _, tc := range []struct {
		name, body string
		status     int
		verified   bool
	}{
		{"typed 409 conflict", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict"}`, 409, true},
		{"typed 409 conflict message says in use", `{"code":"VolumeConflict","message":"name already in use","requestId":"req-conflict"}`, 409, true},
		{"typed 404 conflict", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict"}`, 404, false},
		{"typed 404 conflict with allocation", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict","volume":{"volumeID":"vol-other"}}`, 404, false},
		{"typed 500 conflict", `{"code":"VolumeConflict","message":"name exists","requestId":"req-conflict"}`, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.HttpClient = sdkErrorHTTPClient{status: tc.status, body: tc.body}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
			_, err = caller.CallRaw(context.Background(), "CreateVolume", request)
			if ecerrors.ActionFromError("CreateVolume", err).ProviderVerified != tc.verified {
				t.Fatalf("HTTP %d conflict proof = %v, want %v", tc.status, err, tc.verified)
			}
		})
	}
}

func TestFCSandboxVolumeTransportAbsenceRequiresStructuredCode(t *testing.T) {
	request := map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
	for _, status := range []int{400, 404} {
		for _, disableSDKError := range []bool{false, true} {
			t.Run(fmt.Sprintf("typed SDK error status=%d disabled=%t", status, disableSDKError), func(t *testing.T) {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.DisableSDKError = &disableSDKError
				executor.client.HttpClient = sdkErrorHTTPClient{status: status, body: `{"code":"VolumeNotFound","message":"gone","requestId":"req-specific"}`}
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
				_, err = caller.CallRaw(context.Background(), "GetVolume", request)
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Kind != "not_found" || ecerrors.ActionFromError("GetVolume", err).Code != "VolumeNotFound" || !ecerrors.ActionFromError("GetVolume", err).ProviderVerified {
					t.Fatalf("typed Volume-specific absence was rejected: %v", err)
				}
			})
		}
	}
	untyped := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{callError: errors.New("SDK.ServerError\nErrorCode: VolumeNotFound\nMessage: notfound route text\nRequestId: req-plain")}}
	_, err := untyped.CallRaw(context.Background(), "GetVolume", request)
	var app *ecerrors.AppError
	if !errors.As(err, &app) || app.Payload().Kind != "service" || ecerrors.ActionFromError("GetVolume", err).Code != "VolumeNotFound" {
		t.Fatalf("untyped error text proved Volume absence or lost diagnostic: %v", err)
	}
}

func TestFCSandboxVolumeEnriched404AbsenceReachesAction(t *testing.T) {
	caller := VolumeSDKErrorCallerForTest(t, 404, `{"code":"VolumeNotFound","httpStatusCode":404,"message":"gone","requestId":"req-wire","success":false}`)
	var err error
	_, err = caller.CallRaw(context.Background(), "GetVolume", map[string]any{"volumeID": "vol-1", "teamID": "team-1"})
	var app *ecerrors.AppError
	action := ecerrors.ActionFromError("GetVolume", err)
	if !errors.As(err, &app) || app.Payload().Kind != "not_found" || action.Code != "VolumeNotFound" || action.RequestID != "req-wire" || !action.ProviderVerified {
		t.Fatalf("enriched SDK 404 did not prove scoped absence: app=%v action=%+v", err, action)
	}
}

// VolumeSDKErrorCallerForTest drives external-package CLI tests through the
// real SDK error parser while replacing only the remote HTTP exchange.
func VolumeSDKErrorCallerForTest(t *testing.T, status int, body string) *OpenAPICaller {
	t.Helper()
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	executor.client.HttpClient = sdkErrorHTTPClient{status: status, body: body}
	return &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
}

func TestFCSandboxVolumeAbsenceRejectsContradictoryProviderResponses(t *testing.T) {
	request := map[string]any{"volumeID": "vol-1", "teamID": "team-1"}
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		business bool
		verified bool
	}{
		{"business exact", 200, `{"code":"VolumeNotFound","requestId":"req-1"}`, true, true},
		{"business missing ID", 200, `{"code":"VolumeNotFound"}`, true, false},
		{"business conflicting Volume", 200, `{"code":"VolumeNotFound","requestId":"req-1","volume":{"volumeID":"vol-1"}}`, true, false},
		{"business recovery", 200, `{"code":"VolumeNotFound","requestId":"req-1","recovery_command":"retry"}`, true, false},
		{"business success flag", 200, `{"code":"VolumeNotFound","requestId":"req-1","success":true}`, true, false},
		{"business enriched negative", 200, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":false}`, true, false},
		{"business plural Volumes", 200, `{"code":"VolumeNotFound","requestId":"req-1","volumes":[{"volumeID":"vol-1"}]}`, true, false},
		{"business conflicting request alias", 200, `{"code":"VolumeNotFound","requestId":"req-1","request_id":"req-other"}`, true, false},
		{"business repeated request alias", 200, `{"code":"VolumeNotFound","requestId":"req-1","request_id":"req-1"}`, true, false},
		{"business duplicate field", 200, `{"code":"VolumeNotFound","requestId":"req-1","code":"VolumeNotFound"}`, true, false},
		{"typed wrong status", 403, `{"code":"VolumeNotFound","requestId":"req-1"}`, false, false},
		{"typed server status", 500, `{"code":"VolumeNotFound","requestId":"req-1"}`, false, false},
		{"typed 400 conflicting Volume", 400, `{"code":"VolumeNotFound","requestId":"req-1","volume":{"volumeID":"vol-1"}}`, false, false},
		{"typed 400 missing ID", 400, `{"code":"VolumeNotFound"}`, false, false},
		{"typed conflicting Volume", 404, `{"code":"VolumeNotFound","requestId":"req-1","volume":{"volumeID":"vol-1"}}`, false, false},
		{"typed success flag", 404, `{"code":"VolumeNotFound","requestId":"req-1","success":true}`, false, false},
		{"typed plural Volumes", 404, `{"code":"VolumeNotFound","requestId":"req-1","volumes":[{"volumeID":"vol-1"}]}`, false, false},
		{"typed missing ID", 404, `{"code":"VolumeNotFound"}`, false, false},
		{"typed duplicate field", 404, `{"code":"VolumeNotFound","requestId":"req-1","requestId":"req-1"}`, false, false},
		{"typed enriched wrong status", 404, `{"code":"VolumeNotFound","httpStatusCode":200,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched fractional status", 404, `{"code":"VolumeNotFound","httpStatusCode":404.5,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched string status", 404, `{"code":"VolumeNotFound","httpStatusCode":"404","requestId":"req-1","success":false}`, false, false},
		{"typed enriched missing status", 404, `{"code":"VolumeNotFound","requestId":"req-1","success":false}`, false, false},
		{"typed enriched success", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":true}`, false, false},
		{"typed enriched string success", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":"false"}`, false, false},
		{"typed enriched missing success", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1"}`, false, false},
		{"typed enriched status alias", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"http_status_code":404,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched success alias", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":false,"Success":false}`, false, false},
		{"typed enriched request alias", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","request_id":"req-other","success":false}`, false, false},
		{"typed enriched duplicate status", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"httpStatusCode":404,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched duplicate success", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":false,"success":false}`, false, false},
		{"typed enriched resource payload", 404, `{"code":"VolumeNotFound","httpStatusCode":404,"requestId":"req-1","success":false,"volume":{"volumeID":"vol-1"}}`, false, false},
		{"typed enriched wrong code", 404, `{"code":"OtherError","httpStatusCode":404,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched wrong HTTP status", 403, `{"code":"VolumeNotFound","httpStatusCode":403,"requestId":"req-1","success":false}`, false, false},
		{"typed enriched HTTP 400", 400, `{"code":"VolumeNotFound","httpStatusCode":400,"requestId":"req-1","success":false}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, disableSDKError := range []bool{false, true} {
				var caller *OpenAPICaller
				if tc.business {
					caller = &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: tc.body}}
				} else {
					profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
					executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
					if err != nil {
						t.Fatal(err)
					}
					executor.client.DisableSDKError = &disableSDKError
					executor.client.HttpClient = sdkErrorHTTPClient{status: tc.status, body: tc.body}
					caller = &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
				}
				_, err := caller.CallRaw(context.Background(), "GetVolume", request)
				var app *ecerrors.AppError
				got := errors.As(err, &app) && app.Payload().Kind == "not_found" && ecerrors.ActionFromError("GetVolume", err).ProviderVerified
				if got != tc.verified {
					t.Fatalf("disable SDK error=%v: absence proof=%v, err=%v", disableSDKError, got, err)
				}
			}
		})
	}
}

func TestFCSandboxVolumeTypedConflictRejectsAllocationPayload(t *testing.T) {
	request := map[string]any{"body.volumeName": "workspace", "body.teamID": "team-1"}
	for _, tc := range []struct {
		name, body string
		verified   bool
	}{
		{"exact", `{"code":"VolumeConflict","requestId":"req-1"}`, true},
		{"allocated", `{"code":"VolumeConflict","requestId":"req-1","volume":{"volumeID":"vol-1"}}`, false},
		{"nested allocation", `{"code":"VolumeConflict","requestId":"req-1","data":{"volumeID":"vol-1"}}`, false},
		{"recovery", `{"code":"VolumeConflict","requestId":"req-1","recovery_command":"retry"}`, false},
		{"missing ID", `{"code":"VolumeConflict"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, disableSDKError := range []bool{false, true} {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.DisableSDKError = &disableSDKError
				executor.client.HttpClient = sdkErrorHTTPClient{status: 409, body: tc.body}
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", Profile: profile, executor: executor}
				_, err = caller.CallRaw(context.Background(), "CreateVolume", request)
				if got := ecerrors.ActionFromError("CreateVolume", err).ProviderVerified; got != tc.verified {
					t.Fatalf("disable SDK error=%v: conflict proof=%v, err=%v", disableSDKError, got, err)
				}
			}
		})
	}
}

func TestFCSandboxVolumeRequestRejectsPathInjection(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "volume", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200"}`}}
	for _, input := range []map[string]any{{"volumeID": "other/path"}, {"volumeID": "../other"}, {"volumeID": "vol-1", "teamID": "team/other"}} {
		_, err := caller.Call(context.Background(), "GetVolume", input)
		var app *ecerrors.AppError
		if !errors.As(err, &app) || app.Payload().Code != "InvalidVolumeInput" {
			t.Fatalf("path injection reached provider: %v", err)
		}
	}
}

func jsonRoundTripValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func valueToString(value any) string { return fmt.Sprint(value) }
