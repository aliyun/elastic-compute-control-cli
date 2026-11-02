package e2bapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPErrorClassificationKeepsEmptyBodyStatusAndTimeout(t *testing.T) {
	c := &Caller{}
	for _, tc := range []struct {
		status int
		kind   string
	}{{400, "client"}, {409, "client"}, {422, "client"}, {408, "timeout"}, {504, "timeout"}} {
		response := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(""))}
		err := c.responseError("GetSandbox", response)
		payload := errorPayload(t, err)
		if payload.Kind != tc.kind || payload.Message != http.StatusText(tc.status) {
			t.Fatalf("status %d classification=%+v", tc.status, payload)
		}
	}
	if got := redactString("unchanged", ""); got != "unchanged" {
		t.Fatal("empty secret changed text")
	}
}

func TestDNSConfigurationAndBackendDetectionRejectMalformedRoutes(t *testing.T) {
	for _, port := range []string{"bad", "0", "65536"} {
		if _, err := dnsServer("127.0.0.1", port); err == nil {
			t.Fatal("invalid DNS port accepted")
		}
	}
	for _, raw := range []string{"resolver #1\norder : bad\nnameserver[0] : 127.0.0.1", "resolver #1\nignored line\nnameserver[0] : bad"} {
		if _, err := scutilServers(raw, "example.invalid"); err == nil {
			t.Fatal("malformed system resolver accepted")
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 254), "-invalid.example", strings.Repeat("a", 64) + ".example"} {
		if validDNSName(name) {
			t.Fatal("malformed DNS name accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := identifyBackend(ctx, "fixture.example", func(context.Context, string) (map[string]string, error) { cancel(); return nil, context.Canceled })
	if !errors.Is(err, context.Canceled) || result.reason != "dns_timeout" {
		t.Fatalf("DNS cancellation classification=%+v,%v", result, err)
	}
	_, err = identifyBackend(context.Background(), "fixture.example", func(context.Context, string) (map[string]string, error) {
		return map[string]string{"fixture.example": "one.example", "FIXTURE.EXAMPLE.": "two.example"}, nil
	})
	if err == nil {
		t.Fatal("conflicting CNAME answer accepted")
	}
}

func TestBuildStatusRejectsInvalidStatusAndLogShapes(t *testing.T) {
	for _, raw := range []string{
		`[]`,
		`{"templateID":"t","buildID":"b","status":"unknown","logs":[],"logEntries":[]}`,
		`{"templateID":"t","buildID":"b","status":"ready","logs":[1],"logEntries":[]}`,
		`{"templateID":"t","buildID":"b","status":"ready","logs":["ok"],"logEntries":[false]}`,
		`{"templateID":"t","buildID":"b","status":"ready","logs":["ok"]}`,
	} {
		if _, err := decodeContractResponse(strings.NewReader(raw), "GetTemplateBuildStatus", nil); err == nil {
			t.Fatalf("invalid status response accepted: %s", raw)
		}
	}
	if validObjectArray([]any{false}, "message") {
		t.Fatal("non-object log entry accepted")
	}
}

func TestACSRejectsUnavailableSandboxCapabilitiesBeforeRequests(t *testing.T) {
	c := &Caller{backend: backendACS}
	for _, action := range []string{"refresh", "fork", "logs", "metrics"} {
		if err := c.ValidateOperation(context.Background(), "sandbox", action, nil); err == nil {
			t.Fatalf("unsupported action %s accepted", action)
		}
	}
	for _, field := range []string{"network", "volumeMounts"} {
		if err := c.validateAPIOperation("CreateSandbox", map[string]any{"body": map[string]any{field: map[string]any{}}}); err == nil {
			t.Fatalf("unsupported create field %s accepted", field)
		}
	}
	if err := c.validateAPIOperation("CreateSandbox", map[string]any{"body": map[string]any{"network": nil}}); err != nil {
		t.Fatalf("nil optional field rejected: %v", err)
	}
}
