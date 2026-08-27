package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateSensitiveTempStartsPrivate(t *testing.T) {
	file, err := CreateSensitiveTemp(t.TempDir(), "credential-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	t.Cleanup(func() {
		_ = file.Close()
		_ = os.Remove(path)
	})
	if runtime.GOOS != "windows" {
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("sensitive temporary file mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestCleanupSensitiveTempRemovesPrivateStagingDirectory(t *testing.T) {
	file, err := CreateSensitiveTemp(t.TempDir(), "credential-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	stage := filepath.Dir(path)
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sensitive staging directory remained: %v", err)
	}
}

func TestCleanupSensitiveTempRejectsArbitraryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.json")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(path); err == nil {
		t.Fatal("arbitrary path was accepted as a sensitive temporary file")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "keep" {
		t.Fatalf("arbitrary path changed: %q, %v", raw, err)
	}
}

func TestCleanupSensitiveTempRejectsUnsafeLookalikeDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), sensitiveTempDirectoryPrefix+"lookalike")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keep.json")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(path); err == nil {
		t.Fatal("unsafe lookalike directory was accepted")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "keep" {
		t.Fatalf("lookalike path changed: %q, %v", raw, err)
	}
}
