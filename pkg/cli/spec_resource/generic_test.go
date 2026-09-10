package spec_resource

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestResourceCommandsAreDiscoveredFromSpecDir(t *testing.T) {
	specDir := t.TempDir()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
description:
  en: Manage demo product from spec
examples:
  - ecctl demo list
  - ecctl demo create --name demo-1
`)
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
description:
  en: Manage demo resources
examples:
  - ecctl demo list
  - ecctl demo create --name demo-1
identity:
  field: id
  output_root:
    one: demo
    many: demos
schema:
  fields:
    name:
      type: string
      description:
        en: demo name
operations:
  create:
    description:
      en: Create demo
    examples:
      - ecctl demo create --name demo-1
    input:
      fields:
        - name:
            required: true
    workflow: []
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	stdout, stderr, code := runCLI("--lang", "en", "demo", "create", "--help")
	if code != 0 {
		t.Fatalf("demo create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Create demo") || !strings.Contains(stdout, "--name string") {
		t.Fatalf("demo create help did not come from spec: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("root help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "demo") || !strings.Contains(stdout, "Manage demo product from spec") {
		t.Fatalf("root help should use product description from spec: %s", stdout)
	}

	stdout, _, code = runCLI("--lang", "en", "vpc", "--help")
	if code == 0 {
		t.Fatalf("vpc command must not be hard-coded when spec dir contains only demo: %s", stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownCommand" {
		t.Fatalf("vpc error.code = %q, want UnknownCommand; stdout=%s", got, stdout)
	}
}

func TestDefaultResourceAliasSubcommandRoutesToProductResource(t *testing.T) {
	specDir := t.TempDir()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
description:
  en: Manage demo product
examples:
  - ecctl demo list
  - ecctl demo widget list
`)
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
aliases: [widget]
description:
  en: Manage default demo resources
identity:
  field: id
  output_root:
    one: widget
    many: widgets
schema:
  fields:
    id:
      type: string
probes:
  list:
    api: ListWidgets
    request: {}
    response:
      items: $.Widgets
      fields:
        id: $.WidgetId
operations:
  list:
    description:
      en: List widgets
    workflow:
      - probe: list
        many: true
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	fake := &fakeSpecCaller{responses: []map[string]any{{"Widgets": []any{}}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "demo" || resource.Resource != "demo" {
			t.Fatalf("resource = %s/%s, want demo/demo", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("--region", "cn-beijing", "demo", "widget", "list")
	if code != 0 {
		t.Fatalf("demo widget list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListWidgets" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestProductAliasAndGlobalResourceDoNotRequireRegion(t *testing.T) {
	specDir := t.TempDir()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
aliases: [dm]
expose_default_resource: true
description:
  en: Manage global demos
examples:
  - ecctl demo list
  - ecctl dm demo list
`)
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: global
description:
  en: Manage global demo resources
identity:
  field: id
  output_root: {one: demo, many: demos}
schema:
  fields:
    id: {type: string}
probes:
  list:
    api: ListDemos
    request: {}
    response:
      items: $.items
      fields: {id: $.id}
operations:
  list:
    description: {en: List demos}
    examples: [ecctl demo list]
    workflow:
      - probe: list
        many: true
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	fake := &fakeSpecCaller{responses: []map[string]any{{"items": []any{}}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Kind != "global" || region != "" {
			t.Fatalf("resource kind=%q region=%q", resource.Kind, region)
		}
		return fake, nil
	})
	stdout, stderr, code := runCLI("dm", "demo", "list")
	if code != 0 {
		t.Fatalf("dm demo list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ListDemos" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestBuiltInSandboxAliasesAndTemplateCreate(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"templateID": "tpl-1", "buildID": "build-1"},
		{},
		{"templateID": "tpl-1", "buildID": "build-1", "status": "ready", "logEntries": []any{}},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "template" || resource.Provider != "e2b" || region != "" {
			t.Fatalf("resource=%#v region=%q", resource, region)
		}
		return fake, nil
	})
	stdout, stderr, code := runCLI("sbx", "tpl", "create", "--name", "python", "--from-image", "python:3.12")
	if code != 0 {
		t.Fatalf("sbx tpl create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "CreateTemplate" || fake.calls[1].operation != "StartTemplateBuild" || fake.calls[2].operation != "GetTemplateBuildStatus" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["body.name"] != "python" || fake.calls[1].request["body.fromImage"] != "python:3.12" || fake.calls[1].request["path.templateID"] != "tpl-1" || fake.calls[1].request["path.buildID"] != "build-1" {
		t.Fatalf("create requests = %#v", fake.calls)
	}
	template, _ := decodeObject(t, stdout)["template"].(map[string]any)
	if template["build_status"] != "ready" || template["id"] != "tpl-1" {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestBuiltInSandboxTemplateCreateNoWaitEmitsBuildIdentity(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"templateID": "tpl-1", "buildID": "build-1",
	}, {}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "template" || region != "" {
			t.Fatalf("resource=%s/%s region=%q", resource.Product, resource.Resource, region)
		}
		return fake, nil
	})
	stdout, stderr, code := runCLI("sbx", "tpl", "create", "--name", "python", "--from-image", "python:3.12", "--no-wait")
	if code != 0 {
		t.Fatalf("create --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateTemplate" || fake.calls[1].operation != "StartTemplateBuild" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	template, _ := decodeObject(t, stdout)["template"].(map[string]any)
	if template["id"] != "tpl-1" || template["build_id"] != "build-1" || template["build_status"] != "waiting" {
		t.Fatalf("template = %#v; stdout=%s", template, stdout)
	}
}

func TestBuiltInSandboxSnapshotKeepsSandboxIdentityAndEmitsTemplateID(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{{"snapshotID": "tpl-snapshot"}}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if region != "" {
			t.Fatalf("region = %q", region)
		}
		return fake, nil
	})
	stdout, stderr, code := runCLI("sandbox", "snapshot", "sbx-1", "--snapshot-name", "checkpoint")
	if code != 0 {
		t.Fatalf("snapshot exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	sandbox, _ := decodeObject(t, stdout)["sandbox"].(map[string]any)
	if sandbox["id"] != "sbx-1" || sandbox["snapshot_template"] != "tpl-snapshot" || sandbox["snapshot_name"] != "checkpoint" {
		t.Fatalf("sandbox = %#v; stdout=%s", sandbox, stdout)
	}
}

func TestBuiltInGlobalSandboxAllowsExplicitEmptyRegion(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{{"items": []any{}}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Kind != "global" || region != "" {
			t.Fatalf("resource kind=%q region=%q", resource.Kind, region)
		}
		return fake, nil
	})
	stdout, stderr, code := runCLI("--region=", "sandbox", "list")
	if code != 0 {
		t.Fatalf("sandbox list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

func TestPublicSandboxAliasMapsFiltersAndLogDirectionWithoutRegion(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{{"items": []any{}}, {"logs": []any{}}}}
	factory := func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "sandbox" || region != "" {
			t.Fatalf("resource=%s/%s region=%q", resource.Product, resource.Resource, region)
		}
		return fake, nil
	}
	var stdout, stderr bytes.Buffer
	ctx := cli.WithResourceCallerFactory(context.Background(), factory)
	code := cli.Run(ctx, []string{"--lang", "en", "sbx", "list", "--filter", "state=running", "--filter", "metadata.owner=a b", "--filter", "order=asc"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sbx list exit %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	metadata, ok := request["query.metadata"].([]string)
	if !ok || len(metadata) != 1 || metadata[0] != "owner=a b" {
		t.Fatalf("metadata request = %#v", request["query.metadata"])
	}
	states, ok := request["query.state"].([]string)
	if !ok || len(states) != 1 || states[0] != "running" {
		t.Fatalf("state request = %#v", request["query.state"])
	}
	if request["query.order"] != "asc" {
		t.Fatalf("order request = %#v", request["query.order"])
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(ctx, []string{"--lang", "en", "sbx", "logs", "sbx-1", "--direction", "backward", "--limit", "1000"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("sbx logs exit %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	logs, _ := decodeObject(t, stdout.String())["sandbox"].(map[string]any)["logs"].([]any)
	if logs == nil || len(logs) != 0 {
		t.Fatalf("empty sandbox logs must be preserved as an array: %s", stdout.String())
	}
	if len(fake.calls) != 2 || fake.calls[1].request["query.direction"] != "backward" || fake.calls[1].request["query.limit"] != 1000 {
		t.Fatalf("logs request = %#v", fake.calls)
	}
}

func TestE2BLimitBoundaries(t *testing.T) {
	t.Setenv("ECCTL_SPEC_DIR", "")
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"items": []any{}},
		{"logs": []any{}},
		{"logs": []any{}},
	}}
	runCLI := withCaller(func(_ string, _ string, _ spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if region != "" {
			t.Fatalf("region = %q", region)
		}
		return fake, nil
	})

	if stdout, stderr, code := runCLI("sandbox", "list", "--limit", "100"); code != 0 {
		t.Fatalf("sandbox list --limit 100 exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if _, _, code := runCLI("sandbox", "list", "--limit", "101"); code == 0 {
		t.Fatal("sandbox list --limit 101 succeeded")
	}
	if len(fake.calls) != 1 {
		t.Fatalf("invalid sandbox list reached caller: %#v", fake.calls)
	}

	if stdout, stderr, code := runCLI("sandbox", "logs", "sbx-1", "--limit", "1000"); code != 0 {
		t.Fatalf("sandbox logs --limit 1000 exit %d stderr=%s stdout=%s", code, stderr, stdout)
	} else if logs, _ := decodeObject(t, stdout)["sandbox"].(map[string]any)["logs"].([]any); logs == nil || len(logs) != 0 {
		t.Fatalf("empty sandbox logs must be preserved as an array: %s", stdout)
	}
	if _, _, code := runCLI("sandbox", "logs", "sbx-1", "--limit", "1001"); code == 0 {
		t.Fatal("sandbox logs --limit 1001 succeeded")
	}
	if len(fake.calls) != 2 {
		t.Fatalf("invalid sandbox logs reached caller: %#v", fake.calls)
	}

	if stdout, stderr, code := runCLI("sandbox", "template", "build-logs", "tpl-1", "--build-id", "build-1", "--limit", "100"); code != 0 {
		t.Fatalf("template build-logs --limit 100 exit %d stderr=%s stdout=%s", code, stderr, stdout)
	} else if logs, _ := decodeObject(t, stdout)["template"].(map[string]any)["logs"].([]any); logs == nil || len(logs) != 0 {
		t.Fatalf("empty template build logs must be preserved as an array: %s", stdout)
	}
	if _, _, code := runCLI("sandbox", "template", "build-logs", "tpl-1", "--build-id", "build-1", "--limit", "101"); code == 0 {
		t.Fatal("template build-logs --limit 101 succeeded")
	}
	if len(fake.calls) != 3 {
		t.Fatalf("invalid template build-logs reached caller: %#v", fake.calls)
	}
}

func TestRequiredFlagsAreMarkedAndListedFirstInHelp(t *testing.T) {
	specDir := t.TempDir()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
description:
  en: Manage demo resources
schema:
  fields:
    optional_name:
      type: string
      description:
        en: optional name
    name:
      type: string
      description:
        en: demo name
operations:
  create:
    description:
      en: Create demo
    examples:
      - ecctl demo create --name demo
    input:
      fields:
        - optional_name
        - name:
            required: true
    workflow: []
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	stdout, stderr, code := runCLI("--lang", "en", "demo", "create", "--help")
	if code != 0 {
		t.Fatalf("demo create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Resource Flags (* required):") {
		t.Fatalf("help missing required flag legend:\n%s", stdout)
	}
	required := strings.Index(stdout, "* --name string")
	optional := strings.Index(stdout, "--optional-name string")
	if required < 0 {
		t.Fatalf("help missing required flag marker:\n%s", stdout)
	}
	if optional < 0 {
		t.Fatalf("help missing optional flag:\n%s", stdout)
	}
	if required > optional {
		t.Fatalf("required flag must be listed before optional flag:\n%s", stdout)
	}
	if strings.Contains(stdout, "(required)") {
		t.Fatalf("help should use * marker instead of required suffix:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "zh-CN", "demo", "create", "--help")
	if code != 0 {
		t.Fatalf("demo create --help zh-CN exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "资源参数（* 必填）:") || !strings.Contains(stdout, "* --name string") {
		t.Fatalf("Chinese help missing required flag marker or legend:\n%s", stdout)
	}
}

func TestInvalidResourceSpecErrorIsLocalized(t *testing.T) {
	specDir := t.TempDir()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `product: demo
resource: demo
kind: regional
actions: {}
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	stdout, stderr, code := runCLI("--lang", "zh-CN", "resources")
	if code == 0 {
		t.Fatalf("invalid spec command succeeded; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "InvalidResourceSpec" {
		t.Fatalf("error.code = %q, want InvalidResourceSpec; stdout=%s", got, stdout)
	}
	if got := errorMessage(t, stdout); got != "资源规格无效" {
		t.Fatalf("error.message = %q, want localized InvalidResourceSpec; stdout=%s", got, stdout)
	}
}

func TestResourceGoDoesNotHardCodeSecurityGroupRuleActions(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../resource.go")
	if err != nil {
		t.Fatalf("ReadFile resource.go: %v", err)
	}
	source := string(raw)
	for _, forbidden := range []string{`"authorize"`, `"revoke"`, `"rule_list"`, `"indexed_list"`, "selectMatchingRules", "rulesFromItem"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("resource.go must not hard-code SG rule handling %q", forbidden)
		}
	}
}

func TestComplexObjectParametersUseDeclaredInputShapes(t *testing.T) {
	specDir := t.TempDir()
	writeComplexInputSpec(t, specDir)
	itemFile := filepath.Join(t.TempDir(), "data-disk.json")
	if err := os.WriteFile(itemFile, []byte(`{"category":"cloud_efficiency","size":80}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-create", "WidgetId": "w-123"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "demo" || resource.Resource != "widget" {
			t.Fatalf("resource = %s/%s, want demo/widget", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI(
		"demo", "widget", "create",
		"--region", "cn-beijing",
		"--name", "web",
		"--data-disk", "category=cloud_essd,size=40",
		"--data-disk", "@"+itemFile,
		"--system-disk", "category=cloud_essd,size=20",
	)
	if code != 0 {
		t.Fatalf("demo widget create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateWidget" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"DataDisk.1.Category": "cloud_essd",
		"DataDisk.1.Size":     40,
		"DataDisk.2.Category": "cloud_efficiency",
		"DataDisk.2.Size":     80,
		"SystemDisk.Category": "cloud_essd",
		"SystemDisk.Size":     20,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
}

func TestComplexObjectParameterHelpDeclaresInputShape(t *testing.T) {
	specDir := t.TempDir()
	writeComplexInputSpec(t, specDir)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	stdout, stderr, code := runCLI("--lang", "en", "demo", "widget", "create", "--help")
	if code != 0 {
		t.Fatalf("demo widget create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--data-disk stringArray") || !strings.Contains(stdout, "inline key=value, JSON object, or @file") {
		t.Fatalf("help must expose singular item flag and input shape: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "demo.widget.create")
	if code != 0 {
		t.Fatalf("schema demo.widget.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	dataDisk, _ := params["data-disk"].(map[string]any)
	if dataDisk == nil || dataDisk["input"] != "inline-key-value|json|@file" {
		t.Fatalf("schema must describe item flag and input shape: %#v; stdout=%s", dataDisk, stdout)
	}
}
