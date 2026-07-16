package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunPreservesLargeJSONNumbers(t *testing.T) {
	script := filepath.Join(t.TempDir(), "print-json")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '{\"owner_id\":1754580903499898}'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	res := Run(context.Background(), Config{}, script)
	if res.Exit != 0 || res.Err != nil {
		t.Fatalf("Run exit=%d err=%v stderr=%s", res.Exit, res.Err, res.Stderr)
	}
	obj, ok := res.JSON.(map[string]any)
	if !ok {
		t.Fatalf("JSON = %T, want object", res.JSON)
	}
	if got := fmt.Sprint(obj["owner_id"]); got != "1754580903499898" {
		t.Fatalf("owner_id = %q, want exact integer text", got)
	}
}
