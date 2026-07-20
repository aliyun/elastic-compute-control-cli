package spec_resource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
