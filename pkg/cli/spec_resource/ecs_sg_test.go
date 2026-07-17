package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestECSSecurityGroupCreateUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "SecurityGroupId": "sg-123"},
			{
				"RequestId":         "req-attr",
				"SecurityGroupId":   "sg-123",
				"SecurityGroupName": "web",
				"VpcId":             "vpc-123",
				"SecurityGroupType": "normal",
				"Permissions":       map[string]any{"Permission": []any{}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "create", "--region", "cn-beijing", "--vpc", "vpc-123", "--name", "web")
	if code != 0 {
		t.Fatalf("ecs sg create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateSecurityGroup" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["VpcId"] != "vpc-123" || fake.calls[0].request["SecurityGroupName"] != "web" || fake.calls[0].request["SecurityGroupType"] != "normal" {
		t.Fatalf("create request = %#v", fake.calls[0].request)
	}
	if _, ok := fake.calls[0].request["ClientToken"]; !ok {
		t.Fatalf("CreateSecurityGroup must receive ClientToken: %#v", fake.calls[0].request)
	}
	sg, _ := decodeObject(t, stdout)["security_group"].(map[string]any)
	if sg == nil || sg["id"] != "sg-123" || sg["vpc"] != "vpc-123" || sg["name"] != "web" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSSecurityGroupCreateHelpDoesNotExposeWaitFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "sg", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs sg create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--no-wait") || strings.Contains(stdout, "--timeout") {
		t.Fatalf("security group create should not expose wait flags:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--api-param") {
		t.Fatalf("security group create help missing --api-param:\n%s", stdout)
	}
}

func TestECSSecurityGroupCreateDoesNotSupportDryRun(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("ecs", "sg", "create", "--region", "cn-beijing", "--vpc", "vpc-123", "--dry-run")
	if code != 1 {
		t.Fatalf("ecs sg create --dry-run exit %d, want 1 stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownCommand" {
		t.Fatalf("error.code = %q, want UnknownCommand; stdout=%s", got, stdout)
	}
}

func TestECSSecurityGroupDeleteUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "delete", "sg-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs sg delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteSecurityGroup" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SecurityGroupId"] != "sg-123" {
		t.Fatalf("delete request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	sg, _ := out["security_group"].(map[string]any)
	if out["deleted"] != true || sg == nil || sg["id"] != "sg-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSSecurityGroupDeleteHelpDoesNotExposeWaitFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "sg", "delete", "--help")
	if code != 0 {
		t.Fatalf("ecs sg delete --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--no-wait") || strings.Contains(stdout, "--timeout") {
		t.Fatalf("security group delete should not expose wait flags:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--api-param") {
		t.Fatalf("security group delete help missing --api-param:\n%s", stdout)
	}
}

func TestECSSecurityGroupGetWithReferencesUsesReferencesProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeSecurityGroupAttributeResponse("sg-123", []any{}),
			fakeSecurityGroupReferencesResponse("sg-123", "sg-ref"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "get", "sg-123", "--region", "cn-beijing", "--with-references")
	if code != 0 {
		t.Fatalf("ecs sg get --with-references exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeSecurityGroupAttribute" || fake.calls[1].operation != "DescribeSecurityGroupReferences" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["SecurityGroupId.1"] != "sg-123" {
		t.Fatalf("references request = %#v", fake.calls[1].request)
	}
	out := decodeObject(t, stdout)
	sg, _ := out["security_group"].(map[string]any)
	if sg == nil || sg["id"] != "sg-123" {
		t.Fatalf("security_group = %#v; stdout=%s", sg, stdout)
	}
	references, _ := sg["references"].([]any)
	if len(references) != 1 {
		t.Fatalf("references = %#v; stdout=%s", sg["references"], stdout)
	}
	first, _ := references[0].(map[string]any)
	if first["security_group_id"] != "sg-123" {
		t.Fatalf("reference = %#v; stdout=%s", first, stdout)
	}
}

func TestECSSecurityGroupGetDoesNotEmitUnsupportedType(t *testing.T) {
	t.Parallel()
	response := fakeSecurityGroupAttributeResponse("sg-123", []any{})
	response["SecurityGroupType"] = "normal"
	fake := &fakeSpecCaller{responses: []map[string]any{response}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "get", "sg-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs sg get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	sg, _ := decodeObject(t, stdout)["security_group"].(map[string]any)
	if _, ok := sg["type"]; ok {
		t.Fatalf("security group get must not emit unsupported type: %s", stdout)
	}
}

func TestECSSecurityGroupUpdateAttributeUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-update"},
			map[string]any{
				"RequestId":         "req-attr",
				"SecurityGroupId":   "sg-123",
				"SecurityGroupName": "web-2",
				"Permissions":       map[string]any{"Permission": []any{}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "update", "sg-123", "--region", "cn-beijing", "--name", "web-2")
	if code != 0 {
		t.Fatalf("ecs sg update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifySecurityGroupAttribute" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SecurityGroupName"] != "web-2" || fake.calls[0].request["SecurityGroupId"] != "sg-123" {
		t.Fatalf("update request = %#v", fake.calls[0].request)
	}
	sg, _ := decodeObject(t, stdout)["security_group"].(map[string]any)
	if sg == nil || sg["name"] != "web-2" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSSecurityGroupUpdatePolicyUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-policy"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "update", "sg-123", "--region", "cn-beijing", "--inner-access-policy", "drop")
	if code != 0 {
		t.Fatalf("ecs sg update policy exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifySecurityGroupPolicy" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InnerAccessPolicy"] != "drop" {
		t.Fatalf("policy request = %#v", fake.calls[0].request)
	}
}

func TestECSSecurityGroupUpdateRuleIDOnlyFailsBeforeCloudCall(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("rule-id-only validation should fail before creating a caller")
		return nil, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "update", "sg-123", "--region", "cn-beijing", "--rule-id", "sgr-123")
	if code == 0 {
		t.Fatalf("ecs sg update --rule-id only succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "MissingParameter" {
		t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
	}
	message := errorMessage(t, stdout)
	if !strings.Contains(message, "--protocol") || !strings.Contains(message, "--port") || !strings.Contains(message, "--cidr") {
		t.Fatalf("message should mention mutable rule fields, got %q", message)
	}
}

func TestECSSecurityGroupUpdateRuleFieldsRequireRuleIDBeforeCloudCall(t *testing.T) {
	t.Parallel()
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("rule field validation should fail before creating a caller")
		return nil, nil
	})

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "rule field only",
			args: []string{"--protocol", "tcp"},
		},
		{
			name: "attribute plus rule field",
			args: []string{"--name", "web-2", "--protocol", "tcp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"ecs", "sg", "update", "sg-123", "--region", "cn-beijing"}, tt.args...)
			stdout, stderr, code := runCLI(args...)
			if code == 0 {
				t.Fatalf("ecs sg update %s succeeded stdout=%s stderr=%s", tt.name, stdout, stderr)
			}
			if got := errorCode(t, stdout); got != "MissingParameter" {
				t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
			}
			if message := errorMessage(t, stdout); !strings.Contains(message, "--rule-id") {
				t.Fatalf("message should mention --rule-id, got %q", message)
			}
		})
	}
}

func TestECSSecurityGroupUpdateRuleIDWithAPIParamUsesRuleBinding(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-rule"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "update", "sg-123", "--region", "cn-beijing", "--rule-id", "sgr-123", "--api-param", "Priority=2", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs sg update --rule-id --api-param exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifySecurityGroupRule" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["SecurityGroupRuleId"] != "sgr-123" || request["Priority"] != 2 {
		t.Fatalf("ModifySecurityGroupRule request = %#v", request)
	}
}

func TestECSSecurityGroupUpdateHelpDescribesReadBackNotWaiter(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "sg", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs sg update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, removed := range []string{"return before waiter completion", "wait timeout"} {
		if strings.Contains(stdout, removed) {
			t.Fatalf("security group update help should not mention %q:\n%s", removed, stdout)
		}
	}
	for _, want := range []string{"skip security group readback", "security group readback timeout"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("security group update help missing %q:\n%s", want, stdout)
		}
	}
}

func TestECSSecurityGroupUpdateEgressRuleUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-rule"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{
				map[string]any{
					"SecurityGroupRuleId": "sgr-egress",
					"Direction":           "egress",
					"IpProtocol":          "TCP",
					"PortRange":           "443/443",
					"DestCidrIp":          "0.0.0.0/0",
					"Policy":              "Accept",
					"Priority":            float64(1),
				},
			}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "update", "sg-123", "--region", "cn-beijing",
		"--rule-id", "sgr-egress", "--direction", "egress", "--protocol", "tcp", "--port", "443", "--cidr", "0.0.0.0/0")
	if code != 0 {
		t.Fatalf("ecs sg update egress rule exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifySecurityGroupEgressRule" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"SecurityGroupId":     "sg-123",
		"SecurityGroupRuleId": "sgr-egress",
		"IpProtocol":          "tcp",
		"PortRange":           "443/443",
		"DestCidrIp":          "0.0.0.0/0",
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
}

func TestECSSecurityGroupAuthorizeRulesUsesSpecDrivenBinding(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-authorize"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{
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
			}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "authorize", "sg-123", "--region", "cn-beijing",
		"--rule", "tcp:80@0.0.0.0/0",
		"--rule", "ingress:tcp:443:0.0.0.0/0")
	if code != 0 {
		t.Fatalf("ecs sg authorize exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "AuthorizeSecurityGroup" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"SecurityGroupId":            "sg-123",
		"Permissions.1.IpProtocol":   "tcp",
		"Permissions.1.PortRange":    "80/80",
		"Permissions.1.SourceCidrIp": "0.0.0.0/0",
		"Permissions.2.IpProtocol":   "tcp",
		"Permissions.2.PortRange":    "443/443",
		"Permissions.2.SourceCidrIp": "0.0.0.0/0",
	} {
		if got := request[key]; got != want {
			t.Fatalf("request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	out := decodeObject(t, stdout)
	if _, ok := out["actions"].([]any); !ok {
		t.Fatalf("actions missing from output: %s", stdout)
	}
	sg, _ := out["security_group"].(map[string]any)
	if sg == nil || sg["id"] != "sg-123" {
		t.Fatalf("security_group = %#v; stdout=%s", sg, stdout)
	}
	rules, _ := out["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("rules = %#v, want 2; stdout=%s", out["rules"], stdout)
	}
	first, _ := rules[0].(map[string]any)
	second, _ := rules[1].(map[string]any)
	if first["id"] != "sgr-http" || second["id"] != "sgr-https" {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestECSSecurityGroupAuthorizeEgressUsesDirectionSelectedAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-authorize-egress"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{
				map[string]any{
					"SecurityGroupRuleId": "sgr-egress",
					"Direction":           "egress",
					"IpProtocol":          "TCP",
					"PortRange":           "443/443",
					"DestCidrIp":          "0.0.0.0/0",
					"Policy":              "Accept",
					"Priority":            float64(1),
				},
			}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "authorize", "sg-123", "--region", "cn-beijing",
		"--direction", "egress", "--rule", "tcp:443@0.0.0.0/0")
	if code != 0 {
		t.Fatalf("ecs sg authorize egress exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "AuthorizeSecurityGroupEgress" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["Permissions.1.DestCidrIp"] != "0.0.0.0/0" || request["Permissions.1.SourceCidrIp"] != nil {
		t.Fatalf("authorize egress request = %#v", request)
	}
	rule, _ := decodeObject(t, stdout)["rule"].(map[string]any)
	if rule == nil || rule["id"] != "sgr-egress" || rule["direction"] != "egress" {
		t.Fatalf("rule = %#v; stdout=%s", rule, stdout)
	}
}

func TestECSSecurityGroupAuthorizeFlatRuleUsesBindingSources(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-authorize"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{
				map[string]any{
					"SecurityGroupRuleId": "sgr-http",
					"Direction":           "ingress",
					"IpProtocol":          "TCP",
					"PortRange":           "80/80",
					"SourceCidrIp":        "0.0.0.0/0",
					"Policy":              "Accept",
					"Priority":            float64(5),
				},
			}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "authorize", "sg-123", "--region", "cn-beijing",
		"--protocol", "tcp", "--port", "80", "--cidr", "0.0.0.0/0", "--priority", "5")
	if code != 0 {
		t.Fatalf("ecs sg authorize flat exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	request := fake.calls[0].request
	if request["Permissions.1.PortRange"] != "80/80" || request["Permissions.1.Priority"] != 5 {
		t.Fatalf("authorize request = %#v", request)
	}
	out := decodeObject(t, stdout)
	rule, _ := out["rule"].(map[string]any)
	if rule == nil || rule["id"] != "sgr-http" || rule["port"] != "80" || rule["priority"] != float64(5) {
		t.Fatalf("rule = %#v; stdout=%s", rule, stdout)
	}
}

func TestECSSecurityGroupRevokeRuleIDUsesIndexedBinding(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-revoke"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "revoke", "sg-123", "--region", "cn-beijing", "--rule-id", "sgr-http")
	if code != 0 {
		t.Fatalf("ecs sg revoke exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "RevokeSecurityGroup" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SecurityGroupRuleId.1"] != "sgr-http" {
		t.Fatalf("revoke request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	if out["revoked"] != true {
		t.Fatalf("revoked = %#v, want true; stdout=%s", out["revoked"], stdout)
	}
	if _, ok := out["actions"].([]any); !ok {
		t.Fatalf("actions missing from output: %s", stdout)
	}
	rule, _ := out["rule"].(map[string]any)
	if rule == nil || rule["id"] != "sgr-http" {
		t.Fatalf("rule = %#v; stdout=%s", rule, stdout)
	}
	sg, _ := out["security_group"].(map[string]any)
	if sg == nil || sg["id"] != "sg-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSSecurityGroupRevokeEgressUsesDirectionSelectedAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-revoke-egress"},
			fakeSecurityGroupAttributeResponse("sg-123", []any{}),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "sg" {
			t.Fatalf("resource = %s/%s, want ecs/sg", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "sg", "revoke", "sg-123", "--region", "cn-beijing", "--direction", "egress", "--rule-id", "sgr-egress")
	if code != 0 {
		t.Fatalf("ecs sg revoke egress exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "RevokeSecurityGroupEgress" || fake.calls[1].operation != "DescribeSecurityGroupAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SecurityGroupRuleId.1"] != "sgr-egress" {
		t.Fatalf("revoke egress request = %#v", fake.calls[0].request)
	}
}
