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
)

var (
	githubModulePattern = regexp.MustCompile(`github\.com/[A-Za-z0-9_.-]+/ecctl`)
	githubOwnerPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

func main() {
	module := flag.String("module", "", "public Go module path, for example github.com/<owner>/ecctl")
	write := flag.Bool("write", false, "rewrite repository files to the public module path")
	check := flag.Bool("check", false, "check whether the repository is ready for a public release tag")
	flag.Parse()

	if *write == *check {
		exitError(errors.New("exactly one of --write or --check is required"))
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
	if err := checkReleaseReady(root); err != nil {
		exitError(err)
	}
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
	if len(parts) != 3 || parts[0] != "github.com" || parts[1] == "" || parts[2] != "ecctl" {
		return fmt.Errorf("PUBLIC_MODULE must look like github.com/<owner>/ecctl, got %q", module)
	}
	if strings.Contains(parts[1], "<") || strings.Contains(parts[1], ">") {
		return fmt.Errorf("PUBLIC_MODULE owner must be frozen, got %q", module)
	}
	if !githubOwnerPattern.MatchString(parts[1]) {
		return fmt.Errorf("PUBLIC_MODULE owner must be a valid GitHub namespace, got %q", module)
	}
	return nil
}

func rewritePublicModule(root string, module string) error {
	if err := validatePublicModule(module); err != nil {
		return err
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
		updated := rewriteContent(path, raw, module)
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

func checkReleaseReady(root string) error {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	module := modulePath(raw)
	if err := validatePublicModule(module); err != nil {
		return fmt.Errorf("module path is not frozen: %w", err)
	}
	if hasReplaceDirective(raw) {
		return errors.New("go.mod contains replace directives; go install pkg@version requires the target module to be replace-free")
	}
	if err := checkNoPlaceholders(root); err != nil {
		return err
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

func rewriteContent(path string, raw []byte, module string) []byte {
	text := string(raw)
	if filepath.Base(path) == "go.mod" {
		lines := strings.SplitAfter(text, "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "module ") {
				ending := ""
				if strings.HasSuffix(line, "\n") {
					ending = "\n"
				}
				lines[i] = "module " + module + ending
				break
			}
		}
		text = strings.Join(lines, "")
	}
	text = strings.ReplaceAll(text, "ecctl/pkg/", module+"/pkg/")
	text = strings.ReplaceAll(text, "ecctl/specs/", module+"/specs/")
	text = strings.ReplaceAll(text, `"ecctl/`, `"`+module+`/`)
	text = strings.ReplaceAll(text, "github.com/<owner>/ecctl", module)
	text = githubModulePattern.ReplaceAllString(text, module)
	return []byte(text)
}

func skipDir(name string) bool {
	switch name {
	case ".git", "bin", "dist", "reports", "node_modules", ".docusaurus", "build":
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
	case ".go", ".md", ".ts", ".tsx", ".yaml", ".yml":
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
