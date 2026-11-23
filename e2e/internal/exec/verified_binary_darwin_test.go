//go:build darwin

package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
)

func TestVerifiedExecutableCopyRejectsInheritedACLStagingParent(t *testing.T) {
	withoutExplicitVerifiedExecRoot(t)
	base := t.TempDir()
	temp := filepath.Join(base, "staging")
	if err := os.Mkdir(temp, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "ecctl")
	body := []byte("#!/bin/sh\necho '{}'\n")
	if err := os.WriteFile(bin, body, 0o700); err != nil {
		t.Fatal(err)
	}
	bin, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	if output, err := osexec.Command("/bin/chmod", "+a", "everyone allow read", temp).CombinedOutput(); err != nil {
		t.Fatalf("set inherited ACL fixture: %v: %s", err, output)
	}
	t.Setenv("TMPDIR", temp)
	if _, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:])); err == nil {
		release()
		t.Fatal("ACL-bearing staging parent was accepted")
	}
	if output, err := osexec.Command("/bin/chmod", "-N", temp).CombinedOutput(); err != nil {
		t.Fatalf("remove fixture ACL: %v: %s", err, output)
	}
	copyPath, release, err := VerifiedExecutableCopy(bin, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	copy, err := os.ReadFile(copyPath)
	if err != nil || string(copy) != string(body) {
		t.Fatalf("installed executable differs: %v", err)
	}
}
