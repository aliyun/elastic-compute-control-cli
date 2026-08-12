package telemetryconfig

import (
	"encoding/base64"
	"strings"
	"testing"
)

func encoded(value string) string {
	value = strings.ReplaceAll(value, "example.com", "tracing-analysis-dc-hz.aliyuncs.com")
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func encodedRaw(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func TestDecodeTelemetryConfig(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		headers  string
		wantErr  error
	}{
		{name: "valid empty headers", endpoint: encoded("https://example.com/v1/traces?token=secret"), headers: encoded(`{}`)},
		{name: "valid port 1", endpoint: encoded("https://example.com:1/v1/traces"), headers: encoded(`{}`)},
		{name: "valid port 4318", endpoint: encoded("https://example.com:4318/v1/traces"), headers: encoded(`{"x-token":"value"}`)},
		{name: "valid port 65535", endpoint: encoded("https://example.com:65535/v1/traces"), headers: encoded(`{}`)},
		{name: "valid header tab and obs-text", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"x_test~ok\":\"value\\t\u00e9\"}")},
		{name: "endpoint base64", endpoint: "%%%", headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "empty endpoint", endpoint: encoded(""), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "surrounding whitespace", endpoint: encoded(" https://example.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "http", endpoint: encoded("http://example.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "relative", endpoint: encoded("/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "missing host", endpoint: encoded("https:///v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "userinfo", endpoint: encoded("https://user:secret@example.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "fragment", endpoint: encoded("https://example.com/v1/traces#secret"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "third-party host", endpoint: encodedRaw("https://example.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "suffix confusion", endpoint: encodedRaw("https://evilaliyuncs.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "suffix after attacker host", endpoint: encodedRaw("https://aliyuncs.com.attacker.example/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "trailing dot", endpoint: encodedRaw("https://tracing-analysis-dc-hz.aliyuncs.com./v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "IP address", endpoint: encodedRaw("https://127.0.0.1/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "tenant Function Compute host", endpoint: encodedRaw("https://123456789.cn-hangzhou.fc.aliyuncs.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "tenant OSS host", endpoint: encodedRaw("https://ecctl-metrics.oss-cn-hangzhou.aliyuncs.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "unlisted tracing lookalike", endpoint: encodedRaw("https://tracing-attacker.aliyuncs.com/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port zero", endpoint: encoded("https://example.com:0/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port too large", endpoint: encoded("https://example.com:65536/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port much too large", endpoint: encoded("https://example.com:99999/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port empty", endpoint: encoded("https://example.com:/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port non-numeric", endpoint: encoded("https://example.com:not-a-port/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "port negative", endpoint: encoded("https://example.com:-1/v1/traces"), headers: encoded(`{}`), wantErr: ErrEndpoint},
		{name: "headers base64", endpoint: encoded("https://example.com/v1/traces"), headers: "%%%", wantErr: ErrHeaders},
		{name: "headers null", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`null`), wantErr: ErrHeaders},
		{name: "headers array", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`[]`), wantErr: ErrHeaders},
		{name: "headers non-string", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"x":1}`), wantErr: ErrHeaders},
		{name: "bad header name", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"bad header":"value"}`), wantErr: ErrHeaders},
		{name: "header name colon", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"bad:name":"value"}`), wantErr: ErrHeaders},
		{name: "header name nul", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"bad\\u0000name\":\"value\"}"), wantErr: ErrHeaders},
		{name: "header crlf", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"x\":\"secret\\r\\nvalue\"}"), wantErr: ErrHeaders},
		{name: "header value nul", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"x\":\"secret\\u0000value\"}"), wantErr: ErrHeaders},
		{name: "header value unit separator", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"x\":\"secret\\u001fvalue\"}"), wantErr: ErrHeaders},
		{name: "header value delete", endpoint: encoded("https://example.com/v1/traces"), headers: encoded("{\"x\":\"secret\\u007fvalue\"}"), wantErr: ErrHeaders},
		{name: "duplicate header ignoring case", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"Authorization":"a","authorization":"b"}`), wantErr: ErrHeaders},
		{name: "reserved content encoding", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"Content-Encoding":"gzip"}`), wantErr: ErrHeaders},
		{name: "reserved content type", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"content-type":"application/json"}`), wantErr: ErrHeaders},
		{name: "reserved connection", endpoint: encoded("https://example.com/v1/traces"), headers: encoded(`{"Connection":"close"}`), wantErr: ErrHeaders},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config, err := Decode(tc.endpoint, tc.headers)
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("Decode error = %v, want %v", err, tc.wantErr)
				}
				for _, secret := range []string{"secret", "user", "value"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaked input %q: %v", secret, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.Endpoint == "" || config.Headers == nil {
				t.Fatalf("config = %#v", config)
			}
		})
	}
}
