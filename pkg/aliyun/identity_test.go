package aliyun

import (
	"context"
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
		{name: "account", response: map[string]any{"IdentityType": "Account", "AccountId": "123456789"}, wantHash: "48c8f758e0855696005786ffb5bd7b547267bbdd06d70b4b0f0033e8e2ac344f", wantType: "Account"},
		{name: "RAM user", response: map[string]any{"IdentityType": "RAMUser", "UserId": "user-123"}, wantHash: "35d9b4d55187a78bd6f8e598e18595d01f4170d07e55670d9d90bd9d6f24f6f3", wantType: "RAMUser"},
		{name: "assumed role", response: map[string]any{"IdentityType": "AssumedRoleUser", "RoleId": "role-123"}, wantHash: "b6e496c4bde392bc6574981a7dee37cda0745bb5b820261964eefda03acfd235", wantType: "AssumedRoleUser"},
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
		"IdentityType": "AssumedRoleUser", "RoleId": "role-123", "PrincipalId": "role-123:session-a", "Arn": "acs:ram::123:role/session-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalIdentity(map[string]any{
		"IdentityType": "AssumedRoleUser", "RoleId": "role-123", "PrincipalId": "role-123:session-b", "Arn": "acs:ram::123:role/session-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same role produced different identities: %#v vs %#v", first, second)
	}
}

func TestIdentityResolverUsesFixedSTSRequest(t *testing.T) {
	executor := &fakeOpenAPIExecutor{response: `{"IdentityType":"RAMUser","UserId":"user-123"}`}
	resolver := identityResolver(resolvedOpenAPIProfile{}, executor)
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
