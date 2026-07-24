package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ecs"
)

func TestResolveMappingSkipsEmptyValues(t *testing.T) {
	ctx := ExecutionContext{
		Input: map[string]any{
			"id":     "",
			"limit":  50,
			"filter": []string{"name=prod"},
		},
		Context: map[string]any{"region": "cn-beijing"},
	}
	got, err := ResolveRequest(map[string]string{
		"RegionId":   "$context.region",
		"VpcId":      "$input.id",
		"PageSize":   "$input.limit",
		"FilterExpr": "$input.filter",
	}, ctx)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	want := map[string]any{
		"RegionId":   "cn-beijing",
		"PageSize":   50,
		"FilterExpr": []string{"name=prod"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestCurrentResourceIDsUsesNameOnlyForNameIdentity(t *testing.T) {
	idResource := NewExecutor(spec.ResourceSpec{Identity: spec.Identity{Field: "id"}}, nil)
	if got := idResource.currentResourceIDs(ExecutionContext{Input: map[string]any{"name": "app.conf"}}); len(got) != 0 {
		t.Fatalf("id identity used input.name as resource id: %#v", got)
	}

	nameResource := NewExecutor(spec.ResourceSpec{Identity: spec.Identity{Field: "name"}}, nil)
	got := nameResource.currentResourceIDs(ExecutionContext{Input: map[string]any{"name": "my-policy"}})
	if !reflect.DeepEqual(got, []string{"my-policy"}) {
		t.Fatalf("name identity ids = %#v, want my-policy", got)
	}
}

func TestResolveExpressionRejectsUnknownDollarExpression(t *testing.T) {
	ctx := ExecutionContext{
		Input:   map[string]any{"id": "vpc-1"},
		Context: map[string]any{"region": "cn-beijing"},
	}
	if _, _, err := ResolveExpression("$inputs.id", ctx); err == nil {
		t.Fatal("expected error for unknown expression")
	}
	if _, err := ResolveRequest(map[string]string{"VpcId": "$inputs.id"}, ctx); err == nil {
		t.Fatal("expected request resolution error")
	}
}

func TestResolveExpressionPrefixedValuesSelectsMatchingPrefix(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"private_ip": []string{"+10.0.0.10", "-10.0.0.11", "+", ""},
	}}

	value, ok, err := ResolveExpression("$prefixed_values($.private_ip,+)", ctx)
	if err != nil {
		t.Fatalf("ResolveExpression: %v", err)
	}
	if !ok {
		t.Fatal("prefixed_values did not resolve")
	}
	if got, want := value, []string{"10.0.0.10"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prefixed_values = %#v, want %#v", got, want)
	}
}

func TestResolveExpressionMatchingPrefixKeepsOriginalValues(t *testing.T) {
	ctx := ExecutionContext{
		Input: map[string]any{
			"targets": []string{"lt-id", "web-lt", "lt-name"},
		},
		Captures: map[string]CaptureResult{
			"id_hits": {Items: []map[string]any{{"id": "lt-id"}}},
		},
	}

	value, ok, err := ResolveExpression(`$matching_prefix($.targets,"lt-")`, ctx)
	if err != nil {
		t.Fatalf("ResolveExpression matching_prefix: %v", err)
	}
	if !ok {
		t.Fatal("matching_prefix did not resolve")
	}
	if got, want := value, []string{"lt-id", "lt-name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matching_prefix = %#v, want %#v", got, want)
	}

	value, ok, err = ResolveExpression(`$not_matching_prefix($.targets,"lt-")`, ctx)
	if err != nil {
		t.Fatalf("ResolveExpression not_matching_prefix: %v", err)
	}
	if !ok {
		t.Fatal("not_matching_prefix did not resolve")
	}
	if got, want := value, []string{"web-lt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("not_matching_prefix = %#v, want %#v", got, want)
	}

	value, ok, err = ResolveExpression(`$except($matching_prefix($.targets,"lt-"),$captured_field(id_hits,id))`, ctx)
	if err != nil {
		t.Fatalf("ResolveExpression except nested: %v", err)
	}
	if !ok {
		t.Fatal("except did not resolve")
	}
	if got, want := value, []string{"lt-name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("except nested = %#v, want %#v", got, want)
	}
}

func TestResolveExpressionPrefixedKeyValueObjectsParsesInlineObjects(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"entries": []string{"+cidr=10.0.1.0/24,description=app", "-cidr=10.0.0.0/24", "+10.0.2.0/24"},
	}}

	value, ok, err := ResolveExpression(`$prefixed_kv_objects($.entries,"+")`, ctx)
	if err != nil {
		t.Fatalf("ResolveExpression prefixed_kv_objects: %v", err)
	}
	if !ok {
		t.Fatal("prefixed_kv_objects did not resolve")
	}
	got, ok := value.([]any)
	if !ok || len(got) != 2 {
		t.Fatalf("prefixed_kv_objects = %#v", value)
	}
	first, _ := got[0].(map[string]any)
	second, _ := got[1].(map[string]any)
	if first["cidr"] != "10.0.1.0/24" || first["description"] != "app" || second["cidr"] != "10.0.2.0/24" {
		t.Fatalf("prefixed_kv_objects parsed %#v", got)
	}
}

func TestResolveExpressionPrefixedKeyValueObjectsUsesConfiguredDefaultKey(t *testing.T) {
	ctx := ExecutionContext{Input: map[string]any{
		"entries": []string{"+443/443", "-port-range=80/80"},
	}}

	value, ok, err := ResolveExpression(`$prefixed_kv_objects($.entries,"+","port_range")`, ctx)
	if err != nil {
		t.Fatalf("ResolveExpression prefixed_kv_objects default key: %v", err)
	}
	if !ok {
		t.Fatal("prefixed_kv_objects did not resolve")
	}
	got, ok := value.([]any)
	if !ok || len(got) != 1 {
		t.Fatalf("prefixed_kv_objects = %#v", value)
	}
	first, _ := got[0].(map[string]any)
	if first["port_range"] != "443/443" {
		t.Fatalf("prefixed_kv_objects default key parsed %#v", got)
	}
}

func TestExtractPathHandlesNestedMapsAndSlices(t *testing.T) {
	response := map[string]any{
		"Vpcs": map[string]any{
			"Vpc": []any{
				map[string]any{"VpcId": "vpc-1", "Status": "Available"},
			},
		},
	}
	value, ok := ExtractPath(response, "$.Vpcs.Vpc")
	if !ok {
		t.Fatal("path not found")
	}
	items, ok := value.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", value)
	}
	id, ok := ExtractPath(items[0], "$.VpcId")
	if !ok || id != "vpc-1" {
		t.Fatalf("id = %#v ok=%v", id, ok)
	}
}

func TestExtractStringReturnsFirstStringFromArrayValue(t *testing.T) {
	response := map[string]any{
		"InstanceIdSets": map[string]any{
			"InstanceIdSet": []any{"i-123", "i-456"},
		},
	}

	if got := ExtractString(response, "$.InstanceIdSets.InstanceIdSet"); got != "i-123" {
		t.Fatalf("ExtractString = %q, want first instance ID", got)
	}
}

func TestExtractPathRejectsMalformedPaths(t *testing.T) {
	response := map[string]any{
		"": "empty-key",
		"foo": map[string]any{
			"": map[string]any{"bar": "empty-segment"},
		},
		"Vpcs": map[string]any{
			"Vpc": []any{
				map[string]any{"VpcId": "vpc-1"},
			},
		},
	}
	for _, path := range []string{
		"$.",
		"$.foo..bar",
		"$.Vpcs.Vpc[0].VpcId",
	} {
		if value, ok := ExtractPath(response, path); ok {
			t.Fatalf("ExtractPath(%q) = %#v, true; want false", path, value)
		}
	}
}

func TestMapProbeResponseNormalizesNestedOpenAPIOutput(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ecs",
		Resource: "instance",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Items: "$.Instances.Instance",
			Fields: map[string]spec.ProbeField{
				"id":                 {Path: "$.InstanceId"},
				"network_interfaces": {Path: "$.NetworkInterfaces.NetworkInterface"},
				"metadata_options":   {Path: "$.MetadataOptions"},
				"private_ips":        {Path: "$.PrivateIpAddress"},
			},
		},
	}
	response := map[string]any{
		"Instances": map[string]any{"Instance": []any{map[string]any{
			"InstanceId": "i-123",
			"NetworkInterfaces": map[string]any{"NetworkInterface": []any{
				map[string]any{
					"NetworkInterfaceId": "eni-123",
					"MacAddress":         "00:16:3e:00:00:01",
					"PrivateIpSets": map[string]any{"PrivateIpSet": []any{
						map[string]any{"PrivateIpAddress": "10.0.0.2", "Primary": true},
					}},
				},
			}},
			"MetadataOptions":  map[string]any{"HttpEndpoint": "enabled", "HttpTokens": "required"},
			"PrivateIpAddress": map[string]any{"IpAddress": []any{"10.0.0.2"}},
		}}},
	}

	result := mapProbeResponse(resource, probe, response)
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v", result.Items)
	}
	item := result.Items[0]
	if containsPascalCaseKey(item) {
		t.Fatalf("output still contains PascalCase keys: %#v", item)
	}
	interfaces, _ := item["network_interfaces"].([]any)
	if len(interfaces) != 1 {
		t.Fatalf("network_interfaces = %#v", item["network_interfaces"])
	}
	eni, _ := interfaces[0].(map[string]any)
	if eni["network_interface_id"] != "eni-123" || eni["mac_address"] != "00:16:3e:00:00:01" {
		t.Fatalf("network interface = %#v", eni)
	}
	privateSets, _ := eni["private_ip_sets"].([]any)
	if len(privateSets) != 1 {
		t.Fatalf("private_ip_sets = %#v", eni["private_ip_sets"])
	}
	metadata, _ := item["metadata_options"].(map[string]any)
	if metadata["http_endpoint"] != "enabled" || metadata["http_tokens"] != "required" {
		t.Fatalf("metadata_options = %#v", metadata)
	}
	privateIPs, _ := item["private_ips"].([]any)
	if len(privateIPs) != 1 || privateIPs[0] != "10.0.0.2" {
		t.Fatalf("private_ips = %#v", item["private_ips"])
	}
}

func TestMapProbeResponseRecordsWhetherTotalIsDeclared(t *testing.T) {
	withTotal := mapProbeResponse(spec.ResourceSpec{}, spec.Probe{
		Response: spec.ProbeResponse{Total: "$.TotalCount"},
	}, map[string]any{"TotalCount": 0})
	if !withTotal.HasTotal {
		t.Fatalf("declared zero total must be marked meaningful: %#v", withTotal)
	}

	withoutTotal := mapProbeResponse(spec.ResourceSpec{}, spec.Probe{}, map[string]any{"TotalCount": 7})
	if withoutTotal.HasTotal {
		t.Fatalf("undeclared total must not be marked meaningful: %#v", withoutTotal)
	}
}

func TestMapProbeResponseFallsBackToWrappedTopLevelArrayItems(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ack",
		Resource: "permission",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Items: "$.body",
			ID:    "$.resource_id",
			Fields: map[string]spec.ProbeField{
				"resource_id": {Path: "$.resource_id"},
				"role_name":   {Path: "$.role_name"},
			},
		},
	}
	response := map[string]any{
		"__ecctl_top_level_array_response": true,
		"items": []any{
			map[string]any{"resource_id": "c-123", "role_name": "dev"},
		},
	}

	result := mapProbeResponse(resource, probe, response)
	if len(result.Items) != 1 || result.Items[0]["resource_id"] != "c-123" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMapProbeResponseDoesNotFallbackToItemsForRegularObjectResponses(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ack",
		Resource: "permission",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Items: "$.body",
			ID:    "$.resource_id",
			Fields: map[string]spec.ProbeField{
				"resource_id": {Path: "$.resource_id"},
			},
		},
	}
	response := map[string]any{
		"items": []any{
			map[string]any{"resource_id": "wrong-path"},
		},
	}

	result := mapProbeResponse(resource, probe, response)
	if len(result.Items) != 0 {
		t.Fatalf("result = %#v, want no fallback for regular object response", result)
	}
}

func TestMapProbeResponseUsesFirstWrappedArrayItemForItemPath(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ack",
		Resource: "template",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Item: "$.items",
			ID:   "$.id",
			Fields: map[string]spec.ProbeField{
				"id":   {Path: "$.id"},
				"name": {Path: "$.name"},
			},
		},
	}
	response := map[string]any{
		"__ecctl_top_level_array_response": true,
		"items": []any{
			map[string]any{"id": "tpl-1", "name": "web"},
		},
	}

	result := mapProbeResponse(resource, probe, response)
	if len(result.Items) != 1 || result.Items[0]["id"] != "tpl-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMapProbeResponseTreatsMissingItemsPathAsWholeItem(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ack",
		Resource: "template",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Item: "$.items",
			ID:   "$.id",
			Fields: map[string]spec.ProbeField{
				"id":   {Path: "$.id"},
				"name": {Path: "$.name"},
			},
		},
	}

	result := mapProbeResponse(resource, probe, map[string]any{"id": "tpl-1", "name": "web"})
	if len(result.Items) != 1 || result.Items[0]["id"] != "tpl-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMapProbeResponseSkipsMissingOrScalarItemPaths(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ack",
		Resource: "template",
	}
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Item: "$.missing",
			ID:   "$.id",
			Fields: map[string]spec.ProbeField{
				"id": {Path: "$.id"},
			},
		},
	}
	if result := mapProbeResponse(resource, probe, map[string]any{"id": "tpl-1"}); len(result.Items) != 0 {
		t.Fatalf("missing item path result = %#v", result)
	}

	probe.Response.Item = "$.items"
	if result := mapProbeResponse(resource, probe, map[string]any{"items": []any{"scalar"}}); len(result.Items) != 0 {
		t.Fatalf("scalar item path result = %#v", result)
	}
}

func TestExecuteListFromProbe(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	caller := &fakeCaller{
		responses: []map[string]any{{
			"TotalCount": 1,
			"Vpcs": map[string]any{"Vpc": []any{
				map[string]any{
					"VpcId":     "vpc-1",
					"VpcName":   "prod",
					"CidrBlock": "10.0.0.0/16",
					"Status":    "Available",
					"RegionId":  "cn-beijing",
				},
			}},
		}},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "list",
		Input:  map[string]any{"limit": 50, "page": 1},
		Context: map[string]any{
			"region": "cn-beijing",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "DescribeVpcs" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if caller.calls[0].request["RegionId"] != "cn-beijing" || caller.calls[0].request["PageSize"] != 50 {
		t.Fatalf("request = %#v", caller.calls[0].request)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0]["id"] != "vpc-1" || result.Items[0]["status"] != "Available" {
		t.Fatalf("result = %#v", result)
	}
}

func containsPascalCaseKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key != "" && key[0] >= 'A' && key[0] <= 'Z' {
				return true
			}
			if containsPascalCaseKey(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsPascalCaseKey(item) {
				return true
			}
		}
	}
	return false
}

func TestExecuteGetMissingReturnsNotFound(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	caller := &fakeCaller{
		responses: []map[string]any{{
			"RequestId":  "req-missing",
			"TotalCount": 0,
			"Vpcs":       map[string]any{"Vpc": []any{}},
		}},
	}
	_, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "get",
		Input:  map[string]any{"id": "vpc-missing", "limit": 1, "page": 1},
		Context: map[string]any{
			"region": "cn-beijing",
		},
	})
	if err == nil || err.Error() == "" {
		t.Fatalf("expected not found error, got %v", err)
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || !reflect.DeepEqual(appErr.Actions(), []ecerrors.Action{{
		RequestID: "req-missing", ActionName: "DescribeVpcAttribute",
	}}) {
		t.Fatalf("not found actions = %#v", err)
	}
}

func TestExecuteCreateRunsTransitionWaitAndReadBack(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	waiter := spec.Waiters["available_after_create"]
	waiter.Interval = "1ms"
	spec.Waiters["available_after_create"] = waiter
	caller := &fakeCaller{
		responses: []map[string]any{
			{"VpcId": "vpc-new", "RequestId": "req-create"},
			{"RequestId": "req-pending", "VpcId": "vpc-new", "VpcName": "prod", "CidrBlock": "10.0.0.0/16", "Status": "Pending", "RegionId": "cn-beijing"},
			{"RequestId": "req-available", "VpcId": "vpc-new", "VpcName": "prod", "CidrBlock": "10.0.0.0/16", "Status": "Available", "RegionId": "cn-beijing"},
		},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-1",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if caller.calls[0].operation != "CreateVpc" || caller.calls[1].operation != "DescribeVpcAttribute" || caller.calls[2].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.ID != "vpc-new" || result.RequestID != "req-create" || len(result.Items) != 1 || result.Items[0]["status"] != "Available" {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(result.Actions, []ecerrors.Action{
		{RequestID: "req-create", ActionName: "CreateVpc"},
		{RequestID: "req-pending", ActionName: "DescribeVpcAttribute"},
		{RequestID: "req-available", ActionName: "DescribeVpcAttribute"},
	}) {
		t.Fatalf("actions = %#v", result.Actions)
	}
}

func TestExecuteWaitTimeoutOverrideExtendsAttemptBudget(t *testing.T) {
	resource := fakeVPCSpecForEngine(t)
	waitSpec := resource.Waiters["available_after_create"]
	waitSpec.Interval = "1ms"
	waitSpec.Timeout = "1ms"
	resource.Waiters["available_after_create"] = waitSpec
	caller := &fakeCaller{responses: []map[string]any{
		{"VpcId": "vpc-new", "RequestId": "req-create"},
		{"VpcId": "vpc-new", "Status": "Pending"},
		{"VpcId": "vpc-new", "Status": "Pending"},
		{"VpcId": "vpc-new", "Status": "Pending"},
		{"VpcId": "vpc-new", "Status": "Available"},
	}}

	_, err := NewExecutor(resource, caller).Execute(testContext(), Request{
		Action:  "create",
		Input:   map[string]any{"name": "prod", "cidr": "10.0.0.0/16"},
		Context: map[string]any{"region": "cn-beijing", "client_token": "token-1"},
		Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("timeout override must extend waiter attempts: %v", err)
	}
	if len(caller.calls) != 5 {
		t.Fatalf("calls = %d, want create plus four polls: %#v", len(caller.calls), caller.calls)
	}
}

func TestExecuteRGGroupCreateWaitsUntilOK(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "rg", "group")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	if waiter, ok := loaded.Waiters["ok_after_create"]; ok {
		if waiter.Interval != "1s" || waiter.Timeout != "10s" {
			t.Fatalf("ok_after_create waiter = interval %q timeout %q, want 1s/10s", waiter.Interval, waiter.Timeout)
		}
		waiter.Interval = "1ms"
		loaded.Waiters["ok_after_create"] = waiter
	}
	caller := &fakeCaller{
		responses: []map[string]any{
			{
				"RequestId": "req-create",
				"ResourceGroup": map[string]any{
					"Id": "rg-123",
				},
			},
			{
				"RequestId": "req-creating",
				"ResourceGroup": map[string]any{
					"Id":          "rg-123",
					"Name":        "prod-rg",
					"DisplayName": "Production",
					"Status":      "Creating",
				},
			},
			{
				"RequestId": "req-ok",
				"ResourceGroup": map[string]any{
					"Id":          "rg-123",
					"Name":        "prod-rg",
					"DisplayName": "Production",
					"Status":      "OK",
				},
			},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":         "prod-rg",
			"display_name": "Production",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("calls = %#v, want create plus two get probes", caller.calls)
	}
	if caller.calls[0].operation != "CreateResourceGroup" || caller.calls[1].operation != "GetResourceGroup" || caller.calls[2].operation != "GetResourceGroup" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.Item == nil || result.Item["id"] != "rg-123" || result.Item["status"] != "OK" {
		t.Fatalf("result = %#v, want final OK resource group", result)
	}
}

func TestExecuteInjectsClientTokenOnlyWhenTransitionDeclaresIdempotency(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	create := loaded.Bindings["create_to_available"]
	delete(create.Request, "ClientToken")
	create.Idempotency = spec.Idempotency{Field: "ClientToken", Prefix: "vpc-create"}
	loaded.Bindings["create_to_available"] = create

	caller := &fakeCaller{
		responses: []map[string]any{
			{"VpcId": "vpc-new", "RequestId": "req-create"},
		},
	}

	_, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
		TokenGenerator: func(prefix string, _ map[string]any) string {
			if prefix != "vpc-create" {
				t.Fatalf("token prefix = %q, want vpc-create", prefix)
			}
			return "token-from-engine"
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := caller.calls[0].request["ClientToken"]
	if got != "token-from-engine" {
		t.Fatalf("ClientToken = %#v, want token-from-engine", got)
	}
}

func TestExecuteUsesExplicitIdempotencyKey(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	create := loaded.Bindings["create_to_available"]
	delete(create.Request, "ClientToken")
	create.Idempotency = spec.Idempotency{Field: "ClientToken", Prefix: "vpc-create"}
	loaded.Bindings["create_to_available"] = create

	caller := &fakeCaller{
		responses: []map[string]any{
			{"VpcId": "vpc-new", "RequestId": "req-create"},
		},
	}

	_, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":            "prod",
			"cidr":            "10.0.0.0/16",
			"no_wait":         true,
			"idempotency_key": "agent-retry-1",
		},
		Context: map[string]any{"region": "cn-beijing"},
		TokenGenerator: func(string, map[string]any) string {
			t.Fatal("TokenGenerator should not run when idempotency_key is explicit")
			return ""
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := caller.calls[0].request["ClientToken"]
	if got != "agent-retry-1" {
		t.Fatalf("ClientToken = %#v, want explicit idempotency key", got)
	}
}

func TestMapProbeResponseSupportsSingleItemResponse(t *testing.T) {
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Item:      "$",
			RequestID: "$.RequestId",
			ID:        "$.VpcId",
			State:     "$.Status",
			Fields: map[string]spec.ProbeField{
				"id":     {Path: "$.VpcId"},
				"name":   {Path: "$.VpcName"},
				"status": {Path: "$.Status"},
			},
		},
	}
	result := mapProbeResponse(spec.ResourceSpec{Product: "vpc", Resource: "vpc"}, probe, map[string]any{
		"RequestId": "req-1",
		"VpcId":     "vpc-1",
		"VpcName":   "prod",
		"Status":    "Available",
	})
	if result.RequestID != "req-1" || len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Items[0]["id"] != "vpc-1" || result.Items[0]["status"] != "Available" {
		t.Fatalf("item = %#v", result.Items[0])
	}
}

func TestMapProbeResponsePreservesRawStatusValues(t *testing.T) {
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Items: "$.Instances.Instance",
			Fields: map[string]spec.ProbeField{
				"id":     {Path: "$.InstanceId"},
				"status": {Path: "$.Status"},
			},
		},
	}

	result := mapProbeResponse(spec.ResourceSpec{Product: "ecs", Resource: "instance"}, probe, map[string]any{
		"Instances": map[string]any{"Instance": []any{
			map[string]any{"InstanceId": "i-1", "Status": "Running"},
		}},
	})

	if len(result.Items) != 1 || result.Items[0]["status"] != "Running" {
		t.Fatalf("status = %#v, want raw Running", result.Items)
	}
}

func TestMapProbeResponseNormalizesSecurityGroupRules(t *testing.T) {
	probe := spec.Probe{
		Response: spec.ProbeResponse{
			Item:      "$",
			RequestID: "$.RequestId",
			ID:        "$.SecurityGroupId",
			Fields: map[string]spec.ProbeField{
				"id": {Path: "$.SecurityGroupId"},
				"rules": {
					From: "$.Permissions.Permission",
					Each: map[string]spec.ProbeField{
						"id":        {Path: "$.SecurityGroupRuleId"},
						"direction": {Lower: "$.Direction"},
						"protocol":  {Lower: "$.IpProtocol"},
						"port":      {Port: "$.PortRange"},
						"cidr":      {First: []string{"$.SourceCidrIp", "$.DestCidrIp"}},
						"policy":    {Lower: "$.Policy"},
						"priority":  {Int: "$.Priority"},
					},
				},
			},
		},
	}
	result := mapProbeResponse(spec.ResourceSpec{Product: "ecs", Resource: "sg"}, probe, map[string]any{
		"RequestId":       "req-1",
		"SecurityGroupId": "sg-1",
		"Permissions": map[string]any{"Permission": []any{
			map[string]any{
				"SecurityGroupRuleId": "sgr-1",
				"Direction":           "ingress",
				"IpProtocol":          "TCP",
				"PortRange":           "80/80",
				"SourceCidrIp":        "0.0.0.0/0",
				"Policy":              "Accept",
				"Priority":            float64(1),
			},
		}},
	})
	if len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	rules, ok := result.Items[0]["rules"].([]map[string]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %#v", result.Items[0]["rules"])
	}
	if rules[0]["id"] != "sgr-1" || rules[0]["protocol"] != "tcp" || rules[0]["port"] != "80" || rules[0]["policy"] != "accept" {
		t.Fatalf("normalized rule = %#v", rules[0])
	}
}

func TestMapProbeResponseNormalizesTagsForSupportedResources(t *testing.T) {
	tests := []struct {
		name     string
		product  string
		resource string
		probe    string
		response map[string]any
	}{
		{
			name:     "vpc list",
			product:  "vpc",
			resource: "vpc",
			probe:    "list",
			response: map[string]any{
				"Vpcs": map[string]any{"Vpc": []any{
					map[string]any{"VpcId": "vpc-1", "Tags": map[string]any{"Tag": tagItems()}},
				}},
			},
		},
		{
			name:     "vpc get",
			product:  "vpc",
			resource: "vpc",
			probe:    "attribute",
			response: map[string]any{
				"VpcId": "vpc-1",
				"Tags":  map[string]any{"Tag": tagItems()},
			},
		},
		{
			name:     "vswitch list",
			product:  "vpc",
			resource: "vswitch",
			probe:    "list",
			response: map[string]any{
				"VSwitches": map[string]any{"VSwitch": []any{
					map[string]any{"VSwitchId": "vsw-1", "Tags": map[string]any{"Tag": tagItems()}},
				}},
			},
		},
		{
			name:     "vswitch get",
			product:  "vpc",
			resource: "vswitch",
			probe:    "attribute",
			response: map[string]any{
				"VSwitchId": "vsw-1",
				"Tags":      map[string]any{"Tag": tagItems()},
			},
		},
		{
			name:     "security group list",
			product:  "ecs",
			resource: "sg",
			probe:    "list",
			response: map[string]any{
				"SecurityGroups": map[string]any{"SecurityGroup": []any{
					map[string]any{"SecurityGroupId": "sg-1", "Tags": map[string]any{"Tag": tagItems()}},
				}},
			},
		},
		{
			name:     "ecs instance list",
			product:  "ecs",
			resource: "instance",
			probe:    "state",
			response: map[string]any{
				"Instances": map[string]any{"Instance": []any{
					map[string]any{"InstanceId": "i-1", "Tags": map[string]any{"Tag": tagItems()}},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loaded, err := spec.LoadResource("../../specs", tt.product, tt.resource)
			if err != nil {
				t.Fatalf("LoadResource: %v", err)
			}
			result := mapProbeResponse(loaded, loaded.Probes[tt.probe], tt.response)
			if len(result.Items) != 1 {
				t.Fatalf("items = %#v", result.Items)
			}
			tags, ok := result.Items[0]["tags"].(map[string]string)
			if !ok {
				t.Fatalf("tags = %#v, want map[string]string", result.Items[0]["tags"])
			}
			if tags["env"] != "prod" || tags["team"] != "platform" {
				t.Fatalf("tags = %#v", tags)
			}
		})
	}
}

func tagItems() []any {
	return []any{
		map[string]any{"TagKey": "env", "TagValue": "prod"},
		map[string]any{"Key": "team", "Value": "platform"},
	}
}

func TestExecuteWorkflowAppliesRuleListRequestTransform(t *testing.T) {
	loaded, err := spec.Load([]byte(`
schema_version: 2
product: ecs
resource: sg
kind: regional
identity:
  field: id
  output_root:
    one: security_group
    many: security_groups
schema:
  fields:
    id:
      type: string
    rule:
      type: array
      items:
        type: string
probes:
  attribute:
    api: DescribeSecurityGroupAttribute
    request:
      RegionId: $context.region
      SecurityGroupId: $.id
    response:
      item: $
      request_id: $.RequestId
      id: $.SecurityGroupId
      fields:
        id: $.SecurityGroupId
        rules:
          from: $.Permissions.Permission
          each:
            id: $.SecurityGroupRuleId
            direction:
              lower: $.Direction
            protocol:
              lower: $.IpProtocol
            port:
              port: $.PortRange
            cidr:
              first: [$.SourceCidrIp, $.DestCidrIp]
            policy:
              lower: $.Policy
            priority:
              int: $.Priority
bindings:
  authorize_rules:
    api: AuthorizeSecurityGroup
    request:
      RegionId: $context.region
      SecurityGroupId: $.id
      Permissions:
        capture: rule_permissions
        each:
          normalize: security_group_rule
          sources:
            - source: $.rule
          defaults:
            direction: ingress
            policy: accept
            priority: 1
          enum:
            direction: [ingress]
        fields:
          IpProtocol: $.protocol
          PortRange: $.port_range
          SourceCidrIp: $.cidr
          Policy: $.policy
          Priority: $.priority
    request_id_from: $.RequestId
operations:
  authorize:
    examples:
      - ecctl ecs sg authorize <sg-id> --rule tcp:22@0.0.0.0/0
    input:
      fields: [id, rule]
    workflow:
      - binding: authorize_rules
      - probe: attribute
        ids:
          - $input.id
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-auth"},
			{
				"RequestId":       "req-attr",
				"SecurityGroupId": "sg-123",
				"Permissions": map[string]any{"Permission": []any{
					map[string]any{
						"SecurityGroupRuleId": "sgr-http",
						"Direction":           "ingress",
						"IpProtocol":          "TCP",
						"PortRange":           "80/80",
						"SourceCidrIp":        "0.0.0.0/0",
						"Policy":              "Accept",
						"Priority":            float64(1),
					},
					map[string]any{
						"SecurityGroupRuleId": "sgr-https",
						"Direction":           "ingress",
						"IpProtocol":          "TCP",
						"PortRange":           "443/443",
						"SourceCidrIp":        "0.0.0.0/0",
						"Policy":              "Accept",
						"Priority":            float64(1),
					},
				}},
			},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "authorize",
		Input: map[string]any{
			"id":   "sg-123",
			"rule": []string{"tcp:80@0.0.0.0/0", "ingress:tcp:443:0.0.0.0/0"},
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "AuthorizeSecurityGroup" || caller.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	request := caller.calls[0].request
	for key, want := range map[string]any{
		"RegionId":                   "cn-beijing",
		"SecurityGroupId":            "sg-123",
		"Permissions.1.IpProtocol":   "tcp",
		"Permissions.1.PortRange":    "80/80",
		"Permissions.1.SourceCidrIp": "0.0.0.0/0",
		"Permissions.1.Policy":       "accept",
		"Permissions.1.Priority":     1,
		"Permissions.2.IpProtocol":   "tcp",
		"Permissions.2.PortRange":    "443/443",
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	rules, ok := result.Item["rules"].([]map[string]any)
	if !ok || len(rules) != 2 || rules[0]["id"] != "sgr-http" || rules[1]["id"] != "sgr-https" {
		t.Fatalf("rules = %#v", result.Item["rules"])
	}
	if len(result.Captures["rule_permissions"].Items) != 2 {
		t.Fatalf("captures = %#v", result.Captures)
	}
}

func TestExecuteWorkflowRoutesSingleAndBatchInputs(t *testing.T) {
	loaded, err := spec.Load([]byte(`
schema_version: 2
product: ecs
resource: instance
kind: regional
identity:
  field: id
  output_root:
    one: instance
    many: instances
schema:
  fields:
    ids:
      type: array
      items:
        type: string
probes:
  state:
    api: DescribeInstances
    request:
      RegionId: $context.region
      InstanceIds: $input.ids
    response:
      items: $.Instances.Instance
      id: $.InstanceId
      state: $.Status
      fields:
        id: $.InstanceId
        status: $.Status
bindings:
  start_one:
    api: StartInstance
    request:
      RegionId: $context.region
      InstanceId: $first(input.ids)
    request_id_from: $.RequestId
  start_many:
    api: StartInstances
    request:
      RegionId: $context.region
      InstanceId:
        each: $input.ids
    request_id_from: $.RequestId
operations:
  start:
    examples:
      - ecctl ecs instance start <id>
    input:
      fields:
        - ids:
            positional_many: true
    workflow:
      - binding: start_one
        when: single(input.ids)
      - binding: start_many
        when: multiple(input.ids)
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	single := &fakeCaller{responses: []map[string]any{{"RequestId": "req-one"}}}
	_, err = NewExecutor(loaded, single).Execute(testContext(), Request{
		Action:  "start",
		Input:   map[string]any{"ids": []string{"i-1"}},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("single Execute: %v", err)
	}
	if len(single.calls) != 1 || single.calls[0].operation != "StartInstance" || single.calls[0].request["InstanceId"] != "i-1" {
		t.Fatalf("single calls = %#v", single.calls)
	}

	batch := &fakeCaller{responses: []map[string]any{{"RequestId": "req-many"}}}
	_, err = NewExecutor(loaded, batch).Execute(testContext(), Request{
		Action:  "start",
		Input:   map[string]any{"ids": []string{"i-1", "i-2"}},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("batch Execute: %v", err)
	}
	if len(batch.calls) != 1 || batch.calls[0].operation != "StartInstances" {
		t.Fatalf("batch calls = %#v", batch.calls)
	}
	if batch.calls[0].request["InstanceId.1"] != "i-1" || batch.calls[0].request["InstanceId.2"] != "i-2" {
		t.Fatalf("batch request = %#v", batch.calls[0].request)
	}
}

func TestExecuteProbeExpandsStructuredRequestBindings(t *testing.T) {
	loaded, err := spec.Load([]byte(`
schema_version: 2
product: tag
resource: resource
kind: regional
identity:
  field: id
schema:
  fields:
    ids:
      type: array
      items:
        type: string
probes:
  tags:
    api: ListTagResources
    request:
      RegionId: $context.region
      ResourceId:
        each: $input.ids
    response:
      items: $.TagResources.TagResource
      fields:
        id: $.ResourceId
operations:
  list:
    input:
      fields: [ids]
    workflow:
      - probe: tags
        ids:
          - $input.ids
        many: true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{{
		"TagResources": map[string]any{"TagResource": []any{
			map[string]any{"ResourceId": "i-1"},
		}},
	}}}

	_, err = NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action:  "list",
		Input:   map[string]any{"ids": []string{"i-1", "i-2"}},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].request["ResourceId.1"] != "i-1" || caller.calls[0].request["ResourceId.2"] != "i-2" {
		t.Fatalf("probe request = %#v", caller.calls)
	}
}

func TestWaiterStateCanTargetRequestedIDPresence(t *testing.T) {
	probe := spec.Probe{Response: spec.ProbeResponse{Absent: spec.AbsentRule{WhenEmptyForRequestedIDs: true}}}
	result := ProbeResult{Items: []map[string]any{{"id": "p-2"}}}

	if got := waiterState(spec.Waiter{Target: "present"}, probe, result, []string{"p-1"}); got != "" {
		t.Fatalf("present missing id state = %q, want empty", got)
	}
	if got := waiterState(spec.Waiter{Target: "present"}, probe, result, []string{"p-2"}); got != "present" {
		t.Fatalf("present matching id state = %q, want present", got)
	}
	if got := waiterState(spec.Waiter{Target: "absent"}, probe, result, []string{"p-1"}); got != "absent" {
		t.Fatalf("absent missing id state = %q, want absent", got)
	}
	if got := waiterState(spec.Waiter{Target: "absent"}, probe, result, []string{"p-2"}); got != "" {
		t.Fatalf("absent matching id state = %q, want empty", got)
	}
	if got := waiterState(spec.Waiter{Target: "present"}, probe, ProbeResult{Items: []map[string]any{{"name": "ready"}}}, nil); got != "present" {
		t.Fatalf("present non-empty state = %q, want present", got)
	}
}

func TestWaiterStateCanTargetCapturedItemMatches(t *testing.T) {
	waitSpec := spec.Waiter{
		Target: "matched",
		Match:  spec.WaiterMatch{Capture: "entries", By: []string{"cidr", "description"}},
	}
	probe := spec.Probe{}
	ctx := ExecutionContext{Captures: map[string]CaptureResult{
		"entries": {Items: []map[string]any{{"cidr": "10.0.0.0/24", "description": "web"}}},
	}}

	if got := waiterState(waitSpec, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.0.0/24", "description": "old"}}}, nil, ctx); got != "" {
		t.Fatalf("mismatched capture state = %q, want empty", got)
	}
	if got := waiterState(waitSpec, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.0.0/24", "description": "web"}}}, nil, ctx); got != "matched" {
		t.Fatalf("matched capture state = %q, want matched", got)
	}
	if got := waiterState(waitSpec, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.0.0/24", "description": "web"}}}, nil); got != "" {
		t.Fatalf("missing capture state = %q, want empty", got)
	}
	emptyCaptureCtx := ExecutionContext{Captures: map[string]CaptureResult{
		"entries": {Items: []map[string]any{{}}},
	}}
	if got := waiterState(waitSpec, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.0.0/24", "description": "web"}}}, nil, emptyCaptureCtx); got != "" {
		t.Fatalf("empty capture state = %q, want empty", got)
	}
	absentWait := spec.Waiter{
		Target: "absent",
		Match:  spec.WaiterMatch{Capture: "entries", By: []string{"cidr"}},
	}
	if got := waiterState(absentWait, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.1.0/24"}}}, nil, ctx); got != "absent" {
		t.Fatalf("absent capture state = %q, want absent", got)
	}
	if got := waiterState(absentWait, probe, ProbeResult{Items: []map[string]any{{"cidr": "10.0.0.0/24"}}}, nil, ctx); got != "" {
		t.Fatalf("present removed capture state = %q, want empty", got)
	}
}

func TestWaiterStateCanMatchRequestedFieldsAndCollections(t *testing.T) {
	waitSpec := spec.Waiter{
		Target: "matched",
		Match: spec.WaiterMatch{
			Fields:   map[string]string{"name": "$.name"},
			Contains: map[string]string{"secondary_cidr_blocks": `$prefixed_values($.cidr_changes,"+")`},
			Excludes: map[string]string{"secondary_cidr_blocks": `$prefixed_values($.cidr_changes,"-")`},
		},
	}
	ctx := ExecutionContext{Input: map[string]any{
		"name":         "renamed",
		"cidr_changes": []string{"+172.16.0.0/16", "-192.168.0.0/16"},
	}}

	stale := ProbeResult{Items: []map[string]any{{
		"name": "old", "secondary_cidr_blocks": []any{"192.168.0.0/16"},
	}}}
	if got := waiterState(waitSpec, spec.Probe{}, stale, nil, ctx); got != "" {
		t.Fatalf("stale fields state = %q, want empty", got)
	}
	converged := ProbeResult{Items: []map[string]any{{
		"name": "renamed", "secondary_cidr_blocks": []any{"172.16.0.0/16"},
	}}}
	if got := waiterState(waitSpec, spec.Probe{}, converged, nil, ctx); got != "matched" {
		t.Fatalf("converged fields state = %q, want matched", got)
	}
}

func TestExecuteWorkflowWaitUsesCapturedBindingItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widget.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 2
product: demo
resource: widget
kind: regional
identity:
  field: id
schema:
  fields:
    id:
      type: string
    entries:
      type: array
      items:
        type: object
        fields:
          name:
            type: string
bindings:
  update:
    api: UpdateWidget
    request:
      WidgetId: $.id
      Entry:
        capture:
          name: entries
          fields:
            name: $.name
        each: $.entries
        fields:
          Name: $.name
    request_id_from: $.RequestId
probes:
  entries:
    api: DescribeWidgetEntries
    request:
      WidgetId: $.id
    response:
      items: $.Entries
      request_id: $.RequestId
      fields:
        name: $.Name
waiters:
  entries_visible:
    probe: entries
    target: matched
    interval: 1ms
    timeout: 10ms
    match:
      capture: entries
      by: [name]
operations:
  update:
    examples:
      - ecctl widget update <id>
    workflow:
      - binding: update
      - wait: entries_visible
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resource, err := spec.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{
		{"RequestId": "req-update"},
		{"RequestId": "req-entries", "Entries": []any{map[string]any{"Name": "web"}}},
	}}

	_, err = NewExecutor(resource, caller).Execute(testContext(), Request{
		Action: "update",
		Input:  map[string]any{"id": "w-123", "entries": []any{map[string]any{"name": "web"}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[1].operation != "DescribeWidgetEntries" {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestExecuteWorkflowWaitMatchesTopLevelEachCaptureResponseIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widget.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 2
product: demo
resource: widget
kind: regional
identity:
  field: id
schema:
  fields:
    id:
      type: string
    entries:
      type: array
      items:
        type: object
        fields:
          name:
            type: string
bindings:
  add_entry:
    api: AddWidgetEntry
    each: $.entries
    capture:
      name: entries
      fields:
        name: $.name
    request:
      WidgetId: $input.id
      Name: $.name
    id_from: $.EntryId
    request_id_from: $.RequestId
probes:
  entries:
    api: DescribeWidgetEntries
    request:
      WidgetId: $.id
    response:
      items: $.Entries
      request_id: $.RequestId
      fields:
        id: $.EntryId
        name: $.Name
waiters:
  entries_visible:
    probe: entries
    target: matched
    interval: 1ms
    timeout: 20ms
    match:
      capture: entries
      by: [id, name]
operations:
  update:
    examples:
      - ecctl widget update <id>
    workflow:
      - binding: add_entry
      - wait: entries_visible
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resource, err := spec.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{
		{"RequestId": "req-add", "EntryId": "entry-new"},
		{"RequestId": "req-entries-old", "Entries": []any{map[string]any{"EntryId": "entry-old", "Name": "web"}}},
		{"RequestId": "req-entries-new", "Entries": []any{map[string]any{"EntryId": "entry-new", "Name": "web"}}},
	}}

	result, err := NewExecutor(resource, caller).Execute(testContext(), Request{
		Action: "update",
		Input:  map[string]any{"id": "w-123", "entries": []any{map[string]any{"name": "web"}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 3 || caller.calls[1].operation != "DescribeWidgetEntries" || caller.calls[2].operation != "DescribeWidgetEntries" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	capture := result.Captures["entries"]
	if len(capture.Items) != 1 || capture.Items[0]["id"] != "entry-new" || capture.Items[0]["name"] != "web" {
		t.Fatalf("capture = %#v", capture.Items)
	}
}

func TestExecuteCreateTreatsInitialAbsentAsTransient(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	waiter := spec.Waiters["available_after_create"]
	waiter.Interval = "1ms"
	spec.Waiters["available_after_create"] = waiter
	caller := &fakeCaller{
		responses: []map[string]any{
			{"VpcId": "vpc-new", "RequestId": "req-create"},
			{},
			{"VpcId": "vpc-new", "Status": "Pending"},
			{"VpcId": "vpc-new", "Status": "Available"},
		},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-1",
		},
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 4 {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.ID != "vpc-new" || result.Item["status"] != "Available" {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(result.Actions, []ecerrors.Action{
		{RequestID: "req-create", ActionName: "CreateVpc"},
		{ActionName: "DescribeVpcAttribute"},
		{ActionName: "DescribeVpcAttribute"},
		{ActionName: "DescribeVpcAttribute"},
	}) {
		t.Fatalf("actions = %#v", result.Actions)
	}
}

func TestExecuteECSCreateTreatsInitialStoppedAsTransient(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "ecs", "instance")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	waiterSpec := loaded.Waiters["running_after_create"]
	waiterSpec.Interval = "1ms"
	loaded.Waiters["running_after_create"] = waiterSpec
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "InstanceIdSets": map[string]any{"InstanceIdSet": []any{"i-123"}}},
			{"RequestId": "req-stopped", "TotalCount": 1, "Instances": map[string]any{"Instance": []any{
				map[string]any{"InstanceId": "i-123", "Status": "Stopped"},
			}}},
			{"RequestId": "req-starting", "TotalCount": 1, "Instances": map[string]any{"Instance": []any{
				map[string]any{"InstanceId": "i-123", "Status": "Starting"},
			}}},
			{"RequestId": "req-running", "TotalCount": 1, "Instances": map[string]any{"Instance": []any{
				map[string]any{"InstanceId": "i-123", "Status": "Running"},
			}}},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"type":                 "ecs.u1-c1m1.large",
			"image":                "aliyun_3_x64_20G_alibase_20240528.vhd",
			"sg":                   "sg-123",
			"vswitch":              "vsw-123",
			"instance_charge_type": "PostPaid",
			"system_disk_category": "cloud_essd",
			"no_wait":              false,
		},
		Context: map[string]any{"region": "cn-beijing"},
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 4 {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.ID != "i-123" || result.Item["status"] != "Running" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteCreateNoWaitSkipsWaitAndReadBack(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	caller := &fakeCaller{
		responses: []map[string]any{{"VpcId": "vpc-new", "RequestId": "req-create"}},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"no_wait": true,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-1",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "CreateVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.ID != "vpc-new" || result.RequestID != "req-create" {
		t.Fatalf("result = %#v", result)
	}
	if result.Item["id"] != "vpc-new" || result.Item["name"] != "prod" || result.Item["cidr"] != "10.0.0.0/16" || result.Item["region"] != "cn-beijing" {
		t.Fatalf("named emit did not build context item: %#v", result.Item)
	}
}

func TestExecuteWorkflowStepWhenSkipsUntilConditionMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widget.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 2
product: demo
resource: widget
kind: regional
identity:
  field: id
schema:
  fields: {}
bindings:
  rename:
    api: RenameWidget
    request:
      RegionId: $context.region
      WidgetId: $.id
      Name: $.name
    request_id_from: $.RequestId
  resize:
    api: ResizeWidget
    request:
      RegionId: $context.region
      WidgetId: $.id
      Size: $.size
    request_id_from: $.RequestId
operations:
  update:
    examples:
      - ecctl widget update <id> --name new
    workflow:
      - binding: rename
        when: input.name
      - binding: resize
        when: input.size
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resource, err := spec.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{{"RequestId": "req-rename"}}}

	result, err := NewExecutor(resource, caller).Execute(testContext(), Request{
		Action:  "update",
		Input:   map[string]any{"id": "w-123", "name": "web"},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "RenameWidget" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.Actions[0].ActionName != "RenameWidget" || result.Actions[0].RequestID != "req-rename" {
		t.Fatalf("actions = %#v", result.Actions)
	}
}

func TestExecuteBindingContextFromResponseFeedsLaterProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widget.yaml")
	if err := os.WriteFile(path, []byte(`schema_version: 2
product: demo
resource: widget
kind: regional
identity:
  field: id
schema:
  fields:
    id:
      type: string
bindings:
  create_version:
    api: CreateWidgetVersion
    request:
      WidgetId: $.id
    context_from:
      created_version: $.VersionNumber
    request_id_from: $.RequestId
probes:
  version:
    api: DescribeWidgetVersions
    request:
      WidgetId: $.id
      Version: $context.created_version
    response:
      item: $
      request_id: $.RequestId
      fields:
        version: $.VersionNumber
operations:
  update:
    examples:
      - ecctl widget update <id>
    workflow:
      - binding: create_version
      - probe: version
        merge: true
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resource, err := spec.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	caller := &fakeCaller{responses: []map[string]any{
		{"RequestId": "req-create", "VersionNumber": int64(2)},
		{"RequestId": "req-version", "VersionNumber": int64(2)},
	}}

	result, err := NewExecutor(resource, caller).Execute(testContext(), Request{
		Action: "update",
		Input:  map[string]any{"id": "w-123"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[1].request["Version"] != int64(2) {
		t.Fatalf("version probe request = %#v; calls=%#v", caller.calls[1].request, caller.calls)
	}
	if result.Item["version"] != int64(2) {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteRejectsUnknownNamedEmit(t *testing.T) {
	resource := fakeVPCSpecForEngine(t)
	operation := resource.Operations["list"]
	operation.Workflow = append(operation.Workflow, spec.WorkflowStep{Emit: "unknown_emit"})
	resource.Operations["list"] = operation
	caller := &fakeCaller{
		responses: []map[string]any{{
			"TotalCount": 0,
			"Vpcs":       map[string]any{"Vpc": []any{}},
		}},
	}

	_, err := NewExecutor(resource, caller).Execute(testContext(), Request{
		Action:  "list",
		Input:   map[string]any{"limit": 50, "page": 1},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err == nil || !strings.Contains(err.Error(), `emit "unknown_emit" is not supported`) {
		t.Fatalf("Execute error = %v, want unsupported emit", err)
	}
}

func TestExecuteCreateDryRunSkipsIDWaitAndReadBack(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	caller := &fakeCaller{
		responses: []map[string]any{{"DryRun": true}},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"dry_run": true,
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-1",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "CreateVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if !result.DryRun {
		t.Fatalf("result = %#v, want dry-run signal", result)
	}
}

func TestExecuteDeleteDryRunSkipsDeletedEmit(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	deleteBinding := spec.Bindings["delete_to_absent"]
	deleteBinding.Request["DryRun"] = "$.dry_run"
	spec.Bindings["delete_to_absent"] = deleteBinding
	caller := &fakeCaller{
		responses: []map[string]any{{"DryRun": true}},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"dry_run": true,
			"no_wait": false,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "DeleteVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if !result.DryRun || result.Deleted {
		t.Fatalf("result = %#v, want dry-run without deleted emit", result)
	}
}

func TestExecuteCreateMissingTransitionIDFailsBeforeWait(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	caller := &fakeCaller{
		responses: []map[string]any{{"RequestId": "req-create"}},
	}
	_, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":    "prod",
			"cidr":    "10.0.0.0/16",
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-1",
		},
		Timeout: time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected missing transition ID error")
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "CreateVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
}

func TestExecuteDeleteWaitsForAbsent(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	w := spec.Waiters["deleted_after_delete"]
	w.Interval = "1ms"
	w.Timeout = "50ms"
	spec.Waiters["deleted_after_delete"] = w
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-delete"},
			{"TotalCount": 1, "Vpcs": map[string]any{"Vpc": []any{
				map[string]any{"VpcId": "vpc-1", "Status": "Available"},
			}}},
			{"TotalCount": 0, "Vpcs": map[string]any{"Vpc": []any{}}},
		},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-2",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.RequestID != "req-delete" || !result.Deleted || result.Item["id"] != "vpc-1" {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(result.Actions, []ecerrors.Action{
		{RequestID: "req-delete", ActionName: "DeleteVpc"},
		{ActionName: "DescribeVpcs"},
		{ActionName: "DescribeVpcs"},
	}) {
		t.Fatalf("actions = %#v", result.Actions)
	}
}

func TestExecuteDeleteWaitsForAbsentWhenProbeReturnsNotFound(t *testing.T) {
	spec := fakeVPCSpecForEngine(t)
	w := spec.Waiters["deleted_after_delete"]
	w.Interval = "1ms"
	w.Timeout = "50ms"
	spec.Waiters["deleted_after_delete"] = w
	caller := &fakeCaller{
		errors: []error{
			nil,
			ecerrors.NotFound("NotFound", "vpc not found"),
		},
		responses: []map[string]any{
			{"RequestId": "req-delete"},
		},
	}
	result, err := NewExecutor(spec, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"no_wait": false,
		},
		Context: map[string]any{
			"region":       "cn-beijing",
			"client_token": "token-2",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "DeleteVpc" || caller.calls[1].operation != "DescribeVpcs" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.RequestID != "req-delete" || !result.Deleted || result.Item["id"] != "vpc-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteTransitionRetriesHiddenInitializingStatus(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	binding := loaded.Bindings["delete_to_absent"]
	binding.Retry = spec.TransitionRetry{
		Policy:          "initializing_grace",
		Errors:          []string{"HiddenRetry"},
		InitialInterval: "1ms",
		MaxInterval:     "1ms",
		Timeout:         "20ms",
	}
	loaded.Bindings["delete_to_absent"] = binding
	caller := &fakeCaller{
		errors: []error{
			errors.New("SDK.ServerError\nErrorCode: HiddenRetry\nMessage: The resource is in a hidden state."),
		},
		responses: []map[string]any{
			{"RequestId": "req-delete"},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "DeleteVpc" || caller.calls[1].operation != "DeleteVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.RequestID != "req-delete" || !result.Deleted {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteTransitionRetryWhenGateDisablesRetry(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	binding := loaded.Bindings["delete_to_absent"]
	binding.Retry = spec.TransitionRetry{
		Policy:          "initializing_grace",
		Errors:          []string{"HiddenRetry"},
		When:            "input.force",
		InitialInterval: "1ms",
		MaxInterval:     "1ms",
		Timeout:         "20ms",
	}
	loaded.Bindings["delete_to_absent"] = binding
	caller := &fakeCaller{
		errors: []error{
			errors.New("SDK.ServerError\nErrorCode: HiddenRetry\nMessage: The resource is in a hidden state."),
		},
	}

	_, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err == nil {
		t.Fatalf("Execute: expected error to surface immediately, got nil")
	}
	if len(caller.calls) != 1 || caller.calls[0].operation != "DeleteVpc" {
		t.Fatalf("calls = %#v, want a single DeleteVpc with no retry", caller.calls)
	}
}

func TestExecuteTransitionRetryWhenGateEnablesRetry(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	binding := loaded.Bindings["delete_to_absent"]
	binding.Retry = spec.TransitionRetry{
		Policy:          "initializing_grace",
		Errors:          []string{"HiddenRetry"},
		When:            "input.force",
		InitialInterval: "1ms",
		MaxInterval:     "1ms",
		Timeout:         "20ms",
	}
	loaded.Bindings["delete_to_absent"] = binding
	caller := &fakeCaller{
		errors: []error{
			errors.New("SDK.ServerError\nErrorCode: HiddenRetry\nMessage: The resource is in a hidden state."),
		},
		responses: []map[string]any{
			{"RequestId": "req-delete"},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"force":   true,
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "DeleteVpc" || caller.calls[1].operation != "DeleteVpc" {
		t.Fatalf("calls = %#v, want DeleteVpc retried once", caller.calls)
	}
	if result.RequestID != "req-delete" || !result.Deleted {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteTransitionRetriesHiddenStatusFromRawCause(t *testing.T) {
	loaded := fakeVPCSpecForEngine(t)
	binding := loaded.Bindings["delete_to_absent"]
	binding.Retry = spec.TransitionRetry{
		Policy:          "initializing_grace",
		Errors:          []string{"HiddenRetry"},
		InitialInterval: "1ms",
		MaxInterval:     "1ms",
		Timeout:         "20ms",
	}
	loaded.Bindings["delete_to_absent"] = binding
	caller := &fakeCaller{
		errors: []error{
			ecerrors.Service("CloudAPIError", "The resource is in a hidden state.", false,
				ecerrors.WithRawCause("HiddenRetry", "The resource is in a hidden state."),
			),
		},
		responses: []map[string]any{
			{"RequestId": "req-delete"},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "delete",
		Input: map[string]any{
			"id":      "vpc-1",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 2 || caller.calls[0].operation != "DeleteVpc" || caller.calls[1].operation != "DeleteVpc" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.RequestID != "req-delete" || !result.Deleted {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteGetUsesAttributeProbe(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{{
			"RequestId": "req-attr",
			"VpcId":     "vpc-123",
			"VpcName":   "prod",
			"CidrBlock": "10.0.0.0/16",
			"Status":    "Available",
			"RegionId":  "cn-beijing",
		}},
	}
	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action:  "get",
		Input:   map[string]any{"id": "vpc-123"},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if caller.calls[0].operation != "DescribeVpcAttribute" {
		t.Fatalf("operation = %q, want DescribeVpcAttribute", caller.calls[0].operation)
	}
	if result.Item["id"] != "vpc-123" || result.Item["status"] != "Available" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteUpdateRunsModifyThenAttributeReadBack(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			{
				"RequestId": "req-attr",
				"VpcId":     "vpc-123",
				"VpcName":   "prod-network",
				"CidrBlock": "10.0.0.0/16",
				"Status":    "Available",
				"RegionId":  "cn-beijing",
			},
		},
	}
	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "update",
		Input: map[string]any{
			"id":   "vpc-123",
			"name": "prod-network",
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if caller.calls[0].operation != "ModifyVpcAttribute" || caller.calls[1].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if _, ok := caller.calls[0].request["ClientToken"]; ok {
		t.Fatalf("ModifyVpcAttribute must not receive ClientToken: %#v", caller.calls[0].request)
	}
	if result.Item["name"] != "prod-network" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteVPCUpdateWaitsForDnsHostnameStatusToStabilize(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	waiterSpec := loaded.Waiters["available_after_update"]
	waiterSpec.Interval = "1ms"
	loaded.Waiters["available_after_update"] = waiterSpec
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			{
				"RequestId":         "req-dns-modifying",
				"VpcId":             "vpc-123",
				"VpcName":           "prod-network",
				"CidrBlock":         "10.0.0.0/16",
				"Status":            "Available",
				"DnsHostnameStatus": "MODIFYING",
				"RegionId":          "cn-beijing",
			},
			{
				"RequestId":            "req-dhcp-pending",
				"VpcId":                "vpc-123",
				"VpcName":              "prod-network",
				"CidrBlock":            "10.0.0.0/16",
				"Status":               "Available",
				"DnsHostnameStatus":    "DISABLED",
				"DhcpOptionsSetStatus": "Pending",
				"RegionId":             "cn-beijing",
			},
			{
				"RequestId":         "req-dns-enabled",
				"VpcId":             "vpc-123",
				"VpcName":           "prod-network",
				"CidrBlock":         "10.0.0.0/16",
				"Status":            "Available",
				"DnsHostnameStatus": "ENABLED",
				"RegionId":          "cn-beijing",
			},
		},
	}
	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "update",
		Input: map[string]any{
			"id":                  "vpc-123",
			"enable_dns_hostname": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 4 {
		t.Fatalf("calls = %#v, want ModifyVpcAttribute plus three readiness probes", caller.calls)
	}
	if caller.calls[0].operation != "ModifyVpcAttribute" || caller.calls[1].operation != "DescribeVpcAttribute" || caller.calls[2].operation != "DescribeVpcAttribute" || caller.calls[3].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.Item["dns_hostname_status"] != "ENABLED" {
		t.Fatalf("result = %#v, want stable DNS hostname status", result)
	}
}

func TestExecuteVPCUpdateRetriesTransientDnsHostnameModification(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	binding := loaded.Bindings["update_attributes"]
	binding.Retry.InitialInterval = "1ms"
	binding.Retry.MaxInterval = "1ms"
	binding.Retry.Timeout = "20ms"
	loaded.Bindings["update_attributes"] = binding
	waiterSpec := loaded.Waiters["available_after_update"]
	waiterSpec.Interval = "1ms"
	loaded.Waiters["available_after_update"] = waiterSpec
	caller := &fakeCaller{
		errors: []error{
			ecerrors.Service("CloudAPIError", "DnsHostName resource is in transient state", false,
				ecerrors.WithRawCause("OperationFailed.ModifyVpcDnsHostname", "DnsHostName resource is in transient state")),
		},
		responses: []map[string]any{
			{"RequestId": "req-update"},
			{
				"RequestId":            "req-ready",
				"VpcId":                "vpc-123",
				"VpcName":              "prod-network",
				"CidrBlock":            "10.0.0.0/16",
				"Status":               "Available",
				"DnsHostnameStatus":    "DISABLED",
				"DhcpOptionsSetStatus": "",
				"RegionId":             "cn-beijing",
			},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "update",
		Input: map[string]any{
			"id":                  "vpc-123",
			"enable_dns_hostname": false,
		},
		Context: map[string]any{"region": "cn-beijing"},
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("calls = %#v, want failed ModifyVpcAttribute, retried ModifyVpcAttribute, readiness probe", caller.calls)
	}
	if caller.calls[0].operation != "ModifyVpcAttribute" || caller.calls[1].operation != "ModifyVpcAttribute" || caller.calls[2].operation != "DescribeVpcAttribute" {
		t.Fatalf("calls = %#v", caller.calls)
	}
	if result.Item["dns_hostname_status"] != "DISABLED" {
		t.Fatalf("result = %#v, want disabled DNS hostname status", result)
	}
}

func TestExecuteVPCCreateWaitsForDnsHostnameAndDhcpStatusToStabilize(t *testing.T) {
	loaded, err := spec.LoadResource("../../specs", "vpc", "vpc")
	if err != nil {
		t.Fatalf("LoadResource: %v", err)
	}
	waiterSpec := loaded.Waiters["available_after_create"]
	waiterSpec.Interval = "1ms"
	loaded.Waiters["available_after_create"] = waiterSpec
	caller := &fakeCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "VpcId": "vpc-new"},
			{
				"RequestId":         "req-dns-enabling",
				"VpcId":             "vpc-new",
				"VpcName":           "prod-network",
				"CidrBlock":         "10.0.0.0/16",
				"Status":            "Available",
				"DnsHostnameStatus": "ENABLING",
				"RegionId":          "cn-beijing",
			},
			{
				"RequestId":            "req-dhcp-pending",
				"VpcId":                "vpc-new",
				"VpcName":              "prod-network",
				"CidrBlock":            "10.0.0.0/16",
				"Status":               "Available",
				"DnsHostnameStatus":    "ENABLED",
				"DhcpOptionsSetStatus": "Pending",
				"RegionId":             "cn-beijing",
			},
			{
				"RequestId":            "req-ready",
				"VpcId":                "vpc-new",
				"VpcName":              "prod-network",
				"CidrBlock":            "10.0.0.0/16",
				"Status":               "Available",
				"DnsHostnameStatus":    "ENABLED",
				"DhcpOptionsSetStatus": "InUse",
				"RegionId":             "cn-beijing",
			},
		},
	}

	result, err := NewExecutor(loaded, caller).Execute(testContext(), Request{
		Action: "create",
		Input: map[string]any{
			"name":                "prod-network",
			"cidr":                "10.0.0.0/16",
			"enable_dns_hostname": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 4 {
		t.Fatalf("calls = %#v, want CreateVpc plus three readiness probes", caller.calls)
	}
	if result.Item["dhcp_options_set_status"] != "InUse" {
		t.Fatalf("result = %#v, want stable DHCP options status", result)
	}
}

type callRecord struct {
	operation string
	request   map[string]any
}

type fakeCaller struct {
	calls     []callRecord
	errors    []error
	responses []map[string]any
}

func (f *fakeCaller) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, callRecord{operation: operation, request: request})
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.responses) == 0 {
		return map[string]any{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func fakeVPCSpecForEngine(t *testing.T) spec.ResourceSpec {
	t.Helper()
	loaded, err := spec.LoadFile("../../specs/vpc/vpc.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if err := spec.Validate(loaded); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return loaded
}

func testContext() context.Context {
	return context.Background()
}
