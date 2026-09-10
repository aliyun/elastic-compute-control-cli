package e2bapi

import (
	"context"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configuredCaller(t *testing.T, endpoint, backend, ca string) *Caller {
	t.Helper()
	env := map[string]string{"E2B_API_KEY": "test-key", "E2B_API_URL": endpoint, "ECCTL_SANDBOX_BACKEND": backend, "ECCTL_SANDBOX_CA_FILE": ca}
	c, err := NewCaller(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExplicitBackendOverridesHostWithoutDNS(t *testing.T) {
	for _, backend := range []string{"e2b", "fc", "acs"} {
		t.Run(backend, func(t *testing.T) {
			c := configuredCaller(t, "https://api.e2b.app", backend, "")
			c.lookupCNAME = func(context.Context, string) (map[string]string, error) {
				t.Fatal("explicit backend must not resolve DNS")
				return nil, nil
			}
			got := c.detectBackend(context.Background())
			if got.fc != (backend == "fc") || got.acs != (backend == "acs") {
				t.Fatalf("backend = %+v", got)
			}
		})
	}
	_, err := NewCaller(func(key string) string {
		if key == "E2B_API_KEY" {
			return "key"
		}
		if key == "ECCTL_SANDBOX_BACKEND" {
			return "typo"
		}
		return ""
	})
	assertAppError(t, err, "InvalidSandboxBackend")
}

func TestACSListPaginationAndTokenIsolation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/templates" || r.URL.RawQuery != "" {
			t.Errorf("URL = %s", r.URL)
		}
		fmt.Fprint(w, `[{"templateID":"c"},{"templateID":"a"},{"templateID":"b"}]`)
	}))
	defer server.Close()
	c := configuredCaller(t, server.URL, "acs", "")
	first, err := c.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	token := stringValue(first["nextToken"])
	items := first["items"].([]any)
	if !strings.HasPrefix(token, acsTemplateTokenPrefix) || len(items) != 2 || items[0].(map[string]any)["templateID"] != "a" {
		t.Fatalf("first = %#v", first)
	}
	last, err := c.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 2, "query.nextToken": token})
	if err != nil || len(last["items"].([]any)) != 1 || last["nextToken"] != nil {
		t.Fatalf("last = %#v, %v", last, err)
	}
	for _, backend := range []string{"e2b", "fc"} {
		other := configuredCaller(t, server.URL, backend, "")
		if _, err := other.Call(context.Background(), "ListTemplates", map[string]any{"query.nextToken": token}); err == nil {
			t.Fatal("accepted cross-backend token")
		}
	}
	other := configuredCaller(t, server.URL+"/other", "acs", "")
	if _, err := other.Call(context.Background(), "ListTemplates", map[string]any{"query.nextToken": token}); err == nil {
		t.Fatal("accepted cross-endpoint token")
	}
	if calls != 2 {
		t.Fatalf("token validation made network requests: %d", calls)
	}
}

func TestACSListRejectsIncompleteResponses(t *testing.T) {
	for _, body := range []string{`{}`, `[{"templateID":"a"},{"templateID":"a"}]`, `[{"templateID":""}]`, `[] []`, `[{"templateID":"a","templateID":"b"}]`} {
		t.Run(body, func(t *testing.T) {
			c := configuredCaller(t, "https://example.test", "acs", "")
			c.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, err := c.Call(context.Background(), "ListTemplates", nil)
			assertAppError(t, err, "InvalidACSTemplateResponse")
		})
	}
}

func TestACSUnsupportedAPIsDoNotSendRequests(t *testing.T) {
	c := configuredCaller(t, "https://example.test", "acs", "")
	c.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported operation sent a request")
		return nil, nil
	})
	for _, op := range []string{"UpdateSandboxNetwork", "RefreshSandbox", "ForkSandbox", "GetSandboxLogs", "GetSandboxMetrics",
		"CreateTemplate", "StartTemplateBuild", "UpdateTemplate", "GetTemplateBuildStatus", "GetTemplateBuildLogs", "ListTemplateTags", "AssignTemplateTags", "DeleteTemplateTags"} {
		_, err := c.Call(context.Background(), op, nil)
		assertAppError(t, err, "UnsupportedOperation")
	}
}

func TestACSTemplateDeletionDistinguishesUnsupportedFromAuthentication(t *testing.T) {
	for _, test := range []struct{ body, code string }{
		{"Deleting SandboxSet-backed templates through the E2B API is not supported", "UnsupportedACSTemplateDeletion"},
		{"Invalid API key", "E2BHTTP401"},
	} {
		c := configuredCaller(t, "https://example.test", "acs", "")
		c.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
		})
		_, err := c.Call(context.Background(), "DeleteTemplate", map[string]any{"path.templateID": "template"})
		assertAppError(t, err, test.code)
	}
}

func TestSandboxCustomCAAndHostnameVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Error("missing key")
		}
		fmt.Fprint(w, `{"sandboxID":"sbx"}`)
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c := configuredCaller(t, server.URL, "acs", ca)
	if _, err := c.Call(context.Background(), "GetSandbox", map[string]any{"path.sandboxID": "sbx"}); err != nil {
		t.Fatal(err)
	}
	untrusted := configuredCaller(t, server.URL, "acs", "")
	if _, err := untrusted.Call(context.Background(), "GetSandbox", map[string]any{"path.sandboxID": "sbx"}); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	transport := c.client.Transport.(*http.Transport)
	// Force a fresh handshake; an already verified keep-alive connection does
	// not recheck ServerName for each HTTP request.
	wrongHost := transport.Clone()
	defer wrongHost.CloseIdleConnections()
	wrongHost.TLSClientConfig.ServerName = "wrong.example"
	c.client.Transport = wrongHost
	if _, err := c.Call(context.Background(), "GetSandbox", map[string]any{"path.sandboxID": "sbx"}); err == nil {
		t.Fatal("wrong hostname accepted")
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "empty")} {
		if strings.HasSuffix(path, "empty") {
			_ = os.WriteFile(path, []byte("invalid PEM"), 0600)
		}
		_, err := clientWithCA(path)
		assertAppError(t, err, "InvalidSandboxCA")
	}
}
