//go:build windows

package telemetry

import (
	"context"
	"testing"
)

func TestWindowsIdentityCacheAlwaysResolvesWithoutPersistence(t *testing.T) {
	calls := 0
	for i := 0; i < 2; i++ {
		if _, err := resolveCachedIdentity(context.Background(), `C:\ecctl\config.json`, "ak", func(context.Context) (Identity, error) {
			calls++
			return Identity{Hash: "hash", Type: "RAMUser"}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("Windows unexpectedly persisted identity cache; calls=%d", calls)
	}
}
