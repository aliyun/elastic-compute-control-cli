package spec_resource

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/e2bapi"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

type fcTemplateTransport func(*http.Request) (*http.Response, error)

func (f fcTemplateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicFCTemplateListPagination(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	calls := 0
	client := &http.Client{Transport: fcTemplateTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/templates" || r.URL.RawQuery != "" {
			t.Fatalf("request=%s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[{"templateID":"tpl-c"},{"templateID":"tpl-a"},{"templateID":"tpl-b"}]`))}, nil
	})}
	ctx := cli.WithResourceCallerFactory(context.Background(), func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "template" || region != "" {
			t.Fatalf("unexpected resource=%s/%s region=%s", resource.Product, resource.Resource, region)
		}
		return e2bapi.NewCallerWithClient("https://api.cn-beijing.e2b.fc.aliyuncs.com", "test-key", client)
	})
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, []string{"--lang", "en", "sbx", "template", "list", "--limit", "2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	first := decodeObject(t, stdout.String())
	pagination := first["pagination"].(map[string]any)
	items := first["templates"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "tpl-a" || pagination["has_more"] != true || pagination["limit"] != float64(2) {
		t.Fatalf("first=%s", &stdout)
	}
	token := pagination["next_token"].(string)
	stdout.Reset()
	stderr.Reset()
	code = cli.Run(ctx, []string{"--lang", "en", "sbx", "tpl", "list", "--limit", "2", "--next-token", token}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	last := decodeObject(t, stdout.String())
	items = last["templates"].([]any)
	pagination = last["pagination"].(map[string]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "tpl-c" || pagination["has_more"] != false || calls != 2 {
		t.Fatalf("last=%s calls=%d", &stdout, calls)
	}
}
