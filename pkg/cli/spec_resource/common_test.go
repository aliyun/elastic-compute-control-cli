package spec_resource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ack"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ecs"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/rg"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/tag"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ecctl-cli-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl-config.json"))
	os.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun-config.json"))
	os.Setenv("ECCTL_REGION", "")
	os.Setenv("ECCTL_DISPLAY_MODE", "AI")
	os.Unsetenv("AGENT_FIRST")
	defer engine.SetTimeScaleForTest(0.001)()
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func runCLI(args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	if !hasLangArg(args) {
		args = append([]string{"--lang", "en"}, args...)
	}
	code := cli.Run(cli.WithFullCommandSurface(context.Background()), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func hasLangArg(args []string) bool {
	for _, arg := range args {
		if arg == "--lang" || strings.HasPrefix(arg, "--lang=") {
			return true
		}
	}
	return false
}

func decodeObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected JSON object, got %q: %v", raw, err)
	}
	return decoded
}

func errorCode(t *testing.T, raw string) string {
	t.Helper()
	errObj, _ := decodeObject(t, raw)["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stdout.error missing: %s", raw)
	}
	code, _ := errObj["code"].(string)
	return code
}

func errorMessage(t *testing.T, raw string) string {
	t.Helper()
	errObj, _ := decodeObject(t, raw)["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stdout.error missing: %s", raw)
	}
	message, _ := errObj["message"].(string)
	return message
}

func withCaller(factory cli.ResourceCallerFactory) func(args ...string) (string, string, int) {
	return func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		if !hasLangArg(args) {
			args = append([]string{"--lang", "en"}, args...)
		}
		ctx := cli.WithResourceCallerFactory(context.Background(), factory)
		ctx = cli.WithFullCommandSurface(ctx)
		code := cli.Run(ctx, args, &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
}

func fakeDiskListResponse(id string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe",
		"TotalCount": 1,
		"Disks": map[string]any{"Disk": []any{
			map[string]any{
				"DiskId":     id,
				"DiskName":   "data-1",
				"Status":     status,
				"RegionId":   "cn-beijing",
				"ZoneId":     "cn-beijing-a",
				"Size":       float64(40),
				"Category":   "cloud_essd",
				"InstanceId": "i-123",
			},
		}},
	}
}

func fakeSecurityGroupAttributeResponse(id string, permissions []any) map[string]any {
	return map[string]any{
		"RequestId":       "req-attr",
		"SecurityGroupId": id,
		"Permissions":     map[string]any{"Permission": permissions},
	}
}

func fakeSecurityGroupReferencesResponse(id string, referencingID string) map[string]any {
	return map[string]any{
		"RequestId": "req-references",
		"SecurityGroupReferences": map[string]any{"SecurityGroupReference": []any{
			map[string]any{
				"SecurityGroupId": id,
				"ReferencingSecurityGroups": map[string]any{"ReferencingSecurityGroup": []any{
					map[string]any{"SecurityGroupId": referencingID},
				}},
			},
		}},
	}
}

func fakeNetworkInterfaceAttributeResponse(id string, status string) map[string]any {
	return map[string]any{
		"RequestId":            "req-attr",
		"NetworkInterfaceId":   id,
		"NetworkInterfaceName": "web-eni",
		"Status":               status,
		"VpcId":                "vpc-123",
		"VSwitchId":            "vsw-123",
		"PrivateIpAddress":     "10.0.0.5",
		"SecurityGroupIds":     map[string]any{"SecurityGroupId": []any{"sg-123"}},
	}
}

type fakeSpecCall struct {
	operation string
	request   map[string]any
}

type fakeSpecCaller struct {
	calls                 []fakeSpecCall
	errors                []error
	responses             []map[string]any
	responseWhenExhausted map[string]any
}

func (f *fakeSpecCaller) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, fakeSpecCall{operation: operation, request: request})
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.responses) == 0 {
		if f.responseWhenExhausted != nil {
			return f.responseWhenExhausted, nil
		}
		return map[string]any{}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func writeCLIResourceSpec(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeComplexInputSpec(t *testing.T, specDir string) {
	t.Helper()
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
description:
  en: Demo product
examples:
  - ecctl demo widget list
  - ecctl demo widget create --name web
`)
	writeCLIResourceSpec(t, filepath.Join(specDir, "demo", "widget.yaml"), `schema_version: 2
product: demo
resource: widget
kind: regional
description:
  en: Manage widgets
identity:
  field: id
  output_root:
    one: widget
    many: widgets
schema:
  fields:
    name:
      type: string
      description:
        en: widget name
    data_disks:
      type: array
      description:
        en: data disks
      items:
        type: object
        fields:
          category:
            type: string
            description:
              en: disk category
          size:
            type: integer
            description:
              en: disk size
    system_disk:
      type: object
      description:
        en: system disk
      fields:
        category:
          type: string
          description:
            en: disk category
        size:
          type: integer
          description:
            en: disk size
bindings:
  create_widget:
    api: CreateWidget
    request:
      RegionId: $context.region
      Name: $.name
      DataDisk:
        each: $.data_disks
        fields:
          Category: $.category
          Size: $.size
      SystemDisk:
        from: $.system_disk
        fields:
          Category: $.category
          Size: $.size
    id_from: $.WidgetId
    request_id_from: $.RequestId
operations:
  create:
    description:
      en: Create widget
    examples:
      - ecctl demo widget create --name w-1
    input:
      fields:
        - name
        - data_disks
        - system_disk
    workflow:
      - binding: create_widget
`)
}
