package aliyun

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

type telemetryHTTPResponse struct {
	status     int
	requestID  string
	header     string
	body       string
	expectedID string
}

type telemetryResponseHTTPClient struct {
	responses []telemetryHTTPResponse
	calls     int
}

func (c *telemetryResponseHTTPClient) Call(request *http.Request, _ *http.Transport) (*http.Response, error) {
	item := c.responses[c.calls]
	c.calls++
	body := `{"TotalCount":0,"Instances":{"Instance":[]}}`
	if item.status >= 400 {
		body = `{"Code":"InvalidParameter","Message":"private-response-body"}`
		if item.status == http.StatusTooManyRequests {
			body = `{"Code":"Throttling.User","Message":"too many requests"}`
		}
	}
	if item.body != "" {
		body = item.body
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	if item.header != "" {
		header.Set(item.header, item.requestID)
	}
	return &http.Response{
		StatusCode: item.status, Header: header,
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}, nil
}

func TestOpenAPITelemetryCapturesRequestIDsFromSDKResponses(t *testing.T) {
	type testCase struct {
		name      string
		responses []telemetryHTTPResponse
		wantError bool
	}
	tests := []testCase{
		{name: "success", responses: []telemetryHTTPResponse{{status: 200, requestID: "req-header-success", header: "X-Acs-Request-Id", expectedID: "req-header-success"}}},
		{name: "OSS header alias", responses: []telemetryHTTPResponse{{status: 200, requestID: "req-oss-header", header: "X-Oss-Request-Id", expectedID: "req-oss-header"}}},
		{name: "failure", responses: []telemetryHTTPResponse{{status: 400, requestID: "req-header-error", header: "X-Acs-Request-Id", expectedID: "req-header-error"}}, wantError: true},
		{name: "retries without stale ID", responses: []telemetryHTTPResponse{
			{status: 429, requestID: "req-header-first", header: "X-Acs-Request-Id", expectedID: "req-header-first"},
			{status: 429, requestID: "req-header-second", header: "X-Acs-Request-Id", expectedID: "req-header-second"},
			{status: 200, header: "X-Acs-Request-Id"},
		}},
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		for _, alias := range []string{"RequestId", "requestId", "RequestID", "requestID", "request_id", "requestid"} {
			requestID := fmt.Sprintf("req-body-%d-%s", status, alias)
			tests = append(tests, testCase{
				name: fmt.Sprintf("failure body aliases/%d/%s", status, alias),
				responses: []telemetryHTTPResponse{{
					status: status, expectedID: requestID,
					body: fmt.Sprintf(`{"Code":"InvalidParameter","Message":"private-response-body",%q:%q}`, alias, requestID),
				}},
				wantError: true,
			})
		}
		for _, withHeader := range []bool{false, true} {
			response := telemetryHTTPResponse{
				status: status,
				body:   `{"Code":"InvalidParameter","Message":"bad\nRequestId: user-secret\ntrailer"}`,
			}
			if withHeader {
				response.header, response.requestID, response.expectedID = "X-Acs-Request-Id", "req-trusted-header", "req-trusted-header"
			}
			tests = append(tests, testCase{
				name:      fmt.Sprintf("message RequestId is untrusted/%d/header=%t", status, withHeader),
				responses: []telemetryHTTPResponse{response},
				wantError: true,
			})
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
			executor, err := newStaticDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
			if err != nil {
				t.Fatal(err)
			}
			receiver := &telemetryResponseHTTPClient{responses: tc.responses}
			executor.client.HttpClient = receiver
			caller := &OpenAPICaller{
				Product: "ecs", Resource: "instance", Region: "cn-hangzhou", Profile: profile,
				executor: executor, retryMaxAttempts: len(tc.responses),
				sleepFn: func(context.Context, time.Duration) error { return nil },
			}
			exporter := tracetest.NewInMemoryExporter()
			ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
				Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
			})
			session.RegisterIdentity("test-credential", func(context.Context) (telemetry.Identity, error) {
				return telemetry.Identity{Hash: "user-hash", Type: "RAMUser", AccountID: "123456789", UserID: "user-123"}, nil
			})
			response, err := caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-hangzhou"})
			if (err != nil) != tc.wantError {
				t.Fatalf("Call error = %v, want error=%t", err, tc.wantError)
			}
			if _, exists := response["RequestId"]; exists {
				t.Fatal("telemetry header capture modified the returned API body")
			}
			session.Finish("ecctl ecs instance list", 0)
			if receiver.calls != len(tc.responses) {
				t.Fatalf("HTTP calls = %d, want %d", receiver.calls, len(tc.responses))
			}
			apiSpans := 0
			var traceID string
			for _, span := range exporter.GetSpans() {
				attrs := testSpanAttributes(span.Attributes)
				if span.Name == "ecctl.command" {
					if attrs["ecctl.identity.account_id"] != "123456789" || attrs["ecctl.identity.user_id"] != "user-123" {
						t.Fatalf("command identity = %#v", attrs)
					}
					if traceID != span.SpanContext.TraceID().String() {
						t.Fatal("API attempts and account identity are not in the same trace")
					}
					continue
				}
				if span.Name != "ecctl.cloud.api.request" {
					continue
				}
				wantID := tc.responses[apiSpans].expectedID
				gotID, present := attrs["ecctl.cloud.request_id"]
				if (wantID == "" && present) || (wantID != "" && gotID != wantID) {
					t.Fatalf("attempt %d request ID = %#v (present=%t), want %q", apiSpans+1, gotID, present, wantID)
				}
				if attrs["ecctl.cloud.attempt"] != int64(apiSpans+1) {
					t.Fatalf("attempt metadata = %#v", attrs)
				}
				traceID = span.SpanContext.TraceID().String()
				apiSpans++
			}
			if apiSpans != len(tc.responses) {
				t.Fatalf("API spans = %d, want %d", apiSpans, len(tc.responses))
			}
		})
	}
}
