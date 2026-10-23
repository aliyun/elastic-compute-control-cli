package sweeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalfile"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
)

// TemplateCacheReconcileOptions requires explicit external evidence for the
// journal, its delete binary, and the binary used for a read-only cache probe.
type TemplateCacheReconcileOptions struct {
	ReplayOptions
	JournalSHA256    string
	JournalBinSHA256 string
	ProbeBin         string
	ProbeBinSHA256   string
	Profile          string
}

type TemplateCacheReconcileResult struct {
	RunID         string `json:"run_id"`
	TemplateID    string `json:"template_id"`
	TeamID        string `json:"team_id"`
	Status        string `json:"status"`
	EvidencePath  string `json:"evidence_path"`
	JournalSHA256 string `json:"journal_sha256"`
}

type templateCacheReconcileEvidence struct {
	Version             int                 `json:"version"`
	RunID               string              `json:"run_id"`
	ExecutionID         string              `json:"execution_id"`
	Region              string              `json:"region"`
	Surface             string              `json:"surface"`
	JournalSHA256       string              `json:"journal_sha256"`
	JournalBin          string              `json:"journal_bin"`
	JournalBinSHA256    string              `json:"journal_bin_sha256"`
	ProbeBin            string              `json:"probe_bin"`
	ProbeBinSHA256      string              `json:"probe_bin_sha256"`
	NativeTemplateOwner string              `json:"native_template_owner"`
	TemplateID          string              `json:"template_id"`
	TeamID              string              `json:"team_id"`
	Profile             string              `json:"profile"`
	Identity            execpkg.IdentityPin `json:"identity"`
	Status              string              `json:"status"`
	ProbeSHA256         string              `json:"probe_sha256"`
}

// Test synchronization point for a crash after durable evidence but before
// the pending journal marker is durably cleared.
var reconcileTemplateCacheAfterEvidence = func(string) error { return nil }

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func regularFileSHA256(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("reconciliation binary is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkReconcileBinaries(opt TemplateCacheReconcileOptions) error {
	for _, pair := range [][2]string{{opt.Config.Bin, opt.JournalBinSHA256}, {opt.ProbeBin, opt.ProbeBinSHA256}} {
		actual, err := regularFileSHA256(pair[0])
		if err != nil {
			return err
		}
		if actual != pair[1] {
			return fmt.Errorf("Template Cache reconciliation binary digest changed")
		}
	}
	return nil
}

func readBoundReconcileJournal(path string, opt TemplateCacheReconcileOptions) (report.CleanupJournal, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return report.CleanupJournal{}, err
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != opt.JournalSHA256 {
		return report.CleanupJournal{}, fmt.Errorf("Template Cache reconciliation journal digest changed")
	}
	var journal report.CleanupJournal
	if err := json.Unmarshal(body, &journal); err != nil || journal.Version != 2 {
		return report.CleanupJournal{}, fmt.Errorf("Template Cache reconciliation requires a version 2 journal")
	}
	check := opt.ReplayOptions
	if err := validateJournalIdentity(journal, &check); err != nil {
		return report.CleanupJournal{}, err
	}
	if journal.RunID != opt.RunID || journal.Surface != opt.Surface || journal.Region != opt.Config.Region || journal.EcctlBin != opt.Config.Bin {
		return report.CleanupJournal{}, fmt.Errorf("Template Cache reconciliation journal binding is incomplete")
	}
	return journal, nil
}

func quoteReconcileArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func terminalTemplateCacheStatus(value string) bool {
	return value == "Success" || value == "Failed" || value == "Evicted"
}

// ReconcilePendingTemplateCache proves one reserved cache exists in a terminal
// state before clearing its replay fence. It performs no delete; replay remains
// a separate, fully validated command.
func ReconcilePendingTemplateCache(ctx context.Context, path string, opt TemplateCacheReconcileOptions) (*TemplateCacheReconcileResult, error) {
	if opt.RunID == "" || (opt.Surface != "public" && opt.Surface != "full") || opt.Config.Region == "" || opt.Profile == "" || !filepath.IsAbs(opt.Config.Bin) || !filepath.IsAbs(opt.ProbeBin) || !validSHA256(opt.JournalSHA256) || !validSHA256(opt.JournalBinSHA256) || !validSHA256(opt.ProbeBinSHA256) {
		return nil, fmt.Errorf("Template Cache reconciliation requires explicit run, surface, region, profile, absolute binaries and SHA-256 digests")
	}
	resolved, err := journalfile.ResolveJournalPath(path)
	if err != nil {
		return nil, err
	}
	var result *TemplateCacheReconcileResult
	err = journalfile.WithReplayLock(ctx, resolved, func() error {
		if err := checkReconcileBinaries(opt); err != nil {
			return err
		}
		var journal report.CleanupJournal
		var owned journalfile.TemplateCreateIntent
		var entry report.Resource
		var id string
		if err := journalfile.WithLock(ctx, resolved, func() error {
			var err error
			journal, err = readBoundReconcileJournal(resolved, opt)
			if err != nil {
				return err
			}
			owned, entry, id, err = journalfile.PendingTemplateCacheCreate(resolved, journal, opt.Config.Env)
			return err
		}); err != nil {
			return err
		}
		if owned.Profile != opt.Profile || owned.Identity.Validate() != nil {
			return fmt.Errorf("Template Cache reconciliation profile or identity differs from native Template owner")
		}
		probeCfg := owned.BoundConfig(execpkg.Config{Bin: opt.ProbeBin, Region: journal.Region, Env: opt.Config.Env})
		identityCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := execpkg.ReadIdentity(identityCtx, probeCfg, owned.Profile)
		cancel()
		if err != nil {
			return err
		}
		command := strings.Join([]string{"ecctl", "sandbox", "template-cache", "get", quoteReconcileArg(id), "--team-id", quoteReconcileArg(owned.TeamID), "--region", quoteReconcileArg(journal.Region), "--profile", quoteReconcileArg(owned.Profile), "--output=json", "--lang=en"}, " ")
		probeCtx, cancel := context.WithTimeout(ctx, time.Minute)
		probe := execpkg.Run(probeCtx, probeCfg, command)
		cancel()
		if probe.Err != nil || probe.Exit != 0 {
			return fmt.Errorf("Template Cache reconciliation get did not confirm an owned terminal cache")
		}
		root, ok := probe.JSON.(map[string]any)
		if !ok {
			return fmt.Errorf("Template Cache reconciliation get returned invalid JSON")
		}
		cache, ok := root["template_cache"].(map[string]any)
		status, _ := cache["status"].(string)
		if !ok || cache["id"] != id || cache["template_id"] != id || cache["team_id"] != owned.TeamID || !terminalTemplateCacheStatus(status) {
			return fmt.Errorf("Template Cache reconciliation get did not confirm exact ownership and terminal status")
		}
		if err := checkReconcileBinaries(opt); err != nil {
			return err
		}
		probeDigest := sha256.Sum256([]byte(probe.Stdout))
		evidence := templateCacheReconcileEvidence{Version: 1, RunID: journal.RunID, ExecutionID: journal.ExecutionID, Region: journal.Region, Surface: journal.Surface, JournalSHA256: opt.JournalSHA256, JournalBin: opt.Config.Bin, JournalBinSHA256: opt.JournalBinSHA256, ProbeBin: opt.ProbeBin, ProbeBinSHA256: opt.ProbeBinSHA256, NativeTemplateOwner: owned.Name, TemplateID: id, TeamID: owned.TeamID, Profile: owned.Profile, Identity: owned.Identity, Status: status, ProbeSHA256: hex.EncodeToString(probeDigest[:])}
		entryDigest := sha256.Sum256([]byte(entry.Teardown))
		evidencePath := resolved + ".template-cache-reconcile-" + hex.EncodeToString(entryDigest[:8]) + ".json"
		err = journalfile.WithLock(ctx, resolved, func() error {
			current, err := readBoundReconcileJournal(resolved, opt)
			if err != nil {
				return err
			}
			currentOwner, currentEntry, currentID, err := journalfile.PendingTemplateCacheCreate(resolved, current, opt.Config.Env)
			if err != nil || currentEntry != entry || currentID != id || !reflect.DeepEqual(currentOwner, owned) {
				return fmt.Errorf("Template Cache reconciliation ownership changed before commit")
			}
			if err := checkReconcileBinaries(opt); err != nil {
				return err
			}
			if err := writeTemplateCacheEvidence(evidencePath, evidence); err != nil {
				return err
			}
			if err := reconcileTemplateCacheAfterEvidence(evidencePath); err != nil {
				return err
			}
			index := -1
			for i := range current.Entries {
				if current.Entries[i] == entry {
					if index >= 0 {
						return fmt.Errorf("ambiguous pending Template Cache finalizer")
					}
					index = i
				}
			}
			if index < 0 {
				return fmt.Errorf("pending Template Cache finalizer disappeared")
			}
			current.Entries[index].CacheCreatePending = false
			body, err := json.MarshalIndent(current, "", "  ")
			if err != nil {
				return err
			}
			return journalfile.WriteDurable(resolved, append(body, '\n'))
		})
		if err != nil {
			return err
		}
		result = &TemplateCacheReconcileResult{RunID: journal.RunID, TemplateID: id, TeamID: owned.TeamID, Status: status, EvidencePath: evidencePath}
		updated, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		updatedDigest := sha256.Sum256(updated)
		result.JournalSHA256 = hex.EncodeToString(updatedDigest[:])
		return journalfile.WithLock(ctx, resolved, func() error {
			return journalfile.CheckTemplateCreateIntents(resolved, report.CleanupJournal{}, opt.Config.Env)
		})
	})
	return result, err
}

func writeTemplateCacheEvidence(path string, evidence templateCacheReconcileEvidence) error {
	body, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if err := journalfile.WriteExclusiveDurable(path, append(body, '\n')); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("Template Cache reconciliation evidence is invalid")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(actual))
	decoder.DisallowUnknownFields()
	var previous templateCacheReconcileEvidence
	if err := decoder.Decode(&previous); err != nil || previous.Version != 1 || !terminalTemplateCacheStatus(previous.Status) || !validSHA256(previous.ProbeSHA256) {
		return fmt.Errorf("Template Cache reconciliation evidence is invalid")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("Template Cache reconciliation evidence has trailing data")
	}
	if previous.RunID != evidence.RunID || previous.ExecutionID != evidence.ExecutionID || previous.Region != evidence.Region || previous.Surface != evidence.Surface || previous.JournalSHA256 != evidence.JournalSHA256 || previous.JournalBin != evidence.JournalBin || previous.JournalBinSHA256 != evidence.JournalBinSHA256 || previous.ProbeBin != evidence.ProbeBin || previous.ProbeBinSHA256 != evidence.ProbeBinSHA256 || previous.NativeTemplateOwner != evidence.NativeTemplateOwner || previous.TemplateID != evidence.TemplateID || previous.TeamID != evidence.TeamID || previous.Profile != evidence.Profile || previous.Identity != evidence.Identity || previous.Status != evidence.Status {
		return fmt.Errorf("Template Cache reconciliation evidence conflicts with current ownership")
	}
	return nil
}
