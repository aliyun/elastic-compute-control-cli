package openapimetaformat

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"sync"
)

// Reader verifies the locked index once and decompresses only requested files.
// Its filesystem can be either build output or the same files embedded in Go.
type Reader struct {
	source   fs.FS
	manifest []byte
	once     sync.Once
	entries  map[string]Entry
	err      error
}

func NewReader(source fs.FS, manifest []byte) *Reader {
	return &Reader{source: source, manifest: bytes.Clone(manifest)}
}

func (r *Reader) init() {
	manifest, err := DecodeManifest(r.manifest)
	if err != nil {
		r.err = err
		return
	}
	index, err := fs.ReadFile(r.source, "index.json")
	if err != nil {
		r.err = fmt.Errorf("metadata index: %w", err)
		return
	}
	entries, err := DecodeIndex(index, manifest)
	if err != nil {
		r.err = err
		return
	}
	r.entries = make(map[string]Entry, len(entries))
	for _, entry := range entries {
		r.entries[entry.Path] = entry
	}
}

func (r *Reader) ReadFile(path string) ([]byte, error) {
	fail := func(err error) ([]byte, error) {
		return nil, &fs.PathError{Op: "readfile", Path: path, Err: err}
	}
	if !ValidPath(path) {
		return fail(fs.ErrInvalid)
	}
	r.once.Do(r.init)
	if r.err != nil {
		return fail(r.err)
	}
	entry, ok := r.entries[path]
	if !ok {
		return fail(fs.ErrNotExist)
	}
	compressed, err := fs.ReadFile(r.source, path+".gz")
	if err != nil {
		return fail(err)
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		return fail(err)
	}
	defer reader.Close()
	reader.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(reader, entry.Size+1))
	if err != nil {
		return fail(err)
	}
	if int64(len(data)) != entry.Size || input.Len() != 0 || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.SHA256 {
		return fail(fmt.Errorf("metadata file: size, SHA-256 or gzip boundary mismatch"))
	}
	return data, nil
}

// Check is the offline build gate: reject extra files and validate every entry.
func (r *Reader) Check() error {
	r.once.Do(r.init)
	if r.err != nil {
		return r.err
	}
	expected := map[string]bool{"index.json": true}
	for path := range r.entries {
		expected[path+".gz"] = true
	}
	if err := fs.WalkDir(r.source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || !expected[path] {
			return fmt.Errorf("metadata output: unexpected or non-regular file %q", path)
		}
		delete(expected, path)
		return nil
	}); err != nil {
		return err
	}
	if len(expected) != 0 {
		return fmt.Errorf("metadata output: missing files")
	}
	for path := range r.entries {
		if _, err := r.ReadFile(path); err != nil {
			return err
		}
	}
	return nil
}
