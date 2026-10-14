package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Team has no create idempotency key and no tag sweep. Keep uncertain create
// ownership separate from the strictly delete-only cleanup journal.
type teamCreateIntent struct {
	Version     int       `json:"version"`
	RunID       string    `json:"run_id"`
	ExecutionID string    `json:"execution_id"`
	Region      string    `json:"region"`
	Surface     string    `json:"surface"`
	EcctlBin    string    `json:"ecctl_bin"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Input       []string  `json:"input"`
	CreatedAt   time.Time `json:"created_at"`
	path        string
}

var (
	writeTeamCreateIntent  = journalfile.WriteExclusiveDurable
	writeTeamCreateJournal = writeRunnerJournal
)

func teamCommandArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (intent *teamCreateIntent) command() string {
	// Pin the verified region and machine output before user values: early
	// raw-token checks must see the actual flags, independent of profile defaults.
	args := append([]string{intent.Input[0], "--output=json", "--agent-envelope=false", "--region", intent.Region}, intent.Input[1:]...)
	for i, value := range args {
		args[i] = teamCommandArgument(value)
	}
	return strings.Join(args, " ")
}

func teamOwnership(data map[string]any) error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	owner := hex.EncodeToString(nonce[:])
	data["team_name"] = "ecctl-t-" + owner // 32 characters, independent of run labels.
	data["team_owner"] = "ecctl E2E Team owner=" + owner
	return nil
}

func isTeamCreate(command string) bool {
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 || args[0] != "ecctl" {
		return false
	}
	// Find the command before validating flag values, just as the public CLI
	// does. Even invalid Team inputs must enter reservation, not generic retry.
	root := &cobra.Command{Use: "ecctl"}
	root.PersistentFlags().AddFlagSet(teamCreateFlagSet())
	product := &cobra.Command{Use: "sandbox", Aliases: []string{"sbx"}}
	resource := &cobra.Command{Use: "team"}
	create := &cobra.Command{Use: "create"}
	resource.AddCommand(create)
	product.AddCommand(resource)
	root.AddCommand(product)
	// A separator cannot make an unsupported Team layout bypass reservation.
	// Classify conservatively here; reservation rejects the original separator.
	commandArgs := make([]string, 0, len(args)-1)
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			continue
		}
		commandArgs = append(commandArgs, arg)
		name, _, inline := strings.Cut(arg, "=")
		flag := root.PersistentFlags().Lookup(strings.TrimPrefix(name, "--"))
		if strings.HasPrefix(arg, "--") && !inline && flag != nil && flag.NoOptDefVal == "" && i+1 < len(args) {
			// A string flag consumes its next token even when that value is --.
			i++
			commandArgs = append(commandArgs, args[i])
		}
	}
	found, _, err := root.Find(commandArgs)
	return err == nil && found == create
}

func teamCreateFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("Team create", pflag.ContinueOnError)
	// Match the public root and Team create flag types. pflag handles prefix,
	// interspersed and inline values using the same grammar as Cobra.
	for _, name := range []string{"region", "profile", "lang", "output", "name", "description", "resource-group", "plan"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"json", "agent-envelope", "no-color"} {
		flags.Bool(name, false, "")
	}
	flags.BoolP("help", "h", false, "")
	flags.BoolP("version", "v", false, "")
	return flags
}

func reserveTeamCreate(cmd string, data map[string]any, cfg execpkg.Config, cl *cleanup, st scenario.Step) (*teamCreateIntent, error) {
	args, err := shlex.Split(cmd)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 || args[0] != "ecctl" {
		return nil, fmt.Errorf("Team recovery command must start with ecctl")
	}
	parsed := teamCreateFlagSet()
	if err := parsed.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("invalid Team recovery input: %w", err)
	}
	positionals := parsed.Args()
	if len(positionals) != 3 || (positionals[0] != "sandbox" && positionals[0] != "sbx") || positionals[1] != "team" || positionals[2] != "create" {
		return nil, fmt.Errorf("Team recovery requires exactly the Team create command path")
	}
	name, _ := data["team_name"].(string)
	description, _ := data["team_owner"].(string)
	if len(name) != 32 || !strings.HasPrefix(name, "ecctl-t-") || description != "ecctl E2E Team owner="+strings.TrimPrefix(name, "ecctl-t-") {
		return nil, fmt.Errorf("Team create requires execution-owned name and description")
	}
	if _, err := hex.DecodeString(name[8:]); err != nil {
		return nil, fmt.Errorf("invalid Team owner: %w", err)
	}
	flags := map[string]string{}
	for i := 1; i < len(args); i++ {
		if args[i] == "--" || strings.HasPrefix(args[i], "-") && !strings.HasPrefix(args[i], "--") {
			return nil, fmt.Errorf("unsupported Team recovery input %s", args[i])
		}
		if strings.HasPrefix(args[i], "--") {
			key, value, inline := strings.Cut(args[i], "=")
			switch key {
			case "--name", "--description", "--region", "--lang", "--output", "--json", "--no-color":
			default:
				return nil, fmt.Errorf("unsupported Team recovery input %s", key)
			}
			if !inline && parsed.Lookup(strings.TrimPrefix(key, "--")).NoOptDefVal == "" {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("missing %s value", key)
				}
				value = args[i]
			}
			if _, exists := flags[key]; exists {
				return nil, fmt.Errorf("duplicate Team create flag %s", key)
			}
			flags[key] = value
		}
	}
	if flags["--output"] != "" && flags["--output"] != "json" {
		return nil, fmt.Errorf("Team recovery requires JSON output")
	}
	region, regionSupplied := flags["--region"]
	if flags["--name"] != name || flags["--description"] != description || regionSupplied && region != cfg.Region || st.TeardownRegion != "" && st.TeardownRegion != "primary" {
		return nil, fmt.Errorf("Team create command does not match recovery ownership/region")
	}
	meta := cl.journalMeta
	if cl.journal == "" || meta.RunID == "" || meta.ExecutionID == "" || cfg.Region == "" || cfg.Bin == "" || !scenario.Surface(meta.Surface).Valid() {
		return nil, fmt.Errorf("Team create requires a cleanup journal bound to run, execution, region, surface and binary")
	}
	cleanupCfg, ok := cl.execCfg["primary"]
	if !ok || cleanupCfg.Region != cfg.Region || cleanupCfg.Bin != cfg.Bin {
		return nil, fmt.Errorf("Team create and cleanup targets differ")
	}
	validationData := vars.Clone(data)
	validationData["team_id"] = "intent-validation"
	if _, err := renderTeamRecoveryDelete(st, validationData); err != nil {
		return nil, err
	}
	intent := &teamCreateIntent{Version: 1, RunID: meta.RunID, ExecutionID: meta.ExecutionID, Region: cfg.Region, Surface: meta.Surface, EcctlBin: cfg.Bin, Name: name, Description: description, Input: args, CreatedAt: time.Now().UTC(), path: cl.journal + ".team-create-" + name[8:] + ".json"}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	desired, _, journalPath := cl.journalValues(&cleanupItem{role: "primary"})
	if err := journalfile.WithLock(context.Background(), journalPath, func() error {
		journal, legacy, err := readRunnerJournal(journalPath)
		if err == nil {
			if legacy || journal.Version != 2 {
				return fmt.Errorf("Team create requires a version 2 cleanup journal")
			}
			if journal.RunID != desired.RunID || journal.ExecutionID != desired.ExecutionID || journal.RegionRole != desired.RegionRole || journal.Region != desired.Region || journal.Surface != desired.Surface || journal.EcctlBin != desired.EcctlBin {
				return fmt.Errorf("existing Team cleanup journal does not match run, execution, role, region, surface and binary")
			}
		} else if os.IsNotExist(err) {
			// Bind the journal before another run can reserve or either create
			// can launch. A failed claim must not degrade to in-memory cleanup.
			if err := writeTeamCreateJournal(journalPath, desired); err != nil {
				return fmt.Errorf("claim Team cleanup journal: %w", err)
			}
		} else {
			return err
		}
		return writeTeamCreateIntent(intent.path, append(body, '\n'))
	}); err != nil {
		return nil, fmt.Errorf("reserve Team create intent: %w", err)
	}
	return intent, nil
}

func teamCreateIdentity(result execpkg.Result) (string, bool) {
	root, _ := result.JSON.(map[string]any)
	team, _ := root["team"].(map[string]any)
	id, _ := team["id"].(string)
	return id, safeTeamIdentity(id)
}

func safeTeamIdentity(id string) bool {
	if id == "" || id[0] == '-' || id[0] == '_' {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}

func renderTeamRecoveryDelete(st scenario.Step, data map[string]any) (string, error) {
	id, ok := data["team_id"].(string)
	if !ok || !safeTeamIdentity(id) {
		return "", fmt.Errorf("Team delete requires a safe identity")
	}
	command, err := vars.Render(st.Teardown, data)
	if err != nil {
		return "", err
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" || (args[1] != "sandbox" && args[1] != "sbx") || args[2] != "team" || args[3] != "delete" || args[4] != data["team_id"] || !isReplayableTeardown(command) {
		return "", fmt.Errorf("recovered Team requires a validated Team delete teardown")
	}
	if len(args) != 7 || args[5] != "--timeout" {
		return "", fmt.Errorf("recovered Team delete accepts only its ID and timeout")
	}
	if timeout, err := time.ParseDuration(args[6]); err != nil || timeout <= 0 {
		return "", fmt.Errorf("invalid recovered Team delete timeout")
	}
	return command, nil
}

// A read can discover ownership, but cannot establish that a failed create did
// not commit. Never resend CreateTeam, and keep its original failure visible.
func reconcileTeamCreate(cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, locks []string, timeout time.Duration, intent *teamCreateIntent) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := execpkg.Run(ctx, cfg, "ecctl --output=json --agent-envelope=false --region "+teamCommandArgument(intent.Region)+" sandbox team list --filter name="+intent.Name+" --all")
	if result.Err != nil || result.Exit != 0 {
		return fmt.Errorf("Team reconciliation failed; retained recovery intent %s", intent.path)
	}
	root, ok := result.JSON.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid Team reconciliation; retained recovery intent %s", intent.path)
	}
	teams, ok := root["teams"].([]any)
	pagination, _ := root["pagination"].(map[string]any)
	hasMore, complete := pagination["has_more"].(bool)
	if !ok || !complete || hasMore {
		return fmt.Errorf("incomplete Team reconciliation; retained recovery intent %s", intent.path)
	}
	matched := 0
	for _, value := range teams {
		team, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid Team reconciliation item; retained recovery intent %s", intent.path)
		}
		if team["name"] != intent.Name || team["description"] != intent.Description {
			continue
		}
		id, ok := team["id"].(string)
		if !ok || !safeTeamIdentity(id) {
			return fmt.Errorf("unsafe recovered Team identity; retained recovery intent %s", intent.path)
		}
		// Render the declared delete with only the strictly matched identity.
		recoveryData := make(map[string]any, len(data)+1)
		for k, v := range data {
			recoveryData[k] = v
		}
		recoveryData["team_id"] = id
		command, err := renderTeamRecoveryDelete(st, recoveryData)
		if err != nil {
			return err
		}
		if err := cl.push(scope, caseScope(data), command, st.TeardownRegion, locks); err != nil {
			return fmt.Errorf("journal recovered Team: %w; retained recovery intent %s", err, intent.path)
		}
		matched++
	}
	if matched == 0 {
		return fmt.Errorf("no owned Team found; absence is unproven; retained recovery intent %s", intent.path)
	}
	return fmt.Errorf("journaled %d owned Team(s) for cleanup; CreateTeam remains failed; recovery intent %s", matched, intent.path)
}
