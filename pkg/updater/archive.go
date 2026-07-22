package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const (
	maxBinaryBytes              = 100 << 20
	maxArchiveEntries           = 128
	maxArchiveUncompressedBytes = 256 << 20
)

func extractExecutable(artifact Artifact, goos string) ([]byte, error) {
	if strings.HasSuffix(artifact.Filename, ".tar.gz") {
		return extractTarGzip(artifact.Archive, "ecctl")
	}
	if strings.HasSuffix(artifact.Filename, ".zip") {
		name := "ecctl"
		if goos == "windows" {
			name += ".exe"
		}
		return extractZip(artifact.Archive, name)
	}
	return nil, fmt.Errorf("unsupported archive %q", artifact.Filename)
}

func extractTarGzip(raw []byte, executableName string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var executable []byte
	entries := 0
	var declaredBytes int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return nil, fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
		}
		if header.Size < 0 || header.Size > maxArchiveUncompressedBytes-declaredBytes {
			return nil, fmt.Errorf("archive declares more than %d uncompressed bytes", maxArchiveUncompressedBytes)
		}
		declaredBytes += header.Size
		if !safeArchivePath(header.Name) {
			return nil, fmt.Errorf("archive contains unsafe path %q", header.Name)
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("archive contains link %q", header.Name)
		}
		if header.Name != executableName {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("archive executable %q is not a regular file", header.Name)
		}
		if executable != nil {
			return nil, errors.New("archive contains more than one ecctl executable")
		}
		if header.Size < 1 || header.Size > maxBinaryBytes {
			return nil, fmt.Errorf("archive executable size %d is invalid", header.Size)
		}
		executable, err = io.ReadAll(io.LimitReader(reader, maxBinaryBytes+1))
		if err != nil {
			return nil, err
		}
		if len(executable) > maxBinaryBytes {
			return nil, errors.New("archive executable is too large")
		}
	}
	if executable == nil {
		return nil, errors.New("archive does not contain the ecctl executable")
	}
	return executable, nil
}

func extractZip(raw []byte, executableName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open zip archive: %w", err)
	}
	if len(reader.File) > maxArchiveEntries {
		return nil, fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
	}
	var declaredBytes uint64
	for _, file := range reader.File {
		if file.UncompressedSize64 > uint64(maxArchiveUncompressedBytes)-declaredBytes {
			return nil, fmt.Errorf("archive declares more than %d uncompressed bytes", maxArchiveUncompressedBytes)
		}
		declaredBytes += file.UncompressedSize64
	}
	var executable []byte
	for _, file := range reader.File {
		if !safeArchivePath(file.Name) {
			return nil, fmt.Errorf("archive contains unsafe path %q", file.Name)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("archive contains link %q", file.Name)
		}
		if file.Name != executableName {
			continue
		}
		if file.FileInfo().IsDir() || executable != nil {
			return nil, errors.New("archive executable is missing or ambiguous")
		}
		if file.UncompressedSize64 < 1 || file.UncompressedSize64 > maxBinaryBytes {
			return nil, fmt.Errorf("archive executable size %d is invalid", file.UncompressedSize64)
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		executable, err = io.ReadAll(io.LimitReader(stream, maxBinaryBytes+1))
		closeErr := stream.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(executable) > maxBinaryBytes {
			return nil, errors.New("archive executable is too large")
		}
	}
	if executable == nil {
		return nil, errors.New("archive does not contain the ecctl executable")
	}
	return executable, nil
}

func safeArchivePath(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return false
	}
	cleaned := path.Clean(name)
	return cleaned == name && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
