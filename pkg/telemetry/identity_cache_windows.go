//go:build windows

package telemetry

import (
	"context"
	"errors"
)

func openIdentityCache(context.Context, string) (*identityCacheHandle, error) {
	return nil, errors.New("identity cache persistence is disabled on Windows")
}
