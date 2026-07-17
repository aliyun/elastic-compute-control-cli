package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ecctl/pkg/aliyun"
	"ecctl/pkg/config"
	"ecctl/pkg/engine"
	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/i18n"
	"ecctl/pkg/spec"
)

type ResourceCallerFactory func(profileName, configPath string, resource spec.ResourceSpec, region string, getenv func(string) string) (engine.Caller, error)

type resourceCallerFactoryKey struct{}
type fullCommandSurfaceKey struct{}

func WithResourceCallerFactory(ctx context.Context, factory ResourceCallerFactory) context.Context {
	return context.WithValue(ctx, resourceCallerFactoryKey{}, factory)
}

// WithFullCommandSurface keeps spec-level tests able to exercise resources that
// are intentionally hidden from the public CLI surface.
func WithFullCommandSurface(ctx context.Context) context.Context {
	return context.WithValue(ctx, fullCommandSurfaceKey{}, true)
}

func fullCommandSurfaceFromContext(ctx context.Context) bool {
	full, _ := ctx.Value(fullCommandSurfaceKey{}).(bool)
	return full
}

func resourceCallerFactoryFromContext(ctx context.Context) ResourceCallerFactory {
	if factory, ok := ctx.Value(resourceCallerFactoryKey{}).(ResourceCallerFactory); ok {
		return factory
	}
	return defaultResourceCallerFactory
}

func defaultResourceCallerFactory(profileName, configPath string, resource spec.ResourceSpec, region string, getenv func(string) string) (engine.Caller, error) {
	caller, err := aliyun.NewOpenAPICaller(profileName, configPath, resourceAPIProduct(resource), region, getenv)
	if err != nil {
		return nil, err
	}
	caller.Resource = resource.Resource
	return caller, nil
}

func resourceAPIProduct(resource spec.ResourceSpec) string {
	if resource.APIProduct != "" {
		return resource.APIProduct
	}
	return resource.Product
}

const (
	resourceActionGroupResource      = "resource-operations"
	resourceCommandGroupResources    = "resource-types"
	resourceCommandGroupSubResources = "sub-resources"
	flagOrderedAnnotation            = "ecctl.flag.ordered"
	actionKeyAnnotation              = "ecctl.action"
	fieldSelectorInputName           = "fields"
	idempotencyKeyInputName          = "idempotency_key"
)

func resourceSpecErrorCommand(use string, short string, err error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			return ecerrors.Client("InvalidResourceSpec", err.Error())
		},
	}
}

type discoveredResource struct {
	ref      spec.ResourceRef
	resource spec.ResourceSpec
	err      error
}

type discoveredProduct struct {
	spec spec.ProductSpec
	err  error
}

func newProductCommands(options *globalOptions, stdout io.Writer, target productBuildTarget) []*cobra.Command {
	specDir := os.Getenv("ECCTL_SPEC_DIR")
	refs, err := spec.ListResources(specDir)
	if err != nil {
		return []*cobra.Command{resourceSpecErrorCommand("resources", "Manage cloud resources", err)}
	}

	refsByProduct := map[string][]spec.ResourceRef{}
	productsByName := map[string]discoveredProduct{}
	productNames := make([]string, 0)
	seenProducts := map[string]bool{}
	for _, ref := range refs {
		if !options.fullSurface && specDir == "" && !publicCLIResource(ref.Product, ref.Resource) {
			continue
		}
		refsByProduct[ref.Product] = append(refsByProduct[ref.Product], ref)
		if seenProducts[ref.Product] {
			continue
		}
		seenProducts[ref.Product] = true
		productNames = append(productNames, ref.Product)
		productSpec, productErr := spec.LoadProduct(specDir, ref.Product)
		if productErr != nil && os.IsNotExist(productErr) {
			productErr = nil
		}
		productsByName[ref.Product] = discoveredProduct{spec: productSpec, err: productErr}
	}

	sort.SliceStable(productNames, func(i, j int) bool {
		pi := productsByName[productNames[i]].spec.Priority
		pj := productsByName[productNames[j]].spec.Priority
		if pi != pj {
			if pi == 0 {
				return false
			}
			if pj == 0 {
				return true
			}
			return pi < pj
		}
		return productNames[i] < productNames[j]
	})

	commands := make([]*cobra.Command, 0, len(productNames))
	for _, product := range productNames {
		productSpec := productsByName[product]
		if productSpec.err != nil {
			commands = append(commands, resourceSpecErrorCommand(product, productCommandShort(product, productSpec.spec, options.lang), productSpec.err))
			continue
		}
		if target.buildAll || product == target.product {
			commands = append(commands, newProductCommand(options, stdout, product, productSpec, loadDiscoveredResources(specDir, refsByProduct[product]), target))
			continue
		}
		commands = append(commands, newProductStub(product, productSpec, options.lang))
	}
	return commands
}

func loadDiscoveredResources(specDir string, refs []spec.ResourceRef) []discoveredResource {
	resources := make([]discoveredResource, 0, len(refs))
	for _, ref := range refs {
		loaded, err := spec.LoadResourceWithParent(specDir, ref.Product, ref.Resource, ref.Parent)
		resources = append(resources, discoveredResource{
			ref:      ref,
			resource: loaded,
			err:      err,
		})
	}
	return resources
}

func newProductStub(product string, productSpec discoveredProduct, lang string) *cobra.Command {
	return groupCommandForLanguage(product, productCommandShort(product, productSpec.spec, lang), lang)
}

func newResourceStub(resource spec.ResourceSpec, lang string) *cobra.Command {
	cmd := groupCommandForLanguage(resource.Resource, resourceCommandShort(resource, lang), lang)
	cmd.Aliases = append([]string(nil), resource.Aliases...)
	return cmd
}

func isTargetResource(ref spec.ResourceRef, target productBuildTarget, resource spec.ResourceSpec) bool {
	if target.resource == ref.Resource || target.resource == ref.Parent {
		return true
	}
	for _, alias := range resource.Aliases {
		if target.resource == alias {
			return true
		}
	}
	return false
}

func newProductCommand(options *globalOptions, stdout io.Writer, product string, productSpec discoveredProduct, resources []discoveredResource, target productBuildTarget) *cobra.Command {
	resources = sortResourcesByImportance(resources, product, productSpec.spec.Resources)
	var cmd *cobra.Command
	var defaultResource *spec.ResourceSpec
	hasChildResources := productHasChildResources(product, resources)
	buildFullResource := target.buildAll || target.resource == ""
	for _, item := range resources {
		if item.ref.Resource != product {
			continue
		}
		if item.err != nil {
			cmd = resourceSpecErrorCommand(product, productCommandShort(product, productSpec.spec, options.lang), item.err)
		} else {
			cmd = newResourceCommand(options, stdout, item.resource)
			defaultResource = &item.resource
			if description := productSpec.spec.Description.Text(options.lang); description != "" {
				cmd.Short = description
			}
		}
		break
	}
	exposeDefaultResource := defaultResource != nil && product == "vpc" && defaultResource.Resource == product
	if defaultResource != nil && (len(defaultResource.Aliases) > 0 || exposeDefaultResource) {
		hasChildResources = true
	}
	if cmd == nil {
		cmd = groupCommandForLanguage(product, productCommandShort(product, productSpec.spec, options.lang), options.lang)
	}
	if productSpec.err != nil {
		return resourceSpecErrorCommand(product, productCommandShort(product, productSpec.spec, options.lang), productSpec.err)
	}
	childGroupID := resourceCommandGroupResources
	childGroupTitle := "Resource Types:"
	if hasChildResources {
		addCommandGroups(cmd, cobra.Group{ID: childGroupID, Title: childGroupTitle})
	}
	resourceCmds := map[string]*cobra.Command{}
	for _, item := range resources {
		if item.ref.Resource == product || item.ref.Parent != "" {
			continue
		}
		if item.err != nil {
			child := resourceSpecErrorCommand(item.ref.Resource, "Manage "+item.ref.Resource+" resources", item.err)
			child.GroupID = childGroupID
			cmd.AddCommand(child)
			continue
		}
		var child *cobra.Command
		if buildFullResource || isTargetResource(item.ref, target, item.resource) {
			child = newResourceSubcommand(options, stdout, item.resource)
		} else {
			child = newResourceStub(item.resource, options.lang)
		}
		child.GroupID = childGroupID
		cmd.AddCommand(child)
		resourceCmds[item.ref.Resource] = child
	}
	if defaultResource != nil {
		if exposeDefaultResource {
			var child *cobra.Command
			if buildFullResource || target.resource == defaultResource.Resource {
				child = newResourceSubcommand(options, stdout, *defaultResource)
			} else {
				child = newResourceStub(*defaultResource, options.lang)
			}
			child.GroupID = childGroupID
			cmd.AddCommand(child)
			resourceCmds[defaultResource.Resource] = child
		}
		for _, alias := range defaultResource.Aliases {
			if _, exists := resourceCmds[alias]; exists || alias == product {
				continue
			}
			var child *cobra.Command
			if buildFullResource || target.resource == alias || target.resource == defaultResource.Resource {
				child = newDefaultResourceAliasSubcommand(options, stdout, *defaultResource, alias)
			} else {
				child = newResourceStub(*defaultResource, options.lang)
				child.Use = alias
			}
			child.GroupID = childGroupID
			cmd.AddCommand(child)
			resourceCmds[alias] = child
		}
	}
	for _, item := range resources {
		if item.ref.Parent == "" {
			continue
		}
		parentCmd := resourceCmds[item.ref.Parent]
		if parentCmd == nil {
			continue
		}
		if item.err != nil {
			parentCmd.AddCommand(resourceSpecErrorCommand(item.ref.Resource, "Manage "+item.ref.Resource, item.err))
			continue
		}
		addSubResourceGroups(parentCmd)
		var child *cobra.Command
		if buildFullResource || isTargetResource(item.ref, target, item.resource) {
			child = newResourceSubcommand(options, stdout, item.resource)
		} else {
			child = newResourceStub(item.resource, options.lang)
		}
		child.GroupID = resourceCommandGroupSubResources
		parentCmd.AddCommand(child)
	}
	if examples := keyExamples(productSpec.spec.Examples); len(examples) > 0 {
		examples = publicCLIExamples(examples, options.fullSurface)
		cmd.Example = formatCommandExamples(examples)
	}
	return cmd
}

func publicCLIResource(product string, resource string) bool {
	switch product {
	case "ecs", "vpc":
		return true
	case "ack":
		switch resource {
		case "ack", "nodepool", "node", "region", "kubeconfig", "permission", "version":
			return true
		}
	case "lingjun":
		switch resource {
		case "cluster", "vpd":
			return true
		}
	}
	return false
}

func publicCLICommandAllowed(args []string) bool {
	positionals := commandPositionals(args)
	if len(positionals) == 0 {
		return true
	}
	if positionals[0] == "help" {
		if len(positionals) == 1 {
			return true
		}
		positionals = positionals[1:]
	}
	product := positionals[0]
	if isBuiltinRootCommand(product) {
		return true
	}
	switch product {
	case "ecs":
		return true
	case "vpc":
		return true
	case "ack", "lingjun":
		if len(positionals) == 1 {
			return true
		}
		if publicCLIDefaultResourceAction(product, positionals[1]) {
			return true
		}
		return publicCLIResourceIdentifier(product, positionals[1])
	default:
		return false
	}
}

func publicCLIDefaultResourceAction(product string, action string) bool {
	if product != "ack" || !publicCLIResource(product, product) {
		return false
	}
	switch action {
	case "create", "delete", "get", "list", "update", "upgrade":
		return true
	default:
		return false
	}
}

func publicCLIResourceIdentifier(product string, requested string) bool {
	if publicCLIResource(product, requested) {
		return true
	}
	switch product {
	case "ack":
		switch requested {
		case "cluster":
			return publicCLIResource(product, "ack")
		case "kc":
			return publicCLIResource(product, "kubeconfig")
		case "np":
			return publicCLIResource(product, "nodepool")
		}
	case "lingjun":
		if requested == "ng" {
			return publicCLIResource(product, "node-group")
		}
	}
	return false
}

func publicCLIExamples(examples []string, fullSurface bool) []string {
	if fullSurface {
		return examples
	}
	filtered := make([]string, 0, len(examples))
	for _, example := range examples {
		if publicCLIExample(example) {
			filtered = append(filtered, example)
		}
	}
	return filtered
}

func publicCLIExample(example string) bool {
	fields := strings.Fields(example)
	if len(fields) == 0 {
		return true
	}
	if fields[0] == "ecctl" {
		fields = fields[1:]
	}
	positionals := commandPositionals(fields)
	if len(positionals) == 0 || isBuiltinRootCommand(positionals[0]) {
		return true
	}
	product := positionals[0]
	switch product {
	case "ecs", "vpc":
		return true
	case "ack", "lingjun":
		if len(positionals) < 2 {
			return false
		}
		return publicCLIResourceIdentifier(product, positionals[1])
	default:
		return false
	}
}

func keyExamples(examples []string) []string {
	selected := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if example == "" || seen[example] {
			continue
		}
		selected = append(selected, example)
		seen[example] = true
		if len(selected) == 4 {
			break
		}
	}
	return selected
}

func formatCommandExamples(examples []string) string {
	if len(examples) == 0 {
		return ""
	}
	lines := make([]string, 0, len(examples))
	for _, example := range examples {
		lines = append(lines, "  "+example)
	}
	return strings.Join(lines, "\n")
}

func sortResourcesByImportance(resources []discoveredResource, product string, order []string) []discoveredResource {
	if len(order) == 0 {
		return resources
	}
	rank := map[string]int{}
	for i, name := range order {
		rank[name] = i + 1
	}
	sorted := make([]discoveredResource, len(resources))
	copy(sorted, resources)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri := sorted[i].ref
		rj := sorted[j].ref
		if ri.Resource == product || rj.Resource == product {
			return ri.Resource == product
		}
		if ri.Parent != "" || rj.Parent != "" {
			return false
		}
		oi, oki := rank[ri.Resource]
		oj, okj := rank[rj.Resource]
		if oki && okj {
			return oi < oj
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return ri.Resource < rj.Resource
	})
	return sorted
}

func productHasChildResources(product string, resources []discoveredResource) bool {
	for _, item := range resources {
		if item.ref.Resource != product && item.ref.Parent == "" {
			return true
		}
	}
	return false
}

func addSubResourceGroups(cmd *cobra.Command) {
	for _, group := range cmd.Groups() {
		if group.ID == resourceCommandGroupSubResources {
			return
		}
	}
	addCommandGroups(cmd, cobra.Group{ID: resourceCommandGroupSubResources, Title: "Sub-Resources:"})
}

func productCommandShort(product string, productSpec spec.ProductSpec, lang string) string {
	if description := productSpec.Description.Text(lang); description != "" {
		return description
	}
	return "Manage " + strings.ToUpper(product) + " resources"
}

func newResourceCommand(options *globalOptions, stdout io.Writer, resource spec.ResourceSpec) *cobra.Command {
	cmd := groupCommandForLanguage(resource.Product, resourceCommandShort(resource, options.lang), options.lang)
	cmd.Long = resourceCommandLong(resource, options.lang)
	cmd.Example = formatCommandExamples(keyExamples(resource.Examples))
	preserveCommandOrderInHelp(cmd)
	addResourceActionGroups(cmd, resource)
	addResourceActionCommands(cmd, options, stdout, resource)
	return cmd
}

func newResourceSubcommand(options *globalOptions, stdout io.Writer, resource spec.ResourceSpec) *cobra.Command {
	cmd := groupCommandForLanguage(resource.Resource, resourceCommandShort(resource, options.lang), options.lang)
	cmd.Long = resourceCommandLong(resource, options.lang)
	cmd.Example = formatCommandExamples(keyExamples(resource.Examples))
	preserveCommandOrderInHelp(cmd)
	addResourceActionGroups(cmd, resource)
	cmd.Aliases = append([]string(nil), resource.Aliases...)
	addResourceActionCommands(cmd, options, stdout, resource)
	return cmd
}

func newDefaultResourceAliasSubcommand(options *globalOptions, stdout io.Writer, resource spec.ResourceSpec, alias string) *cobra.Command {
	cmd := groupCommandForLanguage(alias, resourceCommandShort(resource, options.lang), options.lang)
	cmd.Long = resourceCommandLong(resource, options.lang)
	cmd.Example = formatCommandExamples(keyExamples(resource.Examples))
	preserveCommandOrderInHelp(cmd)
	addResourceActionGroups(cmd, resource)
	addResourceActionCommands(cmd, options, stdout, resource)
	return cmd
}

func addResourceActionGroups(cmd *cobra.Command, resource spec.ResourceSpec) {
	groups := resourceActionGroups(resource)
	if len(groups) == 0 {
		return
	}
	addCommandGroups(cmd, groups...)
}

func addCommandGroups(cmd *cobra.Command, groups ...cobra.Group) {
	cobraGroups := make([]*cobra.Group, 0, len(groups))
	for i := range groups {
		cobraGroups = append(cobraGroups, &groups[i])
	}
	cmd.AddGroup(cobraGroups...)
}

func resourceActionGroups(resource spec.ResourceSpec) []cobra.Group {
	if len(orderedResourceActionNames(resource)) == 0 {
		return nil
	}
	return []cobra.Group{{
		ID:    resourceActionGroupID(""),
		Title: resourceActionGroupTitle(""),
	}}
}

func resourceActionGroupID(_ string) string {
	return resourceActionGroupResource
}

func resourceActionGroupTitle(_ string) string {
	return "Resource Operations:"
}

func addResourceActionCommands(cmd *cobra.Command, options *globalOptions, stdout io.Writer, resource spec.ResourceSpec) {
	for _, actionName := range orderedResourceActionNames(resource) {
		operation := resource.Operations[actionName]
		actionCmd := newResourceActionCommand(options, stdout, resource, actionName, operation)
		actionCmd.GroupID = resourceActionGroup(cmd.Groups())
		cmd.AddCommand(actionCmd)
	}
}

func resourceActionGroup(groups []*cobra.Group) string {
	if len(groups) == 0 {
		return ""
	}
	return resourceActionGroupID("")
}

func preserveCommandOrderInHelp(cmd *cobra.Command) {
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(command *cobra.Command, args []string) {
		defaultHelp(command, args)
	})
}

func newResourceActionCommand(options *globalOptions, stdout io.Writer, resource spec.ResourceSpec, actionName string, operation spec.Operation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceActionUse(resource, actionName, operation),
		Short: resourceActionShort(resource, actionName, options.lang),
		Long:  resourceActionLong(resource, actionName, options.lang),
		Args:  resourceActionArgs(resource, actionName, operation),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResourceAction(cmd, options, stdout, resource, actionName, operation, args)
		},
	}
	if examples := keyExamples(operation.Examples); len(examples) > 0 {
		cmd.Example = formatCommandExamples(examples)
	}
	if len(operation.Aliases) > 0 {
		cmd.Aliases = append([]string(nil), operation.Aliases...)
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[actionKeyAnnotation] = resource.Product + "." + resource.Resource + "." + actionName
	addResourceActionFlags(cmd, resource, operation, options.lang)
	return cmd
}

func orderedResourceActionNames(resource spec.ResourceSpec) []string {
	if names, ok := orderedOperationNames(resource); ok {
		return names
	}
	return nil
}

func resourceCommandShort(resource spec.ResourceSpec, lang string) string {
	if description := resource.Description.Text(lang); description != "" {
		return description
	}
	return "Manage " + resource.Resource + " resources"
}

func resourceCommandLong(resource spec.ResourceSpec, lang string) string {
	help := strings.TrimSpace(resource.Help.Text(lang))
	if help == "" {
		return ""
	}
	return resourceCommandShort(resource, lang) + "\n\n" + help
}

func resourceActionShort(resource spec.ResourceSpec, action string, lang string) string {
	if description := operationDescription(resource, action, lang); description != "" {
		return description
	}
	return strings.Title(action) + " " + resource.Resource
}

func resourceActionLong(resource spec.ResourceSpec, action string, lang string) string {
	operation, ok := resource.Operations[action]
	if !ok {
		return ""
	}
	help := strings.TrimSpace(operation.Help.Text(lang))
	if help == "" {
		return ""
	}
	short := resourceActionShort(resource, action, lang)
	if short == "" {
		return help
	}
	return short + "\n\n" + help
}

func resourceActionUse(resource spec.ResourceSpec, actionName string, operation spec.Operation) string {
	parts := []string{actionName}
	for _, input := range positionalInputSpecs(resource, actionName, operation) {
		name := input.name
		param := input.param
		display := resourceCLIFlagName(resource, name, param)
		if param.PositionalMany {
			parts = append(parts, "["+display+"...]")
			continue
		}
		if param.Required {
			parts = append(parts, "<"+display+">")
		} else {
			parts = append(parts, "["+display+"]")
		}
	}
	return strings.Join(parts, " ")
}

func resourceActionArgs(resource spec.ResourceSpec, actionName string, operation spec.Operation) cobra.PositionalArgs {
	positional := positionalInputSpecs(resource, actionName, operation)
	if len(positional) == 0 {
		return cobra.NoArgs
	}
	for _, input := range positional {
		if input.param.PositionalMany {
			return cobra.ArbitraryArgs
		}
	}
	required := 0
	for _, input := range positional {
		if input.param.Required {
			required++
		}
	}
	return argsRange(required, len(positional))
}

func positionalParamNames(operation spec.Operation) []string {
	return positionalInputNames(operationInputSpecsFromOperation(operation))
}

func positionalInputSpecs(resource spec.ResourceSpec, actionName string, operation spec.Operation) []resourceInputSpec {
	return positionalInputSpecList(resourceActionInputSpecs(resource, actionName, operation))
}

func positionalInputSpecList(inputs []resourceInputSpec) []resourceInputSpec {
	positionals := make([]resourceInputSpec, 0)
	for _, input := range inputs {
		if input.param.Positional || input.param.PositionalMany {
			positionals = append(positionals, input)
		}
	}
	return positionals
}

func positionalInputNames(inputs []resourceInputSpec) []string {
	names := make([]string, 0)
	for _, input := range inputs {
		if input.param.Positional || input.param.PositionalMany {
			names = append(names, input.name)
		}
	}
	return names
}

func addResourceActionFlags(cmd *cobra.Command, resource spec.ResourceSpec, operation spec.Operation, lang string) {
	inputs := resourceActionInputSpecs(resource, cmd.Name(), operation)
	ordered := len(inputs) > 0
	for _, input := range inputs {
		name := input.name
		param := input.param
		if param.Positional && !param.PositionalMany {
			continue
		}
		flag := resourceCLIFlagName(resource, name, param)
		addResourceFlag(cmd.Flags(), resource, name, param, resourceFlagUsage(resource, cmd.Name(), name, param, lang))
		if cmd.Name() == "get" && name == "next_token" {
			if hidden := cmd.Flags().Lookup(flag); hidden != nil {
				hidden.Hidden = true
			}
		}
		if ordered {
			markFlagAnnotation(cmd, flagOrderedAnnotation, "true", flag)
		}
		if isResourceActionFlag(name) {
			markResourceFlags(cmd, flag)
		}
		if input.hasBrief && !input.brief {
			markFlagAnnotation(cmd, flagBriefAnnotation, "false", flag)
		}
		if param.Required {
			markRequiredHelpFlags(cmd, flag)
		}
	}
	if ordered {
		cmd.Flags().SortFlags = false
	}
}

func addResourceFlag(flags *pflag.FlagSet, resource spec.ResourceSpec, name string, param spec.Param, usage string) {
	flag := resourceCLIFlagName(resource, name, param)
	if isObjectArrayInput(resource, name, param) {
		flags.StringArray(flag, nil, usage)
		return
	}
	switch param.Type {
	case "boolean":
		flags.Bool(flag, boolDefault(param.Default), usage)
	case "integer":
		flags.Int(flag, intDefault(param.Default), usage)
	case "float":
		flags.Float64(flag, floatDefault(param.Default), usage)
	case "number":
		flags.Float64(flag, floatDefault(param.Default), usage)
	case "duration":
		flags.Duration(flag, durationDefault(param.Default), usage)
	case "key=value":
		if param.Repeatable {
			flags.StringArray(flag, nil, usage)
		} else {
			flags.String(flag, "", usage)
		}
	case "key_value":
		if param.Repeatable {
			flags.StringArray(flag, nil, usage)
		} else {
			flags.String(flag, "", usage)
		}
	case "array", "object":
		flags.String(flag, stringDefault(param.Default), usage)
	case "string_array":
		if name == "ids" {
			flags.String(flag, "", usage)
		} else {
			flags.StringSlice(flag, nil, usage)
		}
	default:
		if param.Repeatable {
			flags.StringArray(flag, nil, usage)
		} else {
			flags.String(flag, stringDefault(param.Default), usage)
		}
	}
}

func resourceFlagUsage(resource spec.ResourceSpec, actionName string, name string, param spec.Param, lang string) string {
	_ = actionName
	usage := paramUsage(resource, name, param, lang)
	if param.Max != nil {
		usage += i18n.NewLocalizer(lang).MessageData("HelpMaxValue", map[string]any{"Value": *param.Max})
	}
	if isObjectArrayInput(resource, name, param) || isObjectInput(resource, name, param) {
		usage += " (" + structuredInputShape(schemaFieldForInput(resource, name), isObjectArrayInput(resource, name, param), lang) + ")"
	} else if param.Use != "" {
		usage += " (" + localizedInputStyle(param.Use, lang) + ")"
	}
	return usage
}

func localizedInputStyle(use string, lang string) string {
	localizer := i18n.NewLocalizer(lang)
	if use == "+value|-value" {
		return localizer.MessageOrDefault("InputStyleSignedValue", "+value or -value")
	}
	separator := localizer.MessageOrDefault("InputStyleSeparator", " or ")
	return strings.Join(strings.Split(use, "|"), separator)
}

func supportedFilterFields(filters map[string]spec.Filter) []string {
	fields := make([]string, 0, len(filters))
	for name, filter := range filters {
		prefix := firstNonEmpty(filter.KeyPrefix, name)
		if strings.HasSuffix(prefix, ".") {
			fields = append(fields, prefix+"<key>")
			continue
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

func sortedParamNames(params map[string]spec.Param) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return flagName(names[i]) < flagName(names[j])
	})
	return names
}

type resourceInputSpec struct {
	name     string
	param    spec.Param
	brief    bool
	hasBrief bool
}

func resourceActionInputSpecs(resource spec.ResourceSpec, actionName string, operation spec.Operation) []resourceInputSpec {
	if inputs, ok := operationInputSpecs(resource, actionName); ok {
		return inputs
	}
	return operationInputSpecsFromOperation(operation)
}

func operationInputSpecsFromOperation(operation spec.Operation) []resourceInputSpec {
	inputs := make([]resourceInputSpec, 0, len(operation.Input.Fields)+len(operation.Input.Controls))
	for _, ref := range operation.Input.Fields {
		inputs = append(inputs, resourceInputSpecFromRef(ref, paramFromOperationRef(ref)))
	}
	for _, ref := range operation.Input.Controls {
		inputs = append(inputs, resourceInputSpecFromRef(ref, paramFromOperationRef(ref)))
	}
	return inputs
}

func resourceInputSpecFromRef(ref spec.OperationFieldRef, param spec.Param) resourceInputSpec {
	return resourceInputSpec{
		name:     ref.Name,
		param:    param,
		brief:    ref.Brief,
		hasBrief: ref.HasBrief,
	}
}

func paramFromOperationRef(ref spec.OperationFieldRef) spec.Param {
	param := spec.Param{Type: "string"}
	applyOperationParamOverrides(&param, ref)
	return param
}

func requiredInputFlagStatuses(resource spec.ResourceSpec, inputs []resourceInputSpec, input map[string]any) []requiredFlagStatus {
	params := map[string]spec.Param{}
	for _, inputSpec := range inputs {
		params[inputSpec.name] = inputSpec.param
	}
	return requiredFlagStatuses(resource, params, input)
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

func operationDescription(resource spec.ResourceSpec, actionName string, lang string) string {
	operation, ok := resource.Operations[actionName]
	if !ok {
		return ""
	}
	return operation.Description.Text(lang)
}

func operationInputSpecs(resource spec.ResourceSpec, actionName string) ([]resourceInputSpec, bool) {
	operation, ok := resource.Operations[actionName]
	if !ok {
		return nil, false
	}
	refs := make([]spec.OperationFieldRef, 0, len(operation.Input.Fields)+len(operation.Input.Controls))
	refs = append(refs, operation.Input.Fields...)
	refs = append(refs, operation.Input.Controls...)
	inputs := make([]resourceInputSpec, 0, len(refs))
	var apiParam *resourceInputSpec
	for _, ref := range refs {
		param, ok := operationInputParam(resource, ref)
		if !ok {
			continue
		}
		inputSpec := resourceInputSpecFromRef(ref, param)
		if ref.Name == "api_param" {
			apiParam = &inputSpec
			continue
		}
		inputs = append(inputs, inputSpec)
	}
	if idempotency, ok := spec.OperationIdempotency(resource, operation); ok {
		inputs = append(inputs, resourceInputSpec{name: idempotencyKeyInputName, param: idempotencyKeyParam(idempotency)})
	}
	if apiParam != nil {
		inputs = append(inputs, *apiParam)
	}
	if actionSupportsFieldSelection(actionName) {
		inputs = append(inputs, resourceInputSpec{name: fieldSelectorInputName, param: fieldSelectorParam()})
	}
	return inputs, true
}

func actionSupportsFieldSelection(actionName string) bool {
	return actionName == "list" || actionName == "get"
}

func fieldSelectorParam() spec.Param {
	return spec.Param{
		Type: "string",
	}
}

func idempotencyKeyParam(idempotency spec.Idempotency) spec.Param {
	field := idempotency.Field
	if field == "" {
		field = "idempotency field"
	}
	return spec.Param{
		Type: "string",
		Description: spec.LocalizedText{
			"en":    "Idempotency key mapped to " + field + "; omit to use an auto-generated token for compatibility.",
			"zh-CN": "幂等键，映射到 " + field + "；省略时为兼容性自动生成 token。",
		},
	}
}

func operationInputParam(resource spec.ResourceSpec, ref spec.OperationFieldRef) (spec.Param, bool) {
	definition, ok := resource.Schema.Fields[ref.Name]
	if !ok {
		definition, ok = resource.Controls[ref.Name]
	}
	if !ok {
		return spec.Param{}, false
	}
	param := paramFromDefinition(definition)
	applyOperationParamOverrides(&param, ref)
	return param, true
}

func paramFromDefinition(value spec.SchemaField) spec.Param {
	return spec.Param{
		Type:           value.Type,
		Description:    value.Description,
		Required:       value.Required,
		Repeatable:     value.Repeatable,
		Positional:     value.Positional,
		PositionalMany: value.PositionalMany,
		AllowEmpty:     value.AllowEmpty,
		Default:        value.Default,
		Max:            value.Max,
		Enum:           value.Enum,
		Use:            value.InputStyle,
	}
}

func applyOperationParamOverrides(param *spec.Param, ref spec.OperationFieldRef) {
	if ref.HasRequired {
		param.Required = ref.Required
	}
	if ref.Positional {
		param.Positional = true
	}
	if ref.PositionalMany {
		param.PositionalMany = true
	}
	if ref.AllowEmpty {
		param.AllowEmpty = true
	}
	if ref.FlagName != "" {
		param.FlagName = ref.FlagName
	}
	if ref.HasRepeatable {
		param.Repeatable = ref.Repeatable
	}
	if ref.Default != nil {
		param.Default = ref.Default
	}
	if len(ref.Enum) > 0 {
		param.Enum = ref.Enum
	}
	if len(ref.Description) > 0 {
		param.Description = ref.Description
	}
	if ref.InputStyle != "" {
		param.Use = ref.InputStyle
	}
}

func requiredFlagStatuses(resource spec.ResourceSpec, params map[string]spec.Param, input map[string]any) []requiredFlagStatus {
	names := make([]string, 0)
	seen := map[string]bool{}
	for _, name := range []string{"name", "cidr", "id"} {
		param, ok := params[name]
		if ok && param.Required && !param.Positional {
			names = append(names, name)
			seen[name] = true
		}
	}
	for _, name := range sortedParamNames(params) {
		param := params[name]
		if seen[name] || !param.Required || param.Positional {
			continue
		}
		names = append(names, name)
	}
	statuses := make([]requiredFlagStatus, 0, len(names))
	for _, name := range names {
		statuses = append(statuses, requiredFlag("--"+resourceCLIFlagName(resource, name, params[name]), isInputValueEmpty(input[name])))
	}
	return statuses
}

func runResourceAction(cmd *cobra.Command, options *globalOptions, stdout io.Writer, resource spec.ResourceSpec, actionName string, operation spec.Operation, args []string) error {
	region, err := resolveRegion(options)
	if err != nil {
		return err
	}
	input, timeout, err := resourceActionInput(cmd, resource, actionName, operation, args)
	if err != nil {
		return err
	}
	if err := applyFilterInput(resource, actionName, input); err != nil {
		return err
	}
	if err := validateResourcePagination(input); err != nil {
		return err
	}
	if err := validateResourceActionInput(resource, actionName, operation, input); err != nil {
		return err
	}
	selectedFields, err := selectedOutputFields(resource, actionName, operation, input)
	if err != nil {
		return err
	}
	delete(input, fieldSelectorInputName)
	callerFactory := resourceCallerFactoryFromContext(cmd.Context())
	caller, err := callerFactory(config.ProfileName(options.profile, os.Getenv), config.ConfigPath(os.Getenv), resource, region, os.Getenv)
	if err != nil {
		return err
	}
	result, err := engine.NewExecutor(resource, caller).Execute(cmd.Context(), engine.Request{
		Action:  actionName,
		Input:   input,
		Context: map[string]any{"region": region},
		Timeout: timeout,
	})
	if err != nil {
		return err
	}
	result = cropResultFields(result, selectedFields)
	return writeCommandOutput(options, stdout, resourceActionPayload(resource, actionName, operation, input, region, result))
}

func selectedOutputFields(resource spec.ResourceSpec, actionName string, operation spec.Operation, input map[string]any) ([]string, error) {
	raw, _ := input[fieldSelectorInputName].(string)
	fields := parseFieldSelector(raw)
	if len(fields) == 0 {
		return nil, nil
	}
	allowed := outputFieldSet(resource, operation)
	for _, field := range fields {
		if !allowed[field] {
			return nil, ecerrors.Client("InvalidFields", "unsupported field "+field+"; supported fields: "+strings.Join(sortedFieldNames(allowed), ", "),
				ecerrors.WithField(field),
				ecerrors.WithAcceptedValues(sortedFieldNames(allowed)...),
				ecerrors.WithSuggestedAction(fmt.Sprintf("Run `ecctl schema %s.%s.%s` to list supported output fields.", resource.Product, resource.Resource, actionName)),
			)
		}
	}
	return fields, nil
}

func parseFieldSelector(raw string) []string {
	fields := make([]string, 0)
	seen := map[string]bool{}
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		fields = append(fields, field)
	}
	return fields
}

func outputFieldSet(resource spec.ResourceSpec, operation spec.Operation) map[string]bool {
	fields := map[string]bool{}
	for _, step := range operation.Workflow {
		if step.Probe == "" {
			continue
		}
		probe, ok := resource.Probes[step.Probe]
		if !ok {
			continue
		}
		for name := range probe.Response.Fields {
			fields[name] = true
		}
	}
	for name := range operation.Output.Fields {
		fields[name] = true
	}
	return fields
}

func sortedFieldNames(fields map[string]bool) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cropResultFields(result engine.Result, fields []string) engine.Result {
	if len(fields) == 0 {
		return result
	}
	result.Item = cropOutputObject(result.Item, fields)
	if len(result.Items) > 0 {
		items := make([]map[string]any, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, cropOutputObject(item, fields))
		}
		result.Items = items
	}
	return result
}

func cropOutputObject(item map[string]any, fields []string) map[string]any {
	if item == nil {
		return nil
	}
	cropped := map[string]any{}
	for _, field := range fields {
		if value, ok := item[field]; ok {
			cropped[field] = value
		}
	}
	return cropped
}

func resourceActionInput(cmd *cobra.Command, resource spec.ResourceSpec, actionName string, operation spec.Operation, args []string) (map[string]any, time.Duration, error) {
	input := map[string]any{}
	inputs := resourceActionInputSpecs(resource, actionName, operation)
	positionals := positionalInputSpecList(inputs)
	for index, inputSpec := range positionals {
		name := inputSpec.name
		param := inputSpec.param
		if param.PositionalMany {
			explicit := ""
			if flag := cmd.Flags().Lookup(flagName(name)); flag != nil {
				explicit = flag.Value.String()
			}
			if strings.HasPrefix(strings.TrimSpace(explicit), "[") {
				return nil, 0, ecerrors.Client("InvalidIDs", "--ids accepts comma-separated IDs, not JSON arrays")
			}
			input[name] = splitIDs(args, explicit)
			continue
		}
		if index < len(args) {
			input[name] = args[index]
		}
	}

	var timeout time.Duration
	for _, inputSpec := range inputs {
		name := inputSpec.name
		param := inputSpec.param
		if param.Positional && !param.PositionalMany {
			continue
		}
		if param.PositionalMany {
			continue
		}
		value, assigned, err := flagInputValue(cmd, resource, name, param)
		if err != nil {
			return nil, 0, err
		}
		if assigned {
			input[name] = value
			if name == "timeout" {
				timeout, _ = value.(time.Duration)
			}
		}
		if assigned {
			if err := validateParamEnum(resourceCLIFlagName(resource, name, param), param, value); err != nil {
				return nil, 0, err
			}
			if err := validateParamMax(resourceCLIFlagName(resource, name, param), param, value); err != nil {
				return nil, 0, err
			}
			if err := validateParamInputStyle(resourceCLIFlagName(resource, name, param), param, value); err != nil {
				return nil, 0, err
			}
		}
	}
	if err := missingRequiredFlags(requiredInputFlagStatuses(resource, inputs, input)...); err != nil {
		return nil, 0, err
	}
	return input, timeout, nil
}

func flagInputValue(cmd *cobra.Command, resource spec.ResourceSpec, name string, param spec.Param) (any, bool, error) {
	flag := cmd.Flags().Lookup(resourceCLIFlagName(resource, name, param))
	if flag == nil {
		return nil, false, nil
	}
	assigned := flag.Changed || param.Default != nil || param.Required
	if !assigned {
		return nil, false, nil
	}
	if isObjectArrayInput(resource, name, param) {
		value, err := cmd.Flags().GetStringArray(flag.Name)
		if err != nil {
			return nil, assigned, err
		}
		return structuredObjectArrayFlagValue(value, flag.Name, schemaFieldForInput(resource, name), assigned)
	}
	switch param.Type {
	case "boolean":
		value, err := cmd.Flags().GetBool(flag.Name)
		return value, assigned, err
	case "integer":
		value, err := cmd.Flags().GetInt(flag.Name)
		return value, assigned, err
	case "float":
		value, err := cmd.Flags().GetFloat64(flag.Name)
		return value, assigned, err
	case "number":
		value, err := cmd.Flags().GetFloat64(flag.Name)
		return value, assigned, err
	case "duration":
		value, err := cmd.Flags().GetDuration(flag.Name)
		return value, assigned, err
	case "key=value":
		if param.Repeatable {
			value, err := cmd.Flags().GetStringArray(flag.Name)
			return value, assigned, err
		}
		return flag.Value.String(), assigned, nil
	case "key_value":
		if param.Repeatable {
			value, err := cmd.Flags().GetStringArray(flag.Name)
			return value, assigned, err
		}
		return flag.Value.String(), assigned, nil
	case "array":
		return jsonFlagValue(flag.Value.String(), name, "array", assigned)
	case "object":
		return structuredObjectFlagValue(flag.Value.String(), flag.Name, schemaFieldForInput(resource, name), assigned)
	case "string_array":
		value, err := cmd.Flags().GetStringSlice(flag.Name)
		return value, assigned, err
	default:
		if param.Repeatable {
			value, err := cmd.Flags().GetStringArray(flag.Name)
			return value, assigned, err
		}
		return stringFlagValue(flag.Value.String(), flag.Name, param, assigned)
	}
}

func stringFlagValue(raw string, flag string, param spec.Param, assigned bool) (any, bool, error) {
	if !strings.Contains(param.Use, "@file") || !strings.HasPrefix(raw, "@") {
		return raw, assigned, nil
	}
	path := strings.TrimPrefix(raw, "@")
	if path == "" {
		return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" @file could not be read")
	}
	loaded, err := os.ReadFile(path)
	if err != nil {
		return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" @file could not be read")
	}
	return string(loaded), assigned, nil
}

func jsonFlagValue(raw string, name string, kind string, assigned bool) (any, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, assigned, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flagName(name)+" must be a JSON "+kind)
	}
	switch kind {
	case "array":
		if _, ok := decoded.([]any); !ok {
			return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flagName(name)+" must be a JSON array")
		}
	case "object":
		if _, ok := decoded.(map[string]any); !ok {
			return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flagName(name)+" must be a JSON object")
		}
	}
	return decoded, assigned, nil
}

func structuredObjectArrayFlagValue(values []string, flag string, field spec.SchemaField, assigned bool) (any, bool, error) {
	itemField := spec.SchemaField{Type: "object"}
	if field.Items != nil {
		itemField = *field.Items
	}
	items := make([]any, 0, len(values))
	for _, raw := range values {
		value, _, err := structuredObjectFlagValue(raw, flag, itemField, assigned)
		if err != nil {
			return nil, assigned, err
		}
		if !isInputValueEmpty(value) {
			items = append(items, value)
		}
	}
	return items, assigned, nil
}

func structuredObjectFlagValue(raw string, flag string, field spec.SchemaField, assigned bool) (any, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, assigned, nil
	}
	if strings.HasPrefix(raw, "@") {
		loaded, err := os.ReadFile(strings.TrimPrefix(raw, "@"))
		if err != nil {
			return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" @file could not be read")
		}
		raw = strings.TrimSpace(string(loaded))
	}
	if strings.HasPrefix(raw, "{") {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" must be a JSON object")
		}
		normalized, err := normalizeStructuredObject(decoded, field)
		if err != nil {
			return nil, assigned, err
		}
		if err := validateStructuredObject(normalized, flag, field); err != nil {
			return nil, assigned, err
		}
		return normalized, assigned, nil
	}
	if strings.HasPrefix(raw, "[") {
		return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" accepts one JSON object per flag, not a JSON array")
	}
	if !isShallowObjectField(field) {
		return nil, assigned, ecerrors.Client("InvalidParameter", "--"+flag+" must be a JSON object or @file")
	}
	parsed, err := parseInlineObject(raw, field)
	if err != nil {
		return nil, assigned, err
	}
	if err := validateStructuredObject(parsed, flag, field); err != nil {
		return nil, assigned, err
	}
	return parsed, assigned, nil
}

func parseInlineObject(raw string, field spec.SchemaField) (map[string]any, error) {
	result := map[string]any{}
	for _, assignment := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(assignment, "=")
		key = strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			return nil, ecerrors.Client("InvalidParameter", "structured parameter entries must be key=value")
		}
		child, ok := field.Fields[key]
		if len(field.Fields) > 0 && !ok {
			return nil, ecerrors.Client("InvalidParameter", "structured parameter field "+key+" is not supported")
		}
		coerced, err := coerceStructuredValue(value, child)
		if err != nil {
			return nil, err
		}
		result[key] = coerced
	}
	return result, nil
}

func normalizeStructuredObject(raw map[string]any, field spec.SchemaField) (map[string]any, error) {
	result := map[string]any{}
	for key, value := range raw {
		name := strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
		child, ok := field.Fields[name]
		if len(field.Fields) > 0 && !ok {
			return nil, ecerrors.Client("InvalidParameter", "structured parameter field "+name+" is not supported")
		}
		coerced, err := coerceStructuredValue(value, child)
		if err != nil {
			return nil, err
		}
		result[name] = coerced
	}
	return result, nil
}

func validateStructuredObject(object map[string]any, flag string, field spec.SchemaField) error {
	for name, child := range field.Fields {
		value, ok := object[name]
		if child.Required && (!ok || isInputValueEmpty(value)) {
			return ecerrors.Client("MissingParameter", "--"+flag+" requires field "+strings.ReplaceAll(name, "_", "-"))
		}
		if ok && len(child.Enum) > 0 && !isInputValueEmpty(value) {
			if !structuredValueInEnum(value, child.Enum) {
				return ecerrors.Client("InvalidParameter", "--"+flag+" field "+strings.ReplaceAll(name, "_", "-")+" must be one of "+strings.Join(child.Enum, ", "))
			}
		}
	}
	return nil
}

func structuredValueInEnum(value any, allowed []string) bool {
	got := fmt.Sprint(value)
	for _, item := range allowed {
		if got == item {
			return true
		}
	}
	return false
}

func coerceStructuredValue(value any, field spec.SchemaField) (any, error) {
	switch field.Type {
	case "integer":
		switch typed := value.(type) {
		case int:
			return typed, nil
		case float64:
			return int(typed), nil
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err != nil {
				return nil, ecerrors.Client("InvalidParameter", "structured parameter value must be an integer")
			}
			return parsed, nil
		default:
			return value, nil
		}
	case "number", "float":
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case int:
			return float64(typed), nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err != nil {
				return nil, ecerrors.Client("InvalidParameter", "structured parameter value must be a number")
			}
			return parsed, nil
		default:
			return value, nil
		}
	case "boolean":
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err != nil {
				return nil, ecerrors.Client("InvalidParameter", "structured parameter value must be a boolean")
			}
			return parsed, nil
		default:
			return value, nil
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		return normalizeStructuredObject(object, field)
	case "array", "string_array":
		if text, ok := value.(string); ok {
			return splitIDs(nil, text), nil
		}
	}
	return value, nil
}

func applyFilterInput(resource spec.ResourceSpec, actionName string, input map[string]any) error {
	filters, _ := input["filter"].([]string)
	if len(filters) == 0 {
		return nil
	}
	actionFilters := resourceActionFilters(resource, actionName)
	for _, filter := range filters {
		key, value, ok := strings.Cut(filter, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			return ecerrors.Client("InvalidFilter", "--filter must be key=value", ecerrors.WithField("filter"))
		}
		filterSpec, dynamicKey, ok := lookupActionFilter(actionFilters, key)
		if !ok {
			return ecerrors.Client("InvalidFilter", "unsupported filter "+key,
				ecerrors.WithField("filter"),
				ecerrors.WithAcceptedValues(supportedFilterFields(actionFilters)...),
				ecerrors.WithSuggestedAction(fmt.Sprintf("Run `ecctl schema %s.%s.%s` to list supported filters.", resource.Product, resource.Resource, actionName)),
			)
		}
		if err := applyFilterValue(resource, input, filterSpec, dynamicKey, key, value); err != nil {
			return err
		}
	}
	delete(input, "filter")
	return nil
}

func resourceActionFilters(resource spec.ResourceSpec, actionName string) map[string]spec.Filter {
	if operation, ok := resource.Operations[actionName]; ok && len(operation.Filters) > 0 {
		return operation.Filters
	}
	return nil
}

// cmdActionFilters returns the filter map for the spec operation that backs the
// given cobra command. It uses the ecctl.action annotation written by
// newResourceActionCommand to locate the operation in the catalog.
func cmdActionFilters(cmd *cobra.Command) map[string]spec.Filter {
	if cmd == nil || cmd.Annotations == nil {
		return nil
	}
	actionKey := cmd.Annotations[actionKeyAnnotation]
	if actionKey == "" {
		return nil
	}
	parts := strings.Split(actionKey, ".")
	if len(parts) != 3 {
		return nil
	}
	product, resourceName, actionName := parts[0], parts[1], parts[2]
	specDir := os.Getenv("ECCTL_SPEC_DIR")
	resource, err := spec.LoadResourceWithParent(specDir, product, resourceName, "")
	if err != nil {
		return nil
	}
	return resourceActionFilters(resource, actionName)
}

// hasFilterableFields reports whether the command exposes filter fields that
// the independent help section should render.
func hasFilterableFields(cmd *cobra.Command) bool {
	return len(cmdActionFilters(cmd)) > 0
}

// helpFilterableFieldsLabel returns the localized label for the filterable
// fields help section (e.g. "Filterable Fields" / "可过滤字段").
func helpFilterableFieldsLabel(cmd *cobra.Command) string {
	return localizerForCommand(cmd).Message("HelpFilterableFields")
}

// helpFilterableFieldsList renders the filter fields as an indented two-column
// block "  <field>    <description>" sorted alphabetically by field name.
func helpFilterableFieldsList(cmd *cobra.Command) string {
	filters := cmdActionFilters(cmd)
	if len(filters) == 0 {
		return ""
	}
	displayLang := ""
	for current := cmd; current != nil; current = current.Parent() {
		if current.Annotations == nil {
			continue
		}
		if value := current.Annotations[helpLangAnnotation]; value != "" {
			displayLang = value
			break
		}
	}

	type fieldEntry struct {
		name        string
		description string
	}
	entries := make([]fieldEntry, 0, len(filters))
	maxName := 0
	for _, name := range supportedFilterFields(filters) {
		entry := fieldEntry{name: name}
		// description comes from the canonical filter definition; for dynamic
		// "<prefix>.<key>" entries we use the prefix's filter spec.
		base := name
		if dot := strings.Index(name, "."); dot >= 0 {
			base = name[:dot+1]
		}
		if filter, ok := filters[base]; ok {
			entry.description = filter.Description.Text(displayLang)
		} else if filter, ok := filters[name]; ok {
			entry.description = filter.Description.Text(displayLang)
		}
		entries = append(entries, entry)
		if len(name) > maxName {
			maxName = len(name)
		}
	}

	var buf strings.Builder
	for _, entry := range entries {
		pad := strings.Repeat(" ", maxName-len(entry.name))
		if entry.description == "" {
			buf.WriteString("  ")
			buf.WriteString(entry.name)
			buf.WriteString("\n")
			continue
		}
		buf.WriteString("  ")
		buf.WriteString(entry.name)
		buf.WriteString(pad)
		buf.WriteString("  ")
		buf.WriteString(entry.description)
		buf.WriteString("\n")
	}
	return strings.TrimRight(buf.String(), "\n")
}

func lookupActionFilter(filters map[string]spec.Filter, key string) (spec.Filter, string, bool) {
	if filter, ok := filters[key]; ok {
		return filter, "", true
	}
	for name, filter := range filters {
		prefix := firstNonEmpty(filter.KeyPrefix, name)
		if prefix == "" || !strings.HasSuffix(prefix, ".") {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			dynamicKey := strings.TrimPrefix(key, prefix)
			if dynamicKey == "" {
				return spec.Filter{}, "", false
			}
			return filter, dynamicKey, true
		}
	}
	return spec.Filter{}, "", false
}

func applyFilterValue(resource spec.ResourceSpec, input map[string]any, filter spec.Filter, dynamicKey, originalKey, value string) error {
	target := filter.Target
	if target == "" {
		target = originalKey
	}
	if dynamicKey == "" {
		if existing, ok := input[target]; ok && !isInputValueEmpty(existing) {
			return ecerrors.Client("ConflictingParameters", "conflicting parameters: "+filterTargetDisplayName(resource, target)+", --filter")
		}
	}
	paramType := filter.Type
	if paramType == "" {
		paramType = resourceInputType(resource, target)
	}
	switch paramType {
	case "boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return ecerrors.Client("InvalidFilter", "--filter "+originalKey+" must be a boolean", ecerrors.WithField("filter."+originalKey))
		}
		input[target] = parsed
	case "integer":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return ecerrors.Client("InvalidFilter", "--filter "+originalKey+" must be an integer", ecerrors.WithField("filter."+originalKey))
		}
		input[target] = parsed
	case "float", "number":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return ecerrors.Client("InvalidFilter", "--filter "+originalKey+" must be a number", ecerrors.WithField("filter."+originalKey))
		}
		input[target] = parsed
	case "array", "string_array":
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			return ecerrors.Client("InvalidIDs", "--ids accepts comma-separated IDs, not JSON arrays")
		}
		input[target] = splitIDs(nil, value)
	case "key=value", "key_value":
		if dynamicKey == "" {
			return ecerrors.Client("InvalidFilter", "--filter "+originalKey+" must include a key", ecerrors.WithField("filter."+originalKey))
		}
		values, _ := input[target].([]string)
		input[target] = append(values, dynamicKey+"="+value)
	default:
		input[target] = value
	}
	return nil
}

func filterTargetDisplayName(resource spec.ResourceSpec, target string) string {
	if field, ok := resource.Schema.Fields[target]; ok {
		return "--" + resourceCLIFlagName(resource, target, paramFromDefinition(field))
	}
	if control, ok := resource.Controls[target]; ok {
		return "--" + resourceCLIFlagName(resource, target, paramFromDefinition(control))
	}
	return "--" + flagName(target)
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

func validateResourcePagination(input map[string]any) error {
	limit, hasLimit := input["limit"].(int)
	page, hasPage := input["page"].(int)
	if hasLimit || hasPage {
		if !hasLimit {
			limit = defaultListLimit
		}
		if !hasPage {
			page = defaultListPage
		}
		return validatePagination(limit, page)
	}
	return nil
}

func validateResourceActionInput(resource spec.ResourceSpec, actionName string, operation spec.Operation, input map[string]any) error {
	if err := validateOperationRequireAny(operation, input); err != nil {
		if fields := operationRequireAnyFlags(resource, operation); len(fields) > 0 {
			return ecerrors.Client("MissingParameter", "missing required parameters: "+strings.Join(fields, ", "))
		}
		return err
	}
	if err := validateOperationConflicts(resource, operation, input); err != nil {
		return err
	}
	if err := validateOperationRequireWhen(resource, operation, input); err != nil {
		return err
	}
	if actionName != "update" {
		return nil
	}
	for _, ref := range operation.Input.Fields {
		if ref.Name == "id" {
			continue
		}
		param, ok := operationInputParam(resource, ref)
		if !ok {
			continue
		}
		if param.Positional {
			continue
		}
		if _, ok := input[ref.Name]; ok && (param.AllowEmpty || !isInputValueEmpty(input[ref.Name])) {
			return nil
		}
	}
	if updateAPIParamRunsBinding(resource, operation, input) {
		return nil
	}
	fields := mutableActionFlags(resource, actionName, operation)
	return ecerrors.Client("MissingParameter", "missing required parameters: "+strings.Join(fields, ", "))
}

func updateAPIParamRunsBinding(resource spec.ResourceSpec, operation spec.Operation, input map[string]any) bool {
	if isInputValueEmpty(input["api_param"]) {
		return false
	}
	ctx := engine.ExecutionContext{Input: input}
	for _, step := range operation.Workflow {
		if step.Binding == "" {
			continue
		}
		binding, ok := resource.Bindings[step.Binding]
		if !ok || !bindingUsesInput(binding.Request, "api_param") {
			continue
		}
		run, err := engine.ShouldRun(step.When, step.WhenAny, ctx)
		if err != nil || !run {
			continue
		}
		skip, err := engine.ShouldSkip(step.Unless, ctx)
		if err != nil || skip {
			continue
		}
		return true
	}
	return false
}

func bindingUsesInput(value any, name string) bool {
	switch typed := value.(type) {
	case string:
		return typed == "$."+name || typed == "$input."+name || strings.Contains(typed, "$."+name+")") || strings.Contains(typed, "$input."+name+")")
	case map[string]any:
		for _, item := range typed {
			if bindingUsesInput(item, name) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if bindingUsesInput(item, name) {
				return true
			}
		}
	}
	return false
}

func validateOperationConflicts(resource spec.ResourceSpec, operation spec.Operation, input map[string]any) error {
	for _, conflict := range operation.Conflicts {
		left := presentConflictFields(resource, operation, conflict.Any, input)
		if len(left) == 0 {
			continue
		}
		right := presentConflictFields(resource, operation, conflict.WithAny, input)
		if len(right) == 0 {
			continue
		}
		return ecerrors.Client("ConflictingParameters", "conflicting parameters: "+strings.Join(append(left, right...), ", "))
	}
	return nil
}

func validateOperationRequireWhen(resource spec.ResourceSpec, operation spec.Operation, input map[string]any) error {
	missing := []string{}
	seen := map[string]bool{}
	for _, requirement := range operation.RequireWhen {
		if !anyOperationFieldPresent(requirement.WhenAny, input) || anyOperationFieldPresent(requirement.RequireAny, input) {
			continue
		}
		for _, field := range operationFieldDisplayNames(resource, operation, requirement.RequireAny) {
			if seen[field] {
				continue
			}
			seen[field] = true
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return ecerrors.Client("MissingParameter", "missing required parameters: "+strings.Join(missing, ", "))
	}
	return nil
}

func anyOperationFieldPresent(names []string, input map[string]any) bool {
	for _, name := range names {
		if value, ok := input[name]; ok && !isInputValueEmpty(value) {
			return true
		}
	}
	return false
}

func operationFieldDisplayNames(resource spec.ResourceSpec, operation spec.Operation, names []string) []string {
	result := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		display := operationFieldDisplayName(resource, operation, name)
		if seen[display] {
			continue
		}
		seen[display] = true
		result = append(result, display)
	}
	return result
}

func presentConflictFields(resource spec.ResourceSpec, operation spec.Operation, names []string, input map[string]any) []string {
	present := make([]string, 0, len(names))
	for _, name := range names {
		value, ok := input[name]
		if !ok || isInputValueEmpty(value) {
			continue
		}
		present = append(present, operationFieldDisplayName(resource, operation, name))
	}
	return present
}

func operationFieldDisplayName(resource spec.ResourceSpec, operation spec.Operation, name string) string {
	for _, ref := range append(append(spec.OperationFields{}, operation.Input.Fields...), operation.Input.Controls...) {
		if ref.Name != name {
			continue
		}
		param, ok := operationInputParam(resource, ref)
		if !ok {
			return "--" + flagName(name)
		}
		if param.Positional && !param.PositionalMany {
			return "<" + flagName(name) + ">"
		}
		return "--" + resourceCLIFlagName(resource, name, param)
	}
	for _, filter := range operation.Filters {
		if filter.Target == name {
			return "--filter"
		}
	}
	return "--" + flagName(name)
}

func updateInputParam(resource spec.ResourceSpec, operation spec.Operation, name string) (spec.Param, bool) {
	for _, ref := range operation.Input.Fields {
		if ref.Name != name {
			continue
		}
		return operationInputParam(resource, ref)
	}
	return spec.Param{}, false
}

func validateOperationRequireAny(operation spec.Operation, input map[string]any) error {
	if len(operation.RequireAny) == 0 {
		return nil
	}
	ctx := engine.ExecutionContext{Input: input}
	for _, requirement := range operation.RequireAny {
		if requirement.Raw != "" && operationExpressionHasValue(requirement.Raw, ctx) {
			return nil
		}
		if requirement.Each != "" && operationExpressionHasValue(requirement.Each, ctx) {
			return nil
		}
	}
	return ecerrors.Client("MissingParameter", "missing required parameters")
}

func operationRequireAnyFlags(resource spec.ResourceSpec, operation spec.Operation) []string {
	names := make([]string, 0, len(operation.RequireAny))
	seen := map[string]bool{}
	for _, requirement := range operation.RequireAny {
		name := requirementInputName(requirement)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, operationFieldDisplayName(resource, operation, name))
	}
	return names
}

func requirementInputName(requirement spec.Requirement) string {
	for _, expr := range []string{requirement.Raw, requirement.Each} {
		expr = strings.TrimSpace(expr)
		switch {
		case strings.HasPrefix(expr, "$."):
			return strings.TrimPrefix(expr, "$.")
		case strings.HasPrefix(expr, "input."):
			return strings.TrimPrefix(expr, "input.")
		}
	}
	return ""
}

func operationExpressionHasValue(expr string, ctx engine.ExecutionContext) bool {
	value, ok, err := engine.ResolveExpression(expr, ctx)
	return err == nil && ok && !isInputValueEmpty(value)
}

func mutableActionFlags(resource spec.ResourceSpec, actionName string, operation spec.Operation) []string {
	names := make([]string, 0)
	for _, ref := range operation.Input.Fields {
		name := ref.Name
		switch name {
		case "id":
			continue
		}
		param, ok := operationInputParam(resource, ref)
		if !ok || param.Positional {
			continue
		}
		names = append(names, "--"+resourceCLIFlagName(resource, name, param))
	}
	return names
}

func withCapabilities(payload map[string]any, result engine.Result) map[string]any {
	if len(result.Capabilities) > 0 {
		payload["ecctl_capabilities_used"] = result.Capabilities
	}
	return payload
}

func resourceActionPayload(resource spec.ResourceSpec, actionName string, operation spec.Operation, input map[string]any, region string, result engine.Result) map[string]any {
	oneRoot := firstNonEmpty(resource.Identity.OutputRoot.One, resource.Resource)
	manyRoot := firstNonEmpty(resource.Identity.OutputRoot.Many, resource.Resource+"s")
	if len(operation.Output.Fields) > 0 || len(operation.Output.Select) > 0 {
		return outputResourceActionPayload(operation, input, region, result)
	}
	switch actionName {
	case "list":
		payload := map[string]any{
			"pagination": operationPaginationPayload(operation, input, result),
			manyRoot:     outputItems(result.Items),
		}
		if result.HasTotal {
			payload["total"] = result.Total
		}
		return withResultExtra(payload, result)
	case "delete":
		if result.DryRun || boolInput(input, "dry_run") {
			requestedCount := dryRunRequestedCount(input)
			return withResultExtra(map[string]any{
				"actions":         result.Actions,
				"available_count": requestedCount,
				"dry_run":         "passed",
				"requested_count": requestedCount,
			}, result)
		}
		item := result.Item
		if item == nil {
			item = map[string]any{"id": firstNonEmpty(result.ID, stringInput(input, "id"))}
		}
		return withResultExtra(withCapabilities(map[string]any{
			"actions": result.Actions,
			"deleted": result.Deleted,
			oneRoot:   item,
		}, result), result)
	case "create":
		if result.DryRun || boolInput(input, "dry_run") {
			requestedCount := dryRunRequestedCount(input)
			return withResultExtra(map[string]any{
				"actions":         result.Actions,
				"available_count": requestedCount,
				"dry_run":         "passed",
				"requested_count": requestedCount,
			}, result)
		}
		return withResultExtra(withCapabilities(map[string]any{
			"actions": result.Actions,
			oneRoot:   firstNonNilMap(result.Item, fallbackItem(resource, input, region, result)),
		}, result), result)
	case "update":
		if result.DryRun || boolInput(input, "dry_run") {
			requestedCount := dryRunRequestedCount(input)
			return withResultExtra(map[string]any{
				"actions":         result.Actions,
				"available_count": requestedCount,
				"dry_run":         "passed",
				"requested_count": requestedCount,
			}, result)
		}
		return withResultExtra(withCapabilities(map[string]any{
			"actions": result.Actions,
			oneRoot:   firstNonNilMap(result.Item, fallbackItem(resource, input, region, result)),
		}, result), result)
	case "get":
		return withResultExtra(map[string]any{oneRoot: result.Item}, result)
	default:
		if len(result.Actions) > 0 {
			payload := map[string]any{"actions": result.Actions}
			if result.Item != nil {
				payload[oneRoot] = result.Item
				return withResultExtra(withCapabilities(payload, result), result)
			}
			if len(result.Items) > 0 {
				payload[manyRoot] = result.Items
				if result.Total > 0 {
					payload["total"] = result.Total
				}
			}
			return withResultExtra(withCapabilities(payload, result), result)
		}
		if result.Item != nil {
			return withResultExtra(map[string]any{oneRoot: result.Item}, result)
		}
		return withResultExtra(map[string]any{manyRoot: outputItems(result.Items), "total": result.Total}, result)
	}
}

func outputItems(items []map[string]any) []map[string]any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}

func operationPaginationPayload(operation spec.Operation, input map[string]any, result engine.Result) map[string]any {
	limit := intInput(input, "limit", defaultListLimit)
	if operationUsesTokenPagination(operation) {
		return tokenPaginationPayload(limit, len(result.Items), result.NextToken)
	}
	page := intInput(input, "page", defaultListPage)
	return paginationPayload(page, limit, len(result.Items), result.Total, result.NextToken)
}

func operationUsesTokenPagination(operation spec.Operation) bool {
	return operationInputHasName(operation.Input.Controls, "next_token") && !operationInputHasName(operation.Input.Controls, "page")
}

func operationInputHasName(fields spec.OperationFields, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func tokenPaginationPayload(limit, returned int, nextToken string) map[string]any {
	payload := map[string]any{
		"has_more": nextToken != "",
		"limit":    limit,
		"returned": returned,
	}
	if nextToken != "" {
		payload["next_token"] = nextToken
	}
	return payload
}

func withResultExtra(payload map[string]any, result engine.Result) map[string]any {
	for key, value := range result.Extra {
		if _, exists := payload[key]; exists || isInputValueEmpty(value) {
			continue
		}
		payload[key] = value
	}
	return payload
}

func dryRunRequestedCount(input map[string]any) int {
	if amount := intInput(input, "amount", 0); amount > 0 {
		return amount
	}
	if ids, ok := input["ids"].([]string); ok && len(ids) > 0 {
		return len(ids)
	}
	if ids, ok := input["ids"].([]any); ok && len(ids) > 0 {
		return len(ids)
	}
	return 1
}

type actionOutputContext struct {
	operation spec.Operation
	input     map[string]any
	region    string
	result    engine.Result
}

func outputResourceActionPayload(operation spec.Operation, input map[string]any, region string, result engine.Result) map[string]any {
	output := operation.Output
	ctx := actionOutputContext{operation: operation, input: input, region: region, result: result}
	payload := map[string]any{}
	for key, value := range output.Fields {
		resolved, ok := outputValue(value, ctx)
		if ok && !isInputValueEmpty(resolved) {
			payload[key] = resolved
		}
	}
	for _, selectSpec := range output.Select {
		key, value, ok := outputSelectValue(selectSpec, ctx)
		if !ok {
			continue
		}
		if _, exists := payload[key]; exists {
			continue
		}
		payload[key] = value
	}
	return withCapabilities(payload, result)
}

func outputValue(value any, ctx actionOutputContext) (any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "$") {
			return outputExpression(typed, ctx)
		}
		return typed, true
	case map[string]any:
		if rawValue, ok := typed["value"]; ok {
			if !outputConditionAllows(typed, ctx) {
				return nil, false
			}
			return outputValue(rawValue, ctx)
		}
		resolved := map[string]any{}
		for key, value := range typed {
			item, ok := outputValue(value, ctx)
			if ok && !isInputValueEmpty(item) {
				resolved[key] = item
			}
		}
		return resolved, true
	case []any:
		resolved := make([]any, 0, len(typed))
		for _, value := range typed {
			item, ok := outputValue(value, ctx)
			if ok {
				resolved = append(resolved, item)
			}
		}
		return resolved, true
	default:
		return typed, true
	}
}

func outputExpression(expr string, ctx actionOutputContext) (any, bool) {
	switch {
	case expr == "$result.actions":
		return ctx.result.Actions, true
	case expr == "$result.items":
		return outputItems(ctx.result.Items), true
	case expr == "$result.item":
		return ctx.result.Item, true
	case expr == "$result.total":
		return ctx.result.Total, true
	case expr == "$result.pagination":
		return operationPaginationPayload(ctx.operation, ctx.input, ctx.result), true
	case expr == "$result.returned":
		return len(ctx.result.Items), true
	case expr == "$result.next_token":
		return ctx.result.NextToken, true
	case expr == "$result.has_more":
		limit := intInput(ctx.input, "limit", defaultListLimit)
		page := intInput(ctx.input, "page", defaultListPage)
		return ctx.result.NextToken != "" || page*limit < ctx.result.Total, true
	case strings.HasPrefix(expr, "$result.extra."):
		if ctx.result.Extra == nil {
			return nil, false
		}
		value, ok := ctx.result.Extra[strings.TrimPrefix(expr, "$result.extra.")]
		return value, ok
	case expr == "$result.request_id":
		return ctx.result.RequestID, true
	case expr == "$result.id":
		return ctx.result.ID, true
	case expr == "$result.deleted":
		return ctx.result.Deleted, true
	case expr == "$result.dry_run":
		return ctx.result.DryRun, true
	case expr == "$context.region":
		return ctx.region, true
	case strings.HasPrefix(expr, "$input."):
		value, ok := ctx.input[strings.TrimPrefix(expr, "$input.")]
		return value, ok
	case strings.HasPrefix(expr, "$result.item."):
		return outputPath(ctx.result.Item, strings.TrimPrefix(expr, "$result.item."))
	case strings.HasPrefix(expr, "$captures."):
		return outputCaptureExpression(strings.TrimPrefix(expr, "$captures."), ctx.result)
	default:
		return nil, false
	}
}

func outputConditionAllows(value map[string]any, ctx actionOutputContext) bool {
	if when, ok := value["when"].(string); ok && !outputConditionMatches(when, ctx) {
		return false
	}
	if unless, ok := value["unless"].(string); ok && outputConditionMatches(unless, ctx) {
		return false
	}
	return true
}

func outputConditionMatches(expr string, ctx actionOutputContext) bool {
	resolved, ok := outputValue(expr, ctx)
	if !ok {
		return false
	}
	if value, ok := resolved.(bool); ok {
		return value
	}
	return !isInputValueEmpty(resolved)
}

func outputCaptureExpression(path string, result engine.Result) (any, bool) {
	name, field, ok := strings.Cut(path, ".")
	if !ok || name == "" || field == "" {
		return nil, false
	}
	capture, ok := result.Captures[name]
	if !ok {
		return nil, false
	}
	switch field {
	case "items":
		return capture.Items, true
	case "request":
		return capture.Request, true
	default:
		return nil, false
	}
}

func outputPath(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, false
		}
		item, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = item[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func outputSelectValue(selectSpec spec.OutputSelect, ctx actionOutputContext) (string, any, bool) {
	rawSource, ok := outputExpression(selectSpec.From, ctx)
	if !ok {
		return "", nil, false
	}
	source := outputMapItems(rawSource)
	selected := source
	if selectSpec.Match != "" {
		rawMatches, ok := outputExpression(selectSpec.Match, ctx)
		if !ok {
			return "", nil, false
		}
		matches := outputMapItems(rawMatches)
		if len(matches) == 0 {
			return "", nil, false
		}
		selected = selectOutputMatches(source, matches, selectSpec)
	}
	if len(selected) == 0 {
		return "", nil, false
	}
	if selectSpec.First && len(selected) > 1 {
		selected = selected[:1]
	}
	items := cleanOutputItems(selected, selectSpec.Fields)
	if len(items) == 0 {
		return "", nil, false
	}
	key := outputSelectKey(selectSpec, len(items))
	if key == "" {
		return "", nil, false
	}
	if key == selectSpec.SingleKey {
		return key, items[0], true
	}
	return key, items, true
}

func selectOutputMatches(source []map[string]any, matches []map[string]any, selectSpec spec.OutputSelect) []map[string]any {
	selected := make([]map[string]any, 0, len(matches))
	used := map[int]bool{}
	for _, want := range matches {
		var matched map[string]any
		for i, candidate := range source {
			if used[i] || !outputItemMatches(candidate, want, selectSpec.By) {
				continue
			}
			matched = candidate
			used[i] = true
			break
		}
		if matched == nil && (selectSpec.FallbackToMatch || selectSpec.UseMatchWhenMissing) {
			matched = want
		}
		if matched != nil {
			selected = append(selected, matched)
		}
	}
	return selected
}

func outputItemMatches(candidate map[string]any, want map[string]any, fields []string) bool {
	for _, field := range fields {
		wantValue, ok := want[field]
		if !ok || isInputValueEmpty(wantValue) {
			continue
		}
		if !strings.EqualFold(fmt.Sprint(candidate[field]), fmt.Sprint(wantValue)) {
			return false
		}
	}
	return true
}

func cleanOutputItems(items []map[string]any, fields []string) []map[string]any {
	cleaned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cleanedItem := map[string]any{}
		if len(fields) == 0 {
			for key, value := range item {
				if !isInputValueEmpty(value) {
					cleanedItem[key] = value
				}
			}
		} else {
			for _, field := range fields {
				if value, ok := item[field]; ok && !isInputValueEmpty(value) {
					cleanedItem[field] = value
				}
			}
		}
		if len(cleanedItem) > 0 {
			cleaned = append(cleaned, cleanedItem)
		}
	}
	return cleaned
}

func outputSelectKey(selectSpec spec.OutputSelect, count int) string {
	if selectSpec.First || count == 1 {
		if selectSpec.SingleKey != "" {
			return selectSpec.SingleKey
		}
	}
	if selectSpec.ManyKey != "" {
		return selectSpec.ManyKey
	}
	return selectSpec.SingleKey
}

func outputMapItems(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(map[string]any)
			if ok {
				items = append(items, item)
			}
		}
		return items
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func fallbackItem(resource spec.ResourceSpec, input map[string]any, region string, result engine.Result) map[string]any {
	item := map[string]any{}
	if id := firstNonEmpty(result.ID, stringInput(input, "id")); id != "" {
		item["id"] = id
	}
	for _, name := range []string{"name", "cidr", "description"} {
		if value := stringInput(input, name); value != "" {
			item[name] = value
		}
	}
	if resource.Kind == "regional" && region != "" {
		item["region"] = region
	}
	return item
}

func firstNonNilMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return map[string]any{}
}

func resourceCLIFlagName(resource spec.ResourceSpec, name string, param spec.Param) string {
	if param.FlagName != "" {
		return flagName(param.FlagName)
	}
	if isObjectArrayInput(resource, name, param) {
		return flagName(singularInputName(name))
	}
	return flagName(name)
}

func flagName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
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

func isObjectArrayInput(resource spec.ResourceSpec, name string, param spec.Param) bool {
	if param.Type != "array" {
		return false
	}
	field := schemaFieldForInput(resource, name)
	return field.Items != nil && field.Items.Type == "object"
}

func isObjectInput(resource spec.ResourceSpec, name string, param spec.Param) bool {
	return param.Type == "object"
}

func schemaFieldForInput(resource spec.ResourceSpec, name string) spec.SchemaField {
	if field, ok := resource.Schema.Fields[name]; ok {
		return field
	}
	if control, ok := resource.Controls[name]; ok {
		return control
	}
	return spec.SchemaField{}
}

func structuredInputShape(field spec.SchemaField, item bool, lang string) string {
	if item && field.Items != nil {
		field = *field.Items
	}
	localizer := i18n.NewLocalizer(lang)
	if isShallowObjectField(field) {
		return localizer.MessageOrDefault("InputStyleInlineObject", "inline key=value, JSON object, or @file")
	}
	return localizer.MessageOrDefault("InputStyleObject", "JSON object or @file")
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

func isResourceActionFlag(name string) bool {
	switch name {
	case "fields", "filter", "ids", "idempotency_key", "limit", "next_token", "page", "timeout", "no_wait":
		return false
	default:
		return true
	}
}

func paramUsage(resource spec.ResourceSpec, name string, actionParam spec.Param, lang string) string {
	if description := actionParam.Description.Text(lang); description != "" {
		return description
	}
	if name == fieldSelectorInputName {
		return i18n.NewLocalizer(lang).Message("FlagUsage.ResourceFields")
	}
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
	return i18n.NewLocalizer(lang).MessageData("FlagUsage.OpenAPIParameter", map[string]any{"Name": flagName(name)})
}

func validateParamEnum(flag string, param spec.Param, value any) error {
	if len(param.Enum) == 0 || isInputValueEmpty(value) {
		return nil
	}
	got, ok := value.(string)
	if !ok {
		return nil
	}
	for _, allowed := range param.Enum {
		if got == allowed {
			return nil
		}
	}
	return ecerrors.Client("InvalidParameter", fmt.Sprintf("--%s must be one of %s", flag, strings.Join(param.Enum, ", ")))
}

func validateParamMax(flag string, param spec.Param, value any) error {
	if param.Max == nil || isInputValueEmpty(value) {
		return nil
	}
	var over bool
	switch typed := value.(type) {
	case int:
		over = typed > *param.Max
	case float64:
		over = typed > float64(*param.Max)
	default:
		return nil
	}
	if !over {
		return nil
	}
	message := fmt.Sprintf("--%s must be less than or equal to %d", flag, *param.Max)
	if flag == "limit" {
		return ecerrors.Client("InvalidLimit", message, ecerrors.WithField(flag))
	}
	return ecerrors.Client("InvalidParameter", message, ecerrors.WithField(flag))
}

func validateParamInputStyle(flag string, param spec.Param, value any) error {
	if param.Use != "+value|-value" || isInputValueEmpty(value) {
		return nil
	}
	for _, item := range inputStringValues(value) {
		item = strings.TrimSpace(item)
		if len(item) < 2 || (item[0] != '+' && item[0] != '-') {
			return ecerrors.Client("InvalidParameter", fmt.Sprintf("--%s must use +value or -value", flag))
		}
	}
	return nil
}

func inputStringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				values = append(values, str)
			}
		}
		return values
	default:
		return nil
	}
}

func isInputValueEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func boolDefault(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func intDefault(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(typed)
		return n
	default:
		return 0
	}
}

func floatDefault(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		n, _ := strconv.ParseFloat(typed, 64)
		return n
	default:
		return 0
	}
}

func durationDefault(value any) time.Duration {
	switch typed := value.(type) {
	case time.Duration:
		return typed
	case string:
		duration, _ := time.ParseDuration(typed)
		return duration
	default:
		return 0
	}
}

func stringDefault(value any) string {
	typed, _ := value.(string)
	return typed
}

func intInput(input map[string]any, key string, fallback int) int {
	value, ok := input[key].(int)
	if !ok || value == 0 {
		return fallback
	}
	return value
}

func stringInput(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func boolInput(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}
