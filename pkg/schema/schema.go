package schema

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

type ProductSurface struct {
	Product     string            `json:"product"`
	Description string            `json:"description,omitempty"`
	Resources   []ResourceSurface `json:"resources"`
}

type ResourceSurface struct {
	Product     string   `json:"product,omitempty"`
	Name        string   `json:"name"`
	Parent      string   `json:"parent,omitempty"`
	SchemaID    string   `json:"schema_id"`
	Description string   `json:"description,omitempty"`
	Actions     []string `json:"actions"`
}

type ProductSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Capabilities struct {
	SchemaVersion int              `json:"schema_version"`
	CLI           string           `json:"cli"`
	Surface       string           `json:"surface"`
	OutputModes   []string         `json:"output_modes"`
	Schema        SchemaCapability `json:"schema"`
	Errors        ErrorContract    `json:"errors"`
	Products      []ProductSurface `json:"products"`
}

type SchemaCapability struct {
	Supported    bool     `json:"supported"`
	ListCommands []string `json:"list_commands"`
	GetCommand   string   `json:"get_command"`
}

type ErrorContract struct {
	Structured   bool     `json:"structured"`
	Stream       string   `json:"stream"`
	Fields       []string `json:"fields"`
	ActionFields []string `json:"action_fields"`
}

type CommandSchema struct {
	SchemaVersion int               `json:"schema_version"`
	Command       string            `json:"command"`
	SchemaID      string            `json:"schema_id,omitempty"`
	CLI           string            `json:"cli,omitempty"`
	Usage         string            `json:"usage,omitempty"`
	Kind          string            `json:"kind"`
	Risk          Risk              `json:"risk"`
	Description   string            `json:"description"`
	Positionals   []PositionalParam `json:"positionals,omitempty"`
	Params        map[string]Param  `json:"params"`
	Filters       map[string]Filter `json:"filters,omitempty"`
	Examples      []string          `json:"examples,omitempty"`
	Output        *OutputShape      `json:"output,omitempty"`
	Contract      *Contract         `json:"contract,omitempty"`
	APICalls      []APICall         `json:"api_calls,omitempty"`
}

type APICall struct {
	API                  string `json:"api"`
	Phase                string `json:"phase"`
	Condition            string `json:"condition,omitempty"`
	ConditionDescription string `json:"condition_description,omitempty"`
	Purpose              string `json:"purpose"`
	Repeated             bool   `json:"repeated,omitempty"`
	Cached               bool   `json:"cached,omitempty"`
}

type Param struct {
	Type           string           `json:"type"`
	Description    string           `json:"description,omitempty"`
	Required       bool             `json:"required,omitempty"`
	Repeatable     bool             `json:"repeatable,omitempty"`
	Positional     bool             `json:"positional,omitempty"`
	PositionalMany bool             `json:"positional_many,omitempty"`
	Default        any              `json:"default,omitempty"`
	Input          string           `json:"input,omitempty"`
	Items          *Param           `json:"items,omitempty"`
	Fields         map[string]Param `json:"fields,omitempty"`
}

type PositionalParam struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Many        bool   `json:"many,omitempty"`
	Input       string `json:"input,omitempty"`
}

type OutputShape struct {
	Root   string   `json:"root,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

type Filter struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type Risk struct {
	Level       string `json:"level"`
	Description string `json:"description"`
}

type Contract struct {
	DryRun      Support             `json:"dry_run"`
	Idempotency IdempotencyContract `json:"idempotency"`
	Wait        *WaitContract       `json:"wait,omitempty"`
}

type Support struct {
	Supported bool   `json:"supported"`
	Flag      string `json:"flag,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type IdempotencyContract struct {
	Supported bool   `json:"supported"`
	Field     string `json:"field,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type WaitContract struct {
	Waitable    bool             `json:"waitable"`
	NoWaitFlag  string           `json:"no_wait_flag,omitempty"`
	TimeoutFlag string           `json:"timeout_flag,omitempty"`
	PollCommand string           `json:"poll_command,omitempty"`
	Waiters     []WaiterContract `json:"waiters,omitempty"`
}

type WaiterContract struct {
	Name          string   `json:"name"`
	Probe         string   `json:"probe,omitempty"`
	Target        string   `json:"target,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	Timeout       string   `json:"timeout,omitempty"`
	FailureStates []string `json:"failure_states,omitempty"`
}

const idempotencyKeyParamName = "idempotency-key"

type CommandSchemaMode string

const (
	CommandSchemaBrief CommandSchemaMode = "brief"
	CommandSchemaFull  CommandSchemaMode = "full"
)

func ProductList(product string) (ProductSurface, bool) {
	return ProductListForLanguage(product, "en")
}

func ProductListForLanguage(product string, lang string) (ProductSurface, bool) {
	refs, err := discoveredResources()
	if err != nil {
		return ProductSurface{}, false
	}
	resources := make([]ResourceSurface, 0)
	loadedResources := make([]spec.ResourceSpec, 0)
	for _, ref := range refs {
		if ref.Product != product {
			continue
		}
		resource, err := spec.LoadResourceWithParent(specDir(), ref.Product, ref.Resource, ref.Parent)
		if err != nil {
			return ProductSurface{}, false
		}
		loadedResources = append(loadedResources, resource)
		resources = append(resources, resourceSurfaceForLanguage(resource, lang, false))
	}
	if len(resources) == 0 {
		return ProductSurface{}, false
	}
	productSpec, _ := spec.LoadProduct(specDir(), product)
	return ProductSurface{
		Product:     product,
		Description: productDescription(product, productSpec, loadedResources, lang),
		Resources:   resources,
	}, true
}

func ResourceForLanguage(product string, requested string, lang string) (ResourceSurface, bool) {
	return resourceForLanguagePath(product, "", requested, lang)
}

func resourceForLanguagePath(product string, parent string, requested string, lang string) (ResourceSurface, bool) {
	if product == "" || requested == "" {
		return ResourceSurface{}, false
	}
	refs, err := discoveredResources()
	if err != nil {
		return ResourceSurface{}, false
	}
	for _, ref := range refs {
		if ref.Product != product || ref.Parent != parent {
			continue
		}
		resource, err := spec.LoadResourceWithParent(specDir(), ref.Product, ref.Resource, ref.Parent)
		if err != nil {
			return ResourceSurface{}, false
		}
		if resource.Resource == requested || hasAlias(resource.Aliases, requested) {
			return resourceSurfaceForLanguage(resource, lang, true), true
		}
	}
	return ResourceSurface{}, false
}

func ResourceForLanguageName(name string, lang string) (ResourceSurface, bool) {
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 2:
		return resourceForLanguagePath(parts[0], "", parts[1], lang)
	case 3:
		return resourceForLanguagePath(parts[0], parts[1], parts[2], lang)
	default:
		return ResourceSurface{}, false
	}
}

func resourceSurfaceForLanguage(resource spec.ResourceSpec, lang string, includeProduct bool) ResourceSurface {
	surface := ResourceSurface{
		Name:        resource.Resource,
		Parent:      resource.Parent,
		SchemaID:    resourceSchemaID(resource),
		Description: resource.Description.Text(lang),
		Actions:     orderedResourceActionNames(resource),
	}
	if includeProduct {
		surface.Product = resource.Product
	}
	return surface
}

func Products() []string {
	refs, err := discoveredResources()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	products := make([]string, 0)
	for _, ref := range refs {
		if seen[ref.Product] {
			continue
		}
		seen[ref.Product] = true
		products = append(products, ref.Product)
	}
	sort.Strings(products)
	return products
}

func ProductsForLanguage(lang string) []ProductSummary {
	products := Products()
	summaries := make([]ProductSummary, 0, len(products))
	for _, product := range products {
		summary := ProductSummary{Name: product}
		if surface, ok := ProductListForLanguage(product, lang); ok {
			summary.Description = surface.Description
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func CapabilitiesForLanguage(lang string) (Capabilities, bool) {
	products := Products()
	surfaces := make([]ProductSurface, 0, len(products))
	for _, product := range products {
		surface, ok := ProductListForLanguage(product, lang)
		if !ok {
			return Capabilities{}, false
		}
		surfaces = append(surfaces, surface)
	}
	return Capabilities{
		SchemaVersion: 1,
		CLI:           "ecctl",
		OutputModes:   []string{"json", "text"},
		Schema: SchemaCapability{
			Supported:    true,
			ListCommands: []string{"ecctl schema --list [product]", "ecctl schema list [product]"},
			GetCommand:   "ecctl schema <product>[.<parent>].<resource>[.<action>] [--brief|--full]",
		},
		Errors: ErrorContract{
			Structured:   true,
			Stream:       "stdout",
			Fields:       []string{"kind", "code", "message", "retryable", "suggestion", "suggested_action", "field", "accepted_values"},
			ActionFields: []string{"action_name", "code", "message"},
		},
		Products: surfaces,
	}, true
}

func Command(name string) (CommandSchema, bool) {
	return CommandForLanguage(name, "en")
}

func CommandForLanguage(name string, lang string) (CommandSchema, bool) {
	return CommandForLanguageMode(name, lang, CommandSchemaBrief)
}

func CommandForLanguageMode(name string, lang string, mode CommandSchemaMode) (CommandSchema, bool) {
	product, parent, resourceName, actionName, ok := parseCommandName(name)
	if !ok {
		return CommandSchema{}, false
	}
	resourceName, parent, ok = commandResource(product, parent, resourceName)
	if !ok {
		return CommandSchema{}, false
	}
	resource, err := spec.LoadResourceWithParent(specDir(), product, resourceName, parent)
	if err != nil {
		return CommandSchema{}, false
	}
	operation, ok := resource.Operations[actionName]
	if !ok {
		return CommandSchema{}, false
	}
	kind := commandKind(operation)
	positionals := commandPositionals(resource, operation, lang, mode)
	command := CommandSchema{
		SchemaVersion: 1,
		Command:       name,
		SchemaID:      commandSchemaID(name, actionName, positionals),
		CLI:           commandCLI(resource, actionName),
		Usage:         commandUsage(resource, actionName, positionals),
		Kind:          kind,
		Risk:          commandRisk(actionName, kind),
		Description:   commandDescription(resource, actionName, name, operation, lang),
		Positionals:   positionals,
		Params:        commandParams(resource, actionName, lang, mode),
		Filters:       commandFilters(resource, actionName, lang),
		Examples:      commandExamples(actionName, operation, mode),
		Output:        commandOutput(resource, actionName, operation),
		Contract:      commandContract(resource, operation),
	}
	if mode == CommandSchemaFull {
		command.APICalls = operationAPICalls(resource, operation, lang)
	}
	return command, true
}

func commandSchemaID(name string, actionName string, positionals []PositionalParam) string {
	if len(positionals) == 0 || isCRUDAction(actionName) {
		return ""
	}
	return name
}

func commandCLI(resource spec.ResourceSpec, actionName string) string {
	parts := resourceCLIPath(resource)
	parts = append(parts, actionName)
	return strings.Join(parts, " ")
}

func resourceCLIPath(resource spec.ResourceSpec) []string {
	parts := []string{"ecctl", resource.Product}
	if resource.Parent != "" {
		parts = append(parts, resource.Parent)
	}
	if resource.Parent != "" || resource.Resource != resource.Product {
		parts = append(parts, resource.Resource)
	}
	return parts
}

func resourceSchemaID(resource spec.ResourceSpec) string {
	parts := []string{resource.Product}
	if resource.Parent != "" {
		parts = append(parts, resource.Parent)
	}
	parts = append(parts, resource.Resource)
	return strings.Join(parts, ".")
}

func commandUsage(resource spec.ResourceSpec, actionName string, positionals []PositionalParam) string {
	if len(positionals) == 0 || isCRUDAction(actionName) {
		return ""
	}
	parts := []string{commandCLI(resource, actionName)}
	for _, positional := range positionals {
		name := positional.Name
		if positional.Many {
			parts = append(parts, "["+name+"...]")
			continue
		}
		if positional.Required {
			parts = append(parts, "<"+name+">")
		} else {
			parts = append(parts, "["+name+"]")
		}
	}
	parts = append(parts, "[flags]")
	return strings.Join(parts, " ")
}

func commandPositionals(resource spec.ResourceSpec, operation spec.Operation, lang string, mode CommandSchemaMode) []PositionalParam {
	positionals := make([]PositionalParam, 0)
	for _, ref := range operation.Input.Fields {
		if !ref.Positional && !ref.PositionalMany {
			continue
		}
		if ref.HasSchema && !ref.Schema {
			continue
		}
		if mode == CommandSchemaBrief && ref.HasBrief && !ref.Brief {
			continue
		}
		param, ok := operationInputSchemaParam(resource, ref, lang)
		if !ok {
			continue
		}
		positionals = append(positionals, PositionalParam{
			Name:        schemaFlagName(resource, ref),
			Type:        param.Type,
			Description: param.Description,
			Required:    param.Required,
			Many:        param.PositionalMany,
			Input:       param.Input,
		})
	}
	return positionals
}

func commandExamples(actionName string, operation spec.Operation, _ CommandSchemaMode) []string {
	if len(operation.Examples) == 0 {
		return nil
	}
	if isCRUDAction(actionName) {
		return nil
	}
	hasPositional := false
	for _, ref := range operation.Input.Fields {
		if ref.Positional || ref.PositionalMany {
			hasPositional = true
			break
		}
	}
	if !hasPositional {
		return nil
	}
	return append([]string(nil), operation.Examples[:1]...)
}

func isCRUDAction(actionName string) bool {
	switch actionName {
	case "list", "get", "create", "update", "delete":
		return true
	default:
		return false
	}
}

func commandOutput(resource spec.ResourceSpec, actionName string, operation spec.Operation) *OutputShape {
	const maxBriefOutputFields = 16

	if !actionSupportsFieldSelection(actionName) || hasOperationOutputRules(operation.Output) {
		return nil
	}
	output := &OutputShape{Root: commandOutputRoot(resource, actionName)}
	if probeName := commandOutputProbe(operation); probeName != "" {
		if probe, ok := resource.Probes[probeName]; ok && len(probe.Response.Fields) > 0 {
			fields := sortedProbeFieldNames(probe.Response.Fields)
			if len(fields) <= maxBriefOutputFields {
				output.Fields = fields
			}
		}
	}
	if len(output.Fields) == 0 {
		return nil
	}
	return output
}

func commandOutputRoot(resource spec.ResourceSpec, actionName string) string {
	if actionName == "list" {
		return resource.Identity.OutputRoot.Many
	}
	return resource.Identity.OutputRoot.One
}

func hasOperationOutputRules(output spec.OperationOutput) bool {
	return len(output.Fields) > 0 || len(output.Select) > 0
}

func commandOutputProbe(operation spec.Operation) string {
	if operation.Call.Probe != "" {
		if len(operation.Workflow) > 0 {
			return ""
		}
		return operation.Call.Probe
	}
	probeName := ""
	for _, step := range operation.Workflow {
		if step.Probe == "" {
			continue
		}
		if step.When != "" || len(step.WhenAny) > 0 || step.Unless != "" || step.Merge || step.Append {
			return ""
		}
		if probeName != "" {
			return ""
		}
		probeName = step.Probe
	}
	return probeName
}

func sortedProbeFieldNames(fields map[string]spec.ProbeField) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func MarshalCompact(v any) ([]byte, error) {
	return json.Marshal(v)
}

func parseCommandName(name string) (product string, parent string, resource string, action string, ok bool) {
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 2:
		return parts[0], "", "", parts[1], true
	case 3:
		return parts[0], "", parts[1], parts[2], true
	case 4:
		return parts[0], parts[1], parts[2], parts[3], true
	default:
		return "", "", "", "", false
	}
}

func specDir() string {
	return os.Getenv("ECCTL_SPEC_DIR")
}

func discoveredResources() ([]spec.ResourceRef, error) {
	return spec.ListResources(specDir())
}

func findParent(product, resource string) string {
	refs, err := discoveredResources()
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		if ref.Product == product && ref.Resource == resource {
			return ref.Parent
		}
	}
	return ""
}

func commandResource(product, parent, requested string) (string, string, bool) {
	refs, err := discoveredResources()
	if err != nil {
		return "", "", false
	}
	var defaultRef spec.ResourceRef
	hasDefault := false
	for _, ref := range refs {
		if ref.Product != product || ref.Parent != parent {
			continue
		}
		if requested != "" && ref.Resource == requested {
			return ref.Resource, ref.Parent, true
		}
		if parent == "" && ref.Resource == product {
			defaultRef = ref
			hasDefault = true
		}
	}
	if !hasDefault {
		return "", "", false
	}
	if requested == "" {
		return defaultRef.Resource, defaultRef.Parent, true
	}
	resource, err := spec.LoadResourceWithParent(specDir(), defaultRef.Product, defaultRef.Resource, defaultRef.Parent)
	if err == nil && hasAlias(resource.Aliases, requested) {
		return defaultRef.Resource, defaultRef.Parent, true
	}
	return "", "", false
}

func hasAlias(aliases []string, requested string) bool {
	for _, alias := range aliases {
		if alias == requested {
			return true
		}
	}
	return false
}

func orderedResourceActionNames(resource spec.ResourceSpec) []string {
	if names, ok := orderedOperationNames(resource); ok {
		return names
	}
	return nil
}

func commandKind(operation spec.Operation) string {
	for _, step := range operation.Workflow {
		if step.Binding != "" {
			return "mutation"
		}
	}
	return "read"
}

func commandRisk(action string, kind string) Risk {
	if kind == "read" {
		return Risk{Level: "low", Description: "Read-only."}
	}
	switch action {
	case "delete", "remove", "destroy":
		return Risk{Level: "high", Description: "Deletes resources."}
	case "exec", "invoke", "sendfile":
		return Risk{Level: "high", Description: "Runs remote work."}
	default:
		return Risk{Level: "medium", Description: "Mutates resources."}
	}
}

func commandContract(resource spec.ResourceSpec, operation spec.Operation) *Contract {
	if commandKind(operation) != "mutation" {
		return nil
	}
	dryRun := dryRunContract(operation)
	idempotency := idempotencyContract(resource, operation)
	wait := waitContract(resource, operation)
	if !dryRun.Supported && !idempotency.Supported && wait == nil {
		return nil
	}
	contract := &Contract{
		DryRun:      dryRun,
		Idempotency: idempotency,
	}
	if wait != nil {
		contract.Wait = wait
	}
	return contract
}

func dryRunContract(operation spec.Operation) Support {
	if operationHasControl(operation, "dry_run") {
		return Support{Supported: true, Flag: "dry-run"}
	}
	return Support{Supported: false, Reason: "unsupported"}
}

func idempotencyContract(resource spec.ResourceSpec, operation spec.Operation) IdempotencyContract {
	for _, binding := range operationBindings(resource, operation) {
		if binding.Idempotency.Field == "" {
			continue
		}
		return IdempotencyContract{
			Supported: true,
			Field:     binding.Idempotency.Field,
			Prefix:    binding.Idempotency.Prefix,
			Mode:      "explicit_or_auto_generated",
		}
	}
	return IdempotencyContract{Supported: false, Reason: "unsupported"}
}

func waitContract(resource spec.ResourceSpec, operation spec.Operation) *WaitContract {
	waiters := operationWaiters(resource, operation)
	if len(waiters) == 0 {
		return nil
	}
	contract := &WaitContract{
		Waitable: true,
		Waiters:  waiters,
	}
	if operationHasControl(operation, "no_wait") {
		contract.NoWaitFlag = "no-wait"
	}
	if operationHasControl(operation, "timeout") {
		contract.TimeoutFlag = "timeout"
	}
	if pollCommand := pollCommand(resource); pollCommand != "" {
		contract.PollCommand = pollCommand
	}
	return contract
}

func operationBindings(resource spec.ResourceSpec, operation spec.Operation) []spec.Binding {
	bindings := make([]spec.Binding, 0)
	for _, step := range operation.Workflow {
		if step.Binding == "" {
			continue
		}
		binding, ok := resource.Bindings[step.Binding]
		if !ok {
			continue
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

func operationWaiters(resource spec.ResourceSpec, operation spec.Operation) []WaiterContract {
	waiterNames := make([]string, 0)
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if _, ok := resource.Waiters[name]; !ok {
			return
		}
		seen[name] = true
		waiterNames = append(waiterNames, name)
	}
	for _, step := range operation.Workflow {
		add(step.Wait)
		if step.Binding == "" {
			continue
		}
		if binding, ok := resource.Bindings[step.Binding]; ok {
			add(binding.Wait)
		}
	}
	waiters := make([]WaiterContract, 0, len(waiterNames))
	for _, name := range waiterNames {
		waiter := resource.Waiters[name]
		waiters = append(waiters, WaiterContract{
			Name:          name,
			Probe:         waiter.Probe,
			Target:        waiter.Target,
			Interval:      waiter.Interval,
			Timeout:       waiter.Timeout,
			FailureStates: waiter.Failure.States,
		})
	}
	return waiters
}

func operationHasControl(operation spec.Operation, name string) bool {
	for _, control := range operation.Input.Controls {
		if control.Name == name {
			return true
		}
	}
	return false
}

func pollCommand(resource spec.ResourceSpec) string {
	if !operationHasPositionalID(resource.Operations["get"]) {
		return ""
	}
	parts := resourceCLIPath(resource)
	parts = append(parts, "get", "<id>")
	if resource.Kind == "regional" {
		parts = append(parts, "--region", "<region>")
	}
	parts = append(parts, "--output", "json")
	return strings.Join(parts, " ")
}

func operationHasPositionalID(operation spec.Operation) bool {
	for _, field := range operation.Input.Fields {
		if field.Name == "id" && field.Positional {
			return true
		}
	}
	return false
}

func commandParams(resource spec.ResourceSpec, actionName string, lang string, mode CommandSchemaMode) map[string]Param {
	if params, ok := commandParamsFromOperationMode(resource, actionName, lang, mode); ok {
		return params
	}
	return nil
}

func commandParamsFromOperation(resource spec.ResourceSpec, actionName string, lang string) (map[string]Param, bool) {
	return commandParamsFromOperationMode(resource, actionName, lang, CommandSchemaBrief)
}

func commandParamsFromOperationMode(resource spec.ResourceSpec, actionName string, lang string, mode CommandSchemaMode) (map[string]Param, bool) {
	operation, ok := resource.Operations[actionName]
	if !ok {
		return nil, false
	}
	params := map[string]Param{}
	if resource.Kind == "regional" {
		params["region"] = Param{Type: "string", Description: "Alibaba Cloud region", Required: true}
	}
	refs := make([]spec.OperationFieldRef, 0, len(operation.Input.Fields)+len(operation.Input.Controls))
	refs = append(refs, operation.Input.Fields...)
	refs = append(refs, operation.Input.Controls...)
	var apiParam *Param
	for _, ref := range refs {
		if ref.HasSchema && !ref.Schema {
			continue
		}
		if mode == CommandSchemaBrief && ref.HasBrief && !ref.Brief {
			continue
		}
		param, ok := operationInputSchemaParam(resource, ref, lang)
		if !ok {
			continue
		}
		if ref.Name == "api_param" {
			copied := param
			apiParam = &copied
			continue
		}
		params[schemaFlagName(resource, ref)] = param
	}
	if apiParam != nil {
		params[flagName("api_param")] = *apiParam
	}
	if idempotency, ok := spec.OperationIdempotency(resource, operation); ok {
		params[idempotencyKeyParamName] = idempotencyKeyParam(idempotency, lang)
	}
	if actionSupportsFieldSelection(actionName) {
		params["fields"] = fieldSelectorSchemaParam(lang)
	}
	return params, true
}

func actionSupportsFieldSelection(actionName string) bool {
	return actionName == "list" || actionName == "get"
}

func fieldSelectorSchemaParam(lang string) Param {
	return Param{
		Type:        "string",
		Description: i18n.NewLocalizer(lang).Message("FlagUsage.ResourceFields"),
	}
}

func idempotencyKeyParam(idempotency spec.Idempotency, lang string) Param {
	field := idempotency.Field
	if field == "" {
		field = "idempotency field"
	}
	description := "Idempotency key for " + field + "."
	if strings.EqualFold(lang, "zh-CN") || strings.HasPrefix(strings.ToLower(lang), "zh-") {
		description = field + " 幂等键。"
	}
	return Param{
		Type:        "string",
		Description: description,
	}
}

func operationInputSchemaParam(resource spec.ResourceSpec, ref spec.OperationFieldRef, lang string) (Param, bool) {
	definition, ok := resource.Schema.Fields[ref.Name]
	if !ok {
		definition, ok = resource.Controls[ref.Name]
	}
	if !ok {
		return Param{}, false
	}
	param := paramFromSchemaDefinition(definition, lang)
	applyOperationParamOverrides(&param, ref, lang)
	applyStructuredInputShape(&param, definition, lang)
	return param, true
}

func paramFromSchemaDefinition(value spec.SchemaField, lang string) Param {
	param := Param{
		Type:        value.Type,
		Description: value.Description.Text(lang),
		Required:    value.Required,
		Repeatable:  value.Repeatable,
		Default:     value.Default,
		Input:       value.InputStyle,
	}
	if value.Items != nil {
		item := paramFromSchemaDefinition(*value.Items, lang)
		if item.Type != "" {
			param.Items = &item
		}
	}
	if len(value.Fields) > 0 {
		param.Fields = map[string]Param{}
		for name, field := range value.Fields {
			child := paramFromSchemaDefinition(field, lang)
			if child.Type != "" {
				param.Fields[name] = child
			}
		}
	}
	return param
}

func applyStructuredInputShape(param *Param, definition spec.SchemaField, lang string) {
	if definition.Type == "object" {
		param.Input = structuredInputShape(definition)
		return
	}
	if definition.Type == "array" && definition.Items != nil && definition.Items.Type == "object" {
		item := paramFromSchemaDefinition(*definition.Items, lang)
		param.Type = "object"
		param.Repeatable = true
		param.Input = structuredInputShape(*definition.Items)
		param.Fields = item.Fields
		param.Items = nil
	}
}

func structuredInputShape(field spec.SchemaField) string {
	if isShallowObjectField(field) {
		return "inline-key-value|json|@file"
	}
	return "json|@file"
}

func isShallowObjectField(field spec.SchemaField) bool {
	if field.Type != "object" || len(field.Fields) == 0 {
		return false
	}
	for _, child := range field.Fields {
		if child.Type == "object" || child.Type == "array" || child.Type == "string_array" {
			return false
		}
	}
	return true
}

func schemaFlagName(resource spec.ResourceSpec, ref spec.OperationFieldRef) string {
	if ref.FlagName != "" {
		return flagName(ref.FlagName)
	}
	name := ref.Name
	if field, ok := resource.Schema.Fields[name]; ok && field.Type == "array" && field.Items != nil && field.Items.Type == "object" {
		return flagName(singularInputName(name))
	}
	return flagName(name)
}

func singularInputName(name string) string {
	switch {
	case strings.HasSuffix(name, "ies"):
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "ses"):
		return strings.TrimSuffix(name, "es")
	case strings.HasSuffix(name, "s"):
		return strings.TrimSuffix(name, "s")
	default:
		return name
	}
}

func applyOperationParamOverrides(param *Param, ref spec.OperationFieldRef, lang string) {
	if ref.HasRequired {
		param.Required = ref.Required
	}
	if ref.HasRepeatable {
		param.Repeatable = ref.Repeatable
	}
	if ref.Default != nil {
		param.Default = ref.Default
	}
	if description := ref.Description.Text(lang); description != "" {
		param.Description = description
	}
	if ref.InputStyle != "" {
		param.Input = ref.InputStyle
	}
	if ref.Positional {
		param.Positional = true
	}
	if ref.PositionalMany {
		param.PositionalMany = true
	}
}

func commandFilters(resource spec.ResourceSpec, actionName string, lang string) map[string]Filter {
	actionFilters := resourceActionFilters(resource, actionName)
	if len(actionFilters) == 0 {
		return nil
	}
	filters := map[string]Filter{}
	for name, filter := range actionFilters {
		filterType := filter.Type
		if filterType == "" {
			filterType = resourceInputType(resource, filter.Target)
		}
		filters[name] = Filter{
			Type:        filterType,
			Description: filterDescription(resource, name, filter, lang),
		}
	}
	return filters
}

func resourceActionFilters(resource spec.ResourceSpec, actionName string) map[string]spec.Filter {
	if operation, ok := resource.Operations[actionName]; ok && len(operation.Filters) > 0 {
		return operation.Filters
	}
	return nil
}

func resourceInputType(resource spec.ResourceSpec, name string) string {
	if field, ok := resource.Schema.Fields[name]; ok {
		return field.Type
	}
	if control, ok := resource.Controls[name]; ok {
		return control.Type
	}
	return ""
}

func commandDescription(resource spec.ResourceSpec, action string, command string, operation spec.Operation, lang string) string {
	if description := operation.Description.Text(lang); description != "" {
		return description
	}
	if !strings.Contains(command, "."+resource.Resource+".") {
		return "Shortcut schema for " + resource.Product + "." + resource.Resource + "." + action + "."
	}
	displayName := schemaResourceDisplayName(resource, lang)
	switch action {
	case "list":
		return "List " + displayName + " resources with optional filters."
	case "get":
		return "Get " + displayName + " by ID."
	case "create":
		return "Create " + displayName + " and wait for completion unless --no-wait is set."
	case "update":
		return "Update " + displayName + " and read back the result unless --no-wait is set."
	case "delete":
		return "Delete " + displayName + " and wait until absent unless --no-wait is set."
	default:
		return strings.Title(action) + " " + displayName + "."
	}
}

func filterDescription(resource spec.ResourceSpec, name string, filter spec.Filter, lang string) string {
	if description := filter.Description.Text(lang); description != "" {
		return description
	}
	if filter.Target != "" {
		if description := localizedInputDescription(resource, lang, filter.Target, ""); description != "" {
			return description
		}
	}
	prefix := filter.KeyPrefix
	if prefix == "" && strings.HasSuffix(name, ".") {
		prefix = name
	}
	if prefix != "" {
		return prefix + "<key> filter."
	}
	return name + " filter."
}

func localizedInputDescription(resource spec.ResourceSpec, lang string, name string, fallback string) string {
	if field, ok := resource.Schema.Fields[name]; ok {
		if description := field.Description.Text(lang); description != "" {
			return description
		}
	}
	if control, ok := resource.Controls[name]; ok {
		if description := control.Description.Text(lang); description != "" {
			return description
		}
	}
	return fallback
}

func schemaResourceDisplayName(resource spec.ResourceSpec, lang string) string {
	if displayName := resource.DisplayName.Text(lang); displayName != "" {
		return displayName
	}
	return resource.Resource
}

func productDescription(product string, productSpec spec.ProductSpec, resources []spec.ResourceSpec, lang string) string {
	if description := productSpec.Description.Text(lang); description != "" {
		return description
	}
	for _, resource := range resources {
		if resource.Product == product && resource.Resource == product {
			if description := resource.Description.Text(lang); description != "" {
				return description
			}
		}
	}
	return "Manage " + strings.ToUpper(product) + " resources"
}

func orderedOperationNames(resource spec.ResourceSpec) ([]string, bool) {
	if len(resource.Operations) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(resource.Operations))
	for name := range resource.Operations {
		names = append(names, name)
	}
	return orderPreferredNames(names), true
}

func orderPreferredNames(names []string) []string {
	available := map[string]bool{}
	for _, name := range names {
		available[name] = true
	}
	preferred := []string{"list", "get", "create", "update", "delete"}
	ordered := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range preferred {
		if available[name] {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	rest := make([]string, 0, len(names))
	for _, name := range names {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func flagName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}
