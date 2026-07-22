//go:build !windows

package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnixReplacementUsesRecoverableJournal(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl")
	candidate := filepath.Join(root, ".ecctl-update-candidate")
	writeTestExecutable(t, target, "old")
	writeTestExecutable(t, candidate, "new")
	if _, _, err := installPreparedExecutable(context.Background(), Options{
		Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner,
	}, candidate, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "new")
	assertPathMissing(t, filepath.Join(root, unixUpdateStateName))
	assertPathMissing(t, candidate)
	backups, err := filepath.Glob(filepath.Join(root, unixUpdateBackup+"*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup files = %v, %v", backups, err)
	}
	if info, err := os.Lstat(filepath.Join(root, unixUpdateLockName)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("persistent update lock = %v, %v", info, err)
	}
}

func TestUnixReplacementSyncFailureRestoresOldExecutable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl")
	candidate := filepath.Join(root, ".ecctl-update-candidate")
	writeTestExecutable(t, target, "old")
	writeTestExecutable(t, candidate, "new")
	originalSync := syncUpdateDirectory
	injected := false
	syncUpdateDirectory = func(path string) error {
		raw, _ := os.ReadFile(target)
		if string(raw) == "new" && !injected {
			injected = true
			return errors.New("injected installed-directory sync failure")
		}
		return originalSync(path)
	}
	t.Cleanup(func() { syncUpdateDirectory = originalSync })
	_, _, err := installPreparedExecutable(context.Background(), Options{
		Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner,
	}, candidate, "1.2.3")
	if err == nil || !injected {
		t.Fatalf("replacement error = %v, injected=%t", err, injected)
	}
	assertFileContent(t, target, "old")
	assertPathMissing(t, filepath.Join(root, unixUpdateStateName))
}

func TestUnixReconcilesRecordedCrashStates(t *testing.T) {
	for _, test := range []struct {
		name          string
		stage         string
		targetContent string
		candidate     bool
		backup        bool
		wantTarget    string
	}{
		{name: "prepared target old", stage: unixStagePrepared, targetContent: "old", candidate: true, wantTarget: "old"},
		{name: "backed up target old", stage: unixStageBackedUp, targetContent: "old", candidate: true, backup: true, wantTarget: "old"},
		{name: "renamed before stage write", stage: unixStageBackedUp, targetContent: "new", backup: true, wantTarget: "new"},
		{name: "installed", stage: unixStageInstalled, targetContent: "new", backup: true, wantTarget: "new"},
		{name: "verified", stage: unixStageVerified, targetContent: "new", backup: true, wantTarget: "new"},
		{name: "target missing", stage: unixStageBackedUp, candidate: true, backup: true, wantTarget: "old"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "ecctl")
			state := testUnixUpdateState(test.stage)
			if test.targetContent != "" {
				writeTestExecutable(t, target, test.targetContent)
			}
			if test.candidate {
				writeTestExecutable(t, filepath.Join(root, state.Candidate), "new")
			}
			if test.backup {
				writeTestExecutable(t, filepath.Join(root, state.Backup), "old")
			}
			if err := writeUnixUpdateState(filepath.Join(root, unixUpdateStateName), state); err != nil {
				t.Fatal(err)
			}
			if err := checkPendingInstall(context.Background(), Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}); err != nil {
				t.Fatal(err)
			}
			assertFileContent(t, target, test.wantTarget)
			assertPathMissing(t, filepath.Join(root, unixUpdateStateName))
			assertPathMissing(t, filepath.Join(root, state.Candidate))
			assertPathMissing(t, filepath.Join(root, state.Backup))
		})
	}
}

func TestUnixReconciliationPreservesUntrustedEvidence(t *testing.T) {
	t.Run("corrupt journal", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "ecctl")
		journal := filepath.Join(root, unixUpdateStateName)
		writeTestExecutable(t, target, "old")
		if err := os.WriteFile(journal, []byte("{not-json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := checkPendingInstall(context.Background(), Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}); err == nil {
			t.Fatal("corrupt journal was accepted")
		}
		if _, err := os.Lstat(journal); err != nil {
			t.Fatalf("corrupt journal was not preserved: %v", err)
		}
	})

	t.Run("symlink artifact", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "ecctl")
		state := testUnixUpdateState(unixStagePrepared)
		writeTestExecutable(t, target, "old")
		outside := filepath.Join(t.TempDir(), "outside")
		writeTestExecutable(t, outside, "new")
		candidate := filepath.Join(root, state.Candidate)
		if err := os.Symlink(outside, candidate); err != nil {
			t.Fatal(err)
		}
		if err := writeUnixUpdateState(filepath.Join(root, unixUpdateStateName), state); err != nil {
			t.Fatal(err)
		}
		if err := checkPendingInstall(context.Background(), Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner}); err == nil {
			t.Fatal("symlink artifact was accepted")
		}
		if info, err := os.Lstat(candidate); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink evidence was not preserved: %v, %v", info, err)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "ecctl")
		state := testUnixUpdateState(unixStageInstalled)
		writeTestExecutable(t, target, "unexpected")
		writeTestExecutable(t, filepath.Join(root, state.Backup), "old")
		if err := writeUnixUpdateState(filepath.Join(root, unixUpdateStateName), state); err != nil {
			t.Fatal(err)
		}
		err := checkPendingInstall(context.Background(), Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner})
		if err == nil || !strings.Contains(err.Error(), "evidence was preserved") {
			t.Fatalf("unknown target error = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(root, unixUpdateStateName)); err != nil {
			t.Fatalf("journal evidence was not preserved: %v", err)
		}
	})
}

func TestUnixUpdateLockIsNonBlocking(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl")
	writeTestExecutable(t, target, "old")
	unlock, err := acquireUnixUpdateLock(target)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	err = checkPendingInstall(context.Background(), Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: unixVersionRunner})
	if err == nil || ErrorKindOf(err) != ErrorBusy {
		t.Fatalf("contended lock error = %v, kind=%q", err, ErrorKindOf(err))
	}
}

func TestUnixReconciliationUsesCallerContext(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl")
	state := testUnixUpdateState(unixStageInstalled)
	writeTestExecutable(t, target, "new")
	writeTestExecutable(t, filepath.Join(root, state.Backup), "old")
	if err := writeUnixUpdateState(filepath.Join(root, unixUpdateStateName), state); err != nil {
		t.Fatal(err)
	}
	runner := func(ctx context.Context, _ []string, _ string, _ ...string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
			return nil, errors.New("caller context was not propagated")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := checkPendingInstall(ctx, Options{Executable: target, CurrentVersion: "1.2.2", RunCommand: runner})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reconciliation error = %v, want caller deadline", err)
	}
}

func testUnixUpdateState(stage string) unixUpdateState {
	const token = "00112233445566778899aabbccddeeff"
	return unixUpdateState{
		Schema: unixUpdateSchema, Token: token, CurrentVersion: "1.2.2", TargetVersion: "1.2.3",
		Candidate: ".ecctl-update-candidate", Backup: unixUpdateBackup + token, Stage: stage,
	}
}

func unixVersionRunner(_ context.Context, _ []string, name string, _ ...string) ([]byte, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	switch string(raw) {
	case "old":
		return []byte("ecctl 1.2.2\n"), nil
	case "new":
		return []byte("ecctl 1.2.3\n"), nil
	default:
		return []byte("ecctl 9.9.9\n"), nil
	}
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("%s content = %q, %v; want %q", path, raw, err, want)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s still exists: %v", path, err)
	}
}
