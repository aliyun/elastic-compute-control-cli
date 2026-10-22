//go:build windows

package telemetry

import (
	"context"
	"testing"
)

func TestWindowsIdentityCacheAlwaysResolvesWithoutPersistence(t *testing.T) {
	calls := 0
	want := Identity{Hash: "hash", Type: "RAMUser", AccountID: "1234567890123456", UserID: "2345678901234567"}
	for i := 0; i < 2; i++ {
		got, err := resolveCachedIdentity(context.Background(), `C:\ecctl\config.json`, "ak", func(context.Context) (Identity, error) {
			calls++
			return want, nil
		})
		if err != nil || got != want {
			t.Fatalf("resolveCachedIdentity = %#v, %v; want %#v", got, err, want)
		}
	}
	if calls != 2 {
		t.Fatalf("Windows unexpectedly persisted identity cache; calls=%d", calls)
	}
}
