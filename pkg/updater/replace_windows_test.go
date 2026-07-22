//go:build windows

package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	testWindowsUpdateToken    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testWindowsUpdateTokenTwo = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestWindowsSelfUpdateCompletesAfterParentExit(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(sourcePath, []byte(windowsUpdaterFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "ecctl.exe")
	newDirectory := filepath.Join(root, "new")
	if err := os.MkdirAll(newDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(newDirectory, "ecctl.exe")
	buildWindowsUpdaterFixture(t, sourcePath, targetPath, "1.2.2")
	buildWindowsUpdaterFixture(t, sourcePath, newPath, "1.2.3")

	newBinary, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := testZip(t, "ecctl.exe", newBinary)
	digest := sha256.Sum256(archive)
	filename := fmt.Sprintf("ecctl_1.2.3_windows_%s.zip", runtime.GOARCH)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), filename)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oss/version.txt":
			fmt.Fprint(w, "1.2.3")
		case "/api/latest", "/api/tags/v1.2.3":
			writeTestRelease(t, w, request, "1.2.3", false, false, true, map[string][]byte{
				"checksums.txt": []byte(checksums),
				filename:        archive,
			})
		case "/oss/1.2.3/checksums.txt":
			fmt.Fprint(w, checksums)
		case "/oss/1.2.3/" + filename:
			_, _ = w.Write(archive)
		case "/assets/" + filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	command := exec.Command(targetPath, "self-update", server.URL+"/oss", server.URL+"/github", server.URL+"/api/latest")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("start self-update: %v\n%s", err, output)
	}
	var result Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode pending result: %v\n%s", err, output)
	}
	if !result.UpdatePending || result.Updated {
		t.Fatalf("self-update result = %#v", result)
	}

	deadline := time.Now().Add(45 * time.Second)
	for {
		versionOutput, versionErr := exec.Command(targetPath, "--version").CombinedOutput()
		if versionErr == nil && strings.TrimSpace(string(versionOutput)) == "ecctl 1.2.3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("updated executable did not become ready: %v, %q", versionErr, versionOutput)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if output, err := exec.Command(targetPath, "reconcile", server.URL+"/oss", server.URL+"/github", server.URL+"/api/latest").CombinedOutput(); err != nil {
		t.Fatalf("reconcile durable update status: %v\n%s", err, output)
	}
	statusPath := targetPath + updateStatusSuffix
	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Fatalf("completed status remains: %v", err)
	}
	if _, err := os.Stat(targetPath + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("completed update lock remains: %v", err)
	}

	for {
		helpers, _ := filepath.Glob(filepath.Join(root, ".ecctl-update-*"))
		backups, _ := filepath.Glob(targetPath + ".update-backup-*")
		installFiles, _ := filepath.Glob(targetPath + ".update-new-*")
		if len(helpers) == 0 && len(backups) == 0 && len(installFiles) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Windows update files remain: helpers=%v backups=%v installs=%v", helpers, backups, installFiles)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestWindowsStalePendingStatusDoesNotBlockRetry(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "ecctl.exe")
	statusPath := executable + updateStatusSuffix
	raw, err := json.Marshal(windowsUpdateStatus{
		State: "pending", TargetVersion: "1.2.3", Token: testWindowsUpdateToken, UpdatedAt: time.Now().Add(-pendingStatusMaxAge - time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	writeWindowsTestLock(t, executable, testWindowsUpdateToken)
	err = checkPendingInstall(context.Background(), Options{CurrentVersion: "1.2.2", Executable: executable})
	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("stale pending status error = %v", err)
	}
	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Fatalf("stale status remains after reconciliation: %v", err)
	}
	if _, err := os.Stat(executable + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("stale lock remains after reconciliation: %v", err)
	}
	if err := checkPendingInstall(context.Background(), Options{CurrentVersion: "1.2.2", Executable: executable}); err != nil {
		t.Fatalf("stale status blocks retry: %v", err)
	}
}

func TestWindowsConcurrentUpdateLockSerializesTarget(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "ecctl.exe")
	_, release, err := acquireWindowsUpdateLock(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := acquireWindowsUpdateLock(targetPath); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second update lock error = %v", err)
	}
	release()
	_, releaseAgain, err := acquireWindowsUpdateLock(targetPath)
	if err != nil {
		t.Fatalf("lock was not reusable after release: %v", err)
	}
	releaseAgain()
}

func TestWindowsRechecksCurrentVersionAfterAcquiringLock(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl.exe")
	candidate := filepath.Join(root, ".ecctl-update-candidate.exe")
	if err := os.WriteFile(candidate, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := Options{
		CurrentVersion: "1.2.2",
		Executable:     target,
		RunCommand: func(context.Context, []string, string, ...string) ([]byte, error) {
			return []byte("ecctl 1.2.3\n"), nil
		},
	}
	retained, pending, err := installPreparedExecutable(context.Background(), options, candidate, "1.2.4")
	if err == nil || !strings.Contains(err.Error(), "current executable changed before update") {
		t.Fatalf("version drift error = %v", err)
	}
	if retained || pending {
		t.Fatalf("install state = retained %v, pending %v", retained, pending)
	}
	if _, err := os.Stat(target + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("version drift left update lock: %v", err)
	}
	if _, err := os.Stat(target + updateStatusSuffix); !os.IsNotExist(err) {
		t.Fatalf("version drift wrote update status: %v", err)
	}
}

func TestWindowsReclaimsCrashedMalformedLockOwner(t *testing.T) {
	target := filepath.Join(t.TempDir(), "ecctl.exe")
	lockPath := target + updateLockSuffix
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsUpdateLockOwner(lockPath), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-pendingStatusMaxAge - time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	_, release, err := acquireWindowsUpdateLock(target)
	if err != nil {
		t.Fatalf("reclaim crashed lock: %v", err)
	}
	release()
	if matches, err := filepath.Glob(lockPath + ".stale-*"); err != nil || len(matches) != 0 {
		t.Fatalf("stale lock quarantine = %v, %v", matches, err)
	}
}

func TestWindowsInternalUpdateProtocolV1IsStable(t *testing.T) {
	if internalApplyMode != "apply-v1" || internalCleanupMode != "cleanup-v1" || internalProbeMode != "probe-v1" {
		t.Fatalf("internal update protocol changed: apply=%q cleanup=%q probe=%q", internalApplyMode, internalCleanupMode, internalProbeMode)
	}
	if err := RunInternalUpdate([]string{internalProbeMode}); err != nil {
		t.Fatalf("probe v1 failed: %v", err)
	}
}

func TestWindowsRejectsTargetWithoutUpdateProtocol(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ecctl.exe")
	candidate := filepath.Join(root, ".ecctl-update-old.exe")
	if err := os.WriteFile(candidate, []byte("candidate"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := Options{
		CurrentVersion: "1.2.2",
		Executable:     target,
		RunCommand: func(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
			if name == target && len(args) == 1 && args[0] == "--version" {
				return []byte("ecctl 1.2.2\n"), nil
			}
			if name == candidate && len(args) == 2 && args[0] == "__update" && args[1] == internalProbeMode {
				return []byte("unknown command"), errors.New("exit status 2")
			}
			return nil, fmt.Errorf("unexpected command %q %v", name, args)
		},
	}
	retained, pending, err := installPreparedExecutable(context.Background(), options, candidate, "1.0.0")
	if err == nil || ErrorKindOf(err) != ErrorInvalidTarget || !strings.Contains(err.Error(), "validate Windows update protocol") {
		t.Fatalf("unsupported protocol error = %v, kind %q", err, ErrorKindOf(err))
	}
	if retained || pending {
		t.Fatalf("install state = retained %v, pending %v", retained, pending)
	}
	if _, err := os.Stat(target + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("protocol rejection left update lock: %v", err)
	}
}

func TestWindowsPendingStatusHasTerminalTimeoutEvenIfPIDIsReused(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "ecctl.exe")
	helper := filepath.Join(root, ".ecctl-update-stale")
	if err := os.WriteFile(helper, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	status := windowsUpdateStatus{
		State: "pending", TargetVersion: "1.2.3", Token: testWindowsUpdateToken, UpdatedAt: time.Now().Add(-pendingTerminalAge - time.Minute),
		HelperPID: os.Getpid(), HelperPath: helper,
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable+updateStatusSuffix, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	writeWindowsTestLock(t, executable, testWindowsUpdateToken)
	err = checkPendingInstall(context.Background(), Options{CurrentVersion: "1.2.2", Executable: executable})
	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("terminal pending status error = %v", err)
	}
	if _, err := os.Stat(executable + updateStatusSuffix); !os.IsNotExist(err) {
		t.Fatalf("terminal status remains: %v", err)
	}
	if _, err := os.Stat(executable + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("terminal lock remains: %v", err)
	}
}

func TestWindowsStaleHelperCannotClaimOrRemoveNewLock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "ecctl.exe")
	statusPath := target + updateStatusSuffix
	writeWindowsTestLock(t, target, testWindowsUpdateTokenTwo)
	if err := writeWindowsUpdateStatus(statusPath, windowsUpdateStatus{
		State: "pending", TargetVersion: "1.2.4", Token: testWindowsUpdateTokenTwo,
	}); err != nil {
		t.Fatal(err)
	}
	if guard, err := openWindowsUpdateLockGuard(target+updateLockSuffix, testWindowsUpdateToken); err == nil {
		_ = guard.Close()
		t.Fatal("stale helper claimed a newer update lock")
	}
	clearWindowsUpdateState(target, testWindowsUpdateToken)
	if !windowsUpdateLockMatches(target+updateLockSuffix, testWindowsUpdateTokenTwo) {
		t.Fatal("stale helper removed the newer update lock")
	}
	if _, err := os.Stat(statusPath); err != nil {
		t.Fatalf("stale helper removed the newer update status: %v", err)
	}
	clearWindowsUpdateState(target, testWindowsUpdateTokenTwo)
	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Fatalf("matching status remains: %v", err)
	}
	if _, err := os.Stat(target + updateLockSuffix); !os.IsNotExist(err) {
		t.Fatalf("matching lock remains: %v", err)
	}
}

func writeWindowsTestLock(t *testing.T, target, token string) {
	t.Helper()
	lockPath := target + updateLockSuffix
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsUpdateLockOwner(lockPath), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildWindowsUpdaterFixture(t *testing.T, sourcePath, outputPath, fixtureVersion string) {
	t.Helper()
	command := exec.Command("go", "build", "-trimpath", "-ldflags", "-X main.version="+fixtureVersion, "-o", outputPath, sourcePath)
	command.Dir = filepath.Clean(filepath.Join("..", ".."))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Windows updater fixture %s: %v\n%s", fixtureVersion, err, output)
	}
}

const windowsUpdaterFixture = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/aliyun/elastic-compute-control-cli/pkg/updater"
)

var version = "dev"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "__update" {
		if err := updater.RunInternalUpdate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println("ecctl " + version)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	if len(os.Args) == 5 && os.Args[1] == "self-update" {
		result, err := updater.Update(context.Background(), updater.Options{
			CurrentVersion: version,
			TargetVersion: "1.2.3",
			Executable: executable,
			GOOS: "windows",
			GOARCH: runtime.GOARCH,
			Client: &updater.Client{OSSBase: os.Args[2], GitHubAPIBase: os.Args[4]},
		})
		if err != nil {
			panic(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			panic(err)
		}
		return
	}
	if len(os.Args) == 5 && os.Args[1] == "reconcile" {
		_, err := updater.Check(context.Background(), updater.Options{
			CurrentVersion: version,
			TargetVersion: version,
			Executable: executable,
			GOOS: "windows",
			GOARCH: runtime.GOARCH,
			Client: &updater.Client{OSSBase: os.Args[2], GitHubAPIBase: os.Args[4]},
		})
		if err != nil {
			panic(err)
		}
		return
	}
	os.Exit(2)
}
`
