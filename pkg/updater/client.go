package updater

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	DefaultOSSBaseURL = "https://ros-public-tools.oss-cn-beijing.aliyuncs.com/github-releases/aliyun/elastic-compute-control-cli"
	githubLatestAPI   = "https://api.github.com/repos/aliyun/elastic-compute-control-cli/releases/latest"
	maxVersionBytes   = 1024
	maxReleaseBytes   = 1 << 20
	maxReleaseAssets  = 64
	maxChecksumsBytes = 1 << 20
	maxArchiveBytes   = 200 << 20
)

var errUntrustedRedirect = errors.New("update redirect uses an untrusted HTTPS host")

type Client struct {
	HTTP          *http.Client
	OSSBase       string
	GitHubAPIBase string
}

type Artifact struct {
	Archive  []byte
	Filename string
	SHA256   string
	Source   string
}

type releaseAsset struct {
	Name   string
	SHA256 string
	URL    string
}

type releaseDescriptor struct {
	Version    string
	Prerelease bool
	Assets     map[string]releaseAsset
}

type sourceUnavailableError struct {
	source string
	err    error
}

func (e *sourceUnavailableError) Error() string {
	return e.source + " is unavailable: " + e.err.Error()
}
func (e *sourceUnavailableError) Unwrap() error { return e.err }

func NewClient(timeout time.Duration) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if request.URL.Scheme != "https" {
					return errors.New("update downloads must remain on HTTPS")
				}
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if len(via) == 0 || !trustedRedirectHost(via[0].URL.Hostname(), request.URL.Hostname()) {
					return fmt.Errorf("%w: from %q to %q", errUntrustedRedirect, redirectOrigin(via), request.URL.Hostname())
				}
				return nil
			},
		},
		OSSBase:       DefaultOSSBaseURL,
		GitHubAPIBase: githubLatestAPI,
	}
}

func redirectOrigin(via []*http.Request) string {
	if len(via) == 0 {
		return ""
	}
	return via[0].URL.Hostname()
}

func trustedRedirectHost(origin, destination string) bool {
	origin = strings.ToLower(strings.TrimSuffix(origin, "."))
	destination = strings.ToLower(strings.TrimSuffix(destination, "."))
	if origin == "" || destination == "" {
		return false
	}
	if origin == destination {
		return true
	}
	return (origin == "github.com" || origin == "api.github.com") && destination == "release-assets.githubusercontent.com"
}

func (client *Client) LatestVersion(ctx context.Context) (string, error) {
	raw, err := client.fetch(ctx, strings.TrimRight(client.OSSBase, "/")+"/version.txt", maxVersionBytes, "OSS version")
	if err != nil {
		return "", unavailableOrIntegrityError("read OSS version", classifyAvailability(ctx, "oss", err))
	}
	text := string(raw)
	if strings.ContainsAny(text, "\r\x00") || strings.Count(strings.TrimSuffix(text, "\n"), "\n") != 0 {
		return "", WrapError(ErrorIntegrity, errors.New("OSS version.txt must contain exactly one SemVer value"))
	}
	line := strings.TrimSuffix(text, "\n")
	if strings.TrimSpace(line) != line {
		return "", WrapError(ErrorIntegrity, errors.New("OSS version.txt must not contain surrounding whitespace"))
	}
	version, err := NormalizeVersion(line)
	if err != nil {
		return "", WrapError(ErrorIntegrity, fmt.Errorf("invalid OSS version.txt: %w", err))
	}
	if isPrereleaseVersion(version) {
		return "", WrapError(ErrorIntegrity, errors.New("OSS version.txt must point to a stable release"))
	}
	return version, nil
}

// ResolveLatestVersion verifies the mutable OSS pointer against GitHub's
// immutable latest stable Release.
func (client *Client) ResolveLatestVersion(ctx context.Context) (string, error) {
	ossVersion, err := client.LatestVersion(ctx)
	if err != nil {
		return "", err
	}
	descriptor, err := client.resolveRelease(ctx, "", true)
	if err != nil {
		return "", err
	}
	order, err := CompareVersions(ossVersion, descriptor.Version)
	if err != nil {
		return "", WrapError(ErrorIntegrity, err)
	}
	switch {
	case order < 0:
		return "", WrapError(ErrorUnavailable, fmt.Errorf("OSS version %s has not reached GitHub latest release %s", ossVersion, descriptor.Version))
	case order > 0:
		return "", WrapError(ErrorIntegrity, fmt.Errorf("OSS version %s is newer than GitHub latest release %s", ossVersion, descriptor.Version))
	default:
		return ossVersion, nil
	}
}

func (client *Client) resolveLatestRelease(ctx context.Context) (releaseDescriptor, error) {
	ossVersion, err := client.LatestVersion(ctx)
	if err != nil {
		return releaseDescriptor{}, err
	}
	descriptor, err := client.resolveRelease(ctx, "", true)
	if err != nil {
		return releaseDescriptor{}, err
	}
	order, err := CompareVersions(ossVersion, descriptor.Version)
	if err != nil {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, err)
	}
	if order < 0 {
		return releaseDescriptor{}, WrapError(ErrorUnavailable, fmt.Errorf("OSS version %s has not reached GitHub latest release %s", ossVersion, descriptor.Version))
	}
	if order > 0 {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("OSS version %s is newer than GitHub latest release %s", ossVersion, descriptor.Version))
	}
	return descriptor, nil
}

func (client *Client) resolveReleaseForVersion(ctx context.Context, version string) (releaseDescriptor, error) {
	normalized, err := NormalizeVersion(version)
	if err != nil {
		return releaseDescriptor{}, WrapError(ErrorInvalidTarget, err)
	}
	return client.resolveRelease(ctx, normalized, false)
}

func (client *Client) resolveRelease(ctx context.Context, version string, latest bool) (releaseDescriptor, error) {
	apiURL := client.GitHubAPIBase
	if apiURL == "" {
		apiURL = githubLatestAPI
	}
	label := "GitHub latest release"
	if !latest {
		apiURL = releaseTagAPIURL(apiURL, version)
		label = "GitHub release v" + version
	}
	raw, err := client.fetch(ctx, apiURL, maxReleaseBytes, label)
	if err != nil {
		return releaseDescriptor{}, unavailableOrIntegrityError("read "+label, classifyAvailability(ctx, "github", err))
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Immutable  bool   `json:"immutable"`
		Assets     []struct {
			Name               string `json:"name"`
			State              string `json:"state"`
			Digest             string `json:"digest"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &release); err != nil {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("invalid %s metadata: %w", label, err))
	}
	if release.Draft || !release.Immutable {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("%s must be published and immutable", label))
	}
	if !strings.HasPrefix(release.TagName, "v") {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("%s tag %q must start with v", label, release.TagName))
	}
	githubVersion, err := NormalizeVersion(release.TagName)
	if err != nil || release.TagName != "v"+githubVersion {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("invalid %s tag %q", label, release.TagName))
	}
	if latest && (release.Prerelease || isPrereleaseVersion(githubVersion)) {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, errors.New("GitHub latest release must be stable"))
	}
	if !latest && githubVersion != version {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release tag %q does not match requested version %s", release.TagName, version))
	}
	if release.Prerelease != isPrereleaseVersion(githubVersion) {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release v%s prerelease metadata does not match its SemVer", githubVersion))
	}
	if len(release.Assets) == 0 || len(release.Assets) > maxReleaseAssets {
		return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release v%s must contain 1 to %d assets", githubVersion, maxReleaseAssets))
	}
	descriptor := releaseDescriptor{Version: githubVersion, Prerelease: release.Prerelease, Assets: make(map[string]releaseAsset, len(release.Assets))}
	for _, rawAsset := range release.Assets {
		if rawAsset.Name == "" || path.Base(rawAsset.Name) != rawAsset.Name {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release v%s contains invalid asset name %q", githubVersion, rawAsset.Name))
		}
		if _, exists := descriptor.Assets[rawAsset.Name]; exists {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release v%s contains duplicate asset %q", githubVersion, rawAsset.Name))
		}
		if rawAsset.State != "uploaded" {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release asset %s is not uploaded", rawAsset.Name))
		}
		digest, digestErr := parseAssetDigest(rawAsset.Digest)
		if digestErr != nil {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release asset %s: %w", rawAsset.Name, digestErr))
		}
		if !trustedAssetURL(apiURL, rawAsset.BrowserDownloadURL) {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release asset %s has untrusted download URL %q", rawAsset.Name, rawAsset.BrowserDownloadURL))
		}
		descriptor.Assets[rawAsset.Name] = releaseAsset{Name: rawAsset.Name, SHA256: digest, URL: rawAsset.BrowserDownloadURL}
	}
	for _, name := range requiredReleaseAssets(githubVersion) {
		if _, ok := descriptor.Assets[name]; !ok {
			return releaseDescriptor{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub release v%s is missing required asset %s", githubVersion, name))
		}
	}
	return descriptor, nil
}

func releaseTagAPIURL(latestAPI, version string) string {
	base := strings.TrimRight(latestAPI, "/")
	if strings.HasSuffix(base, "/latest") {
		base = strings.TrimSuffix(base, "/latest")
	}
	return base + "/tags/v" + url.PathEscape(version)
}

func parseAssetDigest(raw string) (string, error) {
	digest, ok := strings.CutPrefix(raw, "sha256:")
	if !ok || len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return "", errors.New("digest must use sha256:<64 lowercase hex> format")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("digest must use sha256:<64 lowercase hex> format")
	}
	return digest, nil
}

func trustedAssetURL(apiURL, raw string) bool {
	assetURL, err := url.Parse(raw)
	if err != nil || assetURL.Hostname() == "" || assetURL.User != nil || assetURL.Fragment != "" {
		return false
	}
	if assetURL.Scheme == "https" {
		return assetURL.Hostname() == "github.com" || assetURL.Hostname() == "release-assets.githubusercontent.com"
	}
	api, err := url.Parse(apiURL)
	return err == nil && api.Scheme == "http" && assetURL.Scheme == "http" && api.Host == assetURL.Host
}

func requiredReleaseAssets(version string) []string {
	names := []string{"checksums.txt", "version.txt"}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			name, _ := artifactFilename(version, goos, goarch)
			names = append(names, name)
		}
	}
	return names
}

func homebrewCaskAssetName(version string) string {
	return "ecctl_" + version + "_cask.rb"
}

func (client *Client) DownloadArtifact(ctx context.Context, version, goos, goarch string) (Artifact, error) {
	normalized, err := NormalizeVersion(version)
	if err != nil {
		return Artifact{}, WrapError(ErrorInvalidTarget, err)
	}
	descriptor, err := client.resolveReleaseForVersion(ctx, normalized)
	if err != nil {
		return Artifact{}, err
	}
	return client.downloadArtifact(ctx, descriptor, goos, goarch)
}

func (client *Client) downloadArtifact(ctx context.Context, descriptor releaseDescriptor, goos, goarch string) (Artifact, error) {
	filename, err := artifactFilename(descriptor.Version, goos, goarch)
	if err != nil {
		return Artifact{}, WrapError(ErrorInstallation, err)
	}
	asset := descriptor.Assets[filename]
	artifact, err := client.downloadOSSArtifact(ctx, descriptor, asset)
	if err == nil {
		return artifact, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Artifact{}, ctxErr
	}
	var unavailable *sourceUnavailableError
	if !errors.As(err, &unavailable) {
		return Artifact{}, unavailableOrIntegrityError("download from OSS", err)
	}
	archive, err := client.fetch(ctx, asset.URL, maxArchiveBytes, "GitHub archive")
	if err != nil {
		return Artifact{}, unavailableOrIntegrityError("download from GitHub", classifyAvailability(ctx, "github", err))
	}
	if got := digestBytes(archive); got != asset.SHA256 {
		return Artifact{}, WrapError(ErrorIntegrity, fmt.Errorf("GitHub archive checksum mismatch for %s: got %s, want %s", filename, got, asset.SHA256))
	}
	return Artifact{Archive: archive, Filename: filename, SHA256: asset.SHA256, Source: "github"}, nil
}

// ValidateArtifact verifies that a platform artifact is published and returns
// the source an update would use without downloading the archive itself. OSS
// remains preferred, but its mutable checksum must agree with the immutable
// GitHub Release checksum before it is trusted.
func (client *Client) ValidateArtifact(ctx context.Context, version, goos, goarch string) (string, error) {
	normalized, err := NormalizeVersion(version)
	if err != nil {
		return "", WrapError(ErrorInvalidTarget, err)
	}
	descriptor, err := client.resolveReleaseForVersion(ctx, normalized)
	if err != nil {
		return "", err
	}
	return client.validateArtifact(ctx, descriptor, goos, goarch)
}

func (client *Client) validateArtifact(ctx context.Context, descriptor releaseDescriptor, goos, goarch string) (string, error) {
	filename, err := artifactFilename(descriptor.Version, goos, goarch)
	if err != nil {
		return "", WrapError(ErrorInstallation, err)
	}
	asset := descriptor.Assets[filename]
	ossBase := strings.TrimRight(client.OSSBase, "/") + "/" + url.PathEscape(descriptor.Version)
	ossChecksum, ossErr := client.verifiedOSSChecksum(ctx, descriptor, filename)
	if ossErr == nil {
		ossErr = classifyAvailability(ctx, "oss", client.probe(ctx, ossBase+"/"+url.PathEscape(filename), "oss archive"))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if ossErr != nil {
		var unavailable *sourceUnavailableError
		if !errors.As(ossErr, &unavailable) {
			return "", unavailableOrIntegrityError("validate OSS artifact", ossErr)
		}
	}
	if ossErr != nil {
		if probeErr := client.probe(ctx, asset.URL, "github archive"); probeErr != nil {
			return "", unavailableOrIntegrityError("validate GitHub artifact", classifyAvailability(ctx, "github", probeErr))
		}
		return "github", nil
	}
	if ossChecksum != asset.SHA256 {
		return "", WrapError(ErrorIntegrity, fmt.Errorf("OSS checksum for %s does not match immutable GitHub Release checksum", filename))
	}
	return "oss", nil
}

func (client *Client) downloadOSSArtifact(ctx context.Context, descriptor releaseDescriptor, asset releaseAsset) (Artifact, error) {
	want, err := client.verifiedOSSChecksum(ctx, descriptor, asset.Name)
	if err != nil {
		return Artifact{}, err
	}
	if want != asset.SHA256 {
		return Artifact{}, WrapError(ErrorIntegrity, fmt.Errorf("OSS checksum for %s does not match immutable GitHub Release digest", asset.Name))
	}
	base := strings.TrimRight(client.OSSBase, "/") + "/" + url.PathEscape(descriptor.Version)
	archive, err := client.fetch(ctx, base+"/"+url.PathEscape(asset.Name), maxArchiveBytes, "oss archive")
	if err != nil {
		return Artifact{}, classifyAvailability(ctx, "oss", err)
	}
	if got := digestBytes(archive); got != asset.SHA256 {
		return Artifact{}, WrapError(ErrorIntegrity, fmt.Errorf("oss checksum mismatch for %s: got %s, want %s", asset.Name, got, asset.SHA256))
	}
	return Artifact{Archive: archive, Filename: asset.Name, SHA256: asset.SHA256, Source: "oss"}, nil
}

func (client *Client) verifiedOSSChecksum(ctx context.Context, descriptor releaseDescriptor, filename string) (string, error) {
	base := strings.TrimRight(client.OSSBase, "/") + "/" + url.PathEscape(descriptor.Version)
	raw, err := client.fetch(ctx, base+"/checksums.txt", maxChecksumsBytes, "oss checksums")
	if err != nil {
		return "", classifyAvailability(ctx, "oss", err)
	}
	asset := descriptor.Assets["checksums.txt"]
	if got := digestBytes(raw); got != asset.SHA256 {
		return "", WrapError(ErrorIntegrity, fmt.Errorf("OSS checksums.txt does not match immutable GitHub Release digest: got %s, want %s", got, asset.SHA256))
	}
	checksums, err := parseChecksums(raw)
	if err != nil {
		return "", WrapError(ErrorIntegrity, fmt.Errorf("invalid OSS checksums: %w", err))
	}
	want, ok := checksums[filename]
	if !ok {
		return "", WrapError(ErrorIntegrity, fmt.Errorf("OSS checksums do not contain %s", filename))
	}
	return want, nil
}

func digestBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (client *Client) probe(ctx context.Context, location, label string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, location, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "ecctl-updater")
	httpClient := client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("probe %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return &sourceUnavailableError{source: label, err: fmt.Errorf("HTTP %d", response.StatusCode)}
	}
	return nil
}

func (client *Client) fetch(ctx context.Context, location string, limit int64, label string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "ecctl-updater")
	httpClient := client.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, &sourceUnavailableError{source: label, err: fmt.Errorf("HTTP %d", response.StatusCode)}
	}
	reader := io.LimitReader(response.Body, limit+1)
	raw, err := io.ReadAll(reader)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("read %s: %w", label, err)
		}
		if isTLSFailure(err) {
			return nil, fmt.Errorf("read %s: %w", label, err)
		}
		return nil, &sourceUnavailableError{source: label, err: fmt.Errorf("read response: %w", err)}
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	return raw, nil
}

func classifyAvailability(ctx context.Context, source string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &sourceUnavailableError{source: source, err: err}
	}
	var unavailable *sourceUnavailableError
	if errors.As(err, &unavailable) {
		return &sourceUnavailableError{source: source, err: err}
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && !isTLSFailure(urlError.Err) {
		return &sourceUnavailableError{source: source, err: err}
	}
	return err
}

func isTLSFailure(err error) bool {
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var unknown x509.UnknownAuthorityError
	var verification *tls.CertificateVerificationError
	var recordHeader tls.RecordHeaderError
	return errors.Is(err, errUntrustedRedirect) || errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &unknown) ||
		errors.As(err, &verification) || errors.As(err, &recordHeader) ||
		strings.Contains(strings.ToLower(err.Error()), "tls:") ||
		strings.Contains(err.Error(), "must remain on HTTPS")
}

func artifactFilename(version, goos, goarch string) (string, error) {
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported architecture %q", goarch)
	}
	extension := ".tar.gz"
	switch goos {
	case "darwin", "linux":
	case "windows":
		extension = ".zip"
	default:
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
	return "ecctl_" + version + "_" + goos + "_" + goarch + extension, nil
}

func parseChecksums(raw []byte) (map[string]string, error) {
	checksums := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("invalid checksum %q", fields[0])
		}
		filename := path.Base(fields[1])
		if filename != fields[1] {
			return nil, fmt.Errorf("invalid artifact name %q", fields[1])
		}
		if _, exists := checksums[filename]; exists {
			return nil, fmt.Errorf("duplicate artifact %q", filename)
		}
		checksums[filename] = strings.ToLower(fields[0])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return checksums, nil
}
