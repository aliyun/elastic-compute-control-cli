package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateNoopAndDowngradePreserveInstalledExecutable(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("new binary"))
	server := releaseServer(t, archive, false, false)
	defer server.Close()
	for _, current := range []string{"0.2.2", "0.3.0"} {
		path := filepath.Join(t.TempDir(), "ecctl")
		if err := os.WriteFile(path, []byte("installed"), 0o755); err != nil {
			t.Fatal(err)
		}
		result, err := Update(context.Background(), Options{CurrentVersion: current, TargetVersion: "0.2.2", Executable: path, GOOS: "darwin", GOARCH: "arm64", Client: &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}})
		if current == "0.3.0" {
			if ErrorKindOf(err) != ErrorInvalidTarget {
				t.Fatalf("unforced downgrade=%v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if result.Updated || result.UpdatePending {
			t.Fatalf("noop/downgrade reported update: %+v", result)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "installed" {
			t.Fatalf("noop/downgrade changed executable: %q,%v", raw, err)
		}
	}
}

func TestUpdateRejectsMalformedArchiveAndWrongCandidateVersion(t *testing.T) {
	for _, malformed := range []bool{true, false} {
		archive := []byte("not an archive")
		if !malformed {
			archive = testTarGzip(t, "ecctl", []byte("wrong version binary"))
		}
		server := releaseServer(t, archive, false, false)
		path := filepath.Join(t.TempDir(), "ecctl")
		if err := os.WriteFile(path, []byte("installed"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Update(context.Background(), Options{CurrentVersion: "0.2.1", TargetVersion: "0.2.2", Executable: path, GOOS: "darwin", GOARCH: "arm64", Client: &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}, RunCommand: func(context.Context, []string, string, ...string) ([]byte, error) {
			return []byte("ecctl 9.9.9\n"), nil
		}})
		server.Close()
		want := ErrorInstallation
		if malformed {
			want = ErrorIntegrity
		}
		if ErrorKindOf(err) != want {
			t.Fatalf("invalid candidate classification=%v, want %s", err, want)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "installed" {
			t.Fatalf("invalid candidate changed executable: %q,%v", raw, err)
		}
		files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".ecctl-update-*"))
		if err != nil || len(files) != 0 {
			t.Fatalf("rejected candidate leaked staging: %v,%v", files, err)
		}
	}
}

func TestHomebrewDetectionRejectsMalformedLayoutsBeforeRunningBrew(t *testing.T) {
	for _, relative := range []string{"Caskroom/other/1.2.3/ecctl", "Caskroom/ecctl/v1.2.3/ecctl", "Caskroom/ecctl/1.2.3/other", "Caskroom/ecctl/1.2.3/ecctl"} {
		root := t.TempDir()
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "bin"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "bin", "brew"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		options := Options{GOOS: "darwin", Executable: path, RunCommand: func(context.Context, []string, string, ...string) ([]byte, error) {
			t.Fatal("invalid Homebrew layout ran a command")
			return nil, nil
		}}
		if _, err := detectInstaller(context.Background(), options); err == nil {
			t.Fatalf("invalid layout accepted: %s", relative)
		}
	}
	path := filepath.Join(t.TempDir(), "Caskroom", "missing", "ecctl")
	if _, err := Check(context.Background(), Options{CurrentVersion: "1.2.3", GOOS: "darwin", Executable: path}); err == nil || !strings.Contains(err.Error(), "Cask") {
		t.Fatalf("missing Cask installation ignored: %v", err)
	}
	if _, err := Check(context.Background(), Options{CurrentVersion: "1.2.3", TargetVersion: "invalid", Executable: filepath.Join(t.TempDir(), "ecctl")}); ErrorKindOf(err) != ErrorInvalidTarget {
		t.Fatalf("invalid explicit target=%v", err)
	}
}

func TestExecutableCopyDoesNotLeaveDestinationAfterReadFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "directory")
	destination := filepath.Join(root, "copy")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, destination); err == nil {
		t.Fatal("directory copied as executable")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("failed copy left destination: %v", err)
	}
}
