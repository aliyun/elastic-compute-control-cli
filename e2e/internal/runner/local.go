package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

func runLocalAction(ctx context.Context, data map[string]any, action scenario.LocalAction) (result execpkg.Result) {
	startedAt := time.Now()
	result = execpkg.Result{Exit: -1}
	source, err := vars.Render(action.Source, data)
	if err != nil {
		result.Err = fmt.Errorf("render local source: %w", err)
		return result
	}
	destination, err := vars.Render(action.Destination, data)
	if err != nil {
		result.Err = fmt.Errorf("render local destination: %w", err)
		return result
	}
	result.Command = fmt.Sprintf("local %s %s %s", action.Action, source, destination)
	defer func() { result.Duration = time.Since(startedAt) }()

	workDir, _ := data["work_dir"].(string)
	source, err = localWorkFile(workDir, source)
	if err != nil {
		result.Err = fmt.Errorf("local source: %w", err)
		return result
	}
	destination, err = localWorkFile(workDir, destination)
	if err != nil {
		result.Err = fmt.Errorf("local destination: %w", err)
		return result
	}
	if source == destination {
		result.Err = fmt.Errorf("local source and destination must differ")
		return result
	}
	if action.Action != "extract-tar-gzip" {
		result.Err = fmt.Errorf("unsupported local action %q", action.Action)
		return result
	}
	format, err := extractSingleImage(ctx, source, destination)
	if err != nil {
		result.Err = err
		return result
	}
	payload := map[string]any{"artifact": map[string]any{"file": destination, "format": format}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		result.Err = err
		return result
	}
	result.Exit = 0
	result.Stdout = string(encoded)
	result.JSON = payload
	return result
}

func localWorkFile(workDir, path string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		return "", fmt.Errorf("run work directory is missing")
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Dir(absPath) != absWorkDir {
		return "", fmt.Errorf("path must be a direct child of the run work directory")
	}
	return absPath, nil
}

func extractSingleImage(ctx context.Context, source, destination string) (format string, returnErr error) {
	format, ok := imageFormatForPath(destination)
	if !ok {
		return "", fmt.Errorf("unsupported image destination format %q", filepath.Ext(destination))
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("archive source must be a regular file")
	}
	archive, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	var output *os.File
	defer func() {
		if output != nil {
			if err := output.Close(); returnErr == nil && err != nil {
				returnErr = err
			}
		}
		if returnErr != nil {
			_ = os.Remove(destination)
		}
	}()
	regularFiles := 0
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar archive: %w", err)
		}
		cleanName := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("archive contains unsafe path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			regularFiles++
			if regularFiles > 1 {
				return "", fmt.Errorf("archive must contain exactly one regular image file")
			}
			entryFormat, supported := imageFormatForPath(cleanName)
			if !supported || entryFormat != format {
				return "", fmt.Errorf("archive image %q does not match destination format %s", header.Name, format)
			}
			output, err = os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(output, &contextReader{ctx: ctx, reader: tarReader}); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("archive contains unsupported entry %q", header.Name)
		}
	}
	if regularFiles != 1 {
		return "", fmt.Errorf("archive must contain exactly one regular image file")
	}
	return format, nil
}

func imageFormatForPath(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".raw":
		return "RAW", true
	case ".qcow2":
		return "QCOW2", true
	case ".vhd":
		return "VHD", true
	case ".vmdk":
		return "VMDK", true
	default:
		return "", false
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
