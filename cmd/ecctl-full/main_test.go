package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExposesResourcesHiddenFromPublicCLI(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"lingjun", "vcc", "list", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "list") {
		t.Fatalf("full command help = %q, want list operation", stdout.String())
	}
}

func TestRunReportsFullSurface(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--no-color", "capabilities", "--output", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("capabilities exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"surface":"full"`) {
		t.Fatalf("capabilities = %q, want full surface marker", stdout.String())
	}
}
