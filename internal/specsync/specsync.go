package specsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/internal/drift"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/pmezard/go-difflib/difflib"
)

type resourceFile struct {
	path     string
	original []byte
	lines    []string
	resource spec.ResourceSpec
}

type planResult struct {
	item     drift.Item
	action   string // patched, flagged, already-synced
	detail   string
	flagKind string
	flagText string
}

type syncRun struct {
	files      map[string]*resourceFile
	items      []drift.Item
	flags      []planResult
	plan       []planResult
	collisions map[string]bool
}

// Run reconciles missing OpenAPI request parameters from a specdrift JSON
// report into the resource specs under specDir. When dryRun is true it prints
// a unified diff and the structured plan without touching the specs; when
// false it writes the patched spec files in place. The structured per-item
// plan is written to planOut, when non-empty, before specs are modified.
func Run(driftPath, specDir string, dryRun bool, planOut string) error {
	report, err := loadReport(driftPath)
	if err != nil {
		return err
	}
	srun, err := planReport(report, specDir)
	if err != nil {
		return err
	}

	if dryRun {
		if planOut != "" {
			if err := srun.writePlan(planOut); err != nil {
				return err
			}
		}
		printFlags(srun.flags)
		return srun.printDryRun()
	}
	if err := srun.writeFiles(planOut); err != nil {
		return err
	}
	printFlags(srun.flags)
	printPlan(srun.plan)
	return nil
}

func planReport(report drift.Report, specDir string) (*syncRun, error) {
	items := make([]drift.Item, 0, len(report.Missing()))
	items = append(items, report.Missing()...)
	items = append(items, report.Removed()...)
	items = append(items, report.Uncovered()...)

	srun := &syncRun{
		files:      map[string]*resourceFile{},
		items:      items,
		collisions: normalizedFieldCollisions(report.Missing()),
	}

	// Plan every edit in report order before writing anything so an invalid
	// insertion cannot leave one file patched and another untouched.
	for _, item := range items {
		res, err := srun.planItem(specDir, item)
		if err != nil {
			return nil, err
		}
		srun.plan = append(srun.plan, res)
		if res.action == "flagged" {
			srun.flags = append(srun.flags, res)
		}
	}
	return srun, nil
}

func loadReport(path string) (drift.Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return drift.Report{}, fmt.Errorf("read drift report %s: %w", path, err)
	}
	var report drift.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return drift.Report{}, fmt.Errorf("parse drift report %s: %w", path, err)
	}
	if len(report.Items) == 0 && len(report.Skipped) == 0 && report.BaselineGaps == 0 && report.BindingsChecked == 0 {
		return drift.Report{}, errors.New("drift report contains no items; run specdrift first")
	}
	if !strings.EqualFold(strings.TrimSpace(report.Language), "en") {
		return drift.Report{}, fmt.Errorf("drift report language must be en, got %q", report.Language)
	}
	return report, nil
}

func (s *syncRun) planItem(specDir string, item drift.Item) (planResult, error) {
	switch item.Kind {
	case drift.KindMissing:
		// Only missing metadata additions are mechanical to patch.
	default:
		return planResult{item: item, action: "flagged", flagKind: item.Kind, flagText: "destructive or coverage-only drift is not auto-patched; investigate and refresh the baseline manually"}, nil
	}

	rf, err := s.loadResource(specDir, item.Product, item.Resource)
	if err != nil {
		return planResult{item: item, action: "flagged", flagKind: "resource_not_found", flagText: err.Error()}, nil
	}
	if s.collisions[normalizedFieldKey(item)] {
		return planResult{item: item, action: "flagged", flagKind: "normalized_name_collision", flagText: "multiple OpenAPI parameters normalize to the same resource field"}, nil
	}

	binding, ok := rf.resource.Bindings[item.Binding]
	if !ok {
		return planResult{item: item, action: "flagged", flagKind: "binding_not_found", flagText: fmt.Sprintf("binding %q not found in %s", item.Binding, rf.path)}, nil
	}
	if binding.API != item.API {
		return planResult{item: item, action: "flagged", flagKind: "api_mismatch", flagText: fmt.Sprintf("binding %q now targets API %q instead of report API %q", item.Binding, binding.API, item.API)}, nil
	}

	ops := operationsUsingBinding(rf.resource, item.Binding)
	if len(ops) == 0 {
		return planResult{item: item, action: "flagged", flagKind: "binding_unused", flagText: fmt.Sprintf("binding %q is not referenced by any operation workflow in %s", item.Binding, rf.path)}, nil
	}

	if kind, reason, ok := flagForItem(item); ok {
		return planResult{item: item, action: "flagged", flagKind: kind, flagText: reason}, nil
	}

	doc := newDoc(rf.lines)
	res, err := planMissingItem(doc, rf, item, ops)
	if err != nil {
		return planResult{}, err
	}
	if res.action == "patched" {
		rf.lines = doc.lines
		// Refresh the parsed resource so later edits against the same file
		// see the new maps and the strict parser rejects a bad insertion now.
		if err := refreshResource(rf); err != nil {
			return planResult{}, err
		}
		if err := verifyPlannedItem(rf, item, ops); err != nil {
			return planResult{}, err
		}
	}
	return res, nil
}

func verifyPlannedItem(rf *resourceFile, item drift.Item, ops []string) error {
	binding, ok := rf.resource.Bindings[item.Binding]
	if !ok {
		return fmt.Errorf("%s: patched binding %q is missing", rf.path, item.Binding)
	}
	if binding.API != item.API {
		return fmt.Errorf("%s: patched binding %q targets API %q instead of %q", rf.path, item.Binding, binding.API, item.API)
	}
	if !spec.BindingRequestCoverage(binding.Request)[item.Param] {
		return fmt.Errorf("%s: patched binding %q still does not cover %q", rf.path, item.Binding, item.Param)
	}

	parts := strings.Split(item.Param, ".")
	operationField := snakeCase(item.Param)
	if len(parts) == 1 {
		if _, ok := rf.resource.Schema.Fields[operationField]; !ok {
			return fmt.Errorf("%s: patched schema field %q is missing", rf.path, operationField)
		}
	} else {
		parentParts := parts[:len(parts)-1]
		loc, ok := schemaParentLocation(rf.resource.Schema.Fields, parentParts)
		if !ok {
			return fmt.Errorf("%s: patched schema parent %q is missing", rf.path, strings.Join(parentParts, "."))
		}
		if _, ok := loc.childFields[snakeCase(parts[len(parts)-1])]; !ok {
			return fmt.Errorf("%s: patched nested schema field %q is missing", rf.path, item.Param)
		}
		operationField = loc.rootField
	}
	for _, op := range ops {
		if !operationHasField(rf.resource, op, operationField) {
			return fmt.Errorf("%s: patched operation %q does not expose %q", rf.path, op, operationField)
		}
	}
	return nil
}

func (s *syncRun) loadResource(specDir, product, resource string) (*resourceFile, error) {
	key := product + "/" + resource
	if rf, ok := s.files[key]; ok {
		return rf, nil
	}
	path, raw, loaded, err := findResourceSpec(specDir, product, resource)
	if err != nil {
		return nil, err
	}
	rf := &resourceFile{
		path:     path,
		original: raw,
		lines:    splitLines(string(raw)),
		resource: loaded,
	}
	s.files[key] = rf
	return rf, nil
}

func refreshResource(rf *resourceFile) error {
	again, err := spec.Load(joinLines(rf.lines))
	if err != nil {
		return fmt.Errorf("patched spec %s no longer parses: %w", rf.path, err)
	}
	rf.resource = again
	return nil
}

func findResourceSpec(specDir, product, resource string) (string, []byte, spec.ResourceSpec, error) {
	dir := filepath.Join(specDir, product)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, spec.ResourceSpec{}, fmt.Errorf("read spec product dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" || entry.Name() == "product.yaml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", nil, spec.ResourceSpec{}, fmt.Errorf("read spec file %s: %w", path, err)
		}
		loaded, err := spec.Load(raw)
		if err != nil {
			continue
		}
		if loaded.Product == product && loaded.Resource == resource {
			return path, raw, loaded, nil
		}
	}
	return "", nil, spec.ResourceSpec{}, fmt.Errorf("resource spec not found for %s/%s", product, resource)
}

func flagForItem(item drift.Item) (string, string, bool) {
	if !validOpenAPIParameterPath(item.Param) {
		return "invalid_parameter_name", "OpenAPI parameter names must contain only dotted ASCII identifiers", true
	}
	if item.Required {
		return "required_parameter", "new required parameters change the public CLI contract and require manual modeling", true
	}
	if reason, ok := unsupportedTypeFlag(item.Type); ok {
		return "unsupported_type", reason, true
	}
	if name, ok := nameToIDFlag(item); ok {
		return name, "parameter may require a name-to-ID cross-API resolution Go hook", true
	}
	if name, ok := billingAuthPrereqFlag(item); ok {
		return name, "parameter touches billing, authorization, preemptible, or prerequisite resource behavior and needs explicit spec modeling", true
	}
	if name, ok := enumFlag(item); ok {
		return name, "metadata does not carry enum values; model as a plain string without enum and annotate the missing values manually", true
	}
	return "", "", false
}

func validOpenAPIParameterPath(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for index := 0; index < len(part); index++ {
			char := part[index]
			letter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
			digit := char >= '0' && char <= '9'
			if (!letter && char != '_' && (index == 0 || !digit)) || (index == 0 && digit) {
				return false
			}
		}
	}
	return true
}

func unsupportedTypeFlag(apiType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(apiType)) {
	case "string", "integer", "long", "float", "double", "boolean":
		return "", false
	default:
		return "unsupported OpenAPI type " + apiType, true
	}
}

func normalizedFieldCollisions(items []drift.Item) map[string]bool {
	first := map[string]string{}
	collisions := map[string]bool{}
	for _, item := range items {
		key := normalizedFieldKey(item)
		if previous, ok := first[key]; ok && previous != item.Param {
			collisions[key] = true
			continue
		}
		first[key] = item.Param
	}
	return collisions
}

func normalizedFieldKey(item drift.Item) string {
	parts := strings.Split(item.Param, ".")
	for index := range parts {
		parts[index] = snakeCase(parts[index])
	}
	return item.Product + "/" + item.Resource + "/" + strings.Join(parts, ".")
}

func nameToIDFlag(item drift.Item) (string, bool) {
	name := item.Param
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	if len(name) < 3 {
		return "", false
	}
	if !strings.HasSuffix(name, "Id") && !strings.HasSuffix(name, "Name") {
		return "", false
	}
	desc := lowerDesc(item.Description)
	if strings.Contains(desc, "call the [describe") || strings.Contains(desc, "call the [list") {
		return "name_to_id", true
	}
	return "", false
}

func billingAuthPrereqFlag(item drift.Item) (string, bool) {
	low := strings.ToLower(item.Param)
	markers := []string{
		"charge", "autopay", "autorenew", "period", "spot", "price", "credit",
		"ramrole", "role", "permission", "authorization", "auth",
		"dedicatedhost", "hpccluster", "storageset", "deploymentset",
		"capacityreservation", "reservation",
	}
	for _, marker := range markers {
		if strings.Contains(low, marker) {
			return "needs_review", true
		}
	}
	return "", false
}

func enumFlag(item drift.Item) (string, bool) {
	if !strings.EqualFold(item.Type, "String") {
		return "", false
	}
	desc := lowerDesc(item.Description)
	if strings.Contains(desc, "valid values:") || strings.Contains(desc, "valid values") {
		return "enum", true
	}
	return "", false
}

func lowerDesc(description string) string {
	return strings.ToLower(strings.Join(strings.Fields(description), " "))
}

func planMissingItem(doc *yamlDoc, rf *resourceFile, item drift.Item, ops []string) (planResult, error) {
	parts := strings.Split(item.Param, ".")
	if len(parts) == 1 {
		return planTopLevel(doc, rf, item, ops)
	}
	return planNested(doc, rf, item, ops)
}

func planTopLevel(doc *yamlDoc, rf *resourceFile, item drift.Item, ops []string) (planResult, error) {
	fieldName := snakeCase(item.Param)
	resource := rf.resource
	binding := resource.Bindings[item.Binding]
	covered := spec.BindingRequestCoverage(binding.Request)

	var actions []string
	if _, exists := resource.Schema.Fields[fieldName]; !exists {
		block, ok := doc.findPath("schema", "fields")
		if !ok {
			return planResult{}, fmt.Errorf("%s: cannot locate schema.fields block", rf.path)
		}
		doc.insertAt(doc.blockEndIndex(block), buildSchemaFieldLines(4, fieldName, item.Type, item.Description)...)
		actions = append(actions, fmt.Sprintf("add schema field %s", fieldName))
	}

	if !covered[item.Param] {
		if err := addTopLevelRequestMapping(doc, rf, item, fieldName); err != nil {
			return planResult{}, err
		}
		actions = append(actions, fmt.Sprintf("map request %s", item.Param))
	}

	for _, op := range ops {
		if !operationHasField(resource, op, fieldName) {
			if err := addOperationField(doc, op, fieldName); err != nil {
				return planResult{}, err
			}
			actions = append(actions, fmt.Sprintf("expose %s in operation %s", fieldName, op))
		}
	}

	if len(actions) == 0 {
		return planResult{item: item, action: "already-synced", detail: "already present in schema and binding request"}, nil
	}
	return planResult{item: item, action: "patched", detail: strings.Join(actions, "; ")}, nil
}

func planNested(doc *yamlDoc, rf *resourceFile, item drift.Item, ops []string) (planResult, error) {
	allParts := strings.Split(item.Param, ".")
	parentParts := allParts[:len(allParts)-1]
	leafName := allParts[len(allParts)-1]
	fieldName := snakeCase(leafName)

	resource := rf.resource
	loc, ok := schemaParentLocation(resource.Schema.Fields, parentParts)
	if !ok {
		return planResult{item: item, action: "flagged", flagKind: "new_parent_group",
			flagText: fmt.Sprintf("parent group %q must already exist in schema.fields before auto-sync can add %q", strings.Join(parentParts, "."), item.Param)}, nil
	}

	binding := resource.Bindings[item.Binding]
	covered := spec.BindingRequestCoverage(binding.Request)

	var actions []string
	if _, exists := loc.childFields[fieldName]; !exists {
		block, ok := doc.findPath(loc.linePath...)
		if !ok {
			return planResult{}, fmt.Errorf("%s: cannot locate schema block %s", rf.path, strings.Join(loc.linePath, "."))
		}
		indent := doc.indent(block) + 2
		doc.insertAt(doc.blockEndIndex(block), buildSchemaFieldLines(indent, fieldName, item.Type, item.Description)...)
		actions = append(actions, fmt.Sprintf("add nested schema field %s", fieldName))
	}

	if !covered[item.Param] {
		if err := addNestedRequestMapping(doc, rf, item, parentParts, fieldName); err != nil {
			return planResult{}, err
		}
		actions = append(actions, fmt.Sprintf("map request %s", item.Param))
	}

	rootField := loc.rootField
	for _, op := range ops {
		if !operationHasField(resource, op, rootField) {
			if err := addOperationField(doc, op, rootField); err != nil {
				return planResult{}, err
			}
			actions = append(actions, fmt.Sprintf("expose %s in operation %s", rootField, op))
		}
	}

	if len(actions) == 0 {
		return planResult{item: item, action: "already-synced", detail: "already present in schema and binding request"}, nil
	}
	return planResult{item: item, action: "patched", detail: strings.Join(actions, "; ")}, nil
}

type schemaParentLoc struct {
	childFields map[string]spec.SchemaField
	linePath    []string
	rootField   string
	specType    string // object or array
}

func schemaParentLocation(fields map[string]spec.SchemaField, parentParts []string) (schemaParentLoc, bool) {
	loc := schemaParentLoc{childFields: fields, linePath: []string{"schema", "fields"}}
	current := fields
	for i, part := range parentParts {
		name, field, ok := findSchemaField(current, part)
		if ok == false {
			return loc, false
		}
		if i == 0 {
			loc.rootField = name
		}
		switch {
		case field.Type == "object":
			if field.Fields == nil {
				return loc, false
			}
			current = field.Fields
			loc.linePath = append(loc.linePath, name, "fields")
			loc.specType = "object"
		case field.Type == "array" && field.Items != nil && field.Items.Type == "object":
			if field.Items.Fields == nil {
				return loc, false
			}
			current = field.Items.Fields
			loc.linePath = append(loc.linePath, name, "items", "fields")
			loc.specType = "array"
		default:
			return loc, false
		}
		if i == len(parentParts)-1 {
			loc.childFields = current
		}
	}
	return loc, true
}

func findSchemaField(fields map[string]spec.SchemaField, openAPIName string) (string, spec.SchemaField, bool) {
	for _, candidate := range schemaFieldNameCandidates(openAPIName) {
		if field, ok := fields[candidate]; ok {
			return candidate, field, true
		}
	}
	return "", spec.SchemaField{}, false
}

func schemaFieldNameCandidates(openAPIName string) []string {
	base := snakeCase(openAPIName)
	seen := map[string]bool{base: true}
	out := []string{base}
	for _, candidate := range []string{pluralizeSnakeWord(base)} {
		if candidate != "" && seen[candidate] == false {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

func pluralizeSnakeWord(value string) string {
	index := strings.LastIndex(value, "_")
	prefix, last := "", value
	if index >= 0 {
		prefix, last = value[:index], value[index+1:]
	}
	if len(last) < 2 {
		return value + "s"
	}
	switch {
	case strings.HasSuffix(last, "is"):
		last = strings.TrimSuffix(last, "is") + "es"
	case strings.HasSuffix(last, "s"), strings.HasSuffix(last, "x"), strings.HasSuffix(last, "z"),
		strings.HasSuffix(last, "ch"), strings.HasSuffix(last, "sh"):
		last += "es"
	case strings.HasSuffix(last, "y") && len(last) > 1:
		prev := last[len(last)-2]
		if prev != 'a' && prev != 'e' && prev != 'i' && prev != 'o' && prev != 'u' {
			last = last[:len(last)-1] + "ies"
		} else {
			last += "s"
		}
	default:
		last += "s"
	}
	if prefix == "" {
		return last
	}
	return prefix + "_" + last
}

func addTopLevelRequestMapping(doc *yamlDoc, rf *resourceFile, item drift.Item, fieldName string) error {
	request, ok := doc.findPath("bindings", item.Binding, "request")
	if !ok {
		return fmt.Errorf("%s: cannot locate bindings.%s.request block", rf.path, item.Binding)
	}
	indent := doc.indent(request) + 2
	lines := buildRequestMappingLines(indent, item.Param, fieldName, item.Type)
	if apiParam, ok := doc.findChildKey(request, "ApiParam"); ok {
		doc.insertAt(apiParam, lines...)
	} else {
		doc.insertAt(doc.blockEndIndex(request), lines...)
	}
	return nil
}

func addNestedRequestMapping(doc *yamlDoc, rf *resourceFile, item drift.Item, parentParts []string, fieldName string) error {
	requestPath := []string{"bindings", item.Binding, "request"}
	requestPath = append(requestPath, parentParts...)
	requestPath = append(requestPath, "fields")
	fieldsBlock, ok := doc.findPath(requestPath...)
	if !ok {
		return fmt.Errorf("%s: cannot locate binding request fields for %s", rf.path, strings.Join(parentParts, "."))
	}
	indent := doc.indent(fieldsBlock) + 2
	allParts := strings.Split(item.Param, ".")
	apiName := allParts[len(allParts)-1]
	lines := buildRequestMappingLines(indent, apiName, fieldName, item.Type)
	doc.insertAt(doc.blockEndIndex(fieldsBlock), lines...)
	return nil
}

func addOperationField(doc *yamlDoc, op, fieldName string) error {
	fieldsBlock, ok := doc.findPath("operations", op, "input", "fields")
	if !ok {
		return fmt.Errorf("cannot locate operations.%s.input.fields block", op)
	}
	indent := doc.indent(fieldsBlock) + 2
	doc.insertAt(doc.blockEndIndex(fieldsBlock),
		fmt.Sprintf("%s- %s:", strings.Repeat(" ", indent), fieldName),
		fmt.Sprintf("%s%s", strings.Repeat(" ", indent+4), "brief: false"),
	)
	return nil
}

func operationHasField(resource spec.ResourceSpec, op, fieldName string) bool {
	operation, ok := resource.Operations[op]
	if !ok {
		return false
	}
	for _, field := range operation.Input.Fields {
		if field.Name == fieldName {
			return true
		}
	}
	return false
}

func operationsUsingBinding(resource spec.ResourceSpec, bindingName string) []string {
	var ops []string
	for opName, operation := range resource.Operations {
		for _, step := range operation.Workflow {
			if step.Binding == bindingName {
				ops = append(ops, opName)
				break
			}
		}
	}
	sort.Strings(ops)
	return ops
}

// buildSchemaFieldLines returns the YAML lines for one schema field at the
// given key indentation. It intentionally emits only pure, deterministic
// passthrough shapes: scalar, object (from passthrough), and array of string.
func buildSchemaFieldLines(indent int, fieldName, apiType, description string) []string {
	tipe, kind := mapOpenAPIType(apiType)
	pad := strings.Repeat(" ", indent)
	child := strings.Repeat(" ", indent+2)
	lines := []string{fmt.Sprintf("%s%s:", pad, fieldName)}
	lines = append(lines, fmt.Sprintf("%stype: %s", child, tipe))
	if desc := collapsedDescription(description); desc != "" {
		lines = append(lines, fmt.Sprintf("%sdescription: %s", child, yamlSingleQuote(desc)))
	}
	if kind == "array" {
		lines = append(lines, fmt.Sprintf("%sitems:", child))
		lines = append(lines, fmt.Sprintf("%stype: string", strings.Repeat(" ", indent+4)))
	}
	return lines
}

func mapOpenAPIType(apiType string) (specType, kind string) {
	switch strings.ToLower(strings.TrimSpace(apiType)) {
	case "string":
		return "string", "scalar"
	case "integer", "long":
		return "integer", "scalar"
	case "float", "double":
		return "number", "scalar"
	case "boolean":
		return "boolean", "scalar"
	case "repeatlist":
		return "array", "array"
	case "struct":
		return "object", "object"
	default:
		return "", "unknown"
	}
}

func buildRequestMappingLines(indent int, apiName, fieldName, apiType string) []string {
	_, kind := mapOpenAPIType(apiType)
	pad := strings.Repeat(" ", indent)
	child := strings.Repeat(" ", indent+2)
	switch kind {
	case "array":
		return []string{
			fmt.Sprintf("%s%s:", pad, apiName),
			fmt.Sprintf("%seach: $.%s", child, fieldName),
		}
	case "object":
		return []string{
			fmt.Sprintf("%s%s:", pad, apiName),
			fmt.Sprintf("%sfrom: $.%s", child, fieldName),
		}
	default:
		return []string{fmt.Sprintf("%s%s: $.%s", pad, apiName, fieldName)}
	}
}

func collapsedDescription(description string) string {
	fields := strings.Fields(description)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func yamlSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func snakeCase(value string) string {
	runes := []rune(value)
	var out strings.Builder
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := runes[i-1]
				prevLowerOrDigit := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
				prevUpper := prev >= 'A' && prev <= 'Z'
				nextLowerOrDigit := i+1 < len(runes) && ((runes[i+1] >= 'a' && runes[i+1] <= 'z') || (runes[i+1] >= '0' && runes[i+1] <= '9'))
				if prevLowerOrDigit || (prevUpper && nextLowerOrDigit) {
					out.WriteByte('_')
				}
			}
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func printPlan(results []planResult) {
	patched := 0
	flagged := 0
	for _, res := range results {
		switch res.action {
		case "patched":
			patched++
		case "flagged":
			flagged++
		}
	}
	fmt.Printf("specdrift sync: patched=%d flagged=%d already-synced=%d\n", patched, flagged, len(results)-patched-flagged)
}

func printFlags(flags []planResult) {
	for _, res := range flags {
		fmt.Printf("flag\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			res.item.Product, res.item.Resource, res.item.Binding, res.item.API, res.item.Param,
			res.flagKind, res.flagText)
	}
}

// planFile is the structured per-item sync plan written by -plan-out. The
// report-only api-sync monitor attaches it as evidence, and maintainers use it
// to distinguish mechanical patches from items requiring manual adaptation.
type planFile struct {
	Items []planItemJSON `json:"items"`
}

type planItemJSON struct {
	Product  string `json:"product"`
	Resource string `json:"resource"`
	Binding  string `json:"binding"`
	API      string `json:"api"`
	Param    string `json:"param"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Kind     string `json:"kind"`
	Action   string `json:"action"`
	TopLevel bool   `json:"top_level,omitempty"`
	FlagKind string `json:"flag_kind,omitempty"`
	FlagText string `json:"flag_text,omitempty"`
}

func (s *syncRun) planBytes() ([]byte, error) {
	out := planFile{Items: make([]planItemJSON, 0, len(s.plan))}
	for _, res := range s.plan {
		entry := planItemJSON{
			Product:  res.item.Product,
			Resource: res.item.Resource,
			Binding:  res.item.Binding,
			API:      res.item.API,
			Param:    res.item.Param,
			Type:     res.item.Type,
			Required: res.item.Required,
			Kind:     res.item.Kind,
			Action:   res.action,
			TopLevel: !strings.Contains(res.item.Param, "."),
			FlagKind: res.flagKind,
			FlagText: res.flagText,
		}
		out.Items = append(out.Items, entry)
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode sync plan: %w", err)
	}
	raw = append(raw, '\n')
	return raw, nil
}

func (s *syncRun) writePlan(path string) error {
	raw, err := s.planBytes()
	if err != nil {
		return err
	}
	if err := commitFileUpdates([]fileUpdate{{path: path, data: raw, mode: 0o644}}); err != nil {
		return fmt.Errorf("write plan %s: %w", path, err)
	}
	return nil
}

func (s *syncRun) printDryRun() error {
	keys := make([]string, 0, len(s.files))
	for key, rf := range s.files {
		if bytes.Equal(rf.original, joinLines(rf.lines)) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rf := s.files[key]
		if err := printUnifiedDiff(rf.path, rf.original, joinLines(rf.lines)); err != nil {
			return err
		}
	}
	printPlan(s.plan)
	return nil
}

func (s *syncRun) writeFiles(planOut string) error {
	keys := make([]string, 0, len(s.files))
	for key := range s.files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	updates := make([]fileUpdate, 0, len(keys)+1)
	written := make([]string, 0, len(keys))
	for _, key := range keys {
		rf := s.files[key]
		if bytes.Equal(rf.original, joinLines(rf.lines)) {
			continue
		}
		updates = append(updates, fileUpdate{path: rf.path, data: joinLines(rf.lines), mode: 0o644})
		written = append(written, rf.path)
	}
	if planOut != "" {
		raw, err := s.planBytes()
		if err != nil {
			return err
		}
		updates = append(updates, fileUpdate{path: planOut, data: raw, mode: 0o644})
	}
	if err := commitFileUpdates(updates); err != nil {
		return fmt.Errorf("commit sync outputs: %w", err)
	}
	for _, path := range written {
		fmt.Printf("specdrift sync: wrote %s\n", path)
	}
	return nil
}

func printUnifiedDiff(path string, original, updated []byte) error {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(original)),
		B:        difflib.SplitLines(string(updated)),
		FromFile: "a/" + path,
		ToFile:   "b/" + path,
		Context:  3,
	})
	if err != nil {
		return fmt.Errorf("diff %s: %w", path, err)
	}
	fmt.Print(diff)
	return nil
}

func splitLines(value string) []string {
	lines := strings.Split(value, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func lineIndent(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

func lineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "-") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	}
	index := strings.Index(trimmed, ":")
	if index <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[:index])
}

type yamlDoc struct {
	lines []string
}

func newDoc(lines []string) *yamlDoc {
	return &yamlDoc{lines: append([]string(nil), lines...)}
}

func (d *yamlDoc) indent(index int) int {
	if index < 0 || index >= len(d.lines) {
		return 0
	}
	return lineIndent(d.lines[index])
}

// findPath locates a sequence of YAML keys by their conventional two-space
// nesting depth. The spec files maintained in this repository use two-space
// indentation consistently and the editor only appends lines, never rewrites
// existing entries, so depth-derived lookup is deterministic.
func (d *yamlDoc) findPath(keys ...string) (int, bool) {
	from := 0
	to := len(d.lines)
	for depth, key := range keys {
		targetIndent := depth * 2
		index := -1
		for i := from; i < to; i++ {
			if lineIndent(d.lines[i]) == targetIndent && lineKey(d.lines[i]) == key {
				index = i
				break
			}
		}
		if index < 0 {
			return 0, false
		}
		from = index + 1
		to = d.blockEndIndex(index)
	}
	return from - 1, true
}

// blockEndIndex returns the index just after the last line that belongs to the
// block starting at blockLine. Inserting at this index appends to the block.
func (d *yamlDoc) blockEndIndex(blockLine int) int {
	parentIndent := d.indent(blockLine)
	end := blockLine + 1
	for end < len(d.lines) {
		if lineIndent(d.lines[end]) <= parentIndent {
			break
		}
		end++
	}
	return end
}

func (d *yamlDoc) insertAt(index int, lines ...string) {
	if len(lines) == 0 {
		return
	}
	d.lines = append(d.lines[:index], append(lines, d.lines[index:]...)...)
}

func (d *yamlDoc) findChildKey(blockLine int, key string) (int, bool) {
	parentIndent := d.indent(blockLine)
	targetIndent := parentIndent + 2
	for i := blockLine + 1; i < len(d.lines); i++ {
		indent := lineIndent(d.lines[i])
		if indent <= parentIndent {
			break
		}
		if indent == targetIndent && lineKey(d.lines[i]) == key {
			return i, true
		}
	}
	return 0, false
}
