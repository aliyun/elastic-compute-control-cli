package updater

import "testing"

func TestCompareVersionsBoundaries(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"1.0.0", "v1.0.0", 0},
		{"1.0.0-rc.1", "v1.0.0-rc.1", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.beta", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1},
		{"10.0.0", "2.0.0", 1},
		{"18446744073709551616.0.0", "18446744073709551615.0.0", 1},
		{"1.0.0-999999999999999999999999999", "1.0.0-1000000000000000000000000000", -1},
	} {
		t.Run(test.left+"_"+test.right, func(t *testing.T) {
			if got, err := CompareVersions(test.left, test.right); err != nil || got != test.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, %v; want %d", test.left, test.right, got, err, test.want)
			}
			if got, err := CompareVersions(test.right, test.left); err != nil || got != -test.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, %v; want %d", test.right, test.left, got, err, -test.want)
			}
		})
	}
}

func TestVersionsRejectInvalidIdentifiers(t *testing.T) {
	for _, invalid := range []string{"", "1.0", "1.02.3", "1.2.3-", "1.2.3-alpha..1", "1.2.3-alpha_1", "1.2.3-01", "1.2.3+build"} {
		t.Run(invalid, func(t *testing.T) {
			if got, err := CompareVersions(invalid, "1.2.3"); err == nil || got != 0 {
				t.Fatalf("CompareVersions(%q, valid) = %d, %v; want error", invalid, got, err)
			}
			if got, err := CompareVersions("1.2.3", invalid); err == nil || got != 0 {
				t.Fatalf("CompareVersions(valid, %q) = %d, %v; want error", invalid, got, err)
			}
			if got, err := NormalizeVersion(invalid); err == nil || got != "" {
				t.Fatalf("NormalizeVersion(%q) = %q, %v; want error", invalid, got, err)
			}
		})
	}
}

func TestNormalizeVersionTrimsWhitespaceAndPrefix(t *testing.T) {
	if got, err := NormalizeVersion(" \tv1.2.3-rc.1\n"); err != nil || got != "1.2.3-rc.1" {
		t.Fatalf("NormalizeVersion = %q, %v; want 1.2.3-rc.1", got, err)
	}
}
