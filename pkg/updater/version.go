package updater

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	numericIdentifier = regexp.MustCompile(`^[0-9]+$`)
	semverIdentifier  = regexp.MustCompile(`^[0-9A-Za-z-]+$`)
)

type semanticVersion struct {
	major      string
	minor      string
	patch      string
	prerelease []string
}

func NormalizeVersion(raw string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if strings.Contains(value, "+") {
		return "", errors.New("build metadata is not supported")
	}
	if _, err := parseVersion(value); err != nil {
		return "", err
	}
	return value, nil
}

func CompareVersions(left, right string) (int, error) {
	l, err := parseVersion(strings.TrimPrefix(left, "v"))
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", left, err)
	}
	r, err := parseVersion(strings.TrimPrefix(right, "v"))
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", right, err)
	}
	return compareVersion(l, r), nil
}

func isPrereleaseVersion(version string) bool {
	parsed, err := parseVersion(strings.TrimPrefix(version, "v"))
	return err == nil && len(parsed.prerelease) > 0
}

func parseVersion(raw string) (semanticVersion, error) {
	var parsed semanticVersion
	if raw == "" {
		return parsed, errors.New("version is empty")
	}
	if strings.Contains(raw, "+") {
		return parsed, errors.New("build metadata is not supported")
	}
	core, prerelease, hasPrerelease := strings.Cut(raw, "-")
	if hasPrerelease {
		if prerelease == "" {
			return parsed, errors.New("prerelease is empty")
		}
		for _, identifier := range strings.Split(prerelease, ".") {
			if !semverIdentifier.MatchString(identifier) {
				return parsed, fmt.Errorf("invalid prerelease identifier %q", identifier)
			}
			if numericIdentifier.MatchString(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return parsed, fmt.Errorf("numeric prerelease identifier %q has a leading zero", identifier)
			}
			parsed.prerelease = append(parsed.prerelease, identifier)
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return parsed, errors.New("version must contain major, minor, and patch")
	}
	values := []*string{&parsed.major, &parsed.minor, &parsed.patch}
	for index, part := range parts {
		if !numericIdentifier.MatchString(part) || (len(part) > 1 && part[0] == '0') {
			return parsed, fmt.Errorf("invalid numeric identifier %q", part)
		}
		*values[index] = part
	}
	return parsed, nil
}

func compareVersion(left, right semanticVersion) int {
	for _, pair := range [][2]string{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if result := compareNumeric(pair[0], pair[1]); result != 0 {
			return result
		}
	}
	if len(left.prerelease) == 0 || len(right.prerelease) == 0 {
		switch {
		case len(left.prerelease) == len(right.prerelease):
			return 0
		case len(left.prerelease) == 0:
			return 1
		default:
			return -1
		}
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		l, r := left.prerelease[index], right.prerelease[index]
		if l == r {
			continue
		}
		lNumeric, rNumeric := numericIdentifier.MatchString(l), numericIdentifier.MatchString(r)
		switch {
		case lNumeric && rNumeric:
			return compareNumeric(l, r)
		case lNumeric:
			return -1
		case rNumeric:
			return 1
		case l < r:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(left.prerelease) < len(right.prerelease):
		return -1
	case len(left.prerelease) > len(right.prerelease):
		return 1
	default:
		return 0
	}
}

func compareNumeric(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}
