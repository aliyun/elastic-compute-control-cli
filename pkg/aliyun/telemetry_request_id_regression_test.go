package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

type requestIDRegressionHTTPClient func(*http.Request, *http.Transport) (*http.Response, error)

func (f requestIDRegressionHTTPClient) Call(request *http.Request, transport *http.Transport) (*http.Response, error) {
	return f(request, transport)
}

func requestIDRegressionResponse(request *http.Request, header, requestID, body string) *http.Response {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if header != "" {
		headers.Set(header, requestID)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func requestIDRegressionSession(t *testing.T) (context.Context, *telemetry.Session, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
		Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	// Keep command-finalization identity resolution local even if a regression
	// prevents the caller from registering its own operation identity.
	session.RegisterIdentity("request-id-regression", func(context.Context) (telemetry.Identity, error) {
		return telemetry.Identity{Hash: "test-account-hash", Type: "Account", AccountID: "123456789"}, nil
	})
	return ctx, session, exporter
}

func requestIDRegressionAPISpans(t *testing.T, exporter *tracetest.InMemoryExporter, want int) []map[string]any {
	t.Helper()
	var spans []map[string]any
	for _, span := range exporter.GetSpans() {
		if span.Name == "ecctl.cloud.api.request" {
			spans = append(spans, testSpanAttributes(span.Attributes))
		}
	}
	if len(spans) != want {
		t.Fatalf("cloud API spans = %d, want %d", len(spans), want)
	}
	return spans
}

func requestIDRegressionAssertID(t *testing.T, span map[string]any, want string) {
	t.Helper()
	actual, present := span["ecctl.cloud.request_id"]
	if want == "" {
		if present {
			t.Errorf("cloud request ID = %#v, want omitted; span = %#v", actual, span)
		}
		return
	}
	if !present || actual != want {
		t.Errorf("cloud request ID = %#v (present=%t), want %q; span = %#v", actual, present, want, span)
	}
}

func TestOpenAPITelemetryTemplateCacheProviderReadyOutcome(t *testing.T) {
	restore := engine.SetTimeScaleForTest(0)
	defer restore()
	resource, err := spec.LoadResource("../../specs", "sandbox", "template-cache")
	if err != nil {
		t.Fatal(err)
	}
	ready := `{"templateID":"tpl-1","teamID":"team-1","status":"ready"}`
	for _, tc := range []struct {
		name, action string
		input        map[string]any
		responses    []string
		wantSpans    int
	}{
		{"get", "get", map[string]any{"id": "tpl-1", "team_id": "team-1"}, []string{`{"code":"200","templateCache":` + ready + `}`}, 1},
		{"list", "list", map[string]any{"team_id": "team-1", "status": "Success"}, []string{`{"code":"200","templateCaches":[` + ready + `]}`}, 1},
		{"create no wait", "create", map[string]any{"template_id": "tpl-1", "team_id": "team-1", "no_wait": true}, []string{`{"code":"200","templateCache":` + ready + `}`}, 1},
		{"create readiness poll", "create", map[string]any{"template_id": "tpl-1", "team_id": "team-1"}, []string{
			`{"code":"200","templateCache":{"templateID":"tpl-1","teamID":"team-1","status":"InProgress"}}`,
			`{"code":"200","templateCache":` + ready + `}`,
		}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}, executor: &fakeOpenAPIExecutor{responses: tc.responses}}
			ctx, session, exporter := requestIDRegressionSession(t)
			result, err := engine.NewExecutor(resource, caller).Execute(ctx, engine.Request{Action: tc.action, Input: tc.input})
			if err != nil {
				t.Fatalf("CLI-facing %s failed: %v", tc.action, err)
			}
			item := result.Item
			if tc.action == "list" {
				if len(result.Items) != 1 {
					t.Fatalf("listed items = %d, want 1", len(result.Items))
				}
				item = result.Items[0]
			}
			if item["status"] != "Success" {
				t.Fatalf("CLI-facing status = %#v, want Success", item["status"])
			}
			session.Finish("ecctl sandbox template-cache "+tc.action, 0)
			for attempt, span := range requestIDRegressionAPISpans(t, exporter, tc.wantSpans) {
				if span["ecctl.cloud.outcome"] != "success" {
					t.Errorf("attempt %d outcome = %#v, want success; span = %#v", attempt+1, span["ecctl.cloud.outcome"], span)
				}
			}
		})
	}
}

func TestOpenAPITelemetryTemplateCacheScrubsRequestIDs(t *testing.T) {
	for _, source := range []string{"header", "response"} {
		for _, tc := range []struct{ name, requestID, want string }{
			{"credential_assignment", "token=abc123", ""},
			{"access_key_shape", "LTAIabcdefghijklmnop", ""},
			{"compact_token_shape", "AbC_def-123", ""},
			{"opaque_id", "req-cache-safe", "req-cache-safe"},
			{"provider_uuid", "4FDE2B91-8A2D-49B0-8A49-1234567890AB", "4FDE2B91-8A2D-49B0-8A49-1234567890AB"},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
				executor, err := newStaticDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
				if err != nil {
					t.Fatal(err)
				}
				executor.client.HttpClient = requestIDRegressionHTTPClient(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
					body := map[string]any{"code": "200", "requestId": "req-body-safe", "templateCache": map[string]any{"templateID": "tpl-1", "teamID": "team-1", "status": "Success"}}
					header := ""
					if source == "header" {
						header = "X-Acs-Request-Id"
					} else {
						body["requestId"] = tc.requestID
					}
					encoded, err := json.Marshal(body)
					if err != nil {
						return nil, err
					}
					return requestIDRegressionResponse(request, header, tc.requestID, string(encoded)), nil
				})
				caller := &OpenAPICaller{Product: "FCSandbox", Resource: "template-cache", Region: "cn-hangzhou", Profile: profile, executor: executor, retryMaxAttempts: 1}
				ctx, session, exporter := requestIDRegressionSession(t)
				response, err := caller.Call(ctx, "DescribeTemplateCache", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
				if err != nil {
					t.Fatal(err)
				}
				if source == "response" && response["requestId"] != tc.want {
					t.Fatal("CLI response request ID did not use the conservative filter")
				}
				session.Finish("ecctl sandbox template-cache get", 0)
				spans := requestIDRegressionAPISpans(t, exporter, 1)
				requestIDRegressionAssertID(t, spans[0], tc.want)
			})
		}
	}
}

func TestOpenAPITelemetryFCInvokeRequestIDProvenance(t *testing.T) {
	const businessRequestID = "function-business-session-secret"
	for _, tc := range []struct {
		name, header, body, wantID string
	}{
		{name: "business body without header", body: `{"RequestId":"` + businessRequestID + `"}`},
		{name: "ACS header takes precedence over business body", header: "X-Acs-Request-Id", body: `{"RequestId":"` + businessRequestID + `"}`, wantID: "req-fc-acs-header"},
		{name: "FC header takes precedence over business body", header: "X-Fc-Request-Id", body: `{"RequestId":"` + businessRequestID + `"}`, wantID: "req-fc-header"},
		{name: "FC header without body request ID", header: "X-Fc-Request-Id", body: `{"result":"ok"}`, wantID: "req-fc-header-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newStaticDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			httpCalls := 0
			executor.client.HttpClient = requestIDRegressionHTTPClient(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
				httpCalls++
				return requestIDRegressionResponse(request, tc.header, tc.wantID, tc.body), nil
			})
			caller := &OpenAPICaller{
				Product: "fc", Region: "cn-hangzhou", Profile: profile, executor: executor, retryMaxAttempts: 1,
			}
			ctx, session, exporter := requestIDRegressionSession(t)
			response, err := caller.Call(ctx, "InvokeFunction", map[string]any{"functionName": "echo", "body": "function input"})
			if err != nil {
				t.Fatalf("InvokeFunction: %v", err)
			}
			if strings.Contains(tc.body, businessRequestID) && response["RequestId"] != businessRequestID {
				t.Fatalf("function business response changed: %#v", response)
			}
			session.Finish("ecctl call fc InvokeFunction", 0)
			if httpCalls != 1 {
				t.Fatalf("HTTP calls = %d, want 1", httpCalls)
			}
			spans := requestIDRegressionAPISpans(t, exporter, 1)
			requestIDRegressionAssertID(t, spans[0], tc.wantID)
		})
	}
}

func TestOpenAPITelemetryRenewalIdentityRequestIDIsolation(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		networkFailure         bool
		identityChange         bool
		identityServiceFailure bool
		wantID                 string
		wantHTTPCalls          int
	}{
		{name: "business header survives identity probe", wantID: "req-business-renewed", wantHTTPCalls: 2},
		{name: "business network failure has no response", networkFailure: true, wantHTTPCalls: 2},
		{name: "identity change prevents business request", identityChange: true, wantHTTPCalls: 1},
		{name: "identity service failure prevents business request", identityServiceFailure: true, wantHTTPCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalResolver := resolveCredentialSnapshotIdentity
			t.Cleanup(func() { resolveCredentialSnapshotIdentity = originalResolver })
			identityHTTPCalls := 0
			resolveCredentialSnapshotIdentity = func(ctx context.Context, profile resolvedOpenAPIProfile, snapshot *credentialSnapshot, domain string) (telemetry.Identity, error) {
				executor, err := newStaticDarabonbaExecutor(profile, snapshot)
				if err != nil {
					return telemetry.Identity{}, err
				}
				executor.client.HttpClient = requestIDRegressionHTTPClient(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
					identityHTTPCalls++
					if snapshot.AccessKeyID == "renewed-key" && tc.identityServiceFailure {
						response := requestIDRegressionResponse(request, "X-Acs-Request-Id", "req-inner-sts-error", `{"Code":"InvalidAccessKeyId.NotFound","Message":"renewal identity lookup failed","RequestId":"req-inner-sts-error"}`)
						response.StatusCode = http.StatusBadRequest
						return response, nil
					}
					accountID := "123456789"
					if snapshot.AccessKeyID == "renewed-key" && tc.identityChange {
						accountID = "987654321"
					}
					body := fmt.Sprintf(`{"IdentityType":"Account","AccountId":%q}`, accountID)
					return requestIDRegressionResponse(request, "X-Acs-Request-Id", fmt.Sprintf("req-internal-sts-%d", identityHTTPCalls), body), nil
				})
				return resolveCredentialIdentityWithExecutor(ctx, profile, executor, domain)
			}
			acquirer := &renewableCountingCredentialAcquirer{&countingCredentialAcquirer{snapshots: []credentialSnapshot{
				{AccessKeyID: "initial-key", AccessKeySecret: "test-secret", Type: "access_key"},
				{AccessKeyID: "renewed-key", AccessKeySecret: "test-secret", Type: "access_key"},
			}}}
			caller := &OpenAPICaller{
				Product: "ecs", Resource: "instance", Region: "cn-hangzhou", retryMaxAttempts: 1,
				Profile: resolvedOpenAPIProfile{
					Acquirer: acquirer, Mode: credentialModeExternal, AuthType: "AK", RegionID: "cn-hangzhou",
					CredentialPrincipal: "test-helper", PinCredentialIdentity: true,
				},
			}
			ctx, session, exporter := requestIDRegressionSession(t)
			ctx, err := caller.PrepareOperationContext(ctx)
			if err != nil {
				t.Fatalf("PrepareOperationContext: %v", err)
			}
			lease := ctx.Value(credentialLeaseContextKey{}).(*credentialLease)
			executor := lease.executor.(*darabonbaExecutor)
			businessHTTPCalls := 0
			executor.client.HttpClient = requestIDRegressionHTTPClient(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
				businessHTTPCalls++
				requestID := "req-business-first"
				if businessHTTPCalls > 1 {
					if tc.networkFailure {
						return nil, errors.New("dial tcp: network unreachable")
					}
					requestID = tc.wantID
				}
				return requestIDRegressionResponse(request, "X-Acs-Request-Id", requestID, `{"TotalCount":0,"Instances":{"Instance":[]}}`), nil
			})
			if _, err := caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-hangzhou"}); err != nil {
				t.Fatalf("initial business request: %v", err)
			}
			// The second real SDK call acquires fresh credentials and performs a
			// nested GetCallerIdentity before attempting the business HTTP request.
			_, err = caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-hangzhou"})
			wantError := tc.networkFailure || tc.identityChange || tc.identityServiceFailure
			if (err != nil) != wantError {
				t.Fatalf("renewed business error = %v, want error=%t", err, wantError)
			}
			if tc.identityChange {
				var app *ecerrors.AppError
				if !errors.As(err, &app) || app.Payload().Code != "CredentialIdentityChanged" {
					t.Fatalf("identity change error = %v, want CredentialIdentityChanged", err)
				}
			}
			exitCode := 0
			if wantError {
				exitCode = 1
			}
			session.Finish("ecctl ecs instance list", exitCode)
			if businessHTTPCalls != tc.wantHTTPCalls || identityHTTPCalls != 2 || acquirer.calls != 2 {
				t.Fatalf("business HTTP calls = %d (want %d), identity HTTP calls = %d (want 2), acquisitions = %d (want 2)", businessHTTPCalls, tc.wantHTTPCalls, identityHTTPCalls, acquirer.calls)
			}
			spans := requestIDRegressionAPISpans(t, exporter, 2)
			requestIDRegressionAssertID(t, spans[0], "req-business-first")
			requestIDRegressionAssertID(t, spans[1], tc.wantID)
			wantOutcome := "success"
			if wantError {
				wantOutcome = "error"
			}
			if spans[1]["ecctl.cloud.outcome"] != wantOutcome {
				t.Errorf("renewed business outcome = %#v, want %q", spans[1]["ecctl.cloud.outcome"], wantOutcome)
			}
		})
	}
}

func TestOpenAPITelemetryTemplateRegisteredSecretRequestIDs(t *testing.T) {
	const secret = "registered-registry-secret"
	for _, code := range []string{"200", "409"} {
		for _, source := range []string{"body", "header"} {
			for _, idCase := range []struct {
				name, registeredSecret, requestID, wantID string
			}{
				{name: "secret=false", registeredSecret: secret, requestID: "req-template-get", wantID: "req-template-get"},
				{name: "secret=true", registeredSecret: secret, requestID: secret},
				{name: "secret=whitespace", registeredSecret: " " + secret + " ", requestID: " " + secret + " "},
			} {
				name := fmt.Sprintf("code=%s/source=%s/%s", code, source, idCase.name)
				t.Run(name, func(t *testing.T) {
					requestID, wantID := idCase.requestID, idCase.wantID
					profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
					executor, err := newStaticDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
					if err != nil {
						t.Fatal(err)
					}
					httpCalls := 0
					executor.client.HttpClient = requestIDRegressionHTTPClient(func(request *http.Request, _ *http.Transport) (*http.Response, error) {
						httpCalls++
						if httpCalls == 1 {
							return requestIDRegressionResponse(request, "", "", `{"code":"200","requestId":"req-template-create","templateID":"tpl-1"}`), nil
						}
						body := map[string]any{
							"code": code, "message": "template result", "templateID": "tpl-1", "teamID": "team-1",
							"status": map[string]any{"state": "ready"},
						}
						header := ""
						if source == "body" {
							body["requestId"] = requestID
						} else {
							header = "X-Acs-Request-Id"
						}
						encoded, err := json.Marshal(body)
						if err != nil {
							return nil, err
						}
						return requestIDRegressionResponse(request, header, requestID, string(encoded)), nil
					})
					caller := &OpenAPICaller{
						Product: "FCSandbox", Resource: "template", Region: "cn-hangzhou", Profile: profile, executor: executor, retryMaxAttempts: 1,
					}
					ctx, session, exporter := requestIDRegressionSession(t)
					_, err = caller.Call(ctx, "CreateTemplate", map[string]any{
						"body.name": "native", "body.teamID": "team-1",
						"body.runtimeConfig": map[string]any{"sandboxConfig": map[string]any{
							"registryConfig": map[string]any{"authConfig": map[string]any{"password": idCase.registeredSecret}},
						}},
					})
					if err != nil {
						t.Fatalf("CreateTemplate: %v", err)
					}
					response, err := caller.Call(ctx, "GetTemplate", map[string]any{"templateID": "tpl-1", "teamID": "team-1"})
					if (err != nil) != (code != "200") {
						t.Fatalf("GetTemplate error = %v, want error=%t", err, code != "200")
					}
					if err != nil && strings.Contains(err.Error(), secret) {
						t.Fatalf("public error exposed registered secret: %v", err)
					}
					encoded, err := json.Marshal(response)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(encoded), secret) {
						t.Fatalf("public response exposed registered secret: %s", encoded)
					}
					session.Finish("ecctl sandbox template create", 0)
					if httpCalls != 2 {
						t.Fatalf("HTTP calls = %d, want 2", httpCalls)
					}
					spans := requestIDRegressionAPISpans(t, exporter, 2)
					requestIDRegressionAssertID(t, spans[0], "req-template-create")
					requestIDRegressionAssertID(t, spans[1], wantID)
				})
			}
		}
	}
}
