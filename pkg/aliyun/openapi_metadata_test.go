package aliyun

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/internal/openapimeta"
)

func TestOpenAPINewMetadataReaders(t *testing.T) {
	product, ok := OpenAPIProductByCode("ecs", "en")
	if !ok {
		t.Fatal("OpenAPIProductByCode(ecs) did not find embedded metadata")
	}
	if product.Code != "Ecs" || product.Version != "2014-05-26" || product.Style != "rpc" {
		t.Fatalf("OpenAPIProductByCode(ecs) = %#v", product)
	}
	if operation, ok := OpenAPIOperationName(product, "describeinstances"); !ok || operation != "DescribeInstances" {
		t.Fatalf("OpenAPIOperationName(describeinstances) = %q, %v", operation, ok)
	}

	summary, ok := OpenAPIOperationSummaryFor("en", "ecs", "DescribeInstances")
	if !ok || summary.Title != "DescribeInstances" || summary.Summary == "" {
		t.Fatalf("OpenAPIOperationSummaryFor(DescribeInstances) = %#v, %v", summary, ok)
	}

	detail, ok := OpenAPIOperationDetailFor("en", product, "DescribeInstances")
	if !ok || detail.Name != "DescribeInstances" || detail.Method != "GET|POST" || len(detail.Parameters) == 0 {
		t.Fatalf("OpenAPIOperationDetailFor(DescribeInstances) = %#v, %v", detail, ok)
	}
}

func TestOpenAPIOperationNamePrefersCurrentCanonicalCase(t *testing.T) {
	product := OpenAPIProduct{
		APINames:        []string{"CurrentOperation", "currentoperation"},
		currentMetadata: true,
		currentAPINames: map[string]bool{"CurrentOperation": true},
	}
	operation, ok := OpenAPIOperationName(product, "currentoperation")
	if !ok || operation != "CurrentOperation" {
		t.Fatalf("OpenAPIOperationName = %q, %v; want CurrentOperation, true", operation, ok)
	}
}

func TestOpenAPIDetailFindParameterMatchesLegacyNestedParameter(t *testing.T) {
	detail := OpenAPIOperationDetail{
		Parameters: []OpenAPIParameter{
			{
				Name: "Container",
				SubParameters: []OpenAPIParameter{
					{Name: "Name", Position: "Query", Type: "String"},
				},
			},
			{Name: "Tags", Position: "Query", Type: "RepeatList"},
		},
	}

	param := detail.FindParameter("Container.1.Name")
	if param == nil || param.Name != "Name" {
		t.Fatalf("FindParameter nested = %#v, want Name", param)
	}
	param = detail.FindParameter("Tags.1")
	if param == nil || param.Name != "Tags" {
		t.Fatalf("FindParameter repeat list = %#v, want Tags", param)
	}
	if param := detail.FindParameter("Container.Name"); param != nil {
		t.Fatalf("FindParameter without repeat index = %#v, want nil", param)
	}
}

func TestOpenAPIMetadataResolverRejectsCurrentCatalogReadFailure(t *testing.T) {
	want := errors.New("broken current catalog")
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"ecs"}, func(language, path string) ([]byte, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("newOpenAPIMetadataResolverWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIMetadataResolverRejectsProductManifestFailure(t *testing.T) {
	want := errors.New("broken product manifest")
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Broken"}, func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return nil, want
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	})
	if !errors.Is(err, want) {
		t.Fatalf("newOpenAPIMetadataResolverWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIMetadataResolverRejectsProductManifestWithoutAPIs(t *testing.T) {
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Broken"}, func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return []byte(`{"version":"2020-01-01"}`), nil
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	})
	if err == nil {
		t.Fatal("product manifest without APIs unexpectedly succeeded")
	}
}

func TestOpenAPIMetadataResolverRejectsRequiredProductMissingFromCurrentCatalog(t *testing.T) {
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Ecs"}, func(language, path string) ([]byte, error) {
		if path != "/products.json" {
			t.Fatalf("unexpected metadata path %q", path)
		}
		return []byte(`{"products":[{"code":"Other"}]}`), nil
	})
	if err == nil {
		t.Fatal("required product missing from current catalog unexpectedly succeeded")
	}
}

func TestOpenAPIMetadataResolverValidatesOnlyRequiredProductManifests(t *testing.T) {
	resolver, err := newOpenAPIMetadataResolverWithReader("en", []string{"Required"}, func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Required"},{"code":"Unrelated"}]}`), nil
		case "/required/version.json":
			return []byte(`{"apis":{"DoThing":{}}}`), nil
		case "/unrelated/version.json":
			t.Fatal("strict loader read an unrelated product manifest")
			return nil, nil
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatalf("newOpenAPIMetadataResolverWithReader: %v", err)
	}
	if resolver == nil {
		t.Fatal("strict loader returned a nil resolver")
	}
}

func TestOpenAPIMetadataResolverCloneDisksIsCanonical(t *testing.T) {
	resolver, err := NewOpenAPIMetadataResolver("en", []string{"ecs"})
	if err != nil {
		t.Fatalf("NewOpenAPIMetadataResolver: %v", err)
	}
	for _, allowLegacy := range []bool{false, true} {
		leaves, operation, err := resolver.OperationLeaves("ECS", "clonedisks", allowLegacy)
		if err != nil || operation != "CloneDisks" || len(leaves) == 0 {
			t.Fatalf("canonical CloneDisks = %q, %d leaves, %v", operation, len(leaves), err)
		}
	}
}

func TestOpenAPIOperationDetailStrictRejectsBrokenCurrentDetail(t *testing.T) {
	for _, content := range []string{"", `{"name":`, `{}`, `{"name":"Current"}`, `{"name":"Current","parameters":null}`, `{"name":"Other","parameters":[]}`} {
		t.Run(content, func(t *testing.T) {
			read := func(language, path string) ([]byte, error) {
				switch path {
				case "/products.json":
					return []byte(`{"products":[{"code":"Test"}]}`), nil
				case "/test/version.json":
					return []byte(`{"apis":{"Current":{}}}`), nil
				case "/test/Current.json":
					if content == "" {
						return nil, errors.New("missing current detail")
					}
					return []byte(content), nil
				default:
					t.Fatalf("unexpected metadata path %q", path)
					return nil, nil
				}
			}
			resolver, err := newOpenAPIMetadataResolverWithReader("en", []string{"Test"}, read)
			if err != nil {
				t.Fatal(err)
			}
			for _, allowLegacy := range []bool{false, true} {
				if _, _, err := resolver.OperationLeaves("test", "Current", allowLegacy); err == nil {
					t.Fatal("broken current detail unexpectedly succeeded")
				}
			}
		})
	}
}

func TestOpenAPIOperationDetailStrictRejectsAbsentManifestOperation(t *testing.T) {
	for _, allowLegacy := range []bool{false, true} {
		resolver := &OpenAPIMetadataResolver{
			language: "en",
			products: map[string]OpenAPIProduct{"test": {
				Code: "Test", APINames: []string{"LegacyOnly"},
				currentMetadata: true, currentAPINames: map[string]bool{},
			}},
			read: func(language, path string) ([]byte, error) {
				t.Fatalf("absent operation unexpectedly read detail %q", path)
				return nil, nil
			},
		}
		if _, _, err := resolver.OperationLeaves("test", "LegacyOnly", allowLegacy); err == nil {
			t.Fatal("absent operation unexpectedly succeeded")
		}
	}
}

func TestOpenAPIOperationDetailStrictAllowsExplicitEmptyParameterList(t *testing.T) {
	detail, err := openAPIOperationDetailForStrictWithReader(
		"en",
		OpenAPIProduct{
			Code: "Test", APINames: []string{"NoParameters"},
			currentMetadata: true, currentAPINames: map[string]bool{"NoParameters": true},
		},
		"NoParameters",
		func(language, path string) ([]byte, error) {
			return []byte(`{"name":"NoParameters","parameters":[]}`), nil
		},
	)
	if err != nil {
		t.Fatalf("explicit empty parameter list failed: %v", err)
	}
	if detail.Parameters == nil || len(detail.Parameters) != 0 {
		t.Fatalf("explicit empty parameters = %#v, want non-nil empty slice", detail.Parameters)
	}
}

func TestOpenAPIOperationDetailFromNewMetaPreservesSubParameters(t *testing.T) {
	detail := openAPIOperationDetailFromNewMeta(OpenAPIProduct{}, &openAPINewDetail{
		Name: "Nested",
		Parameters: []openAPINewRequestParameter{{
			Name: "Group",
			Type: "RepeatList",
			SubParameters: []openAPINewRequestParameter{{
				Name: "CurrentOnlyChild",
				Type: "String",
			}},
		}},
	})
	if len(detail.Parameters) != 1 || len(detail.Parameters[0].SubParameters) != 1 ||
		detail.Parameters[0].SubParameters[0].Name != "CurrentOnlyChild" {
		t.Fatalf("converted nested parameters = %#v", detail.Parameters)
	}
}

func canonicalTestDetail(t *testing.T, code, operation string) OpenAPIOperationDetail {
	t.Helper()
	product, ok := OpenAPIProductByCode(code, "en")
	if !ok {
		t.Fatalf("missing product %s", code)
	}
	detail, ok := OpenAPIOperationDetailFor("en", product, operation)
	if !ok {
		t.Fatalf("missing canonical detail %s.%s", code, operation)
	}
	return detail
}

func TestCanonicalSnapshotAllProductsLoad(t *testing.T) {
	catalog, err := canonicalOpenAPIProducts()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 351 {
		t.Fatalf("snapshot products = %d, want 351", len(catalog))
	}
	products, err := OpenAPIProducts("en")
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]OpenAPIProduct{}
	for _, product := range products {
		index[strings.ToLower(product.Code)] = product
	}
	operationCount := 0
	for _, rawProduct := range catalog {
		product, ok := index[strings.ToLower(rawProduct.Code)]
		if !ok {
			t.Fatalf("snapshot product %s was silently skipped", rawProduct.Code)
		}
		if product.Version != rawProduct.DefaultVersion {
			t.Fatalf("%s version = %s, want default %s", product.Code, product.Version, rawProduct.DefaultVersion)
		}
		content, err := openapimeta.ReadFile("canonical/" + strings.ToLower(product.Code) + "/" + product.Version + "/version.json")
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			APIs map[string]json.RawMessage `json:"apis"`
		}
		if err := json.Unmarshal(content, &manifest); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(manifest.APIs))
		for name := range manifest.APIs {
			names = append(names, name)
		}
		sort.Strings(names)
		if !reflect.DeepEqual(product.APINames, names) {
			t.Fatalf("%s operation list is not the default-version manifest", product.Code)
		}
		operationCount += len(names)
		if len(names) > 0 {
			if _, ok := OpenAPIOperationDetailFor("en", product, names[0]); !ok {
				t.Fatalf("%s.%s default-version detail did not load", product.Code, names[0])
			}
		}
	}
	if operationCount != 26231 {
		t.Fatalf("snapshot operations = %d, want 26231", operationCount)
	}
}

func TestCanonicalDefaultVersionEndpointsAndLanguages(t *testing.T) {
	product, ok := OpenAPIProductByCode("OuTbOuNdBoT", "en")
	if !ok || product.Version != "2019-12-26" {
		t.Fatalf("plugin default must override latest 2025-11-11: %#v", product)
	}
	caller := &OpenAPICaller{Product: "OutboundBot", Region: "cn-shanghai", Profile: resolvedOpenAPIProfile{Language: "en"}}
	req, err := caller.commonRequest("AssignJobs", nil)
	if err != nil || req.Version != "2019-12-26" {
		t.Fatalf("default-version call = %#v, %v", req, err)
	}
	for _, language := range []string{"en", "zh"} {
		product, ok := OpenAPIProductByCode("eCs", language)
		if !ok {
			t.Fatal("Ecs missing")
		}
		if product.Name != localizedOpenAPIText(map[string]string{"en": "Elastic Compute Service", "zh": "云服务器 ECS"}, language) {
			t.Fatalf("localized product name = %q", product.Name)
		}
		if got := metadataEndpoint(product.Endpoints, "cn-hangzhou", ""); got != "ecs-cn-hangzhou.aliyuncs.com" {
			t.Fatalf("regional endpoint = %q", got)
		}
		if got := metadataEndpoint(product.Endpoints, "cn-hangzhou", "vpc"); got != "ecs-vpc.cn-hangzhou.aliyuncs.com" {
			t.Fatalf("VPC endpoint = %q", got)
		}
		summary, ok := OpenAPIOperationSummaryFor(language, "ECS", "DescribeInstances")
		if !ok || summary.Title != "DescribeInstances" || summary.Summary == "" {
			t.Fatalf("localized summary = %#v", summary)
		}
		content, err := openapimeta.ReadFile("canonical/ecs/2014-05-26/DescribeInstances.json")
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			DescriptionEN string                      `json:"description_en"`
			DescriptionZH string                      `json:"description_zh"`
			Parameters    []canonicalOpenAPIParameter `json:"parameters"`
		}
		if err := json.Unmarshal(content, &raw); err != nil {
			t.Fatal(err)
		}
		if summary.Summary != localizedOpenAPIText(map[string]string{"en": raw.DescriptionEN, "zh": raw.DescriptionZH}, language) {
			t.Fatalf("summary did not use canonical description for %s", language)
		}
		detail, ok := OpenAPIOperationDetailFor(language, product, "DescribeInstances")
		if !ok {
			t.Fatal("DescribeInstances missing")
		}
		for _, parameter := range raw.Parameters {
			got := detail.FindParameter(parameter.RawName)
			if got == nil || got.Description != localizedOpenAPIText(map[string]string{"en": parameter.HelpEN, "zh": parameter.HelpZH}, language) {
				t.Fatalf("%s parameter %s not localized from canonical", language, parameter.RawName)
			}
		}
		deprecated, ok := OpenAPIOperationSummaryFor(language, "ecs", "AddTags")
		if !ok || !deprecated.Deprecated || deprecated.Title != "AddTags" {
			t.Fatalf("deprecated summary = %#v", deprecated)
		}
		addTags, ok := OpenAPIOperationDetailFor(language, product, "AddTags")
		if !ok || !addTags.Deprecated {
			t.Fatal("AddTags lost deprecated flag")
		}
	}
	ram, ok := OpenAPIProductByCode("ram", "en")
	if !ok || ram.EndpointType != "global" || metadataEndpoint(ram.Endpoints, "cn-hangzhou", "") != "ram.aliyuncs.com" {
		t.Fatalf("global endpoint = %#v", ram.Endpoints)
	}
}

func TestCanonicalRPCRepeatFlatAndJSONRequests(t *testing.T) {
	detail := canonicalTestDetail(t, "ecs", "RunInstances")
	for name, wantType := range map[string]string{
		"Tag.1.Key": "String", "DataDisk.2.Size": "Integer", "CpuOptions.Core": "Integer",
		"CpuOptions.NestedVirtualization": "String", "ImageOptions.LoginAsNonRoot": "Boolean",
	} {
		param := detail.FindParameter(name)
		if param == nil || param.Type != wantType || param.Position != "Query" {
			t.Fatalf("FindParameter(%s) = %#v", name, param)
		}
	}
	for _, invalid := range []string{"Tag.Key", "Tag.0.Key", "Tag.bad.Key", "CpuOptions.1.NestedVirtualization", "image_options", "--image-options"} {
		if param := detail.FindParameter(invalid); param != nil {
			t.Fatalf("alias or invalid index %s resolved to %#v", invalid, param)
		}
	}
	caller := &OpenAPICaller{Product: "ecs", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
	req, err := caller.commonRequest("RunInstances", map[string]any{
		"Tag":                             []map[string]any{{"Key": "env", "Value": "test"}},
		"DataDisk":                        []any{map[string]any{"Size": 40}},
		"ImageOptions":                    map[string]any{"LoginAsNonRoot": true},
		"CpuOptions.NestedVirtualization": "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"Tag.1.Key": "env", "Tag.1.Value": "test", "DataDisk.1.Size": "40", "ImageOptions.LoginAsNonRoot": "true", "CpuOptions.NestedVirtualization": "enabled"} {
		if req.QueryParams[key] != value {
			t.Fatalf("query[%s] = %q, want %q", key, req.QueryParams[key], value)
		}
	}
	if req.QueryParams["Tag"] != "" || req.QueryParams["ImageOptions"] != "" {
		t.Fatalf("unflattened groups: %#v", req.QueryParams)
	}
	flat := canonicalTestDetail(t, "accessanalyzer", "CreateAnalyzer")
	param := flat.FindParameter("Configuration.OverPrivilegedConfiguration.UnusedAccessAge")
	if param == nil || param.Position != "FormData" || param.Type != "Integer" {
		t.Fatalf("nested flat field = %#v", param)
	}
	form := newOpenAPIRequest()
	if err := setOpenAPIParam(form, &flat, "Configuration", map[string]any{"OverPrivilegedConfiguration": map[string]any{"UnusedAccessAge": 90}}); err != nil {
		t.Fatal(err)
	}
	if form.FormParams["Configuration.OverPrivilegedConfiguration.UnusedAccessAge"] != "90" || len(form.QueryParams) != 0 {
		t.Fatalf("nested flat form = %#v", form)
	}
	for _, tc := range []struct {
		code, operation, parameter, position string
		value                                any
		want                                 string
	}{
		{"acc", "CreateImageCache", "NetworkConfig", "Query", map[string]any{"VSwitchIds": []string{"vsw-1"}}, `{"VSwitchIds":["vsw-1"]}`},
		{"advisor", "RefreshAdvisorCheck", "ResourceDimensionList", "FormData", []any{map[string]any{"Product": "ecs"}}, `[{"Product":"ecs"}]`},
	} {
		detail := canonicalTestDetail(t, tc.code, tc.operation)
		param := detail.FindParameter(tc.parameter)
		if param == nil || param.Type != "Json" || param.Position != tc.position || len(param.SubParameters) == 0 {
			t.Fatalf("JSON parameter = %#v", param)
		}
		req := newOpenAPIRequest()
		if err := setOpenAPIParam(req, &detail, tc.parameter, tc.value); err != nil {
			t.Fatal(err)
		}
		got := req.QueryParams
		if tc.position == "FormData" {
			got = req.FormParams
		}
		if got[tc.parameter] != tc.want {
			t.Fatalf("JSON %s = %q, want %q", tc.parameter, got[tc.parameter], tc.want)
		}
	}
}

func TestCanonicalROABodiesAndCLIContract(t *testing.T) {
	for _, tc := range []struct {
		code, operation, bodyName, bodyType string
		values                              map[string]any
		want                                any
	}{
		{"agentrun", "CreateTemplate", "body", "Struct", map[string]any{
			"body.templateName": "test", "body.cpu": 2.0, "body.networkConfiguration": `{"networkMode":"PUBLIC"}`,
		}, map[string]any{"templateName": "test", "cpu": 2.0, "networkConfiguration": map[string]any{"networkMode": "PUBLIC"}}},
		{"cs", "InstallClusterAddons", "body", "RepeatList", map[string]any{"ClusterId": "c-1", "body.1.name": "coredns"}, []any{map[string]any{"name": "coredns"}}},
		{"cs", "UnInstallClusterAddons", "addons", "RepeatList", map[string]any{"ClusterId": "c-1", "addons.1.name": "coredns", "addons.1.cleanup_cloud_resources": true}, []any{map[string]any{"name": "coredns", "cleanup_cloud_resources": true}}},
		{"fc", "InvokeFunction", "body", "String", map[string]any{"functionName": "hello", "body": "plain payload", "x-fc-log-type": "Tail"}, "plain payload"},
	} {
		t.Run(tc.code+"/"+tc.operation, func(t *testing.T) {
			detail := canonicalTestDetail(t, tc.code, tc.operation)
			param := openAPIBodyParameter(&detail)
			if param == nil || param.Name != tc.bodyName || param.Type != tc.bodyType {
				t.Fatalf("body = %#v", param)
			}
			if tc.code == "agentrun" && detail.FindParameter("body.networkConfiguration.networkMode") == nil {
				t.Fatal("synthetic body lost nested object fields")
			}
			caller := &OpenAPICaller{Product: tc.code, Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
			req, err := caller.commonRequest(tc.operation, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(req.BodyValue(), tc.want) {
				t.Fatalf("body = %#v, want %#v", req.BodyValue(), tc.want)
			}
			for key := range tc.values {
				if key == tc.bodyName || strings.HasPrefix(key, tc.bodyName+".") {
					if _, ok := req.QueryParams[key]; ok {
						t.Fatalf("body leaked to query: %s", key)
					}
					if _, ok := req.FormParams[key]; ok {
						t.Fatalf("body leaked to form: %s", key)
					}
				}
			}
			if tc.code == "fc" && (req.Headers["x-fc-log-type"] != "Tail" || req.BuildPath() != "/2023-03-30/functions/hello/invocations") {
				t.Fatalf("FC header/path = %#v, %s", req.Headers, req.BuildPath())
			}
			cli := &CLICommandCaller{Product: tc.code}
			args, err := cli.requestArgs(tc.operation, tc.values)
			if err != nil {
				t.Fatal(err)
			}
			want, err := cliParamValue(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if len(args) < 2 || args[0] != "--body" || args[1] != want {
				t.Fatalf("CLI body = %#v, want %q", args, want)
			}
			for _, arg := range args[2:] {
				if arg == "--"+tc.bodyName || strings.HasPrefix(arg, "--"+tc.bodyName+".") {
					t.Fatalf("body leaked to CLI flags: %#v", args)
				}
			}
		})
	}
}

func TestCanonicalAdapterRejectsInvalidManifestAndKeepsEmptyParameters(t *testing.T) {
	products := []canonicalOpenAPIProduct{{Code: "Test", DefaultVersion: "2026-01-01", Style: "restful"}}
	for _, content := range []string{`{`, `{}`, `{"version":"2026-01-01"}`, `{"version":"old","apis":{}}`} {
		_, err := readCanonicalOpenAPIMetadata("en", "/test/version.json", products, func(path string) ([]byte, error) { return []byte(content), nil })
		if err == nil {
			t.Fatalf("invalid canonical manifest accepted: %s", content)
		}
	}
	for _, suffix := range []string{"", `,"parameters":null`, `,"parameters":[]`} {
		content, err := readCanonicalOpenAPIMetadata("en", "/test/NoParameters.json", products, func(path string) ([]byte, error) {
			if path != "canonical/test/2026-01-01/NoParameters.json" {
				t.Fatalf("unexpected canonical path %s", path)
			}
			return []byte(`{"name":"NoParameters"` + suffix + `}`), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var detail openAPINewDetail
		if err := json.Unmarshal(content, &detail); err != nil {
			t.Fatal(err)
		}
		if (detail.Parameters != nil) != (suffix == `,"parameters":[]`) {
			t.Fatalf("adapter changed nil/empty distinction: %s", content)
		}
	}
}

func TestCanonicalParameterTypesLocationsAndAliases(t *testing.T) {
	for raw, normalized := range map[string]string{"string": "String", "int": "Integer", "float": "Float", "bool": "Boolean", "object": "Struct", "array": "RepeatList", "map": "Json", "any": "Json"} {
		got := normalizeCanonicalOpenAPIParameter("en", canonicalOpenAPIParameter{RawName: "Value", Type: raw}, "Body")
		if got.Type != normalized || got.Position != "Body" {
			t.Fatalf("%s = %#v", raw, got)
		}
	}
	for raw, normalized := range map[string]string{"query": "Query", "formData": "FormData", "body": "Body", "path": "Path", "header": "Header", "host": "Domain"} {
		var param canonicalOpenAPIParameter
		if err := json.Unmarshal([]byte(`{"name":"snake_name","raw_name":"RawName","options":["--alias"],"type":"object","fields":[{"raw_name":"Child","type":"bool"}]}`), &param); err != nil {
			t.Fatal(err)
		}
		param.Location = raw
		got := normalizeCanonicalOpenAPIParameter("en", param, "Query")
		if got.Name != "RawName" || got.Position != normalized || len(got.SubParameters) != 1 || got.SubParameters[0].Name != "Child" || got.SubParameters[0].Position != normalized {
			t.Fatalf("%s = %#v", raw, got)
		}
	}
	host := canonicalTestDetail(t, "fc-open", "ListFunctionAsyncInvokeConfigs")
	if p := host.FindParameter("AccountID"); p == nil || p.Position != "Domain" {
		t.Fatalf("canonical host parameter = %#v", p)
	}
}

func TestCanonicalMapAndNestedArrayRequests(t *testing.T) {
	for _, tc := range []struct {
		code, version, operation, key, position string
		value                                   any
		want                                    map[string]string
	}{
		{"websitebuild", "2025-04-29", "CreateAIStaffChat", "MetaData", "FormData", map[string]string{"k": "v"}, map[string]string{"MetaData.k": "v"}},
		{"pairecservice", "2022-12-13", "ListRecallManagementJobs", "Condition", "Query", map[string]any{"k": "v"}, map[string]string{"Condition.k": "v"}},
		{"ecs", "2014-05-26", "CreateDiagnosticReport", "AdditionalOptions", "Query", map[string]string{"k": "v"}, map[string]string{"AdditionalOptions": `{"k":"v"}`}},
		{"ecs", "2014-05-26", "InvokeCommand", "Parameters", "Query", map[string]any{"k": []any{"v"}}, map[string]string{"Parameters": `{"k":["v"]}`}},
		{"csas", "2023-01-20", "CreateApprovalProcess", "ProcessNodes", "FormData", []any{[]any{"u1", "u2"}}, map[string]string{"ProcessNodes.1.1": "u1", "ProcessNodes.1.2": "u2"}},
		{"csas", "2023-01-20", "CreateApprovalProcess", "ProcessNodes.1", "FormData", []string{"u1", "u2"}, map[string]string{"ProcessNodes.1.1": "u1", "ProcessNodes.1.2": "u2"}},
		{"csas", "2023-01-20", "CreateApprovalProcess", "ProcessNodes.1.2", "FormData", "u2", map[string]string{"ProcessNodes.1.2": "u2"}},
		{"dms-enterprise", "2018-11-01", "BatchDeleteDataLakePartitions", "PartitionValuesList", "Query", [][]string{{"u1", "u2"}, {"u3"}}, map[string]string{"PartitionValuesList.1.1": "u1", "PartitionValuesList.1.2": "u2", "PartitionValuesList.2.1": "u3"}},
	} {
		t.Run(tc.code+"/"+tc.operation+"/"+tc.key, func(t *testing.T) {
			caller := &OpenAPICaller{Product: tc.code, Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
			req, err := caller.commonRequest(tc.operation, map[string]any{tc.key: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			if req.Version != tc.version {
				t.Fatalf("version = %q, want %q", req.Version, tc.version)
			}
			assertSerializedOpenAPIParams(t, req, tc.position, tc.want)
		})
	}
}

func TestCanonicalSimpleArrayRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"any slice", []any{"request_count", "success_count"}, "request_count,success_count"},
		{"string slice", []string{"request_count", "success_count"}, "request_count,success_count"},
		{"array", [2]string{"request_count", "success_count"}, "request_count,success_count"},
		{"already joined", "request_count,success_count", "request_count,success_count"},
		{"empty any slice", []any{}, ""},
		{"empty string slice", []string{}, ""},
		{"nil any slice", []any(nil), ""},
		{"nil string slice", []string(nil), ""},
		{"nil", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caller := &OpenAPICaller{Product: "gpdb", Region: "cn-hangzhou", Profile: resolvedOpenAPIProfile{Language: "en"}}
			req, err := caller.commonRequest("DescribeModelOperatorUsage", map[string]any{"Keys": tc.value})
			if err != nil {
				t.Fatal(err)
			}
			if req.Version != "2016-05-03" {
				t.Fatalf("version = %q, want 2016-05-03", req.Version)
			}
			want := map[string]string{}
			if tc.want != "" {
				want["Keys"] = tc.want
			}
			assertSerializedOpenAPIParams(t, req, "Query", want)
		})
	}
}

func TestCanonicalNestedParameterStyleRequests(t *testing.T) {
	for _, tc := range []struct {
		name, schema, key string
		value             any
		want              map[string]string
	}{
		{
			name:   "map of arrays of maps",
			schema: `"type":"map","param_style":"flat","value":{"type":"array","element":{"type":"map","value":{"type":"string"}}}`,
			key:    "Root", value: map[string]any{"k": []any{map[string]string{"leaf": "v"}}},
			want: map[string]string{"Root.k.1.leaf": "v"},
		},
		{
			name:   "indexed map value",
			schema: `"type":"map","param_style":"flat","value":{"type":"array","element":{"type":"map","value":{"type":"string"}}}`,
			key:    "Root.k", value: []any{map[string]string{"leaf": "v"}},
			want: map[string]string{"Root.k.1.leaf": "v"},
		},
		{
			name:   "repeat list of maps of arrays",
			schema: `"type":"array","param_style":"repeatList","element":{"type":"map","value":{"type":"array","element":{"type":"string"}}}`,
			key:    "Root", value: []any{map[string]any{"k": []string{"u1", "u2"}}},
			want: map[string]string{"Root.1.k.1": "u1", "Root.1.k.2": "u2"},
		},
		{
			name:   "explicit JSON element overrides flat",
			schema: `"type":"array","param_style":"flat","element":{"type":"map","param_style":"json","value":{"type":"string"}}`,
			key:    "Root", value: []any{map[string]string{"k": "v"}},
			want: map[string]string{"Root.1": `{"k":"v"}`},
		},
		{
			name:   "explicit JSON value overrides flat",
			schema: `"type":"map","param_style":"flat","value":{"type":"array","param_style":"json","element":{"type":"string"}}`,
			key:    "Root", value: map[string]any{"k": []string{"u1", "u2"}},
			want: map[string]string{"Root.k": `["u1","u2"]`},
		},
		{
			name:   "field styles override inherited flat",
			schema: `"type":"object","param_style":"flat","fields":[{"raw_name":"Plain","type":"map","value":{"type":"string"}},{"raw_name":"Encoded","type":"map","param_style":"json","value":{"type":"string"}},{"raw_name":"Ids","type":"array","param_style":"simple","element":{"type":"string"}}]`,
			key:    "Root", value: map[string]any{"Plain": map[string]string{"k": "v"}, "Encoded": map[string]string{"k": "v"}, "Ids": []string{"u1", "u2"}},
			want: map[string]string{"Root.Plain.k": "v", "Root.Encoded": `{"k":"v"}`, "Root.Ids": "u1,u2"},
		},
		{
			name:   "JSON map remains one field",
			schema: `"type":"map","param_style":"json","value":{"type":"array","element":{"type":"string"}}`,
			key:    "Root", value: map[string]any{"k": []string{"u1", "u2"}},
			want: map[string]string{"Root": `{"k":["u1","u2"]}`},
		},
		{
			name:   "unstyled map preserves JSON default",
			schema: `"type":"map","value":{"type":"string"}`,
			key:    "Root", value: map[string]string{"k": "v"},
			want: map[string]string{"Root": `{"k":"v"}`},
		},
		{
			name:   "nil and empty nested lists preserve indexes",
			schema: `"type":"array","param_style":"flat","element":{"type":"array","element":{"type":"string"}}`,
			key:    "Root", value: []any{nil, []string{}, []string(nil), [1]string{"u1"}},
			want: map[string]string{"Root.4.1": "u1"},
		},
		{
			name:   "nil and empty nested maps are omitted",
			schema: `"type":"array","param_style":"flat","element":{"type":"map","value":{"type":"string"}}`,
			key:    "Root", value: []any{nil, map[string]string{}, map[string]string(nil)},
			want: map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			products := []canonicalOpenAPIProduct{{Code: "Test", DefaultVersion: "2026-01-01", Style: "rpc"}}
			read := func(language, path string) ([]byte, error) {
				return readCanonicalOpenAPIMetadata(language, path, products, func(string) ([]byte, error) {
					return []byte(`{"name":"Serialize","parameters":[{"raw_name":"Root","location":"formData",` + tc.schema + `}]}`), nil
				})
			}
			detail, err := openAPIOperationDetailForStrictWithReader("en", OpenAPIProduct{
				Code: "Test", Style: "rpc", currentMetadata: true, currentAPINames: map[string]bool{"Serialize": true},
			}, "Serialize", read)
			if err != nil {
				t.Fatal(err)
			}
			req := newOpenAPIRequest()
			if err := setOpenAPIParam(req, &detail, tc.key, tc.value); err != nil {
				t.Fatal(err)
			}
			assertSerializedOpenAPIParams(t, req, "FormData", tc.want)
		})
	}
}

func assertSerializedOpenAPIParams(t *testing.T, req *openAPIRequest, position string, want map[string]string) {
	t.Helper()
	for name, params := range map[string]map[string]string{"Query": req.QueryParams, "FormData": req.FormParams} {
		got := map[string]string{}
		for key, value := range params {
			if key != "RegionId" { // commonRequest may inject the caller's region.
				got[key] = value
			}
		}
		if name == position {
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s = %#v, want %#v", name, got, want)
			}
		} else if len(got) != 0 {
			t.Errorf("parameters leaked to %s: %#v", name, got)
		}
	}
}
