package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	metadata "github.com/aliyun/elastic-compute-control-cli/internal/openapimetaformat"
)

func requireDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		return os.MkdirAll(path, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a regular directory: %s", path)
	}
	return nil
}

func requireRegularOrMissing(path string, input os.FileInfo) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || (input != nil && os.SameFile(input, info)) {
		return fmt.Errorf("refusing non-regular file or input archive: %s", path)
	}
	return nil
}

func readLock(root string) ([]byte, metadata.Manifest, error) {
	path := filepath.Join(root, "manifest.json")
	if err := requireRegularOrMissing(path, nil); err != nil {
		return nil, metadata.Manifest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, metadata.Manifest{}, err
	}
	manifest, err := metadata.DecodeManifest(data)
	return data, manifest, err
}

func checkPrepared(root string) error {
	lock, _, err := readLock(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ".generated")
	if err := requireDirectory(dir, false); err != nil {
		return err
	}
	return metadata.NewReader(os.DirFS(dir), lock).Check()
}

func prepareLocked(root, archive string, stdout io.Writer) error {
	return prepareWithFetcher(root, archive, stdout, fetchPinnedArchive)
}

func fetchPinnedArchive(revision, cache string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("metadata download: refusing insecure or excessive redirects")
		}
		return nil
	}}
	url := "https://codeload.github.com/" + repository + "/tar.gz/" + revision
	return downloadArchive(client, url, cache)
}

func prepareWithFetcher(root, archive string, stdout io.Writer, fetch func(string, string) (string, error)) error {
	locked, manifest, err := readLock(root)
	if err != nil {
		return err
	}
	if err := checkPrepared(root); err == nil {
		_, err = fmt.Fprintln(stdout, "using verified prepared metadata (no download)")
		return err
	}
	cacheTarget := ""
	if archive == "" {
		cache := filepath.Join(root, ".cache")
		if err := requireDirectory(cache, true); err != nil {
			return err
		}
		archive = filepath.Join(cache, manifest.Revision+".tar.gz")
		if err := requireRegularOrMissing(archive, nil); err != nil {
			return err
		}
		if _, err := os.Stat(archive); errors.Is(err, os.ErrNotExist) {
			cacheTarget = archive
			archive, err = fetch(manifest.Revision, cache)
			if err != nil {
				return err
			}
			defer os.Remove(archive)
		} else if err != nil {
			return err
		}
	}
	stage, err := os.MkdirTemp(root, ".prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	result, _, err := generateArchive(archive, manifest.Revision, stage)
	if err != nil {
		return fmt.Errorf("prepare metadata from %s: %w", archive, err)
	}
	normalized, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if !bytes.Equal(result.manifestJSON, append(normalized, '\n')) {
		return errors.New("upstream metadata does not match the locked content and inventory")
	}
	if err := metadata.NewReader(os.DirFS(stage), locked).Check(); err != nil {
		return err
	}
	current, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil || !bytes.Equal(current, locked) {
		return errors.New("metadata lock changed during preparation")
	}
	// The canonical source cache must contain only archives verified against
	// the active lock. A failed HTTP 200 body must not poison the next attempt.
	if cacheTarget != "" {
		if err := os.Rename(archive, cacheTarget); err != nil {
			return err
		}
	}
	if err := installDirectory(stage, filepath.Join(root, ".generated")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "prepared %d products, %d operations, %d files from %s\n", manifest.Products, manifest.Operations, manifest.Files, manifest.Revision)
	return err
}

func generateArchive(path, revision, out string) (*generatedSnapshot, os.FileInfo, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("archive must be a regular local file")
	}
	result, err := generate(input, revision, out)
	return result, info, err
}

func updateLock(root, archive, revision string, stdout io.Writer) error {
	stage, err := os.MkdirTemp(root, ".prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	result, input, err := generateArchive(archive, revision, stage)
	if err != nil {
		return err
	}
	for _, name := range []string{"LICENSE", "manifest.json"} {
		if err := requireRegularOrMissing(filepath.Join(root, name), input); err != nil {
			return err
		}
	}
	if err := metadata.NewReader(os.DirFS(stage), result.manifestJSON).Check(); err != nil {
		return err
	}
	if err := installDirectory(stage, filepath.Join(root, ".generated")); err != nil {
		return err
	}
	if err := writeAtomic(root, "LICENSE", result.license); err != nil {
		return err
	}
	// Install the lock last; interrupted updates fail closed and are recoverable
	// with preparation from the still-active lock.
	if err := writeAtomic(root, "manifest.json", result.manifestJSON); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%d products, %d operations, %d files; index_sha256: %s\n", result.manifest.Products, result.manifest.Operations, result.manifest.Files, result.manifest.IndexSHA256)
	return err
}

func installDirectory(stage, target string) error {
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return os.Rename(stage, target)
	}
	if err := requireDirectory(target, false); err != nil {
		return err
	}
	backup := stage + "-previous"
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		return errors.Join(err, os.Rename(backup, target))
	}
	return os.RemoveAll(backup)
}

func writeAtomic(root, name string, data []byte) error {
	file, err := os.CreateTemp(root, ".prepare-write-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	err = errors.Join(writeErr, file.Chmod(0o644), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(root, name))
}

func downloadArchive(client *http.Client, url, cache string) (string, error) {
	response, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download pinned metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download pinned metadata: HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp(cache, ".download-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(file.Name())
		}
	}()
	const limit = 1 << 30
	n, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	err = errors.Join(copyErr, file.Close())
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", errors.New("metadata archive exceeds 1 GiB")
	}
	complete = true
	return file.Name(), nil
}
