package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/aliyun/elastic-compute-control-cli/internal/telemetryconfig"
)

func TestHTTPExporterFlushesOTLPProtobufToConfiguredHTTPSURL(t *testing.T) {
	received := make(chan *collectortrace.ExportTraceServiceRequest, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/custom/v1/traces" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("x-ecctl-token") != "token-value" {
			t.Errorf("token header = %q", request.Header.Get("x-ecctl-token"))
		}
		if request.URL.Query().Get("token") != "query-secret" {
			t.Errorf("token query = %q", request.URL.RawQuery)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		payload := &collectortrace.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(body, payload); err != nil {
			t.Errorf("unmarshal OTLP payload: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		response, _ := proto.Marshal(&collectortrace.ExportTraceServiceResponse{})
		writer.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = writer.Write(response)
	}))
	defer server.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://attacker.invalid")
	config := telemetryconfig.Config{Endpoint: server.URL + "/custom/v1/traces?token=query-secret", Headers: map[string]string{"x-ecctl-token": "token-value"}}
	exporter, err := newHTTPExporter(context.Background(), config, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, session := Start(WithExporterForTest(context.Background(), exporter), Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	if FromContext(ctx) != session {
		t.Fatal("session missing from context")
	}
	session.RegisterIdentity("test-credential", func(context.Context) (Identity, error) {
		return Identity{Hash: "principal-hash", Type: "RAMUser", AccountID: "123456789", UserID: "user-123"}, nil
	})
	for attempt := 1; attempt <= 3; attempt++ {
		finish := session.StartAPI(APIRequest{
			Service: "ecs", API: "DescribeInstances", OperationID: "operation-1",
			Attempt: attempt, RetryObservable: true,
		})
		var requestErr error
		if attempt < 3 {
			requestErr = errors.New("private-response-error")
		}
		finish(APIResult{Err: requestErr, RequestID: "request-" + strconv.Itoa(attempt)})
	}
	session.Finish("ecctl ecs instance list", 0)

	select {
	case payload := <-received:
		if len(payload.ResourceSpans) != 1 || len(payload.ResourceSpans[0].ScopeSpans) != 1 || len(payload.ResourceSpans[0].ScopeSpans[0].Spans) != 4 {
			t.Fatalf("OTLP payload span shape = %#v", payload)
		}
		var root *tracepb.Span
		var requests []*tracepb.Span
		for _, span := range payload.ResourceSpans[0].ScopeSpans[0].Spans {
			switch span.Name {
			case "ecctl.command":
				root = span
			case "ecctl.cloud.api.request":
				requests = append(requests, span)
			}
		}
		if root == nil || len(requests) != 3 {
			t.Fatalf("OTLP command = %v, requests = %d", root != nil, len(requests))
		}
		attributes := map[string]string{}
		for _, item := range root.Attributes {
			attributes[item.Key] = item.Value.GetStringValue()
		}
		for key, want := range map[string]string{
			"ecctl.identity.hash": "principal-hash", "ecctl.identity.type": "RAMUser",
			"ecctl.identity.account_id": "123456789", "ecctl.identity.user_id": "user-123",
		} {
			if got := attributes[key]; got != want {
				t.Fatalf("OTLP %s = %q, want %q", key, got, want)
			}
		}
		for _, request := range requests {
			if !bytes.Equal(request.TraceId, root.TraceId) || !bytes.Equal(request.ParentSpanId, root.SpanId) {
				t.Fatal("OTLP request ID and account identity are not correlated")
			}
			requestAttributes := map[string]string{}
			var attempt int64
			for _, item := range request.Attributes {
				requestAttributes[item.Key] = item.Value.GetStringValue()
				if item.Key == "ecctl.cloud.attempt" {
					attempt = item.Value.GetIntValue()
				}
			}
			if attempt < 1 || attempt > 3 || requestAttributes["ecctl.cloud.request_id"] != "request-"+strconv.FormatInt(attempt, 10) || requestAttributes["ecctl.cloud.operation_id"] != "operation-1" {
				t.Fatalf("OTLP attempt %d attributes = %#v", attempt, requestAttributes)
			}
			wantOutcome := "error"
			if attempt == 3 {
				wantOutcome = "success"
			}
			if requestAttributes["ecctl.cloud.outcome"] != wantOutcome {
				t.Fatalf("OTLP attempt %d outcome = %q", attempt, requestAttributes["ecctl.cloud.outcome"])
			}
			if len(request.Events) != 0 || request.GetStatus().GetMessage() != "" {
				t.Fatal("OTLP request included private error details")
			}
		}
	default:
		t.Fatal("HTTPS receiver did not receive an OTLP request before Finish returned")
	}
}

func TestHTTPExporterDoesNotFollowRedirects(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, downgrade := range []bool{false, true} {
			name := strconv.Itoa(status) + "-cross-origin"
			if downgrade {
				name = strconv.Itoa(status) + "-https-to-http"
			}
			t.Run(name, func(t *testing.T) {
				var secondRequests atomic.Int32
				secondHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					secondRequests.Add(1)
				})
				var second *httptest.Server
				if downgrade {
					second = httptest.NewServer(secondHandler)
				} else {
					second = httptest.NewTLSServer(secondHandler)
				}
				defer second.Close()

				var firstRequests atomic.Int32
				first := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					firstRequests.Add(1)
					if request.URL.Query().Get("token") != "query-secret" || request.Header.Get("x-token") != "header-secret" {
						t.Errorf("first request lost authentication: query=%q header=%q", request.URL.RawQuery, request.Header.Get("x-token"))
					}
					writer.Header().Set("Location", second.URL+"/stolen")
					writer.WriteHeader(status)
				}))
				defer first.Close()

				config := telemetryconfig.Config{
					Endpoint: first.URL + "/v1/traces?token=query-secret",
					Headers:  map[string]string{"x-token": "header-secret"},
				}
				exporter, err := newHTTPExporter(context.Background(), config, first.Client())
				if err != nil {
					t.Fatal(err)
				}
				_, session := Start(WithExporterForTest(context.Background(), exporter), Options{
					Enabled: true, Surface: "public", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
				})
				session.Finish("ecctl --version", 0)
				if firstRequests.Load() != 1 {
					t.Fatalf("first requests = %d, want 1", firstRequests.Load())
				}
				if secondRequests.Load() != 0 {
					t.Fatalf("redirect target requests = %d, want 0", secondRequests.Load())
				}
			})
		}
	}
}
