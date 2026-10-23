package journalfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/journalcmd"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

// ErrTemplateCacheCreatePending keeps a reserved delete until the first
// asynchronous cache create is known to have completed and become visible.
var ErrTemplateCacheCreatePending = errors.New("pending Template Cache create")

// ResolveJournalPath gives lock, journal IO and sidecar discovery one target.
func ResolveJournalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	bound, err := resolveTemplateConfigPath(abs)
	if err != nil {
		return "", err
	}
	return bound.Target, nil
}

// CheckJournalPath pins protected Team/Quota ownership to the pathname whose
// replay lock guards it. A verbatim copy must not become a second owner.
func CheckJournalPath(path string, journal report.CleanupJournal) error {
	if journal.JournalPath != "" && journal.JournalPath != path {
		return fmt.Errorf("cleanup journal canonical path changed")
	}
	for _, entry := range journal.Entries {
		if entry.TeamCreateConfirmed || entry.QuotaBound || entry.QuotaReserved || entry.QuotaUpdatePending {
			if journal.Version != 2 || journal.JournalPath != path {
				return fmt.Errorf("protected Team/Quota journal anchor missing or changed")
			}
		}
	}
	return nil
}

func loadTemplateLedger(path string, expected report.CleanupJournal) (report.CleanupJournal, bool, error) {
	if expected.NativeJournalPath != "" && path != expected.NativeJournalPath {
		return report.CleanupJournal{}, false, fmt.Errorf("retained native ownership journal target changed")
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if expected.NativeJournalPath != "" {
			return report.CleanupJournal{}, false, fmt.Errorf("required native ownership journal is missing")
		}
		return expected, false, nil
	}
	if err != nil {
		return report.CleanupJournal{}, false, err
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		if expected.NativeJournalPath != "" {
			return report.CleanupJournal{}, true, fmt.Errorf("required native ownership metadata is missing")
		}
		return expected, true, nil
	}
	var actual report.CleanupJournal
	if json.Unmarshal(body, &actual) != nil {
		return actual, true, fmt.Errorf("invalid cleanup journal metadata")
	}
	if err := CheckJournalPath(path, actual); err != nil {
		return actual, true, err
	}
	for _, pair := range [][2]string{{actual.RunID, expected.RunID}, {actual.ExecutionID, expected.ExecutionID}, {actual.RegionRole, expected.RegionRole}, {actual.Region, expected.Region}, {actual.Surface, expected.Surface}, {actual.EcctlBin, expected.EcctlBin}} {
		if pair[1] != "" && pair[0] != pair[1] {
			return actual, true, fmt.Errorf("cleanup journal binding changed")
		}
	}
	if actual.NativeJournalPath != "" && actual.NativeJournalPath != path {
		return actual, true, fmt.Errorf("native ownership journal path changed")
	}
	if expected.NativeJournalPath != "" {
		if actual.NativeJournalPath != expected.NativeJournalPath {
			return actual, true, fmt.Errorf("retained native ownership journal anchor changed")
		}
		for _, required := range expected.NativeIntentPaths {
			found := false
			for _, reference := range actual.NativeIntentPaths {
				found = found || reference == required
			}
			if !found {
				return actual, true, fmt.Errorf("retained native ownership reference is missing")
			}
		}
	}
	return actual, true, nil
}

func nativeTemplateDeleteEntry(command string) bool {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 4 || args[0] != "ecctl" || (args[1] != "sandbox" && args[1] != "sbx") || (args[2] != "template" && args[2] != "tpl") || (args[3] != "delete" && args[3] != "rm" && args[3] != "remove") {
		return false
	}
	flags := pflag.NewFlagSet("journal native Template", pflag.ContinueOnError)
	for _, key := range []string{"region", "profile", "team-id", "lang", "output"} {
		flags.String(key, "", "")
	}
	for _, key := range []string{"openapi", "json", "agent-envelope", "no-color"} {
		flags.Bool(key, false, "")
	}
	if flags.Parse(args[4:]) != nil {
		return true
	}
	native, _ := flags.GetBool("openapi")
	return native
}

// IsNativeTemplateDelete classifies the native finalizer for credential
// selection after the shared Template ledger preflight succeeds.
func IsNativeTemplateDelete(command string) bool { return nativeTemplateDeleteEntry(command) }

func sameTemplateDelete(left, right string) bool {
	a, ea := shlex.Split(left)
	b, eb := shlex.Split(right)
	if ea != nil || eb != nil || len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// IsTemplateCacheDelete identifies the child finalizer that must complete
// before an ordered cleanup pass can delete its native Template and Team.
func IsTemplateCacheDelete(command string) bool {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 3 || args[0] != "ecctl" {
		return false
	}
	for index := 2; index < len(args); index++ {
		if (args[index-1] == "sandbox" || args[index-1] == "sbx") && (args[index] == "template-cache" || args[index] == "tcache") {
			return true
		}
	}
	return false
}

// ValidateOwnedTemplateCacheDelete binds a cache delete to the ID of a retained
// native Template. Reservation and replay use the same validation boundary.
func ValidateOwnedTemplateCacheDelete(command string, intent TemplateCreateIntent) error {
	if err := journalcmd.ValidateDelete(command); err != nil {
		return err
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) < 6 || args[0] != "ecctl" || (args[1] != "sandbox" && args[1] != "sbx") || (args[2] != "template-cache" && args[2] != "tcache") || args[3] != "delete" || !safeTemplateID(args[4]) {
		return fmt.Errorf("Template Cache recovery requires one exact delete")
	}
	values := map[string]string{}
	for index := 5; index < len(args); index++ {
		key, value, inline := strings.Cut(args[index], "=")
		if _, exists := values[key]; exists {
			return fmt.Errorf("duplicate Template Cache recovery flag %s", key)
		}
		switch key {
		case "--team-id", "--region", "--profile", "--output", "--timeout":
			if !inline {
				index++
				if index >= len(args) {
					return fmt.Errorf("missing Template Cache recovery flag value")
				}
				value = args[index]
			}
		default:
			return fmt.Errorf("unsupported Template Cache recovery flag %s", key)
		}
		values[key] = value
	}
	if values["--team-id"] != intent.TeamID || values["--region"] != intent.Region || values["--profile"] != intent.Profile || values["--output"] != "json" {
		return fmt.Errorf("Template Cache recovery changed Team, region, profile or output")
	}
	if timeoutValue, explicit := values["--timeout"]; explicit {
		if timeout, err := time.ParseDuration(timeoutValue); err != nil || timeout <= 0 {
			return fmt.Errorf("Template Cache recovery requires a positive timeout")
		}
	}
	for _, owned := range intent.Deletes {
		if intent.ValidateDelete(owned) != nil {
			continue
		}
		parts, err := shlex.Split(owned)
		if err == nil && len(parts) >= 5 && parts[4] == args[4] {
			return nil
		}
	}
	return fmt.Errorf("Template Cache delete target lacks retained Template ownership")
}

func validateTemplateLedger(path string, meta report.CleanupJournal, intents []TemplateCreateIntent, seen map[string]bool, allowPendingCache bool) error {
	if err := CheckJournalPath(path, meta); err != nil {
		return err
	}
	if err := CheckQuotaUpdatePending(meta); err != nil {
		return err
	}
	if len(meta.NativeIntentPaths) > 0 && meta.NativeJournalPath != path {
		return fmt.Errorf("native ownership journal anchor missing or changed")
	}
	for _, reference := range meta.NativeIntentPaths {
		if !filepath.IsAbs(reference) || !seen[reference] {
			return fmt.Errorf("native creation ownership reference is missing")
		}
	}
	for _, entry := range meta.Entries {
		if entry.CacheCreatePending && !IsTemplateCacheDelete(entry.Teardown) {
			return fmt.Errorf("cache creation pending marker is not a Template Cache delete")
		}
		if IsTemplateCacheDelete(entry.Teardown) {
			proven := false
			for _, intent := range intents {
				if intent.State == "journaled" && ValidateOwnedTemplateCacheDelete(entry.Teardown, intent) == nil {
					proven = true
					break
				}
			}
			if !proven || len(meta.NativeIntentPaths) == 0 {
				return fmt.Errorf("Template Cache deletion lacks exact creation ownership evidence")
			}
			if entry.CacheCreatePending && !allowPendingCache {
				return ErrTemplateCacheCreatePending
			}
			continue
		}
		if !nativeTemplateDeleteEntry(entry.Teardown) {
			continue
		}
		proven := false
		for _, intent := range intents {
			if intent.State != "journaled" {
				continue
			}
			for _, command := range intent.Deletes {
				if sameTemplateDelete(command, entry.Teardown) && intent.ValidateDelete(entry.Teardown) == nil {
					proven = true
				}
			}
		}
		if !proven || len(meta.NativeIntentPaths) == 0 {
			return fmt.Errorf("native Template deletion lacks exact creation ownership evidence")
		}
	}
	return nil
}

// PendingTemplateCacheCreate inspects one pending finalizer without making it
// replayable. All native ownership checks still apply; only reconciliation may
// inspect a pending marker before proving the cache's terminal cloud state.
func PendingTemplateCacheCreate(path string, meta report.CleanupJournal, env []string) (TemplateCreateIntent, report.Resource, string, error) {
	resolved, err := ResolveJournalPath(path)
	if err != nil {
		return TemplateCreateIntent{}, report.Resource{}, "", err
	}
	path = resolved
	intents, err := readTemplateCreateIntentsForRecovery(path, meta, env)
	if err != nil {
		return TemplateCreateIntent{}, report.Resource{}, "", err
	}
	var pending report.Resource
	count := 0
	for _, entry := range meta.Entries {
		if !entry.CacheCreatePending {
			continue
		}
		if entry.RegionRole != meta.RegionRole || entry.Region != meta.Region || entry.ExecutionID != meta.ExecutionID {
			return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("pending Template Cache finalizer binding changed")
		}
		pending = entry
		count++
	}
	if count != 1 {
		return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("reconciliation requires exactly one pending Template Cache finalizer")
	}
	args, err := shlex.Split(pending.Teardown)
	if err != nil || len(args) < 5 {
		return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("pending Template Cache finalizer is invalid")
	}
	var owned TemplateCreateIntent
	matches := 0
	for _, intent := range intents {
		if intent.State == "journaled" && ValidateOwnedTemplateCacheDelete(pending.Teardown, intent) == nil {
			ownerPath := path + ".template-create-" + strings.TrimPrefix(intent.Name, "ecctl-p-") + ".json"
			referenced := false
			for _, nativePath := range meta.NativeIntentPaths {
				referenced = referenced || nativePath == ownerPath
			}
			if !referenced {
				return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("pending Template Cache owner is not referenced by the journal")
			}
			owned = intent
			matches++
		}
	}
	if matches != 1 {
		return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("pending Template Cache finalizer lacks one exact native Template owner")
	}
	for _, intent := range intents {
		if intent.State == "journaled" && intent.Identity != owned.Identity {
			return TemplateCreateIntent{}, report.Resource{}, "", fmt.Errorf("native Template journal contains different credential principals")
		}
	}
	return owned, pending, args[4], nil
}
