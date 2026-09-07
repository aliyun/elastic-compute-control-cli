package e2bapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func errorPayload(t *testing.T, err error) ecerrors.ErrorPayload {
	t.Helper()
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("not an AppError: %v", err)
	}
	return appErr.Payload()
}

func TestIdentifyBackend(t *testing.T) {
	tests := []struct {
		name, host  string
		answers     map[string]map[string]string
		fc, wantErr bool
		calls       []string
	}{
		{name: "FC API", host: "API.CN-BEIJING.E2B.FC.ALIYUNCS.COM.", fc: true},
		{name: "FC suffix itself", host: fcDomain, fc: true},
		{name: "native", host: "api.e2b.app"},
		{name: "literal IP", host: "127.0.0.1"},
		{name: "lookalike prefix", host: "note2b.fc.aliyuncs.com", calls: []string{"note2b.fc.aliyuncs.com"}},
		{name: "lookalike suffix", host: "e2b.fc.aliyuncs.com.attacker.example", calls: []string{"e2b.fc.aliyuncs.com.attacker.example"}},
		{name: "connected chain includes FC before NLB", host: "api.custom.example", fc: true, calls: []string{"api.custom.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"API.CUSTOM.EXAMPLE.": "edge.example.", "edge.example.": "api.cn-beijing.e2b.fc.aliyuncs.com.", "api.cn-beijing.e2b.fc.aliyuncs.com.": "nlb.aliyuncsslb.com."},
		}},
		{name: "separate responses", host: "api.custom.example", fc: true, calls: []string{"api.custom.example", "edge.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"api.custom.example": "edge.example"}, "edge.example": {"edge.example": fcDomain},
		}},
		{name: "unrelated FC answer ignored", host: "api.custom.example", calls: []string{"api.custom.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"other.example": fcDomain},
		}},
		{name: "native custom chain", host: "api.custom.example", calls: []string{"api.custom.example", "edge.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"api.custom.example": "edge.example"},
		}},
		{name: "loop", host: "api.custom.example", wantErr: true, calls: []string{"api.custom.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"api.custom.example": "edge.example", "edge.example": "api.custom.example"},
		}},
		{name: "invalid target", host: "api.custom.example", wantErr: true, calls: []string{"api.custom.example"}, answers: map[string]map[string]string{
			"api.custom.example": {"api.custom.example": "https://api.cn-beijing.e2b.fc.aliyuncs.com"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			result, err := identifyBackend(context.Background(), tt.host, func(_ context.Context, host string) (map[string]string, error) {
				calls = append(calls, host)
				return tt.answers[host], nil
			})
			if result.fc != tt.fc || (err != nil) != tt.wantErr || !reflect.DeepEqual(calls, tt.calls) {
				t.Fatalf("result=%+v err=%v calls=%v", result, err, calls)
			}
		})
	}
}

func TestCNAMEHopLimit(t *testing.T) {
	for _, fcAtLastHop := range []bool{false, true} {
		calls := 0
		result, err := identifyBackend(context.Background(), "hop0.example", func(_ context.Context, host string) (map[string]string, error) {
			calls++
			next := fmt.Sprintf("hop%d.example", calls)
			if fcAtLastHop && calls == maxCNAMEHops {
				next = fcDomain
			}
			return map[string]string{host: next}, nil
		})
		if calls != maxCNAMEHops || result.fc != fcAtLastHop || (err == nil) != fcAtLastHop {
			t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
		}
	}
}

func TestBackendDetectionRetriesFailuresAndCachesSuccess(t *testing.T) {
	caller, _ := NewCallerWithClient("https://api.custom.example", "test-key", nil)
	lookups := 0
	caller.lookupCNAME = func(ctx context.Context, host string) (map[string]string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > backendLookupTimeout {
			t.Fatal("missing bounded DNS deadline")
		}
		lookups++
		if lookups == 1 {
			return nil, errors.New("temporary failure")
		}
		return map[string]string{host: fcDomain}, nil
	}
	if result := caller.detectBackend(context.Background()); result.fc || result.reason != "dns_lookup_failed" {
		t.Fatalf("result=%+v", result)
	}
	if !caller.detectBackend(context.Background()).fc || !caller.detectBackend(context.Background()).fc || lookups != 2 {
		t.Fatalf("lookups=%d", lookups)
	}
	other, _ := NewCallerWithClient("https://api.custom.example", "test-key", nil)
	other.lookupCNAME = func(context.Context, string) (map[string]string, error) { return nil, nil }
	if other.detectBackend(context.Background()).fc {
		t.Fatal("backend cached across callers")
	}
}

func TestBackendDetectionCancellation(t *testing.T) {
	caller, _ := NewCallerWithClient("https://api.custom.example", "test-key", nil)
	caller.lookupCNAME = func(context.Context, string) (map[string]string, error) {
		t.Fatal("cancelled lookup ran")
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := caller.detectBackend(ctx)
	if result.fc || result.reason != "dns_timeout" || caller.backendResult != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestEffectiveAPIEndpointSelectsBackend(t *testing.T) {
	for _, tt := range []struct {
		api, domain string
		fc          bool
	}{
		{domain: "cn-beijing.e2b.fc.aliyuncs.com", fc: true},
		{api: "https://api.e2b.app", domain: "cn-beijing.e2b.fc.aliyuncs.com"},
		{api: "https://api.cn-beijing.e2b.fc.aliyuncs.com", domain: "e2b.app", fc: true},
		{},
	} {
		caller, err := NewCaller(func(key string) string {
			return map[string]string{"E2B_API_KEY": "test-key", "E2B_API_URL": tt.api, "E2B_DOMAIN": tt.domain}[key]
		})
		if err != nil {
			t.Fatal(err)
		}
		caller.lookupCNAME = func(context.Context, string) (map[string]string, error) {
			t.Fatal("unexpected DNS lookup")
			return nil, nil
		}
		if got := caller.detectBackend(context.Background()).fc; got != tt.fc {
			t.Fatalf("api=%s domain=%s fc=%v", tt.api, tt.domain, got)
		}
	}
}

func TestCustomDomainFCDoesNotRewriteRequest(t *testing.T) {
	caller, _ := NewCallerWithClient("https://api.custom.example:8443/prefix", "test-key", &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.custom.example:8443/prefix/templates?teamID=team-1" || r.Host != "api.custom.example:8443" || r.Header.Get("X-API-Key") != "test-key" {
			t.Fatalf("unexpected request: %v", r)
		}
		return testJSONResponse(200, `[]`), nil
	})})
	caller.lookupCNAME = func(_ context.Context, host string) (map[string]string, error) {
		return map[string]string{host: fcDomain}, nil
	}
	_, err := caller.Call(context.Background(), "ListTemplates", map[string]any{"query": map[string]any{"limit": 1, "teamID": "team-1"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnidentifiedBackendDoesNotFallbackOnHTTPError(t *testing.T) {
	for _, status := range []int{401, 403, 404, 405, 429, 500} {
		calls := 0
		caller, _ := NewCallerWithClient("https://api.custom.example", "test-key", &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Path != "/v2/templates" {
				t.Fatalf("unexpected fallback: %s", r.URL.Path)
			}
			return testJSONResponse(status, `{"message":"remote failure"}`), nil
		})})
		caller.lookupCNAME = func(context.Context, string) (map[string]string, error) { return nil, errors.New("resolver failed") }
		_, err := caller.Call(context.Background(), "ListTemplates", nil)
		if err == nil || calls != 1 {
			t.Fatalf("status=%d err=%v calls=%d", status, err, calls)
		}
		if !strings.Contains(errorPayload(t, err).Detail, "dns_lookup_failed") {
			t.Fatalf("missing DNS diagnostic: %v", err)
		}
	}
}
