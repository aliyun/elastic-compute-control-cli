package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"testing"
)

func TestArchiveExtractionRejectsMalformedAndAmbiguousExecutables(t *testing.T) {
	if _, err := extractExecutable(Artifact{Filename: "unsupported"}, "linux"); err == nil {
		t.Fatal("unknown archive format accepted")
	}
	if _, err := extractTarGzip([]byte("invalid"), "ecctl"); err == nil {
		t.Fatal("invalid gzip accepted")
	}
	if _, err := extractZip([]byte("invalid"), "ecctl"); err == nil {
		t.Fatal("invalid zip accepted")
	}
	for _, kind := range []string{"missing", "empty", "directory", "duplicate", "broken tar", "symlink"} {
		var data bytes.Buffer
		gz := gzip.NewWriter(&data)
		if kind == "broken tar" {
			if _, err := gz.Write([]byte("broken tar header")); err != nil {
				t.Fatal(err)
			}
		} else {
			tw := tar.NewWriter(gz)
			count := 1
			if kind == "duplicate" {
				count = 2
			}
			for range count {
				header := &tar.Header{Name: "ecctl", Typeflag: tar.TypeReg, Mode: 0o755, Size: 3}
				switch kind {
				case "missing":
					header.Name = "readme"
				case "empty":
					header.Size = 0
				case "directory":
					header.Typeflag = tar.TypeDir
					header.Size = 0
				case "symlink":
					header.Typeflag, header.Size, header.Linkname = tar.TypeSymlink, 0, "outside"
				}
				if err := tw.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
				if header.Size > 0 {
					if _, err := tw.Write([]byte("exe")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := extractTarGzip(data.Bytes(), "ecctl"); err == nil {
			t.Fatalf("%s tar accepted", kind)
		}
	}
	for _, kind := range []string{"missing", "empty", "directory", "duplicate", "unsupported compression", "bad checksum", "symlink", "unsafe path"} {
		var data bytes.Buffer
		writer := zip.NewWriter(&data)
		count := 1
		if kind == "duplicate" {
			count = 2
		}
		for range count {
			header := &zip.FileHeader{Name: "ecctl", Method: zip.Store}
			header.SetMode(0o755)
			if kind == "missing" {
				header.Name = "readme"
			}
			if kind == "directory" {
				header.SetMode(os.ModeDir | 0o755)
			}
			if kind == "symlink" {
				header.SetMode(os.ModeSymlink | 0o777)
			}
			if kind == "unsafe path" {
				header.Name = "../ecctl"
			}
			if kind == "unsupported compression" || kind == "bad checksum" {
				header.UncompressedSize64 = 3
				header.CompressedSize64 = 3
				header.CRC32 = 1
				if kind == "unsupported compression" {
					header.Method = 99
				}
				stream, err := writer.CreateRaw(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := stream.Write([]byte("exe")); err != nil {
					t.Fatal(err)
				}
			} else {
				stream, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if kind != "empty" && kind != "directory" {
					if _, err := stream.Write([]byte("exe")); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := extractZip(data.Bytes(), "ecctl"); err == nil {
			t.Fatalf("%s zip accepted", kind)
		}
	}
	for _, name := range []string{"", "/ecctl", `dir\ecctl`} {
		if safeArchivePath(name) {
			t.Fatalf("unsafe archive path accepted: %q", name)
		}
	}
}
