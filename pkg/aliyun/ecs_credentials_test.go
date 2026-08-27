package aliyun

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestECSMetadataCredentialUsesIMDSv2WithoutLoggingTransport(t *testing.T) {
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	calls := 0
	client := credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			if request.Method != http.MethodPut || request.URL.Path != "/latest/api/token" || request.Header.Get("X-aliyun-ecs-metadata-token-ttl-seconds") != ecsMetadataTokenTTL {
				t.Fatalf("token request = %#v", request)
			}
			return metadataHTTPResponse(http.StatusOK, "metadata-token"), nil
		case 2:
			if request.URL.Path != "/latest/meta-data/ram/security-credentials/role" || request.Header.Get("x-aliyun-ecs-metadata-token") != "metadata-token" {
				t.Fatalf("credential request = %#v", request)
			}
			return metadataHTTPResponse(http.StatusOK, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"sts","Expiration":"`+expiration+`"}`), nil
		default:
			t.Fatalf("unexpected metadata call %d", calls)
			return nil, nil
		}
	})
	snapshot, err := acquireECSMetadataCredential(context.Background(), "role", true, client)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AccessKeyID != "id" || snapshot.SecurityToken != "sts" || snapshot.ExpiresAt.IsZero() || calls != 2 {
		t.Fatalf("snapshot=%#v calls=%d", snapshot, calls)
	}
}

func TestECSMetadataCredentialFallsBackOnlyWhenIMDSv1Allowed(t *testing.T) {
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	client := credentialHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/latest/api/token" {
			return metadataHTTPResponse(http.StatusNotFound, "missing"), nil
		}
		if request.Header.Get("x-aliyun-ecs-metadata-token") != "" {
			t.Fatal("IMDSv1 fallback kept metadata token")
		}
		return metadataHTTPResponse(http.StatusOK, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"sts","Expiration":"`+expiration+`"}`), nil
	})
	if _, err := acquireECSMetadataCredential(context.Background(), "role", false, client); err != nil {
		t.Fatalf("IMDSv1 fallback: %v", err)
	}
	if _, err := acquireECSMetadataCredential(context.Background(), "role", true, client); err == nil || !strings.Contains(err.Error(), "IMDSv2") {
		t.Fatalf("disabled IMDSv1 error = %v", err)
	}
}

func TestECSMetadataCredentialHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireECSMetadataCredential(ctx, "role", true, credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled context reached metadata transport")
		return nil, nil
	})); err == nil {
		t.Fatal("canceled context was accepted")
	}
}

func TestECSMetadataHTTPClientDisablesProxy(t *testing.T) {
	client := newECSMetadataHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || client.CheckRedirect == nil {
		t.Fatalf("metadata HTTP client = %#v", client)
	}
}

func metadataHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}
