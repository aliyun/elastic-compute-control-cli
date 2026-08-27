package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	credentialproviders "github.com/aliyun/credentials-go/credentials/providers"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	ecconfig "github.com/aliyun/elastic-compute-control-cli/pkg/config"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
	"github.com/aliyun/elastic-compute-control-cli/pkg/telemetry"
	_ "github.com/aliyun/elastic-compute-control-cli/specs/ecs"
)

func TestExportedCallerConstructorCompatibilitySignatures(t *testing.T) {
	var openAPILegacy func(string, string, string, string, func(string) string) (*OpenAPICaller, error) = NewOpenAPICaller
	var openAPIWithSource func(string, string, string, ecconfig.ResolvedRegion, func(string) string) (*OpenAPICaller, error) = NewOpenAPICallerWithRegionSource
	var ossLegacy func(string, string, string, func(string) string) (*OSSUtilCaller, error) = NewOSSUtilCaller
	var ossWithSource func(string, string, ecconfig.ResolvedRegion, func(string) string) (*OSSUtilCaller, error) = NewOSSUtilCallerWithRegionSource
	if openAPILegacy == nil || openAPIWithSource == nil || ossLegacy == nil || ossWithSource == nil {
		t.Fatal("exported caller compatibility functions are unavailable")
	}
}

type regionStringer string

func (r regionStringer) String() string { return string(r) }

func testResolvedOpenAPIProfile(t *testing.T, region string) resolvedOpenAPIProfile {
	t.Helper()
	credential, err := staticCredentialFromValues("ak", "secret", "", credentialModeAK, "test-credential")
	if err != nil {
		t.Fatalf("staticCredentialFromValues: %v", err)
	}
	return resolvedOpenAPIProfile{
		Acquirer:            credential.Acquirer,
		AuthType:            credential.AuthType,
		CredentialPrincipal: credential.Principal,
		Mode:                credential.Mode,
		RegionID:            region,
	}
}

func testCredentialSnapshot(t *testing.T, acquirer credentialAcquirer) *credentialSnapshot {
	t.Helper()
	snapshot, err := acquirer.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return snapshot
}

type recordingHTTPClient struct {
	request      *http.Request
	requests     []*http.Request
	responseBody string
}

func (c *recordingHTTPClient) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
	c.request = req
	c.requests = append(c.requests, req)
	body := c.responseBody
	if body == "" {
		body = `{"RequestId":"req-1"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

type fakeOpenAPIExecutor struct {
	requests   []*openAPIRequest
	response   string
	responses  []string
	callError  error
	callErrors []error
}

type fakeCLICommandRunner struct {
	name   string
	args   []string
	env    []string
	config map[string]any
	stdout string
	stderr string
	err    error
}

func (f *fakeCLICommandRunner) Run(_ context.Context, name string, args []string, env []string) ([]byte, []byte, error) {
	f.name = name
	f.args = append([]string(nil), args...)
	f.env = append([]string(nil), env...)
	if configPath := argAfter(args, "--config-path"); configPath != "" {
		f.config, _ = readJSONObjectFromPath(configPath)
	}
	return []byte(f.stdout), []byte(f.stderr), f.err
}

func (f *fakeOpenAPIExecutor) ExecuteOpenAPI(_ context.Context, req *openAPIRequest) (map[string]any, error) {
	callIndex := len(f.requests)
	f.requests = append(f.requests, req)
	if callIndex < len(f.callErrors) && f.callErrors[callIndex] != nil {
		return nil, f.callErrors[callIndex]
	}
	if f.callError != nil {
		return nil, f.callError
	}
	body := f.response
	if callIndex < len(f.responses) {
		body = f.responses[callIndex]
	}
	return decodeOpenAPIResponse(body)
}

func commonRequestParam(req *openAPIRequest, key string) string {
	if value := req.QueryParams[key]; value != "" {
		return value
	}
	return req.FormParams[key]
}

func TestCLICommandCallerRunsAliyunCLIWithMergedConfigAndRequest(t *testing.T) {
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "prod",
		"profiles": []any{
			map[string]any{
				"name":                    "prod",
				"mode":                    "RamRoleArn",
				"access_key_id":           "aliyun-id",
				"access_key_secret":       "aliyun-secret",
				"ram_role_arn":            "acs:ram::1234567890123456:role/admin",
				"source_profile":          "base",
				"region_id":               "us-west-1",
				"auto_plugin_install":     true,
				"auto_plugin_install_pre": true,
			},
			map[string]any{
				"name":              "base",
				"mode":              "AK",
				"access_key_id":     "base-id",
				"access_key_secret": "base-secret",
			},
		},
	})
	writeJSONFile(t, ecctlPath, map[string]any{
		"current": "prod",
		"profiles": []any{
			map[string]any{
				"name":              "prod",
				"mode":              "AK",
				"access_key_id":     "ecctl-id",
				"access_key_secret": "ecctl-secret",
				"region_id":         "cn-hangzhou",
			},
		},
	})
	runner := &fakeCLICommandRunner{stdout: `{"RequestId":"req-1","TotalCount":1}`}
	getenv := func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return aliyunPath
		}
		return ""
	}
	caller, err := newCLICommandCaller("prod", ecctlPath, "ecs", "cn-hangzhou", getenv, runner)
	if err != nil {
		t.Fatalf("newCLICommandCaller: %v", err)
	}

	resp, err := caller.CallWithArgs(context.Background(), "DescribeInstances", map[string]any{
		"PageSize": float64(20),
		"DryRun":   true,
		"Filter":   map[string]any{"Status": "Running"},
	}, []string{"--method", "POST"})
	if err != nil {
		t.Fatalf("CallWithArgs: %v", err)
	}
	if resp["RequestId"] != "req-1" || resp["TotalCount"] != float64(1) {
		t.Fatalf("response = %#v", resp)
	}
	if runner.name != "aliyun" {
		t.Fatalf("runner name = %q, want aliyun", runner.name)
	}
	for _, want := range [][]string{
		{"ecs", "DescribeInstances"},
		{"--profile", "prod"},
		{"--region", "cn-hangzhou"},
		{"--PageSize", "20"},
		{"--DryRun", "true"},
		{"--Filter", `{"Status":"Running"}`},
		{"--method", "POST"},
	} {
		if !containsArgPair(runner.args, want[0], want[1]) {
			t.Fatalf("args missing %v: %#v", want, runner.args)
		}
	}
	configPath := argAfter(runner.args, "--config-path")
	if configPath == "" {
		t.Fatalf("args missing --config-path: %#v", runner.args)
	}
	rawConfig := runner.config
	profile := profileMapFromConfig(t, rawConfig, "prod")
	if profile["access_key_id"] != "ecctl-id" || profile["access_key_secret"] != "ecctl-secret" || profile["region_id"] != "cn-hangzhou" {
		t.Fatalf("ecctl overlay not applied to temp aliyun config: %#v", profile)
	}
	if profile["ram_role_arn"] != "acs:ram::1234567890123456:role/admin" || profile["source_profile"] != "base" {
		t.Fatalf("aliyun-only profile fields not preserved: %#v", profile)
	}
	if profileMapFromConfig(t, rawConfig, "base")["access_key_id"] != "base-id" {
		t.Fatalf("source profile not preserved in temp config: %#v", rawConfig)
	}
}

func TestCLICommandCallerMergesPassthroughProfileFromEcctlConfig(t *testing.T) {
	dir := t.TempDir()
	ecctlPath := filepath.Join(dir, "ecctl.json")
	aliyunPath := filepath.Join(dir, "aliyun.json")
	writeJSONFile(t, aliyunPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "default", "mode": "AK", "access_key_id": "default-id"},
			map[string]any{"name": "prod", "mode": "AK", "access_key_id": "aliyun-id", "region_id": "us-west-1"},
		},
	})
	writeJSONFile(t, ecctlPath, map[string]any{
		"current": "default",
		"profiles": []any{
			map[string]any{"name": "prod", "mode": "AK", "access_key_id": "ecctl-id", "access_key_secret": "ecctl-secret", "region_id": "cn-beijing"},
		},
	})
	runner := &fakeCLICommandRunner{stdout: `{"RequestId":"req-profile"}`}
	getenv := func(name string) string {
		if name == "ECCTL_ALIYUN_CONFIG_PATH" {
			return aliyunPath
		}
		return ""
	}
	caller, err := newCLICommandCaller("", ecctlPath, "ecs", "", getenv, runner)
	if err != nil {
		t.Fatalf("newCLICommandCaller: %v", err)
	}

	if _, err := caller.CallWithArgs(context.Background(), "DescribeInstances", nil, []string{"-p", "prod"}); err != nil {
		t.Fatalf("CallWithArgs: %v", err)
	}
	if !containsArgPair(runner.args, "-p", "prod") {
		t.Fatalf("args missing passthrough profile: %#v", runner.args)
	}
	profile := profileMapFromConfig(t, runner.config, "prod")
	if profile["access_key_id"] != "ecctl-id" || profile["access_key_secret"] != "ecctl-secret" || profile["region_id"] != "cn-beijing" {
		t.Fatalf("passthrough profile did not use ecctl overlay: %#v", profile)
	}
}

func TestCLICommandCallerConvertsDryRunOperation(t *testing.T) {
	runner := &fakeCLICommandRunner{
		stderr: "DryRunOperation: request would have succeeded",
		err:    errors.New("exit status 1"),
	}
	caller, err := newCLICommandCaller("", filepath.Join(t.TempDir(), "missing.json"), "vpc", "cn-beijing", func(string) string { return "" }, runner)
	if err != nil {
		t.Fatalf("newCLICommandCaller: %v", err)
	}

	resp, err := caller.Call(context.Background(), "DeleteVpc", map[string]any{"DryRun": true})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !reflect.DeepEqual(resp, map[string]any{"DryRun": true}) {
		t.Fatalf("response = %#v, want DryRun marker", resp)
	}
}

func TestOpenAPICallerBuildsRPCRequestFromSpecRequest(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","VpcId":"vpc-1"}`}
	caller := &OpenAPICaller{
		Product:  "vpc",
		Region:   "cn-beijing",
		executor: fake,
	}

	resp, err := caller.Call(context.Background(), "CreateVpc", map[string]any{
		"RegionId":    "cn-beijing",
		"VpcName":     "prod",
		"CidrBlock":   "10.0.0.0/16",
		"ClientToken": "token-1",
		"Tag":         []string{"env=dev", "team=platform"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp["VpcId"] != "vpc-1" || resp["RequestId"] != "req-1" {
		t.Fatalf("response = %#v", resp)
	}
	req := fake.requests[0]
	if req.Product != "Vpc" && req.Product != "vpc" {
		t.Fatalf("product = %q", req.Product)
	}
	if req.ApiName != "CreateVpc" {
		t.Fatalf("ApiName = %q", req.ApiName)
	}
	if req.QueryParams["VpcName"] != "prod" || req.QueryParams["ClientToken"] != "token-1" {
		t.Fatalf("query params = %#v", req.QueryParams)
	}
	if req.QueryParams["Tag.1.Key"] != "env" || req.QueryParams["Tag.1.Value"] != "dev" {
		t.Fatalf("tag 1 params = %#v", req.QueryParams)
	}
	if req.QueryParams["Tag.2.Key"] != "team" || req.QueryParams["Tag.2.Value"] != "platform" {
		t.Fatalf("tag 2 params = %#v", req.QueryParams)
	}
}

func TestSetQueryParamExpandsTemplateTagParameters(t *testing.T) {
	out := map[string]string{}

	if err := setQueryParam(out, "TemplateTag", []string{"env=prod"}); err != nil {
		t.Fatalf("setQueryParam: %v", err)
	}
	if out["TemplateTag.1.Key"] != "env" || out["TemplateTag.1.Value"] != "prod" {
		t.Fatalf("TemplateTag params = %#v", out)
	}
	if _, ok := out["TemplateTag"]; ok {
		t.Fatalf("TemplateTag should be expanded, got %#v", out)
	}
}

func TestSetQueryParamEncodesDecodedJSONArrayStringParameters(t *testing.T) {
	out := map[string]string{}

	if err := setQueryParam(out, "DiskIds", []any{"d-1", "d-2"}); err != nil {
		t.Fatalf("setQueryParam: %v", err)
	}
	if out["DiskIds"] != `["d-1","d-2"]` {
		t.Fatalf("DiskIds = %q, want JSON array string", out["DiskIds"])
	}
}

func TestOpenAPICallerRoutesLingjunBodyParamsToFormParams(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","VscId":"vsc-1"}`}
	caller := &OpenAPICaller{Product: "eflo-controller", Resource: "vsc", Region: "cn-wulanchabu", executor: fake}

	_, err := caller.Call(context.Background(), "CreateVsc", map[string]any{
		"NodeId":       "node-1",
		"VscName":      "serial-a",
		"VscType":      "primary",
		"ClientToken":  "token-1",
		"Tag":          []string{"env=prod"},
		"ResourceType": "ignored-if-unknown",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.FormParams["NodeId"] != "node-1" || req.FormParams["VscName"] != "serial-a" {
		t.Fatalf("form params = %#v", req.FormParams)
	}
	if commonRequestParam(req, "ClientToken") != "token-1" {
		t.Fatalf("ClientToken missing from request: form=%#v query=%#v", req.FormParams, req.QueryParams)
	}
	if req.FormParams["Tag.1.Key"] != "env" || req.FormParams["Tag.1.Value"] != "prod" {
		t.Fatalf("Tag form params = %#v", req.FormParams)
	}
	if _, ok := req.QueryParams["NodeId"]; ok {
		t.Fatalf("body param NodeId should not be in query params: %#v", req.QueryParams)
	}
	if req.QueryParams["ResourceType"] != "ignored-if-unknown" {
		t.Fatalf("unknown injected param should fall back to query: %#v", req.QueryParams)
	}
}

func TestOpenAPICallerRoutesLingjunNestedBodyParamsToFormParams(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","Content":{"VpdId":"vpd-1"}}`}
	caller := &OpenAPICaller{Product: "eflo", Resource: "vpd", Region: "cn-wulanchabu", executor: fake}

	_, err := caller.Call(context.Background(), "CreateVpd", map[string]any{
		"RegionId":             "cn-wulanchabu",
		"VpdName":              "train-vpd",
		"Cidr":                 "10.0.0.0/16",
		"Subnets.1.SubnetName": "train-subnet",
		"Subnets.1.Cidr":       "10.0.1.0/24",
		"Subnets.1.ZoneId":     "cn-wulanchabu-a",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	for key, want := range map[string]string{
		"VpdName":              "train-vpd",
		"Cidr":                 "10.0.0.0/16",
		"Subnets.1.SubnetName": "train-subnet",
		"Subnets.1.Cidr":       "10.0.1.0/24",
		"Subnets.1.ZoneId":     "cn-wulanchabu-a",
	} {
		if req.FormParams[key] != want {
			t.Fatalf("FormParams[%s] = %q, want %q; form=%#v query=%#v", key, req.FormParams[key], want, req.FormParams, req.QueryParams)
		}
		if _, ok := req.QueryParams[key]; ok {
			t.Fatalf("%s should not be in query params: %#v", key, req.QueryParams)
		}
	}
}

func TestOpenAPICallerBuildsCSROAPathParams(t *testing.T) {
	caller := &OpenAPICaller{Product: "CS", Resource: "kubeconfig", Region: "cn-hangzhou"}

	req, err := caller.commonRequest("DescribeClusterUserKubeconfig", map[string]any{
		"ClusterId": "c-123",
	})
	if err != nil {
		t.Fatalf("commonRequest: %v", err)
	}
	if req.PathPattern == "" {
		t.Fatalf("PathPattern is empty for ROA request: %#v", req)
	}
	if req.PathParams["ClusterId"] != "c-123" {
		t.Fatalf("PathParams = %#v, want ClusterId", req.PathParams)
	}
	if _, ok := req.QueryParams["ClusterId"]; ok {
		t.Fatalf("path parameter should not be sent as query: %#v", req.QueryParams)
	}
}

func TestOpenAPICallerUsesProductEndpointResolverForCSROA(t *testing.T) {
	caller := &OpenAPICaller{
		Product: "CS",
		Region:  "cn-hangzhou",
		endpointResolver: func(product OpenAPIProduct, region string, endpointType string) (string, error) {
			if product.Code != "CS" || region != "cn-hangzhou" || endpointType != "" {
				t.Fatalf("resolver args = product:%s region:%s endpointType:%s", product.Code, region, endpointType)
			}
			return "cs-resolved.aliyuncs.com", nil
		},
	}

	req, err := caller.commonRequest("DeleteTemplate", map[string]any{
		"TemplateId": "tmpl-123",
	})
	if err != nil {
		t.Fatalf("commonRequest: %v", err)
	}
	if req.Domain != "cs-resolved.aliyuncs.com" {
		t.Fatalf("Domain = %q, want resolver endpoint", req.Domain)
	}
	if req.Method != "DELETE" {
		t.Fatalf("Method = %q, want DELETE", req.Method)
	}
	if _, ok := req.QueryParams["TemplateId"]; ok {
		t.Fatalf("path parameter should not be sent as query: %#v", req.QueryParams)
	}
	req.TransToAcsRequest()
	if got := req.BuildQueries(); got != "/templates/tmpl-123" {
		t.Fatalf("BuildQueries = %q, want /templates/tmpl-123", got)
	}
}

func TestOpenAPICallerUsesMetadataGlobalEndpointForEmptyRegion(t *testing.T) {
	caller := &OpenAPICaller{}
	product := OpenAPIProduct{
		Code: "Dbs",
		Endpoints: map[string]OpenAPIEndpoint{
			"": {Public: "dbs-api.cn-hangzhou.aliyuncs.com"},
		},
	}

	endpoint, err := caller.productEndpoint(product, "")
	if err != nil {
		t.Fatalf("productEndpoint: %v", err)
	}
	if endpoint != "dbs-api.cn-hangzhou.aliyuncs.com" {
		t.Fatalf("endpoint = %q, want metadata global endpoint", endpoint)
	}
}

func TestOpenAPICallerBuildsCSROAPUTMethodAndBody(t *testing.T) {
	caller := &OpenAPICaller{
		Product: "CS",
		Region:  "cn-hangzhou",
		endpointResolver: func(_ OpenAPIProduct, _ string, _ string) (string, error) {
			return "cs-resolved.aliyuncs.com", nil
		},
	}

	req, err := caller.commonRequest("UpdateTemplate", map[string]any{
		"TemplateId": "tmpl-123",
		"body.name":  "web-renamed",
	})
	if err != nil {
		t.Fatalf("commonRequest: %v", err)
	}
	if req.Method != "PUT" {
		t.Fatalf("Method = %q, want PUT", req.Method)
	}
	req.TransToAcsRequest()
	if got := req.BuildQueries(); got != "/templates/tmpl-123" {
		t.Fatalf("BuildQueries = %q, want /templates/tmpl-123", got)
	}
	raw, err := io.ReadAll(req.GetBodyReader())
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("body is not JSON: %q: %v", string(raw), err)
	}
	if decoded["name"] != "web-renamed" {
		t.Fatalf("body = %#v", decoded)
	}
}

func TestOpenAPICallerBuildsCSROAJSONBodyFromFlatBodyFields(t *testing.T) {
	caller := &OpenAPICaller{Product: "cs", Resource: "addon", Region: "cn-hangzhou"}

	req, err := caller.commonRequest("InstallClusterAddons", map[string]any{
		"ClusterId":      "c-123",
		"body.1.name":    "coredns",
		"body.1.version": "v1.10.0",
		"body.1.config":  `{"replicas":2}`,
	})
	if err != nil {
		t.Fatalf("commonRequest: %v", err)
	}
	if req.PathParams["ClusterId"] != "c-123" {
		t.Fatalf("PathParams = %#v, want ClusterId", req.PathParams)
	}
	req.TransToAcsRequest()
	raw, err := io.ReadAll(req.GetBodyReader())
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("body is not JSON array: %q: %v", string(raw), err)
	}
	if len(decoded) != 1 || decoded[0]["name"] != "coredns" || decoded[0]["version"] != "v1.10.0" {
		t.Fatalf("body = %#v", decoded)
	}
	if _, ok := req.FormParams["body.1.name"]; ok {
		t.Fatalf("body fields should not be sent as form params: %#v", req.FormParams)
	}
	if _, ok := req.QueryParams["body.1.name"]; ok {
		t.Fatalf("body fields should not be sent as query params: %#v", req.QueryParams)
	}
}

func TestOpenAPICallerBuildsCSROAJSONBodyFromNamedBodyParameter(t *testing.T) {
	caller := &OpenAPICaller{Product: "cs", Resource: "addon", Region: "cn-hangzhou"}

	req, err := caller.commonRequest("UnInstallClusterAddons", map[string]any{
		"ClusterId":                        "c-123",
		"addons.1.name":                    "coredns",
		"addons.1.cleanup_cloud_resources": true,
	})
	if err != nil {
		t.Fatalf("commonRequest: %v", err)
	}
	req.TransToAcsRequest()
	raw, err := io.ReadAll(req.GetBodyReader())
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("body is not JSON array: %q: %v", string(raw), err)
	}
	if len(decoded) != 1 || decoded[0]["name"] != "coredns" || decoded[0]["cleanup_cloud_resources"] != true {
		t.Fatalf("body = %#v", decoded)
	}
	if _, ok := req.FormParams["addons.1.name"]; ok {
		t.Fatalf("body fields should not be sent as form params: %#v", req.FormParams)
	}
}

func TestOpenAPICallerBuildsACKStateAndInspectRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		values    map[string]any
		wantPath  string
		wantBody  map[string]any
		wantQuery map[string]string
	}{
		{
			name:      "stop alert body",
			operation: "StopAlert",
			values: map[string]any{
				"ClusterId":                  "c-123",
				"body.alert_rule_group_name": "ack-cluster",
			},
			wantPath: "/alert/c-123/alert_rule/stop",
			wantBody: map[string]any{"alert_rule_group_name": "ack-cluster"},
		},
		{
			name:      "disable audit body",
			operation: "UpdateClusterAuditLogConfig",
			values: map[string]any{
				"clusterid":    "c-123",
				"body.disable": true,
			},
			wantPath: "/clusters/c-123/audit_log",
			wantBody: map[string]any{"disable": true},
		},
		{
			name:      "disable control plane log body",
			operation: "UpdateControlPlaneLog",
			values: map[string]any{
				"ClusterId":       "c-123",
				"body.components": []string{},
			},
			wantPath: "/clusters/c-123/controlplanelog",
			wantBody: map[string]any{"components": []any{}},
		},
		{
			name:      "create trigger path and body",
			operation: "CreateTrigger",
			values: map[string]any{
				"cluster_id":      "c-123",
				"body.cluster_id": "c-123",
				"body.project_id": "default/web",
				"body.action":     "redeploy",
				"body.type":       "deployment",
			},
			wantPath: "/clusters/c-123/triggers",
			wantBody: map[string]any{
				"cluster_id": "c-123",
				"project_id": "default/web",
				"action":     "redeploy",
				"type":       "deployment",
			},
		},
		{
			name:      "get inspect config path",
			operation: "GetClusterInspectConfig",
			values:    map[string]any{"clusterId": "c-123"},
			wantPath:  "/clusters/c-123/inspectConfig",
		},
		{
			name:      "list inspect reports path and query",
			operation: "ListClusterInspectReports",
			values: map[string]any{
				"clusterId":  "c-123",
				"maxResults": 10,
			},
			wantPath:  "/clusters/c-123/inspectReports",
			wantQuery: map[string]string{"maxResults": "10"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caller := &OpenAPICaller{Product: "cs", Region: "cn-hangzhou"}
			req, err := caller.commonRequest(tt.operation, tt.values)
			if err != nil {
				t.Fatalf("commonRequest(%s): %v", tt.operation, err)
			}
			if got := req.BuildPath(); got != tt.wantPath {
				t.Fatalf("BuildPath() = %q, want %q; params=%#v", got, tt.wantPath, req.PathParams)
			}
			if tt.wantBody != nil && !reflect.DeepEqual(req.BodyValue(), tt.wantBody) {
				t.Fatalf("BodyValue() = %#v, want %#v", req.BodyValue(), tt.wantBody)
			}
			for key, want := range tt.wantQuery {
				if req.QueryParams[key] != want {
					t.Fatalf("QueryParams[%q] = %q, want %q; query=%#v", key, req.QueryParams[key], want, req.QueryParams)
				}
			}
		})
	}
}

func TestOpenAPICallerRejectsInvalidFlatBodyArrayIndexes(t *testing.T) {
	t.Parallel()

	tests := []string{"body.0.name", "body.-1.name"}
	for _, field := range tests {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			caller := &OpenAPICaller{Product: "cs", Resource: "addon", Region: "cn-hangzhou"}

			_, err := caller.commonRequest("InstallClusterAddons", map[string]any{
				"ClusterId": "c-123",
				field:       "coredns",
			})
			if err == nil {
				t.Fatalf("commonRequest with %s succeeded", field)
			}
			if !strings.Contains(err.Error(), "invalid body array index") {
				t.Fatalf("err = %v, want invalid body array index", err)
			}
		})
	}
}

func TestOpenAPIRequestBodyBuildsObjectBodyAndRejectsMixedFlatFields(t *testing.T) {
	api := &OpenAPIOperationDetail{Style: "ROA", Parameters: []OpenAPIParameter{{Name: "body", Position: "Body"}}}

	body, used, err := openAPIRequestBody(api, map[string]any{
		"body.name":       "coredns",
		"body.config.raw": "{}",
	})
	if err != nil {
		t.Fatalf("openAPIRequestBody object: %v", err)
	}
	object, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("body = %#v, want object", body)
	}
	config, _ := object["config"].(map[string]any)
	if object["name"] != "coredns" || config["raw"] != "{}" {
		t.Fatalf("body object = %#v", object)
	}
	if !used["body.name"] || !used["body.config.raw"] {
		t.Fatalf("used = %#v", used)
	}

	if _, _, err := openAPIRequestBody(api, map[string]any{
		"body.name":   "coredns",
		"body.1.name": "terway",
	}); err == nil || !strings.Contains(err.Error(), "mix object and array") {
		t.Fatalf("mixed body fields err = %v", err)
	}
	if _, _, err := openAPIRequestBody(api, map[string]any{
		"body.1.name": "coredns",
		"body.name":   "terway",
	}); err == nil || !strings.Contains(err.Error(), "mix object and array") {
		t.Fatalf("mixed array fields err = %v", err)
	}
}

func TestOpenAPIRequestBodyUsesExplicitJSONBodyAndSkipsEmptyBodies(t *testing.T) {
	api := &OpenAPIOperationDetail{Style: "ROA", Parameters: []OpenAPIParameter{{Name: "body", Position: "Body"}}}

	body, used, err := openAPIRequestBody(api, map[string]any{"body": `{"enabled":true}`})
	if err != nil {
		t.Fatalf("openAPIRequestBody explicit: %v", err)
	}
	if body != `{"enabled":true}` || !used["body"] {
		t.Fatalf("body = %#v used=%#v", body, used)
	}

	body, used, err = openAPIRequestBody(api, map[string]any{"body": "  "})
	if err != nil {
		t.Fatalf("openAPIRequestBody empty: %v", err)
	}
	if body != nil || !used["body"] {
		t.Fatalf("empty body = %#v used=%#v", body, used)
	}

	body, used, err = openAPIRequestBody(&OpenAPIOperationDetail{}, map[string]any{"body.name": "ignored"})
	if err != nil {
		t.Fatalf("openAPIRequestBody without body param: %v", err)
	}
	if body != nil || len(used) != 0 {
		t.Fatalf("body without metadata = %#v used=%#v", body, used)
	}
}

func TestSetOpenAPIParamRoutesHeaderDomainAndUnknownPositions(t *testing.T) {
	api := &OpenAPIOperationDetail{Parameters: []OpenAPIParameter{
		{Name: "X-Custom", Position: "Header"},
		{Name: "Endpoint", Position: "Domain"},
		{Name: "Broken", Position: "Nowhere"},
	}}
	req := newOpenAPIRequest()

	if err := setOpenAPIParam(req, api, "X-Custom", "value"); err != nil {
		t.Fatalf("set header: %v", err)
	}
	if req.Headers["X-Custom"] != "value" {
		t.Fatalf("headers = %#v", req.Headers)
	}
	if err := setOpenAPIParam(req, api, "Endpoint", "ignored"); err != nil {
		t.Fatalf("set domain: %v", err)
	}
	if _, ok := req.QueryParams["Endpoint"]; ok {
		t.Fatalf("domain param should be ignored: %#v", req.QueryParams)
	}
	if err := setOpenAPIParam(req, api, "Broken", "bad"); err == nil || !strings.Contains(err.Error(), "unknown parameter position") {
		t.Fatalf("unknown position err = %v", err)
	}
}

func TestOpenAPIBodyHelpersCoverEmptyAndMarshalBranches(t *testing.T) {
	for _, value := range []any{nil, "", "  ", []any{}, []map[string]any{}, map[string]any{}} {
		if !isEmptyOpenAPIBody(value) {
			t.Fatalf("%#v should be empty", value)
		}
	}
	if isEmptyOpenAPIBody([]any{"value"}) || isEmptyOpenAPIBody(map[string]any{"name": "value"}) || isEmptyOpenAPIBody(1) {
		t.Fatal("non-empty OpenAPI bodies reported empty")
	}

	req := newOpenAPIRequest()
	if err := setOpenAPIBody(req, "  "); err != nil {
		t.Fatalf("setOpenAPIBody empty string: %v", err)
	}
	if len(req.GetContent()) != 0 {
		t.Fatal("empty string body should not set content")
	}
	if err := setOpenAPIBody(req, make(chan int)); err == nil {
		t.Fatal("setOpenAPIBody accepted non-marshalable value")
	}

	decoded, err := decodeOpenAPIResponse("")
	if err != nil || len(decoded) != 0 {
		t.Fatalf("nil response = %#v err=%v", decoded, err)
	}
	if _, err := decodeOpenAPIResponse("not-json"); err == nil {
		t.Fatal("decodeOpenAPIResponse accepted a non-JSON response")
	}
}

func TestOpenAPICallerInfersEfloEndpointForMissingRegion(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","Content":{"Data":[]}}`}
	caller := &OpenAPICaller{Product: "eflo", Resource: "subnet", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "ListSubnets", map[string]any{
		"RegionId": "cn-beijing",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.Domain != "eflo.cn-beijing.aliyuncs.com" {
		t.Fatalf("Domain = %q, want %q", req.Domain, "eflo.cn-beijing.aliyuncs.com")
	}
}

func TestOpenAPICallerInfersVpcEndpointWithoutMetadata(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1"}`}
	caller := &OpenAPICaller{Product: "Vpc", Region: "cn-heyuan", executor: fake}

	_, err := caller.Call(context.Background(), "DeleteVpc", map[string]any{
		"VpcId": "vpc-1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.Domain != "vpc.cn-heyuan.aliyuncs.com" {
		t.Fatalf("Domain = %q, want vpc.cn-heyuan.aliyuncs.com", req.Domain)
	}
}

func TestOpenAPICallerEndpointFallsBackToRequestedProductCode(t *testing.T) {
	caller := &OpenAPICaller{Product: "vpc"}

	endpoint, err := caller.productEndpoint(OpenAPIProduct{}, "cn-heyuan")
	if err != nil {
		t.Fatalf("productEndpoint: %v", err)
	}
	if endpoint != "vpc.cn-heyuan.aliyuncs.com" {
		t.Fatalf("endpoint = %q, want vpc.cn-heyuan.aliyuncs.com", endpoint)
	}
}

func TestOpenAPICallerUsesStringLikeRegionIDForEndpoint(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1"}`}
	caller := &OpenAPICaller{Product: "Vpc", executor: fake}

	_, err := caller.Call(context.Background(), "DeleteVpc", map[string]any{
		"RegionId": fmt.Stringer(regionStringer("cn-heyuan")),
		"VpcId":    "vpc-1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.Domain != "vpc.cn-heyuan.aliyuncs.com" {
		t.Fatalf("Domain = %q, want vpc.cn-heyuan.aliyuncs.com", req.Domain)
	}
}

func TestOpenAPICallerRejectsUnsafeRegionBeforeEndpointConstruction(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"unexpected"}`}
	caller := &OpenAPICaller{Product: "Vpc", Region: "x@attacker.example-a", executor: fake, Profile: resolvedOpenAPIProfile{Language: "en"}}
	_, err := caller.Call(context.Background(), "DescribeVpcs", map[string]any{"PageSize": 1})
	if appErrorCode(err) != "InvalidRegion" || len(fake.requests) != 0 {
		t.Fatalf("unsafe region error=%v requests=%#v", err, fake.requests)
	}
}

func TestDarabonbaExecutorUsesRequestDomainAsHTTPHost(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-heyuan")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatalf("newDarabonbaExecutor: %v", err)
	}
	recorder := &recordingHTTPClient{}
	executor.client.HttpClient = recorder

	response, err := executor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product:  "Vpc",
		Version:  "2016-04-28",
		ApiName:  "CreateVpc",
		RegionId: "cn-heyuan",
		Domain:   "vpc.cn-heyuan.aliyuncs.com",
		Scheme:   "https",
		Method:   "POST",
		Style:    "RPC",
		QueryParams: map[string]string{
			"CidrBlock": "10.99.0.0/16",
			"RegionId":  "cn-heyuan",
			"VpcName":   "endpoint-probe",
		},
		Headers: map[string]string{},
	})
	if err != nil {
		t.Fatalf("ExecuteOpenAPI: %v", err)
	}
	if response["RequestId"] != "req-1" {
		t.Fatalf("response = %#v, want preserved JSON object", response)
	}
	if recorder.request == nil {
		t.Fatal("HTTP client was not called")
	}
	if recorder.request.URL.Host != "vpc.cn-heyuan.aliyuncs.com" {
		t.Fatalf("URL host = %q, want vpc.cn-heyuan.aliyuncs.com", recorder.request.URL.Host)
	}
	if recorder.request.Host != "vpc.cn-heyuan.aliyuncs.com" {
		t.Fatalf("Host header = %q, want vpc.cn-heyuan.aliyuncs.com", recorder.request.Host)
	}
}

func TestDarabonbaExecutorInjectsCustomBearerHeaderWithoutAKSigning(t *testing.T) {
	resolved, err := bearerCredential("bearer-value", "x-custom-auth", credentialModeBearerToken)
	if err != nil {
		t.Fatalf("bearerCredential: %v", err)
	}
	profile := resolvedOpenAPIProfile{
		Acquirer:             resolved.Acquirer,
		Mode:                 resolved.Mode,
		AuthType:             resolved.AuthType,
		BearerTokenHeaderKey: resolved.BearerTokenHeaderKey,
		RegionID:             "cn-hangzhou",
	}
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatalf("newDarabonbaExecutor: %v", err)
	}
	recorder := &recordingHTTPClient{}
	executor.client.HttpClient = recorder
	_, _ = executor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product:     "Example",
		Version:     "2020-01-01",
		ApiName:     "GetExample",
		RegionId:    "cn-hangzhou",
		Domain:      "example.cn-hangzhou.aliyuncs.com",
		Scheme:      "https",
		Method:      "GET",
		Style:       "RPC",
		QueryParams: map[string]string{},
		Headers:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("ExecuteOpenAPI: %v", err)
	}
	if recorder.request == nil || recorder.request.Header.Get("x-custom-auth") != "bearer-value" {
		t.Fatalf("request headers = %#v", recorder.request)
	}
	if recorder.request.Header.Get("Authorization") != "" {
		t.Fatalf("custom bearer request unexpectedly used AK Authorization: %q", recorder.request.Header.Get("Authorization"))
	}
}

func TestDarabonbaDebugFailsBeforeCredentialUse(t *testing.T) {
	const accessKeyID = "dummy-sensitive-access-key-id"
	const securityToken = "dummy-sensitive-security-token"
	if os.Getenv("ECCTL_DARA_DEBUG_HELPER") == "1" {
		snapshot := &credentialSnapshot{
			AccessKeyID: accessKeyID, AccessKeySecret: "dummy-sensitive-access-key-secret",
			SecurityToken: securityToken, Type: "sts",
		}
		executor, err := newDarabonbaExecutor(resolvedOpenAPIProfile{AuthType: "AK", RegionID: "cn-hangzhou"}, snapshot)
		if executor != nil || appErrorCode(err) != "UnsafeCredentialDebug" {
			t.Fatalf("debug rejection = %#v, %v", executor, err)
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDarabonbaDebugFailsBeforeCredentialUse$")
	command.Env = append(os.Environ(), "DEBUG=dara,other", "ECCTL_DARA_DEBUG_HELPER=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("debug helper failed: %v\n%s", err, output)
	}
	for _, secret := range []string{accessKeyID, securityToken, "Authorization", "Signature"} {
		if bytes.Contains(output, []byte(secret)) {
			t.Fatalf("Dara DEBUG leaked %q: %s", secret, output)
		}
	}
}

func TestCredentialCallersRejectDaraDebugBeforeReadingConfig(t *testing.T) {
	getenv := mapGetenv(map[string]string{"DEBUG": "other,dara", "ECCTL_ALIYUN_CONFIG_PATH": filepath.Join(t.TempDir(), "missing.json")})
	if caller, err := NewOpenAPICaller("missing", filepath.Join(t.TempDir(), "broken.json"), "ecs", "cn-hangzhou", getenv); caller != nil || appErrorCode(err) != "UnsafeCredentialDebug" {
		t.Fatalf("OpenAPI caller = %#v, %v", caller, err)
	}
	if caller, err := NewCLICommandCaller("missing", filepath.Join(t.TempDir(), "broken.json"), "ecs", "cn-hangzhou", getenv); caller != nil || appErrorCode(err) != "UnsafeCredentialDebug" {
		t.Fatalf("CLI caller = %#v, %v", caller, err)
	}
}

func TestBearerCredentialClientRejectsRedirectBeforeDestination(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls++ }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	resolved, err := bearerCredential("bearer-value", "x-custom-auth", credentialModeBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	profile := resolvedOpenAPIProfile{
		Acquirer: resolved.Acquirer, Mode: resolved.Mode, AuthType: resolved.AuthType,
		BearerTokenHeaderKey: resolved.BearerTokenHeaderKey, RegionID: "cn-hangzhou",
	}
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product: "Example", Version: "2020-01-01", ApiName: "GetExample", RegionId: "cn-hangzhou",
		Domain: strings.TrimPrefix(source.URL, "http://"), Scheme: "http", Method: "GET", Style: "RPC",
		QueryParams: map[string]string{}, Headers: map[string]string{},
	})
	if destinationCalls != 0 {
		t.Fatalf("redirect destination received %d requests", destinationCalls)
	}
}

func TestDarabonbaExecutorRefreshesDynamicCredentialForEachRequest(t *testing.T) {
	provider := &rotatingCredentialsProvider{}
	acquirer := &credentialsProviderAcquirer{provider: provider, mode: credentialModeAK}
	profile := resolvedOpenAPIProfile{Acquirer: acquirer, Mode: credentialModeAK, AuthType: "AK", RegionID: "cn-hangzhou"}
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, acquirer))
	if err != nil {
		t.Fatalf("newDarabonbaExecutor: %v", err)
	}
	recorder := &recordingHTTPClient{}
	executor.client.HttpClient = recorder
	request := &openAPIRequest{
		Product:     "Example",
		Version:     "2020-01-01",
		ApiName:     "GetExample",
		RegionId:    "cn-hangzhou",
		Domain:      "example.cn-hangzhou.aliyuncs.com",
		Scheme:      "https",
		Method:      "GET",
		Style:       "RPC",
		QueryParams: map[string]string{},
		Headers:     map[string]string{},
	}
	for i := 0; i < 2; i++ {
		if _, err := executor.ExecuteOpenAPI(context.Background(), request); err != nil {
			t.Fatalf("ExecuteOpenAPI %d: %v", i+1, err)
		}
	}
	if provider.calls != 2 || len(recorder.requests) != 2 {
		t.Fatalf("provider calls = %d requests = %d", provider.calls, len(recorder.requests))
	}
	if first, second := requestHeaderValue(recorder.requests[0], "Authorization"), requestHeaderValue(recorder.requests[1], "Authorization"); !strings.Contains(first, "Credential=id-1") || !strings.Contains(second, "Credential=id-2") {
		t.Fatalf("operation credential did not refresh: first=%q second=%q", first, second)
	}
}

func TestOpenAPICallerClassifiesDynamicCredentialRefreshFailure(t *testing.T) {
	provider := &failAfterCredentialsProvider{err: errors.New("CloudSSO access token is expired, please re-login with cli")}
	profile := resolvedOpenAPIProfile{
		Acquirer: &credentialsProviderAcquirer{provider: provider, mode: credentialModeCloudSSO},
		Mode:     credentialModeCloudSSO,
		AuthType: "AK",
		RegionID: "cn-hangzhou",
		Language: "en",
	}
	caller := &OpenAPICaller{Product: "Vpc", Region: "cn-hangzhou", Profile: profile}
	ctx, err := caller.PrepareOperationContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease, _ := ctx.Value(credentialLeaseContextKey{}).(*credentialLease)
	executor := lease.executor.(*darabonbaExecutor)
	executor.client.HttpClient = &recordingHTTPClient{}
	if _, err := caller.Call(ctx, "DescribeVpcs", map[string]any{"RegionId": "cn-hangzhou", "PageSize": 1}); err != nil {
		t.Fatalf("initial call: %v", err)
	}
	_, err = caller.Call(ctx, "DescribeVpcs", map[string]any{"RegionId": "cn-hangzhou", "PageSize": 1})
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialReauthenticationRequired" {
		t.Fatalf("error = %T %v", err, err)
	}
}

type failAfterCredentialsProvider struct {
	calls int
	err   error
}

func (p *failAfterCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	p.calls++
	if p.calls == 1 {
		return &credentialproviders.Credentials{AccessKeyId: "id", AccessKeySecret: "secret", SecurityToken: "sts", ProviderName: "test"}, nil
	}
	return nil, p.err
}

func (*failAfterCredentialsProvider) GetProviderName() string { return "test" }

type rotatingCredentialsProvider struct{ calls int }

func (p *rotatingCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	p.calls++
	return &credentialproviders.Credentials{
		AccessKeyId:     fmt.Sprintf("id-%d", p.calls),
		AccessKeySecret: "secret",
		ProviderName:    p.GetProviderName(),
	}, nil
}

func (p *rotatingCredentialsProvider) GetProviderName() string { return "rotating" }

type errorCredentialsProvider struct{ err error }

func (p *errorCredentialsProvider) GetCredentials() (*credentialproviders.Credentials, error) {
	return nil, p.err
}

func (p *errorCredentialsProvider) GetProviderName() string { return "failing" }

func requestHeaderValue(request *http.Request, name string) string {
	if request == nil {
		return ""
	}
	for key, values := range request.Header {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func TestDarabonbaExecutorPreservesTopLevelJSONArray(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-zhangjiakou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatalf("newDarabonbaExecutor: %v", err)
	}
	executor.client.HttpClient = &recordingHTTPClient{
		responseBody: `[{"version":"1.32.6-aliyun.1"}]`,
	}

	response, err := executor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product:     "CS",
		Version:     "2015-12-15",
		ApiName:     "DescribeKubernetesVersionMetadata",
		RegionId:    "cn-zhangjiakou",
		Domain:      "cs.cn-zhangjiakou.aliyuncs.com",
		Scheme:      "https",
		Method:      "GET",
		Style:       "ROA",
		QueryParams: map[string]string{"ClusterType": "ManagedKubernetes", "Mode": "creatable"},
		Headers:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("ExecuteOpenAPI: %v", err)
	}
	items, ok := response["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("response = %#v, want one wrapped top-level array item", response)
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["version"] != "1.32.6-aliyun.1" {
		t.Fatalf("items = %#v, want preserved version metadata", items)
	}
	if response[topLevelArrayResponseMarker] != true {
		t.Fatalf("response = %#v, want top-level array marker", response)
	}
}

func TestDarabonbaExecutorRestoresRequestScopedClientState(t *testing.T) {
	profile := testResolvedOpenAPIProfile(t, "cn-hangzhou")
	executor, err := newDarabonbaExecutor(profile, testCredentialSnapshot(t, profile.Acquirer))
	if err != nil {
		t.Fatalf("newDarabonbaExecutor: %v", err)
	}
	executor.client.HttpClient = &recordingHTTPClient{}

	_, err = executor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product:     "Vpc",
		Version:     "2016-04-28",
		ApiName:     "CreateVpc",
		RegionId:    "cn-heyuan",
		Domain:      "vpc.cn-heyuan.aliyuncs.com",
		Scheme:      "https",
		Method:      "POST",
		Style:       "RPC",
		QueryParams: map[string]string{"RegionId": "cn-heyuan", "CidrBlock": "10.99.0.0/16"},
		Headers:     map[string]string{},
	})
	if err != nil {
		t.Fatalf("ExecuteOpenAPI: %v", err)
	}
	if executor.client.Endpoint != nil {
		t.Fatalf("Endpoint leaked after request: %q", *executor.client.Endpoint)
	}
	if executor.client.ProductId != nil {
		t.Fatalf("ProductId leaked after request: %q", *executor.client.ProductId)
	}
	if executor.client.RegionId == nil || *executor.client.RegionId != "cn-hangzhou" {
		t.Fatalf("RegionId = %v, want original cn-hangzhou", executor.client.RegionId)
	}
}

func TestOpenAPICallerBuildsResourceManagerRequest(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"ResourceGroups":{"ResourceGroup":[]}}`}
	caller := &OpenAPICaller{
		Product:  "ResourceManager",
		Region:   "cn-beijing",
		executor: fake,
	}

	_, err := caller.Call(context.Background(), "ListResourceGroups", map[string]any{
		"PageNumber": 2,
		"PageSize":   20,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.Product != "ResourceManager" {
		t.Fatalf("product = %q, want ResourceManager", req.Product)
	}
	if req.Version != "2020-03-31" || req.ApiName != "ListResourceGroups" {
		t.Fatalf("request = product %q version %q api %q", req.Product, req.Version, req.ApiName)
	}
	if req.Domain != "resourcemanager.aliyuncs.com" {
		t.Fatalf("Domain = %q, want resourcemanager.aliyuncs.com", req.Domain)
	}
	if req.QueryParams["PageNumber"] != "2" || req.QueryParams["PageSize"] != "20" {
		t.Fatalf("query params = %#v", req.QueryParams)
	}
}

func TestOpenAPICallerUsesUnitizedTagEndpoint(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","Policies":[]}`}
	caller := &OpenAPICaller{
		Product:  "Tag",
		Region:   "cn-beijing",
		executor: fake,
	}

	_, err := caller.Call(context.Background(), "ListPolicies", map[string]any{
		"MaxResult": 10,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.Product != "Tag" || req.Version != "2018-08-28" || req.ApiName != "ListPolicies" {
		t.Fatalf("request = product %q version %q api %q", req.Product, req.Version, req.ApiName)
	}
	if req.Domain != "tag.cn-beijing.aliyuncs.com" {
		t.Fatalf("Domain = %q, want tag.cn-beijing.aliyuncs.com", req.Domain)
	}
}

func TestOpenAPICallerKeepsRegionIDQueryParameter(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1"}`}
	caller := &OpenAPICaller{Product: "Tag", Region: "cn-heyuan", executor: fake}

	_, err := caller.Call(context.Background(), "TagResources", map[string]any{
		"RegionId":    "cn-heyuan",
		"ResourceARN": []string{"arn:acs:vpc:cn-heyuan:1234567890123456:vpc/vpc-1"},
		"Tags":        `{"phase5":"20260706-143241"}`,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	req := fake.requests[0]
	if req.RegionId != "cn-heyuan" {
		t.Fatalf("request region = %q, want cn-heyuan", req.RegionId)
	}
	if req.QueryParams["RegionId"] != "cn-heyuan" {
		t.Fatalf("RegionId query = %q, want cn-heyuan; params=%#v", req.QueryParams["RegionId"], req.QueryParams)
	}
}

func TestDecodeCommonResponseWrapsTopLevelJSONArray(t *testing.T) {
	decoded, err := decodeOpenAPIResponse(`[{"id":"t-1"}]`)
	if err != nil {
		t.Fatalf("decodeOpenAPIResponse: %v", err)
	}
	items, ok := decoded["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("decoded = %#v, want items wrapper", decoded)
	}
}

func TestOpenAPICallerBuildsResourceManagerFilterRequest(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"ResourceGroups":{"ResourceGroup":[]}}`}
	caller := &OpenAPICaller{
		Product:  "ResourceManager",
		Region:   "cn-beijing",
		executor: fake,
	}

	_, err := caller.Call(context.Background(), "ListResourceGroups", map[string]any{
		"ResourceGroupIds": []string{"rg-123", "rg-456"},
		"Tag":              []string{"env=prod", "team=platform"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	params := fake.requests[0].QueryParams
	if params["ResourceGroupIds.1"] != "rg-123" || params["ResourceGroupIds.2"] != "rg-456" {
		t.Fatalf("ResourceGroupIds params = %#v", params)
	}
	if _, ok := params["ResourceGroupIds"]; ok {
		t.Fatalf("ResourceGroupIds should use flat repeated params: %#v", params)
	}
	if params["Tag.1.Key"] != "env" || params["Tag.1.Value"] != "prod" {
		t.Fatalf("tag 1 params = %#v", params)
	}
	if params["Tag.2.Key"] != "team" || params["Tag.2.Value"] != "platform" {
		t.Fatalf("tag 2 params = %#v", params)
	}
}

func TestOpenAPICallerBuildsCloneDisksRequest(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TaskGroupId":"tg-1"}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "disk", Region: "cn-beijing", executor: fake}

	resp, err := caller.Call(context.Background(), "CloneDisks", map[string]any{
		"RegionId":     "cn-beijing",
		"SourceDiskId": "d-1",
		"Size":         80,
		"DiskCategory": "cloud_essd",
		"MultiAttach":  "Disabled",
		"ClientToken":  "token-1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp["TaskGroupId"] != "tg-1" {
		t.Fatalf("response = %#v", resp)
	}
	req := fake.requests[0]
	if req.ApiName != "CloneDisks" {
		t.Fatalf("ApiName = %q", req.ApiName)
	}
	for key, want := range map[string]string{
		"SourceDiskId": "d-1",
		"Size":         "80",
		"DiskCategory": "cloud_essd",
		"MultiAttach":  "Disabled",
		"ClientToken":  "token-1",
	} {
		if got := req.QueryParams[key]; got != want {
			t.Fatalf("query param %s = %q, want %q; params=%#v", key, got, want, req.QueryParams)
		}
	}
}

func TestOpenAPICallerJoinsStringSlicesForCommaListParameters(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"Vpcs":{"Vpc":[]}}`}
	caller := &OpenAPICaller{Product: "vpc", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "DescribeVpcs", map[string]any{
		"RegionId": "cn-beijing",
		"VpcId":    []string{"vpc-1", "vpc-2"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := fake.requests[0].QueryParams["VpcId"]; got != "vpc-1,vpc-2" {
		t.Fatalf("VpcId = %q, want comma list", got)
	}
}

func TestOpenAPICallerFormatsSecurityGroupIDsAsJSONArray(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"SecurityGroups":{"SecurityGroup":[]}}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "DescribeSecurityGroups", map[string]any{
		"RegionId":          "cn-beijing",
		"SecurityGroupIds":  []string{"sg-1", "sg-2"},
		"SecurityGroupType": "normal",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := fake.requests[0].QueryParams["SecurityGroupIds"]; got != `["sg-1","sg-2"]` {
		t.Fatalf("SecurityGroupIds = %q, want JSON array", got)
	}
}

func TestOpenAPICallerEncodesInstanceIDsAsJSONArrayParameter(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"Instances":{"Instance":[]}}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{
		"RegionId":    "cn-beijing",
		"InstanceIds": []string{"i-1", "i-2"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := fake.requests[0].QueryParams["InstanceIds"]; got != `["i-1","i-2"]` {
		t.Fatalf("InstanceIds = %q, want JSON array", got)
	}
}

func TestOpenAPICallerEncodesRemoveNodePoolNodesQueryParameters(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"request_id":"req-1","task_id":"T-1"}`}
	caller := &OpenAPICaller{Product: "cs", Resource: "nodepool", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "RemoveNodePoolNodes", map[string]any{
		"ClusterId":    "c-123",
		"NodepoolId":   "np-123",
		"instance_ids": []string{"i-1", "i-2"},
		"release_node": true,
		"drain_node":   true,
		"concurrency":  true,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	params := fake.requests[0].QueryParams
	if got := params["instance_ids"]; got != `["i-1","i-2"]` {
		t.Fatalf("instance_ids = %q, want JSON array; params=%#v", got, params)
	}
	for _, key := range []string{"release_node", "drain_node", "concurrency"} {
		if got := params[key]; got != "true" {
			t.Fatalf("%s = %q, want true; params=%#v", key, got, params)
		}
	}
}

func TestOpenAPICallerEncodesJSONQueryParametersFromMetadata(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"request_id":"req-1","tag_resources":{"tag_resource":[]}}`}
	caller := &OpenAPICaller{Product: "cs", Resource: "ack", Region: "cn-hangzhou", executor: fake}

	_, err := caller.Call(context.Background(), "ListTagResources", map[string]any{
		"region_id":     "cn-hangzhou",
		"resource_type": "CLUSTER",
		"resource_ids":  []string{"c-123"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := fake.requests[0].QueryParams["resource_ids"]; got != `["c-123"]` {
		t.Fatalf("resource_ids = %q, want JSON array", got)
	}
}

func TestOpenAPICallerEncodesLingjunNodeListsAsJSONArrayParameters(t *testing.T) {
	fake := &fakeOpenAPIExecutor{responses: []string{
		`{"RequestId":"req-run","InvokeId":"invoke-1"}`,
		`{"RequestId":"req-reboot"}`,
		`{"RequestId":"req-vsc","Content":{"Total":0,"Data":[]}}`,
	}}
	caller := &OpenAPICaller{Product: "eflo-controller", Resource: "node", Region: "cn-beijing", executor: fake}

	if _, err := caller.Call(context.Background(), "RunCommand", map[string]any{
		"NodeIdList": []string{"node-1", "node-2"},
	}); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if _, err := caller.Call(context.Background(), "RebootNodes", map[string]any{
		"Nodes": []string{"node-1", "node-2"},
	}); err != nil {
		t.Fatalf("RebootNodes: %v", err)
	}
	if _, err := caller.Call(context.Background(), "ListVscs", map[string]any{
		"NodeIds": []string{"node-1", "node-2"},
	}); err != nil {
		t.Fatalf("ListVscs: %v", err)
	}
	if got := commonRequestParam(fake.requests[0], "NodeIdList"); got != `["node-1","node-2"]` {
		t.Fatalf("NodeIdList = %q, want JSON array", got)
	}
	if got := commonRequestParam(fake.requests[1], "Nodes"); got != `["node-1","node-2"]` {
		t.Fatalf("Nodes = %q, want JSON array", got)
	}
	if got := commonRequestParam(fake.requests[2], "NodeIds"); got != `["node-1","node-2"]` {
		t.Fatalf("NodeIds = %q, want JSON array", got)
	}
}

func TestOpenAPICallerPreservesStatusQueryValue(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","TotalCount":0,"Instances":{"Instance":[]}}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{
		"RegionId": "cn-beijing",
		"Status":   "running",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := fake.requests[0].QueryParams["Status"]; got != "running" {
		t.Fatalf("Status = %q, want raw caller input", got)
	}
}

func TestResourceExecutorResolvesECSImageNameWithInstanceType(t *testing.T) {
	fake := &fakeOpenAPIExecutor{responses: []string{
		`{"RequestId":"req-img","Images":{"Image":[{"ImageId":"centos-image-id"},{"ImageId":"centos-image-id-2"}]},"TotalCount":2}`,
		`{"RequestId":"req-run","InstanceIdSets":{"InstanceIdSet":["i-1"]}}`,
	}}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}
	resource := ecsInstanceSpecForCallerTest(t)

	result, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{
		Action: "create",
		Input: map[string]any{
			"image":   "centos",
			"type":    "ecs.u1-c1m2.xlarge",
			"sg":      "sg-1",
			"vswitch": "vsw-1",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.RequestID != "req-run" {
		t.Fatalf("result = %#v", result)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("request count = %d, want DescribeImages then RunInstances", len(fake.requests))
	}
	lookupParams := fake.requests[0].QueryParams
	if fake.requests[0].ApiName != "DescribeImages" {
		t.Fatalf("first ApiName = %q, want DescribeImages", fake.requests[0].ApiName)
	}
	if got := lookupParams["ImageName"]; got != "*centos*" {
		t.Fatalf("DescribeImages ImageName = %q, want fuzzy image name; params=%#v", got, lookupParams)
	}
	if got := lookupParams["InstanceType"]; got != "ecs.u1-c1m2.xlarge" {
		t.Fatalf("DescribeImages InstanceType = %q, want requested instance type; params=%#v", got, lookupParams)
	}
	params := fake.requests[1].QueryParams
	if got := params["ImageId"]; got != "centos-image-id" {
		t.Fatalf("ImageId = %q, want first resolved image ID; params=%#v", got, params)
	}
	if _, ok := params["ImageName"]; ok {
		t.Fatalf("ImageName should not be sent to RunInstances: %#v", params)
	}
	if _, ok := params["ImageFamily"]; ok {
		t.Fatalf("ImageFamily should not be sent to RunInstances: %#v", params)
	}
}

func TestResourceExecutorReportsMissingECSImageName(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-img","Images":{"Image":[]},"TotalCount":0}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}
	resource := ecsInstanceSpecForCallerTest(t)

	_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{
		Action: "create",
		Input: map[string]any{
			"image":   "centos",
			"type":    "ecs.u1-c1m2.xlarge",
			"sg":      "sg-1",
			"vswitch": "vsw-1",
			"no_wait": true,
		},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err == nil {
		t.Fatal("Call succeeded, want missing image error")
	}
	if !strings.Contains(err.Error(), `image "centos" not found`) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "ecs.u1-c1m2.xlarge") {
		t.Fatalf("error should mention instance type: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("request count = %d, want only DescribeImages", len(fake.requests))
	}
}

func TestOpenAPICallerKeepsExplicitECSImageID(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-1","InstanceIdSets":{"InstanceIdSet":["i-1"]}}`}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId":        "cn-beijing",
		"ImageId":         "aliyun_3_x64_20G_alibase_20240528.vhd",
		"InstanceType":    "ecs.u1-c1m2.xlarge",
		"VSwitchId":       "vsw-1",
		"SecurityGroupId": "sg-1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("request count = %d, want only RunInstances", len(fake.requests))
	}
	params := fake.requests[0].QueryParams
	if got := params["ImageId"]; got != "aliyun_3_x64_20G_alibase_20240528.vhd" {
		t.Fatalf("ImageId = %q, want explicit image ID; params=%#v", got, params)
	}
	if _, ok := params["ImageFamily"]; ok {
		t.Fatalf("ImageFamily should not be sent for explicit image ID: %#v", params)
	}
}

func TestOpenAPICallerNotFoundPreservesRawCloudCause(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callError: errors.New("SDK.ServerError\nErrorCode: InvalidVSwitchId.NotExist\nRequestId: req-vswitch\nMessage: vSwitch not exists"),
	}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId":        "cn-beijing",
		"ImageId":         "aliyun_3_x64_20G_alibase_20240528.vhd",
		"InstanceType":    "ecs.u1-c1m2.xlarge",
		"VSwitchId":       "vsw-123",
		"SecurityGroupId": "sg-123",
	})
	if err == nil {
		t.Fatal("Call succeeded, want not found error")
	}
	action := ecerrors.ActionFromError("RunInstances", err)
	if action.Code != "InvalidVSwitchId.NotExist" ||
		action.Message != "vSwitch not exists" ||
		action.RequestID != "req-vswitch" {
		t.Fatalf("action = %#v", action)
	}
}

func TestOpenAPICallerCloudAPIErrorUsesRawCloudMessage(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callError: errors.New("SDK.ServerError\nErrorCode: InvalidCidrBlock.Overlapped\nRecommend: https://api.aliyun.com/troubleshoot?q=InvalidCidrBlock.Overlapped\nRequestId: req-cidr\nMessage: Specified CIDR block overlapped with other subnets.\nRespHeaders: map[Connection:[keep-alive]]"),
	}
	caller := &OpenAPICaller{Product: "vpc", Resource: "vswitch", Region: "cn-beijing", executor: fake}

	_, err := caller.Call(context.Background(), "CreateVSwitch", map[string]any{
		"RegionId":  "cn-beijing",
		"VpcId":     "vpc-123",
		"ZoneId":    "cn-beijing-g",
		"CidrBlock": "10.0.1.0/24",
	})
	if err == nil {
		t.Fatal("Call succeeded, want cloud API error")
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T %v, want AppError", err, err)
	}
	payload := appErr.Payload()
	if payload.Code != "CloudAPIError" ||
		payload.Message != "Specified CIDR block overlapped with other subnets." {
		t.Fatalf("payload = %#v", payload)
	}
	action := ecerrors.ActionFromError("CreateVSwitch", err)
	if action.Code != "InvalidCidrBlock.Overlapped" ||
		action.Message != "Specified CIDR block overlapped with other subnets." ||
		action.RequestID != "req-cidr" {
		t.Fatalf("action = %#v", action)
	}
}

func TestOpenAPICallerRejectsUnsuccessfulBusinessResponse(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{
		"Code":"1006",
		"Message":"zoneId 参数为空",
		"RequestId":"req-business"
	}`}
	caller := &OpenAPICaller{
		Product:  "eflo",
		Resource: "vcc",
		Region:   "cn-hangzhou",
		executor: fake,
	}
	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})

	_, err := caller.Call(ctx, "CreateVcc", map[string]any{"RegionId": "cn-hangzhou"})
	if err == nil {
		t.Fatal("Call succeeded, want unsuccessful business response error")
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T %v, want AppError", err, err)
	}
	if payload := appErr.Payload(); payload.Code != "CloudAPIError" || payload.Message != "zoneId 参数为空" {
		t.Fatalf("payload = %#v", payload)
	}
	action := ecerrors.ActionFromError("CreateVcc", err)
	if action.Code != "1006" || action.Message != "zoneId 参数为空" || action.RequestID != "req-business" {
		t.Fatalf("action = %#v", action)
	}
	session.Finish("ecctl eflo vcc create", 1)
	for _, span := range exporter.GetSpans() {
		if span.Name == "ecctl.cloud.api.request" {
			if outcome := testSpanAttributes(span.Attributes)["ecctl.cloud.outcome"]; outcome != "error" {
				t.Fatalf("business error API span outcome = %#v, want error", outcome)
			}
			return
		}
	}
	t.Fatal("business error did not emit an API span")
}

func TestOpenAPICallerLeavesBusinessResponseVisibleForGenericCall(t *testing.T) {
	fake := &fakeOpenAPIExecutor{response: `{"Code":"1006","Message":"zoneId 参数为空"}`}
	caller := &OpenAPICaller{Product: "eflo", Region: "cn-hangzhou", executor: fake}

	response, err := caller.Call(context.Background(), "CreateVcc", map[string]any{"RegionId": "cn-hangzhou"})
	if err != nil {
		t.Fatalf("generic Call returned error: %v", err)
	}
	if response["Code"] != "1006" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResourceExecutorPassesRawIncorrectInstanceStatusThrough(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callErrors: []error{errors.New("SDK.ServerError\nErrorCode: IncorrectInstanceStatus\nMessage: The specified instance status does not support this operation.\nRequestId: req-1")},
	}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake}
	resource := ecsInstanceSpecForCallerTest(t)
	deleteBinding := resource.Bindings["delete_to_absent"]
	deleteBinding.Retry = spec.TransitionRetry{}
	resource.Bindings["delete_to_absent"] = deleteBinding

	_, err := engine.NewExecutor(resource, caller).Execute(context.Background(), engine.Request{
		Action:  "delete",
		Input:   map[string]any{"ids": []string{"i-1"}, "no_wait": true},
		Context: map[string]any{"region": "cn-beijing"},
	})
	if err == nil {
		t.Fatal("expected delete to fail with raw IncorrectInstanceStatus")
	}
	if strings.Contains(err.Error(), "InstanceStateConflict") {
		t.Fatalf("ecctl must not translate IncorrectInstanceStatus to InstanceStateConflict; err=%v", err)
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error must be AppError; got %T", err)
	}
	actions := appErr.Actions()
	if len(actions) != 1 {
		t.Fatalf("actions len = %d, want 1 (raw OpenAPI action passthrough)", len(actions))
	}
	if !strings.Contains(actions[0].Code, "IncorrectInstanceStatus") {
		t.Fatalf("actions[0].Code must keep raw IncorrectInstanceStatus; got %q", actions[0].Code)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("request count = %d, want 1 (DeleteInstance only; no extra DescribeInstances probe)", len(fake.requests))
	}
}

func ecsInstanceSpecForCallerTest(t *testing.T) spec.ResourceSpec {
	t.Helper()
	resource, err := spec.LoadResource("../../specs", "ecs", "instance")
	if err != nil {
		t.Fatalf("LoadResource ecs.instance: %v", err)
	}
	return resource
}

func TestOpenAPICallerConvertsDryRunOperationToEngineDryRunMarker(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callError: errors.New("DryRunOperation: request would have succeeded")}
	caller := &OpenAPICaller{Product: "vpc", Region: "cn-beijing", executor: fake}

	resp, err := caller.Call(context.Background(), "CreateVpc", map[string]any{
		"RegionId": "cn-beijing",
		"DryRun":   true,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !reflect.DeepEqual(resp, map[string]any{"DryRun": true}) {
		t.Fatalf("response = %#v", resp)
	}
}

func TestOpenAPICallerConvertsECSValidationPassedToEngineDryRunMarker(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callError: errors.New("code: 400, Request validation has been passed with DryRun flag set. request id: req-dry-run")}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-hangzhou", executor: fake}

	resp, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId": "cn-hangzhou",
		"DryRun":   true,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !reflect.DeepEqual(resp, map[string]any{"DryRun": true}) {
		t.Fatalf("response = %#v", resp)
	}
}

func TestOpenAPICallerDoesNotConvertECSValidationDryRunNearMiss(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callError: errors.New("request validation has been passed for credentials; DryRun flag set was rejected")}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-hangzhou", executor: fake}

	_, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId": "cn-hangzhou",
		"DryRun":   true,
	})
	if err == nil {
		t.Fatal("Call succeeded, want near-miss dry-run response to remain an error")
	}
}

func TestOpenAPICallerDoesNotConvertEmbeddedECSValidationPassedMessage(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callError: errors.New("authorization failed after Request validation has been passed with DryRun flag set")}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-hangzhou", executor: fake}

	_, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId": "cn-hangzhou",
		"DryRun":   true,
	})
	if err == nil {
		t.Fatal("Call succeeded, want embedded dry-run success text to remain an error")
	}
}

func TestOpenAPICallerDoesNotConvertValidationPassedMessageWithConflictingCode(t *testing.T) {
	fake := &fakeOpenAPIExecutor{callError: errors.New("ErrorCode: AuthorizationFailure\nMessage: Request validation has been passed with DryRun flag set.")}
	caller := &OpenAPICaller{Product: "ecs", Resource: "instance", Region: "cn-hangzhou", executor: fake}

	_, err := caller.Call(context.Background(), "RunInstances", map[string]any{
		"RegionId": "cn-hangzhou",
		"DryRun":   true,
	})
	if err == nil {
		t.Fatal("Call succeeded, want conflicting cloud error code to remain an error")
	}
}

func TestOpenAPICallerRejectsInvalidTagSyntax(t *testing.T) {
	caller := &OpenAPICaller{Product: "vpc", Region: "cn-beijing", executor: &fakeOpenAPIExecutor{}}
	_, err := caller.Call(context.Background(), "CreateVpc", map[string]any{
		"RegionId": "cn-beijing",
		"Tag":      []string{"invalid"},
	})
	if err == nil || !strings.Contains(err.Error(), "--tag must be key=value") {
		t.Fatalf("error = %v", err)
	}
}

func TestCallerSanitizeCloudErrorRedactsDenyPrincipal(t *testing.T) {
	err := errors.New("Message: Deny: LTAI5tPjvkyBdB91zBiPAW7b|source ip: 122.231.145.203")
	got := callerSanitizeCloudError(err)
	if strings.Contains(got, "LTAI5tPjvkyBdB91zBiPAW7b") {
		t.Fatalf("sanitized error leaked access key id: %q", got)
	}
	if !strings.Contains(got, "Deny: [REDACTED]|source ip: 122.231.145.203") {
		t.Fatalf("sanitized error = %q", got)
	}
}

func TestCallerSanitizeCloudErrorRedactsCredentialJSONVariants(t *testing.T) {
	for _, message := range []string{
		`{"accessKeySecret":"secret-value"}`,
		`{"securityToken":"token-value"}`,
		`{"bearer_token":"bearer-value"}`,
		`{"oauthRefreshToken":"refresh-value"}`,
	} {
		got := callerSanitizeCloudError(errors.New(message))
		if strings.Contains(got, "value") || got != "Alibaba Cloud API request failed" {
			t.Fatalf("sanitized error = %q", got)
		}
	}
}

func TestOpenAPICallerRedactsSignedURLFromActionError(t *testing.T) {
	signedURL := `https://ecs.cn-definitely-notreal-1.aliyuncs.com/?AccessKeyId=LTAI-test&Action=DescribeInstances&Signature=secret-signature&SignatureNonce=nonce-1`
	fake := &fakeOpenAPIExecutor{callError: errors.New(`Post "` + signedURL + `": dial tcp: lookup ecs.cn-definitely-notreal-1.aliyuncs.com: no such host`)}
	caller := &OpenAPICaller{Product: "ecs", Region: "cn-definitely-notreal-1", executor: fake}

	_, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{
		"RegionId": "cn-definitely-notreal-1",
		"PageSize": 1,
	})
	if err == nil {
		t.Fatal("Call should fail")
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error must be AppError; got %T", err)
	}
	action := ecerrors.ActionFromError("DescribeInstances", appErr)
	message := action.Message
	for _, leaked := range []string{"AccessKeyId", "Signature", "SignatureNonce", "LTAI-test", "secret-signature", signedURL} {
		if strings.Contains(message, leaked) {
			t.Fatalf("action message leaked %q: %q", leaked, message)
		}
	}
	if !strings.Contains(message, "lookup ecs.cn-definitely-notreal-1.aliyuncs.com: no such host") {
		t.Fatalf("action message lost diagnostic detail: %q", message)
	}
}

func writeJSONFile(t *testing.T, path string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readJSONObjectFromPath(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func profileMapFromConfig(t *testing.T, config map[string]any, name string) map[string]any {
	t.Helper()
	profiles, _ := config["profiles"].([]any)
	for _, raw := range profiles {
		profile, _ := raw.(map[string]any)
		if profile["name"] == name {
			return profile
		}
	}
	t.Fatalf("profile %s not found in %#v", name, config)
	return nil
}

func containsArgPair(args []string, key string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func argAfter(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func TestIsThrottling(t *testing.T) {
	throttling := []error{
		errors.New("SDK.ServerError\nErrorCode: Throttling\nMessage: Request was denied due to user flow control."),
		errors.New("SDK.ServerError\nErrorCode: Throttling.User\nMessage: too many requests"),
		errors.New("SDK.ServerError\nErrorCode: Throttling.Api\nMessage: slow down"),
		errors.New("flow control triggered"),
	}
	for _, err := range throttling {
		if !isThrottling(err) {
			t.Fatalf("isThrottling(%v) = false, want true", err)
		}
	}
	notThrottling := []error{
		nil,
		errors.New("SDK.ServerError\nErrorCode: IncorrectInstanceStatus\nMessage: running"),
		errors.New("SDK.ServerError\nErrorCode: InvalidInstanceId.NotFound\nMessage: not found"),
	}
	for _, err := range notThrottling {
		if isThrottling(err) {
			t.Fatalf("isThrottling(%v) = true, want false", err)
		}
	}
}

func TestCallRawRetriesThenSucceedsOnThrottling(t *testing.T) {
	throttle := errors.New("SDK.ServerError\nErrorCode: Throttling.User\nMessage: Request was denied due to user flow control.")
	fake := &fakeOpenAPIExecutor{
		callErrors: []error{throttle, throttle},
		response:   `{"RequestId":"req-ok","TotalCount":0,"Instances":{"Instance":[]}}`,
	}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake,
		Profile: resolvedOpenAPIProfile{
			Mode: credentialModeAK,
			Acquirer: &countingCredentialAcquirer{snapshot: credentialSnapshot{
				AccessKeyID: "id", AccessKeySecret: "secret", Type: "access_key",
			}},
		},
		sleepFn: func(context.Context, time.Duration) error { return nil },
	}
	acquirer := caller.Profile.Acquirer.(*countingCredentialAcquirer)

	exporter := tracetest.NewInMemoryExporter()
	ctx, session := telemetry.Start(telemetry.WithExporterForTest(context.Background(), exporter), telemetry.Options{
		Enabled: true, Surface: "public", Version: "test", ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	})
	resp, err := caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-beijing"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp["RequestId"] != "req-ok" {
		t.Fatalf("RequestId = %v, want req-ok", resp["RequestId"])
	}
	if len(fake.requests) != 3 {
		t.Fatalf("attempts = %d, want 3 (2 throttled + 1 success)", len(fake.requests))
	}
	session.Finish("ecctl call ecs DescribeInstances", 0)
	if acquirer.calls != 1 {
		t.Fatalf("credential acquisitions = %d, want exactly one for all retries and telemetry", acquirer.calls)
	}
	var operationID string
	apiSpans := 0
	for _, span := range exporter.GetSpans() {
		if span.Name != "ecctl.cloud.api.request" {
			continue
		}
		apiSpans++
		attrs := testSpanAttributes(span.Attributes)
		if attrs["ecctl.cloud.attempt"] != int64(apiSpans) {
			t.Fatalf("attempt attribute = %#v, want %d", attrs["ecctl.cloud.attempt"], apiSpans)
		}
		if apiSpans == 1 {
			operationID, _ = attrs["ecctl.cloud.operation_id"].(string)
		} else if attrs["ecctl.cloud.operation_id"] != operationID {
			t.Fatalf("operation ID changed: %#v", attrs)
		}
	}
	if apiSpans != 3 {
		t.Fatalf("API spans = %d, want 3", apiSpans)
	}
}

func TestCallRawRetryDoesNotGateOnFrozenInitialSnapshot(t *testing.T) {
	throttle := errors.New("SDK.ServerError\nErrorCode: Throttling.User\nMessage: Request was denied due to user flow control.")
	fake := &fakeOpenAPIExecutor{
		callErrors: []error{throttle},
		response:   `{"RequestId":"req-should-not-run"}`,
	}
	acquirer := &countingCredentialAcquirer{snapshot: credentialSnapshot{
		AccessKeyID: "principal-one", AccessKeySecret: "secret", Type: "access_key",
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	caller := &OpenAPICaller{
		Product: "ecs", Region: "cn-beijing", executor: fake,
		Profile:          resolvedOpenAPIProfile{Mode: credentialModeExternal, Acquirer: acquirer, CredentialPrincipal: "helper"},
		retryMaxAttempts: 3,
	}
	ctx, err := caller.PrepareOperationContext(context.Background())
	if err != nil {
		t.Fatalf("PrepareOperationContext: %v", err)
	}
	lease, _ := ctx.Value(credentialLeaseContextKey{}).(*credentialLease)
	caller.sleepFn = func(context.Context, time.Duration) error {
		lease.snapshot.ExpiresAt = time.Now().Add(-time.Second)
		return nil
	}

	_, err = caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-beijing"})
	if err != nil {
		t.Fatalf("retry failed after initial snapshot expired: %v", err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("attempts = %d, want retry to proceed", len(fake.requests))
	}
	if acquirer.calls != 1 {
		t.Fatalf("credential acquisitions = %d, want no mid-operation reacquire", acquirer.calls)
	}
}

func TestPreparedOperationAcquiresInitialCredentialOnceWithInjectedExecutor(t *testing.T) {
	acquirer := &renewableCountingCredentialAcquirer{&countingCredentialAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "principal-one", AccessKeySecret: "secret", Type: "access_key"},
		{AccessKeyID: "principal-two", AccessKeySecret: "secret", Type: "access_key"},
	}}}
	fake := &fakeOpenAPIExecutor{response: `{"RequestId":"req-ok","TotalCount":0,"Instances":{"Instance":[]}}`}
	caller := &OpenAPICaller{
		Product: "ecs", Region: "cn-beijing", executor: fake,
		Profile: resolvedOpenAPIProfile{Mode: credentialModeExternal, Acquirer: acquirer, CredentialPrincipal: "helper"},
	}
	ctx, err := caller.PrepareOperationContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := caller.Call(ctx, "DescribeInstances", map[string]any{"RegionId": "cn-beijing"}); err != nil {
			t.Fatalf("Call %d: %v", i+1, err)
		}
	}
	if acquirer.calls != 1 {
		t.Fatalf("credential acquisitions = %d, want one for complete operation", acquirer.calls)
	}
}

func TestPrepareOperationUsesOperationScopedCredentialSource(t *testing.T) {
	child := &countingCredentialAcquirer{snapshot: credentialSnapshot{AccessKeyID: "operation-id", AccessKeySecret: "secret", Type: "access_key"}}
	factory := &operationFactoryCredentialAcquirer{child: child}
	caller := &OpenAPICaller{
		Product: "ecs", Region: "cn-beijing", executor: &fakeOpenAPIExecutor{},
		Profile: resolvedOpenAPIProfile{Mode: credentialModeEcsRamRole, Acquirer: factory, CredentialPrincipal: "role"},
	}
	if _, err := caller.PrepareOperationContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if factory.operations != 1 || factory.acquisitions != 0 || child.calls != 1 {
		t.Fatalf("factory operations=%d acquisitions=%d child acquisitions=%d", factory.operations, factory.acquisitions, child.calls)
	}
}

func TestOperationTelemetryIdentityStaysBoundToFirstSignedSnapshot(t *testing.T) {
	provider := &rotatingCredentialsProvider{}
	acquirer := &credentialsProviderAcquirer{provider: provider, mode: credentialModeExternal}
	caller := &OpenAPICaller{
		Product: "Example", Region: "cn-hangzhou",
		Profile: resolvedOpenAPIProfile{Acquirer: acquirer, Mode: credentialModeExternal, AuthType: "AK", RegionID: "cn-hangzhou", CredentialPrincipal: "helper"},
	}
	lease, err := caller.acquireCredentialLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mainExecutor := lease.executor.(*darabonbaExecutor)
	mainRecorder := &recordingHTTPClient{}
	mainExecutor.client.HttpClient = mainRecorder
	request := &openAPIRequest{
		Product: "Example", Version: "2020-01-01", ApiName: "GetExample", RegionId: "cn-hangzhou",
		Domain: "example.cn-hangzhou.aliyuncs.com", Scheme: "https", Method: "GET", Style: "RPC",
		QueryParams: map[string]string{}, Headers: map[string]string{},
	}
	for i := 0; i < 2; i++ {
		if _, err := mainExecutor.ExecuteOpenAPI(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	identityExecutor := lease.identityExecutor.(*darabonbaExecutor)
	identityRecorder := &recordingHTTPClient{}
	identityExecutor.client.HttpClient = identityRecorder
	if _, err := identityExecutor.ExecuteOpenAPI(context.Background(), &openAPIRequest{
		Product: "Sts", Version: "2015-04-01", ApiName: "GetCallerIdentity", Domain: "sts.aliyuncs.com",
		Scheme: "https", Method: "GET", Style: "RPC", QueryParams: map[string]string{}, Headers: map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want initial plus one renewal", provider.calls)
	}
	mainFirst := requestHeaderValue(mainRecorder.requests[0], "Authorization")
	mainSecond := requestHeaderValue(mainRecorder.requests[1], "Authorization")
	identity := requestHeaderValue(identityRecorder.requests[0], "Authorization")
	if !strings.Contains(mainFirst, "Credential=id-1") || !strings.Contains(mainSecond, "Credential=id-2") || !strings.Contains(identity, "Credential=id-1") {
		t.Fatalf("main first=%q second=%q identity=%q", mainFirst, mainSecond, identity)
	}
}

func TestOperationRejectsRenewedCredentialFromDifferentIdentityBeforeRequest(t *testing.T) {
	originalResolver := resolveCredentialSnapshotIdentity
	var resolvedIDs []string
	resolveCredentialSnapshotIdentity = func(_ context.Context, _ resolvedOpenAPIProfile, snapshot *credentialSnapshot, _ string) (telemetry.Identity, error) {
		resolvedIDs = append(resolvedIDs, snapshot.AccessKeyID)
		identity := "account-a"
		if snapshot.AccessKeyID == "principal-two" {
			identity = "account-b"
		}
		return telemetry.Identity{Hash: identity, Type: "Account"}, nil
	}
	t.Cleanup(func() { resolveCredentialSnapshotIdentity = originalResolver })

	acquirer := &renewableCountingCredentialAcquirer{&countingCredentialAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "principal-one", AccessKeySecret: "secret", Type: "access_key"},
		{AccessKeyID: "principal-two", AccessKeySecret: "secret", Type: "access_key"},
	}}}
	caller := &OpenAPICaller{
		Product: "Example", Region: "cn-hangzhou",
		Profile: resolvedOpenAPIProfile{
			Acquirer: acquirer, Mode: credentialModeExternal, AuthType: "AK", RegionID: "cn-hangzhou",
			CredentialPrincipal: "helper", PinCredentialIdentity: true,
		},
	}
	lease, err := caller.acquireCredentialLease(withCredentialOperationRegion(context.Background(), "cn-hangzhou"))
	if err != nil {
		t.Fatal(err)
	}
	executor := lease.executor.(*darabonbaExecutor)
	recorder := &recordingHTTPClient{}
	executor.client.HttpClient = recorder
	request := &openAPIRequest{
		Product: "Example", Version: "2020-01-01", ApiName: "GetExample", RegionId: "cn-hangzhou",
		Domain: "example.cn-hangzhou.aliyuncs.com", Scheme: "https", Method: "GET", Style: "RPC",
		QueryParams: map[string]string{}, Headers: map[string]string{},
	}
	if _, err := executor.ExecuteOpenAPI(context.Background(), request); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := executor.ExecuteOpenAPI(context.Background(), request); !errors.Is(err, ErrCredentialIdentityChanged) {
		t.Fatalf("second request identity error = %v resolved=%#v acquisitions=%d", err, resolvedIDs, acquirer.calls)
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("business requests after identity change = %d, want 1", len(recorder.requests))
	}
}

func TestOperationCredentialReacquiresAfterInitialSnapshotExpires(t *testing.T) {
	base := &countingCredentialAcquirer{snapshots: []credentialSnapshot{
		{AccessKeyID: "principal-one", AccessKeySecret: "secret", Type: "access_key", ExpiresAt: time.Now().Add(10 * time.Millisecond)},
		{AccessKeyID: "principal-two", AccessKeySecret: "secret", Type: "access_key", ExpiresAt: time.Now().Add(time.Hour)},
	}}
	acquirer := &renewableCountingCredentialAcquirer{base}
	initial, err := acquirer.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	credential, err := newOperationCredential(acquirer, credentialModeExternal, initial)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	model, err := credential.GetCredential()
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	if model.AccessKeyId != nil {
		got = *model.AccessKeyId
	}
	if got != "principal-two" {
		t.Fatalf("refreshed access key = %q", got)
	}
	if acquirer.calls != 2 {
		t.Fatalf("credential acquisitions = %d, want initial plus refresh", acquirer.calls)
	}
}

func TestPrepareOperationRejectsNonRenewableCredentialShorterThanDeadline(t *testing.T) {
	acquirer := &staticCredentialAcquirer{snapshot: credentialSnapshot{
		AccessKeyID: "principal-one", AccessKeySecret: "secret", SecurityToken: "token", Type: "sts",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	caller := &OpenAPICaller{
		Product: "ecs", Region: "cn-beijing", executor: &fakeOpenAPIExecutor{},
		Profile: resolvedOpenAPIProfile{Mode: credentialModeStsToken, Acquirer: acquirer, CredentialPrincipal: "principal-one"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := caller.PrepareOperationContext(ctx)
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || appErr.Payload().Code != "CredentialLeaseExpired" {
		t.Fatalf("short static lease error = %T %v", err, err)
	}
}

type countingCredentialAcquirer struct {
	calls     int
	snapshot  credentialSnapshot
	snapshots []credentialSnapshot
	err       error
}

type renewableCountingCredentialAcquirer struct {
	*countingCredentialAcquirer
}

func (*renewableCountingCredentialAcquirer) Renewable() bool { return true }

type operationFactoryCredentialAcquirer struct {
	child        credentialAcquirer
	operations   int
	acquisitions int
}

func (a *operationFactoryCredentialAcquirer) Acquire(context.Context) (*credentialSnapshot, error) {
	a.acquisitions++
	return nil, errors.New("base credential source must not be acquired directly")
}

func (a *operationFactoryCredentialAcquirer) ForOperation(context.Context) (credentialAcquirer, error) {
	a.operations++
	return a.child, nil
}

func (a *countingCredentialAcquirer) Acquire(ctx context.Context) (*credentialSnapshot, error) {
	a.calls++
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if a.err != nil {
		return nil, a.err
	}
	selected := a.snapshot
	if len(a.snapshots) > 0 {
		index := a.calls - 1
		if index >= len(a.snapshots) {
			index = len(a.snapshots) - 1
		}
		selected = a.snapshots[index]
	}
	copy := selected
	return &copy, nil
}

func testSpanAttributes(attributes []attribute.KeyValue) map[string]any {
	out := make(map[string]any, len(attributes))
	for _, value := range attributes {
		out[string(value.Key)] = value.Value.AsInterface()
	}
	return out
}

func TestCallRawExhaustsThrottlingRetries(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callError: errors.New("SDK.ServerError\nErrorCode: Throttling\nMessage: Request was denied due to flow control."),
	}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake,
		retryMaxAttempts: 4,
		sleepFn:          func(context.Context, time.Duration) error { return nil },
	}

	_, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{"RegionId": "cn-beijing"})
	if err == nil {
		t.Fatalf("Call: expected throttling error after exhausting retries")
	}
	if code := ecerrors.ActionFromError("", err).Code; !strings.Contains(strings.ToLower(code), "throttl") {
		t.Fatalf("error code = %q, want a throttling code", code)
	}
	var appErr *ecerrors.AppError
	if !errors.As(err, &appErr) || !appErr.Payload().Retryable {
		t.Fatalf("error = %#v, want exhausted throttling to remain retryable by an outer waiter", err)
	}
	if len(fake.requests) != 4 {
		t.Fatalf("attempts = %d, want 4 (retryMaxAttempts)", len(fake.requests))
	}
}

func TestCallRawDoesNotRetryNonThrottling(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callError: errors.New("SDK.ServerError\nErrorCode: IncorrectInstanceStatus\nMessage: running"),
	}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake,
		sleepFn: func(context.Context, time.Duration) error { return nil },
	}

	_, err := caller.Call(context.Background(), "DeleteInstance", map[string]any{"InstanceId": "i-1"})
	if err == nil {
		t.Fatalf("Call: expected error")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry for non-throttling)", len(fake.requests))
	}
}

func TestCallRawStopsRetryingWhenContextCancelled(t *testing.T) {
	fake := &fakeOpenAPIExecutor{
		callError: errors.New("SDK.ServerError\nErrorCode: Throttling\nMessage: flow control"),
	}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake,
		sleepFn: func(ctx context.Context, _ time.Duration) error { return context.Canceled },
	}

	_, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{"RegionId": "cn-beijing"})
	if err == nil {
		t.Fatalf("Call: expected error")
	}
	// First attempt throttles, sleep reports cancellation, so we stop immediately.
	if len(fake.requests) != 1 {
		t.Fatalf("attempts = %d, want 1 (stopped on context cancel)", len(fake.requests))
	}
}

func TestThrottleBackoffStaysWithinBounds(t *testing.T) {
	base := 500 * time.Millisecond
	max := 10 * time.Second
	for attempt := 0; attempt < 8; attempt++ {
		for i := 0; i < 50; i++ {
			d := throttleBackoff(attempt, base, max)
			if d < 0 || d > max {
				t.Fatalf("attempt %d: backoff %v out of [0,%v]", attempt, d, max)
			}
		}
	}
}

func TestIsTransientNetworkError(t *testing.T) {
	transient := []error{
		errors.New("Post \"https://ecs.aliyuncs.com\": read tcp 1.2.3.4:80->5.6.7.8:443: read: connection reset by peer"),
		errors.New("unexpected EOF"),
		errors.New("net/http: request canceled (Client.Timeout exceeded while awaiting headers)"),
	}
	for _, err := range transient {
		if !isTransientNetworkError(err) {
			t.Fatalf("isTransientNetworkError(%v) = false, want true", err)
		}
	}
	if isTransientNetworkError(errors.New("ErrorCode: InvalidParameter")) {
		t.Fatal("isTransientNetworkError misclassified a non-network error")
	}
}

func TestCallRawRetriesTransientNetworkErrorOnReadOperation(t *testing.T) {
	reset := errors.New("Post \"https://ecs.aliyuncs.com\": read tcp: read: connection reset by peer")
	fake := &fakeOpenAPIExecutor{
		callErrors: []error{reset},
		response:   `{"RequestId":"req-ok","TotalCount":0,"Instances":{"Instance":[]}}`,
	}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "instance", Region: "cn-beijing", executor: fake,
		sleepFn: func(context.Context, time.Duration) error { return nil },
	}

	if _, err := caller.Call(context.Background(), "DescribeInstances", map[string]any{"RegionId": "cn-beijing"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("attempts = %d, want 2 (1 reset + 1 retry success)", len(fake.requests))
	}
}

func TestCallRawDoesNotRetryTransientNetworkErrorOnWriteOperation(t *testing.T) {
	reset := errors.New("Post \"https://ecs.aliyuncs.com\": read tcp: read: connection reset by peer")
	fake := &fakeOpenAPIExecutor{callError: reset}
	caller := &OpenAPICaller{
		Product: "ecs", Resource: "port-range-list", Region: "cn-beijing", executor: fake,
		sleepFn: func(context.Context, time.Duration) error { return nil },
	}

	if _, err := caller.Call(context.Background(), "CreatePortRangeList", map[string]any{"RegionId": "cn-beijing"}); err == nil {
		t.Fatal("Call: expected error (writes must not retry on network reset)")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry for a write op)", len(fake.requests))
	}
}
