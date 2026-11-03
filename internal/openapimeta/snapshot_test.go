package openapimeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"testing"

	metadata "github.com/aliyun/elastic-compute-control-cli/internal/openapimetaformat"
)

func TestEmbeddedDefaultVersionsAndCounts(t *testing.T) {
	manifest, err := metadata.DecodeManifest(snapshotManifest)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile("metadatas/products.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Products []struct {
			Code                 string   `json:"code"`
			PluginDefaultVersion string   `json:"plugin_default_version"`
			Versions             []string `json:"versions"`
		} `json:"products"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if manifest.Products == 0 || len(catalog.Products) != manifest.Products || len(manifest.DefaultVersions) != manifest.Products {
		t.Fatalf("product count: catalog=%d, manifest=%d, inventory=%d", len(catalog.Products), manifest.Products, len(manifest.DefaultVersions))
	}
	sort.Slice(catalog.Products, func(i, j int) bool {
		return strings.ToLower(catalog.Products[i].Code) < strings.ToLower(catalog.Products[j].Code)
	})
	seen := map[string]bool{"metadatas/products.json": true, "LICENSE": true}
	products := make(map[string]bool)
	count := 0
	for i, product := range catalog.Products {
		code := strings.ToLower(product.Code)
		if code == "" || products[code] {
			t.Fatalf("invalid or duplicate product %q", code)
		}
		products[code] = true
		found := false
		for _, version := range product.Versions {
			found = found || version == product.PluginDefaultVersion
		}
		if !found || product.PluginDefaultVersion == "" {
			t.Fatalf("%s default version is not in versions", product.Code)
		}
		prefix := "canonical/" + code + "/" + product.PluginDefaultVersion + "/"
		name := prefix + "version.json"
		data, err := ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		var version struct {
			Version string                     `json:"version"`
			APIs    map[string]json.RawMessage `json:"apis"`
		}
		if err := json.Unmarshal(data, &version); err != nil {
			t.Fatal(err)
		}
		if version.Version != product.PluginDefaultVersion || version.APIs == nil {
			t.Fatalf("%s: invalid version manifest", name)
		}
		inventory := manifest.DefaultVersions[i]
		if inventory.Code != product.Code || inventory.CanonicalCode != code || inventory.Version != version.Version || inventory.Operations != len(version.APIs) {
			t.Fatalf("%s: manifest inventory mismatch: %+v", product.Code, inventory)
		}
		for op := range version.APIs {
			name := prefix + op + ".json"
			data, err := ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				Name       string            `json:"name"`
				Parameters []json.RawMessage `json:"parameters"`
			}
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if record.Name != op || record.Parameters == nil {
				t.Fatalf("%s: incomplete declared operation", name)
			}
			seen[name] = true
			count++
		}
	}
	license, err := ReadFile("LICENSE")
	if err != nil || !bytes.Contains(license, []byte("Apache License")) {
		t.Fatalf("upstream license: %v", err)
	}
	if manifest.Operations == 0 || count != manifest.Operations || len(seen) != manifest.Files || manifest.Files != 2+manifest.Products+manifest.Operations {
		t.Fatalf("counts: products=%d, operations=%d/%d, files=%d/%d", manifest.Products, count, manifest.Operations, len(seen), manifest.Files)
	}
	index, err := fs.ReadFile(generatedFiles, "index.json")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := metadata.DecodeIndex(index, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !seen[entry.Path] {
			t.Fatalf("undeclared file or version: %s", entry.Path)
		}
	}
	if err := embedded.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileErrors(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../LICENSE", "/LICENSE", "canonical//ecs", "canonical/./ecs", "canonical/../LICENSE", `canonical\ecs`, "C:/LICENSE", "nul\x00"} {
		if _, err := ReadFile(path); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("ReadFile(%q) = %v, want fs.ErrInvalid", path, err)
		}
	}
	if _, err := ReadFile("does-not-exist.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
}
