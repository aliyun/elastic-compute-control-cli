package e2bapi

import (
	"context"
	"os/exec"
)

func systemDNSServers(ctx context.Context, host string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "/usr/sbin/scutil", "--dns")
	// DNS configuration discovery does not need project credentials.
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C"}
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return scutilServers(string(data), host)
}
