package releaseartifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestBuilderRejectsIncompleteOrInconsistentRelease(t *testing.T) {
	for _, fault := range []string{"version syntax", "version missing", "version mismatch", "checksums missing", "archive missing", "digest mismatch", "extra asset", "cask missing", "cask unreadable", "prerelease cask"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			version := "1.2.3"
			if fault == "prerelease cask" {
				version = "1.2.3-rc.1"
			}
			versionPath, caskPath := filepath.Join(dir, "version.txt"), filepath.Join(dir, "ecctl.rb")
			write := func(path, raw string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(versionPath, version+"\n")
			write(caskPath, "cask bytes")
			var sums strings.Builder
			for _, goos := range []string{"darwin", "linux", "windows"} {
				for _, arch := range []string{"amd64", "arm64"} {
					name := UpdateArchiveName(version, goos, arch)
					write(filepath.Join(dir, name), name)
					fmt.Fprintf(&sums, "%s  %s\n", SHA256([]byte(name)), name)
				}
			}
			checksums := filepath.Join(dir, "checksums.txt")
			write(checksums, sums.String())
			want := ""
			switch fault {
			case "version syntax":
				version, want = "bad", "version"
			case "version missing":
				versionPath += ".missing"
				want = "read version.txt"
			case "version mismatch":
				write(versionPath, "1.2.4\n")
				want = "version.txt does not contain"
			case "checksums missing":
				if err := os.Remove(checksums); err != nil {
					t.Fatal(err)
				}
				want = "read checksums.txt"
			case "archive missing":
				if err := os.Remove(filepath.Join(dir, UpdateArchiveName(version, "darwin", "amd64"))); err != nil {
					t.Fatal(err)
				}
				want = "read release archive"
			case "digest mismatch":
				write(filepath.Join(dir, UpdateArchiveName(version, "darwin", "amd64")), "tampered")
				want = "does not match the archive"
			case "extra asset":
				write(checksums, sums.String()+strings.Repeat("a", 64)+"  unexpected.zip\n")
				want = "unexpected release asset"
			case "cask missing":
				caskPath = ""
				want = "requires a Homebrew Cask"
			case "cask unreadable":
				caskPath += ".missing"
				want = "read Homebrew Cask"
			case "prerelease cask":
				want = "must not include a Homebrew Cask"
			}
			if _, err := BuildUpdateManifestV2(version, dir, versionPath, caskPath); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %v", fault, err)
			}
		})
	}
}

func TestManifestAssetReaderRejectsEmptyDirectoryAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, raw := range []string{"", "oversized"} {
		path := filepath.Join(dir, "asset")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readUpdateAsset(path, 1); err == nil {
			t.Fatal("invalid asset accepted")
		}
	}
	if _, err := readUpdateAsset(dir, 1024); err == nil {
		t.Fatal("directory accepted as asset")
	}
	if _, err := MarshalUpdateManifestV2(UpdateManifestV2{}); err == nil {
		t.Fatal("invalid manifest serialized")
	}
}

func TestCaskValidationRejectsUnsafeTextAndInvalidExpectedMetadata(t *testing.T) {
	valid := CaskExpectation{Version: "1.2.3", IntelSHA256: strings.Repeat("a", 64), ArmSHA256: strings.Repeat("b", 64)}
	for _, raw := range []string{"", strings.Repeat("a", maxCaskBytes+1), "cask\r\n", "cask\x00"} {
		if err := ValidateCask([]byte(raw), valid); err == nil {
			t.Fatal("unsafe cask text accepted")
		}
	}
	for _, expected := range []CaskExpectation{{Version: "bad"}, {Version: "1.2.3", IntelSHA256: "short"}, {Version: "1.2.3", IntelSHA256: strings.Repeat("z", 64)}} {
		if err := ValidateCask([]byte("cask fixture"), expected); err == nil {
			t.Fatal("invalid expected cask metadata accepted")
		}
	}
}
