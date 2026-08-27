//go:build darwin

package aliyun

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/configfile"
)

func inheritedDarwinTempRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,write,execute,file_inherit,directory_inherit", dir).CombinedOutput(); err != nil {
		t.Skipf("filesystem does not support Darwin ACLs: %v: %s", err, output)
	}
	t.Setenv("TMPDIR", dir)
	return dir
}

func TestCLICommandTempConfigIsPrivateUnderInheritedDarwinACL(t *testing.T) {
	root := inheritedDarwinTempRoot(t)
	path, cleanup, err := writeTempConfig(map[string]any{
		"current":  "default",
		"profiles": []any{map[string]any{"name": "default", "access_key_secret": "sentinel"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Dir(filepath.Dir(path)) != root {
		t.Fatalf("temp config root = %q, want %q", filepath.Dir(filepath.Dir(path)), root)
	}
	if err := configfile.ValidatePrivateFile(path); err != nil {
		t.Fatalf("CLI temp config is not private: %v", err)
	}
}

func TestCredentialBrokerProfileIsPrivateUnderInheritedDarwinACL(t *testing.T) {
	root := inheritedDarwinTempRoot(t)
	broker, err := startCredentialBroker(context.Background(), credentialAcquirerFunc(func(context.Context) (*credentialSnapshot, error) {
		return &credentialSnapshot{AccessKeyID: "id", AccessKeySecret: "secret", SecurityToken: "sts", Type: "sts"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	args, err := broker.CommandArgs([]string{"ossutil", "version"}, "cn-hangzhou")
	if err != nil {
		_ = broker.Close()
		t.Fatal(err)
	}
	path := args[1]
	if filepath.Dir(filepath.Dir(path)) != root {
		t.Fatalf("broker config root = %q, want %q", filepath.Dir(filepath.Dir(path)), root)
	}
	if err := configfile.ValidatePrivateFile(path); err != nil {
		_ = broker.Close()
		t.Fatalf("broker temp config is not private: %v", err)
	}
	if err := broker.Close(); err != nil {
		t.Fatal(err)
	}
}
