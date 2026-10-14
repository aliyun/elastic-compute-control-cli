package journalfile

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDurableIntentReservationIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.json")
	var winners atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if WriteExclusiveDurable(path, []byte("owned intent\n")) == nil {
				winners.Add(1)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("intent reservation winners = %d", winners.Load())
	}
	if err := WriteExclusiveDurable(path, []byte("foreign intent\n")); err == nil {
		t.Fatal("overwrote reserved intent")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "owned intent\n" {
		t.Fatalf("intent changed: %q %v", body, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("intent mode = %v", info.Mode())
	}
	if err := WriteDurable(path, []byte("journal replacement\n")); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != "journal replacement\n" {
		t.Fatalf("journal replacement = %q %v", body, err)
	}
}
