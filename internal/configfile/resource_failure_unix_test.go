//go:build !windows

package configfile

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateWritesFailClosedWhenProcessResourcesAreExhausted(t *testing.T) {
	for _, mode := range []string{"file-size", "descriptors"} {
		command := exec.Command(os.Args[0], "-test.run=^TestPrivateResourceFailureHelper$")
		command.Env = []string{"ECCTL_TEST_PRIVATE_FAILURE=" + mode, "PATH=" + os.Getenv("PATH")}
		if coverage := flag.Lookup("test.gocoverdir"); coverage != nil && coverage.Value.String() != "" {
			command.Args = append(command.Args, "-test.gocoverdir="+coverage.Value.String())
			command.Env = append(command.Env, "GOCOVERDIR="+coverage.Value.String())
		}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s failure containment: %v\n%s", mode, err, output)
		}
	}
}

func TestPrivateResourceFailureHelper(t *testing.T) {
	mode := os.Getenv("ECCTL_TEST_PRIVATE_FAILURE")
	if mode == "" {
		return
	}
	dir := filepath.Join(t.TempDir(), "private")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := target.BeginPrivateReplace(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	resource := unix.RLIMIT_FSIZE
	if mode == "descriptors" {
		resource = unix.RLIMIT_NOFILE
	}
	var before unix.Rlimit
	if err := unix.Getrlimit(resource, &before); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setrlimit(resource, &before); err != nil {
			t.Fatal(err)
		}
	}()
	if mode == "file-size" {
		signal.Ignore(syscall.SIGXFSZ)
		defer signal.Reset(syscall.SIGXFSZ)
	}
	limited := before
	limited.Cur = 0
	if err := unix.Setrlimit(resource, &limited); err != nil {
		t.Fatal(err)
	}
	var operations []func() error
	if mode == "file-size" {
		operations = []func() error{
			func() error { return target.AtomicWrite([]byte("replacement"), 0o600) },
			func() error { return target.AtomicWritePrivate([]byte("replacement")) },
			func() error { return prepared.Commit([]byte("replacement")) },
			func() error {
				replacement, err := target.BeginPrivateReplace(4096)
				if replacement != nil {
					_ = replacement.Abort()
				}
				return err
			},
		}
	} else {
		operations = []func() error{
			func() error { _, _, err := target.Read(); return err },
			func() error { _, _, err := target.ReadBoundedRegular(100); return err },
			func() error { return target.WithLock(context.Background(), 0, 0, nil) },
			func() error { return target.AtomicWrite([]byte("replacement"), 0o600) },
			func() error { return target.AtomicWritePrivate([]byte("replacement")) },
			func() error {
				file, err := CreateSensitiveTemp(dir, "failure-*")
				if file != nil {
					_ = file.Close()
					_ = CleanupSensitiveTemp(file.Name())
				}
				return err
			},
		}
	}
	for index, operation := range operations {
		if err := operation(); err == nil || ReplacementApplied(err) {
			t.Fatalf("operation %d accepted exhausted resources or reported a commit: %v", index, err)
		}
	}
	if err := unix.Setrlimit(resource, &before); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "original" {
		t.Fatalf("exhausted resources changed target: %q,%v", raw, err)
	}
	if _, err := os.Stat(prepared.path); errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed commit discarded reserved recovery evidence")
	}
}
