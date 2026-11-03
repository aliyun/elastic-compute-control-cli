// Package openapimetaformat defines the metadata lock and generated file index.
// It deliberately has no embedded data so the preparation tool can bootstrap.
package openapimetaformat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

const Repository = "aliyun/aliyun-openapi-meta"
const Version = 2

type Manifest struct {
	FormatVersion     int              `json:"format_version"`
	Repository        string           `json:"repository"`
	Revision          string           `json:"revision"`
	IndexSHA256       string           `json:"index_sha256"`
	Products          int              `json:"products"`
	Operations        int              `json:"operations"`
	Files             int              `json:"files"`
	UncompressedBytes int64            `json:"uncompressed_bytes"`
	DefaultVersions   []DefaultVersion `json:"default_versions"`
}

type DefaultVersion struct {
	Code          string `json:"code"`
	CanonicalCode string `json:"canonical_code"`
	Version       string `json:"version"`
	Operations    int    `json:"operations"`
}

type Entry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func ValidPath(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, "\\:*<>?\"`'|\x00\r\n")
}

func ValidHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size
}

func DecodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("metadata manifest: %w", err)
	}
	if manifest.FormatVersion != Version || manifest.Repository != Repository ||
		!ValidHex(manifest.Revision, 20) || !ValidHex(manifest.IndexSHA256, sha256.Size) ||
		manifest.Files < 1 || manifest.UncompressedBytes < 0 {
		return manifest, fmt.Errorf("metadata manifest: invalid format, source, index_sha256 or inventory")
	}
	return manifest, nil
}

func DecodeIndex(data []byte, manifest Manifest) ([]Entry, error) {
	if fmt.Sprintf("%x", sha256.Sum256(data)) != manifest.IndexSHA256 {
		return nil, fmt.Errorf("metadata index: SHA-256 checksum mismatch")
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("metadata index: %w", err)
	}
	previous := ""
	var size int64
	for _, entry := range entries {
		if !ValidPath(entry.Path) || entry.Path <= previous || !ValidHex(entry.SHA256, sha256.Size) ||
			entry.Size < 0 || entry.Size > manifest.UncompressedBytes-size {
			return nil, fmt.Errorf("metadata index: unsafe, duplicate, unsorted or invalid entry %q", entry.Path)
		}
		previous = entry.Path
		size += entry.Size
	}
	if len(entries) != manifest.Files || size != manifest.UncompressedBytes {
		return nil, fmt.Errorf("metadata index: inventory mismatch")
	}
	return entries, nil
}
