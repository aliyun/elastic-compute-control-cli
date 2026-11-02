package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
)

func TestCredentialAcquirerAdaptersRejectFailedAndExpiredSources(t *testing.T) {
	cause := errors.New("source failed")
	failed := credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, cause })
	expired := &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", ExpiresAt: time.Now().Add(-time.Second)}}
	for _, source := range []credentialAcquirer{failed, expired} {
		adapter := &credentialAcquirerProvider{acquirer: source}
		if _, err := adapter.GetCredentials(); err == nil {
			t.Fatal("source adapter ignored failure")
		}
		role := &ramRoleCredentialAcquirer{source: source}
		if _, err := role.Acquire(context.Background()); err == nil {
			t.Fatal("RAM role ignored failed source")
		}
	}
	for _, adapter := range []*credentialAcquirerProvider{nil, {}} {
		if _, err := adapter.GetCredentials(); err == nil {
			t.Fatal("unavailable provider accepted")
		}
		if adapter.GetProviderName() != "ecctl_source" {
			t.Fatal("default source name lost")
		}
	}
	if (&credentialAcquirerProvider{name: "custom"}).GetProviderName() != "custom" {
		t.Fatal("custom source name lost")
	}
	var role *ramRoleCredentialAcquirer
	if _, err := role.ForOperation(nil); err == nil {
		t.Fatal("nil RAM role cloned")
	}
	if _, err := (&ramRoleCredentialAcquirer{}).ForOperation(nil); err == nil {
		t.Fatal("RAM role without source cloned")
	}
	var oidc *oidcCredentialAcquirer
	if _, err := oidc.ForOperation(nil); err == nil {
		t.Fatal("nil OIDC source cloned")
	}
	for _, initial := range []*credentialSnapshot{{AccessKeyID: "id", AccessKeySecret: "secret", ExpiresAt: time.Now().Add(-time.Second)}, {AccessKeyID: "id"}} {
		a := &staticCredentialAcquirer{snapshot: *initial}
		credential, err := newOperationCredential(a, credentialModeAK, initial)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := credential.GetCredential(); err == nil {
			t.Fatal("invalid operation credential returned to SDK")
		}
	}
	if _, err := snapshotFromProviderCredentials(nil); err == nil {
		t.Fatal("nil provider snapshot accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&ramRoleCredentialAcquirer{source: failed}).Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled role=%v", err)
	}
	if _, err := (&oidcCredentialAcquirer{}).Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled OIDC=%v", err)
	}
}

func TestECSAcquirerCachesSnapshotsAndReturnsIndependentCopies(t *testing.T) {
	calls := 0
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	client := credentialHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == http.MethodPut {
			return metadataHTTPResponse(200, "token"), nil
		}
		return metadataHTTPResponse(200, `{"Code":"Success","AccessKeyId":"id","AccessKeySecret":"secret","SecurityToken":"token","Expiration":"`+expiration+`"}`), nil
	})
	source := &ecsCredentialAcquirer{roleName: "role", client: client, disableIMDSv1: true}
	first, err := source.Acquire(nil)
	if err != nil {
		t.Fatal(err)
	}
	first.AccessKeyID = "modified"
	second, err := source.Acquire(nil)
	if err != nil || second.AccessKeyID != "id" || calls != 2 {
		t.Fatalf("cached snapshot=%+v calls=%d error=%v", second, calls, err)
	}
	source.cached = nil
	source.client = credentialHTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") })
	if _, err := source.Acquire(context.Background()); err == nil {
		t.Fatal("metadata source failure ignored")
	}
}

func TestCredentialNumericConfigFormsAndOptionalValues(t *testing.T) {
	for _, value := range []any{int(5), float64(5), json.Number("5")} {
		if got := intMapField(map[string]any{"value": value}, "value"); got != 5 {
			t.Fatalf("integer conversion(%#v)=%d", value, got)
		}
		if got := int64MapField(map[string]any{"value": value}, "value"); got != 5 {
			t.Fatalf("int64 conversion(%#v)=%d", value, got)
		}
	}
	if !boolMapField(map[string]any{"value": "TRUE"}, "value") {
		t.Fatal("string boolean lost")
	}
	if pointerString(nil) != "" || pointerString(stringPointer("value")) != "value" || *intPointer(5) != 5 || !*boolPointer(true) {
		t.Fatal("optional SDK values changed")
	}
	if contextError(nil) != nil || credentialEnvironmentValue(nil, "key") != "" {
		t.Fatal("absent context or environment produced a value")
	}
	var recovery *credentialRecoveryError
	if recovery.Unwrap() != nil || recovery.RecoveryCommand() != nil || recovery.Error() != "credential recovery is required" {
		t.Fatal("nil recovery error mishandled")
	}
	if (&credentialRecoveryError{}).Error() != recovery.Error() {
		t.Fatal("empty recovery error mishandled")
	}
	if credentialResolutionError(nil) != nil || appErrorCode(credentialResolutionError(ErrCredentialProfileChanged)) != "CredentialIdentityChanged" {
		t.Fatal("credential source conflict classification changed")
	}
	if _, err := snapshotFromProviderCredentials(&credentialproviders.Credentials{AccessKeyId: "id"}); err == nil {
		t.Fatal("partial credential accepted")
	}
}
