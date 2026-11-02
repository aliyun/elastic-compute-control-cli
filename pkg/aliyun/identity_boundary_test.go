package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestExpectedIdentityRejectsInconsistentPublicIdentifiers(t *testing.T) {
	for _, pin := range []expectedCredentialIdentity{
		{Version: 1, AccountID: "1234567890123456", Type: "Account", UserID: "unexpected"},
		{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", RoleID: "unexpected"},
		{Version: 1, AccountID: "1234567890123456", Type: "AssumedRoleUser", UserID: "unexpected"},
		{Version: 1, AccountID: "1234567890123456", Type: "RAMUser", UserID: "invalid\nidentifier"},
	} {
		pin.AccountID = strings.Repeat("1", 16)
		raw, err := json.Marshal(pin)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := parseExpectedCredentialIdentity(string(raw)); !errors.Is(err, ErrCredentialIdentityChanged) {
			t.Fatalf("inconsistent pin accepted: %v", err)
		}
	}
}

func TestIdentityProofIsBoundToCredentialAccountTypeAndEndpoint(t *testing.T) {
	snapshot := &credentialSnapshot{AccessKeyID: "fake-id", AccessKeySecret: "fake-secret", SecurityToken: "fake-token"}
	identity := telemetry.Identity{Hash: "identity", Type: "RAMUser"}
	snapshot.IdentityProof = &credentialIdentityProof{identity: identity, accountID: "account", endpoint: "sts.example.invalid", fingerprint: credentialSnapshotFingerprint(snapshot)}
	if validCredentialIdentityProof(resolvedOpenAPIProfile{}, snapshot, "sts.example.invalid") == nil {
		t.Fatal("valid identity proof rejected")
	}
	for _, profile := range []resolvedOpenAPIProfile{{ExpectedAccountID: "different"}, {ExpectedIdentityType: "Account"}} {
		if validCredentialIdentityProof(profile, snapshot, "sts.example.invalid") != nil {
			t.Fatal("foreign identity proof accepted")
		}
	}
	if validCredentialIdentityProof(resolvedOpenAPIProfile{}, snapshot, "other.example.invalid") != nil {
		t.Fatal("proof accepted for foreign endpoint")
	}
	snapshot.SecurityToken = "rotated"
	if validCredentialIdentityProof(resolvedOpenAPIProfile{}, snapshot, "sts.example.invalid") != nil {
		t.Fatal("proof survived credential rotation")
	}
	if credentialSnapshotFingerprint(nil) != "" || credentialSnapshotFingerprint(&credentialSnapshot{}) != "" {
		t.Fatal("incomplete snapshot fingerprinted")
	}
	snapshot.SecurityToken = "fake-token"
	for _, guard := range []*operationIdentityGuard{{domain: "sts.example.invalid", profile: resolvedOpenAPIProfile{ExpectedIdentityHash: "different"}}, {domain: "sts.example.invalid", identity: telemetry.Identity{Hash: "different", Type: "RAMUser"}}} {
		if err := guard.Validate(context.Background(), snapshot); !errors.Is(err, ErrCredentialIdentityChanged) {
			t.Fatalf("foreign proof accepted: %v", err)
		}
	}
	var empty *operationIdentityGuard
	if err := empty.Validate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, guard := range []*operationIdentityGuard{nil, {}} {
		if _, err := guard.Resolver()(context.Background()); err == nil {
			t.Fatal("unavailable identity resolved")
		}
	}
	if got, err := (&operationIdentityGuard{identity: identity}).Resolver()(context.Background()); err != nil || got != identity {
		t.Fatalf("identity resolver=%+v, %v", got, err)
	}
}

func TestIdentityResolutionRejectsIncompleteAndChangedPrincipals(t *testing.T) {
	for _, response := range []map[string]any{{"AccountId": "account", "IdentityType": "RAMUser"}, {"AccountId": "account", "IdentityType": "AssumedRoleUser"}, {"AccountId": "account", "IdentityType": "unknown"}} {
		if _, err := canonicalIdentity(response); err == nil {
			t.Fatal("incomplete identity accepted")
		}
	}
	if _, _, err := resolveIdentityAt(context.Background(), nil, ""); err == nil {
		t.Fatal("nil executor accepted")
	}
	cause := errors.New("identity service unavailable")
	if _, _, err := resolveIdentityAt(context.Background(), &fakeOpenAPIExecutor{callError: cause}, ""); !errors.Is(err, cause) {
		t.Fatalf("identity cause lost: %v", err)
	}
	if _, err := newOperationIdentityGuard(context.Background(), resolvedOpenAPIProfile{}, nil); err == nil {
		t.Fatal("nil snapshot accepted")
	}
	if _, err := defaultResolveCredentialSnapshotIdentity(context.Background(), resolvedOpenAPIProfile{}, nil, ""); err == nil {
		t.Fatal("nil snapshot resolved")
	}
	if err := verifyCredentialAccountAt(context.Background(), nil, "", ""); err == nil {
		t.Fatal("missing expected account accepted")
	}
	if err := verifyCredentialAccountAt(context.Background(), nil, "account", ""); err == nil {
		t.Fatal("missing credential account verified")
	}
	executor := &fakeOpenAPIExecutor{response: `{"IdentityType":"Account","AccountId":"account"}`}
	if _, err := resolveCredentialIdentityWithExecutor(context.Background(), resolvedOpenAPIProfile{ExpectedIdentityType: "RAMUser"}, executor, ""); !errors.Is(err, ErrCredentialIdentityChanged) {
		t.Fatalf("identity type mismatch accepted: %v", err)
	}
}
