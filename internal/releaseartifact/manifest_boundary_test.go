package releaseartifact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestProtocolRejectsInvalidMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*UpdateManifestV2){
		"schema":           func(m *UpdateManifestV2) { m.Schema = "unknown" },
		"version":          func(m *UpdateManifestV2) { m.Version = "v1.2.3" },
		"unknown name":     func(m *UpdateManifestV2) { m.Assets[0].Name = "unexpected" },
		"zero size":        func(m *UpdateManifestV2) { m.Assets[0].Size = 0 },
		"oversized":        func(m *UpdateManifestV2) { m.Assets[0].Size = (200 << 20) + 1 },
		"uppercase digest": func(m *UpdateManifestV2) { m.Assets[0].SHA256 = strings.Repeat("A", 64) },
		"nonhex digest":    func(m *UpdateManifestV2) { m.Assets[0].SHA256 = strings.Repeat("z", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := validUpdateManifestV2("1.2.3")
			mutate(&manifest)
			if _, err := MarshalUpdateManifestV2(manifest); err == nil {
				t.Fatal("invalid manifest marshaled")
			}
		})
	}
	for _, raw := range []string{"", `{"schema":`, `{"schema":1}`, `{"unknown":true}`, `[]`, `null`, `{1:2}`, `{"a":[`, `{"a":]}`, `{} {}`, `{} invalid`} {
		if _, err := ParseUpdateManifestV2([]byte(raw)); err == nil {
			t.Fatalf("invalid manifest accepted: %q", raw)
		}
	}
	for _, version := range []string{"1.2.3", "1.2.3-rc.1", "1.2.3-0"} {
		raw, err := MarshalUpdateManifestV2(validUpdateManifestV2(version))
		if err != nil || raw[len(raw)-1] != '\n' {
			t.Fatalf("marshal = %q, %v", raw, err)
		}
		if manifest, err := ParseUpdateManifestV2(raw); err != nil || manifest.Version != version {
			t.Fatalf("roundtrip = %+v, %v", manifest, err)
		}
	}
}

func TestChecksumsProtocolRejectsAmbiguousAndUnsafeRecords(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, raw := range []string{"", "\n", "invalid", strings.ToUpper(sha) + "  archive\n", strings.Repeat("z", 64) + "  archive\n", sha + "  ../archive\n", sha + "  dir\\archive\n", sha + "  archive\n" + sha + "  archive\n"} {
		if _, err := parseReleaseChecksumsV2([]byte(raw)); err == nil {
			t.Fatalf("invalid checksums accepted: %q", raw)
		}
	}
	for _, trailer := range []string{"", "{}", "invalid"} {
		err := requireJSONEOF(json.NewDecoder(strings.NewReader(trailer)))
		if (err == nil) != (trailer == "") {
			t.Fatalf("EOF check %q = %v", trailer, err)
		}
	}
}

func TestSignedManifestRejectsUnboundedOrUntrustedEvidence(t *testing.T) {
	for _, tt := range []struct{ manifest, bundle []byte }{
		{nil, []byte(`{}`)}, {make([]byte, maxSignedUpdateMetadataBytes+1), []byte(`{}`)},
		{[]byte(`{}`), nil}, {[]byte(`{}`), make([]byte, maxSignedUpdateMetadataBytes+1)},
		{[]byte(`{}`), []byte(`invalid`)}, {[]byte(`{}`), []byte(`{}`)},
	} {
		if err := VerifySignedUpdateManifest(tt.manifest, tt.bundle); err == nil {
			t.Fatal("untrusted evidence accepted")
		}
	}
}
