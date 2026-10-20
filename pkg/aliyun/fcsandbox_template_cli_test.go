package aliyun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestNativeTemplateCLINonStringSensitiveResponses(t *testing.T) {
	for _, value := range []any{json.Number("8642097531"), true, false, nil} {
		for _, action := range []string{"get", "list"} {
			t.Run(fmt.Sprintf("%s/%v", action, value), func(t *testing.T) {
				item := map[string]any{"code": "200", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "ready"}, "runtimeConfig": map[string]any{
					"cpu": 0.5, "internetAccess": false,
					"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": value, "userName": value, "nested": []any{value}}}},
				}}
				response, args := any(item), []string{"get", "tpl-1"}
				if action == "list" {
					response, args = map[string]any{"code": "200", "templates": []any{item}}, []string{"list"}
				}
				encoded, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				caller := aliyun.NativeTemplateOutputCallerForTest(t, []string{string(encoded)}, 0)
				output, code := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
				if code != 0 {
					t.Fatalf("accepted anomalous response did not reach CLI output: %d %s", code, output)
				}
				var decoded any
				if err := json.Unmarshal([]byte(output), &decoded); err != nil {
					t.Fatal(err)
				}
				var config map[string]any
				var find func(any)
				find = func(node any) {
					switch node := node.(type) {
					case map[string]any:
						if value, ok := node["runtime_config"].(map[string]any); ok {
							config = value
						}
						for _, child := range node {
							find(child)
						}
					case []any:
						for _, child := range node {
							find(child)
						}
					}
				}
				find(decoded)
				if config == nil || config["cpu"] != 0.5 || config["internetAccess"] != false {
					t.Fatalf("nonsensitive typed configuration lost at CLI boundary: %s", output)
				}
				sandbox, _ := config["sandboxConfig"].(map[string]any)
				registry, _ := sandbox["registryConfig"].(map[string]any)
				auth, _ := registry["authConfig"].(map[string]any)
				nested, _ := auth["nested"].([]any)
				if auth["password"] != "[REDACTED]" || auth["userName"] != "[REDACTED]" || len(nested) != 1 || nested[0] != "[REDACTED]" {
					t.Errorf("non-string authentication value reached CLI stdout: %s", output)
				}
			})
		}
	}
}

func TestNativeTemplateCLILiteralConfigMembers(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, field := range []string{"runtime-config", "build-config"} {
			valid, padded, collision, nested := `{"cpu":0.5,"internetAccess":false}`, `{" cpu ":0.5}`, `{"cpu":0.5," cpu ":1}`, `{"vpcConfig":{" vSwitchIds ":["vsw-1"]}}`
			if field == "build-config" {
				valid, padded, collision, nested = `{"copy":{"enabled":false}}`, `{" copy ":{"enabled":false}}`, `{"copy":{"enabled":false}," copy ":{"enabled":true}}`, `{"copy":{" enabled ":true}}`
			}
			for _, file := range []bool{false, true} {
				for _, tc := range []struct {
					name, input string
					invalid     bool
				}{{"valid", valid, false}, {"padded", padded, true}, {"collision", collision, true}, {"unknown", `{"unsupported":false}`, true}, {"nested", nested, true}} {
					t.Run(action+"/"+field+"/"+tc.name+map[bool]string{false: "/inline", true: "/file"}[file], func(t *testing.T) {
						input := tc.input
						if file {
							path := filepath.Join(t.TempDir(), "input.json")
							if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
								t.Fatal(err)
							}
							input = "@" + path
						}
						caller, calls := aliyun.NativeTemplateSTSCallerForTest(t, time.Now().Add(time.Hour))
						args := []string{action}
						if action == "create" {
							args = append(args, "--name", "native")
						} else {
							args = append(args, "tpl-1")
						}
						args = append(args, "--"+field, input, "--no-wait")
						output, code := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
						if tc.invalid {
							if code != 1 || calls() != 0 || !strings.Contains(output, `"code":"InvalidFCTemplateConfig"`) {
								t.Errorf("literal member changed or collided before validation: exit=%d calls=%d %s", code, calls(), output)
							}
						} else if code != 0 || calls() != 1 {
							t.Errorf("valid typed JSON failed: exit=%d calls=%d %s", code, calls(), output)
						}
					})
				}
			}
		}
	}
}

func TestNativeTemplateCLIInvalidConfigIsPreMutationClientFailure(t *testing.T) {
	caller, calls := aliyun.NativeTemplateSTSCallerForTest(t, time.Now().Add(time.Hour))
	output, code := runNativeTemplateTestCLI(t, context.Background(), caller, "create", "--name", "invalid", "--runtime-config", `{"internetAccess":"wrong"}`, "--no-wait")
	if code != 1 || calls() != 0 || !strings.Contains(output, `"kind":"client"`) || !strings.Contains(output, `"code":"InvalidFCTemplateConfig"`) || strings.Contains(output, "recovery_command") {
		t.Fatalf("trusted no-allocation failure crossed mutation boundary: exit=%d calls=%d %s", code, calls(), output)
	}
}

func runNativeTemplateTestCLI(t *testing.T, ctx context.Context, caller engine.Caller, args ...string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	t.Setenv("ECCTL_PROFILE", "")
	ctx = cli.WithResourceCallerFactory(ctx, func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || region != "cn-hangzhou" {
			t.Fatalf("wrong native boundary: %+v %q", resource, region)
		}
		return caller, nil
	})
	args = append(append([]string{"--lang", "en", "sandbox", "template"}, args...), "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1")
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, args, &stdout, &stderr)
	return stdout.String() + stderr.String(), code
}

func TestNativeTemplateCLIUnknownAmbiguousAssignmentsFailClosed(t *testing.T) {
	for _, value := range []struct{ assignment, leak string }{{"&PRIVATE99", "PRIVATE99"}, {"blue river", "river"}, {"abc,xyz", "xyz"}, {`"PRIVATE99`, "PRIVATE99"}, {`"abc"PRIVATE99`, "PRIVATE99"}} {
		for _, path := range []string{"SDK", "business", "build", "get", "list"} {
			t.Run(value.assignment+"/"+path, func(t *testing.T) {
				reason := "pull failed password=" + value.assignment
				encode := func(value any) string {
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return string(data)
				}
				item := map[string]any{"code": "200", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "failed", "reason": map[string]any{"message": reason}}}
				responses, sdkStatus := []string{encode(item)}, 0
				args, wantCode, positive := []string{"get", "tpl-1"}, 0, "tpl-1"
				switch path {
				case "SDK", "business":
					args, wantCode, positive = []string{"create", "--name", "native"}, 2, "req-error"
					responses = []string{encode(map[string]any{"code": "InvalidParameter", "requestId": "req-error", "message": reason})}
					if path == "SDK" {
						sdkStatus = 400
					}
				case "build":
					args, wantCode, positive = []string{"create", "--name", "native"}, 2, "FCTemplateBuildFailed"
					responses = append([]string{`{"code":"200","templateID":"tpl-1"}`}, responses...)
				case "list":
					args = []string{"list"}
					responses = []string{encode(map[string]any{"code": "200", "templates": []any{item}})}
				}
				if !strings.Contains(strings.Join(responses, ""), value.leak) {
					t.Fatal("unsafe fixture does not contain the disputed suffix")
				}
				caller := aliyun.NativeTemplateOutputCallerForTest(t, responses, sdkStatus)
				output, code := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
				if code != wantCode || !strings.Contains(output, positive) {
					t.Fatalf("did not reach full native output: %d %s", code, output)
				}
				if strings.Contains(output, value.leak) || !strings.Contains(output, "REDACTED") {
					t.Fatalf("ambiguous credential value survived: %s", output)
				}
			})
		}
	}
}

func TestNativeTemplateCLIPaginationPreservesOpaqueToken(t *testing.T) {
	for _, tc := range []struct{ token, username string }{{"password=cursor-2", ""}, {"password=blue river", ""}, {"page-2", "a"}, {"cursor-2", "cursor-2"}} {
		t.Run(tc.token+"/"+tc.username, func(t *testing.T) {
			caller, captured := aliyun.NativeTemplatePaginationCallerForTest(t, tc.token, tc.username)
			output, code := runNativeTemplateTestCLI(t, context.Background(), caller, "list", "--limit", "1")
			var data struct {
				Pagination struct {
					NextToken string `json:"next_token"`
					HasMore   bool   `json:"has_more"`
				} `json:"pagination"`
			}
			if code != 0 || json.Unmarshal([]byte(output), &data) != nil || !data.Pagination.HasMore || data.Pagination.NextToken != tc.token {
				t.Fatalf("validated cursor was changed before CLI: %d %s", code, output)
			}
			output, code = runNativeTemplateTestCLI(t, context.Background(), caller, "list", "--limit", "1", "--next-token", data.Pagination.NextToken)
			if code != 0 || captured() != tc.token || !strings.Contains(output, `"has_more":false`) {
				t.Fatalf("next query did not carry exact cursor: %q %d %s", captured(), code, output)
			}
		})
	}
}

func TestNativeTemplateCLIWaitingTimeoutGuidanceIsLocalized(t *testing.T) {
	for _, language := range []struct{ name, message string }{{"en", "greater than zero"}, {"zh-CN", "大于零"}} {
		caller, calls := aliyun.NativeTemplateSTSCallerForTest(t, time.Now().Add(time.Hour))
		probe := &nativeTemplateBudgetProbe{OpenAPICaller: caller}
		output, code := runNativeTemplateTestCLI(t, context.Background(), probe, "create", "--name", "native", "--timeout", "0s", "--lang", language.name)
		if code == 0 || probe.prepared || calls() != 0 || !strings.Contains(output, `"code":"InvalidFCTemplateWaitTimeout"`) || !strings.Contains(output, language.message) {
			t.Fatalf("waiting rejection lost localized guidance: %d calls=%d %s", code, calls(), output)
		}
	}
}

func TestNativeTemplateCLIPartialAuthKeepsAssignmentLabels(t *testing.T) {
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	password := `MYSECRETZ99 with spaces, quote"tail`
	jsonReason := encode(map[string]string{"password": password})
	template := map[string]any{"code": "200", "requestId": "req-get", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "failed", "reason": map[string]any{"message": jsonReason}}}
	for _, username := range []string{"a", "p", "word", "200", "ready", "req", "D"} {
		for _, path := range []string{"SDK", "business", "build", "get", "list"} {
			t.Run(username+"/"+path, func(t *testing.T) {
				config := encode(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"userName": username}}}})
				responses := []string{`{"code":"200","templateID":"tpl-1"}`, encode(template)}
				sdkStatus := 0
				wantCode, positive := 2, "FCTemplateBuildFailed"
				if path == "SDK" || path == "business" {
					responses = []string{encode(map[string]any{"code": "InvalidParameter", "requestId": "req-write", "message": jsonReason})}
					positive = "req-write"
					if path == "SDK" {
						sdkStatus = 400
					}
				} else if path == "get" || path == "list" {
					wantCode, positive = 0, `"build_status":"failed"`
					if path == "list" {
						responses[1] = encode(map[string]any{"code": "200", "templates": []any{template}})
					}
				}
				if !strings.Contains(strings.Join(responses, ""), "MYSECRETZ99") {
					t.Fatal("unsafe fixture does not carry the disputed password")
				}
				caller := aliyun.NativeTemplateOutputCallerForTest(t, responses, sdkStatus)
				args := []string{"update", "tpl-1", "--runtime-config", config}
				if path == "get" || path == "list" {
					out, code := runNativeTemplateTestCLI(t, context.Background(), caller, append(args, "--no-wait")...)
					if code != 0 || !strings.Contains(out, "tpl-1") {
						t.Fatalf("partial update was not accepted: %d %s", code, out)
					}
					args = []string{path}
					if path == "get" {
						args = append(args, "tpl-1")
					}
				}
				output, code := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
				if code != wantCode || !strings.Contains(output, positive) {
					t.Fatalf("did not reach expected native output: %d %s", code, output)
				}
				if strings.Contains(output, "MYSECRETZ99") {
					t.Fatalf("remembered username corrupted a password label: %s", output)
				}
			})
		}
	}
}

type nativeTemplateBudgetKey struct{}

type nativeTemplateBudgetProbe struct {
	*aliyun.OpenAPICaller
	deadline    time.Time
	hasDeadline bool
	prepared    bool
	parentValue any
	contextErr  error
}

func (probe *nativeTemplateBudgetProbe) PrepareOperationContext(ctx context.Context) (context.Context, error) {
	probe.prepared = true
	probe.deadline, probe.hasDeadline = ctx.Deadline()
	probe.parentValue, probe.contextErr = ctx.Value(nativeTemplateBudgetKey{}), ctx.Err()
	return probe.OpenAPICaller.PrepareOperationContext(ctx)
}

func TestNativeTemplateCLINoWaitShortSTSRequestBudget(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, tc := range []struct {
			name, timeout, errorCode string
			noWait, canceled         bool
			lifetime, outer, budget  time.Duration
		}{
			{"default", "", "", true, false, 5 * time.Minute, 0, 30 * time.Second},
			{"explicit short", "20s", "", true, false, 5 * time.Minute, 0, 20 * time.Second},
			{"explicit long", "10m", "CredentialLeaseExpired", true, false, 5 * time.Minute, 0, 10 * time.Minute},
			{"explicit zero", "0s", "", true, false, 5 * time.Minute, 0, 0},
			{"normal wait", "", "CredentialLeaseExpired", false, false, 5 * time.Minute, 0, 15 * time.Minute},
			{"zero wait", "0s", "InvalidFCTemplateWaitTimeout", false, false, 5 * time.Minute, 0, 0},
			{"negative wait", "-1s", "InvalidFCTemplateWaitTimeout", false, false, 5 * time.Minute, 0, 0},
			{"waiting shorter parent", "20s", "", false, false, 5 * time.Minute, 5 * time.Second, 5 * time.Second},
			{"expired", "", "CredentialLeaseExpired", true, false, -time.Minute, 0, 30 * time.Second},
			{"shorter than request", "", "CredentialLeaseExpired", true, false, 10 * time.Second, 0, 30 * time.Second},
			{"outer shorter", "", "", true, false, 5 * time.Minute, 5 * time.Second, 5 * time.Second},
			{"outer longer", "", "", true, false, 5 * time.Minute, 10 * time.Minute, 30 * time.Second},
			{"explicit bounded by outer", "10m", "", true, false, 5 * time.Minute, 5 * time.Second, 5 * time.Second},
			{"canceled parent", "", "", true, true, 5 * time.Minute, 0, 30 * time.Second},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				caller, calls := aliyun.NativeTemplateSTSCallerForTest(t, time.Now().Add(tc.lifetime))
				probe := &nativeTemplateBudgetProbe{OpenAPICaller: caller}
				ctx := context.WithValue(context.Background(), nativeTemplateBudgetKey{}, "parent-preserved")
				if tc.outer > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.outer)
					defer cancel()
				}
				if tc.canceled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				args := []string{action}
				if action == "create" {
					args = append(args, "--name", "native")
				} else {
					args = append(args, "tpl-1", "--runtime-config", `{"internetAccess":false}`)
				}
				args = append(args, "--profile", "short-sts")
				if tc.noWait {
					args = append(args, "--no-wait")
				}
				if tc.timeout != "" {
					args = append(args, "--timeout", tc.timeout)
				}
				started := time.Now()
				output, code := runNativeTemplateTestCLI(t, ctx, probe, args...)
				if tc.errorCode == "InvalidFCTemplateWaitTimeout" {
					if code == 0 || calls() != 0 || probe.prepared || !strings.Contains(output, `"code":"`+tc.errorCode+`"`) || !strings.Contains(output, "greater than zero") {
						t.Fatalf("nonpositive wait reached credentials/API: %d calls=%d prepared=%v %s", code, calls(), probe.prepared, output)
					}
					return
				}
				if tc.canceled {
					if code == 0 || calls() != 0 || probe.prepared && probe.contextErr != context.Canceled {
						t.Fatalf("parent cancellation lost: exit=%d calls=%d err=%v %s", code, calls(), probe.contextErr, output)
					}
					return
				}
				if !probe.prepared || probe.parentValue != "parent-preserved" {
					t.Fatalf("credential preparation/parent context lost: %+v %s", probe, output)
				}
				if probe.hasDeadline != (tc.budget > 0) {
					t.Fatalf("explicit timeout deadline semantics changed: %+v %s", probe, output)
				}
				if remaining := probe.deadline.Sub(started); tc.budget > 0 && (remaining < tc.budget-time.Second || remaining > tc.budget+time.Second) {
					t.Fatalf("no-wait request budget=%s; want %s; %s", remaining, tc.budget, output)
				}
				if tc.errorCode != "" {
					if code == 0 || calls() != 0 || !strings.Contains(output, `"code":"`+tc.errorCode+`"`) {
						t.Fatalf("shared static lifetime guard bypassed: %d calls=%d %s", code, calls(), output)
					}
				} else {
					wantCalls := 1
					if !tc.noWait {
						wantCalls = 2
					}
					if code != 0 || calls() != wantCalls || !strings.Contains(output, "tpl-1") || tc.noWait && strings.Contains(output, `"action_name":"GetTemplate"`) {
						t.Fatalf("valid STS did not perform expected requests: %d calls=%d want=%d %s", code, calls(), wantCalls, output)
					}
				}
			})
		}
	}
}

func TestNativeTemplateCLICompletePasswordRedaction(t *testing.T) {
	secret := `prefix"secret-tail`
	encode := func(value any) string {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	quoted := encode(secret)
	escaped := quoted[1 : len(quoted)-1]
	jsonEcho := "safe-before " + encode(map[string]string{"password": secret}) + " safe-after"
	registryConfig := func(password string) string {
		return encode(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": password}}}})
	}
	config := registryConfig(secret)
	template := func(reason string) map[string]any {
		return map[string]any{"code": "200", "requestId": "req-get", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "failed", "reason": map[string]any{"message": reason}}}
	}
	for _, tc := range []struct {
		name, leak, positive string
		args                 []string
		responses            []string
		sdkStatus, exit      int
	}{
		{"SDK escaped value", "secret-tail", "req-sdk", []string{"create", "--name", "native", "--runtime-config", config}, []string{encode(map[string]any{"code": "InvalidParameter", "requestId": "req-sdk", "message": "safe-before credential " + escaped + " safe-after"})}, 400, 2},
		{"SDK ordinary JSON escaping", "secret-tail", "req-sdk", []string{"create", "--name", "native", "--runtime-config", registryConfig(secret + "<&")}, []string{encode(map[string]any{"code": "InvalidParameter", "requestId": "req-sdk", "message": "safe-before credential " + escaped + "<& safe-after"})}, 400, 2},
		{"business JSON", "secret-tail", "req-business", []string{"create", "--name", "native", "--runtime-config", config}, []string{encode(map[string]any{"code": "InvalidParameter", "requestId": "req-business", "message": jsonEcho})}, 0, 2},
		{"build reason JSON", "secret-tail", "FCTemplateBuildFailed", []string{"create", "--name", "native", "--runtime-config", config}, []string{`{"code":"200","templateID":"tpl-1"}`, encode(template(jsonEcho))}, 0, 2},
		{"independent get quoted space", "river", "tpl-1", []string{"get", "tpl-1"}, []string{encode(template(`safe-before password="blue river" safe-after`))}, 0, 0},
		{"independent list quoted comma", "xyz", "tpl-1", []string{"list"}, []string{encode(map[string]any{"code": "200", "templates": []any{template(`safe-before password="abc,xyz" safe-after`)}})}, 0, 0},
		{"independent get escaped JSON", "secret-tail", "tpl-1", []string{"get", "tpl-1"}, []string{encode(template(jsonEcho))}, 0, 0},
		{"independent single quote", "river", "tpl-1", []string{"get", "tpl-1"}, []string{encode(template(`safe-before password='blue river' safe-after`))}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Prove the leak assertion checks a value actually emitted by this fixture.
			if !strings.Contains(strings.Join(tc.responses, ""), tc.leak) {
				t.Fatal("unsafe fixture does not contain the disputed password suffix")
			}
			dir := t.TempDir()
			t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl.json"))
			t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
			t.Setenv("ECCTL_PROFILE", "")
			caller := aliyun.NativeTemplateOutputCallerForTest(t, tc.responses, tc.sdkStatus)
			ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
				if resource.Provider != "aliyun" || resource.APIProduct != "FCSandbox" || region != "cn-hangzhou" {
					t.Fatalf("wrong provider boundary: %+v %q", resource, region)
				}
				return caller, nil
			})
			args := append([]string{"--lang", "en", "sandbox", "template"}, tc.args...)
			args = append(args, "--openapi", "--region", "cn-hangzhou", "--team-id", "team-1")
			var stdout, stderr bytes.Buffer
			code := cli.Run(ctx, args, &stdout, &stderr)
			output := stdout.String() + stderr.String()
			if code != tc.exit || !strings.Contains(output, tc.positive) {
				t.Fatalf("did not reach the expected output boundary: exit=%d output=%s", code, output)
			}
			if strings.Contains(output, tc.leak) || strings.Contains(output, secret) || strings.Contains(output, escaped) {
				t.Fatalf("complete CLI output retains a password suffix: %s", output)
			}
		})
	}
}

func TestNativeTemplateCLIStatePresentation(t *testing.T) {
	const secret = "synthetic-registry-password-88"
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, action := range []string{"create", "update"} {
		for _, tc := range []struct {
			name, state, password, wantState, errorCode string
			redacted                                    bool
		}{
			{"ready collision", "ready", "ready", "ready", "", false},
			{"error collision", "error", "error", "error", "FCTemplateBuildFailed", false},
			{"failed collision", "FAILED", "FAILED", "FAILED", "FCTemplateBuildFailed", false},
			{"building collision", "building", "building", "building", "WaitTimeout", false},
			{"waiting collision", "waiting", "waiting", "waiting", "WaitTimeout", false},
			{"unknown assignment", "building password=" + secret, secret, "[REDACTED]", "WaitTimeout", true},
			{"unknown embedded value", "building " + secret, secret, "building [REDACTED]", "WaitTimeout", true},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				response := encode(map[string]any{"code": "200", "requestId": "req-get", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": tc.state}})
				if tc.redacted && !strings.Contains(response, secret) {
					t.Fatal("fixture does not contain the disputed password")
				}
				caller := aliyun.NativeTemplateOutputCallerForTest(t, []string{`{"code":"200","requestId":"req-write","templateID":"tpl-1"}`, response}, 0)
				args := []string{action}
				if action == "create" {
					args = append(args, "--name", "native")
				} else {
					args = append(args, "tpl-1")
				}
				config := encode(map[string]any{"sandboxConfig": map[string]any{"registryConfig": map[string]any{"authConfig": map[string]any{"password": tc.password}}}})
				args = append(args, "--runtime-config", config, "--timeout", "200ms")
				output, exit := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
				if tc.errorCode == "" {
					if exit != 0 || !strings.Contains(output, `"build_status":"`+tc.wantState+`"`) {
						t.Fatalf("ready semantics changed: exit=%d %s", exit, output)
					}
				} else if exit == 0 || !strings.Contains(output, `"code":"`+tc.errorCode+`"`) || !strings.Contains(output, `"current_state":"`+tc.wantState+`"`) {
					t.Errorf("wait/failure presentation changed: exit=%d %s", exit, output)
				}
				if !strings.Contains(output, `"action_name":"GetTemplate"`) || !strings.Contains(output, "req-get") {
					t.Errorf("did not reach the provider/waiter output boundary: %s", output)
				}
				if tc.redacted && strings.Contains(output, secret) {
					t.Errorf("CLI current_state retained the submitted password: %s", output)
				}
			})
		}
	}
	for _, action := range []string{"get", "list"} {
		t.Run(action+"/unknown assignment", func(t *testing.T) {
			item := map[string]any{"code": "200", "requestId": "req-read", "templateID": "tpl-1", "teamID": "team-1", "status": map[string]any{"state": "building password=" + secret}}
			response, args := encode(item), []string{"get", "tpl-1"}
			if action == "list" {
				response, args = encode(map[string]any{"code": "200", "requestId": "req-read", "templates": []any{item}}), []string{"list"}
			}
			caller := aliyun.NativeTemplateOutputCallerForTest(t, []string{response}, 0)
			output, exit := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
			if exit != 0 || !strings.Contains(output, `"build_status":"[REDACTED]"`) || !strings.Contains(output, `"id":"tpl-1"`) {
				t.Errorf("read state was not sanitized at the response boundary: exit=%d %s", exit, output)
			}
			if strings.Contains(output, secret) {
				t.Errorf("CLI read retained an unknown password assignment: %s", output)
			}
		})
	}
}

func TestNativeTemplateCLIErrorCodePresentation(t *testing.T) {
	const secret = "synthetic-registry-password-88"
	for _, action := range []string{"create", "update"} {
		for _, sdkStatus := range []int{0, 400} {
			path := "business"
			if sdkStatus != 0 {
				path = "SDK"
			}
			for _, tc := range []struct {
				name, code, wantCode string
				redacted             bool
			}{
				{"structured control", "InvalidParameter.RegistryConfig", "InvalidParameter.RegistryConfig", false},
				{"known assignment", "RegistryError password=" + secret, "[REDACTED]", true},
				{"known embedded value", "RegistryError." + secret, "RegistryError.[REDACTED]", true},
				{"unknown assignment", "RegistryError password=unremembered-password", "[REDACTED]", true},
			} {
				t.Run(action+"/"+path+"/"+tc.name, func(t *testing.T) {
					data, err := json.Marshal(map[string]any{"code": tc.code, "message": "rejected", "requestId": "req-failure"})
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(data), tc.code) {
						t.Fatal("fixture does not contain the disputed code")
					}
					caller := aliyun.NativeTemplateOutputCallerForTest(t, []string{string(data)}, sdkStatus)
					args := []string{action}
					if action == "create" {
						args = append(args, "--name", "native")
					} else {
						args = append(args, "tpl-1")
					}
					args = append(args, "--runtime-config", `{"sandboxConfig":{"registryConfig":{"authConfig":{"password":"`+secret+`"}}}}`, "--no-wait")
					output, exit := runNativeTemplateTestCLI(t, context.Background(), caller, args...)
					var payload struct {
						Error   struct{ Code string } `json:"error"`
						Actions []struct {
							Code      string `json:"code"`
							RequestID string `json:"request_id"`
						} `json:"actions"`
					}
					if err := json.Unmarshal([]byte(output), &payload); err != nil {
						t.Fatalf("invalid CLI JSON: %v %s", err, output)
					}
					if exit != 2 || payload.Error.Code != "CloudAPIError" || len(payload.Actions) != 1 || payload.Actions[0].RequestID != "req-failure" {
						t.Fatalf("business/SDK classification or evidence changed: exit=%d %s", exit, output)
					}
					if payload.Actions[0].Code != tc.wantCode {
						t.Errorf("public action code=%q; want %q", payload.Actions[0].Code, tc.wantCode)
					}
					if tc.redacted && (strings.Contains(output, secret) || strings.Contains(output, "unremembered-password")) {
						t.Errorf("CLI action code retained a password: %s", output)
					}
				})
			}
		}
	}
}
