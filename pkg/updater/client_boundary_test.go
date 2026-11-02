package updater

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReleaseMetadataRejectsInvalidAssets(t *testing.T) {
	for _, fault := range []string{"json", "tag", "no assets", "too many assets", "name", "duplicate", "state", "digest", "URL", "missing required"} {
		t.Run(fault, func(t *testing.T) {
			asset := map[string]any{"name": "one", "state": "uploaded", "digest": "sha256:" + strings.Repeat("a", 64), "browser_download_url": "https://github.com/example/one"}
			assets := []any{asset}
			release := map[string]any{"tag_name": "v0.2.2", "immutable": true}
			switch fault {
			case "tag":
				release["tag_name"] = "v00.2.2"
			case "no assets":
				assets = nil
			case "too many assets":
				assets = make([]any, maxReleaseAssets+1)
			case "name":
				asset["name"] = "../one"
			case "duplicate":
				assets = append(assets, asset)
			case "state":
				asset["state"] = "new"
			case "digest":
				asset["digest"] = "sha256:" + strings.Repeat("g", 64)
			case "URL":
				asset["browser_download_url"] = "https://untrusted.invalid/one"
			}
			release["assets"] = assets
			raw, err := json.Marshal(release)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "json" {
				raw = []byte("not JSON")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) }))
			defer server.Close()
			client := &Client{HTTP: server.Client(), GitHubAPIBase: server.URL}
			if _, err := client.resolveRelease(context.Background(), "0.2.2", false); ErrorKindOf(err) != ErrorIntegrity {
				t.Fatalf("%s accepted: %v", fault, err)
			}
		})
	}
}

func TestArtifactValidationChecksPrimaryAndFallbackAvailability(t *testing.T) {
	archive := testTarGzip(t, "ecctl", []byte("binary"))
	for _, unavailable := range []bool{false, true} {
		server := releaseServer(t, archive, unavailable, false)
		client := &Client{HTTP: server.Client(), OSSBase: server.URL + "/oss", GitHubAPIBase: server.URL + "/api/latest"}
		want := "oss"
		if unavailable {
			want = "github"
		}
		if source, err := client.ValidateArtifact(context.Background(), "0.2.2", "darwin", "arm64"); err != nil || source != want {
			t.Fatalf("validated artifact source=%s,%v", source, err)
		}
		if _, err := client.ValidateArtifact(context.Background(), "0.2.2", "darwin", "386"); ErrorKindOf(err) != ErrorInstallation {
			t.Fatalf("unsupported artifact architecture=%v", err)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL, githubReleaseBase: server.URL, GitHubAPIBase: server.URL}
	if _, err := client.ValidateArtifact(context.Background(), "1.2.3", "linux", "amd64"); err == nil {
		t.Fatal("unavailable signed release validated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, call := range []func() error{
		func() error { _, err := client.resolveLatestRelease(ctx); return err },
		func() error { _, err := client.resolveReleaseForVersion(ctx, "1.2.3"); return err },
		func() error {
			_, err := client.validateArtifact(ctx, releaseDescriptor{Version: "1.2.3"}, "linux", "amd64")
			return err
		},
	} {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Fatalf("release cancellation lost: %v", err)
		}
	}
}

func TestVerifiedChecksumsRejectMalformedAndMissingEntries(t *testing.T) {
	filename := "ecctl_1.2.3_linux_amd64.tar.gz"
	for _, checksums := range []string{"invalid", strings.Repeat("a", 64) + "  other.tar.gz\n"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, checksums) }))
		client := &Client{HTTP: server.Client(), OSSBase: server.URL}
		descriptor := releaseDescriptor{Version: "1.2.3", Assets: map[string]releaseAsset{"checksums.txt": {Name: "checksums.txt", SHA256: digestBytes([]byte(checksums)), Size: int64(len(checksums))}}}
		if _, err := client.verifiedOSSChecksum(context.Background(), descriptor, filename); ErrorKindOf(err) != ErrorIntegrity {
			t.Fatalf("invalid checksum manifest accepted: %v", err)
		}
		server.Close()
	}
	checksums := strings.Repeat("a", 64) + "  " + filename + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, checksums) }))
	defer server.Close()
	client := &Client{HTTP: server.Client(), OSSBase: server.URL}
	asset := releaseAsset{Name: filename, SHA256: strings.Repeat("b", 64)}
	descriptor := releaseDescriptor{Version: "1.2.3", Assets: map[string]releaseAsset{filename: asset, "checksums.txt": {Name: "checksums.txt", SHA256: digestBytes([]byte(checksums)), Size: int64(len(checksums))}}}
	if _, err := client.validateArtifact(context.Background(), descriptor, "linux", "amd64"); ErrorKindOf(err) != ErrorIntegrity {
		t.Fatalf("verified artifact digest mismatch accepted: %v", err)
	}
	if _, err := client.downloadOSSArtifact(context.Background(), descriptor, asset); ErrorKindOf(err) != ErrorIntegrity {
		t.Fatalf("mismatching artifact downloaded: %v", err)
	}
}

func TestUpdaterHTTPAndChecksumBoundaries(t *testing.T) {
	for _, raw := range []string{" 0.2.2\n", "invalid\n"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) }))
		client := &Client{HTTP: server.Client(), OSSBase: server.URL}
		if _, err := client.LatestVersion(context.Background()); ErrorKindOf(err) != ErrorIntegrity {
			t.Fatalf("invalid version=%v", err)
		}
		server.Close()
	}
	for _, raw := range []string{"bad", strings.Repeat("g", 64) + "  file", strings.Repeat("a", 64) + "  ../file", strings.Repeat("a", 64) + "  file\n" + strings.Repeat("b", 64) + "  file", strings.Repeat("x", 70<<10)} {
		if _, err := parseChecksums([]byte(raw)); err == nil {
			t.Fatalf("invalid checksums accepted: %.80s", raw)
		}
	}
	for _, raw := range []string{"sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("g", 64), "bad"} {
		if _, err := parseAssetDigest(raw); err == nil {
			t.Fatal("invalid digest accepted")
		}
	}
	for _, target := range []string{"://", "https://user:password@github.com/a", "https://github.com/a#fragment"} {
		if trustedAssetURL("https://api.github.com", target) {
			t.Fatalf("untrusted URL accepted: %s", target)
		}
	}
	for _, target := range []struct{ os, arch string }{{"darwin", "386"}, {"plan9", "amd64"}} {
		if _, err := artifactFilename("1.2.3", target.os, target.arch); err == nil {
			t.Fatal("unsupported platform accepted")
		}
	}
	client := &Client{}
	for _, call := range []func() error{
		func() error { _, err := client.fetch(context.Background(), "://", 1, "test"); return err },
		func() error { return client.probe(context.Background(), "://", "test") },
		func() error {
			_, err := client.ValidateArtifact(context.Background(), "invalid", "darwin", "arm64")
			return err
		},
		func() error {
			_, err := client.DownloadArtifact(context.Background(), "invalid", "darwin", "arm64")
			return err
		},
		func() error { _, err := client.resolveReleaseForVersion(context.Background(), "invalid"); return err },
	} {
		if call() == nil {
			t.Fatal("invalid request accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "oversized") }))
	defer server.Close()
	if _, err := client.fetch(context.Background(), server.URL, 1, "test"); err == nil {
		t.Fatal("oversized response accepted")
	}
	if err := client.probe(context.Background(), server.URL, "test"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("transport unavailable")
	client.HTTP = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, cause })}
	if err := client.probe(context.Background(), server.URL, "test"); !errors.Is(err, cause) {
		t.Fatalf("transport error lost: %v", err)
	}
	client.githubReleaseBase = server.URL
	if _, err := client.resolveLatestGitHubReleaseVersion(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("redirect transport cause lost: %v", err)
	}
	if _, err := githubVersionFromManifestRedirect("://", &url.URL{}); err == nil {
		t.Fatal("invalid redirect base accepted")
	}
	if redirectOrigin(nil) != "" || trustedRedirectHost("", "github.com") {
		t.Fatal("missing redirect origin accepted")
	}
}
