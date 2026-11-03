// Package openapimeta provides pinned OpenAPI metadata embedded at build time.
// Run make metadata-prepare before building; runtime reads never use the network.
package openapimeta

import (
	"embed"
	"io/fs"

	metadata "github.com/aliyun/elastic-compute-control-cli/internal/openapimetaformat"
)

//go:embed all:.generated
var snapshotFiles embed.FS

//go:embed manifest.json
var snapshotManifest []byte

var generatedFiles, _ = fs.Sub(snapshotFiles, ".generated")
var embedded = metadata.NewReader(generatedFiles, snapshotManifest)

// ReadFile verifies and decompresses one logical metadata file.
// Every call returns fresh bytes; concurrent calls are safe.
func ReadFile(path string) ([]byte, error) {
	return embedded.ReadFile(path)
}
