package cli

import (
	"maps"
	"slices"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/spf13/cobra"
)

func sandboxTemplateRequestTimeout(cmd *cobra.Command, resource spec.ResourceSpec, action string, input map[string]any, timeout time.Duration) (time.Duration, error) {
	native, _ := input["openapi"].(bool)
	noWait, _ := input["no_wait"].(bool)
	if resource.Product != "sandbox" || resource.Resource != "template" || !native || (action != "create" && action != "update") {
		return timeout, nil
	}
	if !noWait && timeout <= 0 {
		return 0, ecerrors.Client("InvalidFCTemplateWaitTimeout", i18n.NewLocalizer("en").Message("InvalidFCTemplateWaitTimeout"), ecerrors.WithField("timeout"))
	}
	if noWait && !cmd.Flags().Changed("timeout") {
		// Credential preparation precedes the workflow's wait_unless check.
		// Bound the one request rather than requiring the build-wait lifetime.
		timeout = 30 * time.Second
		input["timeout"] = timeout
	}
	return timeout, nil
}

// Only the explicit selector changes transport. FC's E2B compatibility backend
// and E2B credentials remain independent from the native OpenAPI surface.
func sandboxTemplateTransport(cmd *cobra.Command, resource spec.ResourceSpec, action string, operation spec.Operation) (spec.ResourceSpec, spec.Operation, error) {
	if resource.Product != "sandbox" || resource.Resource != "template" {
		return resource, operation, nil
	}
	native := false
	if cmd.Flags().Lookup("openapi") != nil {
		native, _ = cmd.Flags().GetBool("openapi")
	}
	for _, field := range []string{"team-id", "runtime-config", "build-config"} {
		if !native && cmd.Flags().Changed(field) {
			return resource, operation, ecerrors.Client("FCTemplateRequiresOpenAPI", i18n.NewLocalizer("en").Message("FCTemplateRequiresOpenAPI"), ecerrors.WithField(field))
		}
	}
	if !native {
		if action == "update" {
			operation.Input.Fields = slices.Clone(operation.Input.Fields)
			for index := range operation.Input.Fields {
				if operation.Input.Fields[index].Name == "public" {
					operation.Input.Fields[index].Required, operation.Input.Fields[index].HasRequired = true, true
				}
			}
			for _, field := range []string{"no-wait", "timeout"} {
				if cmd.Flags().Changed(field) {
					return resource, operation, ecerrors.Client("FCTemplateRequiresOpenAPI", i18n.NewLocalizer("en").Message("FCTemplateRequiresOpenAPI"), ecerrors.WithField(field))
				}
			}
			// The union schema makes public optional for native discovery, so
			// boolean defaults cannot prove explicit legacy visibility input.
			if !cmd.Flags().Changed("public") {
				return resource, operation, ecerrors.Client("MissingParameter", i18n.NewLocalizer("en").Message("MissingParameter")+": public", ecerrors.WithField("public"))
			}
			operation.Input.Controls = slices.DeleteFunc(slices.Clone(operation.Input.Controls), func(ref spec.OperationFieldRef) bool { return ref.Name == "no_wait" || ref.Name == "timeout" })
			resource.Operations = maps.Clone(resource.Operations)
			resource.Operations[action] = operation
		}
		return resource, operation, nil
	}
	if region := cmd.Flag("region"); region != nil && region.Changed && region.Value.String() == "" {
		return resource, operation, ecerrors.Client("MissingRegion", i18n.NewLocalizer("en").Message("MissingRegion"), ecerrors.WithField("region"))
	}
	incompatible := []string{"public", "tag", "cpu-count", "memory-mb", "from-image", "from-template", "from-image-registry", "force", "step", "steps", "start-cmd", "ready-cmd"}
	if action == "get" {
		incompatible = append(incompatible, "limit", "next-token")
	}
	for _, field := range incompatible {
		if cmd.Flags().Changed(field) {
			return resource, operation, ecerrors.Client("FCTemplateIncompatibleFlag", i18n.NewLocalizer("en").Message("FCTemplateIncompatibleFlag"), ecerrors.WithField(field))
		}
	}
	resource.Provider, resource.Kind = "aliyun", "regional"
	operation.Input.Fields = slices.Clone(operation.Input.Fields)
	if action == "create" {
		operation.RequireAny = nil
	}
	if action == "update" {
		operation.RequireAny = []spec.Requirement{{Raw: "$.runtime_config"}, {Raw: "$.build_config"}}
	}
	resource.Operations = maps.Clone(resource.Operations)
	resource.Operations[action] = operation
	return resource, operation, nil
}
