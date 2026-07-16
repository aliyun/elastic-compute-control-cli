package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/spec"
	"ecctl/pkg/waiter"
)

type Caller interface {
	Call(ctx context.Context, operation string, request map[string]any) (map[string]any, error)
}

var timeScale float64 = 1.0

func SetTimeScaleForTest(scale float64) func() {
	old := timeScale
	timeScale = scale
	return func() { timeScale = old }
}

func scaleDuration(d time.Duration) time.Duration {
	if timeScale == 1.0 {
		return d
	}
	return time.Duration(float64(d) * timeScale)
}

type Request struct {
	Action         string
	Input          map[string]any
	Context        map[string]any
	Timeout        time.Duration
	TokenGenerator TokenGenerator
}

type TokenGenerator func(prefix string, input map[string]any) string

type Result struct {
	Items        []map[string]any
	Item         map[string]any
	Extra        map[string]any
	Total        int
	HasTotal     bool
	NextToken    string
	RequestID    string
	Actions      []ecerrors.Action
	Capabilities []string
	Deleted      bool
	DryRun       bool
	ID           string
	Captures     map[string]CaptureResult
	Named        map[string]ProbeResult
}

type Executor struct {
	spec   spec.ResourceSpec
	caller Caller
}

func NewExecutor(resource spec.ResourceSpec, caller Caller) *Executor {
	return &Executor{spec: resource, caller: caller}
}

func (e *Executor) Execute(ctx context.Context, req Request) (Result, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	if operation, ok := e.spec.Operations[req.Action]; ok {
		if operation.Call.Probe != "" {
			return e.executeOperationCall(ctx, req, operation)
		}
		if len(operation.Workflow) > 0 {
			return e.executeOperationWorkflow(ctx, req, operation)
		}
	}
	return Result{}, ecerrors.Client("UnsupportedAction", fmt.Sprintf("action %q is not supported", req.Action))
}

type cachedProbeResult struct {
	name   string
	ids    []string
	result ProbeResult
}

type transitionRetryConfig struct {
	errors          []string
	initialInterval time.Duration
	maxInterval     time.Duration
	timeout         time.Duration
}

func (e *Executor) callTransition(ctx context.Context, transition spec.Transition, request map[string]any, execCtx ExecutionContext) (map[string]any, error) {
	retry, enabled, err := transitionRetryConfigFor(transition.Retry)
	if err != nil {
		return nil, err
	}
	if enabled && transition.Retry.When != "" {
		matched, err := conditionMatches(transition.Retry.When, execCtx)
		if err != nil {
			return nil, err
		}
		enabled = matched
	}
	if !enabled {
		return e.caller.Call(ctx, transition.Call, request)
	}

	retryCtx, cancel := context.WithTimeout(ctx, scaleDuration(retry.timeout))
	defer cancel()

	interval := retry.initialInterval
	var lastErr error
	for {
		response, err := e.caller.Call(retryCtx, transition.Call, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !isHiddenTransitionRetryError(err, retry.errors) {
			return nil, err
		}
		if retryCtx.Err() != nil {
			return nil, hiddenTransitionRetryTimeout(transition, lastErr)
		}

		timer := time.NewTimer(scaleDuration(interval))
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return nil, hiddenTransitionRetryTimeout(transition, lastErr)
		case <-timer.C:
		}
		interval *= 2
		if interval > retry.maxInterval {
			interval = retry.maxInterval
		}
	}
}

func transitionRetryConfigFor(retry spec.TransitionRetry) (transitionRetryConfig, bool, error) {
	if retry.Policy == "" && len(retry.Errors) == 0 {
		return transitionRetryConfig{}, false, nil
	}
	config := transitionRetryConfig{
		errors:          retry.Errors,
		initialInterval: 5 * time.Second,
		maxInterval:     40 * time.Second,
		timeout:         60 * time.Second,
	}
	var err error
	if retry.InitialInterval != "" {
		config.initialInterval, err = parseDuration(retry.InitialInterval)
		if err != nil {
			return transitionRetryConfig{}, false, err
		}
	}
	if retry.MaxInterval != "" {
		config.maxInterval, err = parseDuration(retry.MaxInterval)
		if err != nil {
			return transitionRetryConfig{}, false, err
		}
	}
	if retry.Timeout != "" {
		config.timeout, err = parseDuration(retry.Timeout)
		if err != nil {
			return transitionRetryConfig{}, false, err
		}
	}
	if config.initialInterval <= 0 {
		config.initialInterval = time.Second
	}
	if config.maxInterval <= 0 || config.maxInterval < config.initialInterval {
		config.maxInterval = config.initialInterval
	}
	if config.timeout <= 0 {
		config.timeout = config.initialInterval
	}
	return config, true, nil
}

func isHiddenTransitionRetryError(err error, codes []string) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	rawCode := ecerrors.ActionFromError("", err).Code
	for _, code := range codes {
		if code != "" && (strings.Contains(rawCode, code) || strings.Contains(message, code)) {
			return true
		}
	}
	return false
}

func hiddenTransitionRetryTimeout(transition spec.Transition, _ error) error {
	return ecerrors.Timeout("HiddenRetryTimeout", fmt.Sprintf("%s was not accepted before the hidden state grace period ended", transition.Call),
		ecerrors.WithCurrentState("hidden_state_grace_period"),
		ecerrors.WithExpectedStates("operation accepted"),
	)
}

func (e *Executor) workflowProbe(ctx context.Context, name string, execCtx ExecutionContext, ids []string, cached *cachedProbeResult) (ProbeResult, error) {
	if cached != nil && cached.name == name && equalStrings(cached.ids, ids) {
		return cached.result, nil
	}
	return e.runProbe(ctx, name, execCtx, ids)
}

func (e *Executor) wait(ctx context.Context, req Request, name string, execCtx ExecutionContext, ids []string) (ProbeResult, error) {
	waitSpec, ok := e.spec.Waiters[name]
	if !ok {
		return ProbeResult{}, ecerrors.Client("UnknownWaiter", fmt.Sprintf("waiter %q is not configured", name))
	}
	probeSpec, ok := e.spec.Probes[waitSpec.Probe]
	if !ok {
		return ProbeResult{}, ecerrors.Client("UnknownProbe", fmt.Sprintf("probe %q is not configured", waitSpec.Probe))
	}

	interval, err := parseDuration(waitSpec.Interval)
	if err != nil {
		return ProbeResult{}, err
	}
	timeout, err := parseDuration(waitSpec.Timeout)
	if err != nil {
		return ProbeResult{}, err
	}
	scaledInterval := scaleDuration(interval)
	scaledTimeout := scaleDuration(timeout)
	if req.Timeout > 0 {
		scaledTimeout = req.Timeout
	}

	var last ProbeResult
	_, err = waiter.Wait(ctx, waiter.Options{
		Target:        waitSpec.Target,
		FailureStates: waitSpec.Failure.States,
		Interval:      scaledInterval,
		Timeout:       scaledTimeout,
		MaxAttempts:   maxAttempts(scaledTimeout, scaledInterval),
		Probe: func(ctx context.Context) (waiter.Observation, error) {
			probeResult, err := e.runProbe(ctx, waitSpec.Probe, execCtx, ids)
			if err != nil {
				if waitSpec.Target == "absent" && isNotFoundError(err) {
					last = ProbeResult{}
					return waiter.Observation{State: "absent", Value: last}, nil
				}
				return waiter.Observation{}, err
			}
			last = probeResult
			return waiter.Observation{
				State: waiterState(waitSpec, probeSpec, probeResult, ids, execCtx),
				Value: probeResult,
			}, nil
		},
	})
	if err != nil {
		return ProbeResult{}, err
	}
	return last, nil
}

func isNotFoundError(err error) bool {
	var appErr *ecerrors.AppError
	return stderrors.As(err, &appErr) && appErr.Payload().Kind == string(ecerrors.CategoryNotFound)
}

func resolveActionIDs(expressions []string, execCtx ExecutionContext) ([]string, error) {
	var ids []string
	for _, expr := range expressions {
		value, ok, err := ResolveExpression(expr, execCtx)
		if err != nil {
			return nil, err
		}
		if !ok || isEmpty(value) {
			continue
		}
		switch typed := value.(type) {
		case string:
			ids = append(ids, typed)
		case []string:
			ids = append(ids, typed...)
		case []any:
			for _, item := range typed {
				id, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("action id expression %q included a non-string value", expr)
				}
				ids = append(ids, id)
			}
		default:
			return nil, fmt.Errorf("action id expression %q did not resolve to a string or string list", expr)
		}
	}
	return ids, nil
}

func shouldSkip(expr string, execCtx ExecutionContext) (bool, error) {
	if expr == "" {
		return false, nil
	}
	return conditionMatches(expr, execCtx)
}

func resolveCondition(expr string, execCtx ExecutionContext) (any, bool, error) {
	if name, arg, ok := conditionFunction(expr); ok {
		return resolveConditionFunction(name, arg, execCtx)
	}
	switch {
	case len(expr) > 0 && expr[0] == '$':
		return ResolveExpression(expr, execCtx)
	case len(expr) > len("input.") && expr[:len("input.")] == "input.":
		return ResolveExpression("$"+expr, execCtx)
	case len(expr) > len("context.") && expr[:len("context.")] == "context.":
		return ResolveExpression("$"+expr, execCtx)
	default:
		return nil, false, fmt.Errorf("unsupported condition expression %q", expr)
	}
}

func resolveConditionFunction(name, arg string, execCtx ExecutionContext) (any, bool, error) {
	if name == "starts_with" {
		return conditionStartsWith(arg, execCtx)
	}
	value, ok, err := resolveConditionArgument(arg, execCtx)
	if err != nil {
		return nil, false, err
	}
	switch name {
	case "specified":
		return ok, true, nil
	case "has":
		return ok && !isEmpty(value), true, nil
	case "single":
		return ok && conditionListLength(value) == 1, true, nil
	case "multiple":
		return ok && conditionListLength(value) > 1, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported condition expression %q", name+"("+arg+")")
	}
}

func conditionStartsWith(arg string, execCtx ExecutionContext) (any, bool, error) {
	args := splitExpressionArgs(arg)
	if len(args) != 2 {
		return nil, false, fmt.Errorf("condition expression %q expects two arguments", "starts_with("+arg+")")
	}
	value, ok, err := resolveConditionArgument(strings.TrimSpace(args[0]), execCtx)
	if err != nil || !ok || isEmpty(value) {
		return false, true, err
	}
	prefix := strings.Trim(strings.TrimSpace(args[1]), `"'`)
	if prefix == "" {
		return nil, false, fmt.Errorf("condition expression %q has empty prefix", "starts_with("+arg+")")
	}
	for _, item := range stringListValue(value) {
		if strings.HasPrefix(strings.TrimSpace(item), prefix) {
			return true, true, nil
		}
	}
	return false, true, nil
}

func resolveConditionArgument(arg string, execCtx ExecutionContext) (any, bool, error) {
	switch {
	case strings.HasPrefix(arg, "$"):
		return ResolveExpression(arg, execCtx)
	case strings.HasPrefix(arg, "input."), strings.HasPrefix(arg, "context."):
		return ResolveExpression("$"+arg, execCtx)
	default:
		return nil, false, fmt.Errorf("unsupported condition argument %q", arg)
	}
}

func conditionListLength(value any) int {
	switch typed := value.(type) {
	case []string:
		return len(typed)
	case []any:
		return len(typed)
	case string:
		if typed == "" {
			return 0
		}
		return 1
	default:
		if isEmpty(value) {
			return 0
		}
		return 1
	}
}

func (e *Executor) currentResourceIDs(execCtx ExecutionContext) []string {
	id := firstNonEmpty(
		stringFromMap(execCtx.Context, "id"),
		stringFromMap(execCtx.Input, "id"),
	)
	if id == "" && e.spec.Identity.Field == "name" {
		id = stringFromMap(execCtx.Input, "name")
	}
	if id == "" {
		return stringListValue(execCtx.Input["ids"])
	}
	return []string{id}
}

func boolFromMap(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func transitionToken(req Request, transition spec.Transition) string {
	if explicit := stringFromMap(req.Input, "idempotency_key"); explicit != "" {
		return explicit
	}
	generate := req.TokenGenerator
	if generate == nil {
		generate = defaultTokenGenerator
	}
	return generate(transition.Idempotency.Prefix, cloneMap(req.Input))
}

func defaultTokenGenerator(prefix string, _ map[string]any) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(raw[:])
	}
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func parseDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	return duration, nil
}

func maxAttempts(timeout, interval time.Duration) int {
	if timeout <= 0 || interval <= 0 {
		return 1
	}
	attempts := int(timeout/interval) + 1
	if attempts < 1 {
		return 1
	}
	return attempts
}

func waiterState(waitSpec spec.Waiter, probe spec.Probe, result ProbeResult, ids []string, execCtx ...ExecutionContext) string {
	if waitSpec.Match.Capture != "" {
		var ctx ExecutionContext
		if len(execCtx) > 0 {
			ctx = execCtx[0]
		}
		capture, ok := ctx.Captures[waitSpec.Match.Capture]
		if !ok || len(capture.Items) == 0 {
			return ""
		}
		matched := captureItemsMatched(result.Items, capture, waitSpec.Match.By)
		switch waitSpec.Target {
		case "absent":
			if !captureItemsAnyMatched(result.Items, capture, waitSpec.Match.By) {
				return "absent"
			}
		default:
			if matched {
				return waitSpec.Target
			}
		}
		return ""
	}
	if len(waitSpec.Match.Fields) > 0 || len(waitSpec.Match.Contains) > 0 || len(waitSpec.Match.Excludes) > 0 {
		var ctx ExecutionContext
		if len(execCtx) > 0 {
			ctx = execCtx[0]
		}
		if waiterRequestedFieldsMatched(waitSpec.Match, result.Items, ctx) {
			return waitSpec.Target
		}
		return ""
	}
	if len(ids) > 0 {
		switch waitSpec.Target {
		case "present":
			if probeResultHasAnyID(result, ids) {
				return "present"
			}
			return ""
		case "absent":
			if !probeResultHasAnyID(result, ids) {
				return "absent"
			}
			return ""
		}
	}
	if waitSpec.Target == "present" && len(result.Items) > 0 {
		return "present"
	}
	if len(result.Items) == 0 {
		if len(ids) > 0 && probe.Response.Absent.WhenEmptyForRequestedIDs {
			return "absent"
		}
		return ""
	}
	defaultState := stringFromMap(result.Items[0], "status")
	for _, pending := range waitSpec.Pending {
		value := stringFromMap(result.Items[0], pending.Field)
		if value != "" && containsString(pending.Values, value) {
			return value
		}
	}
	return defaultState
}

func waiterRequestedFieldsMatched(match spec.WaiterMatch, items []map[string]any, execCtx ExecutionContext) bool {
	if len(items) == 0 {
		return false
	}
	item := items[0]
	compared := false
	for field, expr := range match.Fields {
		want, ok, err := ResolveExpression(expr, execCtx)
		if err != nil || !ok || isEmpty(want) {
			continue
		}
		compared = true
		if !strings.EqualFold(fmt.Sprint(item[field]), fmt.Sprint(want)) {
			return false
		}
	}
	for field, expr := range match.Contains {
		want, ok, err := ResolveExpression(expr, execCtx)
		if err != nil || !ok || isEmpty(want) {
			continue
		}
		compared = true
		if !collectionContainsAll(item[field], want) {
			return false
		}
	}
	for field, expr := range match.Excludes {
		want, ok, err := ResolveExpression(expr, execCtx)
		if err != nil || !ok || isEmpty(want) {
			continue
		}
		compared = true
		if collectionContainsAny(item[field], want) {
			return false
		}
	}
	return compared
}

func collectionContainsAll(observed, expected any) bool {
	for _, want := range listValue(expected) {
		if !collectionContainsValue(observed, want) {
			return false
		}
	}
	return true
}

func collectionContainsAny(observed, expected any) bool {
	for _, want := range listValue(expected) {
		if collectionContainsValue(observed, want) {
			return true
		}
	}
	return false
}

func collectionContainsValue(collection, expected any) bool {
	for _, observed := range listValue(collection) {
		if strings.EqualFold(fmt.Sprint(observed), fmt.Sprint(expected)) {
			return true
		}
	}
	return false
}

func captureItemsMatched(items []map[string]any, capture CaptureResult, fields []string) bool {
	if len(capture.Items) == 0 {
		return false
	}
	if len(items) == 0 {
		return false
	}
	used := map[int]bool{}
	for _, want := range capture.Items {
		matched := false
		for index, item := range items {
			if used[index] || !captureItemMatches(item, want, fields) {
				continue
			}
			used[index] = true
			matched = true
			break
		}
		if !matched {
			return false
		}
	}
	return true
}

func captureItemsAnyMatched(items []map[string]any, capture CaptureResult, fields []string) bool {
	for _, want := range capture.Items {
		for _, item := range items {
			if captureItemMatches(item, want, fields) {
				return true
			}
		}
	}
	return false
}

func captureItemMatches(item map[string]any, want map[string]any, fields []string) bool {
	compared := false
	for _, field := range fields {
		wantValue, ok := want[field]
		if !ok || isEmpty(wantValue) {
			continue
		}
		compared = true
		if !strings.EqualFold(fmt.Sprint(item[field]), fmt.Sprint(wantValue)) {
			return false
		}
	}
	return compared
}

func probeResultHasAnyID(result ProbeResult, ids []string) bool {
	wanted := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return false
	}
	for _, item := range result.Items {
		if wanted[stringFromMap(item, "id")] {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func applyProbeResult(result *Result, probeResult ProbeResult) {
	applyWorkflowProbeResult(result, probeResult, false)
}

func applyWorkflowProbeResult(result *Result, probeResult ProbeResult, many bool) {
	result.Items = probeResult.Items
	result.Extra = cloneMap(probeResult.Extra)
	result.Total = probeResult.Total
	result.HasTotal = probeResult.HasTotal
	result.NextToken = probeResult.NextToken
	if result.RequestID == "" {
		result.RequestID = probeResult.RequestID
	}
	if many || len(probeResult.Items) == 0 {
		return
	}
	result.Item = probeResult.Items[0]
	if id := stringFromMap(result.Item, "id"); id != "" {
		result.ID = id
	}
}

func mergeProbeResult(result *Result, probeResult ProbeResult) {
	if probeResult.NextToken != "" {
		result.NextToken = probeResult.NextToken
	}
	mergeResultExtra(result, probeResult.Extra)
	if result.RequestID == "" {
		result.RequestID = probeResult.RequestID
	}
	if len(probeResult.Items) == 0 {
		return
	}
	if result.Item == nil {
		result.Item = map[string]any{}
	}
	for key, value := range probeResult.Items[0] {
		if !isEmpty(value) {
			result.Item[key] = value
		}
	}
	if id := stringFromMap(result.Item, "id"); id != "" {
		result.ID = id
	}
}

func appendWorkflowProbeResult(result *Result, probeResult ProbeResult) {
	if probeResult.NextToken != "" {
		result.NextToken = probeResult.NextToken
	}
	mergeResultExtra(result, probeResult.Extra)
	if result.RequestID == "" {
		result.RequestID = probeResult.RequestID
	}
	result.Items = append(result.Items, probeResult.Items...)
	if probeResult.HasTotal {
		result.Total += probeResult.Total
		result.HasTotal = true
	} else {
		result.Total = len(result.Items)
	}
}

func mergeResultExtra(result *Result, extra map[string]any) {
	if len(extra) == 0 {
		return
	}
	if result.Extra == nil {
		result.Extra = map[string]any{}
	}
	for key, value := range extra {
		if !isEmpty(value) {
			result.Extra[key] = value
		}
	}
}

func recordAction(actions *[]ecerrors.Action, action ecerrors.Action) {
	if action.ActionName == "" {
		return
	}
	last := len(*actions) - 1
	if last >= 0 && (*actions)[last].ActionName == action.ActionName {
		(*actions)[last] = action
		return
	}
	*actions = append(*actions, action)
}

func appendAction(actions []ecerrors.Action, action ecerrors.Action) []ecerrors.Action {
	result := append([]ecerrors.Action(nil), actions...)
	recordAction(&result, action)
	return result
}

// appendCapability appends a capability tag to the dedup'd capability list. It
// is idempotent — calling it multiple times with the same capability is safe
// and a single instance survives. Used by hooks (waiter, idempotency) to
// declare ecctl-side behaviour to agent callers via `ecctl_capabilities_used`.
func appendCapability(caps []string, cap string) []string {
	if cap == "" {
		return caps
	}
	for _, c := range caps {
		if c == cap {
			return caps
		}
	}
	return append(caps, cap)
}

func applyEmit(result *Result, emit any, execCtx ExecutionContext) error {
	switch typed := emit.(type) {
	case string:
		return applyNamedEmit(result, typed, execCtx)
	case map[string]any:
		return applyEmitMap(result, typed, execCtx)
	default:
		return nil
	}
}

func applyNamedEmit(result *Result, name string, execCtx ExecutionContext) error {
	switch name {
	case "", "vpcs_from_probe":
		return nil
	case "vpc_from_probe":
		if result.Item == nil && len(result.Items) > 0 {
			result.Item = result.Items[0]
		}
		if result.ID == "" && result.Item != nil {
			result.ID = stringFromMap(result.Item, "id")
		}
		return nil
	case "vpc_from_context":
		if result.Item == nil {
			result.Item = map[string]any{}
		}
		setMissingEmitField(result.Item, "id", firstNonEmpty(result.ID, stringFromMap(execCtx.Context, "id"), stringFromMap(execCtx.Input, "id")))
		setMissingEmitField(result.Item, "name", stringFromMap(execCtx.Input, "name"))
		setMissingEmitField(result.Item, "cidr", stringFromMap(execCtx.Input, "cidr"))
		setMissingEmitField(result.Item, "region", stringFromMap(execCtx.Context, "region"))
		if id := stringFromMap(result.Item, "id"); id != "" {
			result.ID = id
		}
		return nil
	default:
		return ecerrors.Client("UnsupportedEmit", fmt.Sprintf("emit %q is not supported", name))
	}
}

func setMissingEmitField(item map[string]any, key string, value any) {
	if _, ok := item[key]; ok || isEmpty(value) {
		return
	}
	item[key] = value
}

func applyEmitMap(result *Result, emit map[string]any, execCtx ExecutionContext) error {
	if deleted, ok := emit["deleted"].(bool); ok && deleted {
		result.Deleted = true
	}
	fields, ok := emit["fields"].(map[string]any)
	if !ok {
		return nil
	}
	if result.Item == nil {
		result.Item = map[string]any{}
	}
	for field, expr := range fields {
		value, ok, err := resolveEmitValue(expr, execCtx)
		if err != nil {
			return err
		}
		if !ok || isEmpty(value) {
			continue
		}
		result.Item[field] = value
	}
	if id := stringFromMap(result.Item, "id"); id != "" {
		result.ID = id
	}
	return nil
}

func resolveEmitValue(value any, execCtx ExecutionContext) (any, bool, error) {
	expr, ok := value.(string)
	if !ok {
		return value, true, nil
	}
	return ResolveExpression(expr, execCtx)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
