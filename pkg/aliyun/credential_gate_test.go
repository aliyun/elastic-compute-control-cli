package aliyun

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContextGateCancellationDoesNotWaitForCurrentHolder(t *testing.T) {
	var gate contextGate
	if err := gate.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := gate.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled lock waited %s", elapsed)
	}
	gate.Unlock()
}
