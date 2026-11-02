package e2bapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestE2BEndpointAndRequestValidationIsOffline(t *testing.T) {
	if _, err := NewCaller(nil); err == nil {
		t.Fatal("missing credentials accepted")
	}
	for _, endpoint := range []string{"relative", "://", "https://user:pass@example.com", "https://example.com?token=secret", "https://example.com#fragment"} {
		if _, err := NewCallerWithClient(endpoint, "key", nil); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	if _, err := NewCallerWithClient("https://example.com", "", nil); err == nil {
		t.Fatal("empty API key accepted")
	}
	for _, env := range []map[string]string{{"E2B_API_KEY": "key", "E2B_DOMAIN": "https://example.com"}, {"E2B_API_KEY": "key", "E2B_API_URL": "https://api.e2b.app", "ECCTL_SANDBOX_CA_FILE": "/nonexistent/ecctl-test-ca.pem"}, {"E2B_API_KEY": "key", "E2B_API_URL": "invalid"}} {
		if _, err := NewCaller(func(key string) string { return env[key] }); err == nil {
			t.Fatalf("invalid endpoint configuration accepted: %#v", env)
		}
	}
	caller, err := NewCallerWithClient("http://127.0.0.1:1", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	caller.backend = backendE2B
	if _, err := caller.Call(context.Background(), "Unknown", nil); err == nil {
		t.Fatal("unknown operation accepted")
	}
	if _, err := caller.Call(context.Background(), "GetSandbox", nil); err == nil {
		t.Fatal("missing path ID accepted")
	}
	if _, err := caller.Call(context.Background(), "CreateSandbox", map[string]any{"body": map[string]any{"invalid": make(chan int)}}); err == nil {
		t.Fatal("unserializable request accepted")
	}
	if _, err := expandPath("/sandboxes/{unclosed", nil); err == nil {
		t.Fatal("invalid path template accepted")
	}
}

func TestE2BQueryMetadataPreservesSupportedForms(t *testing.T) {
	for _, value := range []any{map[string]any{"count": json.Number("2")}, []any{"count=2"}, []string{"count=2"}} {
		if got, ok := metadataQuery(value); !ok || got.Get("count") != "2" {
			t.Fatalf("metadata query=%v, %t", got, ok)
		}
	}
	for _, value := range []any{true, []any{"bad"}, []string{"=value"}} {
		if _, ok := metadataQuery(value); ok {
			t.Fatalf("invalid metadata query accepted: %#v", value)
		}
	}
	if got := encodeQuery(map[string]any{"skip": nil, "values": []string{"a", "b"}}); got.Get("values") != "a,b" || got.Has("skip") {
		t.Fatalf("query=%v", got)
	}
	if got := stringValue(json.Number("42")); got != "42" {
		t.Fatalf("numeric value=%s", got)
	}
	if got := stringValue(url.Values{"a": {"b"}}); !strings.Contains(got, "a") {
		t.Fatalf("stringer value=%s", got)
	}
}

func TestE2BResponseShapesAndForkFailures(t *testing.T) {
	for _, payload := range []string{"invalid JSON", "42"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, payload) }))
		caller, err := NewCallerWithClient(server.URL, "key", server.Client())
		if err != nil {
			t.Fatal(err)
		}
		caller.backend = backendE2B
		response, err := caller.call(context.Background(), "test", operation{method: http.MethodGet, path: "/test"}, nil)
		server.Close()
		if payload == "42" {
			if err != nil || response["value"] != json.Number("42") {
				t.Fatalf("scalar response=%#v, %v", response, err)
			}
		} else if err == nil {
			t.Fatal("malformed response accepted")
		}
	}
	for _, result := range []map[string]any{{}, {"items": true}, {"items": []any{nil, map[string]any{"sandbox": map[string]any{"sandboxID": "one"}}}}} {
		if err := forkResultError(result, "key"); err != nil {
			t.Fatalf("successful or absent fork result=%v", err)
		}
	}
	if err := forkResultError(map[string]any{"items": []any{map[string]any{"error": map[string]any{}}}}, ""); err == nil {
		t.Fatal("empty fork error discarded")
	}
	if got := firstValue(nil, "missing"); got != nil {
		t.Fatalf("missing value=%v", got)
	}
	if got := decodeJSONValue("{invalid JSON"); !reflect.DeepEqual(got, "{invalid JSON") {
		t.Fatalf("raw value=%#v", got)
	}
	if got := decodeJSONValue(`{"count":2}`); !reflect.DeepEqual(got, map[string]any{"count": json.Number("2")}) {
		t.Fatalf("decoded numeric value=%#v", got)
	}
}
