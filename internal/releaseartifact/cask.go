package releaseartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	OSSBaseURL   = "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli"
	Homepage     = "https://github.com/aliyun/elastic-compute-control-cli"
	Description  = "Agent-first command-line controller for Alibaba Cloud elastic computing resources"
	maxCaskBytes = 64 << 10
	CaskFormatV1 = "v1"
	CaskFormatV2 = "v2"
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type CaskExpectation struct {
	Version     string
	IntelSHA256 string
	ArmSHA256   string
}

// ValidateCask preserves the immutable updater Cask v1 protocol without evaluating Ruby.
func ValidateCask(data []byte, expected CaskExpectation) error {
	return ValidateCaskFormat(data, expected, CaskFormatV1)
}

// ValidateCaskFormat accepts only the exact ecctl Cask program for the requested format.
func ValidateCaskFormat(data []byte, expected CaskExpectation, format string) error {
	if len(data) == 0 || len(data) > maxCaskBytes {
		return fmt.Errorf("Homebrew Cask must be between 1 and %d bytes", maxCaskBytes)
	}
	if strings.ContainsAny(string(data), "\r\x00") {
		return errors.New("Homebrew Cask must use Unix text without NUL bytes")
	}
	if !semverPattern.MatchString(expected.Version) {
		return fmt.Errorf("invalid expected Cask version %q", expected.Version)
	}
	for label, digest := range map[string]string{"Intel": expected.IntelSHA256, "Arm": expected.ArmSHA256} {
		if err := validateDigest(digest); err != nil {
			return fmt.Errorf("invalid expected %s checksum: %w", label, err)
		}
	}

	got := significantCaskLines(string(data))
	want, err := expectedCaskFormatLines(expected, format)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("Homebrew Cask has %d significant lines, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			return fmt.Errorf("Homebrew Cask line %d is not allowed: got %q, want %q", index+1, got[index], want[index])
		}
	}
	return nil
}

// ConvertCask changes only the format of an already strictly validated Cask.
// Release assets retain v1 for old updaters; the tap uses v2 install steps.
func ConvertCask(data []byte, expected CaskExpectation, format string) ([]byte, error) {
	lines, err := expectedCaskFormatLines(expected, format)
	if err != nil {
		return nil, err
	}
	if err := ValidateCask(data, expected); err != nil {
		if err := ValidateCaskFormat(data, expected, CaskFormatV2); err != nil {
			return nil, err
		}
	}
	var converted strings.Builder
	indent := 0
	for _, line := range lines {
		if line == "end" {
			indent--
		}
		padding := indent
		if strings.HasPrefix(line, "verified:") {
			padding++
		}
		converted.WriteString(strings.Repeat("  ", padding) + line + "\n")
		if strings.HasSuffix(line, " do") {
			indent++
		}
	}
	return []byte(converted.String()), nil
}

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validateDigest(value string) error {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return errors.New("checksum must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return errors.New("checksum must be hexadecimal")
	}
	return nil
}

func significantCaskLines(raw string) []string {
	lines := make([]string, 0, 32)
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func expectedCaskFormatLines(expected CaskExpectation, format string) ([]string, error) {
	switch format {
	case CaskFormatV1:
		return expectedCaskLines(expected), nil
	case CaskFormatV2:
		return []string{
			`cask "ecctl" do`,
			`version "` + expected.Version + `"`,
			`on_macos do`,
			`on_arm do`,
			`sha256 "` + expected.ArmSHA256 + `"`,
			`url "` + OSSBaseURL + `/#{version}/ecctl_#{version}_darwin_arm64.tar.gz"`,
			`end`,
			`on_intel do`,
			`sha256 "` + expected.IntelSHA256 + `"`,
			`url "` + OSSBaseURL + `/#{version}/ecctl_#{version}_darwin_amd64.tar.gz"`,
			`end`,
			`end`,
			`name "ecctl"`,
			`desc "` + Description + `"`,
			`homepage "` + Homepage + `"`,
			`livecheck do`,
			`skip "Auto-generated on release."`,
			`end`,
			`depends_on :macos`,
			`binary "ecctl"`,
			`postflight_steps do`,
			`on_macos do`,
			`run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{staged_path}}/ecctl"]`,
			`end`,
			`end`,
			`end`,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported Homebrew Cask format %q", format)
	}
}

func expectedCaskLines(expected CaskExpectation) []string {
	verified := strings.TrimPrefix(OSSBaseURL, "https://") + "/"
	intelURL := OSSBaseURL + `/#{version}/ecctl_#{version}_darwin_amd64.tar.gz`
	armURL := OSSBaseURL + `/#{version}/ecctl_#{version}_darwin_arm64.tar.gz`
	return []string{
		`cask "ecctl" do`,
		`version "` + expected.Version + `"`,
		`on_macos do`,
		`on_intel do`,
		`sha256 "` + expected.IntelSHA256 + `"`,
		`url "` + intelURL + `",`,
		`verified: "` + verified + `"`,
		`end`,
		`on_arm do`,
		`sha256 "` + expected.ArmSHA256 + `"`,
		`url "` + armURL + `",`,
		`verified: "` + verified + `"`,
		`end`,
		`end`,
		`name "ecctl"`,
		`desc "` + Description + `"`,
		`homepage "` + Homepage + `"`,
		`livecheck do`,
		`skip "Auto-generated on release."`,
		`end`,
		`binary "ecctl"`,
		`postflight do`,
		`system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/ecctl"]`,
		`end`,
		`end`,
	}
}
