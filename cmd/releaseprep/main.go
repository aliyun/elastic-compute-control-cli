package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/internal/releaseartifact"
)

var (
	githubModulePattern = regexp.MustCompile(`github\.com/[A-Za-z0-9_.-]+/ecctl([^A-Za-z0-9_.-]|$)`)
	githubOwnerPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	githubRepoPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	goInstallPattern    = regexp.MustCompile(`(?m)\bgo[\t ]+install[\t ]+\S+`)
	semverIdentifier    = regexp.MustCompile(`^[0-9A-Za-z-]+$`)
	semverNumeric       = regexp.MustCompile(`^[0-9]+$`)
	caskVersionPattern  = regexp.MustCompile(`(?m)^[\t ]*version "([^"]+)"[\t ]*$`)
	checksumLinePattern = regexp.MustCompile(`^([0-9a-f]{64})[\t ]+([^\t ]+)$`)
)

func main() {
	module := flag.String("module", "", "public Go module path, for example github.com/<owner>/<repo>")
	write := flag.Bool("write", false, "rewrite repository files to the public module path")
	check := flag.Bool("check", false, "check whether the repository is ready for a public release tag")
	checkHomebrew := flag.Bool("check-homebrew-version", false, "check whether a release tag advances the current Homebrew Cask")
	checkVersion := flag.Bool("check-version-file", false, "check the canonical release version file")
	verifyHomebrew := flag.Bool("verify-homebrew-cask", false, "strictly validate a generated Homebrew Cask against immutable release checksums")
	repository := flag.String("repository", "", "GitHub repository identity for public release checks")
	releaseTag := flag.String("release-tag", "", "candidate release tag for version and Homebrew checks")
	versionFile := flag.String("version-file", "version.txt", "path to the canonical release version file")
	previousVersionFile := flag.String("previous-version-file", "", "path to the previous canonical release version file")
	releasedTagsFile := flag.String("released-tags-file", "", "path to newline-delimited existing release tags")
	cask := flag.String("cask", "", "path to the current Homebrew Cask")
	checksumsFile := flag.String("checksums-file", "", "path to the immutable release checksums")
	firstHomebrewRelease := flag.Bool("first-homebrew-release", false, "allow a release when the repository does not have a Homebrew Cask yet")
	allowExistingRelease := flag.Bool("allow-existing-release", false, "validate an existing immutable release without requiring version advancement")
	flag.Parse()

	selected := 0
	for _, enabled := range []bool{*write, *check, *checkHomebrew, *checkVersion, *verifyHomebrew} {
		if enabled {
			selected++
		}
	}
	if selected != 1 {
		exitError(errors.New("exactly one operation is required"))
	}
	if *allowExistingRelease && !*checkVersion {
		exitError(errors.New("--allow-existing-release is only valid with --check-version-file"))
	}
	root, err := os.Getwd()
	if err != nil {
		exitError(err)
	}
	if *write {
		if err := rewritePublicModule(root, *module); err != nil {
			exitError(err)
		}
		return
	}
	if *checkHomebrew {
		if err := checkHomebrewCaskVersion(*releaseTag, *cask, *firstHomebrewRelease); err != nil {
			exitError(err)
		}
		return
	}
	if *checkVersion {
		version, err := checkReleaseVersion(*versionFile, *previousVersionFile, *releasedTagsFile, *releaseTag, *allowExistingRelease)
		if err != nil {
			exitError(err)
		}
		fmt.Println(version)
		return
	}
	if *verifyHomebrew {
		if err := verifyHomebrewCask(*cask, *checksumsFile, strings.TrimPrefix(*releaseTag, "v")); err != nil {
			exitError(err)
		}
		return
	}
	if err := checkReleaseReady(root, *repository); err != nil {
		exitError(err)
	}
}

func verifyHomebrewCask(caskPath string, checksumsPath string, releaseVersion string) error {
	if caskPath == "" || checksumsPath == "" || releaseVersion == "" {
		return errors.New("--cask, --checksums-file, and --release-tag are required")
	}
	if _, err := parseSemVersion(releaseVersion); err != nil {
		return fmt.Errorf("invalid release version %q: %w", releaseVersion, err)
	}
	caskRaw, err := os.ReadFile(caskPath)
	if err != nil {
		return err
	}
	checksums, err := readReleaseChecksums(checksumsPath)
	if err != nil {
		return err
	}
	intelName := "ecctl_" + releaseVersion + "_darwin_amd64.tar.gz"
	armName := "ecctl_" + releaseVersion + "_darwin_arm64.tar.gz"
	intel, ok := checksums[intelName]
	if !ok {
		return fmt.Errorf("release checksums are missing %s", intelName)
	}
	arm, ok := checksums[armName]
	if !ok {
		return fmt.Errorf("release checksums are missing %s", armName)
	}
	return releaseartifact.ValidateCask(caskRaw, releaseartifact.CaskExpectation{Version: releaseVersion, IntelSHA256: intel, ArmSHA256: arm})
}

func readReleaseChecksums(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	checksums := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		match := checksumLinePattern.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("invalid checksum line %q", line)
		}
		if _, exists := checksums[match[2]]; exists {
			return nil, fmt.Errorf("duplicate checksum for %s", match[2])
		}
		checksums[match[2]] = match[1]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return checksums, nil
}

type semVersion struct {
	major      string
	minor      string
	patch      string
	prerelease []string
}

func checkReleaseVersion(versionFile string, previousVersionFile string, releasedTagsFile string, releaseTag string, allowExistingRelease ...bool) (string, error) {
	allowExisting := len(allowExistingRelease) == 1 && allowExistingRelease[0]
	if len(allowExistingRelease) > 1 {
		return "", errors.New("allow-existing-release may only be specified once")
	}
	if allowExisting && releaseTag == "" {
		return "", errors.New("--allow-existing-release requires --release-tag")
	}
	if allowExisting && (previousVersionFile != "" || releasedTagsFile != "") {
		return "", errors.New("--allow-existing-release cannot be combined with previous version or released tag advancement checks")
	}
	version, parsed, err := readReleaseVersionFile(versionFile)
	if err != nil {
		return "", err
	}
	if releaseTag != "" {
		expectedTag := "v" + version
		if releaseTag != expectedTag {
			return "", fmt.Errorf("release tag %q does not match version file; want %q", releaseTag, expectedTag)
		}
	}
	if allowExisting {
		return version, nil
	}
	if previousVersionFile != "" {
		previousVersion, previousParsed, err := readReleaseVersionFile(previousVersionFile)
		if err != nil {
			return "", fmt.Errorf("invalid previous version file: %w", err)
		}
		if order := compareSemVersion(parsed, previousParsed); order <= 0 {
			return "", fmt.Errorf("release version %s must be greater than previous version %s", version, previousVersion)
		}
	}
	if releasedTagsFile != "" {
		raw, err := os.ReadFile(releasedTagsFile)
		if err != nil {
			return "", err
		}
		expectedTag := "v" + version
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		for scanner.Scan() {
			tag := scanner.Text()
			if tag == expectedTag {
				continue
			}
			if !strings.HasPrefix(tag, "v") {
				return "", fmt.Errorf("existing release tag must start with v, got %q", tag)
			}
			released, err := parseSemVersion(strings.TrimPrefix(tag, "v"))
			if err != nil {
				return "", fmt.Errorf("invalid existing release tag %q: %w", tag, err)
			}
			if compareSemVersion(parsed, released) <= 0 {
				return "", fmt.Errorf("release version %s must be greater than existing release tag %s", version, tag)
			}
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
	}
	return version, nil
}

func readReleaseVersionFile(path string) (string, semVersion, error) {
	var parsed semVersion
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", parsed, err
	}
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return "", parsed, errors.New("version file must not contain a byte-order mark")
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return "", parsed, errors.New("version file must contain one line terminated by a newline")
	}
	if bytes.Count(raw, []byte{'\n'}) != 1 || bytes.Contains(raw, []byte{'\r'}) {
		return "", parsed, errors.New("version file must contain exactly one Unix-style line")
	}
	version := string(raw[:len(raw)-1])
	if version == "" || strings.TrimSpace(version) != version {
		return "", parsed, errors.New("version file must not be empty or contain surrounding whitespace")
	}
	if strings.HasPrefix(version, "v") {
		return "", parsed, errors.New("version file must not include the v tag prefix")
	}
	if strings.Contains(version, "+") {
		return "", parsed, errors.New("version file must not include build metadata")
	}
	parsed, err = parseSemVersion(version)
	if err != nil {
		return "", parsed, fmt.Errorf("invalid release version %q: %w", version, err)
	}
	return version, parsed, nil
}

func checkHomebrewCaskVersion(releaseTag string, caskPath string, firstRelease bool) error {
	if !strings.HasPrefix(releaseTag, "v") {
		return fmt.Errorf("release tag must start with v, got %q", releaseTag)
	}
	candidate, err := parseSemVersion(strings.TrimPrefix(releaseTag, "v"))
	if err != nil {
		return fmt.Errorf("invalid release tag %q: %w", releaseTag, err)
	}
	if len(candidate.prerelease) > 0 {
		return nil
	}
	if firstRelease {
		if caskPath != "" {
			return errors.New("--first-homebrew-release cannot be combined with --cask")
		}
		return nil
	}
	if caskPath == "" {
		return errors.New("either --cask or --first-homebrew-release is required")
	}
	raw, err := os.ReadFile(caskPath)
	if err != nil {
		return err
	}
	matches := caskVersionPattern.FindAllSubmatch(raw, -1)
	if len(matches) != 1 {
		return fmt.Errorf("expected exactly one version in current Homebrew Cask, found %d", len(matches))
	}
	currentText := string(matches[0][1])
	current, err := parseSemVersion(currentText)
	if err != nil {
		return fmt.Errorf("invalid current Homebrew Cask version %q: %w", currentText, err)
	}
	order := compareSemVersion(candidate, current)
	if order < 0 {
		return fmt.Errorf("refusing to downgrade Homebrew Cask from %s to %s", currentText, strings.TrimPrefix(releaseTag, "v"))
	}
	if order == 0 {
		return fmt.Errorf("refusing to replace Homebrew Cask version %s with equal-precedence tag %s", currentText, releaseTag)
	}
	return nil
}

func parseSemVersion(raw string) (semVersion, error) {
	var parsed semVersion
	if raw == "" {
		return parsed, errors.New("version is empty")
	}
	precedence, build, hasBuild := strings.Cut(raw, "+")
	if hasBuild {
		if err := validateSemVersionIdentifiers(build, false); err != nil {
			return parsed, fmt.Errorf("invalid build metadata: %w", err)
		}
		if strings.Contains(build, "+") {
			return parsed, errors.New("version contains more than one build metadata separator")
		}
	}
	core, prerelease, hasPrerelease := strings.Cut(precedence, "-")
	if hasPrerelease {
		if err := validateSemVersionIdentifiers(prerelease, true); err != nil {
			return parsed, fmt.Errorf("invalid prerelease: %w", err)
		}
		parsed.prerelease = strings.Split(prerelease, ".")
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return parsed, errors.New("version core must contain major, minor, and patch")
	}
	values := []*string{&parsed.major, &parsed.minor, &parsed.patch}
	for i, part := range parts {
		if !semverNumeric.MatchString(part) || (len(part) > 1 && part[0] == '0') {
			return parsed, fmt.Errorf("invalid numeric identifier %q", part)
		}
		*values[i] = part
	}
	return parsed, nil
}

func validateSemVersionIdentifiers(raw string, prerelease bool) error {
	identifiers := strings.Split(raw, ".")
	for _, identifier := range identifiers {
		if !semverIdentifier.MatchString(identifier) {
			return fmt.Errorf("invalid identifier %q", identifier)
		}
		if prerelease && semverNumeric.MatchString(identifier) && len(identifier) > 1 && identifier[0] == '0' {
			return fmt.Errorf("numeric identifier %q has a leading zero", identifier)
		}
	}
	return nil
}

func compareSemVersion(left semVersion, right semVersion) int {
	for _, pair := range [][2]string{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if order := compareNumericIdentifier(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	if len(left.prerelease) == 0 || len(right.prerelease) == 0 {
		if len(left.prerelease) == len(right.prerelease) {
			return 0
		}
		if len(left.prerelease) == 0 {
			return 1
		}
		return -1
	}
	for i := 0; i < len(left.prerelease) && i < len(right.prerelease); i++ {
		leftID := left.prerelease[i]
		rightID := right.prerelease[i]
		if leftID == rightID {
			continue
		}
		leftNumeric := semverNumeric.MatchString(leftID)
		rightNumeric := semverNumeric.MatchString(rightID)
		switch {
		case leftNumeric && rightNumeric:
			return compareNumericIdentifier(leftID, rightID)
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case leftID < rightID:
			return -1
		default:
			return 1
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}

func compareNumericIdentifier(left string, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func validatePublicModule(module string) error {
	if module == "" {
		return errors.New("PUBLIC_MODULE is required")
	}
	parts := strings.Split(module, "/")
	if len(parts) != 3 || parts[0] != "github.com" || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("PUBLIC_MODULE must look like github.com/<owner>/<repo>, got %q", module)
	}
	if strings.Contains(module, "<") || strings.Contains(module, ">") {
		return fmt.Errorf("PUBLIC_MODULE owner and repository must be frozen, got %q", module)
	}
	if !githubOwnerPattern.MatchString(parts[1]) {
		return fmt.Errorf("PUBLIC_MODULE owner must be a valid GitHub namespace, got %q", module)
	}
	if !githubRepoPattern.MatchString(parts[2]) {
		return fmt.Errorf("PUBLIC_MODULE repository must be a valid GitHub repository name, got %q", module)
	}
	return nil
}

func rewritePublicModule(root string, module string) error {
	if err := validatePublicModule(module); err != nil {
		return err
	}
	rootGoMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	oldModule := modulePath(rootGoMod)
	if oldModule == "" {
		return errors.New("root go.mod does not declare a module path")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !rewriteCandidate(path) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := rewriteContent(path, raw, oldModule, module)
		if bytes.Equal(raw, updated) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, updated, info.Mode())
	})
}

func checkReleaseReady(root string, repository string) error {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	module := modulePath(raw)
	if err := validatePublicModule(module); err != nil {
		return fmt.Errorf("module path is not frozen: %w", err)
	}
	expectedModule := "github.com/" + repository
	if repository == "" || module != expectedModule {
		return fmt.Errorf("public release module must match repository %q, got %q", expectedModule, module)
	}
	if hasReplaceDirective(raw) {
		return errors.New("go.mod contains replace directives; go install pkg@version requires the target module to be replace-free")
	}
	if err := checkNoPlaceholders(root); err != nil {
		return err
	}
	if err := checkGoInstallModule(root, module); err != nil {
		return err
	}
	return nil
}

func checkGoInstallModule(root string, module string) error {
	expectedPrefix := "go install " + module + "/cmd/ecctl@"
	var hits []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !rewriteCandidate(path) {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, command := range goInstallPattern.FindAllString(string(raw), -1) {
			if !strings.HasPrefix(command, expectedPrefix) {
				hits = append(hits, path+": "+command)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(hits) > 0 {
		return fmt.Errorf("go install commands must use public module %q: %s", module, strings.Join(hits, ", "))
	}
	return nil
}

func modulePath(raw []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func hasReplaceDirective(raw []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "replace" || strings.HasPrefix(line, "replace ") || strings.HasPrefix(line, "replace(") {
			return true
		}
	}
	return false
}

func checkNoPlaceholders(root string) error {
	var hits []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !rewriteCandidate(path) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		if strings.Contains(text, "github.com/<owner>/ecctl") {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(hits) > 0 {
		return fmt.Errorf("public release placeholders remain in %s", strings.Join(hits, ", "))
	}
	return nil
}

func rewriteContent(path string, raw []byte, oldModule string, module string) []byte {
	text := string(raw)
	if filepath.Base(path) == "go.mod" {
		lines := strings.SplitAfter(text, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "module ") {
				currentModule := strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
				publicModule := currentModule
				switch {
				case currentModule == oldModule:
					publicModule = module
				case strings.HasPrefix(currentModule, oldModule+"/"):
					publicModule = module + strings.TrimPrefix(currentModule, oldModule)
				}
				ending := ""
				if strings.HasSuffix(line, "\n") {
					ending = "\n"
				}
				lines[i] = "module " + publicModule + ending
				break
			}
		}
		text = strings.Join(lines, "")
	}
	text = strings.ReplaceAll(text, `"`+oldModule+`/`, `"`+module+`/`)
	text = strings.ReplaceAll(text, "-X "+oldModule+"/", "-X "+module+"/")
	text = strings.ReplaceAll(text, "go install "+oldModule+"/", "go install "+module+"/")
	text = strings.ReplaceAll(text, "github.com/<owner>/ecctl", module)
	text = githubModulePattern.ReplaceAllString(text, module+"${1}")
	return []byte(text)
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".claude", ".worktrees", "worktrees", "bin", "dist", "reports", "node_modules", ".docusaurus", "build":
		return true
	default:
		return false
	}
}

func rewriteCandidate(path string) bool {
	if releasePrepInternalPath(path) {
		return false
	}
	switch filepath.Base(path) {
	case "go.mod", "README.md":
		return true
	}
	switch filepath.Ext(path) {
	case ".cjs", ".css", ".go", ".html", ".js", ".json", ".jsx", ".md", ".mjs", ".scss", ".sh", ".toml", ".ts", ".tsx", ".txt", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func releasePrepInternalPath(path string) bool {
	slash := filepath.ToSlash(path)
	return strings.Contains(slash, "/cmd/releaseprep/") ||
		strings.HasPrefix(slash, "cmd/releaseprep/") ||
		strings.Contains(slash, "/docs/superpowers/") ||
		strings.HasPrefix(slash, "docs/superpowers/")
}
