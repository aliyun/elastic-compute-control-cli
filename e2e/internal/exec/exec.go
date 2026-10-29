// Package exec runs a rendered ecctl command line as a subprocess and parses
// its JSON stdout. It mirrors the badcases harness: it strips dev-shell color
// forcers and sets NO_COLOR so JSON output is clean regardless of host setup.
package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/google/shlex"
)

// Config controls how commands are executed.
type Config struct {
	Bin              string // substituted for a leading "ecctl" token (default "ecctl")
	Region           string // appended as --region to ecctl commands when set
	Profile          string // pinned profile for protected Quota lifecycle commands
	Env              []string
	ExpectedIdentity *IdentityPin
}

// Result captures everything one invocation produced, for assertions/reports.
type Result struct {
	Command  string        `json:"command"` // rendered, bin-substituted, for repro
	Exit     int           `json:"exit"`
	Stdout   string        `json:"-"`
	Stderr   string        `json:"stderr,omitempty"`
	JSON     any           `json:"-"` // parsed Stdout, nil if not JSON
	Duration time.Duration `json:"-"`
	Err      error         `json:"-"` // process failed to start / context cancelled
	Started  bool          `json:"started"`
}

// Run executes one rendered command line under ctx (which carries the timeout).
func Run(ctx context.Context, cfg Config, rendered string) Result {
	res := Result{Command: rendered}
	tokens, err := shlex.Split(rendered)
	if err != nil || len(tokens) == 0 {
		res.Err = err
		res.Exit = -1
		return res
	}
	bin := cfg.Bin
	if bin == "" {
		bin = "ecctl"
	}
	if tokens[0] == "ecctl" {
		tokens[0] = bin
	}
	args := tokens[1:]
	if cfg.Region != "" {
		_, count, separator, regionErr := scanStringOption(args, "--region", false)
		if regionErr != nil {
			res.Err = fmt.Errorf("region selector is invalid: %w", regionErr)
			res.Exit = -1
			return res
		}
		if count == 0 {
			args = appendDefaultOption(args, "--region", cfg.Region, separator)
		}
	}
	if cfg.Profile != "" {
		selected, count, profileErr := actualProfileOption(args)
		if profileErr != nil || count > 1 || count == 1 && selected != cfg.Profile {
			res.Err = fmt.Errorf("protected profile selector changed")
			res.Exit = -1
			return res
		}
		if count == 0 {
			args = append(args, "--profile", cfg.Profile)
		}
	}
	res.Command = recordedCommand(tokens[0], args)

	cmd := osexec.CommandContext(ctx, tokens[0], args...)
	identityEnv, identityErr := identityEnvironment(cfg.ExpectedIdentity)
	if identityErr != nil {
		res.Err = identityErr
		res.Exit = -1
		return res
	}
	cmd.Env = append(sanitizedEnv(cfg.Env), identityEnv...)
	var out, errb strings.Builder
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
		var parsed any
		dec := json.NewDecoder(strings.NewReader(v))
		dec.UseNumber()
		if dec.Decode(&parsed) == nil {
			res.JSON = parsed
		}
	}
	return res
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
	for _, name := range []string{"region", "profile", "lang", "output", "name", "team-id", "runtime-config", "build-config", "timeout", "cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "steps", "start-cmd", "ready-cmd", "tag", "step", "build-id", "cursor", "limit", "next-token", "filter", "alias", "disk-size-mb", "fields", "description", "resource-group", "plan", "request", "api-param", "aliyun-arg", "status", "policy-name", "policy-type", "fallback-default-version", "cluster", "instance-name", "cpu-cores", "memory-gb", "page"} {
		stringFlags["--"+name] = true
	}
	stringFlags["-o"] = true
	boolFlags := map[string]bool{}
	for _, name := range []string{"force", "openapi", "no-wait", "json", "agent-envelope", "no-color", "help", "version", "public", "dry-run", "all"} {
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

// sanitizedEnv reproduces the badcases hermetic env: drop FORCE_COLOR /
// CLICOLOR_FORCE, force NO_COLOR=1, then append caller env (e.g. STS creds).
func sanitizedEnv(extra []string) []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+len(extra)+1)
	for _, e := range src {
		if strings.HasPrefix(e, "FORCE_COLOR=") || strings.HasPrefix(e, "CLICOLOR_FORCE=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "NO_COLOR=1")
	out = append(out, extra...)
	return out
}
