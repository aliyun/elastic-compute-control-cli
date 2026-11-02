package aliyun

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDarabonbaExecutorPreservesFormBodyAndBearerHeaders(t *testing.T) {
	for _, style := range []string{"RPC", "ROA"} {
		profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
		executor, err := newStaticDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
		if err != nil {
			t.Fatal(err)
		}
		transport := &recordingHTTPClient{}
		executor.client.HttpClient = transport
		executor.bearerTokenHeaderKey, executor.bearerToken = "x-test-token", "fake-token"
		request := newOpenAPIRequest()
		request.Product, request.ApiName, request.Version, request.Domain, request.Scheme, request.Method, request.Style = "Ecs", "TestAction", "2014-05-26", "example.invalid", "https", "POST", style
		request.QueryParams = nil
		request.FormParams = map[string]string{"Value": "form value"}
		if _, err := executor.ExecuteOpenAPI(nil, request); err != nil {
			t.Fatal(err)
		}
		if transport.request == nil || transport.request.Header.Get("x-test-token") != "fake-token" {
			t.Fatal("bearer header lost")
		}
		if !strings.Contains(requestHeaderValue(transport.request, "Content-Type"), "application/x-www-form-urlencoded") {
			t.Fatalf("form body encoded with wrong content type: %v", transport.request.Header)
		}
		if style == "RPC" && request.QueryParams["SignatureType"] != "BEARERTOKEN" {
			t.Fatal("RPC bearer signature type lost")
		}
		if style == "ROA" && requestHeaderValue(transport.request, "x-acs-signature-type") != "BEARERTOKEN" {
			t.Fatal("ROA bearer signature type lost")
		}
	}
}

func TestOpenAPIEndpointAndResponseFallbacks(t *testing.T) {
	if openAPIMethod("") != "GET" || openAPIProtocol("http") != "http" {
		t.Fatal("OpenAPI transport defaults changed")
	}
	for _, test := range []struct{ product, region, kind, want string }{{"Tag", "cn-hangzhou", "vpc", "tag-vpc.cn-hangzhou.aliyuncs.com"}, {"Tag", "cn-hangzhou", "", "tag.cn-hangzhou.aliyuncs.com"}, {"ResourceManager", "", "", "resourcemanager.aliyuncs.com"}, {"Ecs", "cn-hangzhou", "vpc", "ecs-vpc.cn-hangzhou.aliyuncs.com"}, {"Ecs", "", "", ""}} {
		var got string
		if test.product == "Tag" {
			got = normalizeProductEndpoint(test.product, test.region, test.kind, "")
		} else {
			got = inferRegionalEndpoint(test.product, test.region, test.kind)
		}
		if got != test.want {
			t.Fatalf("endpoint %s=%s", test.product, got)
		}
	}
	cause := errors.New("endpoint metadata unavailable")
	caller := &OpenAPICaller{endpointResolver: func(OpenAPIProduct, string, string) (string, error) { return "", cause }}
	if _, err := caller.productEndpoint(OpenAPIProduct{Code: "Ecs"}, "cn-hangzhou"); err == nil || !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("endpoint failure lost: %v", err)
	}
	for _, response := range []map[string]any{{"body": map[string]any{"id": "one"}}, {"body": []any{map[string]any{"id": "one"}}}, {"id": "one"}} {
		if got, err := normalizeDarabonbaResponse(response); err != nil || len(got) == 0 {
			t.Fatalf("structured response lost: %v,%v", got, err)
		}
	}
	for _, value := range []any{"", "plain", "{invalid"} {
		if got := coerceOpenAPIBodyValue(value, &OpenAPIParameter{Type: "Struct"}); !reflect.DeepEqual(got, value) {
			t.Fatalf("invalid JSON coerced: %#v", got)
		}
	}
	parameter := &OpenAPIParameter{Type: "Struct", SubParameters: []OpenAPIParameter{{Name: "known", Type: "String"}}}
	if got := openAPIBodyParameterAtPath(parameter, []string{"unknown"}); got != parameter {
		t.Fatal("unknown body field lost parent metadata")
	}
	if err := setOpenAPIParamValue(map[string]string{}, nil, "Value", "one"); err != nil {
		t.Fatal(err)
	}
	if callerStringMapValue(map[string]any{"count": 42}, "count") != "42" || callerCloudErrorMessage(nil) != "" {
		t.Fatal("error/scalar fallback changed")
	}
	if code, message, id := callerCloudErrorFields(nil); code != "" || message != "" || id != "" {
		t.Fatal("nil cloud error has fields")
	}
	for _, code := range []any{"", 0, 200, "ok", "success", "successful"} {
		if err := openAPIBusinessError(map[string]any{"Code": code}); err != nil {
			t.Fatalf("success code rejected: %v", err)
		}
	}
	if err := openAPIBusinessError(map[string]any{"Code": "Failed"}); err == nil || !strings.Contains(err.Error(), "unsuccessful business response") {
		t.Fatalf("missing failure message lost: %v", err)
	}
}

func TestCloudDiagnosticsRedactFragmentCredentialsAndRespectRetryBounds(t *testing.T) {
	for _, raw := range []string{"https://example.invalid/#access_token=fragment-secret", "https://example.invalid/#login?access_token=fragment-secret"} {
		if got := redactCredentialBearingURLs(raw); strings.Contains(got, "fragment-secret") {
			t.Fatalf("fragment credential leaked: %s", got)
		}
	}
	if got := redactDenyPrincipal("Deny: no closing delimiter"); got != "Deny: no closing delimiter" {
		t.Fatalf("unstructured denial changed: %s", got)
	}
	if got := throttleBackoff(0, time.Nanosecond, time.Second); got != time.Nanosecond {
		t.Fatalf("tiny retry interval=%s", got)
	}
	if got := throttleBackoff(0, 2*time.Second, time.Second); got < 500*time.Millisecond || got > time.Second {
		t.Fatalf("retry exceeded maximum: %s", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, duration := range []time.Duration{0, time.Hour} {
		if err := sleepWithContext(ctx, duration); !errors.Is(err, context.Canceled) {
			t.Fatalf("retry ignored cancellation: %v", err)
		}
	}
}
