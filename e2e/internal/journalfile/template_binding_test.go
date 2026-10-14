package journalfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
)

func TestTemplateConfigBindingsFreezeAbsoluteTargets(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "source.json")
	if runtime.GOOS == "windows" {
		link = a
	} else if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	env := []string{"ECCTL_CONFIG_PATH=" + link, "ECCTL_ALIYUN_CONFIG_PATH=" + a}
	paths, err := CaptureTemplateConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	intent := TemplateCreateIntent{ConfigPaths: paths}
	bound := intent.BoundConfig(execpkg.Config{Env: env})
	realA, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatal(err)
	}
	if TemplateEnvironmentValue("ECCTL_CONFIG_PATH", bound.Env) != realA || paths["ecctl"].Requested != link || paths["ecctl"].Target != realA {
		t.Fatalf("child config path was not frozen: %+v %v", paths, bound.Env)
	}
	if err := intent.ValidateConfig(env); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(b, link); err != nil {
			t.Fatal(err)
		}
		if intent.ValidateConfig(env) == nil {
			t.Fatal("symlink target changed without failing closed")
		}
	}
	if _, err := CaptureTemplateConfig([]string{"ECCTL_CONFIG_PATH=./config.json", "ECCTL_ALIYUN_CONFIG_PATH=" + a}); err == nil {
		t.Fatal("cwd-sensitive config selector accepted")
	}
	missing := filepath.Join(dir, "missing", "nested", "config.json")
	path, err := resolveTemplateConfigPath(missing)
	realDir, dirErr := filepath.EvalSymlinks(dir)
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if err != nil || path.Requested != missing || path.Target != filepath.Join(realDir, "missing", "nested", "config.json") {
		t.Fatalf("missing source binding: %+v %v", path, err)
	}
}
