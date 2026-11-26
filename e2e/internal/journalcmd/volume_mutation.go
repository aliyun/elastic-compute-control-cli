package journalcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

// RawVolumeMutationCandidate recognizes the raw OpenAPI writes that cannot
// participate in case-owned Volume creation and cleanup. Flag values are
// skipped so a read-only request body cannot become an operation name.
func RawVolumeMutationCandidate(command string) bool {
	tokens, err := shlex.Split(command)
	if err != nil {
		lower := strings.ToLower(command)
		return strings.Contains(lower, "call") &&
			(strings.Contains(lower, "fcsandbox") || strings.Contains(lower, "fc-sandbox") || strings.Contains(lower, "sandbox") || strings.Contains(lower, "sbx")) &&
			(strings.Contains(lower, "createvolume") || strings.Contains(lower, "updatevolume") || strings.Contains(lower, "deletevolume"))
	}
	if len(tokens) == 0 || tokens[0] != "ecctl" {
		return false
	}
	path := make([]string, 0, 3)
	for i := 1; i < len(tokens) && len(path) < 3; i++ {
		token := tokens[i]
		if token == "--" {
			// Cobra ends option parsing here; the next token is still a positional.
			continue
		}
		if strings.HasPrefix(token, "-") {
			name, _, inline := strings.Cut(token, "=")
			if !inline && rawQuotaValueFlag(name) && i+1 < len(tokens) {
				i++
			} else if !inline && !rawVolumeBooleanFlag(name) && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") && tokens[i+1] != "call" {
				// Raw OpenAPI options such as --Tag.1.Key consume one value.
				// Boolean root and call-local flags cannot absorb the call path.
				i++
			}
			continue
		}
		path = append(path, strings.ToLower(token))
	}
	if len(path) != 3 || path[0] != "call" || !rawQuotaProduct(strings.TrimSpace(path[1])) {
		return false
	}
	switch strings.TrimSpace(path[2]) {
	case "createvolume", "updatevolume", "deletevolume":
		return true
	}
	return false
}

func rawVolumeBooleanFlag(name string) bool {
	switch name {
	case "--list", "--schema", "--generate-request", "--json", "--agent-envelope", "--no-color", "--help", "--version", "--all", "--openapi", "--force", "--public", "--dry-run", "-h", "-v":
		return true
	}
	return false
}

func volumeCommandFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("E2E Volume command", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, name := range []string{"region", "profile", "lang", "name", "team-id", "status", "resource-group", "user-id", "limit", "next-token", "filter", "fields", "timeout", "oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config", "mount-config", "request", "api-param", "aliyun-arg"} {
		flags.String(name, "", "")
	}
	flags.String("output", "", "")
	for _, name := range []string{"no-wait", "json", "agent-envelope", "no-color", "help", "version", "all", "openapi", "force", "public", "dry-run"} {
		if name == "help" {
			flags.BoolP(name, "h", false, "")
		} else {
			flags.Bool(name, false, "")
		}
	}
	return flags
}

// VolumeCommandAction returns the action of a first-class Volume command. A
// malformed Volume family is an error, so unknown flags cannot hide a write.
func VolumeCommandAction(command string) (string, error) {
	tokens, splitErr := shlex.Split(command)
	if splitErr != nil {
		if visibleVolumeFamily(strings.Fields(command)) {
			return "", fmt.Errorf("invalid Volume command grammar: %w", splitErr)
		}
		return "", nil
	}
	if len(tokens) == 0 || tokens[0] != "ecctl" {
		return "", nil
	}
	flags := volumeCommandFlags()
	if err := flags.Parse(tokens[1:]); err != nil {
		if visibleVolumeFamily(tokens) {
			return "", fmt.Errorf("cannot classify protected Volume command: %w", err)
		}
		return "", nil
	}
	pos := flags.Args()
	if len(pos) < 2 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" {
		return "", nil
	}
	if len(pos) < 3 {
		help, _ := flags.GetBool("help")
		version, _ := flags.GetBool("version")
		if help || version {
			return "", nil
		}
		return "", fmt.Errorf("incomplete protected Volume command")
	}
	return pos[2], nil
}

// VolumeMutationAction classifies first-class Volume writes only.
func VolumeMutationAction(command string) (string, error) {
	action, err := VolumeCommandAction(command)
	if err != nil {
		return "", err
	}
	switch action {
	case "create", "new", "update", "delete", "rm", "remove":
		return action, nil
	default:
		return "", nil
	}
}

func visibleVolumeFamily(tokens []string) bool {
	if len(tokens) == 0 || tokens[0] != "ecctl" {
		return false
	}
	flags := volumeCommandFlags()
	unknownValue := false
	for i := 1; i < len(tokens); i++ {
		if skip := volumeFlagValue(tokens, i, flags); skip >= 0 {
			i += skip
			continue
		}
		if tokens[i] == "--" {
			continue
		}
		if strings.HasPrefix(tokens[i], "-") {
			// A separate unknown option value can precede the real family.
			unknownValue = !strings.Contains(tokens[i], "=")
			continue
		}
		if tokens[i] != "sandbox" && tokens[i] != "sbx" {
			if unknownValue {
				unknownValue = false
				continue
			}
			// An established call (or another command) owns every later
			// positional, including raw OpenAPI values named sandbox/volume.
			return false
		}
		ambiguousFlag := false
		for j := i + 1; j < len(tokens); j++ {
			token := tokens[j]
			if strings.HasPrefix(token, "-") {
				if skip := volumeFlagValue(tokens, j, flags); skip < 0 {
					// An unknown flag may have a separate value before the
					// Volume family. Keep looking after the parser rejects it.
					ambiguousFlag = true
				} else {
					j += skip
				}
				continue
			}
			if token == "volume" {
				return true
			}
			if !ambiguousFlag {
				break
			}
		}
	}
	return false
}

// Known string options consume their next token even when it looks like a
// command path. Unknown options remain ambiguous so protected writes fail
// closed after the main parser rejects them.
func volumeFlagValue(tokens []string, index int, flags *pflag.FlagSet) int {
	token := tokens[index]
	if !strings.HasPrefix(token, "-") {
		return -1
	}
	key, _, inline := strings.Cut(token, "=")
	var flag *pflag.Flag
	if strings.HasPrefix(key, "--") {
		flag = flags.Lookup(strings.TrimPrefix(key, "--"))
	}
	switch key {
	case "-o":
		flag = flags.Lookup("output")
	case "-p":
		flag = flags.Lookup("profile")
	case "-h":
		flag = flags.Lookup("help")
	}
	if flag == nil {
		return -1
	}
	if !inline && flag.NoOptDefVal == "" && index+1 < len(tokens) {
		return 1
	}
	return 0
}
