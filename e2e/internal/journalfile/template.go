package journalfile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
)

// ErrTemplateCreatePending fences cleanup until creation ownership is resolved.
var ErrTemplateCreatePending = errors.New("pending Template create intent")

// TemplateCreateIntent records ownership before a non-idempotent native create.
// An empty Resource is the original native Template format; "volume" uses the
// same durable journal fence and principal binding.
// Pending intents fence destructive journal replay. Journaled intents retain
// the first result's digest and the exact validated cleanup evidence; they are
// never removed merely because a list returned no match.
type TemplateCreateIntent struct {
	Version              int                           `json:"version"`
	Resource             string                        `json:"resource,omitempty"`
	RunID                string                        `json:"run_id"`
	ExecutionID          string                        `json:"execution_id"`
	RegionRole           string                        `json:"region_role"`
	Region               string                        `json:"region"`
	Surface              string                        `json:"surface"`
	EcctlBin             string                        `json:"ecctl_bin"`
	TeamID               string                        `json:"team_id"`
	Name                 string                        `json:"name"`
	Profile              string                        `json:"profile"`
	Selectors            map[string]string             `json:"selectors"`
	ConfigPaths          map[string]TemplateConfigPath `json:"config_paths"`
	Identity             execpkg.IdentityPin           `json:"identity"`
	CreatedAt            time.Time                     `json:"created_at"`
	State                string                        `json:"state"`
	Exit                 *int                          `json:"exit,omitempty"`
	Started              *bool                         `json:"started,omitempty"`
	StdoutSHA256         string                        `json:"stdout_sha256,omitempty"`
	StderrSHA256         string                        `json:"stderr_sha256,omitempty"`
	NoAllocationCode     string                        `json:"no_allocation_code,omitempty"`
	CreateRequestID      string                        `json:"create_request_id,omitempty"`
	AcceptedVolumeID     string                        `json:"accepted_volume_id,omitempty"`
	CaseScope            string                        `json:"case_scope,omitempty"`
	VolumeObserved       bool                          `json:"volume_observed,omitempty"`
	NoAllocationVerified bool                          `json:"no_allocation_verified,omitempty"`
	Deletes              []string                      `json:"deletes,omitempty"`
	// A restart read may register a Volume whose create result was lost. The
	// sidecar stays fenced in "settling" until its finalizer is durable.
	Recovered       bool   `json:"recovered,omitempty"`
	ReconcileSHA256 string `json:"reconcile_sha256,omitempty"`
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
	if intent.Resource == "volume" {
		return intent.validateVolumeDelete(command)
	}
	if intent.Resource != "" {
		return fmt.Errorf("unknown native creation resource")
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

// A Volume can move to another Team after creation, so its recovery delete is
// bound to the exact returned ID rather than the original Team selector.
func (intent TemplateCreateIntent) validateVolumeDelete(command string) error {
	args, err := shlex.Split(command)
	if err != nil || len(args) != 9 || args[0] != "ecctl" || args[1] != "sandbox" || args[2] != "volume" || args[3] != "delete" || !safeTemplateID(args[4]) {
		return fmt.Errorf("Volume recovery requires one exact Volume delete")
	}
	if args[5] != "--region" || args[6] != intent.Region || args[7] != "--profile" || args[8] != intent.Profile {
		return fmt.Errorf("Volume recovery delete changed region or profile")
	}
	return nil
}

// VolumeDeleteBinding recovers the exact retained creation source for a
// journaled Volume finalizer. Replay must not infer its Team from a delete
// command, because that command predates later transfers.
func VolumeDeleteBinding(path string, meta report.CleanupJournal, env []string, command string) (TemplateCreateIntent, error) {
	resolved, err := ResolveJournalPath(path)
	if err != nil {
		return TemplateCreateIntent{}, err
	}
	intents, err := readTemplateCreateIntents(path, meta, env)
	if err != nil {
		return TemplateCreateIntent{}, err
	}
	var found *TemplateCreateIntent
	for i := range intents {
		intent := &intents[i]
		if intent.Resource != "volume" || intent.State != "journaled" || intent.ValidateDelete(command) != nil {
			continue
		}
		for _, owned := range intent.Deletes {
			if !sameTemplateDelete(owned, command) {
				continue
			}
			if found != nil {
				return TemplateCreateIntent{}, fmt.Errorf("Volume delete has ambiguous creation bindings")
			}
			found = intent
		}
	}
	if found == nil {
		return TemplateCreateIntent{}, fmt.Errorf("Volume delete lacks a retained creation binding")
	}
	if !oneNativeIntentReference(meta, resolved, found.Name) {
		return TemplateCreateIntent{}, fmt.Errorf("Volume delete lacks one exact journaled owner reference")
	}
	return *found, nil
}

func oneNativeIntentReference(meta report.CleanupJournal, path, name string) bool {
	want := path + ".template-create-" + strings.TrimPrefix(name, "ecctl-p-") + ".json"
	count := 0
	for _, reference := range meta.NativeIntentPaths {
		if reference == want {
			count++
		}
	}
	return count == 1
}

// CheckTemplateCreateIntents must run under the journal's replay lock. A corrupt
// or unresolved sidecar is a fence, never an instruction to execute its data.
func CheckTemplateCreateIntents(path string, meta report.CleanupJournal, env []string, roots ...*os.Root) error {
	_, err := readTemplateCreateIntentsMode(path, meta, env, false, nil, roots...)
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

// CheckVolumeReceiptTransition admits only the exact pending sidecar held by
// the ordinary Volume launcher. Generic reservation and replay retain their
// unresolved-intent fence. The caller holds the replay and parent locks and
// has already checked the installed sidecar and parent identities.
func CheckVolumeReceiptTransition(path string, meta report.CleanupJournal, env []string, prospective execpkg.IdentityPin, sidecar string, body []byte, finalizer string) error {
	if sidecar == "" || len(body) == 0 || meta.NativeJournalPath != path || !filepath.IsAbs(sidecar) {
		return fmt.Errorf("Volume receipt lacks its anchored pending reservation")
	}
	intents, err := readTemplateCreateIntentsMode(path, meta, env, false, &volumeReceiptReservation{path: sidecar, body: body, finalizer: finalizer})
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

type volumeReceiptReservation struct {
	path      string
	body      []byte
	finalizer string
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
		if intent.State != "journaled" || intent.Name != binding.Name || intent.Resource != binding.Resource || intent.Version != binding.Version || intent.RunID != binding.RunID || intent.ExecutionID != binding.ExecutionID {
			continue
		}
		if intent.RegionRole != binding.RegionRole || intent.Region != binding.Region || intent.Surface != binding.Surface || intent.EcctlBin != binding.EcctlBin || intent.TeamID != binding.TeamID || intent.Profile != binding.Profile || intent.Identity != binding.Identity || !reflect.DeepEqual(intent.ConfigPaths, binding.ConfigPaths) || !reflect.DeepEqual(intent.Selectors, binding.Selectors) {
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
	return readTemplateCreateIntentsMode(path, meta, env, false, nil)
}

func readTemplateCreateIntentsForRecovery(path string, meta report.CleanupJournal, env []string, roots ...*os.Root) ([]TemplateCreateIntent, error) {
	return readTemplateCreateIntentsMode(path, meta, env, true, nil, roots...)
}

func readTemplateCreateIntentsMode(path string, meta report.CleanupJournal, env []string, allowPendingCache bool, receipt *volumeReceiptReservation, roots ...*os.Root) ([]TemplateCreateIntent, error) {
	var intents []TemplateCreateIntent
	var root *os.Root
	if len(roots) != 0 {
		root = roots[0]
	}
	if root == nil {
		resolved, resolveErr := ResolveJournalPath(path)
		if resolveErr != nil {
			return nil, resolveErr
		}
		path = resolved
	} else if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("rooted Template ownership journal path is not absolute")
	}
	readLedger := os.ReadFile
	if root != nil {
		readLedger = func(string) ([]byte, error) {
			if meta.NativeJournalPath != "" {
				snapshot, err := ReadPrivateRecoverySnapshotRoot(root, filepath.Base(path))
				return snapshot.Body, err
			}
			return root.ReadFile(filepath.Base(path))
		}
	} else if meta.NativeJournalPath != "" {
		readLedger = func(target string) ([]byte, error) {
			snapshot, err := ReadPrivateRecoverySnapshot(target)
			return snapshot.Body, err
		}
	}
	actual, loaded, ledgerErr := loadTemplateLedgerWithRead(path, meta, readLedger)
	if ledgerErr != nil {
		return nil, ledgerErr
	}
	meta = actual
	seen := map[string]bool{}
	var entries []os.DirEntry
	var err error
	if root != nil {
		directory, openErr := root.Open(".")
		if openErr != nil {
			return nil, openErr
		}
		entries, err = directory.ReadDir(-1)
		err = errors.Join(err, directory.Close())
	} else {
		entries, err = os.ReadDir(filepath.Dir(path))
	}
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
		var file *os.File
		if root != nil {
			file, err = root.Open(entry.Name())
		} else {
			file, err = os.Open(intentPath)
		}
		if err != nil {
			return nil, err
		}
		var body []byte
		var private bool
		var readErr error
		if root != nil {
			body, private, readErr = ReadRecoveryFileRoot(root, file, entry.Name())
		} else {
			body, private, readErr = ReadRecoveryFile(file, intentPath)
		}
		err = errors.Join(readErr, file.Close())
		if err != nil {
			return nil, err
		}
		if !private {
			return nil, fmt.Errorf("Template create intent is not a private regular file %s", intentPath)
		}
		var intent TemplateCreateIntent
		if _, err := execpkg.DecodeStrictJSON(body); err != nil {
			return nil, fmt.Errorf("ambiguous Template create intent %s: %w", intentPath, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&intent); err != nil {
			return nil, fmt.Errorf("invalid Template create intent %s: %w", intentPath, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("invalid trailing Template intent data %s", intentPath)
		}
		if intent.Version != 2 || (intent.Resource != "" && intent.Resource != "volume") || len(intent.Name) != 32 || !strings.HasPrefix(intent.Name, "ecctl-p-") || entry.Name() != prefix+strings.TrimPrefix(intent.Name, "ecctl-p-")+".json" {
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
		if intent.Resource != "volume" && (intent.CreateRequestID != "" || intent.AcceptedVolumeID != "" || intent.NoAllocationVerified || intent.CaseScope != "" || intent.VolumeObserved) {
			return nil, fmt.Errorf("unexpected Volume creation evidence %s", intentPath)
		}
		if intent.Resource == "volume" && (intent.CaseScope == "" || strings.ContainsAny(intent.CaseScope, "\r\n") || intent.VolumeObserved && intent.AcceptedVolumeID == "") {
			return nil, fmt.Errorf("invalid Volume case scope or observation %s", intentPath)
		}
		if receipt != nil && intentPath == receipt.path {
			if !bytes.Equal(body, receipt.body) || intent.Resource != "volume" || intent.State != "pending" || intent.CaseScope == "" || intent.NoAllocationCode != "" || intent.NoAllocationVerified || intent.Recovered || intent.ReconcileSHA256 != "" || len(intent.Deletes) != 0 {
				return nil, fmt.Errorf("Volume receipt reservation changed before settlement")
			}
			if intent.AcceptedVolumeID != "" && (!safeTemplateID(intent.AcceptedVolumeID) || intent.Started == nil || !*intent.Started || intent.Exit == nil || !validTemplateResultDigest(intent.StdoutSHA256) || !validTemplateResultDigest(intent.StderrSHA256)) {
				return nil, fmt.Errorf("Volume receipt lacks invocation-linked accepted ID")
			}
			matches := 0
			for _, final := range meta.Entries {
				if !IsNativeVolumeDelete(final.Teardown) {
					continue
				}
				request, err := ParseVolumeDeleteRequest(final.Teardown)
				if err != nil || request.ID != intent.AcceptedVolumeID || intent.AcceptedVolumeID == "" {
					continue
				}
				if receipt.finalizer == "" || !sameTemplateDelete(final.Teardown, receipt.finalizer) || final.Scope != intent.CaseScope || final.RegionRole != intent.RegionRole || final.Region != intent.Region || final.ExecutionID != intent.ExecutionID {
					return nil, fmt.Errorf("Volume receipt finalizer differs from reserved ownership")
				}
				matches++
			}
			if receipt.finalizer == "" && matches != 0 || receipt.finalizer != "" && (matches != 1 || intent.ValidateDelete(receipt.finalizer) != nil) {
				return nil, fmt.Errorf("Volume receipt requires one exact accepted-ID finalizer")
			}
			if receipt.finalizer != "" {
				intent.State = "journaled"
				intent.Deletes = []string{receipt.finalizer}
			}
			intents = append(intents, intent)
			continue
		}
		if intent.State == "not_started" && intent.Started != nil && !*intent.Started && intent.Exit != nil && intent.CreateRequestID == "" && intent.AcceptedVolumeID == "" && !intent.NoAllocationVerified {
			intents = append(intents, intent)
			continue
		}
		if intent.State == "not_allocated" && intent.Started != nil && *intent.Started && intent.Exit != nil && len(intent.Deletes) == 0 && !intent.Recovered && intent.ReconcileSHA256 == "" && intent.AcceptedVolumeID == "" && validTemplateResultDigest(intent.StdoutSHA256) && validTemplateResultDigest(intent.StderrSHA256) && (intent.Resource == "" && *intent.Exit == 1 && TemplatePreMutationFailureCode(intent.NoAllocationCode) && intent.CreateRequestID == "" && !intent.NoAllocationVerified || intent.Resource == "volume" && *intent.Exit == 2 && intent.NoAllocationCode == "VolumeConflict" && intent.CreateRequestID != "" && intent.NoAllocationVerified) {
			intents = append(intents, intent)
			continue
		}
		if intent.State != "journaled" || len(intent.Deletes) == 0 || intent.Resource == "volume" && len(intent.Deletes) != 1 || intent.Exit == nil && !(intent.Resource == "volume" && intent.Recovered && validTemplateResultDigest(intent.ReconcileSHA256)) {
			return nil, fmt.Errorf("%w %s; reconcile ownership before cleanup", ErrTemplateCreatePending, intentPath)
		}
		if intent.Recovered && (intent.Resource != "volume" || !validTemplateResultDigest(intent.ReconcileSHA256)) {
			return nil, fmt.Errorf("invalid native recovery proof %s", intentPath)
		}
		if intent.Resource == "volume" {
			if !safeTemplateID(intent.AcceptedVolumeID) || intent.NoAllocationCode != "" || intent.NoAllocationVerified || intent.Started == nil || !*intent.Started || intent.Exit == nil || !validTemplateResultDigest(intent.StdoutSHA256) || !validTemplateResultDigest(intent.StderrSHA256) {
				return nil, fmt.Errorf("Volume create response lacks invocation-bound ownership %s", intentPath)
			}
			for _, command := range intent.Deletes {
				request, err := ParseVolumeDeleteRequest(command)
				if err != nil || request.ID != intent.AcceptedVolumeID {
					return nil, fmt.Errorf("Volume finalizer differs from accepted create ID %s", intentPath)
				}
			}
		}
		for _, command := range intent.Deletes {
			if err := intent.ValidateDelete(command); err != nil {
				return nil, fmt.Errorf("invalid Template create cleanup evidence %s: %w", intentPath, err)
			}
		}
		intents = append(intents, intent)
	}
	if receipt != nil && !seen[receipt.path] {
		return nil, fmt.Errorf("Volume receipt reservation is not linked to journal")
	}
	if err := validateTemplateLedger(path, meta, intents, seen, allowPendingCache); err != nil {
		return nil, err
	}
	return intents, nil
}
