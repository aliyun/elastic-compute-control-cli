package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/i18n"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/spf13/cobra"
)

func TestResourceFlagsPreserveTypedAndRepeatableValues(t *testing.T) {
	for _, tt := range []struct {
		param spec.Param
		raw   string
		want  any
	}{
		{spec.Param{Type: "float"}, "1.25", 1.25},
		{spec.Param{Type: "key=value", Repeatable: true}, "key=value", []string{"key=value"}},
		{spec.Param{Type: "key=value"}, "key=value", "key=value"},
		{spec.Param{Type: "key_value"}, "key=value", "key=value"},
	} {
		cmd := &cobra.Command{}
		addResourceFlag(cmd.Flags(), spec.ResourceSpec{}, "value", tt.param, "")
		if err := cmd.Flags().Set("value", tt.raw); err != nil {
			t.Fatal(err)
		}
		got, assigned, err := flagInputValue(cmd, spec.ResourceSpec{}, "value", tt.param)
		if err != nil || !assigned || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("flag %s=%#v assigned=%t err=%v", tt.param.Type, got, assigned, err)
		}
	}
	if got, assigned, err := flagInputValue(&cobra.Command{}, spec.ResourceSpec{}, "absent", spec.Param{}); got != nil || assigned || err != nil {
		t.Fatalf("absent flag=%#v, %t, %v", got, assigned, err)
	}
}

func TestResourceOutputSelectorsHandleMissingAndCleanedValues(t *testing.T) {
	ctx := actionOutputContext{result: engine.Result{Items: []map[string]any{{"id": "one", "empty": ""}, {"id": "two"}}, NextToken: "next", Extra: map[string]any{"value": "extra", "matches": []any{}}}}
	for _, tt := range []struct {
		expression string
		want       any
	}{{"$result.returned", 2}, {"$result.next_token", "next"}, {"$result.extra.value", "extra"}} {
		if got, found := outputExpression(tt.expression, ctx); !found || got != tt.want {
			t.Fatalf("output %s=%#v, %t", tt.expression, got, found)
		}
	}
	if _, found := outputExpression("$result.extra.missing", actionOutputContext{}); found {
		t.Fatal("missing extra resolved")
	}
	selectSpec := spec.OutputSelect{From: "$result.items", SingleKey: "item", ManyKey: "items", First: true}
	key, value, found := outputSelectValue(selectSpec, ctx)
	if !found || key != "item" || !reflect.DeepEqual(value, map[string]any{"id": "one"}) {
		t.Fatalf("first cleaned output=%q, %#v, %t", key, value, found)
	}
	for _, selection := range []spec.OutputSelect{
		{From: "$result.items", Match: "$result.extra.missing", SingleKey: "item"},
		{From: "$result.items", Match: "$result.extra.matches", SingleKey: "item"},
		{From: "$result.item", SingleKey: "item"},
		{From: "$result.items", Fields: []string{"missing"}, SingleKey: "item"},
		{From: "$result.items"},
	} {
		if _, _, found := outputSelectValue(selection, ctx); found {
			t.Fatalf("missing selection produced output: %+v", selection)
		}
	}
	if _, found := outputCaptureExpression("invalid", ctx.result); found {
		t.Fatal("invalid capture expression resolved")
	}
	if _, found := outputPath(map[string]any{"id": "one"}, "id.child"); found {
		t.Fatal("scalar child resolved")
	}
	if len(firstNonNilMap(nil, nil)) != 0 {
		t.Fatal("nil fallback not empty")
	}
}

func TestResourceActionPayloadPreservesDryRunCountsAndFallbackIDs(t *testing.T) {
	r := spec.ResourceSpec{Resource: "thing"}
	for _, input := range []map[string]any{{"amount": 3}, {"ids": []any{"one", "two", "three"}}} {
		payload := resourceActionPayload(r, "create", spec.Operation{}, input, "", engine.Result{DryRun: true})
		if payload["requested_count"] != 3 || payload["available_count"] != 3 || payload["dry_run"] != "passed" {
			t.Fatalf("dry-run payload=%#v", payload)
		}
	}
	payload := resourceActionPayload(r, "delete", spec.Operation{}, nil, "", engine.Result{ID: "one", Deleted: true})
	if !reflect.DeepEqual(payload["thing"], map[string]any{"id": "one"}) {
		t.Fatalf("delete fallback=%#v", payload)
	}
	result := engine.Result{Items: []map[string]any{{"id": "one"}}, Total: 1, Actions: []ecerrors.Action{{ActionName: "Change"}}}
	payload = resourceActionPayload(r, "change", spec.Operation{}, nil, "", result)
	if payload["total"] != 1 || !reflect.DeepEqual(payload["things"], result.Items) {
		t.Fatalf("custom mutation payload=%#v", payload)
	}
	result.Actions = nil
	result.Item = result.Items[0]
	if payload := resourceActionPayload(r, "change", spec.Operation{}, nil, "", result); !reflect.DeepEqual(payload["thing"], result.Item) {
		t.Fatalf("custom item payload=%#v", payload)
	}
}

type confirmationIOFailure struct{ err error }

func (f confirmationIOFailure) Read([]byte) (int, error)  { return 0, f.err }
func (f confirmationIOFailure) Write([]byte) (int, error) { return 0, f.err }

func TestOAuthConfirmationAndTerminalIOFailures(t *testing.T) {
	cause := errors.New("terminal unavailable")
	localizer := i18n.NewLocalizer("en")
	for _, tt := range []struct {
		reader io.Reader
		writer io.Writer
	}{{nil, io.Discard}, {strings.NewReader(""), nil}, {confirmationIOFailure{cause}, io.Discard}, {strings.NewReader(""), confirmationIOFailure{cause}}} {
		if err := confirmOAuthAccount(tt.reader, tt.writer, localizer, "1111111111111111", "RAMUser"); err == nil {
			t.Fatal("unavailable confirmation accepted")
		}
	}
	if readerIsTerminal(nil) || readerIsTerminal(strings.NewReader("")) {
		t.Fatal("non-file reader is terminal")
	}
	file, err := os.CreateTemp(t.TempDir(), "io")
	if err != nil {
		t.Fatal(err)
	}
	if readerIsTerminal(file) {
		t.Fatal("regular file is terminal")
	}
	file.Close()
	if readerIsTerminal(file) || isTerminalWriter(file) {
		t.Fatal("closed file is terminal")
	}
}

func TestCallerFactoryContextAndDefaultConstruction(t *testing.T) {
	dir := t.TempDir()
	getenv := func(key string) string {
		return map[string]string{"ALIBABA_CLOUD_IGNORE_PROFILE": "true", "ALIBABA_CLOUD_ACCESS_KEY_ID": "id", "ALIBABA_CLOUD_ACCESS_KEY_SECRET": "secret"}[key]
	}
	for _, factory := range []APICallerFactory{apiCallerFactoryFromContext(context.Background()), defaultAPICallerFactory} {
		if caller, err := factory("", filepath.Join(dir, "config"), "ecs", "cn-hangzhou", getenv); err != nil || caller == nil {
			t.Fatalf("default API factory=%v", err)
		}
	}
	if caller, err := resourceCallerFactoryFromContext(context.Background())("", filepath.Join(dir, "config"), spec.ResourceSpec{Product: "ecs"}, "cn-hangzhou", getenv); err != nil || caller == nil {
		t.Fatalf("default resource factory=%v", err)
	}
	cause := errors.New("injected factory")
	apiFactory := APICallerFactory(func(string, string, string, string, func(string) string) (engine.Caller, error) { return nil, cause })
	if _, err := apiCallerFactoryFromContext(WithAPICallerFactory(context.Background(), apiFactory))("", "", "", "", nil); !errors.Is(err, cause) {
		t.Fatal("API factory context lost")
	}
	resourceFactory := ResourceCallerFactory(func(string, string, spec.ResourceSpec, string, func(string) string) (engine.Caller, error) {
		return nil, cause
	})
	if _, err := resourceCallerFactoryFromContext(WithResourceCallerFactory(context.Background(), resourceFactory))("", "", spec.ResourceSpec{}, "", nil); !errors.Is(err, cause) {
		t.Fatal("resource factory context lost")
	}
}

func TestAPIRequestAndMetadataPresentationBoundaries(t *testing.T) {
	for _, raw := range []string{"@", "@" + filepath.Join(t.TempDir(), "missing"), "null"} {
		if _, err := parseAPIRequest(raw); err == nil {
			t.Fatalf("invalid request accepted: %q", raw)
		}
	}
	if request, err := parseAPIRequest(""); err != nil || len(request) != 0 {
		t.Fatalf("empty request=%#v, %v", request, err)
	}
	for _, kind := range []string{"boolean", "float", "array", "object"} {
		if got := apiParameterPlaceholder(apiParameterSchema{Name: "Value", Type: kind}); got == nil {
			t.Fatalf("missing placeholder for %s", kind)
		}
	}
	if localizedAPIText(map[string]string{"zh-CN": "中文", "en": "English"}, "zh-Hans") != "中文" || localizedAPIText(map[string]string{"en": "English"}, "en") != "English" || localizedAPIText(nil, "en") != "" {
		t.Fatal("localized metadata fallback changed")
	}
	for _, product := range []string{"", "ecs"} {
		if err := missingAPIOperationError(product, "en"); err == nil {
			t.Fatal("missing operation accepted")
		}
	}
	for _, tt := range []struct {
		request map[string]any
		args    []string
	}{{map[string]any{"RegionId": "invalid/region"}, nil}, {nil, []string{"--region", "invalid/region"}}} {
		if _, err := resolveAPIRegion(&globalOptions{}, tt.request, tt.args); err == nil {
			t.Fatal("invalid request region accepted")
		}
	}
	if region, err := resolveAPIRegion(&globalOptions{}, nil, []string{"--region=cn-hangzhou"}); err != nil || region.Value != "cn-hangzhou" {
		t.Fatalf("passthrough region=%+v, %v", region, err)
	}
}
