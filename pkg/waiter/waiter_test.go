package waiter

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func TestWaitReturnsWhenTargetObserved(t *testing.T) {
	states := []Observation{{State: "pending"}, {State: "running"}}
	got, err := Wait(context.Background(), Options{
		Target:      "running",
		MaxAttempts: 3,
		Probe: func(context.Context) (Observation, error) {
			next := states[0]
			states = states[1:]
			return next, nil
		},
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got.State != "running" {
		t.Fatalf("state = %q, want running", got.State)
	}
}

func TestWaitTimeoutUsesStructuredError(t *testing.T) {
	_, err := Wait(context.Background(), Options{
		Target:      "running",
		MaxAttempts: 2,
		Probe: func(context.Context) (Observation, error) {
			return Observation{State: "pending"}, nil
		},
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	appErr, ok := AsAppError(err)
	if !ok {
		t.Fatalf("timeout is not AppError: %T", err)
	}
	if appErr.Payload().Code != "WaitTimeout" || appErr.ExitCode() != 3 {
		t.Fatalf("unexpected timeout error: %#v", appErr.Payload())
	}
}

func TestWaitRequiresProbe(t *testing.T) {
	_, err := Wait(context.Background(), Options{Target: "running"})
	if err == nil {
		t.Fatal("expected error")
	}
	appErr, ok := AsAppError(err)
	if !ok || appErr.Payload().Code != "InvalidWaiter" {
		t.Fatalf("error = %T %v, want InvalidWaiter", err, err)
	}
}

func TestWaitReturnsProbeErrorAndObservation(t *testing.T) {
	wantErr := stderrors.New("probe failed")

	got, err := Wait(context.Background(), Options{
		Target:      "running",
		MaxAttempts: 2,
		Probe: func(context.Context) (Observation, error) {
			return Observation{State: "pending", Value: "i-1"}, wantErr
		},
	})

	if !stderrors.Is(err, wantErr) {
		t.Fatalf("Wait error = %v, want %v", err, wantErr)
	}
	if got.State != "pending" || got.Value != "i-1" {
		t.Fatalf("observation = %#v", got)
	}
}

func TestWaitReturnsServiceFailureOnFailureState(t *testing.T) {
	got, err := Wait(context.Background(), Options{
		Target:        "running",
		FailureStates: []string{"deleted"},
		MaxAttempts:   2,
		Probe: func(context.Context) (Observation, error) {
			return Observation{State: "deleted"}, nil
		},
	})

	if got.State != "deleted" {
		t.Fatalf("state = %q, want deleted", got.State)
	}
	appErr, ok := AsAppError(err)
	if !ok || appErr.Payload().Code != "WaitFailed" || appErr.Payload().Kind != "service" || appErr.Payload().CurrentState != "deleted" {
		t.Fatalf("error = %T %v, want service WaitFailed with current state deleted", err, err)
	}
}

func TestWaitReturnsTimeoutWhenContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Wait(ctx, Options{
		Target:      "running",
		MaxAttempts: 1,
		Probe: func(context.Context) (Observation, error) {
			t.Fatal("probe should not be called")
			return Observation{}, nil
		},
	})

	appErr, ok := AsAppError(err)
	if !ok || appErr.Payload().Code != "WaitTimeout" {
		t.Fatalf("error = %T %v, want WaitTimeout", err, err)
	}
}

func TestWaitReturnsTimeoutWhenContextCanceledDuringInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	got, err := Wait(ctx, Options{
		Target:      "running",
		Interval:    time.Hour,
		MaxAttempts: 2,
		Probe: func(context.Context) (Observation, error) {
			cancel()
			return Observation{State: "pending"}, nil
		},
	})

	if got.State != "pending" {
		t.Fatalf("state = %q, want pending", got.State)
	}
	appErr, ok := AsAppError(err)
	if !ok || appErr.Payload().CurrentState != "pending" {
		t.Fatalf("error = %T %v, want timeout with current state", err, err)
	}
}

func TestWithTimeoutUsesDeadlineForPositiveTimeout(t *testing.T) {
	ctx, cancel := withTimeout(context.Background(), time.Hour)
	defer cancel()

	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("withTimeout did not set deadline")
	}
}

func TestAsAppErrorReturnsFalseForPlainError(t *testing.T) {
	if appErr, ok := AsAppError(stderrors.New("plain")); ok || appErr != nil {
		t.Fatalf("AsAppError plain = (%#v, %v), want nil false", appErr, ok)
	}
}

func TestAsAppErrorFindsWrappedAppError(t *testing.T) {
	err := stderrors.Join(ecerrors.Client("InvalidWaiter", "waiter probe is required"))

	appErr, ok := AsAppError(err)
	if !ok || appErr.Payload().Code != "InvalidWaiter" {
		t.Fatalf("AsAppError wrapped = (%#v, %v)", appErr, ok)
	}
}
