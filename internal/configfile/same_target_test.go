package configfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameTargetUsesFilesystemCaseProbeForAbsentPaths(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "Config.json")
	second := filepath.Join(dir, "config.json")
	original := probeDirectoryCaseInsensitive
	t.Cleanup(func() { probeDirectoryCaseInsensitive = original })
	probeDirectoryCaseInsensitive = func(string) (bool, error) { return false, nil }
	if same, err := SameTarget(first, second); err != nil || same {
		t.Fatalf("case-sensitive same=%t err=%v", same, err)
	}
	probeDirectoryCaseInsensitive = func(string) (bool, error) { return true, nil }
	if same, err := SameTarget(first, second); err != nil || !same {
		t.Fatalf("case-insensitive same=%t err=%v", same, err)
	}
}

func TestSameTargetRecognizesHardLink(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	if err := os.WriteFile(first, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if same, err := SameTarget(first, second); err != nil || !same {
		t.Fatalf("hard-link same=%t err=%v", same, err)
	}
}
