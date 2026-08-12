package aliyun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

type ossUtilRunnerResult struct {
	stdout []byte
	stderr []byte
	err    error
}

type ossUtilRunnerCall struct {
	name string
	args []string
	env  []string
}

type fakeOSSUtilRunner struct {
	results []ossUtilRunnerResult
	calls   []ossUtilRunnerCall
}

func (r *fakeOSSUtilRunner) Run(_ context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
	r.calls = append(r.calls, ossUtilRunnerCall{
		name: name,
		args: append([]string(nil), args...),
		env:  append([]string(nil), env...),
	})
	if len(r.results) == 0 {
		return nil, nil, errors.New("unexpected runner call")
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result.stdout, result.stderr, result.err
}

func TestOSSUtilMetadataDefinesCallSurface(t *testing.T) {
	product, ok := OpenAPIProductByCode("OSS", "en")
	if !ok {
		t.Fatal("OSS product metadata is missing")
	}
	wantOperations := []string{"DeleteBucket", "DeleteObject", "GetBucketAcl", "GetBucketInfo", "GetObject", "ListBuckets", "ListObjects", "PutBucket", "PutObject"}
	if !reflect.DeepEqual(product.APINames, wantOperations) {
		t.Fatalf("OSS operations = %#v, want %#v", product.APINames, wantOperations)
	}
	operation, ok := OpenAPIOperationName(product, "putbucket")
	if !ok || operation != "PutBucket" {
		t.Fatalf("case-insensitive operation = %q, %v", operation, ok)
	}
	detail, ok := OpenAPIOperationDetailFor("en", product, "PutBucket")
	if !ok {
		t.Fatal("PutBucket detail is missing")
	}
	if bucket := detail.FindParameter("Bucket"); bucket == nil || !bucket.Required || bucket.Type != "String" {
		t.Fatalf("Bucket parameter = %#v", bucket)
	}
	if config := detail.FindParameter("CreateBucketConfiguration"); config == nil || config.Type != "Object" {
		t.Fatalf("CreateBucketConfiguration parameter = %#v", config)
	}
	listDetail, ok := OpenAPIOperationDetailFor("en", product, "ListObjects")
	if !ok {
		t.Fatal("ListObjects detail is missing")
	}
	if fetchOwner := listDetail.FindParameter("FetchOwner"); fetchOwner == nil || fetchOwner.Type != "Boolean" || fetchOwner.Required {
		t.Fatalf("FetchOwner parameter = %#v", fetchOwner)
	}
	deleteDetail, ok := OpenAPIOperationDetailFor("en", product, "DeleteObject")
	if !ok {
		t.Fatal("DeleteObject detail is missing")
	}
	if key := deleteDetail.FindParameter("Key"); key == nil || !key.Required || key.Type != "String" {
		t.Fatalf("Key parameter = %#v", key)
	}
	if bypass := deleteDetail.FindParameter("BypassGovernanceRetention"); bypass == nil || bypass.Type != "Boolean" || bypass.Position != "Header" {
		t.Fatalf("BypassGovernanceRetention parameter = %#v", bypass)
	}
	getObject, ok := OpenAPIOperationDetailFor("en", product, "GetObject")
	if !ok {
		t.Fatal("GetObject detail is missing")
	}
	if file := getObject.FindParameter("File"); file == nil || !file.Required || file.Position != "Local" {
		t.Fatalf("GetObject File parameter = %#v", file)
	}
	if force := getObject.FindParameter("Force"); force == nil || force.Required || force.Type != "Boolean" || force.Position != "Local" {
		t.Fatalf("GetObject Force parameter = %#v", force)
	}
	putObject, ok := OpenAPIOperationDetailFor("en", product, "PutObject")
	if !ok {
		t.Fatal("PutObject detail is missing")
	}
	if force := putObject.FindParameter("Force"); force == nil || force.Required || force.Type != "Boolean" || force.Position != "Local" {
		t.Fatalf("PutObject Force parameter = %#v", force)
	}
	summary, ok := OpenAPIOperationSummaryFor("zh-CN", "oss", "GetBucketAcl")
	if !ok || !strings.Contains(summary.Summary, "判断") {
		t.Fatalf("GetBucketAcl Chinese summary = %#v, %v", summary, ok)
	}
}

func TestOSSUtilCallerMapsRequestAndUsesCredentialEnvironment(t *testing.T) {
	for key, value := range map[string]string{
		"ALIBABACLOUD_ACCESS_KEY_ID": "ambient-ak",
		"ALICLOUD_ACCESS_KEY_SECRET": "ambient-secret",
		"SECURITY_TOKEN":             "ambient-token",
		"ALIBABA_CLOUD_PROFILE":      "ambient-profile",
		"OSSUTIL_CONFIG_VALUE":       "ambient-config",
		"OSS_ACCESS_KEY_ID":          "ambient-oss-ak",
		"OSS_ACCESS_KEY_SECRET":      "ambient-oss-secret",
		"ALIBABA_CLOUD_ROLE_ARN":     "ambient-role",
		"OSS_ROLE_ARN":               "ambient-oss-role",
	} {
		t.Setenv(key, value)
	}
	runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
		{stdout: []byte("2.3.0\n")},
		{stdout: []byte("PutBucket help\n")},
		{stdout: []byte("{\"RequestId\":\"req-1\"}\n0.001234(s) elapsed\n")},
	}}
	caller := newTestOSSUtilCaller(t, runner, "")
	response, err := caller.CallWithArgs(context.Background(), "PutBucket", map[string]any{
		"Bucket":          "ecctl-test-bucket",
		"ACL":             "private",
		"ResourceGroupId": "rg-1",
		"CreateBucketConfiguration": map[string]any{
			"StorageClass":       "Standard",
			"DataRedundancyType": "LRS",
		},
	}, []string{"--endpoint", "oss-cn-hangzhou.aliyuncs.com", "--retry-count", "3"})
	if err != nil {
		t.Fatalf("CallWithArgs: %v", err)
	}
	if response["RequestId"] != "req-1" {
		t.Fatalf("response = %#v", response)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("runner calls = %d, want 3", len(runner.calls))
	}
	if runner.calls[0].name != "/test/ossutil" || !reflect.DeepEqual(runner.calls[0].args, []string{"version"}) {
		t.Fatalf("direct version call = %#v", runner.calls[0])
	}
	if runner.calls[1].name != "/test/ossutil" || !reflect.DeepEqual(runner.calls[1].args, []string{"api", "put-bucket", "--help"}) {
		t.Fatalf("direct API help call = %#v", runner.calls[1])
	}
	wantArgs := []string{
		"--auto-plugin-install", "false", "ossutil", "api", "put-bucket",
		"--bucket", "ecctl-test-bucket",
		"--acl", "private",
		"--resource-group-id", "rg-1",
		"--create-bucket-configuration", `{"DataRedundancyType":"LRS","StorageClass":"Standard"}`,
		"--endpoint", "oss-cn-hangzhou.aliyuncs.com",
		"--retry-times", "3",
		"--region", "cn-hangzhou",
		"--output-format", "json",
	}
	if runner.calls[2].name != "aliyun" {
		t.Fatalf("launcher = %q, want aliyun", runner.calls[2].name)
	}
	if got := runner.calls[2].args; !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("api args = %#v, want %#v", got, wantArgs)
	}
	joinedArgs := strings.Join(runner.calls[2].args, " ")
	for _, secret := range []string{"test-ak", "test-secret", "test-token"} {
		if strings.Contains(joinedArgs, secret) {
			t.Fatalf("credential %q leaked into argv: %#v", secret, runner.calls[2].args)
		}
	}
	wantEnv := map[string]string{
		"ALIBABA_CLOUD_IGNORE_PROFILE":    "TRUE",
		"ALIBABA_CLOUD_ACCESS_KEY_ID":     "test-ak",
		"ALIBABA_CLOUD_ACCESS_KEY_SECRET": "test-secret",
		"ALIBABA_CLOUD_SECURITY_TOKEN":    "test-token",
		"ALIBABA_CLOUD_REGION_ID":         "cn-hangzhou",
		"OSS_ACCESS_KEY_ID":               "test-ak",
		"OSS_ACCESS_KEY_SECRET":           "test-secret",
		"OSS_SESSION_TOKEN":               "test-token",
		"OSS_REGION":                      "cn-hangzhou",
	}
	for key, want := range wantEnv {
		if got := environmentValue(runner.calls[2].env, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"ALIBABACLOUD_ACCESS_KEY_ID", "ALICLOUD_ACCESS_KEY_SECRET", "SECURITY_TOKEN", "ALIBABA_CLOUD_PROFILE", "ALIBABA_CLOUD_ROLE_ARN", "OSS_ROLE_ARN", "OSSUTIL_CONFIG_VALUE"} {
		if environmentHasKey(runner.calls[2].env, key) {
			t.Fatalf("ambient identity key %s leaked into child environment", key)
		}
	}
}

func TestOSSUtilCallerRejectsUnsupportedRequestAndPassthrough(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	if _, _, err := caller.commandArgs("PutBucket", map[string]any{"Bucket": "bucket", "DryRun": true}, nil); appErrorCode(err) != "UnsupportedOSSParameter" {
		t.Fatalf("DryRun error = %v", err)
	}
	for _, passthrough := range [][]string{
		{"--dryrun"},
		{"--access-key-id", "other-ak"},
		{"--config-path", "/tmp/config"},
		{"--output", "yaml"},
		{"--waiter", "expr"},
		{"--skip-secure-verify"},
	} {
		if _, _, err := caller.commandArgs("ListBuckets", nil, passthrough); appErrorCode(err) != "UnsupportedOSSParameter" {
			t.Fatalf("passthrough %#v error = %v", passthrough, err)
		}
	}
}

func TestOSSUtilCallerRequiresObjectIdentifiers(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	if _, _, err := caller.commandArgs("GetBucketInfo", nil, nil); appErrorCode(err) != "MissingParameter" {
		t.Fatalf("missing Bucket error = %v", err)
	}
	if _, _, err := caller.commandArgs("DeleteObject", map[string]any{"Bucket": "bucket"}, nil); appErrorCode(err) != "MissingParameter" {
		t.Fatalf("missing Key error = %v", err)
	}
	if _, _, err := caller.commandArgs("GetObject", map[string]any{"Bucket": "bucket", "Key": "object"}, nil); appErrorCode(err) != "MissingParameter" {
		t.Fatalf("missing download File error = %v", err)
	}
}

func TestOSSUtilCallerMapsEveryOperation(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	tests := []struct {
		operation string
		request   map[string]any
		want      []string
	}{
		{operation: "PutBucket", request: map[string]any{"Bucket": "bucket"}, want: []string{"--auto-plugin-install", "false", "ossutil", "api", "put-bucket", "--bucket", "bucket"}},
		{operation: "GetBucketInfo", request: map[string]any{"Bucket": "bucket"}, want: []string{"--auto-plugin-install", "false", "ossutil", "api", "get-bucket-info", "--bucket", "bucket"}},
		{operation: "GetBucketAcl", request: map[string]any{"Bucket": "bucket"}, want: []string{"--auto-plugin-install", "false", "ossutil", "api", "get-bucket-acl", "--bucket", "bucket"}},
		{operation: "ListBuckets", request: map[string]any{
			"Prefix": "pre", "Marker": "mark", "MaxKeys": 10, "ResourceGroupId": "rg-1",
			"TagKey": "key", "TagValue": "value", "Tagging": `"key":"value"`,
		}, want: []string{
			"--auto-plugin-install", "false", "ossutil", "api", "list-buckets", "--prefix", "pre", "--marker", "mark", "--max-keys", "10",
			"--resource-group-id", "rg-1", "--tag-key", "key", "--tag-value", "value", "--tagging", `"key":"value"`,
		}},
		{operation: "DeleteBucket", request: map[string]any{"Bucket": "bucket"}, want: []string{"--auto-plugin-install", "false", "ossutil", "api", "delete-bucket", "--bucket", "bucket"}},
		{operation: "ListObjects", request: map[string]any{
			"Bucket": "bucket", "ContinuationToken": "token", "Delimiter": "/", "EncodingType": "url",
			"FetchOwner": true, "MaxKeys": 10, "Prefix": "pre", "StartAfter": "start",
		}, want: []string{
			"--auto-plugin-install", "false", "ossutil", "api", "list-objects-v2", "--bucket", "bucket",
			"--continuation-token", "token", "--delimiter", "/", "--encoding-type", "url", "--fetch-owner",
			"--max-keys", "10", "--prefix", "pre", "--start-after", "start",
		}},
		{operation: "DeleteObject", request: map[string]any{
			"Bucket": "bucket", "Key": "dir/object.txt", "VersionId": "version-1", "BypassGovernanceRetention": true,
		}, want: []string{
			"--auto-plugin-install", "false", "ossutil", "api", "delete-object", "--bucket", "bucket",
			"--key", "dir/object.txt", "--version-id", "version-1", "--bypass-governance-retention",
		}},
		{operation: "GetObject", request: map[string]any{
			"Bucket": "bucket", "Key": "dir/export.raw.tar.gz", "File": "/tmp/export.raw.tar.gz",
		}, want: []string{
			"--auto-plugin-install", "false", "ossutil", "cp", "oss://bucket/dir/export.raw.tar.gz", "/tmp/export.raw.tar.gz", "--no-progress",
		}},
		{operation: "PutObject", request: map[string]any{
			"Bucket": "bucket", "Key": "dir/import.raw", "File": "/tmp/import.raw",
		}, want: []string{
			"--auto-plugin-install", "false", "ossutil", "cp", "/tmp/import.raw", "oss://bucket/dir/import.raw", "--no-progress",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			args, _, err := caller.commandArgs(tt.operation, tt.request, nil)
			if err != nil {
				t.Fatalf("commandArgs: %v", err)
			}
			want := append(append([]string(nil), tt.want...), "--region", "cn-hangzhou")
			if tt.operation != "GetObject" && tt.operation != "PutObject" {
				want = append(want, "--output-format", "json")
			}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("args = %#v, want %#v", args, want)
			}
		})
	}
}

func TestOSSUtilCallerReturnsStructuredTransferResult(t *testing.T) {
	runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
		{stdout: []byte("2.3.0\n")},
		{stdout: []byte("cp help\n")},
		{stdout: []byte("download succeeded\n")},
	}}
	caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
	response, err := caller.Call(context.Background(), "GetObject", map[string]any{
		"Bucket": "bucket", "Key": "export.raw.tar.gz", "File": "/tmp/export.raw.tar.gz",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	want := map[string]any{"Bucket": "bucket", "Key": "export.raw.tar.gz", "File": "/tmp/export.raw.tar.gz"}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("GetObject response = %#v, want %#v", response, want)
	}
	if got := runner.calls[1].args; !reflect.DeepEqual(got, []string{"cp", "--help"}) {
		t.Fatalf("transfer help args = %#v", got)
	}
}

func TestOSSUtilCallerForwardsTransferParallelism(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	args, _, err := caller.commandArgs("PutObject", map[string]any{
		"Bucket": "bucket", "Key": "import.qcow2", "File": "/tmp/import.qcow2",
	}, []string{"--parallel", "16", "--part-size", "64Mi"})
	if err != nil {
		t.Fatalf("commandArgs: %v", err)
	}
	wantSuffix := []string{"--parallel", "16", "--part-size", "64Mi", "--region", "cn-hangzhou"}
	if !reflect.DeepEqual(args[len(args)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("args suffix = %#v, want %#v", args, wantSuffix)
	}
	if _, _, err := caller.commandArgs("ListObjects", map[string]any{"Bucket": "bucket"}, []string{"--parallel", "16"}); appErrorCode(err) != "UnsupportedOSSParameter" {
		t.Fatalf("non-transfer parallel error = %v", err)
	}
}

func TestOSSUtilCallerRequiresExplicitForceForTransfers(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	for _, operation := range []string{"GetObject", "PutObject"} {
		t.Run(operation, func(t *testing.T) {
			base := map[string]any{"Bucket": "bucket", "Key": "object", "File": "/tmp/object"}
			args, _, err := caller.commandArgs(operation, base, nil)
			if err != nil {
				t.Fatalf("default commandArgs: %v", err)
			}
			if slices.Contains(args, "--force") {
				t.Fatalf("default args contain --force: %#v", args)
			}

			forced := map[string]any{"Bucket": "bucket", "Key": "object", "File": "/tmp/object", "Force": true}
			args, _, err = caller.commandArgs(operation, forced, nil)
			if err != nil {
				t.Fatalf("forced commandArgs: %v", err)
			}
			if !slices.Contains(args, "--force") {
				t.Fatalf("forced args missing --force: %#v", args)
			}

			unforced := map[string]any{"Bucket": "bucket", "Key": "object", "File": "/tmp/object", "Force": false}
			args, _, err = caller.commandArgs(operation, unforced, nil)
			if err != nil {
				t.Fatalf("false Force commandArgs: %v", err)
			}
			if slices.Contains(args, "--force") {
				t.Fatalf("false Force args contain --force: %#v", args)
			}

			invalid := map[string]any{"Bucket": "bucket", "Key": "object", "File": "/tmp/object", "Force": "true"}
			if _, _, err := caller.commandArgs(operation, invalid, nil); appErrorCode(err) != "InvalidParameter" {
				t.Fatalf("string Force error = %v", err)
			}
		})
	}
}

func TestOSSUtilCallerMapsBooleanParametersAsSwitches(t *testing.T) {
	caller := newTestOSSUtilCaller(t, &fakeOSSUtilRunner{}, "cn-hangzhou")
	tests := []struct {
		operation string
		request   map[string]any
		flag      string
	}{
		{operation: "ListObjects", request: map[string]any{"Bucket": "bucket", "FetchOwner": false}, flag: "--fetch-owner"},
		{operation: "DeleteObject", request: map[string]any{"Bucket": "bucket", "Key": "object", "BypassGovernanceRetention": false}, flag: "--bypass-governance-retention"},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			args, _, err := caller.commandArgs(tt.operation, tt.request, nil)
			if err != nil {
				t.Fatalf("commandArgs: %v", err)
			}
			if strings.Contains(strings.Join(args, " "), tt.flag) {
				t.Fatalf("false boolean emitted %s: %#v", tt.flag, args)
			}
		})
	}
	if _, _, err := caller.commandArgs("ListObjects", map[string]any{"Bucket": "bucket", "FetchOwner": "true"}, nil); appErrorCode(err) != "InvalidParameter" {
		t.Fatalf("string FetchOwner error = %v", err)
	}
}

func TestOSSUtilCallerAcceptsEmptyDeleteObjectResponse(t *testing.T) {
	runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
		{stdout: []byte("2.3.0\n")},
		{stdout: []byte("DeleteObject help\n")},
		{},
	}}
	caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
	response, err := caller.Call(context.Background(), "DeleteObject", map[string]any{
		"Bucket": "bucket",
		"Key":    "object",
	})
	if err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if len(response) != 0 {
		t.Fatalf("DeleteObject response = %#v, want empty object", response)
	}
}

func TestOSSUtilCallerRequiresVersion2(t *testing.T) {
	tests := []struct {
		name   string
		result ossUtilRunnerResult
		code   string
	}{
		{name: "version 1", result: ossUtilRunnerResult{stdout: []byte("1.7.19")}, code: "OSSUtilVersionUnsupported"},
		{name: "version 3", result: ossUtilRunnerResult{stdout: []byte("3.0.0")}, code: "OSSUtilVersionUnsupported"},
		{name: "unparseable", result: ossUtilRunnerResult{stdout: []byte("unknown")}, code: "OSSUtilVersionUnsupported"},
		{name: "missing executable", result: ossUtilRunnerResult{err: &exec.Error{Name: "aliyun", Err: exec.ErrNotFound}}, code: "OSSUtilUnavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{tt.result}}
			caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
			var err error
			_, err = caller.Call(context.Background(), "ListBuckets", nil)
			if got := appErrorCode(err); got != tt.code {
				t.Fatalf("error code = %q, want %q; err=%v", got, tt.code, err)
			}
		})
	}
}

func TestDecodeOSSUtilResponseIsStrict(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		allowEmpty bool
		wantCode   string
	}{
		{name: "object with elapsed", stdout: "{\"ok\":true}\n0.001(s) elapsed\n"},
		{name: "empty mutation", allowEmpty: true},
		{name: "empty query", wantCode: "InvalidOSSUtilOutput"},
		{name: "plain text", stdout: "success", wantCode: "InvalidOSSUtilOutput"},
		{name: "multiple json", stdout: "{}\n{}", wantCode: "InvalidOSSUtilOutput"},
		{name: "json plus unknown trailer", stdout: "{}\ndone", wantCode: "InvalidOSSUtilOutput"},
		{name: "array", stdout: "[]", wantCode: "InvalidOSSUtilOutput"},
		{name: "scalar", stdout: "true", wantCode: "InvalidOSSUtilOutput"},
		{name: "null", stdout: "null", wantCode: "InvalidOSSUtilOutput"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := decodeOSSUtilResponse([]byte(tt.stdout), tt.allowEmpty)
			if got := appErrorCode(err); got != tt.wantCode {
				t.Fatalf("error code = %q, want %q; response=%#v err=%v", got, tt.wantCode, response, err)
			}
		})
	}
}

func TestOSSUtilCallerClassifiesBucketErrors(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		wantCode string
		wantExit int
	}{
		{name: "missing", stderr: "Error Code: NoSuchBucket\nError Message: The specified bucket does not exist.\nRequest ID: req-missing", wantCode: "NotFound", wantExit: 4},
		{name: "not empty", stderr: "Error Code: BucketNotEmpty\nError Message: The bucket has objects.\nRequest ID: req-dependency", wantCode: "DependencyViolation", wantExit: 2},
		{name: "access denied", stderr: "Error Code: AccessDenied\nError Message: Access denied.\nRequest ID: req-denied", wantCode: "CloudAPIError", wantExit: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
				{stdout: []byte("2.3.0")},
				{stdout: []byte("GetBucketAcl help")},
				{stderr: []byte(tt.stderr), err: errors.New("exit status 1")},
			}}
			caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
			var err error
			_, err = caller.Call(context.Background(), "GetBucketAcl", map[string]any{"Bucket": "missing-bucket"})
			var appErr *ecerrors.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if appErr.Payload().Code != tt.wantCode || appErr.ExitCode() != tt.wantExit {
				t.Fatalf("error payload = %#v exit=%d, want code=%s exit=%d", appErr.Payload(), appErr.ExitCode(), tt.wantCode, tt.wantExit)
			}
			if tt.wantCode == "NotFound" && appErr.Payload().Message != "missing-bucket not found" {
				t.Fatalf("not-found message = %q", appErr.Payload().Message)
			}
		})
	}
}

func TestOSSUtilCallerValidatesLocallyBeforeRunner(t *testing.T) {
	runner := &fakeOSSUtilRunner{}
	caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
	resolved := false
	caller.resolveRuntime = func(func(string) string) (string, error) {
		resolved = true
		return "/test/ossutil", nil
	}
	_, err := caller.Call(context.Background(), "PutBucket", map[string]any{"DryRun": true})
	if appErrorCode(err) != "UnsupportedOSSParameter" {
		t.Fatalf("local validation error = %v", err)
	}
	if resolved || len(runner.calls) != 0 {
		t.Fatalf("runtime work occurred before local validation: resolved=%v calls=%#v", resolved, runner.calls)
	}
}

func TestOSSUtilCallerRequiresValidRegionAtConstruction(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	raw := `{"current":"prod","profiles":[{"name":"prod","access_key_id":"test-ak","access_key_secret":"test-secret"}]}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := newOSSUtilCaller("prod", configPath, "", ossUtilTestGetenv(t), &fakeOSSUtilRunner{}); appErrorCode(err) != "MissingRegion" {
		t.Fatalf("missing region error = %v", err)
	}
	if _, err := newOSSUtilCaller("prod", configPath, "not-a-real-region", ossUtilTestGetenv(t), &fakeOSSUtilRunner{}); appErrorCode(err) != "InvalidRegion" {
		t.Fatalf("invalid region error = %v", err)
	}
}

func TestOSSUtilCallerReportsUnavailableOperationAsClientError(t *testing.T) {
	runner := &fakeOSSUtilRunner{results: []ossUtilRunnerResult{
		{stdout: []byte("2.3.0")},
		{stderr: []byte("unknown command get-bucket-info"), err: errors.New("exit status 1")},
	}}
	caller := newTestOSSUtilCaller(t, runner, "cn-hangzhou")
	_, err := caller.Call(context.Background(), "GetBucketInfo", map[string]any{"Bucket": "bucket"})
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "OSSUtilAPIUnavailable" || appErr.ExitCode() != 1 {
		t.Fatalf("API unavailable error = %T %v", err, err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("launcher should not run after unavailable API: %#v", runner.calls)
	}
}

func TestResolveInstalledOSSUtilRuntime(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".aliyun", "ossutil")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	resolved, err := resolveInstalledOSSUtilRuntime(func(name string) string {
		if name == "HOME" {
			return home
		}
		return ""
	})
	if err != nil || resolved != path {
		t.Fatalf("resolved runtime = %q, %v; want %q", resolved, err, path)
	}
	if _, err := resolveInstalledOSSUtilRuntime(func(string) string { return t.TempDir() }); appErrorCode(err) != "OSSUtilUnavailable" {
		t.Fatalf("missing runtime error = %v", err)
	}
}

func writeOSSUtilTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"current":"prod","profiles":[{"name":"prod","region_id":"cn-hangzhou","access_key_id":"test-ak","access_key_secret":"test-secret","sts_token":"test-token"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func newTestOSSUtilCaller(t *testing.T, runner CLICommandRunner, region string) *OSSUtilCaller {
	t.Helper()
	caller, err := newOSSUtilCaller("prod", writeOSSUtilTestConfig(t), region, ossUtilTestGetenv(t), runner)
	if err != nil {
		t.Fatalf("newOSSUtilCaller: %v", err)
	}
	caller.resolveRuntime = func(func(string) string) (string, error) {
		return "/test/ossutil", nil
	}
	return caller
}

func ossUtilTestGetenv(t *testing.T) func(string) string {
	t.Helper()
	missingAliyunConfig := filepath.Join(t.TempDir(), "missing-aliyun-config.json")
	return func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return missingAliyunConfig
		}
		return ""
	}
}

func environmentValue(env []string, name string) string {
	prefix := name + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func environmentHasKey(env []string, name string) bool {
	prefix := name + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func appErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		return ""
	}
	return appErr.Payload().Code
}
