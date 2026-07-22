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
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type CaskExpectation struct {
	Version     string
	IntelSHA256 string
	ArmSHA256   string
}

// ValidateCask validates data without evaluating Ruby. The accepted program is
// deliberately limited to the Cask emitted for ecctl by GoReleaser.
func ValidateCask(data []byte, expected CaskExpectation) error {
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
	want := expectedCaskLines(expected)
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
