//go:build !windows

package aliyun

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestExternalCredentialExpirationAndCachingContract(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("%q -test.run=^TestExternalCredentialPayloadHelper$", executable)
	for _, payload := range []string{`{"mode":"OAuth"}`, `{"mode":"StsToken","access_key_id":"id","access_key_secret":"secret"}`, `{"mode":"AK","access_key_id":"id","access_key_secret":"secret","expiration":"bad"}`, `{"mode":"AK","access_key_id":"id","access_key_secret":"secret","expiration":"2000-01-01T00:00:00Z"}`} {
		t.Setenv("ECCTL_TEST_EXTERNAL_PAYLOAD", payload)
		provider, err := newSafeExternalCredentialsProvider(command, mapGetenv(nil))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Acquire(nil); err == nil {
			t.Fatalf("invalid external payload accepted: %s", payload)
		}
	}
	expiration := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	t.Setenv("ECCTL_TEST_EXTERNAL_PAYLOAD", `{"mode":"StsToken","access_key_id":"id","access_key_secret":"secret","sts_token":"token","expiration":"`+expiration+`"}`)
	provider, err := newSafeExternalCredentialsProvider(command, mapGetenv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := provider.Acquire(nil); err != nil || snapshot.SecurityToken != "token" || snapshot.ExpiresAt.IsZero() {
		t.Fatalf("external snapshot=%+v, %v", snapshot, err)
	}
	provider.processCommand = "invalid-binary-that-must-not-run"
	if snapshot, err := provider.Acquire(nil); err != nil || snapshot.AccessKeyID != "id" {
		t.Fatalf("external cache not reused: %+v, %v", snapshot, err)
	}
	provider.cached = nil
	provider.getenv = mapGetenv(map[string]string{credentialExternalDisabledEnv: "true"})
	if _, err := provider.Acquire(nil); err == nil {
		t.Fatal("disabled external source executed")
	}
}

func TestExternalCredentialPayloadHelper(t *testing.T) {
	if payload := os.Getenv("ECCTL_TEST_EXTERNAL_PAYLOAD"); payload != "" {
		fmt.Print(payload)
		os.Exit(0)
	}
}
