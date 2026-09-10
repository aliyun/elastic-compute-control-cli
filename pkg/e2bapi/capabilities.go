package e2bapi

import (
	"context"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

// ValidateOperation runs before the engine sends the first workflow request.
// In particular, a supported timeout update must not precede a rejected network
// update. Only the explicitly selected ACS compatibility profile restricts APIs.
func (c *Caller) ValidateOperation(_ context.Context, resource, action string, input map[string]any) error {
	if c.backend != backendACS {
		return nil
	}
	switch resource {
	case "sandbox":
		switch action {
		case "refresh", "fork", "logs", "metrics":
			return unsupportedACS(resource + "." + action)
		case "create", "update":
			for _, field := range []string{"network", "volume_mounts"} {
				if value, present := input[field]; present && value != nil {
					return unsupportedACS(field)
				}
			}
		}
	case "template":
		switch action {
		case "create", "update", "publish", "unpublish", "build-status", "build-logs", "tag-list", "tag-assign", "tag-delete":
			return unsupportedACS(resource + "." + action)
		}
	}
	return nil
}

func (c *Caller) validateAPIOperation(name string, request map[string]any) error {
	if c.backend != backendACS {
		return nil
	}
	switch name {
	case "UpdateSandboxNetwork", "RefreshSandbox", "ForkSandbox", "GetSandboxLogs", "GetSandboxMetrics",
		"CreateTemplate", "StartTemplateBuild", "UpdateTemplate", "GetTemplateBuildStatus", "GetTemplateBuildLogs",
		"ListTemplateTags", "AssignTemplateTags", "DeleteTemplateTags":
		return unsupportedACS(name)
	case "CreateSandbox":
		body := requestObject(request, "body")
		for _, field := range []string{"network", "volumeMounts"} {
			if value, present := body[field]; present && value != nil {
				return unsupportedACS(field)
			}
		}
	}
	return nil
}

func unsupportedACS(operation string) error {
	return ecerrors.Client("UnsupportedOperation", e2bMessage("UnsupportedACSOperation"), ecerrors.WithDetail(operation))
}
