package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

const repository = "aliyun/aliyun-openapi-meta"

// Revision is the caller's pinned source revision, not a claim that a local
// tarball can authenticate a Git commit. ArchiveSHA256 identifies the exact input.
type snapshotManifest struct {
	FormatVersion     int              `json:"format_version"`
	Repository        string           `json:"repository"`
	Revision          string           `json:"revision"`
	ArchiveSHA256     string           `json:"archive_sha256"`
	SnapshotSHA256    string           `json:"snapshot_sha256"`
	Products          int              `json:"products"`
	Operations        int              `json:"operations"`
	Files             int              `json:"files"`
	UncompressedBytes int64            `json:"uncompressed_bytes"`
	SnapshotBytes     int              `json:"snapshot_bytes"`
	DefaultVersions   []defaultVersion `json:"default_versions"`
}

type defaultVersion struct {
	Code          string `json:"code"`
	CanonicalCode string `json:"canonical_code"`
	Version       string `json:"version"`
	Operations    int    `json:"operations"`
}

type product struct {
	Code                 string   `json:"code"`
	PluginDefaultVersion string   `json:"plugin_default_version"`
	Versions             []string `json:"versions"`
}

type versionManifest struct {
	Version string                     `json:"version"`
	APIs    map[string]json.RawMessage `json:"apis"`
}

type generatedSnapshot struct {
	zip          []byte
	license      []byte
	manifest     snapshotManifest
	manifestJSON []byte
}

func normalizeRevision(revision string) (string, error) {
	if len(revision) != 40 {
		return "", errors.New("revision must be a full 40-character hexadecimal Git SHA")
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return "", errors.New("revision must be a full 40-character hexadecimal Git SHA")
	}
	return strings.ToLower(revision), nil
}

func generate(archive io.ReadSeeker, revision string) (*generatedSnapshot, error) {
	revision, err := normalizeRevision(revision)
	if err != nil {
		return nil, err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, archive); err != nil {
		return nil, fmt.Errorf("hash archive: %w", err)
	}
	manifest := snapshotManifest{
		FormatVersion: 1,
		Repository:    repository,
		Revision:      revision,
		ArchiveSHA256: hex.EncodeToString(hash.Sum(nil)),
	}

	// Products may occur after canonical entries in a GitHub tarball. The first
	// pass retains only the catalog, license and small version manifests. The
	// second pass selects exactly the operations declared by the defaults.
	metadata := make(map[string][]byte)
	if err := walkArchive(archive, revision, func(name string, r io.Reader) error {
		parts := strings.Split(name, "/")
		if name != "metadatas/products.json" && name != "LICENSE" &&
			!(len(parts) == 4 && parts[0] == "canonical" && parts[3] == "version.json") {
			return nil
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		metadata[name] = data
		return nil
	}); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(metadata["LICENSE"])) == 0 {
		return nil, errors.New("missing or empty upstream LICENSE")
	}
	var catalog struct {
		Products []product `json:"products"`
	}
	if err := json.Unmarshal(metadata["metadatas/products.json"], &catalog); err != nil {
		return nil, fmt.Errorf("metadatas/products.json: %w", err)
	}
	if len(catalog.Products) == 0 {
		return nil, errors.New("metadatas/products.json: missing or empty products")
	}
	sort.Slice(catalog.Products, func(i, j int) bool {
		return strings.ToLower(catalog.Products[i].Code) < strings.ToLower(catalog.Products[j].Code)
	})
	files := map[string][]byte{"LICENSE": metadata["LICENSE"]}
	files["metadatas/products.json"], err = compactJSON(metadata["metadatas/products.json"])
	if err != nil {
		return nil, err
	}
	needed := make(map[string]string)
	codes := make(map[string]bool)
	var problems []error
	for _, p := range catalog.Products {
		code := strings.ToLower(p.Code)
		if !safeComponent(code) || !safeComponent(p.PluginDefaultVersion) {
			problems = append(problems, fmt.Errorf("product %q: invalid code or missing/unsafe plugin_default_version %q", p.Code, p.PluginDefaultVersion))
			continue
		}
		if codes[code] {
			problems = append(problems, fmt.Errorf("duplicate case-insensitive product code %q", p.Code))
			continue
		}
		codes[code] = true
		found := false
		for _, v := range p.Versions {
			found = found || v == p.PluginDefaultVersion
		}
		if !found {
			problems = append(problems, fmt.Errorf("product %q: plugin_default_version %q is not in versions", p.Code, p.PluginDefaultVersion))
			continue
		}
		prefix := "canonical/" + code + "/" + p.PluginDefaultVersion + "/"
		name := prefix + "version.json"
		raw, ok := metadata[name]
		if !ok {
			problems = append(problems, fmt.Errorf("product %q: missing default manifest %s", p.Code, name))
			continue
		}
		var v versionManifest
		if err := json.Unmarshal(raw, &v); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if v.Version != p.PluginDefaultVersion || v.APIs == nil {
			problems = append(problems, fmt.Errorf("%s: version %q must match default %q and apis must be a non-null object", name, v.Version, p.PluginDefaultVersion))
			continue
		}
		files[name], err = compactJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		operations := sortedKeys(v.APIs)
		for _, op := range operations {
			if !safeComponent(op) || op == "version" {
				problems = append(problems, fmt.Errorf("%s: unsafe or reserved operation name %q", name, op))
				continue
			}
			needed[prefix+op+".json"] = op
		}
		manifest.DefaultVersions = append(manifest.DefaultVersions, defaultVersion{
			Code: p.Code, CanonicalCode: code, Version: p.PluginDefaultVersion, Operations: len(operations),
		})
	}
	if err := errors.Join(problems...); err != nil {
		return nil, fmt.Errorf("incomplete upstream metadata; no products were dropped and no snapshot was written:\n%w", err)
	}

	if err := walkArchive(archive, revision, func(name string, r io.Reader) error {
		op, ok := needed[name]
		if !ok {
			return nil
		}
		raw, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		var record struct {
			Name       string            `json:"name"`
			Parameters []json.RawMessage `json:"parameters"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if record.Name != op || record.Parameters == nil {
			return fmt.Errorf("incomplete upstream operation %s: name must be %q and parameters must be an array (empty [] is allowed; null or missing is not)", name, op)
		}
		files[name], err = compactJSON(raw)
		return err
	}); err != nil {
		return nil, err
	}
	for _, name := range sortedKeys(needed) {
		if _, ok := files[name]; !ok {
			problems = append(problems, fmt.Errorf("missing declared operation %s", name))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return nil, fmt.Errorf("incomplete upstream metadata; no snapshot was written:\n%w", err)
	}

	data, err := makeZIP(files)
	if err != nil {
		return nil, err
	}
	manifest.Products = len(catalog.Products)
	manifest.Operations = len(needed)
	manifest.Files = len(files)
	manifest.SnapshotBytes = len(data)
	for _, content := range files {
		manifest.UncompressedBytes += int64(len(content))
	}
	manifest.SnapshotSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return &generatedSnapshot{
		zip: data, license: metadata["LICENSE"], manifest: manifest,
		manifestJSON: append(manifestJSON, '\n'),
	}, nil
}

func safeComponent(s string) bool {
	return safePath(s) && !strings.Contains(s, "/")
}

func safePath(s string) bool {
	return s != "." && fs.ValidPath(s) && !strings.ContainsAny(s, "\\:\x00")
}

// walkArchive validates every effective tar path, including ignored versions
// and non-metadata entries. Only a single repository root and regular files or
// directories are allowed; links and duplicate paths cannot alias wanted files.
// Git's optional global PAX comment is metadata, not a filesystem entry.
func walkArchive(archive io.ReadSeeker, revision string, visit func(string, io.Reader) error) error {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := make(map[string]bool)
	directories := make(map[string]bool)
	globalHeaderSeen := false
	root := ""
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		name := header.Name
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if !safePath(name) {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			if globalHeaderSeen || root != "" {
				return errors.New("global PAX header must occur at most once, before repository entries")
			}
			for _, key := range sortedKeys(header.PAXRecords) {
				if key != "comment" {
					return fmt.Errorf("unsupported global PAX record %q", key)
				}
				if header.PAXRecords[key] != revision {
					return fmt.Errorf("global PAX revision %q does not match -revision %q", header.PAXRecords[key], revision)
				}
			}
			globalHeaderSeen = true
			continue
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate archive path %q", name)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("unsupported archive entry %q (type %d); links are not allowed", name, header.Typeflag)
		}
		if header.Typeflag != tar.TypeDir && directories[name] {
			return fmt.Errorf("archive file used as directory: %q", name)
		}
		seen[name] = header.Typeflag == tar.TypeDir
		parts := strings.Split(name, "/")
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root || (len(parts) == 1 && header.Typeflag != tar.TypeDir) {
			return fmt.Errorf("archive must have one repository root directory: %q", name)
		}
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			if isDir, exists := seen[parent]; exists && !isDir {
				return fmt.Errorf("archive file used as directory: %q", parent)
			}
			directories[parent] = true
		}
		if header.Typeflag != tar.TypeDir {
			if err := visit(strings.Join(parts[1:], "/"), tr); err != nil {
				return err
			}
		}
	}
	// Read through gzip's trailer to check its checksum. Tar may have zero
	// padding, but another concatenated tar or hidden nonzero data is invalid.
	buffer := make([]byte, 32*1024)
	for {
		n, err := gz.Read(buffer)
		for _, b := range buffer[:n] {
			if b != 0 {
				return errors.New("unexpected data after end of tar archive")
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read gzip trailer: %w", err)
		}
	}
}

func compactJSON(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func makeZIP(files map[string][]byte) ([]byte, error) {
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	compressor, err := flate.NewWriter(io.Discard, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	// ZIP entries are written sequentially; reuse the compressor's allocation
	// rather than allocating a large deflate buffer for every operation.
	writer.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		compressor.Reset(w)
		return compressor, nil
	})
	for _, name := range sortedKeys(files) {
		header := &zip.FileHeader{
			Name: name, Method: zip.Deflate,
			Modified: time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC),
		}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
