package releaseartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	UpdateManifestV2Schema     = "ecctl-update-manifest-v2"
	UpdateManifestV2Name       = "ecctl-update-manifest-v2.json"
	UpdateManifestV2BundleName = "ecctl-update-manifest-v2.sigstore.json"
)

type UpdateManifestV2 struct {
	Schema     string          `json:"schema"`
	Version    string          `json:"version"`
	Prerelease bool            `json:"prerelease"`
	Assets     []UpdateAssetV2 `json:"assets"`
}

type UpdateAssetV2 struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func BuildUpdateManifestV2(version, distDir, versionPath, caskPath string) (UpdateManifestV2, error) {
	manifest := UpdateManifestV2{
		Schema: UpdateManifestV2Schema, Version: version,
		Prerelease: strings.Contains(version, "-"),
	}
	if err := validateUpdateVersion(version); err != nil {
		return manifest, err
	}
	versionRaw, err := readUpdateAsset(versionPath, 1024)
	if err != nil {
		return manifest, fmt.Errorf("read version.txt: %w", err)
	}
	if string(versionRaw) != version+"\n" {
		return manifest, errors.New("version.txt does not contain the release version as one newline-terminated line")
	}
	checksumsPath := filepath.Join(distDir, "checksums.txt")
	checksumsRaw, err := readUpdateAsset(checksumsPath, 1<<20)
	if err != nil {
		return manifest, fmt.Errorf("read checksums.txt: %w", err)
	}
	checksums, err := parseReleaseChecksumsV2(checksumsRaw)
	if err != nil {
		return manifest, err
	}
	manifest.Assets = append(manifest.Assets,
		updateAssetFromBytes("checksums.txt", "checksums", "", "", checksumsRaw),
	)
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			name := UpdateArchiveName(version, goos, goarch)
			raw, err := readUpdateAsset(filepath.Join(distDir, name), 200<<20)
			if err != nil {
				return manifest, fmt.Errorf("read release archive %s: %w", name, err)
			}
			digest := SHA256(raw)
			if checksums[name] != digest {
				return manifest, fmt.Errorf("checksums.txt digest for %s does not match the archive", name)
			}
			delete(checksums, name)
			manifest.Assets = append(manifest.Assets, updateAssetFromBytes(name, "archive", goos, goarch, raw))
		}
	}
	if len(checksums) != 0 {
		return manifest, errors.New("checksums.txt contains an unexpected release asset")
	}
	if manifest.Prerelease {
		if caskPath != "" {
			return manifest, errors.New("prerelease update manifest must not include a Homebrew Cask")
		}
	} else {
		if caskPath == "" {
			return manifest, errors.New("stable update manifest requires a Homebrew Cask")
		}
		caskRaw, err := readUpdateAsset(caskPath, maxCaskBytes)
		if err != nil {
			return manifest, fmt.Errorf("read Homebrew Cask: %w", err)
		}
		manifest.Assets = append(manifest.Assets, updateAssetFromBytes("ecctl_"+version+"_cask.rb", "homebrew-cask", "", "", caskRaw))
	}
	if err := ValidateUpdateManifestV2(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func MarshalUpdateManifestV2(manifest UpdateManifestV2) ([]byte, error) {
	if err := ValidateUpdateManifestV2(manifest); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func readUpdateAsset(filename string, limit int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, fmt.Errorf("asset must be a regular file between 1 and %d bytes", limit)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != info.Size() {
		return nil, errors.New("asset size changed while it was read")
	}
	return raw, nil
}

func updateAssetFromBytes(name, kind, goos, goarch string, raw []byte) UpdateAssetV2 {
	return UpdateAssetV2{
		Name: name, Kind: kind, GOOS: goos, GOARCH: goarch,
		SHA256: SHA256(raw), Size: int64(len(raw)),
	}
}

func parseReleaseChecksumsV2(raw []byte) (map[string]string, error) {
	checksums := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != strings.ToLower(fields[0]) || len(fields[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid checksums.txt line %q", line)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil || path.Base(fields[1]) != fields[1] || strings.Contains(fields[1], `\`) {
			return nil, fmt.Errorf("invalid checksums.txt line %q", line)
		}
		if _, exists := checksums[fields[1]]; exists {
			return nil, fmt.Errorf("duplicate checksum for %s", fields[1])
		}
		checksums[fields[1]] = fields[0]
	}
	if len(checksums) == 0 {
		return nil, errors.New("checksums.txt is empty")
	}
	return checksums, nil
}

func ParseUpdateManifestV2(raw []byte) (UpdateManifestV2, error) {
	var manifest UpdateManifestV2
	if err := rejectDuplicateJSONFields(raw); err != nil {
		return manifest, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode update manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return manifest, err
	}
	if err := ValidateUpdateManifestV2(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func rejectDuplicateJSONFields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walkValue func() error
	walkValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("update manifest contains a non-string JSON field")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = struct{}{}
				if err := walkValue(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	if err := walkValue(); err != nil {
		return fmt.Errorf("decode update manifest: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("update manifest contains trailing JSON")
		}
		return fmt.Errorf("decode update manifest trailer: %w", err)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("update manifest contains trailing JSON")
		}
		return fmt.Errorf("decode update manifest trailer: %w", err)
	}
	return nil
}

func ValidateUpdateManifestV2(manifest UpdateManifestV2) error {
	if manifest.Schema != UpdateManifestV2Schema {
		return fmt.Errorf("update manifest schema must be %q", UpdateManifestV2Schema)
	}
	if err := validateUpdateVersion(manifest.Version); err != nil {
		return err
	}
	if manifest.Prerelease != strings.Contains(manifest.Version, "-") {
		return errors.New("update manifest prerelease flag does not match its version")
	}
	expected := expectedUpdateAssetsV2(manifest.Version)
	if len(manifest.Assets) != len(expected) {
		return fmt.Errorf("update manifest contains %d assets, want %d", len(manifest.Assets), len(expected))
	}
	seen := make(map[string]struct{}, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		if asset.Name == "" || path.Base(asset.Name) != asset.Name || strings.Contains(asset.Name, `\`) {
			return fmt.Errorf("invalid update asset name %q", asset.Name)
		}
		if _, ok := seen[asset.Name]; ok {
			return fmt.Errorf("duplicate update asset %q", asset.Name)
		}
		seen[asset.Name] = struct{}{}
		want, ok := expected[asset.Name]
		if !ok {
			return fmt.Errorf("unknown update asset %q", asset.Name)
		}
		if asset.Kind != want.Kind || asset.GOOS != want.GOOS || asset.GOARCH != want.GOARCH {
			return fmt.Errorf("update asset %s has invalid kind or platform", asset.Name)
		}
		if asset.Size < 1 || asset.Size > 200<<20 {
			return fmt.Errorf("update asset %s has invalid size %d", asset.Name, asset.Size)
		}
		if len(asset.SHA256) != sha256.Size*2 || strings.ToLower(asset.SHA256) != asset.SHA256 {
			return fmt.Errorf("update asset %s has invalid SHA-256 digest", asset.Name)
		}
		if _, err := hex.DecodeString(asset.SHA256); err != nil {
			return fmt.Errorf("update asset %s has invalid SHA-256 digest", asset.Name)
		}
	}
	return nil
}

func validateUpdateVersion(version string) error {
	if !semverPattern.MatchString(version) || strings.HasPrefix(version, "v") || strings.Contains(version, "+") {
		return fmt.Errorf("invalid update manifest version %q", version)
	}
	_, prerelease, hasPrerelease := strings.Cut(version, "-")
	if !hasPrerelease {
		return nil
	}
	for _, identifier := range strings.Split(prerelease, ".") {
		numeric := true
		for _, character := range identifier {
			if character < '0' || character > '9' {
				numeric = false
				break
			}
		}
		if numeric && len(identifier) > 1 && identifier[0] == '0' {
			return fmt.Errorf("numeric prerelease identifier %q has a leading zero", identifier)
		}
	}
	return nil
}

func expectedUpdateAssetsV2(version string) map[string]UpdateAssetV2 {
	expected := map[string]UpdateAssetV2{
		"checksums.txt": {Name: "checksums.txt", Kind: "checksums"},
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			name := UpdateArchiveName(version, goos, goarch)
			expected[name] = UpdateAssetV2{Name: name, Kind: "archive", GOOS: goos, GOARCH: goarch}
		}
	}
	if !strings.Contains(version, "-") {
		name := "ecctl_" + version + "_cask.rb"
		expected[name] = UpdateAssetV2{Name: name, Kind: "homebrew-cask"}
	}
	return expected
}

func UpdateArchiveName(version, goos, goarch string) string {
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return "ecctl_" + version + "_" + goos + "_" + goarch + extension
}
