package aliyun

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestECSMetadataDiscoveryRejectsInvalidAndUnavailableRoles(t *testing.T) {
	for _, role := range []string{"", "a/b", "a\\b", "a\nb", "a\x00b"} {
		client := credentialHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPut {
				return metadataHTTPResponse(200, "token"), nil
			}
			return metadataHTTPResponse(200, role), nil
		})
		if _, err := acquireECSMetadataCredential(nil, "", true, client); err == nil || !strings.Contains(err.Error(), "role name is invalid") {
			t.Fatalf("invalid discovered role %q: %v", role, err)
		}
	}
	for _, failDiscovery := range []bool{true, false} {
		for _, allowFallback := range []bool{true, false} {
			calls := 0
			client := credentialHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodPut {
					return metadataHTTPResponse(200, "token"), nil
				}
				if strings.HasSuffix(req.URL.Path, "/") {
					if failDiscovery {
						return metadataHTTPResponse(503, "unavailable"), nil
					}
					return metadataHTTPResponse(200, "role"), nil
				}
				return metadataHTTPResponse(503, "unavailable"), nil
			})
			if _, err := acquireECSMetadataCredential(context.Background(), "", !allowFallback, client); err == nil {
				t.Fatal("unavailable metadata accepted")
			}
			want := 3
			if failDiscovery {
				want = 2
			}
			if allowFallback {
				want++
			}
			if calls != want {
				t.Fatalf("metadata fallback calls=%d, want %d", calls, want)
			}
		}
	}
}

func TestECSMetadataRejectsMalformedCredentialsAndTransportFailure(t *testing.T) {
	for _, payload := range []string{"invalid", `{}`, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"bad"}`, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"2000-01-01T00:00:00Z"}`} {
		client := credentialHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPut {
				return metadataHTTPResponse(200, "token"), nil
			}
			return metadataHTTPResponse(200, payload), nil
		})
		if _, err := acquireECSMetadataCredential(context.Background(), "role", true, client); err == nil {
			t.Fatalf("malformed metadata accepted: %s", payload)
		}
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	client := credentialHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPut {
			return metadataHTTPResponse(200, "token"), nil
		}
		if req.Header.Get("x-aliyun-ecs-metadata-token") != "" {
			return metadataHTTPResponse(401, "v2 unsupported"), nil
		}
		return metadataHTTPResponse(200, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"`+expires+`"}`), nil
	})
	if snapshot, err := acquireECSMetadataCredential(context.Background(), "role", false, client); err != nil || snapshot.AccessKeyID != "id" {
		t.Fatalf("per-request v1 fallback = %+v, %v", snapshot, err)
	}
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if canceled {
			cancel()
		}
		_, _, err = doECSMetadataRequest(credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport failure") }), request)
		cancel()
		if err == nil || canceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("transport failure = %v", err)
		}
	}
}

func TestFCTemplateResponsesRejectIncompleteNestedObjects(t *testing.T) {
	caller := &OpenAPICaller{}
	for _, tt := range []struct {
		api      string
		response map[string]any
	}{
		{"GetTemplate", map[string]any{"code": "200", "requestId": 1}},
		{"GetTemplate", map[string]any{"code": "500"}},
		{"ListTemplates", map[string]any{"code": "200", "templates": []any{}, "nextToken": 1}},
		{"ListTemplates", map[string]any{"code": "200", "templates": []any{}, "maxResults": 0}},
		{"ListTemplates", map[string]any{"code": "200", "templates": []any{false}}},
	} {
		req := newOpenAPIRequest()
		req.ApiName = tt.api
		if err := caller.fcTemplateResponseError(req, tt.response); err == nil {
			t.Fatalf("invalid template response accepted: %s %#v", tt.api, tt.response)
		}
	}
	for _, field := range []string{"teamID", "status", "runtimeConfig"} {
		response := map[string]any{"code": "200", "templateID": "tpl-test", "teamID": "team-test", "status": map[string]any{"state": "ready"}}
		response[field] = 1
		req := newOpenAPIRequest()
		req.ApiName = "GetTemplate"
		if err := caller.fcTemplateResponseError(req, response); err == nil {
			t.Fatalf("invalid %s accepted", field)
		}
	}
}

func TestFCTemplateCacheResponsesEnforceRequestedPageShape(t *testing.T) {
	caller := &OpenAPICaller{}
	for _, value := range []any{nil, 1, true, []any{}, map[string]any{}, ""} {
		req := newOpenAPIRequest()
		req.ApiName = "DescribeTemplateCache"
		response := map[string]any{"code": "200", "templateCache": value}
		if err := caller.fcTemplateCacheResponseError(req, response); err == nil {
			t.Fatalf("invalid cache object accepted: %#v", value)
		}
	}
	for _, tt := range []struct {
		query    map[string]string
		response map[string]any
	}{
		{map[string]string{"maxResults": "bad"}, map[string]any{"code": "200", "templateCaches": []any{}}},
		{map[string]string{"maxResults": "2"}, map[string]any{"code": "200", "templateCaches": []any{}, "maxResults": 3}},
		{nil, map[string]any{"code": "200", "templateCaches": []any{}, "maxResults": 1.5}},
		{nil, map[string]any{"code": "200", "templateCaches": []any{}, "maxResults": "2"}},
		{nil, map[string]any{"code": "200", "templateCaches": []any{}, "maxResults": 101}},
		{nil, map[string]any{"code": "200", "templateCaches": []any{map[string]any{"templateID": "tpl-test", "teamID": "team-test", "status": "Success", "progress": 1.5}}}},
		{nil, map[string]any{"code": "200", "templateCaches": []any{map[string]any{"templateID": "tpl-test", "teamID": "team-test", "status": "Success", "teamID.invalid": ""}, map[string]any{"templateID": "tpl-test", "teamID": "team-test", "status": "Success"}}}},
	} {
		req := newOpenAPIRequest()
		req.ApiName, req.QueryParams = "ListTemplateCache", tt.query
		if err := caller.fcTemplateCacheResponseError(req, tt.response); err == nil {
			t.Fatalf("invalid cache page accepted: %#v", tt.response)
		}
	}
	req := newOpenAPIRequest()
	req.ApiName = "CreateTemplateCache"
	if err := caller.fcTemplateCacheResponseError(req, map[string]any{"code": "200"}); err == nil {
		t.Fatal("create response accepted invalid request body")
	}
}
