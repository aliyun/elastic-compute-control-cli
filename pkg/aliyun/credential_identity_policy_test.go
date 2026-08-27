package aliyun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestParseRAMRoleARNStrictlyDerivesAccount(t *testing.T) {
	accountID, roleName, err := parseRAMRoleARN("acs:ram::1234567890123456:role/Admin-1.2")
	if err != nil || accountID != "1234567890123456" || roleName != "Admin-1.2" {
		t.Fatalf("role ARN = %q, %q, %v", accountID, roleName, err)
	}
	for _, invalid := range []string{
		"acs:ram::123:role/admin",
		"acs:ram:*:1234567890123456:role/admin",
		"acs:ecs::1234567890123456:role/admin",
		"acs:ram::1234567890123456:role/admin/path",
		"acs:ram::1234567890123456:user/admin",
	} {
		if account, role, err := parseRAMRoleARN(invalid); err == nil || account != "" || role != "" {
			t.Fatalf("invalid role ARN %q accepted as %q/%q", invalid, account, role)
		}
	}
}

func TestOIDCProviderAndRoleMustShareAccount(t *testing.T) {
	profile := map[string]any{
		"name": "oidc", "mode": credentialModeOIDC,
		"oidc_provider_arn": "acs:ram::2109876543210987:oidc-provider/provider",
		"oidc_token_file":   "/tmp/token",
		"ram_role_arn":      "acs:ram::1234567890123456:role/admin",
	}
	_, err := resolveAliyunProfileCredential(map[string]any{}, profile, "oidc", "/tmp/config.json", mapGetenv(nil), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "same account") {
		t.Fatalf("cross-account OIDC profile error = %v", err)
	}
}

func TestCredentialIdentityEndpointNeverUsesCustomIssuer(t *testing.T) {
	profile := resolvedOpenAPIProfile{
		RegionID:       "cn-hangzhou",
		IdentityPolicy: credentialIdentityPolicy{stsRegion: "cn-shanghai", enableVPC: true},
		Acquirer:       &ramRoleCredentialAcquirer{stsEndpoint: "custom.sts.example.com", stsRegion: "cn-shanghai", enableVPC: true},
	}
	endpoint, err := credentialIdentityEndpoint(profile, withCredentialOperationRegion(context.Background(), "cn-hangzhou"))
	if err != nil || endpoint != "sts-vpc.cn-shanghai.aliyuncs.com" {
		t.Fatalf("identity endpoint = %q, %v", endpoint, err)
	}
}

func TestRenewableIdentityEndpointMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, region, operationRegion, want string
		vpc                                 bool
	}{
		{name: "global", operationRegion: "cn-hangzhou", want: "sts.aliyuncs.com"},
		{name: "regional", region: "cn-shanghai", operationRegion: "cn-hangzhou", want: "sts.cn-shanghai.aliyuncs.com"},
		{name: "vpc operation region", operationRegion: "cn-hangzhou", vpc: true, want: "sts-vpc.cn-hangzhou.aliyuncs.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, err := (credentialIdentityPolicy{stsRegion: tc.region, enableVPC: tc.vpc}).endpoint(tc.operationRegion)
			if err != nil || endpoint != tc.want {
				t.Fatalf("endpoint = %q, %v; want %q", endpoint, err, tc.want)
			}
		})
	}
}

func TestInitialExpectedAccountFailurePreventsBusinessExecutor(t *testing.T) {
	provider := &rotatingCredentialsProvider{}
	profile := resolvedOpenAPIProfile{
		Mode: credentialModeRamRoleArn, AuthType: "AK", RegionID: "cn-hangzhou",
		Acquirer:          &credentialsProviderAcquirer{provider: provider, mode: credentialModeRamRoleArn},
		ExpectedAccountID: "1234567890123456", ExpectedIdentityType: "AssumedRoleUser",
		PinCredentialIdentity: true,
	}
	originalResolver := resolveCredentialSnapshotIdentity
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		return telemetry.Identity{}, ErrCredentialAccountMismatch
	}
	defer func() { resolveCredentialSnapshotIdentity = originalResolver }()
	business := &fakeOpenAPIExecutor{}
	caller := &OpenAPICaller{Product: "ecs", Region: "cn-hangzhou", Profile: profile, executor: business}
	if lease, err := caller.acquireCredentialLease(context.Background()); lease != nil || !errors.Is(err, ErrCredentialAccountMismatch) {
		t.Fatalf("lease=%#v error=%v", lease, err)
	}
	if len(business.requests) != 0 {
		t.Fatalf("business requests = %#v", business.requests)
	}
}

func TestOperationGuardReusesVerifiedIdentityProof(t *testing.T) {
	snapshot := &credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token", Type: "sts"}
	fingerprint := credentialSnapshotFingerprint(snapshot)
	identity := telemetry.Identity{Hash: "hash", Type: "AssumedRoleUser"}
	snapshot.IdentityProof = &credentialIdentityProof{
		identity: identity, accountID: "1234567890123456", endpoint: "sts.aliyuncs.com", fingerprint: fingerprint,
	}
	profile := resolvedOpenAPIProfile{
		ExpectedAccountID: "1234567890123456", ExpectedIdentityType: "AssumedRoleUser",
		IdentityPolicy: credentialIdentityPolicy{},
	}
	originalResolver := resolveCredentialSnapshotIdentity
	calls := 0
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		calls++
		return telemetry.Identity{}, errors.New("unexpected identity RPC")
	}
	defer func() { resolveCredentialSnapshotIdentity = originalResolver }()
	guard, err := newOperationIdentityGuard(context.Background(), profile, snapshot)
	if err != nil || guard == nil || calls != 0 {
		t.Fatalf("guard=%#v calls=%d error=%v", guard, calls, err)
	}
}
