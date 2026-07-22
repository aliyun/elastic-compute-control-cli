package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/internal/releaseartifact"
)

type Result struct {
	CurrentVersion  string `json:"current_version"`
	TargetVersion   string `json:"target_version"`
	UpdateAvailable bool   `json:"update_available"`
	Source          string `json:"source"`
	Installer       string `json:"installer"`
	Updated         bool   `json:"updated"`
	UpdatePending   bool   `json:"update_pending"`
}

type Options struct {
	CurrentVersion string
	TargetVersion  string
	Force          bool
	Executable     string
	GOOS           string
	GOARCH         string
	Client         *Client
	RunCommand     CommandRunner
	LookPath       func(string) (string, error)
}

type CommandRunner func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)

type installerDescriptor struct {
	Kind     string
	Prefix   string
	Caskroom string
	BrewPath string
}

func Check(ctx context.Context, options Options) (Result, error) {
	resolved, err := resolveOptions(options)
	if err != nil {
		return Result{}, ensureInstallError(err)
	}
	result, _, _, err := checkResolved(ctx, resolved)
	return result, err
}

func checkResolved(ctx context.Context, resolved Options) (Result, installerDescriptor, releaseDescriptor, error) {
	if err := checkPendingInstall(ctx, resolved); err != nil {
		return Result{}, installerDescriptor{}, releaseDescriptor{}, ensureInstallError(err)
	}
	installer, err := detectInstaller(ctx, resolved)
	if err != nil {
		return Result{}, installerDescriptor{}, releaseDescriptor{}, ensureInstallError(err)
	}
	var descriptor releaseDescriptor
	target := resolved.TargetVersion
	if target == "" {
		descriptor, err = resolved.Client.resolveLatestRelease(ctx)
		if err != nil {
			return Result{}, installerDescriptor{}, releaseDescriptor{}, err
		}
		target = descriptor.Version
	} else {
		target, err = NormalizeVersion(target)
		if err != nil {
			return Result{}, installerDescriptor{}, releaseDescriptor{}, WrapError(ErrorInvalidTarget, fmt.Errorf("invalid target version: %w", err))
		}
		if installer.Kind == "homebrew" {
			descriptor, err = resolved.Client.resolveLatestRelease(ctx)
			if err != nil {
				return Result{}, installerDescriptor{}, releaseDescriptor{}, err
			}
			if descriptor.Version != target {
				return Result{}, installerDescriptor{}, releaseDescriptor{}, WrapError(ErrorInvalidTarget, errors.New("Homebrew can only install the latest stable version"))
			}
		} else {
			descriptor, err = resolved.Client.resolveReleaseForVersion(ctx, target)
			if err != nil {
				return Result{}, installerDescriptor{}, releaseDescriptor{}, err
			}
		}
	}
	source, err := resolved.Client.validateArtifact(ctx, descriptor, resolved.GOOS, resolved.GOARCH)
	if err != nil {
		return Result{}, installerDescriptor{}, releaseDescriptor{}, err
	}
	if installer.Kind == "homebrew" && source != "oss" {
		return Result{}, installerDescriptor{}, releaseDescriptor{}, WrapError(ErrorUnavailable, errors.New("Homebrew update requires the OSS artifact to be available"))
	}
	if installer.Kind == "homebrew" {
		if _, ok := descriptor.Assets[homebrewCaskAssetName(target)]; !ok {
			return Result{}, installerDescriptor{}, releaseDescriptor{}, WrapError(ErrorIntegrity, errors.New("immutable GitHub Release is missing its Homebrew Cask"))
		}
	}
	order, err := CompareVersions(target, resolved.CurrentVersion)
	if err != nil {
		return Result{}, installerDescriptor{}, releaseDescriptor{}, WrapError(ErrorInvalidTarget, err)
	}
	return Result{
		CurrentVersion:  resolved.CurrentVersion,
		TargetVersion:   target,
		UpdateAvailable: order > 0,
		Source:          source,
		Installer:       installer.Kind,
		Updated:         false,
	}, installer, descriptor, nil
}

func Update(ctx context.Context, options Options) (Result, error) {
	resolved, err := resolveOptions(options)
	if err != nil {
		return Result{}, ensureInstallError(err)
	}
	result, installer, descriptor, err := checkResolved(ctx, resolved)
	if err != nil {
		return Result{}, err
	}
	order, _ := CompareVersions(result.TargetVersion, result.CurrentVersion)
	if order < 0 && !resolved.Force {
		return result, WrapError(ErrorInvalidTarget, errors.New("target version is older than the current version; use --force to allow a downgrade"))
	}
	if order == 0 && !resolved.Force {
		return result, nil
	}
	if result.Installer == "homebrew" {
		if err := updateWithHomebrew(ctx, resolved, installer, descriptor); err != nil {
			return result, ensureInstallError(err)
		}
		result.Source = "oss"
		result.Updated = true
		result.UpdateAvailable = false
		return result, nil
	}

	artifact, err := resolved.Client.downloadArtifact(ctx, descriptor, resolved.GOOS, resolved.GOARCH)
	if err != nil {
		return result, err
	}
	result.Source = artifact.Source
	binary, err := extractExecutable(artifact, resolved.GOOS)
	if err != nil {
		return result, WrapError(ErrorIntegrity, err)
	}
	pending, err := replaceExecutable(ctx, resolved, binary, result.TargetVersion)
	if err != nil {
		return result, ensureInstallError(err)
	}
	if pending {
		result.UpdatePending = true
		return result, nil
	}
	result.Updated = true
	result.UpdateAvailable = false
	return result, nil
}

func resolveOptions(options Options) (Options, error) {
	current, err := NormalizeVersion(options.CurrentVersion)
	if err != nil {
		return options, fmt.Errorf("the current ecctl build does not have a release version: %w", err)
	}
	options.CurrentVersion = current
	if options.Client == nil {
		options.Client = NewClient(30 * time.Second)
	}
	if options.RunCommand == nil {
		options.RunCommand = runCommand
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.Executable == "" {
		options.Executable, err = os.Executable()
		if err != nil {
			return options, err
		}
	}
	if resolvedExecutable, resolveErr := filepath.EvalSymlinks(options.Executable); resolveErr == nil {
		options.Executable = resolvedExecutable
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.GOARCH == "" {
		options.GOARCH = runtime.GOARCH
	}
	return options, nil
}

func detectInstaller(ctx context.Context, options Options) (installerDescriptor, error) {
	direct := installerDescriptor{Kind: "direct"}
	if options.GOOS != "darwin" {
		return direct, nil
	}
	executable, err := filepath.EvalSymlinks(options.Executable)
	if err != nil {
		if pathContainsComponent(options.Executable, "Caskroom") {
			return installerDescriptor{}, fmt.Errorf("resolve Homebrew Cask executable: %w", err)
		}
		return direct, nil
	}
	executable = filepath.Clean(executable)
	if !pathContainsComponent(executable, "Caskroom") {
		return direct, nil
	}
	versionDirectory := filepath.Dir(executable)
	caskDirectory := filepath.Dir(versionDirectory)
	caskroom := filepath.Dir(caskDirectory)
	prefix := filepath.Dir(caskroom)
	version := filepath.Base(versionDirectory)
	if filepath.Base(executable) != "ecctl" || filepath.Base(caskDirectory) != "ecctl" ||
		filepath.Base(caskroom) != "Caskroom" || filepath.Dir(prefix) == prefix {
		return installerDescriptor{}, fmt.Errorf("executable %s is inside Caskroom but does not match <prefix>/Caskroom/ecctl/<version>/ecctl", executable)
	}
	normalizedVersion, versionErr := NormalizeVersion(version)
	if versionErr != nil || normalizedVersion != version {
		return installerDescriptor{}, fmt.Errorf("Homebrew Cask executable has invalid version directory %q", version)
	}
	brewPath := filepath.Join(prefix, "bin", "brew")
	brewInfo, err := os.Stat(brewPath)
	if err != nil {
		return installerDescriptor{}, fmt.Errorf("Homebrew Cask installation requires %s: %w", brewPath, err)
	}
	if !brewInfo.Mode().IsRegular() || brewInfo.Mode().Perm()&0o111 == 0 {
		return installerDescriptor{}, fmt.Errorf("Homebrew executable %s is not an executable regular file", brewPath)
	}
	descriptor := installerDescriptor{Kind: "homebrew", Prefix: filepath.Clean(prefix), Caskroom: filepath.Clean(caskroom), BrewPath: brewPath}
	if err := validateHomebrewLayout(ctx, options, descriptor); err != nil {
		return installerDescriptor{}, err
	}
	return descriptor, nil
}

func pathContainsComponent(value, component string) bool {
	for current := filepath.Clean(value); ; current = filepath.Dir(current) {
		if filepath.Base(current) == component {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func validateHomebrewLayout(ctx context.Context, options Options, descriptor installerDescriptor) error {
	prefixRaw, err := options.RunCommand(ctx, nil, descriptor.BrewPath, "--prefix")
	if err != nil {
		return commandError("brew --prefix", prefixRaw, err)
	}
	caskroomRaw, err := options.RunCommand(ctx, nil, descriptor.BrewPath, "--caskroom")
	if err != nil {
		return commandError("brew --caskroom", caskroomRaw, err)
	}
	prefix, err := canonicalExistingPath(strings.TrimSpace(string(prefixRaw)))
	if err != nil {
		return fmt.Errorf("resolve Homebrew prefix: %w", err)
	}
	caskroom, err := canonicalExistingPath(strings.TrimSpace(string(caskroomRaw)))
	if err != nil {
		return fmt.Errorf("resolve Homebrew Caskroom: %w", err)
	}
	if prefix != descriptor.Prefix {
		return fmt.Errorf("Homebrew prefix %q does not match executable prefix %q", strings.TrimSpace(string(prefixRaw)), descriptor.Prefix)
	}
	if caskroom != descriptor.Caskroom {
		return fmt.Errorf("Homebrew Caskroom %q does not match executable Caskroom %q", strings.TrimSpace(string(caskroomRaw)), descriptor.Caskroom)
	}
	return nil
}

func canonicalExistingPath(value string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(value))
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func updateWithHomebrew(ctx context.Context, options Options, installer installerDescriptor, descriptor releaseDescriptor) error {
	if installer.Kind != "homebrew" || installer.BrewPath == "" {
		return errors.New("valid Homebrew installation descriptor is required")
	}
	if descriptor.Prerelease || isPrereleaseVersion(descriptor.Version) {
		return WrapError(ErrorInvalidTarget, errors.New("Homebrew can only install stable releases"))
	}
	brew := installer.BrewPath
	if err := validateHomebrewLayout(ctx, options, installer); err != nil {
		return err
	}
	caskAsset, ok := descriptor.Assets[homebrewCaskAssetName(descriptor.Version)]
	if !ok {
		return WrapError(ErrorIntegrity, errors.New("immutable GitHub Release is missing its Homebrew Cask"))
	}
	caskRaw, err := options.Client.fetch(ctx, caskAsset.URL, 64<<10, "immutable Homebrew Cask")
	if err != nil {
		return unavailableOrIntegrityError("download immutable Homebrew Cask", classifyAvailability(ctx, "github", err))
	}
	if got := digestBytes(caskRaw); got != caskAsset.SHA256 {
		return WrapError(ErrorIntegrity, fmt.Errorf("Homebrew Cask digest mismatch: got %s, want %s", got, caskAsset.SHA256))
	}
	intel := descriptor.Assets["ecctl_"+descriptor.Version+"_darwin_amd64.tar.gz"]
	arm := descriptor.Assets["ecctl_"+descriptor.Version+"_darwin_arm64.tar.gz"]
	if err := releaseartifact.ValidateCask(caskRaw, releaseartifact.CaskExpectation{
		Version: descriptor.Version, IntelSHA256: intel.SHA256, ArmSHA256: arm.SHA256,
	}); err != nil {
		return WrapError(ErrorIntegrity, fmt.Errorf("unsafe Homebrew Cask: %w", err))
	}
	caskFile, err := os.CreateTemp("", "ecctl-verified-cask-*.rb")
	if err != nil {
		return fmt.Errorf("create verified Homebrew Cask: %w", err)
	}
	caskPath := caskFile.Name()
	defer os.Remove(caskPath)
	if _, err := caskFile.Write(caskRaw); err != nil {
		caskFile.Close()
		return fmt.Errorf("write verified Homebrew Cask: %w", err)
	}
	if err := caskFile.Sync(); err != nil {
		caskFile.Close()
		return fmt.Errorf("sync verified Homebrew Cask: %w", err)
	}
	if err := caskFile.Close(); err != nil {
		return fmt.Errorf("close verified Homebrew Cask: %w", err)
	}
	caskPath, err = filepath.Abs(caskPath)
	if err != nil || !filepath.IsAbs(caskPath) || !strings.Contains(caskPath, string(filepath.Separator)) {
		return errors.New("verified Homebrew Cask path must be absolute")
	}
	command := "upgrade"
	if options.Force {
		command = "reinstall"
	}
	env := replaceEnvironmentValue(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE", "1")
	output, err := options.RunCommand(ctx, env, brew, command, "--cask", caskPath, "--quiet")
	if err != nil {
		return commandError("brew "+command, output, err)
	}
	if err := validateHomebrewLayout(ctx, options, installer); err != nil {
		return err
	}
	linkedExecutable := filepath.Join(installer.Prefix, "bin", "ecctl")
	return verifyExecutableVersion(ctx, options, linkedExecutable, descriptor.Version)
}

func replaceEnvironmentValue(environment []string, name, value string) []string {
	prefix := name + "="
	updated := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			updated = append(updated, entry)
		}
	}
	return append(updated, prefix+value)
}

func replaceExecutable(ctx context.Context, options Options, binary []byte, target string) (bool, error) {
	directory := filepath.Dir(options.Executable)
	temp, err := os.CreateTemp(directory, ".ecctl-update-*")
	if err != nil {
		return false, fmt.Errorf("create update file beside %s: %w", options.Executable, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return false, err
	}
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := verifyExecutableVersion(ctx, options, tempPath, target); err != nil {
		return false, err
	}
	retained, pending, err := installPreparedExecutable(ctx, options, tempPath, target)
	if retained {
		removeTemp = false
	}
	return pending, err
}

func copyExecutable(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	removeDestination := true
	defer func() {
		_ = destination.Close()
		if removeDestination {
			_ = os.Remove(destinationPath)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	removeDestination = false
	return nil
}

func requireMissingPath(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing update file %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func verifyExecutableVersion(ctx context.Context, options Options, executable, target string) error {
	output, err := options.RunCommand(ctx, nil, executable, "--version")
	if err != nil {
		return commandError("validate updated ecctl", output, err)
	}
	fields := bytes.Fields(output)
	if len(fields) < 2 || string(fields[0]) != "ecctl" || strings.TrimPrefix(string(fields[1]), "v") != target {
		return fmt.Errorf("updated executable reported %q, want ecctl %s", strings.TrimSpace(string(output)), target)
	}
	return nil
}

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	if env != nil {
		command.Env = env
	}
	return command.CombinedOutput()
}

func commandError(name string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return fmt.Errorf("%s failed: %w: %s", name, err, detail)
}
