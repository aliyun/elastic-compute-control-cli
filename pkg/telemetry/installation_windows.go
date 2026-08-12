//go:build windows

package telemetry

import (
	"context"
	"errors"
)

func loadOrCreateInstallationToken(ctx context.Context, configPath string) ([]byte, error) {
	return nil, errors.New("installation persistence is disabled on Windows")
}
