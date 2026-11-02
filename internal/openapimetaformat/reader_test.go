package openapimetaformat

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func gzipData(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func fixture(t *testing.T, entries ...Entry) (fstest.MapFS, []byte) {
	t.Helper()
	source := fstest.MapFS{}
	var size int64
	for i := range entries {
		data := []byte("fixture data")
		entries[i].SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		entries[i].Size = int64(len(data))
		size += entries[i].Size
		source[entries[i].Path+".gz"] = &fstest.MapFile{Data: gzipData(t, data)}
	}
	index, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	source["index.json"] = &fstest.MapFile{Data: index}
	manifest, err := json.Marshal(Manifest{
		FormatVersion: Version, Repository: Repository, Revision: strings.Repeat("a", 40),
		IndexSHA256: fmt.Sprintf("%x", sha256.Sum256(index)), Files: len(entries), UncompressedBytes: size,
	})
	if err != nil {
		t.Fatal(err)
	}
	return source, manifest
}

func TestReaderManifestAndIndexValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(fstest.MapFS, *[]byte)
	}{
		{"invalid manifest", func(_ fstest.MapFS, m *[]byte) { *m = []byte(`{`) }},
		{"missing lock fields", func(_ fstest.MapFS, m *[]byte) { *m = []byte(`{}`) }},
		{"wrong format", func(_ fstest.MapFS, m *[]byte) {
			*m = bytes.ReplaceAll(*m, []byte(`"format_version":2`), []byte(`"format_version":1`))
		}},
		{"bad index hash", func(f fstest.MapFS, _ *[]byte) { f["index.json"].Data[0] ^= 0xff }},
		{"missing index", func(f fstest.MapFS, _ *[]byte) { delete(f, "index.json") }},
		{"wrong count", func(_ fstest.MapFS, m *[]byte) { *m = bytes.ReplaceAll(*m, []byte(`"files":1`), []byte(`"files":2`)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, manifest := fixture(t, Entry{Path: "file.json"})
			tc.edit(source, &manifest)
			r := NewReader(source, manifest)
			for range 2 {
				if _, err := r.ReadFile("file.json"); err == nil {
					t.Fatal("invalid metadata accepted")
				}
			}
		})
	}
	for _, paths := range [][]string{{"../escape"}, {"/absolute"}, {`a\b`}, {"a//b"}, {"C:/file"}, {"file", "file"}, {"z", "a"}} {
		entries := make([]Entry, len(paths))
		for i, path := range paths {
			entries[i].Path = path
		}
		source, manifest := fixture(t, entries...)
		if err := NewReader(source, manifest).Check(); err == nil {
			t.Fatalf("unsafe index accepted: %q", paths)
		}
	}
}

func TestReaderChecksRequestedFileOnly(t *testing.T) {
	source, manifest := fixture(t, Entry{Path: "a"}, Entry{Path: "b"})
	source["b.gz"].Data[len(source["b.gz"].Data)-8] ^= 0xff
	r := NewReader(source, manifest)
	if data, err := r.ReadFile("a"); err != nil || string(data) != "fixture data" {
		t.Fatalf("unused corrupt file affected read: %q, %v", data, err)
	}
	if _, err := r.ReadFile("b"); !errors.Is(err, gzip.ErrChecksum) {
		t.Fatalf("corrupt gzip accepted: %v", err)
	}
	if err := r.Check(); err == nil {
		t.Fatal("offline check accepted a corrupt file")
	}
}

func TestReaderRejectsChangedMissingAndExtraData(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(fstest.MapFS)
	}{
		{"different bytes", func(f fstest.MapFS) { f["file.gz"].Data = gzipData(t, []byte("changed data")) }},
		{"oversized", func(f fstest.MapFS) { f["file.gz"].Data = gzipData(t, bytes.Repeat([]byte("x"), 10000)) }},
		{"truncated", func(f fstest.MapFS) { f["file.gz"].Data = f["file.gz"].Data[:10] }},
		{"trailing data", func(f fstest.MapFS) { f["file.gz"].Data = append(f["file.gz"].Data, 'x') }},
		{"missing", func(f fstest.MapFS) { delete(f, "file.gz") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, manifest := fixture(t, Entry{Path: "file"})
			tc.edit(source)
			if _, err := NewReader(source, manifest).ReadFile("file"); err == nil {
				t.Fatal("invalid file accepted")
			}
		})
	}
	source, manifest := fixture(t, Entry{Path: "file"})
	source["extra.gz"] = &fstest.MapFile{Data: nil}
	if err := NewReader(source, manifest).Check(); err == nil {
		t.Fatal("extra output accepted")
	}
	source["extra.gz"].Mode = fs.ModeSymlink
	if err := NewReader(source, manifest).Check(); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestReaderConcurrentFreshCopiesAndPathErrors(t *testing.T) {
	source, manifest := fixture(t, Entry{Path: "file"})
	r := NewReader(source, manifest)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 4 {
				data, err := r.ReadFile("file")
				if err != nil || string(data) != "fixture data" {
					t.Errorf("read = %q, %v", data, err)
					return
				}
				data[0] = 'X'
			}
		})
	}
	wg.Wait()
	for _, path := range []string{"", ".", "..", "../file", "/file", "a//b", `a\b`, "C:/file", "nul\x00"} {
		if _, err := r.ReadFile(path); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("ReadFile(%q) = %v", path, err)
		}
	}
	if _, err := r.ReadFile("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file = %v", err)
	}
	if err := r.Check(); err != nil {
		t.Fatal(err)
	}
}
