package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ecctl/pkg/spec"
)

func TestProductSurfacesStayUnderAgentBudget(t *testing.T) {
	for _, product := range []string{"ack", "ecs", "lingjun", "rg", "tag", "vpc"} {
		surface, ok := ProductList(product)
		if !ok {
			t.Fatalf("ProductList(%q) not found", product)
		}
		raw, err := MarshalCompact(surface)
		if err != nil {
			t.Fatalf("MarshalCompact(%q): %v", product, err)
		}
		// Product-level budgets keep the agent-facing resource catalog small.
		// They are ceilings sized for the full designed ECS surface (~34
		// resources); the tight per-resource guards below (action count and
		// per-action schema size) are what really bound an agent's context.
		if len(raw) > 6144 {
			t.Fatalf("%s product schema is %d bytes, want <= 6144", product, len(raw))
		}

		var decoded struct {
			Resources []struct {
				Name    string   `json:"name"`
				Actions []string `json:"actions"`
			} `json:"resources"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s product schema is not JSON: %v", product, err)
		}
		if len(decoded.Resources) == 0 {
			t.Fatalf("%s product schema has no resources", product)
		}
		if len(decoded.Resources) > 40 {
			t.Fatalf("%s product schema has %d resources, want <= 40", product, len(decoded.Resources))
		}
		for _, resource := range decoded.Resources {
			if len(resource.Actions) > 12 {
				t.Fatalf("%s.%s has %d actions, want <= 12", product, resource.Name, len(resource.Actions))
			}
		}
	}
}

func TestProductsReturnsSupportedSurfaces(t *testing.T) {
	got := Products()
	want := []string{"ack", "ecs", "lingjun", "rg", "tag", "vpc"}
	if len(got) != len(want) {
		t.Fatalf("Products() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Products()[%d] = %q, want %q", i, got[i], want[i])
		}
		if _, ok := ProductList(got[i]); !ok {
			t.Fatalf("Products()[%d] = %q has no ProductList surface", i, got[i])
		}
	}
}

func TestProductSurfacesUseLocalizedSpecDescriptions(t *testing.T) {
	surface, ok := ProductListForLanguage("ecs", "zh-CN")
	if !ok {
		t.Fatal("ProductListForLanguage(ecs, zh-CN) not found")
	}
	if surface.Description != "管理云服务器 ECS 资源，涵盖实例、块存储云盘与快照、镜像、安全组、弹性网卡（ENI）、密钥对、启动模板以及云助手命令。" {
		t.Fatalf("product description = %q, want spec zh product description", surface.Description)
	}
	seenInstance := false
	for _, resource := range surface.Resources {
		if resource.Name == "instance" {
			seenInstance = true
			if resource.Description != "管理实例资源" {
				t.Fatalf("instance description = %q, want spec zh description", resource.Description)
			}
		}
	}
	if !seenInstance {
		t.Fatalf("ecs product surface missing instance resource: %#v", surface.Resources)
	}

	products := ProductsForLanguage("zh-CN")
	if len(products) == 0 || products[0].Description == "" {
		t.Fatalf("ProductsForLanguage should include localized descriptions: %#v", products)
	}
}

func TestProductSurfaceExposesNestedResourceParent(t *testing.T) {
	surface, ok := ProductList("rg")
	if !ok {
		t.Fatal("ProductList(rg) not found")
	}
	for _, resource := range surface.Resources {
		if resource.Name == "version" {
			if resource.Parent != "policy" {
				t.Fatalf("rg.version parent = %q, want policy", resource.Parent)
			}
			raw, err := json.Marshal(resource)
			if err != nil {
				t.Fatalf("marshal rg policy version surface: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal rg policy version surface: %v", err)
			}
			if decoded["schema_id"] != "rg.policy.version" {
				t.Fatalf("rg policy version schema_id = %#v, want rg.policy.version", decoded["schema_id"])
			}
			return
		}
	}
	t.Fatal("rg product surface missing version resource")
}

func TestVPCCreateSchemasExist(t *testing.T) {
	for _, name := range []string{
		"vpc.vpc.create",
		"vpc.create",
	} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		raw, err := MarshalCompact(command)
		if err != nil {
			t.Fatalf("%s MarshalCompact: %v", name, err)
		}
		limit := 2048
		paramLimit := 30
		switch name {
		case "ecs.instance.create":
			limit = 12000
			paramLimit = 120
		case "ecs.instance.update":
			limit = 2300
		}
		if len(raw) > limit {
			t.Fatalf("%s schema is %d bytes, want <= %d", name, len(raw), limit)
		}
		if len(command.Params) > paramLimit {
			t.Fatalf("%s has %d params, want <= %d", name, len(command.Params), paramLimit)
		}
	}
}

func TestVPCActionSchemasExist(t *testing.T) {
	for _, name := range []string{
		"vpc.vpc.list",
		"vpc.vpc.get",
		"vpc.vpc.update",
		"vpc.vpc.delete",
		"vpc.list",
		"vpc.get",
		"vpc.update",
		"vpc.delete",
	} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		raw, err := MarshalCompact(command)
		if err != nil {
			t.Fatalf("%s MarshalCompact: %v", name, err)
		}
		limit := 2048
		paramLimit := 30
		switch name {
		case "ecs.instance.create":
			limit = 12000
			paramLimit = 120
		case "ecs.instance.update":
			limit = 2300
		}
		if len(raw) > limit {
			t.Fatalf("%s schema is %d bytes, want <= %d", name, len(raw), limit)
		}
		if len(command.Params) > paramLimit {
			t.Fatalf("%s has %d params, want <= %d", name, len(command.Params), paramLimit)
		}
	}
}

func TestVSwitchActionSchemasExist(t *testing.T) {
	for _, name := range []string{
		"vpc.vswitch.list",
		"vpc.vswitch.get",
		"vpc.vswitch.create",
		"vpc.vswitch.update",
		"vpc.vswitch.delete",
	} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		raw, err := MarshalCompact(command)
		if err != nil {
			t.Fatalf("%s MarshalCompact: %v", name, err)
		}
		if len(raw) > 2048 {
			t.Fatalf("%s schema is %d bytes, want <= 2048", name, len(raw))
		}
		if len(command.Params) > 30 {
			t.Fatalf("%s has %d params, want <= 30", name, len(command.Params))
		}
	}
	if _, ok := Command("vpc.vsw.create"); ok {
		t.Fatal("vpc.vsw.create schema should remain absent; vsw is a CLI alias only")
	}
}

func TestVSwitchCreateSchemaDoesNotExposeUnsupportedDryRun(t *testing.T) {
	command, ok := Command("vpc.vswitch.create")
	if !ok {
		t.Fatal("vpc.vswitch.create schema not found")
	}
	if _, ok := command.Params["dry-run"]; ok {
		t.Fatalf("vpc.vswitch.create schema exposes unsupported dry-run: %#v", command.Params)
	}
}

func TestCommandSchemaExposesMutationContract(t *testing.T) {
	command, ok := Command("ecs.instance.create")
	if !ok {
		t.Fatal("ecs.instance.create schema not found")
	}
	decoded := marshalCommandObject(t, command)

	if decoded["schema_version"] != float64(1) {
		t.Fatalf("schema_version = %#v, want 1", decoded["schema_version"])
	}
	if decoded["kind"] != "mutation" {
		t.Fatalf("kind = %#v, want mutation", decoded["kind"])
	}
	risk := schemaObject(t, decoded, "risk")
	if risk["level"] != "medium" || risk["description"] == "" {
		t.Fatalf("risk = %#v, want medium risk with description", risk)
	}

	contract := schemaObject(t, decoded, "contract")
	dryRun := schemaObject(t, contract, "dry_run")
	if dryRun["supported"] != true || dryRun["flag"] != "dry-run" {
		t.Fatalf("dry_run contract = %#v, want supported dry-run flag", dryRun)
	}
	idempotency := schemaObject(t, contract, "idempotency")
	if idempotency["supported"] != true || idempotency["field"] != "ClientToken" || idempotency["prefix"] != "instance-create" || idempotency["mode"] != "explicit_or_auto_generated" {
		t.Fatalf("idempotency contract = %#v, want explicit-or-generated ClientToken", idempotency)
	}
	wait := schemaObject(t, contract, "wait")
	if wait["waitable"] != true || wait["no_wait_flag"] != "no-wait" || wait["timeout_flag"] != "timeout" {
		t.Fatalf("wait contract = %#v, want no-wait/timeout support", wait)
	}
	if wait["poll_command"] != "ecctl ecs instance get <id> --region <region> --output json" {
		t.Fatalf("poll_command = %#v", wait["poll_command"])
	}
	waiters, ok := wait["waiters"].([]any)
	if !ok || len(waiters) != 1 {
		t.Fatalf("waiters = %#v, want one waiter", wait["waiters"])
	}
	waiter := waiters[0].(map[string]any)
	if waiter["name"] != "running_after_create" || waiter["target"] != "Running" || waiter["interval"] != "2s" || waiter["timeout"] != "300s" {
		t.Fatalf("waiter = %#v, want running_after_create details", waiter)
	}
}

func TestOperationAPICallsPreserveWorkflowOrderConditionsAndCache(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes: map[string]spec.Probe{
			"state": {API: "DescribeThing"},
		},
		Waiters: map[string]spec.Waiter{
			"ready": {Probe: "state", Target: "Ready"},
		},
		Bindings: map[string]spec.Binding{
			"create_fast": {
				API:    "CreateThing",
				Wait:   "ready",
				IDFrom: "$.Id",
				Hooks: spec.BindingHooks{
					Before: []string{"resolve_name"},
					APICalls: []spec.HookAPICall{{
						Hook: "resolve_name", API: "DescribeThings", Phase: "preflight",
						Condition: "input.name != \"\"", Purpose: spec.LocalizedText{"en": "Resolve the thing name.", "zh-CN": "解析对象名称。"},
					}},
				},
			},
		},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "no_wait"}}},
		Workflow: []spec.WorkflowStep{
			{Binding: "create_fast", When: "input.mode == fast", WaitUnless: "input.no_wait"},
			{Probe: "state", IDs: []string{"$context.id"}, When: "input.mode == fast", Unless: "input.no_wait"},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	want := []APICall{
		{API: "DescribeThings", Phase: "preflight", Condition: `input.mode == fast && input.name != ""`, ConditionDescription: "When `--mode` equals `fast` and `--name` is specified.", Purpose: "Resolve the thing name."},
		{API: "CreateThing", Phase: "operation", Condition: "input.mode == fast", ConditionDescription: "When `--mode` equals `fast`.", Purpose: "Perform the resource operation."},
		{API: "DescribeThing", Phase: "wait", Condition: "input.mode == fast && !(input.no_wait)", ConditionDescription: "When `--mode` equals `fast` and `--no-wait` is not specified.", Purpose: "Poll until the resource reaches the target state.", Repeated: true},
		{API: "DescribeThing", Phase: "readback", Condition: "input.mode == fast && !(input.no_wait)", ConditionDescription: "When `--mode` equals `fast` and `--no-wait` is not specified.", Purpose: "Return the final resource view.", Cached: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operationAPICalls() = %#v\nwant %#v", got, want)
	}
}

func TestOperationAPICallsExposeReadProbeAndLocalizedPurpose(t *testing.T) {
	resource := spec.ResourceSpec{Probes: map[string]spec.Probe{"list": {API: "DescribeThings"}}}
	operation := spec.Operation{Workflow: []spec.WorkflowStep{{Probe: "list", Many: true}}}

	en := operationAPICalls(resource, operation, "en")
	zh := operationAPICalls(resource, operation, "zh-CN")
	if len(en) != 1 || en[0].API != "DescribeThings" || en[0].Phase != "readback" || en[0].Purpose != "Read the resource view." {
		t.Fatalf("English calls = %#v", en)
	}
	if len(zh) != 1 || zh[0].Purpose != "读取资源视图。" {
		t.Fatalf("Chinese calls = %#v", zh)
	}
}

func TestOperationAPICallsDoNotReuseExplicitWaitCacheForDifferentIDs(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:  map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters: map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
	}
	operation := spec.Operation{Workflow: []spec.WorkflowStep{
		{Wait: "ready"},
		{Probe: "state", IDs: []string{"$input.id"}},
	}}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 2 || got[1].Cached {
		t.Fatalf("api calls = %#v, explicit empty IDs must not cache $input.id probe", got)
	}
}

func TestOperationAPICallsDoNotAssumeOptionalBindingIDWasUsedByWaiter(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:   map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters:  map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{"update": {API: "UpdateThing", Wait: "ready"}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Fields: spec.OperationFields{
			{Name: "id"},
			{Name: "ids", Required: true},
		}},
		Workflow: []spec.WorkflowStep{
			{Binding: "update"},
			{Probe: "state", IDs: []string{"$input.id"}},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 3 || got[2].Cached {
		t.Fatalf("api calls = %#v, optional id must not be assumed as waiter identity", got)
	}
}

func TestOperationAPICallsDoNotIgnoreContextIDFromEarlierBinding(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:  map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters: map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{
			"prepare": {API: "PrepareThing", IDFrom: "$.PreparedId"},
			"update":  {API: "UpdateThing", Wait: "ready"},
		},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "id", Required: true}}},
		Workflow: []spec.WorkflowStep{
			{Binding: "prepare"},
			{Binding: "update"},
			{Probe: "state", IDs: []string{"$input.id"}},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 4 || got[3].Cached {
		t.Fatalf("api calls = %#v, earlier context id must prevent input-id cache assumption", got)
	}
}

func TestOperationAPICallsApplyDryRunOnlyAfterDryRunBinding(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes: map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Bindings: map[string]spec.Binding{"update": {
			API: "UpdateThing", Request: map[string]any{"DryRun": "$.dry_run"},
		}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "dry_run"}}},
		Workflow: []spec.WorkflowStep{
			{Probe: "state", IDs: []string{"$input.id"}},
			{Binding: "update"},
			{Probe: "state", IDs: []string{"$input.id"}},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 3 {
		t.Fatalf("api calls = %#v, want three calls", got)
	}
	if got[0].Condition != "" {
		t.Fatalf("pre-binding probe condition = %q, want unconditional", got[0].Condition)
	}
	if got[2].Condition != "!(input.dry_run)" {
		t.Fatalf("post-binding probe condition = %q, want dry-run suppression", got[2].Condition)
	}
}

func TestOperationAPICallsReuseConditionalDryRunWaiterResult(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:  map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters: map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{"create": {
			API: "CreateThing", IDFrom: "$.Id", Wait: "ready", Request: map[string]any{"DryRun": "$.dry_run"},
		}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "dry_run"}, {Name: "no_wait"}}},
		Workflow: []spec.WorkflowStep{
			{Binding: "create", When: "input.mode == fast", WaitUnless: "input.no_wait"},
			{Probe: "state", IDs: []string{"$context.id"}, When: "input.mode == fast", Unless: "input.no_wait"},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 3 || !got[2].Cached {
		t.Fatalf("api calls = %#v, conditional waiter result should be reused", got)
	}
	if got[1].Condition != `input.mode == fast && !(input.no_wait) && !(input.dry_run)` || got[2].Condition != got[1].Condition {
		t.Fatalf("wait condition = %q, readback condition = %q", got[1].Condition, got[2].Condition)
	}
}

func TestOperationAPICallsReuseExhaustiveSingleMultipleWaiterCache(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:  map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters: map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{
			"start_one":  {API: "StartThing", Wait: "ready"},
			"start_many": {API: "StartThings", Wait: "ready"},
		},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{
			Fields:   spec.OperationFields{{Name: "ids", Required: true}},
			Controls: spec.OperationFields{{Name: "no_wait"}},
		},
		Workflow: []spec.WorkflowStep{
			{Binding: "start_one", When: "single(input.ids)", WaitUnless: "input.no_wait"},
			{Binding: "start_many", When: "multiple(input.ids)", WaitUnless: "input.no_wait"},
			{Probe: "state", IDs: []string{"$input.ids"}, Many: true, Unless: "input.no_wait"},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 5 || !got[4].Cached {
		t.Fatalf("api calls = %#v, exhaustive single/multiple waiters must cover the final readback", got)
	}
	if got[4].Condition != "!(input.no_wait)" {
		t.Fatalf("final readback condition = %q, want only no-wait suppression", got[4].Condition)
	}
}

func TestOperationAPICallsDoNotReuseNonExhaustiveConditionalWaiterCache(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:   map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters:  map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{"start": {API: "StartThing", Wait: "ready"}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{
			Fields:   spec.OperationFields{{Name: "ids", Required: true}},
			Controls: spec.OperationFields{{Name: "no_wait"}},
		},
		Workflow: []spec.WorkflowStep{
			{Binding: "start", When: "single(input.ids)", WaitUnless: "input.no_wait"},
			{Probe: "state", IDs: []string{"$input.ids"}, Many: true, Unless: "input.no_wait"},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 3 || got[2].Cached {
		t.Fatalf("api calls = %#v, a non-exhaustive waiter branch must not cover the final readback", got)
	}
}

func TestOperationAPICallsDoNotTreatOptionalSingleMultipleInputAsExhaustive(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:  map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters: map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{
			"start_one":  {API: "StartThing", Wait: "ready"},
			"start_many": {API: "StartThings", Wait: "ready"},
		},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{
			Fields:   spec.OperationFields{{Name: "ids"}},
			Controls: spec.OperationFields{{Name: "no_wait"}},
		},
		Workflow: []spec.WorkflowStep{
			{Binding: "start_one", When: "single(input.ids)", WaitUnless: "input.no_wait"},
			{Binding: "start_many", When: "multiple(input.ids)", WaitUnless: "input.no_wait"},
			{Probe: "state", IDs: []string{"$input.ids"}, Many: true, Unless: "input.no_wait"},
		},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 5 || got[4].Cached {
		t.Fatalf("api calls = %#v, optional input may match neither selector and must remain uncached", got)
	}
}

func TestOperationAPICallsGroupDisjunctionBeforeConjunction(t *testing.T) {
	resource := spec.ResourceSpec{
		Probes:   map[string]spec.Probe{"state": {API: "DescribeThing"}},
		Waiters:  map[string]spec.Waiter{"ready": {Probe: "state", Target: "Ready"}},
		Bindings: map[string]spec.Binding{"update": {API: "UpdateThing", Wait: "ready"}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "no_wait"}}},
		Workflow: []spec.WorkflowStep{{
			Binding:    "update",
			When:       "has(input.name) || has(input.description)",
			WaitUnless: "input.no_wait",
		}},
	}

	got := operationAPICalls(resource, operation, "en")
	if len(got) != 2 {
		t.Fatalf("api calls = %#v, want operation and waiter", got)
	}
	want := "(has(input.name) || has(input.description)) && !(input.no_wait)"
	if got[1].Condition != want {
		t.Fatalf("wait condition = %q, want %q", got[1].Condition, want)
	}
}

func TestCombineAPIConditionsGroupsSeparatelyParenthesizedDisjunction(t *testing.T) {
	got := combineAPIConditions("(has(input.name)) || (has(input.description))", "!(input.no_wait)")
	want := "((has(input.name)) || (has(input.description))) && !(input.no_wait)"
	if got != want {
		t.Fatalf("combined condition = %q, want %q", got, want)
	}
}

func TestCombineAPIConditionsGroupsUnspacedDisjunction(t *testing.T) {
	got := combineAPIConditions("has(input.name)||has(input.description)", "!(input.no_wait)")
	want := "(has(input.name)||has(input.description)) && !(input.no_wait)"
	if got != want {
		t.Fatalf("combined condition = %q, want %q", got, want)
	}
}

func TestCommandSchemaIncludesAPICallsOnlyInFullMode(t *testing.T) {
	brief, ok := CommandForLanguageMode("vpc.vswitch.create", "en", CommandSchemaBrief)
	if !ok {
		t.Fatal("brief vpc.vswitch.create schema not found")
	}
	if len(brief.APICalls) != 0 {
		t.Fatalf("brief api calls = %#v, want omitted", brief.APICalls)
	}
	full, ok := CommandForLanguageMode("vpc.vswitch.create", "en", CommandSchemaFull)
	if !ok {
		t.Fatal("full vpc.vswitch.create schema not found")
	}
	if len(full.APICalls) < 3 || full.APICalls[0].API != "CreateVSwitch" || full.APICalls[1].API != "DescribeVSwitchAttributes" {
		t.Fatalf("full api calls = %#v", full.APICalls)
	}
	briefJSON, err := json.Marshal(brief)
	if err != nil {
		t.Fatalf("Marshal brief: %v", err)
	}
	if strings.Contains(string(briefJSON), `"api_calls"`) {
		t.Fatalf("brief schema includes api_calls: %s", briefJSON)
	}
}

func TestCommandSchemaExposesExecutableShape(t *testing.T) {
	command, ok := Command("ecs.sg.authorize")
	if !ok {
		t.Fatal("ecs.sg.authorize schema not found")
	}
	decoded := marshalCommandObject(t, command)

	if decoded["schema_id"] != "ecs.sg.authorize" {
		t.Fatalf("schema_id = %#v, want ecs.sg.authorize", decoded["schema_id"])
	}
	if decoded["cli"] != "ecctl ecs sg authorize" {
		t.Fatalf("cli = %#v, want executable command path", decoded["cli"])
	}
	if decoded["usage"] != "ecctl ecs sg authorize <id> [flags]" {
		t.Fatalf("usage = %#v, want positional ID usage", decoded["usage"])
	}

	positionals, ok := decoded["positionals"].([]any)
	if !ok || len(positionals) != 1 {
		t.Fatalf("positionals = %#v, want one positional", decoded["positionals"])
	}
	id := positionals[0].(map[string]any)
	if id["name"] != "id" || id["type"] != "string" || id["required"] != true || id["many"] == true {
		t.Fatalf("id positional = %#v, want required string single positional", id)
	}

	params := schemaObject(t, decoded, "params")
	idParam := schemaObject(t, params, "id")
	if idParam["positional"] != true {
		t.Fatalf("params.id = %#v, want positional marker for compatibility", idParam)
	}

	examples, ok := decoded["examples"].([]any)
	if !ok || len(examples) != 1 || !strings.Contains(examples[0].(string), "ecctl ecs sg authorize <sg-id>") {
		t.Fatalf("examples = %#v, want first canonical authorize example", decoded["examples"])
	}
}

func TestNestedResourceCommandSchemaRequiresFullResourcePath(t *testing.T) {
	command, ok := Command("rg.policy.version.list")
	if !ok {
		t.Fatal("rg.policy.version.list schema not found")
	}
	if command.Command != "rg.policy.version.list" {
		t.Fatalf("command = %q, want canonical nested schema ID", command.Command)
	}
	if command.CLI != "ecctl rg policy version list" {
		t.Fatalf("cli = %q, want nested executable command path", command.CLI)
	}
	if _, ok := Command("rg.version.list"); ok {
		t.Fatal("flattened rg.version.list schema must not be supported")
	}

	resource, ok := ResourceForLanguageName("rg.policy.version", "en")
	if !ok || resource.Name != "version" || resource.Parent != "policy" {
		t.Fatalf("rg.policy.version resource = %#v ok=%v", resource, ok)
	}
	if _, ok := ResourceForLanguageName("rg.version", "en"); ok {
		t.Fatal("flattened rg.version resource schema must not be supported")
	}
}

func TestHiddenACKNestedResourceSchemaRequiresFullResourcePath(t *testing.T) {
	command, ok := Command("ack.inspect.report.get")
	if !ok {
		t.Fatal("ack.inspect.report.get schema not found")
	}
	if command.CLI != "ecctl ack inspect report get" {
		t.Fatalf("cli = %q, want full nested command path", command.CLI)
	}
	if _, ok := Command("ack.report.get"); ok {
		t.Fatal("flattened ack.report.get schema must not be supported")
	}
}

func TestCommandSchemaExposesReadOutputShape(t *testing.T) {
	command, ok := Command("ecs.zone.list")
	if !ok {
		t.Fatal("ecs.zone.list schema not found")
	}
	decoded := marshalCommandObject(t, command)

	output := schemaObject(t, decoded, "output")
	if output["root"] != "zones" {
		t.Fatalf("output.root = %#v, want zones", output["root"])
	}
	fields, ok := output["fields"].([]any)
	if !ok {
		t.Fatalf("output.fields = %#v, want array", output["fields"])
	}
	for _, want := range []string{"id", "available_resource_creation", "available_instance_types"} {
		if !containsAnyString(fields, want) {
			t.Fatalf("output.fields missing %q: %#v", want, fields)
		}
	}
}

func TestECSAvailabilityListSchemaIsRemoved(t *testing.T) {
	if command, ok := Command("ecs.availability.list"); ok {
		t.Fatalf("ecs.availability.list schema should be removed, got %#v", command)
	}
}

func TestInstanceCreateAPICallConditionMatchesImageHookNormalization(t *testing.T) {
	command, ok := CommandForLanguageMode("ecs.instance.create", "en", CommandSchemaFull)
	if !ok {
		t.Fatal("ecs.instance.create schema not found")
	}
	for _, call := range command.APICalls {
		if call.API != "DescribeImages" {
			continue
		}
		want := `trim(input.image) != "" && !ends_with(lower(trim(input.image)), ".vhd")`
		if call.Condition != want {
			t.Fatalf("DescribeImages condition = %q, want %q", call.Condition, want)
		}
		if call.ConditionDescription != "When `--image` is not empty and does not end with `.vhd`." {
			t.Fatalf("DescribeImages condition description = %q", call.ConditionDescription)
		}
		return
	}
	t.Fatal("ecs.instance.create schema is missing DescribeImages preflight call")
}

func TestKeyPairCreateAPICallConditionsAreHumanReadable(t *testing.T) {
	tests := []struct {
		lang       string
		importWhen string
		createWhen string
	}{
		{lang: "en", importWhen: "When `--public-key` is specified.", createWhen: "When `--public-key` is not specified."},
		{lang: "zh-CN", importWhen: "指定 `--public-key` 时", createWhen: "未指定 `--public-key` 时"},
	}
	for _, tt := range tests {
		command, ok := CommandForLanguageMode("ecs.keypair.create", tt.lang, CommandSchemaFull)
		if !ok {
			t.Fatalf("ecs.keypair.create schema not found for %s", tt.lang)
		}
		got := map[string]string{}
		for _, call := range command.APICalls {
			got[call.API] = call.ConditionDescription
		}
		if got["ImportKeyPair"] != tt.importWhen {
			t.Fatalf("%s ImportKeyPair condition description = %q", tt.lang, got["ImportKeyPair"])
		}
		if got["CreateKeyPair"] != tt.createWhen {
			t.Fatalf("%s CreateKeyPair condition description = %q", tt.lang, got["CreateKeyPair"])
		}
	}
}

func TestAllAPICallsHaveHumanReadableConditionDescriptions(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		for _, product := range Products() {
			surface, ok := ProductListForLanguage(product, lang)
			if !ok {
				t.Fatalf("ProductListForLanguage(%q, %q) not found", product, lang)
			}
			for _, resource := range surface.Resources {
				for _, action := range resource.Actions {
					commandName := resource.SchemaID + "." + action
					command, ok := CommandForLanguageMode(commandName, lang, CommandSchemaFull)
					if !ok {
						t.Fatalf("CommandForLanguageMode(%q, %q) not found", commandName, lang)
					}
					for _, call := range command.APICalls {
						if call.ConditionDescription == "" {
							t.Fatalf("%s %s API %s condition %q has no human-readable description", lang, commandName, call.API, call.Condition)
						}
					}
				}
			}
		}
	}
}

func TestDescribeAPIConditionUsesCLIParameterSemantics(t *testing.T) {
	resource := spec.ResourceSpec{Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
		"ids":      {Type: "array", Items: &spec.SchemaField{Type: "string"}},
		"entries":  {Type: "string_array"},
		"ram_role": {Type: "string"},
	}}}
	operation := spec.Operation{Input: spec.OperationInput{
		Fields: spec.OperationFields{
			{Name: "ids", FlagName: "instance-id"},
			{Name: "entries"},
			{Name: "ram_role", AllowEmpty: true},
		},
		Controls: spec.OperationFields{{Name: "no_wait"}},
	}}
	tests := []struct {
		condition string
		lang      string
		want      string
	}{
		{condition: "single(input.ids)", lang: "zh-CN", want: "只提供一个 `--instance-id` 值时"},
		{condition: "single(input.ids) && !(input.no_wait)", lang: "zh-CN", want: "只提供一个 `--instance-id` 值且未指定 `--no-wait` 时"},
		{condition: "multiple(input.ids)", lang: "en", want: "When multiple `--instance-id` values are provided."},
		{condition: "specified(input.ram_role) && !has(input.ram_role)", lang: "zh-CN", want: "显式将 `--ram-role` 设置为空时"},
		{condition: "!(input.no_wait || !has(input.entries))", lang: "zh-CN", want: "未指定 `--no-wait` 且指定 `--entries` 时"},
		{condition: "!(input.no_wait) && !(input.dry_run)", lang: "zh-CN", want: "未指定 `--no-wait` 且未指定 `--dry-run` 时"},
		{condition: "!(context.target_template)", lang: "zh-CN", want: "前序步骤未生成 `target_template` 时"},
		{condition: "has(context.existing)", lang: "zh-CN", want: "前序步骤已生成 `existing` 时"},
		{condition: "!(has(context.existing))", lang: "en", want: "When the preceding step did not produce `existing`."},
		{condition: "(input.node || input.config || input.api_param) && !(input.no_wait)", lang: "zh-CN", want: "（指定 `--node` 或指定 `--config` 或指定 `--api-param`）且未指定 `--no-wait` 时"},
		{condition: "input.direction == egress", lang: "zh-CN", want: "`--direction` 等于 `egress` 时"},
	}
	for _, tt := range tests {
		got, ok := describeAPICondition(resource, operation, tt.condition, tt.lang)
		if !ok {
			t.Fatalf("describeAPICondition(%q, %q) was not handled", tt.condition, tt.lang)
		}
		if got != tt.want {
			t.Fatalf("describeAPICondition(%q, %q) = %q, want %q", tt.condition, tt.lang, got, tt.want)
		}
	}

	if got, ok := describeAPICondition(resource, operation, "mystery(input.ids)", "en"); ok || got != "" {
		t.Fatalf("unknown condition description = %q, %v; want empty, false", got, ok)
	}
}

func TestCommandSchemaOmitsOutputShapeForConditionalOutput(t *testing.T) {
	command, ok := Command("tag.policy.list")
	if !ok {
		t.Fatal("tag.policy.list schema not found")
	}
	decoded := marshalCommandObject(t, command)
	if _, ok := decoded["output"]; ok {
		t.Fatalf("tag.policy.list has conditional output roots and should not expose simplified output: %#v", decoded["output"])
	}
}

func TestRootResourcePollCommandDoesNotRepeatResourceName(t *testing.T) {
	command, ok := Command("vpc.vpc.create")
	if !ok {
		t.Fatal("vpc.vpc.create schema not found")
	}
	decoded := marshalCommandObject(t, command)
	contract := schemaObject(t, decoded, "contract")
	wait := schemaObject(t, contract, "wait")
	if wait["poll_command"] != "ecctl vpc get <id> --region <region> --output json" {
		t.Fatalf("poll_command = %#v", wait["poll_command"])
	}
}

func TestCommandSchemaMarksUnsupportedMutationContracts(t *testing.T) {
	command, ok := Command("ecs.instance.delete")
	if !ok {
		t.Fatal("ecs.instance.delete schema not found")
	}
	decoded := marshalCommandObject(t, command)

	if decoded["kind"] != "mutation" {
		t.Fatalf("kind = %#v, want mutation", decoded["kind"])
	}
	risk := schemaObject(t, decoded, "risk")
	if risk["level"] != "high" || risk["description"] == "" {
		t.Fatalf("risk = %#v, want high risk with description", risk)
	}
	contract := schemaObject(t, decoded, "contract")
	dryRun := schemaObject(t, contract, "dry_run")
	if dryRun["supported"] != false || dryRun["reason"] == "" {
		t.Fatalf("dry_run contract = %#v, want explicit unsupported reason", dryRun)
	}
	idempotency := schemaObject(t, contract, "idempotency")
	if idempotency["supported"] != false || idempotency["reason"] == "" {
		t.Fatalf("idempotency contract = %#v, want explicit unsupported reason", idempotency)
	}
}

func marshalCommandObject(t *testing.T, command CommandSchema) map[string]any {
	t.Helper()
	raw, err := MarshalCompact(command)
	if err != nil {
		t.Fatalf("MarshalCompact: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("schema JSON = %s: %v", raw, err)
	}
	return decoded
}

func schemaObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	child, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return child
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestVSwitchListSchemaExposesSupportedFilters(t *testing.T) {
	command, ok := Command("vpc.vswitch.list")
	if !ok {
		t.Fatal("vpc.vswitch.list schema not found")
	}
	for name, wantType := range map[string]string{
		"id":   "array",
		"vpc":  "string",
		"zone": "string",
		"tag.": "key_value",
	} {
		filter, ok := command.Filters[name]
		if !ok {
			t.Fatalf("filter %q missing: %#v", name, command.Filters)
		}
		if filter.Type != wantType {
			t.Fatalf("filter %q type = %q, want %q", name, filter.Type, wantType)
		}
	}
}

func TestECSSecurityGroupActionSchemasExist(t *testing.T) {
	for _, name := range []string{
		"ecs.sg.list",
		"ecs.sg.get",
		"ecs.sg.create",
		"ecs.sg.update",
		"ecs.sg.delete",
		"ecs.sg.authorize",
		"ecs.sg.revoke",
	} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		raw, err := MarshalCompact(command)
		if err != nil {
			t.Fatalf("%s MarshalCompact: %v", name, err)
		}
		if len(raw) > 2048 {
			t.Fatalf("%s schema is %d bytes, want <= 2048", name, len(raw))
		}
		if len(command.Params) > 30 {
			t.Fatalf("%s has %d params, want <= 30", name, len(command.Params))
		}
	}
}

func TestECSSecurityGroupListSchemaExposesSupportedFilters(t *testing.T) {
	command, ok := Command("ecs.sg.list")
	if !ok {
		t.Fatal("ecs.sg.list schema not found")
	}
	for name, wantType := range map[string]string{
		"id":   "string",
		"ids":  "array",
		"vpc":  "string",
		"tag.": "key_value",
	} {
		filter, ok := command.Filters[name]
		if !ok {
			t.Fatalf("filter %q missing: %#v", name, command.Filters)
		}
		if filter.Type != wantType {
			t.Fatalf("filter %q type = %q, want %q", name, filter.Type, wantType)
		}
	}
}

func TestECSInstanceActionSchemasExist(t *testing.T) {
	for _, name := range []string{
		"ecs.instance.list",
		"ecs.instance.get",
		"ecs.instance.create",
		"ecs.instance.update",
		"ecs.instance.delete",
	} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		raw, err := MarshalCompact(command)
		if err != nil {
			t.Fatalf("%s MarshalCompact: %v", name, err)
		}
		limit := 2048
		paramLimit := 30
		switch name {
		case "ecs.instance.create":
			limit = 12000
			paramLimit = 120
		case "ecs.instance.update":
			limit = 2300
		}
		if len(raw) > limit {
			t.Fatalf("%s schema is %d bytes, want <= %d", name, len(raw), limit)
		}
		if len(command.Params) > paramLimit {
			t.Fatalf("%s has %d params, want <= %d", name, len(command.Params), paramLimit)
		}
	}
}

func TestSchemaUsesDescriptionsFromResourceSpec(t *testing.T) {
	command, ok := Command("ecs.instance.create")
	if !ok {
		t.Fatal("ecs.instance.create schema not found")
	}
	if command.Description != "Create instance" {
		t.Fatalf("description = %q, want spec action description", command.Description)
	}
	if command.Params["image"].Description != "ECS image ID or name" {
		t.Fatalf("image description = %q, want spec param description", command.Params["image"].Description)
	}
}

func TestSchemaCanUseChineseDescriptionsFromResourceSpec(t *testing.T) {
	zhCommand, ok := CommandForLanguage("ecs.instance.create", "zh-CN")
	if !ok {
		t.Fatal("ecs.instance.create zh schema not found")
	}
	if zhCommand.Description != "创建实例" {
		t.Fatalf("zh description = %q, want spec zh action description", zhCommand.Description)
	}
	if zhCommand.Params["image"].Description != "ECS 镜像 ID 或名称" {
		t.Fatalf("zh image description = %q, want spec zh param description", zhCommand.Params["image"].Description)
	}
}

func TestMutationSchemaExposesIdempotencyKey(t *testing.T) {
	command, ok := Command("vpc.vpc.create")
	if !ok {
		t.Fatal("vpc.vpc.create schema not found")
	}

	param, ok := command.Params["idempotency-key"]
	if !ok {
		t.Fatalf("idempotency-key param missing; params=%#v", command.Params)
	}
	if param.Type != "string" {
		t.Fatalf("idempotency-key type = %q, want string", param.Type)
	}
	if !strings.Contains(param.Description, "ClientToken") {
		t.Fatalf("idempotency-key description = %q, want ClientToken", param.Description)
	}

	decoded := marshalCommandObject(t, command)
	contract := schemaObject(t, decoded, "contract")
	idempotency := schemaObject(t, contract, "idempotency")
	if idempotency["mode"] != "explicit_or_auto_generated" {
		t.Fatalf("idempotency mode = %#v, want explicit_or_auto_generated", idempotency["mode"])
	}
}

func TestECSInstanceListSchemaExposesSupportedFilters(t *testing.T) {
	command, ok := Command("ecs.instance.list")
	if !ok {
		t.Fatal("ecs.instance.list schema not found")
	}
	for name, wantType := range map[string]string{
		"id":     "array",
		"ids":    "array",
		"status": "string",
		"tag.":   "key_value",
	} {
		filter, ok := command.Filters[name]
		if !ok {
			t.Fatalf("filter %q missing: %#v", name, command.Filters)
		}
		if filter.Type != wantType {
			t.Fatalf("filter %q type = %q, want %q", name, filter.Type, wantType)
		}
	}
}

func TestVPCListSchemaExposesSupportedFilters(t *testing.T) {
	command, ok := Command("vpc.vpc.list")
	if !ok {
		t.Fatal("vpc.vpc.list schema not found")
	}
	for name, wantType := range map[string]string{
		"id":       "array",
		"owner-id": "integer",
		"tag.":     "key_value",
	} {
		filter, ok := command.Filters[name]
		if !ok {
			t.Fatalf("filter %q missing: %#v", name, command.Filters)
		}
		if filter.Type != wantType {
			t.Fatalf("filter %q type = %q, want %q", name, filter.Type, wantType)
		}
	}
	if _, ok := command.Filters["cidr"]; ok {
		t.Fatalf("unexpected cidr filter: %#v", command.Filters)
	}
}

func TestListAndGetSchemasExposeFieldsParam(t *testing.T) {
	for _, name := range []string{"vpc.vpc.list", "vpc.vpc.get", "ecs.assistant.get"} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("%s schema not found", name)
		}
		fields, ok := command.Params["fields"]
		if !ok {
			t.Fatalf("%s schema missing fields param: %#v", name, command.Params)
		}
		if fields.Type != "string" || !strings.Contains(fields.Description, "resource fields") {
			t.Fatalf("%s fields param = %#v, want string resource fields description", name, fields)
		}
	}
}

func TestUnmigratedResourceSchemasAreAbsent(t *testing.T) {
	for _, name := range []string{
		"vpc.vsw.create",
		"vpc.vsw.delete",
	} {
		if _, ok := Command(name); ok {
			t.Fatalf("%s schema should be removed", name)
		}
	}
}

func TestSchemaDiscoversProductsAndCommandsFromSpecDir(t *testing.T) {
	specDir := t.TempDir()
	writeSchemaTestSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
identity:
  field: id
  output_root:
    one: demo
    many: demos
schema:
  fields:
    name:
      type: string
probes:
  list:
    api: DescribeDemos
    response:
      items: $.Demos.Demo
operations:
  create:
    examples:
      - ecctl demo create --name demo
    input:
      fields:
        - name:
            required: true
    workflow: []
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	got := Products()
	if len(got) != 1 || got[0] != "demo" {
		t.Fatalf("Products() = %#v, want demo from spec dir", got)
	}
	surface, ok := ProductList("demo")
	if !ok || len(surface.Resources) != 1 || surface.Resources[0].Name != "demo" {
		t.Fatalf("ProductList(demo) = %#v, %v", surface, ok)
	}
	if _, ok := ProductList("vpc"); ok {
		t.Fatal("ProductList(vpc) must not be hard-coded when spec dir contains only demo")
	}
	for _, name := range []string{"demo.demo.create", "demo.create"} {
		command, ok := Command(name)
		if !ok {
			t.Fatalf("Command(%q) not found", name)
		}
		if command.Params["name"].Type != "string" || !command.Params["name"].Required {
			t.Fatalf("Command(%q) params = %#v", name, command.Params)
		}
	}
}

func TestSchemaFallbackDescriptionsDoNotInventChildResourceShortcut(t *testing.T) {
	specDir := t.TempDir()
	writeSchemaTestSpec(t, filepath.Join(specDir, "demo", "thing.yaml"), `schema_version: 2
product: demo
resource: thing
kind: regional
display_name: Thing
schema:
  fields:
    name:
      type: string
      description: Thing name
    label:
      type: key_value
operations:
  restart:
    examples:
      - ecctl demo thing restart --name thing
    input:
      fields:
        - name:
            required: true
    filters:
      label.:
        target: label
        type: key_value
        key_prefix: label.
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	surface, ok := ProductList("demo")
	if !ok {
		t.Fatal("ProductList(demo) not found")
	}
	if surface.Description != "Manage DEMO resources" {
		t.Fatalf("product description = %q, want fallback", surface.Description)
	}

	if _, ok := Command("demo.restart"); ok {
		t.Fatal("child-only resource must not expose non-executable demo.restart shortcut")
	}

	command, ok := Command("demo.thing.restart")
	if !ok {
		t.Fatal("demo.thing.restart not found")
	}
	if command.Description != "Restart Thing." {
		t.Fatalf("description = %q, want fallback action description", command.Description)
	}
	if command.Params["name"].Description != "Thing name" {
		t.Fatalf("param description = %q, want resource param description", command.Params["name"].Description)
	}
	if command.Filters["label."].Description != "label.<key> filter." {
		t.Fatalf("filter description = %q, want prefix fallback", command.Filters["label."].Description)
	}
}

func TestSchemaDoesNotExposeRGChildResourceShortcut(t *testing.T) {
	if _, ok := Command("rg.list"); ok {
		t.Fatal("rg.list schema must not exist because ecctl rg list is not executable")
	}
	if _, ok := Command("rg.group.list"); !ok {
		t.Fatal("rg.group.list schema not found")
	}
}

func TestECSInstanceCreateSchemaIncludesNestedSubschemas(t *testing.T) {
	command, ok := CommandForLanguageMode("ecs.instance.create", "en", CommandSchemaFull)
	if !ok {
		t.Fatal("ecs.instance.create schema not found")
	}
	dataDisk := command.Params["data-disk"]
	if dataDisk.Type != "object" || !dataDisk.Repeatable || dataDisk.Input != "inline-key-value|json|@file" {
		t.Fatalf("data-disk schema = %#v", dataDisk)
	}
	if dataDisk.Fields["category"].Type == "" || dataDisk.Fields["category"].Description == "" ||
		dataDisk.Fields["size"].Type == "" || dataDisk.Fields["size"].Description == "" {
		t.Fatalf("data-disk item fields = %#v", dataDisk.Fields)
	}
	hostNames := command.Params["host-names"]
	if hostNames.Items == nil || hostNames.Items.Type != "string" {
		t.Fatalf("host-names schema = %#v", hostNames)
	}
}

func TestCommandSchemaProjectsNestedDataDiskSchema(t *testing.T) {
	fixture := spec.ResourceSpec{
		Kind: "regional",
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"data_disks": {
				Type:        "array",
				Description: spec.LocalizedText{"en": "Data disks to attach when creating the instance."},
				Items: &spec.SchemaField{
					Type: "object",
					Fields: map[string]spec.SchemaField{
						"auto_snapshot_policy": {
							Type:        "string",
							Description: spec.LocalizedText{"en": "Automatic snapshot policy ID for the data disk."},
						},
					},
				},
			},
		}},
		Controls: map[string]spec.SchemaField{
			"api_param": {Type: "key_value", Description: spec.LocalizedText{"en": "Additional request parameter key=value."}},
		},
		Operations: map[string]spec.Operation{
			"create": {Input: spec.OperationInput{
				Fields:   spec.OperationFields{{Name: "data_disks", Required: true, HasRequired: true}},
				Controls: spec.OperationFields{{Name: "api_param"}},
			}},
		},
	}

	params, ok := commandParamsFromOperation(fixture, "create", "en")
	if !ok {
		t.Fatal("commandParamsFromOperation returned ok=false")
	}
	dataDisk := params["data-disk"]
	if dataDisk.Type != "object" || !dataDisk.Repeatable || !dataDisk.Required || dataDisk.Input != "inline-key-value|json|@file" {
		t.Fatalf("data-disk schema = %#v", dataDisk)
	}
	autoSnapshot := dataDisk.Fields["auto_snapshot_policy"]
	if autoSnapshot.Type != "string" || autoSnapshot.Description == "" {
		t.Fatalf("auto_snapshot_policy schema = %#v", autoSnapshot)
	}
	if strings.Contains(autoSnapshot.Description, "OpenAPI") || strings.Contains(autoSnapshot.Description, "DataDisk.AutoSnapshotPolicyId") {
		t.Fatalf("description leaked API detail: %q", autoSnapshot.Description)
	}
	requireNoAPIFieldOrAny(t, params)
}

func TestCommandSchemaSkipsFieldsMarkedOutOfSchema(t *testing.T) {
	fixture := spec.ResourceSpec{
		Kind: "regional",
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"name":        {Type: "string"},
			"rare_option": {Type: "string"},
		}},
		Operations: map[string]spec.Operation{
			"update": {Input: spec.OperationInput{
				Fields: spec.OperationFields{
					{Name: "name"},
					{Name: "rare_option", Schema: false, HasSchema: true},
				},
			}},
		},
	}

	params, ok := commandParamsFromOperation(fixture, "update", "en")
	if !ok {
		t.Fatal("commandParamsFromOperation returned ok=false")
	}
	if _, ok := params["name"]; !ok {
		t.Fatalf("name param missing: %#v", params)
	}
	if _, ok := params["rare-option"]; ok {
		t.Fatalf("rare-option should be omitted from schema: %#v", params)
	}
}

func TestECSInstanceExecSchemaDeclaresCommandFileInput(t *testing.T) {
	command, ok := Command("ecs.instance.exec")
	if !ok {
		t.Fatal("ecs.instance.exec schema not found")
	}
	if command.Params["command"].Input != "text|@file" {
		t.Fatalf("command input = %q, want text|@file", command.Params["command"].Input)
	}
}

func TestSchemaRejectsInvalidAndAmbiguousCommands(t *testing.T) {
	specDir := t.TempDir()
	for _, resource := range []string{"one", "two"} {
		writeSchemaTestSpec(t, filepath.Join(specDir, "demo", resource+".yaml"), `schema_version: 2
product: demo
resource: `+resource+`
kind: regional
schema:
  fields:
    id:
      type: string
operations:
  create:
    examples:
      - ecctl demo `+resource+` create
`)
	}
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	if _, ok := Command("demo"); ok {
		t.Fatal("Command(demo) succeeded with invalid command shape")
	}
	if _, ok := Command("demo.create"); ok {
		t.Fatal("Command(demo.create) succeeded with ambiguous default resource")
	}
	if _, ok := ProductList("missing"); ok {
		t.Fatal("ProductList(missing) succeeded")
	}
}

func writeSchemaTestSpec(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func requireNoAPIFieldOrAny(t *testing.T, params map[string]Param) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal params: %v", err)
	}
	if strings.Contains(string(raw), "api_field") {
		t.Fatalf("schema leaked api_field: %s", raw)
	}
	for name, param := range params {
		requireNoAnyParam(t, name, param)
	}
}

func requireNoAnyParam(t *testing.T, name string, param Param) {
	t.Helper()
	if param.Type == "any" {
		t.Fatalf("%s type must not be any: %#v", name, param)
	}
	if param.Items != nil {
		requireNoAnyParam(t, name+".items", *param.Items)
	}
	for childName, child := range param.Fields {
		requireNoAnyParam(t, name+"."+childName, child)
	}
}
