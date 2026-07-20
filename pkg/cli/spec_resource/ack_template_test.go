package spec_resource

import (
	"os"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func ackTemplateCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "template" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api=%s, want ack/template with CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		return fake, nil
	})
}

func TestACKTemplateHelpAndSchemaExposeCRUDAndContentInput(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "template", "--help")
	if code != 0 {
		t.Fatalf("ack template --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"create", "update", "delete", "get", "list"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack template help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "template", "create", "--help")
	if code != 0 {
		t.Fatalf("ack template create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"* --name string", "* --content string", "JSON/YAML text or @file"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack template create help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "template", "delete", "--help")
	if code != 0 {
		t.Fatalf("ack template delete --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--no-wait", "--timeout duration"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack template delete help missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ack.template.create")
	if code != 0 {
		t.Fatalf("schema ack.template.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	contentParam, _ := params["content"].(map[string]any)
	if contentParam == nil || contentParam["input"] != "json|yaml|@file" {
		t.Fatalf("schema content param = %#v; stdout=%s", contentParam, stdout)
	}
}

func TestACKTemplateCRUDRoutesToACKTemplateAPIs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "template_id": "tmpl-123"},
		{"RequestId": "req-describe-create", "id": "tmpl-version-1", "template_with_hist_id": "tmpl-123", "name": "web", "template": "apiVersion: v1", "template_type": "kubernetes"},
		{"RequestId": "req-update"},
		{"RequestId": "req-describe-update", "id": "tmpl-version-2", "template_with_hist_id": "tmpl-123", "name": "web-renamed", "template": "kind: ConfigMap", "template_type": "kubernetes"},
		{"RequestId": "req-delete"},
		{},
		{"RequestId": "req-get", "id": "tmpl-123", "name": "web-renamed", "template": "kind: ConfigMap", "template_type": "kubernetes"},
		{
			"RequestId": "req-list",
			"templates": []any{
				map[string]any{"id": "tmpl-123", "name": "web-renamed", "template": "kind: ConfigMap", "template_type": "kubernetes"},
			},
			"page_info": map[string]any{"page_number": 1, "page_size": 100, "total_count": 1},
		},
	}}
	runCLI := ackTemplateCaller(t, fake)

	content := "apiVersion: v1\nkind: ConfigMap"
	stdout, stderr, code := runCLI("ack", "template", "create",
		"--region", "cn-beijing",
		"--name", "web",
		"--content", content,
		"--tags", "kubernetes",
		"--description", "web template",
		"--template-type", "kubernetes",
	)
	if code != 0 {
		t.Fatalf("ack template create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "template", "update", "tmpl-123",
		"--region", "cn-beijing",
		"--name", "web-renamed",
		"--content", "kind: ConfigMap",
	)
	if code != 0 {
		t.Fatalf("ack template update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	updated := decodeObject(t, stdout)["template"].(map[string]any)
	if updated["id"] != "tmpl-123" || updated["version_id"] != "tmpl-version-2" {
		t.Fatalf("ack template update output should keep stable id and expose version id: %s", stdout)
	}

	stdout, stderr, code = runCLI("ack", "template", "delete", "tmpl-123", "--region", "cn-beijing", "--timeout", "1s")
	if code != 0 {
		t.Fatalf("ack template delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "template", "get", "tmpl-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ack template get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI("ack", "template", "list", "--region", "cn-beijing", "--template-type", "kubernetes")
	if code != 0 {
		t.Fatalf("ack template list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if got := callNames(fake.calls); strings.Join(got, ",") != "CreateTemplate,DescribeTemplateAttribute,UpdateTemplate,DescribeTemplateAttribute,DeleteTemplate,DescribeTemplateAttribute,DescribeTemplateAttribute,DescribeTemplates" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["body.name"] != "web" || fake.calls[0].request["body.template"] != content || fake.calls[0].request["body.tags"] != "kubernetes" || fake.calls[0].request["body.template_type"] != "kubernetes" {
		t.Fatalf("CreateTemplate request = %#v", fake.calls[0].request)
	}
	if fake.calls[2].request["TemplateId"] != "tmpl-123" || fake.calls[2].request["body.name"] != "web-renamed" || fake.calls[2].request["body.template"] != "kind: ConfigMap" {
		t.Fatalf("UpdateTemplate request = %#v", fake.calls[2].request)
	}
	if fake.calls[4].request["TemplateId"] != "tmpl-123" {
		t.Fatalf("DeleteTemplate request = %#v", fake.calls[4].request)
	}
	if fake.calls[5].request["TemplateId"] != "tmpl-123" {
		t.Fatalf("DescribeTemplateAttribute delete waiter request = %#v", fake.calls[5].request)
	}
	if fake.calls[6].request["TemplateId"] != "tmpl-123" {
		t.Fatalf("DescribeTemplateAttribute request = %#v", fake.calls[6].request)
	}
	if fake.calls[7].request["template_type"] != "kubernetes" || fake.calls[7].request["page_num"] != 1 || fake.calls[7].request["page_size"] != 100 {
		t.Fatalf("DescribeTemplates request = %#v", fake.calls[7].request)
	}
}

func TestACKTemplateSpecIsYAMLOnly(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("../../../specs/ack/template.go"); !os.IsNotExist(err) {
		t.Fatalf("ack template must remain YAML-only; stat err=%v", err)
	}
}
