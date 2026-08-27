package aliyun

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestCanonicalIdentityFixedVectors(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		wantHash string
		wantType string
	}{
		{name: "account", response: map[string]any{"IdentityType": "Account", "AccountId": "123456789"}, wantHash: "a32adb8d26aedbf45d49bf14730b39a76b75d1073b14d444306dea4abf93a858", wantType: "Account"},
		{name: "RAM user", response: map[string]any{"IdentityType": "RAMUser", "AccountId": "123456789", "UserId": "user-123"}, wantHash: "2705515811b68ec8e3b9071b17a009a729877ce6bbec0a37a3a0c7733884a2a4", wantType: "RAMUser"},
		{name: "assumed role", response: map[string]any{"IdentityType": "AssumedRoleUser", "AccountId": "123456789", "RoleId": "role-123"}, wantHash: "404cabdabf90c64e1f19fb6bf30b7cc1083016dbb4e388cedde6ab9bd5c6ec0d", wantType: "AssumedRoleUser"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := canonicalIdentity(tc.response)
			if err != nil {
				t.Fatal(err)
			}
			if identity.Hash != tc.wantHash || identity.Type != tc.wantType {
				t.Fatalf("identity = %#v, want hash=%s type=%s", identity, tc.wantHash, tc.wantType)
			}
		})
	}
}

func TestCanonicalIdentityRejectsFallbackFields(t *testing.T) {
	for _, response := range []map[string]any{
		{"IdentityType": "Account", "PrincipalId": "principal", "Arn": "arn"},
		{"IdentityType": "RAMUser", "PrincipalId": "principal", "Arn": "arn"},
		{"IdentityType": "AssumedRoleUser", "PrincipalId": "role:session", "Arn": "arn"},
		{"IdentityType": "Unknown", "UserId": "user"},
	} {
		if identity, err := canonicalIdentity(response); err == nil || identity.Hash != "" {
			t.Fatalf("canonicalIdentity(%#v) = %#v, %v; want rejection", response, identity, err)
		}
	}
}

func TestCanonicalIdentityIgnoresCredentialAndRoleSessionVariation(t *testing.T) {
	first, err := canonicalIdentity(map[string]any{
		"IdentityType": "AssumedRoleUser", "AccountId": "123456789", "RoleId": "role-123", "PrincipalId": "role-123:session-a", "Arn": "acs:ram::1234567890123456:role/session-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalIdentity(map[string]any{
		"IdentityType": "AssumedRoleUser", "AccountId": "123456789", "RoleId": "role-123", "PrincipalId": "role-123:session-b", "Arn": "acs:ram::1234567890123456:role/session-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same role produced different identities: %#v vs %#v", first, second)
	}
}

func TestCanonicalIdentityIncludesOwningAccount(t *testing.T) {
	first, err := canonicalIdentity(map[string]any{
		"IdentityType": "RAMUser", "AccountId": "111", "UserId": "user-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalIdentity(map[string]any{
		"IdentityType": "RAMUser", "AccountId": "222", "UserId": "user-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("same user ID in different accounts produced the same canonical identity")
	}
}

func TestIdentityResolverUsesFixedSTSRequest(t *testing.T) {
	executor := &fakeOpenAPIExecutor{response: `{"IdentityType":"RAMUser","AccountId":"123456789","UserId":"user-123"}`}
	resolver := identityResolver(executor)
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	if _, err := resolver(ctx); err != nil {
		t.Fatalf("resolver: %v", err)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(executor.requests))
	}
	request := executor.requests[0]
	if request.Product != "Sts" || request.Version != "2015-04-01" || request.ApiName != "GetCallerIdentity" || request.Domain != "sts.aliyuncs.com" || request.Scheme != "https" || request.Method != "GET" {
		t.Fatalf("STS request = %#v", request)
	}
	session.Finish("ecctl call ecs DescribeInstances", 0)
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "ecctl.command" {
		t.Fatalf("identity probe produced API telemetry spans: %#v", spans)
	}
}

func TestConfiguredCloudSSOAccountRejectsDifferentCallerIdentity(t *testing.T) {
	executor := &fakeOpenAPIExecutor{response: `{"IdentityType":"RAMUser","AccountId":"unexpected-account","UserId":"user-123"}`}
	profile := resolvedOpenAPIProfile{ExpectedAccountID: "expected-account"}
	identity, err := resolveCredentialIdentityWithExecutor(context.Background(), profile, executor, "sts.aliyuncs.com")
	if !errors.Is(err, ErrCredentialAccountMismatch) || identity.Hash != "" {
		t.Fatalf("identity = %#v, error = %v", identity, err)
	}
	if len(executor.requests) != 1 || executor.requests[0].ApiName != "GetCallerIdentity" {
		t.Fatalf("identity requests = %#v", executor.requests)
	}
}
