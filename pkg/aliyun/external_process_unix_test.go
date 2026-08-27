//go:build !windows

package aliyun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExternalCredentialTimeoutKillsDescendantsHoldingStdout(t *testing.T) {
	script := filepath.Join(t.TempDir(), "credential-helper.sh")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n(sleep 10) &\necho $! > \"$1\"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	provider, err := newSafeExternalCredentialsProvider(strconv.Quote(script)+" "+strconv.Quote(pidFile), mapGetenv(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), externalCredentialWaitDelay+3*time.Second)
	defer cancel()
	started := time.Now()
	_, err = provider.Acquire(ctx)
	if err == nil {
		t.Fatal("external helper with no credential output succeeded")
	}
	if elapsed := time.Since(started); elapsed > externalCredentialWaitDelay+time.Second {
		t.Fatalf("external helper descendants held stdout for %s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, exec.ErrWaitDelay) {
		var providerErr *credentialProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("external helper error = %v", err)
		}
	}
	rawPID, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	deadline := time.Now().Add(time.Second)
	for {
		probeErr := syscall.Kill(pid, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("external credential grandchild %d survived WaitDelay cleanup: %v", pid, probeErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
