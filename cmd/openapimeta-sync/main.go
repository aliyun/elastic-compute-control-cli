// Command openapimeta-sync explicitly refreshes the bundled OpenAPI metadata
// from a local GitHub source tar.gz. It never fetches data or extracts the tar.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "openapimeta-sync: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("openapimeta-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archive := flags.String("archive", "", "local upstream tar.gz (no network access)")
	revision := flags.String("revision", "", "full 40-character upstream Git commit SHA")
	out := flags.String("out", "internal/openapimeta", "destination directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *archive == "" || *out == "" {
		return errors.New("usage: openapimeta-sync -archive local.tar.gz -revision <40-character SHA> [-out internal/openapimeta]")
	}
	if _, err := normalizeRevision(*revision); err != nil {
		return err
	}
	input, err := os.Open(*archive)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("archive must be a regular local file")
	}
	result, err := generate(input, *revision)
	if err != nil {
		return err
	}
	if err := writeSnapshot(*out, info, result); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%d products, %d operations, %d files; snapshot.zip: %d bytes\narchive_sha256: %s\nsnapshot_sha256: %s\n",
		result.manifest.Products, result.manifest.Operations, result.manifest.Files,
		len(result.zip), result.manifest.ArchiveSHA256, result.manifest.SnapshotSHA256)
	return err
}

// Only these three owned artifacts are written, after all input validation and
// compression succeed. No extraction directory, temporary file, or deletion is
// needed. Write the manifest last so an interrupted refresh fails the reader's
// checksum check rather than silently accepting a mismatched snapshot.
func writeSnapshot(out string, input os.FileInfo, result *generatedSnapshot) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	info, err := os.Lstat(out)
	if errors.Is(err, os.ErrNotExist) {
		parent, statErr := os.Stat(filepath.Dir(out))
		if statErr != nil {
			return statErr
		}
		if !parent.IsDir() {
			return fmt.Errorf("output parent is not a directory: %s", filepath.Dir(out))
		}
		if err := os.Mkdir(out, 0o755); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("output must be a directory, not a symlink or file: %s", out)
	}
	artifacts := []struct {
		name string
		data []byte
	}{
		{"snapshot.zip", result.zip},
		{"LICENSE", result.license},
		{"manifest.json", result.manifestJSON},
	}
	for _, artifact := range artifacts {
		name := filepath.Join(out, artifact.name)
		info, err := os.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || os.SameFile(input, info) {
			return fmt.Errorf("refusing to overwrite non-regular file or input archive: %s", name)
		}
	}
	for _, artifact := range artifacts {
		if err := os.WriteFile(filepath.Join(out, artifact.name), artifact.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
