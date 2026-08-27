//go:build windows

package aliyun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestExternalCredentialTimeoutKillsDescendantsHoldingStdoutWindows(t *testing.T) {
	if os.Getenv("ECCTL_EXTERNAL_GRANDCHILD") == "1" {
		if err := os.WriteFile(os.Getenv("ECCTL_EXTERNAL_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		return
	}
	if os.Getenv("ECCTL_EXTERNAL_CHILD") == "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestExternalCredentialTimeoutKillsDescendantsHoldingStdoutWindows$")
		command.Env = append(os.Environ(), "ECCTL_EXTERNAL_CHILD=", "ECCTL_EXTERNAL_GRANDCHILD=1")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("ECCTL_EXTERNAL_CHILD", "1")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	t.Setenv("ECCTL_EXTERNAL_PID_FILE", pidFile)
	provider, err := newSafeExternalCredentialsProvider(strconv.Quote(os.Args[0])+" -test.run=^TestExternalCredentialTimeoutKillsDescendantsHoldingStdoutWindows$", mapGetenv(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), externalCredentialWaitDelay+3*time.Second)
	defer cancel()
	started := time.Now()
	if _, err := provider.Acquire(ctx); err == nil {
		t.Fatal("external helper with no credential output succeeded")
	}
	if elapsed := time.Since(started); elapsed > externalCredentialWaitDelay+2*time.Second {
		t.Fatalf("Windows helper descendants held stdout for %s", elapsed)
	}
	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if openErr != nil {
			break
		}
		_ = windows.CloseHandle(handle)
		if time.Now().After(deadline) {
			t.Fatalf("Windows external credential grandchild %d survived job cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
