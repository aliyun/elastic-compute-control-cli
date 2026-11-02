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

func TestInstallErrorClassificationPreservesCauses(t *testing.T) {
	if ensureInstallError(nil) != nil || WrapError(ErrorBusy, nil) != nil {
		t.Fatal("nil error became failure")
	}
	for _, tt := range []struct {
		err  error
		kind ErrorKind
	}{
		{context.Canceled, ErrorCanceled}, {context.DeadlineExceeded, ErrorTimeout},
		{os.ErrPermission, ErrorPermission}, {errors.New("install failure"), ErrorInstallation},
		{WrapError(ErrorBusy, errors.New("locked")), ErrorBusy},
	} {
		got := ensureInstallError(tt.err)
		if !errors.Is(got, tt.err) || ErrorKindOf(got) != tt.kind || got.Error() != tt.err.Error() {
			t.Fatalf("classification = %v (%s), want %s", got, ErrorKindOf(got), tt.kind)
		}
		if ErrorRetryable(tt.kind) != (tt.kind == ErrorBusy || tt.kind == ErrorUnavailable) {
			t.Fatalf("retryability = %s", tt.kind)
		}
	}
}

func TestUpdaterRejectsInvalidOptionsBeforeNetwork(t *testing.T) {
	for _, op := range []func(context.Context, Options) (Result, error){Check, Update} {
		if _, err := op(context.Background(), Options{CurrentVersion: "development"}); err == nil {
			t.Fatal("non-release build accepted")
		}
	}
	options, err := resolveOptions(Options{CurrentVersion: "v1.2.3"})
	if err != nil || options.CurrentVersion != "1.2.3" || options.Executable == "" || options.GOOS == "" || options.GOARCH == "" || options.Client == nil || options.RunCommand == nil || options.LookPath == nil {
		t.Fatalf("default options = %+v, %v", options, err)
	}
	if _, err := AutoCheck(context.Background(), AutoCheckOptions{CurrentVersion: "development"}); err == nil {
		t.Fatal("invalid advisory version accepted")
	}
}

func TestAutoCheckRejectsMissingHomeAndBrokenCacheWithoutNetwork(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := DefaultCachePath(); err == nil {
		t.Fatal("missing home accepted")
	}
	if _, err := AutoCheck(context.Background(), AutoCheckOptions{CurrentVersion: "1.2.3"}); err == nil {
		t.Fatal("advisory cache without home accepted")
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("invalid JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readCache(path); got != (cacheState{}) {
		t.Fatal("broken cache produced verified state")
	}
	if err := writeCache(path, cacheState{CheckedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("invalid cache timestamp persisted")
	}
}

func TestHomebrewLayoutValidationFailsClosed(t *testing.T) {
	prefix := t.TempDir()
	caskroom := filepath.Join(prefix, "Caskroom")
	if err := os.Mkdir(caskroom, 0o700); err != nil {
		t.Fatal(err)
	}
	canonicalPrefix, err := canonicalExistingPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	canonicalCaskroom, err := canonicalExistingPath(caskroom)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := installerDescriptor{Kind: "homebrew", Prefix: canonicalPrefix, Caskroom: canonicalCaskroom, BrewPath: "fake-brew"}
	for _, fault := range []string{"prefix command", "caskroom command", "missing prefix", "missing caskroom", "wrong prefix", "wrong caskroom", "none"} {
		t.Run(fault, func(t *testing.T) {
			options := Options{RunCommand: func(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
				if name != "fake-brew" {
					t.Fatalf("unexpected executable %q", name)
				}
				prefixCall := args[0] == "--prefix"
				if fault == "prefix command" && prefixCall || fault == "caskroom command" && !prefixCall {
					return nil, errors.New("brew failed")
				}
				value := canonicalCaskroom
				if prefixCall {
					value = canonicalPrefix
				}
				if fault == "missing prefix" && prefixCall || fault == "missing caskroom" && !prefixCall {
					value = filepath.Join(prefix, "missing")
				}
				if fault == "wrong prefix" && prefixCall {
					value = canonicalCaskroom
				}
				if fault == "wrong caskroom" && !prefixCall {
					value = canonicalPrefix
				}
				return []byte(value + "\n"), nil
			}}
			err := validateHomebrewLayout(context.Background(), options, descriptor)
			if (err == nil) != (fault == "none") {
				t.Fatalf("layout %s: %v", fault, err)
			}
		})
	}
	if err := updateWithHomebrew(context.Background(), Options{}, installerDescriptor{}, releaseDescriptor{}); err == nil {
		t.Fatal("missing installer accepted")
	}
	if err := updateWithHomebrew(context.Background(), Options{}, descriptor, releaseDescriptor{Version: "1.2.3-rc.1", Prerelease: true}); ErrorKindOf(err) != ErrorInvalidTarget {
		t.Fatalf("prerelease Homebrew update = %v", err)
	}
}

func TestUpdateCacheHandlesClockSkewAndWriteFailure(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, state := range []cacheState{
		{}, {CheckedAt: now.Add(time.Minute), VerifiedLatestVersion: "2.0.0"},
		{CheckedAt: now.Add(-autoCheckInterval), VerifiedLatestVersion: "2.0.0"},
		{CheckedAt: now, VerifiedLatestVersion: "invalid"}, {CheckedAt: now, VerifiedLatestVersion: "2.0.0-rc.1"},
	} {
		if _, fresh := cachedAutoCheckResult("1.0.0", state, now); fresh {
			t.Fatalf("invalid cache considered fresh: %+v", state)
		}
	}
	result := AutoCheckResult{LatestVersion: "2.0.0", Available: true}
	if !notificationDue(cacheState{NotifiedVersion: "2.0.0", NotifiedAt: now.Add(time.Minute)}, result, now) {
		t.Fatal("clock rollback suppressed notification forever")
	}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, available := range []bool{false, true} {
		if _, err := maybeMarkNotification(filepath.Join(blocker, "cache"), cacheState{}, AutoCheckResult{Available: available}, now, true, true); err == nil {
			t.Fatal("cache write error ignored")
		}
	}
	if _, err := AutoCheck(context.Background(), AutoCheckOptions{CurrentVersion: "1.0.0", CachePath: filepath.Join(blocker, "cache")}); err == nil {
		t.Fatal("cache lock filesystem error ignored")
	}
}

func TestExecutableCopyAndVersionProbeFailures(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := copyExecutable(source, destination); err == nil {
		t.Fatal("missing source copied")
	}
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err == nil {
		t.Fatal("existing destination overwritten")
	}
	if err := requireMissingPath(destination); err == nil {
		t.Fatal("existing path considered missing")
	}
	if err := requireMissingPath(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"", "ecctl", "other 1.2.3", "ecctl 1.2.2"} {
		err := verifyExecutableVersion(context.Background(), Options{RunCommand: func(context.Context, []string, string, ...string) ([]byte, error) { return []byte(output), nil }}, "fixture", "1.2.3")
		if err == nil || !strings.Contains(err.Error(), "reported") {
			t.Fatalf("invalid version %q = %v", output, err)
		}
	}
	_, err := replaceExecutable(context.Background(), Options{Executable: filepath.Join(dir, "missing", "ecctl")}, nil, "1.2.3")
	if err == nil {
		t.Fatal("replacement accepted missing parent")
	}
}
