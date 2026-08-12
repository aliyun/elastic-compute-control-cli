package runner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	execpkg "github.com/aliyun/elastic-compute-control-cli/e2e/internal/exec"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/report"
	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/vars"
)

// Session owns run-lifetime fixture leases across multiple execution-unit
// runner invocations. It is intentionally process-local: no fixture survives a
// top-level ecctl-e2e run.
type Session struct {
	mu       sync.Mutex
	entries  map[string]*sessionFixture
	order    []string
	closed   bool
	ops      *operationRuntime
	parallel int
}

func (s *Session) operationRuntime(parallel int) (*operationRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("runner session is closed")
	}
	if s.ops == nil {
		s.ops = newOperationRuntime(parallel)
		s.parallel = parallel
		return s.ops, nil
	}
	if s.parallel != parallel {
		return nil, fmt.Errorf("runner session concurrency changed from %d to %d", s.parallel, parallel)
	}
	return s.ops, nil
}

type sessionFixture struct {
	surface     string
	binary      string
	region      string
	id          string
	commandHash [sha256.Size]byte
	captures    map[string]any
	failure     string
	step        report.Step
	cleanup     *cleanup
	scope       []*cleanupItem
}

func NewSession() *Session {
	return &Session{entries: map[string]*sessionFixture{}}
}

func sessionFixtureKey(surface, binary, region, id string) string {
	return strings.Join([]string{surface, binary, region, id}, "\x00")
}

func (s *Session) acquire(
	ctx context.Context,
	opt Options,
	execCfg execpkg.Config,
	cl *cleanup,
	base map[string]any,
	stackVars map[string]any,
	fix *Fixture,
	sc *report.Case,
) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("runner session is closed")
	}
	failures := map[string]string{}
	for _, provision := range fix.Provision {
		if failed := directFailedDependency(provision, failures); failed != "" {
			message := fmt.Sprintf("dependency %q failed", failed)
			if stackStepSkipped(failures[failed]) {
				message = fmt.Sprintf("dependency %q unavailable", failed)
				failures[provision.ID] = stackSkippedPrefix + message
			} else {
				failures[provision.ID] = message
			}
			step := report.Step{Name: provision.ID, Status: report.StatusSkipped, Error: message}
			sc.Steps = append(sc.Steps, step)
			continue
		}
		data := vars.Clone(base)
		for name, value := range stackVars {
			data[name] = value
		}
		data["stack"] = stackVars
		if missing := missingPrerequisites(data, provision.RequiresPrerequisites); len(missing) > 0 {
			message := "prerequisites unavailable: " + strings.Join(missing, ", ")
			failures[provision.ID] = stackSkippedPrefix + message
			sc.Steps = append(sc.Steps, report.Step{
				Name: provision.ID, Status: report.StatusSkipped, Error: message,
				RequiresPrerequisites: append([]string(nil), provision.RequiresPrerequisites...),
			})
			continue
		}
		command, err := vars.Render(provision.Run, data)
		if err != nil {
			message := "render: " + err.Error()
			failures[provision.ID] = message
			sc.Steps = append(sc.Steps, report.Step{Name: provision.ID, Status: report.StatusError, Error: message})
			continue
		}
		key := sessionFixtureKey(opt.Surface, opt.EcctlBin, opt.Region, provision.ID)
		hash := sha256.Sum256([]byte(command))
		if existing := s.entries[key]; existing != nil {
			if existing.commandHash != hash {
				return nil, fmt.Errorf(
					"run-lifetime fixture %q rendered command changed for surface=%q binary=%q region=%q",
					provision.ID, opt.Surface, opt.EcctlBin, opt.Region,
				)
			}
			for name, value := range existing.captures {
				stackVars[name] = value
			}
			if existing.failure != "" {
				failures[provision.ID] = existing.failure
			}
			reused := existing.step
			reused.Stdout = strings.TrimSpace(strings.Join([]string{reused.Stdout, "reused run-lifetime fixture"}, "\n"))
			sc.Steps = append(sc.Steps, reused)
			continue
		}

		localScope := []*cleanupItem{}
		localFailures := provisionStack(
			ctx, opt, execCfg, cl, &localScope, base, stackVars,
			&Fixture{Provision: []ProvisionStep{provision}}, sc, failures,
		)
		entry := &sessionFixture{
			surface: opt.Surface, binary: opt.EcctlBin, region: opt.Region, id: provision.ID,
			commandHash: hash, captures: map[string]any{}, cleanup: cl, scope: localScope,
		}
		for name := range provision.Capture {
			if value, ok := stackVars[name]; ok {
				entry.captures[name] = value
			}
		}
		if failure := localFailures[provision.ID]; failure != "" {
			entry.failure = failure
			failures[provision.ID] = failure
		}
		if len(sc.Steps) > 0 {
			entry.step = sc.Steps[len(sc.Steps)-1]
		}
		s.entries[key] = entry
		s.order = append(s.order, key)
	}
	return failures, nil
}

// Close tears down every retained fixture once in reverse acquisition order.
func (s *Session) Close() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.cleanupMatching(func(*sessionFixture) bool { return true })
}

// Discard tears down and forgets fixtures for an assignment before region
// fallback proceeds. A later execution may provision the region again.
func (s *Session) Discard(surface, binary, region string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.cleanupMatching(func(entry *sessionFixture) bool {
		return entry.surface == surface && entry.binary == binary && entry.region == region
	})
}

func (s *Session) cleanupMatching(matches func(*sessionFixture) bool) []string {
	failures := make([]string, 0)
	kept := make([]string, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		key := s.order[i]
		entry := s.entries[key]
		if entry == nil || !matches(entry) {
			if entry != nil {
				kept = append(kept, key)
			}
			continue
		}
		failures = append(failures, entry.cleanup.run(entry.scope)...)
		delete(s.entries, key)
	}
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	s.order = kept
	return failures
}
