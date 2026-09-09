package e2bapi

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestNewCallerUsesStandardE2BEnvironment(t *testing.T) {
	for _, tt := range []struct {
		name, api, domain, want string
	}{
		{name: "default", want: "https://api.cn-hangzhou.e2b.fc.aliyuncs.com"},
		{name: "domain", domain: "example.test", want: "https://api.example.test"},
		{name: "api URL overrides domain", api: "https://api.e2b.app", domain: "example.test", want: "https://api.e2b.app"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{
				"E2B_API_KEY": "project-key",
				"E2B_API_URL": tt.api,
				"E2B_DOMAIN":  tt.domain,
			}
			caller, err := NewCaller(func(key string) string { return values[key] })
			if err != nil {
				t.Fatalf("NewCaller: %v", err)
			}
			if caller.endpoint.String() != tt.want {
				t.Fatalf("endpoint = %q, want %q", caller.endpoint.String(), tt.want)
			}
		})
	}
}

func TestNewCallerRequiresCredential(t *testing.T) {
	_, err := NewCaller(func(string) string { return "" })
	assertAppError(t, err, "MissingCredential")
}

func TestNewCallerWithRegion(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != fcSandboxEndpointsURL || r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected metadata request: %s", r.URL)
		}
		return testJSONResponse(http.StatusOK, fcRegionMetadata("cn-beijing", "cn-shanghai", "cn-hangzhou", "cn-shenzhen",
			"cn-hongkong", "ap-southeast-1", "us-east-1", "us-west-1", "cn-future-1")), nil
	})
	for _, region := range []string{
		"cn-beijing", "cn-shanghai", "cn-hangzhou", "cn-shenzhen",
		"cn-hongkong", "ap-southeast-1", "us-east-1", "us-west-1", "cn-future-1",
		"", "cn-qingdao", "eu-central-1", "../cn-beijing", "cn-beijing.attacker.example",
	} {
		t.Run(region, func(t *testing.T) {
			caller, err := NewCallerWithRegion(context.Background(), region, func(key string) string {
				if key == "E2B_API_KEY" {
					return "project-key"
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			wantRegion := region
			switch region {
			case "", "cn-qingdao", "eu-central-1", "../cn-beijing", "cn-beijing.attacker.example":
				wantRegion = "cn-hangzhou"
			}
			if got, want := caller.endpoint.String(), "https://api."+wantRegion+".e2b.fc.aliyuncs.com"; got != want {
				t.Fatalf("endpoint = %q, want %q", got, want)
			}
			caller.lookupCNAME = func(context.Context, string) (map[string]string, error) {
				t.Fatal("FC default must not need DNS discovery")
				return nil, nil
			}
			if !caller.detectBackend(context.Background()).fc {
				t.Fatal("default endpoint was not identified as FC")
			}
		})
	}
}

func TestNewCallerRejectsOversizedCredential(t *testing.T) {
	_, err := NewCallerWithClient("https://api.e2b.example", strings.Repeat("x", maxErrorBodyLen+1), nil)
	assertAppError(t, err, "InvalidCredential")
}

func TestNewCallerRejectsInsecureNonLoopbackEndpoint(t *testing.T) {
	_, err := NewCallerWithClient("http://api.e2b.example", "key", nil)
	assertAppError(t, err, "InvalidEndpoint")
	_, err = NewCallerWithClient("http://localhost:3000", "key", nil)
	assertAppError(t, err, "InvalidEndpoint")
	if _, err := NewCallerWithClient("http://127.0.0.1:3000", "key", nil); err != nil {
		t.Fatalf("literal loopback endpoint: %v", err)
	}
}

func TestCallerDoesNotForwardAPIKeyAcrossRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		redirected = true
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Errorf("redirect target received X-API-Key %q", got)
		}
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "secret-key" {
			t.Fatalf("source X-API-Key = %q", got)
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	caller, err := NewCallerWithClient(source.URL, "secret-key", source.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.Call(context.Background(), "ListSandboxes", map[string]any{})
	assertAppError(t, err, "E2BHTTP302")
	if redirected {
		t.Fatal("caller followed an E2B redirect")
	}
}

func TestListSandboxesSendsAPIKeyAndMapsArrayHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/sandboxes" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "secret-key" {
			t.Fatalf("X-API-Key = %q", r.Header.Get("X-API-Key"))
		}
		if got := r.URL.Query().Get("state"); got != "running,paused" {
			t.Fatalf("state = %q", got)
		}
		metadata, err := url.ParseQuery(r.URL.Query().Get("metadata"))
		if err != nil || metadata.Get("owner") != "a b" {
			t.Fatalf("metadata = %q, err = %v", r.URL.Query().Get("metadata"), err)
		}
		w.Header().Set("X-Next-Token", "next-page")
		w.Header().Set("X-Total-Running", "7")
		w.Header().Set("X-Request-ID", "request-1")
		_, _ = w.Write([]byte(`[{"sandboxID":"sbx-1","state":"running"}]`))
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, "secret-key", server.Client())
	if err != nil {
		t.Fatalf("NewCallerWithClient: %v", err)
	}
	result, err := caller.Call(context.Background(), "ListSandboxes", map[string]any{
		"query.state.1":  "running",
		"query.state.2":  "paused",
		"query.metadata": []string{"owner=a b"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result[arrayMarker] != true || result["nextToken"] != "next-page" || result["requestID"] != "request-1" {
		t.Fatalf("result = %#v", result)
	}
	if result["total"] != 7 {
		t.Fatalf("total = %#v", result["total"])
	}
}

func TestCreateTemplateAndStartBuildUseSeparateOperations(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		switch requests {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/v3/templates" {
				t.Fatalf("first request = %s %s", r.Method, r.URL.Path)
			}
			if body["name"] != "python" || body["cpuCount"] != float64(2) {
				t.Fatalf("create body = %#v", body)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"templateID":"tpl-1","buildID":"build-1","public":false,"aliases":[],"names":["python"],"tags":[]}`))
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/v2/templates/tpl-1/builds/build-1" {
				t.Fatalf("second request = %s %s", r.Method, r.URL.Path)
			}
			if body["fromImage"] != "python:3.12" {
				t.Fatalf("build body = %#v", body)
			}
			steps, ok := body["steps"].([]any)
			if !ok || len(steps) != 1 {
				t.Fatalf("steps = %#v", body["steps"])
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, "secret-key", server.Client())
	if err != nil {
		t.Fatalf("NewCallerWithClient: %v", err)
	}
	result, err := caller.Call(context.Background(), "CreateTemplate", map[string]any{
		"body.name":     "python",
		"body.cpuCount": 2,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if requests != 1 || result["templateID"] != "tpl-1" || result["buildID"] != "build-1" {
		t.Fatalf("requests = %d, result = %#v", requests, result)
	}
	_, err = caller.Call(context.Background(), "StartTemplateBuild", map[string]any{
		"path.templateID":   "tpl-1",
		"path.buildID":      "build-1",
		"body.fromImage":    "python:3.12",
		"body.steps.1.type": "RUN",
		"body.steps.1.args": []any{"python", "--version"},
	})
	if err != nil {
		t.Fatalf("StartTemplateBuild: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestStartTemplateBuildFailureReturnsAllocatedIDsAndCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "req-build")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"build service unavailable"}`))
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, "secret-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.Call(context.Background(), "StartTemplateBuild", map[string]any{
		"path.templateID": "tpl-partial",
		"path.buildID":    "build-partial",
		"body.fromImage":  "python:3.12",
	})
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("error = %T %v, want AppError", err, err)
	}
	payload := appErr.Payload()
	if !strings.Contains(payload.Detail, "tpl-partial") || !strings.Contains(payload.Detail, "build-partial") {
		t.Fatalf("detail = %q", payload.Detail)
	}
	wantRecovery := []string{"ecctl", "sandbox", "template", "delete", "tpl-partial"}
	if strings.Join(payload.RecoveryCommand, "\x00") != strings.Join(wantRecovery, "\x00") {
		t.Fatalf("recovery = %#v, want %#v", payload.RecoveryCommand, wantRecovery)
	}
}

func TestForkSandboxFailsWhenAnItemReportsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "req-fork-secret-key")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[{"sandbox":{"sandboxID":"sbx-created"}},{"error":{"code":503,"error_code":"sandbox_capacity_unavailable-secret-key","message":"no capacity for secret-key"}}]`))
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, "secret-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.Call(context.Background(), "ForkSandbox", map[string]any{"path.sandboxID": "sbx-source"})
	assertAppError(t, err, "E2BForkPartialFailure")
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatal("fork error is not AppError")
	}
	payload := appErr.Payload()
	if !strings.Contains(payload.Detail, "sandbox_capacity_unavailable") || !strings.Contains(payload.Detail, "sbx-created") {
		t.Fatalf("detail = %q", payload.Detail)
	}
	if strings.Contains(payload.Detail, "secret-key") || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("fork error leaked API key: %v, detail=%q", err, payload.Detail)
	}
	wantRecovery := []string{"ecctl", "sandbox", "delete", "sbx-created"}
	if strings.Join(payload.RecoveryCommand, "\x00") != strings.Join(wantRecovery, "\x00") {
		t.Fatalf("recovery = %#v, want %#v", payload.RecoveryCommand, wantRecovery)
	}
	if actions := appErr.Actions(); len(actions) != 2 || actions[0].Code != "created" || actions[1].Code != "sandbox_capacity_unavailable-[REDACTED]" {
		t.Fatalf("actions = %#v", actions)
	} else if strings.Contains(actions[0].RequestID, "secret-key") || strings.Contains(actions[1].RequestID, "secret-key") {
		t.Fatalf("actions leaked API key: %#v", actions)
	}
}

func TestCallerRedactsAPIKeyFromSuccessfulResponseAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "request-secret-key")
		_, _ = w.Write([]byte(`{"sandboxID":"sandbox-secret-key","nested":{"value":"secret-key","key-secret-key":"value"}}`))
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, "secret-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := caller.Call(context.Background(), "CreateSandbox", map[string]any{"body.templateID": "base"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-key") {
		t.Fatalf("successful response leaked API key: %s", encoded)
	}
}

func TestCallerUsesDefaultDeadlineOnlyWhenContextHasNone(t *testing.T) {
	var deadlines []time.Time
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok {
			t.Fatal("request context has no deadline")
		}
		deadlines = append(deadlines, deadline)
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}
	caller, err := NewCallerWithClient("https://api.e2b.example", "key", client)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := caller.Call(context.Background(), "ListSandboxes", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if remaining := deadlines[0].Sub(started); remaining < 29*time.Second || remaining > 31*time.Second {
		t.Fatalf("default deadline = %s, want about 30s", remaining)
	}

	longDeadline := time.Now().Add(10 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), longDeadline)
	defer cancel()
	if _, err := caller.Call(ctx, "ListSandboxes", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if delta := deadlines[1].Sub(longDeadline); delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("caller replaced operation deadline by %s", delta)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestTemplatePathKeepsNamespacedIDInOneEscapedSegment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.EscapedPath(), "team%2Fpython") {
			t.Fatalf("escaped path = %q", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"templateID":"team/python","public":false,"aliases":[],"names":["team/python"],"createdAt":"now","updatedAt":"now","lastSpawnedAt":null,"spawnCount":0,"builds":[]}`))
	}))
	defer server.Close()
	caller, err := NewCallerWithClient(server.URL, "key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := caller.Call(context.Background(), "GetTemplate", map[string]any{"path.templateID": "team/python"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestHTTPErrorIsStructuredAndDoesNotExposeAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "request-denied")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid project key super-secret"}`))
	}))
	defer server.Close()
	caller, err := NewCallerWithClient(server.URL, "super-secret", server.Client())
	if err != nil {
		t.Fatalf("NewCallerWithClient: %v", err)
	}
	_, err = caller.Call(context.Background(), "GetSandbox", map[string]any{"path.sandboxID": "sbx-1"})
	assertAppError(t, err, "E2BHTTP401")
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestHTTPErrorRedactsAPIKeyAcrossBodyLimitBoundary(t *testing.T) {
	const apiKey = "boundary-secret"
	prefix := strings.Repeat("x", maxErrorBodyLen-len(apiKey)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(prefix + apiKey + "ignored-tail"))
	}))
	defer server.Close()

	caller, err := NewCallerWithClient(server.URL, apiKey, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = caller.Call(context.Background(), "GetSandbox", map[string]any{"path.sandboxID": "sbx-1"})
	assertAppError(t, err, "E2BHTTP500")
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatal("HTTP error is not AppError")
	}
	if strings.Contains(err.Error(), apiKey[:len(apiKey)-1]) || strings.Contains(appErr.Payload().Detail, apiKey[:len(apiKey)-1]) {
		t.Fatal("error exposed an API key prefix across the body limit boundary")
	}
}

func TestCallerCoversEveryE2BSpecOperation(t *testing.T) {
	for _, resourceName := range []string{"sandbox", "template"} {
		resource, err := spec.LoadResource("", "sandbox", resourceName)
		if err != nil {
			t.Fatalf("LoadResource(%s): %v", resourceName, err)
		}
		if resource.Provider != "e2b" || resource.Kind != "global" {
			t.Fatalf("%s provider=%q kind=%q", resourceName, resource.Provider, resource.Kind)
		}
		apis := map[string]bool{}
		for _, probe := range resource.Probes {
			apis[probe.API] = true
		}
		for _, binding := range resource.Bindings {
			apis[binding.API] = true
		}
		for api := range apis {
			if _, ok := operations[api]; !ok {
				t.Fatalf("%s spec API %q has no E2B caller operation", resourceName, api)
			}
		}
	}
}

func assertAppError(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *ecerrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("error %v is not AppError", err)
	}
	if appErr.Payload().Code != code {
		t.Fatalf("code = %q, want %q", appErr.Payload().Code, code)
	}
}
