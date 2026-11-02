//go:build !windows

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelemetryCacheFilesystemFailuresStayLocal(t *testing.T) {
	configPath := secureConfigPath(t)
	directory, err := ensureTelemetryDirectory(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "identity-v1.lock")
	cachePath := filepath.Join(directory, "identity-v1.json")
	if err := os.WriteFile(cachePath, []byte(strings.Repeat("x", identityCacheMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openIdentityCache(context.Background(), configPath); err == nil {
		t.Fatal("oversized cache accepted")
	}
	file, err := os.Open(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lockIdentityFile(context.Background(), file); err == nil {
		t.Fatal("closed lock accepted")
	}
	if _, err := openIdentityLock(filepath.Join(directory, "missing", "lock")); err == nil {
		t.Fatal("missing lock parent accepted")
	}
	if err := writeSecureTelemetryFile(filepath.Join(directory, "missing"), cachePath, lockPath, "stage-*", nil); err == nil {
		t.Fatal("missing directory accepted")
	}
	if err := writeSecureTelemetryFile(directory, cachePath, filepath.Join(directory, "missing"), "stage-*", nil); err == nil {
		t.Fatal("missing lock accepted")
	}
	if err := os.Remove(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cachePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecureTelemetryFile(directory, cachePath, lockPath, "stage-*", nil); err == nil {
		t.Fatal("directory cache overwritten")
	}
	if err := os.Remove(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := writeSecureTelemetryFile(directory, cachePath, lockPath, "invalid/pattern-*", nil); err == nil {
		t.Fatal("invalid staging pattern accepted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "identity-v1.lock" {
		t.Fatalf("failed writes leaked files: %v", entries)
	}
	parent := filepath.Join(filepath.Dir(configPath), "blocker")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureIdentityConfigParent(filepath.Join(parent, "child", "config")); err == nil {
		t.Fatal("non-directory ancestor accepted")
	}
	if _, err := ensureIdentityConfigParent(filepath.Join(t.TempDir(), "missing", "child", "config")); err == nil {
		t.Fatal("missing ancestor accepted")
	}
}

func TestInstallationTokenRejectsUnsafeDirectoryLockAndTokenFiles(t *testing.T) {
	for _, fault := range []string{"parent", "lock", "token"} {
		t.Run(fault, func(t *testing.T) {
			config := secureConfigPath(t)
			directory, err := ensureTelemetryDirectory(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "installation-v1")
			switch fault {
			case "parent":
				config = filepath.Join(t.TempDir(), "missing", "child", "config")
			case "lock":
				path += ".lock"
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "token":
				if err := os.WriteFile(path, []byte("not private"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadOrCreateInstallationToken(context.Background(), config); err == nil {
				t.Fatal("unsafe installation state accepted")
			}
		})
	}
}
