package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ecctl/pkg/engine"
	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/schema"
	"ecctl/pkg/spec"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ecctl-cli-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl-config.json"))
	os.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun-config.json"))
	os.Setenv("ECCTL_DISPLAY_MODE", "AI")
	os.Unsetenv("AGENT_FIRST")
	// Isolate region resolution from the host environment so tests that assert
	// "no region configured" behave the same on machines that export a generic
	// REGION/REGION_ID (common on cloud dev hosts).
	for _, name := range []string{
		"ECCTL_REGION",
		"ALIBABA_CLOUD_REGION_ID",
		"ALIBABACLOUD_REGION_ID",
		"ALICLOUD_REGION_ID",
		"REGION_ID",
		"REGION",
	} {
		os.Unsetenv(name)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCapabilitiesReportsPublicSurface(t *testing.T) {
	stdout, stderr, code := runCLI("--no-color", "capabilities", "--output", "json")
	if code != 0 {
		t.Fatalf("capabilities exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["surface"]; got != "public" {
		t.Fatalf("surface = %#v, want public", got)
	}
}

func runCLI(args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func runCLIWithFullSurface(args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := Run(WithFullCommandSurface(context.Background()), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func runCLIWith(factory ResourceCallerFactory, args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	ctx := WithResourceCallerFactory(context.Background(), factory)
	code := Run(ctx, args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func runCLIWithAPI(factory APICallerFactory, args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	ctx := WithAPICallerFactory(context.Background(), factory)
	code := Run(ctx, args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
			out.WriteByte(s[i])
			i++
			continue
		}
		i += 2
		for i < len(s) {
			c := s[i]
			i++
			if c >= '@' && c <= '~' {
				break
			}
		}
	}
	return out.String()
}

const vpcListDefaultLimit = 50

type fakeAPICaller struct {
	operation   string
	request     map[string]any
	passthrough []string
	response    map[string]any
}

func (f *fakeAPICaller) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	f.operation = operation
	f.request = request
	return f.response, nil
}

func (f *fakeAPICaller) CallWithArgs(_ context.Context, operation string, request map[string]any, passthrough []string) (map[string]any, error) {
	f.operation = operation
	f.request = request
	f.passthrough = append([]string(nil), passthrough...)
	return f.response, nil
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

func errorObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	errObj, _ := decodeObject(t, raw)["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stdout.error missing: %s", raw)
	}
	return errObj
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

func errorSuggestion(t *testing.T, raw string) string {
	t.Helper()
	errObj, _ := decodeObject(t, raw)["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stdout.error missing: %s", raw)
	}
	suggestion, _ := errObj["suggestion"].(string)
	return suggestion
}

func TestNewGlobalOptionsIgnoresResourceLocalProfileFlag(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	raw := `{"profiles":[{"name":"Default","language":"zh-CN","output_format":"text"}]}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	aliyunPath := filepath.Join(t.TempDir(), "missing-aliyun-config.json")
	getenv := func(name string) string {
		switch name {
		case "ECCTL_CONFIG_PATH":
			return configPath
		case "ECCTL_ALIYUN_CONFIG_PATH":
			return aliyunPath
		}
		return ""
	}

	local := newGlobalOptions([]string{"ack", "create", "--profile", "Default"}, getenv)
	if local.lang != "" || local.output != "json" {
		t.Fatalf("resource-local --profile changed global defaults: lang=%q output=%q", local.lang, local.output)
	}

	global := newGlobalOptions([]string{"--profile", "Default", "ack", "create"}, getenv)
	if global.lang != "zh-CN" || global.output != "text" {
		t.Fatalf("global --profile was not applied: lang=%q output=%q", global.lang, global.output)
	}
}

func TestSchemaCommands(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "schema", "--list", "vpc")
	if code != 0 {
		t.Fatalf("schema --list vpc exit %d stderr=%s", code, stderr)
	}
	if len(stdout) > 2048 {
		t.Fatalf("schema --list vpc is %d bytes, want <= 2048", len(stdout))
	}
	resources, _ := decodeObject(t, stdout)["resources"].([]any)
	if len(resources) == 0 {
		t.Fatalf("schema --list vpc returned no resources: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "vpc.vpc.create")
	if code != 0 {
		t.Fatalf("schema vpc.vpc.create exit %d stderr=%s", code, stderr)
	}
	if len(stdout) > 3072 {
		t.Fatalf("create schema is %d bytes, want <= 3072", len(stdout))
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	if len(params) == 0 || len(params) > 30 {
		t.Fatalf("unexpected params count %d: %s", len(params), stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "--list", "ecs")
	if code != 0 {
		t.Fatalf("schema --list ecs exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	// ECS carries the largest resource catalog; the product list grows with it.
	// Per-action schemas (checked above) stay under 2048 — that is the budget
	// that bounds an agent loading a single tool.
	if len(stdout) > 8192 {
		t.Fatalf("schema --list ecs is %d bytes, want <= 8192", len(stdout))
	}
	foundInstance := false
	ecsSurface := decodeObject(t, stdout)
	ecsResources, _ := ecsSurface["resources"].([]any)
	for _, item := range ecsResources {
		resource, _ := item.(map[string]any)
		if resource["name"] == "instance" {
			foundInstance = true
			break
		}
	}
	if !foundInstance {
		t.Fatalf("schema --list ecs missing instance resource: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ecs.instance.create")
	if code != 0 {
		t.Fatalf("schema ecs.instance.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(stdout) > 2048 {
		t.Fatalf("ecs instance create schema is %d bytes, want <= 2048", len(stdout))
	}
	params, _ = decodeObject(t, stdout)["params"].(map[string]any)
	for _, name := range []string{"type", "image", "sg", "vswitch"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("ecs instance create schema missing %s: %s", name, stdout)
		}
	}
}

func TestSchemaBriefIsDefaultAndFullPreservesAdvancedParams(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.instance.create")
	if code != 0 {
		t.Fatalf("schema ecs.instance.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	explicitBrief, explicitBriefStderr, explicitBriefCode := runCLI("--lang", "en", "schema", "ecs.instance.create", "--brief")
	if explicitBriefCode != 0 {
		t.Fatalf("schema ecs.instance.create --brief exit %d stderr=%s stdout=%s", explicitBriefCode, explicitBriefStderr, explicitBrief)
	}
	if explicitBrief != stdout {
		t.Fatalf("--brief output differs from default\nbrief:   %s\ndefault: %s", explicitBrief, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	if _, ok := params["type"]; !ok {
		t.Fatalf("brief schema missing common param type: %s", stdout)
	}
	if _, ok := params["affinity"]; ok {
		t.Fatalf("brief schema should omit advanced param affinity: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ecs.instance.create", "--full")
	if code != 0 {
		t.Fatalf("schema ecs.instance.create --full exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ = decodeObject(t, stdout)["params"].(map[string]any)
	if _, ok := params["affinity"]; !ok {
		t.Fatalf("full schema missing advanced param affinity: %s", stdout)
	}
}

func TestSchemaBriefAndFullAreMutuallyExclusive(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.instance.create", "--brief", "--full")
	if code == 0 {
		t.Fatalf("schema --brief --full should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidParameter" {
		t.Fatalf("error.code = %q, want InvalidParameter; stdout=%s", got, stdout)
	}
	errObj := errorObject(t, stdout)
	if errObj["field"] != "brief" {
		t.Fatalf("error.field = %#v, want brief; stdout=%s", errObj["field"], stdout)
	}
}

func TestSchemaFullHonorsTextAndAgentEnvelopeOutput(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--output", "text", "schema", "ecs.instance.create", "--full")
	if code != 0 {
		t.Fatalf("schema --full --output text exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "affinity:") || !strings.Contains(stdout, "data-disk:") {
		t.Fatalf("schema --full text missing advanced params:\n%s", stdout)
	}
	if strings.Contains(stdout, `"schema_version"`) {
		t.Fatalf("schema --full text should not be JSON:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "--agent-envelope", "schema", "ecs.instance.create", "--full")
	if code != 0 {
		t.Fatalf("schema --full --agent-envelope exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	envelope := decodeObject(t, stdout)
	if envelope["schema_version"] != "ecctl.agent.v1" || envelope["ok"] != true {
		t.Fatalf("agent envelope metadata missing: %s", stdout)
	}
	result, _ := envelope["result"].(map[string]any)
	params, _ := result["params"].(map[string]any)
	if _, ok := params["affinity"]; !ok {
		t.Fatalf("schema --full agent envelope missing advanced param: %s", stdout)
	}
}

func TestSchemaOutputFollowsDisplayModeRendering(t *testing.T) {
	value := map[string]any{"products": []any{map[string]any{"name": "ecs"}}}

	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	var human bytes.Buffer
	if err := writeSchemaOutputMode(&globalOptions{}, &human, value, false); err != nil {
		t.Fatalf("writeSchemaOutputMode(human-display): %v", err)
	}
	pretty := human.String()
	if !strings.Contains(pretty, "\n  ") {
		t.Fatalf("human schema output should be indented, got %q", pretty)
	}
	if !strings.Contains(pretty, "\x1b[") {
		t.Fatalf("human schema output should be highlighted, got %q", pretty)
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "AI")
	t.Setenv("AGENT_FIRST", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var agent bytes.Buffer
	if err := writeSchemaOutputMode(&globalOptions{}, &agent, value, true); err != nil {
		t.Fatalf("writeSchemaOutputMode(ai-display): %v", err)
	}
	compact := agent.String()
	if !strings.Contains(compact, `"name":"ecs"`) {
		t.Fatalf("AI display schema output should be compact JSON, got %q", compact)
	}
	if strings.Contains(strings.TrimRight(compact, "\n"), "\n") {
		t.Fatalf("AI display schema output should be a single line, got %q", compact)
	}
	if strings.Contains(compact, "\x1b[") {
		t.Fatalf("AI display schema output should not be highlighted, got %q", compact)
	}

	// Both renderings must decode to the same structure.
	var fromAgent any
	if err := json.Unmarshal(agent.Bytes(), &fromAgent); err != nil {
		t.Fatalf("agent output is not valid JSON: %v (%q)", err, compact)
	}
	var plainPretty any
	if err := json.Unmarshal([]byte(stripANSI(pretty)), &plainPretty); err != nil {
		t.Fatalf("human output is not valid JSON after stripping ANSI: %v (%q)", err, pretty)
	}
	if !reflect.DeepEqual(fromAgent, plainPretty) {
		t.Fatalf("piped and interactive schema outputs differ structurally:\n%q\nvs\n%q", compact, pretty)
	}
}

func TestResourceActionHelpShowsBriefFlagsAndSchemaHint(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"--type string",
		"--image string",
		"--sg string",
		"--vswitch string",
		"ecctl schema ecs.instance.create",
		"ecctl schema ecs.instance.create --full",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %q:\n%s", want, stdout)
		}
	}
	for _, hidden := range []string{"--affinity", "--auto-renew-period", "--network-interface"} {
		if strings.Contains(stdout, hidden) {
			t.Fatalf("create help should hide advanced flag %q:\n%s", hidden, stdout)
		}
	}
}

func TestHelpCommandLinesFollowDisplayModeRendering(t *testing.T) {
	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "\x1b[") {
		t.Fatalf("Human display should highlight command lines in help: %q", stdout)
	}
	plain := stripANSI(stdout)
	for _, want := range []string{
		"ecctl ecs instance create [flags]",
		"ecctl schema ecs.instance.create --full",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("highlighted help should preserve %q after stripping ANSI:\n%s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("--no-color", "--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("--no-color ecs instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("--no-color should suppress help ANSI escapes: %q", stdout)
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "AI")
	t.Setenv("AGENT_FIRST", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	stdout, stderr, code = runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("AI display ecs instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("AI display help should not be highlighted: %q", stdout)
	}
}

func TestHelpCommandLinesFollowAutoDisplayModeTTY(t *testing.T) {
	t.Setenv("ECCTL_DISPLAY_MODE", "")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	setWriterIsTerminalForTest(t, false)
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("auto-display non-TTY help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("default auto display mode should not highlight non-TTY help: %q", stdout)
	}

	setWriterIsTerminalForTest(t, true)
	stdout, stderr, code = runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("auto-display TTY help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "\x1b[") {
		t.Fatalf("default auto display mode should highlight TTY help: %q", stdout)
	}
}

func TestHelpAcceptsDottedActionTopic(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "help", "ecs.instance.create")
	if code != 0 {
		t.Fatalf("help ecs.instance.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Create instance",
		"ecctl ecs instance create",
		"ecctl schema ecs.instance.create",
		"ecctl schema ecs.instance.create --full",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dotted help missing %q:\n%s", want, stdout)
		}
	}
}

func TestHelpAcceptsDottedDefaultResourceAndAliasTopics(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
	}{
		{name: "vpc.create", want: "ecctl schema vpc.vpc.create"},
		{name: "vpc.vsw.create", want: "ecctl schema vpc.vswitch.create"},
	} {
		stdout, stderr, code := runCLI("--lang", "en", "help", tt.name)
		if code != 0 {
			t.Fatalf("help %s exit %d stderr=%s stdout=%s", tt.name, code, stderr, stdout)
		}
		if !strings.Contains(stdout, tt.want) {
			t.Fatalf("help %s missing %q:\n%s", tt.name, tt.want, stdout)
		}
	}
}

func TestSchemaListSubcommandMatchesListFlag(t *testing.T) {
	wantStdout, wantStderr, wantCode := runCLI("--lang", "en", "schema", "--list", "vpc")
	stdout, stderr, code := runCLI("--lang", "en", "schema", "list", "vpc", "--output", "json")
	if wantCode != 0 {
		t.Fatalf("schema --list vpc exit %d stderr=%s stdout=%s", wantCode, wantStderr, wantStdout)
	}
	if code != 0 {
		t.Fatalf("schema list vpc exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("schema list vpc stderr = %q", stderr)
	}
	if stdout != wantStdout {
		t.Fatalf("schema list vpc output differs\nwant: %s\ngot:  %s", wantStdout, stdout)
	}
}

func TestSchemaProductShorthandMatchesListFlag(t *testing.T) {
	wantStdout, wantStderr, wantCode := runCLI("--lang", "en", "schema", "--list", "vpc")
	if wantCode != 0 {
		t.Fatalf("schema --list vpc exit %d stderr=%s stdout=%s", wantCode, wantStderr, wantStdout)
	}

	for _, args := range [][]string{
		{"--lang", "en", "schema", "vpc"},
		{"--lang", "en", "schema", "--product", "vpc"},
	} {
		stdout, stderr, code := runCLI(args...)
		if code != 0 {
			t.Fatalf("%v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
		}
		if stderr != "" {
			t.Fatalf("%v stderr = %q", args, stderr)
		}
		if stdout != wantStdout {
			t.Fatalf("%v output differs\nwant: %s\ngot:  %s", args, wantStdout, stdout)
		}
	}
}

func TestSchemaResourceLevelSupportsDottedAndSplitForms(t *testing.T) {
	wantStdout, wantStderr, wantCode := runCLI("--lang", "en", "schema", "ecs.instance")
	if wantCode != 0 {
		t.Fatalf("schema ecs.instance exit %d stderr=%s stdout=%s", wantCode, wantStderr, wantStdout)
	}
	want := decodeObject(t, wantStdout)
	if want["product"] != "ecs" || want["name"] != "instance" {
		t.Fatalf("schema ecs.instance = %#v, want ecs instance resource", want)
	}
	if _, ok := want["params"]; ok {
		t.Fatalf("resource-level schema should not expand action params: %s", wantStdout)
	}
	actions, _ := want["actions"].([]any)
	for _, action := range []string{"list", "get", "create", "update", "delete"} {
		if !containsStringValue(actions, action) {
			t.Fatalf("schema ecs.instance missing action %q: %s", action, wantStdout)
		}
	}

	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs", "instance")
	if code != 0 {
		t.Fatalf("schema ecs instance exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("schema ecs instance stderr = %q", stderr)
	}
	if stdout != wantStdout {
		t.Fatalf("split resource schema differs\nwant: %s\ngot:  %s", wantStdout, stdout)
	}
}

func TestSchemaActionLevelSupportsDottedAndSplitForms(t *testing.T) {
	wantStdout, wantStderr, wantCode := runCLI("--lang", "en", "schema", "ecs.instance.list")
	if wantCode != 0 {
		t.Fatalf("schema ecs.instance.list exit %d stderr=%s stdout=%s", wantCode, wantStderr, wantStdout)
	}
	want := decodeObject(t, wantStdout)
	if want["command"] != "ecs.instance.list" || want["kind"] != "read" {
		t.Fatalf("schema ecs.instance.list = %#v, want read action schema", want)
	}
	if _, ok := want["params"]; !ok {
		t.Fatalf("action-level schema should include params: %s", wantStdout)
	}

	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs", "instance", "list")
	if code != 0 {
		t.Fatalf("schema ecs instance list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("schema ecs instance list stderr = %q", stderr)
	}
	if stdout != wantStdout {
		t.Fatalf("split action schema differs\nwant: %s\ngot:  %s", wantStdout, stdout)
	}
}

func TestSchemaDefaultsToProductList(t *testing.T) {
	wantStdout, wantStderr, wantCode := runCLI("--lang", "en", "schema", "--list")
	stdout, stderr, code := runCLI("--lang", "en", "schema")
	if wantCode != 0 {
		t.Fatalf("schema --list exit %d stderr=%s stdout=%s", wantCode, wantStderr, wantStdout)
	}
	if code != 0 {
		t.Fatalf("schema exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("schema stderr = %q", stderr)
	}
	if stdout != wantStdout {
		t.Fatalf("schema output differs\nwant: %s\ngot:  %s", wantStdout, stdout)
	}
}

func TestCapabilitiesCommandDescribesAgentSurface(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "capabilities", "--output", "json")
	if code != 0 {
		t.Fatalf("capabilities exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("capabilities stderr = %q", stderr)
	}
	capabilities := decodeObject(t, stdout)
	if capabilities["cli"] != "ecctl" {
		t.Fatalf("capabilities cli = %#v; stdout=%s", capabilities["cli"], stdout)
	}
	if capabilities["schema_version"] != float64(1) {
		t.Fatalf("capabilities schema_version = %#v; stdout=%s", capabilities["schema_version"], stdout)
	}
	if !containsStringValue(capabilities["output_modes"], "json") || !containsStringValue(capabilities["output_modes"], "text") {
		t.Fatalf("capabilities output_modes missing json/text: %s", stdout)
	}
	schemaInfo, _ := capabilities["schema"].(map[string]any)
	if schemaInfo == nil || schemaInfo["supported"] != true {
		t.Fatalf("capabilities schema support missing: %s", stdout)
	}
	if !containsStringValue(schemaInfo["list_commands"], "ecctl schema --list [product]") {
		t.Fatalf("capabilities schema list commands missing --list form: %s", stdout)
	}
	if !containsStringValue(schemaInfo["list_commands"], "ecctl schema list [product]") {
		t.Fatalf("capabilities schema list commands missing subcommand form: %s", stdout)
	}
	if schemaInfo["get_command"] != "ecctl schema <product>[.<parent>].<resource>[.<action>] [--brief|--full]" {
		t.Fatalf("capabilities schema get command = %#v; stdout=%s", schemaInfo["get_command"], stdout)
	}
	errorsInfo, _ := capabilities["errors"].(map[string]any)
	if errorsInfo == nil || errorsInfo["structured"] != true || errorsInfo["stream"] != "stdout" {
		t.Fatalf("capabilities error contract missing: %s", stdout)
	}
	if !containsStringValue(errorsInfo["fields"], "code") || !containsStringValue(errorsInfo["fields"], "retryable") {
		t.Fatalf("capabilities error fields missing: %s", stdout)
	}
	if !capabilitiesContainResourceAction(capabilities["products"], "vpc", "vpc", "create") {
		t.Fatalf("capabilities product/resource/action overview missing vpc.vpc.create: %s", stdout)
	}
}

// helpListsCommand reports whether name is listed as a command entry in help
// output — i.e. the first token on an indented listing line — rather than
// merely appearing inside some other command's description prose. Hidden
// commands must not be listed, but the same letters may legitimately occur in a
// description (e.g. "addons" in the ACK summary), so a plain substring check
// would false-positive.
func helpListsCommand(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == line {
			continue // command listings are indented; skip flush-left prose
		}
		rest, ok := strings.CutPrefix(trimmed, name)
		if !ok {
			continue
		}
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			return true
		}
	}
	return false
}

func TestOpenSourceCommandSurfaceHidesNonPublicResourcesOnlyInCLI(t *testing.T) {
	for _, tt := range []struct {
		name   string
		args   []string
		hidden []string
	}{
		{
			name:   "ack help hides non-public resources",
			args:   []string{"--lang", "en", "ack", "--help"},
			hidden: []string{"addon", "diagnosis", "inspect"},
		},
		{
			name:   "lingjun help hides non-public resources",
			args:   []string{"--lang", "en", "lingjun", "--help"},
			hidden: []string{"eni", "node", "node-group", "vsc"},
		},
		{
			name:   "root help hides undocumented products",
			args:   []string{"--lang", "en", "--help"},
			hidden: []string{"rg", "tag"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%v exit %d stderr=%s stdout=%s", tt.args, code, stderr, stdout)
			}
			for _, hidden := range tt.hidden {
				if helpListsCommand(stdout, hidden) {
					t.Fatalf("%v should hide %q:\n%s", tt.args, hidden, stdout)
				}
			}
		})
	}

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "ack default cluster create remains public", args: []string{"--lang", "en", "ack", "create", "--help"}, want: "Create ACK cluster"},
		{name: "ack cluster alias remains public", args: []string{"--lang", "en", "ack", "cluster", "create", "--help"}, want: "Create ACK cluster"},
		{name: "ack nodepool remains public", args: []string{"--lang", "en", "ack", "nodepool", "--help"}, want: "nodepool"},
		{name: "lingjun cluster remains public", args: []string{"--lang", "en", "lingjun", "cluster", "--help"}, want: "cluster"},
		{name: "lingjun VPD becomes public", args: []string{"--lang", "en", "lingjun", "vpd", "--help"}, want: "VPD"},
		{name: "ecs remains public", args: []string{"--lang", "en", "ecs", "instance", "--help"}, want: "instance"},
		{name: "vpc remains public", args: []string{"--lang", "en", "vpc", "vswitch", "--help"}, want: "vswitch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code != 0 {
				t.Fatalf("%v exit %d stderr=%s stdout=%s", tt.args, code, stderr, stdout)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("%v missing %q:\n%s", tt.args, tt.want, stdout)
			}
		})
	}

	for _, args := range [][]string{
		{"--lang", "en", "ack", "addon", "--help"},
		{"--lang", "en", "lingjun", "eni", "--help"},
		{"--lang", "en", "lingjun", "node", "--help"},
		{"--lang", "en", "lingjun", "node-group", "--help"},
		{"--lang", "en", "lingjun", "ng", "--help"},
		{"--lang", "en", "rg", "--help"},
		{"--lang", "en", "tag", "--help"},
	} {
		stdout, stderr, code := runCLI(args...)
		if code == 0 {
			t.Fatalf("%v should be hidden from CLI; stdout=%s stderr=%s", args, stdout, stderr)
		}
	}

	stdout, stderr, code := runCLI("--lang", "en", "schema", "ack.addon")
	if code == 0 {
		t.Fatalf("schema ack.addon should be hidden from CLI; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "UnknownSchema" {
		t.Fatalf("schema ack.addon error.code = %q, want UnknownSchema; stdout=%s", got, stdout)
	}
	if surface, ok := schema.ResourceForLanguage("ack", "addon", "en"); !ok || surface.Name != "addon" {
		t.Fatalf("pkg/schema should still expose ack.addon, got %#v ok=%v", surface, ok)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "--list", "lingjun")
	if code != 0 {
		t.Fatalf("schema --list lingjun exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	resources, _ := decodeObject(t, stdout)["resources"].([]any)
	var names []string
	for _, item := range resources {
		resource, _ := item.(map[string]any)
		name, _ := resource["name"].(string)
		names = append(names, name)
	}
	if !reflect.DeepEqual(names, []string{"cluster", "vpd"}) {
		t.Fatalf("public Lingjun schema resources = %#v, want [cluster vpd]; stdout=%s", names, stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "capabilities", "--output", "json")
	if code != 0 {
		t.Fatalf("capabilities exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	products, _ := decodeObject(t, stdout)["products"].([]any)
	names = nil
	for _, item := range products {
		product, _ := item.(map[string]any)
		if product["product"] != "lingjun" {
			continue
		}
		resourceItems, _ := product["resources"].([]any)
		for _, resourceItem := range resourceItems {
			resource, _ := resourceItem.(map[string]any)
			name, _ := resource["name"].(string)
			names = append(names, name)
		}
	}
	if !reflect.DeepEqual(names, []string{"cluster", "vpd"}) {
		t.Fatalf("public Lingjun capabilities resources = %#v, want [cluster vpd]; stdout=%s", names, stdout)
	}

	for _, args := range [][]string{
		{"--lang", "en", "lingjun", "node", "--help"},
		{"--lang", "en", "lingjun", "node-group", "--help"},
		{"--lang", "en", "lingjun", "ng", "--help"},
	} {
		stdout, stderr, code = runCLIWithFullSurface(args...)
		if code != 0 {
			t.Fatalf("full surface %v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
		}
	}
}

func TestUndocumentedGovernanceProductsRemainHidden(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "schema", "--list")
	if code != 0 {
		t.Fatalf("schema --list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	products, _ := decodeObject(t, stdout)["products"].([]any)
	for _, item := range products {
		product, _ := item.(map[string]any)
		if product["name"] == "rg" || product["name"] == "tag" {
			t.Fatalf("undocumented product leaked into public schema list: %s", stdout)
		}
	}

	for _, product := range []string{"rg", "tag"} {
		stdout, stderr, code = runCLI("--lang", "en", product, "--help")
		if code == 0 {
			t.Fatalf("%s must remain hidden; stderr=%s stdout=%s", product, stderr, stdout)
		}
	}
}

func TestFullSurfaceSchemaUsesCanonicalNestedResourcePath(t *testing.T) {
	for _, args := range [][]string{
		{"--lang", "en", "schema", "rg.policy.version.list", "--full"},
		{"--lang", "en", "schema", "rg", "policy", "version", "list", "--full"},
	} {
		stdout, stderr, code := runCLIWithFullSurface(args...)
		if code != 0 {
			t.Fatalf("%v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
		}
		out := decodeObject(t, stdout)
		if out["command"] != "rg.policy.version.list" || out["cli"] != "ecctl rg policy version list" {
			t.Fatalf("%v returned wrong nested schema: %s", args, stdout)
		}
	}

	stdout, stderr, code := runCLIWithFullSurface("--lang", "en", "schema", "rg.version.list", "--full")
	if code == 0 || errorCode(t, stdout) != "UnknownSchema" {
		t.Fatalf("flattened nested schema must be rejected; exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

func TestCapabilitiesHelpPointsToCommandFlagSchemas(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "capabilities", "--help")
	if code != 0 {
		t.Fatalf("capabilities --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"查看具体命令支持的参数",
		"ecctl schema <product>.<resource>.<action>",
		"全局参数:",
		"--output string",
		"--lang string",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("capabilities help missing %q:\n%s", want, stdout)
		}
	}
}

func TestCallHelpDocumentsDiscoveryWithoutListingProducts(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--help")
	if code != 0 {
		t.Fatalf("call help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	shortStdout, shortStderr, shortCode := runCLI("--lang", "en", "call", "-h")
	if shortCode != 0 {
		t.Fatalf("call -h exit %d stderr=%s stdout=%s", shortCode, shortStderr, shortStdout)
	}
	if !strings.Contains(shortStdout, "Call Alibaba Cloud OpenAPI operations") {
		t.Fatalf("call -h did not show ecctl call help:\n%s", shortStdout)
	}
	for _, want := range []string{
		"Call Alibaba Cloud OpenAPI operations",
		"ecctl call --list [--filter <keyword>] [--limit <n>]",
		"ecctl call --list <product> [--filter <keyword>] [--limit <n>]",
		"ecctl call <product> <operation> [OpenAPI parameters] [flags]",
		"OpenAPI parameters may be passed as --Parameter value or --Parameter=value.",
		"ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10",
		"--request string",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("call help missing %q:\n%s", want, stdout)
		}
	}
	for _, forbidden := range []string{"Supported Products:", "api-call"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("call help should not include %q:\n%s", forbidden, stdout)
		}
	}
}

func TestCallListProductsSupportsFilterLimitAndCount(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "--filter", "ecs", "--limit", "1")
	if code != 0 {
		t.Fatalf("call --list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --list stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1; stdout=%s", out["count"], stdout)
	}
	products, _ := out["products"].([]any)
	if len(products) != 1 {
		t.Fatalf("products len = %d, want 1; stdout=%s", len(products), stdout)
	}
	product, _ := products[0].(map[string]any)
	name, _ := product["name"].(string)
	if !strings.Contains(strings.ToLower(name), "ecs") {
		t.Fatalf("filtered product name = %q, want containing ecs; stdout=%s", name, stdout)
	}
	if _, ok := product["description"].(string); !ok {
		t.Fatalf("product description missing: %#v", product)
	}
}

func TestCallListProductsIncludesMetadata(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "--filter", "cloud", "--limit", "1")
	if code != 0 {
		t.Fatalf("call --list metadata exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --list metadata stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1; stdout=%s", out["count"], stdout)
	}
	total, _ := out["total"].(float64)
	if total <= 1 {
		t.Fatalf("total = %#v, want > 1 after filtering before limit; stdout=%s", out["total"], stdout)
	}
	if out["truncated"] != true {
		t.Fatalf("truncated = %#v, want true; stdout=%s", out["truncated"], stdout)
	}
	if out["filter"] != "cloud" || out["limit"] != float64(1) {
		t.Fatalf("filter/limit metadata = %#v/%#v; stdout=%s", out["filter"], out["limit"], stdout)
	}
}

func TestCallListProductsOmitsProductsWithoutCallableAPIs(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "--filter", "xtrace")
	if code != 0 {
		t.Fatalf("call --list --filter xtrace exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["count"] != float64(0) || out["total"] != float64(0) {
		t.Fatalf("xtrace should be omitted from product list: %s", stdout)
	}
	if products, _ := out["products"].([]any); len(products) != 0 {
		t.Fatalf("xtrace product leaked: %#v; stdout=%s", products, stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "call", "xtrace", "--list")
	if code == 0 {
		t.Fatalf("call xtrace --list should fail for products without callable APIs; stderr=%s stdout=%s", stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownProduct" {
		t.Fatalf("call xtrace --list error = %s, want UnknownProduct; stdout=%s", got, stdout)
	}
}

func TestCallListProductAPIsSupportsFilterLimitCountAndDeprecatedFilter(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "ecs", "--filter", "DescribeInstances", "--limit", "1")
	if code != 0 {
		t.Fatalf("call --list ecs exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --list ecs stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "ecs" || out["count"] != float64(1) {
		t.Fatalf("api list metadata = %#v; stdout=%s", out, stdout)
	}
	apis, _ := out["apis"].([]any)
	if len(apis) != 1 {
		t.Fatalf("apis len = %d, want 1; stdout=%s", len(apis), stdout)
	}
	api, _ := apis[0].(map[string]any)
	if api["name"] != "DescribeInstances" {
		t.Fatalf("api name = %#v, want DescribeInstances; stdout=%s", api["name"], stdout)
	}
	if _, ok := api["summary"].(string); !ok {
		t.Fatalf("api summary missing: %#v", api)
	}

	stdout, stderr, code = runCLI("--lang", "en", "call", "--list", "ddoscoo", "--filter", "CreateWebCCRule", "--limit", "10")
	if code != 0 {
		t.Fatalf("call --list ddoscoo deprecated filter exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["count"] != float64(0) {
		t.Fatalf("deprecated API should be omitted, stdout=%s", stdout)
	}
	apis, _ = out["apis"].([]any)
	if len(apis) != 0 {
		t.Fatalf("deprecated API leaked in apis: %#v; stdout=%s", apis, stdout)
	}
}

func TestCallListProductAPIsIncludesMetadata(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "ecs", "--filter", "Describe", "--limit", "1")
	if code != 0 {
		t.Fatalf("call --list ecs metadata exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --list ecs metadata stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "ecs" || out["count"] != float64(1) {
		t.Fatalf("api list metadata = %#v; stdout=%s", out, stdout)
	}
	total, _ := out["total"].(float64)
	if total <= 1 {
		t.Fatalf("total = %#v, want > 1 after filtering before limit; stdout=%s", out["total"], stdout)
	}
	if out["truncated"] != true {
		t.Fatalf("truncated = %#v, want true; stdout=%s", out["truncated"], stdout)
	}
	if out["filter"] != "Describe" || out["limit"] != float64(1) {
		t.Fatalf("filter/limit metadata = %#v/%#v; stdout=%s", out["filter"], out["limit"], stdout)
	}
}

func TestCallListProductsMatchesDomainAliases(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "--filter", "ack", "--limit", "10")
	if code != 0 {
		t.Fatalf("call --list --filter ack exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if product := findCallProduct(out["products"], "cs"); product == nil || !containsStringValue(product["aliases"], "ack") {
		t.Fatalf("ack filter should discover cs with ack alias: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "call", "--list", "--filter", "lingjun", "--limit", "10")
	if code != 0 {
		t.Fatalf("call --list --filter lingjun exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	for _, productName := range []string{"eflo", "eflo-controller", "eflo-cnp"} {
		product := findCallProduct(out["products"], productName)
		if product == nil || !containsStringValue(product["aliases"], "lingjun") {
			t.Fatalf("lingjun filter should discover %s with lingjun alias: %s", productName, stdout)
		}
	}
}

func TestCallListProductAliasListsCanonicalAPIs(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "ack", "--filter", "Cluster", "--limit", "1")
	if code != 0 {
		t.Fatalf("call --list ack exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "ack" || out["canonical_product"] != "cs" {
		t.Fatalf("ack alias metadata = %#v; stdout=%s", out, stdout)
	}
	if out["count"] != float64(1) {
		t.Fatalf("ack alias count = %#v; stdout=%s", out["count"], stdout)
	}
}

func TestCallListProductAliasGroupListsProductQualifiedAPIs(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "lingjun", "--filter", "ListVpds", "--limit", "5")
	if code != 0 {
		t.Fatalf("call --list lingjun exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "lingjun" {
		t.Fatalf("lingjun alias product = %#v; stdout=%s", out["product"], stdout)
	}
	apis, _ := out["apis"].([]any)
	if len(apis) != 1 {
		t.Fatalf("lingjun alias apis = %#v; stdout=%s", apis, stdout)
	}
	api, _ := apis[0].(map[string]any)
	if api["name"] != "ListVpds" || api["product"] != "eflo" {
		t.Fatalf("lingjun alias api = %#v; stdout=%s", api, stdout)
	}
}

func TestCallListRejectsInvalidLimitWithExistingErrorEnvelope(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "ecs", "--limit", "0")
	if code == 0 {
		t.Fatalf("invalid limit should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("invalid limit stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "InvalidParameter" || errObj["kind"] != "client" || errObj["retryable"] != false {
		t.Fatalf("invalid limit error = %#v; stdout=%s", errObj, stdout)
	}
	if errObj["field"] != "limit" {
		t.Fatalf("invalid limit field = %#v; stdout=%s", errObj["field"], stdout)
	}
}

func TestCallParseErrorsStayJSONWhenOutputTextRequested(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--output", "text", "call", "--bogus")
	if code == 0 {
		t.Fatalf("unknown call flag should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("unknown call flag stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownCommand" || errObj["kind"] != "client" || errObj["retryable"] != false {
		t.Fatalf("unknown call flag error = %#v; stdout=%s", errObj, stdout)
	}
}

func TestCallListUnknownProductUsesExistingErrorEnvelope(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--list", "not-a-product")
	if code == 0 {
		t.Fatalf("unknown product should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("unknown product stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownProduct" || errObj["kind"] != "client" || errObj["retryable"] != false {
		t.Fatalf("unknown product error = %#v; stdout=%s", errObj, stdout)
	}
	if !strings.Contains(fmt.Sprint(errObj["suggestion"]), "ecctl call --list") {
		t.Fatalf("unknown product suggestion missing call --list: %#v", errObj)
	}
}

func TestCallCommandRejectsUnknownProductOrOperationBeforeCallingAPI(t *testing.T) {
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		t.Fatal("factory should not be called for unknown product or operation")
		return nil, nil
	}
	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "not-a-product", "DescribeInstances",
		"--request", `{}`,
	)
	if code == 0 {
		t.Fatalf("unknown product should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("unknown product stderr = %q", stderr)
	}
	if got := errorCode(t, stdout); got != "UnknownProduct" {
		t.Fatalf("unknown product code = %s, want UnknownProduct; stdout=%s", got, stdout)
	}

	stdout, stderr, code = runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "NotAnOperation",
		"--request", `{}`,
	)
	if code == 0 {
		t.Fatalf("unknown operation should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("unknown operation stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownOperation" || errObj["field"] != "operation" {
		t.Fatalf("unknown operation error = %#v; stdout=%s", errObj, stdout)
	}
}

func TestCallSchemaDescribesOpenAPIOperation(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--schema", "ecs", "DescribeInstances")
	if code != 0 {
		t.Fatalf("call --schema exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --schema stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "ecs" || out["operation"] != "DescribeInstances" || out["deprecated"] != false {
		t.Fatalf("schema metadata = %#v; stdout=%s", out, stdout)
	}
	if out["title"] != "DescribeInstances" {
		t.Fatalf("schema title = %#v; stdout=%s", out["title"], stdout)
	}
	if summary, _ := out["summary"].(string); !strings.Contains(summary, "ECS instances") {
		t.Fatalf("schema summary = %q; stdout=%s", summary, stdout)
	}
	parameters, _ := out["parameters"].([]any)
	if len(parameters) == 0 {
		t.Fatalf("schema parameters missing: %s", stdout)
	}
	dryRun := findCallParameter(parameters, "DryRun")
	if dryRun == nil {
		t.Fatalf("schema parameters missing DryRun: %s", stdout)
	}
	if dryRun["type"] != "Boolean" || dryRun["required"] != false || dryRun["position"] != "Query" {
		t.Fatalf("DryRun parameter = %#v", dryRun)
	}
	if description, _ := dryRun["description"].(string); !strings.Contains(description, "dry run") {
		t.Fatalf("DryRun description = %q", description)
	}
}

func TestCallSchemaIncludesDeprecatedOperations(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--schema", "ddoscoo", "CreateWebCCRule")
	if code != 0 {
		t.Fatalf("call --schema deprecated API exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["product"] != "ddoscoo" || out["operation"] != "CreateWebCCRule" || out["deprecated"] != true {
		t.Fatalf("deprecated schema metadata = %#v; stdout=%s", out, stdout)
	}
	parameters, _ := out["parameters"].([]any)
	if findCallParameter(parameters, "Act") == nil {
		t.Fatalf("deprecated schema missing required parameter Act: %s", stdout)
	}
}

func TestCallSchemaGenerateRequestOutputsRequiredTemplate(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "call", "--schema", "ddoscoo", "CreateWebCCRule", "--generate-request")
	if code != 0 {
		t.Fatalf("call --schema --generate-request exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call --schema --generate-request stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	for _, name := range []string{"Act", "Domain", "Mode", "Name", "Uri"} {
		if out[name] != "<"+name+">" {
			t.Fatalf("template %s = %#v; stdout=%s", name, out[name], stdout)
		}
	}
	for _, name := range []string{"Count", "Interval", "Ttl"} {
		if out[name] != float64(0) {
			t.Fatalf("template %s = %#v, want 0; stdout=%s", name, out[name], stdout)
		}
	}
	if _, ok := out["ResourceGroupId"]; ok {
		t.Fatalf("optional ResourceGroupId should be omitted from generated request: %s", stdout)
	}
}

func TestCallGenerateRequestRequiresSchema(t *testing.T) {
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		t.Fatal("factory should not be called when --generate-request is used without --schema")
		return nil, nil
	}
	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"--generate-request",
	)
	if code == 0 {
		t.Fatalf("--generate-request without --schema should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("--generate-request error stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "InvalidParameter" || errObj["field"] != "generate-request" {
		t.Fatalf("--generate-request error = %#v; stdout=%s", errObj, stdout)
	}
}

func TestCallCommandRejectsDeprecatedOperationBeforeCallingAPI(t *testing.T) {
	factoryCalls := 0
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		factoryCalls++
		return &fakeAPICaller{response: map[string]any{"RequestId": "should-not-call"}}, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"--region", "cn-hangzhou",
		"call", "ddoscoo", "CreateWebCCRule",
		"--request", `{}`,
	)
	if code == 0 {
		t.Fatalf("deprecated operation should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("deprecated operation stderr = %q", stderr)
	}
	if factoryCalls != 0 {
		t.Fatalf("deprecated operation should be rejected before creating caller, factory calls=%d", factoryCalls)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "DeprecatedOperation" || errObj["kind"] != "client" || errObj["retryable"] != false {
		t.Fatalf("deprecated operation error = %#v; stdout=%s", errObj, stdout)
	}
	if !strings.Contains(fmt.Sprint(errObj["suggestion"]), "ecctl call --list ddoscoo") {
		t.Fatalf("deprecated operation suggestion missing product list command: %#v", errObj)
	}
}

func TestCallErrorsLocalizeChineseMessagesAndSuggestions(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "call", "ddoscoo", "CreateWebCCRule", "--request", `{}`)
	if code == 0 {
		t.Fatalf("deprecated operation should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	errObj := errorObject(t, stdout)
	if errObj["message"] != "call 操作已废弃" {
		t.Fatalf("deprecated message = %#v; stdout=%s", errObj["message"], stdout)
	}
	if !strings.Contains(fmt.Sprint(errObj["suggestion"]), "执行 `ecctl call --list ddoscoo`") {
		t.Fatalf("deprecated suggestion not localized: %#v", errObj)
	}

	stdout, stderr, code = runCLI("--lang", "zh-CN", "call", "not-a-product", "DescribeInstances", "--request", `{}`)
	if code == 0 {
		t.Fatalf("unknown product should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	errObj = errorObject(t, stdout)
	if errObj["message"] != "产品不受支持" {
		t.Fatalf("unknown product message = %#v; stdout=%s", errObj["message"], stdout)
	}
	if !strings.Contains(fmt.Sprint(errObj["suggestion"]), "执行 `ecctl call --list`") {
		t.Fatalf("unknown product suggestion not localized: %#v", errObj)
	}
}

func TestCallProductCompletionSuggestsMatchingProducts(t *testing.T) {
	completions, directive := completeCLI(t, "call", "ec")
	if directive != fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp) {
		t.Fatalf("completion directive = %s, want no file completion; completions=%#v", directive, completions)
	}
	if !containsCompletion(completions, "ecs") {
		t.Fatalf("product completions missing ecs: %#v", completions)
	}
	for _, completion := range completions {
		if !strings.HasPrefix(strings.ToLower(completion), "ec") {
			t.Fatalf("completion %q does not match prefix ec: %#v", completion, completions)
		}
	}
}

func TestCallOperationCompletionSuggestsMatchingNonDeprecatedAPIs(t *testing.T) {
	completions, directive := completeCLI(t, "call", "ecs", "DescribeInst")
	if directive != fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp) {
		t.Fatalf("operation completion directive = %s, want no file completion; completions=%#v", directive, completions)
	}
	if !containsCompletion(completions, "DescribeInstances") {
		t.Fatalf("operation completions missing DescribeInstances: %#v", completions)
	}
	for _, completion := range completions {
		if !strings.HasPrefix(completion, "DescribeInst") {
			t.Fatalf("operation completion %q does not match prefix DescribeInst: %#v", completion, completions)
		}
	}

	completions, _ = completeCLI(t, "call", "ddoscoo", "CreateWebC")
	if containsCompletion(completions, "CreateWebCCRule") {
		t.Fatalf("deprecated API leaked into completion: %#v", completions)
	}
}

func TestCallListCompletionSuggestsProductsOnly(t *testing.T) {
	completions, _ := completeCLI(t, "call", "--list", "ec")
	if !containsCompletion(completions, "ecs") {
		t.Fatalf("--list product completions missing ecs: %#v", completions)
	}
	for _, completion := range completions {
		if !strings.HasPrefix(strings.ToLower(completion), "ec") {
			t.Fatalf("--list completion %q does not match prefix ec: %#v", completion, completions)
		}
	}

	completions, directive := completeCLI(t, "call", "--list", "ecs", "")
	if directive != fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp) {
		t.Fatalf("--list completed product directive = %s, want no file completion; completions=%#v", directive, completions)
	}
	if len(completions) != 0 {
		t.Fatalf("--list mode must not complete operations after product, got %#v", completions)
	}
}

func TestCallCommandCallsGenericOpenAPI(t *testing.T) {
	var gotProduct, gotProfile, gotConfigPath, gotRegion string
	fake := &fakeAPICaller{response: map[string]any{
		"RequestId":  "req-1",
		"TotalCount": float64(1),
	}}
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotProfile = profileName
		gotConfigPath = configPath
		gotProduct = product
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"--profile", "prod",
		"--region", "cn-hangzhou",
		"call", "ecs", "DescribeInstances",
		"--request", `{"PageNumber":2,"DryRun":true,"Filter":{"Status":"Running"}}`,
	)
	if code != 0 {
		t.Fatalf("call exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call stderr = %q", stderr)
	}
	if gotProduct != "ecs" || gotRegion != "cn-hangzhou" {
		t.Fatalf("factory product/region = %q/%q, want ecs/cn-hangzhou", gotProduct, gotRegion)
	}
	if gotProfile != "prod" || gotConfigPath == "" {
		t.Fatalf("factory should receive profile and config path, got profile=%q configPath=%q", gotProfile, gotConfigPath)
	}
	if fake.operation != "DescribeInstances" {
		t.Fatalf("operation = %q, want DescribeInstances", fake.operation)
	}
	filter, _ := fake.request["Filter"].(map[string]any)
	if fake.request["DryRun"] != true || fake.request["PageNumber"] != float64(2) || filter["Status"] != "Running" {
		t.Fatalf("request = %#v", fake.request)
	}

	payload := decodeObject(t, stdout)
	if payload["product"] != "ecs" || payload["operation"] != "DescribeInstances" || payload["region"] != "cn-hangzhou" {
		t.Fatalf("payload metadata = %#v", payload)
	}
	response, _ := payload["response"].(map[string]any)
	if response["RequestId"] != "req-1" {
		t.Fatalf("response payload = %#v", response)
	}
	if strings.Contains(stdout, `"aliyun"`) {
		t.Fatalf("call output should not expose aliyun CLI details: %s", stdout)
	}
}

func TestCallCommandAcceptsOpenAPIParameterFlags(t *testing.T) {
	var gotRegion string
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-flags"}}
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"--request", `{"PageNumber":1,"PageSize":10}`,
		"--RegionId", "cn-hangzhou",
		"--PageSize", "20",
		"--DryRun", "true",
		"--InstanceIds", `["i-1","i-2"]`,
		"--Filter", `{"Status":"Running"}`,
		"--PortRange=-1/-1",
	)
	if code != 0 {
		t.Fatalf("call with OpenAPI flags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call with OpenAPI flags stderr = %q", stderr)
	}
	if gotRegion != "cn-hangzhou" {
		t.Fatalf("region = %q, want RegionId flag cn-hangzhou", gotRegion)
	}
	if fake.operation != "DescribeInstances" {
		t.Fatalf("operation = %q, want DescribeInstances", fake.operation)
	}
	filter, _ := fake.request["Filter"].(map[string]any)
	instanceIDs, _ := fake.request["InstanceIds"].([]any)
	if fake.request["PageNumber"] != float64(1) ||
		fake.request["PageSize"] != float64(20) ||
		fake.request["DryRun"] != true ||
		fake.request["RegionId"] != "cn-hangzhou" ||
		filter["Status"] != "Running" ||
		len(instanceIDs) != 2 ||
		instanceIDs[0] != "i-1" ||
		fake.request["PortRange"] != "-1/-1" {
		t.Fatalf("request = %#v", fake.request)
	}
}

func TestCallCommandPassesAliyunCLIFlagsThrough(t *testing.T) {
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-cli-flags"}}
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		return fake, nil
	}
	args := []string{
		"--lang", "en",
		"call",
		"--method", "POST",
		"--waiter", "expr=Instances.Instance[0].Status",
		"to=Running",
		"ecs", "DescribeInstances",
		"--request", `{"PageSize":10}`,
		"--PageSize", "20",
		"--DryRun", "true",
		"--header", "X-Test=1",
		"--output", "cols=InstanceId,Status",
		"--dryrun",
	}
	normalized := normalizeAPICallParameterFlags(args)
	if containsString(normalized, "--PageSize") || !containsString(normalized, "--api-param") {
		t.Fatalf("normalized args did not convert OpenAPI flags: %#v", normalized)
	}

	stdout, stderr, code := runCLIWithAPI(factory, args...)
	if code != 0 {
		t.Fatalf("call with aliyun CLI flags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call with aliyun CLI flags stderr = %q", stderr)
	}
	if fake.request["PageSize"] != float64(20) || fake.request["DryRun"] != true {
		t.Fatalf("request = %#v", fake.request)
	}
	wantPassthrough := []string{
		"--method", "POST",
		"--waiter", "expr=Instances.Instance[0].Status",
		"to=Running",
		"--header", "X-Test=1",
		"--output", "cols=InstanceId,Status",
		"--dryrun",
	}
	if !reflect.DeepEqual(fake.passthrough, wantPassthrough) {
		t.Fatalf("passthrough = %#v, want %#v", fake.passthrough, wantPassthrough)
	}
}

func TestCallCommandPassesInlineAliyunCLIWaiterBeforeProduct(t *testing.T) {
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-inline-waiter"}}
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call",
		"--waiter=expr=Instances.Instance[0].Status",
		"to=Running",
		"ecs", "DescribeInstances",
	)
	if code != 0 {
		t.Fatalf("call with inline waiter exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("call with inline waiter stderr = %q", stderr)
	}
	if fake.operation != "DescribeInstances" {
		t.Fatalf("operation = %q, want DescribeInstances", fake.operation)
	}
	wantPassthrough := []string{
		"--waiter", "expr=Instances.Instance[0].Status",
		"to=Running",
	}
	if !reflect.DeepEqual(fake.passthrough, wantPassthrough) {
		t.Fatalf("passthrough = %#v, want %#v", fake.passthrough, wantPassthrough)
	}
}

func TestCallCommandAliyunShortProfileFlagDoesNotInjectDefaultRegion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ecctl.json")
	rawConfig := `{"current":"default","profiles":[{"name":"default","region_id":"cn-hangzhou"},{"name":"prod","region_id":"cn-beijing"}]}`
	if err := os.WriteFile(configPath, []byte(rawConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-profile"}}
	var gotRegion string
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"-p", "prod",
	)
	if code != 0 {
		t.Fatalf("call with aliyun -p exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if gotRegion != "" {
		t.Fatalf("region = %q, want empty so aliyun -p can resolve it", gotRegion)
	}
	wantPassthrough := []string{"-p", "prod"}
	if !reflect.DeepEqual(fake.passthrough, wantPassthrough) {
		t.Fatalf("passthrough = %#v, want %#v", fake.passthrough, wantPassthrough)
	}
}

func TestCallCommandAliyunConfigPathDoesNotInjectDefaultRegion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "ecctl.json")
	rawConfig := `{"current":"default","profiles":[{"name":"default","region_id":"cn-hangzhou"}]}`
	if err := os.WriteFile(configPath, []byte(rawConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	aliyunConfigPath := filepath.Join(dir, "aliyun.json")
	if err := os.WriteFile(aliyunConfigPath, []byte(`{"current":"prod","profiles":[{"name":"prod","region_id":"cn-beijing"}]}`), 0o600); err != nil {
		t.Fatalf("write aliyun config: %v", err)
	}
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-config-path"}}
	var gotRegion string
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"--config-path", aliyunConfigPath,
	)
	if code != 0 {
		t.Fatalf("call with aliyun --config-path exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if gotRegion != "" {
		t.Fatalf("region = %q, want empty so aliyun --config-path can resolve it", gotRegion)
	}
	wantPassthrough := []string{"--config-path", aliyunConfigPath}
	if !reflect.DeepEqual(fake.passthrough, wantPassthrough) {
		t.Fatalf("passthrough = %#v, want %#v", fake.passthrough, wantPassthrough)
	}
}

func TestCallCommandRegionFlagSetsPayloadRegion(t *testing.T) {
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-region"}}
	var gotRegion string
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"--region", "cn-beijing",
	)
	if code != 0 {
		t.Fatalf("call with aliyun --region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if gotRegion != "cn-beijing" {
		t.Fatalf("region = %q, want cn-beijing", gotRegion)
	}
	payload := decodeObject(t, stdout)
	if payload["region"] != "cn-beijing" {
		t.Fatalf("payload.region = %#v, want cn-beijing; stdout=%s", payload["region"], stdout)
	}
	if len(fake.passthrough) != 0 {
		t.Fatalf("passthrough = %#v, want none for ecctl --region", fake.passthrough)
	}
}

func TestCallCommandResolvesDomainProductAliases(t *testing.T) {
	tests := []struct {
		name        string
		product     string
		operation   string
		wantProduct string
	}{
		{name: "ack", product: "ack", operation: "DescribeClustersV1", wantProduct: "cs"},
		{name: "lingjun eflo", product: "lingjun", operation: "ListVpds", wantProduct: "eflo"},
		{name: "lingjun controller", product: "lingjun", operation: "ListClusters", wantProduct: "eflo-controller"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotProduct string
			fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-alias"}}
			factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
				gotProduct = product
				return fake, nil
			}

			stdout, stderr, code := runCLIWithAPI(factory,
				"--lang", "en",
				"--region", "cn-hangzhou",
				"call", tt.product, tt.operation,
				"--request", `{}`,
			)
			if code != 0 {
				t.Fatalf("call alias exit %d stderr=%s stdout=%s", code, stderr, stdout)
			}
			if gotProduct != tt.wantProduct {
				t.Fatalf("factory product = %q, want %q", gotProduct, tt.wantProduct)
			}
			if fake.operation != tt.operation {
				t.Fatalf("operation = %q, want %q", fake.operation, tt.operation)
			}
		})
	}
}

func TestCallCommandReadsRequestFromFileAndRegionID(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(requestPath, []byte(`{"RegionId":"cn-shanghai","body":{"name":"coredns"},"ClusterId":"c-123"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var gotRegion string
	fake := &fakeAPICaller{response: map[string]any{"RequestId": "req-2"}}
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		gotRegion = region
		return fake, nil
	}

	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "cs", "InstallClusterAddons",
		"--request", "@"+requestPath,
	)
	if code != 0 {
		t.Fatalf("call @file exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if gotRegion != "cn-shanghai" {
		t.Fatalf("region = %q, want request RegionId cn-shanghai", gotRegion)
	}
	if fake.request["ClusterId"] != "c-123" {
		t.Fatalf("request = %#v", fake.request)
	}
	payload := decodeObject(t, stdout)
	if payload["region"] != "cn-shanghai" {
		t.Fatalf("payload region = %#v; stdout=%s", payload["region"], stdout)
	}
}

func TestCallCommandRejectsNonObjectRequest(t *testing.T) {
	factory := func(profileName, configPath, product, region string, getenv func(string) string) (engine.Caller, error) {
		t.Fatal("factory should not be called for invalid request")
		return nil, nil
	}
	stdout, stderr, code := runCLIWithAPI(factory,
		"--lang", "en",
		"call", "ecs", "DescribeInstances",
		"--request", `[]`,
	)
	if code == 0 {
		t.Fatalf("non-object request should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("non-object request stderr = %q", stderr)
	}
	if got := errorCode(t, stdout); got != "InvalidParameter" {
		t.Fatalf("error code = %s, want InvalidParameter; stdout=%s", got, stdout)
	}
}

func completeCLI(t *testing.T, args ...string) ([]string, string) {
	t.Helper()
	stdout, stderr, code := runCLI(append([]string{"__complete"}, args...)...)
	if code != 0 {
		t.Fatalf("__complete %v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("__complete %v stderr = %q", args, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		t.Fatalf("__complete %v returned empty stdout", args)
	}
	directive := lines[len(lines)-1]
	completions := make([]string, 0, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		value, _, _ := strings.Cut(line, "\t")
		if value != "" {
			completions = append(completions, value)
		}
	}
	return completions, directive
}

func containsCompletion(completions []string, want string) bool {
	for _, completion := range completions {
		if completion == want {
			return true
		}
	}
	return false
}

func findCallProduct(value any, name string) map[string]any {
	products, _ := value.([]any)
	for _, item := range products {
		product, _ := item.(map[string]any)
		if product["name"] == name {
			return product
		}
	}
	return nil
}

func findCallParameter(parameters []any, name string) map[string]any {
	for _, value := range parameters {
		parameter, _ := value.(map[string]any)
		if parameter["name"] == name {
			return parameter
		}
	}
	return nil
}

func containsStringValue(value any, want string) bool {
	values, _ := value.([]any)
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func capabilitiesContainResourceAction(value any, productName string, resourceName string, actionName string) bool {
	products, _ := value.([]any)
	for _, productValue := range products {
		product, _ := productValue.(map[string]any)
		if product["product"] != productName {
			continue
		}
		resources, _ := product["resources"].([]any)
		for _, resourceValue := range resources {
			resource, _ := resourceValue.(map[string]any)
			if resource["name"] == resourceName && containsStringValue(resource["actions"], actionName) {
				return true
			}
		}
	}
	return false
}

func TestSchemaPreservesDefaultValueTypes(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.instance.list")
	if code != 0 {
		t.Fatalf("schema ecs.instance.list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	limit, _ := params["limit"].(map[string]any)
	nextToken, _ := params["next-token"].(map[string]any)
	if limit["default"] != float64(100) {
		t.Fatalf("limit default = %#v, want numeric 100", limit["default"])
	}
	if nextToken["type"] != "string" {
		t.Fatalf("next_token type = %#v, want string", nextToken["type"])
	}
	if _, ok := params["page"]; ok {
		t.Fatalf("token-paginated schema must not expose page: %#v", params["page"])
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ecs.instance.list", "--output", "text")
	if code != 0 {
		t.Fatalf("schema ecs.instance.list --output text exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, forbidden := range []string{`default: "100"`, "page:"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("schema text output quoted numeric default %s:\n%s", forbidden, stdout)
		}
	}
	for _, want := range []string{"default: 100", "next-token:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("schema text output missing %s:\n%s", want, stdout)
		}
	}
}

func TestGlobalLangFlagReplacesLocaleFlag(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("--help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--lang") {
		t.Fatalf("help should include --lang: %s", stdout)
	}
	if !strings.Contains(stdout, "display language (supported: en, zh-CN)") {
		t.Fatalf("help should show supported languages: %s", stdout)
	}
	if strings.Contains(stdout, "--locale") {
		t.Fatalf("help should not include --locale: %s", stdout)
	}
	for _, want := range []string{"--json", "--no-color", "--version", "Examples:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help should include %q: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"Docs:", "Issues:"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("root help should not include %q: %s", forbidden, stdout)
		}
	}
}

func TestLangFlagLocalizesRootHelp(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN")
	if code != 0 {
		t.Fatalf("--lang zh-CN exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Agent 优先的弹性计算控制器",
		"用法:",
		"云产品命令:",
		"辅助命令:",
		"capabilities 描述机器可读的 CLI 能力",
		"completion   生成指定 shell 的自动补全脚本",
		"help         显示命令帮助",
		"参数:",
		"显示语言（支持：en, zh-CN）",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("localized help missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Available Commands:") || strings.Contains(stdout, "display language") {
		t.Fatalf("localized help should not use English help labels: %s", stdout)
	}
	for _, forbidden := range []string{"文档:", "问题反馈:"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("root help should not include %q: %s", forbidden, stdout)
		}
	}
	for _, want := range []string{"--json", "--no-color", "--version"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("localized help should include %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "(default ") {
		t.Fatalf("localized help should translate default labels: %s", stdout)
	}
}

func TestHelpFlagIgnoresOtherFlags(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "vpc", "create", "--bogus", "--help")
	if code != 0 {
		t.Fatalf("help with unknown flag exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Create a VPC") || !strings.Contains(stdout, "--cidr string") {
		t.Fatalf("help output missing command help: %s", stdout)
	}
	if strings.Contains(stdout, `"error"`) || strings.TrimSpace(stderr) != "" {
		t.Fatalf("help should not render structured error; stdout=%s stderr=%s", stdout, stderr)
	}

	stdout, stderr, code = runCLI("--lang", "en", "--output", "table", "--help")
	if code != 0 {
		t.Fatalf("root help with invalid output flag exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Agent-first Elastic Computing Controller") {
		t.Fatalf("root help missing: %s", stdout)
	}
}

func TestGlobalCompatibilityFlags(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--version")
	if code != 0 {
		t.Fatalf("--version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "ecctl") {
		t.Fatalf("--version output should name ecctl: %s", stdout)
	}

	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	writeTestConfig(t, configPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "output_format": "text"},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	stdout, stderr, code = runCLI("--json", "config", "list")
	if code != 0 {
		t.Fatalf("--json config list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("--json should force JSON output over configured text: %s", stdout)
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	stdout, stderr, code = runCLI("--output", "text", "config", "list")
	if code != 0 {
		t.Fatalf("human-display text output exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "\x1b[") {
		t.Fatalf("Human display should color text output: %q", stdout)
	}
	stdout, stderr, code = runCLI("--no-color", "--output", "text", "config", "list")
	if code != 0 {
		t.Fatalf("--no-color text output exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("--no-color should suppress ANSI escapes: %q", stdout)
	}
}

func TestRunOutputRenderingFollowsDisplayModeEnvironment(t *testing.T) {
	t.Setenv("ECCTL_DISPLAY_MODE", "")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	stdout, stderr, code := runCLI("--lang", "en", "config", "list")
	if code != 0 {
		t.Fatalf("auto-display config list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(strings.TrimRight(stdout, "\n"), "\n") {
		t.Fatalf("default auto display mode should compact JSON for non-TTY output: %q", stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("default auto display mode should not highlight non-TTY JSON: %q", stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("default auto display output should remain valid JSON: %q", stdout)
	}

	setWriterIsTerminalForTest(t, true)
	stdout, stderr, code = runCLI("--lang", "en", "config", "list")
	if code != 0 {
		t.Fatalf("auto-display TTY config list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "\n  ") {
		t.Fatalf("default auto display mode should pretty-print JSON for TTY output: %q", stdout)
	}
	if !strings.Contains(stdout, "\x1b[") {
		t.Fatalf("default auto display mode should highlight TTY JSON: %q", stdout)
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "AI")
	t.Setenv("AGENT_FIRST", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	stdout, stderr, code = runCLI("--lang", "en", "config", "list")
	if code != 0 {
		t.Fatalf("AI display config list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(strings.TrimRight(stdout, "\n"), "\n") {
		t.Fatalf("AI display should compact JSON: %q", stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("AI display should not highlight JSON: %q", stdout)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("AI display output should remain valid JSON: %q", stdout)
	}
}

func TestFormatVersionUsesInjectedVersion(t *testing.T) {
	got := formatVersion("v1.2.3", "abc123", "2026-06-15T00:00:00Z", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, true
	})
	want := "v1.2.3 (commit abc123, built 2026-06-15T00:00:00Z)"
	if got != want {
		t.Fatalf("formatVersion = %q, want %q", got, want)
	}
}

func TestFormatVersionFallsBackToBuildInfoVersion(t *testing.T) {
	got := formatVersion("dev", "", "", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, true
	})
	if got != "v0.1.0" {
		t.Fatalf("formatVersion = %q, want v0.1.0", got)
	}
}

func TestFormatVersionKeepsDevForLocalBuilds(t *testing.T) {
	got := formatVersion("dev", "", "", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
	})
	if got != "dev" {
		t.Fatalf("formatVersion = %q, want dev", got)
	}
}

func TestAgentEnvelopeWrapsSuccessfulJSONOutput(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--agent-envelope", "schema", "--list", "vpc")
	if code != 0 {
		t.Fatalf("agent envelope schema list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["ok"] != true {
		t.Fatalf("ok = %#v, want true; stdout=%s", out["ok"], stdout)
	}
	if out["schema_version"] != "ecctl.agent.v1" {
		t.Fatalf("schema_version = %#v, want ecctl.agent.v1; stdout=%s", out["schema_version"], stdout)
	}
	if out["command"] != "ecctl schema" {
		t.Fatalf("command = %#v, want ecctl schema; stdout=%s", out["command"], stdout)
	}
	result, _ := out["result"].(map[string]any)
	if result == nil {
		t.Fatalf("result missing or not object: %s", stdout)
	}
	resources, _ := result["resources"].([]any)
	if len(resources) == 0 {
		t.Fatalf("result.resources missing: %s", stdout)
	}
	if _, ok := out["error"]; ok {
		t.Fatalf("successful envelope should not include error: %s", stdout)
	}
}

func TestAgentEnvelopeWrapsErrorJSONOutput(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--agent-envelope", "schema", "missing.schema")
	if code == 0 {
		t.Fatalf("schema missing.schema should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["ok"] != false {
		t.Fatalf("ok = %#v, want false; stdout=%s", out["ok"], stdout)
	}
	if out["schema_version"] != "ecctl.agent.v1" {
		t.Fatalf("schema_version = %#v, want ecctl.agent.v1; stdout=%s", out["schema_version"], stdout)
	}
	if out["command"] != "ecctl schema" {
		t.Fatalf("command = %#v, want ecctl schema; stdout=%s", out["command"], stdout)
	}
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("error missing or not object: %s", stdout)
	}
	if errObj["code"] != "UnknownSchema" {
		t.Fatalf("error.code = %#v, want UnknownSchema; stdout=%s", errObj["code"], stdout)
	}
	if _, ok := out["result"]; ok {
		t.Fatalf("error envelope should not include result: %s", stdout)
	}
}

func TestAgentEnvelopeWrapsUnsupportedOutputModeError(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--agent-envelope", "--output", "table", "schema", "--list", "vpc")
	if code == 0 {
		t.Fatalf("unsupported output mode should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("stderr must be empty for agent envelope errors, got %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["ok"] != false {
		t.Fatalf("ok = %#v, want false; stdout=%s", out["ok"], stdout)
	}
	if out["schema_version"] != "ecctl.agent.v1" {
		t.Fatalf("schema_version = %#v, want ecctl.agent.v1; stdout=%s", out["schema_version"], stdout)
	}
	if out["command"] != "ecctl" {
		t.Fatalf("command = %#v, want ecctl for pre-dispatch validation; stdout=%s", out["command"], stdout)
	}
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("error missing or not object: %s", stdout)
	}
	if errObj["code"] != "UnsupportedOutputMode" {
		t.Fatalf("error.code = %#v, want UnsupportedOutputMode; stdout=%s", errObj["code"], stdout)
	}
	if errObj["field"] != "output" {
		t.Fatalf("error.field = %#v, want output; stdout=%s", errObj["field"], stdout)
	}
}

func TestRootHelpGroupsCommands(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("--lang en --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	cloudStart := strings.Index(stdout, "Cloud Product Commands:")
	auxiliaryStart := strings.Index(stdout, "Auxiliary Commands:")
	if cloudStart < 0 || auxiliaryStart < 0 || cloudStart > auxiliaryStart {
		t.Fatalf("root help should show cloud commands before auxiliary commands:\n%s", stdout)
	}

	cloudSection := stdout[cloudStart:auxiliaryStart]
	if !strings.Contains(cloudSection, "vpc") {
		t.Fatalf("cloud command section missing vpc:\n%s", stdout)
	}
	if !strings.Contains(cloudSection, "ecs") {
		t.Fatalf("cloud command section missing ecs:\n%s", stdout)
	}

	auxiliarySection := stdout[auxiliaryStart:]
	for _, want := range []string{"completion", "configure", "help", "schema"} {
		if !strings.Contains(auxiliarySection, want) {
			t.Fatalf("auxiliary command section missing %q:\n%s", want, stdout)
		}
	}
}

func TestLangFlagLocalizesNestedCommandHelp(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "vpc", "create", "--help")
	if code != 0 {
		t.Fatalf("nested help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"创建 VPC",
		"--name string",
		"--cidr string",
		"全局参数:",
		"--lang string",
		"显示语言（支持：en, zh-CN）",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("localized nested help missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Create a VPC") || strings.Contains(stdout, "CIDR block") {
		t.Fatalf("localized nested help should not use English text: %s", stdout)
	}
}

func TestLangFlagLocalizesCompletionHelp(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "completion", "--help")
	if code != 0 {
		t.Fatalf("completion help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"生成指定 shell 的自动补全脚本",
		"bash        生成 bash 自动补全脚本",
		"zsh         生成 zsh 自动补全脚本",
		"显示此命令的帮助",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("localized completion help missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Generate the autocompletion script") || strings.Contains(stdout, "help for completion") {
		t.Fatalf("localized completion help should not use English text: %s", stdout)
	}
}

func TestLangFlagLocalizesSchemaHelp(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "schema", "--help")
	if code != 0 {
		t.Fatalf("schema help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"查看产品命令 Schema",
		"用法:",
		"ecctl schema [--list [product] | list [product] | product[.parent].resource[.action] ...] [--brief|--full]",
		"示例:",
		"ecctl schema --list",
		"ecctl schema --list <product>",
		"ecctl schema list <product>",
		"ecctl schema <product>.<resource>.<action>",
		"ecctl schema <product> <resource> <action>",
		"ecctl schema <product>.<parent>.<resource>.<action>",
		"ecctl schema <product> <parent> <resource> <action>",
		"ecctl schema <product>.<resource>.<action> --full",
		"列出支持的产品或资源",
		"仅显示必填和常用参数",
		"显示所有 schema 可见参数",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("localized schema help missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "打印紧凑命令模式") {
		t.Fatalf("schema help should not describe schema as command mode: %s", stdout)
	}
}

func TestLangFlagLocalizesStructuredErrors(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "--region", "", "vpc", "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", code, stdout, stderr)
	}
	if got := errorMessage(t, stdout); got != "必须提供地域" {
		t.Fatalf("error.message = %q, want Chinese MissingRegion", got)
	}
	if got := errorSuggestion(t, stdout); !strings.Contains(got, "ecctl configure set region") {
		t.Fatalf("error.suggestion = %q, want config guidance", got)
	}
}

func TestLangFlagLocalizesOtherStructuredErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unsupported output", args: []string{"--lang", "zh-CN", "--output", "table", "schema"}, want: "输出模式不受支持"},
		{name: "missing parameter", args: []string{"--lang", "zh-CN", "--region", "cn-beijing", "vpc", "get"}, want: "缺少必填参数: <id>"},
		{name: "invalid ids", args: []string{"--lang", "zh-CN", "--region", "cn-beijing", "vpc", "list", "--filter", "ids=[\"vpc-123\"]"}, want: "ID 必须是逗号分隔列表，不能是 JSON 数组"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code == 0 {
				t.Fatalf("expected error exit; stdout=%s stderr=%s", stdout, stderr)
			}
			if got := errorMessage(t, stdout); got != tt.want {
				t.Fatalf("error.message = %q, want %q; stdout=%s", got, tt.want, stdout)
			}
		})
	}
}

func TestLocalizedServiceErrorPreservesCloudMessage(t *testing.T) {
	var stdout bytes.Buffer
	code := writeLocalizedError(&stdout, ecerrors.Service("CloudAPIError", "Unauthorized: source ip denied", false), "zh-CN", "json")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stdout=%s", code, stdout.String())
	}
	if got := errorMessage(t, stdout.String()); got != "Unauthorized: source ip denied" {
		t.Fatalf("error.message = %q, want cloud message", got)
	}
}

func TestCloudAPIErrorWithActionsPointsToActionDetails(t *testing.T) {
	var stdout bytes.Buffer
	err := ecerrors.Service("CloudAPIError", "Specified CIDR block overlapped with other subnets.", false,
		ecerrors.WithActionsOption(ecerrors.Action{
			ActionName: "CreateVSwitch",
			Code:       "InvalidCidrBlock.Overlapped",
			Message:    "Specified CIDR block overlapped with other subnets.",
			RequestID:  "req-cidr",
		}),
	)
	code := writeLocalizedError(&stdout, err, "zh-CN", "json")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stdout=%s", code, stdout.String())
	}
	if got := errorMessage(t, stdout.String()); got != "调用 API 报错，请查看 actions 中的具体报错" {
		t.Fatalf("error.message = %q, want action pointer; stdout=%s", got, stdout.String())
	}
	out := decodeObject(t, stdout.String())
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1; stdout=%s", len(actions), stdout.String())
	}
	action, _ := actions[0].(map[string]any)
	if action["code"] != "InvalidCidrBlock.Overlapped" ||
		action["message"] != "Specified CIDR block overlapped with other subnets." ||
		action["request_id"] != "req-cidr" {
		t.Fatalf("action = %#v", action)
	}
}

func TestLocalizedNotFoundPreservesResourceMessage(t *testing.T) {
	var stdout bytes.Buffer
	code := writeLocalizedError(&stdout, ecerrors.NotFound("NotFound", "vsw-123 not found"), "zh-CN", "json")
	if code != 4 {
		t.Fatalf("exit = %d, want 4; stdout=%s", code, stdout.String())
	}
	if got := errorMessage(t, stdout.String()); got != "vsw-123 资源不存在" {
		t.Fatalf("error.message = %q, want contextual not found message", got)
	}
}

func TestEnvironmentLanguageLocalizesStructuredErrors(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")

	stdout, stderr, code := runCLI("--region", "", "vpc", "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", code, stdout, stderr)
	}
	if got := errorMessage(t, stdout); got != "必须提供地域" {
		t.Fatalf("error.message = %q, want Chinese MissingRegion", got)
	}
}

func TestUnsupportedEnvironmentLanguageFallsBackToEnglish(t *testing.T) {
	t.Setenv("LC_ALL", "fr_FR.UTF-8")

	stdout, stderr, code := runCLI("--region", "", "vpc", "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", code, stdout, stderr)
	}
	if got := errorMessage(t, stdout); got != "region is required" {
		t.Fatalf("error.message = %q, want English fallback", got)
	}
}

func TestStructuredLocalErrors(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")
	tests := []struct {
		name string
		args []string
		exit int
		code string
	}{
		{name: "unknown command", args: []string{"vpc", "this-action-does-not-exist"}, exit: 1, code: "UnknownCommand"},
		{name: "missing region", args: []string{"--region", "", "vpc", "list"}, exit: 1, code: "MissingRegion"},
		{name: "invalid region", args: []string{"--region", "not-a-real-region", "vpc", "list"}, exit: 1, code: "InvalidRegion"},
		{name: "unsupported output", args: []string{"vpc", "list", "--output", "table"}, exit: 1, code: "UnsupportedOutputMode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code != tt.exit {
				t.Fatalf("exit = %d, want %d; stdout=%s stderr=%s", code, tt.exit, stdout, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("stderr must be empty for structured errors, got %q", stderr)
			}
			if got := errorCode(t, stdout); got != tt.code {
				t.Fatalf("error.code = %q, want %q", got, tt.code)
			}
		})
	}
}

func TestStructuredErrorsIncludeActionableSuggestions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown command", args: []string{"--lang", "en", "vpc", "this-action-does-not-exist"}, want: "ecctl --help"},
		{name: "missing region", args: []string{"--lang", "en", "--region", "", "vpc", "list"}, want: "ecctl configure set region"},
		{name: "missing parameter", args: []string{"--lang", "en", "--region", "cn-beijing", "vpc", "get"}, want: "--help"},
		{name: "unsupported output", args: []string{"--lang", "en", "vpc", "list", "--output", "table"}, want: "--json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code == 0 {
				t.Fatalf("expected error exit; stdout=%s stderr=%s", stdout, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("stderr must be empty for structured errors, got %q", stderr)
			}
			if got := errorSuggestion(t, stdout); !strings.Contains(got, tt.want) {
				t.Fatalf("error.suggestion = %q, want it to contain %q; stdout=%s", got, tt.want, stdout)
			}
		})
	}
}

func TestStructuredLocalErrorsIncludeMachineReadableContract(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantCode       string
		wantKind       string
		wantField      string
		wantAccepted   []string
		wantSuggestion string
	}{
		{
			name:           "unknown flag reports flag field",
			args:           []string{"--lang", "en", "vpc", "list", "--definitely-unknown"},
			wantCode:       "UnknownCommand",
			wantKind:       "client",
			wantField:      "--definitely-unknown",
			wantSuggestion: "ecctl --help",
		},
		{
			name:           "unsupported output reports accepted modes",
			args:           []string{"--lang", "en", "vpc", "list", "--output", "table"},
			wantCode:       "UnsupportedOutputMode",
			wantKind:       "client",
			wantField:      "output",
			wantAccepted:   []string{"json", "text"},
			wantSuggestion: "--json",
		},
		{
			name:           "unsupported filter reports accepted filters",
			args:           []string{"--lang", "en", "vpc", "list", "--region", "cn-beijing", "--filter", "cidr=10.0.0.0/8"},
			wantCode:       "InvalidFilter",
			wantKind:       "client",
			wantField:      "filter",
			wantAccepted:   []string{"default", "dhcp-options-set", "enable-ipv6", "id", "ids", "name", "owner-id", "resource-group", "status", "tag.<key>", "vpc", "vpc.id"},
			wantSuggestion: "schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code == 0 {
				t.Fatalf("expected error exit; stdout=%s stderr=%s", stdout, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("stderr must be empty for structured errors, got %q", stderr)
			}
			errObj := errorObject(t, stdout)
			if got, _ := errObj["code"].(string); got != tt.wantCode {
				t.Fatalf("error.code = %q, want %q; stdout=%s", got, tt.wantCode, stdout)
			}
			if got, _ := errObj["kind"].(string); got != tt.wantKind {
				t.Fatalf("error.kind = %q, want %q; stdout=%s", got, tt.wantKind, stdout)
			}
			if got, _ := errObj["field"].(string); got != tt.wantField {
				t.Fatalf("error.field = %q, want %q; stdout=%s", got, tt.wantField, stdout)
			}
			if got, _ := errObj["suggested_action"].(string); !strings.Contains(got, tt.wantSuggestion) {
				t.Fatalf("error.suggested_action = %q, want it to contain %q; stdout=%s", got, tt.wantSuggestion, stdout)
			}
			if len(tt.wantAccepted) == 0 {
				return
			}
			rawAccepted, _ := errObj["accepted_values"].([]any)
			got := make([]string, 0, len(rawAccepted))
			for _, raw := range rawAccepted {
				value, _ := raw.(string)
				got = append(got, value)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantAccepted, ",") {
				t.Fatalf("error.accepted_values = %#v, want %#v; stdout=%s", got, tt.wantAccepted, stdout)
			}
		})
	}
}

func TestMissingPositionalArgsReportParameterName(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "configure profile", args: []string{"--lang", "en", "configure", "use"}, want: "missing required parameters: <profile>"},
		{name: "resource id", args: []string{"--lang", "en", "vpc", "get"}, want: "missing required parameters: <id>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(tt.args...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", code, stdout, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("stderr must be empty for structured errors, got %q", stderr)
			}
			if got := errorCode(t, stdout); got != "MissingParameter" {
				t.Fatalf("error.code = %q, want MissingParameter; stdout=%s", got, stdout)
			}
			if got := errorMessage(t, stdout); got != tt.want {
				t.Fatalf("error.message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigCommandsReadAliyunConfigButWriteEcctlConfig(t *testing.T) {
	dir := t.TempDir()
	aliyunConfigPath := filepath.Join(dir, ".aliyun", "config.json")
	ecctlConfigPath := filepath.Join(dir, ".ecctl", "config.json")
	writeTestConfig(t, aliyunConfigPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{
				"name":              "default",
				"mode":              "AK",
				"access_key_id":     "keep-id",
				"access_key_secret": "keep-secret",
				"region_id":         "us-west-1",
			},
			map[string]any{
				"name":      "prod",
				"mode":      "AK",
				"region_id": "cn-hangzhou",
			},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", ecctlConfigPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyunConfigPath)
	t.Setenv("ECCTL_REGION", "")

	stdout, stderr, code := runCLI("config", "get")
	if code != 0 {
		t.Fatalf("config get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["profile"] != "default" || out["region"] != "us-west-1" || out["mode"] != "AK" {
		t.Fatalf("config get should read Aliyun fallback: %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "set", "--region", "eu-central-1")
	if code != 0 {
		t.Fatalf("config set exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["key"] != "region" || out["value"] != "eu-central-1" {
		t.Fatalf("config set output = %s", stdout)
	}
	aliyunRaw, err := os.ReadFile(aliyunConfigPath)
	if err != nil {
		t.Fatalf("Read Aliyun config: %v", err)
	}
	if strings.Contains(string(aliyunRaw), "eu-central-1") {
		t.Fatalf("config set must not overwrite Aliyun config: %s", aliyunRaw)
	}
	ecctlRaw, err := os.ReadFile(ecctlConfigPath)
	if err != nil {
		t.Fatalf("Read ecctl config: %v", err)
	}
	if !strings.Contains(string(ecctlRaw), "eu-central-1") {
		t.Fatalf("config set should write ecctl config: %s", ecctlRaw)
	}

	stdout, stderr, code = runCLI("config", "use", "prod")
	if code != 0 {
		t.Fatalf("config use exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["profile"] != "prod" || out["region"] != "cn-hangzhou" {
		t.Fatalf("config use output = %s", stdout)
	}
}

func TestConfigCommandsSupportGitStyleGetSetAndList(t *testing.T) {
	dir := t.TempDir()
	aliyunConfigPath := filepath.Join(dir, ".aliyun", "config.json")
	ecctlConfigPath := filepath.Join(dir, ".ecctl", "config.json")
	writeTestConfig(t, aliyunConfigPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{
				"name":              "default",
				"mode":              "AK",
				"access_key_id":     "old-id",
				"access_key_secret": "old-secret",
				"region_id":         "us-west-1",
				"output_format":     "json",
			},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", ecctlConfigPath)
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", aliyunConfigPath)
	t.Setenv("ECCTL_REGION", "")

	stdout, stderr, code := runCLI("config", "set", "region", "eu-central-1")
	if code != 0 {
		t.Fatalf("config set region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["key"] != "region" || out["value"] != "eu-central-1" {
		t.Fatalf("config set region output = %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "set", "access-key-id", "new-id")
	if code != 0 {
		t.Fatalf("config set access-key-id exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["key"] != "access-key-id" || out["value"] != "new-id" {
		t.Fatalf("config set access-key-id output = %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "set", "access-key-secret", "new-secret")
	if code != 0 {
		t.Fatalf("config set access-key-secret exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["key"] != "access-key-secret" || out["value"] == "new-secret" || out["sensitive"] != true {
		t.Fatalf("config set access-key-secret should mask output: %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "set", "security-token", "sts-token")
	if code != 0 {
		t.Fatalf("config set security-token exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["key"] != "security-token" || out["value"] == "sts-token" || out["sensitive"] != true {
		t.Fatalf("config set security-token should mask output: %s", stdout)
	}

	for _, tt := range []struct {
		key   string
		value string
	}{
		{key: "lang", value: "zh-CN"},
		{key: "output", value: "json"},
	} {
		stdout, stderr, code = runCLI("config", "set", tt.key, tt.value)
		if code != 0 {
			t.Fatalf("config set %s exit %d stderr=%s stdout=%s", tt.key, code, stderr, stdout)
		}
		out = decodeObject(t, stdout)
		if out["key"] != tt.key || out["value"] != tt.value {
			t.Fatalf("config set %s output = %s", tt.key, stdout)
		}
	}

	stdout, stderr, code = runCLI("config", "get", "region")
	if code != 0 {
		t.Fatalf("config get region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["key"] != "region" || out["value"] != "eu-central-1" {
		t.Fatalf("config get region output = %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "get", "access-key-secret")
	if code != 0 {
		t.Fatalf("config get access-key-secret exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["value"] == "new-secret" || out["sensitive"] != true {
		t.Fatalf("config get access-key-secret should mask output: %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "get", "access-key-secret", "--show-secret")
	if code != 0 {
		t.Fatalf("config get access-key-secret --show-secret exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	if out["value"] != "new-secret" {
		t.Fatalf("config get access-key-secret --show-secret output = %s", stdout)
	}

	stdout, stderr, code = runCLI("config", "list")
	if code != 0 {
		t.Fatalf("config list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out = decodeObject(t, stdout)
	items, _ := out["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("config list returned no items: %s", stdout)
	}
	for _, want := range []string{"region", "access-key-id", "access-key-secret", "security-token", "lang", "output", "ecctl configure set access-key-secret <value>"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("config list missing %q: %s", want, stdout)
		}
	}

	aliyunRaw, err := os.ReadFile(aliyunConfigPath)
	if err != nil {
		t.Fatalf("Read Aliyun config: %v", err)
	}
	if strings.Contains(string(aliyunRaw), "new-secret") || strings.Contains(string(aliyunRaw), "eu-central-1") {
		t.Fatalf("config set must not write Aliyun config: %s", aliyunRaw)
	}
	raw, err := os.ReadFile(ecctlConfigPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("saved config invalid JSON: %v", err)
	}
	profile := saved["profiles"].([]any)[0].(map[string]any)
	for _, tt := range []struct {
		field string
		want  string
	}{
		{field: "region_id", want: "eu-central-1"},
		{field: "access_key_id", want: "new-id"},
		{field: "access_key_secret", want: "new-secret"},
		{field: "sts_token", want: "sts-token"},
		{field: "language", want: "zh-CN"},
		{field: "output_format", want: "json"},
	} {
		if profile[tt.field] != tt.want {
			t.Fatalf("saved %s = %v, want %s; profile=%#v", tt.field, profile[tt.field], tt.want, profile)
		}
	}
}

func TestConfigCommandsRejectUnknownConfigKey(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))

	stdout, stderr, code := runCLI("config", "set", "unknown", "value")
	if code != 1 {
		t.Fatalf("config set unknown exit %d, want 1; stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownConfigKey" {
		t.Fatalf("error.code = %q, want UnknownConfigKey; stdout=%s", got, stdout)
	}
}

func TestConfigSetOutputAcceptsText(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	stdout, stderr, code := runCLI("config", "set", "output", "text")
	if code != 0 {
		t.Fatalf("config set output text exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["key"] != "output" || out["value"] != "text" {
		t.Fatalf("config set output text output = %s", stdout)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("saved config invalid JSON: %v", err)
	}
	profile := saved["profiles"].([]any)[0].(map[string]any)
	if profile["output_format"] != "text" {
		t.Fatalf("saved output_format = %v, want text", profile["output_format"])
	}
}

func TestConfiguredTextOutputRendersHumanReadableVPCList(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	writeTestConfig(t, configPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "output_format": "text"},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		result: fakeListVPCsResult{
			VPCs:  []testVPCResource{{ID: "vpc-123", Region: "cn-beijing"}},
			Total: 1,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("configured text output exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if json.Valid([]byte(stdout)) {
		t.Fatalf("configured text output should not be JSON: %s", stdout)
	}
	if !strings.Contains(stdout, "vpcs:") || !strings.Contains(stdout, "- id: vpc-123") {
		t.Fatalf("configured text output missing vpc: %s", stdout)
	}
}

func TestConfiguredLanguageLocalizesHelp(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	writeTestConfig(t, configPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "language": "zh-CN", "output_format": "json"},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	stdout, stderr, code := runCLI("--lang", "zh-CN", "vpc", "create", "--help")
	if code != 0 {
		t.Fatalf("vpc create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "创建 VPC") || !strings.Contains(stdout, "资源参数:") {
		t.Fatalf("configured language should localize help: %s", stdout)
	}
}

func TestResourceCommandsResolveRegionFromAliyunCLIConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".aliyun", "config.json")
	writeTestConfig(t, configPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "mode": "AK", "region_id": "us-west-1"},
		},
	})
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), ".ecctl", "missing-config.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", configPath)
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "us-west-1",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		result: fakeListVPCsResult{
			VPCs:  []testVPCResource{{ID: "vpc-123", Region: "us-west-1"}},
			Total: 1,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list")
	if code != 0 {
		t.Fatalf("vpc list should use region from aliyun config; exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if _, ok := out["vpcs"].([]any); !ok {
		t.Fatalf("vpcs missing: %s", stdout)
	}
}

func TestResourceListHelpExposesFieldsFlag(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "vpc", "list", "--help")
	if code != 0 {
		t.Fatalf("vpc list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--fields") || !strings.Contains(stdout, "resource fields") {
		t.Fatalf("help should expose --fields resource object crop flag: %s", stdout)
	}
}

func TestResourceGetHelpExposesFieldsFlagWithoutOtherInputs(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "assistant", "get", "--help")
	if code != 0 {
		t.Fatalf("ecs assistant get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--fields") {
		t.Fatalf("get help should expose --fields even without other inputs: %s", stdout)
	}
}

func TestVPCListUsesCloudService(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		result: fakeListVPCsResult{
			VPCs: []testVPCResource{{
				ID:     "vpc-123",
				Name:   "prod",
				CIDR:   "10.0.0.0/16",
				Status: "Available",
				Region: "cn-beijing",
			}},
			Total: 1,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("vpc list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.calls != 1 {
		t.Fatalf("ListVPCs calls = %d, want 1", fake.calls)
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(1) {
		t.Fatalf("total = %v, want 1; output=%s", out["total"], stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil {
		t.Fatalf("pagination missing: %s", stdout)
	}
	if pagination["page"] != float64(1) || pagination["limit"] != float64(vpcListDefaultLimit) || pagination["has_more"] != false {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
	vpcs, _ := out["vpcs"].([]any)
	if len(vpcs) != 1 {
		t.Fatalf("vpcs len = %d, want 1; output=%s", len(vpcs), stdout)
	}
	vpc, _ := vpcs[0].(map[string]any)
	if vpc["id"] != "vpc-123" || vpc["region"] != "cn-beijing" {
		t.Fatalf("unexpected vpc output: %s", stdout)
	}
}

func TestVPCListFieldsCropsResourceObjects(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		result: fakeListVPCsResult{
			VPCs: []testVPCResource{{
				ID:     "vpc-123",
				Name:   "prod",
				CIDR:   "10.0.0.0/16",
				Status: "Available",
				Region: "cn-beijing",
			}},
			Total: 1,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--fields", "id,status")
	if code != 0 {
		t.Fatalf("vpc list --fields exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(1) {
		t.Fatalf("total metadata missing after fields crop: %s", stdout)
	}
	if _, ok := out["pagination"].(map[string]any); !ok {
		t.Fatalf("pagination metadata missing after fields crop: %s", stdout)
	}
	vpcs, _ := out["vpcs"].([]any)
	if len(vpcs) != 1 {
		t.Fatalf("vpcs len = %d, want 1; output=%s", len(vpcs), stdout)
	}
	vpc, _ := vpcs[0].(map[string]any)
	if len(vpc) != 2 || vpc["id"] != "vpc-123" || vpc["status"] != "Available" {
		t.Fatalf("fields crop returned %#v, want id/status only; output=%s", vpc, stdout)
	}
	for _, name := range []string{"name", "cidr", "region"} {
		if _, ok := vpc[name]; ok {
			t.Fatalf("fields crop kept %q in %#v; output=%s", name, vpc, stdout)
		}
	}
}

func TestVPCListFieldsRejectsUnknownWithoutCloudCall(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	factory := func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		t.Fatal("caller factory must not be invoked for invalid --fields")
		return nil, nil
	}

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--fields", "id,unknown")
	if code != 1 {
		t.Fatalf("invalid --fields exit = %d, want 1; stderr=%s stdout=%s", code, stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidFields" {
		t.Fatalf("error.code = %q, want InvalidFields; stdout=%s", got, stdout)
	}
	errObj, _ := decodeObject(t, stdout)["error"].(map[string]any)
	if errObj["field"] != "unknown" {
		t.Fatalf("error.field = %#v, want unknown; stdout=%s", errObj["field"], stdout)
	}
	if !containsStringValue(errObj["accepted_values"], "id") {
		t.Fatalf("error.accepted_values should include supported fields: %s", stdout)
	}
}

func TestVPCListFiltersUseSpecMappings(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		wantListRequest: map[string]any{
			"VpcOwnerId": 123,
			"Tag":        []string{"env=prod"},
		},
		result: fakeListVPCsResult{Total: 0},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--filter", "owner-id=123", "--filter", "tag.env=prod")
	if code != 0 {
		t.Fatalf("vpc list with filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.calls != 1 {
		t.Fatalf("ListVPCs calls = %d, want 1", fake.calls)
	}
}

func TestOutputTextFlagRendersHumanReadableVPCList(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   defaultListPage,
		result: fakeListVPCsResult{
			VPCs: []testVPCResource{{
				ID:     "vpc-123",
				Name:   "prod",
				Status: "Available",
				Region: "cn-beijing",
			}},
			Total: 1,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--output", "text")
	if code != 0 {
		t.Fatalf("vpc list --output text exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if json.Valid([]byte(stdout)) {
		t.Fatalf("text output should not be JSON: %s", stdout)
	}
	for _, want := range []string{"vpcs:", "- id: vpc-123", "name: prod", "status: Available", "total: 1"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("text output missing %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("non-tty text output should not contain ANSI escapes: %q", stdout)
	}
}

func TestVPCListDefaultsLimitTo50(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   1,
		result: fakeListVPCsResult{
			VPCs:  []testVPCResource{{ID: "vpc-123", Region: "cn-beijing"}},
			Total: 79,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("vpc list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.calls != 1 {
		t.Fatalf("ListVPCs calls = %d, want 1", fake.calls)
	}
	out := decodeObject(t, stdout)
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil {
		t.Fatalf("pagination missing: %s", stdout)
	}
	if pagination["page"] != float64(1) || pagination["limit"] != float64(vpcListDefaultLimit) || pagination["returned"] != float64(1) || pagination["has_more"] != true {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
	if _, ok := pagination["total"]; ok {
		t.Fatalf("pagination must not duplicate top-level total: %s", stdout)
	}
}

func TestVPCListLimit(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  1,
		wantPage:   1,
		result: fakeListVPCsResult{
			VPCs: []testVPCResource{{
				ID:     "vpc-123",
				Region: "cn-beijing",
			}},
			Total: 79,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--limit", "1")
	if code != 0 {
		t.Fatalf("vpc list --limit exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if out["total"] != float64(79) {
		t.Fatalf("total = %v, want true cloud total 79; output=%s", out["total"], stdout)
	}
	vpcs, _ := out["vpcs"].([]any)
	if len(vpcs) != 1 {
		t.Fatalf("vpcs len = %d, want 1; output=%s", len(vpcs), stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil {
		t.Fatalf("pagination missing: %s", stdout)
	}
	if pagination["limit"] != float64(1) || pagination["next_page"] != float64(2) {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
	if _, ok := pagination["total"]; ok {
		t.Fatalf("pagination must not duplicate top-level total: %s", stdout)
	}

	stdout, _, code = runCLI("vpc", "list", "--region", "cn-beijing", "--limit", "0")
	if code != 1 {
		t.Fatalf("invalid limit exit = %d, want 1; stdout=%s", code, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidLimit" {
		t.Fatalf("error.code = %q, want InvalidLimit", got)
	}
}

func TestResourceListLimitMaxFailsBeforeCallerCreation(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	factory := func(_ string, _ string, _ spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		t.Fatal("limit max validation should fail before creating a caller")
		return nil, nil
	}

	stdout, stderr, code := runCLIWith(factory, "--lang", "en", "ecs", "instance", "list", "--region", "cn-hangzhou", "--limit", "101")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", code, stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "InvalidLimit" {
		t.Fatalf("error.code = %q, want InvalidLimit; stdout=%s", got, stdout)
	}
	if got, _ := errorObject(t, stdout)["field"].(string); got != "limit" {
		t.Fatalf("error.field = %q, want limit; stdout=%s", got, stdout)
	}
	if message := errorMessage(t, stdout); !strings.Contains(message, "100") {
		t.Fatalf("error.message = %q, want it to mention 100", message)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("stderr must be empty for structured errors, got %q", stderr)
	}
}

func TestVPCListPage(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  vpcListDefaultLimit,
		wantPage:   2,
		result: fakeListVPCsResult{
			VPCs:  []testVPCResource{{ID: "vpc-456", Region: "cn-beijing"}},
			Total: 79,
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "list", "--region", "cn-beijing", "--page", "2")
	if code != 0 {
		t.Fatalf("vpc list --page exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	pagination, _ := out["pagination"].(map[string]any)
	if pagination == nil {
		t.Fatalf("pagination missing: %s", stdout)
	}
	if pagination["page"] != float64(2) || pagination["has_more"] != false {
		t.Fatalf("unexpected pagination: %s", stdout)
	}
}

func TestCLIDoesNotBranchOnConcreteLanguageTag(t *testing.T) {
	raw, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("ReadFile root.go: %v", err)
	}
	if strings.Contains(string(raw), "zh-Hans") {
		t.Fatalf("CLI package should delegate language-specific behavior to pkg/i18n")
	}
}

func TestVPCGetUsesCloudService(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantGetID:  "vpc-123",
		vpcResult: testVPCResource{
			ID:     "vpc-123",
			Name:   "prod",
			CIDR:   "10.0.0.0/16",
			Status: "Available",
			Region: "cn-beijing",
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "get", "vpc-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("vpc get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.getVPCCalls != 1 {
		t.Fatalf("GetVPC calls = %d, want 1", fake.getVPCCalls)
	}
	vpc, _ := decodeObject(t, stdout)["vpc"].(map[string]any)
	if vpc == nil || vpc["id"] != "vpc-123" || vpc["cidr"] != "10.0.0.0/16" {
		t.Fatalf("unexpected vpc get output: %s", stdout)
	}
}

func TestVPCGetFieldsCropsResourceObject(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantGetID:  "vpc-123",
		vpcResult: testVPCResource{
			ID:     "vpc-123",
			Name:   "prod",
			CIDR:   "10.0.0.0/16",
			Status: "Available",
			Region: "cn-beijing",
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "get", "vpc-123", "--region", "cn-beijing", "--fields", "id,cidr")
	if code != 0 {
		t.Fatalf("vpc get --fields exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	vpc, _ := decodeObject(t, stdout)["vpc"].(map[string]any)
	if len(vpc) != 2 || vpc["id"] != "vpc-123" || vpc["cidr"] != "10.0.0.0/16" {
		t.Fatalf("fields crop returned %#v, want id/cidr only; output=%s", vpc, stdout)
	}
	for _, name := range []string{"name", "status", "region"} {
		if _, ok := vpc[name]; ok {
			t.Fatalf("fields crop kept %q in %#v; output=%s", name, vpc, stdout)
		}
	}
}

func TestVPCCreateUsesCloudService(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantCreateVPC: fakeCreateVPCRequest{
			Region: "cn-beijing",
			Name:   "prod",
			CIDR:   "10.0.0.0/16",
			Tags:   []string{"env=dev"},
		},
		createVPCResult: fakeCreateVPCResult{
			VPC:       testVPCResource{ID: "vpc-123", Name: "prod", CIDR: "10.0.0.0/16", Status: "Available", Region: "cn-beijing"},
			RequestID: "req-vpc",
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "create", "--region", "cn-beijing", "--name", "prod", "--cidr", "10.0.0.0/16", "--tag", "env=dev")
	if code != 0 {
		t.Fatalf("vpc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.createVPCCalls != 1 {
		t.Fatalf("CreateVPC calls = %d, want 1", fake.createVPCCalls)
	}
	out := decodeObject(t, stdout)
	vpc, _ := out["vpc"].(map[string]any)
	if vpc == nil || vpc["id"] != "vpc-123" {
		t.Fatalf("unexpected vpc create output: %s", stdout)
	}
	if _, ok := out["request_id"]; ok {
		t.Fatalf("create output must not include top-level request_id: %s", stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("create output missing actions: %s", stdout)
	}
	first, _ := actions[0].(map[string]any)
	if first["request_id"] != "req-vpc" || first["action_name"] != "CreateVpc" {
		t.Fatalf("unexpected create actions: %#v", actions)
	}
}

func TestVPCCreateAllowsOmittedNameAndCIDR(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantCreateVPC: fakeCreateVPCRequest{
			Region: "cn-beijing",
		},
		createVPCResult: fakeCreateVPCResult{
			VPC:       testVPCResource{ID: "vpc-123", Region: "cn-beijing"},
			RequestID: "req-vpc",
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "create", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("vpc create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.createVPCCalls != 1 {
		t.Fatalf("CreateVPC calls = %d, want 1", fake.createVPCCalls)
	}
}

func TestVPCDeleteUsesCloudService(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeVPCService{
		t:          t,
		wantRegion: "cn-beijing",
		wantDeleteVPC: fakeDeleteVPCRequest{
			Region: "cn-beijing",
			ID:     "vpc-123",
			NoWait: true,
		},
		deleteVPCResult: fakeDeleteVPCResult{
			ID:        "vpc-123",
			Deleted:   true,
			RequestID: "req-del-vpc",
		},
	}
	factory := vpcCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "vpc", "delete", "vpc-123", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("vpc delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.deleteVPCCalls != 1 {
		t.Fatalf("DeleteVPC calls = %d, want 1", fake.deleteVPCCalls)
	}
	out := decodeObject(t, stdout)
	vpc, _ := out["vpc"].(map[string]any)
	if out["deleted"] != true || vpc == nil || vpc["id"] != "vpc-123" {
		t.Fatalf("unexpected vpc delete output: %s", stdout)
	}
	if _, ok := out["request_id"]; ok {
		t.Fatalf("delete output must not include top-level request_id: %s", stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("delete output missing actions: %s", stdout)
	}
	first, _ := actions[0].(map[string]any)
	if first["request_id"] != "req-del-vpc" || first["action_name"] != "DeleteVpc" {
		t.Fatalf("unexpected delete actions: %#v", actions)
	}
}

func TestECSInstanceListUsesCloudService(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantLimit:  defaultListLimit,
		wantListRequest: map[string]any{
			"Status": "Running",
			"Tag":    []string{"env=prod"},
		},
		result: fakeListInstancesResult{
			Instances: []testInstance{{
				ID:      "i-123",
				Name:    "web-01",
				Status:  "Running",
				Region:  "cn-beijing",
				Zone:    "cn-beijing-a",
				VPC:     "vpc-123",
				VSwitch: "vsw-123",
				Image:   "aliyun_3_x64_20G_alibase_20240528.vhd",
				Type:    "ecs.e3.medium",
			}},
			Total: 1,
		},
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "ecs", "instance", "list", "--region", "cn-beijing", "--filter", "status=Running", "--filter", "tag.env=prod")
	if code != 0 {
		t.Fatalf("ecs instance list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.describeInstancesCalls != 1 {
		t.Fatalf("DescribeInstances calls = %d, want 1", fake.describeInstancesCalls)
	}
	out := decodeObject(t, stdout)
	instances, _ := out["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("instances len = %d, want 1; output=%s", len(instances), stdout)
	}
	instance, _ := instances[0].(map[string]any)
	if instance["id"] != "i-123" || instance["status"] != "Running" || instance["vswitch"] != "vsw-123" {
		t.Fatalf("unexpected instance output: %s", stdout)
	}
}

func TestECSInstanceCreateUsesRunInstancesAndWaitsForRunning(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantCreateInstance: fakeCreateInstanceRequest{
			Region:  "cn-beijing",
			Image:   "aliyun_3_x64_20G_alibase_20240528.vhd",
			Type:    "ecs.e3.medium",
			SG:      "sg-123",
			VSwitch: "vsw-123",
			Name:    "web-01",
			Tags:    []string{"env=dev"},
		},
		createInstanceID: "i-123",
		createRequestID:  "req-run",
		result: fakeListInstancesResult{
			Instances: []testInstance{{
				ID:      "i-123",
				Name:    "web-01",
				Status:  "Running",
				Region:  "cn-beijing",
				VSwitch: "vsw-123",
				Image:   "aliyun_3_x64_20G_alibase_20240528.vhd",
				Type:    "ecs.e3.medium",
			}},
			Total: 1,
		},
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory,
		"ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
		"--name", "web-01",
		"--tag", "env=dev",
	)
	if code != 0 {
		t.Fatalf("ecs instance create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.runInstancesCalls != 1 || fake.describeInstancesCalls != 1 {
		t.Fatalf("calls: RunInstances=%d DescribeInstances=%d", fake.runInstancesCalls, fake.describeInstancesCalls)
	}
	out := decodeObject(t, stdout)
	instance, _ := out["instance"].(map[string]any)
	if instance == nil || instance["id"] != "i-123" || instance["status"] != "Running" {
		t.Fatalf("unexpected instance create output: %s", stdout)
	}
	if _, ok := out["request_id"]; ok {
		t.Fatalf("create output must not include top-level request_id: %s", stdout)
	}
	if _, ok := out["actions_taken"]; ok {
		t.Fatalf("create output must use actions, not actions_taken: %s", stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) != 2 {
		t.Fatalf("actions len = %d, want 2; output=%s", len(actions), stdout)
	}
	first, _ := actions[0].(map[string]any)
	second, _ := actions[1].(map[string]any)
	if first["request_id"] != "req-run" || first["action_name"] != "RunInstances" ||
		second["request_id"] != "req-describe-instances" || second["action_name"] != "DescribeInstances" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}

func TestECSInstanceCreateDryRunRejectsAmountAboveOne(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory,
		"--lang", "en",
		"ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
		"--amount", "3",
		"--dry-run",
	)
	if code != 1 {
		t.Fatalf("ecs instance create --dry-run exit %d, want 1; stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	errObject, _ := out["error"].(map[string]any)
	if errObject["kind"] != "client" || errObject["code"] != "InvalidDryRunAmount" {
		t.Fatalf("unexpected dry-run error: %s", stdout)
	}
	if errObject["message"] != "ECS dry-run only supports a single instance" ||
		errObject["field"] != "amount" ||
		!containsStringValue(errObject["accepted_values"], "1") ||
		!strings.Contains(fmt.Sprint(errObject["suggested_action"]), "--amount 1") {
		t.Fatalf("dry-run error must explain the supported amount: %s", stdout)
	}
	if fake.runInstancesCalls != 0 || fake.describeInstancesCalls != 0 {
		t.Fatalf("invalid dry-run must not call ECS, RunInstances=%d DescribeInstances=%d", fake.runInstancesCalls, fake.describeInstancesCalls)
	}
}

func TestECSInstanceCreateErrorOutputsFailedAction(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantCreateInstance: fakeCreateInstanceRequest{
			Region:  "cn-beijing",
			Image:   "missing.vhd",
			Type:    "ecs.e3.medium",
			SG:      "sg-123",
			VSwitch: "vsw-123",
		},
		runInstancesError: ecerrors.Service(
			"InvalidImageId.NotFound",
			"The specified ImageId does not exist.",
			false,
			ecerrors.WithRequestID("req-failed-run"),
		),
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory,
		"ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.e3.medium",
		"--image", "missing.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
	)
	if code == 0 {
		t.Fatalf("ecs instance create succeeded, want API error; stderr=%s stdout=%s", stderr, stdout)
	}
	out := decodeObject(t, stdout)
	if _, ok := out["error"].(map[string]any); !ok {
		t.Fatalf("error missing: %s", stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want failed action; output=%s", len(actions), stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["request_id"] != "req-failed-run" ||
		action["action_name"] != "RunInstances" ||
		action["code"] != "InvalidImageId.NotFound" ||
		action["message"] != "The specified ImageId does not exist." {
		t.Fatalf("unexpected failed action: %#v", action)
	}
}

func TestECSInstanceCreateNotFoundMentionsMissingResourceID(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantCreateInstance: fakeCreateInstanceRequest{
			Region:  "cn-beijing",
			Image:   "aliyun_3_x64_20G_alibase_20240528.vhd",
			Type:    "ecs.u1-c1m2.xlarge",
			SG:      "sg-123",
			VSwitch: "vsw-123",
		},
		runInstancesError: ecerrors.NotFound(
			"NotFound",
			"vsw-123 not found",
			ecerrors.WithRequestID("req-failed-run"),
			ecerrors.WithRawCause("InvalidVSwitchId.NotExist", "vSwitch not exists"),
		),
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory,
		"--lang", "en",
		"ecs", "instance", "create",
		"--region", "cn-beijing",
		"--type", "ecs.u1-c1m2.xlarge",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--sg", "sg-123",
		"--vswitch", "vsw-123",
	)
	if code != 4 {
		t.Fatalf("exit = %d, want 4; stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	errObj, _ := out["error"].(map[string]any)
	if errObj["message"] != "vsw-123 not found" {
		t.Fatalf("error.message = %#v; output=%s", errObj["message"], stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want failed action; output=%s", len(actions), stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["request_id"] != "req-failed-run" ||
		action["action_name"] != "RunInstances" ||
		action["code"] != "InvalidVSwitchId.NotExist" ||
		action["message"] != "vSwitch not exists" {
		t.Fatalf("unexpected failed action: %#v", action)
	}
}

func TestECSInstanceUpdateUsesModifyInstanceAttribute(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantUpdateInstance: fakeUpdateInstanceRequest{
			Region: "cn-beijing",
			ID:     "i-123",
			Name:   "web-02",
		},
		updateRequestID: "req-update",
		result: fakeListInstancesResult{
			Instances: []testInstance{{
				ID:     "i-123",
				Name:   "web-02",
				Status: "Running",
				Region: "cn-beijing",
				Type:   "ecs.e3.medium",
			}},
			Total: 1,
		},
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "ecs", "instance", "update", "i-123", "--region", "cn-beijing", "--name", "web-02")
	if code != 0 {
		t.Fatalf("ecs instance update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.modifyInstanceAttributeCalls != 1 || fake.describeInstancesCalls != 1 {
		t.Fatalf("calls: ModifyInstanceAttribute=%d DescribeInstances=%d", fake.modifyInstanceAttributeCalls, fake.describeInstancesCalls)
	}
	instance, _ := decodeObject(t, stdout)["instance"].(map[string]any)
	if instance == nil || instance["id"] != "i-123" || instance["name"] != "web-02" {
		t.Fatalf("unexpected instance update output: %s", stdout)
	}
}

func TestECSInstanceDeleteUsesDeleteInstance(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantDeleteInstance: fakeDeleteInstanceRequest{
			Region: "cn-beijing",
			ID:     "i-123",
			Force:  true,
		},
		deleteRequestID: "req-delete",
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "ecs", "instance", "delete", "i-123", "--region", "cn-beijing", "--force", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.deleteInstanceCalls != 1 {
		t.Fatalf("DeleteInstance calls = %d, want 1", fake.deleteInstanceCalls)
	}
	out := decodeObject(t, stdout)
	instance, _ := out["instance"].(map[string]any)
	if out["deleted"] != true || instance == nil || instance["id"] != "i-123" {
		t.Fatalf("unexpected instance delete output: %s", stdout)
	}
	if _, ok := out["request_id"]; ok {
		t.Fatalf("delete output must not include top-level request_id: %s", stdout)
	}
	actions, _ := out["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1; output=%s", len(actions), stdout)
	}
	action, _ := actions[0].(map[string]any)
	if action["request_id"] != "req-delete" || action["action_name"] != "DeleteInstance" {
		t.Fatalf("unexpected delete action: %#v", action)
	}
}

func TestECSInstanceDeleteWithoutForceSendsForceFalse(t *testing.T) {
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("ECCTL_REGION", "")

	fake := &fakeECSService{
		t:          t,
		wantRegion: "cn-beijing",
		wantDeleteInstance: fakeDeleteInstanceRequest{
			Region: "cn-beijing",
			ID:     "i-456",
			Force:  false, // safety default: must reach the API as false, never silently true
		},
		deleteRequestID: "req-delete-safe",
	}
	factory := ecsCallerFactory(t, fake)

	stdout, stderr, code := runCLIWith(factory, "ecs", "instance", "delete", "i-456", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs instance delete (no --force) exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if fake.deleteInstanceCalls != 1 {
		t.Fatalf("DeleteInstance calls = %d, want 1", fake.deleteInstanceCalls)
	}
}

func TestHelpSurfacesCanonicalFlags(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "vpc", "list", "--help")
	if code != 0 {
		t.Fatalf("vpc list --help exit %d", code)
	}
	for _, must := range []string{"--region", "--filter", "--limit"} {
		if !strings.Contains(stdout, must) {
			t.Fatalf("list help missing %s:\n%s", must, stdout)
		}
	}
	for _, forbidden := range []string{"--RegionId", "--region-id", "--biz-region-id"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("list help contains forbidden %q:\n%s", forbidden, stdout)
		}
	}
	if !strings.Contains(stdout, "--limit int") || !strings.Contains(stdout, "max 50") || !strings.Contains(stdout, "default 50") || !strings.Contains(stdout, "--page int") {
		t.Fatalf("vpc list help missing limit max/default:\n%s", stdout)
	}

	stdout, _, code = runCLI("--lang", "en", "vpc", "create", "--help")
	if code != 0 {
		t.Fatalf("vpc create --help exit %d", code)
	}
	if !strings.Contains(stdout, "--tag") {
		t.Fatalf("create help missing --tag:\n%s", stdout)
	}
	if strings.Contains(stdout, "--filter") {
		t.Fatalf("create help must not contain --filter:\n%s", stdout)
	}
}

func TestResourceHelpGroupsAllActionsAsResourceOperations(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "sg", "--help")
	if code != 0 {
		t.Fatalf("ecs sg --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if strings.Contains(stdout, "Rule Commands:") || strings.Contains(stdout, "Rule Operations:") {
		t.Fatalf("resource help should not split rule actions into a separate group:\n%s", stdout)
	}
	resourceStart := strings.Index(stdout, "Resource Operations:")
	globalStart := strings.Index(stdout, "\nGlobal Flags:")
	if resourceStart < 0 || globalStart < 0 || resourceStart > globalStart {
		t.Fatalf("help should group all actions before global params:\n%s", stdout)
	}
	resourceSection := stdout[resourceStart:globalStart]
	assertInOrder(t, resourceSection,
		"\n  list",
		"\n  get",
		"\n  create",
		"\n  update",
		"\n  delete",
		"\n  authorize",
		"\n  revoke",
	)
}

func TestResourceHelpAlwaysUsesResourceCommandGroup(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "vpc", "vswitch", "--help")
	if code != 0 {
		t.Fatalf("vpc vswitch --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	if strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("resource help should use semantic groups, not generic available commands:\n%s", stdout)
	}
	resourceStart := strings.Index(stdout, "Resource Operations:")
	globalStart := strings.Index(stdout, "\nGlobal Flags:")
	if resourceStart < 0 || globalStart < 0 || resourceStart > globalStart {
		t.Fatalf("resource help should group list/get/create/update/delete before global params:\n%s", stdout)
	}
	resourceSection := stdout[resourceStart:globalStart]
	assertInOrder(t, resourceSection,
		"\n  list",
		"\n  get",
		"\n  create",
		"\n  update",
		"\n  delete",
	)
}

func TestProductHelpGroupsResourceEntrypoints(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "--help")
	if code != 0 {
		t.Fatalf("ecs --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("product help should use resource command group, not generic available commands:\n%s", stdout)
	}
	resourceStart := strings.Index(stdout, "Resource Types:")
	globalStart := strings.Index(stdout, "\nGlobal Flags:")
	if resourceStart < 0 || globalStart < 0 || resourceStart > globalStart {
		t.Fatalf("product help should group resource entrypoints before global params:\n%s", stdout)
	}
	resourceSection := stdout[resourceStart:globalStart]
	assertInOrder(t, resourceSection, "\n  instance", "\n  sg")

	stdout, stderr, code = runCLI("--lang", "en", "vpc", "--help")
	if code != 0 {
		t.Fatalf("vpc --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("mixed resource help should use semantic groups, not generic available commands:\n%s", stdout)
	}
	assertInOrder(t, stdout, "Resource Operations:", "Resource Types:", "Global Flags:")
	if strings.Contains(stdout, "Subresource Types:") {
		t.Fatalf("nested resource entrypoints should still use resource type label:\n%s", stdout)
	}
}

func TestProductAndResourceHelpShowKeyExamples(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   []string
		forbid []string
	}{
		{name: "ecs product", args: []string{"ecs"}, want: []string{"ecctl ecs instance list", "ecctl ecs sg create"}, forbid: []string{"ecctl call ecs DescribeAvailableResource"}},
		{name: "ecs instance", args: []string{"ecs", "instance"}, want: []string{"ecctl ecs instance list", "ecctl call ecs DescribeAvailableResource --region cn-hangzhou --DestinationResource InstanceType --IoOptimized optimized --InstanceType ecs.g5.large", "ecctl ecs instance create"}},
		{name: "ecs sg", args: []string{"ecs", "sg"}, want: []string{"ecctl ecs sg create", "ecctl ecs sg authorize"}},
		{name: "vpc product", args: []string{"vpc"}, want: []string{"ecctl vpc list", "ecctl vpc vswitch create"}},
		{name: "vpc vswitch", args: []string{"vpc", "vswitch"}, want: []string{"ecctl vpc vswitch list", "ecctl vpc vswitch create"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--lang", "en"}, tt.args...)
			args = append(args, "--help")
			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("%v --help exit %d stderr=%s stdout=%s", tt.args, code, stderr, stdout)
			}
			examples := helpExampleCommandLines(stdout)
			if len(examples) < 2 || len(examples) > 4 {
				t.Fatalf("help should show 2-4 key examples, got %d %#v:\n%s", len(examples), examples, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("help examples missing %q:\n%s", want, stdout)
				}
			}
			for _, forbidden := range tt.forbid {
				if strings.Contains(stdout, forbidden) {
					t.Fatalf("help examples should not include %q:\n%s", forbidden, stdout)
				}
			}
		})
	}
}

func TestECSAvailabilityCommandIsRemoved(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "availability", "list")
	if code == 0 {
		t.Fatalf("ecs availability unexpectedly succeeded stderr=%s stdout=%s", stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "UnknownCommand" {
		t.Fatalf("error.code = %q, want UnknownCommand; stdout=%s stderr=%s", got, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "ecs availability list") {
		t.Fatalf("removed availability command should not be suggested:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

func TestECSInstanceCreateHelpShowsAvailabilityPreflight(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "instance", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs instance create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{
		"Before creating instances, call DescribeAvailableResource to check stock.",
		"ecctl call ecs DescribeAvailableResource --region cn-hangzhou --DestinationResource InstanceType --IoOptimized optimized --InstanceType ecs.g5.large",
		"ecctl ecs instance create --type <type> --image <image-id> --sg <sg-id> --vswitch <vsw-id>",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ecs instance create help missing %q:\n%s", want, stdout)
		}
	}
}

func TestConfigHelpShowsKeyExamples(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "configure", args: []string{"configure"}, want: []string{"ecctl configure get", "ecctl configure set region cn-hangzhou"}},
		{name: "configure get", args: []string{"configure", "get"}, want: []string{"ecctl configure get", "ecctl configure get region"}},
		{name: "configure set", args: []string{"configure", "set"}, want: []string{"ecctl configure set region cn-hangzhou", "ecctl configure set output text"}},
		{name: "configure list", args: []string{"configure", "list"}, want: []string{"ecctl configure list", "ecctl configure list --show-secret"}},
		{name: "configure use", args: []string{"configure", "use"}, want: []string{"ecctl configure use default", "ecctl --profile prod configure get"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--lang", "en"}, tt.args...)
			args = append(args, "--help")
			stdout, stderr, code := runCLI(args...)
			if code != 0 {
				t.Fatalf("%v --help exit %d stderr=%s stdout=%s", tt.args, code, stderr, stdout)
			}
			examples := helpExampleCommandLines(stdout)
			if len(examples) < 2 || len(examples) > 4 {
				t.Fatalf("config help should show 2-4 examples, got %d %#v:\n%s", len(examples), examples, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("config help examples missing %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestConfigSetHelpShowsSupportedKeys(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "configure", "set", "--help")
	if code != 0 {
		t.Fatalf("config set --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Supported settings: region, access-key-id, access-key-secret, security-token, lang, output") {
		t.Fatalf("config set help should show supported settings:\n%s", stdout)
	}
}

func TestRootHelpUsesAuxiliaryCommandGroup(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("root --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "Tool Commands:") || strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("root help should use product and auxiliary command groups:\n%s", stdout)
	}
	assertInOrder(t, stdout, "Cloud Product Commands:", "Auxiliary Commands:", "Flags:")
}

func TestGuideCommandIsNotRegistered(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "guide")
	if code == 0 {
		t.Fatalf("guide should not be registered, got exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "Key Design Principles") {
		t.Fatalf("guide command should not print the legacy reference:\n%s", stdout)
	}
}

func TestRootHelpDoesNotShowGuideHint(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			stdout, _, code := runCLI("--lang", lang, "--help")
			if code != 0 {
				t.Fatalf("root --help exit %d", code)
			}
			if strings.Contains(stdout, "ecctl guide") {
				t.Fatalf("root help should not contain guide hint:\n%s", stdout)
			}
		})
	}
}

func TestGuideHintNotOnSubcommands(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "ecs", "--help")
	if code != 0 {
		t.Fatalf("ecs --help exit %d", code)
	}
	if strings.Contains(stdout, "ecctl guide") {
		t.Fatalf("subcommand help should not contain guide hint:\n%s", stdout)
	}
}

func TestRootGoDoesNotHardCodeChineseTranslations(t *testing.T) {
	raw, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("ReadFile root.go: %v", err)
	}
	if hasChineseText(string(raw)) {
		t.Fatalf("root.go must not hard-code Chinese translations; put user-facing translations in pkg/i18n")
	}
}

func TestResourceHelpGroupsFlags(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "vpc", "create", "--help")
	if code != 0 {
		t.Fatalf("vpc create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	resourceStart := strings.Index(stdout, "资源参数:")
	paramsStart := strings.Index(stdout, "\n参数:")
	globalStart := strings.Index(stdout, "\n全局参数:")
	if resourceStart < 0 || paramsStart < 0 || globalStart < 0 || resourceStart > paramsStart || paramsStart > globalStart {
		t.Fatalf("help should show resource params before general params:\n%s", stdout)
	}

	resourceSection := stdout[resourceStart:paramsStart]
	assertInOrder(t, resourceSection, "--name string", "--cidr string", "--tag stringArray", "--dry-run", "--api-param")
	for _, want := range []string{
		"--cidr string",
		"CIDR 网段",
		"--name string",
		"VPC 名称",
		"--tag stringArray",
		"--dry-run",
		"--api-param stringArray",
	} {
		if !strings.Contains(resourceSection, want) {
			t.Fatalf("resource params missing %q:\n%s", want, stdout)
		}
	}
	for _, forbidden := range []string{"--no-wait", "--timeout"} {
		if strings.Contains(resourceSection, forbidden) {
			t.Fatalf("resource params should not contain %q:\n%s", forbidden, stdout)
		}
	}

	paramsSection := stdout[paramsStart:globalStart]
	for _, want := range []string{"--no-wait", "--timeout duration", "(默认 5m)"} {
		if !strings.Contains(paramsSection, want) {
			t.Fatalf("general params missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(paramsSection, "-h, --help") {
		t.Fatalf("general params should not contain help:\n%s", stdout)
	}
	if strings.Contains(paramsSection, "--dry-run") {
		t.Fatalf("general params should not contain --dry-run:\n%s", stdout)
	}
	globalSection := stdout[globalStart:]
	for _, want := range []string{"-h, --help", "--lang string", `--output string`, `(默认 "json")`, "--profile string", "--region string"} {
		if !strings.Contains(globalSection, want) {
			t.Fatalf("global params missing %q:\n%s", want, stdout)
		}
	}
	for _, want := range []string{"--json", "--no-color"} {
		if !strings.Contains(globalSection, want) {
			t.Fatalf("global params missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "5m0s") {
		t.Fatalf("duration defaults should be compact:\n%s", stdout)
	}
	if strings.Contains(stdout, "(default ") {
		t.Fatalf("localized help should translate default labels:\n%s", stdout)
	}
}

func TestVPCListHelpShowsFilterAsCommandParam(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "zh-CN", "vpc", "list", "--help")
	if code != 0 {
		t.Fatalf("vpc list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	resourceStart := strings.Index(stdout, "资源参数:")
	paramsStart := strings.Index(stdout, "\n参数:")
	if resourceStart >= 0 {
		t.Fatalf("list help should not show resource params:\n%s", stdout)
	}
	if paramsStart < 0 {
		t.Fatalf("help should show command params:\n%s", stdout)
	}

	for _, forbidden := range []string{
		"--default",
		"--dhcp-options-set",
		"--enable-ipv6",
		"--id string",
		"--name string",
		"--owner-id",
		"--resource-group",
		"--tag stringArray",
	} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("list help should not contain %q:\n%s", forbidden, stdout)
		}
	}

	globalStart := strings.Index(stdout, "\n全局参数:")
	if globalStart < 0 || paramsStart > globalStart {
		t.Fatalf("help should show command params before global params:\n%s", stdout)
	}
	paramsSection := stdout[paramsStart:globalStart]
	for _, want := range []string{
		"--filter stringArray",
		"过滤表达式 key=value",
		"--ids string",
		"--limit int",
		"最大值：50",
		"--page int",
	} {
		if !strings.Contains(paramsSection, want) {
			t.Fatalf("list command params missing %q:\n%s", want, stdout)
		}
	}

	// Filterable fields are now rendered in their own section after global
	// flags rather than inlined into the --filter flag's usage. The contents
	// must still be discoverable from the help output.
	filterStart := strings.Index(stdout, "可过滤字段:")
	if filterStart < 0 {
		t.Fatalf("help should show filterable fields section:\n%s", stdout)
	}
	filterSection := stdout[filterStart:]
	for _, want := range []string{
		"id",
		"owner-id",
		"tag.<key>",
	} {
		if !strings.Contains(filterSection, want) {
			t.Fatalf("filterable fields section missing %q:\n%s", want, stdout)
		}
	}
}

func TestApplyFilterInputUsesActionFilterSpec(t *testing.T) {
	resource := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"status": {Type: "string"},
			"labels": {Type: "key_value"},
		}},
		Operations: map[string]spec.Operation{
			"list": {
				Filters: map[string]spec.Filter{
					"state": {
						Target: "status",
					},
					"label.": {
						Target:    "labels",
						KeyPrefix: "label.",
					},
				},
			},
		},
	}
	input := map[string]any{
		"filter": []string{"state=Running", "label.env=prod"},
	}

	if err := applyFilterInput(resource, "list", input); err != nil {
		t.Fatalf("applyFilterInput: %v", err)
	}
	if input["status"] != "Running" {
		t.Fatalf("status = %#v, want Running", input["status"])
	}
	labels, _ := input["labels"].([]string)
	if len(labels) != 1 || labels[0] != "env=prod" {
		t.Fatalf("labels = %#v, want env=prod", input["labels"])
	}
	if _, ok := input["filter"]; ok {
		t.Fatalf("filter input should be consumed: %#v", input)
	}
}

func TestChineseHelpTranslationCoverage(t *testing.T) {
	root := newRootCommand(&globalOptions{lang: "zh-CN", output: "json"}, &bytes.Buffer{}, []string{"--lang", "zh-CN"})

	missingCommands := make([]string, 0)
	missingFlags := make(map[string][]string)
	walkCommands(root, func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Short == "" {
			return
		}
		if !hasChineseText(cmd.Short) {
			missingCommands = append(missingCommands, cmd.CommandPath())
		}
		checkFlagTranslationCoverage(cmd, cmd.LocalFlags(), missingFlags)
		checkFlagTranslationCoverage(cmd, cmd.PersistentFlags(), missingFlags)
		checkFlagTranslationCoverage(cmd, cmd.InheritedFlags(), missingFlags)
	})

	if len(missingCommands) > 0 || len(missingFlags) > 0 {
		sort.Strings(missingCommands)
		flagUsages := make([]string, 0, len(missingFlags))
		for usage, commands := range missingFlags {
			sort.Strings(commands)
			flagUsages = append(flagUsages, fmt.Sprintf("%q on %s", usage, strings.Join(commands, ", ")))
		}
		sort.Strings(flagUsages)
		t.Fatalf("missing Chinese help translations: commands=%v flags=%v", missingCommands, flagUsages)
	}
}

func walkCommands(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, child := range cmd.Commands() {
		walkCommands(child, visit)
	}
}

func checkFlagTranslationCoverage(cmd *cobra.Command, flags *pflag.FlagSet, missing map[string][]string) {
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden || flag.Usage == "" || flag.Name == "help" {
			return
		}
		if !hasChineseFlagUsage(flag.Usage) {
			missing[flag.Usage] = append(missing[flag.Usage], cmd.CommandPath()+" --"+flag.Name)
		}
	})
}

func hasChineseFlagUsage(usage string) bool {
	return hasChineseText(usage) || usage == "VPC ID"
}

func hasChineseText(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func assertInOrder(t *testing.T, text string, values ...string) {
	t.Helper()
	previous := -1
	for _, value := range values {
		current := strings.Index(text, value)
		if current < 0 {
			t.Fatalf("missing %q in:\n%s", value, text)
		}
		if current <= previous {
			t.Fatalf("expected %q after previous values in:\n%s", value, text)
		}
		previous = current
	}
}

func helpExampleCommandLines(text string) []string {
	start := strings.Index(text, "示例:")
	if start < 0 {
		start = strings.Index(text, "Examples:")
	}
	if start < 0 {
		return nil
	}
	section := text[start:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ecctl ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeTestConfig(t *testing.T, path string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

type fakeVPCService struct {
	t               *testing.T
	wantRegion      string
	wantLimit       int
	wantPage        int
	wantGetID       string
	wantCreateVPC   fakeCreateVPCRequest
	wantDeleteVPC   fakeDeleteVPCRequest
	wantListRequest map[string]any
	result          fakeListVPCsResult
	vpcResult       testVPCResource
	createVPCResult fakeCreateVPCResult
	deleteVPCResult fakeDeleteVPCResult
	calls           int
	getVPCCalls     int
	createVPCCalls  int
	deleteVPCCalls  int
}

type testVPCResource struct {
	ID     string
	Name   string
	CIDR   string
	Status string
	Region string
}

type fakeListVPCsResult struct {
	VPCs  []testVPCResource
	Total int
}

type fakeCreateVPCRequest struct {
	Region string
	Name   string
	CIDR   string
	Tags   []string
	DryRun bool
	NoWait bool
}

type fakeCreateVPCResult struct {
	VPC       testVPCResource
	RequestID string
}

type fakeDeleteVPCRequest struct {
	Region string
	ID     string
	NoWait bool
}

type fakeDeleteVPCResult struct {
	ID        string
	RequestID string
	Deleted   bool
}

type fakeECSService struct {
	t                            *testing.T
	wantRegion                   string
	wantLimit                    int
	wantListRequest              map[string]any
	wantCreateInstance           fakeCreateInstanceRequest
	wantUpdateInstance           fakeUpdateInstanceRequest
	wantDeleteInstance           fakeDeleteInstanceRequest
	result                       fakeListInstancesResult
	createInstanceID             string
	createRequestID              string
	runInstancesResponse         map[string]any
	runInstancesError            error
	updateRequestID              string
	deleteRequestID              string
	describeInstancesCalls       int
	runInstancesCalls            int
	modifyInstanceAttributeCalls int
	deleteInstanceCalls          int
}

type fakeListInstancesResult struct {
	Instances []testInstance
	Total     int
}

type testInstance struct {
	ID      string
	Name    string
	Status  string
	Region  string
	Zone    string
	VPC     string
	VSwitch string
	Image   string
	Type    string
}

type fakeCreateInstanceRequest struct {
	Region  string
	Image   string
	Type    string
	SG      string
	VSwitch string
	Name    string
	Tags    []string
	Amount  int
	DryRun  bool
}

type fakeUpdateInstanceRequest struct {
	Region      string
	ID          string
	Name        string
	Description string
}

type fakeDeleteInstanceRequest struct {
	Region string
	ID     string
	Force  bool
}

func vpcCallerFactory(t *testing.T, fake *fakeVPCService) ResourceCallerFactory {
	t.Helper()
	return func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "vpc" || resource.Resource != "vpc" {
			t.Fatalf("resource = %s/%s, want vpc/vpc", resource.Product, resource.Resource)
		}
		if fake.wantRegion != "" && region != fake.wantRegion {
			t.Fatalf("factory region = %q, want %q", region, fake.wantRegion)
		}
		return fake, nil
	}
}

func ecsCallerFactory(t *testing.T, fake *fakeECSService) ResourceCallerFactory {
	t.Helper()
	return func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "instance" {
			t.Fatalf("resource = %s/%s, want ecs/instance", resource.Product, resource.Resource)
		}
		if fake.wantRegion != "" && region != fake.wantRegion {
			t.Fatalf("factory region = %q, want %q", region, fake.wantRegion)
		}
		return fake, nil
	}
}

func (f *fakeVPCService) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	switch operation {
	case "DescribeVpcs":
		f.calls++
		f.checkCommon(stringRequestValue(request, "RegionId"), intRequestValue(request, "PageSize"), intRequestValue(request, "PageNumber"))
		f.checkListRequest(request)
		return fakeDescribeVpcsResponse(f.result), nil
	case "DescribeVpcAttribute":
		f.getVPCCalls++
		if f.wantGetID != "" {
			f.checkGet(stringRequestValue(request, "RegionId"), stringRequestValue(request, "VpcId"))
		}
		vpc := f.vpcResult
		if vpc.ID == "" {
			vpc = f.createVPCResult.VPC
		}
		return fakeDescribeVpcAttributeResponse(vpc), nil
	case "CreateVpc":
		req := fakeCreateVPCRequest{
			Region: stringRequestValue(request, "RegionId"),
			Name:   stringRequestValue(request, "VpcName"),
			CIDR:   stringRequestValue(request, "CidrBlock"),
			Tags:   stringSliceRequestValue(request, "Tag"),
			DryRun: boolRequestValue(request, "DryRun"),
		}
		f.createVPCCalls++
		f.checkCreateVPC(req)
		return map[string]any{
			"RequestId": f.createVPCResult.RequestID,
			"VpcId":     f.createVPCResult.VPC.ID,
		}, nil
	case "DeleteVpc":
		req := fakeDeleteVPCRequest{
			Region: stringRequestValue(request, "RegionId"),
			ID:     stringRequestValue(request, "VpcId"),
			NoWait: f.wantDeleteVPC.NoWait,
		}
		f.deleteVPCCalls++
		f.checkDeleteVPC(req)
		return map[string]any{"RequestId": f.deleteVPCResult.RequestID}, nil
	default:
		f.t.Fatalf("unexpected operation %q request %#v", operation, request)
		return nil, nil
	}
}

func (f *fakeECSService) Call(_ context.Context, operation string, request map[string]any) (map[string]any, error) {
	switch operation {
	case "DescribeInstances":
		f.describeInstancesCalls++
		f.checkCommon(stringRequestValue(request, "RegionId"), intRequestValue(request, "MaxResults"))
		f.checkListRequest(request)
		return fakeDescribeInstancesResponse(f.result), nil
	case "RunInstances":
		req := fakeCreateInstanceRequest{
			Region:  stringRequestValue(request, "RegionId"),
			Image:   stringRequestValue(request, "ImageId"),
			Type:    stringRequestValue(request, "InstanceType"),
			SG:      stringRequestValue(request, "SecurityGroupId"),
			VSwitch: stringRequestValue(request, "VSwitchId"),
			Name:    stringRequestValue(request, "InstanceName"),
			Tags:    stringSliceRequestValue(request, "Tag"),
			Amount:  intRequestValue(request, "Amount"),
			DryRun:  boolRequestValue(request, "DryRun"),
		}
		f.runInstancesCalls++
		f.checkCreateInstance(req)
		if f.runInstancesError != nil {
			return nil, f.runInstancesError
		}
		if f.runInstancesResponse != nil {
			return f.runInstancesResponse, nil
		}
		return map[string]any{
			"RequestId": f.createRequestID,
			"InstanceIdSets": map[string]any{
				"InstanceIdSet": []any{f.createInstanceID},
			},
		}, nil
	case "ModifyInstanceAttribute":
		req := fakeUpdateInstanceRequest{
			Region:      stringRequestValue(request, "RegionId"),
			ID:          stringRequestValue(request, "InstanceId"),
			Name:        stringRequestValue(request, "InstanceName"),
			Description: stringRequestValue(request, "Description"),
		}
		f.modifyInstanceAttributeCalls++
		f.checkUpdateInstance(req)
		return map[string]any{"RequestId": f.updateRequestID}, nil
	case "DeleteInstance":
		req := fakeDeleteInstanceRequest{
			Region: stringRequestValue(request, "RegionId"),
			ID:     stringRequestValue(request, "InstanceId"),
			Force:  boolRequestValue(request, "Force"),
		}
		f.deleteInstanceCalls++
		f.checkDeleteInstance(req)
		return map[string]any{"RequestId": f.deleteRequestID}, nil
	default:
		f.t.Fatalf("unexpected operation %q request %#v", operation, request)
		return nil, nil
	}
}

func (f *fakeVPCService) checkCommon(region string, limit int, page int) {
	if region != f.wantRegion {
		f.t.Fatalf("region = %q, want %q", region, f.wantRegion)
	}
	if limit != f.wantLimit {
		f.t.Fatalf("limit = %d, want %d", limit, f.wantLimit)
	}
	if page != f.wantPage {
		f.t.Fatalf("page = %d, want %d", page, f.wantPage)
	}
}

func (f *fakeECSService) checkCommon(region string, limit int) {
	if region != f.wantRegion {
		f.t.Fatalf("region = %q, want %q", region, f.wantRegion)
	}
	if f.wantLimit != 0 && limit != f.wantLimit {
		f.t.Fatalf("limit = %d, want %d", limit, f.wantLimit)
	}
}

func (f *fakeVPCService) checkListRequest(request map[string]any) {
	for key, want := range f.wantListRequest {
		got := request[key]
		if !requestValueEqual(got, want) {
			f.t.Fatalf("%s = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
}

func (f *fakeECSService) checkListRequest(request map[string]any) {
	for key, want := range f.wantListRequest {
		got := request[key]
		if !requestValueEqual(got, want) {
			f.t.Fatalf("%s = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
}

func (f *fakeVPCService) checkGet(region string, id string) {
	if region != f.wantRegion {
		f.t.Fatalf("region = %q, want %q", region, f.wantRegion)
	}
	if id != f.wantGetID {
		f.t.Fatalf("id = %q, want %q", id, f.wantGetID)
	}
}

func (f *fakeECSService) checkCreateInstance(req fakeCreateInstanceRequest) {
	if req.Region != f.wantCreateInstance.Region ||
		req.Image != f.wantCreateInstance.Image ||
		req.Type != f.wantCreateInstance.Type ||
		req.SG != f.wantCreateInstance.SG ||
		req.VSwitch != f.wantCreateInstance.VSwitch ||
		req.Name != f.wantCreateInstance.Name ||
		req.Amount != f.wantCreateInstance.Amount ||
		req.DryRun != f.wantCreateInstance.DryRun {
		f.t.Fatalf("RunInstances request = %#v, want %#v", req, f.wantCreateInstance)
	}
	f.checkStringSlice("tags", req.Tags, f.wantCreateInstance.Tags)
}

func (f *fakeECSService) checkUpdateInstance(req fakeUpdateInstanceRequest) {
	if req != f.wantUpdateInstance {
		f.t.Fatalf("ModifyInstanceAttribute request = %#v, want %#v", req, f.wantUpdateInstance)
	}
}

func (f *fakeECSService) checkDeleteInstance(req fakeDeleteInstanceRequest) {
	if req != f.wantDeleteInstance {
		f.t.Fatalf("DeleteInstance request = %#v, want %#v", req, f.wantDeleteInstance)
	}
}

func requestValueEqual(got, want any) bool {
	switch typedWant := want.(type) {
	case []string:
		typedGot, ok := got.([]string)
		return ok && strings.Join(typedGot, ",") == strings.Join(typedWant, ",")
	default:
		return got == want
	}
}

func (f *fakeECSService) checkStringSlice(name string, got, want []string) {
	if strings.Join(got, ",") != strings.Join(want, ",") {
		f.t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

func (f *fakeVPCService) checkStringSlice(name string, got, want []string) {
	if strings.Join(got, ",") != strings.Join(want, ",") {
		f.t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

func (f *fakeVPCService) checkCreateVPC(req fakeCreateVPCRequest) {
	if req.Region != f.wantCreateVPC.Region || req.Name != f.wantCreateVPC.Name || req.CIDR != f.wantCreateVPC.CIDR || req.DryRun != f.wantCreateVPC.DryRun || req.NoWait != f.wantCreateVPC.NoWait {
		f.t.Fatalf("CreateVPC request = %#v, want %#v", req, f.wantCreateVPC)
	}
	f.checkStringSlice("tags", req.Tags, f.wantCreateVPC.Tags)
}

func (f *fakeVPCService) checkDeleteVPC(req fakeDeleteVPCRequest) {
	if req.Region != f.wantDeleteVPC.Region || req.ID != f.wantDeleteVPC.ID || req.NoWait != f.wantDeleteVPC.NoWait {
		f.t.Fatalf("DeleteVPC request = %#v, want %#v", req, f.wantDeleteVPC)
	}
}

func fakeDescribeVpcsResponse(result fakeListVPCsResult) map[string]any {
	items := make([]any, 0, len(result.VPCs))
	for _, vpc := range result.VPCs {
		items = append(items, fakeVPCMap(vpc))
	}
	return map[string]any{
		"TotalCount": result.Total,
		"Vpcs":       map[string]any{"Vpc": items},
	}
}

func fakeDescribeInstancesResponse(result fakeListInstancesResult) map[string]any {
	items := make([]any, 0, len(result.Instances))
	for _, instance := range result.Instances {
		items = append(items, fakeInstanceMap(instance))
	}
	return map[string]any{
		"RequestId":  "req-describe-instances",
		"TotalCount": result.Total,
		"Instances":  map[string]any{"Instance": items},
	}
}

func fakeDescribeVpcAttributeResponse(vpc testVPCResource) map[string]any {
	return fakeVPCMap(vpc)
}

func fakeVPCMap(vpc testVPCResource) map[string]any {
	item := map[string]any{}
	if vpc.ID != "" {
		item["VpcId"] = vpc.ID
	}
	if vpc.Name != "" {
		item["VpcName"] = vpc.Name
	}
	if vpc.CIDR != "" {
		item["CidrBlock"] = vpc.CIDR
	}
	if vpc.Status != "" {
		item["Status"] = vpc.Status
	}
	if vpc.Region != "" {
		item["RegionId"] = vpc.Region
	}
	return item
}

func fakeInstanceMap(instance testInstance) map[string]any {
	item := map[string]any{}
	if instance.ID != "" {
		item["InstanceId"] = instance.ID
	}
	if instance.Name != "" {
		item["InstanceName"] = instance.Name
	}
	if instance.Status != "" {
		item["Status"] = instance.Status
	}
	if instance.Region != "" {
		item["RegionId"] = instance.Region
	}
	if instance.Zone != "" {
		item["ZoneId"] = instance.Zone
	}
	if instance.Image != "" {
		item["ImageId"] = instance.Image
	}
	if instance.Type != "" {
		item["InstanceType"] = instance.Type
	}
	if instance.VPC != "" || instance.VSwitch != "" {
		item["VpcAttributes"] = map[string]any{
			"VpcId":     instance.VPC,
			"VSwitchId": instance.VSwitch,
		}
	}
	return item
}

func stringRequestValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func stringSliceRequestValue(values map[string]any, key string) []string {
	value, _ := values[key].([]string)
	return value
}

func intRequestValue(values map[string]any, key string) int {
	value, _ := values[key].(int)
	return value
}

func boolRequestValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

type fakeRegionVerifier struct {
	calls []fakeVerifyCall
	err   error
}

type fakeVerifyCall struct {
	region      string
	serviceCode string
}

func (f *fakeRegionVerifier) Verify(region, serviceCode string) error {
	f.calls = append(f.calls, fakeVerifyCall{region: region, serviceCode: serviceCode})
	return f.err
}

func swapRegionVerifier(t *testing.T, factory RegionVerifierFactory) {
	t.Helper()
	t.Cleanup(SetRegionVerifierFactoryForTest(factory))
}

func TestConfigSetRegionVerifiesViaLocationService(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	verifier := &fakeRegionVerifier{}
	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return verifier, nil
	})

	stdout, stderr, code := runCLI("configure", "set", "region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("configure set region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(verifier.calls) != 1 || verifier.calls[0].region != "cn-hangzhou" {
		t.Fatalf("verifier calls = %#v", verifier.calls)
	}
	out := decodeObject(t, stdout)
	if out["key"] != "region" || out["value"] != "cn-hangzhou" {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if _, ok := out["warnings"]; ok {
		t.Fatalf("successful verification must not emit warnings: %s", stdout)
	}
}

func TestConfigSetRegionRejectsInvalidRegion(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	verifier := &fakeRegionVerifier{err: ecerrors.Client("InvalidRegion", "region cn-hangzho is not recognised by Alibaba Cloud Location service")}
	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return verifier, nil
	})

	stdout, stderr, code := runCLI("--lang", "en", "configure", "set", "region", "cn-hangzho")
	if code == 0 {
		t.Fatalf("configure set region cn-hangzho should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidRegion" {
		t.Fatalf("error.code = %q, want InvalidRegion; stdout=%s", got, stdout)
	}
	if msg := errorMessage(t, stdout); !strings.Contains(msg, "--no-verify") {
		t.Fatalf("error.message should mention --no-verify bypass: %q", msg)
	}
	if _, err := os.Stat(configPath); err == nil {
		t.Fatalf("config file should NOT be created when verification fails: %s", configPath)
	}
}

func TestConfigSetRegionForwardsValidRegionsSuggestion(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	suggestion := "did you mean cn-hangzhou? valid regions: cn-beijing, cn-hangzhou, cn-shanghai"
	verifier := &fakeRegionVerifier{err: ecerrors.Client(
		"InvalidRegion",
		"region cn-hangzh111 is not recognised by Alibaba Cloud Location service",
		ecerrors.WithSuggestion(suggestion),
	)}
	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return verifier, nil
	})

	stdout, _, code := runCLI("configure", "set", "region", "cn-hangzh111")
	if code == 0 {
		t.Fatalf("configure set region cn-hangzh111 should fail; stdout=%s", stdout)
	}
	if got := errorSuggestion(t, stdout); got != suggestion {
		t.Fatalf("error.suggestion = %q, want %q", got, suggestion)
	}
}

func TestConfigSetRegionSkipsVerificationWithNoVerifyFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	verifier := &fakeRegionVerifier{err: ecerrors.Client("InvalidRegion", "should not be called")}
	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return verifier, nil
	})

	stdout, stderr, code := runCLI("configure", "set", "region", "cn-hangzho", "--no-verify")
	if code != 0 {
		t.Fatalf("configure set region --no-verify exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(verifier.calls) != 0 {
		t.Fatalf("--no-verify must skip the verifier entirely: %#v", verifier.calls)
	}
}

func TestConfigSetRegionAddsWarningWhenVerifierUnavailable(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return nil, ecerrors.Client("VerificationUnavailable", "Alibaba Cloud credentials are required")
	})

	stdout, stderr, code := runCLI("configure", "set", "region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("configure set region with unavailable verifier exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := decodeObject(t, stdout)
	warnings, _ := out["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("expected warnings entry when verifier is unavailable: %s", stdout)
	}
	if msg, _ := warnings[0].(string); !strings.Contains(msg, "skipped") {
		t.Fatalf("warning message should mention skipped: %q", msg)
	}
	if out["value"] != "cn-hangzhou" {
		t.Fatalf("region should still be persisted: %s", stdout)
	}
}

func TestConfigSetNonRegionKeyBypassesVerifier(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".ecctl", "config.json")
	t.Setenv("ECCTL_CONFIG_PATH", configPath)

	verifier := &fakeRegionVerifier{}
	swapRegionVerifier(t, func(_ string, _ string, _ func(string) string) (RegionVerifier, error) {
		return verifier, nil
	})

	stdout, stderr, code := runCLI("configure", "set", "lang", "zh-CN")
	if code != 0 {
		t.Fatalf("configure set lang exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(verifier.calls) != 0 {
		t.Fatalf("verifier must not be called for non-region keys: %#v", verifier.calls)
	}
}

func TestProductCommandBuildTarget(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		product   string
		resource  string
		buildAll  bool
		stubsOnly bool
	}{
		{name: "root help", args: []string{"--lang", "en", "--help"}, stubsOnly: true},
		{name: "schema list", args: []string{"--lang=en", "schema", "--list", "vpc"}, stubsOnly: true},
		{name: "capabilities", args: []string{"capabilities", "--output", "json"}, stubsOnly: true},
		{name: "configure", args: []string{"configure", "get"}, stubsOnly: true},
		{name: "product command", args: []string{"--lang", "en", "vpc", "list"}, product: "vpc", resource: "list"},
		{name: "product help", args: []string{"ecs", "--help"}, product: "ecs"},
		{name: "resource command", args: []string{"ecs", "instance", "list"}, product: "ecs", resource: "instance"},
		{name: "resource help", args: []string{"ecs", "instance", "--help"}, product: "ecs", resource: "instance"},
		{name: "help product", args: []string{"help", "vpc"}, product: "vpc"},
		{name: "help product resource", args: []string{"help", "ecs", "instance"}, product: "ecs", resource: "instance"},
		{name: "completion script", args: []string{"completion", "bash"}, buildAll: true},
		{name: "cobra dynamic completion", args: []string{"__complete", "vpc", ""}, buildAll: true},
		{name: "boolean globals", args: []string{"--json", "--no-color", "vpc", "list"}, product: "vpc", resource: "list"},
		{name: "output global value", args: []string{"--output", "text", "vpc", "list"}, product: "vpc", resource: "list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := productCommandBuildTarget(tt.args)
			if got.product != tt.product || got.resource != tt.resource || got.buildAll != tt.buildAll || got.stubsOnly != tt.stubsOnly {
				t.Fatalf("target = %#v, want product=%q resource=%q buildAll=%v stubsOnly=%v", got, tt.product, tt.resource, tt.buildAll, tt.stubsOnly)
			}
		})
	}
}

func TestTargetBuildLeavesNonTargetProductAsStub(t *testing.T) {
	specDir := t.TempDir()
	writeRootTestSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
description:
  en: Demo product
examples:
  - ecctl demo list
  - ecctl demo create --name test
`)
	writeRootTestSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
schema:
  fields:
    id:
      type: string
operations:
  list:
    workflow: []
`)
	writeRootTestSpec(t, filepath.Join(specDir, "other", "product.yaml"), `schema_version: 1
product: other
description:
  en: Other product
examples:
  - ecctl other list
  - ecctl other create --name test
`)
	writeRootTestSpec(t, filepath.Join(specDir, "other", "other.yaml"), `schema_version: 2
product: other
resource: other
kind: regional
schema:
  fields:
    id:
      type: string
    name:
      type: string
operations:
  create:
    input:
      fields:
        - name
    workflow: []
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)
	spec.ResetCacheForTest()

	args := []string{"--lang", "en", "demo", "--help"}
	options := newGlobalOptions(args, os.Getenv)
	root := newRootCommand(options, io.Discard, args)

	demo := findDirectCommand(root, "demo")
	if demo == nil {
		t.Fatalf("demo command missing")
	}
	if len(demo.Commands()) == 0 {
		t.Fatalf("target product should have resource/action children")
	}

	other := findDirectCommand(root, "other")
	if other == nil {
		t.Fatalf("other command missing")
	}
	if len(other.Commands()) != 0 {
		t.Fatalf("non-target product should stay a stub, got %d children", len(other.Commands()))
	}
}

func findDirectCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

func TestLazyProductBuildDelaysNonTargetResourceValidation(t *testing.T) {
	specDir := t.TempDir()
	writeRootTestSpec(t, filepath.Join(specDir, "demo", "product.yaml"), `schema_version: 1
product: demo
description:
  en: Demo product
examples:
  - ecctl demo list
  - ecctl demo create --name test
`)
	writeRootTestSpec(t, filepath.Join(specDir, "demo", "demo.yaml"), `schema_version: 2
product: demo
resource: demo
kind: regional
schema:
  fields:
    id:
      type: string
operations:
  list:
    workflow: []
`)
	writeRootTestSpec(t, filepath.Join(specDir, "broken", "product.yaml"), `schema_version: 1
product: broken
description:
  en: Broken product
examples:
  - ecctl broken list
  - ecctl broken create --name test
`)
	writeRootTestSpec(t, filepath.Join(specDir, "broken", "broken.yaml"), `schema_version: 2
product: broken
resource: broken
kind: regional
schema:
  fields:
    id:
      type: string
waiters:
  invalid:
    probe: missing
    target: ready
`)
	t.Setenv("ECCTL_SPEC_DIR", specDir)

	stdout, stderr, code := runCLI("--lang", "en", "demo", "--help")
	if code != 0 {
		t.Fatalf("demo --help should not expose broken product validation; exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}

	stdout, _, code = runCLI("--lang", "en", "broken")
	if code == 0 {
		t.Fatalf("broken command should surface invalid spec: %s", stdout)
	}
	if got := errorCode(t, stdout); got != "InvalidResourceSpec" {
		t.Fatalf("error.code = %q, want InvalidResourceSpec; stdout=%s", got, stdout)
	}
}

func writeRootTestSpec(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestHelpCommandBuildsTargetProduct(t *testing.T) {
	direct, stderr, code := runCLI("--lang", "en", "vpc", "--help")
	if code != 0 {
		t.Fatalf("vpc --help exit %d stderr=%s stdout=%s", code, stderr, direct)
	}
	viaHelp, stderr, code := runCLI("--lang", "en", "help", "vpc")
	if code != 0 {
		t.Fatalf("help vpc exit %d stderr=%s stdout=%s", code, stderr, viaHelp)
	}
	for _, want := range []string{"Resource Operations:", "list", "create", "delete"} {
		if !strings.Contains(viaHelp, want) {
			t.Fatalf("help vpc missing %q:\n%s", want, viaHelp)
		}
	}
}

func TestRootHelpStillListsProductStubs(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("root help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"Cloud Product Commands:", "vpc", "ecs", "ack", "lingjun"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Resource Operations:") {
		t.Fatalf("root help should not render resource action groups:\n%s", stdout)
	}
}

func TestGuideCommandDoesNotAppearInRootHelp(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("--help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "guide") {
		t.Fatalf("root help should not list guide command:\n%s", stdout)
	}
}

func TestRootHelpHidesExamplesCompatibilityCommand(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--help")
	if code != 0 {
		t.Fatalf("root help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "examples") {
		t.Fatalf("root help should not list examples compatibility command:\n%s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "examples", "vpc")
	if code != 0 {
		t.Fatalf("hidden examples command should remain callable; exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

func TestExamplesDefaultListsProductTopicsOnly(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "examples")
	if code != 0 {
		t.Fatalf("examples exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("examples stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	topics, _ := out["topics"].([]any)
	if len(topics) == 0 {
		t.Fatalf("examples should return at least one topic: %s", stdout)
	}
	for _, raw := range topics {
		topic, _ := raw.(map[string]any)
		name, _ := topic["topic"].(string)
		if strings.Contains(name, ".") {
			t.Fatalf("default examples should only list product-level topics, got %q: %s", name, stdout)
		}
	}
	if _, ok := out["hint"]; !ok {
		t.Fatalf("default examples should include drill-down hint: %s", stdout)
	}
}

func TestExamplesAllListsAllTopics(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "examples", "--all")
	if code != 0 {
		t.Fatalf("examples --all exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("examples --all stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	topics, _ := out["topics"].([]any)
	if len(topics) == 0 {
		t.Fatalf("examples --all should return topics: %s", stdout)
	}
	hasDotTopic := false
	for _, raw := range topics {
		topic, _ := raw.(map[string]any)
		name, _ := topic["topic"].(string)
		if strings.Contains(name, ".") {
			hasDotTopic = true
			break
		}
	}
	if !hasDotTopic {
		t.Fatalf("examples --all should include resource/action topics: %s", stdout)
	}
	if _, ok := out["hint"]; ok {
		t.Fatalf("examples --all should not include hint: %s", stdout)
	}
}

func TestExamplesHideNonPublicTopics(t *testing.T) {
	hiddenTopics := []string{
		"rg",
		"rg.group",
		"tag",
		"tag.resource",
	}
	for _, args := range [][]string{
		{"--lang", "en", "examples"},
		{"--lang", "en", "examples", "--all"},
	} {
		stdout, stderr, code := runCLI(args...)
		if code != 0 {
			t.Fatalf("%v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
		}
		for _, hidden := range hiddenTopics {
			if strings.Contains(stdout, `"`+hidden+`"`) {
				t.Fatalf("%v should hide non-public topic %q:\n%s", args, hidden, stdout)
			}
		}
	}

	for _, topic := range hiddenTopics {
		stdout, stderr, code := runCLI("--lang", "en", "examples", topic)
		if code == 0 {
			t.Fatalf("examples %s should be hidden; stdout=%s stderr=%s", topic, stdout, stderr)
		}
		if got := errorCode(t, stdout); got != "UnknownTopic" {
			t.Fatalf("examples %s error.code = %q, want UnknownTopic; stdout=%s", topic, got, stdout)
		}
	}
}

func TestExamplesSingleTopicReturnsExamples(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "examples", "vpc")
	if code != 0 {
		t.Fatalf("examples vpc exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("examples vpc stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["topic"] != "vpc" {
		t.Fatalf("examples vpc topic = %#v: %s", out["topic"], stdout)
	}
	examples, _ := out["examples"].([]any)
	if len(examples) == 0 {
		t.Fatalf("examples vpc should have examples: %s", stdout)
	}
}

func TestExamplesActionTopicReturnsExamples(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "examples", "vpc.vpc.create")
	if code != 0 {
		t.Fatalf("examples vpc.vpc.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("examples vpc.vpc.create stderr = %q", stderr)
	}
	out := decodeObject(t, stdout)
	if out["topic"] != "vpc.vpc.create" {
		t.Fatalf("topic = %#v: %s", out["topic"], stdout)
	}
	examples, _ := out["examples"].([]any)
	if len(examples) == 0 {
		t.Fatalf("vpc.vpc.create should have examples: %s", stdout)
	}
}

func TestExamplesUnknownTopicReturnsError(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "examples", "not-a-topic")
	if code == 0 {
		t.Fatalf("unknown topic should fail; stderr=%s stdout=%s", stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("unknown topic stderr = %q", stderr)
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownTopic" {
		t.Fatalf("error.code = %#v, want UnknownTopic: %s", errObj["code"], stdout)
	}
	if errObj["field"] != "topic" {
		t.Fatalf("error.field = %#v, want topic: %s", errObj["field"], stdout)
	}
}

func TestExamplesTextOutputListsTopics(t *testing.T) {
	stdout, stderr, code := runCLI("--lang", "en", "--output", "text", "examples")
	if code != 0 {
		t.Fatalf("examples text exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if stderr != "" {
		t.Fatalf("examples text stderr = %q", stderr)
	}
	if !strings.Contains(stdout, "vpc") || !strings.Contains(stdout, "ecs") {
		t.Fatalf("text topic list should include vpc and ecs:\n%s", stdout)
	}
	if strings.Contains(stdout, "vpc.") {
		t.Fatalf("default text topic list should not include dotted topics:\n%s", stdout)
	}
}

func TestExamplesTextOutputAllListsDottedTopics(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "--output", "text", "examples", "--all")
	if code != 0 {
		t.Fatalf("examples --all text exit %d", code)
	}
	if !strings.Contains(stdout, "vpc.vpc.create") {
		t.Fatalf("text --all should include dotted topics:\n%s", stdout)
	}
}

func TestExamplesTextOutputSingleTopic(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "--output", "text", "examples", "vpc")
	if code != 0 {
		t.Fatalf("examples text vpc exit %d", code)
	}
	if !strings.Contains(stdout, "ecctl") {
		t.Fatalf("single topic text output should include ecctl invocations:\n%s", stdout)
	}
}

func TestExamplesLocalizedChinese(t *testing.T) {
	stdout, _, code := runCLI("--lang", "zh-CN", "examples")
	if code != 0 {
		t.Fatalf("zh-CN examples exit %d", code)
	}
	out := decodeObject(t, stdout)
	if _, ok := out["hint"]; !ok {
		t.Fatalf("zh-CN examples should include hint: %s", stdout)
	}
}

func TestExamplesResourceTopicReturnsExamples(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "examples", "ecs.instance")
	if code != 0 {
		t.Fatalf("examples ecs.instance exit %d stdout=%s", code, stdout)
	}
	out := decodeObject(t, stdout)
	if out["topic"] != "ecs.instance" {
		t.Fatalf("topic = %#v: %s", out["topic"], stdout)
	}
}

func TestExamplesWithDescriptionField(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "examples", "ecs")
	if code != 0 {
		t.Fatalf("examples ecs exit %d stdout=%s", code, stdout)
	}
	out := decodeObject(t, stdout)
	if out["topic"] != "ecs" {
		t.Fatalf("topic = %#v: %s", out["topic"], stdout)
	}
}

func TestExamplesTopicListIncludesExamplesCount(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "examples")
	if code != 0 {
		t.Fatalf("examples exit %d", code)
	}
	out := decodeObject(t, stdout)
	topics, _ := out["topics"].([]any)
	for _, raw := range topics {
		topic, _ := raw.(map[string]any)
		count, ok := topic["examples_count"].(float64)
		if !ok || count <= 0 {
			t.Fatalf("topic %#v should have examples_count > 0: %#v", topic["topic"], topic)
		}
	}
}

func TestFilterExamplesByActionPrefix(t *testing.T) {
	examples := []string{
		"ecctl vpc vpc create --name test",
		"ecctl vpc vpc list --region cn-hangzhou",
		"ecctl ecs instance create --type ecs.e3.medium",
	}
	got := filterExamplesByActionPrefix(examples, "vpc", "vpc", "create")
	if len(got) != 1 || got[0] != examples[0] {
		t.Fatalf("filterExamplesByActionPrefix(create) = %#v", got)
	}
	got = filterExamplesByActionPrefix(examples, "vpc", "vpc", "delete")
	if got != nil {
		t.Fatalf("filterExamplesByActionPrefix(delete) = %#v, want nil", got)
	}
	got = filterExamplesByActionPrefix(nil, "vpc", "vpc", "create")
	if got != nil {
		t.Fatalf("filterExamplesByActionPrefix(nil) = %#v, want nil", got)
	}
}

func TestFilterExamplesByActionPrefixCollapsedProductResource(t *testing.T) {
	examples := []string{
		"ecctl tag create --resource i-123 --tag env=prod",
		"ecctl tag tag create --resource i-123 --tag env=prod",
	}
	got := filterExamplesByActionPrefix(examples, "tag", "tag", "create")
	if len(got) != 2 {
		t.Fatalf("collapsed product==resource filter = %#v, want 2 matches", got)
	}
}

func TestFirstDescriptionLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"single line", "single line"},
		{"first\nsecond\nthird", "first"},
		{"  padded  \nsecond", "padded"},
	}
	for _, tt := range tests {
		text := spec.LocalizedText{"en": tt.input}
		if got := firstDescriptionLine(text, "en"); got != tt.want {
			t.Errorf("firstDescriptionLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJsonOutputRequested(t *testing.T) {
	if !jsonOutputRequested(nil) {
		t.Fatal("nil options should default to JSON")
	}
	if !jsonOutputRequested(&globalOptions{}) {
		t.Fatal("empty options should default to JSON")
	}
	if !jsonOutputRequested(&globalOptions{json: true, output: "text"}) {
		t.Fatal("--json should force JSON")
	}
	if !jsonOutputRequested(&globalOptions{agentEnvelope: true}) {
		t.Fatal("--agent-envelope should force JSON")
	}
	if !jsonOutputRequested(&globalOptions{output: "json"}) {
		t.Fatal("--output json should be JSON")
	}
	if jsonOutputRequested(&globalOptions{output: "text"}) {
		t.Fatal("--output text should not be JSON")
	}
}

func TestUnknownFlagErrorReportsField(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "vpc", "list", "--definitely-bogus-flag")
	if code == 0 {
		t.Fatalf("unknown flag should fail")
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownCommand" {
		t.Fatalf("error.code = %#v, want UnknownCommand", errObj["code"])
	}
	if errObj["field"] != "--definitely-bogus-flag" {
		t.Fatalf("error.field = %#v, want --definitely-bogus-flag", errObj["field"])
	}
}

func TestUnknownShorthandFlagErrorReportsField(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "vpc", "list", "-Z")
	if code == 0 {
		t.Fatalf("unknown shorthand flag should fail")
	}
	errObj := errorObject(t, stdout)
	if errObj["code"] != "UnknownCommand" {
		t.Fatalf("error.code = %#v, want UnknownCommand", errObj["code"])
	}
}

func TestUnknownCommandErrorSuggestsConcreteCommand(t *testing.T) {
	stdout, _, code := runCLI("--lang", "en", "vpc", "network", "create")
	if code == 0 {
		t.Fatalf("unknown resource command should fail")
	}
	suggestion := errorSuggestion(t, stdout)
	if !strings.Contains(suggestion, "ecctl vpc create") {
		t.Fatalf("error.suggestion = %q, want concrete vpc create command; stdout=%s", suggestion, stdout)
	}
}

func TestUnknownCommandConcreteSuggestionLocalizesChinese(t *testing.T) {
	stdout, _, code := runCLI("--lang", "zh-CN", "vpc", "network", "create")
	if code == 0 {
		t.Fatalf("unknown resource command should fail")
	}
	suggestion := errorSuggestion(t, stdout)
	if !strings.Contains(suggestion, "执行 `ecctl vpc create`") {
		t.Fatalf("error.suggestion = %q, want localized concrete vpc create command; stdout=%s", suggestion, stdout)
	}
	if strings.Contains(suggestion, "Try ") || strings.Contains(suggestion, "Run ") {
		t.Fatalf("error.suggestion should be localized, got %q; stdout=%s", suggestion, stdout)
	}
}

func TestCobraErrorToAppErrorHandlesNilAndUnknownCommand(t *testing.T) {
	err := cobraErrorToAppError(nil)
	if err == nil || err.Payload().Code != "InternalError" {
		t.Fatalf("nil error should produce InternalError, got %v", err)
	}

	err = cobraErrorToAppError(fmt.Errorf("unknown command \"bogus\""))
	if err.Payload().Code != "UnknownCommand" {
		t.Fatalf("unknown command error code = %q, want UnknownCommand", err.Payload().Code)
	}
}

func TestArgsRangeTooManyArguments(t *testing.T) {
	validator := argsRange(1, 2)
	cmd := &cobra.Command{Use: "test <id>"}
	err := validator(cmd, []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("argsRange should reject too many arguments")
	}
	if !strings.Contains(err.Error(), "expected between 1 and 2") {
		t.Fatalf("error = %q, want mention of expected range", err.Error())
	}

	validator = exactArgs(1)
	err = validator(cmd, []string{"a", "b"})
	if err == nil {
		t.Fatal("exactArgs should reject wrong count")
	}
	if !strings.Contains(err.Error(), "expected 1 arguments") {
		t.Fatalf("error = %q, want mention of expected count", err.Error())
	}
}

func TestMissingPositionalNameFallsBackToArgument(t *testing.T) {
	name := missingPositionalName("test", 0)
	if name != "<argument>" {
		t.Fatalf("missingPositionalName for no positionals = %q, want <argument>", name)
	}

	name = missingPositionalName("test <id> <name>", 2)
	if name != "<argument>" {
		t.Fatalf("missingPositionalName past end = %q, want <argument>", name)
	}

	name = missingPositionalName("test <id> <name>", 1)
	if name != "<name>" {
		t.Fatalf("missingPositionalName for second arg = %q, want <name>", name)
	}
}

func TestFlagHelpPriorityCoversAllCases(t *testing.T) {
	helpFlag := &pflag.Flag{Name: "help"}
	helpFlag.Annotations = map[string][]string{cobra.BashCompOneRequiredFlag: {"true"}}
	if got := flagHelpPriority(helpFlag); got != -1 {
		t.Fatalf("help flag priority = %d, want -1", got)
	}

	requiredFlag := &pflag.Flag{Name: "required"}
	requiredFlag.Annotations = map[string][]string{flagRequiredAnnotation: {"true"}}
	if got := flagHelpPriority(requiredFlag); got != 0 {
		t.Fatalf("required flag priority = %d, want 0", got)
	}

	dryRunFlag := &pflag.Flag{Name: "dry-run"}
	if got := flagHelpPriority(dryRunFlag); got != 2 {
		t.Fatalf("dry-run flag priority = %d, want 2", got)
	}

	apiParamFlag := &pflag.Flag{Name: "api-param"}
	if got := flagHelpPriority(apiParamFlag); got != 3 {
		t.Fatalf("api-param flag priority = %d, want 3", got)
	}

	regularFlag := &pflag.Flag{Name: "output"}
	if got := flagHelpPriority(regularFlag); got != 1 {
		t.Fatalf("regular flag priority = %d, want 1", got)
	}
}

func TestFlagDefaultIsZeroValueCoversAllTypes(t *testing.T) {
	tests := []struct {
		typeName string
		defValue string
		want     bool
	}{
		{"bool", "false", true},
		{"bool", "", true},
		{"bool", "true", false},
		{"boolFunc", "false", true},
		{"duration", "0", true},
		{"duration", "0s", true},
		{"duration", "5s", false},
		{"int", "0", true},
		{"int", "10", false},
		{"int8", "0", true},
		{"int32", "0", true},
		{"int64", "0", true},
		{"uint", "0", true},
		{"uint8", "0", true},
		{"uint16", "0", true},
		{"uint32", "0", true},
		{"uint64", "0", true},
		{"count", "0", true},
		{"float32", "0", true},
		{"float64", "0", true},
		{"string", "", true},
		{"string", "foo", false},
		{"intSlice", "[]", true},
		{"intSlice", "[1]", false},
		{"stringSlice", "[]", true},
		{"stringArray", "[]", true},
		{"custom", "false", true},
		{"custom", "<nil>", true},
		{"custom", "", true},
		{"custom", "0", true},
		{"custom", "something", false},
	}

	for _, tt := range tests {
		flag := &pflag.Flag{
			DefValue: tt.defValue,
			Value:    &fakeValue{typeName: tt.typeName},
		}
		if got := flagDefaultIsZeroValue(flag); got != tt.want {
			t.Errorf("flagDefaultIsZeroValue(%s, %q) = %v, want %v", tt.typeName, tt.defValue, got, tt.want)
		}
	}
}

type fakeValue struct {
	typeName string
}

func (f *fakeValue) String() string   { return "" }
func (f *fakeValue) Set(string) error { return nil }
func (f *fakeValue) Type() string     { return f.typeName }

func TestIsRootCommandDistinguishesRootFromSubcommand(t *testing.T) {
	root := &cobra.Command{Use: "ecctl"}
	sub := &cobra.Command{Use: "vpc"}
	root.AddCommand(sub)

	if !isRootCommand(root) {
		t.Fatal("isRootCommand should return true for root")
	}
	if isRootCommand(sub) {
		t.Fatal("isRootCommand should return false for subcommand")
	}
}

func TestIsBuiltinRootCommandCoversAllBuiltins(t *testing.T) {
	builtins := []string{"call", "configure", "schema", "capabilities", "completion", "help"}
	for _, name := range builtins {
		if !isBuiltinRootCommand(name) {
			t.Errorf("isBuiltinRootCommand(%q) = false, want true", name)
		}
	}
	if isBuiltinRootCommand("vpc") {
		t.Error("isBuiltinRootCommand(vpc) = true, want false")
	}
	if isBuiltinRootCommand("guide") {
		t.Error("isBuiltinRootCommand(guide) = true, want false")
	}
}

func TestMissingRequiredFlagsReturnsNilWhenNoneMissing(t *testing.T) {
	if err := missingRequiredFlags(requiredFlag("name", false), requiredFlag("cidr", false)); err != nil {
		t.Fatalf("missingRequiredFlags should return nil when none missing, got %v", err)
	}

	err := missingRequiredFlags(requiredFlag("name", true), requiredFlag("cidr", false))
	if err == nil {
		t.Fatal("missingRequiredFlags should return error when some missing")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("error = %q, want mention of name", err.Error())
	}
}

func TestValidatePaginationRejectsBothInvalidValues(t *testing.T) {
	if err := validatePagination(0, 1); err == nil {
		t.Fatal("limit=0 should fail")
	}
	if err := validatePagination(1, 0); err == nil {
		t.Fatal("page=0 should fail")
	}
	if err := validatePagination(1, 1); err != nil {
		t.Fatalf("valid pagination should pass, got %v", err)
	}
}

func TestExplicitEmptyRegionDetectsVariousFormats(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"--region", ""}, true},
		{[]string{"--region", "cn-hangzhou"}, false},
		{[]string{"--region="}, true},
		{[]string{"--output", "json"}, false},
		{[]string{"--region"}, true},
	}
	for _, tt := range tests {
		if got := explicitEmptyRegion(tt.args); got != tt.want {
			t.Errorf("explicitEmptyRegion(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
