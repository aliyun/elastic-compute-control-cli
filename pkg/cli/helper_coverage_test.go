package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestSplitIDsCombinesExplicitAndPositionalIDs(t *testing.T) {
	got := splitIDs([]string{"positional-1"}, " id-1, ,id-2 ")
	want := []string{"id-1", "id-2", "positional-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitIDs() = %#v, want %#v", got, want)
	}
}

func TestSplitIDsUsesPositionalIDsWhenExplicitListIsEmpty(t *testing.T) {
	got := splitIDs([]string{"id-1", "id-2"}, "")
	want := []string{"id-1", "id-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitIDs() = %#v, want %#v", got, want)
	}
}

func TestResourceAPIProductUsesSpecOverride(t *testing.T) {
	resource := spec.ResourceSpec{Product: "rg", APIProduct: "ResourceManager"}
	if got := resourceAPIProduct(resource); got != "ResourceManager" {
		t.Fatalf("resourceAPIProduct() = %q, want ResourceManager", got)
	}

	resource = spec.ResourceSpec{Product: "ecs"}
	if got := resourceAPIProduct(resource); got != "ecs" {
		t.Fatalf("resourceAPIProduct() fallback = %q, want ecs", got)
	}
}

func TestConfigValueErrorMapsKnownKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
		err  error
		code string
	}{
		{name: "unknown key", key: "bad", err: errors.New("unknown config key bad"), code: "UnknownConfigKey"},
		{name: "region", key: "region-id", err: errors.New("bad region"), code: "InvalidRegion"},
		{name: "output", key: "output_format", err: errors.New("bad output"), code: "UnsupportedOutputMode"},
		{name: "default", key: "profile", err: errors.New("bad config"), code: "InvalidConfig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configValueError(tt.key, tt.err).Payload().Code
			if got != tt.code {
				t.Fatalf("code = %q, want %q", got, tt.code)
			}
		})
	}
}

func TestRootErrorHelpersCoverSuggestionsAndFallbacks(t *testing.T) {
	if got := cobraErrorToAppErrorForLanguage(nil, "en").Payload().Code; got != "InternalError" {
		t.Fatalf("nil cobra error code = %q, want InternalError", got)
	}

	withSuggestion := cobraErrorToAppErrorForLanguage(errors.New(`unknown command "create" for "ecctl vpc"`), "en", []string{"vpc", "create"}).Payload()
	if withSuggestion.Code != "UnknownCommand" || withSuggestion.Suggestion == "" {
		t.Fatalf("unknown command suggestion payload = %#v", withSuggestion)
	}

	plain := cobraErrorToAppErrorForLanguage(errors.New("plain parse error"), "en").Payload()
	if plain.Code != "UnknownCommand" || plain.Suggestion != "" {
		t.Fatalf("plain cobra error payload = %#v", plain)
	}

	if suggestion := unknownCommandSuggestion([]string{"schema", "bogus"}, "en"); suggestion != "" {
		t.Fatalf("builtin command suggestion = %q, want empty", suggestion)
	}
	if suggestion := unknownCommandSuggestion([]string{"missing-product", "thing"}, "en"); suggestion != "" {
		t.Fatalf("unknown product suggestion = %q, want empty", suggestion)
	}
	if suggestion := unknownCommandSuggestion([]string{"ecs", "instance", "bogus"}, "en"); suggestion == "" {
		t.Fatal("unsupported resource action should suggest listing actions")
	}
	if suggestion := unknownCommandSuggestion([]string{"ecs", "instance", "list"}, "en"); suggestion != "" {
		t.Fatalf("supported resource action suggestion = %q, want empty", suggestion)
	}

	field, ok := unknownFlagField("unknown shorthand flag: 'x'")
	if !ok || field != "" {
		t.Fatalf("unknown shorthand without field = (%q, %v), want empty true", field, ok)
	}
	if containsString([]string{"one", "two"}, "three") {
		t.Fatal("containsString reported missing value as present")
	}
}

func TestRootArgumentPreScanHelpersCoverEdgeCases(t *testing.T) {
	if got := requestedBoolFlag([]string{"--json=false"}, "json"); got {
		t.Fatal("requestedBoolFlag --json=false = true, want false")
	}
	if got := requestedBoolFlag([]string{"--json=1"}, "json"); !got {
		t.Fatal("requestedBoolFlag --json=1 = false, want true")
	}

	value, ok := requestedFlagValue([]string{"--output"}, "output")
	if !ok || value != "" {
		t.Fatalf("requestedFlagValue missing value = (%q, %v), want empty true", value, ok)
	}
	value, ok = requestedFlagValue([]string{"--output=text"}, "output")
	if !ok || value != "text" {
		t.Fatalf("requestedFlagValue inline = (%q, %v), want text true", value, ok)
	}
	value, ok = requestedProfileFlagValue([]string{"--profile"})
	if !ok || value != "" {
		t.Fatalf("requestedProfileFlagValue missing value = (%q, %v), want empty true", value, ok)
	}
	value, ok = requestedProfileFlagValue([]string{"--profile=Default", "ack", "create", "--profile=worker"})
	if !ok || value != "Default" {
		t.Fatalf("requestedProfileFlagValue global inline = (%q, %v), want Default true", value, ok)
	}
	value, ok = requestedProfileFlagValue([]string{"ack", "create", "--profile=worker"})
	if ok || value != "" {
		t.Fatalf("requestedProfileFlagValue local inline = (%q, %v), want empty false", value, ok)
	}
	if !ackCreateLocalProfileFlag([]string{"--output=json", "--", "ack", "create", "--profile", "worker"}, 4) {
		t.Fatal("ackCreateLocalProfileFlag should ignore inline global flags and -- while finding ack create")
	}

	original := []string{"help", "ecs..instance"}
	if got := normalizeHelpTopicArgs(original); !reflect.DeepEqual(got, original) {
		t.Fatalf("normalizeHelpTopicArgs malformed topic = %#v, want original %#v", got, original)
	}
	if got := helpCommandArgIndex([]string{"--output=json", "-v", "help"}); got != 2 {
		t.Fatalf("helpCommandArgIndex = %d, want 2", got)
	}

	got := normalizeAPICallParameterFlags([]string{
		"call", "--region", "cn-hangzhou", "ecs", "DescribeInstances", "--PageSize", "10", "--DryRun",
	})
	want := []string{
		"call", "--region", "cn-hangzhou", "ecs", "DescribeInstances",
		"--api-param", "PageSize=10", "--api-param", "DryRun=true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAPICallParameterFlags = %#v, want %#v", got, want)
	}

	got = normalizeAPICallParameterFlags([]string{"call", "ecs", "DescribeInstances", "--=bad"})
	want = []string{"call", "ecs", "DescribeInstances", "--=bad"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAPICallParameterFlags malformed flag = %#v, want %#v", got, want)
	}
}

func TestRootOutputAndHelpHelpersCoverBranches(t *testing.T) {
	cmd := &cobra.Command{Use: "root"}
	cmd.Flags().Bool("general", false, "general flag")
	if !zhHasGeneralFlags(cmd.Flags()) {
		t.Fatal("zhHasGeneralFlags should detect a visible non-resource flag")
	}

	markFlagAnnotation(cmd, "test.annotation", "true", "missing", "general")
	flag := cmd.Flags().Lookup("general")
	if got := flag.Annotations["test.annotation"]; len(got) != 1 || got[0] != "true" {
		t.Fatalf("flag annotation = %#v, want true", flag.Annotations)
	}

	file, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer file.Close()
	t.Setenv("ECCTL_DISPLAY_MODE", "AI")
	t.Setenv("AGENT_FIRST", "")
	if shouldColorOutput(file) {
		t.Fatal("AI display output should not use color")
	}

	var out bytes.Buffer
	if err := writeSchemaOutput(&globalOptions{output: "text", forceJSON: true}, &out, map[string]any{"ok": true}); err != nil {
		t.Fatalf("writeSchemaOutput force JSON: %v", err)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("writeSchemaOutput force JSON = %q", out.String())
	}

	groupCmd := groupCommandForLanguage("demo", "Demo", "en")
	groupCmd.SetOut(&bytes.Buffer{})
	if err := groupCmd.RunE(groupCmd, nil); err != nil {
		t.Fatalf("group command help: %v", err)
	}
}

func TestSchemaAndConfigCommandsCoverErrorBranches(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code string
	}{
		{name: "unknown schema product", args: []string{"--lang", "en", "schema", "--list", "missing-product"}, code: "UnknownSchema"},
		{name: "unknown split action schema", args: []string{"--lang", "en", "schema", "ecs", "instance", "missing-action"}, code: "UnknownSchema"},
		{name: "config set arity", args: []string{"--lang", "en", "configure", "set", "region"}, code: "MissingParameter"},
		{name: "config set missing region", args: []string{"--lang", "en", "configure", "set"}, code: "MissingRegion"},
		{name: "config get unknown key", args: []string{"--lang", "en", "configure", "get", "bad-key"}, code: "UnknownConfigKey"},
		{name: "config get missing profile", args: []string{"--lang", "en", "configure", "get"}, code: "ProfileNotFound"},
		{name: "config use missing profile", args: []string{"--lang", "en", "configure", "use", "missing"}, code: "ProfileNotFound"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(tt.args...)
			if exitCode == 0 {
				t.Fatalf("%v exit 0 stderr=%s stdout=%s", tt.args, stderr, stdout)
			}
			if got := errorCode(t, stdout); got != tt.code {
				t.Fatalf("%v error.code = %q, want %q; stdout=%s", tt.args, got, tt.code, stdout)
			}
		})
	}

	stdout, stderr, code := runCLI("--lang", "en", "schema", "vpc.vpc.create", "missing.schema")
	if code != 0 {
		t.Fatalf("schema batch exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	batch := decodeObject(t, stdout)
	if batch["vpc.vpc.create"] == nil {
		t.Fatalf("schema batch missing valid command: %s", stdout)
	}
	if _, ok := batch["missing.schema"]; !ok {
		t.Fatalf("schema batch missing nil entry: %s", stdout)
	}
}

func TestRootRegionAndPaginationHelpersCoverBranches(t *testing.T) {
	t.Setenv("ECCTL_REGION", "")
	if err := requireRegion(&globalOptions{}); err == nil {
		t.Fatal("requireRegion without configured region returned nil")
	}

	payload := paginationPayload(1, 10, 10, 10, "next-token")
	if payload["has_more"] != true || payload["next_token"] != "next-token" {
		t.Fatalf("token pagination payload = %#v", payload)
	}
}

func TestRootFlagRenderingCoversOptionalForms(t *testing.T) {
	cmd := &cobra.Command{Use: "flags"}
	cmd.Flags().String("string", "default", "string usage")
	stringFlag := cmd.Flags().Lookup("string")
	stringFlag.NoOptDefVal = "fallback"
	stringFlag.Deprecated = "use --new-string"

	cmd.Flags().Bool("switch", false, "switch usage")
	switchFlag := cmd.Flags().Lookup("switch")
	switchFlag.NoOptDefVal = "false"

	cmd.Flags().Count("count", "count usage")
	countFlag := cmd.Flags().Lookup("count")
	countFlag.NoOptDefVal = "2"

	cmd.Flags().Int("size", 0, "size usage")
	sizeFlag := cmd.Flags().Lookup("size")
	sizeFlag.NoOptDefVal = "10"

	got := renderFlagList([]*pflag.Flag{stringFlag, switchFlag, countFlag, sizeFlag}, nil)
	for _, want := range []string{`--string string[="fallback"]`, "(DEPRECATED: use --new-string)", "--switch[=false]", "--count count[=2]", "--size int[=10]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderFlagList missing %q:\n%s", want, got)
		}
	}
}

func TestVerifyRegionForConfigFallbacks(t *testing.T) {
	if warning, err := verifyRegionForConfig("Default", ""); warning != "" || err != nil {
		t.Fatalf("empty region verify = warning %q err %v, want empty nil", warning, err)
	}
	if code, message, ok := appErrorCodeMessage(nil); ok || code != "" || message != "" {
		t.Fatalf("nil appErrorCodeMessage = (%q, %q, %v), want empty false", code, message, ok)
	}
	if code, message, ok := appErrorCodeMessage(errors.New("plain")); ok || code != "" || message != "" {
		t.Fatalf("plain appErrorCodeMessage = (%q, %q, %v), want empty false", code, message, ok)
	}

	restore := SetRegionVerifierFactoryForTest(func(string, string, func(string) string) (RegionVerifier, error) {
		return nil, errors.New("config broken")
	})
	defer restore()

	warning, err := verifyRegionForConfig("Default", "cn-hangzhou")
	if warning != "" || err == nil || err.Payload().Code != "InvalidConfig" {
		t.Fatalf("factory error verify = warning %q err %#v", warning, err)
	}
}

func TestShouldColorOutputFollowsDisplayModeAndNoColor(t *testing.T) {
	var stdout bytes.Buffer

	if shouldColorOutput(&stdout, true) {
		t.Fatal("noColor should disable color")
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "")
	t.Setenv("AGENT_FIRST", "1")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	setWriterIsTerminalForTest(t, true)
	if !shouldColorOutput(&stdout) {
		t.Fatal("default auto display mode should enable color for TTY output")
	}

	setWriterIsTerminalForTest(t, false)
	if shouldColorOutput(&stdout) {
		t.Fatal("default auto display mode should disable color for non-TTY output")
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "Human")
	t.Setenv("AGENT_FIRST", "1")
	if !shouldColorOutput(&stdout) {
		t.Fatal("Human display mode should enable color")
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "AI")
	t.Setenv("AGENT_FIRST", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	if shouldColorOutput(&stdout) {
		t.Fatal("AI display mode should disable color")
	}

	t.Setenv("ECCTL_DISPLAY_MODE", "unexpected")
	if shouldColorOutput(&stdout) {
		t.Fatal("unknown display mode should fall back to auto behavior")
	}
}

func TestDisplayModeRecognizesAgentAliases(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "AI", want: displayModeAI},
		{value: "ai", want: displayModeAI},
		{value: "Agent", want: displayModeAI},
		{value: "agent", want: displayModeAI},
		{value: "Human", want: displayModeHuman},
		{value: "human", want: displayModeHuman},
		{value: "auto", want: displayModeAuto},
		{value: "AUTO", want: displayModeAuto},
		{value: "", want: displayModeAuto},
		{value: "unexpected", want: displayModeAuto},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := displayMode(func(name string) string {
				if name != displayModeEnv {
					return ""
				}
				return tt.value
			})
			if got != tt.want {
				t.Fatalf("displayMode(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func setWriterIsTerminalForTest(t *testing.T, isTerminal bool) {
	t.Helper()
	previous := writerIsTerminal
	writerIsTerminal = func(io.Writer) bool {
		return isTerminal
	}
	t.Cleanup(func() {
		writerIsTerminal = previous
	})
}

func TestOutputExpressionResolvesResultInputContextAndCaptures(t *testing.T) {
	ctx := actionOutputContext{
		input:  map[string]any{"name": "web"},
		region: "us-west-1",
		result: engine.Result{
			Items:     []map[string]any{{"id": "i-1"}},
			Item:      map[string]any{"id": "i-1", "nested": map[string]any{"state": "running"}},
			Total:     1,
			RequestID: "req-1",
			ID:        "i-1",
			Deleted:   true,
			DryRun:    true,
			Captures: map[string]engine.CaptureResult{
				"rules": {
					Items:   []map[string]any{{"port": "80"}},
					Request: map[string]any{"Permissions.1.PortRange": "80/80"},
				},
			},
		},
	}

	tests := []struct {
		expr string
		want any
	}{
		{expr: "$result.items", want: ctx.result.Items},
		{expr: "$result.item", want: ctx.result.Item},
		{expr: "$result.total", want: 1},
		{expr: "$result.request_id", want: "req-1"},
		{expr: "$result.id", want: "i-1"},
		{expr: "$result.deleted", want: true},
		{expr: "$result.dry_run", want: true},
		{expr: "$context.region", want: "us-west-1"},
		{expr: "$input.name", want: "web"},
		{expr: "$result.item.nested.state", want: "running"},
		{expr: "$captures.rules.items", want: ctx.result.Captures["rules"].Items},
		{expr: "$captures.rules.request", want: ctx.result.Captures["rules"].Request},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, ok := outputExpression(tt.expr, ctx)
			if !ok {
				t.Fatalf("outputExpression(%q) returned ok=false", tt.expr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("outputExpression(%q) = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}

	for _, expr := range []string{"$input.missing", "$result.item.", "$captures", "$captures.missing.items", "$captures.rules.unknown", "$unknown"} {
		if got, ok := outputExpression(expr, ctx); ok {
			t.Fatalf("outputExpression(%q) = %#v, want ok=false", expr, got)
		}
	}
}

func TestOutputValueResolvesNestedValues(t *testing.T) {
	ctx := actionOutputContext{
		input:  map[string]any{"name": "web"},
		region: "us-west-1",
	}

	got, ok := outputValue(map[string]any{
		"name":    "$input.name",
		"missing": "$input.missing",
		"nested":  []any{"literal", "$context.region"},
	}, ctx)
	if !ok {
		t.Fatal("outputValue returned ok=false")
	}
	want := map[string]any{
		"name":   "web",
		"nested": []any{"literal", "us-west-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outputValue() = %#v, want %#v", got, want)
	}
}

func TestOutputValueHonorsBooleanConditions(t *testing.T) {
	ctx := actionOutputContext{
		input: map[string]any{
			"enabled": false,
			"skip":    false,
		},
		result: engine.Result{},
	}

	if got, ok := outputValue(map[string]any{
		"value": "visible",
		"when":  "$input.enabled",
	}, ctx); ok {
		t.Fatalf("outputValue with false when = %#v, want ok=false", got)
	}

	got, ok := outputValue(map[string]any{
		"value":  "visible",
		"unless": "$input.skip",
	}, ctx)
	if !ok || got != "visible" {
		t.Fatalf("outputValue with false unless = %#v ok=%v, want visible true", got, ok)
	}

	if got, ok := outputValue(map[string]any{
		"value": "more",
		"when":  "$result.has_more",
	}, ctx); ok {
		t.Fatalf("outputValue with false result condition = %#v, want ok=false", got)
	}
}

func TestOutputSelectValueSupportsFallbackMatches(t *testing.T) {
	ctx := actionOutputContext{
		result: engine.Result{
			Items: []map[string]any{{"id": "i-1", "name": "web"}},
			Captures: map[string]engine.CaptureResult{
				"requested": {
					Items: []map[string]any{{"id": "i-2", "name": "fallback"}},
				},
			},
		},
	}

	key, value, ok := outputSelectValue(spec.OutputSelect{
		From:                "$result.items",
		Match:               "$captures.requested.items",
		By:                  []string{"id"},
		Fields:              []string{"id", "name"},
		SingleKey:           "item",
		ManyKey:             "items",
		First:               true,
		UseMatchWhenMissing: true,
	}, ctx)
	if !ok {
		t.Fatal("outputSelectValue returned ok=false")
	}
	if key != "item" {
		t.Fatalf("key = %q, want item", key)
	}
	want := map[string]any{"id": "i-2", "name": "fallback"}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("value = %#v, want %#v", value, want)
	}
}

func TestResourceActionInputParsesArrayAndObjectJSON(t *testing.T) {
	resource := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"data_disks":  {Type: "array", Items: &spec.SchemaField{Type: "object"}},
			"system_disk": {Type: "object"},
		}},
		Operations: map[string]spec.Operation{
			"create": {Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "data_disks"}, {Name: "system_disk"}}}},
		},
	}
	operation := resource.Operations["create"]
	cmd := &cobra.Command{Use: "create"}
	addResourceActionFlags(cmd, resource, operation, "en")
	if err := cmd.Flags().Set("data-disk", `{"category":"cloud_essd","size":40}`); err != nil {
		t.Fatalf("Set data-disk: %v", err)
	}
	if err := cmd.Flags().Set("system-disk", `{"category":"cloud_essd"}`); err != nil {
		t.Fatalf("Set system-disk: %v", err)
	}

	input, _, err := resourceActionInput(cmd, resource, "create", operation, nil)
	if err != nil {
		t.Fatalf("resourceActionInput: %v", err)
	}
	dataDisks, ok := input["data_disks"].([]any)
	if !ok || len(dataDisks) != 1 {
		t.Fatalf("data_disks = %#v, want []any with one item", input["data_disks"])
	}
	disk, ok := dataDisks[0].(map[string]any)
	if !ok || disk["category"] != "cloud_essd" || disk["size"] != float64(40) {
		t.Fatalf("data disk = %#v", dataDisks[0])
	}
	systemDisk, ok := input["system_disk"].(map[string]any)
	if !ok || systemDisk["category"] != "cloud_essd" {
		t.Fatalf("system_disk = %#v", input["system_disk"])
	}
}

func TestOperationInputSpecsPreserveOrderAndMoveAPIParamLast(t *testing.T) {
	fixture := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"image":      {Type: "string"},
			"data_disks": {Type: "array"},
		}},
		Controls: map[string]spec.SchemaField{
			"api_param": {Type: "key_value", Repeatable: true},
			"dry_run":   {Type: "boolean"},
		},
		Operations: map[string]spec.Operation{
			"create": {Input: spec.OperationInput{
				Fields:   spec.OperationFields{{Name: "image", Required: true, HasRequired: true}, {Name: "data_disks"}},
				Controls: spec.OperationFields{{Name: "api_param"}, {Name: "dry_run"}},
			}},
		},
	}

	inputs, ok := operationInputSpecs(fixture, "create")
	if !ok {
		t.Fatal("operationInputSpecs returned ok=false")
	}
	got := make([]string, 0, len(inputs))
	for _, input := range inputs {
		got = append(got, input.name)
	}
	want := []string{"image", "data_disks", "dry_run", "api_param"}
	if len(got) != len(want) {
		t.Fatalf("input order = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("input order = %#v, want %#v", got, want)
		}
	}
	if !inputs[0].param.Required {
		t.Fatalf("image should be required: %#v", inputs[0].param)
	}
	if !inputs[3].param.Repeatable {
		t.Fatalf("api_param should keep control repeatable metadata: %#v", inputs[3].param)
	}
}

func TestDefaultAndInputHelpersCoverSupportedTypes(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  int
	}{
		{value: int(2), want: 2},
		{value: int64(3), want: 3},
		{value: uint64(4), want: 4},
		{value: float64(5), want: 5},
		{value: "6", want: 6},
		{value: false, want: 0},
	} {
		if got := intDefault(tt.value); got != tt.want {
			t.Fatalf("intDefault(%#v) = %d, want %d", tt.value, got, tt.want)
		}
	}

	if got := durationDefault(2 * time.Minute); got != 2*time.Minute {
		t.Fatalf("durationDefault(duration) = %s, want 2m", got)
	}
	if got := durationDefault("3m"); got != 3*time.Minute {
		t.Fatalf("durationDefault(string) = %s, want 3m", got)
	}
	if got := durationDefault(10); got != 0 {
		t.Fatalf("durationDefault(unsupported) = %s, want 0", got)
	}

	if !isInputValueEmpty([]string{}) {
		t.Fatal("empty string slice should be empty")
	}
	if isInputValueEmpty([]string{"value"}) {
		t.Fatal("non-empty string slice should not be empty")
	}
	if got := intInput(map[string]any{"limit": 0}, "limit", 50); got != 50 {
		t.Fatalf("intInput zero fallback = %d, want 50", got)
	}
	if got := intInput(map[string]any{"limit": 25}, "limit", 50); got != 25 {
		t.Fatalf("intInput value = %d, want 25", got)
	}
}

func TestStructuredObjectFlagValueParsesInlineJSONAndFiles(t *testing.T) {
	field := spec.SchemaField{
		Type: "object",
		Fields: map[string]spec.SchemaField{
			"port":    {Type: "integer", Required: true},
			"weight":  {Type: "number"},
			"enabled": {Type: "boolean"},
			"mode":    {Type: "string", Enum: []string{"allow", "deny"}},
		},
	}

	got, assigned, err := structuredObjectFlagValue("port=80, weight=1.5, enabled=true, mode=allow", "rule", field, true)
	if err != nil {
		t.Fatalf("structuredObjectFlagValue inline: %v", err)
	}
	if !assigned {
		t.Fatal("assigned = false, want true")
	}
	want := map[string]any{"port": 80, "weight": 1.5, "enabled": true, "mode": "allow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inline object = %#v, want %#v", got, want)
	}

	got, _, err = structuredObjectFlagValue(`{"port":443,"enabled":false,"mode":"deny"}`, "rule", field, true)
	if err != nil {
		t.Fatalf("structuredObjectFlagValue JSON: %v", err)
	}
	want = map[string]any{"port": 443, "enabled": false, "mode": "deny"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON object = %#v, want %#v", got, want)
	}

	file := t.TempDir() + "/rule.json"
	if err := os.WriteFile(file, []byte(`{"port":22,"mode":"allow"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _, err = structuredObjectFlagValue("@"+file, "rule", field, true)
	if err != nil {
		t.Fatalf("structuredObjectFlagValue @file: %v", err)
	}
	want = map[string]any{"port": 22, "mode": "allow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("@file object = %#v, want %#v", got, want)
	}

	for _, raw := range []string{"", "port=", "port=abc", "missing=1", `[]`, `{"port":80,"mode":"drop"}`} {
		t.Run(raw, func(t *testing.T) {
			value, _, err := structuredObjectFlagValue(raw, "rule", field, true)
			if raw == "" {
				if err != nil || value != nil {
					t.Fatalf("empty value = %#v err=%v, want nil nil", value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("structuredObjectFlagValue(%q) returned nil error", raw)
			}
		})
	}
}

func TestJSONAndStringFlagValuesCoverFileAndValidationBranches(t *testing.T) {
	value, assigned, err := jsonFlagValue(`[{"id":"i-1"}]`, "ids", "array", true)
	if err != nil {
		t.Fatalf("jsonFlagValue array: %v", err)
	}
	if !assigned || len(value.([]any)) != 1 {
		t.Fatalf("json array = %#v assigned=%v", value, assigned)
	}
	if _, _, err := jsonFlagValue(`{"id":"i-1"}`, "ids", "array", true); err == nil {
		t.Fatal("jsonFlagValue accepted object as array")
	}
	if _, _, err := jsonFlagValue(`[1]`, "metadata", "object", true); err == nil {
		t.Fatal("jsonFlagValue accepted array as object")
	}

	file := t.TempDir() + "/user-data.txt"
	if err := os.WriteFile(file, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, assigned, err := stringFlagValue("@"+file, "user-data", spec.Param{Use: "@file"}, true)
	if err != nil {
		t.Fatalf("stringFlagValue @file: %v", err)
	}
	if got != "payload" || !assigned {
		t.Fatalf("stringFlagValue = %#v assigned=%v", got, assigned)
	}
	if _, _, err := stringFlagValue("@", "user-data", spec.Param{Use: "@file"}, true); err == nil {
		t.Fatal("stringFlagValue accepted empty @file path")
	}
}

func TestValidateResourceActionInputCoversOperationRules(t *testing.T) {
	resource := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"id":          {Type: "string", Positional: true},
			"name":        {Type: "string"},
			"description": {Type: "string"},
			"tag":         {Type: "string"},
		}},
		Controls: map[string]spec.SchemaField{
			"force": {Type: "boolean"},
		},
	}
	requireAnyOperation := spec.Operation{
		Input: spec.OperationInput{
			Fields: spec.OperationFields{{Name: "id", Positional: true}, {Name: "name"}},
		},
		RequireAny: []spec.Requirement{{Raw: "$.name"}},
	}
	if err := validateResourceActionInput(resource, "create", requireAnyOperation, map[string]any{}); err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("missing require_any err = %v", err)
	}
	if err := validateResourceActionInput(resource, "create", requireAnyOperation, map[string]any{"name": "web"}); err != nil {
		t.Fatalf("validate require_any valid input: %v", err)
	}

	conflictOperation := spec.Operation{
		Input:     spec.OperationInput{Fields: spec.OperationFields{{Name: "name"}, {Name: "description"}}},
		Conflicts: []spec.Conflict{{Any: []string{"name"}, WithAny: []string{"description"}}},
	}
	if err := validateResourceActionInput(resource, "create", conflictOperation, map[string]any{"name": "web", "description": "desc"}); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflict err = %v", err)
	}

	requireWhenOperation := spec.Operation{
		Input:       spec.OperationInput{Fields: spec.OperationFields{{Name: "description"}}, Controls: spec.OperationFields{{Name: "force"}}},
		RequireWhen: []spec.ConditionalRequirement{{WhenAny: []string{"force"}, RequireAny: []string{"description"}}},
	}
	if err := validateResourceActionInput(resource, "create", requireWhenOperation, map[string]any{"force": true}); err == nil || !strings.Contains(err.Error(), "--description") {
		t.Fatalf("require_when err = %v", err)
	}
	if err := validateResourceActionInput(resource, "create", requireWhenOperation, map[string]any{"description": "desc", "force": true}); err != nil {
		t.Fatalf("validateResourceActionInput valid input: %v", err)
	}

	duplicateRequireWhenOperation := spec.Operation{
		Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "name"}, {Name: "description"}}, Controls: spec.OperationFields{{Name: "force"}}},
		RequireWhen: []spec.ConditionalRequirement{
			{WhenAny: []string{"force"}, RequireAny: []string{"name", "description"}},
			{WhenAny: []string{"force"}, RequireAny: []string{"name"}},
		},
	}
	if err := validateResourceActionInput(resource, "create", duplicateRequireWhenOperation, map[string]any{"force": true}); err == nil || strings.Count(err.Error(), "--name") != 1 || !strings.Contains(err.Error(), "--description") {
		t.Fatalf("deduped require_when err = %v", err)
	}

	updateOperation := spec.Operation{Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "id", Positional: true}, {Name: "tag"}}}}
	if err := validateResourceActionInput(resource, "update", updateOperation, map[string]any{"id": "i-1"}); err == nil || !strings.Contains(err.Error(), "--tag") {
		t.Fatalf("update missing mutable field err = %v", err)
	}
	if err := validateResourceActionInput(resource, "update", updateOperation, map[string]any{"id": "i-1", "tag": "prod"}); err != nil {
		t.Fatalf("update valid input: %v", err)
	}
}

func TestApplyFilterValueRejectsDuplicateExplicitTargets(t *testing.T) {
	resource := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"cluster_type": {Type: "string"},
		}},
		Controls: map[string]spec.SchemaField{
			"dry_run": {Type: "boolean"},
		},
	}

	input := map[string]any{"cluster_type": "ManagedKubernetes"}
	err := applyFilterValue(resource, input, spec.Filter{Target: "cluster_type"}, "", "cluster-type", "ExternalKubernetes")
	if err == nil || !strings.Contains(err.Error(), "--cluster-type") || !strings.Contains(err.Error(), "--filter") {
		t.Fatalf("duplicate field filter err = %v", err)
	}

	input = map[string]any{"dry_run": true}
	err = applyFilterValue(resource, input, spec.Filter{Target: "dry_run"}, "", "dry-run", "false")
	if err == nil || !strings.Contains(err.Error(), "--dry-run") {
		t.Fatalf("duplicate control filter err = %v", err)
	}

	if got := filterTargetDisplayName(resource, "custom_target"); got != "--custom-target" {
		t.Fatalf("unknown filter target display = %q", got)
	}
}

func TestResourceActionPayloadCoversDryRunCustomOutputAndDefaults(t *testing.T) {
	resource := spec.ResourceSpec{
		Resource: "instance",
		Identity: spec.Identity{OutputRoot: spec.OutputRoot{
			One:  "instance",
			Many: "instances",
		}},
	}
	result := engine.Result{
		Actions: []ecerrors.Action{{ActionName: "RunInstances"}},
		ID:      "i-1",
		Item:    map[string]any{"id": "i-1", "name": "web"},
		Items:   []map[string]any{{"id": "i-1"}},
		Total:   1,
		DryRun:  true,
	}

	payload := resourceActionPayload(resource, "delete", spec.Operation{}, map[string]any{"ids": []string{"i-1", "i-2"}}, "cn-hangzhou", result)
	if payload["dry_run"] != "passed" || payload["requested_count"] != 2 {
		t.Fatalf("delete dry-run payload = %#v", payload)
	}

	output := spec.OperationOutput{
		Fields: map[string]any{
			"id":      "$result.id",
			"region":  "$context.region",
			"literal": "ok",
		},
		Select: []spec.OutputSelect{{
			From:      "$result.items",
			Fields:    []string{"id"},
			SingleKey: "instance",
			ManyKey:   "instances",
		}},
	}
	payload = resourceActionPayload(resource, "create", spec.Operation{Output: output}, map[string]any{}, "cn-hangzhou", result)
	if payload["id"] != "i-1" || payload["region"] != "cn-hangzhou" || payload["literal"] != "ok" {
		t.Fatalf("custom output payload = %#v", payload)
	}
	if payload["instance"] == nil {
		t.Fatalf("custom output should include selected instance: %#v", payload)
	}

	payload = resourceActionPayload(resource, "custom", spec.Operation{}, map[string]any{}, "cn-hangzhou", engine.Result{Items: result.Items, Total: 1})
	if payload["total"] != 1 || len(payload["instances"].([]map[string]any)) != 1 {
		t.Fatalf("default custom payload = %#v", payload)
	}

	payload = resourceActionPayload(resource, "list", spec.Operation{}, map[string]any{}, "cn-hangzhou", engine.Result{})
	instances, ok := payload["instances"].([]map[string]any)
	if !ok || len(instances) != 0 {
		t.Fatalf("empty list payload should expose an empty array, got %#v", payload["instances"])
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal empty list payload: %v", err)
	}
	if strings.Contains(string(raw), `"instances":null`) {
		t.Fatalf("empty list payload marshaled null: %s", raw)
	}
}

func TestUpdateAPIParamRunsBindingHonorsWorkflowConditions(t *testing.T) {
	resource := spec.ResourceSpec{Bindings: map[string]spec.Binding{
		"apply": {
			Request: map[string]any{
				"Tags": []any{map[string]any{"Value": "json($input.api_param)"}},
			},
		},
		"ignore": {
			Request: map[string]any{"Name": "$.name"},
		},
	}}
	operation := spec.Operation{Workflow: []spec.WorkflowStep{
		{Binding: "missing"},
		{Binding: "ignore"},
		{Binding: "apply", When: "$.enabled", Unless: "$.skip"},
	}}

	if !updateAPIParamRunsBinding(resource, operation, map[string]any{
		"api_param": []string{"Tag.1.Key=env"},
		"enabled":   true,
	}) {
		t.Fatal("updateAPIParamRunsBinding should find runnable api_param binding")
	}
	if updateAPIParamRunsBinding(resource, operation, map[string]any{
		"api_param": []string{"Tag.1.Key=env"},
		"enabled":   true,
		"skip":      true,
	}) {
		t.Fatal("updateAPIParamRunsBinding should honor unless")
	}
	if updateAPIParamRunsBinding(resource, operation, map[string]any{}) {
		t.Fatal("updateAPIParamRunsBinding should ignore empty api_param")
	}
}

func TestBindingUsesInputCoversNestedValues(t *testing.T) {
	if !bindingUsesInput("$.api_param", "api_param") {
		t.Fatal("bindingUsesInput should match short expression")
	}
	if !bindingUsesInput("$input.api_param)", "api_param") {
		t.Fatal("bindingUsesInput should match function argument expression")
	}
	if !bindingUsesInput(map[string]any{"Nested": []any{"literal", "$.api_param)"}}, "api_param") {
		t.Fatal("bindingUsesInput should match nested values")
	}
	if bindingUsesInput(map[string]any{"Nested": []any{"literal", 1}}, "api_param") {
		t.Fatal("bindingUsesInput should reject missing input")
	}
}

func TestPaginationAndInputStyleHelpers(t *testing.T) {
	payload := operationPaginationPayload(spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "next_token"}}},
	}, map[string]any{"limit": 20}, engine.Result{Items: []map[string]any{{"id": "i-1"}}, NextToken: "token-2"})
	if payload["has_more"] != true || payload["next_token"] != "token-2" || payload["returned"] != 1 {
		t.Fatalf("token pagination payload = %#v", payload)
	}

	if got := tokenPaginationPayload(10, 0, ""); got["has_more"] != false {
		t.Fatalf("empty token payload = %#v", got)
	}

	param := spec.Param{Use: "+value|-value"}
	if err := validateParamInputStyle("tag", param, []any{"+env", "-team", 7}); err != nil {
		t.Fatalf("validateParamInputStyle valid slice: %v", err)
	}
	if err := validateParamInputStyle("tag", param, []string{"env"}); err == nil {
		t.Fatal("validateParamInputStyle accepted unsigned value")
	}
	if got := inputStringValues("one"); len(got) != 1 || got[0] != "one" {
		t.Fatalf("inputStringValues string = %#v", got)
	}
	if got := inputStringValues(1); got != nil {
		t.Fatalf("inputStringValues unsupported = %#v, want nil", got)
	}
}

func TestListPayloadIncludesTotalOnlyWhenProbeDeclaresIt(t *testing.T) {
	resource := spec.ResourceSpec{
		Product:  "ecs",
		Resource: "instance",
		Identity: spec.Identity{OutputRoot: spec.OutputRoot{Many: "instances"}},
	}
	operation := spec.Operation{
		Input: spec.OperationInput{Controls: spec.OperationFields{{Name: "limit"}, {Name: "next_token"}}},
	}

	withoutTotal := engine.Result{Total: 7}
	payload := resourceActionPayload(resource, "list", operation, map[string]any{"limit": 100}, "cn-hangzhou", withoutTotal)
	if _, ok := payload["total"]; ok {
		t.Fatalf("token list without a declared total must omit total: %#v", payload)
	}

	withTotal := engine.Result{Total: 0, HasTotal: true}
	payload = resourceActionPayload(resource, "list", operation, map[string]any{"limit": 100}, "cn-hangzhou", withTotal)
	if total, ok := payload["total"]; !ok || total != 0 {
		t.Fatalf("declared zero total must remain distinguishable from an unavailable total: %#v", payload)
	}
}

func TestFloatDefaultCoversSupportedTypes(t *testing.T) {
	tests := []struct {
		value any
		want  float64
	}{
		{value: float64(1.5), want: 1.5},
		{value: float32(2.5), want: 2.5},
		{value: int(3), want: 3},
		{value: int64(4), want: 4},
		{value: "5.5", want: 5.5},
		{value: false, want: 0},
	}
	for _, tt := range tests {
		if got := floatDefault(tt.value); got != tt.want {
			t.Fatalf("floatDefault(%#v) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestOperationInputSpecsFromOperationAndPositionals(t *testing.T) {
	operation := spec.Operation{Input: spec.OperationInput{
		Fields: spec.OperationFields{
			{Name: "id", Positional: true, Required: true, HasRequired: true},
			{Name: "name"},
		},
		Controls: spec.OperationFields{{Name: "force", InputStyle: "+value|-value"}},
	}}

	inputs := operationInputSpecsFromOperation(operation)
	if len(inputs) != 3 || inputs[0].name != "id" || inputs[2].name != "force" {
		t.Fatalf("operationInputSpecsFromOperation = %#v", inputs)
	}
	if !inputs[0].param.Positional || !inputs[0].param.Required || inputs[2].param.Use != "+value|-value" {
		t.Fatalf("operation input params = %#v", inputs)
	}
	if got := positionalParamNames(operation); !reflect.DeepEqual(got, []string{"id"}) {
		t.Fatalf("positionalParamNames = %#v", got)
	}
	if got := positionalInputNames(inputs); !reflect.DeepEqual(got, []string{"id"}) {
		t.Fatalf("positionalInputNames = %#v", got)
	}
}

func TestResourceHelperBranches(t *testing.T) {
	errCmd := resourceSpecErrorCommand("bad", "Bad", errors.New("broken spec"))
	err := errCmd.RunE(errCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "InvalidResourceSpec") {
		t.Fatalf("resourceSpecErrorCommand err = %v", err)
	}

	resource := spec.ResourceSpec{
		Schema: spec.ResourceSchema{Fields: map[string]spec.SchemaField{
			"data_disks": {Type: "array", Items: &spec.SchemaField{Type: "object"}},
			"name":       {Type: "string"},
		}},
		Controls: map[string]spec.SchemaField{"dry_run": {Type: "boolean"}},
	}
	if got := resourceInputType(resource, "dry_run"); got != "boolean" {
		t.Fatalf("resourceInputType control = %q", got)
	}
	param, ok := updateInputParam(resource, spec.Operation{Input: spec.OperationInput{Fields: spec.OperationFields{{Name: "name"}}}}, "name")
	if !ok || param.Type != "string" {
		t.Fatalf("updateInputParam = %#v, %v", param, ok)
	}
	if _, ok := updateInputParam(resource, spec.Operation{}, "missing"); ok {
		t.Fatal("updateInputParam missing returned ok=true")
	}
	if got := singularInputName("policies"); got != "policy" {
		t.Fatalf("singularInputName policies = %q", got)
	}
	if got := singularInputName("classes"); got != "class" {
		t.Fatalf("singularInputName classes = %q", got)
	}
	if got := resourceCLIFlagName(resource, "data_disks", spec.Param{Type: "array"}); got != "data-disk" {
		t.Fatalf("resourceCLIFlagName object array = %q", got)
	}
	if got := schemaFieldForInput(resource, "dry_run"); got.Type != "boolean" {
		t.Fatalf("schemaFieldForInput control = %#v", got)
	}
}

func TestOutputMapItemsAndExtraHelpers(t *testing.T) {
	items := outputMapItems([]any{map[string]any{"id": "i-1"}, "bad"})
	if len(items) != 1 || items[0]["id"] != "i-1" {
		t.Fatalf("outputMapItems []any = %#v", items)
	}
	items = outputMapItems(map[string]any{"id": "i-2"})
	if len(items) != 1 || items[0]["id"] != "i-2" {
		t.Fatalf("outputMapItems map = %#v", items)
	}
	if got := outputMapItems("bad"); got != nil {
		t.Fatalf("outputMapItems unsupported = %#v", got)
	}

	payload := withResultExtra(map[string]any{"kept": "payload"}, engine.Result{Extra: map[string]any{
		"kept":    "result",
		"empty":   "",
		"new_key": "value",
	}})
	if payload["kept"] != "payload" || payload["new_key"] != "value" {
		t.Fatalf("withResultExtra = %#v", payload)
	}
}

// --- BC22b / BC22c regression coverage ----------------------------------------
//
// These tests pin down the two highest-impact P0 invariants that BC22b and
// BC22c assert through the CLI subprocess (and skip without ECCTL_LIVE_ECS):
//
//   1. nil result.Items must surface as `[]` not `null` in JSON payloads.
//   2. result.Capabilities (e.g. ["auto_wait"]) must propagate to the payload
//      under `ecctl_capabilities_used`.
//
// They drive resourceActionPayload + outputResourceActionPayload directly so
// the contract is exercised in CI without any cloud credentials.

func TestResourceActionPayloadEmptyListReturnsArrayNotNil(t *testing.T) {
	resource := spec.ResourceSpec{Product: "ecs", Resource: "image"}
	operation := spec.Operation{}
	result := engine.Result{Items: nil}

	payload := resourceActionPayload(resource, "list", operation, map[string]any{}, "cn-beijing", result)

	images, ok := payload["images"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[\"images\"] type = %T (%v), want []map[string]any", payload["images"], payload["images"])
	}
	if images == nil {
		t.Fatalf("payload[\"images\"] is nil, want non-nil empty slice for JSON `[]` rendering")
	}
	if len(images) != 0 {
		t.Fatalf("payload[\"images\"] = %#v, want empty", images)
	}
}

func TestResourceActionPayloadDefaultBranchEmptyItemsFallback(t *testing.T) {
	// Default branch (action != list/create/update/delete/get and no
	// result.Actions): nil result.Items must still surface as `[]`.
	resource := spec.ResourceSpec{Product: "ecs", Resource: "image"}
	operation := spec.Operation{}
	result := engine.Result{Items: nil}

	payload := resourceActionPayload(resource, "snapshot", operation, map[string]any{}, "cn-beijing", result)

	images, ok := payload["images"].([]map[string]any)
	if !ok || images == nil {
		t.Fatalf("default branch: payload[\"images\"] = %#v (%T), want non-nil []map[string]any", payload["images"], payload["images"])
	}
}

func TestResourceActionPayloadCreateAttachesAutoWaitCapability(t *testing.T) {
	resource := spec.ResourceSpec{Product: "ecs", Resource: "instance"}
	operation := spec.Operation{}
	result := engine.Result{
		Item:         map[string]any{"id": "i-new"},
		Capabilities: []string{"auto_wait"},
	}

	payload := resourceActionPayload(resource, "create", operation, map[string]any{}, "cn-beijing", result)

	got, ok := payload["ecctl_capabilities_used"].([]string)
	if !ok {
		t.Fatalf("ecctl_capabilities_used type = %T (%v), want []string", payload["ecctl_capabilities_used"], payload["ecctl_capabilities_used"])
	}
	found := false
	for _, c := range got {
		if c == "auto_wait" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ecctl_capabilities_used = %v, want to contain auto_wait", got)
	}
}

func TestResourceActionPayloadDefaultWithActionsAttachesCapability(t *testing.T) {
	resource := spec.ResourceSpec{Product: "ack", Resource: "nodepool"}
	operation := spec.Operation{}
	result := engine.Result{
		Item:         map[string]any{"id": "np-1"},
		Actions:      []ecerrors.Action{{ActionName: "wait_for_state"}},
		Capabilities: []string{"auto_wait"},
	}

	payload := resourceActionPayload(resource, "attach", operation, map[string]any{}, "cn-beijing", result)

	if _, ok := payload["ecctl_capabilities_used"]; !ok {
		t.Fatalf("default-with-actions branch must propagate ecctl_capabilities_used; payload=%#v", payload)
	}
}

// H-1 regression: the explicit-output path must also propagate capabilities.
// Operations that declare output.fields/select bypass the canonical case
// branches and previously dropped result.Capabilities silently.
func TestOutputResourceActionPayloadAttachesAutoWaitCapability(t *testing.T) {
	operation := spec.Operation{
		Output: spec.OperationOutput{
			Fields: map[string]any{
				"id": "$result.id",
			},
		},
	}
	result := engine.Result{
		ID:           "cmd-1",
		Capabilities: []string{"auto_wait"},
	}

	payload := outputResourceActionPayload(operation, map[string]any{}, "cn-beijing", result)

	caps, ok := payload["ecctl_capabilities_used"].([]string)
	if !ok {
		t.Fatalf("ecctl_capabilities_used type = %T (%v), want []string", payload["ecctl_capabilities_used"], payload["ecctl_capabilities_used"])
	}
	found := false
	for _, c := range caps {
		if c == "auto_wait" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ecctl_capabilities_used = %v, want to contain auto_wait", caps)
	}
}

// H-2 regression: $result.items in explicit output must surface as [] when nil.
func TestOutputExpressionResultItemsNilFallsBackToEmptySlice(t *testing.T) {
	ctx := actionOutputContext{
		result: engine.Result{Items: nil},
	}

	got, ok := outputExpression("$result.items", ctx)
	if !ok {
		t.Fatalf("outputExpression(\"$result.items\") returned ok=false")
	}
	items, isSlice := got.([]map[string]any)
	if !isSlice {
		t.Fatalf("outputExpression returned %T (%v), want []map[string]any", got, got)
	}
	if items == nil {
		t.Fatalf("outputExpression(\"$result.items\") with nil Items returned nil slice; expected non-nil empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("outputExpression(\"$result.items\") = %#v, want empty", items)
	}
}

// Combined: explicit-output path with $result.items projection must surface
// `[]` not null AND attach capabilities — covers the rg.policy.list shape.
func TestOutputResourceActionPayloadEmptyItemsAndCapabilityCombo(t *testing.T) {
	operation := spec.Operation{
		Output: spec.OperationOutput{
			Fields: map[string]any{
				"policies": "$result.items",
			},
		},
	}
	result := engine.Result{
		Items:        nil,
		Capabilities: []string{"auto_wait"},
	}

	payload := outputResourceActionPayload(operation, map[string]any{}, "cn-hangzhou", result)

	policies, ok := payload["policies"].([]map[string]any)
	if !ok || policies == nil {
		t.Fatalf("policies = %#v (%T), want non-nil []map[string]any", payload["policies"], payload["policies"])
	}
	if _, ok := payload["ecctl_capabilities_used"]; !ok {
		t.Fatalf("explicit-output payload missing ecctl_capabilities_used; payload=%#v", payload)
	}
}
