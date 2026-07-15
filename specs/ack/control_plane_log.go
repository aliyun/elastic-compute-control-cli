package ack

import (
	"context"

	spechooks "ecctl/specs"
)

func init() {
	spechooks.RegisterBeforeOperation("ack", "control-plane-log", "disable_control_plane_log_components", disableControlPlaneLogComponents)
}

func disableControlPlaneLogComponents(_ context.Context, _ spechooks.OperationCaller, request map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(request)+1)
	for key, value := range request {
		resolved[key] = value
	}
	resolved["components"] = []string{}
	return resolved, nil
}
