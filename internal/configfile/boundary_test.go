package configfile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestPrivateReplacementFailureKeepsTarget(t *testing.T) {
	for _, fault := range []string{"oversize", "closed", "retargeted"} {
		t.Run(fault, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "entry")
			if err := PreparePrivateDirectory(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			target, err := Resolve(path, false)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := target.BeginPrivateReplace(8)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Abort()
			raw := []byte("new")
			switch fault {
			case "oversize":
				raw = []byte("more than eight bytes")
			case "closed":
				if err := prepared.file.Close(); err != nil {
					t.Fatal(err)
				}
			case "retargeted":
				target.requestedPath = filepath.Join(filepath.Dir(path), "another")
			}
			if err := prepared.Commit(raw); err == nil {
				t.Fatalf("%s replacement accepted", fault)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "original" {
				t.Fatalf("original changed: %q, %v", got, err)
			}
		})
	}
}

func TestConfigurationLockTimeoutAndUnsafePrivateTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	target, err := Resolve(path, false)
	if err != nil {
		t.Fatal(err)
	}
	lock := flock.New(target.Path() + ".lock")
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	called := false
	if err := target.WithLock(context.Background(), time.Millisecond, time.Millisecond, func() error { called = true; return nil }); err == nil || called {
		t.Fatalf("busy lock entered transaction: %v", err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if target.AtomicWritePrivate([]byte("new")) == nil {
		t.Fatal("directory replaced by private file")
	}
	if _, err := target.BeginPrivateReplace(8); err == nil {
		t.Fatal("directory reserved for replacement")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := &Target{path: filepath.Join(path, "child"), requestedPath: filepath.Join(path, "child")}
	if err := invalid.WithLock(context.Background(), 0, 0, nil); err == nil {
		t.Fatal("invalid lock parent accepted")
	}
}

func TestUnavailableTargetsFailClosed(t *testing.T) {
	for _, target := range []*Target{nil, {}} {
		if target.Path() != "" {
			t.Fatal("unavailable target has a path")
		}
		if target.Verify() == nil || target.WithLock(nil, 0, 0, nil) == nil {
			t.Fatal("unavailable target verified or locked")
		}
		if _, _, err := target.Read(); err == nil {
			t.Fatal("unavailable target read")
		}
		if _, _, err := target.ReadBoundedRegular(1); err == nil {
			t.Fatal("unavailable bounded read")
		}
		if target.AtomicWrite(nil, 0) == nil || target.AtomicWritePrivate(nil) == nil {
			t.Fatal("unavailable target written")
		}
		if _, err := target.BeginPrivateReplace(1); err == nil {
			t.Fatal("unavailable target reserved")
		}
	}
	for _, replacement := range []*PrivateReplace{nil, {}} {
		if replacement.Commit(nil) == nil || replacement.CommitWithLock(nil, 0, 0, nil) == nil {
			t.Fatal("unavailable replacement committed")
		}
		if err := replacement.Abort(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Resolve("", false); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := Resolve(filepath.Join(t.TempDir(), "missing", "file"), false); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestPrivateReplaceCommitWithLockAndAbort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "entry")
	target, err := Resolve(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.WithLock(nil, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := target.BeginPrivateReplace(0); err == nil {
		t.Fatal("zero reservation accepted")
	}
	prepared, err := target.BeginPrivateReplace(1024)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	if err := prepared.CommitWithLock(context.Background(), 0, 0, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit([]byte("second")); err == nil {
		t.Fatal("second commit accepted")
	}
	if err := prepared.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := target.ReadBoundedRegular(0); err == nil {
		t.Fatal("zero read limit accepted")
	}
	raw, info, err := target.ReadBoundedRegular(5)
	if err != nil || string(raw) != "first" || info.Size() != 5 {
		t.Fatalf("read = %q, %v", raw, err)
	}
	prepared, err = target.BeginPrivateReplace(1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary reservation remains: %v", err)
	}
	raw, _, err = target.Read()
	if err != nil || string(raw) != "first" {
		t.Fatalf("abort changed target: %q, %v", raw, err)
	}
}

func TestPostCommitErrorPreservesCauseAndUncertainty(t *testing.T) {
	var empty *PostCommitError
	if empty.Unwrap() != nil || !strings.Contains(empty.Error(), "uncertain") {
		t.Fatal("nil uncertainty lost")
	}
	if !strings.Contains((&PostCommitError{}).Error(), "uncertain") {
		t.Fatal("empty uncertainty lost")
	}
	cause := errors.New("sync failure")
	err := &PostCommitError{Err: cause}
	if err.Error() != cause.Error() || !errors.Is(err, cause) || !ReplacementApplied(errors.Join(errors.New("wrapper"), err)) || ReplacementApplied(cause) {
		t.Fatal("post-commit error classification failed")
	}
}

func TestZeroReservationWritesWholeRangeAndRejectsInvalidFiles(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "reserve")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if writeZeroReservation(nil, 1) == nil || writeZeroReservation(file, 0) == nil || writeZeroReservation(file, -1) == nil {
		t.Fatal("invalid reservation accepted")
	}
	const size = (64 << 10) + 17
	if err := writeZeroReservation(file, size); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Equal(raw, make([]byte, size)) {
		t.Fatalf("reservation differs: len=%d, %v", len(raw), err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeZeroReservation(file, 1); err == nil {
		t.Fatal("closed file accepted")
	}
}

func TestSameTargetAbsentPathsAndProbeFailures(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		first, second string
		same          bool
	}{
		{"", filepath.Join(dir, "file"), false},
		{filepath.Join(dir, "file"), "", false},
		{filepath.Join(dir, "file"), filepath.Join(dir, "file"), true},
		{filepath.Join(dir, "first"), filepath.Join(dir, "second"), false},
	} {
		if same, err := SameTarget(tt.first, tt.second); err != nil || same != tt.same {
			t.Fatalf("SameTarget(%q, %q) = %t, %v", tt.first, tt.second, same, err)
		}
	}
	if _, err := directoryCaseInsensitive(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("case probe leaked files: %v, %v", entries, err)
	}
	if _, err := directoryCaseInsensitive(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("probe accepted missing directory")
	}
	original := probeDirectoryCaseInsensitive
	t.Cleanup(func() { probeDirectoryCaseInsensitive = original })
	cause := errors.New("probe failed")
	probeDirectoryCaseInsensitive = func(string) (bool, error) { return false, cause }
	if _, err := SameTarget(filepath.Join(dir, "Config"), filepath.Join(dir, "config")); !errors.Is(err, cause) {
		t.Fatalf("probe error = %v", err)
	}
	for _, tt := range []struct{ input, want string }{{"abc", "Abc"}, {"ABC", "aBC"}, {"123", "123"}, {"1é", "1É"}} {
		if got := swapFilenameCase(tt.input); got != tt.want {
			t.Fatalf("case swap %q = %q", tt.input, got)
		}
	}
}
