//go:build !darwin && !windows

package e2bapi

import (
	"context"
	"os"
)

func systemDNSServers(_ context.Context, _ string) ([]string, error) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	return resolvConfServers(string(data))
}
