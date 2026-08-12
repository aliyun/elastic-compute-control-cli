//go:build !windows

package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type identityFileInfoWithSys struct {
	os.FileInfo
	sys any
}

func (i identityFileInfoWithSys) Sys() any { return i.sys }

func TestIdentityCacheRejectsSymlinkComponentsWithoutTouchingSentinel(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string, string) string
	}{
		{
			name: "config parent",
			setup: func(t *testing.T, root, victim string) string {
				parent := filepath.Join(root, "config-link")
				if err := os.Symlink(victim, parent); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parent, "config.json")
			},
		},
		{
			name: "telemetry directory",
			setup: func(t *testing.T, root, victim string) string {
				parent := filepath.Join(root, "config")
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victim, filepath.Join(parent, "telemetry")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parent, "config.json")
			},
		},
		{
			name: "cache file",
			setup: func(t *testing.T, root, victim string) string {
				parent := filepath.Join(root, "config")
				telemetryDir := filepath.Join(parent, "telemetry")
				if err := os.MkdirAll(telemetryDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(victim, "sentinel"), filepath.Join(telemetryDir, "identity-v1.json")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parent, "config.json")
			},
		},
		{
			name: "lock file",
			setup: func(t *testing.T, root, victim string) string {
				parent := filepath.Join(root, "config")
				telemetryDir := filepath.Join(parent, "telemetry")
				if err := os.MkdirAll(telemetryDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(victim, "sentinel"), filepath.Join(telemetryDir, "identity-v1.lock")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(parent, "config.json")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			victim := filepath.Join(root, "victim")
			if err := os.Mkdir(victim, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(victim, "sentinel")
			if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			configPath := tc.setup(t, root, victim)
			calls := 0
			for i := 0; i < 2; i++ {
				if _, err := resolveCachedIdentity(context.Background(), configPath, "ak", func(context.Context) (Identity, error) {
					calls++
					return Identity{Hash: "hash", Type: "RAMUser"}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatalf("unsafe path unexpectedly cached identity; resolver calls=%d", calls)
			}
			raw, err := os.ReadFile(sentinel)
			if err != nil || string(raw) != "untouched" {
				t.Fatalf("sentinel changed: raw=%q err=%v", raw, err)
			}
		})
	}
}

func TestIdentityCacheRejectsWritableSharedDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "shared")
	if err := os.Mkdir(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(parent, "config.json")
	calls := 0
	for i := 0; i < 2; i++ {
		if _, err := resolveCachedIdentity(context.Background(), configPath, "ak", func(context.Context) (Identity, error) {
			calls++
			return Identity{Hash: "hash", Type: "RAMUser"}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("world-writable parent unexpectedly cached identity; calls=%d", calls)
	}
	if _, err := os.Lstat(filepath.Join(parent, "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("unsafe parent received telemetry directory: %v", err)
	}
}

func TestIdentityCacheCreatesOneMissingConfigParentAndCaches(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing-config-parent")
	configPath := filepath.Join(parent, "config.json")
	calls := 0
	for i := 0; i < 2; i++ {
		identity, err := resolveCachedIdentity(context.Background(), configPath, "ak", func(context.Context) (Identity, error) {
			calls++
			return Identity{Hash: "hash", Type: "RAMUser"}, nil
		})
		if err != nil || identity.Hash != "hash" {
			t.Fatalf("resolveCachedIdentity = %#v, %v", identity, err)
		}
	}
	if calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
	for _, path := range []string{parent, filepath.Join(parent, "telemetry")} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("created directory %s mode = %v, want directory 0700", path, info.Mode())
		}
	}
}

func TestIdentityCacheRejectsUnsafeOrMissingConfigAncestorWithoutWriting(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) (string, string)
	}{
		{
			name: "world-writable ancestor",
			setup: func(t *testing.T) (string, string) {
				ancestor := filepath.Join(t.TempDir(), "shared")
				if err := os.Mkdir(ancestor, 0o777); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(ancestor, 0o777); err != nil {
					t.Fatal(err)
				}
				parent := filepath.Join(ancestor, "ecctl")
				return filepath.Join(parent, "config.json"), parent
			},
		},
		{
			name: "missing ancestor",
			setup: func(t *testing.T) (string, string) {
				ancestor := filepath.Join(t.TempDir(), "missing-ancestor")
				parent := filepath.Join(ancestor, "ecctl")
				return filepath.Join(parent, "config.json"), ancestor
			},
		},
		{
			name: "symlink ancestor",
			setup: func(t *testing.T) (string, string) {
				root := t.TempDir()
				victim := filepath.Join(root, "victim")
				if err := os.Mkdir(victim, 0o700); err != nil {
					t.Fatal(err)
				}
				ancestor := filepath.Join(root, "ancestor-link")
				if err := os.Symlink(victim, ancestor); err != nil {
					t.Fatal(err)
				}
				parent := filepath.Join(ancestor, "ecctl")
				return filepath.Join(parent, "config.json"), filepath.Join(victim, "ecctl")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configPath, forbiddenPath := tc.setup(t)
			calls := 0
			for i := 0; i < 2; i++ {
				identity, err := resolveCachedIdentity(context.Background(), configPath, "ak", func(context.Context) (Identity, error) {
					calls++
					return Identity{Hash: "hash", Type: "RAMUser"}, nil
				})
				if err != nil || identity.Hash != "hash" {
					t.Fatalf("resolveCachedIdentity = %#v, %v", identity, err)
				}
			}
			if calls != 2 {
				t.Fatalf("unsafe ancestor unexpectedly cached identity; calls=%d", calls)
			}
			if _, err := os.Lstat(forbiddenPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe cache path was written: %s err=%v", forbiddenPath, err)
			}
		})
	}
}

func TestIdentityCacheRejectsDifferentOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned")
	if err := os.WriteFile(path, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("platform file info does not expose syscall.Stat_t")
	}
	other := *stat
	other.Uid = uint32(os.Geteuid() + 1)
	if err := validateIdentityOwner(identityFileInfoWithSys{FileInfo: info, sys: &other}); err == nil {
		t.Fatal("different file owner was accepted")
	}
}
