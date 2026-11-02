package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
)

func captureCommand(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errout, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errout.Close()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errout
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	code := run(args)
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := os.ReadFile(errout.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(raw), string(diagnostic)
}

func TestBaselineAndDetectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	specDir := filepath.Join(dir, "specs")
	if err := os.Mkdir(specDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../specs/ecs/disk.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "disk.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baseline.json")
	code, out, diagnostic := captureCommand(t, "baseline", "-spec-dir", specDir, "-baseline", path, "-lang", "en")
	if code != 0 || !strings.Contains(out, "recorded") || diagnostic != "" {
		t.Fatalf("baseline = %d, %q, %q", code, out, diagnostic)
	}
	base, err := drift.LoadBaseline(path)
	if err != nil || len(base.Bindings) == 0 {
		t.Fatalf("baseline = %+v, %v", base, err)
	}
	for _, format := range []string{"json", "table"} {
		code, out, diagnostic = captureCommand(t, "detect", "-spec-dir", specDir, "-baseline", path, "-lang", "en", "-format", format, "-check")
		if code != 0 || diagnostic != "" {
			t.Fatalf("detect = %d, %q", code, diagnostic)
		}
		if format == "json" {
			var report jsonReport
			if err := json.Unmarshal([]byte(out), &report); err != nil || report.Missing != 0 || report.Removed != 0 || report.Uncovered != 0 || report.BindingsChecked == 0 {
				t.Fatalf("report = %+v, %v", report, err)
			}
		} else if !strings.Contains(out, "bindings checked:") {
			t.Fatalf("table = %q", out)
		}
	}
	base.Bindings = nil // A stale baseline must block newly unacknowledged bindings.
	if err := drift.WriteBaseline(path, base); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic = captureCommand(t, "detect", "-spec-dir", specDir, "-baseline", path, "-check")
	if code != 1 || !strings.Contains(diagnostic, "drift detected") {
		t.Fatalf("drift = %d, %q", code, diagnostic)
	}
	code, _, diagnostic = captureCommand(t, "detect", "-spec-dir", specDir, "-baseline", path, "-format", "bad")
	if code != 2 || !strings.Contains(diagnostic, "unsupported format") {
		t.Fatalf("format = %d, %q", code, diagnostic)
	}
}

func TestCommandsReportInputFailures(t *testing.T) {
	for _, command := range []string{"detect", "baseline", "sync", "render"} {
		for _, flag := range []string{"-h", "-unknown-flag"} {
			want := 2
			if flag == "-h" {
				want = 0
			}
			if code, _, diagnostic := captureCommand(t, command, flag); code != want || diagnostic == "" {
				t.Fatalf("%s %s = %d, %q", command, flag, code, diagnostic)
			}
		}
	}
	missing := filepath.Join(t.TempDir(), "missing")
	for _, args := range [][]string{
		{"detect", "-spec-dir", missing, "-baseline", missing},
		{"baseline", "-spec-dir", missing},
		{"sync", "-dry-run", "-drift", missing},
		{"render", "-input", missing},
	} {
		if code, _, diagnostic := captureCommand(t, args...); code != 2 || !strings.Contains(diagnostic, "specdrift") {
			t.Fatalf("%v = %d, %q", args, code, diagnostic)
		}
	}
}

func TestSyncAndRenderDispatch(t *testing.T) {
	dir := t.TempDir()
	source := "../../internal/specsync/testdata/top-level-string"
	raw, err := os.ReadFile(filepath.Join(source, "specs/fake/fakeres.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fake/fakeres.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(source, "drift.json")
	for _, mode := range []string{"-dry-run", "-write"} {
		code, _, diagnostic := captureCommand(t, "sync", mode, "-drift", report, "-spec-dir", dir)
		if code != 0 {
			t.Fatalf("sync %s = %d, %q", mode, code, diagnostic)
		}
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if (string(actual) == string(raw)) != (mode == "-dry-run") {
			t.Fatalf("unexpected write for %s", mode)
		}
	}
	code, out, diagnostic := captureCommand(t, "render", "-input", report)
	if code != 0 || !strings.Contains(out, "CreateFakeres") {
		t.Fatalf("render = %d, %q, %q", code, out, diagnostic)
	}
}
