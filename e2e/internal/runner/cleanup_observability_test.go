package runner

import (
	"strings"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
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
		"security group dependency": {Exit: 2, Stdout: `{"actions":[{"code":"403, There is still instance(s) in the specified security group."}],"error":{"code":"CloudAPIError"}}`},
		"vpc dependency":            {Exit: 2, Stdout: `{"actions":[{"code":"400, Specified object has dependent resources"}],"error":{"code":"DependencyViolation"}}`},
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
