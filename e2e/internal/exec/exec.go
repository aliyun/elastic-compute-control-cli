// Package exec runs a rendered ecctl command line as a subprocess and parses
// its JSON stdout. It strips inherited color forcers and passes an explicit
// no-color option for protected machine output.
package exec

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/shlex"
)

// Config controls how commands are executed.
type Config struct {
	Bin               string // substituted for a leading "ecctl" token (default "ecctl")
	ExpectedBinSHA256 string // protected calls execute a private copy with this digest
	Region            string // appended as --region to ecctl commands when set
	Profile           string // pinned profile for protected Quota lifecycle commands
	Env               []string
	ExpectedIdentity  *IdentityPin
	MaxOutputBytes    int // optional per-stream bound for setup probes
}

// Result captures everything one invocation produced, for assertions/reports.
type Result struct {
	Command           string        `json:"command"` // rendered, bin-substituted, for repro
	Argv              []string      `json:"-"`       // exact argv passed to the verified executable
	VerifiedBinSHA256 string        `json:"-"`       // set only after the executable copy is verified
	Exit              int           `json:"exit"`
	Stdout            string        `json:"-"`
	Stderr            string        `json:"stderr,omitempty"`
	JSON              any           `json:"-"` // parsed Stdout, nil if not JSON
	Duration          time.Duration `json:"-"`
	Err               error         `json:"-"` // process failed to start / context cancelled
	Started           bool          `json:"started"`
}

// Run executes one rendered command line under ctx (which carries the timeout).
func Run(ctx context.Context, cfg Config, rendered string) Result {
	res := Result{Command: rendered}
	launchBin, args, ecctlCommand, err := prepareCommandArgs(cfg, rendered)
	if err != nil {
		res.Err = err
		res.Exit = -1
		return res
	}
	res.Command = recordedCommand(launchBin, args)
	if cfg.ExpectedBinSHA256 != "" {
		if !ecctlCommand {
			res.Err = fmt.Errorf("verified ecctl command changed its executable")
			res.Exit = -1
			return res
		}
		var release func()
		launchBin, release, err = VerifiedExecutableCopy(launchBin, cfg.ExpectedBinSHA256)
		if err != nil {
			res.Err = err
			res.Exit = -1
			return res
		}
		defer release()
		res.VerifiedBinSHA256 = cfg.ExpectedBinSHA256
	}
	res.Argv = append([]string(nil), args...)

	cmd := osexec.CommandContext(ctx, launchBin, args...)
	if cfg.MaxOutputBytes > 0 {
		cmd.WaitDelay = time.Second
	}
	identityEnv, identityErr := identityEnvironment(cfg.ExpectedIdentity)
	if identityErr != nil {
		res.Err = identityErr
		res.Exit = -1
		return res
	}
	cmd.Env = append(CommandEnvironment(cfg.Env), identityEnv...)
	var out, errb boundedCommandOutput
	out.limit, errb.limit = cfg.MaxOutputBytes, cfg.MaxOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &errb

	start := time.Now()
	runErr := cmd.Start()
	if runErr == nil {
		res.Started = true
		runErr = cmd.Wait()
	}
	res.Duration = time.Since(start)
	res.Stdout = out.String()
	res.Stderr = errb.String()
	if out.exceeded || errb.exceeded {
		res.Err = fmt.Errorf("verified command output exceeded its setup bound")
		res.Exit = -1
		return res
	}

	switch e := runErr.(type) {
	case nil:
		res.Exit = 0
	case *osexec.ExitError:
		res.Exit = e.ExitCode()
	default:
		res.Err = runErr
		res.Exit = -1
	}

	if v := strings.TrimSpace(res.Stdout); v != "" {
		if parsed, err := DecodeCommandJSON([]byte(v)); err == nil {
			res.JSON = parsed
		}
	}
	return res
}

func prepareCommandArgs(cfg Config, rendered string) (string, []string, bool, error) {
	tokens, err := shlex.Split(rendered)
	if err != nil || len(tokens) == 0 {
		return "", nil, false, fmt.Errorf("invalid command: %v", err)
	}
	bin := cfg.Bin
	if bin == "" {
		bin = "ecctl"
	}
	ecctlCommand := tokens[0] == "ecctl"
	if ecctlCommand {
		tokens[0] = bin
	}
	args := tokens[1:]
	if cfg.Region != "" {
		_, count, separator, regionErr := scanStringOption(args, "--region", false)
		if regionErr != nil {
			return "", nil, false, fmt.Errorf("region selector is invalid: %w", regionErr)
		}
		if count == 0 {
			args = appendDefaultOption(args, "--region", cfg.Region, separator)
		}
	}
	if cfg.Profile != "" {
		selected, count, profileErr := actualProfileOption(args)
		if profileErr != nil || count > 1 || count == 1 && selected != cfg.Profile {
			return "", nil, false, fmt.Errorf("protected profile selector changed")
		}
		if count == 0 {
			args = append(args, "--profile", cfg.Profile)
		}
	}
	// Protected raw fixture calls always use the JSON contract, including
	// journals written before the explicit output flag was added to fixtures.
	if cfg.ExpectedIdentity != nil && len(args) >= 2 && args[0] == "call" && (args[1] == "oss" || args[1] == "ram") {
		output, count, separator, outputErr := scanStringOption(args, "--output", false)
		if outputErr != nil || separator >= 0 || count > 1 || count == 1 && output != "json" {
			return "", nil, false, fmt.Errorf("protected fixture output selector changed")
		}
		if count == 0 {
			args = append(args, "--output=json")
		}
	}
	if ecctlCommand && machineProofCommand(cfg, args) {
		// Machine proofs must stay parseable even when the caller's display mode
		// requests colored JSON. Keep this on the actual executed argv.
		selected, count, separator, colorErr := scanStringOption(args, "--no-color", false)
		noColor, valueErr := strconv.ParseBool(selected)
		if colorErr != nil || count > 1 || count == 1 && (valueErr != nil || !noColor) {
			return "", nil, false, fmt.Errorf("machine output color selector changed")
		}
		if count == 0 {
			if separator >= 0 {
				args = append(args, "")
				copy(args[separator+1:], args[separator:len(args)-1])
				args[separator] = "--no-color=true"
			} else {
				args = append(args, "--no-color=true")
			}
		}
	}
	return tokens[0], args, ecctlCommand, nil
}

func machineProofCommand(cfg Config, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "capabilities" {
		return true
	}
	if len(args) >= 2 && args[0] == "configure" && args[1] == "get" {
		return true
	}
	if len(args) >= 3 && args[0] == "call" && args[1] == "sts" && args[2] == "GetCallerIdentity" {
		return true
	}
	if cfg.ExpectedIdentity == nil || len(args) < 2 {
		return false
	}
	if args[0] == "call" && (args[1] == "oss" || args[1] == "ram") {
		return true
	}
	path := sandboxProofCommandPath(args)
	if path[0] != "sandbox" && path[0] != "sbx" {
		return false
	}
	switch path[1] {
	case "team", "volume":
		return true
	case "template", "tpl":
		switch path[2] {
		case "create", "new", "list", "ls", "get", "show", "describe", "delete", "rm", "remove":
			selected, _, _, err := scanStringOption(args, "--openapi", false)
			native, boolErr := strconv.ParseBool(selected)
			return err == nil && boolErr == nil && native
		}
	}
	return false
}

// Match the command words across root options and the native Template selector.
// Split string options consume even flag-shaped values. An actual -- ends
// option parsing, so words beyond it cannot identify a subcommand.
func sandboxProofCommandPath(args []string) [3]string {
	var path [3]string
	next := 0
	for index := 0; index < len(args); index++ {
		arg := args[index]
		name, _, inline := strings.Cut(arg, "=")
		switch name {
		case "--region", "--profile", "--lang", "--output":
			if !inline {
				index++
			}
		case "--agent-envelope", "--json", "--no-color", "--help", "-h", "--version", "-v", "--openapi":
			// Booleans use a bare flag or an inline value.
		default:
			if strings.HasPrefix(arg, "-") {
				return path
			}
			path[next] = arg
			next++
			if next == len(path) {
				return path
			}
		}
	}
	return path
}

type boundedCommandOutput struct {
	strings.Builder
	limit    int
	exceeded bool
}

func (b *boundedCommandOutput) Write(p []byte) (int, error) {
	length := len(p)
	if b.limit > 0 {
		remaining := b.limit - b.Len()
		if remaining <= 0 {
			b.exceeded = true
			return length, nil
		}
		if len(p) > remaining {
			b.exceeded = true
			p = p[:remaining]
		}
	}
	_, err := b.Builder.Write(p)
	return length, err
}

// Record private configuration at actual argv boundaries. Reconstructing it
// from joined shell text loses quoting and cannot safely delimit bad input.
// The subprocess still receives the original args, including @file values.
func recordedCommand(bin string, args []string) string {
	shown := append([]string{bin}, args...)
	for index := 1; index < len(shown); index++ {
		key, value, inline := strings.Cut(shown[index], "=")
		if key != "--runtime-config" && key != "--build-config" {
			continue
		}
		if inline {
			if !strings.HasPrefix(value, "@") {
				shown[index] = key + "=***"
			}
		} else if index+1 < len(shown) {
			index++
			if !strings.HasPrefix(shown[index], "@") {
				shown[index] = "***"
			}
		}
	}
	return strings.Join(shown, " ")
}

// The CLI's string options consume the next argv token even when it looks
// like another flag. Count only actual profile options, not their values.
func actualProfileOption(args []string) (string, int, error) {
	return actualStringOption(args, "--profile")
}

func actualStringOption(args []string, wanted string) (string, int, error) {
	value, count, _, err := scanStringOption(args, wanted, true)
	return value, count, err
}

// Generic region injection must tolerate raw OpenAPI flags. The CLI treats an
// unknown split parameter's following non-flag token as its value; a following
// flag remains a real option. Protected profile parsing stays strict.
func scanStringOption(args []string, wanted string, strict bool) (string, int, int, error) {
	stringFlags := map[string]bool{}
	for _, name := range []string{"region", "profile", "lang", "output", "name", "team-id", "oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config", "mount-config", "runtime-config", "build-config", "timeout", "timeout-seconds", "duration", "count", "template", "cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "steps", "start-cmd", "ready-cmd", "tag", "step", "build-id", "cursor", "limit", "next-token", "filter", "alias", "disk-size-mb", "fields", "description", "resource-group", "user-id", "plan", "request", "api-param", "aliyun-arg", "status", "policy-name", "policy-type", "fallback-default-version", "cluster", "instance-name", "cpu-cores", "memory-gb", "page", "Bucket", "Key", "metadata", "snapshot-name"} {
		stringFlags["--"+name] = true
	}
	stringFlags["-o"] = true
	boolFlags := map[string]bool{}
	for _, name := range []string{"force", "openapi", "no-wait", "json", "agent-envelope", "no-color", "help", "version", "public", "dry-run", "all", "release", "hyper"} {
		boolFlags["--"+name] = true
	}
	boolFlags["-h"], boolFlags["-v"] = true, true
	selected, count, separator := "", 0, -1
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if strict {
				return "", 0, -1, fmt.Errorf("protected command cannot hide its profile after --")
			}
			separator = index
			break
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		key, value, inline := strings.Cut(arg, "=")
		if stringFlags[key] {
			if !inline {
				index++
				if index >= len(args) {
					return "", 0, -1, fmt.Errorf("missing protected command option value")
				}
				value = args[index]
			}
			if key == wanted {
				count++
				selected = value
			}
			continue
		}
		if !boolFlags[key] && strict {
			return "", 0, -1, fmt.Errorf("unclassified protected command option")
		}
		if boolFlags[key] && key == wanted {
			count++
			selected = "true"
			if inline {
				selected = value
			}
		}
		if !boolFlags[key] && !inline && index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			index++
		}
	}
	return selected, count, separator, nil
}

func appendDefaultOption(args []string, key, value string, separator int) []string {
	if separator >= 0 {
		args = append(args, "", "")
		copy(args[separator+2:], args[separator:len(args)-2])
		args[separator], args[separator+1] = key, value
		return args
	}
	return append(args, key, value)
}

// CommandEnvironment keeps every verified E2E subprocess on the executable's
// embedded command catalog, including capability and parser checks.
func CommandEnvironment(extra []string) []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+len(extra)+1)
	for _, e := range src {
		name, _, _ := strings.Cut(e, "=")
		if name == "FORCE_COLOR" || name == "CLICOLOR_FORCE" || strings.EqualFold(name, "ECCTL_SPEC_DIR") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "NO_COLOR=1")
	for _, e := range extra {
		name, _, _ := strings.Cut(e, "=")
		if !strings.EqualFold(name, "ECCTL_SPEC_DIR") {
			out = append(out, e)
		}
	}
	return effectiveCommandEnvironment(out, runtime.GOOS == "windows")
}

// Windows gives the last definition of a variable to the child, regardless
// of its spelling. Keep the environment inspected by replay identical to it.
func effectiveCommandEnvironment(env []string, windows bool) []string {
	if !windows {
		return env
	}
	last := make(map[string]int, len(env))
	for i, item := range env {
		if strings.HasPrefix(item, "=") {
			continue
		}
		name, _, ok := strings.Cut(item, "=")
		if ok {
			last[strings.ToUpper(name)] = i
		}
	}
	out := make([]string, 0, len(last))
	for i, item := range env {
		if strings.HasPrefix(item, "=") {
			out = append(out, item)
			continue
		}
		name, _, ok := strings.Cut(item, "=")
		if !ok || last[strings.ToUpper(name)] == i {
			out = append(out, item)
		}
	}
	return out
}
