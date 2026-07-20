package sweeper

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

var allowedNonSweepableReasons = map[string]bool{
	"provider-no-list":    true,
	"provider-no-delete":  true,
	"unsafe-delete":       true,
	"shared-fixture-only": true,
	"provider-limitation": true,
}

var commandVerbs = map[string]bool{
	"apply":     true,
	"attach":    true,
	"authorize": true,
	"create":    true,
	"delete":    true,
	"detach":    true,
	"get":       true,
	"list":      true,
	"reboot":    true,
	"remove":    true,
	"revoke":    true,
	"start":     true,
	"stop":      true,
	"update":    true,
}

type CheckOptions struct {
	CasesDir   string
	ConfigFile string
}

type CheckReport struct {
	Cases       int                    `json:"cases"`
	SweepKinds  int                    `json:"sweep_kinds"`
	LiveCreates int                    `json:"live_creates"`
	Invalid     int                    `json:"invalid"`
	Errors      []CheckValidationError `json:"errors"`
}

type CheckValidationError struct {
	Path     string `json:"path,omitempty"`
	Step     string `json:"step,omitempty"`
	Resource string `json:"resource,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func CheckConfig(opt CheckOptions) (*CheckReport, error) {
	cfg, err := loadConfig(opt.ConfigFile)
	if err != nil {
		return nil, err
	}
	suites, err := scenario.LoadDir(opt.CasesDir)
	if err != nil {
		return nil, err
	}

	rep := &CheckReport{Cases: len(suites), SweepKinds: len(cfg.Kinds)}
	sweepByResource := map[string]Kind{}
	for _, kind := range cfg.Kinds {
		if resource := validateSweepKind(rep, kind); resource != "" {
			sweepByResource[resource] = kind
		}
	}
	nonSweepable := map[string]NonSweepableKind{}
	for _, entry := range cfg.NonSweepable {
		if resource := validateNonSweepable(rep, entry); resource != "" {
			nonSweepable[resource] = entry
		}
	}

	for _, suite := range suites {
		for _, st := range suite.Steps {
			resource, verb := commandResourceVerb(st.Run)
			if resource == "" || verb != "create" {
				continue
			}
			rep.LiveCreates++
			if strings.TrimSpace(st.Teardown) == "" {
				rep.add(suite.Path, st.Name, resource, "missing_teardown", "create step requires teardown")
			}
			if _, ok := sweepByResource[resource]; !ok {
				if _, exempt := nonSweepable[resource]; !exempt {
					rep.add(suite.Path, st.Name, resource, "missing_sweep_kind", "created resource has no matching sweep kind or non-sweepable reason")
				}
			}
		}
	}
	rep.Invalid = len(rep.Errors)
	return rep, nil
}

func validateSweepKind(rep *CheckReport, kind Kind) string {
	checks := []struct {
		field string
		value string
	}{
		{"name", kind.Name},
		{"items_path", kind.ItemsPath},
		{"id_field", kind.IDField},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.value) == "" {
			rep.add("", "", kind.Name, "missing_field", fmt.Sprintf("sweep kind requires %s", c.field))
		}
	}
	if strings.TrimSpace(kind.List) == "" || !strings.Contains(kind.List, "tag.ecctl-e2e=1") {
		rep.add("", "", kind.Name, "missing_list_selector", "sweep list command must filter tag.ecctl-e2e=1")
	}
	if strings.TrimSpace(kind.RunIDField) == "" {
		rep.add("", "", kind.Name, "missing_run_id_selector", "sweep kind requires runid_field")
	}
	if strings.TrimSpace(kind.CreatedField) == "" {
		rep.add("", "", kind.Name, "missing_created_marker", "sweep kind requires created_field")
	}
	if strings.TrimSpace(kind.Delete) == "" || !strings.Contains(kind.Delete, "{{.id}}") {
		rep.add("", "", kind.Name, "missing_delete_command", "sweep delete command must reference {{.id}}")
	}

	explicit := strings.TrimSpace(kind.Resource)
	inferred, verb := commandResourceVerb(kind.List)
	if kind.List != "" && (inferred == "" || verb != "list") {
		rep.add("", "", kind.Name, "missing_list_selector", "sweep list command must be an ecctl list command")
	}

	resource := inferred
	if explicit != "" {
		resource = explicit
		if inferred != "" && inferred != explicit {
			rep.add("", "", explicit, "resource_mismatch", fmt.Sprintf("sweep kind resource %s does not match list command resource %s", explicit, inferred))
		}
	}
	if resource == "" {
		rep.add("", "", kind.Name, "missing_resource", "sweep kind must declare resource or use a parseable list command")
	}
	return resource
}

func validateNonSweepable(rep *CheckReport, entry NonSweepableKind) string {
	resource := strings.TrimSpace(entry.Resource)
	if resource == "" {
		rep.add("", "", "", "missing_resource", "non-sweepable entry requires resource")
		return ""
	}
	if !allowedNonSweepableReasons[entry.Reason] {
		rep.add("", "", resource, "invalid_non_sweepable_reason", fmt.Sprintf("non-sweepable reason %q is not allowed", entry.Reason))
	}
	if entry.ReviewAfter == "" {
		rep.add("", "", resource, "missing_review_after", "non-sweepable entry requires review_after")
	} else if _, err := time.Parse("2006-01-02", entry.ReviewAfter); err != nil {
		rep.add("", "", resource, "invalid_review_after", "review_after must use YYYY-MM-DD")
	}
	return resource
}

func commandResourceVerb(run string) (resource, verb string) {
	toks, err := shlex.Split(run)
	if err != nil || len(toks) == 0 || toks[0] != "ecctl" {
		return "", ""
	}
	var pos []string
	for _, tok := range toks[1:] {
		if strings.HasPrefix(tok, "-") {
			break
		}
		pos = append(pos, tok)
	}
	if len(pos) < 2 || pos[0] == "call" {
		return "", ""
	}

	product := pos[0]
	for i := 1; i < len(pos); i++ {
		if !commandVerbs[pos[i]] {
			continue
		}
		if i == 1 {
			return product + "/" + product, pos[i]
		}
		return product + "/" + strings.Join(pos[1:i], "-"), pos[i]
	}
	return "", ""
}

func (r *CheckReport) add(path, step, resource, code, msg string) {
	r.Errors = append(r.Errors, CheckValidationError{
		Path: path, Step: step, Resource: resource, Code: code, Message: msg,
	})
}
