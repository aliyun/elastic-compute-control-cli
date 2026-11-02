//go:build !windows

package configfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateFileHelpersRejectUnavailableAndClosedFiles(t *testing.T) {
	if PreparePrivateDirectory("") == nil || preparePrivateTemp(nil) == nil || validatePrivateOpenFile(nil) == nil {
		t.Fatal("unavailable private target accepted")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if validatePrivateDirectory(missing) == nil || prepareCreatedPrivateDirectory(missing) == nil {
		t.Fatal("missing private directory accepted")
	}
	dir := filepath.Join(t.TempDir(), "private")
	if err := PreparePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "entry")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if validatePrivateOpenFile(file) == nil {
		t.Fatal("shared file accepted as private")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if preparePrivateTemp(file) == nil || validatePrivateOpenFile(file) == nil {
		t.Fatal("closed private file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if preparePrivateTemp(file) == nil {
		t.Fatal("removed private file prepared")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PreparePrivateDirectory(filepath.Join(path, "child")); err == nil {
		t.Fatal("file parent accepted as a private directory")
	}
}

func TestSensitiveTempFailuresCleanOnlyTheirOwnStaging(t *testing.T) {
	file, err := CreateSensitiveTemp("", "ecctl-default-temp-*")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(path); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := CreateSensitiveTemp(dir, "invalid/pattern"); err == nil {
		t.Fatal("invalid temporary pattern accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed creation leaked staging: %v,%v", entries, err)
	}
	stage := filepath.Join(dir, sensitiveTempDirectoryPrefix+"fixture")
	if err := PreparePrivateDirectory(stage); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(stage, "not-a-file")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(nested, "evidence")
	if err := os.WriteFile(child, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(nested); err == nil {
		t.Fatal("nonempty staging recursively removed")
	}
	if raw, err := os.ReadFile(child); err != nil || string(raw) != "preserve" {
		t.Fatalf("unowned staging content changed: %q,%v", raw, err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSensitiveTemp(filepath.Join(blocker, sensitiveTempDirectoryPrefix+"fixture", "file")); err == nil {
		t.Fatal("invalid staging parent accepted")
	}
}
