package journalfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

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

func loadTemplateLedger(path string, expected report.CleanupJournal) (report.CleanupJournal, error) {
	if expected.NativeJournalPath != "" && path != expected.NativeJournalPath {
		return report.CleanupJournal{}, fmt.Errorf("retained native ownership journal target changed")
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if expected.NativeJournalPath != "" {
			return report.CleanupJournal{}, fmt.Errorf("required native ownership journal is missing")
		}
		return expected, nil
	}
	if err != nil {
		return report.CleanupJournal{}, err
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		if expected.NativeJournalPath != "" {
			return report.CleanupJournal{}, fmt.Errorf("required native ownership metadata is missing")
		}
		return expected, nil
	}
	var actual report.CleanupJournal
	if json.Unmarshal(body, &actual) != nil {
		return actual, fmt.Errorf("invalid cleanup journal metadata")
	}
	for _, pair := range [][2]string{{actual.RunID, expected.RunID}, {actual.ExecutionID, expected.ExecutionID}, {actual.RegionRole, expected.RegionRole}, {actual.Region, expected.Region}, {actual.Surface, expected.Surface}, {actual.EcctlBin, expected.EcctlBin}} {
		if pair[1] != "" && pair[0] != pair[1] {
			return actual, fmt.Errorf("cleanup journal binding changed")
		}
	}
	if actual.NativeJournalPath != "" && actual.NativeJournalPath != path {
		return actual, fmt.Errorf("native ownership journal path changed")
	}
	if expected.NativeJournalPath != "" {
		if actual.NativeJournalPath != expected.NativeJournalPath {
			return actual, fmt.Errorf("retained native ownership journal anchor changed")
		}
		for _, required := range expected.NativeIntentPaths {
			found := false
			for _, reference := range actual.NativeIntentPaths {
				found = found || reference == required
			}
			if !found {
				return actual, fmt.Errorf("retained native ownership reference is missing")
			}
		}
	}
	return actual, nil
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

func validateTemplateLedger(path string, meta report.CleanupJournal, intents []TemplateCreateIntent, seen map[string]bool) error {
	if len(meta.NativeIntentPaths) > 0 && meta.NativeJournalPath != path {
		return fmt.Errorf("native ownership journal anchor missing or changed")
	}
	for _, reference := range meta.NativeIntentPaths {
		if !filepath.IsAbs(reference) || !seen[reference] {
			return fmt.Errorf("native creation ownership reference is missing")
		}
	}
	for _, entry := range meta.Entries {
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
