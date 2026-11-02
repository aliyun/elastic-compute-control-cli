package aliyun

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
)

func TestRegionVerificationRejectsUnavailableOperationCredentials(t *testing.T) {
	for _, source := range []credentialAcquirer{nil, credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, errors.New("source failed") }), &staticCredentialAcquirer{}} {
		verifier := &RegionVerifier{profile: resolvedOpenAPIProfile{Acquirer: source}}
		if _, _, _, err := verifier.operationExecutor(context.Background()); err == nil {
			t.Fatal("unavailable region credentials accepted")
		}
		if source != nil {
			if err := verifier.VerifyContext(context.Background(), "cn-hangzhou", ""); appErrorCode(err) != ErrCodeVerificationUnavailable {
				t.Fatalf("credential failure not classified as unavailable: %v", err)
			}
		}
	}
	var absent *RegionVerifier
	if _, err := absent.knownRegionIDs(context.Background(), nil, nil, ""); appErrorCode(err) != ErrCodeVerificationUnavailable {
		t.Fatalf("unavailable region enumeration=%v", err)
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	missing := filepath.Join(root, "config.json")
	if _, err := NewRegionVerifier("", missing, nil); appErrorCode(err) != ErrCodeVerificationUnavailable {
		t.Fatalf("missing credential constructor=%v", err)
	}
	getenv := mapGetenv(map[string]string{"ECCTL_ALIYUN_CONFIG_PATH": missing, "ALIBABA_CLOUD_ACCESS_KEY_ID": "id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "secret"})
	if err := VerifyRegion("", missing, "", "", getenv); appErrorCode(err) != "MissingRegion" {
		t.Fatalf("missing region reached remote verification: %v", err)
	}
}

func TestRegionOperationPinsRenewableIdentityWithoutNetwork(t *testing.T) {
	original := resolveCredentialSnapshotIdentity
	t.Cleanup(func() { resolveCredentialSnapshotIdentity = original })
	identity := telemetry.Identity{Hash: "identity", Type: "RAMUser"}
	resolveCredentialSnapshotIdentity = func(context.Context, resolvedOpenAPIProfile, *credentialSnapshot, string) (telemetry.Identity, error) {
		return identity, nil
	}
	source := &renewableCountingCredentialAcquirer{&countingCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token"}}}
	verifier := &RegionVerifier{profile: resolvedOpenAPIProfile{Acquirer: source, PinCredentialIdentity: true, RegionID: "cn-hangzhou", AuthType: "AK"}}
	executor, resolver, _, err := verifier.operationExecutor(context.Background())
	if err != nil || executor == nil || resolver == nil {
		t.Fatalf("renewable operation initialization=%v", err)
	}
	if got, err := resolver(context.Background()); err != nil || got != identity {
		t.Fatalf("pinned identity=%+v,%v", got, err)
	}
	verifier.profile.RegionID = ""
	verifier.profile.IdentityPolicy = credentialIdentityPolicy{enableVPC: true}
	if _, _, _, err := verifier.operationExecutor(context.Background()); err == nil {
		t.Fatal("missing VPC STS region accepted")
	}
	fake := &fakeOpenAPIExecutor{response: `{"ok":true}`}
	if _, err := verifier.executeWith(nil, fake, nil, "", newOpenAPIRequest()); err != nil {
		t.Fatalf("nil context not normalized: %v", err)
	}
}

func TestRegionMetadataIgnoresMalformedEntriesAndBoundsSuggestions(t *testing.T) {
	if parseRegionIDsFromDescribeRegions(nil) != nil || locationEndpointEntries(nil) != nil {
		t.Fatal("nil response produced region metadata")
	}
	response := map[string]any{"Regions": map[string]any{"Region": []any{true, map[string]any{"RegionId": "cn-hangzhou"}}}}
	if got := parseRegionIDsFromDescribeRegions(response); len(got) != 1 || got[0] != "cn-hangzhou" {
		t.Fatalf("malformed region entries retained: %v", got)
	}
	if !locationResponseHasEndpoints(map[string]any{"Endpoints": []any{nil, map[string]any{"Endpoint": "example.invalid"}}}, "cn-hangzhou") {
		t.Fatal("nonempty endpoints without region ID rejected")
	}
	for _, test := range []struct {
		input string
		want  int
	}{{"short", 1}, {"12345678", 2}, {"123456789012345", 5}} {
		if got := regionFuzzyThreshold(test.input, ""); got != test.want {
			t.Fatalf("suggestion threshold=%d", got)
		}
	}
}
