package aliyun

import (
	"context"
	"errors"
	"testing"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
)

type cancelingCredentialProvider struct{ cancel context.CancelFunc }

func (p cancelingCredentialProvider) GetProviderName() string { return "fixture" }
func (p cancelingCredentialProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	p.cancel()
	return &credentialproviders.Credentials{AccessKeyId: "id", AccessKeySecret: "secret"}, nil
}

func TestCredentialAdapterRejectsCancellationBeforeAndAfterProvider(t *testing.T) {
	adapter := &credentialsProviderAcquirer{provider: &rotatingCredentialsProvider{}}
	if _, err := adapter.Acquire(nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("preexisting cancellation lost: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	adapter.provider = cancelingCredentialProvider{cancel}
	if _, err := adapter.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider cancellation lost: %v", err)
	}
}

func TestCredentialEndpointAndTokenGuardsRejectUnsafeIdentifiers(t *testing.T) {
	for _, raw := range []string{"", "https://:443"} {
		if _, err := validateExplicitSTSEndpoint(raw); err == nil {
			t.Fatalf("invalid STS endpoint accepted: %q", raw)
		}
	}
	for _, token := range []string{"token\rvalue", "token\nvalue", "token\x00value"} {
		if _, err := bearerCredential(token, "", credentialModeBearerToken); err == nil {
			t.Fatal("bearer control character accepted")
		}
	}
	if credentialIdentityCacheKey(resolvedOpenAPIProfile{}, nil) != "" || credentialIdentityCacheKey(resolvedOpenAPIProfile{}, &credentialSnapshot{BearerToken: "fixture"}) != "" {
		t.Fatal("unavailable identity cache key populated")
	}
	for _, raw := range []string{"relative", "%invalid"} {
		if credentialSourcePrincipal(raw) != "" || sanitizedCredentialSource(raw) != "[REDACTED]" {
			t.Fatal("unsafe credential source exposed")
		}
	}
	if isDryRunPassed(nil) || isDependencyViolation(nil) || isDryRunValidationPassed("request validation has been passed with dryrun flag set request id:") {
		t.Fatal("incomplete cloud error treated as success")
	}
}
