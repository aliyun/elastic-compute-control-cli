package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var probeDirectoryCaseInsensitive = directoryCaseInsensitive

// SameTarget reports whether two requested paths resolve to the same file.
// When both files are absent and differ only by case, it probes the containing
// filesystem instead of assuming case behavior from the operating system.
func SameTarget(first, second string) (bool, error) {
	if first == "" || second == "" {
		return false, nil
	}
	left := canonicalComparisonPath(first)
	right := canonicalComparisonPath(second)
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo), nil
	}
	if leftErr != nil && !errors.Is(leftErr, os.ErrNotExist) {
		return false, leftErr
	}
	if rightErr != nil && !errors.Is(rightErr, os.ErrNotExist) {
		return false, rightErr
	}
	if left == right {
		return true, nil
	}
	if !strings.EqualFold(left, right) {
		return false, nil
	}
	leftParentInfo, err := os.Stat(filepath.Dir(left))
	if err != nil {
		return false, err
	}
	rightParentInfo, err := os.Stat(filepath.Dir(right))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !os.SameFile(leftParentInfo, rightParentInfo) {
		return false, nil
	}
	caseInsensitive, err := probeDirectoryCaseInsensitive(filepath.Dir(left))
	if err != nil {
		return false, err
	}
	return caseInsensitive, nil
}

func canonicalComparisonPath(path string) string {
	if target, err := Resolve(path, false); err == nil {
		return filepath.Clean(target.Path())
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func directoryCaseInsensitive(dir string) (bool, error) {
	file, err := os.CreateTemp(dir, ".ecctl-case-probe-Aa-*")
	if err != nil {
		return false, err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Close(); err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	alternate := filepath.Join(dir, swapFilenameCase(filepath.Base(path)))
	if alternate == path {
		return false, errors.New("filesystem case probe could not create a distinct name")
	}
	alternateInfo, err := os.Stat(alternate)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !os.SameFile(info, alternateInfo) {
		return false, fmt.Errorf("filesystem case probe collided with %s", alternate)
	}
	return true, nil
}

func swapFilenameCase(name string) string {
	runes := []rune(name)
	for index, value := range runes {
		switch {
		case unicode.IsLower(value):
			runes[index] = unicode.ToUpper(value)
			return string(runes)
		case unicode.IsUpper(value):
			runes[index] = unicode.ToLower(value)
			return string(runes)
		}
	}
	return name
}
