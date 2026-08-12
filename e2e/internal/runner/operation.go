package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

// operationRuntime bounds command execution across every case in one runner
// invocation and provides deterministic keyed mutation locks.
type operationRuntime struct {
	sem   chan struct{}
	locks keyedLocks
}

func newOperationRuntime(parallel int) *operationRuntime {
	if parallel <= 0 {
		parallel = 1
	}
	return &operationRuntime{sem: make(chan struct{}, parallel), locks: keyedLocks{byKey: map[string]chan struct{}{}}}
}

func (r *operationRuntime) execute(ctx context.Context, data map[string]any, lockTemplates []string, fn func()) error {
	keys, err := renderLockKeys(data, lockTemplates)
	if err != nil {
		return err
	}
	return r.executeKeys(ctx, keys, fn)
}

// executeKeys coordinates an operation whose lock templates have already been
// rendered. Cleanup uses this path so teardown competes for the same global
// parallelism and concrete resource locks as normal case operations.
func (r *operationRuntime) executeKeys(ctx context.Context, concreteKeys []string, fn func()) error {
	keys, err := normalizeLockKeys(concreteKeys)
	if err != nil {
		return err
	}
	unlock, err := r.locks.acquire(ctx, keys)
	if err != nil {
		return err
	}
	defer unlock()
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	fn()
	return nil
}

func renderLockKeys(data map[string]any, templates []string) ([]string, error) {
	keys := make([]string, 0, len(templates))
	for _, text := range templates {
		key, err := vars.Render(text, data)
		if err != nil {
			return nil, fmt.Errorf("render lock %q: %w", text, err)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("render lock %q: empty key", text)
		}
		keys = append(keys, key)
	}
	return normalizeLockKeys(keys)
}

func normalizeLockKeys(keys []string) ([]string, error) {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("empty lock key")
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized, nil
}

type keyedLocks struct {
	mu    sync.Mutex
	byKey map[string]chan struct{}
}

func (k *keyedLocks) acquire(ctx context.Context, keys []string) (func(), error) {
	locks := make([]chan struct{}, 0, len(keys))
	k.mu.Lock()
	for _, key := range keys {
		lock := k.byKey[key]
		if lock == nil {
			lock = make(chan struct{}, 1)
			lock <- struct{}{}
			k.byKey[key] = lock
		}
		locks = append(locks, lock)
	}
	k.mu.Unlock()
	acquired := make([]chan struct{}, 0, len(locks))
	for _, lock := range locks {
		select {
		case <-lock:
			acquired = append(acquired, lock)
		case <-ctx.Done():
			for i := len(acquired) - 1; i >= 0; i-- {
				acquired[i] <- struct{}{}
			}
			return nil, ctx.Err()
		}
	}
	return func() {
		for i := len(acquired) - 1; i >= 0; i-- {
			acquired[i] <- struct{}{}
		}
	}, nil
}
