package aliyun

import (
	"context"
	"errors"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/sandbox"
)

func TestFCSandboxQuotaROARequestContract(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "quota", Region: "cn-beijing", Profile: resolvedOpenAPIProfile{Language: "en"}}
	for _, tc := range []struct {
		api, method, path string
		values            map[string]any
	}{
		{"DescribeQuota", "GET", "/pop/2026-05-09/quotas/tag", map[string]any{"tagValue": "team-1"}},
		{"ListQuota", "GET", "/pop/2026-05-09/quotas", map[string]any{"maxResults": 2, "nextToken": "page-2"}},
		{"UpdateQuota", "PUT", "/pop/2026-05-09/quotas/tag", map[string]any{"body.tagValue": "team-1", "body.cpuCores": 32, "body.memoryGB": 64}},
		{"DeleteQuota", "DELETE", "/pop/2026-05-09/quotas/tag", map[string]any{"tagValue": "team-1"}},
	} {
		t.Run(tc.api, func(t *testing.T) {
			req, err := caller.commonRequest(tc.api, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if req.Method != tc.method || req.BuildPath() != tc.path || req.Domain != "fcsandbox.cn-beijing.aliyuncs.com" || req.Version != "2026-05-09" {
				t.Fatalf("wrong ROA request: %+v", req)
			}
			if tc.api == "UpdateQuota" {
				body, ok := req.BodyValue().(map[string]any)
				if !ok || body["tagValue"] != "team-1" || body["cpuCores"] != float64(32) || body["memoryGB"] != float64(64) || len(req.QueryParams) != 0 {
					t.Fatalf("update must send a JSON Quota body: %+v", req)
				}
			} else if tc.api == "ListQuota" {
				if req.QueryParams["maxResults"] != "2" || req.QueryParams["nextToken"] != "page-2" || req.BodyValue() != nil {
					t.Fatalf("list pagination must use query: %+v", req)
				}
			} else if req.QueryParams["tagValue"] != "team-1" || req.BodyValue() != nil {
				t.Fatalf("Team ID must be a query value: %+v", req)
			}
		})
	}
}

func TestFCSandboxQuotaWorkflow(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action, response, api string
		input                 map[string]any
	}{
		{"get", `{"code":"200","requestId":"req-1","quota":{"tagValue":"team-1","cpuCores":32,"memoryGB":64}}`, "DescribeQuota", map[string]any{"id": "team-1"}},
		{"list", `{"code":"200","requestId":"req-1","quotas":[{"tagValue":"team-1","cpuCores":32,"memoryGB":64}],"maxResults":2,"nextToken":"page-2"}`, "ListQuota", map[string]any{"limit": 2, "next_token": "page-1"}},
		{"update", `{"code":"200","requestId":"req-1","quota":{"tagValue":"team-1","cpuCores":32,"memoryGB":64}}`, "UpdateQuota", map[string]any{"id": "team-1", "cpu_cores": 32, "memory_gb": 64}},
		{"delete", `{"code":"200","message":"success","requestId":"req-1"}`, "DeleteQuota", map[string]any{"id": "team-1"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{response: tc.response}
			if tc.action == "delete" {
				fake.responses = []string{tc.response, `{"code":"ResourceQuotaNotFound","message":"gone","requestId":"req-absent"}`}
			}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "quota", Region: "cn-beijing", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: fake}
			result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: tc.action, Input: tc.input})
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 1
			if tc.action == "delete" {
				wantCalls = 2
			}
			if len(fake.requests) != wantCalls || fake.requests[0].ApiName != tc.api || len(result.Actions) != wantCalls || result.Actions[0].RequestID != "req-1" {
				t.Fatalf("unexpected action trace: result=%+v requests=%+v", result, fake.requests)
			}
			if tc.action == "delete" {
				if fake.requests[1].ApiName != "DescribeQuota" || fake.requests[1].QueryParams["tagValue"] != "team-1" || result.Actions[1].Code != "ResourceQuotaNotFound" || result.Actions[1].RequestID != "req-absent" {
					t.Fatalf("bounded quota absence lost Team identity/evidence: result=%+v requests=%+v", result, fake.requests)
				}
				if !result.Deleted || result.Item["team_id"] != "team-1" {
					t.Fatalf("delete result = %+v", result)
				}
			} else if tc.action == "list" {
				if len(result.Items) != 1 || result.Items[0]["team_id"] != "team-1" || result.NextToken != "page-2" {
					t.Fatalf("list result = %+v", result)
				}
			} else if result.Item["team_id"] != "team-1" || result.Item["cpu_cores"] != float64(32) || result.Item["memory_gb"] != float64(64) {
				t.Fatalf("%s field mapping = %+v", tc.action, result)
			}
		})
	}
}

func TestFCSandboxQuotaListAcceptsSmallerResponsePages(t *testing.T) {
	resource, err := spec.LoadResource("../../specs", "sandbox", "quota")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{responses: []string{
		`{"code":"200","requestId":"req-1","maxResults":10,"nextToken":"opaque-next","quotas":[{"tagValue":"other-team","cpuCores":1,"memoryGB":2}]}`,
		`{"code":"200","requestId":"req-2","maxResults":10,"quotas":[{"tagValue":"owned-team","cpuCores":32,"memoryGB":64}]}`,
	}}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "quota", Region: "cn-beijing", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: fake}
	result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "list", Input: map[string]any{"limit": 100, "all": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 2 || fake.requests[0].ApiName != "ListQuota" || fake.requests[1].ApiName != "ListQuota" || fake.requests[0].QueryParams["maxResults"] != "100" || fake.requests[1].QueryParams["maxResults"] != "100" {
		t.Fatalf("quota list requests = %+v", fake.requests)
	}
	if first := fake.requests[0].QueryParams["nextToken"]; first != "" {
		t.Fatalf("first page sent a token: %q", first)
	}
	if fake.requests[1].QueryParams["nextToken"] != "opaque-next" || len(result.Items) != 2 || result.Items[0]["team_id"] != "other-team" || result.Items[1]["team_id"] != "owned-team" || result.NextToken != "" {
		t.Fatalf("smaller response pages lost owned quota or token: requests=%+v result=%+v", fake.requests, result)
	}
	if len(result.Actions) != 2 || result.Actions[0].RequestID != "req-1" || result.Actions[1].RequestID != "req-2" {
		t.Fatalf("page request evidence = %+v", result.Actions)
	}
}

func TestFCSandboxQuotaRejectsBusinessAndMalformedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, api, response, code string
		values                    map[string]any
	}{
		{"business delete failure", "DeleteQuota", `{"code":"QuotaInUse","message":"quota is in use","requestId":"req-1"}`, "CloudAPIError", map[string]any{"tagValue": "team-1"}},
		{"business documented code on delete", "DeleteQuota", `{"code":"ResourceQuotaNotFound","message":"missing","requestId":"req-1"}`, "CloudAPIError", map[string]any{"tagValue": "team-1"}},
		{"business documented code on update", "UpdateQuota", `{"code":"ResourceQuotaNotFound","message":"missing","requestId":"req-1"}`, "CloudAPIError", map[string]any{"body.tagValue": "team-1", "body.cpuCores": 32, "body.memoryGB": 64}},
		{"missing code", "DeleteQuota", `{"requestId":"req-1"}`, "InvalidQuotaResponse", map[string]any{"tagValue": "team-1"}},
		{"numeric code", "DeleteQuota", `{"code":200,"requestId":"req-1"}`, "InvalidQuotaResponse", map[string]any{"tagValue": "team-1"}},
		{"missing get quota", "DescribeQuota", `{"code":"200","requestId":"req-1"}`, "InvalidQuotaResponse", map[string]any{"tagValue": "team-1"}},
		{"wrong team", "DescribeQuota", `{"code":"200","quota":{"tagValue":"team-2","cpuCores":32,"memoryGB":64}}`, "InvalidQuotaResponse", map[string]any{"tagValue": "team-1"}},
		{"wrong memory unit shape", "DescribeQuota", `{"code":"200","quota":{"tagValue":"team-1","cpuCores":32,"memoryGB":"64"}}`, "InvalidQuotaResponse", map[string]any{"tagValue": "team-1"}},
		{"missing list", "ListQuota", `{"code":"200","nextToken":""}`, "InvalidQuotaResponse", nil},
		{"bad token", "ListQuota", `{"code":"200","quotas":[],"nextToken":false}`, "InvalidQuotaResponse", nil},
		{"bad response page size", "ListQuota", `{"code":"200","quotas":[],"maxResults":0}`, "InvalidQuotaResponse", map[string]any{"maxResults": 100}},
		{"noninteger response page size", "ListQuota", `{"code":"200","quotas":[],"maxResults":1.5}`, "InvalidQuotaResponse", map[string]any{"maxResults": 100}},
		{"too many quotas for response page size", "ListQuota", `{"code":"200","quotas":[{"tagValue":"team-1","cpuCores":1,"memoryGB":2},{"tagValue":"team-2","cpuCores":3,"memoryGB":4}],"maxResults":1}`, "InvalidQuotaResponse", map[string]any{"maxResults": 100}},
		{"business list failure", "ListQuota", `{"code":"QuotaBusy","message":"busy","requestId":"req-list"}`, "CloudAPIError", map[string]any{"maxResults": 100}},
		{"duplicate team", "ListQuota", `{"code":"200","quotas":[{"tagValue":"team-1","cpuCores":1,"memoryGB":2},{"tagValue":"team-1","cpuCores":3,"memoryGB":4}]}`, "InvalidQuotaResponse", nil},
		{"missing update quota", "UpdateQuota", `{"code":"200","requestId":"req-1"}`, "InvalidQuotaResponse", map[string]any{"body.tagValue": "team-1", "body.cpuCores": 32, "body.memoryGB": 64}},
		{"quota absent", "DescribeQuota", `{"code":"QuotaNotFound","message":"gone","requestId":"req-1"}`, "NotFound", map[string]any{"tagValue": "team-1"}},
		{"documented quota absence", "DescribeQuota", `{"code":"ResourceQuotaNotFound","message":"documented missing quota","requestId":"req-1"}`, "NotFound", map[string]any{"tagValue": "team-1"}},
		{"quota absent HTTP code", "DescribeQuota", `{"code":"404","message":"gone","requestId":"req-1"}`, "NotFound", map[string]any{"tagValue": "team-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "quota", Region: "cn-beijing", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{response: tc.response}}
			_, err := caller.CallRaw(context.Background(), tc.api, tc.values)
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if tc.name == "quota absent" || tc.name == "documented quota absence" || tc.name == "quota absent HTTP code" {
				wantCode := "QuotaNotFound"
				wantMessage := "gone"
				if tc.name == "documented quota absence" {
					wantCode, wantMessage = "ResourceQuotaNotFound", "documented missing quota"
				}
				if tc.name == "quota absent HTTP code" {
					wantCode = "404"
				}
				action := ecerrors.ActionFromError(tc.api, err)
				if app.ExitCode() != 4 || action.ActionName != "DescribeQuota" || action.Code != wantCode || action.Message != wantMessage || action.RequestID != "req-1" {
					t.Fatalf("post-delete absence action = %+v, exit=%d", action, app.ExitCode())
				}
			}
		})
	}
}

func TestFCSandboxQuotaHTTPAbsenceIsScopedToDescribe(t *testing.T) {
	for _, tc := range []struct {
		api, code, kind string
	}{
		{"DescribeQuota", "QuotaNotFound", "not_found"},
		{"DescribeQuota", "ResourceQuotaNotFound", "not_found"},
		{"DescribeQuota", "404", "not_found"},
		{"DescribeQuota", "TeamNotFound", "service"},
		{"DeleteQuota", "QuotaNotFound", "service"},
		{"DeleteQuota", "ResourceQuotaNotFound", "service"},
		{"UpdateQuota", "ResourceQuotaNotFound", "service"},
	} {
		t.Run(tc.api+"/"+tc.code, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "quota", Region: "cn-beijing", executor: &fakeOpenAPIExecutor{callError: errors.New("SDK.ServerError\nErrorCode: " + tc.code + "\nMessage: missing\nRequestId: req-missing")}}
			_, err := caller.CallRaw(context.Background(), tc.api, map[string]any{"tagValue": "team-1"})
			var app *ecerrors.AppError
			action := ecerrors.ActionFromError(tc.api, err)
			if !errors.As(err, &app) || app.Payload().Kind != tc.kind || action.Code != tc.code || action.Message != "missing" || action.RequestID != "req-missing" {
				t.Fatalf("quota HTTP absence = %v, action=%+v; want %s", err, action, tc.kind)
			}
		})
	}
}
