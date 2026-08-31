package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePrivateFileReportsPostCommitSyncFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := removePrivateFileWithSync(path, func(string) error { return errors.New("directory sync failed") })
	if !ReplacementApplied(err) {
		t.Fatalf("remove error = %v, want post-commit", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("private file still exists: %v", statErr)
	}
	if _, statErr := os.Stat(retiredPrivatePath(path)); statErr != nil {
		t.Fatalf("private tombstone is unavailable after uncertain sync: %v", statErr)
	}
	err = removePrivateFileWithSync(path, func(string) error { return errors.New("directory sync still failed") })
	if !ReplacementApplied(err) {
		t.Fatalf("retry remove error = %v, want post-commit", err)
	}
	if _, statErr := os.Stat(retiredPrivatePath(path)); statErr != nil {
		t.Fatalf("private tombstone disappeared after retry failure: %v", statErr)
	}
	if err := removePrivateFileWithSync(path, func(string) error { return nil }); err != nil {
		t.Fatalf("retry durable removal: %v", err)
	}
	if _, statErr := os.Stat(retiredPrivatePath(path)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("private tombstone remained after retry: %v", statErr)
	}
}

func TestRemovePrivateFileRejectsHardLinkedTombstone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials-v2")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	tombstone := retiredPrivatePath(path)
	if err := os.Link(path, tombstone); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := RemovePrivateFile(path); !errors.Is(err, ErrPrivateRetirementAlias) {
		t.Fatalf("hard-link retirement error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("canonical private file was removed: %v", err)
	}
	if _, err := os.Stat(tombstone); err != nil {
		t.Fatalf("hard-link tombstone was removed: %v", err)
	}
}

func TestRemovePrivateFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := RemovePrivateFile(path); err != nil {
		t.Fatal(err)
	}
}
