package telemetry

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/aliyun/elastic-compute-control-cli/internal/telemetryconfig"
)

// These values are intentionally populated only by the release build. They
// are not configurable at runtime so OTEL_EXPORTER_* cannot redirect ecctl's
// product telemetry.
var (
	releaseEndpointB64 string
	releaseHeadersB64  string
)

func newReleaseExporter(ctx context.Context) (trace.SpanExporter, error) {
	config, err := telemetryconfig.Decode(releaseEndpointB64, releaseHeadersB64)
	if err != nil {
		return nil, err
	}
	return newHTTPExporter(ctx, config, nil)
}

func newHTTPExporter(ctx context.Context, config telemetryconfig.Config, client *http.Client) (trace.SpanExporter, error) {
	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(config.Endpoint),
		otlptracehttp.WithHeaders(config.Headers),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	}
	parsed, _ := url.Parse(config.Endpoint)
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport, Timeout: 10 * time.Second}
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if parsed.RawQuery != "" {
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		client.Transport = queryRoundTripper{base: transport, rawQuery: parsed.RawQuery}
	}
	options = append(options, otlptracehttp.WithHTTPClient(client))
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, err
	}
	return quietExporter{SpanExporter: exporter}, nil
}

// otlptracehttp's WithEndpointURL intentionally keeps only scheme, host and
// path. ARMS reporting addresses can include an authentication query string,
// so preserve the release-injected query without exposing runtime overrides.
type queryRoundTripper struct {
	base     http.RoundTripper
	rawQuery string
}

func (t queryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	urlCopy := *request.URL
	urlCopy.RawQuery = t.rawQuery
	copy.URL = &urlCopy
	return t.base.RoundTrip(copy)
}

// quietExporter makes delivery best-effort. In particular, exporter failures
// must never reach OTel's process-global error handler or the CLI output.
type quietExporter struct {
	trace.SpanExporter
}

func (e quietExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	_ = e.SpanExporter.ExportSpans(ctx, spans)
	return nil
}

func (e quietExporter) Shutdown(ctx context.Context) error {
	_ = e.SpanExporter.Shutdown(ctx)
	return nil
}
