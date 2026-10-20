package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIdentityPinContainsStablePublicIdentifiersOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake uses bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "ecctl")
	body := `#!/usr/bin/env bash
echo '{"response":{"AccountId":"1234567890123456","IdentityType":"AssumedRoleUser","RoleId":"role-1","PrincipalId":"role-1:session-two","SecurityToken":"dummy-token","AccessKeyId":"dummy-key"}}'
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	expected := IdentityPin{Version: 1, AccountID: "1234567890123456", Type: "AssumedRoleUser", RoleID: "role-1"}
	actual, err := ReadIdentity(context.Background(), Config{Bin: bin, ExpectedIdentity: &expected}, "test")
	encoded, _ := json.Marshal(actual)
	if err != nil || actual != expected || strings.Contains(string(encoded), "dummy") || strings.Contains(string(encoded), "session") {
		t.Fatalf("unstable or secret identity tuple: %s %v", encoded, err)
	}
	expected.RoleID = "role-2"
	if _, err := ReadIdentity(context.Background(), Config{Bin: bin, ExpectedIdentity: &expected}, "test"); err == nil {
		t.Fatal("role switch accepted")
	}
}

func TestExecRecordsWhetherMutationProcessStarted(t *testing.T) {
	missing := Run(context.Background(), Config{Bin: filepath.Join(t.TempDir(), "missing")}, "ecctl test create")
	if missing.Started || missing.Err == nil {
		t.Fatalf("unstarted command is uncertain allocation: %+v", missing)
	}
	if runtime.GOOS == "windows" {
		return
	}
	bin := filepath.Join(t.TempDir(), "ecctl")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env bash\necho 'i/o timeout'\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := Run(context.Background(), Config{Bin: bin}, "ecctl test create")
	if !started.Started || started.Exit != 2 {
		t.Fatalf("post-start response loss not distinguished: %+v", started)
	}
}
