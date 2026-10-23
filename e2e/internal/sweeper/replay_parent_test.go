package sweeper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

func TestReplayPinsOriginalJournalParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test renames a directory containing an open lock")
	}
	for _, legacy := range []bool{false, true} {
		for _, stage := range []string{"resolve-lost", "resolve-copy", "read-copy", "success-copy", "write-copy"} {
			t.Run(stage+"/legacy="+map[bool]string{true: "true", false: "false"}[legacy], func(t *testing.T) {
				root := t.TempDir()
				parent := filepath.Join(root, "journal-parent")
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(parent, "cleanup.json")
				fake := filepath.Join(root, "ecctl")
				calls := filepath.Join(root, "calls")
				t.Setenv("FAKE_LOG", calls)
				entries := []report.Resource{{Scope: "case", Teardown: "ecctl t thing delete parent"}, {Scope: "case", Teardown: "ecctl t thing delete child"}}
				journal := report.CleanupJournal{Version: 2, JournalPath: path, RunID: "run", Region: "cn-hangzhou", Surface: "public", EcctlBin: fake, Entries: entries}
				var value any = journal
				if legacy {
					value = entries
				}
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\necho '{\"deleted\":true}'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				moved := false
				swap := func(string) {
					if moved {
						return
					}
					moved = true
					if err := os.Rename(parent, parent+"-old"); err != nil {
						t.Fatal(err)
					}
					if strings.HasSuffix(stage, "copy") {
						if err := os.Mkdir(parent, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, body, 0o600); err != nil {
							t.Fatal(err)
						}
					}
				}
				oldResolve, oldRead, oldSuccess, oldWrite := replayJournalAfterResolve, replayJournalAfterRead, replayJournalAfterSuccess, writeReplayConsumptionJournal
				t.Cleanup(func() {
					replayJournalAfterResolve, replayJournalAfterRead, replayJournalAfterSuccess, writeReplayConsumptionJournal = oldResolve, oldRead, oldSuccess, oldWrite
				})
				switch {
				case strings.HasPrefix(stage, "resolve"):
					replayJournalAfterResolve = swap
				case strings.HasPrefix(stage, "read"):
					replayJournalAfterRead = swap
				case strings.HasPrefix(stage, "write"):
					writeReplayConsumptionJournal = func(path string, data []byte) error {
						if err := oldWrite(path, data); err != nil {
							return err
						}
						swap(path)
						return nil
					}
				default:
					replayJournalAfterSuccess = swap
				}
				result, err := ReplayJournalWithOptions(context.Background(), path, ReplayOptions{Config: execpkg.Config{Bin: fake, Region: "cn-hangzhou"}, RunID: "run", Surface: "public"})
				if err == nil || !moved {
					t.Fatalf("replay followed changed parent: result=%+v err=%v moved=%t", result, err, moved)
				}
				if stage == "resolve-lost" {
					if _, err := os.Stat(parent); !os.IsNotExist(err) {
						t.Fatalf("replay recreated lost parent: %v", err)
					}
				} else if _, err := os.Stat(path + ".replay.lock"); !os.IsNotExist(err) {
					t.Fatalf("replay created a lock in replacement parent: %v", err)
				}
				callBody, readErr := os.ReadFile(calls)
				if stage == "success-copy" || stage == "write-copy" {
					if readErr != nil || strings.Count(string(callBody), "delete") != 1 || !strings.Contains(string(callBody), "delete child") || strings.Contains(string(callBody), "delete parent") {
						t.Fatalf("replay continued to parent after swap: calls=%q err=%v", callBody, readErr)
					}
				} else if !os.IsNotExist(readErr) {
					t.Fatalf("replay called provider after parent swap: calls=%q err=%v", callBody, readErr)
				}
			})
		}
	}
}
