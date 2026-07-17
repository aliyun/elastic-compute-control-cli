package ecs

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	ecerrors "ecctl/pkg/errors"
	spechooks "ecctl/specs"
)

func init() {
	spechooks.RegisterBeforeOperation("ecs", "instance", "validate_dry_run_amount", validateRunInstancesDryRunAmount)
	spechooks.RegisterBeforeOperation("ecs", "instance", "resolve_image_name", resolveRunInstancesImage)
	spechooks.RegisterAfterOperationError("ecs", "instance", "suggest_availability_query", suggestRunInstancesAvailabilityQuery)
	spechooks.RegisterAfterOperationError("ecs", "instance", "suggest_force_release", suggestForceRelease)
	spechooks.RegisterFieldMapper("ecs", "instance", "output_text", decodeInvocationOutputText)
}

func validateRunInstancesDryRunAmount(_ context.Context, _ spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	if !boolMapValue(request, "DryRun") {
		return request, nil
	}
	for _, field := range []struct {
		requestName string
		fieldName   string
	}{
		{requestName: "Amount", fieldName: "amount"},
		{requestName: "MinAmount", fieldName: "min_amount"},
	} {
		raw, specified := request[field.requestName]
		if !specified {
			continue
		}
		amount, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(raw)))
		if err == nil && amount == 1 {
			continue
		}
		return nil, ecerrors.Client("InvalidDryRunAmount", "ECS dry-run only supports a single instance",
			ecerrors.WithField(field.fieldName),
			ecerrors.WithAcceptedValues("1"),
		)
	}
	return request, nil
}

func resolveRunInstancesImage(ctx context.Context, caller spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	image := strings.TrimSpace(stringMapValue(request, "ImageId"))
	if image == "" || strings.HasSuffix(strings.ToLower(image), ".vhd") {
		return request, nil
	}
	imageID, err := lookupECSImageID(ctx, caller, request, image)
	if err != nil {
		return nil, err
	}
	resolved := cloneMap(request)
	resolved["ImageId"] = imageID
	return resolved, nil
}

func lookupECSImageID(ctx context.Context, caller spechooks.OperationCaller, request map[string]any, image string) (string, error) {
	instanceType := stringMapValue(request, "InstanceType")
	response, err := caller.CallRaw(ctx, "DescribeImages", map[string]any{
		"RegionId":     stringMapValue(request, "RegionId"),
		"ImageName":    "*" + image + "*",
		"InstanceType": instanceType,
		"PageNumber":   1,
		"PageSize":     1,
	})
	if err != nil {
		return "", err
	}
	if imageID := firstECSImageID(response); imageID != "" {
		return imageID, nil
	}
	return "", ecerrors.NotFound("ImageNotFound", missingECSImageMessage(image, instanceType))
}

func firstECSImageID(response map[string]any) string {
	images, ok := response["Images"].(map[string]any)
	if !ok {
		return ""
	}
	items, ok := images["Image"].([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		image, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id := stringMapValue(image, "ImageId"); id != "" {
			return id
		}
	}
	return ""
}

func missingECSImageMessage(image, instanceType string) string {
	if instanceType == "" {
		return fmt.Sprintf("image %q not found", image)
	}
	return fmt.Sprintf("image %q not found or unsupported by instance type %q", image, instanceType)
}

func decodeInvocationOutputText(value any) any {
	raw, ok := value.(string)
	if !ok || raw == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func suggestRunInstancesAvailabilityQuery(_ context.Context, _ spechooks.OperationCaller, request map[string]any, err error) error {
	action := ecerrors.ActionFromError("RunInstances", err)
	field, destination := availabilityErrorTarget(action.Code)
	if field == "" {
		return err
	}
	return ecerrors.WithDetails(err,
		ecerrors.WithField(field),
		ecerrors.WithSuggestedAction(availabilitySuggestedAction(request, destination)),
	)
}

// suggestForceRelease turns the IncorrectInstanceStatus error returned when
// deleting a running instance into an actionable hint. Without --force a
// running instance can never be released, so instead of letting the request
// retry until the grace period expires we surface the error immediately and
// tell the user how to proceed.
func suggestForceRelease(_ context.Context, _ spechooks.OperationCaller, request map[string]any, err error) error {
	code := ecerrors.ActionFromError("DeleteInstance", err).Code
	if !strings.Contains(code, "IncorrectInstanceStatus") {
		return err
	}
	if boolMapValue(request, "Force") {
		// Already forcing: the instance is in a transient state (e.g. still
		// initializing) rather than simply running. Retrying shortly resolves it.
		return ecerrors.WithDetails(err, ecerrors.WithSuggestion(
			"the instance is not in a releasable state yet; wait for it to finish its current operation and retry"))
	}
	return ecerrors.WithDetails(err, ecerrors.WithSuggestion(
		"running instances cannot be deleted without --force; rerun with --force to force release, or stop it first with `ecctl ecs instance stop <id>`"))
}

func boolMapValue(values map[string]any, key string) bool {
	switch value := values[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

func availabilityErrorTarget(code string) (field string, destinationResource string) {
	switch code {
	case "InvalidResourceType.NotSupported":
		return "type", "InstanceType"
	case "InvalidDiskCategory.NotSupported":
		return "system_disk.category", "SystemDisk"
	default:
		return "", ""
	}
}

func availabilitySuggestedAction(request map[string]any, destinationResource string) string {
	parts := []string{
		"ecctl call ecs DescribeAvailableResource",
		"--region " + shellArg(stringMapValue(request, "RegionId")),
		"--DestinationResource " + shellArg(destinationResource),
	}
	if zone := stringMapValue(request, "ZoneId"); zone != "" {
		parts = append(parts, "--ZoneId "+shellArg(zone))
	}
	if instanceType := stringMapValue(request, "InstanceType"); instanceType != "" {
		parts = append(parts, "--InstanceType "+shellArg(instanceType))
	}
	if chargeType := stringMapValue(request, "InstanceChargeType"); chargeType != "" {
		parts = append(parts, "--InstanceChargeType "+shellArg(chargeType))
	}
	if network := stringMapValue(request, "NetworkCategory"); network != "" {
		parts = append(parts, "--NetworkCategory "+shellArg(network))
	}
	if ioOptimized := stringMapValue(request, "IoOptimized"); ioOptimized != "" {
		parts = append(parts, "--IoOptimized "+shellArg(ioOptimized))
	}
	if destinationResource == "SystemDisk" {
		parts = append(parts, "--ResourceType instance")
	}
	if systemDisk := stringMapValue(request, "SystemDisk.Category"); systemDisk != "" {
		parts = append(parts, "--SystemDiskCategory "+shellArg(systemDisk))
	}
	return strings.Join(parts, " ")
}

func shellArg(value string) string {
	if value == "" {
		return "<region>"
	}
	if strings.ContainsAny(value, " \t\n\"'") {
		return strconv.Quote(value)
	}
	return value
}

func cloneMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func stringMapValue(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}
