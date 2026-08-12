package spec_resource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLingjunNodeGroupLoginPasswordDeclaresFileInput(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("schema", "lingjun.node-group.update")
	if code != 0 {
		t.Fatalf("schema lingjun.node-group.update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	loginPassword, _ := params["login-password"].(map[string]any)
	if loginPassword == nil || loginPassword["input"] != "text|@file" {
		t.Fatalf("login-password schema input is missing; stdout=%s", stdout)
	}

	stdout, stderr, code = runCLI("lingjun", "node-group", "update", "--help")
	if code != 0 {
		t.Fatalf("lingjun node-group update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "text or @file") {
		t.Fatalf("login-password help does not advertise @file: %s", stdout)
	}
}

func TestLingjunNodeGroupLoginPasswordReadsFile(t *testing.T) {
	t.Parallel()
	passwordPath := filepath.Join(t.TempDir(), "password.txt")
	password := "file-value\n"
	if err := os.WriteFile(passwordPath, []byte(password), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-update"},
		{"RequestId": "req-get", "NodeGroupId": "ng-123"},
	}}
	runCLI := catalogCaller(t, "lingjun", "node-group", fake)

	stdout, stderr, code := runCLI(
		"lingjun", "node-group", "update", "ng-123",
		"--region", "cn-beijing",
		"--login-password", "@"+passwordPath,
	)
	if code != 0 {
		t.Fatalf("lingjun node-group update --login-password @file exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "UpdateNodeGroup" {
		t.Fatalf("unexpected calls: count=%d", len(fake.calls))
	}
	if fake.calls[0].request["LoginPassword"] != password {
		t.Fatal("LoginPassword was not loaded from @file")
	}
}
