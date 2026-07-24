package engine

import (
	"context"
	"fmt"
	"strings"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	spechooks "github.com/aliyun/elastic-compute-control-cli/specs"
)

func (e *Executor) executeOperationCall(ctx context.Context, req Request, operation spec.Operation) (Result, error) {
	execCtx := ExecutionContext{
		Input:    cloneMap(req.Input),
		Context:  cloneMap(req.Context),
		Captures: map[string]CaptureResult{},
	}
	ids, err := resolveActionIDs(operation.Call.IDs, execCtx)
	if err != nil {
		return Result{}, err
	}
	probeResult, err := e.runProbe(ctx, operation.Call.Probe, execCtx, ids)
	if err != nil {
		return Result{}, err
	}
	if operation.Call.NotFound == "error" && len(probeResult.Items) == 0 {
		err := ecerrors.NotFound("NotFound", e.spec.Resource+" not found")
		return Result{}, ecerrors.WithActions(err, probeResult.Actions)
	}

	result := Result{
		Items:     probeResult.Items,
		Extra:     cloneMap(probeResult.Extra),
		Total:     probeResult.Total,
		HasTotal:  probeResult.HasTotal,
		NextToken: probeResult.NextToken,
		RequestID: probeResult.RequestID,
		Actions:   probeResult.Actions,
	}
	if !operation.Call.Many && len(probeResult.Items) > 0 {
		result.Item = probeResult.Items[0]
		result.ID = stringFromMap(result.Item, "id")
	}
	if operation.Emit != nil {
		if err := applyEmit(&result, operation.Emit, execCtx); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (e *Executor) executeOperationWorkflow(ctx context.Context, req Request, operation spec.Operation) (Result, error) {
	execCtx := ExecutionContext{
		Input:    cloneMap(req.Input),
		Context:  cloneMap(req.Context),
		Captures: map[string]CaptureResult{},
	}
	result := Result{}
	var cached *cachedProbeResult

	for _, step := range operation.Workflow {
		run, err := shouldRun(step.When, step.WhenAny, execCtx)
		if err != nil {
			return Result{}, err
		}
		if !run {
			continue
		}
		skip, err := shouldSkip(step.Unless, execCtx)
		if err != nil {
			return Result{}, err
		}
		if skip {
			continue
		}

		switch {
		case step.Binding != "":
			probeResult, err := e.runBinding(ctx, req, step, execCtx, &result)
			if err != nil {
				return Result{}, err
			}
			cached = probeResult
		case step.Wait != "":
			if result.DryRun {
				continue
			}
			ids, err := resolveActionIDs(step.IDs, execCtx)
			if err != nil {
				return Result{}, err
			}
			probeResult, err := e.wait(ctx, req, step.Wait, execCtx, ids)
			if err != nil {
				return Result{}, ecerrors.WithActions(err, append(result.Actions, actionsFromError(err, e.spec.Probes[e.spec.Waiters[step.Wait].Probe].API)...))
			}
			applyProbeResult(&result, probeResult)
			result.Actions = append(result.Actions, probeResult.Actions...)
			waitSpec := e.spec.Waiters[step.Wait]
			result.Capabilities = appendCapability(result.Capabilities, "auto_wait")
			cached = &cachedProbeResult{name: waitSpec.Probe, ids: ids, result: probeResult}
		case step.Probe != "":
			if result.DryRun {
				continue
			}
			ids, err := resolveActionIDs(step.IDs, execCtx)
			if err != nil {
				return Result{}, err
			}
			usedCache := cached != nil && cached.name == step.Probe && equalStrings(cached.ids, ids)
			probeResult, err := e.workflowProbe(ctx, step.Probe, execCtx, ids, cached)
			if err != nil {
				return Result{}, ecerrors.WithActions(err, append(result.Actions, actionsFromError(err, e.spec.Probes[step.Probe].API)...))
			}
			if step.NotFound == "error" && len(probeResult.Items) == 0 {
				err := ecerrors.NotFound("NotFound", e.spec.Resource+" not found")
				return Result{}, ecerrors.WithActions(err, append(result.Actions, probeResult.Actions...))
			}
			if !usedCache {
				result.Actions = append(result.Actions, probeResult.Actions...)
			}
			if step.As != "" {
				if result.Captures == nil {
					result.Captures = map[string]CaptureResult{}
				}
				result.Captures[step.As] = CaptureResult{Items: probeResult.Items}
				if execCtx.Captures == nil {
					execCtx.Captures = map[string]CaptureResult{}
				}
				execCtx.Captures[step.As] = CaptureResult{Items: probeResult.Items}
				if result.Named == nil {
					result.Named = map[string]ProbeResult{}
				}
				result.Named[step.As] = probeResult
				if len(probeResult.Items) > 0 {
					execCtx.Context[step.As] = probeResult.Items[0]
				}
			}
			if step.Merge {
				mergeProbeResult(&result, probeResult)
			} else if step.Append {
				appendWorkflowProbeResult(&result, probeResult)
			} else {
				applyWorkflowProbeResult(&result, probeResult, step.Many)
			}
			cached = &cachedProbeResult{name: step.Probe, ids: ids, result: probeResult}
		case step.Emit != nil:
			if result.DryRun {
				continue
			}
			if err := applyEmit(&result, step.Emit, execCtx); err != nil {
				return Result{}, err
			}
		}
	}
	return result, nil
}

func shouldRun(when string, whenAny []string, execCtx ExecutionContext) (bool, error) {
	if when == "" && len(whenAny) == 0 {
		return true, nil
	}
	if when != "" {
		matched, err := conditionMatches(when, execCtx)
		if err != nil || !matched {
			return matched, err
		}
	}
	if len(whenAny) == 0 {
		return true, nil
	}
	for _, expr := range whenAny {
		matched, err := conditionMatches(expr, execCtx)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func ShouldRun(when string, whenAny []string, execCtx ExecutionContext) (bool, error) {
	return shouldRun(when, whenAny, execCtx)
}

func ShouldSkip(expr string, execCtx ExecutionContext) (bool, error) {
	return shouldSkip(expr, execCtx)
}

func conditionMatches(expr string, execCtx ExecutionContext) (bool, error) {
	expr = strings.TrimSpace(expr)
	parts := strings.Split(expr, "||")
	if len(parts) > 1 {
		for _, part := range parts {
			matched, err := conditionMatches(strings.TrimSpace(part), execCtx)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	}
	parts = strings.Split(expr, "&&")
	if len(parts) > 1 {
		for _, part := range parts {
			matched, err := conditionMatches(strings.TrimSpace(part), execCtx)
			if err != nil || !matched {
				return matched, err
			}
		}
		return true, nil
	}
	if left, right, ok := strings.Cut(expr, "=="); ok {
		return conditionValuesEqual(left, right, execCtx)
	}
	if left, right, ok := strings.Cut(expr, "!="); ok {
		matched, err := conditionValuesEqual(left, right, execCtx)
		return !matched, err
	}
	if strings.HasPrefix(expr, "!") {
		matched, err := conditionMatches(strings.TrimSpace(strings.TrimPrefix(expr, "!")), execCtx)
		return !matched, err
	}
	value, ok, err := resolveCondition(expr, execCtx)
	if err != nil || !ok {
		return false, err
	}
	if enabled, ok := value.(bool); ok {
		return enabled, nil
	}
	return !isEmpty(value), nil
}

func conditionValuesEqual(left string, right string, execCtx ExecutionContext) (bool, error) {
	leftValue, leftOK, err := resolveConditionArgument(strings.TrimSpace(left), execCtx)
	if err != nil {
		return false, err
	}
	if !leftOK {
		leftValue = ""
	}
	rightValue, rightOK, err := conditionCompareValue(strings.TrimSpace(right), execCtx)
	if err != nil {
		return false, err
	}
	if !rightOK {
		rightValue = ""
	}
	return strings.EqualFold(fmt.Sprint(leftValue), fmt.Sprint(rightValue)), nil
}

func conditionCompareValue(raw string, execCtx ExecutionContext) (any, bool, error) {
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") && len(raw) >= 2 {
		return strings.Trim(raw, "\""), true, nil
	}
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
		return strings.Trim(raw, "'"), true, nil
	}
	switch raw {
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	}
	if strings.HasPrefix(raw, "$") || strings.HasPrefix(raw, "input.") || strings.HasPrefix(raw, "context.") {
		return resolveConditionArgument(raw, execCtx)
	}
	return raw, true, nil
}

func conditionFunction(expr string) (string, string, bool) {
	open := strings.IndexByte(expr, '(')
	if open <= 0 || !strings.HasSuffix(expr, ")") {
		return "", "", false
	}
	name := strings.TrimSpace(expr[:open])
	arg := strings.TrimSpace(expr[open+1 : len(expr)-1])
	if name == "" || arg == "" || strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	return name, arg, true
}

func (e *Executor) runBinding(ctx context.Context, req Request, step spec.WorkflowStep, execCtx ExecutionContext, result *Result) (*cachedProbeResult, error) {
	binding, ok := e.spec.Bindings[step.Binding]
	if !ok {
		return nil, ecerrors.Client("UnknownBinding", fmt.Sprintf("binding %q is not configured", step.Binding))
	}
	if binding.Each != "" {
		items, err := bindingEachItems(binding.Each, execCtx)
		if err != nil {
			return nil, err
		}
		var capturedItems []map[string]any
		if captureName, captureFields, ok := parseCaptureSpec(binding.Capture); ok {
			capturedItems, err = captureItems(items, captureFields, execCtx)
			if err != nil {
				return nil, err
			}
			recordCapture(result, execCtx, captureName, CaptureResult{Items: capturedItems})
		}
		var cached *cachedProbeResult
		for index, item := range items {
			itemCached, err := e.runSingleBinding(ctx, req, step, binding, withCurrent(execCtx, item), result)
			if err != nil {
				return nil, err
			}
			if binding.IDFrom != "" && index < len(capturedItems) && result.ID != "" {
				capturedItems[index]["id"] = result.ID
			}
			if itemCached != nil {
				cached = itemCached
			}
		}
		return cached, nil
	}
	return e.runSingleBinding(ctx, req, step, binding, execCtx, result)
}

func bindingEachItems(expr string, execCtx ExecutionContext) ([]any, error) {
	value, ok, err := ResolveExpression(expr, execCtx)
	if err != nil || !ok || isEmpty(value) {
		return nil, err
	}
	return listValue(value), nil
}

func (e *Executor) runSingleBinding(ctx context.Context, req Request, step spec.WorkflowStep, binding spec.Binding, execCtx ExecutionContext, result *Result) (*cachedProbeResult, error) {
	request, captures, err := ResolveResourceBindingRequest(e.spec, binding, execCtx)
	if err != nil {
		return nil, err
	}
	if err := requireAnyBindingInput(binding, request, execCtx); err != nil {
		return nil, err
	}
	if err := requireAllBindingInput(binding, request, execCtx); err != nil {
		return nil, err
	}
	for name, capture := range captures {
		recordCapture(result, execCtx, name, capture)
	}

	transition := spec.Transition{
		Call:          binding.API,
		Request:       binding.Request,
		Idempotency:   binding.Idempotency,
		Retry:         binding.Retry,
		IDFrom:        binding.IDFrom,
		RequestIDFrom: binding.RequestIDFrom,
		Wait:          binding.Wait,
	}
	if transition.Idempotency.Field != "" {
		if _, ok := request[transition.Idempotency.Field]; !ok {
			request[transition.Idempotency.Field] = transitionToken(req, transition)
		}
	}
	beforeHookRequest := cloneMap(request)
	request, err = e.applyBeforeBindingHooks(ctx, binding, request)
	if err != nil {
		return nil, err
	}
	response, err := e.callTransition(ctx, transition, request, execCtx)
	if err != nil {
		hookRequest := afterErrorHookRequest(beforeHookRequest, request)
		err = e.applyAfterErrorBindingHooks(ctx, binding, hookRequest, err)
		return nil, ecerrors.WithActions(err, append(result.Actions, actionsFromError(err, transition.Call)...))
	}
	responseCheck := checkBindingResponse(binding, response, execCtx)
	if responseCheck.err != nil {
		err = responseCheck.err
		actions := responseCheck.actions
		if len(responseCheck.successful.Items) > 0 && transition.Wait != "" {
			skipWait, skipErr := shouldSkip(step.WaitUnless, execCtx)
			if skipErr != nil {
				return nil, skipErr
			}
			if !skipWait {
				reconcileCtx := execCtx
				reconcileCtx.Captures = cloneCaptureResults(execCtx.Captures)
				reconcileCtx.Captures[binding.Response.Match.Capture] = responseCheck.successful
				ids := e.currentResourceIDs(reconcileCtx)
				probeResult, waitErr := e.wait(ctx, req, transition.Wait, reconcileCtx, ids)
				waitSpec := e.spec.Waiters[transition.Wait]
				probeSpec := e.spec.Probes[waitSpec.Probe]
				if waitErr != nil {
					actions = append(actions, actionsFromError(waitErr, probeSpec.API)...)
					err = ecerrors.WithDetails(err, ecerrors.WithDetail("provider-reported successful resources could not be reconciled: "+waitErr.Error()))
				} else {
					actions = append(actions, probeResult.Actions...)
				}
			}
		}
		err = e.applyAfterErrorBindingHooks(ctx, binding, request, err)
		if len(actions) == 0 {
			actions = []ecerrors.Action{ecerrors.ActionFromError(transition.Call, err)}
		}
		return nil, ecerrors.WithActions(err, append(result.Actions, actions...))
	}
	requestID := ExtractString(response, transition.RequestIDFrom)
	recordAction(&result.Actions, ecerrors.Action{RequestID: requestID, ActionName: transition.Call})
	if requestID != "" {
		result.RequestID = requestID
	}
	if boolFromMap(response, "DryRun") {
		result.DryRun = true
		return nil, nil
	}
	if transition.IDFrom != "" {
		id := ExtractString(response, transition.IDFrom)
		if id == "" {
			return nil, ecerrors.Client("MissingBindingID", fmt.Sprintf("binding %q response did not include id at %s", step.Binding, transition.IDFrom))
		}
		result.ID = id
		execCtx.Context["id"] = id
	}
	for name, path := range binding.ContextFrom {
		value, ok := ExtractPath(response, path)
		if ok && !isEmpty(value) {
			execCtx.Context[name] = value
		}
	}

	skipWait, err := shouldSkip(step.WaitUnless, execCtx)
	if err != nil {
		return nil, err
	}
	if transition.Wait == "" || skipWait {
		return nil, nil
	}
	ids := e.currentResourceIDs(execCtx)
	probeResult, err := e.wait(ctx, req, transition.Wait, execCtx, ids)
	if err != nil {
		return nil, ecerrors.WithActions(err, append(result.Actions, actionsFromError(err, e.spec.Probes[e.spec.Waiters[transition.Wait].Probe].API)...))
	}
	applyProbeResult(result, probeResult)
	waitSpec := e.spec.Waiters[transition.Wait]
	result.Actions = append(result.Actions, probeResult.Actions...)
	result.Capabilities = appendCapability(result.Capabilities, "auto_wait")
	return &cachedProbeResult{name: waitSpec.Probe, ids: ids, result: probeResult}, nil
}

type bindingResponseCheck struct {
	err        error
	actions    []ecerrors.Action
	successful CaptureResult
}

func checkBindingResponse(binding spec.Binding, response map[string]any, execCtx ExecutionContext) bindingResponseCheck {
	validation := binding.Response
	if validation.Items == "" {
		return bindingResponseCheck{}
	}
	rawItems, ok := ExtractPath(response, validation.Items)
	if !ok {
		return bindingResponseCheck{err: invalidBindingResponseError(binding, response, fmt.Sprintf("response did not include items at %s", validation.Items))}
	}
	items := listValue(rawItems)
	if len(items) == 0 {
		return bindingResponseCheck{err: invalidBindingResponseError(binding, response, fmt.Sprintf("response included no items at %s", validation.Items))}
	}
	objects := make([]map[string]any, 0, len(items))
	statuses := make([]string, 0, len(items))
	var matchErr error
	for index, rawItem := range items {
		item, ok := objectValue(rawItem)
		if !ok {
			if matchErr == nil {
				matchErr = invalidBindingResponseError(binding, response, fmt.Sprintf("response item %d at %s is not an object", index, validation.Items))
			}
			continue
		}
		status := ExtractString(item, validation.Status)
		if status == "" {
			if matchErr == nil {
				matchErr = invalidBindingResponseError(binding, response, fmt.Sprintf("response item %d did not include status at %s", index, validation.Status))
			}
			continue
		}
		objects = append(objects, item)
		statuses = append(statuses, status)
	}

	var matchedCaptures []map[string]any
	var matchedItems []map[string]any
	if validation.Match.Capture != "" {
		capture, ok := execCtx.Captures[validation.Match.Capture]
		if !ok {
			return bindingResponseCheck{err: invalidBindingResponseError(binding, response, fmt.Sprintf("response match capture %q was not recorded", validation.Match.Capture))}
		}
		matchedItems = make([]map[string]any, 0, len(objects))
		for _, item := range objects {
			matched := map[string]any{}
			for field, path := range validation.Match.Fields {
				value, ok := ExtractPath(item, path)
				if ok && !isEmpty(value) {
					matched[field] = value
				}
			}
			matchedItems = append(matchedItems, matched)
		}
		matchedCaptures, ok = matchCaptureItems(matchedItems, capture, validation.Match.By)
		if !ok && matchErr == nil {
			matchErr = invalidBindingResponseError(binding, response,
				fmt.Sprintf("response items at %s did not match requested capture %q one-to-one", validation.Items, validation.Match.Capture))
		}
	}

	check := bindingResponseCheck{}
	var firstFailureCode, firstFailureMessage, firstFailureRequestID string
	for index, item := range objects {
		status := statuses[index]
		succeeded := bindingResponseSucceeded(status, validation.Success)
		code := ExtractString(item, validation.ErrorCode)
		message := ExtractString(item, validation.ErrorMessage)
		requestID := ExtractString(item, validation.RequestID)
		if requestID == "" {
			requestID = ExtractString(response, binding.RequestIDFrom)
		}
		identity := ""
		if index < len(matchedItems) {
			identity = bindingResponseIdentity(matchedItems[index], validation.Match.By)
		}
		if succeeded {
			matched := validation.Match.Capture == "" ||
				(index < len(matchedCaptures) && matchedCaptures[index] != nil)
			if matched {
				check.actions = append(check.actions, ecerrors.Action{
					RequestID:  requestID,
					ActionName: binding.API,
					Code:       status,
					Message:    bindingResponseOutcomeMessage(identity, "status "+status),
				})
			}
			if validation.Match.Capture != "" && matched {
				check.successful.Items = append(check.successful.Items, matchedCaptures[index])
			}
			continue
		}
		if code == "" {
			code = status
		}
		if message == "" {
			message = fmt.Sprintf("%s returned status %s", binding.API, status)
		}
		check.actions = append(check.actions, ecerrors.Action{
			RequestID:  requestID,
			ActionName: binding.API,
			Code:       code,
			Message:    bindingResponseOutcomeMessage(identity, message),
		})
		if firstFailureCode == "" {
			firstFailureCode = code
			firstFailureMessage = message
			firstFailureRequestID = requestID
		}
	}
	if matchErr != nil {
		check.err = matchErr
		check.actions = append(check.actions, ecerrors.ActionFromError(binding.API, matchErr))
	} else if firstFailureCode != "" {
		check.err = ecerrors.Service("CloudAPIError", firstFailureMessage, false,
			ecerrors.WithRequestID(firstFailureRequestID),
			ecerrors.WithRawCause(firstFailureCode, firstFailureMessage),
		)
	}
	if check.err == nil {
		check.actions = nil
		check.successful = CaptureResult{}
	}
	return check
}

func matchCaptureItems(items []map[string]any, capture CaptureResult, fields []string) ([]map[string]any, bool) {
	used := map[int]bool{}
	matched := make([]map[string]any, len(items))
	for itemIndex, item := range items {
		for index, want := range capture.Items {
			if used[index] || !captureItemMatches(item, want, fields) {
				continue
			}
			used[index] = true
			matched[itemIndex] = want
			break
		}
	}
	return matched, len(items) > 0 && len(items) == len(capture.Items) && len(used) == len(capture.Items)
}

func bindingResponseIdentity(item map[string]any, fields []string) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if value, ok := item[field]; ok && !isEmpty(value) {
			parts = append(parts, field+"="+fmt.Sprint(value))
		}
	}
	return strings.Join(parts, ",")
}

func bindingResponseOutcomeMessage(identity, message string) string {
	if identity == "" {
		return message
	}
	return identity + ": " + message
}

func cloneCaptureResults(captures map[string]CaptureResult) map[string]CaptureResult {
	cloned := make(map[string]CaptureResult, len(captures))
	for name, capture := range captures {
		cloned[name] = capture
	}
	return cloned
}

func bindingResponseSucceeded(status string, success []string) bool {
	for _, expected := range success {
		if strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(expected)) {
			return true
		}
	}
	return false
}

func invalidBindingResponseError(binding spec.Binding, response map[string]any, message string) error {
	requestID := ExtractString(response, binding.RequestIDFrom)
	return ecerrors.Service("CloudAPIError", message, false,
		ecerrors.WithRequestID(requestID),
		ecerrors.WithRawCause("InvalidResponse", message),
	)
}

func recordCapture(result *Result, execCtx ExecutionContext, name string, capture CaptureResult) {
	if result.Captures == nil {
		result.Captures = map[string]CaptureResult{}
	}
	result.Captures[name] = capture
	if execCtx.Captures == nil {
		execCtx.Captures = map[string]CaptureResult{}
	}
	execCtx.Captures[name] = capture
}

func (e *Executor) applyBeforeBindingHooks(ctx context.Context, binding spec.Binding, request map[string]any) (map[string]any, error) {
	for _, name := range binding.Hooks.Before {
		hook, ok := spechooks.BeforeOperationHook(e.spec.Product, e.spec.Resource, name)
		if !ok {
			return nil, ecerrors.Client("UnknownHook", fmt.Sprintf("before hook %q is not registered", name))
		}
		resolved, err := hook(ctx, operationHookCaller{caller: e.caller}, request)
		if err != nil {
			return nil, err
		}
		request = resolved
	}
	return request, nil
}

func (e *Executor) applyAfterErrorBindingHooks(ctx context.Context, binding spec.Binding, request map[string]any, err error) error {
	for _, name := range binding.Hooks.AfterError {
		hook, ok := spechooks.AfterOperationErrorHook(e.spec.Product, e.spec.Resource, name)
		if !ok {
			return ecerrors.Client("UnknownHook", fmt.Sprintf("after_error hook %q is not registered", name))
		}
		err = hook(ctx, operationHookCaller{caller: e.caller}, request, err)
	}
	return err
}

func afterErrorHookRequest(before, resolved map[string]any) map[string]any {
	request := cloneMap(resolved)
	for key, value := range before {
		if _, ok := request[key]; !ok {
			request[key] = value
		}
	}
	return request
}

type operationHookCaller struct {
	caller Caller
}

func (c operationHookCaller) CallRaw(ctx context.Context, operation string, request map[string]any) (map[string]any, error) {
	if raw, ok := c.caller.(interface {
		CallRaw(context.Context, string, map[string]any) (map[string]any, error)
	}); ok {
		return raw.CallRaw(ctx, operation, request)
	}
	return c.caller.Call(ctx, operation, request)
}

func requireAnyBindingInput(binding spec.Binding, request map[string]any, execCtx ExecutionContext) error {
	if len(binding.RequireAny) == 0 {
		return nil
	}
	for _, requirement := range binding.RequireAny {
		switch {
		case requirement.Request != "":
			if requestHasPrefix(request, requirement.Request) {
				return nil
			}
		case requirement.Raw != "":
			if expressionHasValue(requirement.Raw, execCtx) {
				return nil
			}
		case requirement.Each != "":
			if expressionHasValue(requirement.Each, execCtx) {
				return nil
			}
		}
	}
	return ecerrors.Client("MissingParameter", "missing required request input")
}

func requireAllBindingInput(binding spec.Binding, request map[string]any, execCtx ExecutionContext) error {
	for _, requirement := range binding.RequireAll {
		if requirementMatches(requirement, request, execCtx) {
			continue
		}
		return ecerrors.Client("MissingParameter", "missing required request input")
	}
	return nil
}

func requirementMatches(requirement spec.Requirement, request map[string]any, execCtx ExecutionContext) bool {
	switch {
	case requirement.Request != "":
		return requestHasPrefix(request, requirement.Request)
	case requirement.Raw != "":
		return expressionHasValue(requirement.Raw, execCtx)
	case requirement.Each != "":
		return expressionHasValue(requirement.Each, execCtx)
	default:
		return false
	}
}

func requestHasPrefix(request map[string]any, prefix string) bool {
	for key, value := range request {
		if (key == prefix || strings.HasPrefix(key, prefix+".")) && !isEmpty(value) {
			return true
		}
	}
	return false
}

func expressionHasValue(expr string, execCtx ExecutionContext) bool {
	value, ok, err := ResolveExpression(expr, execCtx)
	return err == nil && ok && !isEmpty(value)
}
