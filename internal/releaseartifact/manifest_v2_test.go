package releaseartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUpdateManifestV2BindsCompleteReleaseAssets(t *testing.T) {
	for _, version := range []string{"1.2.3", "1.2.4-rc.1"} {
		t.Run(version, func(t *testing.T) {
			dist := t.TempDir()
			versionFile := filepath.Join(t.TempDir(), "version.txt")
			if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var checksums strings.Builder
			for _, goos := range []string{"darwin", "linux", "windows"} {
				for _, goarch := range []string{"amd64", "arm64"} {
					name := UpdateArchiveName(version, goos, goarch)
					raw := []byte("archive " + name)
					if err := os.WriteFile(filepath.Join(dist, name), raw, 0o600); err != nil {
						t.Fatal(err)
					}
					digest := sha256.Sum256(raw)
					fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(digest[:]), name)
				}
			}
			if err := os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte(checksums.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			caskPath := ""
			if !strings.Contains(version, "-") {
				caskPath = filepath.Join(t.TempDir(), "ecctl.rb")
				if err := os.WriteFile(caskPath, []byte("stable cask\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			manifest, err := BuildUpdateManifestV2(version, dist, versionFile, caskPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateUpdateManifestV2(manifest); err != nil {
				t.Fatal(err)
			}
			for _, asset := range manifest.Assets {
				if asset.Name == "version.txt" || asset.Kind == "version" {
					t.Fatal("manifest must not require a versioned version.txt object")
				}
				var assetPath string
				switch asset.Kind {
				case "homebrew-cask":
					assetPath = caskPath
				default:
					assetPath = filepath.Join(dist, asset.Name)
				}
				raw, err := os.ReadFile(assetPath)
				if err != nil {
					t.Fatal(err)
				}
				if asset.Size != int64(len(raw)) || asset.SHA256 != SHA256(raw) {
					t.Fatalf("asset %s = %#v", asset.Name, asset)
				}
			}

			broken := append([]byte(checksums.String()), []byte("0  unexpected\n")...)
			if err := os.WriteFile(filepath.Join(dist, "checksums.txt"), broken, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildUpdateManifestV2(version, dist, versionFile, caskPath); err == nil {
				t.Fatal("manifest builder accepted checksums with an unexpected asset")
			}
		})
	}
}

func TestParseUpdateManifestV2RejectsDuplicateJSONFields(t *testing.T) {
	manifest := validUpdateManifestV2("1.2.3")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	duplicateTopLevel := strings.Replace(string(raw), `{"schema":`, `{"schema":"ecctl-update-manifest-v2","schema":`, 1)
	duplicateAsset := strings.Replace(string(raw), `"name":"checksums.txt"`, `"name":"checksums.txt","name":"checksums.txt"`, 1)
	for name, candidate := range map[string]string{
		"top level": duplicateTopLevel,
		"asset":     duplicateAsset,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseUpdateManifestV2([]byte(candidate)); err == nil || !strings.Contains(err.Error(), "duplicate JSON field") {
				t.Fatalf("duplicate manifest error = %v", err)
			}
		})
	}
}

func TestValidateUpdateManifestV2RejectsInvalidAssets(t *testing.T) {
	base := validUpdateManifestV2("1.2.3")
	for name, mutate := range map[string]func(*UpdateManifestV2){
		"unknown platform": func(m *UpdateManifestV2) { m.Assets[0].GOOS = "plan9" },
		"path separator":   func(m *UpdateManifestV2) { m.Assets[0].Name = "nested/archive" },
		"empty digest":     func(m *UpdateManifestV2) { m.Assets[0].SHA256 = "" },
		"duplicate asset":  func(m *UpdateManifestV2) { m.Assets[1] = m.Assets[0] },
		"missing asset":    func(m *UpdateManifestV2) { m.Assets = m.Assets[:len(m.Assets)-1] },
		"version mismatch": func(m *UpdateManifestV2) { m.Prerelease = true },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.Assets = append([]UpdateAssetV2(nil), base.Assets...)
			mutate(&candidate)
			if err := ValidateUpdateManifestV2(candidate); err == nil {
				t.Fatal("invalid update manifest was accepted")
			}
		})
	}
}

func TestValidateUpdateManifestV2RejectsNonCanonicalSemVer(t *testing.T) {
	if err := ValidateUpdateManifestV2(validUpdateManifestV2("1.2.3-01")); err == nil {
		t.Fatal("update manifest accepted a numeric prerelease identifier with a leading zero")
	}
}

func validUpdateManifestV2(version string) UpdateManifestV2 {
	expected := expectedUpdateAssetsV2(version)
	assets := make([]UpdateAssetV2, 0, len(expected))
	for _, kind := range []string{"archive", "checksums", "version", "homebrew-cask"} {
		for _, asset := range expected {
			if asset.Kind != kind {
				continue
			}
			asset.SHA256 = strings.Repeat("a", 64)
			asset.Size = 1
			assets = append(assets, asset)
		}
	}
	return UpdateManifestV2{
		Schema: UpdateManifestV2Schema, Version: version,
		Prerelease: strings.Contains(version, "-"), Assets: assets,
	}
}
