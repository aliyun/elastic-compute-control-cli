package i18n

import (
	"os"
	"strings"
	"sync"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

type Localizer struct {
	localizer *i18n.Localizer
	language  string
}

type MessageSpec struct {
	ID   string
	Text map[string]string
}

type localizerCacheEntry struct {
	version   uint64
	localizer *Localizer
}

var (
	messageMu       sync.RWMutex
	registeredSpecs = map[string]map[string]string{}
	messageVersion  uint64

	localizerMu    sync.Mutex
	localizerCache = map[string]localizerCacheEntry{}
)

func RegisterMessage(spec MessageSpec) {
	if spec.ID == "" {
		return
	}
	localizerMu.Lock()
	defer localizerMu.Unlock()

	messageMu.Lock()
	registeredSpecs[spec.ID] = copyText(spec.Text)
	messageVersion++
	messageMu.Unlock()

	localizerCache = map[string]localizerCacheEntry{}
}

func NewLocalizer(tags ...string) *Localizer {
	explicit := ""
	if len(tags) > 0 && tags[0] != "" {
		explicit = tags[0]
	}
	tag := ResolveLanguage(explicit, os.Getenv)

	localizerMu.Lock()
	defer localizerMu.Unlock()

	version := registeredMessageVersion()
	if cached, ok := localizerCache[tag]; ok && cached.version == version {
		return cached.localizer
	}
	specs, version := registeredMessagesSnapshot()
	localizer := buildLocalizer(tag, specs)
	localizerCache[tag] = localizerCacheEntry{version: version, localizer: localizer}
	return localizer
}

func registeredMessageVersion() uint64 {
	messageMu.RLock()
	defer messageMu.RUnlock()
	return messageVersion
}

func registeredMessagesSnapshot() (map[string]map[string]string, uint64) {
	messageMu.RLock()
	defer messageMu.RUnlock()
	return copyRegisteredMessages(registeredSpecs), messageVersion
}

func buildLocalizer(tag string, specs map[string]map[string]string) *Localizer {
	bundle := i18n.NewBundle(language.English)
	bundle.AddMessages(language.English,
		&i18n.Message{ID: "CloudAPIError", Other: "cloud API request failed"},
		&i18n.Message{ID: "CloudAPIErrorWithActions", Other: "API call failed; see actions for details"},
		&i18n.Message{ID: "ConfigWriteFailed", Other: "failed to write config"},
		&i18n.Message{ID: "ConflictingParameters", Other: "parameters conflict"},
		&i18n.Message{ID: "DeprecatedOperation", Other: "call operation is deprecated"},
		&i18n.Message{ID: "DependencyConflict", Other: "resource has dependencies"},
		&i18n.Message{ID: "DependencyViolation", Other: "resource has dependencies"},
		&i18n.Message{ID: "HiddenRetryTimeout", Other: "hidden state grace period timed out"},
		&i18n.Message{ID: "InternalError", Other: "internal error"},
		&i18n.Message{ID: "InvalidConfig", Other: "config is invalid"},
		&i18n.Message{ID: "InvalidCount", Other: "count must be greater than zero"},
		&i18n.Message{ID: "InvalidCredentials", Other: "credentials are invalid"},
		&i18n.Message{ID: "InvalidDryRunAmount", Other: "ECS dry-run only supports a single instance"},
		&i18n.Message{ID: "InvalidFilter", Other: "filter is invalid"},
		&i18n.Message{ID: "InvalidIDs", Other: "IDs must be comma-separated, not JSON arrays"},
		&i18n.Message{ID: "InvalidLimit", Other: "limit must be greater than zero"},
		&i18n.Message{ID: "InvalidPage", Other: "page must be greater than zero"},
		&i18n.Message{ID: "InvalidParameter", Other: "parameter is invalid"},
		&i18n.Message{ID: "InvalidRegion", Other: "region is not supported"},
		&i18n.Message{ID: "InvalidResourceSpec", Other: "resource spec is invalid"},
		&i18n.Message{ID: "InvalidTag", Other: "tag must be key=value"},
		&i18n.Message{ID: "InvalidUserDataFile", Other: "user data file is invalid"},
		&i18n.Message{ID: "InvalidWaiter", Other: "waiter probe is required"},
		&i18n.Message{ID: "LiveOperationUnavailable", Other: "live operation is not implemented"},
		&i18n.Message{ID: "MissingCredentials", Other: "Alibaba Cloud access key is required"},
		&i18n.Message{ID: "MissingOperation", Other: "call operation is required"},
		&i18n.Message{ID: "MissingParameter", Other: "required parameter is missing"},
		&i18n.Message{ID: "MissingRegion", Other: "region is required"},
		&i18n.Message{ID: "MissingRuleID", Other: "rule ID is required"},
		&i18n.Message{ID: "MissingSchema", Other: "schema name is required"},
		&i18n.Message{ID: "MissingStatus", Other: "status is required"},
		&i18n.Message{ID: "MissingTransitionID", Other: "transition response is missing resource ID"},
		&i18n.Message{ID: "NoUpdateFieldsSpecified", Other: "at least one update field is required"},
		&i18n.Message{ID: "NotFound", Other: "resource not found"},
		&i18n.Message{ID: "NotFoundWithResource", Other: "{{.Resource}} not found"},
		&i18n.Message{ID: "ProfileNotFound", Other: "profile is not configured"},
		&i18n.Message{ID: "SuggestionCallGenerateRequestWithSchema", Other: "Run `ecctl call --schema <product> <operation> --generate-request`."},
		&i18n.Message{ID: "SuggestionCallListProducts", Other: "Run `ecctl call --list` to list supported products."},
		&i18n.Message{ID: "SuggestionCallListProductAPIs", Other: "Run `ecctl call --list {{.Product}}` to list supported APIs."},
		&i18n.Message{ID: "SuggestionCallSchemaProductOperation", Other: "Run `ecctl call --schema <product> <operation>`."},
		&i18n.Message{ID: "SuggestionCallUseListForms", Other: "Run `ecctl call --list` or `ecctl call --list <product>`."},
		&i18n.Message{ID: "SuggestionMissingParameter", Other: "Run the command with `--help` to see required parameters."},
		&i18n.Message{ID: "SuggestionMissingRegion", Other: "Pass `--region <region>` or run `ecctl configure set region <region>`."},
		&i18n.Message{ID: "SuggestionUnknownFlag", Other: "Flag {{.Flag}} is not supported. IDs are positional arguments, not flags. Run `ecctl --help` or the command with `--help` to see the correct syntax."},
		&i18n.Message{ID: "SuggestionUnknownCommand", Other: "Run `ecctl --help` to list supported commands."},
		&i18n.Message{ID: "SuggestionUnknownCommandDefaultResource", Other: "Try `ecctl {{.Product}} {{.Action}}` for the default {{.Product}} resource. Run `ecctl --help` to list supported commands."},
		&i18n.Message{ID: "SuggestionUnknownCommandListActions", Other: "Run `ecctl schema --list {{.Product}}` to see supported actions for `{{.Product}}.{{.Resource}}`. Run `ecctl --help` to list supported commands."},
		&i18n.Message{ID: "SuggestionUnknownCommandListResources", Other: "Run `ecctl schema --list {{.Product}}` to see supported resources. Run `ecctl --help` to list supported commands."},
		&i18n.Message{ID: "SuggestionUnknownConfigKey", Other: "Run `ecctl configure list` to list supported config keys."},
		&i18n.Message{ID: "SuggestionUnknownSchema", Other: "Run `ecctl schema --list` to list supported schemas."},
		&i18n.Message{ID: "SuggestionUnsupportedOutputMode", Other: "Use `--output json`, `--output text`, or `--json`."},
		&i18n.Message{ID: "SuggestionUnknownTopic", Other: "Run `ecctl examples` to list available topics."},
		&i18n.Message{ID: "RootLong", Other: "Agent-first Elastic Computing Controller."},
		&i18n.Message{ID: "SchemaAPICallPurpose.Operation", Other: "Perform the resource operation."},
		&i18n.Message{ID: "SchemaAPICallPurpose.Wait", Other: "Poll until the resource reaches the target state."},
		&i18n.Message{ID: "SchemaAPICallPurpose.Readback", Other: "Read the resource view."},
		&i18n.Message{ID: "SchemaAPICallPurpose.CachedReadback", Other: "Return the final resource view."},
		&i18n.Message{ID: "SchemaAPICallCondition.Always", Other: "Every time the command runs."},
		&i18n.Message{ID: "SchemaAPICallCondition.When", Other: "When {{.Condition}}."},
		&i18n.Message{ID: "SchemaAPICallCondition.WhenAfterCode", Other: "When {{.Condition}}."},
		&i18n.Message{ID: "SchemaAPICallCondition.Specified", Other: "`{{.Flag}}` is specified"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotSpecified", Other: "`{{.Flag}}` is not specified"},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitlySpecified", Other: "`{{.Flag}}` is explicitly specified"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotExplicitlySpecified", Other: "`{{.Flag}}` is not explicitly specified"},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitNonEmpty", Other: "When `{{.Flag}}` is explicitly set to a non-empty value."},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitEmpty", Other: "When `{{.Flag}}` is explicitly set to an empty value."},
		&i18n.Message{ID: "SchemaAPICallCondition.Single", Other: "exactly one `{{.Flag}}` value is provided"},
		&i18n.Message{ID: "SchemaAPICallCondition.Multiple", Other: "multiple `{{.Flag}}` values are provided"},
		&i18n.Message{ID: "SchemaAPICallCondition.Equals", Other: "`{{.Flag}}`{{if .Field}} field `{{.Field}}`{{end}} equals `{{.Value}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotEquals", Other: "`{{.Flag}}`{{if .Field}} field `{{.Field}}`{{end}} does not equal `{{.Value}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.StartsWith", Other: "`{{.Flag}}` starts with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotStartsWith", Other: "`{{.Flag}}` does not start with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContextAvailable", Other: "the preceding step produced `{{.Name}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContextUnavailable", Other: "the preceding step did not produce `{{.Name}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsPrefix", Other: "`{{.Flag}}` contains a value prefixed with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotContainsPrefix", Other: "`{{.Flag}}` does not contain a value prefixed with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsMatchingPrefix", Other: "`{{.Flag}}` contains a value starting with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsNonMatchingPrefix", Other: "`{{.Flag}}` contains a value that does not start with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsUnmatchedPrefix", Other: "`{{.Flag}}` contains an unmatched value starting with `{{.Prefix}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ImageLookup", Other: "When `{{.Flag}}` is not empty and does not end with `.vhd`."},
		&i18n.Message{ID: "SchemaAPICallCondition.And", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterCode", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterCodeBeforeCode", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterGroup", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterGroupBeforeCode", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndBeforeCode", Other: "{{.Left}} and {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.Or", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterCode", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterCodeBeforeCode", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterGroup", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterGroupBeforeCode", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrBeforeCode", Other: "{{.Left}} or {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.Group", Other: "({{.Condition}})"},
		&i18n.Message{ID: "RootExample", Other: "  ecctl schema --list\n  ecctl vpc list\n  ecctl vpc create --name prod-vpc --cidr 10.0.0.0/16\n  ecctl ecs instance list --filter status=Running"},
		&i18n.Message{ID: "CommandLong.ecctl.capabilities", Other: "Print a machine-readable overview of CLI capabilities. Use `ecctl schema <product>.<resource>.<action>` to inspect supported flags, filters, and output contracts for a specific command."},
		&i18n.Message{ID: "CommandExample.ecctl.capabilities", Other: "  ecctl capabilities --output json\n  ecctl schema <product>.<resource>.<action>"},
		&i18n.Message{ID: "CommandLong.ecctl.call", Other: "Call Alibaba Cloud OpenAPI operations.\n\nUsage forms:\n  ecctl call --list [--filter <keyword>] [--limit <n>]\n  ecctl call --list <product> [--filter <keyword>] [--limit <n>]\n  ecctl call --schema <product> <operation> [--generate-request]\n  ecctl call <product> <operation> [OpenAPI parameters] [flags]\n\nOpenAPI parameters may be passed as --Parameter value or --Parameter=value.\nUse --request for a JSON object or @file when structured input is clearer."},
		&i18n.Message{ID: "CommandExample.ecctl.call", Other: "  ecctl call --list\n  ecctl call --list --filter ecs\n  ecctl call --list ecs\n  ecctl call --list ecs --filter Instance --limit 20\n  ecctl call --schema ecs DescribeInstances\n  ecctl call --schema ecs DescribeInstances --generate-request\n  ecctl call ecs DescribeInstances --region cn-hangzhou --request '{\"PageSize\":10}'\n  ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10\n  ecctl call cs InstallClusterAddons --request @install-addons.json"},
		&i18n.Message{ID: "CommandExample.ecctl.configure", Other: "  ecctl configure get\n  ecctl configure set region cn-hangzhou\n  ecctl configure list\n  ecctl configure use production"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.get", Other: "  ecctl configure get\n  ecctl configure get region\n  ecctl configure get access-key-secret --show-secret"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.set", Other: "  ecctl configure set region cn-hangzhou\n  ecctl configure set access-key-id <value>\n  ecctl configure set lang zh-CN\n  ecctl configure set output text"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.list", Other: "  ecctl configure list\n  ecctl configure list --show-secret"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.use", Other: "  ecctl configure use default\n  ecctl configure use production\n  ecctl --profile prod configure get"},
		&i18n.Message{ID: "CommandExample.ecctl.examples", Other: "  ecctl examples\n  ecctl examples ecs\n  ecctl examples ecs.instance\n  ecctl examples ecs.instance.create\n  ecctl examples --all"},
		&i18n.Message{ID: "CommandLong.ecctl.configure.set", Other: "Supported settings: {{.Keys}}"},
		&i18n.Message{ID: "CommandLong.ecctl.examples", Other: "Print example invocations for ecctl topics.\n\nTopic format:\n  <product>                            list product-level examples\n  <product>.<resource>                 list resource-level examples\n  <product>.<resource>.<action>        list action-level examples\n\nWith no topic, only product-level topics are listed and a hint shows how to drill\ndown. Pass --all to enumerate every topic (intended for completion/indexing\ntooling). Agents should drill down by topic instead of dumping the full catalog."},
		&i18n.Message{ID: "ExamplesDrillDownHint", Other: "Run `ecctl examples <topic>` to view example invocations. Topic format: <product> | <product>.<resource> | <product>.<resource>.<action>. Pass --all to list every available topic."},
		&i18n.Message{ID: "UnknownTopic", Other: "topic has no examples"},
		&i18n.Message{ID: "HelpUsage", Other: "Usage"},
		&i18n.Message{ID: "HelpExamples", Other: "Examples"},
		&i18n.Message{ID: "HelpAvailableCommands", Other: "Available Commands"},
		&i18n.Message{ID: "HelpOtherCommands", Other: "Other Commands"},
		&i18n.Message{ID: "HelpResourceFlags", Other: "Resource Flags"},
		&i18n.Message{ID: "HelpCommandFlags", Other: "Flags"},
		&i18n.Message{ID: "HelpGlobalFlags", Other: "Global Flags"},
		&i18n.Message{ID: "HelpFilterableFields", Other: "Filterable Fields"},
		&i18n.Message{ID: "HelpOtherHelpTopics", Other: "Additional help topics"},
		&i18n.Message{ID: "HelpUseCommandHelp", Other: "Use \"{{.CommandPath}} [command] --help\" for more information about a command."},
		&i18n.Message{ID: "HelpSchemaHint", Other: "Schemas:\n  ecctl schema {{.Action}}\n  ecctl schema {{.Action}} --full"},
		&i18n.Message{ID: "HelpFlagHelp", Other: "help for this command"},
		&i18n.Message{ID: "HelpRequiredLegend", Other: " (* required)"},
		&i18n.Message{ID: "HelpDefaultQuoted", Other: "(default \"{{.Value}}\")"},
		&i18n.Message{ID: "HelpDefaultValue", Other: "(default {{.Value}})"},
		&i18n.Message{ID: "HelpMaxValue", Other: " (max {{.Value}})"},
		&i18n.Message{ID: "InputStyleInlineObject", Other: "inline key=value, JSON object, or @file"},
		&i18n.Message{ID: "InputStyleObject", Other: "JSON object or @file"},
		&i18n.Message{ID: "InputStyleSeparator", Other: " or "},
		&i18n.Message{ID: "InputStyleSignedValue", Other: "+value assigns, -value unassigns"},
		&i18n.Message{ID: "CommandGroup.cloud_products", Other: "Cloud Product Commands:"},
		&i18n.Message{ID: "CommandGroup.tools", Other: "Auxiliary Commands:"},
		&i18n.Message{ID: "CommandGroup.resource_operations", Other: "Resource Operations:"},
		&i18n.Message{ID: "CommandGroup.resource_types", Other: "Resource Types:"},
		&i18n.Message{ID: "CommandShort.completion", Other: "Generate the autocompletion script for the specified shell"},
		&i18n.Message{ID: "CommandShort.help", Other: "Help about any command"},
		&i18n.Message{ID: "CommandShort.ecctl", Other: "Agent-first Elastic Computing Controller"},
		&i18n.Message{ID: "CommandShort.ecctl.completion", Other: "Generate the autocompletion script for the specified shell"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.bash", Other: "Generate the autocompletion script for bash"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.fish", Other: "Generate the autocompletion script for fish"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.powershell", Other: "Generate the autocompletion script for powershell"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.zsh", Other: "Generate the autocompletion script for zsh"},
		&i18n.Message{ID: "CommandShort.ecctl.call", Other: "Call Alibaba Cloud OpenAPI operations"},
		&i18n.Message{ID: "CommandShort.ecctl.capabilities", Other: "Describe machine-readable CLI capabilities"},
		&i18n.Message{ID: "CommandShort.ecctl.configure", Other: "Manage ecctl configuration"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.get", Other: "Print active config"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.list", Other: "List supported config keys"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.set", Other: "Set config values"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.use", Other: "Switch active profile"},
		&i18n.Message{ID: "CommandShort.ecctl.examples", Other: "List CLI invocation examples for products, resources, and actions"},
		&i18n.Message{ID: "CommandShort.ecctl.help", Other: "Help about any command"},
		&i18n.Message{ID: "CommandShort.ecctl.schema", Other: "Inspect command schemas"},
		&i18n.Message{ID: "FlagUsage.AlibabaCloudRegion", Other: "Alibaba Cloud region"},
		&i18n.Message{ID: "FlagUsage.ConfigurationProfile", Other: "configuration profile"},
		&i18n.Message{ID: "FlagUsage.DefaultRegion", Other: "default region"},
		&i18n.Message{ID: "FlagUsage.DisableCompletionDescriptions", Other: "disable completion descriptions"},
		&i18n.Message{ID: "FlagUsage.DisableColor", Other: "disable color in human output"},
		&i18n.Message{ID: "FlagUsage.ForceJSON", Other: "force JSON output"},
		&i18n.Message{ID: "FlagUsage.AgentEnvelope", Other: "wrap JSON output in the Agent envelope"},
		&i18n.Message{ID: "FlagUsage.FilterProductsOrAPIs", Other: "filter products or APIs by keyword"},
		&i18n.Message{ID: "FlagUsage.GenerateOpenAPIRequest", Other: "generate a request JSON template from --schema"},
		&i18n.Message{ID: "FlagUsage.InspectOpenAPISchema", Other: "inspect an OpenAPI operation schema"},
		&i18n.Message{ID: "FlagUsage.ListSupported", Other: "list supported products or resources"},
		&i18n.Message{ID: "FlagUsage.ListSchemasForProduct", Other: "list schemas for a product"},
		&i18n.Message{ID: "FlagUsage.SchemaBrief", Other: "show required and common parameters only"},
		&i18n.Message{ID: "FlagUsage.SchemaFull", Other: "show all schema-visible parameters"},
		&i18n.Message{ID: "FlagUsage.ListSupportedProductsOrAPIs", Other: "list supported products or APIs"},
		&i18n.Message{ID: "FlagUsage.LimitProductsOrAPIs", Other: "maximum products or APIs to return"},
		&i18n.Message{ID: "FlagUsage.ListAllExamples", Other: "list every product/resource/action topic (default: products only)"},
		&i18n.Message{ID: "FlagUsage.DisplayLanguage", Other: "display language (supported: en, zh-CN)"},
		&i18n.Message{ID: "FlagUsage.OpenAPIRequest", Other: "request JSON object or @file containing a JSON object"},
		&i18n.Message{ID: "FlagUsage.OutputMode", Other: "output mode (json, text)"},
		&i18n.Message{ID: "FlagUsage.OpenAPIParameter", Other: "OpenAPI parameter {{.Name}}"},
		&i18n.Message{ID: "FlagUsage.ResourceFields", Other: "comma-separated resource fields to include"},
		&i18n.Message{ID: "FlagUsage.ShowSensitive", Other: "show sensitive config values"},
		&i18n.Message{ID: "FlagUsage.SkipRegionVerification", Other: "skip online verification of the region against the Alibaba Cloud Location service"},
		&i18n.Message{ID: "FlagUsage.Version", Other: "version for ecctl"},
		&i18n.Message{ID: "InvalidFields", Other: "fields selection is invalid"},
		&i18n.Message{ID: "UnknownAction", Other: "action is not supported"},
		&i18n.Message{ID: "UnknownCommand", Other: "command is not supported"},
		&i18n.Message{ID: "UnknownConfigKey", Other: "config key is not supported"},
		&i18n.Message{ID: "UnknownOperation", Other: "operation is not supported"},
		&i18n.Message{ID: "UnknownProbe", Other: "probe is not configured"},
		&i18n.Message{ID: "UnknownProduct", Other: "product is not supported"},
		&i18n.Message{ID: "UnknownSchema", Other: "schema is not supported"},
		&i18n.Message{ID: "UnknownTransition", Other: "transition is not configured"},
		&i18n.Message{ID: "UnknownWaiter", Other: "waiter is not configured"},
		&i18n.Message{ID: "SuggestionInvalidDryRunAmount", Other: "Rerun with --amount 1 and --min-amount 1 to validate this instance configuration."},
		&i18n.Message{ID: "UnsupportedAction", Other: "action is not supported"},
		&i18n.Message{ID: "UnsupportedEmit", Other: "emit mapping is not supported"},
		&i18n.Message{ID: "UnsupportedOperation", Other: "operation is not supported"},
		&i18n.Message{ID: "UnsupportedOutputMode", Other: "output mode is not supported"},
		&i18n.Message{ID: "UnsupportedProduct", Other: "product is not supported"},
		&i18n.Message{ID: "UnsupportedRuleSelector", Other: "rule selector is not supported"},
		&i18n.Message{ID: "WaitTimeout", Other: "wait timed out"},
	)
	bundle.AddMessages(language.MustParse("zh-Hans"),
		&i18n.Message{ID: "CloudAPIError", Other: "云 API 请求失败"},
		&i18n.Message{ID: "CloudAPIErrorWithActions", Other: "调用 API 报错，请查看 actions 中的具体报错"},
		&i18n.Message{ID: "ConfigWriteFailed", Other: "配置写入失败"},
		&i18n.Message{ID: "ConflictingParameters", Other: "参数冲突"},
		&i18n.Message{ID: "DeprecatedOperation", Other: "call 操作已废弃"},
		&i18n.Message{ID: "DependencyConflict", Other: "资源存在依赖"},
		&i18n.Message{ID: "DependencyViolation", Other: "资源存在依赖"},
		&i18n.Message{ID: "HiddenRetryTimeout", Other: "隐藏状态宽限期超时"},
		&i18n.Message{ID: "InternalError", Other: "内部错误"},
		&i18n.Message{ID: "InvalidConfig", Other: "配置无效"},
		&i18n.Message{ID: "InvalidCount", Other: "数量必须大于 0"},
		&i18n.Message{ID: "InvalidCredentials", Other: "凭证无效"},
		&i18n.Message{ID: "InvalidDryRunAmount", Other: "ECS dry-run 仅支持单实例预检"},
		&i18n.Message{ID: "InvalidFilter", Other: "过滤条件无效"},
		&i18n.Message{ID: "InvalidIDs", Other: "ID 必须是逗号分隔列表，不能是 JSON 数组"},
		&i18n.Message{ID: "InvalidLimit", Other: "limit 必须大于 0"},
		&i18n.Message{ID: "InvalidPage", Other: "page 必须大于 0"},
		&i18n.Message{ID: "InvalidParameter", Other: "参数无效"},
		&i18n.Message{ID: "InvalidRegion", Other: "地域不受支持"},
		&i18n.Message{ID: "InvalidResourceSpec", Other: "资源规格无效"},
		&i18n.Message{ID: "InvalidTag", Other: "标签必须是 key=value"},
		&i18n.Message{ID: "InvalidUserDataFile", Other: "user data 文件无效"},
		&i18n.Message{ID: "InvalidWaiter", Other: "必须提供 waiter probe"},
		&i18n.Message{ID: "LiveOperationUnavailable", Other: "暂未实现真实云资源操作"},
		&i18n.Message{ID: "MissingCredentials", Other: "必须提供阿里云 AccessKey"},
		&i18n.Message{ID: "MissingOperation", Other: "必须提供 call 操作"},
		&i18n.Message{ID: "MissingParameter", Other: "缺少必填参数"},
		&i18n.Message{ID: "MissingRegion", Other: "必须提供地域"},
		&i18n.Message{ID: "MissingRuleID", Other: "必须提供规则 ID"},
		&i18n.Message{ID: "MissingSchema", Other: "必须提供 schema 名称"},
		&i18n.Message{ID: "MissingStatus", Other: "必须提供目标状态"},
		&i18n.Message{ID: "MissingTransitionID", Other: "转换响应缺少资源 ID"},
		&i18n.Message{ID: "NoUpdateFieldsSpecified", Other: "至少需要指定一个更新字段"},
		&i18n.Message{ID: "NotFound", Other: "资源不存在"},
		&i18n.Message{ID: "NotFoundWithResource", Other: "{{.Resource}} 资源不存在"},
		&i18n.Message{ID: "ProfileNotFound", Other: "配置档案未配置"},
		&i18n.Message{ID: "SuggestionCallGenerateRequestWithSchema", Other: "执行 `ecctl call --schema <product> <operation> --generate-request`。"},
		&i18n.Message{ID: "SuggestionCallListProducts", Other: "执行 `ecctl call --list` 查看支持的产品。"},
		&i18n.Message{ID: "SuggestionCallListProductAPIs", Other: "执行 `ecctl call --list {{.Product}}` 查看支持的 API。"},
		&i18n.Message{ID: "SuggestionCallSchemaProductOperation", Other: "执行 `ecctl call --schema <product> <operation>`。"},
		&i18n.Message{ID: "SuggestionCallUseListForms", Other: "执行 `ecctl call --list` 或 `ecctl call --list <product>`。"},
		&i18n.Message{ID: "SuggestionMissingParameter", Other: "使用 `--help` 查看该命令的必填参数。"},
		&i18n.Message{ID: "SuggestionMissingRegion", Other: "传入 `--region <region>`，或执行 `ecctl configure set region <region>`。"},
		&i18n.Message{ID: "SuggestionUnknownFlag", Other: "不支持 {{.Flag}} 参数。ID 是位置参数而非 flag，请执行 `ecctl --help` 或该命令的 `--help` 查看正确语法。"},
		&i18n.Message{ID: "SuggestionUnknownCommand", Other: "执行 `ecctl --help` 查看支持的命令。"},
		&i18n.Message{ID: "SuggestionUnknownCommandDefaultResource", Other: "执行 `ecctl {{.Product}} {{.Action}}` 操作默认 {{.Product}} 资源。执行 `ecctl --help` 查看支持的命令。"},
		&i18n.Message{ID: "SuggestionUnknownCommandListActions", Other: "执行 `ecctl schema --list {{.Product}}` 查看 `{{.Product}}.{{.Resource}}` 支持的操作。执行 `ecctl --help` 查看支持的命令。"},
		&i18n.Message{ID: "SuggestionUnknownCommandListResources", Other: "执行 `ecctl schema --list {{.Product}}` 查看支持的资源。执行 `ecctl --help` 查看支持的命令。"},
		&i18n.Message{ID: "SuggestionUnknownConfigKey", Other: "执行 `ecctl configure list` 查看支持的配置项。"},
		&i18n.Message{ID: "SuggestionUnknownSchema", Other: "执行 `ecctl schema --list` 查看支持的 schema。"},
		&i18n.Message{ID: "SuggestionUnsupportedOutputMode", Other: "使用 `--output json`、`--output text` 或 `--json`。"},
		&i18n.Message{ID: "SuggestionUnknownTopic", Other: "执行 `ecctl examples` 查看可用主题。"},
		&i18n.Message{ID: "RootLong", Other: "Agent 优先的弹性计算控制器。"},
		&i18n.Message{ID: "SchemaAPICallPurpose.Operation", Other: "执行资源操作。"},
		&i18n.Message{ID: "SchemaAPICallPurpose.Wait", Other: "轮询等待资源达到目标状态。"},
		&i18n.Message{ID: "SchemaAPICallPurpose.Readback", Other: "读取资源视图。"},
		&i18n.Message{ID: "SchemaAPICallPurpose.CachedReadback", Other: "返回最终资源视图。"},
		&i18n.Message{ID: "SchemaAPICallCondition.Always", Other: "每次执行命令时"},
		&i18n.Message{ID: "SchemaAPICallCondition.When", Other: "{{.Condition}}时"},
		&i18n.Message{ID: "SchemaAPICallCondition.WhenAfterCode", Other: "{{.Condition}} 时"},
		&i18n.Message{ID: "SchemaAPICallCondition.Specified", Other: "指定 `{{.Flag}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotSpecified", Other: "未指定 `{{.Flag}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitlySpecified", Other: "显式指定 `{{.Flag}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotExplicitlySpecified", Other: "未显式指定 `{{.Flag}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitNonEmpty", Other: "显式将 `{{.Flag}}` 设置为非空值时"},
		&i18n.Message{ID: "SchemaAPICallCondition.ExplicitEmpty", Other: "显式将 `{{.Flag}}` 设置为空时"},
		&i18n.Message{ID: "SchemaAPICallCondition.Single", Other: "只提供一个 `{{.Flag}}` 值"},
		&i18n.Message{ID: "SchemaAPICallCondition.Multiple", Other: "提供多个 `{{.Flag}}` 值"},
		&i18n.Message{ID: "SchemaAPICallCondition.Equals", Other: "`{{.Flag}}`{{if .Field}} 的 `{{.Field}}` 字段{{else}} {{end}}等于 `{{.Value}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotEquals", Other: "`{{.Flag}}`{{if .Field}} 的 `{{.Field}}` 字段{{else}} {{end}}不等于 `{{.Value}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.StartsWith", Other: "`{{.Flag}}` 以 `{{.Prefix}}` 开头"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotStartsWith", Other: "`{{.Flag}}` 不以 `{{.Prefix}}` 开头"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContextAvailable", Other: "前序步骤已生成 `{{.Name}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContextUnavailable", Other: "前序步骤未生成 `{{.Name}}`"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsPrefix", Other: "`{{.Flag}}` 中包含以 `{{.Prefix}}` 为前缀的值"},
		&i18n.Message{ID: "SchemaAPICallCondition.NotContainsPrefix", Other: "`{{.Flag}}` 中不包含以 `{{.Prefix}}` 为前缀的值"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsMatchingPrefix", Other: "`{{.Flag}}` 中包含以 `{{.Prefix}}` 开头的值"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsNonMatchingPrefix", Other: "`{{.Flag}}` 中包含不以 `{{.Prefix}}` 开头的值"},
		&i18n.Message{ID: "SchemaAPICallCondition.ContainsUnmatchedPrefix", Other: "`{{.Flag}}` 中包含以 `{{.Prefix}}` 开头且尚未匹配的值"},
		&i18n.Message{ID: "SchemaAPICallCondition.ImageLookup", Other: "当 `{{.Flag}}` 非空且不以 `.vhd` 结尾时"},
		&i18n.Message{ID: "SchemaAPICallCondition.And", Other: "{{.Left}}且{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterCode", Other: "{{.Left}} 且{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterCodeBeforeCode", Other: "{{.Left}} 且 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterGroup", Other: "{{.Left}}且{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndAfterGroupBeforeCode", Other: "{{.Left}}且 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.AndBeforeCode", Other: "{{.Left}}且 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.Or", Other: "{{.Left}}或{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterCode", Other: "{{.Left}} 或{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterCodeBeforeCode", Other: "{{.Left}} 或 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterGroup", Other: "{{.Left}}或{{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrAfterGroupBeforeCode", Other: "{{.Left}}或 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.OrBeforeCode", Other: "{{.Left}}或 {{.Right}}"},
		&i18n.Message{ID: "SchemaAPICallCondition.Group", Other: "（{{.Condition}}）"},
		&i18n.Message{ID: "RootExample", Other: "  ecctl schema --list\n  ecctl vpc list\n  ecctl vpc create --name prod-vpc --cidr 10.0.0.0/16\n  ecctl ecs instance list --filter status=Running"},
		&i18n.Message{ID: "CommandLong.ecctl.capabilities", Other: "输出 CLI 的机器可读能力概览。查看具体命令支持的参数、过滤器和输出契约，请使用 `ecctl schema <product>.<resource>.<action>`。"},
		&i18n.Message{ID: "CommandExample.ecctl.capabilities", Other: "  ecctl capabilities --output json\n  ecctl schema <product>.<resource>.<action>"},
		&i18n.Message{ID: "CommandLong.ecctl.call", Other: "调用阿里云 OpenAPI 操作。\n\n调用形态:\n  ecctl call --list [--filter <keyword>] [--limit <n>]\n  ecctl call --list <product> [--filter <keyword>] [--limit <n>]\n  ecctl call --schema <product> <operation> [--generate-request]\n  ecctl call <product> <operation> [OpenAPI 参数] [flags]\n\nOpenAPI 参数可用 --Parameter value 或 --Parameter=value 传入。\n结构化输入更清晰时可继续使用 --request JSON 对象或 @file。"},
		&i18n.Message{ID: "CommandExample.ecctl.call", Other: "  ecctl call --list\n  ecctl call --list --filter ecs\n  ecctl call --list ecs\n  ecctl call --list ecs --filter Instance --limit 20\n  ecctl call --schema ecs DescribeInstances\n  ecctl call --schema ecs DescribeInstances --generate-request\n  ecctl call ecs DescribeInstances --region cn-hangzhou --request '{\"PageSize\":10}'\n  ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10\n  ecctl call cs InstallClusterAddons --request @install-addons.json"},
		&i18n.Message{ID: "CommandExample.ecctl.configure", Other: "  ecctl configure get\n  ecctl configure set region cn-hangzhou\n  ecctl configure list\n  ecctl configure use production"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.get", Other: "  ecctl configure get\n  ecctl configure get region\n  ecctl configure get access-key-secret --show-secret"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.set", Other: "  ecctl configure set region cn-hangzhou\n  ecctl configure set access-key-id <value>\n  ecctl configure set lang zh-CN\n  ecctl configure set output text"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.list", Other: "  ecctl configure list\n  ecctl configure list --show-secret"},
		&i18n.Message{ID: "CommandExample.ecctl.configure.use", Other: "  ecctl configure use default\n  ecctl configure use production\n  ecctl --profile prod configure get"},
		&i18n.Message{ID: "CommandExample.ecctl.examples", Other: "  ecctl examples\n  ecctl examples ecs\n  ecctl examples ecs.instance\n  ecctl examples ecs.instance.create\n  ecctl examples --all"},
		&i18n.Message{ID: "CommandLong.ecctl.configure.set", Other: "支持的设置项: {{.Keys}}"},
		&i18n.Message{ID: "CommandLong.ecctl.examples", Other: "打印 ecctl 主题的调用示例。\n\n主题格式：\n  <product>                            列出产品级示例\n  <product>.<resource>                 列出资源级示例\n  <product>.<resource>.<action>        列出动作级示例\n\n不传主题时，仅列出产品级主题并提示如何下钻；传 --all 列出全部主题（供补全/索引工具使用）。\nAgent 建议按主题逐层下钻，避免一次拉全量。"},
		&i18n.Message{ID: "ExamplesDrillDownHint", Other: "执行 `ecctl examples <topic>` 查看具体的调用示例。主题格式：<product> | <product>.<resource> | <product>.<resource>.<action>。使用 --all 列出全部主题。"},
		&i18n.Message{ID: "UnknownTopic", Other: "主题不存在或没有示例"},
		&i18n.Message{ID: "HelpUsage", Other: "用法"},
		&i18n.Message{ID: "HelpExamples", Other: "示例"},
		&i18n.Message{ID: "HelpAvailableCommands", Other: "可用命令"},
		&i18n.Message{ID: "HelpOtherCommands", Other: "其他命令"},
		&i18n.Message{ID: "HelpResourceFlags", Other: "资源参数"},
		&i18n.Message{ID: "HelpCommandFlags", Other: "参数"},
		&i18n.Message{ID: "HelpGlobalFlags", Other: "全局参数"},
		&i18n.Message{ID: "HelpFilterableFields", Other: "可过滤字段"},
		&i18n.Message{ID: "HelpOtherHelpTopics", Other: "其他帮助主题"},
		&i18n.Message{ID: "HelpUseCommandHelp", Other: "使用 \"{{.CommandPath}} [command] --help\" 查看命令的更多信息。"},
		&i18n.Message{ID: "HelpSchemaHint", Other: "Schema:\n  ecctl schema {{.Action}}\n  ecctl schema {{.Action}} --full"},
		&i18n.Message{ID: "HelpFlagHelp", Other: "显示此命令的帮助"},
		&i18n.Message{ID: "HelpRequiredLegend", Other: "（* 必填）"},
		&i18n.Message{ID: "HelpDefaultQuoted", Other: "(默认 \"{{.Value}}\")"},
		&i18n.Message{ID: "HelpDefaultValue", Other: "(默认 {{.Value}})"},
		&i18n.Message{ID: "HelpMaxValue", Other: "（最大值：{{.Value}}）"},
		&i18n.Message{ID: "InputStyleInlineObject", Other: "内联 key=value、JSON 对象或 @file"},
		&i18n.Message{ID: "InputStyleObject", Other: "JSON 对象或 @file"},
		&i18n.Message{ID: "InputStyleSeparator", Other: " 或 "},
		&i18n.Message{ID: "InputStyleSignedValue", Other: "+值表示分配，-值表示回收"},
		&i18n.Message{ID: "CommandGroup.cloud_products", Other: "云产品命令:"},
		&i18n.Message{ID: "CommandGroup.tools", Other: "辅助命令:"},
		&i18n.Message{ID: "CommandGroup.resource_operations", Other: "资源操作:"},
		&i18n.Message{ID: "CommandGroup.resource_types", Other: "资源类型:"},
		&i18n.Message{ID: "CommandShort.completion", Other: "生成指定 shell 的自动补全脚本"},
		&i18n.Message{ID: "CommandShort.help", Other: "显示命令帮助"},
		&i18n.Message{ID: "CommandShort.ecctl", Other: "Agent 优先的弹性计算控制器"},
		&i18n.Message{ID: "CommandShort.ecctl.completion", Other: "生成指定 shell 的自动补全脚本"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.bash", Other: "生成 bash 自动补全脚本"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.fish", Other: "生成 fish 自动补全脚本"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.powershell", Other: "生成 powershell 自动补全脚本"},
		&i18n.Message{ID: "CommandShort.ecctl.completion.zsh", Other: "生成 zsh 自动补全脚本"},
		&i18n.Message{ID: "CommandShort.ecctl.call", Other: "调用阿里云 OpenAPI 操作"},
		&i18n.Message{ID: "CommandShort.ecctl.capabilities", Other: "描述机器可读的 CLI 能力"},
		&i18n.Message{ID: "CommandShort.ecctl.configure", Other: "管理 ecctl 配置"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.get", Other: "打印当前配置"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.list", Other: "列出支持的配置项"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.set", Other: "设置配置值"},
		&i18n.Message{ID: "CommandShort.ecctl.configure.use", Other: "切换当前配置档案"},
		&i18n.Message{ID: "CommandShort.ecctl.examples", Other: "列举产品/资源/动作的 CLI 调用示例"},
		&i18n.Message{ID: "CommandShort.ecctl.help", Other: "显示命令帮助"},
		&i18n.Message{ID: "CommandShort.ecctl.schema", Other: "查看产品命令 Schema"},
		&i18n.Message{ID: "FlagUsage.AlibabaCloudRegion", Other: "阿里云地域"},
		&i18n.Message{ID: "FlagUsage.ConfigurationProfile", Other: "配置档案"},
		&i18n.Message{ID: "FlagUsage.DefaultRegion", Other: "默认地域"},
		&i18n.Message{ID: "FlagUsage.DisableCompletionDescriptions", Other: "禁用补全描述"},
		&i18n.Message{ID: "FlagUsage.DisableColor", Other: "禁用人类可读输出中的颜色"},
		&i18n.Message{ID: "FlagUsage.ForceJSON", Other: "强制 JSON 输出"},
		&i18n.Message{ID: "FlagUsage.AgentEnvelope", Other: "使用 Agent envelope 包装 JSON 输出"},
		&i18n.Message{ID: "FlagUsage.FilterProductsOrAPIs", Other: "按关键词过滤产品或 API"},
		&i18n.Message{ID: "FlagUsage.GenerateOpenAPIRequest", Other: "根据 --schema 生成请求 JSON 模板"},
		&i18n.Message{ID: "FlagUsage.InspectOpenAPISchema", Other: "查看 OpenAPI 接口 schema"},
		&i18n.Message{ID: "FlagUsage.ListSupported", Other: "列出支持的产品或资源"},
		&i18n.Message{ID: "FlagUsage.ListSchemasForProduct", Other: "列出指定产品的 schema"},
		&i18n.Message{ID: "FlagUsage.SchemaBrief", Other: "仅显示必填和常用参数"},
		&i18n.Message{ID: "FlagUsage.SchemaFull", Other: "显示所有 schema 可见参数"},
		&i18n.Message{ID: "FlagUsage.ListSupportedProductsOrAPIs", Other: "列出支持的产品或 API"},
		&i18n.Message{ID: "FlagUsage.LimitProductsOrAPIs", Other: "最多返回的产品或 API 数量"},
		&i18n.Message{ID: "FlagUsage.ListAllExamples", Other: "列出全部产品/资源/动作主题（默认仅列出产品）"},
		&i18n.Message{ID: "FlagUsage.DisplayLanguage", Other: "显示语言（支持：en, zh-CN）"},
		&i18n.Message{ID: "FlagUsage.OpenAPIRequest", Other: "请求 JSON 对象，或包含 JSON 对象的 @file"},
		&i18n.Message{ID: "FlagUsage.OutputMode", Other: "输出模式（json, text）"},
		&i18n.Message{ID: "FlagUsage.OpenAPIParameter", Other: "OpenAPI 参数 {{.Name}}"},
		&i18n.Message{ID: "FlagUsage.ResourceFields", Other: "要包含的资源字段，使用逗号分隔"},
		&i18n.Message{ID: "FlagUsage.ShowSensitive", Other: "显示敏感配置值"},
		&i18n.Message{ID: "FlagUsage.SkipRegionVerification", Other: "跳过通过阿里云 Location 服务对地域的在线校验"},
		&i18n.Message{ID: "FlagUsage.Version", Other: "显示 ecctl 版本"},
		&i18n.Message{ID: "InvalidFields", Other: "字段裁剪参数无效"},
		&i18n.Message{ID: "UnknownAction", Other: "动作不受支持"},
		&i18n.Message{ID: "UnknownCommand", Other: "命令不受支持"},
		&i18n.Message{ID: "UnknownConfigKey", Other: "配置项不受支持"},
		&i18n.Message{ID: "UnknownOperation", Other: "操作不受支持"},
		&i18n.Message{ID: "UnknownProbe", Other: "probe 未配置"},
		&i18n.Message{ID: "UnknownProduct", Other: "产品不受支持"},
		&i18n.Message{ID: "UnknownSchema", Other: "schema 不受支持"},
		&i18n.Message{ID: "UnknownTransition", Other: "transition 未配置"},
		&i18n.Message{ID: "UnknownWaiter", Other: "waiter 未配置"},
		&i18n.Message{ID: "SuggestionInvalidDryRunAmount", Other: "请改用 --amount 1 和 --min-amount 1 验证该实例配置。"},
		&i18n.Message{ID: "UnsupportedAction", Other: "动作不受支持"},
		&i18n.Message{ID: "UnsupportedEmit", Other: "emit 映射不受支持"},
		&i18n.Message{ID: "UnsupportedOperation", Other: "操作不受支持"},
		&i18n.Message{ID: "UnsupportedOutputMode", Other: "输出模式不受支持"},
		&i18n.Message{ID: "UnsupportedProduct", Other: "产品不受支持"},
		&i18n.Message{ID: "UnsupportedRuleSelector", Other: "规则选择器不受支持"},
		&i18n.Message{ID: "WaitTimeout", Other: "等待超时"},
	)
	addRegisteredMessages(bundle, specs)
	return &Localizer{localizer: i18n.NewLocalizer(bundle, tag, "en"), language: tag}
}

func addRegisteredMessages(bundle *i18n.Bundle, specs map[string]map[string]string) {
	for id, text := range specs {
		for tag, value := range text {
			if value == "" {
				continue
			}
			addRegisteredMessage(bundle, tag, id, value)
			if resolved := supportedLanguage(tag); resolved != "" && resolved != tag {
				addRegisteredMessage(bundle, resolved, id, value)
			}
		}
	}
}

func addRegisteredMessage(bundle *i18n.Bundle, tag string, id string, value string) {
	parsed, err := language.Parse(strings.ReplaceAll(tag, "_", "-"))
	if err != nil {
		return
	}
	bundle.AddMessages(parsed, &i18n.Message{ID: id, Other: value})
}

func copyRegisteredMessages(source map[string]map[string]string) map[string]map[string]string {
	copied := make(map[string]map[string]string, len(source))
	for id, text := range source {
		copied[id] = copyText(text)
	}
	return copied
}

func copyText(source map[string]string) map[string]string {
	copied := make(map[string]string, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func ResolveLanguage(explicit string, getenv func(string) string) string {
	if lang := supportedLanguage(explicit); lang != "" {
		return lang
	}
	if explicit != "" {
		return "en"
	}
	if getenv != nil {
		for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			value := getenv(name)
			if value == "" {
				continue
			}
			if lang := supportedLanguage(value); lang != "" {
				return lang
			}
			return "en"
		}
	}
	return "en"
}

func supportedLanguage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexAny(value, ".@"); idx >= 0 {
		value = value[:idx]
	}
	value = strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	switch {
	case value == "c" || value == "posix":
		return "en"
	case value == "en" || strings.HasPrefix(value, "en-"):
		return "en"
	case value == "zh" || strings.HasPrefix(value, "zh-"):
		return "zh-Hans"
	default:
		return ""
	}
}

func (l *Localizer) Message(id string) string {
	return l.MessageData(id, nil)
}

func (l *Localizer) MessageOrDefault(id string, fallback string) string {
	message := l.Message(id)
	if message == "" || message == id {
		return fallback
	}
	return message
}

func (l *Localizer) CommandShort(path string, fallback string) string {
	return l.MessageOrDefault("CommandShort."+messageKey(path), fallback)
}

func (l *Localizer) CommandExample(path string, fallback string) string {
	return l.MessageOrDefault("CommandExample."+messageKey(path), fallback)
}

func (l *Localizer) CommandLongData(path string, fallback string, data map[string]any) string {
	id := "CommandLong." + messageKey(path)
	message := l.MessageData(id, data)
	if message == "" || message == id {
		return fallback
	}
	return message
}

func (l *Localizer) CommandGroupTitle(id string, fallback string) string {
	return l.MessageOrDefault("CommandGroup."+messageKey(id), fallback)
}

func (l *Localizer) FlagUsage(usage string) string {
	id := flagUsageMessages[usage]
	if id == "" {
		return usage
	}
	return l.MessageOrDefault(id, usage)
}

func (l *Localizer) ShouldLocalizeHelp() bool {
	return l.language != "en"
}

func (l *Localizer) ErrorPayload(payload ecerrors.ErrorPayload, hasActions bool) ecerrors.ErrorPayload {
	if payload.Suggestion == "" {
		payload.Suggestion = l.ErrorSuggestion(payload.Code)
	}
	if payload.SuggestedAction == "" {
		payload.SuggestedAction = payload.Suggestion
	}
	if payload.Code == "CloudAPIError" && hasActions {
		payload.Message = l.Message("CloudAPIErrorWithActions")
		return payload
	}
	if payload.Code == "CloudAPIError" {
		return payload
	}
	if !l.ShouldLocalizeHelp() {
		return payload
	}
	if payload.Code == "NotFound" {
		payload.Message = l.NotFoundMessage(payload.Message)
		return payload
	}
	message := l.Message(payload.Code)
	if message == payload.Code {
		return payload
	}
	if payload.Code == "MissingParameter" {
		if names := missingParameterNames(payload.Message); len(names) > 0 {
			message += ": " + strings.Join(names, ", ")
		}
	}
	payload.Message = message
	return payload
}

func (l *Localizer) ErrorSuggestion(code string) string {
	messageID := map[string]string{
		"InvalidDryRunAmount":   "SuggestionInvalidDryRunAmount",
		"MissingParameter":      "SuggestionMissingParameter",
		"MissingRegion":         "SuggestionMissingRegion",
		"UnknownCommand":        "SuggestionUnknownCommand",
		"UnknownConfigKey":      "SuggestionUnknownConfigKey",
		"UnknownSchema":         "SuggestionUnknownSchema",
		"UnknownTopic":          "SuggestionUnknownTopic",
		"UnsupportedOutputMode": "SuggestionUnsupportedOutputMode",
	}[code]
	if messageID == "" {
		return ""
	}
	message := l.Message(messageID)
	if message == messageID {
		return ""
	}
	return message
}

func (l *Localizer) MessageData(id string, data map[string]any) string {
	message, err := l.localizer.Localize(&i18n.LocalizeConfig{MessageID: id})
	if data != nil {
		message, err = l.localizer.Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: data})
	}
	if err != nil {
		return id
	}
	return message
}

func (l *Localizer) NotFoundMessage(message string) string {
	resource, ok := strings.CutSuffix(message, " not found")
	if ok && resource != "" && resource != "resource" {
		return l.MessageData("NotFoundWithResource", map[string]any{"Resource": resource})
	}
	return l.Message("NotFound")
}

func missingParameterNames(message string) []string {
	fields := strings.FieldsFunc(message, func(r rune) bool {
		return r == ' ' || r == ',' || r == ':'
	})
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.HasPrefix(field, "--") || (strings.HasPrefix(field, "<") && strings.HasSuffix(field, ">")) {
			names = append(names, field)
		}
	}
	return names
}

func messageKey(value string) string {
	value = strings.ReplaceAll(value, "-", "_")
	return strings.Join(strings.Fields(value), ".")
}

var flagUsageMessages = map[string]string{
	"Alibaba Cloud region":                                                              "FlagUsage.AlibabaCloudRegion",
	"configuration profile":                                                             "FlagUsage.ConfigurationProfile",
	"default region":                                                                    "FlagUsage.DefaultRegion",
	"disable completion descriptions":                                                   "FlagUsage.DisableCompletionDescriptions",
	"disable color in human output":                                                     "FlagUsage.DisableColor",
	"force JSON output":                                                                 "FlagUsage.ForceJSON",
	"wrap JSON output in the Agent envelope":                                            "FlagUsage.AgentEnvelope",
	"filter products or APIs by keyword":                                                "FlagUsage.FilterProductsOrAPIs",
	"generate a request JSON template from --schema":                                    "FlagUsage.GenerateOpenAPIRequest",
	"inspect an OpenAPI operation schema":                                               "FlagUsage.InspectOpenAPISchema",
	"list supported products or resources":                                              "FlagUsage.ListSupported",
	"list schemas for a product":                                                        "FlagUsage.ListSchemasForProduct",
	"show required and common parameters only":                                          "FlagUsage.SchemaBrief",
	"show all schema-visible parameters":                                                "FlagUsage.SchemaFull",
	"list supported products or APIs":                                                   "FlagUsage.ListSupportedProductsOrAPIs",
	"maximum products or APIs to return":                                                "FlagUsage.LimitProductsOrAPIs",
	"list every product/resource/action topic (default: products only)":                 "FlagUsage.ListAllExamples",
	"display language (supported: en, zh-CN)":                                           "FlagUsage.DisplayLanguage",
	"request JSON object or @file containing a JSON object":                             "FlagUsage.OpenAPIRequest",
	"output mode (json, text)":                                                          "FlagUsage.OutputMode",
	"show sensitive config values":                                                      "FlagUsage.ShowSensitive",
	"skip online verification of the region against the Alibaba Cloud Location service": "FlagUsage.SkipRegionVerification",
	"version for ecctl":                                                                 "FlagUsage.Version",
}

func resetLocalizerCacheForTest() {
	localizerMu.Lock()
	defer localizerMu.Unlock()
	localizerCache = map[string]localizerCacheEntry{}
}
