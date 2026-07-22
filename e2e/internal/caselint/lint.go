// Package caselint validates cross-file invariants for E2E cases without
// contacting Alibaba Cloud.
package caselint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/google/shlex"
	"gopkg.in/yaml.v3"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/coverage"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
	paramspkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/params"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

type Options struct {
	CasesDir        string
	InputsDir       string
	CoveragePath    string
	StackFile       string
	GlobalFixture   string
	ParameterPolicy string
}

type Report struct {
	Cases   int               `json:"cases"`
	Steps   int               `json:"steps"`
	Invalid int               `json:"invalid"`
	Errors  []ValidationError `json:"errors"`
}

type ValidationError struct {
	Path    string `json:"path,omitempty"`
	Step    string `json:"step,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type templateRef struct {
	Root string
	Path []string
}

type commandInfo struct {
	Positionals []string
	Call        bool
	Valid       bool
}

type stackContract struct {
	Tags      map[string]string `yaml:"tags"`
	Provision []stackStep       `yaml:"provision"`
}

type stackStep struct {
	ID                    string            `yaml:"id"`
	Needs                 []string          `yaml:"needs"`
	RequiresParams        []string          `yaml:"requires_params"`
	RequiresPrerequisites []string          `yaml:"requires_prerequisites"`
	Run                   string            `yaml:"run"`
	At                    string            `yaml:"at"`
	Capture               map[string]string `yaml:"capture"`
	Teardown              string            `yaml:"teardown"`
}

type stackIndex struct {
	byID            map[string]stackStep
	captureProvider map[string]string
}

func Check(opts Options) (*Report, error) {
	rep := &Report{}
	suites, err := scenario.LoadDir(opts.CasesDir)
	if err != nil {
		return nil, err
	}
	resources, err := loadCoverageResources(opts.CoveragePath)
	if err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		resources = suiteResources(suites)
	}
	stack, err := loadStackContract(opts.StackFile)
	if err != nil {
		return nil, err
	}
	stackResources := indexStack(rep, opts.StackFile, stack)
	needsGlobal := suitesRequireGlobal(suites)
	needsParams := suitesRequireParams(suites) || stackRequiresParams(stack)
	var global *fixtureconfig.Config
	if opts.GlobalFixture != "" && needsGlobal {
		global, err = fixtureconfig.Load(opts.GlobalFixture)
		if err != nil {
			return nil, err
		}
	}
	var parameterPolicy *paramspkg.Policy
	if opts.ParameterPolicy != "" && needsParams {
		loaded, err := paramspkg.LoadPolicy(opts.ParameterPolicy)
		if err != nil {
			return nil, err
		}
		if err := loaded.ValidateFor(parameterRequirements(suites, stack)); err != nil {
			return nil, fmt.Errorf("validate parameter policy: %w", err)
		}
		parameterPolicy = &loaded
	}
	for _, suite := range suites {
		rep.Cases++
		rep.Steps += len(suite.Steps)
		checkSuite(rep, suite, opts.InputsDir, resources, global, parameterPolicy, stackResources)
	}
	checkStackRequirements(rep, opts.StackFile, stack, parameterPolicy)
	if opts.CoveragePath != "" {
		if err := checkCoverageCases(rep, suites, opts); err != nil {
			return nil, err
		}
	}
	rep.Invalid = len(rep.Errors)
	return rep, nil
}

func indexStack(rep *Report, path string, stack stackContract) stackIndex {
	index := stackIndex{byID: map[string]stackStep{}, captureProvider: map[string]string{}}
	for _, step := range stack.Provision {
		if step.ID == "" {
			rep.add(path, "(stack)", "missing_stack_id", "stack provision step is missing id")
			continue
		}
		if _, exists := index.byID[step.ID]; exists {
			rep.add(path, step.ID, "duplicate_stack_id", fmt.Sprintf("stack provision id %q is declared more than once", step.ID))
			continue
		}
		index.byID[step.ID] = step
		for capture := range step.Capture {
			if previous, exists := index.captureProvider[capture]; exists {
				rep.add(path, step.ID, "duplicate_stack_capture", fmt.Sprintf("stack capture %q is provided by both %q and %q", capture, previous, step.ID))
				continue
			}
			index.captureProvider[capture] = step.ID
		}
	}

	for _, step := range index.byID {
		for _, dependency := range step.Needs {
			if _, exists := index.byID[dependency]; !exists {
				rep.add(path, step.ID, "unknown_stack_dependency", fmt.Sprintf("stack node %q depends on unknown node %q", step.ID, dependency))
			}
		}
	}
	state := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		switch state[id] {
		case 2:
			return
		case 1:
			rep.add(path, id, "stack_dependency_cycle", fmt.Sprintf("stack dependency cycle at %q", id))
			return
		}
		state[id] = 1
		for _, dependency := range index.byID[id].Needs {
			if _, exists := index.byID[dependency]; exists {
				visit(dependency)
			}
		}
		state[id] = 2
	}
	for id := range index.byID {
		visit(id)
	}
	return index
}

func (index stackIndex) capturesFor(needs []string) map[string]bool {
	selected := map[string]bool{}
	var selectNode func(string)
	selectNode = func(id string) {
		if selected[id] {
			return
		}
		step, exists := index.byID[id]
		if !exists {
			return
		}
		selected[id] = true
		for _, dependency := range step.Needs {
			selectNode(dependency)
		}
	}
	for _, need := range needs {
		selectNode(need)
	}
	captures := map[string]bool{}
	for id := range selected {
		for capture := range index.byID[id].Capture {
			captures[capture] = true
		}
	}
	return captures
}

func (index stackIndex) prerequisitesFor(needs []string) map[string]bool {
	selected := map[string]bool{}
	var selectNode func(string)
	selectNode = func(id string) {
		if selected[id] {
			return
		}
		step, exists := index.byID[id]
		if !exists {
			return
		}
		selected[id] = true
		for _, dependency := range step.Needs {
			selectNode(dependency)
		}
	}
	for _, need := range needs {
		selectNode(need)
	}
	prerequisites := map[string]bool{}
	for id := range selected {
		for _, requirement := range index.byID[id].RequiresPrerequisites {
			prerequisites[requirement] = true
		}
	}
	return prerequisites
}

func loadStackContract(path string) (stackContract, error) {
	if path == "" {
		return stackContract{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return stackContract{}, err
	}
	var contract stackContract
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&contract); err != nil {
		return stackContract{}, fmt.Errorf("%s: %w", path, err)
	}
	return contract, nil
}

func checkStackRequirements(rep *Report, path string, stack stackContract, policy *paramspkg.Policy) {
	defined := map[string]bool{"run_name": true, "run_id": true, "resource_prefix": true, "oss_export_prefix": true, "time": true, "region": true, "regions": true, "zone": true, "global": true, "params": true, "prerequisites": true, "stack": true}
	for _, step := range stack.Provision {
		declaredParams := make(map[string]bool, len(step.RequiresParams))
		for _, key := range step.RequiresParams {
			declaredParams[key] = true
			if policy == nil {
				rep.add(path, step.ID, "missing_parameter_policy", "stack node declares dynamic parameters but no parameter policy was supplied")
			} else if !policy.Supports(key) {
				rep.add(path, step.ID, "unknown_parameter_requirement", fmt.Sprintf("parameter policy does not support %q", key))
			}
		}
		declaredPrerequisites := make(map[string]bool, len(step.RequiresPrerequisites))
		for _, key := range step.RequiresPrerequisites {
			declaredPrerequisites[key] = true
		}
		checkStackTemplateRefs(rep, path, step.ID, "run", step.Run, defined, map[string]bool{}, nil, declaredParams, policy, declaredPrerequisites)
		for name := range step.Capture {
			defined[name] = true
		}
		checkStackTemplateRefs(rep, path, step.ID, "teardown", step.Teardown, defined, map[string]bool{}, nil, declaredParams, policy, declaredPrerequisites)
	}
}

func stackRequiresParams(stack stackContract) bool {
	for _, step := range stack.Provision {
		if len(step.RequiresParams) > 0 {
			return true
		}
	}
	return false
}

func parameterRequirements(suites []*scenario.Suite, stack stackContract) []string {
	seen := map[string]bool{}
	var requirements []string
	appendKeys := func(keys []string) {
		for _, key := range keys {
			if !seen[key] {
				seen[key] = true
				requirements = append(requirements, key)
			}
		}
	}
	for _, suite := range suites {
		appendKeys(suite.RequiresParams)
	}
	for _, step := range stack.Provision {
		appendKeys(step.RequiresParams)
	}
	return requirements
}

func checkStackTemplateRefs(rep *Report, path, step, field, text string, defined, declaredGlobal map[string]bool, global *fixtureconfig.Config, declaredParams map[string]bool, policy *paramspkg.Policy, declaredPrerequisites map[string]bool) {
	if strings.TrimSpace(text) == "" || !strings.Contains(text, "{{") {
		return
	}
	refs, err := templateRefs(text)
	if err != nil {
		rep.add(path, step, "template_parse", fmt.Sprintf("%s: %v", field, err))
		return
	}
	for _, ref := range refs {
		switch ref.Root {
		case "run_name", "run_id", "resource_prefix", "oss_export_prefix", "time", "region", "regions", "zone", "stack":
			continue
		case "prerequisites":
			checkPrerequisiteRef(rep, path, step, field, ref.Path, 1, declaredPrerequisites)
		case "global", "params":
			if len(ref.Path) < 2 {
				rep.add(path, step, "invalid_"+map[string]string{"global": "global", "params": "parameter"}[ref.Root]+"_reference", fmt.Sprintf("%s references .%s without a key", field, ref.Root))
				continue
			}
			key := strings.Join(ref.Path[1:], ".")
			declared := declaredGlobal
			codeMissing := "missing_global_requirement"
			unknown := "unknown_global_requirement"
			if ref.Root == "params" {
				declared = declaredParams
				codeMissing = "missing_parameter_requirement"
				unknown = "unknown_parameter_requirement"
			}
			if !declared[key] {
				rep.add(path, step, codeMissing, fmt.Sprintf("%s references %s key %q without stack requirement", field, ref.Root, key))
				continue
			}
			if ref.Root == "global" && global != nil && !global.Has(key) {
				rep.add(path, step, unknown, fmt.Sprintf("global fixture does not declare %q", key))
			}
			if ref.Root == "params" && policy != nil && !policy.Supports(key) {
				rep.add(path, step, unknown, fmt.Sprintf("parameter policy does not support %q", key))
			}
		default:
			if !defined[ref.Root] {
				rep.add(path, step, "undefined_stack_var", fmt.Sprintf("%s references %q before it is captured", field, ref.Root))
			}
		}
	}
}

func suitesRequireGlobal(suites []*scenario.Suite) bool {
	for _, suite := range suites {
		if len(suite.RequiresGlobal) > 0 {
			return true
		}
	}
	return false
}

func suitesRequireParams(suites []*scenario.Suite) bool {
	for _, suite := range suites {
		if len(suite.RequiresParams) > 0 {
			return true
		}
	}
	return false
}

func checkSuite(rep *Report, suite *scenario.Suite, inputsDir string, resources map[string]bool, global *fixtureconfig.Config, parameterPolicy *paramspkg.Policy, stack stackIndex) {
	inputs, err := loadInputKeys(inputsDir, suite.Resource)
	if err != nil {
		rep.add(suite.Path, "", "invalid_inputs", err.Error())
		return
	}
	declaredGlobal := map[string]bool{}
	for _, key := range suite.RequiresGlobal {
		declaredGlobal[key] = true
		if global == nil {
			rep.add(suite.Path, "", "missing_global_fixture", "case declares global requirements but no global fixture was supplied")
			continue
		}
		if !global.Has(key) {
			rep.add(suite.Path, "", "unknown_global_requirement", fmt.Sprintf("global fixture does not declare %q", key))
		}
	}
	declaredParams := map[string]bool{}
	for _, key := range suite.RequiresParams {
		declaredParams[key] = true
		if parameterPolicy == nil {
			rep.add(suite.Path, "", "missing_parameter_policy", "case declares dynamic parameters but no parameter policy was supplied")
			continue
		}
		if !parameterPolicy.Supports(key) {
			rep.add(suite.Path, "", "unknown_parameter_requirement", fmt.Sprintf("parameter policy does not support %q", key))
		}
	}
	for _, need := range suite.Needs {
		if _, exists := stack.byID[need]; !exists {
			rep.add(suite.Path, "", "unknown_stack_need", fmt.Sprintf("case needs unknown stack node %q", need))
		}
	}
	stackCaptures := stack.capturesFor(suite.Needs)
	primaryPrerequisites := stack.prerequisitesFor(suite.Needs)
	for _, requirement := range suite.RequiresPrerequisites {
		primaryPrerequisites[requirement] = true
	}
	regionPrerequisites := map[string]map[string]bool{"primary": primaryPrerequisites}
	for role, requirement := range suite.RegionRequirements {
		declared := map[string]bool{}
		for _, name := range requirement.RequiresPrerequisites {
			declared[name] = true
		}
		regionPrerequisites[role] = declared
	}
	defined := map[string]bool{}
	for _, st := range suite.Steps {
		info := checkCommandShape(rep, suite, st, resources)
		checkTemplateRefs(rep, suite.Path, st.Name, "run", st.Run, inputs, defined, declaredGlobal, global, declaredParams, parameterPolicy, stackCaptures, regionPrerequisites)
		_, verb := commandResourceVerb(info.Positionals, resources)
		if info.Valid && !info.Call && verb == "create" {
			checkCreateTags(rep, suite.Path, st.Name, st.Run)
			if strings.TrimSpace(st.Teardown) == "" {
				rep.add(suite.Path, st.Name, "missing_teardown", "create command requires teardown")
			}
		}
		for _, pm := range st.Expect {
			for _, text := range matcherStrings(pm.Matcher) {
				checkTemplateRefs(rep, suite.Path, st.Name, "expect", text, inputs, defined, declaredGlobal, global, declaredParams, parameterPolicy, stackCaptures, regionPrerequisites)
			}
		}
		for _, assert := range st.Assert {
			checkTemplateRefs(rep, suite.Path, st.Name, "assert", assert, inputs, defined, declaredGlobal, global, declaredParams, parameterPolicy, stackCaptures, regionPrerequisites)
		}
		teardownDefined := cloneSet(defined)
		for name := range st.Capture {
			teardownDefined[name] = true
		}
		checkTemplateRefs(rep, suite.Path, st.Name, "teardown", st.Teardown, inputs, teardownDefined, declaredGlobal, global, declaredParams, parameterPolicy, stackCaptures, regionPrerequisites)
		for name := range st.Capture {
			if defined[name] {
				rep.add(suite.Path, st.Name, "duplicate_capture", fmt.Sprintf("capture %q is already defined", name))
				continue
			}
			defined[name] = true
		}
	}
}

func checkCoverageCases(rep *Report, suites []*scenario.Suite, opts Options) error {
	reg, err := coverage.LoadRegistryFile(opts.CoveragePath)
	if err != nil {
		return err
	}
	resources := map[string]bool{}
	for resource := range reg.Resources {
		resources[resource] = true
	}
	caseCapabilities := map[string]map[coverage.Capability]bool{}
	for _, suite := range suites {
		resolved := normalizePath(suite.Path, "")
		caseCapabilities[resolved] = map[coverage.Capability]bool{}
		for _, st := range suite.Steps {
			info := parseCommand(st.Run)
			if !info.Valid || info.Call {
				continue
			}
			resource, operation := commandResourceVerb(info.Positionals, resources)
			if resource != "" && operation != "" {
				caseCapabilities[resolved][coverage.Capability{Resource: resource, Verb: operation}] = true
			}
		}
	}
	for _, resource := range sortedCoverageResources(reg) {
		rr := reg.Resources[resource]
		for _, opName := range sortedCoverageOps(rr) {
			op := rr.Operations[opName]
			resolved := normalizePath(op.Case, filepath.Dir(opts.CasesDir))
			capabilities, loaded := caseCapabilities[resolved]
			if !loaded {
				rep.add(opts.CoveragePath, resource+"/"+opName, "coverage_case_missing", fmt.Sprintf("case %s is not loaded from cases directory", op.Case))
				continue
			}
			capability := coverage.Capability{Resource: resource, Verb: opName}
			if !capabilities[capability] {
				rep.add(opts.CoveragePath, resource+"/"+opName, "coverage_operation_missing", fmt.Sprintf("case %s does not run %s %s", op.Case, resource, opName))
			}
		}
	}
	return nil
}

func checkTemplateRefs(rep *Report, path, step, field, text string, inputs, defined, declaredGlobal map[string]bool, global *fixtureconfig.Config, declaredParams map[string]bool, parameterPolicy *paramspkg.Policy, stackCaptures map[string]bool, regionPrerequisites map[string]map[string]bool) {
	if !strings.Contains(text, "{{") {
		return
	}
	refs, err := templateRefs(text)
	if err != nil {
		rep.add(path, step, "template_parse", fmt.Sprintf("%s: %v", field, err))
		return
	}
	for _, ref := range refs {
		switch ref.Root {
		case "run_name", "run_id", "resource_prefix", "oss_export_prefix", "time", "region", "zone":
			continue
		case "prerequisites":
			checkPrerequisiteRef(rep, path, step, field, ref.Path, 1, regionPrerequisites["primary"])
		case "regions":
			if len(ref.Path) < 3 {
				rep.add(path, step, "invalid_region_reference", fmt.Sprintf("%s references .regions without a role field", field))
				continue
			}
			role := ref.Path[1]
			declared, exists := regionPrerequisites[role]
			if !exists {
				rep.add(path, step, "unknown_region_role", fmt.Sprintf("%s references undeclared region role %q", field, role))
				continue
			}
			if ref.Path[2] == "id" && len(ref.Path) == 3 {
				continue
			}
			if ref.Path[2] != "prerequisites" {
				rep.add(path, step, "invalid_region_reference", fmt.Sprintf("%s references unsupported region field %q", field, strings.Join(ref.Path, ".")))
				continue
			}
			checkPrerequisiteRef(rep, path, step, field, ref.Path, 3, declared)
		case "stack":
			if len(ref.Path) < 2 {
				rep.add(path, step, "invalid_stack_reference", fmt.Sprintf("%s references .stack without a capture", field))
				continue
			}
			capture := ref.Path[1]
			if !stackCaptures[capture] {
				rep.add(path, step, "missing_stack_need", fmt.Sprintf("%s references stack capture %q outside the case needs closure", field, capture))
			}
		case "inputs":
			if len(ref.Path) < 2 || !inputs[ref.Path[1]] {
				rep.add(path, step, "missing_input", fmt.Sprintf("%s references missing input %q", field, strings.Join(ref.Path, ".")))
			}
		case "global":
			if len(ref.Path) < 2 {
				rep.add(path, step, "invalid_global_reference", fmt.Sprintf("%s references .global without a key", field))
				continue
			}
			key := strings.Join(ref.Path[1:], ".")
			if !declaredGlobal[key] {
				rep.add(path, step, "missing_global_requirement", fmt.Sprintf("%s references global key %q without requires_global", field, key))
				continue
			}
			if global != nil && !global.Has(key) {
				rep.add(path, step, "unknown_global_requirement", fmt.Sprintf("global fixture does not declare %q", key))
			}
		case "params":
			if len(ref.Path) < 2 {
				rep.add(path, step, "invalid_parameter_reference", fmt.Sprintf("%s references .params without a key", field))
				continue
			}
			key := strings.Join(ref.Path[1:], ".")
			if !declaredParams[key] {
				rep.add(path, step, "missing_parameter_requirement", fmt.Sprintf("%s references parameter %q without requires_params", field, key))
				continue
			}
			if parameterPolicy != nil && !parameterPolicy.Supports(key) {
				rep.add(path, step, "unknown_parameter_requirement", fmt.Sprintf("parameter policy does not support %q", key))
			}
		default:
			if !defined[ref.Root] {
				rep.add(path, step, "undefined_var", fmt.Sprintf("%s references %q before it is captured", field, ref.Root))
			}
		}
	}
}

func checkPrerequisiteRef(rep *Report, path, step, field string, refPath []string, offset int, declared map[string]bool) {
	if len(refPath) < offset+3 {
		rep.add(path, step, "invalid_prerequisite_reference", fmt.Sprintf("%s references prerequisite without bundle and field", field))
		return
	}
	bundle := refPath[offset] + "." + refPath[offset+1]
	if !declared[bundle] {
		rep.add(path, step, "missing_prerequisite_requirement", fmt.Sprintf("%s references prerequisite bundle %q without requires_prerequisites", field, bundle))
	}
}

func checkCreateTags(rep *Report, path, step, run string) {
	info := parseCommand(run)
	resource, _ := commandResourceVerb(info.Positionals, nil)
	// RAM roles and Resource Manager policies do not expose tag fields in their
	// APIs. They are still cleaned through the case journal, so requiring sweep
	// tags here would reject valid, untaggable prerequisites.
	if resource == "rg/role" || resource == "rg/policy" || resource == "ack/kubeconfig" || resource == "ack/nodepool" {
		return
	}
	tags := commandTags(run)
	if !tags["ecctl-e2e=1"] || !tags["run-id={{.run_id}}"] {
		rep.add(path, step, "missing_run_tag", "create command requires --tag ecctl-e2e=1 and --tag run-id={{.run_id}}")
	}
}

func checkCommandShape(rep *Report, suite *scenario.Suite, st scenario.Step, resources map[string]bool) commandInfo {
	info := parseCommand(st.Run)
	if !info.Valid {
		rep.add(suite.Path, st.Name, "invalid_command_shape", "run must be a parseable ecctl command")
		return info
	}
	if info.Call {
		return info
	}
	if len(info.Positionals) < 2 {
		rep.add(suite.Path, st.Name, "invalid_command_shape", "resource command requires product and action")
		return info
	}
	resource := commandResource(info.Positionals, resources)
	if resource == "" {
		rep.add(suite.Path, st.Name, "invalid_command_shape", "resource command requires product and resource")
		return info
	}
	if len(resources) > 0 {
		if _, ok := resources[resource]; !ok {
			rep.add(suite.Path, st.Name, "invalid_command_shape", fmt.Sprintf("command resource %s is not declared in coverage registry", resource))
		}
	}
	return info
}

func parseCommand(run string) commandInfo {
	toks, err := shlex.Split(run)
	if err != nil || len(toks) == 0 || toks[0] != "ecctl" {
		return commandInfo{}
	}
	info := commandInfo{Valid: true}
	for _, tok := range toks[1:] {
		if strings.HasPrefix(tok, "-") {
			break
		}
		info.Positionals = append(info.Positionals, tok)
	}
	if len(info.Positionals) > 0 && info.Positionals[0] == "call" {
		info.Call = true
	}
	return info
}

func loadCoverageResources(path string) (map[string]bool, error) {
	out := map[string]bool{}
	if path == "" {
		return out, nil
	}
	reg, err := coverage.LoadRegistryFile(path)
	if err != nil {
		return nil, err
	}
	for resource := range reg.Resources {
		out[resource] = true
	}
	return out, nil
}

func suiteResources(suites []*scenario.Suite) map[string]bool {
	out := map[string]bool{}
	for _, suite := range suites {
		if suite.Resource != "" {
			out[suite.Resource] = true
		}
		for _, st := range suite.Steps {
			info := parseCommand(st.Run)
			if !info.Valid || info.Call {
				continue
			}
			resource, _ := commandResourceVerb(info.Positionals, nil)
			if resource != "" {
				out[resource] = true
			}
		}
	}
	return out
}

func commandResource(pos []string, resources map[string]bool) string {
	resource, _ := commandResourceVerb(pos, resources)
	return resource
}

func commandResourceVerb(pos []string, resources map[string]bool) (resource, verb string) {
	if len(pos) < 2 {
		return "", ""
	}
	product := pos[0]
	if len(resources) > 0 {
		if len(pos) >= 4 {
			nested := product + "/" + pos[2]
			if resources[nested] {
				return nested, pos[3]
			}
		}
		if len(pos) >= 3 {
			subresource := product + "/" + pos[1]
			if resources[subresource] {
				return subresource, pos[2]
			}
		}
		defaultResource := product + "/" + product
		if resources[defaultResource] {
			return defaultResource, pos[1]
		}
		return "", ""
	}
	if len(pos) >= 3 {
		return product + "/" + pos[1], pos[2]
	}
	return product + "/" + product, pos[1]
}

func templateRefs(text string) ([]templateRef, error) {
	t, err := template.New("lint").Parse(text)
	if err != nil {
		return nil, err
	}
	var refs []templateRef
	walkTemplate(t.Tree.Root, &refs)
	return refs, nil
}

func walkTemplate(node parse.Node, refs *[]templateRef) {
	switch n := node.(type) {
	case *parse.ListNode:
		for _, child := range n.Nodes {
			walkTemplate(child, refs)
		}
	case *parse.ActionNode:
		walkTemplate(n.Pipe, refs)
	case *parse.PipeNode:
		for _, cmd := range n.Cmds {
			walkTemplate(cmd, refs)
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			walkTemplate(arg, refs)
		}
	case *parse.FieldNode:
		if len(n.Ident) > 0 {
			*refs = append(*refs, templateRef{Root: n.Ident[0], Path: append([]string(nil), n.Ident...)})
		}
	case *parse.IfNode:
		walkTemplate(n.Pipe, refs)
		walkTemplate(n.List, refs)
		if n.ElseList != nil {
			walkTemplate(n.ElseList, refs)
		}
	case *parse.RangeNode:
		walkTemplate(n.Pipe, refs)
		walkTemplate(n.List, refs)
		if n.ElseList != nil {
			walkTemplate(n.ElseList, refs)
		}
	case *parse.WithNode:
		walkTemplate(n.Pipe, refs)
		walkTemplate(n.List, refs)
		if n.ElseList != nil {
			walkTemplate(n.ElseList, refs)
		}
	}
}

func matcherStrings(m scenario.Matcher) []string {
	var out []string
	addAnyString(&out, m.Eq)
	addAnyString(&out, m.Ne)
	if m.HasPrefix {
		out = append(out, m.Prefix)
	}
	if m.HasSuffix {
		out = append(out, m.Suffix)
	}
	if m.HasContains {
		out = append(out, m.Contains)
	}
	if m.Regex != "" {
		out = append(out, m.Regex)
	}
	for _, v := range m.OneOf {
		addAnyString(&out, v)
	}
	if m.Each != nil {
		out = append(out, matcherStrings(*m.Each)...)
	}
	return out
}

func addAnyString(out *[]string, v any) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, item := range x {
			addAnyString(out, item)
		}
	case map[string]any:
		for _, item := range x {
			addAnyString(out, item)
		}
	}
}

func loadInputKeys(dir, resource string) (map[string]bool, error) {
	out := map[string]bool{}
	name := strings.ReplaceAll(resource, "/", "-")
	for _, ext := range []string{".yaml", ".yml"} {
		path := filepath.Join(dir, name+ext)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var values map[string]any
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for k := range values {
			out[k] = true
		}
		return out, nil
	}
	return out, nil
}

func commandTags(run string) map[string]bool {
	toks, err := shlex.Split(run)
	if err != nil {
		return map[string]bool{}
	}
	tags := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok == "--tag" && i+1 < len(toks) {
			tags[toks[i+1]] = true
			i++
			continue
		}
		if strings.HasPrefix(tok, "--tag=") {
			tags[strings.TrimPrefix(tok, "--tag=")] = true
		}
	}
	return tags
}

func normalizePath(path, base string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && base != "" {
		path = filepath.Join(base, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func sortedCoverageResources(reg *coverage.Registry) []string {
	out := make([]string, 0, len(reg.Resources))
	for resource := range reg.Resources {
		out = append(out, resource)
	}
	sort.Strings(out)
	return out
}

func sortedCoverageOps(rr coverage.RegistryResource) []string {
	out := make([]string, 0, len(rr.Operations))
	for op := range rr.Operations {
		out = append(out, op)
	}
	sort.Strings(out)
	return out
}

func cloneSet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *Report) add(path, step, code, msg string) {
	r.Errors = append(r.Errors, ValidationError{Path: path, Step: step, Code: code, Message: msg})
}
