package aliyun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOperationCredentialSDKGettersUseConsistentSnapshots(t *testing.T) {
	snapshot := credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token"}
	acquirer := &staticCredentialAcquirer{snapshot: snapshot}
	credential, err := newOperationCredential(acquirer, credentialModeStsToken, &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	credential.SetContext(nil)
	for _, tt := range []struct {
		get  func() (*string, error)
		want string
	}{
		{credential.GetAccessKeyId, "id"}, {credential.GetAccessKeySecret, "secret"}, {credential.GetSecurityToken, "token"},
	} {
		got, err := tt.get()
		if err != nil || got == nil || *got != tt.want {
			t.Fatalf("SDK getter = %v, %v", got, err)
		}
	}
	if *credential.GetBearerToken() != "" || *credential.GetType() != "sts" {
		t.Fatal("unexpected SDK credential type")
	}
	var missing *operationCredential
	if *missing.GetType() != "" {
		t.Fatal("nil credential has a type")
	}
	for _, get := range []func() (*string, error){missing.GetAccessKeyId, missing.GetAccessKeySecret, missing.GetSecurityToken} {
		if value, err := get(); value != nil || err == nil {
			t.Fatalf("missing credential = %v, %v", value, err)
		}
	}
	if _, err := newOperationCredential(nil, "", &snapshot); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := newOperationCredential(acquirer, "", nil); err == nil {
		t.Fatal("nil seed accepted")
	}
	cause := errors.New("identity changed")
	credential, err = newOperationCredential(acquirer, credentialModeStsToken, &snapshot, func(context.Context, *credentialSnapshot) error { return cause })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credential.GetCredential(); !errors.Is(err, cause) {
		t.Fatalf("validator bypassed: %v", err)
	}
	credential.validate = nil
	credential.acquirer = credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, cause })
	if _, err := credential.GetCredential(); !errors.Is(err, cause) {
		t.Fatalf("provider cause lost: %v", err)
	}
}

func TestCredentialAcquirerContextAndLifetimeBoundaries(t *testing.T) {
	if _, err := credentialAcquirerForOperation(nil, nil); err == nil {
		t.Fatal("nil source accepted")
	}
	if credentialOperationRegion(nil) != "" || credentialOperationRegion(withCredentialOperationRegion(nil, " ")) != "" || credentialOperationRegion(withCredentialOperationRegion(nil, " cn-hangzhou ")) != "cn-hangzhou" {
		t.Fatal("operation region normalization failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&staticCredentialAcquirer{}).Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled static source = %v", err)
	}
	for _, source := range []*credentialsProviderAcquirer{nil, {}} {
		if _, err := source.Acquire(nil); err == nil {
			t.Fatal("nil provider accepted")
		}
	}
	var ecs *ecsCredentialAcquirer
	if _, err := ecs.ForOperation(nil); err == nil {
		t.Fatal("nil ECS source accepted")
	}
	if _, err := (&ecsCredentialAcquirer{disabled: true}).Acquire(nil); err == nil {
		t.Fatal("disabled metadata source acquired credentials")
	}
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	opts := credentialProviderHTTPOptions(deadline, time.Second, 0)
	if opts.ConnectTimeout != 1 || opts.ReadTimeout != 1 {
		t.Fatalf("expired timeouts = %+v", opts)
	}
	opts = credentialProviderHTTPOptions(nil, time.Nanosecond, -time.Second)
	if opts.ConnectTimeout != 1 || opts.ReadTimeout != 1 {
		t.Fatalf("minimum timeouts = %+v", opts)
	}
	if _, err := staticCredentialForSnapshot(nil); err == nil {
		t.Fatal("nil snapshot accepted")
	}
	if _, err := staticCredentialForSnapshot(&credentialSnapshot{}); err == nil {
		t.Fatal("incomplete snapshot accepted")
	}
	for _, snapshot := range []*credentialSnapshot{
		{BearerToken: "bearer"},
		{AccessKeyID: "id", AccessKeySecret: "secret", Type: "unknown"},
		{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "token", Type: "unknown"},
	} {
		credential, err := staticCredentialForSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.BearerToken != "" && *credential.GetBearerToken() != snapshot.BearerToken {
			t.Fatal("bearer token lost")
		}
	}
}

func TestCredentialErrorsRetainUsefulMessagesAndCauses(t *testing.T) {
	var reauth *OAuthReauthenticationError
	if !strings.Contains(reauth.Error(), "renewed") || !strings.Contains((&OAuthReauthenticationError{}).Error(), "renewed") || (&OAuthReauthenticationError{Reason: "expired"}).Error() != "expired" {
		t.Fatal("reauthentication error message lost")
	}
	var cacheErr *credentialCacheWriteError
	if cacheErr.Unwrap() != nil || cacheErr.Error() != "credential cache write failed" || (&credentialCacheWriteError{}).Error() != cacheErr.Error() {
		t.Fatal("nil cache error mishandled")
	}
	cause := errors.New("disk full")
	wrapped := &credentialCacheWriteError{err: cause}
	if !errors.Is(wrapped, cause) || wrapped.Error() != cause.Error() {
		t.Fatal("cache failure cause lost")
	}
}

func TestExternalCredentialOutputBufferBounds(t *testing.T) {
	buffer := &limitedCredentialBuffer{limit: 5}
	if n, err := buffer.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("first write = %d, %v", n, err)
	}
	if n, err := buffer.Write([]byte("def")); n != 0 || err == nil || buffer.String() != "abc" {
		t.Fatalf("overflow = %d, %v, %q", n, err, buffer.String())
	}
	if n, err := buffer.Write([]byte("de")); n != 2 || err != nil || buffer.String() != "abcde" {
		t.Fatalf("boundary = %d, %v", n, err)
	}
}
