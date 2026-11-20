//go:build linux

package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestVerifiedExecutableCopyRejectsACLStagingRootWithoutFallback(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(base, "private-exec")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "ecctl")
	body := []byte("#!/bin/sh\necho '{}'\n")
	if err := os.WriteFile(bin, body, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(staging)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	acl := []byte{
		2, 0, 0, 0,
		1, 0, 7, 0, 255, 255, 255, 255,
		4, 0, 0, 0, 255, 255, 255, 255,
		32, 0, 0, 0, 255, 255, 255, 255,
	}
	if err := unix.Fsetxattr(int(root.Fd()), "system.posix_acl_default", acl, 0); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EPERM) {
			t.Skipf("test filesystem cannot set a default POSIX ACL: %v", err)
		}
		t.Fatal(err)
	}
	t.Setenv("ECCTL_E2E_VERIFIED_EXEC_DIR", staging)
	digest := sha256.Sum256(body)
	if _, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:])); err == nil {
		release()
		t.Fatal("ACL-bearing staging root was accepted")
	}
	if err := unix.Fremovexattr(int(root.Fd()), "system.posix_acl_default"); err != nil {
		t.Fatal(err)
	}
	copyPath, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if filepath.Dir(filepath.Dir(copyPath)) != staging {
		t.Fatalf("verified copy escaped explicit root: %s", copyPath)
	}
}
