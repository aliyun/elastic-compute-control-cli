package releaseartifact

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	maxSignedUpdateMetadataBytes   = 1 << 20
	githubActionsOIDCIssuer        = "https://token.actions.githubusercontent.com"
	releaseRepositoryURI           = "https://github.com/aliyun/elastic-compute-control-cli"
	semverNumericIdentifierPattern = `(?:0|[1-9][0-9]*)`
	semverPrereleaseIDPattern      = `(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)`
	releaseWorkflowIdentityPattern = `^https://github\.com/aliyun/elastic-compute-control-cli/\.github/workflows/release\.yml@refs/(?:heads/main|tags/v` +
		semverNumericIdentifierPattern + `\.` + semverNumericIdentifierPattern + `\.` + semverNumericIdentifierPattern +
		`(?:-` + semverPrereleaseIDPattern + `(?:\.` + semverPrereleaseIDPattern + `)*)?)$`
	// Provenance: sigstore/root-signing@6fd910368de4a075ad621bc81d1cdda82582da2a,
	// targets/trusted_root.json, SHA-256 6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66.
	embeddedTrustedRootVersion = "root-signing-6fd910368de4a075ad621bc81d1cdda82582da2a"
	embeddedTrustedRootSHA256  = "6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66"
)

//go:embed trusted_root.json
var embeddedTrustedRootJSON []byte

var (
	defaultTrustedRootOnce sync.Once
	defaultTrustedRoot     root.TrustedMaterial
	defaultTrustedRootErr  error
)

// VerifySignedUpdateManifest exercises the production offline trust policy and
// then validates the signed manifest protocol. Signature verification must
// happen before parsing fields from the untrusted manifest.
func VerifySignedUpdateManifest(manifestRaw, bundleRaw []byte) error {
	if len(manifestRaw) == 0 || len(manifestRaw) > maxSignedUpdateMetadataBytes {
		return fmt.Errorf("update manifest must be between 1 and %d bytes", maxSignedUpdateMetadataBytes)
	}
	if len(bundleRaw) == 0 || len(bundleRaw) > maxSignedUpdateMetadataBytes {
		return fmt.Errorf("Sigstore bundle must be between 1 and %d bytes", maxSignedUpdateMetadataBytes)
	}
	if err := verifyDefaultManifest(manifestRaw, bundleRaw); err != nil {
		return err
	}
	if _, err := ParseUpdateManifestV2(manifestRaw); err != nil {
		return fmt.Errorf("validate signed update manifest: %w", err)
	}
	return nil
}

func verifyDefaultManifest(manifestRaw, bundleRaw []byte) error {
	defaultTrustedRootOnce.Do(func() {
		defaultTrustedRoot, defaultTrustedRootErr = root.NewTrustedRootFromJSON(embeddedTrustedRootJSON)
	})
	if defaultTrustedRootErr != nil {
		return fmt.Errorf("load embedded Sigstore trusted root %s: %w", embeddedTrustedRootVersion, defaultTrustedRootErr)
	}
	entity := &bundle.Bundle{}
	if err := entity.UnmarshalJSON(bundleRaw); err != nil {
		return fmt.Errorf("decode standardized Sigstore bundle: %w", err)
	}
	digest := sha256.Sum256(manifestRaw)
	return verifySigstoreEntity(
		entity,
		defaultTrustedRoot,
		digest[:],
		githubActionsOIDCIssuer,
		"",
		releaseWorkflowIdentityPattern,
		certificate.Extensions{
			SourceRepositoryURI: releaseRepositoryURI,
			RunnerEnvironment:   "github-hosted",
		},
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
}

func verifySigstoreEntity(
	entity verify.SignedEntity,
	trustedMaterial root.TrustedMaterial,
	digest []byte,
	issuer string,
	identity string,
	identityRegex string,
	extensions certificate.Extensions,
	options ...verify.VerifierOption,
) error {
	if len(digest) != sha256.Size {
		return fmt.Errorf("manifest digest must contain %d bytes", sha256.Size)
	}
	if !entity.HasInclusionProof() {
		return errors.New("Sigstore bundle must contain a transparency log inclusion proof")
	}
	certificateIdentity, err := verify.NewShortCertificateIdentity(issuer, "", identity, identityRegex)
	if err != nil {
		return fmt.Errorf("configure Sigstore certificate identity: %w", err)
	}
	certificateIdentity.Extensions = extensions
	verifier, err := verify.NewVerifier(trustedMaterial, options...)
	if err != nil {
		return fmt.Errorf("configure Sigstore verifier: %w", err)
	}
	if _, err := verifier.Verify(entity, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(certificateIdentity),
	)); err != nil {
		return fmt.Errorf("verify Sigstore manifest signature and identity: %w", err)
	}
	return nil
}
