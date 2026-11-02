//go:build !windows

package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixUpdateRecoversEveryDirectorySyncFailure(t *testing.T) {
	for failAt := 1; failAt <= 8; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "ecctl")
			candidate := filepath.Join(root, ".ecctl-update-candidate")
			writeTestExecutable(t, target, "old")
			writeTestExecutable(t, candidate, "new")
			original := syncUpdateDirectory
			t.Cleanup(func() { syncUpdateDirectory = original })
			calls := 0
			cause := errors.New("injected directory sync failure")
			syncUpdateDirectory = func(path string) error {
				calls++
				if calls == failAt {
					return cause
				}
				return original(path)
			}
			options := Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}
			_, pending, err := installPreparedExecutable(context.Background(), options, candidate, "1.2.3")
			if !errors.Is(err, cause) || pending {
				t.Fatalf("sync failure %d: pending=%t error=%v calls=%d", failAt, pending, err, calls)
			}
			syncUpdateDirectory = original
			if err := checkPendingInstall(context.Background(), options); err != nil {
				t.Fatalf("sync failure %d recovery: %v", failAt, err)
			}
			want := "old"
			if failAt >= 7 {
				want = "new"
			}
			assertFileContent(t, target, want)
			assertPathMissing(t, filepath.Join(root, unixUpdateStateName))
			assertPathMissing(t, candidate)
			backups, err := filepath.Glob(filepath.Join(root, unixUpdateBackup+"*"))
			if err != nil || len(backups) != 0 {
				t.Fatalf("recovery leaked backups: %v, %v", backups, err)
			}
		})
	}
}

func TestUnixRecoveryPreservesUnverifiableJournalAndBackup(t *testing.T) {
	for _, fault := range []string{"missing backup", "wrong backup", "directory target", "canceled verification"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			token := strings.Repeat("b", 32)
			state := unixUpdateState{Schema: unixUpdateSchema, Token: token, CurrentVersion: "1.2.2", TargetVersion: "1.2.3", Candidate: unixUpdateCandidate + "fixture", Backup: unixUpdateBackup + token, Stage: unixStagePrepared}
			path := filepath.Join(root, "ecctl")
			backup := filepath.Join(root, state.Backup)
			options := Options{Executable: path, RunCommand: unixVersionRunner}
			switch fault {
			case "wrong backup":
				writeTestExecutable(t, backup, "unknown")
			case "directory target":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "canceled verification":
				writeTestExecutable(t, path, "unknown")
				options.RunCommand = func(context.Context, []string, string, ...string) ([]byte, error) { return nil, context.Canceled }
			}
			journal := filepath.Join(root, unixUpdateStateName)
			if err := writeUnixUpdateState(journal, state); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if fault == "canceled verification" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := reconcileUnixUpdate(ctx, options); err == nil {
				t.Fatal("unverifiable interrupted update accepted")
			}
			if _, err := os.Stat(journal); err != nil {
				t.Fatalf("recovery evidence removed: %v", err)
			}
			if fault == "wrong backup" {
				assertFileContent(t, backup, "unknown")
			}
		})
	}
}

func TestUnixUpdateFilesystemGuardsAndFailedStaging(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ecctl")
	if _, err := acquireUnixUpdateLock(filepath.Join(root, "missing", "ecctl")); err == nil {
		t.Fatal("missing lock parent accepted")
	}
	if err := os.Mkdir(filepath.Join(root, unixUpdateLockName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireUnixUpdateLock(path); err == nil {
		t.Fatal("directory update lock accepted")
	}
	if _, _, err := installPreparedExecutable(context.Background(), Options{Executable: path}, "missing", "1.2.3"); err == nil {
		t.Fatal("invalid update lock ignored")
	}
	if err := os.Remove(filepath.Join(root, unixUpdateLockName)); err != nil {
		t.Fatal(err)
	}
	writeTestExecutable(t, path, "unknown")
	if _, _, err := installPreparedExecutable(context.Background(), Options{Executable: path, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}, "missing", "1.2.3"); err == nil {
		t.Fatal("changed current version accepted")
	}
	writeTestExecutable(t, path, "old")
	if _, _, err := installPreparedExecutable(context.Background(), Options{Executable: path, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}, filepath.Join(root, "missing"), "1.2.3"); err == nil {
		t.Fatal("missing candidate accepted")
	}
	if err := requireRegularFile(root); err == nil {
		t.Fatal("directory executable accepted")
	}
	if err := removeRegularFileIfPresent(root); err == nil {
		t.Fatal("directory evidence removed")
	}
	if err := syncDirectory(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing directory synced")
	}
	if err := RunInternalUpdate(nil); err == nil {
		t.Fatal("Windows update protocol accepted on Unix")
	}
	for _, raw := range []string{strings.Repeat("x", unixUpdateStateMax+1), `{} {}`} {
		journal := filepath.Join(root, "journal")
		if err := os.WriteFile(journal, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readUnixUpdateState(journal); err == nil {
			t.Fatal("invalid journal read")
		}
	}
	if _, _, err := readUnixUpdateState(root); err == nil {
		t.Fatal("directory journal accepted")
	}
	if err := writeUnixUpdateState(filepath.Join(root, "missing", "journal"), unixUpdateState{}); err == nil {
		t.Fatal("missing journal parent accepted")
	}
	if err := writeUnixUpdateState(root, unixUpdateState{}); err == nil {
		t.Fatal("directory journal replaced")
	}
	leftovers, err := filepath.Glob(filepath.Join(root, ".ecctl-update-state-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("failed journal write leaked staging: %v, %v", leftovers, err)
	}
	if err := cleanupUnixUpdateState(path, unixUpdateState{Candidate: filepath.Base(root), Backup: "missing"}); err != nil {
		t.Fatalf("absent artifacts cleanup: %v", err)
	}
}

func TestUnixUpdateJournalValidationRejectsInvalidMetadata(t *testing.T) {
	root := t.TempDir()
	token := strings.Repeat("a", 32)
	base := unixUpdateState{Schema: unixUpdateSchema, Token: token, CurrentVersion: "1.2.2", TargetVersion: "1.2.3", Candidate: unixUpdateCandidate + "fixture", Backup: unixUpdateBackup + token, Stage: unixStagePrepared}
	for _, mutate := range []func(*unixUpdateState){
		func(s *unixUpdateState) { s.Schema = -1 }, func(s *unixUpdateState) { s.Token = "invalid" },
		func(s *unixUpdateState) { s.CurrentVersion = "v1.2.2" }, func(s *unixUpdateState) { s.TargetVersion = "invalid" },
		func(s *unixUpdateState) { s.Candidate = "../candidate" }, func(s *unixUpdateState) { s.Backup = "wrong" }, func(s *unixUpdateState) { s.Stage = "unknown" },
	} {
		state := base
		mutate(&state)
		if err := validateUnixUpdateState(root, state); err == nil {
			t.Fatalf("invalid journal accepted: %+v", state)
		}
	}
	if err := os.Mkdir(filepath.Join(root, base.Candidate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateUnixUpdateState(root, base); err == nil {
		t.Fatal("directory candidate accepted")
	}
	if _, err := validatePreparedCandidate(root, filepath.Join(t.TempDir(), ".ecctl-update-foreign")); err == nil {
		t.Fatal("foreign candidate accepted")
	}
	if _, err := validatePreparedCandidate(root, filepath.Join(root, ".ecctl-update-missing")); err == nil {
		t.Fatal("missing candidate accepted")
	}
}
