package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/i18n"
	"ecctl/pkg/spec"
)

// newExamplesCommand wires the `ecctl examples [topic]` subcommand. Without
// arguments it lists every topic that has examples available; with a single
// topic argument it prints the example invocations associated with that topic.
//
// Topic format: <product> | <product>.<resource> | <product>.<resource>.<action>
//
// Example resolution order:
//   - operation.Examples (action topics)
//   - resource.Examples (resource topics, or fallback for an action without examples)
//   - product.Examples  (product topics, or fallback for resource/action without examples)
func newExamplesCommand(options *globalOptions, stdout io.Writer) *cobra.Command {
	var listAll bool
	cmd := &cobra.Command{
		Use:   "examples [topic]",
		Short: "List CLI invocation examples for products, resources, and actions",
		Long: "Print example invocations for ecctl topics.\n\n" +
			"Topic format:\n" +
			"  <product>                            list product-level examples\n" +
			"  <product>.<resource>                 list resource-level examples\n" +
			"  <product>.<resource>.<action>        list action-level examples\n\n" +
			"With no topic, only product-level topics are listed and a hint shows how to drill\n" +
			"down. Pass --all to enumerate every topic (intended for completion/indexing\n" +
			"tooling). Agents should drill down by topic instead of dumping the full catalog.",
		Example: "  ecctl examples\n" +
			"  ecctl examples ecs\n" +
			"  ecctl examples ecs.instance\n" +
			"  ecctl examples ecs.instance.create\n" +
			"  ecctl examples --all",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			topics, err := loadExamplesTopics(options)
			if err != nil {
				return ecerrors.Client("InvalidResourceSpec", err.Error())
			}
			if len(args) == 0 {
				return writeExamplesTopicList(options, stdout, topics, listAll)
			}
			return writeExamplesForTopic(options, stdout, topics, strings.TrimSpace(args[0]))
		},
	}
	cmd.Flags().BoolVar(&listAll, "all", false, "list every product/resource/action topic (default: products only)")
	return cmd
}

// exampleTopic captures a single resolvable topic and its examples.
type exampleTopic struct {
	name        string   // e.g. "ecs", "ecs.instance", "ecs.instance.create"
	description string   // first line of the relevant description (optional)
	examples    []string // resolved examples (may be empty when the topic only exists structurally)
}

// loadExamplesTopics enumerates every topic in the catalog and pre-resolves
// its example set using the (operation → resource → product) fallback chain.
// Topic descriptions are localized for lang via spec.LocalizedText.Text.
func loadExamplesTopics(options *globalOptions) (map[string]exampleTopic, error) {
	specDir := os.Getenv("ECCTL_SPEC_DIR")
	refs, err := spec.ListResources(specDir)
	if err != nil {
		return nil, err
	}
	lang := ""
	if options != nil {
		lang = options.lang
	}
	filterPublic := publicCLIFilterEnabled(options)
	topics := map[string]exampleTopic{}

	productExamples := map[string][]string{}
	productDescription := map[string]string{}
	seenProducts := map[string]bool{}
	for _, ref := range refs {
		if seenProducts[ref.Product] {
			continue
		}
		seenProducts[ref.Product] = true
		productSpec, err := spec.LoadProduct(specDir, ref.Product)
		if err != nil && !os.IsNotExist(err) {
			continue
		}
		if err == nil {
			productExamples[ref.Product] = filterExamplesForPublicCLI(productSpec.Examples, filterPublic)
			productDescription[ref.Product] = firstDescriptionLine(productSpec.Description, lang)
		}
	}

	for _, ref := range refs {
		if filterPublic && !publicCLIResource(ref.Product, ref.Resource) {
			continue
		}
		resource, err := spec.LoadResourceWithParent(specDir, ref.Product, ref.Resource, ref.Parent)
		if err != nil {
			continue
		}

		// Product topic — fed by product.Examples (or any resource fallback if product has none).
		productKey := ref.Product
		if _, ok := topics[productKey]; !ok {
			topics[productKey] = exampleTopic{
				name:        productKey,
				description: productDescription[ref.Product],
				examples:    productExamples[ref.Product],
			}
		}

		// Resource topic.
		resourceKey := ref.Product + "." + ref.Resource
		resourceExamples := filterExamplesForPublicCLI(resource.Examples, filterPublic)
		if len(resourceExamples) == 0 {
			resourceExamples = productExamples[ref.Product]
		}
		topics[resourceKey] = exampleTopic{
			name:        resourceKey,
			description: firstDescriptionLine(resource.Description, lang),
			examples:    resourceExamples,
		}

		// Action topics — one per operation.
		for actionName, operation := range resource.Operations {
			actionKey := ref.Product + "." + ref.Resource + "." + actionName
			examples := filterExamplesForPublicCLI(operation.Examples, filterPublic)
			// Falling back to resource/product examples used to display unrelated
			// commands (e.g. `eni.get` showing list/create/attach examples). Only
			// keep fallback entries whose command prefix matches this action so
			// agents never see commands attributed to the wrong topic.
			if len(examples) == 0 {
				examples = filterExamplesByActionPrefix(resource.Examples, ref.Product, ref.Resource, actionName)
			}
			if len(examples) == 0 {
				examples = filterExamplesByActionPrefix(productExamples[ref.Product], ref.Product, ref.Resource, actionName)
			}
			description := firstDescriptionLine(operation.Description, lang)
			if description == "" {
				description = firstDescriptionLine(resource.Description, lang)
			}
			topics[actionKey] = exampleTopic{
				name:        actionKey,
				description: description,
				examples:    examples,
			}
		}
	}

	return topics, nil
}

func filterExamplesForPublicCLI(examples []string, filterPublic bool) []string {
	if !filterPublic {
		return examples
	}
	return publicCLIExamples(examples, false)
}

// filterExamplesByActionPrefix keeps only example invocations whose command
// prefix matches the action's CLI form. For most resources that means
// `ecctl <product> <resource> <action>`, but when product == resource the
// command tree collapses to `ecctl <product> <action>`. Entries that do not
// match (e.g. `ecctl ecs sg list ...` slipping into `ecs.sg.get`) are dropped.
// When no entry matches the function returns nil so callers can short-circuit
// to "no example" rather than displaying commands attributed to a different
// action.
func filterExamplesByActionPrefix(examples []string, product, resource, action string) []string {
	if len(examples) == 0 {
		return nil
	}
	prefixes := []string{"ecctl " + product + " " + resource + " " + action}
	if product == resource {
		prefixes = append(prefixes, "ecctl "+product+" "+action)
	}
	out := make([]string, 0, len(examples))
	for _, example := range examples {
		trimmed := strings.TrimSpace(example)
		for _, prefix := range prefixes {
			if trimmed == prefix || strings.HasPrefix(trimmed, prefix+" ") {
				out = append(out, example)
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstDescriptionLine resolves text to lang via spec.LocalizedText.Text (which
// handles fallbacks: exact tag → normalized tag → base language → en → first
// available) and trims to the first line so list views stay one line per topic.
func firstDescriptionLine(text spec.LocalizedText, lang string) string {
	value := text.Text(lang)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return strings.TrimSpace(value)
}

// writeExamplesTopicList outputs known topics, sorted alphabetically.
// By default only product-level topics are listed (one entry per product), with
// a drill-down hint, so agents see a small deterministic surface instead of a
// 350+ entry dump. listAll restores the full enumeration for tooling that
// genuinely wants every product/resource/action topic.
func writeExamplesTopicList(options *globalOptions, stdout io.Writer, topics map[string]exampleTopic, listAll bool) error {
	names := make([]string, 0, len(topics))
	for name, topic := range topics {
		if len(topic.examples) == 0 {
			continue
		}
		if !listAll && strings.Contains(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	lang := ""
	if options != nil {
		lang = options.lang
	}
	drillDownHint := i18n.NewLocalizer(lang).Message("ExamplesDrillDownHint")

	if jsonOutputRequested(options) {
		entries := make([]map[string]any, 0, len(names))
		for _, name := range names {
			topic := topics[name]
			entries = append(entries, map[string]any{
				"topic":          topic.name,
				"description":    topic.description,
				"examples_count": len(topic.examples),
			})
		}
		payload := map[string]any{"topics": entries}
		if !listAll {
			payload["hint"] = drillDownHint
		}
		return writeCommandOutput(options, stdout, payload)
	}

	var buf strings.Builder
	for _, name := range names {
		buf.WriteString(name)
		buf.WriteString("\n")
	}
	if !listAll {
		buf.WriteString("\n# ")
		buf.WriteString(drillDownHint)
		buf.WriteString("\n")
	}
	_, err := io.WriteString(stdout, buf.String())
	return err
}

// writeExamplesForTopic outputs examples for a single topic.
func writeExamplesForTopic(options *globalOptions, stdout io.Writer, topics map[string]exampleTopic, topic string) error {
	if topic == "" {
		return ecerrors.Client("UnknownTopic", "topic is required",
			ecerrors.WithField("topic"))
	}
	entry, ok := topics[topic]
	if !ok || len(entry.examples) == 0 {
		// SuggestedAction is intentionally omitted — i18n.Localizer.ErrorPayload
		// fills both Suggestion and SuggestedAction with the localized text
		// (`SuggestionUnknownTopic`) when neither is set explicitly.
		return ecerrors.Client("UnknownTopic", fmt.Sprintf("topic %q has no examples", topic),
			ecerrors.WithField("topic"),
		)
	}

	if jsonOutputRequested(options) {
		payload := map[string]any{
			"topic":    entry.name,
			"examples": entry.examples,
		}
		if entry.description != "" {
			payload["description"] = entry.description
		}
		return writeCommandOutput(options, stdout, payload)
	}

	var buf strings.Builder
	if entry.description != "" {
		buf.WriteString("# ")
		buf.WriteString(entry.description)
		buf.WriteString("\n")
	}
	for _, example := range entry.examples {
		example = strings.TrimSpace(example)
		if example == "" {
			continue
		}
		buf.WriteString(example)
		buf.WriteString("\n")
	}
	_, err := io.WriteString(stdout, buf.String())
	return err
}

// jsonOutputRequested returns true when the user expects machine-readable
// output (the CLI default is JSON, but text mode opts out).
func jsonOutputRequested(options *globalOptions) bool {
	if options == nil {
		return true
	}
	if options.json || options.agentEnvelope {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(options.output)) {
	case "", "json":
		return true
	default:
		return false
	}
}
