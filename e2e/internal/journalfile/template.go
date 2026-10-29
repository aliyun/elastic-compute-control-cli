package journalfile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
)

// ErrTemplateCreatePending fences cleanup until creation ownership is resolved.
var ErrTemplateCreatePending = errors.New("pending Template create intent")

// TemplateCreateIntent records ownership before a non-idempotent native create.
// Pending intents fence destructive journal replay. Journaled intents retain
// the first result's digest and the exact validated cleanup evidence; they are
// never removed merely because a list returned no match.
type TemplateCreateIntent struct {
	Version          int                           `json:"version"`
	RunID            string                        `json:"run_id"`
	ExecutionID      string                        `json:"execution_id"`
	RegionRole       string                        `json:"region_role"`
	Region           string                        `json:"region"`
	Surface          string                        `json:"surface"`
	EcctlBin         string                        `json:"ecctl_bin"`
	TeamID           string                        `json:"team_id"`
	Name             string                        `json:"name"`
	Profile          string                        `json:"profile"`
	Selectors        map[string]string             `json:"selectors"`
	ConfigPaths      map[string]TemplateConfigPath `json:"config_paths"`
	Identity         execpkg.IdentityPin           `json:"identity"`
	CreatedAt        time.Time                     `json:"created_at"`
	State            string                        `json:"state"`
	Exit             *int                          `json:"exit,omitempty"`
	Started          *bool                         `json:"started,omitempty"`
	StdoutSHA256     string                        `json:"stdout_sha256,omitempty"`
	StderrSHA256     string                        `json:"stderr_sha256,omitempty"`
	NoAllocationCode string                        `json:"no_allocation_code,omitempty"`
	Deletes          []string                      `json:"deletes,omitempty"`
}

// TemplateSelectorNames includes only nonsecret configuration selectors. The
// selected profile is pinned in argv; these values prevent replay under other
// config files/home directories. Credential values never enter the intent.
var TemplateSelectorNames = []string{"HOME", "USERPROFILE", "ECCTL_CONFIG_PATH", "ECCTL_ALIYUN_CONFIG_PATH", "ALIBABA_CLOUD_CONFIG_PATH", "ALIBABACLOUD_CONFIG_PATH", "ALICLOUD_CONFIG_PATH", "ALIBABA_CLOUD_IGNORE_PROFILE", "ALIBABACLOUD_IGNORE_PROFILE"}

func TemplateEnvironmentValue(name string, extra []string) string {
	value := os.Getenv(name)
	for _, setting := range extra {
		key, candidate, ok := strings.Cut(setting, "=")
		if ok && key == name {
			value = candidate
		}
	}
	return value
}

func safeTemplateID(id string) bool {
	return id != "" && id[0] != '-' && id[0] != '_' && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}

// ValidateDelete additionally binds the delete-only journal command to this
// creation's exact Team, region and selected profile.
func (intent TemplateCreateIntent) ValidateDelete(command string) error {
	if intent.Profile == "" {
		return fmt.Errorf("Template cleanup requires a pinned configured profile")
	}
	if err := journalcmd.ValidateDelete(command); err != nil {
		return err
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 6 || args[0] != "ecctl" || args[1] != "sandbox" || args[2] != "template" || args[3] != "delete" || !safeTemplateID(args[4]) {
		return fmt.Errorf("Template recovery requires one exact native Template delete")
	}
	seen := map[string]string{}
	for index := 5; index < len(args); index++ {
		key, value, inline := strings.Cut(args[index], "=")
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Template recovery flag %s", key)
		}
		switch key {
		case "--openapi":
			if !inline {
				value = "true"
			}
		case "--region", "--team-id", "--profile":
			if !inline {
				index++
				if index >= len(args) {
					return fmt.Errorf("missing Template recovery flag value")
				}
				value = args[index]
			}
		default:
			return fmt.Errorf("unsupported Template recovery flag %s", key)
		}
		seen[key] = value
	}
	if seen["--openapi"] != "true" || seen["--region"] != intent.Region || seen["--team-id"] != intent.TeamID || seen["--profile"] != intent.Profile {
		return fmt.Errorf("Template recovery delete changed native mode, region, Team or profile")
	}
	return nil
}

// CheckTemplateCreateIntents must run under the journal's replay lock. A corrupt
// or unresolved sidecar is a fence, never an instruction to execute its data.
func CheckTemplateCreateIntents(path string, meta report.CleanupJournal, env []string) error {
	_, err := readTemplateCreateIntents(path, meta, env)
	return err
}

// Reservation and cleanup share one principal invariant. Check while holding
// the exclusive replay lock, before reserving or launching another allocation.
func CheckTemplateCreateIdentity(path string, meta report.CleanupJournal, env []string, prospective execpkg.IdentityPin) error {
	intents, err := readTemplateCreateIntents(path, meta, env)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.State != "not_started" && intent.State != "not_allocated" && intent.Identity != prospective {
			return fmt.Errorf("native Template journal contains a different credential principal")
		}
	}
	return nil
}

// CheckTemplateDeleteOwnership requires the exact target from a retained
// creation, not just the same caller identity. The caller holds the journal's
// cleanup/replay lock. In-memory bindings can predate the journaled state, so
// the completed result and delete commands must come from the durable intent.
func CheckTemplateDeleteOwnership(path string, meta report.CleanupJournal, env []string, binding TemplateCreateIntent, command string) error {
	intents, err := readTemplateCreateIntents(path, meta, env)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.State != "journaled" || intent.Name != binding.Name || intent.Version != binding.Version || intent.RunID != binding.RunID || intent.ExecutionID != binding.ExecutionID {
			continue
		}
		if intent.RegionRole != binding.RegionRole || intent.Region != binding.Region || intent.Surface != binding.Surface || intent.EcctlBin != binding.EcctlBin || intent.TeamID != binding.TeamID || intent.Profile != binding.Profile || intent.Identity != binding.Identity {
			continue
		}
		for _, owned := range intent.Deletes {
			if sameTemplateDelete(owned, command) && intent.ValidateDelete(command) == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("native Template finalizer lacks exact retained creation ownership")
}

// This client code is exclusive to the provider's local referenced-schema
// validation before its API executor. Cloud errors and generic client errors
// cannot establish that a non-idempotent create allocated nothing.
func TemplatePreMutationFailureCode(code string) bool { return code == "InvalidFCTemplateConfig" }

func validTemplateResultDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func readTemplateCreateIntents(path string, meta report.CleanupJournal, env []string) ([]TemplateCreateIntent, error) {
	return readTemplateCreateIntentsMode(path, meta, env, false)
}

func readTemplateCreateIntentsForRecovery(path string, meta report.CleanupJournal, env []string) ([]TemplateCreateIntent, error) {
	return readTemplateCreateIntentsMode(path, meta, env, true)
}

func readTemplateCreateIntentsMode(path string, meta report.CleanupJournal, env []string, allowPendingCache bool) ([]TemplateCreateIntent, error) {
	var intents []TemplateCreateIntent
	resolved, resolveErr := ResolveJournalPath(path)
	if resolveErr != nil {
		return nil, resolveErr
	}
	path = resolved
	actual, loaded, ledgerErr := loadTemplateLedger(path, meta)
	if ledgerErr != nil {
		return nil, ledgerErr
	}
	meta = actual
	seen := map[string]bool{}
	entries, err := os.ReadDir(filepath.Dir(path))
	if os.IsNotExist(err) && meta.NativeJournalPath == "" {
		if loaded {
			return nil, fmt.Errorf("cleanup journal parent disappeared after ledger read")
		}
		// A new, unanchored journal may not have a directory yet.
		return nil, validateTemplateLedger(path, meta, nil, nil, allowPendingCache)
	}
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(path) + ".template-create-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		intentPath := filepath.Join(filepath.Dir(path), entry.Name())
		seen[intentPath] = true
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("invalid Template create intent %s", intentPath)
		}
		body, err := os.ReadFile(intentPath)
		if err != nil {
			return nil, err
		}
		var intent TemplateCreateIntent
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&intent); err != nil {
			return nil, fmt.Errorf("invalid Template create intent %s: %w", intentPath, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("invalid trailing Template intent data %s", intentPath)
		}
		if intent.Version != 2 || len(intent.Name) != 32 || !strings.HasPrefix(intent.Name, "ecctl-p-") || entry.Name() != prefix+strings.TrimPrefix(intent.Name, "ecctl-p-")+".json" {
			return nil, fmt.Errorf("invalid Template create ownership %s", intentPath)
		}
		if _, err := hex.DecodeString(intent.Name[8:]); err != nil {
			return nil, fmt.Errorf("invalid Template owner %s", intentPath)
		}
		if intent.RunID == "" || intent.ExecutionID == "" || !safeTemplateID(intent.TeamID) || intent.CreatedAt.IsZero() || meta.Version != 2 || intent.RunID != meta.RunID || intent.ExecutionID != meta.ExecutionID || intent.RegionRole != meta.RegionRole || intent.Region != meta.Region || intent.Surface != meta.Surface || intent.EcctlBin != meta.EcctlBin {
			return nil, fmt.Errorf("Template create intent binding differs from journal %s", intentPath)
		}
		for _, name := range TemplateSelectorNames {
			value, ok := intent.Selectors[name]
			if !ok || value != TemplateEnvironmentValue(name, env) {
				return nil, fmt.Errorf("Template create intent config selector differs: %s (%s)", name, intentPath)
			}
		}
		if err := intent.Identity.Validate(); err != nil {
			return nil, err
		}
		if err := intent.ValidateConfig(env); err != nil {
			return nil, err
		}
		if intent.State == "not_started" && intent.Started != nil && !*intent.Started && intent.Exit != nil {
			intents = append(intents, intent)
			continue
		}
		if intent.State == "not_allocated" && intent.Started != nil && *intent.Started && intent.Exit != nil && *intent.Exit == 1 && TemplatePreMutationFailureCode(intent.NoAllocationCode) && len(intent.Deletes) == 0 && validTemplateResultDigest(intent.StdoutSHA256) && validTemplateResultDigest(intent.StderrSHA256) {
			intents = append(intents, intent)
			continue
		}
		if intent.State != "journaled" || len(intent.Deletes) == 0 || intent.Exit == nil {
			return nil, fmt.Errorf("%w %s; reconcile ownership before cleanup", ErrTemplateCreatePending, intentPath)
		}
		for _, command := range intent.Deletes {
			if err := intent.ValidateDelete(command); err != nil {
				return nil, fmt.Errorf("invalid Template create cleanup evidence %s: %w", intentPath, err)
			}
		}
		intents = append(intents, intent)
	}
	if err := validateTemplateLedger(path, meta, intents, seen, allowPendingCache); err != nil {
		return nil, err
	}
	return intents, nil
}
