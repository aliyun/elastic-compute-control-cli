package spec_resource

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/e2bapi"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

type templateDeadlineTransport struct {
	deadlines []time.Time
	calls     int
}

func (transport *templateDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	deadline, _ := request.Context().Deadline()
	transport.deadlines = append(transport.deadlines, deadline)
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"templateID":"tpl-1"}`))}, nil
}

func TestE2BTemplateUpdateKeepsOriginalRequestDeadline(t *testing.T) {
	for _, public := range []string{"true", "false"} {
		t.Run(public, func(t *testing.T) {
			transport := &templateDeadlineTransport{}
			caller, err := e2bapi.NewCallerWithClient("https://api.e2b.app", "test-key", &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			run := withCaller(func(_, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				if resource.Provider != "e2b" {
					t.Fatalf("legacy route changed: %+v", resource)
				}
				return caller, nil
			})
			started := time.Now()
			stdout, stderr, code := run("--lang", "en", "sandbox", "template", "update", "tpl-1", "--public="+public)
			if code != 0 || transport.calls != 2 {
				t.Fatalf("E2B update calls=%d exit=%d %s %s", transport.calls, code, stdout, stderr)
			}
			for _, deadline := range transport.deadlines {
				if remaining := deadline.Sub(started); remaining < 29*time.Second || remaining > 31*time.Second {
					t.Fatalf("E2B update request deadline=%s; want 30s", remaining)
				}
			}
			stdout, stderr, code = run("--lang", "en", "sandbox", "template", "update", "tpl-1")
			if code == 0 || transport.calls != 2 || errorCode(t, stdout) != "MissingParameter" {
				t.Fatalf("legacy required public was lost: %d %s %s", code, stdout, stderr)
			}
			stdout, stderr, code = run("--lang", "en", "sandbox", "template", "update", "tpl-1", "--public", "--timeout", "1s")
			if code == 0 || transport.calls != 2 || errorCode(t, stdout) != "FCTemplateRequiresOpenAPI" {
				t.Fatalf("legacy native timeout flag was accepted: %d %s %s", code, stdout, stderr)
			}
		})
	}
}

func TestNativeTemplateRejectsExplicitEmptyRegionBeforeCaller(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(p, []byte(`{"current":"test","profiles":[{"name":"test","mode":"AK","region_id":"cn-hangzhou","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", p)
	for _, fallback := range []string{"cn-hangzhou", ""} {
		t.Setenv("ECCTL_REGION", fallback)
		for _, flag := range [][]string{{"--region="}, {"--region", ""}} {
			called := false
			run := withCaller(func(_, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				called = true
				return &fakeSpecCaller{responses: []map[string]any{{"code": "200"}}}, nil
			})
			args := []string{"--lang", "en", "sandbox", "template", "delete", "tpl-1", "--openapi", "--profile", "test"}
			args = append(args, flag...)
			stdout, stderr, code := run(args...)
			if code == 0 || called || errorCode(t, stdout) != "MissingRegion" {
				t.Fatalf("empty explicit region fell back: %v env=%q caller=%v code=%d %s %s", flag, fallback, called, code, stdout, stderr)
			}
		}
	}
	called := false
	run := withCaller(func(_, _ string, r spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		called = true
		if r.Provider != "e2b" {
			t.Fatalf("legacy route=%+v", r)
		}
		return &fakeSpecCaller{responses: []map[string]any{{"items": []any{}}}}, nil
	})
	stdout, stderr, code := run("--lang", "en", "sandbox", "template", "list", "--region=")
	if code != 0 || !called {
		t.Fatalf("E2B global exception changed: %d %s %s", code, stdout, stderr)
	}
}
