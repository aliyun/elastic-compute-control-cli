package telemetry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestActiveInstallationHashIsStableDistinctAndDoesNotExposeStoredToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables installation persistence")
	}
	configPath := secureConfigPath(t)
	first := activeInstallationHash(configPath)
	second := activeInstallationHash(configPath)
	if len(first) != 64 || first != second {
		t.Fatalf("installation hashes are not stable SHA-256 values: first=%q second=%q", first, second)
	}
	other := activeInstallationHash(secureConfigPath(t))
	if len(other) != 64 || other == first {
		t.Fatalf("independent installation hash = %q, want a distinct SHA-256 value", other)
	}

	path := filepath.Join(filepath.Dir(configPath), "telemetry", "installation-v1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), first) {
		t.Fatal("persisted installation token equals the reported hash")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("installation token mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestActiveInstallationHashConcurrentCallersShareOneValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally disables installation persistence")
	}
	configPath := secureConfigPath(t)
	const workers = 4
	start := make(chan struct{})
	results := make(chan string, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results <- activeInstallationHash(configPath)
		}()
	}
	close(start)
	group.Wait()
	close(results)
	want := ""
	for got := range results {
		if len(got) != 64 {
			t.Fatalf("installation hash = %q, want SHA-256 value", got)
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent installation hashes differ: got=%q want=%q", got, want)
		}
	}
}

func TestActiveInstallationHashFailsClosedOnCorruptState(t *testing.T) {
	configPath := secureConfigPath(t)
	directory := filepath.Join(filepath.Dir(configPath), "telemetry")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "installation-v1")
	if err := os.WriteFile(path, []byte("not-a-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := activeInstallationHash(configPath); got != "" {
		t.Fatalf("corrupt installation state produced hash %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "not-a-token\n" {
		t.Fatalf("corrupt state was overwritten: raw=%q err=%v", raw, err)
	}
}

func TestActiveInstallationHashRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path hardening test")
	}
	configPath := secureConfigPath(t)
	directory := filepath.Join(filepath.Dir(configPath), "telemetry")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "installation-v1")); err != nil {
		t.Fatal(err)
	}
	if got := activeInstallationHash(configPath); got != "" {
		t.Fatalf("symlink installation state produced hash %q", got)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "untouched" {
		t.Fatalf("symlink target changed: raw=%q err=%v", raw, err)
	}
}

func TestDisabledTelemetryDoesNotCreateInstallationState(t *testing.T) {
	configPath := secureConfigPath(t)
	ctx, session := Start(WithExporterForTest(t.Context(), nil), Options{
		Enabled: false, Surface: "public", ConfigPath: configPath,
	})
	if session != nil || FromContext(ctx) != nil {
		t.Fatal("disabled telemetry created a session")
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(configPath), "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("disabled telemetry created installation state: %v", err)
	}
}

func TestActiveInstallationHashIsOmittedOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows privacy boundary test")
	}
	configPath := secureConfigPath(t)
	if got := activeInstallationHash(configPath); got != "" {
		t.Fatalf("Windows installation hash = %q, want omitted", got)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(configPath), "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("Windows installation hash created persistent state: %v", err)
	}
}
