package releaseartifact

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

func TestReleaseWorkflowIdentityPatternIsNarrow(t *testing.T) {
	pattern := regexp.MustCompile(releaseWorkflowIdentityPattern)
	for _, identity := range []string{
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/heads/main",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/tags/v1.2.3",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/tags/v1.2.4-rc.1",
	} {
		if !pattern.MatchString(identity) {
			t.Fatalf("trusted release identity %q does not match", identity)
		}
	}
	for _, identity := range []string{
		"https://github.com/attacker/elastic-compute-control-cli/.github/workflows/release.yml@refs/heads/main",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/other.yml@refs/heads/main",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/heads/feature",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/tags/latest",
		"https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/tags/v1.2.3-01",
		"prefix-https://github.com/aliyun/elastic-compute-control-cli/.github/workflows/release.yml@refs/heads/main",
	} {
		if pattern.MatchString(identity) {
			t.Fatalf("untrusted release identity %q matches", identity)
		}
	}
}

func TestEmbeddedSigstoreTrustedRootSnapshot(t *testing.T) {
	digest := sha256.Sum256(embeddedTrustedRootJSON)
	if got := hex.EncodeToString(digest[:]); got != embeddedTrustedRootSHA256 {
		t.Fatalf("embedded trusted root digest = %s, want %s", got, embeddedTrustedRootSHA256)
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(embeddedTrustedRootJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(trustedRoot.RekorLogs()) < 2 || len(trustedRoot.CTLogs()) < 2 || len(trustedRoot.FulcioCertificateAuthorities()) < 2 {
		t.Fatalf("embedded trusted root %s lacks rotation overlap", embeddedTrustedRootVersion)
	}
}

func TestVerifySigstoreEntityRequiresArtifactAndIdentity(t *testing.T) {
	const (
		issuer   = "http://oidc.local:8080"
		identity = "foo!oidc.local"
	)
	rootJSON, err := os.ReadFile("testdata/sigstore/scaffolding-trusted-root.json")
	if err != nil {
		t.Fatal(err)
	}
	trustedRoot, err := root.NewTrustedRootFromJSON(rootJSON)
	if err != nil {
		t.Fatal(err)
	}
	bundleJSON, err := os.ReadFile("testdata/sigstore/othername.sigstore.json")
	if err != nil {
		t.Fatal(err)
	}
	entity := &bundle.Bundle{}
	if err := entity.UnmarshalJSON(bundleJSON); err != nil {
		t.Fatal(err)
	}
	digest, err := hex.DecodeString("bc103b4a84971ef6459b294a2b98568a2bfb72cded09d4acd1e16366a401f95b")
	if err != nil {
		t.Fatal(err)
	}
	options := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithIntegratedTimestamps(1)}
	if err := verifySigstoreEntity(entity, trustedRoot, digest, issuer, identity, "", certificate.Extensions{}, options...); err != nil {
		t.Fatalf("valid signed manifest: %v", err)
	}
	var promiseOnlyDocument map[string]any
	if err := json.Unmarshal(bundleJSON, &promiseOnlyDocument); err != nil {
		t.Fatal(err)
	}
	tlogEntry := promiseOnlyDocument["verificationMaterial"].(map[string]any)["tlogEntries"].([]any)[0].(map[string]any)
	delete(tlogEntry, "inclusionProof")
	promiseOnlyJSON, err := json.Marshal(promiseOnlyDocument)
	if err != nil {
		t.Fatal(err)
	}
	promiseOnlyEntity := &bundle.Bundle{}
	if err := promiseOnlyEntity.UnmarshalJSON(promiseOnlyJSON); err == nil {
		if promiseOnlyEntity.HasInclusionProof() {
			t.Fatal("promise-only fixture still reports an inclusion proof")
		}
		if err := verifySigstoreEntity(promiseOnlyEntity, trustedRoot, digest, issuer, identity, "", certificate.Extensions{}, options...); err == nil {
			t.Fatal("Sigstore bundle with only an inclusion promise was accepted")
		}
	}
	var tamperedDocument map[string]any
	if err := json.Unmarshal(bundleJSON, &tamperedDocument); err != nil {
		t.Fatal(err)
	}
	tamperedDocument["messageSignature"].(map[string]any)["signature"] = base64.StdEncoding.EncodeToString(make([]byte, 64))
	tamperedJSON, err := json.Marshal(tamperedDocument)
	if err != nil {
		t.Fatal(err)
	}
	tamperedEntity := &bundle.Bundle{}
	if err := tamperedEntity.UnmarshalJSON(tamperedJSON); err != nil {
		t.Fatal(err)
	}
	if err := verifySigstoreEntity(tamperedEntity, trustedRoot, digest, issuer, identity, "", certificate.Extensions{}, options...); err == nil {
		t.Fatal("tampered Sigstore bundle was accepted")
	}

	badDigest := append([]byte(nil), digest...)
	badDigest[0] ^= 0xff
	for name, test := range map[string]struct {
		digest   []byte
		issuer   string
		identity string
	}{
		"manifest": {digest: badDigest, issuer: issuer, identity: identity},
		"issuer":   {digest: digest, issuer: "https://attacker.invalid", identity: identity},
		"identity": {digest: digest, issuer: issuer, identity: "attacker!oidc.local"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifySigstoreEntity(entity, trustedRoot, test.digest, test.issuer, test.identity, "", certificate.Extensions{}, options...); err == nil {
				t.Fatal("tampered or untrusted signed manifest was accepted")
			}
		})
	}
}
