package waiter

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	ecerrors "ecctl/pkg/errors"
)

type Observation struct {
	State string
	Value any
}

type Probe func(context.Context) (Observation, error)

type Options struct {
	Target        string
	FailureStates []string
	Interval      time.Duration
	Timeout       time.Duration
	MaxAttempts   int
	Probe         Probe
}

func Wait(ctx context.Context, options Options) (Observation, error) {
	if options.Probe == nil {
		return Observation{}, ecerrors.Client("InvalidWaiter", "waiter probe is required")
	}
	ctx, cancel := withTimeout(ctx, options.Timeout)
	defer cancel()

	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var last Observation
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return last, timeoutError(options.Target, last)
		}

		observed, err := options.Probe(ctx)
		if err != nil {
			return observed, err
		}
		last = observed
		if observed.State == options.Target {
			return observed, nil
		}
		if contains(options.FailureStates, observed.State) {
			return observed, ecerrors.Service("WaitFailed", fmt.Sprintf("resource reached failure state %s", observed.State), false,
				ecerrors.WithCurrentState(observed.State),
				ecerrors.WithExpectedStates(options.Target),
			)
		}
		if options.Interval > 0 && attempt < maxAttempts-1 {
			timer := time.NewTimer(options.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return last, timeoutError(options.Target, last)
			case <-timer.C:
			}
		}
	}
	return last, timeoutError(options.Target, last)
}

func AsAppError(err error) (*ecerrors.AppError, bool) {
	var appErr *ecerrors.AppError
	if stderrors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func timeoutError(target string, last Observation) *ecerrors.AppError {
	return ecerrors.Timeout("WaitTimeout", fmt.Sprintf("timed out waiting for %s", target),
		ecerrors.WithCurrentState(last.State),
		ecerrors.WithExpectedStates(target),
	)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
