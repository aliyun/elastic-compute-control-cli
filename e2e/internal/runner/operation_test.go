package runner

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestOperationRuntimeSortsMultipleLocksWithoutDeadlock(t *testing.T) {
	runtime := newOperationRuntime(2)
	start := make(chan struct{})
	done := make(chan error, 2)
	run := func(locks []string) {
		<-start
		done <- runtime.execute(context.Background(), map[string]any{}, locks, func() {
			time.Sleep(20 * time.Millisecond)
		})
	}
	go run([]string{"second", "first"})
	go run([]string{"first", "second"})
	close(start)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("multiple rendered locks deadlocked")
		}
	}
}

func TestOperationRuntimeConcreteKeysAndParallelLimitPreventOverlap(t *testing.T) {
	for _, test := range []struct {
		name     string
		parallel int
		keys     [][]string
	}{
		{name: "same concrete key", parallel: 2, keys: [][]string{{"shared"}, {"shared"}}},
		{name: "parallel one", parallel: 1, keys: [][]string{{"first"}, {"second"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := newOperationRuntime(test.parallel)
			start := make(chan struct{})
			var active int32
			var overlap int32
			var wg sync.WaitGroup
			for _, keys := range test.keys {
				keys := keys
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if err := runtime.executeKeys(context.Background(), keys, func() {
						if atomic.AddInt32(&active, 1) != 1 {
							atomic.StoreInt32(&overlap, 1)
						}
						time.Sleep(25 * time.Millisecond)
						atomic.AddInt32(&active, -1)
					}); err != nil {
						t.Errorf("executeKeys: %v", err)
					}
				}()
			}
			close(start)
			wg.Wait()
			if atomic.LoadInt32(&overlap) != 0 {
				t.Fatal("coordinated operations overlapped")
			}
		})
	}
}

func TestOperationRuntimeCancelledMultiLockAcquisitionReleasesEarlierLocks(t *testing.T) {
	runtime := newOperationRuntime(2)
	blockerEntered := make(chan struct{})
	releaseBlocker := make(chan struct{})
	blockerDone := make(chan error, 1)
	go func() {
		blockerDone <- runtime.executeKeys(context.Background(), []string{"second"}, func() {
			close(blockerEntered)
			<-releaseBlocker
		})
	}()
	<-blockerEntered

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := runtime.executeKeys(ctx, []string{"first", "second"}, func() {
		t.Error("timed-out operation unexpectedly ran")
	}); err != context.DeadlineExceeded {
		t.Fatalf("executeKeys error = %v, want deadline exceeded", err)
	}

	firstCtx, firstCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer firstCancel()
	if err := runtime.executeKeys(firstCtx, []string{"first"}, func() {}); err != nil {
		t.Fatalf("first lock leaked after partial acquisition: %v", err)
	}
	close(releaseBlocker)
	if err := <-blockerDone; err != nil {
		t.Fatal(err)
	}
}

func TestFailedStepRegistersConcreteCleanupLock(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("fake uses a bash script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "ecctl")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	operations := newOperationRuntime(1)
	cl := newCleanup(
		map[string]execpkg.Config{"primary": {Bin: fake, Region: "cn-test"}},
		operations, true, "", report.CleanupJournal{}, func(string, ...any) {},
	)
	var scope []*cleanupItem
	_, ok := runStep(
		context.Background(), Options{}, execpkg.Config{Bin: fake, Region: "cn-test"},
		cl, &scope, map[string]any{"run_id": "concrete"},
		scenario.Step{
			Name: "failed create", Run: "ecctl test create",
			Teardown: "ecctl test delete", Locks: []string{"resource:{{.run_id}}"},
		},
		time.Second,
	)
	if ok {
		t.Fatal("failed command unexpectedly passed")
	}
	if len(scope) != 1 {
		t.Fatalf("cleanup registrations = %d, want 1", len(scope))
	}
	if got := scope[0].lockKeys; len(got) != 1 || got[0] != "resource:concrete" {
		t.Fatalf("cleanup lock keys = %#v", got)
	}
}
