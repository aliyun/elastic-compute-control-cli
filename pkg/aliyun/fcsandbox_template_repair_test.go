package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

// NativeTemplateOutputCallerForTest connects external-package CLI tests to the
// real provider boundary without adding a production executor injection API.
func NativeTemplateOutputCallerForTest(t *testing.T, responses []string, sdkStatus int) *OpenAPICaller {
	t.Helper()
	fake := &fakeOpenAPIExecutor{responses: responses}
	if len(responses) > 0 {
		// Waiter tests keep observing the last state, independent of poll rate.
		fake.response = responses[len(responses)-1]
	}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
	if sdkStatus != 0 {
		profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
		executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
		if err != nil {
			t.Fatal(err)
		}
		executor.client.HttpClient = sdkErrorHTTPClient{status: sdkStatus, body: responses[0]}
		caller.executor = executor
	}
	return caller
}

func NativeTemplateSTSCallerForTest(t *testing.T, expiresAt time.Time) (*OpenAPICaller, func() int) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "ecctl.json")
	data, err := json.Marshal(map[string]any{"current": "short-sts", "profiles": []any{map[string]any{"name": "short-sts", "mode": "StsToken", "access_key_id": "test-ak", "access_key_secret": "test-secret", "sts_token": "test-token", "sts_expiration": expiresAt.Unix()}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	caller, err := NewOpenAPICaller("short-sts", config, "FCSandbox", "cn-hangzhou", mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": filepath.Join(dir, "missing-aliyun.json")}))
	if err != nil {
		t.Fatal(err)
	}
	caller.Resource = "template"
	fake := &fakeOpenAPIExecutor{responses: []string{`{"code":"200","requestId":"req-write","templateID":"tpl-1"}`, `{"code":"200","requestId":"req-get","templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}`}}
	caller.executor = fake
	return caller, func() int { return len(fake.requests) }
}

func NativeTemplatePaginationCallerForTest(t *testing.T, token, username string) (*OpenAPICaller, func() string) {
	t.Helper()
	item := map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "ready"}}
	if username != "" {
		item["runtimeConfig"] = map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"userName": username}}}}
	}
	data, err := json.Marshal(map[string]any{"code": "200", "templates": []any{item}, "nextToken": token})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{responses: []string{string(data), `{"code":"200","templates":[],"nextToken":""}`}}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
	return caller, func() string {
		if len(fake.requests) != 2 {
			return "missing second list request"
		}
		return fake.requests[1].QueryParams["nextToken"]
	}
}

func TestNativeTemplateRedactionKeepsReadRetryability(t *testing.T) {
	for _, auth := range []map[string]any{{"userName": "registry-user"}, {"userName": "i"}, {"password": "i"}, {"userName": "i", "password": "o"}} {
		for _, action := range []string{"create", "update"} {
			fake := &fakeOpenAPIExecutor{responses: []string{`{"code":"200","templateID":"tpl-1"}`, "", "", `{"code":"200","requestId":"req-ready","templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}`}, callErrors: []error{nil, errors.New("i/o timeout"), errors.New("i/o timeout")}}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 2, sleepFn: func(context.Context, time.Duration) error { return nil }}
			resource, err := spec.LoadResource("", "sandbox", "template")
			if err != nil {
				t.Fatal(err)
			}
			wait := resource.Waiters["fc_ready"]
			wait.Interval = "1ms"
			resource.Waiters["fc_ready"] = wait
			input := map[string]any{"openapi": true, "id": "tpl-1", "name": "native", "team_id": "team-1", "runtime_config": map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": auth}}}}
			result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: action, Input: input, Timeout: time.Second})
			if err != nil || len(fake.requests) != 4 || result.Item["build_status"] != "ready" || result.ID != "tpl-1" {
				t.Fatalf("redaction changed read retries for %s %v: requests=%d result=%+v err=%v", action, auth, len(fake.requests), result, err)
			}
		}
	}
}

func TestNativeTemplateReadAndMutationRetryabilityUsesOriginalError(t *testing.T) {
	for _, operation := range []string{"GetTemplate", "ListTemplates", "CreateTemplate", "UpdateTemplate"} {
		fake := &fakeOpenAPIExecutor{callError: errors.New("i/o timeout password=unknown suffix")}
		caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake, retryMaxAttempts: 1}
		caller.rememberTemplateSecrets(map[string]any{"authConfig": map[string]any{"userName": "i"}}, false)
		request := map[string]any{}
		if operation == "GetTemplate" || operation == "UpdateTemplate" {
			request["templateID"] = "tpl-1"
		}
		if operation == "CreateTemplate" {
			request["body.name"] = "native"
		}
		if operation == "CreateTemplate" || operation == "UpdateTemplate" {
			request["body.runtimeConfig"] = map[string]any{"internetAccess": false}
		}
		_, err := caller.CallRaw(context.Background(), operation, request)
		var app *ecerrors.AppError
		if !errors.As(err, &app) || app.Payload().Retryable != isReadOperation(operation) || len(fake.requests) != 1 {
			t.Fatalf("retryability was derived from display text for %s: %v requests=%d", operation, err, len(fake.requests))
		}
	}
}

func TestNativeTemplateEnvironmentRecoveryDelete(t *testing.T) {
	for _, token := range []string{"", "test-sts-token"} {
		t.Run(map[bool]string{true: "STS", false: "AK"}[token != ""], func(t *testing.T) {
			dir := t.TempDir()
			getenv := mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": filepath.Join(dir, "missing-aliyun.json"), "ALIBABA_CLOUD_ACCESS_KEY_ID": "test-ak", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "test-secret", "ALIBABA_CLOUD_SECURITY_TOKEN": token})
			config := filepath.Join(dir, "missing-ecctl.json")
			caller, err := NewOpenAPICaller("", config, "FCSandbox", "cn-hangzhou", getenv)
			if err != nil {
				t.Fatal(err)
			}
			caller.Resource = "template"
			caller.executor = &fakeOpenAPIExecutor{responses: []string{`{"code":"200","templateID":"tpl-1"}`, `{"code":"200","templateID":"tpl-1","teamID":"team-1","status":{"state":"failed","reason":{"message":"pull failed"}}}`}}
			resource, err := spec.LoadResource("", "sandbox", "template")
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"openapi": true, "name": "native", "team_id": "team-1"}})
			var app *ecerrors.AppError
			if !errors.As(caller.TemplateCreateRecovery(err), &app) || app.Payload().Code != "FCTemplateBuildFailed" {
				t.Fatalf("create did not reach accepted build failure: %v", err)
			}
			args := app.Payload().RecoveryCommand
			profile := argAfter(args, "--profile")
			deletion, err := NewOpenAPICaller(profile, config, "FCSandbox", argAfter(args, "--region"), getenv)
			if err != nil {
				t.Fatalf("recovery changed environment selection to %q: %v; argv=%q", profile, err, args)
			}
			if profile != "" || deletion.Profile.Mode != caller.Profile.Mode || deletion.Profile.CredentialPrincipal != caller.Profile.CredentialPrincipal {
				t.Fatalf("recovery changed credential binding: %q %+v", args, deletion.Profile)
			}
			deletion.Resource = "template"
			fake := &fakeOpenAPIExecutor{response: `{"code":"200","requestId":"req-delete"}`}
			deletion.executor = fake
			_, err = engine.NewExecutor(resource, deletion).Execute(context.Background(), engine.Request{Action: "delete", Input: map[string]any{"openapi": true, "id": args[4], "team_id": argAfter(args, "--team-id")}})
			if err != nil || len(fake.requests) != 1 || fake.requests[0].ApiName != "DeleteTemplate" || fake.requests[0].PathParams["templateID"] != "tpl-1" || fake.requests[0].QueryParams["teamID"] != "team-1" {
				t.Fatalf("recovery did not delete original allocation: %v %+v", err, fake.requests)
			}
		})
	}
}

func TestNativeTemplateRecoveryPinsOnlyConfiguredProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, profile, want string
		ignore              bool
	}{
		{"named", "prod asia", "prod asia", false},
		{"configured default", "default", "default", false},
		{"ignore configured profile", "prod asia", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "ecctl.json")
			data := `{"current":"` + tc.profile + `","profiles":[{"name":"` + tc.profile + `","mode":"AK","access_key_id":"profile-ak","access_key_secret":"profile-secret"}]}`
			if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": filepath.Join(dir, "missing-aliyun.json"), "ALIBABA_CLOUD_ACCESS_KEY_ID": "environment-ak", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "environment-secret"}
			if tc.ignore {
				env["ALIBABA_CLOUD_IGNORE_PROFILE"] = "true"
			}
			caller, err := NewOpenAPICaller(tc.profile, config, "FCSandbox", "cn-hangzhou", mapGetenv(env))
			if err != nil {
				t.Fatal(err)
			}
			caller.Resource = "template"
			caller.executor = &fakeOpenAPIExecutor{response: `{"code":"200","templateID":"tpl-1"}`}
			if _, err := caller.CallRaw(context.Background(), "CreateTemplate", map[string]any{"body.name": "native", "body.teamID": "team-1"}); err != nil {
				t.Fatal(err)
			}
			var app *ecerrors.AppError
			if !errors.As(caller.TemplateCreateRecovery(ecerrors.Service("CloudAPIError", "connection reset", false)), &app) {
				t.Fatal("accepted create lost recovery")
			}
			args := app.Payload().RecoveryCommand
			if argAfter(args, "--profile") != tc.want || args[4] != "tpl-1" || argAfter(args, "--region") != "cn-hangzhou" || argAfter(args, "--team-id") != "team-1" {
				t.Fatalf("recovery did not retain credential/target selection: %q want profile %q", args, tc.want)
			}
		})
	}
}

func TestNativeTemplateAuthCollisionsPreserveProtocol(t *testing.T) {
	resource, err := spec.LoadResource("", "sandbox", "template")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"200", "ready", "tpl-1", "team-1", "req"} {
		t.Run(value, func(t *testing.T) {
			fake := &fakeOpenAPIExecutor{responses: []string{`{"code":"200","requestId":"req-create","templateID":"tpl-1"}`, `{"code":"200","requestId":"req-get","templateID":"tpl-1","teamID":"team-1","status":{"state":"ready"}}`}}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
			result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Timeout: 500 * time.Millisecond, Input: map[string]any{"openapi": true, "name": "native", "team_id": "team-1", "runtime_config": map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"userName": value, "password": "private-password"}}}}}})
			if err != nil || result.ID != "tpl-1" || result.Item["build_status"] != "ready" || result.Item["team_id"] != "team-1" {
				t.Fatalf("protocol was rewritten for auth %q: result=%+v err=%v", value, result, err)
			}
			if len(result.Actions) != 2 || result.Actions[0].RequestID != "req-create" || result.Actions[1].RequestID != "req-get" {
				t.Fatalf("opaque request identity was rewritten: %+v", result.Actions)
			}
		})
	}
}

func TestNativeTemplateInvalidResponseLocalizationIsDistinct(t *testing.T) {
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: &fakeOpenAPIExecutor{response: `{"code":"200"}`}}
	_, err := caller.CallRaw(context.Background(), "CreateTemplate", map[string]any{"body.name": "native"})
	var app *ecerrors.AppError
	if !errors.As(err, &app) {
		t.Fatal(err)
	}
	for lang, want := range map[string]string{"en": "the native FC Template API returned an invalid response", "zh-CN": "原生 FC 模板 API 返回了无效响应"} {
		if got := i18n.NewLocalizer(lang).ErrorPayload(app.Payload(), false).Message; got != want {
			t.Fatalf("%s native error became E2B listing guidance: %q", lang, got)
		}
	}
}

func TestNativeTemplateAuthCollisionRetainsCreateRecovery(t *testing.T) {
	resource, err := spec.LoadResource("", "sandbox", "template")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenAPIExecutor{responses: []string{`{"code":"200","templateID":"tpl-1"}`, `{"code":"200","requestId":"req-get","templateID":"tpl-1","teamID":"team-1","status":{"state":"failed","reason":{"message":"pull failed"}}}`}}
	caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: fake}
	_, err = engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{Action: "create", Input: map[string]any{"openapi": true, "name": "native", "team_id": "team-1", "runtime_config": map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": "200"}}}}}})
	var app *ecerrors.AppError
	err = caller.TemplateCreateRecovery(err)
	if !errors.As(err, &app) || app.Payload().Code != "FCTemplateBuildFailed" || len(app.Payload().RecoveryCommand) < 5 || app.Payload().RecoveryCommand[4] != "tpl-1" {
		t.Fatalf("validated allocation lost: %v", err)
	}
}

func TestNativeTemplateSDKRedactionPreservesStructuredEvidence(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "SDKError", false: "ResponseError"}[disabled], func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			executor.client.DisableSDKError = &disabled
			executor.client.HttpClient = sdkErrorHTTPClient{status: 404, body: `{"code":"TemplateNotFound","message":"missing private-sdk-secret","requestId":"req-sdk"}`}
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", executor: executor}
			caller.rememberTemplateSecrets(map[string]any{"authConfig": map[string]any{"password": "private-sdk-secret", "userName": "req"}}, false)
			_, err = caller.CallRaw(context.Background(), "GetTemplate", map[string]any{"templateID": "tpl-1"})
			var app *ecerrors.AppError
			if !errors.As(err, &app) || app.Payload().Kind != "not_found" {
				t.Fatalf("structured absence lost: %v", err)
			}
			action := ecerrors.ActionFromError("GetTemplate", err)
			if action.Code != "TemplateNotFound" || action.RequestID != "req-sdk" || strings.Contains(action.Message, "private-sdk-secret") {
				t.Fatalf("structured evidence=%+v", action)
			}
		})
	}
}
