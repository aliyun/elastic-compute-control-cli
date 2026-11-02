package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesValidatedSnapshot(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "input.tar.gz")
	raw := tarball(t, fixtureEntries(fixtureFiles()))
	if err := os.WriteFile(archive, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "snapshot")
	for range 2 {
		var stdout, stderr bytes.Buffer
		if err := run([]string{"-archive", archive, "-revision", testRevision, "-out", out}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "2 products, 3 operations, 7 files") || stderr.Len() != 0 {
			t.Fatalf("output = %q, %q", stdout.String(), stderr.String())
		}
		result, err := generate(bytes.NewReader(raw), testRevision, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{".generated/index.json": result.index, "LICENSE": result.license, "manifest.json": result.manifestJSON} {
			got, err := os.ReadFile(filepath.Join(out, name))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s differs: %v", name, err)
			}
		}
		if err := checkPrepared(out); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunRejectsInvalidLocalInputs(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "input.tar.gz")
	if err := os.WriteFile(archive, []byte("invalid gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		nil, {"-unknown"}, {"extra"},
		{"-archive", archive, "-revision", "short"},
		{"-archive", filepath.Join(dir, "missing"), "-revision", testRevision},
		{"-archive", dir, "-revision", testRevision},
		{"-archive", archive, "-revision", testRevision},
	} {
		args = append(args, "-out", filepath.Join(t.TempDir(), "snapshot"))
		var stdout, stderr bytes.Buffer
		if err := run(args, &stdout, &stderr); err == nil || stdout.Len() != 0 {
			t.Fatalf("run %v: err=%v, stdout=%q", args, err, stdout.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil || stderr.Len() == 0 {
		t.Fatalf("help: %v", err)
	}
}

func TestRunProtectsInputAndUnownedPaths(t *testing.T) {
	archive := writeArchive(t, fixtureFiles())
	before := readBytes(t, archive)
	for _, kind := range []string{"file output", "parent file", "directory artifact", "input hardlink", "artifact symlink"} {
		t.Run(kind, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			switch kind {
			case "file output":
				if err := os.WriteFile(out, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "parent file":
				if err := os.WriteFile(out, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				out = filepath.Join(out, "nested")
			default:
				if err := os.Mkdir(out, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(out, "manifest.json")
				var err error
				switch kind {
				case "directory artifact":
					err = os.Mkdir(path, 0o700)
				case "input hardlink":
					err = os.Link(archive, path)
				case "artifact symlink":
					err = os.Symlink(archive, path)
				}
				if err != nil {
					t.Skipf("fixture unavailable: %v", err)
				}
			}
			var stdout, stderr bytes.Buffer
			if err := run([]string{"-archive", archive, "-revision", testRevision, "-out", out}, &stdout, &stderr); err == nil {
				t.Fatal("unsafe output accepted")
			}
			raw, err := os.ReadFile(archive)
			if err != nil || !bytes.Equal(raw, before) {
				t.Fatalf("input modified: %v", err)
			}
		})
	}
}
