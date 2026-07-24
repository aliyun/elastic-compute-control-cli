package cli

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/output"
	"github.com/aliyun/elastic-compute-control-cli/pkg/schema"
	"github.com/aliyun/elastic-compute-control-cli/pkg/updater"
)

// RegionVerifier is the surface exposed by aliyun.RegionVerifier and any test
// double swapped in via SetRegionVerifierFactoryForTest.
type RegionVerifier interface {
	Verify(region, serviceCode string) error
}

// RegionVerifierFactory builds a RegionVerifier for the active profile. The
// returned error indicates that verification cannot be performed (e.g. missing
// credentials) — the CLI translates that into a soft skip rather than a hard
// failure.
type RegionVerifierFactory func(profileName, configPath string, getenv func(string) string) (RegionVerifier, error)

var newRegionVerifier RegionVerifierFactory = func(profileName, configPath string, getenv func(string) string) (RegionVerifier, error) {
	return aliyun.NewRegionVerifier(profileName, configPath, getenv)
}

// SetRegionVerifierFactoryForTest swaps the default region verifier with a
// fake. Tests should call the returned restore func via t.Cleanup.
func SetRegionVerifierFactoryForTest(factory RegionVerifierFactory) func() {
	old := newRegionVerifier
	newRegionVerifier = factory
	return func() {
		newRegionVerifier = old
	}
}

type globalOptions struct {
	region        string
	profile       string
	lang          string
	output        string
	json          bool
	agentEnvelope bool
	noColor       bool
	forceJSON     bool
	command       string
	fullSurface   bool
}

const (
	defaultListLimit           = 100
	defaultListPage            = 1
	agentEnvelopeSchemaVersion = "ecctl.agent.v1"

	rootCommandGroupCloudProducts = "cloud-products"
	rootCommandGroupTools         = "tools"

	flagGroupAnnotation    = "ecctl.flag.group"
	flagRequiredAnnotation = "ecctl.flag.required"
	flagBriefAnnotation    = "ecctl.flag.brief"
	helpLangAnnotation     = "ecctl.help.lang"
	helpNoColorAnnotation  = "ecctl.help.no_color"
	flagGroupResource      = "resource"
	displayModeEnv         = "ECCTL_DISPLAY_MODE"
	displayModeAI          = "AI"
	displayModeHuman       = "Human"
	displayModeAuto        = "auto"
)

var (
	version         = "dev"
	commit          = ""
	date            = ""
	checkUpdate     = updater.Check
	installUpdate   = updater.Update
	autoCheckUpdate = updater.AutoCheck
)

type buildInfoReader func() (*debug.BuildInfo, bool)

func displayVersion() string {
	return formatVersion(version, commit, date, debug.ReadBuildInfo)
}

func formatVersion(injectedVersion string, injectedCommit string, injectedDate string, readBuildInfo buildInfoReader) string {
	display := injectedVersion
	if display == "" || display == "dev" {
		if readBuildInfo != nil {
			if info, ok := readBuildInfo(); ok && info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
				display = info.Main.Version
			}
		}
	}
	if display == "" {
		display = "dev"
	}

	metadata := make([]string, 0, 2)
	if injectedCommit != "" {
		metadata = append(metadata, "commit "+injectedCommit)
	}
	if injectedDate != "" {
		metadata = append(metadata, "built "+injectedDate)
	}
	if len(metadata) == 0 {
		return display
	}
	return display + " (" + strings.Join(metadata, ", ") + ")"
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	args = normalizeAPICallParameterFlags(args)
	args = normalizeHelpTopicArgs(args)
	options := newGlobalOptions(args, os.Getenv)
	options.fullSurface = fullCommandSurfaceFromContext(ctx)
	if !options.fullSurface && os.Getenv("ECCTL_SPEC_DIR") == "" && !publicCLICommandAllowed(args) {
		return writeRunError(stdout, options, ecerrors.Client("UnknownCommand", "command is not supported"))
	}
	if mode, ok := requestedOutputMode(args); !helpRequested(args) && ok && mode != "" && !output.IsSupportedMode(mode) {
		return writeRunError(stdout, options, unsupportedOutputModeError(fmt.Sprintf("output mode %q is not supported", mode)))
	}
	if !helpRequested(args) && explicitEmptyRegion(args) {
		return writeRunError(stdout, options, ecerrors.Client("MissingRegion", "region is required"))
	}
	if !helpRequested(args) && callCommandRequested(args) {
		options.output = output.ModeJSON
		options.forceJSON = true
	}
	maybeCheckForUpdate(ctx, args, stderr, options)
	root := newRootCommand(options, stdout, args)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(io.Discard)
	root.SilenceErrors = true
	root.SilenceUsage = true
	if helpRequested(args) {
		allowHelpWithUnknownFlags(root)
	}

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var appErr *ecerrors.AppError
	if stderrors.As(err, &appErr) {
		return writeRunError(stdout, options, appErr)
	}
	return writeRunError(stdout, options, cobraErrorToAppErrorForLanguage(err, options.lang, args))
}

func cobraErrorToAppError(err error, args ...[]string) *ecerrors.AppError {
	return cobraErrorToAppErrorForLanguage(err, "", args...)
}

func cobraErrorToAppErrorForLanguage(err error, lang string, args ...[]string) *ecerrors.AppError {
	if err == nil {
		return ecerrors.Client("InternalError", "internal error")
	}
	message := err.Error()
	if field, ok := unknownFlagField(message); ok {
		localizer := i18n.NewLocalizer(lang)
		suggestion := localizer.MessageData("SuggestionUnknownFlag", map[string]any{"Flag": field})
		return ecerrors.Client("UnknownCommand", message,
			ecerrors.WithField(field),
			ecerrors.WithSuggestion(suggestion),
		)
	}
	options := []ecerrors.Option{}
	if strings.Contains(message, "unknown command") {
		if len(args) > 0 {
			if suggestion := unknownCommandSuggestion(args[0], lang); suggestion != "" {
				options = append(options, ecerrors.WithSuggestion(suggestion))
			}
		}
		return ecerrors.Client("UnknownCommand", message, options...)
	}
	return ecerrors.Client("UnknownCommand", message)
}

func unknownCommandSuggestion(args []string, lang string) string {
	positionals := commandPositionals(args)
	if len(positionals) < 2 {
		return ""
	}
	product := positionals[0]
	if isBuiltinRootCommand(product) {
		return ""
	}
	surface, ok := schema.ProductList(product)
	if !ok {
		return ""
	}
	requested := positionals[1]
	action := ""
	if len(positionals) >= 3 {
		action = positionals[2]
	}
	var defaultResource *schema.ResourceSurface
	var requestedResource *schema.ResourceSurface
	for i := range surface.Resources {
		resource := &surface.Resources[i]
		if !publicCLIResource(product, resource.Name) {
			continue
		}
		if resource.Name == product {
			defaultResource = resource
		}
		if resource.Name == requested {
			requestedResource = resource
		}
	}
	localizer := i18n.NewLocalizer(lang)
	data := map[string]any{
		"Action":   action,
		"Product":  product,
		"Resource": requested,
	}
	if requestedResource == nil && defaultResource != nil && action != "" && containsString(defaultResource.Actions, action) {
		return localizer.MessageData("SuggestionUnknownCommandDefaultResource", data)
	}
	if requestedResource == nil {
		return localizer.MessageData("SuggestionUnknownCommandListResources", data)
	}
	if action != "" && !containsString(requestedResource.Actions, action) {
		data["Resource"] = requestedResource.Name
		return localizer.MessageData("SuggestionUnknownCommandListActions", data)
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func unknownFlagField(message string) (string, bool) {
	if field, ok := strings.CutPrefix(message, "unknown flag: "); ok {
		return strings.TrimSpace(field), true
	}
	if !strings.HasPrefix(message, "unknown shorthand flag: ") {
		return "", false
	}
	if _, field, ok := strings.Cut(message, " in "); ok {
		return strings.TrimSpace(field), true
	}
	return "", true
}

func unsupportedOutputModeError(message string) *ecerrors.AppError {
	return ecerrors.Client("UnsupportedOutputMode", message,
		ecerrors.WithField("output"),
		ecerrors.WithAcceptedValues(output.ModeJSON, output.ModeText),
	)
}

func newGlobalOptions(args []string, getenv func(string) string) *globalOptions {
	options := &globalOptions{
		lang:          requestedLanguage(args),
		output:        "json",
		json:          requestedBoolFlag(args, "json"),
		agentEnvelope: requestedBoolFlag(args, "agent-envelope"),
		noColor:       requestedBoolFlag(args, "no-color"),
	}
	requestedOutput, hasRequestedOutput := requestedOutputMode(args)
	if hasRequestedOutput {
		options.output = requestedOutput
	}
	if options.json {
		options.output = output.ModeJSON
	}
	profileName := config.ProfileName(requestedProfile(args), getenv)
	profile, ok, err := config.EffectiveProfile(profileName, config.EcctlConfigPath(getenv), config.AliyunConfigPath(getenv))
	if err != nil || !ok {
		return options
	}
	if options.lang == "" {
		options.lang = profile.Language
	}
	if !hasRequestedOutput && !options.json && profile.Output != "" {
		options.output = profile.Output
	}
	return options
}

func newRootCommand(options *globalOptions, stdout io.Writer, args []string) *cobra.Command {
	requestedLang := options.lang
	requestedOutput := options.output
	requestedNoColor := options.noColor
	defaultLocalizer := i18n.NewLocalizer("en")
	root := &cobra.Command{
		Use:     "ecctl",
		Short:   "Agent-first Elastic Computing Controller",
		Long:    defaultLocalizer.Message("RootLong"),
		Example: defaultLocalizer.Message("RootExample"),
		Version: displayVersion(),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			options.command = cmd.CommandPath()
			if options.json {
				options.output = output.ModeJSON
			}
			if options.agentEnvelope {
				options.output = output.ModeJSON
			}
			if options.output != "" && !output.IsSupportedMode(options.output) {
				return unsupportedOutputModeError(fmt.Sprintf("output mode %q is not supported", options.output))
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&options.region, "region", "", "Alibaba Cloud region")
	root.PersistentFlags().StringVar(&options.profile, "profile", "", "configuration profile")
	root.PersistentFlags().StringVar(&options.lang, "lang", "", "display language (supported: en, zh-CN)")
	root.PersistentFlags().StringVar(&options.output, "output", output.ModeJSON, "output mode (json, text)")
	root.PersistentFlags().BoolVar(&options.json, "json", false, "force JSON output")
	root.PersistentFlags().BoolVar(&options.agentEnvelope, "agent-envelope", options.agentEnvelope, "wrap JSON output in the Agent envelope")
	root.PersistentFlags().BoolVar(&options.noColor, "no-color", false, "disable color in human output")
	root.SetVersionTemplate("ecctl {{.Version}}\n")
	root.InitDefaultVersionFlag()
	if requestedLang != "" {
		options.lang = requestedLang
	}
	if requestedOutput != "" {
		options.output = requestedOutput
	}
	options.lang = i18n.ResolveLanguage(options.lang, os.Getenv)

	root.AddGroup(
		&cobra.Group{ID: rootCommandGroupCloudProducts, Title: "Cloud Product Commands:"},
		&cobra.Group{ID: rootCommandGroupTools, Title: "Auxiliary Commands:"},
	)
	root.SetHelpCommandGroupID(rootCommandGroupTools)
	root.SetCompletionCommandGroupID(rootCommandGroupTools)

	schemaCmd := newSchemaCommand(options, stdout)
	schemaCmd.GroupID = rootCommandGroupTools
	capabilitiesCmd := newCapabilitiesCommand(options, stdout)
	capabilitiesCmd.GroupID = rootCommandGroupTools
	configCmd := newConfigCommand(options, stdout)
	configCmd.GroupID = rootCommandGroupTools
	apiCmd := newAPICommand(options, stdout)
	apiCmd.GroupID = rootCommandGroupTools
	examplesCmd := newExamplesCommand(options, stdout)
	examplesCmd.GroupID = rootCommandGroupTools
	examplesCmd.Hidden = true
	updateCmd := newUpdateCommand(options, stdout)
	updateCmd.GroupID = rootCommandGroupTools
	internalUpdateCmd := &cobra.Command{
		Use:    "__update",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return updater.RunInternalUpdate(args)
		},
	}
	productCommands := newProductCommands(options, stdout, productCommandBuildTarget(args))
	for _, cmd := range productCommands {
		cmd.GroupID = rootCommandGroupCloudProducts
	}

	root.AddCommand(schemaCmd)
	root.AddCommand(capabilitiesCmd)
	root.AddCommand(configCmd)
	root.AddCommand(apiCmd)
	root.AddCommand(examplesCmd)
	root.AddCommand(updateCmd)
	root.AddCommand(internalUpdateCmd)
	root.AddCommand(productCommands...)
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	localizeHelp(root, options.lang, requestedNoColor)
	return root
}

func newUpdateCommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	var checkOnly bool
	var force bool
	cmd := &cobra.Command{
		Use:     "update [version]",
		Short:   "Check for and install ecctl updates",
		Example: "  ecctl update --check\n  ecctl update\n  ecctl update 0.2.0\n  ecctl update --force",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if checkOnly && force {
				return ecerrors.Client("ConflictingParameters", "--check cannot be combined with --force")
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			updateOptions := updater.Options{
				CurrentVersion: currentReleaseVersion(),
				TargetVersion:  target,
				Force:          force,
			}
			operationCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()
			var (
				result updater.Result
				err    error
			)
			if checkOnly {
				result, err = checkUpdate(operationCtx, updateOptions)
			} else {
				result, err = installUpdate(operationCtx, updateOptions)
			}
			if err != nil {
				return updateCommandError(options.lang, err)
			}
			return writeCommandOutput(options, stdout, result)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for an update without installing it")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall the target version or allow a downgrade")
	return cmd
}

func updateCommandError(lang string, err error) *ecerrors.AppError {
	kind := updater.ErrorKindOf(err)
	code := map[updater.ErrorKind]string{
		updater.ErrorUnavailable:   "UpdateUnavailable",
		updater.ErrorIntegrity:     "UpdateIntegrityFailed",
		updater.ErrorInvalidTarget: "UpdateInvalidTarget",
		updater.ErrorPermission:    "UpdatePermissionDenied",
		updater.ErrorBusy:          "UpdateBusy",
		updater.ErrorTimeout:       "UpdateTimeout",
		updater.ErrorCanceled:      "UpdateCanceled",
		updater.ErrorInstallation:  "UpdateInstallFailed",
	}[kind]
	if code == "" {
		code = "UpdateInstallFailed"
	}
	message := i18n.NewLocalizer(lang).Message(code)
	options := []ecerrors.Option{ecerrors.WithDetail(err.Error())}
	if kind == updater.ErrorTimeout {
		return ecerrors.Timeout(code, message, options...)
	}
	if updater.ErrorRetryable(kind) {
		return ecerrors.Service(code, message, true, options...)
	}
	return ecerrors.Client(code, message, options...)
}

func currentReleaseVersion() string {
	display := displayVersion()
	if fields := strings.Fields(display); len(fields) > 0 {
		return strings.TrimPrefix(fields[0], "v")
	}
	return display
}

func maybeCheckForUpdate(ctx context.Context, args []string, stderr io.Writer, options *globalOptions) {
	if os.Getenv("ECCTL_DISABLE_UPDATE_CHECK") == "1" || skipAutomaticUpdateCheck(args) {
		return
	}
	current := currentReleaseVersion()
	if _, err := updater.NormalizeVersion(current); err != nil {
		return
	}
	interactive := writerIsTerminal(stderr)
	checkCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	result, err := autoCheckUpdate(checkCtx, updater.AutoCheckOptions{
		CurrentVersion:   current,
		Client:           updater.NewClient(800 * time.Millisecond),
		MarkNotification: interactive,
	})
	if err != nil || !interactive || !result.Notify {
		return
	}
	localizer := i18n.NewLocalizer(options.lang)
	_, _ = fmt.Fprintln(stderr, localizer.MessageData("UpdateAvailableNotice", map[string]any{
		"CurrentVersion": current,
		"LatestVersion":  result.LatestVersion,
	}))
}

func skipAutomaticUpdateCheck(args []string) bool {
	positionals := commandPositionals(args)
	return len(positionals) > 0 && positionals[0] == "__update"
}

func newSchemaCommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	var list bool
	var brief bool
	var full bool
	var product string
	cmd := &cobra.Command{
		Use:   "schema [--list [product] | list [product] | product[.parent].resource[.action] ...] [--brief|--full]",
		Short: "Inspect command schemas",
		Long:  "Inspect compact JSON schemas for supported ecctl products, resources, and actions.\nMultiple schema names can be passed to retrieve them in a single call.",
		Example: `  ecctl schema --list
  ecctl schema --list <product>
  ecctl schema list <product>
  ecctl schema <product>.<resource>
  ecctl schema <product> <resource>
  ecctl schema <product>.<resource>.<action>
  ecctl schema <product> <resource> <action>
  ecctl schema <product>.<parent>.<resource>.<action>
  ecctl schema <product> <parent> <resource> <action>
  ecctl schema <product>.<resource>.<action> --full
  ecctl schema vpc.vpc.create vpc.vswitch.create ecs.sg.create`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if brief && full {
				return ecerrors.Client("InvalidParameter", "schema --brief cannot be combined with --full", ecerrors.WithField("brief"))
			}
			if len(args) > 0 && args[0] == "list" {
				list = true
				args = args[1:]
			}
			if product != "" {
				list = true
				args = []string{product}
			}
			if list || len(args) == 0 {
				if len(args) == 0 {
					products := schema.ProductsForLanguage(options.lang)
					if publicCLIFilterEnabled(options) {
						products = publicCLIProductSummaries(products)
					}
					return writeSchemaOutput(options, stdout, map[string]any{"products": products})
				}
				if publicCLIFilterEnabled(options) && !publicCLIProduct(args[0]) {
					return ecerrors.Client("UnknownSchema", "schema product is not supported")
				}
				surface, ok := schema.ProductListForLanguage(args[0], options.lang)
				if !ok {
					return ecerrors.Client("UnknownSchema", "schema product is not supported")
				}
				if publicCLIFilterEnabled(options) {
					surface = publicCLIProductSurface(surface)
				}
				return writeSchemaOutput(options, stdout, surface)
			}
			if len(args) < 1 {
				return ecerrors.Client("MissingSchema", "schema name is required")
			}
			mode := schema.CommandSchemaBrief
			if full {
				mode = schema.CommandSchemaFull
			}
			if len(args) == 1 && !strings.Contains(args[0], ".") {
				if surface, ok := schema.ProductListForLanguage(args[0], options.lang); ok {
					if publicCLIFilterEnabled(options) && !publicCLIProduct(args[0]) {
						return ecerrors.Client("UnknownSchema", "schema product is not supported")
					}
					if publicCLIFilterEnabled(options) {
						surface = publicCLIProductSurface(surface)
					}
					return writeSchemaOutput(options, stdout, surface)
				}
			}
			if len(args) >= 2 && len(args) <= 4 && schemaPathArgs(args) {
				name := strings.Join(args, ".")
				if publicCLIFilterEnabled(options) && !publicCLICommandSchema(name) {
					return ecerrors.Client("UnknownSchema", "schema command is not supported")
				}
				command, ok := schema.CommandForLanguageMode(name, options.lang, mode)
				if !ok {
					if surface, found := schema.ResourceForLanguageName(name, options.lang); found {
						return writeSchemaOutput(options, stdout, surface)
					}
					return ecerrors.Client("UnknownSchema", "schema command is not supported")
				}
				return writeSchemaOutput(options, stdout, command)
			}
			if len(args) == 1 {
				if publicCLIFilterEnabled(options) && !publicCLICommandSchema(args[0]) {
					return ecerrors.Client("UnknownSchema", "schema command is not supported")
				}
				command, ok := schema.CommandForLanguageMode(args[0], options.lang, mode)
				if ok {
					return writeSchemaOutput(options, stdout, command)
				}
				if surface, ok := schema.ResourceForLanguageName(args[0], options.lang); ok {
					return writeSchemaOutput(options, stdout, surface)
				}
				return ecerrors.Client("UnknownSchema", "schema command is not supported")
			}
			batch := make(map[string]any, len(args))
			for _, name := range args {
				command, ok := schema.CommandForLanguageMode(name, options.lang, mode)
				if ok {
					if !publicCLIFilterEnabled(options) || publicCLICommandSchema(name) {
						batch[name] = command
					} else {
						batch[name] = nil
					}
				} else {
					batch[name] = nil
				}
			}
			return writeSchemaOutput(options, stdout, batch)
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list supported products or resources")
	cmd.Flags().BoolVar(&brief, "brief", false, "show required and common parameters only")
	cmd.Flags().BoolVar(&full, "full", false, "show all schema-visible parameters")
	cmd.Flags().StringVar(&product, "product", "", "list schemas for a product")
	return cmd
}

func writeSchemaOutput(options *globalOptions, w io.Writer, value any) error {
	return writeSchemaOutputMode(options, w, value, false)
}

// writeSchemaOutputMode keeps the historical test seam for schema rendering,
// but rendering now follows the same display-mode policy as every other command.
func writeSchemaOutputMode(options *globalOptions, w io.Writer, value any, _ bool) error {
	return writeCommandOutput(options, w, value)
}

func newCapabilitiesCommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities",
		Short: "Describe machine-readable CLI capabilities",
		RunE: func(_ *cobra.Command, _ []string) error {
			capabilities, ok := schema.CapabilitiesForLanguage(options.lang)
			if !ok {
				return ecerrors.Client("UnknownSchema", "schema command is not supported")
			}
			if publicCLIFilterEnabled(options) {
				capabilities.Products = publicCLIProductSurfaces(capabilities.Products)
				capabilities.Surface = "public"
			} else {
				capabilities.Surface = "full"
			}
			return writeCommandOutput(options, stdout, capabilities)
		},
	}
}

func publicCLIFilterEnabled(options *globalOptions) bool {
	return (options == nil || !options.fullSurface) && os.Getenv("ECCTL_SPEC_DIR") == ""
}

func publicCLIProduct(product string) bool {
	switch product {
	case "ack", "ecs", "lingjun", "rg", "tag", "vpc":
		return true
	default:
		return false
	}
}

func publicCLIProductSummaries(products []schema.ProductSummary) []schema.ProductSummary {
	filtered := make([]schema.ProductSummary, 0, len(products))
	for _, product := range products {
		if publicCLIProduct(product.Name) {
			filtered = append(filtered, product)
		}
	}
	return filtered
}

func publicCLIProductSurfaces(products []schema.ProductSurface) []schema.ProductSurface {
	filtered := make([]schema.ProductSurface, 0, len(products))
	for _, product := range products {
		if !publicCLIProduct(product.Product) {
			continue
		}
		filtered = append(filtered, publicCLIProductSurface(product))
	}
	return filtered
}

func publicCLIProductSurface(surface schema.ProductSurface) schema.ProductSurface {
	resources := make([]schema.ResourceSurface, 0, len(surface.Resources))
	for _, resource := range surface.Resources {
		if publicCLIResource(surface.Product, resource.Name) {
			resources = append(resources, resource)
		}
	}
	surface.Resources = resources
	return surface
}

func publicCLICommandSchema(name string) bool {
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 1:
		return publicCLIProduct(parts[0])
	case 2, 3:
		return publicCLIResourceIdentifier(parts[0], parts[1])
	case 4:
		return publicCLIProduct(parts[0]) && publicCLIResource(parts[0], parts[2])
	default:
		return false
	}
}

func schemaPathArgs(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, ".") {
			return false
		}
	}
	return true
}

func groupCommandForLanguage(use string, short string, lang string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options := []ecerrors.Option{}
				if suggestion := unknownCommandSuggestion(commandPathPositionals(cmd, args), lang); suggestion != "" {
					options = append(options, ecerrors.WithSuggestion(suggestion))
				}
				return ecerrors.Client("UnknownCommand", "command is not supported", options...)
			}
			return cmd.Help()
		},
	}
}

func commandPathPositionals(cmd *cobra.Command, args []string) []string {
	positionals := strings.Fields(cmd.CommandPath())
	if len(positionals) > 0 && positionals[0] == "ecctl" {
		positionals = positionals[1:]
	}
	return append(positionals, args...)
}

func exactArgs(n int) cobra.PositionalArgs {
	return argsRange(n, n)
}

func argsRange(min int, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		if len(args) < min {
			return ecerrors.Client("MissingParameter", "missing required parameters: "+missingPositionalName(cmd.Use, len(args)))
		}
		if min == max {
			return ecerrors.Client("UnknownCommand", fmt.Sprintf("expected %d arguments, got %d", max, len(args)))
		}
		return ecerrors.Client("UnknownCommand", fmt.Sprintf("expected between %d and %d arguments, got %d", min, max, len(args)))
	}
}

func missingPositionalName(use string, provided int) string {
	position := 0
	for _, field := range strings.Fields(use) {
		if strings.HasPrefix(field, "<") && strings.HasSuffix(field, ">") {
			if position == provided {
				return field
			}
			position++
		}
	}
	return "<argument>"
}

func newConfigCommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "configure",
		Aliases: []string{"config"},
		Short:   "Manage ecctl configuration",
		Example: `  ecctl configure get
  ecctl configure set region cn-hangzhou
  ecctl configure list
  ecctl configure use production`,
	}
	var getShowSecret bool
	getCmd := &cobra.Command{
		Use:     "get [key]",
		Short:   "Print active config",
		Example: "  ecctl configure get\n  ecctl configure get region\n  ecctl configure get access-key-secret --show-secret",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profileName := config.ProfileName(options.profile, os.Getenv)
			if len(args) == 1 {
				value, err := config.EffectiveValue(profileName, args[0], getShowSecret, config.EcctlConfigPath(os.Getenv), config.AliyunConfigPath(os.Getenv))
				if err != nil {
					return configValueError(args[0], err)
				}
				return writeCommandOutput(options, stdout, configValuePayload(profileName, value))
			}
			profile, ok, err := config.EffectiveProfile(profileName, config.EcctlConfigPath(os.Getenv), config.AliyunConfigPath(os.Getenv))
			if err != nil {
				return ecerrors.Client("InvalidConfig", err.Error())
			}
			if !ok {
				return ecerrors.Client("ProfileNotFound", "profile is not configured")
			}
			region := firstNonEmpty(options.region, os.Getenv("ECCTL_REGION"), profile.Region)
			return writeCommandOutput(options, stdout, map[string]any{
				"profile": profile.Name,
				"mode":    profile.Mode,
				"region":  region,
				"lang":    profile.Language,
				"output":  firstNonEmpty(profile.Output, "json"),
			})
		},
	}
	getCmd.Flags().BoolVar(&getShowSecret, "show-secret", false, "show sensitive config values")
	cmd.AddCommand(getCmd)
	var setRegion string
	var setNoVerify bool
	setCmd := &cobra.Command{
		Use:     "set <key> <value>",
		Short:   "Set config values",
		Long:    "Supported settings: " + supportedConfigKeys(),
		Example: "  ecctl configure set region cn-hangzhou\n  ecctl configure set access-key-id <value>\n  ecctl configure set lang zh-CN\n  ecctl configure set output text",
		Args:    cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 && len(args) != 2 {
				return ecerrors.Client("MissingParameter", "missing required parameters: <key>, <value>")
			}
			if len(args) == 0 && setRegion == "" {
				return ecerrors.Client("MissingRegion", "region is required")
			}
			store, err := config.LoadStore(config.EcctlConfigPath(os.Getenv))
			if err != nil {
				return ecerrors.Client("InvalidConfig", err.Error())
			}
			profileName := config.ProfileName(options.profile, os.Getenv)
			key := "region"
			value := setRegion
			if len(args) == 2 {
				key = args[0]
				value = args[1]
			}
			var warnings []string
			if isRegionConfigKey(key) && !setNoVerify {
				if warning, verr := verifyRegionForConfig(profileName, value); verr != nil {
					return verr
				} else if warning != "" {
					warnings = append(warnings, warning)
				}
			}
			configValue, err := store.SetValue(profileName, key, value)
			if err != nil {
				return configValueError(key, err)
			}
			if err := store.Save(); err != nil {
				return ecerrors.Client("ConfigWriteFailed", err.Error())
			}
			payload := configValuePayload(profileName, configValue)
			if len(warnings) > 0 {
				payload["warnings"] = warnings
			}
			return writeCommandOutput(options, stdout, payload)
		},
	}
	setCmd.Flags().StringVar(&setRegion, "region", "", "default region")
	setCmd.Flags().BoolVar(&setNoVerify, "no-verify", false, "skip online verification of the region against the Alibaba Cloud Location service")
	cmd.AddCommand(setCmd)
	var listShowSecret bool
	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "List supported config keys",
		Example: "  ecctl configure list\n  ecctl configure list --show-secret",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			profileName := config.ProfileName(options.profile, os.Getenv)
			items, err := config.EffectiveItems(profileName, listShowSecret, config.EcctlConfigPath(os.Getenv), config.AliyunConfigPath(os.Getenv))
			if err != nil {
				return ecerrors.Client("InvalidConfig", err.Error())
			}
			return writeCommandOutput(options, stdout, map[string]any{
				"profile": effectiveProfile(options),
				"items":   items,
			})
		},
	}
	listCmd.Flags().BoolVar(&listShowSecret, "show-secret", false, "show sensitive config values")
	cmd.AddCommand(listCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "use <profile>",
		Short:   "Switch active profile",
		Example: "  ecctl configure use default\n  ecctl configure use production\n  ecctl --profile prod configure get",
		Args:    exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profile, ok, err := config.EffectiveProfile(args[0], config.EcctlConfigPath(os.Getenv), config.AliyunConfigPath(os.Getenv))
			if err != nil {
				return ecerrors.Client("InvalidConfig", err.Error())
			}
			if !ok {
				return ecerrors.Client("ProfileNotFound", fmt.Sprintf("profile %s not found", args[0]))
			}
			store, err := config.LoadStore(config.EcctlConfigPath(os.Getenv))
			if err != nil {
				return ecerrors.Client("InvalidConfig", err.Error())
			}
			if err := store.SetCurrentProfile(args[0]); err != nil {
				return ecerrors.Client("ProfileNotFound", err.Error())
			}
			if err := store.Save(); err != nil {
				return ecerrors.Client("ConfigWriteFailed", err.Error())
			}
			return writeCommandOutput(options, stdout, map[string]any{"profile": profile.Name, "region": profile.Region})
		},
	})
	return cmd
}

func supportedConfigKeys() string {
	items := config.SupportedItems()
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return strings.Join(keys, ", ")
}

type requiredFlagStatus struct {
	name    string
	missing bool
}

func requiredFlag(name string, missing bool) requiredFlagStatus {
	return requiredFlagStatus{name: name, missing: missing}
}

func missingRequiredFlags(flags ...requiredFlagStatus) error {
	missing := make([]string, 0, len(flags))
	for _, flag := range flags {
		if flag.missing {
			missing = append(missing, flag.name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return ecerrors.Client("MissingParameter", "missing required parameters: "+strings.Join(missing, ", "))
}

func markResourceFlags(cmd *cobra.Command, names ...string) {
	markFlagAnnotation(cmd, flagGroupAnnotation, flagGroupResource, names...)
}

func markRequiredHelpFlags(cmd *cobra.Command, names ...string) {
	markFlagAnnotation(cmd, flagRequiredAnnotation, "true", names...)
}

func markFlagAnnotation(cmd *cobra.Command, key string, value string, names ...string) {
	for _, name := range names {
		flag := cmd.Flags().Lookup(strings.TrimPrefix(name, "--"))
		if flag == nil {
			continue
		}
		if flag.Annotations == nil {
			flag.Annotations = map[string][]string{}
		}
		flag.Annotations[key] = []string{value}
	}
}

func requireRegion(options *globalOptions) error {
	_, err := resolveRegion(options)
	return err
}

func resolveRegion(options *globalOptions) (string, error) {
	region, appErr := config.ResolveRegionForProfile(
		options.region,
		config.ProfileName(options.profile, os.Getenv),
		config.ConfigPath(os.Getenv),
		os.Getenv,
	)
	if appErr != nil {
		return "", appErr
	}
	return region, nil
}

func validatePagination(limit int, page int) error {
	if limit <= 0 {
		return ecerrors.Client("InvalidLimit", "--limit must be greater than zero", ecerrors.WithField("limit"))
	}
	if page <= 0 {
		return ecerrors.Client("InvalidPage", "--page must be greater than zero", ecerrors.WithField("page"))
	}
	return nil
}

func paginationPayload(page, limit, returned, total int, nextToken ...string) map[string]any {
	token := ""
	if len(nextToken) > 0 {
		token = nextToken[0]
	}
	hasMore := page*limit < total
	if token != "" {
		hasMore = true
	}
	payload := map[string]any{
		"has_more": hasMore,
		"limit":    limit,
		"page":     page,
		"returned": returned,
	}
	if token != "" {
		payload["next_token"] = token
	} else if hasMore {
		payload["next_page"] = page + 1
	}
	return payload
}

func requestedOutputMode(args []string) (string, bool) {
	return requestedFlagValue(args, "output")
}

func requestedLanguage(args []string) string {
	value, _ := requestedFlagValue(args, "lang")
	return value
}

func requestedProfile(args []string) string {
	value, _ := requestedProfileFlagValue(args)
	return value
}

func requestedBoolFlag(args []string, name string) bool {
	flag := "--" + name
	prefix := flag + "="
	for _, arg := range args {
		if arg == flag {
			return true
		}
		if strings.HasPrefix(arg, prefix) {
			value := strings.TrimPrefix(arg, prefix)
			return value != "false" && value != "0"
		}
	}
	return false
}

func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func normalizeHelpTopicArgs(args []string) []string {
	helpIndex := helpCommandArgIndex(args)
	if helpIndex < 0 || helpIndex+1 >= len(args) {
		return args
	}
	topic := args[helpIndex+1]
	if strings.HasPrefix(topic, "-") || !strings.Contains(topic, ".") {
		return args
	}
	parts := strings.Split(topic, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return args
		}
	}
	out := make([]string, 0, len(args)+len(parts)-1)
	out = append(out, args[:helpIndex+1]...)
	out = append(out, parts...)
	out = append(out, args[helpIndex+2:]...)
	return out
}

func helpCommandArgIndex(args []string) int {
	skipNext := false
	for index, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name := arg
			hasInlineValue := false
			if split := strings.Index(arg, "="); split >= 0 {
				name = arg[:split]
				hasInlineValue = true
			}
			if commandValueFlag(name) && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "help" {
			return index
		}
		return -1
	}
	return -1
}

func callCommandRequested(args []string) bool {
	positionals := commandPositionals(args)
	return len(positionals) > 0 && positionals[0] == "call"
}

func normalizeAPICallParameterFlags(args []string) []string {
	callIndex := apiCallCommandIndex(args)
	if callIndex < 0 || apiCallUsesMetadataMode(args[callIndex+1:]) {
		return args
	}
	out := make([]string, 0, len(args))
	out = append(out, args[:callIndex+1]...)
	positionals := 0
	for i := callIndex + 1; i < len(args); i++ {
		arg := args[i]
		if positionals < 2 {
			if apiCallAliyunCLIFlag(arg) && !apiCallLocalMetadataFlag(arg) {
				out = appendAliyunCLIFlagArg(out, arg)
				if apiCallAliyunCLIValueFlag(arg) && !longFlagHasInlineValue(arg) && i+1 < len(args) {
					i++
					out = appendAPICommandArg(out, args[i])
				}
				out = appendAliyunCLIExtraValues(out, args, &i, arg)
				continue
			}
			out = append(out, arg)
			if apiCallKnownValueFlag(arg) && !longFlagHasInlineValue(arg) && i+1 < len(args) {
				i++
				out = append(out, args[i])
				continue
			}
			if arg == "--" || strings.HasPrefix(arg, "-") {
				continue
			}
			positionals++
			continue
		}
		if !strings.HasPrefix(arg, "--") || apiCallKnownFlag(arg) {
			if apiCallAliyunCLIFlag(arg) {
				out = appendAPICommandArg(out, arg)
			} else if !strings.HasPrefix(arg, "-") {
				out = appendAPICommandArg(out, arg)
			} else {
				out = append(out, arg)
			}
			if apiCallKnownValueFlag(arg) && !longFlagHasInlineValue(arg) && i+1 < len(args) {
				i++
				if apiCallAliyunCLIFlag(arg) || !strings.HasPrefix(arg, "-") {
					out = appendAPICommandArg(out, args[i])
					out = appendAliyunCLIExtraValues(out, args, &i, arg)
				} else {
					out = append(out, args[i])
				}
			}
			continue
		}
		if apiCallAliyunCLIFlag(arg) {
			out = appendAliyunCLIFlagArg(out, arg)
			if apiCallAliyunCLIValueFlag(arg) && !longFlagHasInlineValue(arg) && i+1 < len(args) {
				i++
				out = appendAPICommandArg(out, args[i])
			}
			out = appendAliyunCLIExtraValues(out, args, &i, arg)
			continue
		}
		name, value, hasValue := splitLongFlag(arg)
		if name == "" {
			out = append(out, arg)
			continue
		}
		if !hasValue {
			value = "true"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value = args[i]
			}
		}
		out = append(out, "--api-param", name+"="+value)
	}
	return out
}

func appendAPICommandArg(out []string, arg string) []string {
	return append(out, "--aliyun-arg="+arg)
}

func appendAliyunCLIFlagArg(out []string, arg string) []string {
	name, value, hasValue := splitLongFlag(arg)
	if name == "" {
		return appendAPICommandArg(out, arg)
	}
	out = appendAPICommandArg(out, "--"+name)
	if hasValue {
		out = appendAPICommandArg(out, value)
	}
	return out
}

func appendAliyunCLIExtraValues(out []string, args []string, index *int, flag string) []string {
	if !apiCallAliyunCLIFlag(flag) || !apiCallAliyunCLIFlagAcceptsExtraValues(flag) {
		return out
	}
	for *index+1 < len(args) && !strings.HasPrefix(args[*index+1], "-") && strings.Contains(args[*index+1], "=") {
		*index = *index + 1
		out = appendAPICommandArg(out, args[*index])
	}
	return out
}

func apiCallLocalMetadataFlag(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func apiCallPassthroughHasFlag(args []string, name string) bool {
	_, ok := apiCallPassthroughFlagValue(args, name)
	return ok
}

func apiCallPassthroughFlagValue(args []string, name string) (string, bool) {
	long := "--" + name
	longPrefix := long + "="
	short := ""
	if name == "profile" {
		short = "-p"
	}
	for i, arg := range args {
		if arg == long || (short != "" && arg == short) {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(arg, longPrefix) {
			return strings.TrimPrefix(arg, longPrefix), true
		}
		if short != "" && strings.HasPrefix(arg, short+"=") {
			return strings.TrimPrefix(arg, short+"="), true
		}
	}
	return "", false
}

func apiCallCommandIndex(args []string) int {
	skipNext := false
	for index, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if apiCallGlobalValueFlag(arg) && !longFlagHasInlineValue(arg) {
			skipNext = true
			continue
		}
		if arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "call" {
			return index
		}
		return -1
	}
	return -1
}

func apiCallUsesMetadataMode(args []string) bool {
	for _, arg := range args {
		name, _, _ := splitLongFlag(arg)
		if name == "list" || name == "schema" {
			return true
		}
	}
	return false
}

func splitLongFlag(arg string) (string, string, bool) {
	if !strings.HasPrefix(arg, "--") || arg == "--" {
		return "", "", false
	}
	name := strings.TrimPrefix(arg, "--")
	if before, after, ok := strings.Cut(name, "="); ok {
		return before, after, true
	}
	return name, "", false
}

func longFlagHasInlineValue(arg string) bool {
	_, _, ok := splitLongFlag(arg)
	return ok
}

func apiCallKnownFlag(arg string) bool {
	name, _, ok := splitLongFlag(arg)
	if !ok && !strings.HasPrefix(arg, "--") {
		return apiCallAliyunCLIFlag(arg)
	}
	switch "--" + name {
	case "--request", "--api-param", "--filter", "--limit", "--region", "--profile", "--lang",
		"--list", "--schema", "--generate-request", "--json", "--agent-envelope", "--no-color":
		return true
	default:
		return apiCallAliyunCLIFlag(arg)
	}
}

func apiCallKnownValueFlag(arg string) bool {
	name, _, ok := splitLongFlag(arg)
	if !ok && !strings.HasPrefix(arg, "--") {
		return false
	}
	switch "--" + name {
	case "--request", "--api-param", "--filter", "--limit", "--region", "--profile", "--lang":
		return true
	default:
		return apiCallAliyunCLIValueFlag(arg)
	}
}

func apiCallGlobalValueFlag(arg string) bool {
	name, _, ok := splitLongFlag(arg)
	if !ok && !strings.HasPrefix(arg, "--") {
		return false
	}
	switch "--" + name {
	case "--region", "--profile", "--lang", "--output":
		return true
	default:
		return false
	}
}

func apiCallAliyunCLIFlag(arg string) bool {
	if strings.HasPrefix(arg, "--") {
		name, _, _ := splitLongFlag(arg)
		return apiCallAliyunCLILongFlags()[name]
	}
	if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	name := strings.TrimPrefix(arg, "-")
	return apiCallAliyunCLIShortFlags()[name]
}

func apiCallAliyunCLIValueFlag(arg string) bool {
	if strings.HasPrefix(arg, "--") {
		name, _, _ := splitLongFlag(arg)
		return apiCallAliyunCLIValueFlags()[name]
	}
	if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	name := strings.TrimPrefix(arg, "-")
	switch name {
	case "p", "e", "o":
		return true
	default:
		return false
	}
}

func apiCallAliyunCLIFlagAcceptsExtraValues(arg string) bool {
	if !strings.HasPrefix(arg, "--") {
		return false
	}
	name, _, _ := splitLongFlag(arg)
	return name == "waiter"
}

func apiCallAliyunCLILongFlags() map[string]bool {
	flags := map[string]bool{}
	for _, name := range []string{
		"mode", "language", "config-path", "access-key-id", "access-key-secret",
		"sts-token", "sts-region", "ram-role-name", "ram-role-arn", "source-profile",
		"role-session-name", "external-id", "private-key", "key-pair-name",
		"read-timeout", "connect-timeout", "retry-count", "skip-secure-verify",
		"expired-seconds", "process-command", "oidc-provider-arn", "oidc-token-file",
		"cloud-sso-sign-in-url", "cloud-sso-access-config", "cloud-sso-account-id",
		"oauth-site-type", "endpoint-type", "endpoint", "external-account-type",
		"auto-plugin-install", "auto-plugin-install-enable-pre", "bearer-token",
		"bearer-token-header-key", "secure", "force", "version", "header", "body",
		"body-file", "pager", "accept", "output", "waiter", "dryrun", "quiet",
		"yes", "cli-query", "roa", "method", "user-agent", "cli-ai-mode",
		"no-cli-ai-mode", "help",
	} {
		flags[name] = true
	}
	return flags
}

func apiCallAliyunCLIValueFlags() map[string]bool {
	flags := map[string]bool{}
	for _, name := range []string{
		"mode", "language", "config-path", "access-key-id", "access-key-secret",
		"sts-token", "sts-region", "ram-role-name", "ram-role-arn", "source-profile",
		"role-session-name", "external-id", "private-key", "key-pair-name",
		"read-timeout", "connect-timeout", "retry-count", "expired-seconds",
		"process-command", "oidc-provider-arn", "oidc-token-file",
		"cloud-sso-sign-in-url", "cloud-sso-access-config", "cloud-sso-account-id",
		"oauth-site-type", "endpoint-type", "endpoint", "external-account-type",
		"auto-plugin-install", "auto-plugin-install-enable-pre", "bearer-token",
		"bearer-token-header-key", "version", "header", "body", "body-file",
		"accept", "output", "waiter", "cli-query", "method", "user-agent",
	} {
		flags[name] = true
	}
	return flags
}

func apiCallAliyunCLIShortFlags() map[string]bool {
	return map[string]bool{
		"p": true,
		"e": true,
		"o": true,
		"q": true,
		"y": true,
		"h": true,
	}
}

func allowHelpWithUnknownFlags(cmd *cobra.Command) {
	cmd.FParseErrWhitelist.UnknownFlags = true
	for _, child := range cmd.Commands() {
		allowHelpWithUnknownFlags(child)
	}
}

func requestedFlagValue(args []string, name string) (string, bool) {
	flag := "--" + name
	prefix := flag + "="
	for i, arg := range args {
		if arg == flag {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix), true
		}
	}
	return "", false
}

func requestedProfileFlagValue(args []string) (string, bool) {
	flag := "--profile"
	prefix := flag + "="
	for i, arg := range args {
		if arg == flag {
			if ackCreateLocalProfileFlag(args, i) {
				continue
			}
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(arg, prefix) {
			if ackCreateLocalProfileFlag(args, i) {
				continue
			}
			return strings.TrimPrefix(arg, prefix), true
		}
	}
	return "", false
}

func ackCreateLocalProfileFlag(args []string, flagIndex int) bool {
	positionals := make([]string, 0, 2)
	skipNext := false
	for i, arg := range args {
		if i >= flagIndex {
			break
		}
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name := arg
			hasInlineValue := false
			if index := strings.Index(arg, "="); index >= 0 {
				name = arg[:index]
				hasInlineValue = true
			}
			if commandValueFlag(name) && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		positionals = append(positionals, arg)
	}
	return len(positionals) >= 2 && positionals[0] == "ack" && positionals[1] == "create"
}

func localizeHelp(root *cobra.Command, lang string, noColor bool) {
	localizer := i18n.NewLocalizer(lang)
	localizeCommandHelp(root, localizer, lang, noColor)
}

func localizeCommandHelp(cmd *cobra.Command, localizer *i18n.Localizer, lang string, noColor bool) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[helpLangAnnotation] = lang
	if noColor {
		cmd.Annotations[helpNoColorAnnotation] = "true"
	} else {
		delete(cmd.Annotations, helpNoColorAnnotation)
	}
	if translated := localizer.CommandShort(cmd.CommandPath(), cmd.Short); translated != cmd.Short {
		cmd.Short = translated
		cmd.Long = ""
	}
	cmd.Example = localizer.CommandExample(cmd.CommandPath(), cmd.Example)
	cmd.Long = localizer.CommandLongData(cmd.CommandPath(), cmd.Long, map[string]any{"Keys": supportedConfigKeys()})
	if cmd.CommandPath() == "ecctl" {
		cmd.Long = localizer.Message("RootLong")
		cmd.Example = localizer.Message("RootExample")
	}
	for _, group := range cmd.Groups() {
		group.Title = localizer.CommandGroupTitle(group.ID, group.Title)
	}
	cmd.SetHelpTemplate(localizedHelpTemplate)
	cmd.SetUsageTemplate(localizedUsageTemplate)
	cmd.InitDefaultHelpFlag()
	if flag := cmd.Flags().Lookup("help"); flag != nil {
		flag.Usage = localizer.Message("HelpFlagHelp")
	}
	localizeFlagSet(cmd.Flags(), localizer)
	localizeFlagSet(cmd.PersistentFlags(), localizer)
	for _, child := range cmd.Commands() {
		localizeCommandHelp(child, localizer, lang, noColor)
	}
}

func localizeFlagSet(flags *pflag.FlagSet, localizer *i18n.Localizer) {
	flags.VisitAll(func(flag *pflag.Flag) {
		flag.Usage = localizer.FlagUsage(flag.Usage)
	})
}

func zhHasResourceFlags(flags *pflag.FlagSet) bool {
	return hasVisibleFlag(flags, isBriefResourceFlag)
}

func zhHasGeneralFlags(flags *pflag.FlagSet) bool {
	return hasVisibleFlag(flags, func(flag *pflag.Flag) bool {
		return !isResourceFlag(flag)
	})
}

func zhHasCommandFlags(cmd *cobra.Command) bool {
	return len(commandFlags(cmd)) > 0
}

func zhHasGlobalFlags(cmd *cobra.Command) bool {
	return len(globalFlags(cmd)) > 0
}

func helpLabel(cmd *cobra.Command, id string) string {
	return localizerForCommand(cmd).Message(id)
}

func helpFlagGroupLabel(cmd *cobra.Command, id string, hasRequired bool) string {
	label := helpLabel(cmd, id)
	if hasRequired {
		label += localizerForCommand(cmd).Message("HelpRequiredLegend")
	}
	return label
}

func helpGroupTitle(cmd *cobra.Command, id string, fallback string) string {
	return localizerForCommand(cmd).CommandGroupTitle(id, fallback)
}

func helpResourceFlagLabel(cmd *cobra.Command, flags *pflag.FlagSet) string {
	return helpFlagGroupLabel(cmd, "HelpResourceFlags", hasRequiredFlag(collectFlags(flags, isBriefResourceFlag)))
}

func helpResourceFlagUsages(cmd *cobra.Command, flags *pflag.FlagSet) string {
	return renderFlagUsages(flags, isBriefResourceFlag, localizerForCommand(cmd))
}

func helpCommandFlagLabel(cmd *cobra.Command) string {
	return helpFlagGroupLabel(cmd, "HelpCommandFlags", hasRequiredFlag(commandFlags(cmd)))
}

func helpCommandFlagUsages(cmd *cobra.Command) string {
	return renderFlagList(commandFlags(cmd), localizerForCommand(cmd))
}

func helpGlobalFlagLabel(cmd *cobra.Command) string {
	return helpFlagGroupLabel(cmd, "HelpGlobalFlags", hasRequiredFlag(globalFlags(cmd)))
}

func helpGlobalFlagUsages(cmd *cobra.Command) string {
	return renderFlagList(globalFlags(cmd), localizerForCommand(cmd))
}

func helpUseCommandHelp(cmd *cobra.Command) string {
	return localizerForCommand(cmd).MessageData("HelpUseCommandHelp", map[string]any{"CommandPath": cmd.CommandPath()})
}

func helpUseLine(cmd *cobra.Command) string {
	return highlightHelpCommandLine(cmd, cmd.UseLine())
}

func helpExample(cmd *cobra.Command) string {
	return highlightHelpCommandBlock(cmd, cmd.Example)
}

func helpSchemaHint(cmd *cobra.Command) string {
	action := cmd.Annotations[actionKeyAnnotation]
	if action == "" {
		return ""
	}
	hint := localizerForCommand(cmd).MessageData("HelpSchemaHint", map[string]any{"Action": action})
	return highlightHelpCommandBlock(cmd, hint)
}

func highlightHelpCommandBlock(cmd *cobra.Command, text string) string {
	if text == "" || !shouldColorHelp(cmd) {
		return text
	}
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		lines[i] = highlightHelpCommandLine(cmd, line)
	}
	return strings.Join(lines, "")
}

func highlightHelpCommandLine(cmd *cobra.Command, line string) string {
	if line == "" || !shouldColorHelp(cmd) {
		return line
	}
	ending := ""
	if strings.HasSuffix(line, "\n") {
		ending = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	indentLen := 0
	for indentLen < len(line) && (line[indentLen] == ' ' || line[indentLen] == '\t') {
		indentLen++
	}
	indent := line[:indentLen]
	body := line[indentLen:]
	if !isHighlightableHelpCommand(body) {
		return indent + body + ending
	}
	return indent + highlightHelpCommandTokens(body) + ending
}

func shouldColorHelp(cmd *cobra.Command) bool {
	if aiDisplayMode(cmd.OutOrStdout()) {
		return false
	}
	for current := cmd; current != nil; current = current.Parent() {
		if current.Annotations != nil && current.Annotations[helpNoColorAnnotation] == "true" {
			return false
		}
	}
	return true
}

func isHighlightableHelpCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "ecctl" ||
		strings.HasPrefix(trimmed, "ecctl ") ||
		strings.HasPrefix(trimmed, "$ ecctl ") ||
		strings.HasPrefix(trimmed, "go install ")
}

func highlightHelpCommandTokens(line string) string {
	var buf strings.Builder
	tokenIndex := 0
	sawExecutable := false
	for i := 0; i < len(line); {
		j := i
		if line[i] == ' ' || line[i] == '\t' {
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			buf.WriteString(line[i:j])
			i = j
			continue
		}
		for j < len(line) && line[j] != ' ' && line[j] != '\t' {
			j++
		}
		token := line[i:j]
		buf.WriteString(highlightHelpCommandToken(token, tokenIndex, sawExecutable))
		if token == "ecctl" || token == "go" {
			sawExecutable = true
		}
		tokenIndex++
		i = j
	}
	return buf.String()
}

func highlightHelpCommandToken(token string, index int, sawExecutable bool) string {
	switch {
	case token == "$":
		return helpColorize("32", token)
	case token == "ecctl" || (index == 0 && token == "go"):
		return helpColorize("1;36", token)
	case strings.HasPrefix(token, "-"):
		return helpColorize("35", token)
	case strings.Contains(token, "=") || strings.Contains(token, "/") || strings.HasPrefix(token, "<") || strings.HasPrefix(token, "[") || strings.HasPrefix(token, "'") || strings.HasPrefix(token, "\""):
		return helpColorize("32", token)
	case sawExecutable:
		return helpColorize("33", token)
	default:
		return token
	}
}

func helpColorize(code string, value string) string {
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func isRootCommand(cmd *cobra.Command) bool {
	return cmd.CommandPath() == "ecctl"
}

func localizerForCommand(cmd *cobra.Command) *i18n.Localizer {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Annotations == nil {
			continue
		}
		if lang := current.Annotations[helpLangAnnotation]; lang != "" {
			return i18n.NewLocalizer(lang)
		}
	}
	return i18n.NewLocalizer("en")
}

func hasVisibleFlag(flags *pflag.FlagSet, include func(*pflag.Flag) bool) bool {
	found := false
	flags.VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden && include(flag) {
			found = true
		}
	})
	return found
}

func commandFlags(cmd *cobra.Command) []*pflag.Flag {
	if !cmd.HasParent() {
		return nil
	}
	return collectFlags(cmd.LocalFlags(), func(flag *pflag.Flag) bool {
		return !isResourceFlag(flag) && !isHelpFlag(flag)
	})
}

func globalFlags(cmd *cobra.Command) []*pflag.Flag {
	if !cmd.HasParent() {
		return collectFlags(cmd.LocalFlags(), func(flag *pflag.Flag) bool {
			return !isResourceFlag(flag)
		})
	}
	flags := make([]*pflag.Flag, 0)
	if help := cmd.LocalFlags().Lookup("help"); help != nil && !help.Hidden {
		flags = append(flags, help)
	}
	flags = append(flags, collectFlags(cmd.InheritedFlags(), func(_ *pflag.Flag) bool {
		return true
	})...)
	return flags
}

func renderFlagUsages(flags *pflag.FlagSet, include func(*pflag.Flag) bool, localizer *i18n.Localizer) string {
	return renderFlagList(collectFlags(flags, include), localizer)
}

func collectFlags(flags *pflag.FlagSet, include func(*pflag.Flag) bool) []*pflag.Flag {
	selected := make([]*pflag.Flag, 0)
	flags.VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden && include(flag) {
			selected = append(selected, flag)
		}
	})
	return selected
}

func hasRequiredFlag(flags []*pflag.Flag) bool {
	for _, flag := range flags {
		if isRequiredHelpFlag(flag) {
			return true
		}
	}
	return false
}

type flagUsageLine struct {
	text     string
	priority int
}

func renderFlagList(flags []*pflag.Flag, localizer *i18n.Localizer) string {
	if localizer == nil {
		localizer = i18n.NewLocalizer("en")
	}
	var buf bytes.Buffer
	lines := make([]flagUsageLine, 0, len(flags))
	maxLen := 0

	for _, flag := range flags {
		line := ""
		marker := "  "
		if isRequiredHelpFlag(flag) {
			marker = "* "
		}
		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			line = fmt.Sprintf("  %s-%s, --%s", marker, flag.Shorthand, flag.Name)
		} else {
			line = fmt.Sprintf("  %s--%s", marker, flag.Name)
		}

		varName, usage := pflag.UnquoteUsage(flag)
		if varName != "" {
			line += " " + varName
		}
		if flag.NoOptDefVal != "" {
			switch flag.Value.Type() {
			case "string":
				line += fmt.Sprintf("[=\"%s\"]", flag.NoOptDefVal)
			case "bool", "boolfunc":
				if flag.NoOptDefVal != "true" {
					line += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
				}
			case "count":
				if flag.NoOptDefVal != "+1" {
					line += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
				}
			default:
				line += fmt.Sprintf("[=%s]", flag.NoOptDefVal)
			}
		}

		line += "\x00"
		if len(line) > maxLen {
			maxLen = len(line)
		}

		line += usage
		if !flagDefaultIsZeroValue(flag) {
			defaultValue := compactFlagDefault(flag)
			if flag.Value.Type() == "string" {
				line += " " + localizer.MessageData("HelpDefaultQuoted", map[string]any{"Value": defaultValue})
			} else {
				line += " " + localizer.MessageData("HelpDefaultValue", map[string]any{"Value": defaultValue})
			}
		}
		if flag.Deprecated != "" {
			line += fmt.Sprintf(" (DEPRECATED: %s)", flag.Deprecated)
		}

		lines = append(lines, flagUsageLine{text: line, priority: flagHelpPriority(flag)})
	}

	sort.SliceStable(lines, func(i, j int) bool {
		return lines[i].priority < lines[j].priority
	})

	for _, entry := range lines {
		line := entry.text
		separator := strings.Index(line, "\x00")
		spacing := strings.Repeat(" ", maxLen-separator)
		fmt.Fprintln(&buf, line[:separator], spacing, line[separator+1:])
	}
	return buf.String()
}

func isResourceFlag(flag *pflag.Flag) bool {
	return flagHasAnnotation(flag, flagGroupAnnotation, flagGroupResource)
}

func isBriefResourceFlag(flag *pflag.Flag) bool {
	return isResourceFlag(flag) && !flagHasAnnotation(flag, flagBriefAnnotation, "false")
}

func isHelpFlag(flag *pflag.Flag) bool {
	return flag.Name == "help"
}

func isRequiredHelpFlag(flag *pflag.Flag) bool {
	return flagHasAnnotation(flag, flagRequiredAnnotation, "true")
}

func flagHelpPriority(flag *pflag.Flag) int {
	switch {
	case isHelpFlag(flag):
		return -1
	case isRequiredHelpFlag(flag):
		return 0
	case flagHasAnnotation(flag, flagOrderedAnnotation, "true"):
		return 1
	case flag.Name == "dry-run":
		return 2
	case flag.Name == "api-param":
		return 3
	default:
		return 1
	}
}

func flagHasAnnotation(flag *pflag.Flag, key string, value string) bool {
	for _, existing := range flag.Annotations[key] {
		if existing == value {
			return true
		}
	}
	return false
}

func flagDefaultIsZeroValue(flag *pflag.Flag) bool {
	switch flag.Value.Type() {
	case "bool", "boolFunc", "boolfunc":
		return flag.DefValue == "false" || flag.DefValue == ""
	case "duration":
		return flag.DefValue == "0" || flag.DefValue == "0s"
	case "int", "int8", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count", "float32", "float64":
		return flag.DefValue == "0"
	case "string":
		return flag.DefValue == ""
	case "intSlice", "stringSlice", "stringArray":
		return flag.DefValue == "[]"
	default:
		switch flag.DefValue {
		case "false", "<nil>", "", "0":
			return true
		default:
			return false
		}
	}
}

func compactFlagDefault(flag *pflag.Flag) string {
	if flag.Value.Type() != "duration" {
		return flag.DefValue
	}
	duration, err := time.ParseDuration(flag.DefValue)
	if err != nil {
		return flag.DefValue
	}
	value := duration.String()
	for strings.HasSuffix(value, "m0s") || strings.HasSuffix(value, "h0s") {
		value = strings.TrimSuffix(value, "0s")
	}
	for strings.HasSuffix(value, "h0m") {
		value = strings.TrimSuffix(value, "0m")
	}
	return value
}

const localizedHelpTemplate = `{{with (or .Long .Short)}}{{.}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const localizedUsageTemplate = `{{$root := .}}{{helpLabel $root "HelpUsage"}}:
  {{helpUseLine .}}{{if .HasExample}}

{{helpLabel $root "HelpExamples"}}:
{{helpExample .}}{{end}}{{if .HasAvailableSubCommands}}
{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{helpLabel $root "HelpAvailableCommands"}}:
{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{helpGroupTitle $root .ID .Title}}
{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{helpLabel $root "HelpOtherCommands"}}:
{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}{{if zhHasResourceFlags .LocalFlags}}

{{helpResourceFlagLabel $root .LocalFlags}}:
{{helpResourceFlagUsages $root .LocalFlags | trimTrailingWhitespaces}}{{end}}{{if zhHasCommandFlags .}}

{{helpCommandFlagLabel .}}:
{{helpCommandFlagUsages . | trimTrailingWhitespaces}}{{else}}{{if not (zhHasResourceFlags .LocalFlags)}}{{if zhHasCommandFlags .}}

{{helpCommandFlagLabel .}}:
{{helpCommandFlagUsages . | trimTrailingWhitespaces}}{{end}}{{end}}{{end}}{{end}}{{if zhHasGlobalFlags .}}

{{helpGlobalFlagLabel .}}:
{{helpGlobalFlagUsages . | trimTrailingWhitespaces}}{{end}}{{if hasFilterableFields .}}

{{helpFilterableFieldsLabel .}}:
{{helpFilterableFieldsList . | trimTrailingWhitespaces}}{{end}}{{with helpSchemaHint .}}

{{.}}{{end}}{{if .HasHelpSubCommands}}

{{helpLabel $root "HelpOtherHelpTopics"}}:
{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{helpUseCommandHelp $root}}{{end}}
`

func init() {
	cobra.EnableCommandSorting = false
	cobra.AddTemplateFunc("helpLabel", helpLabel)
	cobra.AddTemplateFunc("helpGroupTitle", helpGroupTitle)
	cobra.AddTemplateFunc("helpResourceFlagLabel", helpResourceFlagLabel)
	cobra.AddTemplateFunc("helpResourceFlagUsages", helpResourceFlagUsages)
	cobra.AddTemplateFunc("helpCommandFlagLabel", helpCommandFlagLabel)
	cobra.AddTemplateFunc("helpCommandFlagUsages", helpCommandFlagUsages)
	cobra.AddTemplateFunc("helpGlobalFlagLabel", helpGlobalFlagLabel)
	cobra.AddTemplateFunc("helpGlobalFlagUsages", helpGlobalFlagUsages)
	cobra.AddTemplateFunc("helpUseCommandHelp", helpUseCommandHelp)
	cobra.AddTemplateFunc("helpUseLine", helpUseLine)
	cobra.AddTemplateFunc("helpExample", helpExample)
	cobra.AddTemplateFunc("helpSchemaHint", helpSchemaHint)
	cobra.AddTemplateFunc("zhHasResourceFlags", zhHasResourceFlags)
	cobra.AddTemplateFunc("zhHasCommandFlags", zhHasCommandFlags)
	cobra.AddTemplateFunc("zhHasGlobalFlags", zhHasGlobalFlags)
	cobra.AddTemplateFunc("hasFilterableFields", hasFilterableFields)
	cobra.AddTemplateFunc("isRootCommand", isRootCommand)
	cobra.AddTemplateFunc("helpFilterableFieldsLabel", helpFilterableFieldsLabel)
	cobra.AddTemplateFunc("helpFilterableFieldsList", helpFilterableFieldsList)
}

type productBuildTarget struct {
	product   string
	resource  string
	buildAll  bool
	stubsOnly bool
}

func productCommandBuildTarget(args []string) productBuildTarget {
	positional := commandPositionals(args)
	if len(positional) == 0 {
		return productBuildTarget{stubsOnly: true}
	}
	first := positional[0]
	switch first {
	case "call", "configure", "schema", "capabilities", "update", "__update":
		return productBuildTarget{stubsOnly: true}
	case "completion", "__complete", "__completeNoDesc":
		return productBuildTarget{buildAll: true}
	case "help":
		if len(positional) >= 2 && !isBuiltinRootCommand(positional[1]) {
			resource := ""
			if len(positional) >= 3 {
				resource = positional[2]
			}
			return productBuildTarget{product: positional[1], resource: resource}
		}
		return productBuildTarget{stubsOnly: true}
	default:
		resource := ""
		if len(positional) >= 2 {
			resource = positional[1]
		}
		return productBuildTarget{product: first, resource: resource}
	}
}

func commandPositionals(args []string) []string {
	positionals := make([]string, 0, len(args))
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name := arg
			hasInlineValue := false
			if index := strings.Index(arg, "="); index >= 0 {
				name = arg[:index]
				hasInlineValue = true
			}
			if commandValueFlag(name) && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		positionals = append(positionals, arg)
	}
	return positionals
}

func commandValueFlag(name string) bool {
	switch name {
	case "--fields", "--filter", "--limit", "--region", "--profile", "--lang", "--output", "--request", "--api-param":
		return true
	default:
		return false
	}
}

func isBuiltinRootCommand(name string) bool {
	switch name {
	case "call", "config", "configure", "schema", "capabilities", "examples", "update", "completion", "__complete", "__completeNoDesc", "__update", "help":
		return true
	default:
		return false
	}
}

func explicitEmptyRegion(args []string) bool {
	for i, arg := range args {
		if arg == "--region" {
			return i+1 >= len(args) || args[i+1] == ""
		}
		if arg == "--region=" {
			return true
		}
	}
	return false
}

func writeCommandOutput(options *globalOptions, w io.Writer, value any) error {
	mode := output.ModeJSON
	if options != nil && options.output != "" {
		mode = options.output
	}
	if options != nil && options.forceJSON {
		mode = output.ModeJSON
	}
	if shouldUseAgentEnvelope(options) {
		mode = output.ModeJSON
	} else if !output.IsSupportedMode(mode) {
		mode = output.ModeJSON
	}
	if shouldUseAgentEnvelope(options) {
		value = agentEnvelope(options, true, value)
	}
	noColor := options != nil && options.noColor
	return output.Write(w, mode, value, commandTextOptions(w, noColor))
}

func commandTextOptions(w io.Writer, noColor bool) output.TextOptions {
	aiDisplay := aiDisplayMode(w)
	return output.TextOptions{
		Color:       !aiDisplay && !noColor,
		CompactJSON: aiDisplay,
	}
}

func aiDisplayMode(w io.Writer) bool {
	switch displayMode(os.Getenv) {
	case displayModeAI:
		return true
	case displayModeHuman:
		return false
	default:
		return !writerIsTerminal(w)
	}
}

func displayMode(getenv func(string) string) string {
	value := strings.TrimSpace(getenv(displayModeEnv))
	if strings.EqualFold(value, displayModeAI) || strings.EqualFold(value, "agent") {
		return displayModeAI
	}
	if strings.EqualFold(value, displayModeHuman) {
		return displayModeHuman
	}
	if strings.EqualFold(value, displayModeAuto) {
		return displayModeAuto
	}
	return displayModeAuto
}

var writerIsTerminal = isTerminalWriter

func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok || file == nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func shouldColorOutput(w io.Writer, noColor ...bool) bool {
	if len(noColor) > 0 && noColor[0] {
		return false
	}
	return !aiDisplayMode(w)
}

func writeLocalizedError(w io.Writer, err *ecerrors.AppError, lang string, mode string, noColor ...bool) int {
	if err == nil {
		err = ecerrors.Client("InternalError", "internal error")
	}
	if !output.IsSupportedMode(mode) {
		mode = output.ModeJSON
	}
	localizer := i18n.NewLocalizer(lang)
	payload := localizer.ErrorPayload(err.Payload(), len(err.Actions()) > 0)
	out := map[string]any{"error": payload}
	actions := err.Actions()
	if len(actions) > 0 {
		out["actions"] = actions
	}
	disableColor := len(noColor) > 0 && noColor[0]
	_ = output.Write(w, mode, out, commandTextOptions(w, disableColor))
	return err.ExitCode()
}

func writeRunError(w io.Writer, options *globalOptions, err *ecerrors.AppError) int {
	mode := output.ModeJSON
	if options != nil && options.output != "" {
		mode = options.output
	}
	if options != nil && options.forceJSON {
		mode = output.ModeJSON
	}
	if shouldUseAgentEnvelope(options) {
		return writeAgentLocalizedError(w, options, err)
	}
	lang := ""
	noColor := false
	if options != nil {
		lang = options.lang
		noColor = options.noColor
	}
	return writeLocalizedError(w, err, lang, mode, noColor)
}

func writeAgentLocalizedError(w io.Writer, options *globalOptions, err *ecerrors.AppError) int {
	if err == nil {
		err = ecerrors.Client("InternalError", "internal error")
	}
	lang := ""
	noColor := false
	if options != nil {
		lang = options.lang
		noColor = options.noColor
	}
	localizer := i18n.NewLocalizer(lang)
	payload := localizer.ErrorPayload(err.Payload(), len(err.Actions()) > 0)
	out := agentEnvelope(options, false, payload)
	actions := err.Actions()
	if len(actions) > 0 {
		out["actions"] = actions
	}
	_ = output.Write(w, output.ModeJSON, out, commandTextOptions(w, noColor))
	return err.ExitCode()
}

func shouldUseAgentEnvelope(options *globalOptions) bool {
	return options != nil && options.agentEnvelope
}

func agentEnvelope(options *globalOptions, ok bool, payload any) map[string]any {
	command := "ecctl"
	if options != nil && options.command != "" {
		command = options.command
	}
	out := map[string]any{
		"ok":             ok,
		"schema_version": agentEnvelopeSchemaVersion,
		"command":        command,
	}
	if ok {
		out["result"] = payload
	} else {
		out["error"] = payload
	}
	return out
}

func configValuePayload(profile string, value config.ConfigValue) map[string]any {
	return map[string]any{
		"profile":   firstNonEmpty(profile, config.DefaultProfileName),
		"key":       value.Key,
		"value":     value.Value,
		"sensitive": value.Sensitive,
	}
}

func isRegionConfigKey(key string) bool {
	switch key {
	case "region", "region-id", "region_id":
		return true
	}
	return false
}

// verifyRegionForConfig calls the Location service to confirm the region is
// recognised. Returns a non-nil error when verification reports the region as
// invalid; returns a non-empty warning string (and nil error) when verification
// cannot be performed (no credentials / network unreachable) so the caller can
// surface a hint while still persisting the value.
func verifyRegionForConfig(profileName, region string) (string, *ecerrors.AppError) {
	if region == "" {
		return "", nil
	}
	verifier, err := newRegionVerifier(profileName, config.EcctlConfigPath(os.Getenv), os.Getenv)
	if err != nil {
		if code, message, ok := appErrorCodeMessage(err); ok && code == aliyun.ErrCodeVerificationUnavailable {
			return "region verification skipped: " + message, nil
		}
		return "", ecerrors.Client("InvalidConfig", err.Error())
	}
	if err := verifier.Verify(region, ""); err != nil {
		if code, message, ok := appErrorCodeMessage(err); ok {
			if code == aliyun.ErrCodeVerificationUnavailable {
				return "region verification skipped: " + message, nil
			}
			if code == aliyun.ErrCodeInvalidRegion {
				options := []ecerrors.Option{}
				if appErr, ok := err.(*ecerrors.AppError); ok {
					if suggestion := appErr.Payload().Suggestion; suggestion != "" {
						options = append(options, ecerrors.WithSuggestion(suggestion))
					}
				}
				return "", ecerrors.Client("InvalidRegion", message+"; pass --no-verify to bypass this check", options...)
			}
		}
		return "", ecerrors.Client("InvalidConfig", err.Error())
	}
	return "", nil
}

func appErrorCodeMessage(err error) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	appErr, ok := err.(*ecerrors.AppError)
	if !ok {
		return "", "", false
	}
	payload := appErr.Payload()
	return payload.Code, payload.Message, true
}

func configValueError(key string, err error) *ecerrors.AppError {
	if strings.Contains(err.Error(), "unknown config key") {
		return ecerrors.Client("UnknownConfigKey", err.Error())
	}
	switch key {
	case "region", "region-id", "region_id":
		return ecerrors.Client("InvalidRegion", err.Error())
	case "output", "output-format", "output_format":
		return unsupportedOutputModeError(err.Error())
	default:
		return ecerrors.Client("InvalidConfig", err.Error())
	}
}

func effectiveProfile(options *globalOptions) string {
	return firstNonEmpty(config.ProfileName(options.profile, os.Getenv), "default")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func splitIDs(positional []string, explicit string) []string {
	var ids []string
	if explicit != "" {
		for _, id := range strings.Split(explicit, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	for _, arg := range positional {
		for _, id := range strings.Split(arg, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
