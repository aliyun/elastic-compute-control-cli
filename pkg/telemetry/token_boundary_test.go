package telemetry

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/telemetryconfig"
)

func TestTelemetryUnavailableInputsStayQuietAndMalformedTokensFail(t *testing.T) {
	if activeInstallationHash("") != "" || FromContext(nil) != nil {
		t.Fatal("unavailable context acquired identity")
	}
	var session *Session
	session.RecordCredentialOutcome("")
	session.Finish("ecctl", 0)
	if pruneIdentityCache(nil, time.Now()) {
		t.Fatal("nil cache pruned")
	}
	cache := identityCacheFile{}
	if !pruneIdentityCache(&cache, time.Now()) || cache.Entries == nil {
		t.Fatal("missing entries not initialized")
	}
	if _, err := encodeInstallationToken([]byte("short")); err == nil {
		t.Fatal("short installation token accepted")
	}
	if _, err := decodeInstallationToken([]byte(strings.Repeat("g", 64))); err == nil {
		t.Fatal("non-hex token accepted")
	}
}

func TestReleaseExporterInitializesAndShutsDownWithoutSending(t *testing.T) {
	oldEndpoint, oldHeaders := releaseEndpointB64, releaseHeadersB64
	t.Cleanup(func() { releaseEndpointB64, releaseHeadersB64 = oldEndpoint, oldHeaders })
	releaseEndpointB64 = base64.StdEncoding.EncodeToString([]byte("https://tracing-cn-hangzhou.arms.aliyuncs.com/v1/traces"))
	releaseHeadersB64 = base64.StdEncoding.EncodeToString([]byte(`{}`))
	exporter, err := newReleaseExporter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	exporter, err = newHTTPExporter(context.Background(), telemetryconfig.Config{Endpoint: "https://example.invalid/v1/traces?token=fixture"}, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCachedIdentity(context.Background(), "", "", nil); err == nil {
		t.Fatal("unavailable resolver accepted")
	}
}
