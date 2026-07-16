// Package exec runs a rendered ecctl command line as a subprocess and parses
// its JSON stdout. It mirrors the badcases harness: it strips dev-shell color
// forcers and sets NO_COLOR so JSON output is clean regardless of host setup.
package exec

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/google/shlex"
)

// Config controls how commands are executed.
type Config struct {
	Bin    string // substituted for a leading "ecctl" token (default "ecctl")
	Region string // appended as --region to ecctl commands when set
	Env    []string
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
	if cfg.Region != "" && !hasFlag(args, "--region") {
		args = append(args, "--region", cfg.Region)
	}
	res.Command = strings.Join(append([]string{tokens[0]}, args...), " ")

	cmd := osexec.CommandContext(ctx, tokens[0], args...)
	cmd.Env = sanitizedEnv(cfg.Env)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb

	start := time.Now()
	runErr := cmd.Run()
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

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
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
