package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestFCSandboxVolumePublicAndUsesAliyunProfile(t *testing.T) {
	for _, args := range [][]string{{"sandbox", "volume", "--help"}, {"sbx", "volume", "--help"}, {"schema", "--list", "sandbox"}, {"examples", "sandbox.volume"}} {
		stdout, stderr, code := runCLI(append([]string{"--lang", "en"}, args...)...)
		if code != 0 || !strings.Contains(stdout, "volume") {
			t.Fatalf("public Volume command %v: exit %d stdout=%s stderr=%s", args, code, stdout, stderr)
		}
	}
	dir := t.TempDir()
	aliyunPath := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyunPath, []byte(`{"current":"test","profiles":[{"name":"test","mode":"AK","region_id":"cn-beijing","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(key string) string {
		switch key {
		case "ECCTL_ALIYUN_CONFIG_PATH":
			return aliyunPath
		case "E2B_API_KEY":
			return "test-e2b-key"
		case "ECCTL_SANDBOX_BACKEND":
			return "e2b"
		}
		return ""
	}
	resource, err := spec.LoadResource("", "sandbox", "volume")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := defaultResourceCallerFactoryResolved(context.Background(), "test", filepath.Join(dir, "ecctl.json"), resource, config.ResolvedRegion{Value: "cn-hangzhou", Source: config.RegionSourceExplicit}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	fc, ok := caller.(*aliyun.OpenAPICaller)
	if !ok || fc.Product != "FCSandbox" || fc.Resource != "volume" || fc.Region != "cn-hangzhou" || fc.Profile.Mode != "AK" {
		t.Fatalf("Volume route must ignore E2B overrides: %T %+v", caller, resource)
	}
}

func TestFCSandboxVolumeCreateParsesNestedJSONAndFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "oss.json")
	if err := os.WriteFile(file, []byte(`{"bucketName":"dedicated-test-bucket","bucketPath":"/run-1","readOnly":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAPICaller{response: map[string]any{"code": "200", "requestId": "req-create", "volume": map[string]any{
		"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING",
	}}}
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return fake, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-1",
		"--oss-volume-config", "@"+file,
		"--agentic-fs-volume-config", `{"serverAddr":"test.example:/","userID":1000,"groupID":1000}`,
		"--agentic-bucket-volume-config", `{"agenticBucket":"owned-bucket","bucketName":"space","readOnly":false}`,
		"--mount-config", `{"role":"acs:ram::0000000000000000:role/test","vpcConfig":{"vpcId":"vpc-test","vSwitchIds":["vsw-test"],"securityGroupId":"sg-test"}}`,
		"--no-wait", "--region", "cn-hangzhou", "--output=json")
	if code != 0 || stderr != "" || fake.operation != "CreateVolume" || !strings.Contains(stdout, `"status":"CREATING"`) {
		t.Fatalf("Volume create: code=%d stdout=%s stderr=%s request=%#v", code, stdout, stderr, fake.request)
	}
	for _, field := range []string{"body.ossVolumeConfig", "body.agenticFSVolumeConfig", "body.agenticBucketVolumeConfig", "body.mountConfig"} {
		serialized, ok := fake.request[field].(string)
		if !ok {
			t.Errorf("%s is not serialized JSON: %#v", field, fake.request[field])
			continue
		}
		var nested map[string]any
		if err := json.Unmarshal([]byte(serialized), &nested); err != nil || len(nested) == 0 {
			t.Errorf("%s did not preserve nested JSON: %q (%v)", field, serialized, err)
		}
	}
	if fake.request["body.volumeName"] != "workspace" || fake.request["body.teamID"] != "team-1" {
		t.Fatalf("Volume ownership mapping: %#v", fake.request)
	}
}

func TestVolumeCreateHelpMatchesExplicitStructuredInput(t *testing.T) {
	stdout, stderr, code := runCLIWith(nil, "--lang", "en", "sandbox", "volume", "create", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("Volume create help: code=%d stderr=%s", code, stderr)
	}
	for _, flag := range []string{"oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config"} {
		if !strings.Contains(stdout, "--"+flag) {
			t.Errorf("help omitted %s", flag)
		}
	}
	if strings.Contains(stdout, "inline key=value") {
		t.Fatal("Volume create help advertised an unsupported inline form")
	}
}

func TestVolumeCreateRejectsExplicitBlankInputsBeforeCaller(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--name", "", "--team-id", "team-1"},
		{"--name", "workspace", "--team-id", "team-1", "--oss-volume-config", ""},
		{"--name", "workspace", "--team-id", "team-1", "--oss-volume-config", "@" + empty},
	} {
		called := false
		factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
			called = true
			return &fakeAPICaller{}, nil
		}
		_, _, code := runCLIWith(factory, append([]string{"--lang", "en", "sandbox", "volume", "create"}, args...)...)
		if code == 0 || called {
			t.Fatalf("blank Volume input reached caller: args=%q code=%d", args, code)
		}
	}
}

func TestNativeTemplateOptionalBlankConfigKeepsOtherConfig(t *testing.T) {
	for _, blank := range []string{"runtime-config", "build-config"} {
		valid := "runtime-config"
		if blank == valid {
			valid = "build-config"
		}
		configuration := `{"cpu":0.5}`
		if valid == "build-config" {
			configuration = `{"envdInject":{"enabled":false}}`
		}
		fake := &fakeAPICaller{response: map[string]any{"code": "200", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "ready"}}}
		factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
			return fake, nil
		}
		stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "template", "update", "tpl-1", "--openapi", "--team-id", "team-1", "--region", "cn-hangzhou", "--"+blank, "", "--"+valid, configuration, "--no-wait", "--output=json")
		if code != 0 || fake.operation != "UpdateTemplate" {
			t.Fatalf("optional blank --%s rejected valid --%s: code=%d operation=%q stdout=%q stderr=%q", blank, valid, code, fake.operation, stdout, stderr)
		}
	}
}

func TestNativeTemplateBlankConfigFileIsRejected(t *testing.T) {
	for _, contents := range []string{"", " \n\t"} {
		path := filepath.Join(t.TempDir(), "blank.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, blank := range []string{"runtime-config", "build-config"} {
			valid, configuration := "build-config", `{"envdInject":{"enabled":false}}`
			if blank == valid {
				valid, configuration = "runtime-config", `{"cpu":0.5}`
			}
			fake := &fakeAPICaller{response: map[string]any{"code": "200", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "ready"}}}
			factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
				return fake, nil
			}
			stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "template", "update", "tpl-1", "--openapi", "--team-id", "team-1", "--region", "cn-hangzhou", "--"+blank, "@"+path, "--"+valid, configuration, "--no-wait", "--output=json")
			if code == 0 || fake.operation != "" || errorCode(t, stdout) != "InvalidParameter" || errorObject(t, stdout)["field"] != blank {
				t.Fatalf("blank --%s file reached UpdateTemplate or lost its input error: contents=%q code=%d operation=%q stdout=%q stderr=%q", blank, contents, code, fake.operation, stdout, stderr)
			}
		}
	}
}

func TestFCSandboxVolumeNestedInputUsesProviderSchema(t *testing.T) {
	resource, err := spec.LoadResource("", "sandbox", "volume")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field string
		json  string
	}{
		{"oss_volume_config", `{"bucketName":"bucket","readOnly":true}`},
		{"agentic_fs_volume_config", `{"userID":1000,"groupID":1000}`},
		{"mount_config", `{"vpcConfig":{"vSwitchIds":["vsw-one"]}}`},
	} {
		if _, _, err := structuredObjectFlagValue(tc.json, tc.field, resource.Schema.Fields[tc.field], true); err != nil {
			t.Fatalf("valid %s provider shape: %v", tc.field, err)
		}
	}
	parsed, _, err := structuredObjectFlagValue(`{"userID":2147483647}`, "agentic_fs_volume_config", resource.Schema.Fields["agentic_fs_volume_config"], true)
	if err != nil || parsed.(map[string]any)["userID"] != json.Number("2147483647") {
		t.Fatalf("integer provider value was changed: %#v %v", parsed, err)
	}
	for _, tc := range []struct {
		field string
		json  string
	}{
		{"oss_volume_config", `{"readOnly":"true"}`},
		{"agentic_fs_volume_config", `{"userID":1.5}`},
		{"agentic_bucket_volume_config", `{"unsupported":"value"}`},
		{"mount_config", `{"vpcConfig":{"vSwitchIds":[1]}}`},
	} {
		if _, _, err := structuredObjectFlagValue(tc.json, tc.field, resource.Schema.Fields[tc.field], true); err == nil {
			t.Fatalf("invalid %s provider shape accepted", tc.field)
		}
	}
}

func TestAPIShapedInputLocalizedDiagnostics(t *testing.T) {
	t.Setenv("ECCTL_DISABLE_TELEMETRY", "1")
	t.Setenv("ECCTL_DISABLE_UPDATE_CHECK", "1")
	t.Setenv("ECCTL_SPEC_DIR", "")

	duplicateFile := filepath.Join(t.TempDir(), "duplicate.json")
	if err := os.WriteFile(duplicateFile, []byte(`{"vpcConfig":{"vpcId":"vpc-a","vpcId":"vpc-b"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name           string
		flag           string
		value          string
		path           string
		english        string
		chinese        string
		englishMessage string
		chineseMessage string
		genericControl bool
	}{
		{
			name:           "wrong-read-only-type",
			flag:           "--oss-volume-config",
			value:          `{"readOnly":"true"}`,
			path:           "oss-volume-config.readOnly",
			english:        "does not match the API JSON schema",
			chinese:        "不符合 API JSON 结构",
			englishMessage: "--oss-volume-config.readOnly does not match the API JSON schema",
			chineseMessage: "--oss-volume-config.readOnly 的值不符合 API JSON 结构",
		},
		{
			name:           "unknown-nested-member",
			flag:           "--mount-config",
			value:          `{"vpcConfig":{"vpcID":"vpc-fixture"}}`,
			path:           "mount-config.vpcConfig.vpcID",
			english:        "unsupported API JSON member",
			chinese:        "不支持 API JSON 成员",
			englishMessage: "unsupported API JSON member --mount-config.vpcConfig.vpcID",
			chineseMessage: "不支持 API JSON 成员 --mount-config.vpcConfig.vpcID",
		},
		{
			name:           "duplicate-inline-member",
			flag:           "--oss-volume-config",
			value:          `{"readOnly":true,"readOnly":false}`,
			path:           "oss-volume-config",
			english:        "one unambiguous JSON object",
			chinese:        "单个无歧义的 JSON 对象",
			englishMessage: "--oss-volume-config must be one unambiguous JSON object",
			chineseMessage: "--oss-volume-config 必须是单个无歧义的 JSON 对象",
		},
		{
			name:           "duplicate-file-member",
			flag:           "--mount-config",
			value:          "@" + duplicateFile,
			path:           "mount-config",
			english:        "one unambiguous JSON object",
			chinese:        "单个无歧义的 JSON 对象",
			englishMessage: "--mount-config must be one unambiguous JSON object",
			chineseMessage: "--mount-config 必须是单个无歧义的 JSON 对象",
		},
		{
			name:           "generic-invalid-parameter",
			flag:           "--oss-volume-config",
			value:          "",
			path:           "oss-volume-config",
			english:        "parameter is invalid",
			chinese:        "参数无效",
			genericControl: true,
		},
	} {
		for _, locale := range []struct{ lang, reason, message string }{
			{"en", tc.english, tc.englishMessage},
			{"zh-CN", tc.chinese, tc.chineseMessage},
		} {
			t.Run(tc.name+"/"+locale.lang, func(t *testing.T) {
				fake := &callOnlyAPICaller{}
				factoryCalls := 0
				factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
					factoryCalls++
					return fake, nil
				}
				stdout, stderr, exit := runCLIWith(factory, "--lang", locale.lang, "--output=json",
					"sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-1",
					"--region", "cn-hangzhou", tc.flag, tc.value)
				t.Logf("exit=%d factory_calls=%d caller_calls=%d stdout=%s stderr=%q",
					exit, factoryCalls, fake.calls, stdout, stderr)

				if exit != 1 || factoryCalls != 0 || fake.calls != 0 {
					t.Fatalf("CLI rejection contract: got exit=%d factory_calls=%d caller_calls=%d, want exit=1 and zero calls",
						exit, factoryCalls, fake.calls)
				}
				var envelope struct {
					Error struct {
						Code      string `json:"code"`
						Kind      string `json:"kind"`
						Retryable *bool  `json:"retryable"`
						Field     string `json:"field"`
						Message   string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
					t.Fatalf("CLI rejection contract: decode JSON stdout: %v", err)
				}
				got := envelope.Error
				if got.Code != "InvalidParameter" || got.Kind != "client" || got.Retryable == nil || *got.Retryable {
					t.Fatalf("CLI rejection contract: expected InvalidParameter/client/retryable=false, got stdout=%s", stdout)
				}

				if tc.genericControl {
					if got.Field != tc.path || got.Message != locale.reason {
						t.Errorf("generic InvalidParameter control: got field=%q message=%q, want field=%q message=%q",
							got.Field, got.Message, tc.path, locale.reason)
					}
					return
				}
				if got.Field != tc.path || got.Message != locale.message ||
					!strings.Contains(got.Message, "--"+tc.path) || !strings.Contains(got.Message, locale.reason) {
					t.Errorf("localized API JSON diagnostic: got field=%q message=%q, want field=%q message=%q containing %q and %q",
						got.Field, got.Message, tc.path, locale.message, "--"+tc.path, locale.reason)
				}
			})
		}
	}
}

func TestFCSandboxVolumeRejectsExplicitNonpositiveMutationTimeout(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		for _, noWait := range []bool{false, true} {
			for _, timeout := range []string{"0s", "-1s"} {
				called := false
				factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
					called = true
					return &fakeAPICaller{}, nil
				}
				args := []string{"--lang", "en", "sandbox", "volume", action}
				if action == "create" {
					args = append(args, "--name", "workspace")
				} else {
					args = append(args, "vol-1")
				}
				args = append(args, "--team-id", "team-1", "--region", "cn-hangzhou", "--timeout", timeout, "--output=json")
				if noWait {
					args = append(args, "--no-wait")
				}
				stdout, _, code := runCLIWith(factory, args...)
				if code == 0 || called || !strings.Contains(stdout, "InvalidVolumeTimeout") {
					t.Fatalf("%s noWait=%t timeout=%s: code=%d called=%t output=%s", action, noWait, timeout, code, called, stdout)
				}
			}
		}
	}
}

type scopedVolumeDeleteCaller struct{}

func (scopedVolumeDeleteCaller) Call(_ context.Context, operation string, _ map[string]any) (map[string]any, error) {
	if operation == "DeleteVolume" {
		return map[string]any{"code": "200", "requestId": "req-delete"}, nil
	}
	return nil, ecerrors.NotFound("NotFound", "gone", ecerrors.WithRawCause("VolumeNotFound", "gone"))
}

func TestFCSandboxVolumeDeleteReportsOnlyScopedAbsence(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return scopedVolumeDeleteCaller{}, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "volume", "delete", "vol-1", "--team-id", "team-1", "--region", "cn-hangzhou", "--output=json")
	if code != 0 || stderr != "" {
		t.Fatalf("scoped delete failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	result := decodeObject(t, stdout)
	if result["absent_in_scope"] != true || result["deleted"] != nil {
		t.Fatalf("scoped absence was presented as global deletion: %v", result)
	}
}

func TestFCSandboxVolumeGetNotFoundUsesSelectedLanguage(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return failingAPICaller{err: ecerrors.NotFound("NotFound", i18n.NewLocalizer("en").Message("VolumeNotFound"),
			ecerrors.WithRawCause("VolumeNotFound", "gone"), ecerrors.WithVerifiedProviderResponse())}, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "zh-CN", "sandbox", "volume", "get", "vol-1", "--team-id", "team-1", "--region", "cn-hangzhou", "--output=json")
	if code != 4 || stderr != "" {
		t.Fatalf("scoped Volume absence = code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	failure, _ := decodeObject(t, stdout)["error"].(map[string]any)
	if failure["code"] != "NotFound" || failure["message"] != "未找到存储卷" {
		t.Fatalf("selected language lost the Volume-specific absence: %v", failure)
	}
}

type volumeWaitFailureCaller struct{}

type volumeAncillaryFailureCaller struct{}

func (volumeAncillaryFailureCaller) Call(_ context.Context, operation string, _ map[string]any) (map[string]any, error) {
	if operation != "CreateVolume" {
		return nil, ecerrors.Client("UnexpectedCall", "unexpected Volume operation")
	}
	return nil, ecerrors.Service("InvalidVolumeResponse", "malformed optional Volume output", false, ecerrors.WithRequestID("req-create"), ecerrors.WithAcceptedVolume(ecerrors.AcceptedVolume{
		ID: "vol-owned-1", Name: "workspace", TeamID: "team-owned-1", Region: "cn-hangzhou", RequestID: "req-create", Operation: "CreateVolume", AllocationConfirmed: true,
	}))
}

type volumeContradictoryCreateCaller struct{ volumeAncillaryFailureCaller }

func (volumeContradictoryCreateCaller) Call(ctx context.Context, operation string, input map[string]any) (map[string]any, error) {
	_, err := (volumeAncillaryFailureCaller{}).Call(ctx, operation, input)
	accepted, _ := ecerrors.AcceptedVolumeFrom(err)
	accepted.AllocationConfirmed = false
	return nil, ecerrors.Service("InvalidVolumeResponse", "contradictory Volume output", false, ecerrors.WithRequestID("req-create"), ecerrors.WithAcceptedVolume(accepted))
}

func TestFCSandboxVolumeContradictoryCreateHasNoRecoveryCommand(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return volumeContradictoryCreateCaller{}, nil
	}
	stdout, _, code := runCLIWith(factory, "--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-owned-1", "--region", "cn-hangzhou", "--profile", "owner", "--no-wait", "--output=json")
	var payload map[string]any
	if code == 0 || json.Unmarshal([]byte(stdout), &payload) != nil {
		t.Fatalf("contradictory create result: code=%d output=%s", code, stdout)
	}
	failure, _ := payload["error"].(map[string]any)
	if failure["recovery_command"] != nil {
		t.Fatalf("contradictory create offered destructive recovery: %v", failure)
	}
}

func TestFCSandboxVolumeAncillaryCreateErrorRetainsScopedRecovery(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return volumeAncillaryFailureCaller{}, nil
	}
	for _, withTeam := range []bool{false, true} {
		args := []string{"--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--region", "cn-hangzhou", "--profile", "owner", "--no-wait", "--output=json"}
		if withTeam {
			args = append(args, "--team-id", "team-owned-1")
		}
		stdout, _, code := runCLIWith(factory, args...)
		if code == 0 {
			t.Fatalf("malformed optional output was reported as success: %s", stdout)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			t.Fatal(err)
		}
		errorPayload, _ := payload["error"].(map[string]any)
		accepted, _ := errorPayload["accepted_volume"].(map[string]any)
		if accepted["id"] != "vol-owned-1" || accepted["name"] != "workspace" ||
			accepted["team_id"] != "team-owned-1" || accepted["region"] != "cn-hangzhou" ||
			accepted["request_id"] != "req-create" || accepted["operation"] != "CreateVolume" {
			t.Fatalf("withTeam=%t: accepted allocation receipt is missing: %v", withTeam, payload)
		}
		actions, _ := payload["actions"].([]any)
		if len(actions) != 1 {
			t.Fatalf("withTeam=%t: CreateVolume action is missing: %v", withTeam, payload)
		}
		action, _ := actions[0].(map[string]any)
		if action["action_name"] != "CreateVolume" || action["code"] != "InvalidVolumeResponse" ||
			action["request_id"] != "req-create" || action["provider_verified"] == true {
			t.Fatalf("withTeam=%t: accepted error action changed: %v", withTeam, payload)
		}
		got, _ := errorPayload["recovery_command"].([]any)
		want := []any{"ecctl", "sandbox", "volume", "delete", "vol-owned-1", "--region", "cn-hangzhou", "--team-id", "team-owned-1", "--profile", "owner"}
		if errorPayload["code"] != "InvalidVolumeResponse" || len(got) != len(want) {
			t.Fatalf("withTeam=%t: accepted create error lost scoped recovery: %v", withTeam, payload)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("recovery command = %v, want %v", got, want)
			}
		}
	}
}

func (volumeWaitFailureCaller) Call(_ context.Context, operation string, _ map[string]any) (map[string]any, error) {
	if operation == "CreateVolume" {
		return map[string]any{"code": "200", "requestId": "req-create", "volume": map[string]any{"volumeID": "vol-owned-1", "volumeName": "workspace", "teamID": "team-owned-1", "status": "CREATING"}}, nil
	}
	return map[string]any{"code": "200", "requestId": "req-read", "volume": map[string]any{"volumeID": "vol-owned-1", "volumeName": "workspace", "teamID": "team-owned-1", "status": "ERROR", "statusReason": "failed"}}, nil
}

func TestFCSandboxVolumeWaitFailureHasExactRecoveryDelete(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return volumeWaitFailureCaller{}, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--region", "cn-hangzhou", "--profile", "owner", "--output=json")
	if code == 0 {
		t.Fatalf("failed Volume wait succeeded: %s %s", stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON error: %v: %s", err, stdout)
	}
	errorPayload, _ := payload["error"].(map[string]any)
	got, ok := errorPayload["recovery_command"].([]any)
	want := []any{"ecctl", "sandbox", "volume", "delete", "vol-owned-1", "--region", "cn-hangzhou", "--team-id", "team-owned-1", "--profile", "owner"}
	if !ok || len(got) != len(want) {
		t.Fatalf("accepted Volume recovery missing: %v", payload)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recovery command = %v, want %v", got, want)
		}
	}
}

func TestFCSandboxVolumeInitialErrorKeepsRecoveryInBothWaitModes(t *testing.T) {
	for _, noWait := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "no_wait"}[noWait], func(t *testing.T) {
			factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
				return &fakeAPICaller{response: map[string]any{"code": "200", "requestId": "req-create", "volume": map[string]any{"volumeID": "vol-owned-1", "volumeName": "workspace", "teamID": "team-owned-1", "status": "ERROR", "statusReason": "provider failed"}}}, nil
			}
			args := []string{"--lang", "en", "sandbox", "volume", "create", "--name", "workspace", "--region", "cn-hangzhou", "--profile", "owner", "--output=json"}
			if noWait {
				args = append(args, "--no-wait")
			}
			stdout, stderr, code := runCLIWith(factory, args...)
			if code == 0 {
				t.Fatalf("initial ERROR succeeded: %s %s", stdout, stderr)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("invalid error: %s %v", stdout, err)
			}
			failure, _ := payload["error"].(map[string]any)
			if failure["current_state"] != "ERROR" || failure["detail"] != "provider failed" || !strings.Contains(stdout, "vol-owned-1") {
				t.Fatalf("initial ERROR lost provider reason or accepted recovery ID: %s", stdout)
			}
			accepted, _ := failure["accepted_volume"].(map[string]any)
			command, _ := failure["recovery_command"].([]any)
			if accepted["team_id"] != "team-owned-1" || !slices.Contains(command, any("team-owned-1")) {
				t.Fatalf("initial ERROR lost provider accepted Team: %v", payload)
			}
		})
	}
}

func TestFCSandboxVolumeInitialErrorUsesSelectedLanguage(t *testing.T) {
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return &fakeAPICaller{response: map[string]any{"code": "200", "volume": map[string]any{"volumeID": "vol-owned-1", "volumeName": "workspace", "teamID": "team-owned-1", "status": "ERROR", "statusReason": "provider failed"}}}, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "zh-CN", "sandbox", "volume", "create", "--name", "workspace", "--team-id", "team-owned-1", "--region", "cn-hangzhou", "--profile", "owner", "--output=json")
	if code == 0 {
		t.Fatalf("initial ERROR succeeded: %s %s", stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON: %s: %v", stdout, err)
	}
	failure, _ := payload["error"].(map[string]any)
	if failure["code"] != "WaitFailed" || failure["message"] != "存储卷进入失败状态 ERROR" || failure["detail"] != "provider failed" {
		t.Fatalf("localized Volume error lost stable fields: %v", failure)
	}
}

type volumeUpdateCall struct {
	operation string
	request   map[string]any
}

type volumeUpdateCaller struct {
	calls    []volumeUpdateCall
	response map[string]any
}

func (f *volumeUpdateCaller) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, volumeUpdateCall{operation: operation, request: request})
	return f.response, nil
}

func TestFCSandboxVolumeUpdateReplacesConfigAndReadsBack(t *testing.T) {
	fake := &volumeUpdateCaller{response: map[string]any{"code": "200", "requestId": "req-update", "volume": map[string]any{
		"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "AVAILABLE",
	}}}
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return fake, nil
	}
	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "sandbox", "volume", "update", "vol-1", "--team-id", "team-1",
		"--oss-volume-config", `{"bucketName":"bucket","bucketPath":"/","endpoint":"https://oss.example","readOnly":false}`,
		"--region", "cn-hangzhou", "--output=json")
	if code != 0 || stderr != "" || len(fake.calls) != 2 {
		t.Fatalf("Volume update: code=%d stdout=%s stderr=%s calls=%+v", code, stdout, stderr, fake.calls)
	}
	if fake.calls[0].operation != "UpdateVolume" || fake.calls[0].request["volumeID"] != "vol-1" || fake.calls[0].request["teamID"] != "team-1" {
		t.Fatalf("update request: %+v", fake.calls[0])
	}
	if sent, _ := fake.calls[0].request["body.ossVolumeConfig"].(string); !strings.Contains(sent, `"readOnly":false`) {
		t.Fatalf("update did not send the storage configuration body: %+v", fake.calls[0])
	}
	if fake.calls[1].operation != "GetVolume" || fake.calls[1].request["teamID"] != "team-1" {
		t.Fatalf("readback lost the same-Team scope: %+v", fake.calls[1])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON: %s: %v", stdout, err)
	}
	volume, _ := payload["volume"].(map[string]any)
	if volume["team_id"] != "team-1" || volume["id"] != "vol-1" {
		t.Fatalf("update output lost the updated volume: %s", stdout)
	}
}

func TestFCSandboxVolumeUpdateRequiresTeamScopeAndConfiguration(t *testing.T) {
	fake := &volumeUpdateCaller{}
	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return fake, nil
	}
	for _, args := range [][]string{
		{"--region", "cn-hangzhou", "--output=json"},
		{"--team-id", "team-1", "--region", "cn-hangzhou", "--output=json"},
		{"--oss-volume-config", `{"bucketName":"bucket"}`, "--region", "cn-hangzhou", "--output=json"},
	} {
		args = append([]string{"--lang", "en", "sandbox", "volume", "update", "vol-1"}, args...)
		stdout, stderr, code := runCLIWith(factory, args...)
		if code == 0 || len(fake.calls) != 0 {
			t.Fatalf("incomplete Volume update reached provider: code=%d stdout=%s stderr=%s calls=%+v", code, stdout, stderr, fake.calls)
		}
	}
}

func TestIssue128HumanMachineJSON(t *testing.T) {
	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("ECCTL_DISABLE_TELEMETRY", "1")
	t.Setenv("ECCTL_DISABLE_UPDATE_CHECK", "1")
	for _, noColor := range []bool{false, true} {
		fake := &fakeAPICaller{response: map[string]any{
			"code": "200", "requestId": "req-create",
			"volume": map[string]any{"volumeID": "vol-1", "volumeName": "workspace", "teamID": "team-1", "status": "CREATING"},
		}}
		factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
			return fake, nil
		}
		args := []string{"--output=json", "--agent-envelope=false", "--region", "cn-hangzhou", "sbx", "--lang", "en",
			"volume", "create", "--name", "workspace", "--team-id", "team-1", "--no-wait"}
		if noColor {
			args = append(args, "--no-color=true")
		}
		stdout, stderr, code := runCLIWith(factory, args...)
		if code != 0 || fake.operation != "CreateVolume" {
			t.Fatalf("real CLI did not execute the inert CreateVolume: --no-color=%v exit=%d stderr=%q stdout=%q", noColor, code, stderr, stdout)
		}
		if json.Valid([]byte(stdout)) != noColor || strings.Contains(stdout, "\x1b[") == noColor {
			t.Errorf("real CLI output disagrees with --no-color=%v: %q", noColor, stdout)
		}
	}
}
