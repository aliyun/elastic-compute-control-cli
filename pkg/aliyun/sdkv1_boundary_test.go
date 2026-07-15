package aliyun

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNoSDKV1PackagesInCompiledDependencyTree(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps -test ./... failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "github.com/aliyun/alibaba-cloud-sdk-go") {
			t.Fatalf("compiled dependency tree still includes SDK v1 package %q", line)
		}
	}
}
