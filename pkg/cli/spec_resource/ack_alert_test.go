package spec_resource

import (
	"reflect"
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func TestACKAlertCommandSurfaceUsesSpecDrivenBindings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		resource  string
		wantCall  string
		wantReq   map[string]any
		wantRoot  string
		wantState map[string]any
	}{
		{
			name:     "alert update starts alert",
			args:     []string{"ack", "alert", "update", "--region", "cn-beijing", "--cluster", "c-123", "--enabled=true", "--rule-id", "cpu-high"},
			resource: "alert",
			wantCall: "StartAlert",
			wantReq: map[string]any{
				"ClusterId":             "c-123",
				"alert_rule_name":       "cpu-high",
				"alert_rule_group_name": nil,
			},
			wantRoot: "alert",
			wantState: map[string]any{
				"cluster": "c-123",
				"enabled": true,
				"rule_id": "cpu-high",
			},
		},
		{
			name:     "alert update stops ruleset",
			args:     []string{"ack", "alert", "update", "--region", "cn-beijing", "--cluster", "c-123", "--enabled=false", "--ruleset-id", "ack-cluster"},
			resource: "alert",
			wantCall: "StopAlert",
			wantReq: map[string]any{
				"ClusterId":             "c-123",
				"alert_rule_group_name": "ack-cluster",
			},
			wantRoot: "alert",
			wantState: map[string]any{
				"cluster":    "c-123",
				"enabled":    false,
				"ruleset_id": "ack-cluster",
			},
		},
		{
			name:     "contact delete",
			args:     []string{"ack", "alert", "contact", "delete", "12345", "--region", "cn-beijing"},
			resource: "contact",
			wantCall: "DeleteAlertContact",
			wantReq: map[string]any{
				"contact_ids.1": "12345",
			},
			wantRoot: "contact",
			wantState: map[string]any{
				"id": "12345",
			},
		},
		{
			name:     "contact-group update",
			args:     []string{"ack", "alert", "contact-group", "update", "--region", "cn-beijing", "--cluster", "c-123", "--ruleset-id", "ack-cluster", "--group-id", "12345"},
			resource: "contact-group",
			wantCall: "UpdateContactGroupForAlert",
			wantReq: map[string]any{
				"ClusterId":             "c-123",
				"alert_rule_group_name": "ack-cluster",
				"contact_group_ids.1":   "12345",
			},
			wantRoot: "contact_group",
			wantState: map[string]any{
				"cluster":    "c-123",
				"group_ids":  []any{"12345"},
				"ruleset_id": "ack-cluster",
			},
		},
		{
			name:     "contact-group delete",
			args:     []string{"ack", "alert", "contact-group", "delete", "12345", "--region", "cn-beijing"},
			resource: "contact-group",
			wantCall: "DeleteAlertContactGroup",
			wantReq: map[string]any{
				"contact_group_ids.1": "12345",
			},
			wantRoot: "contact_group",
			wantState: map[string]any{
				"id": "12345",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-ack", "status": true, "msg": "success"}}}
			runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
				if resource.Product != "ack" || resource.Resource != tt.resource {
					t.Fatalf("resource = %s/%s, want ack/%s", resource.Product, resource.Resource, tt.resource)
				}
				if resource.APIProduct != "CS" {
					t.Fatalf("api_product = %q, want CS", resource.APIProduct)
				}
				if region != "cn-beijing" {
					t.Fatalf("region = %q, want cn-beijing", region)
				}
				return fake, nil
			})

			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%s exit %d stderr=%s stdout=%s", strings.Join(tt.args, " "), code, stderr, stdout)
			}
			if len(fake.calls) != 1 || fake.calls[0].operation != tt.wantCall {
				t.Fatalf("calls = %#v, want %s", fake.calls, tt.wantCall)
			}
			for key, want := range tt.wantReq {
				if want == nil {
					if _, ok := fake.calls[0].request[key]; ok {
						t.Fatalf("%s should be omitted from request %#v", key, fake.calls[0].request)
					}
					continue
				}
				if !valuesEqual(fake.calls[0].request[key], want) {
					t.Fatalf("%s request %s = %#v, want %#v; request=%#v", tt.name, key, fake.calls[0].request[key], want, fake.calls[0].request)
				}
			}
			out := decodeObject(t, stdout)
			actions, _ := out["actions"].([]any)
			if len(actions) != 1 {
				t.Fatalf("actions missing from output: %s", stdout)
			}
			root, _ := out[tt.wantRoot].(map[string]any)
			if root == nil {
				t.Fatalf("%s root missing from output: %s", tt.wantRoot, stdout)
			}
			for key, want := range tt.wantState {
				if !valuesEqual(root[key], want) {
					t.Fatalf("%s output %s = %#v, want %#v; stdout=%s", tt.name, key, root[key], want, stdout)
				}
			}
		})
	}
}

func TestACKAlertHelpExposesExpectedFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "alert update help",
			args: []string{"ack", "alert", "update", "--help"},
			want: []string{"--cluster", "--enabled", "--rule-id", "--ruleset-id"},
		},
		{
			name: "contact delete help",
			args: []string{"ack", "alert", "contact", "delete", "--help"},
			want: []string{"delete <id>"},
		},
		{
			name: "contact-group update help",
			args: []string{"ack", "alert", "contact-group", "update", "--help"},
			want: []string{"--cluster", "--rule-id", "--ruleset-id", "--group-id"},
		},
		{
			name: "contact-group delete help",
			args: []string{"ack", "alert", "contact-group", "delete", "--help"},
			want: []string{"delete <id>"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%s exit %d stderr=%s stdout=%s", strings.Join(tt.args, " "), code, stderr, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("help missing %q; stdout=%s", want, stdout)
				}
			}
		})
	}
}

func valuesEqual(got, want any) bool {
	return reflect.DeepEqual(got, want)
}
