package e2bapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fcRegionMetadata(regions ...string) string {
	endpoints := []map[string]string{}
	for _, region := range regions {
		endpoints = append(endpoints, map[string]string{"regionId": region, "public": "fcsandbox." + region + ".aliyuncs.com"})
	}
	raw, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"type": "regional", "endpoints": endpoints}})
	return string(raw)
}

func TestFCRegionDiscoveryFallback(t *testing.T) {
	valid := fcRegionMetadata("cn-beijing")
	for _, tt := range []struct {
		name, body string
		status     int
		err        error
	}{
		{name: "unsupported region", body: fcRegionMetadata("cn-shanghai")},
		{name: "empty regions", body: fcRegionMetadata()},
		{name: "network failure", err: errors.New("offline")},
		{name: "HTTP failure", status: http.StatusServiceUnavailable, body: valid},
		{name: "error code", body: strings.Replace(valid, `"code":0`, `"code":1`, 1)},
		{name: "missing code", body: strings.Replace(valid, `"code":0,`, ``, 1)},
		{name: "null data", body: `{"code":0,"data":null}`},
		{name: "invalid JSON", body: `{`},
		{name: "trailing JSON", body: valid + `{}`},
		{name: "wrong metadata type", body: strings.Replace(valid, `"regional"`, `"global"`, 1)},
		{name: "wrong endpoint", body: strings.Replace(valid, "fcsandbox.cn-beijing.aliyuncs.com", "attacker.example", 1)},
		{name: "oversized response", body: valid + strings.Repeat(" ", maxFCRegionBodyLen)},
		{name: "redirect", status: http.StatusFound, body: valid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			calls := 0
			http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != fcSandboxEndpointsURL {
					t.Fatalf("discovery followed redirect to %s", r.URL)
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > fcRegionLookupTimeout {
					t.Fatal("metadata lookup has no bounded deadline")
				}
				if tt.err != nil {
					return nil, tt.err
				}
				status := tt.status
				if status == 0 {
					status = http.StatusOK
				}
				resp := testJSONResponse(status, tt.body)
				resp.Header.Set("Location", "https://attacker.example")
				return resp, nil
			})
			if got := defaultFCDomain(context.Background(), "cn-beijing"); got != defaultDomain || calls != 1 {
				t.Fatalf("domain=%s calls=%d", got, calls)
			}
		})
	}
}

func TestFCRegionDiscoveryUsesCommandContext(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	ctx, cancel := context.WithCancel(context.Background())
	http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(time.Second):
			t.Fatal("command cancellation did not cancel metadata lookup")
			return nil, context.DeadlineExceeded
		}
	})
	defer cancel()
	if got := defaultFCDomain(ctx, "cn-beijing"); got != defaultDomain {
		t.Fatalf("domain=%s", got)
	}
}

func TestFCRegionDiscoverySkipsUnneededRequests(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected metadata lookup")
		return nil, errors.New("unexpected lookup")
	})
	for _, region := range []string{"", "cn-hangzhou", "../bad", "cn-beijing.attacker.example"} {
		if got := defaultFCDomain(context.Background(), region); got != defaultDomain {
			t.Fatalf("region=%s domain=%s", region, got)
		}
	}
	for _, env := range []map[string]string{
		{"E2B_API_URL": "https://api.e2b.app"},
		{"E2B_DOMAIN": "e2b.app"},
	} {
		env["E2B_API_KEY"] = "project-key"
		caller, err := NewCallerWithRegion(context.Background(), "cn-beijing", func(key string) string { return env[key] })
		if err != nil {
			t.Fatal(err)
		}
		if caller.endpoint.String() != "https://api.e2b.app" {
			t.Fatalf("explicit endpoint changed: %s", caller.endpoint)
		}
	}
}
