// Package sweeper deletes leftover tagged resources — the third cleanup layer.
// It is config-driven: each sweepable kind declares a list command
// (filtered by the e2e tag) and a delete command template. Orphans are selected
// by TTL or by "owning GitHub run already finished".
package sweeper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/jsonq"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

// defaultConcurrency bounds parallel deletes within a single kind when the
// caller does not set Options.Concurrency.
const defaultConcurrency = 20

// Transient delete-failure retry: a freshly emptied network resource can report
// DependencyViolation until its ENIs finish releasing (seconds to ~a minute).
const (
	sweepDeleteRetries         = 4
	sweepRetryInterval         = 15 * time.Second
	replayJournalCommitTimeout = 30 * time.Second
)

// sweepRetrySleep is a package var so tests can stub out the backoff.
var sweepRetrySleep = time.Sleep

// replayJournalAfterRead is a test synchronization point for deterministic
// writer/replayer interleavings. Production keeps the no-op default.
var replayJournalAfterRead = func(string) {}

// replayJournalAfterResolve is a test synchronization point before lock admission.
var replayJournalAfterResolve = func(string) {}

// replayJournalAfterSuccess is a test synchronization point for the narrow
// window between a successful external command and its journal commit.
var replayJournalAfterSuccess = func(string) {}

// Injectable at the attempt barrier for post-rename failure tests.
var writeTeamReplayAttemptJournal = journalfile.WriteDurable
var writeVolumeFixtureReplayAttemptJournal = journalfile.WriteDurable
var writeVolumeFixtureReplayRestoreJournal = journalfile.WriteDurable

// Injectable at the consumption barrier for post-rename failure tests.
var writeReplayConsumptionJournal = journalfile.WriteDurable

var errHistoricalVolumeTransfer = errors.New("historical Volume transfer requires separate reconciliation")

// isTransientSweepFailure reports whether a failed delete is worth retrying:
// dependency-still-releasing or throttling. Both clear on their own shortly.
func isTransientSweepFailure(dr execpkg.Result) bool {
	reason := strings.ToLower(failureReason(dr))
	return strings.Contains(reason, "dependency") || strings.Contains(reason, "throttl")
}

// Config is sweep.yaml.
type Config struct {
	Kinds        []Kind             `yaml:"kinds"`
	NonSweepable []NonSweepableKind `yaml:"non_sweepable"`
}

// Kind is one sweepable resource type.
type Kind struct {
	Name         string            `yaml:"name"`
	Resource     string            `yaml:"resource"`      // optional checker metadata: product/resource
	List         string            `yaml:"list"`          // full ecctl list command (tag-filtered)
	ItemsPath    string            `yaml:"items_path"`    // jsonpath to the array of items
	IDField      string            `yaml:"id_field"`      // relative path within an item
	RunIDField   string            `yaml:"runid_field"`   // optional
	CreatedField string            `yaml:"created_field"` // optional; RFC3339 or Aliyun minute-precision
	DeleteFields map[string]string `yaml:"delete_fields"` // template field -> relative item path
	DependsOn    []string          `yaml:"depends_on"`    // providers that must be deleted after this kind
	Delete       string            `yaml:"delete"`        // template with {{.id}} and explicit delete_fields
}

// NonSweepableKind documents a live-created resource that has an explicit,
// reviewable reason why `ecctl-e2e sweep` cannot reclaim it automatically.
type NonSweepableKind struct {
	Resource    string `yaml:"resource"`
	Reason      string `yaml:"reason"`
	ReviewAfter string `yaml:"review_after"`
}

// Options configures a sweep.
type Options struct {
	ConfigFile    string
	Region        string
	EcctlBin      string
	TTL           time.Duration
	ByFinishedRun bool
	CheckFinished RunChecker // required when ByFinishedRun
	DryRun        bool
	Concurrency   int // parallel deletes within a kind; <1 means defaultConcurrency
	Env           []string
	Logf          func(string, ...any)
}

// RunChecker reports whether a GitHub run id has finished.
type RunChecker func(runID string) (bool, error)

// Result tallies a sweep.
type Result struct {
	Deleted int      `json:"deleted"`
	Skipped int      `json:"skipped"`
	Errors  int      `json:"errors"`
	Details []string `json:"details"`
}

// ReplayOptions identifies the run that may be replayed. Empty values use the
// metadata recorded in the journal; conflicting values are rejected.
type ReplayOptions struct {
	Config  execpkg.Config
	RunID   string
	Surface string
	// Package tests may model an exclusive fake-provider scope.
	fixtureMutationScope func(report.CleanupJournal, report.Resource, execpkg.Config) bool
}

func (o ReplayOptions) fixtureMutationAllowed(journal report.CleanupJournal, entry report.Resource, cfg execpkg.Config) bool {
	if journal.VolumeFixtureBinding != nil &&
		cfg.Bin == journal.EcctlBin && cfg.Region == journal.Region &&
		cfg.Profile == journal.VolumeFixtureBinding.Profile && cfg.ExpectedIdentity != nil &&
		*cfg.ExpectedIdentity == journal.VolumeFixtureBinding.Identity {
		if journalfile.VolumeFixtureMutationAllowed("delete", journal, entry, cfg) {
			return true
		}
		return o.fixtureMutationScope != nil && o.fixtureMutationScope(journal, entry, cfg)
	}
	return false
}

// ReplayJournal replays registered teardown commands in reverse creation order.
// It is the crash-recovery counterpart to the tag-based sweeper: journals can
// reclaim resources even when their APIs do not support tag-filtered listing.
func ReplayJournal(ctx context.Context, path string, cfg execpkg.Config) (*Result, error) {
	return ReplayJournalWithOptions(ctx, path, ReplayOptions{Config: cfg})
}

// ReplayJournalWithOptions replays a journal after validating its run identity.
// Raw []Resource journals from older runners remain readable, but new journals
// always carry metadata and are never replayed against conflicting options.
func ReplayJournalWithOptions(ctx context.Context, path string, opt ReplayOptions) (result *Result, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, resolveErr := journalfile.ResolveJournalPath(path)
	if resolveErr != nil {
		return nil, resolveErr
	}
	path = resolved
	parent, err := replayParentIdentity(path)
	if err != nil {
		return nil, err
	}
	releaseRun, err := journalfile.AcquireExistingRunLease(path)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		return nil, err
	}
	defer func() { err = errors.Join(err, releaseRun()) }()
	replayJournalAfterResolve(path)
	err = withAnchoredReplayLock(ctx, path, parent, func() error {
		var journal report.CleanupJournal
		err := withAnchoredJournalLock(ctx, path, parent, func() error {
			var err error
			journal, _, err = readReplayJournal(path)
			if err != nil {
				return err
			}
			if err := validateJournalIdentity(journal, &opt); err != nil {
				return err
			}
			return journalfile.CheckTemplateCreateIntents(path, journal, opt.Config.Env)
		})
		if err != nil {
			return err
		}
		replayJournalAfterRead(path)
		result, err = replayJournalEntries(ctx, path, opt, journal, parent)
		return err
	})
	return result, err
}

func replayParentIdentity(path string) (os.FileInfo, error) {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return nil, fmt.Errorf("existing cleanup journal parent %s is unavailable: %w", parent, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("existing cleanup journal parent %s is not a directory", parent)
	}
	return info, nil
}

func checkReplayParent(path string, original os.FileInfo) error {
	current, err := replayParentIdentity(path)
	if err != nil {
		return err
	}
	if !os.SameFile(original, current) {
		return fmt.Errorf("cleanup journal parent %s changed during replay", filepath.Dir(path))
	}
	return nil
}

func withAnchoredJournalLock(ctx context.Context, path string, parent os.FileInfo, update func() error) error {
	if err := checkReplayParent(path, parent); err != nil {
		return err
	}
	return journalfile.WithExistingParentLock(ctx, path, func() error {
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		return errors.Join(update(), checkReplayParent(path, parent))
	})
}

func withAnchoredReplayLock(ctx context.Context, path string, parent os.FileInfo, update func() error) error {
	if err := checkReplayParent(path, parent); err != nil {
		return err
	}
	return journalfile.WithExistingReplayLock(ctx, path, func() error {
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		return errors.Join(update(), checkReplayParent(path, parent))
	})
}

func readReplayJournal(path string) (report.CleanupJournal, bool, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return report.CleanupJournal{}, false, err
	}
	defer root.Close()
	name := filepath.Base(path)
	file, err := root.Open(name)
	if err != nil {
		return report.CleanupJournal{}, false, err
	}
	data, private, readErr := journalfile.ReadRecoveryFileRoot(root, file, name)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return report.CleanupJournal{}, false, err
	}
	var journal report.CleanupJournal
	legacy := strings.HasPrefix(strings.TrimSpace(string(data)), "[")
	if legacy {
		if err := json.Unmarshal(data, &journal.Entries); err != nil {
			return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
		}
	} else if err := json.Unmarshal(data, &journal); err != nil {
		return report.CleanupJournal{}, false, fmt.Errorf("cleanup journal %s: %w", path, err)
	}
	if !legacy && journalfile.ProtectedRecoveryJournal(journal) && !private {
		return report.CleanupJournal{}, false, fmt.Errorf("protected cleanup journal is not a private regular file")
	}
	if err := journalfile.CheckJournalPath(path, journal); err != nil {
		return report.CleanupJournal{}, false, err
	}
	return journal, legacy, nil
}

func replayJournalEntries(ctx context.Context, path string, opt ReplayOptions, journal report.CleanupJournal, parent os.FileInfo) (*Result, error) {
	entries := journal.Entries
	originalEntries := append([]report.Resource(nil), entries...)
	result := &Result{}
	// Process Volume children before Teams and retire the bucket before its RAM dependencies.
	order := make([]int, 0, len(entries))
	for pass := 0; pass < 4; pass++ {
		for i := len(entries) - 1; i >= 0; i-- {
			kind := entries[i].VolumeFixtureKind
			group := 1
			switch {
			case journalfile.IsNativeVolumeDelete(entries[i].Teardown):
				group = 0
			case kind == "bucket":
				group = 2
			case kind == "attachment" || kind == "policy" || kind == "role":
				group = 3
			}
			if group == pass {
				order = append(order, i)
			}
		}
	}
	consumed := make(map[int]bool)
	volumeBlocked := false
	retainHistoricalVolume := func(entry report.Resource) {
		result.Errors++
		result.Details = append(result.Details, fmt.Sprintf("journal %s: %v", entry.Scope, errHistoricalVolumeTransfer))
		volumeBlocked = true
	}
	refresh := func(originalIndex, index int) error {
		consumed[originalIndex] = true
		current, _, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		expected := append([]report.Resource(nil), entries[:index]...)
		expected = append(expected, entries[index+1:]...)
		if validateJournalIdentity(current, &currentOpt) != nil || current.Version != journal.Version || len(current.Entries) < len(expected) {
			return fmt.Errorf("cleanup journal changed during replay")
		}
		for i, entry := range expected {
			if current.Entries[i] != entry {
				return fmt.Errorf("cleanup journal changed during replay")
			}
		}
		journal, entries = current, current.Entries
		return nil
	}
	for _, originalIndex := range order {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return result, err
		}
		i := originalIndex
		for removed := range consumed {
			if removed < originalIndex {
				i--
			}
		}
		if i < 0 || i >= len(entries) || entries[i].Scope != originalEntries[originalIndex].Scope || entries[i].Teardown != originalEntries[originalIndex].Teardown {
			return result, fmt.Errorf("cleanup journal entry changed during replay")
		}
		entry := entries[i]
		if strings.TrimSpace(entry.Teardown) == "" {
			continue
		}
		if journalfile.IsQuotaCommandCandidate(entry.Teardown) && !journalfile.IsSandboxQuotaDelete(entry.Teardown) {
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: invalid Quota teardown", entry.Scope))
			break
		}
		validateTeardown := validateJournalTeardown(entry.Teardown)
		if entry.VolumeFixtureKind != "" {
			validateTeardown = journalfile.ValidateVolumeFixtureTeardown(entry, journal)
		}
		if err := validateTeardown; err != nil {
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: invalid teardown: %v", entry.Scope, err))
			// The invalid command may be a child whose parents are earlier.
			break
		}
		// A retained Volume fences its ancestors, but independent finalizers
		// can still pass through their ordinary replay checks.
		if volumeBlocked && (journalfile.IsTeamDeleteCandidate(entry.Teardown) || entry.VolumeFixtureKind != "") {
			continue
		}
		if journalfile.IsTeamDeleteCandidate(entry.Teardown) && !entry.TeamCreateConfirmed {
			if err := checkOrdinaryTeamReplay(ctx, path, opt, journal, entry, parent); err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: %v", entry.Scope, err))
				break
			}
		}
		if entry.QuotaBound && (!entry.TeamCreateConfirmed || journal.QuotaBinding == nil) {
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: protected Team credential binding is missing", entry.Scope))
			break
		}
		if journalfile.IsQuotaDeleteCandidate(entry.Teardown) {
			if journal.QuotaBinding == nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: protected Quota credential binding is missing", entry.Scope))
				break
			}
			if err := withAnchoredJournalLock(ctx, path, parent, func() error {
				current, legacy, err := readReplayJournal(path)
				if err != nil {
					return err
				}
				currentOpt := opt
				if legacy || validateJournalIdentity(current, &currentOpt) != nil || current.Version != journal.Version || current.RunID != journal.RunID || current.ExecutionID != journal.ExecutionID || current.RegionRole != journal.RegionRole || current.Region != journal.Region || current.Surface != journal.Surface || current.EcctlBin != journal.EcctlBin {
					return fmt.Errorf("Quota delete journal binding changed")
				}
				return journalfile.CheckOwnedQuotaDelete(current, entry)
			}); err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: %v", entry.Scope, err))
				break
			}
		}
		if err := checkReplayParent(path, parent); err != nil {
			return result, err
		}
		volumeDelete := journalfile.IsNativeVolumeDelete(entry.Teardown)
		var volumeBinding journalfile.TemplateCreateIntent
		var volumeTeams []string
		if volumeDelete {
			var err error
			volumeBinding, err = journalfile.VolumeDeleteBinding(path, journal, opt.Config.Env, entry.Teardown)
			if err == nil && journalfile.HasHistoricalVolumeTransfer(journal, volumeBinding, entry) {
				retainHistoricalVolume(entry)
				continue
			}
			if err == nil {
				volumeTeams, err = journalfile.VolumePossibleTeams(journal, volumeBinding)
			}
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume scope binding is unavailable: %v", entry.Scope, err))
				break
			}
		}
		templateBound, err := journalfile.PrepareTemplateCleanup(ctx, path, journal, opt.Config)
		if err != nil {
			return result, err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return result, err
		}
		bound := opt.Config
		if entry.VolumeFixtureKind != "" {
			bucketPresence := journalfile.VolumeFixtureBucketUnknown
			if entry.VolumeFixturePending || journal.VolumeFixtureBinding == nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture creation outcome is uncertain", entry.Scope))
				break
			}
			bound, err = journalfile.ProbeQuotaBinding(ctx, journal.VolumeFixtureBinding, opt.Config)
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture credential binding changed: %v", entry.Scope, err))
				break
			}
			err = withAnchoredJournalLock(ctx, path, parent, func() error {
				current, legacy, err := readReplayJournal(path)
				if err != nil || legacy {
					return fmt.Errorf("Volume fixture journal is unavailable")
				}
				currentOpt := opt
				if validateJournalIdentity(current, &currentOpt) != nil || !reflect.DeepEqual(current.VolumeFixtureBinding, journal.VolumeFixtureBinding) || current.VolumeFixtureBucket != journal.VolumeFixtureBucket {
					return fmt.Errorf("Volume fixture journal ownership changed")
				}
				return journalfile.VolumeFixtureCleanupReady(path, current, entry, opt.Config.Env)
			})
			if err == nil {
				bucketPresence, err = journalfile.VolumeFixtureBucketEmpty(ctx, bound, journal.VolumeFixtureBucket, execpkg.Run)
			}
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture dependency is not safe to delete: %v", entry.Scope, err))
				break
			}
			if entry.VolumeFixtureKind != "bucket" && bucketPresence != journalfile.VolumeFixtureBucketVerifiedAbsent {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture RAM teardown requires verified bucket absence", entry.Scope))
				break
			}
			if entry.VolumeFixtureKind == "bucket" && bucketPresence == journalfile.VolumeFixtureBucketVerifiedAbsent {
				replayJournalAfterSuccess(path)
				if err := consumeVerifiedAbsentVolumeFixtureBucket(ctx, path, opt, journal, entry, parent); err != nil {
					return result, fmt.Errorf("retire absent Volume fixture bucket: %w", err)
				}
				result.Deleted++
				if err := refresh(originalIndex, i); err != nil {
					return result, err
				}
				continue
			}
		} else if entry.QuotaReserved || entry.QuotaBound {
			bound, err = journalfile.PrepareQuotaCleanup(ctx, path, journal, opt.Config)
			if err != nil {
				return result, err
			}
			if err := checkReplayParent(path, parent); err != nil {
				return result, err
			}
		} else if journalfile.IsTemplateCacheDelete(entry.Teardown) || journalfile.IsNativeTemplateDelete(entry.Teardown) || volumeDelete {
			bound = templateBound
		} else if journalfile.IsTeamDeleteCandidate(entry.Teardown) {
			var teamID string
			if entry.TeamCreateConfirmed {
				teamID, err = journalfile.TeamDeleteTarget(entry.Teardown)
			} else {
				region := journal.Region
				if region == "" {
					region = opt.Config.Region
				}
				teamID, err = journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, region)
			}
			if err != nil {
				return result, fmt.Errorf("Team replay target changed before binding: %w", err)
			}
			bound, _, err = journalfile.TemplateTeamCleanupBinding(path, journal, opt.Config, teamID)
			if err != nil {
				return result, err
			}
			creatorBound, found, creatorErr := journalfile.VolumeTeamCreatorConfig(ctx, journal, opt.Config, teamID)
			if creatorErr != nil {
				return result, creatorErr
			}
			if found {
				bound = creatorBound
			}
		}
		volumeTeamAlreadyAbsent := false
		var volumeTeamID string
		if entry.VolumeTeamBound && journalfile.IsTeamDeleteCandidate(entry.Teardown) {
			volumeTeamID, err = journalfile.TeamDeleteTarget(entry.Teardown)
			if err != nil {
				return result, err
			}
			if entry.TeamReplayAttempted {
				present, checkErr := journalfile.VolumeTeamChildInventoryPresent(path, volumeTeamID)
				if checkErr != nil {
					return result, checkErr
				}
				if !present {
					if journalfile.ProbeVolumeTeamAbsent(ctx, bound, volumeTeamID, journal.Region, bound.Profile, execpkg.Run) != nil {
						result.Errors++
						result.Details = append(result.Details, fmt.Sprintf("journal %s: attempted Volume Team lacks a child inventory; deletion remains uncertain", entry.Scope))
						continue
					}
					volumeTeamAlreadyAbsent = true
				}
			}
			if !volumeTeamAlreadyAbsent {
				volumeTeamAlreadyAbsent, err = journalfile.PrepareVolumeTeamChildren(ctx, path, journal, entry, bound, execpkg.Run)
				if err != nil {
					result.Errors++
					result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume Team child cleanup is unproven: %v", entry.Scope, err))
					continue
				}
			}
		}
		if entry.TeamCreateConfirmed {
			entry, err = markTeamReplayAttempt(ctx, path, opt, entry, parent)
			if err != nil {
				return result, fmt.Errorf("record Team replay attempt before delete: %w", err)
			}
			entries[i] = entry
			journal.Entries[i] = entry
		}
		if err := checkReplayParent(path, parent); err != nil {
			return result, err
		}
		deleteCommand := entry.Teardown
		volumeReadOnly := false
		freshVolumeAttempt := false
		if volumeDelete {
			bound = volumeBinding.BoundConfig(opt.Config)
			bound.Region, bound.Profile = volumeBinding.Region, volumeBinding.Profile
			if _, err := execpkg.ReadIdentity(ctx, bound, volumeBinding.Profile); err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume identity binding is unavailable: %v", entry.Scope, err))
				break
			}
			request, parseErr := journalfile.ParseVolumeDeleteRequest(entry.Teardown)
			if parseErr != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume delete target is invalid: %v", entry.Scope, parseErr))
				break
			}
			_, status, absent, probeErr := journalfile.ProbeVolumeScopesWithStatus(ctx, bound, request.ID, volumeBinding.Name, volumeBinding.Region, volumeBinding.Profile, volumeTeams, execpkg.Run)
			if probeErr != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume scope read is unavailable: %v", entry.Scope, probeErr))
				volumeBlocked = true
				continue
			}
			if status == "DELETING" {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume is already deleting", entry.Scope))
				volumeBlocked = true
				continue
			}
			if absent {
				if !journalfile.VolumeAbsenceTerminal(journal, volumeBinding, entry) {
					result.Errors++
					result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume absence is provisional", entry.Scope))
					volumeBlocked = true
					continue
				}
				volumeReadOnly = true
			} else {
				if !volumeBinding.VolumeObserved {
					result.Errors++
					result.Details = append(result.Details, fmt.Sprintf("journal %s: accepted Volume requires durable positive observation before replay delete", entry.Scope))
					volumeBlocked = true
					continue
				}
				if entry.VolumeDeleteAttempted {
					volumeReadOnly = true
				} else {
					if err := ctx.Err(); err != nil {
						return result, err
					}
					entry, err = markVolumeDeleteReplayAttempt(ctx, path, opt, journal, volumeBinding, entry, parent)
					if errors.Is(err, errHistoricalVolumeTransfer) {
						retainHistoricalVolume(entry)
						continue
					}
					if err != nil {
						return result, fmt.Errorf("record Volume delete attempt before replay: %w", err)
					}
					entries[i], journal.Entries[i] = entry, entry
					freshVolumeAttempt = true
					deleteCommand += " --team-id " + volumeBinding.TeamID
				}
			}
			deleteCommand += " --output=json --agent-envelope=false"
		}
		fencedFixture := journalfile.VolumeFixtureFencedKind(entry.VolumeFixtureKind)
		fixtureReadOnly := fencedFixture && (entry.VolumeFixtureDeleteAttempted || entry.VolumeFixtureDeleteProtocol != 1)
		var fixtureAbsentRead *execpkg.Result
		freshFixtureAttempt := false
		if fencedFixture && !fixtureReadOnly {
			if !opt.fixtureMutationAllowed(journal, entry, bound) {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture name mutation requires a provider-bound target or an exclusive mutation scope", entry.Scope))
				break
			}
			switch entry.VolumeFixtureKind {
			case "role":
				read, readErr := journalfile.VolumeFixtureRoleMatches(ctx, bound, entry, journal, execpkg.Run)
				err = readErr
				if err != nil {
					if _, absentErr := journalfile.VolumeFixtureRoleAbsent(ctx, bound, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return read }); absentErr == nil {
						fixtureReadOnly = true
						fixtureAbsentRead = &read
						err = nil
					}
				}
			case "policy":
				read, readErr := journalfile.VolumeFixturePolicyMatches(ctx, bound, entry, journal, execpkg.Run)
				err = readErr
				if err != nil && read.Err == nil && read.Started && read.Exit == 4 {
					if _, absentErr := journalfile.VolumeFixturePolicyAbsent(ctx, bound, entry, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return read }); absentErr == nil {
						fixtureReadOnly = true
						fixtureAbsentRead = &read
						err = nil
					} else {
						err = absentErr
					}
				}
			case "bucket":
				_, err = journalfile.VolumeFixtureBucketMatches(ctx, bound, entry, journal, execpkg.Run)
			case "attachment":
				var role, policy report.Resource
				role, policy, err = journalfile.VolumeFixtureRAMParents(journal)
				if err == nil {
					read, readErr := journalfile.VolumeFixtureRoleMatches(ctx, bound, role, journal, execpkg.Run)
					err = readErr
					if err != nil && read.Err == nil && read.Started && read.Exit == 4 {
						if _, absentErr := journalfile.VolumeFixtureRoleAbsent(ctx, bound, role, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return read }); absentErr == nil {
							fixtureReadOnly = true
							fixtureAbsentRead = &read
							err = nil
						} else {
							err = absentErr
						}
					}
				}
				if err == nil && !fixtureReadOnly {
					read, readErr := journalfile.VolumeFixturePolicyMatches(ctx, bound, policy, journal, execpkg.Run)
					err = readErr
					if err != nil && read.Err == nil && read.Started && read.Exit == 4 {
						if _, absentErr := journalfile.VolumeFixturePolicyAbsent(ctx, bound, policy, journal, func(context.Context, execpkg.Config, string) execpkg.Result { return read }); absentErr == nil {
							fixtureReadOnly = true
							fixtureAbsentRead = &read
							err = nil
						} else {
							err = absentErr
						}
					}
				}
			}
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: current Volume fixture identity is unproven: %v", entry.Scope, err))
				break
			}
			if !fixtureReadOnly {
				entry, err = markVolumeFixtureReplayAttempt(ctx, path, opt, journal, entry, parent)
				if err != nil {
					return result, fmt.Errorf("record Volume fixture delete attempt before replay: %w", err)
				}
				entries[i], journal.Entries[i] = entry, entry
				freshFixtureAttempt = true
			}
		}
		var res execpkg.Result
		if fixtureReadOnly {
			if fixtureAbsentRead != nil {
				res = *fixtureAbsentRead
			} else {
				switch entry.VolumeFixtureKind {
				case "role":
					res, err = journalfile.VolumeFixtureRoleAbsent(ctx, bound, entry, journal, execpkg.Run)
				case "bucket":
					res, err = journalfile.VolumeFixtureBucketAbsent(ctx, bound, entry, journal, execpkg.Run)
				case "policy":
					res, err = journalfile.VolumeFixturePolicyAbsent(ctx, bound, entry, journal, execpkg.Run)
				case "attachment":
					res, err = journalfile.VolumeFixtureAttachmentAbsent(ctx, bound, entry, journal, execpkg.Run)
				}
			}
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture absence is unproven: %v", entry.Scope, err))
				break
			}
		} else if volumeTeamAlreadyAbsent {
			res = execpkg.Result{Started: true, Exit: 4, JSON: map[string]any{"deleted": true}}
		} else if volumeReadOnly {
			res = execpkg.Result{Started: true, Exit: 0, JSON: map[string]any{"deleted": true}}
		} else {
			if entry.VolumeTeamBound {
				if err := journalfile.ReserveVolumeTeamDelete(ctx, path, journal, entry, bound); err != nil {
					result.Errors++
					result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume Team delete is fenced: %v", entry.Scope, err))
					continue
				}
			}
			res = execpkg.Run(ctx, bound, deleteCommand)
			if entry.VolumeTeamBound && journalfile.IsVolumeTeamChildConflict(res) {
				if err := journalfile.RecordVolumeTeamConflict(ctx, path, journal, entry, bound, res); err != nil {
					return result, fmt.Errorf("record Volume Team conflict: %w", err)
				}
				retry, retryErr := journalfile.RetryVolumeTeamDeleteAfterConflict(ctx, path, journal, entry, bound, execpkg.Run)
				if retryErr != nil {
					result.Errors++
					result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume Team conflict retry is fenced: %v", entry.Scope, retryErr))
					continue
				}
				res = retry
			}
		}
		if freshVolumeAttempt && !res.Started && res.Exit == -1 && res.Err != nil {
			commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replayJournalCommitTimeout)
			restored, restoreErr := restoreUnstartedVolumeDeleteReplayAttempt(commitCtx, path, opt, journal, volumeBinding, entry, parent)
			cancel()
			if errors.Is(restoreErr, errHistoricalVolumeTransfer) {
				retainHistoricalVolume(entry)
				continue
			}
			if restoreErr != nil {
				return result, fmt.Errorf("restore unused Volume replay marker: %w", restoreErr)
			}
			entry = restored
			entries[i], journal.Entries[i] = entry, entry
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume delete did not start: %v", entry.Scope, res.Err))
			volumeBlocked = true
			continue
		}
		if freshVolumeAttempt && journalfile.VolumeDefinitiveDeleteRejection(res) {
			current, absent, probeErr := journalfile.ProbeVolumeScopes(ctx, bound, volumeBinding.AcceptedVolumeID, volumeBinding.Name, volumeBinding.Region, volumeBinding.Profile, volumeTeams, execpkg.Run)
			if probeErr != nil || absent || current == "" {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: rejected Volume presence is unproven: %v", entry.Scope, probeErr))
				volumeBlocked = true
				continue
			}
			commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replayJournalCommitTimeout)
			restored, restoreErr := restoreUnstartedVolumeDeleteReplayAttempt(commitCtx, path, opt, journal, volumeBinding, entry, parent)
			cancel()
			if errors.Is(restoreErr, errHistoricalVolumeTransfer) {
				retainHistoricalVolume(entry)
				continue
			}
			if restoreErr != nil {
				return result, fmt.Errorf("restore rejected Volume replay marker: %w", restoreErr)
			}
			entry = restored
			entries[i], journal.Entries[i] = entry, entry
			result.Errors++
			result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume delete was definitively rejected; retry is available", entry.Scope))
			volumeBlocked = true
			continue
		}
		if freshFixtureAttempt && !res.Started && res.Err != nil && res.Exit == -1 {
			commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replayJournalCommitTimeout)
			restored, restoreErr := restoreUnstartedVolumeFixtureReplayAttempt(commitCtx, path, opt, journal, entry, parent)
			cancel()
			if restoreErr != nil {
				return result, fmt.Errorf("restore unused Volume fixture replay marker: %w", restoreErr)
			}
			entry = restored
			entries[i], journal.Entries[i] = entry, entry
		}
		if freshFixtureAttempt && entry.VolumeFixtureKind == "attachment" && entry.VolumeFixtureDeleteAttempted {
			res, err = journalfile.VolumeFixtureAttachmentAbsent(ctx, bound, entry, journal, execpkg.Run)
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture attachment absence is unproven: %v", entry.Scope, err))
				break
			}
		}
		if freshFixtureAttempt && entry.VolumeFixtureKind != "attachment" && entry.VolumeFixtureDeleteAttempted {
			if err := journalfile.VolumeFixtureDeleteConfirmed(entry, journal, bound, res); err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture delete outcome is unproven: %v", entry.Scope, err))
				break
			}
			res, err = journalfile.VolumeFixtureTerminalAbsent(ctx, bound, entry, journal, execpkg.Run)
			if err != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume fixture terminal absence is unproven: %v", entry.Scope, err))
				break
			}
		}
		if err := checkReplayParent(path, parent); err != nil {
			return result, err
		}
		volumeConfirmed := true
		if volumeDelete {
			request, parseErr := journalfile.ParseVolumeDeleteRequest(entry.Teardown)
			waitCtx, cancelWait := context.WithTimeout(ctx, 45*time.Second)
			volumeConfirmed = parseErr == nil && journalfile.WaitVolumeAbsent(waitCtx, bound, request.ID, volumeBinding.Name, volumeBinding.Region, volumeBinding.Profile, volumeTeams, execpkg.Run)
			cancelWait()
			volumeConfirmed = volumeConfirmed && journalfile.VolumeAbsenceTerminal(journal, volumeBinding, entry)
			if volumeConfirmed {
				res.Err = nil
				res.Exit = 0
			}
		}
		if entry.VolumeTeamBound && journalfile.IsTeamDeleteCandidate(entry.Teardown) && (res.Exit == 0 || res.Exit == 4) {
			teamID, parseErr := journalfile.TeamDeleteTarget(entry.Teardown)
			if parseErr != nil {
				return result, parseErr
			}
			if proofErr := journalfile.ProbeVolumeTeamAbsent(ctx, bound, teamID, journal.Region, bound.Profile, execpkg.Run); proofErr != nil {
				result.Errors++
				result.Details = append(result.Details, fmt.Sprintf("journal %s: Volume Team absence is unproven: %v", entry.Scope, proofErr))
				break
			}
		}
		if res.Err != nil || !volumeConfirmed || (res.Exit != 0 && res.Exit != 4 && !volumeDelete) {
			result.Errors++
			detail := fmt.Sprintf("journal %s: delete exit=%d %s", entry.Scope, res.Exit, failureReason(res))
			if volumeDelete && !volumeConfirmed {
				detail += "; Volume absence unproven"
			}
			result.Details = append(result.Details, detail)
			if volumeDelete {
				volumeBlocked = true
				continue
			}
			if journalfile.IsTemplateCacheDelete(entry.Teardown) || journalfile.IsQuotaDeleteCandidate(entry.Teardown) || entry.VolumeFixtureKind != "" {
				break
			}
			continue
		}
		result.Deleted++
		replayJournalAfterSuccess(path)
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replayJournalCommitTimeout)
		err = consumeReplayJournalEntry(commitCtx, path, opt, entry, parent)
		cancel()
		if err != nil {
			return result, fmt.Errorf("rewrite cleanup journal %s after replay: %w", path, err)
		}
		if err := refresh(originalIndex, i); err != nil {
			return result, err
		}
	}
	return result, nil
}

// A protocol-1 fixture gets one destructive attempt after this anchored
// marker is durably installed under the replay lock.
func markVolumeFixtureReplayAttempt(ctx context.Context, path string, opt ReplayOptions, expected report.CleanupJournal, entry report.Resource, parent os.FileInfo) (report.Resource, error) {
	if !journalfile.VolumeFixtureFencedKind(entry.VolumeFixtureKind) || entry.VolumeFixtureDeleteProtocol != 1 || entry.VolumeFixtureDeleteAttempted || entry.VolumeFixturePending {
		return entry, fmt.Errorf("Volume fixture has no fresh replay delete attempt")
	}
	err := withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if legacy || validateJournalIdentity(journal, &currentOpt) != nil || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
			return fmt.Errorf("Volume fixture replay journal binding changed")
		}
		if err := journalfile.ValidateVolumeFixtureTeardown(entry, journal); err != nil {
			return err
		}
		if err := journalfile.VolumeFixtureCleanupReady(path, journal, entry, opt.Config.Env); err != nil {
			return err
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate != entry {
				continue
			}
			if index >= 0 {
				return fmt.Errorf("Volume fixture replay entry is ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture replay entry changed")
		}
		originalBody, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeFixtureDeleteAttempted = true
		body, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		markedBody := append(body, '\n')
		if err := writeVolumeFixtureReplayAttemptJournal(path, markedBody); err != nil {
			if installed, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(installed, markedBody) {
				return errors.Join(err, journalfile.RestoreUninvokedMarker(path, markedBody, originalBody))
			}
			return err
		}
		installed, legacy, err := readReplayJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture replay delete attempt was not installed exactly")
		}
		return nil
	})
	if err != nil {
		return entry, err
	}
	entry.VolumeFixtureDeleteAttempted = true
	return entry, nil
}

func markVolumeDeleteReplayAttempt(ctx context.Context, path string, opt ReplayOptions, expected report.CleanupJournal, binding journalfile.TemplateCreateIntent, entry report.Resource, parent os.FileInfo) (report.Resource, error) {
	if !journalfile.IsNativeVolumeDelete(entry.Teardown) || entry.VolumeDeleteAttempted {
		return entry, fmt.Errorf("Volume has no fresh replay delete attempt")
	}
	err := withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if legacy || validateJournalIdentity(journal, &currentOpt) != nil || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin {
			return fmt.Errorf("Volume replay journal binding changed")
		}
		if journalfile.HasHistoricalVolumeTransfer(journal, binding, entry) {
			return errHistoricalVolumeTransfer
		}
		if _, err := journalfile.VolumePossibleTeams(journal, binding); err != nil {
			return err
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate.Teardown != entry.Teardown {
				continue
			}
			if candidate != entry || index >= 0 {
				return fmt.Errorf("Volume replay entry changed or became ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume replay entry disappeared")
		}
		originalBody, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		journal.Entries[index].VolumeDeleteAttempted = true
		body, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		markedBody := append(body, '\n')
		if err := journalfile.WriteDurable(path, markedBody); err != nil {
			if installed, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(installed, markedBody) {
				return errors.Join(err, journalfile.RestoreUninvokedMarker(path, markedBody, originalBody))
			}
			return err
		}
		installed, legacy, err := readReplayJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume replay delete attempt was not installed exactly")
		}
		return nil
	})
	if err != nil {
		return entry, err
	}
	entry.VolumeDeleteAttempted = true
	return entry, nil
}

// Restore only a marker committed by this replay when its subprocess did not
// start or the provider definitively rejected it without mutation.
func restoreUnstartedVolumeDeleteReplayAttempt(ctx context.Context, path string, opt ReplayOptions, expected report.CleanupJournal, binding journalfile.TemplateCreateIntent, entry report.Resource, parent os.FileInfo) (report.Resource, error) {
	if !journalfile.IsNativeVolumeDelete(entry.Teardown) || !entry.VolumeDeleteAttempted {
		return entry, fmt.Errorf("Volume has no newly marked replay delete attempt")
	}
	err := withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if legacy || validateJournalIdentity(journal, &currentOpt) != nil || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin {
			return fmt.Errorf("Volume replay journal binding changed before marker restoration")
		}
		if journalfile.HasHistoricalVolumeTransfer(journal, binding, entry) {
			return errHistoricalVolumeTransfer
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate.Teardown != entry.Teardown {
				continue
			}
			if candidate != entry || index >= 0 {
				return fmt.Errorf("Volume replay entry changed or became ambiguous before marker restoration")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume replay entry disappeared before marker restoration")
		}
		journal.Entries[index].VolumeDeleteAttempted = false
		body, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		if err := journalfile.WriteDurable(path, append(body, '\n')); err != nil {
			if barrierErr := journalfile.ReaffirmDurable(path, append(body, '\n')); barrierErr != nil {
				return errors.Join(err, barrierErr)
			}
		}
		installed, legacy, err := readReplayJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("unused Volume replay marker was not restored exactly")
		}
		return nil
	})
	if err != nil {
		return entry, err
	}
	entry.VolumeDeleteAttempted = false
	return entry, nil
}

// The caller has just installed this exact marker and its subprocess failed
// before Start. Historical markers never enter this restoration path.
func restoreUnstartedVolumeFixtureReplayAttempt(ctx context.Context, path string, opt ReplayOptions, expected report.CleanupJournal, entry report.Resource, parent os.FileInfo) (report.Resource, error) {
	if !journalfile.VolumeFixtureFencedKind(entry.VolumeFixtureKind) || entry.VolumeFixtureDeleteProtocol != 1 || !entry.VolumeFixtureDeleteAttempted || entry.VolumeFixturePending {
		return entry, fmt.Errorf("Volume fixture has no newly marked replay delete attempt")
	}
	err := withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if legacy || validateJournalIdentity(journal, &currentOpt) != nil || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
			return fmt.Errorf("Volume fixture replay journal binding changed")
		}
		if err := journalfile.ValidateVolumeFixtureTeardown(entry, journal); err != nil {
			return err
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate != entry {
				continue
			}
			if index >= 0 {
				return fmt.Errorf("Volume fixture replay entry is ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture replay entry changed")
		}
		journal.Entries[index].VolumeFixtureDeleteAttempted = false
		body, err := json.MarshalIndent(journal, "", "  ")
		if err != nil {
			return err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		if err := writeVolumeFixtureReplayRestoreJournal(path, append(body, '\n')); err != nil {
			return err
		}
		installed, legacy, err := readReplayJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("Volume fixture unused replay marker was not restored exactly")
		}
		return nil
	})
	if err != nil {
		return entry, err
	}
	entry.VolumeFixtureDeleteAttempted = false
	return entry, nil
}

// The replay lock is held. The original snapshot rules out a duplicate of a
// confirmed Team already consumed in this replay; the current journal checks
// the remaining children and ordinary finalizers.
func checkOrdinaryTeamReplay(ctx context.Context, path string, opt ReplayOptions, original report.CleanupJournal, entry report.Resource, parent os.FileInfo) error {
	region := original.Region
	if region == "" {
		region = opt.Config.Region
	}
	target, err := journalfile.TeamDeleteCandidateTargetForRegion(entry.Teardown, region)
	if err != nil {
		return err
	}
	for _, candidate := range original.Entries {
		if !candidate.TeamCreateConfirmed {
			continue
		}
		id, parseErr := journalfile.TeamDeleteTarget(candidate.Teardown)
		if parseErr != nil {
			return fmt.Errorf("invalid confirmed Team ownership in replay snapshot: %w", parseErr)
		}
		if id == target {
			return fmt.Errorf("ordinary Team replay target conflicts with confirmed ownership")
		}
	}
	return withAnchoredJournalLock(ctx, path, parent, func() error {
		current, _, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if validateJournalIdentity(current, &currentOpt) != nil {
			return fmt.Errorf("ordinary Team replay journal binding changed")
		}
		count, sameTarget := 0, 0
		for _, candidate := range current.Entries {
			if candidate == entry {
				count++
			}
			if journalfile.IsQuotaCommandCandidate(candidate.Teardown) {
				childID, parseErr := journalfile.SandboxQuotaDeleteTarget(candidate.Teardown)
				if parseErr != nil || childID == target {
					return fmt.Errorf("ordinary Team replay must wait for its Quota child")
				}
			}
			if !journalfile.IsTeamDeleteCandidate(candidate.Teardown) {
				continue
			}
			currentRegion := current.Region
			if currentRegion == "" {
				currentRegion = currentOpt.Config.Region
			}
			id, parseErr := journalfile.TeamDeleteCandidateTargetForRegion(candidate.Teardown, currentRegion)
			if parseErr != nil {
				return fmt.Errorf("unclassified Team delete in replay journal: %w", parseErr)
			}
			if id == target {
				sameTarget++
				if candidate.TeamCreateConfirmed {
					return fmt.Errorf("ordinary Team replay target conflicts with confirmed ownership")
				}
			}
		}
		if count != 1 || sameTarget != 1 {
			return fmt.Errorf("ordinary Team replay target is missing or ambiguous")
		}
		return nil
	})
}

// The replay lock is held by the caller. A durable pre-delete mark prevents a
// later Quota reservation from treating a Team whose delete result was unknown
// or whose successful journal consumption failed as an untouched parent.
func markTeamReplayAttempt(ctx context.Context, path string, opt ReplayOptions, entry report.Resource, parent os.FileInfo) (report.Resource, error) {
	err := withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if err := validateJournalIdentity(journal, &currentOpt); err != nil {
			return err
		}
		if outstanding, err := journalfile.OutstandingVolumeChild(path, journal, opt.Config.Env); err != nil {
			return err
		} else if outstanding {
			return fmt.Errorf("Team replay must wait for its Volume child")
		}
		if entry.VolumeTeamBound {
			for name, id := range journal.VolumeTeamCreateIDs {
				if id == "" {
					return fmt.Errorf("Volume Team replay must wait for pending registration %s", name)
				}
			}
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate == entry {
				if index >= 0 {
					return fmt.Errorf("ambiguous Team replay parent")
				}
				index = i
			}
		}
		if index < 0 {
			return fmt.Errorf("Team replay parent changed before delete")
		}
		target, err := journalfile.TeamDeleteTarget(entry.Teardown)
		if err != nil {
			return err
		}
		confirmed := 0
		for _, candidate := range journal.Entries {
			if journalfile.IsTeamDeleteCandidate(candidate.Teardown) && !candidate.TeamCreateConfirmed {
				ordinaryTarget, ordinaryErr := journalfile.TeamDeleteCandidateTargetForRegion(candidate.Teardown, journal.Region)
				if ordinaryErr != nil {
					return fmt.Errorf("unclassified ordinary Team delete in replay journal: %w", ordinaryErr)
				}
				if ordinaryTarget == target {
					return fmt.Errorf("Team replay target conflicts with ordinary finalizer")
				}
			}
			candidateTarget, candidateErr := journalfile.TeamDeleteTarget(candidate.Teardown)
			if candidateErr == nil && candidateTarget == target && candidate.TeamCreateConfirmed {
				confirmed++
			}
			if journalfile.IsQuotaCommandCandidate(candidate.Teardown) {
				childID, parseErr := journalfile.SandboxQuotaDeleteTarget(candidate.Teardown)
				if parseErr != nil || childID == target {
					return fmt.Errorf("Team replay must wait for its Quota child")
				}
			}
		}
		if confirmed > 1 || confirmed == 1 && !entry.TeamCreateConfirmed {
			return fmt.Errorf("Team replay target conflicts with confirmed ownership")
		}
		journal.Entries[index].TeamReplayAttempted = true
		var value any = journal
		if legacy {
			value = journal.Entries
		}
		body, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		return writeTeamReplayAttemptJournal(path, append(body, '\n'))
	})
	if err != nil {
		return entry, err
	}
	entry.TeamReplayAttempted = true
	return entry, nil
}

func consumeReplayJournalEntry(ctx context.Context, path string, opt ReplayOptions, completed report.Resource, parent os.FileInfo) error {
	return withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if err := validateJournalIdentity(journal, &currentOpt); err != nil {
			return err
		}
		quotaDelete := journalfile.IsQuotaDeleteCandidate(completed.Teardown)
		if quotaDelete {
			if legacy || journalfile.CheckOwnedQuotaDelete(journal, completed) != nil {
				return fmt.Errorf("settled Quota reservation changed before consumption")
			}
		}
		if completed.TeamCreateConfirmed {
			if outstanding, err := journalfile.OutstandingVolumeChild(path, journal, opt.Config.Env); err != nil {
				return err
			} else if outstanding {
				return fmt.Errorf("Team replay must wait for its Volume child before consumption")
			}
		}
		index := -1
		for i := len(journal.Entries) - 1; i >= 0; i-- {
			if journal.Entries[i] == completed {
				index = i
				break
			}
		}
		if index < 0 {
			if quotaDelete || journalfile.IsNativeVolumeDelete(completed.Teardown) {
				return fmt.Errorf("settled protected finalizer disappeared")
			}
			return nil
		}
		if journalfile.IsNativeVolumeDelete(completed.Teardown) {
			if legacy {
				return fmt.Errorf("owned Volume finalizer requires a versioned journal")
			}
			intent, err := journalfile.VolumeDeleteBinding(path, journal, opt.Config.Env, completed.Teardown)
			if err != nil {
				return err
			}
			if err := journalfile.RetireVolumeFinalizer(&journal, intent, journal.Entries[index]); err != nil {
				return err
			}
			if err := checkReplayParent(path, parent); err != nil {
				return err
			}
			return rewriteReplayJournal(path, journal, journal.Entries, false)
		}
		pending := append([]report.Resource(nil), journal.Entries...)
		pending = append(pending[:index], pending[index+1:]...)
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		return rewriteReplayJournal(path, journal, pending, legacy)
	})
}

func consumeVerifiedAbsentVolumeFixtureBucket(ctx context.Context, path string, opt ReplayOptions, expected report.CleanupJournal, entry report.Resource, parent os.FileInfo) error {
	if entry.VolumeFixtureKind != "bucket" || entry.VolumeFixturePending {
		return fmt.Errorf("Volume fixture bucket is not settled")
	}
	return withAnchoredJournalLock(ctx, path, parent, func() error {
		journal, legacy, err := readReplayJournal(path)
		if err != nil {
			return err
		}
		currentOpt := opt
		if legacy || validateJournalIdentity(journal, &currentOpt) != nil || journal.Version != expected.Version || journal.JournalPath != path || journal.RunID != expected.RunID || journal.ExecutionID != expected.ExecutionID || journal.RegionRole != expected.RegionRole || journal.Region != expected.Region || journal.Surface != expected.Surface || journal.EcctlBin != expected.EcctlBin || !reflect.DeepEqual(journal.VolumeFixtureBinding, expected.VolumeFixtureBinding) || journal.VolumeFixtureBucket != expected.VolumeFixtureBucket {
			return fmt.Errorf("Volume fixture bucket journal binding changed before absence settlement")
		}
		if err := journalfile.VolumeFixtureCleanupReady(path, journal, entry, opt.Config.Env); err != nil {
			return err
		}
		index := -1
		for i, candidate := range journal.Entries {
			if candidate.Teardown != entry.Teardown {
				continue
			}
			if candidate != entry || index >= 0 {
				return fmt.Errorf("Volume fixture bucket finalizer changed or became ambiguous")
			}
			index = i
		}
		if index < 0 {
			return fmt.Errorf("Volume fixture bucket finalizer disappeared")
		}
		pending := append([]report.Resource(nil), journal.Entries...)
		pending = append(pending[:index], pending[index+1:]...)
		if err := checkReplayParent(path, parent); err != nil {
			return err
		}
		if err := rewriteReplayJournal(path, journal, pending, false); err != nil {
			return err
		}
		journal.Entries = pending
		installed, legacy, err := readReplayJournal(path)
		if err != nil || legacy || !reflect.DeepEqual(installed, journal) {
			return fmt.Errorf("absent Volume fixture bucket finalizer was not retired exactly")
		}
		return nil
	})
}

func rewriteReplayJournal(path string, journal report.CleanupJournal, pending []report.Resource, legacy bool) error {
	var value any
	if legacy {
		value = pending
	} else {
		journal.Entries = pending
		value = journal
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeReplayConsumptionJournal(path, append(data, '\n'))
}

func validateJournalIdentity(journal report.CleanupJournal, opt *ReplayOptions) error {
	if opt.RunID != "" && journal.RunID != "" && opt.RunID != journal.RunID {
		return fmt.Errorf("cleanup journal run id %q does not match %q", journal.RunID, opt.RunID)
	}
	if opt.Surface != "" && journal.Surface != "" && opt.Surface != journal.Surface {
		return fmt.Errorf("cleanup journal surface %q does not match %q", journal.Surface, opt.Surface)
	}
	if opt.Config.Region != "" && journal.Region != "" && opt.Config.Region != journal.Region {
		return fmt.Errorf("cleanup journal region %q does not match %q", journal.Region, opt.Config.Region)
	}
	if opt.Config.Bin != "" && filepath.IsAbs(journal.EcctlBin) && !filepath.IsAbs(opt.Config.Bin) {
		found, err := exec.LookPath(opt.Config.Bin)
		if err != nil {
			return fmt.Errorf("resolve cleanup replay binary: %w", err)
		}
		opt.Config.Bin, err = filepath.Abs(found)
		if err != nil {
			return err
		}
	}
	if opt.Config.Bin != "" && journal.EcctlBin != "" && opt.Config.Bin != journal.EcctlBin {
		return fmt.Errorf("cleanup journal binary %q does not match %q", journal.EcctlBin, opt.Config.Bin)
	}
	if opt.Config.Region == "" {
		opt.Config.Region = journal.Region
	}
	if opt.Config.Bin == "" {
		opt.Config.Bin = journal.EcctlBin
	}
	if opt.Config.Bin == "" {
		opt.Config.Bin = "ecctl"
	}
	if journal.EcctlBinSHA256 != "" {
		actual, err := journalfile.ExecutableSHA256(opt.Config.Bin)
		if err != nil || actual != journal.EcctlBinSHA256 {
			return fmt.Errorf("cleanup journal executable digest changed: %v", err)
		}
		opt.Config.ExpectedBinSHA256 = journal.EcctlBinSHA256
	}
	return nil
}

func validateJournalTeardown(command string) error {
	return journalcmd.ValidateDelete(command)
}

// Sweep lists every configured kind and deletes the orphans.
func Sweep(ctx context.Context, opt Options) (*Result, error) {
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	cfg, err := loadConfig(opt.ConfigFile)
	if err != nil {
		return nil, err
	}
	if opt.ByFinishedRun && opt.CheckFinished == nil {
		return nil, fmt.Errorf("by-finished-run requires a run checker (GH_TOKEN + repo)")
	}
	concurrency := opt.Concurrency
	if concurrency < 1 {
		concurrency = defaultConcurrency
	}
	execCfg := execpkg.Config{Bin: opt.EcctlBin, Region: opt.Region, Env: opt.Env}
	res := &Result{}
	var mu sync.Mutex

	// Kinds are processed in config order so dependency ordering holds (e.g.
	// instances before the vswitches/vpcs they sit in); deletes *within* a kind
	// are independent, so they run concurrently with a per-kind barrier.
	for _, k := range cfg.Kinds {
		opt.Logf("scanning %s ...", k.Name)
		listRes := execpkg.Run(ctx, execCfg, k.List)
		if listRes.Exit != 0 || listRes.Err != nil {
			res.Errors++
			res.Details = append(res.Details, fmt.Sprintf("%s: list failed: exit=%d %s", k.Name, listRes.Exit, failureReason(listRes)))
			opt.Logf("%s: list failed (%s)", k.Name, failureReason(listRes))
			continue
		}
		items, _ := jsonq.Get(listRes.JSON, "$", k.ItemsPath)
		arr, _ := items.([]any)

		// Selection is sequential and cheap; collect the orphans to delete.
		var tasks []deleteTask
		for _, item := range arr {
			id, ok := stringAt(item, k.IDField)
			if !ok {
				continue
			}
			del, reason := shouldDelete(opt, item, k)
			if !del {
				res.Skipped++
				opt.Logf("%s %s: keep (%s)", k.Name, id, reason)
				continue
			}
			cmd, err := renderDeleteCommand(k, item, id)
			if err != nil {
				res.Errors++
				res.Details = append(res.Details, fmt.Sprintf("%s %s: render delete command failed: %v", k.Name, id, err))
				continue
			}
			tasks = append(tasks, deleteTask{kind: k.Name, id: id, reason: reason, cmd: cmd})
		}

		opt.Logf("%s: found %d, deleting %d", k.Name, len(arr), len(tasks))

		if opt.DryRun {
			for _, t := range tasks {
				opt.Logf("%s %s: would delete (%s): %s", t.kind, t.id, t.reason, t.cmd)
				res.Deleted++
			}
			continue
		}

		runDeletes(ctx, tasks, concurrency, res, &mu, opt.Logf, func(ctx context.Context, t deleteTask) execpkg.Result {
			return execpkg.Run(ctx, execCfg, t.cmd)
		})
	}
	return res, nil
}

func renderDeleteCommand(kind Kind, item any, id string) (string, error) {
	data := map[string]any{"id": id}
	for name, path := range kind.DeleteFields {
		value, ok := stringAt(item, path)
		if !ok {
			return "", fmt.Errorf("delete field %q path %q is missing or not a string", name, path)
		}
		data[name] = value
	}
	return vars.Render(kind.Delete, data)
}

// deleteTask is one orphan selected for deletion.
type deleteTask struct {
	kind   string
	id     string
	reason string
	cmd    string
}

// runDeletes executes the given deletes with at most `concurrency` in flight,
// updating res under mu, and blocks until all have finished (a barrier, so the
// caller can safely move on to a dependent kind). run performs a single delete;
// it is a parameter so tests can inject one without spawning processes.
func runDeletes(ctx context.Context, tasks []deleteTask, concurrency int, res *Result, mu *sync.Mutex, logf func(string, ...any), run func(context.Context, deleteTask) execpkg.Result) {
	if len(tasks) == 0 {
		return
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t deleteTask) {
			defer wg.Done()
			defer func() { <-sem }()
			dr := run(ctx, t)
			// A just-emptied network resource (vpc/vswitch/sg) often reports
			// DependencyViolation for a short while because its ENIs are still
			// being released; throttling is likewise transient. Retry both with a
			// linear backoff so a single sweep is self-sufficient.
			for attempt := 1; attempt < sweepDeleteRetries &&
				(dr.Exit != 0 || dr.Err != nil) && isTransientSweepFailure(dr) && ctx.Err() == nil; attempt++ {
				sweepRetrySleep(time.Duration(attempt) * sweepRetryInterval)
				dr = run(ctx, t)
			}
			mu.Lock()
			defer mu.Unlock()
			if dr.Exit != 0 || dr.Err != nil {
				res.Errors++
				res.Details = append(res.Details, fmt.Sprintf("%s %s: delete exit=%d %s", t.kind, t.id, dr.Exit, failureReason(dr)))
				return
			}
			res.Deleted++
			logf("%s %s: deleted (%s)", t.kind, t.id, t.reason)
		}(t)
	}
	wg.Wait()
}

// shouldDelete decides whether one item is an orphan to remove.
func shouldDelete(opt Options, item any, k Kind) (bool, string) {
	if opt.ByFinishedRun {
		runID, ok := stringAt(item, k.RunIDField)
		if !ok {
			return false, "no run-id tag"
		}
		finished, err := opt.CheckFinished(runID)
		if err != nil {
			return false, "run check error: " + err.Error()
		}
		if finished {
			return true, "run " + runID + " finished"
		}
		return false, "run " + runID + " active"
	}
	// TTL mode
	created, ok := stringAt(item, k.CreatedField)
	if !ok {
		return false, fmt.Sprintf("no %s field (skipped in ttl mode)", k.CreatedField)
	}
	t, err := parseTimestamp(created)
	if err != nil {
		return false, fmt.Sprintf("unparseable %s %q", k.CreatedField, created)
	}
	if time.Since(t) > opt.TTL {
		return true, fmt.Sprintf("age %s > ttl", time.Since(t).Round(time.Minute))
	}
	return false, "within ttl"
}

// parseTimestamp accepts the timestamp formats Alibaba Cloud returns. Most APIs
// emit UTC minute-precision (e.g. "2026-06-29T06:04Z"), which is not valid
// RFC3339, so several layouts are tried before giving up.
func parseTimestamp(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,             // 2026-06-29T06:04:05Z07:00
		"2006-01-02T15:04Z07:00", // 2026-06-29T06:04Z (Aliyun default)
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04Z",
		"2006-01-02 15:04:05",
	}
	var err error
	for _, layout := range layouts {
		var t time.Time
		if t, err = time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, err
}

// failureReason extracts a concise, single-line reason from a failed ecctl
// invocation (list or delete) so the sweep summary can explain why, rather than
// just reporting an exit code. ecctl prints its structured error to stdout (as
// {"error": {...}}), so that is the primary signal; it falls back to raw
// stdout, then stderr, then the process-level error (which is nil for a clean
// non-zero exit).
func failureReason(r execpkg.Result) string {
	if msg := ecctlErrorMessage(r.JSON); msg != "" {
		return msg
	}
	if msg := flattenLine(r.Stdout); msg != "" {
		return msg
	}
	if msg := flattenLine(r.Stderr); msg != "" {
		return msg
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return "(no output)"
}

// ecctlErrorMessage formats the {"error": {...}} payload ecctl emits on failure
// into a "code: message (suggestion)" line. Returns "" when the parsed JSON
// is not an ecctl error object.
func ecctlErrorMessage(v any) string {
	root, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errObj["code"].(string)
	message, _ := errObj["message"].(string)
	out := strings.TrimSpace(strings.TrimPrefix(code+": "+message, ": "))
	if action, _ := errObj["suggested_action"].(string); action != "" {
		out += " (" + action + ")"
	}
	return flattenLine(out)
}

// flattenLine collapses multi-line, whitespace-heavy output into a single
// trimmed line, truncating to keep summaries readable.
func flattenLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	const max = 300
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func stringAt(item any, field string) (string, bool) {
	if field == "" {
		return "", false
	}
	v, ok := jsonq.Get(item, "$", field)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(s), s != ""
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Kinds) == 0 {
		return nil, fmt.Errorf("%s: no kinds defined", path)
	}
	return &c, nil
}
