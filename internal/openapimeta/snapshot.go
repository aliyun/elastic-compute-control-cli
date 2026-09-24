// Package openapimeta provides the pinned default-version OpenAPI metadata.
// Refreshes are explicit: use cmd/openapimeta-sync with a local source tarball.
package openapimeta

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
)

//go:embed snapshot.zip
var snapshotZIP []byte

//go:embed manifest.json
var snapshotManifest []byte

var embedded = snapshotReader{data: snapshotZIP, manifest: snapshotManifest}

type snapshotReader struct {
	data     []byte
	manifest []byte
	once     sync.Once
	files    map[string]*zip.File
	err      error
}

// ReadFile returns a fresh copy of one snapshot entry, using slash-separated
// paths relative to the upstream repository (for example,
// canonical/ecs/2014-05-26/version.json). The ZIP checksum and central directory
// are checked once; only the requested file is decompressed on each call.
// Concurrent calls are safe. Missing entries wrap fs.ErrNotExist, and unsafe
// paths wrap fs.ErrInvalid. Non-default versions are intentionally not bundled.
func ReadFile(path string) ([]byte, error) {
	return embedded.readFile(path)
}

func validPath(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, "\\:\x00")
}

func (s *snapshotReader) init() {
	var manifest struct {
		SnapshotSHA256 string `json:"snapshot_sha256"`
	}
	if err := json.Unmarshal(s.manifest, &manifest); err != nil {
		s.err = fmt.Errorf("openapimeta manifest: %w", err)
		return
	}
	want, err := hex.DecodeString(manifest.SnapshotSHA256)
	if err != nil || len(want) != sha256.Size {
		s.err = fmt.Errorf("openapimeta manifest: invalid snapshot_sha256")
		return
	}
	actual := sha256.Sum256(s.data)
	if !bytes.Equal(actual[:], want) {
		s.err = fmt.Errorf("openapimeta snapshot: SHA-256 checksum mismatch")
		return
	}
	reader, err := zip.NewReader(bytes.NewReader(s.data), int64(len(s.data)))
	if err != nil {
		s.err = fmt.Errorf("openapimeta snapshot: %w", err)
		return
	}
	s.files = make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if !validPath(file.Name) || !file.Mode().IsRegular() {
			s.err = fmt.Errorf("openapimeta snapshot: unsafe entry %q", file.Name)
			return
		}
		if _, exists := s.files[file.Name]; exists {
			s.err = fmt.Errorf("openapimeta snapshot: duplicate entry %q", file.Name)
			return
		}
		s.files[file.Name] = file
	}
}

func (s *snapshotReader) readFile(path string) ([]byte, error) {
	pathError := func(err error) ([]byte, error) {
		return nil, &fs.PathError{Op: "readfile", Path: path, Err: err}
	}
	if !validPath(path) {
		return pathError(fs.ErrInvalid)
	}
	s.once.Do(s.init)
	if s.err != nil {
		return pathError(s.err)
	}
	file, ok := s.files[path]
	if !ok {
		return pathError(fs.ErrNotExist)
	}
	reader, err := file.Open()
	if err != nil {
		return pathError(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return pathError(err)
	}
	return data, nil
}
