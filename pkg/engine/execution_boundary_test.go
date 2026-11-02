package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestRetryConfigurationAndTimeoutBoundaries(t *testing.T) {
	for _, retry := range []spec.TransitionRetry{
		{Errors: []string{"Retry"}, InitialInterval: "bad"}, {Errors: []string{"Retry"}, MaxInterval: "bad"}, {Errors: []string{"Retry"}, Timeout: "bad"},
	} {
		if _, _, err := transitionRetryConfigFor(retry); err == nil {
			t.Fatal("invalid retry duration accepted")
		}
		if _, err := NewExecutor(spec.ResourceSpec{}, &fakeCaller{}).callTransition(context.Background(), spec.Transition{Retry: retry}, nil, ExecutionContext{}); err == nil {
			t.Fatal("transition ignored retry configuration error")
		}
	}
	config, enabled, err := transitionRetryConfigFor(spec.TransitionRetry{Errors: []string{"Retry"}, InitialInterval: "0s", MaxInterval: "0s", Timeout: "0s"})
	if err != nil || !enabled || config.initialInterval != time.Second || config.maxInterval != time.Second || config.timeout != time.Second {
		t.Fatalf("normalized retry = %+v, %t, %v", config, enabled, err)
	}
	transition := spec.Transition{Call: "Change", Retry: spec.TransitionRetry{Errors: []string{"Retry"}, InitialInterval: "1s", Timeout: "1ms"}}
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		caller := &fakeCaller{errors: []error{ecerrors.Service("Retry", "retry", false)}}
		_, err := NewExecutor(spec.ResourceSpec{}, caller).callTransition(ctx, transition, nil, ExecutionContext{})
		cancel()
		if err == nil || !strings.Contains(err.Error(), "HiddenRetryTimeout") {
			t.Fatalf("retry timeout = %v", err)
		}
	}
	transition.Retry.When = "unknown"
	if _, err := NewExecutor(spec.ResourceSpec{}, &fakeCaller{}).callTransition(context.Background(), transition, nil, ExecutionContext{}); err == nil {
		t.Fatal("invalid retry condition accepted")
	}
	if isHiddenTransitionRetryError(nil, []string{"Retry"}) {
		t.Fatal("nil error retried")
	}
}

func TestEngineRejectsInvalidConditionsAndActionIDs(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"value": "x", "ids": []any{"one", 2}, "number": 1}, Context: map[string]any{}}
	for _, expr := range []string{"unknown", "unknown(input.value)", "has(unknown)", "starts_with(input.value)", `starts_with(input.value,"")`, "unknown || input.value", "unknown == x", "input.value == $unknown", "input.value == context.absent"} {
		_, err := conditionMatches(expr, ctx)
		if expr == "input.value == context.absent" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("invalid condition accepted: %q", expr)
		}
	}
	if _, err := shouldRun("", []string{"unknown"}, ctx); err == nil {
		t.Fatal("invalid when_any accepted")
	}
	if _, err := shouldRun("", []string{"input.absent"}, ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := conditionMatches(`input.value == 'X'`, ctx); err != nil || !got {
		t.Fatalf("single quote comparison = %t, %v", got, err)
	}
	for _, expr := range []string{"$unknown", "$.ids", "$.number"} {
		if _, err := resolveActionIDs([]string{expr}, ctx); err == nil {
			t.Fatalf("invalid action IDs accepted: %s", expr)
		}
	}
	for _, tt := range []struct {
		value any
		want  int
	}{{[]any{1}, 1}, {"x", 1}, {"", 0}, {nil, 0}, {1, 1}} {
		if got := conditionListLength(tt.value); got != tt.want {
			t.Fatalf("condition list length(%#v) = %d, want %d", tt.value, got, tt.want)
		}
	}
	if _, err := NewExecutor(spec.ResourceSpec{}, &fakeCaller{}).Execute(context.Background(), Request{Action: "missing"}); err == nil {
		t.Fatal("unsupported action accepted")
	}
}

func TestWaiterRejectsInvalidConfiguration(t *testing.T) {
	for _, resource := range []spec.ResourceSpec{
		{}, {Waiters: map[string]spec.Waiter{"wait": {Probe: "missing"}}},
		{Waiters: map[string]spec.Waiter{"wait": {Probe: "probe", Interval: "bad"}}, Probes: map[string]spec.Probe{"probe": {}}},
		{Waiters: map[string]spec.Waiter{"wait": {Probe: "probe", Timeout: "bad"}}, Probes: map[string]spec.Probe{"probe": {}}},
	} {
		if _, err := NewExecutor(resource, &fakeCaller{}).wait(context.Background(), Request{}, "wait", ExecutionContext{}, nil); err == nil {
			t.Fatal("invalid waiter accepted")
		}
	}
	if result, err := NewExecutor(spec.ResourceSpec{}, nil).runWaiterProbe(context.Background(), spec.Waiter{Match: spec.WaiterMatch{ProbeEachCapture: true, Capture: "missing"}}, ExecutionContext{}, nil); err != nil || len(result.Items) != 0 {
		t.Fatalf("missing capture = %+v, %v", result, err)
	}
}

func TestMappingFunctionsRejectMalformedAndMissingArguments(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{"value": "x", "invalid": make(chan int)}}
	for _, expr := range []string{"$kv_bad", "$except($.value)", "$except($unknown,$.value)", "$except($.value,$unknown)", "$prefixed_values($.value)", `$prefixed_values($.value,"")`, "$matching_prefix($.value)", `$matching_prefix($.value,"")`, "$not_matching_prefix($.value)", `$not_matching_prefix($.value,"")`, "$captured_field(name)", `$captured_field("",id)`, "$prefixed_kv_objects($.value)", `$prefixed_kv_objects($.value,"")`, `$prefixed_kv_objects($.value,"+","")`, "$json($.invalid)", "$input.", "$context."} {
		if _, _, err := ResolveExpression(expr, ctx); err == nil {
			t.Fatalf("malformed expression accepted: %s", expr)
		}
	}
	for _, expr := range []string{"$json($.missing)", "$list($.missing)", "$prefixed_values($.missing,+)", "$matching_prefix($.missing,+)", "$not_matching_prefix($.missing,+)", "$prefixed_kv_objects($.missing,+)", "$first($.missing)"} {
		if _, found, err := ResolveExpression(expr, ctx); err != nil || found {
			t.Fatalf("missing expression resolved: %s, %t, %v", expr, found, err)
		}
	}
}
