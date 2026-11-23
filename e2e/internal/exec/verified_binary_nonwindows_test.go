//go:build !windows

package exec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withoutExplicitVerifiedExecRoot(t *testing.T) {
	t.Helper()
	const key = "ECCTL_E2E_VERIFIED_EXEC_DIR"
	previous, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		if present {
			err = os.Setenv(key, previous)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Errorf("restore %s: %v", key, err)
		}
	})
}

func TestVerifiedExecutableCopyRequiresStablePrivateTempCustody(t *testing.T) {
	withoutExplicitVerifiedExecRoot(t)
	base := t.TempDir()
	bin := filepath.Join(base, "ecctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '{\"ok\":true}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	bin, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("#!/bin/sh\necho '{\"ok\":true}'\n"))
	digest := hex.EncodeToString(sum[:])
	unsafe := filepath.Join(base, "insecure-temp")
	if err := os.Mkdir(unsafe, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", unsafe)
	if result := Run(context.Background(), Config{Bin: bin, ExpectedBinSHA256: digest}, "ecctl capabilities"); result.Started || result.Err == nil || result.VerifiedBinSHA256 != "" {
		t.Fatalf("untrusted temporary parent launched verified bytes: %+v", result)
	}
	secure := filepath.Join(base, "private-temp")
	if err := os.Mkdir(secure, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", secure)
	result := Run(context.Background(), Config{Bin: bin, ExpectedBinSHA256: digest}, "ecctl capabilities")
	if result.Err != nil || result.Exit != 0 || !result.Started || result.VerifiedBinSHA256 != digest {
		t.Fatalf("owner-controlled temporary parent lost verified execution: %+v", result)
	}
}

func TestVerifiedExecutableCopyUsesExplicitPrivateStagingRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("#!/bin/sh\necho '{\"ok\":true}'\n")
	bin := filepath.Join(base, "ecctl")
	if err := os.WriteFile(bin, body, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	unsafeTemp := filepath.Join(base, "unsafe-temp")
	if err := os.Mkdir(unsafeTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeTemp, 0o777); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(base, "private-exec")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", unsafeTemp)
	t.Setenv("ECCTL_E2E_VERIFIED_EXEC_DIR", staging)
	copyPath, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatalf("explicit private staging failed: %v", err)
	}
	defer release()
	child := filepath.Dir(copyPath)
	if filepath.Dir(child) != staging {
		t.Fatalf("verified copy escaped configured staging root: %s", copyPath)
	}
	if info, err := os.Stat(child); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("verified child is not private: %v %v", info, err)
	}
	if info, err := os.Stat(copyPath); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("verified executable lost private mode: %v %v", info, err)
	}
	output, err := osexec.Command(copyPath, "capabilities").Output()
	if err != nil || strings.TrimSpace(string(output)) != `{"ok":true}` {
		t.Fatalf("verified copy did not execute from configured root: %q %v", output, err)
	}
	release()
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Fatalf("verified child survived release: %v", err)
	}
}

func TestVerifiedExecutableCopyRejectsInvalidExplicitRootWithoutFallback(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("#!/bin/sh\necho '{}'\n")
	bin := filepath.Join(base, "ecctl")
	if err := os.WriteFile(bin, body, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	safeTemp := filepath.Join(base, "safe-temp")
	private := filepath.Join(base, "private-exec")
	broad := filepath.Join(base, "broad-exec")
	linked := filepath.Join(base, "linked-exec")
	unsafeParent := filepath.Join(base, "unsafe-parent")
	for _, dir := range []string{safeTemp, private, broad, unsafeParent} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(broad, 0o777); err != nil {
		t.Fatal(err)
	}
	privateUnderUnsafeParent := filepath.Join(unsafeParent, "private-exec")
	if err := os.Mkdir(privateUnderUnsafeParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", safeTemp)
	for _, tc := range []struct{ name, value string }{
		{"empty", ""},
		{"relative", "relative-exec"},
		{"noncanonical", private + "/."},
		{"broad", broad},
		{"unsafe-parent", privateUnderUnsafeParent},
		{"symlink", linked},
		{"missing", filepath.Join(base, "missing-exec")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ECCTL_E2E_VERIFIED_EXEC_DIR", tc.value)
			copyPath, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:]))
			if release != nil {
				release()
			}
			if err == nil || copyPath != "" {
				t.Fatalf("invalid explicit staging root %q used fallback: %q %v", tc.value, copyPath, err)
			}
		})
	}
}
