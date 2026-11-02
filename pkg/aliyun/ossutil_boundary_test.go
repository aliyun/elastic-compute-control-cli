package aliyun

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOSSCallerRejectsCredentialFailuresBeforeLaunchingOperation(t *testing.T) {
	for _, source := range []credentialAcquirer{
		nil,
		credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) { return nil, errors.New("credential source failed") }),
		&staticCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "fake-id", AccessKeySecret: "fake-secret", ExpiresAt: time.Now().Add(-time.Hour)}},
	} {
		runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{{stdout: []byte("2.3.0")}, {}}}
		caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
		caller.Profile.Acquirer = source
		if _, err := caller.CallWithArgs(nil, "ListBuckets", nil, nil); err == nil {
			t.Fatal("invalid credential source accepted")
		}
		if len(runner.calls) != 2 {
			t.Fatalf("credential failure launched cloud operation: %d calls", len(runner.calls))
		}
	}
}

func TestOSSRuntimeProbesAndParameterValidation(t *testing.T) {
	caller := &OSSUtilCaller{runner: &fakeOSSUtilRunner{results: []ossUtilRunnerResult{{err: &exec.Error{Name: "ossutil", Err: exec.ErrNotFound}}}}}
	if err := caller.requireOperation(context.Background(), "unused", ossUtilOperationMetadata{command: "list-buckets"}, nil); appErrorCode(err) != "OSSUtilUnavailable" {
		t.Fatalf("missing runtime=%v", err)
	}
	if err := caller.requireLauncherProfile(context.Background(), nil, nil, ""); appErrorCode(err) != "OSSUtilUnavailable" {
		t.Fatalf("invalid launcher profile=%v", err)
	}
	caller.runner = &fakeOSSUtilRunner{results: []ossUtilRunnerResult{{stdout: []byte("ossutil 1.9.0")}}}
	if err := caller.requireLauncherProfile(context.Background(), []string{"--config-path", "config", "--profile", "profile"}, nil, ""); appErrorCode(err) != "OSSUtilVersionUnsupported" {
		t.Fatalf("old launcher accepted: %v", err)
	}
	for _, args := range [][]string{{"--endpoint"}, {"--endpoint", "--retry-count=1"}, {"--endpoint="}} {
		if _, err := ossUtilPassthroughArgs(args, false); appErrorCode(err) != "MissingParameter" {
			t.Fatalf("missing OSS flag value accepted: %v", args)
		}
	}
	if _, _, err := caller.commandArgs("UnknownOperation", nil, nil); appErrorCode(err) != "UnsupportedOperation" {
		t.Fatalf("unknown OSS operation=%v", err)
	}
	if _, _, err := caller.commandArgs("PutBucket", map[string]any{"Bucket": make(chan int)}, nil); appErrorCode(err) != "InvalidParameter" {
		t.Fatalf("invalid OSS API parameter=%v", err)
	}
	if _, _, err := caller.commandArgs("PutObject", map[string]any{"Bucket": make(chan int)}, nil); appErrorCode(err) != "InvalidParameter" {
		t.Fatalf("invalid transfer parameter=%v", err)
	}
	if _, _, err := caller.commandArgs("PutObject", map[string]any{"Bucket": "bucket", "Key": "key", "File": "file"}, []string{"--invalid"}); err == nil {
		t.Fatal("invalid transfer passthrough accepted")
	}
	if _, err := caller.commandEnv(nil); appErrorCode(err) != "MissingCredentials" {
		t.Fatalf("nil credentials=%v", err)
	}
	if _, err := caller.commandEnv(&credentialSnapshot{}); appErrorCode(err) != "InvalidCredentials" {
		t.Fatalf("incomplete credentials=%v", err)
	}
	caller.Profile.BearerTokenHeaderKey = "x-bearer"
	env, err := caller.commandEnv(&credentialSnapshot{BearerToken: "fake-bearer"})
	if err != nil || environmentValue(env, "ALIBABA_CLOUD_BEARER_TOKEN") != "fake-bearer" || environmentValue(env, "ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY") != "x-bearer" {
		t.Fatalf("bearer environment lost: %v", err)
	}
	if err := caller.commandError(&exec.Error{Name: "ossutil", Err: exec.ErrNotFound}, nil, nil, nil); appErrorCode(err) != "OSSUtilUnavailable" {
		t.Fatalf("runtime path failure=%v", err)
	}
	if err := caller.commandError(errors.New("exit 1"), nil, []byte("NoSuchBucket: absent"), nil); appErrorCode(err) != "NotFound" || !strings.Contains(err.Error(), "resource not found") {
		t.Fatalf("missing bucket identity=%v", err)
	}
	if isOSSBucketNotEmpty(nil) {
		t.Fatal("nil error classified as non-empty bucket")
	}
}
