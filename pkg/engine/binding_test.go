package engine

import (
	"context"
	"reflect"
	"testing"

	"ecctl/pkg/spec"
	_ "ecctl/specs/ecs"
)

func TestResolveBindingRequestExpandsObjectArrayFields(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"ImageId": "$.image",
		"DataDisk": map[string]any{
			"each": "$.data_disks",
			"fields": map[string]any{
				"Category":             "$.category",
				"AutoSnapshotPolicyId": "$.auto_snapshot_policy",
			},
		},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"image": "aliyun_3",
		"data_disks": []any{
			map[string]any{"category": "cloud_essd", "auto_snapshot_policy": "sp-123"},
		},
	}}

	got, _, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	want := map[string]any{
		"ImageId":                         "aliyun_3",
		"DataDisk.1.Category":             "cloud_essd",
		"DataDisk.1.AutoSnapshotPolicyId": "sp-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestResolveBindingRequestExpandsScalarArray(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"SecurityGroupIds": map[string]any{"each": "$.security_group_ids"},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"security_group_ids": []any{"sg-a", "sg-b"},
	}}

	got, _, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	want := map[string]any{"SecurityGroupIds.1": "sg-a", "SecurityGroupIds.2": "sg-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestResolveBindingRequestExpandsFromObjectAndRawAPIParams(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"SystemDisk": map[string]any{
			"from": "$.system_disk",
			"fields": map[string]any{
				"Category": "$.category",
			},
		},
		"ApiParam": map[string]any{"raw": "$.api_param"},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"system_disk": map[string]any{"category": "cloud_essd"},
		"api_param":   []string{"DryRun=true"},
	}}

	got, _, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	want := map[string]any{
		"SystemDisk.Category": "cloud_essd",
		"DryRun":              "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestResolveBindingRequestKeepsRawObjectWhenFromHasNoFields(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"SystemDisk": map[string]any{"from": "$.system_disk"},
		"JSONDisk":   map[string]any{"from": "$.json_disk"},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"system_disk": map[string]any{"Category": "cloud_essd"},
		"json_disk":   `{"Category":"cloud_ssd"}`,
	}}

	got, _, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	if got["SystemDisk"] != `{"Category":"cloud_essd"}` || got["JSONDisk"] != `{"Category":"cloud_ssd"}` {
		t.Fatalf("request = %#v", got)
	}
}

func TestResolveBindingRequestRejectsInvalidFromAndRawValues(t *testing.T) {
	cases := []struct {
		name    string
		binding spec.Binding
		input   map[string]any
	}{
		{
			name:    "from scalar",
			binding: spec.Binding{Request: map[string]any{"SystemDisk": map[string]any{"from": "$.system_disk"}}},
			input:   map[string]any{"system_disk": "cloud_essd"},
		},
		{
			name:    "raw missing equals",
			binding: spec.Binding{Request: map[string]any{"ApiParam": map[string]any{"raw": "$.api_param"}}},
			input:   map[string]any{"api_param": []string{"DryRun"}},
		},
		{
			name: "raw duplicate",
			binding: spec.Binding{Request: map[string]any{
				"DryRun":   true,
				"ApiParam": map[string]any{"raw": "$.api_param"},
			}},
			input: map[string]any{"api_param": []string{"DryRun=false"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ResolveBindingRequest(tc.binding, ExecutionContext{Input: tc.input})
			if err == nil {
				t.Fatal("ResolveBindingRequest succeeded, want error")
			}
		})
	}
}

func TestResolveBindingRequestExpandsNestedEach(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"NetworkInterface": map[string]any{
			"each": "$.network_interfaces",
			"fields": map[string]any{
				"SecurityGroupIds": map[string]any{"each": "$.security_group_ids"},
			},
		},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"network_interfaces": []any{
			map[string]any{"security_group_ids": []any{"sg-a", "sg-b"}},
		},
	}}

	got, _, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	want := map[string]any{
		"NetworkInterface.1.SecurityGroupIds.1": "sg-a",
		"NetworkInterface.1.SecurityGroupIds.2": "sg-b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestResolveBindingRequestCapturesSelectedFields(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"Permissions": map[string]any{
			"capture": map[string]any{
				"name": "rules",
				"fields": map[string]any{
					"protocol": "$.protocol",
					"cidr":     "$.cidr",
				},
			},
			"each": "$.rules",
			"fields": map[string]any{
				"IpProtocol":   "$.protocol",
				"SourceCidrIp": "$.cidr",
			},
		},
	}}
	ctx := ExecutionContext{Input: map[string]any{
		"rules": []any{
			map[string]any{"protocol": "tcp", "cidr": "0.0.0.0/0", "ignored": true},
		},
	}}

	_, captures, err := ResolveBindingRequest(binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	want := []map[string]any{{"protocol": "tcp", "cidr": "0.0.0.0/0"}}
	if !reflect.DeepEqual(captures["rules"].Items, want) {
		t.Fatalf("captures = %#v, want %#v", captures["rules"].Items, want)
	}
}

func TestResolveBindingRequestEnhancedEachValidatesSourcesAndEnums(t *testing.T) {
	cases := []struct {
		name     string
		eachSpec any
		input    map[string]any
	}{
		{
			name:     "source must be object",
			eachSpec: map[string]any{"sources": []any{"bad"}},
		},
		{
			name: "from fields expression must be string",
			eachSpec: map[string]any{"sources": []any{map[string]any{
				"from_fields": map[string]any{"protocol": 1},
			}}},
		},
		{
			name: "enum rejects value",
			eachSpec: map[string]any{
				"sources": []any{map[string]any{"from_fields": map[string]any{"protocol": "$.protocol"}}},
				"enum":    map[string]any{"protocol": []any{"tcp"}},
			},
			input: map[string]any{"protocol": "udp"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := spec.Binding{Request: map[string]any{
				"Permissions": map[string]any{
					"each":   tc.eachSpec,
					"fields": map[string]any{"IpProtocol": "$.protocol"},
				},
			}}
			_, _, err := ResolveBindingRequest(binding, ExecutionContext{Input: tc.input})
			if err == nil {
				t.Fatal("ResolveBindingRequest succeeded, want error")
			}
		})
	}
}

func TestResolveBindingRequestNormalizesRuleSourcesAndCapturesItems(t *testing.T) {
	binding := spec.Binding{Request: map[string]any{
		"Permissions": map[string]any{
			"capture": "rule_permissions",
			"each": map[string]any{
				"normalize": "security_group_rule",
				"sources": []any{
					map[string]any{
						"source": "$.rule",
					},
					map[string]any{
						"from_fields": map[string]any{
							"protocol": "$.protocol",
							"port":     "$.port",
							"cidr":     "$.cidr",
						},
						"when_any": []any{"protocol", "port", "cidr"},
					},
				},
				"defaults": map[string]any{"policy": "accept", "priority": 1},
			},
			"fields": map[string]any{
				"IpProtocol":   "$.protocol",
				"PortRange":    "$.port_range",
				"SourceCidrIp": "$.cidr",
				"Policy":       "$.policy",
				"Priority":     "$.priority",
			},
		},
	}}
	ctx := ExecutionContext{Input: map[string]any{"rule": []string{"tcp:80@0.0.0.0/0"}}}

	got, captures, err := ResolveResourceBindingRequest(spec.ResourceSpec{Product: "ecs", Resource: "sg"}, binding, ctx)
	if err != nil {
		t.Fatalf("ResolveBindingRequest: %v", err)
	}
	wantRequest := map[string]any{
		"Permissions.1.IpProtocol":   "tcp",
		"Permissions.1.PortRange":    "80/80",
		"Permissions.1.SourceCidrIp": "0.0.0.0/0",
		"Permissions.1.Policy":       "accept",
		"Permissions.1.Priority":     1,
	}
	if !reflect.DeepEqual(got, wantRequest) {
		t.Fatalf("request = %#v, want %#v", got, wantRequest)
	}
	if captures["rule_permissions"].Items[0]["port_range"] != "80/80" {
		t.Fatalf("captures = %#v", captures)
	}
}

func TestExecutorCreateUsesBindingRequest(t *testing.T) {
	resource := engineSpecForTest()
	caller := &fakeCaller{responses: []map[string]any{{
		"RequestId":      "req-1",
		"InstanceIdSets": map[string]any{"InstanceIdSet": []any{"i-123"}},
	}}}

	result, err := NewExecutor(resource, caller).Execute(context.Background(), Request{
		Action: "create",
		Input: map[string]any{
			"image":      "aliyun_3",
			"data_disks": []any{map[string]any{"category": "cloud_essd"}},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := caller.calls[0].request["DataDisk.1.Category"]; got != "cloud_essd" {
		t.Fatalf("DataDisk.1.Category = %#v", got)
	}
	if result.ID != "i-123" || result.RequestID != "req-1" {
		t.Fatalf("result = %#v", result)
	}
}

func engineSpecForTest() spec.ResourceSpec {
	return spec.ResourceSpec{
		SchemaVersion: 2,
		Product:       "ecs",
		Resource:      "instance",
		Kind:          "regional",
		Bindings: map[string]spec.Binding{
			"create_to_running": {
				API: "RunInstances",
				Request: map[string]any{
					"ImageId": "$.image",
					"DataDisk": map[string]any{
						"each":   "$.data_disks",
						"fields": map[string]any{"Category": "$.category"},
					},
				},
				IDFrom:        "$.InstanceIdSets.InstanceIdSet",
				RequestIDFrom: "$.RequestId",
			},
		},
		Operations: map[string]spec.Operation{
			"create": {Workflow: []spec.WorkflowStep{{Binding: "create_to_running"}}},
		},
	}
}
