package spec_resource

import (
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func fakeACKOperationPlansResponse(id string, state string) map[string]any {
	return map[string]any{
		"request_id": "req-list-operation-plans",
		"plans": []any{
			map[string]any{
				"plan_id":      id,
				"cluster_id":   "c-123",
				"created":      "2026-06-01T10:00:00+08:00",
				"start_time":   "2026-06-02T10:00:00+08:00",
				"end_time":     "2026-06-02T12:00:00+08:00",
				"state":        state,
				"type":         "CLUSTER_UPGRADE_MASTER",
				"target_type":  "cluster",
				"target_id":    "c-123",
				"task_id":      "T-123",
				"state_reason": map[string]any{"code": "CanceledByUser", "message": "plan has been canceled by user"},
			},
		},
	}
}

func ackOperationPlanCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "operation-plan" || resource.APIProduct != "cs" {
			t.Fatalf("resource = %#v, want ack/operation-plan with cs API product", resource)
		}
		return fake, nil
	})
}

func TestACKOperationPlanListRoutesDefaultAndByRegion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			fakeACKOperationPlansResponse("P-123", "Scheduled"),
			fakeACKOperationPlansResponse("P-456", "Scheduled"),
		},
	}
	runCLI := ackOperationPlanCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ack", "operation-plan", "list",
		"--region", "cn-beijing",
		"--cluster", "c-123",
		"--type", "CLUSTER_UPGRADE_MASTER",
		"--limit", "100",
	)
	if code != 0 {
		t.Fatalf("ack operation-plan list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListOperationPlans" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["cluster_id"] != "c-123" || fake.calls[0].request["type"] != "CLUSTER_UPGRADE_MASTER" {
		t.Fatalf("ListOperationPlans request = %#v", fake.calls[0].request)
	}
	if _, ok := fake.calls[0].request["region_id"]; ok {
		t.Fatalf("account-level ListOperationPlans must not send region_id: %#v", fake.calls[0].request)
	}
	plans, _ := decodeObject(t, stdout)["operation_plans"].([]any)
	if len(plans) != 1 {
		t.Fatalf("operation_plans = %#v; stdout=%s", plans, stdout)
	}

	stdout, stderr, code = runCLI(
		"ack", "operation-plan", "list",
		"--region", "cn-beijing",
		"--by-region",
		"--cluster", "c-123",
		"--state", "Scheduled",
	)
	if code != 0 {
		t.Fatalf("ack operation-plan list --by-region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[1].operation != "ListOperationPlansForRegion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[1].request
	if request["region_id"] != "cn-beijing" || request["cluster_id"] != "c-123" || request["state"] != "Scheduled" {
		t.Fatalf("ListOperationPlansForRegion request = %#v", request)
	}
}

func TestACKOperationPlanAliasGetUsesListOperationPlans(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeACKOperationPlansResponse("P-123", "Scheduled")}}
	runCLI := ackOperationPlanCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "op", "get", "P-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack op get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListOperationPlans" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["plan_id"] != "P-123" {
		t.Fatalf("ListOperationPlans get request = %#v", fake.calls[0].request)
	}
	plan, _ := decodeObject(t, stdout)["operation_plan"].(map[string]any)
	if plan == nil || plan["id"] != "P-123" {
		t.Fatalf("unexpected get output: %s", stdout)
	}
}

func TestACKOperationPlanCancelCallsCancelAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"request_id": "req-cancel-operation-plan"},
			fakeACKOperationPlansResponse("P-123", "Canceled"),
		},
	}
	runCLI := ackOperationPlanCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "op", "cancel", "P-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack op cancel exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CancelOperationPlan" || fake.calls[1].operation != "ListOperationPlans" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["plan_id"] != "P-123" {
		t.Fatalf("CancelOperationPlan request = %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["plan_id"] != "P-123" {
		t.Fatalf("cancel readback request = %#v", fake.calls[1].request)
	}
}

func TestACKOperationPlanHelpExposesDesignedFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ack", "operation-plan", "list", "--help")
	if code != 0 {
		t.Fatalf("ack operation-plan list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster", "--by-region", "--limit"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list help missing %s:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "op", "cancel", "--help")
	if code != 0 {
		t.Fatalf("ack op cancel --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "plan") {
		t.Fatalf("cancel help missing plan argument context:\n%s", stdout)
	}
}
