package aliyun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

var ErrCredentialIdentityChanged = errors.New("credential identity changed during operation")
var ErrCredentialAccountMismatch = errors.New("credential account does not match the configured account")

type operationIdentityGuard struct {
	profile resolvedOpenAPIProfile
	domain  string

	mu          sync.Mutex
	identity    telemetry.Identity
	fingerprint string
}

type credentialIdentityProof struct {
	identity    telemetry.Identity
	accountID   string
	endpoint    string
	fingerprint string
}

type credentialSnapshotIdentityResolver func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error)

var resolveCredentialSnapshotIdentity credentialSnapshotIdentityResolver

func defaultResolveCredentialSnapshotIdentity(ctx context.Context, profile resolvedOpenAPIProfile, snapshot *credentialSnapshot, domain string) (telemetry.Identity, error) {
	executor, err := newStaticDarabonbaExecutor(profile, snapshot)
	if err != nil {
		return telemetry.Identity{}, err
	}
	return resolveCredentialIdentityWithExecutor(ctx, profile, executor, domain)
}

func resolveCredentialIdentityWithExecutor(ctx context.Context, profile resolvedOpenAPIProfile, executor openAPIExecutor, domain string) (telemetry.Identity, error) {
	identity, accountID, err := resolveIdentityAt(ctx, executor, domain)
	if err != nil {
		return telemetry.Identity{}, err
	}
	if profile.ExpectedAccountID != "" && accountID != profile.ExpectedAccountID {
		return telemetry.Identity{}, ErrCredentialAccountMismatch
	}
	if profile.ExpectedIdentityType != "" && identity.Type != profile.ExpectedIdentityType {
		return telemetry.Identity{}, ErrCredentialIdentityChanged
	}
	return identity, nil
}

func newOperationIdentityGuard(ctx context.Context, profile resolvedOpenAPIProfile, snapshot *credentialSnapshot) (*operationIdentityGuard, error) {
	if snapshot == nil {
		return nil, errors.New("credential snapshot is unavailable")
	}
	domain, err := credentialIdentityEndpoint(profile, ctx)
	if err != nil {
		return nil, err
	}
	guard := &operationIdentityGuard{profile: profile, domain: domain}
	if err := guard.validateLocked(ctx, snapshot); err != nil {
		return nil, err
	}
	return guard, nil
}

func (g *operationIdentityGuard) Validate(ctx context.Context, snapshot *credentialSnapshot) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	fingerprint := credentialSnapshotFingerprint(snapshot)
	if fingerprint != "" && fingerprint == g.fingerprint {
		return nil
	}
	return g.validateLocked(ctx, snapshot)
}

func (g *operationIdentityGuard) validateLocked(ctx context.Context, snapshot *credentialSnapshot) error {
	if proof := validCredentialIdentityProof(g.profile, snapshot, g.domain); proof != nil {
		if g.identity.Hash != "" && (proof.identity.Hash != g.identity.Hash || proof.identity.Type != g.identity.Type) {
			return ErrCredentialIdentityChanged
		}
		g.identity = proof.identity
		g.fingerprint = proof.fingerprint
		return nil
	}
	resolver := resolveCredentialSnapshotIdentity
	if resolver == nil {
		resolver = defaultResolveCredentialSnapshotIdentity
	}
	identity, err := resolver(ctx, g.profile, snapshot, g.domain)
	if err != nil {
		return err
	}
	if g.identity.Hash != "" && (identity.Hash != g.identity.Hash || identity.Type != g.identity.Type) {
		return ErrCredentialIdentityChanged
	}
	g.identity = identity
	g.fingerprint = credentialSnapshotFingerprint(snapshot)
	if snapshot != nil {
		snapshot.IdentityProof = &credentialIdentityProof{
			identity: identity, accountID: g.profile.ExpectedAccountID, endpoint: g.domain, fingerprint: g.fingerprint,
		}
	}
	return nil
}

func validCredentialIdentityProof(profile resolvedOpenAPIProfile, snapshot *credentialSnapshot, endpoint string) *credentialIdentityProof {
	if snapshot == nil || snapshot.IdentityProof == nil {
		return nil
	}
	proof := snapshot.IdentityProof
	if proof.endpoint != endpoint || proof.fingerprint == "" || proof.fingerprint != credentialSnapshotFingerprint(snapshot) || proof.identity.Hash == "" || proof.identity.Type == "" {
		return nil
	}
	if profile.ExpectedAccountID != "" && proof.accountID != profile.ExpectedAccountID {
		return nil
	}
	if profile.ExpectedIdentityType != "" && proof.identity.Type != profile.ExpectedIdentityType {
		return nil
	}
	return proof
}

func (g *operationIdentityGuard) Resolver() telemetry.IdentityResolver {
	return func(context.Context) (telemetry.Identity, error) {
		if g == nil {
			return telemetry.Identity{}, errors.New("identity unavailable")
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.identity.Hash == "" || g.identity.Type == "" {
			return telemetry.Identity{}, errors.New("identity unavailable")
		}
		return g.identity, nil
	}
}

func credentialSnapshotFingerprint(snapshot *credentialSnapshot) string {
	if snapshot == nil || snapshot.AccessKeyID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(snapshot.AccessKeyID + "\x00" + snapshot.SecurityToken))
	return hex.EncodeToString(digest[:])
}

func credentialIdentityEndpoint(profile resolvedOpenAPIProfile, ctx context.Context) (string, error) {
	return profile.IdentityPolicy.endpoint(firstNonEmptyString(credentialOperationRegion(ctx), profile.RegionID))
}

func identityResolver(executor openAPIExecutor) telemetry.IdentityResolver {
	return identityResolverAt(executor, "sts.aliyuncs.com")
}

func identityResolverAt(executor openAPIExecutor, domain string) telemetry.IdentityResolver {
	return func(ctx context.Context) (telemetry.Identity, error) {
		identity, _, err := resolveIdentityAt(ctx, executor, domain)
		return identity, err
	}
}

func resolveIdentityAt(ctx context.Context, executor openAPIExecutor, domain string) (telemetry.Identity, string, error) {
	if executor == nil {
		return telemetry.Identity{}, "", errors.New("identity executor is unavailable")
	}
	req := newOpenAPIRequest()
	req.Product = "Sts"
	req.Version = "2015-04-01"
	req.ApiName = "GetCallerIdentity"
	req.Domain = firstNonEmptyString(domain, "sts.aliyuncs.com")
	req.Scheme = "https"
	req.Method = "GET"
	response, err := executor.ExecuteOpenAPI(ctx, req)
	if err != nil {
		return telemetry.Identity{}, "", err
	}
	identity, err := canonicalIdentity(response)
	if err != nil {
		return telemetry.Identity{}, "", err
	}
	accountID, _ := response["AccountId"].(string)
	return identity, accountID, nil
}

func verifyCredentialAccountAt(ctx context.Context, snapshot *credentialSnapshot, expectedAccountID, endpoint string) error {
	if expectedAccountID == "" {
		return errors.New("expected credential account is unavailable")
	}
	executor, err := newStaticDarabonbaExecutor(resolvedOpenAPIProfile{AuthType: "AK"}, snapshot)
	if err != nil {
		return err
	}
	identity, accountID, err := resolveIdentityAt(ctx, executor, endpoint)
	if err != nil {
		return err
	}
	if accountID != expectedAccountID {
		return ErrCredentialAccountMismatch
	}
	snapshot.IdentityProof = &credentialIdentityProof{
		identity: identity, accountID: accountID, endpoint: endpoint, fingerprint: credentialSnapshotFingerprint(snapshot),
	}
	return nil
}

func canonicalIdentity(response map[string]any) (telemetry.Identity, error) {
	identityType, _ := response["IdentityType"].(string)
	accountID, _ := response["AccountId"].(string)
	if accountID == "" {
		return telemetry.Identity{}, errors.New("caller account identity is incomplete")
	}
	var canonical string
	switch identityType {
	case "Account":
		canonical = "v2\x00account\x00" + accountID
	case "RAMUser":
		userID, _ := response["UserId"].(string)
		if userID == "" {
			return telemetry.Identity{}, errors.New("caller RAM user identity is incomplete")
		}
		canonical = "v2\x00ram-user\x00" + accountID + "\x00" + userID
	case "AssumedRoleUser":
		roleID, _ := response["RoleId"].(string)
		if roleID == "" {
			return telemetry.Identity{}, errors.New("caller RAM role identity is incomplete")
		}
		canonical = "v2\x00ram-role\x00" + accountID + "\x00" + roleID
	default:
		return telemetry.Identity{}, errors.New("caller identity type is unsupported")
	}
	digest := sha256.Sum256([]byte(canonical))
	return telemetry.Identity{Hash: hex.EncodeToString(digest[:]), Type: identityType}, nil
}
