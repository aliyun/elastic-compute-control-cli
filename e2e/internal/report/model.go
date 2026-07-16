// Package report holds the result model produced by a run and renders it to
// JSON, HTML, JUnit XML and a GitHub step-summary (GFM markdown).
package report

import "time"

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
	Version     int        `json:"version"`
	RunID       string     `json:"run_id"`
	ExecutionID string     `json:"execution_id,omitempty"`
	RegionRole  string     `json:"region_role,omitempty"`
	Region      string     `json:"region"`
	Surface     string     `json:"surface"`
	EcctlBin    string     `json:"ecctl_bin"`
	Entries     []Resource `json:"entries"`
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
	Name       string  `json:"name"`
	Command    string  `json:"command"` // rendered, copy-pasteable
	WantExit   int     `json:"want_exit"`
	Exit       int     `json:"exit"`
	DurationMs int64   `json:"duration_ms"`
	Status     string  `json:"status"`
	Stdout     string  `json:"stdout,omitempty"` // full ecctl output
	Stderr     string  `json:"stderr,omitempty"`
	Checks     []Check `json:"checks,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Check is one matcher/assert outcome.
type Check struct {
	Path   string `json:"path"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Resource is a created resource recorded for the manifest.
type Resource struct {
	Scope       string `json:"scope"` // "stack" or the case name
	Teardown    string `json:"teardown"`
	RegionRole  string `json:"region_role,omitempty"`
	Region      string `json:"region,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
}

// Failed reports whether any case failed or errored.
func (r *Run) Failed() bool {
	return r.Summary.Failed > 0
}

// WallMs is the total wall-clock duration of the run in milliseconds.
func (r *Run) WallMs() int64 {
	return r.FinishedAt.Sub(r.StartedAt).Milliseconds()
}
