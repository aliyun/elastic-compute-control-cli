package openapimeta

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestEmbeddedDefaultVersionsAndCounts(t *testing.T) {
	var manifest struct {
		FormatVersion     int    `json:"format_version"`
		Repository        string `json:"repository"`
		Revision          string `json:"revision"`
		ArchiveSHA256     string `json:"archive_sha256"`
		SnapshotSHA256    string `json:"snapshot_sha256"`
		Products          int    `json:"products"`
		Operations        int    `json:"operations"`
		Files             int    `json:"files"`
		SnapshotBytes     int    `json:"snapshot_bytes"`
		UncompressedBytes int64  `json:"uncompressed_bytes"`
		DefaultVersions   []struct {
			Code          string `json:"code"`
			CanonicalCode string `json:"canonical_code"`
			Version       string `json:"version"`
			Operations    int    `json:"operations"`
		} `json:"default_versions"`
	}
	if err := json.Unmarshal(snapshotManifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != 1 || manifest.Repository != "aliyun/aliyun-openapi-meta" {
		t.Fatalf("unexpected manifest provenance: %s", snapshotManifest)
	}
	if revision, err := hex.DecodeString(manifest.Revision); err != nil || len(revision) != 20 {
		t.Fatal("manifest revision must be a full commit SHA")
	}
	if sum, err := hex.DecodeString(manifest.ArchiveSHA256); err != nil || len(sum) != sha256.Size {
		t.Fatal("manifest must identify the source archive")
	}
	if manifest.SnapshotSHA256 != fmt.Sprintf("%x", sha256.Sum256(snapshotZIP)) || manifest.SnapshotBytes != len(snapshotZIP) {
		t.Fatal("snapshot hash or byte count mismatch")
	}
	data, err := ReadFile("metadatas/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Products []struct {
			Code                 string   `json:"code"`
			PluginDefaultVersion string   `json:"plugin_default_version"`
			Versions             []string `json:"versions"`
		} `json:"products"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if manifest.Products == 0 || len(catalog.Products) != manifest.Products || len(manifest.DefaultVersions) != manifest.Products {
		t.Fatalf("product count: catalog=%d, manifest=%d, inventory=%d", len(catalog.Products), manifest.Products, len(manifest.DefaultVersions))
	}
	sort.Slice(catalog.Products, func(i, j int) bool {
		return strings.ToLower(catalog.Products[i].Code) < strings.ToLower(catalog.Products[j].Code)
	})
	seen := map[string]bool{"metadatas/products.json": true, "LICENSE": true}
	products := make(map[string]bool)
	count := 0
	for i, product := range catalog.Products {
		code := strings.ToLower(product.Code)
		if code == "" || products[code] {
			t.Fatalf("invalid or duplicate product %q", code)
		}
		products[code] = true
		found := false
		for _, version := range product.Versions {
			found = found || version == product.PluginDefaultVersion
		}
		if !found || product.PluginDefaultVersion == "" {
			t.Fatalf("%s default version is not in versions", product.Code)
		}
		prefix := "canonical/" + code + "/" + product.PluginDefaultVersion + "/"
		name := prefix + "version.json"
		data, err := ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		var version struct {
			Version string                     `json:"version"`
			APIs    map[string]json.RawMessage `json:"apis"`
		}
		if err := json.Unmarshal(data, &version); err != nil {
			t.Fatal(err)
		}
		if version.Version != product.PluginDefaultVersion || version.APIs == nil {
			t.Fatalf("%s: invalid version manifest", name)
		}
		inventory := manifest.DefaultVersions[i]
		if inventory.Code != product.Code || inventory.CanonicalCode != code || inventory.Version != version.Version || inventory.Operations != len(version.APIs) {
			t.Fatalf("%s: manifest inventory mismatch: %+v", product.Code, inventory)
		}
		for op := range version.APIs {
			name := prefix + op + ".json"
			data, err := ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				Name       string            `json:"name"`
				Parameters []json.RawMessage `json:"parameters"`
			}
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if record.Name != op || record.Parameters == nil {
				t.Fatalf("%s: incomplete declared operation", name)
			}
			seen[name] = true
			count++
		}
	}
	license, err := ReadFile("LICENSE")
	if err != nil || !bytes.Contains(license, []byte("Apache License")) {
		t.Fatalf("upstream license: %v", err)
	}
	if manifest.Operations == 0 || count != manifest.Operations || len(seen) != manifest.Files || manifest.Files != 2+manifest.Products+manifest.Operations {
		t.Fatalf("counts: products=%d, operations=%d/%d, files=%d/%d", manifest.Products, count, manifest.Operations, len(seen), manifest.Files)
	}
	zr, err := zip.NewReader(bytes.NewReader(snapshotZIP), int64(len(snapshotZIP)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != len(seen) {
		t.Fatal("snapshot contains undeclared files or versions")
	}
	previous := ""
	var size uint64
	for _, file := range zr.File {
		if !seen[file.Name] || file.Name <= previous || file.Method != zip.Deflate {
			t.Fatalf("unexpected, unsorted or non-deflated file: %s", file.Name)
		}
		size += file.UncompressedSize64
		previous = file.Name
	}
	if int64(size) != manifest.UncompressedBytes {
		t.Fatal("uncompressed size mismatch")
	}
}

func readerFixture(t *testing.T, names ...string) *snapshotReader {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, name := range names {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			if _, err := file.Write([]byte("fixture data")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return readerWithChecksum(out.Bytes())
}

func readerWithChecksum(data []byte) *snapshotReader {
	return &snapshotReader{
		data:     data,
		manifest: []byte(fmt.Sprintf(`{"snapshot_sha256":"%x"}`, sha256.Sum256(data))),
	}
}

func TestReaderChecksumAndManifestFailures(t *testing.T) {
	tests := []struct {
		name   string
		change func(*snapshotReader)
		want   string
	}{
		{"corrupt snapshot", func(r *snapshotReader) { r.data[len(r.data)/2] ^= 0xff }, "checksum mismatch"},
		{"invalid JSON", func(r *snapshotReader) { r.manifest = []byte(`{`) }, "manifest"},
		{"missing checksum", func(r *snapshotReader) { r.manifest = []byte(`{}`) }, "invalid snapshot_sha256"},
		{"short checksum", func(r *snapshotReader) { r.manifest = []byte(`{"snapshot_sha256":"ab"}`) }, "invalid snapshot_sha256"},
		{"invalid hex", func(r *snapshotReader) { r.manifest = []byte(`{"snapshot_sha256":"` + strings.Repeat("z", 64) + `"}`) }, "invalid snapshot_sha256"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := readerFixture(t, "file.json")
			test.change(r)
			for range 2 {
				if _, err := r.readFile("file.json"); err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("got %v, want %q", err, test.want)
				}
			}
		})
	}
	if _, err := readerWithChecksum([]byte("not a zip")).readFile("file.json"); !errors.Is(err, zip.ErrFormat) {
		t.Fatalf("bad ZIP: %v", err)
	}
}

func TestReaderRejectsUnsafeAndDuplicateEntries(t *testing.T) {
	for _, names := range [][]string{
		{"../escape"}, {"/absolute"}, {`dir\escape`}, {"a//b"}, {"a/./b"}, {"C:/escape"}, {"directory/"}, {"file", "file"},
	} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			reader := readerFixture(t, names...)
			if _, err := reader.readFile("anything"); err == nil {
				t.Fatal("unsafe or duplicate ZIP entry accepted")
			}
		})
	}
}

func TestReadFileErrors(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../LICENSE", "/LICENSE", "canonical//ecs", "canonical/./ecs", "canonical/../LICENSE", `canonical\ecs`, "C:/LICENSE", "nul\x00"} {
		if _, err := ReadFile(path); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("ReadFile(%q) = %v, want fs.ErrInvalid", path, err)
		}
	}
	if _, err := ReadFile("does-not-exist.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
}

func TestReaderConcurrentFreshCopies(t *testing.T) {
	reader := readerFixture(t, "file.json")
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 4 {
				data, err := reader.readFile("file.json")
				if err != nil || string(data) != "fixture data" {
					t.Errorf("concurrent read: %q, %v", data, err)
					return
				}
				data[0] = 'X'
			}
		}()
	}
	wg.Wait()
}

func TestReaderDecompressesOnlyRequestedFile(t *testing.T) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	data := []byte("payload")
	for _, name := range []string{"good", "corrupt"} {
		sum := crc32.ChecksumIEEE(data)
		if name == "corrupt" {
			sum ^= 0xff
		}
		entry, err := writer.CreateRaw(&zip.FileHeader{
			Name: name, Method: zip.Store, CRC32: sum,
			CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader := readerWithChecksum(out.Bytes())
	got, err := reader.readFile("good")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("unused corrupt entry should not be decompressed: %s, %v", got, err)
	}
	if _, err := reader.readFile("corrupt"); !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("requested entry must pass its CRC check: %v", err)
	}
}
