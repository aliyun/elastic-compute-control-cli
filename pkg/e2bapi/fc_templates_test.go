package e2bapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestFCTemplateListUsesLegacyPathAndLocalPagination(t *testing.T) {
	var paths []string
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Host != "api.cn-beijing.e2b.fc.aliyuncs.com" || r.Header.Get("X-API-Key") != "test-key" {
			t.Fatal("request changed endpoint or credential")
		}
		if r.Method != http.MethodGet || r.URL.Path != "/prefix/templates" {
			return testJSONResponse(405, ""), nil
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("FC received pagination query: %s", r.URL.RawQuery)
		}
		return testJSONResponse(200, `[{"templateID":"c"},{"templateID":"a"},{"templateID":"b"}]`), nil
	})}
	caller, err := NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com/prefix", "test-key", client)
	if err != nil {
		t.Fatal(err)
	}
	first, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := templateIDs(first); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("first = %v", got)
	}
	token, _ := first["nextToken"].(string)
	if token == "" {
		t.Fatal("missing next token")
	}
	second, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 2, "query.nextToken": token})
	if err != nil {
		t.Fatal(err)
	}
	if got := templateIDs(second); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("second = %v", got)
	}
	if second["nextToken"] != nil {
		t.Fatal("last page has a token")
	}
	if !reflect.DeepEqual(paths, []string{"/prefix/templates", "/prefix/templates"}) {
		t.Fatalf("paths = %v", paths)
	}
}

func TestFCTemplateTokensAreBoundToEndpointAndBackend(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return testJSONResponse(200, `[{"templateID":"a"},{"templateID":"b"}]`), nil
	})}
	caller, _ := NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com", "test-key", client)
	first, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	token := first["nextToken"].(string)
	for _, endpoint := range []string{"https://api.cn-shanghai.e2b.fc.aliyuncs.com", "https://api.cn-beijing.e2b.fc.aliyuncs.com/other", "https://api.e2b.app"} {
		other, _ := NewCallerWithClient(endpoint, "test-key", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid token reached HTTP"); return nil, nil })})
		_, err := other.Call(context.Background(), "ListTemplates", map[string]any{"query.nextToken": token})
		assertAppError(t, err, "InvalidFCTemplateToken")
	}
	caller.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid token reached HTTP"); return nil, nil })
	for _, invalid := range []string{"native-token", "ecctl:fc-templates:v2:foo", fcTemplateTokenPrefix + "bad~", fcTemplateTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"after":"a"}`)), token + strings.Repeat("x", 8192)} {
		_, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.nextToken": invalid})
		assertAppError(t, err, "InvalidFCTemplateToken")
	}
}

func TestFCTemplateListRejectsIncompleteOrAmbiguousResponse(t *testing.T) {
	for _, tt := range []struct{ body, token string }{
		{body: `{"items":[]}`}, {body: `null`}, {body: `[{}]`}, {body: `[{"templateID":1}]`},
		{body: `[{"templateID":""}]`}, {body: `["a"]`}, {body: `[{"templateID":"a"},{"templateID":"a"}]`},
		{body: `[]`, token: "more"},
		{body: `[{"templateID":"a"}] [{"templateID":"b"}]`},
		{body: `{"__ecctl_top_level_array_response":true,"items":[{"templateID":"a"}],"has_more":true}`},
		{body: `[{"templateID":"a","templateID":"b"}]`},
		{body: `[{"templateID":"a","metadata":{"x":1,"x":2}}]`},
		{body: `[{"templateID":"a"}] trailing garbage`},
		{body: `[{"templateID":"a"}`},
		{body: ``},
	} {
		caller, _ := NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com", "test-key", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			response := testJSONResponse(200, tt.body)
			response.Header.Set("X-Next-Token", tt.token)
			return response, nil
		})})
		_, err := caller.Call(context.Background(), "ListTemplates", nil)
		assertAppError(t, err, "InvalidFCTemplateResponse")
	}
}

func TestFCTemplatePaginationDefaultsAndConcurrentDeletion(t *testing.T) {
	items := make([]map[string]any, 101)
	for i := range items {
		items[i] = map[string]any{"templateID": string(rune('a' + i))}
	}
	caller, _ := NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com", "test-key", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		data, _ := json.Marshal(items)
		return testJSONResponse(200, string(data)), nil
	})})
	first, err := caller.Call(context.Background(), "ListTemplates", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(templateIDs(first)) != 100 || first["nextToken"] == nil {
		t.Fatalf("default page = %v", first)
	}
	token := first["nextToken"]
	items = items[100:] // including the previous cursor item itself being deleted
	last, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.nextToken": token})
	if err != nil || len(templateIDs(last)) != 1 || last["nextToken"] != nil {
		t.Fatalf("last=%v err=%v", last, err)
	}
	items = []map[string]any{}
	empty, err := caller.Call(context.Background(), "ListTemplates", nil)
	if err != nil || len(templateIDs(empty)) != 0 || empty["nextToken"] != nil {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
}

func TestFCDoesNotChangeOtherOperationPaths(t *testing.T) {
	for _, tt := range []struct{ op, method, path string }{
		{"ListSandboxes", "GET", "/v2/sandboxes"},
		{"GetTemplate", "GET", "/templates/template-1"},
		{"CreateTemplate", "POST", "/v3/templates"},
		{"UpdateTemplate", "PATCH", "/v2/templates/template-1"},
	} {
		caller, _ := NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com", "test-key", &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != tt.method || r.URL.Path != tt.path {
				t.Fatalf("%s: %s %s", tt.op, r.Method, r.URL.Path)
			}
			return testJSONResponse(200, `{}`), nil
		})})
		caller.lookupCNAME = func(context.Context, string) (map[string]string, error) {
			t.Fatal("unrelated operation performed DNS detection")
			return nil, nil
		}
		if _, err := caller.Call(context.Background(), tt.op, map[string]any{"path.templateID": "template-1"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeTemplateListPreservesV2Pagination(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v2/templates" || r.URL.Query().Get("nextToken") != "native-token" || r.URL.Query().Get("limit") != "2" {
			t.Fatalf("request = %s", r.URL)
		}
		res := testJSONResponse(200, `[{"templateID":"z"},{"templateID":"a"}]`)
		res.Header.Set("X-Next-Token", "native-next")
		return res, nil
	})}
	caller, err := NewCallerWithClient("https://api.e2b.app", "test-key", client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query.limit": 2, "query.nextToken": "native-token"})
	if err != nil {
		t.Fatal(err)
	}
	if result["nextToken"] != "native-next" || !reflect.DeepEqual(templateIDs(result), []string{"z", "a"}) {
		t.Fatalf("result = %v", result)
	}
}

func testJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func templateIDs(result map[string]any) []string {
	ids := []string{}
	for _, item := range result["items"].([]any) {
		ids = append(ids, item.(map[string]any)["templateID"].(string))
	}
	return ids
}
