package aliyun

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareOperationContextPinsCredentialSourceAndReusesLease(t *testing.T) {
	for _, fakeExecutor := range []bool{false, true} {
		calls := 0
		source := credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) {
			calls++
			return &credentialSnapshot{AccessKeyID: "fake-id", AccessKeySecret: "fake-secret"}, nil
		})
		caller := &OpenAPICaller{Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Mode: credentialModeAK, AuthType: "AK", Acquirer: source}}
		if fakeExecutor {
			caller.executor = &fakeOpenAPIExecutor{}
		}
		ctx, err := caller.PrepareOperationContext(nil)
		if err != nil {
			t.Fatal(err)
		}
		lease, _ := ctx.Value(credentialLeaseContextKey{}).(*credentialLease)
		if lease == nil || lease.owner != caller || lease.executor == nil || lease.identity == nil || lease.snapshot == nil || lease.snapshot.AccessKeyID != "fake-id" {
			t.Fatalf("incomplete lease: %+v", lease)
		}
		if reused, err := caller.PrepareOperationContext(ctx); err != nil || reused != ctx || calls != 1 {
			t.Fatalf("lease reacquired: %d calls, %v", calls, err)
		}
		other := &OpenAPICaller{executor: &fakeOpenAPIExecutor{}}
		otherCtx, err := other.PrepareOperationContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if otherLease, _ := otherCtx.Value(credentialLeaseContextKey{}).(*credentialLease); otherLease == lease || otherLease.owner != other {
			t.Fatal("caller reused another caller's credential lease")
		}
	}
}

func TestPrepareOperationContextRejectsUnavailableExpiredAndFailedSources(t *testing.T) {
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("USERPROFILE", isolatedHome)
	cause := errors.New("credential source failed")
	for _, caller := range []*OpenAPICaller{
		{},
		{Profile: resolvedOpenAPIProfile{Acquirer: credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, cause })}},
		{Profile: resolvedOpenAPIProfile{Acquirer: &staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", ExpiresAt: time.Now().Add(-time.Hour)}}}},
		{Profile: resolvedOpenAPIProfile{Acquirer: &staticCredentialAcquirer{snapshot: credentialSnapshot{}}}},
	} {
		ctx, err := caller.PrepareOperationContext(context.Background())
		if err == nil {
			t.Fatal("unavailable operation credentials accepted")
		}
		if ctx.Value(credentialLeaseContextKey{}) != nil {
			t.Fatal("failed operation published a credential lease")
		}
	}
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := NewOpenAPICaller("", path, "ecs", "cn-hangzhou", mapGetenv(map[string]string{"ALIBABA_CLOUD_ACCESS_KEY_ID": "id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "secret"})); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOpenAPICaller("", path, "ecs", "", nil); err == nil {
		t.Fatal("missing default credentials accepted")
	}
	if _, err := NewOpenAPICaller("", path, "ecs", "", mapGetenv(map[string]string{expectedCredentialIdentityEnv: "invalid"})); err == nil {
		t.Fatal("invalid identity pin accepted")
	}
}
