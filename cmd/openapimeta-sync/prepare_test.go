package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func writeArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(path, tarball(t, fixtureEntries(files)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func syncFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	archive := writeArchive(t, fixtureFiles())
	if err := run([]string{"-archive", archive, "-revision", testRevision, "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	return root, archive
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrepareAndCheckNeverChangeTrackedFiles(t *testing.T) {
	root, archive := syncFixture(t)
	lock := readBytes(t, filepath.Join(root, "manifest.json"))
	license := readBytes(t, filepath.Join(root, "LICENSE"))
	index := readBytes(t, filepath.Join(root, ".generated", "index.json"))
	// A verified generated cache must not need even the supplied source file.
	if err := run([]string{"-prepare", "-archive", "does-not-exist", "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".generated")); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-check", "-out", root}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "make metadata-prepare") {
		t.Fatalf("missing output must fail offline with preparation instructions: %v", err)
	}
	if err := run([]string{"-prepare", "-archive", archive, "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-check", "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"manifest.json": lock, "LICENSE": license, ".generated/index.json": index} {
		if !bytes.Equal(readBytes(t, filepath.Join(root, filepath.FromSlash(name))), want) {
			t.Fatalf("preparation or checking changed %s", name)
		}
	}
	if err := os.RemoveAll(filepath.Join(root, ".generated")); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, ".cache")
	if err := os.Mkdir(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, testRevision+".tar.gz"), readBytes(t, archive), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-prepare", "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatalf("cached source must prepare without downloading: %v", err)
	}
}

func TestPreparationFailurePreservesLockAndExistingOutput(t *testing.T) {
	root, archive := syncFixture(t)
	locked := readBytes(t, filepath.Join(root, "manifest.json"))
	indexPath := filepath.Join(root, ".generated", "index.json")
	index := readBytes(t, indexPath)
	missing := filepath.Join(root, ".generated", "canonical", "ecs", "2020-01-01", "Empty.json.gz")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	changed := fixtureFiles()
	changed["canonical/ecs/2020-01-01/Empty.json"] = `{"name":"Empty","parameters":[],"changed":true}`
	bad := writeArchive(t, changed)
	truncated := filepath.Join(t.TempDir(), "truncated.tar.gz")
	data := readBytes(t, archive)
	if err := os.WriteFile(truncated, data[:len(data)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{bad, truncated} {
		if err := run([]string{"-prepare", "-archive", input, "-out", root}, io.Discard, io.Discard); err == nil {
			t.Fatal("changed or truncated archive accepted")
		}
		if !bytes.Equal(readBytes(t, filepath.Join(root, "manifest.json")), locked) || !bytes.Equal(readBytes(t, indexPath), index) {
			t.Fatal("failed preparation replaced the lock or prior output")
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatal("failed preparation installed partial output")
		}
		stages, err := filepath.Glob(filepath.Join(root, ".prepare-*"))
		if err != nil || len(stages) != 0 {
			t.Fatalf("unfinished staging directory: %v, %v", stages, err)
		}
	}
	if err := run([]string{"-prepare", "-archive", archive, "-out", root}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPreparationRejectsSymlinkOutputsAndInputAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	root, archive := syncFixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".generated")); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(root, ".generated")); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-prepare", "-archive", archive, "-out", root}, io.Discard, io.Discard); err == nil {
		t.Fatal("output symlink accepted")
	}
	entries, err := os.ReadDir(other)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote through output symlink")
	}
	aliasRoot := t.TempDir()
	if err := os.Link(archive, filepath.Join(aliasRoot, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, archive)
	if err := run([]string{"-archive", archive, "-revision", testRevision, "-out", aliasRoot}, io.Discard, io.Discard); err == nil {
		t.Fatal("input/output alias accepted")
	}
	if !bytes.Equal(before, readBytes(t, archive)) {
		t.Fatal("input archive overwritten")
	}
}

func TestDownloadArchivePublishesOnlyCompleteResponses(t *testing.T) {
	for _, mode := range []string{"success", "status", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if mode == "status" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if mode == "truncated" {
					w.Header().Set("Content-Length", "1000")
				}
				_, _ = io.WriteString(w, "archive bytes")
			}))
			defer server.Close()
			dir := t.TempDir()
			target := filepath.Join(dir, "cached.tar.gz")
			if err := os.WriteFile(target, []byte("previous cache"), 0o600); err != nil {
				t.Fatal(err)
			}
			downloaded, err := downloadArchive(server.Client(), server.URL, dir)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if string(readBytes(t, downloaded)) != "archive bytes" {
					t.Fatal("downloaded bytes changed")
				}
				if err := os.Remove(downloaded); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("failed response accepted")
			}
			if string(readBytes(t, target)) != "previous cache" {
				t.Fatal("unverified download replaced the canonical cache")
			}
			stages, err := filepath.Glob(filepath.Join(dir, ".download-*"))
			if err != nil || len(stages) != 0 {
				t.Fatal("partial download was retained")
			}
		})
	}
}

func TestRejectedHTTP200DoesNotPoisonNextPreparation(t *testing.T) {
	for _, invalid := range []string{"malformed", "wrong content"} {
		t.Run(invalid, func(t *testing.T) {
			root, original := syncFixture(t)
			if err := os.RemoveAll(filepath.Join(root, ".generated")); err != nil {
				t.Fatal(err)
			}
			good := readBytes(t, original)
			bad := []byte("HTTP 200 but not an archive")
			if invalid == "wrong content" {
				changed := fixtureFiles()
				changed["canonical/ecs/2020-01-01/Empty.json"] = `{"name":"Empty","parameters":[],"unexpected":true}`
				bad = tarball(t, fixtureEntries(changed))
			}
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					_, _ = w.Write(bad)
				} else {
					_, _ = w.Write(good)
				}
			}))
			defer server.Close()
			fetch := func(revision, cache string) (string, error) {
				if revision != testRevision {
					t.Fatalf("unlocked source revision: %s", revision)
				}
				return downloadArchive(server.Client(), server.URL, cache)
			}
			if err := prepareWithFetcher(root, "", io.Discard, fetch); err == nil {
				t.Fatal("invalid HTTP 200 body accepted")
			}
			cached := filepath.Join(root, ".cache", testRevision+".tar.gz")
			if _, err := os.Stat(cached); !os.IsNotExist(err) {
				t.Fatal("unverified archive poisoned the source cache")
			}
			if err := prepareWithFetcher(root, "", io.Discard, fetch); err != nil {
				t.Fatalf("second valid response did not recover: %v", err)
			}
			if calls.Load() != 2 || !bytes.Equal(readBytes(t, cached), good) {
				t.Fatal("verified archive was not cached")
			}
			if !bytes.Equal(readBytes(t, original), good) {
				t.Fatal("caller-supplied archive was changed")
			}
		})
	}
}
