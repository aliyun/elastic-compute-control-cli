package runner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

type archiveEntry struct {
	name     string
	typeflag byte
	body     string
	linkname string
}

func writeTarGzip(t *testing.T, path string, entries ...archiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0o600, Typeflag: typeflag, Linkname: entry.linkname}
		if typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRunLocalActionExtractsSupportedImageFiles(t *testing.T) {
	for _, test := range []struct {
		extension string
		format    string
	}{
		{extension: ".raw", format: "RAW"},
		{extension: ".qcow2", format: "QCOW2"},
	} {
		t.Run(test.format, func(t *testing.T) {
			workDir := t.TempDir()
			source := filepath.Join(workDir, "export"+test.extension+".tar.gz")
			destination := filepath.Join(workDir, "import"+test.extension)
			writeTarGzip(t, source, archiveEntry{name: "disk" + test.extension, body: "image"})

			result := runLocalAction(context.Background(), map[string]any{"work_dir": workDir}, scenario.LocalAction{
				Action: "extract-tar-gzip", Source: source, Destination: destination,
			})
			if result.Err != nil || result.Exit != 0 {
				t.Fatalf("runLocalAction = exit %d err %v", result.Exit, result.Err)
			}
			if got, err := os.ReadFile(destination); err != nil || string(got) != "image" {
				t.Fatalf("destination = %q, %v", got, err)
			}
			artifact := result.JSON.(map[string]any)["artifact"].(map[string]any)
			if artifact["file"] != destination || artifact["format"] != test.format {
				t.Fatalf("artifact = %#v", artifact)
			}
		})
	}
}

func TestRunLocalActionRejectsUnsafeArchives(t *testing.T) {
	tests := []struct {
		name    string
		entries []archiveEntry
	}{
		{name: "traversal", entries: []archiveEntry{{name: "../disk.raw", body: "raw"}}},
		{name: "link", entries: []archiveEntry{{name: "disk.raw", typeflag: tar.TypeSymlink, linkname: "/tmp/target"}}},
		{name: "multiple", entries: []archiveEntry{{name: "one.raw", body: "one"}, {name: "two.raw", body: "two"}}},
		{name: "mismatched format", entries: []archiveEntry{{name: "disk.qcow2", body: "image"}}},
		{name: "unsupported format", entries: []archiveEntry{{name: "disk.iso", body: "image"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			source := filepath.Join(workDir, "export.tar.gz")
			destination := filepath.Join(workDir, "import.raw")
			writeTarGzip(t, source, test.entries...)
			result := runLocalAction(context.Background(), map[string]any{"work_dir": workDir}, scenario.LocalAction{
				Action: "extract-tar-gzip", Source: source, Destination: destination,
			})
			if result.Err == nil || result.Exit != -1 {
				t.Fatalf("unsafe archive result = exit %d err %v", result.Exit, result.Err)
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("partial destination remains: %v", err)
			}
		})
	}
}

func TestRunLocalActionConfinesPathsToWorkDirectory(t *testing.T) {
	workDir := t.TempDir()
	source := filepath.Join(workDir, "export.tar.gz")
	writeTarGzip(t, source, archiveEntry{name: "disk.raw", body: "raw"})
	result := runLocalAction(context.Background(), map[string]any{"work_dir": workDir}, scenario.LocalAction{
		Action: "extract-tar-gzip", Source: source, Destination: filepath.Join(t.TempDir(), "outside.raw"),
	})
	if result.Err == nil {
		t.Fatal("outside destination was accepted")
	}
}

func TestOSSBucketNameIsValidAndExecutionUnique(t *testing.T) {
	startedAt := time.Date(2026, 8, 5, 3, 4, 5, 6, time.UTC)
	first := ossBucketName("run", "execution-a", startedAt)
	second := ossBucketName("run", "execution-b", startedAt)
	if first == second {
		t.Fatalf("bucket names are not execution unique: %q", first)
	}
	if len(first) > 63 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]+[a-z0-9]$`).MatchString(first) {
		t.Fatalf("invalid bucket name %q", first)
	}
}
