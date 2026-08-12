package sweeper

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
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

var canonicalResourceAliases = map[string]string{
	"ack/inspect-report":  "ack/report",
	"ack/policy-instance": "ack/instance",
	"rg/policy-version":   "rg/version",
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
	validateSweepDependencies(rep, cfg.Kinds)
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
			exemption, exempt := nonSweepable[resource]
			providerOwnsLifetime := exempt && (exemption.Reason == "provider-no-delete" || exemption.Reason == "shared-fixture-only")
			if strings.TrimSpace(st.Teardown) == "" && !providerOwnsLifetime {
				rep.add(suite.Path, st.Name, resource, "missing_teardown", "create step requires teardown")
			}
			if _, ok := sweepByResource[resource]; !ok {
				if !exempt {
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
	validateDeleteTemplate(rep, kind)

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

func validateDeleteTemplate(rep *CheckReport, kind Kind) {
	if strings.TrimSpace(kind.Delete) == "" {
		return
	}
	data := map[string]any{"id": "resource-id"}
	for name, path := range kind.DeleteFields {
		if strings.TrimSpace(name) == "" || name == "id" {
			rep.add("", "", kind.Name, "invalid_delete_field", fmt.Sprintf("delete field name %q is empty or reserved", name))
			continue
		}
		if strings.TrimSpace(path) == "" {
			rep.add("", "", kind.Name, "invalid_delete_field", fmt.Sprintf("delete field %q requires an item path", name))
			continue
		}
		data[name] = "value"
	}
	if _, err := vars.Render(kind.Delete, data); err != nil {
		rep.add("", "", kind.Name, "invalid_delete_template", fmt.Sprintf("sweep delete template references an unmapped field: %v", err))
	}
}

func validateSweepDependencies(rep *CheckReport, kinds []Kind) {
	positions := make(map[string]int, len(kinds))
	for i, kind := range kinds {
		name := strings.TrimSpace(kind.Name)
		if name == "" {
			continue
		}
		if _, exists := positions[name]; exists {
			rep.add("", "", name, "duplicate_sweep_kind", "sweep kind names must be unique")
			continue
		}
		positions[name] = i
	}
	for i, kind := range kinds {
		seen := map[string]bool{}
		for _, rawProvider := range kind.DependsOn {
			provider := strings.TrimSpace(rawProvider)
			if provider == "" || seen[provider] {
				rep.add("", "", kind.Name, "invalid_dependency", "depends_on entries must be non-empty and unique")
				continue
			}
			seen[provider] = true
			providerIndex, ok := positions[provider]
			if !ok {
				rep.add("", "", kind.Name, "invalid_dependency", fmt.Sprintf("depends_on provider %q is not a configured sweep kind", provider))
				continue
			}
			if providerIndex <= i {
				rep.add("", "", kind.Name, "invalid_dependency_order", fmt.Sprintf("consumer %q must be listed before provider %q", kind.Name, provider))
			}
		}
	}
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
			return canonicalSweepResource(product + "/" + product), pos[i]
		}
		return canonicalSweepResource(product + "/" + strings.Join(pos[1:i], "-")), pos[i]
	}
	return "", ""
}

func canonicalSweepResource(resource string) string {
	if canonical, ok := canonicalResourceAliases[resource]; ok {
		return canonical
	}
	return resource
}

func (r *CheckReport) add(path, step, resource, code, msg string) {
	r.Errors = append(r.Errors, CheckValidationError{
		Path: path, Step: step, Resource: resource, Code: code, Message: msg,
	})
}
