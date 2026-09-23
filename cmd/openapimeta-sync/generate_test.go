package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const testRevision = "3c5b9bdc7d9f06fe06b5793713845820121f1f17"

func fixtureFiles() map[string]string {
	return map[string]string{
		"LICENSE": "upstream fixture license\n",
		"metadatas/products.json": `{
  "products": [
    {"code":"Ecs","version":"2019-01-01","plugin_default_version":"2020-01-01","versions":["2019-01-01","2020-01-01"],"extra":{"keep":true}},
    {"code":"RAM","plugin_default_version":"2015-05-01","versions":["2015-05-01"]}
  ], "unknown_field": [1,2,3]
}`,
		"canonical/ecs/2020-01-01/version.json": `{"version":"2020-01-01","apis":{"DescribeThings":{"description":"说明"},"Empty":{}},"keep":"all fields"}`,
		"canonical/ecs/2020-01-01/DescribeThings.json": `{
 "name": "DescribeThings", "parameters": [{"name":"filter", "schema":{"type":"string"}}],
 "description": "保留 Unicode", "unknown": {"number":9007199254740993, "null":null}, "method":"POST"
}`,
		"canonical/ecs/2020-01-01/Empty.json":     `{"name":"Empty", "parameters":[], "keep":false}`,
		"canonical/ram/2015-05-01/version.json":   `{"version":"2015-05-01","apis":{"ListUsers":{}}}`,
		"canonical/ram/2015-05-01/ListUsers.json": `{"name":"ListUsers","parameters":[]}`,
		// Ignore historical versions and undeclared files, not entire products.
		"canonical/ecs/2019-01-01/version.json": `{"version":"2019-01-01","apis":{"Old":{}}}`,
		"canonical/ecs/2019-01-01/Old.json":     `{"name":"Old","parameters":[]}`,
		"canonical/ecs/2020-01-01/Unused.json":  `{"irrelevant":true}`,
		"metadatas/ecs/DescribeThings.json":     `{"legacy":true}`,
	}
}

type tarEntry struct {
	name string
	data string
	kind byte
	link string
	pax  map[string]string
}

func fixtureEntries(files map[string]string) []tarEntry {
	var entries []tarEntry
	for _, name := range sortedKeys(files) {
		entries = append(entries, tarEntry{name: "upstream/" + name, data: files[name], kind: tar.TypeReg})
	}
	return entries
}

// Fixtures stay entirely in memory, including malicious tar headers.
func tarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name, Typeflag: entry.kind, Linkname: entry.link,
			Mode: 0o600, ModTime: time.Unix(1700000000, 0), PAXRecords: entry.pax,
		}
		if entry.kind == tar.TypeXGlobalHeader {
			header = &tar.Header{Name: entry.name, Typeflag: entry.kind, PAXRecords: entry.pax}
		}
		if entry.kind == tar.TypeReg || entry.kind == tar.TypeRegA {
			header.Size = int64(len(entry.data))
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size != 0 {
			if _, err := io.WriteString(tw, entry.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestGenerateDeterministicCompleteDefaults(t *testing.T) {
	files := fixtureFiles()
	entries := fixtureEntries(files)
	archive := tarball(t, entries)
	first, err := generate(bytes.NewReader(archive), testRevision)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(bytes.NewReader(archive), strings.ToUpper(testRevision))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.zip, second.zip) || !bytes.Equal(first.manifestJSON, second.manifestJSON) {
		t.Fatal("same input must produce byte-identical outputs")
	}
	slices.Reverse(entries)
	reordered, err := generate(bytes.NewReader(tarball(t, entries)), testRevision)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.zip, reordered.zip) {
		t.Fatal("tar entry order changed snapshot bytes")
	}
	manifest := first.manifest
	if manifest.Repository != repository || manifest.Revision != testRevision || manifest.FormatVersion != 1 {
		t.Fatalf("source identity: %+v", manifest)
	}
	if manifest.ArchiveSHA256 != fmt.Sprintf("%x", sha256.Sum256(archive)) ||
		manifest.SnapshotSHA256 != fmt.Sprintf("%x", sha256.Sum256(first.zip)) {
		t.Fatal("manifest checksums do not match artifacts")
	}
	if manifest.Products != 2 || manifest.Operations != 3 || manifest.Files != 7 || manifest.SnapshotBytes != len(first.zip) {
		t.Fatalf("wrong counts: %+v", manifest)
	}
	wantDefaults := []defaultVersion{
		{Code: "Ecs", CanonicalCode: "ecs", Version: "2020-01-01", Operations: 2},
		{Code: "RAM", CanonicalCode: "ram", Version: "2015-05-01", Operations: 1},
	}
	if !reflect.DeepEqual(manifest.DefaultVersions, wantDefaults) {
		t.Fatalf("default inventory: %+v", manifest.DefaultVersions)
	}
	var decoded snapshotManifest
	if err := json.Unmarshal(first.manifestJSON, &decoded); err != nil || !reflect.DeepEqual(decoded, manifest) {
		t.Fatalf("manifest JSON mismatch: %v", err)
	}
	if string(first.license) != files["LICENSE"] {
		t.Fatal("upstream license was changed")
	}
	zr, err := zip.NewReader(bytes.NewReader(first.zip), int64(len(first.zip)))
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{
		"LICENSE",
		"canonical/ecs/2020-01-01/DescribeThings.json",
		"canonical/ecs/2020-01-01/Empty.json",
		"canonical/ecs/2020-01-01/version.json",
		"canonical/ram/2015-05-01/ListUsers.json",
		"canonical/ram/2015-05-01/version.json",
		"metadatas/products.json",
	}
	var paths []string
	var size int64
	for _, entry := range zr.File {
		paths = append(paths, entry.Name)
		if entry.Method != zip.Deflate || entry.Mode().Perm() != 0o644 ||
			!entry.Modified.Equal(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("non-deterministic header: %+v", entry.FileHeader)
		}
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := []byte(files[entry.Name])
		if entry.Name != "LICENSE" {
			want, err = compactJSON(want)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("lost or changed raw JSON fields: %s", entry.Name)
		}
		size += int64(len(data))
		var best bytes.Buffer
		compressor, err := flate.NewWriter(&best, flate.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compressor.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := compressor.Close(); err != nil {
			t.Fatal(err)
		}
		raw, err := entry.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		compressed, err := io.ReadAll(raw)
		if err != nil || !bytes.Equal(compressed, best.Bytes()) {
			t.Fatalf("entry not best-compression deflate: %s, %v", entry.Name, err)
		}
	}
	if !reflect.DeepEqual(paths, wantPaths) || manifest.UncompressedBytes != size {
		t.Fatalf("wrong inventory: %v, size %d", paths, size)
	}
}

func TestGenerateRejectsIncompleteMetadata(t *testing.T) {
	const catalog = "metadatas/products.json"
	const version = "canonical/ecs/2020-01-01/version.json"
	const operation = "canonical/ecs/2020-01-01/Empty.json"
	tests := []struct {
		name   string
		path   string
		data   string
		absent bool
		want   string
	}{
		{"missing catalog", catalog, "", true, "metadatas/products.json"},
		{"missing default", catalog, `{"products":[{"code":"Ecs","versions":["2020-01-01"]}]}`, false, "plugin_default_version"},
		{"unlisted default", catalog, `{"products":[{"code":"Ecs","plugin_default_version":"2020-01-01","versions":["2019-01-01"]}]}`, false, "not in versions"},
		{"null versions", catalog, `{"products":[{"code":"Ecs","plugin_default_version":"2020-01-01","versions":null}]}`, false, "not in versions"},
		{"unsafe product", catalog, `{"products":[{"code":"../ecs","plugin_default_version":"2020-01-01","versions":["2020-01-01"]}]}`, false, "invalid code"},
		{"unsafe default", catalog, `{"products":[{"code":"Ecs","plugin_default_version":"../2020-01-01","versions":["../2020-01-01"]}]}`, false, "unsafe plugin_default_version"},
		{"duplicate product", catalog, `{"products":[{"code":"Ecs","plugin_default_version":"2020-01-01","versions":["2020-01-01"]},{"code":"ECS","plugin_default_version":"2020-01-01","versions":["2020-01-01"]}]}`, false, "duplicate case-insensitive"},
		{"empty catalog", catalog, `{"products":[]}`, false, "empty products"},
		{"missing default manifest", version, "", true, "missing default manifest"},
		{"version mismatch", version, `{"version":"2019-01-01","apis":{}}`, false, "must match default"},
		{"null apis", version, `{"version":"2020-01-01","apis":null}`, false, "non-null object"},
		{"missing apis", version, `{"version":"2020-01-01"}`, false, "non-null object"},
		{"unsafe operation", version, `{"version":"2020-01-01","apis":{"../Empty":{}}}`, false, "unsafe or reserved operation"},
		{"reserved operation", version, `{"version":"2020-01-01","apis":{"version":{}}}`, false, "unsafe or reserved operation"},
		{"missing operation", operation, "", true, "missing declared operation"},
		{"missing name", operation, `{"parameters":[]}`, false, "name must be"},
		{"null name", operation, `{"name":null,"parameters":[]}`, false, "name must be"},
		{"empty name", operation, `{"name":"","parameters":[]}`, false, "name must be"},
		{"wrong name", operation, `{"name":"Other","parameters":[]}`, false, "name must be"},
		{"missing parameters", operation, `{"name":"Empty"}`, false, "parameters must be an array"},
		{"null parameters", operation, `{"name":"Empty","parameters":null}`, false, "parameters must be an array"},
		{"object parameters", operation, `{"name":"Empty","parameters":{}}`, false, "cannot unmarshal object"},
		{"bad JSON", operation, `{"name":`, false, "unexpected end of JSON"},
		{"missing license", "LICENSE", "", true, "upstream LICENSE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := fixtureFiles()
			if test.absent {
				delete(files, test.path)
			} else {
				files[test.path] = test.data
			}
			result, err := generate(bytes.NewReader(tarball(t, fixtureEntries(files))), testRevision)
			if err == nil || !strings.Contains(err.Error(), test.want) || result != nil {
				t.Fatalf("got result %v, error %v; want %q", result != nil, err, test.want)
			}
		})
	}
}

func TestGenerateKeepsProductWithNoDeclaredOperations(t *testing.T) {
	files := fixtureFiles()
	files["canonical/ram/2015-05-01/version.json"] = `{"version":"2015-05-01","apis":{}}`
	result, err := generate(bytes.NewReader(tarball(t, fixtureEntries(files))), testRevision)
	if err != nil {
		t.Fatal(err)
	}
	if result.manifest.Products != 2 || result.manifest.Operations != 2 || len(result.manifest.DefaultVersions) != 2 {
		t.Fatalf("empty product was dropped: %+v", result.manifest)
	}
}

func TestGenerateRejectsUnsafeArchives(t *testing.T) {
	tests := []struct {
		entry tarEntry
		want  string
	}{
		{tarEntry{name: "upstream/../escape", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "/absolute", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "upstream/canonical/./file", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "upstream/canonical//file", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: `upstream/canonical\..\file`, kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "C:/escape", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "upstream/ignored:stream", kind: tar.TypeReg}, "unsafe archive path"},
		{tarEntry{name: "upstream/LICENSE", data: "duplicate", kind: tar.TypeReg}, "duplicate archive path"},
		{tarEntry{name: "upstream/LICENSE/", kind: tar.TypeDir}, "duplicate archive path"},
		{tarEntry{name: "another-root/ignored", kind: tar.TypeReg}, "one repository root"},
		{tarEntry{name: "upstream/link", kind: tar.TypeSymlink, link: "LICENSE"}, "links are not allowed"},
		{tarEntry{name: "upstream/link", kind: tar.TypeLink, link: "upstream/LICENSE"}, "links are not allowed"},
		{tarEntry{name: "upstream/pipe", kind: tar.TypeFifo}, "unsupported archive entry"},
		{tarEntry{name: "upstream/LICENSE/child", kind: tar.TypeReg}, "file used as directory"},
	}
	for _, test := range tests {
		t.Run(test.entry.name, func(t *testing.T) {
			entries := append(fixtureEntries(fixtureFiles()), test.entry)
			result, err := generate(bytes.NewReader(tarball(t, entries)), testRevision)
			if result != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestGitHubGlobalPAXHeader(t *testing.T) {
	global := tarEntry{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"comment": testRevision}}
	entries := append([]tarEntry{global}, fixtureEntries(fixtureFiles())...)
	if _, err := generate(bytes.NewReader(tarball(t, entries)), testRevision); err != nil {
		t.Fatalf("GitHub global PAX header: %v", err)
	}
	for _, test := range []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{"revision mismatch", append([]tarEntry{{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"comment": strings.Repeat("a", 40)}}}, fixtureEntries(fixtureFiles())...), "does not match -revision"},
		{"global path", append([]tarEntry{{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"path": "upstream/LICENSE"}}}, fixtureEntries(fixtureFiles())...), "unsupported global PAX record"},
		{"duplicate global header", append([]tarEntry{global}, entries...), "at most once"},
		{"late global header", append(fixtureEntries(fixtureFiles()), global), "before repository entries"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := generate(bytes.NewReader(tarball(t, test.entries)), testRevision); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestEffectivePAXPathsAndImplicitDirectories(t *testing.T) {
	longName := "upstream/" + strings.Repeat("a", 300)
	for _, test := range []struct {
		entries []tarEntry
		want    string
	}{
		{[]tarEntry{{name: longName + "/../escape", kind: tar.TypeReg}}, "unsafe archive path"},
		{[]tarEntry{{name: longName, kind: tar.TypeReg}, {name: longName, kind: tar.TypeReg}}, "duplicate archive path"},
		{[]tarEntry{{name: "upstream/canonical", kind: tar.TypeReg}}, "file used as directory"},
	} {
		entries := append(fixtureEntries(fixtureFiles()), test.entries...)
		if _, err := generate(bytes.NewReader(tarball(t, entries)), testRevision); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("got %v, want %q", err, test.want)
		}
	}
}

func TestGenerateRejectsBadArchiveChecksum(t *testing.T) {
	archive := tarball(t, fixtureEntries(fixtureFiles()))
	archive[len(archive)-8] ^= 0xff
	if _, err := generate(bytes.NewReader(archive), testRevision); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("gzip corruption not detected: %v", err)
	}
}

func TestGenerateRejectsAppendedTar(t *testing.T) {
	archive := tarball(t, fixtureEntries(fixtureFiles()))
	archive = append(archive, tarball(t, []tarEntry{{name: "upstream/LICENSE", kind: tar.TypeReg, data: "hidden"}})...)
	if _, err := generate(bytes.NewReader(archive), testRevision); err == nil || !strings.Contains(err.Error(), "after end of tar") {
		t.Fatalf("concatenated tar not detected: %v", err)
	}
}

func TestRunFlags(t *testing.T) {
	for _, revision := range []string{"", "3c5b9bd", strings.Repeat("g", 40), testRevision + "0"} {
		if _, err := normalizeRevision(revision); err == nil {
			t.Fatalf("accepted invalid revision %q", revision)
		}
	}
	for _, args := range [][]string{
		{}, {"-archive", "unused"},
		{"-archive", "unused", "-revision", testRevision, "extra"},
		{"-archive", "unused", "-revision", testRevision, "-out", ""},
		{"-unknown"},
	} {
		if err := run(args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted invalid flags %v", args)
		}
	}
	if err := run([]string{"-help"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("help: %v", err)
	}
}
