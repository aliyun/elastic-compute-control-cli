package spec_resource

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func nativeTemplate(state string) map[string]any {
	return map[string]any{"code": "200", "requestId": "req-get", "templateID": "tpl-1", "name": "native", "teamID": "team-1", "teamName": "dev", "resourceGroupID": "rg-1", "createdTime": "2026-10-14T00:00:00Z", "runtimeConfig": map[string]any{"cpu": 0.5, "memorySize": float64(1024), "internetAccess": false}, "status": map[string]any{"state": state, "reason": map[string]any{"step": "build", "message": "image pull failed"}, "finishedAt": "2026-10-14T00:00:01Z"}}
}

func nativeTemplateCLI(t *testing.T, fake *fakeSpecCaller) func(...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || resource.Kind != "regional" || region != "cn-hangzhou" {
			t.Fatalf("native transport/region = %#v %q", resource, region)
		}
		return fake, nil
	})
}

func TestSandboxTemplateNativeConfigurationOutputPreservesJSON(t *testing.T) {
	config := decodeObject(t, `{"internetAccess":false,"vpcConfig":{"vSwitchIds":["vsw-1"]},"sandboxConfig":{"steps":[{"type":"RUN","args":["true"]}]},"environmentVariables":{},"layers":[]}`)
	for _, action := range []string{"get", "list", "create", "update"} {
		t.Run(action, func(t *testing.T) {
			response := nativeTemplate("ready")
			response["runtimeConfig"] = config
			responses := []map[string]any{response}
			args := []string{"--lang", "en", "sandbox", "template", action}
			switch action {
			case "get", "update":
				args = append(args, "tpl-1")
			case "create":
				args = append(args, "--name", "native")
			}
			if action == "create" || action == "update" {
				args = append(args, "--runtime-config", `{"internetAccess":false}`)
				responses = append([]map[string]any{{"code": "200", "templateID": "tpl-1"}}, response)
			} else if action == "list" {
				responses = []map[string]any{{"code": "200", "templates": []any{response}}}
			}
			args = append(args, "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1")
			stdout, stderr, exit := nativeTemplateCLI(t, &fakeSpecCaller{responses: responses})(args...)
			if exit != 0 {
				t.Fatalf("exit=%d %s %s", exit, stdout, stderr)
			}
			out := decodeObject(t, stdout)
			template, _ := out["template"].(map[string]any)
			if action == "list" {
				template = out["templates"].([]any)[0].(map[string]any)
			}
			if !reflect.DeepEqual(template["runtime_config"], config) {
				t.Fatalf("native JSON changed shape/keys: %#v; want %#v", template["runtime_config"], config)
			}
		})
	}
}

func TestSandboxTemplateOpenAPICRUDAndTypes(t *testing.T) {
	runtimeJSON := `{"cpu":0.5,"memorySize":1024,"diskSize":2048,"internetAccess":false,"sandboxConfig":{"image":"example/image:v1","generation":2,"registryConfig":{"authConfig":{"password":"private-password","userName":"user"}}}}`
	buildJSON := `{"copy":{"enabled":false,"image":"example/image:v1"},"envdInject":{"enabled":true}}`
	file := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(file, []byte(runtimeJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		args       []string
		responses  []map[string]any
		operations []string
		want       map[string]any
	}{
		{"create", []string{"create", "--name", "native", "--runtime-config", "@" + file, "--build-config", buildJSON}, []map[string]any{{"code": "200", "requestId": "req-create", "templateID": "tpl-1"}, nativeTemplate("ready")}, []string{"CreateTemplate", "GetTemplate"}, map[string]any{"body.name": "native", "body.teamID": "team-1", "body.runtimeConfig": decodeObject(t, runtimeJSON), "body.buildConfig": decodeObject(t, buildJSON)}},
		{"update", []string{"update", "tpl-1", "--runtime-config", runtimeJSON, "--build-config", buildJSON}, []map[string]any{{"code": "200", "requestId": "req-update", "templateID": "tpl-1"}, nativeTemplate("ready")}, []string{"UpdateTemplate", "GetTemplate"}, map[string]any{"templateID": "tpl-1", "teamID": "team-1", "body.runtimeConfig": decodeObject(t, runtimeJSON), "body.buildConfig": decodeObject(t, buildJSON)}},
		{"get", []string{"get", "tpl-1"}, []map[string]any{nativeTemplate("failed")}, []string{"GetTemplate"}, map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
		{"list", []string{"list", "--limit", "2", "--next-token", "page-2"}, []map[string]any{{"code": "200", "requestId": "req-list", "templates": []any{nativeTemplate("ready")}, "nextToken": "page-3", "maxResults": float64(2)}}, []string{"ListTemplates"}, map[string]any{"teamID": "team-1", "maxResults": 2, "nextToken": "page-2"}},
		{"delete", []string{"delete", "tpl-1"}, []map[string]any{{"code": "200", "requestId": "req-delete"}}, []string{"DeleteTemplate"}, map[string]any{"templateID": "tpl-1", "teamID": "team-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSpecCaller{responses: tc.responses}
			args := append([]string{"--lang", "en", "sbx", "tpl"}, tc.args...)
			args = append(args, "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1")
			stdout, stderr, code := nativeTemplateCLI(t, fake)(args...)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if len(fake.calls) != len(tc.operations) {
				t.Fatalf("calls=%+v", fake.calls)
			}
			for index, want := range tc.operations {
				if fake.calls[index].operation != want {
					t.Fatalf("calls=%+v", fake.calls)
				}
			}
			request := fake.calls[0].request
			for _, field := range []string{"body.runtimeConfig", "body.buildConfig"} {
				if text, ok := request[field].(string); ok {
					request[field] = decodeObject(t, text)
				}
			}
			if !reflect.DeepEqual(request, tc.want) {
				t.Fatalf("request=%#v want=%#v", fake.calls[0].request, tc.want)
			}
			if len(fake.calls) > 1 && (fake.calls[1].request["teamID"] != "team-1" || fake.calls[1].request["templateID"] != "tpl-1") {
				t.Fatalf("waiter lost scope: %+v", fake.calls)
			}
			out := decodeObject(t, stdout)
			if tc.name == "list" {
				if out["pagination"].(map[string]any)["next_token"] != "page-3" {
					t.Fatalf("pagination=%s", stdout)
				}
			} else if tc.name == "delete" {
				if out["deleted"] != true {
					t.Fatalf("delete=%s", stdout)
				}
			} else {
				template := out["template"].(map[string]any)
				if template["id"] != "tpl-1" || template["team_id"] != "team-1" {
					t.Fatalf("identity=%s", stdout)
				}
				if tc.name == "get" && (template["build_status"] != "failed" || template["reason"].(map[string]any)["message"] != "image pull failed") {
					t.Fatalf("status=%s", stdout)
				}
			}
		})
	}
}

func TestSandboxTemplateOpenAPINoWaitAndFailure(t *testing.T) {
	for _, tc := range []struct {
		state    string
		noWait   bool
		wantCode int
	}{
		{"ready", true, 0}, {"ready", false, 0}, {"error", false, 2}, {"failed", false, 2},
	} {
		t.Run(tc.state+strings.Repeat("-no-wait", boolInt(tc.noWait)), func(t *testing.T) {
			fake := &fakeSpecCaller{responses: []map[string]any{{"code": "200", "templateID": "tpl-1"}, nativeTemplate(tc.state)}}
			args := []string{"--lang", "en", "sandbox", "template", "create", "--openapi", "--name", "native", "--team-id", "team-1", "--region", "cn-hangzhou"}
			if tc.noWait {
				args = append(args, "--no-wait")
			}
			stdout, stderr, code := nativeTemplateCLI(t, fake)(args...)
			if code != tc.wantCode {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if tc.noWait && (len(fake.calls) != 1 || strings.Contains(stdout, `"ready"`)) {
				t.Fatalf("accepted ID is not build readiness: %+v %s", fake.calls, stdout)
			}
			if code != 0 && errorCode(t, stdout) != "WaitFailed" {
				t.Fatalf("waiter did not report a failed state: %s", stdout)
			}
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestSandboxTemplateOpenAPIRejectsMixedInputsBeforeCaller(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--openapi", "--name", "x", "--from-image", "ubuntu"},
		{"create", "--openapi", "--name", "x", "--cpu-count", "2"},
		{"update", "tpl-1", "--openapi", "--public=false"},
		{"update", "tpl-1", "--openapi"},
		{"create", "--name", "x", "--from-image", "ubuntu", "--team-id", "team-1"},
		{"update", "tpl-1", "--public", "--runtime-config", `{"cpu":1}`},
		{"create", "--openapi=false", "--name", "x"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			run := withCaller(func(_, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
				called = true
				return &fakeSpecCaller{}, nil
			})
			stdout, stderr, code := run(append([]string{"--lang", "en", "sandbox", "template"}, args...)...)
			if code == 0 || called {
				t.Fatalf("mixed/empty update reached credentials: called=%v code=%d %s %s", called, code, stdout, stderr)
			}
		})
	}
}
