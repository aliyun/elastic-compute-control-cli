package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var ErrTeamCreatePending = errors.New("protected Team creation pending")
var ErrTeamRegistrationPending = errors.New("confirmed Team registration pending")

// Injectable at the durable attempt write for post-rename failure tests.
var writeTeamAttemptJournal = writeRunnerJournal

func ownedTeamDeleteCandidate(command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && ((path[0] == "sandbox" || path[0] == "sbx") && path[1] == "team" && (path[2] == "delete" || path[2] == "rm" || path[2] == "remove") || path[0] == "call" && strings.EqualFold(strings.TrimSpace(path[1]), "fcsandbox") && strings.EqualFold(strings.TrimSpace(path[2]), "DeleteTeam"))
	}
	if unclassifiedSandboxFamily(command, "team") {
		return true
	}
	args, splitErr := shlex.Split(command)
	if splitErr != nil {
		args = strings.Fields(command)
	}
	return possibleRawTeamDelete(args)
}

// An unknown flag must not turn a possible raw DeleteTeam into an ordinary
// call. The strict target parser still decides whether it can be invoked.
func possibleRawTeamDelete(args []string) bool {
	if len(args) < 2 {
		return false
	}
	part := 0
	for _, arg := range args[1:] {
		switch part {
		case 0:
			if arg == "call" {
				part++
			}
		case 1:
			if strings.EqualFold(strings.TrimSpace(arg), "fcsandbox") {
				part++
			}
		case 2:
			if strings.EqualFold(strings.TrimSpace(arg), "DeleteTeam") {
				return true
			}
		}
	}
	return false
}

// The generic raw caller intentionally returns business errors in response.
// This owned-delete path must confirm request acceptance independently of the
// subprocess exit code. Acceptance does not establish Team absence.
func acceptedRawTeamDelete(result execpkg.Result, region string) error {
	root, ok := result.JSON.(map[string]any)
	if !ok || root["product"] != "fcsandbox" || root["operation"] != "DeleteTeam" || root["region"] != region {
		return fmt.Errorf("raw Team delete lacks a matching response envelope")
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		return fmt.Errorf("raw Team delete lacks a response object")
	}
	if requestID, present := response["requestId"]; present {
		if _, ok := requestID.(string); !ok {
			return fmt.Errorf("raw Team delete has an invalid response request ID")
		}
	}
	if code, ok := response["code"].(string); !ok || code != "200" {
		return fmt.Errorf("raw Team delete did not confirm business success")
	}
	return nil
}

func ownedTeamClaimKey(command, region string) string {
	id, err := teamDeleteTargetForOwnership(command, region)
	if err != nil {
		return command
	}
	return "ecctl sandbox team delete " + id
}

// Ordinary Team deletes may omit the protected finalizer's timeout or use an
// action alias. Normalize only for target classification; ownership itself
// still requires the original command to pass TeamDeleteTarget.
func teamDeleteTargetForOwnership(command, region string) (string, error) {
	if rawTeamDeleteCandidate(command) {
		return journalfile.RawTeamDeleteTarget(command, region)
	}
	return journalfile.TeamDeleteCandidateTargetForRegion(command, region)
}

// The shared replay fence is held. A first registration write may have failed
// after provider creation, leaving only the pre-create journal and its binding.
// Install that exact confirmed finalizer before the guard selects credentials.
func (c *cleanup) repairTeamBeforeCleanupBinding(role, command string) (bool, error) {
	target, err := teamDeleteTargetForOwnership(command, c.execCfg[role].Region)
	if err != nil {
		return false, nil // Other commands retain their normal validation path.
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.teamCreatesPending > 0 {
		return false, fmt.Errorf("Team delete must wait for protected Team creation: %w", ErrTeamCreatePending)
	}
	var owned *cleanupItem
	for _, item := range c.ownedTeams {
		if item.role != role || !item.teamCreateConfirmed || item.ownedTeamID != target || item.done {
			continue
		}
		if owned != nil {
			return false, fmt.Errorf("owned Team cleanup is ambiguous")
		}
		owned = item
	}
	if owned == nil || !owned.teamRegistrationFailed {
		return false, nil
	}
	if err := c.repairTeamRegistration(owned); err != nil {
		return false, err
	}
	return true, nil
}

func rawTeamDeleteCandidate(command string) bool {
	path, err := commandAction(command)
	if err == nil {
		return len(path) == 3 && path[0] == "call" && strings.EqualFold(strings.TrimSpace(path[1]), "fcsandbox") && strings.EqualFold(strings.TrimSpace(path[2]), "DeleteTeam")
	}
	args, splitErr := shlex.Split(command)
	if splitErr != nil {
		args = strings.Fields(command)
	}
	return possibleRawTeamDelete(args)
}

// Caller holds c.mu. The per-role journal is shared by every case in this
// execution, including cases whose in-memory cleanup scope has already run.
// Only a first ordinary registration may create a journal parent; owned
// deletion and every later lookup require the directory already observed.
func (c *cleanup) retainedTeamParent(role, target string, allowInitialParent bool) (report.Resource, bool, error) {
	if c.journal == "" {
		return report.Resource{}, false, nil
	}
	var owner report.Resource
	found := false
	for _, currentRole := range c.coLocatedTeamRoles(role) {
		if _, err := c.checkedJournalPath(currentRole); err != nil {
			return report.Resource{}, false, err
		}
		template, _, path := c.journalValues(&cleanupItem{role: currentRole})
		lock := journalfile.WithExistingParentLock
		firstForRole := allowInitialParent && c.journalParents[path] == nil && c.nativeJournals[currentRole].NativeJournalPath == ""
		for _, item := range c.registeredItems {
			if item.role == currentRole {
				firstForRole = false
			}
		}
		for _, item := range c.quotaReservations {
			if item.role == currentRole {
				firstForRole = false
			}
		}
		if firstForRole {
			lock = journalfile.WithLock
		}
		var parent os.FileInfo
		err := lock(context.Background(), path, func() error {
			var parentErr error
			parent, parentErr = c.checkedJournalParentLocked(path)
			if parentErr != nil {
				return parentErr
			}
			journal, legacy, err := readRunnerJournal(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
				return fmt.Errorf("Team ownership journal binding changed")
			}
			for _, entry := range journal.Entries {
				if journalfile.IsQuotaCommandCandidate(entry.Teardown) {
					childID, parseErr := journalfile.SandboxQuotaDeleteTarget(entry.Teardown)
					if parseErr != nil || target == "" || childID == target {
						return fmt.Errorf("Team delete must wait for its Quota child")
					}
				}
				if !entry.TeamCreateConfirmed {
					continue
				}
				id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
				if parseErr != nil || entry.RegionRole != currentRole || entry.Region != template.Region || entry.ExecutionID != template.ExecutionID {
					return fmt.Errorf("invalid confirmed Team ownership in journal")
				}
				if target == "" || id == target {
					if found {
						return fmt.Errorf("ambiguous retained Team ownership")
					}
					owner, found = entry, true
				}
			}
			return nil
		})
		if err != nil {
			return report.Resource{}, false, err
		}
		c.rememberJournalParentLocked(path, parent)
	}
	return owner, found, nil
}

// prepareOwnedTeamDelete runs under the shared replay fence. It records the
// attempt before a Team subprocess can invalidate a later Quota reservation.
func (c *cleanup) prepareOwnedTeamDelete(scope []*cleanupItem, command, role string, expected *cleanupItem) (*cleanupItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.teamCreatesPending > 0 {
		return nil, fmt.Errorf("Team delete must wait for protected Team creation: %w", ErrTeamCreatePending)
	}
	target, targetErr := teamDeleteTargetForOwnership(command, c.execCfg[role].Region)
	if targetErr != nil {
		return nil, fmt.Errorf("Team delete requires a validated target and region: %w", targetErr)
	}
	var owned *cleanupItem
	for _, item := range c.ownedTeams {
		if !c.teamTargetsMayOverlap(role, item.role) || !item.teamCreateConfirmed || item.ownedTeamID == "" {
			continue
		}
		if item.ownedTeamID == target {
			if item.role != role {
				return nil, fmt.Errorf("Team delete target belongs to protected Team in a co-located region role")
			}
			if owned != nil || item.done {
				return nil, fmt.Errorf("owned Team delete is ambiguous or already consumed")
			}
			owned = item
		}
	}
	parent, retained, err := c.retainedTeamParent(role, target, false)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		if retained {
			return nil, fmt.Errorf("Team delete target belongs to a retained Team in another scope")
		}
		return nil, nil
	}
	withinScope := false
	for _, item := range scope {
		if item == owned {
			withinScope = true
			break
		}
	}
	if !withinScope {
		return nil, fmt.Errorf("Team delete target belongs to a retained Team in another scope")
	}
	if expected != nil && owned != expected {
		return nil, fmt.Errorf("ordinary Team finalizer cannot consume retained Team ownership")
	}
	if owned.teamRegistrationFailed {
		if err := c.repairTeamRegistration(owned); err != nil {
			return nil, err
		}
		parent, retained, err = c.retainedTeamParent(role, target, false)
		if err != nil {
			return nil, err
		}
	}
	if !rawTeamDeleteCandidate(command) {
		if _, err := journalfile.TeamDeleteTarget(command); err != nil {
			return nil, fmt.Errorf("owned Team delete requires a validated finalizer: %w", err)
		}
	}
	_, entry, _ := c.journalValues(owned)
	if !retained || parent.Teardown != entry.Teardown || parent.Scope != entry.Scope || parent.RegionRole != entry.RegionRole || parent.Region != entry.Region || parent.ExecutionID != entry.ExecutionID || parent.QuotaBound != entry.QuotaBound {
		return nil, fmt.Errorf("Team delete lacks matching durable ownership")
	}
	if err := c.withOwnedTeamJournal(owned, true); err != nil {
		return nil, err
	}
	return owned, nil
}

// Caller holds c.mu. A confirmed create can outlive a failed first journal
// append; reconcile that exact finalizer before allowing its first delete.
func (c *cleanup) repairTeamRegistration(item *cleanupItem) error {
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, expected, _ := c.journalValues(item)
	err = journalfile.WithExistingParentLock(context.Background(), path, func() error {
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.JournalPath != path || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("confirmed Team registration journal binding changed")
		}
		count := 0
		for _, entry := range journal.Entries {
			if journalfile.IsTeamDeleteCandidate(entry.Teardown) && !entry.TeamCreateConfirmed {
				id, parseErr := journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, template.Region)
				if parseErr != nil || id == item.ownedTeamID {
					return fmt.Errorf("confirmed Team registration conflicts with ordinary ownership")
				}
			}
			if !entry.TeamCreateConfirmed {
				continue
			}
			id, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
			if parseErr != nil {
				return fmt.Errorf("invalid confirmed Team ownership in journal: %w", parseErr)
			}
			if id == item.ownedTeamID {
				if entry != expected {
					return fmt.Errorf("confirmed Team registration conflicts with retained ownership")
				}
				count++
			}
		}
		if count > 1 {
			return fmt.Errorf("ambiguous confirmed Team registration")
		}
		if count == 0 {
			journal.Entries = append(journal.Entries, expected)
		}
		if err := writeTeamRegistrationJournal(path, journal); err != nil {
			return errors.Join(ErrTeamRegistrationPending, err)
		}
		installed, _, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if len(installed.Entries) != len(journal.Entries) {
			return fmt.Errorf("confirmed Team registration was not installed")
		}
		found := 0
		for _, entry := range installed.Entries {
			if entry == expected {
				found++
			}
		}
		if found != 1 {
			return fmt.Errorf("confirmed Team registration was not installed exactly once")
		}
		return nil
	})
	if err == nil {
		item.teamRegistrationFailed = false
	}
	return err
}

func (c *cleanup) consumeOwnedTeamDelete(item *cleanupItem) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item.done || !item.teamReplayAttempted {
		return fmt.Errorf("owned Team delete has no active attempt")
	}
	if err := c.withOwnedTeamJournal(item, false); err != nil {
		return err
	}
	item.done = true
	return nil
}

// Caller holds c.mu and the shared cleanup/replay fence.
func (c *cleanup) withOwnedTeamJournal(item *cleanupItem, mark bool) error {
	teamID, teamErr := journalfile.TeamDeleteTarget(item.cmd)
	if c.journal == "" || !item.teamCreateConfirmed || item.ownedTeamID == "" || teamErr != nil || teamID != item.ownedTeamID {
		return fmt.Errorf("Team delete lacks confirmed creation ownership")
	}
	path, err := c.checkedJournalPath(item.role)
	if err != nil {
		return err
	}
	template, expected, _ := c.journalValues(item)
	return journalfile.WithExistingParentLock(context.Background(), path, func() error {
		journal, legacy, err := readRunnerJournal(path)
		if err != nil {
			return err
		}
		if legacy || journal.Version != template.Version || journal.RunID != template.RunID || journal.ExecutionID != template.ExecutionID || journal.RegionRole != template.RegionRole || journal.Region != template.Region || journal.Surface != template.Surface || journal.EcctlBin != template.EcctlBin {
			return fmt.Errorf("owned Team journal binding changed")
		}
		index := -1
		for i, entry := range journal.Entries {
			if journalfile.IsQuotaCommandCandidate(entry.Teardown) {
				childID, parseErr := journalfile.SandboxQuotaDeleteTarget(entry.Teardown)
				if parseErr != nil || childID == item.ownedTeamID {
					return fmt.Errorf("Team delete must wait for its Quota child")
				}
			}
			if entry.Teardown == expected.Teardown {
				if index >= 0 || !entry.TeamCreateConfirmed || entry.Scope != expected.Scope || entry.RegionRole != expected.RegionRole || entry.Region != expected.Region || entry.ExecutionID != expected.ExecutionID || entry.QuotaBound != expected.QuotaBound {
					return fmt.Errorf("ambiguous confirmed Team finalizer")
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("confirmed Team finalizer disappeared")
		}
		if mark {
			journal.Entries[index].TeamReplayAttempted = true
			if err := writeTeamAttemptJournal(path, journal); err != nil {
				return err
			}
			item.teamReplayAttempted = true
			return nil
		}
		if !journal.Entries[index].TeamReplayAttempted {
			return fmt.Errorf("owned Team delete attempt was not recorded")
		}
		journal.Entries = append(journal.Entries[:index], journal.Entries[index+1:]...)
		return writeRunnerJournal(path, journal)
	})
}

// Team has no create idempotency key and no tag sweep. Keep uncertain create
// ownership separate from the strictly delete-only cleanup journal.
type teamCreateIntent struct {
	Version      int                            `json:"version"`
	RunID        string                         `json:"run_id"`
	ExecutionID  string                         `json:"execution_id"`
	Region       string                         `json:"region"`
	Surface      string                         `json:"surface"`
	EcctlBin     string                         `json:"ecctl_bin"`
	Name         string                         `json:"name"`
	Description  string                         `json:"description"`
	Input        []string                       `json:"input"`
	QuotaBinding *report.QuotaCredentialBinding `json:"quota_binding,omitempty"`
	CreatedAt    time.Time                      `json:"created_at"`
	path         string
	journalPath  string
	parent       os.FileInfo
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

func reserveTeamCreate(cmd string, data map[string]any, cfg execpkg.Config, cl *cleanup, st scenario.Step, quotaBinding ...*report.QuotaCredentialBinding) (*teamCreateIntent, error) {
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
			case "--name", "--description", "--plan", "--region", "--lang", "--output", "--json", "--no-color":
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
	if plan, supplied := flags["--plan"]; supplied {
		switch plan {
		case "eco", "std", "pro", "ent":
		default:
			return nil, fmt.Errorf("unsupported Team recovery plan %q", plan)
		}
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
	if _, err := cl.checkedJournalPath("primary"); err != nil {
		return nil, err
	}
	validationData := vars.Clone(data)
	validationData["team_id"] = "intent-validation"
	if _, err := renderTeamRecoveryDelete(st, validationData); err != nil {
		return nil, err
	}
	var binding *report.QuotaCredentialBinding
	if len(quotaBinding) > 0 {
		binding = quotaBinding[0]
	}
	intent := &teamCreateIntent{Version: 1, RunID: meta.RunID, ExecutionID: meta.ExecutionID, Region: cfg.Region, Surface: meta.Surface, EcctlBin: cfg.Bin, Name: name, Description: description, Input: args, QuotaBinding: binding, CreatedAt: time.Now().UTC(), path: cl.journal + ".team-create-" + name[8:] + ".json"}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, err
	}
	desired, _, journalPath := cl.journalValues(&cleanupItem{role: "primary"})
	desired.JournalPath = journalPath
	intent.journalPath = journalPath
	known, err := cl.rememberedJournalParent(journalPath)
	if err != nil {
		return nil, err
	}
	lock := journalfile.WithLock
	if known != nil {
		lock = journalfile.WithExistingParentLock
	}
	if err := lock(context.Background(), journalPath, func() error {
		if err := checkJournalParentIdentity(journalPath, known); err != nil {
			return err
		}
		parent, err := os.Stat(filepath.Dir(journalPath))
		if err != nil {
			return err
		}
		intent.parent = parent
		journal, legacy, err := readRunnerJournal(journalPath)
		if err == nil {
			if legacy || journal.Version != 2 {
				return fmt.Errorf("Team create requires a version 2 cleanup journal")
			}
			if journal.RunID != desired.RunID || journal.ExecutionID != desired.ExecutionID || journal.RegionRole != desired.RegionRole || journal.Region != desired.Region || journal.Surface != desired.Surface || journal.EcctlBin != desired.EcctlBin {
				return fmt.Errorf("existing Team cleanup journal does not match run, execution, role, region, surface and binary")
			}
			if journal.JournalPath == "" {
				journal.JournalPath = journalPath
				if err := writeTeamCreateJournal(journalPath, journal); err != nil {
					return fmt.Errorf("anchor Team cleanup journal: %w", err)
				}
			}
			if binding != nil {
				if journal.QuotaBinding != nil && !reflect.DeepEqual(journal.QuotaBinding, binding) {
					return fmt.Errorf("Team cleanup journal Quota credential binding changed")
				}
				journal.QuotaBinding = binding
				if err := writeTeamCreateJournal(journalPath, journal); err != nil {
					return fmt.Errorf("bind Team cleanup credential identity: %w", err)
				}
			}
		} else if os.IsNotExist(err) {
			// Bind the journal before another run can reserve or either create
			// can launch. A failed claim must not degrade to in-memory cleanup.
			desired.QuotaBinding = binding
			if err := writeTeamCreateJournal(journalPath, desired); err != nil {
				return fmt.Errorf("claim Team cleanup journal: %w", err)
			}
		} else {
			return err
		}
		return writeTeamCreateIntent(intent.path, append(body, '\n'))
	}); err != nil {
		if binding != nil {
			// Journal replacement can succeed even when its durability barrier
			// reports an error. Read the installed state before choosing the
			// in-memory cleanup binding, and fence all later allocations.
			cl.mu.Lock()
			cl.allocationFence = "Team create recovery reservation failed"
			cl.mu.Unlock()
			_ = lock(context.Background(), journalPath, func() error {
				if err := checkJournalParentIdentity(journalPath, known); err != nil {
					return err
				}
				current, legacy, readErr := readRunnerJournal(journalPath)
				if readErr != nil || legacy || !reflect.DeepEqual(current.QuotaBinding, binding) {
					return fmt.Errorf("Team cleanup binding cannot be reconciled")
				}
				cl.mu.Lock()
				cl.quotaBinding = binding
				cl.mu.Unlock()
				return nil
			})
		}
		return nil, fmt.Errorf("reserve Team create intent: %w", err)
	}
	if binding != nil {
		cl.mu.Lock()
		cl.quotaBinding = binding
		cl.mu.Unlock()
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
	target, err := journalfile.TeamDeleteTarget(command)
	if err != nil || target != id || !isReplayableTeardown(command) {
		return "", fmt.Errorf("recovered Team requires a validated Team delete teardown")
	}
	return command, nil
}

// A read can discover ownership, but cannot establish that a failed create did
// not commit. Never resend CreateTeam, and keep its original failure visible.
func reconcileTeamCreate(cfg execpkg.Config, cl *cleanup, scope *[]*cleanupItem, data map[string]any, st scenario.Step, locks []string, timeout time.Duration, intent *teamCreateIntent, returnedID string) error {
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
	var matchedID string
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
		if matchedID != "" {
			return fmt.Errorf("ambiguous owned Team reconciliation; retained recovery intent %s", intent.path)
		}
		matchedID = id
	}
	if matchedID == "" {
		return fmt.Errorf("no owned Team found; absence is unproven; retained recovery intent %s", intent.path)
	}
	if returnedID != "" && matchedID != returnedID {
		return fmt.Errorf("returned Team identity differs from intent-bound reconciliation; retained recovery intent %s", intent.path)
	}
	// Render the declared delete with only the strictly matched identity.
	recoveryData := make(map[string]any, len(data)+1)
	for k, v := range data {
		recoveryData[k] = v
	}
	recoveryData["team_id"] = matchedID
	command, err := renderTeamRecoveryDelete(st, recoveryData)
	if err != nil {
		return err
	}
	if _, err := cl.pushItem(scope, caseScope(data), command, st.TeardownRegion, locks, false, true, intent.QuotaBinding != nil); err != nil {
		return fmt.Errorf("journal recovered Team: %w; retained recovery intent %s", err, intent.path)
	}
	if intent.QuotaBinding != nil {
		cl.mu.Lock()
		cl.unresolvedQuotaTeamCreate = false
		cl.mu.Unlock()
	}
	return fmt.Errorf("journaled owned Team for cleanup; CreateTeam remains failed; recovery intent %s", intent.path)
}
