//go:build darwin

package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinAtomicCloneFailuresPreserveOriginalAndCleanStaging(t *testing.T) {
	originalClone := cloneDarwinFile
	t.Cleanup(func() { cloneDarwinFile = originalClone })
	for _, fault := range []string{"clone denied", "metadata changed", "fallback source missing"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("clone denied")
			cloneDarwinFile = func(src, dst string, flags int) error {
				switch fault {
				case "clone denied":
					return cause
				case "metadata changed":
					return os.WriteFile(dst, []byte("staged"), 0o644)
				default:
					if err := os.Remove(src); err != nil {
						t.Fatal(err)
					}
					return unix.ENOTSUP
				}
			}
			target, err := Resolve(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = target.AtomicWrite([]byte("replacement"), 0); err == nil {
				t.Fatal("unsafe replacement accepted")
			}
			if fault == "clone denied" && !errors.Is(err, cause) {
				t.Fatalf("cause lost: %v", err)
			}
			if fault != "fallback source missing" {
				raw, err := os.ReadFile(path)
				if err != nil || string(raw) != "original" {
					t.Fatalf("original changed: %q, %v", raw, err)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "config" {
					t.Fatalf("staging leaked: %s", entry.Name())
				}
			}
		})
	}
}

func TestDarwinMetadataValidationRejectsMissingAndChangedFiles(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if _, err := readDarwinFileMetadata(missing); err == nil {
		t.Fatal("missing metadata read")
	}
	if _, err := readDarwinFileMetadata(dir); err == nil {
		t.Fatal("directory metadata accepted")
	}
	if _, err := readDarwinACL(missing); err == nil {
		t.Fatal("missing ACL read")
	}
	if _, err := readDarwinXattrs(missing); err == nil {
		t.Fatal("missing xattrs read")
	}
	if err := verifyDarwinFileMetadata(missing, darwinFileMetadata{}); err == nil {
		t.Fatal("missing metadata verified")
	}
	if equalDarwinXattrs(map[string][]byte{"a": nil}, nil) {
		t.Fatal("unequal xattrs accepted")
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := readDarwinFileMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyDarwinFileMetadata(path, metadata); err == nil {
		t.Fatal("permission change accepted")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := finishReplacementAfterWrite(file, replacementMetadata{requested: 0o600}); err == nil {
		t.Fatal("closed replacement accepted")
	}
	if _, _, err := createAtomicTemp(missing, "stage-*", path, 0o600); err == nil {
		t.Fatal("missing staging directory accepted")
	}
}
