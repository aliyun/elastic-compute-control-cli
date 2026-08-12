package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestCleanupFailurePreservesRedactedOutputAndCloudCode(t *testing.T) {
	const secret = "cleanup-secret-value"
	result := execpkg.Result{
		Exit:   2,
		Stdout: `{"actions":[{"action_name":"DeleteInstance","code":"IncorrectInstanceStatus.Initializing","message":"token=` + secret + `"}],"error":{"code":"CloudAPIError","message":"delete failed"}}`,
		Stderr: "access_key_secret=" + secret,
	}

	got := cleanupFailure(result, "ecctl ecs instance delete i-test --force", false, cleanupTimeout)
	for _, wanted := range []string{"exit=2", "cloud_code=CloudAPIError", "action_code=IncorrectInstanceStatus.Initializing", "stdout=", "stderr=", "***"} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("cleanup failure %q does not contain %q", got, wanted)
		}
	}
	if strings.Contains(got, secret) {
		t.Fatalf("cleanup failure leaked secret: %q", got)
	}
}

func TestCleanupFailureIdentifiesTimeout(t *testing.T) {
	got := cleanupFailure(execpkg.Result{Exit: -1}, "ecctl vpc delete vpc-test", true, cleanupTimeout)
	if !strings.Contains(got, "timeout=") {
		t.Fatalf("cleanup timeout detail missing: %q", got)
	}
}

func TestCleanupCommandTimeoutAllowsEcctlWaiterToFinish(t *testing.T) {
	for name, tt := range map[string]struct {
		command string
		want    time.Duration
	}{
		"default":       {command: "ecctl vpc delete vpc-test", want: cleanupTimeout},
		"short waiter":  {command: "ecctl vpc delete vpc-test --timeout 5m", want: cleanupTimeout},
		"long waiter":   {command: "ecctl ack nodepool delete np-test --timeout 30m", want: 31 * time.Minute},
		"equals syntax": {command: "ecctl ack delete c-test --timeout=60m", want: 61 * time.Minute},
		"invalid":       {command: "ecctl ack delete c-test --timeout forever", want: cleanupTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			if got := cleanupCommandTimeout(tt.command); got != tt.want {
				t.Fatalf("cleanupCommandTimeout(%q) = %s, want %s", tt.command, got, tt.want)
			}
		})
	}
}

func TestCleanupRetryableOnlyForTransientResourceStatus(t *testing.T) {
	for name, transient := range map[string]execpkg.Result{
		"instance status":           {Exit: 2, Stdout: `{"actions":[{"code":"403, The specified instance status does not support this operation."}],"error":{"code":"CloudAPIError"}}`},
		"cluster updating":          {Exit: 2, Stdout: `{"actions":[{"code":"400, cannot operate cluster where state is updating"}],"error":{"code":"CloudAPIError"}}`},
		"security group dependency": {Exit: 2, Stdout: `{"actions":[{"code":"403, There is still instance(s) in the specified security group."}],"error":{"code":"CloudAPIError"}}`},
		"vpc dependency":            {Exit: 2, Stdout: `{"actions":[{"code":"400, Specified object has dependent resources"}],"error":{"code":"DependencyViolation"}}`},
		"charge-type token pending": {Exit: 2, Stdout: `{"actions":[{"code":"403, The last token request is processing."}],"error":{"code":"CloudAPIError"}}`},
	} {
		if !cleanupRetryable(transient) {
			t.Fatalf("temporary %s must be retried", name)
		}
	}
	permanent := execpkg.Result{Exit: 2, Stdout: `{"actions":[{"code":"400, The input parameter regionId is mandatory."}],"error":{"code":"CloudAPIError"}}`}
	if cleanupRetryable(permanent) {
		t.Fatal("parameter errors must not be retried")
	}
}

func TestCleanupUsesOperationLocksAndParallelLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	for _, test := range []struct {
		name        string
		parallel    int
		holderKeys  []string
		cleanupKeys []string
	}{
		{name: "same concrete lock", parallel: 2, holderKeys: []string{"shared"}, cleanupKeys: []string{"shared"}},
		{name: "parallel one", parallel: 1, holderKeys: []string{"holder"}, cleanupKeys: []string{"cleanup"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := filepath.Join(dir, "ecctl")
			if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho cleanup >> \"$FAKE_LOG\"\necho '{}'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "calls.log")
			t.Setenv("FAKE_LOG", logPath)
			operations := newOperationRuntime(test.parallel)
			holderEntered := make(chan struct{})
			releaseHolder := make(chan struct{})
			holderDone := make(chan error, 1)
			go func() {
				holderDone <- operations.executeKeys(context.Background(), test.holderKeys, func() {
					close(holderEntered)
					<-releaseHolder
				})
			}()
			<-holderEntered

			cl := newCleanup(
				map[string]execpkg.Config{"primary": {Bin: fake, Region: "cn-test"}},
				operations, false, "", report.CleanupJournal{}, func(string, ...any) {},
			)
			var scope []*cleanupItem
			if err := cl.push(&scope, "case", "ecctl test delete", "primary", test.cleanupKeys); err != nil {
				t.Fatal(err)
			}
			cleanupDone := make(chan []string, 1)
			go func() { cleanupDone <- cl.run(scope) }()
			select {
			case failures := <-cleanupDone:
				close(releaseHolder)
				t.Fatalf("cleanup bypassed operation coordination: %v", failures)
			case <-time.After(40 * time.Millisecond):
			}
			if _, err := os.Stat(logPath); err == nil {
				close(releaseHolder)
				t.Fatal("cleanup command ran while coordinated operation was active")
			}
			close(releaseHolder)
			if err := <-holderDone; err != nil {
				t.Fatal(err)
			}
			if failures := <-cleanupDone; len(failures) != 0 {
				t.Fatalf("cleanup failures = %v", failures)
			}
		})
	}
}

func TestCleanupWaitDoesNotConsumeCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	oldTimeout := cleanupTimeout
	cleanupTimeout = time.Second
	defer func() { cleanupTimeout = oldTimeout }()

	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_LOG\"\necho '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", logPath)
	operations := newOperationRuntime(2)
	holderEntered := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- operations.executeKeys(context.Background(), []string{"shared"}, func() {
			close(holderEntered)
			<-releaseHolder
		})
	}()
	<-holderEntered

	cl := newCleanup(
		map[string]execpkg.Config{"primary": {Bin: fake, Region: "cn-test"}},
		operations, false, "", report.CleanupJournal{}, func(string, ...any) {},
	)
	var scope []*cleanupItem
	if err := cl.push(&scope, "case", "ecctl test delete", "primary", []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	cleanupDone := make(chan []string, 1)
	go func() { cleanupDone <- cl.run(scope) }()
	select {
	case failures := <-cleanupDone:
		close(releaseHolder)
		t.Fatalf("cleanup wait consumed command timeout: %#v", failures)
	case <-time.After(1250 * time.Millisecond):
	}
	if data, err := os.ReadFile(logPath); err == nil {
		if strings.Contains(string(data), "test delete") {
			close(releaseHolder)
			t.Fatalf("delete ran before the resource lock was released:\n%s", data)
		}
	} else if !os.IsNotExist(err) {
		close(releaseHolder)
		t.Fatal(err)
	}
	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	select {
	case failures := <-cleanupDone:
		if len(failures) != 0 {
			t.Fatalf("cleanup failures = %#v", failures)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not run after the resource lock was released")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test delete") {
		t.Fatalf("delete did not run after the resource lock was released:\n%s", data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := operations.executeKeys(ctx, []string{"shared"}, func() {}); err != nil {
		t.Fatalf("cleanup leaked lock after command completion: %v", err)
	}
}
