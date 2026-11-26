// Package report holds the result model produced by a run and renders it to
// JSON, HTML, JUnit XML and a GitHub step-summary (GFM markdown).
package report

import (
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
)

const CurrentSchemaVersion = 2

// Status values for cases and steps.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

// Run is the top-level result of one `ecctl-e2e run`.
type Run struct {
	SchemaVersion  int             `json:"schema_version"`
	RunID          string          `json:"run_id"`
	Region         string          `json:"region"`
	RegionAttempts []RegionAttempt `json:"region_attempts,omitempty"`
	Executions     []Execution     `json:"executions,omitempty"`
	Surface        string          `json:"surface,omitempty"`
	EcctlBin       string          `json:"ecctl_bin,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     time.Time       `json:"finished_at"`
	Summary        Summary         `json:"summary"`
	Parameters     map[string]any  `json:"parameters,omitempty"`
	Cases          []Case          `json:"cases"`
	Manifest       []Resource      `json:"manifest"` // created resources (teardown commands)
}

// Execution is one independently runnable group of cases with the same
// prerequisite and region-role signature.
type Execution struct {
	ID         string             `json:"id"`
	Signature  string             `json:"signature"`
	Regions    map[string]string  `json:"regions"`
	Attempts   []ExecutionAttempt `json:"attempts,omitempty"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt time.Time          `json:"finished_at"`
	Summary    Summary            `json:"summary"`
	Parameters map[string]any     `json:"parameters,omitempty"`
	Cases      []Case             `json:"cases"`
	Manifest   []Resource         `json:"manifest"`
	Error      string             `json:"error,omitempty"`
}

// ExecutionAttempt records one complete named-role assignment considered for
// an execution unit.
type ExecutionAttempt struct {
	Regions map[string]string `json:"regions"`
	Status  string            `json:"status"`
	Reason  string            `json:"reason,omitempty"`
}

// RegionAttempt records one ordered candidate tried by the runner. A later
// candidate is used only when the previous attempt was classified as
// region-unavailable and cleanup completed.
type RegionAttempt struct {
	Region string `json:"region"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// CleanupJournal is the crash-recovery artifact for one run. Metadata is
// persisted with the teardown commands so replay cannot silently target a
// different region, command surface, or binary.
type CleanupJournal struct {
	Version              int                     `json:"version"`
	JournalPath          string                  `json:"journal_path,omitempty"`
	RunID                string                  `json:"run_id"`
	ExecutionID          string                  `json:"execution_id,omitempty"`
	RegionRole           string                  `json:"region_role,omitempty"`
	Region               string                  `json:"region"`
	Surface              string                  `json:"surface"`
	EcctlBin             string                  `json:"ecctl_bin"`
	EcctlBinSHA256       string                  `json:"ecctl_bin_sha256,omitempty"`
	QuotaBinding         *QuotaCredentialBinding `json:"quota_binding,omitempty"`
	VolumeFixtureBinding *QuotaCredentialBinding `json:"volume_fixture_binding,omitempty"`
	VolumeFixtureBucket  string                  `json:"volume_fixture_bucket,omitempty"`
	// VolumeTeamCreators pins the original credential of each Volume case Team.
	VolumeTeamCreators map[string]*QuotaCredentialBinding `json:"volume_team_creators,omitempty"`
	// An empty ID means the named Team create may have allocated without a
	// confirmed finalizer. A verified ID records its durable registration.
	VolumeTeamCreateIDs map[string]string `json:"volume_team_create_ids,omitempty"`
	Entries             []Resource        `json:"entries"`
	NativeJournalPath   string            `json:"native_journal_path,omitempty"`
	NativeIntentPaths   []string          `json:"native_intent_paths,omitempty"`
	// VolumeTeamBindings maps each destination Team to its first retained
	// Volume create intent, which pins the Team's cleanup credentials.
	VolumeTeamBindings map[string]string `json:"volume_team_bindings,omitempty"`
	// VolumeScopeBindings records every possible destination before a Volume
	// transfer reaches the provider. Keys are the reserved Volume names.
	VolumeScopeBindings map[string][]string `json:"volume_scope_bindings,omitempty"`
	// VolumeTransferHistory cross-checks each Volume's possible Teams and
	// retains unresolved transfers when several Volumes share a destination.
	// The nested bool is true until that transfer is observed at its Team.
	VolumeTransferHistory map[string]map[string]bool `json:"volume_transfer_history,omitempty"`
	// A terminal receipt replaces one exact Volume finalizer in the same
	// durable journal write. Its absence leaves the retained sidecar fenced.
	VolumeTerminalReceipts map[string]VolumeTerminalReceipt `json:"volume_terminal_receipts,omitempty"`
	// A local retirement receipt binds an unlaunched reservation removal to
	// the exact prior journal bytes for a restart after a failed barrier.
	VolumeRetirementReceipts map[string]VolumeRetirementReceipt `json:"volume_retirement_receipts,omitempty"`
	// A read-only reconciliation of an already journaled Volume pins the
	// resulting sidecar bytes in a journal transition before exposing them.
	VolumeObservationReceipts map[string]string `json:"volume_observation_receipts,omitempty"`
	// OriginalJournalSHA256 pins the first independently retained journal
	// through every replay and reconciliation successor.
	OriginalJournalSHA256        string                                 `json:"original_journal_sha256,omitempty"`
	HistoricalCompletionReceipts map[string]HistoricalCompletionReceipt `json:"historical_completion_receipts,omitempty"`
}

// HistoricalCompletionReceipt records a replay decision against one entry in
// the independently retained original journal.
type HistoricalCompletionReceipt struct {
	Version                  int    `json:"version"`
	RunID                    string `json:"run_id"`
	ExecutionID              string `json:"execution_id"`
	JournalPath              string `json:"journal_path"`
	EcctlBinSHA256           string `json:"ecctl_bin_sha256"`
	OriginalJournalSHA256    string `json:"original_journal_sha256"`
	OriginalIndex            int    `json:"original_index"`
	EntrySHA256              string `json:"entry_sha256"`
	PredecessorJournalSHA256 string `json:"predecessor_journal_sha256,omitempty"`
	PredecessorEntrySHA256   string `json:"predecessor_entry_sha256,omitempty"`
	EvidenceSHA256           string `json:"evidence_sha256"`
	Evidence                 string `json:"evidence,omitempty"`
	Readbacks                string `json:"readbacks,omitempty"`
	Proof                    string `json:"proof"`
}

type VolumeRetirementReceipt struct {
	OriginalSHA256 string    `json:"original_sha256"`
	TeamName       string    `json:"team_name,omitempty"`
	FixtureEntry   *Resource `json:"fixture_entry,omitempty"`
	FixtureIndex   int       `json:"fixture_index,omitempty"`
}

type VolumeTerminalReceipt struct {
	VolumeID    string              `json:"volume_id"`
	TeamID      string              `json:"team_id"`
	Teardown    string              `json:"teardown"`
	RunID       string              `json:"run_id"`
	ExecutionID string              `json:"execution_id"`
	RegionRole  string              `json:"region_role"`
	Region      string              `json:"region"`
	Surface     string              `json:"surface"`
	EcctlBin    string              `json:"ecctl_bin"`
	Profile     string              `json:"profile"`
	Identity    execpkg.IdentityPin `json:"identity"`
}

// QuotaCredentialBinding records public selectors and a trusted STS principal
// for the owned Team and its Quota child. Credential material is never stored.
type QuotaCredentialBinding struct {
	Profile     string                           `json:"profile"`
	Mode        string                           `json:"mode"`
	Selectors   map[string]string                `json:"selectors"`
	ConfigPaths map[string]CredentialConfigPath  `json:"config_paths"`
	ConfigState map[string]CredentialConfigState `json:"config_state"`
	// SourceProfiles records the Aliyun profiles recursively used by a
	// ChainableRamRoleArn credential, excluding the selected profile above.
	SourceProfiles map[string]CredentialConfigState `json:"source_profiles,omitempty"`
	// EffectiveSTS contains only nonsecret endpoint, region and VPC selectors
	// that can affect the selected renewable credential or its source chain.
	EffectiveSTS map[string]string   `json:"effective_sts,omitempty"`
	ResolvedMode bool                `json:"resolved_mode,omitempty"`
	Identity     execpkg.IdentityPin `json:"identity"`
}

type CredentialConfigPath struct {
	Requested string `json:"requested"`
	Target    string `json:"target"`
}

// CredentialConfigState describes the selected profile without storing secret
// values or a digest of credentials. Changes to another profile or `current`
// do not alter the selected profile's binding.
type CredentialConfigState struct {
	Exists          bool              `json:"exists"`
	Mode            uint32            `json:"mode,omitempty"`
	AuthSelectors   map[string]string `json:"auth_selectors,omitempty"`
	SensitiveFields map[string]bool   `json:"sensitive_fields,omitempty"`
}

// Summary is the at-a-glance tally.
type Summary struct {
	Cases   int `json:"cases"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Case is one suite's result.
type Case struct {
	Name        string            `json:"name"`
	Resource    string            `json:"resource"`
	Path        string            `json:"path"`
	ExecutionID string            `json:"execution_id,omitempty"`
	Regions     map[string]string `json:"regions,omitempty"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	DurationMs  int64             `json:"duration_ms"`
	Steps       []Step            `json:"steps"`
}

// Step is one command's result.
type Step struct {
	Name                  string   `json:"name"`
	DependsOn             []string `json:"depends_on,omitempty"`
	DirectNeeds           []string `json:"direct_needs,omitempty"`
	RequiresPrerequisites []string `json:"requires_prerequisites,omitempty"`
	Locks                 []string `json:"locks,omitempty"`
	Command               string   `json:"command"` // rendered, copy-pasteable
	WantExit              int      `json:"want_exit"`
	Exit                  int      `json:"exit"`
	DurationMs            int64    `json:"duration_ms"`
	Status                string   `json:"status"`
	Stdout                string   `json:"stdout,omitempty"` // full ecctl output
	Stderr                string   `json:"stderr,omitempty"`
	Checks                []Check  `json:"checks,omitempty"`
	Error                 string   `json:"error,omitempty"`
}

// Check is one matcher/assert outcome.
type Check struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Resource is a created resource recorded for the manifest.
type Resource struct {
	Scope                         string `json:"scope"` // "stack" or the case name
	Teardown                      string `json:"teardown"`
	RegionRole                    string `json:"region_role,omitempty"`
	Region                        string `json:"region,omitempty"`
	ExecutionID                   string `json:"execution_id,omitempty"`
	CacheCreatePending            bool   `json:"cache_create_pending,omitempty"`
	TeamCreateConfirmed           bool   `json:"team_create_confirmed,omitempty"`
	TeamProviderTemplateCleanup   bool   `json:"team_provider_template_cleanup,omitempty"`
	VolumeTeamBound               bool   `json:"volume_team_bound,omitempty"`
	QuotaBound                    bool   `json:"quota_bound,omitempty"`
	QuotaReserved                 bool   `json:"quota_reserved,omitempty"`
	QuotaUpdatePending            bool   `json:"quota_update_pending,omitempty"`
	TeamReplayAttempted           bool   `json:"team_replay_attempted,omitempty"`
	VolumeTransferIntent          string `json:"volume_transfer_intent,omitempty"`
	VolumeTransferPending         bool   `json:"volume_transfer_pending,omitempty"`
	VolumeDeleteAttempted         bool   `json:"volume_delete_attempted,omitempty"`
	VolumeFixtureKind             string `json:"volume_fixture_kind,omitempty"`
	VolumeFixturePending          bool   `json:"volume_fixture_pending,omitempty"`
	VolumeFixtureDispatchProtocol int    `json:"volume_fixture_dispatch_protocol,omitempty"`
	VolumeFixtureDispatched       bool   `json:"volume_fixture_dispatched,omitempty"`
	// Captured from the exact successful create invocation before later
	// lookups or capture processing can fail. A receipt alone does not prove
	// that a same-name fixture remains the created provider instance.
	VolumeFixtureCreateRequestID      string `json:"volume_fixture_create_request_id,omitempty"`
	VolumeFixtureCreateResponseSHA256 string `json:"volume_fixture_create_response_sha256,omitempty"`
	VolumeFixtureCreateValidated      bool   `json:"volume_fixture_create_validated,omitempty"`
	// The RoleId returned by this run's CreateRole is required before a first
	// name-based role delete. Older entries without it remain read-only.
	VolumeFixtureRoleID             string `json:"volume_fixture_role_id,omitempty"`
	VolumeFixturePolicyCreateDate   string `json:"volume_fixture_policy_create_date,omitempty"`
	VolumeFixtureBucketCreationDate string `json:"volume_fixture_bucket_creation_date,omitempty"`
	// Protocol 1 marks a newly reserved one-shot fixture finalizer. Older
	// entries have an ambiguous first-mutation history and use read-only recovery.
	VolumeFixtureDeleteProtocol int `json:"volume_fixture_delete_protocol,omitempty"`
	// After a role, bucket, policy, or attachment mutation could have reached the
	// provider, later cleanup may only confirm absence without mutating a replacement.
	VolumeFixtureDeleteAttempted bool `json:"volume_fixture_delete_attempted,omitempty"`
}

// Failed reports whether any case failed or errored.
func (r *Run) Failed() bool {
	return r.Summary.Failed > 0
}

// WallMs is the total wall-clock duration of the run in milliseconds.
func (r *Run) WallMs() int64 {
	return r.FinishedAt.Sub(r.StartedAt).Milliseconds()
}
